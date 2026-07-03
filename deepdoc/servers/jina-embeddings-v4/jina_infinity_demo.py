#!/usr/bin/env python3

# PEP 723 metadata
# /// script
# requires-python = ">=3.10,<3.11"
# dependencies = [
#   "requests",
#   "numpy",
#   "pdfplumber",
#   "infinity",
# ]
# ///

"""
$ python jina_infinity_demo.py
"""

import base64
import json
import glob
import os.path
import io

import numpy as np
import requests
import pdfplumber
import infinity

queries = open("queries.txt").readlines()
queries = [query.strip() for query in queries]
queries = [query for query in queries if query]


def parse_pdfs() -> list:
    pdf_images = []
    for pdf_fp in glob.glob("pdfs/*.pdf"):
        fn = os.path.basename(pdf_fp)
        with pdfplumber.open(pdf_fp) as pdf:
            for num, page in enumerate(pdf.pages):
                img = page.to_image().annotated
                # img = page.to_image(resolution=72 * 3).annotated
                byte_io = io.BytesIO()
                img.save(byte_io, format="PNG")
                img_bytes = byte_io.getvalue()
                img_b64 = base64.b64encode(img_bytes).decode("utf-8")
                pdf_images.append((fn, num + 1, img_b64))
    return pdf_images


def jina_embedding_texts(texts: list) -> list:
    return jina_embedding_payload({"texts": texts})


def jina_embedding_images(images: list) -> list:
    return jina_embedding_payload({"images": images})


def jina_embedding_payload(req_payload: dict) -> list:
    resp = requests.post("http://localhost:8000/predict", json=req_payload)
    resp = json.loads(resp.content.decode("utf-8"))
    if not isinstance(resp, list):
        print(f"got invalid response: {resp}")
        return None
    embeddings = []
    for payload in resp:
        embedding_bytes = base64.b64decode(payload["data"].encode("utf-8"))
        embedding = np.frombuffer(embedding_bytes, dtype=payload["dtype"]).reshape(payload["shape"])
        embeddings.append(embedding)
    return embeddings


def infinity_index():
    infinity_instance = infinity.connect(infinity.common.NetworkAddress("127.0.0.1", 23817))
    db_instance = infinity_instance.get_database("default_db")
    db_instance.drop_table("jina_infinity_demo", infinity.common.ConflictType.Ignore)
    table_instance = db_instance.create_table(
        "jina_infinity_demo",
        {
            "file_name": {"type": "varchar"},
            "page_num": {"type": "int"},
            "embedding": {"type": "multivector,128,float"},
        },
        infinity.common.ConflictType.Error,
    )
    table_instance.create_index(
        "index1", infinity.index.IndexInfo("embedding", infinity.index.IndexType.Hnsw, {"m": "16", "ef_construction": "200", "metric": "ip"}), infinity.common.ConflictType.Error
    )

    pdf_images = parse_pdfs()
    batch_size = 5
    for i in range(0, len(pdf_images), batch_size):
        batch = pdf_images[i : i + batch_size]
        page_embeddings = jina_embedding_images([img_b64 for _, _, img_b64 in batch])
        values = []
        for j, (fn, num, _) in enumerate(batch):
            values.append(
                {
                    "file_name": fn,
                    "page_num": num,
                    "embedding": page_embeddings[j],
                }
            )
        table_instance.insert(values)


def infinity_search():
    infinity_instance = infinity.connect(infinity.common.NetworkAddress("127.0.0.1", 23817))
    db_instance = infinity_instance.get_database("default_db")
    table_instance = db_instance.get_table("jina_infinity_demo")
    query_embeddings = jina_embedding_texts(queries)
    for i, query_embedding in enumerate(query_embeddings):
        query_embedding_flatten = query_embedding.flatten().tolist()
        res, extra_result = table_instance.output(["file_name", "page_num", "SIMILARITY()"]).match_dense("embedding", query_embedding_flatten, "float", "ip", 5).to_pl()
        print(f"query: {queries[i]}")
        print(f"result: {res}")


# infinity_index()
infinity_search()
