# 命令基线：make check 是全 roadmap 后续 feature 的验证入口（roadmap §6）。

.PHONY: check build lint test generate generate-check db-up migrate-up backend-build frontend-build frontend-install webui-sync planning-ops-safety planning-hardening

BUILD_REVISION ?= $(shell git rev-parse HEAD 2>/dev/null || printf development)

check: build lint test generate-check

build: backend-build frontend-build

# 顺序约束（design 2.2）：前端构建先于后端编译（go:embed 输入）
backend-build: webui-sync
	cd backend && go build ./...
	cd backend && go build -o bin/server ./cmd/server
	cd backend && go build -ldflags "-X main.runtimeBuildRevision=$(BUILD_REVISION)" -o bin/accountctl ./cmd/accountctl

frontend-build: frontend/node_modules
	cd frontend && npm run build

# 把前端构建产物同步进 go:embed 输入目录（产物不入 git，.gitkeep 除外）
webui-sync: frontend-build
	rm -rf backend/internal/platform/webui/dist
	cp -R frontend/dist backend/internal/platform/webui/dist
	touch backend/internal/platform/webui/dist/.gitkeep

frontend/node_modules: frontend/package-lock.json
	cd frontend && npm ci
	touch frontend/node_modules

frontend-install: frontend/node_modules

lint:
	cd backend && golangci-lint run ./...
	cd frontend && npm run lint

test:
	# Testcontainers 逐包并发会偶发丢失 PostgreSQL mapped port；串行 package/test 保持门禁稳定。
	cd backend && go test -p=1 ./... -count=1 -parallel=1
	cd frontend && npm run test:customer-money
	cd frontend && npm run test:package-price
	cd frontend && npm run test:api-client
	cd frontend && npm run test:auth
	cd frontend && npm run test:schedule
	cd frontend && npm run test:openings-golden
	cd frontend && npm run test:dashboard-v2
	cd frontend && npm run test:order-payment
	cd frontend && npm run test:dashboard-v2-layout
	cd frontend && npm run test:settings
	cd frontend && npm run test:avatar-layout
	cd frontend && npm run test:app-shell-layout
	cd frontend && npm run test:telegram-digest
	cd frontend && npm run test:data-export
	cd frontend && npm run test:account-center
	cd frontend && npm run test:customer-avatar
	cd frontend && npm run test:v1-hardening
	cd frontend && npm run test:shoot-planning
	cd frontend && npm run test:planning-media
	cd frontend && npm run test:plan-ingestion
	cd frontend && npm run test:shoot-plan-crm
	cd frontend && npm run test:plan-share
	node --test frontend/scripts/planning-prototype-v2.test.mjs
	./scripts/test-auth-legacy-cutover.sh
	./scripts/test-auth-security-catalog.sh
	bash ./scripts/test-v1-ops-common.sh
	python3 ./scripts/lib/v1-ops-package-selftest.py
	bash ./scripts/test-v1-ops-backup-restore-safety.sh
	bash ./scripts/test-planning-ops-backup-restore-safety.sh
	bash ./scripts/test-planning-hardening.sh
	./scripts/test-v1-ops-contract.sh

planning-ops-safety:
	bash ./scripts/test-planning-ops-backup-restore-safety.sh

planning-hardening:
	bash ./scripts/test-planning-hardening.sh

# 契约线（design 2.2）：api/openapi.yaml -> Go 服务端类型（按 tag）+ TS 全量类型
generate: frontend/node_modules
	cd backend && go tool oapi-codegen -config oapi-codegen.yaml ../api/openapi.yaml
	cd backend/internal/shootplanning/httpcontract && go tool oapi-codegen -config oapi-codegen.yaml ../../../../api/openapi.yaml
	cd frontend && npm run generate

# 漂移检查：比较生成前后内容，允许 feature 在提交前验证已同步的生成物（CMD-002 / A11）
generate-check: frontend/node_modules
	@tmp_dir="$$(mktemp -d)"; \
	trap 'rm -rf "$$tmp_dir"' EXIT; \
	cp backend/internal/platform/httpapi/api.gen.go "$$tmp_dir/api.gen.go"; \
	cp backend/internal/shootplanning/httpcontract/api.gen.go "$$tmp_dir/shootplanning.gen.go"; \
	cp frontend/src/api/schema.d.ts "$$tmp_dir/schema.d.ts"; \
	$(MAKE) generate; \
	cmp -s "$$tmp_dir/api.gen.go" backend/internal/platform/httpapi/api.gen.go \
		|| { echo "Go OpenAPI 生成物存在漂移" >&2; exit 1; }; \
	cmp -s "$$tmp_dir/shootplanning.gen.go" backend/internal/shootplanning/httpcontract/api.gen.go \
		|| { echo "shootplanning Go OpenAPI 生成物存在漂移" >&2; exit 1; }; \
	cmp -s "$$tmp_dir/schema.d.ts" frontend/src/api/schema.d.ts \
		|| { echo "TypeScript OpenAPI 生成物存在漂移" >&2; exit 1; }

# 本地 dev 库：单起 compose 的 postgres 服务（全容器模式见 docker-compose.yml 注释）
db-up:
	docker compose up -d --wait postgres

# schema 迁移手动入口（CMD-003）；服务启动时也会自动执行同一迁移
migrate-up:
	cd backend && go run ./cmd/migrate
