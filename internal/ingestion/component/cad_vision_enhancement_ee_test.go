//go:build ee

package component

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"gorm.io/gorm"
	"ragflow/internal/common"
	"ragflow/internal/entity"
	"ragflow/internal/entity/models"
	"ragflow/internal/ingestion/component/schema"
	"ragflow/internal/storage"
)

type drawingVisionDriver struct {
	models.ModelDriver
	err          error
	wantImage    string
	wantLanguage string
	answer       string
}

func (d drawingVisionDriver) ChatWithMessages(ctx context.Context, _ string, messages []models.Message, _ *models.APIConfig, config *models.ChatConfig, _ *common.ModelUsage) (*models.ChatResponse, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if config.Vision == nil || !*config.Vision || len(messages) == 0 {
		return nil, errors.New("missing visual request")
	}
	text, ok := firstUserText(messages)
	if !ok || !strings.Contains(text, "只描述可见内容") {
		return nil, errors.New("missing configured prompt")
	}
	if d.wantLanguage != "" && !strings.Contains(text, "Write the description in "+d.wantLanguage+".") {
		return nil, errors.New("wrong description language")
	}
	parts, ok := messages[0].Content.([]any)
	if !ok || len(parts) != 2 {
		return nil, errors.New("missing region image")
	}
	part, ok := parts[1].(map[string]any)
	if !ok {
		return nil, errors.New("invalid image content")
	}
	imageURL, ok := part["image_url"].(map[string]any)
	if !ok || imageURL["url"] != d.wantImage {
		return nil, errors.New("wrong rendered region sent to model")
	}
	answer := "```markdown\n四个安装孔\n```"
	if d.answer != "" {
		answer = d.answer
	}
	return &models.ChatResponse{Answer: &answer}, d.err
}

func TestDrawingVisionEnhancesStoredRegion(t *testing.T) {
	store := storage.NewMemoryStorage()
	factory := storage.GetStorageFactory()
	original := factory.GetStorage()
	factory.SetStorage(store)
	t.Cleanup(func() { factory.SetStorage(original) })
	picture := picturePNG(t)
	if err := store.Put(t.Context(), "kb-test", "region", picture); err != nil {
		t.Fatal(err)
	}
	originalResolver := resolveModelConfig
	resolveModelConfig = func(_ context.Context, _ *gorm.DB, tenant string, kind entity.ModelType, id string) (models.ModelDriver, string, *models.APIConfig, int, error) {
		if tenant != "tenant1" || kind != entity.ModelTypeImage2Text || id != "vision1" {
			return nil, "", nil, 0, errors.New("incorrect vision model selection")
		}
		return drawingVisionDriver{wantImage: "data:image/png;base64," + base64.StdEncoding.EncodeToString(picture), wantLanguage: "Chinese"}, "vision", &models.APIConfig{}, 0, nil
	}
	t.Cleanup(func() { resolveModelConfig = originalResolver })

	chunks := []schema.ChunkDoc{
		{ID: "region", Text: "φ30", ImgID: "kb-test-region", Sheet: "A1", Positions: []byte(`[[1,0,10,0,10]]`), Extra: map[string]json.RawMessage{"extra": json.RawMessage(`"{\"cad\":{\"sheet_id\":\"s1\",\"source_ids\":[\"text:1\"]}}"`)}},
		{ID: "notes", Text: "技术说明"},
	}
	warnings, err := EnhanceCADChunks(t.Context(), nil, map[string]any{"tenant_id": "tenant1", "kb_id": "kb-test", "lang": " Chinese "}, chunks, CADVisionParam{LLMID: "vision1", SystemPrompt: "只描述可见内容"})
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) > 0 || len(chunks) != 2 || chunks[0].Text != "φ30\n\n[Visual description]\n四个安装孔" {
		t.Fatalf("description not appended: %+v warnings=%v", chunks, warnings)
	}
	if chunks[0].ID != "region" || chunks[0].ImgID != "kb-test-region" || chunks[0].Sheet != "A1" || string(chunks[0].Positions) != "[[1,0,10,0,10]]" || chunks[1].Text != "技术说明" {
		t.Fatalf("chunk identity/provenance changed: %#v", chunks)
	}
	var extra string
	if err := json.Unmarshal(chunks[0].Extra["extra"], &extra); err != nil {
		t.Fatalf("extra is not an encoded string: %s", chunks[0].Extra["extra"])
	}
	var visualPayload struct {
		CAD struct {
			SheetID     string   `json:"sheet_id"`
			SourceIDs   []string `json:"source_ids"`
			VisualRaw   string   `json:"visual_description"`
			VisualClean string   `json:"visual_clean"`
			VisualState string   `json:"visual_status"`
		} `json:"cad"`
	}
	if err := json.Unmarshal([]byte(extra), &visualPayload); err != nil {
		t.Fatal(err)
	}
	if visualPayload.CAD.SheetID != "s1" || len(visualPayload.CAD.SourceIDs) != 1 || visualPayload.CAD.VisualRaw != "四个安装孔" ||
		visualPayload.CAD.VisualClean != "四个安装孔" || visualPayload.CAD.VisualState != "clean" {
		t.Fatalf("visual provenance not preserved: %+v", visualPayload.CAD)
	}
}

func TestDrawingVisionDropsUnverifiedNumbersFromBody(t *testing.T) {
	store := storage.NewMemoryStorage()
	factory := storage.GetStorageFactory()
	original := factory.GetStorage()
	factory.SetStorage(store)
	t.Cleanup(func() { factory.SetStorage(original) })
	payload := picturePNG(t)
	if err := store.Put(t.Context(), "kb-test", "region", payload); err != nil {
		t.Fatal(err)
	}
	originalResolver := resolveModelConfig
	resolveModelConfig = func(_ context.Context, _ *gorm.DB, tenant string, kind entity.ModelType, id string) (models.ModelDriver, string, *models.APIConfig, int, error) {
		return drawingVisionDriver{
			wantImage: "data:image/png;base64," + base64.StdEncoding.EncodeToString(payload),
			answer:    "The region has a large circular vessel. The drawing number is M0000096 and the size is 4 inches. A pipe leaves from the right.",
		}, "vision", &models.APIConfig{}, 0, nil
	}
	t.Cleanup(func() { resolveModelConfig = originalResolver })
	chunks := []schema.ChunkDoc{
		{ID: "region", Text: "M0000506\n3-28-88\nGLYCOL PIPING DETAIL", ImgID: "kb-test-region", Extra: map[string]json.RawMessage{"extra": json.RawMessage(`"{\"cad\":{\"sheet_id\":\"s1\"}}"`)}},
	}
	warnings, err := EnhanceCADChunks(t.Context(), nil, map[string]any{"tenant_id": "tenant1", "kb_id": "kb-test"}, chunks, CADVisionParam{LLMID: "vision1", SystemPrompt: "只描述可见内容"})
	if err != nil || len(warnings) > 0 {
		t.Fatalf("enhancement failed: %v %v", err, warnings)
	}
	if strings.Contains(chunks[0].Text, "M0000096") || strings.Contains(chunks[0].Text, "4 inches") {
		t.Fatalf("unverified value entered retrieval text: %q", chunks[0].Text)
	}
	if !strings.Contains(chunks[0].Text, "[Visual description]") || !strings.Contains(chunks[0].Text, "circular vessel") {
		t.Fatalf("visual description missing from body: %q", chunks[0].Text)
	}
	var extra string
	if err := json.Unmarshal(chunks[0].Extra["extra"], &extra); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(extra, "M0000096") || !strings.Contains(extra, `"visual_status":"partial"`) {
		t.Fatalf("raw description not audited in extra: %s", extra)
	}
}

func TestDrawingVisionKeepsOriginalOnImageOrModelFailure(t *testing.T) {
	originalResolver := resolveTenantModelByType
	t.Cleanup(func() { resolveTenantModelByType = originalResolver })
	payload := picturePNG(t)
	wantImage := "data:image/png;base64," + base64.StdEncoding.EncodeToString(payload)
	resolveTenantModelByType = func(context.Context, *gorm.DB, string, entity.ModelType) (models.ModelDriver, string, *models.APIConfig, int, error) {
		return drawingVisionDriver{wantImage: wantImage, err: errors.New("model unavailable")}, "vision", &models.APIConfig{}, 0, nil
	}
	for _, tc := range []struct{ name, image, imgID string }{
		{"model failure", wantImage, ""},
		{"invalid image", "broken-png", ""},
		{"foreign image", "", "otherkb-region"},
		{"missing image", "", "kb1-missing"},
	} {
		t.Run(tc.name, func(t *testing.T) {

			chunks := []schema.ChunkDoc{{Text: "φ30", Image: tc.image, ImgID: tc.imgID}}
			warnings, err := EnhanceCADChunks(t.Context(), nil, map[string]any{"tenant_id": "tenant1", "kb_id": "kb1"}, chunks, CADVisionParam{SystemPrompt: "只描述可见内容"})
			if err != nil || chunks[0].Text != "φ30" || len(warnings) != 1 {
				t.Fatalf("failed enhancement changed chunk: %+v warnings=%v error=%v", chunks, warnings, err)
			}
		})
	}
}

func TestDrawingVisionCancellation(t *testing.T) {

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := EnhanceCADChunks(ctx, nil, nil, []schema.ChunkDoc{{Text: "φ30", ImgID: "kb1-region"}}, CADVisionParam{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
}

func TestDrawingVisionKeepsOriginalWhenModelCannotBeResolved(t *testing.T) {
	originalResolver := resolveModelConfig
	t.Cleanup(func() { resolveModelConfig = originalResolver })
	resolveModelConfig = func(context.Context, *gorm.DB, string, entity.ModelType, string) (models.ModelDriver, string, *models.APIConfig, int, error) {
		return nil, "", nil, 0, errors.New("model unavailable")
	}
	chunks := []schema.ChunkDoc{{ID: "region", Text: "φ30", ImgID: "kb1-region"}}
	warnings, err := EnhanceCADChunks(t.Context(), nil, map[string]any{"tenant_id": "tenant1"}, chunks, CADVisionParam{LLMID: "vision1"})
	if err != nil || chunks[0].Text != "φ30" || chunks[0].ImgID != "kb1-region" ||
		len(warnings) != 1 || !strings.Contains(warnings[0], "model unavailable") {
		t.Fatalf("model lookup failure must preserve chunks: chunks=%+v warnings=%v error=%v", chunks, warnings, err)
	}
}

func TestDrawingVisionKeepsApostrophesAndDropsQuotedSpans(t *testing.T) {
	raw := "The valve's flange doesn't leak. The tag 'V-101' is visible. A pipe turns upward."
	clean, dropped := cleanCADVisualDescription(raw, "")
	if dropped != 1 {
		t.Fatalf("dropped = %d, want 1: clean=%q", dropped, clean)
	}
	if clean != "The valve's flange doesn't leak. A pipe turns upward." {
		t.Fatalf("clean = %q", clean)
	}
}

func TestTransientCADVisionErrorAvoidsFalsePositives(t *testing.T) {
	if transientCADVisionError(errors.New("bad request: 5000 tokens exceeded")) {
		t.Fatal("5000 matched HTTP 500 retry filter")
	}
	if !transientCADVisionError(io.EOF) || !transientCADVisionError(errors.New("upstream HTTP 503")) {
		t.Fatal("real transient errors were not retried")
	}
}
