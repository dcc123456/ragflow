//go:build ee

package handler

import (
	"encoding/json/v2"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"ragflow/internal/common"
	"ragflow/internal/entity"
	"ragflow/internal/service"
)

func TestSettingsAccountAudit(t *testing.T) {
	user := &UserHandler{}
	system := &SystemHandler{}
	billing := &BillingHandler{}
	for _, test := range []struct {
		method, route, action string
		handler               gin.HandlerFunc
	}{
		{"PATCH", "/api/v1/users/me", "update_user_settings", user.Setting},
		{"PATCH", "/api/v1/users/me/models", "set_tenant_info", user.SetTenantInfo},
		{"POST", "/api/v1/auth/logout", "logout", user.Logout},
		{"POST", "/api/v1/system/keys", "create_api_key", system.CreateKey},
		{"DELETE", "/api/v1/system/keys/:key", "delete_api_key", system.DeleteKey},
		{"POST", "/api/v1/system/tokens", "create_api_key", system.CreateKey},
		{"DELETE", "/api/v1/system/tokens/:key", "delete_api_key", system.DeleteKey},
		{"PUT", "/api/v1/system/config/log", "set_system_log_level", system.SetLogLevel},
		{"PUT", "/api/v1/system/cores", "set_system_cores", system.SetCores},
		{"PUT", "/api/v1/system/memory", "set_system_memory", system.SetMemory},
		{"PUT", "/api/v1/system/concurrency", "set_system_concurrency", system.SetConcurrency},
		{"POST", "/api/v1/billing/subscription", "request_billing_subscription_checkout", billing.CheckoutSubscription},
		{"PATCH", "/api/v1/billing/subscription", "request_billing_subscription_checkout", billing.CheckoutSubscription},
		{"POST", "/api/v1/billing/addon-purchases", "request_billing_addon_checkout", billing.AddonCheckout},
		{"PATCH", "/api/v1/billing/storage", "update_billing_storage_target", billing.SetStorageTarget},
		{"POST", "/api/v1/billing/setup-intents", "request_billing_setup_intent", billing.SetupIntent},
		{"POST", "/api/v1/billing/portal-sessions", "request_billing_portal_session", billing.PortalSession},
		{"POST", "/api/v1/billing/points/checkout", "request_billing_points_checkout", billing.PointsCheckout},
	} {
		t.Run(test.method+test.route, func(t *testing.T) {
			for _, authenticated := range []bool{true, false} {
				var events []*common.OperationLog
				common.SetAuditLogSink(func(event *common.OperationLog) error { events = append(events, event); return nil })
				t.Cleanup(func() { common.SetAuditLogSink(nil) })
				router := gin.New()
				router.Handle(test.method, test.route, func(c *gin.Context) {
					if authenticated {
						c.Set("user_id", "actor")
						c.Set("tenant_id", "tenant")
					}
					// Stop at identity validation for account/key handlers; no database side effects.
					c.Set("user", "invalid")
					if strings.Contains(test.route, "/users/me") {
						c.Set("user", &entity.User{ID: "actor"})
					}
					test.handler(c)
				})
				request := httptest.NewRequest(test.method, strings.ReplaceAll(test.route, ":key", "secret-credential"), strings.NewReader(`{"password":"secret-payload",`))
				request.Header.Set("Content-Type", "application/json")
				router.ServeHTTP(httptest.NewRecorder(), request)
				if !authenticated {
					if len(events) != 0 {
						t.Fatalf("anonymous event: %+v", events)
					}
					continue
				}
				if len(events) != 1 {
					t.Fatalf("got %d events", len(events))
				}
				event := events[0]
				if event.Operation != test.action || event.APIPath != test.route || event.HTTPMethod != test.method || event.UserID != "actor" || event.Resources["tenant_id"] != "tenant" || event.Resources["outcome"] != "response_error" {
					t.Fatalf("wrong event: %+v", event)
				}
				encoded, err := json.Marshal(event)
				if err != nil || strings.Contains(string(encoded), "secret-") || event.RequestParams != "" || event.Error != nil {
					t.Fatalf("unsafe event: %s %v", encoded, err)
				}
			}
		})
	}
}

func TestSettingsProfileAuditSuccess(t *testing.T) {
	fixture, _, db := setupGinContextWithUserAndDB(t, "PATCH", "/")
	user := &entity.User{ID: "user-1", Email: "audit@example.test"}
	if err := db.Take(user, "id = ?", user.ID).Error; err != nil {
		t.Fatal(err)
	}
	var events []*common.OperationLog
	common.SetAuditLogSink(func(event *common.OperationLog) error { events = append(events, event); return nil })
	t.Cleanup(func() { common.SetAuditLogSink(nil) })
	h := NewUserHandler(service.NewUserService(), nil)
	router := gin.New()
	router.PATCH("/api/v1/users/me", func(c *gin.Context) {
		c.Set("user", user)
		c.Set("user_id", user.ID)
		c.Set("tenant_id", "user-1")
		h.Setting(c)
	})
	request := httptest.NewRequest("PATCH", "/api/v1/users/me", strings.NewReader(`{"nickname":"private-profile"}`)).WithContext(fixture.Request.Context())
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(httptest.NewRecorder(), request)
	if len(events) != 1 || events[0].Resources["outcome"] != "response_success" {
		t.Fatalf("events: %+v", events)
	}
	var saved entity.User
	if err := db.Take(&saved, "id = ?", user.ID).Error; err != nil || saved.Nickname != "private-profile" {
		t.Fatalf("profile not saved: %v", err)
	}
	encoded, _ := json.Marshal(events[0])
	if strings.Contains(string(encoded), "private-profile") {
		t.Fatalf("profile leaked: %s", encoded)
	}
}
