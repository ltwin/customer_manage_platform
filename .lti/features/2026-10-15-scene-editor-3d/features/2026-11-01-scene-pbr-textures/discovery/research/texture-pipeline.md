# 调研：贴图接入渲染、世界空间层与数据（2026-11-02）

在 develop 上只读调研，three 0.186.1。路径简写：`FE` = `frontend/src/creative-scene`，`BE` = `backend/internal`，`T` = `frontend/node_modules/three/src`。

## 1. 普通渲染（光栅）的纹理预算

- 每个着色阶段最多 16 张采样纹理（WebGPU 默认值，`FE/render/gpu.ts:412-419` 没有调高）。three 给每张纹理配一个独立的 sampler 绑定（`T/renderers/webgpu/nodes/WGSLNodeBuilder.js:1355-1366,2145-2157`），所以 sampler 的上限同样是 16，只提高纹理上限不起作用。
- 带光照的原理化材质在片元阶段占用的纹理：
  - 固定：DFG LUT 1 张、PMREM 环境 1 张。
  - 玻璃（透射）：+1。
  - 太阳：4 张级联阴影。
  - 文档灯：投影平行光 4、聚光 1、点光 1（立方体图）、面光 1。
  - 只要有面光：LTC 表 +2。
  - AO、空气透视、雾、夜色、探针都在后处理里，不占材质的纹理。
- 普通表面现在没有阴影上限，只有体积光有（`FE/render/volumes/state.ts:22-24`，`selectLights`）。e2e「W5 天空下多灯」场景不带贴图就用到 14/16；再加一盏投影平行光就是 18，在 Metal 上黑屏（lesson `2026-10-24-webgpu-16-textures-metal-only`）。
- 同一个 Texture 对象只占一个绑定（按 uuid 共享，`T/nodes/accessors/TextureNode.js:227-231`），所以三平面投影对同一张图采样三次也只占一个绑定。所有占位图必须共用同一个对象。
- **结论**：
  - 每套材质固定最多 3 张：基础色（sRGB）、切线空间法线、glTF ORM（R = AO，G = 糙度，B = 金属度，与 three 的通道约定一致，见 `T/nodes/accessors/MaterialNode.js:190-216`）。
  - 普通表面加阴影上限 8 张（过渡做法，见 brainstorm 决定 6）。预留 8 张：DFG 1、环境 1、玻璃 1、LTC 2、贴图 3。

## 2. 材质构建

- 用显式的 TSL 节点（`colorNode`、`normalNode`、`roughnessNode`、`metalnessNode`、`aoNode`），不用 `map` 这类内置字段：内置字段只走 UV，做不了三平面投影（`T/materials/nodes/NodeMaterial.js:836,936,1042`；`MeshStandardNodeMaterial.js:149-162`）。
- 三平面法线沿用 `bumpNormal` 的表面梯度思路：
  1. 每个轴平面采样切线空间法线，换算成高度梯度 (−nx/nz, −ny/nz)。
  2. 用 `triWeights` 混合三个梯度。
  3. 按协向量变换到视空间，投影到表面，得到 `normalize(N − grad)`。
- UV 模式用 `normalMap(texture(n, uv()))`。几何体没有 `TEXCOORD_0` 时退回三平面投影。
- 三个轴都无条件采样、按权重混合，不在逐像素分支里采样（`.lti/contracts/patterns.md:104-118`；lesson `2026-10-26-tsl-sample-in-divergent-branch-nan`）。
- 实时 GI 的预渲染不替换材质，会自动用上 `normalNode` 和 `roughnessNode`（`FE/render/realtimeGI.ts:25-27,65`）。GI 光照的 `giBase` 取 `diffuseColor`，`diffuseShare` 乘以 `ctx.ambientOcclusion`（`FE/render/giSplit.ts:127-149`）。
- 线稿（`FE/render/lineArt.ts:81`）和白模的 AO 预渲染（`FE/render/sceneRenderer.ts:631-637`）会替换材质，所以看不到法线贴图。这符合「白模、线稿不受影响」的要求。
- 实例缓存 `keyOf`（`FE/materials/instances.ts:29-34`）：
  - 图片身份进键，只增加一次低成本的 CPU 节点构建。GPU 程序按 WGSL 文本缓存（`T/renderers/common/Pipelines.js:187-194`），换图不会重新编译。
  - 结构变化才会编译：增减某种贴图、切换映射方式。
  - Texture 对象要保持身份稳定；图片晚到时更新 `version` 即可，不要换对象。

## 3. 图片缓存与多设备

- 有 4 个 GPU 设备：视口（`FE/Viewport.tsx:264`）、离屏出图（`FE/host/render.ts:141`）、缩略图（`FE/sandbox/thumbnail.ts:18`）、3D 预览（`FE/preview/ModelPreview.tsx:72`）。
- three 按 renderer 各自保存一份 GPU 副本，按需上传；`Texture.dispose()` 会通知所有 renderer（`T/renderers/common/Textures.js:403-410`）。
- 先例是月面贴图（`FE/world/nightSky/assets.ts`）：一个模块级 Texture 被所有设备共享；离屏出图前 `loadNightAssets` 等它加载，失败时抛出 `NIGHT_ASSETS_FAILED`（`FE/host/render.ts:122-126`）。
- **结论**：做一个页面级的图片缓存（`materials/images.ts`）：
  - 按内容修订 ID 加颜色角色作键，保存 ImageBitmap 和唯一的 Texture，带引用计数。
  - 合成的 ORM 按三张源图修订的组合作键。
  - 离屏出图、材质缩略图、3D 预览都先等图片就绪；视口先画、图片到了再重绘。

## 4. 世界空间层（屏幕外反射重追踪、探针 GI），即 U6

- 反射重追踪在片元阶段运行（`FE/render/nodes/SSRTraceNode.js:1481`），探针光线在计算着色器里（`FE/render/probes/kernels.ts:100-120`）。
- 命中点能拿到：三角形的三个顶点索引和重心坐标（`FE/render/trace/wgsl.ts`），材质行号存在变换槽 `_alignment0` 里（`FE/render/trace/layer.ts:33-34`）。顶点颜色是按顶点单独存的缓冲（`FE/render/trace/hit.ts:471-474`），UV 可以照这个做。现在 `geometryData` 不复制 UV（`FE/render/trace/pool.ts:46-56`）。
- 绑定余量：
  - 反射片元：存储缓冲 6 个；纹理不带探针时 8 张，带探针时 11 张。
  - 探针计算：存储缓冲 7 个（默认上限是 8），纹理约 5 张。
  - 所以 UV 要和顶点颜色合并进同一个「顶点附加」缓冲，存储缓冲的数量不增加。
- three r186 能在计算阶段采样 `texture_2d_array`：
  - 仓库里的 `.grad()` 补丁已经带上数组层号（`FE/render/trace/computePatches.ts:24-30`）。
  - `textureSampleLevel` 不限阶段。
  - 只有一层的 DataArrayTexture 有边界问题，至少分配 2 层。
- **结论**：
  - 每个设备放三张 `texture_2d_array`：基础色 `rgba8unorm-srgb`、法线（rg8，运行时重建 z）、ORM，每层固定 512²（导出时可以用 1024²），带完整 mip，平铺方式为 repeat。
  - 写入层的方式：先重采样到中间渲染目标，再用 `copyTextureToTexture` 按层、按 mip 拷贝。
  - 命中点用已有的光线锥微分 `dpx/dpy` 做 `.grad()` 采样。三平面投影的输入用现成的 `toPattern(P,1)`。UV 模式用每个三角形的 UV 雅可比矩阵求梯度和 TBN，不需要存切线。
  - 材质行第 6 个 vec4 存三个层号和映射方式，第 7 个 vec4 存尺寸倒数、旋转、法线强度。
  - 贴图要等异步上传完成：期间先用常数参数，上传完再改写材质行，并更新 `lightingVersion`。导出要等上传完成；层的分配要确定（按图片 ID 排序）。
  - 映射函数和法线解码写成一个共享的 TSL 函数，由光栅和命中点各自传入采样方式。e2e 检查用的第三份实现（`FE/render/trace/checks.ts:177-218`）一起改。
- 显存（每个设备，视口和导出各一份）：
  - 每层 512² 时，一套带贴图的材质约 3.3 MiB；16 套 53 MiB，64 套 213 MiB。
  - 1024² 时是 4 倍。
  - 数组层数默认上限 256（`maxTextureArrayLayers`），需要申请更高的值，或者规定超出后怎么处理。
- 要改的约定：`.lti/contracts/patterns.md` 里「命中点不做凹凸」一条要修订。

## 5. 后端数据

- 场景引用图片：在 `encodeScene` 里把图片修订 ID 并进 `Snapshot.Refs`（`BE/creativescene/model.go:423-433`）。现有的 `creative_document_content_refs` 已经是 GC 持有方，不需要新表，也不需要改投影。
  - 种类校验由场景自己做：照 `usableModel`（`BE/creativescene/models.go:218-233`）用 `RequireUsable(display)` 加 `Kind == "image"`。底座只检查「可展示」。
  - 不调用 `Retain`：它只用于在这次写入里新建的内容。
  - `modelRefs` 保持只收模型，它还有别的调用方。
  - 渲染指纹已经整体计入 `material_slots`（`BE/creativescene/fingerprint.go:61`），贴图一变，导出图就会提示过时。
- 内容里引用的内容（库副本 `materials`、材质资产）目前没有任何 GC 持有方。需要一张通用表，例如 `creative_content_revision_refs(account_id, holder_revision_id, content_revision_id, role)`，与持有它的修订在同一事务里写入；再加一条 `LiveOwnerCondition` 条件，并在 `projection_roles_test.go` 里登记；`gc.go:62` 的可回收种类加上 `material`。
- 材质资产要从迁移一直改到前端资产库，清单约三四十处。可参照 `model` 资产（迁移 0079 及 `scene-model-library` 的提交）；动工时以当时的代码为准重新核对。
- 助手各处都用内容修订 ID（`ccrv_…`）指认图片。写贴图前要先确认这张图助手可读（`readableRevisionInTx`，参照 `save_library`）。缺口：只被场景当作贴图引用的图片，助手的 `read_asset` 读不到。
- 媒体：原图按上传原样保存，PNG 和 WebP 保留透明通道。展示图只在超过 1600 px 时生成，而且一律是 JPEG（有损、丢透明通道）。所以贴图一律读原图，在客户端缩放到 2048–4096 px 以内，注意 JPEG 原图的 EXIF 方向。
