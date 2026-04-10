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

"""Unit tests for OCRClient response normalization."""

import importlib.util
import os
import sys
from unittest import mock

import numpy as np


def _find_project_root(marker="pyproject.toml"):
    cur = os.path.dirname(os.path.abspath(__file__))
    while True:
        if os.path.exists(os.path.join(cur, marker)):
            return cur
        parent = os.path.dirname(cur)
        if parent == cur:
            raise FileNotFoundError(f"Could not locate project root (missing {marker})")
        cur = parent


def _timeout_decorator(_seconds):
    def decorator(func):
        return func

    return decorator


if "api" not in sys.modules:
    sys.modules["api"] = mock.MagicMock()
if "api.utils" not in sys.modules:
    sys.modules["api.utils"] = mock.MagicMock()
api_utils = mock.MagicMock()
api_utils.timeout = _timeout_decorator
sys.modules["api.utils.api_utils"] = api_utils

_MODULE_PATH = os.path.join(_find_project_root(), "deepdoc", "vision", "ocr_cli.py")
_SPEC = importlib.util.spec_from_file_location("ocr_cli", _MODULE_PATH)
_MOD = importlib.util.module_from_spec(_SPEC)
_SPEC.loader.exec_module(_MOD)

OCRClient = _MOD.OCRClient


def _mock_response(payload):
    response = mock.Mock()
    response.json.return_value = payload
    return response


class TestOCRClientDetect:
    def test_detect_preserves_standard_bbox_shape(self):
        client = OCRClient("http://deepdoc")
        client.session = mock.Mock()
        client.session.post.return_value = _mock_response(
            {
                "output": [
                    [
                        [[1.0, 2.0], [11.0, 2.0], [11.0, 8.0], [1.0, 8.0]],
                    ]
                ]
            }
        )

        results = list(client.detect(np.zeros((8, 8, 3), dtype=np.uint8)))

        assert results == [
            ([1.0, 2.0], ("", 0)),
            ([11.0, 2.0], ("", 0)),
            ([11.0, 8.0], ("", 0)),
            ([1.0, 8.0], ("", 0)),
        ]

    def test_detect_unwraps_singleton_wrapped_bbox(self):
        client = OCRClient("http://deepdoc")
        client.session = mock.Mock()
        client.session.post.return_value = _mock_response(
            {
                "output": [
                    [
                        [[[12.0, 18.0], [42.0, 18.0], [42.0, 32.0], [12.0, 32.0]]],
                    ]
                ]
            }
        )

        results = list(client.detect(np.zeros((8, 8, 3), dtype=np.uint8)))

        assert results == [
            (
                [[12.0, 18.0], [42.0, 18.0], [42.0, 32.0], [12.0, 32.0]],
                ("", 0),
            )
        ]
