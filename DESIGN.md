---
version: alpha
name: 影约 CRM · 暗房
description: 让摄影素材成为画面主体，以中性界面和少量琥珀指引操作。
colors:
  primary: "oklch(52% 0.12 55)"
  background: "oklch(97.6% 0.004 75)"
  surface: "oklch(100% 0 0)"
  text: "oklch(18% 0.008 60)"
  muted: "oklch(50% 0.01 60)"
omitted:
  - section: typography
    reason: 沿用 frontend/src/index.css 的 font-body、font-display、font-mono 及中文回退字体。
  - section: spacing
    reason: 沿用共享页面与控件样式；创意空间的网格间距仅由 creative.css 定义。
  - section: rounded
    reason: 控件圆角沿用共享 btn/input，内容容器由现有页面语义定义。
  - section: components
    reason: 共享组件及交互归属见 UX-CONTRACT.md，不复制一套组件 token。
---

# 暗房

这是现有界面的扫描记录，不引入新品牌。视觉权威是 `frontend/src/index.css` 开头的暗房约定与运行时 token；本文件解释设计意图。颜色修改首先发生在该权威文件，再同步本记录，不从此文件生成第二份 CSS。

照片是唯一的大面积高饱和内容。界面使用暖轴中性色；琥珀只服务主要动作与焦点。标题、正文和数值分别消费 `--font-display`、`--font-body`、`--font-mono`。明暗主题由 `AppShell` 管理，通过同名变量切换。

创意空间采用 `docs/prototypes/creative-workspace/v3/` 的图片优先网格：素材按等宽单元并排，正文与链接也占一个单元；现场模式把参考图作为视觉中心。避免经营仪表盘、完成度、流程步骤与营销说明挤占素材位置。

共享按钮与表单沿用 `frontend/src/index.css` 的 `.btn`、`.btn-primary`、`.input`；新页面只增加布局类，不重新定义全局品牌色。反馈、确认和键盘行为见 UX-CONTRACT.md。

桌面采用宽网格，375px 下保留双列灵感墙、单列空间列表与拍摄清单；拍摄动作保持可触达。颜色、字体的实际计算值及布局以浏览器走查为证据。
