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
V2_OUTPUT=
V2_IDENTITY_FILE=
V2_OUTPUT_SET=0
V2_ORIGINAL_RUNNING=0
V2_APP_STOPPED=0
V2_APP_REOPENED=0
V2_PRIVATE_DIR=
V2_PLANNING_VOLUME=
V2_BREAK_STALE_LOCK=0

v2_create_planning_helper() {
  local purpose="$1" entrypoint="$2" name created id
  shift 2
  name="photographer-private-crm-planning-v2-${purpose}-${V1_TARGET_HASH:0:16}-${#V1_OPERATION_HELPERS[@]}"
  created="$(v1_docker create --name "$name" --network none --user 0:0 \
    --mount "type=volume,source=$V2_PLANNING_VOLUME,destination=/var/lib/crm/planning-media,readonly" \
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

usage() {
  v1_die 2 usage arguments use-required-v2-backup-flags
}

while (($#)); do
  case "$1" in
    --env-file|--compose-file|--docker-context|--project-name|--output|--deployment-identity)
      (($# >= 2)) || usage
      case "$1" in
        --env-file) V2_ENV_FILE="$2" ;;
        --compose-file) V2_COMPOSE_FILE="$2" ;;
        --docker-context) V2_DOCKER_CONTEXT="$2" ;;
        --project-name) V2_PROJECT_NAME="$2" ;;
        --output) V2_OUTPUT="$2"; V2_OUTPUT_SET=1 ;;
        --deployment-identity) V2_IDENTITY_FILE="$2" ;;
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

backup_exit() {
  local original="$?"
  trap - EXIT INT TERM
  set +e
  if [[ "$original" -ne 0 && "$V2_APP_STOPPED" -eq 1 && "$V2_ORIGINAL_RUNNING" -eq 1 && "$V2_APP_REOPENED" -eq 0 ]]; then
    v1_start_and_health >/dev/null 2>&1 || v1_failure_stop >/dev/null 2>&1 || true
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

backup_signal() {
  case "$1" in
    INT) exit 130 ;;
    TERM) exit 143 ;;
    *) exit 1 ;;
  esac
}

trap backup_exit EXIT
trap 'backup_signal INT' INT
trap 'backup_signal TERM' TERM

[[ -n "$V2_ENV_FILE" && -n "$V2_COMPOSE_FILE" && -n "$V2_DOCKER_CONTEXT" && -n "$V2_PROJECT_NAME" && "$V2_OUTPUT_SET" -eq 1 && -n "$V2_IDENTITY_FILE" ]] || usage
v1_require_commands
v1_validate_common_files
[[ -f "$V2_IDENTITY_FILE" && ! -L "$V2_IDENTITY_FILE" ]] || v1_die 3 identity-parse identity-file-regular
V2_IDENTITY_FILE="$(python3 -c 'import os,sys; print(os.path.realpath(sys.argv[1]))' "$V2_IDENTITY_FILE")"
PYTHONPATH="$repo_root/scripts/lib" python3 -c 'from pathlib import Path; import sys; from planningbackup.preflight import _identity; _identity(Path(sys.argv[1]))' "$V2_IDENTITY_FILE" || \
  v1_die 3 identity-parse identity-document-exact

V1_STAGE=path-validate
[[ -n "$V2_OUTPUT" && "$V2_OUTPUT" != / ]] || v1_die 2 path-validate output choose-new-backup-leaf
python3 - "$V2_OUTPUT" "$repo_root" <<'PY' || v1_die 2 path-validate output choose-safe-new-backup-leaf
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
V2_OUTPUT_PARENT="$(python3 -c 'import os,sys; print(os.path.realpath(os.path.dirname(sys.argv[1]) or "."))' "$V2_OUTPUT")"
V2_OUTPUT="$V2_OUTPUT_PARENT/$(python3 -c 'import os,sys; print(os.path.basename(sys.argv[1].rstrip(os.sep)))' "$V2_OUTPUT")"
V2_PRIVATE_DIR="$(python3 -c 'import tempfile,sys; print(tempfile.mkdtemp(prefix=".planning-v2-backup-", dir=sys.argv[1]))' "$V2_OUTPUT_PARENT")"
V1_PRIVATE_DIR="$V2_PRIVATE_DIR"
V1_PATH_AUTHORITY="$V2_OUTPUT"

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

if [[ "$V1_APP_STATE" == running ]]; then
  V2_ORIGINAL_RUNNING=1
  v1_stop_app || v1_die 8 backup-stop app stop-and-verify-app
  V2_APP_STOPPED=1
fi

base_output="$V2_PRIVATE_DIR/v1-package"
backup_args=(
  --env-file "$V2_ENV_FILE" --compose-file "$V2_COMPOSE_FILE" --docker-context "$V2_DOCKER_CONTEXT"
  --project-name "$V2_PROJECT_NAME" --output "$base_output"
)
if [[ "$V2_BREAK_STALE_LOCK" -eq 1 ]]; then backup_args+=(--break-stale-lock); fi
V1_STAGE=backup-v1-base
V1_INHERITED_LOCK_ID="$V1_LOCK_ID" \
V1_INHERITED_OWNER_NONCE="$V1_OWNER_NONCE" \
V1_INHERITED_FENCE_ID="$V1_FENCE_ID" \
  "$repo_root/scripts/backup-compose.sh" "${backup_args[@]}" || v1_die 8 backup-v1-base package create-v1-base

V1_STAGE=backup-planning-media-tar
v2_create_planning_helper tar /bin/tar -C /var/lib/crm/planning-media -czf - . || v1_die 8 backup-planning-media-tar helper create-tar-helper
planning_tar_helper="$V1_LAST_HELPER_ID"
v1_start_helper_to_file "$planning_tar_helper" "$V2_PRIVATE_DIR/planning-media-volume.tgz" 2>"$V2_PRIVATE_DIR/planning-tar.stderr" || \
  v1_die 8 backup-planning-media-tar planning-media retry-after-volume-check

v2_create_planning_helper manifest /usr/local/bin/planning-media-manifest --root /var/lib/crm/planning-media generate || \
  v1_die 8 backup-planning-media-manifest helper create-manifest-helper
planning_manifest_helper="$V1_LAST_HELPER_ID"
v1_start_helper_to_file "$planning_manifest_helper" "$V2_PRIVATE_DIR/planning-media-manifest.json" 2>"$V2_PRIVATE_DIR/planning-manifest.stderr" || \
  v1_die 8 backup-planning-media-manifest manifest inspect-app-image-and-data

mkdir -m 700 "$V2_PRIVATE_DIR/v2-package"
cp "$base_output/database.sql" "$V2_PRIVATE_DIR/v2-package/database.sql"
cp "$base_output/avatar-volume.tgz" "$V2_PRIVATE_DIR/v2-package/avatar-volume.tgz"
cp "$base_output/avatar-manifest.json" "$V2_PRIVATE_DIR/v2-package/avatar-manifest.json"
cp "$V2_PRIVATE_DIR/planning-media-volume.tgz" "$V2_PRIVATE_DIR/v2-package/planning-media-volume.tgz"
cp "$V2_PRIVATE_DIR/planning-media-manifest.json" "$V2_PRIVATE_DIR/v2-package/planning-media-manifest.json"

compose_sha="$(python3 -c 'import hashlib,sys; print(hashlib.sha256(open(sys.argv[1],"rb").read()).hexdigest())' "$V2_COMPOSE_FILE")"
app_digest="$(v1_docker image inspect "$V1_APP_IMAGE_ID" --format '{{.Id}}')"
pg_digest="$(v1_docker image inspect "$V1_PG_IMAGE_ID" --format '{{.Id}}')"
restore_tool_digest="$app_digest"
PYTHONPATH="$repo_root/scripts/lib" python3 - "$base_output/metadata.json" "$V2_IDENTITY_FILE" "$V2_PRIVATE_DIR/v2-package" "$compose_sha" "$app_digest" "$pg_digest" "$restore_tool_digest" "$V2_ORIGINAL_RUNNING" <<'PY'
import json, sys
from pathlib import Path
from planningbackup.package import V2_PAYLOADS, write_json, sha256_file
base_path, identity_path, package_path, compose_sha, app_digest, pg_digest, tool_digest, running = sys.argv[1:]
base = json.loads(Path(base_path).read_text(encoding="utf-8"))
identity = json.loads(Path(identity_path).read_text(encoding="utf-8"))
package = Path(package_path)
payloads = {filename: {"filename": filename, "sha256": sha256_file(package / filename), "size_bytes": (package / filename).stat().st_size} for filename in V2_PAYLOADS}
metadata = {
    "schema_version": 2,
    "created_at": base["created_at"],
    "source_deployment_identity": identity,
    "compose_file_sha256": compose_sha,
    "images": {"app": app_digest, "postgres": pg_digest, "restore_tool": tool_digest},
    "postgres_server_major": str(base["postgres_server_major"]),
    "app_was_running": running == "1",
    "database_counts": base["database_counts"],
    "payloads": payloads,
    "manifest_schemas": {"avatar": base["manifest_schema_version"], "planning_media": "planning-media-manifest-v1"},
    "package_writer_version": "planningbackup-v2",
}
write_json(package / "metadata.json", metadata)
PY
PYTHONPATH="$repo_root/scripts/lib" python3 -m planningbackup assemble-v2 \
  --output "$V2_PRIVATE_DIR/v2-assembled" \
  --database "$V2_PRIVATE_DIR/v2-package/database.sql" \
  --avatar-tar "$V2_PRIVATE_DIR/v2-package/avatar-volume.tgz" \
  --avatar-manifest "$V2_PRIVATE_DIR/v2-package/avatar-manifest.json" \
  --planning-tar "$V2_PRIVATE_DIR/v2-package/planning-media-volume.tgz" \
  --planning-manifest "$V2_PRIVATE_DIR/v2-package/planning-media-manifest.json" \
  --metadata "$V2_PRIVATE_DIR/v2-package/metadata.json" >/dev/null || v1_die 8 backup-package-verify package rebuild-backup

if [[ "$V2_ORIGINAL_RUNNING" -eq 1 ]]; then
  v1_start_and_health || v1_die 8 backup-state-restore app-health keep-package-unpublished
  V2_APP_REOPENED=1
fi

PYTHONPATH="$repo_root/scripts/lib" python3 -m planningbackup publish-v2 --input "$V2_PRIVATE_DIR/v2-assembled" --output "$V2_OUTPUT" >/dev/null || \
  v1_die 8 backup-publish output choose-new-backup-leaf
V2_APP_STOPPED=0
printf 'stage/backup-package-verify=pass\n'
printf 'stage/backup-publish=pass\n'
printf 'stage/complete=pass\n'
