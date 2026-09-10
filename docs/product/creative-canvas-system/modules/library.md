---
status: proposed
version: 0.1
created: 2026-09-09
---

# 个人资产库模块

承接 LIB-01–06 与 FLOW-02/03/06，遵循[公共契约](common-contracts.md)、[内容](content.md)和[媒体](media.md)。资产库属于账号，可在没有项目时使用。本文定义真实查询与编辑接口，不恢复独立素材分类体系，也不把 Agent 的库管理工具纳入首期。

## 1. 对象与版本

资产采用一个明确的 content_id/content_revision_id，名称/描述、最爱与整理关系属于资产条目。分组是多重归类目录，标签分类仅组织标签。最爱是固定查询，未归类是没有普通分组成员的查询，均不创建可删除的系统组。

| 对象 | 前缀 / 关键字段 | 写入与版本 |
|---|---|---|
| Asset | ccas；kind、title/normalized_title、description、内容引用、is_favorite、deleted_at/purge_after | revision 覆盖元信息、采用内容和成员/标签变化；kind 从真实内容推导 |
| AssetGroup | ccag；parent_id、name、position | revision 为对象版本；移动、删除、排序还检查 hierarchy_revision |
| Tag / TagCategory | cctg / cctc；name、normalized_name、color / category_id | 标签规范化名称账号内唯一；改名/颜色不修改内容修订 |
| LibrarySettings | 每账号一行 | hierarchy_revision 管目录/标签结构；library_revision 是检索结果水位，所有影响检索/数量/排序的变化递增 |

新增相关版本均用正 BIGINT 的十进制字符串。目录或标签删除导致受影响资产的关联变化时，递增这些资产 revision，并在一次库事务中递增一次 library_revision；不能让旧“整组替换标签”请求覆盖后来清理。无实际变化的幂等设置可返回当前状态，不能伪造一次编辑版本。

## 2. 检索契约

`GET /api/v1/creative/assets` 参数如下；GET 资产详情同样校验账号。

| 参数 | 语义 |
|---|---|
| view | all / favorites / unclassified / trash；默认 all。trash 独立视图，不与其他 view 混用 |
| group_id、include_descendants | 指定组直接成员；显式 true 扩到子树。无 group_id 表示全库；与 unclassified 同传拒绝 |
| kind | image / text / link / video / audio；不传不过滤 |
| q | 一个规范化的字面包含查询，最多 200 字符；空白视为未过滤。不支持隐式布尔表达式或 RAG |
| tag_ids、tag_mode | 去重后最多 50 个有效标签；all 表示同时包含，any 表示任意包含；空列表均不加标签条件 |
| sort | recent / oldest / name，分别按 created_at DESC/ASC 或 normalized_title ASC，并用 id 同向打破相同值 |
| limit、cursor | 沿公共契约，游标绑定账号/所有过滤条件/排序和末项 key |

除 trash 外仅返回 deleted_at IS NULL；同一资产匹配多个标签或多个子组仍只出现一次。q 覆盖规范化标题、描述、当前文字/链接内容、source_url，并 OR 匹配已关联标签名称。标签名称通过账号 JOIN 实时读取，改名不需要把旧名写回内容；q 中的 `%`、`_` 和反斜线作为字面值转义，不作为 SQL 通配符执行。

规范化为固定 Unicode 版本的 NFKC、统一大小写、去首尾空白，原始显示文字保留；算法版本进入游标和重建任务。名称排序使用 normalized_title 的字节序及 ID，首期不承诺拼音排序或相关度排序。pg_trgm 的中文/locale 行为按真实查询计划验证，不以“建了 GIN”宣布达标。

无效或已不可见的 group/tag 返回 not_found，不静默退回全库；前端保留原条件并标注需要刷新。结果对象包括 items、next_cursor、total_count、library_revision；页与数量从同一短只读快照获取。后续页发现水位变化可提示刷新并重建当前列表，不承诺跨页历史快照。取消旧网络请求加查询序号，避免较慢响应覆盖新选择。

查询结构示例见 [library-search.sql](library-search.sql)：子树使用去重递归、成员/标签用 EXISTS，避免 JOIN 扩行。它只给 recent 排序的可执行骨架；其余排序用固定 SQL 变体，共享过滤构造，不能拼接客户端 SQL 标识符。计数复用同一过滤表达式，在同一 ReadTxAccountScope 内执行。

## 3. 写 API

所有业务写入携带 operation envelope；单对象修改要求 expected_revision，涉及结构的动作还要求 hierarchy_revision。以下路径均位于 `/api/v1/creative`。

| 接口 | 输入与结果 |
|---|---|
| POST /assets | 严格区分 text、link、from_revision 三种创建；title/description、整理信息、来源输入；返回资产及实际 tag ID |
| POST /assets/{id}/metadata | 具名可选字段 title/description/is_favorite；只改出现的字段，不允许任意 JSON Patch |
| POST /assets/{id}/content | 明确的新文字/链接或已获准修订；核对旧指针，内容用 Fork/明确修订替换，绝不修改其他使用方 |
| POST /assets/batch-organize | 最多 100 个明确资产 ID 及各自 revision；add/remove 的 group_ids/tag_ids；全批成功或回滚 |
| POST /assets/{id}/trash /restore /purge | 软删、恢复、彻底删除；批量对应 /assets/batch-trash、batch-restore、batch-purge，最多100个显式 ID |
| GET/POST /asset-groups；POST /asset-groups/{id}/rename /move /delete | 移动带 parent_id 与目标同级顺序；删除提升直接子组、释放该组成员，保留资产 |
| GET/POST /tags；POST /tags/{id}/edit /delete | 名称、颜色、分类；删除显式移除所有资产标签关系，不删资产 |
| GET/POST /tag-categories；POST /tag-categories/{id}/edit /delete | 删除分类时标签 category_id 置空；标签继续存在 |
| GET /library-settings；POST /library-settings | 保留天数 7/30/90/null，expected_revision 使用设置自身 revision；不静默重算历史 purge_after |

文件类型的普通导入走已有 POST /uploads target=asset，不接受把 OSS URL 当作 from_revision。新文字/链接的来源声明与内容/资产/整理关系同事务创建。from_revision 必须来自账号内合法可用的使用入口，不能凭一个历史 ID 复活已清理内容。保存画布内容的专用协作用例见 §5。

新标签草稿使用 client_tag_key、name、color、category_id?；临时 key 不入库。保存资产时规范化去重，并发同名归并现有 ID/颜色而不覆盖；返回每个 client_tag_key 的实际 ID。取消资产草稿不产生标签；独立“新建标签”是摄影师明确保存的动作，可单独持久化。新标签与既有 tag_ids 的结果集合再次去重。

标题默认来自文件名/首行内容的有界摘要，必须返回实际标题；title≤200、description≤2000、组/标签/分类名1–80字符，颜色为 #RRGGBB。批量编辑不得把查询条件当作隐式“全选全库”写范围；前端传明确选中身份，未来跨页全选另设计服务端选择快照。

## 4. 目录、回收站与锁

所有资产成员变更、目录/标签结构写入和 purge 都取得账号库根锁；其后按 ID 锁资产，核对完整目标集合的账号、状态和版本。创建/移动目录在锁内验证目标父链无环；历史坏数据扫描不能靠递归无限循环才报错。

普通组同级名称首期不强制唯一，以稳定 ID 区分；移动/删除后重排 position 并递增受影响组 revision。删除组不保留其目录身份用于恢复，回收站资产恢复时只保留仍存在的组；最爱标志保留。非空组的删除反馈说明子组提升及资产仍在库中，不新增隐式资产删除。

软删固定 deleted_at/purge_after，保留分组/标签关系、最爱和内容引用；trash 内禁止常规内容/整理编辑，允许恢复/彻底删除。恢复与到期 purge 用同一库根/资产锁和前置版本，只有一方成功；恢复清空 deleted_at/purge_after。不直接允许活跃资产 purge，先移入回收站。

purge 同事务删除成员、标签、搜索投影、资产行并释放该条目内容引用；文件交媒体 GC 复核。目录/标签删除作用于普通与回收站成员，且维护计数/版本。设置的 retention_days 变更仅影响后续删除；历史过期时间批量调整不属于首期隐含动作。

## 5. 对媒体、画布与 Agent 的端口

| 受信端口 | 责任与调用约束 |
|---|---|
| ValidateImportTargetInTx | 预查 target=asset 的目录/标签/草稿合法性，创建会话和发布均调用；锁库根，后续发布不能跨另一个事务失去验证 |
| CreateFromRevisionInTx | caller 已取得统一锁序所需根；核对 revision/用途，创建资产、实际整理关系、搜索投影并递增库水位；参与调用方操作，不独立写第二份回执 |
| ResolveAssetsInTx | 锁库根/明确资产，校验可见活跃状态、expected_asset_revision 与选定 content_revision；返回可绑定修订及来源快照 |
| SearchAssets | Agent 与前端使用同一过滤语义；Agent 外发搜索结果仍需 Harness/Gateway 授权，不扩大搜索范围 |

媒体发布 target=asset 原子完成 CreateFromRevisionInTx 与上传结果/回执交接；目录变化等业务冲突转媒体候选，内部错误/提交结果未知先核实，不能把任意错误当可安全重建的候选。

画布拖入资产由画布应用编排先锁库根和选中资产，再锁项目/画布，再锁内容；不能先锁画布再回头拿库锁。保存节点到个人库使用 `POST /assets/from-canvas-node`，携带 canvas_id/node_id/expected_data_revision、目标元信息及整理草稿；同事务固定节点当前修订并创建资产。再次同操作不重复收藏，不同明确操作允许形成另一个资产条目；不以 hash 自动去重。

读取、导入与保存仍使用各自模块端口，禁止库仓储直接更新节点，或让画布仓储写标签表。该协作不建立项目→资产所有权，更不建立项目→CRM 关系。

## 6. 交付与验证

表结构见 [library-canvas-schema.sql](library-canvas-schema.sql)，与第一批内容/媒体13表叠加。标签精确交集、未归类、子树去重、跨账号和回收站查询有 SQL 探针；正式来源/用途、域内错误与全部并发仍须通过服务端口测试。后续搜索性能按10,000资产样本验证，不能用本批小样本推断 p95。
