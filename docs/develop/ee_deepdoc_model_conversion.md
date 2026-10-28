# EE DeepDoc Model (Re)generation and `.ort` Conversion Manual

> This document is the **authoritative reproduction manual** for converting the EE DeepDoc
> four-model weights from their upstream formats (PaddleOCR `.pdparams` / PyTorch `.pt`) into
> the `.ee.cpu.ort` FlatBuffer that the Go backend actually loads. For the model hash locks,
> docker deployment, and parity verification, see [`ee_deepdoc_parity.md`](ee_deepdoc_parity.md).

## 1. Why `.ort` + minimal ORT

The Go EE backend statically links the **reduced-operator (minimal) ONNX Runtime static
library**, which implements only modern op versions and **consumes `.ort` FlatBuffers
exclusively** (it does not parse the protobuf `.onnx` format). Therefore every EE weight must
end up as `.ee.cpu.ort`, and the ORT version used for conversion must match the version the Go
binary links (currently **1.29.0** — `convert_deepdoc_model.py` enforces this with a hard
version gate and aborts on mismatch).

Two upstream lines:

| Upstream | Models | Upstream → ONNX tool | Special preprocessing |
|---|---|---|---|
| PaddleOCR `ch_PP-OCRv4_*` | `det`, `rec` | `paddle2onnx` 1.3.1, **opset14** | **HardSwish decomposition** (minimal ORT has no HardSwish kernel) |
| PyTorch (ultralytics) | `tsr` (YOLOv8 2-class), `layout` (YOLOv10) | `ultralytics export` | `tsr` needs opset 11→17 upgrade |

Shared downstream (both lines): ONNX → `.ort` (ORT 1.29.0 official
`onnxruntime.tools.convert_onnx_models_to_ort`), plus numerical cross-checks.

> **"Modeled after the OSS `.ort` generation"**: the OSS `.ort` files
> (`~/.cache/stele/models/deepdoc/`, provided by the upstream `InfiniFlow/deepdoc` snapshot,
> where `.onnx`+`.ort` coexist in the same directory) were produced with the **same tool at
> the same version**. EE conversion reuses this exact mechanism; `cmp_orts.py` directly
> cross-checks EE `.ort` against OSS `.ort` as a regression baseline. `decompose_hardswish.py`'s
> docstring also states explicitly that it *"mirrors how the OSS .ort export already lowered
> HardSwish"*.

## 2. Runtime environment

- **Upstream export (paddle2onnx / ultralytics / torch / onnx)**: run inside the **Python EE
  docker** (`deepdoc_cpu`) — the dev host cannot install `ultralytics`, and `paddle2onnx`
  1.3.1 also lives in that image.
- **ONNX → `.ort` conversion (ORT 1.29.0 + version gate)**: also run inside the EE docker; its
  `onnxruntime` **must be 1.29.0** (`convert_deepdoc_model.py` checks this and refuses to
  continue otherwise). The converter is the ORT-bundled module
  `onnxruntime.tools.convert_onnx_models_to_ort` — no separate script needed.
- **Verification / cross-check (verify_ort.py / cmp_orts.py)**: pure `onnxruntime` + `numpy`,
  runnable inside the EE docker.

The EE docker's entrypoint is `deepdoc_svr.py`; override it to run a script:

```bash
docker run --rm --entrypoint /app/.venv/bin/python3 \
  -v "$PWD:/w" -w /w 192.168.1.51/infiniflow-ai/deepdoc_cpu:latest \
  tools/scripts/<script>.py ...
# or, against an already-running container:
docker exec -w /ragflow/deepdoc_ee_models deepdoc_ee_cpu \
  python tools/scripts/<script>.py ...
```

## 3. PaddleOCR line (det / rec): 4 steps

```bash
# Step 1: paddle2onnx 1.3.1 (command-line tool, no wrapper script).
#   Source = the PaddleOCR inference dir (contains inference.pdmodel + inference.pdiparams).
#   det:  ch_PP-OCRv4_det_infer   → det.onnx  (opset 14)
#   rec:  ch_PP-OCRv4_rec_infer   → rec.onnx  (opset 14)
#   NOTE: the source flag is --model_dir (a DIRECTORY), NOT --model; the two files
#   inside it are named via --model_filename / --params_filename, and the opset flag
#   is --opset_version (NOT --opset). The paths below are the ones baked into the
#   deepdoc_cpu EE docker image.
paddle2onnx \
  --model_dir /root/.paddleocr/whl/det/ch/ch_PP-OCRv4_det_infer \
  --model_filename inference.pdmodel \
  --params_filename inference.pdiparams \
  --save_file det.onnx \
  --opset_version 14 \
  --enable_onnx_checker 1

paddle2onnx \
  --model_dir /root/.paddleocr/whl/rec/ch/ch_PP-OCRv4_rec_infer \
  --model_filename inference.pdmodel \
  --params_filename inference.pdiparams \
  --save_file rec.onnx \
  --opset_version 14 \
  --enable_onnx_checker 1
#   (run these inside the deepdoc_cpu EE docker; paddle2onnx 1.3.1 is on its PATH)

# Step 2: HardSwish decomposition (EE-specific; minimal ORT has no HardSwish kernel)
#   rec: 28 HardSwish sites; det: 24 sites
python tools/scripts/decompose_hardswish.py --input det.onnx --output det.onnx
python tools/scripts/decompose_hardswish.py --input rec.onnx --output rec.onnx

# Step 3: ONNX → .ort (ORT 1.29.0 hard gate).
#   CRITICAL: det/rec MUST be converted with `--opset 14` (their native paddle2onnx
#   export opset). convert_deepdoc_model.py defaults to `--opset 17`; that upgrade
#   promotes BatchNormalization→op15 and Shape→op15, which the minimal ORT the Go
#   backend links does NOT implement, so the resulting det.ort/rec.ort fail to load
#   with "Could not find an implementation for BatchNormalization(15)/Shape(15)".
#   Only tsr needs the opset-17 upgrade (see §4); det/rec must stay at 14.
python tools/scripts/convert_deepdoc_model.py --input det.onnx --opset 14
python tools/scripts/convert_deepdoc_model.py --input rec.onnx --opset 14
#   → produces det.ort / rec.ort; drop them into deepdoc_ee_models/ renamed *.ee.cpu.ort

# Step 4: numerical verification + cross-check against OSS .ort (regression baseline)
#   (verify_ort.py is directory-based: it pairs <stem>.onnx in --onnx-dir with the
#    same-stem <stem>.ort in --ort-dir; run it where both det.onnx and det.ort live)
python tools/scripts/verify_ort.py --onnx-dir . --ort-dir .
python tools/scripts/cmp_orts.py   --ee det.ort --oss ~/.cache/stele/models/deepdoc/det.ort
```

**HardSwish decomposition principle**: `HardSwish(x) = x · min(max(x+3, 0), 6) / 6`,
decomposed into `Add(x, 3) → Clip(0, 6) → Div(6) → Mul(x, ·)`, all of which are ops the
minimal ORT registers. The pre-decomposition `.onnx` files are backed up under
`deepdoc_ee_models/.bak_hardswish_decomp_20261015/`.

## 4. PyTorch line (tsr / layout)

```bash
# tsr: ultralytics export opset11 → upgrade opset17 → .ort
#   Step A — the raw ultralytics export (opset 11, the ultralytics default).
#   This is exactly what export_tsr_ee_onnx.py wraps internally; shown here so the
#   upstream step is reproducible without the wrapper:
python - <<'PY'
from ultralytics import YOLO
YOLO("ee_deepdoc_models/tsr/tsr.pt").export(
    format="onnx", opset=11, simplify=True, imgsz=640)
#   → writes tsr.onnx (in the weights dir), opset 11, ~272 MB
PY
#   Step B — opset upgrade 11→17 (version_converter) + shape/opset validation,
#   then save as the EE .onnx. Wrapped by export_tsr_ee_onnx.py:
python tools/scripts/export_tsr_ee_onnx.py \
  --weights ee_deepdoc_models/tsr/tsr.pt \
  --out deepdoc_ee_models/tsr.ee.cpu.onnx
#   (outputs .onnx; opset already 17 after the internal upgrade)
python tools/scripts/convert_deepdoc_model.py --input deepdoc_ee_models/tsr.ee.cpu.onnx
#   (opset already 17, upgrade is a no-op; only .ort serialization happens)

# layout: ultralytics YOLOv10 export (already opset17), convert to .ort directly.
#   Raw ultralytics export (no wrapper script exists for layout):
python - <<'PY'
from ultralytics import YOLO
YOLO("doclayout_yolo_docstructbench_imgsz1024.pt").export(
    format="onnx", opset=17, simplify=True, imgsz=1024)
#   → writes layout.onnx, opset 17
PY
python tools/scripts/convert_deepdoc_model.py --input layout.onnx
```

> The `tsr` opset 11→17 upgrade exists because the minimal ORT registers `Split`'s `split`
> as an **input tensor** only from opset 13 on; opset 11 carries `split` as an **attribute**
> and fails to load with "Could not find an implementation for Split(11)".
> `convert_deepdoc_model.py`'s `--opset 17` default exists for exactly this. `layout` is
> already opset17 upstream, so no upgrade is needed.
>
> **Asymmetry — read carefully**: `tsr` and `layout` are correctly left at the 17 default
> (a no-op — both are already opset17 before this step). But `det`/`rec` (§3 Step 3) MUST be
> converted with `--opset 14`: the 17 default promotes their `BatchNormalization`/`Shape` to
> op15, which the minimal ORT cannot run. Never run det/rec through the default opset.

## 5. Verification and cross-check

- **`verify_ort.py --onnx-dir <dir> --ort-dir <dir>`**: for each `<stem>.onnx` in `--onnx-dir`
  it runs it against the same-stem `<stem>.ort` in `--ort-dir` on the same random input and
  asserts the outputs are numerically identical (confirms the ONNX→`.ort` serialization is
  lossless). It is directory-based, not single-file.
- **`cmp_orts.py --ee X.ee.cpu.ort --oss <oss>.ort`**: cross-checks the EE `.ort` against the
  OSS `.ort` on multiple real inputs as a cross-engine regression baseline (EE and OSS are two
  independent weight sets, so byte-for-byte equality is *not* expected; same-input inference
  outputs should stay within an explainable range — used to catch accidental conversion drift).
- **`generate_overlay_ee`** (in-repo, `tools/scripts/generate_overlay_ee.go`): parity-diff visualization
  for human review of TSR boxes.

## 6. `required_operators.config` (minimal-build op allow-list)

The minimal ORT static library trims ops according to `required_operators.config`.
`merge_ops.py` (in this repo under `tools/scripts/`) merges the actually-used ops from the EE
models into that config; ragflow-build has an equivalent tool `scripts/gen_required_ops.sh`,
**reuse it rather than maintaining a second copy of the config-generation logic here**. Note
that `Clip@13` must be present in the config's `ai.onnx;13` allow-list, otherwise rec/tsr's
`Clip(13)` reports a missing implementation (see OSS PR #21051 / ragflow-build patch2).
Likewise det/rec converted at opset 14 keep `BatchNormalization(14)` / `Shape(13)` (which the
minimal ORT must therefore implement); promoting them to op15 — the side effect of an opset-17
upgrade — leaves them outside the allow-list and the model fails to load (see §3 Step 3).

## 7. Pitfall checklist

1. **ORT version is sensitive**: `.ort` is not forward/backward compatible across ORT majors;
   the conversion environment must be 1.29.0 (matching the Go link).
2. **HardSwish**: minimal ORT has no kernel, so det/rec **must** go through
   `decompose_hardswish.py` first; otherwise a missing-implementation error is raised.
3. **Split opset**: `tsr`'s upstream opset11 `split` is an attribute; it must be upgraded to 17,
   or `Split(11)` is reported as missing.
4. **det/rec opset must stay 14**: converting them with the default `--opset 17` promotes
   `BatchNormalization`/`Shape` to op15, which the minimal ORT does not implement → the `.ort`
   fails to load (`Could not find an implementation for BatchNormalization(15)/Shape(15)`).
   Always pass `--opset 14` for det/rec; only `tsr` is upgraded to 17 (§3 Step 3, §4).
5. **`.ort` is not committed**: large files are gitignored; the repo stores scripts only. There is
   **no sha256 lock** on the EE `.ort` — the serialization is non-deterministic (same toolchain +
   weights yield different bytes each run), so a byte-exact hash can never be reproduced by
   regeneration. Correctness is enforced by the **parity / golden tests**, not a hash: a wrong or
   upgraded weight set fails parity. On an intentional weight upgrade, regenerate the goldens
   against the new snapshot in the same change; do not try to "refresh a hash lock" (there isn't one).
6. **Runtime environment**: upstream export must run inside the EE docker
   (`ultralytics`/`paddle2onnx` are not on the dev host).

## 8. Script inventory in this repo (added/restored by this PR)

| Script | Status | Purpose |
|---|---|---|
| `tools/scripts/convert_deepdoc_model.py` | **shared — lives in OSS `ragflow`, inherited by EE via the OSS→EE merge** (not added by this PR) | ONNX→`.ort` + opset upgrade + ORT 1.29.0 version gate |
| `tools/scripts/decompose_hardswish.py` | **added** | HardSwish→Mul/Clip/Div decomposition for det/rec (EE-specific) |
| `tools/scripts/verify_ort.py` | **added** | `.onnx` vs `.ort` same-input numerical-equality check |
| `tools/scripts/cmp_orts.py` | **added** | EE `.ort` vs OSS `.ort` cross-check regression |
| `tools/scripts/export_tsr_ee_onnx.py` | existing (docstring updated) | `tsr.pt`→`.onnx` (opset11→17), chained to the `.ort` step above |

External dependencies (not in this repo): `paddle2onnx` 1.3.1, `ultralytics`/`torch`, `onnx`,
`onnxruntime` 1.29.0 (which bundles `convert_onnx_models_to_ort`).

> **Note on `convert_deepdoc_model.py`**: this converter is **shared tooling maintained in the
> OSS `ragflow` repo** (it is OSS-origin and has no EE-specific logic). It is **not** added by
> this EE PR; EE obtains it through the regular OSS→EE merge. When regenerating `.ort` in an EE
> checkout taken *before* that merge, copy the script from OSS (or run the conversion in an
> environment that already has it). The remaining scripts in the table above are EE-specific and
> live only here.
