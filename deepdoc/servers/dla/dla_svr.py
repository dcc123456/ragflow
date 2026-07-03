import argparse
import base64
import io
import logging
import PIL
import cv2
import numpy as np
import torch
import litserve as ls
from fastapi import UploadFile
from .yolov10_to_tensor.utils import letterbox

# Conditional import for GPU (TensorRT) vs CPU (ONNX)
try:
    from .yolov10_to_tensor.engine import BaseEngine

    HAS_TENSORRT = True
except ImportError:
    HAS_TENSORRT = False

from .yolov10_to_tensor.onnx_engine import ONNXEngine

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

# Set float32 matrix multiplication precision if GPU is available and capable
if torch.cuda.is_available() and torch.cuda.get_device_capability() >= (8, 0):
    torch.set_float32_matmul_precision("high")


class Predictor:
    """Unified Predictor that supports both TensorRT (GPU) and ONNX (CPU)"""

    def __init__(self, engine_path: str, use_gpu: bool = False):
        """
        Args:
            engine_path: Path to model file (.trt for TensorRT, .onnx for ONNX)
            use_gpu: Use GPU inference (TensorRT). Default False uses CPU (ONNX)
        """
        self.use_gpu = use_gpu and HAS_TENSORRT and not engine_path.endswith(".onnx")

        if self.use_gpu:
            self.engine = BaseEngine(engine_path)
        else:
            # Use ONNX for CPU inference
            self.engine = ONNXEngine(engine_path)

        self.n_classes = len(DLA_CLASSES)
        self.class_names = DLA_CLASSES
        self.imgsz = self.engine.imgsz

    def infer(self, img):
        """Run inference

        Args:
            img: Preprocessed image tensor

        Returns:
            num: Number of detections
            boxes: Bounding boxes (N, 4) format [x1, y1, x2, y2]
            scores: Confidence scores (N, 1)
            cls_inds: Class indices (N, 1)
        """
        outputs = self.engine.infer(img)

        # Process outputs based on engine type
        if not self.use_gpu:
            # ONNX output: (1, 300, 6) format [x1, y1, x2, y2, score, class]
            # outputs is a list with one element
            output = outputs[0]  # Shape: (1, 300, 6)
            output = output[0]  # Remove batch dimension: (300, 6)

            # Filter out detections with score < 0.25
            valid_dets = output[output[:, 4] > 0.25]

            if len(valid_dets) > 0:
                boxes = valid_dets[:, :4]  # (N, 4)
                scores = valid_dets[:, 4:5]  # (N, 1)
                cls_inds = valid_dets[:, 5:6]  # (N, 1)
                num = np.array([len(valid_dets)])
            else:
                boxes = np.zeros((0, 4))
                scores = np.zeros((0, 1))
                cls_inds = np.zeros((0, 1))
                num = np.array([0])
        else:
            # TensorRT outputs
            num, boxes, scores, cls_inds = outputs

        return num, boxes, scores, cls_inds


def process_image(image_data):
    image = base64.b64decode(image_data)
    pil_image = PIL.Image.open(io.BytesIO(image)).convert("RGB")
    return pil_image


class DLAEndpoint(ls.LitAPI):
    """Document Layout Analysis Endpoint for Unified DeepDoc Server"""

    def __init__(self, engine_path: str, use_gpu: bool = False):
        self.engine_path = engine_path
        self.use_gpu = use_gpu
        super().__init__()
        self.api_path = "/predict/dla"
        # Set max_batch_size on the API object (new LitServe usage)
        self.max_batch_size = 4

    def setup(self, device):
        self.device = device
        self.engine = Predictor(self.engine_path, use_gpu=self.use_gpu)

    def decode_request(self, request: UploadFile):
        img = cv2.imdecode(np.frombuffer(request.file.read(), np.uint8), cv2.IMREAD_UNCHANGED)
        img, ratio, dwdh = letterbox(img, self.engine.imgsz)

        # For ONNX (CPU), letterbox already returns (3, H, W) float32 normalized array
        # We just need to add batch dimension
        if not self.engine.use_gpu:
            img = img[np.newaxis, ...]  # Add batch dimension: (3, H, W) -> (1, 3, H, W)

        # Match ragflow_enterprise2: dwdh * 2 creates shape (2,) array, which broadcasts correctly
        return img, np.asarray(dwdh * 2, dtype=np.float32), ratio

    def predict(self, tensors):
        # Handle both batched and single requests
        if isinstance(tensors, list) and len(tensors) > 0 and isinstance(tensors[0], tuple):
            # Batched requests
            batch = tensors
        else:
            # Single request - tensors is (img, dwdh, ratio)
            batch = [tensors]

        res = []
        for img, dwdh, ratio in batch:
            num, final_boxes, final_scores, final_cls_inds = self.engine.infer(img)

            # For ONNX (CPU), output may already be in correct format
            if not self.engine.use_gpu:
                # ONNX outputs may need different processing
                if len(final_boxes.shape) == 1:
                    final_boxes = final_boxes.reshape(-1, 4)
                if len(final_scores.shape) == 1:
                    final_scores = final_scores.reshape(-1, 1)
                if len(final_cls_inds.shape) == 1:
                    final_cls_inds = final_cls_inds.reshape(-1, 1)

                # Adjust boxes by letterbox padding
                if len(final_boxes) > 0:
                    final_boxes = final_boxes - dwdh
                    final_boxes = final_boxes / ratio

                num_dets = min(int(num[0]) if len(num) > 0 else len(final_boxes), len(final_boxes))
            else:
                # TensorRT processing
                final_boxes -= dwdh
                final_boxes = np.reshape(final_boxes / ratio, (-1, 4))
                final_scores = np.reshape(final_scores, (-1, 1))
                final_cls_inds = np.reshape(final_cls_inds, (-1, 1))
                num_dets = int(num[0])

            # Combine results
            dets = np.concatenate([np.array(final_boxes)[:num_dets], np.array(final_scores)[:num_dets], np.array(final_cls_inds)[:num_dets]], axis=-1)
            res.append(dets.tolist())
        return res

    def encode_response(self, output):
        import sys

        print(f"DLA encode_response called: output type={type(output)}, len={len(output) if isinstance(output, list) else 'N/A'}", file=sys.stderr, flush=True)
        print(f"DLA encode_response: output={output}", file=sys.stderr, flush=True)

        # In multi-API mode ([api1, api2, api3]), each API is independent
        # encode_response receives the output for a SINGLE request (already unbundled by LitServe)
        # predict() returns: [[dets1], [dets2], ...] (batch results)
        # LitServe unbatchs and calls encode_response([dets1]) for each request
        # So output is detections for one image: [[x1,y1,x2,y2,conf,cls], ...]

        if not output:
            return {"bboxes": []}

        # output is detections for single image
        dets = output
        print(f"DLA encode_response: dets type={type(dets)}, len={len(dets)}", file=sys.stderr, flush=True)

        # Filter: keep detections with confidence > 0.4 (column 4)
        # dets format: [[x1,y1,x2,y2,conf,cls], ...]
        if len(dets) > 0:
            # dets might be a list or numpy array, handle both
            if isinstance(dets, list):
                # Already converted to list by predict's .tolist()
                filtered = [d for d in dets if len(d) > 4 and d[4] > 0.4]
            else:
                # Still a numpy array
                filtered = dets[dets[:, 4] > 0.4].tolist()
            print(f"DLA encode_response: filtered to {len(filtered)} detections", file=sys.stderr, flush=True)
            return {"bboxes": filtered}

        return {"bboxes": []}


# Backward compatibility alias
ImageClassifierAPI = DLAEndpoint


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser()
    parser.add_argument("--engine", type=str, help="Engine file")
    parser.add_argument("--port", type=int, default=11234, help="serving port")
    parser.add_argument("--workers", type=int, default=2, help="Workers for every device")
    args = parser.parse_args()
    return args


if __name__ == "__main__":
    ARGS = parse_args()
    api = ImageClassifierAPI(ARGS.engine)
    server = ls.LitServer(api, timeout=100, workers_per_device=ARGS.workers, max_batch_size=4, track_requests=True)
    server.run(port=ARGS.port, log_level="warning")
