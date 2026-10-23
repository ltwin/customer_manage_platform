# Change 详细设计

## 继承的设计

- Epic `scene-world-environment` 批准版本 v6：
  - 「领域模型与业务规则」：空气透视的概念与边界，E2 单一来源，E5 三种雾共存。
  - 「架构设计」：`world/atmosphere/` 的职责（「空气透视 froxel 在 W4」），`SceneRenderer` 的合成位置，空气透视 froxel 的更新时机。
  - 「关键机制」：LUT 规格表（空气透视 32 × 32 × 32，RGBA16F），环境相对单位，「合成顺序」第 4 步。
  - 「技术选型」：Hillaire 2020，用 TSL 自写，片元 pass，两个后端共用。
- `data: ADD 空气透视`（本次完整交付）。
- W2 `scene-physical-sky` 的 `world/atmosphere/luts.ts`（透射率、多次散射 LUT，`setSun`），W3 `scene-night-sky` 的 `setMoon`。
- 本次承担 roadmap W4：空气透视的数据、渲染、编辑界面与助手说明。体积物件（W5）不在本次。

## 领域模型与业务规则

- 空气透视是物理天空的一部分（`sky.aerial_perspective`），属于场景，与摄像机无关；地点模式和手动太阳模式都生效（它只需要太阳和月亮的方向，不需要日期）。
- 作用对象：不透明表面在摄像机与表面之间的那段大气。天空背景已含整条大气，不叠加（AC-W4-4）。
- 光源：太阳；夜里月亮按 W3 的规则成为光源时也计入（E2：与天空背景和天空光照同一套光源）。星空、银河和夜天光的贡献很弱，不计入。
- 距离：表面到摄像机的距离（米）× `distance_scale`，换算成千米查表；倍率只影响空气透视，不影响天空背景、全局雾和体积物件。
- 不计阴影：空气透视里的散射不判断视线上的点是否被物体或地形挡住（Hillaire 2020 同样不算），所以不会出现光束；光束由 W5 的体积物件负责。

## 模块职责与协作

| 模块 | 职责 | 不负责 |
|---|---|---|
| `world/atmosphere/aerial.ts`（新） | froxel 的生成 pass（每个摄像机每帧一次）与查表节点 `applyAerial(color, depth)`；全局 uniform：开关、倍率、太阳和月亮的方向与照度 | 何时开启（由 `PhysicalLighting` 决定）、曝光的求法 |
| `objects/Lighting.tsx:PhysicalLighting` | 环境变化时写 `aerial.ts` 的 uniform（开关、倍率、光源）；卸载时关闭 | froxel 的生成 |
| `render/sceneRenderer.ts:SceneRenderer` | 每次出图前，空气透视开启时用本次的摄像机生成 froxel，再画；把查表节点接进两条路径的合成位置 | — |
| `render/realtimeGI.ts` | 在全局雾之后、叠加辅助对象之前调用 `applyAerial` | — |

主流程（视口，一帧）：

```
SceneRenderer.render(camera, frame)
  ├─ 写曝光（applyExposure）与 preExposure
  ├─ aerial 开启？→ renderAerialFroxel(renderer, camera)   // 32 768 像素的片元 pass，写进 1024 × 32 图集
  └─ pipeline.render()
       └─ 合成：表面光照 → 全局雾 → applyAerial（查表：color · T + S · k / s_env）→ 景深 → 夜色 → 色调映射
```

froxel 与曝光一样在同一个同步调用里生成和使用（沿用 `preExposure` 的约束：生成与绘制之间不 `await`），所以视口和离屏出图共用一张图集也不会串用摄像机。

## 关键机制

### froxel 的生成

- **布局**：32 个深度切片，每片 32 × 32（屏幕 uv），横向排成 1024 × 32 的 RGBA16F 图集。`rgb` 为入射散射（环境相对单位，与天空视图相同），`a` 为透射率的三通道平均（Hillaire 的做法）。
- **深度分布**：切片 `i` 的中心距离 `d_i = D_MAX · ((i + 0.5) / 32)²`，D_MAX = 32 km，近处的切片更密；查表时 `slice = sqrt(d / D_MAX) · 32`。超过 D_MAX 的取最后一片。
- **积分**：每个像素从摄像机沿该 uv 的视线方向积分到 `d_i`，步数 `max(1, i + 1)`（最多 32），每步：
  ```
  σ_s, σ_t = 大气系数（setAtmosphere 已有）在该点的高度
  L_sun  = T_sun(点) · (σ_s^R · P_R(θ) + σ_s^M · P_M(θ)) · E_sun + 多次散射项 · σ_s
  L_moon = 同上，取月亮方向与照度（月亮不是光源时照度为 0）
  S += T_view · (L_sun + L_moon) · Δs；T_view ·= exp(−σ_t · Δs)
  ```
  T_sun 与多次散射从 W2 的两张 LUT 查，E_sun、E_moon 就是 `setSun`、`setMoon` 的 scale（已含 s_env）。
- **何时生成**：空气透视开启、天空为物理天空、显示模式为最终渲染时，每次出图前生成一次。不做「摄像机不动就跳过」的缓存：pass 很小，缓存要比较摄像机矩阵和环境，得不偿失（以 AC-W4-9 实测为准；超标时再加）。

### 查表与合成

```
applyAerial(color, depth):
  opaque = depth < 1                                   // 天空背景不叠加
  dist_m = |由 depth 与投影矩阵重建的视空间位置|
  d_km   = dist_m · distance_scale / 1000
  (S, T) = 图集的三线性采样（切片内双线性，相邻切片线性混合）
  out    = mix(color, color · T + S · relative, on · opaque)     // relative = k / s_env，与天空背景相同
```

- 两条路径的接入点：
  - 工作台：`beauty × AO` 之后、景深之前。辅助对象画在 beauty 里，取它后面表面的空气透视（与 AO 的现状相同）。
  - 实时 GI：`realtimeGI.ts` 里 `fogged` 之后、叠加 `aside`（辅助对象）之前。
- `on` 是 uniform：开关、换天空不重建管线、不编译着色器。
- 透明物件：用的是不透明表面的深度，玻璃取它后面表面的空气透视（局限，与 GI 路径的全局雾一致）。

### 数据与服务端

- `model.go`：`Sky` 加 `AerialPerspective *AerialPerspective`，`AerialPerspective{Enabled *bool; DistanceScale *float64}`，未知字段照旧拒绝。
- `sky.go`：`physicalOnly` 列入 `aerial_perspective`（其他天空写它返回 400）；`completeSky` 逐项补齐（true、1）；`validatePhysicalSky` 校验倍率 0.1…1000，越界返回 400 并写 `sky.aerial_perspective.distance_scale`。
- 前端 `world/physicalSky.ts`：`aerialDefaults`，`completeSky` 与 `isPhysical` 与 `night` 同样处理。
- 渲染指纹：随 `sky` 计入 `lit`，不需改 `fingerprint.go`（与 W3 的 `night` 相同，由测试固定）。

### 编辑界面

- `PhysicalSkyFields.tsx` 在夜空一组之前加「空气透视」：开关；「距离倍率」对数滑杆 0.1…1000（显示 ×0.1…×1000），开关关闭时滑杆置灰。拖动沿用环境的合并键折叠成一次提交。
- 说明文字：「远处景物因大气变蓝变淡。小场景调大距离倍率才看得出」。

### 助手说明

`edit_scene` 的 `set_environment` 说明加：`aerial_perspective: {enabled, distance_scale 0.1–1000}`，只在物理天空下可写；真实尺度下几百米内很弱，院子、街道这类场景要调到 50–200 才看得出；不影响天空。

## 局部设计与偏差

- **froxel 存成 2D 图集，不用 `RenderTarget3D`**（Epic roadmap 留给 W4 的问题）：片元 pass 写 3D 纹理的切片在 WebGL2 后端需要逐层绑定，three 0.186 的 `RenderPipeline` 也没有现成写法；2D 图集一次 pass 写完，两个后端相同，查表多一次切片间的手动插值。
- **与实时 GI 的合成顺序**（Epic roadmap 留给 W4 的问题）：放在 GI 合成与全局雾之后，GI 不看空气透视；与 Epic「合成顺序」1→2→4 一致。体积物件（W5）将插在全局雾与空气透视之间。
- **不按摄像机缓存 froxel**：见上，超标时再加。
- 没有改变 Epic 共享设计的决定。

## 接口与数据契约

- 契约状态：扩展。`data: ADD 空气透视` 按 Epic 定义完整交付，本 Change 的 contracts.md 不另加增量。
- OpenAPI：`CreativeSceneSky` 加 `aerial_perspective`（`CreativeSceneAerialPerspective`：`enabled` boolean，`distance_scale` number 0.1…1000）；Go 与 TS 生成物同步。

## 挂载点

| 位置 | 动作 | 说明 |
|---|---|---|
| `frontend/src/creative-scene/world/atmosphere/aerial.ts` | 新增 | froxel 生成 pass、`applyAerial`、全局 uniform |
| `frontend/src/creative-scene/world/atmosphere/constants.ts` | 修改 | `AERIAL` 尺寸与 D_MAX |
| `frontend/src/creative-scene/world/atmosphere/luts.ts` | 修改 | 导出 froxel 积分要用的系数与多次散射采样 |
| `frontend/src/creative-scene/render/sceneRenderer.ts:SceneRenderer` | 修改 | 出图前生成 froxel；工作台路径接 `applyAerial` |
| `frontend/src/creative-scene/render/realtimeGI.ts:realtimeGI` | 修改 | GI 路径接 `applyAerial` |
| `frontend/src/creative-scene/objects/Lighting.tsx:PhysicalLighting` | 修改 | 写空气透视的开关、倍率、光源 |
| `frontend/src/creative-scene/world/physicalSky.ts` | 修改 | 缺省值、`completeSky`、`isPhysical` |
| `frontend/src/creative-scene/panels/PhysicalSkyFields.tsx` | 修改 | 「空气透视」一组 |
| `frontend/src/creative-scene/editing.ts` | 修改 | 空气透视的提交与合并键（如需） |
| `backend/internal/creativescene/model.go` | 修改 | `AerialPerspective` 类型 |
| `backend/internal/creativescene/sky.go` | 修改 | `physicalOnly`、`completeSky`、校验 |
| `backend/internal/creativeagent/scene_tools.go` | 修改 | `edit_scene` 说明 |
| `api/openapi.yaml` | 修改 | `CreativeSceneSky.aerial_perspective` |
| `backend/internal/platform/httpapi/api.gen.go` | 生成 | — |
| `backend/internal/shootplanning/httpcontract/` 生成物 | 生成 | 如 OpenAPI 变更涉及 |
| `backend/internal/creativecanvas/tool_schemas.gen.go` | 生成 | 如涉及 |
| `frontend/src/api/schema.d.ts` | 生成 | — |
| `docs/dev/creative-scene-agent.md`、`docs/dev/agent-skills-tools.md` | 修改 | 记录 |

## 验证与交付

- 后端：`scene_test.go` 补齐、400 的各情形（删去「aerial_perspective 返回 400」的旧用例）；`fingerprint_internal_test.go`：改空气透视 `lit` 指纹变、`clay`/`line` 不变，W3 时期的快照补齐后 `lit` 指纹变；`scene_tools_test.go` 固定关键句。
- 前端单测：补齐与 `isPhysical`；CPU 上同一积分的参考值（单条视线）与 TSL 查表一致（在 e2e 中读回图集比对，误差 ≤ 2%）。
- e2e（新增「W4」用例，`SCENE_E2E_ONLY=W4`）：AC-W4-1（近远方块的对比度与蓝色比例，正午、黄昏、雾霾三组）、AC-W4-2、AC-W4-3（关闭时与 W3 基线逐像素一致）、AC-W4-4（天空像素不变）、AC-W4-8；AC-W4-5 在 `SCENE_E2E_GPU` 下两个后端对照；AC-W4-9 复用「W3 performance」加开关对照。
- 已有用例：W2、W3 依赖基线逐像素比较的用例显式关闭空气透视；其他按缺省（开启，倍率 1）运行，预期仍通过（院子尺度下变化很小），不通过时逐个看是否只是空气透视带来的预期变化。
- 观感由 owner 在视口里看（远山、城市参考场景调大倍率）。
