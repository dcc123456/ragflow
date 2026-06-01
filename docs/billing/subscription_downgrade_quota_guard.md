# 订阅降级配额超限拦截方案

## 1. 背景

### 1.1 问题

用户可以在当前计费周期内预约降级（例如 40GB 存储降为 20GB），系统会在周期结束时自动执行降级并扣费。

但中间存在一个时间窗口：在预约到生效之间，用户可能超额使用资源。例如：

| 时间点 | 事件 |
|--------|------|
| 月初 | 用户预约降级 40GB → 20GB，已支付 40GB 的费用 |
| 月中 | 用户上传文件至 35GB（合法，因为已支付 40GB） |
| 月末 | 降级生效，配额变为 20GB + 基础套餐。35GB 超出新限额 |

**核心矛盾**：降级生效瞬间，实际用量可能超出降级后的配额。

### 1.2 约束

- **不修改核心写路径**：不在上传文件、添加成员、创建知识库等高频操作中增加检查
- **财务安全**：扣费前完成拦截，不产生多收费
- **用户知情**：超限时主动通知用户

---

## 2. 解决方案

在降级预约到生效之间，增加**三层防护**：

```
用户预约降级
     │
     ▼
┌─────────────────────────────────────────┐
│ 第 1 层：每日扫描                        │
│   距生效 > 3 天 + 超限 → 发邮件提醒       │
│   距生效 ≤ 3 天 → 加入高频检查池          │
└─────────────────────────────────────────┘
     │
     ▼
┌─────────────────────────────────────────┐
│ 第 2 层：高频检查（每 15 分钟）            │
│   超限 → 取消降级 + 发邮件 + 记录日志      │
│   未超限 → 保留在池中继续监控              │
└─────────────────────────────────────────┘
     │
     ▼
┌─────────────────────────────────────────┐
│ 第 3 层：生效时刻检查                     │
│   降级生效时最终校验                      │
│   超限 → 紧急通知用户（无法回滚）           │
│   未超限 → 正常执行降级                   │
└─────────────────────────────────────────┘
```

**残余风险**：第 1-2 层覆盖生效前（最长 15 分钟窗口）。第 3 层在生效瞬间兜底——即使前两层都失效，仍有告警。

---

## 3. 降级类型

系统支持两类降级，可以单独或同时发生：

| 类型 | 举例 | 记录位置 |
|------|------|----------|
| 基础套餐降级 | Starter → Trial、Pro → Trial | 新增字段：预约降级目标套餐名 |
| 存储附加包降级 | 40GB → 20GB、20GB → 0GB | 已有字段：预约降级目标存储量 |

降级生效时间：当前计费周期的结束时间（已有字段）。

---

## 4. 第 1 层：每日扫描

### 4.1 触发方式

系统每天自动运行一次。利用分布式锁保证多台服务器上只有一个实例执行。如果执行过程中崩溃，锁过期后会由其他服务器自动重试。

### 4.2 扫描什么

找出所有**已预约降级但尚未生效**的租户。

### 4.3 怎么处理每个租户

对每个租户，比较当前用量和降级后的配额：

| 当前用量 vs 目标配额 | 距生效时间 | 系统行为 |
|:---:|:---:|---|
| 超限 | > 3 天 | 发邮件提醒（同租户 7 天内不重复） |
| 超限 | ≤ 3 天 | 加入高频检查池 |
| 不超限 | > 3 天 | 不做任何操作 |
| 不超限 | ≤ 3 天 | 加入高频检查池 |

### 4.4 配额如何计算

系统检查三个维度：**存储用量**、**成员数量**、**应用数量**。

目标配额 = 目标套餐的基础配额 + 降级后的存储附加包配额（如果存储附加包也在降级，用降级后的较小值）。

---

## 5. 第 2 层：高频检查

### 5.1 触发方式

每 **15 分钟**执行一次。多个服务器实例同时运行时，每个租户由第一个抢到锁的实例处理。

### 5.2 怎么处理池中租户

| 条件 | 行为 |
|------|------|
| 降级已不存在（用户自己取消了） | 从池中移除 |
| 超限 | **取消降级**：通知 Stripe 释放降级排期、清除降级标记、发邮件告知用户 |
| 不超限 | 保留在池中，继续监控 |

### 5.3 取消降级会发生什么

- Stripe 侧的降级排期被取消，周期结束时照常续费当前套餐
- 用户收到邮件，告知降级被取消的原因（展示超限的维度）
- 用户释放多余资源后可重新预约降级
- 系统记录操作日志供审计

### 5.4 故障处理

如果某台服务器宕机，它持有的处理锁最长 10 分钟后过期，其他服务器会自动接管。最多丢失一个检查周期。

---

## 6. 第 3 层：生效时刻检查

前两层的目标是在降级生效**前**拦截。但极端情况下（比如 webhook 刚好在高频检查间隔内到达），降级可能在检查之前就已生效。

### 6.1 行为

降级生效时，系统做一次最终的配额校验：

| 校验结果 | 行为 |
|----------|------|
| 不超限 | 正常执行降级，配额更新 |
| 超限 | 正常执行降级，同时发紧急邮件通知用户；用户后续上传文件、添加成员等操作会被新配额限制 |

### 6.2 为什么超限时不回滚

此时 Stripe 侧已完成扣费和降级，无法回滚。但配额限制会正常工作——用户选择了降级，即意味着接受了更低配额。系统只是确保用户知情。

### 6.3 与第 1-2 层的区别

| | 第 1-2 层 | 第 3 层 |
|---|---|---|
| 时机 | 降级生效**前** | 降级生效**时** |
| 能否阻止 | **能**——取消降级排期 | **不能**——Stripe 已执行 |
| 用户结果 | 保留原套餐 | 降级生效，超限功能受限 |

---

## 7. 用户通知

### 7.1 通知类型

| 场景 | 邮件主题 | 频率限制 |
|------|----------|:---:|
| 超限提醒（距生效 > 3 天） | "降级预约提醒 — 配额超出降级后限制" | 7 天 1 次 |
| 降级被取消 | "您的降级预约已被取消" | 不限（关键事件） |
| 降级生效但超限 | "降级已生效 — 您的用量超出新配额" | 不限（关键事件） |

### 7.2 邮件内容

以取消通知为例：

> 您的降级（Pro → Trial）已被系统自动取消。原因是当前用量超出了降级后的配额：
> - 存储：35 GB 用量，30 GB 限额
> - 成员：8 人，5 人限额
>
> 您未被收取额外费用。如需降级，请先释放多余资源后重新提交。
> 如有疑问，请联系客服。

### 7.3 站内通知

当前系统仅支持全局通知栏。本期不涉及按用户推送的站内通知。

---

## 8. 运维

### 8.1 监控指标

| 指标 | 含义 |
|------|------|
| 每日扫描执行次数 | 应为每天 1 次 |
| 降级被取消次数 | 按套餐分组 |
| Webhook 防线触发次数 | 预期为 0 |
| 高频检查池大小 | 异常增长需关注 |

### 8.2 告警规则

- 24 小时内每日扫描未执行 → 告警
- Webhook 防线触发 → 立即告警
- 检查池大小超过 100 → 可能存在异常积压

### 8.3 部署控制

可通过环境变量 `DOWNGRADE_GUARD_ENABLED=false` 关闭守护任务。正常运行时默认开启。

---

## 9. 测试要点

### 9.1 降级标记字段

| 场景 | 期望 |
|------|------|
| 预约套餐降级 | 数据库写入目的套餐名；如同时有存储附加包降级，存储目标也写入 |
| 降级生效 | 套餐名更新；降级标记清空 |
| 预约存储附加包降级（40→20GB） | 存储目标 = 20GB，当前存储附加包保持 40GB |
| 存储附加包降到 0 | 存储目标 = 0 |
| 非降级操作（如升级） | 降级标记保持为空 |

### 9.2 守护逻辑

| 场景 | 期望 |
|------|------|
| 超限 + 距生效 > 3 天 | 发邮件提醒，不取消 |
| 距生效 ≤ 3 天（不管是否超限） | 加入高频检查池 |
| 池中租户超限 | 取消降级，发邮件，从池中移除 |
| 池中租户不超限 | 保留在池中 |
| 降级已被用户取消 | 从池中移除 |
| 多台服务器同时扫描 | 只执行一次 |
| 扫描中途崩溃 | 锁过期后自动重试 |

---

## 附录：技术实现速查

### A1. 涉及文件

| 文件 | 改动类型 | 说明 |
|------|:---:|------|
| `api/db/db_models.py` | 修改 | 新增 `target_plan_name` 字段 + 数据库迁移 |
| `api/apps/billing_app.py` | 修改 | 降级预约时写入 `target_plan_name` |
| `api/services/billing_webhook_service.py` | 修改 | 降级生效时清空 `target_plan_name`；Webhook 最终防线检查 |
| `api/services/downgrade_guard.py` | **新建** | 守护任务核心逻辑 |
| `api/wsgi.py` | 修改 | 启动 3 个后台线程 |
| `api/utils/email_templates.py` | 修改 | 3 个邮件模板 |

### A2. Redis 密钥

| Key | 类型 | TTL | 用途 |
|-----|------|-----|------|
| `downgrade:daily_scan_lock` | 分布式锁 | 30 分钟 | 每日扫描防多实例并发 |
| `downgrade:last_scan_date` | 字符串 | 到当天 23:59:59 | 每日扫描防同天重复 |
| `downgrade:check:{tenant_id}` | 分布式锁 | 600 秒 | 高频检查防多实例重复处理同一租户 |
| `downgrade:high_freq_pool` | Set | 无 | 高频检查池 |
| `downgrade:warn:{tenant_id}` | 字符串 | 7 天 | 邮件限频标记 |

### A3. 环境变量

| 变量 | 默认值 | 用途 |
|------|--------|------|
| `DOWNGRADE_GUARD_ENABLED` | `true` | 设为 `false` 可关闭守护任务 |
| `DOWNGRADE_CHECK_INTERVAL_SEC` | `900` | 高频检查间隔（秒） |

### A4. 邮件模板 key

| 模板 key | 场景 |
|-----------|------|
| `downgrade_warning` | 超限提醒（距生效 > 72h） |
| `downgrade_cancelled` | 降级被取消 |
| `downgrade_effective_exceeded` | 降级生效但超限 |

### A5. 日志关键字

| 关键字 | 级别 | 含义 |
|--------|------|------|
| `Downgrade CANCELLED` | ERROR | 高频检查拦截了降级 |
| `DOWNGRADE EFFECTIVE BUT QUOTA EXCEEDED` | CRITICAL | Webhook 防线检测到超限 |
| `Daily downgrade scan` | INFO | 每日扫描执行情况 |

### A6. 健康检查端点

`GET /billing/downgrade-guard/health`

返回 `last_scan_date`、`high_freq_pool_size`、`last_check_round_at`。

### A7. 手动测试计划

#### 环境准备

- Server 已启动，`BILLING_ENABLED=1`，Redis 可用
- Stripe test mode，Stripe CLI 转发 webhook：`stripe listen --forward-to 127.0.0.1:9380/v1/billing/webhooks/stripe`
- 准备好 test clock、subscription ID、tenant ID

#### 时钟推进方法

降级生效需等到计费周期结束。通过 Stripe Dashboard 的 Test Clocks 页面可瞬间跳到未来：

1. 打开 Stripe Dashboard Test Clocks 页面（当前：`https://dashboard.stripe.com/test/billing/subscriptions/simulations`）
2. 点击目标 clock，找到 subscription 的 `current_period_end` 时间
3. 在页面上将 frozen time 推进到 `current_period_end` 之后（例如加 1 小时）
4. 等待 clock 状态变为 `ready`
5. 降级相关事件会被 Stripe CLI 自动转发到本地 server，也可以手动触发 webhook 补发

完成后降级立即生效，无需等待实际时间。

#### 已知限制：Guard 每日扫描与 Test Clock 的时间偏差

Guard 每日扫描用服务器系统时间判断 `end_time - now ≤ 72h`，test clock 不推进系统时间。因此测试 7 中超限取消的完整自动链路无法自动触发。

**解决方法**：预约降级并注入假文件后，直接修改 DB `end_time` 到系统时间的 48h 后：

```sql
UPDATE billing_subscription
SET end_time = NOW() + INTERVAL 48 HOUR,
    update_time = UNIX_TIMESTAMP(NOW())
WHERE tenant_id = '<tenant_id>';
```

然后等 guard 的下一个 tick（最长 10 分钟）即可触发入池和取消。

---

#### 测试 1：预约降级时写入降级目标

| # | 操作 | 检查点 |
|---|------|--------|
| 1 | 注册用户，升级到 Starter | `GET /v1/billing/subscription` 返回 `plan_name = "Starter"` |
| 2 | 调用 `POST /v1/billing/subscription` 预约 Starter → Trial | 返回 `scheduled_change`，含 `schedule_id` |
| 3 | `GET /v1/billing/subscription` | `target_plan_name = "Trial"`，`plan_name` 仍为 `"Starter"`，`pending_subscription_change.pending_plan_name = "Trial"` |

#### 测试 2：降级生效后清空降级目标

| # | 操作 | 检查点 |
|---|------|--------|
| 1 | 完成测试 1 步骤 1-2 | |
| 2 | 在 Stripe Dashboard 上将 test clock 推到 `end_time` 之后（见上述"时钟推进方法"） | |
| 3 | 同步 webhook | |
| 4 | `GET /v1/billing/subscription` | `plan_name = "Trial"`，`target_plan_name = null` |

#### 测试 3：SubscriptionSchedule 创建不误清降级目标

核心场景：预约降级后，Stripe 创建 SubscriptionSchedule 也会触发 `customer.subscription.updated`——此时套餐价格没变，系统必须保留 `target_plan_name`。

| # | 操作 | 检查点 |
|---|------|--------|
| 1 | 完成测试 1 步骤 1-2 | `target_plan_name = "Trial"` |
| 2 | 搜索 server 日志 | 找到预约降级后的 `customer.subscription.updated` 处理日志，`plan_changed=False`，确认 marker 被保留 |
| 3 | `GET /v1/billing/subscription` | `target_plan_name` 仍为 `"Trial"` |

#### 测试 4：存储附加包降级

| # | 操作 | 检查点 |
|---|------|--------|
| 1 | Starter 用户，添加 40GB 存储附加包 | `GET /v1/billing/storage` 返回 `addon_storage_bytes = 40GB` |
| 2 | `PATCH /v1/billing/storage`，target 设为 20GB | 返回 `scheduled_change` |
| 3 | `GET /v1/billing/storage` | `addon_storage_bytes = 40GB`（当前不变），`target_storage_bytes = 20GB` |
| 4 | 在 Stripe Dashboard 推进 test clock + 同步 webhook | |
| 5 | `GET /v1/billing/storage` | `addon_storage_bytes = 20GB`（降级生效） |

#### 测试 5：组合降级（套餐 + 存储同时降）

| # | 操作 | 检查点 |
|---|------|--------|
| 1 | Starter + 40GB 存储 | |
| 2 | 预约 Starter → Trial | `GET /v1/billing/subscription` 返回 `target_plan_name = "Trial"`<br>`GET /v1/billing/storage` 返回 `target_storage_bytes = 0` |

#### 测试 6：守护任务运行状态

| # | 操作 | 检查点 |
|---|------|--------|
| 1 | `GET /v1/billing/downgrade-guard/health` | 返回 `daily_scan_ok`、`high_freq_pool_size`、`metrics` |
| 2 | 搜索 server 日志 `Daily scan` | 有近期扫描记录 |
| 3 | 搜索 server 日志 `High-freq check` | 有近期检查记录 |

#### 测试 7：超限自动取消降级

前提：用量超过降级后配额，且 `end_time` 在 72 小时内。

| # | 操作 | 检查点 |
|---|------|--------|
| 1 | 预约降级（确保 `end_time` ≤ 72h） | |
| 2 | 上传文件 / 注入数据，使用量超过降级目标 | |
| 3 | 等待每日扫描（默认 10 分钟窗口）或手动触发 | 日志出现 `Added tenant ... to high-freq pool` |
| 4 | 等待高频检查（默认 15 分钟，可临时设 `DOWNGRADE_CHECK_INTERVAL_SEC=60` 加速） | 日志出现 `Downgrade CANCELLED` |
| 5 | `GET /v1/billing/subscription` | `target_plan_name = null`，降级已被取消 |
| 6 | Stripe 控制台查看 SubscriptionSchedule | 状态为 `released` |
| 7 | 检查注册邮箱 | 收到主题为 "Your Scheduled Downgrade Has Been Cancelled" 的邮件 |

#### 测试 8：Webhook 防线触发（存储降级超限未被前两层拦截）

前提：构造超限数据，模拟前两层防线均失效的极端场景。

| # | 操作 | 检查点 |
|---|------|--------|
| 1 | Starter + 40GB 存储，预约降至 20GB | |
| 2 | 注入假文件记录使用存储超过 25GB（Starter 基础 5GB + 目标 20GB） | |
| 3 | `GET /v1/billing/downgrade-guard/health` | 记录 `webhook_violations_total` 当前值 |
| 4 | 在 Stripe Dashboard 推进 test clock + 同步 webhook | |
| 5 | 再次读健康端点 | `webhook_violations_total` 增加了 |
| 6 | 搜索 server 日志 | `DOWNGRADE EFFECTIVE BUT QUOTA EXCEEDED` |
| 7 | 检查注册邮箱 | 收到主题为 "Downgrade Effective — Usage Exceeds New Quota" 的邮件 |
| 8 | 清理假文件 | 假文件删除 |

#### 测试 9：邮件限频

| # | 操作 | 检查点 |
|---|------|--------|
| 1 | 制造超限场景，触发每日扫描 | 收到提醒邮件 |
| 2 | 不修改使用量，再次触发每日扫描 | **不**收到提醒邮件（7 天内限频） |
| 3 | 检查 Redis key `downgrade:warn:{tenant_id}` | 存在，TTL ≈ 7 天 |

#### 测试 10：end_time 已过则跳过取消

| # | 操作 | 检查点 |
|---|------|--------|
| 1 | 制造超限场景，将租户手动加入 Redis 池 | |
| 2 | 在 Stripe Dashboard 上将 test clock 推到 `end_time` 之后（见上述"时钟推进方法"） | |
| 3 | 触发高频检查 | 日志出现 `Skip cancel ... end_time already passed` |
| 4 | 检查 Redis | 租户已从池中移除 |

#### 测试 11：禁用开关

| # | 操作 | 检查点 |
|---|------|--------|
| 1 | 设 `DOWNGRADE_GUARD_ENABLED=false`，重启 server | |
| 2 | 搜索日志 | `Downgrade guard disabled` |
| 3 | `GET /v1/billing/downgrade-guard/health` | metrics 不再更新（daemon 未运行） |
