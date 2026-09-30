# 创作工作台 v6 迁移进度

更新：2026-09-30。工作分支为 `feat/creative-workspace-redesign`，工作目录为 `.worktrees/creative-workspace-redesign`；原型为 `docs/prototypes/creative-workspace/v6`。本次提交包含下述迁移代码、契约生成物、测试和截图，未合并或发布。

## 接手时的状态

外框与节点基础交互已有 `4006741`、`cf26ffc` 两个提交。资产抽屉、批量拖入、从库填充空节点、节点存回分组及对应后端契约存在未提交改动。本轮保留并验证这些改动，未将其归为本轮新增实现。

## 本轮实现

- 节点生成面板改为 460px 紧凑输入框，使用原型的实色底、16px 圆角、模型与参数 chip、白色圆形生成键。参数按真实模型目录呈现，不写死模型或点数。
- 显示草稿参考、节点输入和已连接上游的参考缩略图。鼠标悬停或键盘聚焦可放大查看；连线参考的移除走画布命令，保留撤销路径；资产库入口复用多选、放入目标左侧并连线的流程。
- Enter 保存并生成；Shift+Enter 换行；输入法确认不触发生成；参数面板支持点击外部、Escape 关闭及焦点归还。节点拖动期间隐藏生成面板，松手恢复。桌面输入框在节点下方居中；手机输入框使用可用宽度并避开底部工具轨。
- 创作助手的对话选择与新建入口收进标题菜单，模型与 Skill 放在输入框底部，发送键改为圆形。支持 Enter 发送、Shift+Enter 换行与输入法确认；保留原有外发授权、未确认操作恢复及待补充任务流程。
- 助手入口移到右上角，解决手机端与底部工具轨重叠的问题。

## 验证

- `make check-frontend`：构建、lint 与 390 项测试通过。lint 保留既有 Fast Refresh 和资产库浏览器脚本未使用变量警告。
- `make generate-check`：通过，无生成物漂移。
- `creative-library.e2e.mjs`：通过，抽屉实测宽度 344px、左侧位置 64px、底色 `#1b1b1d`。
- `creative-generation.e2e.mjs`：通过，验证草稿保存与刷新恢复、Enter/Shift+Enter/输入法、参数关闭、连线参考、资产库选择模式、拖动时收起，以及桌面/手机布局；页面错误为 0。
- `creative-agent.e2e.mjs`：通过，验证标题菜单、输入框布局、延迟授权回复的两种顺序不重复执行，以及刷新和拒绝后恢复原文、模型、Skill、会话。
- `creative-canvas-interaction.e2e.mjs`：通过，覆盖文字编辑、磁吸端口、分组、版本预览、派生节点、复制与撤销。
- 真库媒体验证：`CREATIVE_EDITOR_URL=http://localhost:5173 CREATIVE_EDITOR_SCRIPT=creative-media.e2e.mjs go test ./internal/platform/httpapi -run '^TestCreativeEditorBrowser$' -count=1 -timeout=8m -v` 通过，使用隔离 PostgreSQL、真实媒体校验与本地媒体工作器。账号由测试生成，不使用真实账号。
- 首次模拟浏览器检查与生成物检查并跑时出现导航中断和悬停超时；停止源码改写后重跑通过。浏览器验证期间应避免 Vite 热重载。
- `make check-go`：后端构建、lint（0 issues）和全量测试在第二轮通过。首轮在 `shootplanning/business` 的容器启动阶段因 Docker 查询超时失败；该包单独重跑通过后，按默认 `-p=4` 完整重跑通过，未缩小范围或跳过用例。

截图位于 `docs/dev/evidence/creative-workspace-v6/`。这些截图与数据均为测试样例；模拟生成面板验证不代表真实供应商生成已验收。

| 画面 | 桌面 | 手机 |
|---|---|---|
| 节点生成面板 | [桌面截图](evidence/creative-workspace-v6/composer-desktop.png) | [手机截图](evidence/creative-workspace-v6/composer-mobile.png) |
| 创作助手 | [桌面截图](evidence/creative-workspace-v6/agent-desktop.png) | [手机截图](evidence/creative-workspace-v6/agent-mobile.png) |
| 真库媒体画布 | [桌面截图](evidence/creative-workspace-v6/media-desktop.png) | [手机截图](evidence/creative-workspace-v6/media-mobile.png) |

## 本轮继续迁移

- 提示词改为浏览器编辑器管理的内容区。`@` 只筛选已连接的参考，支持方向键、Enter/Tab、Escape、整块删除、原生撤销、复制粘贴和悬停预览。普通粘贴保持纯文本；32000 字符上限按 Unicode rune 计算，不截断引用 token。草稿保存 `@[节点ID]`，缩略图、标题及编号随当前连线快照更新；断开连线会清理对应 token。
- 从资产库选参考后，节点竖排放入目标左侧并连线，引用同时写入目标节点的本机草稿。回填不依赖输入框是否可见，切换节点再回来仍能看到引用。修复拖入资产库后 React Flow 遗留 dragging 标记导致输入框一直隐藏的问题：显示状态改由实际拖动开始、结束和指针释放事件驱动。
- 服务端编译句内引用。媒体转为 `[图1]`、`[视频1]` 等编号；文本在引用处展开。编号、附件顺序和重复引用沿用准入排序。编译后的内容重新通过冻结策略的校验、计价和指纹计算，最终取得业务锁后再次复核；使用参考的授权仍然逐项检查。
- 主版本默认跟随最新生成结果；手动选择版本会固定，后续生成只登记新版本。可以改回跟随最新。模式与版本一起进入图的撤销链，恢复自动时校验目标确为当前最新版本。生成溯源记录参考节点当时的内容修订，参考行能提示“已更新”。
- 全屏媒体预览新增上一版、下一版、键盘左右切换、版本编号与设为主版本。临时预览不发送画布写命令，设主版本仍走保存队列和版本校验。
- 新建空图片、视频默认 1:1，显示比例标记。真实媒体与不同画幅的临时预览都按原始尺寸适配节点，去掉旧布局额外预留的 74px；缩放保持画幅，音频保留播放器尺寸。引用左侧排布与本机投影使用同一比例规则。

## 本轮验证

- `make check-frontend`：构建、lint、390 项测试通过；没有新增 Hook 依赖警告。保留既有 Fast Refresh 与资产库脚本未使用变量警告。
- `make generate-check`：通过。`select_version` 增加可选的 `follow_latest`，两端生成契约已更新。
- `make check-go`：最终按默认 `-p=4` 全量通过，lint 为 0 issues。中间两轮分别在 `dataexport`、`creativeops` 和 `reminder` 的容器启动阶段出现 Docker 查询超时；失败包单独重跑均通过，再次全量运行通过，没有跳过用例或降低并发度。
- 新增后端测试覆盖引用排序与编号、文本展开、断线拒绝、真实数据库中的草稿修订检查与生成用途授权，以及编译后重新校验、计价和快照恢复。真实生成工作器测试覆盖固定主版本后再次生成不覆盖，以及恢复自动后更新到最新输出。
- `creative-generation.e2e.mjs`：草稿恢复、键盘过滤选择、整块删除、macOS 原生撤销、纯文本粘贴、引用 token 粘贴恢复、断线清理、桌面/手机布局及拖动结束恢复显示通过。
- `creative-library.e2e.mjs`：资产多选、左侧排布和连线通过；新增检查确认所选两项自动成为提示词中的 chip。
- `creative-canvas-interaction.e2e.mjs`：版本预览不改主版本、不同画幅预览改变节点尺寸、全屏版本键盘切换和原有画布交互通过。
- `CREATIVE_AGENT_URL=http://localhost:5173 node scripts/creative-agent.e2e.mjs`：通过，延迟授权与草稿恢复行为保持成立。
- 真库媒体流程再次通过：64×48 媒体按 280×210 显示，覆盖上传、替换、资产与节点绑定；使用隔离 PostgreSQL 和真实媒体工作器。

浏览器生成交互使用模拟模型；服务端策略、工作器及主版本测试使用测试部署，不表示真实供应商已配置或验收。

新增截图：[句内引用](evidence/creative-workspace-v6/mentions.png)、[全屏版本浏览](evidence/creative-workspace-v6/fullscreen-versions.png)。

## 剩余差异

整体迁移仍未全部完成，主要还剩：

1. 图片局部重绘、扩图、放大等工具及对应正式生成能力接线。
2. 资产库“最近使用 / 本画布”视图，以及删除分组后的 toast 撤销。
3. 模型选择 popover、比例/数量分段控件及正式预算估价展示；不应迁入原型硬编码点数。
4. 助手的画布上下文引用交互和项目级一次授权；目前保留正式服务端的逐次授权协议。

后续继续按原型交互实现，同时保留真实目录、保存队列、权限和版本校验。
