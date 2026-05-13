import argparse
import io
import logging
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
