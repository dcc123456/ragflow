//
//  Copyright (c) 2026 InfiniFlow. All rights reserved.
//
//  RAGFlow Enterprise Edition — Proprietary and Confidential.
//
//  This file is part of the RAGFlow Enterprise Edition and is licensed,
//  not sold, under the RAGFlow Enterprise Commercial License Agreement.
//  Unauthorized copying, modification, distribution, or disclosure of this
//  file, via any medium, is strictly prohibited.
//
//  Portions of this file may incorporate code from RAGFlow Community Edition,
//  which is licensed under the Apache License, Version 2.0.
//
//  For commercial licensing inquiries: sales@infiniflow.ai
//

//go:build ee

package router

import (
	"ragflow/internal/handler"
	"ragflow/internal/observability"
	"ragflow/internal/server"
	"ragflow/internal/utility"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
)

func SetupEERouter(engine *gin.Engine) {
	engine.Use(apiRetrievalMetrics())
	serverName := server.GetServerName()
	engine.Use(otelgin.Middleware(serverName))
	engine.Use(utility.GetRateLimit().Middleware())
	engine.Use(handler.RegistrationWhitelist())
}

func RegisterEENoAuthRouter(engine *gin.Engine, apiNoAuth *gin.RouterGroup, r *Router) {
	engine.GET("/metrics", gin.WrapH(observability.MetricsHandler()))
	// Banner and feature-exposure settings the web client reads before login.
	apiNoAuth.GET("/notification", r.systemHandler.Notification)
	apiNoAuth.GET("/expose-model-provider", r.systemHandler.ExposeModelProvider)
	apiNoAuth.GET("/expose-api", r.systemHandler.ExposeAPI)

	// For EE
	apiNoAuth.GET("/auth/login/channels", r.userHandler.GetLoginChannels)
	apiNoAuth.GET("/auth/login/:channel", r.userHandler.OAuthLogin)
	apiNoAuth.GET("/auth/oauth/:channel/callback", r.userHandler.OAuthChannelCallback)
	apiNoAuth.GET("/auth/oauth/callback", r.userHandler.OAuthCallback)
	apiNoAuth.POST("/auth/register/captcha", r.userHandler.Captcha)
	apiNoAuth.POST("/auth/register/otp", r.userHandler.SendOTP)
	apiNoAuth.POST("/auth/register/otp/verify", r.userHandler.VerifyOTP)
	apiNoAuth.POST("/auth/password/forgot/captcha", r.userHandler.ForgotCaptcha)
	apiNoAuth.POST("/auth/password/forgot/otp", r.userHandler.ForgotSendOTP)
	apiNoAuth.POST("/auth/password/forgot/otp/verify", r.userHandler.ForgotVerifyOTP)
	apiNoAuth.POST("/auth/password/reset", r.userHandler.ForgotResetPassword)
	apiNoAuth.GET("/tenant-invitations/:token", r.tenantHandler.GetTenantInviteLinkInfo)

	// Billing routes that must stay anonymous: the web client and Stripe
	// both reach them without a session.
	billingHandler := handler.NewBillingHandler()
	apiNoAuth.GET("/billing/status", billingHandler.Status)
	apiNoAuth.GET("/billing/plans", billingHandler.Plans)
	apiNoAuth.GET("/billing/downgrade-guard/health", billingHandler.Health)
	apiNoAuth.POST("/billing/webhooks/stripe", billingHandler.Webhook)
}

// RegisterEEAuthRouter registers authenticated enterprise routes.
func RegisterEEAuthRouter(authorized *gin.RouterGroup, r *Router) {
	authorized.GET("/api/v1/heartbeat", userHeartbeat)
	billingHandler := handler.NewBillingHandler()
	{
		group := authorized.Group("/api/v1/billing")
		group.GET("/subscription", billingHandler.CurrentSubscription)
		group.GET("/subscription/overview", billingHandler.SubscriptionOverview)
		group.GET("/storage", billingHandler.Storage)
		group.GET("/addons", billingHandler.Addons)
		group.GET("/addons/overview", billingHandler.AddonOverview)
		group.GET("/points/price", billingHandler.PointsPrice)
		group.GET("/points/balance", billingHandler.PointsBalance)
		group.GET("/points/overview", billingHandler.PointsBalance)
		group.GET("/points/ledger", billingHandler.PointsLedger)
		group.GET("/points/holds", billingHandler.PointsHolds)
		group.GET("/spend-overview", billingHandler.SpendOverview)
		group.GET("/spend-metrics", billingHandler.SpendMetrics)
		group.GET("/usages/deepdoc", billingHandler.DeepdocUsage)

		group.POST("/subscription", billingHandler.CheckoutSubscription)
		group.PATCH("/subscription", billingHandler.CheckoutSubscription)
		group.POST("/subscription/preview", billingHandler.PreviewSubscription)
		group.POST("/addon-purchases", billingHandler.AddonCheckout)
		group.PATCH("/storage", billingHandler.SetStorageTarget)
		group.POST("/setup-intents", billingHandler.SetupIntent)
		group.POST("/portal-sessions", billingHandler.PortalSession)
		group.POST("/points/checkout", billingHandler.PointsCheckout)
		group.GET("/checkouts/:session-id", billingHandler.CheckoutSession)
		group.GET("/subscriptions/:subscription-id/payment-status", billingHandler.SubscriptionPaymentStatus)
	}
}

func RegisterEEProtectedRouter(authorized *gin.RouterGroup, r *Router) {
	authorized.POST("/api/v1/tenants/:tenant_id/switch", r.tenantHandler.SwitchTenant)
	authorized.POST("/api/v1/tenants/:tenant_id/invitation-links", r.tenantHandler.CreateTenantInviteLink)
	authorized.POST("/api/v1/tenant-invitations/:token/accept", r.tenantHandler.AcceptTenantInviteLink)

	permissions := authorized.Group("/api/v1/permission")
	permissions.PUT("/update", r.tenantHandler.UpdateResourcePermissions)
	permissions.PUT("/transfer_owner", r.tenantHandler.TransferResourceOwnership)
	permissions.POST("/list", r.tenantHandler.ListResourcePermissions)
	permissions.POST("/list_by_target", r.tenantHandler.ListResourcePermissionsByTarget)

	// Tenant-scoped role configuration uses the Tenant carried by the current token.
	tenant := authorized.Group("/api/v1/tenant")
	tenant.GET("/groups", r.tenantHandler.ListGroups)
	tenant.POST("/groups", r.tenantHandler.CreateGroup)
	tenant.GET("/groups/:group_id", r.tenantHandler.GetGroup)
	tenant.PUT("/groups/:group_id", r.tenantHandler.UpdateGroup)
	tenant.PUT("/groups/:group_id/parent", r.tenantHandler.MoveGroup)
	tenant.DELETE("/groups/:group_id", r.tenantHandler.DeleteGroup)
	tenant.GET("/groups/:group_id/members", r.tenantHandler.ListGroupMembers)
	tenant.POST("/groups/:group_id/members", r.tenantHandler.InviteGroupMember)
	tenant.DELETE("/groups/:group_id/members/:user_id", r.tenantHandler.RemoveGroupMember)
	tenant.PUT("/groups/:group_id/members/:user_id/permissions", r.tenantHandler.SetGroupMemberPermissions)
	tenant.PUT("/groups/:group_id/owner", r.tenantHandler.TransferGroupOwnership)
	tenant.GET("/group-invitations", r.tenantHandler.ListGroupInvitations)
	tenant.POST("/group-invitations/:invitation_id/response", r.tenantHandler.RespondToGroupInvitation)
	tenant.GET("/roles", r.tenantHandler.ListTenantRoles)
	tenant.POST("/roles", r.tenantHandler.CreateTenantRole)
	tenant.GET("/roles/:role_id", r.tenantHandler.GetTenantRole)
	tenant.PATCH("/roles/:role_id", r.tenantHandler.UpdateTenantRole)
	tenant.DELETE("/roles/:role_id", r.tenantHandler.DeleteTenantRole)
	tenant.GET("/roles/:role_id/permissions", r.tenantHandler.GetTenantRolePermissions)
	tenant.PUT("/roles/:role_id/permissions", r.tenantHandler.SetTenantRolePermissions)
	tenant.GET("/role-resources", r.tenantHandler.ListTenantRoleResources)
	tenant.GET("/default-role", r.tenantHandler.GetTenantDefaultRole)
	tenant.PUT("/default-role", r.tenantHandler.SetTenantDefaultRole)
	tenant.GET("/management-permissions", r.tenantHandler.ListTenantManagementPermissions)
	tenant.PUT("/management-permissions", r.tenantHandler.SetTenantManagementPermissions)
	tenant.GET("/members/:user_id/role", r.tenantHandler.GetTenantMemberRole)
	tenant.PUT("/members/:user_id/role", r.tenantHandler.SetTenantMemberRole)
	tenant.GET("/me/permissions", r.tenantHandler.GetTenantMyPermissions)
	tenant.GET("/permission-logs", r.tenantHandler.ListTenantPermissionLogs)
}

func ToggleWorkflow() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()
	}
}

func ToggleChat() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()
	}
}

func ToggleFile() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()
	}
}

func ToggleSearch() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()
	}
}

func ToggleModel() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()
	}
}

func ToggleUserInTenant() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()
	}
}

func ToggleGroup() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()
	}
}

func ToggleEvaluation() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()
	}
}

func ToggleDataset() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()
	}
}

func ToggleMemory() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()
	}
}
