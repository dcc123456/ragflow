#!/usr/bin/env python3
"""Rebuild the EE TSR model (`tsr.ee.cpu.onnx`) from the EE PyTorch weights.

The Go EE backend loads the EE 2-class table-structure model as
`tsr.ee.cpu.onnx` (see `internal/deepdoc/native/model_files_ee.go`). It is
produced from the EE PyTorch weights in two steps:

  1. **ultralytics export** — `tsr.pt` -> opset-11 ONNX via
     `YOLO(weights).export(format='onnx', opset=11, simplify=True, imgsz=640)`.
     Do NOT use the deprecated `ee_deepdoc_models/tsr/yolov8_to_tensorrt/export-det.py`
     (its own docstring warns it produces incomplete ~30-50KB exports); the
     complete export is ~272MB.
  2. **opset upgrade** — opset 11 -> opset 17 via `onnx.version_converter`.
     The Go backend links the reduced-operator ORT build, which registers
     `Split` only from opset 13 on (where `split` is an input tensor).
     The opset-11 export carries `split` as an attribute and fails to load
     with "Could not find an implementation for Split(11)" — upgrading to 17
     fixes that without touching the Go side.

This script produces the **`.onnx`** half of the pipeline. The Go EE backend
does NOT load `.onnx` at runtime — it links the *reduced-operator* ORT static
library (minimal build) and loads the `.ort` FlatBuffer instead. So after this
script writes `tsr.ee.cpu.onnx`, finish the pipeline with
`tools/scripts/convert_deepdoc_model.py`, which upgrades the opset (already 17
here, so a no-op) and serializes to `tsr.ee.cpu.ort`:

  python tools/scripts/convert_deepdoc_model.py \
      --input deepdoc_ee_models/tsr.ee.cpu.onnx

(det/rec additionally need `decompose_hardswish.py` first — see
`docs/develop/ee_deepdoc_model_conversion.md`.)

The `.onnx` graph is byte-compatible with the historical `tsr_op17.onnx`
pipeline (same producer, same graph, opset 17). It is NOT yet the `.ort`
FlatBuffer the backend loads; the conversion above is mandatory on the EE
side.

Run inside the Python EE image (it carries ultralytics + torch + onnx; the
dev host cannot install ultralytics). The image's entrypoint is
deepdoc_svr.py, so override it:

  docker run --rm --entrypoint /app/.venv/bin/python3 \
    -v "$PWD:/w" -w /w 192.168.1.51/infiniflow-ai/deepdoc_cpu:latest \
    tools/scripts/export_tsr_ee_onnx.py \
      --weights ee_deepdoc_models/tsr/tsr.pt \
      --out deepdoc_ee_models/tsr.ee.cpu.onnx

  # then convert the produced .onnx to the .ort the Go backend loads:
  docker exec -w /ragflow/deepdoc_ee_models deepdoc_ee_cpu \
    python tools/scripts/convert_deepdoc_model.py --input tsr.ee.cpu.onnx

After rebuilding, refresh the snapshot lock in
`internal/deepdoc/native/ocr_rec_integration_ee_test.go`
(`eeModelSnapshotHashes["tsr.ee.cpu.onnx"]`) to the new sha256 — a stale lock
is a hard test failure by design.
"""

import argparse
import os
import shutil
import sys
import tempfile

EXPORT_OPSET = 11  # ultralytics export opset (Split as attribute -> must be upgraded)
DEFAULT_TARGET_OPSET = 17
EXPECTED_INPUT_NAME = "images"
EXPECTED_INPUT_DIMS = [1, 3, 640, 640]
EXPECTED_OUTPUT_CHANNELS = 6  # [cx, cy, w, h, cls0_prob, cls1_prob] x 8400 anchors


def model_opset(model) -> int:
    for entry in model.opset_import:
        if entry.domain in ("", "ai.onnx"):
            return entry.version
    raise RuntimeError("no ai.onnx opset entry found")


def main() -> int:
    ap = argparse.ArgumentParser(
        description="Rebuild tsr.ee.cpu.onnx (EE 2-class TSR) from the .pt weights.",
    )
    ap.add_argument("--weights", required=True, help="source .pt weights (EE 2-class YOLOv8 separator model)")
    ap.add_argument("--out", required=True, help="output path for the .ee.cpu.onnx (e.g. deepdoc_ee_models/tsr.ee.cpu.onnx)")
    ap.add_argument("--opset", type=int, default=DEFAULT_TARGET_OPSET, help=f"target opset after upgrade (default {DEFAULT_TARGET_OPSET})")
    ap.add_argument("--imgsz", type=int, default=640, help="export image size (default 640, matching the EE service)")
    args = ap.parse_args()

    if not os.path.isfile(args.weights):
        print(f"ERROR: weights not found: {args.weights}", file=sys.stderr)
        return 2

    try:
        import onnx
        from onnx import version_converter
        from ultralytics import YOLO
    except ImportError as e:
        print(f"ERROR: missing dependency ({e}). Run inside the Python EE image (ultralytics + torch + onnx); see the module docstring.", file=sys.stderr)
        return 2

    print(f"[1/3] ultralytics export: {args.weights} -> opset-{EXPORT_OPSET} onnx")
    model = YOLO(args.weights)
    with tempfile.TemporaryDirectory(prefix="tsr_export_") as td:
        exported = model.export(format="onnx", opset=EXPORT_OPSET, simplify=True, imgsz=args.imgsz)
        staged = os.path.join(td, os.path.basename(exported))
        # shutil.move (not os.replace): the weights dir is usually a bind mount
        # and /tmp is the container layer, i.e. different filesystems.
        shutil.move(exported, staged)  # keep the source weights dir clean

        m = onnx.load(staged)
        cur = model_opset(m)
        size_mb = os.path.getsize(staged) / 1e6
        print(f"      exported opset = {cur}, size = {size_mb:.1f} MB")
        if size_mb < 100:
            print(f"ERROR: export is only {size_mb:.1f} MB — incomplete export (the deprecated export-det.py failure mode); expected ~272 MB.", file=sys.stderr)
            return 1

        if cur < args.opset:
            print(f"[2/3] upgrading opset {cur} -> {args.opset} via version_converter")
            m = version_converter.convert_version(m, args.opset)
        else:
            print(f"[2/3] opset {cur} already >= {args.opset}; no upgrade needed")

        print(f"[3/3] saving {args.out}")
        out_dir = os.path.dirname(os.path.abspath(args.out))
        os.makedirs(out_dir, exist_ok=True)
        onnx.save(m, args.out)

    # Sanity: reload from disk and confirm opset + IO contract.
    final = onnx.load(args.out)
    got_opset = model_opset(final)
    if got_opset != args.opset:
        print(f"ERROR: saved opset = {got_opset}, expected {args.opset}", file=sys.stderr)
        return 1
    inp = final.graph.input[0]
    out = final.graph.output[0]
    dims = [d.dim_value or d.dim_param for d in inp.type.tensor_type.shape.dim]
    out_dims = [d.dim_value or d.dim_param for d in out.type.tensor_type.shape.dim]
    print(f"      input  {inp.name} {dims}")
    print(f"      output {out.name} {out_dims}")
    if inp.name != EXPECTED_INPUT_NAME or dims != EXPECTED_INPUT_DIMS:
        print(f"WARNING: unexpected input {inp.name} {dims} (expected {EXPECTED_INPUT_NAME} {EXPECTED_INPUT_DIMS})", file=sys.stderr)
    if len(out_dims) != 3 or out_dims[1] != EXPECTED_OUTPUT_CHANNELS:
        print(f"WARNING: unexpected output shape {out_dims} (expected [1, {EXPECTED_OUTPUT_CHANNELS}, 8400])", file=sys.stderr)
    print(f"DONE -> {args.out} ({os.path.getsize(args.out) / 1e6:.1f} MB)")
    print('Remember: refresh eeModelSnapshotHashes["tsr.ee.cpu.onnx"] in ocr_rec_integration_ee_test.go if this rebuild is meant to ship.')
    return 0


if __name__ == "__main__":
    sys.exit(main())
