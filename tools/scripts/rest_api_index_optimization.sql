-- RAGFlow REST API index optimization for MySQL 8.0+
--
-- Generated from the REST API query audit and the supplied SHOW INDEXES output.
-- This script only adds indexes; it does not remove the existing duplicate indexes.
-- Run against a staging copy first and confirm the plans with EXPLAIN ANALYZE.

-- ---------------------------------------------------------------------------
-- Phase 1: confirmed unindexed equality lookups
-- ---------------------------------------------------------------------------

-- Provider/model endpoints filter tenant_model_instance by provider_id and
-- commonly by instance_name. The supplied schema has no index on either field.
ALTER TABLE tenant_model_instance
    ADD INDEX idx_tmi_provider_instance (provider_id, instance_name),
    ALGORITHM=INPLACE, LOCK=NONE;

-- Stripe webhook processing looks up and updates payment orders by order_id,
-- and checks payment_intent_id for idempotency.
ALTER TABLE billing_payment_order
    ADD INDEX idx_bpo_order_id (order_id),
    ADD INDEX idx_bpo_payment_intent_id (payment_intent_id),
    ALGORITHM=INPLACE, LOCK=NONE;

-- ---------------------------------------------------------------------------
-- Phase 2: REST list/filter queries that combine predicates with ordering
-- ---------------------------------------------------------------------------

-- GET /files: tenant_id + parent_id, normally ordered by create_time.
ALTER TABLE file
    ADD INDEX idx_file_tenant_parent_created (tenant_id, parent_id, create_time),
    ALGORITHM=INPLACE, LOCK=NONE;

-- Dataset document listing: kb_id, normally ordered by create_time.
ALTER TABLE document
    ADD INDEX idx_document_kb_created (kb_id, create_time),
    ALGORITHM=INPLACE, LOCK=NONE;

-- Dataset ingestion log listing and per-dataset retention cleanup.
ALTER TABLE pipeline_operation_log
    ADD INDEX idx_pipeline_log_kb_created (kb_id, create_time),
    ALGORITHM=INPLACE, LOCK=NONE;

-- Chat session listing.
ALTER TABLE conversation
    ADD INDEX idx_conversation_dialog_created (dialog_id, create_time),
    ALGORITHM=INPLACE, LOCK=NONE;

-- Chat listing for a tenant.
ALTER TABLE dialog
    ADD INDEX idx_dialog_tenant_status_created (tenant_id, status, create_time),
    ALGORITHM=INPLACE, LOCK=NONE;

-- Agent/dataflow listing for an owner and canvas category.
ALTER TABLE user_canvas
    ADD INDEX idx_canvas_user_category_created (user_id, canvas_category, create_time),
    ALGORITHM=INPLACE, LOCK=NONE;

-- Latest/released agent version lookup.
ALTER TABLE user_canvas_version
    ADD INDEX idx_canvas_version_canvas_release_created (user_canvas_id, release, create_time),
    ALGORITHM=INPLACE, LOCK=NONE;

-- Repeated tenant membership and authorization lookup.
ALTER TABLE user_tenant
    ADD INDEX idx_user_tenant_tenant_user_status (tenant_id, user_id, status),
    ALGORITHM=INPLACE, LOCK=NONE;

-- Document task lookup and ordering.
ALTER TABLE task
    ADD INDEX idx_task_doc_page_created (doc_id, from_page, create_time),
    ALGORITHM=INPLACE, LOCK=NONE;

-- Billing point history filtering and ordering.
ALTER TABLE billing_point_ledger
    ADD INDEX idx_point_ledger_tenant_event_created (tenant_id, event_type, create_time),
    ALGORITHM=INPLACE, LOCK=NONE;

-- Billing invoice history filtering and ordering.
ALTER TABLE billing_payment_order
    ADD INDEX idx_bpo_tenant_order_created (tenant_id, order_created_at),
    ALGORITHM=INPLACE, LOCK=NONE;

-- Permission lookup by tenant and resource.
ALTER TABLE permission
    ADD INDEX idx_permission_resource_access (tenant_id, resource_type, resource_id, status),
    ALGORITHM=INPLACE, LOCK=NONE;

-- Refresh optimizer statistics after creating the indexes.
ANALYZE TABLE tenant_model_instance, billing_payment_order, file, document,
    pipeline_operation_log, conversation, dialog, user_canvas,
    user_canvas_version, user_tenant, task, billing_point_ledger, permission;

-- Verify that every index created by this script is visible.
SELECT table_name, index_name, non_unique,
       GROUP_CONCAT(column_name ORDER BY seq_in_index) AS indexed_columns
FROM information_schema.statistics
WHERE table_schema = DATABASE()
  AND index_name IN (
      'idx_tmi_provider_instance',
      'idx_bpo_order_id',
      'idx_bpo_payment_intent_id',
      'idx_file_tenant_parent_created',
      'idx_document_kb_created',
      'idx_pipeline_log_kb_created',
      'idx_conversation_dialog_created',
      'idx_dialog_tenant_status_created',
      'idx_canvas_user_category_created',
      'idx_canvas_version_canvas_release_created',
      'idx_user_tenant_tenant_user_status',
      'idx_task_doc_page_created',
      'idx_point_ledger_tenant_event_created',
      'idx_bpo_tenant_order_created',
      'idx_permission_resource_access'
  )
GROUP BY table_name, index_name, non_unique
ORDER BY table_name, index_name;

-- ---------------------------------------------------------------------------
-- Optional correctness constraints: inspect before applying
-- ---------------------------------------------------------------------------

-- order_id is described as unique per payment in the application model. Check
-- for duplicates before replacing idx_bpo_order_id with a UNIQUE index:
--
-- SELECT order_id, COUNT(*) AS row_count
-- FROM billing_payment_order
-- GROUP BY order_id
-- HAVING COUNT(*) > 1;
--
-- ALTER TABLE billing_payment_order
--     DROP INDEX idx_bpo_order_id,
--     ADD UNIQUE INDEX uq_bpo_order_id (order_id),
--     ALGORITHM=INPLACE, LOCK=NONE;

-- The application model expects one subscription per tenant, but the supplied
-- database has a non-unique tenant_id index. Check duplicates before enforcing
-- the model constraint:
--
-- SELECT tenant_id, COUNT(*) AS row_count
-- FROM billing_subscription
-- GROUP BY tenant_id
-- HAVING COUNT(*) > 1;
--
-- ALTER TABLE billing_subscription
--     ADD UNIQUE INDEX uq_billing_subscription_tenant (tenant_id),
--     ALGORITHM=INPLACE, LOCK=NONE;
