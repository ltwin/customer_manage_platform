#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
work_dir="$(mktemp -d)"
trap 'rm -rf "$work_dir"' EXIT
chmod 700 "$work_dir"

run_go_case() {
  local name="$1"
  local package="$2"
  local pattern="$3"
  if ! (cd "$repo_root/backend" && go test -p=1 "$package" -run "$pattern" -count=1 -parallel=1) \
    >"$work_dir/$name.log" 2>&1; then
    printf 'auth rotation/rollback rehearsal failed: %s\n' "$name" >&2
    sed -n '1,120p' "$work_dir/$name.log" >&2
    return 1
  fi
  printf 'auth-rotation-rollback/%s=passed\n' "$name"
}

run_go_case root-key-negative ./internal/platform/auth \
  '^(TestAccessTokenVersionedClaimsAndTTL|TestReplayCipherEnvelopeAndAADContract|TestRootRotationChangesLimiterNamespace)$'
run_go_case session-revocation ./internal/platform/store \
  '^TestPasswordResetAndChangeRevokeAllRefreshFamilies$'
run_go_case limiter-schema-rollback ./internal/platform/store \
  '^(TestAttemptLimiterMigrationCreatesVersionedBudgetTable|TestAuthReadinessInspectsCurrentLimiterSchemaAndLegacyCutover)$'

if ! "$repo_root/scripts/test-auth-legacy-cutover.sh" >"$work_dir/legacy.log" 2>&1; then
  printf 'auth rotation/rollback rehearsal failed: legacy-boundary\n' >&2
  sed -n '1,120p' "$work_dir/legacy.log" >&2
  exit 1
fi
printf 'auth-rotation-rollback/legacy-boundary=passed\n'

for forbidden in \
  'postgres://' \
  'DATABASE_URL=' \
  'AUTH_TOKEN_SECRET=' \
  'Authorization' \
  'Bearer ' \
  'Set-Cookie' \
  'token=' \
  '#token' \
  '@example.invalid'
do
  if grep -R -F -q "$forbidden" "$work_dir"; then
    printf 'auth rotation/rollback rehearsal leaked forbidden material\n' >&2
    exit 1
  fi
done

printf '%s\n' 'AUTH_ROTATION_REHEARSAL {"version":1,"status":"passed","synthetic":true,"old_access_rejected":true,"old_replay_rejected":true,"old_limiter_namespace_rejected":true,"all_refresh_families_revoked":true,"dual_key_mode":false,"production_effect":false}'
printf '%s\n' 'AUTH_ROLLBACK_CATALOG {"version":1,"status":"passed","synthetic":true,"migration_up_down":true,"limiter_schema_rollback":true,"legacy_only_down":true,"new_style_down_blocked":true,"old_binary_boundary":"new_style_accounts_unavailable","production_effect":false}'
printf '%s\n' 'auth rotation/rollback rehearsal: passed'
