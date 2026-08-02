---
doc_type: feature-design
feature: 2026-08-02-account-center-hardening
requirement: account-center
roadmap: account-center
roadmap_item: account-center-hardening
execution_lane: goal
execution_lane_reason: epic child batch under account-center
status: approved
summary: 完成旧路由 replace redirect、旧业务导航收口、前序 deferral 呈现项、跨板块复验、真实 restore rehearsal、响应式／可访问性与全仓回归；不首次实现核心协议
tags: [account, navigation, a11y, restore, e2e, regression]
---

# account-center-hardening · 导航收口与集成硬化 design

## 0. 术语约定

| 术语 | 定义 | 防冲突结论 |
|---|---|---|
| 固定 replace redirect | `/settings`→`/account/settings`；`/change-password`→`/account/security/password`；丢弃未知 search/hash | 仅本条建立；放在 RequireAuth **内**，保证未登录深链登录后落新路径 |
| 六项业务导航 | `navItems` 单数组驱动侧栏＋底栏，删除「设置」后剩 6 项 | 账号入口只经 AccountMenu |
| 核心协议 | manifest／codec／profile API／export v3／AccountScope／五区 owned-field **正确性首次实现**／最终安全 IA 首次实现 | **不含**本条自有呈现：方向键循环、theme-toggle 去留、死代码清理 |
| 复验深度 | Goal Coverage co-cover 行：跑前序 **完整矩阵命令／用例**（非点一下冒烟） | 失败按分流规则退回 owner，不就地补协议 |
| 真实 restore rehearsal | v1 旧包路径 ＋ **v2 mixed**（customer＋account_profile）backup→validate→restore→verify | 消费前序；无臆造 flag |
| 缺陷分流 | 协议／契约级→退回 owner；呈现级→自修；accepted-residual→白名单；legacy source-grep→D11 | 见 D8／D11 |

## 1. 决策与约束

### 需求摘要

- **做什么**：redirect；删主导航「设置」；接收前序 deferral（键盘循环、浮动 theme-toggle、旧页清理）；Goal Coverage 复验；375／1440／200%／coarse；双账号；v1＋v2 restore；`make check`。
- **为谁**：epic 收尾验收。
- **成功标准**：Goal Coverage 中本条 co-cover 的 core 行均有**完整矩阵**证据；无入口丢失／焦点陷阱／横向溢出。
- **明确不做**：首次实现核心协议（见术语）；不改 auth／settings 业务语义。

### 复杂度档位

生产 Web + 集成回归。偏离：`Compatibility = 深链迁移`；`Evidence = 多形态证据`。

### 关键决策

- **D1 redirect**：在 RequireAuth＋AppShell 保护组**内** `Navigate replace`；丢弃 search/hash；未登录深链 → 登录后二次落到新路径（`from` 旧路径 → replace）。旧 auth action token 路由不改。
- **D2 导航收口**：改 `AppShell` `navItems` 单点删除「设置」（侧栏＋底栏同步）；清理未用 Settings icon import。
- **D3 接收 deferral（闭合 BL-4）**：
  1. AccountMenu 实现 §4.6 ArrowUp/Down／Home/End **循环**（呈现实现，非核心协议）；
  2. 移除浮动 theme-toggle（含 `Sun`/`Moon` import 与 `.theme-toggle` CSS）；
  3. 按 **D11** 处置 legacy 测试后，删除不可达旧 `SettingsPage`／`AuthPageShell` 版 ChangePasswordPage；无路由死链。HTTP `/settings` API（`client.ts`／OpenAPI）**不是**死链。
- **D4 复验矩阵（闭合 BL-1）**：重跑 privacy／settings／profile 完整矩阵（非冒烟）。system-settings **accepted-residual**（跨标签页 settings 丢更新）命中记白名单，不按 D8 退回。
- **D5 证据形态（闭合 BL-2）**：模型测试／CSS 正则／人工断点截图／Go E2E+HTTP。截图落盘 `.codestable/features/2026-08-02-account-center-hardening/evidence/`（入 git）。
- **D6 restore（闭合 BL-3）**：v1 safety＋wipe（stopped→verify→`make migrate-up`，无臆造 flag）＋v2 mixed rehearsal；脚本未落盘则 STEP-000b blocked。
- **D7 测试挂载**：扩展 `test:account-center`；登记 Makefile；顺手登记 `test:customer-avatar`（纯登记）。
- **D8 缺陷分流**：
  | 类型 | 动作 |
  |---|---|
  | 核心协议／契约 | cs-issue＋items notes；blocked-on-upstream |
  | 呈现／集成 | 本条自修 |
  | accepted-residual | 白名单，不退回 |
  | legacy source-grep | D11 |
- **D9 范围词表**：禁 `schema_version` 升版、新 avatar writer、新 profile 表定义、新 SectionController 业务逻辑；禁对 `api/openapi.yaml` 与 `backend/internal/{accountprofile,avatarmedia,auth,dataexport}` 的非删除改动（纯删除／redirect／Makefile 登记除外）。
- **D10 分支门禁**：离开 `main`；问 owner 分支／worktree。
- **D11 legacy 测试处置（闭合 F-1）**：删旧页前，对 `settings.test.ts`／`auth.test.ts`／`telegram-digest.test.ts`／`data-export.test.ts` 等 `readFile` 旧路径断言逐条三选一并记录：
  1. 重定向到新组件路径（仅呈现／集成范围）；或
  2. 删除并注明已由 `test:account-center` 承接；或
  3. 退回 owner（上游正确性契约，本条不重写）。
  要求断言无净损失（A14），然后才删旧页，保证 A8 可绿。

### 执行风险与证据计划

- **Top 3**：① 偷补协议 → D8＋D9；② 删旧页撞 make check → D11；③ 前序脚本未落盘 → blocked。
- **必跑**：见 checklist。

## 2. 名词与编排

### 2.1 名词层

**现状（Wave 3 后）**：最终 `/account/*`；旧深链与主导航「设置」仍在；浮动 toggle／键盘循环／旧页可能残留。

**变化**：redirect；navItems；键盘循环；去 toggle＋死 CSS；D11 后删旧页；复验；v1＋v2 restore。

### 2.2 编排层

```mermaid
flowchart TD
  Old[/settings /change-password] -->|replace in auth| New[/account/*]
  Nav[navItems 删设置] --> Menu[AccountMenu + 键盘循环]
  Menu --> Toggle[移除浮动 theme-toggle]
  Menu --> D11[处置 legacy 测试断言]
  D11 --> Dead[删残留旧页]
  Menu --> Rev[完整复验]
  Rev --> A11y[人工断点矩阵]
  Rev --> E2E[Go 双账号 E2E]
  Rev --> RST[v1 + v2 mixed restore]
  RST --> Check[make check]
```

### 2.3 挂载点

1. App 路由 redirect（保护组内）  
2. `AppShell` `navItems`  
3. AccountMenu 键盘循环  
4. 移除浮动 theme-toggle＋死 CSS/import  
5. D11 后删除残留旧页  
6. `test:account-center`＋`test:customer-avatar` Makefile 登记  
7. v2 mixed restore 入口  
8. `.codestable/features/2026-08-02-account-center-hardening/evidence/`  
9. items.yaml → done  

### 2.4 / 2.5

见 checklist。硬化＝卸载旧入口＋接收 deferral＋证据；不扩域。

## 3. 验收契约

| ID | 触发 | 期望 | 证据 |
|---|---|---|---|
| A1 | 已登录 `/settings?x=1#y` | replace→`/account/settings`；无 query/hash | browser |
| A1b | 未登录 `/settings` | 登录后落 `/account/settings` | browser |
| A2 | `/change-password` | replace→`/account/security/password` | browser |
| A3 | 桌面／移动导航 | 无「设置」；六菜单可达 | browser |
| A4 | 菜单键盘 | 方向键循环＋Escape／焦点；双插槽 | model+css+screenshot |
| A5 | 375／1440／200%／coarse | 无入口丢失／横向溢出 | evidence/ 截图包 |
| A6 | 双 active 账号 | 资料／头像／设置／导出隔离 | Go E2E+HTTP |
| A7a | v1 restore＋migrate | stopped＋migrate-up 绿 | ops log |
| A7b | v2 mixed restore | customer＋account_profile current 均在 | ops log |
| A8 | `make check` | 全绿；含 customer-avatar；generate-check 零漂移 | command |
| A9 | 范围词表 | 无核心协议首次实现；openapi／所列包无非删除改动 | grep+report |
| A10 | 复验 privacy | 完整矩阵绿 | test:account-center |
| A11 | 复验 settings | 完整矩阵绿；accepted-residual 白名单 | test:account-center |
| A12 | 残留清理 | D11 后旧页已删；无路由死链；HTTP `/settings` API 保留 | diff+D11 log |
| A13 | 浮动 toggle | 组件＋import＋`.theme-toggle` CSS 清除 | diff |
| A14 | D11 承接 | 每个改动断言有重定向／承接／退回记录，无净损失 | report |

### Coverage Matrix

| 成功标准 | 场景 | checks |
|---|---|---|
| redirect／导航 | A1–A3 A1b | CHK-001–003 |
| deferral＋D11 | A4 A12–A14 | CHK-004 CHK-010–011 CHK-014 |
| a11y／响应式 | A4–A5 | CHK-004–005 |
| 复验／隔离／restore | A6 A7a/b A10 A11 | CHK-006–008 CHK-012–013 |
| 回归／ownership | A8 A9 | CHK-009 CHK-014 |

### DoD

核心证据齐全；Goal Coverage co-cover 行可勾；epic 完成信号可勾选。

## 4. 架构关系

- roadmap §4.6–4.7、§5 Ownership／Goal Coverage  
- 接收：profile-center D15／D5；privacy／system-settings 旧页清理预期  
- 发现核心缺陷 → D8 退回，不在本条补协议  
