#!/usr/bin/env python3

# PEP 723 metadata
# /// script
# requires-python = ">=3.10,<3.11"
# dependencies = [
#   "huggingface-hub",
# ]
# ///

import os
import pathlib
import shutil
import urllib.request
from huggingface_hub import snapshot_download

urls = [
    "https://github.com/astral-sh/uv/releases/download/0.6.12/uv-x86_64-unknown-linux-gnu.tar.gz",
]

for url in urls:
    filename = url.split("/")[-1]
    print(f"Downloading {url}...")
    if not os.path.exists(filename):
        urllib.request.urlretrieve(url, filename)

script_dir = os.path.dirname(os.path.abspath(__file__))
hugging_dir = os.path.join(script_dir, "huggingface")
shutil.rmtree(hugging_dir, ignore_errors=True)
os.makedirs(hugging_dir)
all_repo_ids = ["BAAI/bge-m3", "BAAI/bge-large-zh-v1.5", "BAAI/bge-large-en-v1.5", "maidalun1020/bce-embedding-base_v1"]
for repo_id in all_repo_ids:
    snapshot_dir = snapshot_download(repo_id=repo_id, local_dir_use_symlinks=False)
    model_name = repo_id.split("/")[-1]
    model_dir = os.path.join(hugging_dir, model_name)
    os.mkdir(model_dir)
    extra_files = [item for item in pathlib.Path(snapshot_dir).iterdir() if item.is_file() and "onnx" not in item.name]
    for extra_file in extra_files:
        shutil.copy(str(extra_file), os.path.join(model_dir, extra_file.name))
    extra_dirs = [item for item in pathlib.Path(snapshot_dir).iterdir() if item.is_dir() and item.name != "onnx"]
    for extra_dir in extra_dirs:
        shutil.copytree(str(extra_dir), os.path.join(model_dir, extra_dir.name), dirs_exist_ok=True)
