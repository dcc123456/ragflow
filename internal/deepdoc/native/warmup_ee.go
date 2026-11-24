//go:build cgo && ee

package native

// warmup_ee.go — EE startup warmup (GPU and CPU EE). Creates one session per model through the
// same pool getters the HTTP handlers use, so a post-restart request burst finds
// warm sessions instead of triggering 17 concurrent session Initialize() calls
// (which OOM the device and cause a cluster of 500s).

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"go.uber.org/zap"
	"ragflow/internal/common"
	doctype "ragflow/internal/deepdoc/parser/type"
)

// WarmupGPU pre-populates the session pools for the four models while the
// backpressure gate is engaged (set by the caller). Session creation is serial
// here, so device memory stays bounded. Safe no-op when called with an empty
// model dir or on non-GPU paths.
func WarmupGPU(modelDir string) {
	if modelDir == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	type job struct {
		name string
		fn   func() error
	}
	jobs := []job{
		{"dla", func() error {
			path := filepath.Join(modelDir, layoutModel())
			_, release, err := getModelSession(ctx, path, "images", []int64{1, 3, 1024, 1024}, "output0")
			if err == nil {
				release()
			}
			return err
		}},
		{"tsr", func() error {
			path := filepath.Join(modelDir, tsrModel())
			_, release, err := getModelSession(ctx, path, "images", []int64{1, 3, 640, 640}, "output0")
			if err == nil {
				release()
			}
			return err
		}},
		{"ocr_det", func() error {
			path := filepath.Join(modelDir, detModel())
			_, release, err := getDetSession(ctx, path, 640, 640)
			if err == nil {
				release()
			}
			return err
		}},
		{"ocr_rec", func() error {
			path := filepath.Join(modelDir, recModel())
			_, release, err := getRecSession(ctx, path, "x", []int64{1, 3, 48, 640}, "softmax_11.tmp_0")
			if err == nil {
				release()
			}
			return err
		}},
	}

	// Gate new requests during serial warmup so a burst cannot race the session
	// creation and OOM the device.
	doctype.SetBackpressure(true)
	defer doctype.SetBackpressure(false)
	common.Info("EE warmup: engaged backpressure for serial session creation")

	for _, j := range jobs {
		start := time.Now()
		if err := j.fn(); err != nil {
			common.Warn("EE warmup: session create failed",
				zap.String("model", j.name), zap.Error(err))
		} else {
			common.Info("EE warmup: session ready",
				zap.String("model", j.name),
				zap.Int64("elapsed_ms", time.Since(start).Milliseconds()))
		}
	}
	_ = os.Getenv // keep os import used if env checks added later
}
