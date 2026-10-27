#!/usr/bin/env python3
"""Cross-check an EE .ort against its OSS counterpart (.ort vs .ort).

Both files are fixed ORT-format serializations consumed by the reduced
(minimal) ORT build, which performs no runtime graph optimization, so both are
run with ORT_DISABLE_ALL on a constant input and compared. Used during the
2026-10-15 EE .ort regeneration to confirm the EE conversion matches the OSS
reference: the OSS .ort comes from the InfiniFlow/deepdoc snapshot (produced
by the same ORT 1.29.0 converter), so an EE .ort built with the same tool
should agree to float-rounding level.

Usage:
  python tools/scripts/cmp_orts.py \
      --ee /tmp/ee_ort/layout.ee.cpu.ort \
      --oss ~/.cache/stele/models/deepdoc/layout.ort
"""

import argparse
import os

import numpy as np
import onnxruntime as ort


def run(path, val):
    so = ort.SessionOptions()
    so.graph_optimization_level = ort.GraphOptimizationLevel.ORT_DISABLE_ALL
    s = ort.InferenceSession(path, so, providers=["CPUExecutionProvider"])
    feeds = {
        i.name: np.full(
            tuple(i.shape) if all(isinstance(d, int) for d in i.shape) else (1, 3, 1024, 1024),
            val,
            dtype=np.float32,
        )
        for i in s.get_inputs()
    }
    return s.run(None, feeds)[0]


def main():
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--ee", required=True, help="EE .ort file")
    ap.add_argument("--oss", required=True, help="OSS reference .ort file")
    ap.add_argument("--value", type=float, default=0.5, help="constant input value (default: %(default)s)")
    args = ap.parse_args()

    ee = run(args.ee, args.value)
    oss = run(args.oss, args.value)
    if ee.shape != oss.shape:
        print(f"shape mismatch: EE {ee.shape} vs OSS {oss.shape}")
        raise SystemExit(1)
    d = np.abs(ee - oss)
    print(f"{os.path.basename(args.ee)} vs {os.path.basename(args.oss)} (const {args.value}): max|d|={d.max():.3e}  #(>1e-3)={int((d > 1e-3).sum())} / {d.size}")


if __name__ == "__main__":
    main()
