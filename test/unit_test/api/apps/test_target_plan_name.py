#
#  Copyright 2026 The InfiniFlow Authors. All Rights Reserved.
#
"""Unit tests for target_plan_name field lifecycle."""

import inspect


# ---------------------------------------------------------------------------
# test: model has target_plan_name field
# ---------------------------------------------------------------------------

def test_field_exists_on_model():
    from api.db.db_models import Subscription
    assert hasattr(Subscription, "target_plan_name"), \
        "Subscription model must have target_plan_name field"


# ---------------------------------------------------------------------------
# test: raw fields include target_plan_name
# ---------------------------------------------------------------------------

def test_field_in_raw_subscription_fields():
    from api.db.services.billing_service import SubscriptionService
    assert SubscriptionService.model.target_plan_name in \
        SubscriptionService._RAW_SUBSCRIPTION_FIELDS, \
        "_RAW_SUBSCRIPTION_FIELDS must include target_plan_name"


# ---------------------------------------------------------------------------
# test: database migration added column
# ---------------------------------------------------------------------------

def test_migration_adds_target_plan_name():
    with open("api/db/db_models.py") as f:
        src = f.read()
    assert 'alter_db_add_column(migrator, "billing_subscription", "target_plan_name"' in src, \
        "migrate_db() must call alter_db_add_column for billing_subscription.target_plan_name"


# ---------------------------------------------------------------------------
# test: webhook clears target_plan_name on plan change
# ---------------------------------------------------------------------------

def test_webhook_clears_target_plan_name():
    from api.services.billing_webhook_service import _sync_main_subscription_from_stripe
    source = inspect.getsource(_sync_main_subscription_from_stripe)
    assert "target_plan_name" in source and "plan_changed" in source, \
        "subscription_dict must conditionally clear target_plan_name when plan changes"


# ---------------------------------------------------------------------------
# test: billing_app writes target_plan_name on downgrade
# ---------------------------------------------------------------------------

def test_billing_app_writes_target_plan_name_on_downgrade():
    with open("api/apps/billing_app.py") as f:
        src = f.read()
    assert "target_plan_name=target_plan_name_for_downgrade" in src, \
        "downgrade branch must write target_plan_name to DB"


# ---------------------------------------------------------------------------
# webhook final defense: plan downgrade gated by plan_changed,
# storage downgrade independent of plan_changed
# ---------------------------------------------------------------------------

def test_plan_downgrade_gated_by_plan_changed(monkeypatch):
    """plan_changed=False → plan downgrade defense does NOT fire."""
    from api.services.downgrade_guard import check_downgrade_effective_exceeded

    pre_sync = {
        "plan_name": "Starter", "target_plan_name": "Trial",
        "addon_storage_bytes": 0, "target_storage_bytes": None,
    }
    monkeypatch.setattr(
        "api.services.downgrade_guard._load_tenant_data",
        lambda *_: ({"num_storage_bytes": 10**6, "num_members": 1, "num_apps": 2},
                     {"quota_storage": 10**6, "quota_members": 1, "quota_apps": 5}),
    )
    r = check_downgrade_effective_exceeded("t1", pre_sync, plan_changed=False)
    assert r is None, f"plan_changed=False should skip defense, got {r}"


def test_storage_downgrade_fires_without_plan_changed(monkeypatch):
    """plan_changed=False + storage 40→20GB exceeded → defense fires."""
    from api.services.downgrade_guard import check_downgrade_effective_exceeded

    pre_sync = {
        "plan_name": "Starter", "target_plan_name": None,
        "addon_storage_bytes": 40 * 1000**3,
        "target_storage_bytes": 20 * 1000**3,
    }
    monkeypatch.setattr(
        "api.services.downgrade_guard._load_tenant_data",
        lambda *_: ({"num_storage_bytes": 30 * 1000**3, "num_members": 1, "num_apps": 2},
                     {"quota_storage": 5 * 1000**3, "quota_members": 1, "quota_apps": 5}),
    )
    r = check_downgrade_effective_exceeded("t1", pre_sync, plan_changed=False)
    assert r is not None, "storage downgrade must fire regardless of plan_changed"
    assert r.get("storage_used", 0) > r.get("storage_limit", 0)
