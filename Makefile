# 命令基线：make check 是全 roadmap 后续 feature 的验证入口（roadmap §6）。

.PHONY: check build lint test generate generate-check db-up migrate-up backend-build frontend-build frontend-install webui-sync planning-ops-safety planning-hardening test-go test-frontend test-ops check-frontend check-go check-ops

BUILD_REVISION ?= $(shell git rev-parse HEAD 2>/dev/null || printf development)

# 后端测试范围，可被分层门禁覆盖：make check-go PKG=./internal/order/...
PKG ?= ./...
GOTEST_P ?= 4

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

# 全量测试 = 三段之和。分段是为了让分层门禁能只取其一（范式见 AGENTS.md），
# 组合而非各写一份清单：手工清单曾漂移过——三个 test:* 脚本存在却从未进门禁。
test: test-go test-frontend test-ops

# 并发度 4 是实测上限。容器等待策略补上端口监听后（storetest.waitReady），
# -p=4 连续六轮全绿、约 50s，串行是 158s；-p=8 会把 Docker（12 核 / 8GB）压到
# 容器启动排队，实测十分钟跑不完。调高前先按 AGENTS.md 的稳定性判据实测。
test-go:
	cd backend && go test -p=$(GOTEST_P) $(PKG) -count=1 -parallel=$(GOTEST_P)

# glob 自动发现，新增 scripts/*.test.* 无需改 Makefile。
# *.e2e.mjs 不匹配 node 的测试文件名模式，故不会被卷进来——它需要 Playwright、
# 运行中的前端和一对真实账号口令，只能手动跑。
test-frontend: frontend/node_modules
	cd frontend && node --test --experimental-transform-types "scripts/*.test.ts" "scripts/*.test.mjs"

test-ops:
	./scripts/test-auth-legacy-cutover.sh
	./scripts/test-auth-security-catalog.sh
	bash ./scripts/test-v1-ops-common.sh
	python3 ./scripts/lib/v1-ops-package-selftest.py
	bash ./scripts/test-v1-ops-backup-restore-safety.sh
	bash ./scripts/test-planning-ops-backup-restore-safety.sh
	bash ./scripts/test-planning-hardening.sh
	./scripts/test-v1-ops-contract.sh

# ---- 分层门禁 ----
# 按影响面选门禁，不要习惯性跑 check。判定表见 AGENTS.md「验证范式」。

# 前端改动（CSS / 组件 / 页面）：lint + 构建 + 全部前端单测
check-frontend: frontend/node_modules
	cd frontend && npm run lint
	cd frontend && npm run build
	$(MAKE) test-frontend

# 后端限定包：make check-go PKG=./internal/order/...
check-go:
	cd backend && go build ./...
	cd backend && golangci-lint run $(PKG)
	$(MAKE) test-go PKG=$(PKG)

# 部署脚本 / ops 契约改动
check-ops: test-ops

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
