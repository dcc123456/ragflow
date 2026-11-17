//go:build ee

package handler

import (
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gorm.io/gorm"
	"ragflow/internal/common"
	"ragflow/internal/dao"
	"ragflow/internal/entity"
	"ragflow/internal/ingestion/testutil"
	"ragflow/internal/service"

	"github.com/gin-gonic/gin"
)

func TestDeleteAccount(t *testing.T) {
	for _, test := range []struct {
		name, body               string
		authenticated, superuser bool
		code                     common.ErrorCode
	}{
		{"active user", `{"confirmation_email":"actor@example.test","user_id":"other","email":"other@example.test"}`, true, false, common.CodeSuccess},
		{"API key", `{"confirmation_email":"actor@example.test"}`, true, false, common.CodeForbidden},
		{"missing authentication", `{"confirmation_email":"actor@example.test"}`, false, false, common.CodeUnauthorized},
		{"missing confirmation", `{}`, true, false, common.CodeArgumentError},
		{"wrong confirmation", `{"confirmation_email":"wrong@example.test"}`, true, false, common.CodeArgumentError},
		{"malformed confirmation", `{`, true, false, common.CodeArgumentError},
		{"superuser", `{"confirmation_email":"actor@example.test"}`, true, true, common.CodeForbidden},
	} {
		t.Run(test.name, func(t *testing.T) {
			db := setupAccountDeletionDB(t)
			user := &entity.User{ID: "actor", Email: "actor@example.test", Nickname: "Actor", IsActive: "1", IsAuthenticated: "1", IsAnonymous: "0", IsSuperuser: new(test.superuser)}
			if err := db.Create(user).Error; err != nil {
				t.Fatal(err)
			}
			other := &entity.User{ID: "other", Email: "other@example.test", Nickname: "Other", IsActive: "1", IsAuthenticated: "1", IsAnonymous: "0"}
			if err := db.Create(other).Error; err != nil {
				t.Fatal(err)
			}
			h := NewUserHandler(service.NewUserService(), service.NewUserTenantService())
			router := gin.New()
			router.DELETE("/api/v1/users/me", func(c *gin.Context) {
				if test.authenticated {
					c.Set("user", user)
					c.Set("auth_via_api_token", test.name == "API key")
				}
				h.DeleteAccount(c)
			})
			request := httptest.NewRequest(http.MethodDelete, "/api/v1/users/me", strings.NewReader(test.body))
			request.Header.Set("Content-Type", "application/json")
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, request)
			var response struct {
				Code common.ErrorCode `json:"code"`
			}
			if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			if response.Code != test.code {
				t.Fatalf("response = %s", recorder.Body.String())
			}
			var count int64
			if err := db.Model(&entity.User{}).Where("id = ?", user.ID).Count(&count).Error; err != nil {
				t.Fatal(err)
			}
			want := int64(1)
			if test.code == common.CodeSuccess {
				want = 0
			}
			if count != want {
				t.Fatalf("remaining actor = %d, want %d", count, want)
			}
			if _, err := dao.NewUserDAO().GetByID(t.Context(), db, other.ID); err != nil {
				t.Fatalf("other user removed: %v", err)
			}
			if test.code == common.CodeSuccess {
				cookies := recorder.Result().Cookies()
				if len(cookies) != 1 || cookies[0].Name != "ragflow_auth" || cookies[0].MaxAge != -1 {
					t.Fatalf("cookies = %+v", cookies)
				}
			}
		})
	}
}

func setupAccountDeletionDB(t *testing.T) *gorm.DB {
	db := testutil.SetupTestDB(t, &entity.BillingSubscription{},
		&entity.User{}, &entity.Tenant{}, &entity.UserTenant{},
		&entity.Knowledgebase{}, &entity.Document{}, &entity.Task{}, &entity.File{}, &entity.File2Document{},
		&entity.Chat{}, &entity.ChatSession{}, &entity.ConversationMessage{}, &entity.ConversationReference{},
		&entity.API4Conversation{}, &entity.API4ConversationMessage{}, &entity.API4ConversationReference{},
		&entity.ChatChannel{}, &entity.UserCanvas{}, &entity.UserCanvasVersion{},
		&entity.Search{}, &entity.Memory{}, &entity.MemoryTask{}, &entity.MCPServer{},
		&entity.SkillSearchConfig{},
		&entity.CompilationTemplate{}, &entity.CompilationTemplateGroup{},
		&entity.Connector{}, &entity.Connector2Kb{}, &entity.SyncLogs{},
		&entity.KnowledgeCompileDataset{}, &entity.WikiDocumentDirty{}, &entity.PipelineOperationLog{},
		&entity.IngestionTask{}, &entity.IngestionTaskLog{}, &entity.FileCommit{}, &entity.FileCommitItem{},
		&entity.TenantModelProvider{}, &entity.TenantModelInstance{}, &entity.TenantModel{},
		&entity.TenantLangfuse{}, &entity.APIToken{}, &entity.InvitationCode{},
	)
	t.Cleanup(testutil.ReplaceDBForTest(t, db))
	return db
}
