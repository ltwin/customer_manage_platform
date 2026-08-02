---
doc_type: feature-design
feature: 2026-08-02-account-privacy-security
requirement: account-center
roadmap: account-center
roadmap_item: account-privacy-security
execution_lane: goal
execution_lane_reason: epic child batch under account-center
status: approved
summary: 在既有 /account/security* URL 上将兼容组合面升级为最终身份／安全／导出信息架构，不改 auth／dataexport 后端语义
tags: [account, security, privacy, frontend, export]
---

# account-privacy-security · 隐私与安全最终 IA design

## 0. 术语约定

| 术语 | 定义 | 防冲突结论 |
|---|---|---|
| account-security-surface | `/account/security*` 前端组合面 | 不新增万能 security endpoint；owner 仍是 auth／dataexport |
| 最终安全 IA | 身份卡＋安全动作＋数据导出＋边界说明的分区呈现 | 相对 profile-center **已交付的兼容组合面**做信息架构升级，URL 不变 |
| change-password 401 例外 | 当前密码错误→401，但**不得**走 auth single-flight refresh／replay | 必须继续 `requestWithoutAuthRetry`；否则误登出 |
| 全设备退出语义 | 改密成功 → 服务端撤全部 refresh family＋清 cookie＋前端 `setAnonymous`＋登录页 notice | 服务端保证；前端不复制撤销逻辑 |
| 当前会话退出 | 仅撤当前 family；服务端不可达仍清本地认证内存 | 与既有 logout 一致 |
| 边界文案 | 不做设备中心／换绑／删号／登录历史／OAuth／彻底擦除备份；头像 24h GC／备份边界 | 产品承诺；按 export v3 **实际结构**写导出提示 |
| 已验证标签 | 静态文案；active account 即前置，**无** `email_verified` 字段 | 禁止读不存在字段 |

术语对齐 roadmap §4.7–4.8；export schema v3 **已由** profile-center 拥有，本条只消费／**修正展示文案**。

## 1. 决策与约束

### 需求摘要

- **做什么**：在 profile-center 已挂好的 `/account/security*` 兼容面上，升级为最终 IA（身份卡分区、安全动作分区、导出分区、边界说明）；按 v3 复核导出文案；固化改密 no-auth-retry 与成功 notice 渲染。
- **为谁**：需要在用户中心完成身份确认、改密、退出、导出的摄影师。
- **成功标准**：邮箱只读＋静态「已验证」；改密全设备退出且错误密码不登出；当前退出；导出成功／失败／取消可观测；边界说明可见；旧 `/change-password` 仍可成功改密。
- **明确不做**：设备／会话中心、换绑邮箱、删号、擦除备份、新 security API、重写 auth／dataexport、首次实现 export v3、旧路由 replace redirect、主导航删除、把安全卡加回旧 SettingsPage。

### 复杂度档位

生产 Web 默认。偏离：`Compatibility = 改密 401 例外路径`。

### 关键决策

- **D1 URL 冻结**：不改 `/account/security`、`/account/security/password`。
- **D2 前置基线（闭合 B2）**：本条实现前，profile-center D4 **交付后应存在**——`/account/security` 挂导出＋退出＋改密入口；Settings 组件（含旧 `/settings` 与兼容 `/account/settings` 共用时）**已剥离**安全／导出；共享改密 form 供 layout 内与旧 `/change-password` 共用；`test:account-center` 已在 `package.json`＋`Makefile`。未齐则 STEP-000b blocked。本条 delta＝**IA 分区呈现＋身份卡＋边界文案＋导出文案按 v3 修正＋改密成功 notice／401 例外固化**，不是再次「迁入」。
- **D3 改密 401 例外（闭合 B1）**：共享 form／client 必须继续 `requestWithoutAuthRetry`；A2 断言无 refresh 发出且 snapshot 仍 authenticated。§4.10 泛化「401→single-flight」**不覆盖**本例外——本例外已回写 roadmap §4.10（2026-08-02 契约回写），实现以 roadmap 为准。
- **D4 改密成功**：`setAnonymous`＋登录页**实际渲染** notice「密码已修改，所有设备需要重新登录。」（不只 URL）；处理与 `RequireAuth` Navigate 的竞态（notice 优先或统一 state）。
- **D5 身份卡**：消费 `AccountCenterContext.account`（或等价 snapshot.account）；禁止第二套收窄／复制 `parseAccount`；静态「已验证」。
- **D6 导出文案**：按 v3 复核——账号头像仅 metadata（version／media_type／size／updated_at），**无** `avatar_url`／object_id／bytes；修正现有「公开头像引用」类过时措辞；内容正确性仍归前置。
- **D7 导出取消判据**：应用内无取消按钮；「取消」＝浏览器保存对话框取消 → 无文件落盘＋无残留 loading＋无错误态。
- **D8 边界词表**：设备中心／换绑／删号／登录历史／安全事件／OAuth／SSO／手机号身份／彻底擦除备份；头像 24h GC＋历史备份边界；可给一句从旧「设置」导航到用户中心的指路（旧 `/settings` 已无安全块）。
- **D9 旧入口**：不删 `/change-password`、不建 redirect；footer 在最终页回 `/account/security`（旧页 footer 可仍指向旧路径直至 hardening）。
- **D10 测试**：断言追加进 **`test:account-center`**（不另立 `test:account-security`）；登记 Makefile（若尚未）。
- **D11 分支门禁**：实现前问 owner 当前分支或新开 worktree；离开受保护 `main`；提交需人工同意。

### 执行风险与证据计划

- **Top 3**：① 误用 authorizedFetch 导致误登出 → A2；② 导出文案与 v3 不一致 → A6；③ layout 内 notice 竞态 → A4。
- **非显然依赖**：profile-center 兼容面与共享 form／`test:account-center`（实现前由 STEP-000b 核对，非当前仓库既成事实）。
- **清洁度**：禁新后端域；禁 redirect／主导航删除；禁把安全卡加回旧 Settings。

## 2. 名词与编排

### 2.1 名词层

**现状（profile-center 完成后）**：`/account/security*` 兼容组合面已存在；共享改密 form；Settings 兼容面无安全／导出；旧 `/change-password` 仍可用；`changePassword` 已用 `requestWithoutAuthRetry`；DataExportCard 文案可能仍偏 v2。

**变化**：最终 IA 分区组件／文案；导出提示按 v3；身份卡；改密成功 notice 断言强化；边界词表。

**接口**：无新 OpenAPI；零改动 `backend/internal/platform/auth/**`、dataexport、`api/openapi.yaml`（A8 路径级 diff）。

### 2.2 编排层

```mermaid
flowchart LR
  Menu[AccountMenu] --> Sec[/account/security 最终 IA]
  Sec --> Id[身份卡静态已验证]
  Sec --> PW[/account/security/password]
  Sec --> LO[logout]
  Sec --> EX[DataExportCard v3 copy]
  PW --> API[changePassword no-auth-retry]
  API -->|401| Stay[仍登录 + 文案]
  API -->|204| Out[anonymous + notice 渲染]
```

**流程级约束**：改密 401 **不**走 single-flight；其他 401 仍走既有路径；一区失败不屏蔽其余 account 路由。

### 2.3 挂载点

1. `/account/security` 最终 IA 组件（替换兼容呈现）  
2. `/account/security/password` 最终文案／footer（共享 form 已存在则只升级壳）  
3. DataExportCard（或包装）文案  
4. `test:account-center` 追加本条用例＋Makefile  
5. items.yaml  

### 2.4 / 2.5

见 checklist。结构：`frontend/src/account/security/` 升级呈现；**不**再从 SettingsPage「迁出」安全卡（已剥离）。微重构＝IA 组件，不改 auth 包。

## 3. 验收契约

| ID | 触发 | 期望 | 证据 |
|---|---|---|---|
| A1 | 打开 `/account/security` | 身份卡邮箱＋静态已验证；无设备／换绑／删号／登录历史控件 | browser |
| A2 | 改密错误当前密码 | 401 文案；仍 authenticated；**无** refresh 请求 | browser+network |
| A3 | 改密限速 | 429 文案（可 API 层证据） | browser/API |
| A4 | 改密成功 | 全设备退出；登录页**渲染** notice；第二 cookie jar refresh→401 | browser+API |
| A5 | 退出当前登录 | 仅当前会话；服务端失败仍清本地 | browser |
| A6 | 导出 | 成功完整；取消＝无落盘／无残留 loading／无错误（取证前须开启浏览器「下载前询问保存位置」）；失败提示；文案符合 v3（无 avatar_url 暗示） | browser+JSON |
| A7 | 边界文案 | 含 D8 词表＋GC／备份 | screenshot |
| A8 | 范围 | **后端** `platform/auth/**`、dataexport、`api/openapi.yaml` 零改动（前端 DataExportCard 文案属本条交付）；无 redirect／五区 Settings；无安全卡加回旧 Settings | path diff |
| A9 | 旧 `/change-password` | 仍可成功改密且语义一致 | browser |

### Coverage Matrix

| 成功标准 | 场景 | checks |
|---|---|---|
| 身份／改密／退出 | A1–A5 A9 | CHK-001–005 CHK-008 |
| 导出／边界／范围 | A6–A8 | CHK-006–007 |
| 门禁／挂载 | STEP／CMD | CHK-009 |

### DoD

`test:account-center` 本条用例绿；A2／A4 证据齐全；checks 可勾选。

## 4. 架构关系

- roadmap §4.7–4.8；§4.10 泛化不覆盖改密 401 例外  
- 前置：profile-center D4 兼容面（本条不重做迁入）  
- 后续：hardening redirect／导航删除／残留页清理  
- Learning：`requestWithoutAuthRetry` 为刻意例外，宜后续 cs-keep  
