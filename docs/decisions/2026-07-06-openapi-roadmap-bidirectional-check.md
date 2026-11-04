# OpenAPI 与 roadmap 契约双向核对

## 背景

platform-skeleton 把 roadmap §4 的 HTTP 契约固化为 `api/openapi.yaml`，再由 oapi-codegen / openapi-typescript 生成双端类型。这个步骤如果只检查“生成能跑”，仍可能发生两类漂移：roadmap 已写的端点漏进 OpenAPI，或 OpenAPI 多了 roadmap 没拍板的端点。

## 结论

后续所有进入 `api/openapi.yaml` 的端点必须满足一个来源规则：

- 能精确指向 roadmap §4 的条文；或
- 被当前 feature design 明确列为临时白名单，并在 acceptance 提示 owner 后续走 `cs-roadmap update` 收编。

验收时做双向核对：

- roadmap §4 → OpenAPI：应实现的 HTTP 端点不能漏。
- OpenAPI → roadmap §4：OpenAPI 中不能私自新增未拍板端点。
- `make generate && git diff --exit-code -- backend/internal/platform/httpapi/api.gen.go frontend/src/api/schema.d.ts` 必跑，确认生成物无漂移。

## 证据

- 契约源：`.codestable/roadmap/photographer-private-crm/photographer-private-crm-roadmap.md` §4
- 机器契约：`api/openapi.yaml`
- codegen 目标：`backend/internal/platform/httpapi/api.gen.go`、`frontend/src/api/schema.d.ts`
- platform-skeleton 特例：`GET /api/v1/me` 是完成信号白名单，验收已提示后续收编进 roadmap §4
