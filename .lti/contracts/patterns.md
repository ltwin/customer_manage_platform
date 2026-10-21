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

## 几何模块契约

- **约定**：模型的生成代码是一个不含 `import` / `export` 以外语句的 ES 模块文本，由沙箱以注入依赖的方式执行：

```js
export const params = {
  height: { type: 'number', default: 6, min: 2, max: 20, step: 0.1, unit: 'm', label: '高度' },
  broken: { type: 'boolean', default: false, label: '断裂' },
  style:  { type: 'enum', default: 'gothic', options: ['gothic', 'roman'], label: '样式' },
}
export const mount = 'floor' // 'floor' | 'wall' | 'ceiling' | 'free'
export default function build({ params, THREE, lib, rng }) {
  const group = new THREE.Group()
  // ……用 THREE 与 lib（布尔、挤出、旋转体、阵列等标准库）构建
  return group
}
```

- **参数类型**：`number`（`default`、`min`、`max` 必填，`step`、`unit` 可选）、`integer`、`boolean`、`enum`（`options` 1–32 项）、`color`（`#rrggbb`）。每项可带 `label`，最多 32 项。
- **坐标与单位**：米，Y 轴向上，正面朝 +Z。原点：`floor` 为底面中心，`wall` 为背面底边中心，`ceiling` 为顶面中心，`free` 为几何中心。
- **依赖**：`THREE`、`lib` 和 `rng` 都由宿主注入，模块内不得出现 `import` 语句或动态 `import()`。`lib` 是平台的建模标准库，按版本发布（`lib@1`），模块文本里声明 `export const lib_version = 1`，省略时为 1。`rng` 是带种子的确定性随机数。`Math.random` 在沙箱内被替换为每次编译从固定种子开始的确定性序列（three.js 创建对象时用它生成 UUID，不能禁用）；`Date` 不可用。
- **`lib@1`**（发布后行为不再改变，需要新能力时发布 `lib@2`，沙箱同时内置所有已发布版本）：
  - `lib.material(slot, hint?)`：名为 `slot:<slot>` 的 `MeshStandardMaterial`，`userData.hint = hint`；同一次编译里同名槽返回同一个材质。
  - `lib.subtract(a, b)`、`lib.union(a, b)`、`lib.intersect(a, b)`：两个 `Mesh` 按各自的世界变换做布尔运算（`three-bvh-csg`，保留材质分组），返回位于原点的新 `Mesh`。
  - `lib.extrude(shape, depth, {bevel?})`：`shape` 是 `THREE.Shape` 或二维点数组，返回 `BufferGeometry`。
  - `lib.lathe(points, segments = 32)`：二维轮廓绕 Y 轴旋转，返回 `BufferGeometry`。
  - `lib.array(object, count, step)`：线性阵列，`step` 为 `[x,y,z]`，返回 `Group`。
  - `lib.radial(object, count, radius, axis = 'y')`：环形阵列，返回 `Group`。
- **材质**：只能使用 `MeshStandardMaterial`（或 `lib.material(slot, hint)` 生成的同类材质）的常量参数。材质名 `slot:<id>` 表示可分配材质预设的槽位，`userData.hint` 给语义提示（如 `stone`、`wood`）。不得使用 `ShaderMaterial` / `RawShaderMaterial`，也不得使用任何纹理（外部 URL、数据纹理、视频纹理）。程序化外观由编辑器材质库在渲染时提供。导出的 GLB 必须落在 `data: 模型内容` 的静态 GLB 子集内：没有形变、骨骼、动画、灯光和摄像机。
- **命名约定**（进入 manifest）：对象名 `part:<id>` 表示可寻址部件，用于点选改某个部位；空对象 `anchor:<id>` 表示挂点。
- **几何**：只接受三角网格（GLB 中 primitive 的 `mode` 为 4）；线和点会被拒绝。
- **产物**：沙箱返回 GLB（经 `GLTFExporter` 二进制导出）和规范化的模块信息 `{contract_version, lib_version, mount, params_schema, params}`（`params` 为补齐默认值后实际使用的值）。manifest 不由沙箱提供，由宿主和服务端从 GLB 算出：`{bounds:{min:[x,y,z],max:[x,y,z]}, triangles, slots:[{id,hint}], parts:[id], anchors:[{id,position:[x,y,z]}]}`。GLB 是几何事实源，只有代码、参数或 `lib_version` 变化时才重新编译。
- **上限**（初始值，见 design 未决 #3）：代码 ≤ 64 KiB；编译 ≤ 5 秒；三角形 ≤ 300k；GLB ≤ 16 MiB。
- **为什么**：只用 primitive 的 JSON 表达力不够；整个场景是一段代码则无法支持手动编辑、点选修改和撤销。以物件为粒度写代码，既有建模自由度，又让场景编排可编辑。先例见 research §3（Pascal authored objects）。
- **不适用于**：内置基础体是前端打包的可信模块，遵守同一契约，但不经沙箱、不烘焙。外部 GLB 导入（下一版）不经过本契约。
- **例子**：C2 交付后补充内置基础体 `box@1` 的实现路径作为参照。

## 客户端代码沙箱

- **约定**：任何来自模型或助手的代码，只能在满足以下条件的环境中执行：
  1. 独立的 `<iframe sandbox="allow-scripts">`（不含 `allow-same-origin`，因此是 opaque origin），内容是平台自带的沙箱页面 `/scene-sandbox/sandbox.html`（具体文件路径；该前缀下找不到的文件返回 404，不回退到应用首页）。页面由 HTTP 头下发 CSP，模板只有一份（`frontend/public/scene-sandbox/csp.txt`）：
     `default-src 'none'; script-src <站点源>/scene-sandbox/ blob:; worker-src blob:; connect-src 'none'; img-src blob: data:; style-src 'none'; base-uri 'none'; form-action 'none'; frame-ancestors <站点源>`。
     `script-src` 和 `frame-ancestors` 必须写明确的站点源，不能用 `'self'`：WebKit 在 opaque origin 里不把 `'self'` 匹配到站点源（穿刺实测）。限定到沙箱路径，避免代码对其他路径发起带 cookie 的脚本请求。
  2. 实际编译在沙箱页面内部的 **classic** Worker 中进行，Worker 从页面内嵌的源码经 Blob URL 创建（Chromium 拒绝 opaque origin 里从 Blob 创建 module Worker）。**每次编译新建一个 Worker**，结束即销毁，避免上一次的代码改写 three.js 或 `lib` 影响下一次。
  3. 宿主通过 `postMessage` 传入代码、参数和 `lib_version`，只接收 GLB 的 ArrayBuffer、规范化的模块信息和错误信息。只接受来自该 iframe 的消息；传出的消息有固定结构，宿主逐字段校验，超长或结构不符一律丢弃。模块信息按不可信 JSON 校验（与服务端同一套参数定义规则）；代码哈希由宿主对自己发出的代码计算，不取沙箱的值。
  4. 宿主为每次编译设硬超时（5 秒）；超时后移除 iframe 并重建。沙箱不持有凭证，不能访问 cookie、站点存储、父页面 DOM 和网络，也不能导航顶层页面。
  5. **沙箱回传的内容不可信**：宿主先按 `data: 模型内容` 的静态 GLB 子集做与服务端相同的预检，并按同一算法从顶点数据重新计算 manifest、检查上限；预检不通过按编译失败处理。预检通过后才交给 `GLTFLoader`，并用 `LoadingManager.setURLModifier` 拒绝 `blob:`、`data:` 以外的地址，否则 GLB 里的 `uri` 会让可信页面带着 cookie 发请求。服务端另以 worker 从 GLB 算出的 manifest 为准。
- **为什么**：助手会读网页，网页内容可能把恶意代码注入生成代码。代码在摄影师浏览器里执行，和摄影师的登录会话同处一个浏览器，所以必须隔离。
- **验收**：C2 交付安全样例集（尝试 `fetch`、`XMLHttpRequest`、`WebSocket`、`EventSource`、动态 `import`、`importScripts`、`document.cookie`、`localStorage`、`indexedDB`、`parent.document`、`top.location`、`eval`、死循环、内存耗尽、超大输出、GLB 内含外链 URI），全部被阻断或在时限内终止，外部地址收不到请求。样例集在 Chromium 上纳入 `make check-frontend`；WebKit 用环境变量手动运行，结果记入 verification。穿刺记录见子项 C2 的 `discovery/research/sandbox-spike.md`。
- **不适用于**：前端自带的可信代码（内置基础体、材质库）。
- **备选**：将来某个目标浏览器的样例无法全部通过时，改用 QuickJS-wasm 执行，契约其余部分不变。

## 场景着色写法

- **约定**：
  - 适用范围：`frontend/src/creative-scene/`，以及所有渲染场景或模型的前端代码（模型缩略图、资产 3D 预览）。
  - 渲染器只从渲染器创建入口获得（`WebGPURenderer`，WebGPU 不可用时自动改用它的 WebGL2 后端），其他代码不直接创建渲染器。
  - 着色只用 TSL（`three/tsl`）：材质用节点材质（`*NodeMaterial`），或由渲染器自动映射的内置材质；后处理用 `RenderPipeline` 和节点。不使用 `ShaderMaterial`、`RawShaderMaterial`、`onBeforeCompile`，也不写 GLSL / WGSL 源码字符串（包括 `glslFn`、`wgslFn`）。
  - 程序化材质预设是 TSL `Fn`：输入只有对象空间位置（米）、对象空间法线、参数 uniform 和像素尺度，输出 `{albedo, roughness, metalness, height, emission}`；不读取渲染器状态、屏幕坐标、帧序号或时间。
  - 已发布的预设外观不变，改外观发布新版本（沿用材质目录的规则）。
  - 需要随帧变化的量（TRAA 抖动、时域噪声）只取渲染器创建入口提供的帧序号；不读墙钟时间，也不依赖渲染器自己的 rAF 计数。每张导出图从帧序号 0 开始。
  - 第三方 3D 组件（drei 等）只用不含自定义着色器的。引入新组件前先确认它能在 `WebGPURenderer` 下运行，再加入静态检查的白名单。
  - three 锁定精确版本；升级时重跑迁移等价对照（`scene-webgpu-renderer` AC-1 的参考场景集）。
  - fork 进项目的 three 节点和着色代码（正交 GTAO、天空、SSGI）在文件头注明上游版本和改动的行；升级 three 时逐个与上游比对。
- **为什么**：`WebGPURenderer` 只接受节点材质。只有一份着色源码，WebGPU 和 WebGL2 两个后端以及将来的 compute 路径追踪器才能共用同一份定义，不会出现两套实现的外观漂移；预设写成不依赖渲染器的纯函数，才能在 compute 着色器里复用。
- **不适用于**：不做 3D 渲染的 2D canvas（例如画布页的生成遮罩用 2D context）；沙箱里的几何代码只生成几何、不渲染。
- **检查**：`make check-frontend` 中的静态检查（`scene-webgpu-renderer` R1 交付）：`frontend/src` 里出现 `WebGLRenderer`、`WebGLRenderTarget`、`ShaderMaterial`、`RawShaderMaterial`、`onBeforeCompile`、`ShaderLib`、`ShaderChunk`、GLSL 模板字符串、`three/examples/jsm/postprocessing/*`、`three/addons/objects/Sky.js`、非 WebGPU 版的 `lines/*`，或白名单以外的 drei 组件时失败。
- **例子**：`frontend/src/creative-scene/materials/patterns.ts` 的预设函数和 `render/` 下的渲染器创建入口（R1 改写后）。
