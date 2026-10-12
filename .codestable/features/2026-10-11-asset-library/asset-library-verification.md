---
doc_type: feature-verification
feature: 2026-10-11-asset-library
requirement: V6-066、V6-067、V6-068、V6-069（资产库第 2 期），并入 V6-016、V6-018、V6-060，V6-020 随 067.3 验收
status: in-progress
date: 2026-10-12
---

# 资产库第 2 期验证记录

设计见 [asset-library-design.md](asset-library-design.md)。按设计 §9 分六步提交，每步完成后在这里补一节。

## 第 1 步：后端与契约（2026-10-12）

### 实现

- **迁移 `0061_creative_asset_library`**：
  - `creative_assets.rating_halves`（1–10，空值为未评分）和按评分排序的部分索引。
  - `creative_blob_palettes`：建表，第 6 步才写入。
  - `creative_asset_group_deletions`：删除分组的快照。
  - 回滚时有评分就拒绝；主色和快照随回滚丢弃。
- **列表**（`creativelibrary/list.go`）：
  - 新增 `view=recent`、`min_rating`、`unrated`、`sort=rating`，游标带上评分。
  - 条件拼装抽成 `searchFilter.condition`。列表、`total_count` 和 `include_facets` 的计数都用它生成条件，并在同一个 `WithReadSnapshot` 里执行。类型计数不带 `kind`；标签计数在 all 模式下带上已选标签，any 模式下不带。
  - 关键词正文命中用的授权条件抽成 `readableContent`，并补上了原来缺的「依赖授权失效」判断，和 `ReadDisplayBatch` 一致。
- **新增命令**：
  - `BatchRate`：批量评分，整批要么全部写入，要么一项都不写。
  - `EmptyTrash`：核对 `library_revision` 后清空回收站，和 `batch-purge` 共用 `purgeAsset`。
  - `RestoreGroup`：撤销删除分组。分组删除时会写快照，并返回 `deletion_id`。
  - 三者都走 `libraryCommand`，即把 `libraryRun` 泛化成可以返回任意响应体的版本，仍然先锁资料库根。
- **分组计数**：`ListGroups` 返回 `asset_counts`。子组只数自己的成员；顶层分组把子组算进去，同一份素材只算一次；回收站里的不算。
- **用量**：新增 `GET /creative/library-usage`，读 `creative_media_quotas`。账号还没有配额记录时，返回 0 和配置里的上限。
- **store**：
  - 新增 `GroupCount`（按一列分组计数，列名必须是标识符字面量）。
  - `OrderBy` 加了 `NullsLast`。
- **契约**：`api/openapi.yaml` 新增 4 个接口，并改了列表参数、`CreativeLibraryAsset.rating`、`CreativeAssetPage.facets`、`CreativeGroupPage.asset_counts` 和删除分组的返回值；已执行 `make generate`。
- **ADR-008 关联清单**：`docs/product/creative-canvas-system/data-model.md` 登记了 `creative_blob_palettes`（派生数据，不是保留根）和 `creative_asset_group_deletions`（历史来源 key），以及 `creative_assets.rating_halves`。

### 实现偏差

- **状态码**：
  - 评分回收站里的素材返回 409 `creative_asset_trashed`，和改标题等编辑一致；设计稿原写 422。
  - 带 `cursor` 时传 `include_facets` 返回 400 `validation_failed`，和其他参数错误一致；设计稿原写 422。
  - 设计 §2.3、§2.4、§5 已同步。
- **回收站到期清理任务不存在**：设计 §1 原写「到期由后台清理」，核对代码后发现没有这个任务，`purge_after` 只写不用。这是 V6-020 AC-02 的缺口。设计 §1、§3.1、§3.7、§5、§6、§9 已改为由 067.3 新增 `library.trash_expiry`，删除快照的到期清理也一并放到这个任务里。在那之前，超过 30 天的快照在恢复时按不存在处理。
- **一致性扫描**：仓库里目前没有任何关系的只读扫描器。本步只登记关联清单，并用事务守卫和测试覆盖（跨账号、悬空 id 跳过、结构变化拒绝）。扫描器等第一个需要接入破坏性 GC 的关系时一起建。主色表不参与 GC，所以不受影响。
- **删除快照多存了分组的 `revision`**：恢复后分组 revision 加一，避免旧标签页拿着删除前的 revision 写入。

### 门禁

- `go build ./...` 通过。`golangci-lint run`（creativelibrary、httpapi、creativemedia、store）结果为 0 issues。
- `go test`：
  - `./internal/creativelibrary/`、`./internal/platform/store/`（包括所有迁移回滚链，91 秒）、`./internal/platform/httpapi/`、`./internal/creativemedia/`、`./internal/creativecanvas/`、`./internal/creativegeneration/` 全部通过。
  - 数据库测试使用真实 PostgreSQL（storetest 容器）。
- `make check-frontend` 通过（exit 0）。生成的 `schema.d.ts` 新增了必填的 `rating` 和 `asset_counts`，现有前端代码不受影响。

新增测试：

| 测试 | 覆盖 |
|---|---|
| `TestBatchRateValidatesAndWritesAllOrNothing` | 0、5.5、0.3、-1 被拒；漏传 `rating` 被拒；一项 revision 不对，整批都不写；各项分别写入；幂等回放；null 清除；跨账号返回 404；回收站里的素材返回 `ErrTrashed` |
| `TestRatingFiltersSortAndPagesWithoutGaps` | 按评分排序，未评分的排最后，同分按时间倒序；每页 3 项，跨三页逐条比对；`min_rating`、`unrated`，以及非法组合 |
| `TestRecentViewIsTheLastSevenDays` | 7 天前减 1 分钟不出现、加 1 分钟出现；回收站里的不出现 |
| `TestFacetsCountKindsAndTagsLikeTheList` | 类型计数忽略 `kind`；标签在 all 和 any 两种模式下分别正确，并和 `total_count` 交叉核对；数量为 0 的标签照常列出；其他账号的标签不计；带游标时被拒；不请求时不返回 |
| `TestFacetsShareTheListSnapshot` | 6 个协程不停地移入、恢复回收站，同时读 40 次：类型计数之和、所选标签计数、items 数都等于 `total_count` |
| `TestReadableConditionMatchesTheDisplayGuard` | 正常、展示授权已撤销、依赖授权失效、来源声明不允许展示、修订删除中、其他账号，共 6 个样例，`readableContent` 和 `ReadDisplayBatch` 的结论逐个一致；关键词正文命中也遵守这个条件 |
| `TestEmptyTrashPurgesExactlyTheConfirmedBatch` | 读取版本号之后，又移入一项或恢复一项，都返回 409，一项都不删；版本号一致时删除 2 项，`library_revision` 加一；幂等回放；画布节点引用的内容仍然可读（V6-020 AC-03 的后端部分）；回收站为空时不推进版本号 |
| `TestGroupDeletionRestoresTreeAndMembers` | 恢复后整棵树（id、父级、位置、名称）与删除前逐项一致；期间新加的归类保留（取并集）；其他账号返回 404；幂等回放；换一个幂等键再恢复返回 409；删除后结构有变化时返回 409；分组计数包含子组且去重，回收站里的不算 |
| `TestGroupRestoreRefusesToDropMembersAtTheGroupLimit` | 删除分组后，原成员被归入另外 100 个分组：撤销返回 `ErrGroupRestoreLimit`，分组列表、结构版本号、素材 revision 都不变；移出一个分组后，同一个快照撤销成功，成员关系恢复 |
| `TestUsageReportsCountAndQuota` | 回收站里的不计入素材数；没有配额记录时用默认上限，有记录时读记录 |
| `TestNameSortIsCodePointOrderAcrossPages` | 「10、2、a、b、Ｂ2、张三、阿」按码位排序，含全角字符和首尾空白；每页 2 项，跨页稳定 |
| `TestAssetLibraryMigrationRefusesToDropRatings` / `…RollsBackWithoutRatings` | 旧版本应用照常写入；评分越界被拒；有评分时拒绝回滚；没有评分时可以回滚，素材保留，并且可以重新迁移 |
| `TestAccountScopeGroupCount` | 按列分组计数；带条件；账号隔离；拒绝非标识符列名 |
| `TestCreativeLibraryHTTPRatingFacetsUndoAndTrash` | 经路由走完：带 facets 的首屏、带游标时 400、参数冲突时 400（`min_rating=0`、`min_rating=0&unrated=true`、`min_rating=0.3`、`unrated` 与 `min_rating` 同时出现）、只传 `unrated` 时正常、批量评分、按评分筛选、分组计数、删除后撤销、二次撤销 409、清空回收站时版本号过期返回 409、清空成功、用量 |

故意改坏代码后，测试都能发现，改完已恢复：

| 改坏的地方 | 失败的测试 |
|---|---|
| 去掉 `readableContent` 的依赖授权条件 | `TestReadableConditionMatchesTheDisplayGuard` |
| 清空回收站不核对版本号 | `TestEmptyTrashPurgesExactlyTheConfirmedBatch` |
| 恢复分组不核对 `hierarchy_revision` | `TestGroupDeletionRestoresTreeAndMembers` |
| 类型计数带上 `kind` 条件 | `TestFacetsCountKindsAndTagsLikeTheList` |
| any 模式的标签计数也带上已选标签 | `TestFacetsCountKindsAndTagsLikeTheList` |
| 按评分排序时去掉 `NULLS LAST` | `TestRatingFiltersSortAndPagesWithoutGaps` |
| facets 改在另一个读事务里查 | `TestFacetsShareTheListSnapshot`，连跑 3 次都失败；改回后连跑 3 次都通过 |

### 代码审计修复（2026-10-12）

- **[P2] 分组撤销会静默漏掉成员，同时把撤销机会用掉**：原来的实现会跳过已满 100 个分组的素材，照样把快照标为已恢复。现在恢复前先检查容量，有素材已满就整体返回 409 `creative_group_restore_limit`，事务回滚，快照仍可再次撤销。证据：`TestGroupRestoreRefusesToDropMembersAtTheGroupLimit`；去掉容量检查后这条测试失败。
- **[P3] `min_rating=0` 被当作没传，绕过了参数校验**：`Search.MinRating` 改成指针，只要传了就校验。`min_rating=0`，以及它和 `unrated` 同时出现，现在都返回 400。证据：领域测试的非法组合，加上 HTTP 测试的 4 种请求。

### 验收对应（本步能关闭的部分）

| AC | 结论 |
|---|---|
| V6-068 AC-05 | 满足：评分写入核对 revision，冲突时整批 409；跨账号返回 404（`TestBatchRateValidatesAndWritesAllOrNothing`） |
| V6-018 AC-04 | 满足：同一幂等键回放原结果；换一个键再恢复返回 409，不会重复建组，成员关系用条件插入（`TestGroupDeletionRestoresTreeAndMembers`） |
| V6-016 AC-02 | 满足：规则写进了契约（`sort` 参数说明）和设计 §2.1；混合名称跨页排序（`TestNameSortIsCodePointOrderAcrossPages`） |
| 其余 | 依赖界面，随后续各步验收。V6-068 AC-04 的服务端筛选、排序和跨页正确已有测试，界面部分随 067.1 / 067.2 验收；V6-018 AC-03 的服务端冲突和保留新归类已有测试，界面提示随 067.2 验收 |

## 第 2 步：067.1 页面骨架与浏览（2026-10-12）

### 实现

- **路由**：`/creative/library` 改为懒加载 `assets/AssetsPage.tsx`，不再渲染 `WorkspacePage`，页面不加载画布。侧栏用 `home/AppSidebar`。
- **布局**（`assets/assets.css`）：
  - 宽度大于 1100px 时四栏：侧栏、资料库树 248px、浏览区、检查器 300px。
  - ≤1100px 收起检查器和它的开关。
  - ≤760px 只留浏览区，侧栏变顶栏，资料库树收进「视图」下拉。
  - 浏览区用容器查询：宽度 ≤640px 时收起缩放滑杆，搜索框换到下一行。
- **资料库树**（`assets/LibraryTree.tsx`）：
  - 固定视图：全部、未归类、最爱、最近添加、回收站。
  - 两层分组，可折叠，计数取 `asset_counts`。
  - 标签按分类列出，未分类排最后，空分类不显示。数量取当前条件的 `facets`，点一下加入或移出筛选。
  - 底部用量「N 项 · 已用 X / 上限」加进度条。
- **浏览区**（`assets/AssetBrowser.tsx`）：
  - 标题和面包屑，父组可点。
  - 搜索：防抖 300ms，输入法组字时不提交，按 `/` 聚焦。
  - 排序：最近添加、最早添加、名称、评分。
  - 类型分段带数量，数量为 0 的不显示。
  - 评分筛选：5 星、4.5 / 4 / 3 星以上、未评分。
  - 标签弹层。
  - 条件 pill：评分、各标签、「同时满足 / 满足任一」（两个及以上标签时出现，点击切换）、关键词、清除全部。
  - 子分组 chip。
  - 瀑布流 / 方格切换和缩略图大小记在 `creative-assets:prefs`，检查器开关也记在这里，读写都包了 try/catch。
  - 滚到底部附近按游标加载下一页；翻页时 `library_revision` 变了就提示刷新。
- **条件与请求**（`assets/query.ts`、`assets/useLibrary.ts`）：
  - 网址参数：`view`、`group`、`tags`、`mode`、`kind`、`q`、`rating`、`sort`，与抽屉的 `l*` 参数互不干扰。
  - 换视图、进分组时写入历史，可以后退；改筛选只替换当前记录。换视图时清空筛选，保留排序。
  - 首屏带 `include_facets=true`，翻页只带 `cursor`。
  - 条件变化时用请求序号加 `AbortController` 丢弃过期响应；新响应到达前，计数沿用上一份，不会闪一下。
  - 切回标签页时重读。
- **卡片**（V6-060，`editor/LibraryTiles.tsx` 的 `TileFace` 加 `editor/cards.css`，抽屉、悬停放大和资产库页共用）：
  - 图片：画面。
  - 视频：`preload="metadata"` 的首帧，加时长角标。
  - 音频：由素材 id 决定的 36 根装饰条，加播放标记和时长。
  - 链接：标题和域名，不抓取网页。
  - 文字：标题和正文前 7 行。
  - 资产库页卡片下方只放标题（仅图片、视频）和只读星级。星级在 `editor/StarRating.tsx`，白色，半星用裁切。
- **标签计数（V6-016）**：
  - `TagPopover` 移到 `editor/TagPopover.tsx`，两边共用，每个标签带数量。
  - 抽屉在弹层打开时按当前条件发 `limit=1&include_facets=true`，条件变了就重读，只认与当前条件对应的那次响应。
  - 「同时满足 / 满足任一」只在选中两个及以上标签时出现。
  - 抽屉搜索框的占位文字改为「搜索标题、备注、正文、网址、标签」，与实际搜索范围一致。
- **检查器**（`assets/AssetInspector.tsx`）：本步只读。单击卡片选中，显示预览、标题、星级、备注、分组、标签和属性；点空白处取消选中。

### 实现偏差

- **多了两个纯逻辑和数据文件**：`assets/query.ts`（条件、网址、请求路径、本机偏好、分组和标签的排列）和 `assets/useLibrary.ts`（目录和列表的读取）。设计 §4.1 没有列出这两个文件。拆出来是为了单测，也避免浏览区组件过长。
- **检查器本步只读**：编辑、多选、操作按钮按设计随 067.2 补上；本步只做单选。
- **缩放滑杆的收起阈值从原型的 820px 改为 640px**：1440 宽、侧栏展开、检查器打开时，浏览区约 676px。按原型阈值滑杆会被收起，实际上工具条约 580px 就放得下。
- **手机上瀑布流至少两列，方格固定两列**：手机上没有缩放滑杆，按 220px 算只能排一列，卡片会占满一屏。
- **抽屉小方格里的音频卡片隐藏标题和星级**：约 100px 的方格放不下，标题在悬停放大里看。
- **回收站视图本步只列出素材**：剩余天数、顶部提示和「清空回收站」随 067.3。
- **两份连真实后端的旧 e2e 脚本受影响**：
  - 受影响的步骤：`creative-media.e2e.mjs` 的第 1 步（资产库导入），以及 `creative-text-canvas.e2e.mjs` 里手机新建资产、FND-03 独立整理和资产库规模采样三段。这些步骤都在 `/creative/library` 上用画布抽屉的独立模式。
  - 改路由后，这些步骤在新页面上找不到对应控件；`WorkspacePage` 的独立模式分支（`standalone={!canvasID}`）也不会再被访问到。
  - 这两份脚本需要真实账号，本地没有跑。
  - 计划：067.3 把导入、新建搬到新页面时，一并把这些步骤改到新页面或画布抽屉，并删掉独立模式分支。

### 门禁

- `make check-frontend` 通过：oxlint 没有新增警告，build 成功，456 条前端单测全部通过。
- 新文件按 `prettier --single-quote --no-semi` 排版。HEAD 时就不干净的 `App.tsx`、`LibraryPanel.tsx`、`LibraryTiles.tsx`、`libraryState.ts`、`library.css` 和 e2e 脚本是手工改的。

新增测试：

| 测试 | 覆盖 |
|---|---|
| `scripts/creative-assets.test.ts`（8 条） | 网址读写：非法值回落为缺省，进分组时视图固定为全部，往返一致，只有一个标签时不写 `mode`；换视图清空筛选、保留排序；首屏带 `include_facets`，翻页只带 `cursor`，「未评分」发 `unrated`；本机偏好遇到坏数据和存储被禁用时不出错，大小钳到 140–360；分组两层排列；标签按分类，未分类排最后，空分类不显示；时长、容量、域名格式化；音频装饰条由 id 决定 |
| `scripts/creative-assets.e2e.mjs` AC-01 | 不加载画布，也不请求 canvases；1440：四栏、检查器开关记在本机且刷新后保持、标题和排序不被截断、滑杆可用；1000：检查器和开关隐藏；390：树隐藏、侧栏变顶栏、「视图」下拉可以进分组、瀑布流两列；三档都没有横向滚动 |
| 同上 AC-02 | 视图顺序；分组计数「婚礼 5（含子组且去重）/ 外景 3 / 棚拍 2 / 品牌 2」；折叠和展开；最近添加只有 7 天内的，并按时间倒序；标签分类顺序；点标签加入筛选；all 模式下的数量是「再勾上它还剩几项」；用量文字和进度条 |
| 同上 AC-03 | 类型分段数量，进棚拍后只剩图片和音频；首屏请求都带 `include_facets`；面包屑、子分组 chip、包含子组、后退；关键词命中备注、正文、网址、标签；`/` 聚焦；移除关键词 pill；标签选一个时没有模式切换，选两个后出现，切到「满足任一」后结果和数量都变化；评分筛选（`min_rating`、`unrated`）；按评分排序；换视图清空筛选、保留排序；瀑布流两列、滑杆、刷新后保持；方格是正方形 |
| 同上 V6-016-AC-03 | 先点视频（响应被拖住 1.2s），再点文字：最终结果是文字；空结果「在全部里查找」保留关键词、扩大到全部；只有「清除条件」时也能清除 |
| 同上 paging | 70 项先显示 60 项，滚到底部取第二页（`cursor=60`，不带 `include_facets`），不重复；翻页时版本号变化时提示刷新，刷新后恢复；切回标签页时重读 |
| 同上 AC-09 | 图片卡片下方是标题和白色星级（4.5 星半颗）；视频首帧和 0:12；音频 36 根装饰条和 3:04，没有通用图标，未评分的没有星级；文字显示正文，下方只有星级；链接显示 example.com，没有对外请求；单击选中，检查器显示尺寸和分组；点空白处取消 |
| `scripts/creative-library.e2e.mjs`（抽屉，新增断言） | 未归类里的音频卡片和链接卡片；悬停放大用同一形态；标签弹层的数量来自 `include_facets` 请求；只选一个标签时没有模式切换 |

回归：`creative-library.e2e.mjs` 和 `creative-library-baseline.e2e.mjs`（AC-01–09 全部通过）。截图在 `/tmp/creative-assets-qa/`（ac01-desktop / medium / phone、ac02-tree、ac03-grid / masonry、ac09-cards / inspector、v6016-empty）和 `/tmp/creative-library-cards.png`。

### 待 owner 走查

- 音频卡片的装饰条（§8 D-2）：认可后 V6-060 AC-02 才能勾选。
- 三档宽度的整体观感。

### 验收对应（本步）

| AC | 结论 |
|---|---|
| V6-067 AC-01 | 满足：e2e AC-01 |
| V6-067 AC-02 | 满足：e2e AC-02 |
| V6-067 AC-03 | 满足：e2e AC-03。关键词覆盖范围由服务端搜索投影决定，本步没有改动 |
| V6-067 AC-09 | 满足：e2e AC-09 |
| V6-016 AC-01 | 满足：资产库页的树和标签弹层，以及抽屉标签弹层，数量都随当前条件变化（e2e AC-02、AC-03 和抽屉 e2e）；服务端计数口径见第 1 步 |
| V6-016 AC-03 | 满足：e2e V6-016-AC-03 |
| V6-016 AC-05 | 满足：两边都用 pill 或 chip 列出已选标签并可以移除，两个及以上才出现模式切换，占位文字写明搜索范围；抽屉「全部」里有关键词或标签时平铺改为结果网格，沿用原有行为 |
| V6-060 AC-01 | 满足：类型分段筛选结果与类型一致（e2e AC-03、V6-016-AC-03） |
| V6-068 AC-04 | 满足：筛选项和按评分排序（e2e AC-03）；服务端筛选、排序和跨页正确见第 1 步 |
| 未勾选 | V6-060 AC-02 待走查；V6-060 AC-03 的快速查看随 V6-066 浮窗；V6-016 AC-04 的新页面即时建标签随 067.2；V6-068 AC-03 的评分交互随 067.2 |
