---
doc_type: feature-design
feature: 2026-07-22-v1-hardening
roadmap: photographer-private-crm
roadmap_item: v1-hardening
status: approved
summary: 收口首版页面状态、375px 移动轻路径、生产运维安全网与全链路回归证据
tags: [v1, hardening, mobile, operations, regression]
---

# v1-hardening 方案

## 0. 术语约定

| 术语 | 定义 | 防冲突结论 |
|---|---|---|
| 移动轻路径 | 在 375 CSS px 竖屏视口内完成“查档期、搜客户、记备注”三项高频动作；只承诺轻量读取和备注写入，不把全部桌面编辑能力搬到移动端 | 沿用 roadmap 与 schedule-calendar requirement 的叫法，不称“移动端完整功能” |
| 页面读取状态 | 页面级或主要数据区的 loading、empty、error、ready、unauthorized 五类互斥基础状态；ready 再区分 current 与 stale-refresh-error | 不等于订单、提醒等领域状态；refresh 失败不是与 ready 并列的第六种基础状态 |
| 可恢复错误 | 用户仍可在原上下文重试，输入和已确认数据不会被静默丢弃的失败 | 不把 401 算作可恢复错误；401 清 token 后回登录 |
| 生产预检 | 读取部署环境配置并 fail-fast 的只读检查；只报告 key 与结论，不输出 secret 值 | 不等于运行时健康检查，也不替代 ECS、防火墙、TLS 或备份监控 |
| 一致备份包 | 同一冻结窗口内取得的 PostgreSQL dump、完整头像卷归档、exact-generation manifest、校验和与不含凭证的元数据 | 数据导出的 reference-only JSON 不是备份包 |
| V1 回归清单 | 带稳定场景 ID、前置条件、操作、期望结果和证据位置的首版验收清单 | 不等于本 feature 的 checklist.yaml 执行步骤 |

术语核对来源：.codestable/requirements/CONTEXT.md、roadmap §3/§4/§5、schedule-calendar requirement、customer-avatar ADR-004 与既有 feature 报告。没有新增领域实体或公开 API 名词。

## 1. 决策与约束

### 1.1 需求摘要

本 feature 为摄影师 owner 收口首版，不增加新的经营域能力。它交付四件事：

1. 清扫共享 AppShell 与 App.tsx 十个正式 screen route 的初始加载、空结果、读取失败、重试与 401 表达，避免把错误伪装成空态或把旧数据伪装成当前成功结果。
2. 在 375 CSS px 下保证查档期、搜客户、记备注三条轻路径无需桌面专属控件、无需整页横向滚动即可完成。
3. 把生产配置预检、数据库与头像卷一致备份/恢复、凭证操作和 HTTP 空闲连接 hardening 变成可执行、可失败关闭的运维入口。
4. 落一份可复跑的 V1 回归清单，并用真实 HTTP/PostgreSQL/头像卷和浏览器证据完成首版全链路核对。

成功标准：

- 375 CSS px 下三条移动轻路径均有成功、空态/边界和失败恢复证据。
- 正式顶层路由的页面读取状态矩阵逐项核对，不出现“请求失败但显示暂无数据”、无 retry 的初始读取错误或未处理 401。
- 一致备份包可在隔离 compose 项目中备份、破坏性恢复并通过 exact-generation manifest 与业务 counts 核对。
- README 成为使用与运维入口，部署、备份、恢复、凭证、Telegram、数据导出边界与仓库事实一致。
- V1 回归清单逐项有 pass/fail/blocked 与证据，make check 全绿且真实执行本 feature 的定向前端回归。
- roadmap 最终链路由 owner 在真机浏览器按“建档→套系→订单→档期→标定金→次日摘要→dashboard 五卡有数”逐节点核对；data export 作为额外节点，不替代任何原完成信号。

### 1.2 明确不做

1. 不新增或改变 HTTP API、OpenAPI shape、数据库 schema、领域状态机或账号隔离语义。
2. 不把移动端升级为完整 CRUD：档期创建/编辑/删除、复杂订单/套系管理继续是桌面路径；移动只保证查档期、搜客户、记备注。
3. 不在未获 owner 决策时重做认证：不新增登录限速/失败锁定、不从 localStorage Bearer 改为 HttpOnly cookie、不改变 JWT 30 天 TTL/吊销模型、不增加改密 API。这些会改变公开认证或运维契约；由于 platform-skeleton 曾明确把这些风险移交给 v1-hardening，本 design 的整体确认必须同时拍板“当前 V1 接受 residual，并落一个独立 auth-hardening 后续入口”或“退回本 design 扩范围”，不能静默丢弃。
4. 不在应用内终止 TLS、不选定反向代理或云防火墙产品；README 明确生产必须在 TLS 与网络边界之后运行，但证书、域名、ECS 安全组和定时任务由部署环境负责。
5. 不改变 owner 已接受的 compose PostgreSQL 端口策略；生产暴露面必须由 README 的防火墙/SSH/VPN 要求约束。
6. 不处理 Telegram 入站失败安全提示、Vite 500 kB chunk、idempotency 物理清理、各域 N+1、头像文件系统 TOCTOU/双 runner 等与本完成信号无直接关系的 residual。
7. 不引入 UI 组件库、全局数据获取框架、完整 WCAG 审计、视觉品牌重做、PWA/原生 App。
8. 不把 reference-only JSON 描述成可跨部署恢复的媒体备份，也不把线上头像删除描述成能追溯删除历史备份。

### 1.3 复杂度档位与方案深度

本项目是长期运行的生产单体，默认走 L3 + modules/layers + reasonable + team + stable + logged + tested + validated。

偏离项：

- Security = hardened：生产预检与 restore 是凭证敏感/破坏性边界；必须 fail-closed、最小输出、显式确认、拒绝开发占位值。
- Testability = verified：备份/恢复的精确代次、失败关闭和三条核心移动路径必须有真实系统证据，不能只靠静态 review。
- Compatibility = backward-compatible：不改 API/schema/路由 URL；桌面现有能力与历史验收不能回退。
- Performance = reasonable：保持现有数据规模与交互预算；既有 555.35 kB chunk 警告没有性能预算或已观察失败，本条不以“顺手拆包”扩范围。

方案深度 pre-pass：

- 三条轻路径与备份/恢复是用户直接依赖、长期维护且错误代价高的能力，必须走真实 API、PostgreSQL、头像卷和浏览器，不用 prototype/mock 冒充完成。
- 外部 ECS、TLS、BotFather 与异地介质可用 synthetic/隔离环境替代，因为它们是环境边界；替身只证明编排，真实生产动作仍由 owner 决定。
- 错误分支允许用可控 fake command 验证 docker/pg_dump/tar 失败，但成功闭环必须至少在隔离 compose 栈真实运行一次。

### 1.4 基线与历史债分流

2026-07-22 启动基线：

- 当前分支 develop；工作区只有与本 feature 无关的未跟踪 .workflow/ 与 install-cpamp.sh，本轮不触碰。
- make check fresh run 退出码 0：Go build/lint/串行 Testcontainers、前端 build/lint/tests、OpenAPI 双端生成漂移检查均通过。
- Vite 报既有单 chunk 555.35 kB 警告；记录为非阻塞性能候选。
- 本机 Testcontainers 必须沿用 make check 的 -p=1、-parallel=1，避免已知 mapped port 偶发失败。

历史候选债按以下方式处理：

| 历史事实 | 本 feature 处理 |
|---|---|
| server 已有 ReadHeaderTimeout，但没有 IdleTimeout | 纳入：固定 60 秒 IdleTimeout 并做 server construction 测试；不新增可能截断导出/上传的 ReadTimeout/WriteTimeout |
| graceful shutdown 曾留给 v1-hardening | 已由 customer-avatar 提前完成；只做回归，不重复实现 |
| README 备份曾是手工建议，脚本化留给 v1-hardening | 纳入：生产预检、backup、restore 三个稳定 CLI 入口与隔离恢复演练 |
| 启动错误可能输出头像根目录等部署路径 | 纳入：启动错误保留分类/key/操作，去除 secret 与原始本机路径 |
| CustomerPicker 空结果 ArrowDown、稳定 active descendant、Tab/blur 证据不足 | 纳入支持性 UI hardening，与移动/键盘状态矩阵同轮收口 |
| 登录限速、明文 HTTP、30 天 JWT、localStorage token | TLS/网络/轮换写入本轮运维说明；其余作为 owner checkpoint H1，只有 owner 明确接受 residual 并授权持久化到独立 auth-hardening 后续入口，本轮才可继续；否则扩当前 design 并重审 |
| Telegram 入站失败提示 | 已由 owner 明确延后为单独设计决策，不并入 |
| 目标 ECS 文件系统 fsync/rename durability | 纳入 owner attestation：记录生产 app 启动时 mount/write/fsync/dir-sync probe 通过与恢复演练事实；断电级 durability 仍保留为环境 residual，不用本机 synthetic 结果冒充 |

### 1.5 关键决策

#### D1. 页面读取状态使用共享“语义外壳”，数据生命周期仍归页面

候选比较：

| 候选 | 优点 | 缺点 | 结论 |
|---|---|---|---|
| 每页继续手写 loading/empty/error | 改动最少 | role、retry、旧数据和文案继续漂移 | 拒绝 |
| 共享 StateNotice，只负责呈现与 retry 事件 | 统一语义，页面仍掌握请求/数据，迁移可分片 | 需要各页显式接入 | 采用 |
| 引入全局 fetch hook/store 状态机 | 统一度最高 | 会重写既有页面数据流并扩大回归面 | 拒绝 |

页面状态采用可判别模型，禁止用多个互不约束的 boolean 拼出矛盾状态：

    PageReadState<T> =
      | loading
      | empty
      | error(message, retryable, retry?)
      | ready(data, freshness=current)
      | ready(data, freshness=stale, refreshError, retry)
      | unauthorized

五个基础 kind 互斥；`refresh-error` 是 `ready` 的 stale 子态，旧数据与错误提示可以同时可见，但不得被标成 current，也不得再渲染 empty。首次读取失败进入 error；可恢复网络/5xx 错误必须带 retry，404/明确无效链接等 terminal error 不伪造 retry。401 先由 API client 清 token，再由 RequireAuth/页面回登录；业务页不渲染 unauthorized notice。

StateNotice 不发请求、不清 token、不保存业务数据。它只有四种呈现 variant：

| variant | 必填 props | 可观察语义 |
|---|---|---|
| loading | message | `role=status`，所属数据区 `aria-busy=true`；不显示 empty/ready |
| empty | message | 仅在成功读取且集合为空时显示；搜索页必须带过滤上下文 |
| error | message；retryable 时 `onRetry` 必填 | `role=alert`；terminal error 不显示无效重试 |
| refresh-error | message + onRetry | `role=alert`，文案明确“显示上次成功数据”；与 stale ready 内容同层，禁止盖成空态 |

渲染优先级固定为 `unauthorized redirect → initial loading → initial error → empty → ready(current|stale)`；StateNotice 只承担 loading/empty/error/refresh-error 的语义外壳，ready 数据仍由页面渲染。

#### D2. 客户列表同一查询结果做桌面表格与移动卡片双投影

CustomersPage 继续是唯一查询与筛选状态所有者。桌面保留表格；不超过 768 CSS px 时显示可纵向浏览的客户卡片，至少直接展示头像/昵称、渠道、状态、最近拍摄和“记备注”。两种投影复用同一 items/total/loading/error，不发第二次请求。375、768、769 与桌面视口使用同一 fixture 做边界断言：375/768 只显示卡片，769/桌面只显示表格，内容与动作等价且网络层仍只有一次列表请求。

移动“记备注”保持两步动作：点“记备注”展开输入，保存。成功后有可感知确认并保留列表/筛选上下文；失败保留输入；Escape 取消并把焦点还给触发按钮；IME composing 的 Enter 不提交；401 清 token 回登录。

#### D3. 移动档期维持只读轻路径，不复制桌面写入面

CalendarPage 在移动端继续使用 42 天月历格 + 类型点阵 + 当日 drawer。用户可以切月、回今天、选日期、查看空日/密集日/冲突/归档客户或取消订单提示；创建、编辑、删除按钮在移动端不可见且不可通过隐藏焦点到达。错误使用可恢复提示与 retry，加载 skeleton 带 status/busy 语义。

#### D4. 375px 的可用性以 CSS viewport 和任务完成为准

- 证据记录 viewport width/height、devicePixelRatio、innerWidth、scrollWidth 与原始截图尺寸；不能把 PNG 物理宽度当 CSS viewport。
- 主页面不得横向溢出；局部复杂表格可以自身滚动，但三条轻路径不得依赖水平滚动。
- Customers 与 Calendar 在底部导航保持直接入口；不新增“更多”层级。现有七项导航只有在 375px 实测触控区小于 44×44、文字遮挡或溢出时才调整布局。
- 可操作控件键盘焦点可见；移动粗指针核心按钮至少 44 CSS px 高/宽；长昵称、500 字备注、密集日不遮住核心动作。
- 系统字体放大固定用浏览器 200% text zoom（或等价地把根字号从 16px 提升到 32px，并记录方法）验证 375px 三条核心动作仍可见、可聚焦、可滚动到达；不把整页截图缩放冒充字体放大。

#### D5. 运维入口采用小型 shell CLI，而不是 README 复制粘贴或新 Go 运维子系统

候选比较：

| 候选 | 优点 | 缺点 | 结论 |
|---|---|---|---|
| 只保留 README 命令块 | 零代码 | 失败清理、trap、确认和漂移无法可靠验证 | 拒绝 |
| 三个职责单一 shell CLI | 与现有 compose/telegram-smoke 习惯一致，无新运行时；可用 PATH fake 测失败分支 | 必须严格控制 quoting、输出和 trap | 采用 |
| 新建 Go ops CLI | 类型与测试更强 | 为三段 compose 编排增加长期子系统，范围过重 | 拒绝 |

##### D5.1 公开命令与支持边界

    ./scripts/production-preflight.sh \
      --mode binary \
      --seed-state empty|initialized \
      --env-file PATH

    ./scripts/production-preflight.sh \
      --mode compose-managed-db|compose-external-db \
      --seed-state empty|initialized \
      --env-file PATH \
      --compose-file PATH \
      --docker-context NAME \
      --project-name NAME

    ./scripts/backup-compose.sh \
      --env-file PATH \
      --compose-file PATH \
      --docker-context NAME \
      --project-name NAME \
      --output DIR \
      [--break-stale-lock]

    ./scripts/restore-compose.sh \
      --env-file PATH \
      --compose-file PATH \
      --docker-context NAME \
      --project-name NAME \
      --input DIR \
      --confirm-project NAME \
      [--break-stale-lock]

- preflight 明确支持 binary、compose 内置 PostgreSQL、compose 外部 PostgreSQL 三种部署轨；所有 compose mode 与 backup/restore 都要求显式 `--docker-context`。backup/restore CLI 首版只支持已经 seed 完成、app container 存在且满足 D5.3 正常 running 或 Engine exited predicate、经本机 Unix socket 访问的 `compose-managed-db` target；其他 app 状态、TCP/SSH 远程 Docker endpoint 均不支持。两者固定、字面等价地调用 `production-preflight --mode compose-managed-db --seed-state initialized --env-file ... --compose-file ... --docker-context ... --project-name ...`，所以 `SEED_ADMIN_PASSWORD` 非空一律 fail-closed，不从 env 内容猜 seed 状态。binary、compose-external-db、unsupported app state、远程 Docker endpoint 或尚未 seed 的 managed target 若要上线，README 必须声明脚本不覆盖其数据库/状态，operator 需提供等价的“停 app→数据库备份→头像卷/目录→manifest→恢复验证”演练记录，否则 V1 go-live checklist 为 blocked。
- `--docker-context` 是唯一允许的 Docker selector。`DOCKER_HOST`、`DOCKER_CONTEXT`、`DOCKER_TLS_VERIFY` 或 `DOCKER_CERT_PATH` 任一非空都在第一次 Engine call 前 exit 2；脚本不得读取 active/default context 来猜。脚本只调用一次 `docker context inspect <NAME>` 解析 endpoint，要求 `Host` 为 `unix://` absolute existing socket，取 socket realpath 后冻结为进程内 `pinned-host`；TCP/SSH/npipe、相对/缺失 socket 或 selector 冲突均拒绝。之后每一次 `docker` 与 `docker compose`（Engine ID、image/container/volume inspect、lock create/rm、helper 查询、render/runtime query、stop/restore/start/cleanup）都必须在清空上述 selector env 后显式使用同一 `docker --host <pinned-host>`，不再按 context name 或环境重新解析；pinned-host/path 不进入 stdout/stderr/证据。两个 local context alias 解析到同一 socket/Engine 时必须得到同一 physical target identity。
- 配置来源 descriptor 与物理运维 target identity 必须分离。来源 descriptor 由 `compose-file realpath + compose-file SHA-256 + env-file realpath + docker-context name/pinned-host（仅进程内）+ 规范化 project-name + 固定 service app/postgres + 固定 logical volume avatar_data/pgdata` 构成，只用于显式输入规范化、render/preflight、runtime identity 核对和 metadata provenance；realpath、context 与 endpoint 只在进程内使用，不进入日志/证据，也绝不能参与互斥锁 key。物理 target identity 见 D5.3。禁止依赖 cwd、默认 project、`COMPOSE_PROJECT_NAME`、active context 或用户 shell 中的隐式 compose flags。`project-name` 必须显式传入且逐字满足 Compose 的 lowercase ASCII 名称语法；“规范化”只表示语法验证后的原值，不做 lowercase、trim 或 cwd 派生变换。restore 的确认值必须与该值逐字完全相同。
- `docker-compose.yml` 的 app `env_file` 要改为可由脚本显式选择的路径（例如 `${CRM_ENV_FILE:-.env}`）；脚本同时把同一个 canonical env file 用于 compose interpolation 和 app env_file，render 后核对选择结果，避免“preflight 检查 A、restore 启动却读取仓库 `.env`”。
- compose CLI 最低版本固定为 v2.24（仓库使用 `env_file.required`）；版本不足、compose render 失败、固定 service/volume/mount/image identity 不匹配时 fail-closed。

##### D5.2 production-preflight 判定矩阵

env file 采用严格 dotenv data parser：只接受空行、注释与唯一 `KEY=VALUE`，允许整值单/双引号但不执行 shell；禁止 `source`、`eval`、命令替换、重复 key、非法 key 和未闭合引号。RHS 作为不可信数据处理，任何输出不得回显原值。

| 条件 / key | binary | compose-managed-db | compose-external-db | 失败或警告规则 |
|---|---|---|---|---|
| `--seed-state` | 必填 | 必填 | 必填 | `empty` 时 SEED_ADMIN_PASSWORD 必填、至少 12 字符且非 sentinel；`initialized` 时必须为空/缺失，防长期保留 seed 凭证 |
| DATABASE_URL | 必填 | compose app 不使用 env file 中该值 | compose app 不使用 env file 中该值 | binary 缺失、含 `crm-dev-password` 或远程 host 使用 `sslmode=disable` 失败；本地 socket/loopback 可 disable 但输出 network-boundary warning |
| APP_DATABASE_URL | 不适用，非空警告 | 必须为空/缺失 | 必填 | managed 非空失败，防误切外部库；external 缺失/dev sentinel/远程 `sslmode=disable` 失败 |
| POSTGRES_USER / POSTGRES_DB | 不适用 | 必填、非空 | 不适用 | managed 缺失失败；只输出 key 状态 |
| POSTGRES_PASSWORD | 不适用 | 必填 | 不适用 | 缺失或等于 `crm-dev-password` 失败 |
| AUTH_TOKEN_SECRET | 必填 | 必填 | 必填 | 缺失、少于 32 字符或等于 `dev-only-token-secret-change-me` 失败 |
| HTTP_ADDR | 可缺省 `:8080` | render 后必须为 `:8080` | render 后必须为 `:8080` | 对公网 bind 只给 TLS/reverse-proxy attention，不宣称已验证外部边界 |
| AVATAR_STORAGE_DRIVER | 必须为 local | render 后必须为 local | render 后必须为 local | 其他值失败 |
| AVATAR_LOCAL_ROOT | 必填、absolute realpath、已存在且当前账号可读写 | render 后必须为 `/var/lib/crm/avatars` | 同 managed | binary 的 repo/tmp 路径警告；compose path/mount 不符失败 |
| AVATAR_LOCAL_REQUIRE_MOUNT | 必须显式 true/false | render 后必须为 true | 同 managed | binary=false 允许 host persistent dir，但输出 durability/备份 attestation warning；compose 非 true 或未挂 `avatar_data` 失败 |
| TELEGRAM_BOT_TOKEN / USERNAME | 两空合法 disabled；两项同时设置合法 enabled | 同 binary | 同 binary | 只设一项、username 非既有校验规则或 token 为示例值时失败；不调用 Telegram、不输出 token |
| compose project / file / endpoint | 不适用 | 显式 context/project/file 且 render/identity 通过 | 同 managed | 缺显式 context/project、active/default context 猜测、selector env 非空、非本机 Unix endpoint、缺 service/volume、Compose <2.24 均失败；selector/remote 为 exit 2，render/identity 为 exit 5 |

dev sentinel 固定至少覆盖 `.env.example` 的 `crm-dev-password`、`dev-only-token-secret-change-me`、`dev-only-admin-password` 以及包含这些值的 DB URL；用户名/数据库名 `crm` 本身不是 secret sentinel。TLS、反向代理、防火墙、异地介质和断电级 filesystem durability 是外部 attestation，不可被 exit 0 冒充已验证。

稳定 exit code：`0=机器可验证项通过`、`2=usage/不支持 selector/mode/backup output 路径`、`3=env 文件读取或语法错误`、`4=配置矩阵失败`、`5=compose 版本/render/image/target identity 失败`、`6=必需本机命令缺失`、`7=lock busy/post-inspect/stale/ownership 失败`、`8=backup pipeline/health/publish 失败`、`9=restore input/staged package/confirm 的 pre-mutation 校验失败`、`10=restore 已开始破坏后的 replace/start/health/verify 失败`、`11=cleanup/residual 失败`。公开 CLI 在第一次 Docker context/Engine 调用前先核对本轮所需命令，缺失时固定以 `dependency-check` terminal stage exit 6；backup/restore 必须原样保留前置 preflight 的 2～6。同一运行出现多个错误时 cleanup=11 优先，其次 destructive restore=10，再次为原始阶段 code。stdout 只允许 `stage/key=status`；stderr 只允许 error code、key/阶段和不含 value/path 的修复动作。任何 secret、DATABASE_URL、Bot API URL、chat id、客户正文、env 原始行或本机绝对路径都禁止输出。

##### D5.3 backup/restore 目标、锁与运行状态

- 两个破坏性 CLI 先按 D5.1 解析并冻结 pinned-host，以该 host 只读取得非空 Docker Engine ID。脚本完成 strict env/compose render 后，解析 render 中的 app image ID；该 image 必须已存在本机、不得临时 pull/build，且 `docker image inspect` 的 `.Config.Volumes` 必须为 null/empty，否则在 lock create 前 exit 5。随后计算并原子取得下述 Docker Engine 侧物理 target lock，再在锁内固定调用 D5.1 的 `compose-managed-db + initialized` 完整 preflight、读取 app 状态和核对 runtime target。固定操作 app/postgres/avatar_data/pgdata，不允许 service/volume 名从环境覆盖。preflight 或 runtime 核对失败时释放本次新锁并在任何 target stop/replace/volume/network mutation 前 fail-closed；创建/删除经 post-create inspect 证明无 mount、network=none、永未启动的 lock container，是取得/释放 mutex 所必需且唯一允许的 preflight Docker metadata mutation。
- 物理 target identity v1 的 canonical bytes 固定为 `schema=v1\0docker-engine-id=<engine-id>\0project=<normalized-project-name>\0services=app,postgres\0volumes=avatar_data,pgdata`；`physical-target-hash` 是其 SHA-256。Engine ID 不可取得时 exit 5，不允许退回 context name、socket path、compose/env realpath、cwd 或 source fingerprint。由此，同一 Docker Engine 上同一规范化 project（包括两个 context alias 指向同一 Engine）必须得到同一 hash；把等价 compose 文件复制到另一 realpath、或改用等价 env/compose 来源，不得产生第二锁域。不同 Engine 上的同名 project 可以得到不同 hash，且不得互相破锁。
- Engine 侧 lock 使用确定性容器名 `photographer-private-crm-ops-lock-<physical-target-hash>`。脚本生成进程内 256-bit owner nonce，以已验证 volume-free 的 app image ID 执行一次不 pull 的 atomic `docker create --network none --name <deterministic-name> ...`，不传任何 bind/volume/tmpfs；Docker daemon 的容器名唯一性是 primary mutex，不能用 `XDG_RUNTIME_DIR`、`/tmp` 或其他 client-local 目录冒充跨 context/client 的锁。create 成功必须保存返回并经 inspect 规范化的完整 immutable container ID；随后所有检查/清理只使用该 ID，不再以 name 作为删除目标。lock container 固定标签 `com.photographer-crm.ops=true`、`com.photographer-crm.ops.target-sha256=<physical-target-hash>`、`com.photographer-crm.ops.kind=lock`、`com.photographer-crm.ops.owner-pid=<decimal-pid>`、`com.photographer-crm.ops.owner-started-at=<UTC-RFC3339>`、`com.photographer-crm.ops.owner-nonce-sha256=<sha256>`；nonce 原文与 Engine ID 原文均不落盘/不输出。
- 在把 create 视为 acquire 成功前，必须按完整 ID inspect 并同时断言：name、全部 lock labels、nonce hash 与期望逐字相同；state=created、running=false、从未 start；`HostConfig.NetworkMode=none`；image `.Config.Volumes` null/empty；container `Mounts=[]`。任一断言失败时，只能以该完整 ID 执行 `docker rm -v` 并验证该 ID 与其 inspect 暴露的匿名 volume ID 均消失；清理不完整则 case/命令失败并保留脱敏 residual，不得按 name 猜删。create 非零或未返回可验证完整 ID 时同样不得按 name 清理。只有上述 post-create invariant 全部通过后才算持锁；锁从完整 preflight/target lookup 前持有到最终状态/cleanup 完成。
- 所有实际 ops helper container 固定标签 `com.photographer-crm.ops=true`、`com.photographer-crm.ops.target-sha256=<physical-target-hash>`、`com.photographer-crm.ops.kind=backup|restore`、`com.photographer-crm.ops.lock-id-sha256=<sha256(acquired-lock-id)>`与 `com.photographer-crm.ops.owner-nonce-sha256=<sha256>`。除 primary lock 外，每个 target 另有一个不区分 backup/restore 种类的确定性 helper fence name `photographer-private-crm-ops-helper-<physical-target-hash>`；它与 lock 一样用已验证 volume-free image 以 created/network-none/no-mount/never-started 状态原子创建，并绑定本轮 lock ID hash/nonce。任何 app stop/start、DB/avatar 读写或 operational helper start 前，owner 必须先取得并 post-inspect 这个 helper fence，然后按完整 lock ID 再核对本轮 lock/nonce 仍存在；旧 delayed helper/fence 若在 breaker 查询后才到达 daemon，必须要么与新 fence name 冲突，要么在 lock-ID recheck 时失败，不得进入 target operation。任何 actual helper 也必须先 create 为不运行对象，绑定同一 generation，主进程在 helper start/exec 前再次核对 lock ID/nonce/fence；不能用一次“no helper”查询代替 generation fencing。helper fence 在所有 target/helper cleanup 后、primary lock 释放前按其 immutable ID 删除。
- 同名 lock 已存在时默认失败。stale break 必须先把 deterministic name 一次解析为旧锁的完整 immutable `candidate-id`，之后所有 name/hash/label/PID/nonce-shape/helper-fence/helper 检查与最终 `docker rm -v` 都绑定该 candidate-id；只有对应 CLI 显式传 `--break-stale-lock`、candidate identity 完整匹配、记录的本机 PID 已不存在且相同 hash 无已创建的 helper fence/backup/restore helper 时，才可按 candidate-id 删除。candidate 已消失、re-inspect 不一致、删除失败/not-found 或权限/状态不可证时，绝不能回退为按 name 删除；只能直接 fail-closed，或进行至多一次正常 atomic create，让 name conflict 决定输赢。成功删除后也只允许至多一次正常 create；若新同名锁已由他人取得则当前操作失败，不能碰后来者。不能仅按年龄自动破锁。
- 正常 release 只允许：按本次 acquire 保存的完整 ID re-inspect owner-nonce-sha256/name/hash，匹配后对该 ID 执行一次 `docker rm -v` 并验证该 ID 不存在；ID 已消失、inspect/nonce 不匹配或删除失败均标记 cleanup/命令失败，绝不按 name 重试或删除当前同名 container。lock removal 是脚本最后的 mutex 动作；随后出现的同名不同 ID 一律视为他人 ownership。双 breaker、breaker↔普通 acquire、owner release↔新 acquire 的竞态都必须以 immutable ID 证明旧 cleanup 不能删除新锁。
- 持锁后必须从显式 compose/env 来源生成 canonical render，并只通过 Compose runtime labels 与 `docker ps --all`/inspect 解析实际 target：render 必须含固定 app/postgres 与 avatar_data/pgdata；initialized target 的 app、postgres container 及两个 named volume 各恰好一个，并同时匹配 project/service 或 project/logical-volume labels。app 的支持状态只有两种精确 predicate：`running = State.Status=running && Running=true && Paused=false && Restarting=false && Dead=false`；`stopped = State.Status=exited && Running=false && Paused=false && Restarting=false && Dead=false`。`created|paused|restarting|removing|dead|unknown`、字段自相矛盾、app absent、多 app、资源重复/歧义均作为 `rejected` 在任何 target mutation 前 exit 5；V1 不把 created 折叠为 stopped，也不试图把异常态恢复成普通 running/exited。render 的解析后 image ID、app/PG volume mount identity 与实际资源必须唯一对应；另一 realpath 的等价 render 允许通过。来源描述与现存 project 在这些 operation-relevant identity 上不一致或无法证明一致时同样 exit 5。不能用来源 realpath 的不同绕锁，也不能因 project name 相同就跳过 runtime identity 核对；特别测试 app absent/每个 rejected Engine 状态及 alternate compose 改 app image/mount 时必须拒绝，不能用“image 本机存在”代替 runtime 绑定证据。
- backup 在 stop 前探测并保存 app 的 supported predicate；仅当原为正常 running 时 stop，包生成与自校验完成后恢复并等待 health，再原子发布。任何失败清理临时半包，并恢复原正常 running 或保持原 Engine exited；原 exited target 不被擅自启动。其他 Engine 状态是 pre-mutation identity reject，不是可执行状态。
- restore 在持锁并完成 target identity 后、任何 stop/replace 前，把输入包五件套物化到本次 `umask 077` 私有 staging snapshot；只使用 staged regular files 完成 schema、no-symlink、SHA-256、tar entry（拒绝 absolute、`..`、symlink/hardlink/device）与 typed target confirm 校验，后续 replace 也只能读取该 staged snapshot。输入包在 copy/校验期间变化、staging 不完整或校验失败都在 target mutation 前 exit 9；破坏前失败不碰 app/DB/volume。破坏开始后任何失败都保持 app `logical=stopped, engine_status=exited`，并忠实记录可能已部分变化的 DB/avatar after-state，不虚构 rollback。成功 exact-generation verify 后仅当 app 原为正常 running 才启动并等待 health；start 失败或 health 失败时必须再次 stop 并 inspect 到精确 exited predicate 后以 `restore-start|restore-health` terminal stage exit 10，不得把已验证数据冒充整个 restore 成功。原 Engine exited 时保持 exited。其他 Engine 状态在破坏前拒绝。
- `--confirm-project NAME` 不是裸 boolean，必须与规范化 target project 完全相同；输出只引用 project name、相对 artifact 名、stage 和 checksum，不打印 compose/env 绝对路径。

##### D5.4 路径与一致备份包 schema

- owner 可以把备份写到仓库外；“外部路径”本身不是错误。output 必须是尚不存在的新 leaf，parent realpath 已存在；input 必须是现有目录。拒绝空路径、`/`、project root、本轮临时目录、source avatar/pg volume mount、input/output 重叠、symlink leaf/逃逸、特殊设备和非 regular artifact。不依赖 runtime target 的 lexical/filesystem 路径反例在第一次 Engine call 前以 `path-validate` 拒绝：backup output 为 exit 2，restore input 为 exit 9；与实际 source volume mount 重叠只能在持锁的 runtime identity 查询后判定，但仍必须在 stop/replace 前以 `target-identity` exit 5 拒绝。所有敏感临时/最终文件使用 `umask 077`。
- 一致备份包固定恰含 `database.sql`、`avatar-volume.tgz`、`avatar-manifest.json`、`metadata.json`、`SHA256SUMS`。前三项在同一 app freeze 窗口取得；SHA256SUMS 覆盖除自身外四个文件；写同一 parent 下临时目录，全部验证与 app 状态恢复成功后才 rename 原子发布。
- `metadata.json` 固定 `schema_version: 1`、UTC RFC3339 `created_at`、三个 payload 的相对 filename/sha256、manifest schema version、source compose project、compose-file SHA-256、解析后的 app/postgres image ID（有 repo digest 时并记 digest）、PostgreSQL server major、`app_was_running`。禁止 env value、DATABASE_URL、password/token/chat id、客户正文、绝对路径与 volume mountpoint。未知 schema version fail-closed；artifact filename 不允许由 metadata 跳出包目录。
- restore 顺序固定 `resolve input/typed confirm → acquire/verify target+helper fence → private staging snapshot + authoritative package validation → stop-if-running → replace DB → replace avatar volume → exact-generation verify → start-if-originally-running → health-if-started`；数据库 counts 和 manifest 必须与 metadata/fixture 共同核对，start/health 失败还必须执行 failure-stop 并验证 exited。任何 pre-lock 快速检查都不是 authority，不能替代锁内 staged validation。

##### D5.5 隔离 smoke

`v1-ops-smoke.sh` 必须创建独立 temp env、显式 compose file、显式 local Unix `--docker-context` 和唯一 synthetic project name，把它们原样传给 production preflight/backup/restore CLI；backup/restore target 先完成 synthetic seed，再按固定 `initialized` preflight 运行，temp env 中 seed password 必须移除。不得读取仓库 `.env`、active/default context 或默认 project。selector matrix 必须独立覆盖：显式 local context、同 socket 的 local alias（两者 Engine ID/hash 相同）、remote context、`DOCKER_HOST=tcp://...`、`DOCKER_HOST=ssh://...`、非空 `DOCKER_CONTEXT` 及 flag/env 冲突；拒绝 case 在任何 Engine/lock/target call 前 exit 2，所有后续 Docker/Compose 调用由 shim/argv 证明显式使用同一 pinned-host。

另复制一份内容等价但 realpath 不同的 compose file：用故障注入让第一个操作在取得 physical target lock 后、任何 target mutation 前暂停，再以相同 synthetic project 从另一 realpath 发起第二个操作；`backup→backup`、`backup→restore`、`restore→backup`、`restore→restore` 四种组合必须各自形成稳定 machine case，第二个操作都因同一 deterministic name 忙而 fail-closed，target before/after 相同。lock safety matrix 还必须逐 case 覆盖：候选 image 声明 `VOLUME` 时 create 前拒绝；create 未显式/未落实 network=none、post-create mount/label/state 不符时按返回的完整 ID fail-closed 清理；清理后 candidate ID 与匿名 volume 零残留；双 breaker、breaker↔普通 acquire、owner release↔新 acquire 的确定性竞态中旧 owner/candidate 只能删除旧 immutable ID；另注入“旧 owner 已发出 helper-fence create，breaker 的 no-helper 查询先返回，旧 create 后到 daemon”的 deterministic delayed-helper race，要求旧 generation 不得进入 target operation，新 owner 最多 fail-closed 且不删他人 lock/helper；app absent（含 alternate compose 改 app image/mount）在 target mutation 前拒绝。测试另建一个带 marker 的 sentinel project，所有成功、reject、alias-lock、stale-break、race 和故障注入 case 都必须断言 sentinel container/DB/volume marker 不变；target oracle 则严格按 D8 的 pre-mutation/backup/restore-success/destructive-failure 分类，不能笼统要求所有 restore failure 的 target 不变。

每个 case 按 D8 schema 写入机器结果；trap 清理 smoke project、lock ID、helper、temp env、compose 副本、包和 volume；清理失败使 CMD-005 与顶层 results status 非 pass，并记录脱敏残留名称，不以用户生产项目做测试。

#### D6. README 是入口，详细运维语义与脚本保持单一真相

README 覆盖：

- 首次登录与客户/订单/档期/提醒/dashboard/设置/导出的最短使用路径。
- 二进制与 compose 部署、TLS/反向代理/防火墙边界、healthz、单 Telegram replica。
- DATABASE_URL、POSTGRES_PASSWORD、AUTH_TOKEN_SECRET、SEED_ADMIN_PASSWORD、TELEGRAM_BOT_TOKEN/USERNAME 的生成、保存、轮换/撤销和 seed 后清理。
- 三种 preflight mode、显式 `--docker-context`/pinned-host 规则、compose-managed-db + initialized + app-present + local Docker endpoint 的脚本支持边界、seed 后移除密码、binary/external/app-absent/remote Docker 的等价备份责任；备份频率、异地加密副本、retention/销毁、backup/restore/显式 stale-lock 命令与恢复演练。
- reference-only JSON 不是备份；线上移除头像不追溯历史备份。
- 修正“服务端不读取 TELEGRAM_BOT_TOKEN”等已经与生产装配冲突的旧说明。
- Compose v2.24+、显式 project/env/compose file、env file 不被仓库 `.env` 偷换、生产 app 单 replica 与 DB/头像一致 freeze 的约束。

README 可以链接详细 runbook，但上述边界不得只藏在未链接文档里。

#### D7. 本 feature 不宣称“安全完成”

v1-hardening 是 roadmap 的收口名称，完成信号以本 design 为准。生产预检、TLS/网络说明、IdleTimeout 与凭证操作降低上线失误，但登录暴力破解、cookie 认证、JWT 生命周期和完整威胁模型仍是显式 residual。它们曾由 platform-skeleton acceptance 明确转交本 feature，因此设置阻塞性 owner checkpoint：

| ID | owner 在整体 design review 必须拍板 | 采用建议 | 持久落点 |
|---|---|---|---|
| H1 | 当前 V1 是否接受“无登录限速、30 天 JWT、localStorage Bearer、无改密 API”的风险并延后 | 接受当前 V1 residual，不在收口期重写认证 | owner 批准后、goal package 前创建 `.codestable/issues/2026-07-22-auth-hardening/auth-hardening-report.md`（status=open，记录四项风险、owner/date、延后理由与建议转 `cs-feat`/`cs-epic`）；若不接受则扩本 design、更新 roadmap 并重跑 review |
| H2 | TG 真外部最终链路是否复用 2026-07-17 owner-attested transport/binding 证据 | 复用真外部 transport，fresh 验证同一 synthetic fixture 的次日 digest payload/调度关联 | 写入 V1 回归 A24 与 acceptance；若要求 fresh TG，则由 owner 提供脱敏真机证据 |

未得到 owner 对 H1/H2 的明确回答不得把 design 标 approved。implementation 不得临时加认证中间件/错误码，也不得把“文档有 TLS”写成认证风险已关闭。

#### D8. 回归与证据使用 canonical artifact contract

- 权威回归清单：`.codestable/features/2026-07-22-v1-hardening/v1-hardening-regression.yaml`，固定 `schema_version: 1`、`feature` 与 `scenarios[]`。每项至少含 `scenario_id`、`precondition`、`action`、`expected`、`result(pending|pass|fail|blocked)`、`evidence_paths[]`、`notes`；A1-A27 每个 ID 恰出现一次，acceptance 前 core scenario 必须为 pass。
- 浏览器证据：`evidence/browser/`；截图命名 `<scenario-id>-<slug>.png`，viewport 元数据统一落 `evidence/browser/viewport-metadata.json`，每条记录含 CSS viewport width/height、DPR、innerWidth/innerHeight、scrollWidth/scrollHeight、原始截图 width/height、字体放大设置和关联 scenario/screenshot。
- 运维证据：脱敏日志 `evidence/ops/v1-ops-smoke.log` 与机器结果 `evidence/ops/v1-ops-smoke-results.json`；命令证据 `evidence/commands/CMD-001.log`～`CMD-006.log`。不得把 temp env、备份包、数据库 dump、头像、Docker inspect 原始 env 或 secret-bearing stdout 存进仓库。
- 权威 ops case catalog 路径固定为 `.codestable/features/2026-07-22-v1-hardening/v1-hardening-ops-case-catalog.yaml`，由 STEP-001 按下方冻结 inventory 逐项物化，不得在实现时自行删减；展开后必须恰为 148 个唯一 case ID。catalog 固定 `schema_version: 1`、`feature` 与 `cases[]`；每项恰含 `{case_id,scenario_ids[],fixture,action,oracle_class,expected}`。`fixture` / `action` 是与 case ID 对应的稳定、非空复跑说明：fixture 必须写明该 case 特有的部署 mode、selector/input、app Engine state、target/package generation 或 fault/race setup，不适用的维度以字段内 `N/A` 明示，不得让整个 fixture 只是占位；action 必须写明被调用的脚本入口、关键 flags 以及注入失败或竞态编排。两者不得写“同上”、`TBD`、引用另一 case 代替内容或承载与 `expected` 冲突的判定；它们只解释如何复跑，oracle truth 仍由本行展开的 `scenario_ids`、`oracle_class` 与 `expected` 决定。validator 必须拒绝空白/整字段占位/跨 case 引用，并要求 result 对应字段与 catalog 深相等。`expected` 固定 `{exit_code,terminal_stage,stage_plan,engine_call_policy,target_mutation_allowed,required_lock_roles[],cleanup_objects[],forbidden_side_effects[],backup_package_expectation,operation_cleanup_expected,allowed_operation_removers{}}`；cleanup_objects 表示该 case 的 logical scope，`allowed_operation_removers` 按每个 required lock role 冻结只允许的 `owner|authorized-breaker|none`，`harness-finalizer` 永不是 production operation 的合法 remover。`engine_call_policy` 只允许 `forbidden|readonly|lock-metadata|target-operation`，后一档包含前一档能力。terminal/stage name 只允许 `dependency-check|path-validate|complete|usage|endpoint-select|env-parse|config-matrix|compose-version|compose-render|image-safety|lock-acquire|lock-post-inspect|target-identity|package-validate|backup-stop|backup-pg-dump|backup-avatar-tar|backup-manifest|backup-package-verify|backup-state-restore|backup-health|backup-publish|restore-stop|restore-db-replace|restore-avatar-replace|restore-verify|restore-start|restore-health|restore-failure-stop|cleanup`。
- terminal/stage allowed set 还包含 `helper-fence`，它固定位于 `lock-post-inspect` 之后、`target-identity`/任何 target operation 之前；所有已取得 primary lock 且可能触及 target 的 BKR/BKE/BKF/BKP/LCK/LCO/RSP/RSR/RSE/RSF/COR plan 必须显式记录 helper-fence 为 ok、failed 或经 oracle 证明不适用，不得省略。
- `expected.backup_package_expectation` 只允许 `none|published-valid|not-published-clean`，`expected.operation_cleanup_expected` 只允许 `pass|fail`；两者都由 inventory Plan 机械推导并在 catalog 中物化，实现者不得为单个 case 自由放宽。
- `v1-ops-smoke-results.json` 固定 `schema_version: 1`、`feature`、顶层 `status(pending|pass|fail|blocked)`、不含路径的 `target {project_name,target_hash}`、`catalog {path,sha256,required_case_ids[]}`、`coverage {executed_case_ids[],missing_case_ids[],unknown_case_ids[],duplicate_case_ids[]}`、`suite_cleanup {result,residual_resources[]}` 与 `cases[]`；其中 `target_hash` 是 D5.3 physical-target-hash，catalog sha256 必须由 canonical catalog 文件计算。结果中的 required IDs 必须与 catalog ID 集合完全相等；executed 与 required 也必须集合相等且 case_id 全局唯一，missing/unknown/duplicate 三数组必须全空，否则 case 自报全 pass 也不得使顶层 pass。
- 每个 result case 固定 `{case_id,scenario_ids[],fixture,action,oracle_class,expected,observed,result,stages[],target_before,target_after,package_oracle,backup_package,lock_observations[],sentinel_unchanged,operation_cleanup,harness_cleanup,evidence_paths[]}`；`scenario_ids`、`fixture`、`action`、`oracle_class`、`expected` 必须分别与 catalog 对应 entry 深相等。`observed` 固定 `{exit_code,terminal_stage,engine_calls{readonly,lock_metadata,target_mutation},target_mutation_started,lock_roles[],cleanup_objects[],side_effects[]}`。engine call 计数均为非负整数；observed 不得超过 expected policy，exit/stage/roles/cleanup 必须与 expected 相等，target_mutation_started 只能在 allowed=true 时为 true，side_effects 与 forbidden_side_effects 交集必须为空。`stages[]` 固定 `{name,operation_status,oracle_status}`；name 全局唯一，operation_status 只允许 `not-run|ok|failed|blocked`，oracle_status 与 case result 使用 `pending|pass|fail|blocked`。每个 `stage_plan` 要求的 stage 必须恰出现一次；预期注入失败的 terminal stage 必须写 operation_status=failed、oracle_status=pass，后续业务 stage 为 not-run，但 plan 要求的状态恢复/清理 stage 仍必须记录；禁止再用一个模糊 `result` 同时表示操作和 oracle。
- `stage_plan` 由 inventory 的 Plan 列冻结：`PFB/PFC` 分别是 binary/config-only 与 compose 成功 preflight；`PFR/DEP/PATH` 要求 terminal stage failed 且未越过拒绝边界；`BKR` 要求 `lock-acquire→lock-post-inspect→target-identity→backup-stop→backup-pg-dump→backup-avatar-tar→backup-manifest→backup-package-verify→backup-state-restore→backup-health→backup-publish→complete` 全部 ok；`BKE` 要求同一 pipeline，但 stop/state-restore/health 是 not-run、其余必须 ok；`BKF` 以上述 backup 顺序及 catalog terminal stage 机械推导前缀 ok/terminal failed/业务后续 not-run，原 running target 的 `backup-state-restore` 仍必须 ok，除 terminal=`backup-health` 时 health 本身为 failed；`BKP/LCK/RSP` 是在 target mutation 前的 backup/lock/restore reject；`LCO` 是按正确 immutable ID 完成的 lock cleanup；`RSR` 要求 `package-validate→restore-stop→restore-db-replace→restore-avatar-replace→restore-verify→restore-start→restore-health→complete` 全部 ok；`RSE` 的 stop/start/health 为 not-run、其余 ok；`RSF` 以 restore 顺序与 terminal stage 推导前缀，terminal failed，且 `restore-failure-stop` 必须 ok 并证明 exited；`COR` 专用于“operation cleanup 预期失败但 oracle 正确检出”。所有 plan 都必须先完成它所需的 dependency/endpoint/env/render/image/lock/target 前置；validator 以固定 plan+原 app predicate+terminal stage 生成 exact required outcomes，不接受 catalog 自由缩减 stage。
- `PFB` 的 exact required outcomes 是 `dependency-check=ok、env-parse=ok、config-matrix=ok、complete=ok`；`PFC` 在此基础上还必须 `endpoint-select=ok、compose-version=ok、compose-render=ok`。`DEP` 恰为 `dependency-check=failed/oracle pass` 且后续全 not-run；`PATH` 恰为 `path-validate=failed/oracle pass` 且 Engine/lock/target 全 not-run；`COR` 恰为 `cleanup=failed/oracle pass`、operation_cleanup=fail、harness_cleanup=pass。`PFR/BKP/LCK/RSP/LCO` 的 terminal stage 与不越过拒绝/所有权边界由 catalog exit、Engine policy、target_mutation_allowed 与 cleanup/remover 联合机械判定，不能仅查 terminal 文本。
- `oracle_class` 固定为 `pre-mutation-readonly|pre-mutation-reject|backup-readonly|restore-success|restore-destructive-failure|cleanup-only`。`target_before/after` 各为 null 或 `{app_state{logical,engine_status,running,paused,restarting,dead},db_counts,marker_sha256,manifest_sha256}`，其中三个 data field 都不再是裸值，而是 `{status,value,error_class}` envelope；status 只允许 `observed|absent|unreadable`，observed 时 value 必填且 error_class=null，absent/unreadable 时 value=null 且 error_class 只允许 `artifact-missing|artifact-invalid|schema-missing|query-failed|permission-denied|unknown`。logical 只允许 `running|stopped|rejected|absent`，engine_status 只允许 `created|running|paused|restarting|removing|exited|dead|absent|unknown`，app absent 时四个 boolean 为 null。`package_oracle` 仅 restore-success 非 null，形状为 `{db_counts,marker_sha256,manifest_sha256}` 裸期望值。pre-mutation-readonly/reject 要求 target mutation=false 且 before/after envelope 深相等或同为 null；backup-readonly 要求业务 data envelope 深相等且 app 恢复同一 supported predicate；restore-success 要求 after 三个 envelope 都 status=observed且 value 等于 package_oracle，app 恢复同一 supported predicate；restore-destructive-failure 要求 target_after 非 null、target_mutation_started=true、after.logical=stopped、after.engine_status=exited，并对每个可读/缺失/不可读 data 忠实写 envelope，禁止丢掉整个 after 或伪造 counts/hash；cleanup-only 核对 operation/harness 两层 cleanup oracle。
- `backup_package` 在 `backup_package_expectation=none` 时为 null，否则固定 `{status,files[],metadata_schema_version,checksums_verified,manifest_verified,self_check_passed,temp_package_residuals[]}`；status 只允许 `not-created|partial-cleaned|published`。`published-valid` 要求 status=published、files 恰为五件套、schema=1、三个 verified/self-check 全 true、temp residual 空；`not-published-clean` 要求无最终 published package、status 为 not-created/partial-cleaned 且 temp residual 空。BKR/BKE 固定为 published-valid，BKF 固定为 not-published-clean，其他 plan 为 none；由此 exit 0/complete 但完全未执行 backup pipeline 的 no-op result 必须被拒绝。
- `lock_observations[]` 每项固定 `{role,container_id_sha256,image_declared_volume_count,state,network_mode,mount_count,started,removed,removed_by,anonymous_volume_count_after}`；没有 create 时 ID/state/network/mount/started/removed/removed_by 用 null，state 只允许 `created|absent|null`，removed_by 只允许 `owner|authorized-breaker|harness-finalizer|none|null`。任何成功 acquired 的 lock/helper fence 都必须 volume count=0、state=created、network=none、mount=0、started=false，最终旧 ID removed=true 且 anonymous volume=0。race 中后来者 ID 的 removed_by 不得是旧 owner/旧 breaker。`operation_cleanup` 与 `harness_cleanup` 均固定 `{result,residual_resources[]}`；先记 production operation cleanup，再记独立 harness finalizer。正常 case 只有 operation_cleanup=pass 才能 oracle pass；任何 operation-owned 对象需要 harness-finalizer 删除时 operation_cleanup 必须 fail，即使最终 residual 为空也不得掩盖。只有 `COR`/`OPS-CLEANUP-RESIDUAL` 的 expected 固定 operation_cleanup_expected=fail，用 operation_status=failed/oracle_status=pass 证明检出泄漏；随后 harness_cleanup 仍必须 pass。所有 case 的 harness_cleanup 与 suite_cleanup 都必须 pass，evidence path 必须 feature-relative。
- catalog/results validator 必须由 CMD-004 定向测试并在 CMD-005 真实执行：校验 catalog 自身 ID 恰等于下方冻结 inventory，结果集合完全相等、expected 深相等、stage-plan/backup-package/observed/oracle/lock/operation-cleanup/harness-cleanup 规则成立。CMD-004 的独立反例至少包含 missing、unknown、duplicate、catalog sha/expected/scenario/oracle mismatch、exit/terminal/stage-plan mismatch、未授权 Engine/target mutation、backup no-op 伪报 published success、错误 immutable ID removed_by、owner 泄漏被 finalizer 清空后伪报 operation cleanup pass、destructive target_after 为 null/伪造 observation、sentinel changed 与 suite cleanup residual；任一反例都必须让 validator 非零。顶层 pass 仅在 validator pass、所有 required case result=pass（包含 COR 对预期 operation-cleanup failure 的 oracle pass）、sentinel 全部 unchanged、operation cleanup 等于各自 expected、每个 harness cleanup pass，且 suite_cleanup 删除 `[temp-env,compose-copy,synthetic-project,sentinel-project,synthetic-volumes]` 后无 residual 时成立。真实生产 project name、Engine ID、endpoint/context、绝对路径、raw container ID 和 marker 原值不得进入 JSON；只允许 synthetic project 与 SHA-256 ID。

冻结 inventory 采用以下缩写；同一表格单元内以逗号分隔的每个 ID 都必须在 catalog 中展开为独立 entry，继承该行全部 expected 字段，禁止把一行合并成一个 runtime case。冻结联合集是 148 个唯一 ID；scenario membership 固定为 A18=66、A19=2、A20=41、A21=2、A22=39，共 150 个 membership，因 `OPS-CLEANUP-RESIDUAL` 与 `OPS-LOCK-RACE-DELAYED-HELPER` 各同时映射 A20/A22：

- Oracle：`PO=pre-mutation-readonly`、`PM=pre-mutation-reject`、`BR=backup-readonly`、`RS=restore-success`、`RF=restore-destructive-failure`、`CO=cleanup-only`。
- Plan：`PFB/PFC/PFR/DEP/PATH/BKR/BKE/BKF/BKP/LCK/LCO/RSP/RSR/RSE/RSF/COR` 按上文 stage-plan 规则机械展开；Plan 是 expected 的必填字段，不是人读注释。
- Engine policy：`F=forbidden`、`R=readonly`、`L=lock-metadata`、`T=target-operation`；Mutation 为 `0|1`。
- Cleanup 只列 case-local 对象：`N=[]`、`L=[lock-containers,ops-helpers]`、`P=[lock-containers,ops-helpers,private-staging]`、`S=[lock-containers,ops-helpers,private-staging,temp-package]`；suite 共享 env/compose/projects/volumes 只由顶层 suite_cleanup 负责。Forbidden 默认 `[]`，只有标 `X` 的 case 固定为 `[command-marker]`。

| Required case IDs（逐个展开） | Scenario | Oracle | Exit | Terminal stage | Plan | Engine | Mutation | Required lock roles | Cleanup | Forbidden | 覆盖目的 |
|---|---|---:|---:|---|---:|---:|---:|---|---:|---:|---|
| `OPS-PF-BIN-INIT-OK`, `OPS-PF-BIN-EMPTY-OK`, `OPS-PF-BIN-LOOPBACK-WARN`, `OPS-PF-BIN-IGNORED-WARN`, `OPS-PF-HTTP-BIN-PUBLIC-WARN`, `OPS-PF-AVATAR-BIN-MOUNT-FALSE-WARN`, `OPS-PF-AVATAR-BIN-REPO-WARN` | A18 | PO | 0 | complete | PFB | F | 0 | `[]` | N | - | binary 两 seed 状态、loopback/public-bind/avatar durability warning、APP/POSTGRES ignored |
| `OPS-PF-MANAGED-INIT-OK`, `OPS-PF-MANAGED-EMPTY-OK`, `OPS-PF-MANAGED-IGNORED-DB` | A18 | PO | 0 | complete | PFC | R | 0 | `[]` | N | - | managed 两 seed 状态与 DATABASE_URL ignored |
| `OPS-PF-EXTERNAL-INIT-OK`, `OPS-PF-EXTERNAL-EMPTY-OK`, `OPS-PF-EXTERNAL-IGNORED-POSTGRES` | A18 | PO | 0 | complete | PFC | R | 0 | `[]` | N | - | external 两 seed 状态与 POSTGRES_* ignored |
| `OPS-PF-TG-DISABLED-OK`, `OPS-PF-TG-ENABLED-OK` | A18 | PO | 0 | complete | PFB | F | 0 | `[]` | N | - | TG fixture 固定为 binary；两空 disabled 与成对 enabled |
| `OPS-PF-SELECTOR-LOCAL-ALIAS`, `OPS-PF-SELECTOR-ARGV-PINNED` | A18 | PO | 0 | complete | PFC | R | 0 | `[]` | N | - | local alias 同 Engine/hash；所有后续 argv 使用同一 pinned host |
| `OPS-PF-ENV-INJECTION-DATA` | A18 | PO | 0 | complete | PFB | F | 0 | `[]` | N | X | `$()`/反引号只作 data，command-marker 不得出现 |
| `OPS-PF-USAGE-MISSING-MODE` | A18 | PM | 2 | usage | PFR | F | 0 | `[]` | N | - | 缺 mode |
| `OPS-PF-USAGE-MISSING-CONTEXT` | A18 | PM | 2 | endpoint-select | PFR | F | 0 | `[]` | N | - | compose 缺显式 context，不读 active/default |
| `OPS-PF-ENV-READ`, `OPS-PF-ENV-DUPLICATE`, `OPS-PF-ENV-INVALID-KEY`, `OPS-PF-ENV-UNCLOSED-QUOTE` | A18 | PM | 3 | env-parse | PFR | F | 0 | `[]` | N | - | env 不可读、重复、非法 key、引号未闭合 |
| `OPS-PF-SELECTOR-REMOTE-TCP`, `OPS-PF-SELECTOR-REMOTE-SSH`, `OPS-PF-SELECTOR-DOCKER-HOST-TCP`, `OPS-PF-SELECTOR-DOCKER-HOST-SSH`, `OPS-PF-SELECTOR-DOCKER-CONTEXT`, `OPS-PF-SELECTOR-DOCKER-TLS`, `OPS-PF-SELECTOR-FLAG-ENV-CONFLICT` | A18 | PM | 2 | endpoint-select | PFR | F | 0 | `[]` | N | - | remote context 与所有 selector env/conflict 在 Engine call 前拒绝 |
| `OPS-PF-COMPOSE-VERSION` | A18 | PM | 5 | compose-version | PFR | R | 0 | `[]` | N | - | Compose <2.24 |
| `OPS-PF-COMPOSE-RENDER`, `OPS-PF-COMPOSE-SERVICE`, `OPS-PF-COMPOSE-VOLUME`, `OPS-PF-COMPOSE-ENVFILE` | A18 | PM | 5 | compose-render | PFR | R | 0 | `[]` | N | - | render 失败、缺 service/volume、app env_file 偷换 |
| `OPS-PF-SEED-EMPTY-MISSING`, `OPS-PF-SEED-EMPTY-SHORT`, `OPS-PF-SEED-EMPTY-SENTINEL`, `OPS-PF-SEED-INIT-NONEMPTY` | A18 | PM | 4 | config-matrix | PFR | R | 0 | `[]` | N | - | seed empty/initialized 反例 |
| `OPS-PF-BIN-DATABASE-MISSING`, `OPS-PF-BIN-DATABASE-DEV`, `OPS-PF-BIN-DATABASE-REMOTE-NOSSL` | A18 | PM | 4 | config-matrix | PFR | F | 0 | `[]` | N | - | binary DATABASE_URL required/sentinel/remote SSL，禁止 Engine call |
| `OPS-PF-MANAGED-APPDB-NONEMPTY`, `OPS-PF-MANAGED-PGUSER-MISSING`, `OPS-PF-MANAGED-PGDB-MISSING`, `OPS-PF-MANAGED-PGPASSWORD-MISSING`, `OPS-PF-MANAGED-PGPASSWORD-DEV` | A18 | PM | 4 | config-matrix | PFR | R | 0 | `[]` | N | - | managed APP_DATABASE_URL forbidden 与 POSTGRES_* |
| `OPS-PF-EXTERNAL-APPDB-MISSING`, `OPS-PF-EXTERNAL-APPDB-DEV`, `OPS-PF-EXTERNAL-APPDB-REMOTE-NOSSL` | A18 | PM | 4 | config-matrix | PFR | R | 0 | `[]` | N | - | external APP_DATABASE_URL required/sentinel/SSL |
| `OPS-PF-AUTH-MISSING`, `OPS-PF-AUTH-SHORT`, `OPS-PF-AUTH-SENTINEL`, `OPS-PF-HTTP-COMPOSE-ADDR` | A18 | PM | 4 | config-matrix | PFR | R | 0 | `[]` | N | - | auth secret 与 compose HTTP_ADDR |
| `OPS-PF-AVATAR-DRIVER`, `OPS-PF-AVATAR-BIN-ROOT-RELATIVE`, `OPS-PF-AVATAR-BIN-ROOT-MISSING`, `OPS-PF-AVATAR-BIN-ROOT-PERMISSION` | A18 | PM | 4 | config-matrix | PFR | F | 0 | `[]` | N | - | binary fixture 的 driver/root/read-write 反例，禁止 Engine call |
| `OPS-PF-AVATAR-COMPOSE-ROOT`, `OPS-PF-AVATAR-COMPOSE-MOUNT`, `OPS-PF-AVATAR-COMPOSE-REQUIRE-MOUNT` | A18 | PM | 4 | config-matrix | PFR | R | 0 | `[]` | N | - | compose root/mount/require-mount 反例 |
| `OPS-PF-TG-HALF`, `OPS-PF-TG-USERNAME`, `OPS-PF-TG-TOKEN-EXAMPLE` | A18 | PM | 4 | config-matrix | PFR | F | 0 | `[]` | N | - | TG 半配置、username、示例 token |
| `OPS-PF-CMD-MISSING` | A18 | PM | 6 | dependency-check | DEP | F | 0 | `[]` | N | - | preflight 必需本机命令缺失，首个 Engine call 前拒绝 |
| `OPS-BK-SUCCESS-RUNNING` | A19 | BR | 0 | complete | BKR | T | 1 | `[owner]` | S | - | running backup 必须 stop/pipeline/package-verify/state-restore/health/publish，business before=after |
| `OPS-BK-SUCCESS-EXITED` | A19 | BR | 0 | complete | BKE | T | 1 | `[owner]` | S | - | exited backup 必须 pipeline/package-verify/publish，不 stop/start，business before=after |
| `OPS-BK-CMD-MISSING` | A20 | PM | 6 | dependency-check | DEP | F | 0 | `[]` | N | - | backup 必需本机命令缺失，首个 Engine call 前拒绝 |
| `OPS-BK-PATH-EMPTY`, `OPS-BK-PATH-ROOT`, `OPS-BK-PATH-PROJECT-ROOT`, `OPS-BK-PATH-PARENT-MISSING`, `OPS-BK-PATH-LEAF-EXISTS`, `OPS-BK-PATH-SYMLINK`, `OPS-BK-PATH-SPECIAL` | A20 | PM | 2 | path-validate | PATH | F | 0 | `[]` | N | - | output lexical/filesystem 路径反例在 Engine 前拒绝 |
| `OPS-BK-PATH-AVATAR-OVERLAP`, `OPS-BK-PATH-PG-OVERLAP` | A20 | PM | 5 | target-identity | BKP | L | 0 | `[owner]` | L | - | output 与 runtime source avatar/pg volume mount 重叠，持锁查询后但 target mutation 前拒绝 |
| `OPS-BK-FAIL-PGDUMP` | A20 | BR | 8 | backup-pg-dump | BKF | T | 1 | `[owner]` | S | - | pg_dump 注入失败，无 published package，原 running 恢复 |
| `OPS-BK-FAIL-AVATAR-TAR` | A20 | BR | 8 | backup-avatar-tar | BKF | T | 1 | `[owner]` | S | - | avatar tar 注入失败，无 published package，原 running 恢复 |
| `OPS-BK-FAIL-MANIFEST` | A20 | BR | 8 | backup-manifest | BKF | T | 1 | `[owner]` | S | - | manifest 注入失败，无 published package，原 running 恢复 |
| `OPS-BK-FAIL-HEALTH` | A20 | BR | 8 | backup-health | BKF | T | 1 | `[owner]` | S | - | 原 running 已恢复 Engine predicate 但 health oracle 失败，不 publish |
| `OPS-BK-FAIL-PUBLISH`, `OPS-BK-HALF-PACKAGE-CLEANUP` | A20 | BR | 8 | backup-publish | BKF | T | 1 | `[owner]` | S | - | 原子发布失败/半包清理，无 final package/temp residual |
| `OPS-BK-APP-ABSENT`, `OPS-BK-APP-CREATED`, `OPS-BK-APP-PAUSED`, `OPS-BK-APP-RESTARTING`, `OPS-BK-APP-REMOVING`, `OPS-BK-APP-DEAD`, `OPS-BK-APP-UNKNOWN` | A20 | PM | 5 | target-identity | BKP | L | 0 | `[owner]` | L | - | backup 对 absent/created/paused/restarting/removing/dead/unknown-or-inconsistent 全拒绝 |
| `OPS-LOCK-IMAGE-VOLUME` | A20 | PM | 5 | image-safety | BKP | R | 0 | `[]` | N | - | Config.Volumes 非空，create 前拒绝 |
| `OPS-LOCK-NETWORK-DEFAULT`, `OPS-LOCK-MOUNT-INVALID`, `OPS-LOCK-LABEL-INVALID`, `OPS-LOCK-STATE-INVALID` | A20 | PM | 7 | lock-post-inspect | LCK | L | 0 | `[invalid-candidate]` | L | - | post-create network/mount/label/state 失败，仅按 ID 清理 |
| `OPS-LOCK-BUSY`, `OPS-LOCK-STALE-LIVEPID`, `OPS-LOCK-STALE-HELPER` | A20 | PM | 7 | lock-acquire | LCK | L | 0 | `[existing-owner,contender]` | L | - | busy、PID live、helper present 不破锁 |
| `OPS-LOCK-STALE-BREAK-OK` | A20 | CO | 0 | complete | LCO | L | 0 | `[stale-candidate,new-owner]` | L | - | authorized breaker 只删 candidate ID，再正常 acquire/release |
| `OPS-LOCK-RACE-DOUBLE-BREAKER`, `OPS-LOCK-RACE-BREAKER-ACQUIRE` | A20 | PM | 7 | lock-acquire | LCK | L | 0 | `[stale-candidate,winner,loser]` | L | - | loser 不得删除 winner ID |
| `OPS-LOCK-RACE-RELEASE-NEW` | A20 | CO | 0 | complete | LCO | L | 0 | `[old-owner,new-owner]` | L | - | old release/trap 不得删除 new owner ID |
| `OPS-LOCK-RACE-DELAYED-HELPER` | A20,A22 | PM | 7 | helper-fence | LCK | L | 0 | `[old-helper,new-owner]` | L | - | no-helper 查询后才到 daemon 的旧 helper fence 不得进入 target operation，新 owner 最多 fail-closed |
| `OPS-LOCK-ALIAS-BK-BK`, `OPS-LOCK-ALIAS-BK-RS` | A20 | PM | 7 | lock-acquire | LCK | L | 0 | `[owner,contender]` | L | - | 首个 backup 持锁，alternate realpath 第二操作被拒 |
| `OPS-CLEANUP-RESIDUAL` | A20,A22 | CO | 11 | cleanup | COR | L | 0 | `[owner]` | S | - | operation cleanup 预期 fail/oracle pass，harness 清空，不掩盖 production 泄漏 |
| `OPS-RS-SUCCESS-RUNNING` | A21 | RS | 0 | complete | RSR | T | 1 | `[owner]` | S | - | after=package oracle，stop/replace/verify/start/health 全部完成 |
| `OPS-RS-SUCCESS-EXITED` | A21 | RS | 0 | complete | RSE | T | 1 | `[owner]` | S | - | after=package oracle，原 exited 不 stop/start/health，最终仍 exited |
| `OPS-RS-CMD-MISSING` | A22 | PM | 6 | dependency-check | DEP | F | 0 | `[]` | N | - | restore 必需本机命令缺失，首个 Engine call 前拒绝 |
| `OPS-RS-PATH-EMPTY`, `OPS-RS-PATH-ROOT`, `OPS-RS-PATH-PROJECT-ROOT`, `OPS-RS-PATH-MISSING`, `OPS-RS-PATH-SYMLINK`, `OPS-RS-PATH-SPECIAL` | A22 | PM | 9 | path-validate | PATH | F | 0 | `[]` | N | - | input lexical/filesystem 路径反例在 Engine 前拒绝 |
| `OPS-RS-PATH-AVATAR-OVERLAP`, `OPS-RS-PATH-PG-OVERLAP` | A22 | PM | 5 | target-identity | RSP | L | 0 | `[owner]` | L | - | input 与 runtime source avatar/pg volume mount 重叠，持锁查询后但 target mutation 前拒绝 |
| `OPS-RS-CHECKSUM`, `OPS-RS-SCHEMA`, `OPS-RS-SYMLINK`, `OPS-RS-TAR-ABSOLUTE`, `OPS-RS-TAR-DOTDOT`, `OPS-RS-TAR-LINK`, `OPS-RS-TAR-DEVICE`, `OPS-RS-INPUT-CHANGED` | A22 | PM | 9 | package-validate | RSP | L | 0 | `[owner]` | P | - | staged package checksum/schema/file/tar/input-race 全拒绝 |
| `OPS-RS-CONFIRM`, `OPS-RS-PROJECT` | A22 | PM | 9 | package-validate | RSP | F | 0 | `[]` | N | - | typed confirm/project 可在 Engine 前判定 |
| `OPS-RS-SOURCE-IMAGE`, `OPS-RS-SOURCE-MOUNT` | A22 | PM | 5 | target-identity | RSP | L | 0 | `[owner]` | L | - | source/runtime image/mount mismatch |
| `OPS-RS-APP-ABSENT`, `OPS-RS-APP-CREATED`, `OPS-RS-APP-PAUSED`, `OPS-RS-APP-RESTARTING`, `OPS-RS-APP-REMOVING`, `OPS-RS-APP-DEAD`, `OPS-RS-APP-UNKNOWN` | A22 | PM | 5 | target-identity | RSP | L | 0 | `[owner]` | L | - | restore 对全部 unsupported Engine state 拒绝 |
| `OPS-BK-SELECTOR-REMOTE`, `OPS-BK-SELECTOR-CONFLICT` | A20 | PM | 2 | endpoint-select | PFR | F | 0 | `[]` | N | - | backup remote/env selector pre-Engine 拒绝 |
| `OPS-RS-SELECTOR-REMOTE`, `OPS-RS-SELECTOR-CONFLICT` | A22 | PM | 2 | endpoint-select | PFR | F | 0 | `[]` | N | - | restore remote/env selector pre-Engine 拒绝 |
| `OPS-RS-FAIL-DB-REPLACE` | A22 | RF | 10 | restore-db-replace | RSF | T | 1 | `[owner]` | S | - | DB replace 失败，failure-stop 证明 app exited，data envelope 如实可读/缺失/不可读 |
| `OPS-RS-FAIL-AVATAR-REPLACE` | A22 | RF | 10 | restore-avatar-replace | RSF | T | 1 | `[owner]` | S | - | avatar replace 失败，failure-stop 证明 app exited，data envelope 如实记录 |
| `OPS-RS-FAIL-VERIFY` | A22 | RF | 10 | restore-verify | RSF | T | 1 | `[owner]` | S | - | exact-generation verify 失败，failure-stop 证明 app exited，data envelope 如实记录 |
| `OPS-RS-FAIL-START` | A22 | RF | 10 | restore-start | RSF | T | 1 | `[owner]` | S | - | data verify 通过但 start 失败，再 stop/inspect 到 exited，不报成功 |
| `OPS-RS-FAIL-HEALTH` | A22 | RF | 10 | restore-health | RSF | T | 1 | `[owner]` | S | - | data verify/start 通过但 health 失败，再 stop/inspect 到 exited，不报成功 |
| `OPS-LOCK-ALIAS-RS-BK`, `OPS-LOCK-ALIAS-RS-RS` | A22 | PM | 7 | lock-acquire | LCK | L | 0 | `[owner,contender]` | L | - | 首个 restore 持锁，alternate realpath 第二操作被拒 |

- CodeStable 标准产物固定为 `v1-hardening-design-review.md`、`v1-hardening-implementation.md`、`v1-hardening-review.md`、`v1-hardening-qa.md`、`v1-hardening-acceptance.md`、`v1-hardening-evidence-pack.md`、`v1-hardening-evidence-pack-results.json`、`v1-hardening-dod-contract-results.json`、`v1-hardening-dod-results.json`、`v1-hardening-gate-results.json`。`v1-hardening-dod-contract-results.json` 是当前 `.codestable/tools/codestable-goal-consistency-gate.py` 硬性消费的 canonical artifact，status 必须为 `passed|generated`；Markdown/JSON 只引用仓库相对 evidence path，不存在“另一个未命名 results”。
- `npm run test:v1-hardening` 与 `scripts/test-v1-ops-contract.sh` 必须在 Makefile `test` target 接线；CMD-001 日志必须真实包含两者。CMD-002 保留前端定向复跑，CMD-004 保留 ops catalog/results validator 反例与镜像定向门禁，不得成为游离于长期基线的一次性检查。
- H1=接受时的 canonical risk intake 是 `.codestable/issues/2026-07-22-auth-hardening/auth-hardening-report.md`；它是 owner-approved open risk 的持久入口，不冒充已设计/已修复，也不阻塞本 feature 代码范围。H1=不接受时不得创建这个“延后”报告。

### 1.6 Top 3 风险、依赖与关键假设

Top 3 风险：

1. 全站状态清扫误改既有领域行为或让旧数据看似最新。缓解：StateNotice 不拥有数据；先做路由状态矩阵；API/schema 零变更；每页正常路径与 401 回归。
2. 桌面表格与移动卡片出现内容/动作漂移。缓解：同一 items 状态、同一 customer item 投影契约；375/768/769/desktop 同一 fixture 对拍；QuickNote 只有一个保存编排。
3. restore 操作错误 project，或 endpoint/来源/cleanup 竞态绕过并发锁，导致停机或数据损坏。缓解：显式 context 一次解析并 pin host、来源 descriptor 与 physical target identity 分离、volume-free/network-none Engine-side lock 的 post-create inspect、全生命周期 immutable-ID ownership、绑定 lock generation 的 deterministic helper fence、operation/harness cleanup 分层、app-present runtime label/inspect 核对、project-bound typed confirm、realpath/selector/race 隔离矩阵、包校验先于破坏、sentinel 始终隔离、破坏失败后 app stopped/逐项 data envelope 忠实记录、exact-generation verify 与 start/health 才恢复原运行状态。

非显然依赖：

- Docker/Compose、pg_dump、tar、sha256 工具与 avatar-manifest 镜像入口；任一缺失阻塞运维演练步骤。
- 真实浏览器证据需要可控 synthetic 账号、客户、订单、档期、提醒和 375 viewport；真实客户 PII 不进截图。
- Telegram 真外部 transport/binding 建议沿用 2026-07-17 owner-attested 事实，同一 synthetic fixture 的次日 digest payload 与 dashboard 关联必须 fresh 验证；H2 若选择 fresh 真机，证据由 owner 提供且脱敏。
- ECS 的 TLS、网络边界、定时器、异地介质与断电级 filesystem durability 无法由仓库自动证明，只能由 README checklist + owner attestation 记录；生产 app 的 mount/write/fsync/dir-sync probe 与恢复演练仍须留事实证据。
- design 获批后、写代码前，按 attention.md 由 owner 选择当前 branch 或新 worktree；本 design 不替 owner 选。

关键假设：

1. 375 指 CSS viewport 宽度，竖屏；DPR 只记录证据，不改变验收宽度。
2. schedule-calendar 已拍板的“移动端只查档期”继续有效。
3. 当前 compose 是首版主要可执行部署轨，外部 PostgreSQL 轨仍需保留。
4. 生产前必须有 TLS 与受控网络边界，但具体代理/云产品不在仓库选型。
5. 认证模型是否延后不是默认假设；必须由 owner 在 H1 明确接受 residual 并授权独立持久入口，否则本 design 退回扩范围。

### 1.7 证据、验证命令与交付物

证据类型：

- 状态语义：前端确定性测试 + 浏览器 DOM/无障碍观察。
- 三条移动轻路径：375 CSS px 浏览器操作、截图与 viewport 元数据；Customers 另有 768/769/desktop breakpoint 同 fixture 证据与 200% 字体放大记录。
- 401：API stub/integration + token 清除和登录重定向观察。
- server hardening：Go 单测与进程级构造/日志捕获。
- 运维：逐 mode/key preflight 矩阵、shell failure injection、显式 synthetic target 的隔离 compose 真实 backup→mutate→restore→verify、sentinel project 不变断言。
- V1 全链路：fresh synthetic fixture 的浏览器/API/次日 digest payload/dashboard 五卡证据；TG true-external transport 按 H2 使用既有 owner attestation 或 fresh 脱敏证据。

必跑命令：

- CMD-001：make check
- CMD-002：cd frontend && npm run test:v1-hardening
- CMD-003：bash -n scripts/*.sh
- CMD-004：./scripts/test-v1-ops-contract.sh && docker build -t crm:v1-hardening .
- CMD-005：./scripts/v1-ops-smoke.sh（只允许独立 compose project 与 synthetic 数据）
- CMD-006：git diff --check

基线风险：CMD-001 当前通过但有既有 chunk warning；Testcontainers 沿用串行门禁。CMD-002、CMD-004 的 ops-contract 前缀与 CMD-005 是本 feature 新增入口，implementation 完成前不存在不算基线红灯；STEP-001 先物化 catalog，implementation 再把 frontend 与 ops-contract 两个定向测试接入 Makefile，使后续 CMD-001 持续覆盖。

最终交付物：

- 页面状态语义外壳、正式路由状态接入、客户移动卡片、移动档期/shell polish 与相关测试。
- HTTP IdleTimeout 与启动日志脱敏测试。
- production-preflight、backup-compose、restore-compose、v1-ops-smoke 四个脚本入口。
- README 使用/部署/备份/凭证说明与 canonical `v1-hardening-regression.yaml` / `v1-hardening-ops-case-catalog.yaml`。
- D8 固定路径下的 browser/viewport、命令、运维演练与 review/QA/acceptance/evidence pack/DoD/gate 产物。
- H1 批准后、goal package 前的 `.codestable/issues/2026-07-22-auth-hardening/auth-hardening-report.md`；若 H1 不接受则本 design 扩范围而不是生成该入口。
- roadmap items 与主文档状态在 acceptance 后回写。

清洁度规则：禁止 console.log/fmt.Print 调试输出、debugger、临时 TODO/FIXME/XXX、注释掉实现、无用 import、真实 secret/PII、本机绝对备份路径、半成品 backup 包和测试 compose 残留。结构化运行日志是功能所需例外，但不得含 secret、Bot API URL/正文、原始本机路径或客户正文。

## 2. 名词与编排

### 2.1 名词层

#### 现状

- frontend/src/App.tsx 的 RequireAuth 仅按本地 token 守卫路由；frontend/src/api/client.ts 在任意 401 清 token，各页面再跳 /login。
- Dashboard/Reminders/Settings 已有相对完整的 loading/error/retry；CustomersPage、CustomerDetailPage、PackagesPage 的初始错误仍缺统一 role/retry，有的错误与 empty/旧列表可同时出现。
- CustomersPage 用最小宽 560px 的表格承载移动页，QuickNote 在最右列；移动“搜客户/记备注”会依赖局部横向滚动。
- CalendarPage 已有 42 日月历、移动点阵、只读 desktop-schedule-actions 分支、drawer focus trap、retry 与 account timezone fail-closed。
- AppShell 在移动显示七项 bottom-nav；Customers/Calendar 已是直接入口。
- README 已有双轨部署和头像一致备份命令，但没有稳定脚本/完整 V1 使用入口，且 Telegram token 说明与当前 server 装配不一致。
- backend/cmd/server/main.go 已有 5 秒 ReadHeaderTimeout 和 signal-aware shutdown，没有 IdleTimeout；启动失败日志可能透出底层路径。

#### 变化

1. 新增页面状态展示契约：

| 基础 kind / 子态 | 可观察语义 | retry | ARIA |
|---|---|---|---|
| loading | 明确正在读取，不展示“暂无”或旧 ready | 无 | `role=status`、所属区 `aria-busy=true` |
| empty | 请求成功且集合确为空；搜索空态带当前过滤上下文 | 无 | 普通说明 |
| error(retryable) | 首次读取失败，不冒充 empty；保留输入 | 必有 | `role=alert` |
| error(terminal) | 404/无效链接等明确不可重试结果 | 无 | `role=alert` |
| ready(current) | 当前成功数据 | 无 | 数据自身语义 |
| ready(stale, refreshError) | 上次成功数据 + 明确 stale 错误 | 必有 | 数据语义 + `role=alert` refresh notice |
| unauthorized | 清 token 并回登录，不在业务页继续渲染 | 无 | 登录页接管 |

正式 screen route 与共享 shell 的落地矩阵：

| route / surface | 初始读取 | loading / empty | initial error / retry | refresh / 401 |
|---|---|---|---|---|
| `/login` | 无页面读取；只有 login action | page N/A；submit busy 属 action state | 登录/网络错误保留用户名并可重提 | 401 是登录失败，不走受保护页 unauthorized |
| protected `AppShell` | `GET /me` 账号时区 | loading 明示；无 empty | 失败 role=alert + retry，Calendar 写入保持 fail-closed | 有旧 timezone 时失败标 stale；401 清 token 回登录 |
| `/dashboard` | dashboard aggregate | initial loading；零值卡是 ready，不叫页面 empty | 初始失败 + retry | 保留旧数据时 stale notice；401 回登录 |
| `/customers` | customer list | loading；成功零结果为带筛选上下文 empty | 初始失败 + retry，筛选保留 | stale notice；401 回登录 |
| `/customers/new` | 无页面级初始读取；CustomerPicker 按需读候选 | page N/A；picker 自有 loading/empty | picker 可 retry；create action error 保留表单 | child/action 401 回登录 |
| `/customers/:id` | customer detail；tab 按需读取 | initial loading；detail 不存在不是 empty | 网络/5xx 可 retry；404 terminal error；tab 各自可恢复 | detail/tab stale 明示；401 回登录 |
| `/orders` | order list / focused order | loading；成功零结果 empty | 初始失败 + retry，filter/focus 保留 | stale notice；401 回登录 |
| `/calendar` | `GET /me` 后读取 42 日 slots | skeleton loading；空日是 ready 内 drawer empty | 初始失败 + retry；无 timezone 时 terminal config error/fail-closed | stale grid 明示；401 回登录 |
| `/packages` | package list | loading；成功零结果 empty | 初始失败 + retry，status filter 保留 | load-more error 不抹旧数据；401 回登录 |
| `/reminders` | reminder list | loading；成功零结果 empty | 初始失败 + retry，filter 保留 | action/refresh error 不冒充 empty；401 回登录 |
| `/settings` | settings singleton | loading；无 empty | 初始失败 + retry | stale form 禁止误保存未知值；401 回登录 |

`/` 与 `*` 仅 redirect，不渲染页面读取状态，不计为漏测 route；矩阵覆盖 App.tsx 的 10 个正式 screen route 与共享 AppShell。页面/子区必须在 canonical regression 清单逐项留 terminal 结果。

2. 新增 CustomerResult 的响应式投影契约。父页面传同一 customer item、打开详情、保存备注后的刷新与 unauthorized 回调；移动卡片不得手写 DTO 或重新请求。375/768/769/desktop 同 fixture 证明投影边界、内容/动作等价与单请求。
3. QuickNote 补 success、failure-preserve、Escape focus return 与可访问错误语义；NotesPanel 同步失败保留与 success 可感知规则。
4. 运维 CLI 使用显式 mode/seed-state/env/compose/project/input/output/typed-confirm 参数与稳定 exit code；输出只包含阶段、key 名、artifact 相对名、checksum 和修复动作，不含 secret value。
5. 一致备份包固定包含 database.sql、avatar-volume.tgz、avatar-manifest.json、SHA256SUMS、metadata.json；metadata 按 D5.4 的 versioned schema，不含 DATABASE_URL、token、password、chat id、客户正文或绝对路径。
6. http.Server 增 60 秒 IdleTimeout；启动错误对外只保留 operation/config key/error class。
7. V1 回归与证据严格写入 D8 canonical paths；A24 恢复 roadmap 精确完成链，export 独立为 A27。

接口示例：

    StateNotice(kind="error", message="客户列表加载失败", onRetry=reload)
    -> role=alert 的错误文案 + “重试”按钮；不渲染“暂无客户”

    CustomerResult(customer, mode="mobile", onOpen, onQuickNoteSaved)
    -> 同一列表数据的纵向卡片；保存备注后仍停在当前搜索上下文

    production-preflight --mode compose-managed-db --seed-state initialized \
      --env-file /secure/crm.env --compose-file ./docker-compose.yml \
      --docker-context default --project-name crm-prod
    -> exit 0 + stage/key=status；永不打印实际值/路径
    -> dev sentinel / 模式冲突 / compose identity 错误时稳定非零 + key/阶段与修复建议

    restore-compose --env-file /secure/crm.env --compose-file ./docker-compose.yml \
      --docker-context default --project-name crm-prod \
      --input /backup/20260722 --confirm-project crm-prod
    -> package/target/confirm 先验通过才停止并替换；manifest verify 后按原运行状态 start-or-stay-stopped

##### Interface 设计检查：StateNotice

- Module：新增的 in-process 呈现组件；只集中页面读取状态的语义和可恢复按钮。
- Interface：discriminated props：`loading(message)`、`empty(message)`、`error(message,retryable,onRetry?)`、`refresh-error(message,onRetry)`；retryable/refresh-error 的 onRetry 必填，terminal error 禁止伪造 retry。组件不拥有请求、ready 数据、token 或路由。
- Seam：各页面 JSX 的状态分支；页面和浏览器测试通过可见 DOM/role 观察。
- Depth / locality：删除后 ARIA、retry、loading-vs-empty 规则会重新散回多个页面，模块有真实 leverage；不会把业务状态藏进万能组件。
- Dependency strategy：in-process。
- Adapter：无；这不是外部 seam，不造 production/test adapter。
- Test surface：渲染优先级、loading/empty/initial error、ready(current)、ready(stale+refresh-error)、retry props invariant 与 401 不渲染分流。

##### Interface 设计检查：运维 CLI

- Module：三个职责单一 shell CLI + 一个隔离 smoke 编排。
- Interface：显式 deployment/seed/source/docker-context flags；backup/restore 固定 initialized、app-present、pinned local-Docker target 并公开可选 break-stale-lock；配置来源与 physical target identity 分离；versioned backup/ops-results schema、稳定 exit code、stdout/stderr 脱敏、volume-free/network-none Engine-side lock、immutable-ID ownership 与 stop/restore-original-state ordering。
- Seam：docker/pg_dump/tar/avatar-manifest 命令边界；失败测试可通过 PATH 注入 fake executable。
- Depth / locality：trap、校验、冻结、原子发布、失败关闭集中在脚本；删除后复杂度会回到 README 复制粘贴。
- Dependency strategy：local-substitutable；真实 compose 用 production adapter，失败分支用 fake command。
- Adapter：不额外建面向对象 adapter；shell 的 PATH/环境变量已经是真实替换 seam。
- Test surface：D5.2 全矩阵、effective endpoint selector/pinning、initialized target/seed secret rejection、env parser injection、错误 target/confirm、app-absent/source-runtime mismatch、同 project/different compose realpath 四组合、image VOLUME/default network/post-create inspect、immutable-ID release/stale/race、命令失败、半包/匿名 volume 清理、restore 校验先行、tar traversal/symlink、成功 package oracle、destructive failure after-state、原运行状态、sentinel project 与 D8 machine results schema。

### 2.2 编排层

~~~mermaid
flowchart TD
    A["登录并进入 AppShell"] --> B{"选择移动轻路径"}
    B -->|查档期| C["Calendar 读取账号时区与 42 天档期"]
    C --> D{"loading / error / ready current|stale"}
    D -->|error| E["显示可恢复错误并重试"]
    D -->|ready| F["选择日期并查看只读 drawer"]
    B -->|搜客户| G["Customers 按 query/channel/status 读取"]
    G --> H{"loading / empty / error / ready current|stale"}
    H -->|error| I["保留筛选并重试"]
    H -->|ready| J["375px 客户卡片"]
    B -->|记备注| J
    J --> K["展开 QuickNote 并保存"]
    K --> L{"成功 / 失败 / 401"}
    L -->|成功| M["确认、刷新并保留列表上下文"]
    L -->|失败| N["保留输入并重试"]
    L -->|401| O["清 token，回登录"]

    P["生产运维"] --> Q["只读 preflight"]
    Q -->|pass| R["锁定显式 target，按原状态冻结 app 并生成一致备份包"]
    R --> S["隔离环境 restore"]
    S --> T{"checksum + manifest verify"}
    T -->|pass| U["恢复原运行状态并跑 V1 回归"]
    T -->|fail| V["保持 app stopped，给出恢复动作"]
~~~

#### 现状

- 页面各自用 boolean/null 组合表达请求状态，拓扑相似但语义不统一。
- Customers 的查询、表格行点击和 QuickNote 嵌套在同一 table row；QuickNote 通过 stopPropagation 避免误打开详情。
- Calendar 的 loadSlots、query deep-link、drawer/dialog 互斥和移动只读已经形成稳定分支。
- API client 是 401 清 token 的单点；页面负责 navigate。
- 备份/恢复目前是 README 线性命令块，set -e 中途失败时缺少可靠的“恢复 app/保持停止”分流。

#### 变化

1. 页面数据读取统一为“unauthorized → initial loading/error → empty → ready(current|stale)”判定，再渲染 StateNotice 或 ready 内容；不把 fetch 移进全局 hook。
2. Customers 的 ready 内容分为 desktop table 与 mobile cards；查询、总数、reloadTick 和 QuickNote 保存仍是单一编排，375/768/769/desktop 不触发额外请求。
3. QuickNote 的打开、保存、失败、取消和 focus return 成为显式小状态机；重复提交与 IME 守护保持。
4. Calendar 只补状态语义、375px overflow/focus/长文本/200% 字体放大与 read-only 证据，不重写排期 journal/幂等编排。
5. 生产 preflight 是 backup/restore 的前置 gate；两者固定 initialized、app-present target 且 seed secret 已移除，以唯一显式 context 解析并 pin local Docker host；Engine-side lock 仅在 volume-free/network-none/immutable-ID post-inspect 后成立。backup 原子发布，restore 采用 validate → stop-if-running → replace → verify → restore-original-state 的 fail-closed 顺序；每个分支按 D8 oracle_class 写 machine case。
6. canonical V1 回归清单聚合既有域验收入口，并补三条移动路径、运维演练、roadmap 精确链路和独立 export 回归节点。

流程级约束：

- 错误语义：首次 load error 不展示 empty；action error 保留输入；refresh error 只能作为 stale ready 子态并显式标识。
- 幂等性：前端重复 note submit 仍只发一次；backup 对新输出目录原子发布；restore 不声称幂等，必须显式确认并在 verify 后开放流量。
- 顺序：401 先清 token 后跳转；backup 先锁 target/记录运行状态/冻结再取三类资产；restore 先校验包/target/confirm 再破坏、最后 verify 后恢复原运行状态。
- 并发：backup/restore 用 `Docker Engine ID + normalized project + 固定资源 namespace` 的 physical-target-hash 确定 Engine 侧 lock container 名，并以 Docker name uniqueness 原子拒绝并发；compose/env realpath、context/endpoint、client local lock dir 或 source fingerprint 不参与锁 key。同一 Engine/project 的等价来源/context alias/client 必须竞争同一 Engine-side mutex；首版显式 context 只接受 pinned local Unix endpoint，使 stale PID 可在本机 fail-closed 判定。acquire/release/stale-break 全程保存并只操作完整 immutable container ID；name 只用于 atomic create/初次 stale candidate lookup，绝不用于删除。不与同 target 在线 mutator/maintenance 并跑，破锁需 public flag + matching immutable candidate + dead local PID + no matching helper，竞态失败即关闭。
- 可观测：UI error/status 用 role；脚本输出阶段与 artifact checksum；server 日志保留分类但不含路径/secret。
- 安全：路径引用加引号并拒绝 root/空/overlap/symlink/special-file/tar traversal；允许 owner 显式选择仓库外 parent；env 值不回显；restore 只接受 D5.4 完整包。
- 扩展点：未来新增移动路径只扩 canonical 回归清单/响应式投影；auth-hardening 只按 H1 的独立入口推进，不塞进 StateNotice 或脚本。

### 2.3 挂载点清单

1. 全路由状态入口：frontend/src/App.tsx、AppShell 与 10 个 screen route 的初始/子区读取分支 — 接入 D1 route matrix 与 StateNotice，不改 fetch owner。
2. AppShell 移动主导航：frontend/src/components/AppShell.tsx 的 bottom-nav — 修改并验证 Customers/Calendar 直接入口、触控区与 safe area。
3. 客户入口：/customers 页面结果区与 QuickNote 公共动作 — 新增移动卡片投影并统一状态/焦点。
4. 档期入口：/calendar 月历与当日 drawer — 修改移动只读状态呈现与长文本/错误恢复。
5. 平台 HTTP 入口：backend/cmd/server 的 http.Server construction — 增 IdleTimeout 与脱敏启动错误。
6. 运维入口：docker-compose.yml、scripts/production-preflight.sh、backup-compose.sh、restore-compose.sh、v1-ops-smoke.sh 与 Makefile/README 链接 — 参数化 app env file，新增显式 context/target/preflight/backup/restore/smoke。
7. 证据入口：v1-hardening-regression.yaml、v1-hardening-ops-case-catalog.yaml、evidence/browser|commands|ops 与 CodeStable 标准 report/results — 按 D8 固定路径生成、验证与引用。

拔除检查：删除上述 UI 投影/状态接入、运维命令和 IdleTimeout 增量后，业务 API/schema/data 均保持不变，系统退回现有桌面页面、横向客户表格和 README 命令块；没有隐藏 migration、定时任务或第三方注册项。

### 2.4 推进策略

1. `STEP-001` 状态与证据契约骨架：建立 discriminated state、StateNotice、逐 route matrix、canonical regression/artifact 骨架，并按 D8 frozen inventory 逐项物化 ops case catalog；创建 `npm run test:v1-hardening`，后续 ops-contract test 同属 Makefile 长期基线。
   - 退出信号：loading/empty/error/ready current|stale/401 能独立观察；A1-A27 每项有唯一 pending record；catalog 148 个 ID 与 D8 inventory 集合完全相等，scenario/oracle/expected/stage-plan/fixture/action 物化合法且无重复；design-review/dod-contract/dod/gate 等 canonical artifacts 与 Makefile 长期接线契约固定。
2. `STEP-002` 页面状态接入：逐个正式 route/surface 接入初始读取、空态、terminal/retryable error、stale refresh 与 401，不改业务请求契约。
   - 退出信号：route matrix 无 error 当 empty、无 retryable initial error 缺 retry、无 stale 当 current、无未处理 401。
3. `STEP-003` 客户移动垂直切片：接通响应式卡片、搜索、详情与 QuickNote/NotesPanel 成功/失败/IME/focus。
   - 退出信号：375px 核心路径通过；375/768/769/desktop 同 fixture 边界/等价/单请求通过；200% 字体放大仍可完成；CustomerPicker 空 options 的 ArrowDown 不产生非法 index/`aria-activedescendant`，Tab/blur/close 后焦点与关闭行为稳定。
4. `STEP-004` 档期与 shell 移动收口：验证导航、42 日网格、drawer、空/密集/冲突、错误、字体放大和只读边界。
   - 退出信号：375px 查档期不横向溢出，核心控件可达，200% 字体可滚动完成，移动写入口不可见/不可聚焦。
5. `STEP-005` 平台运行 hardening：补 IdleTimeout、启动日志脱敏与 D5.2 三 mode production preflight/compose env 选择。
   - 退出信号：Go/config/进程测试与 A18 catalog 的 66 个 required case 均执行；逐 key、strict parser、exit 0/2/3/4/5/6、binary Engine=forbidden、compose identity、context pinning、initialized/seed 拒绝的 structured expected/observed 全部匹配；无 missing/unknown/duplicate；输出不含 secret/path value。
6. `STEP-006` 备份恢复与文档：实现 D5.3/D5.4 target/lock/package/fail-closed restore、sentinel 隔离 smoke，更新 README。
   - 退出信号：A19-A22 catalog 的每个 required case 均执行且集合完全相等；显式 local-context synthetic initialized/app-present target 的 backup stage-plan/package observation、restore package oracle/data envelopes、所有 Engine state、static/source-overlap path、realpath/immutable-ID/helper-fence race、image/staging/start/health/failure 全部 structured observed 与 expected 匹配；operation cleanup 不被 harness 掩盖；sentinel/tar/半包/匿名 volume/破坏后 exited 均按 oracle 通过；ops results 顶层 pass；文档命令可执行。
7. `STEP-007` 全量收口：运行 CMD-001～006、浏览器矩阵、roadmap 精确链路、独立 export、H1/H2/ECS attestation 与清洁度/范围审计，归档 canonical evidence。
   - 退出信号：A1-A27 与 V1 回归均 terminal；A24 精确链 pass、A27 export pass、H1/H2 有 owner 决策、无 unresolved blocking/important，roadmap acceptance 可据事实回写。

### 2.5 结构健康度与微重构

##### 评估

- 文件级 — frontend/src/pages/CustomersPage.tsx（192 行）：查询、筛选、桌面表格和 QuickNote 编排同属客户列表职责；新增移动投影若继续内联会形成第二套大 JSX，宜把新投影作为 feature 组件，不需要先搬现有行为。
- 文件级 — frontend/src/pages/CalendarPage.tsx（398 行）：月历/drawer/dialog 协调职责集中但接近偏大；本轮只改状态叶子与样式，不拆 journal/写入编排。
- 文件级 — frontend/src/pages/CustomerDetailPage.tsx（401 行）：多 tab/头像/档期协调偏胖；本轮仅收口页面状态与 NotesPanel 接口，不做职责重划。
- 文件级 — frontend/src/index.css（802 行）：已混合 shell、各域页面和 responsive 规则；是显著结构债。本轮新移动样式应放独立 feature stylesheet，避免继续堆叠；把旧 CSS 整体拆层会改变 cascade 风险，不是“只搬即可靠证明”。
- 文件级 — frontend/src/pages/PackagesPage.tsx（721 行）与 ScheduleSlotDialog.tsx（1120 行）：超过阈值，但本轮只验证/做叶子状态修正；拆分会涉及组件状态与调用语义。
- 文件级 — backend/cmd/server/main.go（249 行）、config.go（208 行）：新增 timeout/脱敏检查局部，不触发拆分。
- 目录级 — frontend/src/components 当前 7 个同层文件，新增 StateNotice 后为 8，尚未越过摊平触发边界；移动客户组件继续归 components/customers，状态组件若只一项先留 components 根，不新造单文件 ui 目录。
- 目录级 — scripts 当前只有 telegram-smoke.sh，本轮新增 4 个同属运维入口，命名按 verb-scope，尚不触发子目录重组。
- compound convention 检索只命中“前端工具链 + 薄人工组件边界清单”，没有现成目录/命名归属规则。

##### 结论：不做行为等价微重构

理由：本轮可通过“新逻辑落新组件/新 stylesheet”控制增量；对 index.css、PackagesPage、ScheduleSlotDialog 的真正拆分不是低风险纯移动，强塞为前置会扩大回归面。现有胖文件不阻塞本 feature。

##### 超出范围的观察

- frontend/src/index.css：后续可走 cs-refactor 建立 base/shell/domain/responsive 样式层，并以全路由视觉对拍证明 cascade 不变。
- PackagesPage、ScheduleSlotDialog、CustomerDetailPage：后续按状态编排与呈现边界拆分；本 feature 不改签名/调用拓扑。

## 3. 验收契约

### 3.1 关键场景清单

| ID | 输入 / 触发 | 期望可观察结果 | 证据 |
|---|---|---|---|
| A1 | fresh checkout 运行 CMD-001 | build/lint/串行测试/codegen 全绿；日志真实包含 `npm run test:v1-hardening` 与 `test-v1-ops-contract.sh`；chunk warning 单独记录；regression A1-A27 唯一，ops catalog 148 个 frozen case ID/expected 唯一且 schema 可解析；design-review 与 dod-contract/dod/gate results 全部使用 D8 canonical path | canonical command log + YAML/catalog validation |
| A2 | AppShell 与 10 个正式 screen route 首次加载 | route matrix 的 loading 可观察且不同时显示 empty/旧成功；无初始读取的 `/login`、`/customers/new` 明确 N/A | frontend test + browser |
| A3 | 各正式 collection surface 返回空集合 | 只有请求成功后显示带当前上下文的空态；singleton/redirect/N/A surface 不伪造 empty | frontend test + browser |
| A4 | 各正式读取首次 500/网络失败，另测 detail 404/无效链接 | retryable error 为 role=alert + retry 且恢复为 current ready；terminal error 无无效 retry；均不冒充“暂无” | frontend test + browser |
| A5 | 有旧数据时 refresh 失败 | 基础状态仍为 ready，但 freshness=stale；显示“上次成功数据”+ role=alert + retry，不显示 current/empty | frontend test |
| A6 | 任一受保护请求返回 401 | token 先清除，RequireAuth/页面跳 `/login`，业务页不渲染 unauthorized notice 或旧数据 | API/client test + browser |
| A7 | 375px 打开 AppShell | Customers/Calendar 直接可达；无页面横向 overflow；核心触控区≥44px、焦点可见、safe area 生效 | screenshot + DOM metrics |
| A8 | 375px 打开 Calendar，切月/今天/选日期 | 42 日网格可读；当日 drawer 显示空态或按稳定顺序显示档期 | browser |
| A9 | Calendar 密集日、跨日、长 note、冲突、归档客户/取消订单 | 点阵/摘要不破版，drawer 完整可滚；冲突/警示可辨识 | browser screenshot |
| A10 | Calendar current/stale load 500 后重试 | 初始失败不显示网格为成功；有旧 slots 时明确 stale；重试恢复；移动无写入口/隐藏焦点 | browser |
| A11 | 375px Customers 输入昵称/手机号/identity handle | loading 后显示移动卡片，结果可打开详情且不需横向滚动 | browser |
| A12 | Customers 无匹配、长昵称、多渠道与 500→retry | 空态带查询上下文；长文本不遮动作；失败保留筛选并恢复 | browser screenshot |
| A13 | 移动卡片点记备注、输入并保存 | 只创建一条备注；成功确认、输入清空、列表上下文保留 | API + browser |
| A14 | note 保存 500、连按 Enter、IME Enter、Escape、401 | 500 保留内容；连按不重复；composing 不提交；Escape 返回焦点；401 回登录 | frontend test + browser |
| A15 | CustomerPicker 空结果后 ArrowDown/Tab/blur | active descendant 不指无效 option；焦点/关闭行为稳定 | frontend test + keyboard |
| A16 | http.Server 构造与空闲连接 | ReadHeaderTimeout 仍 5s，IdleTimeout=60s；graceful shutdown 回归 | Go test |
| A17 | 启动 config/avatar 路径失败 | 日志保留操作/config key/error class，不含 secret、DATABASE_URL、token 或原始本机 root | Go test/log capture |
| A18 | D5.2 每个 mode、seed-state、key 与 effective Docker selector 条件的 synthetic 正反例 | 三种 mode 的 required/ignored/forbidden、seed empty/initialized、TG disabled/half/enabled、dev sentinel、remote DB SSL、strict parser、exit code 0/2/3/4/5/6 与 Compose 2.24/identity 全部有 oracle；缺必需命令在 Engine 前 dependency-check exit 6；binary 正反例 Engine policy 均为 forbidden；compose 明确传 context，local alias 同 Engine/hash；remote context、DOCKER_HOST tcp/ssh、DOCKER_CONTEXT 与 flag/env 冲突在首个 Engine call 前 exit 2；后续 argv 全部 pin 同一 host；backup/restore 固定 initialized 且非空 seed 被拒；输出无 value/path；每个 matrix row 有独立 machine case | script matrix test + ops results cases |
| A19 | 显式 local-context synthetic initialized、app-present target 执行 backup，旁置 sentinel project | seed password 已移除；volume-free app image + `--network none` lock/helper fence 经 immutable-ID post-inspect 才 acquired；原正常 running/Engine exited predicate 被记录/恢复；业务 before=after；BKR/BKE required stages 完整，backup_package 五件套、metadata v1、SHA/manifest/self-check 全部结构化通过并原子 publish；sentinel 不变；`backup-readonly`/lock/operation+harness cleanup oracle pass | ops smoke + results JSON |
| A20 | backup 的 command/path/pg_dump/tar/manifest/health/publish/Engine-side lock 任一步失败；另测正常 running/exited 与 created/paused/restarting/removing/dead/unknown/inconsistent/absent reject、image VOLUME/default-network/post-create-invalid、stale-lock 正反例、delayed helper 及其他竞态、首个 backup 持锁的两种 realpath alias | static path 在 Engine 前拒绝，source-volume overlap 在 target mutation 前拒绝；unsupported state/alias before=after；VOLUME 在 create 前拒绝，invalid create 只按 immutable ID 清理且 candidate/anonymous volume 零残留；旧 cleanup 不能删后来者，delayed helper 不能越过 generation fence；backup 失败无 published package/半包残留且业务 target 不变；operation cleanup 失败不得被 harness 掩盖；sentinel 始终不变；每个 frozen catalog case 独立 | failure injection + results JSON |
| A21 | 合法备份包以 typed project confirm restore 到已变更 initialized、app-present target | target identity 后使用 private staged package；confirm/validation 先于破坏；DB counts/头像 exact generation 的 after data envelope 均为 observed，value 等于 package_oracle，而不要求等于 mutated before；RSR/RSE exact stage-plan 通过，verify 后恢复原正常 running 或保持 Engine exited；sentinel 不变；`restore-success` machine case pass | ops smoke + results JSON |
| A22 | 错 command/input path/checksum/schema/confirm/project、staging input race、source/runtime identity、每个 unsupported Engine state、remote/selector、symlink/tar traversal，replace/verify/start/health 中途失败，delayed helper，以及首个 restore 持锁的两种 realpath alias | 所有 pre-mutation reject（含 static path、source-volume overlap、alternate app image/mount 与 alias）before=after；remote/selector pre-Engine exit 2，runtime/state mismatch pre-mutation exit 5，input/package/staging exit 9；破坏后失败 exit 10，RSF 要求 failure-stop 后 after.logical=stopped/engine_status=exited，DB/avatar 每项以 observed/absent/unreadable envelope 如实记录，不丢整个 after；旧 generation/immutable ID cleanup 不碰后来者；operation cleanup 与 harness cleanup 分层；sentinel 始终不变；每个 frozen catalog case 用 structured expected/observed 独立判定 | failure injection + results JSON |
| A23 | 新 owner 按 README 走 binary/managed/external 部署、显式 context、凭证、备份责任与恢复演练 | 命令/mode/key/TG 服务端读取/JSON 非备份边界一致；managed ops 明确只支持 initialized+app-present+pinned local Unix endpoint，binary/external/app-absent/remote 不会误称已备份；生产 mount/write/fsync/dir-sync probe 与 ECS durability/TLS/网络/异地介质 attestation 有记录 | doc diff + command + owner attestation |
| A24 | owner 在真机浏览器逐节点确认 fresh synthetic 完整链：带 identity 建档→建套系→建订单→建档期→标定金→下一账号自然日摘要→dashboard 五卡有数 | 主链 customer/package/order/slot/reminder ID 贯穿可兼容节点；因“今日档期/近期待收/长期流失”时间条件互斥而需要的辅助 fixture 必须显式标 ID/用途，不能替代主链。digest payload/调度 fresh 验证，五卡逐项与领域 API 交叉一致；owner 对 fresh browser/API 节点逐项 attested，TG true-external transport 按 H2 复用 2026-07-17 owner-attested 或 fresh 脱敏证据；本场景 pass 才允许 roadmap closure | browser/API/integration/owner walkthrough |
| A25 | 范围、H1 与清洁度反向审计 | API/schema/migration/auth 模型未偷偷改变；H1=接受时 canonical `auth-hardening-report.md` 已以 open risk 落盘、H1=不接受时当前 design 已扩范围；无 UI 库/PWA/移动完整 CRUD/真实 secret/PII/临时残留 | diff/grep + owner decision |
| A26 | Customers 用同一 fixture 在 375、768、769、desktop 与 375+200% 字体下复跑 | 375/768 仅 cards，769/desktop 仅 table；字段/打开详情/记备注动作等价，列表只请求一次；字体放大后核心动作仍可见/聚焦/滚达 | frontend network assertion + browser metrics/screenshots |
| A27 | 在 A24 fixture 上执行 reference-only data export | 七类实体/Settings counts 与数组长度一致；当前头像只为公开引用，无 binary/object_id/key/manifest/secret；导出是额外回归节点，不替代 A24 任一完成信号 | existing export test + browser/API |

### 3.2 明确不做的反向核对

1. api/openapi.yaml、生成 DTO 与 migrations 不应出现本 feature 的业务契约增量。
2. 移动 Calendar 不应出现可操作的新建/编辑/删除入口或可聚焦隐藏按钮。
3. H1 选择延后时，代码不应出现 cookie auth、登录 429/lockout、JWT TTL 配置或新改密端点，且独立 auth-hardening intake 必须存在；H1 选择纳入时，本条作废并先扩 design/roadmap/验收后重审。
4. Go server 不应直接终止 TLS，也不应新增某一云厂商/代理 SDK。
5. docker-compose PostgreSQL 端口策略不在本 diff 改写。
6. package.json 不应新增 UI/data-fetch/PWA 库；Vite chunk 拆分不作为隐性工作。
7. Telegram 入站失败回复、idempotency cleanup、各域 N+1、头像 TOCTOU/双 runner 不应混入实现 diff。
8. 导出/README/UI 不应声称 JSON 可恢复头像或线上删除会清除历史备份。
9. 脚本输出、日志、证据不应包含真实 DATABASE_URL/password/token/chat id/客户正文或用户机器绝对路径；operator 显式选择的仓库外 backup input/output 可以存在，但只能在运行时使用，不写入仓库证据。
10. 运维脚本不得读取默认 project/cwd/`COMPOSE_PROJECT_NAME` 或仓库 `.env` 来猜 target，也不得把 binary/external DB 轨伪装成 compose-managed-db 已备份。

### 3.3 Acceptance Coverage Matrix

| Scenario | Covered By Step | Evidence Type | Command / Action | Core? |
|---|---|---|---|---|
| A1 canonical gate/artifact wiring | STEP-001, STEP-007 | command + YAML | CMD-001、CMD-004、CMD-006、regression/catalog schema/ID uniqueness | yes |
| A2-A6 全路由状态/401 | STEP-001, STEP-002 | frontend test + browser | CMD-002 + 逐 route fixture | yes |
| A7 shell 375px | STEP-004 | screenshot + DOM metrics | 375 viewport 检查 | yes |
| A8-A10 查档期 | STEP-004 | browser + schedule regression | CMD-002 + 375/200% 字体 | yes |
| A11-A15 搜客户/记备注/keyboard | STEP-003 | frontend/API/browser | CMD-002 + 375 手工 | yes |
| A16-A18 server/preflight | STEP-005 | Go/script matrix + machine cases | CMD-001、CMD-003、CMD-005 + mode/key cases | yes |
| A19-A22 backup/restore/target isolation | STEP-006 | isolated integration + failure injection | CMD-004、CMD-005 | yes |
| A23 README/ECS attestation | STEP-006, STEP-007 | doc review + command + attestation | README walkthrough | yes |
| A24 roadmap 精确全链路 | STEP-007 | browser/API/integration/owner walkthrough | canonical regression A24 | yes |
| A25 H1/范围/清洁度 | STEP-007 | diff + owner decision | CMD-006、范围 grep、follow-up intake | yes |
| A26 breakpoint/字体放大 | STEP-003, STEP-004 | frontend network assertion + browser | 375/768/769/desktop + 200% | yes |
| A27 reference-only export 额外节点 | STEP-007 | existing export test + browser/API | A24 fixture export | yes |

### 3.4 DoD Contract

| ID | 要求 | 证据 | 阻塞级别 |
|---|---|---|---|
| DOD-DESIGN-001 | design/checklist 覆盖 roadmap 完成信号、历史债分流、D1-D8、H1/H2 与 A1-A27 | design-review passed | blocking |
| DOD-IMPL-001 | STEP-001～STEP-007 全部 done，step/check trace 与 canonical regression/ops catalog/evidence 落盘 | checklist + implementation evidence | blocking |
| DOD-REVIEW-001 | 独立 code review passed，无 unresolved blocking/important | review report | blocking |
| DOD-QA-001 | CMD-001～006、A1-A27、逐 route/viewport/ops target isolation 核心证据完成 | QA + evidence pack/results | blocking |
| DOD-ACCEPT-001 | H1/H2 已拍板，owner 真机逐节点确认的 A24 精确链 pass，A27 export pass；acceptance 核对交付物/范围/清洁度后才回写 roadmap | acceptance report + regression YAML + owner attestation + docs diff | blocking |

Validation Commands:

| ID | 命令 | 目的 | 核心性 | 失败处理 |
|---|---|---|---|---|
| CMD-001 | make check | 全仓 build/lint/test/codegen 门禁；必须真实执行 test:v1-hardening | core | fix-or-block |
| CMD-002 | cd frontend && npm run test:v1-hardening | 页面状态、移动投影、note/focus 契约 | core | fix-or-block |
| CMD-003 | bash -n scripts/*.sh | shell 入口语法 | supporting | fix-or-block |
| CMD-004 | ./scripts/test-v1-ops-contract.sh && docker build -t crm:v1-hardening . | catalog/results validator 反例 + 生产镜像/运维入口 | core | fix-or-block |
| CMD-005 | ./scripts/v1-ops-smoke.sh | 隔离 backup/restore/preflight 闭环 | core | fix-or-block |
| CMD-006 | git diff --check | diff 清洁度 | supporting | fix-or-block |

Required Artifacts: 全部使用 D8 canonical path：`v1-hardening-design-review.md`、`v1-hardening-implementation.md`、`v1-hardening-review.md`、`v1-hardening-qa.md`、`v1-hardening-acceptance.md`、`v1-hardening-regression.yaml`、`v1-hardening-ops-case-catalog.yaml`、`evidence/browser/viewport-metadata.json` 与场景截图、`evidence/commands/CMD-001.log`～`CMD-006.log`、`evidence/ops/v1-ops-smoke.log`、`evidence/ops/v1-ops-smoke-results.json`、`v1-hardening-evidence-pack.md`、`v1-hardening-evidence-pack-results.json`、`v1-hardening-dod-contract-results.json`、`v1-hardening-dod-results.json`、`v1-hardening-gate-results.json`；H1=接受时另需 `.codestable/issues/2026-07-22-auth-hardening/auth-hardening-report.md`。

### 3.5 自我批判结论

- 可证伪性：把“状态友好、移动可用、文档完善”改成 A1-A27 的输入→结果与命令/canonical evidence。
- 步骤原子性：状态契约、全路由接入、客户移动、档期移动、平台 hardening、运维文档、终验分为七步；没有把 backup 与最终回归塞进同一步。
- 最弱依赖：restore 的破坏性最高，单独 STEP-006，显式 pinned context/target、typed confirm、immutable-ID Engine lock、校验先行、sentinel 隔离与失败保持 stopped/after-state 忠实记录是门禁。
- 证据完整性：三条轻路径必须浏览器证据，Customers 必测 breakpoint/字体放大，运维必须真实显式 target compose + sentinel，状态/脚本失败分支另有自动化。
- 基线可执行性：make check fresh 通过；新命令标明 implementation 前不存在；STEP-001 固定 Makefile 接线；Docker/Compose 2.24/Testcontainers 环境限制已披露。
- 交付物可核验性：代码入口、脚本、README、D8 regression/截图/metadata/logs/reports/JSON 和 roadmap 回写均有唯一 canonical path。
- 清洁度：secret/PII、绝对路径、半包、compose 残留与临时调试项有明确反向门禁。
- 接口深度：StateNotice 不吞业务数据；shell CLI 隐藏真实失败编排而不是透传命令；没有引入假 adapter。

## 4. 与项目级架构文档的关系

- 领域名词：无新实体/API；CONTEXT.md 不需改。
- 系统结构：不改变 ADR-001 账号隔离、ADR-002 PostgreSQL 主存、ADR-003 Gin/OpenAPI 单体或 ADR-004 exact-generation 备份模型。
- 稳定约束：若 production-preflight 与一致备份包在真实演练中证明有效，可在 acceptance 后提示 cs-keep 沉淀“生产预检不回显 secret + validate-before-destructive-restore”模式；本 design 不提前归档。
- 认证 residual：H1 是设计批准前的阻塞性 owner 决策；延后时必须先落独立 auth-hardening intake，纳入时必须扩 design/roadmap 并重审，不把历史移交事项静默删除。
- 部署 residual：H2 与 A23 记录 TG true-external 证据来源、目标 ECS mount/write/fsync/dir-sync probe 和 TLS/网络/异地介质 attestation；断电级 durability 仍是环境风险，不改 ADR-004。
- 本 feature 的 UI 状态/移动投影属于 webapp 内部契约，运维 CLI 属 platform/deployment 表面；均无新跨域数据契约，因此不新增 ADR。
