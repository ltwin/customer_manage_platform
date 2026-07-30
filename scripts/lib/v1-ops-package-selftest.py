#!/usr/bin/env python3
"""Deterministic, daemon-free self-test for the V1 package helper."""

from __future__ import annotations

import importlib.util
import io
import json
import os
import tarfile
import tempfile
from argparse import Namespace
from pathlib import Path


HELPER = Path(__file__).with_name("v1-ops-package.py")
SPEC = importlib.util.spec_from_file_location("v1_ops_package", HELPER)
assert SPEC and SPEC.loader
ops = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(ops)


DB_COUNTS = {
    "accounts": 1,
    "avatar_object_gc": 0,
    "avatar_reconciliation_checkpoint": 0,
    "customer_notes": 2,
    "customers": 1,
    "idempotency_records": 0,
    "orders": 3,
    "packages": 1,
    "reminder_scan_state": 0,
    "reminders": 4,
    "schedule_slots": 2,
    "schema_migrations": 1,
    "settings": 1,
    "social_identities": 1,
    "telegram_bind_tokens": 0,
    "telegram_deliveries": 0,
}
AVATAR_PAYLOAD = b"synthetic-marker"
AVATAR_SHA = ops.hashlib.sha256(AVATAR_PAYLOAD).hexdigest()
AVATAR_VERSION = f"sha256-{AVATAR_SHA}"
AVATAR_OBJECT_ID = "a" * 32
AVATAR_KEY = f"avatars/account-1/customers/customer-1/{AVATAR_VERSION}/{AVATAR_OBJECT_ID}"
AVATAR_METADATA = {
    "MediaType": "image/jpeg",
    "Size": len(AVATAR_PAYLOAD),
    "Checksum": AVATAR_VERSION,
    "ModifiedAt": "2026-07-27T00:00:00Z",
}


def write_tar(path: Path, entries: dict[str, bytes] | None = None) -> None:
    if entries is None:
        entries = {
            f"{AVATAR_KEY}/content": AVATAR_PAYLOAD,
            f"{AVATAR_KEY}/metadata.json": json.dumps(AVATAR_METADATA).encode(),
        }
    with tarfile.open(path, "w:gz") as archive:
        for name, payload in entries.items():
            info = tarfile.TarInfo(name)
            info.size = len(payload)
            archive.addfile(info, io.BytesIO(payload))


def rewrite_checksums(path: Path) -> None:
    checksum = path / "SHA256SUMS"
    if checksum.exists():
        checksum.unlink()
    ops.cmd_checksums(Namespace(output=checksum))


def write_package(path: Path, project: str = "synthetic-project") -> None:
    path.mkdir(mode=0o700)
    (path / "database.sql").write_text(
        "CREATE TABLE synthetic (id integer);\nCOPY synthetic (id) FROM stdin;\n1\n\\.\n",
        encoding="utf-8",
    )
    write_tar(path / "avatar-volume.tgz")
    inventory_sha = ops.hashlib.sha256(
        f"{AVATAR_KEY}\0{len(AVATAR_PAYLOAD)}\0{AVATAR_SHA}\n".encode()
    ).hexdigest()
    (path / "avatar-manifest.json").write_text(json.dumps({
        "format": "customer-avatar-exact-generation-v1",
        "generated_at": "2026-07-27T00:00:00Z",
        "current": [{
            "account_id": "account-1",
            "customer_id": "customer-1",
            "avatar_version": AVATAR_VERSION,
            "avatar_object_id": AVATAR_OBJECT_ID,
            "key": AVATAR_KEY,
            "media_type": "image/jpeg",
            "size": len(AVATAR_PAYLOAD),
            "actual_sha256": AVATAR_SHA,
        }],
        "inventory": {
            "count": 1,
            "actual_sha256": inventory_sha,
            "objects": [{
                "key": AVATAR_KEY,
                "size": len(AVATAR_PAYLOAD),
                "actual_sha256": AVATAR_SHA,
            }],
        },
    }) + "\n", encoding="utf-8")
    counts = path / "database-counts.json"
    counts.write_text(json.dumps(DB_COUNTS) + "\n", encoding="utf-8")
    ops.cmd_metadata(Namespace(
        output=path / "metadata.json", project=project, compose_sha256="1" * 64,
        app_image_id="sha256:app", pg_image_id="sha256:postgres",
        app_repo_digest="", pg_repo_digest="", pg_major="17", app_was_running="true",
        db_counts_file=counts,
    ))
    counts.unlink()
    rewrite_checksums(path)


def must_reject(name: str, package: Path) -> None:
    try:
        ops.validate_package(package, "synthetic-project")
    except ops.ContractError:
        return
    raise AssertionError(f"negative package was accepted: {name}")


def main() -> int:
    with tempfile.TemporaryDirectory(prefix="v1-ops-package-test-") as raw:
        root = Path(raw)
        runtime_source = root / "runtime-source.env"
        runtime_source.write_text(
            "POSTGRES_USER=crm\n"
            "POSTGRES_PASSWORD=synthetic-password\n"
            "POSTGRES_DB=crm\n"
            "AUTH_TOKEN_SECRET=synthetic-auth-secret-0123456789\n",
            encoding="utf-8",
        )
        runtime_output = root / "runtime.env"
        ops.cmd_env_runtime(Namespace(env_file=runtime_source, output=runtime_output))
        runtime_values = ops.read_env(runtime_output)
        if runtime_values.get("AUTH_TOKEN_SECRET") != "synthetic-auth-secret-0123456789":
            raise AssertionError("runtime env omitted AUTH_TOKEN_SECRET")
        if runtime_output.stat().st_mode & 0o077:
            raise AssertionError("runtime env permissions are not private")

        missing_auth = root / "runtime-missing-auth.env"
        missing_auth.write_text(
            "POSTGRES_USER=crm\nPOSTGRES_PASSWORD=synthetic-password\nPOSTGRES_DB=crm\n",
            encoding="utf-8",
        )
        try:
            ops.cmd_env_runtime(Namespace(env_file=missing_auth, output=root / "missing-auth-output.env"))
        except ops.ContractError:
            pass
        else:
            raise AssertionError("runtime env accepted missing AUTH_TOKEN_SECRET")

        valid = root / "valid"
        write_package(valid)
        ops.validate_package(valid, "synthetic-project")
        staged = root / "staged"
        ops.cmd_snapshot(Namespace(input=valid, output=staged, project="synthetic-project"))
        ops.validate_package(staged, "synthetic-project")

        unknown = root / "unknown-schema"
        write_package(unknown)
        metadata = json.loads((unknown / "metadata.json").read_text(encoding="utf-8"))
        metadata["schema_version"] = 2
        (unknown / "metadata.json").write_text(json.dumps(metadata), encoding="utf-8")
        rewrite_checksums(unknown)
        must_reject("unknown-schema", unknown)

        checksum = root / "checksum"
        write_package(checksum)
        (checksum / "database.sql").write_text("changed\n", encoding="utf-8")
        must_reject("checksum", checksum)

        symlink = root / "symlink"
        write_package(symlink)
        (symlink / "database.sql").unlink()
        os.symlink(valid / "database.sql", symlink / "database.sql")
        must_reject("symlink", symlink)

        comment_only = root / "comment-only"
        write_package(comment_only)
        (comment_only / "database.sql").write_text("-- no database here\n/* still empty */\n", encoding="utf-8")
        metadata = json.loads((comment_only / "metadata.json").read_text(encoding="utf-8"))
        metadata["payloads"]["database.sql"]["sha256"] = ops.sha256_file(comment_only / "database.sql")
        (comment_only / "metadata.json").write_text(json.dumps(metadata), encoding="utf-8")
        rewrite_checksums(comment_only)
        must_reject("comment-only-dump", comment_only)

        manifest_fields = root / "manifest-fields"
        write_package(manifest_fields)
        manifest = json.loads((manifest_fields / "avatar-manifest.json").read_text(encoding="utf-8"))
        manifest["unexpected"] = True
        (manifest_fields / "avatar-manifest.json").write_text(json.dumps(manifest), encoding="utf-8")
        metadata = json.loads((manifest_fields / "metadata.json").read_text(encoding="utf-8"))
        metadata["payloads"]["avatar-manifest.json"]["sha256"] = ops.sha256_file(manifest_fields / "avatar-manifest.json")
        (manifest_fields / "metadata.json").write_text(json.dumps(metadata), encoding="utf-8")
        rewrite_checksums(manifest_fields)
        must_reject("manifest-unknown-field", manifest_fields)

        tar_mismatch = root / "tar-mismatch"
        write_package(tar_mismatch)
        write_tar(tar_mismatch / "avatar-volume.tgz", {
            f"{AVATAR_KEY}/content": b"different-content",
            f"{AVATAR_KEY}/metadata.json": json.dumps(AVATAR_METADATA).encode(),
        })
        metadata = json.loads((tar_mismatch / "metadata.json").read_text(encoding="utf-8"))
        metadata["payloads"]["avatar-volume.tgz"]["sha256"] = ops.sha256_file(tar_mismatch / "avatar-volume.tgz")
        (tar_mismatch / "metadata.json").write_text(json.dumps(metadata), encoding="utf-8")
        rewrite_checksums(tar_mismatch)
        must_reject("manifest-tar-mismatch", tar_mismatch)

        missing_object_metadata = root / "missing-object-metadata"
        write_package(missing_object_metadata)
        write_tar(missing_object_metadata / "avatar-volume.tgz", {
            f"{AVATAR_KEY}/content": AVATAR_PAYLOAD,
        })
        metadata = json.loads((missing_object_metadata / "metadata.json").read_text(encoding="utf-8"))
        metadata["payloads"]["avatar-volume.tgz"]["sha256"] = ops.sha256_file(missing_object_metadata / "avatar-volume.tgz")
        (missing_object_metadata / "metadata.json").write_text(json.dumps(metadata), encoding="utf-8")
        rewrite_checksums(missing_object_metadata)
        must_reject("missing-object-metadata", missing_object_metadata)

        unsafe_object_metadata = root / "unsafe-object-metadata"
        write_package(unsafe_object_metadata)
        unsafe = dict(AVATAR_METADATA)
        unsafe["AbsolutePath"] = "/private/customer.jpg"
        write_tar(unsafe_object_metadata / "avatar-volume.tgz", {
            f"{AVATAR_KEY}/content": AVATAR_PAYLOAD,
            f"{AVATAR_KEY}/metadata.json": json.dumps(unsafe).encode(),
        })
        metadata = json.loads((unsafe_object_metadata / "metadata.json").read_text(encoding="utf-8"))
        metadata["payloads"]["avatar-volume.tgz"]["sha256"] = ops.sha256_file(unsafe_object_metadata / "avatar-volume.tgz")
        (unsafe_object_metadata / "metadata.json").write_text(json.dumps(metadata), encoding="utf-8")
        rewrite_checksums(unsafe_object_metadata)
        must_reject("unsafe-object-metadata", unsafe_object_metadata)

        missing_counts = root / "missing-counts"
        write_package(missing_counts)
        metadata = json.loads((missing_counts / "metadata.json").read_text(encoding="utf-8"))
        del metadata["database_counts"]
        (missing_counts / "metadata.json").write_text(json.dumps(metadata), encoding="utf-8")
        rewrite_checksums(missing_counts)
        must_reject("missing-database-counts", missing_counts)

        traversal = root / "traversal"
        write_package(traversal)
        write_tar(traversal / "avatar-volume.tgz", {"../escape": AVATAR_PAYLOAD})
        metadata = json.loads((traversal / "metadata.json").read_text(encoding="utf-8"))
        metadata["payloads"]["avatar-volume.tgz"]["sha256"] = ops.sha256_file(traversal / "avatar-volume.tgz")
        (traversal / "metadata.json").write_text(json.dumps(metadata), encoding="utf-8")
        rewrite_checksums(traversal)
        must_reject("tar-traversal", traversal)

        changing = root / "changing"
        write_package(changing)
        original_copy = ops.shutil.copyfileobj
        changed = False

        def mutate_after_copy(reader, writer, length):
            nonlocal changed
            original_copy(reader, writer, length)
            if not changed:
                changed = True
                with (changing / "database.sql").open("ab") as stream:
                    stream.write(b"-- changed during snapshot\n")

        ops.shutil.copyfileobj = mutate_after_copy
        try:
            try:
                ops.cmd_snapshot(Namespace(input=changing, output=root / "changed-staging", project="synthetic-project"))
            except ops.ContractError:
                pass
            else:
                raise AssertionError("input-changed snapshot was accepted")
        finally:
            ops.shutil.copyfileobj = original_copy

        published = root / "published"
        source = root / "publish-source"
        write_package(source)
        ops.cmd_publish(Namespace(input=source, output=published, project="synthetic-project"))
        ops.validate_package(published, "synthetic-project")
        if source.exists():
            raise AssertionError("published source still exists")

        no_replace_source = root / "publish-no-replace-source"
        write_package(no_replace_source)
        occupied = root / "occupied"
        occupied.mkdir()
        try:
            ops.cmd_publish(Namespace(input=no_replace_source, output=occupied, project="synthetic-project"))
        except ops.ContractError:
            pass
        else:
            raise AssertionError("publish replaced an existing leaf")
        ops.validate_package(no_replace_source, "synthetic-project")

        symlink_source = root / "publish-symlink-source"
        write_package(symlink_source)
        symlink_leaf = root / "publish-symlink-leaf"
        os.symlink(root / "missing-target", symlink_leaf)
        try:
            ops.cmd_publish(Namespace(
                input=symlink_source, output=symlink_leaf, project="synthetic-project",
            ))
        except ops.ContractError:
            pass
        else:
            raise AssertionError("publish replaced a symlink leaf")
        if not symlink_leaf.is_symlink():
            raise AssertionError("publish altered the competing symlink leaf")
        ops.validate_package(symlink_source, "synthetic-project")

        counts_file = root / "actual-counts.json"
        counts_file.write_text(json.dumps(DB_COUNTS), encoding="utf-8")
        ops.cmd_verify_db_counts(Namespace(
            metadata=published / "metadata.json", actual=counts_file,
        ))
        changed_counts = dict(DB_COUNTS)
        changed_counts["customers"] += 1
        counts_file.write_text(json.dumps(changed_counts), encoding="utf-8")
        try:
            ops.cmd_verify_db_counts(Namespace(
                metadata=published / "metadata.json", actual=counts_file,
            ))
        except ops.ContractError:
            pass
        else:
            raise AssertionError("changed database counts were accepted")

    print("v1 ops package helper self-test: passed")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
