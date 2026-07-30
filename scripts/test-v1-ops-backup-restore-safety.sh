#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
backup="$repo_root/scripts/backup-compose.sh"
restore="$repo_root/scripts/restore-compose.sh"

fail() {
  printf 'v1 ops backup/restore safety test failed: %s\n' "$1" >&2
  exit 1
}

bash -n "$backup" "$restore"
python3 "$repo_root/scripts/lib/v1-ops-package-selftest.py"

! grep -Eq '^trap .*EXIT INT TERM' "$backup" || fail backup-shared-signal-trap
! grep -Eq '^trap .*EXIT INT TERM' "$restore" || fail restore-shared-signal-trap
grep -Fq "trap 'backup_signal INT' INT" "$backup" || fail backup-int-handler
grep -Fq "trap 'backup_signal TERM' TERM" "$backup" || fail backup-term-handler
grep -Fq "trap 'restore_signal INT' INT" "$restore" || fail restore-int-handler
grep -Fq "trap 'restore_signal TERM' TERM" "$restore" || fail restore-term-handler

for signal_case in INT:130 TERM:143; do
  signal="${signal_case%%:*}"
  expected="${signal_case##*:}"
  set +e
  (
    V1_SIGNAL_EXIT=0
    V1_BACKUP_ORIGINAL_RUNNING=0
    V1_APP_ID=
    v1_cleanup() { return 0; }
    # Source only the production exit/signal handlers and their trap installation.
    eval "$(sed -n '/^backup_exit()/,/^trap.*backup_signal TERM/p' "$backup")"
    backup_signal "$signal"
    exit 99
  )
  backup_signal_code=$?
  set -e
  [[ "$backup_signal_code" -eq "$expected" ]] || fail "backup-$signal-exit-$backup_signal_code"

  set +e
  (
    V1_SIGNAL_EXIT=0
    V1_RESTORE_DESTRUCTIVE=0
    V1_APP_ID=
    V1_CLEANUP_FAILED=0
    v1_cleanup() { return 0; }
    eval "$(sed -n '/^restore_exit()/,/^trap.*restore_signal TERM/p' "$restore")"
    restore_signal "$signal"
    exit 99
  )
  restore_pre_mutation_code=$?
  set -e
  [[ "$restore_pre_mutation_code" -eq "$expected" ]] || \
    fail "restore-pre-mutation-$signal-exit-$restore_pre_mutation_code"

  marker="$(mktemp)"
  rm -f "$marker"
  export marker
  set +e
  (
    V1_SIGNAL_EXIT=0
    V1_RESTORE_DESTRUCTIVE=1
    V1_APP_ID=synthetic-app
    V1_CLEANUP_FAILED=0
    v1_failure_stop() { printf called >"$marker"; }
    v1_cleanup() { return 0; }
    eval "$(sed -n '/^restore_exit()/,/^trap.*restore_signal TERM/p' "$restore")"
    restore_signal "$signal"
    exit 99
  )
  restore_signal_code=$?
  set -e
  [[ "$restore_signal_code" -eq 10 ]] || fail "restore-$signal-exit-$restore_signal_code"
  [[ "$(<"$marker")" == called ]] || fail "restore-$signal-missed-failure-stop"
  rm -f "$marker"
done

for operation in backup restore; do
  handler_file="$backup"
  handler_name=backup_exit
  signal_name=backup_signal
  if [[ "$operation" == restore ]]; then
    handler_file="$restore"
    handler_name=restore_exit
    signal_name=restore_signal
  fi
  cleanup_error="$repo_root/.v1-ops-$operation-cleanup-test.err"
  set +e
  (
    V1_SIGNAL_EXIT=0
    V1_BACKUP_ORIGINAL_RUNNING=0
    V1_RESTORE_DESTRUCTIVE=0
    V1_APP_ID=
    V1_CLEANUP_FAILED=0
    v1_cleanup() { set -e; return 1; }
    eval "$(sed -n "/^${handler_name}()/,/^trap.*${signal_name} TERM/p" "$handler_file")"
    false
  ) 2>"$cleanup_error"
  cleanup_code=$?
  set -e
  [[ "$cleanup_code" -eq 11 ]] || fail "$operation-cleanup-exit-$cleanup_code"
  grep -qx 'error code=11 stage=cleanup key=residual action=inspect-immutable-ids' "$cleanup_error" || \
    fail "$operation-cleanup-missing-stable-error"
  rm -f "$cleanup_error"
done

grep -Fq 'db-counts-sql' "$backup" || fail backup-database-count-oracle
grep -Fq 'verify-db-counts' "$restore" || fail restore-database-count-verify
backup_pg_line="$(grep -n 'V1_STAGE=backup-pg-dump' "$backup" | cut -d: -f1)"
backup_tar_line="$(grep -n 'V1_STAGE=backup-avatar-tar' "$backup" | cut -d: -f1)"
backup_manifest_line="$(grep -n 'V1_STAGE=backup-manifest' "$backup" | head -n1 | cut -d: -f1)"
[[ "$backup_pg_line" -lt "$backup_tar_line" && "$backup_tar_line" -lt "$backup_manifest_line" ]] || \
  fail backup-freeze-stage-order
grep -Fq 'v1_start_app' "$restore" || fail restore-start-not-distinguished
grep -Fq 'v1_wait_app_health' "$restore" || fail restore-health-not-distinguished
grep -Fq '"$V1_OPS_HELPER" publish' "$backup" || fail backup-no-replace-publish
! grep -Eq '^[[:space:]]*mv[[:space:]]+"\$V1_TEMP_PACKAGE"' "$backup" || fail backup-plain-mv-publish

for command_key in pg-dump psql docker-cp publish; do
  grep -Fq "$command_key.stderr" "$backup" "$restore" || fail "missing-private-stderr-$command_key"
done

printf 'v1 ops backup/restore safety test: passed\n'
