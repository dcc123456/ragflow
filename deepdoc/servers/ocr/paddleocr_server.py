#!/usr/bin/env python3
import cv2
# PEP 723 metadata
# /// script
# requires-python = ">=3.10,<3.11"
# dependencies = [
#   "paddlepaddle-gpu==3.0.0",
#   "paddleocr",
#   "litserve",
# ]
# ///

import litserve as ls
import numpy as np
from paddleocr import PaddleOCR

class OcrAPI(ls.LitAPI):
    def setup(self, device):
        # need to run only once to load model into memory
        self.ocr = PaddleOCR(use_gpu=True, lang='ch')

    def decode_request(self, request):
        opt = request["operator"]
        img = cv2.imdecode(np.frombuffer(request["request"].file.read(), np.uint8), cv2.IMREAD_UNCHANGED)
        return img, opt

    def predict(self, x):
        # Easily build compound systems. Run inference and return the output.
        res = []
        for img, opt in x:
            if opt == "det":
                res.append(self.ocr.ocr(img, rec=False))
            else:
                res.append(self.ocr.ocr(img, det=False, cls=False))

        return res

    def encode_response(self, output):
        # Convert the model output to a response payload.
        return {"output": output} 

if __name__ == "__main__":
    # scale with advanced features (batching, GPUs, etc...)
    server = ls.LitServer(OcrAPI(), accelerator="gpu", max_batch_size=32, workers_per_device=3)
    server.run(port=8000)
