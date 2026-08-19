"""Read-only restore preflight checks.

No function in this module writes the target database or object volume.  The
caller must use the returned ``destructive_authorized`` bit as the only gate
before invoking a replace operation.
"""

from __future__ import annotations

import hashlib
import json
import os
import stat
import tarfile
from pathlib import Path
from typing import Any

from .package import ContractError, detect_schema, load_json, sha256_file, validate_package


def _package_sha256(package: Path) -> str:
    digest = hashlib.sha256()
    for path in sorted((entry for entry in package.iterdir() if entry.is_file()), key=lambda item: item.name):
        digest.update(path.name.encode("utf-8"))
        digest.update(b"\0")
        with path.open("rb") as stream:
            for chunk in iter(lambda: stream.read(1024 * 1024), b""):
                digest.update(chunk)
    return digest.hexdigest()


def _inventory(path: Path) -> dict[str, int]:
    if not path.exists():
        return {"regular_files": 0, "bytes": 0, "unsafe_entries": 0, "total_entries": 0}
    if path.is_symlink() or not path.is_dir():
        raise ContractError("target-not-directory")
    regular_files = 0
    total_bytes = 0
    unsafe_entries = 0
    total_entries = 0
    for root, directories, files in os.walk(path, followlinks=False):
        root_path = Path(root)
        for directory in directories:
            total_entries += 1
            if (root_path / directory).is_symlink():
                unsafe_entries += 1
        for filename in files:
            total_entries += 1
            file_path = root_path / filename
            info = file_path.lstat()
            if file_path.is_symlink() or not stat.S_ISREG(info.st_mode):
                unsafe_entries += 1
            else:
                regular_files += 1
                total_bytes += info.st_size
    return {"regular_files": regular_files, "bytes": total_bytes, "unsafe_entries": unsafe_entries, "total_entries": total_entries}


def _tar_uncompressed_bytes(path: Path) -> int:
    total = 0
    with tarfile.open(path, "r:gz") as archive:
        for member in archive:
            if member.isreg():
                total += member.size
    return total


def _required_bytes(package: Path, package_schema: int) -> int:
    compressed = sum((package / filename).stat().st_size for filename in package.iterdir() if filename.is_file())
    expanded = _tar_uncompressed_bytes(package / "avatar-volume.tgz")
    if package_schema == 2:
        expanded += _tar_uncompressed_bytes(package / "planning-media-volume.tgz")
    # Covers private staging, replacement copies and filesystem metadata.  It
    # is intentionally conservative and never accepts an unknown oracle.
    return compressed + expanded + 64 * 1024 * 1024


def _identity(path: Path) -> dict[str, str]:
    value = load_json(path)
    fields = {"environment_id", "deployment_id", "compose_project", "storage_layout_version"}
    if not isinstance(value, dict) or set(value) != fields:
        raise ContractError("identity-schema")
    output: dict[str, str] = {}
    for key, item in value.items():
        if not isinstance(item, str) or not item or "/" in item or "\n" in item or "\r" in item:
            raise ContractError("identity-value")
        output[key] = item
    return output


def _database_inventory(counts: Any, dump_size_bytes: int | None = None) -> dict[str, Any]:
    if not isinstance(counts, dict) or not counts:
        raise ContractError("database-inventory-schema")
    normalized: dict[str, int] = {}
    for table, count in counts.items():
        if not isinstance(table, str) or not table or isinstance(count, bool) or not isinstance(count, int) or count < 0:
            raise ContractError("database-inventory-value")
        normalized[table] = count
    result: dict[str, Any] = {
        "status": "validated",
        "table_count": len(normalized),
        "row_count": sum(normalized.values()),
    }
    if dump_size_bytes is not None:
        if isinstance(dump_size_bytes, bool) or not isinstance(dump_size_bytes, int) or dump_size_bytes <= 0:
            raise ContractError("database-inventory-dump-size")
        result["dump_size_bytes"] = dump_size_bytes
    return result


def _avatar_inventory(path: Path) -> dict[str, Any]:
    manifest = load_json(path)
    if not isinstance(manifest, dict) or set(manifest) != {"format", "generated_at", "current", "inventory"}:
        raise ContractError("avatar-inventory-schema")
    inventory = manifest["inventory"]
    current = manifest["current"]
    if not isinstance(inventory, dict) or set(inventory) != {"count", "actual_sha256", "objects"} or not isinstance(current, list):
        raise ContractError("avatar-inventory-schema")
    objects = inventory["objects"]
    if not isinstance(objects, list) or isinstance(inventory["count"], bool) or inventory["count"] != len(objects):
        raise ContractError("avatar-inventory-count")
    total_bytes = 0
    for item in objects:
        if not isinstance(item, dict) or set(item) != {"key", "size", "actual_sha256"}:
            raise ContractError("avatar-inventory-object")
        if isinstance(item["size"], bool) or not isinstance(item["size"], int) or item["size"] < 0:
            raise ContractError("avatar-inventory-size")
        total_bytes += item["size"]
    return {
        "status": "validated",
        "manifest_sha256": sha256_file(path),
        "object_count": len(objects),
        "current_subject_count": len(current),
        "bytes": total_bytes,
    }


def _planning_inventory(path: Path | None) -> dict[str, Any]:
    if path is None:
        return {
            "status": "not_in_schema_v1",
            "manifest_sha256": None,
            "object_count": 0,
            "bytes": 0,
        }
    manifest = load_json(path)
    if not isinstance(manifest, dict) or set(manifest) != {"version", "entries", "digest"} or manifest["version"] != 1 or not isinstance(manifest["entries"], list):
        raise ContractError("planning-inventory-schema")
    total_bytes = 0
    for entry in manifest["entries"]:
        if not isinstance(entry, dict) or not isinstance(entry.get("metadata"), dict):
            raise ContractError("planning-inventory-entry")
        size = entry["metadata"].get("Size")
        if isinstance(size, bool) or not isinstance(size, int) or size <= 0:
            raise ContractError("planning-inventory-size")
        total_bytes += size
    return {
        "status": "validated",
        "manifest_sha256": sha256_file(path),
        "object_count": len(manifest["entries"]),
        "bytes": total_bytes,
    }


def _check(status: str, reason_key: str | None = None) -> dict[str, str]:
    result = {"id": status, "status": "passed" if reason_key is None else "failed"}
    if reason_key is not None:
        result["reason_key"] = reason_key
    return result


def run_preflight(
    package: Path,
    identity_path: Path,
    target_planning_media: Path,
    available_bytes: int | None,
    restore_tool_digest: str | None = None,
    target_generation: str | None = None,
    target_inventory_override: dict[str, int] | None = None,
    target_compose_sha256: str | None = None,
    target_app_digest: str | None = None,
    target_postgres_digest: str | None = None,
    target_postgres_major: str | None = None,
    target_database_counts: dict[str, int] | None = None,
    target_avatar_manifest: Path | None = None,
) -> dict[str, Any]:
    checks: list[dict[str, str]] = []
    package_schema: int | None = None
    metadata: dict[str, Any] | None = None
    package_sha = ""
    identity_sha = ""
    required_bytes: int | None = None
    target_inventory: dict[str, int] = {"regular_files": 0, "bytes": 0, "unsafe_entries": 0, "total_entries": 0}
    source_inventory: dict[str, Any] = {
        "database": {"status": "unavailable"},
        "avatar": {"status": "unavailable"},
        "planning_media": {"status": "unavailable"},
    }
    target_runtime_inventory: dict[str, Any] = {
        "database": {"status": "unavailable"},
        "avatar": {"status": "unavailable"},
        "planning_media": {"status": "unavailable"},
    }
    try:
        identity = _identity(identity_path)
        identity_sha = sha256_file(identity_path)
        checks.append(_check("deployment_identity"))
    except ContractError as error:
        identity = {}
        checks.append(_check("deployment_identity", str(error)))

    try:
        package_schema = detect_schema(package)
        metadata = validate_package(package, identity.get("compose_project"))
        package_sha = _package_sha256(package)
        source_inventory = {
            "database": _database_inventory(metadata["database_counts"], (package / "database.sql").stat().st_size),
            "avatar": _avatar_inventory(package / "avatar-manifest.json"),
            "planning_media": _planning_inventory(package / "planning-media-manifest.json" if package_schema == 2 else None),
        }
        checks.extend([_check("schema_version"), _check("package_members_and_types"), _check("package_digests")])
        checks.extend([_check("manifests"), _check("tar_safety"), _check("database_dump")])
    except (ContractError, OSError, tarfile.TarError, json.JSONDecodeError) as error:
        checks.append(_check("package_validation", str(error)))

    try:
        target_runtime_inventory["database"] = _database_inventory(target_database_counts)
        checks.append(_check("target_database_inventory"))
    except ContractError as error:
        checks.append(_check("target_database_inventory", str(error)))

    try:
        if target_avatar_manifest is None:
            raise ContractError("target-avatar-inventory-unknown")
        target_runtime_inventory["avatar"] = _avatar_inventory(target_avatar_manifest)
        checks.append(_check("target_avatar_inventory"))
    except ContractError as error:
        checks.append(_check("target_avatar_inventory", str(error)))

    if metadata is not None and package_schema is not None:
        if package_schema == 2:
            source_identity = metadata.get("source_deployment_identity")
            checks.append(_check("deployment_identity_match" if source_identity == identity else "deployment_identity_match", None if source_identity == identity else "identity-mismatch"))
            images = metadata.get("images", {})
            checks.append(_check("restore_tool_compatibility", None if restore_tool_digest is not None and images.get("restore_tool") == restore_tool_digest else "restore-tool-digest-mismatch-or-unknown"))
            checks.append(_check("target_generation_fence", None if target_generation is not None and target_generation == identity.get("deployment_id") else "target-generation-mismatch-or-unknown"))
            checks.append(_check("compose_compatibility", None if target_compose_sha256 is not None and metadata.get("compose_file_sha256") == target_compose_sha256 else "compose-digest-mismatch-or-unknown"))
            checks.append(_check("app_image_compatibility", None if target_app_digest is not None and images.get("app") == target_app_digest else "app-image-mismatch-or-unknown"))
            checks.append(_check("postgres_image_compatibility", None if target_postgres_digest is not None and images.get("postgres") == target_postgres_digest else "postgres-image-mismatch-or-unknown"))
            checks.append(_check("postgres_major_compatibility", None if target_postgres_major is not None and metadata.get("postgres_server_major") == target_postgres_major else "postgres-major-mismatch-or-unknown"))
        else:
            # v1 packages predate a typed deployment identity.  They are only
            # admissible when the target layout explicitly declares that no
            # planning-media volume existed in that era.
            legacy_project = metadata.get("source_compose_project")
            compatible = identity.get("storage_layout_version") == "v1-no-planning-media" and legacy_project == identity.get("compose_project")
            checks.append(_check("deployment_identity_match", None if compatible else "v1-identity-layout-mismatch"))
            images = metadata.get("images", {})
            source_app = images.get("app", {}).get("id") if isinstance(images.get("app"), dict) else None
            source_postgres = images.get("postgres", {}).get("id") if isinstance(images.get("postgres"), dict) else None
            tool_compatible = (
                restore_tool_digest is not None
                and target_app_digest is not None
                and restore_tool_digest == target_app_digest == source_app
            )
            checks.append(_check("restore_tool_compatibility", None if tool_compatible else "restore-tool-digest-mismatch-or-unknown"))
            checks.append(_check("target_generation_fence", None if target_generation is not None and target_generation == identity.get("deployment_id") else "target-generation-mismatch-or-unknown"))
            checks.append(_check("compose_compatibility", None if target_compose_sha256 is not None and metadata.get("compose_file_sha256") == target_compose_sha256 else "compose-digest-mismatch-or-unknown"))
            checks.append(_check("app_image_compatibility", None if target_app_digest is not None and source_app == target_app_digest else "app-image-mismatch-or-unknown"))
            checks.append(_check("postgres_image_compatibility", None if target_postgres_digest is not None and source_postgres == target_postgres_digest else "postgres-image-mismatch-or-unknown"))
            checks.append(_check("postgres_major_compatibility", None if target_postgres_major is not None and metadata.get("postgres_server_major") == target_postgres_major else "postgres-major-mismatch-or-unknown"))

    try:
        target_inventory = target_inventory_override if target_inventory_override is not None else _inventory(target_planning_media)
        if set(target_inventory) != {"regular_files", "bytes", "unsafe_entries", "total_entries"} or any(isinstance(value, bool) or not isinstance(value, int) or value < 0 for value in target_inventory.values()):
            raise ContractError("target-inventory-schema")
        if target_inventory["regular_files"] > target_inventory["total_entries"] or target_inventory["unsafe_entries"] > target_inventory["total_entries"]:
            raise ContractError("target-inventory-counts")
        if package_schema == 1 and target_inventory["total_entries"] != 0:
            checks.append(_check("planning_target_inventory", "v1-planning-target-nonempty"))
        elif package_schema == 2 and target_inventory["unsafe_entries"] != 0:
            checks.append(_check("planning_target_inventory", "v2-planning-target-unsafe"))
        else:
            checks.append(_check("planning_target_inventory"))
        target_runtime_inventory["planning_media"] = {"status": "validated", **target_inventory}
    except ContractError as error:
        checks.append(_check("planning_target_inventory", str(error)))

    if package_schema is not None and metadata is not None:
        try:
            required_bytes = _required_bytes(package, package_schema)
            if available_bytes is None or isinstance(available_bytes, bool) or available_bytes < 0:
                checks.append(_check("available_space", "available-space-unknown"))
            else:
                checks.append(_check("available_space", None if available_bytes >= required_bytes else "available-space-insufficient"))
        except (OSError, tarfile.TarError, ContractError) as error:
            checks.append(_check("available_space", str(error)))
    else:
        checks.append(_check("available_space"))

    allowed = bool(checks) and all(item["status"] == "passed" for item in checks)
    return {
        "schema": "planning-restore-preflight-v1",
        "package_schema": package_schema,
        "package_sha256": package_sha,
        "deployment_identity_sha256": identity_sha,
        "checks": checks,
        "required_bytes": required_bytes,
        "available_bytes": available_bytes,
        "source_package_inventory": source_inventory,
        "target_runtime_inventory": target_runtime_inventory,
        "destructive_authorized": allowed,
    }
