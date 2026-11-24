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

package admin

import (
	"ragflow/internal/observability"
	"ragflow/internal/server"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
)

func SetupEERouter(engine *gin.Engine) {
	serverName := server.GetServerName()
	engine.Use(otelgin.Middleware(serverName))
	engine.GET("/metrics", gin.WrapH(observability.MetricsHandler()))
}

func RegisterEERouter(protected *gin.RouterGroup, r *Router) {
	protected.POST("/validate_mail", r.handler.validateMail)
	protected.PUT("/me/password", r.handler.changeOwnPassword)

	// Tenant administration and global permission audit
	protected.GET("/tenants", r.handler.ListTenants)
	protected.PUT("/tenants/:tenant_id/role", r.handler.UpdateTenantRole)
	protected.PUT("/tenants/:tenant_id/status", r.handler.UpdateTenantStatus)
	protected.GET("/permission-logs", r.handler.ListPermissionLogs)
	protected.GET("/settings/default-tenant-role", r.handler.ShowDefaultTenantSystemRole)
	protected.PUT("/settings/default-tenant-role", r.handler.UpdateDefaultTenantSystemRole)

	// Role management
	protected.GET("/roles", r.handler.ListRoles)
	protected.POST("/roles", r.handler.CreateRole)
	protected.GET("/roles/:role_name", r.handler.ShowRole)
	protected.PUT("/roles/:role_name", r.handler.UpdateRole)
	protected.DELETE("/roles/:role_name", r.handler.DropRole)
	protected.GET("/roles/:role_name/permission", r.handler.ShowRolePermission)
	protected.POST("/roles/:role_name/permission", r.handler.GrantRolePermission)
	protected.DELETE("/roles/:role_name/permission", r.handler.RevokeRolePermission)
	protected.GET("/roles/resource", r.handler.ListResources)
	protected.GET("/roles/permission", r.handler.ListRolesWithPermission)
	protected.GET("/roles/:role_name/default-models", r.handler.ShowRoleDefaultModels)
	protected.PATCH("/roles/:role_name/default-models", r.handler.SetRoleDefaultModel)
	protected.DELETE("/roles/:role_name/default-models", r.handler.ResetRoleDefaultModel)

	// Providers and models
	protected.GET("/models", r.handler.ListSystemAddedModels)
	provider := protected.Group("/providers")
	{
		provider.GET("", r.handler.ListModelProviders)
		provider.PUT("", r.handler.AddModelProvider)
		provider.GET("/:provider_name", r.handler.ShowProvider)
		provider.DELETE("/:provider_name", r.handler.DeleteModelProvider)
		provider.GET("/:provider_name/models", r.handler.ListModels)
		provider.GET("/:provider_name/models/:model_name", r.handler.ShowProviderModel)

		provider.POST("/:provider_name/instances", r.handler.AddModelInstance)
		provider.GET("/:provider_name/instances", r.handler.ListModelInstances)
		provider.DELETE("/:provider_name/instances", r.handler.DeleteModelInstance)
		provider.GET("/:provider_name/instances/:instance_name", r.handler.ShowProviderInstance)
		provider.GET("/:provider_name/instances/:instance_name/balance", r.handler.ShowProviderInstanceBalance)
		provider.GET("/:provider_name/instances/:instance_name/connection", r.handler.CheckInstanceConnection)
		provider.POST("/:provider_name/connection", r.handler.CheckProviderConnection)
		provider.PUT("/:provider_name/instances/:instance_name", r.handler.AlterProviderInstance)

		provider.GET("/:provider_name/instances/:instance_name/models", r.handler.ListInstanceModels)
		provider.PATCH("/:provider_name/instances/:instance_name/models/*model_name", r.handler.AlterModel)
		provider.POST("/:provider_name/instances/:instance_name/models", r.handler.AddModel)
		provider.DELETE("/:provider_name/instances/:instance_name/models", r.handler.DeleteModels)
	}

	// License
	protected.GET("/system/fingerprint", r.handler.GetSystemFingerprint)
	protected.POST("/system/license", r.handler.SetSystemLicense)
	protected.GET("/system/license", r.handler.ShowSystemLicense)
	protected.PUT("/system/license/config", r.handler.UpdateSystemLicenseConfig)

	protected.POST("/system/soft-fingerprint", r.handler.SetSoftFingerprint)
	protected.GET("/system/soft-fingerprint", r.handler.ShowSoftFingerprint)
	protected.DELETE("/system/soft-fingerprint", r.handler.DeleteSoftFingerprint)

	// Features
	protected.GET("/system/features", r.handler.ListFeatures)

	protected.GET("/system/deployment-type", r.handler.GetDeploymentType)

	// Token statistics
	protected.GET("/stats/token", r.handler.GetTokenStats)
	protected.GET("/stats/token/users", r.handler.GetTokenUsersStats)
	protected.GET("/stats/token/summary", r.handler.GetTokenStatsSummary)

	// Logs
	protected.GET("/logs", r.handler.ListLogs)

	// Stats data info
	protected.GET("/users/:username/activity", r.handler.ShowUserActivity)
	protected.GET("/users/:username/dataset", r.handler.ShowUserDatasetSummary)
	protected.GET("/users/:username/summary", r.handler.ShowUserSummary)
	protected.GET("/users/:username/storage", r.handler.ShowUserStorage)
	protected.GET("/users/:username/quota", r.handler.ShowUserQuota)
	protected.GET("/users/:username/index", r.handler.ShowUserIndex)
	protected.GET("/users/:username/permission", r.handler.ShowUserPermission)
	protected.GET("/users/:username/datasets", r.handler.ListUserDatasets)
	protected.GET("/users/:username/agents", r.handler.ListUserAgents)
	protected.GET("/users/:username/chats", r.handler.ListUserChats)
	protected.GET("/users/:username/searches", r.handler.ListUserSearches)
	protected.GET("/users/:username/models", r.handler.ListUserModels)
	protected.GET("/users/:username/files", r.handler.ListUserFiles)
	protected.GET("/users/:username/providers", r.handler.ListUserProviders)
	protected.GET("/users/:username/providers/:provider_name/instances", r.handler.ListUserProviderInstances)
	protected.GET("/users/:username/providers/:provider_name/instances/:instance_name/models", r.handler.ListUserProviderInstanceModels)
	protected.GET("/users/:username/default-models", r.handler.ListUserDefaultModels)
	protected.GET("/users/summary", r.handler.ShowUsersSummary)
	protected.GET("/users/activity", r.handler.ShowUsersActivity)
	protected.GET("/users/reports", r.handler.ListUsersReports)
	protected.GET("/users/storage", r.handler.ListUsersStorage)
	protected.GET("/users/documents", r.handler.ListUsersDocuments)
	protected.GET("/users/index", r.handler.ListUsersIndex)
	protected.GET("/users/quota", r.handler.ListUsersQuota)
	protected.GET("/users/plan/summary", r.handler.ShowUsersPlanSummary)
	protected.GET("/users/plan", r.handler.ShowUsersPlan)
	protected.GET("/users/quota/summary", r.handler.ShowUsersQuotaSummary)
	protected.DELETE("/ingestion/tasks", r.handler.RemoveIngestionTasks)
	protected.PUT("/ingestion/tasks", r.handler.StopIngestionTasks)
	protected.GET("/ingestion/tasks", r.handler.ListIngestionTasks)
	protected.GET("/ingestion/tasks/summary", r.handler.ShowIngestionTasksSummary)
	protected.POST("/ingestion/tasks/:task_id/promote", r.handler.PromoteIngestionTask)
	protected.GET("/ingestion/logs/:log_id", r.handler.GetIngestionLog)
	protected.GET("/ingestion/logs/:log_id/messages", r.handler.GetIngestionMessages)
	protected.GET("/data/summary", r.handler.ShowDataSummary)
	protected.GET("/data/orphan", r.handler.ShowDataOrphan)
	protected.GET("/data/storage", r.handler.ShowDataStorage)
	protected.GET("/data/index", r.handler.ShowDataIndex)
	protected.DELETE("/data/orphan", r.handler.PurgeOrphanData)
	protected.DELETE("/users/:username/data", r.handler.PurgeUserData)
	protected.DELETE("/users/data", r.handler.PurgeUsersData)

	// API Keys
	protected.POST("/users/:username/keys", r.handler.GenerateUserAPIKey)
	protected.DELETE("/users/:username/keys/:key", r.handler.DeleteUserAPIKey)
	protected.GET("/users/:username/keys", r.handler.ListUserAPIKeys)

	protected.GET("/users/:username/tokens", r.handler.ListUserAPITokens)
	//protected.POST("/users/:username/keys", r.handler.GenerateUserAPIToken)
	protected.POST("/users/:username/tokens", r.handler.GenerateUserAPIToken)
	protected.DELETE("/users/:username/tokens/:token", r.handler.DeleteUserAPIToken)

	// Sensitive words, EE
	protected.GET("/sensitive-words-file", r.handler.DownloadSensitiveWords)
	protected.POST("/sensitive-words-file", r.handler.UploadSensitiveWords)

	// Verification email, EE
	protected.POST("/email/verification", r.handler.BindVerificationEmail)
	protected.GET("/email/verification", r.handler.ShowVerificationEmail)

	// White list, EE
	protected.GET("/whitelist", r.handler.ShowWhiteList)
	protected.POST("/whitelist", r.handler.AddWhiteList)
	protected.POST("/whitelist/batch", r.handler.BatchAddWhiteList)
	protected.PUT("/whitelist/:id", r.handler.UpdateWhiteList)
	protected.DELETE("/whitelist/:id", r.handler.DeleteWhiteList)
	protected.DELETE("/whitelist/batch", r.handler.BatchDeleteWhiteList)

}
