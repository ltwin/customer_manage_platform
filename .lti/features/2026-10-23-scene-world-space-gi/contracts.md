<!-- 已批准的目标契约，不代表项目当前已实现。正文以 2026-10-23 的项目契约为底；scene-world-environment 也修订 patterns: 场景着色写法，后合回的一方以当时的项目契约为底重写本条。data: 场景文档 以 2026-10-23 的项目契约为底，只改 render.engine 一行，由 S2 交付。 -->

## architecture: MODIFY 场景模块

### 职责与边界
- 依赖通用文档底座（`creative-document-foundation`）：场景以文档类型端口注册为 `scene` 类型，读取、命令、回收站和历史都走通用入口。
- `backend/internal/creativescene`：
  - 场景快照的 schema 校验、单元版本与命令应用、恢复。
  - 模型提交（消费暂存上传，写 `model` 内容）。
  - 客户端任务生命周期、导出编排。
  - 场景助手工具的领域入口。
- 不职责：执行几何代码、渲染、画布节点的布局规则（调用画布命令层完成）。
- 依赖方向：`creativeagent` → `creativescene` → `creativecontent` / `creativestaging` / `creativecanvas`（命令层）/ 通用文档层 / `platform/store` / `platform/realtime`。`creativescene` 不 import gin（ADR-003），`gin.Context` 不下穿。客户端任务到达终态时，`creativescene` 经自己定义的端口通知 `creativeagent`（进程启动时注册实现），不反向 import。画布读取导出图节点时，`creativecanvas` 经自己定义的端口问 `creativescene` 这张图是否过时（进程启动时注册实现），`creativecanvas` 不 import `creativescene`。
- 前端 `frontend/src/creative-scene/`：编辑器外壳（R3F）、场景状态（快照 + outbox + 撤销栈）、沙箱宿主、渲染端（客户端任务执行器）、材质库、导出器。编辑视图嵌入画布的同一个助手面板。渲染端不依赖编辑器外壳：画布页在认领到任务时按需加载它，摄影师不打开场景也能让助手搭建；没有任务的画布页不加载 3D 引擎。
- 渲染、烘焙、截图、导出只在浏览器完成；服务端不渲染。服务端经客户端任务派发工作，经暂存上传接收产物。
- 渲染统一用 three.js 的 `WebGPURenderer`，WebGPU 不可用时用它的 WebGL2 后端；着色全部用 TSL（见 `patterns: 场景着色写法`）。视口、截图、导出、模型缩略图和资产 3D 预览都经同一个渲染器创建入口获得渲染器；视口、截图和导出经同一条渲染管线（`SceneRenderer`）出图。
- 实时 GI 的世界空间追踪层（`scene-world-space-gi`）：渲染端的光追底座 `frontend/src/creative-scene/render/trace/` 从已挂载的 three 场景图收集网格（规则同不透明预通道），在 Worker 里为每个几何构建原始 BVH 和按绝对误差简化的追踪代理（`three-mesh-bvh`、`meshoptimizer`），增量打包成两级 BVH（`three-mesh-bvh/webgpu`）并配上材质表，提供求交与命中点着色；底座按渲染设备持有，跨导出图复用，编辑时上传量只与改动的几何成正比。构建结果按「几何身份 + 构建算法版本」缓存在本机 IndexedDB，只作加速，可随时清除或重建，不上传、不是事实来源。探针光线追代理、锐利反射追原始几何（画质优先的精度分档）；场景超出设备容量时按档降级（反射改追代理 → 不画这一层）并在渲染选项卡说明。底座就绪前视口只画屏幕空间，导出和 `render_view` 等底座就绪后才开始计帧。实时 GI 用它补屏幕外的反射（项目内 fork 的 SSR 对没命中和边缘淡出的采样补追）和漫反射（辐照度探针，只替换环境贴图的辐照度那一份），将来的路径追踪复用同一底座。底座、探针和反射历史都是渲染端的 GPU 派生状态：不读场景文档、不持久化、不新增数据流。只在 WebGPU 后端且设备 limits 满足底座要求时运行；兼容模式、设备不支持或场景超出容量时本机不画这一层，实时 GI 退回屏幕空间和现有标定值，不改写场景（G4）。
- 环境类效果（物理天空，以及之后在其上增加的环境效果）的换算与绘制只在渲染端：前端 `frontend/src/creative-scene/world/` 下的天文计算（`astro`）、物理大气（`atmosphere`）和曝光（`exposure`）是不依赖编辑器外壳的模块，画布页按需加载渲染端时同样可用。服务端按数据契约校验字段（时区名用二进制内嵌的 `time/tzdata` 校验），不参与画面上的天体换算；渲染指纹照常由服务端从快照算出。服务端另为助手算「太阳参考」（当前太阳的高度和方位、日出日落、正午、黄金时刻和蓝调时刻，NOAA 公式）和「月亮参考」（当前月亮的高度和方位、月相与照亮比例、月出月落、前后最近的满月和新月，Meeus 公式），只放在助手读写场景的结果里，不存储，不进指纹（修订 v2、v4）。
- 曝光属于观看者：物理天空下，每次出图由场景摄像机或默认摄像机（视口自由视角、`render_view` 的临时视点）的曝光设置求出曝光：手动按 ISO、快门、光圈或默认曝光值；环境测光按场景原点的水平照度测光，叠加夜间压暗曲线和补偿，只由场景决定（修订 v5）。曝光在送进 GPU 前乘到光源上（预曝光）；天空和环境贴图按只取决于环境的相对单位存储，再乘以相对系数。夜色（入夜偏蓝、降饱和）属于场景，在色调映射之前作用。其他天空模式沿用场景级的 `render.exposure_ev`。白模不读取环境和灯光，用固定的工作室光。

### 关键数据流
- 手动编辑：编辑器 outbox → `POST /creative/documents/{id}/commands` → 新文档修订 → 账号失效通知 → 其他标签页重读。
- 助手写模型 / 截图 / 导出：工具创建客户端任务 → 运行进入 `waiting_client` 并释放槽位 → 渲染端（场景编辑视图，或按需加载渲染模块的画布页）认领、执行、暂存上传 → 完成任务 → 同事务落结果并让运行继续。
- 导出：渲染端暂存上传导出图。摄影师手动导出时调 `POST /creative/documents/{id}/scene-exports`；助手导出（`export_shots`）把图片清单随完成任务提交，在完成事务里放置。两条路径走同一个放置流程：画布命令层建图片节点，写入 `based_on`。读画布时经上述端口为导出图节点计算过时状态。
- 环境到画面：`environment` 单元与观看者的曝光 → 渲染端求天体方向、大气 LUT（参数变化时重算）和曝光 → 天空背景、太阳光、天空光照及其上的环境类效果 → 色调映射。所有环境类效果读同一份天体方向、大气 LUT 和曝光。

## patterns: MODIFY 场景着色写法

- **约定**：
  - 适用范围：`frontend/src/creative-scene/`，以及所有渲染场景或模型的前端代码（模型缩略图、资产 3D 预览）。
  - 渲染器只从渲染器创建入口获得（`WebGPURenderer`，WebGPU 不可用时自动改用它的 WebGL2 后端），其他代码不直接创建渲染器。
  - 着色只用 TSL（`three/tsl`）：材质用节点材质（`*NodeMaterial`），或由渲染器自动映射的内置材质；后处理用 `RenderPipeline` 和节点。不使用 `ShaderMaterial`、`RawShaderMaterial`、`onBeforeCompile`，也不写 GLSL / WGSL 源码字符串（包括 `glslFn`、`wgslFn`、`wgslTagFn`）。例外只有以下几处（`scene-world-space-gi`）：
    - npm 依赖 `three-mesh-bvh/webgpu` 的 BVH 遍历是 WGSL（owner 2026-10-23）；
    - `render/trace/` 里一个调用它求交的包装模块：在 three r186 上，库的求交不能从 TSL 直接调用，包装模块只构造参数、声明局部结果、按值返回，不写任何着色逻辑（修订 v3）。上游提供可从 TSL 调用的版本后删除。
  - 程序化材质预设是 TSL `Fn`：输入只有对象空间位置（米）、对象空间法线、参数 uniform、像素尺度和宽度函数，输出 `{albedo, roughness, metalness, height, emission}`；不读取渲染器状态、屏幕坐标、帧序号或时间。函数体里不直接调用屏幕导数（`fwidth`、`dFdx`、`dFdy`）：抗锯齿需要的宽度经输入的宽度函数取得，光栅路径传 `fwidth`（生成的着色与改动前相同，已发布预设的外观不变），光线命中点（屏幕外，没有屏幕导数）传由光线锥合成光线微分、再取有限差分得到的宽度，像素尺度同样按光线锥估计（`scene-world-space-gi`）。材质函数的输入在 TSL 的 `Switch` 之前存成变量（见下文「分叉之前先存成变量」）。凹凸由 `height` 在预设函数之外求得，命中点不做凹凸。
  - 已发布的预设外观不变，改外观发布新版本（沿用材质目录的规则）。
  - 按像素分叉的控制流里，不放依赖隐式导数的纹理采样。
    - **范围**：
      - 分叉指条件随像素变化的 `If`、`Loop` 里的提前退出，以及条件不是常量的 `select`：three 把这种 `select` 编成 if/else；
      - 依赖隐式导数的采样，指 `texture()` 不带 `.level()` 或 `.grad()` 的采样，以及深度比较采样。
    - **原因**：
      - WGSL 规定这类采样只在一致的控制流里有定义，而 three 生成 WGSL 时关掉了 `derivative_uniformity` 诊断，所以编译不报错；
      - 没进分支的相邻像素，坐标为零或未定义，导数随之为 NaN；
      - 结果是整块纯白或纯黑。
    - **写法**：在分叉之外无条件采样，再用算术（`mix`、`step`）选择；或者显式指定 LOD。
    - **验证**：这类错误 SwiftShader 不复现，要在本机 GPU 上验证。

    出处：`scene-world-space-gi` 修订 v5；来源是 2026-10-26 体积雾的纯白（`f3c4807f`）。
  - 分叉之前先存成变量：按像素分叉的控制流里共用的表达式，在分叉之前存成变量（`.toVar()`）。
    - **范围**：
      - 分叉与上一条相同：`Switch`、条件随像素变化的 `If`、`Loop` 里的提前退出，以及条件不是常量的 `select`；
      - 共用指几个分支（包括循环展开出的几份分支）用到同一个节点，也包括作为参数传进函数的表达式，例如 `dir.negate()`。
    - **原因**：
      - TSL 在第一次构建某个表达式的分支里把它存成变量，只在那个分支里赋值；
      - 其他分支读到的是没赋过值的变量，编译不报错；
      - 结果是数值错了，而不是整块纯白或纯黑，所以更难发现。
    - **写法**：在函数入口或分叉之前存成变量，函数收到的参数也一样。
    - **验证**：写不成静态检查。调整分叉的结构（例如调换分支顺序、加提前退出）时，在同一个着色器里算出新旧两种写法，逐像素对照。

    出处：`scene-world-space-gi` 修订 v6。来源是 S2 任务 7.0：探针逐级回退只调换了累加顺序，`thin-wall@level2` 就从 +13% 变成 +34%。原因是传进来的视线方向只在第一级的分支里赋了值。
  - 需要随帧变化的量（TRAA 抖动、时域噪声）只取渲染器创建入口提供的帧序号；不读墙钟时间，也不依赖渲染器自己的 rAF 计数。每张导出图从帧序号 0 开始；跨帧累积的状态（实时 GI 世界空间追踪层的探针和反射历史）在这时一并清空。
  - 预计算表（LUT，例如物理大气的透射率、多次散射、天空视图和空气透视）用全屏片元 pass 写入 `RenderTarget`，3D 表写入 `RenderTarget3D` 的各层或拼成 2D 图集；不用 compute 写存储纹理。理由是 WebGL2 后端的 compute 由 transform feedback 模拟，只能写存储缓冲。
  - 默认两个后端都支持。只支持 WebGPU 的功能须经所属 Epic 修订确认，并按统一规则降级和认领（`scene-world-environment` design「只支持 WebGPU 的统一规则」）。已确认只支持 WebGPU 的：实时 GI 的世界空间追踪层（`scene-world-space-gi`，降级与认领方式见该 Epic design「能力与容量」）。
  - 光追（实时 GI 的世界空间追踪层，以及将来的路径追踪）：场景的 BVH、材质表、求交和命中点着色只经光追底座（`render/trace/`）获得，每个渲染设备一份、跨导出图复用。命中点着色使用与光栅相同的材质参数和预设函数、灯光 uniform（含预曝光）和场景环境贴图，阴影朝灯光发阴影光线（屏幕外的命中点不在视锥拟合的阴影贴图覆盖范围内）；命中点的光照模型是与光栅等价的另一份实现（three 的光照模型只在材质构建时运行），由同一表面的光栅对照测试把关。只支持 WebGPU 的光追可以用 compute 写存储缓冲和存储纹理（上一条对 LUT 的限制只针对两个后端都要运行的预计算表）。为它重新标定的常数（SSGI 增益、环境光填充、收敛帧数、时限）只在这一层构建时生效，其余情况保持原值。为性能做的几何简化只用在画面上看不出差别的地方：漫反射探针的光线追误差受控的代理（按 ε 简化，由画质对照把关），锐利反射和将来的路径追踪追原始几何；规模超出设备容量时逐档降级并明确提示，不悄悄降低画质（`scene-world-space-gi` design I8）。导出和 `render_view` 等光追底座就绪后才开始计帧，结果不随构建快慢变化。
  - 物理天空下光照按物理单位（亮度 cd/m²，平行光照度 lux，点光和聚光 cd），光源强度在送进 GPU 前乘以曝光系数（预曝光）；天空 LUT、空气透视和环境贴图等环境派生数据按「环境相对单位」存储（乘以只取决于环境的 s_env），每个观看者再乘以相对系数；天空、灯光和材质写入 HDR 缓冲前钳到 60 000。这样 RGBA16F 缓冲在正午不溢出，夜里也不下溢（`scene-world-environment` design「物理单位与曝光」）。着色代码里不写绝对亮度的常数；绝对量只出现在 `world/exposure` 和 `world/atmosphere` 的 CPU 端。
  - 环境类效果（天空、太阳光、天空光照、空气透视、体积，以及将来的云和地表效果）取同一份天体方向、大气 LUT 和曝光，不各自近似。天体位置只由文档字段算出，不读当前时间。
  - 第三方 3D 组件（drei 等）只用不含自定义着色器的。引入新组件前先确认它能在 `WebGPURenderer` 下运行，再加入静态检查的白名单。
  - three 锁定精确版本；升级时重跑迁移等价对照（`scene-webgpu-renderer` AC-1 的参考场景集）。`three-mesh-bvh` 和 `meshoptimizer`（直接依赖）同样锁定精确版本；升级时重跑 `scene-world-space-gi` 的参考场景对照，并核对 `three-mesh-bvh/webgpu` 的不稳定接口（包括项目用到的非公开扩展点）。
  - fork 进项目的 three 节点和着色代码（正交 GTAO、天空、SSGI，以及 `scene-world-space-gi` 起的 SSR）在文件头注明上游版本和改动的行；升级 three 时逐个与上游比对。`three-mesh-bvh` 的增量打包用子类扩展，不 fork（修订 v3），依赖的非公开接口在文件头列出。
  - three r186 在 compute 阶段的限制（`TextureNode.grad()`、PMREM 的环境旋转）由 `render/trace/` 里一个限定范围的补丁模块处理：只改 compute 阶段原本会报错的分支，光栅着色器不受影响；文件头注明上游版本、改动的方法和对应的上游 issue，升级 three 时先检查能否删除（`scene-world-space-gi` 修订 v3）。按论文或外部示例自写的着色代码（Hillaire 物理大气）在文件头注明参照的论文和示例版本。
- **为什么**：`WebGPURenderer` 只接受节点材质。只有一份着色源码，WebGPU 和 WebGL2 两个后端以及将来的 compute 路径追踪器才能共用同一份定义，不会出现两套实现的外观漂移；预设写成不依赖渲染器的纯函数，才能在 compute 着色器里复用。
- **不适用于**：不做 3D 渲染的 2D canvas（例如画布页的生成遮罩用 2D context）；沙箱里的几何代码只生成几何、不渲染。
- **检查**：`make check-frontend` 中的静态检查（`scene-webgpu-renderer` R1 交付）：`frontend/src` 里出现 `WebGLRenderer`、`WebGLRenderTarget`、`ShaderMaterial`、`RawShaderMaterial`、`onBeforeCompile`、`ShaderLib`、`ShaderChunk`、GLSL 模板字符串、`three/examples/jsm/postprocessing/*`、`three/addons/objects/Sky.js`、非 WebGPU 版的 `lines/*`，或白名单以外的 drei 组件时失败。`frontend/src/creative-scene/world/` 和 `render/` 里出现 `Date.now()` 或无参的 `new Date()` 时也失败（`scene-world-environment` W2 交付；节流用的 `performance.now()` 不影响画面，不在此列）。`frontend/src` 里出现 `wgslFn` 或 `wgslTagFn` 时同样失败，`render/trace/` 的包装模块除外（`scene-world-space-gi` S1 交付）。「分叉里不放隐式导数的采样」和「分叉之前先存成变量」写不成静态检查，由在本机 GPU 上运行的定向 e2e 把关。
- **例子**：
  - `frontend/src/creative-scene/materials/patterns.ts` 的预设函数；
  - `render/` 下的渲染器创建入口（R1 改写后）；
  - `render/trace/` 的光追底座（`scene-world-space-gi` S1 交付后）；
  - 分叉里的采样：`render/volumes/lights.ts` 的点光阴影查找，每步无条件采样，再乘以范围掩码；`render/probes/lookup.ts` 的探针采样，显式指定 LOD（S2 交付后）。
  - 分叉之前先存成变量：`render/probes/lookup.ts` 的 `probeIrradiance` 在入口把位置、法线和视线方向存成变量（S2 交付后）。

## data: MODIFY 场景文档

### 用途与存储角色
- **用途**：保存 3D 场景的编排，包括对象树、变换、环境、渲染设置和单元版本。不保存几何（几何在模型内容里）、GLB 字节、生成代码和导出图。
- **存储**：PostgreSQL。
  - 通用文档层：`creative_documents` 行，`type_key = 'scene'`。
  - 修订：`creative_document_revisions`。
  - 引用：`creative_document_content_refs`。
  - 每个修订的全文快照：一条 `creative_content_revisions`，内容类型 `scene`。
  - 通用文档的表（文档行、修订、引用、工作会话）由通用文档底座 change 交付，本条目只定义 `scene` 类型的载荷、单元和引用登记。
- **角色**：权威主数据。`creative_documents.content_revision_id` 指向当前快照；修订表是历史的唯一顺序来源。
- **所有权**：只有 `creativescene` 写入，摄影师和助手都经场景命令；画布命令层只在新建 `core.scene` 节点时同事务创建文档。读取方是场景接口、画布节点视图和助手工具。

### 结构与身份
- 文档 ID 形如 `cwdoc_<uuid>`，账号内唯一。对象 ID 形如 `sobj_<21位 nanoid>`，由客户端生成，场景内唯一，删除后不复用。不复用由客户端随机生成来保证；服务端只拒绝与当前快照重复的 ID，不查历史。
- 快照载荷（`creative_content_revisions.payload`，`schema_version = 1`，≤ 512 KiB）：

| 路径 | 类型 | 必须 | 缺省 / 约束 |
|---|---|---|---|
| `scene.environment.sky.kind` | `"procedural"` \| `"hdri"` \| `"color"` | 是 | 新建场景为 `procedural` |
| `scene.environment.sky.sun_elevation_deg` | number −10…90 | `procedural` 时 | 新建为 35；0° 为地平线，90° 为天顶 |
| `scene.environment.sky.sun_azimuth_deg` | number 0…360 | `procedural` 时 | 新建为 135；0° 指向 +Z，90° 指向 +X（与 three.js Sky 示例的 `azimuth` 相同） |
| `scene.environment.sky.turbidity` | number 1…20 | `procedural` 时 | 新建为 3 |
| `scene.environment.sky.hdri` | string，内置 HDRI ID（`^[a-z0-9_]+@[1-9][0-9]*$`） | `hdri` 时 | — |
| `scene.environment.sky.color` | `#rrggbb` | `color` 时 | — |
| `scene.environment.ambient` | `{color:#rrggbb, intensity:0…10}` | 是 | `{#ffffff, 0.4}` |
| `scene.environment.fog` | `{color:#rrggbb, near>0, far>near}`（three.js `Fog`，米）或 null | 是 | null |
| `scene.render.mode` | `"clay"` \| `"lit"` | 是 | `clay`；只影响编辑器默认显示，导出时另行指定 |
| `scene.render.exposure_ev` | number −10…10（曝光补偿，档） | 否 | 0；只作用于 `lit`（编辑器的最终渲染、`lit` 导出、`render_view` 的 `lit`）：渲染时 three.js `toneMappingExposure = 2^exposure_ev`。白模、线稿和材质预览不受影响（修订 v10） |
| `scene.render.engine` | `"workbench"` \| `"realtime_gi"`（渲染器） | 否 | `workbench`；只决定最终渲染怎么出图（编辑器的最终渲染、`lit` 导出、`render_view` 的 `lit`）。`workbench` 是光栅化渲染；`realtime_gi` 在其上加入屏幕空间的间接光、反射与接触阴影，并在满足上限的 WebGPU 设备上用世界空间追踪补上屏幕外的反射和漫反射（`scene-world-space-gi`；兼容模式、设备不支持或场景过大时只做屏幕空间，不改写场景），视口停止操作后收敛，导出和 `render_view` 多帧累积（`scene-webgpu-renderer` design「渲染器与显示模式」）。白模、线稿、线框和材质预览不受影响；正交视图按 `workbench` 显示。只计入 `lit` 的渲染指纹，值为 `workbench` 时不计入；`realtime_gi` 计入时连同世界空间追踪的版本号一起计入（`scene-world-space-gi` S2 起为 1），所以 S2 上线后已有的实时 GI `lit` 导出各显示一次「场景已更新」，`workbench` 的 `lit` 和 `clay`、`line` 不受影响，指纹版本前缀不升级。`path_tracing` 由后续 Epic 加入，现在写入返回 400 |
| `scene.render.resolution` | `"1k"` \| `"2k"` \| `"4k"` \| `"8k"`（输出分辨率） | 否 | `2k`；导出图（`clay`、`line`、`lit`）的长边分别为 1024、2048、4096、8192 px，短边按摄像机的画幅比例。不影响视口、`render_view`（JPEG，长边 1024）和缩略图。不计入渲染指纹 |
| `scene.objects[]` | 对象数组，≤ 2000 | 是 | 新建为一个内置地面 |
| `objects[].id` | string | 是 | 见身份规则 |
| `objects[].kind` | `"primitive"` \| `"model_instance"` \| `"light"` \| `"camera"` \| `"group"` | 是 | `model_instance` 自 C2 起可写入 |
| `objects[].name` | string ≤ 80 字符 | 是 | 可为空串 |
| `objects[].parent_id` | string 或 null | 是 | null 表示根；父对象必须是 `group`；不能成环 |
| `objects[].order` | integer ≥ 0 | 是 | 同一父对象下的兄弟顺序，兄弟之间不重复 |
| `objects[].transform` | `{position:[3], rotation:[4]（单位四元数 x,y,z,w）, scale:[3]（每项 0.001…1000）}` | 是 | 数值有限；位置每轴 ±100000 m |
| `objects[].visible` / `locked` | boolean | 是 | `true` / `false` |
| `objects[].primitive` | `{builtin:"<name>@<ver>", params:{…}}` | `kind=primitive` 时 | `builtin` 必须在服务端登记的内置清单里（C1 交付清单）；`params` 外形同模型参数值 |
| `objects[].model` | `{content_revision_id}` | `kind=model_instance` 时 | 必须是本账号 `model` 类型、`ready` 且可展示的内容修订 |
| `objects[].material_slots` | `{<slot_id>:{preset, params}}` | 否 | 缺省 `{}`；`preset` 匹配 `^[a-z0-9_]+@[1-9][0-9]*$`；`params` 是平铺对象，≤ 32 键，值为 number/string(≤80)/boolean |
| `objects[].light` | 按 `type` 区分，字段与 three.js 灯光类同名同义，见下方「灯光」 | `kind=light` 时 | 只有 `type` 必填，其余缺省取 three.js 默认值，存储时补齐 |
| `objects[].camera` | `{focal_mm:8…800, sensor_mm:[w,h]（各 1…100）, aspect:"<w>:<h>"（各 1…32 的整数）, near>0, far>near（米，同 three.js `PerspectiveCamera`）, label≤40, depth_of_field:bool, f_stop:0.5…64, focus>0（米，同 three.js `PerspectiveCamera.focus`）, focus_object_id:物件 ID 或 ""}` | `kind=camera` 时 | 各字段可省略，缺省 `{35,[36,24],"3:2",0.1,1000,"",false,2.8,10,""}`，存储时补齐；景深四个字段为修订 v8 新增，旧快照在读取时补齐。朝局部 −Z 看（three.js 摄像机约定）；视场角按画面长边对应传感器长边算出：`tan(fov/2) = max(sensor_mm) / (2·focal_mm) / max(画幅宽高比, 1)`，`fov` 为 three.js 的垂直视场角（修订 v8），不写 three.js 的 `fov`。`focus_object_id` 非空时按该物件原点沿视线方向的深度对焦：写入时必须指向修改后场景里的 `primitive`、`model_instance` 或 `group`，且不是摄像机自己；之后目标被删除不算违反契约，按 `focus` 对焦。写空字符串取消对焦物件（按键合并下 `null` 等于没写）。`depth_of_field = true` 时，白模导出按光圈和对焦虚化（design「渲染模式与导出」） |
| 单元（存于底座的 `creative_documents.unit_revs`，不在载荷里） | — | — | 场景的单元为 `environment`、`render`、`objects`，以及每个对象的两个单元：摆放 `object:<id>`（`name`、`transform`、`visible`、`locked`）和内容 `object:<id>:content`（`primitive`、`model`、`material_slots`、`light`、`camera`）；`meta` 由底座保留。`parent_id` 和 `order` 由 `objects` 管：它负责对象的存在性、层级和兄弟顺序 |

- **引用表**：`creative_document_content_refs` 记录每个修订引用的所有模型 `content_revision_id`。
- **灯光**（字段与 three.js 同名同义，渲染时原样赋给对应的灯光类）：

| `type` | three.js | 字段与缺省 |
|---|---|---|
| `directional` | `DirectionalLight` | `color`（#rrggbb，#ffffff）、`intensity`（0…1000，1）、`cast_shadow`（false） |
| `point` | `PointLight` | 以上三项，加 `distance`（米，≥ 0，0 表示不截断，缺省 0）、`decay`（0…10，2） |
| `spot` | `SpotLight` | `point` 的全部字段，加 `angle`（弧度，光锥中心到边缘的半角，0 < angle ≤ π/2，缺省 π/3）、`penumbra`（0…1，0） |

  - 写了该类型没有的字段返回 400 并指出字段（例如点光写了 `angle`）。
  - `intensity` 的含义同 three.js 默认的物理光照：点光和聚光为坎德拉（cd），平行光为 three.js 的强度值。
  - 方向：平行光和聚光沿物件局部 −Z 照射，与 three.js 摄像机、Blender 灯光一致。three.js 的 `target` 不进格式，渲染时由旋转算出；平行光的位置只影响阴影范围。
- **命名原则**（修订 v7）：助手要写的字段与 three.js 同名同义；three.js 没有对应字段的，保留带单位的名字（`sun_elevation_deg`、`focal_mm`、`sensor_mm`）。不接受别名，写错名字返回 400 并指出字段。变换的 `rotation` 保持四元数：three.js 里它对应 `quaternion`（three.js 的 `rotation` 是欧拉角），但两者长度不同，写错会被拒绝而不会悄悄出错，C3 试用中助手也全部写对。

### 访问模式与保证
- **读当前**：按文档 ID 读文档行和当前快照；同时批量读引用的模型修订摘要（manifest、生成参数、名称提示，不含代码和地址）。GLB 和缩略图由编辑器按模型修订另取媒体读取票据（C2 修改）。
- **写**：在文档行锁内（`SELECT … FOR UPDATE`）检查单元版本 → 应用 → 校验全部不变量 → 写新快照、修订行（`actor_kind`、`run_id`、`changed_units`、`restored_from`）和引用行 → 更新文档行的 `revision` 和 `content_revision_id`。整个过程在一个事务里，失败则不产生任何修订。
- **不变量的保证方**：
  - 由应用保证：对象 ID 唯一、父子无环、`order` 不重复、引用的模型可用、单元版本单调。
  - 由数据库保证：修订号主键唯一、`account_id` 限定、内容类型 CHECK。
- **历史**：按 `revision DESC` 分页列出修订；`changed_units` 直接读，不比对快照。

### 生命周期与演进
- 进回收站与彻底删除沿用通用文档层。彻底删除时删除修订行和引用行，之后模型修订失去持有方，可被 GC 回收。
- 内容类型 CHECK 加 `scene`。回滚时如已有 `scene` 内容则拒绝，前向修复。
- 快照 `schema_version` 升级时，读取端兼容旧版本，写入只写新版本；不做批量回填。
- 例外：修订 v7 的改名（雾和摄像机的 `near_m`/`far_m` → `near`/`far`，灯光 `range_m` → `distance`、`angle_deg` → `angle` 并补 `decay`）发生在上线之前，生产库没有场景数据。由一次迁移改写已有的场景快照，`schema_version` 仍为 1，不保留旧字段的读取兼容。旧的 `angle_deg` 没有定义是整锥角还是半角，也从未渲染过，迁移按整锥角换算（`angle = angle_deg / 2`，换成弧度）；2026-10-18 开发库里没有灯光物件。
- 修订 v8（C4 起草时）：摄像机视场角改为画面长边对应传感器长边（原为固定按 `sensor_mm[1]` 算垂直视场角，竖幅画面比同焦距的全画幅相机窄约 1.5 倍）。只改渲染端的换算，字段外形和存储不变，不迁移；默认传感器下 3:2 横幅的画面不变，助手已建的非 3:2 摄像机和 `render_view` 临时视点的取景随之改变。
- 修订 v8（C4 起草时）：摄像机新增 `depth_of_field`、`f_stop`、`focus`、`focus_object_id`。不迁移：读取时补齐默认值（服务端解码时补齐，读取场景的响应里也带齐），之后的写入按完整字段存储。「空物体」是没有子物件的 `group`，不新增 `kind`。
- 修订 v10（C5 起草时）：`render` 新增 `exposure_ev`。灯光按 three.js 的物理单位（坎德拉），室外有太阳和只有几盏灯的室内亮度差几个数量级，固定曝光无法同时照顾两者，所以由场景自己定曝光，与摄影里按场景定曝光一致；用「档」而不是倍数，与相机的曝光补偿、Blender 色彩管理的曝光相同。不迁移：读取时补 0，读取场景的响应里也带齐。
- `scene-webgpu-renderer` R2：`render` 新增 `engine` 和 `resolution`。两者是场景的渲染设置而不是本机偏好，这样编辑器、`render_view` 和导出看到同样的画面，摄影师、其他标签页和助手也一致。不迁移：读取时补 `workbench`、`2k`，读取场景的响应里也带齐。回滚到不认识这两个字段的后端会读取失败，只做前向修复。
