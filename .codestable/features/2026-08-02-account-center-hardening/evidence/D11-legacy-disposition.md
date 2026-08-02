# D11 · Legacy source-grep 处置表（A14）

删旧页前对 `readFile` 旧路径断言逐条处置；要求断言无净损失。

| 源文件 | 原断言 | 处置 | 承接／替代 |
|---|---|---|---|
| `frontend/scripts/settings.test.ts` | SettingsPage 整表 freeze（`settings-edit-fields` + `updateSettings`） | **删除** | `test:account-center` A6/A11 section fieldset `disabled` + `canSubmitSettings` |
| `frontend/scripts/settings.test.ts` | SettingsPage `settingsSaveInFlightRef` 重入守卫 | **删除** | `test:account-center` A5c `settingsSaveQueue` 串行 PATCH |
| `frontend/scripts/auth.test.ts` | `ChangePasswordPage` 含 `changePassword` / `setAnonymous` | **重定向** | `ChangePasswordForm.tsx` |
| `frontend/scripts/auth.test.ts` | `AppShell` 含 `logoutSession` | **重定向** | `AccountMenu.tsx` |
| `frontend/scripts/auth.test.ts` | `SettingsPage` 含 `/change-password` + `logoutSession` | **重定向** | `AccountSecurityPage.tsx`（密码链 + logout） |
| `frontend/scripts/auth.test.ts` | `path="/change-password"` 仍挂载 | **保留并收紧** | 现为 `Navigate …/account/security/password replace` |
| `frontend/scripts/telegram-digest.test.ts` | SettingsPage TG 绑定文案／button／无 secret | **重定向** | `AccountSettingsPage.tsx`（适配新 `onClick={() => void onBindTelegram()}`） |
| `frontend/scripts/data-export.test.ts` | SettingsPage 不含 `DataExportCard` | **重定向** | `AccountSettingsPage.tsx` |
| `frontend/scripts/account-center.test.ts` | 读旧 SettingsPage／ChangePasswordPage；旧页仍挂路由 | **重定向** | 新 redirect／nav／toggle／键盘断言；承接面改 Account* |

**净损失**：无。删除的两条 settings 呈现断言由 account-center 五区控制器矩阵覆盖；其余均改指向新路径并保持等价契约。

**退回 owner**：无。
