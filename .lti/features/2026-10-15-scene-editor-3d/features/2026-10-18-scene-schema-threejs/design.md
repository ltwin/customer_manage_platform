# Change 详细设计

## 继承的设计

- Epic `scene-editor-3d` 修订 v7（owner 2026-10-18 批准）。
  - `data: 场景文档`：灯光表、命名原则、v7 迁移例外。
  - `api: 提交场景命令`：`add_objects` 补默认值，`update_objects` 按键合并。
  - `api: 读取运行的场景改动`：3 处补记。
  - `design.md`「材质与光照（C5）」：灯光字段原样赋给 three.js 灯光类。
- 本子项只落地这些已批准的定义，不新增共享概念。字段表、默认值、类型规则以 epic 契约为准，本文不复制。
- 证据来自 C3 试用。2026-10-18 开发库只读查询：
  - 3 次灯光写入都按 three.js 命名；
  - 旋转全部写成四元数；
  - 70 个场景修订里 13 个带摄像机或雾，没有灯光。

## 领域模型与业务规则

不变量（服务端保证，编辑器本地应用保持一致）：

- **L1**：存储的灯光恰好含其 `type` 的全部字段（epic 灯光表），不含其他字段。
- **L2**：输入的灯光只能含其 `type` 有的字段，否则 400，消息指出第一个多余字段（按字段表顺序）。
- **L3**：存储的摄像机含全部 6 个字段。
- **L4**：`update_objects` 只改变写到的字段。灯光 `type` 改变时，保留新类型也有的旧字段，去掉其余字段，缺少的补默认值；更新里本身写了新类型没有的字段，按 L2 拒绝。

内部类型（`backend/internal/creativescene/model.go`）：

| 类型 | 字段 | 说明 |
|---|---|---|
| `Fog` | `Color`、`Near`、`Far`（`near`、`far`） | 仍为必填，值类型 |
| `Light` | `Type string`（必填）；`Color *string`、`Intensity *float64`、`CastShadow *bool`、`Distance`/`Decay`/`Angle`/`Penumbra *float64`，全部 `omitempty` | 指针区分「没写」和「写了 0」。存储时按 L1 补齐，`omitempty` 只省略 nil，所以 `distance: 0` 会照常写出 |
| `lightPatch` | 同 `Light`，但 `Type` 也是 `omitempty` | 只用于 `objectUpdate.Light`，更新时可以不写 `type` |
| `Camera` | `FocalMM *float64`、`SensorMM []float64`、`Aspect *string`、`Near`/`Far *float64`、`Label *string`，全部 `omitempty` | 存储时按 L3 补齐 |

`decodeInput` 的必填规则由 json 标签推出（`requireFields`），改成 `omitempty` 就是「可省略」，不另写规则。`range_m` 是原来唯一可为 null 的灯光字段，改名后灯光没有可空字段：写 `null` 返回「字段不能为 null」。

## 模块职责与协作

```
edit_scene / POST commands
  → decodeInput（严格解码：未知字段，含旧名 range_m、near_m → 400 指出字段）
  → addObjectsOp.apply：completeLight / completeCamera（L2 检查 + 补默认值）
    updateObjectsOp.apply：mergeLight / mergeCamera（L4）
  → 整份校验 validateObject → validateLight / validateCamera（数值范围）
  → 写快照
```

- 新函数都放在 `model.go`，紧挨着 `validateLight`、`validateCamera`。不新建文件，不新建抽象。
- 字段表用一处常量 `lightFields`：类型 → 有序字段名，供 L1、L2、L4 共用。
- 默认值放在 `completeLight`、`completeCamera` 里。
- 前端 `state/apply.ts` 镜像一份 `lightFields` 和默认值，只用于本地应用 `update_objects`。服务端仍是唯一的校验和补值方：新增物件时编辑器不替服务端补值。

## 关键机制

### 灯光的补齐与合并（L1–L4）

```
completeLight(l):            // add_objects
  if l.Type ∉ lightFields: 400 未知灯光类型
  for f in 所有可选字段（固定顺序）: if l.f != nil && f ∉ lightFields[l.Type]: 400 "<类型>没有 <f>"
  for f in lightFields[l.Type]: if l.f == nil: l.f = default(f)

mergeLight(cur, p):          // update_objects
  t = p.Type ?? cur.Type
  for f in p 写了的字段: if f ∉ lightFields[t]: 400（L2，按更新本身判断）
  next = cur 中 f ∈ lightFields[t] 的字段 ⊕ p 写了的字段（p 覆盖）
  next.Type = t; completeLight(next)
```

决策表（`update_objects` 的 `light`）：

| 当前 | 更新 | 结果 |
|---|---|---|
| point（`#ff963d`、45、7、2、false） | `{intensity: 80}` | point（`#ff963d`、80、7、2、false） |
| spot（含 `angle`、`penumbra`） | `{type: "point"}` | point，去掉 `angle`、`penumbra`，其余保留 |
| point | `{type: "spot"}` | spot，`angle` π/3、`penumbra` 0，其余保留 |
| point | `{angle: 0.5}` | 400：点光没有 angle |
| point | `{type: "spot", angle: 0.5}` | spot，`angle` 0.5 |

摄像机没有类型，按键覆盖后补齐即可。`update_objects` 涉及的单元不变（`light`、`camera` 都属于内容单元），并发与冲突规则不变。

### 迁移 0076

`creative_content_revisions` 中 `creative_contents.kind = 'scene'` 的行，在一个事务里改写 `payload->'scene'`：

| 位置 | up | down |
|---|---|---|
| `environment.fog`（非 null） | `near_m`→`near`，`far_m`→`far` | 反向 |
| `objects[].camera` | `near_m`→`near`，`far_m`→`far` | 反向 |
| 灯光 `directional` | 去掉 `range_m`、`angle_deg`、`penumbra`（旧格式里可为 null） | 补回三项 null |
| 灯光 `point` | `distance = coalesce(range_m, 0)`，`decay = 2`，去掉 `range_m`、`angle_deg`、`penumbra` | `range_m = nullif(distance, 0)`，`angle_deg`、`penumbra` 为 null，去掉 `distance`、`decay` |
| 灯光 `spot` | 同 point，另 `angle = radians(angle_deg) / 2`、`penumbra = coalesce(penumbra, 0)` | 同 point，另 `angle_deg = degrees(angle × 2)` |

- **down 拒绝有损回滚**：存在 `decay ≠ 2` 的灯光时 `RAISE EXCEPTION`，前向修复。做法沿用 0074、0075。
- **只改 payload**：不动修订号、单元版本和引用表；改写前后的数值相同，历史预览和恢复照常。
- 服务启动时执行迁移（`backend/cmd/server/main.go` 的 `store.MigrateUp`），新代码只在迁移后的数据上运行。

## 局部设计与偏差

- **前端读取处用契约默认值兜底**：OpenAPI 的 `CreativeSceneLight`、`CreativeSceneCamera` 读写共用。字段改为可选后，生成的 TS 类型也是可选。读取处（`host/framing.ts` 的 near/far、`CameraFrustum` 已有的兜底）写 `??` 加契约默认值。服务端返回的快照总是完整的，兜底只是为了满足类型。另一种做法是拆出读写两套 schema，为这几处兜底不值得。
- **开发库是共享的远程库**：在 worktree 里启动新后端会把 0076 跑到开发库上，主目录里没有 C3a 的旧后端随后就读不了摄像机和雾。所以实施期间只用测试容器（`startPostgres`）验证迁移。owner 验收试用时才从 worktree 启动新后端，此后到合入 develop 之前，不再启动主目录的旧后端。

## 接口与数据契约

- **契约状态**：破坏性修改，由 `epic bind --deliver` 交付 `data: 场景文档`、`api: 提交场景命令`、`api: 读取运行的场景改动`。本 Feature 的 `contracts.md` 不新增局部契约。
- **OpenAPI**：
  - `CreativeSceneFog`：`near`、`far`。
  - `CreativeSceneLight`：只有 `type` 必填，加 `distance`、`decay`、`angle`，去掉 `range_m`、`angle_deg`，各字段不再 nullable。描述写明响应里按类型总是完整。
  - `CreativeSceneCamera`：全部可选，`near`、`far`。
  - `update_objects` 条目的 `light` 允许不写 `type`。
- 「读取运行的场景改动」只改契约文字，不改代码和 OpenAPI：OpenAPI 的 `blocked.reason` 枚举和描述已含 `hierarchy_not_restorable` 与多来源规则（2026-10-18 核对）。

### `edit_scene` 说明（灯光、摄像机、雾部分的新文字）

> light 字段同 three.js：{type:directional|point|spot, color:"#rrggbb", intensity, cast_shadow}，point 和 spot 另有 distance（米，0 不截断）、decay（默认 2），spot 另有 angle（弧度，半角，≤π/2，默认 π/3）、penumbra（0…1）；只有 type 必填，其余取 three.js 默认值，写了该类型没有的字段会被拒绝。intensity 同 three.js 物理光照：点光和聚光为坎德拉，平行光为 three.js 强度。平行光和聚光沿物件局部 −Z 照射（不用 target），摄像机朝局部 −Z。camera{focal_mm:8…800, sensor_mm:[宽,高], aspect:"3:2", near, far, label} 均可省略。update_objects 里 light、camera 只改写到的字段。fog:{color, near, far} 或 null。太阳方位 0° 朝 +Z、90° 朝 +X。

「灯光物件、lit 模式和 hdri 天空暂不显示」那句保留，到 C5 再去掉。

## 挂载点

| 位置 | 动作 | 说明 |
|---|---|---|
| `backend/internal/creativescene/model.go:Fog` | 修改 | `near`、`far` |
| `backend/internal/creativescene/model.go:Light` | 修改 | 指针字段、新名字 |
| `backend/internal/creativescene/model.go:Camera` | 修改 | 指针字段、`near`、`far` |
| `backend/internal/creativescene/model.go:validateEnvironment` | 修改 | 雾的消息用新名 |
| `backend/internal/creativescene/model.go:validateLight` | 修改 | 新字段的范围 |
| `backend/internal/creativescene/model.go:validateCamera` | 修改 | 新名 |
| `backend/internal/creativescene/model.go:decodeInput` | 修改 | 注释里的可空字段示例 |
| `backend/internal/creativescene/model.go:completeLight` | 新增 | L1、L2 |
| `backend/internal/creativescene/model.go:completeCamera` | 新增 | L3 |
| `backend/internal/creativescene/model.go:mergeLight` | 新增 | L4 |
| `backend/internal/creativescene/model.go:mergeCamera` | 新增 | L4 |
| `backend/internal/creativescene/ops.go:objectUpdate` | 修改 | `Light *lightPatch`，注释改为按键合并 |
| `backend/internal/creativescene/ops.go:addObjectsOp.apply` | 修改 | 调用 `completeLight` / `completeCamera` |
| `backend/internal/creativescene/ops.go:updateObjectsOp.apply` | 修改 | 调用 `mergeLight` / `mergeCamera` |
| `backend/internal/creativescene/scene_test.go` | 修改 | AC-1–AC-4 用例，旧夹具改名 |
| `backend/internal/creativescene/agent_tools_test.go` | 修改 | 夹具改名 |
| `backend/internal/creativeagent/scene_tools.go:sceneTool.Info` | 修改 | `edit_scene` 说明 |
| `backend/internal/platform/store/migrations/0076_creative_scene_threejs_fields.up.sql` | 新增 | 迁移 |
| `backend/internal/platform/store/migrations/0076_creative_scene_threejs_fields.down.sql` | 新增 | 反向迁移，拒绝有损回滚 |
| `backend/internal/platform/store/creative_scene_threejs_migration_test.go` | 新增 | 迁移测试与回滚辅助函数 |
| `backend/internal/platform/store/creative_scene_jobs_migration_test.go:rollbackSceneJobs` | 修改 | 先回滚 0076 |
| `backend/internal/platform/httpapi/creative_documents_test.go` | 修改 | 夹具改名 |
| `api/openapi.yaml` | 修改 | `CreativeSceneFog`、`CreativeSceneLight`、`CreativeSceneCamera` 与更新条目 |
| `backend/internal/platform/httpapi/api.gen.go` | 生成 | `make generate` |
| `frontend/src/api/schema.d.ts` | 生成 | `make generate` |
| `frontend/src/creative-scene/Viewport.tsx:Environment` | 修改 | 雾用 `near`/`far` |
| `frontend/src/creative-scene/host/framing.ts:cameraFraming` | 修改 | `near`/`far`，带契约默认值兜底 |
| `frontend/src/creative-scene/state/apply.ts:updateObject` | 修改 | `light`、`camera` 按键合并；灯光类型切换 |
| `frontend/scripts/creative-scene.test.ts` | 修改 | 本地合并用例（同 AC-4 决策表） |
| `frontend/scripts/creative-scene-agent.test.ts` | 修改 | 夹具改名 |
| `frontend/scripts/creative-scene-model.test.ts` | 修改 | 夹具改名 |
| `frontend/scripts/creative-scene.e2e.mjs` | 修改 | 夹具改名 |
| `frontend/scripts/creative-scene-agent.e2e.mjs` | 修改 | 夹具改名 |

## 验证与交付

| 验收 | 验证 |
|---|---|
| AC-1、AC-2、AC-3 | `creativescene` 单元测试：按类型补齐；只写 `type`；旧名、多余字段、`null` 各一例，断言消息指出字段且场景不变；`httpapi` 走一遍 400 |
| AC-4 | `creativescene` 用例覆盖决策表 5 行加摄像机局部更新；`creative-scene.test.ts` 用同样的输入和期望覆盖本地应用 |
| AC-5 | `creative-scene.e2e.mjs` 带雾的场景照常渲染；`creative-scene-agent.e2e.mjs` 按摄像机取景的截图用新字段 |
| AC-6 | 迁移测试（测试容器）：插入旧格式快照（雾、摄像机、三种灯光），up 后断言新字段和数值；down 后还原；`decay = 3` 时 down 被拒 |
| AC-7 | `scene_tools_test` 断言说明包含 `distance`、`decay`、`angle`、`near`、`far`，且不含旧名 |
| AC-8 | 归档时核对 `contractsApplied`；契约文字与 `run_changes.go` 对照 |

- **门禁**：日常用 `make check-go PKG=./internal/creativescene/...`、`check-go PKG=./internal/platform/store/...` 和 `make check-frontend`。合回 develop 前跑一次 `make check`（先合 develop）。
- **owner 验收试用**（从 worktree 启动后端，见上节开发库注意事项）：请助手给场景加点光、聚光和雾，不再被拒；读回字段完整。灯光此时仍不渲染，只看写入是否成功。开发库里原有带摄像机或雾的场景照常打开（AC-6 的真实数据证据）。
