#!/usr/bin/env python3

# PEP 723 metadata
# /// script
# requires-python = ">=3.10,<3.11"
# dependencies = [
#   "litserve",
#   "nvidia-ml-py",
#   "torch",
#   "vllm",
# ]
# ///

import litserve as ls
import sys
import os
import base64
import pynvml

SUPPORTED_MODELS = ["bge-m3", "bge-large-en-v1.5", "bge-large-zh-v1.5", "bce-embedding-base_v1"]


class EmbedAPI(ls.LitAPI):
    """
    Handler for BAAI embedding models: [bge-m3](https://huggingface.co/BAAI/bge-m3), [bge-large-en-v1.5](https://huggingface.co/BAAI/bge-large-en-v1.5), [bge-large-zh-v1.5](https://huggingface.co/BAAI/bge-large-zh-v1.5) and [bce-embedding-base_v1](https://huggingface.co/maidalun1020/bce-embedding-base_v1).

    Refers to:
    - https://github.com/FlagOpen/FlagEmbedding/issues/1060
    - https://github.com/FlagOpen/FlagEmbedding/issues/987

    BGE models give following error if the input is too long:
    Input prompt (612 tokens) is too long and exceeds limit of 512

    https://github.com/vllm-project/vllm/pull/4598 add truncate_prompt_tokens to work offline, but it has been closed for unknown reason.

    I have to truncate manually before predication.
    """

    MAX_TOKENS = {"bge-m3": 8000, "bge-large-en-v1.5": 500, "bge-large-zh-v1.5": 500}

    def __init__(self, model_name):
        super().__init__()
        self.max_tokens = self.MAX_TOKENS.get(model_name, 500)
        script_dir = os.path.dirname(os.path.abspath(__file__))
        self.model_dir = os.path.join(script_dir, "huggingface", model_name)
        assert os.path.exists(self.model_dir), f"Model {model_name} not found in {self.model_dir}"

    def setup(self, device):
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
        import vllm

        self.llm = vllm.LLM(self.model_dir)
        self.tokenizer = self.llm.get_tokenizer()

    def decode_request(self, request):
        sentences = request.get("sentences", [])
        assert isinstance(sentences, list), f"sentences must be a list, got {type(sentences)}. request: {request}"
        for i in range(len(sentences)):
            ids = self.tokenizer.encode(sentences[i])
            if len(ids) > self.max_tokens:
                print(f"before truncation({len(ids)} tokens): {sentences[i]}")
                sentences[i] = self.tokenizer.decode(ids[: self.max_tokens], skip_special_tokens=True)
                print(f"after truncation({self.max_tokens} tokens): {sentences[i]}")
        return sentences

    def predict(self, x):
        assert isinstance(x, list)
        batch_shape = [len(sentences) for sentences in x]
        batch_sentences = [sentence for sentences in x for sentence in sentences]
        batch_outputs = []
        for i in range(0, len(batch_sentences), 8):
            batch_outputs.extend(self.llm.encode(batch_sentences[i : i + 8]))
        embeddings = [output.outputs.data.numpy() for output in batch_outputs]
        batch_responses = []
        for i in range(len(batch_shape)):
            batch_responses.append(embeddings[: batch_shape[i]])
            embeddings = embeddings[batch_shape[i] :]
        return batch_responses

    def encode_response(self, output):
        # Convert the model output to a response payload.
        assert isinstance(output, list)
        resp = [base64.b64encode(embedding.tobytes()).decode("utf-8") for embedding in output]
        return {"embeddings": resp}


if __name__ == "__main__":
    args = sys.argv
    if len(args) != 2 or args[1] not in SUPPORTED_MODELS:
        print(f"Usage: python3 embed_server.py <model_name>\n<model_name> is one of: {SUPPORTED_MODELS}")
        sys.exit(-1)
    model_name = args[1]
    server = ls.LitServer(EmbedAPI(model_name), accelerator="gpu", max_batch_size=8)
    server.run(port=8000)
