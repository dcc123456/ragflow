# EE DeepDoc Native Inference Migration — Equivalence (Parity) Conclusions and Deployment Notes

> Scope: Migrating the RAGFlow Enterprise Edition (EE) Python deepdoc inference service into a standalone Go binary.
> This document records the wrap-up conclusions for **B1 (EE 2-class TSR validation landing)** and **B2 (DLA / OCR parity verification)**.
> All parity numbers come from real runs of `TestEquivalenceReport` / `TestTSREEParity` (CGO + ORT static library).
> ⚠️ The **OCR row** was measured on the OSS `rec.ort` (`a6eb83a8…`) under the **minimal** ORT library; the EE deployment uses the **EE `rec.ee.cpu.ort` (`e3625db2…`)**, whose HardSwish was decomposed to `Mul/Clip/Div` so the **minimal ORT** runs it without a custom kernel. The EE backend links the ORT library patched for `SessionGetInitializer` (required by `sharedWeights()` in `session.go`) — that patching is for the initializer API, not HardSwish (see §4). Its OCR parity is noted at the end of §3.

> 📌 **Model (re)generation / `.ort` conversion pipeline** has its own authoritative
> manual: [**`ee_deepdoc_model_conversion.md`**](ee_deepdoc_model_conversion.md).
> Since **2026-10-15** the EE backend loads `.ee.cpu.ort` through the **minimal** ORT
> build (not `-full`, not raw `.onnx`) — the conversion scripts there are the only way
> to reproduce the four `*.ee.cpu.ort` weights. The EE backend still links the ORT build
> patched for `SessionGetInitializer` (used by `sharedWeights()`); because HardSwish is
> decomposed, the reduced/minimal op set is sufficient and the `-full` build is no longer
> required (see §4, reconciled to the minimal-ORT state).

---

## 1. EE 2-class TSR vs OSS 6-class TSR (key concept)

EE does **not** use the open-source 6-class PP-Det `TableStructureRecognizer`. EE replaces it with a self-developed **2-class YOLOv8 separator model** (vertical / horizontal). The two **coexist**: OSS in-process inference (`deepdoc` method) revives the open-source 6-class `RunTSR`; the EE docker (parse method `deepdoc_cpu_ee`, image tagged `deepdoc_ee_cpu`) serves the 2-class `RunTSREE` as an independent HTTP service.

| | Classes | Model file | Call path | Deployment / routing |
|---|---|---|---|---|
| **OSS 6-class TSR** | table / column / row / … (6) | `tsr.ort` | `RunTSR` (`tsr.go`) | **OSS in-process** (`ragflow_server` + `deepdoc` / `DeepDOC` parse methods); `NativeAnalyzer.TSR` defaults `tsrImpl=RunTSR` |
| **EE 2-class TSR** | `v` (vertical) / `h` (horizontal) (2) | `tsr.ee.cpu.ort` | `RunTSREE` (`tsr_ee.go`) | **EE docker** `deepdoc_cpu_ee` parse method → `eeclient` → `DEEPDOC_CPU_EE_URL` → HTTP `/predict/tsr` |

> OSS uses `tsr.ort` (6-class), EE uses `tsr.ee.cpu.ort` (2-class); the **`.ee.cpu` suffix marks the EE deployment identity** (both are `.ort` FlatBuffers, so the format is the same — only the name differs), isolated naturally by the deployed model directory: the OSS process holds 6-class weights, the EE image holds 2-class weights. The OSS 6-class `RunTSR` has been **revived** for the OSS in-process path (it was retired when EE switched to 2-class) and is no longer treated as dead code; EE `/predict/tsr` still goes through `RunTSREE`.
>
> `NativeAnalyzer.TSR` selects the decoder via the `tsrImpl` injection point: its function signature returns `native.TSRResult`, whereas `RunTSREE` returns `native.TSREEResult`, so the compiler **forbids** mistakenly wiring the EE decoder into the OSS in-process analyzer (EE uses its own HTTP handler to inject `RunTSREE`).

EE Python service (`deepdoc/servers/tsr/tsr_svr.py`) output format:

```json
{"bboxes": [[x0, y0, x1, y1, score, cls], ...]}   // cls ∈ {0:"v", 1:"h"}
```

`RunTSREE` faithfully replicates this format (see the header comment in `tsr_ee.go`). The OSS 6-class `RunTSR` outputs the same-shaped `{"bboxes":[[x0,y0,x1,y1,score,cls]...]}` (cls ∈ 0..5), serialized via `native.TSRResult.Wire()`.

---

## 2. Model filename conventions (EE TSR: `tsr.ee.cpu.ort`)

The EE 2-class TSR weights use the canonical filename **`tsr.ee.cpu.ort`** (the `.ee.cpu` deployment suffix marks the EE identity; the `.ort` FlatBuffer is the same on-disk format as the OSS `*.ort`, only the name differs). The **actual content** of `tsr.ee.cpu.ort` is deployment-determined: the **OSS in-process** path holds 6-class PP-Det weights (the revived `RunTSR`, file `tsr.ort`), while the **EE docker** holds 2-class YOLOv8 separator weights (serialized from `ee_deepdoc_models/tsr/tsr_op17.onnx`, opset17, 272 MB, via the pipeline in `ee_deepdoc_model_conversion.md`). The two are isolated naturally by their respective model directories, and the decoders are injected at their own entry points (`NativeAnalyzer.TSR` → `RunTSR`; EE HTTP handler → `RunTSREE`), so they never cross. The minimal ORT library statically linked by the Go backend loads `.ort` exclusively, so the EE TSR consumes `tsr.ee.cpu.ort`; the serialized graph content is byte-equivalent to the golden reference source (`tsr_op17.onnx`).

**EE four-model unified `*.ee.cpu.ort` (2026-10-14):** The EE docker's `MODEL_DIR` is now `det.ee.cpu.ort` / `rec.ee.cpu.ort` / `layout.ee.cpu.ort` / `tsr.ee.cpu.ort` / `ocr.res`. All were switched to this naming only after numerical-equivalence cross-checks against the old `.ort`/`.onnx` versions using the same ORT 1.29.0 (rec bit-identical, det max 7e-05, layout valid-box max 6e-08; post-switch docker parity 33/34 is identical to pre-switch). The `.ee.cpu.ort` suffix both marks the EE deployment identity and creates a literal distinction from the OSS `*.ort`. The EE in-process backend switches to these filenames at runtime via `InitCPUEE()` (see §5); the OSS default filenames are left untouched. The old `.ort` files were moved to `deepdoc_ee_models/.superseded-20261014/` as a backup, not deleted.

**Deployment conventions (parity cutover checklist):**

1. **OSS in-process** (`ragflow_server`, DSL `deepdoc` / `DeepDOC`): `DEEPDOC_MODEL_DIR` holds 6-class weights, including:
   | File | Content | Locked hash (sha256, drift = FATAL) |
   |---|---|---|
   | `det.ort` | OSS text detection | `fd62614cbae0b073aa203b2393081e641826ecfc85414669458506d91e23c095` |
   | `layout.ort` | DLA layout | `55e3e5743ba2f2a03879578ac818089afa32bdedda51159d340bc2eec968b030` |
   | `tsr.ort` | **OSS 6-class TSR** (`RunTSR`, 6-class PP-Det) | see OSS 6-class snapshot |
   | `rec.ort` | OCR recognition | `a6eb83a8943d2dca6de8e1393420904bddd4dded455d72ca177ade63b56533d0` |
   | `ocr.res` | OCR dictionary | `28b2362ad4ab2dc38769aa72feb535e3a9ddb3fd2a7585a05920e6393b1dc7f7` |
2. **EE docker** (parse method `deepdoc_cpu_ee`): the in-image `MODEL_DIR` holds the EE's own weights, uniformly named **`*.ee.cpu.ort`** (the minimal ORT loads `.ort` directly; the `.onnx` exports are only intermediates produced by the generation scripts — see `ee_deepdoc_model_conversion.md` — and are never loaded at runtime; the `.ee.cpu` naming creates a visible boundary with the OSS `*.ort`, and the two weight sets are intentionally different — do not mix). Here `tsr.ee.cpu.ort` is the **EE 2-class TSR** (`RunTSREE`, serialized from `ee_deepdoc_models/tsr/tsr_op17.onnx`, opset17, 272 MB)
   | File | Content | Locked hash (sha256, drift = FATAL) |
   |---|---|---|
   | `tsr.ee.cpu.ort` | **EE 2-class TSR** (serialized from `ee_deepdoc_models/tsr/tsr_op17.onnx`, opset17, 272 MB) | `f150cc930f56190ac26285e8433e240f94f2cdae3e80bbe227ee6a2ab3955a4b` |
   | `rec.ee.cpu.ort` | **EE OCR recognition** (`RunOCRRec`): exported from EE Paddle `ch_PP-OCRv4_rec_infer` via `paddle2onnx` (1.3.1), opset14, then HardSwish **decomposed to `Mul/Clip/Div`** (28 sites) so the minimal ORT runs it; semantically aligned with the EE Python OCR service. **Not** the OSS `rec.ort` | `e3625db28983790f2e43539d293d4b8bdd1f4fd93f26b3453cc5d77275c5668d` |
   | `det.ee.cpu.ort` | **EE text detection** (`RunDet`): exported from EE Paddle `ch_PP-OCRv4_det_infer` via `paddle2onnx` (1.3.1), opset14, then HardSwish decomposed to `Mul/Clip/Div` (24 sites). **Not** the same file as OSS `det.ort` (EE/OSS are two independent weight sets, intentionally different — do not align to OSS) | `6f5da8e7105d5f00034db9cd9529e35c142d98074ce8c1df6d34a00cbd6d184b` |
   | `layout.ee.cpu.ort` | **EE layout (DLA)** (`RunDLA`): ultralytics YOLOv10 ONNX export (`doclayout_yolo_docstructbench_imgsz1024.pt`), serialized to `.ort`. **Not** the same file as OSS `layout.ort` (EE/OSS are two independent weight sets, intentionally different — do not align to OSS) | `471600eb10d58655e28788cb08756c98987d4159e9ad6bf14d3d3c40a5a30629` |
   | `ocr.res` | OCR dictionary | `28b2362ad4ab2dc38769aa72feb535e3a9ddb3fd2a7585a05920e6393b1dc7f7` |

   > ⚠️ The EE `rec.ee.cpu.ort` previously shipped HardSwish operators; those are now **decomposed to `Mul/Clip/Div`** by `decompose_hardswish.py`, so the minimal ORT runs it. The Go backend's `sharedWeights()` still calls the infiniflow-patched `SessionGetInitializer`, so the EE docker must link the **infiniflow-patched** ORT library (see §4) — but the patching is for the initializer API, not HardSwish, and the **minimal** patched build now suffices (the `-full` build is no longer required).
   >
   > **The EE four weight files and the OSS same-named files are intentionally two different weight sets — there is no need, and you should not, align them with OSS.** The locked hashes above are the actual EE weight hashes; lock them to the real values before shipping; do not change weights under the mistaken expectation that "an independent re-export looks suspicious and must be aligned to OSS".
   >
   > **2026-10-14 serialization switch record**: EE weights are serialized to `.ee.cpu.ort` (FlatBuffer) with the unified `*.ee.cpu.ort` naming; the paddle2onnx/YOLOv10 `.onnx` exports are only intermediates produced by the generation scripts (see `ee_deepdoc_model_conversion.md`), never loaded at runtime. Before the `.ort` switch, the same ORT 1.29.0 was used for numerical-equivalence cross-checks (rec bit-identical, det max 7.3e-05, layout valid-box max 6.0e-08); the docker-vs-docker parity is identical to pre-switch (DLA 11/11, TSR 300=300, OCR-rec 11/11, OCR-det 10/11, the same known 1↔2 difference). The ONNX→`.ort` converter (`convert_deepdoc_model.py`) is OSS-origin tooling kept in the OSS repo (inherited by EE via the OSS→EE merge), not added by this PR. The historical `.ort` files are backed up in `deepdoc_ee_models/.superseded-20261014/`.
3. `deepdoc_cpu_ee` routing (configured via `parse_method: deepdoc_cpu_ee`; the gpu/ascend variants are `deepdoc_gpu_ee` / `deepdoc_ascend_ee`): the PDF parser selects the backend via saas's generic external-analyzer dispatch — `internal/parser/parser/pdf_deepdoc_dispatch_ee.go` registers each suffixed method against `client` in its `init()` via a shared loop, and `resolveDocAnalyzerForMethod` (an OSS seam) routes the method to that factory, reading the endpoint from the `<METHOD>_URL` environment variable (`externalAnalyzerURL("deepdoc_cpu_ee")` → `DEEPDOC_CPU_EE_URL`). If the method's `<METHOD>_URL` is unset, parsing errors explicitly (**not** silently degrading to empty results); the other methods (`""` / `deepdoc` / `DeepDOC`) go through the OSS in-process path. The EE DeepDoc inference server is the `--deepdoc` mode of the single `ragflow_server` binary: `cmd/ragflow_server.go`'s `main()` has an early-return branch that, for the deepdoc modes, calls `runDeepDoc` (in `cmd/deepdoc_server_ee.go`), which calls `native.InitCPUEE()` / `infnative.InitCPUEE()` at startup to load the EE weights. There is deliberately no un-suffixed `deepdoc_ee` / `DeepDoc_EE` alias and no `DEEPDOC_EE_URL`; the UI selects an explicit hardware variant and the parser only speaks suffixed methods (the `SetDeepDocEEURL` test helper was removed).
4. `tsr.ee.cpu.ort` is the serialized form of the EE service model `ee_deepdoc_models/tsr/tsr_op17.onnx` (opset17, 272 MB) — `export_tsr_ee_onnx.py` produces the `.onnx`, then it is serialized to `.ort` (the runtime loads `.ort`, not `.onnx`; the two share byte-equivalent graph content). To rebuild from a new `.pt`, use `tools/scripts/export_tsr_ee_onnx.py` (ultralytics export opset11 → version_converter upgrade to opset17), then serialize with `convert_deepdoc_model.py` per `ee_deepdoc_model_conversion.md`.
5. The reduced production ORT static library only supports newer ops, so the **OSS-side `.onnx` must be upgraded to opset17 before converting to `.ort`**:
   - DLA `layout.onnx` is already opset17, convert directly;
   - OCR `det.onnx` (opset11) / `rec.onnx` (opset10) need `version_converter.convert_version(., 17)` upgrade;
   - The OSS `.ort` weights are provided by the upstream `InfiniFlow/deepdoc` snapshot (fetched by `ragflow_deps/download_deps.py`); the EE side serializes its own weights to `*.ee.cpu.ort` via the conversion scripts in `ee_deepdoc_model_conversion.md` (the converter itself lives in OSS and is inherited via the OSS→EE merge).
   - **EE `rec.ort` exception**: it is the EE Paddle export via `paddle2onnx` (1.3.1) at **opset14** (with 28 HardSwish), **not** upgraded to opset17, and **must** be loaded with the infiniflow patched **non-reduced** library (the reduced library lacks HardSwish and fails). The `pst → onnx → ort` conversion is fixed, with no model drift (§2.2 already verified `tsr.ort` with the same conclusion).

---

## 3. Parity summary (`TestEquivalenceReport`, measured)

Environment: `MODEL_DIR=/tmp/eemodel_ee`, CGO + reduced ORT static library (48 MB, `v1.29.0-2da6c6ac8165a16e`).
> Note: the **OCR row (11/11) in this parity was measured on the OSS `rec.ort` (`a6eb83a8…`) under the minimal ORT library**. The EE deployment uses **EE `rec.ee.cpu.ort` (`e3625db2…`)** with HardSwish decomposed to `Mul/Clip/Div`, so it runs on the **minimal ORT** (no HardSwish kernel needed; the infiniflow patched library is still required only for the `SessionGetInitializer` API, see §4). OCR parity was re-verified against the same EE goldens after decomposition — differences vs the native-HardSwish golden are at ~1e-6 (float rounding), far below CTC/box thresholds, so the 11/11 exact-text result is preserved.

```
TASK                   MATCH/TOT  MAXΔ(px)  NOTE
DLA (layout)            148/148    0.129    sub-pixel
TSR (OSS, retired in EE) 190/190   2.155    <1px (<=10px @4:1 aspect)
TSR-EE (2-class)        298/300    1.010    298/300: 2 boxes ~40px (PNG fixture rules out JPG-decode diff; root cause = Go bilinearResize vs cv2.INTER_LINEAR, <=1 gray level; the 2 mismatches are NMS-IMPLEMENTATION-layer divergence (Go per-class iou=0.7 vs ultralytics) tipped by that <=1-level input diff — see L71+); **human-reviewed 2026-09-29: both divergent boxes are valid ambiguous cells, accepted as known-tolerable**
OCR (text)               11/11     0.000    exact text
Det (text boxes)        622/625    0.000    IoU orphan(gold/go)=3/5 (accepted floor 3/5)
```

Appendix: `TestTSREEParity` (standalone EE 2-class fixture test) = **298/300, max coord diff 1.010 px, unmatched=2**.

### Attribution of the 2 boxes in 298/300 (**prior errors corrected**)

> ⚠️ The early erroneous conclusion "golden comes from `.pt`, Go runs onnx, PyTorch→ONNX export drift" is **wrong** and was experimentally falsified.

Evidence chain:
1. After re-sampling the golden from the **same opset-17 ONNX** (the then-numpy-replica script reproduced `tsr_svr.py`'s CPU decode path; that script is deleted, the authoritative collector is `tools/scripts/collect_ee_docker_golden.py`), Go was still 298/300 with the same 2 boxes → rules out `.pt↔onnx` drift;
2. Using **onnxruntime + a line-by-line replica of Go's decode/NMS** (only letterbox uses cv2) yielded **300/300** → proves the Go decode logic itself is correct;
3. Go's own `bilinearResize` (`geometry.go`) vs cv2.INTER_LINEAR differs by **at most 1 gray level** byte-for-byte (measured max=1, mean=0.02), which is the **input-side source** of these 2 boxes' difference.

**PNG isolation experiment (2026-09-29, elevating the root cause from "inferred" to "proven")**: switched the fixture from JPG to PNG (`tsr_ee_table.png`). Go decodes with the standard-library `image/png` (not `image/jpeg`), and the EE service decodes with `cv2/libpng`; for lossless PNG the two are **bit-for-bit identical**, thereby eliminating another genuinely-present preprocessing difference — **Go stdlib `image/jpeg` vs libjpeg** — leaving only the resize kernel. The re-run result was **still 298/300, the same 2 boxes, max diff 1.010**. This **locks the root cause as the resize kernel (B)**, ruling out the JPG-decode difference (A). Thus "≤1 gray level" is no longer just a theoretical value measured on cv2-decoded data, but the **measured difference of the real full pipeline** (under PNG, A=0, so the pipeline difference is exactly B).

**B is an inherent property of the Go native port (proven, 2026-09-29)**: using the **verbatim-copied** `bilinearResize` from `geometry.go` (pure Go, standalone program, not a Python replica) to resize a stress corpus and comparing byte-for-byte with cv2.INTER_LINEAR — the corpus = pure noise / 1px stripes / 8px checkerboard / diagonal edges / gradients (worst-case high-contrast synthetic images) + 2 real images, each image × 10 target sizes (including TSR 640 / DLA 1024 / OCR-rec 32-high / Det 736) = **70 resizes**. Result: **global max diff = 1 gray level, and 0 of the 70 exceed 1** (GLOBAL MAX ABS DIFF=1, GLOBAL MEAN=0.0645). Even on the most extreme high-frequency synthetic images the peak is exactly 1. This proves that the difference between Go `bilinearResize` and cv2.INTER_LINEAR **structurally exists for any input and is strictly bounded at ≤1 gray level** — it is a property of the same Go implementation shared by all recognizers (DLA/Det/OCR/OSS-TSR/EE-TSR), so "B ≤1 gray level" is an inherent property of the Go native port, not EE-specific or an occasional artifact of a particular image.

**Mechanism of the 2-box flip (proven, controlled causal experiment, 2026-09-29, overturning the old "DFL→NMS ~40px candidate amplification" story)**: the prior claim "those 2 boxes are DFL→NMS being ambiguous between two candidates ~40px apart and flipped by the ≤1-level difference" was a **misreading**, falsified by a clean causal experiment. Experiment method: the same **Go-style NMS decode** (`mechanism2.py`, already validated by first reproducing 298/300 on the go-blob to confirm trustworthiness) fed two versions of the 640 blob — the pixels decoded by cv2 (cv2 blob) and the pixels decoded by Go (go blob, the two differ only by ≤1 level per pixel):

```
Go-NMS(cv2 pixels)  vs golden : 300/300
Go-NMS(go  pixels)  vs golden : 298/300      ← only the ≤1-level input pixel difference causes these 2 boxes to flip
CLEAN CAUSAL: same decode same NMS, only ≤1-level input pixel difference → 2 flip = True
```

Key counterintuitive finding (using empirical matching to back-derive the real 640 coordinates): golden#298/#299's **best-matching candidate is the same** in the **cv2 raw output** and the **go raw output** (idx 6961/3504, score 0.893/0.884), while their IoU with golden is **only 0.043/0.046**. This means:
- cv2 and go's **raw model output at the 2 locations is nearly identical** (the difference is only from the ≤1-level input pixels, far from "two candidates 40px apart");
- the 2 boxes' "mismatch" is caused by the **NMS implementation-layer difference**: Go's per-class `iou=0.7` NMS and ultralytics' built-in NMS make different tie-breaking choices at the boundary, and that ≤1-level preprocessing difference exactly **tips** Go's NMS choice, so the box it keeps differs from the box golden (ultralytics NMS) keeps by ~40px.
→ So the "2-box flip" is not "two candidates competing within the same output being amplified", but "**the same input goes through two NMS implementations making different tie-breaking choices, tipped by the ≤1-level input difference**".

**Conclusion**: the difference consists of two parts — (1) **input-side** Go bilinear letterbox vs cv2.INTER_LINEAR ≤1 gray-level inherent difference (B), and (2) **NMS implementation-side** Go per-class iou=0.7 vs ultralytics' different tie-breaking, tipped by (1). Combined, they make Go-NMS keep a different candidate than golden for exactly 2 boxes. The Go inference logic itself is correct (cv2 blob + the same Go-NMS = 300/300), not a model/export problem, nor a JPG-decode difference. Chasing 300/300 would require making Go's letterbox byte-identical to cv2 (fragile, low payoff), which is not done currently; the NMS implementation difference falls in the acceptable "equivalent but not byte-identical" category.

**Human-review overlay images (generated by `tools/scripts/generate_overlay_ee.go`)**: both 2 boxes are **vertical separators (cls=0)**, with x differing by only 1px; the only divergence is **y direction ±41px** (exactly one "row height") — #298 Go is one row higher than golden, #299 Go is one row lower than golden, and both fall in the table's **blank-row region** (truly ambiguous row boundaries). The overlay matches the 300 boxes using the same tolerance as parity (coord 3.5px / score 0.05), highlights golden (red) and the nearest Go box (orange) with a connecting line (yellow), and additionally outputs 2 280px local zoom images for reviewers to visually judge which side is more accurate.

> **⚠️ The 3 overlay PNGs (`ee_tsr_ee_overlay.png` 2161×1335 full table, `_zoom1.png` / `_zoom2.png` local views of the 2 diff points) are intentionally NOT committed** to keep the repo free of binaries — they are fully reproducible on demand from the live Go port + golden with the command below (same CGO/ORT static-linking environment as the parity test, see §4):
> ```
> go run -tags "cgo static" ./tools/scripts/generate_overlay_ee.go -out docs/develop/ee_tsr_ee_overlay.png
> ```
> This regenerates all 3 images into `docs/develop/`.

**Human-review conclusion (2026-09-29)**: the reviewer inspected the 2 divergent boxes (#298, #299) — one by one, via the zoom overlays generated by `tools/scripts/generate_overlay_ee.go` — concluding that the red box (golden) and orange box (Go) **each correspond to a valid cell/separator position, and it is impossible to judge which side is more accurate**. That is, the ground truth at those 2 locations is itself ambiguous (both fall in blank-row bands, differing only by 41px in y = one row height), and both boxes are acceptable. **Thus 298/300 closes as "300/300, where the 2 locations are human-verified known-acceptable deviations at ambiguous positions"**; under the delivery criterion the equivalence holds, with no unexplained residual.

---

## 4. How to re-run parity

```bash
# Method A: build.sh wrapper (auto setup_cgo_env + MODEL_DIR defaults to rag/res/deepdoc)
./build.sh --test -run 'TestEquivalenceReport|TestTSREEParity|TestTSREE|TestModelSnapshotHash' ./internal/deepdoc/native/...

# Method B: manual (needs the infiniflow-patched ORT static library — patched for SessionGetInitializer; the minimal build now suffices because HardSwish is decomposed)
V=~/ragflow-native-libs/onnxruntime/static_lib/onnxruntime-linux-x64-static_lib-1.29.0-glibc2_28  # patched minimal build from ragflow-build
DL="$(pwd)/.cache/ort_dynamic_list.txt"; printf '{\n  OrtGetApiBase;\n};\n' > "$DL"
export MODEL_DIR=/tmp/eemodel_ee CGO_ENABLED=1
export CGO_LDFLAGS="-Wl,--undefined=OrtGetApiBase -Wl,--dynamic-list=$DL $V/libonnxruntime.a -lstdc++ -lm -lpthread -ldl -lrt -lgcc_s -lutil -lc"
go test -tags "cgo static integration fetch_testdata" -count=1 \
  -run 'TestModelSnapshotHash|TestEquivalenceReport|TestTSREEParity|TestTSREE' -v ./internal/deepdoc/native/
```

> ⚠️ **ORT library selection (hard constraint for EE docker)**: Method B's `V` must point to the **infiniflow-patched** ORT static library — i.e. the patched **minimal** build `onnxruntime-linux-x64-static_lib-1.29.0` from `ragflow-build` (patched for `SessionGetInitializer`). Two reasons it must be the *patched* build, not a stock one:
> - The patched build appends 3 custom OrtApi slots `SessionGetInitializerCount/Name/GetInitializer`; the Go backend's `sharedWeights()` calls them **on every model load**. An unpatched stock library (including the upstream full library) lacks these slots; linking passes, but the OrtApi slot misalignment causes a **runtime crash** at the first model load.
> - This constraint is independent of the op set. Previously the EE also needed the **non-reduced (`-full`)** build because `rec.ee.cpu.ort`'s HardSwish had no minimal-ORT kernel; that is **no longer the case** — `decompose_hardswish.py` lowers HardSwish to `Mul/Clip/Div`, so the reduced/minimal op set runs all four EE weights. Use the minimal patched build; the `-full` build is unnecessary.
>
> That is: EE docker = infiniflow ORT fork (v1.29.0 + `0001-shared-initializer.patch`) + **minimal_build enabled** (reduced). The OSS in-process path uses the same patched minimal library.

- `TestModelSnapshotHash` locks the model snapshot hash; drift = FATAL (prevents models from being silently swapped).
- `TestEquivalenceReport` aggregates all parity (`native_integration_test.go`; the TSR row is already the EE 2-class `RunTSREE`).
- `TestTSREEParity` standalone EE 2-class fixture (`tsr_ee_parity/tsr_ee_table.png` + `.golden.json`).
- `tsr_integration_ee_test.go` EE 2-class-specific integration tests (`TestTSREESessionReuse` session-reuse stability / `TestTSREEConcurrencyConsistent` concurrency safety / `TestTSREEBehavioralContract` behavioral contract: exactly 2 classes v=283/h=17, 300 boxes, all boxes within image with positive finite coords).

---

## 5. Related file index

| File | Role |
|---|---|
| `internal/deepdoc/native/tsr_ee.go` | `RunTSREE`: loads `tsr.ee.cpu.ort`, outputs the EE 2-class separator format |
| `internal/deepdoc/native/tsr_parity_ee_test.go` | EE 2-class fixture parity test (`maxDrift=3`, hard-caps hallucination) |
| `internal/deepdoc/native/tsr_integration_ee_test.go` | EE 2-class-specific integration tests (session reuse / concurrency / behavioral contract 2 classes v=283/h=17); after the OSS 6-class `RunTSR` retired, TSR tests uniformly land on the EE path |
| `testdata/tsr_ee_parity/` (external testdata repo, tag `deepdoc-native-testdata-v2-ee`) | fixture: `tsr_ee_table.png` + `tsr_ee_table.golden.json` (300 boxes, v=283/h=17; PNG isolates JPG-decode diff) + `corpus/` 13 images (source = Python EE docker `.pt` wire output) |
| `internal/deepdoc/native/native_integration_test.go` | `TestEquivalenceReport` / `TestModelSnapshotHash` |
| `tools/scripts/generate_overlay_ee.go` | runs real `RunTSREE` + golden matching, generates the 2-box-diff human-review overlay PNGs (output NOT committed — see §3) |
| `tools/scripts/export_tsr_ee_onnx.py` | rebuild `tsr.ee.cpu.onnx` from `tsr.pt` (ultralytics export opset11 → version_converter upgrade to opset17 + shape/opset validation; run inside the EE python container) — outputs `.onnx`; follow with `convert_deepdoc_model.py` to get the `.ort` the backend loads |
| `tools/scripts/convert_deepdoc_model.py` | ONNX→`.ort` FlatBuffer serializer + opset upgrade + ORT 1.29.0 version gate — **shared OSS tool**, inherited by EE via OSS→EE merge (not added by this PR; see `ee_deepdoc_model_conversion.md`) |
| `tools/scripts/decompose_hardswish.py` | EE-only: decompose `HardSwish`→`Mul/Clip/Div` in det/rec so minimal ORT runs them without a HardSwish kernel |
| `tools/scripts/verify_ort.py` | assert `.onnx` vs `.ort` produce identical output for the same input (conversion-loss check) |
| `tools/scripts/cmp_orts.py` | EE `.ort` vs OSS `.ort` numerical cross-check (regression baseline) |
| `internal/deepdoc/server/server_ee.go` | EE DeepDoc HTTP service (library package, consumed by the `--deepdoc` mode of the single `ragflow_server` binary); `Run` starts a gin service exposing `POST /predict/dla`, `/predict/tsr`, `/predict/ocr_det`, `/predict/ocr_rec` and `/health`, calling `native.RunDLA`/`RunTSREE`/`RunDet`/`RunOCRRecBatchReal` |
| `cmd/deepdoc_server_ee.go` | `runDeepDoc` — the single-binary activation point for the EE DeepDoc inference server (`//go:build cgo`); `cmd/ragflow_server.go`'s `main()` early-returns into it for the deepdoc CPU/GPU/Ascend modes, calling `native.InitCPUEE()` / `infnative.InitCPUEE()` then `deepdocserver.Run(ctx, port)` (default port 8001, overridable via `--port`) |
| `internal/deepdoc/server/server_test.go` | HTTP-layer TDD tests: multipart / base64-JSON / no file → 400 / inference error → 500 (stub backend, no model needed) |
| `internal/deepdoc/server/server_integration_test.go` | end-to-end integration test (`cgo && integration`): real `tsr.ee.cpu.ort` runs the full pipeline, checks `v=283/h=17`, ≥298 boxes |
