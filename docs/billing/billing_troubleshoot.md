# RAGFlow Stripe Billing 排障指南

## 概述

典型现象：租户在 Stripe Checkout 完成支付、成功页也弹出了，但控制台仍显示旧套餐（例如付了 Starter 仍显示 Trial）。本文按「数据库 → Stripe → 日志 → 修复」的顺序给出排查路径。

排查前先确认三件事：

1. 服务端 `billing_enabled=true`（`GET /api/v1/billing/status`）
2. `service_conf.yaml` 含 `billing` 段，且 `BILLING_ENABLED=1`
3. Stripe webhook 能到达本服务（生产为公网地址；本地用 `stripe listen` 转发到 `http://127.0.0.1:<port>/v1/billing/webhooks/stripe`）

## 快速判断

| 现象 | 首选检查 |
|---|---|
| Stripe 已支付，DB 仍是 Trial，`billing_webhook_event` 无记录 | webhook 根本没送达（URL 不可达 / 未注册成功） |
| 事件表有记录但 `processing_status=processing` 且时间很久 | 处理进程崩溃或 lease 未释放（见 Step 4） |
| 事件表 `failed` 且 `last_error` 非空 | 处理函数报错，按 `last_error` 定位 |
| DB 有新套餐但 `entitlement_ready=false` | 本地套餐与 Stripe 订阅价格未收敛（`plan_name` vs `items[].price`） |
| 订单表缺失但订阅已生效 | `invoice.paid` 未处理或 `latest_invoice` 身份校验未通过 |

## Step 1：查询数据库

Docker Compose：

```bash
docker exec -it <mysql-container> mysql -uroot -p'<password>' rag_flow
```

Kubernetes：

```bash
kubectl exec -n ragflow mysql-0 -- mysql -uragflow -p'<password>' rag_flow
```

### 1.1 订阅行

```sql
SELECT id, tenant_id, plan_name, product_id, price_id, status, subscription_status,
       subscription_id, customer_id, invoice_id,
       start_time, end_time, last_event_at,
       addon_subscription_item_id, addon_storage_bytes,
       target_storage_bytes, target_plan_name, schedule_id
FROM billing_subscription
WHERE tenant_id = '<tenant_id>';
```

关键列：

- `plan_name` / `price_id`：本地认定生效的套餐与价格
- `subscription_status`：Stripe 订阅状态（`active` / `past_due` / `canceled` …）
- `last_event_at`：已应用的最新 Stripe 事件时间，用于判断"新事件是否被旧事件覆盖"
- `target_plan_name` / `target_storage_bytes` / `schedule_id`：待生效的降级或存储变更

### 1.2 webhook 事件

```sql
SELECT event_id, event_type, object_id, processing_status,
       processing_started_at, processed_at, failed_at, last_error, received_at
FROM billing_webhook_event
ORDER BY received_at DESC
LIMIT 20;
```

`processing_status` 的取值与含义：

| 值 | 含义 |
|---|---|
| `processing` | 已被某个投递声明处理权，`processing_started_at` 为 lease 起点（15 分钟） |
| `completed` | 已处理且产生本地变更 |
| `unhandled` | 已处理但该事件类型不改变权益（如 `payment_intent.*`、订阅型 `checkout.session.completed`） |
| `failed` | 处理报错，`last_error` 记录原因，等待启动补投重试 |

### 1.3 订单

```sql
SELECT order_id, tenant_id, payment_type, payment_status, amount_cents,
       currency, payment_subscription_id, receipt_url
FROM billing_payment_order
WHERE tenant_id = '<tenant_id>'
ORDER BY create_time DESC
LIMIT 10;
```

## Step 2：查询 Stripe 权威状态

当前 Stripe API 使用 **Bearer** 认证（`-u sk_xxx:` 的 Basic Auth 已失效）：

```bash
export STRIPE_KEY='sk_test_...'

curl -s -H "Authorization: Bearer $STRIPE_KEY" \
  "https://api.stripe.com/v1/subscriptions/<subscription_id>"

curl -s -H "Authorization: Bearer $STRIPE_KEY" \
  "https://api.stripe.com/v1/checkout/sessions/<cs_id>"

curl -s -H "Authorization: Bearer $STRIPE_KEY" \
  "https://api.stripe.com/v1/invoices/<invoice_id>"

curl -s -H "Authorization: Bearer $STRIPE_KEY" \
  "https://api.stripe.com/v1/prices/<price_id>"

curl -s -H "Authorization: Bearer $STRIPE_KEY" \
  "https://api.stripe.com/v1/events?limit=20"
```

注意：新版 API 中计费周期位于 **订阅项**上，不在订阅对象上：

```bash
curl -s -H "Authorization: Bearer $STRIPE_KEY" \
  "https://api.stripe.com/v1/subscriptions/<subscription_id>" | python3 -c "
import json,sys,datetime
d=json.load(sys.stdin)
print('status:', d['status'])
for item in d['items']['data']:
    print('item:', item['id'], 'price:', item['price']['id'], 'qty:', item['quantity'])
    print('  period:', datetime.datetime.utcfromtimestamp(item['current_period_start']),
          '->', datetime.datetime.utcfromtimestamp(item['current_period_end']))
inv = d.get('latest_invoice')
print('latest_invoice:', inv['id'] if isinstance(inv, dict) else inv)
"
```

## Step 3：对比分析

| 对比项 | 数据库 | Stripe |
|---|---|---|
| 套餐 | `plan_name` | `items[].price.id` 经 `billing.price_ids → plan` 映射 |
| 价格 | `price_id` | `items[].price.id` |
| 状态 | `subscription_status` | `status` |
| 周期 | `start_time` / `end_time` | `items[].current_period_start/end` |
| 最新发票 | `invoice_id` | `latest_invoice` |
| 事件水位 | `last_event_at` | 事件的 `created` |

判断规则：

- Stripe 有新订阅、DB 仍 Trial 且事件表为空 → webhook 未送达（检查 URL 可达性、endpoint 注册是否成功）
- 事件表 `completed` 但 DB 未变更 → 处理逻辑判定为"无需变更"（例如历史发票、旧订阅、校验未通过），需看 Step 4 的身份校验分支
- DB 比 Stripe 旧、且 `last_event_at` 早于 Stripe 最新事件 → 该事件被跳过或未送达

## Step 4：日志与事件状态机

```bash
# 单机运行
grep -iE "billing|webhook" logs/api_server_9380.log | tail -50

# 容器
docker logs <api-container> --tail 300 | grep -iE "billing|webhook"

# K8s
kubectl logs -n ragflow -l app=ragflow-server --tail=300 | grep -iE "billing|webhook"
```

日志关键字：

| 关键字 | 说明 |
|---|---|
| `billing retryable event failed` | 启动补投处理失败，附带 `event_id` 与错误 |
| `billing retryable event drain failed` | 补投整体失败（会作为错误返回） |
| `billing webhook replay failed` | 按 checkpoint 回放 Stripe 事件失败 |
| `Billing undelivered event replay failed` | 启动补投整体失败 |
| `Billing downgrade guard sweep failed` | 降级守卫单次扫描失败 |
| `Billing API token rate limit sync failed` | 限流键同步失败 |
| `deleted stale webhook endpoint` | 重建 webhook endpoint 时清理旧地址 |
| `Billing scheduled downgrade cancelled: quota exceeded` | 降级守卫撤销排期 |

关于"事件被忽略"：Go 版本**不会**输出 `Ignore stale ...` 之类的日志，旧事件是静默丢弃的（返回成功、不落库变更）。判断依据是：

1. `billing_subscription.last_event_at` 是否晚于该事件的 `created`
2. 同一订阅的旧事件（`created` 更早）会被丢弃
3. 周期回退（`period_start` 早于 `start_time`）也会被丢弃

`processing` 长时间不结束：lease 为 15 分钟，超过后启动补投会重新声明处理权；若确认进程已崩溃且不想等，可手工把该行改回 `failed`：

```sql
UPDATE billing_webhook_event
SET processing_status = 'failed', processing_started_at = NULL,
    last_error = 'manual reset'
WHERE event_id = '<evt_id>';
```

## Step 5：手工修复

仅在确认 Stripe 状态正确、只是本地未收敛时使用。

```sql
UPDATE billing_subscription
SET plan_name = '<Starter|Pro|Trial>',
    product_id = '<product_id>',
    price_id = '<price_id>',
    status = 'active',
    subscription_status = 'active',
    subscription_id = '<sub_id>',
    customer_id = '<cus_id>',
    invoice_id = '<in_id>',
    start_time = '<YYYY-MM-DD HH:MM:SS>',
    end_time = '<YYYY-MM-DD HH:MM:SS>',
    last_event_at = NOW(),
    update_time = UNIX_TIMESTAMP(),
    update_date = NOW()
WHERE tenant_id = '<tenant_id>';
```

`product_id` 取 `billing_product` 中该套餐的最新版本：

```sql
SELECT name, version, price_ids, quota_apps, quota_members, quota_storage, quota_points
FROM billing_product
ORDER BY name, version DESC;
```

修复后必须收尾，否则下一次事件仍会按旧状态收敛：

1. 确认 `billing_webhook_event` 中没有 `failed` 残留（否则重启会再次处理）
2. 重启 API 服务触发补投，或让 Stripe 重发对应事件（Dashboard → Events → Resend）
3. 复核 `GET /api/v1/billing/subscription` 与 `GET /api/v1/billing/subscriptions/<sub_id>/payment-status` 的 `plan_name`、`entitlement_ready`

## Step 6：常见问题

### 1. 支付成功但事件表为空

webhook URL 不可达或未注册。生产环境要求 `billing.webhook_url` 是公网 `https` 地址；本地回环地址 Stripe 无法访问，必须用 `stripe listen` 转发（回环地址会跳过签名校验，因此无需配置 `whsec_`）。

### 2. 事件 `completed` 但订阅没变

`invoice.paid` / `subscription.updated` 有身份校验：

- 发票必须属于当前订阅的 `latest_invoice`
- 订阅 ID 必须与本地 `subscription_id` 一致（否则视为历史订阅，只记订单不改权益）
- 事件时间不得早于 `last_event_at`

对应场景（历史发票补付、旧订阅延迟事件）属于预期行为。

### 3. 重复充值 / 积分翻倍

积分只由 `checkout.session.completed` 按 `checkout:<session_id>` 幂等入账；`payment_intent.succeeded` 仅审计。若发现翻倍，检查 `billing_point_ledger` 的 `idempotency_key` 是否重复。

### 4. 升级后 `entitlement_ready=false`

表示本地套餐与 Stripe 订阅价格尚未一致：可能升级发票仍未支付（`payment_state=requires_action`），或 webhook 尚未到达。前端会持续轮询 `payment-status`，事件到达后自动变为 `true`。

### 5. 降级预约后被撤销

降级守卫每 10 分钟检查一次，若当前用量超过目标套餐配额，会释放 Stripe schedule 并清空 `target_*`/`schedule_id`，只在日志留 `Billing scheduled downgrade cancelled: quota exceeded`。需释放资源后重新预约。
