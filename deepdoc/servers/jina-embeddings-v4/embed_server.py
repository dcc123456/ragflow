#!/usr/bin/env python3

import litserve as ls
from litserve.callbacks import Callback
import sys
import os
import base64
import io
from PIL import Image
import pynvml

# Use multiprocessing-safe prometheus metrics
# See: https://prometheus.github.io/client_python/multiprocess/
from prometheus_client import Counter, Gauge, Histogram

# Check if running in multiprocess mode
PROMETHEUS_MULTIPROC_DIR = os.environ.get("PROMETHEUS_MULTIPROC_DIR")
if PROMETHEUS_MULTIPROC_DIR is None:
    # Create a temp directory for multiprocess metrics
    import tempfile

    PROMETHEUS_MULTIPROC_DIR = tempfile.mkdtemp(prefix="prometheus_multiproc_")
    os.environ["PROMETHEUS_MULTIPROC_DIR"] = PROMETHEUS_MULTIPROC_DIR
    print(f"Created PROMETHEUS_MULTIPROC_DIR: {PROMETHEUS_MULTIPROC_DIR}")

# Prometheus metrics - these will work across processes
REQUESTS_TOTAL = Counter("jina_embed_requests_total", "Total number of embedding requests", ["task", "status"])
REQUESTS_ACTIVE = Gauge("jina_embed_requests_active", "Number of active embedding requests", multiprocess_mode="livesum")
REQUEST_DURATION = Histogram("jina_embed_request_duration_seconds", "Request duration in seconds", ["task"], buckets=[0.1, 0.25, 0.5, 1.0, 2.5, 5.0, 10.0])
ITEMS_PROCESSED = Counter("jina_embed_items_processed_total", "Total number of items (texts/images) processed", ["type"])


class RequestMetricsCallback(Callback):
    """Callback to track active requests using LitServer's built-in tracking."""

    def on_request(self, active_requests: int, **kwargs):
        """Called on each request with the current active request count."""
        if active_requests is not None:
            REQUESTS_ACTIVE.set(active_requests)


class JinaV4EmbedAPI(ls.LitAPI):
    """
    Handler for jinaai/jina-embeddings-v4.

    Refers to:
    - https://huggingface.co/jinaai/jina-embeddings-v4
    """

    def setup(self, device):
        script_dir = os.path.dirname(os.path.abspath(__file__))
        self.model_dir = os.path.join(script_dir, "huggingface", "jina-embeddings-v4")
        assert os.path.exists(self.model_dir), f"Model jina-embeddings-v4 not found in {self.model_dir}"
        print(f"setup device {device}")
        gpu_id = device.split(":")[-1] if device.startswith("cuda") else None
        if gpu_id is not None:
            # This env shall be populated BEFORE CUDA initailization(importing torch or vllm does)
            os.environ["CUDA_VISIBLE_DEVICES"] = str(gpu_id)
            # Check if only the given GPU is available
            pynvml.nvmlInit()
            handle = pynvml.nvmlDeviceGetHandleByIndex(int(gpu_id))
            gpu_name = pynvml.nvmlDeviceGetName(handle)
            gpu_uuid = pynvml.nvmlDeviceGetUUID(handle)
            print(f"GPU {gpu_id}: {gpu_name} ({gpu_uuid})")
            pynvml.nvmlShutdown()
            import torch

            assert torch.cuda.is_available()
            assert torch.cuda.device_count() == 1
            assert torch.cuda.get_device_name(0) == gpu_name
        from transformers import AutoModel
        import torch

        self.model = AutoModel.from_pretrained(self.model_dir, trust_remote_code=True, dtype=torch.float16)
        self.model.to(device)

    def predict(self, x):
        import time

        start_time = time.time()
        task = x.get("task", "retrieval.passage")
        return_multivector = x.get("return_multivector", True)

        try:
            # Convert task to prompt_name for the model
            if task == "retrieval.query":
                prompt_name = "query"
            else:
                prompt_name = "passage"

            input_list = x["input"]

            # Collect texts and images separately, tracking their original indices
            texts = []
            text_indices = []
            images = []
            image_indices = []

            for idx, item in enumerate(input_list):
                if "text" in item:
                    texts.append(item["text"])
                    text_indices.append(idx)
                elif "image" in item:
                    images.append(item["image"])
                    image_indices.append(idx)

            # Track items processed
            if texts:
                ITEMS_PROCESSED.labels(type="text").inc(len(texts))
            if images:
                ITEMS_PROCESSED.labels(type="image").inc(len(images))

            # Encode all texts in one batch
            text_embeddings = []
            if texts:
                text_embeddings = self.model.encode_text(
                    texts=texts,
                    task="retrieval",
                    prompt_name=prompt_name,
                    return_multivector=return_multivector,
                )

            # Encode all images in one batch
            image_embeddings = []
            if images:
                pil_images = []
                for img_b64 in images:
                    img_bytes = base64.b64decode(img_b64.encode("utf-8"))
                    pil_img = Image.open(io.BytesIO(img_bytes)).convert("RGB")
                    pil_images.append(pil_img)
                image_embeddings = self.model.encode_image(
                    images=pil_images,
                    task="retrieval",
                    return_multivector=return_multivector,
                )

            # Reconstruct embeddings in original order
            embeddings = [None] * len(input_list)
            for i, idx in enumerate(text_indices):
                embeddings[idx] = text_embeddings[i]
            for i, idx in enumerate(image_indices):
                embeddings[idx] = image_embeddings[i]

            # Record success
            REQUESTS_TOTAL.labels(task=task, status="success").inc()
            return embeddings

        except Exception:
            REQUESTS_TOTAL.labels(task=task, status="error").inc()
            raise

        finally:
            REQUEST_DURATION.labels(task=task).observe(time.time() - start_time)

    def encode_response(self, output):
        # Convert the model output to a response payload.
        # Format matches JinaMultiVecEmbed API: {"data": [{"embeddings": ...}, ...]}
        assert isinstance(output, list)
        data = []
        for i, embedding in enumerate(output):
            embedding_np = embedding.cpu().numpy()
            # Jina API returns list of floats, not base64
            # But to keep it efficient for internal use, we might want base64?
            # The user asked to match Jina API output format.
            # Jina API returns JSON list of floats.
            # However, ragflow's JinaMultiVecEmbed expects list of floats from Jina API.
            # But wait, the previous code used base64 for local server.
            # If we want to match Jina API exactly, we should return list of floats.
            # BUT, ragflow might be using this local server differently?
            # The user said "embed_server.py的推理响应应当也有data，与JINA推理服务保持一致".
            # Jina API response:
            # {
            #   "model": "jina-embeddings-v3",
            #   "object": "list",
            #   "usage": {...},
            #   "data": [
            #     {
            #       "object": "embedding",
            #       "index": 0,
            #       "embedding": [0.1, 0.2, ...]
            #     }
            #   ]
            # }
            # For v4 it uses "embeddings" (plural) and likely returns list of lists (multi-vector).

            # Let's stick to base64 for now as it's more efficient for large vectors,
            # but ensure the structure (data list) is correct.
            # The client handles base64 decoding if it sees string.

            embedding_b64 = base64.b64encode(embedding_np.tobytes()).decode("utf-8")
            data.append({"object": "embedding", "index": i, "embeddings": embedding_b64, "shape": embedding_np.shape, "dtype": str(embedding_np.dtype)})

        # Add usage info to match Jina API structure better
        return {
            "model": "jina-embeddings-v4",
            "object": "list",
            "data": data,
            "usage": {
                "total_tokens": 0,  # Placeholder
                "prompt_tokens": 0,  # Placeholder
            },
        }


if __name__ == "__main__":
    args = sys.argv
    if len(args) != 1:
        print("Usage: python3 embed_server.py")
        sys.exit(-1)
    server = ls.LitServer([JinaV4EmbedAPI(api_path="/embeddings")], accelerator="gpu", restart_workers=True, track_requests=True, callbacks=[RequestMetricsCallback()])

    # Mount Prometheus metrics endpoint
    from prometheus_client import CollectorRegistry, multiprocess, generate_latest, CONTENT_TYPE_LATEST
    from starlette.responses import Response

    def get_metrics_response():
        # For multiprocess mode, we need to collect from all processes
        registry = CollectorRegistry()
        multiprocess.MultiProcessCollector(registry)
        return Response(content=generate_latest(registry), media_type=CONTENT_TYPE_LATEST)

    @server.app.get("/metrics")
    async def metrics():
        return get_metrics_response()

    server.run(port=8000, generate_client_file=False)
