#!/usr/bin/env python3

# PEP 723 metadata
# /// script
# requires-python = ">=3.10,<3.11"
# dependencies = [
# ]
# ///

import os
import urllib.request

# https://github.com/nvidia/tensorrt
urls = [
    "https://github.com/astral-sh/uv/releases/download/0.6.12/uv-x86_64-unknown-linux-gnu.tar.gz",
    "https://developer.nvidia.com/downloads/compute/machine-learning/tensorrt/10.9.0/local_repo/nv-tensorrt-local-repo-ubuntu2204-10.9.0-cuda-12.8_1.0-1_amd64.deb",
    "./tsr.engine",
]

for url in urls:
    filename = url.split("/")[-1]
    print(f"Downloading {url}...")
    if not os.path.exists(filename):
        urllib.request.urlretrieve(url, filename)
