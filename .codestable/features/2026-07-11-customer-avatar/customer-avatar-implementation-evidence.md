# customer-avatar 实现证据

## 基线预检

- 初次 `make check`：前端 build、Go build、Go/前端 lint 通过；Docker daemon 卡死导致所有 Testcontainers 测试在功能代码改动前失败。
- 经 owner 明确允许后执行 `docker desktop restart`；Docker Client/Server 29.2.1、API 1.53 恢复。
- 恢复后重跑 `make check`：全绿；唯一提示为 design 已记录的 Vite 单 chunk >500 kB warning。

## Step 1：契约地基

- 退出信号：生成物一致；migration up/down 通过；revision 编码、原子递增/no-op 不增、customer status 单值/all 兼容及合法集合分页前过滤、重复/unknown/all 混用 400、object_id 格式/唯一性、全空全非空 pointer 与双 cursor 约束固定；未知 driver/缺 root/require-mount 无真实 mount fail-fast，direct-binary 可用。
- TDD RED：
  - `go test ./internal/customer ./internal/platform/config` 首次因头像配置字段/错误与 status-set 行为尚不存在而编译失败。
  - `go test ./internal/platform/store -run TestCustomerAvatarMigrationUpDownAndConstraints -count=1` 首次证明 PostgreSQL CHECK 的 NULL 三值逻辑允许 partial pointer。
- GREEN / 验证动作：
  - `go test ./internal/platform/config ./internal/customer`：通过。
  - `go test ./internal/platform/store -run TestCustomerAvatarMigrationUpDownAndConstraints -count=1`：补显式全非空约束后通过。
  - `go test ./internal/customer -run 'TestListFiltersAndPagination|TestNormalizeListFilterStatusSet|TestBuildCustomerFilterUsesStatusSetBeforePagination' -count=1`：通过。
  - `go test ./internal/platform/httpapi -run TestCustomerProfileAPIArchiveListFilter -count=1`：通过，覆盖 `ar-0` 投影、单值/all/status-set 与非法组合 400。
  - `go test ./...`：通过。
  - `npm run build`：通过；仅既有 chunk warning。
  - `docker compose config`：通过，`avatar_data` 挂载与 production require-mount 配置可解析。
  - 连续 `make generate` 前后 Go/TS 生成物 SHA-1 相同：无 codegen 漂移。
- 失败恢复：第一次 migration 失败根因为 SQL `CHECK` 的 UNKNOWN 结果也通过；仅修改 0008 pointer 完整性约束，补 `IS NOT NULL` 后原测试转绿。一次组合验证命令因从仓库根使用了错误的 gofmt 相对路径立即失败，未改变代码；改正命令路径后通过。
- 影响面：OpenAPI/customer-avatar tag 与三端点、Customer/CustomerSummary 头像投影、0008 migration、customer status-set、local 配置与 compose volume、Go/TS 生成物。
- 清洁度：`git diff --check` 通过；新增 diff 未发现 debug 输出、TODO/FIXME/XXX、注释掉代码、无用 import、凭证或方案外实现文件。

## Step 2：编排骨架

- 退出信号：无嵌套事务；typed errors、结果未知、A→B→A、same-content 完整性修复、PUT/DELETE/merge 线性化；merged PUT 恒 409 语义而匹配 revision 的 DELETE 只清 pointer，历史与 pre-current generation 的迟到 Delete 不能删除新 current。
- TDD RED：`go test ./internal/customer -run TestAvatarApplication -count=1` 首次因 `AvatarApplication`、AvatarObjectStore port、ObjectRef/ObjectMeta 与 revision conflict 尚不存在而编译失败。
- GREEN / 验证动作：
  - `go test ./internal/customer -run TestAvatarApplicationSetNoOpReplaceAndABA -count=1 -timeout=30s -v`：通过。
  - `go test ./internal/customer -run TestAvatarApplicationMergedCleanupOnly -count=1 -timeout=30s -v`：通过。
  - `go test ./internal/customer -run TestAvatarApplication -count=1 -timeout=60s`：8 个编排行为测试全绿，覆盖 revision CAS、same-content no-op、损坏修复、ABA、merged cleanup、pre-current burn、结果未知和 merge-first。
  - `go test ./...`：全绿。
  - `golangci-lint run ./...`：`0 issues`。
- 失败恢复：首次组合执行长时间无输出，拆为带超时/详细日志的单测后确认无死锁，容器及场景各约 2 秒完成；全仓 lint 首次发现 3 个 Close 返回值未检查，限定修复 config/store test 的关闭处理后复跑为 0 issues。
- 影响面：新增 customer avatar application、AvatarObjectStore port、PostgresAvatarRepository、不可复用随机 object_id/key、Customer→GC row 锁序、pointer CAS 与 cleanup-only Remove；对象 fake 仅在测试文件中。
- 代码质量反射检查：`publishAndSwitch` 较长，已触发反射核对；它只表达 design 2.2 的单一“发布新 generation 后在事务内决策是否切 pointer”编排，Put 重试、current 完整性验证、SQL 写面均已拆到独立 helper/Repository，继续拆会割裂事务分支且没有新增职责，因此不追加微重构 step。
- 清洁度：`git diff --check` 通过；新增 diff 未发现 debug 输出、TODO/FIXME/XXX、注释掉代码、无用 import、凭证或方案外文件。

## Step 3：图片计算节点

- 退出信号：合法 JPEG/PNG/WebP 与损坏、伪 MIME、空、禁用格式、>5 MiB、4097 边界 fixture 都得到 A3/A6/A7 规定结果。
- Context7 证据：解析并查询 `/disintegration/imaging` 与 `/golang/image` 当前文档；采用 `imaging.Decode(..., AutoOrientation(true))`、`imaging.Resize(..., Lanczos)`、固定 PNG compression，以及 blank import `golang.org/x/image/webp` 注册 WebP decoder。第一次 pkg.go.dev 聚合文档查询超时，按 skill 回退到同库官方 GitHub 文档源后成功返回 API 示例。
- TDD RED：`go test ./internal/customer/avatarimage -count=1` 首次因 `NewProcessor` 尚不存在而编译失败。
- GREEN / 验证动作：
  - `go test ./internal/customer/avatarimage -count=1`：通过；覆盖三格式、EXIF orientation 6、最长边、确定性 bytes/checksum、metadata 移除和全部反例。
  - `go mod tidy && go test ./... && golangci-lint run ./...`：全绿，lint `0 issues`。
- 影响面：新增 `backend/internal/customer/avatarimage` 内聚子目录；新增 `github.com/disintegration/imaging v1.6.2` 与 `golang.org/x/image v0.44.0` 直接依赖；统一输出 metadata-free PNG，不引入 WebP encoder、C 库、原图持久化或裁剪能力。
- 清洁度：fixture 来自两项依赖各自 testdata，并在本地 README 标明 BSD-3-Clause/MIT 来源；无 PII；无 debug/TODO/FIXME、注释掉代码或方案外格式支持。

## Step 4：持久化节点

- 退出信号：durable local I/O、精确 generation GC、双 cursor reconciliation、current integrity 报告、每账号各一页+100 due、single-flight、signal cancellation/有界退出、退避及历史/pre-current DB session loss 隔离成立。
- TDD RED：`go test ./internal/customer/avatarstore -count=1` 首次因 `NewLocal` 尚不存在而编译失败。
- GREEN / 验证动作：
  - `go test ./internal/customer/avatarstore -count=1`：通过；覆盖 immutable Put/replay/conflict、实际 checksum Stat、稳定 cursor List、路径逃逸、幂等 Delete+稳定 not-found。
  - `go test ./internal/customer -run TestAvatarMaintenanceReconcilesAndDeletesOnlyExactOldGeneration -count=1`：旧 generation 到期删除、误入 GC 的 current 只删队列行且对象保留。
  - `go test ./internal/customer -run TestAvatarMaintenanceDBSessionLossCannotDeleteRepublishedChecksum -count=1 -timeout=90s -v`：真实终止 GC 的 PostgreSQL session，随后同 checksum 用新 object_id 发布；恢复的旧 Delete 只删除旧 key，新 current 存活。
  - `go test ./internal/customer -run TestAvatarMaintenancePreCurrentGenerationBurnSurvivesDBSessionLoss -count=1 -timeout=90s -v`：PUT 已发布未切 pointer 的 X 被 reconciliation 入队，GC session 被终止后原 PUT 看到 GC row 并烧毁 X、发布 Y；迟到 Delete(X) 不伤 Y。
  - `go test ./... && golangci-lint run ./...`：全绿，lint `0 issues`。
  - `go build ./cmd/server` 与 `docker compose config`：通过。
- 影响面：新增 local durable adapter、AccountScopes 服务端枚举、GC/checkpoint repository、MaintenanceRunner；server 使用 signal.NotifyContext、HTTP graceful shutdown 和 runner 有界等待；镜像为 app 账号准备头像卷目录。
- 清洁度：local adapter 错误不暴露 root 路径到业务封套；启动清理仅处理超过一小时的自有 `.avatar-tmp-*`；无 debug/TODO/FIXME、凭证或方案外 provider。

## Step 5：HTTP 垂直切片

- 退出信号：API 覆盖条件 PUT/DELETE、强版本 GET、完整性先于 200/304、typed errors、缓存 header、跨账号/无 token；handler 不直接调用 store。
- 验证动作：
  - `go test ./internal/platform/httpapi -run TestCustomerAvatarHTTP -count=1 -timeout=90s`：通过；覆盖 multipart PNG、`If-Match`、`ar-0→ar-1→ar-2`、same-content no-op、different-content conflict、v/ETag/304/private,no-cache/Vary/nosniff、stale v、删除后 404、无认证、跨账号、缺 header、伪 MIME。
  - `go test ./internal/customer -run 'TestAvatarApplication|TestAvatarMaintenance' -count=1 -timeout=120s`：通过；READ 完整性与编排/GC 回归全绿。
  - `golangci-lint run ./...`：`0 issues`。
- 失败恢复：全仓首次复跑时，READ 新增真实图片解码校验使 Step 2 用任意字符串模拟 PNG/WebP 的 fake fixture 被判为损坏并进入 CAS；生产校验保持不变，只把测试 helper 改为按种子生成真实确定性 PNG，原测试与 HTTP 测试复跑通过。
- 影响面：新增 application `ReadContent`；受保护 group 挂三路由；薄 handler 只做 multipart/header/status 适配；server composition 注入 AvatarApplication 与 Processor。
- 清洁度：文件名不进入领域/日志；body 有 5 MiB 限制；无匿名媒体路由、token query、store 直调、debug/TODO/FIXME 或方案外错误码。

## Step 6：展示组件

- 退出信号：CustomerAvatar 对真实图/no-url/load-error/emoji fallback、同 URL revision 重取、引用计数、详情条件写 UI 和 375px 布局均有证据。
- 自动化验证：
  - `npm run test:customer-avatar`：3/3 通过；emoji ZWJ/组合字符 grapheme、同 key 两消费者在途去重、单消费者 release 不 revoke、最后消费者 revoke、revision/auth generation 隔离通过。
  - `npm run test:avatar-layout`：通过。
  - `npm run build && npm run lint`：通过；仅既有 chunk warning。
- 浏览器证据（localhost:18080）：
  - 新建显示名 `👩‍🎨 小茶` 的客户，无头像时 DOM/a11y 与视觉均显示完整 `👩‍🎨` grapheme，按钮为“设置头像”。
  - 通过本地 API 上传非 PII EXIF orientation fixture 后刷新：真实 `<img>` 可见，入口变为“替换头像 / 移除头像”。
  - 375×812：document `clientWidth=scrollWidth=375`，头像 rect `52×52`，无横向溢出且保持正圆/object-fit cover。
  - 实际点击“移除头像”后出现“客户头像已移除”，真实图回到 grapheme fallback，入口恢复“设置头像”。
- 失败恢复：首次 Node 测试因原生 ESM 不解析 extensionless `.ts` import 失败；只补媒体模块两个 `.ts` 后缀后原链全绿。首次本地 server 因未知进程占用 8080 失败，未终止该进程，改用隔离端口 18080 验证。
- 影响面：CustomerAvatar/媒体缓存、token generation 订阅、列表/详情注入、上传/替换/移除 API client 与详情条件 UI；merged 只在有图时显示“移除头像（隐私清理）”。
- 清洁度：object URL 最后引用释放才 revoke；logout/401 统一 abort/clear；无 token query、页面级 Blob 复制、UI 库、debug/TODO/FIXME。

## Step 7：选择组件与调用方接入

- 退出信号：referral/merge/订单/档期候选遵守 active-only、active+archived、merged-never、exclude 空页续取与 pinned 非 active 当前值；订单/档期固定客户摘要显示 CustomerAvatar 且不渲染 picker。
- 自动化验证：
  - `npm run test:customer-avatar`：6/6 通过；新增覆盖 `status=active`、`status=active,archived`、merged 排除、首个 raw page 20 项全部 excluded 后继续第 2 页，以及不在当前候选中的 archived selectedCustomer 仍可 pinned 显示。
  - `npm run test:schedule`：32/32 通过，既有订单/档期恢复与状态同步逻辑无回归。
  - `npm run test:avatar-layout`：通过。
  - `npm run build && npm run lint`：通过；仅既有 Vite chunk warning。
- 浏览器证据（localhost:18080）：
  - 客户详情的新建订单与新建拍摄档期均显示固定客户的 grapheme 头像和名称，只读摘要中没有 CustomerPicker。
  - 全局订单 CustomerPicker 展示头像、名称、短 UID 与 active 状态；Enter 键从展开列表完成选择，输入框保留所选客户。
  - 30 秒建档把来源切为“客户介绍”后，介绍人搜索通过服务端 q 返回 active 候选；合并对话框搜索当前客户时返回“暂无匹配客户”，证明 exclude 生效。
  - 全局未来拍摄档期搜索只显示 active 候选；active+archived 参数矩阵由定向测试锁定。
  - 375×812 新建订单对话框：`clientWidth=scrollWidth=375`、dialog 335 px、picker 287 px，候选展开时无横向溢出并完成截图复核。
- 失败恢复：一次浏览器导航后立即定位在页面尚未稳定时超时，刷新 DOM 后原入口存在且正常；一次截图在默认 1280 视口超时，按 viewport capability 重置并重新应用 375×812 后，以新页签得到准确移动端指标与截图。未修改生产代码绕过浏览器时序。
- 影响面：新增 CustomerPicker/纯查询模型与样式；替换建档介绍人、档案介绍人、合并来源、订单和档期的原生客户输入；FixedCustomer/FixedScheduleCustomer 增加 avatar revision/url 并显示统一头像摘要。
- 清洁度：移除由 picker 取代的客户全量预加载 state/helper/import；401 callback 用 ref 避免 inline callback 导致重复加载；无调试输出、TODO/FIXME、注释掉代码、无用 import、UI 库或方案外 caller 改动。

## Step 8：终验收口与运维恢复

- 退出信号：A19-A22/A24 证据落盘；require-mount/volume/recreate、停 app DB+volume+exact-generation manifest 备份恢复、错误 object_id 拒绝、PII 边界和 inventory 清洁度成立；全量验证通过。
- mount / volume / exact-generation 实证：
  - `docker run` 不挂卷、`AVATAR_LOCAL_REQUIRE_MOUNT=true`：exit 1，启动日志为“AVATAR_LOCAL_ROOT 不是可验证的独立挂载点”。
  - production image 以 named volume 启动并健康；上传规范化头像后记录 object_id=`97e93b6763b7829f512f337af8301196`、actual SHA-256=`efae…b2990`；删除并 recreate app 容器后两者完全相同。
  - 停 app 后备份 PostgreSQL、完整 avatar volume 与 manifest；manifest current 逐项包含 account/customer/version/object_id/derived key/media/size/actual SHA-256，inventory count=1 且含物理 key 与汇总 SHA-256。
  - 真实 drop/create/restore 数据库并清空/恢复 volume 后，`avatar-manifest verify` 通过；app 恢复后鉴权 GET 字节 SHA-256 与 `avatar_version` 一致。
  - 故意把 DB pointer 和物理目录同时替换为相同 checksum、不同 object_id=`2222…2222`，verify exit 1 并报 `current exact-generation manifest mismatch`；恢复正确备份后再次通过。
  - 对已进入历史备份的头像执行在线 DELETE：revision `ar-3→ar-4`、pointer 为空、对象进入 pending GC；历史 manifest 仍保留 current=1/inventory=1，证明在线清理不追溯擦除历史备份。
- inventory / 输入 / 路径 harden：
  - local `Inventory` 校验完整 content+metadata、canonical key、实际 size/checksum，稳定排序，并拒绝残缺 generation、未知文件和 `.avatar-tmp-*`。
  - 配置启动增加 create+chmod+fsync+remove+directory fsync 写权限探针；direct-binary false 轨和 production mount true 轨均通过。
  - 在线 DELETE 后停 app 生成 inventory：current=0、physical=1；该物理 exact key 在 `avatar_object_gc` pending 中精确存在，无 untracked orphan/stale temp。24h grace 内 pending 符合设计；注入时钟到期清零已有 maintenance 自动化测试覆盖。
- 文档：README 给出停 app 的 DB/volume/manifest 备份、破坏性恢复、verify-before-start、权限/加密/访问控制、retention/销毁说明；明确 cleanup-only DELETE 不删除历史备份，恢复旧备份可能重新带回头像 PII。
- 全量验证：
  - `go test ./...`：全绿；customer 约 40s、HTTP API 约 22s；`golangci-lint run ./...` 为 `0 issues`。
  - `npm ci && npm run build && npm run test:customer-avatar && npm run test:avatar-layout && npm run test:schedule && npm run lint`：全部通过；仅既有 Vite chunk warning。
  - `make generate` 前后 Go/TS 生成物 SHA-1 完全相同。
  - 使用临时独立 `GIT_INDEX_FILE` 仅登记本轮预期 codegen 文件后运行 `make check`：build、Go/前端 lint、全仓测试、generate-check 全绿；真实 git index 未修改。
  - `docker compose config`、`git diff --check`：通过。
- 失败恢复：
  - production image 第一次 build 在干净 `npm ci` 发现 lockfile 缺少镜像内 npm 11.16 所需的两个 `@emnapi` optional peer；限定用同一 `node:24-alpine` 执行 package-lock-only 刷新，第二次干净 build 通过，无依赖选型变化。
  - 本机 8080 被 worktree 外既有 server 占用；未终止该进程，使用临时 compose override 映射 18081 完成验证，随后删除临时 `.env`/override 并停止测试 app。
  - 两次验证脚本问题（zsh 保留变量 `status`、误用会等待容器停止的 `docker compose wait`）均在无代码变更下立即纠正；使用 `exit_code` 与有限 health poll 后通过。
- 影响面：新增 `avatar-manifest` 停机工具与 `avatarbackup` manifest 逻辑；local adapter 增 exact inventory；Docker image 携带核验二进制；README 更新生产 volume/备份恢复说明；package-lock 修复干净容器安装一致性。
- 清洁度 / 反向范围：新增 diff 无 debug/TODO/FIXME/XXX、注释掉代码、真实 PII fixture、凭证或本机绝对存储路径；无 OSS SDK/env、预签名/匿名媒体路由、建档头像、裁剪/相册/原图/GIF/SVG/HEIC、JSON 二进制导出或 UI 库；订单/档期既有回归全绿。

## Review-fix：REV-001 MaintenanceRunner pointer audit

- Review finding：round 1 独立 reviewer 判定 pointer audit 对 Temporary/internal I/O 只尝试一次即推进 cursor，且 A23 的永久 Temporary、多页双 cursor、重叠 tick、restart、100 due 与 backoff 缺少自动化证据；verdict=`changes-requested`。
- 修复范围：仅修改 `avatar_maintenance.go` 的 pointer audit 与 `avatar_maintenance_test.go`，未处理 round 1 的 important/nit，也未改变 cadence、page size、100 due、pointer 不自动清除或 GC 决策。
- 修复行为：每个 current pointer 在同 tick 最多进行 3 次完整 `verifyCurrent`；成功/完整性失败立即结束；Temporary/internal I/O 耗尽后记录 `avatar current pointer audit temporary` + `attempts=3`，推进 cursor；context cancellation/deadline 立即返回且不推进。
- 新增证据：
  - `TestAvatarMaintenancePointerAuditRetriesTemporaryAndRevisitsNextCycle`：永久 Temporary 每 tick 恰好 3 次；audit-temporary 日志分类/attempts 正确；后项仍被审计；cursor 完成 cycle 后归零，下一完整 cycle 再次访问并累计到 6 次。
  - `TestAvatarMaintenanceA23ThreePageRestartSingleFlightDueLimitAndBackoff`：201 个 physical/current 使 object/pointer 双 cursor 各跨 3 页；第三页由新 runner 接续 checkpoint 并完成 cycle；重叠 RunOnce 不再次枚举账号；101 个 due 中单 tick 最多 claim 100，一个 Delete 失败回滚并单独写 attempts=1/next_attempt_at backoff，最终保留失败项+第 101 项两行。
- 验证：两项定向测试通过（约 9s）；`go test ./...` 全绿（customer 约 56s、HTTP API 约 27s）；`golangci-lint run ./...` 为 `0 issues`；临时独立 index 下 `make check` 与 `git diff --check` 全绿。
- 下一门禁：必须由独立 reviewer 重跑 `cs-code-review`；复审通过前不进入 QA。

## QA-fix：QA-F001～QA-F004

- 修复范围：严格限定 `.codestable/features/2026-07-11-customer-avatar/customer-avatar-qa.md` 的四个 failed items；未修改 design/checklist、公开 API、对象 key、GC/HTTP 语义、前端或 residual risks。
- 第一性原则：只改变 direct-binary 缺省配置、local online 路径/错误安全边界和 server runner bounded wait；production Compose 仍显式 `require-mount=true`，`AvatarObjectStore` 接口不变。

### QA-F001：direct-binary require-mount 缺省 false

- RED：新增 `TestLoadAvatarStorageDirectBinaryDefaultsRequireMountToFalse`；`go test ./internal/platform/config -run TestLoadAvatarStorageDirectBinaryDefaultsRequireMountToFalse -count=1` 真实失败，错误为 `AVATAR_LOCAL_REQUIRE_MOUNT 必须是 true 或 false`。
- GREEN：`parseRequiredBool` 仅对空值返回 false；非法非空值继续 fail-fast，显式 true/false 不变。
- VERIFY：`go test ./internal/platform/config -count=1` 通过；真实 unset server 进程已越过 config 进入预期的不可达测试 DB 连接错误，非法 `sometimes` 仍在 config 阶段失败。

### QA-F002：local online intermediate symlink

- RED：新增 `TestLocalStoreRejectsIntermediateSymlinksForOnlineOperations`；对 `avatars`、account、`customers`、customer、version、generation 六层分别运行 Put/Open/Stat/List/Delete，原实现全部未返回 `ErrAvatarObjectKey`，Delete 还能触及 root 外 generation。
- GREEN：`safeJoin` 对每个已存在的受控路径 component 使用 `Lstat` 拒绝 symlink；List 同时拒绝扫描树内遇到的 symlink，并保持 `ErrAvatarObjectKey` 分类。没有改变公开 store port 或逻辑 key。
- VERIFY：同一矩阵全部通过，并逐项断言 root 外 sentinel generation 保持完整；完整 `go test ./internal/customer/avatarstore -count=1` 通过。

### QA-F004：local temporary error 不泄露 root

- RED：新增 `TestLocalStoreTemporaryErrorsDoNotExposeRootPath`，通过 public `Stat` 制造真实 `*os.PathError`；原错误包含完整临时 root 路径。
- GREEN：`temporaryError` 只保留 `ErrAvatarObjectTemporary` 分类与安全 operation 名称，不传播底层错误文本；context/integrity/key/not-found 分类路径不变。
- VERIFY：测试断言 `errors.Is(err, ErrAvatarObjectTemporary)` 且错误字符串不含 root；avatarstore 全包和 golangci-lint 通过。MaintenanceRunner 记录的同一 error 因此不再携带 local root。

### QA-F003：listener error 与 signal 两条 runner bounded wait

- RED：新增 server 包测试 `TestWaitForRunnerUsesCallerDeadline` / `TestWaitForRunnerReturnsWhenRunnerStops`；首次因 bounded wait 行为不存在而编译失败。
- GREEN：抽取最小 `waitForRunner(ctx, runnerDone)`；listener error 与 OS signal 两条分支都使用各自 10 秒 shutdown context，deadline 返回可 `errors.Is(..., context.DeadlineExceeded)` 的 timeout，runner 正常结束立即返回。
- VERIFY：`go test ./cmd/server -run TestWaitForRunner -count=1` 通过；server 包从无测试变为两项 lifecycle 测试。

### QA-fix 全量验证与清洁度

- `go test ./internal/platform/config ./internal/customer/avatarstore ./cmd/server -count=1`：全部通过；定向 golangci-lint 为 `0 issues`。
- `go test ./... -count=1`：全绿；customer 45.174s、avatarstore 2.923s、config 2.610s、httpapi 26.222s、store 20.983s。
- `golangci-lint run ./...`：`0 issues`。
- 临时独立 `GIT_INDEX_FILE` 下 `make check`：build、Go/前端 lint、全仓测试与 generate-check 全绿；真实 index 未修改。仅保留既有 Vite 537.17 kB chunk warning。
- 清洁度：qa-fix 仅修改 `config.go/config_test.go`、`avatarstore/local.go/local_test.go`、`cmd/server/main.go`、README 并新增 `cmd/server/main_test.go`；README 同步 direct-binary 缺省 false，属于 QA-F001 同一边界；无 debug/TODO/FIXME、注释掉代码、无用 import、凭证或方案外文件。
- 下一门禁：qa-fix 改变了代码 diff，必须重跑独立 `cs-code-review`；review passed 后再运行 `cs-feat` QA round 2，不能直接进入 acceptance。

## Review-fix：REV-008 / REV-009

- Round 3 独立 review：`status=changes-requested`。Task agent 构造出两个 qa-fix 证据缺口：固定物理叶子 symlink 未纳入拒绝不变量；server 测试只测 helper、删掉生产 wiring 仍会绿。主 agent 依据 QA-F003 的显式“可注入/进程级测试”退出条件，把后者从 reviewer important 升为 blocking。
- 修复范围：只修改 `avatarstore/local.go/local_test.go` 与 `cmd/server/main.go/main_test.go`；不处理 round 3 nit/suggestion/residual，也不改公开 store API、object key、HTTP/GC 语义、前端、design/checklist。

### REV-008：固定叶子 no-follow + regular-file

- RED：`TestLocalStoreRejectsGenerationLeafSymlinks` 先发布 root 与 root 外两个都完整有效的同 generation，再分别把 root 内 `content` / `metadata.json` 换成指向外部对应叶子的 symlink；旧实现的 Put/Open/Stat/Delete/Inventory 10 个子场景全部未返回 `ErrAvatarObjectKey`。
- GREEN：`objectDir` 在六层 directory `safeJoin` 后统一验证两个固定叶子；已存在叶子必须是非 symlink regular file，symlink 返回 `ErrAvatarObjectKey`，其他非 regular 返回 integrity。Put/Open/Stat/Delete/Inventory 由既有 `objectDir`/Stat 路径复用同一不变量。
- VERIFY：10 个 fixed-leaf 子场景全绿，并逐项断言 root 外 leaf bytes、generation metadata/content 与目录保持不变；原六层 directory × Put/Open/Stat/List/Delete 矩阵、临时错误脱敏测试同时通过。
- 失败恢复：新 regular-file 不变量使 QA-F004 原“metadata 建成目录”fixture 更早正确返回 integrity；只把 fixture 改为先发布合法 generation、再 chmod metadata 触发真实 permission `PathError`，生产逻辑未为测试绕过。

### REV-009：可注入 lifecycle wiring 证据

- RED：新增 `TestServerLifecycleBoundsListenerErrorWhenRunnerDoesNotStop` 与 `TestServerLifecycleBoundsSignalShutdownWhenRunnerDoesNotStop`；首次因 `serverLifecycle` 不存在而编译失败。
- GREEN：把 `run` 的现有 select 收口为窄 `serverLifecycle.wait`，只注入 timeout、runner cancel、HTTP Shutdown、server result 与 runner done；生产仍用单一 10 秒预算。
- VERIFY：listener 立即失败 + runner 永不结束时，测试断言 cancel runner 且在 caller deadline 内返回；root context 已取消 + runner 永不结束时，测试另断言实际调用 HTTP Shutdown。删除任一 production branch wiring 都会使对应测试失败；原 `waitForRunner` deadline/normal-done 测试保留。

### Review-fix 验证

- `go test ./internal/platform/config ./internal/customer/avatarstore ./cmd/server -count=1`：通过。
- `go test -race ./internal/platform/config ./internal/customer/avatarstore ./cmd/server -count=1`：通过。
- `go test ./... -count=1`：全绿；customer 48.538s、avatarstore 2.036s、config 2.209s、httpapi 28.901s、store 24.109s。
- `golangci-lint run ./...`：`0 issues`。
- 临时独立 index 下 `make check`：build、Go/前端 lint、全仓测试、generate-check 全绿；真实 git index 未改。仅既有 Vite 537.17 kB chunk warning。
- `git diff --check` 与 qa-fix/review-fix debug/TODO/FIXME 扫描：通过。
- 下一门禁：必须重跑 `cs-code-review` round 4；passed 后才可进入 QA round 2。
