#!/usr/bin/env bash
# CMD-002 / A7b：v2 mixed restore rehearsal（customer + account_profile current）。
# 消费既有 validate_package / restore-compose；无臆造 schema_version 或 writer flag。
#
# 前置：Docker Desktop context desktop-linux + 可用 app 镜像（默认 crm:local）。
# 证据：.codestable/features/2026-08-02-account-center-hardening/evidence/restore_fixture_log
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

EVIDENCE_DIR="${AVATAR_MEDIA_V2_MIXED_EVIDENCE:-$repo_root/.codestable/features/2026-08-02-account-center-hardening/evidence}"
mkdir -p "$EVIDENCE_DIR"
LOG="$EVIDENCE_DIR/restore_fixture_log"
: >"$LOG"

log() { printf '%s\n' "$*" | tee -a "$LOG"; }
fail() { log "FAIL: $*"; exit 1; }
blocked() { log "BLOCKED: $*"; exit 2; }

HELPER="$repo_root/scripts/lib/v1-ops-package.py"
RESTORE="$repo_root/scripts/restore-compose.sh"
GOLDEN_DUAL="$repo_root/scripts/lib/testdata/avatar-media-golden/dual-keys.json"
[[ -f "$HELPER" ]] || fail "v1-ops-package.py missing"
[[ -x "$RESTORE" || -f "$RESTORE" ]] || fail "restore-compose.sh missing"
[[ -f "$GOLDEN_DUAL" ]] || fail "dual-keys golden missing"

# 断言齐备：dual-key codec + v2 package validate（不依赖 Docker）
python3 - "$HELPER" "$GOLDEN_DUAL" <<'PY' | tee -a "$LOG"
import importlib.util, json, sys
from pathlib import Path

helper, golden_path = sys.argv[1], Path(sys.argv[2])
spec = importlib.util.spec_from_file_location("v1_ops_package", helper)
ops = importlib.util.module_from_spec(spec)
assert spec.loader is not None
spec.loader.exec_module(ops)

keys = json.loads(golden_path.read_text(encoding="utf-8"))
account_id, kind, subject_id, _, _ = ops.parse_avatar_object_key(keys["customer_key"], "customer")
assert kind == "customer" and account_id == "account-golden" and subject_id == "customer-golden"
account_id, kind, subject_id, _, _ = ops.parse_avatar_object_key(keys["account_profile_key"], "profile")
assert kind == "account_profile" and subject_id == account_id == "account-golden"
print("assert_dual_key_codec=pass")
PY

if ! command -v docker >/dev/null 2>&1; then
  blocked "docker CLI missing (assertions above recorded)"
fi
if ! docker info >/dev/null 2>&1; then
  blocked "docker engine unavailable (start Docker Desktop desktop-linux)"
fi
context="$(docker context show 2>/dev/null || true)"
if [[ "$context" != "desktop-linux" && "${AVATAR_MEDIA_V2_MIXED_ALLOW_ANY_CONTEXT:-}" != "1" ]]; then
  blocked "docker context is '${context:-unknown}', want desktop-linux"
fi

IMAGE="${AVATAR_MEDIA_V2_MIXED_IMAGE:-crm:local}"
if ! docker image inspect "$IMAGE" >/dev/null 2>&1; then
  blocked "app image missing: $IMAGE (build with: docker build -t crm:local .)"
fi

WORKDIR="$(mktemp -d "${TMPDIR:-/tmp}/avatar-media-v2-mixed.XXXXXX")"
COMPOSE_PROJECT="avatar-v2-mixed-$$"
COMPOSE_FILE="$WORKDIR/docker-compose.yml"
ENV_FILE="$WORKDIR/.env"
PACKAGE_DIR="$WORKDIR/v2-package"
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
      DATABASE_URL: postgres://crm:mixed-password@postgres:5432/crm?sslmode=disable
      AUTH_TOKEN_SECRET: mixed-auth-secret-0123456789abcdef
      AUTH_TOKEN_ISSUER: photographer-crm
      PUBLIC_BASE_URL: https://crm.mixed.invalid
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
      POSTGRES_PASSWORD: mixed-password
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
POSTGRES_PASSWORD=mixed-password
POSTGRES_DB=crm
AUTH_TOKEN_SECRET=mixed-auth-secret-0123456789abcdef
EOF

log "stage=bootstrap project=$COMPOSE_PROJECT image=$IMAGE context=$context"

python3 - "$HELPER" "$PACKAGE_DIR" "$COMPOSE_PROJECT" <<'PY' | tee -a "$LOG"
import importlib.util, io, json, sys, tarfile
from argparse import Namespace
from pathlib import Path

helper, package_dir, project = sys.argv[1], Path(sys.argv[2]), sys.argv[3]
spec = importlib.util.spec_from_file_location("v1_ops_package", helper)
ops = importlib.util.module_from_spec(spec)
assert spec.loader is not None
spec.loader.exec_module(ops)

package_dir.mkdir(parents=True, exist_ok=True)
account = "mixed-account"
cust_payload = b"mixed-v2-customer"
prof_payload = b"mixed-v2-profile"
cust_sha = ops.hashlib.sha256(cust_payload).hexdigest()
prof_sha = ops.hashlib.sha256(prof_payload).hexdigest()
cust_version = f"sha256-{cust_sha}"
prof_version = f"sha256-{prof_sha}"
cust_object = "c" * 32
prof_object = "d" * 32
cust_key = f"avatars/{account}/customers/mixed-customer/{cust_version}/{cust_object}"
prof_key = f"avatars/{account}/account-profile/{prof_version}/{prof_object}"

def meta(version: str, size: int) -> dict:
    return {
        "MediaType": "image/png",
        "Size": size,
        "Checksum": version,
        "ModifiedAt": "2026-08-02T00:00:00Z",
    }

entries = {
    f"{prof_key}/content": prof_payload,
    f"{prof_key}/metadata.json": json.dumps(meta(prof_version, len(prof_payload))).encode(),
    f"{cust_key}/content": cust_payload,
    f"{cust_key}/metadata.json": json.dumps(meta(cust_version, len(cust_payload))).encode(),
}
with tarfile.open(package_dir / "avatar-volume.tgz", "w:gz") as archive:
    for name, body in entries.items():
        info = tarfile.TarInfo(name)
        info.size = len(body)
        archive.addfile(info, io.BytesIO(body))

(package_dir / "database.sql").write_text(
    "SELECT 1;\n-- account-center-hardening v2 mixed fixture\n",
    encoding="utf-8",
)

# inventory objects must be key-sorted: account-profile before customers
objects = [
    {"key": prof_key, "size": len(prof_payload), "actual_sha256": prof_sha},
    {"key": cust_key, "size": len(cust_payload), "actual_sha256": cust_sha},
]
summary = ops.hashlib.sha256()
for item in objects:
    summary.update(f"{item['key']}\0{item['size']}\0{item['actual_sha256']}\n".encode())
inventory_sha = summary.hexdigest()

# current identity order: (account_id, subject_kind, subject_id, key)
current = [
    {
        "account_id": account,
        "subject_kind": "account_profile",
        "subject_id": account,
        "avatar_version": prof_version,
        "avatar_object_id": prof_object,
        "key": prof_key,
        "media_type": "image/png",
        "size": len(prof_payload),
        "actual_sha256": prof_sha,
    },
    {
        "account_id": account,
        "subject_kind": "customer",
        "subject_id": "mixed-customer",
        "avatar_version": cust_version,
        "avatar_object_id": cust_object,
        "key": cust_key,
        "media_type": "image/png",
        "size": len(cust_payload),
        "actual_sha256": cust_sha,
    },
]

(package_dir / "avatar-manifest.json").write_text(
    json.dumps(
        {
            "format": "avatar-exact-generation-v2",
            "generated_at": "2026-08-02T00:00:00Z",
            "current": current,
            "inventory": {
                "count": 2,
                "actual_sha256": inventory_sha,
                "objects": objects,
            },
        }
    )
    + "\n",
    encoding="utf-8",
)

counts = {table: 0 for table in ops.BASELINE_V1}
counts["customers"] = 1
counts["settings"] = 1
counts["account_profiles"] = 0
counts["account_profile_avatar_gc"] = 0
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
print("stage=v2-mixed-package-validate-pass")
print(f"customer_key={cust_key}")
print(f"account_profile_key={prof_key}")
PY

# Seed host with distractors that restore must replace.
mkdir -p "$AVATAR_HOST/avatars/stale/account-profile/sha256-stale/staleobj"
printf 'stale\n' >"$AVATAR_HOST/avatars/stale/account-profile/sha256-stale/staleobj/content"

log "stage=restore-invoke"
set +e
IMAGE="$IMAGE" AVATAR_HOST="$AVATAR_HOST" bash "$RESTORE" \
  --env-file "$ENV_FILE" \
  --compose-file "$COMPOSE_FILE" \
  --docker-context "$context" \
  --project-name "$COMPOSE_PROJECT" \
  --input "$PACKAGE_DIR" \
  --confirm-project "$COMPOSE_PROJECT" 2>&1 | tee -a "$LOG"
restore_rc=${PIPESTATUS[0]}
set -e

if [[ "$restore_rc" -ne 0 ]]; then
  blocked "restore-compose exited $restore_rc; v2 package-validate evidence written. Full A7b needs migrate-capable compose target (see $LOG)."
fi

if ! find "$AVATAR_HOST" -type f -path '*/customers/mixed-customer/*/content' 2>/dev/null | grep -q .; then
  fail "customer current object missing after v2 mixed restore"
fi
if ! find "$AVATAR_HOST" -type f -path '*/account-profile/*/content' 2>/dev/null | grep -q .; then
  fail "account_profile current object missing after v2 mixed restore"
fi
if find "$AVATAR_HOST" -type d -path '*/avatars/stale/*' 2>/dev/null | grep -q .; then
  fail "pre-restore stale path survived avatar root replace"
fi

log "assert_customer_current_present=pass"
log "assert_account_profile_current_present=pass"
log "CMD-002 v2 mixed restore harness: passed"
exit 0
