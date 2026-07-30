#!/usr/bin/env bash
# Shared private runtime for backup-compose.sh and restore-compose.sh.

set -euo pipefail
umask 077

V1_OPS_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
V1_OPS_HELPER="$V1_OPS_ROOT/scripts/lib/v1-ops-package.py"
V1_STAGE=usage
V1_PINNED_HOST=
V1_ENGINE_ID=
V1_TARGET_HASH=
V1_LOCK_ID=
V1_FENCE_ID=
V1_OWNER_NONCE=
V1_OWNER_NONCE_HASH=
V1_LOCK_ID_HASH=
V1_APP_ID=
V1_PG_ID=
V1_AVATAR_VOLUME=
V1_PG_VOLUME=
V1_NETWORK=
V1_APP_STATE=
V1_APP_IMAGE_ID=
V1_PG_IMAGE_ID=
V1_APP_IMAGE_REF=
V1_PG_IMAGE_REF=
V1_EXPECTED_APP_DATABASE_URL=
V1_EXPECTED_PG_USER=
V1_EXPECTED_PG_DB=
V1_RUNTIME_ENV=
V1_PRIVATE_DIR=
V1_OPERATION_HELPERS=()
V1_LAST_HELPER_ID=
V1_CLEANUP_FAILED=0
V1_BREAK_STALE_LOCK=0

v1_status() {
  printf 'stage/%s=%s\n' "$1" "$2"
}

v1_die() {
  local code="$1" stage="$2" key="$3" action="$4"
  printf 'error code=%s stage=%s key=%s action=%s\n' "$code" "$stage" "$key" "$action" >&2
  exit "$code"
}

v1_require_commands() {
  local command
  V1_STAGE=dependency-check
  for command in bash docker python3 awk sed env mkdir mv rm sleep; do
    command -v "$command" >/dev/null 2>&1 || v1_die 6 dependency-check "$command" install-required-command
  done
  [[ -x "$V1_OPS_ROOT/scripts/production-preflight.sh" ]] || \
    v1_die 6 dependency-check production-preflight install-required-command
}

v1_validate_project() {
  [[ "$1" =~ ^[a-z0-9][a-z0-9_-]*$ ]] || v1_die 2 usage project-name use-lowercase-compose-name
}

v1_validate_common_files() {
  [[ -n "$V1_ENV_FILE" && -f "$V1_ENV_FILE" && ! -L "$V1_ENV_FILE" ]] || \
    v1_die 3 env-parse env-file provide-regular-env-file
  [[ -n "$V1_COMPOSE_FILE" && -f "$V1_COMPOSE_FILE" && ! -L "$V1_COMPOSE_FILE" ]] || \
    v1_die 5 compose-render compose-file provide-regular-compose-file
  [[ -n "$V1_DOCKER_CONTEXT" ]] || v1_die 2 endpoint-select docker-context provide-explicit-context
  [[ -n "$V1_PROJECT_NAME" ]] || v1_die 2 usage project-name provide-explicit-project
  v1_validate_project "$V1_PROJECT_NAME"
  V1_ENV_FILE="$(python3 -c 'import os,sys; print(os.path.realpath(sys.argv[1]))' "$V1_ENV_FILE")"
  V1_COMPOSE_FILE="$(python3 -c 'import os,sys; print(os.path.realpath(sys.argv[1]))' "$V1_COMPOSE_FILE")"
}

v1_validate_no_selector_env() {
  local key value
  for key in DOCKER_HOST DOCKER_CONTEXT DOCKER_TLS_VERIFY DOCKER_CERT_PATH; do
    value="${!key-}"
    [[ -z "$value" ]] || v1_die 2 endpoint-select "$key" clear-docker-selector-environment
  done
}

v1_docker_client() {
  env -u DOCKER_HOST -u DOCKER_CONTEXT -u DOCKER_TLS_VERIFY -u DOCKER_CERT_PATH docker "$@"
}

v1_docker() {
  [[ -n "$V1_PINNED_HOST" ]] || v1_die 5 endpoint-select pinned-host internal-endpoint-error
  if [[ -n "${V1_PRIVATE_DIR:-}" && -d "$V1_PRIVATE_DIR" ]]; then
    v1_docker_client --host "$V1_PINNED_HOST" "$@" 2>>"$V1_PRIVATE_DIR/docker.stderr"
  else
    v1_docker_client --host "$V1_PINNED_HOST" "$@" 2>/dev/null
  fi
}

v1_compose() {
  (
    unset COMPOSE_FILE COMPOSE_PROJECT_NAME COMPOSE_PROFILES COMPOSE_ENV_FILES COMPOSE_DISABLE_ENV_FILE
    CRM_ENV_FILE="$V1_ENV_FILE" v1_docker compose \
      --env-file "$V1_ENV_FILE" -f "$V1_COMPOSE_FILE" -p "$V1_PROJECT_NAME" "$@"
  )
}

v1_pin_endpoint() {
  local endpoint socket canonical
  V1_STAGE=endpoint-select
  v1_validate_no_selector_env
  endpoint="$(v1_docker_client context inspect "$V1_DOCKER_CONTEXT" --format '{{.Endpoints.docker.Host}}' 2>/dev/null)" || \
    v1_die 2 endpoint-select docker-context use-existing-local-context
  [[ "$endpoint" == unix:///* ]] || v1_die 2 endpoint-select docker-context use-local-unix-context
  socket="${endpoint#unix://}"
  [[ -S "$socket" ]] || v1_die 2 endpoint-select docker-context use-existing-unix-socket
  canonical="$(python3 -c 'import os,sys; print(os.path.realpath(sys.argv[1]))' "$socket")" || \
    v1_die 2 endpoint-select docker-context resolve-local-unix-socket
  [[ "$canonical" == /* && -S "$canonical" ]] || v1_die 2 endpoint-select docker-context use-absolute-unix-socket
  V1_PINNED_HOST="unix://$canonical"
  V1_ENGINE_ID="$(v1_docker info --format '{{.ID}}' 2>/dev/null)" || \
    v1_die 5 target-identity docker-engine-id verify-local-engine
  [[ -n "$V1_ENGINE_ID" ]] || v1_die 5 target-identity docker-engine-id verify-local-engine
}

v1_render_and_image_safety() {
  local render app_volumes images runtime_identity env_pg_user env_pg_db
  V1_STAGE=compose-render
  render="$(v1_compose config --format json 2>/dev/null)" || \
    v1_die 5 compose-render compose-config fix-compose-render
  images="$(printf '%s' "$render" | python3 "$V1_OPS_HELPER" render-info)" || \
    v1_die 5 compose-render compose-identity fix-fixed-services-volumes
  V1_APP_IMAGE_REF="$(printf '%s\n' "$images" | sed -n '1p')"
  V1_PG_IMAGE_REF="$(printf '%s\n' "$images" | sed -n '2p')"
  [[ -n "$V1_APP_IMAGE_REF" && -n "$V1_PG_IMAGE_REF" ]] || v1_die 5 compose-render compose-images fix-compose-images
  runtime_identity="$(printf '%s' "$render" | python3 -c '
import json,sys
model=json.load(sys.stdin)
services=model.get("services") or {}
app_env=(services.get("app") or {}).get("environment") or {}
pg_env=(services.get("postgres") or {}).get("environment") or {}
for value in (app_env.get("DATABASE_URL"), pg_env.get("POSTGRES_USER"), pg_env.get("POSTGRES_DB")):
    if not isinstance(value, str) or not value or "\n" in value or "\r" in value:
        raise SystemExit(1)
    print(value)
' 2>/dev/null)" || v1_die 5 compose-render runtime-env fix-managed-compose-environment
  V1_EXPECTED_APP_DATABASE_URL="$(printf '%s\n' "$runtime_identity" | sed -n '1p')"
  V1_EXPECTED_PG_USER="$(printf '%s\n' "$runtime_identity" | sed -n '2p')"
  V1_EXPECTED_PG_DB="$(printf '%s\n' "$runtime_identity" | sed -n '3p')"
  env_pg_user="$(python3 "$V1_OPS_HELPER" env-get --env-file "$V1_ENV_FILE" --key POSTGRES_USER 2>/dev/null)" || \
    v1_die 3 env-parse POSTGRES_USER fix-managed-postgres-env
  env_pg_db="$(python3 "$V1_OPS_HELPER" env-get --env-file "$V1_ENV_FILE" --key POSTGRES_DB 2>/dev/null)" || \
    v1_die 3 env-parse POSTGRES_DB fix-managed-postgres-env
  [[ "$V1_EXPECTED_PG_USER" == "$env_pg_user" && "$V1_EXPECTED_PG_DB" == "$env_pg_db" ]] || \
    v1_die 5 compose-render postgres-env select-same-canonical-env-file
  V1_STAGE=image-safety
  V1_APP_IMAGE_ID="$(v1_docker image inspect "$V1_APP_IMAGE_REF" --format '{{.Id}}' 2>/dev/null)" || \
    v1_die 5 image-safety app-image build-image-before-operation
  V1_PG_IMAGE_ID="$(v1_docker image inspect "$V1_PG_IMAGE_REF" --format '{{.Id}}' 2>/dev/null)" || \
    v1_die 5 image-safety postgres-image pull-image-before-operation
  app_volumes="$(v1_docker image inspect "$V1_APP_IMAGE_ID" --format '{{json .Config.Volumes}}' 2>/dev/null)" || \
    v1_die 5 image-safety app-image inspect-local-image
  [[ "$app_volumes" == "null" || "$app_volumes" == "{}" ]] || \
    v1_die 5 image-safety app-image use-volume-free-app-image
  V1_TARGET_HASH="$(python3 -c 'import hashlib,sys; b=("schema=v1\\0docker-engine-id="+sys.argv[1]+"\\0project="+sys.argv[2]+"\\0services=app,postgres\\0volumes=avatar_data,pgdata").encode(); print(hashlib.sha256(b).hexdigest())' "$V1_ENGINE_ID" "$V1_PROJECT_NAME")"
}

v1_random_nonce() {
  python3 -c 'import secrets; print(secrets.token_hex(32))'
}

v1_sha_text() {
  python3 -c 'import hashlib,sys; print(hashlib.sha256(sys.argv[1].encode()).hexdigest())' "$1"
}

v1_inspect_invariant() {
  local id="$1" name="$2"
  shift 2
  local args=() label
  for label in "$@"; do args+=(--label "$label"); done
  v1_docker container inspect "$id" 2>/dev/null | \
    python3 "$V1_OPS_HELPER" container-invariant --container-id "$id" --name "$name" "${args[@]}"
}

v1_candidate_metadata() {
  local candidate="$1" name="$2" snapshot
  snapshot="$(v1_docker container inspect "$candidate" 2>/dev/null)" || return 1
  printf '%s' "$snapshot" | python3 -c '
import json
import re
import sys

candidate, name, target = sys.argv[1:]
try:
    items = json.load(sys.stdin)
    if not isinstance(items, list) or len(items) != 1:
        raise ValueError
    item = items[0]
    labels = (item.get("Config") or {}).get("Labels") or {}
    state = item.get("State") or {}
    pid = labels.get("com.photographer-crm.ops.owner-pid", "")
    started = labels.get("com.photographer-crm.ops.owner-started-at", "")
    nonce = labels.get("com.photographer-crm.ops.owner-nonce-sha256", "")
    image = item.get("Image", "")
    valid = (
        item.get("Id") == candidate
        and item.get("Name") == "/" + name
        and labels.get("com.photographer-crm.ops") == "true"
        and labels.get("com.photographer-crm.ops.kind") == "lock"
        and labels.get("com.photographer-crm.ops.target-sha256") == target
        and re.fullmatch(r"[0-9]+", pid)
        and re.fullmatch(r"[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z", started)
        and re.fullmatch(r"[0-9a-f]{64}", nonce)
        and isinstance(image, str) and bool(image)
        and state.get("Status") == "created"
        and state.get("Running") is False
        and state.get("StartedAt") in ("", "0001-01-01T00:00:00Z")
        and (item.get("HostConfig") or {}).get("NetworkMode") == "none"
        and item.get("Mounts") == []
    )
    if not valid:
        raise ValueError
except (AttributeError, TypeError, ValueError, json.JSONDecodeError):
    raise SystemExit(1)
print(pid)
print(started)
print(nonce)
print(image)
' "$candidate" "$name" "$V1_TARGET_HASH"
}

v1_candidate_is_alone() {
  local candidate="$1" helper_ids id
  helper_ids="$(v1_docker ps -aq --no-trunc --filter "label=com.photographer-crm.ops.target-sha256=$V1_TARGET_HASH" --filter label=com.photographer-crm.ops=true 2>/dev/null)" || return 1
  while IFS= read -r id; do
    [[ -z "$id" || "$id" == "$candidate" ]] && continue
    return 1
  done <<< "$helper_ids"
  return 0
}

v1_candidate_break() {
  local name="$1" candidate metadata pid image current
  [[ "$V1_BREAK_STALE_LOCK" -eq 1 ]] || return 1
  candidate="$(v1_docker container inspect "$name" --format '{{.Id}}' 2>/dev/null)" || return 1
  [[ -n "$candidate" ]] || return 1
  metadata="$(v1_candidate_metadata "$candidate" "$name")" || return 1
  pid="$(printf '%s\n' "$metadata" | sed -n '1p')"
  image="$(printf '%s\n' "$metadata" | sed -n '4p')"
  [[ "$pid" =~ ^[0-9]+$ && -n "$image" ]] || return 1
  [[ "$(v1_docker image inspect "$image" --format '{{json .Config.Volumes}}' 2>/dev/null)" =~ ^(null|\{\})$ ]] || return 1
  kill -0 "$pid" 2>/dev/null && return 1
  v1_candidate_is_alone "$candidate" || return 1
  current="$(v1_docker container inspect "$name" --format '{{.Id}}' 2>/dev/null)" || return 1
  [[ "$current" == "$candidate" ]] || return 1
  metadata="$(v1_candidate_metadata "$candidate" "$name")" || return 1
  [[ "$(printf '%s\n' "$metadata" | sed -n '1p')" == "$pid" && "$(printf '%s\n' "$metadata" | sed -n '4p')" == "$image" ]] || return 1
  kill -0 "$pid" 2>/dev/null && return 1
  v1_candidate_is_alone "$candidate" || return 1
  v1_docker rm -v "$candidate" >/dev/null 2>&1 || return 1
  v1_docker container inspect "$candidate" >/dev/null 2>&1 && return 1
  return 0
}

v1_acquire_lock() {
  local name created started label_args
  V1_STAGE=lock-acquire
  name="photographer-private-crm-ops-lock-$V1_TARGET_HASH"
  V1_OWNER_NONCE="$(v1_random_nonce)"
  V1_OWNER_NONCE_HASH="$(v1_sha_text "$V1_OWNER_NONCE")"
  started="$(python3 -c 'from datetime import datetime,timezone; print(datetime.now(timezone.utc).isoformat(timespec="seconds").replace("+00:00","Z"))')"
  label_args=(
    --label com.photographer-crm.ops=true
    --label "com.photographer-crm.ops.target-sha256=$V1_TARGET_HASH"
    --label com.photographer-crm.ops.kind=lock
    --label "com.photographer-crm.ops.owner-pid=$$"
    --label "com.photographer-crm.ops.owner-started-at=$started"
    --label "com.photographer-crm.ops.owner-nonce-sha256=$V1_OWNER_NONCE_HASH"
  )
  created="$(v1_docker create --network none --name "$name" "${label_args[@]}" "$V1_APP_IMAGE_ID" 2>/dev/null)" || {
    v1_candidate_break "$name" || v1_die 7 lock-acquire lock busy-or-not-authorized
    created="$(v1_docker create --network none --name "$name" "${label_args[@]}" "$V1_APP_IMAGE_ID" 2>/dev/null)" || \
      v1_die 7 lock-acquire lock lost-break-race
  }
  V1_LOCK_ID="$created"
  [[ -n "$V1_LOCK_ID" ]] || v1_die 7 lock-post-inspect lock-id cleanup-by-returned-id
  V1_STAGE=lock-post-inspect
  if ! v1_inspect_invariant "$V1_LOCK_ID" "$name" \
      com.photographer-crm.ops=true \
      "com.photographer-crm.ops.target-sha256=$V1_TARGET_HASH" \
      com.photographer-crm.ops.kind=lock \
      "com.photographer-crm.ops.owner-pid=$$" \
      "com.photographer-crm.ops.owner-started-at=$started" \
      "com.photographer-crm.ops.owner-nonce-sha256=$V1_OWNER_NONCE_HASH"; then
    if v1_docker rm -v "$V1_LOCK_ID" >/dev/null 2>&1 && \
        ! v1_docker container inspect "$V1_LOCK_ID" >/dev/null 2>&1; then
      V1_LOCK_ID=
    else
      V1_CLEANUP_FAILED=1
    fi
    v1_die 7 lock-post-inspect lock-invariant inspect-lock-metadata
  fi
  V1_LOCK_ID_HASH="$(v1_sha_text "$V1_LOCK_ID")"
}

v1_recheck_lock() {
  local nonce target kind
  [[ -n "$V1_LOCK_ID" ]] || return 1
  nonce="$(v1_docker container inspect "$V1_LOCK_ID" --format '{{index .Config.Labels "com.photographer-crm.ops.owner-nonce-sha256"}}' 2>/dev/null)" || return 1
  target="$(v1_docker container inspect "$V1_LOCK_ID" --format '{{index .Config.Labels "com.photographer-crm.ops.target-sha256"}}' 2>/dev/null)" || return 1
  kind="$(v1_docker container inspect "$V1_LOCK_ID" --format '{{index .Config.Labels "com.photographer-crm.ops.kind"}}' 2>/dev/null)" || return 1
  [[ "$nonce" == "$V1_OWNER_NONCE_HASH" && "$target" == "$V1_TARGET_HASH" && "$kind" == lock ]]
}

v1_preflight() {
  V1_STAGE=config-matrix
  "$V1_OPS_ROOT/scripts/production-preflight.sh" \
    --mode compose-managed-db \
    --seed-state initialized \
    --env-file "$V1_ENV_FILE" \
    --compose-file "$V1_COMPOSE_FILE" \
    --docker-context "$V1_DOCKER_CONTEXT" \
    --pinned-host "$V1_PINNED_HOST" \
    --project-name "$V1_PROJECT_NAME" || exit $?
  v1_recheck_lock || v1_die 7 lock-acquire lock-generation lock-lost-before-target-lookup
}

v1_one_id() {
  local kind="$1" value="$2" ids count
  if [[ "$kind" == container ]]; then
    ids="$(v1_docker ps -aq --filter "label=com.docker.compose.project=$V1_PROJECT_NAME" --filter "label=com.docker.compose.service=$value")"
  else
    ids="$(v1_docker volume ls -q --filter "label=com.docker.compose.project=$V1_PROJECT_NAME" --filter "label=com.docker.compose.volume=$value")"
  fi
  count="$(printf '%s\n' "$ids" | awk 'NF {n++} END {print n+0}')"
  [[ "$count" -eq 1 ]] || return 1
  printf '%s' "$ids"
}

v1_container_env_matches() {
  local id="$1"
  shift
  v1_docker container inspect "$id" --format '{{json .Config.Env}}' 2>/dev/null | python3 -c '
import json
import sys

try:
    actual = json.load(sys.stdin)
    if not isinstance(actual, list) or not all(isinstance(item, str) for item in actual):
        raise ValueError
    for expected in sys.argv[1:]:
        key, value = expected.split("=", 1)
        matches = [item.split("=", 1)[1] for item in actual if item.split("=", 1)[0] == key and "=" in item]
        if matches != [value]:
            raise ValueError
except (IndexError, TypeError, ValueError, json.JSONDecodeError):
    raise SystemExit(1)
' "$@"
}

v1_resolve_runtime() {
  local app_mount pg_mount runtime_app_image runtime_pg_image avatar_mountpoint pg_mountpoint
  V1_STAGE=target-identity
  V1_APP_ID="$(v1_one_id container app)" || v1_die 5 target-identity app require-one-existing-app
  V1_PG_ID="$(v1_one_id container postgres)" || v1_die 5 target-identity postgres require-one-existing-postgres
  V1_AVATAR_VOLUME="$(v1_one_id volume avatar_data)" || v1_die 5 target-identity avatar-volume require-one-existing-volume
  V1_PG_VOLUME="$(v1_one_id volume pgdata)" || v1_die 5 target-identity pg-volume require-one-existing-volume
  V1_APP_STATE="$(v1_docker container inspect "$V1_APP_ID" | python3 "$V1_OPS_HELPER" app-state 2>/dev/null)" || \
    v1_die 5 target-identity app-state use-running-or-exited-app
  runtime_app_image="$(v1_docker container inspect "$V1_APP_ID" --format '{{.Image}}')"
  runtime_pg_image="$(v1_docker container inspect "$V1_PG_ID" --format '{{.Image}}')"
  app_mount="$(v1_docker container inspect "$V1_APP_ID" --format '{{range .Mounts}}{{if eq .Destination "/var/lib/crm/avatars"}}{{.Name}}{{end}}{{end}}')"
  pg_mount="$(v1_docker container inspect "$V1_PG_ID" --format '{{range .Mounts}}{{if eq .Destination "/var/lib/postgresql/data"}}{{.Name}}{{end}}{{end}}')"
  [[ "$runtime_app_image" == "$V1_APP_IMAGE_ID" && "$runtime_pg_image" == "$V1_PG_IMAGE_ID" ]] || \
    v1_die 5 target-identity image source-runtime-mismatch
  [[ "$app_mount" == "$V1_AVATAR_VOLUME" && "$pg_mount" == "$V1_PG_VOLUME" ]] || \
    v1_die 5 target-identity mount source-runtime-mismatch
  [[ -n "$V1_EXPECTED_APP_DATABASE_URL" && -n "$V1_EXPECTED_PG_USER" && -n "$V1_EXPECTED_PG_DB" ]] || \
    v1_die 5 target-identity runtime-env missing-rendered-runtime-identity
  v1_container_env_matches "$V1_APP_ID" "DATABASE_URL=$V1_EXPECTED_APP_DATABASE_URL" || \
    v1_die 5 target-identity app-env recreate-app-from-canonical-env
  v1_container_env_matches "$V1_PG_ID" "POSTGRES_USER=$V1_EXPECTED_PG_USER" "POSTGRES_DB=$V1_EXPECTED_PG_DB" || \
    v1_die 5 target-identity postgres-env recreate-postgres-from-canonical-env
  V1_NETWORK="$(v1_docker network ls -q --filter "label=com.docker.compose.project=$V1_PROJECT_NAME" | awk 'NF {print; exit}')"
  [[ -n "$V1_NETWORK" ]] || v1_die 5 target-identity network require-compose-network
  avatar_mountpoint="$(v1_docker volume inspect "$V1_AVATAR_VOLUME" --format '{{.Mountpoint}}')"
  pg_mountpoint="$(v1_docker volume inspect "$V1_PG_VOLUME" --format '{{.Mountpoint}}')"
  if python3 -c 'import os,sys; p=os.path.realpath(sys.argv[1]); roots=[os.path.realpath(x) for x in sys.argv[2:]]; raise SystemExit(0 if any(p==r or p.startswith(r+os.sep) or r.startswith(p+os.sep) for r in roots) else 1)' "$V1_PATH_AUTHORITY" "$avatar_mountpoint" "$pg_mountpoint"; then
    v1_die 5 target-identity path source-volume-overlap
  fi
}

v1_acquire_fence() {
  local name created
  V1_STAGE=helper-fence
  name="photographer-private-crm-ops-helper-$V1_TARGET_HASH"
  created="$(v1_docker create --network none --name "$name" \
    --label com.photographer-crm.ops=true \
    --label "com.photographer-crm.ops.target-sha256=$V1_TARGET_HASH" \
    --label com.photographer-crm.ops.kind=fence \
    --label "com.photographer-crm.ops.lock-id-sha256=$V1_LOCK_ID_HASH" \
    --label "com.photographer-crm.ops.owner-nonce-sha256=$V1_OWNER_NONCE_HASH" \
    "$V1_APP_IMAGE_ID" 2>/dev/null)" || v1_die 7 helper-fence helper busy
  V1_FENCE_ID="$created"
  if ! v1_inspect_invariant "$V1_FENCE_ID" "$name" \
      com.photographer-crm.ops=true \
      "com.photographer-crm.ops.target-sha256=$V1_TARGET_HASH" \
      com.photographer-crm.ops.kind=fence \
      "com.photographer-crm.ops.lock-id-sha256=$V1_LOCK_ID_HASH" \
      "com.photographer-crm.ops.owner-nonce-sha256=$V1_OWNER_NONCE_HASH"; then
    if v1_docker rm -v "$V1_FENCE_ID" >/dev/null 2>&1 && \
        ! v1_docker container inspect "$V1_FENCE_ID" >/dev/null 2>&1; then
      V1_FENCE_ID=
    else
      V1_CLEANUP_FAILED=1
    fi
    v1_die 7 helper-fence helper-invariant inspect-helper-metadata
  fi
  v1_recheck_generation || v1_die 7 helper-fence generation lock-or-fence-lost
}

v1_recheck_generation() {
  local lock_hash nonce
  v1_recheck_lock || return 1
  [[ -n "$V1_FENCE_ID" ]] || return 1
  lock_hash="$(v1_docker container inspect "$V1_FENCE_ID" --format '{{index .Config.Labels "com.photographer-crm.ops.lock-id-sha256"}}' 2>/dev/null)" || return 1
  nonce="$(v1_docker container inspect "$V1_FENCE_ID" --format '{{index .Config.Labels "com.photographer-crm.ops.owner-nonce-sha256"}}' 2>/dev/null)" || return 1
  [[ "$lock_hash" == "$V1_LOCK_ID_HASH" && "$nonce" == "$V1_OWNER_NONCE_HASH" ]]
}

v1_prepare_runtime_env() {
  V1_RUNTIME_ENV="$V1_PRIVATE_DIR/runtime.env"
  python3 "$V1_OPS_HELPER" env-runtime --env-file "$V1_ENV_FILE" --output "$V1_RUNTIME_ENV" || \
    v1_die 3 env-parse postgres-env fix-managed-postgres-env
}

v1_create_helper() {
  local kind="$1" purpose="$2" entrypoint="$3"
  shift 3
  local name created id label_kind
  label_kind="$kind"
  # Data helpers must read 0600 staged artifacts and cover every volume entry,
  # including root-owned files.  The application containers remain unprivileged.
  name="photographer-private-crm-ops-${kind}-${V1_TARGET_HASH:0:16}-${purpose}-${#V1_OPERATION_HELPERS[@]}"
  created="$(v1_docker create --name "$name" --network "$V1_NETWORK" --user 0:0 \
    --env-file "$V1_RUNTIME_ENV" \
    --mount "type=volume,source=$V1_AVATAR_VOLUME,destination=/var/lib/crm/avatars" \
    --label com.photographer-crm.ops=true \
    --label "com.photographer-crm.ops.target-sha256=$V1_TARGET_HASH" \
    --label "com.photographer-crm.ops.kind=$label_kind" \
    --label "com.photographer-crm.ops.lock-id-sha256=$V1_LOCK_ID_HASH" \
    --label "com.photographer-crm.ops.owner-nonce-sha256=$V1_OWNER_NONCE_HASH" \
    --entrypoint "$entrypoint" "$V1_APP_IMAGE_ID" "$@" 2>/dev/null)" || return 1
  V1_OPERATION_HELPERS+=("$created")
  id="$(v1_docker container inspect "$created" --format '{{.Id}}' 2>/dev/null)" || return 1
  [[ "$id" == "$created" ]] || return 1
  V1_LAST_HELPER_ID="$created"
}

v1_start_helper_to_file() {
  local id="$1" output="$2"
  v1_recheck_generation || return 1
  local nonce lock_hash status
  nonce="$(v1_docker container inspect "$id" --format '{{index .Config.Labels "com.photographer-crm.ops.owner-nonce-sha256"}}' 2>/dev/null)" || return 1
  lock_hash="$(v1_docker container inspect "$id" --format '{{index .Config.Labels "com.photographer-crm.ops.lock-id-sha256"}}' 2>/dev/null)" || return 1
  status="$(v1_docker container inspect "$id" --format '{{.State.Status}}' 2>/dev/null)" || return 1
  [[ "$nonce" == "$V1_OWNER_NONCE_HASH" && "$lock_hash" == "$V1_LOCK_ID_HASH" && "$status" == created ]] || return 1
  v1_docker start -a "$id" >"$output"
}

v1_remove_helper() {
  local id="$1" volume volumes
  volumes="$(v1_docker container inspect "$id" \
    --format '{{range .Mounts}}{{if eq .Type "volume"}}{{println .Name}}{{end}}{{end}}')" || return 1
  v1_docker rm -v "$id" >/dev/null 2>&1 || return 1
  ! v1_docker container inspect "$id" >/dev/null 2>&1 || return 1
  while IFS= read -r volume; do
    [[ -z "$volume" || "$volume" == "$V1_AVATAR_VOLUME" || "$volume" == "$V1_PG_VOLUME" ]] && continue
    ! v1_docker volume inspect "$volume" >/dev/null 2>&1 || return 1
  done <<<"$volumes"
  return 0
}

v1_stop_app() {
  v1_recheck_generation || return 1
  v1_compose stop app >/dev/null 2>&1 || return 1
  [[ "$(v1_docker container inspect "$V1_APP_ID" | python3 "$V1_OPS_HELPER" app-state 2>/dev/null)" == exited ]]
}

v1_start_app() {
  v1_recheck_generation || return 1
  v1_compose start app >/dev/null 2>&1 || return 1
}

v1_wait_app_health() {
  local health attempts=0
  v1_recheck_generation || return 1
  while (( attempts < 60 )); do
    health="$(v1_docker container inspect "$V1_APP_ID" --format '{{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}}' 2>/dev/null)" || return 1
    [[ "$health" == healthy ]] && return 0
    [[ "$health" == unhealthy ]] && return 1
    attempts=$((attempts + 1))
    sleep 1
  done
  return 1
}

v1_start_and_health() {
  v1_start_app && v1_wait_app_health
}

v1_failure_stop() {
  v1_recheck_generation || return 1
  v1_compose stop app >/dev/null 2>&1 || return 1
  [[ "$(v1_docker container inspect "$V1_APP_ID" | python3 "$V1_OPS_HELPER" app-state 2>/dev/null)" == exited ]]
}

v1_cleanup() {
  local id
  set +u
  for id in "${V1_OPERATION_HELPERS[@]}"; do
    [[ -n "$id" ]] && v1_remove_helper "$id" || V1_CLEANUP_FAILED=1
  done
  set -u
  if [[ "$V1_CLEANUP_FAILED" -eq 0 && -n "$V1_FENCE_ID" ]]; then
    if v1_recheck_lock && v1_remove_helper "$V1_FENCE_ID"; then
      V1_FENCE_ID=
    else
      V1_CLEANUP_FAILED=1
    fi
  fi
  if [[ "$V1_CLEANUP_FAILED" -eq 0 && -z "$V1_FENCE_ID" && -n "$V1_LOCK_ID" ]]; then
    if v1_recheck_lock; then
      if v1_docker rm -v "$V1_LOCK_ID" >/dev/null 2>&1 && \
          ! v1_docker container inspect "$V1_LOCK_ID" >/dev/null 2>&1; then
        V1_LOCK_ID=
      else
        V1_CLEANUP_FAILED=1
      fi
    else
      V1_CLEANUP_FAILED=1
    fi
  fi
  if [[ -n "$V1_PRIVATE_DIR" && -d "$V1_PRIVATE_DIR" ]]; then
    rm -rf "$V1_PRIVATE_DIR" || V1_CLEANUP_FAILED=1
  fi
  return "$V1_CLEANUP_FAILED"
}
