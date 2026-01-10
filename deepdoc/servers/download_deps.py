#!/usr/bin/env python3
"""
DeepDoc Dependencies Downloader

IMPORTANT REQUIREMENTS:
1. Must be run from project root directory (ragflow_enterprise/)
2. huggingface.co must be accessible (set up proxy/VPN if needed)

Usage:
    cd /path/to/ragflow_enterprise
    python deepdoc/servers/download_deps.py

If download fails due to network issues:
1. Check your proxy settings: echo $http_proxy $https_proxy
2. Verify huggingface.co is accessible: curl -I https://huggingface.co
3. Try again later or use alternative download methods
"""

# PEP 723 metadata
# /// script
# requires-python = ">=3.11,<3.16"
# dependencies = [
#   "paddleocr>=2.10.0,<3.0.0",
# ]
# ///

import shutil
import sys
import urllib.request
from pathlib import Path


def check_prerequisites():
    """Check if running conditions are met."""
    errors = []

    # Check if we're in project root directory
    # Project root contains: api/, deepdoc/, web/, docker/, pyproject.toml, etc.
    current_dir = Path.cwd()
    required_items = ["api", "deepdoc", "web", "docker", "pyproject.toml"]

    missing = [item for item in required_items if not (current_dir / item).exists()]
    if missing:
        errors.append(
            f"Must run from project root directory!\n"
            f"Current directory: {current_dir}\n"
            f"Missing items: {', '.join(missing)}\n"
            f"Please run: cd /path/to/ragflow_enterprise && python deepdoc/servers/download_deps.py"
        )

    # Check if huggingface.co is accessible
    try:
        import socket
        socket.setdefaulttimeout(10)
        with urllib.request.urlopen("https://huggingface.co", timeout=10) as response:
            if response.status != 200:
                errors.append(
                    f"huggingface.co is not accessible (status: {response.status})!\n"
                    "Please set up proxy/VPN and try again.\n"
                    "Example: export https_proxy=http://127.0.0.1:7890"
                )
    except Exception as e:
        errors.append(
            f"huggingface.co is not accessible!\n"
            f"Error: {e}\n"
            "Please set up proxy/VPN and try again.\n"
            "Example: export https_proxy=http://127.0.0.1:7890"
        )

    if errors:
        print("=" * 60, file=sys.stderr)
        print("ERROR: Prerequisites not met!", file=sys.stderr)
        print("=" * 60, file=sys.stderr)
        for error in errors:
            print(error, file=sys.stderr)
            print("-" * 60, file=sys.stderr)
        sys.exit(1)

    print("✓ Prerequisites check passed")
    print(f"  - Current directory: {current_dir}")
    print("  - huggingface.co: accessible")
    print()

# UV package manager
UV_URL = "https://github.com/astral-sh/uv/releases/download/0.9.18/uv-x86_64-unknown-linux-gnu.tar.gz"

# TensorRT for DLA and TSR
# Ubuntu 24.04 to match base image
# CUDA 12.8 to match inf24 cluster
TENSORRT_URL = "https://developer.nvidia.com/downloads/compute/machine-learning/tensorrt/10.8.0/local_repo/nv-tensorrt-local-repo-ubuntu2404-10.8.0-cuda-12.8_1.0-1_amd64.deb"

# YOLOv10 for DLA (Document Layout Analysis)
YOLOV10_URL = "https://github.com/THU-MIG/yolov10/archive/refs/heads/main.zip"

# ONNX models from HuggingFace (for CPU inference)
HUGGINGFACE_BASE_URL = "https://huggingface.co/InfiniFlow/deepdoc/resolve/main"
ONNX_MODELS = [
    "layout.onnx",
]

# DLA and TSR models from inf26:infiniflow_enterprise/ragflow/deepdoc/servers/tsr
TENSORRT_MODELS = [
    "dla.trt",
    "tsr.trt",
]

# PaddleOCR v2.10.0 PP-OCRv4 models (for Chinese)
# These models are downloaded to ~/.paddleocr/whl/ by PaddleOCR
PADDLEOCR_BASE_URL = "https://paddleocr.bj.bcebos.com/PP-OCRv4/chinese"
PADDLEOCR_MODELS = [
    # Detection model
    [f"{PADDLEOCR_BASE_URL}/ch_PP-OCRv4_det_infer.tar", "ch_PP-OCRv4_det_infer.tar"],
    # Recognition model
    [f"{PADDLEOCR_BASE_URL}/ch_PP-OCRv4_rec_infer.tar", "ch_PP-OCRv4_rec_infer.tar"],
    # Classification model (text direction)
    ["https://paddleocr.bj.bcebos.com/dygraph_v2.0/ch/ch_ppocr_mobile_v2.0_cls_infer.tar", "ch_ppocr_mobile_v2.0_cls_infer.tar"],
]

# All URLs to download
# Each entry can be either:
# - A string URL (filename extracted from URL)
# - A list [url, filename] where filename is explicitly specified
URLS = [
    UV_URL,
    TENSORRT_URL,
    [YOLOV10_URL, "yolov10.zip"],  # Explicitly specify filename
]


def download_file(url_entry: str | list[str], target_dir: str = ".") -> str:
    """Download a file from URL to target directory.

    Args:
        url_entry: Either a URL string or a list [url, filename]
        target_dir: Directory to save the file

    Returns:
        Path to the downloaded file
    """
    if isinstance(url_entry, list):
        url, filename = url_entry[0], url_entry[1]
    else:
        url = url_entry
        filename = Path(url).name

    filepath = Path(target_dir) / filename

    if filepath.exists():
        print(f"✓ Already exists: {filename}")
        return str(filepath)

    print(f"Downloading {url}...")
    try:
        # Create target directory if it doesn't exist
        Path(target_dir).mkdir(parents=True, exist_ok=True)
        urllib.request.urlretrieve(url, filepath)
        print(f"✓ Downloaded: {filename}")
        return str(filepath)
    except Exception as e:
        print(f"✗ Failed to download {filename}: {e}")
        raise


def download_onnx_models() -> None:
    """Download ONNX models for CPU inference"""
    print("=" * 60)
    print("Downloading ONNX models for CPU inference...")
    print("=" * 60)

    for model_name in ONNX_MODELS:
        url = f"{HUGGINGFACE_BASE_URL}/{model_name}"
        print(f"\nModel: {model_name}")
        try:
            download_file(url)
        except Exception as e:
            print(f"Warning: Failed to download {model_name}: {e}")
            print("Continuing with other downloads...")

    print("\n" + "=" * 60)
    print("ONNX models download complete!")
    print("=" * 60)


def download_paddleocr_models() -> None:
    """Download PaddleOCR models to current directory.

    IMPORTANT: PaddleOCR 2.10.0 stores models in ~/.paddleocr/
    - Models are downloaded to ~/.paddleocr/whl/
    - Directory structure: det/ch/, rec/ch/, cls/
    - Docker COPY will copy this directory structure
    """

    print("Downloading PaddleOCR models...")
    print("Checking dependencies...")

    # Check and install PaddleOCR 2.10.0
    try:
        import paddleocr
        ocr_version = paddleocr.__version__
        print(f"✓ PaddleOCR {ocr_version} already installed")
    except ImportError:
        print("✗ PaddleOCR not installed. Installing PaddleOCR 2.10.0...")
        import subprocess
        subprocess.check_call([sys.executable, "-m", "pip", "install", "paddleocr==2.10.0"])
        import paddleocr
        print(f"✓ PaddleOCR {paddleocr.__version__} installed")

    # Create paddleocr directory structure
    paddleocr_home = Path.home() / ".paddleocr"
    paddleocr_whl = paddleocr_home / "whl"
    paddleocr_whl.mkdir(parents=True, exist_ok=True)

    # Download and extract models
    import tarfile

    # Define model types and their target directories
    model_configs = [
        ("ch_PP-OCRv4_det_infer.tar", "det", "ch"),
        ("ch_PP-OCRv4_rec_infer.tar", "rec", "ch"),
        ("ch_ppocr_mobile_v2.0_cls_infer.tar", "cls", None),  # cls has no language subdirectory
    ]

    for model_url, filename in PADDLEOCR_MODELS:
        print(f"Downloading {filename}...")
        tar_file = Path(filename)

        if not tar_file.exists():
            try:
                urllib.request.urlretrieve(model_url, filename)
                print(f"✓ Downloaded: {filename}")
            except Exception as e:
                print(f"✗ Failed to download {filename}: {e}")
                print("Continuing with other models...")
                continue

        # Find the target directory structure for this model
        target_config = None
        for config_filename, model_type, lang in model_configs:
            if config_filename == filename:
                target_config = (model_type, lang)
                break

        if not target_config:
            print(f"✗ Unknown model: {filename}")
            continue

        model_type, lang = target_config

        # Extract to the correct directory structure
        print(f"Extracting {filename}...")
        try:
            # Create target directory: ~/.paddleocr/whl/det/ch/ or ~/.paddleocr/whl/cls/
            if lang:
                target_dir = paddleocr_whl / model_type / lang
            else:
                target_dir = paddleocr_whl / model_type

            target_dir.mkdir(parents=True, exist_ok=True)

            # Extract tar file to target directory
            with tarfile.open(filename, 'r') as tar:
                tar.extractall(target_dir)

            # Clean up tar file
            tar_file.unlink()
            print(f"✓ Extracted to {target_dir}")
        except Exception as e:
            print(f"✗ Failed to extract {filename}: {e}")
            print("Continuing with other models...")
            continue

    # Create local .paddleocr directory for Docker COPY
    paddleocr_local = Path(".paddleocr")
    shutil.rmtree(paddleocr_local, ignore_errors=True)
    shutil.copytree(paddleocr_home, paddleocr_local)
    print("✓ PaddleOCR models downloaded to .paddleocr/")
    print("  Directory structure:")
    print("    .paddleocr/whl/det/ch/ch_PP-OCRv4_det_infer/")
    print("    .paddleocr/whl/rec/ch/ch_PP-OCRv4_rec_infer/")
    print("    .paddleocr/whl/cls/ch_ppocr_mobile_v2.0_cls_infer/")
    print("  Ready for offline deployment in Docker")




def main():
    """Main download function."""
    import sys

    # Check if we should skip prerequisites (for PaddleOCR-only download)
    skip_prereqs = '--skip-prereqs' in sys.argv

    if not skip_prereqs:
        # Check prerequisites first
        check_prerequisites()

    print("=" * 60)
    print("DeepDoc Dependencies Downloader")
    print("=" * 60)
    print("TensorRT: 10.8.0 (CUDA 12.8)")
    print("UV: 0.9.18")
    print("=" * 60)
    print()

    # Download GPU dependencies
    print("Downloading GPU dependencies...")
    for url in URLS:
        download_file(url)
        print()

    # Download ONNX models
    download_onnx_models()
    print()

    # Download PaddleOCR models
    try:
        download_paddleocr_models()
    except Exception as e:
        print(f"Warning: Failed to download PaddleOCR models: {e}")
        print("You can download them later or run this script again.")

    print()
    print("=" * 60)
    print("Download complete!")
    print("=" * 60)
    print("\nDownloaded files:")
    print("  - uv-x86_64-unknown-linux-gnu.tar.gz")
    print("  - nv-tensorrt-local-repo-ubuntu2404-10.8.0-cuda-12.8_1.0-1_amd64.deb")
    print("  - yolov10.zip (YOLOv10 source)")
    print("  - layout.onnx (DLA ONNX model)")
    print("  - tsr.onnx (TSR ONNX model)")
    print("  - .paddleocr/ (OCR models)")
    print("\nThese files are ready for Docker build.")


if __name__ == "__main__":
    main()
