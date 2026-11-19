//
//  Copyright (c) 2026 InfiniFlow. All rights reserved.
//
//  RAGFlow Enterprise Edition — Proprietary and Confidential.
//
//  This file is part of the RAGFlow Enterprise Edition and is licensed,
//  not sold, under the RAGFlow Enterprise Commercial License Agreement.
//  Unauthorized copying, modification, distribution, or disclosure of this
//  file, via any medium, is strictly prohibited.
//
//  Portions of this file may incorporate code from RAGFlow Community Edition,
//  which is licensed under the Apache License, Version 2.0.
//
//  For commercial licensing inquiries: sales@infiniflow.ai
//

//go:build cgo && ee

// Package deepdocserver implements the EE DeepDoc inference HTTP service: it
// wires the /predict/dla, /predict/tsr, /predict/ocr (operator-switched
// det/rec, mirroring the Python EE docker), /predict/ocr_det, /predict/ocr_rec
// and /health endpoints and exposes Run, the composition root that boots the
// server, plus RunServer, the edition-selected entrypoint the ragflow_server
// --deepdoc mode calls. Keeping it in its own (non-main) package lets that mode
// and any future in-process caller import it without ever sharing a package
// with a func main(), so there is no "two mains" clash.
package deepdocserver

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"ragflow/internal/common"
	"ragflow/internal/deepdoc/native"
)

// ── injectable backends: default to the native implementations; tests supply stubs ──
type dlaFunc func(ctx context.Context, modelDir string, img *native.Image) (native.DLAResult, error)
type tsrFunc func(ctx context.Context, modelDir string, img *native.Image) (native.TSREEResult, error)
type detFunc func(ctx context.Context, modelDir string, img *native.Image) (native.DetResult, error)
type ocrRecBatchFunc func(ctx context.Context, modelDir string, imgs []*native.Image) ([]native.OCRRecResult, error)

// OCRRecItem is one batch OCR-recognition result, aligned with
// native.OCRRecResult (Text/Score) so the HTTP response is order-correspondent
// with the input image batch.
type OCRRecItem struct {
	Text  string  `json:"text"`
	Score float32 `json:"score"`
}

// deepDocHandler serves the EE DeepDoc inference endpoints over HTTP.
type deepDocHandler struct {
	modelDir    string
	dla         dlaFunc
	tsr         tsrFunc
	det         detFunc
	ocrRecBatch ocrRecBatchFunc
}

// newDeepDocHandler builds a handler. A nil backend uses the real native
// implementation. Tests pass stubs so the HTTP layer runs without models.
func newDeepDocHandler(modelDir string, dla dlaFunc, tsr tsrFunc, det detFunc, ocrRecBatch ocrRecBatchFunc) *deepDocHandler {
	if dla == nil {
		dla = native.RunDLA
	}
	if tsr == nil {
		tsr = native.RunTSREE
	}
	if det == nil {
		det = native.RunDet
	}
	if ocrRecBatch == nil {
		ocrRecBatch = native.RunOCRRecBatchReal
	}
	return &deepDocHandler{modelDir: modelDir, dla: dla, tsr: tsr, det: det, ocrRecBatch: ocrRecBatch}
}

// newTSRHandler is the TSR-only convenience constructor, kept for callers (and
// tests) that only need the /predict/tsr endpoint.
func newTSRHandler(modelDir string, tsr tsrFunc) *deepDocHandler {
	return newDeepDocHandler(modelDir, nil, tsr, nil, nil)
}

// registerRoutes wires the EE DeepDoc endpoints.
func (h *deepDocHandler) registerRoutes(e *gin.Engine) {
	e.POST("/predict/dla", h.predictDLA)
	e.POST("/predict/tsr", h.predictTSR)
	e.POST("/predict/ocr", h.predictOCR)
	e.POST("/predict/ocr_det", h.predictOCRDet)
	e.POST("/predict/ocr_rec", h.predictOCRRec)
	// /health mirrors the Python deepdoc server (Dockerfile HEALTHCHECK) so the Go
	// image can use the same probe.
	e.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
}

// predictDLA implements POST /predict/dla → native.RunDLA (layout analysis).
func (h *deepDocHandler) predictDLA(c *gin.Context) {
	img, err := readSingleImage(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	res, err := h.dla(c.Request.Context(), h.modelDir, img)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Data(http.StatusOK, "application/json", []byte(res.Wire()))
}

// predictTSR implements POST /predict/tsr → native.RunTSREE (EE 2-class TSR).
// The response mirrors the EE Python service
// ({"bboxes": [[x0,y0,x1,y1,score,cls],...]}, cls ∈ {0,1}).
func (h *deepDocHandler) predictTSR(c *gin.Context) {
	img, err := readSingleImage(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	res, err := h.tsr(c.Request.Context(), h.modelDir, img)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Data(http.StatusOK, "application/json", []byte(res.Wire()))
}

// predictOCRDet implements POST /predict/ocr_det → native.RunDet (OCR text-box
// detection).
func (h *deepDocHandler) predictOCRDet(c *gin.Context) {
	img, err := readSingleImage(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	res, err := h.det(c.Request.Context(), h.modelDir, img)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Data(http.StatusOK, "application/json", []byte(res.Wire()))
}

// predictOCRRec implements POST /predict/ocr_rec → native.RunOCRRecBatchReal
// (batch OCR text recognition), aligned with the in-process OCR batch rec.
//
//	request:  multipart/form-data multiple files ("files"/"images"), OR
//	          JSON {"images": ["<base64>", ...]}
//	response: [{"text":..., "score":...}, ...]  (order-correspondent with input)
func (h *deepDocHandler) predictOCRRec(c *gin.Context) {
	imgs, err := readImages(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	results, err := h.ocrRecBatch(c.Request.Context(), h.modelDir, imgs)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	items := make([]OCRRecItem, len(results))
	for i, r := range results {
		items[i] = OCRRecItem{Text: r.Text, Score: r.Score}
	}
	c.JSON(http.StatusOK, items)
}

// predictOCR implements POST /predict/ocr — the operator-switched OCR endpoint
// that mirrors the Python EE docker's OCREndpoint contract EXACTLY, so the
// existing Python assembly logic (deepdoc/vision/ocr_cli.py OCRClient) can call
// the Go docker transparently with no changes:
//
//	request:  multipart/form-data: field "request" = image file(s),
//	          field "operator" = "det" (default) | "rec"
//	          OR JSON {"request": "<base64 image>", "operator": "det"|"rec"}
//	response (det): {"output": [[[ [x,y], ... ]]]}     (quadrilateral boxes)
//	response (rec): {"output": [[[ ["text", score] ]]]} (per-image OCR-rec)
//
// This is deliberately distinct from /predict/ocr_det and /predict/ocr_rec,
// which keep the Go-native flat schemas used by in-process callers.
func (h *deepDocHandler) predictOCR(c *gin.Context) {
	images, operator, err := readOCRRequest(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if len(images) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing 'request' image"})
		return
	}
	switch operator {
	case "rec":
		results, rerr := h.ocrRecBatch(c.Request.Context(), h.modelDir, images)
		if rerr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": rerr.Error()})
			return
		}
		// Mirror Python OCREndpoint's rec encode_response exactly: for a single
		// image PaddleOCR returns [[(text,score),...]], predict wraps it once
		// more, and encode_response puts it under "output", giving
		//   {"output": [[[ ["text", score], ... ]]]}   (4 nest levels)
		// so the Python client's output[0][0][0][0] == text, as in ocr_cli.py.
		out := make([]any, len(results))
		for i, r := range results {
			out[i] = [][][]any{[][]any{[]any{r.Text, r.Score}}}
		}
		c.JSON(http.StatusOK, gin.H{"output": out})
	case "det":
		// The EE assembly sends exactly one page per det request; use the first.
		res, derr := h.det(c.Request.Context(), h.modelDir, images[0])
		if derr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": derr.Error()})
			return
		}
		c.Data(http.StatusOK, "application/json", []byte(res.Wire()))
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("unknown operator %q (want det or rec)", operator)})
		return
	}
}

// readOCRRequest reads the operator and the image(s) for POST /predict/ocr,
// matching the Python OCREndpoint contract: the image(s) travel in the
// "request" form field (multipart file OR base64 JSON) and the operator in the
// "operator" field ("det" by default, "rec" for recognition). It returns one
// *native.Image per uploaded file (the EE assembly sends one crop per rec
// request, and one page per det request).
func readOCRRequest(c *gin.Context) (images []*native.Image, operator string, err error) {
	ct := c.ContentType()
	operator = "det"
	switch {
	case strings.HasPrefix(ct, "multipart/form-data"):
		if op := c.PostForm("operator"); op != "" {
			operator = op
		}
		form, ferr := c.MultipartForm()
		if ferr != nil {
			return nil, "", ferr
		}
		files := form.File["request"]
		if len(files) == 0 {
			return nil, "", errors.New("missing 'request' form field")
		}
		for _, f := range files {
			rc, oerr := f.Open()
			if oerr != nil {
				return nil, "", oerr
			}
			data, oerr := io.ReadAll(rc)
			rc.Close()
			if oerr != nil {
				return nil, "", oerr
			}
			img, oerr := decodeNative(data)
			if oerr != nil {
				return nil, "", oerr
			}
			images = append(images, img)
		}
		return images, operator, nil
	case strings.HasPrefix(ct, "application/json"):
		var body struct {
			Request  string `json:"request"`
			Operator string `json:"operator"`
		}
		if berr := c.ShouldBindJSON(&body); berr != nil {
			return nil, "", berr
		}
		if body.Operator != "" {
			operator = body.Operator
		}
		if body.Request == "" {
			return nil, "", errors.New("missing 'request' field")
		}
		data, derr := base64.StdEncoding.DecodeString(body.Request)
		if derr != nil {
			return nil, "", fmt.Errorf("invalid base64 in request: %w", derr)
		}
		img, derr := decodeNative(data)
		if derr != nil {
			return nil, "", derr
		}
		images = append(images, img)
		return images, operator, nil
	default:
		return nil, "", fmt.Errorf("unsupported content-type %q", ct)
	}
}

// readSingleImage extracts one image from a multipart "request" field or a JSON
// {"image": "<base64>"} body, decodes it, and converts it to *native.Image.
// The "request" multipart field mirrors the Python deepdoc server's contract
// (deepdoc/vision/{dla_cli,tsr_cli,ocr_cli}.py all POST files={"request": ...}),
// so the Go docker is a drop-in for the Python deepdoc service; the JSON "image"
// body is used by the in-repo client.
func readSingleImage(c *gin.Context) (*native.Image, error) {
	data, err := readSingleImageBody(c)
	if err != nil {
		return nil, err
	}
	return decodeNative(data)
}

// readImages extracts one or more images for batch OCR-rec: multipart file
// fields ("files" then "images") or a JSON {"images": ["<base64>", ...]} body.
func readImages(c *gin.Context) ([]*native.Image, error) {
	ct := c.ContentType()
	switch {
	case strings.HasPrefix(ct, "multipart/form-data"):
		form, err := c.MultipartForm()
		if err != nil {
			return nil, err
		}
		files := form.File["files"]
		if len(files) == 0 {
			files = form.File["images"]
		}
		if len(files) == 0 {
			return nil, errors.New("missing 'files' form field")
		}
		imgs := make([]*native.Image, 0, len(files))
		for _, f := range files {
			rc, err := f.Open()
			if err != nil {
				return nil, err
			}
			data, err := io.ReadAll(rc)
			rc.Close()
			if err != nil {
				return nil, err
			}
			img, err := decodeNative(data)
			if err != nil {
				return nil, err
			}
			imgs = append(imgs, img)
		}
		return imgs, nil
	case strings.HasPrefix(ct, "application/json"):
		var body struct {
			Images []string `json:"images"`
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			return nil, err
		}
		if len(body.Images) == 0 {
			return nil, errors.New("missing 'images' field")
		}
		imgs := make([]*native.Image, 0, len(body.Images))
		for _, s := range body.Images {
			data, err := base64.StdEncoding.DecodeString(s)
			if err != nil {
				return nil, fmt.Errorf("invalid base64 in images: %w", err)
			}
			img, err := decodeNative(data)
			if err != nil {
				return nil, err
			}
			imgs = append(imgs, img)
		}
		return imgs, nil
	default:
		return nil, fmt.Errorf("unsupported content-type %q", ct)
	}
}

// readSingleImageBody returns the raw image bytes from a multipart "request"
// field or a JSON {"image": "<base64>"} body. The "request" multipart field
// matches the Python deepdoc server contract so the Go docker can replace it;
// the JSON "image" body is the in-repo client contract.
func readSingleImageBody(c *gin.Context) ([]byte, error) {
	ct := c.ContentType()
	switch {
	case strings.HasPrefix(ct, "multipart/form-data"):
		f, err := c.FormFile("request")
		if err != nil {
			return nil, errors.New("missing 'request' form field")
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		defer rc.Close()
		return io.ReadAll(rc)
	case strings.HasPrefix(ct, "application/json"):
		var body struct {
			Image string `json:"image"`
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			return nil, err
		}
		if body.Image == "" {
			return nil, errors.New("missing 'image' field")
		}
		return base64.StdEncoding.DecodeString(body.Image)
	default:
		return nil, fmt.Errorf("unsupported content-type %q", ct)
	}
}

// decodeNative decodes image bytes and converts to *native.Image.
func decodeNative(data []byte) (*native.Image, error) {
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("cannot decode image: %w", err)
	}
	ni, err := native.FromImage(img)
	if err != nil {
		return nil, err
	}
	return ni, nil
}

// validateModelDir is the fail-fast boot preflight: EE_MODEL_DIR must be set and
// must contain every weight listed in common.DeepDocModelFiles. It runs before
// the server starts so a misconfigured deploy — unset dir or a partially
// provisioned model snapshot — fails loudly at launch instead of serving 500s
// on the first inference request.
func validateModelDir(modelDir string) error {
	if modelDir == "" {
		return errors.New("EE_MODEL_DIR not set: the EE DeepDoc server requires the model directory")
	}
	if !common.HasModelFiles(modelDir) {
		return fmt.Errorf("EE_MODEL_DIR %q is missing one or more required model files %v", modelDir, common.DeepDocModelFiles)
	}
	return nil
}

// newEngine builds the inference HTTP engine: the project-standard per-request
// access log, panic recovery, and the EE DeepDoc routes. It reuses
// common.GinLogger — the same middleware the admin/api/router services use — so
// every request lands in the shared log with the rest of the application.
// GinLogger is registered BEFORE Recovery so a panicking handler is still logged
// as a 5xx: Recovery's recover() unwinds past any middleware placed after it,
// skipping its post-Next logging. This mirrors gin.Default() (Logger before
// Recovery).
func newEngine(h *deepDocHandler) *gin.Engine {
	e := gin.New()
	e.RemoveExtraSlash = true
	e.Use(common.GinLogger())
	e.Use(gin.Recovery())
	h.registerRoutes(e)
	return e
}

// Run boots the EE DeepDoc HTTP server. It initializes the statically-linked
// ONNX Runtime, then serves /predict/dla, /predict/tsr, /predict/ocr
// (operator-switched det/rec, mirroring the Python EE docker), /predict/ocr_det,
// /predict/ocr_rec and /health until ctx is cancelled, with graceful shutdown.
func Run(ctx context.Context, port int) error {
	return run(ctx, port, nil)
}

func run(ctx context.Context, port int, ready func() error) error {
	if err := native.InitORT(); err != nil {
		return fmt.Errorf("InitORT: %w", err)
	}
	modelDir := os.Getenv("EE_MODEL_DIR")
	if err := validateModelDir(modelDir); err != nil {
		return err
	}
	h := newDeepDocHandler(modelDir, nil, nil, nil, nil)

	e := newEngine(h)

	addr := fmt.Sprintf(":%d", port)
	srv := &http.Server{Addr: addr, Handler: e}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen DeepDoc server on %s: %w", addr, err)
	}
	defer listener.Close()

	common.Info(fmt.Sprintf("Starting DeepDoc HTTP server on %s", addr))
	if ready != nil {
		if err := ready(); err != nil {
			return fmt.Errorf("report DeepDoc readiness: %w", err)
		}
	}
	go func() {
		if err := srv.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			common.Fatal("DeepDoc server failed", zap.Error(err))
		}
	}()

	<-ctx.Done()
	common.Info("DeepDoc server received shutdown signal")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		common.Error("DeepDoc shutdown error", err)
	}
	return nil
}
