#!/usr/bin/env python3
"""
Unified DeepDoc Model Server

Serves DLA (Document Layout Analysis), OCR (PaddleOCR), and TSR (Table Structure Recognition)
models on a single server using LitServe's multi-endpoint capability.

Endpoints:
- /predict/dla - Document layout analysis
- /predict/ocr - Text detection and recognition
- /predict/tsr - Table structure recognition

LitServe 0.2.11+ supports multiple LitAPIs passed as a list.

Usage:
    # Enable all endpoints (default)
    python deepdoc_svr.py --gpu

    # Disable TSR (enable OCR and DLA only)
    python deepdoc_svr.py --gpu --disable-tsr

    # Disable DLA and TSR (enable OCR only)
    python deepdoc_svr.py --gpu --disable-dla --disable-tsr
"""
import argparse
import logging
import os
import multiprocessing

# IMPORTANT: Force 'fork' multiprocessing context to avoid spawn issues with PaddleOCR
# This must be done before importing litserve
try:
    multiprocessing.set_start_method('fork', force=True)
except RuntimeError:
    pass  # Method already set

os.environ['PYTHONHASHSEED'] = '0'

import litserve as ls
from dla import DLAEndpoint
from ocr import OCREndpoint
from tsr import TSREndpoint

logging.basicConfig(
    level=logging.INFO,
    format='%(asctime)s - %(name)s - %(levelname)s - %(message)s'
)
logger = logging.getLogger(__name__)


def parse_args():
    parser = argparse.ArgumentParser(
        description="Unified DeepDoc Model Server",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="""
Examples:
  python deepdoc_svr.py --gpu                              # Enable all endpoints
  python deepdoc_svr.py --gpu --disable-tsr                # Disable TSR (OCR + DLA only)
  python deepdoc_svr.py --gpu --disable-dla --disable-tsr  # Enable OCR only
        """
    )
    parser.add_argument(
        '--gpu',
        action='store_true',
        help='Use GPU inference (TensorRT models for DLA/TSR, PaddlePaddle GPU for OCR)'
    )
    parser.add_argument(
        '--port',
        type=int,
        default=8000,
        help='Serving port (default: 8000)'
    )
    parser.add_argument(
        '--workers',
        type=int,
        default=1,
        help='Workers per device (default: 1)'
    )
    parser.add_argument(
        '--timeout',
        type=int,
        default=100,
        help='Request timeout in seconds (default: 100)'
    )

    # Endpoint disable flags (all enabled by default)
    parser.add_argument(
        '--disable-ocr',
        action='store_true',
        dest='disable_ocr',
        default=False,
        help='Disable OCR endpoint (Text Detection & Recognition)'
    )
    parser.add_argument(
        '--disable-dla',
        action='store_true',
        dest='disable_dla',
        default=False,
        help='Disable DLA endpoint (Document Layout Analysis)'
    )
    parser.add_argument(
        '--disable-tsr',
        action='store_true',
        dest='disable_tsr',
        default=False,
        help='Disable TSR endpoint (Table Structure Recognition)'
    )

    return parser.parse_args()


def main():
    args = parse_args()

    # Get enable flags from args (inverting disable flags)
    enable_ocr = not args.disable_ocr
    enable_dla = not args.disable_dla
    enable_tsr = not args.disable_tsr

    # Determine inference mode (default to CPU)
    use_gpu = args.gpu

    if use_gpu:
        dla_engine = '/app/dla/dla.trt'
        tsr_engine = '/app/tsr/tsr.trt'
        accelerator = 'gpu'
        logger.info("Using GPU inference (TensorRT models)")
    else:
        dla_engine = '/app/dla/layout.onnx'
        tsr_engine = '/app/tsr/tsr.pt'  # Use PyTorch .pt file for CPU
        accelerator = 'cpu'
        logger.info("Using CPU inference (DLA: ONNX, TSR: PyTorch, OCR: PaddleOCR)")

    logger.info("Initializing Unified DeepDoc Server...")
    logger.info(f"DLA Engine: {dla_engine if enable_dla else 'disabled'}")
    logger.info(f"TSR Engine: {tsr_engine if enable_tsr else 'disabled'}")
    logger.info(f"Accelerator: {accelerator}")

    # Initialize enabled model endpoints
    apis = []
    if enable_ocr:
        ocr_api = OCREndpoint(use_gpu=use_gpu)
        apis.append(ocr_api)
        logger.info("OCR model initialized successfully")

    if enable_dla:
        dla_api = DLAEndpoint(engine_path=dla_engine, use_gpu=use_gpu)
        apis.append(dla_api)
        logger.info("DLA model initialized successfully")

    if enable_tsr:
        tsr_api = TSREndpoint(engine_path=tsr_engine, use_gpu=use_gpu)
        apis.append(tsr_api)
        logger.info("TSR model initialized successfully")

    if not apis:
        logger.error("No endpoints enabled! All endpoints are disabled.")
        return

    # Log available endpoints
    logger.info("Available endpoints:")
    if enable_ocr:
        logger.info("  - POST /predict/ocr  (OCR - Text Detection & Recognition)")
    if enable_dla:
        logger.info("  - POST /predict/dla  (DLA - Document Layout Analysis)")
    if enable_tsr:
        logger.info("  - POST /predict/tsr  (TSR - Table Structure Recognition)")

    # Create server with multiple APIs
    # max_batch_size is set on each API object (OCREndpoint, DLAEndpoint, TSREndpoint)
    # following the new LitServe 0.3.x pattern

    if accelerator == 'cpu':
        server = ls.LitServer(
            lit_api=apis,
            accelerator='cpu',
            workers_per_device=1,
            timeout=args.timeout,
            track_requests=True,
            restart_workers=True,  # Automatically restart failed workers
        )
    else:
        server = ls.LitServer(
            lit_api=apis,
            accelerator=accelerator,
            workers_per_device=args.workers,
            timeout=args.timeout,
            track_requests=True,
            restart_workers=True,  # Automatically restart failed workers
        )

    logger.info(f"Starting server on port {args.port}...")
    server.run(port=args.port, log_level="warning")


if __name__ == "__main__":
    main()
