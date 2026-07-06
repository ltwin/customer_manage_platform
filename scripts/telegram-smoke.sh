#!/usr/bin/env bash
# Telegram bot sendMessage 冒烟（A12 / CMD-004）。
# 前置（owner 动作）：① BotFather 建 bot 取 token；② 给 bot 发一条任意消息（脚本经 getUpdates 取 chat_id）。
# 凭证红线：token 只经环境变量 TELEGRAM_BOT_TOKEN 注入，不入库、不入 git（硬规则 4）。
set -euo pipefail

: "${TELEGRAM_BOT_TOKEN:?TELEGRAM_BOT_TOKEN 未设置：请先在 BotFather 建 bot，并经环境变量注入 token}"
API="https://api.telegram.org/bot${TELEGRAM_BOT_TOKEN}"

chat_id=$(curl -fsS "${API}/getUpdates" | python3 -c '
import json, sys
updates = json.load(sys.stdin).get("result", [])
for u in reversed(updates):
    chat = (u.get("message") or {}).get("chat") or {}
    if "id" in chat:
        print(chat["id"])
        break
')

if [[ -z "${chat_id}" ]]; then
    echo "未从 getUpdates 取到 chat_id：请先用你的 TG 账号给 bot 发一条消息再重试" >&2
    exit 1
fi

text="CRM 平台基座 TG 冒烟：$(date '+%Y-%m-%d %H:%M:%S')"
curl -fsS -X POST "${API}/sendMessage" \
    --data-urlencode "chat_id=${chat_id}" \
    --data-urlencode "text=${text}" >/dev/null

echo "已向 chat_id=${chat_id} 发送测试消息：${text}"
echo "请在真机确认收到并截图归档到 .codestable/features/2026-07-06-platform-skeleton/evidence/"
