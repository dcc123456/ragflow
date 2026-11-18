//go:build ee

package component

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"
	"ragflow/internal/agent/runtime"
	"ragflow/internal/entity"
	"ragflow/internal/entity/models"
	"ragflow/internal/ingestion/component/globals"
	"ragflow/internal/ingestion/component/progress"
	"ragflow/internal/ingestion/component/schema"
)

const (
	cadVisionComponentName = "CADChunker"
	// This budget covers both retrieving the region and describing it.
	cadVisionRegionTimeout = 90 * time.Second
)

// CADVisionParam configures visual extraction after drawing regions have
// been chunked, rendered and uploaded. An empty model selects the tenant's
// default vision model; an empty prompt uses the drawing-specific instructions.
type CADVisionParam struct {
	Enabled      bool   `json:"enabled"`
	LLMID        string `json:"llm_id"`
	SystemPrompt string `json:"system_prompt"`
}

// EnhanceCADChunks appends visual descriptions to rendered CAD chunks.
// Call after chunk image upload so image references and highlight positions stay stable.
func EnhanceCADChunks(ctx context.Context, db *gorm.DB, inputs map[string]any, chunks []schema.ChunkDoc, param CADVisionParam) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var candidates []int
	for i := range chunks {
		if chunks[i].Image != "" || chunks[i].ImgID != "" {
			candidates = append(candidates, i)
		}
	}
	if len(candidates) == 0 {
		return nil, nil
	}
	tenantID := globals.GlobalOrInput(ctx, inputs, "tenant_id", "")
	if tenantID == "" {
		return skipDrawingVision(ctx, "tenant ID is missing")
	}
	var driver models.ModelDriver
	var model string
	var config *models.APIConfig
	var err error
	if param.LLMID != "" {
		driver, model, config, _, err = resolveModelConfig(ctx, db, tenantID, entity.ModelTypeImage2Text, param.LLMID)
	} else {
		driver, model, config, _, err = resolveTenantModelByType(ctx, db, tenantID, entity.ModelTypeImage2Text)
	}
	if err != nil {
		return skipDrawingVision(ctx, fmt.Sprintf("resolve vision model: %v", err))
	}
	if driver == nil {
		return skipDrawingVision(ctx, "no usable vision model")
	}
	prompt := strings.TrimSpace(param.SystemPrompt)
	if prompt == "" {
		prompt = "Describe this CAD drawing region for document retrieval. Extract visible components, geometry, labels, dimensions, tolerances, symbols and their spatial relationships. Preserve visible labels and numbers verbatim. Do not infer hidden geometry, unspecified dimensions, materials or engineering intent. If information is unreadable, do not guess. Output only a concise description."
	}
	prompt += "\nWrite the description in " + normalizeVisionLanguage(globals.GlobalOrInput(ctx, inputs, "lang", "")) + ". Preserve visible text and numbers in their original form."
	kbID := globals.GlobalOrInput(ctx, inputs, "kb_id", "")
	chatModel := models.NewChatModel(driver, &model, config)
	warnings := make([]string, len(chunks))
	jobs := make(chan int)
	var workers sync.WaitGroup
	done := (progress.Window{Span: 1}).Counter(ctx, len(candidates))
	for range min(visionEnhancementConcurrency, len(candidates)) {
		workers.Go(func() {
			for i := range jobs {
				if ctx.Err() != nil {
					continue
				}
				description, err := describeCADRegion(ctx, chatModel, chunks[i], kbID, prompt)
				if err != nil {
					warnings[i] = err.Error()
				} else {
					chunks[i].Text = strings.TrimSpace(chunks[i].Text + "\n\n" + description)
					chunks[i].TKNums = nil
				}
				done()
			}
		})
	}
schedule:
	for _, i := range candidates {
		select {
		case jobs <- i:
		case <-ctx.Done():
			break schedule
		}
	}
	close(jobs)
	workers.Wait()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var reported []string
	for i, warning := range warnings {
		if warning != "" {
			message := fmt.Sprintf("chunk %d: %s", i+1, warning)
			reported = append(reported, message)
			runtime.ReportProgressMessage(ctx, cadVisionComponentName, "WARNING: "+message)
		}
	}
	return reported, nil
}

// describeCADRegion loads one rendered region and issues one vision request.
func describeCADRegion(ctx context.Context, model *models.ChatModel, chunk schema.ChunkDoc, kbID, prompt string) (string, error) {
	callCtx, cancel := context.WithTimeout(ctx, cadVisionRegionTimeout)
	defer cancel()
	image, err := drawingChunkImage(callCtx, chunk, kbID)
	if err != nil {
		return "", err
	}
	vision := true
	config := &models.ChatConfig{Vision: &vision}
	if _, ok := model.ModelDriver.(*models.OllamaModel); ok {
		thinking := false
		config.Thinking = &thinking
	}
	finish := runtime.TrackIO(callCtx, cadVisionComponentName, "VLM", "operation=drawing_vision_enhancement")
	response, err := model.ChatWithMessages(callCtx, buildVisionMessages(prompt, image), config, nil)
	finish()
	if err != nil {
		return "", fmt.Errorf("vision request failed: %w", err)
	}
	description := strings.TrimSpace(extractVisionAnswer(response))
	if description == "" {
		return "", fmt.Errorf("vision model returned an empty description")
	}
	return description, nil
}

func skipDrawingVision(ctx context.Context, reason string) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	message := "vision enhancement skipped: " + reason
	runtime.ReportProgressMessage(ctx, cadVisionComponentName, "WARNING: "+message)
	return []string{message}, nil
}

func drawingChunkImage(ctx context.Context, chunk schema.ChunkDoc, kbID string) (string, error) {
	payload := chunk.Image
	if payload == "" {
		key, ok := strings.CutPrefix(chunk.ImgID, kbID+"-")
		if !ok || key == "" || kbID == "" {
			return "", fmt.Errorf("drawing image does not belong to the current knowledge base")
		}
		data, err := FetchBinary(ctx, kbID, key)
		if err != nil {
			return "", err
		}
		if len(data) > maxVisionImageBytes {
			return "", fmt.Errorf("drawing image exceeds size limit")
		}
		payload = base64.StdEncoding.EncodeToString(data)
	}
	image, err := materializeInlineVisionImage(payload)
	if err != nil {
		return "", err
	}
	if image == nil || !image.VLMDataValidated {
		return "", fmt.Errorf("drawing image is invalid")
	}
	return image.VLMData, nil
}
