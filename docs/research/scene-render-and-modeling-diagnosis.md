# 3D 编辑器渲染质量与助手建模质量诊断

诊断日期：2026-11-15。本文是诊断结论与待定方向，不代表已批准实现。

资料范围：

- 渲染端代码（`frontend/src/creative-scene/render/`、`objects/`、`Viewport.tsx`）与 Epic `scene-world-space-gi` 的 design、roadmap 和证据图；
- 助手建模链路（`backend/internal/creativeagent/scene_tools.go`、`frontend/src/creative-scene/sandbox/`、`backend/internal/platform/llmgateway/`）；
- 2026-11-15 的外部技术调研（来源见文末）；
- 建模助手在一次硬表面建模任务后自己总结的 8 条反馈（第 4 节）。

**未实测声明**：本次只读代码和仓库里已有的证据图，没有运行编辑器、没有复现任何画面问题。凡标「假设」的成因都来自读代码，需要实测确认。外部调研的数字是来源页面所写，不是本项目实测值。

## 1. 结论摘要

和 UE5 Lumen 的差距主要不在 GI 算法的实现质量，而在三件事：

1. **间接光只有低频**：用 DDGI 探针替代了 Lumen 的逐像素 final gather，物件尺度的间接阴影和接触遮挡丢失。
2. **视口分辨率和采样不足**：实时 GI 视口按 CSS 像素渲染、没有 MSAA，所有随机采样的效果都靠 TRAA 在这个分辨率下收敛。
3. **内容本身是纯色基础体**：助手用代码生成的模型没有倒角、没有贴图、没有做旧，渲染器再好也是 CG 感。

此外有一个缺陷级的问题：**面光源投影出现条纹和阶梯**（建模助手用对照实验缩小到了面光源阴影路径，第 4 节第 1 条）。

定位问题已由 owner 在 2026-11-15 决定（第 6 节）：3D 出图仍以精细为目标，先做好实时 GI，路径追踪后置。`scene-editor-3d` 提案里「不追求精细渲染」的说法已不适用。

## 2. 渲染侧诊断

### 2.1 现状

| 部分 | 实现 | 位置 |
|---|---|---|
| 管线 | 不透明预通道（法线、粗糙度、速度）→ 拆分材质的光照通道 → SSGI、SSR、接触阴影 → 合成 → 雾、体积 → TRAA | `render/realtimeGI.ts` |
| 漫反射间接光 | 以摄像机为中心的 3–5 级 DDGI 探针，每级 16³ 个，间距 0.25 m 起逐级翻倍，每探针 64 条光线 | `render/probes/cascade.ts` |
| 反射 | 随机 SSR，没命中处用 BVH 补追，时域重投影加循环降噪 | `render/nodes/SSRTraceNode.js` |
| 太阳阴影 | 4 级 × 2048 的级联，从 0.5 m 到远裁面对数切分；有尺寸时 PCSS | `render/sunShadow.ts` |
| 其他灯阴影 | 1024 的贴图；面光源从中心画一张点光源立方体贴图，按面的形状做 PCSS | `objects/lights.ts`、`render/lightModel/shadowFilter.ts` |
| 成像 | AgX 色调映射；没有 bloom、颗粒、锐化 | `render/sceneRenderer.ts` |
| 路径追踪 | 计划中，不可选 | `render/engines.ts` |

### 2.2 与 Lumen 的结构性差距

- **DDGI 探针替代了 final gather。** Lumen 的画质核心是屏幕探针的逐像素采集，能算出物件尺度的间接阴影。探针间距 0.25 m 起，小于间距的遮挡全部丢失。Epic 自己的研究文档 `world-space-tracing-options.md` 里已把这一层标成「换成 DDGI 式探针」。
- **探针光不乘 AO。** S2-U7（owner 2026-10-26）拍板不加。对照图 `scene-probe-gi/discovery/research/evidence/after/s2-u7-ao.jpg` 里，不加的一侧物体没有落地感。模型目前也没有 AO 贴图可以补这个空缺。
- **没有真值渲染器。** 路径追踪没做，所有画面都是近似，并依赖一组标定常数（`GI_INTENSITY`、`AMBIENT_FILL_GI` 等）。

### 2.3 锯齿与瑕疵的来源

- **实时 GI 视口按 CSS 像素渲染。** `Viewport.tsx:1223` 在 2× 屏上只画一半分辨率，预通道和光照通道都是 `samples: 0`。PCSS（16 次搜索 + 32 次滤波，逐帧旋转）、SSGI（2 × 8）、随机 SSR 的噪点都靠 TRAA 在这个分辨率下收敛。
- **阴影贴图精度有限（假设）。** 太阳的级联按对数切到 5000 m 远裁面；点光、聚光和面光源只有 1024。场景里有大面积投射体时，中景级联的纹素可能到厘米级。
- **面光源阴影的比较余量不随坡度变化（假设）。** 2D 贴图的 PCSS 把余量乘了接收面的深度梯度（`shadowFilter.ts` 的 `mapPCSS`）；立方体贴图的 PCSS 用的是固定余量 `dR × texel × 1.5 + 5 mm`（`cubePCSS`），贴图又是最近点读取。掠射角大的表面上，一个纹素内的深度变化会超过这个余量，结果是规律的自阴影条纹。这与第 4 节第 1 条描述的「上臂侧面和圆柱表面的斜条纹」吻合，但没有复现验证。
- **面光源只从中心取一张阴影贴图。** 这是 PCSS 对大面积光源的固有近似，灯越大、离物体越近，半影越不准。
- **证据图里已有可见瑕疵。** `ac-s2-1-before-after.jpg` 的盒子间后墙有横向条纹，红绿墙和金属球上有同心环纹，成因未查。那个场景用的是聚光灯，是否与面光源条纹同源未知。
- **深度缓冲。** 视口摄像机 `near: 0.05`、`far: 5000`，没有找到反向深度或对数深度的设置。亚毫米偏移的薄片几何在几米外可能出现深度竞争，待测。

## 3. 助手建模侧诊断

| 问题 | 证据 |
|---|---|
| `write_model` 只返回包围盒、面数、槽位和部件，不带图片 | `creativecontent/model_facts.go:15-24` |
| `render_view` 一次一张、长边固定 1024，且只对支持图片输入的模型开放 | `creativeagent/scene_tools.go:202-220`、`tool_registry.go:83`；DeepSeek V4 Pro 为 `ImageInput: false`（`llmgateway/catalog_default.go:128`） |
| 没有几何校验：穿模、悬空、法线、非流形、近共面都不检查 | `sandbox/inspect.ts` 只查 GLB 结构和预算 |
| `mount` 只是声明，摆放坐标由模型手算 | `placement/snap.ts:43` 只在手动拖放时使用 |
| 几何库只有 CSG、挤出（倒角固定 2 段）、旋转体、阵列；禁止 import | `sandbox/lib.ts:85-160`、`sandbox/compile.ts:66-72` |
| 代码里禁用贴图；hint 命中预设后代码写的颜色被忽略 | `sandbox/worker.ts:52-58`、`materials/resolve.ts:88-90` |
| 内置模型只有男、女两个素体 | `backend/internal/creativescene/models.json` |
| 提示词只有单位、命名约定和「先粗后细」，没有建模方法 | `creativeagent/instructions/base.md`「搭建场景」 |
| 推理强度固定为 medium | `llmgateway/catalog_4router.go:53` |
| 改模型要重写整段代码 | `write_model` 的入参只有代码，或基线修订加参数 |

## 4. 建模助手的反馈与对照

建模助手在一次硬表面建模任务后总结了 8 条，下表逐条对照。「核实」一栏是本次读代码的结果。

| # | 助手的反馈（它给的优先级） | 与本诊断的关系 | 核实 |
|---|---|---|---|
| 1 | 面光源投影出现严重条纹和阶梯；关掉 `cast_shadow` 后消失（P0） | **补充，且最重要。** 本诊断只看到证据图里有条纹，没有定位到面光源 | 成因假设见 2.3：立方体 PCSS 的固定余量、1024 贴图、单点取样 |
| 2 | 缺少渲染质量的诊断和控制接口：调试通道、收敛状态、实际生效配置（P1） | **新增。** 本诊断没有覆盖 | `render_view` 只有 `clay` 和 `lit` 两种。实时 GI 的预通道和光照通道已经有法线、粗糙度、基础色、环境漫反射、环境高光这几张 MRT，做调试通道的成本低 |
| 3 | 缺少分层材质、做旧遮罩、贴花（P1） | **补充。** 本诊断只说到材质扁平和可以烘焙 AO | 没有找到贴花、遮罩混合或烘焙的实现 |
| 4 | 近共面细节没有可靠的放置机制（P1） | **新增** | 没有找到 `polygonOffset` 或深度偏移的设置；深度缓冲情况见 2.3 |
| 5 | 缺少硬表面的高层建模操作：倒角、加权法线、抽壳、布尔后清理、几何检查（P1） | **重合**（第 3 节「几何库」「没有几何校验」）。它补充了加权法线、硬边管理和抽壳 | 同第 3 节 |
| 6 | 部件可以标识，但不能局部修改（P2） | **重合**（第 3 节「重写整段代码」）。它补充了部件独显、剖切和修改前后差异 | 同第 3 节 |
| 7 | 缺工业材质；环境不支持 HDRI 加载与旋转（P2） | **补充。** 本诊断没有注意到 HDRI | 数据契约留了 `sky.kind = hdri`，但环境面板写着「暂无内置 HDRI」；助手的工具说明里没有 hdri |
| 8 | 预览能力不足：固定 1024、没有局部渲染和 100% 裁切（P2） | **部分重合**（第 3 节 `render_view`）。它补充了区域渲染和并排对比 | 同第 3 节 |

两份诊断的视角不同：助手是从工具使用方看「缺什么接口」，本诊断是从实现看「为什么画面是这样」。助手没有覆盖的部分：

- 第 2.2 节的结构性差距（探针、AO、路径追踪）；
- 视口像素比；
- 摆放改成「关系 + 求解」；
- 资产库与 AI 3D 生成；
- 提示词和推理强度。

对助手反馈的两点保留意见：

- 第 2 条里「阴影分辨率、偏移、过滤质量」不宜直接做成给助手调的参数。条纹是渲染器的缺陷，应该修掉；调试通道和生效配置回执才是该给的。
- 第 1 条的对照实验只证明条纹出在面光源阴影路径上，具体成因仍需复现。助手自己也这样说明了。

## 5. 优化方向

### 5.1 渲染

按 owner 2026-11-15 的决定排序（第 6 节）：实时 GI 优先，路径追踪后置。

| 优先级 | 方向 | 说明 |
|---|---|---|
| P0 | 修面光源阴影条纹 | 先复现，再验证 2.3 的假设（余量随坡度缩放、提高立方体贴图分辨率） |
| P1 | 逐像素漫反射采集（final gather） | 与 Lumen 差距最大的一层，方案见 5.3。它同时补上接触遮挡，S2-U7 不必再用 AO 凑 |
| P1 | 有尺寸的灯的阴影改光追 | 用现有 BVH 向灯面随机点发阴影光线，逐帧累积。不再受贴图分辨率和偏移限制，面光源的单点近似由此根治 |
| P1 | 视口静止后精化 | 相机停下后切到设备像素并渐进累积；运动中保持现状，或用 three 0.186 自带的 `TAAUNode` 上采样 |
| P1 | 摄影棚 HDRI 与工业材质 | 金属的写实感依赖反射环境；契约里的 hdri 类型还是空的 |
| P2 | 粗糙反射复用探针、命中点贴图 | Epic `scene-world-space-gi` 已列为后续增强 |
| P2 | 成像 | three 0.186 里已有 `BloomNode`、`FilmNode`、`SharpenNode`，随后处理器一起接 |
| 后续 | 路径追踪 | 复用光追底座。参考 three-gpu-pathtracer 的 WebGPU 版和 oidn-webgpu |

不建议做：

- 照搬 Lumen 的 SDF 加 surface cache（依赖 Nanite，Epic 的研究文档也已否决）；
- 换成 three 自带的 VXGI（比现有方案更粗）；
- 等 WebGPU 硬件光追（2026-10 才提出提案，未进规范）。

### 5.2 助手建模

| 优先级 | 方向 | 说明 |
|---|---|---|
| P1 | 反馈回路 | `write_model` 自动返回多视角素模拼图和数值校验结果（部件包围盒、相交、悬空、接地、近共面）；`render_view` 加调试通道、区域渲染、可选分辨率和生效配置回执。合并了助手反馈的第 2、4、8 条 |
| P1 | 几何库第 2 版 | 圆角盒、按角度倒角、扫掠、放样、细分、抽壳、加权法线、布尔后清理；可引入 manifold-3d（WASM，保证流形布尔）；生成后自动加 1–2 mm 倒角 |
| P1 | 贴花、遮罩混合与烘焙 | 表面投射贴花替代薄片几何；用现有 BVH 烘焙 AO 和曲率遮罩，同时驱动颜色、粗糙度和法线。AO 烘焙还能部分填上 S2-U7 的空缺 |
| P2 | 摆放改成「关系 + 求解」 | 助手说「放在桌面上」「锚点 A 对锚点 B」，坐标由程序算 |
| P2 | 部件级局部修改 | 按稳定部件 ID 更新，保留其他部件和材质覆盖 |
| P2 | 主体物件用 AI 3D 生成或资产检索 | 助手只管布局和参数化部件。Tripo 的许可阻断项见 `docs/research/tripo3d-modeling.md` |
| P2 | 建模技能文档与推理强度 | 真实尺寸参考、分件步骤、自检清单；`write_model` 单独提高推理强度 |

外部证据（来源见文末）：

- VIGA 的写码—渲染—检查闭环在 BlenderGym 上比单次生成提升 35.32%；
- CADSmith 指出纯看图解决不了尺寸错误，改用几何内核精确量测后中位 IoU 从 0.81 升到 0.96；
- MMSI-Bench 上 GPT-5 的空间推理约 40%，人类 97%；Holodeck 让模型只输出物体间关系、由求解器落位。

### 5.3 实时 GI 能接近 Lumen 到什么程度

结论分两种情况：

- **出图（多帧累积的静帧）：可以接近。** 我们的场景规模小，命中的是真实三角形和真实材质，而 Lumen 的软件光追命中的是距离场加低分辨率的表面缓存；出图又有 64 帧可以累积，预算远多于 Lumen 的每帧约 8 ms。
- **运动中的视口：做不到 Lumen 的水平。** 没有 Nanite、虚拟阴影贴图和时域超分这一整套，浏览器里也没有硬件光追。视口的目标应是「运动中够用，停下后收敛到接近出图」。

**要补的一层**：把 SSGI 按 SSR 在 S1 里的做法 fork 一次。屏幕上追得到的照旧；追不到的方向改用 BVH 追一条短光线，命中点按自己的材质和直接光着色，再加上命中点处探针的辐照度（多次反弹由探针承担）；没命中的方向取探针。合成里用它替换「像素直接查探针」。

**预算估算**（S0 `throughput.md` 的实测吞吐，参考设备 M4 Pro，只算求交，不含命中点着色）：

| 场景 | 不相干光线 | 阴影光线 |
|---|---|---|
| 参考场景 | 每毫秒 29 万–38 万条 | 每毫秒 120 万–220 万条 |
| 压力场景（约 2000 万面） | 每毫秒约 7 万条 | 每毫秒 18.5 万–27.8 万条 |

2K 出图约 280 万像素。按上表推算（未实测）：

- 采集光线每像素每帧 1 条：参考场景每帧约 8 ms，64 帧约 0.5 s；压力场景每帧约 40 ms。
- 阴影光线每像素每灯每帧 1 条：参考场景每帧约 1.6 ms；压力场景每帧约 12 ms。
- 视口（1600 × 1000 CSS 像素）半分辨率采集：参考场景每帧约 1–1.5 ms。

对照：路径追踪要每像素数百次采样、每次多次反弹，光线量高出一到两个数量级。混合方案的主可见性由光栅完成、远场由探针缓存，所以便宜得多。

**风险**：

- 参考设备是高端机，集成显卡上的吞吐没有测过。按「画质优先于性能」，慢设备应该多花时间，而不是降画质。
- 合成又要改一次，`AMBIENT_FILL_GI` 等常数要重新标定。
- 视口里的降噪：可以复用 SSGI 现有的重投影加循环降噪链，效果待验证。
- WebGPU 每阶段 16 张贴图的上限和着色器编译时间，之前已经碰到过。
- Epic `scene-world-space-gi` 的非目标里明确排除了 Screen Probe，这一层要走 Epic 修订或另立 Epic。

## 6. owner 决定（2026-11-15）

- **3D 出图仍以精细为目标。** 它虽然是重绘的参考，但出图越精细，重绘效果越好、AI 味越少。
- **先做好实时 GI，尽可能接近 Lumen；路径追踪作为后续迭代项。** 理由是路径追踪对大部分创作者的电脑要求太高。

仍待决定：

- **S2-U7 是否重开？** 如果做 5.3 的逐像素采集，这个问题自然消失；不做的话建议重开。

## 7. 来源

- [Lumen Technical Details](https://dev.epicgames.com/documentation/en-us/unreal-engine/lumen-technical-details-in-unreal-engine)
- [Virtual Shadow Maps](https://dev.epicgames.com/documentation/en-us/unreal-engine/virtual-shadow-maps-in-unreal-engine)
- [gpuweb 加速结构提案 PR #11054](https://github.com/gpuweb/gpuweb/pull/11054)
- [three.js releases](https://github.com/mrdoob/three.js/releases)
- [three-gpu-pathtracer releases](https://github.com/gkjohnson/three-gpu-pathtracer/releases)
- [oidn-webgpu](https://github.com/pissang/oidn-webgpu)
- [Sundial 分页虚拟阴影](https://discourse.threejs.org/t/sundial-paged-virtual-sun-shadows-for-three-js-webgpurenderer-mit-on-npm/94912)
- [VIGA](https://arxiv.org/abs/2601.11109)
- [CADSmith](https://arxiv.org/abs/2603.26512)
- [Holodeck](https://arxiv.org/abs/2312.09067)
- [MMSI-Bench](https://arxiv.org/abs/2505.23764)
- [manifold](https://github.com/elalish/manifold)
- [TRELLIS.2](https://github.com/microsoft/TRELLIS.2)
