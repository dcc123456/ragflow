import argparse
import io
import logging
import os
from collections import Counter

import numpy as np
from api.utils.api_utils import timeout
import requests
from PIL import Image as PILImage
from PIL.Image import Image

# Import vis module only when needed (lazy import to avoid torch dependency in client-only mode)
# vis = None  # Will be imported if needed

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


class DLAClient:
    def __init__(self, http_ip_port):
        # Use new unified endpoint: /predict/dla
        self.url = http_ip_port + "/predict/dla"
        self.session = requests.Session()

    @timeout(18)
    def predict(self, images: list[Image], **kwargs):
        res = []
        for i, img in enumerate(images):
            img_byte_arr = io.BytesIO()
            img.save(img_byte_arr, format="jpeg")
            payload = img_byte_arr.getvalue()
            success = False
            for _ in range(3):
                try:
                    response = self.session.post(self.url, files={"request": payload})
                    response = response.json()
                    if "bboxes" not in response:
                        raise Exception(str(response))
                    res.append([{
                        "type": DLA_CLASSES[int(ty)].lower(),
                        "type_idx": ty,
                        "bbox": [left, t, r, b,],
                        "score": s
                    } for left, t, r, b, s, ty in response["bboxes"]])
                    success = True
                    break
                except requests.RequestException as e:
                    # Recreate the session only for transport-level failures.
                    logging.exception(e)
                    self.session = requests.Session()
                except Exception as e:
                    logging.exception(e)
            if not success and len(res) <= i:
                res.append([])
        return res


class LayoutRecognizer:
    """Remote-only DLA adapter compatible with parser-side layout calls."""

    garbage_layouts = ["footer", "header", "reference"]

    def __init__(self, domain):
        del domain
        dla_url = (os.environ.get("DEEPDOC_URL") or os.environ.get("TENSORRT_DLA_SVR") or "").strip()
        if not dla_url:
            raise RuntimeError("DEEPDOC_URL environment variable is required for DLA")
        self.client = DLAClient(dla_url)

    @staticmethod
    def sort_Y_firstly(arr, threshold):
        def _key(item):
            return (round(item["top"] / max(threshold, 1e-6)) if threshold else item["top"], item["x0"])

        return sorted(arr, key=_key)

    @staticmethod
    def overlapped_area(a, b, ratio=True):
        tp, btm, x0, x1 = a["top"], a["bottom"], a["x0"], a["x1"]
        if b["x0"] > x1 or b["x1"] < x0 or b["bottom"] < tp or b["top"] > btm:
            return 0
        x0_ = max(b["x0"], x0)
        x1_ = min(b["x1"], x1)
        y0_ = max(b["top"], tp)
        y1_ = min(b["bottom"], btm)
        area = max(0, x1_ - x0_) * max(0, y1_ - y0_)
        if not ratio:
            return area
        base = max((x1 - x0) * (btm - tp), 1e-6)
        return area / base

    @classmethod
    def find_overlapped_with_threshold(cls, box, layouts, thr=0.4):
        matched_idx = None
        max_overlap = thr
        for idx, layout in enumerate(layouts):
            overlap = max(cls.overlapped_area(box, layout), cls.overlapped_area(layout, box))
            if overlap >= max_overlap:
                matched_idx = idx
                max_overlap = overlap
        return matched_idx

    @staticmethod
    def layouts_cleanup(bxs, lts):
        del bxs
        return lts

    def __call__(self, image_list, ocr_res, scale_factor=3, thr=0.2, batch_size=16, drop=True):
        del thr, batch_size

        layouts = self.client.predict(image_list)
        assert len(image_list) == len(ocr_res) == len(layouts)

        boxes = []
        garbages = {}
        page_layout = []

        for pn, lts in enumerate(layouts):
            bxs = ocr_res[pn]
            normalized_layouts = [
                {
                    "type": b["type"],
                    "score": float(b["score"]),
                    "x0": b["bbox"][0] / scale_factor,
                    "x1": b["bbox"][2] / scale_factor,
                    "top": b["bbox"][1] / scale_factor,
                    "bottom": b["bbox"][3] / scale_factor,
                    "page_number": pn,
                }
                for b in lts
                if float(b["score"]) >= 0.4 or b["type"] not in self.garbage_layouts
            ]
            avg_h = np.mean([lt["bottom"] - lt["top"] for lt in normalized_layouts]) if normalized_layouts else 0
            normalized_layouts = self.sort_Y_firstly(normalized_layouts, avg_h / 2 if avg_h > 0 else 0)
            normalized_layouts = self.layouts_cleanup(bxs, normalized_layouts)
            page_layout.append(normalized_layouts)

            def _is_garbage(b):
                return "(cid:" in b.get("text", "")

            def find_layout(layout_type):
                nonlocal bxs
                layouts_of_type = [lt for lt in normalized_layouts if lt["type"] == layout_type]
                i = 0
                while i < len(bxs):
                    if bxs[i].get("layout_type"):
                        i += 1
                        continue
                    if _is_garbage(bxs[i]):
                        bxs.pop(i)
                        continue

                    matched = self.find_overlapped_with_threshold(bxs[i], layouts_of_type, thr=0.4)
                    if matched is None:
                        bxs[i]["layout_type"] = ""
                        i += 1
                        continue

                    layouts_of_type[matched]["visited"] = True
                    keep_feats = [
                        layouts_of_type[matched]["type"] == "footer" and bxs[i]["bottom"] < image_list[pn].size[1] * 0.9 / scale_factor,
                        layouts_of_type[matched]["type"] == "header" and bxs[i]["top"] > image_list[pn].size[1] * 0.1 / scale_factor,
                    ]
                    if drop and layouts_of_type[matched]["type"] in self.garbage_layouts and not any(keep_feats):
                        garbages.setdefault(layouts_of_type[matched]["type"], []).append(bxs[i]["text"])
                        bxs.pop(i)
                        continue

                    bxs[i]["layoutno"] = f"{layout_type}-{matched}"
                    bxs[i]["layout_type"] = layouts_of_type[matched]["type"] if layouts_of_type[matched]["type"] != "equation" else "figure"
                    i += 1

            for layout_type in ["footer", "header", "reference", "figure caption", "table caption", "title", "table", "text", "figure", "equation"]:
                find_layout(layout_type)

            for i, layout in enumerate([lt for lt in normalized_layouts if lt["type"] in ["figure", "equation"]]):
                if layout.get("visited"):
                    continue
                layout = dict(layout)
                layout.pop("type", None)
                layout["text"] = ""
                layout["layout_type"] = "figure"
                layout["layoutno"] = f"figure-{i}"
                bxs.append(layout)

            boxes.extend(bxs)

        garbag_set = set()
        for values in garbages.values():
            for text, count in Counter(values).items():
                if count > 1:
                    garbag_set.add(text)

        return [b for b in boxes if b["text"].strip() not in garbag_set], page_layout


AscendLayoutRecognizer = LayoutRecognizer


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser()
    parser.add_argument('--ip', type=str, help="Server's IP")
    parser.add_argument('--port', type=int, default=11234, help="Server's port")
    parser.add_argument('--image', type=str, help='Input image file')
    args = parser.parse_args()
    return args


if __name__ == "__main__":
    import cv2
    from deepdoc.servers.dla.yolov10_to_tensor.utils import vis
    args = parse_args()
    cli = DLAClient(f"http://{args.ip}:{args.port}")
    img = PILImage.open(args.image, mode='r')
    draw = cv2.imread(args.image)
    preds = cli.predict([img.convert("RGB")])
    final_boxes = [p["bbox"] for p in preds[0]]
    final_scores = [p["score"] for p in preds[0]]
    final_cls_inds = [p["type_idx"] for p in preds[0]]

    origin_img = vis(draw, final_boxes, final_scores, final_cls_inds,
                     conf=0.1, class_names=DLA_CLASSES)

    cv2.imwrite(args.image + ".o.jpg", draw)
