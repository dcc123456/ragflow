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
"""

import base64
import json
import numpy as np
import requests
from enum import Enum

class EmbedType(Enum):
    QUERY="query"
    PASSAGE="passage"
    IMAGE="image"

image_files = ['beach1.jpg', 'beach2.jpg']
def get_image(image_fp: str) -> str:
    img_b64 = base64.b64encode(open(image_fp, 'rb').read()).decode('utf-8')
    assert img_b64 is not None
    # print(img_b64)
    return img_b64

req0 = {"texts": ['A beautiful sunset over the beach', ]}
req1 = {"texts": ['A beautiful sunset over the beach', '海滩上美丽的日落', ]}
req2 = {"images": [get_image('beach1.jpg'), get_image('beach2.jpg'), ]}
req3 = {"prompt_name": "query", "texts": ['Show my a picture on sunset', ]}
reqs = [req0, req1, req2, req3]
for i in range(len(reqs)):
    resp = requests.post("http://localhost:8000/predict", json=reqs[i])
    resp = json.loads(resp.content.decode("utf-8"))
    if not isinstance(resp, list):
        print(f"got invalid response: {resp}")
        continue
    embeddings = []
    for payload in resp:
        embedding_bytes = base64.b64decode(payload["data"].encode("utf-8"))
        embedding =  np.frombuffer(embedding_bytes, dtype=payload["dtype"]).reshape(payload["shape"])
        embeddings.append(embedding)
    print(f"response {i}:", embeddings)
