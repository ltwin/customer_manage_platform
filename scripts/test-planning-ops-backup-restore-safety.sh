#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
fail() {
  printf 'planning ops backup/restore safety test failed: %s\n' "$1" >&2
  exit 1
}

bash -n "$repo_root/scripts/planning-backup-compose.sh" "$repo_root/scripts/planning-restore-compose.sh"
PYTHONPATH="$repo_root/scripts/lib" python3 -m planningbackup.selftest
PYTHONPATH="$repo_root/scripts/lib" python3 -m planningbackup.preflight_selftest

set +e
state_output="$(PYTHONPATH="$repo_root/scripts/lib" python3 -m planningbackup state --failure-after replace_database 2>/dev/null)"
state_code=$?
set -e
[[ "$state_code" -eq 10 ]] || fail "state-machine-exit-$state_code"
printf '%s' "$state_output" | grep -Fq '"stage":"failed_restore_stopped"' || fail state-machine-not-stopped
printf '%s' "$state_output" | grep -Fq '"writers_stopped":true' || fail writers-not-stopped

grep -Fq 'planning-media-volume.tgz' "$repo_root/scripts/planning-backup-compose.sh" || fail backup-missing-planning-payload
grep -Fq 'planning-media-volume.tgz' "$repo_root/scripts/planning-restore-compose.sh" || fail restore-missing-planning-payload
grep -Fq 'planningbackup preflight' "$repo_root/scripts/planning-restore-compose.sh" || fail restore-missing-preflight
grep -Fq 'planningbackup snapshot-v2' "$repo_root/scripts/planning-restore-compose.sh" || fail restore-missing-immutable-snapshot
grep -Fq 'failure-stop' "$repo_root/scripts/planning-restore-compose.sh" || fail restore-missing-failure-stop
grep -Fq -- '--target-generation' "$repo_root/scripts/planning-restore-compose.sh" || fail restore-missing-explicit-generation
grep -Fq -- '--target-unsafe-entries' "$repo_root/scripts/planning-restore-compose.sh" || fail restore-missing-unsafe-inventory
grep -Fq -- '--target-total-entries' "$repo_root/scripts/planning-restore-compose.sh" || fail restore-missing-total-inventory
grep -Fq -- '--target-compose-sha256' "$repo_root/scripts/planning-restore-compose.sh" || fail restore-missing-compose-compatibility
grep -Fq -- '--target-app-digest' "$repo_root/scripts/planning-restore-compose.sh" || fail restore-missing-app-compatibility
grep -Fq -- '--target-postgres-digest' "$repo_root/scripts/planning-restore-compose.sh" || fail restore-missing-postgres-compatibility
grep -Fq -- '--target-postgres-major' "$repo_root/scripts/planning-restore-compose.sh" || fail restore-missing-postgres-major
grep -Fq -- '--target-database-counts' "$repo_root/scripts/planning-restore-compose.sh" || fail restore-missing-target-database-counts
grep -Fq -- '--target-avatar-manifest' "$repo_root/scripts/planning-restore-compose.sh" || fail restore-missing-target-avatar-manifest
grep -Fq 'destination=/var/lib/crm/avatars,readonly' "$repo_root/scripts/planning-restore-compose.sh" || fail restore-target-avatar-helper-not-readonly
grep -Fq 'PGOPTIONS=-c default_transaction_read_only=on' "$repo_root/scripts/planning-restore-compose.sh" || fail restore-target-inventory-database-not-readonly
grep -Fq -- '--entrypoint /usr/local/bin/migrate app' "$repo_root/scripts/planning-restore-compose.sh" || fail restore-migration-entrypoint-not-pinned
grep -Fq 'V1_INHERITED_LOCK_ID="$V1_LOCK_ID"' "$repo_root/scripts/planning-backup-compose.sh" || fail backup-missing-inherited-lock
grep -Fq 'V1_INHERITED_LOCK_ID="$V1_LOCK_ID"' "$repo_root/scripts/planning-restore-compose.sh" || fail restore-missing-inherited-lock
grep -Fq 'V1_INHERITED_OWNER_NONCE="$V1_OWNER_NONCE"' "$repo_root/scripts/planning-backup-compose.sh" || fail backup-missing-inherited-nonce
grep -Fq 'V1_INHERITED_OWNER_NONCE="$V1_OWNER_NONCE"' "$repo_root/scripts/planning-restore-compose.sh" || fail restore-missing-inherited-nonce
! grep -Fq 'V1_TARGET_HASH=' "$repo_root/scripts/planning-backup-compose.sh" || fail backup-split-lock-namespace
! grep -Fq 'V1_TARGET_HASH=' "$repo_root/scripts/planning-restore-compose.sh" || fail restore-split-lock-namespace
! grep -Fq -- '-printf' "$repo_root/scripts/planning-restore-compose.sh" || fail restore-uses-non-busybox-find
! grep -Fq -- '--evidence' "$repo_root/scripts/planning-restore-compose.sh" || fail restore-mints-rehearsal-evidence
! grep -Fq 'planning-restore-rehearsal-v1' "$repo_root/scripts/planning-restore-compose.sh" || fail restore-mints-passed-rehearsal
! grep -Eq -- '--skip-manifest|--force-space|--ignore-orphan' "$repo_root/scripts/planning-restore-compose.sh" || fail unsafe-preflight-bypass

python3 - "$repo_root/scripts/planning-backup-compose.sh" "$repo_root/scripts/planning-restore-compose.sh" <<'PY' || fail wrapper-stage-order
import sys
from pathlib import Path

for raw in sys.argv[1:]:
    text = Path(raw).read_text(encoding="utf-8")
    required = ("v1_render_and_image_safety", "v1_acquire_lock", "v1_acquire_fence", "v1_resolve_runtime", 'V2_PLANNING_VOLUME="$(v1_one_id')
    positions = [text.index(token) for token in required]
    if positions != sorted(positions):
        raise SystemExit(1)

restore = Path(sys.argv[2]).read_text(encoding="utf-8")
if restore.index("planningbackup snapshot-v2") > restore.index("planningbackup preflight"):
    raise SystemExit(1)
if "restore-v1-planning-fence" not in restore or "-mindepth 1 -print -quit" not in restore:
    raise SystemExit(1)
if '--replace-only' not in restore:
    raise SystemExit(1)
preflight = restore.index('planningbackup preflight')
for token in ('target-database-counts.json', 'target-avatar-manifest.json'):
    if restore.index(token) > preflight:
        raise SystemExit(1)
ordered_final = (
    'V1_STAGE=restore-v1-base',
    'V1_STAGE=restore-planning-media-replace',
    'V1_STAGE=restore-migrate',
    'V1_STAGE=restore-validate-database',
    'V1_STAGE=restore-validate-avatar',
    'V1_STAGE=restore-validate-planning-media',
    'V1_STAGE=restore-reopen',
    "printf 'stage/complete=pass",
)
positions = [restore.index(token) for token in ordered_final]
if positions != sorted(positions):
    raise SystemExit(1)
if restore.index('verify-db-counts', positions[2]) > positions[-2]:
    raise SystemExit(1)
PY

printf 'planning ops backup/restore safety test: passed\n'
