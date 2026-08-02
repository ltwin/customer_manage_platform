---
doc_type: roadmap-review
roadmap: account-center
status: passed
review_state: passed
review_reason: round-3-no-blocking
reviewer_id: /root/account_center_roadmap_review
reviewed: 2026-08-02
round: 3
---

# account-center roadmap 审查报告

## 1. Scope And Inputs

- Roadmap: `.codestable/roadmap/account-center/account-center-roadmap.md`
- Items: `.codestable/roadmap/account-center/account-center-items.yaml`
- Approval input: `.codestable/roadmap/account-center/approval-report.md`
- 代码事实：现有 AppShell／Settings／auth／customer avatar／avatarbackup／dataexport 实现，由 CodeGraph 和独立 reviewer 只读核验。

## 2. Independent Review Execution

| Round | Verdict | 主要结果 |
|---|---|---|
| 1 | changes-requested | 发现路由过渡态、manifest v2／restore 协议、dataexport 单事务／schema、item ownership 和 If-Match/no-op 次序缺口 |
| 2 | changes-requested | Round 1 问题均基本闭合；追加发现 v1 整包 restore 到已有 profile 的目标环境时最终状态不明 |
| 3 | pass | v1 exact restore 整体替换语义、两阶段 fixture 和 canonical timestamp 全部闭合；无新 blocking、owner 重叠或 DAG 回归 |

- Detection: independent-agent
- Provider / agent: `/root/account_center_roadmap_review`
- Independence: reviewer 三轮均只读，未修改 roadmap、items、review 或业务文件。
- Merge policy: 主 agent 逐条核验后修订；每次实质修订都重跑独立 review，未用 reviewer 替 Owner 批准路线图或风险。

## 3. Findings And Disposition

| Finding | Severity | Final disposition |
|---|---|---|
| RMR-001 Wave 1 无可执行安全／设置路由过渡态 | blocking | closed：account-profile-center 一次建立全部最终 `/account/*` URL；安全／设置兼容面功能可用，无死链或占位页；旧 URL／导航由 hardening 退役 |
| RMR-002 manifest v2／双 key／restore 协议不完整且 owner 重叠 | blocking | closed：完整 v2 JSON schema、required/null、subject 语义、sort/digest、strict dispatch、v1/v2 matrix、发布／回滚顺序已锁定；avatar-media-safety-net 是 codec/preflight 唯一 owner |
| RMR-003 dataexport seam 与单事务 snapshot 冲突，无 schema 演进 | blocking | closed：dataexport repository 在原 account-scoped read transaction 内读专用 profile projection；顶层 required `account_profile` 和 schema v3 已定义 |
| RMR-004 profile-center 过载、hardening 成补漏桶 | important | closed：拆出 avatar-media-safety-net，建立 Implementation Ownership 矩阵；hardening 只拥有最终导航、集成、可访问性、演练和回归 |
| RMR-005 If-Match 与幂等 no-op 顺序未锁定 | important | closed：状态等价且对象完整时 no-op 先于 stale CAS；只有真实 pointer 变更／修复才返回 409 |
| RMR-006 roadmap 与 accepted ADR-004 双重权威 | important | planning closed / Owner gate pending：已设 avatar-media-safety-net implementation gate，写任何代码前先补充 ADR-004 |
| RMR-007 缺少 account-center 长期 requirement | important | planning closed / Owner gate pending：已设 child-design gate，任何子 feature design 前创建最小 account-center requirement |
| RMR-S01 拆出 avatar-media-safety-net | suggestion | adopted：成为 account-profile-center 的唯一前置 item，不改用户可见行为 |
| RMR-S02 认证 snapshot 类型仍可空 | suggestion | closed：AccountCenterContext 只消费 session boundary 验证后的 `AuthenticatedAccount` |
| R2-B01 v1 exact restore 对目标已有 profile 的最终状态不明 | blocking | closed：v1 manifest 只描述 customer avatar，但 restore 仍整体替换 DB dump 和头像根目录；不混合保留目标 profile。safety-net 用 synthetic marker 证明替换，profile-center 用真实 migration 证明 profile／GC 表为空 |
| R2-N01 `generated_at` 字节级规范未收窄 | nit | closed：v2 writer 固定 UTC `Z` + `RFC3339Nano` 最短必要精度，reader parse/re-encode 必须完全相同 |

## 4. Mechanical Checks

- YAML/frontmatter: pass。
- Items: 5，必需字段齐全。
- DAG: acyclic，无未知依赖、自依赖或 feature slug 冲突。
- Unique minimal loop: pass，仅 `account-profile-center` 为 `minimal_loop: true`。
- 最短闭环路径: `avatar-media-safety-net → account-profile-center`。
- 可并行分支: `account-privacy-security` 与 `account-system-settings`。
- Final join: 两个分支共同进入 `account-center-hardening`。
- Whitespace: pass。
- Worktree: 业务代码未修改；只有 `.codestable/roadmap/account-center/` 为未跟踪规划文件。

## 5. Evidence Confidence Ledger

| Check | Verdict | Evidence class | Basis | Follow-up |
|---|---|---:|---|---|
| Granularity Gate | pass | E/C | profile、共享媒体安全迁移、前端壳层、安全／settings IA 和运维恢复构成 epic | none |
| 5-node DAG | pass | E | 机械解析 5 节点、无未知依赖、无环 | none |
| Unique minimal loop | pass | E | 仅 account-profile-center 为 true，前置 safety-net 明确 | none |
| Route transition | pass | E/C | 按 item 定义了全部新 URL、兼容页真实行为和旧入口退役时点 | child design 机械引用 |
| Manifest v2 / v1 compatibility | pass | E/C | schema、sort/digest、strict reader、typed inventory、restore 和 rollback 完整 | codec/restore fixtures |
| v1 restore result | pass | E/C | 整包替换和空 profile 结果可机械判定 | two-stage fixture + rehearsal |
| Dataexport v3 | pass | E/C | 精确 schema、null、禁止字段、counts、版本和单事务快照已定义 | transaction/JSON fixtures |
| If-Match/no-op | pass | E/C | 已锁定先后次序与 multi-tab 语义 | customer characterization |
| Implementation ownership | pass | E | Roadmap 与 items 一致，hardening 不首次实现核心协议 | child design gate |
| AuthenticatedAccount boundary | pass | E/C | 只有 session boundary 做非空收窄 | TS compile/lint |
| Requirement traceability | owner gate | C | gate 已定义，尚未获授权／落盘 | Owner decision |
| ADR consistency | owner gate | C | implementation gate 已定义，ADR 尚未更新 | Owner decision |
| 5 MiB performance | residual | C/H | 范围和降级已定义，tradeoff 尚未接受 | Owner decision + QA |
| Export rollback | residual | C/H | 旧 binary 回滚会再产生 v2 且不含 profile | Owner decision + runbook |
| Accessibility evidence | residual | C/H | 合同充分，尚待 DOM／browser／visual 实证 | QA / acceptance |

## 6. Residual Risks And User Review Focus

### URF-001 · account-center requirement 授权

- 推荐：批准。
- 效果：在任何 child design 前创建最小长期 requirement，锁定“可识别当前账号、维护私有资料、聚合安全与设置”价值和明确不做。
- 未批准影响：child design 保持 blocked，或 Owner 显式接受缺少长期 traceability 的风险。

### URF-002 · ADR-004 补充授权

- 推荐：批准。
- 效果：在 avatar-media-safety-net 写任何代码前，记录“共享 avatar-specific object/content/inventory primitives，customer/account-profile 各自拥有 pointer/revision/GC”，避免 ADR 与 roadmap 双重权威。
- 未批准影响：avatar-media-safety-net implementation 保持 blocked。

### URF-003 · 5 MiB 原图、首版无缩略图

- 推荐：首版接受，不引入派生缩略图。
- 缓解：头像读取／解码异步、不阻塞业务页；同 auth generation 复用 object URL；延迟或失败立即回退字母头像；QA 记录首次下载／解码证据。
- 不接受影响：需把服务端缩略图／派生 generation、备份和 GC 纳入本 epic，重新 review。

### URF-004 · 旧应用回滚窗口的数据导出

- 推荐：接受并写入发布／回滚 runbook。
- 精确影响：新版产生 schema v3；紧急回滚到旧 binary 后会再产生 v2，该时段下载的“完整导出”不含仍存在的 account profile；恢复新版后重新下载 v3 即包含。
- 不接受影响：需设计跨版本独立 export compatibility 路径，扩大发布架构范围并重新 review。

### URF-005 · 可访问性证据

- 无需产品选择，但属于 blocking acceptance evidence。
- DOM/accessibility assertions 证明 role、tab order 和隐藏插槽；键盘 trace 证明 Arrow/Home/End/Escape 与焦点归还；真实浏览器和人工视觉证据证明 375／1440／200%／coarse pointer 无溢出或入口丢失。

## 7. Verdict

- Status: passed
- Blocking findings: none
- Roadmap state: 仍为 `draft`，等待 Owner 统一确认路线图与 URF-001..004。
- Reviewer authority: 只确认规划可执行，不替 Owner 批准 roadmap、requirement、ADR 或 residual risk。
- Next after Owner approval: 持久化各项批准，把 roadmap 设为 `active`，重跑 YAML/DAG 校验；完成 requirement gate 后才进入 child design，完成 ADR gate 后才实现 avatar-media-safety-net。
