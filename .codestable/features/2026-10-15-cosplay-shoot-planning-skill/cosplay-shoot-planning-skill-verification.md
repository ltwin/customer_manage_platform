---
doc_type: feature-verification
feature: 2026-10-15-cosplay-shoot-planning-skill
requirement: V6-081
status: in-progress
date: 2026-10-15
---

# cosplay 拍摄策划 Skill · 前两步真实模型走查

V6-081 允许先写 Skill 正文，用现有工具验证「角色调研（IP 拆解）」与「创意方向」两步的内容质量。本记录覆盖这一步；策划文档（V6-079）、网上取图与看图（V6-080）、视觉设定与分镜尚未接入，相应 AC 不在本轮。

## 环境

- Skill 包：`deploy/creative-skills/cosplay-shoot-planning.v1/`（`SKILL.md`、`references/ip-breakdown.md`、`references/creative-direction.md`）。SOP 内容按 owner 最初提供的 Z-POINT 全流程 SOP 截图逐项写入。
- 测试：`backend/internal/creativeagent/cosplay_skill_live_test.go`，显式开启（`CREATIVE_AGENT_LIVE=1`、`CREATIVE_SKILL_LIVE_OUT`），可用 `CREATIVE_SKILL_LIVE_CASE` 只跑一例。从仓库目录同步真实 Skill 包，接真实模型 `gpt-6.1-sol` 与 Firecrawl；授权与面板默认一致（文字、图片预览、画布元数据）。不生成图片。
- 每例输出一份记录：工具调用（参数与结果片段）、每次模型调用（时间、通道、状态、失败类型、请求体大小）、最终回复。

## 用例

| 用例 | 输入 | 覆盖 |
|---|---|---|
| hot | 芙宁娜，想拍一组合成片，交付 9 张。 | 热门角色全流程前两步（AC-01、AC-02） |
| cold | 古剑奇谭三的北洛，外景，想拍得和别人不一样。 | 冷门角色（AC-01、AC-02） |
| ambiguous | 想拍阿尔托莉雅。 | 身份有歧义时停下来问（AC-01） |
| hot-ip / cold-ip | ……这次只做 IP 拆解，完整写出来。 | IP 拆解质量；「只做某一步」（AC-08） |

## 各轮结果与处置

| 轮次 | 运行 | 发现 | 处置 |
|---|---|---|---|
| 1 | hot、cold、ambiguous 各 1 次，均成功；工具 2–7 次，用时 22 秒至 1 分 26 秒 | ambiguous 正确停下来列出版本请摄影师选。hot、cold 跳过或压缩了 IP 拆解，cold 没读官方和资料站；hot 把 9 张分成 3/3/3；参考文件里的例子恰好与芙宁娜相关（审判庭、空剧院），被模型照搬 | IP 拆解标为不能省；交付固定 2+4+3；例子换成与角色无关的写法 |
| 2 | hot、cold，成功；工具 8–10 次 | 两例都读了官方和资料站（BWIKI、萌娘百科）；常见拍法读不到时标「经验判断」；IP 拆解仍没完整写出；部分参考链接无法确认出自检索结果 | 链接只用本轮检索结果里的地址；没有文档工具时要求先写章节全文 |
| 3 | hot、cold，成功 | 退步：hot 没搜资料就凭印象提问；cold 又跳过 IP 拆解 | SKILL.md 开头加「新策划的固定顺序」：先用 `update_plan` 列步骤，IP 拆解没做完不进创意方向，问摄影师的选项必须来自读到的资料 |
| 4 | hot、cold 各 2 次，均成功；工具 5–9 次，用时 57 秒至 2 分 1 秒 | 4 次都先读两份参考文件并读官方与资料站；方向按 2+4+3；停下来请摄影师选。回复仍只把 IP 拆解当依据带过 | 接受：IP 拆解在正式流程里写进策划文档，不在过渡写法上继续调；改用「只做 IP 拆解」单独检查质量 |
| 5 | cold-ip 成功（19 次工具，5 分 49 秒）；hot-ip 失败（`creative_model_providers_exhausted`，14 分 27 秒） | cold-ip 质量好：来源齐全、事实与推断分开、缺口写清，并识别出萌娘百科「北洛」条目属于《白荆回廊》而未混用；但写到了画布文字节点。hot-ip 读了 6 个网页后模型调用重试用尽 | 查明挂 Skill 时 `read_run_result` 不可用，网页结果最多 60 KB 整段进上下文；见下节 |
| 6 | hot-ip 成功（14 次工具，3 分 24 秒，15 次模型调用全部成功，请求体 24–111 KB） | 网页结果走预览加分页；IP 拆解八个小标题完整，来源齐全，区分原作事实与创作推断，写在回复里 | — |

## 运行时问题：挂 Skill 时大结果整段进上下文

`read_run_result` 原先只在 Skill 声明时可用，这是早期「Skill 白名单即本次全部工具」设计的遗留；之后业务工具都已不受白名单限制。结果是挂了 Skill 的运行不能分页读回大结果，网页正文整段进上下文。owner 2026-10-15 定：先对所有运行开放 `read_run_result`，按 Skill 只暴露指定工具留待以后设计。改动在 `creativeagent/runtime.go` 的 `allowed()`，测试与 `docs/dev/creative-agent.md` 同步。Skill 里另写读网页的建议（每阶段一般两三页，长页面分页读需要的部分），不设硬上限。

## AC 现状

| AC | 现状 |
|---|---|
| AC-01 带来源的视觉锚点与世界观；歧义时问、不编造 | 部分：IP 拆解质量在 hot-ip、cold-ip 中达标，歧义用例正确停问；看图相关细节（发型、纹样）只能列缺口，待 V6-080 |
| AC-02 2–3 个方向、字段齐全、选择前不往下做 | 部分：第 4 轮起稳定给方向并停下；方向字段齐全度不一（有时以表格或叙述带过） |
| AC-08 「只做某一步」 | 部分：hot-ip、cold-ip 只做了 IP 拆解；cold-ip 写到了画布文字节点，违反 Skill 规定 |
| 其他 AC | 依赖 V6-079 / V6-080 / 生成链路，未开始 |

## 已知问题

- 正式流程里 IP 拆解不会完整写进回复，靠 `write_plan` 接入后写进策划文档解决。
- 「只做 IP 拆解」曾写到画布文字节点，`write_plan` 接入后复测。
- 模型输出波动明显（第 3 轮退步），后续每次改 Skill 至少各跑两次。
- IP 拆解偏长，写进策划章节时可能需要压缩。
