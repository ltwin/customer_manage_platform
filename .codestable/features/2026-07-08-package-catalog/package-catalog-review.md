---
doc_type: feature-review
feature: 2026-07-08-package-catalog
status: passed
reviewer: subagent+ocr
reviewed: 2026-07-09
round: 6
---

# package-catalog 代码审查报告

## 1. Scope And Inputs

- Design: `.codestable/features/2026-07-08-package-catalog/package-catalog-design.md`
- Checklist: `.codestable/features/2026-07-08-package-catalog/package-catalog-checklist.yaml`
- Evidence pack: none（同目录有浏览器截图证据：`evidence-packages-*.png`）
- Gate results: none
- DoD results: none
- Implementation evidence: 本轮为 review-fix 后最终复审。`REV-018` 到 `REV-024` 已在前轮核验修复；`REV-025` 静默四舍五入改价已修；最终窄复审又发现 `REV-026` 基础价 raw 输入中间态被 `Number()` 吞掉，已修为 raw string draft 并纳入 `make check`。
- Diff basis: `git status --short --untracked-files=all` + `git diff` + `git diff --cached`
- Baseline dirty files: 当前 dirty / untracked 文件均属于 package-catalog 实现、CodeStable 产物、需求 / roadmap 回写或截图证据；生成物 `backend/internal/platform/httpapi/api.gen.go` 与 `frontend/src/api/schema.d.ts` 当前 staged，源码仍 unstaged，提交前需统一 stage。
- Code facts checked: `frontend/src/pages/packagePrice.ts`、`frontend/scripts/package-price.test.ts`、`frontend/src/pages/PackagesPage.tsx`、`frontend/package.json`、`Makefile`、`backend/internal/package/*`、`backend/internal/platform/httpapi/packages.go`、`backend/internal/platform/httpapi/packages_test.go`、`api/openapi.yaml` 与生成物。
- Verification run by main agent: `which ocr && ocr llm test` 通过；`cd frontend && npm run test:package-price` 通过；`cd frontend && npm run build` 通过；`cd frontend && npm run lint` 通过；`make check` 通过（含新增 `npm run test:package-price`）；`git diff --check && git diff --cached --check` 通过；commit gate 通过。

### Independent Review

- Detection: Paseo MCP 工具未暴露；原生 multi-agent Task 可用；`ocr` CLI 可用且 `ocr llm test` 通过。
- 环节 A 独立隔离 Task agent: native-agent + completed（agent `019f44aa-4f56-7d10-af07-0e627b28e94f`；最终窄复核确认 `REV-026` 已解除，价格路径无新的 blocking / important）。
- 环节 B OCR CLI: completed（最终窄复核 22 files reviewed, 1 comment；仅 Low style，按规则丢弃）。
- OCR severity mapping: High->blocking/important, Medium->nit/suggestion，Low->discarded。
- Merge policy: subagent 与 OCR finding 已逐条本地事实核验后合并；`.codestable/` finding 不作为行级代码 finding。
- Gate effect: Task agent 与 OCR 均完成，无 blocking / important，reviewer gate 放行。

## 2. Diff Summary

- 新增：`backend/internal/package/*`、`backend/internal/platform/httpapi/packages.go`、`backend/internal/platform/httpapi/packages_test.go`、`backend/internal/platform/store/migrations/0004_packages.*.sql`、`frontend/src/pages/packagePrice.ts`、`frontend/scripts/package-price.test.ts`、feature design/checklist/review/截图证据、`.codestable/requirements/package-catalog.md`
- 修改：`api/openapi.yaml`、`backend/oapi-codegen.yaml`、server/router/deps/envelope、生成物、`frontend/src/api/client.ts`、`frontend/src/pages/PackagesPage.tsx`、`frontend/src/index.css`、`frontend/package.json`、`Makefile`、roadmap / requirement 文档
- 删除：none
- 未跟踪 / staged：生成物 `backend/internal/platform/httpapi/api.gen.go` 与 `frontend/src/api/schema.d.ts` 当前 staged；多数本 feature 新代码与 CodeStable 产物 untracked
- 风险热点：前端金额输入精度、前端异步列表状态、mutation 后分页请求时序、外部整数输入到分页 / PG `INTEGER` 的边界、PATCH 请求体大小、删除 in-use 域生长、混合 staged/unstaged 提交风险

## 3. Adversarial Pass

- 假设的生产 bug：用户逐字输入基础价小数时，UI 要么悄悄四舍五入、要么吞掉 `680.` 这类中间态，导致改价不可用或落库价与输入价不一致。
- 主动攻击过的反例：`12.345` / `0.004` 是否被拒绝且不保存；`680.` / `680.5` / `680.55` / `0.05` 是否能作为 raw 输入路径保留并正确转分；超 int4 分值是否前端拒绝；新增测试是否进入 `make check`；旧 `orders_count=0` OCR finding 是否违反 design。
- 结果：`REV-025` 已由 `packagePriceYuanToCents` 精确解析、`validatePackagePriceYuan` 和输入拦截解决；`REV-026` 已由 raw string `PriceDraft` + `PriceField` 解决；价格测试进入 `Makefile test`。OCR 的 `orders_count=0` 旧 finding 与 design“order 域未落地前恒为 0”冲突，未采纳；最终 OCR 只剩 Low style，不影响放行。

## 4. Findings

### blocking

- none

### important

- none

### nit

- none

### suggestion

- none

### learning

- 价格输入不要在受控 input 的 `onChange` 阶段用 `Number()` 归一化，否则会吞掉 `680.` 这类合法编辑中间态；应保留 raw string，保存前校验并转换。
- 新增前端纯函数测试后，需要接进 `make check`，否则核心 DoD 命令无法证明回归保护存在。
- `orders_count=0` 是本 feature 明确设计：order 域未落地前保持响应 shape，真实聚合留给 order-tracking 接通；不要把后续域职责提前升级为本轮 blocking。

### praise

- 最终价格路径清晰：`PriceField` 只负责保留用户输入和最多两位小数拦截，`validatePackagePriceYuan` 负责保存前校验，`packagePriceYuanToCents` 负责无 round 转分。
- 后端分层贴合 design：handler 没把 `gin.Context` 下穿 service/repository，套系读写通过 AccountScope，PATCH 在事务内锁行并校验有效 shot range。
- 删除 in-use 不是空桩：无 orders 表返回 0，有表时在同事务中锁 package 后计数并拒绝引用删除，符合“接口先行、order 域后接通”的设计切法。

## 5. Test And QA Focus

- QA 必须重点复核：真实浏览器逐字输入基础价 `680.` → `680.5` → `680.55`、`0.05`，保存请求分别转为正确分值；逐字或粘贴 `12.345`、`0.004` 不能保存且不会静默变成 `12.35` / `0`；active / archived / all 切换时接口失败不展示旧 filter 卡片；archived tab 下新建套系后对象可见；mutation 与 load-more 并发不会混入旧页；删除被 scheduled/cancelled 订单引用均返回 409。
- Evidence pack residual risks / gate warnings：独立 Task agent 与 OCR 本轮均完成，无 blocking / important。
- 建议新增或加强的测试：none（价格 helper 已覆盖边界，并已进入 `make check`；浏览器真实输入路径留 QA 手工确认）。
- 不能靠 review 完全确认的点：真实浏览器移动端 number keyboard、慢网竞态、截图中的键盘 focus 完整路径。

## 6. Residual Risk

- 当前 git 状态混合 staged/unstaged/untracked：`api.gen.go` 与 `schema.d.ts` 已 staged，但 `api/openapi.yaml`、实现源码、新增测试文件和 CodeStable 产物仍 unstaged；提交前必须整体 stage 一致性复核，避免契约来源、生成物或新增 helper/test 文件错位。
- OCR 最终给出一个 Low style：`packagePriceYuanToCents` 里 `!= null` 可换成 `!== null`。该项不影响行为、lint/build/test 均通过，按 OCR Low 规则丢弃，不阻塞 QA。
- 真实价格输入、慢网和移动端行为仍需 QA 阶段用浏览器证据确认。

## 7. Verdict

- Status: passed
- Next: 进入 `cs-feat-qa`。
