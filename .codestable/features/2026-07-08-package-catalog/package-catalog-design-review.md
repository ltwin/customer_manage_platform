---
doc_type: feature-design-review
feature: 2026-07-08-package-catalog
status: passed
reviewed: 2026-07-08
round: 1
---

# package-catalog feature design 审查报告

## 1. Scope And Inputs

- Design: `.codestable/features/2026-07-08-package-catalog/package-catalog-design.md`
- Checklist: `.codestable/features/2026-07-08-package-catalog/package-catalog-checklist.yaml`
- Intent / brainstorm: none（roadmap 起头）
- Roadmap: `.codestable/roadmap/photographer-private-crm/photographer-private-crm-roadmap.md`（§3 package 域、§4.1/§4.2/§4.3 契约、§8 变更日志 2026-07-08）+ items.yaml package-catalog 条
- Requirement: `.codestable/requirements/package-catalog.md`（draft）
- Related docs: CLAUDE.md 硬规则、compound《openapi-feature-tag-slicing》《accountscope-fail-loud》《openapi-roadmap-bidirectional-check》
- Code facts checked:
  - `api/openapi.yaml`：/packages POST/GET/PATCH（确认 PATCH properties 漏 note）、无 DELETE 端点、Package 系 schema、409 response 写法（inline description + ErrorEnvelope）
  - `backend/oapi-codegen.yaml`：include-tags 现状（auth/customer-core/customer-profile-complete，无 packages）
  - `backend/internal/customer/`：model/service/repository（PATCH 动态 setClause set() 闭包、requireActiveReferrer 领域校验、WithinTx）作为套系域范本
  - `backend/internal/platform/store/scope.go`：Delete/Exists/Count/Insert/QueryPage/Update 能力齐备
  - `backend/internal/platform/httpapi/customers_profile.go`：DeleteCustomerIdentity 用 c.Status(204) 范本
  - `frontend/src/pages/PackagesPage.tsx`（原型桩）、`api/client.ts`、`components/AppShell.tsx`（PrototypeProvider）

### Independent Review

- Status: local-only
- Detection: native-agent（general-purpose Task agent 已启动两次）
- Provider / agent: 第一次 reviewer 上个进程退出未留完成记录；第二次 reviewer（a11cb6ef53a0fce90）完成大量审查后 transcript 静默，催促消息发出后最终以 **502 上游请求失败（inference gateway SendRequest error）** 收场，未回传任何审查结论——网关错误而非审查输出
- Raw output: 不可用（两次独立 reviewer 均未成功回传结论；第二次确认为 502 网关错误）
- Merge policy: 无外部结论可合并；本报告为主 agent 本地审查，已对 design 声称的关键代码事实逐条核验（见 Code facts checked）
- Gate effect: **用户已明确授权降级 local-only**（"如果过几分钟再不返回，你就自己 review"）；残余风险见第 5 节 Ledger 的 H 级条目
- 降级残余风险：本地审查与 design 作者是同一上下文，缺少异构视角；对"删除 in-use 域生长切法"这类前瞻设计判断，独立 provider 可能给出不同意见。建议 code review 阶段用独立 reviewer 复核 D9。

## 2. Design Summary

- Goal: 套系域闭环——CRUD + 上架/下架 + 删除（引用完整性保护）+ 列表 status 过滤；前端 PackagesPage 从原型桩迁移真 API。作为订单与流失阈值引用源。
- Key contracts: 名词层新增 Package 实体 / PackageListItem(orders_count 恒 0) / 套系域 service+repository interface（含删除 in-use 领域方法）；OpenAPI 补 PATCH note + 新增 DELETE 端点；编排层 5 条线（契约/写入/读取/删除/前端），删除线含 in-use 检查分支。
- Steps: 6 步（契约同步 → 套系域持久化 → HTTP 切片 → 前端 client → PackagesPage 迁移 → harden 终验）；风险热点 = PATCH 部分更新、元↔分换算、下架/删除 in-use 分工。
- Checks: 37 条，来源覆盖名词契约 / 编排骨架 / 流程级约束 / 挂载点 / 范围守护 / 验收场景，均可追溯到 design 对应节。
- Baseline / validation: CMD-001~004（make check / generate 零漂移 / go test / npm build），预检 make check 已写。

## 3. Findings

### blocking

无 blocking。design 正确遵守 roadmap §4.2/§4.3 商品心智契约（已走 cs-roadmap update 改契约在先，非绕开）；关键代码事实（PATCH 漏 note、无 DELETE 端点、AccountScope 能力齐备、DELETE 204 范本）均经核验属实；名词层 / 编排层 / 验收契约 / checklist 可执行且可证伪。

### important

- [ ] FDR-001 `design D9 / 2.2 流程级约束（事务与一致性）` 删除 in-use 检查的 TOCTOU 事务预留描述与本轮实现存在张力
  - Evidence: design 2.2 写"删除的 in-use 检查与删行本轮无跨表事务需求（无 orders 表），order 域接通后须在同事务内'检查引用 + 删除'防 TOCTOU（列入 order-tracking 接通职责，本轮领域方法签名预留事务 scope 入参）"；但 checklist S2 exit_signal 只要求"in-use 方法查引用数返回 0 而非空桩"，未要求本轮就把删除包进 WithinTx。
  - Impact: 若本轮删除领域方法签名不接受事务 scope（只接受 AccountScope），order-tracking 接通时要改方法签名，破坏"数据源后接不改签名"的承诺；这会让 D9 的"接口先行"打折扣。
  - Expected fix scope: implement 时明确——删除领域方法本轮就走 `scope.WithinTx`（照 customer DeleteIdentity 的先查后删同事务模式），in-use 检查方法在事务内调用，order-tracking 只替换"查引用数"的实现体、不改签名。建议在 design 2.2 或 D9 补一句"本轮删除即包 WithinTx，与 DeleteIdentity 同构"，消除张力。（不阻塞用户 review，implement 可据此定）

### nit

- [ ] FDR-002 `design D2 / 2.5` Go 包名 pkgcatalog 与目录名 package 的关系可更明确
  - Evidence: D2 写"目录 `backend/internal/package`（Go 包名 pkgcatalog）"；2.5 写"新增 package/（Go 包 pkgcatalog）"。Go 里目录名与包名可不同，但 `package` 作为目录名合法、作为标识符是关键字。
  - Impact: 表述已正确，但 implement 时 import 路径是 `.../internal/package` 而包声明是 `package pkgcatalog`，容易让人误以为不一致。轻微清晰度问题。
  - Expected fix scope: implement 时在包内加一行注释说明"目录 package / 包名 pkgcatalog（避开关键字）"即可，无需改 design。

- [ ] FDR-003 `checklist A15 check` A15 措辞较长，"in-use 语义"表达可再收紧
  - Evidence: checks 里 A15 条 "DELETE 无引用套系 204 物理删；in-use 领域方法查引用数（本轮返回 0 放行），order 域接通后被引用 409..." 一条塞了本轮行为 + 未来行为。
  - Impact: 可验证性不受影响（本轮只验 204 + 方法非空桩），但读者要分辨哪部分是本轮验收、哪部分是 order-tracking 验收。
  - Expected fix scope: 无需改；acceptance 时只核验本轮部分（204 + 方法查引用数实现），未来部分已在 roadmap order-tracking 备注登记。

### suggestion

- [ ] FDR-004 前端删除二次确认的实现方式可留给 implement
  - design D8/S5 要求"删除二次确认"，但未指定用原生 confirm 还是自建弹窗。customer 域 MergeDialog 是自建弹窗范本。建议 implement 参考 MergeDialog 保持视觉一致，但这是实现细节，不进 design。

### learning

- 「引用完整性校验接口先行、数据源随下游域接通」与既有「聚合字段 orders_count 恒 0 → order 域接通真实计算」是同一模式的两次应用。若 order-tracking 落地后两者都平滑接通、未改本 feature 签名，可考虑由 acceptance 提炼成 compound（"跨域前向依赖：本域先定接口/错误码，数据源随被依赖域落地接通，字段/校验从第一天存在不破坏 shape"）。

### praise

- D4/D9 把"下架无 in-use 校验"与"删除有 in-use 校验"的分工写得干净，并在 Top3 风险 #3、A6/A15、反向核对项里三处交叉守住，避免了"有订单不让下架"这个最可能的语义误加。
- design 没有在 feature 里私自绕开 §4 契约，而是先走 cs-roadmap update 改契约、design 再遵循——符合 CLAUDE.md 硬规则3「契约先行」。

## 4. User Review Focus

- 用户需重点拍板：
  1. 关键假设④ base_price 必填且 > 0（0 元套系按 400 拒绝）——若有"赠拍/0 元体验套系"场景需放开。
  2. D9 删除 in-use "接口先行、数据源随 order 域接通"切法——本轮删除恒放行是否可接受（本轮无订单，实际无风险，但需你认可"删除能力先上、真实拦截等 order 域"）。
  3. 关键假设③ 上架/下架走同一 PATCH status，不单开 archive 端点（与客户域一致）。
- implement 需重点遵守：删除领域方法走 WithinTx 同事务先查后删（FDR-001）；删除 in-use 方法禁 TODO 空桩（须查引用数返回 0 放行）；PackagesPage 移除原型桩 import；元↔分换算集中处理。
- code review / QA / acceptance 需重点复核：AccountScope 账号隔离（删除跨账号 404）、ADR-003 handler 薄层、PATCH 部分更新未传不动、D9 切法（建议 code review 用独立 reviewer 复核，补本次降级缺失的异构视角）。

## 5. Evidence Confidence Ledger

| Check | Verdict | Evidence Class | Basis | Follow-up |
|---|---|---|---|---|
| 需求边界可核对（目标/行为/成功标准/不做） | pass | E | design §1 需求摘要 + 明确不做 6 条 | — |
| 术语无冲突（上架/下架/删除 vs CONTEXT） | pass | C | CONTEXT §套系 + roadmap §4.2 update；术语表已锁 | acceptance 评估"上架/下架"是否进 CONTEXT |
| 遵守 roadmap §4.2/§4.3 契约 | pass | E+C | roadmap §4.2/§4.3/§8 已 update；design 逐条对齐 | — |
| PATCH 漏 note 属实 | pass | E | api/openapi.yaml PATCH properties 无 note（已核验） | S1 补 |
| 无 DELETE 端点属实 | pass | E | api/openapi.yaml 无 /packages DELETE（已核验） | S1 补 |
| AccountScope 能力够（Delete/Exists/Count） | pass | E | scope.go 三方法均存在（已核验） | 无需扩展基座 |
| 409 package_in_use 有表达范式 | pass | E | merge_conflict/last_identity 用 inline desc+ErrorEnvelope（已核验） | S1 照此写 |
| DELETE 204 handler 范本存在 | pass | E | DeleteCustomerIdentity c.Status(204)（已核验） | — |
| 删除 in-use 域生长切法（D9） | pass-with-risk | H | 与 orders_count 恒 0 同模式，工程上成立；但缺异构视角 | FDR-001 + code review 独立复核 |
| checklist steps 独立可验、exit yes/no | pass | E | 6 步各有 yes/no exit_signal | — |
| checks 可追溯 design | pass | E | 37 条 source 分类对应 design 各节 | — |
| DoD Contract 覆盖五阶段 + 命令 + 交付物 | pass | E | 3.y DoD + Validation Commands + Artifacts | — |
| Module depth/seam/adapter | pass | E+C | 2.1 Interface 检查；中等深度、复用 AccountScope | — |
| Acceptance Coverage Matrix 覆盖核心场景 | pass | E | A1-A15 均映射 step + evidence + command | — |
| 基线与验证命令写清 | pass | E | CMD-001~004 + 预检 make check | — |
| 交付物可仓库核验 | pass | E | 交付物清单可从 diff/文件系统/路由 grep 核验 | — |
| 清洁度规则明确（含删除方法禁空桩） | pass | E | §1 清洁度 + checklist dod.cleanliness | — |

## 6. Verdict

`passed`——无 blocking finding。1 条 important（FDR-001，implement 可解，不阻塞用户 review）、2 条 nit、1 条 suggestion。

降级说明：本轮独立 Task agent reviewer 两次均未成功回传（进程静默），**用户已明确授权 local-only 降级**。本报告已对 design 声称的全部关键代码事实逐条核验（Ledger 中 E 级证据）；唯一 H 级判断是 D9 删除 in-use 域生长切法，已记 FDR-001 + 建议 code review 阶段补独立复核。可进入用户整体 review。
