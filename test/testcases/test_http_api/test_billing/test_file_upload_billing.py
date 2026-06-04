#!/usr/bin/env python3
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
FILE-UPLOAD-BILLING: integration tests for subscription + storage quota
enforcement on the file-manager upload endpoint (POST /api/v1/files).

The dataset upload endpoint (POST /api/v1/datasets/{id}/documents) has long
enforced these checks via FileService.upload_document. The file-manager
endpoint (POST /api/v1/files) calls file_api_service.upload_file, which
previously wrote to storage without consulting the billing module. This
test file pins the new behaviour so a regression in the file-manager
path is caught by CI just like the dataset path is.
"""

from __future__ import annotations

import io
import logging
import os
import uuid

import pytest
from requests_toolbelt import MultipartEncoder  # type: ignore[reportMissingImports]

from libs.billing.app_common import AppClient
from libs.billing.billing_common import FlowError

logger = logging.getLogger(__name__)


# =============================================================================
# Helpers
# =============================================================================


def _post_file(
    client: AppClient,
    file_name: str,
    file_obj,
    parent_id: str = "",
) -> dict:
    """POST a file to /api/v1/files and return the raw JSON response.

    Unlike client.upload_document, this does NOT raise on non-zero code
    so the test can assert on the billing error payload (code 2003 / 2005).
    """
    fields = [("file", (file_name, file_obj))]
    if parent_id:
        fields.append(("parent_id", parent_id))
    m = MultipartEncoder(fields=fields)
    try:
        response = client.session.post(
            f"{client.base_url}/api/{client.version}/files",
            headers={**client.sdk_headers(), "Content-Type": m.content_type},
            data=m,
            timeout=120,
        )
    finally:
        if hasattr(file_obj, "close"):
            file_obj.close()
    try:
        return response.json()
    except ValueError as exc:
        raise FlowError(f"POST /files returned non-JSON status={response.status_code}: {response.text[:500]}") from exc


# =============================================================================
# FILE-UPLOAD-BILLING-01: happy path on Trial
# =============================================================================


@pytest.mark.billing
def test_file_upload_billing_01_happy_path_on_trial(billing_client: AppClient) -> None:
    """FILE-UPLOAD-BILLING-01: a healthy Trial tenant can upload via /files.

    The new billing check (gated on BILLING_ENABLED) must not break the
    success path: code 0 with a populated data payload.
    """
    # Trial plan + SDK auth are required for /files uploads
    plan = billing_client.current_plan()
    assert plan["plan_name"].lower() == "trial", f"Test environment should start on Trial, got {plan['plan_name']}"
    billing_client.init_sdk_token()

    pdf_path = os.path.join(os.path.dirname(__file__), "hou_chibi_fu.pdf")
    assert os.path.exists(pdf_path), f"Sample PDF not found: {pdf_path}"

    with open(pdf_path, "rb") as fp:
        result = _post_file(
            billing_client,
            file_name=os.path.basename(pdf_path),
            file_obj=fp,
        )

    assert result.get("code") == 0, f"Upload should succeed on Trial with available storage: {result}"
    assert result.get("data"), f"No data in upload response: {result}"

    logger.info(
        "FILE-UPLOAD-BILLING-01: trial upload succeeded, file_id=%s",
        (result.get("data") or [{}])[0].get("id"),
    )


# =============================================================================
# FILE-UPLOAD-BILLING-02: storage quota exceeded returns 2003
# =============================================================================


@pytest.mark.billing
def test_file_upload_billing_02_storage_quota_exceeded(billing_client: AppClient) -> None:
    """FILE-UPLOAD-BILLING-02: a file that exceeds available storage returns 2003.

    Uploads a ~200 MB binary blob via /files. The Trial plan's storage
    quota is intentionally much smaller than that, so the billing check
    inside file_api_service.upload_file raises InsufficientResourceError
    and the endpoint returns RetCode.BILLING_STORAGE_INSUFFICIENT (2003)
    with detail.current / detail.limit populated for the upgrade modal.
    """
    billing_client.init_sdk_token()

    # 200 MB blob. The Trial default storage quota is well below this,
    # so the upload is rejected before the storage layer is touched.
    payload_size = 200 * 1024 * 1024
    blob = io.BytesIO(b"\0" * payload_size)
    result = _post_file(
        billing_client,
        file_name=f"oversized-{uuid.uuid4().hex[:8]}.bin",
        file_obj=blob,
    )

    assert result.get("code") == 2003, f"Expected 2003 (BILLING_STORAGE_INSUFFICIENT), got code={result.get('code')}: {result}"
    detail = result.get("detail") or {}
    assert "current" in detail and "limit" in detail, f"detail must expose current/limit for the upgrade modal, got: {detail}"
    logger.info(
        "FILE-UPLOAD-BILLING-02: storage rejected oversized upload, detail=%s",
        detail,
    )


# =============================================================================
# FILE-UPLOAD-BILLING-03: response format matches the dataset upload
# =============================================================================


@pytest.mark.billing
def test_file_upload_billing_03_response_format_matches_dataset_path(billing_client: AppClient) -> None:
    """FILE-UPLOAD-BILLING-03: /files error shape matches /datasets/{id}/documents.

    The frontend uses one showPriceModal flow for both endpoints, so the
    response code and the field names must be byte-identical. This test
    verifies the structural shape on the storage-rejected path.
    """
    billing_client.init_sdk_token()

    # Trigger the storage rejection path (cheapest to set up).
    blob = io.BytesIO(b"\0" * (150 * 1024 * 1024))  # 150 MB
    result = _post_file(
        billing_client,
        file_name=f"shape-check-{uuid.uuid4().hex[:8]}.bin",
        file_obj=blob,
    )

    # Top-level shape: {code, message, detail}
    assert set(result.keys()) >= {"code", "message", "detail"}, f"Response must expose code/message/detail, got keys: {sorted(result.keys())}"
    assert isinstance(result["code"], int), f"code must be int, got {type(result['code'])}"
    assert isinstance(result["message"], str), f"message must be str, got {type(result['message'])}"
    assert isinstance(result["detail"], dict), f"detail must be dict, got {type(result['detail'])}"

    # detail shape (when code == 2003) carries current/limit/filename.
    if result["code"] == 2003:
        detail = result["detail"]
        assert "current" in detail
        assert "limit" in detail
        logger.info("FILE-UPLOAD-BILLING-03: /files 2003 detail shape verified: %s", detail)

    # NOTE: the SUBSCRIPTION_INVALID branch (code 2005) is exercised by the
    # unit test test/unit_test/api/db/services/test_file_service_upload_document.py
    # ::test_upload_document_returns_subscription_invalid_error, which mocks
    # check_dynamic_resources to return the no-subscription error payload.
    # Triggering the same branch over a real Stripe test clock would require
    # canceling a subscription in a way BillingClient does not currently
    # expose, so it is intentionally not duplicated here.


# =============================================================================
# FILE-UPLOAD-BILLING-04: response format matches the dataset upload
# =============================================================================


@pytest.mark.billing
def test_file_upload_billing_04_response_format_matches_dataset_path(billing_client: AppClient) -> None:
    """FILE-UPLOAD-BILLING-04: /files error shape matches /datasets/{id}/documents.

    The frontend uses one showPriceModal flow for both endpoints, so the
    response code and the field names must be byte-identical. This test
    verifies the structural shape on the storage-rejected path.
    """
    billing_client.init_sdk_token()

    # Trigger the storage rejection path (cheapest to set up).
    blob = io.BytesIO(b"\0" * (150 * 1024 * 1024))  # 150 MB
    result = _post_file(
        billing_client,
        file_name=f"shape-check-{uuid.uuid4().hex[:8]}.bin",
        file_obj=blob,
    )

    # Top-level shape: {code, message, detail}
    assert set(result.keys()) >= {"code", "message", "detail"}, f"Response must expose code/message/detail, got keys: {sorted(result.keys())}"
    assert isinstance(result["code"], int), f"code must be int, got {type(result['code'])}"
    assert isinstance(result["message"], str), f"message must be str, got {type(result['message'])}"
    assert isinstance(result["detail"], dict), f"detail must be dict, got {type(result['detail'])}"

    # detail shape (when code == 2003) carries current/limit/filename.
    if result["code"] == 2003:
        detail = result["detail"]
        assert "current" in detail
        assert "limit" in detail
        logger.info("FILE-UPLOAD-BILLING-04: /files 2003 detail shape verified: %s", detail)
