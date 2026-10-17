<!-- 本 change 的局部契约增量。epic 目标契约（patterns: 几何模块契约、patterns: 客户端代码沙箱、data: 模型内容、api: 场景模型命令）经 epic bind --deliver 交付，正文不复制到这里。下面一条修改底座已交付的项目契约。C1 已交付的 `api: 读取场景`、`data: 场景文档` 归 epic 所有，按 owner 2026-10-17 的决定（模型地址走媒体票据）在 epic 修订中以 MODIFY 改写，也由本 change `--deliver`。 -->

## patterns: MODIFY 文档类型端口

- **约定**：
  - 新的文档类型实现 `creativedocument.Type`（`Key`、`ContentKind`、`NodeTypeKey`、`Creatable`、`Empty`、`Units`、`Touched`、`Apply`、`RestoreTouched`、`RestoreUnits`、`View`、`Summary`），按需实现 `RestoreHook`，在类型所在包的 `init()` 中 `Register`，并保证 server 与 creative-worker 两个进程都导入该包（内置的 `internal.document` 在 `creativedocument` 包里注册）。重复的类型 key 或节点类型注册时直接 panic。
  - 类型不得自行写 `creative_documents` 及修订、引用、会话表，所有写入都经命令外壳。
  - `Touched` 不得写库；`Apply` 可以在同一事务里写类型自己的附属数据。
  - 单元的存在性发生变化时（新增、删除，或恢复一个当前不存在的单元），`Touched` / `RestoreTouched` 必须同时返回类型声明的集合单元（如场景的 `objects`、策划的 `shots`）。这样，「不存在 → 新增 → 删除」之后再提交的旧请求也会冲突。
  - `Apply` 新增的引用和 `RestoreUnits` 从旧修订带回的引用，由通用层分别校验（见 `api: 提交文档命令`「引用校验」）。
  - **节点类型映射**：每个文档类型声明且只声明一个画布节点类型（`NodeTypeKey`），两边都唯一，例如 `scene` ↔ `core.scene`、`photo.shoot_plan` ↔ `photo.shoot_plan`、`internal.document` ↔ `internal.document`。画布按注册表查映射，不写死任何类型。
  - **类型内新建内容的持有方交接**：
    - `Apply` / `RestoreUnits` 收到通用层传入的 `Retain` 回调。类型在事务内新建内容时，调用该内容类型规定的写入口，并把这个回调作为 retain 参数：一般内容用 `creativecontent.WriteAndRetain`；`model` 只能用 `creativecontent.WriteModelAndRetain`（场景的 `commit_model`）。
    - 回调会把该内容修订预登记到本次即将产生的修订（N+1）的引用行上，使内容层返回前的持有方检查通过。
    - 通用层在写入前核对：预登记的引用必须全部出现在最终快照的引用里，否则整个事务回滚。
  - **推进版本与只做标注**：`Changes.Changed` 中的单元推进到新修订号；`Changes.Annotated` 中的单元载荷有变化，但单元修订号**不**推进。用于「确认」这类操作：策划的 `confirm_section` 把 `confirmed_rev` 设为当前 `rev`，本身不改 `rev`。两类都写入修订行，供历史显示。
  - **类型专属的回执字段**：
    - `Changes.Response` 是类型要附加到命令回执的字段（键 → JSON），例如场景 `commit_model` 的 `models`。
    - 通用层把它并入 `CommandResult` 顶层；键与通用字段（`revision`、`changed_units`、`annotated_units`、`removed_units`、`reset_required`）冲突时视为实现错误，整个事务回滚。
    - 回执随幂等记录保存，重放时原样返回。
    - 没有附加字段的类型不设置它，回执与以前完全相同。
- **单元命名**：`meta` 保留给标题；其余单元由类型定义，形如 `<kind>` 或 `<kind>:<id>`，≤ 120 字符，`[A-Za-z0-9_:.-]`（场景物件 ID 是含大写的 nanoid，实施时放宽）。
- **为什么**：策划和场景共享修订、并发、恢复、回收站和画布接入的全部机制，避免两套实现分叉。回执字段由类型附加，是因为只有类型知道批内临时名（如 `commit_model` 的 `ref`）对应到哪个新内容，调用方需要据此推进本机状态，而不必为此重读整份快照。
- **不适用于**：不挂在画布节点上的业务对象（例如资产库素材），以及不需要修订历史的配置数据。
- **例子**：`internal.document` 的实现在 `backend/internal/creativedocument/internal_type.go`，单元 `body`，操作 `set_body`，不可新建。场景的回执扩展见 `api: 场景模型命令`「成功响应」。
