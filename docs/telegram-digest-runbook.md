# Telegram 每日经营摘要运维手册

本文只覆盖首版单 Bot、单私聊、单进程 long polling。它不授权向客户发送 Telegram 消息，也不引入 Webhook、多 Bot、多会话、通用通知队列或多实例选主。

## 1. 安全边界

- `TELEGRAM_BOT_TOKEN` 只能通过生产 secret/environment 注入。禁止写入数据库、Git、前端、工单、截图、日志或命令输出。
- `TELEGRAM_BOT_USERNAME` 不含 `@`，必须是 BotFather 返回的完整 username。
- 浏览器拿到的 Bind token 是 32-byte 随机值的 43 字符无填充 base64url 表示，只在组件内存和 Telegram `/start` payload 短期存在；数据库只保存 SHA-256。
- 真机验证只使用 synthetic 账号、客户、订单、档期和提醒。截图必须裁剪/脱敏，不包含 Bind token、Bot token、chat id、真实客户或业务正文。
- 日志只记录 integration 状态、错误类型和必要的账号内部标识；不得记录 Bot API URL、请求正文、消息正文或 token。

## 2. BotFather 与 Bot API 前置检查

1. 由 owner 在 Telegram 中手工使用官方 BotFather 创建 Bot；不要脚本化或代替 owner 操作 BotFather。
2. 将 BotFather 返回的 token 直接写入部署平台的 secret，避免经过聊天、文档或 shell 历史。
3. 记录不含 `@` 的 username 到 `TELEGRAM_BOT_USERNAME`。
4. 用具备 secret masking 的运维工具调用 Bot API `getWebhookInfo`，确认结果中的 `url` 为空。long polling 与活动 Webhook 互斥；本服务不会自动删除 Webhook。
5. 若 `getUpdates` 返回 `webhook_conflict`，先暂停验证并由 owner 核对 Bot 归属和 Webhook；禁止在未确认归属时调用 `deleteWebhook`。

## 3. 配置与启动语义

```dotenv
TELEGRAM_BOT_TOKEN=
TELEGRAM_BOT_USERNAME=
```

- 两项同时为空：Telegram disabled；不启动 TelegramRunner，主 HTTP、Reminder 和 dashboard 正常工作，bind-token 返回 500。
- 两项同时合法：Telegram active；composition root 只注册一个 TelegramRunner，其内部运行 long poll、daily enqueue 和唯一 DeliverySender。
- 只配置一项或 username 非法：Telegram unavailable；主应用仍启动并输出脱敏的 `invalid_config` 状态，bind-token 返回 500。
- Bot API `invalid_auth`：integration 进入五分钟探测退避；不会阻断主应用，也不会烧掉 Delivery 失败预算。
- `webhook_conflict`、client/protocol error：poll 暂停五分钟再探测；不得形成热循环或日志风暴。

配置状态的安全日志示例（无 token、URL、chat id 或正文）：

```json
{"level":"INFO","msg":"telegram integration disabled","status":"disabled"}
{"level":"INFO","msg":"telegram integration enabled","status":"active"}
{"level":"WARN","msg":"telegram integration unavailable","status":"invalid_config","error":"telegram 配置不完整"}
{"level":"INFO","msg":"telegram integration runner stopped"}
```

## 4. 单副本与发布

首版强制 `app replicas=1`，并使用 Recreate/stop-old-before-start-new。`docker-compose.yml` 固定 `replicas: 1`、`update_config.order: stop-first` 和 12 秒容器 grace period；应用内部收到 SIGTERM 后在 10 秒 lifecycle 内关闭 HTTP 并等待所有 runner。

单机 Compose 更新应用时使用会重建单个 app 容器的发布流程，并在发布平台确认旧 app 已停止后再启动新 app；不要同时执行两个 `docker compose up`，不要滚动重叠。多副本、Kubernetes RollingUpdate 或蓝绿双活上线前必须另做 leader election/Webhook 设计。

SIGTERM 验证：

1. 确认 Bot 没有活动 Webhook，应用为 active，long poll 正在等待。
2. 向 app 容器发送正常 stop/SIGTERM，不发送 SIGKILL。
3. 计时确认进程在 10 秒内退出；日志出现 `telegram integration runner stopped`，且没有 token、URL、正文。
4. 重启后确认 HTTP `/healthz` 正常，pending Delivery 由 lease/retry 恢复，不手工篡改 attempts/claim。

## 5. Synthetic 真 Bot 验收

执行前建立全新的 synthetic 工作室账号和合成业务数据。禁止使用现有生产账号或真实客户。

### 5.1 绑定

1. 登录 synthetic CRM，打开“设置”。
2. 点击“绑定 Telegram”；浏览器应直接打开 `t.me/<bot>?start=<opaque>`。若弹窗被拦截，应显示 `role=alert` 和“再次打开 Telegram”按钮。
3. 只在与 Bot 的私聊中发送 `/start`；群聊 `/start` 必须拒绝。
4. 收到固定“Telegram 绑定成功”回执后，仅截取回执和 synthetic Bot 名称；裁掉地址栏/start payload、chat id 和其他会话。

### 5.2 每日摘要

1. 创建合成的 pending reminder、今日 shoot 档期和 delivered 未结清订单；不要使用真实姓名或正文。
2. 把 synthetic 账号 `digest_hour` 调整到当前账号时区即将到达的小时。
3. 等待每分钟 scheduler tick；同一 local date 应只收到一次 daily 摘要。
4. 截图只保留合成摘要，不包含 Telegram 账号侧边栏、chat id 或其他会话。

### 5.3 `/today`

1. 在同一私聊发送 `/today`。
2. 验证它先触发 Reminder scan，再使用与 daily 相同 Renderer；scan 暂时失败时收到固定 temporary-unavailable 文案。
3. 同一 Telegram update 重投不得重复 Delivery 或改变已确定的 message kind。
4. 截图仅保留 synthetic `/today` 与响应。

三张脱敏截图分别命名为 binding、daily、today，放入 feature 的 evidence 目录。没有 owner 提供的真实 Bot 凭证时，此项必须保持 blocked/handoff，禁止以 fake 或本地 HTTP 截图冒充真 Bot。

## 6. 故障处理

| 现象 | 行为 | 运维动作 |
|---|---|---|
| Telegram 配置为空 | integration disabled，Web 正常 | 仅在确需启用时注入两项配置并 Recreate app |
| 配置部分缺失/username 非法 | `invalid_config`，Web 正常 | 修正 secret/config；不要把 token 打到日志 |
| `webhook_conflict` | poll 暂停五分钟探测 | 核对 Bot 归属和 `getWebhookInfo`；owner 决定是否移除 Webhook |
| `invalid_auth` | integration 五分钟退避，Delivery 不烧预算 | 由 owner 轮换 Bot token，Recreate app |
| network/timeout/server error | 1s/5s/30s/60s poll backoff；Delivery 1m/5m/30m retry | 检查网络与 Telegram 状态，不手工重放正文 |
| recipient missing/conflict | Delivery 五分钟退避且不烧预算 | 在 Settings 重新绑定；不要直接写 `telegram_chat_id` |
| send outcome unknown | 保留 claim 等 lease/repair，禁止立即重发 | 等 30 秒 lease 恢复并检查脱敏状态，不猜测 Telegram 结果 |

## 7. 卸载

1. 同时清空 `TELEGRAM_BOT_TOKEN` 与 `TELEGRAM_BOT_USERNAME`，Recreate app；确认状态为 disabled。
2. 如需彻底移除功能，再按 migration down、OpenAPI route/tag、Settings 卡片、TelegramRunner/config mount 的逆序变更执行；不要只删除前端入口后留下后台发送器。
3. 是否在 BotFather 撤销 token 由 owner 决定；应用不会自动操作 BotFather 或 Webhook。
