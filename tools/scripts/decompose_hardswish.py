#!/usr/bin/env python3
"""Decompose every HardSwish op into primitive ops so the model runs on a
vanilla ORT CPU EP that has no HardSwish kernel (mirrors how the OSS .ort
export already lowered HardSwish).

HardSwish(X) = X * min(max(X+3, 0), 6) / 6   (ONNX HardSwish spec, opset14)
decomposed as:  Add(X,3) -> Clip(.,0,6) -> Div(.,6) -> Mul(X,.)

USAGE
-----
    python decompose_hardswish.py --input det.onnx --output det.onnx
    # (same path for in-place decomposition)
"""

import argparse

import numpy as np
import onnx
from onnx import helper, numpy_helper


def make_const(name, val):
    return numpy_helper.from_array(np.array(val, dtype=np.float32), name=name)


def decompose(path_in, path_out):
    m = onnx.load(path_in)
    g = m.graph

    # shared scalar constants (float32, scalar)
    c3 = make_const("hsd_c3", 3.0)
    c0 = make_const("hsd_c0", 0.0)
    c6 = make_const("hsd_c6", 6.0)
    g.initializer.extend([c3, c0, c6])

    new_nodes = []
    cnt = 0
    for n in g.node:
        if n.op_type != "HardSwish":
            new_nodes.append(n)
            continue
        cnt += 1
        X = n.input[0]
        Y = n.output[0]
        a = helper.make_node("Add", [X, c3.name], [f"hsd_{cnt}_add"])
        c = helper.make_node("Clip", [a.output[0], c0.name, c6.name], [f"hsd_{cnt}_clip"])
        d = helper.make_node("Div", [c.output[0], c6.name], [f"hsd_{cnt}_div"])
        mu = helper.make_node("Mul", [X, d.output[0]], [Y])  # keep original output name
        new_nodes.extend([a, c, d, mu])

    del g.node[:]
    g.node.extend(new_nodes)

    onnx.checker.check_model(m)
    m = onnx.shape_inference.infer_shapes(m)
    onnx.save(m, path_out)
    print(f"decomposed {cnt} HardSwish in {path_in} -> {path_out}")


if __name__ == "__main__":
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--input", required=True, help="source .onnx with HardSwish ops")
    ap.add_argument("--output", required=True, help="output .onnx with HardSwish decomposed (can equal --input)")
    args = ap.parse_args()
    decompose(args.input, args.output)
