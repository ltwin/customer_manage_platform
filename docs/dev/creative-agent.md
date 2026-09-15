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

因此 `GET /creative/agent/catalog` 的 `tools` 目前恒为空数组，任何声明了工具的 Skill 都报
`available: false` 并附原因「所需工具尚未在本部署注册」。这是如实反映部署能力，不是占位。

**S3 起 Skill 不再随二进制发布**：目录读 `creativeskill`（数据库 + 对象存储）。没跑过
`creativectl skill import` 的部署，`skills` 就是空数组、`skill_catalog_revision` 是空摘要的哈希；
普通对话不依赖它。导入见 [Skill 受信导入与回收](creative-skill-import.md)。

`skills` 为空还有第二种成因：catalog 只带**有界**摘要（20 条，按 `updated_at` 倒序），如果最靠前的
20 条 Skill 的推荐版本都被停用，摘要就是空的而目录里仍有内容。完整目录走 `GET /creative/agent/skills`，
它的空页规则见 OpenAPI 里 `CreativeAgentSkillPage` 的说明——**空 items 加非空 next_cursor 是合法的一页**，
客户端必须继续翻。

`skill_catalog_revision` 是**整个 `skills` 数组序列化后的哈希**，不是挑几个字段拼出来的。挑字段的写法
会漏掉「不激活地追加一个版本」这一类改动——导入总会改写 Skill 行的名称与描述，而推荐版本指针没动，
于是答案里的每个 version id 和 digest 都不变。按这个标识判断是否刷新的客户端会一直显示旧名字。哈希
整个答案则让「以后给 `SkillEntry` 加字段」不必再记得同步改哈希。

## 端口

| 端口 | 契约 |
|---|---|
| `Service.CreateConversation` | 绑定一张画布；`project_id` 由服务端从该画布读出，客户端给的对不上也不采信 |
| `Service.ListConversations` | 按画布分页，游标绑定账号与查询串 |
| `Service.ListMessages` | 从最新往回翻，每页按阅读顺序返回；`before_ordinal` 不含该序号 |
| `Service.appendMessageInTx` | 受信服务端写入。客户端没有直接发消息的入口：消息要么随触发它的 run 产生，要么来自某个步骤结果 |
| `Service.GrantConsent` | 记录外发授权。授权即许可外发，不额外引入 `ai_analysis` 权利判断；但选中修订仍须是摄影师自己能展示的（见下） |
| `Service.RevokeConsent` | 阻止后续外发；不召回已发出的内容，授权证据保留 |
| `Service.Catalog` | 本部署真正可用的模型、Skill、工具与冻结上限；Skill 摘要有界（20 条），完整目录走下面两个端口 |
| `Service.ListSkills` | 选框与斜杠选择器的统一查询；自己的 Skill 与受信平台目录合成一页 |
| `Service.SkillVersion` | 一个固定版本的摘要与声明；正文与对象地址不出这一层 |
| `Service.ResolveInstruction` | 把一次提交解析成可存可执行的东西：每个引用都在本账号范围内回数据库重新推导，客户端送来的任何值都不当作凭据 |
| `SkillDirectory` | 本包对 `creativeskill` 的窄接口，只有读。导入/激活/停用不在其中——发布是部署动作，HTTP 处理器不该离它只有一次类型断言 |

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
  - **需求已对齐，不再是偏差**：`.codestable/requirements/creative-canvas-foundation.md` 原先要求
    「账号外发同意与素材用途权利是两项独立校验」，该句已于 2026-09-13 经 `cs-epic` 边界重确认修订为
    「外发同意 + 修订在自己工作区能正常展示」，见 Epic 的「2026-09-13 边界重确认」小节。实现无需改动。
- **`vendor_key` 是接收字节的公司**，不是线路协议（`provider`，如 `openai_compatible`），也不是
  按模型的 `deployment_key`（如 `deepseek/api/flash`）。授权按公司记录，因此在同一家的两个模型
  之间切换不会重新索要授权。`llmgateway.ModelConfig.VendorKey` 为必填，缺失时目录加载即失败。
  - 设计稿 `agent-api-events.md` 原字段名为 `provider_key`，语义即本字段；实现统一改名为
    `vendor_key`，因为 `provider` 在 Gateway 里已被协议族占用。
- **Skill 的权威来源是数据库与对象存储**（S3 起；此前是 `//go:embed skillpkg`，已随本切片移除，
  种子包移到仓库根的 `deploy/creative-skills/`，只作可审计导入素材）。资源只按声明的相对路径查表，
  底下没有文件系统，`../../../etc/passwd` 这类路径按「路径不合法」拒绝，未声明的名字按不存在拒绝。
  深拷贝的那套不变量随 registry 一起消失：每次 `ResolveVersion` 都从行里现构值，没有共享可写状态。
- **跨账号只有平台目录一条路，且在 `creativeskill` 内走完**。读者传自己的 scope，拿回值对象；
  平台发布账号的 `AccountScope` 不出包。两处判定各自独立：`ListAccessibleSkills` 对平台侧加
  `origin='platform'`（发布账号自己的私有 Skill 不因此外泄），`ReadResource` **两条分支**都从库里核对
  「这个版本属于这个 Skill」，跨账号那条再加一问「这个 Skill 是 platform」——**不信调用方递进来的
  snapshot**，因为 snapshot 是普通结构体，谁都能造一个。绑定检查只在跨账号分支做过一版，那样
  同一个参数在一次调用里可能被校验也可能被忽略，签名就承诺了它只在一半情况下做的事。
- **`skill_catalog_revision` 是摘要的哈希，不是单调计数器**，也刻意不复用 Gateway 的
  `catalog_version`：模型来自部署随附的配置文件，Skill 来自任何一次导入都能改动的数据库。
  客户端要的答案是「和我缓存的那份一样吗」，对一个按账号拼出来的视图，诚实的答案就是答案本身的哈希。
- **`limits_version` 与 `policy_version` 是冻结常量**。调大任何一个上限都必须改
  `limitsVersion`，因为明天恢复的 run 要按它创建时的上限来判定。

## 验证缺口

1. 消息 chunk 表与流式落库不在本里程碑，`status='streaming'` 目前没有写入方。
2. ~~Skill 可用性判定只验证了「不可用」一侧~~ **S3b 已补**：`registeredTools` 改为读
   `Service.tools` 字段而不是返回字面空切片，测试因此能给出一个真实注册表，两侧都跑到了
   （`TestASkillWhoseToolsAreNotRegisteredCannotBeSubmitted`）。生产里该字段仍为空，
   直到 FND-08 注册真实工具。
3. 外发同意目前只有授予与撤销，**派发时的二次校验属里程碑 B**：撤销先提交则不外发这条
   不变量尚未有代码可验。
4. 附件草稿（`creative_agent_drafts` / `creative_agent_attachments`）未建表，属 FND-09。
5. `appendMessageInTx` 的 `ContentRefs` 只校验角色枚举、非空与同次重复，**不校验 `RevisionID` 的归属、
   `ready` 状态或保留根**（与 `GrantConsent` 对每条修订调 `RequireUsable` 的严谨度不对称）。
   `account_id` 由 repository 基座强制，所以不构成跨账号影响；保留根这一侧已经生效，但一行指向
   不存在修订的 ref 仍写得进去（它只是永远保不住任何东西）。写入方全是受信服务端路径，
   里程碑 B 接入真实 run 时应补齐。**S3b 起经 `ResolveInstruction` 产生的那条路已经校验过**
   （它对每个 `content_ref` 调 `RequireUsable`），但校验在解析器里而不在写入处，
   所以直接构造 `newMessage` 的调用方仍绕得过去。`SkillRefs` 同理：字段非空与段序号不重复由
   `appendMessageInTx` 保证，「这个版本真的存在且可见」由解析器保证。
6. 重放身份是 `(source_step_id, role)`，**不含 body**：同一步骤用不同正文二次消费会静默返回首条消息，
   而不像 `creativeops.Executor` 那样比对 hash 后报冲突。里程碑 B 的 ConsumeResult 若可能产出
   「同一 step 两份不同正文」，需要在那一侧报出来。

## 结构化输入（S3b）

一次提交是**有序的判别联合** `instruction_segments`，三种片段：`text` / `skill_ref` / `content_ref`。每种只允许自身字段，写了别的字段会被**拒绝而不是忽略**——丢掉它等于把另一条指令发给模型。未知类型同理。契约见 OpenAPI 的 `CreativeAgentInstruction`。

`ResolveInstruction` 在写入之前跑，回答的是「这次提交能不能被提交」：

- **客户端只给 ID。** 名称、版本号、digest、作者全部由服务端读出。`skill_ref` 必须同时带 `skill_id` 与 `skill_version_id`——只给版本会让服务端替客户端挑 Skill，而那正是「一个 Skill 的 ID 配另一个 Skill 的版本」能读到不该读的东西的路子。
- **两种「太多」是两种答案。** 片段数超过 200 是 `ErrLimit`（少发一点就行）；两个 `skill_ref` 是 `ErrValidation`——修法是二选一，回 413 等于让客户端去缩短一个并不长的东西。同一个 Skill 写两遍也是同样的拒绝，不做静默去重、不拼接。
- **拒绝分三类**：不存在/看不见 → `creativeskill.ErrNotFound` 或 `ErrNotFound`（跨账号一律报「没有」，猜 ID 学不到任何东西）；本部署跑不了 → `ErrUnsupportedSegment`；请求本身不合法 → `ErrValidation`。`ErrUnsupportedSegment` 刻意不是 `ErrValidation`：请求是好的，摄影师换一个 Skill 就能继续，答「格式错误」是在撒谎。
- **本部署目前没有注册任何工具**（FND-08 才注册），所以**任何声明了工具的 Skill 都会被 `ErrUnsupportedSegment` 拒绝**，和 catalog 里 `available: false` 是同一个事实。这不是缺陷，是如实反映能力；FND-08 注册后它自己就通了，不需要改这里的代码。
- `content_ref` 走 `creativecontent.RequireUsable(..., "display")`，**并且检查它返回的修订 `Kind`——本阶段只收 `text`**。这两件事是两个问题：`RequireUsable` 回答「本账号能不能展示这个」，它对 image / video / audio 一律放行；而「本部署能不能把它发给模型」取决于模型链路，那条路现在没有图片能力。只看错误不看 kind 就会接受一张图，然后要么静默丢掉、要么抽帧外发——两种都没人授权过。图片按 `ErrUnsupportedSegment` 拒绝（能力问题，不是格式问题）。
- 同一个修订写两遍只留一条保留根（主键是修订），但两个片段都照常显示——那是摄影师写的。
- **重新校验的分工**：版本内容不可变，所以在这里读是成立的；但 Skill 可以被停用、内容授权可以被撤销，**里程碑 B 的 CreateRun 必须在自己的事务里再核一次**。这里回答的是「能不能提交」，不是「现在就可以派发」。

消息正文升到 `schema_version=2`：`skill_ref` 与 `content_ref` 是明确的块类型，v1 的无类型 `reference` 不再写入，但历史消息**按原样读出**（读路径不重新校验正文），不会被重新猜成 Skill。`skill_ref` 块里的显示名和版本号是**快照**——之后改名不能改写历史里已经显示过的内容。消息同时把这次引用登记进 `creative_message_skill_refs`（段序号、Skill、版本、发布账号、digest），让后来者不必解析 JSON 就能查。

`limits_version` 升到 `creative-agent-2`。新增的四个维度里，`skill_resource_files` 与 `skill_package_bytes` 是从 `creativeskill` 投影过来的，不是另写一份数字——两处写同一个上限，正是「导入时通过、执行时失败」的来源。**只增加维度也必须换号**：按 `-1` 创建的运行从没被片段数判定过，用 `-2` 的集合重放它等于套用它没同意过的规则。
