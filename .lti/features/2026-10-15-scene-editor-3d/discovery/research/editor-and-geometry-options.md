# 3D 场景编辑器选型与「几何即代码」可行性调研

调研日期：2026-10-15。来源为各项目 README / 文档 / npm registry 元数据，未做上手穿刺；标注「未核实」的条目只来自二手检索结果。

## 1. 问题

1. 从 0 到 1 基于 three.js 自研，还是有成熟开源编辑器可以直接用或 fork？
2. 「编排用 JSON，几何用代码」是否可行，有没有先例？
3. Agent 建出的模型能否可选地存进资产库？

## 2. 候选项目

| 项目 | 形态 | 许可 | 和本需求的匹配度 | 结论 |
|---|---|---|---|---|
| three.js 官方 editor（`three.js/editor`） | 原生 JS 单页应用，自带 UI 框架、命令式撤销、`Object3D.toJSON` 场景格式 | MIT | 通用，但不是 React，也不是可嵌入组件；场景格式是 three.js 内部 JSON，没有业务语义 | 只作交互参考 |
| Pascal Editor（`pascalorg/editor`） | R3F + three.js WebGPU 渲染器，monorepo：core / viewer / editor / nodes / geometry-script / mcp | MIT（LICENSE 文件已核对） | **架构高度相似**：扁平节点字典 + Zod schema + Zundo 撤销，Agent 用 MCP 写 three.js 几何模块，编译成 GLB。但领域是建筑（Site / Building / Level / Wall / Slab），持久化是本地 IndexedDB / SQLite，没写 WebGL 回退 | **作为核心参考**，不整体 fork（见 §4） |
| Theatre.js `@theatre/r3f` | R3F 场景属性的可视化编辑，可导出 JSON | Apache-2.0（未核实） | 偏动画编排；维护者声明 API 会大幅变动 | 不采用 |
| Triplex（pmndrs） | 开发 R3F 组件的 VS Code 插件 | 开源 | 是开发工具，不能嵌入产品 | 不采用 |
| PlayCanvas Editor 前端 | 2025 年起前端 MIT 开源 | MIT（前端） | 后端闭源；引擎不是 three.js | 不采用 |
| img2threejs | Claude Code / Codex Skill：从一张参考图生成返回 `THREE.Group` 的 TS 工厂函数，渲染预览并和参考图比对，通过质量门才进入下一步 | Apache-2.0（未核实） | 证明「LLM 用代码建单个物件 + 渲染回看」可行 | 方法参考 |
| CoDeR（arXiv 2609.26458，2026-09） | 用代码构建可执行世界，再用视频生成模型出画面 | 代码未发布 | 方向一致（代码白模 + 生成式渲染），只读过摘要 | 方向佐证 |

## 3. 关键先例：Pascal 的 authored objects

文档：`wiki/architecture/authored-objects.md`（pascalorg/editor@main）。要点如下：

- **模块契约**：three.js 模块导出 `params`（含默认值、范围、步长、单位）、`mount`（`floor` / `wall` / `ceiling` 等，决定原点约定）和默认导出的 `build({ params, THREE })`，返回一个 Group。单位为米，Y 轴向上。
- **能力边界**：只允许 import `three`、精选的 `three/addons` 和 `three-bvh-csg`；禁用网络、DOM、`eval`、动态 import 和 `process`。
- **执行位置**：编译器可以跑在浏览器 worker、Bun 或 Node 里。编辑器在拿掉网络和存储的 worker 中执行模块。隔离方式由宿主决定。
- **事实源**：**编译产物（GLB + manifest）才是事实源**。只有代码、参数或宿主输入变化时才重跑；查看、发布、烘焙都不会重跑。
- **存储**：节点里存 `source`（语言、脚本 hash、参数、产物 hash、manifest）；GLB 按内容地址存（`artifact://<sha256>`）。代码是单独的产物，只有项目编辑者可读。
- **编辑**：`add_object` 可以传新代码，也可以只传新参数重建；`get_source` 读回代码；通用的 `update_node` / `apply_patch` **不能**改 `source` 或脚本生成的尺寸，保证存储的尺寸始终和几何一致。
- **manifest 命名约定**：材质 `slot_<id>` 变成可涂装槽；`part:<id>` 变成可寻址部件；`anchor:<id>` 是挂点；灯光变成可开关的灯光效果。
- **自检**：`verify_scene` 只报告放置问题，不自动修复。

npm（2026-10-13 发布）：`@pascal-app/geometry-script@1.0.4` 依赖 `@pascal-app/core` 和 `three-bvh-csg`，peer 依赖 `three@^0.186`。core 会带上 zustand、zundo、idb-keyval 和建筑领域的 schema，所以**单独复用编译器也会连带引入 core**。

## 4. 为什么不整体 fork Pascal

- **事实源位置不同**：Pascal 是 local-first，场景在 IndexedDB / SQLite 里，撤销是前端 Zundo。本项目的画布已经定了「服务端修订号 + 命令队列 + 服务端撤销回执」，用户操作优先，并且要和 Agent 共用同一套写入路径。
- **领域不同**：建筑层级（Level / Wall / Slab）和 cosplay 合成场景（地形、远景、道具、机位、镜头）基本不重合；需要的摄像机 / 镜头 / 导出通道 / 渲染桥它都没有。
- **WebGPU**：文档没有说明 WebGL 回退。摄影师设备上的 WebGPU 覆盖情况未核实。
- **维护成本**：fork 整个应用会长期和上游分叉；只借用它的契约设计，没有这个负担。

## 5. 可直接使用的构件级库

本项目前端是 React 19、zustand 5、Vite 8，还没有引入 three.js。

- `three`（r186 一代）、`@react-three/fiber@9`（支持 React 19）、`@react-three/drei@10`（TransformControls、CameraControls、Bounds、Gizmo 等）
- `three-mesh-bvh`：射线拾取、贴地、碰撞检测
- `three-bvh-csg`（偏速度，保留属性）/ `manifold-3d`（偏稳健，WASM，需要手动 `delete()`）：布尔运算。Pascal 两个都用。
- `GLTFExporter` / `GLTFLoader`：烘焙与加载

所以「自研」指的是自己做**编辑器外壳、数据模型和 Agent 工具**，渲染和几何底座都用成熟库，不是从 0 写 three.js。

## 6. 局限

- 以上都没有上手穿刺。Pascal 的 geometry-script 能否脱离 core 使用、worker 隔离是否满足 SaaS 场景（Agent 读网页可能被注入恶意代码），都要在设计阶段验证。
- 「Claude 能稳定写出合格的单物件建模代码」的证据来自 img2threejs 和 Pascal 两个先例，不是本项目自己的测量结果。
