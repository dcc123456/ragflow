#!/usr/bin/env python3

import litserve as ls
import sys
import os
import base64
import io
from PIL import Image
import pynvml


class EmbedAPI(ls.LitAPI):
    """
    Handler for jinaai/jina-embeddings-v4.

    Refers to:
    - https://huggingface.co/jinaai/jina-embeddings-v4
    """
    def __init__(self):
        super().__init__()
        script_dir = os.path.dirname(os.path.abspath(__file__))
        self.model_dir = os.path.join(script_dir, "huggingface", "jina-embeddings-v4")
        assert os.path.exists(self.model_dir), f"Model jina-embeddings-v4 not found in {self.model_dir}"

    def setup(self, device):
        print(f"setup device {device}")
        gpu_id = device.split(':')[-1] if device.startswith('cuda') else None
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
            assert torch.cuda.device_count()==1
            assert torch.cuda.get_device_name(0)==gpu_name
        from transformers import AutoModel
        import torch
        self.model = AutoModel.from_pretrained(self.model_dir, trust_remote_code=True, dtype=torch.float16)
        self.model.to(device)

    def predict(self, x):
        texts = x.get("texts", [])
        images = x.get("images", [])
        return_multivector=x.get("return_multivector", True)
        embeddings = []
        if texts:
            prompt_name=x.get("prompt_name", "passage")
            if prompt_name not in ["passage", "query"]:
                prompt_name = "passage"
            embeddings = self.model.encode_text(
                texts=texts,
                task="retrieval",
                prompt_name=prompt_name,
                return_multivector=return_multivector,
            )
        elif images:
            assert isinstance(images, list)
            pil_images = []
            for img_b64 in images:
                # print(f'img_b64: {img_b64}')
                img_bytes = base64.b64decode(img_b64.encode('utf-8'))
                pil_img = Image.open(io.BytesIO(img_bytes)).convert("RGB")
                pil_images.append(pil_img)
            embeddings = self.model.encode_image(
                images=pil_images,
                task="retrieval",
                return_multivector=return_multivector,
            )
        return embeddings

    def encode_response(self, output):
        # Convert the model output to a response payload.
        assert isinstance(output, list)
        resp = []
        for embedding in output:
            embedding_np = embedding.cpu().numpy()
            embedding_b64 = base64.b64encode(embedding_np.tobytes()).decode("utf-8")
            resp.append({"data": embedding_b64, "shape": embedding_np.shape, "dtype": str(embedding_np.dtype)})
        return resp 

if __name__ == "__main__":
    args = sys.argv
    if len(args) != 1:
        print("Usage: python3 embed_server.py")
        sys.exit(-1)
    server = ls.LitServer(EmbedAPI(), accelerator="gpu")
    server.run(port=8000)
