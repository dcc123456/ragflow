//go:build ee

package handler

import (
	"encoding/json/v2"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"ragflow/internal/common"
	"ragflow/internal/entity"
	"ragflow/internal/permission/testsupport"
	"ragflow/internal/service"
)

func TestSettingsTeamAuditValidationAndExclusions(t *testing.T) {
	handler := &TenantHandler{}
	for _, test := range []struct {
		name, method, path, url, body, action     string
		handler                                   gin.HandlerFunc
		missingTenant, anonymous, invalidIdentity bool
	}{
		{name: "models", method: "PATCH", path: "/api/v1/models", body: `{"model_type":`, action: "update_default_models", handler: handler.SetModels},
		{name: "default models alias", method: "PATCH", path: "/api/v1/models/default", body: `{"model_type":`, action: "update_default_models", handler: handler.SetDefaultModels},
		{name: "invite member", method: "POST", path: "/api/v1/tenants/:tenant_id/users", url: "/api/v1/tenants/tenant/users", body: `{"email":"private-email",`, action: "invite_tenant_member", handler: handler.AddTenantMember},
		{name: "remove member", method: "DELETE", path: "/api/v1/tenants/:tenant_id/users", url: "/api/v1/tenants/tenant/users", body: `{"user_id":`, action: "remove_tenant_member", handler: handler.RemoveTenantMember},
		{name: "accept invite", method: "PATCH", path: "/api/v1/tenants/:tenant_id", url: "/api/v1/tenants/target", action: "accept_tenant_invitation", handler: handler.AcceptTenantInvite, invalidIdentity: true},
		{name: "switch tenant", method: "POST", path: "/api/v1/tenants/:tenant_id/switch", url: "/api/v1/tenants/target/switch", action: "switch_tenant", handler: handler.SwitchTenant},
		{name: "create invitation link", method: "POST", path: "/api/v1/tenants/:tenant_id/invitation-links", url: "/api/v1/tenants/tenant/invitation-links", action: "create_tenant_invitation_link", handler: handler.CreateTenantInviteLink},
		{name: "request invitation", method: "POST", path: "/api/v1/tenant-invitations/:token/request", url: "/api/v1/tenant-invitations/private-invitation-token/request", action: "request_tenant_membership", handler: handler.RequestTenantInviteLink},
		{name: "approve member", method: "POST", path: "/api/v1/tenant/members/:user_id/approve", url: "/api/v1/tenant/members/user-one/approve", action: "approve_tenant_member", handler: handler.ApproveTenantMember, missingTenant: true},
		{name: "create group", method: "POST", path: "/api/v1/tenant/groups", body: `{"name":"private-content",`, action: "create_tenant_group", handler: handler.CreateGroup},
		{name: "update group", method: "PUT", path: "/api/v1/tenant/groups/:group_id", url: "/api/v1/tenant/groups/group-one", body: `{"name":`, action: "update_tenant_group", handler: handler.UpdateGroup},
		{name: "move group", method: "PUT", path: "/api/v1/tenant/groups/:group_id/parent", url: "/api/v1/tenant/groups/group-one/parent", body: `{"parent_id":`, action: "move_tenant_group", handler: handler.MoveGroup},
		{name: "delete group", method: "DELETE", path: "/api/v1/tenant/groups/:group_id", url: "/api/v1/tenant/groups/group-one", action: "delete_tenant_group", handler: handler.DeleteGroup, missingTenant: true},
		{name: "add group member", method: "POST", path: "/api/v1/tenant/groups/:group_id/members", url: "/api/v1/tenant/groups/group-one/members", body: `{"user_id":`, action: "add_tenant_group_member", handler: handler.AddGroupMember},
		{name: "remove group member", method: "DELETE", path: "/api/v1/tenant/groups/:group_id/members/:user_id", url: "/api/v1/tenant/groups/group-one/members/user-one", action: "remove_tenant_group_member", handler: handler.RemoveGroupMember, missingTenant: true},
		{name: "group member permissions", method: "PUT", path: "/api/v1/tenant/groups/:group_id/members/:user_id/permissions", url: "/api/v1/tenant/groups/group-one/members/user-one/permissions", body: `{"permissions":`, action: "update_tenant_group_member_permissions", handler: handler.SetGroupMemberPermissions},
		{name: "group owner", method: "PUT", path: "/api/v1/tenant/groups/:group_id/owner", url: "/api/v1/tenant/groups/group-one/owner", body: `{"user_id":`, action: "transfer_tenant_group_ownership", handler: handler.TransferGroupOwnership},
		{name: "create role", method: "POST", path: "/api/v1/tenant/roles", body: `{"role_name":"private-content",`, action: "create_tenant_role", handler: handler.CreateTenantRole},
		{name: "update role", method: "PATCH", path: "/api/v1/tenant/roles/:role_id", url: "/api/v1/tenant/roles/12", body: `{"role_name":`, action: "update_tenant_role", handler: handler.UpdateTenantRole},
		{name: "delete role", method: "DELETE", path: "/api/v1/tenant/roles/:role_id", url: "/api/v1/tenant/roles/invalid", action: "delete_tenant_role", handler: handler.DeleteTenantRole},
		{name: "role permissions", method: "PUT", path: "/api/v1/tenant/roles/:role_id/permissions", url: "/api/v1/tenant/roles/12/permissions", body: `{"permissions":`, action: "update_tenant_role_permissions", handler: handler.SetTenantRolePermissions},
		{name: "default role", method: "PUT", path: "/api/v1/tenant/default-role", body: `{"default_role_id":"private-content"}`, action: "update_tenant_default_role", handler: handler.SetTenantDefaultRole},
		{name: "management permissions", method: "PUT", path: "/api/v1/tenant/management-permissions", body: `{"permissions":`, action: "update_tenant_management_permissions", handler: handler.SetTenantManagementPermissions},
		{name: "member role", method: "PUT", path: "/api/v1/tenant/members/:user_id/role", url: "/api/v1/tenant/members/user-one/role", body: `{"role_id":`, action: "update_tenant_member_role", handler: handler.SetTenantMemberRole},
		{name: "resource permissions", method: "PUT", path: "/api/v1/permission/update", body: `{"resource_type":"private-content",`, action: "update_resource_permissions", handler: handler.UpdateResourcePermissions},
		{name: "resource ownership", method: "PUT", path: "/api/v1/permission/transfer_owner", body: `{"user_id":`, action: "transfer_resource_ownership", handler: handler.TransferResourceOwnership},
		{name: "read excluded", method: "GET", path: "/api/v1/tenant/roles", handler: handler.ListTenantRoles, missingTenant: true},
		{name: "permission read excluded", method: "POST", path: "/api/v1/permission/list", body: `{`, handler: handler.ListResourcePermissions},
		{name: "anonymous excluded", method: "POST", path: "/api/v1/tenant/groups", handler: handler.CreateGroup, anonymous: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			var events []*common.OperationLog
			common.SetAuditLogSink(func(event *common.OperationLog) error { events = append(events, event); return nil })
			t.Cleanup(func() { common.SetAuditLogSink(nil) })
			router := searchAuditRouter(test.handler, test.method, test.path, test.anonymous)
			if test.missingTenant {
				router = searchAuditRouter(func(c *gin.Context) { c.Set("tenant_id", ""); test.handler(c) }, test.method, test.path, false)
			}
			if test.invalidIdentity {
				router = searchAuditRouter(func(c *gin.Context) { c.Set("user", "invalid"); test.handler(c) }, test.method, test.path, false)
			}
			url := test.url
			if url == "" {
				url = test.path
			}
			writer := searchAuditRequest(router, test.method, url, test.body)
			if test.action == "" {
				if len(events) != 0 {
					t.Fatalf("excluded action emitted %d events", len(events))
				}
				return
			}
			if len(events) != 1 {
				t.Fatalf("got %d events; response %s", len(events), writer.Body.String())
			}
			event := events[0]
			if event.Operation != test.action || event.UserID != "actor" || event.Resources["outcome"] != "response_error" || event.Resources["event_kind"] != "action_request" || event.APIPath != test.path || event.HTTPMethod != test.method || event.Code == 0 {
				t.Fatalf("incorrect event: %+v", event)
			}
			if !test.missingTenant && event.Resources["tenant_id"] != "tenant" {
				t.Fatalf("actor tenant missing: %+v", event.Resources)
			}
			if strings.Contains(test.url, "group-one") && event.Resources["group_id"] != "group-one" {
				t.Fatalf("group identifier missing: %+v", event.Resources)
			}
			encoded, err := json.Marshal(event)
			if err != nil {
				t.Fatal(err)
			}
			for _, secret := range []string{"private-content", "private-email", "private-invitation-token"} {
				if strings.Contains(string(encoded), secret) {
					t.Fatalf("private payload captured: %s", encoded)
				}
			}
			if event.RequestParams != "" || event.Error != nil {
				t.Fatalf("request body or error captured: %+v", event)
			}
		})
	}
}

func TestSettingsTeamAuditGroupAndRoleCRUD(t *testing.T) {
	db := setupSearchHandlerTestDB(t)
	if err := db.AutoMigrate(&entity.RoleDefaultModel{}); err != nil {
		t.Fatal(err)
	}
	if err := testsupport.PrepareEnterprisePermissionFixture(db, "tenant", "actor"); err != nil {
		t.Fatal(err)
	}
	handler := NewTenantHandler(service.NewTenantService(), nil, nil)
	var events []*common.OperationLog
	common.SetAuditLogSink(func(event *common.OperationLog) error { events = append(events, event); return nil })
	t.Cleanup(func() { common.SetAuditLogSink(nil) })
	writer := searchAuditRequest(searchAuditRouter(handler.CreateGroup, "POST", "/api/v1/tenant/groups", false), "POST", "/api/v1/tenant/groups", `{"name":"private-group"}`)
	var group struct {
		Code common.ErrorCode  `json:"code"`
		Data service.GroupItem `json:"data"`
	}
	if err := json.Unmarshal(writer.Body.Bytes(), &group); err != nil || group.Code != common.CodeSuccess || group.Data.ID == "" {
		t.Fatalf("create group: %v %s", err, writer.Body.String())
	}
	id := group.Data.ID
	searchAuditRequest(searchAuditRouter(handler.UpdateGroup, "PUT", "/api/v1/tenant/groups/:group_id", false), "PUT", "/api/v1/tenant/groups/"+id, `{"name":"private-renamed"}`)
	searchAuditRequest(searchAuditRouter(handler.DeleteGroup, "DELETE", "/api/v1/tenant/groups/:group_id", false), "DELETE", "/api/v1/tenant/groups/"+id, "")
	writer = searchAuditRequest(searchAuditRouter(handler.CreateTenantRole, "POST", "/api/v1/tenant/roles", false), "POST", "/api/v1/tenant/roles", `{"role_name":"private-role"}`)
	var role struct {
		Code common.ErrorCode       `json:"code"`
		Data service.TenantRoleItem `json:"data"`
	}
	if err := json.Unmarshal(writer.Body.Bytes(), &role); err != nil || role.Code != common.CodeSuccess || role.Data.ID == 0 {
		t.Fatalf("create role: %v %s", err, writer.Body.String())
	}
	roleID := strconv.FormatInt(int64(role.Data.ID), 10)
	searchAuditRequest(searchAuditRouter(handler.UpdateTenantRole, "PATCH", "/api/v1/tenant/roles/:role_id", false), "PATCH", "/api/v1/tenant/roles/"+roleID, `{"role_name":"private-renamed-role"}`)
	searchAuditRequest(searchAuditRouter(handler.DeleteTenantRole, "DELETE", "/api/v1/tenant/roles/:role_id", false), "DELETE", "/api/v1/tenant/roles/"+roleID, "")
	if len(events) != 6 {
		t.Fatalf("got %d events", len(events))
	}
	for index, action := range []string{"create_tenant_group", "update_tenant_group", "delete_tenant_group", "create_tenant_role", "update_tenant_role", "delete_tenant_role"} {
		event := events[index]
		if event.Operation != action || event.Code != 0 || event.Resources["outcome"] != "response_success" {
			t.Fatalf("incorrect CRUD event: %+v", event)
		}
		if index < 3 && event.Resources["group_id"] != id {
			t.Fatalf("group identifier missing: %+v", event.Resources)
		}
		if index >= 3 && (event.Resources["role_id"] != roleID && event.Resources["role_id"] != role.Data.ID) {
			t.Fatalf("role identifier missing: %+v", event.Resources)
		}
		encoded, err := json.Marshal(event)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(encoded), "private-") {
			t.Fatalf("private names captured: %s", encoded)
		}
	}
	var count int64
	if err := db.Model(&entity.PermissionChangeLog{}).Where("event_type = ?", "role.created").Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("permission history missing: count=%d error=%v", count, err)
	}
}
