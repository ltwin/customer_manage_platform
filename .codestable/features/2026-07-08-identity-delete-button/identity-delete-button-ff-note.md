---
doc_type: feature-ff-note
feature: identity-delete-button
date: 2026-07-08
requirement:
tags: [frontend, customer-profile, ui]
---

## 做了什么
优化客户档案「私域账号」行内删除按钮：按钮改为暗红色危险操作样式，并固定在账号行右侧，避免掉到下一行。

## 改了哪些
- `frontend/src/components/customers/IdentitySection.tsx:75` — 给私域账号行增加专用布局 class，删除按钮切换为危险按钮样式。
- `frontend/src/index.css:196` — 新增暗红色实心危险按钮及 hover 变亮反馈。
- `frontend/src/index.css:523` — 私域账号行改为四列 grid，删除按钮右对齐且不参与换行。

## 怎么验证的
已运行 `npm run build` 与 `make build`，前端类型 / Vite 构建通过，且新前端产物已同步进后端 go:embed 目录。当前 `localhost:8081` 仍是运行中的旧二进制，重启后端后会加载新 bundle。
