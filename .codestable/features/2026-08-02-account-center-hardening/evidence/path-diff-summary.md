# Path diff summary（hardening 本条）

## 新增
- `frontend/src/account/legacyRedirects.ts` — 固定 redirect 纯函数
- `scripts/test-avatar-media-v2-mixed-restore.sh` — v2 mixed restore harness
- `.codestable/features/2026-08-02-account-center-hardening/evidence/*`

## 修改（呈现／测试／登记）
- `frontend/src/App.tsx` — 保护组内 `/settings`、`/change-password` → Navigate replace
- `frontend/src/components/AppShell.tsx` — navItems 去「设置」；去浮动 theme-toggle
- `frontend/src/account/AccountMenu.tsx` — ArrowUp/Down/Home/End 循环
- `frontend/src/index.css` — 清除 `.theme-toggle`
- `Makefile` — 登记 `test:customer-avatar`
- `frontend/scripts/{account-center,auth,settings,telegram-digest,data-export}.test.ts` — D11 + hardening 断言

## 删除
- `frontend/src/pages/SettingsPage.tsx`
- `frontend/src/pages/auth/ChangePasswordPage.tsx`

## A9 未触碰（本条）
- `api/openapi.yaml`
- `backend/internal/{accountprofile,avatarmedia,auth,dataexport}`（分支上 Wave1–3 既有改动不属于本条）
