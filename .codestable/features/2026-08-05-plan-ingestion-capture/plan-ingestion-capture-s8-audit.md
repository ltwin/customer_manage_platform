---
doc_type: feature-implementation-audit
feature: 2026-08-05-plan-ingestion-capture
status: passed
stage: implementation.before_review
updated: 2026-08-07
---

# S8 实现审计

## 1. 真实 API 最小闭环

在本地 PostgreSQL 与本地 HTTP 服务上完成一次无 fixture 的真实链路，数据使用一次性测试策划和虚构来源，不保留原文、Bearer token 或响应原始 JSON：

1. `POST /api/v1/auth/login`：200；
2. `POST /api/v1/shoot-plans`：201，返回 `draft` 与 revision 1；
3. `POST /api/v1/shoot-plans/{id}/ingestion-sessions`：201，返回 `editing` session；
4. `POST /api/v1/shoot-plans/{id}/ingestion-sessions/{sessionId}/commit`：200，同时返回 1 个核心候选与 2 条 plan-level reference link；
5. `POST /api/v1/shoot-plans/{id}/transitions`（`mark_ready`）：200，plan 进入 `ready`；
6. `POST /api/v1/shoot-plans/{id}/run-sessions`：201，打开 execution-only Run Mode；
7. 另以仅含 URL 的 session 验证五个 snapshot 数组均为非 null 空数组，随后用 transition abandon 清理编辑态。

结果证明摄取 session、核心写入、参考链接、ready transition、Run Mode 与账号作用域使用真实 HTTP/DB wiring 串接；没有使用 parser fixture 冒充端到端结果。

## 2. 浏览器最小闭环与断点

本地嵌入式前端完成：登录 → 打开 `/shoot-plans/{id}/ingestions/new` → 粘贴文本 → 解析候选 → 查看保存摘要 → 确认保存。页面进入“已保存摄取结果”，显示核心候选、参考链接、素材绑定三项摘要，并可返回策划工作台。

已采集并现场检查：

- 1280×900：侧栏、顶部 breadcrumb、三步状态条、保存摘要卡片和返回按钮均在同一视区内；
- 430×900：侧栏折叠为底部导航，三步状态条和摘要卡片保持可读，按钮保持足够触控区域；
- 键盘：页面存在稳定的 `focus-visible` 样式契约，交互控件使用原生 button/input/select；
- 200% zoom：浏览器自动化连接不暴露系统缩放控制，因此以 430px 窄视口、源码响应式契约和 `document.documentElement.scrollWidth === innerWidth` 作为替代证据；未把替代证据写成真实系统缩放通过。

## 3. 审计中发现并修复

首次真实浏览器解析后，后端对空候选集合编码为 JSON `null`，前端在 `dropped.length` 处崩溃。修复内容：

- `snapshotFromParse` 和 session decode 对五个候选集合统一输出空数组；
- 前端 `applySession` 与 commit feedback 对旧/异常响应增加空数组兜底；
- 新增 `TestSnapshotFromParseUsesJSONArraysForEmptyCollections`；
- 重新构建嵌入前端并复测真实闭环，页面不再崩溃。

## 4. 故障与边界证据索引

- parser golden / canonical byte stability：`backend/internal/shootplanning/ingestion/*_test.go`、`testdata/parser_v1_corpus.json`；
- session revision / account scope / observation clock：`repository_integration_test.go`、`application_integration_test.go`；
- combined rollback / reference-only / replay：`application_integration_test.go`、`commit_test.go`；
- stage-1 report dry-run / stable hash：`evidence_test.go`；
- route-enabled + Noop sink fail-closed：`backend/cmd/server/shoot_planning_test.go`；
- generated contract / responsive / no AI/provider/timer guard：`frontend/scripts/plan-ingestion.test.ts`、`frontend/scripts/shoot-planning.test.ts`；
- scope / DoD / evidence machine results：同目录 `plan-ingestion-capture-gate-results.json`、`plan-ingestion-capture-dod-results.json`、`plan-ingestion-capture-evidence-pack-results.json`。

## 5. Residual risks

- source changed/source missing 的逐项 ack override 尚未进入正式 HTTP preview/commit contract，当前仅保留后端 candidate 状态字段；需在 review/QA 中作为明确 residual risk，不得宣称已闭合；
- staged media 的真实 object/DB 双失败与跨账号/cross-plan fault injection 尚未在本次本地最小闭环中执行，已有 participant seam 与 integration baseline，交由 QA 矩阵补证；
- stage-1 evidence 仍是报告生成能力，owner approval 与 CRM dispatch gate 继续保持 pending；
- 真实环境部署、发布、生产采样不属于本 feature execution 授权。
