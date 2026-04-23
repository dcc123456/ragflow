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
import warnings

import pytest

warnings.filterwarnings(
    "ignore",
    message="pkg_resources is deprecated as an API.*",
    category=UserWarning,
)
warnings.filterwarnings(
    "ignore",
    message="\\[Errno 13\\] Permission denied\\.  joblib will operate in serial mode",
    category=UserWarning,
)

from api.db.services import billing_service  # noqa: E402


@pytest.mark.p2
def test_entitled_main_subscription_statuses_are_allowlist():
    assert billing_service.ENTITLED_MAIN_SUBSCRIPTION_STATUSES == {"active", "trialing"}


@pytest.mark.p2
@pytest.mark.parametrize(
    "status",
    [
        "incomplete",
        "incomplete_expired",
        "past_due",
        "unpaid",
        "canceled",
        "paused",
        "unknown",
        "",
    ],
)
def test_non_entitled_statuses_are_not_in_allowlist(status):
    assert status not in billing_service.ENTITLED_MAIN_SUBSCRIPTION_STATUSES
