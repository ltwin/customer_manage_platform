#!/usr/bin/env bash
set -euo pipefail

readonly EXIT_USAGE=2
readonly EXIT_ENV=3
readonly EXIT_CONFIG=4
readonly EXIT_COMPOSE=5
readonly EXIT_DEPENDENCY=6
readonly AUTH_SCHEMA_VERSION=13
readonly AUTH_COOKIE_PROFILE='__Host-crm_refresh|HttpOnly|Secure|SameSite=Strict|Path=/|no-domain'
readonly AUTH_LIMITER_KDF_VERSION='crm-auth/v1/limiter-hmac@v1'

MODE=""
SEED_STATE=""
ENV_FILE=""
COMPOSE_FILE=""
DOCKER_CONTEXT_NAME=""
PROJECT_NAME=""
PINNED_HOST=""
EVIDENCE_DIR=""
ACCOUNTCTL_BIN=""
BUILD_REVISION=""
PREFLIGHT_NOW=""
RENDER_FILE=""
TEMP_DIR=""
ENV_KEYS=()
ENV_VALUES=()
VALUE=""
FOUND="false"
PUBLIC_REGISTRATION_ENABLED=""
AUTH_TOKEN_ISSUER_VALUE=""
PUBLIC_BASE_URL_VALUE=""
AUTH_MAIL_DRIVER_VALUE=""
AUTH_MAIL_FROM_VALUE=""
TRUSTED_PROXY_CIDRS_VALUE=""
AUTH_TOKEN_SECRET_VERSION_VALUE=""
CANONICAL_PATH=""
SCRIPT_DIR="$(cd "${BASH_SOURCE[0]%/*}" && pwd -P)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd -P)"

status() {
  printf '%s/%s=%s\n' "$1" "$2" "$3"
}

fail() {
  local code="$1"
  local stage="$2"
  local key="$3"
  local remediation="$4"
  printf 'error code=%s stage=%s key=%s remediation=%s\n' \
    "$code" "$stage" "$key" "$remediation" >&2
  exit "$code"
}

cleanup() {
  if [[ -n "$TEMP_DIR" && -d "$TEMP_DIR" ]]; then
    rm -rf "$TEMP_DIR" >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT

usage_failure() {
  fail "$EXIT_USAGE" "usage" "ARGUMENTS" "use-explicit-required-flags"
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --mode)
      [[ $# -ge 2 ]] || usage_failure
      MODE="$2"
      shift 2
      ;;
    --seed-state)
      [[ $# -ge 2 ]] || usage_failure
      SEED_STATE="$2"
      shift 2
      ;;
    --env-file)
      [[ $# -ge 2 ]] || usage_failure
      ENV_FILE="$2"
      shift 2
      ;;
    --compose-file)
      [[ $# -ge 2 ]] || usage_failure
      COMPOSE_FILE="$2"
      shift 2
      ;;
    --docker-context)
      [[ $# -ge 2 ]] || usage_failure
      DOCKER_CONTEXT_NAME="$2"
      shift 2
      ;;
    --project-name)
      [[ $# -ge 2 ]] || usage_failure
      PROJECT_NAME="$2"
      shift 2
      ;;
    --pinned-host)
      [[ $# -ge 2 ]] || usage_failure
      PINNED_HOST="$2"
      shift 2
      ;;
    --evidence-dir)
      [[ $# -ge 2 ]] || usage_failure
      EVIDENCE_DIR="$2"
      shift 2
      ;;
    --accountctl-bin)
      [[ $# -ge 2 ]] || usage_failure
      ACCOUNTCTL_BIN="$2"
      shift 2
      ;;
    --build-revision)
      [[ $# -ge 2 ]] || usage_failure
      BUILD_REVISION="$2"
      shift 2
      ;;
    --now)
      [[ $# -ge 2 ]] || usage_failure
      PREFLIGHT_NOW="$2"
      shift 2
      ;;
    *)
      usage_failure
      ;;
  esac
done

case "$MODE" in
  binary|compose-managed-db|compose-external-db) ;;
  *) usage_failure ;;
esac
case "$SEED_STATE" in
  empty|initialized) ;;
  *) usage_failure ;;
esac
[[ -n "$ENV_FILE" ]] || usage_failure

if [[ "$MODE" == "binary" ]]; then
  [[ -z "$COMPOSE_FILE" && -z "$DOCKER_CONTEXT_NAME" && -z "$PROJECT_NAME" && -z "$PINNED_HOST" ]] || usage_failure
else
  [[ -n "$COMPOSE_FILE" && -n "$DOCKER_CONTEXT_NAME" && -n "$PROJECT_NAME" ]] || \
    fail "$EXIT_USAGE" "endpoint-select" "DOCKER_CONTEXT" "provide-explicit-compose-target"
  [[ "$PROJECT_NAME" =~ ^[a-z0-9][a-z0-9_-]*$ ]] || \
    fail "$EXIT_USAGE" "usage" "PROJECT_NAME" "use-lowercase-compose-name"
fi

for selector_name in DOCKER_HOST DOCKER_CONTEXT DOCKER_TLS_VERIFY DOCKER_CERT_PATH; do
  if [[ -n "${!selector_name:-}" ]]; then
    fail "$EXIT_USAGE" "endpoint-select" "$selector_name" "unset-selector-environment"
  fi
done

require_command() {
  command -v "$1" >/dev/null 2>&1 || \
    fail "$EXIT_DEPENDENCY" "dependency-check" "COMMANDS" "install-required-command"
}

require_command python3
require_command mktemp
require_command rm
require_command chmod
if [[ "$MODE" != "binary" ]]; then
  require_command docker
fi
status "dependency-check" "commands" "ok"

TEMP_DIR="$(mktemp -d 2>/dev/null)" || \
  fail "$EXIT_DEPENDENCY" "dependency-check" "TEMP_DIRECTORY" "provide-secure-temp-directory"
chmod 700 "$TEMP_DIR" 2>/dev/null || \
  fail "$EXIT_DEPENDENCY" "dependency-check" "TEMP_DIRECTORY" "provide-secure-temp-directory"

canonical_existing_file() {
  local input="$1"
  CANONICAL_PATH="$(python3 -c '
import os, sys
p = os.path.realpath(sys.argv[1])
if not os.path.isfile(p) or not os.access(p, os.R_OK):
    raise SystemExit(1)
print(p)
' "$input" 2>/dev/null)" || return 1
  [[ -n "$CANONICAL_PATH" ]]
}

canonical_existing_directory() {
  local input="$1"
  CANONICAL_PATH="$(python3 -c '
import os, sys
p = os.path.realpath(sys.argv[1])
if not os.path.isdir(p) or not os.access(p, os.R_OK | os.X_OK):
    raise SystemExit(1)
print(p)
' "$input" 2>/dev/null)" || return 1
  [[ -n "$CANONICAL_PATH" ]]
}

canonical_existing_file "$ENV_FILE" || \
  fail "$EXIT_ENV" "env-parse" "ENV_FILE" "provide-readable-regular-env-file"
ENV_FILE="$CANONICAL_PATH"

parse_env_file() {
  local line=""
  local key=""
  local raw=""
  local parsed=""
  local first=""
  local last=""
  local existing=""
  local index=0
  while IFS= read -r line || [[ -n "$line" ]]; do
    line="${line%$'\r'}"
    if [[ "$line" =~ ^[[:space:]]*$ || "$line" =~ ^[[:space:]]*# ]]; then
      continue
    fi
    if [[ ! "$line" =~ ^([A-Za-z_][A-Za-z0-9_]*)=(.*)$ ]]; then
      return 1
    fi
    key="${BASH_REMATCH[1]}"
    raw="${BASH_REMATCH[2]}"
    first="${raw:0:1}"
    last=""
    if [[ ${#raw} -gt 0 ]]; then
      last="${raw:${#raw}-1:1}"
    fi
    if [[ "$first" == "'" || "$first" == '"' ]]; then
      if [[ ${#raw} -lt 2 || "$last" != "$first" ]]; then
        return 1
      fi
      parsed="${raw:1:${#raw}-2}"
    else
      if [[ "$raw" == *"'"* || "$raw" == *'"'* ]]; then
        return 1
      fi
      parsed="$raw"
    fi
    for existing in "${ENV_KEYS[@]:-}"; do
      if [[ "$existing" == "$key" ]]; then
        return 1
      fi
    done
    index=${#ENV_KEYS[@]}
    ENV_KEYS[$index]="$key"
    ENV_VALUES[$index]="$parsed"
  done <"$ENV_FILE"
}

parse_env_file || \
  fail "$EXIT_ENV" "env-parse" "ENV_FILE" "use-strict-unique-dotenv-data"
status "env-parse" "file" "ok"

get_value() {
  local requested="$1"
  local index=0
  VALUE=""
  FOUND="false"
  while [[ $index -lt ${#ENV_KEYS[@]} ]]; do
    if [[ "${ENV_KEYS[$index]}" == "$requested" ]]; then
      VALUE="${ENV_VALUES[$index]}"
      FOUND="true"
      return 0
    fi
    index=$((index + 1))
  done
}

is_dev_sentinel() {
  case "$1" in
    *crm-dev-password*|dev-only-token-secret-change-me|dev-only-admin-password)
      return 0
      ;;
    *) return 1 ;;
  esac
}

validate_seed() {
  get_value "SEED_ADMIN_PASSWORD"
  if [[ "$FOUND" == "true" && -n "$VALUE" ]]; then
    fail "$EXIT_CONFIG" "config-matrix" "SEED_ADMIN_PASSWORD" "remove-retired-seed-secret"
  fi
  status "config-matrix" "SEED_ADMIN_PASSWORD" "ok"
}

validate_auth() {
  get_value "AUTH_TOKEN_SECRET"
  if [[ "$FOUND" != "true" || ${#VALUE} -lt 32 ]] || is_dev_sentinel "$VALUE"; then
    fail "$EXIT_CONFIG" "config-matrix" "AUTH_TOKEN_SECRET" "set-strong-non-sentinel-secret"
  fi
  status "config-matrix" "AUTH_TOKEN_SECRET" "ok"
}

validate_account_auth_profile() {
  local public_base_url=""
  local base_policy=""
  local registration_enabled=""
  local mail_driver=""
  local mail_key=""
  local mail_from=""

  get_value "AUTH_TOKEN_ISSUER"
  if [[ "$FOUND" != "true" || -z "$VALUE" ]]; then
    fail "$EXIT_CONFIG" "config-matrix" "AUTH_TOKEN_ISSUER" "set-stable-auth-token-issuer"
  fi
  AUTH_TOKEN_ISSUER_VALUE="$VALUE"
  status "config-matrix" "AUTH_TOKEN_ISSUER" "ok"

  get_value "PUBLIC_BASE_URL"
  public_base_url="$VALUE"
  base_policy="$(PREFLIGHT_PUBLIC_BASE_URL="$public_base_url" python3 -c '
import os
from urllib.parse import urlparse
try:
    raw = os.environ["PREFLIGHT_PUBLIC_BASE_URL"]
    parsed = urlparse(raw)
    canonical = "https://" + (parsed.netloc or "").lower()
    valid = (
        parsed.scheme == "https"
        and bool(parsed.hostname)
        and parsed.port != 443
        and parsed.username is None
        and parsed.password is None
        and parsed.path == ""
        and not parsed.params
        and not parsed.query
        and not parsed.fragment
        and raw == canonical
    )
    print("ok" if valid else "invalid")
except Exception:
    print("invalid")
' 2>/dev/null)" || base_policy="invalid"
  if [[ "$FOUND" != "true" || "$base_policy" != "ok" ]]; then
    fail "$EXIT_CONFIG" "config-matrix" "PUBLIC_BASE_URL" "use-canonical-https-origin"
  fi
  PUBLIC_BASE_URL_VALUE="$public_base_url"
  status "config-matrix" "PUBLIC_BASE_URL" "ok"

  get_value "AUTH_PUBLIC_REGISTRATION_ENABLED"
  registration_enabled="$VALUE"
  if [[ "$FOUND" != "true" || ( "$registration_enabled" != "true" && "$registration_enabled" != "false" ) ]]; then
    fail "$EXIT_CONFIG" "config-matrix" "AUTH_PUBLIC_REGISTRATION_ENABLED" "set-explicit-boolean"
  fi
  PUBLIC_REGISTRATION_ENABLED="$registration_enabled"
  if [[ "$registration_enabled" == "true" ]]; then
    status "config-matrix" "AUTH_PUBLIC_REGISTRATION_ENABLED" "enabled-requested"
  else
    status "config-matrix" "AUTH_PUBLIC_REGISTRATION_ENABLED" "disabled"
  fi

  get_value "AUTH_MAIL_DRIVER"
  mail_driver="$VALUE"
  [[ "$FOUND" == "true" && "$mail_driver" == "resend" ]] || \
    fail "$EXIT_CONFIG" "config-matrix" "AUTH_MAIL_DRIVER" "use-verified-production-mail-adapter"
  get_value "RESEND_API_KEY"
  mail_key="$VALUE"
  [[ "$FOUND" == "true" && -n "$mail_key" ]] || \
    fail "$EXIT_CONFIG" "config-matrix" "RESEND_API_KEY" "set-production-mail-credential"
  get_value "AUTH_MAIL_FROM"
  mail_from="$VALUE"
  [[ "$FOUND" == "true" && -n "$mail_from" ]] || \
    fail "$EXIT_CONFIG" "config-matrix" "AUTH_MAIL_FROM" "set-verified-production-sender"
  AUTH_MAIL_DRIVER_VALUE="$mail_driver"
  AUTH_MAIL_FROM_VALUE="$mail_from"

  get_value "TRUSTED_PROXY_CIDRS"
  TRUSTED_PROXY_CIDRS_VALUE="$VALUE"

  get_value "AUTH_TOKEN_SECRET_VERSION"
  AUTH_TOKEN_SECRET_VERSION_VALUE="$VALUE"
  status "config-matrix" "AUTH_MAIL_DRIVER" "ok"
}

complete_auth_readiness() {
  local config_fingerprint=""
  local database_url=""
  local live_report="$TEMP_DIR/auth-readiness-live.json"
  local evidence_error="$TEMP_DIR/auth-readiness-evidence.err"

  if [[ "$PUBLIC_REGISTRATION_ENABLED" != "true" ]]; then
    status "readiness" "registration" "secure-baseline-ready"
    status "complete" "preflight" "secure-baseline-ready"
    return
  fi

  if [[ -z "$EVIDENCE_DIR" || -z "$BUILD_REVISION" || -z "$AUTH_TOKEN_SECRET_VERSION_VALUE" ]]; then
    fail "$EXIT_CONFIG" "readiness" "AUTH_READINESS_EVIDENCE" "provide-complete-current-evidence"
  fi
  if [[ "$MODE" == "binary" && -z "$ACCOUNTCTL_BIN" ]]; then
    fail "$EXIT_CONFIG" "readiness" "AUTH_READINESS_EVIDENCE" "provide-complete-current-evidence"
  fi
  if [[ "$MODE" != "binary" && -n "$ACCOUNTCTL_BIN" ]]; then
    usage_failure
  fi
  canonical_existing_directory "$EVIDENCE_DIR" || \
    fail "$EXIT_CONFIG" "readiness" "AUTH_READINESS_EVIDENCE" "provide-complete-current-evidence"
  EVIDENCE_DIR="$CANONICAL_PATH"
  if [[ -z "$PREFLIGHT_NOW" ]]; then
    PREFLIGHT_NOW="$(python3 -c 'from datetime import datetime, timezone; print(datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"))')" || \
      fail "$EXIT_DEPENDENCY" "readiness" "CLOCK" "provide-working-python-runtime"
  fi

  config_fingerprint="$(python3 "$SCRIPT_DIR/lib/auth-preflight-evidence.py" fingerprint \
    --mail-driver "$AUTH_MAIL_DRIVER_VALUE" \
    --mail-provider "$AUTH_MAIL_DRIVER_VALUE" \
    --mail-from "$AUTH_MAIL_FROM_VALUE" \
    --issuer "$AUTH_TOKEN_ISSUER_VALUE" \
    --public-base-url "$PUBLIC_BASE_URL_VALUE" \
    --proxy-cidrs "$TRUSTED_PROXY_CIDRS_VALUE" \
    --cookie-profile "$AUTH_COOKIE_PROFILE" \
    --limiter-kdf-version "$AUTH_LIMITER_KDF_VERSION" \
    --secret-version-ref "$AUTH_TOKEN_SECRET_VERSION_VALUE" 2>/dev/null)" || \
      fail "$EXIT_CONFIG" "readiness" "AUTH_CONFIG_FINGERPRINT" "fix-non-secret-auth-config"
  status "readiness" "config_fingerprint" "$config_fingerprint"

  if [[ "$MODE" == "binary" ]]; then
    canonical_existing_file "$ACCOUNTCTL_BIN" || \
      fail "$EXIT_DEPENDENCY" "readiness" "ACCOUNTCTL" "provide-current-accountctl-binary"
    ACCOUNTCTL_BIN="$CANONICAL_PATH"
    [[ -x "$ACCOUNTCTL_BIN" ]] || \
      fail "$EXIT_DEPENDENCY" "readiness" "ACCOUNTCTL" "provide-current-accountctl-binary"
    get_value "DATABASE_URL"
    database_url="$VALUE"
    if ! (umask 077; env -i PATH="$PATH" DATABASE_URL="$database_url" \
      "$ACCOUNTCTL_BIN" auth readiness \
      --build-revision "$BUILD_REVISION" \
      --config-fingerprint "$config_fingerprint" >"$live_report") 2>/dev/null; then
      fail "$EXIT_CONFIG" "readiness" "AUTH_LIVE_CHECK" "fix-limiter-schema-or-legacy-cutover"
    fi
  else
    if ! (umask 077; compose_pinned run --rm --no-deps \
      --entrypoint /usr/local/bin/accountctl app auth readiness \
      --build-revision "$BUILD_REVISION" \
      --config-fingerprint "$config_fingerprint" >"$live_report") 2>/dev/null; then
      fail "$EXIT_CONFIG" "readiness" "AUTH_LIVE_CHECK" "fix-limiter-schema-or-legacy-cutover"
    fi
  fi

  if ! python3 "$SCRIPT_DIR/lib/auth-preflight-evidence.py" verify \
    --evidence-dir "$EVIDENCE_DIR" \
    --live-report "$live_report" \
    --build-revision "$BUILD_REVISION" \
    --schema-version "$AUTH_SCHEMA_VERSION" \
    --config-fingerprint "$config_fingerprint" \
    --now "$PREFLIGHT_NOW" \
    --mail-provider "$AUTH_MAIL_DRIVER_VALUE" \
    --secret-version-ref "$AUTH_TOKEN_SECRET_VERSION_VALUE" \
    2>"$evidence_error"; then
    sed -n '1p' "$evidence_error" >&2
    fail "$EXIT_CONFIG" "readiness" "AUTH_READINESS_EVIDENCE" "refresh-or-regenerate-evidence"
  fi
  status "readiness" "registration" "enable-ready"
  status "complete" "preflight" "enable-ready"
}

validate_cookie_profile() {
  local source="$REPO_ROOT/backend/internal/platform/httpapi/auth.go"
  if python3 - "$source" <<'PY' >/dev/null 2>&1
import pathlib
import sys

source = pathlib.Path(sys.argv[1]).read_text(encoding="utf-8")
required = (
    'const refreshCookieName = "__Host-crm_refresh"',
    'if session.RefreshAbsoluteAt.Before(expiresAt)',
    'Path: "/", Expires: expiresAt.UTC(), MaxAge: maxAge',
    'HttpOnly: true, Secure: true, SameSite: http.SameSiteStrictMode',
    'Expires: time.Unix(1, 0).UTC(), MaxAge: -1',
)
if any(marker not in source for marker in required):
    raise SystemExit(1)
cookie_block = source.split("func (h *handlers) setRefreshCookie", 1)[1].split("func (h *handlers) writeAuthError", 1)[0]
if "Domain:" in cookie_block:
    raise SystemExit(1)
PY
  then
    status "config-matrix" "REFRESH_COOKIE_PROFILE" "ok"
  else
    fail "$EXIT_CONFIG" "config-matrix" "REFRESH_COOKIE_PROFILE" "restore-host-http-only-secure-strict-cookie-contract"
  fi
}

validate_telegram() {
  local token=""
  local username=""
  get_value "TELEGRAM_BOT_TOKEN"
  token="$VALUE"
  get_value "TELEGRAM_BOT_USERNAME"
  username="$VALUE"
  if [[ -z "$token" && -z "$username" ]]; then
    status "config-matrix" "TELEGRAM" "disabled"
    return
  fi
  if [[ -z "$token" || -z "$username" ]]; then
    fail "$EXIT_CONFIG" "config-matrix" "TELEGRAM" "set-both-token-and-username-or-neither"
  fi
  if [[ ! "$username" =~ ^[A-Za-z0-9_]{5,32}$ || ! "$username" =~ [Bb][Oo][Tt]$ ]]; then
    fail "$EXIT_CONFIG" "config-matrix" "TELEGRAM_BOT_USERNAME" "set-valid-bot-username"
  fi
  case "$token" in
    *example*|*EXAMPLE*|*replace*|*REPLACE*|*test-token*|*TEST-TOKEN*|*synthetic*|*SYNTHETIC*)
      fail "$EXIT_CONFIG" "config-matrix" "TELEGRAM_BOT_TOKEN" "replace-example-token"
      ;;
  esac
  status "config-matrix" "TELEGRAM" "enabled"
}

validate_database_url() {
  local key="$1"
  local url="$2"
  local policy=""
  if [[ -z "$url" ]] || is_dev_sentinel "$url"; then
    fail "$EXIT_CONFIG" "config-matrix" "$key" "set-production-database-url"
  fi
  policy="$(PREFLIGHT_DATABASE_URL="$url" python3 -c '
import os
from urllib.parse import parse_qs, urlparse
try:
    parsed = urlparse(os.environ["PREFLIGHT_DATABASE_URL"])
    if parsed.scheme not in {"postgres", "postgresql"}:
        raise ValueError
    host = parsed.hostname or ""
    query = parse_qs(parsed.query, keep_blank_values=True)
    sslmode = (query.get("sslmode") or [""])[-1].lower()
    socket_host = (query.get("host") or [""])[-1]
    local = host in {"", "localhost", "127.0.0.1", "::1"} or socket_host.startswith("/")
    if sslmode == "disable" and not local:
        print("remote-disable")
    elif sslmode == "disable" and local:
        print("local-disable")
    else:
        print("ok")
except Exception:
    print("invalid")
' 2>/dev/null)" || policy="invalid"
  case "$policy" in
    remote-disable|invalid)
      fail "$EXIT_CONFIG" "config-matrix" "$key" "use-valid-ssl-protected-database-url"
      ;;
    local-disable)
      status "config-matrix" "$key" "network-boundary-attention"
      ;;
    ok)
      status "config-matrix" "$key" "ok"
      ;;
    *)
      fail "$EXIT_CONFIG" "config-matrix" "$key" "use-valid-ssl-protected-database-url"
      ;;
  esac
}

validate_binary_avatar() {
  local driver=""
  local root=""
  local require_mount=""
  local real_root=""
  get_value "AVATAR_STORAGE_DRIVER"
  driver="$VALUE"
  [[ "$driver" == "local" ]] || \
    fail "$EXIT_CONFIG" "config-matrix" "AVATAR_STORAGE_DRIVER" "use-local-avatar-storage"
  get_value "AVATAR_LOCAL_ROOT"
  root="$VALUE"
  [[ "$FOUND" == "true" && "$root" == /* && -d "$root" && -r "$root" && -w "$root" && -x "$root" ]] || \
    fail "$EXIT_CONFIG" "config-matrix" "AVATAR_LOCAL_ROOT" "use-readable-writable-absolute-directory"
  real_root="$(python3 -c 'import os,sys; print(os.path.realpath(sys.argv[1]))' "$root" 2>/dev/null)" || \
    fail "$EXIT_CONFIG" "config-matrix" "AVATAR_LOCAL_ROOT" "use-readable-writable-absolute-directory"
  [[ -n "$real_root" && -d "$real_root" ]] || \
    fail "$EXIT_CONFIG" "config-matrix" "AVATAR_LOCAL_ROOT" "use-readable-writable-absolute-directory"
  get_value "AVATAR_LOCAL_REQUIRE_MOUNT"
  require_mount="$VALUE"
  [[ "$FOUND" == "true" && ( "$require_mount" == "true" || "$require_mount" == "false" ) ]] || \
    fail "$EXIT_CONFIG" "config-matrix" "AVATAR_LOCAL_REQUIRE_MOUNT" "set-explicit-boolean"
  if [[ "$require_mount" == "true" ]]; then
    python3 -c 'import os,sys; raise SystemExit(0 if os.path.ismount(sys.argv[1]) else 1)' "$real_root" 2>/dev/null || \
      fail "$EXIT_CONFIG" "config-matrix" "AVATAR_LOCAL_ROOT" "use-verified-mount-point"
    status "config-matrix" "AVATAR_LOCAL_REQUIRE_MOUNT" "ok"
  else
    status "config-matrix" "AVATAR_LOCAL_REQUIRE_MOUNT" "durability-attention"
  fi
  case "$real_root" in
    "$TEMP_DIR"/*|/tmp/*|/private/tmp/*|"$REPO_ROOT"/*)
      status "config-matrix" "AVATAR_LOCAL_ROOT" "durability-attention"
      ;;
    *) status "config-matrix" "AVATAR_LOCAL_ROOT" "ok" ;;
  esac
  status "config-matrix" "AVATAR_STORAGE_DRIVER" "ok"
}

validate_binary() {
  local database_url=""
  get_value "DATABASE_URL"
  database_url="$VALUE"
  [[ "$FOUND" == "true" ]] || \
    fail "$EXIT_CONFIG" "config-matrix" "DATABASE_URL" "set-production-database-url"
  validate_database_url "DATABASE_URL" "$database_url"
  validate_auth
  validate_seed
  validate_account_auth_profile
  validate_cookie_profile
  validate_binary_avatar
  validate_telegram
  get_value "HTTP_ADDR"
  if [[ "$FOUND" != "true" || -z "$VALUE" ]]; then
    status "config-matrix" "HTTP_ADDR" "public-bind-attention"
  elif [[ "$VALUE" == :* || "$VALUE" == 0.0.0.0:* || "$VALUE" == \[::\]:* ]]; then
    status "config-matrix" "HTTP_ADDR" "public-bind-attention"
  else
    status "config-matrix" "HTTP_ADDR" "ok"
  fi
  get_value "APP_DATABASE_URL"
  if [[ -n "$VALUE" ]]; then
    status "config-matrix" "APP_DATABASE_URL" "ignored-attention"
  fi
  for ignored_key in POSTGRES_USER POSTGRES_PASSWORD POSTGRES_DB; do
    get_value "$ignored_key"
    if [[ -n "$VALUE" ]]; then
      status "config-matrix" "$ignored_key" "ignored-attention"
    fi
  done
  complete_auth_readiness
}

if [[ "$MODE" == "binary" ]]; then
  validate_binary
  exit 0
fi

canonical_existing_file "$COMPOSE_FILE" || \
  fail "$EXIT_USAGE" "usage" "COMPOSE_FILE" "provide-readable-regular-compose-file"
COMPOSE_FILE="$CANONICAL_PATH"

docker_context() {
  (
    unset DOCKER_HOST DOCKER_CONTEXT DOCKER_TLS_VERIFY DOCKER_CERT_PATH
    command docker context inspect "$DOCKER_CONTEXT_NAME" \
      --format '{{ (index .Endpoints "docker").Host }}'
  )
}

if [[ -n "$PINNED_HOST" ]]; then
  endpoint="$PINNED_HOST"
else
  endpoint="$(docker_context 2>/dev/null)" || \
    fail "$EXIT_USAGE" "endpoint-select" "DOCKER_CONTEXT" "use-existing-local-unix-context"
fi
case "$endpoint" in
  unix:///*) ;;
  *) fail "$EXIT_USAGE" "endpoint-select" "DOCKER_CONTEXT" "use-existing-local-unix-context" ;;
esac
socket_path="${endpoint#unix://}"
[[ "$socket_path" == /* && -S "$socket_path" ]] || \
  fail "$EXIT_USAGE" "endpoint-select" "DOCKER_CONTEXT" "use-existing-local-unix-context"
socket_real="$(python3 -c 'import os,sys; print(os.path.realpath(sys.argv[1]))' "$socket_path" 2>/dev/null)" || \
  fail "$EXIT_USAGE" "endpoint-select" "DOCKER_CONTEXT" "use-existing-local-unix-context"
[[ "$socket_real" == /* && -S "$socket_real" ]] || \
  fail "$EXIT_USAGE" "endpoint-select" "DOCKER_CONTEXT" "use-existing-local-unix-context"
[[ -z "$PINNED_HOST" || "$PINNED_HOST" == "unix://$socket_real" ]] || \
  fail "$EXIT_USAGE" "endpoint-select" "PINNED_HOST" "use-canonical-local-unix-socket"
PINNED_HOST="unix://$socket_real"
status "endpoint-select" "context" "ok"

docker_pinned() {
  (
    unset DOCKER_HOST DOCKER_CONTEXT DOCKER_TLS_VERIFY DOCKER_CERT_PATH
    command docker --host "$PINNED_HOST" "$@"
  )
}

compose_pinned() {
  local selected_env_file="$ENV_FILE"
  local selected_compose_file="$COMPOSE_FILE"
  local selected_project_name="$PROJECT_NAME"
  (
    unset DOCKER_HOST DOCKER_CONTEXT DOCKER_TLS_VERIFY DOCKER_CERT_PATH
    unset COMPOSE_FILE COMPOSE_PROJECT_NAME COMPOSE_PROFILES COMPOSE_ENV_FILES COMPOSE_DISABLE_ENV_FILE
    CRM_ENV_FILE="$selected_env_file" command docker --host "$PINNED_HOST" compose \
      --env-file "$selected_env_file" -f "$selected_compose_file" --project-name "$selected_project_name" "$@"
  )
}

engine_id="$(docker_pinned info --format '{{.ID}}' 2>/dev/null)" || \
  fail "$EXIT_COMPOSE" "compose-render" "ENGINE_ID" "verify-local-docker-engine"
[[ -n "$engine_id" ]] || \
  fail "$EXIT_COMPOSE" "compose-render" "ENGINE_ID" "verify-local-docker-engine"
status "compose-render" "ENGINE_ID" "ok"

compose_version="$(docker_pinned compose version --short 2>/dev/null)" || \
  fail "$EXIT_COMPOSE" "compose-version" "COMPOSE_VERSION" "install-compose-2-24-or-newer"
compose_version="${compose_version#v}"
compose_major="${compose_version%%.*}"
compose_rest="${compose_version#*.}"
compose_minor="${compose_rest%%.*}"
if [[ ! "$compose_major" =~ ^[0-9]+$ || ! "$compose_minor" =~ ^[0-9]+$ ]] || \
  (( compose_major < 2 || (compose_major == 2 && compose_minor < 24) )); then
  fail "$EXIT_COMPOSE" "compose-version" "COMPOSE_VERSION" "install-compose-2-24-or-newer"
fi
status "compose-version" "cli" "ok"

RENDER_FILE="$TEMP_DIR/render.json"
if ! (umask 077; compose_pinned config --format json >"$RENDER_FILE") 2>/dev/null; then
  fail "$EXIT_COMPOSE" "compose-render" "COMPOSE_MODEL" "fix-compose-render"
fi

set +e
python3 - "$RENDER_FILE" "$ENV_FILE" <<'PY' >/dev/null 2>&1
import json
import re
import sys

render_path, env_path = sys.argv[1:]
with open(render_path, encoding="utf-8") as source:
    model = json.load(source)
services = model.get("services") or {}
volumes = model.get("volumes") or {}
if set(services) != {"app", "postgres"}:
    raise SystemExit(10)
if set(volumes) != {"avatar_data", "pgdata"}:
    raise SystemExit(20)
if not services["app"].get("image") or not services["postgres"].get("image"):
    raise SystemExit(30)

values = {}
with open(env_path, encoding="utf-8") as source:
    for raw in source:
        line = raw.rstrip("\n\r")
        if not line.strip() or line.lstrip().startswith("#"):
            continue
        match = re.fullmatch(r"([A-Za-z_][A-Za-z0-9_]*)=(.*)", line)
        if not match:
            raise SystemExit(60)
        key, value = match.groups()
        if value[:1] in {"'", '"'}:
            value = value[1:-1]
        values[key] = value
expected_auth = values.get("AUTH_TOKEN_SECRET", "")
rendered_auth = (services["app"].get("environment") or {}).get("AUTH_TOKEN_SECRET", "")
if expected_auth and rendered_auth != expected_auth:
    raise SystemExit(70)
PY
render_check=$?
set -e
case "$render_check" in
  0) ;;
  10) fail "$EXIT_COMPOSE" "compose-render" "SERVICES" "use-fixed-app-and-postgres-services" ;;
  20) fail "$EXIT_COMPOSE" "compose-render" "VOLUMES" "use-fixed-avatar-and-postgres-volumes" ;;
  30) fail "$EXIT_COMPOSE" "compose-render" "IMAGES" "set-service-images" ;;
  70) fail "$EXIT_COMPOSE" "compose-render" "APP_ENV_FILE" "select-same-canonical-env-file" ;;
  *) fail "$EXIT_COMPOSE" "compose-render" "COMPOSE_MODEL" "fix-compose-render" ;;
esac
status "compose-render" "model" "ok"

render_env_value() {
  local service="$1"
  local key="$2"
  VALUE="$(python3 -c '
import json,sys
with open(sys.argv[1], encoding="utf-8") as source:
    model=json.load(source)
value=((model.get("services") or {}).get(sys.argv[2], {}).get("environment") or {}).get(sys.argv[3], "")
if value is None:
    value=""
print(value, end="")
' "$RENDER_FILE" "$service" "$key" 2>/dev/null)" || \
    fail "$EXIT_COMPOSE" "compose-render" "COMPOSE_MODEL" "fix-compose-render"
}

render_has_mount() {
  local service="$1"
  local source_name="$2"
  local target_path="$3"
  python3 -c '
import json,sys
with open(sys.argv[1], encoding="utf-8") as source:
    model=json.load(source)
mounts=((model.get("services") or {}).get(sys.argv[2], {}).get("volumes") or [])
matched=any(isinstance(item, dict) and item.get("type") == "volume" and item.get("source") == sys.argv[3] and item.get("target") == sys.argv[4] for item in mounts)
raise SystemExit(0 if matched else 1)
' "$RENDER_FILE" "$service" "$source_name" "$target_path" >/dev/null 2>&1
}

validate_auth
validate_seed
validate_account_auth_profile
validate_cookie_profile
validate_telegram

render_env_value "app" "HTTP_ADDR"
[[ "$VALUE" == ":8080" ]] || \
  fail "$EXIT_CONFIG" "config-matrix" "HTTP_ADDR" "use-fixed-compose-http-address"
status "config-matrix" "HTTP_ADDR" "public-bind-attention"
render_env_value "app" "AVATAR_STORAGE_DRIVER"
[[ "$VALUE" == "local" ]] || \
  fail "$EXIT_CONFIG" "config-matrix" "AVATAR_STORAGE_DRIVER" "use-local-avatar-storage"
render_env_value "app" "AVATAR_LOCAL_ROOT"
[[ "$VALUE" == "/var/lib/crm/avatars" ]] || \
  fail "$EXIT_CONFIG" "config-matrix" "AVATAR_LOCAL_ROOT" "use-fixed-compose-avatar-root"
render_env_value "app" "AVATAR_LOCAL_REQUIRE_MOUNT"
[[ "$VALUE" == "true" ]] || \
  fail "$EXIT_CONFIG" "config-matrix" "AVATAR_LOCAL_REQUIRE_MOUNT" "require-compose-avatar-mount"
render_has_mount "app" "avatar_data" "/var/lib/crm/avatars" || \
  fail "$EXIT_CONFIG" "config-matrix" "AVATAR_LOCAL_ROOT" "use-fixed-compose-avatar-mount"
render_has_mount "postgres" "pgdata" "/var/lib/postgresql/data" || \
  fail "$EXIT_CONFIG" "config-matrix" "POSTGRES_DB" "use-fixed-compose-postgres-mount"
status "config-matrix" "AVATAR_STORAGE" "ok"

if [[ "$MODE" == "compose-managed-db" ]]; then
  get_value "APP_DATABASE_URL"
  [[ -z "$VALUE" ]] || \
    fail "$EXIT_CONFIG" "config-matrix" "APP_DATABASE_URL" "remove-external-database-url"
  local_user=""
  local_password=""
  local_database=""
  get_value "POSTGRES_USER"
  local_user="$VALUE"
  [[ "$FOUND" == "true" && -n "$local_user" ]] || \
    fail "$EXIT_CONFIG" "config-matrix" "POSTGRES_USER" "set-managed-database-user"
  get_value "POSTGRES_DB"
  local_database="$VALUE"
  [[ "$FOUND" == "true" && -n "$local_database" ]] || \
    fail "$EXIT_CONFIG" "config-matrix" "POSTGRES_DB" "set-managed-database-name"
  get_value "POSTGRES_PASSWORD"
  local_password="$VALUE"
  if [[ "$FOUND" != "true" || -z "$local_password" ]] || is_dev_sentinel "$local_password"; then
    fail "$EXIT_CONFIG" "config-matrix" "POSTGRES_PASSWORD" "set-production-database-password"
  fi
  render_env_value "app" "DATABASE_URL"
  [[ "$VALUE" == "postgres://$local_user:$local_password@postgres:5432/$local_database?sslmode=disable" ]] || \
    fail "$EXIT_CONFIG" "config-matrix" "DATABASE_URL" "use-managed-compose-database"
  status "config-matrix" "POSTGRES" "ok"
  get_value "DATABASE_URL"
  if [[ -n "$VALUE" ]]; then
    status "config-matrix" "DATABASE_URL" "ignored-attention"
  fi
else
  get_value "APP_DATABASE_URL"
  external_database_url="$VALUE"
  [[ "$FOUND" == "true" ]] || \
    fail "$EXIT_CONFIG" "config-matrix" "APP_DATABASE_URL" "set-production-database-url"
  validate_database_url "APP_DATABASE_URL" "$external_database_url"
  render_env_value "app" "DATABASE_URL"
  [[ "$VALUE" == "$external_database_url" ]] || \
    fail "$EXIT_CONFIG" "config-matrix" "DATABASE_URL" "use-external-compose-database"
  for ignored_key in POSTGRES_USER POSTGRES_PASSWORD POSTGRES_DB; do
    get_value "$ignored_key"
    if [[ -n "$VALUE" ]]; then
      status "config-matrix" "$ignored_key" "ignored-attention"
    fi
  done
  get_value "DATABASE_URL"
  if [[ -n "$VALUE" ]]; then
    status "config-matrix" "DATABASE_URL" "ignored-attention"
  fi
  status "support-boundary" "backup-restore" "unsupported"
fi

complete_auth_readiness
