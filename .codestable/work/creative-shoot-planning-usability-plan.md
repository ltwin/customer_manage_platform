---
kind: dev-plan
epic: ../requirements/creative-shoot-planning.md
related_review: ./creative-shoot-planning-pm-review.md
status: open
disposition: iteration-planning-input
scope: usability-quickwins-prefilter
measurement_window: design-partner 证据采集窗口开启前
---

# 拍摄策划易用性快赢——开发计划

## 0. 目的与前提

本计划承接 PM 评审（`creative-shoot-planning-pm-review.md`）Phase 1 的快赢项，落为可执行、可验收的开发任务。

**时序前提（本计划的关键约束）**：产品当前处于 `draft`、发布闸门 fail-closed、真实验证零样本。下阶段将进入 design partner 小范围测试（圈内朋友体验，单实例多账号即可）。**证据采集窗口开启后，建案漏斗必须冻结**——因此所有触碰建案漏斗的改动必须在窗口开启前落地。现在正是这个窗口。

**范围纪律**：本计划只做「触漏斗 + 零契约 + 低确定性」的快赢项；凡是需要契约扩展（openapi + `make generate`）、或应由测试证据裁决的项，一律明确移出本次范围，见 §3。

## 1. 批次结构

| 批次 | 内容 | 契约 | 是否本次范围 |
|---|---|---|---|
| **A** | U1 规范标签中文化 / U2 术语统一 / U3 到期本地化 / U5 摄取主路径 | 零契约 | ✅ 本次 |
| B | U7a 列表翻页 UI | 零契约 | ⏳ 紧随（可选） |
| C | U7b 关键词搜索 + 排序 | 加性契约 | ⏳ 测试期并行，本计划不含 |

---

## 2. 批次 A：四项快赢

### A1 · U1 规范标签值级中文化

**目标**：UI 不再直接显示英文枚举原值（`extreme_closeup`、`high_saturation`、`natural`…），全部显示中文；API 契约与存储值不动。

**做法**：在既有权威源 `frontend/src/planning/ingestionCandidates.ts` 的 `shotTagFields` 上，为每个 value 增加中文显示映射；新建一个导出函数（如 `shotTagLabel(key, value)`）供所有渲染点复用。**单一映射源，禁止各页面各自硬编码。**

**覆盖面（渲染点）**：
- `frontend/src/planning/panels/ShotsPanel.tsx` — 镜头卡 `planning-tag` chip（L88 附近）+ 镜头编辑弹窗 `EnumSelect`（L218-222）
- `frontend/src/planning/ShootPlanRunPage.tsx` — Run Mode 镜头卡 `run-tag` chip（L213 附近）
- `frontend/src/planning/panels/ShotsPanel.tsx` / `ingestionCandidates.ts` — 摄取候选编辑器（如有独立渲染点）
- `frontend/src/planning/share/` — 分享视图若展示规范标签，需同步接入；**实施时先 grep 确认分享侧是否渲染 value**，若渲染则一并覆盖，避免后端值仍英文、前端显示中文、但分享侧漏成英文的割裂。

**验收**：
- 全站 grep 无任何 `.planning-tag` / `.run-tag` / `EnumSelect` 直接输出英文原值；
- 至少一处下拉选择 `extreme_closeup` 后，全站显示为中文「特写」；
- 文案与 `other` 二态保持默认中文；
- 后端/OpenAPI 不产生任何 diff；`npm run test:shoot-planning`、`test:plan-ingestion`、`test:plan-share`、`lint`、`build` 通过。

### A2 · U2 术语统一

**目标**：消除「同一功能三个名字」与中英混排。用户可见文案统一为：

- 「摄取工作台」 → **「从聊天整理」**（页面标题、面包屑、加载文案、错误文案同步）
- 「Run Mode」 → **「现场模式」**

**注意**：
- [Run Mode] 一词在 `frontend/src/planning/` 多个页面出现（已确认 5 处 UI 页面 + `schema.d.ts` 类型名）。**只改用户可见文案**，不改 `capture_mode` 枚举值、路由、类名；`schema.d.ts` 为生成物不动。
- 内部技术语境（如错误日志、注释、代码标识符）不强制改名，避免无谓 diff。

**验收**：
- `frontend/src/planning/` 用户可见字符串不再出现「Run Mode」「摄取工作台」；
- 全站无「从聊天整理」之外的歧义名称；
- 路由 `/ingestions/` 与页面功能不受影响；`lint` / `build` 通过。

### A3 · U3 分享到期时间本地化

**目标**：`ShareCollaborationPanel.tsx` 的到期输入不再要求用户手填 UTC 绝对时间。

**做法**：文件 `frontend/src/planning/share/ShareCollaborationPanel.tsx`：
- L589 标签「绝对到期时间（UTC）」 → 「到期时间（本地时间）」；
- `toDatetimeLocal` / `fromDatetimeLocal`（L794+）从 `getUTC*` / `Date.UTC` 改为本地时间换算；提交时仍以 ISO（UTC）序列化，前后端契约不变；
- 「使用默认到期」「允许范围」的展示值（用 `formatInstant`）保持可读，不做大改（仅确认其展示不含由本地化引入的错觉）。

**验收**：
- 标签不再含「UTC」；输入框值、min、max 均按用户本地时区呈现；
- 提交后保存的 `expires_at` 仍是 UTC ISO 且与用户选择的本地时刻正确对应（含东八区正负校验）；
- `test:plan-share` / `lint` / `build` 通过。

### A4 · U5 摄取提为建案主路径

**目标**：列表页「＋ 新建策划」从单一空白建案，拆为以「从聊天整理」为主的双入口；空态同步引导。

**做法**：`frontend/src/planning/ShootPlansPage.tsx`（L33-44 `createBlank`、L58 附近按钮）：
- 主按钮「**从聊天整理**」：建空白案后直跳 `/shoot-plans/{id}/ingestions/new`（复用 `createBlank()`，其后追加 navigate 到摄取路由）；
- 次级按钮「**空白建案**」：保留现有直接进工作台的路径；
- 空态文案从「可以先新建一份空白策划」改为引导「从聊天整理」，对齐「把聊天记录变成方案」的立项价值主张。

**验收**：
- 列表页两个入口；主入口一步直达摄取页；
- 空态引导指向主入口；
- 摄影师的零成本建案（直接「空白建案」、后续补信息）路径仍保留；
- 工作台顶栏既有「从聊天整理」入口保留，不重复；`test:shoot-planning` / `lint` / `build` 通过。

---

## 3. 明确移出本次范围（非本次开发项）

| 项 | 归属 | 理由 |
|---|---|---|
| U4 保存反馈（每卡保存 + 全局 toast） | 观察项 | toast 体系是原型对齐既定决策（GAP-UX-02 刚统一），无证据不翻案；留给 design partner 真实反馈裁决 |
| U6 一次性密钥强确认、认领提醒位置、CRM 搜索 | 观察项 | 安全性是有意代价；不预支 |
| U8 Run Mode 内查看结构 | 观察项 | 现状已展示场景/动作/表情/构图/打光 + 五个规范标签 + 参考素材；真实余缺（如备注、准备项）待现场用过再定 |
| U7b 搜索 + 排序 | 批次 C（测试期并行） | 需 openapi 加性扩展 + `make generate`，走契约先行（roadmap §4 同步），且该页不在 G2 漏斗内，可测试期并行 |
| B U7a 列表翻页 UI | 批次 B（紧随） | 后端分页现成，纯前端小活；本计划先聚焦 A |

---

## 4. 执行约定（开工前须确认）

1. **分支**：当前工作区在 `feat/oss-object-storage`，含未提交 OSS 改动。批次 A 建议从 `develop` 另开分支（如 `feat/planning-usability-quickwins`），不与 OSS 改动混入；是否用 worktree 由 owner 开工时定夺。
2. **提交**：单个 feature 一个 commit；提交需 owner 人工同意，不自动 commit；禁止 `--no-verify`。
3. **验证**：分批提交后跑对应前端套件（`test:shoot-planning` / `test:plan-ingestion` / `test:plan-share`）+ `lint` + `build`；不触碰后端/契约，故无 `make generate`。
4. **证据对齐**：本计划所有改动应在 design partner 证据窗口开启前全部落地（尤其 A4，它直接改变 G2 的分母构成——建案主路径）。

## 5. 完成判据（本次范围）

- A1–A4 四项全部落地并通过各自验收项；
- 无任何 OpenAPI / Go / 生成物 diff；
- 前端全套件 + `lint` + `build` 绿；
- 就此回到 owner，交接给批次 B/C 与后续 design partner 日程。
