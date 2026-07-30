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
V1_OUTPUT=
V1_OUTPUT_SET=0
V1_PATH_AUTHORITY=
V1_TEMP_PACKAGE=
V1_BACKUP_STOPPED=0
V1_BACKUP_ORIGINAL_RUNNING=0
V1_SIGNAL_EXIT=0

usage() {
  v1_die 2 usage arguments use-required-backup-flags
}

while (($#)); do
  case "$1" in
    --env-file|--compose-file|--docker-context|--project-name|--output)
      (($# >= 2)) || usage
      case "$1" in
        --env-file) V1_ENV_FILE="$2" ;;
        --compose-file) V1_COMPOSE_FILE="$2" ;;
        --docker-context) V1_DOCKER_CONTEXT="$2" ;;
        --project-name) V1_PROJECT_NAME="$2" ;;
        --output) V1_OUTPUT="$2"; V1_OUTPUT_SET=1 ;;
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

backup_exit() {
  local original="$?" state
  if [[ "$V1_SIGNAL_EXIT" -ne 0 ]]; then
    original="$V1_SIGNAL_EXIT"
  fi
  trap - EXIT INT TERM
  set +e
  if [[ "$original" -ne 0 && "$V1_BACKUP_ORIGINAL_RUNNING" -eq 1 && -n "$V1_APP_ID" ]]; then
    state="$(v1_docker container inspect "$V1_APP_ID" 2>/dev/null | python3 "$V1_OPS_HELPER" app-state 2>/dev/null)"
    if [[ "$state" == exited ]]; then
      v1_start_and_health >/dev/null 2>&1 || true
    fi
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

backup_signal() {
  case "$1" in
    INT) V1_SIGNAL_EXIT=130 ;;
    TERM) V1_SIGNAL_EXIT=143 ;;
    *) V1_SIGNAL_EXIT=1 ;;
  esac
  exit "$V1_SIGNAL_EXIT"
}

trap backup_exit EXIT
trap 'backup_signal INT' INT
trap 'backup_signal TERM' TERM

[[ -n "$V1_ENV_FILE" && -n "$V1_COMPOSE_FILE" && -n "$V1_DOCKER_CONTEXT" && -n "$V1_PROJECT_NAME" && "$V1_OUTPUT_SET" -eq 1 ]] || usage
v1_require_commands
v1_validate_common_files

V1_STAGE=path-validate
[[ -n "$V1_OUTPUT" && "$V1_OUTPUT" != / ]] || v1_die 2 path-validate output choose-new-backup-leaf
python3 - "$V1_OUTPUT" "$repo_root" <<'PY' || v1_die 2 path-validate output choose-safe-new-backup-leaf
import os, stat, sys
raw, root = sys.argv[1:]
parent, leaf = os.path.split(raw.rstrip(os.sep))
if not leaf:
    raise SystemExit(1)
parent = parent or "."
if not os.path.isdir(parent) or os.path.islink(parent):
    raise SystemExit(1)
candidate = os.path.abspath(os.path.join(os.path.realpath(parent), leaf))
if candidate in (os.path.realpath(root), os.sep) or os.path.lexists(candidate):
    raise SystemExit(1)
PY
V1_OUTPUT_PARENT="$(python3 -c 'import os,sys; print(os.path.realpath(os.path.dirname(sys.argv[1]) or "."))' "$V1_OUTPUT" 2>/dev/null)" || \
  v1_die 2 path-validate output choose-safe-new-backup-leaf
V1_OUTPUT_LEAF="$(python3 -c 'import os,sys; print(os.path.basename(sys.argv[1].rstrip(os.sep)))' "$V1_OUTPUT" 2>/dev/null)" || \
  v1_die 2 path-validate output choose-safe-new-backup-leaf
V1_OUTPUT="$V1_OUTPUT_PARENT/$V1_OUTPUT_LEAF"
V1_PATH_AUTHORITY="$V1_OUTPUT"
V1_PRIVATE_DIR="$(python3 -c 'import tempfile,sys; print(tempfile.mkdtemp(prefix=".v1-ops-backup-", dir=sys.argv[1]))' "$V1_OUTPUT_PARENT" 2>/dev/null)" || \
  v1_die 2 path-validate output create-private-staging
V1_TEMP_PACKAGE="$V1_PRIVATE_DIR/package"
mkdir -m 700 "$V1_TEMP_PACKAGE"

v1_pin_endpoint
v1_render_and_image_safety
v1_acquire_lock
v1_preflight
v1_acquire_fence
v1_resolve_runtime
v1_prepare_runtime_env

if [[ "$V1_APP_STATE" == running ]]; then
  V1_BACKUP_ORIGINAL_RUNNING=1
  V1_STAGE=backup-stop
  v1_stop_app || v1_die 8 backup-stop app stop-and-verify-app
  V1_BACKUP_STOPPED=1
  v1_status backup-stop pass
fi

pg_user="$(python3 "$V1_OPS_HELPER" env-get --env-file "$V1_ENV_FILE" --key POSTGRES_USER 2>"$V1_PRIVATE_DIR/helper.stderr")" || \
  v1_die 3 env-parse POSTGRES_USER fix-managed-postgres-env
pg_db="$(python3 "$V1_OPS_HELPER" env-get --env-file "$V1_ENV_FILE" --key POSTGRES_DB 2>"$V1_PRIVATE_DIR/helper.stderr")" || \
  v1_die 3 env-parse POSTGRES_DB fix-managed-postgres-env

V1_STAGE=backup-pg-dump
v1_recheck_generation || v1_die 7 helper-fence generation lock-or-fence-lost
python3 "$V1_OPS_HELPER" db-counts-sql >"$V1_PRIVATE_DIR/database-counts.sql" \
  2>"$V1_PRIVATE_DIR/helper.stderr" || v1_die 8 backup-pg-dump database-counts build-count-oracle
v1_docker exec -i "$V1_PG_ID" psql -X -Atq -v ON_ERROR_STOP=1 -U "$pg_user" "$pg_db" \
  <"$V1_PRIVATE_DIR/database-counts.sql" >"$V1_PRIVATE_DIR/database-counts.json" \
  2>"$V1_PRIVATE_DIR/psql.stderr" || v1_die 8 backup-pg-dump database-counts retry-after-postgres-check
v1_docker exec "$V1_PG_ID" pg_dump -U "$pg_user" "$pg_db" >"$V1_TEMP_PACKAGE/database.sql" \
  2>"$V1_PRIVATE_DIR/pg-dump.stderr" || \
  v1_die 8 backup-pg-dump database retry-after-postgres-check
v1_status backup-pg-dump pass

V1_STAGE=backup-avatar-tar
v1_create_helper backup tar /bin/tar -C /var/lib/crm/avatars -czf - . || \
  v1_die 8 backup-avatar-tar helper create-tar-helper
tar_helper="$V1_LAST_HELPER_ID"
v1_start_helper_to_file "$tar_helper" "$V1_TEMP_PACKAGE/avatar-volume.tgz" \
  2>"$V1_PRIVATE_DIR/tar.stderr" || \
  v1_die 8 backup-avatar-tar avatar retry-after-volume-check
v1_status backup-avatar-tar pass

V1_STAGE=backup-manifest
v1_create_helper backup manifest /usr/local/bin/avatar-manifest generate || \
  v1_die 8 backup-manifest helper create-manifest-helper
manifest_helper="$V1_LAST_HELPER_ID"
v1_start_helper_to_file "$manifest_helper" "$V1_TEMP_PACKAGE/avatar-manifest.json" \
  2>"$V1_PRIVATE_DIR/manifest.stderr" || \
  v1_die 8 backup-manifest manifest inspect-app-image-and-data
compose_sha="$(python3 -c 'import hashlib,sys; print(hashlib.sha256(open(sys.argv[1],"rb").read()).hexdigest())' \
  "$V1_COMPOSE_FILE" 2>"$V1_PRIVATE_DIR/compose-hash.stderr")" || \
  v1_die 8 backup-manifest compose-file rebuild-backup
app_digest="$(v1_docker image inspect "$V1_APP_IMAGE_ID" --format '{{if .RepoDigests}}{{index .RepoDigests 0}}{{end}}' 2>"$V1_PRIVATE_DIR/image-inspect.stderr")" || \
  v1_die 8 backup-manifest app-image inspect-runtime-image
pg_digest="$(v1_docker image inspect "$V1_PG_IMAGE_ID" --format '{{if .RepoDigests}}{{index .RepoDigests 0}}{{end}}' 2>"$V1_PRIVATE_DIR/image-inspect.stderr")" || \
  v1_die 8 backup-manifest postgres-image inspect-runtime-image
pg_major="$(v1_docker exec "$V1_PG_ID" postgres -V 2>"$V1_PRIVATE_DIR/postgres-version.stderr" | python3 -c 'import re,sys; m=re.search(r"(\d+)(?:\.\d+)?",sys.stdin.read()); print(m.group(1) if m else "")')" || \
  v1_die 8 backup-manifest postgres-version inspect-postgres-image
[[ -n "$pg_major" ]] || v1_die 8 backup-manifest postgres-version inspect-postgres-image
python3 "$V1_OPS_HELPER" metadata \
  --output "$V1_TEMP_PACKAGE/metadata.json" \
  --project "$V1_PROJECT_NAME" \
  --compose-sha256 "$compose_sha" \
  --app-image-id "$V1_APP_IMAGE_ID" \
  --pg-image-id "$V1_PG_IMAGE_ID" \
  --app-repo-digest "$app_digest" \
  --pg-repo-digest "$pg_digest" \
  --pg-major "$pg_major" \
  --app-was-running "$([[ "$V1_BACKUP_ORIGINAL_RUNNING" -eq 1 ]] && printf true || printf false)" \
  --db-counts-file "$V1_PRIVATE_DIR/database-counts.json" \
  2>"$V1_PRIVATE_DIR/helper.stderr" || \
  v1_die 8 backup-manifest metadata rebuild-backup
python3 "$V1_OPS_HELPER" checksums --output "$V1_TEMP_PACKAGE/SHA256SUMS" \
  2>"$V1_PRIVATE_DIR/helper.stderr" || \
  v1_die 8 backup-manifest checksums rebuild-backup
v1_status backup-manifest pass

V1_STAGE=backup-package-verify
python3 "$V1_OPS_HELPER" validate --input "$V1_TEMP_PACKAGE" --project "$V1_PROJECT_NAME" \
  2>"$V1_PRIVATE_DIR/helper.stderr" || \
  v1_die 8 backup-package-verify package rebuild-backup
v1_status backup-package-verify pass

if [[ "$V1_BACKUP_ORIGINAL_RUNNING" -eq 1 ]]; then
  V1_STAGE=backup-state-restore
  v1_start_and_health || v1_die 8 backup-health app-health keep-package-unpublished
  V1_BACKUP_STOPPED=0
  v1_status backup-state-restore pass
  v1_status backup-health pass
fi

V1_STAGE=backup-publish
python3 "$V1_OPS_HELPER" publish \
  --input "$V1_TEMP_PACKAGE" \
  --output "$V1_OUTPUT" \
  --project "$V1_PROJECT_NAME" \
  2>"$V1_PRIVATE_DIR/publish.stderr" || v1_die 8 backup-publish output choose-new-backup-leaf
v1_status backup-publish pass
V1_TEMP_PACKAGE=
v1_status backup-package published-valid
v1_status target-hash "$V1_TARGET_HASH"
V1_STAGE=complete
v1_status complete pass
