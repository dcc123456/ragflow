#!/usr/bin/env python3

# PEP 723 metadata
# /// script
# requires-python = ">=3.10,<3.15"
# dependencies = [
#   "huggingface-hub>=1.0.0,<2.0.0",
# ]
# ///

import os
import pathlib
import shutil
from huggingface_hub import snapshot_download

script_dir = os.path.dirname(os.path.abspath(__file__))
hugging_dir = os.path.join(script_dir, "huggingface")
shutil.rmtree(hugging_dir, ignore_errors=True)
os.makedirs(hugging_dir)
all_repo_ids = ["jinaai/jina-embeddings-v4",]
for repo_id in all_repo_ids:
    snapshot_dir = snapshot_download(repo_id=repo_id, local_dir_use_symlinks=False)
    model_name = repo_id.split("/")[-1]
    model_dir = os.path.join(hugging_dir, model_name)
    os.mkdir(model_dir)
    extra_files = [item for item in pathlib.Path(snapshot_dir).iterdir() if item.is_file() and 'onnx' not in item.name]
    for extra_file in extra_files:
        shutil.copy(str(extra_file), os.path.join(model_dir, extra_file.name))
    extra_dirs = [item for item in pathlib.Path(snapshot_dir).iterdir() if item.is_dir() and item.name != "onnx"]
    for extra_dir in extra_dirs:
        shutil.copytree(str(extra_dir), os.path.join(model_dir, extra_dir.name), dirs_exist_ok=True)
