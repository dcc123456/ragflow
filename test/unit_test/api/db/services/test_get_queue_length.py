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
import logging
import sys
import types
import warnings

import pytest

# xgboost imports pkg_resources and emits a deprecation warning that is promoted
# to error in our pytest configuration; ignore it for this unit test module.
warnings.filterwarnings(
    "ignore",
    message="pkg_resources is deprecated as an API.*",
    category=UserWarning,
)


def _install_cv2_stub_if_unavailable():
    try:
        import cv2  # noqa: F401

        return
    except (ImportError, OSError) as exc:
        logging.debug("cv2 unavailable; installing test stub: %s", exc)

    stub = types.ModuleType("cv2")

    def _missing(*_args, **_kwargs):
        raise RuntimeError("cv2 runtime call is unavailable in this test environment")

    def _module_getattr(name):
        if name.isupper():
            return 0
        return _missing

    stub.__getattr__ = _module_getattr
    sys.modules["cv2"] = stub


_install_cv2_stub_if_unavailable()

from api.db.services import document_service as ds  # noqa: E402


@pytest.mark.p2
class TestGetQueueLength:
    def test_returns_rabbitmq_message_count(self, monkeypatch):
        monkeypatch.setattr(ds.RABBITMQ_CONN, "get_queue_length", lambda q, vhost="/": 15)
        assert ds.get_queue_length(0, "common") == 15

    def test_passes_correct_routing_key(self, monkeypatch):
        calls = []

        def _capture(q, vhost="/"):
            calls.append(q)
            return 0

        monkeypatch.setattr(ds.RABBITMQ_CONN, "get_queue_length", _capture)
        ds.get_queue_length(3, "embedding")
        assert len(calls) == 1
        assert "3" in calls[0] and "embedding" in calls[0]

    def test_zero_when_queue_empty(self, monkeypatch):
        monkeypatch.setattr(ds.RABBITMQ_CONN, "get_queue_length", lambda q, vhost="/": 0)
        assert ds.get_queue_length(0, "common") == 0

    def test_different_priority_queues_are_separate(self, monkeypatch):
        calls = []

        def _capture(q, vhost="/"):
            calls.append(q)
            return 0

        monkeypatch.setattr(ds.RABBITMQ_CONN, "get_queue_length", _capture)
        ds.get_queue_length(1, "common")
        ds.get_queue_length(2, "common")
        assert calls[0] != calls[1]

    def test_different_suffix_queues_are_separate(self, monkeypatch):
        calls = []

        def _capture(q, vhost="/"):
            calls.append(q)
            return 0

        monkeypatch.setattr(ds.RABBITMQ_CONN, "get_queue_length", _capture)
        ds.get_queue_length(0, "common")
        ds.get_queue_length(0, "embedding")
        assert calls[0] != calls[1]
