---
doc_type: approval-report
goal: account-center
status: approved
reason: RiskAcceptanceNeeded
decision: "1A 2A 3A"
decided_by: owner
updated: 2026-08-02
---

# Owner 裁决 · account-center residual

账号中心产品面（头像按钮、`/account/*`、privacy／settings、旧入口收口）已可验收；下列证据缺口阻塞「epic 全量 DoD／items done」。

## 选项

### 1. restore compose-identity（CMD-002）

`restore-compose` 在 `compose-identity`／`fix-fixed-services-volumes` 失败（exit 5）。package-validate＋dual-key 已绿。

- **A**：waive 本机全量 restore，另开 issue 修 compose 契约后再补跑  
- **B**：你提供可用 Docker／compose 目标，本 goal 继续补跑到绿  
- **C**：认为属本 epic 必须绿，暂停 goal 直到修好

**推荐**：A（呈现与协议实现已齐；compose 属 ops 环境债）

### 2. 人工断点截图（STEP-004）

- **A**：接受模型／CSS／清单为阶段性证据，截图后补进 evidence  
- **B**：阻塞至你补齐 375／1440／200%／coarse 截图包

**推荐**：A

### 3. `make check`

`golangci-lint ./...` 已 **0 issues**（含 dataexport gofmt）。若本机 Docker 仍不可用，Testcontainers／restore 仍可能红。

- **A**：waive 依赖 Docker 的全量 `make check`，以已绿的 lint＋CMD-001／003／generate-check 为准  
- **B**：你启 Docker 后要求本 goal 跑通完整 `make check`

**推荐**：有 Docker 选 B；否则 A

## Owner 决定

**`1A 2A 3A`**（2026-08-02，采纳推荐）：

1. waive 本机全量 restore；compose-identity 另开／挂账，不阻塞本 goal  
2. 接受模型／CSS／清单为阶段性断点证据；截图可后补  
3. waive 依赖 Docker 的全量 `make check`；以 lint＋CMD-001／003／generate-check 为准  

下一步：Task agent 功能验收（导航／账号中心产品面）。
