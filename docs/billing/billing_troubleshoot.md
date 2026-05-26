# RAGFlow Stripe Billing Webhook 排障指南

## 概述

当租户从 Trial 升级到付费 plan（如 Starter/Pro）成功，但界面仍显示原 plan 时，按以下步骤排查。

## 排查步骤

### Step 1: 查询 MySQL 数据库记录

**连接方式**：
```bash
kubectl exec -n ragflow mysql-0 -- mysql -uragflow -p'<password>' rag_flow
```

**查询租户信息**（通过邮箱或 tenant_id）：
```sql
-- 通过邮箱查找 tenant_id
SELECT id, email, status FROM user WHERE email='<email>';

-- 查询 billing_subscription
SELECT id, plan_name, product_id, price_id, status, subscription_status,
       customer_id, subscription_id, start_time, end_time
FROM billing_subscription
WHERE tenant_id='<tenant_id>';

-- 查询 billing_product（获取 plan 名称映射）
SELECT id, name, price_ids, product_type FROM billing_product;

-- 查询最近 webhook 事件
SELECT event_id, event_type, object_id, processing_status, received_at
FROM billing_webhook_event
WHERE object_id='<subscription_id>' OR payload LIKE '%<customer_id>%'
ORDER BY received_at DESC LIMIT 10;
```

### Step 2: 查询 Stripe API 获取权威状态

**必要信息**：
- `customer_id`（来自 billing_subscription 表）
- `subscription_id`（来自 billing_subscription 表）

**查询命令**：
```bash
# 获取订阅详情
curl -s "https://api.stripe.com/v1/subscriptions/<subscription_id>" \
  -u "sk_live_xxx:"

# 查询客户信息
curl -s "https://api.stripe.com/v1/customers/<customer_id>" \
  -u "sk_live_xxx:"

# 查询客户所有订阅
curl -s "https://api.stripe.com/v1/customers/<customer_id>/subscriptions" \
  -u "sk_live_xxx:"

# 查询 price 详情
curl -s "https://api.stripe.com/v1/prices/<price_id>" \
  -u "sk_live_xxx:"

# 查询 product 详情
curl -s "https://api.stripe.com/v1/products/<product_id>" \
  -u "sk_live_xxx:"

# 查询事件历史
curl -s "https://api.stripe.com/v1/events?type=subscription%2A&customer=<customer_id>&limit=20" \
  -u "sk_live_xxx:"
```

### Step 3: 对比分析

| 对比项 | 数据库值 | Stripe 值 | 是否一致 |
|--------|---------|-----------|---------|
| plan_name | ? | metadata.product_name 或 price 对应 product | |
| product_id | ? | items.data[0].price.product | |
| price_id | ? | items.data[0].price.id | |
| 状态 | subscription_status | status | |
| 计费周期 | start_time / end_time | current_period_start / current_period_end | |

**关键判断**：
- Stripe 显示 Starter，DB 显示 Trial → **Webhooks 未正确处理**
- Stripe 显示 active，DB 显示不同状态 → **状态同步失败**

### Step 4: 检查 Webhook 处理日志

```bash
# 查看 ragflow pod 日志
kubectl logs -n ragflow -l app=ragflow-server --tail=500 | grep -i subscription

# 搜索特定事件
kubectl exec -n ragflow <pod-name> -- grep "evt_xxx" /ragflow/logs/ragflow_server.log.2

# 查找 "Ignore stale" 日志
kubectl logs -n ragflow <pod-name> --tail=500 | grep "Ignore stale"
```

**常见问题**：日志中出现 `Ignore stale customer.subscription.updated` 说明 webhook 被误判为过期而忽略。

### Step 5: 手工修复数据库

**确定正确值后，执行 UPDATE**：
```sql
UPDATE billing_subscription
SET
    plan_name = '<Starter|Pro等>',
    product_id = '<product_id>',
    price_id = '<price_id>',
    status = 'active',
    subscription_status = 'active',
    start_time = '<YYYY-MM-DD HH:MM:SS>',
    end_time = '<YYYY-MM-DD HH:MM:SS>',
    update_time = UNIX_TIMESTAMP(),
    update_date = NOW()
WHERE tenant_id = '<tenant_id>';
```

**获取 Stripe 数据的正确格式**：
```bash
# 获取计费周期
curl -s "https://api.stripe.com/v1/subscriptions/<subscription_id>" \
  -u "sk_live_xxx:" | python3 -c "
import sys,json
from datetime import datetime
d=json.load(sys.stdin)
print(f'start_time: {datetime.fromtimestamp(d[\"current_period_start\"])}')
print(f'end_time: {datetime.fromtimestamp(d[\"current_period_end\"])}')
print(f'status: {d[\"status\"]}')
items = d.get('items',{}).get('data',[])
for item in items:
    price = item.get('price',{})
    print(f'price_id: {price[\"id\"]}')
    prod = price.get('product')
    if isinstance(prod, dict):
        print(f'product_name: {prod.get(\"name\")}')
"
```

## 常见问题

### 1. Webhook 被标记为 stale 而忽略

**症状**：`billing_webhook_event` 显示 `completed`，但 `billing_subscription` 未更新。

**原因**：`_should_apply_subscription_event()` 函数误判同一订阅的计划变更为 stale 事件。

**日志**：
```
Ignore stale customer.subscription.updated for tenant <id>:
event subscription <sub_id> status=active current subscription=<sub_id>
```

**处理**：手工修复 DB 后，需修复代码逻辑。

### 2. BILLING_PRICEID_TO_PRODUCT 为空

**症状**：代码中 `settings.BILLING_PRICEID_TO_PRODUCT.get(price_id)` 返回空。

**原因**：在容器中以独立python进程检查将获得与 ragflow 进程并不一致的配置。应当检查容器内 `/ragflow/conf/service_conf.yaml` 中的 `billing.billing_plans` 是否包含正确的 price_id 映射。

### 3. previous_attributes 包含 plan 变化但未触发更新

**原因**：同订阅 ID 的 plan 变更被认为是 "stale snapshot"，被 `_should_apply_subscription_event()` 拒绝。

**修复**：在 `_should_apply_subscription_event()` 中，当 `previous` 包含 `plan` 或 `items` 变化时，应允许处理。

## 相关代码位置

| 文件 | 函数 | 说明 |
|------|------|------|
| `api/services/billing_webhook_service.py` | `_handle_customer_subscription_updated` | 处理 subscription.updated 事件 |
| `api/services/billing_webhook_service.py` | `_should_apply_subscription_event` | 判断事件是否应处理 |
| `api/utils/billing.py` | `extract_plan_item_and_price` | 提取订阅中的 plan/price |
| `api/db/services/billing_service.py` | `upsert_subscription` | 插入或更新订阅记录 |
| `common/settings.py` | 加载逻辑 | `BILLING_PRICEID_TO_PRODUCT` 映射 |

## 修复后验证

1. 检查 DB 记录已更新
2. 清除相关缓存（如有）
3. 通过前端验证 plan 显示正确
4. 监控后续 webhook 事件是否正常处理