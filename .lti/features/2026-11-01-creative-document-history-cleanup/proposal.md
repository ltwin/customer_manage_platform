---
risk:
  factors: [irreversible, data-migration, concurrency, perf-sensitive]
behavior: changed
reads: [creative/documents, creative/scene]
decisionsPending: false
---
# 文档修订历史清理

> **暂缓**（owner 2026-11-01）：从 Epic `scene-editor-3d` 移出（原子项 C7 `scene-history-cleanup`，已放弃，背景见其 proposal），上线前或媒体额度吃紧时再开工。保留规则已由 owner 确定。开工时先按「现状数据」的方法重测，确认数字仍然支持这个规则，再补 design 和 `creative/documents`、`creative/scene` 的规格 delta。

## Why

文档每次保存都存一份完整快照，修订和它引用的模型修订只增不减。当前场景已经不用的模型 GLB，只要还有旧修订引用，媒体 GC 就回收不了，会一直占用账号的媒体额度。

## 现状数据（2026-11-01，开发库）

- 共 19 个场景、3552 个修订，大多是测试产生的。修订相关的表约 12 MB：快照 8.9 MB，修订行 2.9 MB，模型引用 0.5 MB。平均每个修订约 3 KB，修订最多的一个场景有 2754 个。
- 模型 GLB 共 82 MB，其中约 39 MB（140 个模型修订）只被旧的场景修订引用。每个账号的媒体额度是 20 GiB。
- 按历史面板的分组只保留每组最后一版，3552 个修订剩 235 个，减少 93%。
- 方法：在只读事务里查询 `creative_document_revisions`、`creative_content_revisions`、`creative_document_content_refs`、`creative_content_objects` 和 `creative_blobs`。分组规则与前端 `groupRevisions` 相同。

## What Changes

- 文档底座（`creativedocument`）新增修订精简：超过 30 天的修订，按历史面板的分组规则只保留每组最后一版的快照。分组规则是：同一摄影师 2 分钟内的连续修订，或同一次助手运行的修订，合为一组；新建和各种恢复各自单独成组。
- 被精简的修订删除快照内容和模型引用，但保留一条精简记录（写入者、运行、时间、改动和删除了哪些单元），修订号不断档。
- 按文档类型开启，先只开场景；策划文档等其他类型以后按需开启。
- 清理在后台定时运行，分批处理、每批一个短事务，不锁文档头。
- 彻底删除的文档留下的快照内容也一并回收。
- 读到已精简的修订时明确报告「已清理」，各个入口按下面的验收标准降级。

## Acceptance Criteria

- WHEN 场景有超过 30 天的修订 THEN 清理后每个历史分组的最后一版仍可预览和恢复；历史面板的分组、标签、物件数与清理前相同。
- WHEN 修订在 30 天之内 THEN 全部保留，不受清理影响。
- WHEN 修订是新建、整份恢复或按单元恢复 THEN 不被精简，因为它们各自单独成组。
- WHEN 某个模型修订只被已精简的修订引用 THEN 它不再计为持有方，媒体 GC 按现有规则回收它的 GLB，账号已用额度相应减少；仍被保留的修订、资产、画布或其他持有方引用的模型修订不受影响。
- WHEN 按修订号读取或恢复一个已精简的修订 THEN 返回明确的「该版本已清理」错误，不返回空内容或别的修订。
- WHEN 对话里超过 30 天的本轮改动清单点「恢复这一处」或「撤销本轮」，或者助手要撤回超过 30 天的运行 THEN 提示「这一轮的改动已超过保留期，请在历史版本里恢复」，不做部分恢复。
- WHEN 助手在 30 天后回到同一个对话 THEN 变更提示和冲突说明照常工作，因为它们读的是该运行写的最后一版，这一版会被保留。
- WHEN 清理与摄影师或助手的写入同时进行 THEN 写入不被阻塞，也不会因此失败；清理期间正在预览的旧版本如果被清理，再次读取时按「已清理」处理。
- WHEN 文档被彻底删除 THEN 它的快照内容在下一次清理时被回收。
- WHEN 清理中途失败或进程重启 THEN 已提交的批次有效，未处理的部分下次继续；不会出现快照已删而引用还在（或反过来）的修订。

## Impact

- 代码：
  - `backend/internal/creativedocument`：读取修订、精简、彻底删除。
  - `backend/internal/creativescene`：本轮改动清单、助手撤回、变更提示的降级处理。
  - `backend/internal/creativereference`：持有方条件。
  - creative-worker 的定时任务。
  - 前端历史面板和本轮改动清单的「已清理」提示。
- 契约：项目 `data` 契约中的文档修订要能表示「已精简」；`api` 的读取修订、恢复、读取运行的场景改动新增「已清理」错误。
- 外部：对象存储（GLB 经媒体 GC 删除）。

## 读取旧修订的入口（2026-11-01 检索，开工前复核）

| 入口 | 读哪一版 | 精简后 |
|---|---|---|
| 历史面板：列表、只读预览、整份恢复（`creativedocument/history.go`、前端 `creative-scene/history.ts`） | 组内最后一版 | 不变 |
| 本轮改动清单、「恢复这一处」「撤销本轮」、助手撤回（`creativescene/run_changes.go`） | 运行中各版及其前一版 | 超过保留期后失效，改为提示 |
| 助手变更提示与冲突说明（`creativescene/agent.go`） | 该会话最后一次写入的那一版 | 保留 |
| 客户端任务：导出（`exports.go`）、曝光测光（`shot_exposure.go`）、烘焙时比对面分配（`faces.go`，读前一版） | 分钟级 | 在保留期内 |
| Cmd+Z（前端撤销栈，只在本次打开期间有效） | 本次打开期间 | 在保留期内 |
| 导出图过时提示（`creativescene/fingerprint.go`） | 不读旧快照，比较渲染指纹 | 不受影响 |

## Risk Notes

- **irreversible**：删除快照后无法恢复。开工前要全面重新检索读取旧修订的代码，因为上表是 2026-11-01 的结果，之后新增的功能可能还有别的读取入口。可以考虑先标记、延迟若干天再删。
- **data-migration**：修订行要能表示「已精简」（新增列或约定），需要迁移和回滚链。
- **concurrency**：清理会与写入、预览、本轮改动的计算同时进行，判断一个修订是否已精简，在同一个事务内的结论必须一致。
- **perf-sensitive**：第一次运行要处理全部存量数据，需要分批、限速，避免长事务和大批量删除锁表。
