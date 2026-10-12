# THROWAWAY — Streamdown 选型实验

这不是产品代码。入口只使用内存数据，模拟 chatbox 的样式、分片和滚动。依赖安装在本目录，未修改 frontend 的依赖。

从本目录运行：

```sh
npm ci --ignore-scripts
npm run build
npm run preview
```

打开 <http://127.0.0.1:4179/>。另一个终端在本目录运行 `npm run verify`。需要 Playwright 对应的 Chromium（本次使用机器已有浏览器）。`npm run dev` 用于改实验，证据使用生产 preview。

- 默认：局部 CSS 适配 + 按需公式/代码/图表。
- `?raw=1`：默认库控件，仅用原项目 CSS，作为对照。
- `?case=text&closed=1`：测助手关闭到纯文本的加载成本。
- `?case=text&scrollfix=0`：模拟原事件触发的滚动方式。
- 主页面按钮可重播逐块输出或停止生成。

`verify.mjs` 生成 evidence/results.json 与截图。报告见 [验证结论](../streamdown-validation.md)。

限制：插件需求检测是本样本使用的简化版本；停止围栏语义尚未修复；不是产品发布候选。不要直接复制整个实验替换生产组件。
