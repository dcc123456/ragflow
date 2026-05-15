#
#  Copyright 2026 The InfiniFlow Authors. All Rights Reserved.
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
import enum
import sys
import types
from datetime import datetime
from types import SimpleNamespace

import pytest

if "strenum" not in sys.modules:
    strenum_stub = types.ModuleType("strenum")

    class StrEnum(str, enum.Enum):
        pass

    strenum_stub.StrEnum = StrEnum
    sys.modules["strenum"] = strenum_stub

from api.db.services.document_service import DocumentService
from api.db.services.task_service import TaskService
from common.constants import TaskStatus


def _unwrapped_sync_progress():
    return DocumentService._sync_progress.__func__.__wrapped__


class _FakeUpdateQuery:
    def __init__(self, calls):
        self._calls = calls

    def where(self, *_args, **_kwargs):
        return self

    def execute(self):
        self._calls.append(True)
        return 1


class _FakeModel:
    update_calls: list[bool] = []

    @classmethod
    def update(cls, *_args, **_kwargs):
        return _FakeUpdateQuery(cls.update_calls)


@pytest.mark.p2
def test_clone_task_does_not_rewrite_source_document_status(monkeypatch):
    monkeypatch.setattr(DocumentService, "model", _FakeModel)
    monkeypatch.setattr(
        DocumentService,
        "get_by_id",
        lambda _doc_id: (True, SimpleNamespace(id="doc-1", progress=0, run=TaskStatus.UNSTART.value)),
    )
    monkeypatch.setattr(
        TaskService,
        "query",
        lambda **_kwargs: [SimpleNamespace(task_type="clone", progress=1, progress_msg="done", priority=0)],
    )

    sync_progress = _unwrapped_sync_progress()
    sync_progress(DocumentService, [{"id": "doc-1", "process_begin_at": datetime.now()}])

    assert _FakeModel.update_calls == []
