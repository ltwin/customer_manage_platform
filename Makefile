# 命令基线：make check 是全 roadmap 后续 feature 的验证入口（roadmap §6）。

.PHONY: check build lint test generate generate-check db-up backend-build frontend-build frontend-install

check: build lint test generate-check

build: backend-build frontend-build

backend-build:
	cd backend && go build ./...

frontend-build: frontend/node_modules
	cd frontend && npm run build

frontend/node_modules: frontend/package-lock.json
	cd frontend && npm ci
	touch frontend/node_modules

frontend-install: frontend/node_modules

lint:
	cd backend && golangci-lint run ./...
	cd frontend && npm run lint

test:
	cd backend && go test ./...

# 契约线（design 2.2）：api/openapi.yaml -> Go 服务端类型（按 tag）+ TS 全量类型
generate: frontend/node_modules
	cd backend && go tool oapi-codegen -config oapi-codegen.yaml ../api/openapi.yaml
	cd frontend && npm run generate

# 漂移检查：生成物必须与契约同步提交（CMD-002 / A11）
generate-check: generate
	git diff --exit-code -- backend/internal/platform/httpapi/api.gen.go frontend/src/api/schema.d.ts

# 本地 dev 库：单起 compose 的 postgres 服务（全容器模式见 docker-compose.yml 注释）
db-up:
	docker compose up -d --wait postgres
