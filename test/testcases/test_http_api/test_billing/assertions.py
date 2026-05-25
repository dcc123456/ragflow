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
"""
Shared pytest assertion helpers for billing test cases.

These helpers keep FlowError contained within the helper layer while test files
report failures through pytest-native assertions and pytest.fail().
"""

from __future__ import annotations

from collections.abc import Callable
from typing import TypeVar

import pytest

from libs.billing.billing_common import FlowError

T = TypeVar("T")


def expect_failure_with_message(
    action: Callable[[], T],
    *,
    expected_substrings: tuple[str, ...],
    success_message: str,
    unexpected_message: str,
) -> str:
    """Assert that an action fails and its message contains one expected token."""
    try:
        result = action()
    except (FlowError, AssertionError) as exc:
        error_message = str(exc)
    else:
        if isinstance(result, dict):
            if result.get("code") != 0:
                error_message = str(result.get("message") or result)
            else:
                pytest.fail(success_message)
        else:
            pytest.fail(success_message)

    lowered = error_message.lower()
    if not any(part in lowered for part in expected_substrings):
        pytest.fail(f"{unexpected_message}: {error_message}")
    return error_message


def fail_on_flow_error(message: str, action: Callable[[], T]) -> T:
    """Run an action and convert helper-layer FlowError into pytest.fail()."""
    try:
        return action()
    except FlowError as exc:
        pytest.fail(f"{message}: {exc}")
