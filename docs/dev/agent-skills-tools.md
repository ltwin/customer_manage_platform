# Agent Skills 与 Tools 运行契约

本文件记录 2026-10-15 改造后的现行契约。它替代早期文档中「Skill 工具白名单是权限上界」「只能读取本轮显式 Skill 资源」和「只发现会话用过的 Skill」的描述。

## 职责与权限

Skill 提供方法与参考资料；`required_tools` 表示执行方法所需的工具，不授予、不缩小业务权限。工具是否可用由部署能力、账号能力、会话授权及领域层最终检查决定。加载多个 Skill 不合并或扩大授权。

Agent 工具注册信息统一在 `creativeagent/tool_registry.go`：目录、版本、Eino 绑定、调用可用性及并发资源声明由同一份定义生成。参数说明与执行仍由相邻业务工具实现。写操作继续使用领域命令与回执，不借此次改造开放 `creativeops` 的新权限。未知工具保守占用整个运行；新工具未登记资源规则时不会意外并行。

展示名称来自完整注册定义，不受部署就绪状态影响。工具目录每次查询按当前部署条件生成，不能在初始化时缓存：生成服务在绑定任务队列前会返回空模型目录，而队列绑定发生在 Agent 接入生成服务之后。本轮模型工具集合再叠加模型能力和会话许可过滤，执行时仍重新检查。

## 发现、激活、资源

- 手动选择仍直接注入该固定版本正文，并登记为本轮已激活。
- 新运行按账号自己的 Skill 与受信平台目录提供最多 20 条摘要。摘要不包含正文；名称、描述仅供模型选择方法。窗口不足时目录摘要可以让位，`search_skills` 仍可检索。
- `search_skills@1(query,cursor)` 按名称或描述搜索，每页最多 20 条；空页且有 `next_cursor` 时继续翻页。
- `load_skill@2(skill_version_id)` 只加载本轮已发现或历史索引登记的固定版本，返回正文、版本和资源清单。最多激活 20 个不同版本，重复加载不重复计数。
- `read_skill_resource@2(skill_version_id,path)` 只能读本轮已激活版本的声明资源。服务端持有 digest 并核对资源哈希；不同 Skill 使用相同路径不会混淆。
- 模型能力、依赖工具和本轮授权不满足时，不推荐自动候选；激活时再次校验。目录端点的可用性表示部署可用，实际模型与会话条件在执行时裁定。

初始目录保存在 run 的 `skill_discovery`；搜索页按 run/step 保存。`creative_run_skills` 记录账号、运行、版本、digest、候选来源及激活快照。候选/搜索页先持久化再交给模型，重放不重新查 latest。激活前未公开正文；激活成功的状态与版本在恢复中保留。

加载与读取通过 `skills` 资源依赖排序；其他无冲突工具仍可并行。运行准入在 run 锁之后、consent/content 锁之前，按版本 ID 排序检查所有激活版本的控制锁。撤销先提交后，不再向模型派发这些方法或资源。

后续会话索引包含自动激活版本，并兼容旧 run 的显式快照。方法加载一次后跨轮沿用：早先运行激活过、加载结果仍在本轮历史里的版本，新运行开始时重新校验并登记为本轮已激活（来源 `history`），正文照常随历史发送，资源直接可读，不重复加载。版本已停用、模型或依赖工具不满足时不沿用，历史里的方法/资源正文替换为提示，需要时重新加载。压缩只保留使用记录，正文离开上下文后按固定版本重新加载。

## 场景工具（C3，2026-10-18）

五个场景工具同样在 `tool_registry.go` 登记，实现在 `creativeagent/scene_tools.go`，领域入口在 `creativescene`。都要求会话授权含 `canvas_metadata`；资源键是 `scene:<document_id>`，参数里没有场景时占用整个运行。

| 工具 | 展示名 | 效果 | 资源 | 可用性与说明 |
|---|---|---|---|---|
| `read_scene@1` | 读取场景 | 只读 | 共享 | 概要上限 300 个物件，`include_code` 至多 3 个模型的代码；带「自你上次写入后的变化」摘要；按 64 KB 结果上限降级。W2 起物理天空按地点和时间放太阳时带太阳参考 `sun`（当前高度、罗盘与场景方位、正午、日出日落、黄金时刻与蓝调时刻）；W3 起同时带月亮参考 `moon`（月相与照亮比例、高度与方位、月出月落、前后最近的满月和新月） |
| `edit_scene@1` | 修改场景 | 写，影响上限 2000 | 独占 | 基线规则：基线之后别人改过的单元与本次涉及的单元有交集即冲突，冲突作为失败结果返回；`revert_run` 撤回某次运行；不开放 `commit_model`、整份 `restore`。C4 起：摄像机可写全部参数（含景深与对焦），条目可写 `look_at`（换算成 `transform` 后再交给场景命令）。C5 起：开放 `reparent_objects`（可建、删分组，改挂由服务端换算成保持世界位置的变换，不收 `transform`）；`material_slots` 在工具层按槽位合并（`null` 清除）并按物件槽位与材质目录校验，错误列出可用项；拒绝 `hdri` 天空；`set_render` 按键合并，可写曝光补偿 `exposure_ev`。R2 起 `set_render` 可写渲染器 `engine`（`workbench` / `realtime_gi`，`path_tracing` 被拒）和输出分辨率 `resolution`（`1k`–`8k`）。W1 起说明写明 `clay` 用固定的工作室光、不受环境和灯光影响，环境只作用于 `lit`。W2 起 `set_environment` 可写物理天空（地点三项成组、IANA 时区名区分大小写、`date` 必填、大气预设、太阳亮度与色温；物理天空下环境光不生效），`set_render` 可写 `exposure_ev100`，摄像机可写 `iso`、`shutter_s`；说明给出两段曝光建议与太阳参考 `sun` 的字段和口径；写了物理天空（地点模式）的结果带 `sun`。W3 起物理天空可写 `night`（月亮、星星、银河的开关，月亮与星空的亮度和色温，夜色 `night_look`），结果另带 `moon`；摄像机与 `set_render` 可写 `exposure_mode`（`ambient` 环境测光 / `manual`，缺省环境测光）和 `exposure_compensation`，说明改为「默认环境测光，入夜不必改曝光；写实时用手动」。W4 起物理天空可写 `aerial_perspective`（`enabled`、`distance_scale` 0.1–1000），说明提醒院子、街道这类场景调到 50–200 才看得出 |
| `write_model@1` | 生成模型 | 写，影响上限 1 | 共享 | 建 `bake_model` 任务并挂起运行，页面执行后恢复；只改参数时用 `base_content_revision_id` 沿用代码；冲突后用 `retry_job_id` 复用已烘焙的产物 |
| `render_view@1` | 回看场景 | 只读 | 共享 | 只在模型支持图片输入时提供；建 `render_view` 任务并挂起，截图进入下一步的图片输入。C4 起截图与编辑器视口、白模导出同一套渲染（带 AO）；可选 `depth_of_field: true`：只能与 `camera_object_id` 一起用、摄像机须已打开景深，再加景深虚化。C5 起可选 `mode: clay\|lit`，`lit` 是最终渲染（材质、灯光、天空光照、曝光补偿）。R2 起 `lit` 按场景的渲染器画（`clay` 总是工作台），实时 GI 预热 12 帧后累积 16 帧，时限按估算（至少 60 秒）。W1 起 `clay` 是固定的工作室光，不受环境和灯光影响。W2 起物理天空下 `lit` 按摄像机自己的 ISO、快门、光圈曝光，临时视点和预设取景按 `render.exposure_ev100`。W3 起按摄像机或 `render` 的曝光模式：环境测光按场景照度测光（不看画面）并做夜间与低太阳压暗，手动同 W2；入夜后画面带夜色；物理天空下 `lit` 的结果带 `exposure`（`by` 摄像机或默认摄像机、`mode`、环境测光的 `compensation` 或手动的 `ev100`），说明提醒临时视点和预设取景的亮度可能与摄像机不同 |
| `export_shots@1` | 导出分镜 | 写，影响上限 48 | 场景共享、画布独占 | C4：建 `export_shots` 任务并挂起；页面出图上传后随完成任务提交，节点在完成事务里放到运行所在画布的场景节点旁；结果只有节点 ID，不带图片。C5 起 `passes` 可含 `lit`（省略仍为 clay + line）。R2 起 lit 按场景的渲染器出图（clay、line 总是工作台），长边按 `render.resolution`（4K、8K 的 clay 与 lit 为 JPEG）；4K、8K 出图时设备丢失以 `host_error` 结束、不回队。W1 起 clay 是固定的工作室光，环境只让 lit 图过时 |

任务、挂起与恢复、渲染端的细节见 [场景助手开发说明](creative-scene-agent.md)。

## 包格式与兼容

标准纯文本包只需 `SKILL.md`，YAML frontmatter 必含 `name`、`description`。支持 `license`、`compatibility`、`metadata`、`allowed-tools` 元数据；后者不会被转换成权限。正文与参考资源按需读取。

可选的平台扩展 `manifest.json`：

```json
{
  "manifest_schema_version": 2,
  "display_name": "参考整理与创作方向",
  "required_tools": ["search_assets@2", "read_canvas@1", "read_skill_resource@2"],
  "required_model_capabilities": ["tool_calling"],
  "output_guidance": "如实说明候选取舍与资料缺口。"
}
```

扩展不得重复定义 name/description；无依赖的方法允许空数组。模型能力名沿用网关 `tool_calling`、`image_input`、`tool_choice`、`json_object`、`temperature`，未实现的名称判不可用。`completion_check_key` 不再出现在 v2；`output_guidance` 是指导，不是执行验收器。旧输入分类和数量声明也不迁入 v2。

支持标准描述上限 1024 字符。资源仍限每文件 64 KiB、最多 20 个、整包 256 KiB；路径穿越、隐藏条目、符号链接、脚本和非文本资源明确拒绝。这是标准包的纯文本支持范围，不承诺执行任意生态脚本。

v1 编码与 digest 固定样例保持原值，历史版本和导入回执不重写。v1 的 `tool_allowlist` 在执行侧适配为依赖，旧加载/资源工具的 @1 依赖映射至新的 @2 能力；这不暴露旧调用参数。v1 API 仍返回原字段，v2 API 返回新结构。内置包变更通过发布新版本生效。

## 验证与质量评测

`skill_runtime_test.go` 覆盖自动发现、固定版本、历史资源、跨账号隔离、停用、激活上限、分页、同批加载/读取以及检查点恢复。`package_v2_test.go` 覆盖标准格式；旧编码固定值测试继续约束 v1。

`skill_evaluation_test.go` 包含两个内置 Skill 的固定任务集。运行状态、方法激活、参考资源读取、必需工具调用和未获指令的生成调用分别判定。内容质量状态初始为 `unreviewed`，按每例 rubric 核对来源、缺口、交付和阶段停顿，不能把运行成功填成质量通过。

真实评测仅在显式开启后运行，使用独立测试账号。它会消耗模型 token 与网页检索额度，不配置图片生成器。示例（在 backend 执行，凭证按已有环境配置）：

```sh
CREATIVE_AGENT_LIVE=1 CREATIVE_SKILL_EVAL_LIVE=1 CREATIVE_SKILL_LIVE_OUT=/tmp/creative-skill-eval go test ./internal/creativeagent -run '^TestLiveSkillEvaluation$' -v -count=1
```

比较案例使用固定的绿色配色参考素材，空库案例单独检查资料缺口。续用案例必须先完成首轮激活，第二轮沿用同一固定版本且不重复加载。每例输出 JSON（逐轮运行、行为检查、工具次数、原始 token 用量证据、质量 rubric）和完整的逐轮 Markdown 对话记录。用量证据保留未知项及修订，不累加可能被替代的计量记录。

## 排空后升级

此次工具参数及检查点指纹有版本变化，不支持旧活动运行跨此发布续跑。

C4（scene-camera-export）沿用这套步骤：`render_view@1` 新增可选参数、`edit_scene@1` 说明与 `look_at`、新增 `export_shots@1`，版本号不变（同 C3a 修改 `edit_scene@1`），工具定义变化会改变检查点指纹，发布前同样排空；没有迁移。

C5（scene-materials-lighting）同样：`edit_scene@1` 开放 `reparent_objects`、说明加材质目录与打光建议（场景工具说明共增长约 2.8 KB）、`render_view@1` 加 `mode`、`export_shots@1` 的 `passes` 加 `lit`，版本号不变，发布前排空；没有迁移。

R2（scene-render-settings）同样：`edit_scene@1` 的 `set_render` 说明加渲染器与输出分辨率、`render_view@1` 与 `export_shots@1` 说明随渲染器和分辨率（场景工具说明共增长约 0.9 KB），参数不变、版本号不变，发布前排空；没有迁移（老场景缺的 `engine`、`resolution` 读取时补缺省）。

W1（scene-display-look）同样：`edit_scene@1`、`render_view@1`、`export_shots@1` 的说明写明白模用固定的工作室光、环境只作用于材质光照（场景工具说明共增长约 0.3 KB），参数不变、版本号不变，发布前排空；没有迁移。

1. 在入口进入维护窗口，阻断新增 Agent 运行与重试，保留读取和处理已有等待任务所需的入口。
2. 旧 worker 继续完成运行。检查 queued/running/waiting_input/waiting_apply/waiting_client/reconciling 等非终态运行与待恢复任务；全部处理到终态才继续。不要自动取消、删除检查点或伪造终态。`waiting_client` 的运行在等页面执行场景任务（`creative_scene_jobs` 里 `queued` / `claimed` 的行）：保持一个创意空间页面开着让任务做完，或者等任务到 30 分钟期限后由清扫转为过期，运行随之恢复（助手得知页面没有打开）；迁移 `0075` 的回滚在还有 `waiting_client` 运行时会拒绝。
3. 停止旧 worker，执行迁移并发布新服务，启动新 worker，再恢复新增运行入口。先迁移、后启动；多版本 worker 不混跑。
4. 历史消息、Skill 版本、digest 和业务回执继续保留。回退前同样排空；若已写入更长描述或新格式名称，down 迁移会拒绝缩窄约束，不能截断不可变数据，应前向修复。

本次源码改造不执行上述正式发布步骤。

## 本次实施验证记录（2026-10-15）

- 独立审查三轮，五项发现均已修复，无剩余代码发现；不执行提交或部署。
- `make check-frontend` 通过：编译、lint、521 项测试。
- `make generate-check` 通过；`git diff --check` 通过。
- 新增历史隔离、目录预算、续用评测回归通过；标准包和 v1 固定 digest、旧导入回放，以及发现、激活、资源和检查点恢复测试均通过。
- 最终两轮 `make check-go` 的 build、lint 均通过（0 issues），**全量进程没有获得全绿退出**。第一轮仅 order 在测试开始前启动 PostgreSQL 容器失败；第二轮 order 通过，仅 creativeskill 在同一启动阶段失败。共同错误为 Docker `ContainerInspect` 的 `context deadline exceeded`，发生在 `wait until ready / mapped port / check target`，不是测试断言失败。
- order 单独 `go test ./internal/order -count=3` 通过（8.092s）；creativeskill 分别启动三个独立 `go test ./internal/creativeskill -count=1` 进程，均通过（8.705s、7.264s、6.991s）。失败位置游走且独立复跑变绿，符合仓库环境失败分诊条件；保留全量未全绿这一限制，不把它记为完整门禁通过。
- store 迁移包在上述两轮分别通过（110.529s、116.718s），其余后端包在对应轮次通过。未修改 ops 脚本，未增加 `check-ops`。
- 真实模型评测 **未运行**，内容质量保持 `unreviewed`；不得把本次本地回归解释为真实模型质量通过。
