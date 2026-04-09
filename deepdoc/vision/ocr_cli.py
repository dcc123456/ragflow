import argparse
import io
import logging
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

    @staticmethod
    def _is_point(point):
        return (
            isinstance(point, (list, tuple))
            and len(point) >= 2
            and all(isinstance(v, (int, float)) for v in point[:2])
        )

    @classmethod
    def _normalize_box(cls, box):
        while (
            isinstance(box, (list, tuple))
            and len(box) == 1
            and isinstance(box[0], (list, tuple))
            and not cls._is_point(box[0])
        ):
            box = box[0]

        if not isinstance(box, (list, tuple)):
            return None

        normalized = []
        for point in box:
            if not cls._is_point(point):
                return None
            normalized.append([point[0], point[1]])
        return normalized

    @timeout(2)
    def detect(self, arr: np.ndarray, **kwargs):
        img = Image.fromarray(arr, 'RGB')
        img_byte_arr = io.BytesIO()
        img.save(img_byte_arr, format="jpeg")
        for _ in range(3):
            try:
                response = self.session.post(self.url, files={"request": img_byte_arr.getvalue()}, data={"operator": "det"})
                response = response.json()
                if "output" not in response:
                    raise Exception(str(response))
                # PaddleOCR with LitServe returns:
                # response["output"] = [ocr_result] where ocr_result is PaddleOCR output
                # PaddleOCR ocr(img, rec=False) returns: [[[[x0,y0], [x1,y1], [x2,y2], [x3,y3]], ...]]
                # So response["output"][0] = [[[x0,y0], [x1,y1], [x2,y2], [x3,y3]], ...] (list of boxes)
                if not response["output"] or not response["output"][0]:
                    return []
                else:
                    boxes = [box for raw_box in response["output"][0] if (box := self._normalize_box(raw_box))]
                    return zip(boxes, [("", 0) for _ in range(len(boxes))])
            except Exception as e:
                logging.exception(e)
                self.session = requests.Session()

        return []

    @timeout(18)
    def recognize_batch(self, images: list[np.ndarray], **kwargs):
        res = []
        for i, arr in enumerate(images):
            img = Image.fromarray(arr, 'RGB')
            img_byte_arr = io.BytesIO()
            img.save(img_byte_arr, format="jpeg")
            for _ in range(3):
                try:
                    response = self.session.post(self.url, files={"request": img_byte_arr.getvalue()}, data={"operator": "rec"})
                    response = response.json()
                    if "output" not in response:
                        raise Exception(str(response))
                    if not response["output"] or not response["output"][0] or not response["output"][0][0]:
                        res.append("")
                    else:
                        res.append(response["output"][0][0][0])
                    break
                except Exception as e:
                    logging.exception(e)
                    self.session = requests.Session()
            if i == len(res):
                res.append("")
        return res

    def get_rotate_crop_image(self, img, points):
        '''
        img_height, img_width = img.shape[0:2]
        left = int(np.min(points[:, 0]))
        right = int(np.max(points[:, 0]))
        top = int(np.min(points[:, 1]))
        bottom = int(np.max(points[:, 1]))
        img_crop = img[top:bottom, left:right, :].copy()
        points[:, 0] = points[:, 0] - left
        points[:, 1] = points[:, 1] - top
        '''
        assert len(points) == 4, "shape of points must be 4*2"
        img_crop_width = int(
            max(
                np.linalg.norm(points[0] - points[1]),
                np.linalg.norm(points[2] - points[3])))
        img_crop_height = int(
            max(
                np.linalg.norm(points[0] - points[3]),
                np.linalg.norm(points[1] - points[2])))
        pts_std = np.float32([[0, 0], [img_crop_width, 0],
                              [img_crop_width, img_crop_height],
                              [0, img_crop_height]])
        M = cv2.getPerspectiveTransform(points, pts_std)
        dst_img = cv2.warpPerspective(
            img,
            M, (img_crop_width, img_crop_height),
            borderMode=cv2.BORDER_REPLICATE,
            flags=cv2.INTER_CUBIC)
        dst_img_height, dst_img_width = dst_img.shape[0:2]
        if dst_img_height * 1.0 / dst_img_width >= 1.5:
            dst_img = np.rot90(dst_img)
        return dst_img


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser()
    parser.add_argument('--ip', type=str, help="Server's IP")
    parser.add_argument('--port', type=int, default=11234, help="Server's port")
    parser.add_argument('--operator', type=str, default="det", help="det|rec")
    parser.add_argument('--image', type=str, help='Input image file')
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
    print(preds)
