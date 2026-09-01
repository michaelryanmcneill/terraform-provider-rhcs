// Copyright Red Hat
// SPDX-License-Identifier: Apache-2.0

package common

import (
	"context"
	"net/http"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/ghttp"
	sdk "github.com/openshift-online/ocm-sdk-go"
	amsv1 "github.com/openshift-online/ocm-sdk-go/accountsmgmt/v1"
	cmv1 "github.com/openshift-online/ocm-sdk-go/clustersmgmt/v1"
	sdktesting "github.com/openshift-online/ocm-sdk-go/testing"
)

var _ = Describe("Notification contacts helpers", func() {
	Context("GetSubscriptionID", func() {
		It("returns the subscription ID when present", func() {
			cluster, err := cmv1.NewCluster().
				Subscription(cmv1.NewSubscription().ID("sub-123")).
				Build()
			Expect(err).NotTo(HaveOccurred())

			id, ok := GetSubscriptionID(cluster)
			Expect(ok).To(BeTrue())
			Expect(id).To(Equal("sub-123"))
		})

		It("returns false when cluster is nil", func() {
			id, ok := GetSubscriptionID(nil)
			Expect(ok).To(BeFalse())
			Expect(id).To(BeEmpty())
		})

		It("returns false when subscription is not set", func() {
			cluster, err := cmv1.NewCluster().Build()
			Expect(err).NotTo(HaveOccurred())

			id, ok := GetSubscriptionID(cluster)
			Expect(ok).To(BeFalse())
			Expect(id).To(BeEmpty())
		})
	})

	Context("API operations", func() {
		var (
			server              *ghttp.Server
			ca                  string
			connection          *sdk.Connection
			subscriptionsClient *amsv1.SubscriptionsClient
			ctx                 context.Context
		)

		BeforeEach(func() {
			server, ca = sdktesting.MakeTCPTLSServer()
			token := sdktesting.MakeTokenString("Bearer", 10*time.Minute)
			ctx = context.Background()
			var err error
			connection, err = sdk.NewConnectionBuilder().
				URL(server.URL()).
				TrustedCAFile(ca).
				Tokens(token).
				BuildContext(ctx)
			Expect(err).NotTo(HaveOccurred())
			subscriptionsClient = connection.AccountsMgmt().V1().Subscriptions()
		})

		AfterEach(func() {
			server.Close()
			connection.Close()
		})

		Context("FetchNotificationContacts", func() {
			It("returns sorted usernames from the subscription", func() {
				subJSON := `{
					"kind": "Subscription",
					"id": "sub-123",
					"notification_contacts": [
						{"kind": "Account", "username": "zuser"},
						{"kind": "Account", "username": "auser"}
					]
				}`
				server.AppendHandlers(
					ghttp.CombineHandlers(
						ghttp.VerifyRequest("GET", "/api/accounts_mgmt/v1/subscriptions/sub-123"),
						sdktesting.RespondWithJSON(http.StatusOK, subJSON),
					),
				)

				usernames, err := FetchNotificationContacts(ctx, subscriptionsClient, "sub-123")
				Expect(err).NotTo(HaveOccurred())
				Expect(usernames).To(Equal([]string{"auser", "zuser"}))
			})

			It("returns nil when no contacts are set", func() {
				subJSON := `{
					"kind": "Subscription",
					"id": "sub-123"
				}`
				server.AppendHandlers(
					ghttp.CombineHandlers(
						ghttp.VerifyRequest("GET", "/api/accounts_mgmt/v1/subscriptions/sub-123"),
						sdktesting.RespondWithJSON(http.StatusOK, subJSON),
					),
				)

				usernames, err := FetchNotificationContacts(ctx, subscriptionsClient, "sub-123")
				Expect(err).NotTo(HaveOccurred())
				Expect(usernames).To(BeNil())
			})

			It("returns an error when the API call fails", func() {
				server.AppendHandlers(
					ghttp.CombineHandlers(
						ghttp.VerifyRequest("GET", "/api/accounts_mgmt/v1/subscriptions/sub-123"),
						sdktesting.RespondWithJSON(http.StatusNotFound,
							`{"kind":"Error","id":"404","href":"/api/accounts_mgmt/v1/errors/404","code":"AMS-404","reason":"not found"}`),
					),
				)

				_, err := FetchNotificationContacts(ctx, subscriptionsClient, "sub-123")
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("can't read subscription"))
			})
		})

		Context("UpdateNotificationContacts", func() {
			It("sends a PATCH with account usernames", func() {
				server.AppendHandlers(
					ghttp.CombineHandlers(
						ghttp.VerifyRequest("PATCH", "/api/accounts_mgmt/v1/subscriptions/sub-123"),
						sdktesting.VerifyJQ(".notification_contacts[0].username", "user1"),
						sdktesting.VerifyJQ(".notification_contacts[1].username", "user2"),
						sdktesting.RespondWithJSON(http.StatusOK, `{
							"kind": "Subscription",
							"id": "sub-123",
							"notification_contacts": [
								{"kind": "Account", "username": "user1"},
								{"kind": "Account", "username": "user2"}
							]
						}`),
					),
				)

				err := UpdateNotificationContacts(ctx, subscriptionsClient, "sub-123", []string{"user1", "user2"})
				Expect(err).NotTo(HaveOccurred())
			})

			It("sends a PATCH with empty contacts to clear", func() {
				server.AppendHandlers(
					ghttp.CombineHandlers(
						ghttp.VerifyRequest("PATCH", "/api/accounts_mgmt/v1/subscriptions/sub-123"),
						sdktesting.RespondWithJSON(http.StatusOK, `{
							"kind": "Subscription",
							"id": "sub-123"
						}`),
					),
				)

				err := UpdateNotificationContacts(ctx, subscriptionsClient, "sub-123", []string{})
				Expect(err).NotTo(HaveOccurred())
			})

			It("returns an error when the API call fails", func() {
				server.AppendHandlers(
					ghttp.CombineHandlers(
						ghttp.VerifyRequest("PATCH", "/api/accounts_mgmt/v1/subscriptions/sub-123"),
						sdktesting.RespondWithJSON(http.StatusForbidden,
							`{"kind":"Error","id":"403","href":"/api/accounts_mgmt/v1/errors/403","code":"AMS-403","reason":"forbidden"}`),
					),
				)

				err := UpdateNotificationContacts(ctx, subscriptionsClient, "sub-123", []string{"user1"})
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("can't update notification contacts"))
			})
		})

		Context("ResolveNotificationContacts", func() {
			It("returns contacts from the subscription", func() {
				cluster, err := cmv1.NewCluster().
					Subscription(cmv1.NewSubscription().ID("sub-123")).
					Build()
				Expect(err).NotTo(HaveOccurred())

				subJSON := `{
					"kind": "Subscription",
					"id": "sub-123",
					"notification_contacts": [
						{"kind": "Account", "username": "alice"},
						{"kind": "Account", "username": "bob"}
					]
				}`
				server.AppendHandlers(
					ghttp.CombineHandlers(
						ghttp.VerifyRequest("GET", "/api/accounts_mgmt/v1/subscriptions/sub-123"),
						sdktesting.RespondWithJSON(http.StatusOK, subJSON),
					),
				)

				listVal, diags := ResolveNotificationContacts(ctx, subscriptionsClient, cluster)
				Expect(diags).To(BeEmpty())
				Expect(listVal.IsNull()).To(BeFalse())
				Expect(listVal.Elements()).To(HaveLen(2))
			})

			It("returns a warning when subscription ID is not available", func() {
				cluster, err := cmv1.NewCluster().Build()
				Expect(err).NotTo(HaveOccurred())

				listVal, diags := ResolveNotificationContacts(ctx, subscriptionsClient, cluster)
				Expect(diags).To(HaveLen(1))
				Expect(diags[0].Detail()).To(ContainSubstring("subscription ID is not available"))
				Expect(listVal.IsNull()).To(BeTrue())
			})

			It("returns a warning when the API call fails", func() {
				cluster, err := cmv1.NewCluster().
					Subscription(cmv1.NewSubscription().ID("sub-123")).
					Build()
				Expect(err).NotTo(HaveOccurred())

				server.RouteToHandler("GET",
					"/api/accounts_mgmt/v1/subscriptions/sub-123",
					sdktesting.RespondWithJSON(http.StatusNotFound,
						`{"kind":"Error","id":"404","href":"/api/accounts_mgmt/v1/errors/404","code":"AMS-404","reason":"not found"}`),
				)

				listVal, diags := ResolveNotificationContacts(ctx, subscriptionsClient, cluster)
				Expect(diags).To(HaveLen(1))
				Expect(diags[0].Detail()).To(ContainSubstring("Could not read notification contacts"))
				Expect(listVal.IsNull()).To(BeTrue())
			})

			It("returns null list when no contacts are set", func() {
				cluster, err := cmv1.NewCluster().
					Subscription(cmv1.NewSubscription().ID("sub-123")).
					Build()
				Expect(err).NotTo(HaveOccurred())

				subJSON := `{
					"kind": "Subscription",
					"id": "sub-123"
				}`
				server.AppendHandlers(
					ghttp.CombineHandlers(
						ghttp.VerifyRequest("GET", "/api/accounts_mgmt/v1/subscriptions/sub-123"),
						sdktesting.RespondWithJSON(http.StatusOK, subJSON),
					),
				)

				listVal, diags := ResolveNotificationContacts(ctx, subscriptionsClient, cluster)
				Expect(diags).To(BeEmpty())
				Expect(listVal.IsNull()).To(BeTrue())
			})
		})
	})
})
