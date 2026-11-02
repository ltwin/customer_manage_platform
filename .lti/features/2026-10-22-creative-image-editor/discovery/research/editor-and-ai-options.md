# 图像编辑器选型与 AI 选区调研

调研日期 2026-10-22。来源以 GitHub API（`gh api`）、项目官方文档和 npm registry 为主，第三方转述的数据单独标注。结论只适用于本项目的约束：商业闭源多租户 SaaS、前端 React 19 + TS strict、已有 three.js WebGPU 渲染栈、部署在阿里云、用户以中国大陆为主。

## 1. 硬约束

- **许可证**：闭源 SaaS 不能把 GPL / AGPL 代码嵌进前端；CC BY-NC 类模型不能商用。
- **集成深度**：编辑器要接通用文档底座（修订、单元并发、恢复）、画布导出、资产库（画笔资产）和创作助手。只能 iframe 嵌入、自带存储和 UI 的方案集成成本高。
- **视觉**：界面要跟 v6 暗房风格一致（中性深灰、白色主操作色），不能换成另一套皮肤。
- **大图**：60MP（如 9504×6336）的一层 RGBA8 约 241 MB，10 层约 2.4 GB；WebGPU 规范默认的 `maxTextureDimension2D` 是 8192，超过 8192 的边要么请求更高的设备上限、要么分块。所以引擎必须是**分块存储 + 按视口和缩放级别加载**，不能「一层一张纹理」。

## 2. 整体方案候选

| 候选 | 许可证 | 技术栈 | 成熟度（2026-10-22） | 能力覆盖 | 对本项目的问题 |
|---|---|---|---|---|---|
| **PhotoCraft**（storytold/photocraft） | Apache-2.0 / MIT 双许可 | 纯 Rust；wgpu 合成；egui 界面；可编译为 WASM 在浏览器运行 | 2026-09-30 建库，v0.12.0（2026-10-21）；约 7.1 万 star，2935 个未关闭 issue；自评 **alpha**，「可用于实际工作」约 45%，25 项性能预算中 22 项未达标 | 很广：27 种混合模式、剪贴/像素/矢量蒙版、图层样式、16 种调整图层、选择主体与选择并遮住、画笔引擎、ABR 导入、液化、操控变形、透视变形、修复画笔/仿制图章/修补、PSD 读写 | 太新，API 不稳定（每天数百个 PR）；界面是 egui（画在 canvas 上，不是 React，中文输入与无障碍弱，风格不可控）；「选择主体」不是 AI（`ml` crate 还没做，见其 architecture.md）；性能未达标（12MP 图层改不透明度 p50 276 ms，预算 20 ms）；它有自己的文档模型和历史，与我们的文档底座冲突 |
| **Photopea** | 闭源商业 | 自研 JS | 成熟 | 接近 PS | 只能 iframe 嵌入，通过消息接口脚本控制；官方页写自托管约 $500–2000/月，白标分发需单独商谈（第三方转述，价格未公开）；界面和存储都不是我们的，深度集成困难 |
| **photobaer**（IT-BAER/photobaer） | **AGPL-3.0**（可购商业许可） | Rust/WASM 引擎 + React + WebGPU，PSD 用 ag-psd | 3 star，未到 1.0 | 图层/样式/调整图层/修复/ABR 导入/PSD | AGPL 不能直接用；项目太小 |
| **Graphite**（graphite.rs） | 开源（以官方 LICENSE 为准） | Rust/WASM，节点式 | 2026-01 发布第二个桌面 RC；栅格编辑仍有限（第三方报道） | 偏矢量/程序化 | 栅格修图能力不足 |
| miniPaint / TOAST UI / Filerobot 等 | MIT 等 | Canvas2D | 维护放缓 | 基础图层和滤镜 | Canvas2D 合成能力和性能都撑不住专业修图，不考虑 |

PhotoCraft 的价值不在于直接嵌进来，而在于它是 **Apache-2.0 的、按论文 clean-room 写的算法参考实现**：液化（`crates/algo/src/liquify.rs`，位移场 + Catmull-Rom 上采样）、操控变形（`crates/algo/src/puppet.rs`，ARAP）、修复/修补、ABR 解析（`crates/io/src/abr_map.rs`、`crates/psd` 的 `abr` 模块）、27 种混合模式和图层样式。Apache-2.0 允许移植和修改，只需保留版权与 NOTICE。

## 3. 构件库

| 构件 | 许可证 | 用途 | 说明 |
|---|---|---|---|
| **ag-psd** 31.0.3（npm，2026-10-07 更新） | MIT | ABR 画笔解析（`src/abr.ts` 导出 `brushes` / `samples` / `patterns` 与画笔动态类型）；以后做 PSD 读写 | 不支持 16 位；图层样式只读写名称引用；文字图层不完整（README）。ABR 没有公开规范，所有解析器都靠逆向 |
| **transformers.js** 4.3.1 | Apache-2.0 | 浏览器端推理（WebGPU） | 官方示例 Segment Anything WebGPU 用 SlimSAM；有 BiRefNet、BEN2 的 ONNX 版本 |
| **onnxruntime-web** 1.30.0 | MIT | 浏览器端推理底座 | transformers.js 基于它 |
| three.js `WebGPURenderer` + TSL | MIT | GPU 合成 | 已在场景里使用；是否用于 2D 合成待穿刺（见 brainstorm U6） |

## 4. AI 选区

### 模型与许可证

| 模型 | 许可证 | 商用 | 适合做什么 |
|---|---|---|---|
| SAM 2 / 2.1 | Apache-2.0 | 可以 | 点选/框选交互分割 |
| SAM 3 | Meta 自定义 SAM License（2025-11-19） | 未明确写商用和 SaaS；有出口管制、禁用用途、赔偿条款 | 按概念文字分割（「选中所有人」）；上线前需法务确认 |
| SlimSAM / MobileSAM | 随 SAM（Apache-2.0） | 可以 | 浏览器端轻量交互分割 |
| BiRefNet | MIT | 可以 | 选择主体 / 去背景，边缘质量好 |
| BEN2 | MIT | 可以 | 同上 |
| ViTMatte | MIT | 可以 | 发丝级 matting（需要 trimap） |
| RMBG-2.0（BRIA） | **CC BY-NC 4.0** | **不可以**（需与 BRIA 签商业协议） | 排除 |
| @imgly/background-removal | **AGPL-3.0** | 不能嵌入闭源前端 | 排除 |

### 浏览器端

- 交互分割：SlimSAM 已有 transformers.js + WebGPU 官方示例；SAM 2.1 tiny/small 有 ONNX 社区版本，需要穿刺确认体积和速度。
- 选择主体：BiRefNet_lite ONNX fp16 约 115 MB，完整版 fp16 约 490 MB（Hugging Face onnx-community 仓库文件列表），首次下载重。
- 这类模型都在 512–1024 px 上推理。60MP 原片要先在低分辨率出蒙版，再放大并在全分辨率上做边缘细化（导向滤波或 matting），这一步本身就是「选择并遮住」要做的事。

### 第三方 API

| 服务 | 价格 | 限制 | 风险 |
|---|---|---|---|
| 阿里云视觉智能开放平台「通用高清分割」`SegmentHDCommonImage` | 通用分割按量约 ¥0.002/次（月 1 万次以内），高清分割单价未查到 | ≤ 40 MB、边长 < 10000，输出与输入同尺寸的四通道 PNG；异步，结果 URL 30 分钟过期 | 平台文档标注「已停止维护」，2023 年停过 26 项公测能力；长期可用性存疑 |
| fal.ai SAM 3（image） | 约 $0.005/次（fal 官方页） | 只对成功结果计费 | 海外服务；大陆访问和数据出境需评估；SAM 3 许可见上 |

## 5. 结论（供 brainstorm 采用）

1. **不整体嵌入现成编辑器**。Photopea 闭源、需授权、界面和存储都不是我们的；PhotoCraft 太新、alpha、性能未达标、UI 是 egui，且自带文档模型。
2. **推荐「自研外壳 + 自研分块 GPU 合成核心 + 构件库」**，与 3D 场景的选型同一思路：
   - 外壳、文档模型、存储、助手接入自研，接通用文档底座。
   - 合成核心自研（分块、按视口加载、WebGPU）。
   - 算法以 PhotoCraft（Apache-2.0）和论文为参考移植，ABR 用 ag-psd（MIT）。
3. **Epic 第一个子项做穿刺**，用同一组基准（60MP、10 层、画笔、液化、调整图层拖动）验证分块合成架构；并以「PhotoCraft 引擎编译成 WASM、只用它的引擎不用它的界面」作为对照组，测包体积、性能和 API 能不能嵌入。对照组明显更好时，再回来讨论要不要改选型。
4. **AI 选区分两条路**：
   - 点选/框选交互分割在浏览器端跑（SAM 2.1 或 SlimSAM，Apache-2.0，不产生调用费用）。
   - 选择主体优先走服务端 API（阿里云通用高清分割价格低、支持全尺寸）。阿里云平台的长期可用性存疑，所以接口要能换供应商。
   - 发丝细化先做 GPU 上的导向滤波边缘细化；matting 模型（ViTMatte）放到后续。

## 6. 局限

- PhotoCraft 的数据来自它自己的 README、ROADMAP 和 scorecard（自评），没有实测它的浏览器版本。
- 浏览器端模型的速度和体积来自模型仓库文件列表和官方示例，没有在目标设备上实测，留给穿刺。
- 阿里云高清分割、头发分割的单价和中国大陆访问 fal.ai 的稳定性没有查实。

## 来源

- PhotoCraft：https://github.com/storytold/photocraft （`ROADMAP.md`、`docs/architecture.md`、`docs/scorecard.md`；元数据经 `gh api repos/storytold/photocraft` 取得）
- PhotoCraft 报道：https://www.omgubuntu.co.uk/2026/10/artcraft-free-photoshop-ai-clone
- photobaer：https://github.com/IT-BAER/photobaer
- Photopea 自托管：https://www.photopea.com/api/accounts
- Graphite 现状（第三方）：https://itsfoss.com/graphite-graphics-editor/
- ag-psd：https://github.com/Agamnentzar/ag-psd ，ABR 源码 https://cdn.jsdelivr.net/npm/ag-psd@31.0.0/src/abr.ts
- SAM 3 许可证：https://github.com/facebookresearch/sam3/blob/main/LICENSE
- RMBG-2.0：https://github.com/Bria-AI/RMBG-2.0
- BiRefNet ONNX：https://www.huggingface.co/onnx-community/BiRefNet-ONNX/tree/main ，https://huggingface.co/onnx-community/BiRefNet_lite-ONNX/tree/main
- Segment Anything WebGPU 示例：https://huggingface.co/spaces/webml-community/segment-anything-webgpu
- 阿里云通用高清分割：https://help.aliyun.com/zh/viapi/developer-reference/api-universal-hd-split ；计费：https://help.aliyun.com/zh/viapi/product-overview/billing-is-introduced-1 ；停服公告：https://help.aliyun.com/zh/viapi/product-overview/about-visual-intelligent-open-platform-part-of-the-open-beta-ability-stop-service-announcements
- fal.ai SAM 3：https://fal.ai/models/fal-ai/sam-3/image
- ARAP 论文：https://www-ui.is.s.u-tokyo.ac.jp/~takeo/research/rigid
