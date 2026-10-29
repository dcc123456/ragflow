//
//  Copyright 2026 The InfiniFlow Authors. All Rights Reserved.
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.
//

//go:build ee

package router

import (
	"ragflow/internal/server"
	"ragflow/internal/utility"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
)

func SetupEERouter(engine *gin.Engine) {
	serverName := server.GetServerName()
	engine.Use(otelgin.Middleware(serverName))
	engine.Use(utility.GetRateLimit().Middleware())
}

func RegisterEEAuthRouter(_ *gin.RouterGroup, _ *Router) {}

func RegisterEENoAuthRouter(_ *gin.Engine, apiNoAuth *gin.RouterGroup, r *Router) {
	// For EE
	apiNoAuth.POST("/auth/register/captcha", r.userHandler.Captcha)
	apiNoAuth.POST("/auth/register/otp", r.userHandler.SendOTP)
	apiNoAuth.POST("/auth/register/otp/verify", r.userHandler.VerifyOTP)
}

func RegisterEEProtectedRouter(authorized *gin.RouterGroup, r *Router) {
	authorized.POST("/api/v1/tenants/:tenant_id/switch", r.tenantHandler.SwitchTenant)

	// Tenant-scoped role configuration uses the Tenant carried by the current token.
	tenant := authorized.Group("/api/v1/tenant")
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
