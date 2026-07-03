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
https://paddlepaddle.github.io/PaddleOCR/latest/quick_start.html#python

$ python embed_client.py
"""

import base64
import json
import numpy as np
import requests

req0 = {"sentences": ["What is Deep Learning?", "Hello world!"]}
req1 = {
    "sentences": [
        """9.13.1   Licensed corporations and registered institutions are primarily responsible for planning and implementing a continuous education programme best suited to the training needs of the licensed  representatives or relevant individuals they engage. Such programmes  should enhance the individuals' industry knowledge, skills and professionalism. The firms should perform due diligence to ensure CPT compliance by the individuals they engage.
  9.13.2   Licensed individuals and relevant individuals of registered institutions are required to complete 10 CPT hours per calendar year, regardless of the number and types of regulated activities he or she engages in. Five of these 10 CPT hours must be on topics directly relevant to the regulated activities for which he or she is licensed at the time the CPT hours are undertaken.
  9.13.3   Individuals who engage in the sponsor work or Codes on Takeovers  transaction work for a firm are required to attend 2.5 CPT hours per  calendar year on topics that are relevant to their sponsor work or Codes  on Takeovers advisory work.
  9.13.4   In view of the higher level of responsibility and accountability placed on Responsible officers and Executive Officers, they are required to take two additional CPT hours per calendar year on regulatory compliance.
  9.13.5   Within the 12 months after a person first becomes a licensed individual or relevant individuals, he or she must undertake two CPT hours on ethics. Thereafter, that person is required to complete two CPT hours  per calendar year on topics relating to either ethics or compliance.
  9.13.6   Details of CPT requirements for corporations and individuals are set out in paragraphs 4 and 5 of the “Guidelines on Continuous Professional Training"""
    ]
}
reqs = [req0, req1]
for i in range(len(reqs)):
    resp = requests.post("http://localhost:8000/predict", json=reqs[i])
    resp = json.loads(resp.content.decode("utf-8"))
    embeddings_b64 = resp.get("embeddings", [])
    if not isinstance(embeddings_b64, list):
        print(f"got invalid response: {resp}")
        continue
    embeddings = []
    for embedding_b64 in embeddings_b64:
        embedding = np.frombuffer(base64.b64decode(embedding_b64.encode("utf-8")), dtype=np.float32)
        embeddings.append(embedding)
    print(f"response {i}:", embeddings)
