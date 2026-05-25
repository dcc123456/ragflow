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
Common utilities for storage billing API flows.
"""

from __future__ import annotations

import sys
from pathlib import Path

import stripe  # type: ignore[reportMissingImports]

PROJECT_ROOT = Path(__file__).resolve().parents[4]
if str(PROJECT_ROOT) not in sys.path:
    sys.path.insert(0, str(PROJECT_ROOT))

BYTES_PER_GB = 1000 * 1000 * 1000


def attach_default_test_card(customer_id: str) -> str:
    """Attach the shared test Visa card (pm_card_visa) to the customer and return its ID."""
    attached = stripe.PaymentMethod.attach("pm_card_visa", customer=customer_id)
    pm_id = getattr(attached, "id", None) or (attached.get("id") if isinstance(attached, dict) else None) or "pm_card_visa"
    stripe.Customer.modify(customer_id, invoice_settings={"default_payment_method": pm_id})
    return pm_id


def attach_decline_test_card(customer_id: str, payment_method_id: str = "pm_card_chargeCustomerFail") -> str:
    """Attach a reusable Stripe test card that fails when charged."""
    attached = stripe.PaymentMethod.attach(payment_method_id, customer=customer_id)
    pm_id = getattr(attached, "id", None) or (attached.get("id") if isinstance(attached, dict) else None) or payment_method_id
    stripe.Customer.modify(customer_id, invoice_settings={"default_payment_method": pm_id})
    return pm_id


def gb_to_bytes(gb: int) -> int:
    return gb * BYTES_PER_GB
