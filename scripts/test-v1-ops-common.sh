#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
# shellcheck source=scripts/lib/v1-ops-common.sh
source "$repo_root/scripts/lib/v1-ops-common.sh"

work_dir="$(mktemp -d)"
trap 'rm -rf "$work_dir"' EXIT
case_count=0

pass_case() {
  case_count=$((case_count + 1))
}

test_cleanup_helper_failure_keeps_fence_and_lock() {
  local calls="$work_dir/cleanup-helper-failure.calls"
  : >"$calls"
  V1_OPERATION_HELPERS=(operation-id)
  V1_FENCE_ID=fence-id
  V1_LOCK_ID=lock-id
  V1_PRIVATE_DIR=
  V1_CLEANUP_FAILED=0
  v1_remove_helper() {
    printf 'remove:%s\n' "$1" >>"$calls"
    [[ "$1" != operation-id ]]
  }
  v1_recheck_lock() { return 0; }
  v1_docker() {
    printf 'docker:%s\n' "$*" >>"$calls"
    return 0
  }

  if v1_cleanup; then
    printf 'helper cleanup failure was reported as success\n' >&2
    return 1
  fi
  set -e
  grep -qx 'remove:operation-id' "$calls"
  [[ "$(wc -l <"$calls")" -eq 1 ]]
  [[ "$V1_FENCE_ID" == fence-id && "$V1_LOCK_ID" == lock-id ]]
  pass_case
}

test_cleanup_fence_failure_keeps_primary_lock() {
  local calls="$work_dir/cleanup-fence-failure.calls"
  : >"$calls"
  V1_OPERATION_HELPERS=(operation-id)
  V1_FENCE_ID=fence-id
  V1_LOCK_ID=lock-id
  V1_PRIVATE_DIR=
  V1_CLEANUP_FAILED=0
  v1_remove_helper() {
    printf 'remove:%s\n' "$1" >>"$calls"
    [[ "$1" != fence-id ]]
  }
  v1_recheck_lock() { return 0; }
  v1_docker() {
    printf 'docker:%s\n' "$*" >>"$calls"
    return 0
  }

  if v1_cleanup; then
    printf 'fence cleanup failure was reported as success\n' >&2
    return 1
  fi
  set -e
  grep -qx 'remove:operation-id' "$calls"
  grep -qx 'remove:fence-id' "$calls"
  ! grep -q '^docker:rm -v lock-id$' "$calls"
  [[ "$V1_FENCE_ID" == fence-id && "$V1_LOCK_ID" == lock-id ]]
  pass_case
}

test_operation_helper_records_create_id_before_inspect() {
  V1_TARGET_HASH=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
  V1_NETWORK=synthetic-network
  V1_RUNTIME_ENV="$work_dir/runtime.env"
  V1_AVATAR_VOLUME=synthetic-avatar
  V1_LOCK_ID_HASH=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
  V1_OWNER_NONCE_HASH=cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc
  V1_APP_IMAGE_ID=sha256:synthetic-app
  V1_OPERATION_HELPERS=()
  V1_LAST_HELPER_ID=
  v1_docker() {
    if [[ "$1" == create ]]; then
      printf '%s\n' immutable-helper-id
      return 0
    fi
    return 1
  }

  if v1_create_helper backup manifest /bin/true; then
    printf 'helper inspect fault was reported as success\n' >&2
    return 1
  fi
  [[ "${#V1_OPERATION_HELPERS[@]}" -eq 1 ]]
  [[ "${V1_OPERATION_HELPERS[0]}" == immutable-helper-id ]]
  pass_case
}

test_lock_records_create_id_before_inspect() {
  local output="$work_dir/lock-inspect-fault.out"
  set +e
  (
    source "$repo_root/scripts/lib/v1-ops-common.sh"
    V1_TARGET_HASH=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
    V1_APP_IMAGE_ID=sha256:synthetic-app
    v1_random_nonce() { printf '%s\n' synthetic-nonce; }
    v1_docker() {
      if [[ "$1" == create ]]; then
        printf '%s\n' immutable-lock-id
        return 0
      fi
      return 1
    }
    trap 'printf "lock-id=%s\n" "$V1_LOCK_ID"' EXIT
    v1_acquire_lock
  ) >"$output" 2>/dev/null
  local observed=$?
  set -e
  [[ "$observed" -ne 0 ]]
  grep -qx 'lock-id=immutable-lock-id' "$output"
  pass_case
}

test_fence_records_create_id_before_inspect() {
  local output="$work_dir/fence-inspect-fault.out"
  set +e
  (
    source "$repo_root/scripts/lib/v1-ops-common.sh"
    V1_TARGET_HASH=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
    V1_LOCK_ID_HASH=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
    V1_OWNER_NONCE_HASH=cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc
    V1_APP_IMAGE_ID=sha256:synthetic-app
    v1_docker() {
      if [[ "$1" == create ]]; then
        printf '%s\n' immutable-fence-id
        return 0
      fi
      return 1
    }
    trap 'printf "fence-id=%s\n" "$V1_FENCE_ID"' EXIT
    v1_acquire_fence
  ) >"$output" 2>/dev/null
  local observed=$?
  set -e
  [[ "$observed" -ne 0 ]]
  grep -qx 'fence-id=immutable-fence-id' "$output"
  pass_case
}

write_candidate_inspect() {
  local network_mode="$1"
  python3 - "$network_mode" <<'PY'
import json
import sys

network_mode = sys.argv[1]
print(json.dumps([{
    "Id": "candidate-id",
    "Name": "/synthetic-lock",
    "Image": "sha256:stale-image",
    "Config": {"Labels": {
        "com.photographer-crm.ops": "true",
        "com.photographer-crm.ops.kind": "lock",
        "com.photographer-crm.ops.target-sha256": "target-hash",
        "com.photographer-crm.ops.owner-pid": "99999999",
        "com.photographer-crm.ops.owner-started-at": "2026-07-27T00:00:00Z",
        "com.photographer-crm.ops.owner-nonce-sha256": "d" * 64,
    }},
    "State": {"Status": "created", "Running": False, "StartedAt": ""},
    "HostConfig": {"NetworkMode": network_mode},
    "Mounts": [],
}]))
PY
}

test_stale_break_rejects_incomplete_candidate_invariant() {
  local calls="$work_dir/stale-invalid.calls"
  : >"$calls"
  V1_BREAK_STALE_LOCK=1
  V1_TARGET_HASH=target-hash
  v1_docker() {
    printf '%s\n' "$*" >>"$calls"
    if [[ "$1 $2 $3" == 'container inspect synthetic-lock' && "${4-}" == --format ]]; then
      printf '%s\n' candidate-id
      return 0
    fi
    if [[ "$1 $2 $3" == 'container inspect candidate-id' && "${4-}" == --format ]]; then
      case "${5-}" in
        '{{json .Config.Labels}}') printf '%s\n' '{"com.photographer-crm.ops":"true","com.photographer-crm.ops.kind":"lock","com.photographer-crm.ops.target-sha256":"target-hash","com.photographer-crm.ops.owner-pid":"99999999","com.photographer-crm.ops.owner-started-at":"2026-07-27T00:00:00Z","com.photographer-crm.ops.owner-nonce-sha256":"dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"}' ;;
        '{{index .Config.Labels "com.photographer-crm.ops.owner-pid"}}') printf '%s\n' 99999999 ;;
      esac
      return 0
    fi
    if [[ "$1 $2 $3" == 'container inspect candidate-id' ]]; then
      write_candidate_inspect bridge
      return 0
    fi
    if [[ "$1 $2" == 'ps -aq' ]]; then
      printf '%s\n' candidate-id
      return 0
    fi
    if [[ "$1" == rm ]]; then
      return 0
    fi
    return 1
  }

  if v1_candidate_break synthetic-lock; then
    printf 'stale candidate with default network was removed\n' >&2
    return 1
  fi
  ! grep -q '^rm ' "$calls"
  pass_case
}

test_stale_break_rechecks_name_before_delete() {
  local calls="$work_dir/stale-race.calls"
  local count_file="$work_dir/stale-race.count"
  : >"$calls"
  printf '%s\n' 0 >"$count_file"
  V1_BREAK_STALE_LOCK=1
  V1_TARGET_HASH=target-hash
  v1_docker() {
    printf '%s\n' "$*" >>"$calls"
    if [[ "$1 $2 $3" == 'container inspect synthetic-lock' && "${4-}" == --format ]]; then
      local count
      count="$(<"$count_file")"
      count=$((count + 1))
      printf '%s\n' "$count" >"$count_file"
      if [[ "$count" -eq 1 ]]; then
        printf '%s\n' candidate-id
      else
        printf '%s\n' replacement-id
      fi
      return 0
    fi
    if [[ "$1 $2 $3" == 'container inspect candidate-id' && "${4-}" == --format ]]; then
      case "${5-}" in
        '{{index .Config.Labels "com.photographer-crm.ops.owner-pid"}}') printf '%s\n' 99999999 ;;
        '{{.Image}}') printf '%s\n' sha256:stale-image ;;
      esac
      return 0
    fi
    if [[ "$1 $2 $3" == 'container inspect candidate-id' ]]; then
      write_candidate_inspect none
      return 0
    fi
    if [[ "$1 $2 $3" == 'image inspect sha256:stale-image' ]]; then
      printf '%s\n' null
      return 0
    fi
    if [[ "$1 $2" == 'ps -aq' ]]; then
      printf '%s\n' candidate-id
      return 0
    fi
    if [[ "$1" == rm ]]; then
      return 0
    fi
    return 1
  }

  if v1_candidate_break synthetic-lock; then
    printf 'stale break ignored name-to-ID replacement race\n' >&2
    return 1
  fi
  ! grep -q '^rm ' "$calls"
  [[ "$(<"$count_file")" -ge 2 ]]
  pass_case
}

test_stale_break_removes_only_verified_immutable_id_with_v() {
  local calls="$work_dir/stale-success.calls"
  : >"$calls"
  V1_BREAK_STALE_LOCK=1
  V1_TARGET_HASH=target-hash
  v1_docker() {
    printf '%s\n' "$*" >>"$calls"
    if [[ "$1 $2 $3" == 'container inspect synthetic-lock' && "${4-}" == --format ]]; then
      printf '%s\n' candidate-id
      return 0
    fi
    if [[ "$1 $2 $3" == 'container inspect candidate-id' && $# -eq 3 ]]; then
      if grep -q '^rm -v candidate-id$' "$calls"; then
        return 1
      fi
      write_candidate_inspect none
      return 0
    fi
    if [[ "$1 $2 $3" == 'image inspect sha256:stale-image' ]]; then
      printf '%s\n' null
      return 0
    fi
    if [[ "$1 $2" == 'ps -aq' ]]; then
      printf '%s\n' candidate-id
      return 0
    fi
    if [[ "$1 $2 $3" == 'rm -v candidate-id' ]]; then
      return 0
    fi
    return 1
  }

  v1_candidate_break synthetic-lock
  [[ "$(grep -c '^container inspect synthetic-lock --format ' "$calls")" -eq 2 ]]
  [[ "$(grep -c '^container inspect candidate-id$' "$calls")" -eq 3 ]]
  [[ "$(grep -c '^ps -aq ' "$calls")" -eq 2 ]]
  grep -qx 'rm -v candidate-id' "$calls"
  pass_case
}

install_runtime_fixture() {
  local app_database_url="$1"
  local pg_user="$2"
  local pg_database="$3"
  V1_FIXTURE_APP_DATABASE_URL="$app_database_url"
  V1_FIXTURE_PG_USER="$pg_user"
  V1_FIXTURE_PG_DB="$pg_database"
  v1_one_id() {
    case "$1:$2" in
      container:app) printf '%s' app-id ;;
      container:postgres) printf '%s' pg-id ;;
      volume:avatar_data) printf '%s' avatar-volume ;;
      volume:pgdata) printf '%s' pg-volume ;;
      *) return 1 ;;
    esac
  }
  v1_docker() {
    if [[ "$1 $2 $3" == 'container inspect app-id' && $# -eq 3 ]]; then
      printf '%s\n' '[{"State":{"Status":"running","Running":true,"Paused":false,"Restarting":false,"Dead":false}}]'
      return 0
    fi
    if [[ "$1 $2 $3" == 'container inspect app-id' ]]; then
      case "$5" in
        '{{.Image}}') printf '%s\n' sha256:app ;;
        '{{range .Mounts}}{{if eq .Destination "/var/lib/crm/avatars"}}{{.Name}}{{end}}{{end}}') printf '%s\n' avatar-volume ;;
        '{{json .Config.Env}}') printf '["DATABASE_URL=%s"]\n' "$V1_FIXTURE_APP_DATABASE_URL" ;;
        *) return 1 ;;
      esac
      return 0
    fi
    if [[ "$1 $2 $3" == 'container inspect pg-id' ]]; then
      case "$5" in
        '{{.Image}}') printf '%s\n' sha256:pg ;;
        '{{range .Mounts}}{{if eq .Destination "/var/lib/postgresql/data"}}{{.Name}}{{end}}{{end}}') printf '%s\n' pg-volume ;;
        '{{json .Config.Env}}') printf '["POSTGRES_USER=%s","POSTGRES_DB=%s"]\n' "$V1_FIXTURE_PG_USER" "$V1_FIXTURE_PG_DB" ;;
        *) return 1 ;;
      esac
      return 0
    fi
    if [[ "$1 $2" == 'network ls' ]]; then
      printf '%s\n' network-id
      return 0
    fi
    if [[ "$1 $2 $3" == 'volume inspect avatar-volume' ]]; then
      printf '%s\n' /synthetic/volumes/avatar
      return 0
    fi
    if [[ "$1 $2 $3" == 'volume inspect pg-volume' ]]; then
      printf '%s\n' /synthetic/volumes/postgres
      return 0
    fi
    return 1
  }
}

test_runtime_rejects_live_app_env_drift() {
  V1_APP_IMAGE_ID=sha256:app
  V1_PG_IMAGE_ID=sha256:pg
  V1_PROJECT_NAME=synthetic-project
  V1_PATH_AUTHORITY=/synthetic/output
  V1_EXPECTED_APP_DATABASE_URL='postgres://crm:expected@postgres:5432/crm?sslmode=disable'
  V1_EXPECTED_PG_USER=crm
  V1_EXPECTED_PG_DB=crm
  install_runtime_fixture 'postgres://crm:stale@postgres:5432/crm?sslmode=disable' crm crm
  if (v1_resolve_runtime >/dev/null 2>&1); then
    printf 'runtime app Config.Env drift was accepted\n' >&2
    return 1
  fi
  pass_case
}

test_runtime_rejects_live_postgres_env_drift() {
  V1_APP_IMAGE_ID=sha256:app
  V1_PG_IMAGE_ID=sha256:pg
  V1_PROJECT_NAME=synthetic-project
  V1_PATH_AUTHORITY=/synthetic/output
  V1_EXPECTED_APP_DATABASE_URL='postgres://crm:expected@postgres:5432/crm?sslmode=disable'
  V1_EXPECTED_PG_USER=crm
  V1_EXPECTED_PG_DB=crm
  install_runtime_fixture "$V1_EXPECTED_APP_DATABASE_URL" stale-user crm
  if (v1_resolve_runtime >/dev/null 2>&1); then
    printf 'runtime postgres Config.Env drift was accepted\n' >&2
    return 1
  fi
  pass_case
}

test_runtime_accepts_matching_live_env_identity() {
  V1_APP_IMAGE_ID=sha256:app
  V1_PG_IMAGE_ID=sha256:pg
  V1_PROJECT_NAME=synthetic-project
  V1_PATH_AUTHORITY=/synthetic/output
  V1_EXPECTED_APP_DATABASE_URL='postgres://crm:expected@postgres:5432/crm?sslmode=disable'
  V1_EXPECTED_PG_USER=crm
  V1_EXPECTED_PG_DB=crm
  install_runtime_fixture "$V1_EXPECTED_APP_DATABASE_URL" crm crm
  v1_resolve_runtime >/dev/null
  pass_case
}

test_preflight_reuses_already_pinned_host() {
  local fake_root="$work_dir/fake-root"
  local args_log="$work_dir/preflight.args"
  mkdir -p "$fake_root/scripts"
  {
    printf '%s\n' '#!/usr/bin/env bash'
    printf '%s\n' 'printf "%s\n" "$*" >"$V1_TEST_ARGS_LOG"'
  } >"$fake_root/scripts/production-preflight.sh"
  chmod +x "$fake_root/scripts/production-preflight.sh"
  V1_OPS_ROOT="$fake_root"
  V1_ENV_FILE=/synthetic/env
  V1_COMPOSE_FILE=/synthetic/compose
  V1_DOCKER_CONTEXT=synthetic-context
  V1_PROJECT_NAME=synthetic-project
  V1_PINNED_HOST=unix:///synthetic/docker.sock
  V1_TEST_ARGS_LOG="$args_log"
  export V1_TEST_ARGS_LOG
  v1_recheck_lock() { return 0; }
  v1_preflight
  grep -q -- '--pinned-host unix:///synthetic/docker.sock' "$args_log"
  pass_case
}

test_preflight_pinned_host_skips_context_resolution() {
  local root="$work_dir/pinned-preflight"
  local fake_bin="$root/bin"
  local socket_path
  socket_path="$(python3 -c 'import os; print(os.path.realpath("/var/run/docker.sock"))')"
  local env_file="$root/managed.env"
  local compose_file="$root/compose.yml"
  local render_file="$root/render.json"
  local docker_log="$root/docker.log"
  mkdir -p "$fake_bin"
  [[ -S "$socket_path" ]]
  {
    printf '%s\n' 'DATABASE_URL=ignored'
    printf '%s\n' 'APP_DATABASE_URL='
    printf '%s\n' 'POSTGRES_USER=crm'
    printf '%s\n' 'POSTGRES_PASSWORD=managed-production-password'
    printf '%s\n' 'POSTGRES_DB=crm'
    printf '%s\n' 'AUTH_TOKEN_SECRET=0123456789abcdef0123456789abcdef'
    printf '%s\n' 'SEED_ADMIN_PASSWORD='
    printf '%s\n' 'TELEGRAM_BOT_TOKEN='
    printf '%s\n' 'TELEGRAM_BOT_USERNAME='
  } >"$env_file"
  printf '%s\n' 'services: {}' >"$compose_file"
  printf '%s\n' '{"services":{"app":{"image":"crm:local","environment":{"AUTH_TOKEN_SECRET":"0123456789abcdef0123456789abcdef","HTTP_ADDR":":8080","AVATAR_STORAGE_DRIVER":"local","AVATAR_LOCAL_ROOT":"/var/lib/crm/avatars","AVATAR_LOCAL_REQUIRE_MOUNT":"true","SEED_ADMIN_PASSWORD":"","TELEGRAM_BOT_TOKEN":"","TELEGRAM_BOT_USERNAME":"","DATABASE_URL":"postgres://crm:managed-production-password@postgres:5432/crm?sslmode=disable"},"volumes":[{"type":"volume","source":"avatar_data","target":"/var/lib/crm/avatars"}]},"postgres":{"image":"postgres:17-alpine","environment":{"POSTGRES_USER":"crm","POSTGRES_DB":"crm"},"volumes":[{"type":"volume","source":"pgdata","target":"/var/lib/postgresql/data"}]}},"volumes":{"avatar_data":{},"pgdata":{}}}' >"$render_file"
  {
    printf '%s\n' '#!/usr/bin/env bash'
    printf '%s\n' 'printf "%s\n" "$*" >>"$V1_TEST_DOCKER_LOG"'
    printf '%s\n' 'if [[ "$1" == context ]]; then exit 90; fi'
    printf '%s\n' '[[ "$1" == --host ]] || exit 91'
    printf '%s\n' 'shift 2'
    printf '%s\n' 'if [[ "$1" == info ]]; then printf "%s\n" engine-fixture; exit 0; fi'
    printf '%s\n' 'if [[ "$1 $2" == "compose version" ]]; then printf "%s\n" 2.24.0; exit 0; fi'
    printf '%s\n' 'if [[ "$1" == compose ]]; then /bin/cat "$V1_TEST_RENDER_FILE"; exit 0; fi'
    printf '%s\n' 'exit 92'
  } >"$fake_bin/docker"
  chmod +x "$fake_bin/docker"
  : >"$docker_log"

  env -i PATH="$fake_bin:$PATH" HOME="${HOME:-}" TMPDIR="${TMPDIR:-/tmp}" \
    V1_TEST_DOCKER_LOG="$docker_log" V1_TEST_RENDER_FILE="$render_file" \
    "$repo_root/scripts/production-preflight.sh" \
      --mode compose-managed-db --seed-state initialized \
      --env-file "$env_file" --compose-file "$compose_file" \
      --docker-context must-not-be-inspected --pinned-host "unix://$socket_path" \
      --project-name synthetic-project >"$root/out" 2>"$root/err"
  grep -qx 'complete/preflight=ok' "$root/out"
  ! grep -q '^context ' "$docker_log"
  grep -q "^--host unix://$socket_path " "$docker_log"
  pass_case
}

test_public_docker_stderr_is_private() {
  local root="$work_dir/docker-stderr-private"
  mkdir -m 700 "$root"
  (
    source "$repo_root/scripts/lib/v1-ops-common.sh"
    V1_PINNED_HOST=unix:///synthetic/docker.sock
    V1_PRIVATE_DIR="$root"
    v1_docker_client() {
      printf '%s\n' 'unix:///private/secret/docker.sock' >&2
      return 1
    }
    ! v1_docker info
  ) >"$root/public.out" 2>"$root/public.err"
  [[ ! -s "$root/public.err" ]]
  grep -qx 'unix:///private/secret/docker.sock' "$root/docker.stderr"
  pass_case
}

test_remove_helper_checks_unexpected_volume_residual() {
  local calls="$work_dir/remove-helper-volume.calls"
  : >"$calls"
  (
    source "$repo_root/scripts/lib/v1-ops-common.sh"
    V1_AVATAR_VOLUME=target-avatar
    V1_PG_VOLUME=target-pgdata
    v1_docker() {
      printf '%s\n' "$*" >>"$calls"
      case "$*" in
        'container inspect helper-id --format '* )
          printf '%s\n' target-avatar synthetic-anonymous-volume
          return 0
          ;;
        'rm -v helper-id') return 0 ;;
        'container inspect helper-id') return 1 ;;
        'volume inspect synthetic-anonymous-volume') return 1 ;;
        *) return 1 ;;
      esac
    }
    v1_remove_helper helper-id
  )
  grep -qx 'volume inspect synthetic-anonymous-volume' "$calls"
  pass_case
}

test_cleanup_helper_failure_keeps_fence_and_lock
test_cleanup_fence_failure_keeps_primary_lock
test_operation_helper_records_create_id_before_inspect
test_lock_records_create_id_before_inspect
test_fence_records_create_id_before_inspect
test_stale_break_rejects_incomplete_candidate_invariant
test_stale_break_rechecks_name_before_delete
test_stale_break_removes_only_verified_immutable_id_with_v
test_runtime_rejects_live_app_env_drift
test_runtime_rejects_live_postgres_env_drift
test_runtime_accepts_matching_live_env_identity
test_preflight_reuses_already_pinned_host
test_preflight_pinned_host_skips_context_resolution
test_public_docker_stderr_is_private
test_remove_helper_checks_unexpected_volume_residual

printf 'v1 ops common fixture tests: passed (%s cases)\n' "$case_count"
