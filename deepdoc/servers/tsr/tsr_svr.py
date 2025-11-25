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
from yolov8_to_tensorrt.utils import letterbox, blob, det_postprocess
from yolov8_to_tensorrt.engine import TRTModule

logger = logging.getLogger(__name__)
ENGINE_PATH = "./yolov8x.engine"

# Set float32 matrix multiplication precision if GPU is available and capable
if torch.cuda.is_available() and torch.cuda.get_device_capability() >= (8, 0):
    torch.set_float32_matmul_precision("high")


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
        self.engine = TRTModule(self.engine_path, device)
        self.H, self.W = self.engine.inp_info[0].shape[-2:]

    def decode_request(self, request: UploadFile):
        img = cv2.imdecode(np.frombuffer(request.file.read(), np.uint8), cv2.IMREAD_UNCHANGED)
        img, ratio, dwdh = letterbox(img, (self.W, self.H))
        rgb = cv2.cvtColor(img, cv2.COLOR_BGR2RGB)
        tensor = blob(rgb, return_seg=False)
        dwdh = torch.asarray(dwdh * 2, dtype=torch.float16, device=self.device)
        return torch.asarray(tensor, device=self.device), dwdh, ratio

    def predict(self, tensors):
        res = []
        for tensor, dwdh, ratio in tensors:
            bboxes, scores, labels = det_postprocess(self.engine(tensor))
            bboxes -= dwdh
            bboxes /= ratio
            bboxes = [bbox.round().int().tolist() for bbox in bboxes]
            arr = list(zip(bboxes, scores.float().tolist(), labels.int().tolist()))
            for bx, s, l in arr:
                bx.append(s)
                bx.append(l)
            res.append([bx for bx, _, _ in arr])
        return res

    def encode_response(self, output):
        print(output)
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

