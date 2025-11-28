#!/usr/bin/env python3

# PEP 723 metadata
# /// script
# requires-python = ">=3.10,<3.11"
# dependencies = [
#   "requests",
#   "numpy",
# ]
# ///

"""
https://huggingface.co/jinaai/jina-embeddings-v4-vllm-retrieval

$ python embed_client.py


https://jina.ai/embeddings/

- Request:
curl https://api.jina.ai/v1/embeddings \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer jina_a85e4ebd3aa14479bc17fd24a7bf1c5fNVYiSt65jTusehrWs9i8jedGpB6M" \
  -d @- <<EOFEOF
  {
    "model": "jina-embeddings-v4",
    "task": "retrieval.passage",
    "truncate": true,
    "return_multivector": true,
    "input": [
        {
            "text": "A beautiful sunset over the beach"
        },
        {
            "text": "Un beau coucher de soleil sur la plage"
        },
        {
            "text": "海滩上美丽的日落"
        },
        {
            "text": "浜辺に沈む美しい夕日"
        },
        {
            "image": "https://i.ibb.co/nQNGqL0/beach1.jpg"
        },
        {
            "image": "https://i.ibb.co/r5w8hG8/beach2.jpg"
        },
        {
            "image": "iVBORw0KGgoAAAANSUhEUgAAABwAAAA4CAIAAABhUg/jAAAAMklEQVR4nO3MQREAMAgAoLkoFreTiSzhy4MARGe9bX99lEqlUqlUKpVKpVKpVCqVHksHaBwCA2cPf0cAAAAASUVORK5CYII="
        }
    ]
  }
EOFEOF

- Response:
{
  "model": "jina-embeddings-v4",
  "object": "list",
  "usage": {
    "total_tokens": 5965
  },
  "data": [
    {
      "object": "embeddings",
      "index": 0,
      "embeddings": [
        [
          -0.07617188,
          0.12695312,
          0.12402344,
          -0.0234375,
          0.04614258,
          ...
        ],
        ...
      ]
    },
    {
      "object": "embeddings",
      "index": 1,
      "embeddings": [
        [
          -0.07617188,
          0.12695312,
          0.12402344,
          -0.0234375,
          0.04614258,
          ...
        ],
        ...
      ]
    },
    {
      "object": "embeddings",
      "index": 2,
      "embeddings": [
        [
          -0.07617188,
          0.12158203,
          0.12402344,
          -0.02087402,
          0.04492188,
          ...
        ],
        ...
      ]
    },
    {
      "object": "embeddings",
      "index": 3,
      "embeddings": [
        [
          -0.07617188,
          0.12158203,
          0.12402344,
          -0.02087402,
          0.04492188,
          ...
        ],
        ...
      ]
    },
    {
      "object": "embeddings",
      "index": 4,
      "embeddings": [
        [
          -0.08935547,
          0.14550781,
          0.09716797,
          -0.01635742,
          0.06982422,
          ...
        ],
        ...
      ]
    },
    {
      "object": "embeddings",
      "index": 5,
      "embeddings": [
        [
          -0.09082031,
          0.14355469,
          0.09375,
          -0.01757812,
          0.06884766,
          ...
        ],
        ...
      ]
    },
    {
      "object": "embeddings",
      "index": 6,
      "embeddings": [
        [
          -0.10351562,
          0.14453125,
          0.09082031,
          -0.0043335,
          0.06933594,
          ...
        ],
        ...
      ]
    }
  ]
}

"""

import base64
import numpy as np
import requests
import os
from enum import Enum


class EmbedType(Enum):
    QUERY = "query"
    PASSAGE = "passage"
    IMAGE = "image"


JINA_API_KEY = os.environ.get("JINA_API_KEY")

image_files = ["beach1.jpg", "beach2.jpg"]


def get_image(image_fp: str) -> str:
    img_b64 = base64.b64encode(open(image_fp, "rb").read()).decode("utf-8")
    assert img_b64 is not None
    return img_b64


def parse_response(resp_data):
    embeddings = []
    if "data" in resp_data:
        for item in resp_data["data"]:
            # Jina API v4 uses 'embeddings'
            # Local server uses 'embeddings'
            emb_data = item.get("embeddings")
            if emb_data is None:
                continue

            # Handle format difference
            if isinstance(emb_data, str):
                # Base64 encoded (Local Server)
                if "shape" in item and "dtype" in item:
                    embedding_bytes = base64.b64decode(emb_data.encode("utf-8"))
                    embedding = np.frombuffer(embedding_bytes, dtype=item["dtype"]).reshape(item["shape"])
                    embeddings.append(embedding)
            else:
                # List of floats/lists (Jina API)
                embeddings.append(np.array(emb_data))
    return embeddings


def call_local_server(payload):
    try:
        resp = requests.post("http://localhost:8000/embeddings", json=payload)
        resp.raise_for_status()
        return parse_response(resp.json())
    except Exception as e:
        print(f"Local server error: {e}")
        return []


def call_jina_api(payload):
    if not JINA_API_KEY:
        print("Skipping Jina API call: JINA_API_KEY not set")
        return []

    url = "https://api.jina.ai/v1/embeddings"
    headers = {"Content-Type": "application/json", "Authorization": f"Bearer {JINA_API_KEY}"}

    # Adjust payload for Jina API
    jina_payload = payload.copy()
    jina_payload["model"] = "jina-embeddings-v4"
    jina_payload["truncate"] = True
    jina_payload["return_multivector"] = True

    try:
        resp = requests.post(url, headers=headers, json=jina_payload)
        resp.raise_for_status()
        return parse_response(resp.json())
    except Exception as e:
        print(f"Jina API error: {e}")
        if "resp" in locals():
            print(f"Response content: {resp.text}")
        return []


def cosine_similarity(a, b):
    return np.dot(a, b) / (np.linalg.norm(a) * np.linalg.norm(b))


# Request format matching JinaMultiVecEmbed API
req0 = {"task": "retrieval.passage", "input": [{"text": "A beautiful sunset over the beach"}]}
req1 = {"task": "retrieval.passage", "input": [{"text": "A beautiful sunset over the beach"}, {"text": "海滩上美丽的日落"}]}
req2 = {"task": "retrieval.passage", "input": [{"image": get_image("beach1.jpg")}, {"image": get_image("beach2.jpg")}]}
req3 = {"task": "retrieval.query", "input": [{"text": "Show my a picture on sunset"}]}

reqs = [req0, req1, req2, req3]
for i in range(len(reqs)):
    print(f"\n--- Request {i} ---")
    # print(f"Request {i}: {reqs[i]}")

    # Call Local Server
    print("Calling Local Server...")
    local_embeddings = call_local_server(reqs[i])
    print(f"Local: Got {len(local_embeddings)} embeddings")
    for j, emb in enumerate(local_embeddings):
        print(f"  Local Embedding {j} shape: {emb.shape}, first 5: {emb.flatten()[:5]}")

    # Call Jina API
    print("Calling Jina API...")
    jina_embeddings = call_jina_api(reqs[i])
    if jina_embeddings:
        print(f"Jina: Got {len(jina_embeddings)} embeddings")
        for j, emb in enumerate(jina_embeddings):
            print(f"  Jina Embedding {j} shape: {emb.shape}, first 5: {emb.flatten()[:5]}")

        # Compare if counts match
        if len(local_embeddings) == len(jina_embeddings):
            for j in range(len(local_embeddings)):
                # Only compare if shapes match (v3 vs v4 might differ in dimension)
                if local_embeddings[j].shape == jina_embeddings[j].shape:
                    sim = cosine_similarity(local_embeddings[j].flatten(), jina_embeddings[j].flatten())
                    print(f"  Similarity (Local vs Jina) for item {j}: {sim:.4f}")
                else:
                    print(f"  Skipping similarity check for item {j}: shapes differ {local_embeddings[j].shape} vs {jina_embeddings[j].shape}")
    else:
        print("Jina API result empty or skipped.")
