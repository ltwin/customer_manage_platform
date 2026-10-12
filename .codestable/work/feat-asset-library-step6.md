---
type: feat
status: verified
commit_authorized: true
---

# 资产库第 6 步：主色与画幅

## 目标

完成已批准资产库设计 §3.4–3.6、§9 第 6 步及 V6-069。继续使用既有 worktree 与 `feat/creative-workspace-redesign`。本轮尚无提交授权；后续提交信息按 `type: 中文描述`。

## 现场

本轮基线 `dcf718ca839e1d0798adf4e371ae9efbe18060be`，含已有查看浮窗原图修复；该既有修复不在本轮 diff。0061 已建主色表，无需新迁移。上传和生成导入共用 Verify；主色归 `creativepalette`，持久化查询归 creativelibrary，后台计算/读取锁归 creativemedia。

图谱项目为 worktree 对应项目，generation `2026-09-28T12:10:12Z`，Tier 2。相关文件 metadata_changed/not_tracked，依赖直接源码；不能用旧图谱行号或缺失符号作否定结论。结构搜索 47/47 完整返回，覆盖检查和源码核对另见临时审查 packet。

## 边界

- 完成图片最多 5 个主色、历史原始图片回填、故障恢复与运维 rebuild、颜色与三档画幅筛选、检查器主色点击。
- 不新增表、不做视频主色，不扩展智能分组、生成记录或内容查重。缩略图 issue 的剩余优化仍在后续。
- 颜色预设按批准的 12 类，在原型色板中用棕色替换第二个灰色，布局/尺寸不变。方形阈值与颜色阈值按原批准设计。
- 对历史 ready 原始 blob 回填，不对 display/thumbnail 派生 blob 重复计算；JPEG 优先读已存在显示图；独立审查发现显示图会丢失透明度，PNG/WebP 改为读原图以保持入库与重建语义一致。滚动部署旧实例晚到的数据可在下一次启动或运维重建补齐。

## 证据

- `/tmp/codex-step6-final-targeted.log`：算法、照片、上传/生成、授权撤销与依赖授权、画幅、后台恢复/幂等/队列原子性、重建范围、删除跳过、显示图读取锁、短读分类、启动分页、CLI 参数验证通过。
- `/tmp/codex-step6-go2.log`：全后端 build/lint 通过；测试仅 media 任务时长登记守卫失败。修复 LeaseBudget 新任务登记后，媒体包全量复测通过（`/tmp/codex-step6-go3.log`）；该 make 多包参数随后被递归 make 误当 target，其他两包以显式 go test 补跑通过，不能称单次全门禁全绿。
- `/tmp/codex-step6-frontend2.log`：前端 lint/build 与 474 项单测通过。
- `/tmp/codex-step6-generate-check.log`：生成漂移检查通过。
- `/tmp/codex-step6-all-e2e.log`：38 组资产库完整浏览器回归通过。
- `/tmp/codex-step6-prototype.log`：新增两组浏览器回归通过，实际色块弹层/网格/色块/提示文字的计算尺寸、背景、间距、圆角与阴影和原型一致；主色条 26px 高、6px 圆角、6px 间距。
- `/tmp/creative-assets-qa/step6-{colors,inspector,phone,prototype-colors}.png`：浏览器截图。
- `/tmp/codex-step6-photos2.log` 与 `/tmp/codex-step6-photos2.png`：12 张原型照片视觉走查；主色计算两次合计约 1.3–4.0ms，纯色/灰阶/透明/五色单测通过，固定初始中心改善彩色场景被灰化的问题，保持批准匹配阈值。
- `/tmp/codex-step6-scale.log`：5001 张独立修订/blob、每类 20 次首屏含计数查询；画幅 p95 77ms、颜色 p95 134ms、组合 p95 201ms。本机单请求暖缓存采样，不作生产并发 SLO 承诺。

## 验收

AC-01～04 已通过对应验证，第二轮独立 change review 通过，已回写 canonical 状态。操作说明、回滚与验证入口已写 `docs/dev/creative-palette.md`。不操作真实账号的重建命令；使用真实 PG + local 对象存储验证命令服务。

## 状态与未决

owner 审计修复已完成，第 3 轮独立复核通过，完整后端门禁通过，无未决 finding。风险：持久化派生事实、读取锁与队列原子提交、内容授权筛选，因此保留独立审查，最多三轮。最终 reviewer 创建方式为 native collaboration，fresh gpt-6-astra/high；当前工具表无 Paseo/异构创建能力、已查偏好文件和 bundled CLI 均不存在，按 cs-feat 回退同构最强模型。审查目标冻结为完整 diff 加 untracked 文件 patch，审查期间不改工作树。提交由 owner 另行批准。

### Change review R1

Reviewer `/root/asset_step6_review`，native gpt-6-astra/high；完整目标 `/tmp/codex-step6-review-r1.patch`，SHA256 `c0c79948d6923fbba2947ce22bb7a24809e7273d09e6deb2c6a52d5bd7df764c`，33 文件。结论需修改：blocking 1 项，无其他 important/nit。

确认问题：历史回填读取 JPEG display 丢失透明度，全透明图会从 failed 变成黑色 ready。修复为仅 JPEG 原始媒体可选择 display，PNG/WebP 读取原始字节，沿用原始读取锁。真实 PG+local 先复现全透明/半透明/现有 WebP 经 JPEG 显示派生图回填后色板不一致（`/tmp/codex-step6-alpha-red.log`），再验证直接回填和运维 rebuild 与上传时的色板、失败状态完全一致，透明图不命中黑色筛选。原型与前端无变化。

R1 修复验证：`make check-go PKG=./internal/creativemedia/...` 全部通过，含 build、lint、媒体包完整集成测试；日志 `/tmp/codex-step6-alpha-green.log`。

### Change review R2 与收尾

完整目标 `/tmp/codex-step6-review-r2.patch`，SHA256 `abf38938687abbb53336b1bf6cc086ee31c0a0bebf2e2553066d3737f4b580ae`，同 33 文件，同 reviewer lineage。结论通过，可合：R1 resolved；unresolved 无；new findings 无。Reviewer 独立重跑原 overlay 复现及新增透明/半透明/WebP/JPEG 显示图回归全部通过，终态前再次核对 manifest 未变。

完成状态回写只涉及本游标、canonical V6-069 与总表，不改变已审代码。毕业清单：V6-069 AC-01～04 与需求总表维护验收状态；`docs/dev/creative-palette.md` 维护计算、授权、回填、重建及回滚说明；本次发现已进入可执行回归，不新增 lesson。保留游标供 owner 检查；尚未提交，提交需本轮独立授权。


### Owner 审计修复（2026-10-12）

本次授权仅修复，不提交；沿用本 feature 同一 reviewer，拟用剩余第 3 轮完整候选复核，不重置轮次。

- 缺对象：`OpenVersion` 的 `versionedfs.ErrNotFound` 归类为 `failed/object_missing`。55 张图片的真实 PG 回归中，首张缺失仍能提交其余首批 49 张，续批完成后总计 54 张 ready，重复执行与读取锁释放通过；超时、短读的整批重试逻辑保留。
- 视频画幅：采用 owner 提供的方案 2。当前素材/媒体事实保存的是编码尺寸，完整显示方向支持需要新旧媒体一致的探测及历史修正；本次画幅筛选只匹配图片，视频详情标为「编码尺寸」且不显示横/竖/方形标签。API 描述、mock 与 canonical AC 范围同步，[后续 issue](issue-asset-video-orientation.md) 保留完整方向修正验收。没有执行 iPhone 真机上传，也不宣称视频方向已修好。
- 颜色匹配：删除仅测试使用的 Go `Matches`，SQL 为唯一实现。`TestPalettePhotoMatchingUsesProductionSQL` 覆盖 12 张真实照片经上传校验、入库、搜索、总数和 facets 的路径；`TestPaletteSQLThresholdBoundaries` 覆盖色度分界、色相环绕、明度范围与最低占比。
- 同色 hex：检查器 key 使用序号+hex，真实浏览器捕获控制台告警的回归已由红变绿。`narrowed` 注释补齐画幅、颜色。

红色证据 `/tmp/codex-step6-owner-red.log`、`/tmp/codex-step6-owner-ui-red.log`；定向绿色证据 `/tmp/codex-step6-owner-green.log`（实际生产 SQL 与回填）和 `/tmp/codex-step6-owner-ui-green.log`（3 组桌面/手机/审计浏览器回归）。本次按影响面复测，不沿用旧「视频参与画幅」结论。

Owner 修复验证：前端 474 项测试、3 组定向浏览器回归与 generate-check 通过；全 Go build/lint 通过，全包测试正在运行。日志 `/tmp/codex-step6-owner-check-frontend.log`、`/tmp/codex-step6-owner-ui-green.log`、`/tmp/codex-step6-owner-generate-check.log`、`/tmp/codex-step6-owner-check-go.log`。


### Change review R3 与最终状态

同一 reviewer `/root/asset_step6_review`；完整目标 `/tmp/codex-step6-review-r3.patch`，SHA256 `4a997114f8373b93f140133905627f23995fd52884bdc9e4bef0d8f24c71c4a1`，36 文件。终态：审查通过，owner 所列项全部 resolved，无 blocking/important/nit；完整候选及 R1/R2 透明度修复未回退。收尾再次确认 manifest 全部一致。

审查结束后 `make check-go` 正常退出 0，build、全量 Go lint、全包测试本轮一次通过（`/tmp/codex-step6-owner-check-go.log`）。前端 474 项测试、3 组相关浏览器回归、generate-check 同样通过；本轮不把前一版 38 组全量浏览器验证冒充新的 39 组全量运行。

毕业去向：缺对象错误分类与唯一 SQL 匹配实现记录在 `docs/dev/creative-palette.md`；当前图片画幅范围回写 V6-069；视频完整显示方向与历史修正进入 `issue-asset-video-orientation.md`，明确为待办。未新增 lesson，未运行真实账号重建或真机视频验收，未提交代码。


### 提交授权

2026-10-12 owner 明确授权提交第 6 步及本轮审计修复，提交信息为 `feat: 完善资产主色提取与画幅筛选`。提交前再次核对 R3 manifest，除最终状态文档外，已审实现与测试未变化。
