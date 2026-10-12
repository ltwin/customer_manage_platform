# Agent 助手 Markdown 渲染增强

探索日期：2026-10-12。状态：已完成代码现状、候选库调研及 Owner 授权的隔离浏览器验证。推荐 Streamdown 加局部适配；尚未进入产品实施。仅实验目录安装独立依赖，未改产品源码/锁文件或创建 Git 提交。

## 原始诉求与已确认范围

Owner 认为 Agent 助手 chatbox 的 Markdown 渲染简陋，希望改善表格、公式、Mermaid 等格式，优先使用优秀开源项目或库；当前请求是探索。

## 核实事实

- 当前前端为 React 19.2.7 / Vite 8.1.1，依赖清单无 Markdown 库、Tailwind 或 Ant Design：`frontend/package.json`、`frontend/vite.config.ts`。
- `frontend/src/creative-canvas/editor/markdown.ts:47` 自写解析器只有 paragraph / heading / list / code 四类节点；围栏语言被丢弃。执行五个最小样本证实：表格、公式、链接、引用输出为普通 paragraph，Mermaid 输出为无语言 code。
- `frontend/src/creative-canvas/editor/AgentBlocks.tsx:52` 的 Markdown 是统一渲染入口；正文 text block（304）、展开的思考摘要（120）、旧历史回复 `AgentPanel.tsx:1049` 复用它。
- 现有块状态已提供 `streaming`，可以用于渲染器流式状态；思考摘要调用目前没有传递此状态。替换时需要区分仍在生成与已结束/中断内容。
- 样式由 `agent.css:567` 起的 `.cc-agent-text` 规则提供。改用标准渲染器后需要适配 strong、blockquote、table、pre/code 嵌套结构及 KaTeX，不能仅替换解析函数。
- `AgentPanel.tsx:386` 的底部跟随依赖消息变化及 store 通知。异步图表/字体导致的后续高度变化可能没有相应通知，这是待浏览器验证的集成风险，尚未复现。
- 原测试明确约定未闭合粗体保持原文，E2E 还定位 `b`：`frontend/scripts/creative-agent-blocks.test.ts:266`、`creative-agent-blocks.e2e.mjs:259`。新渲染器可能提前补齐格式并使用 strong，应转为验证可见效果、流结束一致性和块稳定性，不能照搬旧 AST 断言。

图谱证据限制：项目 `Users-samson-workspace-my_project-customer_manage_platform` 的索引 generation 为 `2026-10-08T09:27:36Z`；目标编辑器及测试文件 coverage 返回 `not_tracked`，package.json 返回 `metadata_changed`。已回退到直接源码读取、限定目录引用搜索和解析器执行。本结论是上述范围内的源码证据，不宣称图谱已覆盖整个最新项目。

## 建议及理由（尚未批准）

经隔离验证，推荐 **Streamdown + math / mermaid / code 插件及局部适配**。它针对流式 Markdown，基础 GFM、未闭合语法处理、公式、图表及交互能力组合较完整。独立渲染组件即可承接现有字符串和块状态，无需迁移会话传输层。

项目适配的主要代价是样式：官方内置控件采用 Tailwind utility 和 shadcn 风格变量，不能以为导入 `streamdown/styles.css` 就获得完整 UI。先验证局部样式适配；若需要大量自写控件或引入全局样式改造，则选择 **独立 X Markdown + LaTeX 插件 + 自定义 Mermaid 组件**。X Markdown 本身不依赖 antd，但官方现成 Mermaid 控件属于 `@ant-design/x`，直接照搬会引入另一套组件体系。

`react-markdown + remark-gfm + remark-math + rehype-katex + Mermaid` 为偏基础、可控的备选，适合愿意维护流式补全和图表交互层的情况。

详细比较、版本/许可证与来源见 [开源方案调研](research/renderer-options.md)。

## 建议的一次交付范围

1. 一个统一的 AgentMarkdown 展示组件覆盖正文、思考摘要、历史回复；保留业务 block 身份和原始 Markdown。
2. 基础 Markdown/GFM：表格、嵌套列表、任务列表、引用、链接、删除线、代码语言及高亮/复制。
3. 公式：行内与独立公式、错误源码回退；公式分隔符与金额误识别一起验收。
4. Mermaid：完整围栏后渲染、加载与失败回退、源码复制、放大查看；图表限宽、独立滚动。
5. 流式和布局：窄面板、长消息、停止生成、历史回放、异步高度变化、已向上滚动时不抢位置。

假设：此次聚焦 Agent chatbox，预计无后端协议和数据库变更。图片自动加载、HTML 富组件、下载导出属于另行确认的扩展，不把安装库的默认能力直接当作产品需求。

## 验收建议与待决项

- 三个主要格式在完整回复与历史回放中正常渲染；基础 Markdown 保持可读。
- 分片切在表头、粗体标记、公式、围栏中间时无页面异常；Mermaid 不随每个 token 重绘。
- 宽表格、长公式与图表不会撑破聊天面板；放大层不被画布/侧栏裁切。
- 不完整/错误内容可看到源码，不能因为一个图表失败丢失整条回复。
- 有界安全策略：原始 HTML 不作为任意组件执行、限制链接协议、图表严格模式、公式不启用 trust；外部图片行为单独确定。
- `make check-frontend` + 扩展已有 Agent browser E2E；实际测布局、滚动与样式；测 Vite 构建产物和重库加载时机，不从 npm 包大小推断首屏大小。
- Owner 尚需选择技术方向；推荐先以 Streamdown 做局部集成验证，再冻结正式方案。公式是否兼容 `$...$`、`\\(...\\)` 等写法在样本核对后确定。
- 已做 Streamdown 浏览器 PoC、加载量和一组长文本实验，见 [验证结果](research/streamdown-validation.md)。9 组检查通过，另记录中断语义待处理；并非多库性能排名或产品上线验收。

## 建议路径

**单 change，预计 1 个。** 渲染器替换、样式、流式集成和验收可以一次交付，不需要拆 Epic。当前仍是探索记录；后续进入 proposal/design 时沿用本 Feature ID。实现前按项目约定确认分支安排，提交仍需单独明确授权。

## 验证后的更新

Owner 已授权验证。普通 CSS 适配可行，无需全站引入 Tailwind；图表需 memo 和稳定渲染模式，滚动需尺寸监听及程序滚动辨别。插件按需加载降低纯文本首载。停止生成时的未闭合围栏不能仅依赖库 isIncomplete；需在正式设计补齐。选型验证完成，正式接入仍建议 1 个 change。

## 快速实施完成（2026-10-12）

Owner 随后授权在主工作区切分支实施，并明确改走快速流程；执行了 lti quick，不继续正式 proposal/design 门禁。分支为 `feat/agent-chat-markdown`。

产品改动仅在 frontend：统一 AgentMarkdown、按需公式/代码/图表、局部样式、内容尺寸驱动的滚动跟随。保留原始 Markdown，使用解析器提供的代码块范围检查真实围栏闭合，解决中断后及历史回放误出图的问题；图表加载或语法失败只回退该图源码。公式采用已验证的 $$ 行内/块语法，单美元符号保留为金额。外部图片显示替代文字。

验证：`make check-frontend` 通过（457/457 单测）；`make generate-check` 通过且生成物无 diff；`agent-markdown.e2e.mjs` 与 `creative-agent-blocks.e2e.mjs` 两套 Chromium 浏览器回归通过；`git diff --check` 通过。覆盖深色表格计算颜色、代码计算高亮、375px 宽内容、嵌套/重复 Mermaid、中断/刷新、加载失败局部降级、HTML/URL 样本、复制/放大、滚动和原块流回归。

没有后端、OpenAPI、数据库改动；没有升级原有依赖版本。Owner 在验收说明后明确授权提交本次改动。验证范围不包含真实模型请求或 Safari/Firefox。[桌面截图](research/implementation-evidence/desktop.png) / [手机截图](research/implementation-evidence/mobile.png)。
