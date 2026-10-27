#!/usr/bin/env python3
"""Collect EE parity goldens from a REAL Python EE docker (wire output).

This is the docker-HTTP golden collector for the EE parity suite: it captures
the wire output of a deployed Python EE service, so the Go parity tests prove
Go-EE == the EE docker rather than Go == a re-implementation of the EE code.
(The earlier in-process replayers collect_tsr_ee_golden.py and
collect_ocr_rec_ee_golden.py were removed once this HTTP collector was verified
to reproduce their goldens byte-identically.)

All inputs are PNG fixtures under internal/deepdoc/native/testdata (the
symlinked external testdata repo); the docker's own decoding is the reference.

⚠️ Engine-variant caveat (TSR): an EE docker may serve EITHER a .pt (PyTorch)
or a .onnx (ultralytics-ONNX) engine. The Python EE CPU docker serves tsr.pt
(PyTorch); the Go port ships tsr.onnx (the ONNX export of tsr_op17.onnx, loaded
by the static ORT build). ORT (.onnx) and PyTorch (.pt) differ by ~1 marginal
box per real-document page (the curated tsr_ee_table golden is identical under
both). The CORPUS goldens below are sourced from the actual Python EE docker
(HTTP /predict/tsr), i.e. the real Python EE reference output (.pt on CPU); the
Go port (tsr.onnx/ORT) vs these goldens therefore measures the ORT-vs-PyTorch
drop-in fidelity gap, accepted as an engine-level difference. The curated
tsr_ee_table golden is engine-invariant, so it is collected once and stays.

The EE docker (ragflow_enterprise/deepdoc/servers/deepdoc_svr.py) serves, by
default on port 8001 (the Go EE server; the Python EE docker, if used as the
golden source, serves on port 8000):

  POST /predict/dla                 -> {"bboxes": [[x1,y1,x2,y2,score,cls], ...]}
  POST /predict/tsr                 -> {"bboxes": [[x1,y1,x2,y2,score,cls], ...]}
  POST /predict/ocr (operator=rec)  -> {"output": [[[["text", score]]]]}
  POST /predict/ocr (operator=det)  -> {"output": [[[[[x,y], ...]]]]}   (optional)

The golden files mirror exactly what the Go tests already consume:

  testdata/<stem>.dla.ee.golden.json          {"bboxes": [...]}
  testdata/tsr_ee_parity/tsr_ee_table.golden.json      {"bboxes": [...]}
  testdata/<stem>.ocr_rec_ee.golden.json      {"output": [[[["text", score]]]]}
  testdata/<stem>.det.ee.golden.json          {"output": [[[[[x,y], ...]]]]}
  testdata/tsr_ee_parity/corpus/<stem>.golden.json    {"bboxes": [...]}

Usage:
  # default target: the locally published Python EE CPU docker on :8001
  python3 tools/scripts/collect_ee_docker_golden.py --only all

  # point at a different EE docker (e.g. the Python EE CPU container on :8000)
  python3 tools/scripts/collect_ee_docker_golden.py --base http://localhost:8000 --only all
"""

import argparse
import json
import os
import subprocess
import sys

REPO = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
TESTDATA = os.path.join(REPO, "internal/deepdoc/native/testdata")
# The TSR-EE fixtures live UNDER testdata (the symlinked external repo), not in
# a worktree-local internal/deepdoc/native/tsr_ee_parity directory — pointing at
# the latter silently SKIPs every TSR/corpus collection.
TSRPARITY = os.path.join(TESTDATA, "tsr_ee_parity")

DLA_PAGES = [
    "page0",
    "mp_textbook_en_p0",
    "mp_whitepaper_cn_p0",
    "mp_paper_eq_p0",
    "mp_zhtw_ent_p0",
    "dla_2510_figcap",
    "dla_bookrag_figcap",
    "dla_2510_eq",
    "dla_real_cn_report",
    "dla_real_zhtw",
    "dla_real_en_paper",
]
OCR_REC_LINES = [
    "line0",
    "line_cn",
    "line_en_bold",
    "line_en_italic",
    "line_en_serif",
    "line_mix",
    "line_num",
    "line_cn_long",
    "line_real_cn",
    "line_real_zhtw",
    "line_real_en",
]
TSR_IMG = "tsr_ee_table.png"
# Det (text detection) stems — the same set TestEquivalenceReportEE asserts
# against testdata/<stem>.det.ee.golden.json. The golden is the EE docker's own
# wire output (POST /predict/ocr?operator=det), pinning the Go EE build (which
# runs PaddleOCR's default det_db_box_thresh=0.6) to the EE docker, not the OSS
# 0.5 in-process TextDetector golden.
DET_STEMS = [
    "mp_arxiv_p0",
    "mp_cn_sm_p0",
    "mp_en_dense_p0",
    "mp_sec_p0",
    "mp_physics_p5",
]


def _post(path, img_path, operator=None, base="http://localhost:8001"):
    cmd = ["curl", "-s", "-m", "120", "-F", f"request=@{img_path}", base + path]
    if operator:
        cmd += ["-F", f"operator={operator}"]
    r = subprocess.run(cmd, capture_output=True, text=True, check=False)
    if r.returncode != 0:
        sys.exit(f"curl failed ({path} {img_path}): {r.stderr}")
    if not r.stdout.strip():
        sys.exit(f"empty response from {path} for {img_path}")
    return r.stdout


def _collect_dla(base):
    for stem in DLA_PAGES:
        img = os.path.join(TESTDATA, stem + ".png")
        if not os.path.exists(img):
            print(f"DLA  SKIP (missing image) {stem}")
            continue
        data = json.loads(_post("/predict/dla", img, base=base))
        out = os.path.join(TESTDATA, stem + ".dla.ee.golden.json")
        with open(out, "w") as f:
            json.dump({"bboxes": data["bboxes"]}, f)
        print(f"DLA  {stem:24s} boxes={len(data['bboxes'])} -> {out}")


def _collect_tsr(base):
    img = os.path.join(TSRPARITY, TSR_IMG)
    if not os.path.exists(img):
        print(f"TSR  SKIP (missing image) {TSR_IMG}")
        return
    data = json.loads(_post("/predict/tsr", img, base=base))
    out = os.path.join(TSRPARITY, "tsr_ee_table.golden.json")
    with open(out, "w") as f:
        json.dump({"bboxes": data["bboxes"]}, f)
    print(f"TSR  {TSR_IMG:24s} boxes={len(data['bboxes'])} -> {out}")


def _collect_ocr_rec(base):
    for stem in OCR_REC_LINES:
        img = os.path.join(TESTDATA, stem + ".png")
        if not os.path.exists(img):
            print(f"OCR  SKIP (missing image) {stem}")
            continue
        data = json.loads(_post("/predict/ocr", img, operator="rec", base=base))
        # data is already {"output": [[[["text", score]]]]} as consumed by
        # loadOCRRecEEGolden (Output[0][0][0][0] -> text).
        out = os.path.join(TESTDATA, stem + ".ocr_rec_ee.golden.json")
        with open(out, "w") as f:
            json.dump(data, f)
        try:
            text = data["output"][0][0][0][0]
        except (KeyError, IndexError, TypeError):
            text = "<unparseable>"
        print(f"OCR  {stem:24s} text={text!r} -> {out}")


def _collect_det(base):
    """Collect text-detection goldens from the EE docker.

    POST /predict/ocr?operator=det returns {"output": [[[[[x,y], ...]]]]}, which
    is byte-for-byte what RunDet(...).Wire() emits (the Go test consumes it via
    FlattenQuads). Dumping it verbatim keeps the Go EE det build pinned to the
    EE docker (box_thresh=0.6), matching how TestEquivalenceReportEE reads
    testdata/<stem>.det.ee.golden.json.
    """
    for stem in DET_STEMS:
        img = os.path.join(TESTDATA, stem + ".png")
        if not os.path.exists(img):
            print(f"DET  SKIP (missing image) {stem}")
            continue
        data = json.loads(_post("/predict/ocr", img, operator="det", base=base))
        # data is already {"output": [[[[[x,y], ...]]]]} as consumed by the
        # det golden reader (Output[][][][][2]float64).
        out = os.path.join(TESTDATA, stem + ".det.ee.golden.json")
        with open(out, "w") as f:
            json.dump(data, f)
        try:
            nboxes = len(data["output"][0][0]) if data.get("output") else 0
        except (KeyError, IndexError, TypeError):
            nboxes = 0
        print(f"DET  {stem:24s} boxes={nboxes} -> {out}")


def _collect_tsr_corpus(base):
    """Re-collect every corpus golden from the EE docker (PNG input), one
    <stem>.golden.json next to each <stem>.png under tsr_ee_parity/corpus/."""
    corpus = os.path.join(TSRPARITY, "corpus")
    if not os.path.isdir(corpus):
        print("TSR-corpus SKIP (no corpus dir)")
        return
    for name in sorted(os.listdir(corpus)):
        if not name.endswith(".png"):
            continue
        stem = name[: -len(".png")]
        img = os.path.join(corpus, name)
        data = json.loads(_post("/predict/tsr", img, base=base))
        out = os.path.join(corpus, stem + ".golden.json")
        with open(out, "w") as f:
            json.dump({"bboxes": data["bboxes"]}, f)
        print(f"TSR-corpus {stem:44s} boxes={len(data['bboxes'])}")


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--base", default=os.environ.get("EE_DOCKER_URL", "http://localhost:8001"))
    ap.add_argument("--only", choices=["dla", "tsr", "ocr_rec", "det", "corpus", "all"], default="all")
    args = ap.parse_args()
    if args.only in ("dla", "all"):
        _collect_dla(args.base)
    if args.only in ("tsr", "all"):
        _collect_tsr(args.base)
    if args.only in ("ocr_rec", "all"):
        _collect_ocr_rec(args.base)
    if args.only in ("det", "all"):
        _collect_det(args.base)
    if args.only in ("corpus", "all"):
        _collect_tsr_corpus(args.base)


if __name__ == "__main__":
    main()
