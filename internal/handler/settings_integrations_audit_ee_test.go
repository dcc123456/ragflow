//go:build ee

package handler

import (
	"context"
	"encoding/json/v2"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"ragflow/internal/common"
	"ragflow/internal/entity"
	"ragflow/internal/permission"
	"ragflow/internal/service"

	"github.com/gin-gonic/gin"
)

func TestSettingsIntegrationsAuditValidation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	providers := &ProviderHandler{}
	connectors := &ConnectorHandler{connectorService: fakeConnectorService{err: errors.New("private-service-error"), code: common.CodeDataError}}
	mcp := &MCPHandler{}
	channels := &ChatChannelHandler{}
	langfuse := NewLangfuseHandler(fakeLangfuseService{deleteFn: func(context.Context) (bool, common.ErrorCode, string, error) {
		return false, common.CodeDataError, "private-error", errors.New("private-error")
	}})
	groups := &CompilationTemplateGroupHandler{}
	for _, test := range []struct {
		name, method, route, url, action string
		handle                           gin.HandlerFunc
		resource, id                     string
	}{
		{"provider create", "PUT", "/providers", "/providers", "create_model_provider", providers.AddProvider, "", ""},
		{"provider delete missing selector", "DELETE", "/providers", "/providers", "delete_model_provider", providers.DeleteProvider, "", ""},
		{"instance create", "POST", "/providers/:provider_id_or_name/instances", "/providers/provider-one/instances", "create_provider_instance", providers.CreateProviderInstance, "provider_id_or_name", "provider-one"},
		{"instance update", "PUT", "/providers/:provider_id_or_name/instances/:instance_id_or_name", "/providers/provider-one/instances/instance-one", "update_provider_instance", providers.AlterProviderInstance, "instance_id_or_name", "instance-one"},
		{"instance delete", "DELETE", "/providers/:provider_id_or_name/instances", "/providers/provider-one/instances", "delete_provider_instances", providers.DropProviderInstance, "provider_id_or_name", "provider-one"},
		{"provider connection", "POST", "/providers/:provider_id_or_name/connection", "/providers/provider-one/connection", "test_provider_connection", providers.CheckConnection, "provider_id_or_name", "provider-one"},
		{"saved connection identity rejection", "GET", "/providers/:provider_id_or_name/instances/:instance_id_or_name/connection", "/providers/provider-one/instances/instance-one/connection", "test_provider_connection", providers.CheckInstanceConnection, "instance_id_or_name", "instance-one"},
		{"saved connection missing selector", "GET", "/connection", "/connection", "test_provider_connection", providers.CheckInstanceConnection, "", ""},
		{"model create", "POST", "/providers/:provider_id_or_name/instances/:instance_id_or_name/models", "/providers/provider-one/instances/instance-one/models", "add_provider_model", providers.AddModel, "instance_id_or_name", "instance-one"},
		{"model update", "PATCH", "/providers/:provider_id_or_name/instances/:instance_id_or_name/models/*model_name", "/providers/provider-one/instances/instance-one/models/model-one", "update_provider_model", providers.AlterModel, "instance_id_or_name", "instance-one"},
		{"model delete", "DELETE", "/providers/:provider_id_or_name/instances/:instance_id_or_name/models", "/providers/provider-one/instances/instance-one/models", "delete_provider_models", providers.DropInstanceModels, "instance_id_or_name", "instance-one"},
		{"model group delete", "DELETE", "/models/groups/:group_id", "/models/groups/group-one", "delete_model_group", providers.DeleteModelGroup, "group_id", "group-one"},
		{"mcp delete", "DELETE", "/mcp/servers/:mcp_id", "/mcp/servers/mcp-one", "delete_mcp_server", mcp.DeleteMCPServer, "mcp_id", "mcp-one"},
		{"model group create", "POST", "/models/groups", "/models/groups", "create_model_group", providers.CreateModelGroup, "", ""},
		{"model group add", "POST", "/models/groups/:group_id/model", "/models/groups/group-one/model", "add_model_to_group", providers.AddModelToGroup, "group_id", "group-one"},
		{"model group remove", "DELETE", "/models/groups/:group_id/model", "/models/groups/group-one/model", "remove_model_from_group", providers.RemoveModelFromGroup, "group_id", "group-one"},
		{"connector create", "POST", "/connectors", "/connectors", "create_connector", connectors.CreateConnector, "", ""},
		{"connector update", "PATCH", "/connectors/:connector_id", "/connectors/connector-one", "update_connector", connectors.UpdateConnector, "connector_id", "connector-one"},
		{"connector delete", "DELETE", "/connectors/:connector_id", "/connectors/connector-one", "delete_connector", connectors.DeleteConnector, "connector_id", "connector-one"},
		{"connector rebuild", "POST", "/connectors/:connector_id/rebuild", "/connectors/connector-one/rebuild", "request_connector_rebuild", connectors.RebuildConnector, "connector_id", "connector-one"},
		{"connector resume", "POST", "/connectors/:connector_id/resume-failed-sync", "/connectors/connector-one/resume-failed-sync", "request_connector_sync_resume", connectors.ResumeFailedSync, "connector_id", "connector-one"},
		{"connector test", "POST", "/connectors/:connector_id/test", "/connectors/connector-one/test", "test_connector", connectors.TestConnector, "connector_id", "connector-one"},
		{"google oauth start", "POST", "/connectors/google/oauth/web/start", "/connectors/google/oauth/web/start", "request_connector_oauth", connectors.StartGoogleWebOAuth, "", ""},
		{"box oauth start", "POST", "/connectors/box/oauth/web/start", "/connectors/box/oauth/web/start", "request_connector_oauth", connectors.StartBoxWebOAuth, "", ""},
		{"mcp create", "POST", "/mcp/servers", "/mcp/servers", "create_mcp_server", mcp.CreateMCPServer, "", ""},
		{"mcp update", "PUT", "/mcp/servers/:mcp_id", "/mcp/servers/mcp-one", "update_mcp_server", mcp.UpdateMCPServer, "mcp_id", "mcp-one"},
		{"mcp import", "POST", "/mcp/servers/import", "/mcp/servers/import", "import_mcp_servers", mcp.ImportMCPServers, "", ""},
		{"mcp test", "POST", "/mcp/servers/:mcp_id/test", "/mcp/servers/mcp-one/test", "test_mcp_server", mcp.TestMCPServer, "mcp_id", "mcp-one"},
		{"channel create", "POST", "/chat-channels", "/chat-channels", "create_chat_channel", channels.CreateChatChannel, "", ""},
		{"channel update", "PATCH", "/chat-channels/:channel_id", "/chat-channels/channel-one", "update_chat_channel", channels.UpdateChatChannel, "channel_id", "channel-one"},
		{"channel delete missing selector", "DELETE", "/chat-channels", "/chat-channels", "delete_chat_channel", channels.DeleteChatChannel, "", ""},
		{"langfuse post", "POST", "/langfuse/api-key", "/langfuse/api-key", "set_langfuse_api_key", langfuse.SetAPIKey, "", ""},
		{"langfuse put", "PUT", "/langfuse/api-key", "/langfuse/api-key", "set_langfuse_api_key", langfuse.SetAPIKey, "", ""},
		{"langfuse delete", "DELETE", "/langfuse/api-key", "/langfuse/api-key", "delete_langfuse_api_key", langfuse.DeleteAPIKey, "", ""},
		{"template group create", "POST", "/compilation-template-groups", "/compilation-template-groups", "create_compilation_template_group", groups.Save, "", ""},
		{"template group update", "PUT", "/compilation-template-groups/:group_id", "/compilation-template-groups/group-one", "update_compilation_template_group", groups.Update, "compilation_template_group_id", "group-one"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var events []*common.OperationLog
			common.SetAuditLogSink(func(event *common.OperationLog) error { events = append(events, event); return nil })
			t.Cleanup(func() { common.SetAuditLogSink(nil) })
			router := gin.New()
			router.Handle(test.method, "/api/v1"+test.route, func(c *gin.Context) {
				c.Set("user_id", "actor")
				c.Set("tenant_id", "tenant")
				c.Set("auth_via_api_token", true)
				test.handle(c)
			})
			request := httptest.NewRequest(test.method, "/api/v1"+test.url+"?api_key=private-query&config=private-config", strings.NewReader(`{"private-body":`))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if len(events) != 1 {
				t.Fatalf("events=%d response=%s", len(events), response.Body.String())
			}
			event := events[0]
			if event.APIPath != "/api/v1"+test.route || event.Operation != test.action || event.UserID != "actor" || event.Resources["tenant_id"] != "tenant" || event.Resources["outcome"] != "response_error" || event.Code == 0 {
				t.Fatalf("event=%+v", event)
			}
			if test.resource != "" && event.Resources[test.resource] != test.id {
				t.Fatalf("missing selector: %+v", event)
			}
			encoded, err := json.Marshal(event)
			if err != nil || strings.Contains(string(encoded), "private-") || event.RequestParams != "" || event.Error != nil {
				t.Fatalf("unsafe event=%s err=%v", encoded, err)
			}
		})
	}
}

func TestSettingsIntegrationsAuditSuccessAndExclusions(t *testing.T) {
	gin.SetMode(gin.TestMode)
	connector := &ConnectorHandler{connectorService: fakeConnectorService{connector: &entity.Connector{ID: "created-connector"}}}
	langfuse := NewLangfuseHandler(fakeLangfuseService{setFn: func(_ context.Context, secret, public, host string) (*entity.TenantLangfuse, common.ErrorCode, error) {
		return &entity.TenantLangfuse{SecretKey: secret, PublicKey: public, Host: host}, common.CodeSuccess, nil
	}, getFn: func(context.Context) (*entity.LangfuseInfoResponse, common.ErrorCode, string, error) {
		return nil, common.CodeSuccess, "success", nil
	}})
	for _, test := range []struct {
		name, method, body, action, resource, id string
		handle                                   gin.HandlerFunc
	}{
		{"create connector", "POST", `{"name":"private-name","source":"private-source","config":{"password":"private-password"}}`, "create_connector", "connector_id", "created-connector", connector.CreateConnector},
		{"set langfuse", "POST", `{"secret_key":"private-secret","public_key":"private-public","host":"private-host"}`, "set_langfuse_api_key", "", "", langfuse.SetAPIKey},
		{"read langfuse", "GET", "", "", "", "", langfuse.GetAPIKey},
		{"list connectors", "GET", "", "", "", "", connector.ListConnectors},
		{"google poll", "POST", "{}", "", "", "", connector.PollGoogleWebOAuthResult},
		{"box poll", "POST", "{}", "", "", "", connector.PollBoxWebOAuthResult},
		{"google callback", "GET", "", "", "", "", connector.GoogleDriveWebOAuthCallback},
		{"gmail callback", "GET", "", "", "", "", connector.GmailWebOAuthCallback},
		{"box callback", "GET", "", "", "", "", connector.BoxWebOAuthCallback},
		{"provider read", "GET", "", "", "", "", (&ProviderHandler{}).ShowProvider},
		{"model execution", "POST", "{", "", "", "", (&ProviderHandler{}).ChatToModel},
		{"mcp list", "GET", "", "", "", "", (&MCPHandler{}).ListMCPServers},
		{"channel read", "GET", "", "", "", "", (&ChatChannelHandler{}).GetChatChannel},
		{"channel runtime", "GET", "", "", "", "", (&ChatChannelHandler{}).GetChatChannelRuntime},
	} {
		t.Run(test.name, func(t *testing.T) {
			var events []*common.OperationLog
			common.SetAuditLogSink(func(event *common.OperationLog) error { events = append(events, event); return nil })
			t.Cleanup(func() { common.SetAuditLogSink(nil) })
			router := gin.New()
			router.Handle(test.method, "/action", func(c *gin.Context) { c.Set("user_id", "actor"); c.Set("tenant_id", "tenant"); test.handle(c) })
			request := httptest.NewRequest(test.method, "/action?page=bad&state=private-state&code=private-code", strings.NewReader(test.body))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if test.action == "" {
				if len(events) != 0 {
					t.Fatalf("excluded route emitted %d events", len(events))
				}
				return
			}
			if len(events) != 1 || events[0].Operation != test.action || events[0].Resources["outcome"] != "response_success" {
				t.Fatalf("events=%+v response=%s", events, response.Body.String())
			}
			if test.resource != "" && events[0].Resources[test.resource] != test.id {
				t.Fatalf("missing created ID: %+v", events[0])
			}
			encoded, err := json.Marshal(events[0])
			if err != nil || strings.Contains(string(encoded), "private-") {
				t.Fatalf("unsafe event=%s err=%v", encoded, err)
			}
		})
	}
}

func TestSettingsTemplateGroupCreationAudit(t *testing.T) {
	setupGroupHandlerDB(t)
	handler := NewCompilationTemplateGroupHandler(service.NewCompilationTemplateGroupService())
	var events []*common.OperationLog
	common.SetAuditLogSink(func(event *common.OperationLog) error { events = append(events, event); return nil })
	t.Cleanup(func() { common.SetAuditLogSink(nil) })
	router := gin.New()
	router.POST("/api/v1/compilation-template-groups", func(c *gin.Context) {
		c.Set("user_id", "actor")
		c.Set("tenant_id", "actor")
		c.Request = c.Request.WithContext(permission.WithIdentity(c.Request.Context(), "actor", "actor"))
		handler.Save(c)
	})
	response := postCompilationTemplateGroup(t, router, `{"name":"private-group","templates":[{"name":"private-template","kind":"text","config":{}}]}`)
	if response.Code != int(common.CodeSuccess) || len(events) != 1 {
		t.Fatalf("response=%+v events=%+v", response, events)
	}
	if events[0].Resources["compilation_template_group_id"] != response.Data["id"] || events[0].Resources["outcome"] != "response_success" {
		t.Fatalf("event=%+v response=%+v", events[0], response)
	}
	encoded, err := json.Marshal(events[0])
	if err != nil || strings.Contains(string(encoded), "private-") {
		t.Fatalf("unsafe event=%s err=%v", encoded, err)
	}
}

func TestSettingsIntegrationParsedSelectorsAudit(t *testing.T) {
	connector := &ConnectorHandler{connectorService: fakeConnectorService{code: common.CodeDataError, err: errors.New("private-error")}}
	for _, test := range []struct {
		name, body string
		handle     gin.HandlerFunc
		expected   map[string]any
	}{
		{"resume", `{"kb_id":"dataset-one","task_id":"task-one","config":"private-config"}`, connector.ResumeFailedSync, map[string]any{"dataset_id": "dataset-one", "task_id": "task-one"}},
		{"rebuild", `{"kb_id":"dataset-one","config":"private-config"}`, connector.RebuildConnector, map[string]any{"dataset_id": "dataset-one"}},
		{"canonical model", `{"model_id":" model-one ","status":"private-invalid","extra":{"password":"private-password"}}`, (&ProviderHandler{}).AlterModel, map[string]any{"model_name": "model/one", "model_id": "model-one"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			var events []*common.OperationLog
			common.SetAuditLogSink(func(event *common.OperationLog) error { events = append(events, event); return nil })
			t.Cleanup(func() { common.SetAuditLogSink(nil) })
			router := gin.New()
			router.POST("/action", func(c *gin.Context) {
				c.Set("user_id", "actor")
				c.Set("tenant_id", "tenant")
				c.Params = gin.Params{{Key: "connector_id", Value: "connector-one"}, {Key: "provider_id_or_name", Value: "provider-one"}, {Key: "instance_id_or_name", Value: "instance-one"}, {Key: "model_name", Value: "/model/one "}}
				test.handle(c)
			})
			request := httptest.NewRequest("POST", "/action", strings.NewReader(test.body))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if len(events) != 1 {
				t.Fatalf("events=%d response=%s", len(events), response.Body.String())
			}
			for key, want := range test.expected {
				if events[0].Resources[key] != want {
					t.Fatalf("%s=%v want %v", key, events[0].Resources[key], want)
				}
			}
			encoded, err := json.Marshal(events[0])
			if err != nil || strings.Contains(string(encoded), "private-") {
				t.Fatalf("unsafe event=%s err=%v", encoded, err)
			}
		})
	}
}
