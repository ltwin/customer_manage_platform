# Streamdown 浏览器验证结果

日期：2026-10-12。结论：**适合本项目，建议采用；需要一个局部适配组件，不能直接替换后就视为完成。** 本轮仅创建隔离实验，没有修改产品源码、产品 package.json/锁文件，也没有提交。

## 环境与证据

- 独立 npm 项目位于 [streamdown-poc](streamdown-poc/README.md)，使用 React 19.2.7、Vite 8.1.1、Streamdown 2.7.0、math/mermaid 1.0.3、code 2.0.0，版本由独立锁文件固定。
- 使用现有 workspace 深色 token 与 agent.css 的快照，尺寸覆盖桌面及 375px 手机视口；通过 Playwright Chromium 检查实际 DOM、computed styles、滚动、剪贴板及弹窗。
- 实际测试的是 Vite **生产构建 + preview**，不是只在 dev server 验证。
- [机器结果](streamdown-poc/evidence/results.json)：9 组检查通过，0 个页面异常；另记录 1 项中断行为观察，它不算通过的中断验收。
- 没有做真实后端 SSE、产品登录态、完整历史消息迁移或多浏览器验收；这是一组用于选型的有界实验。

## 功能与布局

| 检查 | 实测结果 |
| --- | --- |
| 表格、嵌套列表、任务列表、公式 | 正常；样本美元金额 $5 / $10 未被误识别为公式 |
| 中文 Mermaid | 流程图正常；源码复制成功；原生 dialog 放大不被聊天面板裁切，Escape 可退出 |
| 代码高亮与复制 | 检查到至少 3 种 computed color；行结构正确；复制内容含 console.log |
| 375px 视口 | 表格和长公式未撑破面板；长公式在宽 304px 的内部容器中滚动，内容宽 686px |
| 任意分片 | 587 字符样本逐字符输入后表格/公式/图表均正常 |
| Mermaid 更新次数 | 围栏未闭合时 0 次；闭合时 1 次；后续 20 次文字追加以及流结束仍为 1 次，SVG 节点保留 |
| 错误 Mermaid / 公式 | 不导致页面异常；图表显示源码，未知公式命令仍可见。完整公式源码切换尚待正式组件实现 |
| 最小恶意内容样本 | script、javascript: URL 和外部图片未执行/加载；不代表完整安全审计 |

直接接入时，默认表格按钮纵向堆叠、表格缺少排版样式；代码高亮虽生成 token，缺少 Tailwind 对应规则时文字仍只有一种颜色。这说明只看 HTML 或单测不足以验证本任务。

**本次证明不必为 chatbox 引入全站 Tailwind。** 实验用约 38 行紧凑的局部 CSS 和一个适配模块解决展示；实际正式代码会展开格式并补齐状态/错误分支，这不是实施行数估计。代码控件的定位/可访问性以及其他语言仍需最终验收。

截图：[直接接入](streamdown-poc/evidence/raw-top.png)、[局部适配](streamdown-poc/evidence/adapted-top.png)、[图表](streamdown-poc/evidence/adapted-diagram.png)、[放大](streamdown-poc/evidence/diagram-expanded.png)、[手机](streamdown-poc/evidence/mobile-top.png)、[代码高亮](streamdown-poc/evidence/code-block.png)。

## 流式与滚动的具体发现

1. **图表节点替换**：初版适配虽然没有重新调用 Mermaid，后续 token 仍会替换 SVG 节点。用 memo 包住图表组件，并保持流式/结束后的分块渲染模式一致，验证可保留节点。不要把 isAnimating=false 简单等同于切换整个渲染树到 static。
2. **异步高度**：用模块加载延迟重现原跟随机制，图表完成后距底部 348px；加入 ResizeObserver 后为 0px。手动滚到顶部后，追加内容不会抢回底部。
3. **长消息竞态**：长回复中，程序滚动产生的 scroll 事件可能在下一次内容增长后被误认为手动离开底部。实验增加程序滚动位置辨别和 overflow-anchor:none，最终长回复也保持距底部 0px。正式接入需要继续覆盖触摸、键盘和折叠面板。
4. **中断仍有未决语义**：isAnimating 变为 false 后，库给自定义 renderer 的 isIncomplete 会变成 false，即使实际围栏没有闭合；有效 Mermaid 源码会开始绘图。停止样本里的未闭合粗体保留原文。正式组件应分别表达“正在接收 / 正常结束 / 被中断”，并保留原始源码。此项是接入任务，不宣称当前实验已解决。

## 加载成本

按浏览器实际请求的本地构建文件去重，再对 JS 文件离线 gzip（十进制 KB）计算。以下是**累计**值，包含 React 和实验页面壳；不包含 CSS、字体，不是本项目最终 bundle 增量或真实服务器传输量。

| 阶段 | 累计 JS gzip | 比上一阶段增加 |
| --- | ---: | ---: |
| 页面壳，助手未打开 | 63.5 KB | 63.5 KB |
| 打开助手，纯文本 | 220.4 KB | 156.9 KB |
| 再显示公式 | 301.8 KB | 81.4 KB |
| 再显示 TypeScript 代码 | 383.6 KB | 81.8 KB |
| 再显示 Mermaid 流程图 | 593.9 KB | 210.3 KB |

- 原先静态导入 math/code 的实验，纯文本累计约 359.0 KB；按内容加载后为 220.4 KB，减少约 39%。助手关闭时不会请求渲染器；纯文本不请求公式字体或 Mermaid。
- 公式阶段另请求约 28.8 KB 未压缩 KaTeX CSS、42.7 KB woff2 字体（本组公式所需）；更多公式/字体可能不同。
- 全部构建产物约 15.35 MB、498 个文件，包含许多未请求的语法/图表分块，以及 raw 对照入口。**按需加载改善网络首载，但不会自动消除部署产物大小。** 正式构建要复测，若需要缩减部署包，再裁剪语法和图表类型。
- 在本地 Chromium 单次 22,210 字符、每 10ms 追加 160 字符的样本中，约 1.42s 完成，未记录到 >=50ms 的 long task。它只说明此样本可用，不外推为移动设备或超长多轮历史的性能保证。
- PoC 中将两个 dynamic import 写在一个条件表达式里，生产构建曾出现加载另一个插件依赖且缺失 KaTeX CSS 的结果；改为独立 loader 函数后，网络及样式实测恢复。这是本构建组合的复现事实，不泛化为所有 Vite 版本缺陷。

## 推荐接入方案

保持现有 Agent block/SSE 数据结构，新建统一 AgentMarkdown：Streamdown 负责通用 Markdown；公式、代码与 Mermaid 按需加载；Mermaid 用轻量自定义 renderer 实现延迟出图、源码和放大；沿用项目样式 token。

一个正式 change 内完成：统一入口替换、局部 CSS、图表缓存/失败回退、中断语义、滚动跟随、历史回复/思考摘要、针对性的 browser 验收和 make check-frontend。实验中插件需求检测只覆盖 $$ 与少量代码语言，不能直接作为生产版完整语法判断。网络加载失败处理、多图表并发/主题切换、真正历史回放以及链接/图片策略均需正式实现。

不需要因这次结果改选 X Markdown；如果后续产品决定大规模采用 Ant Design X 整套交互组件，再重新比较它的整体价值。
