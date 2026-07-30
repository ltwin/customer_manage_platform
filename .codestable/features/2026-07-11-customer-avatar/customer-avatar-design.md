---
doc_type: feature-design
feature: 2026-07-11-customer-avatar
requirement: customer-profile
roadmap: photographer-private-crm
roadmap_item: customer-avatar
status: approved
summary: 为客户提供可选头像的安全上传、替换、鉴权读取与移除能力，并让客户展示和选择面统一使用真实头像或首字 fallback
tags: [customer, avatar, media, storage, picker]
---

# customer-avatar · 客户头像 design

## 0. 术语约定

| 术语 | 定义 | 防冲突结论 |
|---|---|---|
| 客户头像（Customer Avatar） | 客户档案上的可选视觉标识；每个客户最多一个当前头像 | 属于 Customer 自身属性，不是作品、拍摄回顾、相册或交付照片 |
| AvatarObjectStore | customer 应用层拥有的头像对象存储 port，语义为按应用逻辑 key 执行不可变 Put / Open / Stat / List / Delete | 不叫 FileStore 或 OSSClient；逻辑 key 是跨 adapter 的对象身份，接口不泄漏本地 root/path、bucket、endpoint 或 SDK 类型 |
| avatar_url | Customer API 的只读相对 URL，精确形式为 `/api/v1/customers/{id}/avatar/content?v={avatar_version}` | 指向应用内鉴权媒体端点；不是本地路径或 OSS 临时签名 URL，v 必须与当前 pointer 相等 |
| avatar_version | `sha256-` + 最终规范化字节 SHA-256 的 64 位小写十六进制；是公开内容版本与 HTTP ETag 值 | 与厂商 ETag 解耦；相同精确字节得到相同版本，但可以对应不同的物理发布代次 |
| avatar_revision | Customer 上始终返回的写并发 token，API 编码为 `ar-{非负十进制 bigint}`；初始 ar-0，pointer 每次成功改变后 +1 | PUT/DELETE 的 If-Match 比较它而非 checksum，防 A→B→A 的 ABA；no-op 不增，GET URL/ETag 不使用它 |
| avatar_object_id | 服务端每次物理发布生成的 crypto-random 128-bit 值，以 32 位小写十六进制保存 | 只用于区分不可复用的物理代次，不下发前端；一个代次离开 current 后永不复用其 object_id/key |
| 当前头像 pointer | PostgreSQL 中 Customer 的 `avatar_version/avatar_object_id/avatar_media_type/avatar_size/avatar_updated_at` 五字段集合；avatar_revision 是同一 Customer 行上的独立 revision | PostgreSQL 是业务事实源；local/OSS 只保存由 pointer 引用的 durable binary generation |
| avatar_object_gc / avatar_reconciliation_checkpoint | GC 表是按 `(account_id, customer_id, avatar_object_id)` 唯一的 pending 延迟队列；checkpoint 持久化每账号 List cursor/cycle | 精确代次删除防止旧 worker 伤及同 checksum 的新 current；后台任务只经 server-side AccountScopeEnumerator，不接受客户端 account_id |
| 首字 fallback | 无头像或头像读取失败时，以 display_name 的首个 Unicode grapheme 加稳定色块显示 | 沿用现有圆形 avatar 视觉，但不再直接使用 display_name[0]，避免 emoji / 组合字符截断 |
| CustomerPicker | 在客户候选中展示头像、昵称、短 UID 与状态的共享选择组件 | 替换只能显示纯文本的原生 select / 手填客户 ID；不新增第二套客户搜索实体 |

术语 grep 输入：CONTEXT.md、roadmap §4、customer-profile requirement、既有 customer designs、backend/internal/customer、api/openapi.yaml、frontend/src 下 avatar / referrer / customer selector、头像拉伸 issue。结论：

- 代码中的 CSS 类 avatar 与 prototypeData.avatar 目前只表示首字色块；新业务字段必须命名 avatar_url，不复用 prototype 的颜色类字符串。
- Customer / Account / Social Identity 等领域术语全部沿用 CONTEXT.md；正文不用“用户”指代摄影师或客户。
- CustomerPicker 是前端组件名，不新增领域实体或公开 API 术语。

## 1. 决策与约束

### 需求摘要

- **做什么**：客户建档后可设置、替换或移除可选头像；客户列表、详情和所有客户选择面统一显示真实头像，未设置或读取失败时显示首字 fallback。
- **为谁**：摄影师 owner——在同名、昵称相近或账号较多时，靠头像 + 昵称 + 短 UID 更快识别客户。
- **成功标准**：
  1. 合法 JPEG / PNG / WebP 可上传并经鉴权读取；当前 pointer 始终只指向一个不可变物理代次，移除、结果未知重试与迟到删除安全；
  2. 跨账号访问不可见；merged 客户禁止设置/替换但可显式 cleanup-only 移除既有头像，archived 客户仍可设置 / 替换 / 移除；
  3. 列表、详情、转介绍、merge、订单和档期客户选择面均显示真实头像或一致 fallback；
  4. 首版本地持久卷在容器重建后保留数据，配置与备份说明可执行；
  5. make check 全绿且浏览器证据覆盖桌面与 375px。
- **明确不做**：
  1. 不把头像加入 30 秒建档必填项或建档表单；
  2. 不实现 OSS adapter、存量头像搬迁、客户端直传或预签名 URL 直读；这些是后续迁移 / 交付优化；
  3. 不做裁剪编辑器、人脸识别、自动抠图、滤镜、拍摄回顾、相册或原图保留；
  4. 不接收 GIF / SVG / HEIC，不保留动画与 EXIF 等原始元数据；
  5. 不开放匿名媒体目录，不把 token 放进 URL query，不把本地路径或对象 key 下发给前端；
  6. merge 不自动迁移、继承、删除或二选一头像：target 与 source 默认各自保留原头像；merged source 只额外开放显式 cleanup-only DELETE，不恢复 PUT 或其他档案编辑能力；
  7. 现有 JSON data-export 不内嵌头像二进制，本 feature 不修改导出格式；
  8. 不引入前端 UI 组件库。

### 复杂度档位

沿用生产 Web feature 默认档位：健壮性 L3、结构 layers、性能 reasonable、可读性 team、可演进性 stable、可观测性 logged、可测试性 tested、安全性 validated。特殊维度：

- **Concurrency = revision CAS + per-customer serialized**：PUT / DELETE 强制 If-Match=avatar_revision；同一客户写入与 merge 共享客户行锁和锁内状态重验，A→B→A 后的旧 revision 也不能改变较新 pointer。desired=current / already-none 无状态变化，可对结果未知重放返回 200。
- **Idempotency = immutable generation convergence**：当前已是 desired checksum 的 PUT 不发布新对象并直接 200、当前已无头像的 DELETE 直接 200；单次发布仅可用同一 object_id/key 做内部重试，离开 current 的物理 object_id 永不复用。
- **Compatibility = backward-compatible**：avatar_url 为可选只读字段；既有无头像数据与不消费该字段的 caller 行为不变。

### 关键决策

- **D1 推进条目**：customer-avatar 已从 roadmap 启动并标记 in-progress；customer-profile-complete、order-tracking、schedule-calendar 均已 done，后两项作为“既有客户选择 UI 的兼容增量”依赖，不回退其状态。同样已解锁的 reminder-engine 不在本 feature 范围。
- **D2 头像专用 port，而非路径透传或万能 BlobStore**：选择 customer 侧拥有的 AvatarObjectStore。比较过三种接口：
  1. service 直接操作本地路径——首版代码少，但路径、原子替换、安全校验会进入业务编排，迁 OSS 必改上层，否决；
  2. 全局万能 BlobStore——表面通用，但当前只有头像用例，caller 仍要自己拼 key / 元数据 / 生命周期，接口浅，否决；
  3. 头像语义 port——PutImmutable / Open / Stat / List / Delete 接受应用拥有的逻辑 ObjectRef/key，隐藏本地 root/path、durable write、bucket/endpoint、厂商元数据和将来 OSS Put/Get/List/Delete，选择；Stat/List 分别服务读取完整性校验与 orphan reconciliation，不是空泛预留。
- **D3 媒体始终经应用鉴权读取**：avatar_url 精确指向 `GET /api/v1/customers/{id}/avatar/content?v={avatar_version}`，前端通过统一媒体 client 携 Bearer 获取 Blob。比较过匿名高熵 URL（PII 泄漏且属于安全靠猜，否决）与私有 OSS 预签名 URL（可行但会改变交付层、增加过期/CORS 语义，后置）；应用代理让首轮 local→OSS 只换 adapter。
- **D4 内容 version、写 revision 与物理代次三分离**：`avatar_version` 是最终规范化字节的 `sha256-{64位小写 hex}`，只用于 avatar_url/v/ETag；`avatar_revision` 是每次 pointer 改变时递增的 CAS token，只用于 PUT/DELETE If-Match；`avatar_object_id` 是内部不可复用物理代次。对象 key 固定为 `avatars/{account_id}/customers/{customer_id}/{avatar_version}/{avatar_object_id}`。每次实际发布生成新的 128-bit 随机 object_id；同一发布 key 的内部重试只核对 metadata 与实际 checksum，不覆盖字节，目标 key 碰撞则换新 ID；同内容以后再次发布可复用公开 version，但必须使用新 object_id/key。avatar_url 不暴露 revision/object_id；缺 v 400、非当前 v 409，旧 URL 不返回新字节。customer avatar application read service 根据五字段 pointer 派生当前 key，在 5 MiB 上限内完整读取并核对实际 media type/size/checksum，向 HTTP 层返回 validated bytes + metadata 后，handler 才能发送 200/304；因此 If-None-Match 命中也先验完整性，再由 handler 返回 quoted ETag、private/no-cache、Vary Authorization、nosniff 的 304。
- **D5 文件边界与规范化**：原始文件 ≤5 MiB；仅 JPEG / PNG / WebP，服务端同时核对声明 MIME、魔数与真实解码；解码后宽高各 ≤4096。服务端应用 EXIF 方向、移除元数据、生成最长边 ≤512 且编码后 ≤5 MiB 的非动画栅格结果，不保留原图。规范化不得写入时间戳、随机数等非确定 metadata；同一构建对同一输入必须产生相同精确字节与 checksum。失败统一 400 validation_failed，且不得改变当前头像。
- **D6 客户状态与 merge 语义**：active / archived 允许 PUT / DELETE；merged 的 PUT（含 same-content）始终返回 409 customer_merged，GET 仍可读取其既有头像，但 DELETE 是 owner 拍板的唯一 cleanup-only PII 清理例外：有头像且 revision 匹配时只清 pointer、revision +1 并入 GC，已无头像则 200 no-op。该例外不恢复 merged 档案的其他编辑能力；merge 本身不修改两边头像，避免隐藏继承或删除。
- **D7 跨资源一致性采用“新代次先落、revision CAS、五字段 pointer 后切、精确代次延迟 GC”**：唯一 transaction owner 为 AccountScope.WithTxScope，callback 只拿不可嵌套开事务的 TxAccountScope；customer repository 的锁行/pointer/GC 操作消费该 scope。PUT 在锁外规范化；快照若已是 desired checksum，则短事务锁 Customer，先重验账号与状态，merged 必须 409，active/archived 才继续；随后完整读取/验证当前 ObjectRef，只有实际字节完整才 no-op 200，缺失/损坏时不得成功，revision 匹配才进入 fresh generation 修复。其余路径生成全新 object_id 并 PutImmutable；事务内锁 Customer，再锁新 object_id 的 GC row：只要 row 存在，该代次就视为已进入回收生命周期，绝不取消或晋升，直接弃用并以全新 object_id 有界重发；无 row 才 final Stat，缺失或 key 碰撞同样换新 ID。并发请求若已把相同 checksum 设为 current，也须重验状态并完整验证 current；有效时本次未引用 generation 入队并 200，无效时走 revision CAS 修复。任何需要改变 pointer 的路径都校验 If-Match=avatar_revision；失配 409，匹配时原子切换五字段 pointer、revision +1，并把旧 ObjectRef 入队。DB 失败最多留下 reconciliation 可发现的 orphan，不破坏旧 current/revision。GC 只删除队列记录指定的旧 object_id/key；即使 PostgreSQL session 丢失后 Delete 迟到，也不能命中新 current。
- **D8 OpenAPI 与运行时切片**：roadmap §4.1 / §4.2 / §4.3 / §4.3a 为语义权威；OpenAPI 的 Customer / CustomerListItem / CustomerSummary 都投影始终存在的 avatar_revision 与成对缺省的 avatar_version/avatar_url，其中 CustomerSummary 保留既有 id/display_name/channel/status，只增头像字段，使 referrer 等既有嵌套摘要无需额外 detail 请求即可显示头像；新增 revision If-Match、v、If-None-Match、ETag/304、binary/multipart 与 typed `avatar_revision_conflict` / `avatar_version_stale` 子码；`GET /customers status` 保持单值兼容并扩为逗号分隔状态集合，服务端在分页前应用 status+q。头像操作打 customer-avatar tag，Go include-tags 与受保护路由同步；TS/Go 类型只从 codegen 生成，禁手写重复 DTO。
- **D9 共享 CustomerAvatar**：组件独占鉴权 Blob、fallback、object URL、尺寸与 a11y。候选进入视口才取图；同 auth generation+URL+avatar_revision 在途去重并按消费者引用计数，单个组件卸载只释放自身引用，最后一个消费者离开才 abort/revoke；revision 改变即使 URL 因同 checksum repair 未变，也必须释放旧媒体状态并重取，401/logout 则统一 abort/clear，绝不跨 token 复用。401 沿用 ApiError 清 token并跳登录，其他失败 fallback。页面只传 id/display_name/avatar_revision/avatar_url，不复制媒体逻辑。
- **D10 共享 CustomerPicker**：原生 select 无法在候选项显示图片，保留它只能在选中后显示预览，不满足“选择前辅助识别”；嵌套 modal 会与订单 / 档期现有 dialog 冲突。选择可嵌入表单的可访问 search + listbox 组件：候选行固定显示 CustomerAvatar、display_name、短 UID、status，支持 loading / empty / error / disabled、键盘和 focus。组件消费生成的 CustomerListItem；caller 显式给 `candidateStatuses`、`excludeCustomerIds` 和可选 `selectedCustomer` 摘要。服务端必须在 q+完整状态集合过滤后稳定分页；首版不新增 exclude query，组件按原始页应用 exclude，并以服务端 raw page/total 推进，过滤后空页必须继续取后页，不能把“当前页无可见项”误判为结果结束。caller 矩阵固定：新建/编辑转介绍的新候选、merge source、订单新建、未来档期均 active-only；历史档期为 active+archived；merged 永不作为新候选。编辑页若既有 referrer 后来 archived/merged，selectedCustomer 仍以 pinned 当前值显示并可原样保留，但不混回新候选结果；清除时必须同请求把 channel 改为非 referral，否则 required 校验阻止保存，不得放宽既有 channel/referrer 不变量。
- **D11 展示范围一次收口**：真实头像 / fallback 覆盖客户列表、客户详情、转介绍创建与编辑、merge source、订单新建、档期客户选择及固定客户只读摘要。Dashboard 尚未接真实 API，留 dashboard feature 消费同一 CustomerAvatar；不把原型假数据改造塞进本条。
- **D12 本地持久化配置 fail-fast**：首版 driver=local，配置 `AVATAR_STORAGE_DRIVER/AVATAR_LOCAL_ROOT/AVATAR_LOCAL_REQUIRE_MOUNT`。二进制直跑 require-mount 缺省 false；production compose 固定 true。true 时先解析真实路径并从 Linux mount table 证明 root 是独立 mountpoint，再检查创建/权限/可写；缺卷、无法验证或身份不符均启动失败，不能因镜像预建目录可写而落容器层。local PutImmutable 仍使用 content+metadata fsync、atomic rename、parent fsync与受控 stale temp cleanup。
- **D13 GC、双向 reconciliation 与 MaintenanceRunner**：GC 表只有 pending，以 `(account_id, customer_id, avatar_object_id)` 唯一并保存 avatar_version；首次 enqueue 写 `not_before=now+24h`，重复 enqueue 保留最早 not_before/retry，失败退避只改 next_attempt_at/attempts/last_error。GC 每对象独立事务按 `Customer → GC row` 加锁并复核 pointer：待删 object_id 若等于 current，只删 stale queue row并 commit，绝不调用 storage Delete；不等时才对精确 generation key 执行有超时的幂等 Delete。Delete 失败先 rollback 主事务，再按同一 row identity 在独立短事务条件更新退避，row 已不存在时不重建；commit unknown 保留 row 并由下轮幂等重试。reconciliation 经 AccountScopeEnumerator 做两个稳定分页方向：object inventory 按账号 key cursor 找 orphan，pending insert+cursor 同事务；current-pointer audit 按 Customer id cursor 对非空 ObjectRef 做与 GET 相同的完整字节校验，缺失/损坏只记录 integrity 并前进，单项 Temporary/internal I/O 本 tick 有限重试后记录 audit-temporary、推进 cursor并在下一完整 cycle 再访，绝不清 pointer或入 GC，也不能让单 key 饿死后页。checkpoint 分别保存两套 cursor/cycle。runner ready 后立即一轮、之后每小时 single-flight；首版固定每账号一页 object inventory + 一页 pointer audit + 最多100 due，注入 clock/ticker，重叠不并发，shutdown取消并有界等待。多 runner 由行锁、队列唯一约束和不可复用 generation 保证正确性；备份 freeze 覆盖 API/runner，OSS 迁移另起。
- **D14 生产生命周期与 exact-generation 备份**：现有 server 入口使用 `context.Background()` 且未做 signal-aware graceful shutdown，不能承载 D13；本 feature 必须把最小生产生命周期纳入范围：OS signal 取消统一 root context，停止新 maintenance tick，取消在途 storage I/O，有界等待 runner，并在 HTTP graceful shutdown 后退出，不能继续推迟到 v1-hardening。首版本地一致备份优先停 app；manifest 对每个 current 记录 account/customer/avatar_version/avatar_object_id/derived key/media type/size/actual SHA-256，并附全物理 key/count/checksum 汇总。恢复必须逐精确 key 核验 DB pointer 与实际字节后才开放写；同 checksum 不同 object_id 不能靠 count/checksum 汇总替代。cleanup-only DELETE 不追溯擦除既有历史备份，恢复旧备份可能重新带回头像 PII；备份 retention/销毁策略与此在线清理动作分开说明，UI/文档不得承诺跨备份彻底擦除。未来 OSS freeze-copy-verify-switch 也按 exact key/object_id 核对。

### 执行风险与证据计划

- **Top 3 风险**：
  1. **DB pointer 与二进制对象错配或迟到删除**（最难恢复）：跨资源无法由 PostgreSQL 单事务原子覆盖，DB/session 丢失也不能取消已经发出的存储 Delete。缓解：D4/D7/D13 不可复用 generation key、revision CAS、五字段 pointer 后切、进入 GC 即烧毁、精确代次 GC/reconciliation；S4 + A8-A11/A23/A24 注入 rollback/commit unknown/storage timeout/DB restart/迟到 Delete 与 runner/reconciliation 矩阵。
  2. **私有头像被匿名或跨账号读取**（安全风险最高）：普通 img src 容易诱导公开目录或 token query。缓解：D3 同源鉴权媒体 client；A13 双账号 / 无 token / 路径逃逸矩阵。
  3. **共享客户选择组件回归订单 / 档期关键流**（验收最易漏）：caller 的 active/archived/merged 与“既有非 active 当前值”规则不同。缓解：D8/D10 固定服务端 status-set + q 分页与 pinned selected 语义；S7 独立切片；A17/A18 逐入口浏览器证据。
- **非显然依赖**：应用在阿里云 ECS 自部署但当前 compose 仅有 pgdata 卷；现有 server 以 `context.Background()` 启动且 graceful shutdown 留给 v1-hardening，本条为保证 MaintenanceRunner 必须提前接入 signal-aware root context；现有 request helper 强制 JSON Content-Type，binary / multipart 需独立媒体请求面；现有 .avatar CSS 有 customer-profile-avatar-stretch 回归门禁；image 规范化依赖需由 implement 选择可维护库，但库类型不得进入 port；订单/档期虽已 done，本条必须修改其选择 UI 并负责回归；cleanup-only DELETE 只处理在线 pointer/活动存储，历史备份保留与销毁是独立运维策略。
- **Owner 已拍板（2026-07-13）**：① merged 禁止 PUT，但允许显式 cleanup-only DELETE 既有头像；② 首版采用 24h grace、每小时 runner、每账号每 tick 各 1 页 object inventory/current-pointer audit + 100 due。
- **关键假设**（review 时可精确反驳）：① 5 MiB / 4096 / 512 是首版合理边界；② archived 可维护头像；③ production 容器为可读 Linux mount table 的环境，binary 轨显式关闭 mount attestation；④首轮 OSS 仍应用代理；⑤全客户选择面本条统一，Dashboard 除外；⑥ JSON export 暂不含二进制。
- **验证基线**：2026-07-11 在 develop 执行 make check 已全绿；唯一非失败提示为既有 Vite 单 chunk >500 kB warning。implement 开始仍须重跑轻量预检，后续红灯按 diff 归因。
- **必跑验证命令**：见 3.y。新增 customer-avatar 前端测试命令须接入 make test / make check；命令不存在时不得把手工截图当自动化替代。
- **交付物清单**：OpenAPI/生成物（含 customer status-set）、avatar_revision+五字段 pointer+按 object_id 唯一的 GC+双 cursor checkpoint migration、AvatarObjectStore local/fake、不可复用 generation key 与精确代次删除、MaintenanceRunner/双向 reconciliation、signal-aware server lifecycle、三条 HTTP 方法、媒体 client、CustomerAvatar/Picker 与 fixed summary 注入、require-mount 配置/volume attestation、exact-generation 备份恢复 manifest、测试/截图及 CodeStable reports。
- **清洁度规则**：禁止 fmt.Println / console.log 调试输出、临时 TODO/FIXME、注释掉代码、无用 import、真实客户图片 / PII、凭证与本机绝对存储路径入库。测试图必须为仓库内生成或明确无 PII 的微型 fixture；无例外。

## 2. 名词与编排

### 2.1 名词层

**现状**：

- backend/internal/customer/model.go 的 Customer 无头像字段；repository.go 的 customerColumns / scanCustomer 只读取现有档案列，customers 表也无头像元数据。
- backend/internal/customer/service.go 的 Repository 与 Service 只覆盖客户档案、身份、备注和 merge；没有对象存储 port 或头像编排。
- api/openapi.yaml 的 Customer / CustomerListItem / CustomerDetail 均无 avatar_url，运行时与生成代码也没有 roadmap 已预告的 PUT / DELETE avatar。
- backend/internal/platform/config/config.go 仅加载数据库、认证、seed 与 HTTP 配置；docker-compose.yml 只有 pgdata volume；README 备份只覆盖 pg_dump。
- frontend/src/api/client.ts 的 request helper 固定 application/json 并只解析 JSON；无法上传 multipart 或读取二进制。
- CustomersPage 与 CustomerDetailPage 用 display_name[0] 渲染首字；CustomerNewPage / OrderWorkspace / ShootOrderFlow 用纯文本 select，CustomerProfileForm / MergeDialog 仍手填客户 ID。现有 .avatar CSS 与 avatar-layout test 只保证色块不被 flex 拉伸。

**变化**：

| 名词 | 动作 + 动机 |
|---|---|
| Customer.avatar_revision / avatar_version / avatar_url | avatar_revision 是始终存在的只读 `ar-{非负 bigint}` 写 token；avatar_version/avatar_url 成对缺省，存在时 URL 精确携带当前 checksum version；写 CAS 与媒体缓存不复用同一字段 |
| CustomerSummary 头像投影 | 保留既有 id/display_name/channel/status，只新增 avatar_revision/avatar_version/avatar_url；同 Customer 使用始终 revision + 可选 version/url 规则，referrer 等既有非 active selectedCustomer 可直接显示真实头像 |
| AvatarMetadata | customers 新增独立非负 avatar_revision（初始 0）及 avatar_version / avatar_object_id / avatar_media_type / avatar_size / avatar_updated_at 五字段 pointer；五个 pointer 字段全空表示无头像，存在时全非空、object_id 为 32 位小写 hex 且 size>0，由 CHECK 约束；不存路径 / bucket / 签名 URL |
| AvatarContent | 新增规范化内容值：≤5 MiB 的只读可重放精确字节 / media type / size / checksum；同一应用操作的 storage-timeout 重试必须重读相同字节，限制与去元数据规则由 customer 计算层保证；READ service 完整验证后也以该值把 validated bytes + metadata 交给 HTTP adapter |
| ObjectRef / ObjectMeta / ObjectStream | ObjectRef 是内部 `{avatar_version, avatar_object_id}` 物理代次引用；store-neutral 元数据含 media type / size / checksum；Open 返回可关闭内容流，每个直接 application caller（鉴权 READ、same-content 校验、pointer audit）都负责所有路径 close，HTTP handler 不直接调用 store |
| AvatarObjectStore | 新增 PutImmutable / Open / Stat / List / Delete port；local filesystem 为 production adapter，in-memory 为测试 adapter，未来 OSS 实现同一语义 |
| avatar_object_gc / avatar_reconciliation_checkpoint | 前者按 `(account_id, customer_id, avatar_object_id)` 唯一，保存 avatar_version、pending/not_before/retry；重复 enqueue 不推迟 due；后者分别保存每账号 object inventory cursor/cycle 与 current-pointer customer-id cursor/cycle，runner 驱动双向核对 |
| CustomerAvatar | 新增共享展示组件，统一鉴权 Blob、avatar_revision 驱动的同 URL 重取、fallback、object URL 回收、尺寸与 a11y |
| CustomerPicker | 新增共享客户选择组件，统一头像 + 昵称 + 短 UID + 状态、搜索 / 分页 / 过滤与 UI 状态 |
| 本地头像持久卷 | 新增可配置挂载点；production 以 require-mount + Linux mount table attestation 证明不是容器层，binary 轨可显式关闭 |

**接口示例**：

~~~http
PUT /api/v1/customers/cus_a/avatar
Authorization: Bearer ...
If-Match: "ar-0"
Content-Type: multipart/form-data
file=<portrait.jpg>

→ 200 Customer {
  "id": "cus_a",
  "display_name": "阿茶",
  "avatar_revision": "ar-1",
  "avatar_version": "sha256-0123...cdef",
  "avatar_url": "/api/v1/customers/cus_a/avatar/content?v=sha256-0123...cdef"
}

file 为 SVG / GIF / HEIC、伪造 MIME、损坏图片、>5 MiB 或宽高 >4096
→ 400 { "error": { "code": "validation_failed", ... } }

merged 客户 PUT（即使内容与 current 相同）
→ 409 { "error": { "code": "customer_merged", ... } }

merged 客户 DELETE + 当前 avatar_revision
→ 200 Customer（只清头像 pointer；其他档案字段仍只读，旧 ObjectRef 进入 GC）

当前 revision 已是 ar-3，但请求 If-Match 仍是 ar-1，且上传内容不是当前完整字节
→ 409 { "error": { "code": "avatar_revision_conflict", ... } }
// 来源：roadmap §4.3 customer avatar
~~~

~~~http
GET /api/v1/customers/cus_a/avatar/content?v=sha256-0123...cdef
Authorization: Bearer ...
If-None-Match: "sha256-0123...cdef"
→ 304（无 body）

未命中时 → 200 image/*
ETag: "sha256-0123...cdef"
Cache-Control: private, no-cache
Vary: Authorization
X-Content-Type-Options: nosniff

v 缺失 / 格式错 → 400 validation_failed
无头像 → 404 not_found
v 不是当前 pointer → 409 avatar_version_stale（不得返回当前新字节）
跨账号 / 不存在客户 → 404 not_found
无 token / token 失效 → 401 unauthorized
数据库宣称有头像但对象缺失或 checksum/metadata 不符 → 500 internal，并写结构化 integrity 日志
// 来源：roadmap §4.1 / §4.3 / §4.3a
~~~

~~~text
CustomerAvatarApplication
  ReadContent(ctx, accountScope, customerId, requestedVersion) -> AvatarContent

AvatarObjectStore
  PutImmutable(ctx, objectKey, replayableBody, expectedMeta) -> PutResult{meta, created}
  Open(ctx, objectKey) -> ObjectStream
  Stat(ctx, objectKey) -> ObjectMeta
  List(ctx, prefix, cursor, limit) -> ObjectPage{items, next_cursor, done}
  Delete(ctx, objectKey) -> error

ObjectMeta = {media_type, size, checksum, modified_at}
PutResult = {meta, created}
ObjectItem = {key, meta}
ObjectRef = {avatar_version, avatar_object_id}
objectKey = avatars/{account_id}/customers/{customer_id}/{avatar_version}/{avatar_object_id}
replayableBody = 最终规范化的只读精确字节（≤5 MiB），caller 持有，adapter 不保留/不关闭
约束：ReadContent 在 customer application service 内完成 pointer/v/实际字节完整性验证，返回 ≤5 MiB AvatarContent；HTTP handler 只据此适配 200/304/binary/error，不直接调用 store；objectKey 由应用从 AccountScope、Customer 与 ObjectRef 派生；object_id 每次发布随机生成且离开 current 后永不复用；同一 key 已存在且完整性一致时 created=false，只有同一请求的结果未知内部重试可接受，fresh ID 首次调用遇到 false 必换 ID；Delete 缺对象成功，且成功返回后同 adapter Stat(key) 必须稳定 not-found，不能提前确认异步删除；每个 Open caller 均负责 close；
错误分类至少含 ObjectNotFound / InvalidObjectKey / IntegrityMismatch / Temporary / internal I/O；
任何 adapter 错误都不带本地绝对路径或 OSS 凭证进入公开错误封套。
// 来源：roadmap §4.3a
~~~

~~~tsx
<CustomerAvatar
  customerId={customer.id}
  displayName={customer.display_name}
  avatarRevision={customer.avatar_revision}
  avatarUrl={customer.avatar_url}
  size="md"
  decorative
/>

<CustomerPicker
  value={customerId}
  candidateStatuses={['active']}
  selectedCustomer={selectedCustomer}
  excludeCustomerIds={[currentCustomerId]}
  onChange={(choice) => setCustomerId(choice?.id ?? '')}
  required
/>
// 来源：frontend/src/pages/CustomersPage.tsx、CustomerNewPage.tsx、
// frontend/src/components/customers/CustomerProfileForm.tsx、MergeDialog.tsx、
// frontend/src/components/orders/OrderWorkspace.tsx、
// frontend/src/components/schedule/ShootOrderFlow.tsx
~~~

##### Interface 设计检查

- **Module**：customer avatar 应用能力 + AvatarObjectStore/MaintenanceRunner + CustomerAvatar / CustomerPicker 前端模块（均新增）。
- **Interface**：caller 必须知道——头像可选；avatar_revision 始终存在且 PUT/DELETE 的 If-Match 比较 revision，CustomerAvatar 也用它使同 URL repair 触发重取；avatar_url/v/ETag 比较内容 version；写入限制与状态矩阵；application ReadContent 返回完整性已验证的 ≤5 MiB AvatarContent，HTTP handler 不接触 store；内部 ObjectRef 包含 avatar_version+avatar_object_id，avatar_object_id 不下发且永不复用；进入 GC 的 generation 不得再晋升 current；PutImmutable 消费 caller-owned replayable exact bytes、不覆盖并用 `created` 区分首次发布/已存在完整对象；每个 Open caller 负责 close；List 有稳定顺序/opaque cursor/bounded limit；Delete 对精确 generation 幂等且成功后 Stat 必须稳定 not-found；CustomerPicker 保留服务端账号 / status-set / q / pagination 语义。
- **Seam**：handler→avatar application service；service→AvatarObjectStore + AccountScope.WithTxScope/repository；composition root→AvatarMaintenanceRunner；页面→CustomerAvatar / CustomerPicker。生产与测试都穿同一 seam。
- **Depth / locality**：图片验证、方向、规范化与 pointer 状态机藏在 customer 应用层；key 映射、durable immutable write、错误归一和 OSS I/O 藏在 port / adapter；Blob 生命周期和 fallback 藏在组件内。删除这些 module 会把存储和鉴权细节散到多个 handler / 页面，通过 deletion test。
- **Dependency strategy**：HTTP 为 remote-owned；PostgreSQL 与 local filesystem 为 local-substitutable；未来 OSS 为 true external。业务包不 import Gin、os.File 具体路径或 OSS SDK。
- **Adapter**：本轮 local + in-memory 两个真实 adapter；未来 OSS 是第三实现方向，不是假 seam。
- **Test surface**：A3-A14 经 service/HTTP/transaction/storage fault injection，含 revision ABA 与 same-content repair；A15-A20 经组件与浏览器；A23 经 runner fake clock/ticker + process signal integration 测 single-flight/retry/shutdown；local adapter 测 durable I/O/mount 与精确 generation 删除，transaction fake 测 revision+五字段 pointer/GC 原子更新；A11/A24 真实终止 PostgreSQL session 或重启 DB 后恢复历史/pre-current 旧 Delete，证明存储侧代次隔离；不依赖私有路径布局。

### 2.2 编排层

~~~mermaid
flowchart TD
    UI[客户详情头像操作] --> U{PUT 或 DELETE}
    U --> H[校验 quoted avatar_revision<br/>If-Match 存在与格式]
    H -->|PUT| PRE[AccountScope 非锁定确认 Customer]
    PRE --> N[限流、校验并规范化图片<br/>锁外计算 checksum]
    PRE -->|不存在/跨账号| E404
    N -->|非法| E400[400 validation_failed<br/>当前头像不变]
    N --> SNAP{非锁定快照<br/>desired checksum=current?}
    SNAP -->|是| RECHECK[短事务锁 Customer 重验]
    RECHECK -->|merged| E409
    RECHECK -->|active/archived 且仍相同| VERIFY[完整读取并验证<br/>当前物理 generation]
    VERIFY -->|完整| OK[提交 no-op 并返回 Customer]
    VERIFY -->|缺失/损坏且 revision 匹配| GEN[生成全新且不可复用<br/>avatar_object_id]
    VERIFY -->|缺失/损坏且 revision 失配| EVC[409 avatar_revision_conflict]
    RECHECK -->|active/archived 且已变化| GEN[生成全新且不可复用<br/>avatar_object_id]
    SNAP -->|否| GEN
    GEN --> P[PutImmutable<br/>version/object_id generation key]
    P -->|key 碰撞| RETRY
    P -->|存储失败| E500[500 internal<br/>DB pointer 不变]
    P --> L[DB 事务：按账号锁定 Customer<br/>与 merge 共用行锁]
    H -->|DELETE| L
    L -->|不存在/跨账号| E404[404 not_found]
    L --> OP{本次操作}
    OP -->|PUT| WS{Customer status<br/>是 merged?}
    WS -->|是| E409[409 customer_merged]
    WS -->|否，active/archived| Q{本次新 object_id<br/>存在任意 GC row?}
    OP -->|DELETE| D0{current=none?}
    D0 -->|是| OK
    D0 -->|否| DM{If-Match revision<br/>等于 current?}
    DM -->|否| EVC
    DM -->|是| DS[清空五字段 pointer<br/>revision +1，登记旧 ObjectRef GC]
    DS --> OK2[DB commit 后返回 Customer]
    Q -->|是| BURN[该 generation 已进入 GC<br/>不得晋升 current]
    BURN --> RETRY
    Q -->|否| ST[锁内最终 Stat/integrity]
    ST -->|generation 缺失| RETRY[回滚并弃用 object_id<br/>fresh ID 有界重发]
    RETRY --> GEN
    ST -->|完整性冲突| RETRY
    ST -->|完整| C{并发请求已让<br/>desired checksum=current?}
    C -->|是| CV[完整读取并验证<br/>并发 current generation]
    CV -->|完整| UNUSED[本次未引用 generation 入 GC]
    UNUSED --> OK
    CV -->|缺失/损坏| M{If-Match revision<br/>等于 current?}
    C -->|否| M
    M -->|否| EVC
    M -->|是| S[切换完整五字段 pointer<br/>revision +1，登记旧 ObjectRef GC]
    S --> OK2
    RUN[AvatarMaintenanceRunner<br/>立即一轮 + 每小时 single-flight] --> REC[bounded 双向 reconciliation]
    RUN --> GC[bounded due GC]
    REC --> INV[List object inventory<br/>+ DB pointer 对照]
    INV --> ENQ[orphan 只 upsert pending GC]
    REC --> PAUDIT[分页读取 current pointers]
    PAUDIT --> PVERIFY[Stat + Open<br/>完整验证实际字节]
    PVERIFY -->|missing/corrupt| REPORT[只记录 integrity<br/>不改 pointer / 不入 GC]
    PVERIFY -->|Temporary| NEXT[有限重试后记录并推进<br/>下一 cycle 再访]
    GC --> CLAIM[同一事务持 Customer→GC row<br/>复查 object_id 是否 current]
    CLAIM -->|是 current| CANCEL[只删 stale GC row<br/>绝不调用 storage Delete]
    CLAIM -->|不是 current| DEL[Delete 精确旧 generation key<br/>成功删 GC row并 commit]
    AV[CustomerAvatar] --> G[鉴权 GET content?v=current]
    G --> V{v 与 DB pointer 相等?}
    V -->|否| EVS[409 avatar_version_stale]
    V -->|是| O[Stat + Open<br/>完整缓冲并验证实际字节]
    O -->|校验后| IMG[再发送 200/304<br/>Blob URL 显示真实头像]
    O -->|缺失/损坏| E500
    G -->|无 URL/失败| FB[首 grapheme + 稳定色 fallback]
    CP[CustomerPicker] --> LC[GET customers<br/>status + q + pagination]
    LC --> ROW[头像 + 昵称 + 短 UID + 状态候选]
~~~

**现状**：

- 客户编排只有 JSON 档案 CRUD / merge；头像 PUT / GET / DELETE 全部 NoRoute 404。
- 客户展示直接计算首字色块；各选择 caller 自己加载候选并渲染纯文本，过滤 / 分页实现不一致。
- 部署启动不校验头像存储，容器重建只保证 PostgreSQL 数据。

**变化**：

1. **契约线**：roadmap 增量 → OpenAPI 始终存在的 avatar_revision + 可选 avatar_version/avatar_url、revision If-Match、v/If-None-Match、ETag/304、multipart/binary/errors/tag → 双端 codegen；既有 caller 不消费新增字段时向后兼容。
2. **编排骨架**：customer avatar service 先用 in-memory AvatarObjectStore 跑通 SET / READ / REMOVE 三分支，状态 / 账号 / 错误先可测试，图片计算节点暂为规范化 stub。
3. **计算线**：把 raw upload 转为受限 AvatarContent，统一大小、格式、方向、元数据、checksum；handler 不做领域校验。
4. **持久化线**：local adapter 执行 durable immutable generation I/O；PostgreSQL avatar_revision + 五字段 current pointer、按 object_id 唯一的 pending GC queue 与 checkpoint reconciliation 收口生命周期；进入 GC 的 generation 永不晋升，精确代次删除让 DB/session 丢失后的迟到 Delete 仍无害。
5. **HTTP 线**：三方法挂受保护 group；READ service 先产出 validated AvatarContent，handler 只在成功后适配 200/304 与 binary/JSON ErrorEnvelope；multipart client / media Blob client 不复用强制 JSON 的 request helper。
6. **展示线**：CustomerAvatar 替换散落首字实现；详情头像操作驱动 PUT / DELETE 后以返回 Customer 刷新版本。
7. **选择线**：CustomerPicker 替换可交互的转介绍、merge、订单、档期选择；订单/档期固定客户摘要单独注入 CustomerAvatar，不把只读摘要伪装成 picker。
8. **运维线**：production mount attestation、Docker volume、signal-aware server lifecycle、AvatarMaintenanceRunner、write-freeze 一致备份/恢复、exact-generation manifest 与容器重建验证成为 feature DoD。

**流程级约束**：

- **错误语义**：上传输入、缺失/非法 revision If-Match 或 v 为 400；无认证 401；DB 无头像 / 客户不存在 / 跨账号为 404；merged PUT、写 revision 失配、stale v 分别为 409 customer_merged/avatar_revision_conflict/avatar_version_stale；merged DELETE 是唯一 cleanup-only 例外，按普通 DELETE 的 revision/no-op 语义返回 200 或 conflict。DB pointer 有值但对象缺失/损坏时 GET 为 500 internal，不伪装客户未设置；same-content PUT 必须验证并在 CAS 允许时修复，不能对损坏对象假报 200。
- **事务与结果未知**：PUT 先写全新不可变 generation，再在事务内持 Customer 行锁以 revision CAS 切完整五字段 pointer并递增 revision；DB rollback 只留可被 inventory 发现并入队的 orphan，旧 current/revision 完好。单次请求的 storage timeout 可用同 object_id 重试，跨 HTTP 重试可生成新 object_id；若 desired checksum 已 current，只有完整读取验证当前物理 generation 后才 200。GC Delete 精确指向队列中的旧 ObjectRef；Delete success + commit unknown 下轮幂等重试并 finalize。
- **并发顺序**：同一 Customer 的 PUT / DELETE / merge 共享行锁；PUT 对新 generation 的 GC row 与 GC worker 都统一 `Customer row → GC row` 锁序。PUT/PUT、PUT/DELETE、PUT/merge 按先锁者线性化；merge 先赢时后续 PUT 409，而带匹配 revision 的 cleanup DELETE 仍可把 merged 当前头像清空；DELETE 先赢时 merge 只看到已清空 pointer。avatar_revision 关闭 A→B→A ABA。任何出现 GC row 的 pre-current generation 都被烧毁并换 fresh ID；GC 即使因 DB/session 丢失在事务结束后迟到，也只能删除不会再晋升的旧 key。GET 不返回半写对象或用旧 v 读新字节。
- **账号隔离**：先经 AccountScope 确认客户属于当前账号，再派生 object key；绝不以 URL id 直接拼路径读取。跨账号所有三方法均 404。
- **安全**：限制 request body 和 decoded dimensions；只接受真实可解码栅格；文件名被忽略；路径 root containment；响应 nosniff + 正确 Content-Type；日志不写图片字节、原文件名、路径、token 或 PII。
- **缓存**：v 是“只允许读取当前精确版本”的条件；旧 v 返回 409，绝不映射到新字节。即使 If-None-Match 命中，也先在 5 MiB 上限内完整读取并重算 media type/size/checksum，确认 pointer/ObjectMeta/实际字节一致后才发 quoted ETag + 304；未命中才发 200 body。响应带 private/no-cache、Vary Authorization、nosniff。CustomerAvatar 必须回收 object URL。
- **生命周期与恢复**：旧 current/DELETE 对象按 ObjectRef 首次入 pending GC，重复 enqueue 不改 not_before/retry metadata；GC transaction 发现 row 指向 current 时只删 stale row，不调用 storage Delete，非 current 才删除精确 generation。AvatarMaintenanceRunner 立即一轮、每小时 single-flight，每账号每 tick 各一页 object inventory/current-pointer audit + bounded due GC；双 checkpoint 防后页饥饿，GC 错误用 next_attempt_at 退避，signal-aware root context 负责取消和有界 shutdown。GET 与 pointer audit 都报告 pointer 缺失/损坏且不自动清理；合法 same-content PUT 可在 revision CAS 成功时以 fresh generation 修复。
- **可观测点**：结构化记录 operation、account/customer 内部 id、media type、normalized size、checksum 短前缀、duration、result；对象缺失、integrity mismatch、GC/reconciliation 计数与存储错误有可检索类别，不记录图片内容。
- **扩展点**：OSS 只实现 AvatarObjectStore 与配置装配；首轮继续由应用代理端点返回完整验证后的受限 bytes。若改预签名直读，需另起媒体交付 design，不得在 adapter 内偷偷让 avatar_url 变成外部 URL。

### 2.3 挂载点清单

| 挂载位置 | 具体落点 | 动作 |
|---|---|---|
| API 契约与受保护路由 | Customer.avatar_revision/avatar_version/avatar_url；revision If-Match、v/ETag/304；PUT / GET / DELETE；customer-avatar codegen tag | 新增 |
| 数据库 schema | customers avatar_revision + 五字段 current pointer + 按 account/customer/avatar_object_id 唯一的 avatar_object_gc + avatar_reconciliation_checkpoint migration | 修改 |
| 对象基础设施与运维 | AvatarObjectStore、driver/root/require-mount、volume attestation、signal-aware lifecycle、AvatarMaintenanceRunner、reconciliation、备份/恢复与 exact-generation manifest | 新增 |
| 前端公共 UI 注入 | 客户列表 / 详情头像；转介绍 / merge / 订单 / 档期 CustomerPicker；详情设置 / 替换 / 移除入口 | 修改 |

拔除以上四处并清理 pointer/GC/checkpoint schema、runner 装配与头像 volume，系统回到“客户仅有首字色块、客户选择纯文本、无头像写入/读取”的现状。AvatarObjectStore 内部文件、图片计算 helper 与 import 不单列为挂载点。

### 2.4 推进策略

1. **契约地基**：同步 OpenAPI、avatar_revision+五字段 pointer/按 account+customer+object_id 唯一的 GC/checkpoint schema 与 local require-mount config  
   退出信号：生成物一致；migration up/down 通过；revision 编码/递增、pointer 全空全非空、object_id 格式/唯一性约束与 direct-binary / production mount-attestation 两轨配置测试可执行。
2. **编排骨架**：用 in-memory store/transaction fake 跑通 revision CAS、新 generation 先落、进入 GC 即烧毁、五字段 pointer 后切、merge 共锁与 Customer→GC row 锁序  
   退出信号：service tests 证明 200/400/404/409/500、结果未知、A→B→A、same-content integrity repair、同 checksum 不同物理代次，以及 PUT/DELETE/merge 线性化；历史与 pre-current 旧代次 Delete 迟到均不能删除新 current。
3. **计算节点**：实现图片识别、边界、方向、规范化、去元数据与 checksum  
   退出信号：JPEG / PNG / WebP 正例和损坏、伪 MIME、超大小、超维度、禁用格式反例全部有确定测试结果。
4. **持久化节点**：接通 local store、精确 generation GC/双向 reconciliation、signal-aware lifecycle 与 AvatarMaintenanceRunner  
   退出信号：durable I/O、not_before 幂等、历史/pre-current DB session 丢失后旧 Delete 恢复、object/pointer 双 cursor、current integrity 主动报告、立即/小时 single-flight、生产 signal 取消/有界退出/失败退避矩阵通过。
5. **HTTP 垂直切片**：注册条件 multipart PUT、强版本 binary GET、条件 DELETE 与错误封套  
   退出信号：A3-A14 API 集成测试全绿；缓存 header/304/stale v、无 token / 跨账号与 integrity failure 语义正确；无匿名媒体路径。
6. **展示组件**：接入 CustomerAvatar 与详情上传 / 替换 / 移除  
   退出信号：列表 / 详情真实头像和 no-url / load-error fallback 可见；active/archived 三操作可完成，merged 仅在有头像时显示 cleanup-only 移除且不显示设置/替换，object URL 无泄漏。
7. **选择组件**：CustomerPicker 接入可交互候选，CustomerAvatar 接入订单/档期固定摘要  
   退出信号：A17/A18 的过滤、分页、选择与 FixedCustomer/FixedScheduleCustomer 头像 props/展示逐项通过。
8. **终验收口**：mount/volume/exact-generation 备份、输入 harden、UI polish、inventory/清洁度与全量回归  
   退出信号：A19-A22/A24 证据落盘；逐 current ObjectRef 恢复核验通过；所有无引用对象均已被 pending/reconciliation 追踪，注入时钟后到期可清零；允许 24h grace 内 pending，不允许 untracked orphan/stale temp。

### 2.5 结构健康度与微重构

compound 检索关键词“目录 / 命名 / 归属 / 组件 / OpenAPI / AccountScope / 存储”命中《AccountScope 隔离基座要 fail-loud》《OpenAPI 与 roadmap 契约双向核对》《OpenAPI 全量同步与 Go tag 切片》《前端工具链优先》。本 feature 直接遵守：头像 metadata 查询必须走 AccountScope；契约 / tag / 生成物三面同步；TS API 类型只用 schema.d.ts；无专门头像目录 convention。

##### 评估

- 文件级 — backend/internal/customer/repository.go：605 行，已偏胖；本轮只改 customer scan / columns 的小范围并把头像仓储写面落新文件，避免继续追加完整编排。纯拆现有 SQL helper 会扩大回归面。
- 文件级 — frontend/src/components/orders/OrderWorkspace.tsx：1152 行，明显偏胖；本轮只把既有客户 select 替换为共享 CustomerPicker，不在此文件新增加载 / 头像逻辑。安全拆分 OrderDialog 牵涉 schedule recovery 大量 props，不适合作为本 feature 前置。
- 文件级 — frontend/src/index.css：729 行；现有 .avatar 规则与 avatar-layout 门禁稳定。本轮新组件样式随组件放新样式文件，不搬旧规则，不扩大 index.css。
- 文件级 — backend/cmd/server/main.go：当前以 `context.Background()` + `ListenAndServe` 启动并把 graceful shutdown 留给 v1-hardening；文件不胖，但 D14 要求在本条做最小 signal-aware root context / HTTP shutdown / runner wait 装配。该变化是本 feature 的生产正确性，不扩成通用 server framework 重构。
- 文件级 — CustomersPage / CustomerDetailPage / CustomerNewPage / CustomerProfileForm / MergeDialog / ShootOrderFlow / api client / router 均小于 500 行；变化是装配或单一适配职责，新增逻辑优先落新文件。
- 目录级 — backend/internal/customer 当前 8 个文件，本轮若把 processor / store / tests 全部摊平会继续拥挤；图片计算落 customer 内聚子目录，local adapter 落 infrastructure 子目录，父 customer 只保留编排与 repository 扩展。
- 目录级 — backend/internal/platform/httpapi 当前 17 个文件但已有按业务 slice 成对命名的稳定模式；新增头像 handler + test 对保持该模式。重组整个 httpapi 会移动大量文件与生成物，不是本 feature 的“只搬不改”安全前置。
- 目录级 — frontend/src/components/customers 当前 5 个文件，新增 CustomerAvatar / CustomerPicker / 局部样式后仍是客户域内聚目录；未达到“现有 ≥8 且新增 ≥2”的摊平触发条件。

##### 结论：不做微重构

本次不做前置微重构。原因：新职责可通过新 module / 子目录承载；所有偏胖文件仅做局部装配，安全拆分收益不足以抵过客户、订单和档期回归风险。checklist 第 1 步直接进入契约地基。

##### 超出范围的观察

- frontend/src/components/orders/OrderWorkspace.tsx：订单列表、表单、状态推进、排期恢复集中在 1152 行；建议后续走 cs-refactor 做有 characterization tests 的组件拆分，本 feature 只替换客户选择注入点。
- backend/internal/customer/repository.go 与 backend/internal/platform/httpapi 的平铺增长已出现信号；等再有一个 customer 子能力或 httpapi 域接入时建议走 cs-refactor 评估仓储分片 / handler 子目录，本 feature 不搬现有文件。

## 3. 验收契约

### 关键场景清单

| # | 输入 / 触发 | 期望可观察结果 | 证据类型 |
|---|---|---|---|
| A1 | 实现前后执行 make check | build + lint + 全部 test + generate-check 退出码 0；既有 Vite chunk warning 单独记录不算失败 | command |
| A2 | OpenAPI / codegen / migration / config 检查；调用 GET customers 的 status=active、all、active,archived、active,active、unknown、all,active | Customer/CustomerListItem/CustomerSummary 的 revision/version/url 投影一致，CustomerSummary 保留既有 id/display_name/channel/status，只新增头像字段；avatar_revision 编码为 ar-{非负 bigint}，revision 初始 0、pointer 变更原子 +1、no-op 不增；五字段 pointer 全空/全非空且 object_id 格式受约束；status 单值/all 向后兼容、合法集合在分页前过滤，重复/unknown/all混用 400；GC 对 `(account_id, customer_id, avatar_object_id)` 唯一并保存 version；checkpoint 含 object/pointer 两套 cursor/cycle且 migration up/down；未知 driver/缺 root/require-mount 不可验证均 fail-fast | command + API + diff + test |
| A3 | 分别为三个无头像、avatar_revision=ar-0 的 active 客户以 `If-Match: "ar-0"` 上传合法 JPEG、PNG、WebP | 各返回 200 Customer + revision=ar-1 + 精确 version/url；鉴权 GET 返回可解码规范化图片与规定 headers | API + integration |
| A4 | 同一客户用原 revision If-Match 重放当前内容；再从 A 替换为 B，随后用 B 的 revision 重新上传与历史 A 完全相同的内容 | 当前同内容经实际 generation 完整验证后直接 200、不新建对象且 revision 不变；不同内容生成全新 object_id/key、切完整 pointer并 revision +1；再次发布 A 时公开 version 可与历史 A 相同，但 object_id/key 与 revision 必须全新，历史 A generation 的 GC 不影响新 A | test + filesystem evidence |
| A5 | 当前 v 分别不带/带匹配 If-None-Match 请求 GET；再请求缺失 v、旧 v | 首次 200 带 quoted ETag/private,no-cache/Vary/nosniff；匹配时也先验证实际字节再 304 无 body；缺 v 400；旧 v 409 avatar_version_stale | API + cache test |
| A6 | 上传伪 MIME、损坏/空文件、SVG/GIF/HEIC、>5 MiB、宽或高 4097；PUT/DELETE 缺失或格式非法 If-Match | 全部 400 validation_failed；未调用对象写/删，原 pointer / URL / 对象内容不变 | test |
| A7 | 带 EXIF 方向与元数据的大尺寸合法图，同一构建重复处理同一 fixture | 输出方向正确、最长边 ≤512、无原 EXIF/动画/时间随机 metadata；重复输出字节与 checksum 完全相同；原始文件不落持久卷 | fixture test |
| A8 | local PutImmutable 遇中途写失败、同一 generation key 同内容结果未知重试、fresh ID 首次调用却命中已存在同内容 key、同 key metadata/字节冲突；另用相同 checksum+不同 object_id 发布；模拟 provider Delete 先返回接受但 Stat 暂时仍可见；客户端在 GET copy 中断 | 无半成品 final；首次发布 created=true，结果未知内部重试 created=false 且完整性一致后收敛，fresh ID 首次 created=false 必换新 ID，冲突报 integrity mismatch；相同 checksum 的不同 generation 各自存在且互不覆盖；Delete 在 Stat 稳定 not-found 前不得成功，超时为 Temporary；content/meta 与目录 durable publish 顺序可注入验证；流被取消并 close；只清自有超时 temp | fault-injection + filesystem |
| A9 | 对已有头像执行：新 generation 成功后 DB rollback；storage timeout 但实际成功；DB commit unknown 分别模拟已提交/已回滚，再重放原 PUT | 旧 revision+五字段 pointer 在 rollback 时保持；遗留 generation 可被 inventory 入队；单次请求以同 object_id 重试核完整性，跨 HTTP 重试允许新 object_id；已提交时 desired=current 验完整性后 200 且 revision 不再增加，已回滚时以新 generation 安全切换并只增一次 revision | fault-injection |
| A10 | 同一初始 revision 并发 PUT/PUT、PUT/DELETE、PUT/merge、DELETE/merge；再构造 A/rev1→B/rev2→A/rev3，让仍携 rev1 的 PUT different-content 与 DELETE 晚到 | 每组按 Customer 行锁线性化；至多一个不同 desired CAS 成功，loser 409 avatar_revision_conflict；same desired 完整时可 200 no-op；ABA 后 rev1 的状态变更均 409，不能覆盖/清空 rev3；merge 先赢则 PUT 409，但匹配 revision 的 cleanup DELETE 可清 merged pointer；DELETE 先赢则 merge 看到已清空 pointer，均无对象错配 | concurrency integration |
| A11 | 旧 generation A 已 pending、current=B；GC 锁 Customer+GC row 并把 A 的 Delete 暂停在存储 adapter 内；终止该 PostgreSQL session 或重启 DB，再上传与 A 相同 checksum 的内容形成新 object_id/current，最后恢复旧 Delete | 旧事务回滚或失去 session 后，上传可用全新 object_id/key 成功切回公开 version A；恢复的旧 Delete 只能删除旧 generation，新 current 对象仍存在，鉴权 GET 完整性校验通过并返回 200；不存在依赖 DB lock 生命周期的正确性假设 | concurrency + DB restart + storage fault injection |
| A12 | active / archived / merged 三状态执行 PUT / DELETE / GET，并在 merge 前后制造竞态；对 merged 客户上传新内容与当前完全相同内容，再以匹配/失配 revision cleanup DELETE | active/archived PUT/DELETE 200；merged 的新内容与 same-content PUT 都先返回 409 customer_merged；merged GET 可读既有头像，匹配 revision 的 DELETE 清 pointer、revision +1 并入 GC，失配 409，已无头像重放 200 no-op；merge 不自动迁移/继承/删除 source/target 头像且与写入线性化 | API + concurrency |
| A13 | 账号 B 对账号 A 客户执行 PUT / GET / DELETE；无 token 执行三方法；构造路径逃逸文件名/id/key | 跨账号均 404，无 token 均 401；请求输入不影响 root 外文件；客户端不能传 object key；无匿名媒体路径 | security test |
| A14 | DB 明确无头像；pointer 对象缺失；sidecar/ObjectMeta 正常但实际内容字节、size、media type 或 checksum 损坏，并分别带/不带匹配 If-None-Match；随后以匹配/失配 revision 上传与 pointer checksum 相同的合法图片 | 无头像 GET 404；损坏 GET 在发送任何 200/304 header/body 前返回 500 + integrity 日志；reconciliation 不清 pointer；same-content PUT 不得假报 no-op，匹配 revision 时以 fresh generation 修复并 revision +1，失配时 409；修复后 GET 200，UI 失败期间 fallback | test + log |
| A15 | 列表/详情有图、无图、失败；同 URL 两消费者中一个先卸载；同 URL 的 avatar_revision 改变（模拟 same-content repair）；候选滚入视口、logout/401 | 真实头像或 grapheme fallback 正确；离屏不取图、同 auth+URL+revision 在途只一请求；单消费者卸载不取消另一方，最后离开才 abort/revoke；revision 改变释放旧状态并重取；认证结束全部清理且不跨 token 复用 | browser + component test |
| A16 | 详情页设置、替换、移除；快速重复点击；模拟 409 revision conflict；archived / merged 有图与无图页面 | 客户端始终用当前 avatar_revision 组 If-Match；成功刷新 revision+pointer；冲突重拉并提示；有 loading/error/disabled；archived 可执行三操作；merged 有图时只显示带明确清理含义的“移除头像”，不显示设置/替换，成功后变 fallback 且入口消失；merged 无图无写入口 | browser |
| A17 | 客户新建 referral、档案编辑 referral、merge source；让带真实头像的既有 referrer 在保存前变为 archived/merged；分别尝试原样保留、直接清除、改走其他 channel 后清除；构造第 1 个 raw page 全被 exclude、有效候选只在第 2 页 | 新候选在选择前显示头像/fallback、昵称、短 UID、状态；服务端按 q+active 状态过滤后分页，组件再按 exclude 过滤并在空可见页继续取第 2 个 raw page；CustomerSummary 直接携头像投影，既有非 active referrer 作为带真实头像的 pinned selected 可显示并原样保留，保存其他字段不重写该历史指针，且不混入新候选；直接清除时 required 阻止保存，只有同请求把 channel 改为非 referral 才允许由服务端联动清空 referrer | browser + component test |
| A18 | 订单新建、未来/历史档期可交互选择，并各构造 active/archived/merged 跨页候选；OrderWorkspace.FixedCustomer 与 FixedScheduleCustomer 固定摘要 | 订单与未来档期只返回 active；历史档期返回 active+archived；merged 永不候选；q+status-set 服务端过滤后稳定分页，不能只筛当前页；固定摘要扩展 id/display_name/avatar_revision/avatar_url props并用 CustomerAvatar 显示头像/fallback，不渲染不可交互 CustomerPicker | browser + regression |
| A19 | CustomerPicker loading / empty / error / 401、键盘 / focus、长昵称、375px | 状态可见；401 清 token 并回登录；键盘可选择/关闭，focus 可见；长文本与窄屏不溢出 | browser screenshot |
| A20 | CustomerAvatar 在 1280 / 390 视口与详情 profile-head | 头像保持正圆且 object-fit cover，不复发 277×52 拉伸；avatar-layout 回归门禁继续通过 | browser + test |
| A21 | require-mount=true 的同一容器 env 分别无卷/挂 named volume 启动并 recreate；direct binary 以 `AVATAR_LOCAL_REQUIRE_MOUNT=false` 启动；停 app 后备份 DB+volume+exact-generation manifest 并恢复，并故意用同 checksum 的错误 object_id 替换一次；再对已进入该历史备份的头像执行 cleanup DELETE | 无卷自动启动失败、挂卷成功且 recreate 保留；binary 轨显式目录可用；manifest 逐 current 记录 account/customer/version/object_id/key/media/size/actual SHA-256，错误 object_id 恢复必须失败，精确 inventory 全符才开放写；PII 权限/访问/加密符合说明；cleanup 后在线 pointer/活动存储按规则清理，但旧备份仍可能含该头像，恢复前须按 retention/销毁说明评估且 UI 不宣称跨备份彻底擦除 | command + integration + manual |
| A22 | grep / diff / dependency / inventory review | 无 OSS SDK/匿名路由/建档头像/裁剪相册/导出二进制/UI 库/调试 TODO/PII fixture；order/schedule done 不回退；无 stale temp 或 untracked orphan，24h 内 pending GC 允许存在且可核验 | scope + cleanliness |
| A23 | 重复 enqueue/reconciliation，并让一次 object 扫描与 PUT pointer 切换竞态、current generation 被暂时误入 GC；分别构造 3 页 object inventory 与 3 页 current pointers，后者含 missing、实际字节损坏和一个永久 Temporary 的首项；再让立即 tick、重叠 tick、跨小时 tick、完整 cycle、进程重启与 OS shutdown signal 发生 | 重复 enqueue 保留最早 not_before 与 retry metadata；GC 复核发现 row 指向 current 时只删 row、不调用 storage Delete；每账号每 tick 最多各一页 object/pointer +100 due；双 cursor 跨 tick/restart 续扫且后页不饥饿；missing/损坏记录 integrity 后前进且不改 pointer，永久 Temporary 有限重试后记录并推进、后页仍审计且下一 cycle 再访；所有 Open 流关闭；生产 root context 接收 signal，HTTP graceful shutdown、在途 I/O 取消与 runner 有界退出可观察；注入时钟到期后 pending 可清零 | runner + restart + process integration |
| A24 | PUT 已发布但尚未写 pointer 的 generation X 被 reconciliation 入队并到期；GC 对 X 的 Delete 暂停后终止其 PostgreSQL session，再恢复原 PUT，最后恢复旧 Delete | 原 PUT 看到 X 存在任意 GC row 后不得取消或晋升 X，必须烧毁 X、发布 fresh Y并只把 Y 切为 current；迟到 Delete(X) 完成后 Y 仍存在且鉴权 GET 200；bounded fresh-ID 重试耗尽时返回 500且旧 current/revision 不变 | concurrency + DB restart + storage fault injection |

### 明确不做的反向核对项

| 不做项 | 核对方式 |
|---|---|
| 建档上传 / 强制头像 | POST /customers body 无 avatar 字段；CustomerNewPage 无 file input |
| OSS adapter / 迁移 / 预签名直读 | go.mod / config / 代码无 OSS SDK 与 OSS_*；avatar_url 仍是同源 /api/v1 路径 |
| 裁剪、人脸、相册、原图 | 无 crop / face / gallery / original asset 模块与路由；local root 只含规范化不可变对象、adapter metadata 与受控 temp，不保留上传原图 |
| GIF / SVG / HEIC 与元数据保留 | 反例测试 400；输出 metadata 检查无 EXIF / animation |
| 匿名媒体与 token query | 所有媒体读取在 auth group；URL / 日志 / HTML 无 token= |
| merge 自动迁移/继承/删除头像 | merge diff 本身不更新 avatar pointer / storage；A12 证明只有合并后的显式 cleanup DELETE 才清 source 头像 |
| JSON export 内嵌头像 | /export shape 与 application/json 保持；无 base64 / data URL |
| UI 组件库 | frontend package dependencies 无新增 UI library |

### 3.x Acceptance Coverage Matrix

| Scenario | Covered By Step | Evidence Type | Command / Action | Core? |
|---|---|---|---|---|
| A1 基线 | S8 | command | CMD-001 | yes |
| A2 契约 / schema / config | S1 | command + diff + test | CMD-002 / CMD-003 | yes |
| A3-A7 上传、版本读取、缓存、规范化 | S3/S5 | unit + integration + API | CMD-003 | yes |
| A8-A11 durable store、结果未知、revision CAS、历史代次 GC | S2/S4 | fault-injection + integration | backend avatar tests | yes |
| A12-A14 状态、隔离、integrity | S2/S4/S5 | API + security + log | CMD-003 | yes |
| A15-A16 展示与头像操作 | S6 | component + browser | CMD-004 + desktop/375px | yes |
| A17-A18 全客户选择面 | S7 | component + browser regression | CMD-004 + 路径演示 | yes |
| A19-A20 UI polish / 布局 | S7/S8 | browser + existing regression | CMD-004 / CMD-005 | yes |
| A21 持久卷 / exact-generation 备份恢复 | S8 | command + manual + manifest | CMD-006 + recreate / restore | yes |
| A22 范围 / 清洁度 | S8 | grep + dependency + diff review | review actions | yes |
| A23 runner / reconciliation / restart | S4/S8 | fault-injection + restart | backend avatar tests | yes |
| A24 pre-current generation burn | S2/S4 | DB restart + delayed Delete | backend avatar tests | yes |

### 3.y DoD Contract

| ID | 要求 | 证据 | 阻塞级别 |
|---|---|---|---|
| DOD-DESIGN-001 | design + checklist 通过独立 design review 且 owner 确认 | design-review / owner confirmation | blocking |
| DOD-IMPL-001 | checklist 8 steps 全 done，每步有命令 / 测试 / UI / 运维证据 | checklist / implementation evidence | blocking |
| DOD-REVIEW-001 | code review passed；重点复核 avatar_revision CAS、不可复用 object_id/五字段 pointer、pre-current burn、精确代次 GC、transaction owner、signal-aware runner 装配、mount attestation、账号隔离与图片输入 | review report | blocking |
| DOD-QA-001 | QA 覆盖 A1-A24，含 ABA、same-content integrity repair、历史/pre-current DB session 丢失后的迟到 Delete、runner/process shutdown、无卷启动、跨账号、浏览器与 exact-generation 备份恢复 | QA report / evidence | blocking |
| DOD-ACCEPT-001 | acceptance 反查交付物、清洁度、roadmap / requirement 状态与架构沉淀候选 | acceptance report | blocking |

Validation Commands:

| ID | 命令 | 目的 | 核心性 | 失败处理 |
|---|---|---|---|---|
| CMD-001 | make check | 全仓 build / lint / test / codegen 漂移 | core | fix-or-block |
| CMD-002 | make generate && git diff --exit-code -- backend/internal/platform/httpapi/api.gen.go frontend/src/api/schema.d.ts | OpenAPI 双端生成物一致 | core | fix-or-block |
| CMD-003 | cd backend && go test ./... | 图片计算、port、local adapter、customer / HTTP / 隔离 / 故障集成 | core | fix-or-block |
| CMD-004 | cd frontend && npm run build && npm run test:customer-avatar | TS 构建、媒体 client、fallback / picker / caller 回归 | core | fix-or-block |
| CMD-005 | cd frontend && npm run test:avatar-layout | 详情头像正圆布局回归 | supporting | fix-or-block |
| CMD-006 | docker compose config | app 头像 volume 与环境装配可解析 | supporting | fix-or-block |

Required Artifacts: roadmap/design/implementation review、QA、acceptance；revision ABA/API concurrency/same-content repair/DB-session-loss/history+pre-current exact-generation-delete/runner process fault-injection；无卷失败+挂卷 recreate；reconciliation checkpoint/inventory；桌面/375px 截图；停 app backup/restore+exact-generation manifest；diff/dependency/scope summary。

## 4. 与项目级架构文档的关系

- **roadmap §3 / §4.1 / §4.2 / §4.3 / §4.3a**：本 design 启动时已按 owner 的 local-first / OSS-later 决策补齐 AvatarObjectStore 与鉴权媒体契约；OpenAPI 机器形式由本 feature 落地。
- **requirement customer-profile**：头像是集中档案的识别增强，沿用现有用户故事与边界；acceptance 时把 2026-07-11-customer-avatar 追加到 implemented_by 并复核 requirement 仍为 current。
- **CONTEXT.md**：Customer / Account 术语不变；“客户头像”若后续被其他模块复用再评估是否加入领域词表，本轮组件名不单独写术语。
- **ADR-001 / ADR-003**：头像 pointer、GC 与对象 key 均有账号维度；handler 只做 multipart / binary 适配，图片与存储规则不下穿 Gin。code review 按 AccountScope fail-loud 核对。
- **ADR-002 调和**：不修改 ADR-002 对 PostgreSQL 的既有选择；PostgreSQL 仍是 Customer 与当前头像 pointer 的 system of record，local/OSS 是二进制 durable adjunct。acceptance 建议走 cs-domain 补充 ADR，记录 AvatarObjectStore、不可变对象、pointer/GC 与 local→OSS 演进边界，避免未来把 SDK 接进 handler。
- **compound 候选**：content version + write revision + immutable generation + 五字段 DB pointer + “进入 GC 即不再晋升” + exact-generation pending GC 处理 ABA、跨 store 结果未知与迟到删除的模式若验证有效，可在 acceptance 走 cs-keep；未实测前不归档。
- **data-export 决策 gate**：本 feature 不把鉴权 avatar_url 当作可携带二进制资产；data-export design 启动前 owner 必须拍板 reference-only JSON，或先 update roadmap §4.6 为媒体包 + exact-generation manifest/key/count/checksum，未决不得推进该条。
