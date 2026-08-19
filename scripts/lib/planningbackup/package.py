"""Strict readers and writers for deployment backup packages.

The v1 package is delegated to ``v1-ops-package.py`` after schema dispatch so
its five-file behavior remains characterized by the existing self-test.  The
v2 decoder is intentionally a separate exact-field implementation.
"""

from __future__ import annotations

import copy
import hashlib
import importlib.util
import json
import re
import stat
import errno
import ctypes
import os
import platform
import shutil
import tarfile
from datetime import datetime
from pathlib import Path, PurePosixPath
from typing import Any


class ContractError(Exception):
    """A stable, operator-facing package contract failure."""


V1_FILES = frozenset({
    "database.sql",
    "avatar-volume.tgz",
    "avatar-manifest.json",
    "metadata.json",
    "SHA256SUMS",
})
V2_FILES = frozenset({
    "database.sql",
    "avatar-volume.tgz",
    "avatar-manifest.json",
    "planning-media-volume.tgz",
    "planning-media-manifest.json",
    "metadata.json",
    "SHA256SUMS",
})
V2_PAYLOADS = (
    "database.sql",
    "avatar-volume.tgz",
    "avatar-manifest.json",
    "planning-media-volume.tgz",
    "planning-media-manifest.json",
)
V2_METADATA_FIELDS = frozenset({
    "schema_version",
    "created_at",
    "source_deployment_identity",
    "compose_file_sha256",
    "images",
    "postgres_server_major",
    "app_was_running",
    "database_counts",
    "payloads",
    "manifest_schemas",
    "package_writer_version",
})
V2_IDENTITY_FIELDS = frozenset({
    "environment_id",
    "deployment_id",
    "compose_project",
    "storage_layout_version",
})
V2_PAYLOAD_FIELDS = frozenset({"filename", "sha256", "size_bytes"})
V2_MANIFEST_SCHEMAS = frozenset({"avatar", "planning_media"})
HEX64 = re.compile(r"^[0-9a-f]{64}$")
IDENTIFIER = re.compile(r"^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$")
UTC = re.compile(r"^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}(?:\.[0-9]+)?Z$")
PLANNING_KEY = re.compile(
    r"^planning/([A-Za-z0-9_-]+)/assets/([A-Za-z0-9_-]+)/g([1-9][0-9]*)/(original|display)$"
)
MEDIA_TYPES = frozenset({"image/jpeg", "image/png", "image/webp"})


def _legacy_module() -> Any:
    path = Path(__file__).resolve().parents[1] / "v1-ops-package.py"
    spec = importlib.util.spec_from_file_location("planningbackup_v1_ops_package", path)
    if spec is None or spec.loader is None:
        raise ContractError("v1-reader-unavailable")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def canonical_json(value: Any) -> bytes:
    return (json.dumps(value, ensure_ascii=False, sort_keys=True, separators=(",", ":")) + "\n").encode("utf-8")


def _reject_constant(value: str) -> Any:
    raise ContractError(f"json-constant-{value}")


def load_json(path: Path) -> Any:
    require_regular(path)

    def reject_duplicates(pairs: list[tuple[str, Any]]) -> dict[str, Any]:
        output: dict[str, Any] = {}
        for key, value in pairs:
            if key in output:
                raise ContractError("json-duplicate-key")
            output[key] = value
        return output

    try:
        return json.loads(
            path.read_text(encoding="utf-8"),
            object_pairs_hook=reject_duplicates,
            parse_constant=_reject_constant,
        )
    except (UnicodeError, json.JSONDecodeError) as error:
        raise ContractError("json-invalid") from error


def write_json(path: Path, value: Any) -> None:
    path.write_bytes(canonical_json(value))


def require_regular(path: Path) -> None:
    try:
        info = path.lstat()
    except OSError as error:
        raise ContractError("artifact-missing") from error
    if path.is_symlink() or not stat.S_ISREG(info.st_mode):
        raise ContractError("artifact-not-regular")


def sha256_file(path: Path) -> str:
    require_regular(path)
    digest = hashlib.sha256()
    with path.open("rb") as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def require_nonnegative_int(value: Any, key: str) -> int:
    if isinstance(value, bool) or not isinstance(value, int) or value < 0:
        raise ContractError(key)
    return value


def require_string(value: Any, key: str, pattern: re.Pattern[str] | None = None) -> str:
    if not isinstance(value, str) or not value:
        raise ContractError(key)
    if pattern is not None and not pattern.fullmatch(value):
        raise ContractError(key)
    return value


def require_utc(value: Any, key: str) -> str:
    return require_string(value, key, UTC)


def _safe_member_name(raw: str) -> str:
    name = raw
    while name.startswith("./"):
        name = name[2:]
    if name in {"", "."}:
        return ""
    candidate = PurePosixPath(name)
    if candidate.is_absolute() or any(part in {"", ".", ".."} for part in candidate.parts):
        raise ContractError("tar-path")
    return "/".join(candidate.parts)


def _read_tar_member(stream: Any, size: int, key: str) -> bytes:
    if size < 0 or size > 128 * 1024 * 1024:
        raise ContractError(key)
    body = stream.read(size + 1)
    if len(body) != size:
        raise ContractError("tar-size")
    return body


def _planning_manifest_digest(manifest: dict[str, Any]) -> str:
    payload = copy.deepcopy(manifest)
    payload.pop("digest", None)
    # PlanningMediaManifestV1 is produced by Go json.Marshal.  The current
    # field insertion order is stable and is kept here for byte-compatible
    # digest verification of that owner-owned manifest.
    encoded = json.dumps(payload, ensure_ascii=False, separators=(",", ":"), allow_nan=False).encode("utf-8")
    return "sha256-" + hashlib.sha256(encoded).hexdigest()


def _validate_metadata(value: Any, key: str) -> dict[str, Any]:
    if not isinstance(value, dict):
        raise ContractError(key)
    required = {"MediaType", "Size", "Checksum", "ModifiedAt"}
    if set(value) - required - {"Width", "Height"} or not required.issubset(value):
        raise ContractError(key)
    if value["MediaType"] not in MEDIA_TYPES:
        raise ContractError(f"{key}-media-type")
    size = require_nonnegative_int(value["Size"], f"{key}-size")
    if size <= 0:
        raise ContractError(f"{key}-size")
    if not isinstance(value["Checksum"], str) or not value["Checksum"].startswith("sha256-") or not HEX64.fullmatch(value["Checksum"][7:]):
        raise ContractError(f"{key}-checksum")
    require_utc(value["ModifiedAt"], f"{key}-modified-at")
    for optional in ("Width", "Height"):
        if optional in value:
            require_nonnegative_int(value[optional], f"{key}-{optional.lower()}")
    return value


def validate_planning_manifest(path: Path) -> dict[str, Any]:
    manifest = load_json(path)
    if not isinstance(manifest, dict) or set(manifest) != {"version", "entries", "digest"}:
        raise ContractError("planning-manifest-schema")
    if manifest["version"] != 1 or not isinstance(manifest["entries"], list):
        raise ContractError("planning-manifest-version")
    previous = ""
    for entry in manifest["entries"]:
        if not isinstance(entry, dict) or set(entry) != {"key", "asset_id", "generation", "rendition", "metadata"}:
            raise ContractError("planning-manifest-entry-schema")
        key = require_string(entry["key"], "planning-manifest-key", PLANNING_KEY)
        if previous and key <= previous:
            raise ContractError("planning-manifest-order")
        previous = key
        asset_id = require_string(entry["asset_id"], "planning-manifest-asset-id", IDENTIFIER)
        generation = entry["generation"]
        if isinstance(generation, bool) or not isinstance(generation, int) or generation < 1:
            raise ContractError("planning-manifest-generation")
        if entry["rendition"] not in {"original", "display"}:
            raise ContractError("planning-manifest-rendition")
        key_match = PLANNING_KEY.fullmatch(key)
        if key_match is None:
            raise ContractError("planning-manifest-key")
        if key_match.group(2) != asset_id:
            raise ContractError("planning-manifest-key-asset-id")
        if int(key_match.group(3)) != generation:
            raise ContractError("planning-manifest-key-generation")
        if key_match.group(4) != entry["rendition"]:
            raise ContractError("planning-manifest-key-rendition")
        _validate_metadata(entry["metadata"], "planning-manifest-metadata")
    digest = require_string(manifest["digest"], "planning-manifest-digest")
    if digest != _planning_manifest_digest(manifest):
        raise ContractError("planning-manifest-digest")
    return manifest


def _validate_planning_tar(path: Path, manifest: dict[str, Any]) -> None:
    expected: dict[str, tuple[str, dict[str, Any]]] = {}
    for entry in manifest["entries"]:
        expected[f"{entry['key']}/content"] = ("content", entry["metadata"])
        expected[f"{entry['key']}/metadata.json"] = ("metadata", entry["metadata"])
    observed: set[str] = set()
    try:
        archive = tarfile.open(path, "r:gz")
    except (OSError, tarfile.TarError) as error:
        raise ContractError("planning-tar-open") from error
    with archive:
        for member in archive:
            name = _safe_member_name(member.name)
            if name == "":
                if not member.isdir():
                    raise ContractError("tar-path")
                continue
            if member.isdir():
                continue
            if member.issym() or member.islnk() or not member.isreg():
                raise ContractError("tar-type")
            if name in observed:
                raise ContractError("tar-duplicate")
            observed.add(name)
            if name not in expected:
                raise ContractError("planning-tar-orphan")
            kind, metadata = expected[name]
            stream = archive.extractfile(member)
            if stream is None:
                raise ContractError("tar-read")
            body = _read_tar_member(stream, member.size, "tar-size")
            if kind == "content":
                if len(body) != metadata["Size"] or hashlib.sha256(body).hexdigest() != metadata["Checksum"][7:]:
                    raise ContractError("planning-tar-content-mismatch")
            else:
                try:
                    actual = json.loads(body.decode("utf-8"), object_pairs_hook=lambda pairs: _pairs_without_duplicates(pairs), parse_constant=_reject_constant)
                except (UnicodeError, json.JSONDecodeError) as error:
                    raise ContractError("planning-tar-metadata-json") from error
                _validate_metadata(actual, "planning-tar-metadata")
                if canonical_json(actual) != canonical_json(metadata):
                    raise ContractError("planning-tar-metadata-mismatch")
    if observed != set(expected):
        raise ContractError("planning-tar-missing")


def _pairs_without_duplicates(pairs: list[tuple[str, Any]]) -> dict[str, Any]:
    result: dict[str, Any] = {}
    for key, value in pairs:
        if key in result:
            raise ContractError("json-duplicate-key")
        result[key] = value
    return result


def _validate_checksums(package: Path, expected_files: frozenset[str]) -> None:
    require_regular(package / "SHA256SUMS")
    observed: dict[str, str] = {}
    for line in (package / "SHA256SUMS").read_text(encoding="utf-8").splitlines():
        match = re.fullmatch(r"([0-9a-f]{64})  ([A-Za-z0-9._-]+)", line)
        if match is None or match.group(2) == "SHA256SUMS" or match.group(2) in observed:
            raise ContractError("checksums-format")
        observed[match.group(2)] = match.group(1)
    expected = expected_files - {"SHA256SUMS"}
    if set(observed) != expected:
        raise ContractError("checksums-membership")
    for filename, digest in observed.items():
        if sha256_file(package / filename) != digest:
            raise ContractError("checksums-mismatch")


def _validate_v2_metadata(package: Path) -> dict[str, Any]:
    metadata = load_json(package / "metadata.json")
    if not isinstance(metadata, dict) or set(metadata) != V2_METADATA_FIELDS or metadata["schema_version"] != 2:
        raise ContractError("v2-metadata-schema")
    require_utc(metadata["created_at"], "v2-created-at")
    identity = metadata["source_deployment_identity"]
    if not isinstance(identity, dict) or set(identity) != V2_IDENTITY_FIELDS:
        raise ContractError("v2-identity-schema")
    for field in V2_IDENTITY_FIELDS:
        require_string(identity[field], f"v2-identity-{field}", IDENTIFIER)
    if not HEX64.fullmatch(metadata["compose_file_sha256"]):
        raise ContractError("v2-compose-sha")
    images = metadata["images"]
    if not isinstance(images, dict) or set(images) != {"app", "postgres", "restore_tool"}:
        raise ContractError("v2-images-schema")
    for name, value in images.items():
        require_string(value, f"v2-image-{name}")
    require_string(metadata["postgres_server_major"], "v2-postgres-major", re.compile(r"^[0-9]+$"))
    if not isinstance(metadata["app_was_running"], bool):
        raise ContractError("v2-app-state")
    counts = metadata["database_counts"]
    if not isinstance(counts, dict) or not counts:
        raise ContractError("v2-database-counts")
    for key, value in counts.items():
        require_string(key, "v2-database-count-key", re.compile(r"^[a-z][a-z0-9_]{0,127}$"))
        require_nonnegative_int(value, "v2-database-count-value")
    payloads = metadata["payloads"]
    if not isinstance(payloads, dict) or set(payloads) != set(V2_PAYLOADS):
        raise ContractError("v2-payloads-schema")
    for filename in V2_PAYLOADS:
        record = payloads[filename]
        if not isinstance(record, dict) or set(record) != {"filename", "sha256", "size_bytes"}:
            raise ContractError("v2-payload-record")
        if record["filename"] != filename or not HEX64.fullmatch(str(record["sha256"])):
            raise ContractError("v2-payload-value")
        require_nonnegative_int(record["size_bytes"], "v2-payload-size")
        if record["sha256"] != sha256_file(package / filename) or record["size_bytes"] != (package / filename).stat().st_size:
            raise ContractError("v2-payload-integrity")
    manifest_schemas = metadata["manifest_schemas"]
    if not isinstance(manifest_schemas, dict) or set(manifest_schemas) != V2_MANIFEST_SCHEMAS:
        raise ContractError("v2-manifest-schemas")
    for value in manifest_schemas.values():
        require_string(value, "v2-manifest-schema")
    if metadata["manifest_schemas"]["planning_media"] != "planning-media-manifest-v1":
        raise ContractError("v2-planning-manifest-schema")
    require_string(metadata["package_writer_version"], "v2-writer-version")
    return metadata


def validate_v2(package: Path) -> dict[str, Any]:
    if package.is_symlink() or not package.is_dir():
        raise ContractError("package-directory")
    if {entry.name for entry in package.iterdir()} != V2_FILES:
        raise ContractError("v2-package-membership")
    for filename in V2_FILES:
        require_regular(package / filename)
    _validate_checksums(package, V2_FILES)
    metadata = _validate_v2_metadata(package)
    legacy = _legacy_module()
    legacy.validate_database_dump(package / "database.sql")
    avatar_format = metadata["manifest_schemas"]["avatar"]
    legacy.validate_manifest(package, avatar_format)
    planning_manifest = validate_planning_manifest(package / "planning-media-manifest.json")
    _validate_planning_tar(package / "planning-media-volume.tgz", planning_manifest)
    return metadata


def detect_schema(package: Path) -> int:
    metadata = load_json(package / "metadata.json")
    if not isinstance(metadata, dict) or isinstance(metadata.get("schema_version"), bool) or not isinstance(metadata.get("schema_version"), int):
        raise ContractError("schema-version")
    if metadata["schema_version"] not in {1, 2}:
        raise ContractError("schema-version-unsupported")
    return metadata["schema_version"]


def validate_v1(package: Path, project: str) -> dict[str, Any]:
    legacy = _legacy_module()
    try:
        legacy.validate_package(package, project)
    except legacy.ContractError as error:
        raise ContractError(str(error)) from error
    return legacy.load_one_json(package / "metadata.json")


def validate_package(package: Path, project: str | None = None) -> dict[str, Any]:
    schema = detect_schema(package)
    if schema == 1:
        if project is None:
            raise ContractError("v1-project-required")
        return validate_v1(package, project)
    return validate_v2(package)


def write_checksums(package: Path) -> None:
    files = V2_FILES - {"SHA256SUMS"}
    (package / "SHA256SUMS").write_text(
        "".join(f"{sha256_file(package / filename)}  {filename}\n" for filename in sorted(files)),
        encoding="utf-8",
    )


def assemble_v2(
    destination: Path,
    source_files: dict[str, Path],
    metadata: dict[str, Any],
) -> None:
    if destination.exists() or destination.is_symlink():
        raise ContractError("v2-destination-exists")
    if set(source_files) != set(V2_PAYLOADS):
        raise ContractError("v2-source-membership")
    destination.mkdir(mode=0o700)
    try:
        for filename, source in source_files.items():
            require_regular(source)
            target = destination / filename
            target.write_bytes(source.read_bytes())
        write_json(destination / "metadata.json", metadata)
        write_checksums(destination)
        validate_v2(destination)
    except Exception:
        for child in destination.iterdir():
            if child.is_file() or child.is_symlink():
                child.unlink()
        destination.rmdir()
        raise


def snapshot_v2(source: Path, destination: Path) -> None:
    """Copy a mutable package once, then validate only the private snapshot."""
    if destination.exists() or destination.is_symlink():
        raise ContractError("v2-snapshot-destination")
    if source.is_symlink() or not source.is_dir():
        raise ContractError("v2-snapshot-source")
    before: dict[str, tuple[int, int, int, int]] = {}
    source_fd = -1
    destination_fd = -1
    old_umask = os.umask(0o077)
    try:
        source_fd = os.open(source, os.O_RDONLY | getattr(os, "O_DIRECTORY", 0) | getattr(os, "O_NOFOLLOW", 0) | getattr(os, "O_CLOEXEC", 0))
        source_directory = os.fstat(source_fd)
        if set(os.listdir(source_fd)) != V2_FILES:
            raise ContractError("v2-package-membership")
        destination.mkdir(mode=0o700)
        destination_fd = os.open(destination, os.O_RDONLY | getattr(os, "O_DIRECTORY", 0) | getattr(os, "O_CLOEXEC", 0))
        for filename in sorted(V2_FILES):
            reader_fd = -1
            writer_fd = -1
            try:
                reader_fd = os.open(filename, os.O_RDONLY | getattr(os, "O_NOFOLLOW", 0) | getattr(os, "O_CLOEXEC", 0), dir_fd=source_fd)
                info = os.fstat(reader_fd)
                if not stat.S_ISREG(info.st_mode):
                    raise ContractError("regular-file-required")
                before[filename] = (info.st_dev, info.st_ino, info.st_size, info.st_mtime_ns)
                writer_fd = os.open(filename, os.O_WRONLY | os.O_CREAT | os.O_EXCL | getattr(os, "O_CLOEXEC", 0), 0o600, dir_fd=destination_fd)
                with os.fdopen(reader_fd, "rb") as reader, os.fdopen(writer_fd, "wb") as writer:
                    reader_fd = -1
                    writer_fd = -1
                    shutil.copyfileobj(reader, writer, 1024 * 1024)
                    writer.flush()
                    os.fsync(writer.fileno())
                    after = os.fstat(reader.fileno())
                    if (after.st_dev, after.st_ino, after.st_size, after.st_mtime_ns) != before[filename]:
                        raise ContractError("v2-snapshot-input-changed")
            finally:
                if writer_fd >= 0:
                    os.close(writer_fd)
                if reader_fd >= 0:
                    os.close(reader_fd)
        os.fsync(destination_fd)
        if set(os.listdir(source_fd)) != V2_FILES:
            raise ContractError("v2-snapshot-input-changed")
        for filename, signature in before.items():
            check_fd = os.open(filename, os.O_RDONLY | getattr(os, "O_NOFOLLOW", 0) | getattr(os, "O_CLOEXEC", 0), dir_fd=source_fd)
            try:
                info = os.fstat(check_fd)
                if (info.st_dev, info.st_ino, info.st_size, info.st_mtime_ns) != signature:
                    raise ContractError("v2-snapshot-input-changed")
            finally:
                os.close(check_fd)
        current_source = os.stat(source, follow_symlinks=False)
        if not stat.S_ISDIR(current_source.st_mode) or (current_source.st_dev, current_source.st_ino) != (source_directory.st_dev, source_directory.st_ino):
            raise ContractError("v2-snapshot-input-changed")
        validate_v2(destination)
    except Exception as error:
        shutil.rmtree(destination, ignore_errors=True)
        if isinstance(error, ContractError):
            raise
        raise ContractError("v2-snapshot-copy") from error
    finally:
        if destination_fd >= 0:
            os.close(destination_fd)
        if source_fd >= 0:
            os.close(source_fd)
        os.umask(old_umask)


def _fsync_regular_file(path: Path) -> None:
    descriptor = os.open(path, os.O_RDONLY | getattr(os, "O_NOFOLLOW", 0) | getattr(os, "O_CLOEXEC", 0))
    try:
        if not stat.S_ISREG(os.fstat(descriptor).st_mode):
            raise ContractError("v2-publish-source-file")
        os.fsync(descriptor)
    finally:
        os.close(descriptor)


def _fsync_directory(path: Path) -> None:
    descriptor = os.open(
        path,
        os.O_RDONLY
        | getattr(os, "O_DIRECTORY", 0)
        | getattr(os, "O_NOFOLLOW", 0)
        | getattr(os, "O_CLOEXEC", 0),
    )
    try:
        if not stat.S_ISDIR(os.fstat(descriptor).st_mode):
            raise ContractError("v2-publish-directory")
        os.fsync(descriptor)
    finally:
        os.close(descriptor)


def _rename_no_replace(source: Path, destination: Path) -> None:
    libc = ctypes.CDLL(None, use_errno=True)
    source_bytes = str(source).encode()
    destination_bytes = str(destination).encode()
    if platform.system() == "Linux" and hasattr(libc, "renameat2"):
        rename = libc.renameat2
        rename.argtypes = [ctypes.c_int, ctypes.c_char_p, ctypes.c_int, ctypes.c_char_p, ctypes.c_uint]
        rename.restype = ctypes.c_int
        result = rename(-100, source_bytes, -100, destination_bytes, 1)
    elif platform.system() == "Darwin" and hasattr(libc, "renamex_np"):
        rename = libc.renamex_np
        rename.argtypes = [ctypes.c_char_p, ctypes.c_char_p, ctypes.c_uint]
        rename.restype = ctypes.c_int
        result = rename(source_bytes, destination_bytes, 0x00000004)
    else:
        raise ContractError("v2-publish-no-replace-unsupported")
    if result != 0:
        error_number = ctypes.get_errno()
        if error_number in (errno.EEXIST, errno.ENOTEMPTY):
            raise ContractError("v2-publish-destination-exists")
        raise ContractError("v2-publish-rename")


def publish_v2(source: Path, destination: Path) -> None:
    if destination.exists() or destination.is_symlink() or not destination.name:
        raise ContractError("v2-publish-destination")
    if source.is_symlink() or not source.is_dir():
        raise ContractError("v2-publish-source")
    parent = destination.parent
    if parent.is_symlink() or not parent.is_dir():
        raise ContractError("v2-publish-parent")
    validate_v2(source)
    for filename in V2_FILES:
        _fsync_regular_file(source / filename)
    _fsync_directory(source)
    _rename_no_replace(source, destination)
    # The rename is not durable until its destination directory entry is.
    _fsync_directory(parent)
