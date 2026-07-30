#!/usr/bin/env bash
set -euo pipefail
umask 077

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# shellcheck source=scripts/lib/v1-ops-common.sh
source "$repo_root/scripts/lib/v1-ops-common.sh"

V1_ENV_FILE=
V1_COMPOSE_FILE=
V1_DOCKER_CONTEXT=
V1_PROJECT_NAME=
V1_INPUT=
V1_INPUT_SET=0
V1_CONFIRM_PROJECT=
V1_PATH_AUTHORITY=
V1_STAGING=
V1_RESTORE_DESTRUCTIVE=0
V1_RESTORE_ORIGINAL_RUNNING=0
V1_SIGNAL_EXIT=0

usage() {
  v1_die 2 usage arguments use-required-restore-flags
}

while (($#)); do
  case "$1" in
    --env-file|--compose-file|--docker-context|--project-name|--input|--confirm-project)
      (($# >= 2)) || usage
      case "$1" in
        --env-file) V1_ENV_FILE="$2" ;;
        --compose-file) V1_COMPOSE_FILE="$2" ;;
        --docker-context) V1_DOCKER_CONTEXT="$2" ;;
        --project-name) V1_PROJECT_NAME="$2" ;;
        --input) V1_INPUT="$2"; V1_INPUT_SET=1 ;;
        --confirm-project) V1_CONFIRM_PROJECT="$2" ;;
      esac
      shift 2
      ;;
    --break-stale-lock)
      V1_BREAK_STALE_LOCK=1
      shift
      ;;
    *) usage ;;
  esac
done

restore_exit() {
  local original="$?"
  if [[ "$V1_SIGNAL_EXIT" -ne 0 ]]; then
    original="$V1_SIGNAL_EXIT"
  fi
  trap - EXIT INT TERM
  set +e
  if [[ "$original" -ne 0 && "$V1_RESTORE_DESTRUCTIVE" -eq 1 && -n "$V1_APP_ID" ]]; then
    if v1_failure_stop >/dev/null 2>&1; then
      printf 'stage/restore-failure-stop=pass\n'
    else
      V1_CLEANUP_FAILED=1
    fi
    original=10
  fi
  local cleanup
  if v1_cleanup; then
    cleanup=0
  else
    cleanup="$?"
  fi
  set -e
  if [[ "$cleanup" -ne 0 ]]; then
    printf 'error code=11 stage=cleanup key=residual action=inspect-immutable-ids\n' >&2
    exit 11
  fi
  exit "$original"
}

restore_signal() {
  case "$1" in
    INT) V1_SIGNAL_EXIT=130 ;;
    TERM) V1_SIGNAL_EXIT=143 ;;
    *) V1_SIGNAL_EXIT=1 ;;
  esac
  exit "$V1_SIGNAL_EXIT"
}

trap restore_exit EXIT
trap 'restore_signal INT' INT
trap 'restore_signal TERM' TERM

[[ -n "$V1_ENV_FILE" && -n "$V1_COMPOSE_FILE" && -n "$V1_DOCKER_CONTEXT" && -n "$V1_PROJECT_NAME" && "$V1_INPUT_SET" -eq 1 && -n "$V1_CONFIRM_PROJECT" ]] || usage
v1_require_commands
v1_validate_common_files

[[ -n "$V1_CONFIRM_PROJECT" && "$V1_CONFIRM_PROJECT" == "$V1_PROJECT_NAME" ]] || \
  v1_die 9 package-validate confirm-project type-exact-project-name

V1_STAGE=path-validate
[[ -n "$V1_INPUT" && "$V1_INPUT" != / ]] || v1_die 9 path-validate input choose-existing-package-directory
python3 - "$V1_INPUT" "$repo_root" <<'PY' || v1_die 9 path-validate input choose-safe-package-directory
import os, stat, sys
raw, root = sys.argv[1:]
if os.path.islink(raw) or not os.path.isdir(raw):
    raise SystemExit(1)
resolved = os.path.realpath(raw)
if resolved in (os.sep, os.path.realpath(root)):
    raise SystemExit(1)
for entry in os.scandir(resolved):
    mode = entry.stat(follow_symlinks=False).st_mode
    if entry.is_symlink() or not stat.S_ISREG(mode):
        raise SystemExit(1)
PY
V1_INPUT="$(python3 -c 'import os,sys; print(os.path.realpath(sys.argv[1]))' "$V1_INPUT" 2>/dev/null)" || \
  v1_die 9 path-validate input choose-safe-package-directory
V1_PATH_AUTHORITY="$V1_INPUT"
# This is only an early typed-project rejection.  The authoritative schema,
# checksum and project validation is repeated from the lock-protected staging
# snapshot below.
python3 - "$V1_INPUT/metadata.json" "$V1_PROJECT_NAME" <<'PY' || \
  v1_die 9 package-validate project use-package-for-target-project
import json, os, stat, sys
path, project = sys.argv[1:]
try:
    info = os.lstat(path)
    if stat.S_ISREG(info.st_mode) and not os.path.islink(path):
        with open(path, encoding="utf-8") as stream:
            metadata = json.load(stream)
        source = metadata.get("source_compose_project") if isinstance(metadata, dict) else None
        if isinstance(source, str) and source and source != project:
            raise SystemExit(1)
except (OSError, json.JSONDecodeError):
    pass
PY
V1_PRIVATE_DIR="$(python3 -c 'import tempfile; print(tempfile.mkdtemp(prefix="v1-ops-restore-"))' 2>/dev/null)" || \
  v1_die 9 package-validate staging create-private-staging
V1_STAGING="$V1_PRIVATE_DIR/staging"

v1_pin_endpoint
v1_render_and_image_safety
v1_acquire_lock
v1_preflight
v1_acquire_fence
v1_resolve_runtime
v1_prepare_runtime_env

V1_STAGE=package-validate
python3 "$V1_OPS_HELPER" snapshot \
  --input "$V1_INPUT" \
  --output "$V1_STAGING" \
  --project "$V1_PROJECT_NAME" \
  2>"$V1_PRIVATE_DIR/helper.stderr" || v1_die 9 package-validate package use-valid-five-file-package
python3 "$V1_OPS_HELPER" db-counts-sql >"$V1_PRIVATE_DIR/database-counts.sql" \
  2>"$V1_PRIVATE_DIR/helper.stderr" || v1_die 9 package-validate database-counts use-valid-five-file-package
v1_status package-validate pass

if [[ "$V1_APP_STATE" == running ]]; then
  V1_RESTORE_ORIGINAL_RUNNING=1
  V1_STAGE=restore-stop
  v1_stop_app || v1_die 10 restore-stop app stop-and-verify-app
  v1_status restore-stop pass
fi
V1_RESTORE_DESTRUCTIVE=1

pg_user="$(python3 "$V1_OPS_HELPER" env-get --env-file "$V1_ENV_FILE" --key POSTGRES_USER 2>"$V1_PRIVATE_DIR/helper.stderr")" || \
  v1_die 3 env-parse POSTGRES_USER fix-managed-postgres-env
pg_db="$(python3 "$V1_OPS_HELPER" env-get --env-file "$V1_ENV_FILE" --key POSTGRES_DB 2>"$V1_PRIVATE_DIR/helper.stderr")" || \
  v1_die 3 env-parse POSTGRES_DB fix-managed-postgres-env

V1_STAGE=restore-db-replace
v1_recheck_generation || v1_die 10 restore-db-replace generation failure-stop-required
v1_docker exec "$V1_PG_ID" dropdb -U "$pg_user" --if-exists --force "$pg_db" \
  >/dev/null 2>"$V1_PRIVATE_DIR/dropdb.stderr" || \
  v1_die 10 restore-db-replace dropdb failure-stop-required
v1_docker exec "$V1_PG_ID" createdb -U "$pg_user" "$pg_db" \
  >/dev/null 2>"$V1_PRIVATE_DIR/createdb.stderr" || \
  v1_die 10 restore-db-replace createdb failure-stop-required
v1_docker exec -i "$V1_PG_ID" psql -X -v ON_ERROR_STOP=1 -U "$pg_user" "$pg_db" \
  <"$V1_STAGING/database.sql" >/dev/null 2>"$V1_PRIVATE_DIR/psql.stderr" || \
  v1_die 10 restore-db-replace psql failure-stop-required
v1_status restore-db-replace pass

V1_STAGE=restore-avatar-replace
v1_create_helper restore avatar /bin/sh -c \
  'find /var/lib/crm/avatars -mindepth 1 -maxdepth 1 -exec rm -rf {} + && tar -xzf /tmp/avatar-volume.tgz -C /var/lib/crm/avatars' || \
  v1_die 10 restore-avatar-replace helper failure-stop-required
avatar_helper="$V1_LAST_HELPER_ID"
v1_docker cp "$V1_STAGING/avatar-volume.tgz" "$avatar_helper:/tmp/avatar-volume.tgz" \
  >/dev/null 2>"$V1_PRIVATE_DIR/docker-cp.stderr" || \
  v1_die 10 restore-avatar-replace staging failure-stop-required
v1_start_helper_to_file "$avatar_helper" "$V1_PRIVATE_DIR/avatar-restore.log" \
  2>"$V1_PRIVATE_DIR/avatar-restore.stderr" || \
  v1_die 10 restore-avatar-replace avatar failure-stop-required
v1_status restore-avatar-replace pass

V1_STAGE=restore-verify
v1_create_helper restore verify /usr/local/bin/avatar-manifest --manifest /tmp/avatar-manifest.json verify || \
  v1_die 10 restore-verify helper failure-stop-required
verify_helper="$V1_LAST_HELPER_ID"
v1_docker cp "$V1_STAGING/avatar-manifest.json" "$verify_helper:/tmp/avatar-manifest.json" \
  >/dev/null 2>"$V1_PRIVATE_DIR/docker-cp.stderr" || \
  v1_die 10 restore-verify staging failure-stop-required
v1_start_helper_to_file "$verify_helper" "$V1_PRIVATE_DIR/avatar-verify.log" \
  2>"$V1_PRIVATE_DIR/avatar-verify.stderr" || \
  v1_die 10 restore-verify exact-generation failure-stop-required
v1_docker exec -i "$V1_PG_ID" psql -X -Atq -v ON_ERROR_STOP=1 -U "$pg_user" "$pg_db" \
  <"$V1_PRIVATE_DIR/database-counts.sql" >"$V1_PRIVATE_DIR/database-counts-actual.json" \
  2>"$V1_PRIVATE_DIR/psql.stderr" || v1_die 10 restore-verify database-counts failure-stop-required
python3 "$V1_OPS_HELPER" verify-db-counts \
  --metadata "$V1_STAGING/metadata.json" \
  --actual "$V1_PRIVATE_DIR/database-counts-actual.json" \
  2>"$V1_PRIVATE_DIR/helper.stderr" || v1_die 10 restore-verify database-counts failure-stop-required
v1_status restore-verify pass

if [[ "$V1_RESTORE_ORIGINAL_RUNNING" -eq 1 ]]; then
  V1_STAGE=restore-start
  v1_recheck_generation || v1_die 10 restore-start generation failure-stop-required
  if ! v1_start_app; then
    if v1_failure_stop; then
      v1_status restore-failure-stop pass
      V1_RESTORE_DESTRUCTIVE=0
    else
      v1_die 10 restore-failure-stop app inspect-exited-state
    fi
    v1_die 10 restore-start app failure-stop-complete
  fi
  v1_status restore-start pass
  V1_STAGE=restore-health
  if ! v1_wait_app_health; then
    if v1_failure_stop; then
      v1_status restore-failure-stop pass
      V1_RESTORE_DESTRUCTIVE=0
    else
      v1_die 10 restore-failure-stop app inspect-exited-state
    fi
    v1_die 10 restore-health app failure-stop-complete
  fi
  v1_status restore-health pass
else
  [[ "$(v1_docker container inspect "$V1_APP_ID" 2>/dev/null | python3 "$V1_OPS_HELPER" app-state 2>/dev/null)" == exited ]] || \
    v1_die 10 restore-failure-stop app inspect-exited-state
fi

V1_RESTORE_DESTRUCTIVE=0
v1_status restore-package verified
v1_status target-hash "$V1_TARGET_HASH"
V1_STAGE=complete
v1_status complete pass
