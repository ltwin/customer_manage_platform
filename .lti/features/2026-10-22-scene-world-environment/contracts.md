<!-- 本 Epic 的目标契约 delta（待 owner 批准为基线）。各条目由一个子项完整交付，见 roadmap.md 的「交付契约」列。 -->

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
- 环境类效果（物理天空，以及之后在其上增加的环境效果）的换算与绘制只在渲染端：前端 `frontend/src/creative-scene/world/` 下的天文计算（`astro`）、物理大气（`atmosphere`）和曝光（`exposure`）是不依赖编辑器外壳的模块，画布页按需加载渲染端时同样可用。服务端按数据契约校验字段（时区名用二进制内嵌的 `time/tzdata` 校验），不参与画面上的天体换算；渲染指纹照常由服务端从快照算出。服务端另为助手算「太阳参考」（当前太阳的高度和方位、日出日落、正午、黄金时刻和蓝调时刻，NOAA 公式）和「月亮参考」（当前月亮的高度和方位、月相与照亮比例、月出月落、前后最近的满月和新月，Meeus 公式），只放在助手读写场景的结果里，不存储，不进指纹（修订 v2、v4）。
- 曝光属于观看者：物理天空下，每次出图由场景摄像机或默认摄像机（视口自由视角、`render_view` 的临时视点）的曝光设置求出曝光：手动按 ISO、快门、光圈或默认曝光值；自动按场景原点的水平照度测光，叠加夜间压暗曲线和补偿，只由场景决定（修订 v5）。曝光在送进 GPU 前乘到光源上（预曝光）；天空和环境贴图按只取决于环境的相对单位存储，再乘以相对系数。夜色（入夜偏蓝、降饱和）属于场景，在色调映射之前作用。其他天空模式沿用场景级的 `render.exposure_ev`。白模不读取环境和灯光，用固定的工作室光。

### 关键数据流
- 手动编辑：编辑器 outbox → `POST /creative/documents/{id}/commands` → 新文档修订 → 账号失效通知 → 其他标签页重读。
- 助手写模型 / 截图 / 导出：工具创建客户端任务 → 运行进入 `waiting_client` 并释放槽位 → 渲染端（场景编辑视图，或按需加载渲染模块的画布页）认领、执行、暂存上传 → 完成任务 → 同事务落结果并让运行继续。
- 导出：渲染端暂存上传导出图。摄影师手动导出时调 `POST /creative/documents/{id}/scene-exports`；助手导出（`export_shots`）把图片清单随完成任务提交，在完成事务里放置。两条路径走同一个放置流程：画布命令层建图片节点，写入 `based_on`。读画布时经上述端口为导出图节点计算过时状态。
- 环境到画面：`environment` 单元与观看者的曝光 → 渲染端求天体方向、大气 LUT（参数变化时重算）和曝光 → 天空背景、太阳光、天空光照及其上的环境类效果 → 色调映射。所有环境类效果读同一份天体方向、大气 LUT 和曝光。

## patterns: MODIFY 场景着色写法

- **约定**：
  - 适用范围：`frontend/src/creative-scene/`，以及所有渲染场景或模型的前端代码（模型缩略图、资产 3D 预览）。
  - 渲染器只从渲染器创建入口获得（`WebGPURenderer`，WebGPU 不可用时自动改用它的 WebGL2 后端），其他代码不直接创建渲染器。
  - 着色只用 TSL（`three/tsl`）：材质用节点材质（`*NodeMaterial`），或由渲染器自动映射的内置材质；后处理用 `RenderPipeline` 和节点。不使用 `ShaderMaterial`、`RawShaderMaterial`、`onBeforeCompile`，也不写 GLSL / WGSL 源码字符串（包括 `glslFn`、`wgslFn`）。
  - 程序化材质预设是 TSL `Fn`：输入只有对象空间位置（米）、对象空间法线、参数 uniform 和像素尺度，输出 `{albedo, roughness, metalness, height, emission}`；不读取渲染器状态、屏幕坐标、帧序号或时间。
  - 已发布的预设外观不变，改外观发布新版本（沿用材质目录的规则）。
  - 需要随帧变化的量（TRAA 抖动、时域噪声）只取渲染器创建入口提供的帧序号；不读墙钟时间，也不依赖渲染器自己的 rAF 计数。每张导出图从帧序号 0 开始。
  - 预计算表（LUT，例如物理大气的透射率、多次散射、天空视图和空气透视）用全屏片元 pass 写入 `RenderTarget`，3D 表写入 `RenderTarget3D` 的各层或拼成 2D 图集；不用 compute 写存储纹理。理由是 WebGL2 后端的 compute 由 transform feedback 模拟，只能写存储缓冲。
  - 默认两个后端都支持。只支持 WebGPU 的功能须经所属 Epic 修订确认，并按统一规则降级和认领（`scene-world-environment` design「只支持 WebGPU 的统一规则」）。
  - 物理天空下光照按物理单位（亮度 cd/m²，平行光照度 lux，点光和聚光 cd），光源强度在送进 GPU 前乘以曝光系数（预曝光）；天空 LUT、空气透视和环境贴图等环境派生数据按「环境相对单位」存储（乘以只取决于环境的 s_env），每个观看者再乘以相对系数；天空、灯光和材质写入 HDR 缓冲前钳到 60 000。这样 RGBA16F 缓冲在正午不溢出，夜里也不下溢（`scene-world-environment` design「物理单位与曝光」）。着色代码里不写绝对亮度的常数；绝对量只出现在 `world/exposure` 和 `world/atmosphere` 的 CPU 端。
  - 环境类效果（天空、太阳光、天空光照、空气透视、体积，以及将来的云和地表效果）取同一份天体方向、大气 LUT 和曝光，不各自近似。天体位置只由文档字段算出，不读当前时间。
  - 第三方 3D 组件（drei 等）只用不含自定义着色器的。引入新组件前先确认它能在 `WebGPURenderer` 下运行，再加入静态检查的白名单。
  - three 锁定精确版本；升级时重跑迁移等价对照（`scene-webgpu-renderer` AC-1 的参考场景集）。
  - fork 进项目的 three 节点和着色代码（正交 GTAO、天空、SSGI）在文件头注明上游版本和改动的行；升级 three 时逐个与上游比对。按论文或外部示例自写的着色代码（Hillaire 物理大气）在文件头注明参照的论文和示例版本。
- **为什么**：`WebGPURenderer` 只接受节点材质。只有一份着色源码，WebGPU 和 WebGL2 两个后端以及将来的 compute 路径追踪器才能共用同一份定义，不会出现两套实现的外观漂移；预设写成不依赖渲染器的纯函数，才能在 compute 着色器里复用。
- **不适用于**：不做 3D 渲染的 2D canvas（例如画布页的生成遮罩用 2D context）；沙箱里的几何代码只生成几何、不渲染。
- **检查**：`make check-frontend` 中的静态检查（`scene-webgpu-renderer` R1 交付）：`frontend/src` 里出现 `WebGLRenderer`、`WebGLRenderTarget`、`ShaderMaterial`、`RawShaderMaterial`、`onBeforeCompile`、`ShaderLib`、`ShaderChunk`、GLSL 模板字符串、`three/examples/jsm/postprocessing/*`、`three/addons/objects/Sky.js`、非 WebGPU 版的 `lines/*`，或白名单以外的 drei 组件时失败。`frontend/src/creative-scene/world/` 和 `render/` 里出现 `Date.now()` 或无参的 `new Date()` 时也失败（`scene-world-environment` W2 交付；节流用的 `performance.now()` 不影响画面，不在此列）。
- **例子**：`frontend/src/creative-scene/materials/patterns.ts` 的预设函数和 `render/` 下的渲染器创建入口（R1 改写后）。

## data: ADD 物理天空

### 用途与存储角色
- **用途**：扩展 `data: 场景文档` 的 `scene.environment.sky`，新增天空模式 `physical`。太阳由地点和时间推算或手动指定，天空和光照由物理大气算出，场景按物理单位渲染。
- **存储**：与场景文档相同，位于快照载荷（`schema_version = 1`）的 `environment` 单元内。不新增表、列或单元。
- **角色**：权威主数据。天体方向、大气 LUT 和曝光都是渲染端的派生量，不落库。
- **所有权**：
  - 只由 `creativescene` 经 `set_environment` 写入（环境选项卡、助手 `edit_scene`）。
  - 读取方：渲染端、环境选项卡、助手 `read_scene`、渲染指纹。

### 结构与身份
`sky.kind = "physical"` 时的字段如下。除 `kind` 和 `date` 外都可省略，存储时补齐缺省值。读取场景的响应里总是带齐。

| 路径 | 类型 / 约束 | 缺省 | 说明 |
|---|---|---|---|
| `sky.kind` | 新增取值 `"physical"`（与 `procedural`、`hdri`、`color` 并列） | — | 新建场景仍为 `procedural` |
| `sky.sun_mode` | `"location"` \| `"manual"` | `location` | 选择太阳由地点和时间推算，还是用下面的手动角度。两套字段都保存 |
| `sky.latitude_deg` | number −90…90（北纬为正） | 31.2304 | 与 `longitude_deg`、`timezone` 要么都写，要么都省略；都省略时为上海。只写一部分返回 400 |
| `sky.longitude_deg` | number −180…180（东经为正） | 121.4737 | 同上 |
| `sky.timezone` | IANA 时区名，区分大小写：服务端 `time.LoadLocation` 能加载，写法与时区数据里的名字完全一致（`asia/shanghai` 拒绝），且不是 `Local` 或空串（`LoadLocation("")` 会返回 UTC） | `Asia/Shanghai` | 同上；`local_time` 是这个时区的当地时间，夏令时由时区库处理 |
| `sky.place` | string ≤ 80 | `""` | 地点的显示名（例如「成都」），只用于界面和助手阅读，不参与计算 |
| `sky.date` | `"YYYY-MM-DD"`，1900-01-01…2100-12-31，必须是存在的日期 | 无，**必填** | 当地日期。`set_environment` 整体替换，缺省值会在每次省略时重置日期，所以不设缺省；编辑器切到物理天空时写入本机的当天 |
| `sky.local_time` | `"HH:MM"`，00:00…23:59 | `"12:00"` | 当地时刻，精确到分钟 |
| `sky.north_azimuth_deg` | number 0…360 | 180 | 正北在文档坐标中的方位，约定同 `sun_azimuth_deg`（0° 指向 +Z，90° 指向 +X）；180° 即正北指向 −Z（视图空间 +Y，同 Blender 的 +Y 为北） |
| `sky.sun_elevation_deg` | number −10…90 | 35 | `sun_mode = manual` 时的太阳高度，含义同程序化天空 |
| `sky.sun_azimuth_deg` | number 0…360 | 135 | `sun_mode = manual` 时的太阳方位，含义同程序化天空 |
| `sky.altitude_m` | number 0…20000 | 0 | 场景 Y = 0 处的海拔（米） |
| `sky.air_density` | number 0…10 | 1 | 空气（瑞利散射）密度倍率，同 Blender Nishita 天空的 Air |
| `sky.dust_density` | number 0…50 | 1 | 尘埃与气溶胶（米氏散射）密度倍率，同 Blender 的 Dust；基准与 Blender 同量级。上限比 Blender 的 10 大，是为了做出浓雾霾和沙尘（修订 v2） |
| `sky.ozone_density` | number 0…10 | 1 | 臭氧吸收倍率，同 Blender 的 Ozone |
| `sky.sun_disc` | boolean | true | 是否画太阳盘；不影响太阳光 |
| `sky.sun_size_deg` | number 0.1…10 | 0.545 | 太阳盘的视直径（度） |
| `sky.sun_strength` | number 0…10 | 1 | 太阳的亮度倍率：太阳光、太阳盘和它照亮的天空一起乘（修订 v2） |
| `sky.sun_temperature_k` | number 2000…12000 | 6500 | 太阳的色温（开尔文），按黑体颜色相对 6500 K 换算；6500 为中性，调低更暖、调高更冷；同样作用于太阳光和天空（修订 v2） |

- `physical` 时不得出现 `turbidity`、`color`、`hdri`，写了返回 400 并指出字段，与「天空只带自己类型的字段」一致。
- 反过来，`procedural`、`hdri`、`color` 时不得出现上表中物理天空独有的字段（除 `kind`、`sun_elevation_deg`、`sun_azimuth_deg` 以外的各行），写了同样返回 400 并指出字段。否则这些字段会进入 `lit` 指纹，已有导出误报过时。
- `sky.night`、`sky.aerial_perspective` 由 `data: 夜空`、`data: 空气透视` 定义，交付之前写入返回 400。
- `sky.clouds` 和 `environment.weather` 保留给远期的云层和天气，现在写入返回 400。
- 预设（晴朗、雾霾、高原、沙尘等）不是字段：选预设等于写入一组 `altitude_m`、`air_density`、`dust_density`、`ozone_density`。各预设的取值由 W2 定，列在助手工具说明和编辑器里。预设不改太阳的亮度和色温。

**物理天空下其他字段的解释**（只改解释，不改存储）：

| 字段 | 物理天空下 | 其他天空模式下 |
|---|---|---|
| 平行光 `intensity` | 照度（lux） | 不变（three.js 的强度值，按 `litLook` 标定） |
| 点光、聚光 `intensity` | 坎德拉（cd） | 坎德拉（cd） |
| `environment.ambient` | 不生效（字段保留） | 不变 |
| `environment.fog` | 雾色视为介质的反照率，亮度随太阳直射与天空漫射的水平照度变化 | 不变 |
| 材质的自发光（`emissive` × `emissive_intensity`，模型的 `KHR_materials_emissive_strength`） | 亮度（cd/m²），乘以预曝光（修订 v2） | 不变 |
| `render.exposure_ev` | 不生效（字段保留） | 场景曝光补偿，`toneMappingExposure = 2^exposure_ev` |
| `render.exposure_ev100`、摄像机曝光字段 | 生效（`data: 摄像机曝光`） | 保存但不生效 |

切换天空模式时不改写任何曝光字段。

已知限制：灯光的 `intensity` 上限仍是 1000（`data: 场景文档`「灯光」）。物理天空下按 lux 解释时，场景里的平行光最多约为正午阳光的 1%，只能用来补光，不能当第二个太阳。需要放宽时另行修订灯光的取值范围。

### 访问模式与保证
- **写**：`set_environment` 整体替换 `environment`。服务端在同一事务里校验上表的约束（沿用场景文档的写入流程），补齐缺省值后存储。
- **读**：随场景快照一起读。
- **保证方**：取值范围、日期有效、三项地点字段成组、时区名可加载，都由应用（`creativescene`）保证。数据库不感知这些字段。
- **渲染指纹**：`lit` 计入整个 `sky`。天空是 `physical` 时，`lit` 不计入 `render.exposure_ev`，因为它在物理天空下不生效。`clay`、`line` 不计入 `environment`（`scene-world-environment` design「渲染指纹」）。

### 生命周期与演进
- 新增取值，不迁移：已有快照不含 `physical`，读取不受影响。
- 回滚到不认识 `physical` 的后端时，这些场景会读取失败，只做前向修复（沿用 R2 的做法）。
- 示例（`set_environment` 的 `environment`）：

```json
{"sky": {"kind": "physical", "sun_mode": "location",
         "latitude_deg": 30.5728, "longitude_deg": 104.0668, "timezone": "Asia/Shanghai", "place": "成都",
         "date": "2026-10-01", "local_time": "17:40", "north_azimuth_deg": 180,
         "altitude_m": 500, "air_density": 1, "dust_density": 3, "ozone_density": 1},
 "ambient": {"color": "#ffffff", "intensity": 0.4},
 "fog": null}
```

存储后，`sun_elevation_deg`、`sun_azimuth_deg`、`sun_disc`、`sun_size_deg`、`sun_strength`、`sun_temperature_k` 都按缺省补齐。

## data: MODIFY 摄像机曝光

### 用途与存储角色
- **用途**：
  - 扩展 `data: 场景文档` 的 `objects[].camera`：曝光模式（自动或手动）、自动曝光的补偿、手动曝光的 ISO 和快门（光圈沿用现有的 `f_stop`）。
  - 扩展 `scene.render`：默认摄像机（视口自由视角、`render_view` 的临时视点）的曝光模式、补偿，以及手动时的默认曝光值。
  - 曝光只在物理天空下生效。W2 交付手动曝光；修订 v5（owner 2026-10-23）加入自动曝光，新建摄像机和默认摄像机缺省自动。
- **存储**：与场景文档相同。摄像机的字段位于摄像机对象的内容单元 `object:<id>:content` 内，默认摄像机的字段位于 `render` 单元内。
- **角色**：权威主数据。实际的 EV100、自动曝光的测光值和预曝光系数是渲染端的派生量，不落库。
- **所有权**：
  - 摄像机的字段由 `creativescene` 经 `add_objects`、`update_objects` 写入（摄像机属性、助手）。
  - 默认摄像机的字段经 `set_render` 写入（渲染选项卡、助手）。
  - 读取方：渲染端、渲染指纹。

### 结构与身份
新增的摄像机字段与现有摄像机字段平级，`update_objects` 里按键合并；`render` 的新字段在 `set_render` 里按键合并。各字段都可省略，存储时补齐缺省值。

| 路径 | 类型 / 约束 | 缺省 | 说明 |
|---|---|---|---|
| `camera.exposure_mode` | `auto` \| `manual` | 写入时 `auto`；读取没有这个字段的已存储摄像机时 `manual` | 曝光模式（修订 v5） |
| `camera.exposure_compensation` | number −5…5（档） | 0 | 自动曝光的补偿，正值变亮；手动时保存但不生效（修订 v5） |
| `camera.iso` | number 25…102400 | 100 | 感光度，只在手动时生效 |
| `camera.shutter_s` | number 0.000125…30（秒，即 1/8000 s…30 s） | 0.00025（1/4000 s） | 快门，只在手动时生效 |
| `render.exposure_mode` | `auto` \| `manual` | `auto` | 默认摄像机的曝光模式（修订 v5） |
| `render.exposure_compensation` | number −5…5（档） | 0 | 默认摄像机自动曝光的补偿（修订 v5） |
| `render.exposure_ev100` | number −10…20 | 15 | 默认摄像机手动时的曝光值（EV100）。15 约为晴天正午 |

- 手动：摄像机的 EV100 = log₂(`f_stop`² / `shutter_s`) − log₂(`iso` / 100)。用现有的 `f_stop`，与 `depth_of_field` 是否打开无关。缺省值（ISO 100、1/4000 s、f/2.8）约为 EV100 15。默认摄像机的 EV100 = `render.exposure_ev100`。
- 自动：EV100 = EV_m + D(EV_m) − 补偿。EV_m 是场景原点水平照度的入射式测光值，D 是夜间压暗曲线（Epic design「物理单位与曝光」），都由渲染端从场景算出，不落库。`f_stop` 只决定景深。
- 编辑器在物理天空下添加摄像机时，沿用默认摄像机的曝光设置，新摄像机的画面与视口一样亮：
  - 默认摄像机为自动时，写 `exposure_mode: auto` 和同样的补偿；
  - 默认摄像机为手动时，写 `exposure_mode: manual`，按 `render.exposure_ev100` 换算：先按 ISO 100 取最接近的 1/3 档快门，快门越界时取边界快门，再调 `iso`。缺省光圈 f/2.8 下可达 EV100 约 −11.9…17.9，超出时新摄像机偏暗（已知局限）。
- 缺省值只用于没写曝光字段的写入，例如助手不带曝光地添加摄像机：自动曝光，补偿 0。
- 天空不是 `physical` 时，这些字段保存但不生效；切换天空模式时不改写它们。

### 访问模式与保证
- **写**：按现有摄像机字段和渲染字段的规则校验和合并。取值范围由应用保证。`update_objects` 的按键合并在补齐后的旧值上进行：只改 ISO 的旧摄像机仍是手动。
- **读**：旧快照里的摄像机和 `render` 可能缺这些字段，服务端解码时补齐，读取场景的响应里也带齐（与修订 v8 的景深字段、修订 v10 的 `exposure_ev` 相同）：
  - 摄像机缺 `exposure_mode` 时补 `manual`：W2 时期的摄像机都按手动曝光拍，画面和导出不变；
  - `render` 缺 `exposure_mode` 时补 `auto`：默认摄像机不进导出，没有导出因此过时；
  - 其余字段补缺省值。
- **渲染指纹**：
  - 摄像机的曝光字段只在 `lit` 且天空为 `physical` 时计入：
    - 手动：计入 `iso`、`shutter_s`；即使景深关闭也计入 `f_stop`，因为光圈决定亮度。不计入 `exposure_mode`（`manual`）和 `exposure_compensation`，所以 W2 时期的摄像机补齐为手动后，哈希输入不变。
    - 自动：计入 `exposure_mode`、`exposure_compensation`；不计入 `iso`、`shutter_s`；`f_stop` 按景深的现有规则。
  - `clay`、`line`，以及非物理天空的 `lit`，都清除摄像机的曝光字段；
  - 任何导出方式都不计入 `render` 的曝光字段（`exposure_mode`、`exposure_compensation`、`exposure_ev100`）：导出总是通过场景摄像机。
  - 自动曝光的测光值随环境变化，环境已经计入指纹，不另计。
  - 因此改一台摄像机的曝光，只让这台摄像机在物理天空下的 `lit` 导出过时；改默认摄像机的曝光不让任何导出过时。
- 不进导出图来源的 `lens` 快照（`data: 导出图来源` 不变）。

### 生命周期与演进
- 不迁移，读取时补齐（规则见上）。
- 回滚到不认识这些字段的后端时，带这些字段的快照会读取失败，只做前向修复。
- 修订 v5 取代 W2 时「将来加自动曝光时缺省为手动」的约定：新写入的摄像机缺省自动；已存储的摄像机读作手动，已有导出不变；默认摄像机缺省自动，视口的画面会变，但不影响导出。

## data: ADD 夜空

### 用途与存储角色
- **用途**：扩展物理天空，控制月亮、星星和银河的绘制。
- **存储**：`scene.environment.sky.night`，位于 `environment` 单元内。
- **角色**：权威主数据。月亮的位置和月相、星星的位置都是渲染端由地点和时间算出的派生量。
- **所有权**：同 `data: 物理天空`。

### 结构与身份

| 路径 | 类型 | 缺省 | 说明 |
|---|---|---|---|
| `sky.night` | object | 下列缺省 | 只在 `sky.kind = physical` 时允许，可省略 |
| `sky.night.moon` | boolean | true | 月亮（月盘、月光） |
| `sky.night.stars` | boolean | true | 星表中的亮星 |
| `sky.night.milky_way` | boolean | true | 银河（渲染端程序化生成） |
| `sky.night.moon_strength` | number | 1 | 月亮的亮度倍率，0…10：月盘、月光和月光照亮的天空一起变化 |
| `sky.night.moon_temperature_k` | number | 6500 | 月亮的色温，2000…12000 K；6500 为物理计算的颜色，越低越暖 |
| `sky.night.stars_strength` | number | 1 | 星空的亮度倍率，0…10：星星、银河和夜天光一起变化 |
| `sky.night.stars_temperature_k` | number | 6500 | 星空的色温，2000…12000 K，含义同上 |
| `sky.night.night_look` | number | 1 | 夜色强度，0…1：入夜后画面偏蓝、降饱和的程度；0 为纯物理颜色（修订 v5） |

- 夜空只在 `sun_mode = location` 时绘制；手动模式下字段保存但不生效。`night_look` 例外：夜色只取决于场景照度，两种太阳模式下都生效（Epic design「夜空」）。
- 亮度倍率和色温与太阳的 `sun_strength`、`sun_temperature_k` 同义（owner 2026-10-22，修订 v4）。
- `stars` 同时控制星星和夜天光（无月晴夜天空本身的微光）。
- 天亮后夜空的亮度自然被天空盖过，不需要另外设开关。
- 其他天空模式下写 `night` 返回 400。

### 访问模式与保证
- **写**：随 `set_environment` 一起写，读取时补齐缺省值。
- **渲染指纹**：随 `sky` 计入 `lit`，不计入 `clay`、`line`。
- **助手的月亮参考**（修订 v4）：物理天空的地点模式下，`read_scene` 和写入了环境的 `edit_scene` 结果里在 `sun` 旁边带 `moon`：当前月亮的视高度和方位（含场景坐标的方位）、月相名称与照亮比例、当天的月出月落、前后最近的满月和新月日期（当地日期）。服务端计算，不存储，不进指纹；`night.moon` 关闭时照样给出（它描述的是天上，不是画面）。

### 生命周期与演进
- 不迁移。W2 交付之后、W3 交付之前存储的物理天空没有 `night`，读取时补齐为缺省值（开关全部 `true`，倍率 1，色温 6500，夜色 1）；只写了部分字段的 `night` 同样逐项补齐。W3 上线后，这些场景的 `lit` 导出各提示一次「场景已更新」（指纹按补齐后的快照计算），与 `data: 空气透视` 相同。
- 回滚同 `data: 物理天空`。

## data: ADD 空气透视

### 用途与存储角色
- **用途**：扩展物理天空，控制空气透视：远处景物按大气的散射和透射变蓝变淡。
- **存储**：`scene.environment.sky.aerial_perspective`，位于 `environment` 单元内。
- **角色**：权威主数据。空气透视的体积是渲染端的派生量。
- **所有权**：同 `data: 物理天空`。

### 结构与身份

| 路径 | 类型 / 约束 | 缺省 | 说明 |
|---|---|---|---|
| `sky.aerial_perspective` | object | 下列缺省 | 只在 `sky.kind = physical` 时允许，可省略 |
| `sky.aerial_perspective.enabled` | boolean | true | 开关 |
| `sky.aerial_perspective.distance_scale` | number 0.1…1000 | 1 | 计算空气透视时，摄像机到表面的距离乘以这个倍率；调大后小尺度的场景也能看出效果。天空背景不受影响 |

- 与全局雾、体积物件叠加，不互相替代（`scene-world-environment` design「合成顺序」）。
- 其他天空模式下写这个字段返回 400。

### 访问模式与保证
- **写**：随 `set_environment` 一起写，读取时补齐缺省值。
- **渲染指纹**：随 `sky` 计入 `lit`，不计入 `clay`、`line`。

### 生命周期与演进
- 不迁移。W4 交付之前存储的物理天空没有这个字段，读取时补齐为缺省值（开启，倍率 1）。W4 上线后，这些场景出现空气透视；渲染指纹按补齐后的快照计算，因此它们的 `lit` 导出各提示一次「场景已更新」。这是预期行为：画面确实变了。
- 回滚同 `data: 物理天空`。

## data: ADD 体积物件

### 用途与存储角色
- **用途**：扩展 `data: 场景文档` 的对象类型，新增 `kind = "volume"`：立方体或球体范围内的均匀介质，有散射和吸收，受太阳、聚光和点光照亮，形成光束和阴影。参照 Blender 中赋予了 Volume Scatter 和 Volume Absorption 的物体。
- **存储**：与场景文档相同，位于快照载荷的 `objects[]` 内。摆放字段在 `object:<id>`，内容字段 `volume` 在 `object:<id>:content`。
- **角色**：权威主数据。
- **所有权**：
  - 由 `creativescene` 经 `add_objects`、`update_objects`、`remove_objects`、`reparent_objects` 写入（添加菜单、属性栏、物件列表、助手）。
  - 读取方：渲染端、物件列表、助手、渲染指纹。

### 结构与身份
ID、`name`、`parent_id`、`order`、`transform`、`visible`、`locked` 与其他对象相同。体积物件的父对象只能是 `group`（现有规则）；它不能做父对象，因为父对象只能是 `group`。

| 路径 | 类型 / 约束 | 缺省 | 说明 |
|---|---|---|---|
| `objects[].kind` | 新增取值 `"volume"` | — | — |
| `objects[].volume` | object | 下列缺省 | `kind = volume` 时必须有（字段可省略，存储时补齐）；其他类型不得出现 |
| `volume.shape` | `"box"` \| `"sphere"` | `box` | 单位体：边长 1 m 的立方体，或直径 1 m 的球；中心在物件原点，大小和形状由 `transform.scale` 决定 |
| `volume.scatter_color` | `#rrggbb` | `#cccccc` | 散射颜色 |
| `volume.scatter_density` | number 0…100（每米） | 0.1 | 散射密度。散射系数 σs = `scatter_density` × `scatter_color`（线性值），同 Blender 的 Volume Scatter |
| `volume.anisotropy` | number −0.95…0.95 | 0 | Henyey–Greenstein 相函数的 g；正值为前向散射，逆光时光束更亮。同 Blender |
| `volume.absorption_color` | `#rrggbb` | `#cccccc` | 吸收颜色，即透过介质后保留的颜色 |
| `volume.absorption_density` | number 0…100（每米） | 0 | 吸收密度。吸收系数 σa = `absorption_density` × (1 − `absorption_color`)（线性值），同 Blender 的 Volume Absorption |
| `volume.edge_falloff` | number 0…1 | 0.1 | 从边界向内、密度从 0 升到满值所占的比例；0 为硬边 |

- 体积物件不得带 `material_slots`、`primitive`、`model`、`light`、`camera`，写了返回 400。
- 每个场景最多 8 个体积物件，超出返回 413（上限待 W5 穿刺确认，`scene-world-environment` design 未决事项 7）。
- 体积物件计入现有的 2000 个对象上限。

### 访问模式与保证
- **写**：沿用对象的写入流程。`update_objects` 里的 `volume` 按键合并（`api: 提交场景命令`）。上限、取值范围和类型匹配由应用保证。
- **绘制**：
  - 只在最终渲染、`lit` 导出和 `render_view` 的 `lit` 里绘制。
  - 在白模、线框和材质预览的视口里只显示范围线框，和灯光、摄像机等辅助对象一样不参与着色。白模导出、线稿导出和 `render_view` 的 `clay` 不画辅助对象，所以完全看不到体积物件。
  - 所有天空模式下都可用。在物理天空下，按物理单位受光。
- **渲染指纹**：`lit` 计入体积物件，`clay`、`line` 清除。

### 生命周期与演进
- 新增对象类型，不迁移。
- 回滚到不认识 `volume` 的后端时，含体积物件的场景会读取失败，只做前向修复。
- 如果 W5 穿刺决定体积只支持 WebGPU，按 `scene-world-environment` design「只支持 WebGPU 的统一规则」处理，并由 Epic 修订补上客户端任务的能力认领。本条目的数据外形不变。
- 示例（`add_objects` 的一项）：

```json
{"id": "sobj_V1o2l3u4m5e6F7o8g9x0q", "kind": "volume", "name": "窗前的雾", "parent_id": null, "order": 4,
 "transform": {"position": [0, 1.5, -2], "rotation": [0, 0, 0, 1], "scale": [4, 3, 3]},
 "visible": true, "locked": false,
 "volume": {"shape": "box", "scatter_color": "#e8e4dc", "scatter_density": 0.3, "anisotropy": 0.6,
            "absorption_color": "#cccccc", "absorption_density": 0, "edge_falloff": 0.2}}
```

## api: MODIFY 提交场景命令

### 用途与调用
- **用途**：对场景做一组原子编辑，成功时产生一个新修订。
- **消费者**：前端场景编辑器（outbox）；助手工具 `edit_scene` 经同一领域命令（不经 HTTP，写入者记为助手和 `run_id`）。
- **调用**：`POST /api/v1/creative/documents/{id}/commands`，`Idempotency-Key` 必填，`application/json`。这是通用命令接口在 `scene` 类型上的操作集合：外壳（`expected` 为单元到修订号的映射、`operations` 数组、整批原子、单元乐观并发）由底座的 `api: 提交文档命令` 定义，本条目定义场景的单元和操作。
- **权限**：场景属于当前账号，且不在回收站。

### 请求
外层 `{operation_id, client_created_at, payload}`（公共约定）。`payload`（外壳见底座，以下是场景的约束）：

| 字段 | 类型 | 必传 | 说明 |
|---|---|---|---|
| `expected` | object（单元 → CreativeRevision 或 null，见底座） | 非 `restore` 批次必传 | 本批涉及的每个单元及其预期 `rev`，null 表示预期该单元不存在；缺少涉及单元时返回 400。`restore` 批次改用底座的 `expected_revision` |
| `operations` | array，1–200 项 | 是 | 按顺序应用；每个单元最多出现一次（同一对象的多处改动合并成 `update_objects` 里的一项）。批量操作的条目上限与物件上限一致（2000），一次多选操作因此始终是一个操作、一个修订 |

`operations[]` 按 `op` 区分：

| op | 字段 | 涉及单元 |
|---|---|---|
| `rename` | `title`（1–200） | `meta` |
| `add_objects` | `objects`（1–2000 个完整对象，见数据契约；`kind` 本条目允许 `primitive`/`light`/`camera`/`group`/`volume`；`primitive.params` 缺省的键补成默认值；`light`、`camera`、`volume` 缺省的字段补成数据契约里的默认值） | `objects`（一次；新对象的两个单元以 N+1 出现在 `changed_units` 中） |
| `update_objects` | `updates`（1–2000 项）；每项 `object_id`，以及 `name`/`transform`/`visible`/`locked`/`primitive.params`/`material_slots`/`light`/`camera`/`volume` 中任选，至少一项（省略即不改；`primitive.params`、`light`、`camera`、`volume` 按键合并，`light.type` 改变时去掉新类型没有的字段、缺少的补默认值；`material_slots` 整体替换；字段必须与对象的 `kind` 相符）；同一物件只能出现一次 | 每项：只改摆放字段时为 `object:<id>`；只改内容字段时为 `object:<id>:content`；两类都改时两个单元都涉及 |
| `remove_objects` | `object_ids`（1–2000；分组连同后代一起删除） | `objects`，以及每个被删对象的 `object:<id>` 和 `object:<id>:content` |
| `reparent_objects` | `moves`（1–2000 项 `{object_id, parent_id（null 为根）, order, transform?}`）；可选 `create_groups`（1–2000 个新分组，外形同 `add_objects` 里 `kind=group` 的对象）和 `remove_groups`（1–2000 个分组 ID）。按顺序执行：先建 `create_groups`；再按 `moves` 改挂，`parent_id` 可以是本次新建的分组，带 `transform` 时同时替换该物件的局部变换（调用方据此保持世界位置，服务端照写）；最后删除 `remove_groups`，删除时它们必须没有子物件。同一物件在三处合计只能出现一次（修订 v10） | `objects`（一次）；带 `transform` 的移动另涉及 `object:<id>`；新建的分组的两个单元以 N+1 出现在 `changed_units` 中；删除的分组涉及 `object:<id>` 和 `object:<id>:content` |
| `set_environment` | `environment`（整体替换） | `environment` |
| `set_render` | `render`（按键合并，只改写到的字段；修订 v10） | `render` |
| `restore` | `revision`（旧修订号）；**必须单独成批**，带 `expected_revision` | 全部单元；按文档修订号检查（底座规则） |
| `restore_units` | `units`（1–4096 个单元名，随底座放宽；不能含 `objects`）、`from_revision` | 列出的单元；**必须单独成批**，删除大分组后的撤销也是一个请求，整体成功或整体失败。对象两边都存在时只换列出单元的字段，`parent_id`、`order` 保持现状。存在性会变化时，该对象的两个单元都必须列出，并额外涉及 `objects`，当前不存在的单元在 `expected` 中写 null。服务端先算出恢复后的完整对象集合，再统一解析层级：每个对象沿来源快照（加回的取旧修订，保留的取当前）的祖先链，挂到第一个仍在最终集合中且是分组的祖先下，没有就放到根；父对象同在本次加回的集合里时层级保持不变；旧修订中不存在的对象被删除时，它的后代按同一规则上移，不连带删除。顺序：父对象没变的保留原 `order`，其余按来源快照的层级和顺序依次放入，原 `order` 被占用时排到末尾。结果与 `units` 的排列顺序无关 |

### 成功响应
200（同一 operation 回放原回执）：

| 字段 | 类型 | 必返 | 说明 |
|---|---|---|---|
回执外形由底座 `api: 提交文档命令` 定义：`revision`、`changed_units`、`annotated_units`、`removed_units`、`reset_required`。场景不增加字段（`api: 场景模型命令` 另加 `models`）。删除大分组或整份恢复时 `reset_required = true`，编辑器重新读取场景。

### 失败响应
| 状态码 | 错误码 | 触发条件 | 调用方处理 |
|---|---|---|---|
| 400 | `validation_failed` | 外形非法、单元重复、`expected` 缺单元、对象违反数据契约（父子成环、未知内置体、数值越界、ID 与当前对象重复、体积物件带了 `material_slots` 等）、引用的物件不存在、本次写到的 `focus_object_id` 不可对焦（不存在、是摄像机自己、灯光或摄像机）、`reparent_objects` 里同一物件出现多次、`create_groups` 不是分组、`remove_groups` 不是分组或删除时仍有子物件、`restore_units` 列了 `objects` 或只列了存在性会变化的对象的一个单元 | 修正，不可原样重试 |
| 404 | `not_found` | 场景不存在或不属于当前账号 | 关闭编辑视图 |
| 409 | `creative_document_conflict` | 任一单元的 `rev` 与 `expected` 不符。`details.conflicts` 为数组：`[{unit, expected_rev, current_rev}]` | 重读，按冲突界面处理后再提交 |
| 409 | `creative_state_conflict` | 场景在回收站 | 先恢复场景 |
| 409 | `idempotency_conflict` | 同一 key 用于不同请求体 | 换新 key 前先查原回执 |
| 413 | `creative_size_limit` | 请求体超限，或应用后快照 > 512 KiB / 对象 > 2000 / 摄像机 > 32 / 体积物件 > 8 | 缩小改动 |

### 交互规则
- **原子性**：全部成功或全部不生效。
- **幂等**：沿用公共约定。
- **并发**：按单元乐观并发，规则见 design「单元版本、写入与撤销」。
- 不适用：分页、异步、限流（沿用全局请求体上限）。

### 调用示例
```http
POST /api/v1/creative/documents/cwdoc_7b1d6c1e-2f0a-4c55-9a0e-3c1f5e2a9b10/commands
Authorization: Bearer <token>
Idempotency-Key: 5f0c3a8e-6a51-4d0b-9a77-1b2c3d4e5f60
Content-Type: application/json
```
```json
{
  "operation_id": "5f0c3a8e-6a51-4d0b-9a77-1b2c3d4e5f60",
  "client_created_at": "2026-10-20T09:30:00Z",
  "payload": {
    "expected": {"object:sobj_3kTMd9Qp2xLw7nB0cR5yZ": "4", "objects": "4"},
    "operations": [
      {"op": "update_objects", "updates": [{"object_id": "sobj_3kTMd9Qp2xLw7nB0cR5yZ",
       "transform": {"position": [1.5, 1.4, 7], "rotation": [0, 0.0998, 0, 0.995], "scale": [1, 1, 1]}}]},
      {"op": "add_objects", "objects": [{
        "id": "sobj_Q9w8E7r6T5y4U3i2O1p0a", "kind": "primitive", "name": "石台", "parent_id": null, "order": 2,
        "transform": {"position": [0, 0, -3], "rotation": [0, 0, 0, 1], "scale": [1, 1, 1]},
        "visible": true, "locked": false,
        "primitive": {"builtin": "box@1", "params": {"width": 2, "height": 0.8, "depth": 1}}, "material_slots": {}}]}
    ]
  }
}
```
```json
{"revision": "5", "changed_units": ["object:sobj_3kTMd9Qp2xLw7nB0cR5yZ", "objects", "object:sobj_Q9w8E7r6T5y4U3i2O1p0a", "object:sobj_Q9w8E7r6T5y4U3i2O1p0a:content"],
 "annotated_units": [], "removed_units": [], "reset_required": false}
```
编组示例（修订 v10，Ctrl + G：新建分组，把两个分属不同父级的物件挂进去，并写入保持世界位置的局部变换；一个修订，撤销是另一个 `reparent_objects`）：
```json
{"operation_id": "8a1b2c3d-4e5f-4a6b-8c7d-9e0f1a2b3c4d", "client_created_at": "2026-10-20T09:40:00Z",
 "payload": {
   "expected": {"objects": "5", "object:sobj_3kTMd9Qp2xLw7nB0cR5yZ": "4", "object:sobj_Q9w8E7r6T5y4U3i2O1p0a": "5"},
   "operations": [{"op": "reparent_objects",
     "create_groups": [{"id": "sobj_G1h2J3k4L5z6X7c8V9b0n", "kind": "group", "name": "石台组", "parent_id": null, "order": 3,
       "transform": {"position": [0.75, 0, 2], "rotation": [0, 0, 0, 1], "scale": [1, 1, 1]}, "visible": true, "locked": false}],
     "moves": [
       {"object_id": "sobj_3kTMd9Qp2xLw7nB0cR5yZ", "parent_id": "sobj_G1h2J3k4L5z6X7c8V9b0n", "order": 0,
        "transform": {"position": [0.75, 1.4, 5], "rotation": [0, 0.0998, 0, 0.995], "scale": [1, 1, 1]}},
       {"object_id": "sobj_Q9w8E7r6T5y4U3i2O1p0a", "parent_id": "sobj_G1h2J3k4L5z6X7c8V9b0n", "order": 1,
        "transform": {"position": [-0.75, 0, -5], "rotation": [0, 0, 0, 1], "scale": [1, 1, 1]}}]}]}}
```
```json
{"revision": "6", "changed_units": ["objects", "object:sobj_G1h2J3k4L5z6X7c8V9b0n", "object:sobj_G1h2J3k4L5z6X7c8V9b0n:content", "object:sobj_3kTMd9Qp2xLw7nB0cR5yZ", "object:sobj_Q9w8E7r6T5y4U3i2O1p0a"],
 "annotated_units": [], "removed_units": [], "reset_required": false}
```
取消编组时，`moves` 把子物件挂回分组原来的父级并写回局部变换，`remove_groups` 删除这个分组。

冲突示例（助手已先改过这台摄像机）：
```json
{"error": {"code": "creative_document_conflict", "message": "文档已被修改",
  "details": {"conflicts": [{"unit": "object:sobj_3kTMd9Qp2xLw7nB0cR5yZ", "expected_rev": "4", "current_rev": "6"}]}}}
```

### 兼容与变更
在通用命令接口上新增 `scene` 类型的操作集合，其他类型不受影响。`api: 场景模型命令` 会在此基础上增加操作，并放开 `add_objects` 的 `model_instance`，属于兼容扩展。旧客户端不会发送新操作。

`scene-world-environment` W5：`add_objects`、`update_objects` 放开 `kind = volume` 和它的内容字段 `volume`（见 `data: 体积物件`），`volume` 与 `light`、`camera` 一样按键合并，属于兼容扩展；旧客户端不会写出体积物件。物理天空、夜空、空气透视和摄像机曝光的新字段走已有的 `set_environment`（整体替换）、`update_objects` 的 `camera`（按键合并）和 `set_render`（按键合并，默认曝光值），本接口的外形不变，只是按数据契约校验新字段。

修订 v10（C5 起草时）：`set_render` 由整体替换改为按键合并（`render` 新增 `exposure_ev` 后，只写 `mode` 的旧写法不能把曝光重置为 0；旧客户端总是写全部字段，不受影响）。`reparent_objects` 增加可选的 `moves[].transform`、`create_groups` 和 `remove_groups`，编组和取消编组因此能在一次提交里完成：同一批里每个单元只能被一个操作涉及，「新建分组 + 改挂」或「改挂 + 删除分组」若拆成两个操作，会两次涉及 `objects`。旧请求不受影响；`remove_objects` 删除分组时仍连同后代一起删除。

修订 v7：`update_objects` 里的 `light`、`camera` 由整体替换改为按键合并（字段允许省略后，整体替换会把没写的字段悄悄重置成默认值；three.js 里改 `light.intensity` 也只改这一项）；`add_objects` 可省略它们的字段。字段改名见 `data: 场景文档`。编辑器同一版本更新，旧字段名返回 400。

### 补充说明
新建场景不经本接口，而是走画布命令 `add_node`（`type_key = "core.scene"`）。底座的文档类型端口负责在同一事务里创建文档；场景类型提供初始快照（一块内置地面 `ground@1`，摄像机随 C4 的放置 UI 出现）和卡片摘要。
