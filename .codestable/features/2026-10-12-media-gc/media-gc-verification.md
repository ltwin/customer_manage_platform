# V6-075 媒体 GC 验证记录

设计以 [媒体模块 §6](../../../docs/product/creative-canvas-system/modules/media.md#6-gc恢复和上线约束) 为准。本文件只记录逐条 AC 的证据。

## 已满足

| AC | 证据 | 验证方式 |
|---|---|---|
| AC-01 | `creativereference.TestEveryReferenceColumnIsClassified`：枚举全库 30 个指向修订或 blob 的 text 列，逐列登记持有与否；登记为「持有修订」的表必须出现在 `LiveOwnerCondition` | 真实 Postgres；删掉一条登记后测试按预期失败（063b4db） |
| AC-02 | `creativemedia.TestGCReleasesAPurgedAssetsSpaceExactlyOnce`：资产永久删除 → 修订置 deleted、blob 置 deleting（24 小时宽限、额度未动）→ 到期后对象删除、`stored_bytes` 归零 → 再跑一轮无任何变化 | 真实 Postgres + local 驱动 |
| AC-05 | 同上测试的 scan 段：统计出 1 个修订、全部 blob 与字节数，库内状态不变 | 同上 |
| AC-06 | `creativemedia.TestGCResetsAStaleGenerationImport`：新导入不重置；`updated_at` 早于 48 小时后被重置为 writing、预留额度退还、旧 blob 置 deleting；迟到的提交得到 `ErrEpoch` | 同上 |
| AC-08 | `creativeagent.TestAnUnsentApprovalThatLostItsHolderDoesNotStopTheDispatch` | 旧代码上失败（`creative_input_unavailable`），修正后通过（063b4db） |

## 部分满足

**AC-03**（仍被使用的内容不被回收）：

- `TestGCKeepsWhatIsStillInUse`：画布节点持有的修订、有未过期读 pin 的 blob，都不被标记。去掉 blob 持有条件里的读 pin 一项后，该测试按预期失败。
- `TestGCQuarantinesBrokenOwnerReferences`（owner 审计补充）：资产或节点指向损坏的内容时，scan 与 delete 都返回 `ErrCreativeTextReferences`、报告为空，修订、blob 与对象绑定都不变；另一个账号照常回收。这条防的是把「引用损坏」误判为「没有持有方」。检查在每步的账号写屏障下执行。
- 未满足：GC 标记与新增引用并发时只有一方成功，目前靠锁序保证（账号写屏障 → 行锁重查；读取锁 blob 行并要求 ready），尚缺并发测试。

**AC-04**（删除失败或结果未知时不提前释放额度）：

- `TestGCDeletionFailureRetainsQuotaUntilConfirmedRetry`（owner 审计补充）：用故障注入让 `DeleteCurrent` 返回 `ErrUnknownResult`，分「对象实际未删」和「对象已删但回执丢失」两种；blob 保持 deleting、额度不动；恢复后重试恰好释放一次，再跑一轮无变化。
- `TestGCFailedBatchYieldsToLaterDueObjects`（owner 审计补充）：100 个删除持续失败的 blob 在 I/O 前把 `delete_after` 推后一个周期，下一轮不再占满批次，后到期的对象照常删除并释放额度；故障恢复后这 100 个也被删除。
- 未满足：OSS 只删当前对象（`DeleteCurrent` 写 delete marker）已实现，按记录的 key 与 version 恢复的演练需要开了版本控制的真实 OSS bucket。

## 其他覆盖（owner 审计补充）

- `TestGCDoesNotReleaseUnbookedRenditions`：因额度不足失败的上传，原图与 display 都保持未计额度并置 deleting；GC 删除它们时不扣 `stored_bytes`。配套修正：上传发布在全部额度检查通过后才把 blob 记为已计额度（`worker.go`）。
- `TestGCMismatchedStorageDoesNotStarve`：存储驱动或 bucket 与当前配置不符的 blob 单独计入 `skipped_blobs`，不占批次；同账号可删的 blob 第一轮就被删除。
- `TestGCStillCollectsInactiveAccounts`：非 active 账号照常回收。

## 未满足

- AC-07：资产库确认文案与「待释放」展示未开始。

## 门禁

对提交 a20db0e 在独立 worktree 中验证（2026-10-12）：`go build ./...`、`go vet ./...` 通过；`golangci-lint`（creative-worker、creativemedia、creativereference、versionedfs、store）0 issues；creativemedia、creativereference、versionedfs、store、creativegeneration、creativelibrary、creativecontent、creativeagent、creativecanvas 测试通过。
