# 创作助手（Harness）开发说明

归属 `backend/internal/creativeagent`。设计权威源是
[modules/harness.md](../product/creative-canvas-system/modules/harness.md)、
[modules/gateway-harness-data.md](../product/creative-canvas-system/modules/gateway-harness-data.md)、
[modules/agent-api-events.md](../product/creative-canvas-system/modules/agent-api-events.md)、
[eino-adoption.md](../product/creative-canvas-system/eino-adoption.md)，运行状态机唯一权威在
[data-model.md §7](../product/creative-canvas-system/data-model.md)。本文只记实现事实与已知边界。

## 当前交付范围（FND-07 里程碑 A + B1/B2）

已落地：会话、持久消息、外发同意、模型/Skill/工具目录、迁移 `0044_creative_agent_conversations`；
以及 B1 的 run/step/slot/epoch 持久结构（迁移 `0046_creative_agent_runs`）、`CreateRun` 的原子受理、
River Worker 接管、派发前外发同意二次校验、经 Eino ChatModelAgent 的有界多轮模型/只读工具调用与结果消费。B2 增加迁移
`0047_creative_agent_context`、固定 Skill Backend、持久工具计划及上下文材料。

**尚未落地**（随里程碑 B3 与 C）：版本化 Checkpoint、等待/取消/对账/终态恢复、救援扫描、
SSE、最小 Agent 面板。自动模型摘要仍默认关闭，达到上下文上限明确失败。
写工具与提案采纳属 FND-08，独立附件属 FND-09。

`GET /creative/agent/catalog` 的 `tools` 注册 `load_skill@1`、`read_skill_resource@1`、
`read_run_result@1`。声明其他尚未注册工具（如画布/素材业务工具）的 Skill 仍报
`available: false` 并附不可用原因。运行时仅开放本次固定 Skill 许可的工具；普通对话只开放
`read_run_result`，无 Skill 目录和包资源读取权限。

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
| `Service.CreateRun` | 一次提交变成工作：消息、冻结输入、写槽位、预算预留、首个事件与 River 入队同事务，202 只表示受理 |
| `Service.ReadRun` | 一次运行的当前状态、终态与费用投影；跨账号是「不存在」而非「拒绝」 |
| `Service.Handlers` | Worker 注册入口。一个 run 只对应一种任务；任务载荷只有 run id，其余事实 Worker 自己回库读 |
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
   （`TestASkillWhoseToolsAreNotRegisteredCannotBeSubmitted`）。B2 已注册三个只读运行时工具，
   画布业务工具仍等 FND-08。
3. ~~外发同意目前只有授予与撤销，派发时的二次校验属里程碑 B~~ **B1 已补**：`CallSession.Admit` 在
   记录派发意图的同一事务里重跑同意与内容校验，`TestAWithdrawnAuthorisationStopsTheDispatch`
   验证撤销先提交时供应商一次都没被调用。B2 对每轮派发、工具读取及工具结果落库再检查同意；
   已经发送的字节无法收回，撤回后不再发送后续材料。
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
- **重新校验的分工**：版本内容不可变，所以在这里读是成立的；但 Skill 可以被停用、内容授权可以被撤销。
  **B1 起这两件事分别在不同的事务里再核一次，而且位置不一样**：内容修订与本账号同域，所以
  `CreateRun` 在创建 run 的同一事务里对每条 `content_ref` 重跑一次 `RequireUsable`；Skill 的可用性
  **不可能**在那个事务里核——平台 Skill 属于发布账号，读它需要那个账号的 scope，账号隔离的事务看不见。
  它由 Worker 在派发前经端口重新解析（无缓存），这也正是验收要求成立的地方：不变量说的是
  「禁用先提交则之后的**派发**不通过」。这里回答的是「能不能提交」，不是「现在就可以派发」。

消息正文升到 `schema_version=2`：`skill_ref` 与 `content_ref` 是明确的块类型，v1 的无类型 `reference` 不再写入，但历史消息**按原样读出**（读路径不重新校验正文），不会被重新猜成 Skill。`skill_ref` 块里的显示名和版本号是**快照**——之后改名不能改写历史里已经显示过的内容。消息同时把这次引用登记进 `creative_message_skill_refs`（段序号、Skill、版本、发布账号、digest），让后来者不必解析 JSON 就能查。

`limits_version` 升到 `creative-agent-2`。新增的四个维度里，`skill_resource_files` 与 `skill_package_bytes` 是从 `creativeskill` 投影过来的，不是另写一份数字——两处写同一个上限，正是「导入时通过、执行时失败」的来源。**只增加维度也必须换号**：按 `-1` 创建的运行从没被片段数判定过，用 `-2` 的集合重放它等于套用它没同意过的规则。

## 运行（B1/B2）

一次提交变成一次运行，`CreateRun` 的整个受理是**一个事实**：摄影师的消息、冻结输入、账号写槽位、
预算预留、首个事件与 River 入队同事务提交。看得见的 run 却没有排队任务、或排了任务却没有 run，
都是工作凭空消失的路子。202 只表示已受理，之后的结果不会改写这张回执。

**锁序**（全局锁序的一段）：账号能力 → Agent slot → Gateway 分组准入行与预算桶 → 会话 → Skill 控制行
→ 外发同意 → 内容修订。预算预留用的输入上界是本 run 自己的**每请求上限**（`InputTextBytes`），不是对
首轮提示的测量——限额快照就是这个 run 将被判定的依据，由它推导出的 hold 不会被之后变长的组装拆穿，
也不需要为了测量而把内容读取提到 slot 之前、破坏锁序。

**首轮只占一笔额度**。创建时的预留用的正是**首轮自己的身份**
（`llmgateway.ReserveOperationID(callerService, runID+"#1")`），所以 Worker 跑第一轮时 Gateway 回放这笔
预留而不是在旁边再占一笔。换成任何别的 ID，创建时那笔就永远无人认领，而 run 却又占了第二笔——一个
模型调用都没发生，账号的月度额度却在被吃掉。turn 的 binding key 因此是 `runID#ordinal`：ordinal 来自
run 行锁下的计数器，是持久的，不是本进程编的（后者会让每次恢复都重新付一次钱）。

**终态一定归还未花掉的额度**。`closeRunInTx` 是所有终态路径的必经点，它通过 Gateway 的 caller-group 清理端口释放所有未认领预留，并取消仍未派发的请求。
已取消且不可能产生结果的请求同事务放弃本 caller 的消费保留。
跨月份预算桶按固定顺序先锁定；已派发/未知/已结算请求保留真实核算，不能当作未花掉的额度释放。

**历史在创建时冻结**。选取最近若干条已完成消息写进 `creative_run_inputs`，派发时不再重读会话：
输入是历史事实，摄影师按下发送时看到的东西才是模型被告知的东西；另一个窗口随后追加的那条属于下一次
运行。条数由 `limits.HistoryMessages` 约束，真正的上限是字节预算——`InputTextBytes` 扣掉本次提交与
Skill 正文之后的余额。只有 `text` 块进入历史；引用块是定位符，把它指向的东西再发一遍等于一次没人
授权过的外发。

**派发前的二次校验在派发事务内**。`llmgateway.CallSession.Admit` 是本次为此新增的钩子，它在记录派发
意图的那个事务里运行：先锁 run（epoch/claim_token/取消标记），再取 Skill 控制锁并复核可用性，再锁同意，
最后重跑每条内容修订的 `RequireUsable`。放在自己的事务里做不行——撤销可能在检查通过之后、派发意图
提交之前提交，字节就出去了。所以「撤销先提交则不外发」这条只有在同事务里才是真的。

**Skill 的那一半靠共享的版本控制锁**，这是 [skill-foundation.md](../product/creative-canvas-system/modules/skill-foundation.md)
指定的机制：禁用与派发意图取同一把锁。它不能是行锁、键里也不能带账号——平台 Skill 的发布者与读者
天然是两个账号，账号范围的查询够不到对方的行，账号范围的键也无法把两边串起来。所以是
`pg_advisory_xact_lock('creative-skill-version:<版本ID>')`，两侧都取：`DisableVersion` 在改行之前取，
`creativeskill.RequireRunnableInTx` 在读之前取。跨账号读只读**控制事实**（执行状态、Skill 可用性、
归属与 origin），经 `txcap.SkillControlView` 这个密封能力，由 store 独家产出；正文、资源与对象地址
仍只走 `creativeskill` 自己的端口。可见性规则也留在 `creativeskill` 里，跨账号私有版本一律报「不存在」。

派发前那次**只问可变的那一半**。正文不重读——run 已经带着冻结快照，重读它正是「run 悄悄按另一份指令
执行」的路子。`executeTurn` 开头的 `stillRunnable` 仍在，但它只是便宜的前置拒绝（顺带核对 digest 与
工具注册），**权威的那次在派发事务里**。

**Worker 与 slot**。`claimRun` 先锁 slot 再锁 run（全局序），确认 slot 确实归这个 run，然后 epoch+1、
轮换 claim_token（run 与 slot 两行一起轮换）、写租约并置 `running`。释放必须同时匹配 run 与
claim_token，旧 run 不能清掉别人后来取得的占用。租约 30 秒、每 10 秒续租，只有仍是持有者才续得上；
读它的救援扫描属里程碑 C，B1 里它已经如实记录「有没有人在干活」。

**模型解析在接管事务之内**。目录是进程内的只读配置，所以它属于这个事务——而且必须在里面：排队期间
模型被禁用、下线，或这台 Worker 根本没有凭证，若在事务外才发现，run 已经是 `running`、槽位已经占住，
而失败没有回终态的路（重投的任务只认 `queued`，什么都 claim 不到就结束了），账号会一直收到 busy。
现在它和「排队期间过期」走同一条出口：在事务内直接收成终态并释放槽位。

**终态收尾**。turn 的错误写进终态；收尾与接管统一按 slot → 预算桶 → run 加锁，避免重投与收尾
分别占住 slot 和 run 后互相等待。已在运行的重复任务在 slot 锁内读取状态后退出，不再争用预算和 run。
预算桶通过 Gateway 的 `LockReservationBudgetInTx` 预锁，Agent 不直接查询 Gateway 表。

最后请求定位查询和收尾事务各有独立的 30 秒清理期限，执行上下文取消后仍可释放槽位。数据库明确回滚的序列化失败或死锁
最多尝试 3 次，只重试收尾事务，不重复调用模型或消费答复。其他错误、提交结果不明及重试耗尽仍返回错误，
不能承诺数据库持续不可用时必达终态；这类遗留与进程崩溃同样需要里程碑 C 的救援。

**模型步骤先于发送存在**。`prepareModelStep` 在 run 行锁下取 `next_step_ordinal`，把规范化请求与
`RequestHash` 落库为 `prepared` 步骤；交给 Gateway 的 binding key 是 `runID#ordinal`，后续恢复必须按这个持久身份定位
已经付过钱的请求；B2 不从相同 prompt hash 推断重放，也尚未提供恢复入口。`llm_request_id` 是事后补写的**索引不是权威**——Gateway 存的
binding 由该 key 派生，这一列没写上也不丢失关联。

**256 KiB 输入上限在这里执行**，因为只有到这一步「实际要发出去的请求」才存在。创建时算的是
`content_ref` 的**标识符**长度，而发出去的是那条修订的**全文**：一条合法引用就能比整个预算还大
（内容层允许 100,000 字，中文即 300,000 字节）。用的是 Gateway 自己的 `EstimateInputTokens`——正文、
历史、Skill 指令、分隔符与工具定义都在内，与 hold 的算法同源，不会一个放行一个拒绝。**不能靠 hold 代劳**：
Gateway 拒绝时给的是「预算超限」，而摄影师遇到的其实是「引用太长」，前者他无从下手；而且 hold 的尺寸
一旦为别的原因改变，这条上限就静默失效了。

**结果消费与助手消息同事务**。`Consume` 在标记结果已消费的那个事务里写步骤结果与助手消息，所以不存在
「付过钱、已消费、却看不到」的中间态。空白答复记步骤但不产生消息；当前只读阶段仍只有文字答复算交付，没有持久答复时
run 以 `creative_model_empty_result` 失败。Gateway 的完整结果仍记已消费，已发生的计费保留，重投不会
为了补一条答复再次调用供应商。

`limits_version` 当前为 `creative-agent-4`：B2 开启持久多轮/工具计数。Worker 使用创建时保存的
limits_snapshot；旧版本记录不按新部署参数静默重算。此前 `-3` 增加 `history_messages` 维度。

### 当前已知边界

1. **没有救援扫描**：进程在一轮中途死掉，run 会停在 `running` 直到里程碑 C 的救援按租约接管。
   B1 已经堵掉的是**可预见**的那类搁浅（排队期间过期、模型不可用），它们都在接管事务里收成终态；
   剩下的是真正的进程崩溃，那只能靠救援。
2. **没有取消入口**：`cancel_requested_at` 列与派发前的检查都在，但还没有写它的 API（里程碑 C）。
3. **只读运行时工具**：业务写工具、画布提案和采纳仍属 FND-08。
4. **没有 Checkpoint**：Runner 不带 `CheckPointStore`，中断即失败，不做恢复（B3）。

## B2：固定 Skill 与只读多轮

- **Skill Backend**：本次 submission 仍最多选择一个 Skill。List 仅返回该固定版本的 ID/描述；
  Get 按版本 ID 返回冻结正文，并重查停用状态和 digest；不会解析 latest。明确选择的正文在初始化
  主动激活；只有 manifest 允许 `load_skill@1` 时才额外开放框架加载工具。平台包更新不改变旧 run。
  不挂载宿主文件系统，不传 ModelHub/AgentHub，不启用 fork 或模型覆盖。
- **工具身份和限额**：模型结果消费事务保存唯一 `(parent_model_step_id,tool_call_index)` 计划。
  供应商 call ID 保留在计划输入；给 Eino 的 ID 按持久 model step/index 投影，跨轮重复 ID 不会混淆。
  工具串行执行，每轮最多 4 个、run 最多 12 个，非法请求也占计划数；超出剩余额度的整批拒绝，
  原调用保留在已消费模型结果和步骤输出中，不创建越限可执行计划。模型步骤独立累计最多 13 个，
  相同提示也不复用另一轮的身份。Worker 使用 run 冻结的 limits，框架迭代上限只是第二道保护。
- **资源与结果读取**：`read_skill_resource` 的模型可见 schema 列出固定 digest 和登记路径，调用仍逐项校验资源 hash，
  拒绝未登记路径、越界路径及非 UTF-8 内容。`read_run_result` 只读同账号同 run 的未过期 item，
  offset/limit 按 UTF-8 字节且必须从字符边界开始，一页最多 8 KiB；重新检查执行权、Skill、同意及来源用途。
- **卸载与来源**：完整工具结果先入 `creative_agent_context_items`，单项最多 64 KiB；超过 8 KiB
  的资源结果在具备 `read_run_result` 权限时向模型返回定位符、字节数和有界预览；没有读回权限时
  保留完整 inline 结果，仍执行单项/总输入上限。Skill 正文及已分页的读取结果不再递归卸载。
  source_manifest 保留工具参数、固定 Skill/version/digest 和来源修订；来源修订另入显式 refs，
  供内容根查询使用；重复指令引用保留正文，授权来源及 refs 去重。保留期 90 天，清理扫描仍由 FND-10 落地。回滚有内容的 0047 会要求先导出。
- **交付与退出**：只读工具成功不是最终交付；只有持久 assistant 答复才可成功。中途撤回同意时
  已付费结果仍消费留档，不向模型继续传递工具结果。终止时关闭未执行工具计划，释放本组未用预留；
  不恢复进程崩溃现场（B3/C）。自动摘要未启用，也不以静默截断绕过 256 KiB 输入上限。
- **Eino 接缝**：v0.9.19 的工具 middleware 经每次 Generate/Stream 的 `model.WithTools` 传入
  当前工具列表；Gateway 适配器处理这条路径并保持实例配置隔离，拒绝调用时更换模型或输出上限。

B2 验证使用真实 PostgreSQL、River 受理、Eino Runner 和 Gateway 账本，供应商使用脚本桩；
不产生真实模型费用。覆盖循环上限、重复供应商 call ID、固定版本加载、大结果卸载、账号/run 隔离、
路径和 digest 拒绝、跨轮撤权、后续未认领预留释放及迁移无损回滚/有数据拒绝回滚。
