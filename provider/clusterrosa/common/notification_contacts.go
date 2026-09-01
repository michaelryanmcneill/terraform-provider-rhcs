// Copyright Red Hat
// SPDX-License-Identifier: Apache-2.0

package common

import (
	"context"
	"fmt"
	"sort"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	amsv1 "github.com/openshift-online/ocm-sdk-go/accountsmgmt/v1"
	cmv1 "github.com/openshift-online/ocm-sdk-go/clustersmgmt/v1"

	"github.com/terraform-redhat/terraform-provider-rhcs/provider/common"
)

const notificationContactsResourceDescription = "List of OCM account usernames " +
	"to receive cluster notification emails. " +
	"All contacts must belong to the same Red Hat organization " +
	"as the cluster. " +
	"This attribute is configured after cluster creation (Day-2). " +
	"By default, the cluster creator is set as the notification " +
	"contact. For clusters created with service accounts, " +
	"no default contact is set."

const notificationContactsDatasourceDescription = "List of OCM account " +
	"usernames configured to receive cluster notification emails."

// NotificationContactsResourceSchema returns the schema definition for the notification_contacts
// resource attribute.
func NotificationContactsResourceSchema() schema.ListAttribute {
	return schema.ListAttribute{
		Description: notificationContactsResourceDescription,
		ElementType: types.StringType,
		Optional:    true,
		Computed:    true,
		PlanModifiers: []planmodifier.List{
			listplanmodifier.UseStateForUnknown(),
		},
	}
}

// NotificationContactsDatasourceSchema returns the schema definition for the notification_contacts
// data source attribute.
func NotificationContactsDatasourceSchema() schema.ListAttribute {
	return schema.ListAttribute{
		Description: notificationContactsDatasourceDescription,
		ElementType: types.StringType,
		Computed:    true,
	}
}

// GetSubscriptionID extracts the subscription ID from a ClustersMgmt Cluster object.
func GetSubscriptionID(cluster *cmv1.Cluster) (string, bool) {
	if cluster == nil {
		return "", false
	}
	sub := cluster.Subscription()
	if sub == nil {
		return "", false
	}
	id := sub.ID()
	return id, id != ""
}

// FetchNotificationContacts reads notification contacts from the subscription.
func FetchNotificationContacts(
	ctx context.Context,
	subscriptionsClient *amsv1.SubscriptionsClient,
	subscriptionID string,
) ([]string, error) {
	resp, err := subscriptionsClient.Subscription(subscriptionID).Get().SendContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("can't read subscription '%s': %w", subscriptionID, err)
	}
	contacts, ok := resp.Body().GetNotificationContacts()
	if !ok {
		return nil, nil
	}
	usernames := make([]string, 0, len(contacts))
	for _, account := range contacts {
		if u := account.Username(); u != "" {
			usernames = append(usernames, u)
		}
	}
	sort.Strings(usernames)
	return usernames, nil
}

// UpdateNotificationContacts patches the subscription's notification contacts.
func UpdateNotificationContacts(
	ctx context.Context,
	subscriptionsClient *amsv1.SubscriptionsClient,
	subscriptionID string,
	usernames []string,
) error {
	builders := make([]*amsv1.AccountBuilder, len(usernames))
	for i, u := range usernames {
		builders[i] = amsv1.NewAccount().Username(u)
	}
	sub, err := amsv1.NewSubscription().NotificationContacts(builders...).Build()
	if err != nil {
		return fmt.Errorf("can't build subscription object: %w", err)
	}
	_, err = subscriptionsClient.Subscription(subscriptionID).Update().Body(sub).SendContext(ctx)
	if err != nil {
		return fmt.Errorf("can't update notification contacts for subscription '%s': %w", subscriptionID, err)
	}
	return nil
}

// ResolveNotificationContacts resolves notification contacts from the cluster's subscription,
// returning a types.List suitable for Terraform state.
func ResolveNotificationContacts(
	ctx context.Context,
	subscriptionsClient *amsv1.SubscriptionsClient,
	cluster *cmv1.Cluster,
) (types.List, diag.Diagnostics) {
	subID, ok := GetSubscriptionID(cluster)
	if !ok {
		return types.ListNull(types.StringType), diag.Diagnostics{
			diag.NewWarningDiagnostic(
				"Can't read notification contacts",
				"Cluster subscription ID is not available. "+
					"Notification contacts will be populated on the next terraform apply.",
			),
		}
	}

	usernames, err := FetchNotificationContacts(ctx, subscriptionsClient, subID)
	if err != nil {
		return types.ListNull(types.StringType), diag.Diagnostics{
			diag.NewWarningDiagnostic(
				"Can't read notification contacts",
				fmt.Sprintf(
					"Could not read notification contacts from the API: %v. "+
						"Run terraform apply again to refresh.",
					err,
				),
			),
		}
	}

	if len(usernames) == 0 {
		return types.ListNull(types.StringType), nil
	}

	listVal, err := common.StringArrayToList(usernames)
	if err != nil {
		return types.ListNull(types.StringType), diag.Diagnostics{
			diag.NewWarningDiagnostic(
				"Can't convert notification contacts",
				fmt.Sprintf("Failed to convert notification contacts to Terraform list: %v", err),
			),
		}
	}
	return listVal, nil
}
