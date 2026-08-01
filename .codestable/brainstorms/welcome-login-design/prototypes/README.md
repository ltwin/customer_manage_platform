# 欢迎页 / 登录页 原型存档

2026-07-30 归档。这两个 HTML 是欢迎页与登录页的**静态原型**，已被 React 实现取代，不参与构建。

## 现行实现

| 原型 | 现行实现 |
|---|---|
| `welcome-page.html` | `frontend/src/pages/WelcomePage.tsx` + `WelcomePage.css` |
| `login-page.html` | `frontend/src/pages/LoginPage.tsx` + `LoginPage.css` |

以 React 版本为准。原型仅供回看当初的视觉与交互意图。

原型中的验证码 `123456` 与密码 `photo2026` 只用于静态页面交互演示，不是应用账号、环境变量或生产凭证，也不参与现行登录流程。

## 与实现的已知差异

- **手机号验证码通道**：原型里是完整可跑的（60 秒倒计时、演示验证码 `123456`）。React 版按决策 1B **禁用并标注「即将开放」**——`api/openapi.yaml` 当前只有 `/auth/login` 单账号密码登录，没有发码/校验接口。若后续补上 SMS 后端，原型里的倒计时与错误态草图可作参考。
- **设计 token**：原型内嵌了一份 `:root` token 副本；React 版改为消费 `frontend/src/index.css`，因而能跟随暗色主题。原型不跟随主题。
- **图标**：原型是手写 SVG；React 版统一换成 `lucide-react`。

## 图片

图片与 favicon 不在本目录，引用相对路径指向 `frontend/public/`（React 版在用的同一批文件），避免重复占用仓库空间。保持当前仓库目录结构时，直接用浏览器打开 HTML 即可正常显示。
