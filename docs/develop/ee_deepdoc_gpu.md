# EE DeepDoc GPU backend (CUDA EP)

EE-only. The OSS tree builds CPU-only; GPU support is opt-in through EE build/runtime flags.

## Why a dynamic provider

ORT 1.29's CUDA EP must be loaded as a runtime plugin. Statically linking the CUDA
kernels into the binary crashes before `main()` (the provider-bridge global
constructor dereferences an un-injected `ProviderHost`). Instead, the Go binary
links the **full CPU static lib** (which exports `RegisterExecutionProviderLibrary`)
and ORT `dlopen`s two plugins at runtime:

- `libonnxruntime_providers_shared.so` — host shim (`Provider_SetHost`)
- `libonnxruntime_providers_cuda.so` — CUDA kernels (`CreateEpFactories`)

`RegisterExecutionProviderLibrary` resolves `providers_shared.so` from the binary's
own directory (`GetRuntimePath()`), so both `.so` files must sit next to
`ragflow_server`.

`github.com/infiniflow/onnxruntime_go@v1.29.1` provides the Go wrappers for
`RegisterExecutionProviderLibrary` and `SessionGetInitializerOnDevice`. The latter
is a custom ORT API used for zero-copy weight sharing and is compiled only under
the `cuda && static` build tags.

## Build

| Variant | `DEEPDOC_ORT_VARIANT` | Static lib | GPU |
|---|---|---|---|
| OSS / CPU / desktop | unset (`cpu`) | minimal 48 MB (`...-glibc2_28`) | no |
| EE GPU | `gpu` | full 124 MB (`...-full`) + 2 plugins | yes |

The split is size-driven (desktop cannot absorb +76 MB); CPU/GPU output parity is
verified, so this does not change correctness.

`ragflow-build` publishes `onnxruntime-v1.29.0-gpu-linux-x86_64.zip` containing
`lib/libonnxruntime.a` (full), `lib/libonnxruntime_providers_cuda.so`,
`lib/libonnxruntime_providers_shared.so`, and `include/`.

```bash
docker build -f Dockerfile_deepdoc_gpu_ee -t deepdoc_gpu_ee:latest .
docker run --gpus all -p 8001:8001 -e EE_MODEL_DIR=/ragflow/models deepdoc_gpu_ee
```

The production stage is based on `nvidia/cuda:12.8.0-cudnn-runtime-ubuntu24.04`
(cudart / cublas / cudnn; GLIBC 2.38 requires Ubuntu 24.04). Both plugins are
copied to `/ragflow/bin/`, next to `ragflow_server`.

## Runtime device selection

- `DEEPDOC_DEVICE=cpu|gpu[:id]` (default `cpu`)
- CLI `--deepdoc-device` overrides the env
- `DEEPDOC_GPU_MEM_LIMIT_MB` sets the per-session `gpu_mem_limit` (optional)
- CPU mode loads no `.so`; GPU mode `dlopen`s the plugins
- GPU configured with a minimal lib or missing `.so` -> fail-fast, no silent CPU fallback

## Weights

GPU weights must be **de-fused**: the CUDA EP cannot execute
`com.microsoft.FusedConv`, the CPU-only fused Conv+HardSigmoid kernel that ORT's
default Fixed optimization emits. Export with `--optimization_style Runtime`,
which keeps Conv and HardSigmoid as separate primitives
(`<stem>.with_runtime_opt.ort`).

They share `deepdoc_ee_models/` with the CPU set but use a different suffix:
`det.ee.gpu.ort`, `layout.ee.gpu.ort`, `tsr.ee.gpu.ort`, `rec.ee.gpu.ort`. The
suffix is a compile-time constant (`model_files_*_ee.go`), so a GPU binary cannot
load CPU weights and vice versa. `ocr.res` is device-independent and shared.

Weights are **never downloaded at build time**. CI stages them into the build
context; local developers use the prebuilt images. Verify a staging directory
before a build:

```bash
sha256sum -c tools/scripts/GPU_WEIGHTS_SHA256.txt
```

Locked hashes (ORT 1.29.0, Runtime style, generated 2026-11-17; a mismatch is fatal):

| file | sha256 |
|---|---|
| `det.ee.gpu.ort` | `68f91d655d9ed63489daefb9361008a58626ae8a96ff01f9196132ed02241eb1` |
| `rec.ee.gpu.ort` | `42ae8a07db3acfa92c9307e487f8ebed48d91bce309ed0c545c9e7ca07018fae` |
| `layout.ee.gpu.ort` | `eda5fa990e8cb9e5fad5568cf2bc31c869d9f0ad21910a3b1c50a306f294d7ee` |
| `tsr.ee.gpu.ort` | `481d2673df5f558f6a6326aaab99aaae13d8f099e476e32f6abe2a7e494bb156` |

To regenerate, run inside the EE Python docker (paddle2onnx 1.3.1, ONNX Runtime
1.29.0):

```bash
python tools/scripts/convert_deepdoc_model.py \
  --input ee_deepdoc_models/tsr/tsr_op17.onnx \
  --out-dir ee_deepdoc_models/ --opset 17 --gpu
```

The script calls `convert_onnx_models_to_ort --optimization_style Runtime`,
writes `<stem>.ee.gpu.ort`, and sanity-checks that the result loads on the CPU EP.
Note: the `--optimization_level disable` flag mentioned in older handoff notes
does not exist in ORT 1.29.

## Tests

- `device_config_ee_test.go` — device parsing, no GPU required
- `device_gpu_integration_ee_test.go` (`//go:build cgo && integration`) — run on a GPU host:

```bash
MODEL_DIR=<de-fused weights> DEEPDOC_DEVICE=gpu RAGFLOW_GPU_TEST=1 \
  go test -tags 'cgo static ee integration' -run TestGPUDevice -v ./internal/deepdoc/native/
```

## Troubleshooting

- `cannot open shared object file: libonnxruntime_providers_cuda.so` — the plugins must be next to the binary.
- `RegisterExecutionProviderLibrary is not supported in a minimal build` — GPU requires the full static lib.
- `unsupported conv activation mode "HardSigmoid"` — fused CPU weights on the CUDA EP; re-export with `--gpu`.
- `cudaGetDeviceProperties_v2` undefined — CUDA 13 runtime with a `.so` built against 12.8; use CUDA 12.8 runtime libraries.
