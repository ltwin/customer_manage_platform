# Step 4 窄范围修复说明

## 失败的 exit signal / checks

- exit signal：CRUD、未来/历史矩阵、摘要、隔离与并发锁序集成测试通过。
- 第一次失败：测试套系夹具缺 `shoot_type/pricing_mode` 必填字段；已补合法最小值。
- 第二次失败：客户已 archived 且订单已 cancelled 后修改 slot 时间，服务返回 `customer_archived`，测试错误期待 `validation_failed`。
- owner 授权续跑后，新增双迁移 barrier 用例首次执行超时：主测试等待第二事务取得 `cus-move-middle` 行锁，第二事务则等待第一事务释放该行的外键键锁，形成测试通道与数据库锁互等；`-timeout=20s -v` 栈停在 `schedule_test.go` 的 `<-secondReady`。
- 同轮 lint 报告测试文件 3 项：observer `db.Close` 错误未检查，以及两个嵌入 `Slot` 选择器可简化。

## 根因判断

Design D6 明确未来档期引用 archived 客户返回 409 `customer_archived`。同一候选还存在 cancelled 状态时，先完成 customer→order 锁序并优先报告客户状态是合法 typed error；测试不应把它降级成泛化 validation。

双迁移超时来自 fixture 排队顺序，不是生产锁序：第一事务把订单从 source 改到 middle 时持有 middle 外键键锁，第二事务不能在第一事务提交前取得 middle 的 `FOR UPDATE`。修复后先让第二事务进入锁等待，再启动 schedule create，并用 `pg_stat_activity` 的阻塞数量证明两方均已排队；第一事务提交后等待第二事务真正取得 middle 锁，再证明 create 的自动重试阻塞于 middle，最后释放第二事务完成第二次迁移。

## 允许修改的范围

- 仅修改 `backend/internal/schedule/schedule_test.go` 的错误断言、测试 barrier 排队与 lint 问题。
- 不改变 repository、service、锁序、错误优先级、迁移或 API 契约。

## 修复后必须重跑

- `go test ./internal/schedule -run 'TestScheduleCRUDRangeSummaryIsolationAndIdempotency|TestScheduleCreateRetriesAfterCustomerMergeAndRejectsArchive' -count=1`
- `go test ./internal/schedule -run 'TestScheduleCRUDRangeSummaryIsolationAndIdempotency|TestScheduleCreateRetriesAfterCustomerMergeAndRejectsArchive|TestScheduleConcurrentCreatesKeepOneShootWithTypedConflict|TestScheduleUpdateRetriesAfterCustomerMerge|TestScheduleCreateReturnsCustomerChangedAfterTwoMoves' -count=1 -timeout=2m`
- `go test ./internal/platform/store/... ./internal/platform/idempotency/... ./internal/order/... ./internal/schedule/... -timeout=3m`
- `golangci-lint run ./internal/platform/store/... ./internal/platform/idempotency/... ./internal/order/... ./internal/schedule/...`
- 通过后再跑 Step 4 目标包回归与 lint。

## 最终结果

- 双迁移单用例：通过，`TestScheduleCreateReturnsCustomerChangedAfterTwoMoves` 约 2 秒完成。
- 五组 Step 4 定向用例：通过，约 6 秒完成。
- Step 4 相关包回归：全部通过。
- Step 4 相关包 lint：`0 issues`。
