# 改进支付体验计划

## 背景

当前 billing 支付完成后的体验不完全一致：

- 升级 plan 后，前端会在当前页面展示支付成功弹窗。
- 增购 storage addon 后，部分即时付款场景会新开 tab 打开 Stripe invoice。
- 购买 points 使用 Stripe Checkout，支付后回到页面并由成功弹窗展示结果。

这些流程都属于用户主动发起的付费行为，完成后应尽量使用一致的站内反馈，减少新 tab、重复跳转和状态不明确的问题。

## 目标

1. 统一 plan、storage addon、points 的支付成功反馈。
2. 支付成功后优先展示站内弹窗，而不是新开 tab。
3. 弹窗文案按购买内容区分，例如：
   - `Plan upgraded successfully`
   - `Storage added successfully`
   - `Points purchased successfully`
4. 弹窗展示关键支付信息：
   - 金额
   - 币种
   - 购买内容
   - invoice id / session id
   - points 数量或 storage 数量
5. 对 Stripe 托管页面保留外部跳转能力，但只用于需要用户继续完成支付、3DS、SCA 或查看官方发票页面的场景。

## 非目标

1. 不在 iframe 内嵌 Stripe Hosted Invoice Page。
2. 不改变 Stripe Checkout / Hosted Invoice Page 的支付确认职责。
3. 不把需要 SCA 或继续支付的流程强行留在站内弹窗里。
4. 不在本轮重构 billing 订单模型或 webhook 幂等模型。

## 设计原则

### 支付完成后

如果后端已经确认本次操作已完成，前端展示站内弹窗：

- plan upgrade：展示升级成功。
- storage addon：展示存储购买或变更成功。
- points：展示积分购买成功。

弹窗可提供 `View invoice`、`View receipt` 或 `Download PDF` 按钮，但这些按钮应由用户主动点击，不自动打开新 tab。

### 仍需支付动作时

如果后端返回 `redirect_to` 且该 URL 表示用户仍需在 Stripe 页面完成支付确认，例如：

- Stripe Checkout Session URL
- Hosted Invoice Page URL
- SCA / 3DS 需要继续操作的 invoice URL

前端应跳转到 Stripe 托管页面。返回站内后再展示统一成功弹窗。

### Stripe invoice 展示

Stripe Hosted Invoice Page 不作为 iframe 内容嵌入。原因：

- Stripe 官方定位是托管支付和发票页面。
- 部分支付方式、3DS、SCA 需要顶层跳转或 Stripe 控制的页面上下文。
- iframe 嵌入可能受安全 header、浏览器策略或支付方式限制影响。

推荐做法是在站内弹窗中展示摘要，并提供外链按钮打开 Stripe invoice 或 receipt。

## 当前流程评估

### Plan Upgrade

现状：

- 前端调用订阅变更接口。
- 后端通过 `stripe.Subscription.modify` 修改订阅。
- 如果支付已完成，前端通过 `BillingDirectCheckoutResultEvent` 触发成功弹窗。
- 如果仍需支付，前端跳转到 Stripe invoice URL。

建议：

- 保持当前总体方向。
- 成功弹窗文案从通用支付成功调整为 plan 相关文案。
- 弹窗增加 invoice / receipt 链接按钮。

### Storage Addon

现状：

- 前端调用 storage target 接口。
- 后端可能通过 `stripe.Subscription.modify` 立即开票。
- 当前部分成功结果收到 `redirect_to` 后会直接 `window.open(res.redirect_to)`。

问题：

- 与 plan upgrade 的成功体验不一致。
- 用户可能在新 tab 看到已支付 invoice，而主页面没有明确成功反馈。

建议：

- 如果后端返回结果表示支付已完成，前端触发统一成功弹窗。
- 如果后端返回结果表示还需要用户支付或认证，才跳转 Stripe 页面。
- 不再对成功 invoice 自动 `window.open`。

### Points

现状：

- points 使用 Stripe Checkout 一次性支付。
- 支付成功后通过 `session_id` 回到站内。
- 前端轮询 `/billing/checkouts/<session_id>` 并展示成功弹窗。

建议：

- 保留 Stripe Checkout 支付流程。
- 成功弹窗文案改为 points 专属文案。
- 展示 points 数量、支付金额、币种和 session / receipt 信息。
- 后端可扩展 session status 接口，返回 `payment_intent_id` 和 receipt URL。

## 后端改动计划

### 1. 明确支付结果语义

对 subscription modify 类接口返回值增加或统一字段：

```json
{
  "payment_state": "paid | requires_action | pending | scheduled",
  "redirect_to": "https://...",
  "invoice_id": "in_...",
  "invoice_url": "https://invoice.stripe.com/...",
  "invoice_pdf_url": "https://...",
  "amount_cents": 1000,
  "currency": "usd",
  "product_type": "subscription | storage | points",
  "product_name": "Pro",
  "quantity": 1
}
```

说明：

- `paid`：前端展示成功弹窗。
- `requires_action`：前端跳转 `redirect_to`。
- `pending`：前端展示处理中或轮询。
- `scheduled`：前端展示计划变更成功。

### 2. 扩展 Checkout Session 查询接口

`GET /billing/checkouts/<session_id>` 可补充返回：

- `session_id`
- `payment_intent_id`
- `invoice_id`
- `receipt_url`
- `amount_cents`
- `currency`
- `metadata.points_amount`

points 成功弹窗可直接使用这些字段。

### 3. 保留 Stripe URL 的外链职责

后端继续返回：

- `invoice_url`
- `invoice_pdf_url`
- `receipt_url`
- `redirect_to`

但前端根据 `payment_state` 判断是自动跳转还是只展示按钮。

## 前端改动计划

### 1. 扩展 PaymentStatusModal

增加购买类型识别：

- `subscription`
- `storage`
- `points`

根据类型展示不同标题和内容。

### 2. 统一 direct checkout result 事件

复用现有 `BillingDirectCheckoutResultEvent`，将 storage addon 成功结果也转换成 `SessionData` 并 dispatch。

建议字段：

```ts
interface SessionData {
  status: StripePaymentStatus;
  amount?: number;
  credits?: number;
  storageGb?: number;
  invoice_id?: string;
  invoice_url?: string;
  invoice_pdf_url?: string;
  receipt_url?: string;
  currency?: string;
  subscription_id?: string;
  plan_name?: string;
  product_type?: 'subscription' | 'storage' | 'points';
}
```

### 3. 调整 storage addon 成功处理

当前逻辑中类似下面的行为应调整：

```ts
if (res?.redirect_to) {
  window.open(res.redirect_to);
}
```

改为：

- `payment_state === 'paid'`：展示成功弹窗。
- `payment_state === 'requires_action'`：跳转 Stripe 页面。
- 没有明确状态但有 `redirect_to`：保守按需跳转处理。

### 4. 优化 points 成功弹窗

points 成功后：

- 标题展示 `Points purchased successfully`。
- 内容展示 points 数量和金额。
- 如果有 receipt URL，展示 `View receipt`。

### 5. 弹窗按钮

成功弹窗按钮建议：

- `Close`
- `View invoice`
- `Download PDF`
- `View receipt`

按钮只在对应 URL 存在时展示。

## 验收标准

1. Plan upgrade 支付成功后展示站内成功弹窗。
2. Storage addon 支付成功后展示站内成功弹窗，不自动新开 invoice tab。
3. Points 支付成功后展示 points 专属成功弹窗。
4. 需要 SCA / 3DS / 继续支付时仍能正常跳转 Stripe 页面。
5. 成功弹窗中可手动打开 invoice、PDF 或 receipt。
6. 刷新页面后不会重复弹出已处理的成功弹窗。
7. 支付取消时不展示误导性的失败弹窗，可展示取消状态或静默返回。

## 风险与注意事项

1. Stripe Hosted Invoice Page 不应 iframe 内嵌。
2. `redirect_to` 目前语义可能同时表示"继续支付"和"查看 invoice"，需要后端补充 `payment_state` 以减少前端猜测。
3. Webhook 仍是最终支付状态的权威来源，前端弹窗只能作为用户反馈，不应直接作为权益发放依据。
4. 对已支付 invoice 的展示应允许 webhook 延迟，必要时通过 session 查询或 payment order 查询补齐信息。

## 推荐实施顺序

1. 后端统一 subscription modify 返回结构，增加 `payment_state`。
2. 前端扩展 `PaymentStatusModal` 数据结构和标题文案。
3. 改造 storage addon 成功处理，移除成功后自动 `window.open` invoice。
4. 优化 points 成功弹窗字段展示。
5. 增加 receipt / invoice 按钮。
6. 做 plan、storage、points 三条端到端手工验收。