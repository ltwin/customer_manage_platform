# STEP-004 · 断点矩阵证据（A5）— partial

本会话无法稳定启动人工浏览器完成截图包；以下为可复跑清单 + 已自动化覆盖。

## 可复跑人工清单

1. 已登录访问 `/settings?x=1#y` → 地址栏为 `/account/settings`（无 query/hash）。
2. 未登录访问 `/settings` → 登录后落 `/account/settings`。
3. `/change-password` → `/account/security/password`。
4. 桌面侧栏 + 移动底栏：无「设置」；六项业务导航可达。
5. AccountMenu：ArrowDown／Up 循环、Home／End、Escape 归还触发器焦点；主题仅菜单内切换。
6. 断点：
   - 375px 宽：底栏六项无横向溢出；mobile 账号菜单插槽可见；desktop 插槽 `display:none`。
   - 1440px：侧栏六项 + desktop 账号菜单；无浮动 theme-toggle。
   - 200% 缩放：账号菜单与设置五区主操作仍可达。
   - coarse pointer：菜单项与设置保存按钮可点，无焦点陷阱。

## 自动化已覆盖（替代部分截图）

| 断言 | 位置 |
|---|---|
| 双插槽 CSS `display:none`／mobile 切换 | `test:account-center` A8 dual-slot |
| 方向键循环模型 `nextMenuIndex` + AccountMenu 源码 | hardening A4 |
| navItems 六项、无设置、无 `.theme-toggle` | hardening A3/A13 |
| redirect 纯函数 + App 路由 | hardening A1/A2 |

## 状态

`partial` — 模型／CSS 断言绿；人工截图待 owner 浏览器补齐后可将本文件升级为 `complete` 并放入 PNG。
