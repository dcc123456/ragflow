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
"""Pydantic schemas for billing API request/response validation."""

from datetime import datetime
from typing import Literal, Optional

from pydantic import BaseModel, Field, field_validator, model_validator

from api.db import PriceType
from api.utils.billing import BYTES_PER_GB


# ---------------------------------------------------------------------------
# Request schemas
# ---------------------------------------------------------------------------


class StorageSetTargetRequest(BaseModel):
    """Request schema for PATCH /billing/storage."""

    tenant_id: Optional[str] = None
    target_storage_bytes: int
    setup_intent_id: Optional[str] = ""
    session_success_url: str
    session_cancel_url: str

    @field_validator("target_storage_bytes")
    @classmethod
    def target_storage_bytes_must_be_non_negative(cls, v: int) -> int:
        if v < 0:
            raise ValueError("target_storage_bytes must be >= 0")
        return v

    @field_validator("target_storage_bytes")
    @classmethod
    def target_storage_bytes_must_be_multiple_of_gb(cls, v: int) -> int:
        if v % BYTES_PER_GB != 0:
            raise ValueError("target_storage_bytes must be a multiple of 1GB")
        return v


class SubscriptionPreviewRequest(BaseModel):
    """Request schema for POST /billing/subscription/preview."""

    tenant_id: Optional[str] = None
    customer_id: Optional[str] = None
    new_price_id: Optional[str] = None
    target_storage_bytes: Optional[int] = None

    @model_validator(mode="after")
    def must_have_price_id_or_storage_bytes(self) -> "SubscriptionPreviewRequest":
        if not self.new_price_id and self.target_storage_bytes is None:
            raise ValueError("At least one of new_price_id or target_storage_bytes must be provided")
        return self

    @field_validator("target_storage_bytes")
    @classmethod
    def target_storage_bytes_must_be_non_negative(cls, v: Optional[int]) -> Optional[int]:
        if v is not None and v < 0:
            raise ValueError("target_storage_bytes must be >= 0")
        return v

    @field_validator("target_storage_bytes")
    @classmethod
    def target_storage_bytes_must_be_multiple_of_gb(cls, v: Optional[int]) -> Optional[int]:
        if v is not None and v % BYTES_PER_GB != 0:
            raise ValueError("target_storage_bytes must be a multiple of 1GB")
        return v


class CheckoutRequest(BaseModel):
    """Request schema for POST/PATCH /billing/subscription and POST /billing/addon-purchases."""

    tenant_id: Optional[str] = None
    payment_type: PriceType
    subscription_price_id: Optional[str] = None
    addon_price_id: Optional[str] = None
    quantity: int = Field(default=1, ge=0)
    expiry_time: Optional[str] = None
    setup_intent_id: Optional[str] = ""
    session_success_url: str
    session_cancel_url: str

    @model_validator(mode="after")
    def validate_subscription_price_id_for_subscription(self) -> "CheckoutRequest":
        if self.payment_type == PriceType.SUBSCRIPTION and not self.subscription_price_id:
            raise ValueError("subscription_price_id is required when payment_type is SUBSCRIPTION")
        return self

    @model_validator(mode="after")
    def validate_addon_price_id_for_addon(self) -> "CheckoutRequest":
        if self.payment_type == PriceType.ADDON and not self.addon_price_id:
            raise ValueError("addon_price_id is required when payment_type is ADDON")
        return self

    @model_validator(mode="after")
    def validate_quantity_for_subscription(self) -> "CheckoutRequest":
        if self.payment_type == PriceType.SUBSCRIPTION and self.quantity <= 0:
            raise ValueError("Quantity must be a positive integer for SUBSCRIPTION.")
        return self


class SetupIntentRequest(BaseModel):
    """Request schema for POST /billing/setup-intents."""

    tenant_id: Optional[str] = None
    setup_type: Literal["subscription_upgrade", "storage_addon"]
    price_id: Optional[str] = None
    target_storage_bytes: Optional[int] = None

    @field_validator("target_storage_bytes")
    @classmethod
    def target_storage_bytes_must_be_non_negative(cls, v: Optional[int]) -> Optional[int]:
        if v is not None and v < 0:
            raise ValueError("target_storage_bytes must be >= 0")
        return v


class PortalSessionRequest(BaseModel):
    """Request schema for POST /billing/portal-sessions."""

    tenant_id: Optional[str] = None
    return_url: str


class PointsCheckoutRequest(BaseModel):
    """Request schema for POST /billing/points/checkout."""

    tenant_id: Optional[str] = None
    quantity: int = Field(gt=0)
    session_success_url: str
    session_cancel_url: str


# ---------------------------------------------------------------------------
# Response schemas
# ---------------------------------------------------------------------------


class BillingCycleResponse(BaseModel):
    """Response schema for billing cycle dates."""

    start: Optional[str] = None
    end: Optional[str] = None


class SubscriptionOverviewResponse(BaseModel):
    """Response schema for /billing/subscription/overview."""

    customer_id: str = ""
    subscription_id: str = ""
    price_id: str = ""
    plan_name: str = "unknown"
    subscription_status: str = ""
    billing_cycle: BillingCycleResponse = Field(default_factory=BillingCycleResponse)
    payment_required: bool = False
    payment_recoverable: bool = False
    payment_recovery_url: str = ""


class StorageCurrentResponse(BaseModel):
    """Response schema for GET /billing/storage."""

    tenant_id: str = ""
    plan_name: str = ""
    trial_forbidden: bool = False
    unit_price: float = 0.0
    addon_storage_bytes: int = 0
    target_storage_bytes: int = 0
    subscription_id: str = ""
    price_id: str = ""
    status: str = ""
    cancel_at_period_end: bool = False
    current_period_start: Optional[datetime] = None
    current_period_end: Optional[datetime] = None
    payment_required: bool = False
    payment_recovery_url: str = ""


class PointsBalanceResponse(BaseModel):
    """Response schema for /billing/points/balance and /billing/points/overview."""

    plan_points: dict = Field(default_factory=dict)
    addon_points: dict = Field(default_factory=dict)


class CheckoutResponse(BaseModel):
    """Response schema for checkout endpoints."""

    redirect_to: Optional[str] = None
    requires_payment_method_setup: bool = False


class SetupIntentResponse(BaseModel):
    """Response schema for /billing/setup-intents."""

    client_secret: str = ""
    setup_intent_id: str = ""


class PortalSessionResponse(BaseModel):
    """Response schema for /billing/portal-sessions."""

    redirect_to: str = ""


class PointsCheckoutResponse(BaseModel):
    """Response schema for /billing/points/checkout."""

    checkout_url: str = ""


class SessionStatusResponse(BaseModel):
    """Response schema for GET /billing/checkouts/<session_id>."""

    payment_status: str = "unknown"
    mode: Optional[str] = None
    amount_cents: Optional[int] = None
    currency: Optional[str] = None
    created: Optional[int] = None
    metadata: dict = Field(default_factory=dict)
    payment_intent_id: Optional[str] = None
    receipt_url: Optional[str] = None
    invoice_id: Optional[str] = None
    invoice_url: Optional[str] = None
    invoice_pdf_url: Optional[str] = None
