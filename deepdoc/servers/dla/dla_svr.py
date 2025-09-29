import argparse
import base64
import io
import logging
import PIL
import cv2
import numpy as np
import torch
import litserve as ls
from PIL import Image
from fastapi import UploadFile
from yolov10_to_tensor.utils import letterbox
from yolov10_to_tensor.engine import BaseEngine

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


class Predictor(BaseEngine):
    def __init__(self, engine_path):
        super(Predictor, self).__init__(engine_path)
        self.n_classes = len(DLA_CLASSES)  # your model classes
        self.class_names = DLA_CLASSES


def process_image(image_data):
    image = base64.b64decode(image_data)
    pil_image = PIL.Image.open(io.BytesIO(image)).convert("RGB")
    return pil_image


class ImageClassifierAPI(ls.LitAPI):
    def __init__(self, engine_path):
        self.engine_path = engine_path
        super().__init__()

    def setup(self, device):
        self.device = device
        self.engine = Predictor(self.engine_path)

    def decode_request(self, request: UploadFile):
        img = cv2.imdecode(np.frombuffer(request.file.read(), np.uint8), cv2.IMREAD_UNCHANGED)
        img, ratio, dwdh = letterbox(img, self.engine.imgsz)
        return img, np.asarray(dwdh * 2, dtype=np.float32), ratio

    def predict(self, tensors):
        res = []
        for img, dwdh, ratio in tensors:
            num, final_boxes, final_scores, final_cls_inds  = self.engine.infer(img)
            final_boxes -= dwdh
            final_boxes = np.reshape(final_boxes/ratio, (-1, 4))
            final_scores = np.reshape(final_scores, (-1, 1))
            final_cls_inds = np.reshape(final_cls_inds, (-1, 1))
            dets = np.concatenate([np.array(final_boxes)[:int(num[0])], np.array(final_scores)[:int(num[0])], np.array(final_cls_inds)[:int(num[0])]], axis=-1)
            res.append(dets.tolist())
        return res

    def encode_response(self, output):
        output = [o for o in output if o[-2]>0.4]
        return {"bboxes": output}


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser()
    parser.add_argument('--engine', type=str, help='Engine file')
    parser.add_argument('--port', type=int, default=11234, help='serving port')
    parser.add_argument('--workers',
                        type=int,
                        default=2,
                        help='Workers for every device')
    args = parser.parse_args()
    return args

if __name__ == "__main__":
    ARGS = parse_args()
    api = ImageClassifierAPI(ARGS.engine)
    server = ls.LitServer(
        api,
        timeout=100,
        workers_per_device=ARGS.workers,
        max_batch_size=4,
        track_requests=True
    )
    server.run(port=ARGS.port, log_level="warning")

