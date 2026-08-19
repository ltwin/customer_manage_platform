#!/usr/bin/env python3
"""Preflight zero-mutation and v1/v2 target-inventory characterization."""

from __future__ import annotations

import tempfile
from pathlib import Path

from .package import load_json, write_json
from .preflight import run_preflight
from .selftest import _legacy_selftest, _write_v2


def main() -> int:
    with tempfile.TemporaryDirectory(prefix="planning-preflight-test-") as raw:
        root = Path(raw)
        legacy = _legacy_selftest()
        v1 = root / "v1"
        legacy.write_package(v1)
        v2 = root / "v2"
        v2.mkdir(mode=0o700)
        _write_v2(v2, v1)
        v2_metadata = load_json(v2 / "metadata.json")
        identity = root / "identity.json"
        write_json(identity, {
            "environment_id": "test",
            "deployment_id": "deployment-1",
            "compose_project": "synthetic-project",
            "storage_layout_version": "planning-media-v1",
        })
        allowed = run_preflight(
            v2,
            identity,
            root / "missing-target",
            512 * 1024 * 1024,
            "sha256:tool",
            "deployment-1",
            {"regular_files": 3, "bytes": 9, "unsafe_entries": 0, "total_entries": 7},
            "b" * 64,
            "sha256:app",
            "sha256:pg",
            "17",
            v2_metadata["database_counts"],
            v2 / "avatar-manifest.json",
        )
        if not allowed["destructive_authorized"]:
            raise AssertionError(f"v2 damaged target should be repairable: {allowed}")

        blocked_space = run_preflight(
            v2, identity, root / "missing-target", None, "sha256:tool", "deployment-1",
            {"regular_files": 0, "bytes": 0, "unsafe_entries": 0, "total_entries": 0},
            "b" * 64, "sha256:app", "sha256:pg", "17",
            v2_metadata["database_counts"], v2 / "avatar-manifest.json",
        )
        if blocked_space["destructive_authorized"] or not any(item.get("id") == "available_space" and item.get("status") != "passed" for item in blocked_space["checks"]):
            raise AssertionError("unknown free space must block destructive restore")

        v1_identity = root / "v1-identity.json"
        write_json(v1_identity, {
            "environment_id": "test",
            "deployment_id": "legacy-deployment",
            "compose_project": "synthetic-project",
            "storage_layout_version": "v1-no-planning-media",
        })
        v1_metadata = load_json(v1 / "metadata.json")
        allowed_v1 = run_preflight(
            v1, v1_identity, root / "legacy-target", 512 * 1024 * 1024,
            v1_metadata["images"]["app"]["id"], "legacy-deployment",
            {"regular_files": 0, "bytes": 0, "unsafe_entries": 0, "total_entries": 0},
            v1_metadata["compose_file_sha256"],
            v1_metadata["images"]["app"]["id"],
            v1_metadata["images"]["postgres"]["id"],
            v1_metadata["postgres_server_major"],
            v1_metadata["database_counts"],
            v1 / "avatar-manifest.json",
        )
        if not allowed_v1["destructive_authorized"]:
            raise AssertionError(f"compatible v1 restore rejected: {allowed_v1}")

        mismatched_generation = run_preflight(
            v1, v1_identity, root / "legacy-target", 512 * 1024 * 1024,
            v1_metadata["images"]["app"]["id"], "wrong-generation",
            {"regular_files": 0, "bytes": 0, "unsafe_entries": 0, "total_entries": 0},
            v1_metadata["compose_file_sha256"],
            v1_metadata["images"]["app"]["id"],
            v1_metadata["images"]["postgres"]["id"],
            v1_metadata["postgres_server_major"],
            v1_metadata["database_counts"],
            v1 / "avatar-manifest.json",
        )
        if mismatched_generation["destructive_authorized"] or not any(item.get("id") == "target_generation_fence" and item.get("status") != "passed" for item in mismatched_generation["checks"]):
            raise AssertionError("v1 target generation mismatch must fail closed")

        blocked_v1 = run_preflight(
            v1, v1_identity, root / "legacy-target", 512 * 1024 * 1024,
            v1_metadata["images"]["app"]["id"], "legacy-deployment",
            {"regular_files": 0, "bytes": 0, "unsafe_entries": 1, "total_entries": 1},
            v1_metadata["compose_file_sha256"],
            v1_metadata["images"]["app"]["id"],
            v1_metadata["images"]["postgres"]["id"],
            v1_metadata["postgres_server_major"],
            v1_metadata["database_counts"],
            v1 / "avatar-manifest.json",
        )
        if blocked_v1["destructive_authorized"] or not any(item.get("reason_key") == "v1-planning-target-nonempty" for item in blocked_v1["checks"]):
            raise AssertionError("v1 non-empty planning target must fail closed")

        inventories = allowed["source_package_inventory"], allowed["target_runtime_inventory"]
        for inventory in inventories:
            if set(inventory) != {"database", "avatar", "planning_media"}:
                raise AssertionError("preflight inventory is not split by restored resource")
            if any(inventory[resource]["status"] not in {"validated", "not_in_schema_v1"} for resource in inventory):
                raise AssertionError("authorized preflight contains an unvalidated resource inventory")

    print("planning restore preflight selftest: passed")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
