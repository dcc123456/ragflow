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
# Import PaddleOCR lazily to avoid initialization issues at import time

class OCREndpoint(ls.LitAPI):
    """OCR Endpoint for Unified DeepDoc Server"""

    def __init__(self, use_gpu: bool = False):
        super().__init__()
        self.api_path = "/predict/ocr"
        self.use_gpu = use_gpu
        # Set max_batch_size on the API object (new LitServe usage)
        self.max_batch_size = 32

    def setup(self, device):
        # need to run only once to load model into memory
        from paddleocr import PaddleOCR  # Lazy import to avoid hang at module level
        self.ocr = PaddleOCR(use_angle_cls=True, lang='ch', use_gpu=self.use_gpu)

    def decode_request(self, request):
        import sys
        print("OCR decode_request called", file=sys.stderr, flush=True)
        opt = request.get("operator", "det")
        print(f"OCR decode_request: operator={opt}", file=sys.stderr, flush=True)
        file_obj = request.get("request")
        print(f"OCR decode_request: file_obj type={type(file_obj)}", file=sys.stderr, flush=True)

        if hasattr(file_obj, 'file'):
            img_bytes = file_obj.file.read()
        elif hasattr(file_obj, 'read'):
            img_bytes = file_obj.read()
        else:
            raise ValueError(f"Cannot read file from {type(file_obj)}")

        # Load image and convert to RGB (3 channels) for PaddleOCR
        img = cv2.imdecode(np.frombuffer(img_bytes, np.uint8), cv2.IMREAD_COLOR)
        if img is None:
            raise ValueError("Failed to decode image")

        # Convert BGR to RGB (PaddleOCR expects RGB)
        img = cv2.cvtColor(img, cv2.COLOR_BGR2RGB)
        print(f"OCR decode_request: decoded image shape={img.shape}", file=sys.stderr, flush=True)

        # When using multiple APIs with LitServe, we cannot return a tuple
        # Store operator as instance variable and return only the image
        self._last_operator = opt
        return img

    def predict(self, x):
        import sys
        print(f"OCR predict called: x type={type(x)}, shape={x.shape if hasattr(x, 'shape') else 'N/A'}", file=sys.stderr, flush=True)

        # LitServe with max_batch_size=1 passes a single image: (H, W, C)
        # LitServe with max_batch_size>1 passes batched: (N, H, W, C)
        # However, when using the API in a multi-API setup, LitServe may add batch dimension
        # So we need to handle: (H, W, C), (N, H, W, C), or list of images
        opt = getattr(self, '_last_operator', 'det')
        print(f"OCR predict: operator={opt}", file=sys.stderr, flush=True)

        if isinstance(x, np.ndarray):
            if x.ndim == 3:
                # Single image: (H, W, C)
                images = [x]
            elif x.ndim == 4:
                # Batched images: (N, H, W, C)
                images = list(x)
            else:
                raise ValueError(f"Unexpected image shape: {x.shape}")
        else:
            # List of images
            images = x

        print(f"OCR predict: processing {len(images)} images", file=sys.stderr, flush=True)
        res = []
        for i, img in enumerate(images):
            print(f"OCR predict: processing image {i}, shape={img.shape if hasattr(img, 'shape') else 'N/A'}", file=sys.stderr, flush=True)
            if opt == "det":
                ocr_result = self.ocr.ocr(img, rec=False)
                print(f"OCR predict: image {i} det result type={type(ocr_result)}, len={len(ocr_result) if isinstance(ocr_result, list) else 'N/A'}", file=sys.stderr, flush=True)
                res.append(ocr_result)
            else:
                ocr_result = self.ocr.ocr(img, det=False, cls=False)
                print(f"OCR predict: image {i} rec result type={type(ocr_result)}", file=sys.stderr, flush=True)
                res.append(ocr_result)

        print(f"OCR predict: returning {len(res)} results", file=sys.stderr, flush=True)
        return res

    def encode_response(self, output):
        import sys
        print(f"OCR encode_response called: output type={type(output)}, len={len(output) if isinstance(output, list) else 'N/A'}", file=sys.stderr, flush=True)

        # In multi-API mode ([api1, api2, api3]), each API is independent
        # encode_response receives the output for a SINGLE request (already unbundled by LitServe)
        # predict() returns: [ocr_result1, ocr_result2, ...] (batch results)
        # LitServe unbatchs and calls encode_response(ocr_result1) for each request
        # So output is the OCR result for one image: [[[[x0,y0], [x1,y1], [x2,y2], [x3,y3]], ...]]

        # Convert the model output to a response payload
        return {"output": output}


# Backward compatibility alias
OcrAPI = OCREndpoint 

if __name__ == "__main__":
    # scale with advanced features (batching, GPUs, etc...)
    server = ls.LitServer(OcrAPI(), accelerator="gpu", max_batch_size=32, workers_per_device=3)
    server.run(port=8000)
