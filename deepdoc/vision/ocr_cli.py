import argparse
import io
import logging
import os
import reprlib
from api.utils.api_utils import timeout
import cv2
import numpy as np
import requests
from PIL import Image


class OCRClient:
    def __init__(self, http_ip_port):
        # Use new unified endpoint: /predict/ocr
        self.url = http_ip_port + "/predict/ocr"
        self.session = requests.Session()

    @classmethod
    def _extract_detect_boxes(cls, output):
        if not isinstance(output, (list, tuple)) or not output:
            return []

        # LitServe may wrap OCR detection payloads with one extra batch level:
        # [[[box1, box2, ...]]] instead of [[box1, box2, ...]].
        # Traverse nested containers and keep only valid quadrilateral boxes.
        boxes = []
        stack = [output[0]]
        while stack:
            item = stack.pop()
            box = cls._normalize_box(item)
            if box:
                boxes.append(box)
                continue
            if isinstance(item, (list, tuple)):
                for child in reversed(item):
                    stack.append(child)
        return boxes

    @timeout(2)
    def detect(self, arr: np.ndarray, **kwargs):
        img = Image.fromarray(arr, "RGB")
        img_byte_arr = io.BytesIO()
        img.save(img_byte_arr, format="jpeg")
        for _ in range(3):
            try:
                response = self.session.post(self.url, files={"request": img_byte_arr.getvalue()}, data={"operator": "det"})
                response = response.json()
                logging.info(
                    "OCR detect response keys: %s, output_len=%s, output[0]_type=%s",
                    list(response.keys()) if isinstance(response, dict) else "not_dict",
                    len(response.get("output", [])) if isinstance(response.get("output"), list) else "not_list",
                    type(response.get("output", [[]])[0]) if isinstance(response.get("output"), list) and response.get("output") else "empty",
                )
                if "output" not in response:
                    raise Exception(str(response))
                if not response["output"] or not response["output"][0]:
                    return []
                raw = response["output"][0]
                r = reprlib.Repr()
                r.maxdepth = 4
                r.maxlist = 10
                logging.info("OCR detect: raw=%s", r.repr(raw))
                # Normalize to list of boxes: each box is [[x0,y0],[x1,y1],[x2,y2],[x3,y3]]
                boxes = self._normalize_detect_output(raw)
                return zip(boxes, [("", 0) for _ in range(len(boxes))])
            except Exception as e:
                logging.exception(e)
                self.session = requests.Session()

        return []

    @classmethod
    def _normalize_detect_output(cls, raw):
        """Normalize OCR detection output to list of quadrilateral boxes.

        Handles multiple formats:
        - PaddleOCR format: [[[[x0,y0],[x1,y1],[x2,y2],[x3,y3]], ...]] (4D - one batch)
        - PaddleOCR format: [[[x0,y0],[x1,y1],[x2,y2],[x3,y3]], ...] (3D - no batch)
        - Flat coords format: [x0,y0,x1,y1,x2,y2,x3,y3,...]
        - Center+size format: [cx,cy,w,h,angle] or [cx,cy,w,h,angle,score]
        - Detection format: [x,y,score] or [x,y]
        """
        if not isinstance(raw, (list, tuple)):
            logging.warning("_normalize_detect_output: raw is not list/tuple, type=%s", type(raw))
            return []

        # Unwrap extra batch dimension: [[[[box1], [box2]]]] -> [[box1], [box2]]
        # deepdoc returns 4D: output[0] = list of 16 boxes, output[0][0] = first box (4 coord pairs)
        # Check: if raw[0][0][0] is a 2-element list (coord pair), then raw[0] is a box
        # and raw is 4D (needs one unwrap)
        if len(raw) > 0 and isinstance(raw[0], (list, tuple)) and len(raw[0]) > 0:
            first_box = raw[0][0] if len(raw[0]) > 0 else None
            if first_box is not None and isinstance(first_box, (list, tuple)) and len(first_box) > 0:
                if isinstance(first_box[0], (list, tuple)) and len(first_box[0]) >= 2 and not isinstance(first_box[0][0], (list, tuple)):
                    raw = raw[0]
                    logging.info("_normalize_detect_output: unwrapped 4D->3D, new len=%d", len(raw))

        logging.info("_normalize_detect_output: processing %d items, first_item_type=%s", len(raw), type(raw[0]) if raw else "empty")
        result = []
        for i, item in enumerate(raw):
            if not isinstance(item, (list, tuple)):
                continue
            box = cls._normalize_box(item)
            if box:
                result.append(box)
        logging.info("_normalize_detect_output: produced %d boxes", len(result))
        return result

    @classmethod
    def _normalize_box(cls, item):
        """Normalize a single box to [[x0,y0],[x1,y1],[x2,y2],[x3,y3]] format."""
        if not item:
            logging.warning("_normalize_box: item is None or empty, type=%s", type(item))
            return None

        logging.info("_normalize_box: item type=%s, len=%d, item=%s", type(item), len(item) if hasattr(item, "__len__") else "N/A", item)
        if len(item) == 4 and all(isinstance(p, (list, tuple)) and len(p) >= 2 for p in item):
            logging.info("_normalize_box: matched 4-coordinate-pairs format")
            return [[float(p[0]), float(p[1])] for p in item]

        # Flat format: [x0,y0,x1,y1,x2,y2,x3,y3] or similar
        if len(item) >= 8:
            logging.info("_normalize_box: matched flat-format (len=%d)", len(item))
            coords = []
            for i in range(0, 8, 2):
                coords.append([float(item[i]), float(item[i + 1])])
            return coords

        # Center-size-angle format: [cx, cy, w, h, angle] or [cx,cy,w,h,angle,score]
        if len(item) >= 5:
            logging.info("_normalize_box: matched center-size format (len=%d)", len(item))
            cx, cy, w, h = item[0], item[1], item[2], item[3]
            x0, y0 = cx - w / 2, cy - h / 2
            x1, y1 = cx + w / 2, cy - h / 2
            x2, y2 = cx + w / 2, cy + h / 2
            x3, y3 = cx - w / 2, cy + h / 2
            return [[x0, y0], [x1, y1], [x2, y2], [x3, y3]]

        # Detection format: [x,y,score] or [x,y]
        if len(item) >= 2:
            logging.info("_normalize_box: matched detection format (len=%d)", len(item))
            x, y = item[0], item[1]
            size = 10
            return [[x - size, y - size], [x + size, y - size], [x + size, y + size], [x - size, y + size]]

        logging.warning("_normalize_box: unrecognized format, item=%s", item)
        return None

    @timeout(18)
    def recognize_batch(self, images: list[np.ndarray], **kwargs):
        res = []
        for i, arr in enumerate(images):
            img = Image.fromarray(arr, "RGB")
            img_byte_arr = io.BytesIO()
            img.save(img_byte_arr, format="jpeg")
            for _ in range(3):
                try:
                    response = self.session.post(self.url, files={"request": img_byte_arr.getvalue()}, data={"operator": "rec"})
                    response = response.json()
                    if "output" not in response:
                        raise Exception(str(response))
                    if not response["output"] or not response["output"][0] or not response["output"][0][0][0]:
                        res.append("")
                    else:
                        res.append(response["output"][0][0][0][0])
                    break
                except Exception as e:
                    logging.exception(e)
                    self.session = requests.Session()
            if i == len(res):
                res.append("")
        return res

    def get_rotate_crop_image(self, img, points):
        """
        img_height, img_width = img.shape[0:2]
        left = int(np.min(points[:, 0]))
        right = int(np.max(points[:, 0]))
        top = int(np.min(points[:, 1]))
        bottom = int(np.max(points[:, 1]))
        img_crop = img[top:bottom, left:right, :].copy()
        points[:, 0] = points[:, 0] - left
        points[:, 1] = points[:, 1] - top
        """
        assert len(points) == 4, "shape of points must be 4*2"
        img_crop_width = int(max(np.linalg.norm(points[0] - points[1]), np.linalg.norm(points[2] - points[3])))
        img_crop_height = int(max(np.linalg.norm(points[0] - points[3]), np.linalg.norm(points[1] - points[2])))
        pts_std = np.float32([[0, 0], [img_crop_width, 0], [img_crop_width, img_crop_height], [0, img_crop_height]])
        M = cv2.getPerspectiveTransform(points, pts_std)
        dst_img = cv2.warpPerspective(img, M, (img_crop_width, img_crop_height), borderMode=cv2.BORDER_REPLICATE, flags=cv2.INTER_CUBIC)
        dst_img_height, dst_img_width = dst_img.shape[0:2]
        if dst_img_height * 1.0 / dst_img_width >= 1.5:
            dst_img = np.rot90(dst_img)
        return dst_img


class OCR(OCRClient):
    """Client-side OCR adapter that preserves the local OCR call shape."""

    def __init__(self, http_ip_port: str | None = None):
        resolved_url = (http_ip_port or os.environ.get("DEEPDOC_URL") or "").strip()
        if not resolved_url:
            raise RuntimeError("DEEPDOC_URL environment variable is required for OCR")
        super().__init__(resolved_url)

    def __call__(self, img, device_id=0, cls=True):
        del device_id, cls
        if img is None:
            return None

        boxes = list(self.detect(img))
        if not boxes:
            return []

        crops = []
        normalized_boxes = []
        for box, _ in boxes:
            box_arr = np.array(box, dtype=np.float32)
            normalized_boxes.append(box_arr.tolist())
            crops.append(self.get_rotate_crop_image(img, box_arr))

        texts = self.recognize_batch(crops)
        return list(zip(normalized_boxes, [(text or "", 1.0) for text in texts]))


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser()
    parser.add_argument("--ip", type=str, help="Server's IP")
    parser.add_argument("--port", type=int, default=11234, help="Server's port")
    parser.add_argument("--operator", type=str, default="det", help="det|rec")
    parser.add_argument("--image", type=str, help="Input image file")
    args = parser.parse_args()
    return args


if __name__ == "__main__":
    args = parse_args()
    cli = OCRClient(f"http://{args.ip}:{args.port}")
    img = cv2.imread(args.image)
    if args.operator == "det":
        preds = cli.detect(img)
    else:
        preds = cli.recognize_batch([img])
