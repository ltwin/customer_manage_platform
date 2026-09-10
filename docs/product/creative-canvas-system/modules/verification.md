# 公共契约、内容与媒体：模块设计验证记录

日期：2026-09-09。范围仅为本批设计、DDL 草案及其 SQL 锁协议，不代表真实领域服务、OSS、前端或完整迁移已实现。

## 已执行

使用仓库 `storetest.Main` / `storetest.NewURL(t)` 启动标准 `postgres:17-alpine` 隔离测试容器，沿用日志和监听端口联合等待。通过临时 `backend/internal/cmdesignprobe_*` 包执行 [schema_probe_test.go](schema_probe_test.go)，结束后自动移除临时包并终止测试容器；未连接开发/生产数据库。

复现：在仓库根执行 `python3 docs/product/creative-canvas-system/modules/run-schema-probe.py`。测试使用空模板库，仅执行本批 SQL，不假称与所有旧迁移已完成合并验证。

| 检查 | 结果 | 证据边界 |
|---|---|---|
| 13 表 DDL 在 PostgreSQL 17 可执行，外键数量为 0 | 通过 | 只确认结构与约束语法 |
| 同内容修订序号唯一、pending 候选必须有内容、配额超额拒绝、ready 上传不能保留 blob_id | 通过（已补齐其余合法字段及合法 ready 对照） | 是数据库单行/唯一约束，不替代跨表守卫 |
| 原始 SQL 插入悬空 content/rights key | 如设计允许 | 证明无外键的风险真实存在；正式业务入口必须另测拒绝 |
| 双事务竞争相同修订行锁 | 通过，第二事务得到 55P03 | 通过服务端 lock_timeout 验证互斥，不依赖客户端调度时序猜测 |
| 锁释放后 GC 重新查询候选保留根 | 通过 | 本探针仅包含候选根，不声称覆盖全部保留根 |
| 清理先标 deleting 后，后续读取看到不可绑定状态 | 通过 | 领域服务实际拒绝与对象清理尚待 CM-02/05 实现 |

新增 TestDurableSourceAndPublicationIdentity：会话经明确 key 重读来源声明，独立 202 受理与 200 发布回执同时保留，并能查询发布目标；只证明本模块记录，不冒充真实节点/资产绑定。

修复后的最终输出：TestSchemaConstraints PASS（0.09s）、TestReferenceLockProtocol PASS（0.62s）、TestDurableSourceAndPublicationIdentity PASS（0.17s），测试包总计约 3.8s。首轮 ready 测试同时遗漏交接字段，不能单独证明 Blob 清空约束；现以其他字段均合法的反例和对应合法行修正。探针源码经 gofmt 格式化，重复执行时包目录随机生成，属于正常隔离机制。

## 尚未执行

- 正式 OpenAPI 与两侧生成物、实际账号/用途/幂等服务入口。
- 各类素材的真实解码/探测、资源限制和浏览器播放矩阵；能力清单不得提前开启未验证格式。
- OSS 预签名、RAM/CORS、固定版本、故障恢复与 Local/OSS conformance。
- 读取票据和 read pin 续期、候选采用/到期及配额发布/释放的完整服务并发。
- 与现有迁移合集成、旧对象迁移、备份/恢复、新旧同 bucket 清理隔离。
- River 入队桥接、React Flow、Gateway/Harness 的适配验证（属于对应后续模块）。

设计审核结果与最终哈希由 Epic work 记录；本文件不替代完整功能验收。
