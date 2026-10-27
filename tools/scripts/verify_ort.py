#!/usr/bin/env python3
"""Verify ONNX -> .ort conversion correctness for the EE DeepDoc weights.

Runs each model pair (<stem>.ee.cpu.onnx vs <stem>.ee.cpu.ort) with the SAME
random input under ORT and compares the outputs index-by-index. A correct
.ort serialization is numerically identical to its .onnx source (the .ort is a
fixed-graph serialization of the same model), so max|delta| must stay at
float-rounding level.

Usage (any env with onnxruntime; run right after convert_deepdoc_model.py):
  python tools/scripts/verify_ort.py \
      --onnx-dir /path/to/converted-onnx --ort-dir /path/to/ort-output
"""

import argparse

import numpy as np
import onnxruntime as ort

STEMS = ("det", "layout", "rec", "tsr")


def make_input(meta):
    feeds = {}
    for i in meta:
        shape = []
        for d in i.shape:
            if isinstance(d, int) and d > 0:
                shape.append(d)
            else:
                shape.append(1 if d is None or isinstance(d, str) else 256)
        feeds[i.name] = np.random.rand(*shape).astype(np.float32)
    return feeds


def main():
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--onnx-dir", required=True, help="directory holding <stem>.ee.cpu.onnx (pre-conversion)")
    ap.add_argument("--ort-dir", required=True, help="directory holding <stem>.ee.cpu.ort (converter output)")
    args = ap.parse_args()

    rc = 0
    for stem in STEMS:
        onx = f"{args.onnx_dir}/{stem}.ee.cpu.onnx"
        ort_f = f"{args.ort_dir}/{stem}.ee.cpu.ort"
        so = ort.SessionOptions()
        so.graph_optimization_level = ort.GraphOptimizationLevel.ORT_ENABLE_ALL
        s_onx = ort.InferenceSession(onx, so, providers=["CPUExecutionProvider"])
        s_ort = ort.InferenceSession(ort_f, so, providers=["CPUExecutionProvider"])
        feeds = make_input(s_onx.get_inputs())
        try:
            o_onx = s_onx.run(None, feeds)
            o_ort = s_ort.run(None, feeds)
        except (RuntimeError, ort.OnnxRuntimeException) as e:
            print(f"{stem}: RUN ERROR {e}")
            rc = 1
            continue
        maxd = 0.0
        for a, b in zip(o_onx, o_ort):
            if a.shape != b.shape:
                print(f"{stem}: shape mismatch {a.shape} vs {b.shape}")
                maxd = float("inf")
                break
            maxd = max(maxd, float(np.max(np.abs(a - b))))
        ok = maxd < 1e-4
        print(f"{stem}: max|delta| = {maxd:.3e}  (onnx outs={len(o_onx)}, ort outs={len(o_ort)})  {'OK' if ok else 'FAIL'}")
        if not ok:
            rc = 1
    raise SystemExit(rc)


if __name__ == "__main__":
    main()
