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

package deepdocserver

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"go.uber.org/zap"

	"ragflow/internal/common"
	"ragflow/internal/deepdoc/native"
	infnative "ragflow/internal/deepdoc/parser/pdf/inference/native_analyzer"
	doctype "ragflow/internal/deepdoc/parser/type"
)

// DefaultPort is the listen port for the DeepDoc inference server.
const DefaultPort = 8001

// Standalone resource limits. The --deepdoc server does not go through the
// API server's RegisterNativeDeepDoc, so it must install its own inference
// gate and (on GPU) a device-memory budget. Both are soft limits: the gate
// queues excess Runs, and gpu_mem_limit makes ORT return a recoverable error
// and self-reclaim instead of hard-OOMing the CUDA context.
const (
	envStandaloneConcurrency = "RAGFLOW_DEEPDOC_INFERENCE_CONCURRENCY"
	envGPUMemLimitMB         = "RAGFLOW_DEEPDOC_GPU_MEM_LIMIT_MB"
	defaultStandaloneK       = 8    // conservative floor when env is unset
	defaultGPUMemRatio       = 0.85 // use at most 85% of detected VRAM
)

// resolveStandaloneK reads the concurrency gate from env (same key the API
// server uses) with a conservative default. It never returns < 1.
func resolveStandaloneK() int {
	raw := strings.TrimSpace(os.Getenv(envStandaloneConcurrency))
	if raw == "" {
		return defaultStandaloneK
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 {
		common.Warn("invalid standalone inference concurrency, using default",
			zap.String("raw", raw), zap.Int("default", defaultStandaloneK))
		return defaultStandaloneK
	}
	return n
}

// detectTotalVRAMMB returns total GPU VRAM in MiB via nvidia-smi. Returns 0
// when unavailable (non-GPU host, driver missing) so the caller can skip the
// memory budget. Called only on the GPU path.
func detectTotalVRAMMB() int {
	out, err := exec.Command("nvidia-smi", "--query-gpu=memory.total",
		"--format=csv,noheader,nounits").Output()
	if err != nil {
		return 0
	}
	fields := strings.Fields(string(out))
	if len(fields) == 0 {
		return 0
	}
	mb, err := strconv.Atoi(strings.TrimSpace(fields[0]))
	if err != nil || mb <= 0 {
		return 0
	}
	return mb
}

// resolveGPUMemLimitMB computes the device memory budget: an explicit env
// value wins, otherwise total VRAM × ratio. 0 means "no explicit limit".
func resolveGPUMemLimitMB() int {
	if raw := strings.TrimSpace(os.Getenv(envGPUMemLimitMB)); raw != "" {
		if mb, err := strconv.Atoi(raw); err == nil && mb > 0 {
			return mb
		}
		common.Warn("invalid GPU mem limit env, falling back to ratio", zap.String("raw", raw))
	}
	total := detectTotalVRAMMB()
	if total <= 0 {
		return 0
	}
	return int(float64(total) * defaultGPUMemRatio)
}

// RunServer boots the EE DeepDoc HTTP inference server: it switches the
// in-process DeepDoc backend to the EE weights and post-processors (before any
// inference), then serves /predict/{dla,tsr,ocr_det,ocr_rec} and /health. A nil
// or non-positive port keeps DefaultPort.
//
// The DeepDoc inference server is intentionally standalone: it only needs the
// statically-linked ONNX Runtime and MODEL_DIR, never the full RAGFlow
// infrastructure (DB/engine/cache/storage/MQ).
func RunServer(ctx context.Context, serverName string, port *int, ready func() error) error {
	listenPort := DefaultPort
	if port != nil && *port > 0 {
		listenPort = *port
	}

	native.InitCPUEE()
	infnative.InitCPUEE()

	// The standalone --deepdoc server must configure the device itself: unlike
	// the API server's RegisterNativeDeepDoc, nothing else in this code path
	// parses DEEPDOC_DEVICE or sets the process-wide device policy. Without this,
	// a GPU-linked binary would silently run every session on CPU.
	deviceSpec := strings.TrimSpace(os.Getenv("DEEPDOC_DEVICE"))
	if deviceSpec == "" {
		deviceSpec = "cpu"
	}
	dev, err := native.ParseDevice(deviceSpec)
	if err != nil {
		return fmt.Errorf("invalid DeepDoc device: %w", err)
	}

	// Resource gate #1 (application layer): cap concurrent inference Runs. The
	// process-wide limiter is shared by all four models (dla/tsr/ocr_det/ocr_rec).
	// On GPU the K is derived from runtime-detected hardware so it scales.
	k := resolveStandaloneK()
	if dev.UseGPU && os.Getenv("RAGFLOW_DEEPDOC_K_MODE") != "fixed" {
		if ak := native.AdaptiveInferenceConcurrency(defaultGPUMemRatio); ak > 0 {
			k = ak
		}
	}
	doctype.SetInferenceLimit(k)
	doctype.SetLocalInferenceLimit(k)
	native.SetIntraOpThreads(1)
	common.Info("standalone DeepDoc inference gate registered",
		zap.Int("max_concurrent_inference", k))

	// Resource gate #2 (device layer): on GPU, cap ORT's BFC arena to a fraction
	// of detected VRAM (recoverable error instead of hard CUDA OOM).
	if dev.UseGPU {
		if mb := resolveGPUMemLimitMB(); mb > 0 {
			dev.MemLimitMB = mb
			common.Info("standalone DeepDoc GPU memory budget registered",
				zap.Int("mem_limit_mb", mb))
		} else {
			common.Warn("GPU memory budget unavailable (nvidia-smi not found); relying on inference gate only")
		}
	}
	native.SetDevice(dev)

	// GPU adaptive idle-session budget (idle LRU/TTL + high-water backpressure).
	if dev.UseGPU {
		native.InitGPUVramLedger(ctx)
	}

	common.Info(fmt.Sprintf("Starting EE DeepDoc inference server %s on port %d (device %s)", serverName, listenPort, dev))
	return run(ctx, listenPort, ready)
}
