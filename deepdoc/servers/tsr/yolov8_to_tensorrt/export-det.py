"""
⚠️  DEPRECATION WARNING ⚠️

This script has known compatibility issues and produces incomplete ONNX exports.

SYMPTOMS:
- The exported ONNX file will be unusually small (~30-50 KB instead of 200-300 MB)
- The exported model may not include all weights and parameters
- TensorRT engine built from this ONNX may have inference accuracy issues

RECOMMENDED ALTERNATIVE:
Use the standard Ultralytics export method instead:

    from ultralytics import YOLO
    model = YOLO("yolov8x.pt")
    model.export(format="onnx", opset=11, simplify=True, imgsz=640)

This produces a complete ONNX file (~261 MB) with full model weights.

For complete build instructions, see:
  deepdoc/servers/README_UNIFIED.md

If you still choose to use this script, proceed with caution and verify your ONNX file size.
"""

import argparse
import os
import sys
from io import BytesIO

import onnx
import torch
from ultralytics import YOLO

from common import PostDetect, optim

try:
    import onnxsim
except ImportError:
    onnxsim = None

# Print deprecation warning on script execution
print("\n" + "=" * 70, file=sys.stderr)
print("⚠️  DEPRECATION WARNING: export-det.py has known issues", file=sys.stderr)
print("=" * 70, file=sys.stderr)
print("This script may produce incomplete ONNX exports.", file=sys.stderr)
print("\nRecommended alternative:", file=sys.stderr)
print('  python -c \'from ultralytics import YOLO; YOLO("yolov8x.pt").export(format="onnx", opset=11, simplify=True, imgsz=640)\'', file=sys.stderr)
print("\nSee: deepdoc/servers/README_UNIFIED.md for complete instructions.", file=sys.stderr)
print("=" * 70 + "\n", file=sys.stderr)


def parse_args():
    parser = argparse.ArgumentParser()
    parser.add_argument("-w", "--weights", type=str, required=True, help="PyTorch yolov8 weights")
    parser.add_argument("--iou-thres", type=float, default=0.65, help="IOU threshoud for NMS plugin")
    parser.add_argument("--conf-thres", type=float, default=0.25, help="CONF threshoud for NMS plugin")
    parser.add_argument("--topk", type=int, default=100, help="Max number of detection bboxes")
    parser.add_argument("--opset", type=int, default=11, help="ONNX opset version")
    parser.add_argument("--sim", action="store_true", help="simplify onnx model")
    parser.add_argument("--input-shape", nargs="+", type=int, default=[1, 3, 640, 640], help="Model input shape only for api builder")
    parser.add_argument("--device", type=str, default="cpu", help="Export ONNX device")
    args = parser.parse_args()
    assert len(args.input_shape) == 4
    PostDetect.conf_thres = args.conf_thres
    PostDetect.iou_thres = args.iou_thres
    PostDetect.topk = args.topk
    return args


def main(args):
    b = args.input_shape[0]
    YOLOv8 = YOLO(args.weights)
    model = YOLOv8.model.fuse().eval()
    for m in model.modules():
        optim(m)
        m.to(args.device)
    model.to(args.device)
    fake_input = torch.randn(args.input_shape).to(args.device)
    for _ in range(2):
        model(fake_input)
    save_path = args.weights.replace(".pt", ".onnx")
    with BytesIO() as f:
        torch.onnx.export(model, fake_input, f, opset_version=args.opset, input_names=["images"], output_names=["num_dets", "bboxes", "scores", "labels"])
        f.seek(0)
        onnx_model = onnx.load(f)
    onnx.checker.check_model(onnx_model)
    shapes = [b, 1, b, args.topk, 4, b, args.topk, b, args.topk]
    for i in onnx_model.graph.output:
        for j in i.type.tensor_type.shape.dim:
            j.dim_param = str(shapes.pop(0))
    if args.sim:
        try:
            onnx_model, check = onnxsim.simplify(onnx_model)
            assert check, "assert check failed"
        except Exception as e:
            print(f"Simplifier failure: {e}")
    onnx.save(onnx_model, save_path)
    print(f"ONNX export success, saved as {save_path}")

    # Verify file size and warn if too small
    file_size_mb = os.path.getsize(save_path) / (1024 * 1024)
    print(f"ONNX file size: {file_size_mb:.2f} MB")

    if file_size_mb < 50:
        print("\n" + "!" * 70, file=sys.stderr)
        print("⚠️  WARNING: ONNX file size is unusually small!", file=sys.stderr)
        print(f"Expected: 200-300 MB, Got: {file_size_mb:.2f} MB", file=sys.stderr)
        print("This indicates the export did NOT include all model weights.", file=sys.stderr)
        print("\nRECOMMENDED ACTION:", file=sys.stderr)
        print("Use Ultralytics standard export method instead:", file=sys.stderr)
        print('  python -c \'from ultralytics import YOLO; YOLO("yolov8x.pt").export(format="onnx", opset=11, simplify=True, imgsz=640)\'', file=sys.stderr)
        print("\nSee: deepdoc/servers/README_UNIFIED.md for complete instructions.", file=sys.stderr)
        print("!" * 70 + "\n", file=sys.stderr)
    else:
        print(f"✓ ONNX file size looks good ({file_size_mb:.2f} MB)")


if __name__ == "__main__":
    main(parse_args())
