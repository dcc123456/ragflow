//go:build ee

package handler

import (
	"bytes"
	"encoding/json/v2"
	"fmt"
	"mime/multipart"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"ragflow/internal/common"
	"ragflow/internal/dao"
	"ragflow/internal/entity"
	"ragflow/internal/permission"
	"ragflow/internal/permission/testsupport"
	"ragflow/internal/service/file"
	"ragflow/internal/storage"
)

func fileAuditCapture(t *testing.T) *[]*common.OperationLog {
	t.Helper()
	events := []*common.OperationLog{}
	common.SetAuditLogSink(func(event *common.OperationLog) error {
		events = append(events, event)
		return nil
	})
	t.Cleanup(func() { common.SetAuditLogSink(nil) })
	return &events
}

func fileAuditCheck(t *testing.T, event *common.OperationLog, action, outcome string) {
	t.Helper()
	if event.Operation != action || event.UserID != "actor" || event.Resources["tenant_id"] != "tenant" || event.Resources["outcome"] != outcome || event.Resources["event_kind"] != "action_request" || event.EventTime.IsZero() {
		t.Fatalf("incorrect audit event: %+v", event)
	}
	if (outcome == "response_error") != (event.Code != 0) {
		t.Fatalf("incorrect response code: %+v", event)
	}
	encoded, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"private-content", "private-name", "origin", "initiator", "Unsupported content type", "required argument", "Workspace not found", "invalid file rename target"} {
		if strings.Contains(string(encoded), secret) {
			t.Fatalf("private request or response information captured: %s", encoded)
		}
	}
	if event.RequestParams != "" || event.Error != nil {
		t.Fatalf("raw payload or error captured: %+v", event)
	}
}

func TestFileAuditValidationAndExclusions(t *testing.T) {
	handler := NewFileHandler(nil, nil)
	for _, test := range []struct {
		name, method, path, body, contentType, action string
		handler                                       gin.HandlerFunc
		anonymous                                     bool
	}{
		{name: "directory malformed", method: "POST", path: "/api/v1/files", body: `{"name":"private-name",`, action: "create_directory", handler: handler.UploadFile},
		{name: "unsupported upload", method: "POST", path: "/api/v1/files", body: "private-content", contentType: "text/plain", action: "upload_files_or_create_directory", handler: handler.UploadFile},
		{name: "multipart malformed", method: "POST", path: "/api/v1/files", body: "private-content", contentType: "multipart/form-data", action: "upload_files", handler: handler.UploadFile},
		{name: "delete malformed", method: "DELETE", path: "/api/v1/files", body: `{"ids":`, action: "delete_files", handler: handler.DeleteFiles},
		{name: "move malformed", method: "POST", path: "/api/v1/files/move", body: `{"src_file_ids":`, action: "move_files", handler: handler.MoveFiles},
		{name: "move rejected", method: "POST", path: "/api/v1/files/move", body: `{"src_file_ids":["one","two"],"dest_file_id":"destination","new_name":"private-name"}`, action: "move_files", handler: handler.MoveFiles},
		{name: "download missing identifier", method: "GET", path: "/api/v1/files", action: "download_file", handler: handler.Download},
		{name: "link malformed unchanged", method: "POST", path: "/api/v1/files/link-to-datasets", body: `{"file_ids":`, action: "link_files_to_datasets", handler: handler.LinkToDatasets},
		{name: "anonymous excluded", method: "DELETE", path: "/api/v1/files", body: `{}`, handler: handler.DeleteFiles, anonymous: true},
		{name: "read excluded", method: "GET", path: "/api/v1/files", handler: handler.ListFiles, anonymous: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			events := fileAuditCapture(t)
			request := httptest.NewRequest(test.method, test.path, strings.NewReader(test.body))
			contentType := test.contentType
			if contentType == "" {
				contentType = "application/json"
			}
			request.Header.Set("Content-Type", contentType)
			writer := httptest.NewRecorder()
			searchAuditRouter(test.handler, test.method, test.path, test.anonymous).ServeHTTP(writer, request)
			if test.action == "" {
				if len(*events) != 0 {
					t.Fatalf("excluded request emitted %d events", len(*events))
				}
				return
			}
			if len(*events) != 1 {
				t.Fatalf("got %d events; response %s", len(*events), writer.Body.String())
			}
			event := (*events)[0]
			fileAuditCheck(t, event, test.action, "response_error")
			if event.APIPath != test.path || event.Resources["http_status"] != writer.Code {
				t.Fatalf("incorrect request context: %+v", event)
			}
			if test.name == "move rejected" && (!slices.Equal(event.Resources["file_ids"].([]string), []string{"one", "two"}) || event.Resources["dest_file_id"] != "destination") {
				t.Fatalf("move identifiers missing: %+v", event.Resources)
			}
		})
	}
}

func fileAuditDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{TranslateError: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&entity.File{}, &entity.File2Document{}, &entity.Document{}, &entity.Knowledgebase{}, &entity.FileCommit{}, &entity.FileCommitItem{}); err != nil {
		t.Fatal(err)
	}
	previous := dao.DB
	dao.DB = db
	t.Cleanup(func() { dao.DB = previous })
	if err := testsupport.PrepareEnterprisePermissionFixture(db, "tenant", "actor"); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&entity.File{ID: "root", ParentID: "root", TenantID: "tenant", CreatedBy: "actor", Name: "/", Type: "folder"}).Error; err != nil {
		t.Fatal(err)
	}
	ctx := permission.WithIdentity(t.Context(), "actor", "tenant")
	if err := permission.NewDatabaseChecker(db).GrantResourceOwner(ctx, permission.ResourceRef{Kind: permission.ResourceKindFolder, ID: "root"}); err != nil {
		t.Fatal(err)
	}
	factory := storage.GetStorageFactory()
	previousStorage := factory.GetStorage()
	factory.SetStorage(storage.NewMemoryStorage())
	t.Cleanup(func() { factory.SetStorage(previousStorage) })
	return db
}

func TestFileAuditCRUD(t *testing.T) {
	db := fileAuditDB(t)
	handler := NewFileHandler(file.NewFileService(nil), nil)
	events := fileAuditCapture(t)
	writer := searchAuditRequest(searchAuditRouter(handler.UploadFile, "POST", "/api/v1/files", false), "POST", "/api/v1/files", `{"name":"private-name","parent_id":"root","type":"folder"}`)
	var folderResponse struct {
		Code common.ErrorCode `json:"code"`
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(writer.Body.Bytes(), &folderResponse); err != nil || folderResponse.Code != 0 || folderResponse.Data.ID == "" {
		t.Fatalf("create folder failed: %v %s", err, writer.Body.String())
	}
	folderID := folderResponse.Data.ID
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	if err := form.WriteField("parent_id", folderID); err != nil {
		t.Fatal(err)
	}
	part, err := form.CreateFormFile("file", "private-name.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte("private-content")); err != nil {
		t.Fatal(err)
	}
	if err := form.Close(); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest("POST", "/api/v1/files", &body)
	request.Header.Set("Content-Type", form.FormDataContentType())
	writer = httptest.NewRecorder()
	searchAuditRouter(handler.UploadFile, "POST", "/api/v1/files", false).ServeHTTP(writer, request)
	var uploaded entity.File
	if err := db.Where("parent_id = ? AND type != ?", folderID, "folder").Take(&uploaded).Error; err != nil {
		t.Fatalf("upload failed: %v %s", err, writer.Body.String())
	}
	writer = searchAuditRequest(searchAuditRouter(handler.Download, "GET", "/api/v1/files/:id", false), "GET", "/api/v1/files/"+uploaded.ID, "")
	if writer.Code != 200 || writer.Body.String() != "private-content" {
		t.Fatalf("download failed: %d %s", writer.Code, writer.Body.String())
	}
	searchAuditRequest(searchAuditRouter(handler.MoveFiles, "POST", "/api/v1/files/move", false), "POST", "/api/v1/files/move", fmt.Sprintf(`{"src_file_ids":[%q],"dest_file_id":"root","new_name":"private-name-renamed.txt"}`, uploaded.ID))
	var moved entity.File
	if err := db.Where("id = ?", uploaded.ID).Take(&moved).Error; err != nil || moved.ParentID != "root" || moved.Name != "private-name-renamed.txt" {
		t.Fatalf("file move failed: %v %+v", err, moved)
	}
	searchAuditRequest(searchAuditRouter(handler.DeleteFiles, "DELETE", "/api/v1/files", false), "DELETE", "/api/v1/files", fmt.Sprintf(`{"ids":[%q,%q]}`, uploaded.ID, folderID))
	var remaining int64
	db.Model(&entity.File{}).Where("id IN ?", []string{uploaded.ID, folderID}).Count(&remaining)
	if remaining != 0 {
		t.Fatalf("delete retained %d files", remaining)
	}
	if len(*events) != 5 {
		t.Fatalf("expected five action events, got %d", len(*events))
	}
	for i, action := range []string{"create_directory", "upload_files", "download_file", "move_files", "delete_files"} {
		outcome := "response_success"
		if action == "download_file" {
			outcome = "unknown"
		}
		fileAuditCheck(t, (*events)[i], action, outcome)
	}
	if (*events)[0].Resources["file_id"] != folderID || (*events)[0].Resources["parent_id"] != "root" || (*events)[1].Resources["parent_id"] != folderID || !slices.Equal((*events)[1].Resources["file_ids"].([]string), []string{uploaded.ID}) || (*events)[2].Resources["file_id"] != uploaded.ID || (*events)[3].Resources["dest_file_id"] != "root" || !slices.Equal((*events)[4].Resources["file_ids"].([]string), []string{uploaded.ID, folderID}) {
		t.Fatalf("missing action identifiers: %+v", *events)
	}
}

func TestFileAuditCommitAliases(t *testing.T) {
	db := fileAuditDB(t)
	if err := db.Create(&entity.File{ID: "file-one", ParentID: "root", TenantID: "tenant", CreatedBy: "actor", Name: "private-name.txt", Type: "txt"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&entity.File{}).Where("id = ?", "root").Updates(map[string]any{"name": "private-name", "source_type": string(entity.FileSourceKnowledgebase)}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&entity.Knowledgebase{ID: "dataset-one", TenantID: "tenant", CreatedBy: "actor", Name: "private-name"}).Error; err != nil {
		t.Fatal(err)
	}
	ctx := permission.WithIdentity(t.Context(), "actor", "tenant")
	for _, resource := range []permission.ResourceRef{{Kind: permission.ResourceKindFile, ID: "file-one"}, {Kind: permission.ResourceKindDataset, ID: "dataset-one"}} {
		if err := permission.NewDatabaseChecker(db).GrantResourceOwner(ctx, resource); err != nil {
			t.Fatal(err)
		}
	}
	handler := NewFileCommitHandler(file.NewFileCommitService())
	for _, test := range []struct{ path, url, action string }{
		{"/api/v1/folders/:folder_id/commits", "/api/v1/folders/root/commits", "create_file_commit"},
		{"/api/v1/workspaces/:workspace_id/commits", "/api/v1/workspaces/root/commits", "create_file_commit"},
		{"/api/v1/datasets/:dataset_id/commits", "/api/v1/datasets/dataset-one/commits", "create_dataset_commit"},
	} {
		t.Run(test.action+test.path, func(t *testing.T) {
			events := fileAuditCapture(t)
			handlerFunc := handler.CreateCommit
			if test.action == "create_dataset_commit" {
				handlerFunc = func(c *gin.Context) {
					CommitFolderResolver(handler, "datasets", "dataset_id")(c)
					if !c.IsAborted() {
						handler.CreateCommit(c)
					}
				}
			}
			writer := searchAuditRequest(searchAuditRouter(handlerFunc, "POST", test.path, false), "POST", test.url, `{"message":"private-content","files":[{"file_id":"file-one","file_name":"private-name.txt","operation":"modify","content":"private-content"}]}`)
			if len(*events) != 1 {
				t.Fatalf("expected one event, got %d; response %s", len(*events), writer.Body.String())
			}
			event := (*events)[0]
			fileAuditCheck(t, event, test.action, "response_success")
			commitID, ok := event.Resources["commit_id"].(string)
			if !ok || commitID == "" || event.Resources["folder_id"] != "root" || !slices.Equal(event.Resources["file_ids"].([]string), []string{"file-one"}) {
				t.Fatalf("commit identifiers missing: %+v", event)
			}
			var commit entity.FileCommit
			if err := db.Where("id = ?", commitID).Take(&commit).Error; err != nil || commit.FolderID != "root" {
				t.Fatalf("commit not persisted: %v %+v", err, commit)
			}
			if test.action == "create_dataset_commit" && event.Resources["dataset_id"] != "dataset-one" {
				t.Fatalf("dataset identifier missing: %+v", event)
			}
			searchAuditRequest(searchAuditRouter(handlerFunc, "POST", test.path, false), "POST", test.url, `{"message":"private-content","files":[{"file_id":"file-one","file_name":"private-name.txt","operation":"rename","new_name":" "}]}`)
			if len(*events) != 2 {
				t.Fatalf("rejected commit emitted %d events", len(*events)-1)
			}
			rejected := (*events)[1]
			fileAuditCheck(t, rejected, test.action, "response_error")
			if rejected.Resources["folder_id"] != "root" || !slices.Equal(rejected.Resources["file_ids"].([]string), []string{"file-one"}) || rejected.Resources["commit_id"] != nil {
				t.Fatalf("incorrect rejected commit identifiers: %+v", rejected)
			}
		})
	}
}
