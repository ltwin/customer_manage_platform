# CRM 前端

React + TypeScript + Vite 单页应用。开发和部署总览见仓库根 `README.md`；接口语义以 `../api/openapi.yaml` 为机器契约。

## 本地开发

```bash
npm ci
npm run dev
```

Vite 开发服务器默认打开 `http://localhost:5173`，并把 `/api` 代理到后端 `:8080`。若提示 `vite: command not found`，先在本目录执行 `npm ci`。

## 验证与生成

```bash
npm run lint
npm run build
npm run test:package-price
npm run test:api-client
npm run test:schedule
npm run test:avatar-layout
npm run generate
```

仓库级聚合门禁仍使用根目录的 `make check`。`npm run generate` 从 `../api/openapi.yaml` 生成 `src/api/schema.d.ts`。

## 约束

- API DTO 必须引用 `src/api/schema.d.ts` 的生成类型，不手写重复 DTO。
- HTTP 调用统一收口在 `src/api/client.ts`，组件不直接拼 URL。
- 账号时区来自 `GET /me.timezone`；日历和历史时间表单不使用浏览器时区兜底。
- 编码与 review 口径见 `../docs/frontend-style-checklist.md`。
