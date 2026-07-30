---
doc_type: feature-ff-note
feature: referrer-select
date: 2026-07-08
requirement: customer-profile
tags: [frontend, customer-profile, ui]
---

## 做了什么
优化新建客户表单的「客户介绍」场景：介绍人不再手写客户 ID，而是从当前账号下可用的 active 客户下拉选择；下拉展示压缩为昵称 + 短 UID，不拼社交账号或来源渠道。

## 改了哪些
- `frontend/src/pages/CustomerNewPage.tsx:47` — 当来源渠道为客户介绍时加载 active 客户列表作为介绍人候选。
- `frontend/src/pages/CustomerNewPage.tsx:81` — 提交前校验客户介绍场景必须选择介绍人。
- `frontend/src/pages/CustomerNewPage.tsx:155` — 将介绍人输入框改为下拉选择，展示客户昵称与短 UID。
- `frontend/scripts/prototype-contract.test.mjs:39` — 增加回归检查，防止退回手写「介绍人客户 ID」或在下拉项拼来源渠道。
- `.codestable/roadmap/photographer-private-crm/photographer-private-crm-roadmap.md` — 将头像作为独立 `customer-avatar` planned 子 feature，后续再做头像上传与选择面缩略图。

## 怎么验证的
已先让新增回归检查失败，再实现并跑过 `npm run test:prototype`、`npm run build`、`npm run lint`、`make build`。浏览器在 `http://localhost:5173/customers/new` 切到「客户介绍」后确认介绍人字段为 select，选项显示如 `test002 · UID #b65b7d`，旧文案「介绍人客户 ID」已消失。
