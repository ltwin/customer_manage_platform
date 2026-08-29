#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
work_dir="$(mktemp -d)"
trap 'rm -rf "$work_dir"' EXIT
chmod 700 "$work_dir"

run_go_test() {
  local name="$1"
  shift
  if ! (cd "$repo_root/backend" && go test "$@") >"$work_dir/$name.log" 2>&1; then
    printf 'auth legacy cutover check failed: %s\n' "$name" >&2
    sed -n '1,120p' "$work_dir/$name.log" >&2
    return 1
  fi
  printf 'auth-legacy-check/%s=passed\n' "$name"
}

run_go_test accountctl ./cmd/accountctl -count=1
run_go_test legacy-access-jwt ./internal/platform/auth \
  -run '^TestAccessTokenRejectsLegacyAndMissingRequiredClaims$' -count=1
run_go_test legacy-password-only-shape ./internal/platform/httpapi \
  -run '^TestLegacyPasswordOnlyShapeRejected$' -count=1
run_go_test seed-startup-retired ./cmd/server \
  -run '^TestServerStartupDoesNotReferenceLegacySeed$' -count=1
run_go_test account-auth-admission ./internal/platform/store \
  -run '^TestAccountAuthApplicationPostgres$' -count=1 -parallel=1
run_go_test rollback-harness ./internal/platform/store \
  -run '^TestAuthLegacyCutoverHarness$' -count=1 -parallel=1 -v

report_file="$work_dir/reports.log"
sed -n 's/^.*AUTH_LEGACY_HARNESS /AUTH_LEGACY_HARNESS /p' \
  "$work_dir/rollback-harness.log" >"$report_file"

# 泄露扫描先于字段断言：断言失败时要把报告转到 stderr 才能定位，所以报告必须已经
# 确认是干净的，否则诊断本身就成了绕过这道闸的口子。
for forbidden in \
  'postgres://' \
  'Authorization' \
  'Bearer ' \
  'Set-Cookie' \
  'token=' \
  '#token' \
  'deprecated-legacy-hash' \
  'synthetic-avatar-object' \
  '@example.invalid'
do
  if grep -F -q "$forbidden" "$report_file"; then
    printf 'auth legacy report leaked forbidden material\n' >&2
    exit 1
  fi
done

report_lines="$(grep -c '^AUTH_LEGACY_HARNESS ' "$report_file")"
if [[ "$report_lines" -ne 2 ]]; then
  printf 'auth legacy harness emitted %s report lines, want 2 (cutover + rollback)\n' \
    "$report_lines" >&2
  cat "$report_file" >&2
  exit 1
fi

expect_report() {
  if ! grep -F -q "$1" "$report_file"; then
    printf 'auth legacy report missing %s\n' "$1" >&2
    cat "$report_file" >&2
    return 1
  fi
}

expect_report '"report":"auth_legacy_cutover"'
expect_report '"account_id_preserved":true'
expect_report '"business_counts_preserved":true'
expect_report '"avatar_checksum_preserved":true'
expect_report '"legacy_unclaimed_count":0'
expect_report '"pending_claim_count":0'
expect_report '"report":"auth_legacy_rollback"'
expect_report '"legacy_down_passed":true'
expect_report '"limiter_schema_rollback":true'
expect_report '"new_style_down_blocked":true'
expect_report '"new_style_account_preserved":true'

cat "$report_file"
printf '%s\n' 'AUTH_LEGACY_CUTOVER_CHECK {"report":"auth_legacy_cutover_checks","ready":true,"checks":["accountctl_contract","legacy_password_only_shape_rejected","legacy_access_jwt_rejected","seed_startup_path_retired","account_admission_concurrency","repo_pinned_rollback"]}'
printf '%s\n' 'auth legacy cutover fixture tests: passed'
