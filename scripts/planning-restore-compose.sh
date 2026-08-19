#!/usr/bin/env bash
set -euo pipefail
umask 077

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# shellcheck source=scripts/lib/v1-ops-common.sh
source "$repo_root/scripts/lib/v1-ops-common.sh"

V2_ENV_FILE=
V2_COMPOSE_FILE=
V2_DOCKER_CONTEXT=
V2_PROJECT_NAME=
V2_INPUT=
V2_IDENTITY_FILE=
V2_CONFIRM_PROJECT=
V2_TARGET_GENERATION=
V2_INPUT_SET=0
V2_ORIGINAL_RUNNING=0
V2_APP_STOPPED=0
V2_APP_REOPENED=0
V2_PRIVATE_DIR=
V2_PLANNING_VOLUME=
V2_BREAK_STALE_LOCK=0

usage() {
  v1_die 2 usage arguments use-required-v2-restore-flags
}

v2_create_planning_helper() {
  local mode="$1" purpose="$2" entrypoint="$3" name created id mount
  shift 3
  mount="type=volume,source=$V2_PLANNING_VOLUME,destination=/var/lib/crm/planning-media"
  [[ "$mode" == ro ]] && mount+=",readonly"
  name="photographer-private-crm-planning-v2-${purpose}-${V1_TARGET_HASH:0:16}-${#V1_OPERATION_HELPERS[@]}"
  created="$(v1_docker create --name "$name" --network none --user 0:0 \
    --mount "$mount" \
    --label com.photographer-crm.ops=true \
    --label "com.photographer-crm.ops.target-sha256=$V1_TARGET_HASH" \
    --label com.photographer-crm.ops.kind=planning-v2-helper \
    --label "com.photographer-crm.ops.lock-id-sha256=$V1_LOCK_ID_HASH" \
    --label "com.photographer-crm.ops.owner-nonce-sha256=$V1_OWNER_NONCE_HASH" \
    --entrypoint "$entrypoint" "$V1_APP_IMAGE_ID" "$@" 2>/dev/null)" || return 1
  V1_OPERATION_HELPERS+=("$created")
  id="$(v1_docker container inspect "$created" --format '{{.Id}}' 2>/dev/null)" || return 1
  [[ "$id" == "$created" ]] || return 1
  V1_LAST_HELPER_ID="$created"
}

v2_create_avatar_helper() {
  local purpose="$1" entrypoint="$2" name created id
  shift 2
  name="photographer-private-crm-planning-v2-avatar-${purpose}-${V1_TARGET_HASH:0:16}-${#V1_OPERATION_HELPERS[@]}"
  created="$(v1_docker create --name "$name" --network "$V1_NETWORK" --user 0:0 \
    --env-file "$V1_RUNTIME_ENV" \
    --env 'PGOPTIONS=-c default_transaction_read_only=on' \
    --mount "type=volume,source=$V1_AVATAR_VOLUME,destination=/var/lib/crm/avatars,readonly" \
    --label com.photographer-crm.ops=true \
    --label "com.photographer-crm.ops.target-sha256=$V1_TARGET_HASH" \
    --label com.photographer-crm.ops.kind=planning-v2-avatar-readonly-helper \
    --label "com.photographer-crm.ops.lock-id-sha256=$V1_LOCK_ID_HASH" \
    --label "com.photographer-crm.ops.owner-nonce-sha256=$V1_OWNER_NONCE_HASH" \
    --entrypoint "$entrypoint" "$V1_APP_IMAGE_ID" "$@" 2>/dev/null)" || return 1
  V1_OPERATION_HELPERS+=("$created")
  id="$(v1_docker container inspect "$created" --format '{{.Id}}' 2>/dev/null)" || return 1
  [[ "$id" == "$created" ]] || return 1
  V1_LAST_HELPER_ID="$created"
}

while (($#)); do
  case "$1" in
    --env-file|--compose-file|--docker-context|--project-name|--input|--confirm-project|--deployment-identity|--target-generation)
      (($# >= 2)) || usage
      case "$1" in
        --env-file) V2_ENV_FILE="$2" ;;
        --compose-file) V2_COMPOSE_FILE="$2" ;;
        --docker-context) V2_DOCKER_CONTEXT="$2" ;;
        --project-name) V2_PROJECT_NAME="$2" ;;
        --input) V2_INPUT="$2"; V2_INPUT_SET=1 ;;
        --confirm-project) V2_CONFIRM_PROJECT="$2" ;;
        --deployment-identity) V2_IDENTITY_FILE="$2" ;;
        --target-generation) V2_TARGET_GENERATION="$2" ;;
      esac
      shift 2
      ;;
    --break-stale-lock)
      V2_BREAK_STALE_LOCK=1
      shift
      ;;
    *) usage ;;
  esac
done

restore_exit() {
  local original="$?"
  trap - EXIT INT TERM
  set +e
  if [[ "$original" -ne 0 && "$V2_APP_STOPPED" -eq 1 && "$V2_ORIGINAL_RUNNING" -eq 1 && "$V2_APP_REOPENED" -eq 0 ]]; then
    # After a destructive attempt the only truthful terminal state is stopped.
    v1_failure_stop >/dev/null 2>&1 || true
  fi
  local cleanup
  if v1_cleanup; then cleanup=0; else cleanup="$?"; fi
  set -e
  if [[ "$cleanup" -ne 0 ]]; then
    printf 'error code=11 stage=cleanup key=residual action=inspect-immutable-ids\n' >&2
    exit 11
  fi
  exit "$original"
}

restore_signal() {
  case "$1" in
    INT) exit 130 ;;
    TERM) exit 143 ;;
    *) exit 1 ;;
  esac
}

trap restore_exit EXIT
trap 'restore_signal INT' INT
trap 'restore_signal TERM' TERM

[[ -n "$V2_ENV_FILE" && -n "$V2_COMPOSE_FILE" && -n "$V2_DOCKER_CONTEXT" && -n "$V2_PROJECT_NAME" && "$V2_INPUT_SET" -eq 1 && -n "$V2_CONFIRM_PROJECT" && -n "$V2_IDENTITY_FILE" && -n "$V2_TARGET_GENERATION" ]] || usage
[[ "$V2_CONFIRM_PROJECT" == "$V2_PROJECT_NAME" ]] || v1_die 9 package-validate confirm-project type-exact-project-name
v1_require_commands
v1_validate_common_files
[[ -d "$V2_INPUT" && ! -L "$V2_INPUT" ]] || v1_die 9 path-validate input choose-existing-package-directory
V2_INPUT="$(python3 -c 'import os,sys; print(os.path.realpath(sys.argv[1]))' "$V2_INPUT")"
[[ -f "$V2_IDENTITY_FILE" && ! -L "$V2_IDENTITY_FILE" ]] || v1_die 3 identity-parse identity-file-regular
V2_IDENTITY_FILE="$(python3 -c 'import os,sys; print(os.path.realpath(sys.argv[1]))' "$V2_IDENTITY_FILE")"
PYTHONPATH="$repo_root/scripts/lib" python3 -c 'from pathlib import Path; import sys; from planningbackup.preflight import _identity; _identity(Path(sys.argv[1]))' "$V2_IDENTITY_FILE" || \
  v1_die 3 identity-parse identity-document-exact
V2_PRIVATE_DIR="$(python3 -c 'import tempfile; print(tempfile.mkdtemp(prefix="planning-v2-restore-"))')"
V1_PRIVATE_DIR="$V2_PRIVATE_DIR"
V1_PATH_AUTHORITY="$V2_INPUT"

v1_pin_endpoint
v1_render_and_image_safety
v1_acquire_lock
v1_preflight
v1_acquire_fence
v1_resolve_runtime
V2_PLANNING_VOLUME="$(v1_one_id volume planning_media_data)" || v1_die 5 target-identity planning-media-volume require-one-existing-volume
V1_PLANNING_VOLUME="$V2_PLANNING_VOLUME"
planning_mount="$(v1_docker container inspect "$V1_APP_ID" --format '{{range .Mounts}}{{if eq .Destination "/var/lib/crm/planning-media"}}{{.Name}}{{end}}{{end}}')"
[[ "$planning_mount" == "$V2_PLANNING_VOLUME" ]] || v1_die 5 target-identity planning-media-mount recreate-from-canonical-compose
planning_mountpoint="$(v1_docker volume inspect "$V2_PLANNING_VOLUME" --format '{{.Mountpoint}}')"
if python3 -c 'import os,sys; p=os.path.realpath(sys.argv[1]); r=os.path.realpath(sys.argv[2]); raise SystemExit(0 if p==r or p.startswith(r+os.sep) or r.startswith(p+os.sep) else 1)' "$V1_PATH_AUTHORITY" "$planning_mountpoint"; then
  v1_die 5 target-identity path source-volume-overlap
fi
v1_prepare_runtime_env

package_schema="$(PYTHONPATH="$repo_root/scripts/lib" python3 -c 'from pathlib import Path; import sys; from planningbackup.package import detect_schema; print(detect_schema(Path(sys.argv[1])))' "$V2_INPUT")" || \
  v1_die 9 package-validate schema use-valid-package
source_package_bytes="$(PYTHONPATH="$repo_root/scripts/lib" python3 - "$V2_INPUT" "$package_schema" <<'PY'
import os
import stat
import sys
from pathlib import Path

from planningbackup.package import V1_FILES, V2_FILES

root = Path(sys.argv[1])
expected = V2_FILES if sys.argv[2] == "2" else V1_FILES
if root.is_symlink() or not root.is_dir() or {entry.name for entry in root.iterdir()} != expected:
    raise SystemExit(1)
total = 0
for filename in expected:
    info = os.lstat(root / filename)
    if not stat.S_ISREG(info.st_mode):
        raise SystemExit(1)
    total += info.st_size
print(total)
PY
)" || v1_die 9 package-validate members-and-types use-valid-package
initial_staging_available="$(df -Pk "$V2_PRIVATE_DIR" 2>"$V2_PRIVATE_DIR/space-initial.stderr" | awk 'NR == 2 {printf "%.0f", $4 * 1024; exit}')" || true
[[ "$source_package_bytes" =~ ^[0-9]+$ && "$initial_staging_available" =~ ^[0-9]+$ ]] || \
  v1_die 9 preflight available-space inspect-staging-filesystem
(( initial_staging_available >= source_package_bytes + 67108864 )) || \
  v1_die 9 preflight available-space free-staging-capacity
if [[ "$package_schema" == 2 ]]; then
  PYTHONPATH="$repo_root/scripts/lib" python3 -m planningbackup snapshot-v2 \
    --input "$V2_INPUT" --output "$V2_PRIVATE_DIR/package-snapshot" >/dev/null || \
    v1_die 9 package-validate snapshot package-changed-or-invalid
  V2_INPUT="$V2_PRIVATE_DIR/package-snapshot"
fi

volume_available_bytes="$(v1_docker run --rm --network none \
  --mount "type=volume,source=$V2_PLANNING_VOLUME,destination=/var/lib/crm/planning-media,readonly" \
  --mount "type=volume,source=$V1_AVATAR_VOLUME,destination=/var/lib/crm/avatars,readonly" \
  --mount "type=volume,source=$V1_PG_VOLUME,destination=/var/lib/postgresql/data,readonly" \
  --entrypoint /bin/df "$V1_APP_IMAGE_ID" -Pk /var/lib/crm/planning-media /var/lib/crm/avatars /var/lib/postgresql/data \
  2>"$V2_PRIVATE_DIR/space.stderr" | awk 'NR > 1 {value=$4 * 1024; if (!minimum || value < minimum) minimum=value} END {if (minimum) printf "%.0f", minimum}')" || true
staging_available_bytes="$(df -Pk "$V2_PRIVATE_DIR" 2>>"$V2_PRIVATE_DIR/space.stderr" | awk 'NR == 2 {printf "%.0f", $4 * 1024; exit}')" || true
if [[ "$volume_available_bytes" =~ ^[0-9]+$ && "$staging_available_bytes" =~ ^[0-9]+$ ]]; then
  if (( volume_available_bytes < staging_available_bytes )); then available_bytes="$volume_available_bytes"; else available_bytes="$staging_available_bytes"; fi
else
  available_bytes=
fi
target_inventory="$(v1_docker run --rm --network none --mount "type=volume,source=$V2_PLANNING_VOLUME,destination=/var/lib/crm/planning-media,readonly" --entrypoint /bin/sh "$V1_APP_IMAGE_ID" -c '
root=/var/lib/crm/planning-media
total="$(find "$root" -mindepth 1 -print0 | tr -cd "\000" | wc -c)" || exit 1
files="$(find "$root" -mindepth 1 -type f -print0 | tr -cd "\000" | wc -c)" || exit 1
directories="$(find "$root" -mindepth 1 -type d -print0 | tr -cd "\000" | wc -c)" || exit 1
bytes="$(find "$root" -mindepth 1 -type f -exec stat -c %s {} + | awk '\''{sum+=$1} END {printf "%d", sum+0}'\'')" || exit 1
unsafe=$((total - files - directories))
test "$unsafe" -ge 0 || exit 1
printf "%d %d %d %d" "$files" "$bytes" "$unsafe" "$total"
' 2>"$V2_PRIVATE_DIR/target-inventory.stderr")" || true
read -r target_regular_files target_bytes target_unsafe_entries target_total_entries <<<"$target_inventory"
[[ "$target_regular_files" =~ ^[0-9]+$ && "$target_bytes" =~ ^[0-9]+$ && "$target_unsafe_entries" =~ ^[0-9]+$ && "$target_total_entries" =~ ^[0-9]+$ ]] || \
  v1_die 9 preflight target-inventory inspect-planning-volume
restore_tool_digest="$V1_APP_IMAGE_ID"
compose_sha="$(python3 -c 'import hashlib,sys; print(hashlib.sha256(open(sys.argv[1],"rb").read()).hexdigest())' "$V2_COMPOSE_FILE")" || \
  v1_die 9 preflight compose-file inspect-canonical-compose
target_pg_major="$(v1_docker exec "$V1_PG_ID" postgres -V 2>"$V2_PRIVATE_DIR/postgres-version.stderr" | python3 -c 'import re,sys; m=re.search(r"(\d+)(?:\.\d+)?",sys.stdin.read()); print(m.group(1) if m else "")')" || true
[[ "$target_pg_major" =~ ^[0-9]+$ ]] || v1_die 9 preflight postgres-major inspect-postgres-runtime
pg_user="$(python3 "$V1_OPS_HELPER" env-get --env-file "$V1_ENV_FILE" --key POSTGRES_USER 2>"$V2_PRIVATE_DIR/helper.stderr")" || \
  v1_die 9 preflight POSTGRES_USER inspect-managed-postgres-env
pg_db="$(python3 "$V1_OPS_HELPER" env-get --env-file "$V1_ENV_FILE" --key POSTGRES_DB 2>"$V2_PRIVATE_DIR/helper.stderr")" || \
  v1_die 9 preflight POSTGRES_DB inspect-managed-postgres-env
python3 "$V1_OPS_HELPER" db-counts-sql >"$V2_PRIVATE_DIR/target-database-counts.sql" \
  2>"$V2_PRIVATE_DIR/helper.stderr" || v1_die 9 preflight target-database-counts build-readonly-oracle
v1_docker exec -e 'PGOPTIONS=-c default_transaction_read_only=on' -i "$V1_PG_ID" \
  psql -X -Atq -v ON_ERROR_STOP=1 -U "$pg_user" "$pg_db" \
  <"$V2_PRIVATE_DIR/target-database-counts.sql" >"$V2_PRIVATE_DIR/target-database-counts.json" \
  2>"$V2_PRIVATE_DIR/target-database-counts.stderr" || \
  v1_die 9 preflight target-database-counts inspect-target-database-readonly
v2_create_avatar_helper manifest /usr/local/bin/avatar-manifest generate || \
  v1_die 9 preflight target-avatar-manifest create-readonly-helper
target_avatar_helper="$V1_LAST_HELPER_ID"
v1_start_helper_to_file "$target_avatar_helper" "$V2_PRIVATE_DIR/target-avatar-manifest.json" \
  2>"$V2_PRIVATE_DIR/target-avatar-manifest.stderr" || \
  v1_die 9 preflight target-avatar-manifest inspect-target-avatar-readonly
preflight_args=(
  --package "$V2_INPUT" --identity "$V2_IDENTITY_FILE"
  --target-planning-media "$V2_PRIVATE_DIR/unmounted-planning-target"
  --restore-tool-digest "$restore_tool_digest"
  --target-generation "$V2_TARGET_GENERATION"
  --target-compose-sha256 "$compose_sha"
  --target-app-digest "$V1_APP_IMAGE_ID"
  --target-postgres-digest "$V1_PG_IMAGE_ID"
  --target-postgres-major "$target_pg_major"
  --target-database-counts "$V2_PRIVATE_DIR/target-database-counts.json"
  --target-avatar-manifest "$V2_PRIVATE_DIR/target-avatar-manifest.json"
  --target-regular-files "$target_regular_files" --target-bytes "$target_bytes"
  --target-unsafe-entries "$target_unsafe_entries" --target-total-entries "$target_total_entries"
  --output "$V2_PRIVATE_DIR/preflight.json"
)
if [[ -n "$available_bytes" ]]; then preflight_args+=(--available-bytes "$available_bytes"); fi
set +e
PYTHONPATH="$repo_root/scripts/lib" python3 -m planningbackup preflight "${preflight_args[@]}"
preflight_code=$?
set -e
[[ -f "$V2_PRIVATE_DIR/preflight.json" ]] && cat "$V2_PRIVATE_DIR/preflight.json" || true
[[ "$preflight_code" -eq 0 ]] || v1_die 9 preflight destructive-not-authorized inspect-preflight-and-use-trusted-package

if [[ "$V1_APP_STATE" == running ]]; then
  V2_ORIGINAL_RUNNING=1
  v1_stop_app || v1_die 10 restore-stop app stop-and-verify-app
  V2_APP_STOPPED=1
fi

if [[ "$package_schema" == 1 ]]; then
  V1_STAGE=restore-v1-planning-fence
  v2_create_planning_helper ro v1-empty-before-replace /bin/sh -c 'test -z "$(find /var/lib/crm/planning-media -mindepth 1 -print -quit)"' || \
    v1_die 10 restore-v1-planning-fence helper create-verify-helper
  planning_v1_fence_helper="$V1_LAST_HELPER_ID"
  v1_start_helper_to_file "$planning_v1_fence_helper" "$V2_PRIVATE_DIR/planning-v1-fence.log" 2>"$V2_PRIVATE_DIR/planning-v1-fence.stderr" || \
    v1_die 10 restore-v1-planning-fence planning-media target-changed-after-preflight
fi

restore_input="$V2_INPUT"
if [[ "$package_schema" == 2 ]]; then
  PYTHONPATH="$repo_root/scripts/lib" python3 -m planningbackup make-v1-view --input "$V2_INPUT" --output "$V2_PRIVATE_DIR/v1-view" --project "$V2_PROJECT_NAME" >/dev/null || \
    v1_die 10 package-validate compatibility-view create-v1-reader-view
  restore_input="$V2_PRIVATE_DIR/v1-view"
fi

restore_args=(
  --env-file "$V2_ENV_FILE" --compose-file "$V2_COMPOSE_FILE" --docker-context "$V2_DOCKER_CONTEXT"
  --project-name "$V2_PROJECT_NAME" --input "$restore_input" --confirm-project "$V2_PROJECT_NAME" --replace-only
)
if [[ "$V2_BREAK_STALE_LOCK" -eq 1 ]]; then restore_args+=(--break-stale-lock); fi
V1_STAGE=restore-v1-base
V1_INHERITED_LOCK_ID="$V1_LOCK_ID" \
V1_INHERITED_OWNER_NONCE="$V1_OWNER_NONCE" \
V1_INHERITED_FENCE_ID="$V1_FENCE_ID" \
  "$repo_root/scripts/restore-compose.sh" "${restore_args[@]}" || v1_die 10 restore-v1-base database-avatar failure-stop-required

if [[ "$package_schema" == 2 ]]; then
  V1_STAGE=restore-planning-media-replace
  v2_create_planning_helper rw replace /bin/sh -c 'find /var/lib/crm/planning-media -mindepth 1 -maxdepth 1 -exec rm -rf {} + && tar -xzf /tmp/planning-media-volume.tgz -C /var/lib/crm/planning-media' || \
    v1_die 10 restore-planning-media-replace helper create-replace-helper
  planning_replace_helper="$V1_LAST_HELPER_ID"
  v1_docker cp "$V2_INPUT/planning-media-volume.tgz" "$planning_replace_helper:/tmp/planning-media-volume.tgz" >/dev/null 2>"$V2_PRIVATE_DIR/planning-docker-cp.stderr" || \
    v1_die 10 restore-planning-media-replace staging failure-stop-required
  v1_start_helper_to_file "$planning_replace_helper" "$V2_PRIVATE_DIR/planning-replace.log" 2>"$V2_PRIVATE_DIR/planning-replace.stderr" || \
    v1_die 10 restore-planning-media-replace planning-media failure-stop-required
fi

V1_STAGE=restore-migrate
v1_compose run --rm --no-deps --entrypoint /usr/local/bin/migrate app >/dev/null 2>"$V2_PRIVATE_DIR/migrate.stderr" || \
  v1_die 10 restore-migrate database failure-stop-required

V1_STAGE=restore-validate-database
python3 "$V1_OPS_HELPER" db-counts-sql >"$V2_PRIVATE_DIR/final-database-counts.sql" \
  2>"$V2_PRIVATE_DIR/helper.stderr" || v1_die 10 restore-validate-database oracle failure-stop-required
v1_docker exec -e 'PGOPTIONS=-c default_transaction_read_only=on' -i "$V1_PG_ID" \
  psql -X -Atq -v ON_ERROR_STOP=1 -U "$pg_user" "$pg_db" \
  <"$V2_PRIVATE_DIR/final-database-counts.sql" >"$V2_PRIVATE_DIR/final-database-counts.json" \
  2>"$V2_PRIVATE_DIR/final-database-counts.stderr" || \
  v1_die 10 restore-validate-database database-counts failure-stop-required
python3 "$V1_OPS_HELPER" verify-db-counts \
  --metadata "$restore_input/metadata.json" \
  --actual "$V2_PRIVATE_DIR/final-database-counts.json" \
  2>"$V2_PRIVATE_DIR/helper.stderr" || v1_die 10 restore-validate-database database-counts failure-stop-required

V1_STAGE=restore-validate-avatar
v2_create_avatar_helper final-verify /usr/local/bin/avatar-manifest --manifest /tmp/avatar-manifest.json verify || \
  v1_die 10 restore-validate-avatar helper create-verify-helper
final_avatar_helper="$V1_LAST_HELPER_ID"
v1_docker cp "$restore_input/avatar-manifest.json" "$final_avatar_helper:/tmp/avatar-manifest.json" >/dev/null \
  2>"$V2_PRIVATE_DIR/final-avatar-cp.stderr" || v1_die 10 restore-validate-avatar staging failure-stop-required
v1_start_helper_to_file "$final_avatar_helper" "$V2_PRIVATE_DIR/final-avatar-verify.log" \
  2>"$V2_PRIVATE_DIR/final-avatar-verify.stderr" || \
  v1_die 10 restore-validate-avatar exact-generation failure-stop-required

if [[ "$package_schema" == 2 ]]; then
  V1_STAGE=restore-validate-planning-media
  v2_create_planning_helper ro verify /usr/local/bin/planning-media-manifest --root /var/lib/crm/planning-media --manifest /tmp/planning-media-manifest.json || \
    v1_die 10 restore-validate-planning-media helper create-verify-helper
  planning_verify_helper="$V1_LAST_HELPER_ID"
  v1_docker cp "$V2_INPUT/planning-media-manifest.json" "$planning_verify_helper:/tmp/planning-media-manifest.json" >/dev/null 2>"$V2_PRIVATE_DIR/planning-verify-cp.stderr" || \
    v1_die 10 restore-validate-planning-media staging failure-stop-required
  v1_start_helper_to_file "$planning_verify_helper" "$V2_PRIVATE_DIR/planning-verify.log" 2>"$V2_PRIVATE_DIR/planning-verify.stderr" || \
    v1_die 10 restore-validate-planning-media exact-generation failure-stop-required
else
  V1_STAGE=restore-validate-planning-target
  v2_create_planning_helper ro empty /bin/sh -c 'test -z "$(find /var/lib/crm/planning-media -mindepth 1 -print -quit)"' || \
    v1_die 10 restore-validate-planning-target helper create-verify-helper
  planning_empty_helper="$V1_LAST_HELPER_ID"
  v1_start_helper_to_file "$planning_empty_helper" "$V2_PRIVATE_DIR/planning-empty.log" 2>"$V2_PRIVATE_DIR/planning-empty.stderr" || \
    v1_die 10 restore-validate-planning-target planning-media target-not-empty
fi

if [[ "$V2_ORIGINAL_RUNNING" -eq 1 ]]; then
  V1_STAGE=restore-reopen
  v1_start_and_health || v1_die 10 restore-reopen app failure-stop-complete
  V2_APP_REOPENED=1
fi

V2_APP_STOPPED=0
printf 'stage/restore-preflight=pass\n'
printf 'stage/restore-stop-writes=pass\n'
printf 'stage/restore-replace=pass\n'
printf 'stage/restore-migrate=pass\n'
printf 'stage/restore-validate=pass\n'
printf 'stage/restore-reopen=%s\n' "$([[ "$V2_ORIGINAL_RUNNING" -eq 1 ]] && printf pass || printf preserved-exited)"
printf 'stage/complete=pass\n'
