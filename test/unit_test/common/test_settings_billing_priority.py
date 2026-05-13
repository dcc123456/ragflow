from collections import defaultdict

import pytest
from rag.graphrag import search as kg_search

from common import settings


def test_init_settings_requires_billing_plan_priority(monkeypatch):
    def fake_get_base_config(name, default=None):
        if name == "billing":
            return {
                "billing_plans": [
                    {
                        "name": "Trial",
                        "price_ids": "price_trial",
                        "task_priority": "low",
                    }
                ]
            }
        return default or {}

    monkeypatch.setattr(settings, "get_base_config", fake_get_base_config)
    monkeypatch.setattr(settings, "BILLING", {})
    monkeypatch.setattr(settings, "BILLING_PRICEID_TO_PRODUCT", {})
    monkeypatch.setattr(settings, "BILLING_PRIORITY_TO_PLANS", defaultdict(list))
    monkeypatch.setattr(settings, "BILLING_PLAN_TO_INFO", {})
    monkeypatch.setattr(settings, "BILLING_PRICE_POINT", {})
    monkeypatch.setattr(settings.rag.utils.es_conn, "ESConnection", lambda: object())
    monkeypatch.setattr(settings.memory_es_conn, "ESConnection", lambda: object())
    monkeypatch.setattr(settings.StorageFactory, "create", lambda _storage: object())
    monkeypatch.setattr(settings.search, "Dealer", lambda _conn: object())
    monkeypatch.setattr(kg_search, "KGSearch", lambda _conn: object())

    with pytest.raises(ValueError, match="Billing plan 'Trial' is missing required priority"):
        settings.init_settings()