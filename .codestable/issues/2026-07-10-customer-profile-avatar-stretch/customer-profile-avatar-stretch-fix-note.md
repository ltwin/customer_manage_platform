---
doc_type: issue-fix
issue: 2026-07-10-customer-profile-avatar-stretch
status: verified
path: fast-track
fix_date: 2026-07-10
tags: [frontend, css, customer-profile]
---

# 客户详情头像横向拉伸修复记录

## 1. 问题描述

客户详情页的档案头像在窄屏单列布局中被横向拉伸，原本应为圆形的头像显示成长椭圆。浏览器修复前实测尺寸为 `277px × 52px`。

## 2. 根因

`frontend/src/index.css` 中 `.profile-head > div { flex: 1 1 0; }` 同时命中了头像和右侧文字容器。该选择器覆盖了通用 `.avatar` 的 `flex: none`，导致头像参与剩余空间分配，宽度被拉伸而高度仍保持 `52px`。

## 3. 修复方案

将文字容器规则缩窄为 `.profile-head > div:not(.avatar)`，保留右侧文字区域的自适应能力，同时让头像继续使用通用 `.avatar` 的固定尺寸和 `flex: none`。未修改 React 组件、全局头像规则或其他页面布局。

## 4. 改动文件清单

- `frontend/src/index.css`：缩窄客户档案头部的 flex 选择器，显式排除头像。
- `frontend/scripts/avatar-layout.test.mjs`：新增样式回归测试，防止直接子 `div` 的伸缩规则再次作用于头像。
- `frontend/package.json`：新增 `test:avatar-layout` 测试命令。
- `Makefile`：将头像布局回归测试接入标准 `make test` / `make check` 门禁。
- `.codestable/issues/2026-07-10-customer-profile-avatar-stretch/worktree-override.md`：记录 owner 对当前 `develop` 检出直接修复的授权和范围。
- `.codestable/issues/2026-07-10-customer-profile-avatar-stretch/customer-profile-avatar-stretch-fix-note.md`：记录本次快速通道修复闭环。

## 5. 验证结果

- TDD RED：修复前运行 `node --test scripts/avatar-layout.test.mjs`，用例按预期失败，指出文字容器规则未排除 `.avatar`。
- TDD GREEN：修复后同一命令通过，`1` 个测试全部成功。
- 标准测试门禁：`make test` 通过；后端全部 package 通过，前端 package-price `4` 项、api-client `3` 项、schedule `23` 项及 avatar-layout `1` 项全部通过。
- 静态检查：`npm run lint` 通过，无 lint 错误。
- 生产构建：`npm run build` 通过；Vite 仅报告既有的单个 chunk 超过 `500 kB` 提示，不影响构建结果。
- 桌面浏览器：`1280 × 900` 下头像实测 `52px × 52px`、`border-radius: 50%`、`flex: 0 0 auto`，完整位于档案头部内。
- 手机浏览器：`390 × 844` 下头像实测 `52px × 52px`、`border-radius: 50%`、`flex: 0 0 auto`，与昵称文字无重叠。

## 6. 遗留事项

无。本次未发现需要另开 issue 的范围外问题，也未修改当前工作区中其他尚未提交的功能改动。
