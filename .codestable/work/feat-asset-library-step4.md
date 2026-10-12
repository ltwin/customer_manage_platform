---
feature: asset-library-step4
status: verified
date: 2026-10-12
commit_authorized: true
---

# 资产库第 4 步：共用查看浮窗

## 目标

在第 3 步提交 `9fa4ec4` 上继续第 4 步，实现已批准的 V6-066 共用查看浮窗，补齐 V6-068 浮窗评分。owner 明确授权开始，并要求交互、样式与设计原型对齐。

## 现场

- 工作区 `.worktrees/creative-workspace-redesign`，分支 `feat/creative-workspace-redesign`，沿用当前会话已选工作区。
- 设计：[资产库设计 §4.4 / §9](../features/2026-10-11-asset-library/asset-library-design.md)。原型：`docs/prototypes/creative-workspace/v6/viewer.mjs`、`viewer.css`、`assets.mjs`。
- 代码图 ready，但 generation 仍为 `2026-09-28T12:10:12Z`。候选及依赖 coverage 显示陈旧或未跟踪，采用真实源码、生成类型和浏览器运行结果验证。

## 边界

- `MediaViewer` 负责通用媒体外框、导航、播放和焦点；`AssetViewer` 负责读取当前素材的完整详情，`AssetInfoPanel` 负责资产信息。
- 独立页单击仍选择，双击、空格、Enter、悬停放大按钮打开；抽屉接入同一浮窗，保留单击快看、Enter 放入和任务式选素材等既有取用交互。
- 图片按比例完整显示，背景使用同一内容的模糊填充；1×–8× 指针中心缩放、拖动与触控板平移、边界限制、还原与翻项复位。视频不缩放，视频和音频使用设计规定的原生控件，替代原型里的模拟播放。
- 标题、备注、评分、分组、标签、放入项目与下载复用从第 3 步提取的 `useOrganization`。浮窗可移到回收站并直接恢复，未知结果保持原请求重试；输入失焦触发保存时，关闭不能丢弃恢复入口。
- “生成信息”随 V6-071；生成记录接入随 V6-070；画布节点及助手结果接入仍由各自条目负责。第 5 步导入与完整回收站管理、第 6 步颜色画幅不在本次范围。
- 本步无后端、迁移、API 契约变更；owner 于 2026-10-12 明确授权提交当前代码。

## 证据

| 验证 | 结果 |
|---|---|
| `make check-frontend` | lint / build 通过，463 项前端测试通过 |
| `make generate-check` | 通过，生成物无漂移 |
| `creative-assets.e2e.mjs` | 23 组通过；包含前 3 步的 16 组与本步 7 组，无页面错误 |
| `creative-library.e2e.mjs` | 抽屉共用查看与原有取用、分组撤销回归通过 |
| `creative-library-baseline.e2e.mjs` | AC-01～09（含 AC-05b）通过 |
| `creative-viewer-zoom.test.ts` | 先红后绿；指针锚点、倍率与平移限制、鼠标/触控板区分 |
| 编辑后点遮罩 | 新增测试先红后绿；从 mousedown 关闭改为 click，保存未知时仍可在浮窗重试同一请求 |
| 原型对照 | 在真实浏览器同时打开原型与产品，外框、信息栏、内容区、标题、底栏、翻页按钮和信息行的尺寸、内边距、间隔、字体、边框与颜色逐项一致 |
| `git diff --check` | 通过 |

七组浮窗浏览器验收覆盖：

1. 四种打开方式、关闭回到卡片焦点，单击不打开。
2. 竖图完整显示、缩放倍率/还原/切换复位、平移、模糊媒体背景；340px 信息栏、22px 星级，界面不使用玻璃模糊。
3. 标题和备注编辑、半星和数字键评分、标签与分组操作、弹层位于浮窗内、Escape 只关顶层、Tab 循环；409 回滚、未知响应同身份重试。
4. 浮窗内新建项目并放入、原件 download ticket、移到回收站与直接恢复。
5. 视频/音频真实原生播放和空格暂停、完整文字与链接、390px 手机布局无横向溢出。
6. 编辑后点遮罩关闭时保留未决请求与恢复入口。
7. 快速翻项时旧详情不覆盖新项；详情失败可重试；摘要内容补读完整修订。

所有浏览器测试连接工作区 Vite `http://localhost:5173`，使用 mock API；视频和音频使用仓库真实测试媒体文件。数据库事务与幂等规则沿用前序后端证据，本步不把 mock 称作数据库集成验收。

本机日志：`/tmp/codex-step4-r2-check-final.log`、`/tmp/codex-step4-generate.log`、`/tmp/codex-step4-r2-assets-final.log`、`/tmp/codex-step4-r2-drawer-final.log`、`/tmp/codex-step4-baseline.log`、`/tmp/codex-step4-prototype-final.log`。红灯证据：`/tmp/codex-step4-zoom-red.log`、`/tmp/codex-step4-backdrop-red.log`。截图：`/tmp/creative-assets-qa/step4-viewer-desktop.png`、`step4-viewer-phone.png`、`step4-prototype-desktop.png`。

独立 change review：native collaboration `/root/asset_step4_review`，`gpt-6-astra` / high；当前会话无可用异构 reviewer 创建入口，Claude 原会话已限额，本机没有 Paseo provider 偏好，回退当前可用最强稳定模型。审查阶段冻结工作树。

- R1 完整 patch SHA-256 `e91da1fb092b41f4ab5cabc99b883e62d08f0bb745e589bbfc98a7fd4acb0d87`，15 个文件的 manifest。发现两项 important：原生媒体未被焦点循环枚举，聚焦星级时数字评分不生效。为原生媒体添加 tabIndex，星级自身处理数字键；浏览器回归先红后绿。

- 补验发现回收/恢复导致卡片 DOM 重建后焦点失联；独立页与抽屉都改为等待列表刷新后，按素材 ID 找当前卡片。原项不再出现时回到首张卡片或浏览区。`/tmp/codex-step4-r2-red.log`、`/tmp/codex-step4-r2-drawer-focus.log` 记录修复前失败，最终日志全绿。
- R2 同一代理复审完整候选与增量，patch SHA-256 `b27a318171474a09e4df2f7bd92d79a44b6566277be81ad0d9f9cd22c26332d9`，16 文件 manifest 一致。两项审查 finding 及补验的回焦点问题均 resolved，无 unresolved / new findings，终态通过。审查代理另挂起恢复后的列表响应，在浮窗关闭后释放请求，确认焦点回到原素材。

## 验收

- V6-066 AC-01～04 / 06 / 07 勾选。AC-05 现有资产信息编辑已通过，但其中“生成信息”按原依赖留到 V6-071，整条 AC-05 保持未勾选，需求总项仍为实现中；生成记录宿主接入随 V6-070。第 4 步已批准范围完成。
- V6-068 AC-03：浮窗白色半星交互完成；与前序已完成的检查器、批量栏、右键菜单及只读卡片合并，本条完成。

## 状态与未决

- 第 4 步已实现并通过验证，owner 已授权提交；本次提交包含实现、验收记录与延期性能 issue。
- 图片缩略图与原图按需加载已记录为 [性能优化 issue](issue-asset-image-renditions.md)，owner 决定先完成功能，再处理此项；本步不实施图片管线改动。
- Safari `gesturestart` / `gesturechange` 已实现；本机自动浏览器证据为 Chromium，未冒充真实 Safari 触控板设备验收。
- 独立页及浮窗当前沿用第 3 步页面生命周期内的未知操作恢复与 beforeunload 提示，未扩展为跨刷新持久化队列；画布原有 SaveQueue 保留。
- 依赖真实账号且包含旧资产独立页入口的媒体/文字 E2E 仍按既定计划在第 5 步迁移，本次未运行。
- 毕业清单：验收状态回写 canonical V6-066、V6-068 与需求总表；关闭与异步详情约束落实到回归，不新增 lesson。本游标保留供下一步接续，旧 feature 文档只读。
