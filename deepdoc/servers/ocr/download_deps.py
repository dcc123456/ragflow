#!/usr/bin/env python3

# PEP 723 metadata
# /// script
# requires-python = ">=3.10,<3.11"
# dependencies = [
#   "paddlepaddle==3.0.0",
#   "paddleocr",
#   "setuptools",
# ]
# ///

import os
import shutil
import urllib.request
from paddleocr import PaddleOCR

urls = [
         "https://paddle-whl.bj.bcebos.com/stable/cu126/paddlepaddle-gpu/paddlepaddle_gpu-3.0.0-cp310-cp310-linux_x86_64.whl",
         "https://github.com/astral-sh/uv/releases/download/0.6.12/uv-x86_64-unknown-linux-gnu.tar.gz",
]

def download_model():
    # Download models to $HOME/.paddleocr, then move to $PWD/.paddleocr
    PaddleOCR(lang='ch')
    shutil.rmtree(".paddleocr", ignore_errors=True)
    shutil.move(os.path.expanduser("~/.paddleocr"), ".")


for url in urls:
    filename = url.split("/")[-1]
    print(f"Downloading {url}...")
    if not os.path.exists(filename):
        urllib.request.urlretrieve(url, filename)

download_model()
