# RAGFlow 计费系统文档

计费系统业务规则参考 [billing_spec_zh.md](./billing_spec_zh.md)。本文档记录系统实现、接口与测试流程说明。

## 1. 系统概述

RAGFlow 使用 Stripe 作为支付提供商，采用**单租户单订阅**模型。每个租户只有一个订阅，该订阅可以包含多个产品（套餐 + 存储附加）。所有计费操作都通过 Stripe Checkout 或直接 Stripe API 调用完成，Webhook 处理器将状态同步回本地数据库。

### 设计约束：不做 subscription 操作幂等控制

- 当前实现**不在应用层对 subscription 操作做额外幂等或互斥控制**。
- 原因不是“重复提交绝对无副作用”，而是系统权衡后认为这层控制的收益不足以覆盖其复杂度。
- 对于套餐升级、套餐降级、存储增加等通过 `stripe.Subscription.modify` 或 Stripe schedule 提交的操作，Stripe 负责维护订阅对象的一致状态；最终本地状态以后端 webhook 收敛为准。
- 在这个前提下，短时间重复请求或并发请求更可能带来的是：
  - 多次返回不同的发票 URL 或待支付状态
  - 多个 proration 相关中间状态
  - webhook 到达顺序更复杂
- 但系统判断这些影响主要体现在过程复杂度与用户体验上，而不是“Stripe 最终金额被算错”。
- 因此当前设计选择：
  - 不新增 `pending_operation_type` 一类的本地互斥字段
  - 不在 `checkout` / `storage/set-target` 请求入口做 subscription 级幂等锁
  - 继续依赖 Stripe 订阅对象语义与 webhook 最终收敛
- 积分充值等一次性支付仍然遵循它们各自的订单 / webhook 幂等处理，不受本结论影响。

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

### Webhook 处理设计

- 系统启动时会确保 Stripe webhook 配置已注册，并从持久化配置读取 webhook id / secret 供验签使用。
- 系统启动时会调用 `handle_undelivered_events()` 回补最近未成功投递的 Stripe events，而不是假设在线 webhook 一定完整到达。
- 启动回补只处理计费子系统关注的事件类型，并按 `(created, event_id)` 排序后串行重放，尽量逼近 Stripe 真实时间顺序。
- 系统维护一个单调递增的 webhook checkpoint：
  - `created`
  - `event_id`
- 启动回补时，会从 checkpoint 向前留一个小的时间缓冲区重新拉取事件，并结合 `ending_before` 限制范围，兼顾“补漏”与“避免无限回放”。
- 每个 webhook event 都会先落到本地 `billing_webhook_event` 记录中，再进入业务处理；重复 event 依靠 event id 去重。
- 乱序是设计前提而不是异常情况，主要来自：
  - Stripe 重试
  - 网络延迟
  - 启动回补时重放历史事件
  - 同一 subscription 在短时间内产生多个状态快照
- 对 `customer.subscription.updated` 事件，系统不会简单按“后到者覆盖先到者”处理，而是先比较 subscription 所属计费周期：
  - 如果 event 的计费周期明显早于本地当前周期，则视为旧事件并跳过
  - 如果是同一 subscription、同一计费周期的多个 events，则进一步调用 Stripe API 读取当前 subscription 状态，而不是直接信任 event payload snapshot
- 这样设计的原因是：同一计费周期内的多个 `customer.subscription.updated` 往往只是不同时间点的快照，较旧 snapshot 可能晚到；直接套用 event body 容易把本地状态回滚到旧值。
- webhook 是本地订阅状态的最终收敛来源，但不是盲目覆盖来源；系统会结合本地当前周期、event 周期、Stripe 当前对象状态共同决定是否应用某个 event。

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

### 3.0 统一的付费订阅变更链路

对所有“立即生效且可能立即扣款”的套餐升级/存储增加场景，前后端遵循统一链路：

1. 前端调用 `POST /billing/upcoming`
2. 后端返回预览金额，以及 `has_reusable_payment_method`
3. 如果 `has_reusable_payment_method=true`，前端可直接调用真实变更接口
4. 如果 `has_reusable_payment_method=false`，前端先调用 `POST /billing/setup-intent` 并完成 `confirmSetup`
5. 前端再调用真实变更接口，并携带已成功的 `setup_intent_id`
6. 后端校验 `SetupIntent`、保存默认支付方式，然后执行唯一一次真实订阅修改
7. 最终订阅状态以后端 webhook 同步结果为准，而不是以前端乐观状态为准

套餐升级与存储增加必须共享上述契约，避免两条链路行为漂移。

### 3.1 Trial → Starter（升级）

**触发条件**：用户调用 `POST /billing/checkout`，传入 Starter 的 `price_id`。

**流程**：
1. 前端先调用 `POST /billing/upcoming`
2. 如无可复用支付方式，则前端调用 `POST /billing/setup-intent` 并完成 `confirmSetup`
3. 前端调用 `POST /billing/checkout`
4. 后端在必要时应用 `setup_intent_id` 对应的默认支付方式
5. 后端创建新的订阅或执行首次付费订阅切换
6. 订阅状态由 `checkout.session.completed` / `customer.subscription.updated` 同步到本地数据库
7. 计费周期立即开始（从现在开始新周期）

**前置条件**：
- 租户必须有 Stripe `customer_id`
- 需要有效的支付方式

**生效时间**：立即生效（开始新的计费周期）

### 3.2 Starter → Pro（升级）

**触发条件**：用户调用 `POST /billing/checkout`，传入 Pro 的 `price_id`。

**流程**：
1. 前端先调用 `POST /billing/upcoming`
2. 如无可复用支付方式，则前端调用 `POST /billing/setup-intent` 并完成 `confirmSetup`
3. 前端调用 `POST /billing/checkout`
4. 后端在必要时应用 `setup_intent_id` 对应的默认支付方式
5. 后端调用 `modify_subscription_plan_async()` 修改 Stripe 订阅
6. Stripe 立即收取按比例计算的差价（`proration_behavior: always_invoice`）
7. `invoice.paid` 与 `customer.subscription.updated` Webhook 同步付款和订阅状态
8. 数据库更新为 "Pro" 套餐

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

说明：这类“期末生效”的降级不需要 `setup-intent`，当前实现也不额外增加本地 subscription 互斥控制。

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

说明：这类“期末生效”的降级不需要 `setup-intent`，当前实现也不额外增加本地 subscription 互斥控制。

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
1. 前端先调用 `POST /billing/upcoming`
2. 如无可复用支付方式，则前端调用 `POST /billing/setup-intent` 并完成 `confirmSetup`
3. 前端调用 `POST /billing/storage/set-target`
4. 后端在必要时应用 `setup_intent_id` 对应的默认支付方式
5. 如果 `target > current` → 立即修改 Stripe 订阅，按比例收费
6. 系统立即同步新的目标存储额度，并按场景返回发票链接或待处理结果
7. `invoice.paid` 与 `customer.subscription.updated` 后续同步付款和订阅状态

**前置条件**：
- 活跃的付费订阅（非 Trial）
- 有效的支付方式

### 5.3 减少/取消存储

**流程**：
1. 如果 `target < current` → 通过 SubscriptionSchedule 在计费周期结束时安排
2. `target_storage_bytes` 立即在数据库中更新
3. 在计费周期结束时 → 数量变更生效

说明：存储减少/取消通常不需要 `setup-intent`，当前实现也不额外增加本地 subscription 互斥控制。

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
| POST | `/billing/setup-intent` | 为套餐/存储立即支付场景采集可复用支付方式 |
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

### 8.1 为什么不做 subscription 操作幂等控制

- 当前系统不新增本地 subscription 互斥锁，也不额外保存 subscription 操作 request id。
- 主要原因是：重复或并发的 `subscription.modify` / schedule 操作更可能造成过程复杂度，而不是 Stripe 最终金额错误。
- 对系统而言，更重要的是：
  - 前端先拿到准确预览金额
  - 后端只通过真实 Stripe 操作提交变更
  - 本地最终状态由 webhook 收敛
- 如果未来观察到真实生产问题主要来自“重复请求导致金额错误”而不是“中间状态更复杂”，再重新引入应用层幂等控制会更合适。

---

## 附录：关键术语表

### Stripe 术语

| 术语 | 说明 |
|------|------|
| `invoice_url` | Stripe Invoice 页面 URL。Stripe 生成的正式发票，可有 `open`/`paid`/`draft` 等状态。用户可用来付款或查看账单明细。可包含多个产品行（套餐 + 存储附加）。 |
| `receipt_url` | Payment Receipt URL。支付成功后自动生成的收据，仅在支付完成后才有。只能查看，不能用于付款。 |
| `payment_state` | 后端返回的支付状态语义：`paid`（已完成）、`requires_action`（需继续操作）、`pending`（处理中）、`scheduled`（已安排待生效）。用于前端判断展示弹窗还是跳转 Stripe 页面。 |
| `product_type` | 产品类型标识：`subscription`（套餐）、`storage`（存储附加）、`points`（积分充值）。驱动成功弹窗的标题和内容展示。 |
| `setup_intent_id` | Stripe SetupIntent ID。用于采集可复用的支付方式，区别于一次性 Checkout。套餐立即升级和存储立即增加场景需要先完成 SetupIntent 再执行真实变更。 |
| `subscription` | Stripe 订阅对象。单租户单订阅模型中，每个租户对应一个 Stripe Subscription，包含套餐行项目和可能的存储附加行项目。订阅状态决定了用户是否有权使用付费功能。 |
| `subscription_schedule` | Stripe SubscriptionSchedule。用于安排计费周期结束时生效的变更（如降级、存储减少），而不是立即执行的修改。 |
| `proration` | 按比例计算。套餐升级时立即收取差价，或降级时安排期末退还。Stripe 按 `proration_behavior: always_invoice` 处理。 |
| `webhook` | Stripe Webhook。后端通过 webhook 事件（`invoice.paid`、`customer.subscription.updated` 等）同步订阅和付款状态到本地数据库，webhook 是本地状态的最终权威来源。 |

### 业务术语

| 术语 | 说明 |
|------|------|
| `quota_points` | 套餐包含的积分配额。每个计费周期重置，不累计。 |
| `addon_points` | 通过充值购买的附加积分，不过期。优先在套餐积分用完后消耗。 |
| `pending_subscription_change` | 已安排但未生效的套餐变更。在当前计费周期结束前以 `SubscriptionSchedule` 形式存在，期末自动执行。 |
| `payment_required` | 欠费标志。当 `invoice.payment_failed` 触发后变为 `true`，表示需要用户更新支付方式才能恢复服务。 |

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
