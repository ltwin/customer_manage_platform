<!-- 已批准的目标契约，不代表项目当前已实现。与 Change delta 相同格式：
## architecture: ADD 模块边界
完整章节正文
## api: ADD 创建订单
API 正文按项目 api 契约中的写作骨架起草；至少明确以下内容：
- 必填：用途/消费者、方法与路径或操作名、鉴权与资源权限、请求字段表、成功响应字段表、错误码/触发条件/调用方处理、完整请求与成功/失败示例、兼容性。
- 字段表分别说明位置、类型、必传/必返、null、缺省行为和约束；展开嵌套字段或精确引用 schema。无参数/无响应体须明说。
- 按场景必填：列表分页/过滤/排序，写操作副作用/原子性/重复提交，并发冲突，异步生命周期，已有的限流/超时规则；不适用写原因，不为填表新增功能。
- 可选：时序图、选型背景、SDK 示例。正文内部用三级标题；不能用 TODO、object、正常返回或省略号替代定义。未知项标影响和解决时点。
MODIFY 给完整新正文；REMOVE 写原接口、原因、消费者影响与迁移/替代安排。
## data: ADD 订单
数据正文按项目 data 契约骨架：必填用途/存储形态/对象标识、主数据或派生角色、结构与身份、读写所有权、访问模式、约束/一致性、生命周期与演进。
按用途选择或组合：关系型的键/关系/事务；文档的嵌套/引用/动态校验；分析的粒度/排序分区/重复迟到数据；搜索的 mapping/分词/可见性；向量的来源模型/表示/维度规则/度量/过滤；图的节点/关系方向/属性/遍历。
派生数据须定义来源、ID 映射、同步/删除传播、一致性与重建切换。可引用原生 schema/DDL/mapping/图定义，配结构说明与访问样例；ER 图等按需，不强制 SQL 表或所有向量固定维度。
不适用说明原因，未知项标影响与解决时点；区分数据库保证和应用保证，不能用 schema-less、JSON、vector、最终一致或 TODO 代替设计。具体产品能力按采用版本核实。
MODIFY 给完整新正文；REMOVE 写退役原因、消费者、停读停写、保留/清理及恢复安排。
每个条目应能由一个 Change 完整交付；子项通过 epic bind --deliver 引用，不复制正文。
无共享契约变化时保持本文件为空或只有注释。
-->

## patterns: ADD 场景渲染批次

- **约定**：
  - **身份**：场景图里每个物件都有一个以物件 ID 命名的 group，它是身份、变换、可见性和拾取的唯一来源。渲染端为了少画几次而做的合并、共享和实例化，统称「渲染批次」，都是派生状态：可以随时重建，不写进场景文档、GLB 或 manifest，也不进渲染指纹。
  - **画质优先**（owner 2026-10-24）：批次层和阴影缓存不得改变画面，包括透明排序、幽灵模型、选中描边和线框边线。允许近似的优化（视口 LOD、阴影隔帧更新）要由 owner 看图确认肉眼看不出，而且出图（导出、`render_view`）不用。
  - **合并只在加载后做**：模型的网格片（`partsOf` 产出的 `ModelMeshPart`，即 GLB 里的每个 mesh，不是命名部件 `part:<id>`）在 `models/cache.ts` 的 `partsOf` 里合并：
    - 分组键是「槽位、hint、材质常量全部字段、顶点属性签名（名称、`itemSize`、`normalized`、数组类型）、是否有索引」；
    - 用 `mergeGeometries(geoms, false)`，不生成 group；
    - 组按场景树遍历中首次出现的顺序排列，组内按遍历顺序拼接，只有一个成员的组沿用原几何；
    - 合并时记录每个网格片的索引区间（非索引几何记顶点区间）和它自己的包围球。

    消费方看到的仍是 `{geometry, slot, hint, constants}`，另加只读的网格片范围。
  - **需要等价的地方按网格片画**：槽位解析出的材质半透明或透射时，以及幽灵模型，按网格片绘制「范围视图」几何：与合并几何共用 attribute 和 index，只设 `drawRange` 和该网格片的 `boundingSphere`。选中描边的外壳、线框边线也按网格片分别生成再拼接，不跨网格片计算。
  - **模型代码不负责合并**：沙箱的 `lib` 不提供合并，Skill 和工具说明也不要求作者合并。部件（`part:`）和挂点（`anchor:`）在 GLB 里保持原样。
  - **共享基础体几何**：参数相同的内置基础体共用一份几何，键是 `builtin` 加排序后的参数 JSON。缓存带引用计数，最后一个使用者卸载后延迟释放，线框边线随几何一起释放。物件自己不创建、也不释放基础体几何。
  - **按 draw 数衡量性能**：视口开销以「每帧 draw 数」为主要指标，由 `SceneRenderer` 每帧记录（draw 数、三角面、`render()` 的 CPU 毫秒）。新增渲染 pass 或阴影类型时，在设计里写明它让每个物件每帧多画几次。
- **为什么**：three r186 的 `WebGPURenderer` 每次 draw 约 3 µs CPU，帧时随 draw 数线性增长，与显卡无关。代码模型的 part 原样成为 draw，是视口卡顿的根因（`scene-performance` 的 `drawcall-diagnosis.md`）。合并放在渲染端，任何来源的模型都能受益（助手、创作者改的代码、资产库、以后的外部导入），GLB 也保留了部件结构。
- **不适用于**：
  - 辅助显示（灯光图标、摄像机视锥、网格、gizmo）：它们不在 0 号图层的主体里，数量少，不参与批次；
  - 幽灵和占位块；
  - 线框模式的边线：它按几何缓存，随共享几何一起共享。
- **例子**：`frontend/src/creative-scene/models/cache.ts` 的 `partsOf`（P1 交付后）。

## architecture: ADD 场景渲染负载

### 职责与边界
- **渲染端的批次层**（前端，属于渲染端，不依赖编辑器外壳）：
  - 加载模型时合并 part；
  - 按参数共享基础体几何；
  - 统计每帧负载。

  视口、离屏截图、导出和资产预览都经过它（`patterns: 场景渲染批次`）。
- **编辑器的统计叠加层**：读取渲染端的实测值，显示物件数、三角面、每帧 draw 数和帧时间。可以开关，默认关闭，不做估算。
- **服务端的负载估算**：`backend/internal/creativescene` 的纯函数，输入是快照和同一事务里批量读取的模型摘要，输出 `render_load`（`api: 场景渲染负载`）。不读 GLB 字节，不存储，不进渲染指纹，也不影响写入的成败。
- `backend/internal/creativeagent` 把 `render_load` 放进 `read_scene`、`edit_scene`、`write_model` 的结果；工具说明里有一句成本模型。负载只提示，不拦截。
- **估算规则与渲染端同版本**：`rule_version` 和渲染端的批次规则在同一次发布里一起变。

### 关键数据流
- **视口**：快照 → 物件 group 挂载 → 模型 part 从缓存取（已合并），基础体几何从共享缓存取 → `SceneRenderer` 画各个 pass，并记录每帧统计 → 统计叠加层。
- **助手**：工具 → `creativescene` 读快照和模型摘要 → 估算 → 工具结果里的 `render_load`。

## api: ADD 场景渲染负载

### 用途与调用
- **用途**：让助手在不打开页面、看不到帧率的情况下，知道场景结构会让视口多卡，以及主要来源是什么，从而调整搭法（成批的小件写成一个代码模型、重复的东西放同一模型的多个实例、减少点光投影）。`render_load` 正确反映当前快照，就算成功。
- **消费者**：内置助手、经 MCP 调用同名工具的外部助手（`creative-mcp-server` 规定同名工具结果相同），以及建模 Skill 的自检流程。
- **调用**：不是 HTTP 接口，是以下工具结果里的字段：
  - `read_scene@1`：成功结果的顶层，总是返回。按 64 KB 结果上限降级时保留，不在省略之列。
  - `edit_scene@1`：成功结果（`status` 为成功）的顶层。写入后档位为 `medium` 或 `heavy` 时返回，`light` 时省略。
  - `write_model@1`：成功结果的顶层。写入后档位为 `medium` 或 `heavy` 时返回，`light` 时省略。
- **格式**：工具结果的 JSON 结构化内容，与所在工具的结果一起返回。
- **权限**：与所在工具相同（会话授权含 `canvas_metadata`；外部连接按 `api: MCP 场景与 Skill 工具`），不额外判定。

### 请求
无。不新增任何工具入参，由服务端在返回结果时计算。

### 成功响应
`render_load` 对象：

| 字段路径 | 类型/格式 | 必返或出现条件 | 可为 null | 业务含义与约束 |
|---|---|---|---|---|
| `render_load.rule_version` | integer | 必返 | 否 | 估算规则版本。P2 交付 v1；P4 交付实例化时升到 v2 |
| `render_load.level` | string 枚举 | 必返 | 否 | 按 `draws.lit` 分档：`light`（< 3000）、`medium`（3000–7999）、`heavy`（≥ 8000）。阈值是初值，由 P2 标定后经 Epic 修订回填 |
| `render_load.draws.clay` | integer ≥ 0 | 必返 | 否 | 镜头运动时，白模视口每帧 draw 数的估算 |
| `render_load.draws.lit` | integer ≥ 0 | 必返 | 否 | 镜头运动时，最终渲染视口每帧 draw 数的估算（工作台与实时 GI 取同一估算） |
| `render_load.triangles` | integer ≥ 0 | 必返 | 否 | 可见物件的三角面之和：模型实例取 manifest 的 `triangles`；基础体取 `frontend/src/creative-scene/builtins/geometry.ts` 按参数生成的几何的三角形数（服务端实现与之对拍） |
| `render_load.sources` | array，0–3 项 | 必返 | 否 | 按 `draws` 降序的主要来源；没有来源（空场景）时为空数组 |
| `render_load.sources[].kind` | string 枚举 | 必返 | 否 | `model`：同一模型修订的全部可见实例；`primitives`：全部可见基础体；`shadow_lights`：投影灯带来的额外绘制 |
| `render_load.sources[].label` | string ≤ 80 | 必返 | 否 | 中文说明，例如「「哥特柱」× 40，每个 4 个槽位」「1000 个基础体」「1 盏点光、2 盏聚光投影」 |
| `render_load.sources[].count` | integer ≥ 1 | 必返 | 否 | `model` 是实例数，`primitives` 是基础体数，`shadow_lights` 是投影灯数 |
| `render_load.sources[].object_ids` | string[]，1–5 项 | 必返 | 否 | 例子物件 ID：`model` 和 `primitives` 取前 5 个，按场景顺序；`shadow_lights` 是投影灯的物件 ID |
| `render_load.sources[].draws` | integer ≥ 0 | 必返 | 否 | 这一来源占 `draws.lit` 的部分 |

**估算规则 v1**（对应 P1 交付后的渲染端：part 已合并，每个物件单独绘制）：
- **可见**：物件自己的 `visible` 为 true。按场景约定，隐藏编组不隐藏子物件。
- **渲染单元**：
  - 模型实例：每个实例计 `max(1, 槽位数)`，槽位数取 manifest 的 `slots`。不可用的模型计 1 个单元、0 面。
  - 基础体：每个计 1 个单元。
  - 灯光、摄像机、编组、体积：不计。
- **投影倍数 S**：只计可见、`cast_shadow` 为 true、强度大于 0 的灯，再加太阳。
  - 平行光：每盏 + 4（4 级级联）；
  - 聚光：每盏 + 1；
  - 点光：每盏 + 6（立方体贴图的 6 个面）；
  - 太阳：程序天空或物理天空下 + 4。
- **draw 数**：`draws.clay = 2 × 单元数`，`draws.lit = (2 + S) × 单元数`。
- **来源的 draws**：
  - `model` 或 `primitives`：它的单元数 × 2；
  - `shadow_lights`：S × 全部单元数。

**v2**（P4）：同一模型修订、`material_slots` 相同的可见实例，满 2 个以上成为一组，一组只计一次槽位数；参数和 `main` 槽材质都相同的基础体同理。具体文字由 P4 前的 Epic 修订定稿。

### 失败响应
无独立的失败信号：估算是对已读快照的纯计算，不会单独失败。所在工具失败时（例如 `read_scene` 的场景不存在、`edit_scene` 冲突），沿用该工具的错误，结果里没有 `render_load`。

### 交互规则
- **一致性**：在所在工具读快照的同一事务里计算，反映该修订；`edit_scene` 和 `write_model` 反映写入之后的修订。
- **不拦截**：档位为 `heavy` 时写入照常成功，只附带 `render_load`。助手据此自行调整。
- 不适用分页、幂等、并发冲突、异步、限流：它只是结果里的一个只读字段，随所在工具的规则。

### 调用示例
场景：1000 个基础体，40 个「哥特柱」实例（每个 3 个槽位），1 盏点光和 2 盏聚光投影，纯色天空（没有太阳）。

计算：
- 单元数 = 1000 + 40 × 3 = 1120；
- S = 6 + 1 + 1 = 8；
- `draws.clay` = 2 × 1120 = 2240；
- `draws.lit` = 10 × 1120 = 11200，档位 `heavy`。

`read_scene` 的成功结果（只列本契约相关的字段，`read_scene` 其余既有字段不变）：

```json
{
  "document_id": "cwdoc_0f8e2a6c-1d3b-4e5f-9a7b-2c4d6e8f0a1b",
  "title": "哥特长廊",
  "revision": 42,
  "object_count": 1043,
  "render_load": {
    "rule_version": 1,
    "level": "heavy",
    "draws": { "clay": 2240, "lit": 11200 },
    "triangles": 486000,
    "sources": [
      { "kind": "shadow_lights", "label": "1 盏点光、2 盏聚光投影", "count": 3, "draws": 8960, "object_ids": ["sobj_lightpoint0000000000", "sobj_lightspota000000000", "sobj_lightspotb000000000"] },
      { "kind": "primitives", "label": "1000 个基础体", "count": 1000, "draws": 2000, "object_ids": ["sobj_brick00000000000000", "sobj_brick00010000000000", "sobj_brick00020000000000", "sobj_brick00030000000000", "sobj_brick00040000000000"] },
      { "kind": "model", "label": "「哥特柱」× 40，每个 3 个槽位", "count": 40, "draws": 240, "object_ids": ["sobj_pillar0000000000000", "sobj_pillar0100000000000", "sobj_pillar0200000000000", "sobj_pillar0300000000000", "sobj_pillar0400000000000"] }
    ]
  }
}
```

失败或省略的情况：
- `edit_scene` 写入后档位仍为 `light`：结果里没有 `render_load`，其余与现在相同。
- `read_scene` 的场景不存在：照旧返回该工具的 `not_found`，没有本字段。

### 兼容与变更
- 兼容扩展：既有工具结果新增一个顶层字段，入参不变，工具版本不变（owner 2026-10-24）。
- 已有消费者只是多读到一个字段，默认行为不变。
- `edit_scene` 和 `write_model` 在负载为 `light` 时结果与现在完全相同。
- 估算规则升级时（v1 → v2）改 `rule_version`，字段结构不变。
