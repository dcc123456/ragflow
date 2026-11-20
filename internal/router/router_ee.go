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
	"ragflow/internal/common"
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
	engine.Use(handler.RegistrationWhitelist())
	engine.Use(handler.RegistrationVerification())
}

func RegisterEENoAuthRouter(engine *gin.Engine, apiNoAuth *gin.RouterGroup, r *Router) {
	engine.GET("/metrics", gin.WrapH(observability.MetricsHandler()))
	// Banner and feature-exposure settings the web client reads before login.
	apiNoAuth.GET("/notification", r.systemHandler.Notification)
	apiNoAuth.GET("/expose-model-provider", r.systemHandler.ExposeModelProvider)
	apiNoAuth.GET("/expose-api", r.systemHandler.ExposeAPI)
	apiNoAuth.GET("/system/features", r.systemHandler.ListFeatures)
	apiNoAuth.GET("/system/license", r.systemHandler.CheckLicense)

	// For EE
	apiNoAuth.GET("/auth/login/channels", r.userHandler.GetLoginChannels)
	apiNoAuth.GET("/auth/login/:channel", r.userHandler.OAuthLogin)
	// JSON initiate for the embedded QR login flow (e.g. the Feishu SDK):
	// returns the authorization parameters instead of redirecting, so the
	// login page can render the provider's QR code on the card itself.
	apiNoAuth.GET("/auth/login/:channel/initiate", r.userHandler.OAuthLoginInitiate)
	apiNoAuth.GET("/auth/cas/callback", r.userHandler.CASCallback)
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
	authorized.DELETE("/api/v1/users/me", r.userHandler.DeleteAccount)

	// Ingestion monitoring data endpoints (read-only, data/display decoupled).
	ingestionMonitorHandler := handler.NewIngestMonitorHandler()
	ingestMonitor := authorized.Group("/api/v1/admin/ingestion")
	ingestMonitor.GET("/overview", ingestionMonitorHandler.Overview)
	ingestMonitor.GET("/timeseries", ingestionMonitorHandler.Timeseries)
	ingestMonitor.GET("/filters", ingestionMonitorHandler.Filters)
	ingestMonitor.GET("/breakdown", ingestionMonitorHandler.Breakdown)
	ingestMonitor.GET("/fail-rate", ingestionMonitorHandler.FailRate)
	ingestMonitor.GET("/running/tasks", ingestionMonitorHandler.RunningTasks)
	ingestMonitor.GET("/running/overview", ingestionMonitorHandler.RunningOverview)
	ingestMonitor.GET("/events", ingestionMonitorHandler.Events)
	ingestMonitor.GET("/timeline", ingestionMonitorHandler.Timeline)

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
	evaluation := handler.NewEvaluationHandler()
	evaluations := authorized.Group("/api/v1/evaluations", ToggleEvaluation())
	evaluations.GET("/collections", evaluation.Collections)
	evaluations.POST("/collections", evaluation.CreateCollection)
	evaluations.DELETE("/collections", evaluation.DeleteCollection)
	evaluations.GET("/collections/:collection_id", evaluation.Collection)
	evaluations.PATCH("/collections/:collection_id", evaluation.UpdateCollection)
	evaluations.DELETE("/collections/:collection_id", evaluation.DeleteCollection)
	evaluations.GET("/collections/:collection_id/cases", evaluation.Cases)
	evaluations.POST("/collections/:collection_id/cases", evaluation.CreateCase)
	evaluations.PATCH("/collections/:collection_id/cases/:case_id", evaluation.UpdateCase)
	evaluations.DELETE("/collections/:collection_id/cases/:case_id", evaluation.DeleteCase)
	evaluations.POST("/collections/:collection_id/imports", evaluation.ImportCases)
	evaluations.GET("/collections/:collection_id/export", evaluation.ExportCollection)
	evaluations.GET("/runs", evaluation.Runs)
	evaluations.POST("/runs", evaluation.CreateRun)
	evaluations.GET("/runs/:run_id", evaluation.Run)
	evaluations.PATCH("/runs/:run_id", evaluation.UpdateRun)
	evaluations.DELETE("/runs/:run_id", evaluation.DeleteRun)
	evaluations.POST("/runs/:run_id/executions", evaluation.StartExecution)
	evaluations.GET("/runs/:run_id/results", evaluation.Results)
	evaluations.DELETE("/runs/:run_id/results/:case_id", evaluation.ClearResult)
	evaluations.DELETE("/runs/:run_id/results/:case_id/answer", evaluation.ClearResult)
	evaluations.DELETE("/runs/:run_id/results/:case_id/metrics", evaluation.ClearResult)
	evaluations.DELETE("/runs/:run_id/results/:case_id/metrics/:metric", evaluation.ClearResult)
	evaluations.GET("/runs/:run_id/export", evaluation.ExportRun)

	// Model group membership status is enterprise-only, so its route is
	// registered here instead of next to the other group routes in router.go,
	// which the community tree also compiles.
	models := authorized.Group("/api/v1/models", ToggleModel())
	models.PATCH("/groups/:group_id/model", r.providerHandler.SetGroupModelStatus)
	models.PATCH("/groups/:group_id/order", r.providerHandler.ReorderModelGroupMembers)

	authorized.POST("/api/v1/tenants/:tenant_id/switch", r.tenantHandler.SwitchTenant)
	authorized.POST("/api/v1/tenants/:tenant_id/invitation-links", r.tenantHandler.CreateTenantInviteLink)
	authorized.GET("/api/v1/tenant-invitations/:token/status", r.tenantHandler.GetTenantInviteLinkMembership)
	authorized.POST("/api/v1/tenant-invitations/:token/request", r.tenantHandler.RequestTenantInviteLink)

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
	tenant.POST("/groups/:group_id/members", r.tenantHandler.AddGroupMember)
	tenant.DELETE("/groups/:group_id/members/:user_id", r.tenantHandler.RemoveGroupMember)
	tenant.PUT("/groups/:group_id/members/:user_id/permissions", r.tenantHandler.SetGroupMemberPermissions)
	tenant.PUT("/groups/:group_id/owner", r.tenantHandler.TransferGroupOwnership)
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
	tenant.POST("/members/:user_id/approve", r.tenantHandler.ApproveTenantMember)
	tenant.GET("/me/permissions", r.tenantHandler.GetTenantMyPermissions)
	tenant.GET("/permission-logs", r.tenantHandler.ListTenantPermissionLogs)

	// Tenant-scoped ingestion-task promotion to the high-priority lane.
	// The dataset param enforces tenant scope through the permission engine.
	datasets := authorized.Group("/api/v1/datasets/:dataset_id", ToggleDataset())
	datasets.POST("/ingestion/tasks/:task_id/promote", r.documentHandler.PromoteIngestionTask)

	// The chunk editor lists, previews and restores the states a source chunk
	// went through.
	chunkVersions := authorized.Group("/api/v1/datasets/:dataset_id/documents/:document_id/chunks/:chunk_id/versions")
	chunkVersions.GET("", r.chunkHandler.ListChunkVersions)
	chunkVersions.GET("/diff", r.chunkHandler.DiffChunkVersions)
	chunkVersions.GET("/:version", r.chunkHandler.GetChunkVersion)
	chunkVersions.POST("/:version/restore", r.chunkHandler.RestoreChunkVersion)
	chunkVersions.DELETE("/:version", r.chunkHandler.DeleteChunkVersion)
	// An old picture is no longer referenced by the live chunk, so the version
	// that recorded it is what authorizes reading its bytes.
	chunkVersions.GET("/:version/image", r.chunkHandler.GetChunkVersionImage)

	// A dataset keeps a configuration version per change, and a restore writes a
	// stored version back. The routes live with the edition that keeps history.
	datasetVersions := authorized.Group("/api/v1/datasets/:dataset_id/versions")
	datasetVersions.GET("", r.datasetsHandler.ListDatasetVersions)
	datasetVersions.GET("/:version", r.datasetsHandler.GetDatasetVersion)
	datasetVersions.POST("/:version/restore", r.datasetsHandler.RestoreDatasetVersion)
	datasetVersions.DELETE("/:version", r.datasetsHandler.DeleteDatasetVersion)
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
		if quota := utility.GetAPIServiceQuota(); !quota.Features.Evaluation {
			common.Respond(c, 403, common.CodeForbidden, nil, "Evaluation is not enabled")
			c.Abort()
			return
		}
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
