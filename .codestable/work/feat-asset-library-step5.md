---
feature: asset-library-step5
status: verified
date: 2026-10-12
commit_authorized: true
---

# 资产库第 5 步：导入与回收站

## 目标

继续第 4 步（原提交 `ba7324d`；工作期间仓库同步 develop 后经 rebase 为 `e7984da`），实现已批准设计 §4.5 / §3.7 的 067.3 与 V6-020 验收。owner 授权继续下一步，并在反馈修复后明确授权提交本步；缩略图性能优化继续延期，见 [性能 issue](issue-asset-image-renditions.md)。

## 现场

沿用 `.worktrees/creative-workspace-redesign` 与 `feat/creative-workspace-redesign`。设计：[资产库第 2 期](../features/2026-10-11-asset-library/asset-library-design.md)。交互与样式依据 `docs/prototypes/creative-workspace/v6/assets.html` / `assets.mjs` / `assets.css`。设计明确覆盖原型中的演示行为：同名导入不查重、链接不抓远程标题、期限读取真实账号设置。

代码图 generation `2026-09-28T12:10:12Z` 陈旧；结构检索定位后对候选文件检查 coverage，源码未跟踪或元数据变化的路径均直接读取。图里的旧行号与当前源码不一致，不作为实现证据。

## 边界

- 独立页导入菜单、文件拖入、剪贴板文件/HTTP(S) 链接/文字；上传复用 `useUploads` 和 `importPlan`，固定入队时的分组，逐项说明不支持、超限与空间不足，不做同名查重。
- 文字/链接通过现有 `/assets` 契约创建；输入框和弹窗内粘贴不被全局拦截。未知创建结果保留同一个操作身份重试。
- 回收站检查器、批量栏、右键菜单与查看浮窗提供恢复/永久删除；期限由各自 `purge_after` 计算。新增设置入口支持 7/30/90 天或不清除，并明确旧素材保持原期限。
- 清空回收站先读无筛选的回收站总数与 `library_revision`；409 保持确认框，重读数量后再确认；未知结果重试原请求，不能换版本重新删除。
- 新增 `library.trash_expiry`，由 creative-worker 启动时及每小时扫描。maintenance 队列单并发、每账号每批 100 项，账号分页没有固定总数截断；回执、删除和后续批次入队在同一事务中提交。按已有资料库写入锁序执行，复用 `purgeAsset`。
- 同时清除超过 30 天的分组删除快照，包括只有快照而没有到期素材的账号。停用账号沿用 jobs 拒绝执行规则。
- 不修改 HTTP API、数据库 schema 或原图/展示图管线。按批准设计 §6，本任务只释放资产保留根，不新增通用物理 GC；真实 local 对象验证覆盖仍有引用时字节可读、最后根释放后不再授权读取，不能据此声称已回收物理字节或配额。

## 证据

- 新增到期清理测试先因缺少实现编译失败；随后真实数据库验证：到期/未到期/不自动清除/跨账号、205 项分批、重试不多删、后续入队失败整批回滚、快照单独到期和启动扫描/停止。
- 新增真实数据库 + local 对象存储测试：恢复、单个删除、批量删除、清空、到期清理之后独立节点仍可通过 ticket 读取同一 PNG 字节；最后根移除后的可回收资格验证。
- 恢复保留最爱和有效分组，不复活期间已删除分组。
- 前端门禁：469 项测试通过；生成物检查无漂移。
- 浏览器定向验证：导入文字/链接、同名文件、跳过原因、拖放/剪贴板文件、空间不足、恢复/取消删除/永久删除、清空冲突/未知响应原身份重试、手机设置与批量操作、查看浮窗删除。
- 原型实测：标题区、导入按钮、回收站提示条、剩余天数标签的 padding/gap/fontSize/lineHeight/borderRadius/backgroundColor/color 一致。保留设置和旧期限说明为真实功能所需补充。

本机日志：`/tmp/codex-step5-frontend-final.log`、`/tmp/codex-step5-generate.log`、`/tmp/codex-step5-expiry-r3.log`、`/tmp/codex-step5-go-r2.log`、`/tmp/codex-step5-media-retention.log`、`/tmp/codex-step5-e2e-full.log`、`/tmp/codex-step5-viewer-purge.log`、`/tmp/codex-step5-drawer.log`、`/tmp/codex-step5-prototype.log`。截图：`/tmp/creative-assets-qa/step5-*.png`。

首轮单包测试遇到 Docker 创建期超时；同包重跑进入断言并发现测试数据/分页调用问题，已修复，最终结果按实际运行记录，不把后续断言失败归为环境问题。

## 验收

V6-067 AC-07/08 与 V6-020 AC-01～04 已完成并回写；后者的媒体回收边界遵循批准设计 §6，只验证保留根与回收资格，不宣称通用物理 GC 已实现。浏览器使用 mock API；后端另用真实 PostgreSQL 与 local 存储，二者不混称端到端真实账号验收。

## 状态与未决

后端门禁已运行，媒体测试参数错误修正后整包通过；dataexport 容器启动失败后按规则三轮复核（第一轮另一独立容器启动失败，后两轮整包通过）。独立复审已通过；尚未提交。新增维护队列/持久化清理触发独立 change review；当前无 Paseo/Claude 创建能力，使用 native collaboration 最强可用稳定模型 gpt-6-astra / high，审查中冻结候选，不修改源文件。

老的真实账号媒体/文字整套 E2E 包含多处早期界面选择器，尚未整体迁移或运行；本步已用独立页回归和真实后端媒体测试覆盖新功能，未声明旧脚本全量通过。


### 独立审查

- R1：`/root/asset_step5_review`，gpt-6-astra / high；完整 patch SHA-256 `3c7458e850b2ca42d292822136a918d3d6de16d093fa3a58e139c921d7f34ffc`，21 文件。发现两项 blocking：后台上传完成时自动导航丢失清空回收站的未知请求；导入在确定成功前关闭表单导致被拒绝后丢失草稿。
- 修复：导航只在摄影师主动开始导入时发生，完成回调只刷新数据；导入成功才清除草稿，确定拒绝保留正文与标题，未知结果可在模态内部重试原操作。正文 100000 字、链接 4096 字及 URL 账号信息校验与后端对齐；全局粘贴校验失败或服务端拒绝时打开保留全文的表单。
- 失败先复现：`/tmp/codex-step5-review-red.log` 两个真实浏览器失败、`/tmp/codex-step5-bounds-red.log` 长度校验失败；修复后 `/tmp/codex-step5-review-green.log` 与 `/tmp/codex-step5-r2-qa-final.log` 通过，包含粘贴超长/含账号网址/服务端拒绝。
- 前端最终门禁 `/tmp/codex-step5-r2-frontend.log`：468 项通过。原候选完整 30 组浏览器回归全通过 `/tmp/codex-step5-e2e-final.log`；修复后新增三组定向回归通过，后续完整回归另记录。
- 后端 `/tmp/codex-step5-go-r2.log` build/lint 通过，完整测试仅媒体新测试缺 Limit（已修正）与 dataexport 容器创建超时失败。媒体整个包重跑 `/tmp/codex-step5-media-full.log` 通过；dataexport `/tmp/codex-step5-dataexport-r1.log` 为测试内部独立容器创建超时，`r2.log` / `r3.log` 两轮通过。未降低测试并行度或跳过断言。

- R2：完整 patch SHA-256 `28423e5fe9eebeb6bf1efb5149633028de8b9ffce106cd2eb0bf23f4e30773b6`；两项旧问题 resolved，新增一项链接校验 blocking：原文 `@` 正则越过 authority 边界误拒绝 query/fragment 中的合法 `@`。修复为仅扫描 authority，同时保留空 userinfo 的拒绝；加入 query、fragment、path 中合法 `@` 的回归。`/tmp/codex-step5-url-red.log` 已先复现。
- 修复 R1 后完整 33 组浏览器回归通过，日志 `/tmp/codex-step5-r2-all-qa.log`。R3 只调整 URL 纯校验与对应单测，后端和其他交互不变。

- R3：完整 patch SHA-256 `ba782961123ac247c4899eef64c3c13de2b3039e9712bde3dab763000b628060`；21 文件 manifest 一致。旧问题全部 resolved，无 unresolved 或新增 finding，终态通过。469 项前端测试通过 `/tmp/codex-step5-r3-frontend.log`；链接边界回归绿灯 `/tmp/codex-step5-url-green.log`。

毕业清单：验收状态回写 canonical V6-067、V6-020 与需求总表；并发导航、草稿保留与链接边界约束进入可执行回归，不新增 lesson。本游标保留供下一步接续。第 5 步实现与审查完成，等待 owner 检查；第 6 步主色与画幅尚未开始，缩略图性能 issue 仍延期。

收尾核对：reflog 显示当前工作分支已同步包含 Agent Markdown 改动的 develop 基线；本任务未执行 rebase 或创建提交。所有本步实现/测试文件仍与 R3 冻结 manifest 一致。

### owner 反馈后的定向修复（2026-10-12）

四项反馈均已处理，本轮未提交：

- 清空回收站的未知状态被拖放导航带走：在真实浏览器先复现遮罩 drop 使 view 从 trash 变为 all。遮罩不是 `role=alertdialog` 的后代，因此不只判断事件目标 `closest()`；浏览区的 dragover/drop 在事件发生时检查是否存在模态框，拒绝导入并阻止浏览器默认打开文件；文件导入入口也作同样检查，覆盖延迟返回的文件选择器。回归逐一覆盖遮罩、对话框、底层浏览区、文件选择器；未知请求保持可重试，重试 key 与原请求相同。
- 导入表单不再读取全局 toast 作为错误；只在自己的 create 失败时保存 `draft.error`，重试时清除本次错误。后台上传失败仍显示其 toast/上传列表，不污染添加链接表单；创建自己的拒绝和未知重试回归保持通过。
- 从最爱、未归类或回收站主动导入时切到全部；当前分组与最近添加视图保持。新增素材可见，后台上传完成仍只刷新、不导航。
- 删除 AssetInspector 操作区多余的 JSX 花括号。

失败证据：`/tmp/codex-step5-followup-red.log` 三组功能问题均红；`/tmp/codex-step5-followup-drop-red.log` 专门确认遮罩 drop 导致确认框/未知身份丢失的路径。修复后 `/tmp/codex-step5-followup-green.log` 三组全绿，`/tmp/codex-step5-followup-related.log` 第 5 步相关 13 组浏览器回归全部通过。`/tmp/codex-step5-followup-frontend.log` 前端 lint/build 与 469 项测试通过，`/tmp/codex-step5-followup-generate.log` 无契约漂移，`git diff --check` 通过。

本轮只修正前端输入守卫、错误展示和导入入口的视图选择，没有改服务端删除路径、请求幂等、维护调度或数据持久化语义。此前 R3 审查结论对应其冻结版本；本轮按 owner 提供的发现做先红后绿的定向修复，未擅自增加第 4 轮独立审查。约束已进入可执行回归，无新增 lesson。

提交授权：owner 于本轮明确说“ok，你提交吧”，授权提交第 5 步完整实现、验收记录及反馈修复。本记录随本次提交保存。
