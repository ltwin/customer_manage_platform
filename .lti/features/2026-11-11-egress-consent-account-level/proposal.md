---
risk:
  factors: [trust-boundary, broad-impact]
behavior: changed
reads: [creative/agent-run]
decisionsPending: true
---

<!-- 草稿占位：owner 2026-11-11 同意另开此 change，尚未起草完整方案，也未提交批准。 -->

## Why

摄影师真正做出的外发同意，是首次使用助手时的「发送前说明」，这是账号级的、按说明版本生效的同意。后端仍然按逐个资产修订核对授权：前端每发一条消息就自动建一份授权，修订清单从消息里的附件推出来，摄影师从来没有机会逐项选择。这份账本挡住了正当操作（例如 `agent-message-queue` 里插队带附件），实际上没有保护到任何东西。

## What Changes（待起草）

- 把外发授权收敛为「账号级同意 + 说明版本 + 撤销」。删除逐条消息自动建授权的步骤，以及逐修订清单的核对；保留归属、可用性、用途权利、数据类别和撤销这几项检查。
- 处理现有的 `creative_egress_consents` / `creative_egress_consent_contents` 数据、可读性判定（`readableInTx`）、派发复核（`admitDispatchInTx`）和前端发送链。
- 待定：注册时是否补一份正式的同意条款；撤销在账号级下的语义。

## Acceptance Criteria

待起草。

## Impact

- 代码：`backend/internal/creativeagent/`（consents、runs、context、execute）、`frontend/src/creative-canvas/editor/`（发送链）、`docs/product/creative-canvas-system/data-model.md`。
- 契约：外发同意的资源边界；`egress-consents` 接口。

## Risk Notes

- trust-boundary：这是外发隐私边界的收敛，需要逐项确认撤销、用途权利、工具返回素材的授权不被放宽。
- broad-impact：建运行、补充、工具读取、派发复核都会受影响。
