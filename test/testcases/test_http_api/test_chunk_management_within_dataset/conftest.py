#
#  Copyright 2025 The InfiniFlow Authors. All Rights Reserved.
#
#  Licensed under the Apache License, Version 2.0 (the "License");
#  you may not use this file except in compliance with the License.
#  You may obtain a copy of the License at
#
#      http://www.apache.org/licenses/LICENSE-2.0
#
#  Unless required by applicable law or agreed to in writing, software
#  distributed under the License is distributed on an "AS IS" BASIS,
#  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
#  See the License for the specific language governing permissions and
#  limitations under the License.
#


from time import sleep

import pytest
from common import add_chunk, batch_add_chunks, delete_all_chunks, list_documents
from utils import wait_for


@wait_for(200, 1, "Document upload visibility timeout")
def condition(_auth, _dataset_id, _document_id):
    res = list_documents(_auth, _dataset_id)
    if res.get("code") != 0:
        raise RuntimeError(f"list_documents failed: {res}")
    docs = res.get("data", {}).get("docs", [])
    return any(str(doc.get("id")) == str(_document_id) for doc in docs)


def _add_baseline_chunk(auth, dataset_id, document_id):
    res = add_chunk(auth, dataset_id, document_id, {"content": "ragflow test upload"})
    if res.get("code") != 0:
        raise RuntimeError(f"add_chunk failed: {res}")


@pytest.fixture(scope="class")
def add_chunks(HttpApiAuth, add_document):
    dataset_id, document_id = add_document
    condition(HttpApiAuth, dataset_id, document_id)
    _add_baseline_chunk(HttpApiAuth, dataset_id, document_id)
    chunk_ids = batch_add_chunks(HttpApiAuth, dataset_id, document_id, 4)
    sleep(1)  # issues/6487
    return dataset_id, document_id, chunk_ids


@pytest.fixture(scope="function")
def add_chunks_func(request, HttpApiAuth, add_document):
    def cleanup():
        delete_all_chunks(HttpApiAuth, dataset_id, document_id)

    request.addfinalizer(cleanup)

    dataset_id, document_id = add_document
    condition(HttpApiAuth, dataset_id, document_id)
    _add_baseline_chunk(HttpApiAuth, dataset_id, document_id)
    chunk_ids = batch_add_chunks(HttpApiAuth, dataset_id, document_id, 4)
    # issues/6487
    sleep(1)
    return dataset_id, document_id, chunk_ids
