# Telegram 冒烟脚本的 chat_id 坑

## 背景

platform-skeleton 只做一次性 TG bot `sendMessage` 冒烟，用 `getUpdates` 找 chat_id。review 指出：如果 bot 被拉进群，或其他人刚给 bot 发过消息，脚本取“最新 chat”可能把测试消息发到错误会话；如果 Telegram API 返回 `ok:false`，脚本也应 fail-fast。

## 结论

后续写 TG 脚本或 bot 绑定逻辑时，不要默认“getUpdates 最新一条就是 owner 的 chat”：

- 支持 `TELEGRAM_CHAT_ID` 显式覆盖，owner 已知 chat_id 时优先用它。
- 自动发现 chat_id 只适合本地一次性冒烟，且应在输出里明确显示目标 chat。
- Telegram API 响应必须检查 `ok` 字段；`ok:false` 时打印错误并退出非 0。
- 真正的绑定流程仍应走 bind-token / deep-link，不复用冒烟脚本的简化假设。

## 证据

- 脚本落点：`scripts/telegram-smoke.sh`
- 审查记录：`.codestable/features/2026-07-06-platform-skeleton/platform-skeleton-review.md` R2-11
- 冒烟证据：`.codestable/features/2026-07-06-platform-skeleton/evidence/a12-tg-smoke-received.png`
