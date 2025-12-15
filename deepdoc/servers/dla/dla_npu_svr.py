"""
Ascend (NPU) layout detection server.

Uses ais_bench InferSession to run `.om` model and exposes the same `/predict`
HTTP contract as the GPU TensorRT server: returns {"bboxes": [[x1,y1,x2,y2,score,class_id], ...]}.
"""

import argparse
import logging
import os

import cv2
import litserve as ls
import numpy as np
from ais_bench.infer.interface import InferSession
from fastapi import UploadFile
from yolov10_to_tensor.utils import nms

logger = logging.getLogger(__name__)

DLA_CLASSES = [
    "title",
    "Text",
    "Reference",
    "Figure",
    "Figure caption",
    "Table",
    "Table caption",
    "Table caption",
    "Equation",
    "Figure caption",
]


def preprocess_image(img: np.ndarray, input_shape: tuple[int, int]) -> tuple[np.ndarray, dict]:
    """Align with AscendLayoutRecognizer preprocess: resize+pad, normalize, CHW, add batch."""
    H, W = input_shape
    h, w = img.shape[:2]
    img = cv2.cvtColor(img, cv2.COLOR_BGR2RGB).astype(np.float32)

    r = min(H / h, W / w)
    new_unpad = (int(round(w * r)), int(round(h * r)))
    dw, dh = (W - new_unpad[0]) / 2.0, (H - new_unpad[1]) / 2.0

    img = cv2.resize(img, new_unpad, interpolation=cv2.INTER_LINEAR)
    top, bottom = int(round(dh - 0.1)), int(round(dh + 0.1))
    left, right = int(round(dw - 0.1)), int(round(dw + 0.1))
    img = cv2.copyMakeBorder(img, top, bottom, left, right, cv2.BORDER_CONSTANT, value=(114, 114, 114))

    img /= 255.0
    img = img.transpose(2, 0, 1)[np.newaxis, :, :, :].astype(np.float32)

    meta = {
        "scale_factor": [w / new_unpad[0], h / new_unpad[1]],
        "pad": [dw, dh],
        "orig_shape": [h, w],
    }
    return img, meta


def postprocess(arr: np.ndarray, meta: dict, conf_thr: float) -> list[list[float]]:
    """
    Align with AscendLayoutRecognizer postprocess:
    expects arr shape [N,6] -> x1,y1,x2,y2,score,cls (possibly already NMSed).
    Applies per-class NMS again for safety, adjusts pad/scale.
    """
    arr = np.squeeze(arr)
    if arr.ndim == 1:
        arr = arr.reshape(1, -1)

    results: list[list[float]] = []
    if arr.shape[1] != 6:
        raise ValueError(f"Unexpected output shape: {arr.shape}")

    m = arr[:, 4] >= conf_thr
    arr = arr[m]
    if arr.size == 0:
        return results

    xyxy = arr[:, :4].astype(np.float32)
    scores = arr[:, 4].astype(np.float32)
    cls_ids = arr[:, 5].astype(np.int32)

    if "pad" in meta:
        dw, dh = meta["pad"]
        sx, sy = meta["scale_factor"]
        xyxy[:, [0, 2]] -= dw
        xyxy[:, [1, 3]] -= dh
        xyxy *= np.array([sx, sy, sx, sy], dtype=np.float32)
    else:
        sx, sy = meta["scale_factor"]
        xyxy *= np.array([sx, sy, sx, sy], dtype=np.float32)

    keep_indices: list[int] = []
    for c in np.unique(cls_ids):
        idx = np.where(cls_ids == c)[0]
        k = nms(xyxy[idx], scores[idx], 0.45)
        keep_indices.extend(idx[k])

    for i in keep_indices:
        cid = int(cls_ids[i])
        if 0 <= cid < len(DLA_CLASSES):
            box = [float(t) for t in xyxy[i].tolist()]
            results.append(box + [float(scores[i]), float(cid)])
    return results


class AscendDLAAPI(ls.LitAPI):
    def __init__(self, om_path: str, device_id: int = 0, conf_thr: float = 0.4):
        self.om_path = om_path
        self.device_id = device_id
        self.conf_thr = conf_thr
        self.n_classes = len(DLA_CLASSES)
        super().__init__()

    def setup(self, device):
        self.session = InferSession(device_id=self.device_id, model_path=self.om_path)
        self.imgsz = self.session.get_inputs()[0].shape[2:4]  # H,W

    def decode_request(self, request: UploadFile):
        img = cv2.imdecode(np.frombuffer(request.file.read(), np.uint8), cv2.IMREAD_UNCHANGED)
        tensor, meta = preprocess_image(img, self.imgsz)
        return tensor, meta

    def predict(self, tensors):
        res = []
        for tensor, meta in tensors:
            outputs = self.session.infer(feeds=[tensor], mode="static")
            out_arr = outputs[0] if isinstance(outputs, list) else outputs
            dets = postprocess(out_arr, meta, self.conf_thr)
            res.append(dets)
        return res

    def encode_response(self, output):
        # Client sends one image per request; flatten first element and filter by conf.
        dets = output[0] if output else []
        dets = [d for d in dets if d[4] > self.conf_thr]
        return {"bboxes": dets}


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser()
    parser.add_argument("--om", required=True, help="Path to layout om model")
    parser.add_argument("--port", type=int, default=11236, help="Serving port")
    parser.add_argument("--workers", type=int, default=2, help="Workers per device")
    parser.add_argument("--device-id", type=int, default=int(os.getenv("ASCEND_DLA_DEVICE_ID", 0)), help="Ascend device id")
    parser.add_argument("--conf", type=float, default=0.4, help="Confidence threshold")
    return parser.parse_args()


if __name__ == "__main__":
    args = parse_args()
    api = AscendDLAAPI(args.om, device_id=args.device_id, conf_thr=args.conf)
    server = ls.LitServer(
        api,
        timeout=100,
        workers_per_device=args.workers,
        max_batch_size=4,
        track_requests=True,
    )
    server.run(port=args.port, log_level="warning")
