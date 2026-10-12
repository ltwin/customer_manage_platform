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
