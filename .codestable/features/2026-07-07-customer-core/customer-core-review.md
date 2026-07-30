---
doc_type: feature-review
feature: 2026-07-07-customer-core
status: passed
reviewer: subagent+ocr
reviewed: 2026-07-07
round: 2
---

# customer-core 代码审查报告

> Round 2（review-fix 复审）：round 1 的 3 条 important（REV-001/002/003）已修复并经双环节独立复审确认解决；verdict 改判 **passed**。round 1 完整记录保留在下方正文，round 2 增量见「Round 2 复审记录」节。

## Round 2 复审记录

- 修复内容：
  - REV-003：`scope.go` WithinTx 增加 defer+recover 回滚后 re-panic + `scope_test.go` panic 回滚集成测试
  - REV-001：`scope.go` 新增受控 `QueryPage`（ORDER BY 列名过 identPattern 白名单、方向由 bool 派生、LIMIT/OFFSET 占位符、空 order fail-loud）；`customer.List` 改 DB 侧分页 `created_at DESC, id DESC`，删除内存排序切片；`customer_test.go` 新增 5 条同 created_at 客户翻 3 页无重复无丢行测试
  - REV-002：`customers.go` bindOptionalPageParam 显式 `<1` 返回 400（nil 缺省链路不动）；`customers_test.go` 新增 page=0/page_size=0 断言
- 独立复审（环节 A，native-agent）：逐行核验占位符编号（含 status+channel+q 全开的最坏情况 `$2..$4`+`$5`/`$6` 对齐）、panic/error 回滚路径互斥、pgx Rollback 幂等、nil→默认值链路无回归、新测试真实有效（去掉 id 二级键或回滚会真失败）；`go vet` clean、无调试残留、无扩范围。**3 条逐一 RESOLVED，0 blocking 0 important**
- OCR（环节 B，round 2）：9 条 Medium，逐条核验后无一动摇修复正确性——新增有效 nit 3 条（见下），其余为 round 1 已记录项的重复或无触发面项（notes 渲染、金额格式化均属 order/notes 未实现域）
- 命令验证：`go build ./...`、`go vet ./...` 通过；`go test` store/customer/httpapi 三包全绿（含三组新测试）
- Round 2 新增 nit（不阻塞，随后续 feature 处理）：
  - REV-012 `scope.go:84-96` Rollback 复用请求 ctx，ctx 已取消时回滚可能失败（pgx 连接销毁兜底）；可用 `context.WithoutCancel` + 短超时加固（来源：ocr）
  - REV-013 `frontend/src/api/client.ts:74-75` `if (params.page)` truthiness 判断会吞掉显式 0；前端自身不传 0，无实际触发面（来源：ocr）
  - REV-014 `customer.go:422` 域层 `Page==0` 作「未设置」哨兵与 HTTP 层 400 语义的分工是有意为之但未注释，建议加一行注释防止后人「统一」时弱化契约（来源：native-agent suggestion）

## 1. Scope And Inputs

- Design: `.codestable/features/2026-07-07-customer-core/customer-core-design.md`（`status: approved`，checklist steps 全 done）
- Checklist: `.codestable/features/2026-07-07-customer-core/customer-core-checklist.yaml`
- Evidence pack: none（非 goal/gate 模式；`evidence/` 目录含 browser-evidence.json 与 6 张截图）
- Gate results: none
- DoD results: none
- Implementation evidence: `evidence/browser-evidence.json` + 截图（A11/A12 自报证据）；实现汇报在对话历史
- Diff basis: `develop` 分支工作区未提交改动，25 个已跟踪文件修改 + 8 个 untracked 新增（backend/internal/customer/、httpapi/customers*.go、migrations/0002、前端 CustomerNewPage/customerLabels），约 +1151/-391 行（round 2 追加 scope.go/customer.go/customers.go 三处修复 + 配套测试）
- Baseline dirty files: none——roadmap items.yaml / roadmap.md / README 的修改均可归因于本 feature 的状态回写与契约同步（design 交付物清单）；`.codestable/features/2026-07-07-customer-core/` untracked 为 spec 产物，不是代码审查对象

### Independent Review

- Detection: 无 `mcp__paseo__create_agent`（Paseo 不可用）；宿主原生 Task agent 可用；`ocr` CLI 可用（`ocr llm test` 通过，provider krill-ai-codex / gpt-5.5）
- 环节 A 独立隔离 Task agent: `native-agent`（code-reviewer-pro，独立上下文，只给原始材料）+ `completed`（round 1 与 round 2 各一轮）。同宿主降级说明：Paseo 缺失，无法跨 provider 异构，独立上下文已达成
- 环节 B OCR CLI: `completed`（round 1 与 round 2 各一轮；裸跑条件核验通过——`git status --short` 全部非 ignored 路径属本轮 scope）
- OCR severity mapping: High→blocking/important, Medium→nit/suggestion, Low→discarded。两轮均无 High；Medium 经本地核验按事实定级（round 1 WithinTx panic 升级 important；round 2 无升级）；Low 丢弃
- Merge policy: 三路（native-agent / ocr / local）结果已逐条本地事实核验后去重合并；每条 finding 标注来源
- Gate effect: none——两轮均在全部环节返回后定稿

## 2. Diff Summary

- 新增：`backend/internal/customer/{customer.go,customer_test.go}`、`backend/internal/platform/httpapi/{customers.go,customers_test.go}`、`backend/internal/platform/store/migrations/0002_customers.{up,down}.sql`、`frontend/src/pages/{CustomerNewPage.tsx,customerLabels.ts}`
- 修改：`api/openapi.yaml`（§7 契约收编）、`backend/oapi-codegen.yaml`（+customer-core tag）、`api.gen.go`（+393 行生成物）、`router.go`/`auth.go`/`main.go`（依赖装配与三条受保护路由）、`scope.go`（WithinTx/Count/Exists/InsertReturningID + round 2 的 panic 回滚与 QueryPage）、scope 双测试 + probe 测试迁移、`frontend/src/{App.tsx,api/client.ts,api/schema.d.ts,index.css,pages/CustomersPage.tsx,pages/CustomerDetailPage.tsx}`、README×2、roadmap 状态回写
- 删除：none（CustomersPage/CustomerDetailPage 从原型 store 改接真实 API，净删行来自原型逻辑移除）
- 未跟踪 / staged：untracked 见上；无 staged
- 风险热点：账号隔离基座扩展（scope.go）、创建事务、列表搜索与分页 SQL、referral 可见性、公共 API 契约、跨模块装配

## 3. Adversarial Pass

- 假设的生产 bug：账号隔离或事务回滚在复杂读模型扩展中被绕开
- 主动攻击过的反例：跨账号列表/详情/referrer（双账号测试实证守住）；identities 部分非法时的半成品写入（before/after Count 实证回滚）；`page=0` 边界（round 1 攻破 → round 2 修复并实证）；同 `created_at` 跨页翻页稳定性（round 1 理论攻破 → round 2 DB 侧 `id DESC` 二级键根除并有专项测试）；WithinTx panic 路径（round 1 攻破 → round 2 修复并实证）；QueryPage 占位符编号错位（round 2 主攻方向，最坏情况逐一对齐，未攻破）；生成代码 RegisterHandlers 无鉴权注册（核验未被使用，learning）；`q` ILIKE 通配符（无注入，匹配语义瑕疵，nit）
- 结果：round 1 的 3 条 important 全部修复并复审确认；跨页一致性风险 R1 已根除；其余留 residual risk / QA focus

## 4. Findings

### blocking

- none

### important

- [x] REV-001 `backend/internal/customer/customer.go:217-252` 列表全量加载内存排序分页，索引意图与实现脱节（来源：native-agent + ocr + local 三路一致）——**round 2 已修复并复审确认**：AccountScope 新增受控 QueryPage，List 改 DB 侧 `ORDER BY created_at DESC, id DESC LIMIT/OFFSET`；跨页一致性专项测试绿
- [x] REV-002 `backend/internal/customer/customer.go:431-436` 显式 `page=0`/`page_size=0` 被静默纠偏为 1/20，未按契约 400（来源：native-agent）——**round 2 已修复并复审确认**：bindOptionalPageParam 显式 `<1` 返回 400，nil 缺省链路核验无回归；0 值测试绿
- [x] REV-003 `backend/internal/platform/store/scope.go:72-94` WithinTx 的 fn panic 路径不回滚事务（来源：ocr，核验升级）——**round 2 已修复并复审确认**：defer+recover 回滚后 re-panic，与 error 路径互斥无双重回滚；panic 回滚测试绿

### nit

- [ ] REV-004 `backend/internal/platform/httpapi/customers.go:171-178` `customerMessage` 按首个 `": "` 截断剥错误前缀，是基于字符串形态的隐式耦合；任何校验文案含 `": "` 会被错误截断。建议 `errors.As` 取 `ValidationError.Message`（来源：native-agent）
- [ ] REV-005 `backend/internal/customer/customer.go:372-386` `q` 未转义 ILIKE 通配符 `%`/`_`，`50%` 会匹配非预期结果；走占位符无注入风险，纯匹配语义瑕疵（来源：native-agent）
- [ ] REV-006 `backend/internal/customer/customer.go:180-188` referral 校验只验「同账号存在」，未排除 `status=merged/archived` 介绍人；当前无触发面（merge 未实现），`customer-profile-complete` 落地时需明确介绍人可选状态范围（来源：native-agent）
- [ ] REV-007 `frontend/src/api/client.ts:65-70` `listCustomers` 的 `channel` 参数手写为裸 `string`，未从生成的 `paths` 类型派生，与「API 类型一律引用 schema.d.ts」的规范精神有偏差（来源：ocr）
- [ ] REV-008 `backend/internal/platform/store/testmigrations/0001_probe_items.up.sql:8-14` `probe_item_tags.item_id` 只 FK 到 `probe_items.id`，未复合到 `(account_id, item_id)`，测试夹具允许造出跨账号 tag；业务表 0002 已用复合 FK，probe 表建议对齐（来源：ocr）
- [ ] REV-012 `backend/internal/platform/store/scope.go:84-96` Rollback 复用请求 ctx，ctx 已取消时回滚可能失败（pgx 连接销毁兜底）；可用 `context.WithoutCancel` + 短超时加固（来源：ocr，round 2）
- [ ] REV-013 `frontend/src/api/client.ts:74-75` `if (params.page)` truthiness 会吞掉显式 0；前端自身不传 0，无实际触发面（来源：ocr，round 2）
- [ ] REV-014 `backend/internal/customer/customer.go:422` 域层 `Page==0` 哨兵与 HTTP 层 400 的语义分工未注释，建议加一行防止后人误「统一」（来源：native-agent，round 2）

### suggestion

- [ ] REV-009 `frontend/src/pages/CustomerNewPage.tsx:105-115` 介绍人是裸客户 ID 文本框，真实 owner 记不住 `cus_...`，referral 路径在演示外基本不可用；design 已把关系经营推迟到后续 feature，不算越界，建议后续做搜索选择，本轮至少文案提示从详情页复制 ID（来源：native-agent）
- [ ] REV-010 `frontend/src/pages/CustomersPage.tsx:28` 列表写死 `page:1, pageSize:20` 且无翻页控件，>20 客户 UI 不可达；design 未把翻页 UI 列入 S5/S6 出口信号，最小闭环可接受，建议列入 UI 二期（来源：ocr + native-agent）
- [ ] REV-011 `api/openapi.yaml` `PageSize` 仅 `minimum:1` 无 `maximum`，建议契约加上限并同步实现 clamp；round 2 改 DB 分页后单页大 LIMIT 仍可能造成大分配（来源：ocr + native-agent，两轮均提及）

### learning

- 生成物 `api.gen.go` 的 `RegisterHandlers` 辅助函数会把受保护端点注册为无鉴权路由、且生成的 wrapper 不校验 `page minimum:1`——本项目未使用它（路由由 `NewRouter` 手工挂到 auth group，已 grep 核验零调用），但未来任何人改用 codegen 注册器就会同时打开无鉴权面和绕过手工参数校验。这是 include-tags 切片模式的固有暗礁，值得沉淀到 compound（来源：ocr 两轮均命中，核验后定级 learning）
- OCR 建议的 pg_trgm GIN 索引（Low，已按规则丢弃）在 `q` 搜索变慢时可再评估

### praise

- `scope.go:25-38` `validateIdents` 标识符白名单 + SQL 片段契约注释，把「请求派生字符串误入 SQL」挡在基座，fail-loud 落实到位；round 2 的 QueryPage 延续同一契约（列名白名单、方向 bool 派生、limit/offset 占位符）
- `q` 搜索用 `EXISTS` 子查询而非 JOIN（customer.go:379-384），规避一客户多身份的行翻倍，且子查询内保持 `account_id` 关联——账号隔离细节到位
- 迁移用复合 `UNIQUE(account_id, id)` + 复合外键（0002:13-15,32）把账号隔离做成 DB 层不变量，ADR-001 纵深防御
- 测试质量高：`TestCreateValidationRollback` 用 before/after Count 实证无半成品；referral 四态全覆盖；HTTP 层覆盖 201/200/400/401/404 与 PATCH 仍 404 的范围守护；round 2 三组新测试均被独立复审确认「代码坏掉时会真实失败」

## 5. Test And QA Focus

- QA 必须重点复核：
  1. ~~`page=0`、`page_size=0` 显式传参~~（round 2 已修复并有测试；QA 复跑确认即可）
  2. ~~翻页正确性~~（round 2 已修复并有同 created_at 专项测试；QA 复跑确认即可）
  3. 规模观察：seed 数千客户后 `GET /customers` 的耗时（DB 分页后主要看深分页 OFFSET 成本与 `q` 的 ILIKE 全扫）
  4. `q` 含 `%`/`_` 关键字的搜索结果是否符合产品预期（REV-005）
  5. 500 路径实证：制造 DB 层错误，确认统一 `internal` 封套、不泄内部文本
  6. A11/A12 浏览器证据逐张核对截图（375px 长昵称不溢出、键盘 focus），`browser-evidence.json` 是自报字段不能免检
- Evidence pack residual risks / gate warnings：n/a（非 gate 模式）
- 建议新增或加强的测试：none 新增必需项（round 1 建议的三组测试已随修复落地）
- 不能靠 review 完全确认的点：CMD-001（`make check` 全量）、CMD-002（生成物零漂移）、CMD-004（前端 build）未在 review 中实跑——本地已实跑 `go build`/`go vet`/三包 `go test`（全绿），QA 阶段必须补跑全量四命令并归档输出

## 6. Residual Risk

- R1 跨页翻页一致性：**round 2 已根除**（DB 侧 ORDER BY + id 二级键 + 专项测试）
- R2 `Count` 与 `QueryPage` 非同一快照，并发插入时 total 与 items 瞬时不一致；可接受的读一致性弱化，记录即可
- R3 详情页 referrer 缺失会放大为整单 404（customer.go 详情路径）：当前复合 FK 兜底不可触发；order/merge 落地后必须回归此路径
- R4 design review 状态债：`customer-core-design-review.md` 为 `status: stale`（identities[1..N] 变更后 round 2 design review 未重跑），design.md 本身 `approved` 且有 owner 2026-07-07 口头确认（worktree-override.md）；acceptance 时记录该豁免
- R5 CMD-001/002/004 全量命令未在 review 阶段实跑，QA 必须实跑归档
- R6 深分页 OFFSET 成本：大 Page 值时 Postgres scan-and-discard；当前规模无虞，列表增长后再评估 keyset 分页（来源：round 2 native-agent）

## 7. Verdict

- Status: **passed**
- Round 1 的 3 条 important 已全部修复，round 2 双环节（native-agent + ocr）独立复审逐条确认 RESOLVED，无新增 blocking/important；nit/suggestion 均不阻塞，已记录在案随后续 feature 处理
- Next: 进入 `cs-feat-qa`（QA 需实跑 CMD-001~004 全量命令并覆盖上节 focus 项）
