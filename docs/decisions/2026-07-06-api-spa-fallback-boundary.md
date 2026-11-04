# API 路径与 SPA fallback 边界

## 背景

platform-skeleton QA 发现 Gin 默认行为会让部分 API 边缘路径绕开统一错误封套：`/api/v1/me/` 会被尾斜杠重定向，`/api`、`//api/v1/me`、非 GET 的非 API 请求可能落进 SPA fallback，导致“API 非 2xx 必须 ErrorEnvelope”和“SPA 只承接前端导航”两个契约边界变模糊。

## 结论

后续新增 API / 前端路由时，API 与 SPA fallback 必须显式分界：

- Gin router 设置 `RedirectTrailingSlash = false`，不要让尾斜杠自动 301 绕过 API 封套。
- NoRoute 里把 `/api`、`/api/...`、`//api/...` 都识别为 API 路径，统一返回 404 `not_found` ErrorEnvelope。
- SPA fallback 只承接浏览器导航语义的 `GET` / `HEAD`；`POST /whatever` 这类非导航请求不应返回前端 HTML。
- 回归测试必须覆盖 `/api/v1/me/`、`/api`、`//api/v1/me`、非 API POST 四类旁路。

## 证据

- 修复落点：`backend/internal/platform/httpapi/router.go`
- 回归测试：`backend/internal/platform/httpapi/router_test.go`
- 验证命令：`go test -count=1 ./internal/platform/httpapi`
- 验收记录：`.codestable/features/2026-07-06-platform-skeleton/platform-skeleton-acceptance.md` 的 REV-007 缺口修复记录
