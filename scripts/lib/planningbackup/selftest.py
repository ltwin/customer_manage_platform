#!/usr/bin/env python3
"""Daemon-free schema-v2 and adversarial package self-test."""

from __future__ import annotations

import importlib.util
import io
import json
import shutil
import tarfile
import tempfile
from types import SimpleNamespace
from pathlib import Path
from unittest import mock

try:
    from .package import (
        ContractError,
        V2_FILES,
        V2_PAYLOADS,
        _planning_manifest_digest,
        _legacy_module,
        load_json,
        validate_planning_manifest,
        publish_v2,
        snapshot_v2,
        validate_package,
        validate_v2,
        write_checksums,
        write_json,
    )
except ImportError:  # direct script invocation from the ops safety shell
    from package import (  # type: ignore[import-not-found]
        ContractError,
        V2_FILES,
        V2_PAYLOADS,
        _planning_manifest_digest,
        _legacy_module,
        load_json,
        validate_planning_manifest,
        publish_v2,
        snapshot_v2,
        validate_package,
        validate_v2,
        write_checksums,
        write_json,
    )


def _legacy_selftest():
    path = Path(__file__).resolve().parents[1] / "v1-ops-package-selftest.py"
    spec = importlib.util.spec_from_file_location("planningbackup_v1_selftest", path)
    if spec is None or spec.loader is None:
        raise RuntimeError("legacy selftest unavailable")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def _planning_manifest() -> dict:
    metadata = {
        "MediaType": "image/png",
        "Size": 14,
        "Checksum": "sha256-" + ("a" * 64),
        "ModifiedAt": "2026-08-18T00:00:00Z",
        "Width": 2,
        "Height": 2,
    }
    manifest = {
        "version": 1,
        "entries": [{
            "key": "planning/account-1/assets/asset-1/g1/display",
            "asset_id": "asset-1",
            "generation": 1,
            "rendition": "display",
            "metadata": metadata,
        }],
    }
    manifest["digest"] = _planning_manifest_digest(manifest)
    return manifest


def _write_planning_payload(root: Path, *, orphan: bool = False, metadata_size_float: bool = False) -> None:
    manifest = _planning_manifest()
    (root / "planning-media-manifest.json").write_text(json.dumps(manifest, separators=(",", ":")) + "\n", encoding="utf-8")
    body = b"planning-media"  # sha256 is intentionally replaced below.
    digest = __import__("hashlib").sha256(body).hexdigest()
    manifest["entries"][0]["metadata"]["Size"] = len(body)
    manifest["entries"][0]["metadata"]["Checksum"] = "sha256-" + digest
    manifest["digest"] = _planning_manifest_digest(manifest)
    (root / "planning-media-manifest.json").write_text(json.dumps(manifest, separators=(",", ":")) + "\n", encoding="utf-8")
    key = manifest["entries"][0]["key"]
    with tarfile.open(root / "planning-media-volume.tgz", "w:gz") as archive:
        for directory in ("planning", "planning/account-1", "planning/account-1/assets", f"planning/account-1/assets/asset-1", f"planning/account-1/assets/asset-1/g1", key):
            info = tarfile.TarInfo(directory)
            info.type = tarfile.DIRTYPE
            archive.addfile(info)
        tar_metadata = dict(manifest["entries"][0]["metadata"])
        if metadata_size_float:
            tar_metadata["Size"] = float(tar_metadata["Size"])
        for name, content in ((f"{key}/content", body), (f"{key}/metadata.json", json.dumps(tar_metadata, separators=(",", ":")).encode())):
            info = tarfile.TarInfo(name)
            info.size = len(content)
            archive.addfile(info, io.BytesIO(content))
        if orphan:
            content = b"orphan"
            info = tarfile.TarInfo(f"planning/account-1/assets/asset-2/g1/display/content")
            info.size = len(content)
            archive.addfile(info, io.BytesIO(content))


def _write_v2(root: Path, legacy_root: Path) -> None:
    for filename in ("database.sql", "avatar-volume.tgz", "avatar-manifest.json"):
        shutil.copy2(legacy_root / filename, root / filename)
    _write_planning_payload(root)
    payloads = {
        filename: {
            "filename": filename,
            "sha256": __import__("hashlib").sha256((root / filename).read_bytes()).hexdigest(),
            "size_bytes": (root / filename).stat().st_size,
        }
        for filename in V2_PAYLOADS
    }
    metadata = {
        "schema_version": 2,
        "created_at": "2026-08-18T00:00:00Z",
        "source_deployment_identity": {
            "environment_id": "test",
            "deployment_id": "deployment-1",
            "compose_project": "synthetic-project",
            "storage_layout_version": "planning-media-v1",
        },
        "compose_file_sha256": "b" * 64,
        "images": {"app": "sha256:app", "postgres": "sha256:pg", "restore_tool": "sha256:tool"},
        "postgres_server_major": "17",
        "app_was_running": True,
        "database_counts": {"accounts": 1},
        "payloads": payloads,
        "manifest_schemas": {"avatar": "customer-avatar-exact-generation-v1", "planning_media": "planning-media-manifest-v1"},
        "package_writer_version": "planningbackup-v2-test",
    }
    write_json(root / "metadata.json", metadata)
    write_checksums(root)


def _refresh_planning_manifest_package(root: Path, manifest: dict) -> None:
    manifest["digest"] = _planning_manifest_digest(manifest)
    write_json(root / "planning-media-manifest.json", manifest)
    metadata = load_json(root / "metadata.json")
    payload = root / "planning-media-manifest.json"
    metadata["payloads"][payload.name]["sha256"] = __import__("hashlib").sha256(payload.read_bytes()).hexdigest()
    metadata["payloads"][payload.name]["size_bytes"] = payload.stat().st_size
    write_json(root / "metadata.json", metadata)
    write_checksums(root)


def _must_reject(name: str, root: Path) -> None:
    try:
        validate_v2(root)
    except ContractError:
        return
    raise AssertionError(f"negative package accepted: {name}")


def main() -> int:
    with tempfile.TemporaryDirectory() as creative_dir:
        creative = _planning_manifest()
        entry = creative["entries"][0]
        entry["key"] = "creative/account-1/assets/asset-1/" + entry["metadata"]["Checksum"] + "/display"
        creative["digest"] = _planning_manifest_digest(creative)
        path = Path(creative_dir) / "manifest.json"
        path.write_text(json.dumps(creative, ensure_ascii=False, separators=(",", ":")), encoding="utf-8")
        validate_planning_manifest(path)
        entry["key"] = entry["key"].replace("a" * 64, "b" * 64)
        creative["digest"] = _planning_manifest_digest(creative)
        path.write_text(json.dumps(creative, ensure_ascii=False, separators=(",", ":")), encoding="utf-8")
        try:
            validate_planning_manifest(path)
        except ContractError:
            pass
        else:
            raise AssertionError("creative key/checksum mismatch accepted")
    with tempfile.TemporaryDirectory(prefix="planningbackup-v2-test-") as raw:
        root = Path(raw)
        legacy = _legacy_selftest()
        legacy_ops = _legacy_module()
        v1 = root / "v1"
        legacy.write_package(v1)
        if validate_package(v1, "synthetic-project")["schema_version"] != 1:
            raise AssertionError("v1 dual reader dispatch failed")
        legacy_metadata = load_json(v1 / "metadata.json")
        changed_counts = dict(legacy_metadata["database_counts"])
        changed_key = next(iter(changed_counts))
        changed_counts[changed_key] += 1
        changed_counts_path = root / "post-migrate-changed-counts.json"
        write_json(changed_counts_path, changed_counts)
        try:
            legacy_ops.cmd_verify_db_counts(SimpleNamespace(metadata=v1 / "metadata.json", actual=changed_counts_path))
        except legacy_ops.ContractError:
            pass
        else:
            raise AssertionError("post-migrate protected row-count drift was accepted")
        v2 = root / "v2"
        v2.mkdir(mode=0o700)
        _write_v2(v2, v1)
        validate_v2(v2)
        validate_package(v2, "synthetic-project")
        snapshot = root / "snapshot"
        snapshot_v2(v2, snapshot)
        validate_v2(snapshot)

        publish_source = root / "publish-source"
        shutil.copytree(v2, publish_source)
        publish_destination = root / "published"
        events: list[str] = []
        module_name = publish_v2.__module__
        with (
            mock.patch(f"{module_name}._fsync_regular_file", side_effect=lambda _: events.append("file")),
            mock.patch(
                f"{module_name}._fsync_directory",
                side_effect=lambda path: events.append("source-dir" if path == publish_source else "parent-dir"),
            ),
            mock.patch(
                f"{module_name}._rename_no_replace",
                side_effect=lambda _source, _destination: events.append("rename"),
            ),
        ):
            publish_v2(publish_source, publish_destination)
        expected_publish_events = ["file"] * len(V2_FILES) + ["source-dir", "rename", "parent-dir"]
        if events != expected_publish_events:
            raise AssertionError(f"publish durability order is wrong: {events}")

        changing = root / "changing"
        shutil.copytree(v2, changing)
        rejected_snapshot = root / "rejected-snapshot"
        original_copyfileobj = shutil.copyfileobj
        changed = False

        def mutate_during_copy(source, destination, length=0):
            nonlocal changed
            original_copyfileobj(source, destination, length)
            if not changed:
                changed = True
                with (changing / "SHA256SUMS").open("ab") as stream:
                    stream.write(b"\n")

        shutil.copyfileobj = mutate_during_copy
        try:
            snapshot_v2(changing, rejected_snapshot)
        except ContractError:
            pass
        else:
            raise AssertionError("mutable v2 source accepted during snapshot")
        finally:
            shutil.copyfileobj = original_copyfileobj
        if rejected_snapshot.exists():
            raise AssertionError("failed v2 snapshot was not cleaned")

        old = legacy_ops
        try:
            old.validate_package(v2, "synthetic-project")
        except old.ContractError:
            pass
        else:
            raise AssertionError("legacy reader accepted v2 package")

        unknown = root / "unknown"
        shutil.copytree(v2, unknown)
        metadata = load_json(unknown / "metadata.json")
        metadata["unexpected"] = True
        write_json(unknown / "metadata.json", metadata)
        write_checksums(unknown)
        _must_reject("unknown-metadata-field", unknown)

        for field, value in (("asset_id", "different-asset"), ("generation", 2), ("rendition", "original")):
            mismatch = root / f"manifest-key-{field}-mismatch"
            shutil.copytree(v2, mismatch)
            manifest = load_json(mismatch / "planning-media-manifest.json")
            manifest["entries"][0][field] = value
            _refresh_planning_manifest_package(mismatch, manifest)
            _must_reject(f"planning-manifest-key-{field}-mismatch", mismatch)

        bad_digest = root / "bad-digest"
        shutil.copytree(v2, bad_digest)
        manifest = load_json(bad_digest / "planning-media-manifest.json")
        manifest["digest"] = "sha256-" + "0" * 64
        write_json(bad_digest / "planning-media-manifest.json", manifest)
        metadata = load_json(bad_digest / "metadata.json")
        metadata["payloads"]["planning-media-manifest.json"]["sha256"] = __import__("hashlib").sha256((bad_digest / "planning-media-manifest.json").read_bytes()).hexdigest()
        metadata["payloads"]["planning-media-manifest.json"]["size_bytes"] = (bad_digest / "planning-media-manifest.json").stat().st_size
        write_json(bad_digest / "metadata.json", metadata)
        write_checksums(bad_digest)
        _must_reject("planning-manifest-digest", bad_digest)

        orphan = root / "orphan"
        shutil.copytree(v2, orphan)
        _write_planning_payload(orphan, orphan=True)
        metadata = load_json(orphan / "metadata.json")
        for filename in ("planning-media-volume.tgz", "planning-media-manifest.json"):
            metadata["payloads"][filename]["sha256"] = __import__("hashlib").sha256((orphan / filename).read_bytes()).hexdigest()
            metadata["payloads"][filename]["size_bytes"] = (orphan / filename).stat().st_size
        write_json(orphan / "metadata.json", metadata)
        write_checksums(orphan)
        _must_reject("planning-tar-orphan", orphan)

        float_metadata = root / "float-metadata"
        shutil.copytree(v2, float_metadata)
        _write_planning_payload(float_metadata, metadata_size_float=True)
        metadata = load_json(float_metadata / "metadata.json")
        for filename in ("planning-media-volume.tgz", "planning-media-manifest.json"):
            metadata["payloads"][filename]["sha256"] = __import__("hashlib").sha256((float_metadata / filename).read_bytes()).hexdigest()
            metadata["payloads"][filename]["size_bytes"] = (float_metadata / filename).stat().st_size
        write_json(float_metadata / "metadata.json", metadata)
        write_checksums(float_metadata)
        _must_reject("planning-tar-metadata-number-type", float_metadata)
    print("planningbackup schema-v2 selftest: passed")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
