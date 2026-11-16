<!-- 本 Feature 的目标契约，批准前为草稿，不代表项目当前已实现。 -->

## api: ADD 画布节点的创建时间

- 用途 / 消费者：让客户端按节点创建先后排列画布节点（节点树）。消费者：画布编辑器节点树；助手 `read_canvas` 与外部助手只读工具随 OpenAPI 生成的输出 schema 一并看到该字段，不依赖它。
- 位置：`CreativeCanvasNode.created_at`。凡返回 `CreativeCanvasNode` 的接口都带它：`GET /creative/canvases/{id}`（快照）、画布事件与命令结果中的节点对象、`read_canvas` 工具输出。
- 鉴权与资源权限：不变，沿用所在接口。

| 字段 | 类型 | 必返 | null | 说明 |
|---|---|---|---|---|
| `created_at` | string（RFC 3339，`date-time`，UTC） | 是 | 否 | 节点这个对象第一次出现在画布上的时间。只读；移动、改名、换内容、换分组不变；删除后被撤销恢复保持原值；复制、粘贴、复制画布产生的新节点是新对象，取新时间。客户端不能在命令里传入或修改。 |

- 精度：微秒（PostgreSQL `timestamptz`）。客户端排序时同值按 `id` 字典序兜底，保证顺序稳定。
- 错误码：无新增。
- 示例（快照中的一个节点，其余字段省略号表示与现状相同，非契约内容）：

```json
{
  "id": "cwnode_6f0f3c3e-2a51-4a4f-9a51-0f2c8c1c1a10",
  "parent_id": null,
  "created_at": "2026-11-15T09:12:33.418204Z",
  "metadata": { "type_key": "core.image", "type_version": 1, "title": "晨光人像", "intent": "", "x": 120, "y": 80, "width": 300, "height": 300, "z_order": 3 }
}
```

- 兼容性：只增字段，不破坏现有调用方；`CreativeCanvasNode` 是 `additionalProperties: false`，所有生成物（Go、TS、助手工具 schema）随 `make generate` 同步。前端本地投影的节点（离线 / 保存中）以客户端当前时间填该字段，仅供类型完整，排序不以它为准（见 design「排序」）。

## data: ADD 画布对象的创建时间

- 用途：记录画布对象（节点、连线、输入、版本）第一次出现的时间，使节点被删除后再经撤销恢复时能还原创建时间。本次只有节点使用它。
- 存储：`creative_graph_identities` 新增列 `created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp()`。身份行在对象删除后保留（`is_live = false`），所以它比 `creative_nodes` 行活得久，是对象创建时间的权威来源。
- 派生：`creative_nodes.created_at`（已存在列）改为由应用保证与身份行一致：节点行在身份已存在时（撤销恢复）以身份行的 `created_at` 插入；首次创建时两者都取默认值，相差在同一事务内的微秒级，不影响排序。读接口取 `creative_nodes.created_at`。
- 迁移：`0084_creative_graph_identity_created_at`。加列（默认值 `clock_timestamp()` 是易变函数，加列时整表重写一次，同 0060、0072 的做法）后回填：`UPDATE creative_graph_identities i SET created_at = n.created_at FROM creative_nodes n WHERE n.account_id = i.account_id AND n.id = i.id`。已删除节点的身份行没有可回填的来源，保留迁移时刻；它们若在迁移后被撤销恢复，会排到最前（一次性、仅限上线前已删除且仍在撤销窗口内的节点）。`down` 删除该列。
- 一致性：业务不变量「恢复的节点保持原创建时间」由 `persistObject` 保证；数据库只保证非空与默认值。
