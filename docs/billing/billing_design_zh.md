# RAGFlow Billing 设计文档

本文档描述当前代码中已经实现的 Billing 行为、接口、Webhook 收敛方式，以及自动化测试运行方式。

## 1. 范围

本文档覆盖以下当前实现：

- Stripe 计费模型与套餐规则
- 主套餐、存储附加、积分充值的后端行为
- Stripe Webhook 处理与本地状态收敛
- 已实现的 REST API
- Billing 自动化测试与本地运行前置

## 2. 系统概述

RAGFlow 使用 Stripe 作为支付提供商，采用单租户单主订阅模型。

每个租户的计费状态由以下对象组成：

- 一个主套餐订阅
- 零个或一个存储附加订阅项
- 一组积分账户与积分流水

积分充值不属于订阅项，而是独立的一次性支付。

### 2.1 核心组件

| 组件 | 说明 |
|------|------|
| `internal/handler/billing_ee.go` + `internal/router/router_ee.go` | Billing HTTP API、`/api/v1` 与 `/v1` 双前缀路由、Stripe webhook 入口 |
| `internal/service/billing/service_ee.go` | 服务单例、启动序列（注册 webhook、补投、限流同步、降级守卫）、套餐目录 seed |
| `internal/service/billing/checkout_ee.go` | Checkout、升级、降级排期、存储变更、SetupIntent、Portal、preview、会话与支付状态 |
| `internal/service/billing/webhook_ee.go` / `webhook_event_ee.go` | 事件分发、订阅收敛、webhook 注册与启动补投 |
| `internal/service/billing/points_ee.go` / `parse_billing_ee.go` | 积分 hold/commit/release、DeepDOC 按页计费 |
| `internal/service/billing/quota_ee.go` / `ratelimit_ee.go` / `downgrade_guard_ee.go` / `read_ee.go` / `payment_state_ee.go` / `stripe_client_ee.go` | 配额校验、限流键同步、降级守卫、只读聚合、支付状态归一、Stripe 客户端 |
| `internal/dao/billing_*_ee.go` + `internal/entity/enterprise_billing_*_ee.go` | 9 张表的 DAO 与实体 |
| `internal/ingestion/component/parser.go` | DeepDOC PDF 解析计费挂点 |
| `test/api/billing_routing_ee_test.go` | 路由/鉴权契约（`//go:build integration`） |
| `test/api/billing_*_flow_ee_test.go` | 真实 Stripe 业务流程用例（按关注点拆分） |
| `internal/service/billing/*_ee_test.go`、`internal/handler/*_ee_test.go`、`internal/dao/*_ee_test.go` | 服务/Handler/DAO 单测（无 build tag） |

### 2.2 当前实现约束

- subscription 变更通过 Stripe `Idempotency-Key` 与 `payment_behavior=pending_if_incomplete` 保证重放安全；本地不加进程内互斥锁，并发一致性依赖数据库事务与 Stripe 侧幂等
- 同一 subscription 已有 `SubscriptionSchedule` 时复用并原地改写 phase（`ensureSchedule`），不会重复创建 schedule
- webhook 事件按 15 分钟 lease 声明处理权，完成/失败写入必须匹配该 lease，避免过期持有者覆盖后继结果
- 本地数据库中的订阅状态是 Stripe 状态的收敛结果，不是权威来源
- 订阅类事件以 Stripe 权威对象为准，不直接信任 event snapshot
- 权益发放与套餐落库由后端处理，不由前端直接决定

## 3. 套餐与配额

### 3.1 套餐层级

```text
Trial -> Starter -> Pro
```

- `Trial` 为免费套餐
- `Starter`、`Pro` 为付费套餐

### 3.2 配额来源

每个套餐在 `service_conf.yaml` 中定义以下字段：

- `quota_apps`
- `quota_members`
- `quota_storage`
- `quota_points`
- `api_request_limit_per_minute`

Go 栈的配额来自 `service_conf.yaml` 的 `billing.billing_plans`。模板 `docker/service_conf.yaml.template` 用 `${BILLING_QUOTA_*}` 占位符给出默认值，本地 Docker 环境通过 `docker/.env-go` 覆盖，容器启动时生成最终的 `/ragflow/conf/service_conf.yaml`；直接在本机运行 `ragflow_server` 时读取 `-f` 指定的配置文件。

模板默认量级（apps / members / storage / points）：

- Trial：`5 / 1 / 100MB / 500`，RPM 10
- Starter：`50 / 5 / 5G / 5000`，RPM 100
- Pro：`100000 / 20 / 50G / 20000`，RPM 1000
- 存储附加：`1G`

修改配额后需重启 API 服务：启动时的 `Seed` 会以新的 product 版本写入 `billing_product`，只读接口按最新版本返回。

### 3.3 有效状态

主订阅常见状态包括：

- `active`
- `trialing`
- `past_due`
- `incomplete`
- `unpaid`
- `canceled`

当前实现中，`active` 和 `trialing` 视为可用的付费套餐状态。

## 4. 当前计费行为

### 4.1 Trial 升级到付费套餐

- 当租户当前没有可修改的 Stripe 主订阅时，后端创建 Stripe Checkout Session，`mode=subscription`
- 前端跳转到 `https://checkout.stripe.com/` 完成收款与订阅创建
- 升级为 `Starter` 或 `Pro` 时立即生效
- Stripe 会创建对应订阅并产生相关账单对象

### 4.2 付费套餐升级

- `Starter -> Pro` 立即生效
- 对已经存在的主订阅，后端继续使用 Stripe Subscription modify 流程，而不是重新创建一个 subscription checkout
- 当前周期内按 Stripe 的 proration 规则处理补差价
- Webhook 收到后，后端按 Stripe 权威订阅状态刷新本地套餐信息

### 4.3 付费套餐降级

- `Pro -> Starter` 在周期末生效
- 后端通过 Stripe `SubscriptionSchedule` 记录待生效的降级
- 当前周期结束前，租户继续保留原套餐权益
- 降级预约到生效之间可能存在配额超限风险，详见 [subscription_downgrade_quota_guard.md](subscription_downgrade_quota_guard.md)

### 4.4 回落到 Trial

- `Starter/Pro -> Trial` 在执行时**不做**资源兼容性检查，直接排期：给主订阅设置 `cancel_at_period_end`，并把本地 `TargetPlanName` 记为 `Trial`
- 接口立即返回 `scheduled=true` / `payment_state=scheduled`，当前周期内权益不变
- 周期末 Stripe 取消订阅 → `customer.subscription.deleted` 到达后本地回落 Trial，同时清空存储附加（`AddonSubscriptionItemID` / `AddonStorageBytes`）
- 若排期期间资源占用超过 Trial 配额，由降级守卫在扫描时撤销该排期（见 §5），而不是在发起时拒绝

### 4.5 删除订阅

- 当 Stripe 侧订阅被删除后，租户立即回落到 Trial
- 同步更新主套餐配额
- 同步收缩存储附加配额
- 同步刷新 points、storage 与其他配额相关派生状态

### 4.6 存储附加

存储附加与主套餐共享同一个 Stripe Subscription。

- 增加存储附加时立即生效，并按比例收费
- 减少存储附加时在周期末生效
- 回落到 Trial 时，存储附加自动取消

`billing_subscription` 里两列的分工与 Python 版一致：`addon_storage_bytes` 表示"当前生效"的附加量，由 `customer.subscription.*` webhook 收敛；`target_storage_bytes` 表示"排期中的目标量"，是 API 在schedule/release 之后写入的写穿缓存（读接口优先用 Stripe schedule，DB 作为 fallback，下行保护也读它）。

API 只做**列级更新**（`UpdateStorageAddon` / `UpdatePendingStorageTarget`），不整行 `Save`：本地行快照可能早于期间到达的 webhook，整行写回会把并发收敛的 `plan_name` / `status` / `last_event_at` 回退。增量成功后的 `addon_storage_bytes` 取自 `subscription.Update` 返回的订阅项数量，而不是请求里的目标值。

### 4.7 积分

当前实现区分两类积分：

- 套餐积分：按套餐周期刷新，不结转
- 充值积分：通过一次性支付获得，不过期

积分充值通过 Stripe Checkout 完成，到账以 webhook 处理结果为准。

### 4.8 SetupIntent / Checkout 的实际使用方式

三个写入口走不同路径，SetupIntent 只在"没有可用支付方式、无法直接扣款"时作为兜底：

| 场景 | 判定 | 后端路径 | SetupIntent 的作用 |
|---|---|---|---|
| 新建付费订阅（Trial 升级、或原订阅已 `canceled`/`incomplete_expired`/`paused`） | `billing_subscription.subscription_id` 为空或状态为终态 | `CreateSubscriptionCheckout`，返回 `redirect_to` + `session_id` | 可选：请求里带 `setup_intent_id` 时，先校验归属与 `succeeded`，把其 payment method 设为 customer 默认，再创建 session |
| 既有订阅的套餐升降 | 有 `subscription_id` 且状态非终态 | `ChangePlan`：`subscription.Update`（换价 / 周期末降级排期），`proration_behavior=always_invoice` + `payment_behavior=pending_if_incomplete` | **不消费** `setup_intent_id`；若本次 proration 无法扣款，返回 `payment_state=requires_action` 与 `redirect_to`（发票托管页） |
| 存储附加增减 | 同上 | `SetStorageTarget`；增量为立即生效，减少为周期末排期 | 先查可复用支付方式；缺失时返回 `requires_payment_method_setup=true` + setup-mode Checkout 的 `redirect_to`，客户端完成后带 `setup_intent_id` 重试 |

补充规则：

- `POST /billing/setup-intents` 只负责创建 SetupIntent 并返回 `client_secret` / `setup_intent_id`，不做扣款
- `applySetupIntentPaymentMethod` 会校验三件事：SetupIntent 属于该租户的 Stripe customer、状态为 `succeeded`、带 payment method；任一条不满足直接报错，不落库
- 可复用支付方式的判定同时看 subscription 的 `default_payment_method` 与 customer 的 `invoice_settings.default_payment_method`
- `POST /billing/subscription/preview` 返回 `has_reusable_payment_method`，前端据此决定是否先走 SetupIntent / setup-mode Checkout

为什么首购不要求先跑 SetupIntent：`mode=subscription` 的 Checkout 本身会收集支付方式、完成首单支付并创建订阅；此时还没有可被 modify 的订阅，额外一次 SetupIntent 只会增加失败面。

为什么修改既有订阅需要兜底：套餐与存储附加共享同一个 subscription，`subscription.Update` 会产生 proration 发票；没有可复用支付方式时无法直接扣款，只能补一次 setup-mode 流程或把用户引导到发票托管页。

#### 已知缺口

- `ChangePlan` 不接收 `setup_intent_id`：前端在升级时即便传了也被忽略，只能依赖 `requires_action` + 发票页兜底
- 后端不强制"修改既有订阅前必须存在可复用支付方式"，实际由 Stripe 的 `pending_if_incomplete` 与发票状态兜住
- `requires_payment_method_setup` 只在存储增量路径返回，套餐升级路径没有对应的结构化字段

## 5. 资源兼容性检查

当前实现是**事后由后台守卫处理**，不是发起时同步校验。

### 5.1 检查时点与范围

- 发起降级（付费套餐之间或回落到 Trial）时只写排期标记，不做占用校验，接口也不会返回资源冲突信息
- `StartDowngradeGuard` 在 API 进程内每 **10 分钟**扫描一次 `billing_subscription` 中带 `target_plan_name` / `target_storage_bytes` 的行
- 触发条件（任一超出即判定超限）：

  | 维度 | 判定 |
  |---|---|
  | apps | `apps_used > target_quota_apps` |
  | members | `members_used > target_quota_members` |
  | storage | `storage_used > target_quota_storage (+ 存储附加)` |

- 存储限额的计算方式：目标为 Trial 时**不加**存储附加；目标为付费套餐时加上 `TargetStorageBytes`（若存在）或当前 `AddonStorageBytes`
- 守卫可通过 `DOWNGRADE_GUARD_ENABLED=false` 关闭；单次扫描失败只记 warn，不中断后续扫描

### 5.2 超限后的处理

`CancelScheduledChange` 会：

1. 回落到 Trial 的排期：撤销 Stripe `cancel_at_period_end`
2. 付费套餐之间的降级：释放对应的 `SubscriptionSchedule`
3. 清空本地 `TargetPlanName`、`TargetStorageBytes`、`ScheduleID`

处理结果只写日志（`Billing scheduled downgrade cancelled: quota exceeded` + 超限维度），当前没有推送给前端的事件或通知。

### 5.3 已知缺口

- 用户发起降级时拿不到「当前占用会在周期末导致降级被撤销」的提示，只能在周期末发现 `pending_subscription_change` 消失
- 没有对外的手动取消排期接口（`CancelScheduledChange` 只在守卫内部调用）；前端「取消降级」依赖重新选择当前套餐触发
- 事务边界：占用统计与排期写入不在同一个事务内，扫描期间新增的应用/成员不会被计入本次判定

## 6. Webhook 处理

### 6.1 当前处理的主要事件

服务端注册（`focusedEvents`）并处理的 8 类事件：

- `checkout.session.completed`：积分充值按 `checkout:<session_id>` 幂等入账；订阅型 session 只做审计
- `invoice.paid`：订单落库、订阅状态收敛、按当前 `latest_invoice` 校验身份
- `invoice.payment_failed`、`invoice.payment_action_required`：订阅转欠费/待补费，刷新限流缓存
- `customer.subscription.created` / `updated`：读取 Stripe 权威订阅，按事件时间戳与周期顺序收敛本地套餐、存储附加、周期重置
- `customer.subscription.deleted`：回落 Trial
- `payment_intent.succeeded`：仅审计，不重复发放权益（避免与 checkout 重复充值）

### 6.2 收敛规则

- 本地订阅记录是 Stripe 状态的收敛缓存
- 订阅类事件发生时，后端优先读取 Stripe 权威 subscription 对象
- 不依赖单个 webhook event payload 中的快照字段作为最终状态依据

### 6.3 启动回补

服务启动（`billing.Start`）时依次执行：

- 校验或注册 Stripe webhook endpoint（注册失败直接 fail-fast）
- 后台补投：先 drain 本地可重试事件（按 `id > cursor` 游标分页，`failed` 或 lease 已过期的 `processing`），再按 checkpoint 回退 600 秒拉取 Stripe 事件，最旧优先，失败处停止并上抛错误
- 同步全部 API/登录 token 的限流键
- 启动降级守卫（默认每 10 分钟扫描排期降级并处理超限）

事件处理权通过 `billing_webhook_event` 的 `processing_started_at` lease 管理；`failed` 事件由下一次补投重试，`completed`/`unhandled` 不再重复处理。

### 6.4 本地 Stripe CLI 转发

本地开发和测试环境使用以下命令转发 webhook：

```bash
# CLI 的 default profile 若已过期，先用 test key 覆盖配置项
stripe config --set test_mode_api_key "$BILLING_STRIPE_API_KEY"

stripe listen --forward-to http://127.0.0.1:9380/api/v1/billing/webhooks/stripe
```

URL 必须带 `http://` 前缀。当 `billing.webhook_url` 指向 `localhost` 或 `127.0.0.1` 时，后端会识别为本地转发地址，并跳过 Stripe webhook signature verification（因此无需把 CLI 打印的 `whsec_...` 写进配置）。

## 7. 付款失败与恢复

### 7.1 支付失败

- `invoice.payment_failed` 会将主订阅收敛到欠费相关状态
- Billing 状态接口会带出 `payment_required`，前端可据此展示补费提示

### 7.2 支付恢复

- 用户补充或更新支付方式后，Stripe 重试成功
- `invoice.paid` 或 `customer.subscription.updated` 到达后，本地状态收敛回有效状态

### 7.3 订阅取消

- 订阅删除后，租户立即回落到 Trial
- 同时同步收缩套餐、存储和积分相关状态

## 8. REST API

所有 Billing 路由挂在 `/api/v1/billing/...` 下（下文省略前缀）。匿名路由只做鉴权闸门，其余路由需要登录态。

### 8.1 匿名路由

| Route | Method | Handler | 说明 |
|---|---|---|---|
| `/billing/status` | GET | `Status` | 裸 JSON `{"billing_enabled":bool}`，web 启动时读取 |
| `/billing/plans` | GET | `Plans` | 套餐目录；未启用时返回 `code=102` |
| `/billing/downgrade-guard/health` | GET | `Health` | 降级守卫健康信息 |
| `/billing/webhooks/stripe` | POST | `Webhook` | Stripe 事件入口，回环地址跳过签名校验 |

### 8.2 Subscription

| Route | Method | Handler |
|---|---|---|
| `/billing/subscription` | GET | `CurrentSubscription` |
| `/billing/subscription` | POST/PATCH | `CheckoutSubscription` |
| `/billing/subscription/preview` | POST | `PreviewSubscription` |
| `/billing/subscription/overview` | GET | `SubscriptionOverview` |
| `/billing/subscriptions/:subscription-id/payment-status` | GET | `SubscriptionPaymentStatus` |

### 8.3 Storage / Add-ons

| Route | Method | Handler |
|---|---|---|
| `/billing/storage` | GET | `Storage` |
| `/billing/storage` | PATCH | `SetStorageTarget` |
| `/billing/addons` | GET | `Addons` |
| `/billing/addons/overview` | GET | `AddonOverview` |
| `/billing/addon-purchases` | POST | `AddonCheckout` |

### 8.4 Setup / Portal / Checkout

| Route | Method | Handler |
|---|---|---|
| `/billing/setup-intents` | POST | `SetupIntent` |
| `/billing/portal-sessions` | POST | `PortalSession` |
| `/billing/checkouts/:session-id` | GET | `CheckoutSession` |

### 8.5 Points

| Route | Method | Handler |
|---|---|---|
| `/billing/points/checkout` | POST | `PointsCheckout` |
| `/billing/points/price` | GET | `PointsPrice` |
| `/billing/points/balance` | GET | `PointsBalance` |
| `/billing/points/overview` | GET | `PointsBalance` |
| `/billing/points/ledger` | GET | `PointsLedger` |
| `/billing/points/holds` | GET | `PointsHolds` |

### 8.6 Spend / Usage

| Route | Method | Handler |
|---|---|---|
| `/billing/spend-overview` | GET | `SpendOverview` |
| `/billing/spend-metrics` | GET | `SpendMetrics` |
| `/billing/usages/deepdoc` | GET | `DeepdocUsage` |

`spend-overview` 是 Billing History 表格的数据源，返回 `{"total": n, "items": [...]}` 分页信封（支持 `page` / `page_size`（1–100）/ `start` / `end`；时间参数接受 Unix 秒或 ISO 日期）。每行字段与前端 `Invoice` 类型一致：`invoice_id`、`amount`（美元浮点）、`currency`（大写）、`status`（`paid` / `unpaid` / `pending`）、`created_at`（毫秒时间戳）、`hosted_invoice_url`、`invoice_pdf_url`、`product_ids`、`product`、`product_quantities`、`product_amount_cents`。`product*` 三列由 `invoice.paid` 的 line items 聚合：价格 ID 经 `PriceIDToPlan` 映射为套餐名，未映射的价格记为 `UNKNOWN`，因此历史遗留价格（已下线）仍会显示 `UNKNOWN`。

## 9. 当前支付交互结果

当前后端接口会根据不同支付类型返回对应的 checkout、portal、preview、status 或 payment 相关信息。

- 新建付费主订阅：返回 `redirect_to`（Stripe Checkout Session URL）与 `session_id`，前端直接跳转
- 已有主订阅的套餐升降：同步执行 `subscription.Update`，返回 `payment_state`、`invoice_id`、`invoice_url`、`amount_cents`、`currency`；`payment_state=requires_action` 时额外返回 `redirect_to` 指向发票托管页
- 存储附加增加：若已有可复用支付方式，同步执行并返回 `payment_state`；若没有，返回 `requires_payment_method_setup=true` 与 setup-mode Checkout 的 `redirect_to`，客户端完成后带 `setup_intent_id` 重试
- 存储附加减少：返回 `scheduled=true` 与目标值，周期末生效
- 积分充值：返回一次性支付 Checkout Session URL

`setup_intent_id` 在代码里只有两处被消费：新建订阅的 `CreateSubscriptionCheckout` 与存储增减的 `SetStorageTarget`。套餐升降的 `ChangePlan` 不接收该参数，缺卡时只能走发票托管页兜底。

preview 返回 `amount_due_today`、`currency`、`has_reusable_payment_method`、`stripe_publishable_key`，前端据此决定是否需要先做卡绑定。

对套餐、存储和积分购买，最终权益发放都以后端 webhook 处理结果为准，而不是以前端跳转是否完成为准。

## 10. Billing 自动化测试

### 10.1 测试入口

Billing 后端测试全部是 Go：

| 层级 | 位置 | 运行方式 |
|---|---|---|
| 路由/鉴权契约 | `test/api/billing_routing_ee_test.go`（`//go:build integration`） | `bash build.sh --test-integration-go ./test/api/ -run '^TestBilling'` |
| 真实 Stripe 流程 | `test/api/billing_{plan,storage,points,trial,member,app_quota,file_upload}_flow_ee_test.go`（`BILLING_STRIPE_E2E=1`，上传类另需 `BILLING_STORAGE_E2E=1`；PLAN-02 需 `RAGFLOW_TEST_MYSQL_DSN`） | `bash build.sh --test-integration-go ./test/api/ -run '^Test(Plan\|Storage\|Point\|Trial\|Member\|App\|FileUpload)'` |
| 服务单测 | `internal/service/billing/*_ee_test.go` | `bash build.sh --test ./internal/service/billing/` |
| Handler 单测 | `internal/handler/billing_ee_test.go`、`billing_webhook_ee_test.go`（sqlite + gin） | `bash build.sh --test ./internal/handler/` |
| DAO 单测 | `internal/dao/*_ee_test.go` | `bash build.sh --test ./internal/dao/` |

单元测试使用 per-test 的临时 sqlite 文件（`file:<tmp>/billing.db`），不依赖外部服务；并发用例会收集所有 goroutine 的返回错误，并对 sqlite 的 `database is locked` 做有限重试。

### 10.2 集成用例范围与运行前置

`test/api` 的 4 个用例覆盖：`/billing/status` 裸 JSON 契约、`/billing/plans` 闸门、匿名访问鉴权路由返回 401、`/v1` 与 `/api/v1` 双前缀一致。

前置条件：

- 已启动 Go API 服务（默认 `http://127.0.0.1:9384`，可用 `-p` 或 `go.http_port` 改）
- `BILLING_ENABLED=1` 且 `service_conf.yaml` 含 `billing` 段
- 环境变量：`HOST_ADDRESS`、`ZHIPU_AI_API_KEY`、`SILICONFLOW_API_KEY`（`TestConfig` 强校验），可覆盖 `RAGFLOW_TEST_EMAIL` / `RAGFLOW_TEST_PASSWORD`
- `TestMain` 会先用上述邮箱注册（已存在则忽略）再登录，因此需要一个可写的数据库和一个健康的 Admin 服务

`TestConfig` 只用于登录与通用断言，上面的 4 个用例本身不依赖登录态。

### 10.3 真实 Stripe 流程测试

`test/api/billing_*_flow_ee_test.go` 直接调用 stripe.com（test 模式），不使用任何 mock；共享夹具在 `billing_harness_ee_test.go`：

1. 通过 API 确保租户存在 Stripe customer，并挂一张 test 卡（`tok_visa`）作为默认支付方式
2. 用 Stripe API 直接建/改订阅到 Pro（绕过 Checkout 的卡号输入），等 webhook 把本地套餐收敛到 Pro
3. `Pro → Starter`：断言 `scheduled=true`、`target_plan_name=Starter`，且 Stripe 上存在 active schedule
4. `Pro → Trial`（**回归用例**）：在 schedule 仍生效时回落 Trial，断言接口返回 0 且 `scheduled=true`、Stripe `cancel_at_period_end=true`、原 schedule 不再 active
5. 删除订阅：断言 webhook 收敛回 Trial

`TestPlan01CancelLifecycleWithClock` 进一步用 Stripe test clock 覆盖取消套餐的三个观测点：取消后（仍为原套餐 + `cancel_at_period_end=true` + 待生效 target=Trial）、时钟 +1 天后（状态不变）、以及推进到周期结束 +1 小时后（Stripe 订阅 `canceled`、本地回落 Trial、`pending_subscription_change` 与存储附加清空）。三个阶段同时核对收费：Stripe 已付发票数与金额、`billing_payment_order` 的条数与金额都必须保持首次扣费（$29.00 / 2900 分）不变，且不允许出现待支付的 open 发票，验证「周期末取消不再续费」。

`TestStorage01TimelineWithClock`、`TestStorage03TimelineWithClock` 与 `TestStorage04DowngradeTimelineWithClock` 共用 `clockFixture`（customer 绑定 test clock），在时间轴上同时核对**数量与金额**：

- STORAGE-01：购买 1 GB（立即）→ 续费后。购买时接口 `amount_cents` 必须与 Stripe 已付总额同步增长，附加项数量=1、发票净计费 1 GB；续费后数量仍为 1，续费发票计费 1 GB 且确已扣款。
- STORAGE-03：购买 1 GB → 升至 2 GB → 续费。升级发票的净数量必须是 1 GB 差额（proration 发票含「旧数量抵扣行 + 新数量收费行」，按金额符号相抵后才是实际增量），接口返回的 proration 金额与已付总额同步；续费发票计费 2 GB。
- STORAGE-04：排期降容后（立即、1 天后）数量与金额都不变；到期 +1 小时后附加项移除，续费发票不含 storage 行、只扣套餐费。

测试时钟有三个实现约定：检查点一律从冻结时刻 `base` 推导（`f.base.Add(24*time.Hour)`）而不是重新取 `time.Now()`，避免用例变慢时越过周期边界；续费前重新读取订阅的 `CurrentPeriodEnd`（附加项可能移动周期边界）；`deleteTestClock` 等 clock 离开 `advancing` 后再删除，删除失败要记录而不能静默泄漏。

前置：`BILLING_STRIPE_E2E=1`、运行中的 API 服务（billing 已启用）、`stripe listen --forward-to http://127.0.0.1:<port>/api/v1/billing/webhooks/stripe`；test clock 用例还需 `RAGFLOW_TEST_MYSQL_DSN` 把租户绑定到 clock customer。
未设置 `BILLING_STRIPE_E2E` 时上述用例 skip，不会影响 CI 的只读冒烟。

### 10.4 本地手工验证链路

Stripe 相关的升级、降级、存储变更目前靠手工链路验证（真实 test-mode 账户）：

```bash
# 1. 后端（示例：9380）
BILLING_ENABLED=1 ./bin/ragflow_server --api -p 9380 -f conf/service_conf.yaml

# 2. 前端
cd web && npm run dev -- --port 9222 --host 127.0.0.1

# 3. Stripe CLI 转发（见 6.4）
stripe listen --forward-to http://127.0.0.1:9380/api/v1/billing/webhooks/stripe
```

验证点建议覆盖：preview 金额与 Stripe invoice line 一致、升级后 `payment_state=paid`、`billing_payment_order` 落单、`billing_subscription` 的 `plan_name/price_id/invoice_id` 收敛、`payment-status` 的 `entitlement_ready=true`。

### 10.5 覆盖范围与运行前置

从 Python 删除的 7 个流程测试文件（`test_plan_flows`、`test_points_flows`、`test_storage_flows`、`test_member_flows`、`test_app_quota_flows`、`test_trial_flows`、`test_file_upload_billing`）共 28 个用例已全部移植为 Go，并按关注点拆分：

- `billing_routing_ee_test.go`：路由契约与鉴权
- `billing_plan_flow_ee_test.go` / `billing_trial_flow_ee_test.go`：套餐变更、取消、回落 Trial
- `billing_storage_flow_ee_test.go` / `billing_points_flow_ee_test.go`：附加存储与积分充值的数量与收费
- `billing_member_flow_ee_test.go` / `billing_app_quota_flow_ee_test.go` / `billing_file_upload_flow_ee_test.go`：席位、应用与文件配额

真实 Stripe 用例由 `BILLING_STRIPE_E2E=1` 统一开关；依赖 Stripe test clock 的用例（`*WithClock`）额外要求 `RAGFLOW_TEST_MYSQL_DSN`，用于把租户的 billable 记录绑定到 clock customer，未设置时 skip 并给出原因。CI 默认只跑只读冒烟，不触达 Stripe。

## 11. CI 集成

### 11.1 工作流位置

- `.github/workflows/tests.yml`

### 11.2 执行步骤

Billing 任务只在 `HTTP_API_TEST_LEVEL=p3` 时执行：

1. 构建 Go 镜像：`docker build -f Dockerfile_go -t infiniflow-ai/ragflow-go:<run>-<job> .`
2. `sed -i 's/^BILLING_ENABLED=.*/BILLING_ENABLED=1/' docker/.env-go`
3. 先 `docker compose -f docker/docker-compose.yml down -v`，再 `docker compose -f docker/docker-compose-go.yml up -d --force-recreate ragflow-cpu`
4. 轮询 `/api/v1/system/ping`（最多 15 分钟）
5. 断言 `/api/v1/billing/status` 返回 `"billing_enabled":true`，否则直接失败
6. 执行 `bash build.sh --test-integration-go ./test/api/ -run '^TestBilling'`，日志输出到 `es_http_api_billing_test.log`

### 11.3 CI 前置

- 仓库 secrets：`ZHIPU_AI_API_KEY`、`SILICONFLOW_API_KEY`（缺失时任务显式报错退出，不会静默跳过）
- Go 镜像内已安装 OpenResty，`docker/nginx/rate_limit.lua` 随镜像发布
- 集群内 `docker/.env-go` 的 `BILLING_STRIPE_*`、`BILLING_QUOTA_*` 使用模板默认值；如需小配额回归，改动 `.env-go` 后必须重建容器

## 12. 术语

| 术语 | 说明 |
|------|------|
| `invoice_url` | Stripe 托管发票页面 |
| `receipt_url` | Stripe 支付完成后的收据页面 |
| `setup_intent_id` | 前端用 `POST /billing/setup-intents` 拿到并在确认后回传；仅新建订阅与存储变更会消费，用于把支付方式设为 customer 默认 |
| `subscription_schedule` | Stripe 周期末变更调度对象 |
| `proration` | 周期中变更的按比例计费 |
| `pending_subscription_change` | 已安排但未生效的套餐变更 |
| `payment_required` | 欠费恢复提示标志 |
