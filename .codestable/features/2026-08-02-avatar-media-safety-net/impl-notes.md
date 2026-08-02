# avatar-media-safety-net · 实现笔记

日期：2026-08-02  
分支：`feat/account-center`  
范围：STEP-002…008（STEP-000/001 已完成）

## 命令结果摘要

| ID | 命令 | 结果 | 说明 |
|---|---|---|---|
| CMD-001 | `cd backend && go test ./internal/customer/... ./internal/platform/httpapi/... ./internal/avatarmedia/... -count=1 -parallel=1` | **pass** | 含 avatarmedia；customer／httpapi 零漂移回归绿 |
| CMD-002 | `python3 scripts/lib/v1-ops-package-selftest.py` | **pass** | v1/v2 validate、metadata 派生、dual key golden、重复 JSON key、伪造 format |
| CMD-003 | `scripts/test-v1-ops-backup-restore-safety.sh` | **pass** | 含 selftest；信号／预检契约 |
| CMD-004 | `scripts/test-avatar-media-v1-restore-wipe.sh` | **blocked** | Docker engine 不可用（`docker info` 失败）；脚本与 A14/A15 断言已落盘；证据路径见 `evidence/restore_fixture_log` |
| CMD-005 | `scripts/check-avatar-media-safety-net-scope.sh` | **pass** | merge-base…HEAD ∪ untracked；无产品 profile 泄漏 |

## 落地要点

- 新包 `backend/internal/avatarmedia`：typed `Key`/`Prefix`/`Cursor`、`DecodeConfirm`（原始字节）、`Local` inventory（ParseKey；`.avatar-tmp-*`→integrity）、dual key、`GenerateV2`／`ProjectV1ForVerify`／strict decode。
- customer：`avatarimage` 先 DecodeConfirm 再 PNG≤512 normalize；读路径 `ConfirmIntegrity`；`avatarstore` 转发 `avatarmedia.Local`。
- CLI `avatar-manifest generate` 仅 v2；verify 双格式分派。
- ops：`manifest_schema_version` allowlist + 自 manifest.format 派生；伪造／不一致拒。
- 未建 `account_profiles`／profile API／UI／`accountprofile` 包。

## 风险 / 决策点

1. CMD-004 完整 A15 需 desktop-linux + `crm:local`（或 `AVATAR_MEDIA_WIPE_IMAGE`）+ migrate 可用 compose 目标；当前环境 blocked。
2. 建议下一 iteration 可进 `account-profile-center`（共享 seam／dual reader／v2 customer-only writer 已就绪）；profile 接入 mixed writer 与真实 migration 仍归 profile-center。
