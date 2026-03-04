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
from datetime import datetime
from typing import Literal, Optional

from pydantic import BaseModel, Field, field_validator

from api.db import PaymentCurrency
from common.billing_utils import to_utc_datetime


# ----------------------intent.succeeded
class IntentSucceed(BaseModel):
    id: str
    object: Literal["payment_intent"]
    amount: int
    currency: str
    customer_id: Optional[str] = Field(default=None, alias="customer")
    payment_method: Optional[str] = None
    created: datetime
    status: str

    latest_charge_id: Optional[str] = Field(default=None, alias="latest_charge")
    metadata: dict[str, str] = Field(default_factory=dict)

    amount_received: int
    receipt_email: Optional[str] = None

    @field_validator("created", mode="before")
    @classmethod
    def convert_timestamp(cls, v):
        return to_utc_datetime(v)


# --------------------- session.completed
class CheckoutSessionLineItemPrice(BaseModel):
    price_id: str
    product: str
    unit_amount: int
    currency: str


class CheckoutSessionLineItem(BaseModel):
    id: str
    price: CheckoutSessionLineItemPrice
    quantity: int
    description: Optional[str] = None
    amount_subtotal: int
    amount_total: int
    currency: str


class CheckoutSessionLineItems(BaseModel):
    data: list[CheckoutSessionLineItem]
    has_more: bool = False
    object: Literal["list"] = "list"


class CheckoutSessionCompleted(BaseModel):
    id: str
    customer_id: Optional[str] = Field(default=None, alias="customer")
    customer_email: Optional[str] = None
    payment_intent_id: Optional[str] = Field(alias="payment_intent")
    subscription_id: Optional[str] = Field(alias="subscription")
    payment_status: str
    status: Optional[str] = None
    mode: str  # "payment" or "subscription" or "setup"
    currency: Optional[PaymentCurrency] = None
    amount_subtotal: Optional[int] = None
    amount_total: Optional[int] = None
    invoice_id: Optional[str] = Field(default="", alias="invoice")
    metadata: Optional[dict[str, str]] = None
    created: datetime
    expires_at: datetime  # cehckout session expires time, typically is created + 24h
    line_items: CheckoutSessionLineItems = Field(default_factory=lambda: CheckoutSessionLineItems(data=[], has_more=False, object="list"))

    @field_validator("created", "expires_at", mode="before")
    @classmethod
    def parse_timestamp(cls, v):
        return to_utc_datetime(v)

    @field_validator("subscription_id", mode="before")
    @classmethod
    def handle_null_subscription(cls, v):
        return "" if v is None else v


# -----------------------------------invoice.paid
class InvoicePeriod(BaseModel):
    start: datetime
    end: datetime

    @field_validator("start", "end", mode="before")
    @classmethod
    def parse_timestamp(cls, v):
        return to_utc_datetime(v)


class InvoiceProrationDetails(BaseModel):
    credited_items: Optional[dict] = None


class InvoiceSubscriptionItemDetails(BaseModel):
    subscription: Optional[str] = None
    subscription_item: str
    proration: bool
    proration_details: Optional[InvoiceProrationDetails] = None


class InvoiceParentDetails(BaseModel):
    type: Literal["subscription_item_details", "invoice_item_details"]
    subscription_item_details: Optional[InvoiceSubscriptionItemDetails] = None
    invoice_item_details: Optional[dict] = None


class InvoicePriceDetails(BaseModel):
    price: str
    product: str


class InvoicePricing(BaseModel):
    type: Literal["price_details"]
    price_details: InvoicePriceDetails
    unit_amount_decimal: Optional[str]


class InvoiceLineItem(BaseModel):
    id: str
    amount: int
    currency: str
    description: str
    quantity: int
    period: InvoicePeriod
    pricing: InvoicePricing
    discounts: list[dict] = Field(default_factory=list)
    taxes: list[dict] = Field(default_factory=list)
    parent: InvoiceParentDetails
    metadata: dict[str, str] = Field(default_factory=dict)


class InvoiceLines(BaseModel):
    data: list[InvoiceLineItem]
    has_more: bool
    total_count: int
    object: Literal["list"]
    url: Optional[str] = None


class InvoicePaid(BaseModel):
    id: str
    object: Literal["invoice"]
    number: str
    status: Optional[str] = None  # "paid", "draft", "void"
    currency: str

    amount_due: int  # should pay
    amount_paid: int
    amount_remaining: int
    subtotal: int
    total: int

    customer_id: str = Field(alias="customer")
    customer_email: Optional[str] = None
    customer_name: Optional[str] = None

    created: datetime
    period_start: datetime  # invoice
    period_end: datetime  # invoice
    effective_at: Optional[datetime] = None

    billing_reason: Optional[str] = None
    subscription_id: Optional[str] = Field(default=None, alias="subscription")

    hosted_invoice_url: Optional[str] = None
    invoice_pdf: Optional[str] = None

    lines: InvoiceLines

    metadata: Optional[dict[str, str]] = None

    description: Optional[str]

    @field_validator("created", "period_start", "period_end", "effective_at", mode="before")
    @classmethod
    def parse_timestamps(cls, v):
        return to_utc_datetime(v)


# -----------------------------------subscription.updated
class SubscriptionUpdatedPlan(BaseModel):
    id: str
    amount: Optional[int] = None
    interval: Optional[str] = None
    product: str
    nickname: Optional[str] = None


class SubscriptionUpdatedPrice(BaseModel):
    id: str
    product: str
    unit_amount: Optional[int] = None
    currency: Optional[str] = None
    nickname: Optional[str] = None
    recurring: Optional[dict] = None


class SubscriptionUpdatedSubscriptionItem(BaseModel):
    id: str
    price: SubscriptionUpdatedPrice
    quantity: Optional[int] = 1
    subscription_id: str = Field(alias="subscription")
    current_period_start: Optional[datetime] = None
    current_period_end: Optional[datetime] = None

    @field_validator("current_period_start", "current_period_end", mode="before")
    @classmethod
    def parse_optional_timestamps(cls, v):
        return to_utc_datetime(v)


class SubscriptionUpdatedSubscriptionItems(BaseModel):
    data: list[SubscriptionUpdatedSubscriptionItem]
    subscription: Optional[str] = None


class SubscriptionUpdatedSubscriptionObject(BaseModel):
    id: str
    object: Literal["subscription"]
    status: str
    customer_id: str = Field(alias="customer")
    plan: Optional[SubscriptionUpdatedPlan] = None
    items: SubscriptionUpdatedSubscriptionItems
    current_period_start: Optional[datetime] = None
    current_period_end: Optional[datetime] = None
    metadata: dict[str, str] = Field(default_factory=dict)
    cancel_at_period_end: Optional[bool] = False
    schedule: Optional[str] = None
    pending_update: Optional[dict] = None
    latest_invoice_id: Optional[str] = Field(alias="latest_invoice")
    billing_cycle_anchor: datetime
    trial_start: Optional[datetime] = None
    trial_end: Optional[datetime] = None

    @field_validator("current_period_start", "current_period_end", "billing_cycle_anchor", "trial_start", "trial_end", mode="before")
    @classmethod
    def parse_timestamps(cls, v):
        return to_utc_datetime(v)


class SubscriptionUpdatedPreviousAttributes(BaseModel):
    plan: Optional[SubscriptionUpdatedPlan] = None
    trial_end: Optional[datetime] = None
    status: Optional[str] = None
    latest_invoice: Optional[str] = None
    items: Optional[SubscriptionUpdatedSubscriptionItems] = None
    billing_cycle_anchor: Optional[datetime] = None

    @field_validator("trial_end", "billing_cycle_anchor", mode="before")
    @classmethod
    def parse_optional_timestamps(cls, v):
        return to_utc_datetime(v)


class SubscriptionUpdatedData(BaseModel):
    object: SubscriptionUpdatedSubscriptionObject
    previous_attributes: Optional[SubscriptionUpdatedPreviousAttributes] = None


class SubscriptionUpdated(BaseModel):
    id: str
    object: Literal["event"]
    created: datetime
    type: Literal["customer.subscription.updated"]
    api_version: Optional[str]

    data: SubscriptionUpdatedData

    @field_validator("created", mode="before")
    @classmethod
    def parse_created(cls, v):
        return to_utc_datetime(v)
