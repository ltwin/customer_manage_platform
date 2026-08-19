#!/usr/bin/env python3
"""Owner-authorized production-shaped restore rehearsal evidence gate."""

from __future__ import annotations

import argparse
import copy
import os
import re
import stat
import subprocess
import tempfile
from datetime import datetime, timezone
from pathlib import Path
from typing import Any

from planningbackup.package import ContractError, canonical_json, load_json, sha256_file
from planningbackup.preflight import _identity


HEX64 = re.compile(r"^[0-9a-f]{64}$")
IMAGE_DIGEST = re.compile(r"^sha256:[0-9a-f]{64}$")
UTC = re.compile(r"^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}(?:\.[0-9]+)?Z$")
IDENTIFIER = re.compile(r"^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$")
STAGES = ("preflight", "stop_writes", "replace", "migrate", "validate", "reopen")
FAILURE_TERMINALS = {
    "preflight_rejection": "rejected_before_mutation",
    "destructive_failure_stop": "failed_restore_stopped",
}
REPORT_FIELDS = {
    "schema", "rehearsal_id", "environment_class", "deployment_identity_sha256",
    "source_image_digests", "restore_tool_digest", "package_sha256", "stage_results",
    "database_oracle", "avatar_oracle", "planning_media_oracle", "failure_cases",
    "started_at", "finished_at", "status",
}


def _expect_keys(value: Any, fields: set[str], key: str) -> dict[str, Any]:
    if not isinstance(value, dict) or set(value) != fields:
        raise ContractError(key)
    return value


def _timestamp(value: Any, key: str) -> datetime:
    if not isinstance(value, str) or not UTC.fullmatch(value):
        raise ContractError(key)
    try:
        parsed = datetime.fromisoformat(value.replace("Z", "+00:00"))
    except ValueError as error:
        raise ContractError(key) from error
    if parsed.utcoffset() != timezone.utc.utcoffset(parsed):
        raise ContractError(key)
    return parsed


def _regular_executable(path: Path) -> None:
    info = path.lstat()
    if path.is_symlink() or not stat.S_ISREG(info.st_mode) or info.st_mode & 0o111 == 0 or info.st_mode & 0o022:
        raise ContractError("rehearsal-hook-not-executable")


def _file_signature(info: os.stat_result) -> tuple[int, int, int, int]:
    return info.st_dev, info.st_ino, info.st_size, info.st_mtime_ns


def _snapshot_identity(source: Path, destination: Path, expected_sha256: str) -> tuple[int, int, int, int]:
    source_fd = os.open(source, os.O_RDONLY | getattr(os, "O_NOFOLLOW", 0) | getattr(os, "O_CLOEXEC", 0))
    destination_fd = -1
    try:
        before = os.fstat(source_fd)
        if not stat.S_ISREG(before.st_mode):
            raise ContractError("rehearsal-config-deployment-regular")
        digest = __import__("hashlib").sha256()
        destination_fd = os.open(
            destination,
            os.O_WRONLY | os.O_CREAT | os.O_EXCL | getattr(os, "O_CLOEXEC", 0),
            0o400,
        )
        while True:
            chunk = os.read(source_fd, 1024 * 1024)
            if not chunk:
                break
            digest.update(chunk)
            view = memoryview(chunk)
            while view:
                view = view[os.write(destination_fd, view):]
        os.fsync(destination_fd)
        after = os.fstat(source_fd)
        current = os.stat(source, follow_symlinks=False)
        if _file_signature(before) != _file_signature(after) or _file_signature(before) != _file_signature(current):
            raise ContractError("rehearsal-config-deployment-changed")
        if digest.hexdigest() != expected_sha256:
            raise ContractError("rehearsal-config-deployment-digest")
    finally:
        if destination_fd >= 0:
            os.close(destination_fd)
        os.close(source_fd)
    _identity(destination)
    return _file_signature(before)


def _verify_identity_unchanged(path: Path, expected_signature: tuple[int, int, int, int], expected_sha256: str) -> None:
    descriptor = os.open(path, os.O_RDONLY | getattr(os, "O_NOFOLLOW", 0) | getattr(os, "O_CLOEXEC", 0))
    try:
        before = os.fstat(descriptor)
        if not stat.S_ISREG(before.st_mode) or _file_signature(before) != expected_signature:
            raise ContractError("rehearsal-config-deployment-changed")
        digest = __import__("hashlib").sha256()
        while True:
            chunk = os.read(descriptor, 1024 * 1024)
            if not chunk:
                break
            digest.update(chunk)
        after = os.fstat(descriptor)
        current = os.stat(path, follow_symlinks=False)
        if _file_signature(before) != _file_signature(after) or _file_signature(before) != _file_signature(current):
            raise ContractError("rehearsal-config-deployment-changed")
        if digest.hexdigest() != expected_sha256:
            raise ContractError("rehearsal-config-deployment-changed")
    finally:
        os.close(descriptor)


def _write_frozen_config(path: Path, config: dict[str, Any], identity_snapshot: Path) -> None:
    frozen = copy.deepcopy(config)
    frozen["deployment_identity"]["path"] = str(identity_snapshot)
    encoded = canonical_json(frozen)
    descriptor = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | getattr(os, "O_CLOEXEC", 0), 0o400)
    try:
        view = memoryview(encoded)
        while view:
            view = view[os.write(descriptor, view):]
        os.fsync(descriptor)
    finally:
        os.close(descriptor)


def _run_verified_hook(hook_path: Path, expected_sha256: str, config_path: Path, result_path: Path) -> int:
    flags = os.O_RDONLY | getattr(os, "O_NOFOLLOW", 0)
    descriptor = os.open(hook_path, flags)
    verified_hook = result_path.parent / "approved-hook"
    try:
        info = os.fstat(descriptor)
        if not stat.S_ISREG(info.st_mode) or info.st_mode & 0o111 == 0 or info.st_mode & 0o022:
            raise ContractError("rehearsal-hook-not-executable")
        digest = __import__("hashlib").sha256()
        copy_descriptor = os.open(verified_hook, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o500)
        try:
            while True:
                chunk = os.read(descriptor, 1024 * 1024)
                if not chunk:
                    break
                digest.update(chunk)
                view = memoryview(chunk)
                while view:
                    written = os.write(copy_descriptor, view)
                    view = view[written:]
            os.fsync(copy_descriptor)
        finally:
            os.close(copy_descriptor)
        if digest.hexdigest() != expected_sha256:
            raise ContractError("rehearsal-config-hook-digest")
        completed = subprocess.run(
            [str(verified_hook), "--config", str(config_path), "--result", str(result_path)],
            cwd=config_path.parent,
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
            check=False,
        )
        return completed.returncode
    finally:
        os.close(descriptor)


def validate_rehearsal(value: Any) -> dict[str, Any]:
    report = _expect_keys(value, REPORT_FIELDS, "rehearsal-report-schema")
    if report["schema"] != "planning-restore-rehearsal-v1" or report["environment_class"] != "production_shaped_non_production":
        raise ContractError("rehearsal-report-environment")
    if not isinstance(report["rehearsal_id"], str) or not IDENTIFIER.fullmatch(report["rehearsal_id"]):
        raise ContractError("rehearsal-report-id")
    if not isinstance(report["deployment_identity_sha256"], str) or not HEX64.fullmatch(report["deployment_identity_sha256"]):
        raise ContractError("rehearsal-report-deployment-digest")
    images = _expect_keys(report["source_image_digests"], {"app", "postgres"}, "rehearsal-report-images")
    if not IMAGE_DIGEST.fullmatch(str(images["app"])) or not IMAGE_DIGEST.fullmatch(str(images["postgres"])):
        raise ContractError("rehearsal-report-image-digest")
    if not IMAGE_DIGEST.fullmatch(str(report["restore_tool_digest"])) or not HEX64.fullmatch(str(report["package_sha256"])):
        raise ContractError("rehearsal-report-tool-package-digest")

    started = _timestamp(report["started_at"], "rehearsal-report-started")
    finished = _timestamp(report["finished_at"], "rehearsal-report-finished")
    if finished < started:
        raise ContractError("rehearsal-report-time-order")
    stages = report["stage_results"]
    if not isinstance(stages, list) or len(stages) != len(STAGES):
        raise ContractError("rehearsal-report-stage-count")
    previous = started
    for expected, stage in zip(STAGES, stages, strict=True):
        row = _expect_keys(stage, {"stage", "status", "started_at", "finished_at"}, "rehearsal-report-stage")
        row_started = _timestamp(row["started_at"], "rehearsal-report-stage-time")
        row_finished = _timestamp(row["finished_at"], "rehearsal-report-stage-time")
        if row["stage"] != expected or row["status"] != "passed" or row_started < previous or row_finished < row_started or row_finished > finished:
            raise ContractError("rehearsal-report-stage-order")
        previous = row_finished

    for name in ("database_oracle", "avatar_oracle", "planning_media_oracle"):
        oracle = _expect_keys(report[name], {"status", "sha256"}, "rehearsal-report-oracle")
        if oracle["status"] != "passed" or not isinstance(oracle["sha256"], str) or not HEX64.fullmatch(oracle["sha256"]):
            raise ContractError("rehearsal-report-oracle-status")

    cases = report["failure_cases"]
    if not isinstance(cases, list):
        raise ContractError("rehearsal-report-failure-cases")
    seen_ids: set[str] = set()
    seen_kinds: set[str] = set()
    for case in cases:
        row = _expect_keys(case, {"id", "kind", "status", "terminal_state"}, "rehearsal-report-failure-case")
        if not isinstance(row["id"], str) or not IDENTIFIER.fullmatch(row["id"]) or row["id"] in seen_ids:
            raise ContractError("rehearsal-report-failure-id")
        if row["kind"] not in FAILURE_TERMINALS or row["status"] != "passed" or row["terminal_state"] != FAILURE_TERMINALS[row["kind"]]:
            raise ContractError("rehearsal-report-failure-terminal")
        seen_ids.add(row["id"])
        seen_kinds.add(row["kind"])
    if seen_kinds != set(FAILURE_TERMINALS) or report["status"] != "passed":
        raise ContractError("rehearsal-report-not-passed")
    return report


def _load_config(path: Path, now: datetime) -> tuple[dict[str, Any], Path, Path]:
    config = _expect_keys(load_json(path), {
        "schema", "environment_class", "owner_approval_ref", "approval_starts_at", "approval_expires_at",
        "deployment_identity", "hook", "expected_image_digests",
    }, "rehearsal-config-schema")
    if config["schema"] != "planning-restore-rehearsal-config-v1" or config["environment_class"] != "production_shaped_non_production":
        raise ContractError("rehearsal-config-environment")
    if not isinstance(config["owner_approval_ref"], str) or not config["owner_approval_ref"] or "\n" in config["owner_approval_ref"] or "\r" in config["owner_approval_ref"]:
        raise ContractError("rehearsal-config-approval-ref")
    starts = _timestamp(config["approval_starts_at"], "rehearsal-config-approval-start")
    expires = _timestamp(config["approval_expires_at"], "rehearsal-config-approval-expiry")
    normalized_now = now.astimezone(timezone.utc)
    if expires <= starts or normalized_now < starts or normalized_now > expires:
        raise ContractError("rehearsal-config-approval-window")

    deployment = _expect_keys(config["deployment_identity"], {"path", "sha256"}, "rehearsal-config-deployment")
    hook = _expect_keys(config["hook"], {"path", "sha256"}, "rehearsal-config-hook")
    images = _expect_keys(config["expected_image_digests"], {"app", "postgres", "restore_tool"}, "rehearsal-config-images")
    for digest in images.values():
        if not isinstance(digest, str) or not IMAGE_DIGEST.fullmatch(digest):
            raise ContractError("rehearsal-config-image-digest")

    deployment_path = Path(deployment["path"]) if isinstance(deployment["path"], str) else Path()
    hook_path = Path(hook["path"]) if isinstance(hook["path"], str) else Path()
    if not deployment_path.is_absolute() or not hook_path.is_absolute():
        raise ContractError("rehearsal-config-path")
    _identity(deployment_path)
    _regular_executable(hook_path)
    if not isinstance(deployment["sha256"], str) or sha256_file(deployment_path) != deployment["sha256"]:
        raise ContractError("rehearsal-config-deployment-digest")
    if not isinstance(hook["sha256"], str) or sha256_file(hook_path) != hook["sha256"]:
        raise ContractError("rehearsal-config-hook-digest")
    return config, deployment_path, hook_path


def run_rehearsal(config_path: Path, evidence_dir: Path, now: datetime | None = None) -> Path:
    current_time = now or datetime.now(timezone.utc)
    config, deployment_path, hook_path = _load_config(config_path, current_time)
    info = evidence_dir.lstat()
    if evidence_dir.is_symlink() or not stat.S_ISDIR(info.st_mode) or stat.S_IMODE(info.st_mode) & 0o077:
        raise ContractError("rehearsal-evidence-directory")
    destination = evidence_dir / "planning_restore_rehearsal.json"
    if destination.exists() or destination.is_symlink():
        raise ContractError("rehearsal-evidence-no-replace")

    with tempfile.TemporaryDirectory(prefix=".planning-rehearsal-", dir=evidence_dir) as raw:
        private_root = Path(raw)
        result_path = private_root / "hook-result.json"
        identity_snapshot = private_root / "approved-deployment-identity.json"
        approved_identity_sha256 = config["deployment_identity"]["sha256"]
        original_signature = _snapshot_identity(deployment_path, identity_snapshot, approved_identity_sha256)
        snapshot_signature = _file_signature(identity_snapshot.stat(follow_symlinks=False))
        frozen_config_path = private_root / "execution-config.json"
        _write_frozen_config(frozen_config_path, config, identity_snapshot)
        hook_code = _run_verified_hook(hook_path, config["hook"]["sha256"], frozen_config_path, result_path)
        _verify_identity_unchanged(deployment_path, original_signature, approved_identity_sha256)
        _verify_identity_unchanged(identity_snapshot, snapshot_signature, approved_identity_sha256)
        if hook_code != 0 or not result_path.is_file() or result_path.is_symlink():
            raise ContractError("rehearsal-hook-result-missing")
        report = validate_rehearsal(load_json(result_path))
        expected = config["expected_image_digests"]
        if report["deployment_identity_sha256"] != approved_identity_sha256:
            raise ContractError("rehearsal-result-deployment-mismatch")
        if report["source_image_digests"] != {"app": expected["app"], "postgres": expected["postgres"]} or report["restore_tool_digest"] != expected["restore_tool"]:
            raise ContractError("rehearsal-result-image-mismatch")
        approval_starts = _timestamp(config["approval_starts_at"], "rehearsal-config-approval-start")
        approval_expires = _timestamp(config["approval_expires_at"], "rehearsal-config-approval-expiry")
        report_started = _timestamp(report["started_at"], "rehearsal-report-started")
        report_finished = _timestamp(report["finished_at"], "rehearsal-report-finished")
        if report_started < approval_starts or report_finished > approval_expires or report_finished > current_time.astimezone(timezone.utc):
            raise ContractError("rehearsal-result-outside-approval-window")
        encoded = canonical_json(report)

    flags = os.O_WRONLY | os.O_CREAT | os.O_EXCL | getattr(os, "O_NOFOLLOW", 0)
    descriptor = os.open(destination, flags, 0o600)
    try:
        with os.fdopen(descriptor, "wb") as stream:
            stream.write(encoded)
            stream.flush()
            os.fsync(stream.fileno())
    except BaseException:
        try:
            destination.unlink()
        except OSError:
            pass
        raise
    return destination


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--config", type=Path, required=True)
    parser.add_argument("--evidence", type=Path, required=True)
    args = parser.parse_args(argv)
    try:
        config_path = Path(os.path.abspath(args.config))
        evidence_path = Path(os.path.abspath(args.evidence))
        destination = run_rehearsal(config_path, evidence_path)
    except (ContractError, OSError, ValueError):
        return 9
    print(f"planning restore rehearsal evidence: {destination}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
