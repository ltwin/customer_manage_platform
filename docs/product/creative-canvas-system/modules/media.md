---
status: proposed
version: 0.1
created: 2026-09-09
---

# 媒体模块：上传、绑定、读取与回收

遵循[公共契约](common-contracts.md)与[内容模块](content.md)。媒体是图片/视频/音频的字节与处理事实；上传到画布不强制成为个人库资产。所有网络 I/O 在数据库事务外，所有正式引用交接在短事务中完成。

## 1. 配置与状态

建议实施起始配置：图片 25 MiB、音视频 250 MiB、每批 50 项，分片 8 MiB、单文件客户端并发 3，上传会话 24 小时、签名 10 分钟。图片限制解码像素与解码资源；音视频限制探测资源而不转码。上述是可配置的验证基线，格式/像素/时长/工具版本的启用清单在真实样本验证后冻结；不能在格式未经验证时向摄影师宣称支持。

HTTP API 通过 `GET /api/v1/creative/media-capabilities` 返回当前启用的 MIME/容器组合、大小和批量上限。服务端按文件内容复核，不能仅按扩展名、浏览器 MIME、HEAD metadata 或 multipart ETag 判断；未知格式返回明确失败，保留其他已成功项。

持久业务 state 保持 created / uploading / verifying / ready / failed / expired / cancelled。另以 io_phase 表示同一业务状态内的外部动作（none / initializing / completing / promoting），execution_epoch + lease_until 控制任务执行权；phase 不是可以绕过主状态的新业务状态。

| 动作 | 允许状态与过程 | 结果及恢复 |
|---|---|---|
| 创建会话 | 短事务预留 declared_size、created、入队初始化 | 返回 202 及上传 ID；Worker 标 initializing 后在事务外初始化分片，成功记录 multipart_id 并转 uploading |
| 获取分片授权 | uploading 且 io_phase=none，未取消/到期 | 短签名绑定该会话 key/uploadId/part；不能拿到 final key 写权限 |
| 请求完成 | uploading/none → uploading/completing，同事务记录操作及任务 | 返回 202；停止签新分片，Worker 列实际分片并完成，固定结果版本后转 verifying、入队校验 |
| 校验 | verifying，按固定 staging version 流式计算 hash、大小、类型和元信息 | 失败固定错误；合格后在事务中预建 Blob 身份/正式 key、设 promoting，再写正式对象 |
| 发布 | verifying/promoting，确认正式对象的固定版本与校验结果 | 同事务创建内容、绑定目标或候选、清空 uploads.blob_id、写交接和回执、置 ready |
| 取消 | created/uploading/verifying | 记录 cancelled 并失效旧 epoch，后台终止分片和核对未交接对象；已签 URL 可能短期仍可写 staging，但不能发布 |
| 到期 | 非 ready 且期限已到 | 失效旧执行权，记 expired；正在执行/结果未知的外部动作先核实，不能直接物理删除 |
| 重试 | 同一动作结果未知或临时失败 | 先查状态/回执；可恢复动作沿用会话与阶段身份。内容校验明确失败后重新选文件创建新会话，不复用失败会话冒充同内容 |

Worker 取得新 lease 时 epoch 增长；任何状态更新和正式发布均检查 state、epoch、lease 与期限。租约过期不证明外部请求未成功；救援先核实当前 io_phase 的实际结果。定向重复失败进入 failed 并呈现原因，不无限 running。初始 lease 建议 60 秒、20 秒内续租，单次处理总期限不超过会话 expires_at；具体探测超时由启用配置固定。

初始化和完成的返回丢失：通过会话唯一 staging key 列举/核实分片或已完成固定版本；不能直接重复初始化无限产生分片。出现多个候选会话/版本且不能核实时隔离为待处理错误，不猜一个发布。正式发布的 Blob ID/key 在外部写入前落库，未知结果按该位置核对后恢复；旧 worker 的迟到对象不得获得业务引用。

## 2. 上传与候选 API

| 方法 / 路径（公共前缀 `/api/v1/creative`） | 请求 | 成功结果 |
|---|---|---|
| POST /uploads | operation_id、client_created_at、文件名/声明类型/大小、target、来源声明 | 202 UploadView：id、state、io_phase、expires_at、revision |
| GET /uploads/{id} | 账号鉴权 | 当前 UploadView、已上传分片摘要、校验/绑定结果；不在历史回执中返回过期签名 |
| POST /uploads/{id}/part-authorizations | part_numbers（1–100，按实际分片数校验） | 短期 URL、方法、必需头、expires_at；只生成临时能力，不作为业务修改回执，无需 operation_id |
| POST /uploads/{id}/complete | operation envelope、expected_revision、客户端已知 part_number/etag 列表 | 202 稳定上传 ID；以服务端列分片/大小为准，不接受客户端指定 key/version |
| POST /uploads/{id}/cancel | operation envelope、expected_revision | 200 状态回执；清理异步完成，ready 不可取消而返回 state_conflict |
| GET /upload-candidates | 状态/游标 | 本账号待处理成果、到期时间、来源与原目标摘要 |
| POST /upload-candidates/{id}/adopt | operation envelope、candidate_revision、新的 target 与前置条件 | 200 采用结果及关联对象身份；同事务采纳，不返回伪成功 |
| POST /upload-candidates/{id}/discard | operation envelope、candidate_revision | 200 放弃回执；与采用/到期竞争同一候选锁 |

创建会话先核对目标账号、存在性、可写状态和引用字段；不合法则拒绝且不分配上传能力，发布时仍要再次校验，以区分初始非法目标与上传期间发生的变化。

操作 envelope 采用 common-contracts；管理会话、完成、取消、采纳和放弃不各自创建第二份幂等库。授权签名接口可重复调用且不改变文件/业务状态，调用仍限流、校验上传未进入完成阶段。

### 来源声明必须随会话持久保存

创建上传会话时，在同一事务写不可变 creative_rights_declarations、已通过策略核验的用途授权、uploads.rights_declaration_id、配额预留和初始化任务；任何一步失败整体回滚。声明不能仅存在 HTTP 请求、Worker 内存、临时签名或日志里。任务只携带 upload_id，Worker 按账号读取该明确关联，重启仍得到当时的来源证据。

发布事务再次核对声明所属账号及当前用途授权，修订采用相同 rights_declaration_id；只有用途仍允许才建立可用修订/目标引用。已撤销或不再满足用途时记失败原因，不自动补授权；已写对象保留待对账清理，不能发布成可用素材。声明的保留根包括尚未结束或仍有待核实外部动作的上传会话，以及发布后的内容修订/授权证据；压缩会话前确保来源证据已交接，不能因没有修订就提前删除仍需恢复的声明。

### 受理操作与后台发布分开

Create 和 Complete 各自使用客户端 operation_id，成功后保存各自不可变的 202 受理回执；再次请求返回首次受理结果，即使上传已经 ready。客户端随后 GET 上传读取最新状态，后台不能把原 202 回执改为 200 或另一份结果。

创建会话时另外生成服务器 publish_operation_id，一次生成、保存在 uploads，初始化/重试/进程恢复都复用；该内部操作的 client_created_at 固定为 uploads.created_at，不随重试变化。Worker 发布以该 ID 和规范化动作 publish_upload（upload_id、固定校验对象身份；epoch 是执行控制而非命令语义）进入公共命令编排；先查发布回执再决定是否应用，不重新生成发布 ID。领域的 InTx 端口参与同一操作，不各自开启另一份回执事务。

发布事务同时保存最终目标与发布回执、内容/保留关系、上传 ready 和临时保留释放。uploads.publication_result_kind/id/revision 保存当次发布结果：直接资产是 asset/asset_id，节点是 node/node_id，冲突则是 candidate/candidate_id。它们是可定位的历史结果，不能在源目标后来删除时自动重新创建。GET 上传不从“最新对象”猜测结果。

若发布进入候选，publication_result 始终记录原 candidate；GET 再读取候选当前 state。采用候选使用新的客户端操作及 candidate_revision，同事务保存 adopted_target_kind/id/revision 与 adopted_change_id、建立真实引用并释放候选保留。这样清空候选内容指针后仍能找回最终资产/节点；丢失采纳响应时同 key 回放，不再次创建。

### Target 是严格类型，而非任意表名

| kind | 字段 | 目标发生变化时 |
|---|---|---|
| asset | name、description?、有效 group_ids/tag_ids、草稿新标签、is_favorite | 标签同名竞争按库规则归并；目录已删除等无法原子满足时转待处理候选，不静默丢分类 |
| node | canvas_id、node_id、expected_data_revision | 节点必须存在、类型兼容、项目可写；已删除/已变化/已归档则转候选，不覆盖、不自动收藏 |

首批模块只定义 asset/node 两种目标，不开放任意 run/message 写目标。Agent 附件接入时扩展明确的上传目标与消息草稿保留规则，不能复用一个未绑定文件 URL 绕过授权。候选可被重新绑定到有效节点或显式存入个人库。

上传 view 的 state=ready 只证明媒体发布结束，publication 保存当次发布身份，binding 反映目标处理状态：直接发布或候选已采用为 `{status: applied, target_kind, target_id}`，待采用为 `{status: needs_review, candidate_id, expires_at}`，已放弃/到期则明确显示 discarded/expired。UI 必须分别显示上传完成和目标绑定结果，不凭 ready 就显示节点已替换。

## 3. 引用交接与配额

创建会话在账号配额锁内预留声明大小；完成后以可信字节数核算。实际超过许可/预留上限时不得擅自发布；如需增加预留仍须锁配额核验，失败清理临时对象并返回限额原因。

预留会话在上传阶段代表占用；首次正式 Blob 入库时以幂等结算标记将 reserved_bytes 转为 stored_bytes，重复任务不重复加容量。派生对象也是独立 Blob，产生前预留预算；非必要预览容量不足时回落类型卡，不破坏可用原件。staging 重试产生的临时占用需单独限制和对账，不因不计入摄影师 stored_bytes 而允许无界耗盘。图片编辑预览是另一类不计额度的 ready blob（`quota_booked_at` 为空），只由有效候选持有，采用时才绑定内容并计入额度，数量由候选上限与创建频率约束，回收见 §6。

发布短事务按统一锁序核对账号/库配额、上传、目标根和修订/Blob，建立有效使用方引用；失败回滚，外部对象暂存给恢复任务。目标变化导致候选时，pending 候选本身持有修订引用。清空上传临时 blob_id 与建立新保留在同一事务，不允许中间存在无主窗口。

采用、放弃和到期锁相同候选并检查 revision/state；只有 pending 且未到期可采纳。采用成功清空候选内容指针并保留状态/来源/操作摘要。确认目标无权访问只返回可解释冲突，不泄露其他账号资源。已入库的字节在实际物理删除确认后才扣减 stored_bytes，使用唯一释放标记，删除结果未知不能提前退容量。

## 4. 媒体读取

媒体读取从明确的合法使用入口发起，例如资产、节点或候选及其当前 revision。创建读取描述时校验账号/用途/来源修订；访问时再次核对当前授权与 ready 状态。只持有一个历史 blob ID 不自动拥有读取能力。

需要浏览器媒体标签时，`POST /media-access-tickets` 返回短期不透明票据和 API URL；票据绑定账号、明确使用入口、修订、媒体角色、display/download 用途及到期。该接口是短期访问描述的重复签发，不改变业务内容，像分片授权一样不要求 operation envelope，但仍需鉴权、限流与实时用途核验。票据不授予列举、上传或模型外发权限，也不直接延长媒体生命周期；实际 GET 时锁修订/Blob 并建立 read pin。Agent 模型输入通过内容用途守卫，不复用浏览器展示票据。

`GET/HEAD /media/{revision_id}/{role}` 以受控票据或现有认证访问。实现单段 Range：合法范围返回 206 与 Content-Range，超界返回 416，多段首期返回 416；普通 GET 返回 200。提供 Content-Type、Content-Length、ETag、私有缓存策略及安全下载名；所有响应前先授权，不能通过缓存绕过撤销。If-Range/HEAD 和取消行为通过实际浏览器验证。

read pin 建议 120 秒有效、活跃读取每 30 秒续期；续期失败或授权失效时在 lease 到期前中断流，不能让活跃读取在无保留状态下无限持续。票据只用短期、日志脱敏，不包含长期 access token。读取票据与 pin 的精确存储由本模块拥有，实施时补齐身份/过期索引，不在正文或前端永久保存。

## 5. 对象存储端口

新增流式 StorageAdapter，旧 immutablefs.ObjectStore 保持不变。

**2026-09-14 更新：该端口已不再域内。** 实现是 `platform/versionedfs` 的 `Adapter` 及其 local / oss 两个实现，媒体只是消费方之一；Skill 资源要记录并回读同一个对象版本，需要同一形状（见 [Skill 资源化与结构化输入底座 §3](skill-foundation.md)）。上提只搬接口与实现，下表的端口能力与媒体生命周期语义不变。

| 端口能力 | 必须保证 |
|---|---|
| InitMultipart / ListParts / Complete / Abort | key 由服务端产生；返回明确 session/version；完成和中止幂等可核实，未知结果单独表示 |
| AuthorizePart | 准确 key/session/part，短期方法和必要头；URL 不作为可持久业务身份 |
| OpenVersion / StatVersion | 固定版本与受控范围；返回 io.ReadCloser，不能整段视频装入内存 |
| PublishVerified | 输入可信流和预分配正式位置；服务端写入、固定版本、重复恢复不覆盖别的内容 |
| DeleteExact / ListOwned | 只操作已核对的命名空间与精确版本；分页列举，不扫整个 bucket |

Local 与 OSS 各提供适配器。Local 的分片由鉴权 API 写本地受控目录，完成后原子发布；不能把本地实现伪装成无需授权的任意文件路径读取。域逻辑共用同一状态机和 conformance，不以本地通过代替真实 OSS 权限验证。

旧端口缺流式写入、分片、版本读取的事实见 [immutablefs](../../../../backend/internal/platform/immutablefs/store.go)。当前设计只复用底层能力和已有策略，不能让旧图片调用方被迫处理本模块的新状态机。

## 6. GC、恢复和上线约束

新对象仅在 creative-v2/{account}/staging 与 blobs，旧 creative/、planning/、avatars/ 不属于新清理范围。GC 按内容保留→对象占有的顺序核实；读取保护/校验任务/上传临时持有/受信 hold 均参与判断。关系异常隔离该范围，禁止把漏引用当孤立对象删除。任何永久 hold 只由迁移/备份受信流程创建并记录理由，普通请求不能制造无限保留。正式恢复需覆盖数据库与固定对象版本，不能只备份当前列表。

**2026-10-12 修订（owner 拍板本期实现）**：schema 早已预留 blob 的 deleting/deleted、delete_after、quota_booked_at/quota_released_at 与到期索引，但回收程序一直没有实现：`stored_bytes` 全仓只增不减，资产库永久删除（`purgeAsset`）只删资产行，文件与额度都不回收。本期补齐下列设计，并作为图片编辑一期的前置（编辑预览依赖它回收，见[图片编辑后端设计](image-editing-backend.md)）。

### 6.1 不变量

- 节点、资产、版本共享同一份不可变修订和 blob；放进画布在行为上等同复制，存储上不复制。额度按 blob 计一次，与被多少处使用无关。
- 只回收「没有任何持有方」的修订和 blob。持有方没有登记完整，不能开启物理删除。
- 标记事务先取账号写屏障 `LockCreativeWrite`，与该账号全部创意写入串行（`RequireCreativeCapability` 内部也取这把锁）；新增引用仍须经 `RequireUsable` / `requireReadyBlob` 锁行并要求 ready，被标记后的修订和 blob 不能再被引用。
- 额度只在对象删除确认后释放一次，以 `quota_released_at` 为唯一释放标记；结果未知保持 deleting，不提前退额度，也不回到 ready。

### 6.2 持有方登记

| 层级 | 持有方 |
|---|---|
| 修订 | `creativereference` 的 `LiveOwnerCondition`：资产（含回收站）、节点、消息、Agent checkpoint / context、撤销记录（未过期）、节点输入、Prompt 引用、未删除版本及其输入、执行引用（保留期内或未结束）、文档、待处理上传候选、受信 hold；新增：未结束 Agent 运行的冻结输入、未结束且未过期的图片编辑候选的源修订 |
| blob | 修订的对象绑定 `creative_content_objects`；未结束上传的 `blob_id`；写入中 / 已存储的生成导入（陈旧者按 §6.3 第 2 步重置）；未结束且未过期的图片编辑候选；未过期的读 pin |
| 随 blob 删除 | `creative_blob_palettes`、`creative_media_facts` 等派生事实 |

2026-10-12 逐表定性（依据代码核对）：

| 表 | 定性 | 依据 |
|---|---|---|
| `creative_run_inputs` | 运行未结束（`creative_agent_runs.finished_at IS NULL`）时持有修订，结束后不持有 | 只有 `assemble` 与等待补充在运行期间读取；冻结时已校验可用，运行中节点被删也必须仍能发送。结束后输入只是历史事实，历史消息里的附件另由消息引用持有 |
| `creative_egress_consent_contents` | 不持有 | 迁移 0044 已声明它是授权记录而非保留根。前提是修正 `admitDispatchInTx`：现在它对授权批准过的全部修订（最多 200 个）逐个 `RequireUsable`，任何一个被回收都会让该授权下后续的所有运行失败；改为只复核本次实际发送的修订 |
| `llm_generation_outputs` | 不持有 | `content_revision_id` 只在导入成功的同一事务里写入，此时修订已由执行保留持有；之后无读取 |
| `creative_generation_imports` | 修订列不持有；写入中 / 已存储时持有其 blob 与 `reserved_bytes` | ready 时修订已由执行保留持有。未提交的导入可能被生成恢复（每次续 24 小时）再次拾起，不能按 blob 年龄回收；陈旧者由 GC 按 Prepare 已有的过期重置路径处理，之后的重试会重新拉取 |
| `creative_image_edit_renders`（V6-076，2026-10-13） | queued / running / ready 且未过期时持有源修订与结果 blob；`result_revision_id` 不持有 | 结果 blob 从未计额度，候选终结后由 GC 按 15 分钟宽限回收；写到一半的 verifying blob 由候选终结（丢弃、过期、写入失败）转为 deleting。采用时结果修订与持有它的节点版本同事务写入 |

新增 schema 守卫测试：枚举创意表中所有引用修订或 blob 的列，每列必须登记为「持有」或「不持有」，漏登记即失败，防止以后新表静默漏引用。

### 6.3 回收步骤

GC 是 creative-worker 进程内的周期循环（与上传过期扫描同一写法，不经 River 队列：每步都是「取写屏障 → 锁行重查 → 状态迁移」，天然幂等，失败留到下一轮），每 10 分钟逐账号执行，每批 100 行，单批事务保持短小以免长时间占用账号写屏障。停用账号同样回收，GC 直接取写屏障，不走 `RequireCreativeCapability`。

1. **修订**：扫描 state=ready 的图片 / 视频 / 音频修订中无持有方者；事务内取写屏障 → `FOR UPDATE` 重查 → 置 `deleted` 并删除其对象绑定。这一步不碰外部存储，不需要中间态。文本修订不占字节，一期不回收。
2. **blob**：扫描 ready 且无对象绑定、无持有方的 blob；事务内取写屏障 → `FOR UPDATE` 重查 → 置 deleting 并写 `delete_after`。verifying blob 不按年龄回收：上传的由上传过期路径处理；生成导入的，由 GC 对 lease 已过期且 48 小时没有进展的写入中 / 已存储导入执行与 `PrepareGeneratedMedia` 相同的重置（epoch+1、清空 blob 字段、退还 `reserved_bytes`、旧 blob 置 deleting）。为此给 `creative_generation_imports` 补 `updated_at`。重置后，迟到的 Commit 会因 epoch 不符而失败，之后的重试会重新拉取。
3. **删除对象**：每批先在账号写屏障内检查资产 / 节点引用一致性，异常则停止该账号本轮回收。到期对象在分页前按当前驱动与 bucket 过滤，不匹配者单独计数；选中批次先把 `delete_after` 推迟 10 分钟作为下次重试时间，再在事务外删除对象并做 HEAD 不存在确认；再在事务内置 deleted，若已计额度且未释放，则扣减 `stored_bytes` 并写 `quota_released_at`，同时删除派生事实。失败或结果未知保持 deleting，下一轮重试。

| blob 类别 | 宽限 | 理由 |
|---|---|---|
| 已计额度（曾绑定内容） | 24 小时 | 误删兜底，并覆盖票据与 read pin |
| 从未绑定、未计额度（图片编辑预览） | 15 分钟 | 不可能被内容引用，只需覆盖 10 分钟读票据与 120 秒 read pin |

对象删除方式：`versionedfs.Adapter` 现有 `DeleteExact(key, version)` 会永久删除指定版本。OSS 侧新增「删除当前对象」方法，只留 delete marker，固定版本转为 noncurrent，由 bucket 生命周期规则在 N 天后清除（[部署契约](../../../dev/object-storage.md)建议 30 天）；这段时间内可凭库中保留的 key 与 version 恢复。Local 驱动没有版本兜底，沿用 `DeleteExact`，仅用于开发与自部署。

### 6.4 额度与资产库体验

- 永久删除后空间不会立即释放：需要等 24 小时宽限，并且该内容不再被任何画布、历史版本、撤销记录或生成记录使用（生成执行结果保留 90 天）。确认弹窗写明「画布里已经用到的副本会保留，空间在它们也不再使用后释放」；资产库按约定不展示具体被哪个画布使用。
- 用量面板增加「待释放」：已计额度且处于 deleting 的 blob 字节合计。

### 6.5 上线顺序

GC 用运行模式配置 `CREATIVE_MEDIA_GC_MODE`（off / scan / delete，缺省 scan）区分「只读扫描」与「标记并删除」。创意空间截至 2026-10-12 尚未上生产，没有存量数据可扫，因此：

1. 完成持有方登记与守卫测试；任一表未定性，不进入下一步。
2. 本地 / 测试库只读扫描：先走完上传、生成、Agent 运行、撤销、版本删除、资产永久删除等真实流程，再按账号输出「应回收」的修订数、blob 数与字节，逐项核对与预期一致，不做任何标记。
3. 恢复演练：从 deleted 的 blob 记录按 key 与 version 找回对象。通过后在本地 / 测试环境开启标记与删除。
4. 首次上生产时 GC 先以只读模式运行 3 天，核对指标后再切到删除模式。creative-worker 的镜像与部署随创意空间发布一起补齐，不算本条前置。
5. 图片编辑在此之上上线，不另做回收程序。

指标：每轮扫描 / 标记 / 删除数量与字节、deleting 积压、删除失败、待释放额度、单批持锁时长。测试矩阵与分阶段验证见 [实施切片](implementation-slices.md)。具体 OSS 权限/CORS 仍沿[部署契约](../../../dev/object-storage.md#创意画布新版媒体部署契约待实施)；新增的删除当前对象需要 `oss:DeleteObject`，未执行真实环境验证时必须标明。


## 第三批附件目标接入

第一批target=asset/node保持原语义；第三批新增agent_attachment，需在正式目标union及迁移中一起接入，旧13表草案不是附件实现。目标包含draft_id/expected_draft_revision，发布前由Harness.ValidateAttachmentTargetInTx锁同会话draft并验证open、期限/容量；PublishAttachmentInTx建立ready附件→revision根，与媒体publish_operation回执同事务。目标改变沿用pending候选，adopt可以绑定新draft或存库，禁止插入已发送消息。附件发送、过期和显式存库的根交接见[Harness附件](harness.md#4-独立附件)。具体锁序包含draft在upload之前，不能先锁upload再回头取draft。

## 首期节点Prompt附件与生成结果

FND-13新增上传target=node_prompt，正式union/目标守卫与其他目标一并生成。发布时锁上传→所属project/canvas→node→node_prompt_draft→授权/revision/blob，核对draft_revision、节点存活/可写；这是节点草稿的专用顺序，区别于既有Agent会话draft。编辑/删除节点或草稿采用canvas→node→draft，不在持有这些锁时倒序获取upload；上传关闭通过任务重新核验目标，不由删节点反向锁上传。目标冲突进入既有pending候选；成功则创建prompt_refs固定修订根，发送时同事务建立execution输入根。完整生成输入/输出、版本保留及受控供应商结果导入见[node-generation](../node-generation.md)，不把供应商URL当长期作品地址。
