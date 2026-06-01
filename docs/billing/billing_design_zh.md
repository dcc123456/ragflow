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
| `api/apps/billing_app.py` | Billing HTTP API 与 Stripe webhook 入口 |
| `api/services/billing_webhook_service.py` | Webhook 事件处理与 Stripe 状态收敛 |
| `api/db/services/billing_service.py` | 订阅、产品、订单、积分等数据库服务 |
| `test/testcases/libs/billing/` | Billing pytest 公共 helper |
| `test/testcases/test_http_api/test_billing/` | Billing HTTP API 自动化测试 |

### 2.2 当前实现约束

- 不在应用层为 subscription 变更增加额外幂等锁
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

本地 Docker 环境可通过 `docker/.env` 中的 `BILLING_QUOTA_*` 环境变量覆盖模板值，容器启动时会生成最终的 `/ragflow/conf/service_conf.yaml`。

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

- 通过 Billing API 创建订阅或创建 checkout 流程
- 升级为 `Starter` 或 `Pro` 时立即生效
- Stripe 会创建对应订阅并产生相关账单对象

### 4.2 付费套餐升级

- `Starter -> Pro` 立即生效
- 当前周期内按 Stripe 的 proration 规则处理补差价
- Webhook 收到后，后端按 Stripe 权威订阅状态刷新本地套餐信息

### 4.3 付费套餐降级

- `Pro -> Starter` 在周期末生效
- 后端通过 Stripe `SubscriptionSchedule` 记录待生效的降级
- 当前周期结束前，租户继续保留原套餐权益
- 降级预约到生效之间可能存在配额超限风险，详见 [subscription_downgrade_quota_guard.md](subscription_downgrade_quota_guard.md)

### 4.4 回落到 Trial

- `Starter/Pro -> Trial` 前会做资源兼容性检查
- 若应用数、成员数、存储占用超过 Trial 配额，则拒绝降级
- 若兼容，则在周期末安排回落到 Trial
- 降级到 Trial 时，存储附加一并取消

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

### 4.7 积分

当前实现区分两类积分：

- 套餐积分：按套餐周期刷新，不结转
- 充值积分：通过一次性支付获得，不过期

积分充值通过 Stripe Checkout 完成，到账以 webhook 处理结果为准。

## 5. 资源兼容性检查

降级前会检查以下条件：

- `apps_used <= target_quota_apps`
- `members_used <= target_quota_members`
- `storage_used <= target_quota_storage`

不兼容时，接口返回资源冲突信息，不会静默忽略。

## 6. Webhook 处理

### 6.1 当前处理的主要事件

- `checkout.session.completed`
- `invoice.paid`
- `invoice.payment_failed`
- `customer.subscription.updated`
- `customer.subscription.deleted`
- `payment_intent.succeeded`

### 6.2 收敛规则

- 本地订阅记录是 Stripe 状态的收敛缓存
- 订阅类事件发生时，后端优先读取 Stripe 权威 subscription 对象
- 不依赖单个 webhook event payload 中的快照字段作为最终状态依据

### 6.3 启动回补

服务启动时会执行以下动作：

- 初始化数据库表
- 校验或注册 Stripe webhook endpoint
- 处理未成功投递或未完成处理的 webhook 事件

### 6.4 本地 Stripe CLI 转发

本地开发和测试环境使用以下命令转发 webhook：

```bash
stripe listen --forward-to 127.0.0.1:9380/v1/billing/webhooks/stripe
```

当 `billing.webhook_url` 指向 `localhost` 或 `127.0.0.1` 时，后端会识别为本地转发地址，并跳过 Stripe webhook signature verification。

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

以下为当前代码中已实现的主要 REST 路由。

### 8.1 Subscription

| Route | Method | Handler |
|---|---|---|
| `/billing/subscription` | GET | `billing_current_plan` |
| `/billing/subscription` | POST | `billing_checkout` |
| `/billing/subscription` | PATCH | `billing_checkout` |
| `/billing/subscription/preview` | POST | `billing_upcoming` |
| `/billing/subscription/overview` | GET | `billing_plan_overview` |

### 8.2 Storage

| Route | Method | Handler |
|---|---|---|
| `/billing/storage` | GET | `billing_storage_current` |
| `/billing/storage` | PATCH | `billing_storage_set_target` |

### 8.3 Add-ons

| Route | Method | Handler |
|---|---|---|
| `/billing/addons` | GET | `billing_all_addon_plans` |
| `/billing/addon-purchases` | POST | `billing_checkout` |
| `/billing/addons/overview` | GET | `billing_addon_overview` |

### 8.4 Setup / Portal / Checkout Status

| Route | Method | Handler |
|---|---|---|
| `/billing/setup-intents` | POST | `billing_create_setup_intent` |
| `/billing/portal-sessions` | POST | `customer_portal` |
| `/billing/checkouts/<session_id>` | GET | `billing_session_status` |
| `/billing/webhooks/stripe` | POST | `billing_webhook` |

### 8.5 Points

| Route | Method | Handler |
|---|---|---|
| `/billing/points/checkout` | POST | `billing_points_checkout` |
| `/billing/points/price` | GET | `billing_points_price` |
| `/billing/points/balance` | GET | `billing_points_balance` |
| `/billing/points/overview` | GET | `billing_points_balance` |
| `/billing/points/ledger` | GET | `billing_points_ledger` |
| `/billing/points/holds` | GET | `billing_points_holds` |

### 8.6 Overview / Metrics / Status

| Route | Method | Handler |
|---|---|---|
| `/billing/spend/overview` | GET | `billing_spend_overview` |
| `/billing/spend_metrics` | GET | `billing_spend_metrics` |
| `/billing/usages/deepdoc` | GET | `billing_deepdoc_usage` |
| `/billing/status` | GET | `billing_status` |
| `/billing/downgrade-guard/health` | GET | `downgrade_guard_health` |

## 9. 当前支付交互结果

当前后端接口会根据不同支付类型返回对应的 checkout、portal、preview、status 或 payment 相关信息。前端是否先调用 preview、是否先创建 setup intent，取决于当前页面交互流程与支付方式状态，不属于后端状态机本身的保证。

对套餐、存储和积分购买，最终权益发放都以后端 webhook 处理结果为准，而不是以前端跳转是否完成为准。

## 10. Billing 自动化测试

### 10.1 测试入口

Billing 自动化测试入口位于：

- `test/testcases/test_http_api/test_billing/`

公共 helper 位于：

- `test/testcases/libs/billing/`

### 10.2 用例文件

- `test_plan_flows.py`
- `test_points_flows.py`
- `test_storage_flows.py`
- `test_member_flows.py`
- `test_app_quota_flows.py`
- `conftest.py`

### 10.3 主要 fixture

- `billing_runtime_config`
- `billing_test_args`
- `billing_email_factory`
- `billing_client`
- `points_client`
- `app_client`

### 10.4 配置读取顺序

测试优先读取运行中 ragflow 容器内的配置文件：

1. `/ragflow/conf/service_conf.yaml`
2. 项目根目录 `conf/service_conf.yaml`

测试会从配置中读取：

- `billing.stripe_api_key`
- `billing.stripe_api_version`
- `billing.points_recharge`
- `billing.billing_plans`
- `ragflow.http_port`

### 10.5 本地运行前置

本地手动运行 Billing 自动化测试时，`docker/.env` 需要包含以下环境变量：

```dotenv
# billing
BILLING_ENABLED=1
BILLING_QUOTA_TRIAL_APPS=1
BILLING_QUOTA_TRIAL_MEMBERS=1
BILLING_QUOTA_TRIAL_STORAGE=100KB
BILLING_QUOTA_TRIAL_POINTS=10
BILLING_QUOTA_STARTER_APPS=3
BILLING_QUOTA_STARTER_MEMBERS=3
BILLING_QUOTA_STARTER_STORAGE=300KB
BILLING_QUOTA_STARTER_POINTS=30
BILLING_QUOTA_PRO_APPS=5
BILLING_QUOTA_PRO_MEMBERS=5
BILLING_QUOTA_PRO_STORAGE=500KB
BILLING_QUOTA_PRO_POINTS=50
BILLING_QUOTA_STORAGE_ADDON_STORAGE=1GB
```

完成环境变量设置后，在仓库根目录执行以下命令：

1. 构建并启动 Docker 栈：

```bash
docker compose -f docker/docker-compose.yml up -d --build
```

2. 安装测试依赖并激活虚拟环境：

```bash
uv sync --python 3.13 --all-extras --group test
source .venv/bin/activate
```

3. 启动 Stripe CLI webhook 转发：

```bash
stripe listen --forward-to 127.0.0.1:9380/v1/billing/webhooks/stripe
```

4. 执行 Billing 测试：

```bash
pytest -s --tb=short test/testcases/test_http_api/test_billing
```

如果修改了 `docker/.env` 中的 Billing 配额相关变量，需要重建 `ragflow` 与 `parser` 相关容器，使容器内的 `/ragflow/conf/service_conf.yaml` 按新环境变量重新生成。

### 10.6 测试方法

当前 Billing 自动化测试以黑盒方式驱动：

- 通过真实 RAGFlow API 创建或变更计费状态
- 通过 Stripe CLI 转发真实 webhook
- 通过 Billing API 与 Stripe 权威对象共同校验结果

测试中会使用以下辅助动作：

- 绑定 Stripe 测试卡
- 绑定拒付测试卡
- 移除 payment method
- 推进 Stripe test clock
- 查询 Stripe subscription、invoice、checkout 状态做断言

测试不直接通过白盒方式伪造本地 Billing 最终状态。

### 10.7 FlowError 分层

当前测试代码中：

- `FlowError` 保留在 helper 层
- pytest 测试层通过 `pytest.fail(...)` 或显式 `assert` 呈现失败
- 测试层辅助断言位于 `test/testcases/test_http_api/test_billing/assertions.py`

## 11. CI 集成

### 11.1 工作流位置

- `.github/workflows/tests.yml`

### 11.2 执行方式

Billing 用例由 pytest 统一执行，并通过 `@pytest.mark.p3` 参与测试分级收集。

当前执行命令为：

```bash
pytest -s --tb=short --level=${HTTP_API_TEST_LEVEL} test/testcases/test_http_api
```

### 11.3 CI 运行前置

CI 环境需要满足以下条件：

- `BILLING_ENABLED=1`
- 提供 Stripe test secret key
- 已安装 Stripe CLI
- 已启动 `stripe listen --forward-to 127.0.0.1:9380/v1/billing/webhooks/stripe`
- Billing quota 环境变量已设置为测试所需值

### 11.4 小配额运行方式

当前 Billing 测试按运行时实际 quota 驱动，不把固定配额值写死在测试断言中。

CI 和本地测试环境都可以使用较小的 quota 值，以便稳定覆盖配额边界：

- Trial: `apps=3`, `members=1`, `storage=100KB`, `points=10`
- Starter: `apps=4`, `members=3`, `storage=300KB`, `points=30`
- Pro: `apps=5`, `members=5`, `storage=500KB`, `points=50`

## 12. 术语

| 术语 | 说明 |
|------|------|
| `invoice_url` | Stripe 托管发票页面 |
| `receipt_url` | Stripe 支付完成后的收据页面 |
| `setup_intent_id` | 用于收集可复用支付方式的 Stripe SetupIntent |
| `subscription_schedule` | Stripe 周期末变更调度对象 |
| `proration` | 周期中变更的按比例计费 |
| `pending_subscription_change` | 已安排但未生效的套餐变更 |
| `payment_required` | 欠费恢复提示标志 |
