#!/usr/bin/env python3
"""Private data/JSON helper for the V1 compose backup/restore shell CLIs.

The public scripts deliberately keep secrets and machine paths out of their
output.  This helper therefore reports only stable error keys on failure.
"""

from __future__ import annotations

import argparse
import ctypes
import errno
import hashlib
import json
import os
import platform
import re
import shutil
import stat
import sys
import tarfile
from datetime import datetime, timezone
from pathlib import Path
from pathlib import PurePosixPath
from typing import Any
from urllib.parse import quote


PACKAGE_FILES = {
    "database.sql",
    "avatar-volume.tgz",
    "avatar-manifest.json",
    "metadata.json",
    "SHA256SUMS",
}
PAYLOADS = ("database.sql", "avatar-volume.tgz", "avatar-manifest.json")
HEX64 = re.compile(r"^[0-9a-f]{64}$")
PROJECT = re.compile(r"^[a-z0-9][a-z0-9_-]*$")
UTC_RFC3339 = re.compile(r"^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}(?:\.[0-9]+)?Z$")
AVATAR_SEGMENT = re.compile(r"^[A-Za-z0-9_-]+$")
AVATAR_OBJECT_ID = re.compile(r"^[0-9a-f]{32}$")
AVATAR_MEDIA_TYPES = {"image/jpeg", "image/png", "image/webp"}
MANIFEST_FORMAT_V1 = "customer-avatar-exact-generation-v1"
MANIFEST_FORMAT_V2 = "avatar-exact-generation-v2"
MANIFEST_FORMAT_ALLOWLIST = frozenset({MANIFEST_FORMAT_V1, MANIFEST_FORMAT_V2})
MAX_AVATAR_CONTENT_BYTES = 5 * 1024 * 1024
MAX_AVATAR_METADATA_BYTES = 4096
# BASELINE_V1：历史 16 表强制下界；OPTIONAL_ACCOUNT_PROFILE 为兼容扩展。
BASELINE_V1 = (
    "accounts",
    "avatar_object_gc",
    "avatar_reconciliation_checkpoint",
    "customer_notes",
    "customers",
    "idempotency_records",
    "orders",
    "packages",
    "reminder_scan_state",
    "reminders",
    "schedule_slots",
    "schema_migrations",
    "settings",
    "social_identities",
    "telegram_bind_tokens",
    "telegram_deliveries",
)
OPTIONAL_ACCOUNT_PROFILE = (
    "account_profiles",
    "account_profile_avatar_gc",
)
SQL_COUNT_TABLES = tuple(sorted(set(BASELINE_V1) | set(OPTIONAL_ACCOUNT_PROFILE)))
# 兼容旧引用名：完整可计数集合（含可选表）。
DATABASE_COUNT_TABLES = SQL_COUNT_TABLES
METADATA_FIELDS = {
    "schema_version",
    "created_at",
    "payloads",
    "manifest_schema_version",
    "source_compose_project",
    "compose_file_sha256",
    "images",
    "postgres_server_major",
    "app_was_running",
    "database_counts",
}


class ContractError(Exception):
    pass


def fail(key: str) -> None:
    print(f"v1-ops-helper error key={key}", file=sys.stderr)
    raise SystemExit(1)


def sha256_file(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def load_one_json(path: Path) -> Any:
    with path.open("r", encoding="utf-8") as stream:
        return json.load(stream)


def require_regular(path: Path) -> None:
    info = path.lstat()
    if not stat.S_ISREG(info.st_mode) or path.is_symlink():
        raise ContractError("artifact-not-regular")


def require_int(value: Any, key: str) -> int:
    if isinstance(value, bool) or not isinstance(value, int) or value < 0:
        raise ContractError(key)
    return value


def parse_utc_timestamp(value: Any, key: str) -> None:
    if not isinstance(value, str) or not UTC_RFC3339.fullmatch(value):
        raise ContractError(key)
    try:
        parsed = datetime.fromisoformat(value.replace("Z", "+00:00"))
    except ValueError as error:
        raise ContractError(key) from error
    if parsed.utcoffset() != timezone.utc.utcoffset(parsed):
        raise ContractError(key)


def validate_database_counts(value: Any) -> dict[str, int]:
    """键集合必须 ⊆ SQL_COUNT_TABLES 且 ⊇ BASELINE_V1（16/18 键均合法）。"""
    if not isinstance(value, dict):
        raise ContractError("database-counts-schema")
    keys = set(value)
    allowed = set(SQL_COUNT_TABLES)
    baseline = set(BASELINE_V1)
    if not keys.issubset(allowed) or not baseline.issubset(keys):
        raise ContractError("database-counts-schema")
    return {
        table: require_int(value[table], "database-counts-value")
        for table in sorted(keys)
    }


def validate_relative_key(value: Any, key: str) -> str:
    if not isinstance(value, str) or not value or "\\" in value:
        raise ContractError(key)
    path = PurePosixPath(value)
    if path.is_absolute() or value.startswith("./") or any(part in ("", ".", "..") for part in path.parts):
        raise ContractError(key)
    return value


def parse_avatar_object_key(value: Any, error_key: str) -> tuple[str, str, str, str, str]:
    """Return (account_id, subject_kind, subject_id, version, object_id)."""
    key = validate_relative_key(value, error_key)
    parts = key.split("/")
    if (
        len(parts) == 6
        and parts[0] == "avatars"
        and parts[2] == "customers"
        and AVATAR_SEGMENT.fullmatch(parts[1])
        and AVATAR_SEGMENT.fullmatch(parts[3])
        and re.fullmatch(r"sha256-[0-9a-f]{64}", parts[4])
        and AVATAR_OBJECT_ID.fullmatch(parts[5])
    ):
        return parts[1], "customer", parts[3], parts[4], parts[5]
    if (
        len(parts) == 5
        and parts[0] == "avatars"
        and parts[2] == "account-profile"
        and AVATAR_SEGMENT.fullmatch(parts[1])
        and re.fullmatch(r"sha256-[0-9a-f]{64}", parts[3])
        and AVATAR_OBJECT_ID.fullmatch(parts[4])
    ):
        return parts[1], "account_profile", parts[1], parts[3], parts[4]
    raise ContractError(error_key)


def load_json_reject_duplicate_keys(path: Path) -> Any:
    raw = path.read_text(encoding="utf-8")
    decoder = json.JSONDecoder(object_pairs_hook=_reject_duplicate_pairs)
    value, index = decoder.raw_decode(raw)
    if raw[index:].strip():
        raise ContractError("manifest-trailing")
    return value


def _reject_duplicate_pairs(pairs: list[tuple[str, Any]]) -> dict[str, Any]:
    result: dict[str, Any] = {}
    for key, value in pairs:
        if key in result:
            raise ContractError("manifest-duplicate-key")
        result[key] = value
    return result


def read_env(path: Path) -> dict[str, str]:
    values: dict[str, str] = {}
    with path.open("r", encoding="utf-8") as stream:
        for raw in stream:
            line = raw.rstrip("\r\n")
            stripped = line.strip()
            if not stripped or stripped.startswith("#"):
                continue
            if "=" not in line:
                raise ContractError("env-syntax")
            key, value = line.split("=", 1)
            if not re.fullmatch(r"[A-Za-z_][A-Za-z0-9_]*", key) or key in values:
                raise ContractError("env-key")
            if len(value) >= 2 and value[0] == value[-1] and value[0] in "\"'":
                value = value[1:-1]
            elif value.startswith(("\"", "'")) or value.endswith(("\"", "'")):
                raise ContractError("env-quote")
            values[key] = value
    return values


def cmd_env_runtime(args: argparse.Namespace) -> None:
    values = read_env(args.env_file)
    user = values.get("POSTGRES_USER", "")
    password = values.get("POSTGRES_PASSWORD", "")
    database = values.get("POSTGRES_DB", "")
    auth_secret = values.get("AUTH_TOKEN_SECRET", "")
    if not user or not password or not database:
        raise ContractError("postgres-env")
    if not auth_secret:
        raise ContractError("auth-env")
    destination = args.output
    old_umask = os.umask(0o077)
    try:
        with destination.open("x", encoding="utf-8") as stream:
            stream.write(f"POSTGRES_USER={user}\n")
            stream.write(f"POSTGRES_DB={database}\n")
            stream.write(
                "DATABASE_URL=postgres://"
                f"{quote(user, safe='')}:{quote(password, safe='')}@postgres:5432/"
                f"{quote(database, safe='')}?sslmode=disable\n"
            )
            stream.write(f"AUTH_TOKEN_SECRET={auth_secret}\n")
            stream.write("AVATAR_STORAGE_DRIVER=local\n")
            stream.write("AVATAR_LOCAL_ROOT=/var/lib/crm/avatars\n")
            stream.write("AVATAR_LOCAL_REQUIRE_MOUNT=true\n")
    finally:
        os.umask(old_umask)


def cmd_env_get(args: argparse.Namespace) -> None:
    values = read_env(args.env_file)
    value = values.get(args.key, "")
    if not value:
        raise ContractError("env-key-missing")
    sys.stdout.write(value)


def cmd_render_info(_: argparse.Namespace) -> None:
    document = json.load(sys.stdin)
    services = document.get("services")
    volumes = document.get("volumes")
    if not isinstance(services, dict) or set(("app", "postgres")) - set(services):
        raise ContractError("render-services")
    if not isinstance(volumes, dict) or set(("avatar_data", "pgdata")) - set(volumes):
        raise ContractError("render-volumes")
    app = services["app"]
    postgres = services["postgres"]
    app_image = app.get("image")
    pg_image = postgres.get("image")
    if not isinstance(app_image, str) or not app_image or not isinstance(pg_image, str) or not pg_image:
        raise ContractError("render-images")
    app_mounts = app.get("volumes", [])
    pg_mounts = postgres.get("volumes", [])

    def has_mount(items: Any, source: str, target: str) -> bool:
        return isinstance(items, list) and any(
            isinstance(item, dict)
            and item.get("type") == "volume"
            and item.get("source") == source
            and item.get("target") == target
            for item in items
        )

    if not has_mount(app_mounts, "avatar_data", "/var/lib/crm/avatars"):
        raise ContractError("render-avatar-mount")
    if not has_mount(pg_mounts, "pgdata", "/var/lib/postgresql/data"):
        raise ContractError("render-pg-mount")
    print(app_image)
    print(pg_image)


def cmd_container_invariant(args: argparse.Namespace) -> None:
    documents = json.load(sys.stdin)
    if not isinstance(documents, list) or len(documents) != 1:
        raise ContractError("inspect-shape")
    item = documents[0]
    labels = item.get("Config", {}).get("Labels") or {}
    state = item.get("State") or {}
    expected_labels = dict(pair.split("=", 1) for pair in args.label)
    if item.get("Id") != args.container_id:
        raise ContractError("inspect-id")
    if item.get("Name") != f"/{args.name}":
        raise ContractError("inspect-name")
    if any(labels.get(key) != value for key, value in expected_labels.items()):
        raise ContractError("inspect-label")
    if state.get("Status") != "created" or state.get("Running") is not False:
        raise ContractError("inspect-state")
    if state.get("StartedAt") not in ("", "0001-01-01T00:00:00Z"):
        raise ContractError("inspect-started")
    if item.get("HostConfig", {}).get("NetworkMode") != "none":
        raise ContractError("inspect-network")
    if item.get("Mounts") != []:
        raise ContractError("inspect-mount")


def cmd_app_state(_: argparse.Namespace) -> None:
    documents = json.load(sys.stdin)
    if not isinstance(documents, list) or len(documents) != 1:
        raise ContractError("app-inspect-shape")
    state = documents[0].get("State") or {}
    status = state.get("Status")
    running = state.get("Running")
    paused = state.get("Paused")
    restarting = state.get("Restarting")
    dead = state.get("Dead")
    if status == "running" and running is True and paused is False and restarting is False and dead is False:
        print("running")
        return
    if status == "exited" and running is False and paused is False and restarting is False and dead is False:
        print("exited")
        return
    raise ContractError("unsupported-app-state")


def cmd_runtime_check(args: argparse.Namespace) -> None:
    document = json.load(sys.stdin)
    if document.get("app_image_id") != document.get("runtime_app_image_id"):
        raise ContractError("runtime-app-image")
    if document.get("pg_image_id") != document.get("runtime_pg_image_id"):
        raise ContractError("runtime-pg-image")
    mounts = document.get("mounts")
    if not isinstance(mounts, dict):
        raise ContractError("runtime-mounts")
    if mounts.get("avatar") != args.avatar_volume or mounts.get("pgdata") != args.pg_volume:
        raise ContractError("runtime-volume")


def validate_avatar_metadata(value: Any, content: dict[str, Any], version: str) -> dict[str, Any]:
    fields = {"MediaType", "Size", "Checksum", "ModifiedAt"}
    if not isinstance(value, dict) or set(value) != fields:
        raise ContractError("tar-object-metadata-schema")
    media_type = value.get("MediaType")
    if media_type not in AVATAR_MEDIA_TYPES:
        raise ContractError("tar-object-metadata-media-type")
    size = require_int(value.get("Size"), "tar-object-metadata-size")
    if size == 0 or size > MAX_AVATAR_CONTENT_BYTES or size != content["size"]:
        raise ContractError("tar-object-metadata-size")
    checksum = value.get("Checksum")
    if checksum != version or checksum != f"sha256-{content['actual_sha256']}":
        raise ContractError("tar-object-metadata-checksum")
    parse_utc_timestamp(value.get("ModifiedAt"), "tar-object-metadata-modified-at")
    return {"media_type": media_type, "size": size, "actual_sha256": content["actual_sha256"]}


def tar_inventory(path: Path) -> tuple[list[dict[str, Any]], dict[str, dict[str, Any]]]:
    object_leaves: dict[str, dict[str, Any]] = {}
    seen: dict[str, str] = {}
    with tarfile.open(path, "r:gz") as archive:
        for member in archive:
            name = member.name
            while name.startswith("./"):
                name = name[2:]
            if name in ("", "."):
                if member.isdir():
                    continue
                raise ContractError("tar-path")
            canonical_name = name.rstrip("/") if member.isdir() else name
            validate_relative_key(canonical_name, "tar-path")
            kind = "directory" if member.isdir() else "file"
            if canonical_name in seen:
                raise ContractError("tar-duplicate")
            seen[canonical_name] = kind
            if member.isdir():
                continue
            if not member.isreg():
                raise ContractError("tar-type")
            parts = canonical_name.split("/")
            if len(parts) < 2 or parts[-1] not in {"content", "metadata.json"}:
                raise ContractError("tar-object-leaf")
            object_key = "/".join(parts[:-1])
            _, _, _, version, _ = parse_avatar_object_key(object_key, "tar-object-key")
            stream = archive.extractfile(member)
            if stream is None:
                raise ContractError("tar-read")
            leaves = object_leaves.setdefault(object_key, {"version": version})
            leaf = parts[-1]
            if leaf in leaves:
                raise ContractError("tar-object-duplicate-leaf")
            if leaf == "content":
                if member.size == 0 or member.size > MAX_AVATAR_CONTENT_BYTES:
                    raise ContractError("tar-object-content-size")
                digest = hashlib.sha256()
                size = 0
                for chunk in iter(lambda: stream.read(1024 * 1024), b""):
                    size += len(chunk)
                    if size > MAX_AVATAR_CONTENT_BYTES:
                        raise ContractError("tar-object-content-size")
                    digest.update(chunk)
                if size != member.size:
                    raise ContractError("tar-size")
                leaves[leaf] = {"size": size, "actual_sha256": digest.hexdigest()}
            else:
                if member.size == 0 or member.size > MAX_AVATAR_METADATA_BYTES:
                    raise ContractError("tar-object-metadata-size")
                raw = stream.read(MAX_AVATAR_METADATA_BYTES + 1)
                if len(raw) != member.size:
                    raise ContractError("tar-size")
                try:
                    leaves[leaf] = json.loads(raw.decode("utf-8"))
                except (UnicodeError, json.JSONDecodeError) as error:
                    raise ContractError("tar-object-metadata-json") from error

    inventory: list[dict[str, Any]] = []
    metadata_by_key: dict[str, dict[str, Any]] = {}
    for object_key in sorted(object_leaves):
        leaves = object_leaves[object_key]
        if set(leaves) != {"version", "content", "metadata.json"}:
            raise ContractError("tar-object-incomplete")
        metadata = validate_avatar_metadata(leaves["metadata.json"], leaves["content"], leaves["version"])
        metadata_by_key[object_key] = metadata
        inventory.append({
            "key": object_key,
            "size": metadata["size"],
            "actual_sha256": metadata["actual_sha256"],
        })
    return inventory, metadata_by_key


def validate_manifest(package: Path, expected_format: str) -> dict[str, Any]:
    if expected_format not in MANIFEST_FORMAT_ALLOWLIST:
        raise ContractError("manifest-format")
    manifest = load_json_reject_duplicate_keys(package / "avatar-manifest.json")
    if not isinstance(manifest, dict) or set(manifest) != {"format", "generated_at", "current", "inventory"}:
        raise ContractError("manifest-schema")
    if manifest.get("format") != expected_format:
        raise ContractError("manifest-format")
    parse_utc_timestamp(manifest.get("generated_at"), "manifest-generated-at")

    inventory = manifest.get("inventory")
    if not isinstance(inventory, dict) or set(inventory) != {"count", "actual_sha256", "objects"}:
        raise ContractError("manifest-inventory-schema")
    objects = inventory.get("objects")
    if not isinstance(objects, list):
        raise ContractError("manifest-inventory-objects")
    normalized: list[dict[str, Any]] = []
    previous = ""
    for item in objects:
        if not isinstance(item, dict) or set(item) != {"key", "size", "actual_sha256"}:
            raise ContractError("manifest-object-schema")
        object_key = validate_relative_key(item.get("key"), "manifest-object-key")
        account_id, subject_kind, _, object_version, _ = parse_avatar_object_key(
            object_key, "manifest-object-key",
        )
        if expected_format == MANIFEST_FORMAT_V1 and subject_kind != "customer":
            raise ContractError("manifest-object-key")
        if previous and object_key <= previous:
            raise ContractError("manifest-object-order")
        previous = object_key
        size = require_int(item.get("size"), "manifest-object-size")
        checksum = item.get("actual_sha256")
        if not isinstance(checksum, str) or not HEX64.fullmatch(checksum):
            raise ContractError("manifest-object-checksum")
        if object_version != f"sha256-{checksum}":
            raise ContractError("manifest-object-version")
        _ = account_id
        normalized.append({"key": object_key, "size": size, "actual_sha256": checksum})
    if require_int(inventory.get("count"), "manifest-inventory-count") != len(normalized):
        raise ContractError("manifest-inventory-count")
    summary = hashlib.sha256()
    for item in normalized:
        summary.update(f"{item['key']}\0{item['size']}\0{item['actual_sha256']}\n".encode())
    if inventory.get("actual_sha256") != summary.hexdigest():
        raise ContractError("manifest-inventory-checksum")
    tar_objects, metadata_by_key = tar_inventory(package / "avatar-volume.tgz")
    if tar_objects != normalized:
        raise ContractError("manifest-tar-mismatch")

    current = manifest.get("current")
    if not isinstance(current, list):
        raise ContractError("manifest-current")
    inventory_by_key = {item["key"]: item for item in normalized}
    if expected_format == MANIFEST_FORMAT_V1:
        previous_identity: tuple[Any, ...] | None = None
        for item in current:
            fields = {
                "account_id", "customer_id", "avatar_version", "avatar_object_id",
                "key", "media_type", "size", "actual_sha256",
            }
            if not isinstance(item, dict) or set(item) != fields:
                raise ContractError("manifest-current-schema")
            for field in ("account_id", "customer_id", "avatar_object_id", "media_type"):
                if not isinstance(item.get(field), str) or not item[field]:
                    raise ContractError("manifest-current-value")
            object_key = validate_relative_key(item.get("key"), "manifest-current-key")
            account_id, subject_kind, subject_id, object_version, object_id = parse_avatar_object_key(
                object_key, "manifest-current-key",
            )
            if subject_kind != "customer":
                raise ContractError("manifest-current-key")
            checksum = item.get("actual_sha256")
            if not isinstance(checksum, str) or not HEX64.fullmatch(checksum):
                raise ContractError("manifest-current-checksum")
            if item.get("avatar_version") != f"sha256-{checksum}":
                raise ContractError("manifest-current-version")
            if (
                account_id != item["account_id"]
                or subject_id != item["customer_id"]
                or object_version != item["avatar_version"]
                or object_id != item["avatar_object_id"]
            ):
                raise ContractError("manifest-current-key-identity")
            size = require_int(item.get("size"), "manifest-current-size")
            identity = (item["account_id"], item["customer_id"])
            if previous_identity is not None and identity <= previous_identity:
                raise ContractError("manifest-current-order")
            previous_identity = identity
            physical = inventory_by_key.get(object_key)
            if physical is None or physical["size"] != size or physical["actual_sha256"] != checksum:
                raise ContractError("manifest-current-inventory")
            if metadata_by_key[object_key]["media_type"] != item["media_type"]:
                raise ContractError("manifest-current-media-type")
        return manifest

    previous_identity = None
    seen_subjects: set[tuple[str, str, str]] = set()
    for item in current:
        fields = {
            "account_id", "subject_kind", "subject_id", "avatar_version", "avatar_object_id",
            "key", "media_type", "size", "actual_sha256",
        }
        if not isinstance(item, dict) or set(item) != fields:
            raise ContractError("manifest-current-schema")
        if "customer_id" in item:
            raise ContractError("manifest-current-schema")
        for field in ("account_id", "subject_kind", "subject_id", "avatar_object_id", "media_type"):
            if not isinstance(item.get(field), str) or not item[field]:
                raise ContractError("manifest-current-value")
        if item["subject_kind"] not in {"customer", "account_profile"}:
            raise ContractError("manifest-current-subject-kind")
        object_key = validate_relative_key(item.get("key"), "manifest-current-key")
        account_id, subject_kind, subject_id, object_version, object_id = parse_avatar_object_key(
            object_key, "manifest-current-key",
        )
        checksum = item.get("actual_sha256")
        if not isinstance(checksum, str) or not HEX64.fullmatch(checksum):
            raise ContractError("manifest-current-checksum")
        if item.get("avatar_version") != f"sha256-{checksum}":
            raise ContractError("manifest-current-version")
        if (
            account_id != item["account_id"]
            or subject_kind != item["subject_kind"]
            or subject_id != item["subject_id"]
            or object_version != item["avatar_version"]
            or object_id != item["avatar_object_id"]
        ):
            raise ContractError("manifest-current-key-identity")
        if subject_kind == "account_profile" and subject_id != account_id:
            raise ContractError("manifest-current-subject-id")
        size = require_int(item.get("size"), "manifest-current-size")
        identity = (item["account_id"], item["subject_kind"], item["subject_id"], item["key"])
        if previous_identity is not None and identity <= previous_identity:
            raise ContractError("manifest-current-order")
        previous_identity = identity
        subject = (item["account_id"], item["subject_kind"], item["subject_id"])
        if subject in seen_subjects:
            raise ContractError("manifest-current-duplicate-subject")
        seen_subjects.add(subject)
        physical = inventory_by_key.get(object_key)
        if physical is None or physical["size"] != size or physical["actual_sha256"] != checksum:
            raise ContractError("manifest-current-inventory")
        if metadata_by_key[object_key]["media_type"] != item["media_type"]:
            raise ContractError("manifest-current-media-type")
    return manifest


def validate_database_dump(path: Path) -> None:
    if path.stat().st_size == 0:
        raise ContractError("database-dump-empty")
    state = "normal"
    carry = ""
    substantive = False
    with path.open("r", encoding="utf-8") as stream:
        while True:
            chunk = stream.read(64 * 1024)
            if not chunk:
                break
            data = carry + chunk
            carry = ""
            index = 0
            while index < len(data):
                character = data[index]
                following = data[index + 1] if index + 1 < len(data) else None
                if state == "line-comment":
                    if character in "\r\n":
                        state = "normal"
                    index += 1
                    continue
                if state == "block-comment":
                    if character == "*" and following == "/":
                        state = "normal"
                        index += 2
                    elif following is None and character == "*":
                        carry = character
                        break
                    else:
                        index += 1
                    continue
                if character == "\x00":
                    raise ContractError("database-dump-binary")
                if following is None and character in ("-", "/"):
                    carry = character
                    break
                if character == "-" and following == "-":
                    state = "line-comment"
                    index += 2
                    continue
                if character == "/" and following == "*":
                    state = "block-comment"
                    index += 2
                    continue
                if character.isalpha() or character == "\\":
                    substantive = True
                index += 1
    if state == "normal" and any(character.isalpha() or character == "\\" for character in carry):
        substantive = True
    if not substantive:
        raise ContractError("database-dump-empty")


def validate_metadata(package: Path, project: str) -> dict[str, Any]:
    metadata = load_one_json(package / "metadata.json")
    if not isinstance(metadata, dict) or set(metadata) != METADATA_FIELDS or metadata.get("schema_version") != 1:
        raise ContractError("metadata-schema")
    if metadata.get("source_compose_project") != project:
        raise ContractError("metadata-project")
    parse_utc_timestamp(metadata.get("created_at"), "metadata-created-at")
    payloads = metadata.get("payloads")
    if not isinstance(payloads, dict) or set(payloads) != set(PAYLOADS):
        raise ContractError("metadata-payloads")
    for filename in PAYLOADS:
        record = payloads.get(filename)
        if not isinstance(record, dict) or set(record) != {"filename", "sha256"}:
            raise ContractError("metadata-payload-record")
        if record.get("filename") != filename or not HEX64.fullmatch(str(record.get("sha256", ""))):
            raise ContractError("metadata-payload-value")
        if record["sha256"] != sha256_file(package / filename):
            raise ContractError("metadata-payload-checksum")
    manifest_schema = metadata.get("manifest_schema_version")
    if manifest_schema not in MANIFEST_FORMAT_ALLOWLIST:
        raise ContractError("metadata-manifest-schema")
    manifest = load_json_reject_duplicate_keys(package / "avatar-manifest.json")
    if not isinstance(manifest, dict) or manifest.get("format") != manifest_schema:
        raise ContractError("metadata-manifest-schema")
    if not HEX64.fullmatch(str(metadata.get("compose_file_sha256", ""))):
        raise ContractError("metadata-compose-sha")
    images = metadata.get("images")
    if not isinstance(images, dict) or set(images) != {"app", "postgres"}:
        raise ContractError("metadata-images")
    for image in images.values():
        if not isinstance(image, dict) or not isinstance(image.get("id"), str) or not image["id"]:
            raise ContractError("metadata-image-id")
        if set(image) - {"id", "repo_digest"}:
            raise ContractError("metadata-image-fields")
        if "repo_digest" in image and (not isinstance(image["repo_digest"], str) or not image["repo_digest"]):
            raise ContractError("metadata-image-digest")
    if not isinstance(metadata.get("postgres_server_major"), str) or not re.fullmatch(r"[0-9]+", metadata["postgres_server_major"]):
        raise ContractError("metadata-postgres-major")
    if not isinstance(metadata.get("app_was_running"), bool):
        raise ContractError("metadata-app-state")
    serialized = json.dumps(metadata, ensure_ascii=False)
    if serialized.startswith("/") or re.search(r'"/[^"]+', serialized):
        raise ContractError("metadata-absolute-path")
    metadata["database_counts"] = validate_database_counts(metadata.get("database_counts"))
    return metadata


def validate_checksums(package: Path) -> None:
    lines = (package / "SHA256SUMS").read_text(encoding="utf-8").splitlines()
    observed: dict[str, str] = {}
    for line in lines:
        match = re.fullmatch(r"([0-9a-f]{64})  ([A-Za-z0-9._-]+)", line)
        if not match or match.group(2) == "SHA256SUMS" or match.group(2) in observed:
            raise ContractError("checksums-format")
        observed[match.group(2)] = match.group(1)
    expected = PACKAGE_FILES - {"SHA256SUMS"}
    if set(observed) != expected:
        raise ContractError("checksums-membership")
    for filename, checksum in observed.items():
        if sha256_file(package / filename) != checksum:
            raise ContractError("checksums-mismatch")


def validate_package(package: Path, project: str) -> None:
    if not PROJECT.fullmatch(project):
        raise ContractError("project")
    if package.is_symlink() or not package.is_dir():
        raise ContractError("package-directory")
    members = {entry.name for entry in package.iterdir()}
    if members != PACKAGE_FILES:
        raise ContractError("package-membership")
    for filename in PACKAGE_FILES:
        require_regular(package / filename)
    validate_checksums(package)
    metadata = validate_metadata(package, project)
    validate_database_dump(package / "database.sql")
    validate_manifest(package, metadata["manifest_schema_version"])


def cmd_snapshot(args: argparse.Namespace) -> None:
    source = args.input
    destination = args.output
    if destination.exists() or destination.is_symlink():
        raise ContractError("snapshot-destination")
    if source.is_symlink() or not source.is_dir():
        raise ContractError("snapshot-source")
    before: dict[str, tuple[int, int, int, int]] = {}
    members = {entry.name for entry in source.iterdir()}
    if members != PACKAGE_FILES:
        raise ContractError("package-membership")
    old_umask = os.umask(0o077)
    try:
        destination.mkdir(mode=0o700)
        for filename in sorted(PACKAGE_FILES):
            path = source / filename
            require_regular(path)
            info = path.stat()
            before[filename] = (info.st_dev, info.st_ino, info.st_size, info.st_mtime_ns)
            with path.open("rb") as reader, (destination / filename).open("xb") as writer:
                shutil.copyfileobj(reader, writer, 1024 * 1024)
                writer.flush()
                os.fsync(writer.fileno())
        for filename, signature in before.items():
            info = (source / filename).stat()
            if (info.st_dev, info.st_ino, info.st_size, info.st_mtime_ns) != signature:
                raise ContractError("input-changed")
        validate_package(destination, args.project)
    except Exception:
        shutil.rmtree(destination, ignore_errors=True)
        raise
    finally:
        os.umask(old_umask)


def cmd_validate(args: argparse.Namespace) -> None:
    validate_package(args.input, args.project)


def cmd_db_counts_sql(_: argparse.Namespace) -> None:
    # 无参：恒定输出 SQL_COUNT_TABLES；缺表时 to_regclass 守卫计 0（禁止跳过）。
    pairs = ",\n  ".join(
        (
            f"'{table}', CASE WHEN to_regclass('public.{table}') IS NULL THEN 0 "
            f"ELSE (SELECT count(*)::bigint FROM public.{table}) END"
        )
        for table in SQL_COUNT_TABLES
    )
    print(f"SELECT json_build_object(\n  {pairs}\n)::text;")


def project_counts_to_keys(counts: dict[str, int], keys: set[str]) -> dict[str, int]:
    return {table: counts[table] for table in sorted(keys) if table in counts}


def cmd_verify_db_counts(args: argparse.Namespace) -> None:
    metadata = load_one_json(args.metadata)
    if not isinstance(metadata, dict):
        raise ContractError("metadata-schema")
    expected = validate_database_counts(metadata.get("database_counts"))
    actual_raw = load_one_json(args.actual)
    if not isinstance(actual_raw, dict):
        raise ContractError("database-counts-schema")
    # actual 可含额外可选键；只按包自述键比对。
    actual_projected = project_counts_to_keys(
        {table: require_int(actual_raw[table], "database-counts-value") for table in actual_raw if table in set(SQL_COUNT_TABLES)},
        set(expected),
    )
    if set(actual_projected) != set(expected) or actual_projected != expected:
        raise ContractError("database-counts-mismatch")


def fsync_package(package: Path) -> None:
    for filename in sorted(PACKAGE_FILES):
        with (package / filename).open("rb") as stream:
            os.fsync(stream.fileno())
    descriptor = os.open(package, os.O_RDONLY | getattr(os, "O_DIRECTORY", 0))
    try:
        os.fsync(descriptor)
    finally:
        os.close(descriptor)


def rename_no_replace(source: Path, destination: Path) -> None:
    libc = ctypes.CDLL(None, use_errno=True)
    source_bytes = os.fsencode(source)
    destination_bytes = os.fsencode(destination)
    system = platform.system()
    if system == "Linux" and hasattr(libc, "renameat2"):
        rename = libc.renameat2
        rename.argtypes = [ctypes.c_int, ctypes.c_char_p, ctypes.c_int, ctypes.c_char_p, ctypes.c_uint]
        rename.restype = ctypes.c_int
        result = rename(-100, source_bytes, -100, destination_bytes, 1)
    elif system == "Darwin" and hasattr(libc, "renamex_np"):
        rename = libc.renamex_np
        rename.argtypes = [ctypes.c_char_p, ctypes.c_char_p, ctypes.c_uint]
        rename.restype = ctypes.c_int
        result = rename(source_bytes, destination_bytes, 0x00000004)
    else:
        raise ContractError("publish-no-replace-unsupported")
    if result != 0:
        error_number = ctypes.get_errno()
        if error_number in (errno.EEXIST, errno.ENOTEMPTY):
            raise ContractError("publish-destination-exists")
        raise ContractError("publish-rename")


def cmd_publish(args: argparse.Namespace) -> None:
    source = args.input
    destination = args.output
    if destination.exists() or destination.is_symlink() or not destination.name:
        raise ContractError("publish-destination")
    parent = destination.parent
    parent_info = parent.lstat()
    if parent.is_symlink() or not stat.S_ISDIR(parent_info.st_mode):
        raise ContractError("publish-parent")
    validate_package(source, args.project)
    fsync_package(source)
    rename_no_replace(source, destination)


def derive_manifest_schema_version(package: Path) -> str:
    manifest = load_json_reject_duplicate_keys(package / "avatar-manifest.json")
    if not isinstance(manifest, dict):
        raise ContractError("manifest-schema")
    format_name = manifest.get("format")
    if format_name not in MANIFEST_FORMAT_ALLOWLIST:
        raise ContractError("manifest-format")
    return str(format_name)


def cmd_metadata(args: argparse.Namespace) -> None:
    package = args.output.parent
    database_counts = validate_database_counts(load_one_json(args.db_counts_file))
    payloads = {
        filename: {"filename": filename, "sha256": sha256_file(package / filename)}
        for filename in PAYLOADS
    }
    metadata = {
        "schema_version": 1,
        "created_at": datetime.now(timezone.utc).isoformat(timespec="seconds").replace("+00:00", "Z"),
        "payloads": payloads,
        "manifest_schema_version": derive_manifest_schema_version(package),
        "source_compose_project": args.project,
        "compose_file_sha256": args.compose_sha256,
        "images": {
            "app": {"id": args.app_image_id},
            "postgres": {"id": args.pg_image_id},
        },
        "postgres_server_major": args.pg_major,
        "app_was_running": args.app_was_running == "true",
        "database_counts": database_counts,
    }
    if args.app_repo_digest:
        metadata["images"]["app"]["repo_digest"] = args.app_repo_digest
    if args.pg_repo_digest:
        metadata["images"]["postgres"]["repo_digest"] = args.pg_repo_digest
    with args.output.open("x", encoding="utf-8") as stream:
        json.dump(metadata, stream, ensure_ascii=False, sort_keys=True, indent=2)
        stream.write("\n")


def cmd_checksums(args: argparse.Namespace) -> None:
    package = args.output.parent
    with args.output.open("x", encoding="utf-8") as stream:
        for filename in sorted(PACKAGE_FILES - {"SHA256SUMS"}):
            stream.write(f"{sha256_file(package / filename)}  {filename}\n")


def parser() -> argparse.ArgumentParser:
    root = argparse.ArgumentParser()
    commands = root.add_subparsers(dest="command", required=True)
    render = commands.add_parser("render-info")
    render.set_defaults(run=cmd_render_info)
    env_get = commands.add_parser("env-get")
    env_get.add_argument("--env-file", type=Path, required=True)
    env_get.add_argument("--key", required=True)
    env_get.set_defaults(run=cmd_env_get)
    env_runtime = commands.add_parser("env-runtime")
    env_runtime.add_argument("--env-file", type=Path, required=True)
    env_runtime.add_argument("--output", type=Path, required=True)
    env_runtime.set_defaults(run=cmd_env_runtime)
    invariant = commands.add_parser("container-invariant")
    invariant.add_argument("--container-id", required=True)
    invariant.add_argument("--name", required=True)
    invariant.add_argument("--label", action="append", default=[])
    invariant.set_defaults(run=cmd_container_invariant)
    app_state = commands.add_parser("app-state")
    app_state.set_defaults(run=cmd_app_state)
    runtime = commands.add_parser("runtime-check")
    runtime.add_argument("--avatar-volume", required=True)
    runtime.add_argument("--pg-volume", required=True)
    runtime.set_defaults(run=cmd_runtime_check)
    snapshot = commands.add_parser("snapshot")
    snapshot.add_argument("--input", type=Path, required=True)
    snapshot.add_argument("--output", type=Path, required=True)
    snapshot.add_argument("--project", required=True)
    snapshot.set_defaults(run=cmd_snapshot)
    validate = commands.add_parser("validate")
    validate.add_argument("--input", type=Path, required=True)
    validate.add_argument("--project", required=True)
    validate.set_defaults(run=cmd_validate)
    db_counts_sql = commands.add_parser("db-counts-sql")
    db_counts_sql.set_defaults(run=cmd_db_counts_sql)
    verify_db_counts = commands.add_parser("verify-db-counts")
    verify_db_counts.add_argument("--metadata", type=Path, required=True)
    verify_db_counts.add_argument("--actual", type=Path, required=True)
    verify_db_counts.set_defaults(run=cmd_verify_db_counts)
    publish = commands.add_parser("publish")
    publish.add_argument("--input", type=Path, required=True)
    publish.add_argument("--output", type=Path, required=True)
    publish.add_argument("--project", required=True)
    publish.set_defaults(run=cmd_publish)
    metadata = commands.add_parser("metadata")
    metadata.add_argument("--output", type=Path, required=True)
    metadata.add_argument("--project", required=True)
    metadata.add_argument("--compose-sha256", required=True)
    metadata.add_argument("--app-image-id", required=True)
    metadata.add_argument("--pg-image-id", required=True)
    metadata.add_argument("--app-repo-digest", default="")
    metadata.add_argument("--pg-repo-digest", default="")
    metadata.add_argument("--pg-major", required=True)
    metadata.add_argument("--app-was-running", choices=("true", "false"), required=True)
    metadata.add_argument("--db-counts-file", type=Path, required=True)
    metadata.set_defaults(run=cmd_metadata)
    checksums = commands.add_parser("checksums")
    checksums.add_argument("--output", type=Path, required=True)
    checksums.set_defaults(run=cmd_checksums)
    return root


def main() -> int:
    args = parser().parse_args()
    try:
        args.run(args)
    except (ContractError, OSError, UnicodeError, ValueError, json.JSONDecodeError, tarfile.TarError):
        fail("contract")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
