#!/usr/bin/env python3
"""Negative and publication tests for the rehearsal wrapper contract."""

from __future__ import annotations

import copy
import json
import stat
import tempfile
from datetime import datetime, timezone
from pathlib import Path
from typing import Any

from planningbackup.package import ContractError, sha256_file, write_json

from .rehearsal import run_rehearsal


NOW = datetime(2026, 8, 18, 0, 30, tzinfo=timezone.utc)
APP_DIGEST = "sha256:" + "1" * 64
POSTGRES_DIGEST = "sha256:" + "2" * 64
TOOL_DIGEST = "sha256:" + "3" * 64


def _report(identity_sha256: str) -> dict[str, Any]:
    stages = []
    for index, stage in enumerate(("preflight", "stop_writes", "replace", "migrate", "validate", "reopen")):
        stages.append({
            "stage": stage,
            "status": "passed",
            "started_at": f"2026-08-18T00:0{index}:00Z",
            "finished_at": f"2026-08-18T00:0{index + 1}:00Z",
        })
    return {
        "schema": "planning-restore-rehearsal-v1",
        "rehearsal_id": "owner-approved-run-1",
        "environment_class": "production_shaped_non_production",
        "deployment_identity_sha256": identity_sha256,
        "source_image_digests": {"app": APP_DIGEST, "postgres": POSTGRES_DIGEST},
        "restore_tool_digest": TOOL_DIGEST,
        "package_sha256": "4" * 64,
        "stage_results": stages,
        "database_oracle": {"status": "passed", "sha256": "5" * 64},
        "avatar_oracle": {"status": "passed", "sha256": "6" * 64},
        "planning_media_oracle": {"status": "passed", "sha256": "7" * 64},
        "failure_cases": [
            {"id": "reject-before-mutation", "kind": "preflight_rejection", "status": "passed", "terminal_state": "rejected_before_mutation"},
            {"id": "stop-after-replace", "kind": "destructive_failure_stop", "status": "passed", "terminal_state": "failed_restore_stopped"},
        ],
        "started_at": "2026-08-18T00:00:00Z",
        "finished_at": "2026-08-18T00:06:00Z",
        "status": "passed",
    }


def _hook(path: Path, report: dict[str, Any] | None) -> None:
    if report is None:
        source = "#!/usr/bin/env python3\nraise SystemExit(0)\n"
    else:
        source = """#!/usr/bin/env python3
import argparse
import json
from pathlib import Path

parser = argparse.ArgumentParser()
parser.add_argument("--config", required=True)
parser.add_argument("--result", required=True)
args = parser.parse_args()
config = json.loads(Path(args.config).read_text(encoding="utf-8"))
if Path(config["deployment_identity"]["path"]).name != "approved-deployment-identity.json":
    raise SystemExit(8)
Path(args.result).write_text(json.dumps(REPORT, sort_keys=True), encoding="utf-8")
""".replace("REPORT", repr(report))
    path.write_text(source, encoding="utf-8")
    path.chmod(0o700)


def _identity_mutating_hook(path: Path, identity: Path, report: dict[str, Any], replacement: dict[str, Any]) -> None:
    source = """#!/usr/bin/env python3
import argparse
import json
from pathlib import Path

parser = argparse.ArgumentParser()
parser.add_argument("--config", required=True)
parser.add_argument("--result", required=True)
args = parser.parse_args()
config = json.loads(Path(args.config).read_text(encoding="utf-8"))
if Path(config["deployment_identity"]["path"]).name != "approved-deployment-identity.json":
    raise SystemExit(8)
Path(IDENTITY).write_text(json.dumps(REPLACEMENT, sort_keys=True), encoding="utf-8")
Path(args.result).write_text(json.dumps(REPORT, sort_keys=True), encoding="utf-8")
""".replace("IDENTITY", repr(str(identity))).replace("REPLACEMENT", repr(replacement)).replace("REPORT", repr(report))
    path.write_text(source, encoding="utf-8")
    path.chmod(0o700)


def _config(path: Path, identity: Path, hook: Path) -> None:
    write_json(path, {
        "schema": "planning-restore-rehearsal-config-v1",
        "environment_class": "production_shaped_non_production",
        "owner_approval_ref": "owner-approval:test-only",
        "approval_starts_at": "2026-08-18T00:00:00Z",
        "approval_expires_at": "2026-08-18T01:00:00Z",
        "deployment_identity": {"path": str(identity), "sha256": sha256_file(identity)},
        "hook": {"path": str(hook), "sha256": sha256_file(hook)},
        "expected_image_digests": {"app": APP_DIGEST, "postgres": POSTGRES_DIGEST, "restore_tool": TOOL_DIGEST},
    })


def _expect_rejected(config: Path, evidence: Path, message: str) -> None:
    try:
        run_rehearsal(config, evidence, NOW)
    except ContractError:
        if (evidence / "planning_restore_rehearsal.json").exists():
            raise AssertionError("rejected rehearsal published evidence")
        return
    raise AssertionError(message)


def main() -> int:
    with tempfile.TemporaryDirectory(prefix="planning-rehearsal-test-") as raw:
        root = Path(raw)
        identity = root / "deployment-identity.json"
        write_json(identity, {
            "environment_id": "production-shaped-test",
            "deployment_id": "isolated-deployment-1",
            "compose_project": "planning-rehearsal",
            "storage_layout_version": "planning-media-v1",
        })
        evidence = root / "evidence"
        evidence.mkdir(mode=0o700)
        config = root / "config.json"
        hook = root / "hook.py"
        valid = _report(sha256_file(identity))

        _hook(hook, None)
        _config(config, identity, hook)
        _expect_rejected(config, evidence, "zero-exit hook without a result was accepted")

        _hook(hook, {})
        _config(config, identity, hook)
        _expect_rejected(config, evidence, "empty rehearsal result was accepted")

        synthetic = copy.deepcopy(valid)
        synthetic["environment_class"] = "synthetic"
        _hook(hook, synthetic)
        _config(config, identity, hook)
        _expect_rejected(config, evidence, "synthetic environment was accepted")

        wrong_order = copy.deepcopy(valid)
        wrong_order["stage_results"][1], wrong_order["stage_results"][2] = wrong_order["stage_results"][2], wrong_order["stage_results"][1]
        _hook(hook, wrong_order)
        _config(config, identity, hook)
        _expect_rejected(config, evidence, "wrong stage order was accepted")

        empty_oracles = copy.deepcopy(valid)
        empty_oracles["database_oracle"] = {}
        _hook(hook, empty_oracles)
        _config(config, identity, hook)
        _expect_rejected(config, evidence, "empty restore oracle was accepted")

        wrong_digest = copy.deepcopy(valid)
        wrong_digest["restore_tool_digest"] = "sha256:" + "9" * 64
        _hook(hook, wrong_digest)
        _config(config, identity, hook)
        _expect_rejected(config, evidence, "unexpected restore-tool digest was accepted")

        replacement_identity = {
            "environment_id": "production-shaped-test",
            "deployment_id": "isolated-deployment-2",
            "compose_project": "planning-rehearsal",
            "storage_layout_version": "planning-media-v1",
        }
        replacement_path = root / "replacement-identity.json"
        write_json(replacement_path, replacement_identity)
        malicious_report = _report(sha256_file(replacement_path))
        _identity_mutating_hook(hook, identity, malicious_report, replacement_identity)
        _config(config, identity, hook)
        _expect_rejected(config, evidence, "hook replaced the approved deployment identity")

        write_json(identity, {
            "environment_id": "production-shaped-test",
            "deployment_id": "isolated-deployment-1",
            "compose_project": "planning-rehearsal",
            "storage_layout_version": "planning-media-v1",
        })
        valid = _report(sha256_file(identity))

        _hook(hook, valid)
        _config(config, identity, hook)
        destination = run_rehearsal(config, evidence, NOW)
        if not destination.is_file() or stat.S_IMODE(destination.stat().st_mode) != 0o600:
            raise AssertionError("validated rehearsal evidence was not privately published")
        try:
            run_rehearsal(config, evidence, NOW)
        except ContractError:
            pass
        else:
            raise AssertionError("rehearsal evidence no-replace contract was bypassed")

    print("planning rehearsal wrapper selftest: passed")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
