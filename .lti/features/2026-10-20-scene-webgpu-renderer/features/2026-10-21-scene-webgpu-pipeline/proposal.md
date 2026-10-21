---
risk:
  factors:
    - perf-sensitive
    - broad-impact
behavior: changed
reads:
  - creative/scene
  - creative/library
decisionsPending: false
---
# 本次交付

## Why

Epic `scene-webgpu-renderer` 基线 v2（owner 2026-10-21 批准）的 R1。roadmap 交给 R1 的问题是：**迁移后的画面和性能能否与迁移前持平**。

R1 只做等价迁移，不加新效果：3D 编辑器的全部渲染从 WebGL 管线换到 `WebGPURenderer` + TSL，换完以后渲染器就是「工作台」。这样 AC-1 的对照有明确的判断标准，出了偏差能确定是迁移造成的。实时 GI、渲染选项卡和输出分辨率都留给 R2，R2 在 R1 的新管线上加能力。

R0（`scene-webgpu-spike`）已经回答了 R1 依赖的技术问题（帧推进、G-buffer、读回、编辑器辅助、正交 GTAO、天空），结论在 Epic design 里；对照基线（参考场景、WebGL 基线图、性能基线）在 R0 的 `discovery/research/baseline/`。

本次完整交付两条目标契约：`architecture: 场景模块`（渲染端的技术基线）和 `patterns: 场景着色写法`（含静态检查）。

## What Changes

1. **渲染器创建入口**（新）：前端唯一创建 `WebGPURenderer` 的地方，负责后端选择与自动退回 WebGL2、按适配器上限申请设备限制、接管帧序号、订阅设备丢失。视口、资产 3D 预览、离屏渲染端、模型与资产卡片缩略图都从这里拿渲染器。
2. **渲染管线 `SceneRenderer`**：内部从 `EffectComposer` 改为 `RenderPipeline`，只组装工作台：不透明预通道 → GTAO（项目内 TSL 版，透视与正交共用）→ 主画面（MSAA 4×）× AO → 景深 → 输出。对外形状（`render(camera, frame)` 与覆盖层约定）不变。线稿改写为 TSL 节点。
3. **材质**：原理化 BSDF 映射到 `MeshPhysicalNodeMaterial`；14 个程序化预设改写为 TSL `Fn`，删除 GLSL 注入。预设不升版本。
4. **天空与阴影**：白模和最终渲染的两种天空各改写为一个 TSL 函数；阴影用 `PCFShadowMap`、半径 1，保留包围盒拟合。
5. **编辑器辅助**：网格、选中描边、平面物件与助手高亮的粗线、视图 gizmo 换成能在 `WebGPURenderer` 下运行的实现。
6. **离屏出图**：`render_view`、手动导出、助手导出、模型烘焙缩略图都改走新渲染器；每张图从帧序号 0 开始显式推进；仍是「画到画布后立刻 `toDataURL`」。导出仍是长边 2048、PNG。
7. **退回与设备丢失**：设备探测改为异步的准确探测，「WebGL2 能拿到，或 WebGPU 能拿到适配器」；视口在设备丢失后自动重建；离屏任务丢设备时整批丢弃、不报失败、停止续租，让任务回队，本页暂停认领直到设备恢复；缩略图丢设备时重建后重试。
8. **删除与防回退**：删除 `EffectComposer` 管线、GLSL 着色器、`Sky.js` 和不兼容的 drei 组件；`check-frontend` 新增静态检查，出现这些写法时失败。
9. **验证**：迁移对照（对 R0 的 WebGL 基线）、隐藏标签页连拍确定性、WebGL2 后端、设备丢失、性能对比各有 e2e；对照图、差异图和性能数据存进 evidence。

不改后端、HTTP 接口、OpenAPI 和场景格式。

## Acceptance Criteria

整体验收（AC-1 至 AC-9）留在 Epic；R1 承担 AC-1、AC-2、AC-3 的全部和 AC-7 的工作台部分，以及 Epic 未决事项 6、11。

- **AC-R1-1 迁移等价**（Epic AC-1）：**WHEN** 在参考设备上对 R0 的六组参考场景各导出 `clay`、`line`、`lit`，并截正交视图的白模，**THEN**
  - 每张图与 R0 的 WebGL 基线并排对照，附差异图，存进 evidence，owner 判断是「同一张图」；
  - 自动化阈值逐张通过：`line` 平均差 ≤ 0.1、差 > 8 的像素 ≤ 0.05%；`clay`（含正交白模）和 `lit` 平均差 ≤ 1.5、差 > 8 的像素 ≤ 1%。第一次跑通后按实测值的 1.5 倍收紧（不超过上述值）；超出阈值交 owner 看图，不放宽；
  - 材质板覆盖全部 14 个预设，预设版本号不变。
- **AC-R1-2 全部走新渲染器**（Epic AC-2 前半）：**WHEN** 在编辑器视口、`render_view`、手动导出、助手导出、模型缩略图、资产卡片缩略图和资产 3D 预览中渲染，**THEN** 用的都是创建入口给出的 `WebGPURenderer`；`check-frontend` 的静态检查在 `frontend/src` 出现 `WebGLRenderer`、`WebGLRenderTarget`、`ShaderMaterial`、`RawShaderMaterial`、`onBeforeCompile`、`ShaderLib`、`ShaderChunk`、GLSL 源码字符串、`three/examples/jsm/postprocessing/*`、`three/addons/objects/Sky.js`、非 WebGPU 版的 `lines/*` 或白名单以外的 drei 组件时失败（用一个故意违规的样例证明它会失败）。
- **AC-R1-3 结果确定**（Epic AC-2 后半）：**WHEN** 在真实 Chrome 的隐藏标签页里，对同一场景连续两次执行包含多台摄像机和 `clay`、`line`、`lit` 三种方式的导出，**THEN** 两次结果逐像素相同，每张图都是各自摄像机和导出方式的正确画面（与前台导出的同一张图逐像素相同）。
- **AC-R1-4 退回 WebGL2**（Epic AC-3 前半）：
  - **画面**：**WHEN** 在参考设备上用测试参数强制 WebGL2 后端（`forceWebGL`）导出参考场景，**THEN** 与同一设备 WebGPU 后端的同一张图在 AC-R1-1 的阈值内。
  - **功能**：**WHEN** 浏览器没有 WebGPU 适配器（`--use-angle=swiftshader`，不加 `--enable-unsafe-webgpu`），**THEN** 自动退回 WebGL2，编辑器照常打开、编辑和导出，界面不出现原始错误（软件渲染不比画面）。
  - **WHEN** WebGPU 和 WebGL2 都拿不到（例如 headless 默认参数：`navigator.gpu` 存在但没有适配器），**THEN** 编辑视图显示现有的「设备不支持」提示，画布页不认领渲染任务。
- **AC-R1-5 设备丢失**（Epic AC-3 后半）：用测试钩子触发设备丢失（与真实丢失走同一个处理函数，可指定「第 N 张之后」）。
  - **WHEN** 视口的设备丢失，**THEN** 视口自动重建并重新显示场景，选中、视角和已进 outbox 的编辑不丢；正在拖、还没提交的变换被取消。
  - **WHEN** 离屏渲染端在执行 `render_view` 或 `export_shots` 时设备丢失，**THEN** 不完成任务、停止续租，任务在租约到期后回队并被重新认领，最终正常完成。
  - **WHEN** `export_shots` 在第 2 张图之后设备丢失，**THEN** 一张图都不上传。
  - **WHEN** 摄影师手动导出时设备丢失，**THEN** 导出失败并提示可以重试，画布上没有图片，重试成功。
  - **WHEN** 生成模型缩略图或资产卡片缩略图时设备丢失，**THEN** 重建设备后重试成功。
- **AC-R1-6 编辑器辅助与显示模式**：**WHEN** 在四种显示模式（含正交视图）下选中物件、选中平面物件、助手高亮物件、拖动变换 gizmo、显示网格和视图 gizmo，**THEN** 外观与迁移前一致（截图存进 evidence，owner 看图）；现有的场景 e2e 定向用例（交互、导出、C4、C5 校准、AC-6）全部通过。
- **AC-R1-7 性能**（Epic AC-7 工作台部分、未决事项 6）：**WHEN** 在参考设备上重跑 AC-9 性能用例，**THEN** 四种显示模式的视口帧时间（平均、p95、最大）不高于 R0 基线，加灯、删灯时最长的一帧不高于基线，三种导出的耗时不高于基线；连续导出 48 张后 GPU 纹理数不增长（每张图新建管线不残留纹理）。不满足时逐项列出差距交 owner 决定。
- **AC-R1-8 兼容模式与 Safari**：**WHEN** 设备是兼容模式（没有 `core-features-and-limits`，three 强制 `samples = 0`），**THEN** 工作台改用 SMAA；能找到真实设备或可靠的模拟方式就验证，找不到时作为已知限制写进验收记录（未决事项 11）。**WHEN** R1 验收前在 Safari 26.5 人工打开编辑器、导出一次，**THEN** 画面正确，结果记入 evidence。

## Impact

- 前端 `frontend/src/creative-scene/`：
  - `render/`：新增渲染器创建入口，改写 `sceneRenderer.ts`、`linePass.ts`、`dofPass.ts`、`sky.ts`、`atmosphere.ts`、`sunShadow.ts`、`transparent.ts`，新增项目内 fork 的正交 GTAO 与天空；
  - `materials/`：`patterns.ts` 改写为 TSL，`instances.ts`、`principled.ts` 改用节点材质，删除 `inject.ts`；
  - `objects/`：`Lighting.tsx`、`SceneObjects.tsx`、`ModelMesh.tsx` 的天空、描边和粗线；
  - `Viewport.tsx`、`SceneEditor.tsx`：异步 `gl` 工厂、帧推进、设备丢失重建、网格与视图 gizmo、设备探测；
  - `host/render.ts`、`host/exportShots.ts`、`host/executor.ts`、`host/launcher.ts`、`host/pageExecutor.ts`：离屏出图、设备丢失回队与暂停认领、异步探测；
  - `sandbox/thumbnail.ts`、`library/thumbnail.ts`、`preview/ModelPreview.tsx`：缩略图与资产预览。
- 前端测试：`frontend/scripts/creative-scene-*.test.ts` 中依赖 GLSL 注入和 `lookAlongZ` 的单测改写；新增静态检查测试；`creative-scene.e2e.mjs` 新增迁移对照、确定性、WebGL2 后端、设备丢失用例，性能用例改为与基线对比。
- 契约：交付 `architecture: 场景模块`、`patterns: 场景着色写法`（由 Epic 批准基线提取，不复制到本子项）。不改 API、数据契约和场景格式。
- Living Spec：`creative/scene` 新增「渲染设备不可用与丢失」要求。
- 依赖：不新增 npm 依赖；`three` 仍锁定 0.186.1。drei 只保留白名单内的组件。

## Risk Notes

- **影响面**：场景编辑器、助手的截图和导出、缩略图、资产预览全部换底座，R1 合入后整体切换，没有开关（Epic design「上线」）。回滚方式是撤销 R1 的合并，场景数据不受影响。
- **性能**：R0 的初步判断是工作台 GPU 耗时约为现有管线的 40%，但没测节点材质重新编译时的卡顿（现有编辑器加一盏灯最长一帧 120 ms）。如果加灯、删灯的卡顿明显变差，优先用 `compileAsync` 预编译，仍不达标时交 owner 决定。
- **画面差异**：阴影（PCF 对 PCFSoft）、AO 降噪做法、MSAA 解析的差异 R0 没有量化，AC-R1-1 的阈值为此留了余量；超出时交 owner 看图，不放宽阈值、不私自改外观。
- **私有接口**：帧推进依赖 three 的私有字段（`renderer._animation`、`renderer._nodes.nodeFrame`），封装在创建入口一处并配单测；three 升级时这个测试先失败。
- **兼容模式设备**：本机拿不到，SMAA 路径可能只能按已知限制交付（AC-R1-8）。
- **Epic 未决事项 7**：owner 2026-10-21 决定 C5c（PBR 贴图与 HDRI）排在 R1 之后，直接在节点材质上实现；C5b、C7、C8 与渲染无关，可以穿插。`scene-editor-3d` 目前没有实施中的子项，与 R1 不冲突。
- **设备丢失的残余窗口**：交出结果前的提交点之后仍有毫秒级的窗口可能漏掉丢失，交出一批空白图。设备丢失本身少见，接受这个窗口，作为已知限制写进验收记录。
- **新增决定**（Epic 未规定、本子项设计里确定，详见 design「局部设计与偏差」）：
  - Epic 修订 v3：材质预览与白模、最终渲染一样有 AO（更正 Epic 表格对现状的记录）；
  - 正交和透视共用一份项目内 GTAO；AO 降噪用 three 的 `DenoiseNode`；
  - 两种输出约定：视口和离屏走管线内编码（R3F 需显式传 `linear`、`flat`、`shadows="percentage"`），缩略图和资产预览直接输出；变换 gizmo 在管线里合成（有退出条件和退路），视图 gizmo 预换算颜色；
  - 设备丢失：只订阅 `renderer.onDeviceLost`；出图统一经入口的 `capture`；交出结果前过提交点，丢失则整批丢弃；离屏任务放弃并暂停本页认领；缩略图重建设备重试一次；
  - 渲染能力探测改为异步（请求一次 WebGPU 适配器）；
  - 每张图重置帧序号时一并清空节点的更新记录；设备上的 GPU 资源挂在入口对象上，不放模块级缓存；
  - 静态检查用 AST，豁免用写死的路径清单。
