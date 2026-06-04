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
import pytest
import requests
from common import related_questions
from configs import HOST_ADDRESS, INVALID_API_TOKEN, VERSION
from libs.auth import RAGFlowHttpApiAuth


def _get_beta_auth(auth):
    tokens_res = requests.get(
        f"{HOST_ADDRESS}/api/{VERSION}/system/tokens",
        headers={"Authorization": auth},
        timeout=30,
    )
    assert tokens_res.status_code == 200, tokens_res.text
    tokens_payload = tokens_res.json()
    assert tokens_payload["code"] == 0, tokens_payload
    return RAGFlowHttpApiAuth(tokens_payload["data"][0]["beta"])


class TestRelatedQuestions:
    @pytest.mark.p3
    def test_related_questions_success(self, auth):
        res = related_questions(_get_beta_auth(auth), {"question": "ragflow", "industry": "search"})
        assert res["code"] == 0, res
        assert isinstance(res.get("data"), list), res

    @pytest.mark.p2
    def test_related_questions_missing_question(self, auth):
        res = related_questions(_get_beta_auth(auth), {"industry": "search"})
        assert res["code"] == 101, res
        assert "question" in res.get("message", ""), res

    @pytest.mark.p2
    def test_related_questions_invalid_auth(self):
        res = related_questions(RAGFlowHttpApiAuth(INVALID_API_TOKEN), {"question": "ragflow", "industry": "search"})
        assert res["code"] == 102, res
        assert "API key is invalid" in res.get("message", ""), res
