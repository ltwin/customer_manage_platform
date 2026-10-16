# 技术选型与设计模式

本项目"怎么写"的约定:选了什么、拒绝了什么、什么时候不适用。
`lti verify --review` 的代码审查以本文件为判据 —— 写在这里的约定才会被检查。

<!-- 建议的条目骨架(参考用,CLI 不校验格式):

## <选型或模式名>

- 约定:具体怎么做(可判定的规则,不是口号)
- 为什么:选它而不是 X 的理由
- 不适用于:什么情况下应该偏离
- 例子:代码库里的一个既有实现(file:symbol)

-->

## 文档类型端口

- **约定**：
  - 新的文档类型实现 `creativedocument.Type`（`Key`、`ContentKind`、`NodeTypeKey`、`Creatable`、`Empty`、`Units`、`Touched`、`Apply`、`RestoreTouched`、`RestoreUnits`、`View`、`Summary`），按需实现 `RestoreHook`，在类型所在包的 `init()` 中 `Register`，并保证 server 与 creative-worker 两个进程都导入该包（内置的 `internal.document` 在 `creativedocument` 包里注册）。重复的类型 key 或节点类型注册时直接 panic。
  - 类型不得自行写 `creative_documents` 及修订、引用、会话表，所有写入都经命令外壳。
  - `Touched` 不得写库；`Apply` 可以在同一事务里写类型自己的附属数据。
  - 单元的存在性发生变化时（新增、删除，或恢复一个当前不存在的单元），`Touched` / `RestoreTouched` 必须同时返回类型声明的集合单元（如场景的 `objects`、策划的 `shots`）。这样，「不存在 → 新增 → 删除」之后再提交的旧请求也会冲突。
  - `Apply` 新增的引用和 `RestoreUnits` 从旧修订带回的引用，由通用层分别校验（见 `api: 提交文档命令`「引用校验」）。
  - **节点类型映射**：每个文档类型声明且只声明一个画布节点类型（`NodeTypeKey`），两边都唯一，例如 `scene` ↔ `core.scene`、`photo.shoot_plan` ↔ `photo.shoot_plan`、`internal.document` ↔ `internal.document`。画布按注册表查映射，不写死任何类型。
  - **类型内新建内容的持有方交接**：`Apply` / `RestoreUnits` 收到通用层传入的 `Retain` 回调。类型在事务内新建内容（如场景的 `commit_model`）时，调用 `creativecontent.WriteAndRetain` 并把这个回调作为 retain 参数；回调会把该内容修订预登记到本次即将产生的修订（N+1）的引用行上，使内容层返回前的持有方检查通过。通用层在写入前核对：预登记的引用必须全部出现在最终快照的引用里，否则整个事务回滚。
  - **推进版本与只做标注**：`Changes.Changed` 中的单元推进到新修订号；`Changes.Annotated` 中的单元载荷有变化，但单元修订号**不**推进。用于「确认」这类操作：策划的 `confirm_section` 把 `confirmed_rev` 设为当前 `rev`，本身不改 `rev`。两类都写入修订行，供历史显示。
- **单元命名**：`meta` 保留给标题；其余单元由类型定义，形如 `<kind>` 或 `<kind>:<id>`，≤ 120 字符，`[A-Za-z0-9_:.-]`（场景物件 ID 是含大写的 nanoid，实施时放宽）。
- **为什么**：策划和场景共享修订、并发、恢复、回收站和画布接入的全部机制，避免两套实现分叉。
- **不适用于**：不挂在画布节点上的业务对象（例如资产库素材），以及不需要修订历史的配置数据。
- **例子**：`internal.document` 的实现在 `backend/internal/creativedocument/internal_type.go`，单元 `body`，操作 `set_body`，不可新建。
