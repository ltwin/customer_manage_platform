<!-- Epic 目标契约草案。项目 .lti/contracts/ 是现行事实。
M1 交付本文件的两条契约。检查代码与阈值经 M0 验证后定稿。
M1 实施前，在 creative-mcp-server 交付场景客户端任务后，以现行基线补任务与完成接口、回看输入和能力协商。
M2 另补库版本涉及的几何模块、模型内容、命令与任务契约。M3、M4 按实际交付基线补摆放及补丁契约。
同一条目分次修改时，后一个子项基于已交付正文修订并绑定其交付版本。 -->

## data: ADD 模型检查报告

### 用途与数据所有权

报告给出本次烘焙模型的几何量测、证据与覆盖情况。以通过预检的 GLB 和模块的 `mount` 为输入，服务端校验外形后随场景客户端任务的终态保存 JSON。报告属于该任务实际使用的模型产物，与写入后的内容修订关联。冲突重试可复用同一产物的报告。报告的生命周期跟随任务结果，资产、场景快照和模型内容的持久化结构不变。

在相同 GLB 字节、`mount` 和检查版本下，完整检查的量测、排序及截断可重现。部分完成取决于执行预算。模型默认材质回执独立定义在工具结果。

### 结构与身份

| 字段 | 类型 | 必须 | 约束与含义 |
|---|---|---|---|
| `version` | integer | 是 | 初版 1。代码表、量测、容差、定位或排序语义变化需要新版本 |
| `status` | `complete` 或 `partial` | 是 | 全部检查有确定的执行结果时为 `complete`，有检查未覆盖其输入时为 `partial`。裁剪输出列表不改变执行状态 |
| `checks` | array | 是 | 该版本每个检查代码恰一项，按版本代码表顺序 |
| `checks[].code` | enum | 是 | 对应本版本的代码表 |
| `checks[].status` | `complete`、`partial` 或 `not_applicable` | 是 | `complete` 表示覆盖该检查的输入；`partial` 包括未运行完或存在无法判定的输入；`not_applicable` 表示没有适用输入 |
| `checks[].reason` | `budget`、`error` 或 `indeterminate` | 仅 `partial` | 检查覆盖不足的原因。预算用于时间、内存或工作量限制 |
| `parts` | array，≤ 200 项 | 是 | 命名部件摘要，按 manifest 顺序保留可放入预算的项 |
| `parts[].id` | string | 是 | manifest 中的部件 ID，同一报告至多出现一次 |
| `parts[].status` | `measured`、`ambiguous`、`empty` 或 `unmeasured` | 是 | 唯一部件且已量测、ID 对应多个部件、无归属几何、或未完成量测 |
| `parts[].size_m`、`center_m` | [number, number, number] | 仅 `measured` | 模型坐标的包围盒尺寸和中心，单位米 |
| `parts[].triangles` | integer | 仅 `measured` | 部件归属的三角形数 |
| `findings` | array，≤ 64 项 | 是 | 实际测出的诊断项，有序 |
| `findings[].code`、`severity` | enum | 是 | 代码和固定严重度 `problem` 或 `note` |
| `findings[].where` | array，≤ 2 项 | 是 | 全模型量测用空数组；其余位置为 `{"part":"<id>"}` 或 `{"primitive":<整数>}`，字段互斥。后者按 GLB 活动场景的根节点及 children 顺序深度优先遍历，在每个网格节点按 primitive 顺序累加，从 0 起计。每个节点实例各自计数 |
| `findings[].value`、`vector_m`、`area_m2`、`count` | number、[number, number, number]、number、integer | 按代码 | 只允许该代码规定的量。长度单位米、面积单位平方米，计数非负 |
| `omitted.parts` | integer | 是 | 已整理但因输出预算省略的部件数。未扫描部分体现在执行状态 |
| `omitted.findings` | object | 是 | 各已知代码因输出预算省略的已找到条目数，为空时用 `{}` |

所有数值须有限。尺寸和距离非负。字段禁止 `null`。部件、去重与 primitive 定位规则见 design「几何诊断的范围」。

### 首版检查代码

M0 确定这些代码的适用输入、容差、严重度与排序后固化版本 1。量测分别记录明确缺陷及观察提示。

| code | severity（草案） | where | 量测 |
|---|---|---|---|
| `origin_offset` | `problem` | 全模型 | `value` 为偏移长度，`vector_m` 为偏移向量 |
| `winding_conflict` | `problem` | 一个部件或 primitive | `count` 为绕序冲突边数 |
| `inverted_shell` | `problem` | 一个部件或 primitive | `count` 为确认的内翻壳体数 |
| `coplanar_overlap` | `problem` | 一或两个几何位置 | `value` 为间距，`area_m2` 为该诊断测得的重叠面积，`count` 为涉及的三角形数 |

若 M0 支持距离或表面相交提示，定稿时追加独立代码，保留与推断意图的区别。

### 预算、校验与失败

- 整份报告紧凑 JSON ≤ 16 KiB。每个代码最多列 8 条诊断，诊断总数 ≤ 64。先为执行状态和省略计数预留空间，再按严重度、代码及各自的量测优先级填充诊断，最后按 manifest 顺序填充可容纳的部件。并列按版本规定的定位顺序决胜。截断精确计入两个省略字段。部件摘要是否入表不影响 manifest 内合法 ID 的诊断定位。
- `creativescene` 在完成任务时验证报告版本、枚举、数值、必传字段、总字节数和数组约束。部件引用必须在服务端预检得出的 manifest 内；primitive 序号按版本约束进行结构校验，仅用于定位。结构不合规的报告独立丢弃。检查结果、客户端量测和标识的内容都属于不可信观测。
- 只接收已知报告版本与代码。旧宿主可省略报告。未来版本通过能力协商启用；无法解释的新版本不会作为任意 JSON 原样进入助手上下文。
- 独立丢弃的范围是通过外层格式、请求大小与准入校验后的可选附件。任务完成接口须把可选载荷的解码与必需字段校验隔离；同一合法完成请求内，拼图无效按缺图处理，不触发必需上传缺失的错误与重传。
- 失效、过期、未完成和截断都可以降级。回执缺失时仍返回已成功写入的事实和其他有效反馈。

## api: MODIFY 助手场景工具的输入与写入结果

### `edit_scene@1`

已获场景权限的调用者通过既有的场景文档 ID 和读取基线提交原子批次。普通写入需要 `based_on_revision`。单独撤回按现有撤回规则执行。

| 字段 | 可省略与含义 |
|---|---|
| `add_objects.objects[].id` | 省略则服务端生成合法物件 ID |
| `parent_id`、`visible`、`locked`、`order` | 新增时分别默认根级、可见、未锁定和在同级末尾追加 |
| `transform` | 新增时缺省为原点、单位旋转、单位缩放，更新时只应用传入的分量。新物件使用 `look_at` 时须提供 `position` |
| `look_at`、`yaw_deg`、`transform.rotation` | 三者至多给一个。目标点是世界坐标，水平旋转是局部 Y 轴的角度 |
| `added_object_ids` | 写入新增物件时按新增输入的顺序返回 ID |

分镜更新只写相关关键帧。缺失、错位和互斥参数返回 `creative_tool_refused`，整批回滚。修订冲突和回执重放沿用既有语义。存量调用保持有效。

### `write_model@1`

目标保持 `add` 或 `replace` 二选一。新任务给非空 `code` 或可复用的 `base_content_revision_id`。冲突重试给 `retry_job_id`，与前两种来源及 `params` 互斥。参数错误在入队前返回具体提示。

沿用现有请求授权、写入条件、暂存来源及错误语义。M1 无新增必填请求。最终成功时，在既有写入修订、模型事实和面分配回执之上追加以下字段。未提供时省略，各字段均不接受 `null`。

| 字段 | 必返 | 含义 |
|---|---|---|
| `report_status` | 开启报告反馈时 | `not_run` 表示已知未启动；`unavailable` 表示未取得有效报告，例如旧宿主无回传、超时无产物或校验丢弃；`complete` 或 `partial` 对应 `report.status`。未启用反馈的旧结果可省略 |
| `report` | `report_status` 为 `complete` 或 `partial` 时 | 本次烘焙模型的检查报告，见数据契约 |
| `material_defaults` | 否 | ≤ 32 项，按 GLB manifest 槽位顺序提供模型默认解析，单独受工具结果预算约束。整份无效或超预算时省略 |
| `sheet` | 否 | 仅本次工具结果实际附有多视角素模图片块时为 `true` |

`material_defaults` 描述忽略实例材质覆盖与面重分配时的模型材质。同一模型在不同实例可有不同外观。回执使用现有材质解析规则，目录查找按执行时的有效目录。每项的结构如下。

| 字段 | 必须 | 含义 |
|---|---|---|
| `slot` | 是 | 本次 GLB manifest 槽位 ID |
| `source` | 是 | `hint` 为命中预设，`model` 为 GLB 常量，`default` 为默认材质，`mixed` 为同一槽不同 primitive 的结果有差异 |
| `preset` | 仅 `hint` | 实际命中的材质预设 ID |
| `ignored` | 否 | 被默认解析覆盖的 GLB 常量字段，去重，取值为 `color`、`roughness`、`metalness`、`emissive`、`emissiveIntensity`。仅在能确认整个槽位适用时列出 |

**执行结果和错误**

- 几何发现、报告丢失和可选附件失效都不改变写入结论。编译失败、GLB 预检失败、冲突、取消及其他既有失败继续返回原状态。
- 同一任务的正常完成、后台终态与外部任务等待共用数值回执。成功的冲突重试关联实际写入修订和复用的同一产物，过期附件可省略。图片随内置模型请求发送时复查外发授权；外部读取复查当前连接与任务归属。能力、授权、附件有效性或预算不满足时省略 `sheet`。报告与拼图均不能作为后续场景命令的修订。
- 在现有工具结果总预算内优先保留写入事实和报告状态，其次是有效数值报告和材质回执。投影时若无法装入有效报告，将其状态改为 `unavailable`。生成代码原文、错误细节、任意 extras 或节点标签不进入附件。

**示例**

新建模型成功，附有一张拼图。以下 JSON 与图片块一起返回。

```json
{
  "status": "succeeded",
  "content_revision_id": "ccrv_example",
  "object_ids": ["chair"],
  "written_revision": 12,
  "size_m": [0.42, 0.9, 0.45],
  "triangles": 6144,
  "slots": [{"id": "wood", "hint": "plank", "source": "model"}],
  "parts": ["seat"],
  "anchors": [],
  "uv": true,
  "report_status": "complete",
  "report": {
    "version": 1,
    "status": "complete",
    "checks": [
      {"code": "origin_offset", "status": "complete"},
      {"code": "winding_conflict", "status": "complete"},
      {"code": "inverted_shell", "status": "complete"},
      {"code": "coplanar_overlap", "status": "not_applicable"}
    ],
    "parts": [
      {"id": "seat", "status": "measured", "size_m": [0.42, 0.04, 0.45], "center_m": [0, 0.45, 0], "triangles": 3072}
    ],
    "findings": [
      {"code": "origin_offset", "severity": "problem", "where": [], "value": 0.02, "vector_m": [0, 0.02, 0]}
    ],
    "omitted": {"parts": 0, "findings": {}}
  },
  "material_defaults": [
    {"slot": "wood", "source": "hint", "preset": "wood_plank@1", "ignored": ["color"]}
  ],
  "sheet": true
}
```

检查超时且无有效报告时，成功结果返回 `"report_status":"unavailable"`，省略 `report`，其余字段独立决定。已有写入冲突继续返回原冲突回执和有效的重试句柄。旧宿主仍可按原协议完成普通任务。
