#!/usr/bin/env bash
# scripts/githooks/pre-commit 的分派自测（GUAN-3 AC9）。
# 只在 mktemp 临时仓库里跑：桩 Makefile 的 lint-go / lint-frontend 只写标记，
# PATH 前置桩 golangci-lint / npm；不碰真实仓库的 git config。
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
hook="$repo_root/scripts/githooks/pre-commit"
[[ -x "$hook" ]] || { echo "pre-commit hook test: $hook missing or not executable" >&2; exit 1; }

tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT
stub_bin="$tmp_dir/bin"
marks="$tmp_dir/marks"
mkdir -p "$stub_bin"
printf '#!/bin/sh\nexit 0\n' >"$stub_bin/golangci-lint"
printf '#!/bin/sh\necho npm >>"$HOOKTEST_MARKS"\nexit 0\n' >"$stub_bin/npm"
chmod +x "$stub_bin/golangci-lint" "$stub_bin/npm"
export PATH="$stub_bin:$PATH" HOOKTEST_MARKS="$marks"

failures=0
output=""
status=0

new_repo() {
  local dir="$tmp_dir/repo-$1"
  git init -q "$dir"
  cd "$dir"
  git config user.name hooktest
  git config user.email hooktest@example.invalid
  mkdir -p scripts/githooks backend/internal docs frontend/src
  cp "$hook" scripts/githooks/pre-commit
  cat >Makefile <<'EOF'
lint-go:
	@echo lint-go >>"$(HOOKTEST_MARKS)"
	@test -z "$(HOOKTEST_FAIL_GO)"
lint-frontend:
	@echo lint-frontend >>"$(HOOKTEST_MARKS)"
EOF
  echo 'package internal' >backend/internal/a.go
  echo 'export const a = 1' >frontend/src/a.ts
  echo '# doc' >docs/a.md
  echo 'version: "2"' >.golangci.yml
  git add -A
  git commit -q -m init
  git config core.hooksPath scripts/githooks
  : >"$marks"
}

commit() {
  status=0
  output="$(git commit -q -m change 2>&1)" || status=$?
}

ran() { sort -u "$marks" | paste -sd, -; }

expect() {
  local name="$1" want_status="$2" want_ran="$3" status="$4"
  local got_ran
  got_ran="$(ran)"
  if [[ "$status" != "$want_status" || "$got_ran" != "$want_ran" ]]; then
    printf 'FAIL %s: want status=%s ran=[%s], got status=%s ran=[%s]\n%s\n' \
      "$name" "$want_status" "$want_ran" "$status" "$got_ran" "$output" >&2
    failures=$((failures + 1))
  else
    printf 'ok   %s\n' "$name"
  fi
}

new_repo docs-only
echo more >>docs/a.md && git add docs/a.md
commit
expect "只改 docs：不跑 lint，提交成功" 0 "" "$status"

new_repo go-only
echo '// x' >>backend/internal/a.go && git add backend/internal/a.go
commit
expect "只改 Go：只跑 lint-go" 0 "lint-go" "$status"

new_repo frontend-no-modules
echo '// x' >>frontend/src/a.ts && git add frontend/src/a.ts
commit
expect "缺 frontend/node_modules 改前端：拒绝且不装依赖" 1 "" "$status"
if [[ "$output" != *"make frontend-install"* ]]; then
  echo "FAIL 缺 node_modules 提示里没有 make frontend-install: $output" >&2
  failures=$((failures + 1))
fi

new_repo go-lint-fails
echo '// x' >>backend/internal/a.go && git add backend/internal/a.go
export HOOKTEST_FAIL_GO=1
commit
unset HOOKTEST_FAIL_GO
expect "lint-go 失败：提交被拒" 1 "lint-go" "$status"

new_repo golangci-config
echo '# x' >>.golangci.yml && git add .golangci.yml
commit
expect "改 .golangci.yml：跑 lint-go" 0 "lint-go" "$status"

new_repo go-moved-out
git mv backend/internal/a.go docs/a.txt
commit
expect "Go 文件移出 backend/：仍跑 lint-go" 0 "lint-go" "$status"

new_repo frontend-moved-out
mkdir -p frontend/node_modules
git mv frontend/src/a.ts docs/a.ts
commit
expect "前端文件移出 frontend/：仍跑 lint-frontend" 0 "lint-frontend" "$status"

new_repo go-deleted
git rm -q backend/internal/a.go
commit
expect "只删除 Go 文件：跑 lint-go" 0 "lint-go" "$status"

if ((failures)); then
  printf 'pre-commit hook dispatch: %d case(s) failed\n' "$failures" >&2
  exit 1
fi
printf 'pre-commit hook dispatch: passed (8 cases)\n'
