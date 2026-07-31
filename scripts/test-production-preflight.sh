#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
preflight="$repo_root/scripts/production-preflight.sh"
evidence_helper="$repo_root/scripts/lib/auth-preflight-evidence.py"
work_dir="$(mktemp -d)"
trap 'rm -rf "$work_dir"' EXIT
case_count=0
TEST_PATH="$PATH"
TEST_DOCKER_HOST=""
FAKE_ENDPOINT="unix:///var/run/docker.sock"
FAKE_VERSION="2.24.0"
FAKE_RENDER_FILE=""
DOCKER_LOG="$work_dir/docker.log"
BUILD_REVISION="aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
FIXED_NOW="2030-01-08T12:00:00Z"

avatar_root="$work_dir/avatars"
mkdir -p "$avatar_root"

write_binary_env() {
  local destination="$1"
  local seed_password="${2:-}"
  {
    printf '%s\n' 'DATABASE_URL=postgres://crm:production-password@localhost:5432/crm?sslmode=disable'
	printf '%s\n' 'AUTH_TOKEN_SECRET=0123456789abcdef0123456789abcdef'
	printf '%s\n' 'AUTH_TOKEN_SECRET_VERSION=synthetic-v1'
	printf '%s\n' 'AUTH_TOKEN_ISSUER=photographer-crm'
	printf '%s\n' 'PUBLIC_BASE_URL=https://app.example.invalid'
	printf '%s\n' 'AUTH_PUBLIC_REGISTRATION_ENABLED=false'
	printf '%s\n' 'AUTH_MAIL_DRIVER=resend'
	printf '%s\n' 'RESEND_API_KEY=synthetic-production-mail-key'
	printf '%s\n' 'AUTH_MAIL_FROM=CRM <auth@example.invalid>'
	printf '%s\n' 'TRUSTED_PROXY_CIDRS=192.0.2.0/24,2001:db8::/32'
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
    FAKE_VERSION="$FAKE_VERSION" FAKE_RENDER_FILE="$FAKE_RENDER_FILE" \
    FAKE_READINESS_NOW="$FIXED_NOW" DOCKER_LOG="$DOCKER_LOG" \
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
	printf '%s\n' 'AUTH_TOKEN_SECRET_VERSION=synthetic-v1'
	printf '%s\n' 'AUTH_TOKEN_ISSUER=photographer-crm'
	printf '%s\n' 'PUBLIC_BASE_URL=https://app.example.invalid'
	printf '%s\n' 'AUTH_PUBLIC_REGISTRATION_ENABLED=false'
	printf '%s\n' 'AUTH_MAIL_DRIVER=resend'
	printf '%s\n' 'RESEND_API_KEY=synthetic-production-mail-key'
	printf '%s\n' 'AUTH_MAIL_FROM=CRM <auth@example.invalid>'
	printf '%s\n' 'TRUSTED_PROXY_CIDRS=192.0.2.0/24,2001:db8::/32'
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
	printf '%s\n' 'AUTH_TOKEN_SECRET_VERSION=synthetic-v1'
	printf '%s\n' 'AUTH_TOKEN_ISSUER=photographer-crm'
	printf '%s\n' 'PUBLIC_BASE_URL=https://app.example.invalid'
	printf '%s\n' 'AUTH_PUBLIC_REGISTRATION_ENABLED=false'
	printf '%s\n' 'AUTH_MAIL_DRIVER=resend'
	printf '%s\n' 'RESEND_API_KEY=synthetic-production-mail-key'
	printf '%s\n' 'AUTH_MAIL_FROM=CRM <auth@example.invalid>'
	printf '%s\n' 'TRUSTED_PROXY_CIDRS=192.0.2.0/24,2001:db8::/32'
    printf '%s\n' 'SEED_ADMIN_PASSWORD='
    printf '%s\n' 'TELEGRAM_BOT_TOKEN='
    printf '%s\n' 'TELEGRAM_BOT_USERNAME='
  } >"$destination"
}

write_evidence_set() {
  local destination="$1"
  local fingerprint="$2"
  local generated_at="${3:-$FIXED_NOW}"
  mkdir -p "$destination"
  for artifact in monitor security rollback rotation; do
    printf '{"version":1,"generated_at":"%s","build_revision":"%s","schema_version":"13","config_fingerprint":"%s","environment":"production","status":"passed","evidence_path":"%s.json"}\n' \
      "$generated_at" "$BUILD_REVISION" "$fingerprint" "$artifact" >"$destination/$artifact.json"
  done
  printf '{"version":1,"generated_at":"%s","build_revision":"%s","schema_version":"13","config_fingerprint":"%s","environment":"production","status":"passed","evidence_path":"mail-accepted.json","provider":"resend","provider_message_id":"receipt_synthetic_123","secret_version_ref":"synthetic-v1","recipient_ref":"email:353587cfa45e"}\n' \
    "$generated_at" "$BUILD_REVISION" "$fingerprint" >"$destination/mail-accepted.json"
}

run_ready_preflight() {
  local name="$1"
  local expected_exit="$2"
  local selected_evidence="$3"
  local selected_accountctl="${4:-$fake_accountctl}"
  run_preflight "$name" "$expected_exit" \
    --mode binary --seed-state initialized --env-file "$registration_enabled_env" \
    --evidence-dir "$selected_evidence" --accountctl-bin "$selected_accountctl" \
    --build-revision "$BUILD_REVISION" --now "$FIXED_NOW"
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
printf '%s\n' 'if [[ "$1" == "compose" && " $* " == *" run "* ]]; then build=""; fingerprint=""; while [[ $# -gt 0 ]]; do case "$1" in --build-revision) build="$2"; shift 2;; --config-fingerprint) fingerprint="$2"; shift 2;; *) shift;; esac; done; printf '\''{"version":1,"generated_at":"%s","build_revision":"%s","schema_version":"13","config_fingerprint":"%s","environment":"production","status":"passed","evidence_path":"accountctl://auth/readiness","checks":{"database":"passed","limiter_schema":"passed","legacy_cutover":"passed"}}\n'\'' "$FAKE_READINESS_NOW" "$build" "$fingerprint"; exit 0; fi' >>"$fake_bin/docker"
printf '%s\n' 'if [[ "$1" == "compose" ]]; then /bin/cat "$FAKE_RENDER_FILE"; exit 0; fi' >>"$fake_bin/docker"
printf '%s\n' 'exit 92' >>"$fake_bin/docker"
chmod +x "$fake_bin/docker"

fake_accountctl="$work_dir/accountctl"
{
  printf '%s\n' '#!/usr/bin/env bash'
  printf '%s\n' 'set -euo pipefail'
  printf '%s\n' '[[ "$1" == "auth" && "$2" == "readiness" ]] || exit 2'
  printf '%s\n' 'shift 2'
  printf '%s\n' 'build_revision=""; config_fingerprint=""'
  printf '%s\n' 'while [[ $# -gt 0 ]]; do case "$1" in --build-revision) build_revision="$2"; shift 2;; --config-fingerprint) config_fingerprint="$2"; shift 2;; *) exit 2;; esac; done'
  printf '%s\n' "printf '{\"version\":1,\"generated_at\":\"$FIXED_NOW\",\"build_revision\":\"%s\",\"schema_version\":\"13\",\"config_fingerprint\":\"%s\",\"environment\":\"production\",\"status\":\"passed\",\"evidence_path\":\"accountctl://auth/readiness\",\"checks\":{\"database\":\"passed\",\"limiter_schema\":\"passed\",\"legacy_cutover\":\"passed\"}}\\n' \"\$build_revision\" \"\$config_fingerprint\""
} >"$fake_accountctl"
chmod +x "$fake_accountctl"

valid_env="$work_dir/binary.env"
write_binary_env "$valid_env"
run_preflight binary-valid 0 \
  --mode binary --seed-state initialized --env-file "$valid_env"
grep -qx 'readiness/registration=secure-baseline-ready' "$work_dir/binary-valid.out"
grep -qx 'complete/preflight=secure-baseline-ready' "$work_dir/binary-valid.out"

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
run_preflight seed-empty-missing 0 \
  --mode binary --seed-state empty --env-file "$seed_missing_env"

seed_sentinel_env="$work_dir/seed-sentinel.env"
write_binary_env "$seed_sentinel_env" 'dev-only-admin-password'
run_preflight seed-empty-sentinel 4 \
  --mode binary --seed-state empty --env-file "$seed_sentinel_env"

seed_retained_env="$work_dir/seed-retained.env"
write_binary_env "$seed_retained_env" 'production-seed-password'
run_preflight seed-initialized-nonempty 4 \
  --mode binary --seed-state initialized --env-file "$seed_retained_env"

registration_enabled_env="$work_dir/registration-enabled.env"
sed 's/^AUTH_PUBLIC_REGISTRATION_ENABLED=false$/AUTH_PUBLIC_REGISTRATION_ENABLED=true/' \
  "$valid_env" >"$registration_enabled_env"
run_preflight registration-enabled-without-evidence 4 \
  --mode binary --seed-state initialized --env-file "$registration_enabled_env"
grep -q 'key=AUTH_READINESS_EVIDENCE remediation=provide-complete-current-evidence' \
  "$work_dir/registration-enabled-without-evidence.err"

config_fingerprint="$(python3 "$evidence_helper" fingerprint \
  --mail-driver resend \
  --mail-provider resend \
  --mail-from 'CRM <auth@example.invalid>' \
  --issuer photographer-crm \
  --public-base-url https://app.example.invalid \
  --proxy-cidrs '192.0.2.0/24,2001:db8::/32' \
  --cookie-profile '__Host-crm_refresh|HttpOnly|Secure|SameSite=Strict|Path=/|no-domain' \
  --limiter-kdf-version 'crm-auth/v1/limiter-hmac@v1' \
  --secret-version-ref synthetic-v1)"
evidence_dir="$work_dir/readiness-evidence"
write_evidence_set "$evidence_dir" "$config_fingerprint"
run_preflight registration-enabled-ready 0 \
  --mode binary --seed-state initialized --env-file "$registration_enabled_env" \
  --evidence-dir "$evidence_dir" --accountctl-bin "$fake_accountctl" \
  --build-revision "$BUILD_REVISION" --now "$FIXED_NOW"
grep -qx 'readiness/registration=enable-ready' "$work_dir/registration-enabled-ready.out"
grep -qx 'complete/preflight=enable-ready' "$work_dir/registration-enabled-ready.out"

for artifact in mail-accepted monitor security rollback rotation; do
  missing_dir="$work_dir/missing-$artifact"
  cp -R "$evidence_dir" "$missing_dir"
  rm "$missing_dir/$artifact.json"
  run_ready_preflight "evidence-missing-$artifact" 4 "$missing_dir"
  grep -q "evidence_error artifact=${artifact/mail-accepted/mail} class=missing" \
    "$work_dir/evidence-missing-$artifact.err"
done

failed_status_dir="$work_dir/evidence-failed-status"
cp -R "$evidence_dir" "$failed_status_dir"
sed 's/"status":"passed"/"status":"failed"/' \
  "$failed_status_dir/monitor.json" >"$failed_status_dir/monitor.changed"
mv "$failed_status_dir/monitor.changed" "$failed_status_dir/monitor.json"
run_ready_preflight evidence-failed-status 4 "$failed_status_dir"
grep -q 'evidence_error artifact=monitor class=status' "$work_dir/evidence-failed-status.err"

for mismatch in revision schema fingerprint; do
  mismatch_dir="$work_dir/evidence-mismatch-$mismatch"
  cp -R "$evidence_dir" "$mismatch_dir"
  case "$mismatch" in
    revision)
      sed 's/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/cccccccccccccccccccccccccccccccccccccccc/' \
        "$mismatch_dir/security.json" >"$mismatch_dir/security.changed"
      ;;
    schema)
      sed 's/"schema_version":"13"/"schema_version":"12"/' \
        "$mismatch_dir/security.json" >"$mismatch_dir/security.changed"
      ;;
    fingerprint)
      sed "s/$config_fingerprint/dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd/" \
        "$mismatch_dir/security.json" >"$mismatch_dir/security.changed"
      ;;
  esac
  mv "$mismatch_dir/security.changed" "$mismatch_dir/security.json"
  run_ready_preflight "evidence-mismatch-$mismatch" 4 "$mismatch_dir"
done

mail_boundary_dir="$work_dir/evidence-mail-boundary"
cp -R "$evidence_dir" "$mail_boundary_dir"
sed 's/2030-01-08T12:00:00Z/2030-01-07T12:00:00Z/' \
  "$mail_boundary_dir/mail-accepted.json" >"$mail_boundary_dir/mail.changed"
mv "$mail_boundary_dir/mail.changed" "$mail_boundary_dir/mail-accepted.json"
run_ready_preflight evidence-mail-boundary 0 "$mail_boundary_dir"

mail_stale_dir="$work_dir/evidence-mail-stale"
cp -R "$evidence_dir" "$mail_stale_dir"
sed 's/2030-01-08T12:00:00Z/2030-01-07T11:59:59Z/' \
  "$mail_stale_dir/mail-accepted.json" >"$mail_stale_dir/mail.changed"
mv "$mail_stale_dir/mail.changed" "$mail_stale_dir/mail-accepted.json"
run_ready_preflight evidence-mail-stale 4 "$mail_stale_dir"
grep -q 'evidence_error artifact=mail class=stale' "$work_dir/evidence-mail-stale.err"

rotation_boundary_dir="$work_dir/evidence-rotation-boundary"
cp -R "$evidence_dir" "$rotation_boundary_dir"
sed 's/2030-01-08T12:00:00Z/2030-01-01T12:00:00Z/' \
  "$rotation_boundary_dir/rotation.json" >"$rotation_boundary_dir/rotation.changed"
mv "$rotation_boundary_dir/rotation.changed" "$rotation_boundary_dir/rotation.json"
run_ready_preflight evidence-rotation-boundary 0 "$rotation_boundary_dir"

rollback_stale_dir="$work_dir/evidence-rollback-stale"
cp -R "$evidence_dir" "$rollback_stale_dir"
sed 's/2030-01-08T12:00:00Z/2030-01-01T11:59:59Z/' \
  "$rollback_stale_dir/rollback.json" >"$rollback_stale_dir/rollback.changed"
mv "$rollback_stale_dir/rollback.changed" "$rollback_stale_dir/rollback.json"
run_ready_preflight evidence-rollback-stale 4 "$rollback_stale_dir"
grep -q 'evidence_error artifact=rollback class=stale' "$work_dir/evidence-rollback-stale.err"

future_boundary_dir="$work_dir/evidence-future-boundary"
cp -R "$evidence_dir" "$future_boundary_dir"
sed 's/2030-01-08T12:00:00Z/2030-01-08T12:05:00Z/' \
  "$future_boundary_dir/security.json" >"$future_boundary_dir/security.changed"
mv "$future_boundary_dir/security.changed" "$future_boundary_dir/security.json"
run_ready_preflight evidence-future-boundary 0 "$future_boundary_dir"

future_invalid_dir="$work_dir/evidence-future-invalid"
cp -R "$evidence_dir" "$future_invalid_dir"
sed 's/2030-01-08T12:00:00Z/2030-01-08T12:05:01Z/' \
  "$future_invalid_dir/security.json" >"$future_invalid_dir/security.changed"
mv "$future_invalid_dir/security.changed" "$future_invalid_dir/security.json"
run_ready_preflight evidence-future-invalid 4 "$future_invalid_dir"
grep -q 'evidence_error artifact=security class=future' "$work_dir/evidence-future-invalid.err"

unknown_field_dir="$work_dir/evidence-unknown-field"
cp -R "$evidence_dir" "$unknown_field_dir"
sed 's/}$/,"unexpected":"forbidden"}/' \
  "$unknown_field_dir/security.json" >"$unknown_field_dir/security.changed"
mv "$unknown_field_dir/security.changed" "$unknown_field_dir/security.json"
run_ready_preflight evidence-unknown-field 4 "$unknown_field_dir"
grep -q 'evidence_error artifact=security class=fields' "$work_dir/evidence-unknown-field.err"

raw_recipient_dir="$work_dir/evidence-raw-recipient"
cp -R "$evidence_dir" "$raw_recipient_dir"
sed 's/email:353587cfa45e/owner@example.invalid/' \
  "$raw_recipient_dir/mail-accepted.json" >"$raw_recipient_dir/mail.changed"
mv "$raw_recipient_dir/mail.changed" "$raw_recipient_dir/mail-accepted.json"
run_ready_preflight evidence-raw-recipient 4 "$raw_recipient_dir"
grep -q 'evidence_error artifact=mail class=recipient_ref' "$work_dir/evidence-raw-recipient.err"

missing_secret_version_env="$work_dir/registration-missing-secret-version.env"
sed '/^AUTH_TOKEN_SECRET_VERSION=/d' "$registration_enabled_env" >"$missing_secret_version_env"
registration_enabled_env_saved="$registration_enabled_env"
registration_enabled_env="$missing_secret_version_env"
run_ready_preflight registration-missing-secret-version 4 "$evidence_dir"
grep -q 'key=AUTH_READINESS_EVIDENCE remediation=provide-complete-current-evidence' \
  "$work_dir/registration-missing-secret-version.err"
registration_enabled_env="$registration_enabled_env_saved"

failed_accountctl="$work_dir/accountctl-failed"
sed 's/"limiter_schema":"passed"/"limiter_schema":"failed"/' \
  "$fake_accountctl" >"$failed_accountctl"
chmod +x "$failed_accountctl"
run_ready_preflight live-limiter-failed 4 "$evidence_dir" "$failed_accountctl"
grep -q 'evidence_error artifact=live class=checks' "$work_dir/live-limiter-failed.err"

http_public_base_env="$work_dir/http-public-base.env"
sed 's#^PUBLIC_BASE_URL=https://app.example.invalid$#PUBLIC_BASE_URL=http://app.example.invalid#' \
  "$valid_env" >"$http_public_base_env"
run_preflight public-base-http 4 \
  --mode binary --seed-state initialized --env-file "$http_public_base_env"

default_port_public_base_env="$work_dir/default-port-public-base.env"
sed 's#^PUBLIC_BASE_URL=https://app.example.invalid$#PUBLIC_BASE_URL=https://app.example.invalid:443#' \
  "$valid_env" >"$default_port_public_base_env"
run_preflight public-base-explicit-default-port 4 \
  --mode binary --seed-state initialized --env-file "$default_port_public_base_env"

nondefault_port_public_base_env="$work_dir/nondefault-port-public-base.env"
sed 's#^PUBLIC_BASE_URL=https://app.example.invalid$#PUBLIC_BASE_URL=https://app.example.invalid:8443#' \
  "$valid_env" >"$nondefault_port_public_base_env"
run_preflight public-base-nondefault-port 0 \
  --mode binary --seed-state initialized --env-file "$nondefault_port_public_base_env"

ipv6_public_base_env="$work_dir/ipv6-public-base.env"
sed 's#^PUBLIC_BASE_URL=https://app.example.invalid$#PUBLIC_BASE_URL=https://[2001:db8::1]:8443#' \
  "$valid_env" >"$ipv6_public_base_env"
run_preflight public-base-ipv6-nondefault-port 0 \
  --mode binary --seed-state initialized --env-file "$ipv6_public_base_env"

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
grep -qx 'complete/preflight=secure-baseline-ready' "$work_dir/managed-valid.out"
[[ "$(grep -c '^context inspect ' "$DOCKER_LOG")" -eq 1 ]]
if tail -n +2 "$DOCKER_LOG" | grep -v '^--host unix://' >/dev/null; then
  printf 'compose docker argv was not pinned\n' >&2
  exit 1
fi
grep -q -- 'compose --env-file ' "$DOCKER_LOG"
grep -q -- '--project-name crm-fixture config --format json' "$DOCKER_LOG"

managed_enabled_env="$work_dir/managed-enabled.env"
sed 's/^AUTH_PUBLIC_REGISTRATION_ENABLED=false$/AUTH_PUBLIC_REGISTRATION_ENABLED=true/' \
  "$managed_env" >"$managed_enabled_env"
run_preflight managed-registration-enabled-ready 0 \
  --mode compose-managed-db --seed-state initialized --env-file "$managed_enabled_env" \
  --compose-file "$repo_root/docker-compose.yml" --docker-context local-fixture --project-name crm-fixture \
  --evidence-dir "$evidence_dir" --build-revision "$BUILD_REVISION" --now "$FIXED_NOW"
grep -qx 'complete/preflight=enable-ready' "$work_dir/managed-registration-enabled-ready.out"
grep -q -- 'run --rm --no-deps --entrypoint /usr/local/bin/accountctl app auth readiness' "$DOCKER_LOG"

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
  'synthetic-production-mail-key' \
  'owner@example.invalid' \
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
