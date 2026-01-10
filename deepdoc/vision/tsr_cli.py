import argparse
import io
import logging
from api.utils.api_utils import timeout
import numpy as np
import requests
from PIL import Image


class Dummy:
    def __init__(self, data):
        self.data = data


class Prediction:
    def __init__(self, boxes):
        self.boxes = boxes


class TSRClient:
    def __init__(self, http_ip_port):
        # Use new unified endpoint: /predict/tsr
        self.url = http_ip_port + "/predict/tsr"
        self.session = requests.Session()

    @timeout(18)
    def predict(self, images: list[Image], **kwargs):
        res = []
        for i, img in enumerate(images):
            img_byte_arr = io.BytesIO()
            img.save(img_byte_arr, format="jpeg")
            for _ in range(3):
                try:
                    response = self.session.post(self.url, files={"request": img_byte_arr.getvalue()})
                    response = response.json()
                    if "bboxes" not in response:
                        raise Exception(str(response))
                    res.append(Prediction(boxes=Dummy(data=np.array(response["bboxes"]))))
                    break
                except Exception as e:
                    logging.exception(e)
                    self.session = requests.Session()
            if i == len(res):
                res.append(Prediction(boxes=Dummy(data=np.array([]))))
        return res


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser()
    parser.add_argument('--ip', type=str, help="Server's IP")
    parser.add_argument('--port', type=int, default=11234, help="Server's port")
    parser.add_argument('--image', type=str, help='Input image file')
    args = parser.parse_args()
    return args


if __name__ == "__main__":
    import random
    import cv2
    args = parse_args()
    cli = TSRClient(f"http://{args.ip}:{args.port}")
    img = Image.open(args.image, mode='r')
    draw = cv2.imread(args.image)
    preds = cli.predict([img.convert("RGB")])
    bboxs = [(left, top, right, bottom) for left, top, right, bottom, sc, _ in preds[0].boxes.data.tolist()]
    print(bboxs)
    for x1, y1, x2, y2 in bboxs:
        color = [random.randint(0, 255) for _ in range(3)]
        cv2.rectangle(draw, (int(x1), int(y1)), (int(x2), int(y2)), color, 2)

    cv2.imwrite(args.image + ".o.jpg", draw)