# Change 详细设计

## 继承的设计

- Epic `scene-editor-3d` 修订 v10（C5 起草时提交，待 owner 与本设计一并批准）。适用条目：
  - design：「渲染模式与导出」（`lit` 与四种显示模式）、「材质与光照（C5）」、「单元版本、写入与撤销」（编组与改挂）、「编辑器交互约定」（C5 按键）、「技术选型」（材质、材质预览的光照）。
  - 契约：`data: 场景文档`（材质槽、灯光、环境、`render.mode`）、`data: 导出图来源` 与 `api: 导出分镜`（`lit`，已交付）。
  - 本次交付四条契约修改：`api: 提交场景命令`（`reparent_objects` 扩展、`set_render` 按键合并）、`data: 场景文档`（`render.exposure_ev`）、`data: 场景客户端任务`（`render_view` 的 `mode: "lit"`）、`api: 读取运行的场景改动`（改挂后的不可恢复规则）。
- 2026-10-20 设计评审的意见已逐项并入：曝光补偿（proposal 决定 8）、环境贴图不含太阳盘、程序化图案抗锯齿、环境光只作补光、线框拾取措辞、沿用 `fitSunShadow`、`moves` 至少一项、`anisotropy_rotation` 用弧度、浮点容差、镜像部件绕序、轮廓边按几何缓存、运行改动只对候选修订读快照，以及「恢复这一处」的后续选项。
- 决定来源：proposal「新增决定」1–7，owner 2026-10-19 全部确认。
- 沿用 C4 的渲染骨架：
  - `ClayRenderer`：`RenderPass` → GTAO → 景深 → `OutputPass`；
  - 视口用 `useFrame` 优先级 1 出帧；
  - 渲染端用 `openShooter` 离屏出图。
- 现状核对见 proposal「现状」，代码事实来自 2026-10-19 对 develop 20b9da5 的盘点。

## 领域模型与业务规则

### 显示模式

| 模式 | 物件材质 | 光照与背景 | AO / 景深预览 | 高亮 |
|---|---|---|---|---|
| 线框 `wireframe` | 网格不画（材质 `visible: false`，仍能被射线拾取），只画轮廓边（`EdgesGeometry`，夹角阈值 30°，按几何缓存，共用模型的实例共用一份） | 中性灰背景，无光照 | 不走合成器，直接 `gl.render` | 轮廓边用高亮色，不加 `Outlines` |
| 白模 `solid` | 现有白模材质 | 现有 `Environment`：天空、太阳、半球光、环境光（+0.3）、雾 | 有 | 现状 |
| 材质预览 `preview` | 按槽位解析的材质 | `RoomEnvironment` 经 PMREM 作 `scene.environment`；中性灰背景；不挂太阳、半球光、环境光、灯光物件、雾 | 有 | 现状 |
| 最终渲染 `rendered` | 按槽位解析的材质 | 天空背景；太阳平行光；天空作 `scene.environment`（不含太阳盘）；环境光作补光；灯光物件；雾；阴影；曝光 `2^render.exposure_ev` | 有 | 现状（高亮颜色随曝光一起缩放） |

- **两组渲染方式**：白模和最终渲染分别对应 `clay`、`lit` 两种「渲染方式」（`Look = 'clay' | 'lit'`），视口、`render_view` 和导出共用。线框和材质预览只在编辑器里用。
- **存储**：显示模式是本机状态。
  - 存在 `SavedView.display` 里，按账号和场景记住，与视角同一个键。
  - 不写进文档，也不进撤销栈。
  - 没有记录时按 `scene.render.mode` 取初值：`lit` → 最终渲染，其余 → 白模。
- **白模不变**：为保证 C4 的白模导出不变，白模模式的光照保持现状，包括环境光 +0.3、半球光、drei 的 `Sky`、按投影物体贴合的阳光阴影（`render/sunShadow.ts:fitSunShadow`，78a47dd），以及曝光 1；新的光照规则只用于最终渲染。

### 材质解析

槽位 → 材质的规则。同一模型的所有实例按实例的 `material_slots` 各自解析。

| 顺序 | 条件 | 结果 |
|---|---|---|
| 1 | `material_slots[slot]` 存在，`preset` 在目录里 | 该预设或原理化 BSDF。参数按目录补默认值，越界的值夹到范围内（命令接口只校验外形，旧数据或手写数据可能越界） |
| 2 | `material_slots[slot]` 存在，`preset` 不在目录里 | 中性灰（`#8a8a8a`，糙度 0.9），属性栏提示「未知材质」 |
| 3 | 没有指定，槽位提示能映射到预设（目录的 `hints`；提示按小写、去空白后整词匹配） | 该预设的默认参数 |
| 4 | 没有指定、映射不到，是模型 | GLB 里这块网格的材质常量：`color`、`roughness`、`metalness`、`emissive`、`emissiveIntensity`，以及有 `COLOR_0` 时的顶点色，按原理化 BSDF 渲染 |
| 5 | 没有指定、映射不到，是基础体 | 中性浅灰：原理化 BSDF，`color #d9d5cd`、`roughness 0.92`，与白模同色 |

- **槽位的来源**：
  - 基础体固定为 `main`；
  - 模型取 manifest 的 `slots`；
  - 网格的材质名不是 `slot:<id>` 的部分（代码里直接 `new MeshStandardMaterial()`）没有槽位，只走第 4 条。
- **预设不可变**：同一 ID（如 `stone_brick@1`）发布后着色器和参数含义不变，要改外观就发布 `@2`，旧场景的观感因此不变。目录测试锁定每个预设的参数表。
- **透明**：原理化 BSDF 的 `opacity < 1` 或 `transmission > 0` 的网格是「透明网格」，不参与 AO 的法线和深度，景深按它后面物体的距离虚化。所有程序化预设都不透明。

### 编组规则

所有编组操作都保持世界位置，生成一个 `reparent_objects`，一个修订、一步撤销。

| 操作 | 对象 | 新父级 | 顺序 | 失败 |
|---|---|---|---|---|
| `Ctrl + G` 编组 | 选中物件取最上层（祖先也被选中的不计，`input/selection.ts:topmost`） | 新分组：中点为这些物件世界位置的平均，无旋转，缩放 1；分组挂在它们共同的父级下，父级不同时放在根；顺序排在它们原来位置中最靠前的一个；名称「分组 N」 | 子物件保持原来的相对顺序，从 0 起排 | 没有选中物件时不可用 |
| `Ctrl + Alt + G` 取消编组 | 选中的分组（可多个） | 各分组原来的父级 | 子物件按原顺序插在分组原来的位置，后面的兄弟顺延 | 选中的不是分组时提示 |
| `Ctrl + P` 设置父级 | 选中物件（除活动物件）的最上层 | 活动物件 | 排在活动物件已有子物件之后 | 活动物件不是分组 / 空物体，或会成环（活动物件是被挂物件的后代）时提示，场景不变 |
| `Alt + P` 清除父级 | 选中物件中有父级的 | 根 | 排在根的末尾 | 都在根下时不可用 |
| 物件列表拖动 | 被拖的行；行已选中时为全部选中物件的最上层 | 放在一行中间 1/2 且该行是分组：挂进去；放在上沿 1/4 或下沿 1/4：挂到该行的父级 | 插在该行之前或之后，后面的兄弟顺延 | 拖到自己或后代上不可放 |

- **局部变换的算法**：`local = inverse(新父级世界矩阵) × 物件世界矩阵`，再分解为位置、四元数和缩放（与 three.js `Matrix4.decompose` 相同）。
  - 父级带非等比缩放、物件又有旋转时会含切变，分解只能近似，与 Blender、three.js 相同。
  - 分解出的缩放超出 0.001–1000，或位置超出 ±100000 m 时，不提交，提示「无法在保持位置的前提下改挂」；不像模态变换那样把缩放夹到范围内。
- **并发**：
  - 改挂只把被改挂物件的摆放单元和 `objects` 计入版本检查，不检查父级链的摆放单元。
  - 别人同时移动了新父级或旧父级时，结果等于「先改挂、后移动父级」的串行结果：物件随父级移动。这是合法的串行顺序，不算跳位。
  - 别人同时改了被改挂物件本身，摆放单元冲突，走现有冲突处理。

### 内部类型

```ts
// frontend/src/creative-scene/materials/catalog.ts
type MaterialEntry = {
  id: string                         // 'principled@1' | 'stone_brick@1' …
  kind: 'principled' | 'pattern'
  label: string
  params: Record<string, ParamSpec>  // 复用 builtins/catalog.ts 的 ParamSpec（number|integer|boolean|enum|color）
}
type MaterialCatalog = { materials: MaterialEntry[]; hints: Record<string, string> }  // hint 词 → 预设 ID

// materials/resolve.ts
type SlotMaterial =
  | { kind: 'principled'; params: PrincipledParams }
  | { kind: 'pattern'; id: string; params: Record<string, number | string | boolean> }
  | { kind: 'unknown' }               // 第 2 条：中性灰 + 提示
type ResolvedSlot = { slot: string | null; source: 'assigned' | 'hint' | 'model' | 'default' | 'unknown'; material: SlotMaterial }

// state/sceneStore.ts
type DisplayMode = 'wireframe' | 'solid' | 'preview' | 'rendered'
type Look = 'clay' | 'lit'
```

```go
// backend/internal/creativescene/ops.go
type reparentObjectsOp struct {
	Op           string   `json:"op"`
	Moves        []move   `json:"moves"`
	CreateGroups []Object `json:"create_groups,omitempty"`
	RemoveGroups []string `json:"remove_groups,omitempty"`
}
type move struct {
	ObjectID  string     `json:"object_id"`
	ParentID  *string    `json:"parent_id"`
	Order     int        `json:"order"`
	Transform *Transform `json:"transform,omitempty"`
}
```

材质目录 `backend/internal/creativescene/materials.json`。外形如下，参数定义的外形与 `builtins.json` 相同：

```json
{"materials": [
  {"id": "principled@1", "kind": "principled", "label": "原理化 BSDF", "params": {
    "color": {"type": "color", "default": "#cccccc", "label": "基础色"},
    "metalness": {"type": "number", "default": 0, "min": 0, "max": 1, "step": 0.01, "label": "金属度"},
    "roughness": {"type": "number", "default": 0.5, "min": 0, "max": 1, "step": 0.01, "label": "糙度"},
    "ior": {"type": "number", "default": 1.5, "min": 1, "max": 2.333, "step": 0.01, "label": "IOR"},
    "opacity": {"type": "number", "default": 1, "min": 0, "max": 1, "step": 0.01, "label": "Alpha"}}},
  {"id": "stone_brick@1", "kind": "pattern", "label": "石砖", "params": {
    "color": {"type": "color", "default": "#8f8676", "label": "色调"},
    "brick_size": {"type": "number", "default": 0.3, "min": 0.05, "max": 2, "step": 0.01, "unit": "m", "label": "砖块尺寸"},
    "mortar_color": {"type": "color", "default": "#bdb5a6", "label": "灰缝颜色"},
    "roughness": {"type": "number", "default": 0.85, "min": 0, "max": 1, "step": 0.01, "label": "糙度"},
    "metalness": {"type": "number", "default": 0, "min": 0, "max": 1, "step": 0.01, "label": "金属度"}}}],
 "hints": {"stone": "stone@1", "rock": "stone@1", "brick": "stone_brick@1", "wood": "wood@1"}}
```

- 原理化 BSDF 的完整参数，沿用 three.js 的名字，转成蛇形写法：
  - `color`、`metalness`、`roughness`、`ior`、`opacity`；
  - `specular_intensity`、`specular_color`；
  - `transmission`、`thickness`；
  - `clearcoat`、`clearcoat_roughness`；
  - `sheen`、`sheen_roughness`、`sheen_color`；
  - `emissive`、`emissive_intensity`；
  - `anisotropy`、`anisotropy_rotation`（弧度，与 three.js 同义，界面显示为度，同聚光的 `angle`）；
  - `iridescence`、`iridescence_thickness`（纳米，赋给 `iridescenceThicknessRange` 的两端）。
  - 共 20 个键，在 32 个以内。

## 模块职责与协作

**前端 `frontend/src/creative-scene/`**

- `materials/`（新）：材质的全部逻辑，不依赖编辑器外壳，渲染端也用它。
  - `catalog.ts`：读取生成的 `materials.gen.json`，类型化，按 ID 查找，补默认值并夹到范围内。
  - `resolve.ts`：槽位解析（上表）。
  - `principled.ts`：参数 → `MeshPhysicalMaterial` 属性。
  - `patterns.ts`：每个预设的 GLSL 片段。
  - `inject.ts`：`onBeforeCompile` 注入与 `customProgramCacheKey`。
  - `instances.ts`：材质实例缓存，按「材质规格的规范化 JSON + 是否双面」共享，引用计数释放。
- `objects/`：`SceneObjects`、`ModelMesh` 按 `look` 选材质。
  - 白模用现有白模材质；材质预览和最终渲染用 `materials/` 解析的实例；线框用不可见材质加轮廓边。
  - 透明网格登记到 `render/transparent.ts` 的集合里，供 AO 跳过。
- `objects/SceneLights.tsx`（新）：把灯光物件挂成 three.js 灯光，只在 `look = 'lit'` 时挂载。
- `objects/Lighting.tsx`（新，替代 `Viewport.tsx` 里的 `Environment`）：按显示模式挂天空、太阳、环境贴图、环境光和雾。
  - 白模保持原 `Environment` 的内容不变，仍用 drei 的 `Sky`；
  - 最终渲染用 three 自带的 `Sky`（`three/examples/jsm/objects/Sky.js`）画背景，也用它生成环境贴图：r186 的这一版有 `showSunDisc`，drei 用的 three-stdlib 版没有。背景和环境贴图用同一个着色器，颜色才一致；
  - `ShotScene` 也用它。
- `render/sceneRenderer.ts`（由 `render/clayExport.ts` 改名，类名 `ClayRenderer` → `SceneRenderer`）：合成器不变，增加「透明网格在 AO 期间隐藏」的钩子；`Frame` 增加 `ao: boolean`（线框模式关闭）和 `exposure: number`（出帧前写入 `gl.toneMappingExposure`，`OutputPass` 读取，出帧后恢复）。
- `models/cache.ts`：`ModelParts.meshes[]` 增加 `slot`、`hint` 和材质常量；部件的矩阵烘焙进几何，使所有网格都在模型空间。
- `state/`：
  - `world.ts`（新）：按文档快照计算世界矩阵，并从世界矩阵换算局部变换；
  - `apply.ts`、`intents.ts`、`sync.ts`、`undo.ts`：`reparent_objects` 扩展的本地应用、单元计算、冲突重排和新的撤销步骤；
  - `sceneStore.ts`：`display` 状态。
- `grouping.ts`（新）：编组规则表的五个操作。输入快照和选择，输出 `reparent_objects` 及其反向操作。
- `panels/`：
  - `MaterialSlots.tsx`、`LightFields.tsx`、`EnvironmentPanel.tsx`（新）；
  - `Properties.tsx` 接入以上三个，并导出参数字段组件 `ParamField` 供复用；
  - `Outliner.tsx` 增加拖动；
  - `ExportDialog.tsx` 增加 `lit`。
- `host/`：`render.ts` 的 `ShotKind` 增加 `lit`，`openShooter` 能切换渲染方式；`exportShots.ts` 按渲染方式分批出图；`executor.ts` 的截图模式。
- `SceneEditor.tsx`、`input/keymap.ts`：新动作、菜单项和顶栏的显示模式按钮。

**后端 `backend/internal/creativescene/`**

- `ops.go`：`reparent_objects` 扩展的解码、涉及单元和应用。
- `materials.go`（新）：嵌入 `materials.json`，提供目录和助手路径的校验；`MaterialCatalog()` 给工具说明用。
- `lookat.go`：矩阵求逆和分解，供助手改挂换算。
- `reparent.go`（新）：助手改挂的预处理，与 `resolveLookAt` 并列。
- `agent.go`：
  - 去掉对 `reparent_objects` 的禁用；
  - 预处理顺序：材质槽按键合并 → 校验 → 改挂换算 → `look_at` 换算；
  - 拒绝 `hdri` 天空；
  - 变更摘要报告父级变化。
- `run_changes.go`：层级变化按前后快照判断，阻止对应物件的恢复。
- `jobs.go`：`render_view` 的 `mode`、`export_shots` 接受 `lit`。

**后端 `backend/internal/creativeagent/`**：`scene_tools.go` 的参数与说明（材质目录由 `creativescene.MaterialCatalog()` 生成，与 `BuiltinCatalog()` 同法），`instructions/base.md`。

**依赖方向不变**：`creativeagent` → `creativescene`。前端 `materials/` 不依赖 `panels/`、`SceneEditor`，渲染端按需加载时一起带上。

## 关键机制

### 视口出帧

```
useFrame(priority 1):
  mode = ui.display
  if mode == wireframe: gl.render(scene, camera); 画 top 覆盖层; return
  renderer.render(camera, {…, ao: true, dof: 相机视图且景深预览 ? lens : null})
```

- **切换显示模式**：只改 `objects/`、`Lighting`、`SceneLights` 的挂载内容，`SceneRenderer` 不重建。切换后 `invalidate()`。
- **材质预览的环境贴图**：
  - 第一次进入时用 `PMREMGenerator.fromScene(new RoomEnvironment(), 0.04)` 生成，缓存在 `Lighting` 模块级，页面内复用；
  - 离开编辑器时释放。

### 最终渲染的光照

- **曝光**：最终渲染、`lit` 导出和 `render_view` 的 `lit` 用 `2^render.exposure_ev`，其余为 1。曝光解决的是「各场景整体亮度不同」；下面的常数解决「同一场景里太阳、天空、环境光和灯光的比例」，只标定一次。
- **太阳**：方向沿用 `sunDirection`。强度和颜色随高度角 `el` 变化：

  ```
  k = smoothstep(-2°, 20°, el)
  intensity = SUN_MAX × k
  color = mix(#ffb46b, #fff4e6, smoothstep(0°, 35°, el))
  ```

  - `SUN_MAX` 暂定 3，由参考场景标定（假设，见未决事项 1）。
  - 投射阴影，阴影相机和 `normalBias` 沿用 `fitSunShadow`（贴合可见的投影物体，`normalBias` 取 1.5 个 texel），贴图 2048，与白模同一套。
- **天空环境贴图**：
  - 程序化天空：把 three 的 `Sky`（与背景相同的参数，`showSunDisc = 0`）单独放进一个场景，`PMREMGenerator.fromScene` 生成。太阳盘的亮度是天空本身的上万倍，留在环境贴图里会和太阳平行光重复计光：金属上出现两个太阳高光，漫反射也被加亮。天空参数变化时重新生成，节流到每 200 ms 一次；拖动太阳高度时，背景实时变，环境贴图稍后跟上。
  - 纯色天空：用 three addons 的 `ColorEnvironment`（天空色）生成。
  - `scene.environmentIntensity` 暂定 1，与 `SUN_MAX` 一起标定。
- **环境光**：天空环境贴图已经提供漫反射环境光，再加平的环境光会把明暗压平，所以只当补光用：`AmbientLight` 强度为场景值乘以 `AMBIENT_FILL`（暂定 0.5，与 `SUN_MAX` 一起标定），不加 0.3，也不挂半球光（这两项是白模的观感调校）。纯色天空没有天空的方向性，补光照常起作用。
- **灯光物件**：在物件内层 `docToView` 分组里挂 three.js 灯光，字段原样赋值。
  - `DirectionalLight` 和 `SpotLight` 的 `target` 是同组内局部 `(0, 0, −1)` 处的子物件，随旋转一起走。
  - `cast_shadow` 的灯光：点光、聚光的阴影贴图 1024；平行光物件的阴影相机也用 `fitSunShadow`。
  - 灯光数量变化会让 three.js 重新编译使用灯光的材质，卡顿由 AC-9 实测。
- **色调映射**：沿用 r3f 默认的 ACES。高亮（`Outlines`、`Edges`）在合成器里随画面一起经过 `OutputPass`，所以最终渲染下会随曝光变亮或变暗，可以接受。

### 材质着色

- **原理化 BSDF**：参数直接赋给 `MeshPhysicalMaterial`。
  - 有颜色和薄膜厚度区间两处换算，其余原样赋值。
  - 透射大于 0 时打开 three.js 的透射；`opacity < 1` 时设 `transparent = true`、`depthWrite = false`。
  - 没用到的特性（`clearcoat`、`sheen`、`transmission`、`iridescence`、`anisotropy` 为 0）由 three.js 自动省掉对应着色分支。
- **程序化预设**：在 `MeshPhysicalMaterial` 上用 `onBeforeCompile` 注入。
  - 程序缓存键为预设 ID；参数走 uniform；同一预设不同参数共用一个着色程序。
  - **坐标**：在顶点着色器里取模型空间位置，乘以世界矩阵各列长度得到的缩放：

    ```
    vPos = position * vec3(length(modelMatrix[0].xyz), length(modelMatrix[1].xyz), length(modelMatrix[2].xyz))
    ```

    模型空间法线传给片元着色器，用于投影权重。纹理因此贴在物件上，移动、旋转时不滑动；按真实米数铺，缩放时不拉伸。
    - 模型的部件矩阵在加载时烘焙进几何，所有部件共用模型空间，相邻部件的纹理连续。
    - 基础体本身就在物件空间。
  - **两类图案**：
    - 噪声类（灰泥、混凝土、岩石、大理石、木纹、泥土、草地）直接在三维位置上求值（实体纹理），不需要投影，没有接缝；
    - 平面图案类（石砖、瓷砖、木地板、布料）按法线主轴做三平面投影，用 `pow(|n|, 4)` 作权重混合，砖缝在棱角处可能错开，接受。
  - **输出**：每个预设的片段输出四项，分别替换 `map_fragment`、`roughnessmap_fragment`、`metalnessmap_fragment`、`normal_fragment_maps` 对应的值：
    - 基础色 `diffuseColor.rgb`；
    - 糙度；
    - 金属度；
    - 高度 `h`：凹凸用屏幕空间导数扰动法线（与 three.js `bumpmap_pars_fragment` 的 `dHdxy_fwd` 同法，高度由函数给出，不读贴图）。
  - **噪声函数**：一份 3D 值噪声 + fbm + Voronoi（岩石、草地用）的公共片段，所有预设共用。
  - **抗锯齿**：合成器的多重采样只对几何边缘有效，对着色器内部的图案无效。2048 导出和远处的墙上，硬边图案和高频噪声会走样、闪烁，用屏幕空间导数做凹凸会放大走样，`lit` 图带上 CG 噪点，正好会诱发「照抄 CG 质感」。因此 `patterns.ts` 定两条规则：
    - 硬边（砖缝、瓷砖缝、板缝）用 `fwidth` 驱动的 `smoothstep` 过渡，不用阶跃；
    - fbm 按 `fwidth(vPos)` 估计每像素覆盖的尺度，频率高于约半个像素的八度逐渐淡出；凹凸的高度用同样淡出后的值，远处随之变平。
  - **注入失败**：three 升级后着色器片段名变化时，注入函数检测到缺少锚点，返回 false。这时该材质退回原理化 BSDF（只用颜色、糙度、金属度），并在控制台告警。做法与 C4 的 `lookAlongZ` 相同，有单测锁定锚点。
- **实例缓存**：
  - 键是 `{kind, id, params(补默认后按键排序), doubleSided}` 的 JSON；
  - 引用计数归零后在下一帧释放（防止切换模式时反复编译）；
  - 首批 14 个预设加原理化，每个场景实际用到的程序数在 20 个以内。

### AO 跳过透明网格

- `SceneRenderer` 的 AO 前钩子在隐藏 `aside` 覆盖层的同时，隐藏 `render/transparent.ts` 集合里当前可见的网格，AO 后恢复。
- 集合由 `objects/` 在材质变化时增删，不每帧遍历场景。
- 景深读 GTAO 的深度，因此透明网格处按它后面物体的距离虚化。

### `lit` 导出与 `render_view`

- `openShooter(scene, models, look)` 挂一次 `ShotScene`。新增 `shooter.setLook(look)`：用新的 `look` 重新 `root.render`，等提交后调用 `gl.compileAsync(scene, camera)`，保证第一张图不缺材质。
- `shoot({kind})`：
  - `clay`、`lit` 走 `SceneRenderer`，景深两者都按摄像机；
  - `line` 走 `LineRenderer`。线稿用覆盖材质，与渲染方式无关。
- `renderShots` 按渲染方式分批：
  1. 先用 `clay` 出全部摄像机的白模和线稿；
  2. 若有 `lit`，切到 `lit`，再出全部摄像机的材质光照图；
  3. 返回顺序按原来的「摄像机 × 方式」排列。
  - 每张图 30 秒超时不变。切换本身的超时为 60 秒，超时按出图失败处理。
- `render_view` 的 `mode` 由任务请求带到渲染端。`renderView` 按它打开 shooter，长边仍 1024，JPEG。
- **后端**：
  - `jobs.go` 解析 `render_view` 的 `mode`（缺省 `clay`，只接受 `clay`、`lit`）；
  - `exportJobPasses` 加 `lit`，省略 `passes` 时仍为 `clay` + `line`。
- **导出对话框**：三种方式。默认勾选白模、线稿；当前显示模式为材质预览或最终渲染时也勾选材质光照。不记住上次的选择，保持规则简单。

### 编组：编辑器侧

```
grouping.group(view, selection) -> { op: reparent_objects, inverse: reparent_objects } | { error }
  roots = topmost(selection)
  W = world(view, id) for id in roots
  G = 新分组 {id: newID(), kind: group, parent: 共同父级或 null, order: …, transform: {position: avg(W.position), rotation: id, scale: 1}}
  moves = roots.map((id, i) => {object_id: id, parent_id: G.id, order: i, transform: localOf(inverse(world(G)) × W[id])})
  inverse = { remove_groups: [G.id], moves: roots.map(原父级、原顺序、原局部变换) }
```

- 五个操作都返回「正向操作 + 反向操作」。反向操作在编辑时按当时的快照算好，存进撤销步骤：
  - 取消编组的反向用 `create_groups` 以原 ID 重建分组。服务端只拒绝与当前快照重复的 ID，删除后重建同一 ID 是允许的，现有的撤销删除也是这样。
- **本地应用**（`state/apply.ts`）与服务端完全一致，顺序为新建 → 改挂（带变换时替换）→ 删除。确认后本地基线由它推出，不重读。
- **`moves` 至少一项**（契约 1–2000）：取消编组时，选中的分组全都没有子物件，就改发 `remove_objects`，撤销走现有的 `restore_units` 步骤；新建空分组仍用 `add_objects`。
- **单元计算**（`state/intents.ts`）：
  - `touchedUnits`：`objects`，加上带变换的移动的 `object:<id>`，以及删除的分组的两个单元；
  - `changedUnits`：加上新建分组的两个单元，否则紧接着改名的请求会带错 `expected` 而冲突；
  - `removedUnits`：加上删除的分组。
- **没有改变的改挂不提交**：父级、顺序、变换都与当前相同的移动被 `withoutNoops` 去掉；全部去掉且没有新建和删除时，不提交。变换按容差比较（位置和缩放各分量相差 < 1e-6，四元数 `|dot| > 1 − 1e-9`），重算出来的局部变换在浮点下几乎不会精确相等。
- **冲突重排**（`state/sync.ts`）：只在 `objects` 一个单元冲突时（别人同时增删或改挂了别的物件）自动重试：
  1. 读新的基线；
  2. 检查涉及的物件、父级和要删除的分组仍存在，且父级仍是分组；不满足就走现有的冲突提示；
  3. 顺序与新的兄弟冲突的，排到该父级末尾，保持本次各物件之间的相对顺序；
  4. 变换不重算，理由见「编组规则 · 并发」；
  5. 用新的 `expected` 重新排队。
  - 摆放单元冲突时不自动重试，走现有冲突提示。
- `keepPresent`（重读后丢弃已不存在的物件）把本次新建的分组视为存在。分组、父级或要删除的分组缺失时整条丢弃，不拆开执行。

### 编组：撤销

- 新增撤销步骤类型 `ops`（现有步骤都是 `restore_units`），存 `undo`、`redo` 两个 `reparent_objects`，以及步骤完成后各相关单元的版本 `after`。
- 相关单元包括：带变换的移动的摆放单元，以及新建和删除的分组的两个单元。
- **撤销时**：
  1. 相关单元的当前版本都等于 `after`，否则按现有规则跳过并提示「之后被别人改过」；
  2. 反向操作里的父级都存在且是分组，否则跳过并提示；
  3. 顺序冲突按「冲突重排」的第 3 步处理；
  4. 以当前 `objects` 版本为 `expected` 提交。
- 重做对称。
- `record` 遇到含 `reparent_objects` 的意图时，记 `ops` 步骤；其余仍记 `restore_units` 步骤。同一意图里不会混合两者：编组操作单独成批。
- **画布外的撤销**：`restore_units` 恢复不了层级（服务端规则），所以历史版本列表的整份恢复仍是唯一能回到任意旧层级的方式，不变。
- **后续选项**（不在 C5 做）：本轮改动清单的「恢复这一处」目前对改挂过的物件直接阻止（决定 4）。有了带变换的 `reparent_objects`，它其实可以对这类物件发一次改挂，回到旧父级、旧顺序和旧变换，条件是旧父级仍在并且是分组。记在这里，避免以后以为做不到。

### 编组：助手

- **工具层预处理** `resolveReparent`，在 `AgentEditInTx` 里 `resolveLookAt` 之前对原始 JSON 改写。
- **助手写的外形**：
  - `{op:"reparent_objects", moves:[{object_id, parent_id, order?}], create_groups?:[{id, name, parent_id, order?, transform?}], remove_groups?:[id]}`；
  - 不写移动的 `transform`；写了会被拒绝，并提示改挂会自动保持位置。
- **服务端补齐**：
  - 每个移动的 `transform`，按 `local = inverse(新父级世界) × 物件世界` 计算。新父级是本次新建的分组时，用它的变换和它父级的世界矩阵。
  - 省略的 `order`：按出现顺序排到父级末尾。
  - 新建分组省略 `transform` 时，放在被挂进它的物件的世界位置中点，无旋转。
- **拒绝的情形**，说明原因：
  - 同一批里还移动、改挂了这些物件或它们的祖先（同 `look_at` 的规则）；
  - 分解出的缩放或位置越界。
- **矩阵**：`lookat.go` 增加 `mat4.invert` 和 `decompose`（与 three.js `Matrix4.invert`、`decompose` 一致，含行列式为负时翻转 X 缩放），用 three.js 的数值对照测试。
- **运行改动**（`run_changes.go`）：
  - 先用修订行的 `changed_units` 筛出候选：`objects` 有变化，并且有已存在物件的摆放单元一起变化。修订行没有存操作列表，所以按单元筛选。
  - 「已存在」从运行首个修订之前的快照出发，按历史行里新增、删除的单元逐条推进；遇到整份恢复就重读那一版。实施时这样做，而不是读前一修订的 `unit_revs`：不多查修订表，那份快照条目本来就要读。只增删物件的修订（例如助手大批 `add_objects`）不是候选，不读它前后的快照；
  - 候选修订读它与前一修订的快照，比较两边都存在的物件的 `parent_id` 和 `order`；
  - 有变化的修订记入层级集合；
  - 这些物件，以及它们改挂进、改挂出的分组，条目都以 `hierarchy_not_restorable` 阻止恢复。分组也要阻止：撤销本轮会删掉新建的分组，按单元恢复的规则把子物件上移，子物件就跳位了。`{kind: unit, unit: objects}` 条目保留，候选修订也计入它；
  - 只读运行内、`objects` 有变化的修订，最多 5000 条历史的上限不变。
- **变更摘要**（`summarizeChanges`）：比较前后快照，对两边都存在、父级变了的物件报「改挂到 <分组名>/根」。新建的分组照常按新增物件报告。

### 助手：材质、灯光与环境

- **材质槽按键合并**（只在助手路径）：
  - 助手的 `update_objects.material_slots` 按槽位合并到当前值，写 `null` 清除该槽位；
  - 预处理成完整的映射后再交给命令（命令接口仍是整体替换）；
  - `add_objects` 照写。
- **目录校验**（只在助手路径），任一失败，整批不写入，错误列出可用项：
  - 槽位名必须是该物件的槽位：基础体为 `main`，模型取 manifest；
  - `preset` 在目录里；
  - 参数名在该材质的参数表里；
  - 数值在范围内，颜色是 `#rrggbb`，枚举在选项里。
- **HDRI**：`set_environment` 的 `sky.kind = "hdri"` 返回「暂无内置 HDRI，请用程序化天空或纯色」。
- **曝光**：`set_render{render:{exposure_ev}}`，按键合并。说明里写明它只影响 `lit`，并给出标定后各类场景的建议值（例如室外正午 0、只有灯光的室内 +1～+3）。
- **`edit_scene` 说明**：
  - 删掉「暂不显示，先别打光」；
  - 加入材质目录：由 `MaterialCatalog()` 生成，每个预设一行，写 ID、名称和参数范围；
  - 加入灯光常用强度：太阳由天空决定；室内台灯、壁灯的点光 20–80 cd；聚光舞台灯 200–1000 cd；平行光补光 0.5–2；投射阴影的灯光建议不超过 4 盏；
  - 加入显示模式和 `render_view` 的 `mode`；
  - 加入改挂的写法。
  - 数值在标定后写定（未决事项 1）。
- **`render_view`**：参数 `mode: clay|lit`。
- **`export_shots`**：`passes` 枚举加 `lit`。
- **`instructions/base.md`**：保守写法，`lit` 图作为材质和光影方向的参考，不要求照搬画面质感；按 AC-10 的结论修改。

### 属性栏与复制材质

- **「材质」区**（`panels/MaterialSlots.tsx`）：基础体和模型实例显示。
  - 每个槽位一行：槽位名（及提示）、材质下拉（原理化 BSDF 在前，预设按目录顺序）、当前来源标记（「按提示」「模型自带」「未知材质」）。
  - 展开后按目录的参数定义出字段，复用 `ParamField`。
  - 拖动滑块时用合并键 `${id}:slot.${slot}.${param}`，与相机参数相同，松手才成一步撤销。
  - **多选**：选材质或改参数时，作用于所有选中物件里同名的槽位，合成一个 `update_objects`。只有活动物件有这个槽位时，只改它。
  - **「转为原理化」**：
    - 漆面、自发光按对应字段转换；
    - 带图案的预设取色调、糙度、金属度，图案丢弃（界面提示）。
- **复制材质**：
  - **「复制材质到选中物件」**（物件菜单、右键菜单）：活动物件的 `material_slots` 按槽位名复制给其他选中物件；活动物件只有一个槽位时，复制给目标的全部槽位。目标没有对应槽位的跳过。整个操作是一个 `update_objects`。
  - **槽位右键「复制材质」「粘贴材质」**：剪贴板存 `localStorage` 的 `creative-scene:material-clipboard`，内容是 `{preset, params}`，可以跨场景粘贴。
- **「灯光」区**（`panels/LightFields.tsx`）：类型、颜色、强度（标注单位）、投射阴影、距离、衰减、光锥角（度，存弧度）、柔化。不在最终渲染模式时显示提示。拖动用合并键。
- **「环境」面板**（`panels/EnvironmentPanel.tsx`）：没有活动物件时显示在属性栏的位置。
  - `set_environment` 整体替换，按当前值改一项后提交；
  - 拖动用合并键 `environment.<field>`；
  - `state/intents.ts` 的合并规则扩展到 `set_environment`，现在只合并 `update_objects`。

## 局部设计与偏差

1. **白模光照不改**：
   - 白模的 +0.3 环境光、半球光、±30 m 的固定阴影范围都是 C4 以前的观感调校，改了会让 C4 的白模导出和回归截图变化。
   - 新的、按物理取值的光照只用于最终渲染。
   - 代价：同一场景两种模式的明暗不完全可比。
2. **模型部件矩阵烘焙进几何**：
   - 三平面投影和实体纹理需要所有部件共用模型空间，否则相邻部件在接缝处图案对不上。
   - 加载时对每个部件 `applyMatrix4` 到几何上；几何被多个部件共用时先克隆。
   - 矩阵行列式为负（镜像部件）时，`applyMatrix4` 不改三角形的绕序。烘焙前 three.js 会按物件矩阵自动翻转正面，烘焙后矩阵是单位矩阵，就不再翻转，内外面会反过来；所以烘焙时检测到 `det < 0` 就交换每个三角形的两个顶点索引。
   - 内存最多翻倍，只在共用几何时发生，GLTFExporter 输出的模型很少这样。
   - 测试里 `meshes[].matrix` 的断言改为单位矩阵。
3. **噪声类材质用实体纹理，不投影**：没有接缝，比三平面便宜。只有砖、瓷砖、木地板、布料这类平面图案需要投影。
4. **改挂不检查父级链的摆放单元**：理由见「编组规则 · 并发」。把父级链加进版本检查，会让编组和任何移动父级的操作互相冲突。客户端算的单元集合与服务端不一致时，会报 400，而不是 409。
5. **线框模式下网格不画但可拾取**：与 Blender 透视模式一致。点击仍拾取最前面的表面，框选包含被挡住的物件（`SelectionBox` 本来就不看遮挡）。
6. **助手的材质槽按键合并只在工具层**：命令接口的「整体替换」不变，编辑器照旧整体写。助手只写要改的槽位，不会把别的槽位清掉。
7. **嵌套分组一起删除**（实施 1.1 时补充）：契约要求 `remove_groups` 删除时没有子物件。同时选中外层和内层分组取消编组时，内层分组在删除前仍是外层的子物件。服务端把「也在本次 `remove_groups` 里的子分组」不算作子物件，两层一起删除；其他子物件仍拒绝。编辑器取消嵌套分组时，把内层的子物件挂到最近的、不被删除的祖先下。

## 接口与数据契约

契约状态：**扩展**，全部兼容。

- `api: 提交场景命令`：`reparent_objects` 增加 `moves[].transform`、`create_groups`、`remove_groups`（Epic 修订 v10）。
  - `api/openapi.yaml` 的 `CreativeSceneReparentObjects`：`moves` 仍为 1–2000 项；新增两个可选数组，`create_groups` 的元素引用 `CreativeSceneObject`。
  - `CreativeSceneMove` 增加可选的 `transform`。
  - 运行 `make generate`。
- `api: 提交场景命令`：`set_render` 由整体替换改为按键合并。旧客户端总写全部字段，结果不变。
- `data: 场景文档`：`render.exposure_ev`（−10…10，缺省 0）。`model.go` 解码时补 0，读取场景的响应带齐；OpenAPI 的 `CreativeSceneRender` 增加可选的 `exposure_ev`。
  - 指纹：`fingerprint.go` 的 `lit` 投影在曝光不为 0 时写入它；`clay`、`line` 和曝光为 0 的 `lit` 指纹不变，不升版本。
- `data: 场景客户端任务`：`render_view` 请求的 `mode` 接受 `lit`。任务请求只在后端 JSON 里，OpenAPI 不变。
- `api: 读取运行的场景改动`：外形不变，`hierarchy_not_restorable` 的适用范围扩大。
- **助手工具参数**（不在 OpenAPI 里）：
  - `edit_scene` 的 `op` 枚举加 `reparent_objects`；
  - `render_view` 加 `mode`；
  - `export_shots` 的 `passes` 加 `lit`。
  - 这些都会改变检查点指纹，按「排空后升级」上线。
- **新增共享数据**：`backend/internal/creativescene/materials.json` 由 `make generate` 复制成 `frontend/src/creative-scene/materials/materials.gen.json`，`generate-check` 检查两者一致，与 `builtins.json` 同法。

## 挂载点

| 位置 | 动作 | 说明 |
|---|---|---|
| `backend/internal/creativescene/materials.json` | 新增 | 材质目录：原理化 BSDF、14 种预设、提示映射 |
| `backend/internal/creativescene/materials.go` | 新增 | 嵌入与解析目录（未知字段报错）、`MaterialCatalog()`、助手路径的槽位校验 |
| `backend/internal/creativescene/ops.go` | 修改 | `reparentObjectsOp`、`move` 扩展；`decodeOp`、`touched`、`apply`；`set_render` 按键合并 |
| `backend/internal/creativescene/model.go` | 修改 | `Render.ExposureEV` 的校验与解码补默认值 |
| `backend/internal/creativescene/fingerprint.go` | 修改 | `lit` 投影计入非 0 的曝光 |
| `backend/internal/creativescene/lookat.go` | 修改 | `mat4.invert`、`decompose` |
| `backend/internal/creativescene/reparent.go` | 新增 | `resolveReparent`：助手改挂的保持位置换算与补齐 |
| `backend/internal/creativescene/agent.go` | 修改 | `opsNotForTheAssistant` 去掉改挂；`AgentEditInTx` 预处理（材质槽合并与校验、HDRI、改挂）；`summarizeChanges` 报改挂 |
| `backend/internal/creativescene/run_changes.go` | 修改 | 层级变化按快照判断，阻止对应物件的恢复 |
| `backend/internal/creativescene/jobs.go` | 修改 | `render_view` 的 `mode`；`exportJobPasses` 加 `lit` |
| `backend/internal/creativeagent/scene_tools.go` | 修改 | `edit_scene`、`render_view`、`export_shots` 的参数与说明 |
| `backend/internal/creativeagent/instructions/base.md` | 修改 | `lit` 图的重绘用法 |
| `api/openapi.yaml` | 修改 | `CreativeSceneReparentObjects`、`CreativeSceneMove`、`CreativeSceneRender` |
| `backend/internal/platform/httpapi/api.gen.go`、`frontend/src/api/schema.d.ts` | 生成 | `make generate` |
| `Makefile` | 修改 | `generate` 复制 `materials.json`，`generate-check` 比对 |
| `frontend/src/creative-scene/materials/`（`catalog.ts`、`resolve.ts`、`principled.ts`、`patterns.ts`、`inject.ts`、`instances.ts`、`materials.gen.json`） | 新增 | 材质目录、解析、着色与实例缓存 |
| `frontend/src/creative-scene/objects/SceneObjects.tsx` | 修改 | 按 `look`、显示模式选材质；线框的轮廓边；透明网格登记；挂 `SceneLights` |
| `frontend/src/creative-scene/objects/ModelMesh.tsx` | 修改 | 每个部件按槽位解析材质 |
| `frontend/src/creative-scene/objects/look.ts` | 修改 | 线框和中性灰的颜色常量 |
| `frontend/src/creative-scene/objects/SceneLights.tsx` | 新增 | 灯光物件 → three.js 灯光 |
| `frontend/src/creative-scene/objects/Lighting.tsx` | 新增 | 各显示模式的天空、太阳、环境贴图、环境光和雾；白模部分从 `Viewport.tsx:Environment` 原样搬来 |
| `frontend/src/creative-scene/models/cache.ts` | 修改 | `ModelParts` 带槽位、提示、材质常量；部件矩阵烘焙进几何 |
| `frontend/src/creative-scene/render/clayExport.ts` → `render/sceneRenderer.ts` | 改名并修改 | `SceneRenderer`；AO 期间隐藏透明网格；`Frame.ao` |
| `frontend/src/creative-scene/render/transparent.ts` | 新增 | 透明网格集合 |
| `frontend/src/creative-scene/Viewport.tsx` | 修改 | 用 `Lighting` 代替 `Environment`；`ClayView` 按显示模式出帧 |
| `frontend/src/creative-scene/state/sceneStore.ts` | 修改 | `display` 状态与初值 |
| `frontend/src/creative-scene/viewState.ts` | 修改 | `SavedView.display` 的读写与兜底 |
| `frontend/src/creative-scene/state/world.ts` | 新增 | 世界矩阵与保持位置的局部变换 |
| `frontend/src/creative-scene/grouping.ts` | 新增 | 编组、取消编组、设置和清除父级、列表移动，各带反向操作 |
| `frontend/src/creative-scene/state/apply.ts` | 修改 | `reparent_objects` 扩展的本地应用；`withoutNoops` 按容差去掉无变化的移动；`set_render` 按键合并 |
| `frontend/src/creative-scene/state/intents.ts` | 修改 | 涉及、改变、删除的单元；`set_environment` 的合并 |
| `frontend/src/creative-scene/state/sync.ts` | 修改 | `keepPresent` 认新建的分组；`objects` 冲突时重排顺序后重试 |
| `frontend/src/creative-scene/state/undo.ts` | 修改 | `ops` 步骤类型：有效性检查、顺序重排、撤销与重做 |
| `frontend/src/creative-scene/agentActivity.ts` | 修改 | `editedObjects` 认 `create_groups`、`remove_groups` |
| `frontend/src/creative-scene/input/keymap.ts` | 修改 | 编组、取消编组、设置和清除父级、显示模式菜单、线框切换 |
| `frontend/src/creative-scene/SceneEditor.tsx` | 修改 | 新动作；物件菜单的编组项和「复制材质到选中物件」；添加菜单的灯光；顶栏显示模式按钮和 `Z` 菜单 |
| `frontend/src/creative-scene/editing.ts` | 修改 | `newLight`（三种灯光） |
| `frontend/src/creative-scene/panels/Properties.tsx` | 修改 | 接入材质、灯光、环境；导出 `ParamField` |
| `frontend/src/creative-scene/panels/MaterialSlots.tsx`、`LightFields.tsx`、`EnvironmentPanel.tsx` | 新增 | 见「属性栏与复制材质」 |
| `frontend/src/creative-scene/panels/Outliner.tsx` | 修改 | 拖动：之前、之后、放入 |
| `frontend/src/creative-scene/panels/ExportDialog.tsx` | 修改 | `lit` 与默认勾选规则 |
| `frontend/src/creative-scene/host/render.ts` | 修改 | `ShotKind` 加 `lit`；`openShooter` 的 `look` 与 `setLook`；`renderView` 的模式 |
| `frontend/src/creative-scene/host/ShotScene.tsx` | 修改 | 按 `look` 挂 `Lighting`、`SceneLights` 和材质 |
| `frontend/src/creative-scene/host/exportShots.ts` | 修改 | 按渲染方式分批；`lit` 带景深 |
| `frontend/src/creative-scene/host/executor.ts`、`host/framing.ts` | 修改 | `render_view` 任务的 `mode` |
| `frontend/src/creative-scene/scene.css` | 修改 | 显示模式按钮、材质区、拖放指示 |
| `docs/dev/agent-skills-tools.md`、`docs/dev/creative-scene-agent.md` | 修改 | 助手可改挂；材质、灯光、`lit` 的用法 |

## 验证与交付

| 验收 | 验证 |
|---|---|
| AC-1 显示模式 | 前端单测：`display` 的读写与兜底（`viewState`），`render.mode` 初值。e2e：四种模式的截图像素检查（线框背景占比、材质预览在删灯前后像素相同、最终渲染有阴影区），`Z` 菜单与 `Shift + Z`，重开保持，撤销不影响 |
| AC-2 材质 | 单测：解析表五条、未知预设、提示映射、参数夹取、实例缓存共享与释放、注入锚点。e2e：方块放大 4 倍后同一块砖的屏幕宽度不变（取相同距离截图比较周期）；原理化金属反射天空（亮度方差）；金属球上只有一个太阳高光（高光连通区域计数为 1，环境贴图不含太阳盘）；远处砖墙在相机平移半个像素前后的像素差低于阈值（抗锯齿）；Alpha 0.3 能看到后方物件且该处无 AO（对比 `transparent` 集合开关）。单测：镜像部件烘焙后绕序翻转 |
| AC-3 复制材质 | 单测：按槽位名复制、单槽位扩散、跳过无对应槽位。e2e：一次撤销恢复；跨场景粘贴 |
| AC-4、AC-5 灯光与环境 | 单测：灯光字段 → three.js 属性（含角度换算、target 方向）。e2e：加聚光后光锥内外亮度差、阴影区；改类型保留字段；太阳 5° 时影子变长（阴影像素增多）；纯色与雾；曝光补偿 +2 时最终渲染平均亮度上升、白模不变。Go：`set_render` 按键合并、`exposure_ev` 的范围与补默认、`lit` 指纹计入曝光且为 0 时与原指纹相同 |
| AC-6 `lit` 导出 | 单测：`renderShots` 分批顺序与结果映射、`lit` 带景深、对话框默认勾选。Go：`export_shots` 接受 `lit`，`render_view` 的 `mode`。e2e：最终渲染模式导出 9 张，`lit` 图与相机视图截图的结构相似度达到阈值（C4 白模用的同一检查） |
| AC-7 编组 | Go：扩展的解码、单元、应用、各失败情形、`Validate`；与 three.js 对照的求逆和分解。前端单测：`grouping` 五个操作的保持位置（世界矩阵误差 < 1e-6）、反向操作精确回到原父级、顺序与变换；`apply` 与服务端一致（同一用例两边比对）；冲突重排；撤销跳过规则。e2e：Ctrl + G、Ctrl + Alt + G、Ctrl + P 提示、Alt + P、列表拖进旋转缩放过的分组 |
| AC-8 助手 | Go：材质槽合并与目录校验的错误信息、HDRI 拒绝、改挂预处理（含新建分组、省略顺序、拒绝同批移动祖先）、运行改动的层级阻止、变更摘要的改挂。助手 e2e：黄昏布置样例（mock 模型脚本），`render_view` 的 `lit` |
| AC-9 性能 | 参考场景 fixture（200 物件、8 种材质、4 盏灯其中 2 盏投射阴影），沿用 C4 的帧时间测法，记录白模、材质预览、最终渲染、线框的帧时间和加灯卡顿，写进本设计 |
| AC-10 重绘对照 | owner 手动：同一场景 3 机位两组参考各重绘一次，保存原图、重绘图、组合和提示词到 `discovery/research/lit-repaint.md` |

- **回归**：C4 的白模和线稿导出测试、景深与 AO 测试、助手导出测试保持通过。白模模式的观感不变，由 `Lighting` 的白模分支原样搬迁保证，e2e 比较搬迁前后同一视角的截图。
- **门禁**：日常 `make check-frontend`、`make check-go PKG=./internal/creativescene/... PKG=./internal/creativeagent/...`、`make generate-check`；合回 develop 用 `make check-merge`。
- **上线**：工具参数变化，按「排空后升级」上线。无迁移，无功能开关（项目未对外上线）。

## 未决事项

| # | 问题 / 假设 | 影响 | 解决方式 | 最晚解决时点 |
|---|---|---|---|---|
| 1 | 最终渲染的强度常数：`SUN_MAX = 3`、环境贴图强度 1、`AMBIENT_FILL = 0.5`、工具说明里的常用灯光强度和各类场景的建议曝光都是假设 | 最终渲染与 `lit` 导出的明暗、助手写的灯光强度和曝光 | 用参考场景（室外正午、黄昏、室内两盏壁灯）标定：常数只定一组，各场景的差别由曝光补偿吸收；结果写回本节和工具说明 | 灯光任务完成前 |
| 2 | 最终渲染的帧时间（AC-9）：透射、多盏投射阴影的点光是否超预算 | 是否限制投射阴影的灯光数量、视口透射降分辨率 | AC-9 实测；超预算时先降阴影贴图和透射分辨率，仍超再提给 owner | 验收前 |
| 3 | 14 个预设的具体图案和默认参数 | 观感 | 实现时逐个出样张，验收时 owner 看样张；不改 ID 和参数名 | 验收前 |
