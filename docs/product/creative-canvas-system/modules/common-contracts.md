---
status: proposed
version: 0.1
created: 2026-09-09
---

# 公共契约：创意空间基础模块

本稿是[架构](../architecture.md)的模块设计承接，适用于新基础框架。旧先导、CRM 及已有 API 契约保持原样。这里只定义接口与实现约束，正式 OpenAPI 和迁移在开发切片中接入；不能把本文路径当作已经部署。

## 1. 基础类型与身份

| 类型 | 约定 |
|---|---|
| AccountScope | 只由认证/受信任务主体构造；业务请求不含 account_id，所有资源访问核对账号 |
| ResourceID | TEXT 类型前缀加 UUID；本批前缀 ccnt 内容、ccrv 修订、ccrd 声明、ccug 用途授权、ccup 上传、ccuc 候选、ccbl 媒体对象、ccpn 读取保留、cchd 修订保留；第二批增加 ccas 资产、ccag 组、cctg 标签、cctc 标签分类、ccpj 项目、cccv 画布、cwnode 节点、cwedge 边、cwinp 输入、ccch 变更。下划线连接；不是授权凭证 |
| OperationID | 请求体 operation_id，UUID；REST 的 Idempotency-Key 必须与之相同，缺失或不一致拒绝；内部命令使用同一操作身份 |
| RevisionToken | 正整数的十进制字符串，上限 BIGINT；不能在 JS 中转 Number。SQL 用 BIGINT，API 中如 "42" |
| SequenceCursor | 非负整数的十进制字符串，例如 SSE after_seq；与业务修订类型分开 |
| Instant | UTC RFC3339 时间戳；审计时间由服务端产生；摄影师设备时间不作为授权证据 |
| Optional | 创建/替换型字段缺省与显式 null 含义在各请求定义，不使用任意 PATCH。非法额外字段拒绝 |
| Reference | 资源 ID 加明确修订；历史来源字段以 snapshot 命名，不能用它获得实际读取权限 |

Change 明细的 base_revision 可用0表示新对象此前不存在，不得把0当作现有对象的有效并发版本。

资源 ID 由服务端产生；画布乐观预分配另遵循画布命令契约。本批 API 不接受客户端任意指定内容/Blob ID、对象 key/version、账号或 worker 身份。JSON 的尺寸/时长在本批上限内使用安全整数；字段超出允许范围即拒绝。

## 2. 路由与返回

新路由使用 `/api/v1/creative/` 命名空间，避免与旧 `/creative-workspaces/` 混用。路径中的 ID 均为该类型稳定 ID；前端只能通过 API 返回的访问描述读取媒体，不拼 OSS key。成功响应采用各用例的类型化对象，不另包装一层通用 data；失败沿用现有 ErrorEnvelope：

```json
{
  "error": {
    "code": "creative_revision_conflict",
    "message": "内容已变化，请确认后重试",
    "details": {
      "kind": "creative_conflict",
      "resource_id": "ccuc_00000000-0000-4000-8000-000000000001",
      "current_revision": "3",
      "reason": "target_changed"
    }
  }
}
```

ErrorDetails 作为既有 OpenAPI union 的新增分支，不能手写第二份生产 DTO。请求追踪沿用已有请求标识机制；日志通过 operation_id/upload_id 与任务关联，不打印签名 URL、密钥、完整正文。

| HTTP | code / 语义 | 调用方动作 |
|---|---|---|
| 400 | validation_failed：JSON、字段格式、header/body 不一致 | 修正输入，不自动重试 |
| 401 | unauthorized | 走现有认证恢复，不丢弃本地草稿 |
| 403 | forbidden / creative_usage_denied | 说明权限或用途原因；不换 ID 绕过 |
| 404 | not_found | 不存在或属于其他账号统一不可见，避免暴露对象是否存在 |
| 409 | idempotency_conflict / creative_revision_conflict / creative_state_conflict / creative_operation_expired | 保存当前草稿；读取现状，显式产生新的操作才能改变原请求 |
| 413 / 415 / 422 | creative_size_limit / creative_media_unsupported / validation_failed（语义不合法） | 按字段/文件显示失败；不把后台校验失败伪装成网络错误 |
| 429 | rate_limited / creative_quota_exceeded | 返回可重试时间或额度原因，不无界重试 |
| 503 | creative_dependency_unavailable / creative_commit_unknown | 保留同 body/key，优先查回执/资源状态；不换 key 或立即删除外部对象 |
| 500 | internal | 服务端追踪；同 key 查询/恢复，不报告已保存 |

归档写入统一在具体新接口返回 409 archived_read_only；403 留给认证后确实无权执行的用途。上述 code 是本批待加入 OpenAPI 的枚举，不修改旧接口错误语义。

## 3. 幂等与命令恢复

每次写请求携带 operation_id、client_created_at 和明确的领域前置条件。服务端规范化 payload 后计算 hash：包含动作类型、目标、字段、前置条件、client_created_at；排除认证令牌、追踪标识、签名上传地址等临时传输信息。规范化规则在 OpenAPI 示例与测试中固定。

异步请求的 202 表示受理事实，不会被 Worker 发布结果覆盖；上传会话的 create/complete 操作与服务器预分配 publish_operation_id 分开，各自回放首次结果，具体映射见[媒体模块](media.md#受理操作与后台发布分开)。

请求进入受信账号范围，先确认仍有权读取该操作结果，再检查回执；成功同 key 同 hash 先回放原结果，不因对象版本已增长而改成冲突。同 key 不同 hash 返回 idempotency_conflict。未提交的校验失败不保存成功回执；提交成功但响应丢失不能再次执行。并发同 key 通过事务级幂等锁与唯一约束串行，不能仅做事务外 Exists。

采用 creative_operation_receipts 作为新命令唯一重放权威，补足字段：operation_type、client_created_at、request_hash、http_status、response JSONB、result_kind、result_id?、result_revision?、created_at、retained_until。response 只保留有界状态/身份，不含媒体、签名 URL 或大正文；GET 当前资源用于取得最新状态或新上传签名。不同领域均使用该表，不扩旧 idempotency_records 白名单。

过期规则沿用数据模型总表：客户端始终保留原 client_created_at/body/key，超期离线操作不自动重放；服务端拒绝时间已超恢复窗口或不合理未来时间的无回执请求。该时间保护正常客户端恢复，不能充当安全授权或无限期去重承诺。未查到回执也不等于没有执行，COMMIT unknown 必须核实原操作与领域状态；无法核实则留待处理。回执查询是 `GET /api/v1/creative/operations/{operation_id}`，只返回本账号已保存回执，404 本身不证明未提交。

幂等锁属于最外层事务协调；随后按数据模型的账号能力、槽位/配额、业务根、修订、Blob 顺序加锁；第二批资产行锁位于 run 后、upload/candidate 与项目/画布前；本批将候选放在 upload 后、project/canvas 前，声明/用途授权放在内容身份后、修订前。只读预查可以确定需锁的 key，但加锁后必须重新核对实际关系。锁键使用 account + operation_id 的稳定映射，散列碰撞最多造成额外串行，唯一约束仍是去重依据；不将进程内 mutex 当跨 Worker 锁。嵌套调用只使用同一 TxAccountScope，不再次 Begin。

## 4. 读取、分页与流通知

- 列表使用 limit（默认 30，上限 100）、不透明 cursor；游标携带规范化过滤条件摘要、排序和最后 key，服务端校验其账号/条件，不能用它扩大查询范围。
- 首期 keyset 分页不提供历史快照跨页一致性；返回结果和 count 在同一短读快照，并带查询水位。条件改变丢弃旧 cursor，旧请求不能覆盖新结果。
- ETag 用于 HTTP 缓存验证，RevisionToken 用于领域并发前置条件，不能混用。授权媒体每次仍检查用途；不得因 304 绕过授权。
- 画布命令/上传管理/Agent 控制走 REST；Agent 增量通知走鉴权 fetch SSE。SSE 事件包含 schema_version、run_id、seq、type、时间与有界 payload，断线按 after_seq 补读，超窗要求重新查询快照。
- SSE 是提交后通知，不能在事务提交前发“已保存”；前端断流不自动取消 Agent。媒体上传进度由上传 HTTP 和状态轮询得到，不给每个节点建独立 SSE。

## 5. 应用接口与事务端口

对外应用接口接受 AccountScope，负责用例完整性；跨模块原子协作调用明确的 TxAccountScope 端口。下列是角色约定，不是已存在的方法签名：

| 接口角色 | 调用方必须知道 | 必须隐藏的复杂度 |
|---|---|---|
| CommandExecutor | 操作、明确前置条件、类型化输入；得到回执或领域错误 | hash、同 key 并发、提交结果未知、事务提交与日志 |
| ContentWriter | 创建/派生内容、锁定可用修订、引用转移 | 修订序号、归属/权利验证、不可变性与 GC 竞态 |
| MediaApplication | 会话与候选动作、状态查询、受控读取 | 外部对象版本、分片恢复、校验任务、保留交接 |
| Jobs.EnqueueInTx | 已构造的受信业务任务描述 | River 使用同物理事务的适配，不向领域暴露裸 pgx.Tx |

不向调用方开放 AddAnyReference(table, id) 这类任意关系写入口。使用方所属模块拥有自己的关系行，内容模块提供修订锁与可用性检查，应用编排保证两者在一个事务；关系清单用于审计和 GC，不是运行时任意关系引擎。

## 6. 实施验证

公共契约测试覆盖：非法额外字段、超 BIGINT/非整数版本、跨账号 ID、同 key 并发、同 key 异 body、已提交后响应丢失、操作过期、游标换条件、事务回滚与通知时机。接口正式写入 api/openapi.yaml 时执行 make generate/generate-check 并验证受影响两侧；本稿未生成或变更生产 schema.d.ts。

源码依据：现有 [ErrorEnvelope 出口](../../../../backend/internal/platform/httpapi/envelope.go)、[TxAccountScope](../../../../backend/internal/platform/store/scope_tx.go)、[OpenAPI](../../../../api/openapi.yaml)。图谱相关符号查询未命中，直接读取上述范围；不宣称新接口已经存在。

第三批Agent/Gateway的额外身份、事务参与者与细化锁序见[数据协议](gateway-harness-data.md#4-全局锁序与事务配方)。它保持上述领域相对顺序，补充供应商共享准入锁、会话/草稿、Gateway记录及外发同意；高层锁必须在业务校验前预取，禁止在revision/blob锁内重新准入。
