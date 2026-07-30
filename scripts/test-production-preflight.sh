#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
preflight="$repo_root/scripts/production-preflight.sh"
work_dir="$(mktemp -d)"
trap 'rm -rf "$work_dir"' EXIT
case_count=0
TEST_PATH="$PATH"
TEST_DOCKER_HOST=""
FAKE_ENDPOINT="unix:///var/run/docker.sock"
FAKE_VERSION="2.24.0"
FAKE_RENDER_FILE=""
DOCKER_LOG="$work_dir/docker.log"

avatar_root="$work_dir/avatars"
mkdir -p "$avatar_root"

write_binary_env() {
  local destination="$1"
  local seed_password="${2:-}"
  {
    printf '%s\n' 'DATABASE_URL=postgres://crm:production-password@localhost:5432/crm?sslmode=disable'
    printf '%s\n' 'AUTH_TOKEN_SECRET=0123456789abcdef0123456789abcdef'
    printf 'SEED_ADMIN_PASSWORD=%s\n' "$seed_password"
    printf '%s\n' 'HTTP_ADDR=127.0.0.1:8080'
    printf '%s\n' 'AVATAR_STORAGE_DRIVER=local'
    printf 'AVATAR_LOCAL_ROOT=%s\n' "$avatar_root"
    printf '%s\n' 'AVATAR_LOCAL_REQUIRE_MOUNT=false'
    printf '%s\n' 'TELEGRAM_BOT_TOKEN='
    printf '%s\n' 'TELEGRAM_BOT_USERNAME='
  } >"$destination"
}

run_preflight() {
  local name="$1"
  local expected_exit="$2"
  shift 2
  case_count=$((case_count + 1))
  set +e
  env -i PATH="$TEST_PATH" HOME="${HOME:-}" TMPDIR="${TMPDIR:-/tmp}" \
    DOCKER_HOST="$TEST_DOCKER_HOST" FAKE_ENDPOINT="$FAKE_ENDPOINT" \
    FAKE_VERSION="$FAKE_VERSION" FAKE_RENDER_FILE="$FAKE_RENDER_FILE" DOCKER_LOG="$DOCKER_LOG" \
    "$preflight" "$@" >"$work_dir/$name.out" 2>"$work_dir/$name.err"
  local observed_exit=$?
  set -e
  if [[ "$observed_exit" -ne "$expected_exit" ]]; then
    printf 'case %s: exit=%s want=%s\n' "$name" "$observed_exit" "$expected_exit" >&2
    sed -n '1,80p' "$work_dir/$name.out" >&2
    sed -n '1,80p' "$work_dir/$name.err" >&2
    return 1
  fi
}

write_managed_env() {
  local destination="$1"
  local seed_password="${2:-}"
  {
    printf '%s\n' 'DATABASE_URL=postgres://ignored:ignored@ignored.invalid/ignored'
    printf '%s\n' 'APP_DATABASE_URL='
    printf '%s\n' 'POSTGRES_USER=crm'
    printf '%s\n' 'POSTGRES_PASSWORD=managed-production-password'
    printf '%s\n' 'POSTGRES_DB=crm'
    printf '%s\n' 'AUTH_TOKEN_SECRET=0123456789abcdef0123456789abcdef'
    printf 'SEED_ADMIN_PASSWORD=%s\n' "$seed_password"
    printf '%s\n' 'TELEGRAM_BOT_TOKEN='
    printf '%s\n' 'TELEGRAM_BOT_USERNAME='
  } >"$destination"
}

write_external_env() {
  local destination="$1"
  {
    printf '%s\n' 'DATABASE_URL=postgres://ignored:ignored@ignored.invalid/ignored'
    printf '%s\n' 'APP_DATABASE_URL=postgres://crm:external-production-password@db.example/crm?sslmode=require'
    printf '%s\n' 'POSTGRES_USER=ignored'
    printf '%s\n' 'POSTGRES_PASSWORD=ignored'
    printf '%s\n' 'POSTGRES_DB=ignored'
    printf '%s\n' 'AUTH_TOKEN_SECRET=0123456789abcdef0123456789abcdef'
    printf '%s\n' 'SEED_ADMIN_PASSWORD='
    printf '%s\n' 'TELEGRAM_BOT_TOKEN='
    printf '%s\n' 'TELEGRAM_BOT_USERNAME='
  } >"$destination"
}

write_render() {
  local destination="$1"
  local database_url="$2"
  local auth_secret="${3:-0123456789abcdef0123456789abcdef}"
  printf '%s' '{"services":{"app":{"image":"crm:local","environment":{' >"$destination"
  printf '"AUTH_TOKEN_SECRET":"%s",' "$auth_secret" >>"$destination"
  printf '%s' '"HTTP_ADDR":":8080","AVATAR_STORAGE_DRIVER":"local","AVATAR_LOCAL_ROOT":"/var/lib/crm/avatars","AVATAR_LOCAL_REQUIRE_MOUNT":"true","SEED_ADMIN_PASSWORD":"","TELEGRAM_BOT_TOKEN":"","TELEGRAM_BOT_USERNAME":"",' >>"$destination"
  printf '"DATABASE_URL":"%s"' "$database_url" >>"$destination"
  printf '%s' '},"volumes":[{"type":"volume","source":"avatar_data","target":"/var/lib/crm/avatars"}]},"postgres":{"image":"postgres:17-alpine","environment":{},"volumes":[{"type":"volume","source":"pgdata","target":"/var/lib/postgresql/data"}]}},"volumes":{"avatar_data":{},"pgdata":{}}}' >>"$destination"
}

fake_bin="$work_dir/fake-bin"
mkdir -p "$fake_bin"
printf '%s\n' '#!/bin/bash' >"$fake_bin/docker"
printf '%s\n' 'printf "%s\n" "$*" >>"$DOCKER_LOG"' >>"$fake_bin/docker"
printf '%s\n' 'if [[ "$1" == "context" && "$2" == "inspect" ]]; then printf "%s\n" "$FAKE_ENDPOINT"; exit 0; fi' >>"$fake_bin/docker"
printf '%s\n' 'if [[ "$1" != "--host" ]]; then exit 91; fi' >>"$fake_bin/docker"
printf '%s\n' 'shift 2' >>"$fake_bin/docker"
printf '%s\n' 'if [[ "$1" == "info" ]]; then printf "%s\n" "engine-fixture"; exit 0; fi' >>"$fake_bin/docker"
printf '%s\n' 'if [[ "$1" == "compose" && "$2" == "version" ]]; then printf "%s\n" "$FAKE_VERSION"; exit 0; fi' >>"$fake_bin/docker"
printf '%s\n' 'if [[ "$1" == "compose" ]]; then /bin/cat "$FAKE_RENDER_FILE"; exit 0; fi' >>"$fake_bin/docker"
printf '%s\n' 'exit 92' >>"$fake_bin/docker"
chmod +x "$fake_bin/docker"

valid_env="$work_dir/binary.env"
write_binary_env "$valid_env"
run_preflight binary-valid 0 \
  --mode binary --seed-state initialized --env-file "$valid_env"
grep -qx 'complete/preflight=ok' "$work_dir/binary-valid.out"

duplicate_env="$work_dir/duplicate.env"
write_binary_env "$duplicate_env"
printf '%s\n' 'AUTH_TOKEN_SECRET=another-value-that-must-not-be-echoed' >>"$duplicate_env"
run_preflight env-duplicate 3 \
  --mode binary --seed-state initialized --env-file "$duplicate_env"
grep -q 'error code=3 stage=env-parse key=ENV_FILE' "$work_dir/env-duplicate.err"

missing_auth_env="$work_dir/missing-auth.env"
write_binary_env "$missing_auth_env"
sed '/^AUTH_TOKEN_SECRET=/d' "$missing_auth_env" >"$work_dir/missing-auth-filtered.env"
run_preflight auth-missing 4 \
  --mode binary --seed-state initialized --env-file "$work_dir/missing-auth-filtered.env"
grep -q 'error code=4 stage=config-matrix key=AUTH_TOKEN_SECRET' "$work_dir/auth-missing.err"

invalid_key_env="$work_dir/invalid-key.env"
write_binary_env "$invalid_key_env"
printf '%s\n' 'export BAD=value' >>"$invalid_key_env"
run_preflight env-invalid-key 3 \
  --mode binary --seed-state initialized --env-file "$invalid_key_env"

unclosed_env="$work_dir/unclosed.env"
write_binary_env "$unclosed_env"
printf '%s\n' "UNTRUSTED='unterminated" >>"$unclosed_env"
run_preflight env-unclosed-quote 3 \
  --mode binary --seed-state initialized --env-file "$unclosed_env"

marker="$work_dir/command-marker"
injection_env="$work_dir/injection.env"
write_binary_env "$injection_env"
printf 'UNTRUSTED=$(touch %s)`touch %s`\n' "$marker" "$marker" >>"$injection_env"
run_preflight env-injection-data 0 \
  --mode binary --seed-state initialized --env-file "$injection_env"
[[ ! -e "$marker" ]]

seed_missing_env="$work_dir/seed-missing.env"
write_binary_env "$seed_missing_env"
run_preflight seed-empty-missing 4 \
  --mode binary --seed-state empty --env-file "$seed_missing_env"

seed_sentinel_env="$work_dir/seed-sentinel.env"
write_binary_env "$seed_sentinel_env" 'dev-only-admin-password'
run_preflight seed-empty-sentinel 4 \
  --mode binary --seed-state empty --env-file "$seed_sentinel_env"

seed_retained_env="$work_dir/seed-retained.env"
write_binary_env "$seed_retained_env" 'production-seed-password'
run_preflight seed-initialized-nonempty 4 \
  --mode binary --seed-state initialized --env-file "$seed_retained_env"

remote_db_env="$work_dir/remote-db.env"
write_binary_env "$remote_db_env"
sed 's#postgres://crm:production-password@localhost:5432/crm?sslmode=disable#postgres://crm:production-password@db.example:5432/crm?sslmode=disable#' \
  "$remote_db_env" >"$work_dir/remote-db-filtered.env"
run_preflight binary-remote-db-no-ssl 4 \
  --mode binary --seed-state initialized --env-file "$work_dir/remote-db-filtered.env"

relative_avatar_env="$work_dir/relative-avatar.env"
write_binary_env "$relative_avatar_env"
sed "s#^AVATAR_LOCAL_ROOT=.*#AVATAR_LOCAL_ROOT=relative/avatar#" \
  "$relative_avatar_env" >"$work_dir/relative-avatar-filtered.env"
run_preflight avatar-relative 4 \
  --mode binary --seed-state initialized --env-file "$work_dir/relative-avatar-filtered.env"

telegram_half_env="$work_dir/telegram-half.env"
write_binary_env "$telegram_half_env"
sed 's/^TELEGRAM_BOT_TOKEN=$/TELEGRAM_BOT_TOKEN=123456:production-token/' \
  "$telegram_half_env" >"$work_dir/telegram-half-filtered.env"
run_preflight telegram-half 4 \
  --mode binary --seed-state initialized --env-file "$work_dir/telegram-half-filtered.env"

run_preflight usage-missing-mode 2 \
  --seed-state initialized --env-file "$valid_env"

managed_env="$work_dir/managed.env"
managed_render="$work_dir/managed-render.json"
write_managed_env "$managed_env"
write_render "$managed_render" 'postgres://crm:managed-production-password@postgres:5432/crm?sslmode=disable'
python3 -m json.tool "$managed_render" >/dev/null
TEST_PATH="$fake_bin:$PATH"
FAKE_RENDER_FILE="$managed_render"
: >"$DOCKER_LOG"
run_preflight managed-valid 0 \
  --mode compose-managed-db --seed-state initialized --env-file "$managed_env" \
  --compose-file "$repo_root/docker-compose.yml" --docker-context local-fixture --project-name crm-fixture
grep -qx 'complete/preflight=ok' "$work_dir/managed-valid.out"
[[ "$(grep -c '^context inspect ' "$DOCKER_LOG")" -eq 1 ]]
if tail -n +2 "$DOCKER_LOG" | grep -v '^--host unix://' >/dev/null; then
  printf 'compose docker argv was not pinned\n' >&2
  exit 1
fi
grep -q -- 'compose --env-file ' "$DOCKER_LOG"
grep -q -- '--project-name crm-fixture config --format json' "$DOCKER_LOG"

external_env="$work_dir/external.env"
external_render="$work_dir/external-render.json"
write_external_env "$external_env"
write_render "$external_render" 'postgres://crm:external-production-password@db.example/crm?sslmode=require'
FAKE_RENDER_FILE="$external_render"
: >"$DOCKER_LOG"
run_preflight external-valid 0 \
  --mode compose-external-db --seed-state initialized --env-file "$external_env" \
  --compose-file "$repo_root/docker-compose.yml" --docker-context local-fixture --project-name crm-fixture
grep -qx 'support-boundary/backup-restore=unsupported' "$work_dir/external-valid.out"

run_preflight compose-missing-context 2 \
  --mode compose-managed-db --seed-state initialized --env-file "$managed_env" \
  --compose-file "$repo_root/docker-compose.yml" --project-name crm-fixture

TEST_DOCKER_HOST='tcp://remote.example:2376'
: >"$DOCKER_LOG"
run_preflight selector-env-conflict 2 \
  --mode compose-managed-db --seed-state initialized --env-file "$managed_env" \
  --compose-file "$repo_root/docker-compose.yml" --docker-context local-fixture --project-name crm-fixture
[[ ! -s "$DOCKER_LOG" ]]
TEST_DOCKER_HOST=""

FAKE_ENDPOINT='ssh://remote.example'
: >"$DOCKER_LOG"
run_preflight selector-remote-context 2 \
  --mode compose-managed-db --seed-state initialized --env-file "$managed_env" \
  --compose-file "$repo_root/docker-compose.yml" --docker-context remote-fixture --project-name crm-fixture
[[ "$(wc -l <"$DOCKER_LOG")" -eq 1 ]]
FAKE_ENDPOINT='unix:///var/run/docker.sock'

FAKE_VERSION='2.23.9'
: >"$DOCKER_LOG"
run_preflight compose-version-old 5 \
  --mode compose-managed-db --seed-state initialized --env-file "$managed_env" \
  --compose-file "$repo_root/docker-compose.yml" --docker-context local-fixture --project-name crm-fixture
FAKE_VERSION='2.24.0'

mismatched_render="$work_dir/mismatched-render.json"
write_render "$mismatched_render" 'postgres://crm:managed-production-password@postgres:5432/crm?sslmode=disable' \
  'different-auth-secret-that-is-long-enough'
FAKE_RENDER_FILE="$mismatched_render"
run_preflight compose-envfile-mismatch 5 \
  --mode compose-managed-db --seed-state initialized --env-file "$managed_env" \
  --compose-file "$repo_root/docker-compose.yml" --docker-context local-fixture --project-name crm-fixture

FAKE_RENDER_FILE="$external_render"
managed_external_env="$work_dir/managed-external.env"
write_managed_env "$managed_external_env"
sed 's#^APP_DATABASE_URL=$#APP_DATABASE_URL=postgres://crm:external-production-password@db.example/crm?sslmode=require#' \
  "$managed_external_env" >"$work_dir/managed-external-filtered.env"
run_preflight managed-appdb-nonempty 4 \
  --mode compose-managed-db --seed-state initialized --env-file "$work_dir/managed-external-filtered.env" \
  --compose-file "$repo_root/docker-compose.yml" --docker-context local-fixture --project-name crm-fixture

missing_bin="$work_dir/missing-bin"
mkdir -p "$missing_bin"
ln -s /bin/bash "$missing_bin/bash"
TEST_PATH="$missing_bin"
FAKE_RENDER_FILE=""
run_preflight dependency-missing 6 \
  --mode binary --seed-state initialized --env-file "$valid_env"
TEST_PATH="$PATH"

for forbidden in \
  'production-password' \
  '0123456789abcdef0123456789abcdef' \
  "$work_dir" \
  "$avatar_root"
do
  if grep -R -F -q "$forbidden" "$work_dir"/*.out "$work_dir"/*.err; then
    printf 'preflight output leaked forbidden value\n' >&2
    exit 1
  fi
done

grep -qF '${CRM_ENV_FILE:-.env}' "$repo_root/docker-compose.yml"

printf 'production preflight fixture tests: passed (%s cases)\n' "$case_count"
