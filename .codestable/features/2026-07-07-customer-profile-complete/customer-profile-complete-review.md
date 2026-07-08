---
doc_type: feature-review
feature: 2026-07-07-customer-profile-complete
status: passed
reviewer: subagent+ocr
reviewed: 2026-07-08
round: 2
---

# customer-profile-complete 代码审查报告

## 1. Scope And Inputs

- Design: `.codestable/features/2026-07-07-customer-profile-complete/customer-profile-complete-design.md`（approved）
- Checklist: `.codestable/features/2026-07-07-customer-profile-complete/customer-profile-complete-checklist.yaml`（9 steps 全 done）
- Evidence pack: none（对话内实现证据 + step 证据）
- Gate results: commit gate round 1 预跑 P1（缺本报告）已随落盘解除；P2「staged 跨 bucket」提交时拆分
- DoD results: CMD-001~004 全绿；review-fix 后 `make check` 复跑 exit 0
- Implementation evidence: 实现完成汇报 + 浏览器验证（A13/A14/A15）+ review-fix 汇报（REV-001 含变异校验证据）
- Diff basis: round 1 全量 feature diff + round 2 修复增量（repository.go 行锁对齐、merge_concurrent_test.go、NotesPanel/QuickNote 守护）
- Baseline dirty files: `.codestable/features/2026-07-08-identity-delete-button/` 及对应 `IdentitySection.tsx` class / `index.css` 样式（owner 手工改动，另一 unit），见 R-2

### Independent Review

- Detection: 无 Paseo；原生 Claude Task agent 可用；`ocr` CLI 可用
- 环节 A 独立隔离 Task agent: native-agent + completed ×2 轮（round 1 全量对抗式；round 2 聚焦修复增量的对抗式复审——攻击事务内锁定位、错误语义保持、并发测试假阴性窗口、前端守护竞态、IME 兼容性，全部通过）
- 环节 B OCR CLI: completed ×2 轮（round 2 报 1 Low 丢弃 + 1 Medium 属 customer-core 既有代码非本轮范围，转顺手发现）
- OCR severity mapping: High→blocking/important, Medium→nit/suggestion, Low→discarded
- Merge policy: 全部环节结果逐条本地核验后合并
- Gate effect: none

## 2. Diff Summary

同 round 1（新增 customer 域三文件 + 5 测试文件、customers_profile.go、迁移 0003、前端 5 组件与两页改造、契约与生成物、scope.go QueryRowForUpdate），round 2 增量：

- `backend/internal/customer/repository.go`：新增 `findCustomerForUpdate` helper；Update / requireWritableCustomer 改锁读；DeleteIdentity / Merge 内联锁读收敛到同一 helper
- `backend/internal/customer/merge_concurrent_test.go`：新增 merge×Update 并发守护用例（8 轮）
- `frontend/src/components/customers/{NotesPanel,QuickNote}.tsx`：in-flight 守护 + `!isComposing` 条件

## 3. Adversarial Pass

- round 1：merge×写路径 TOCTOU 命中（REV-001）；重复提交命中（REV-002）；IME 命中（REV-003）；占位符 / 跨账号 / 死锁 / 回滚假阳性 / buildPatch 语义全部未命中
- round 2（攻击修复本身）：① 5 处 `findCustomerForUpdate` 调用点全部在 WithinTx 内，无事务外 FOR UPDATE；② 404/409/merge_conflict 错误语义与修复前逐点一致；③ 并发测试双分支断言（nil→status active / ErrCustomerMerged→零写入）无可构造的假阴性路径，且实现者已做去锁变异校验（3 次运行稳定失败于 TOCTOU 断言）；④ React 19 discrete 事件同步 flush 下 saving 守护对真人输入成立；⑤ `isComposing` 为现代浏览器标准行为，textarea 保留 Shift+Enter 换行。全部通过。

## 4. Findings

### blocking

- none

### important

- [x] REV-001 merged 只读守护无行锁 TOCTOU —— **resolved（round 2 核实）**：`findCustomerForUpdate` 落地、三条写路径与 Merge/DeleteIdentity 锁面对齐；并发用例 PASS + 变异校验闭环
- [x] REV-002 备注提交无 in-flight 守护 —— **resolved**：两组件 `submit()` 首行 `if (saving) return`
- [x] REV-003 IME 组合态 Enter 误提交 —— **resolved**：两处 Enter 分支补 `!event.nativeEvent.isComposing`

### nit

- [ ] REV-004 `frontend/src/components/customers/*.tsx` `customer.id ?? ''` 防御噪声（owner 裁量，未动）
- [ ] REV-005 PATCH 非 nullable 字段显式 null 被静默忽略（契约外输入宽容处理，未动）
- [ ] REV-006 deleteCustomerIdentity 未声明 400（实际不可达，与 design 矩阵一致，未动）
- [ ] REV-008 并发用例仅覆盖 Update 路径；AddIdentity/AddNote 走同一锁机制具代表性，可接受（round 2 新增，非必须）

### suggestion

- [ ] REV-007 merged 敏感写的 UPDATE WHERE 可加 `status <> 'merged'` 纵深谓词（未动，见 R-3）

### learning

- 「同一 diff 内的并发防护要横向对齐」：按不变量（而非按端点）逐条核对锁面才完整
- OCR Low 级 finding 按目标用户群重估：IME 组合态是中文产品的真实高频路径
- 并发修复的测试要配变异校验（去掉修复应稳定失败），否则时序型用例可能是假阳性

### praise

- 锁面约束写进 helper 注释契约（「只应在 WithinTx 内调用」）
- 并发用例双分支断言精准区分合法交错与 TOCTOU，配合变异校验形成闭环证据
- `QueryRowForUpdate` 复用 scope 基座，账号隔离与占位符纪律零破坏
- merge 事务固定锁序、先重定向后清自指、迁移面三表无越界；注入约束的真回滚断言
- null 三态全程 codegen 生成类型透传（D9），跨账号隔离双层测试覆盖

## 5. Test And QA Focus

- QA 必须重点复核：① buildPatch 差异语义四边界（只改 real_name 不带 channel/referrer；referral→douyin 仅带 channel；换介绍人带双字段；清空发 null）；② birthday 边界（02-29 合法、02-30/13-45 400）；③ `{status:merged}` 恒 400；④ archived 可编辑+恢复；⑤ 3+ 客户指向 source 的规模化 referrer 重定向；⑥ IME 修复真机中文输入复核（浏览器行为 review 只能静态确认）
- Evidence pack residual risks / gate warnings：commit gate P2 staged 跨 bucket → 提交时按契约/生成物/代码拆分
- 建议新增或加强的测试：Merge×AddNote 并发用例（REV-008，非必须）
- 不能靠 review 完全确认的点：IME 行为依赖浏览器/输入法组合；并发窗口的生产触发频率

## 6. Residual Risk

- R-1 前端 saving 守护是渲染闭包状态非 ref：真人输入安全（React 19 discrete 事件同步 flush），仅程序化同 tick 双 dispatch 可绕过；生产可忽略
- R-2 工作树混入范围外 unit `2026-07-08-identity-delete-button`（spec 目录 + IdentitySection class + index.css 样式）：**提交时必须拆为独立 commit**
- R-3 `requireActiveReferrer` 的介绍人 active 校验仍为无锁 Exists，与并发 merge/归档介绍人有窄 TOCTOU（既有行为，非本轮引入）；与 REV-007 同族——compound 候选「AccountScope 写路径的锁面约定」，留 acceptance 评估
- R-4 `page_size` 契约无 maximum、服务端不设上限（OCR round 2 发现，customer-core 既有代码）——顺手发现记后续 issue，不在本 feature 修
- R-5 独立 reviewer 与主 agent 同为 Claude 系（非异构）；notes 无分页为 design 已接受契约

## 7. Verdict

- Status: passed
- Next: 进入 `cs-feat-qa`（QA focus 见第 5 节）；nit/suggestion 由 owner 裁量；提交时按 R-2 拆分范围外 unit
