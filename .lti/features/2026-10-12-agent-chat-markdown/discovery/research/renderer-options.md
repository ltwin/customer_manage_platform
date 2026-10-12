# Agent chatbox 开源 Markdown 方案调研

日期：2026-10-12。方法：Context7 解析并查询 `/vercel/streamdown`、`/remarkjs/react-markdown`、`/ant-design/x`；交叉读取官方站点/仓库和 npm Registry 发布元数据。仅研究与现有 React chatbox 的适配。

## 比较

| 方案 | 表格 / 公式 / Mermaid | 流式能力 | 本项目代价 | 建议 |
| --- | --- | --- | --- | --- |
| Streamdown | GFM 内建；math、mermaid、code 为可选插件 | 针对不完整 Markdown；分块及记忆化；有围栏完整性 hook | 内置 UI 的 Tailwind 样式、主题变量需要适配；重库需验证拆包 | 优先验证，功能组合最贴近诉求 |
| 独立 X Markdown | GFM；LaTeX 插件；Mermaid 用自定义组件或 Ant Design X 控件 | hasNextChunk、未完成片段组件映射、streamStatus | 本体不依赖 antd；现成 Mermaid 交互组件属于 @ant-design/x | 无 Tailwind 接入诉求下的有力备选 |
| react-markdown 组合 | remark-gfm；remark-math + rehype-katex；自定义 Mermaid | 本体渲染传入字符串；未闭合修复与图表调度需补充 | 更自由，但需要维护更多集成代码 | 偏基础、可控的备选 |

这不是性能排名。没有运行三者同等功能基准，不能依据官方宣传或解析器速度断言完整聊天体验优劣。

## 发布与兼容快照

直接读取 npm Registry `latest`（实施时应锁定并再核对）：

| 包 | 版本 | 许可证 | React peer |
| --- | --- | --- | --- |
| streamdown | 2.7.0 | Apache-2.0 | 18 或 19 |
| @streamdown/math | 1.0.3 | Apache-2.0 | 18 或 19 |
| @streamdown/mermaid | 1.0.3 | Apache-2.0 | 18 或 19 |
| @ant-design/x-markdown | 2.9.0 | MIT | >=18 |
| react-markdown | 10.1.0 | MIT | >=18 |

当前项目 React 19 满足上述 peer 声明，这不等于已通过构建/浏览器验证。Streamdown 2.7.0 的 npm 发布时间为 2026-09-30。X Markdown 仓库 main 中 marked 范围与 npm 2.9.0 发布包有差异，应以选定版本包和锁文件为准，不能直接把 main 示例当成发布版事实。

Registry 来源：[Streamdown](https://registry.npmjs.org/streamdown/latest)、[math](https://registry.npmjs.org/@streamdown/math/latest)、[mermaid](https://registry.npmjs.org/@streamdown/mermaid/latest)、[X Markdown](https://registry.npmjs.org/@ant-design/x-markdown/latest)、[react-markdown](https://registry.npmjs.org/react-markdown/latest)。

## Streamdown 接入注意点

- 独立安装即可消费 Markdown 字符串；AI SDK 的 useChat 只是官方示例，不是必须迁移的依赖。GFM 提供表格、任务列表、删除线，增强能力通过插件加入。[官方仓库](https://github.com/vercel/streamdown)、[使用说明](https://streamdown.ai/docs/usage)。
- math 基于 KaTeX，需要字体/CSS。当前默认关闭 single-dollar 数学语法，避免金额被识别为公式。需要标准 `$...$` 时显式配置并测试金额混排；`\\(...\\)` / `\\[...\\]` 不能假定天然兼容。[公式文档](https://streamdown.ai/docs/plugins/math)。
- Mermaid 插件含复制、下载、全屏等交互。文档提供 `useIsCodeFenceIncomplete`，可在围栏闭合后才渲染，避免逐分片重绘；错误回退和 strict 模式可配置。关闭中的代码围栏不等于消息结束，停止生成时应保留源码。[图表文档](https://streamdown.ai/docs/plugins/mermaid)。
- Tailwind utility 需要生成对应样式，主题依赖 CSS 变量；`streamdown/styles.css` 主要处理动画，不能替代完整 utility CSS。普通文本可用组件覆盖和 `data-streamdown` 选择器，但控件布局、全屏层、焦点样式都需验证。[样式文档](https://streamdown.ai/docs/styling)、[assistant-ui 官方集成说明](https://www.assistant-ui.com/docs/guides/streamdown)。
- 默认含 sanitize 和 harden，但外部图片等策略较宽；自定义 rehypePlugins 会替换默认数组，应保留所需防护。项目应显式决定允许的 HTML/URL/图片范围。[安全文档](https://streamdown.ai/docs/security)。

## X Markdown 与 react-markdown 的定位

X Markdown 可单独安装，已核对发布包 dependencies 不含 antd。LaTeX 从其 plugins 子路径导入；现成 Mermaid 控件从 `@ant-design/x` 导入，需要区分本体与组件库。若希望保持当前视觉体系，可使用自定义 Mermaid renderer，但需自己处理缩放/错误/完成状态。[独立库说明](https://x.ant.design/x-markdowns/introduce/)、[插件说明](https://raw.githubusercontent.com/ant-design/x/main/packages/x/docs/x-markdown/plugins.en-US.md)、[Mermaid 控件](https://x.ant.design/components/mermaid/)、[流式说明](https://x.ant.design/x-markdowns/streaming/)。

react-markdown 的 remark/rehype 插件路径明确、结构容易控制；GFM 与 KaTeX 官方 README 都有组合示例，默认安全渲染但自定义插件/URL 策略仍需检查。用于本项目意味着额外承担未闭合语法、Mermaid 完整性判定及交互工作。[官方文档](https://github.com/remarkjs/react-markdown)。

## 推荐验证方式

采用同一组中文样本验证：宽表格及单元格内粗体、嵌套/任务列表、行内/块公式、金额混排、中文 Mermaid 流程图与时序图、语法错误、在任意字符处切片、停止后保留不完整内容。分别检查实时正文、思考摘要和历史消息。

Streamdown 样式与重库加载作为首个技术验证点：只在隔离实验或正式授权的 change 中安装，记录实际构建分块和浏览器表现。如果复用内置控件必须引入过多全局样式改造，改选 X Markdown 组合方案。不要以未测量的包体积或库名替 Owner 决定成本。

本轮只执行了现有 parser 的五个最小样本，未运行候选库 PoC，也未变更 package.json/锁文件。
