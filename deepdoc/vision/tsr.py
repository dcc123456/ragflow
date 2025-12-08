#  Licensed under the Apache License, Version 2.0 (the "License");
#  you may not use this file except in compliance with the License.
#  You may obtain a copy of the License at
#
#      http://www.apache.org/licenses/LICENSE-2.0
#
#  Unless required by applicable law or agreed to in writing, software
#  distributed under the License is distributed on an "AS IS" BASIS,
#  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
#  See the License for the specific language governing permissions and
#  limitations under the License.
#
import argparse
import os
import random
import re
import math
import cv2
import numpy as np
from PIL import ImageDraw
from PIL import Image
import torch
from common.file_utils import traversal_files, get_project_base_directory
from deepdoc.vision import Recognizer


DEBUG=True


class TableStructureRecognizer(Recognizer):

    def __init__(self):
        if os.environ.get("TENSORRT_TSR_SVR"):
            from deepdoc.vision.tsr_cli import TSRClient
            self.model = TSRClient(os.environ["TENSORRT_TSR_SVR"])
        else:
            if os.environ.get("TABLE_STRUCTURE_RECOGNIZER_TYPE", "") == "ascend":
                pass
            else:
                with torch.amp.autocast(device_type="cuda"):
                    from ultralytics import YOLO
                    self.model = YOLO(os.path.join(get_project_base_directory(), "rag/res/deepdoc/tsr.pt"))

    def __call__(self, images, **kwargs):
        cell_bbx_list = []

        if os.environ.get("TABLE_STRUCTURE_RECOGNIZER_TYPE", "") == "ascend":
            thr = 0.4
            tbls = self._run_ascend_tsr(images, thr)
            for tbl in tbls:
                cell_bbx = []
                for table in tbl: 
                    bbox = table["bbox"]
                    left, top, right, bottom, sc = bbox[0], bbox[1], bbox[2], bbox[3], table["score"]
                    cell_bbx.append((left, top, right, bottom, sc))
                cell_bbx_list.append(cell_bbx)
        else:
            BS = 1
            for bs in range(0, len(images), BS):
                preds = self.model.predict(images[bs:bs + BS], imgsz=640, conf=0.4, stream=False)
                for pred, img in zip(preds, images[bs:bs + BS]):
                    w, h = img.size
                    bboxs =  pred.boxes.data.tolist()
                    cell_bbx_list.append([(left, top, right, bottom, sc) for left, top, right, bottom, sc, _ in bboxs])

        res = []
        for n, cells in enumerate(cell_bbx_list):
            cells = [{"x0": x0, "top": y0, "x1": x1, "bottom": y1, "width": x1 - x0, "height": y1 - y0, "score": sc} for
                     x0, y0, x1, y1, sc in cells if x1 > x0 and y1 > y0]
            if not cells:
                continue
            min_height = max(3, np.min([c["height"] for c in cells]))
            min_width = max(5, np.min([c["width"] for c in cells]))
            cells = Recognizer.sort_Y_firstly(cells, min_height / 1.1)
            if DEBUG:
                print(cells, "||", len(cells))

            # split overlapped horizantally

            def split_merge4overlapped():
                nonlocal cells
                i = 0
                while i < len(cells) - 1:
                    ov1 = Recognizer.overlapped_area(cells[i], cells[i + 1], True)
                    ov2 = Recognizer.overlapped_area(cells[i + 1], cells[i], True)
                    ov = max(ov1, ov2)
                    if ov < 0.3:
                        i += 1
                        continue

                    if False and ov > 0.9: # one include the other
                        w1, h1 = cells[i]["width"], cells[i]["height"]
                        w2, h2 = cells[i+1]["width"], cells[i+1]["height"]
                        if (ov1 < ov2 and cells[i]["score"] < cells[i+1]["score"]) or (ov1 > ov2 and cells[i]["score"] > cells[i+1]["score"]):
                            big,small = cells[i], cells[i+1]
                            if ov1 > ov2:
                                big,small = cells[i+1], cells[i]
                            if abs(w1-w2)/max(w1, w2) > abs(h1-h2)/max(h1, h2):# slice vertically
                                if abs(big["x0"]-small["x0"]) > abs(big["x1"]-small["x1"]):
                                    big["x1"] = small["x0"]
                                else:
                                    big["x0"] = small["x1"]
                                big["width"] = big["x1"] - big["x0"]
                            else:# slice horizontally
                                if abs(big["top"]-small["top"]) > abs(big["bottom"]-small["bottom"]):
                                    big["bottom"] = small["top"]
                                else:
                                    big["top"] = small["bottom"]
                                big["height"] = big["bottom"] - big["top"]
                            continue

                    cells[i]["x0"] = min(cells[i + 1]["x0"], cells[i]["x0"])
                    cells[i]["x1"] = max(cells[i + 1]["x1"], cells[i]["x1"])
                    cells[i]["top"] = min(cells[i + 1]["top"], cells[i]["top"])
                    cells[i]["bottom"] = max(cells[i + 1]["bottom"], cells[i]["bottom"])
                    cells[i]["score"] = max(cells[i]["score"], cells[i+1]["score"] )
                    cells[i]["width"] = cells[i]["x1"] - cells[i]["x0"]
                    cells[i]["height"] = cells[i]["bottom"] - cells[i]["top"]
                    cells.pop(i + 1)

            split_merge4overlapped()
            cells = Recognizer.sort_X_firstly(cells, min_width / 1.1)
            split_merge4overlapped()

            mean_height = np.median([c["height"] for c in cells])
            min_height = max(3, np.min([c["height"] for c in cells]))
            mean_width = np.median([c["width"] for c in cells])
            min_width = max(5, np.min([c["width"] for c in cells]))

            def borders(key1, key2, distance):
                nonlocal cells
                rows_y = [int(c[key1]) for c in cells]
                rows_y.extend([int(c[key2]) for c in cells])
                rows_y = sorted(rows_y)
                row_lines = {}
                i = 1
                avg = [rows_y[0] if rows_y else 0]
                while i < len(rows_y):
                    if rows_y[i] - np.median(avg) >= distance:
                        y = np.median(avg)
                        row_lines[y] = len(avg)
                        avg = [rows_y[i]]
                    else:
                        avg.append(rows_y[i])
                    i += 1
                if avg:
                    row_lines[np.median(avg)] = len(avg)
                return sorted(row_lines.items(), key=lambda x: x[0])

            col_lines = borders("x0", "x1", mean_width / 3)
            row_lines = borders("top", "bottom", mean_height / 3)
            if col_lines[0][0] >= mean_width:  # stich right
                for c in cells:
                    if abs(c["x0"] - col_lines[0][0]) < min_width / 2:
                        c["x0"] = min_width / 2
                col_lines = borders("x0", "x1", min(5, mean_width / 5))
            # if row_lines[0][0] > 2 * mean_height: #stich top
            #    row_lines.insert(0, (row_lines[0][0]-mean_height, 1))
            if images[n].size[1] - row_lines[-1][0] > 2 * mean_height:  # stich bottom
                row_lines.append((row_lines[-1][0] + mean_height, 1))
            ## supplements missing cells
            virtual_cells = []
            for i in range(len(row_lines) - 1):
                for j in range(len(col_lines) - 1):
                    virtual_cells.append({
                        "x0": col_lines[j][0], "x1": col_lines[j + 1][0],
                        "top": row_lines[i][0], "bottom": row_lines[i + 1][0],
                        "width": col_lines[j + 1][0] - col_lines[j][0],
                        "height": row_lines[i + 1][0] - row_lines[i][0]
                    })
            for v in virtual_cells:
                for c in cells:
                    if Recognizer.overlapped_area(v, c, True) >= 0.3:
                        break
                else:
                    cells.append(v)

            def cross_boders(left, right, borders, thr):
                c = 0
                for b, _ in borders:
                    if b <= left:
                        continue
                    if b >= right:
                        continue
                    if min(abs(b - left), abs(b - right)) < thr:
                        continue
                    c += 1
                return c

            cells = Recognizer.sort_Y_firstly(cells, min_height * 0.75)
            for c in cells:
                span = cross_boders(c["x0"], c["x1"], col_lines, min_width*.8)
                if span > 0:
                    c["colspan"] = span + 1
                span = cross_boders(c["top"], c["bottom"], row_lines, min_height*.8)
                if span > 0:
                    c["rowspan"] = span + 1

            if DEBUG:
                print(cells, "<||>", len(cells))
            res.append(cells)
        return res

    @staticmethod
    def is_caption(bx):
        patt = [
            r"[图表]+[ 0-9:：]{2,}"
        ]
        if any([re.match(p, bx["text"].strip()) for p in patt]) \
                or bx["layout_type"].find("caption") >= 0:
            return True
        return False

    @staticmethod
    def construct_table(boxes, is_english, ocr_model=None, with_image=False, img_cells_pairs=[]):
        cap = ""
        i = 0

        def ocr(img, cell):
            nonlocal ocr_model
            if not ocr_model:
                return ""
            left, t, r, b = cell["_x0"], cell["_top"], cell["_x1"], cell["_bottom"]
            img = img.crop((left, t, r, b))
            bxs = ocr_model.detect(np.array(img))
            txt = ""
            for b, _ in bxs:
                if not (b[0][0] <= b[1][0] and b[0][1] <= b[-1][1]):
                    continue
                lft, r, t, bo = b[0][0], b[1][0], b[0][1], b[-1][1]
                if lft >= r  or t >= bo:
                    continue
                if re.search(r"[a-zA-Z,:;'!.]{2,}$", txt):
                    txt += " "
                txt += ocr_model.recognize_batch([np.array(img.crop((lft, t, r, bo)))])[0]
            return txt

        while i < len(boxes):
            if TableStructureRecognizer.is_caption(boxes[i]):
                if is_english:
                    cap += " "
                cap += boxes[i]["text"]
                boxes.pop(i)
                i -= 1
            i += 1

        st, ed = -1, 0
        for i, (img, cells) in enumerate(img_cells_pairs):
            x = 0
            for b in boxes:
                if not b["text"]:
                    continue
                ii = Recognizer.find_overlapped_with_threshold(b, cells, 0.85)
                if ii is None:
                    continue
                if re.search(r"[a-zA-Z,:;'!.]{2,}$", cells[ii]["text"]):
                    cells[ii]["text"] += " "
                cells[ii]["text"] += b["text"]
                x += 1
            if x == 0 and st > -1:
                ed = i
                break
            if x > 0 > st:
                st = i

        if st < 0:
            if not with_image:
                return ""
            return None, None

        html = "<table>"
        if cap:
            html += f"<caption>{cap}</caption>"
        for i, (img, cells) in enumerate(img_cells_pairs[st: max(ed, st+1)]):
            html += "<tr>"
            for j, c in enumerate(cells):
                if j > 0 and (cells[j]["top"] >= cells[j - 1]["bottom"]-2 or cells[j]["x1"] - 2 <= cells[j - 1]["x0"]):
                    html += "</tr><tr>"
                html += "<td "
                if "colspan" in c:
                    html += f" colspan={c['colspan']}"
                if "rowspan" in c:
                    html += f" rowspan={c['rowspan']}"
                txt = c.get("text", "")
                if not txt:
                    txt = ocr(img, c)
                html += ">"
                html += txt
                html += "</td>"

        html += "</tr></table>"
        if not with_image:
            return html
        else:
            return img, html


    def _run_ascend_tsr(self, image_list, thr=0.2, batch_size=16):
        from ais_bench.infer.interface import InferSession

        model_dir = os.path.join(get_project_base_directory(), "rag/res/deepdoc")
        model_file_path = os.path.join(model_dir, "tsr.om")

        if DEBUG:
            print(f"using ascend model {model_file_path=}", flush=True)

        if not os.path.exists(model_file_path):
            raise ValueError(f"Model file not found: {model_file_path}")

        device_id = int(os.getenv("ASCEND_LAYOUT_RECOGNIZER_DEVICE_ID", 0))
        session = InferSession(device_id=device_id, model_path=model_file_path)

        images = [np.array(im) if not isinstance(im, np.ndarray) else im for im in image_list]
        results = []

        conf_thr = max(thr, 0.08)

        def preprocess(image_list):
            inputs = []
            hh, ww = 640, 640
            for img in image_list:
                h, w = img.shape[:2]
                img = cv2.cvtColor(img, cv2.COLOR_BGR2RGB)
                img = cv2.resize(np.array(img).astype('float32'), (ww, hh))
                # Scale input pixel values to 0 to 1
                img /= 255.0
                img = img.transpose(2, 0, 1)
                img = img[np.newaxis, :, :, :].astype(np.float32)
                inputs.append({"image": img, "scale_factor": [w/ww, h/hh]})
            return inputs

        def postprocess(boxes, inputs, thr):
            def xywh2xyxy(x):
                # [x, y, w, h] to [x1, y1, x2, y2]
                y = np.copy(x)
                y[:, 0] = x[:, 0] - x[:, 2] / 2
                y[:, 1] = x[:, 1] - x[:, 3] / 2
                y[:, 2] = x[:, 0] + x[:, 2] / 2
                y[:, 3] = x[:, 1] + x[:, 3] / 2
                return y

            def compute_iou(box, boxes):
                # Compute xmin, ymin, xmax, ymax for both boxes
                xmin = np.maximum(box[0], boxes[:, 0])
                ymin = np.maximum(box[1], boxes[:, 1])
                xmax = np.minimum(box[2], boxes[:, 2])
                ymax = np.minimum(box[3], boxes[:, 3])

                # Compute intersection area
                intersection_area = np.maximum(0, xmax - xmin) * np.maximum(0, ymax - ymin)

                # Compute union area
                box_area = (box[2] - box[0]) * (box[3] - box[1])
                boxes_area = (boxes[:, 2] - boxes[:, 0]) * (boxes[:, 3] - boxes[:, 1])
                union_area = box_area + boxes_area - intersection_area

                # Compute IoU
                iou = intersection_area / union_area

                return iou

            def iou_filter(boxes, scores, iou_threshold):
                sorted_indices = np.argsort(scores)[::-1]

                keep_boxes = []
                while sorted_indices.size > 0:
                    # Pick the last box
                    box_id = sorted_indices[0]
                    keep_boxes.append(box_id)

                    # Compute IoU of the picked box with the rest
                    ious = compute_iou(boxes[box_id, :], boxes[sorted_indices[1:], :])

                    # Remove boxes with IoU over the threshold
                    keep_indices = np.where(ious < iou_threshold)[0]

                    # print(keep_indices.shape, sorted_indices.shape)
                    sorted_indices = sorted_indices[keep_indices + 1]

                return keep_boxes

            boxes = np.squeeze(boxes).T
            # Filter out object confidence scores below threshold
            scores = np.max(boxes[:, 4:], axis=1)
            boxes = boxes[scores > thr, :]
            scores = scores[scores > thr]
            if len(boxes) == 0:
                return []

            # Get the class with the highest confidence
            class_ids = np.argmax(boxes[:, 4:], axis=1)
            boxes = boxes[:, :4]
            input_shape = np.array([inputs["scale_factor"][0], inputs["scale_factor"][1], inputs["scale_factor"][0], inputs["scale_factor"][1]])
            boxes = np.multiply(boxes, input_shape, dtype=np.float32)
            boxes = xywh2xyxy(boxes)

            unique_class_ids = np.unique(class_ids)
            indices = []
            for class_id in unique_class_ids:
                class_indices = np.where(class_ids == class_id)[0]
                class_boxes = boxes[class_indices, :]
                class_scores = scores[class_indices]
                class_keep_boxes = iou_filter(class_boxes, class_scores, 0.2)
                indices.extend(class_indices[class_keep_boxes])

            labels = [
                "table",
                "table column",
                "table row",
                "table column header",
                "table projected row header",
                "table spanning cell",
            ]

            return [{
                "type": labels[class_ids[i]].lower(),
                "bbox": [float(t) for t in boxes[i].tolist()],
                "score": float(scores[i])
            } for i in indices]

        batch_loop_cnt = math.ceil(float(len(images)) / batch_size)
        for bi in range(batch_loop_cnt):
            s = bi * batch_size
            e = min((bi + 1) * batch_size, len(images))
            batch_images = images[s:e]

            inputs_list = preprocess(batch_images)
            for ins in inputs_list:
                feeds = []
                feeds.append(ins["image"])
                output_list = session.infer(feeds=feeds, mode="static")
                bb = postprocess(output_list, ins, conf_thr)
                results.append(bb)
        return results


def draw_box(im, bboxes):
    draw_thickness = min(im.size) // 320
    draw = ImageDraw.Draw(im)

    for (xmin, ymin, xmax, ymax) in bboxes:
        xmin += 1
        ymin += 1
        xmax -= 1
        ymax -= 1
        draw.line(
            [(xmin, ymin), (xmin, ymax), (xmax, ymax), (xmax, ymin),
             (xmin, ymin)],
            width=draw_thickness,
            fill=(random.randint(0, 256), random.randint(0, 256), random.randint(0, 256)))
    return im


def main(args):
    if not os.path.exists(args.output_dir):
        os.mkdir(args.output_dir)
    
    tsr = TableStructureRecognizer()
    fnms = []
    if os.path.isdir(args.inputs):
        for fnm in traversal_files(args.inputs):
            fnms.append(fnm)
    else:
        fnms.append(args.inputs)


    images = []
    for fnm in fnms:
        images.append(Image.open(fnm).convert('RGB'))
        print(fnm)
    cells = tsr(images)

    for i, (fig, cell) in enumerate(zip(images, cells)):
        fig = draw_box(fig, [[t["x0"], t["top"], t["x1"], t["bottom"]] for t in cell])
        fig.save(os.path.join(args.output_dir, os.path.split(fnms[i])[-1] + f".{i}.jpg"), quality=95)


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument('--inputs',
                        help="Directory where to store images or PDFs, or a file path to a single image or PDF",
                        required=True)
    parser.add_argument('--model',
                        help="The path of model.",
                        required=True)
    parser.add_argument('--output_dir', help="Directory where to store the output images. Default: './layouts_outputs'",
                        default="./tables_outputs")

    args = parser.parse_args()
    main(args)

