"""
Ascend (NPU) table structure recognition server.

Uses ais_bench InferSession to run `.om` models and exposes the same `/predict`
HTTP contract as the GPU TensorRT server: returns {"bboxes": [[x1,y1,x2,y2,score,class_id], ...]}.
"""

import argparse
import logging
import os

import cv2
import litserve as ls
import numpy as np
from fastapi import UploadFile
from ais_bench.infer.interface import InferSession

logger = logging.getLogger(__name__)

# Model input size expected by current tsr.om export
IMG_SIZE = (640, 640)
LABELS = [
    "table",
    "table column",
    "table row",
    "table column header",
    "table projected row header",
    "table spanning cell",
]


def preprocess_image(img: np.ndarray) -> tuple[np.ndarray, tuple[float, float]]:
    """Resize, normalize, and add batch dimension."""
    hh, ww = IMG_SIZE
    h, w = img.shape[:2]
    img = cv2.cvtColor(img, cv2.COLOR_BGR2RGB)
    img = cv2.resize(np.array(img).astype("float32"), (ww, hh))
    img /= 255.0
    img = img.transpose(2, 0, 1)
    img = img[np.newaxis, :, :, :].astype(np.float32)
    return img, (w / ww, h / hh)


def xywh2xyxy(x: np.ndarray) -> np.ndarray:
    y = np.copy(x)
    y[:, 0] = x[:, 0] - x[:, 2] / 2
    y[:, 1] = x[:, 1] - x[:, 3] / 2
    y[:, 2] = x[:, 0] + x[:, 2] / 2
    y[:, 3] = x[:, 1] + x[:, 3] / 2
    return y


def compute_iou(box: np.ndarray, boxes: np.ndarray) -> np.ndarray:
    xmin = np.maximum(box[0], boxes[:, 0])
    ymin = np.maximum(box[1], boxes[:, 1])
    xmax = np.minimum(box[2], boxes[:, 2])
    ymax = np.minimum(box[3], boxes[:, 3])
    intersection_area = np.maximum(0, xmax - xmin) * np.maximum(0, ymax - ymin)
    box_area = (box[2] - box[0]) * (box[3] - box[1])
    boxes_area = (boxes[:, 2] - boxes[:, 0]) * (boxes[:, 3] - boxes[:, 1])
    union_area = box_area + boxes_area - intersection_area
    return intersection_area / union_area


def iou_filter(boxes: np.ndarray, scores: np.ndarray, iou_threshold: float) -> list[int]:
    sorted_indices = np.argsort(scores)[::-1]
    keep_boxes = []
    while sorted_indices.size > 0:
        box_id = sorted_indices[0]
        keep_boxes.append(box_id)
        ious = compute_iou(boxes[box_id, :], boxes[sorted_indices[1:], :])
        keep_indices = np.where(ious < iou_threshold)[0]
        sorted_indices = sorted_indices[keep_indices + 1]
    return keep_boxes


def postprocess(boxes: np.ndarray, scale: tuple[float, float], thr: float) -> list[list[float]]:
    """Convert model output to bbox list [x1,y1,x2,y2,score,class_id]."""
    boxes = np.squeeze(boxes).T
    scores = np.max(boxes[:, 4:], axis=1)
    mask = scores > thr
    boxes = boxes[mask, :]
    scores = scores[mask]
    if len(boxes) == 0:
        return []

    class_ids = np.argmax(boxes[:, 4:], axis=1)
    boxes = boxes[:, :4]
    scale_factor = np.array([scale[0], scale[1], scale[0], scale[1]])
    boxes = np.multiply(boxes, scale_factor, dtype=np.float32)
    boxes = xywh2xyxy(boxes)

    keep_indices = []
    for class_id in np.unique(class_ids):
        class_indices = np.where(class_ids == class_id)[0]
        class_boxes = boxes[class_indices, :]
        class_scores = scores[class_indices]
        kept = iou_filter(class_boxes, class_scores, 0.2)
        keep_indices.extend(class_indices[kept])

    return [
        [
            float(boxes[i][0]),
            float(boxes[i][1]),
            float(boxes[i][2]),
            float(boxes[i][3]),
            float(scores[i]),
            int(class_ids[i]),
        ]
        for i in keep_indices
    ]


class AscendTSRAPI(ls.LitAPI):
    def __init__(self, om_path: str, device_id: int = 0, conf_thr: float = 0.4):
        self.om_path = om_path
        self.device_id = device_id
        self.conf_thr = max(conf_thr, 0.08)  # align with existing ascend path
        super().__init__()

    def setup(self, device):
        # device parameter is not used for NPU; we set device_id via InferSession
        self.session = InferSession(device_id=self.device_id, model_path=self.om_path)

    def decode_request(self, request: UploadFile):
        img = cv2.imdecode(np.frombuffer(request.file.read(), np.uint8), cv2.IMREAD_UNCHANGED)
        tensor, scale = preprocess_image(img)
        return tensor, scale

    def predict(self, tensors):
        res = []
        for tensor, scale in tensors:
            feeds = [tensor]
            outputs = self.session.infer(feeds=feeds, mode="static")
            outputs = outputs[0] if isinstance(outputs, list) else outputs
            res.append(postprocess(outputs, scale, self.conf_thr))
        return res

    def encode_response(self, output):
        return {"bboxes": output}


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser()
    parser.add_argument("--om", required=True, help="Path to tsr.om model")
    parser.add_argument("--port", type=int, default=11235, help="Serving port")
    parser.add_argument("--workers", type=int, default=2, help="Workers per device")
    parser.add_argument("--device-id", type=int, default=int(os.getenv("ASCEND_TSR_DEVICE_ID", 0)), help="Ascend device id")
    parser.add_argument("--conf", type=float, default=0.4, help="Confidence threshold")
    return parser.parse_args()


if __name__ == "__main__":
    args = parse_args()
    api = AscendTSRAPI(args.om, device_id=args.device_id, conf_thr=args.conf)
    server = ls.LitServer(
        api,
        timeout=100,
        workers_per_device=args.workers,
        max_batch_size=4,
        track_requests=True,
    )
    server.run(port=args.port, log_level="warning")
