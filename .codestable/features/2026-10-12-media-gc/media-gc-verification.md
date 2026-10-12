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

`TestGCKeepsWhatIsStillInUse` 覆盖 AC-03 的「仍被使用不回收」：画布节点持有的修订，以及有未过期读 pin 的 blob，都不被标记。去掉 blob 持有条件里的读 pin 一项后，该测试按预期失败。

## 未满足

- AC-03 的并发部分（GC 标记与新增引用并发只有一方成功）：靠锁序保证（账号写屏障 → 行锁重查；读取锁 blob 行并要求 ready），尚缺并发测试。
- AC-04：删除失败与结果未知保持 deleting 的路径已实现，缺故障注入测试；OSS 只删当前对象（`DeleteCurrent` 写 delete marker）已实现，恢复演练需要真实 OSS bucket。
- AC-07：资产库确认文案与「待释放」展示未开始。

## 门禁

`golangci-lint`（creative-worker、creativemedia、creativereference、versionedfs、store）0 issues；creativemedia、creativereference、versionedfs、store、creativegeneration、creativelibrary、creativecontent 测试通过（2026-10-12）。
