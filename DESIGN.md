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

## v4 原型设计方向

2026-09-07 owner 指定自适应长宽比瀑布流、懒加载与空白卡预占位，以及液态玻璃和优雅艺术气质。该方向落在独立的 [v4 原型](docs/prototypes/creative-workspace/v4/README.md)：浅雾灰绿、宋体标题、浮动玻璃导航/筛选栏，照片完整保留比例。原型运行时 token 由该目录 studio.css 管理，映射与意图见其 DESIGN.md；只用于原型走查，不改生产 index.css 或既有「暗房」约定。

同日项目原型进一步展开为概览、参考、策划、画布预览和本地现场演示，由 project.css 消费相同 token。原型可领先正式开发，但未来能力明确标注并禁用，不以视觉占位冒充实际功能。

## v5 原型设计方向

2026-09-08 owner 指定暗色基调与液态玻璃：左侧工具/资产侧栏、中央通用画布、右侧 Agent；资产库分个人库和公共市场。进一步明确策划详情是策划节点的最大化状态，不能作为侧栏独立子页面。落点为 [v5 夜间创作台](docs/prototypes/creative-workspace/v5/README.md)，运行时 token 由其 tokens.css 统一管理，资产侧栏共享该主题。v4 默认视觉和生产 CRM 保持原有约定。

2026-09-09 资产库方向更新：v5 改为原生画布辅助侧栏，用可嵌套分组导航和实时标签检索替换 v4 集合封面与独立页面交互。支持窄栏取用、拉宽整理、批量拖入画布、节点存回个人库，以及按类型导入；具体行为见 v5 的 DESIGN 与 UX-CONTRACT。

画布节点支持独立的布局分组：框选/多选打组，拖标题移动子树，子节点使用父组相对坐标；解组不改变画布位置。它与资产库分组分别承担布局组织和素材归类。

## FND-01工程验证页

`frontend/creative-lab.html`与`src/creative-canvas/lab/`仅供Vite开发环境验证，采用v5暗色/玻璃语言，运行时样例token由lab/style.css拥有。节点/边是React Flow本地view-model，不是生产API DTO；不保存业务数据。框选/平移和父子坐标沿当前基础需求，按钮/焦点/采样反馈限该独立页面，不改旧CRM组件。正式构建不把此HTML列为入口。
