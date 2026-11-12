//go:build ee

package handler

import (
	"bytes"
	"encoding/json/v2"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"ragflow/internal/dao"
	"ragflow/internal/entity"
	"ragflow/internal/permission"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestEvaluationRESTCollectionLifecycleAndIsolation(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err = db.AutoMigrate(&entity.EvaluationCollection{}, &entity.EvaluationCase{}, &entity.EvaluationRun{}, &entity.EvaluationResult{}, &entity.User{}, &entity.Tenant{}, &entity.UserTenant{}, &entity.Role{}, &entity.RoleResource{}); err != nil {
		t.Fatal(err)
	}
	old := dao.DB
	dao.DB = db
	t.Cleanup(func() { dao.DB = old })
	status := "1"
	for _, tenant := range []string{"first", "second"} {
		if err = db.Create(&entity.User{ID: tenant, Email: tenant + "@test.invalid", Nickname: tenant}).Error; err != nil {
			t.Fatal(err)
		}
		if err = db.Create(&entity.Tenant{ID: tenant, ParserIDs: "", Status: &status}).Error; err != nil {
			t.Fatal(err)
		}
		if err = db.Create(&entity.UserTenant{ID: tenant, UserID: tenant, TenantID: tenant, Role: "owner", Status: &status}).Error; err != nil {
			t.Fatal(err)
		}
	}
	h := NewEvaluationHandler()
	r := gin.New()
	r.Use(func(c *gin.Context) {
		if user := c.GetHeader("Test-Identity"); user != "" {
			c.Request = c.Request.WithContext(permission.WithIdentity(c.Request.Context(), user, user))
		}
	})
	r.POST("/collections", h.CreateCollection)
	r.GET("/collections/:collection_id", h.Collection)
	r.GET("/collections/:collection_id/cases", h.Cases)
	r.PATCH("/collections/:collection_id", h.UpdateCollection)
	r.DELETE("/collections/:collection_id", h.DeleteCollection)
	request := func(method, path, identity, content string, body []byte) (int, map[string]any) {
		t.Helper()
		req := httptest.NewRequest(method, path, bytes.NewReader(body))
		if identity != "" {
			req.Header.Set("Test-Identity", identity)
		}
		if content != "" {
			req.Header.Set("Content-Type", content)
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		var response map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
			t.Fatalf("invalid response: %s", w.Body)
		}
		return w.Code, response
	}
	code, _ := request("GET", "/collections/missing", "", "", nil)
	if code != http.StatusUnauthorized {
		t.Fatalf("no authentication = %d", code)
	}
	var upload bytes.Buffer
	form := multipart.NewWriter(&upload)
	part, err := form.CreateFormFile("file", "cases.csv")
	if err != nil {
		t.Fatal(err)
	}
	part.Write([]byte("question,reference_answer\nq,a\n"))
	form.WriteField("name", "Fixture")
	form.Close()
	code, body := request("POST", "/collections", "first", form.FormDataContentType(), upload.Bytes())
	if code != 200 || body["code"] != float64(0) {
		t.Fatalf("upload: %d %+v", code, body)
	}
	data := body["data"].(map[string]any)
	id := data["collection_id"].(string)
	code, body = request("GET", "/collections/"+id+"/cases", "first", "", nil)
	if code != 200 || body["data"].(map[string]any)["total"] != float64(1) {
		t.Fatalf("cases: %d %+v", code, body)
	}
	code, body = request("GET", "/collections/"+id, "second", "", nil)
	if code != 404 {
		t.Fatalf("cross-tenant collection: %d %+v", code, body)
	}
	code, _ = request("PATCH", "/collections/"+id, "first", "application/json", []byte(`{"tenant_id":"second"}`))
	if code != 400 {
		t.Fatalf("ownership reassignment accepted: %d", code)
	}
	code, _ = request("DELETE", "/collections/"+id, "first", "", nil)
	if code != 200 {
		t.Fatalf("delete: %d", code)
	}
	code, _ = request("GET", "/collections/"+id, "first", "", nil)
	if code != 404 {
		t.Fatalf("deleted collection: %d", code)
	}
}
