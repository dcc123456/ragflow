# RAGFlow 计费系统文档

计费系统业务规则参考 [billing_spec_zh.md](./billing_spec_zh.md)。本文档记录系统实现、接口与测试流程说明。

## 1. 系统概述

RAGFlow 使用 Stripe 作为支付提供商，采用**单租户单订阅**模型。每个租户只有一个订阅，该订阅可以包含多个产品（套餐 + 存储附加）。所有计费操作都通过 Stripe Checkout 或直接 Stripe API 调用完成，Webhook 处理器将状态同步回本地数据库。

### 核心组件

| 组件 | 说明 |
|------|------|
| `billing_app.py` | Quart API 端点，处理计费相关操作 |
| `billing_service.py` | 数据库服务层，处理订阅/订单 |
| `billing_client.py` | 计费 API 流程的测试客户端 |
| `billing_common.py` | 共享工具函数（配置加载、Stripe 辅助） |
| `points_common.py` | 积分专用工具 |
| `storage_common.py` | 存储专用工具 |

### Stripe Webhook 事件

| 事件 | 用途 |
|------|------|
| `checkout.session.completed` | 订阅 Checkout 完成；积分充值成功时直接入账 |
| `invoice.paid` | 收到订阅或附加项付款（续费、升级、存储加购） |
| `invoice.payment_failed` | 付款失败 → 欠费状态 |
| `customer.subscription.updated` | 订阅状态、套餐或存储变更同步回本地 |
| `customer.subscription.deleted` | 订阅已取消 |
| `payment_intent.succeeded` | 一次性支付成功事件，保留给兼容的一次性 addon 流程 |

---

## 2. 订阅套餐

### 套餐层级关系

```
Trial → Starter → Pro
  ↑        ↓
  └────────┘（在计费周期结束时降级）
```

### 套餐配额

每个套餐在 `service_conf.yaml` 中定义资源限制：

| 资源 | Trial | Starter | Pro |
|------|-------|---------|-----|
| `quota_apps` | 5 | 50 | 100000 |
| `quota_members` | 1 | 5 | 20 |
| `quota_storage` | 100MB | 5GB | 50GB |
| `quota_points` | 500 | 5000 | 20000 |

### 订阅状态

| 状态 | 有权使用？ | 说明 |
|------|-----------|------|
| `active` | ✅ | 正常运行 |
| `trialing` | ✅ | 试用期内 |
| `past_due` | ❌ | 付款失败，可恢复 |
| `incomplete` | ❌ | 初始付款待处理 |
| `incomplete_expired` | ❌ | 付款窗口已过期 |
| `unpaid` | ❌ | 多次重试后付款失败 |
| `canceled` | ❌ | 订阅已取消 |
| `paused` | ❌ | 订阅已暂停 |

---

## 3. 套餐升级/降级流程

### 3.1 Trial → Starter（升级）

**触发条件**：用户完成 Stripe Checkout 会话。

**流程**：
1. 前端调用 `POST /billing/checkout`，传入 Starter 的 `price_id`
2. 后端创建 Stripe Checkout 会话 → 返回跳转地址
3. 用户在 Stripe 完成订阅 Checkout → 触发 `checkout.session.completed`
4. 订阅状态由 `customer.subscription.updated` 同步到本地数据库
5. 计费周期立即开始（从现在开始新周期）

**前置条件**：
- 租户必须有 Stripe `customer_id`
- 需要有效的支付方式

**生效时间**：立即生效（开始新的计费周期）

### 3.2 Starter → Pro（升级）

**触发条件**：用户调用 `POST /billing/checkout`，传入 Pro 的 `price_id`。

**流程**：
1. 后端调用 `modify_subscription_plan_async()` 修改 Stripe 订阅
2. Stripe 立即收取按比例计算的差价（`proration_behavior: always_invoice`）
3. `invoice.paid` 与 `customer.subscription.updated` Webhook 同步付款和订阅状态
4. 数据库更新为 "Pro" 套餐

**前置条件**：
- 活跃的 Starter 订阅
- 已保存有效的支付方式

**生效时间**：立即生效（按比例收费）

### 3.3 Pro → Starter（降级）

**触发条件**：用户调用 `POST /billing/checkout`，传入 Starter 的 `price_id`。

**流程**：
1. 后端调用 `schedule_subscription_price_change_at_period_end_async()`
2. Stripe 创建 `SubscriptionSchedule`，包含待处理的变更
3. `pending_subscription_change` 出现在 `GET /billing/current_plan` 响应中
4. 在计费周期结束时 → 触发 `customer.subscription.updated` Webhook
5. 数据库更新为 "Starter" 套餐，开始新的计费周期

**前置条件**：
- 活跃的 Pro 订阅
- 无资源冲突（apps ≤ Starter 配额等）

**生效时间**：当前计费周期结束时

### 3.4 Starter → Trial（降级）

**触发条件**：用户调用 `POST /billing/checkout`，传入 Trial 的 `price_id`。

**流程**：
1. 后端通过 `_check_downgrade_resource_compatibility()` 验证资源兼容性
2. 如果使用量超过 Trial 配额 → 返回错误及冲突详情
3. 如果兼容 → 在计费周期结束时安排降级
4. **存储附加自动取消**（数量设为 0）
5. 在计费周期结束时 → 套餐变更为 "Trial"

**前置条件**：
- 活跃的付费订阅
- 所有资源使用量在 Trial 限制内：
  - `apps_used ≤ Trial quota_apps`
  - `members_used ≤ Trial quota_members`
  - `storage_used ≤ Trial quota_storage`（不保留附加存储）

**生效时间**：当前计费周期结束时

**重要提示**：Trial 套餐**不支持**存储附加。降级到 Trial 时，任何现有的存储附加都会被取消。

---

## 4. 资源兼容性检查

在任何降级之前，`_check_downgrade_resource_compatibility()` 会验证：

```python
conflicts = []
if storage_used > total_storage_limit:
    conflicts.append({"resource": "storage", ...})
if members_used > target_quota_members:
    conflicts.append({"resource": "members", ...})
if apps_used > target_quota_apps:
    conflicts.append({"resource": "apps", ...})
```

说明：当前实现不检查 points 使用量；当目标套餐为 Trial 时，存储兼容性仅按 Trial 套餐自带存储配额计算，不保留已购存储附加。

**冲突响应**：
```json
{
  "code": 40006,
  "data": {"resource_conflicts": [...]},
  "message": "Resource usage exceeds Trial quota: storage, apps. ..."
}
```

---

## 5. 存储附加

### 5.1 概述

存储附加是**与套餐在同一订阅上的行项目**，不是单独的订阅。

| 属性 | 值 |
|------|-----|
| 价格 | $10/GB/月（可配置） |
| 最小单位 | 1GB 递增 |
| 计费 | 周期中变更按比例计算 |
| Trial 支持 | ❌ Trial 套餐不允许 |

### 5.2 添加存储

**端点**：`POST /billing/storage/set-target`

**流程**：
1. 如果 `target > current` → 立即修改 Stripe 订阅，按比例收费
2. 系统立即同步新的目标存储额度，并返回 Stripe 发票跳转地址
3. `invoice.paid` 与 `customer.subscription.updated` 后续同步付款和订阅状态

**前置条件**：
- 活跃的付费订阅（非 Trial）
- 有效的支付方式

### 5.3 减少/取消存储

**流程**：
1. 如果 `target < current` → 通过 SubscriptionSchedule 在计费周期结束时安排
2. `target_storage_bytes` 立即在数据库中更新
3. 在计费周期结束时 → 数量变更生效

**特殊情况 — 降级到 Trial**：
降级到 Trial 时，存储会作为同一调度调用的一部分**自动取消**（原子操作）。

### 5.4 存储生命周期

```
添加（立即，按比例收费）
  ↓
活跃（与套餐一起在续费时计费）
  ↓
减少/取消（在计费周期结束时安排）
  ↓
生效（在计费周期结束时数量 = 0）
```

---

## 6. 积分系统

### 6.1 概述

积分是**独立于套餐配额的消耗型货币**。分为两种类型：

| 类型 | 来源 | 过期 |
|------|------|------|
| 套餐积分 | 包含在套餐配额中 | 每个计费周期重置 |
| 附加积分 | 通过充值购买 | 不过期 |

### 6.2 积分充值

**端点**：`POST /billing/points/checkout`

**流程**：
1. 创建 Stripe Checkout 会话（`mode=payment`，非订阅）
2. 用户付款成功 → 触发 `checkout.session.completed`（`mode=payment`）
3. 积分记入租户的 `PointAccount`
4. 创建账本条目，`event_type=recharge`

**定价**：在 `service_conf.yaml` 中配置：
```yaml
points_recharge:
  price_id: "price_xxx"
  points_per_unit: 100  # 1 unit = 100 points
```

### 6.3 积分幂等性

Webhook 处理器使用 `BillingWebhookEventService` 跟踪已处理的事件。重放同一事件是**幂等的**——不会重复记入积分。

### 6.4 积分消耗

- 优先消耗套餐积分（在配额内）
- 套餐配额用完后消耗附加积分
- DeepDoc 页面解析按 `page_count × consuming_point_amount` 消耗积分
- 当前配置下，`consuming_point_amount = 100`，即 1 个 DeepDoc PDF 页面消耗 100 points

---

## 7. 付款失败与恢复

### 7.1 失败流程

1. 续费发票失败 → 触发 `invoice.payment_failed` Webhook
2. 订阅状态 → `past_due`
3. `GET /billing/plan_overview` 中 `payment_required=true`
4. 前端显示注意横幅，附带发票 URL

### 7.2 恢复

1. 用户通过客户门户更新支付方式
2. Stripe 重新尝试付款 → 触发 `invoice.paid` Webhook
3. 订阅状态 → `active`
4. `payment_required=false`

### 7.3 欠费订阅处理

当订阅变为欠费时：
- 主订阅被视为非 entitled 状态，不再属于 `active` / `trialing`
- 如果用户仍要变更到付费套餐，接口会返回当前未支付发票 URL，要求先完成补款
- 如果用户改为降级到 Trial，系统会立即取消当前欠费订阅，并同步取消存储附加目标额度

---

## 8. API 端点汇总

| 方法 | 路径 | 用途 |
|------|------|------|
| GET | `/billing/status` | 检查计费是否启用 |
| GET | `/billing/current_plan` | 当前套餐 + 待处理变更 |
| GET | `/billing/plan_overview` | 完整概览，含资源使用量 |
| GET | `/billing/addon_overview` | 附加资源概览 |
| POST | `/billing/checkout` | 发起套餐变更 |
| GET | `/billing/plans` | 列出可用套餐及配额 |
| GET | `/billing/addon_plans` | 列出可购买的附加项 |
| POST | `/billing/upcoming` | 预览升级发票 |
| GET | `/billing/storage/current` | 当前存储状态 |
| POST | `/billing/storage/set-target` | 变更存储数量 |
| POST | `/billing/points/checkout` | 购买积分 |
| GET | `/billing/points/price` | 查看积分充值单价 |
| GET | `/billing/deepdoc/usage` | 查看 DeepDoc 解析用量 |
| GET | `/billing/points/balance` | 积分余额 |
| GET | `/billing/points/ledger` | 积分交易历史 |
| GET | `/billing/points/holds` | 积分预占记录 |
| GET | `/billing/spend_overview` | 计费历史 |
| GET | `/billing/spend_metrics` | 计费聚合指标 |
| POST | `/billing/webhook` | Stripe Webhook 处理器 |

---

## 9. 测试流程参考

| 测试 | 说明 |
|------|------|
| `billing_plan01` | 完整生命周期：Trial→Starter→Pro→Starter→Trial→Starter |
| `billing_plan02` | 续费失败 → 欠费 → 恢复 |
| `billing_plan03` | 带资源验证的套餐升级 |
| `billing_plan04` | 带资源冲突检测的降级 |
| `billing_plan05` | 直接 Stripe API 套餐变更 |
| `billing_storage01` | 带比例计算的存储附加购买 |
| `billing_storage02` | 存储生命周期 + 套餐降级自动取消 |
| `billing_point01` | 积分购买（100 积分） |
| `billing_point05` | 积分 Webhook 幂等性 |
| `billing_app01` | 带计费的 App 配额强制执行 |
| `billing_app02` | 因资源使用量被阻止的降级 |
| `billing_member01-05` | 成员配额强制执行流程 |
