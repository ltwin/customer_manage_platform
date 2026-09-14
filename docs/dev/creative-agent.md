# 创作助手（Harness）开发说明

归属 `backend/internal/creativeagent`。设计权威源是
[modules/harness.md](../product/creative-canvas-system/modules/harness.md)、
[modules/gateway-harness-data.md](../product/creative-canvas-system/modules/gateway-harness-data.md)、
[modules/agent-api-events.md](../product/creative-canvas-system/modules/agent-api-events.md)、
[eino-adoption.md](../product/creative-canvas-system/eino-adoption.md)，运行状态机唯一权威在
[data-model.md §7](../product/creative-canvas-system/data-model.md)。本文只记实现事实与已知边界。

## 当前交付范围（FND-07 里程碑 A）

已落地：会话、持久消息、外发同意、模型/Skill/工具目录，以及迁移 `0044_creative_agent_conversations`。

**尚未落地**（随里程碑 B/C 与后续项）：run / step / slot / epoch 状态机、Eino Runner 接入、
checkpoint 与 context backend、只读运行时工具、Gateway 结果消费、River Worker、等待/取消/对账/
终态恢复、最小 Agent 面板。写工具与提案采纳属 FND-08，独立附件属 FND-09，SSE 流式属 FND-08。

因此 `GET /creative/agent/catalog` 的 `tools` 目前恒为空数组，内置 Skill `reference-direction@1`
一律报 `available: false` 并附原因「所需工具尚未在本部署注册」。这是如实反映部署能力，不是占位。

## 端口

| 端口 | 契约 |
|---|---|
| `Service.CreateConversation` | 绑定一张画布；`project_id` 由服务端从该画布读出，客户端给的对不上也不采信 |
| `Service.ListConversations` | 按画布分页，游标绑定账号与查询串 |
| `Service.ListMessages` | 从最新往回翻，每页按阅读顺序返回；`before_ordinal` 不含该序号 |
| `Service.appendMessageInTx` | 受信服务端写入。客户端没有直接发消息的入口：消息要么随触发它的 run 产生，要么来自某个步骤结果 |
| `Service.GrantConsent` | 记录外发授权。授权即许可外发，不额外引入 `ai_analysis` 权利判断；但选中修订仍须是摄影师自己能展示的（见下） |
| `Service.RevokeConsent` | 阻止后续外发；不召回已发出的内容，授权证据保留 |
| `Service.Catalog` | 本部署真正可用的模型、Skill、工具与冻结上限 |
| `SkillRegistry` | 启动时加载内嵌包并校验；同 key/version 不同 digest 直接拒绝启动 |

## 已确定的实现事实

- **消息序号在会话行锁下发放**。`appendMessageInTx` 先 `SELECT ... FOR UPDATE` 会话行再取
  `next_message_ordinal`，不使用 `MAX(ordinal)+1`。去掉行锁后两个并发事务会撞唯一约束
  `creative_agent_messages_account_id_conversation_id_ordinal_key`（已复现）。
- **一个步骤结果按角色投影成一条消息**。`(account_id, source_step_id, role)` 部分唯一索引；
  重复消费同一步骤返回原消息，不追加第二条。同一步骤的不同角色（assistant 与 tool）仍是两条。
- **消息是保留根**。消息展示的内容通过 `creative_message_content_refs` 显式登记；藏在 body JSON
  里的 ID 不算根，运行载荷到期后不保证可读。执行点是 `creativecontent.requireRoot` 的根表清单
  ——该表在里程碑 A 一并加入，因此原资产改指向新修订之后，只被历史消息引用的旧内容仍可读
  （已复现：移掉该行后同一读取返回 `creative content not found`）。
- **`ai_analysis` 用途未开启，外发授权不引入第二层权利判断（2026-09-13 owner 拍板）**。
  `creativecontent` 一行未改。授权时对每个选中修订调用
  `creativecontent.RequireUsable(..., "display")`，**该调用实际执行的不止归属检查**，逐条列明：
  1. 同账号、`state='ready'`、`requireRoot` 保留根存在；
  2. `planningmedia.ValidatePurpose(..., PurposeMoodboardDisplay)` —— 权利矩阵。注意
     `PurposeAllowed` 对 `moodboard_display` 恒为 true，因此它只能因声明组合自相矛盾而失败，
     不构成「哪些来源可以外发」的限制；
  3. `creative_usage_grants` 存在一条 `purpose='display' AND revoked_at IS NULL`；
  4. `creative_content_required_grants` 的派生闭包——派生修订的每条上游声明都被 display 授权。
  即：**摄影师能在自己工作区里正常展示的修订就能被授权外发；display 授权被撤销的不能**。
  第 3、4 条会返回 `ErrUsageDenied` → 403 `creative_usage_denied`。今天每条新修订创建时自动获得
  display grant，所以触发面很窄，但它确实存在，不能说成「只是身份完整性校验」。
  - 去掉这一调用之后，另一账号的修订可以被授权外发（已复现）。
  - **与需求的偏差**：`.codestable/requirements/creative-canvas-foundation.md:119`
    「账号外发同意与素材用途权利是两项独立校验，任何一项不满足都不能发送」。准确的偏差是：
    **没有引入 `ai_analysis` 这项独立的外发权利校验**；display 权利闸门仍然生效。该句需要在
    FND-12 验收前经 `cs-epic` 边界重确认修订，否则会被记为不符。
- **`vendor_key` 是接收字节的公司**，不是线路协议（`provider`，如 `openai_compatible`），也不是
  按模型的 `deployment_key`（如 `deepseek/api/flash`）。授权按公司记录，因此在同一家的两个模型
  之间切换不会重新索要授权。`llmgateway.ModelConfig.VendorKey` 为必填，缺失时目录加载即失败。
  - 设计稿 `agent-api-events.md` 原字段名为 `provider_key`，语义即本字段；实现统一改名为
    `vendor_key`，因为 `provider` 在 Gateway 里已被协议族占用。
- **Skill 包随二进制发布**。`//go:embed skillpkg`；digest 覆盖包内每个文件的路径与内容哈希，
  移动或改名同样改变身份。资源只按声明的相对路径做 map 查表，底下没有文件系统，
  `../../../etc/passwd` 这类路径按「未声明资源」拒绝。
  - 进出 registry 的 `SkillPackage` 各深拷贝一次（`NewSkillRegistry` 与 `Get`）。`SkillPackage` 是值，
    但 `Resources` map、其中的 `[]byte` 与 manifest 的几个切片不是；不拷贝的话，拿到包的调用方
    能改掉指令、塞进资源或放宽 `ToolAllowlist`，而 digest 纹丝不动——digest 就失去了「当时跑的是
    哪份文本」的意义（两侧均已复现）。`packagesInOrder` 是包内列举口，仍直接返回存量值，
    只读不写，避免目录请求为了几个 manifest 字段复制全部资源字节。
- **`limits_version` 与 `policy_version` 是冻结常量**。调大任何一个上限都必须改
  `limitsVersion`，因为明天恢复的 run 要按它创建时的上限来判定。

## 验证缺口

1. 消息 chunk 表与流式落库不在本里程碑，`status='streaming'` 目前没有写入方。
2. `GET /agent/catalog` 的 `tools` 为空，Skill 可用性判定逻辑因此只验证了「不可用」一侧；
   工具注册后需补「可用」一侧的用例。
3. 外发同意目前只有授予与撤销，**派发时的二次校验属里程碑 B**：撤销先提交则不外发这条
   不变量尚未有代码可验。
4. 附件草稿（`creative_agent_drafts` / `creative_agent_attachments`）未建表，属 FND-09。
5. `appendMessageInTx` 的 `ContentRefs` 只校验角色枚举、非空与同次重复，**不校验 `RevisionID` 的归属、
   `ready` 状态或保留根**（与 `GrantConsent` 对每条修订调 `RequireUsable` 的严谨度不对称）。
   `account_id` 由 repository 基座强制，所以不构成跨账号影响；保留根这一侧已经生效，但一行指向
   不存在修订的 ref 仍写得进去（它只是永远保不住任何东西）。写入方全是受信服务端路径，
   里程碑 B 接入真实 run 时应补齐。
6. 重放身份是 `(source_step_id, role)`，**不含 body**：同一步骤用不同正文二次消费会静默返回首条消息，
   而不像 `creativeops.Executor` 那样比对 hash 后报冲突。里程碑 B 的 ConsumeResult 若可能产出
   「同一 step 两份不同正文」，需要在那一侧报出来。
