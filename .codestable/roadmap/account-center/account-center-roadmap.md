---
doc_type: roadmap
slug: account-center
status: complete
created: 2026-08-02
last_reviewed: 2026-08-02
confirmed_at: 2026-08-02
confirmation_id: "342a9f49-5c90-4386-a59a-51cd118a1c2e"
tags: [account, profile, avatar, security, settings, navigation, frontend]
related_requirements: [self-service-account-system, account-center]
related_architecture: [002-postgresql-as-primary-store, 003-monolith-first-gin-openapi, 004-avatar-object-store-as-binary-adjunct, 005-separate-account-identity-and-credential, 006-short-lived-access-and-rotating-refresh-sessions]
---

# 摄影师用户中心

## 1. 背景

当前经营台已经具备公开邮箱账号、密码修改、当前会话退出、账号级业务设置、Telegram 绑定和数据导出，但没有“当前登录的是谁”的稳定产品入口。`Account` API 只返回账号 ID、邮箱、创建时间和时区；桌面侧栏底部直接堆放“修改密码／退出登录”，移动端只有底部“设置”，两端都没有头像、展示名称或可进入的用户中心。

现有 `/settings` 同时承载账号安全、数据导出、Telegram、时区、可约时段、提醒参数和流失阈值。它把身份、安全、隐私和经营配置压在一张纵向表单里，也使“设置”继续占用业务主导航。Owner 已于 2026-08-02 选择 A：首版资料主体是当前经营账号的摄影师，支持可维护头像与展示名称，登录邮箱只读；不引入工作室双资料、团队成员或角色模型。

本 epic 将用户中心建设为受保护的账号级工作区：全局头像按钮负责识别和快捷入口，`/account` 负责聚合资料、安全和设置，三个核心板块分别拥有清晰的数据与操作边界。已有认证安全和 Settings 领域语义保持不变；新增账号资料与账号头像必须独立于登录身份、密码凭证、客户资料和业务 Settings。

### 目标完成信号

1. 任一受保护页面都能看到当前账号的头像按钮；桌面端位于侧栏底部，移动端位于壳层右上角，点击后可进入用户中心、用户资料、隐私与安全、系统设置，并可退出当前登录。
2. 摄影师可设置／修改展示名称，设置／替换／移除自己的头像；刷新、重新登录和多标签页冲突后仍按服务端当前版本显示，未设置或读取失败时有稳定回退头像。
3. `/account/security` 集中展示只读登录邮箱、修改密码、退出当前登录和完整数据导出；改密／退出／导出继续遵守现有安全语义。
4. `/account/settings` 把时区、可约时段、提醒参数、Telegram 摘要和外观分区呈现；每个分区只提交自己拥有的字段，不因另一个分区的陈旧表单覆盖最新设置。
5. 账号资料和头像继续按 `AccountScope` 隔离；账号头像纳入精确代次备份／恢复，JSON 数据导出从 schema v2 明确升级到 v3 并包含资料引用元数据，但不包含头像二进制或物理存储标识。
6. 旧 `/settings`、`/change-password` 深链安全重定向到新位置；桌面、375px 移动端、键盘、粗指针和 200% 文本缩放均无入口丢失、焦点陷阱或横向溢出。

## 2. 范围与明确不做

### 本 roadmap 覆盖

- 当前摄影师的私有账号资料：可空展示名称、账号头像、只读登录邮箱和账号创建时间；
- 账号资料的服务端模型、账号隔离仓储、OpenAPI、前端生成类型与读写状态；
- 账号头像设置／替换／移除／鉴权读取、并发 revision、强内容版本、不可变对象代次、延迟 GC、备份／恢复与失败回退；
- 全局头像按钮、账号菜单、用户中心总览和三个核心板块路由；
- 隐私与安全板块：验证邮箱只读展示、修改密码、退出当前登录、完整数据导出和安全语义说明；
- 系统设置板块：时区、可约时段、提醒参数、Telegram 摘要／绑定和当前浏览器外观；
- Settings 分区保存的字段所有权与陈旧数据保护；
- 旧入口兼容重定向、桌面／移动响应式、键盘／焦点／粗指针／文本缩放与跨账号回归；
- 账号资料元数据进入账号 JSON 导出；账号头像二进制继续由头像备份链路负责。

### 明确不做

- 工作室资料、工作室 Logo、个人／工作室双主体、团队成员、邀请、角色或权限；
- 修改／换绑登录邮箱、增加手机号身份、OAuth／SSO；
- 设备／会话管理中心、逐设备下线、登录历史、安全事件时间线或风险评分；
- 账号注销／删除、在线请求擦除历史备份、隐私政策／服务条款管理；
- 公开个人主页、头像公开直链、跨账号资料搜索或社交展示；
- 真实姓名、手机号、简介、地区、社交链接、作品集等扩展资料字段；首版资料只有展示名称与头像；
- 图片裁切器、滤镜、服务端缩略图、多尺寸派生或自动人脸识别；首版保存 JPEG／PNG／WebP 原始合规字节并以 `object-fit: cover` 展示；
- 跨设备同步主题；外观主题仍是当前浏览器本地偏好，并在 UI 明示；
- 改变密码、refresh session、Telegram 绑定、提醒阈值、可约时段或数据导出的既有业务语义；
- 把账号资料字段塞入 `accounts`、`account_identities`、`password_credentials` 或 `settings`；
- 改写现有客户头像对象键或客户头像 API。

### Granularity Gate

| 判断项 | 结论 |
|---|---|
| 为什么不是 single feature | 新增资料／头像持久化与媒体生命周期，同时重组全局壳层、认证安全页面、数据导出和 Settings 信息架构；存在媒体备份、路由迁移、字段所有权与响应式硬化等独立风险和依赖 DAG |
| 为什么不是 brainstorm | 三个核心板块、头像入口和资料主体均已明确；Owner 已选择 A，成功信号与明确不做均可证伪 |
| roadmap 边界 | 只建设单摄影师账号的私有用户中心；团队、工作室双主体、身份换绑、设备中心和账号删除延后 |
| 最小闭环 | 最短路径是前置 `avatar-media-safety-net` 完成后交付 `account-profile-center`；届时用户可从任一受保护页面点击头像进入 `/account/profile`，修改展示名称和头像，并立即看到全局入口同步更新 |

## 3. 模块拆分（概设）

```text
account-center
├── account-profile          深模块：账号资料、revision、账号头像指针与 GC 生命周期
├── avatar-media             共享基础设施：不可变头像对象、受限键、完整性与精确代次清单
├── webapp-account-center    前端深模块：资料状态、用户中心路由、头像菜单与响应式交互
├── account-security-surface 组合面：现有 auth action、会话退出和数据导出的安全呈现
└── account-settings-surface 组合面：现有 Settings 的分区编辑与外观本地偏好
```

### account-profile · 账号资料深模块

- **职责**：维护当前账号的展示名称、资料 revision、头像 pointer／revision 和账号头像 GC；无资料行时返回虚拟默认资料，不隐式写库。它不拥有登录邮箱、密码、refresh session、业务 Settings 或客户资料。
- **承载的子 feature**：account-profile-center 拥有实现；account-center-hardening 只做跨板块复验，不首次实现资料不变量。
- **触碰的现有代码／模块**：新增 `backend/internal/accountprofile`、PostgreSQL migration、OpenAPI／HTTP adapter、server composition、dataexport；通过 `AccountScope` 访问当前账号。
- **Depth 判断**：deep。HTTP 和前端只表达“读资料／改展示名称／设置头像／移除头像／读当前强版本头像”；revision、对象发布、pointer 切换、同内容重放、GC 与缺省行语义隐藏在模块内。

### avatar-media · 头像媒体共享基础设施

- **职责**：提供与业务主体无关的 `AvatarContent`、`ObjectMeta`、不可变对象存取和受限 key codec；同时识别现有 customer key 与新增 account-profile key。客户和账号资料各自拥有 pointer／revision／GC 编排，不共享业务仓储。
- **承载的子 feature**：avatar-media-safety-net 拥有中性 port、双 key codec 与 manifest codec；account-profile-center 拥有 account-profile pointer source 的真实接入；account-center-hardening 只复验。
- **触碰的现有代码／模块**：现有 `customer.AvatarObjectStore`／`avatarstore.Local`／avatarbackup 的中性化抽取；既有 customer key、API、revision 和 24h GC 行为保持字节级兼容。
- **Depth 判断**：deep infrastructure。复杂的安全路径校验、symlink 防护、不可变发布、元数据完整性、inventory 和 local／future object-store 差异集中在小 port 后；它不是把 customer service 再包一层。

### webapp-account-center · 用户中心前端深模块

- **职责**：维护账号资料读取状态和头像 object URL 生命周期，提供 `/account` 布局、三个板块路由、头像按钮／菜单、回退头像和跨页面刷新。它组合 auth snapshot、profile API 与 Settings API，但不把三者合并为一个可写 DTO。
- **承载的子 feature**：除 avatar-media-safety-net 外的四条。
- **触碰的现有代码／模块**：`App.tsx`、`AppShell.tsx`、shell context、auth snapshot 消费、现有 theme state、新增 account pages／components／tests。
- **Depth 判断**：deep。页面只消费 `AccountCenterContext` 和分区 actions；资料加载、auth generation 清理、菜单交互、旧路由重定向与响应式位置变化不散到每个业务页面。

### account-security-surface · 隐私与安全组合面

- **职责**：把现有只读登录邮箱、密码修改、当前会话退出和数据导出集中到 `/account/security`；保持 auth 和 dataexport 为真实 owner，不复制密码规则、cookie 或导出实现。
- **承载的子 feature**：account-privacy-security、account-center-hardening。
- **触碰的现有代码／模块**：`ChangePasswordPage`、`DataExportCard`、auth session、`GET /me`、dataexport 文档投影。
- **Depth 判断**：刻意保持组合层，不新增 pass-through backend service。安全动作继续直接穿过既有 auth API；新增价值是边界、解释和一致状态，而非重新实现认证。

### account-settings-surface · 系统设置组合面

- **职责**：以分区表单消费现有 `GET/PATCH /settings`，每区只提交自己拥有的字段；把 Telegram binding action 与 Settings read model 组合，把主题作为当前浏览器本地偏好呈现。
- **承载的子 feature**：account-system-settings、account-center-hardening。
- **触碰的现有代码／模块**：`SettingsPage`、AvailabilityEditor、telegramBinding、theme state、Settings API client；后端 `settings.Service.Patch` 语义保持不变。
- **Depth 判断**：前端 section controllers 隐藏 hydration、dirty、saving、stale 和字段投影；不创建新的后端“用户中心设置”万能 DTO。

## 4. 模块间接口契约／共享协议（架构层详设）

本节是所有子 feature 的硬约束。账号资料、认证账号和业务 Settings 必须保持三个独立读写边界；用户中心只在前端组合它们。要改变字段、错误码、路由或媒体 key 规则，先回本 epic 更新并重跑 review。

### 4.1 账号资料持久化与状态

**方向**：account-profile → PostgreSQL／avatar-media

**形式**：账号隔离关系行 + 账号资料专属 GC 行 + 不可变头像对象。

```text
account_profiles
  account_id          text primary key references accounts(id)
  display_name        text nullable
  profile_revision    bigint not null default 0
  avatar_revision     bigint not null default 0
  avatar_version      text nullable                 # sha256-<64 lower hex>
  avatar_object_id    text nullable                 # 32 lower hex
  avatar_media_type   text nullable                 # image/jpeg|image/png|image/webp
  avatar_size         bigint nullable
  avatar_updated_at   timestamptz nullable
  updated_at          timestamptz not null

account_profile_avatar_gc
  account_id          text not null references accounts(id)
  avatar_version      text not null
  avatar_object_id    text not null
  not_before          timestamptz not null
  next_attempt_at     timestamptz not null
  attempts            integer not null default 0
  last_error_class    text nullable
  primary key (account_id, avatar_object_id)
```

不变量：

- 表中最多一行当前资料；`AccountScope` 从认证上下文提供 account ID，客户端永不提交 account ID。
- 无行时 `Get` 返回 `display_name=null`、`profile_revision=pr-0`、`avatar_revision=ar-0` 的虚拟资料，不写库；第一次成功 mutation 原子 upsert。
- 首次 upsert 必须是 **column-local CAS**：展示名称 mutation 只写 `display_name/profile_revision/updated_at`，头像 mutation 只写 `avatar_*/avatar_revision/updated_at`；两者同时从虚拟 revision 0 首写时，`ON CONFLICT` 不得用自己的缺省值覆盖对方列。修改行只比较自己的 revision 列。
- `display_name` 为 null 或 trim 后 1–40 个 Unicode grapheme；禁止控制字符，保存规范值但不改变大小写。空字符串按 validation_failed 处理，显式 `null` 表示清除。
- `profile_revision` 只保护资料文本，`avatar_revision` 只保护头像 pointer；二者分离，避免头像上传与展示名称保存互相制造假冲突。
- 头像 pointer 五个 nullable 字段必须全空或全有；pointer 切换与旧对象 GC enqueue 同一数据库事务。
- 头像对象采用不可变 generation；同内容且当前对象完整时幂等 no-op，否则生成新 object ID。替换／移除后的旧 generation 至少保留 24 小时后才可 GC。新对象已发布但 pointer CAS 失败时，必须安全删除该未引用 generation 或纳入可证明的 orphan 清理，不得留下无主对象。
- 头像幂等判定先于 revision CAS，与已验证的 customer avatar 语义一致：同内容且 current object 完整，或移除时 current 已 absent，即使 `If-Match` 已旧也返回当前资料的 200 no-op，不增 revision；只有真正需要改变／修复 pointer 时才校验 expected revision，不匹配返回 409。
- 账号 profile 不进入 `account_identities`、`password_credentials` 或 `settings`；账号激活状态仍由 account-auth 负责。

### 4.2 account-profile 应用接口

**方向**：HTTP／dataexport／维护 runner → account-profile

**形式**：进程内 Go 接口；仓储为 local-substitutable，媒体对象为 local-substitutable port。

```go
type Profile struct {
    DisplayName       *string
    ProfileRevision   int64
    AvatarRevision    int64
    AvatarVersion     *string
    AvatarObjectID    *string
    AvatarMediaType   *string
    AvatarSize        *int64
    AvatarUpdatedAt   *time.Time
    UpdatedAt         *time.Time
}

type PatchInput struct {
    DisplayName *OptionalString // omitted=no change; null=clear; value=set
}

type Service interface {
    Get(ctx context.Context, scope store.AccountScope) (Profile, error)
    Patch(ctx context.Context, scope store.AccountScope, expectedProfileRevision int64, input PatchInput) (Profile, error)
    SetAvatar(ctx context.Context, scope store.AccountScope, expectedAvatarRevision int64, content avatar.Content) (Profile, error)
    RemoveAvatar(ctx context.Context, scope store.AccountScope, expectedAvatarRevision int64) (Profile, error)
    ReadAvatar(ctx context.Context, scope store.AccountScope, requestedVersion string) (avatar.Content, error)
}
```

错误分类：`validation_failed`、`profile_revision_conflict`、`avatar_revision_conflict`、`avatar_version_stale`、`not_found`（仅无当前头像内容）、`avatar_object_integrity`、`avatar_object_temporary`、`internal`。HTTP adapter 不根据字符串猜错误。

**Interface 设计检查**：

- **Module / interface**：`accountprofile.Service` 是唯一 mutation seam；caller 只知道两个 revision 和公开错误分类，不知道 SQL、对象键、GC 或 retry。
- **Seam placement**：HTTP 与账号资料维护操作穿过 account-profile 的 read／mutation seam；媒体实现只在 composition root 注入。`dataexport` 是明确例外：它的 repository 在同一 account-scoped read transaction 内直接读取专用 account-profile export projection，不另调会开新事务的 `Service.Get`，且绝不通过该读模型写 profile。
- **Depth / locality**：资料字段、revision、头像 pointer 和 GC 变化集中在本模块；auth／settings caller 无需改变。
- **Dependency strategy**：PostgreSQL 为 local-substitutable；AvatarObjectStore 为 local-substitutable，production local volume、测试 in-memory／tempdir。
- **Adapter**：PostgreSQL repository + deterministic fake repository；local volume + test object store。二者都承载真实替换价值，不为单一实现制造空转发层。

### 4.3 账号资料 HTTP／OpenAPI 契约

**方向**：webapp-account-center → auth-http → account-profile

**形式**：受 Bearer access token 保护的同源 HTTP API。

```text
GET /api/v1/account/profile
200 AccountProfile
401 unauthorized
500 internal

PATCH /api/v1/account/profile
If-Match: "pr-<current>"                # required
Body: { "display_name": string | null } # additionalProperties=false
200 AccountProfile
400 validation_failed
401 unauthorized
409 profile_revision_conflict
500 internal

PUT /api/v1/account/profile/avatar
If-Match: "ar-<current>"                # required
Content-Type: multipart/form-data; file required
200 AccountProfile                       # same-content valid replay = no-op
400 validation_failed
401 unauthorized
409 avatar_revision_conflict
500 internal

DELETE /api/v1/account/profile/avatar
If-Match: "ar-<current>"                # required
200 AccountProfile                       # already absent = no-op
400 validation_failed
401 unauthorized
409 avatar_revision_conflict
500 internal

GET /api/v1/account/profile/avatar/content?v=sha256-<64 lower hex>
If-None-Match: "sha256-..."             # optional
200 image/jpeg|image/png|image/webp
304 current complete object matches ETag
400 validation_failed
401 unauthorized
404 not_found
409 avatar_version_stale
500 internal
```

```ts
type AccountProfile = {
  display_name: string | null
  profile_revision: `pr-${number}`
  avatar_revision: `ar-${number}`
  avatar_version?: `sha256-${string}`
  avatar_url?: string
  avatar_updated_at?: string
  updated_at?: string
}
```

约束：

- 响应不重复返回 email、password、identity ID 或 account ID；只读邮箱和账号创建时间来自现有 auth snapshot／`GET /me`。
- `GET /me` 的认证职责和现有 schema 不因用户中心改变；前端并行组合 auth snapshot 与 AccountProfile。
- 上传最大 5 MiB，只接受经内容解码确认的 JPEG／PNG／WebP；不信任扩展名或仅信 `Content-Type`。
- `avatar_url` 是同源鉴权相对 URL，查询参数 `v` 必须等于当前强版本；无头像时字段缺失。
- 内容响应固定 `Cache-Control: private, no-cache`、`Vary: Authorization`、`X-Content-Type-Options: nosniff` 和 quoted ETag。URL 不带 token，不开放匿名读取。
- 任何 mutation 409 后前端重新读取资料并保留用户尚未提交的本地输入，不自动覆盖服务端新版本。

### 4.4 avatar-media key、对象与备份协议

**方向**：account-profile／customer-avatar → avatar-media → local volume／未来对象存储

**形式**：中性进程内 port + 两类受限 canonical key。

```go
type Content interface {
    Bytes() []byte
    MediaType() string
    Size() int64
    Checksum() string
}

type ObjectStore interface {
    PutImmutable(context.Context, Key, Content, ObjectMeta) (PutResult, error)
    Open(context.Context, Key) (io.ReadCloser, error)
    Stat(context.Context, Key) (ObjectMeta, error)
    List(context.Context, Prefix, Cursor, int) (ObjectPage, error)
    Delete(context.Context, Key) error
}
```

Canonical key：

```text
# 既有 key，必须原样保持
avatars/{account_id}/customers/{customer_id}/{avatar_version}/{avatar_object_id}

# 新增账号资料 key
avatars/{account_id}/account-profile/{avatar_version}/{avatar_object_id}
```

- key 只能由 typed builder 创建；adapter 不接受未经解析的任意相对路径。所有段继续执行 canonical、segment、symlink 和 regular-file 校验。
- 抽取到中性包后，既有 customer API、object bytes、key、revision、GC grace、backup inventory 和错误分类必须通过 characterization tests 保持不变；account-profile 不得导入 customer domain。
- 客户和账号资料各自拥有 pointer repository／GC repository／maintenance orchestration；只共享 object store、content validation 和 inventory primitives，避免万能“所有媒体业务”模块。

#### Exact-generation manifest v2

新 binary 只生成 `avatar-exact-generation-v2`，其规范 JSON 为：

```json
{
  "format": "avatar-exact-generation-v2",
  "generated_at": "RFC3339 UTC timestamp",
  "current": [
    {
      "account_id": "account id",
      "subject_kind": "customer | account_profile",
      "subject_id": "customer id | owning account id",
      "avatar_version": "sha256-<64 lower hex>",
      "avatar_object_id": "<32 lower hex>",
      "key": "canonical object key",
      "media_type": "image/jpeg | image/png | image/webp",
      "size": 123,
      "actual_sha256": "<64 lower hex without sha256- prefix>"
    }
  ],
  "inventory": {
    "count": 1,
    "actual_sha256": "<64 lower hex>",
    "objects": [
      {
        "key": "canonical object key",
        "size": 123,
        "actual_sha256": "<64 lower hex without sha256- prefix>"
      }
    ]
  }
}
```

- v2 上述字段全部 required，不允许 `null`、额外字段或历史 `customer_id`；`customer` 的 `subject_id` 是 customer ID，`account_profile` 的 `subject_id` 必须等于 `account_id`。每个 `(account_id, subject_kind, subject_id)` 最多一条 current entry。
- `current` 按 `account_id + subject_kind + subject_id + key` 升序，`inventory.objects` 按 key 升序。Inventory digest 继续对排序后每条 `key + NUL + decimal(size) + NUL + actual_sha256 + LF` 取 SHA-256；`generated_at` 不参与 digest。
- v1/v2 decoder 先严格读取 `format` 再分派，两者都 reject unknown fields、重复 JSON key、非 canonical key 和无法解析的主体路径；未知 format fail closed。v2 canonical writer 对 `generated_at` 固定使用 UTC `Z` 和 Go `RFC3339Nano` 的最短必要小数精度；strict reader 解析后重编码必须与原字符串完全相同，不接受等价的 `+00:00` 或非 canonical 小数形式。
- 新 binary 对历史 `customer-avatar-exact-generation-v1` 提供 strict decode、verify 和 restore preflight，但不再生成 v1。v1 manifest 始终只描述和验证 customer avatar：按 customer-only 视图识别并排除后来的 account-profile key，但任何未知／非法 key 仍失败。
- **v1 restore 结果语义**：它不是向当前环境增量导入 customer generation，而是继续执行项目现有的整包 exact restore，即用 v1 package 的数据库 dump 和头像根目录归档整体替换目标状态。恢复到已有 profile pointer、GC row 和物理头像的 v2 环境时，目标中恢复前的 account profile 数据／对象不保留；随后运行新 migration，`account_profiles` 与 profile GC 表为空，头像根目录也不存在 account-profile 对象。不从 v1 合成 profile，不混合保留目标环境的 profile；若未来需要仅导入客户头像，必须另定义 selective import，且不称为 exact restore。
- v2 inventory 同时覆盖两类物理 generation；typed parser 必须把每个 key 归一为 customer 或 account_profile。任一 current pointer 缺对象、元数据不一致、主体不匹配或 inventory 多／少对象都 fail closed。
- backup/restore ops package 必须在解包和改动状态前完成 format dispatch、schema 严格校验、typed inventory 和数据库 migration preflight。v2 恢复同时处理 customer/account_profile current pointer 与对象；不能退化成仅 PostgreSQL pointer。

#### 发布、回滚与唯一 owner

- `avatar-media-safety-net` 首先拥有中性 port、双 key codec、双格式 strict reader、v2 customer-only writer/verifier 和 backup/restore preflight；验收要求现有 customer bytes/key/API/revision/GC 零漂移。
- `avatar-media-safety-net` 还必须用 target-only 资料行／对象 marker 的 synthetic blocking fixture 覆盖“已有 profile-like 状态的目标环境恢复 v1 package”，机械证明整包替换后 marker 不被混合保留；该 fixture 不提前引入产品 profile migration。`account-profile-center` 引入真实 migration 时再扩展同一 case，证明 restore + forward migrations 后真实 `account_profiles`/GC 表为空且根目录无 account-profile 对象。
- `account-profile-center` 拥有 account-profile pointer source、v2 混合 current/inventory 的真实生成与恢复接入。`account-center-hardening` 只做 v1/v2 真实 restore rehearsal 和回归，不再修改 codec 或首次接入主体。
- 发布顺序是：先上线 dual reader/typed inventory，再上线 account profile 数据库 migration 与 pointer source，最后启用 v2 mixed writer。回滚窗口内不执行删表／删对象 down migration；已生成 v2 后回滚应用时，保留新版 maintenance binary 处理 v2 验证／恢复，或使用启用 v2 前的已验证 v1 备份；在 account-profile pointer/GC row 存在时禁止破坏性 down migration。

**Design It Twice 结论**：

- 未选“账号头像复制一套 local store”：会复制 symlink／完整性／backup／未来 OSS 迁移复杂度。
- 未选“账号头像存 PostgreSQL BYTEA”：生命周期更简单，但破坏现有二进制 adjunct、备份和对象存储演进边界。
- 选“中性化共享对象 port、领域各自拥有 pointer／GC”：在小接口后隐藏物理媒体复杂度，同时保持客户和账号资料的业务局部性。

#### Ops database_counts 兼容契约

> 2026-08-02 契约回写（来源：account-profile-center design D6；触发：design review 发现该机制此前仅在 items notes 授权方向、未入 §4 硬约束）。owner：`account-profile-center`；实现不得偏离以下条文。

- `account_profiles`／`account_profile_avatar_gc` 引入后，ops `database_counts` 的物理表集合从当前 16 表扩展。定义 `BASELINE_V1`＝当前 16 表历史集合（`DATABASE_COUNT_TABLES` 原样）；`OPTIONAL_ACCOUNT_PROFILE`＝`{account_profiles, account_profile_avatar_gc}`；`SQL_COUNT_TABLES`＝`BASELINE_V1 ∪ OPTIONAL_ACCOUNT_PROFILE`。基线演进须改 ops 版本／runbook，不得 silently 删强制下界。
- `validate_database_counts` 键集合必须 ⊆ `SQL_COUNT_TABLES` 且 ⊇ `BASELINE_V1`（16 键旧包与 18 键新包均合法）；废除「与全局常量集合全等」判据。
- `db-counts-sql` 签名不变（无参）：恒定输出 `SQL_COUNT_TABLES` 全部键，每键用 `to_regclass`（或等价）守卫，表不存在时该键计 0（定死，禁止「跳过」）。`backup-compose.sh`、`restore-compose.sh`、`v1-ops-smoke-runner.py` 三个调用点均不传 metadata。
- `verify-db-counts` 只按包 metadata 自述键比对 expected↔actual（actual 可含额外可选键，忽略之）；不得再对 actual 做全集全等校验。
- 消费者对齐：`v1_ops_results.py` 的 `restore-success` oracle 与 smoke-runner 采样比对按包自述键投影后再比（不得要求 after 的 18 键字典 ≡ 旧包 16 键）。
- 回滚：新包（18 键）被旧 ops 脚本读取会失败——runbook「脚本×镜像配对表」必须含 counts 行（对齐上文发布／回滚约束）。

### 4.5 前端 AccountCenterContext 与头像缓存

**方向**：AppShell／account pages → webapp-account-center → auth snapshot／profile API／Settings API

**形式**：React context + auth generation 绑定的内存状态；不写 Web Storage。

```ts
type ProfileReadState =
  | { kind: 'loading' }
  | { kind: 'ready'; data: AccountProfile; freshness: 'current' }
  | { kind: 'ready'; data: AccountProfile; freshness: 'stale'; refreshError: string }
  | { kind: 'error'; retry: () => void }

type AccountCenterContext = {
  account: AuthenticatedAccount            // session boundary 已验证非空的 email/id/created_at/timezone
  profile: ProfileReadState
  theme: 'light' | 'dark'
  refreshProfile(): void
  setTheme(theme: 'light' | 'dark'): void
  notify(message: string): void
}
```

- protected shell 启动后读取一次 AccountProfile；profile 失败不得让全部业务页面不可用，头像回退且菜单提供重试。
- auth session boundary 必须把 OpenAPI 生成的可选字段收窄为已校验的 `AuthenticatedAccount`；AccountCenter 下游不使用非空断言、不再重复猜测 id/email/created_at/timezone。可选地把 OpenAPI `Account` 这些实际必返字段改为 required，但只允许一个收窄边界。
- profile mutation 成功原子替换 context 中的 profile；菜单、总览和资料页不各自维护第二份 current profile。
- 头像 blob cache key 至少包含 auth generation + avatar URL + avatar version；logout、账号切换、401 转 anonymous 和 version 变化都 revoke object URL 并清缓存。
- 回退头像：优先展示名称首个 Unicode grapheme；其次登录邮箱首字符；最终为“影”。颜色由稳定 account ID 的非加密 hash 选择，不写入服务端。
- 主题仍存 `localStorage('od-crm-theme')`，仅用于非敏感 UI 偏好；System Settings 标记“仅当前浏览器”。认证 token、资料或 avatar bytes 不进入 Web Storage。

### 4.6 全局头像按钮与菜单契约

**方向**：AppShell → AccountMenu → account routes／auth logout

**形式**：同一语义组件的桌面／移动响应式插槽。

菜单结构固定为：

```text
[可点击身份摘要：头像 + display_name fallback + email] -> /account
用户资料                                         -> /account/profile
隐私与安全                                       -> /account/security
系统设置                                         -> /account/settings
切换浅色／深色                                   -> local theme action
退出登录                                         -> existing logoutSession
```

- 桌面触发器替换侧栏底部现有“修改密码／退出登录／工作室单账号”块；移动端在壳层右上角提供至少 44×44 CSS px 触发器。业务主导航和移动底栏删除“设置”，最终只保留六个业务入口。
- DOM 同时存在两个响应式插槽时，非当前断点实例必须 `display:none` 且不进入可访问树／tab order；不得出现两个同名可操作菜单按钮。
- trigger 使用 `aria-haspopup="menu"`、`aria-expanded`、`aria-controls`；打开后首个 menuitem 可聚焦，ArrowUp／ArrowDown 循环，Home／End 跳转，Escape／外部点击关闭并把焦点归还 trigger。
- 主题操作使用 `role="menuitemcheckbox"` 与 `aria-checked`表达当前深色状态，其余导航／退出项使用 `menuitem`；非当前响应式插槽的整个容器必须被 CSS 真正移出 layout/accessibility tree，不只是视觉透明或移出画布。
- 路由变化、logout、auth generation 变化和窗口失焦不保留过期菜单状态；菜单不是 hover-only。
- 头像加载失败不隐藏入口；展示回退头像并允许正常进入用户中心。

### 4.7 用户中心路由与页面信息架构

**方向**：React Router → AccountCenterLayout → section pages

**形式**：AppShell 内受保护嵌套路由。

```text
/account                           用户中心总览：三块摘要与快捷入口
/account/profile                   用户资料：头像、展示名称、只读邮箱／创建时间
/account/security                  隐私与安全：身份、安全动作、数据导出
/account/security/password         修改密码表单；成功后保持现有全设备退出语义
/account/settings                  系统设置分区

/settings                          replace redirect -> /account/settings
/change-password                   replace redirect -> /account/security/password
```

- 总览不是第四套表单，只显示 current read model 和三块入口；编辑动作进入对应 section。
- 桌面 AccountCenterLayout 使用 section navigation + content；移动端使用可纵向阅读的入口／标题导航，不把三个长标签挤成水平溢出 tab。
- `/settings` 与 `/change-password` 没有受支持的 query/hash 契约；最终 replace redirect 只去往上述固定新路径，丢弃未知 search/hash，避免把敏感或无意义内容带入新页。旧 auth action token 路由不在本 epic 改写。
- 每个 section 独立呈现 loading、stale、error、saving 和 success；一个 section 失败不屏蔽另两个 section 或业务主导航。

#### 可执行的路由过渡态

| 阶段 / owner | 必须存在的实际行为 |
|---|---|
| account-profile-center | 一次建立全部最终 `/account/*` URL；菜单从第一天只指向新 URL，不指向占位页。`/account/security` 以兼容组合面复用已验证邮箱、现有退出、DataExportCard 和改密入口；`/account/security/password` 在 AccountCenterLayout 内复用现有改密表单；`/account/settings` 在同一 layout 内挂载现有单页 Settings 行为。这三个页面都必须功能可用，不得显示“即将上线”。 |
| account-privacy-security | 在不改 URL 的前提下，把兼容组合面升级为最终身份／安全／数据导出信息架构；底层 auth/dataexport owner 不变。 |
| account-system-settings | 在不改 URL 的前提下，用五个 section controller 替换兼容的单页大表单。 |
| account-center-hardening | 才删除业务主导航／移动底栏的旧“设置”项，并建立 `/settings`、`/change-password` 到固定新路径的 replace redirect。 |

account-profile-center 验收时，旧 `/settings`、`/change-password` 和旧业务导航仍可用，以保持渐进迁移；但头像菜单的六个动作必须全部可实际完成，并验证身份摘要、资料、兼容安全、兼容设置、主题和退出路径。

### 4.8 隐私与安全板块契约

**方向**：account-security-surface → auth snapshot／auth actions／dataexport

**形式**：既有 API 的前端组合，不新增万能 security endpoint。

- 身份卡只读展示当前登录邮箱和“已验证”；active account 是可进入经营台的既有前置，因此不新增 `email_verified` 可写状态。
- 修改密码继续调用既有 change-password API：当前密码错误 401、限速 429、成功 204，清 refresh cookie、撤销全部 refresh family、前端清内存并跳转登录页。
- “退出当前登录”继续调用既有 logout：可信 Origin、撤销当前 family、清 cookie；服务端不可达时前端仍清本地认证内存。
- DataExportCard 移入本板块，下载、取消、错误和敏感数据提示保持现有行为。account-profile-center 首次引入资料时就必须升级导出契约，不留给 hardening 补齐。
- UI 不声称支持“查看所有设备”“退出其他设备”“换绑邮箱”“删除账号”或“彻底擦除备份”。头像移除说明 24h online GC 与历史备份边界。

#### 完整数据导出 schema v3

`dataexport.Repository.LoadSnapshot` 继续在单一 account-scoped read transaction 内读取全部 allowlisted 数据，并新增 dataexport-owned 的 profile projection；不在 snapshot 后另调 `accountprofile.Service.Get`。顶层 `account_profile` 为 required 且结构固定：

```json
{
  "schema_version": 3,
  "account_profile": {
    "display_name": null,
    "profile_revision": "pr-0",
    "avatar_revision": "ar-0",
    "avatar": null,
    "updated_at": null
  }
}
```

有当前头像时，`avatar` 结构为：

```json
{
  "version": "sha256-<64 lower hex>",
  "media_type": "image/jpeg | image/png | image/webp",
  "size": 123,
  "updated_at": "RFC3339 timestamp"
}
```

- 无 profile 行时导出与 `Get` 相同的虚拟默认值；无展示名称、无头像或无持久化更新时分别使用显式 `null`，不省略 required 字段。
- 导出不包含 avatar bytes、`avatar_url`、object ID、filesystem/object-store path、GC row、secret 或凭证；URL 是短期鉴权读取位置，不是可移植数据。
- `schema_version` 从现有 2 升到 3；新 binary 只产生 v3，counts 仍只统计现有七个 collection，`account_profile` 不伪装成 collection count。任何消费端必须按 schema_version 显式分派并对未知版本拒绝静默解析；v2/v3 fixture 和下载 JSON inspection 是 blocking evidence。
- 资料、Settings 和业务 collection 必须来自同一事务 snapshot；两个 active account 的 export 相互不可见。回滚到旧应用只会再产生 v2，不删除 profile 表或头像对象；产品与支持文档不承诺不同时间下载的导出文件 schema 始终不变。

### 4.9 系统设置分区与字段所有权

**方向**：account-settings-surface → existing `GET/PATCH /settings`／Telegram bind API／theme store

**形式**：一个 read model、五个独立 section controller；后端 PATCH 契约不改变。

| Section | 读取字段 | 唯一可提交字段 | 保存后额外动作 |
|---|---|---|---|
| 常规 | timezone | timezone | 刷新 Shell timezone cache |
| 可约时段 | availability | availability | 无 |
| 提醒规则 | birthday_lead_days, follow_up_after_days, churn_thresholds | 同左 | 无 |
| Telegram 摘要 | digest_hour, telegram_chat_id | digest_hour；绑定／重绑走既有 bind-token action | 清理过期 deep-link 内存 |
| 外观 | theme | 不调用后端 | 写当前浏览器 theme preference |

- 所有 section 共享最近一次成功 `GET /settings` snapshot，但各自维护 draft／dirty／saving／error；A 保存不得发送 B 的字段。
- 每次成功 PATCH 用完整响应刷新共享 snapshot；其他 dirty section 保留自己的本地 draft并标明“服务端设置已更新”，不得静默 hydrate 覆盖未保存输入。
- stale snapshot 时允许阅读，不允许提交；用户重试刷新后，只有未 dirty section 自动 hydrate。
- Telegram `telegram_chat_id` 只读；PATCH 永不提交该字段，绑定 token 不进入 DOM href、Web Storage 或日志，延续现有 10 分钟内存 deep-link 清理契约。
- 旧大表单的单次“保存全部”语义退役；后端 pointer-based partial PATCH 已能承载 section save，不新增 endpoint。

### 4.10 跨账号、缓存与失败安全协议

- 所有 profile query／mutation 使用当前 `AccountScope`；两个 active account 的 display name、pointer、GC row、avatar content、data export 和前端缓存互不可见。
- 客户头像和账号头像同 checksum 也必须拥有不同 subject key／object ID；不得因内容相同跨主体 deduplicate pointer。
- profile 读取失败只降级用户中心身份展示，不能阻塞客户、订单、档期、套系或提醒页面。
- avatar object 临时／完整性错误不回传旧或错误账号字节；UI 显示回退头像。维护 runner 失败记录脱敏 class，不记录对象原始内容、完整邮箱或 bearer token。
- profile、settings 或 auth 任一 401 统一交给现有 auth single-flight refresh／single replay；重放一次仍 401 时进入 anonymous。非认证 409／429／5xx 不触发刷新循环。**例外（2026-08-02 契约回写，来源：account-privacy-security design D3）**：改密当前密码错误（`POST /auth/password/change` 返回 401）不进入 single-flight refresh／replay——沿用既有 `requestWithoutAuthRetry` 单次请求语义，防止把改密失败误判为会话过期而登出；该例外由 account-privacy-security 验收 A2 固化。

## 5. 子 feature 清单

1. **avatar-media-safety-net** — 在不改变任何用户可见行为的前提下，把既有 customer-only 头像存储抽取为 avatar-specific 中性 seam，建立双 key codec 和 v1/v2 backup/restore 安全网。
   - 所属模块：avatar-media
   - 依赖：无
   - 状态：done（goal 功能验收 pass；feat/account-center；residual 见 approval 1A/2A/3A）
   - 对应 feature：2026-08-02-avatar-media-safety-net
   - 完成信号：customer key/bytes/API/revision/24h GC 零漂移；typed customer/account-profile key 均可严格解析；v2 customer-only manifest 可生成、v1/v2 可严格验证和 restore preflight；无账号资料表、无新 UI。

2. **account-profile-center** — 交付可维护的摄影师账号资料、强版本账号头像、用户中心路由骨架和全局头像菜单，使“进入资料并立即更新全局身份”端到端可用。
   - 所属模块：account-profile、avatar-media、webapp-account-center
   - 依赖：avatar-media-safety-net，因为第二类 subject 接入前必须先证明客户头像零漂移和备份双读能力
   - 状态：done（goal 功能验收 pass；feat/account-center；residual 见 approval 1A/2A/3A）
   - 对应 feature：2026-08-02-account-profile-center
   - 完成信号：资料／头像 API、column-local 首写 CAS、隔离 schema、v2 mixed exact-generation、导出 schema v3、全部最终 `/account/*` URL、功能可用的安全／设置兼容面、桌面／移动头像菜单和回退头像均可验证。

3. **account-privacy-security** — 把只读登录身份、改密、当前会话退出和数据导出重组为 `/account/security`，并把改密纳入用户中心受保护布局。
   - 所属模块：account-security-surface、webapp-account-center
   - 依赖：account-profile-center，因为它提供 AccountCenterLayout、账号菜单和最终路由骨架
   - 状态：done（goal 功能验收 pass；feat/account-center；residual 见 approval 1A/2A/3A）
   - 对应 feature：2026-08-02-account-privacy-security
   - 完成信号：邮箱只读、改密全设备退出、当前退出、导出成功／失败／取消和边界说明均从新路由完成；不新增设备／邮箱／删除能力。

4. **account-system-settings** — 把现有 Settings 拆成常规、可约时段、提醒规则、Telegram 摘要和外观五个独立保存分区，迁入 `/account/settings`。
   - 所属模块：account-settings-surface、webapp-account-center
   - 依赖：account-profile-center，因为它提供 AccountCenterLayout、账号菜单和最终路由骨架
   - 状态：done（goal 功能验收 pass；feat/account-center；residual 见 approval 1A/2A/3A）
   - 对应 feature：2026-08-02-account-system-settings
   - 完成信号：每区只发送 owned fields；并发／陈旧／dirty 场景不互相覆盖；Telegram 与 theme 既有行为保持；Shell timezone 只在常规保存后刷新。

5. **account-center-hardening** — 完成旧路由退役、业务导航收口、跨板块集成、运维恢复演练、响应式／键盘／文本缩放和全仓回归。
   - 所属模块：全部模块
   - 依赖：account-privacy-security、account-system-settings，因为完整导航和集成回归必须覆盖三个最终板块
   - 状态：done（goal 功能验收 pass；feat/account-center；residual 见 approval 1A/2A/3A）
   - 对应 feature：2026-08-02-account-center-hardening
   - 完成信号：旧深链固定 replace redirect、六项业务导航、菜单焦点契约、375px／1440px／200% zoom／coarse pointer、v1/v2/customer/account_profile 真实 restore rehearsal、双账号隔离、OpenAPI codegen 和 `make check` 全部有证据；不在此首次实现 manifest、profile API 或 AccountScope 基础语义。

**最小闭环**：最短交付路径是 `avatar-media-safety-net → account-profile-center`，且只有 `account-profile-center` 标记 `minimal_loop: true`。完成后，用户可在任一受保护页面点击头像，通过全部最终 `/account/*` URL 进入用户中心、资料、功能可用的安全兼容面和设置兼容面，修改展示名称和头像，并看到全局入口立即使用服务端新版本。

### Implementation Ownership

| Item | Owns implementation | Only re-verifies / must not defer |
|---|---|---|
| avatar-media-safety-net | 中性 avatar-specific port、双 key codec、v1 strict reader、v2 customer-only writer/verifier、restore preflight、customer characterization、目标专有 profile-like marker 不保留的 synthetic restore fixture | 不引入产品 profile schema/UI；不允许把 manifest codec 或 v1 整包结果语义留给 hardening |
| account-profile-center | profile schema/API/revision/GC、v2 account_profile pointer source、导出 schema v3、AccountCenterContext、全部新 URL 与菜单、安全／设置兼容面；用真实 profile migration 扩展 v1 整包 restore fixture | 已有 customer 行为和 v1 reader 只复验；不把资料隔离、导出或 mixed manifest 留给 hardening |
| account-privacy-security | 最终安全信息架构、边界文案与现有 auth/dataexport action 组合 | 不重写 auth/dataexport service；不首次实现 export v3 |
| account-system-settings | 五区 controller、owned-field PATCH、dirty/stale 保护、theme/Telegram 组合 | 不改后端 Settings 语义；不把交错保存正确性留给 hardening |
| account-center-hardening | 旧路由 replace redirect、旧业务导航删除、跨 section 集成、响应式／可访问性、双账号 E2E、最终 restore rehearsal 与全仓回归 | 不首次实现 manifest/codec/profile API/export/AccountScope 隔离；发现核心缺陷时退回 owner item 修正后重验 |

### Goal Coverage Matrix

| Goal / completion signal | Covered by item(s) | Verification entry | Evidence type | Core? |
|---|---|---|---|---|
| 全局头像按钮可识别当前账号并直达三个板块 | account-profile-center, account-center-hardening | 桌面与 375px 浏览器路径；菜单键盘矩阵 | screenshot + browser trace + accessibility assertions | yes |
| 展示名称和头像设置／替换／移除后全局同步 | account-profile-center | profile API／column-local CAS／PostgreSQL／前端 auth-generation tests | Go integration + frontend test + screenshot | yes |
| 账号头像不可变代次、GC、备份／恢复不破坏客户头像 | avatar-media-safety-net, account-profile-center, account-center-hardening | avatar object／manifest v1/v2／restore matrix | command logs + manifest diff + tests | yes |
| 隐私与安全集中且保持既有 auth／export 语义 | account-privacy-security, account-center-hardening | password multi-session、logout、export browser tests | integration + browser + downloaded JSON inspection | yes |
| Settings 五区只提交 owned fields且不覆盖 dirty draft | account-system-settings, account-center-hardening | request-body capture、stale／dirty／conflict matrix | frontend tests + API tests + browser evidence | yes |
| 导出 v3 在单事务快照中含资料引用且不泄露物理媒体信息 | account-profile-center | v2/v3 fixture、downloaded JSON、transaction interleaving | Go integration + JSON inspection | yes |
| 账号资料、头像、设置和导出跨账号隔离 | account-profile-center, account-center-hardening | two-active-account E2E | Go E2E + HTTP evidence | yes |
| 旧深链、移动端、文本缩放和粗指针可用 | account-center-hardening | `/settings`／`/change-password` redirects；375／1440／200%／coarse | browser screenshots + assertions | yes |
| 主题在用户中心可配置且明确仅当前浏览器 | account-system-settings | theme reload／storage inspection | frontend test + browser evidence | no |

## 6. 排期思路

### 技术依赖波次

0. **Traceability gates**：Owner 批准本 roadmap 后、child design 前，先用最小 account-center requirement 锁定“可识别当前账号、维护私有资料、聚合安全与设置”价值与明确不做；avatar-media-safety-net 写代码前更新／补充 ADR-004，记录 avatar-specific 中性 primitives 与各主体仍独立拥有 pointer/revision/GC 的边界。两者都需 Owner 明确授权，不由 roadmap 自动改写长期权威。
1. **Wave 1 — avatar-media-safety-net**：先以无用户可见变化的 characterization 证明 customer avatar 零漂移，再建立 dual reader/typed inventory/v2 customer-only writer 与 restore preflight。
2. **Wave 2 — account-profile-center**：在安全媒体 seam 上建立资料、AccountCenterLayout、全部最终路由和菜单，包含功能可用的安全／设置兼容面；这是最窄端到端产品闭环。
3. **Wave 3 — account-privacy-security / account-system-settings**：两条在 Wave 2 后可并行 design／实现，都在稳定 URL 上升级信息架构。它们的相对产品优先级未替 Owner 排定；默认仅按可并行性处理。
4. **Wave 4 — account-center-hardening**：三个板块稳定后统一删除旧主导航入口、完成固定 redirects、真实 restore rehearsal、双账号 E2E 与多断点可访问性证据；不接收前序 owner 未完成的核心协议。

### Top 3 风险与缓解

1. **共享头像基础设施时破坏既有客户头像或备份**：现有 local store 与 manifest 强绑定 customer key。缓解：保持 customer key 字节不变；先做 characterization；中性化只抽对象语义；v2 manifest 向后接受 v1；客户头像全套测试与真实 inventory diff 是 blocking gate。
2. **全局壳层改动导致桌面／移动入口、焦点或业务导航回归**：AppShell 同时拥有侧栏、底栏、theme 和退出，页面又各自拥有 topbar。缓解：AvatarMenu 只由 AppShell 挂载；定义双插槽单可访问实例；保留旧路由 redirect；375／1440／200%／coarse／键盘矩阵进入每条验收而非最后人工看一眼。
3. **拆分 Settings 后用陈旧完整 payload 覆盖其他分区**：当前页面 hydrate 一张全量表单。缓解：4.9 字段所有权是硬契约；每区 PATCH 只发 owned fields；dirty section 不被成功响应重置；请求体 capture 与交错保存测试为 blocking。

### 非显然依赖

- `GET /me` 已属于 account-auth + timezone projection，用户中心不得为了少一次请求把可写 profile 塞入认证 DTO。
- 现有 avatar store types、key parser、inventory 和 backup format位于 customer 边界，账号头像必须先中性化对象 seam并保持兼容。
- dataexport 当前在一个 read transaction 内生成固定 `schema_version: 2` 的完整 snapshot；profile 必须作为 dataexport-owned projection 进入同一事务，并明确升到 v3，不能在 snapshot 后另调一次 service read。
- AppShell 现在单独请求 `/me` 只为 timezone；profile 失败必须与业务页面可用性隔离。
- Telegram `telegram_chat_id` 不是普通 Settings PATCH 字段；UI 分区不能把 read-only binding 结果回写。
- 账号资料能力尚无独立长期 requirement；本 roadmap 以 Owner 原始诉求和已批准 interview 为输入，不顺手重写 requirement。

### 关键假设

- 本 epic 生命周期内维持“一账号对应一位经营摄影师”；未来团队能力会新增 member/person 层，不复用本次 profile 表表达多个成员。
- 展示名称允许初始为空，避免迁移时伪造姓名；私有 UI 以登录邮箱作为次级回退。
- 首版不需要图片裁切器；JPEG／PNG／WebP 原图在展示时居中裁切，不生成缩略图。
- 首版接受最大 5 MiB 原图可能导致首次头像延迟；资料请求和图片解码不得阻塞业务页，同 auth generation 复用 object URL，失败立即回退。本风险需 Owner 在 roadmap review 显式接受，否则改为本期引入派生缩略图。
- 用户中心及头像完全私有，只在认证后的同源经营台使用。
- 外观主题是设备本地偏好，不属于账号业务 Settings。
- 当前 public-auth-hardening 已交付的改密、退出、Origin、refresh rotation 和限速语义是可复用基线，不在本 epic 重做。

### 基线与验证入口

- 全仓：`make generate-check`、`make check`；本机 Testcontainers 使用 `-count=1 -parallel=1` 的低并行稳定入口。
- 后端：accountprofile／customer avatar／avatarbackup／dataexport／httpapi／auth 的 package tests 与双账号 E2E。
- 前端：现有 `test:auth`、`test:settings`、`test:data-export`、`test:customer-avatar`、`test:avatar-layout`、`test:v1-hardening`、`build`、`lint`；新增 `test:account-center` 聚合菜单、profile store、section ownership 和 redirects。
- 浏览器：真实本地服务，桌面 1440×900、移动 375×812、200% 文本缩放、coarse pointer、键盘-only；验证无横向 overflow、焦点可见、Escape／外部点击、菜单焦点归还和下载路径。
- 媒体运维：v1 manifest fixture 验证、v2 generate／verify、缺对象／多对象／metadata mismatch、backup／restore synthetic root 与 profile/customer current pointer 比对；必含“已有 profile 状态的 v2 目标恢复 v1 整包后 profile 表／对象为空”的 blocking case。

### 交付物落点

- PostgreSQL migration、`backend/internal/accountprofile`、中性 avatar-media port／local adapter、profile GC／maintenance、dataexport／avatarbackup projection；
- `api/openapi.yaml` 与双端生成物、HTTP handlers／router／composition；
- `frontend/src/account` 或等价聚合目录、AccountCenterLayout、AccountMenu、profile media cache、三个 section pages、旧路由 redirects；
- 新增／更新 Go、frontend、browser、backup／restore 与双账号隔离证据；
- 每条 feature 的 design／review／QA／acceptance 与本 items 状态回写。

## 7. 观察项

- **Requirement 缺口（child design gate）**：当前 `self-service-account-system` requirement 明确了认证与安全边界，但没有“账号资料／用户中心”用户故事。Roadmap 统一确认时请 Owner 一并批准；获得批准后，在任何 child design 前用 `cs-req draft` 新建最小 `account-center` requirement。本阶段不自动修改长期愿景。
- **未来团队迁移点**：若以后引入成员／工作室双主体，需要决定当前 `account_profiles` 迁为 owner profile 还是 workspace profile；本 epic 不预建成员表。
- **ADR-004 补充（implementation gate）**：中性 avatar-media seam、双 key 与 v2 manifest 会改变现有 ADR-004 记录的 port 归属。Roadmap 统一确认时请 Owner 一并批准；获得批准后，在 avatar-media-safety-net 任何代码实现前用 `cs-domain` 更新／补充 ADR-004，避免 accepted ADR 与 roadmap 成为双重权威。
- **数据擦除边界**：账号头像移除只保证 current pointer 立即清除与 24h online GC，不追溯历史备份；账号删除／法务擦除另行规划。
- **媒体派生**：真实使用若证明 5 MiB 原图显著影响壳层加载，再独立规划缩略图／派生代次；本 epic 不以未验证性能假设预建图像处理管线。
- **知识回写候选**：avatar-media 中性化、auth-generation 头像缓存、Settings section field ownership 若经 acceptance 验证，应分别考虑 `cs-keep`；影响每次会话的硬规则才进入 attention。

## 8. 修订记录

- **2026-08-02 · 契约回写（design review 后）**：已批准 roadmap 正文补两处 §4 硬约束条文，消除 design 单方面细化／豁免导致的契约权威悬空：
  1. §4.4 新增「Ops database_counts 兼容契约」（16→18 键、按包自述键、无参 SQL、配对表 counts 行）——来源 account-profile-center design D6，此前仅 items notes 授权方向；
  2. §4.10 新增改密 401 例外（不进入 single-flight refresh／replay）——来源 account-privacy-security design D3，此前设计单方面豁免 §4.10 泛化。
  - 两处条文均已在对应 design 中标注「回写 roadmap」引用；建议重跑一次 `cs-roadmap review` 确认修订（内容未改变任何已批准产品决策，仅落盘实现层契约机制）。
