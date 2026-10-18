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
- [x] 3.6 4 个场景工具（`creativeagent/scene_tools.go`、`creativescene/agent.go`），在 `tool_registry.go` 登记，资源键 `scene:<doc>`：
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
  - 实施记录：
    - 领域入口在 `creativescene`：`AgentReadInTx`、`AgentEditInTx`（含 `revert_run`）、`CreateBakeJobInTx`、`CreateRenderJobInTx`、`RetryBakeInTx`、`JobConflictInTx`、`BakedModelInTx`；建任务放在 `jobs.go`，重试放在 `jobs_complete.go`（`completeBake` 拆出 `writeBake` 共用）。`creativedocument` 新增 `LockCurrentInTx`（基线检查与命令在同一把锁下）和 `LastRunWriter`（`revert_run` 的 `previous`）。
    - 基线检查：基线之后、不是本运行写的修订所改的单元与本次涉及的单元有交集即冲突（整份恢复算全部单元）。`add_objects` 涉及 `objects` 单元，所以基线之后别人新增或删除过物件时，助手新增物件也会冲突，按设计处理，冲突结果给出新基线。
    - 写入类调用（`edit_scene`、`write_model` 的 `retry_job_id`）走操作回执：操作 ID 由运行与步骤派生，重跑先查回执，命中就原样返回，不会写两次；准入复查作为回执的 Guard，锁序为运行 → Skill → consent → 场景文档。建任务（`write_model`、`render_view`）在同一事务里先 `admitRunInTx`。
    - 冲突结果作为失败的工具结果返回（新增 `structuredRefusal`，步骤记为 failed、结果是冲突的 JSON），与任务冲突经 `Settle` 写回时一致。
    - 场景工具与 3.5 的场景行一样要求 consent 含 `canvas_metadata`，不含时返回「本次对话没有授权发送这类内容」。工具可用性没有按 consent 收窄，与画布写工具一致。
    - 工具结果上限是 64 KB，300 个物件的完整信息就会超出，所以 `read_scene` 带字节预算：先放请求的代码，放不下的列入 `code_omitted`；物件放不下时降为概要，仍放不下就截断并给出 `omitted_objects` 与提示。编辑与撤回结果的列表各最多 200 项，另给总数。
    - 给模型看的修订号改为整数（API 仍是十进制字符串）：模型照抄结果里的值，字符串会让参数校验失败；工具参数两种写法都接受。
    - 给模型看的文字一律用「创作者」（owner 2026-10-14 的中性称呼规则），3.5 的编辑器上下文和变更摘要一并改过；`creativescene` 新增守护测试。
    - 编译错误的行号由渲染端写在 `message` 里（完成接口的错误只有 `code` 与 `message`，契约未变）。
    - `bindMessages` 从 `render_view` 结果取图片，标签「(render_view 截图)」；`toolContentRevisions` 与 `admitDispatchInTx` 的复查名单加入 `render_view`。基础指令（`instructions/base.md`）加「搭建场景」一节。
    - 4 个工具的定义约 7 KB，每次请求都会带上；压缩回归测试的小窗口相应放大（16000→18000、8000→10000），测试意图不变。
- [x] 3.7 画布上新建场景：`canvas_tools.go:nodeTypes` 加 `scene`，结果带 `document_id`；`creativecanvas` 的 `attachDocument` 写入者取画布命令的执行者（未决 #4 在本任务开始时解决）。先写测试：助手建出场景节点和文档，修订写入者为该运行；摄影师新建照旧。
  - 实施记录：
    - 未决 #4 的结论：画布命令层拿不到执行者（命令载荷不带，也不应由客户端带）。按设计的备选方案，`creativecanvas` 新增 `ApplyCommandsAs(actor)`，执行者只从服务端传入；`ApplyCommands` 等于以摄影师执行，原有调用不变。画布工具用助手运行作为执行者。
    - 局部偏差：设计写的是把结果 `created` 的值改成 `{node_id, document_id}`。`created` 现在是「临时名 → 节点 ID」，模型和现有测试都按这个形态使用，所以保留不变，另加 `documents`（临时名 → document_id），只在新建了文档节点时出现。`edit_canvas` 的说明写明新建 scene 节点后用 `documents` 里的 ID 调场景工具。
    - 摄影师新建场景照旧记为摄影师，由已有的 `creativecanvas/document_node_test.go` 覆盖。

## 4. 渲染端（前端）

- [x] 4.1 执行器：
  - 从 `modelEdits.ts` 抽出 `host/bake.ts`，`ModelEdits` 改用它；
  - `host/jobs.ts`；
  - `host/executor.ts`：认领、续租、执行 `bake_model`、完成，`host_error` 等失败映射，`creative_upload_unavailable` 重传一次；
  - `host/ticker.ts`；
  - `uploadGenerated` 可注入等待函数。

  单测：失败映射、重传、租约丢失时丢弃产物；C2 现有单测与 e2e 不变。
  - 实施记录：
    - 分工：认领接口在 `host/jobs.ts`，由 4.3 的 launcher 调用；`SceneExecutor.run(job)` 接手已认领的任务，自己续租、执行、完成。浏览器侧的组装（沙箱、缩略图、上传、Worker 节拍）随 launcher 在 4.3 写。
    - 沙箱页没启动、已关闭时，`CompileError` 带 `host: true`（只有宿主能设，沙箱回传的错误不会带），映射为 `host_error`；编译错误的行列号写进 message（「第 3 行第 7 列：…」）。缩略图和上传失败都算 `host_error`。
    - 结果处理：服务端记下结果（200，成功或失败）就保留产物，冲突的烘焙要靠它重试；续租或完成返回 409 / 404 时丢弃产物；完成遇到网络错误或 5xx 用同一个 operation id 重试 3 次，仍不行就放手、等租约过期回队；成功结果被 400 拒绝时改报 `host_error`，避免反复回队。
    - `SandboxHost` 增加可注入的计时（启动与编译超时），执行器在隐藏标签页里也按 Worker 节拍判断超时。
    - 顺手修正后端 `host_error` 的补充说明：原文「页面多次没能完成」对页面一次上报的失败不准确，改为「问题出在页面上，可以稍后重试」。
    - 验证：`creative-scene-agent.test.ts` 10 项（变异检查 5 处均被抓到：不重传、丢失时不丢弃、忽略租约、重试换 operation id、超时码错映射）；C2 场景单测 99 项、`creative-scene.e2e.mjs` 30 项照常通过。
- [x] 4.2 离屏截图：先做最小样例，确认 R3F `createRoot` 离屏渲染可行（未决 #2），不行就为截图写独立的 three 场景构建。然后：
  - 导出 `Environment`，`ModelMesh` 同步读缓存（`modelCache.peek`）；
  - `host/render.ts`：四种取景、JPEG、`scene_revision`、用后释放 WebGL 上下文。

  单测：四种取景的相机参数；e2e：截图长边 1024。
  - 实施记录：
    - 未决 #2 的结论：可行。R3F 9.8 的 `createRoot` 配不挂到页面的 canvas、`frameloop: 'never'`，手动渲染一帧即可，SwiftShader 下一张 1024 的截图约 0.4 秒。唯一的坑：`<Canvas>` 会自动 `extend(THREE)`，单独的 root 不会，漏了时 `<ambientLight>` 报错、提交永远不来、截图一直等。已补 `extend`，并加错误边界让绘制错误直接失败；执行器另设 60 秒上限，超时报 `host_error`。
    - 局部偏差：
      - 取景计算放在 `host/framing.ts`（纯计算，node 单测可直接测），绘制的组件树放在 `host/ShotScene.tsx`（只导出组件，保持 lint 的 fast-refresh 规则无告警），`host/render.ts` 只负责读场景、预加载模型、出图。这两个文件挂载点里没有列。
      - `SceneObjects` 增加 `helpers` 开关，截图不画灯光和摄像机图标：否则用摄像机截图时，它自己的视锥线会出现在画面里。
    - 取景：摄像机的垂直视角按传感器高度算，与编辑器里画的视锥一致；预设按全部可见物件（不含地面、灯光、摄像机）的世界包围盒取景，内容最多占画面的 1/1.1。
    - 执行器接入 `render_view`：拍不了（摄像机已删）报 `render_error`，页面自己的问题（WebGL、超时、上传）报 `host_error`；截图以 `image/jpeg` 暂存上传，上传不可用时与烘焙一样重传一次。`uploadGenerated` 的声明类型增加 `image/jpeg`。
    - 验证：单测新增取景 4 项、截图任务 2 项、`peek` 1 项（共 17 项）；`creative-scene-agent.e2e.mjs` 3 项：JPEG、长边 1024、`scene_revision`、模型首帧即为几何、四种预设、连拍 20 张不触发浏览器的上下文上限。变异检查：去掉 `peek` 首帧读取、去掉卸载，都会被 e2e 抓到。C2 场景单测与 e2e 30 项照常通过。
- [x] 4.3 画布页接入：
  - `host/launcher.ts`：每页唯一的认领循环、能力探测、优先场景、按需加载执行器、加载期间续租；
  - `WorkspacePage` 挂载 launcher；
  - `useCanvasSnapshot` 增加 `keepAlive`；
  - `host/leaveGuard.ts`：常驻提示、BroadcastChannel、`beforeunload`、「已继续助手的任务」。

  `creative-scene-agent.e2e.mjs`（mock 接口）：只开画布页时执行器被按需加载并完成任务，加载前没有 three；关闭拦截只在唯一标签页生效；能力探测不满足时不认领。
  - 实施记录：
    - 认领时机：挂载、画布失效通知（500 ms 节流）、回到前台、恢复联网；做完一个任务立即再认领；认领遇到网络错误不重试，等下一次触发。认领时带 `kinds: [bake_model, render_view]`。
    - 执行器代码块加载失败时，本页标记为不能执行、不再认领，并把这个任务以 `host_error` 报回（单测抓到过一次：最初失败后循环还会认领下一个任务并同样报错，已修正）。在应用内离开工作区时，进行中的任务做完再释放执行器，不会被打断。
    - `keepAlive`：本页持有任务或助手有进行中的运行（含 `waiting_client`）时，隐藏标签页也不断开实时流；回到前台照常重连并重读。隐藏标签页的行为留给 6.2 用真实 Chrome 验证。
    - 常驻提示放在工作区头部（`cc-scene-host`）：「助手正在用这个页面生成模型 / 截图，关闭页面会中断」；接手中断的任务时第一次显示「已继续助手的任务，关闭页面会中断」。
    - 局部偏差：
      - `host/pageExecutor.ts`（按需加载的代码块：沙箱、缩略图、截图、上传与 Worker 节拍的组装）挂载点里没有列；
      - `launcher.ts` 提供 `useSceneHost(account)`，把启动、触发和关闭拦截收在一起，`WorkspacePage` 只加一个 hook 和提示。
      - e2e 用只挂 `useSceneHost` 的页面代替完整的 `WorkspacePage`（现有 e2e 都不挂完整工作区）；`WorkspacePage` 的模块图仍由 C2 的「打开画布页不加载 three」检查覆盖，现在包含 launcher。
    - 验证：单测新增认领循环 8 项、关闭拦截 1 项（共 26 项）；`creative-scene-agent.e2e.mjs` 新增 3 项（共 6 项）。变异检查：有其他标签页也拦截、去掉能力探测、静态引入执行器，都会被 e2e 抓到。C2 场景 e2e 30 项照常通过；生产构建中执行器是单独的代码块，工作区代码块不引用 three。

## 5. 编辑视图与助手面板（前端）

- [x] 5.1 编辑视图：
  - 向 launcher 登记本场景优先；
  - `api: 列出场景客户端任务` 与最近修订（`readRevisions` 支持 `run_id` / `since_revision`），驱动高亮描边（`look.ts` 第三种颜色）、烘焙占位、状态栏进度和软提示；
  - `creative-scene/context.ts` 共享选中。

  `creative-scene.e2e.mjs` 增加：高亮出现与消失、软提示不阻止编辑、占位、编辑视图里认领本场景任务。
  - 实施记录：
    - 读取与计算放在新文件 `creative-scene/agentActivity.ts`（挂载点里没有列）：任务列表和最近修订的读取、1 秒节流、高亮集合、占位、状态栏文案，都是可在 node 里单测的纯逻辑；`SceneEditor` 只负责接线。
    - 修订读取：打开时读最近 20 条，之后只读 `since_revision` 之后的。「60 秒内」的计时：打开时读到的那一页按服务端的 `created_at`（不晚于本机当前时间），之后新到的按到达时间，避免本机时钟偏差让新修订的高亮一闪而过或不出现。每条到期时由定时器移除，不靠重绘时取时间。
    - 描边颜色 `agentEdge`（#17a397）优先级最低：选中、活动物件的颜色在上面。标签「助手正在修改」放在物件列表的行尾（设计没有指定位置）。历史预览时不显示高亮和占位。
    - 局部偏差：占位没有沿用 `GhostModel`。`GhostModel` 画的是已有的模型修订，助手新增的物件此时还没有模型，所以改为 1 m 的半透明方块，按目标的 `parent_id` 挂在父物件下，用 `transform` 定位。方块用助手色淡染并描边：泥土色地面上的泥土色半透明块几乎看不见。占位不可拾取。
    - 状态栏：编辑器自己的烘焙文案优先；否则显示「助手正在生成「名称」…」，多个时「助手正在生成「名称」等 N 个模型…」；替换已有物件时名称取该物件的名字。
    - 软提示：选中集合变化且含高亮物件时，以及编辑（含调参数、改代码重新生成）涉及高亮物件时显示，不阻止操作；打开编辑器时恢复的上次选中不触发。
    - launcher 由 `WorkspacePage` 以 `host` 传给 `SceneEditor`，编辑器打开期间 `prefer(documentID)`。编辑视图没有另外订阅执行事件：任务列表已含排队和执行中的任务；认领不发通知（按设计），所以两种状态显示一样。
    - `context.ts` 保存打开的场景与选中，编辑器关闭时清除；5.2 的面板读取。
    - 验证：单测新增 2 项（共 28 项）；`creative-scene.e2e.mjs` 新增 3 项（共 33 项）。为此 e2e 的 mock 服务端增加了任务列表、`since_revision` 过滤、助手修订和认领 / 完成接口，测试页面加 `?host=1` 时与 `WorkspacePage` 一样挂 `useSceneHost`。变异检查 5 处，全部被抓到：去掉优先认领、编辑时不提示、不画描边、占位可拾取、占位不跟随父物件。最后一处最初没抓到：顶层的占位本来就拾取不到，所以改为把占位挂在父物件下，并检查视图中心附近有占位的描边。C2 原有 30 项与助手 e2e 6 项照常通过；生产构建中工作区代码块不引用 three。
- [x] 5.2 助手面板：
  - `waiting_client` / `slot_busy` 文案；
  - `render_view` 截图缩略图（媒体票据，未决 #3 在本任务开始时验证）；
  - `SceneRunChanges`：恢复这一处、撤销本轮，分批遇冲突时显示部分成功并重读；
  - 编辑视图里创建运行带 `scene_context`。

  e2e：截图显示；恢复这一处、撤销本轮各 1 个修订；部分成功；请求带选中物件。
  - 实施记录：
    - 未决 #3 的结论：可以。暂存的截图修订通过 `RequireUsable(display)`，展示票据照常签发。截图长边 1024，小于生成展示图的阈值，只有 `original` 角色，所以面板签 `original` 的展示票据。`creativemedia` 的 `TestModelThumbnailTicketAndGC` 加了一条断言守住这一点。
    - 局部偏差：截图和本轮改动清单放在新文件 `AgentScene.tsx`（组件）与 `agentScene.ts`（读详情、解析结果、按顺序提交、文案），`AgentBlocks.tsx` 只挂载它们。挂载点里没有列这两个文件。
    - 状态：运行在 `waiting_client` 时，挂起的工具行还开着，状态行原本会显示工具名；现在优先显示等待原因：`client_job` 为「等待页面执行」，`slot_busy` 为「等待其他助手运行结束」。
    - 截图：`render_view` 成功后，从工具详情的结果取 `content_revision_id` 显示缩略图，点开是全屏浮层，Esc 或点击关闭。运行结束后工具行和其他工具结果一样折叠在分段里。暂存上传过期或票据失败时显示「截图已不可用」。
    - 本轮改动：场景集合取自 `edit_scene` / `write_model` 的参数（工具详情每个块读一次并缓存，读失败下次重试），运行写过修订的场景各显示一份清单，标题取画布上对应节点的名字。
      - 「恢复这一处」提交该条的 `restore`；「撤销本轮」按顺序提交 `undo_all`，遇到失败就停下，后面的批次不再发。
      - 冲突时提示「撤销了一部分：其余的地方刚被修改，没有覆盖，清单已重新读取」，每次提交后都重读清单。
      - 提交走通用命令接口，账号经 `AgentTurnContext.account` 传入。
      - 清单自己恢复出的修订会让该条显示为别人改过，这种情况改显示「已恢复」。
    - `scene_context`：编辑器只在场景可编辑时写上下文；场景在回收站或已删除时不写，因为后端会以 404 拒绝创建运行。选中物件截到 50 个。上下文在点发送时冻结进待确认链，重放沿用。
    - 验证：
      - 单测新增 4 项，共 32 项：分批提交与部分成功、详情读取与缓存、等待文案、`scene_context`。
      - `creative-scene-agent.e2e.mjs` 新增 3 项，共 9 项：截图显示与大图、恢复这一处与撤销本轮各 1 个修订及部分成功、请求带选中物件。
      - `creative-scene.e2e.mjs` 新增 1 项，共 34 项：编辑器写入与收回上下文。
      - 变异检查 4 处，全部被抓到：签 `display` 角色、冲突后继续提交、提交后不重读、不带 `scene_context`。
      - 原有面板 e2e（`agent-send`、`creative-agent-blocks`、`agent-history`）照常通过；`creative-agent.test.ts` 13 项通过。
    - 顺手修了 C2 一项偶发失败：「leaving and coming back」在重载后只固定等 1.2 秒就做像素比对，负载高时截到还没画出来的画面（5 次中 1 次，100% 像素不同）。改为 5 秒内轮询比对，断言含义不变。

## 6. 收尾

- [x] 6.1 文档：`docs/dev/agent-skills-tools.md` 排空清单加 `waiting_client`、登记 4 个工具；新增 `docs/dev/creative-scene-agent.md`（客户端任务、渲染端、调试与日志字段）。
  - 实施记录：
    - `agent-skills-tools.md`：新增「场景工具」一节（四个工具的版本、效果、资源键、可用性）。排空清单加 `waiting_client`，并说明这类运行在等页面任务：要么开着页面让任务做完，要么等 30 分钟期限过期后运行恢复；`0075` 回滚在有 `waiting_client` 运行时拒绝。
    - `creative-scene-agent.md`：模块分工、任务生命周期与完成接口的结果表、失败码、运行的挂起与恢复、渲染端（认领循环、执行器、截图、编辑视图、面板）、接口一览、调试（日志字段、只读查库、常见现象）、测试命令、已知边界。
    - 写文档时核对日志，发现设计列的 `editor_session_id` 与「本段耗时」没有进日志，已补上：认领返回任务进入 `queued` 的时间，`Job` 多读 `editor_session_id`、`updated_at` 两列，迁移日志增加 `editor_session_id` 与 `in_state`。`TestJobTransitionsAreLogged` 覆盖建任务、认领、完成三条日志；去掉 `in_state` 时测试失败。
- [ ] 6.2 实测：
  - AC-12：`creative-scene-agent-latency.e2e.mjs` 经 `TestCreativeEditorBrowser` 测各段 p50 / p95；
  - 后台标签页：用真实 Chrome 隐藏 6 分钟以上，助手照常完成烘焙和截图（未决 #1）；
  - 结论写进 verification。
- [ ] 6.3 交 owner：用真实模型跑 3 个搭景样例各两次（AC-13，保留对话、截图和最终场景），并本机体验画布页搭景、编辑视图点选修改、本轮清单；记录质量结论与遗留问题。
