//go:build ee

package component

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"regexp"
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
	// Native text is supplied as read-only context so the model does not
	// re-OCR it. Very large regions are clipped; the prompt explicitly says
	// the reference is partial and must not be transcribed.
	cadVisionNativeTextLimit = 6000
)

// cadVisionPromptSuffix is appended even when a tenant supplies its own
// prompt. The visual description may enter retrieval text, so the model must
// never restate values that the CAD parser already extracted.
const cadVisionPromptSuffix = "\n\nNative CAD text extracted by the parser is authoritative. Do not transcribe, repeat, correct, or reinterpret any text, tag, drawing number, dimension, material, or other value. Do not output numbers or quoted text. Use words such as 'several' or 'multiple' instead of counts. Describe only visible geometry, symbols, spatial relationships, and connections. Do not infer hidden geometry, unspecified dimensions, or engineering intent."

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
		prompt = "Describe this CAD drawing region for document retrieval. Focus on visible components, geometry, symbols and their spatial relationships. Do not infer hidden geometry, unspecified dimensions, materials or engineering intent. If information is unreadable, do not guess. Output only a concise description."
	}
	lang := normalizeVisionLanguage(globals.GlobalOrInput(ctx, inputs, "lang", ""))
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
				description, err := describeCADRegion(ctx, chatModel, chunks[i], kbID, prompt, lang)
				if err != nil {
					warnings[i] = err.Error()
					_ = setCADVisualExtra(&chunks[i], "", "", "failed", 0, model, err.Error())
				} else {
					clean, dropped := cleanCADVisualDescription(description, chunks[i].Text)
					status := "clean"
					switch {
					case strings.TrimSpace(clean) == "":
						status = "rejected"
					case dropped > 0:
						status = "partial"
					}
					if err := setCADVisualExtra(&chunks[i], description, clean, status, dropped, model, ""); err != nil {
						warnings[i] = "store visual description: " + err.Error()
					}
					if clean != "" {
						chunks[i].Text = strings.TrimSpace(chunks[i].Text + "\n\n[Visual description]\n" + clean)
						chunks[i].TKNums = nil
					}
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
// Only timeout/network/rate-limit failures are retried: a model that returns a
// description we cannot use will not become correct by asking it again.
func describeCADRegion(ctx context.Context, model *models.ChatModel, chunk schema.ChunkDoc, kbID, prompt, lang string) (string, error) {
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		description, err := describeCADRegionOnce(ctx, model, chunk, kbID, prompt, lang)
		if err == nil {
			return description, nil
		}
		lastErr = err
		if attempt == 1 || !transientCADVisionError(err) {
			break
		}
		timer := time.NewTimer(500 * time.Millisecond)
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			return "", ctx.Err()
		}
	}
	return "", lastErr
}

func describeCADRegionOnce(ctx context.Context, model *models.ChatModel, chunk schema.ChunkDoc, kbID, prompt, lang string) (string, error) {
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
	response, err := model.ChatWithMessages(callCtx, buildVisionMessages(cadVisionPrompt(prompt, chunk, lang), image), config, nil)
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

func cadVisionPrompt(prompt string, chunk schema.ChunkDoc, lang string) string {
	var b strings.Builder
	b.WriteString(strings.TrimSpace(prompt))
	b.WriteString(cadVisionPromptSuffix)
	native := strings.TrimSpace(chunk.Text)
	if native != "" {
		header := "\n\nNative CAD text (authoritative reference; do not repeat or transcribe):\n<<<\n"
		if runes := []rune(native); len(runes) > cadVisionNativeTextLimit {
			native = string(runes[:cadVisionNativeTextLimit]) + "\n[...]"
			header = "\n\nNative CAD text (partial authoritative reference; do not repeat or transcribe):\n<<<\n"
		}
		b.WriteString(header)
		b.WriteString(native)
		b.WriteString("\n>>>")
	}
	if lang != "" {
		b.WriteString("\nWrite the description in " + lang + ".")
	}
	return b.String()
}

var (
	cadVisualDigit        = regexp.MustCompile(`[0-9０-９]`)
	cadVisualQuoted       = regexp.MustCompile(`["“”]|(?:^|[^\p{L}\p{N}])['‘][^'’\n]+['’](?:$|[^\p{L}\p{N}])`)
	cadVisualSplit        = regexp.MustCompile(`[.!?。！？;\n]+`)
	cadVisualTransientErr = regexp.MustCompile(`\b(?:eof|429|500|502|503|504)\b`)
)

// cleanCADVisualDescription keeps only visual statements. The model is told
// not to transcribe values; this is the enforcement layer. Any sentence that
// contains a digit, a quoted span, or a near-copy of native text is dropped
// rather than trusted, because even a number that appears in the native text
// can be attached to the wrong device.
func cleanCADVisualDescription(raw, native string) (string, int) {
	nativeNorm := normalizeCADFactText(native)
	var kept []string
	dropped := 0
	matches := cadVisualSplit.FindAllStringIndex(raw, -1)
	start := 0
	for i := 0; i <= len(matches); i++ {
		end := len(raw)
		delim := ""
		if i < len(matches) {
			end = matches[i][0]
			delim = strings.TrimSpace(strings.ReplaceAll(raw[matches[i][0]:matches[i][1]], "\n", " "))
		}
		sentence := strings.Join(strings.Fields(strings.TrimSpace(raw[start:end])), " ")
		if i < len(matches) {
			start = matches[i][1]
		}
		if sentence == "" {
			continue
		}
		if cadVisualDigit.MatchString(sentence) || cadVisualQuoted.MatchString(sentence) {
			dropped++
			continue
		}
		norm := normalizeCADFactText(sentence)
		if norm == "" {
			continue
		}
		if len([]rune(norm)) >= 24 && (strings.Contains(nativeNorm, norm) || (len([]rune(nativeNorm)) >= 24 && strings.Contains(norm, nativeNorm))) {
			dropped++
			continue
		}
		if delim != "" {
			sentence += delim
		}
		kept = append(kept, sentence)
	}
	return strings.Join(kept, " "), dropped
}

func normalizeCADFactText(text string) string {
	var b strings.Builder
	space := false
	for _, r := range strings.ToLower(text) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			space = false
		case r >= 0x4e00 && r <= 0x9fff:
			b.WriteRune(r)
			space = false
		default:
			if !space {
				b.WriteByte(' ')
				space = true
			}
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// setCADVisualExtra stores the raw model answer and its cleaned projection in
// extra.cad. The raw answer is never searched; the cleaned projection is what
// the enhancement appends to the chunk body.
func setCADVisualExtra(ck *schema.ChunkDoc, raw, clean, status string, dropped int, model, failure string) error {
	payload := map[string]any{}
	if encoded, ok := ck.GetExtraString("extra"); ok && strings.TrimSpace(encoded) != "" {
		if err := json.Unmarshal([]byte(encoded), &payload); err != nil {
			return err
		}
	}
	cad, _ := payload["cad"].(map[string]any)
	if cad == nil {
		cad = map[string]any{}
		payload["cad"] = cad
	}
	cad["visual_description"] = raw
	cad["visual_clean"] = clean
	cad["visual_status"] = status
	cad["visual_dropped"] = dropped
	if model != "" {
		cad["visual_model"] = model
	}
	if failure != "" {
		cad["visual_error"] = failure
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return ck.SetExtraValue("extra", string(encoded))
}

func transientCADVisionError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	message := strings.ToLower(err.Error())
	if cadVisualTransientErr.MatchString(message) {
		return true
	}
	for _, token := range []string{
		"timeout", "timed out", "deadline exceeded", "connection reset",
		"connection refused", "temporary", "temporarily",
	} {
		if strings.Contains(message, token) {
			return true
		}
	}
	return false
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
