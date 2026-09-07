# 创作空间先导原型 v3

本目录是 `creative-workspace-redesign` Epic 的 ITEM-1A 交付物：受版本控制、可直接用浏览器打开的静态原型，用于 `prototype-shape-go` 走查。它取代旧 `docs/prototypes/creative-shoot-planning/v2/`（旧策划工作台）作为新形态的参考，旧目录保持只读、不再更新。

## 页面

| 页面 | 用途 | 对应 Epic 范围 |
|---|---|---|
| [index.html](index.html) | 入口、页面地图、任务脚本入口 | — |
| [workspaces.html](workspaces.html) | 创作空间台账：未归类置顶、按最近打开排序、显示关联对象、旧策划记录以历史入口层级出现 | 一步创建、未归类空间、关联显示、新旧入口层级 |
| [workspace.html?id=…](workspace.html?id=ws_2) | 空间内页：灵感墙（图片优先等宽网格）、批量搬入、排序与分组、选卡建立「要拍的画面」、拍摄清单、拍摄备忘、可选关联 | 批量搬入、灵感墙、拍摄项、备忘、弱关联 |
| [live.html?id=…](live.html?id=ws_2) | 现场模式：参考图为主体，完成 / 撤销单步，跳过与补记不超过两层，备忘可拉出，断网只读与诚实失败 | 现场模式、离线只读、`live-unverified` |
| [TASKS.md](TASKS.md) | 冻结的走查任务脚本 `v3-tasks-1` | `prototype-shape-go` |
| [workspace.css](workspace.css) | 共享样式，token 与生产 `frontend/src/index.css`「暗房」同名同值 | — |
| [proto.js](proto.js) | 原型状态层（localStorage），跨页面保持，可重置 | — |
| [check.mjs](check.mjs) / [walk.mjs](walk.mjs) | 走查前自检：语法检查；playwright 自动跑轻主链并截桌面 + 375px 两套图（`NODE_PATH=<含 playwright 的 node_modules> node walk.mjs <base-url> <out-dir>`） | — |

打开方式：直接双击 `index.html`，或在仓库根目录 `python3 -m http.server` 后访问 `/docs/prototypes/creative-workspace/v3/`。示例图片引用 `frontend/public/marketing/`，保持仓库目录结构即可显示。

## 原型固定的形态意图

- **一步创建**：新建空间不弹任何表单，直接进入空的灵感墙；标题、关联都是之后可选的事。
- **批量搬入是主路径**：拖多张图按张成卡，粘贴多行按行成卡，链接原样成链接卡；页面任何位置都可以拖入。
- **灵感墙不弱于收藏夹**：图片是主体，等宽网格，文字卡与链接卡同尺寸并排；没有列表态。
- **排序与分组是唯一的结构**：拖拽换序、多选成组、组名可改；没有坐标、连线、嵌套。
- **要拍的画面由摄影师主动挑出**：多选后一键建立，值拷贝；来源被移走后仍显示当初内容，只是「查看原始灵感」变灰。
- **现场模式第一眼是参考图**：完成 / 撤销一步，跳过 / 补记两层；备忘从底部拉出，不主动弹。
- **关联与备忘是次要位置**：都在标题下方与次级 tab，不进创建路径，没有完成度。
- **旧策划记录是历史入口**：放在台账底部一行，不与「新建创作空间」并列。
- **空间只有可用 / 归档两态，没有删除**：台账每行「⋯」可起名、关联、归档；归档不弹确认但可在提示条上撤销，归档后进入底部「已归档」折叠区随时恢复。卡片由账号持有，归档空间不删卡片（Epic DEC-17 (d)）。

## 原型不承诺的事

- 不是 API、数据库或字段契约；`proto.js` 里的数据形状只为演示 Epic 共享语言，字段名不约束实现。
- 不含任何 AI、生成、画布、跨空间检索、知识库入口或占位按钮（Epic 验收 21）。
- 不含视频卡片；拖入视频会被明确拒绝。
- 素材来源分类、搬入批次原文等留门字段在原型里只落数据不出界面。
- 「模拟断网」等原型控制只面向走查，正式产品不显示。

## 生命周期

- 走查开始前计算并冻结本目录 SHA-256（`find docs/prototypes/creative-workspace/v3 -type f | sort | xargs shasum -a 256 | shasum -a 256`），写进每份走查记录。
- 走查期间只修 bug 与错字，不改形态；形态变化必须回到 Epic 讨论并重新冻结 hash。
- `prototype-shape-go` 为 `reshape` 时在本目录原位更新并升 task 版本；`stop` 时本目录归档只读。
