#
#  Copyright 2025 The InfiniFlow Authors. All Rights Reserved.
#
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
from functools import cmp_to_key


class Recognizer:
    """Lightweight geometry helpers for parser-side post-processing only."""

    def __init__(self, *_args, **_kwargs):
        raise RuntimeError("Local OCR/DLA/TSR inference is disabled in ragflow/parser. Use DEEPDOC_URL-backed clients instead.")

    @staticmethod
    def sort_Y_firstly(arr, threshold):
        def cmp(c1, c2):
            diff = c1["top"] - c2["top"]
            if abs(diff) < threshold:
                diff = c1["x0"] - c2["x0"]
            return diff

        return sorted(arr, key=cmp_to_key(cmp))

    @staticmethod
    def sort_X_firstly(arr, threshold):
        def cmp(c1, c2):
            diff = c1["x0"] - c2["x0"]
            if abs(diff) < threshold:
                diff = c1["top"] - c2["top"]
            return diff

        return sorted(arr, key=cmp_to_key(cmp))

    @staticmethod
    def sort_C_firstly(arr, thr=0):
        arr = Recognizer.sort_X_firstly(arr, thr)
        for i in range(len(arr) - 1):
            for j in range(i, -1, -1):
                if "C" not in arr[j] or "C" not in arr[j + 1]:
                    continue
                if arr[j + 1]["C"] < arr[j]["C"] or (arr[j + 1]["C"] == arr[j]["C"] and arr[j + 1]["top"] < arr[j]["top"]):
                    arr[j], arr[j + 1] = arr[j + 1], arr[j]
        return arr

    @staticmethod
    def sort_R_firstly(arr, thr=0):
        arr = Recognizer.sort_Y_firstly(arr, thr)
        for i in range(len(arr) - 1):
            for j in range(i, -1, -1):
                if "R" not in arr[j] or "R" not in arr[j + 1]:
                    continue
                if arr[j + 1]["R"] < arr[j]["R"] or (arr[j + 1]["R"] == arr[j]["R"] and arr[j + 1]["x0"] < arr[j]["x0"]):
                    arr[j], arr[j + 1] = arr[j + 1], arr[j]
        return arr

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

    @classmethod
    def find_overlapped(cls, box, layouts, naive=False):
        del naive
        return cls.find_overlapped_with_threshold(box, layouts, thr=1e-6)

    @staticmethod
    def find_horizontally_tightest_fit(box, layouts):
        matched_idx = None
        best_gap = None
        for idx, layout in enumerate(layouts):
            if layout["x0"] <= box["x0"] and layout["x1"] >= box["x1"]:
                gap = abs(layout["x0"] - box["x0"]) + abs(layout["x1"] - box["x1"])
                if best_gap is None or gap < best_gap:
                    best_gap = gap
                    matched_idx = idx
        return matched_idx

    @staticmethod
    def layouts_cleanup(boxes, layouts, *_args, **_kwargs):
        del boxes
        return layouts
