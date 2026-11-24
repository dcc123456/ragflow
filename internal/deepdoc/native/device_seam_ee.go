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

package native

// EE-only implementation of the device seam: CUDA EP selection, provider
// options, and dynamic EP plugin loading. Implements the four edition hooks
// required by device_seam.go: normalizeDevice, parseDeviceHook, applyDeviceHook,
// registerEPHook.

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	ort "github.com/infiniflow/onnxruntime_go"
)

const (
	cudaEPName      = "CUDA"
	cudaEPLibrary   = "libonnxruntime_providers_cuda.so"
	cudaHostLibrary = "libonnxruntime_providers_shared.so"
)

var (
	epOnce sync.Once
	epErr  error
)

// gpuAvailable reports that this EE build supports GPU inference.
func gpuAvailable() bool { return true }

// normalizeDevice keeps a valid GPU policy as-is; otherwise degrades to CPU.
func normalizeDevice(cfg DeviceConfig) DeviceConfig {
	if !gpuAvailable() || !cfg.UseGPU {
		return DeviceConfig{}
	}
	if cfg.DeviceID < 0 {
		cfg.DeviceID = 0
	}
	if cfg.MemLimitMB < 0 {
		cfg.MemLimitMB = 0
	}
	return cfg
}

// parseDeviceHook accepts cpu, gpu, gpu:N (N>=0). A typo fails at startup
// rather than silently running on CPU.
func parseDeviceHook(spec string) (DeviceConfig, error) {
	s := strings.ToLower(strings.TrimSpace(spec))
	if s == "" || s == "cpu" {
		return DeviceConfig{}, nil
	}
	if s == "gpu" {
		return DeviceConfig{UseGPU: true}, nil
	}
	if strings.HasPrefix(s, "gpu:") {
		idStr := strings.TrimSpace(strings.TrimPrefix(s, "gpu:"))
		id, err := strconv.Atoi(idStr)
		if err != nil || id < 0 {
			return DeviceConfig{}, fmt.Errorf("invalid deepdoc device %q: gpu id must be a non-negative integer", spec)
		}
		return DeviceConfig{UseGPU: true, DeviceID: id}, nil
	}
	return DeviceConfig{}, fmt.Errorf("invalid deepdoc device %q: expected cpu, gpu, or gpu:N", spec)
}

// applyDeviceHook appends the CUDA EP to per-session options when GPU is
// selected. A CPU-linked binary (no real provider loader) fails here with a
// clear error — no silent CPU fallback.
func applyDeviceHook(opts *ort.SessionOptions) error {
	dev := getDevice()
	if !dev.UseGPU {
		return nil
	}
	cudaOpts, err := ort.NewCUDAProviderOptions()
	if err != nil {
		return fmt.Errorf("create CUDA provider options: %w", err)
	}
	cfg := map[string]string{"device_id": strconv.Itoa(dev.DeviceID)}
	if dev.MemLimitMB > 0 {
		cfg["gpu_mem_limit"] = strconv.FormatInt(int64(dev.MemLimitMB)*1024*1024, 10)
	}
	if err := cudaOpts.Update(cfg); err != nil {
		cudaOpts.Destroy()
		return fmt.Errorf("configure CUDA provider: %w", err)
	}
	if err := opts.AppendExecutionProviderCUDA(cudaOpts); err != nil {
		cudaOpts.Destroy()
		return fmt.Errorf("append CUDA execution provider for %s: %w (was the binary linked with the full CPU lib + provider .so?)", dev, err)
	}
	cudaOpts.Destroy()
	// Fail session creation if CUDA cannot cover every node in the graph.
	// Without this, ORT silently falls unsupported nodes back to the CPU EP,
	// so a model can appear healthy while part of its inference runs on CPU.
	// Setting the entry makes the GPU backend all-or-nothing: any CPU fallback
	// becomes a startup/run error instead of a silent slowdown.
	if err := opts.AddSessionConfigEntry("session.disable_cpu_ep_fallback", "1"); err != nil {
		return fmt.Errorf("disable CPU EP fallback: %w", err)
	}
	return nil
}

// registerEPHook loads the CUDA EP plugin .so into ORT's process-global env.
// Must run after InitORT, before any session. Idempotent.
func registerEPHook() error {
	epOnce.Do(func() {
		dev := getDevice()
		if !dev.UseGPU {
			return
		}
		libPath := dev.EPLibraryPath
		if libPath == "" {
			exe, err := os.Executable()
			if err != nil {
				epErr = fmt.Errorf("resolve executable path for CUDA EP: %w", err)
				return
			}
			libPath = filepath.Join(filepath.Dir(exe), cudaEPLibrary)
		}
		if _, err := os.Stat(libPath); err != nil {
			epErr = fmt.Errorf("CUDA EP library %q not found: %w (place %s and %s next to the binary, or set the EP library path)",
				libPath, err, cudaEPLibrary, cudaHostLibrary)
			return
		}
		if err := ort.RegisterExecutionProviderLibrary(cudaEPName, libPath); err != nil {
			epErr = fmt.Errorf("register CUDA EP library %q: %w", libPath, err)
		}
	})
	return epErr
}
