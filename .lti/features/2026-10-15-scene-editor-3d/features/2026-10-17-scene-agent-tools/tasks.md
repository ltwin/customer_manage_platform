# Tasks

## 1. 客户端任务（后端）

- [x] 1.1 迁移 `0075_creative_scene_jobs` up/down：
  - up：新表 `creative_scene_jobs`（列、CHECK、索引按契约，另加唯一 `(run_id, tool_call_id)`）；`AFTER INSERT` 与 `AFTER UPDATE ... WHEN (OLD.state IS DISTINCT FROM NEW.state AND NEW.state <> 'claimed')` 两个触发器，执行现有的 `notify_creative_canvas_changed('account')`；`creative_agent_runs.state` 的 CHECK 加 `waiting_client`，重建活跃索引。
  - down：存在 `waiting_client` 运行时拒绝。
  - 迁移测试 `creative_scene_jobs_migration_test.go`：新回滚链头 `rollbackSceneJobs`，`rollbackModelContent` 先调用它；各迁移测试的回滚标签列表最前面加新标签。
  - 完成判据：up、down、有 `waiting_client` 时拒绝回滚三种情况通过；认领、续租不发通知，新建和终态发通知。
- [x] 1.2 存储辅助：`store.TxAccountScope.Savepoint(ctx, name, fn)`；`ClaimCreativeSceneJob`（`FOR UPDATE SKIP LOCKED`，账号级与按场景两种，先例 `assignment_quarantine_claim.go`）。先写测试：保存点内出错只回滚保存点；两个事务并发认领拿到不同任务；没有任务时不加锁返回空。
- [x] 1.3 `creativescene/jobs.go`：`Job`、`JobResult`、`JobWaiter` 与 `SetJobWaiter`、`CreateBakeJob` / `CreateRenderJob`（同 `(run_id, tool_call_id)` 返回已有任务）、`Claim`（`kinds` 过滤）、`Lease`（不超过 deadline）、`ListActive`（不含代码与令牌）、`CancelRunJobs`、`ErrJobLeaseLost`。先写测试：
  - 屏障测试 20 轮：两个会话同时认领，只有一个拿到；
  - 旧令牌续租返回 `ErrJobLeaseLost`；
  - 列表不泄露代码；
  - 跨账号返回不存在。
- [x] 1.4 完成任务：
  - `creativedocument.CommandInTx`（包装 `decodeCommand` + `commandInTx`，沿用 `CommandAs` 的写入者检查）；
  - `creativescene.Complete`，按 design「完成任务的事务与锁序」：
    - `Admit` 复查失败时走 `Abort`；
    - `bake_model` 由任务代码与 `module` 组装 `generator`，在保存点内执行场景命令；
    - 冲突结果记 `conflict` 并带烘焙产物，校验失败记 `invalid_result`，都返回 200；
    - 暂存上传不可用时整体回滚，返回 409；
    - `render_view` 校验 `scene_revision ≤` 当前修订、不消费候选。

  用假的 `JobWaiter` 先写测试，覆盖上面每个分支，以及幂等重放。
- [x] 1.5 `creativescene/jobs_sweep.go`：租约回收（第 3 次转 `failed`、`host_error`）、deadline 过期（经端口；复查失败走 `Abort`）、删除终态满 30 天的任务；creative-worker 新增 15 秒一轮的 goroutine。每次状态迁移打结构化日志，每轮清扫打一条汇总计数。先写测试：回收、上限、过期、删除。

## 2. 接口（后端）

- [x] 2.1 `api/openapi.yaml` 增加 5 个接口：认领、续租、完成、列出、运行改动；运行状态枚举加 `waiting_client`；等待原因加 `client_job`、`slot_busy`；创建运行请求加 `scene_context`。然后 `make generate` 并提交两份生成物。`httpapi/creative_scene_jobs.go` 实现处理函数，`creativeError` 映射 `ErrJobLeaseLost`。`httpapi` 测试覆盖：
  - 每个接口的成功路径；
  - 跨账号 404；
  - 错误码 400 / 409 `creative_state_conflict` / 409 `creative_upload_unavailable`；
  - 完成接口的幂等。
  - 实施记录：运行改动接口 `GET /documents/{id}/run-changes` 依赖 3.4 的 `RunChanges`，随 3.4 一起加入 OpenAPI 与处理函数；本任务交付其余 4 个接口、运行状态与等待原因、`scene_context`。生成的前端类型要求运行状态文案覆盖全部状态，所以先在 `agentState.ts` 补了 `waiting_client` 的文案并计入进行中（面板的完整表现在 5.2）。任务不存在时返回 404「任务不存在」（`creativescene.ErrJobNotFound`，仍匹配 `creativedocument.ErrNotFound`）。

## 3. 助手运行与工具（后端）

- [x] 3.1 `waiting_client` 状态：
  - 加状态常量 `RunWaitingClient`；
  - `liveRunStates` 加入它，阻止归档；
  - `SupplementRun` 拒绝它；
  - 新增 `waitForClient`；
  - `SweepRuns` / `sweepRun` 在没有 `prepared` 步骤时取槽位入队，`BusyError` 时 `wait_reason = slot_busy`；
  - `endControlledRun` 调用 `CancelRunJobs`。

  先写测试：补充输入不唤醒；归档被拒；槽位被占时保持等待、释放后清扫恢复；取消时任务同事务取消。
- [x] 3.2 挂起：
  - 工具返回 `errParked`，`callTool` 转成 `tool.StatefulInterrupt`，步骤保持 `prepared`；
  - `batch.dispatch` 遇到挂起不停止整批；
  - `drainAgent` 收集 interrupt 里的任务 ID；
  - `completeAttempt` 按任务 ID 分支：全部终态则直接入队，否则 `waitForClient`。

  先写测试：并行两个挂起；屏障测试卡在「建任务提交」与 `completeAttempt` 之间完成任务，运行不进 `waiting_input`、只恢复一次；崩溃重跑不重复建任务。
  - 实施记录：interrupt 的 info 用字符串 `client_job:<任务 ID>`，检查点是 gob 序列化，字符串不需要注册类型；`drainAgent` 返回 `parkedRun`（`errors.Is` 仍是 `errRunInterrupted`）。`callTool` 写结果的事务体抽成 `publishResultInTx`（行为不变），3.3 的 `Settle` 复用它，保证与工具成功分支同一顺序；本任务的测试也用它模拟任务完成。并行挂起测试用两个写同一资源的调用，第二个排在第一个挂起之后，能区分「挂起停批」的回归。
- [x] 3.3 `JobWaiter` 实现（`creativeagent/scene_waiter.go`）：
  - 从 `admitRunInTx` 抽出 `admitParkedRunInTx`，`Admit` 与它共用，锁序为项目 → 槽位 → 网关组 → 运行 → Skill → consent；
  - `Settle` 按 `callTool` 成功分支的顺序写步骤结果，取到槽位后入队；
  - `Abort` 结束运行；
  - server 与 creative-worker 启动时注册。

  先写测试：用测试渲染端完成任务后运行恢复并读到结果；等待期间撤回 consent 或停用 Skill 时，完成返回 409、运行结束、场景无新修订；过期路径同理。
  - 实施记录：`Admit` 也接受 `running` 与 `queued` 的运行——前者是挂起的那次执行还没退出（B2），后者是崩溃后重跑还没走到这个调用；两种情况 `Settle` 只写结果，由执行或日志回放接上。`Abort` 只结束 `waiting_client` 的运行，其余交给持有它的执行，后者自己的准入会遇到同样的拒绝。`admitRunInTx` 的 Skill 与 consent 复查改为调用 `admitParkedRunInTx`，所以已有的 Skill 停用测试同样覆盖这段；本任务的测试覆盖 consent 撤回（完成与过期两条路径）。工具结果先给基础形态（成功、失败、冲突带 `retry_job_id`、过期），`changes_since` 与尺寸、三角面等字段随 3.4、3.6 补上。测试用渲染端上报失败的完成，截图成功与烘焙的整条链路在 3.6 的搭景全流程里测。
- [x] 3.4 `creativescene.RunChanges` 与 `ChangesSince`，以及 `GET /documents/{id}/run-changes` 的领域实现。表驱动测试先写：
  - `RunChanges`：新增、删除、修改、别人改过、整份恢复过、需要分批、层级条目不可恢复；
  - `ChangesSince`：手动修改、整份恢复、按单元恢复、从未写过。
  - 实施记录：
    - 读取层放在 `creativedocument`（`History`、`RunRevisions`、`LastRunRevision`、`ReadAtInTx` / `ReadAtInSnapshot`），计算放在 `creativescene/run_changes.go` 与 `agent.go`；`ChangesAfterInTx` 给冲突结果用（旧基线到现在、排除本运行）。
    - 运行改动接口加入 OpenAPI 与处理函数；运行是否属于本账号由新增的 `creativeagent.RequireRun` 判断（不需要服务实例），不存在返回 404。
    - 与契约的出入，需 owner 知悉：①`blocked.reason` 按设计 H4 多一个 `hierarchy_not_restorable`（契约表只列了两种）；C3 的 `edit_scene` 不开放 `reparent_objects`，正常不会出现。②同一物件的两个单元来源修订不同、且中间有别人写过时，设计写「按 pre 分批」，但契约里条目的 `restore` 是单个批次；实现按保守处理，记为 `changed_by_others`（指向中间那次修订），不单独恢复、也不进 `undo_all`，摄影师可用历史版本。③运行之后的历史超过 5000 条时同样返回 400 `validation_failed`（读取上限）。
    - 文档的第 1 版（新建）没有更早的版本可回，不计入本轮改动。
- [x] 3.5 运行输入：`createRun` 接收 `scene_context` 并冻结；运行创建时计算变更摘要（编辑器上下文里的场景加上本会话最近写过的至多 3 个场景）并冻结；`assemble` 渲染编辑器上下文与摘要。先写测试：输入冻结后，恢复时模型请求哈希一致；画布视图发起的运行不带上下文。
  - 实施记录：
    - 冻结为 `creative_run_inputs` 的 `selection` 行，`source_revision_snapshot.kind` 区分 `scene_context` 与 `scene_changes`；画布概览行没有快照，`assemble` 据此区分渲染。沿用已有角色，不加迁移。
    - 场景属于画布元数据，与画布概览行一样只在 consent 含 `canvas_metadata` 时写入（面板的授权默认带它）。场景行的字节计入历史预算，和提交内容一样从历史的份额里扣。
    - `scene_context` 的场景不存在、属于别的账号或在回收站里时返回 404；选中的物件已不在场景里的去掉，重复的去重；`document_id` 为空、选中超过 50 个或有空 ID 时返回 400。
    - 「本会话最近写过」按本会话各次运行写入修订的时间取最近 3 个场景（`creativedocument.RunDocuments`，回收站里的不算）；编辑中的场景另算，所以最多 4 段摘要。没有别人改过的场景不写摘要。
    - 测试说明：从检查点恢复时消息取自检查点，不重新组装，所以哈希一致由检查点保证。本任务测的是冻结本身：建运行之后场景继续被改，每次组装的请求都相同，且写的是建运行时的版本。另有两处变异检查：去掉 `canvas_metadata` 判断、放宽最近场景的上限，测试都会失败。
    - Skill 资源恢复运行（`RetryRun`）不带场景行。
- [ ] 3.6 4 个场景工具（`creativeagent/scene_tools.go`、`creativescene/agent.go`），在 `tool_registry.go` 登记，资源键 `scene:<doc>`：
  - `read_scene`：概要上限 300 个物件，`include_code` 至多 3 个，附变更摘要；
  - `edit_scene`：基线规则；拒绝 `reparent_objects`、`commit_model`、整份 `restore`；撤回操作 `revert_run`；
  - `write_model`：建任务并挂起；只改参数时代码沿用原修订；`retry_job_id` 的约束；
  - `render_view`：`allowed` 按看图能力；`bindMessages`、`toolContentRevisions`、`admitDispatchInTx` 登记它。

  用脚本化的假模型先写测试：
  - 搭景全流程（写模型 → 测试渲染端完成 → 恢复 → 改场景 → 截图进入下一步图片输入）；
  - 编译失败自修；
  - 基线反例（B1）；
  - 冲突后 `retry_job_id` 直接写入；并行重试同一任务时第二个失败；
  - 无看图能力时没有 `render_view`；
  - 截图被回收后降级为文字。
- [ ] 3.7 画布上新建场景：`canvas_tools.go:nodeTypes` 加 `scene`，结果带 `document_id`；`creativecanvas` 的 `attachDocument` 写入者取画布命令的执行者（未决 #4 在本任务开始时解决）。先写测试：助手建出场景节点和文档，修订写入者为该运行；摄影师新建照旧。

## 4. 渲染端（前端）

- [ ] 4.1 执行器：
  - 从 `modelEdits.ts` 抽出 `host/bake.ts`，`ModelEdits` 改用它；
  - `host/jobs.ts`；
  - `host/executor.ts`：认领、续租、执行 `bake_model`、完成，`host_error` 等失败映射，`creative_upload_unavailable` 重传一次；
  - `host/ticker.ts`；
  - `uploadGenerated` 可注入等待函数。

  单测：失败映射、重传、租约丢失时丢弃产物；C2 现有单测与 e2e 不变。
- [ ] 4.2 离屏截图：先做最小样例，确认 R3F `createRoot` 离屏渲染可行（未决 #2），不行就为截图写独立的 three 场景构建。然后：
  - 导出 `Environment`，`ModelMesh` 同步读缓存（`modelCache.peek`）；
  - `host/render.ts`：四种取景、JPEG、`scene_revision`、用后释放 WebGL 上下文。

  单测：四种取景的相机参数；e2e：截图长边 1024。
- [ ] 4.3 画布页接入：
  - `host/launcher.ts`：每页唯一的认领循环、能力探测、优先场景、按需加载执行器、加载期间续租；
  - `WorkspacePage` 挂载 launcher；
  - `useCanvasSnapshot` 增加 `keepAlive`；
  - `host/leaveGuard.ts`：常驻提示、BroadcastChannel、`beforeunload`、「已继续助手的任务」。

  `creative-scene-agent.e2e.mjs`（mock 接口）：只开画布页时执行器被按需加载并完成任务，加载前没有 three；关闭拦截只在唯一标签页生效；能力探测不满足时不认领。

## 5. 编辑视图与助手面板（前端）

- [ ] 5.1 编辑视图：
  - 向 launcher 登记本场景优先；
  - `api: 列出场景客户端任务` 与最近修订（`readRevisions` 支持 `run_id` / `since_revision`），驱动高亮描边（`look.ts` 第三种颜色）、烘焙占位、状态栏进度和软提示；
  - `creative-scene/context.ts` 共享选中。

  `creative-scene.e2e.mjs` 增加：高亮出现与消失、软提示不阻止编辑、占位、编辑视图里认领本场景任务。
- [ ] 5.2 助手面板：
  - `waiting_client` / `slot_busy` 文案；
  - `render_view` 截图缩略图（媒体票据，未决 #3 在本任务开始时验证）；
  - `SceneRunChanges`：恢复这一处、撤销本轮，分批遇冲突时显示部分成功并重读；
  - 编辑视图里创建运行带 `scene_context`。

  e2e：截图显示；恢复这一处、撤销本轮各 1 个修订；部分成功；请求带选中物件。

## 6. 收尾

- [ ] 6.1 文档：`docs/dev/agent-skills-tools.md` 排空清单加 `waiting_client`、登记 4 个工具；新增 `docs/dev/creative-scene-agent.md`（客户端任务、渲染端、调试与日志字段）。
- [ ] 6.2 实测：
  - AC-12：`creative-scene-agent-latency.e2e.mjs` 经 `TestCreativeEditorBrowser` 测各段 p50 / p95；
  - 后台标签页：用真实 Chrome 隐藏 6 分钟以上，助手照常完成烘焙和截图（未决 #1）；
  - 结论写进 verification。
- [ ] 6.3 交 owner：用真实模型跑 3 个搭景样例各两次（AC-13，保留对话、截图和最终场景），并本机体验画布页搭景、编辑视图点选修改、本轮清单；记录质量结论与遗留问题。
