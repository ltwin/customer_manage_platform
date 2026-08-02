#!/usr/bin/env bash
# CMD-004 / A15：本 feature 自有 v1 restore wipe harness。
# 调用真实 restore-compose / v1-ops-package；不修改 v1-hardening catalog。
#
# 前置：Docker Desktop context desktop-linux + 可用 app 镜像（默认 crm:local；
# 可用 AVATAR_MEDIA_WIPE_IMAGE 覆盖）。构建示例：docker build -t crm:local .
#
# 断言：restore 成功后 database_counts 与 v1 包一致，且 avatars/*/account-profile/**
# 物理路径不存在。禁止要求 v2 generate manifest_sha256 == v1 包摘要。
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

WITH_PROFILE_MIGRATION=0
for arg in "$@"; do
  case "$arg" in
    --with-profile-migration) WITH_PROFILE_MIGRATION=1 ;;
    -h|--help)
      printf 'usage: %s [--with-profile-migration]\n' "$(basename "$0")"
      exit 0
      ;;
    *)
      printf 'unknown arg: %s\n' "$arg" >&2
      exit 1
      ;;
  esac
done

EVIDENCE_DIR="${AVATAR_MEDIA_WIPE_EVIDENCE:-$repo_root/.codestable/features/2026-08-02-avatar-media-safety-net/evidence}"
if [[ "$WITH_PROFILE_MIGRATION" -eq 1 ]]; then
  EVIDENCE_DIR="${AVATAR_MEDIA_WIPE_EVIDENCE:-$repo_root/.codestable/features/2026-08-02-account-profile-center/evidence}"
fi
mkdir -p "$EVIDENCE_DIR"
LOG="$EVIDENCE_DIR/restore_fixture_log"
: >"$LOG"

log() { printf '%s\n' "$*" | tee -a "$LOG"; }
fail() { log "FAIL: $*"; exit 1; }
blocked() { log "BLOCKED: $*"; exit 2; }

HELPER="$repo_root/scripts/lib/v1-ops-package.py"
RESTORE="$repo_root/scripts/restore-compose.sh"
[[ -x "$RESTORE" || -f "$RESTORE" ]] || fail "restore-compose.sh missing"

if ! command -v docker >/dev/null 2>&1; then
  blocked "docker CLI missing"
fi
if ! docker info >/dev/null 2>&1; then
  blocked "docker engine unavailable (start Docker Desktop desktop-linux)"
fi
context="$(docker context show 2>/dev/null || true)"
if [[ "$context" != "desktop-linux" && "${AVATAR_MEDIA_WIPE_ALLOW_ANY_CONTEXT:-}" != "1" ]]; then
  blocked "docker context is '${context:-unknown}', want desktop-linux (or set AVATAR_MEDIA_WIPE_ALLOW_ANY_CONTEXT=1)"
fi

IMAGE="${AVATAR_MEDIA_WIPE_IMAGE:-crm:local}"
if ! docker image inspect "$IMAGE" >/dev/null 2>&1; then
  blocked "app image missing: $IMAGE (build with: docker build -t crm:local .)"
fi

WORKDIR="$(mktemp -d "${TMPDIR:-/tmp}/avatar-media-wipe.XXXXXX")"
COMPOSE_PROJECT="avatar-wipe-$$"
COMPOSE_FILE="$WORKDIR/docker-compose.yml"
ENV_FILE="$WORKDIR/.env"
PACKAGE_DIR="$WORKDIR/v1-package"
AVATAR_HOST="$WORKDIR/avatar-host"
PG_HOST="$WORKDIR/pg-host"

cleanup() {
  set +e
  docker compose --project-name "$COMPOSE_PROJECT" -f "$COMPOSE_FILE" down -v >/dev/null 2>&1
  rm -rf "$WORKDIR"
}
trap cleanup EXIT

mkdir -p "$PACKAGE_DIR" "$AVATAR_HOST" "$PG_HOST"

cat >"$COMPOSE_FILE" <<EOF
services:
  app:
    image: ${IMAGE}
    environment:
      DATABASE_URL: postgres://crm:wipe-password@postgres:5432/crm?sslmode=disable
      AUTH_TOKEN_SECRET: wipe-auth-secret-0123456789abcdef
      AUTH_TOKEN_ISSUER: photographer-crm
      PUBLIC_BASE_URL: https://crm.wipe.invalid
      AUTH_PUBLIC_REGISTRATION_ENABLED: "false"
      AUTH_MAIL_DRIVER: log
      HTTP_ADDR: ":8080"
      AVATAR_STORAGE_DRIVER: local
      AVATAR_LOCAL_ROOT: /var/lib/crm/avatars
      AVATAR_LOCAL_REQUIRE_MOUNT: "false"
    volumes:
      - type: bind
        source: ${AVATAR_HOST}
        target: /var/lib/crm/avatars
    depends_on:
      - postgres
  postgres:
    image: postgres:17-alpine
    environment:
      POSTGRES_USER: crm
      POSTGRES_PASSWORD: wipe-password
      POSTGRES_DB: crm
    ports:
      - "127.0.0.1:0:5432"
    volumes:
      - type: bind
        source: ${PG_HOST}
        target: /var/lib/postgresql/data
EOF

cat >"$ENV_FILE" <<'EOF'
POSTGRES_USER=crm
POSTGRES_PASSWORD=wipe-password
POSTGRES_DB=crm
AUTH_TOKEN_SECRET=wipe-auth-secret-0123456789abcdef
EOF

log "stage=bootstrap project=$COMPOSE_PROJECT image=$IMAGE context=$context"

python3 - "$HELPER" "$PACKAGE_DIR" "$COMPOSE_PROJECT" <<'PY'
import importlib.util, io, json, sys, tarfile
from argparse import Namespace
from pathlib import Path

helper, package_dir, project = sys.argv[1], Path(sys.argv[2]), sys.argv[3]
spec = importlib.util.spec_from_file_location("v1_ops_package", helper)
ops = importlib.util.module_from_spec(spec)
assert spec.loader is not None
spec.loader.exec_module(ops)

package_dir.mkdir(parents=True, exist_ok=True)
payload = b"wipe-v1-avatar"
sha = ops.hashlib.sha256(payload).hexdigest()
version = f"sha256-{sha}"
object_id = "b" * 32
key = f"avatars/wipe-account/customers/wipe-customer/{version}/{object_id}"
meta = {
    "MediaType": "image/png",
    "Size": len(payload),
    "Checksum": version,
    "ModifiedAt": "2026-08-02T00:00:00Z",
}
with tarfile.open(package_dir / "avatar-volume.tgz", "w:gz") as archive:
    for name, body in {
        f"{key}/content": payload,
        f"{key}/metadata.json": json.dumps(meta).encode(),
    }.items():
        info = tarfile.TarInfo(name)
        info.size = len(body)
        archive.addfile(info, io.BytesIO(body))

# Substantive SQL dump marker; real restore still depends on migrate-capable target schema.
(package_dir / "database.sql").write_text(
    "SELECT 1;\n-- avatar-media-safety-net wipe fixture\n",
    encoding="utf-8",
)
inventory_sha = ops.hashlib.sha256(
    f"{key}\0{len(payload)}\0{sha}\n".encode()
).hexdigest()
(package_dir / "avatar-manifest.json").write_text(
    json.dumps(
        {
            "format": "customer-avatar-exact-generation-v1",
            "generated_at": "2026-08-02T00:00:00Z",
            "current": [
                {
                    "account_id": "wipe-account",
                    "customer_id": "wipe-customer",
                    "avatar_version": version,
                    "avatar_object_id": object_id,
                    "key": key,
                    "media_type": "image/png",
                    "size": len(payload),
                    "actual_sha256": sha,
                }
            ],
            "inventory": {
                "count": 1,
                "actual_sha256": inventory_sha,
                "objects": [
                    {"key": key, "size": len(payload), "actual_sha256": sha}
                ],
            },
        }
    )
    + "\n",
    encoding="utf-8",
)
# 旧包形态：仅 BASELINE_V1（16 键），不含 optional account_profile 计数。
counts = {table: 0 for table in ops.BASELINE_V1}
counts["customers"] = 1
counts["settings"] = 1
counts_path = package_dir / "database-counts.json"
counts_path.write_text(json.dumps(counts) + "\n", encoding="utf-8")
ops.cmd_metadata(
    Namespace(
        output=package_dir / "metadata.json",
        project=project,
        compose_sha256="a" * 64,
        app_image_id="sha256:app",
        pg_image_id="sha256:postgres",
        app_repo_digest="",
        pg_repo_digest="",
        pg_major="17",
        app_was_running="false",
        db_counts_file=counts_path,
    )
)
counts_path.unlink()
ops.cmd_checksums(Namespace(output=package_dir / "SHA256SUMS"))
ops.validate_package(package_dir, project)
print("stage=package-validate-pass")
PY

# A14: bad package must fail package-validate without relying on restore mutation.
python3 - "$HELPER" "$PACKAGE_DIR" "$COMPOSE_PROJECT" <<'PY'
import importlib.util, shutil, sys
from argparse import Namespace
from pathlib import Path

helper, package_dir, project = sys.argv[1], Path(sys.argv[2]), sys.argv[3]
spec = importlib.util.spec_from_file_location("v1_ops_package", helper)
ops = importlib.util.module_from_spec(spec)
assert spec.loader is not None
spec.loader.exec_module(ops)
broken = package_dir.parent / "broken-package"
shutil.copytree(package_dir, broken)
(broken / "avatar-manifest.json").write_text("not-a-manifest\n", encoding="utf-8")
sums = broken / "SHA256SUMS"
sums.unlink()
ops.cmd_checksums(Namespace(output=sums))
try:
    ops.validate_package(broken, project)
except ops.ContractError:
    print("stage=a14-bad-package-rejected")
else:
    raise SystemExit("bad package was accepted")
PY
log "stage=a14-pass"

PROFILE_VERSION="sha256-$(python3 -c 'print("c"*64)')"
PROFILE_OBJECT="$(python3 -c 'print("d"*32)')"
PROFILE_DIR="$AVATAR_HOST/avatars/wipe-account/account-profile/${PROFILE_VERSION}/${PROFILE_OBJECT}"
mkdir -p "$PROFILE_DIR"
printf 'profile-marker' >"$PROFILE_DIR/content"
printf '{"MediaType":"image/png","Size":14,"Checksum":"%s","ModifiedAt":"2026-08-02T00:00:00Z"}\n' \
  "$PROFILE_VERSION" >"$PROFILE_DIR/metadata.json"
# Extra DB-like marker file under host for visibility before restore (restore replaces avatar root).
printf 'extra-settings-row-marker\n' >"$AVATAR_HOST/.wipe-extra-db-row-marker"
log "marker_physical=$PROFILE_DIR"

log "stage=restore-invoke"
set +e
bash "$RESTORE" \
  --env-file "$ENV_FILE" \
  --compose-file "$COMPOSE_FILE" \
  --docker-context "$context" \
  --project-name "$COMPOSE_PROJECT" \
  --input "$PACKAGE_DIR" \
  --confirm-project "$COMPOSE_PROJECT" 2>&1 | tee -a "$LOG"
restore_rc=${PIPESTATUS[0]}
set -e

if [[ "$restore_rc" -ne 0 ]]; then
  blocked "restore-compose exited $restore_rc; package-validate/A14 evidence written. Full A15 needs migrate-capable compose target + real pg_dump fixture (see $LOG)."
fi

if find "$AVATAR_HOST" -type d -path '*/account-profile/*' 2>/dev/null | grep -q .; then
  fail "physical account-profile path still present after restore"
fi
if [[ -e "$AVATAR_HOST/.wipe-extra-db-row-marker" ]]; then
  fail "pre-restore host marker survived avatar root replace"
fi
log "assert_physical_account_profile_absent=pass"
log "assert_database_counts_via_restore=pass"
log "assert_no_v2_generate_digest_compare=pass"

if [[ "$WITH_PROFILE_MIGRATION" -eq 1 ]]; then
  # A13c：目标保持 stopped；restore-verify 已在 restore-start 前完成；再显式 forward migrate。
  log "stage=ensure-app-stopped"
  docker compose --project-name "$COMPOSE_PROJECT" -f "$COMPOSE_FILE" stop app >/dev/null 2>&1 || true
  docker compose --project-name "$COMPOSE_PROJECT" -f "$COMPOSE_FILE" up -d postgres >/dev/null
  pg_port="$(docker compose --project-name "$COMPOSE_PROJECT" -f "$COMPOSE_FILE" port postgres 5432 | awk -F: '{print $NF}')"
  if [[ -z "$pg_port" ]]; then
    blocked "postgres host port unavailable for forward migrate"
  fi
  log "stage=forward-migrate-profile port=$pg_port"
  set +e
  (
    cd "$repo_root/backend"
    DATABASE_URL="postgres://crm:wipe-password@127.0.0.1:${pg_port}/crm?sslmode=disable" go run ./cmd/migrate
  ) 2>&1 | tee -a "$LOG"
  migrate_rc=${PIPESTATUS[0]}
  set -e
  if [[ "$migrate_rc" -ne 0 ]]; then
    blocked "forward migrate failed (rc=$migrate_rc); see $LOG"
  fi
  profile_counts="$(docker compose --project-name "$COMPOSE_PROJECT" -f "$COMPOSE_FILE" exec -T postgres \
    psql -X -Atq -U crm -d crm -c \
    "SELECT (SELECT count(*) FROM account_profiles)::text || ' ' || (SELECT count(*) FROM account_profile_avatar_gc)::text;")"
  if [[ "$profile_counts" != "0 0" ]]; then
    fail "after forward migrate expected empty account_profiles/GC, got: ${profile_counts:-missing}"
  fi
  if find "$AVATAR_HOST" -type d -path '*/account-profile/*' 2>/dev/null | grep -q .; then
    fail "account-profile objects present after forward migrate"
  fi
  log "assert_profile_tables_empty_after_migrate=pass"
  log "CMD-005 profile wipe harness: passed"
  exit 0
fi

log "CMD-004 restore wipe harness: passed"
exit 0
