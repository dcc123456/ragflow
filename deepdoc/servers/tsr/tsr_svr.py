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
from .yolov8_to_tensorrt.utils import letterbox, blob, det_postprocess

# Conditional import for GPU (TensorRT) vs CPU (PyTorch)
try:
    from .yolov8_to_tensorrt.engine import TRTModule

    HAS_TENSORRT = True
except ImportError:
    HAS_TENSORRT = False

logger = logging.getLogger(__name__)
ENGINE_PATH = "./yolov8x.engine"

# Set float32 matrix multiplication precision if GPU is available and capable
if torch.cuda.is_available() and torch.cuda.get_device_capability() >= (8, 0):
    torch.set_float32_matmul_precision("high")


def process_image(image_data):
    image = base64.b64decode(image_data)
    pil_image = PIL.Image.open(io.BytesIO(image)).convert("RGB")
    return pil_image


class TSREndpoint(ls.LitAPI):
    """Table Structure Recognition Endpoint for Unified DeepDoc Server"""

    def __init__(self, engine_path: str, use_gpu: bool = False):
        """
        Args:
            engine_path: Path to model file (.engine for TensorRT, .onnx for ONNX)
            use_gpu: Use GPU inference (TensorRT). Default False uses CPU (ONNX)
        """
        self.engine_path = engine_path
        self.use_gpu = use_gpu and HAS_TENSORRT and not engine_path.endswith(".onnx")
        super().__init__()
        self.api_path = "/predict/tsr"
        # Set max_batch_size on the API object (new LitServe usage)
        self.max_batch_size = 4

    def setup(self, device):
        self.device = device

        if self.use_gpu:
            # Use TensorRT for GPU inference
            self.engine = TRTModule(self.engine_path, device)
            self.H, self.W = self.engine.inp_info[0].shape[-2:]
        else:
            # Use PyTorch for CPU inference
            # Load YOLOv8 model using ultralytics
            from ultralytics import YOLO

            self.model = YOLO(self.engine_path)
            self.model.to("cpu")
            # Get model input size from first layer
            self.H, self.W = 640, 640  # YOLOv8 default input size

    def decode_request(self, request: UploadFile):
        # Load image as color (3 channels) to ensure consistency
        img = cv2.imdecode(np.frombuffer(request.file.read(), np.uint8), cv2.IMREAD_COLOR)
        if img is None:
            raise ValueError("Failed to decode image")

        img, ratio, dwdh = letterbox(img, (self.W, self.H))
        rgb = cv2.cvtColor(img, cv2.COLOR_BGR2RGB)

        # Match ragflow_enterprise2: dwdh * 2 creates shape (2,) array, which broadcasts correctly
        dwdh_array = np.asarray(dwdh * 2, dtype=np.float32)

        if not self.use_gpu:
            # For PyTorch (CPU): convert RGB image to tensor
            # ultralytics YOLO expects numpy array (H, W, 3) in RGB format
            dwdh_tensor = torch.from_numpy(dwdh_array)
            return rgb, dwdh_tensor, ratio  # Return numpy RGB array for YOLO
        else:
            # For TensorRT (GPU): blob returns torch tensor
            tensor = blob(rgb, return_seg=False)
            dwdh_tensor = torch.asarray(dwdh_array, dtype=torch.float16, device=self.device)
            return torch.asarray(tensor, device=self.device), dwdh_tensor, ratio

    def predict(self, tensors):
        # Handle both batched and single requests
        if isinstance(tensors, list) and len(tensors) > 0 and isinstance(tensors[0], tuple) and len(tensors[0]) == 3:
            # Batched requests
            batch = tensors
        else:
            # Single request - tensors is (img/dwhd_tensor, dwdh, ratio)
            batch = [tensors]

        res = []
        for img_input, dwdh, ratio in batch:
            if not self.use_gpu:
                # PyTorch CPU inference using ultralytics YOLO
                # img_input is numpy RGB array (H, W, 3)
                results = self.model(img_input, verbose=False)

                # Extract boxes, scores, labels from ultralytics results
                # results[0].boxes.data has shape (N, 6) with [x1, y1, x2, y2, conf, cls]
                boxes_data = results[0].boxes.data
                if len(boxes_data) > 0:
                    bboxes = boxes_data[:, :4]  # (N, 4)
                    scores = boxes_data[:, 4]  # (N,)
                    labels = boxes_data[:, 5]  # (N,)

                    # Convert to torch tensors for compatibility with rest of code
                    bboxes = torch.tensor(bboxes)
                    scores = torch.tensor(scores)
                    labels = torch.tensor(labels)
                else:
                    bboxes = torch.zeros((0, 4))
                    scores = torch.zeros((0,))
                    labels = torch.zeros((0,))
            else:
                # TensorRT GPU inference
                bboxes, scores, labels = det_postprocess(self.engine(img_input))

            # Coordinate transformation (same for CPU and GPU)
            if len(bboxes) > 0:
                bboxes -= dwdh
                bboxes /= ratio
                bboxes = [bbox.round().int().tolist() for bbox in bboxes]
                arr = list(zip(bboxes, scores.float().tolist(), labels.int().tolist()))
                for bx, s, lbl in arr:
                    bx.append(s)
                    bx.append(lbl)
                res.append([bx for bx, _, _ in arr])
            else:
                res.append([])
        return res

    def encode_response(self, output):
        # In multi-API mode ([api1, api2, api3]), each API is independent
        # encode_response receives the output for a SINGLE request (already unbundled by LitServe)
        # predict() returns: [[bboxes1], [bboxes2], ...] (batch results)
        # LitServe unbatchs and calls encode_response([bboxes1]) for each request
        # So output is bboxes for one image: [[x1,y1,x2,y2,score,cls], ...]
        return {"bboxes": output if output else []}


# Backward compatibility alias
ImageClassifierAPI = TSREndpoint


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
