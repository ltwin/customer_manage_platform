---
doc_type: brainstorm
slug: self-service-account-system
created: 2026-07-30
status: active
summary: 将现有单账号 password-only 认证升级为邮箱自助注册、验证后准入、可恢复且可撤销的多用户账号体系
tags: [authentication, registration, account, session, security, migration, frontend]
---

# 公开自助注册与多用户认证系统

> 创意空间 | 2026-07-30 | 下一步：cs-epic

## 出发点

前端已经有欢迎页和登录页，但产品仍无法让新用户真正自助开通账号。最初诉求是“实现注册 + 登录后端并接入前端”；检查仓库后发现，系统并非完全没有认证，而是已经交付了一套刻意受限的首版单账号基座：

- 空数据库启动时通过 `SEED_ADMIN_PASSWORD` 创建唯一默认账号；
- 登录请求只提交密码，后端固定校验最早创建的账号；
- 登录后签发 30 天 HS256 JWT，前端把 Bearer token 放在 `localStorage`；
- 所有业务数据已经通过 `AccountScope` 按 token 中的 `account_id` 强制隔离；
- 没有注册、邮箱／手机号身份、邮箱验证、找回／修改密码、服务端会话撤销或登录限速。

原 roadmap 明确把多账号注册排除在首版之外，现有登录页也写着“工作室单账号登录，暂不支持自助注册”。因此，这次需求的本质不是补一个 `/register` handler，而是把产品从单账号私有部署模型升级为公开自助注册的多用户账号体系，同时保留现有租户数据隔离地基。

## 聊过的方向

首先比较了三种产品开通模型：公开自助注册、申请／邀请开通、继续私有部署单账号。Owner 选择公开自助注册，意味着这是一组有依赖的 feature，而不是一个小型登录接入。

身份渠道比较了邮箱 + 密码、手机号 + 验证码和用户名 + 密码。Owner 判断邮箱与手机号长期都需要，但首期先以邮箱 + 密码作为 canonical identity；手机号在后续作为绑定身份和新增登录通道，不引入首期短信依赖。

邮箱准入比较了验证后才能使用、未验证受限使用和不验证直接使用。Owner 选择邮箱验证为硬准入门槛：注册后账号保持 `pending_verification`，完成邮箱所有权验证后才成为 active 账号并进入经营台。未验证状态不下穿到客户、订单、档期等业务域。

会话模型比较了服务端 opaque cookie session、短期 access JWT + 可轮换 refresh session，以及保留 `localStorage` Bearer JWT。Owner 选择 access/refresh 双 token：短期 access JWT 用于 API，refresh token 放在 HttpOnly cookie 中，服务端持久化 refresh session 并负责轮换、重放防护和撤销，为未来移动端或其他 API 客户端保留扩展能力。

安全范围比较了只做 happy path、公开可用的最小安全闭环和完整账号安全中心。Owner 选择最小安全闭环：首期必须同时具备密码找回／修改、会话撤销和登录限速，但不把设备中心、邮箱换绑、账号删除或安全审计中心塞进首期。

旧账号迁移比较了原地认领、临时双登录自助绑定，以及创建新账号后搬迁全库数据。Owner 选择原地认领：保留现有稳定 `account_id` 和全部业务数据，用一次性运维流程绑定并验证 owner 邮箱；认领完成后关闭 password-only 旧入口。未来空库不再自动 seed 默认账号，直接进入公开注册流程。

## 当前倾向

倾向把这项能力作为一个认证 epic，由多个有依赖的 feature 逐步交付，而不是一次横切前后端完成。初步可供 roadmap 评估的模块轮廓是：

1. 账号身份模型与公开注册：邮箱规范化／唯一性、账号状态、注册事务、验证 token、邮件投递边界；
2. 登录与会话生命周期：邮箱密码登录、短期 access JWT、refresh session 持久化／轮换／重放防护／撤销、当前设备退出；
3. 账户恢复与登录防护：忘记／重置密码、登录后改密、全 refresh session 撤销、登录限速；
4. 旧 seed 账号原地认领与上线切换：一次性安全认领、旧 JWT 失效、password-only 入口与 `SEED_ADMIN_PASSWORD` 退役；
5. 前端注册登录接入：注册页、待验证／验证结果／重发、邮箱密码登录、透明刷新、401 恢复、退出、忘记／重置／修改密码；
6. 契约、威胁模型与迁移验收：OpenAPI/codegen、跨账号不可见、token 重放、邮箱枚举、限速、恢复和回滚证据。

具体 feature 数量、先后依赖与最小上线闭环由 `cs-epic` planning 决定；上面的分组只是 brainstorm 输入，不是已批准 roadmap。Owner 额外要求 feature 不得拆得过小过细：应优先按可独立交付、可端到端验收的业务闭环聚合，只有存在真实依赖、独立风险或独立验收价值时才拆分，以控制跨 feature 协调成本并保持开发效率。

## 已敲定的点

- **产品模型已确认**：公开自助注册、多用户、多租户；不再以单账号私有部署作为目标形态。
- **租户边界已确认**：继续复用稳定的 `account_id` 与 `AccountScope`；客户端永不传 `account_id`，现有业务域不重写账号隔离。
- **首期主身份已确认**：邮箱 + 密码；邮箱是 canonical identity。手机号长期需要，但首期不实现，后续作为绑定身份／额外登录通道。
- **准入已确认**：注册账号先进入 `pending_verification`，验证邮箱后才 active；未验证账号不能访问 CRM 业务 API。
- **会话方向已确认**：短期 access JWT + HttpOnly refresh token + 服务端 refresh session；需要 refresh rotation、撤销和重放防护。正式方案不沿用 `localStorage + 30 天单 JWT`。
- **首期安全闭环已确认**：注册、验证、登录、刷新、当前设备退出、忘记／重置密码、登录后改密、改密／重置后撤销全部 refresh session、登录限速均在范围内。
- **首期明确不做**：手机号验证码、设备／会话管理中心、换绑邮箱、账号注销／删除、登录历史与安全审计中心。
- **旧数据迁移已确认**：旧 seed 账号原地认领，保留 `account_id`；不创建新租户后搬迁业务表与头像对象。
- **旧入口退役已确认**：认领完成后关闭 password-only `FirstAccount` 登录；未来空库不再自动 seed 默认账号。
- **结构性决策候选**：账号与登录身份模型、access/refresh 会话模型、旧 seed 账号原地认领均满足 ADR 判据，应在实现前由 `cs-domain` 正式记录。

## 遗留问题 & 下一步

进入 roadmap／design 后仍需回答：

- `accounts` 是否直接承载首期邮箱，还是从一开始拆出可扩展的 `account_identities`；手机号后续扩展需要什么稳定 seam；
- 邮箱大小写规范化、唯一索引、注册并发与“已存在邮箱”防枚举响应；
- 邮件 provider、outbox／重试、开发环境 mail sink，以及验证／重置邮件的投递可观测性；
- 验证与重置 token 的哈希存储、用途隔离、单次消费、过期、重发替换和并发点击语义；
- access/refresh TTL、refresh token family、rotation race、reuse detection、改密／重置后的撤销范围，以及短期 access JWT 的剩余风险窗；
- Web cookie 的 `Secure`、`HttpOnly`、`SameSite`、path/domain、CSRF 与未来跨源客户端边界；
- 登录／注册／重发／找回的限速维度、存储位置、代理 IP 信任与错误返回，避免账号枚举和可用性攻击；
- 密码长度／泄露密码策略、bcrypt 参数演进，以及是否保持对现有 bcrypt hash 的兼容；
- 旧账号认领的可信触发方式、防抢占、幂等／失败恢复、旧 JWT 失效时点、`SEED_ADMIN_PASSWORD` 退役检查；
- OpenAPI 与前端 codegen 契约、欢迎页 CTA、注册／验证／找回页面和现有登录页从 password-only 到 email + password 的迁移；
- 分阶段上线时是否允许兼容窗口，以及每一步的回��策略、数据不变量、安全测试和运维证据。

建议下一步进入 `cs-epic` planning，先完成模块拆分、依赖排序、跨模块契约和最小闭环；上述结构性 ADR 在实现前用 `cs-domain` 落盘，避免设计阶段重新争论已确认方向。
