#!/usr/bin/env python3
"""Build the dependency admission matrix without touching business state."""

from __future__ import annotations

import argparse
import hashlib
import json
import subprocess
from pathlib import Path
from typing import Any

from planningbackup.package import canonical_json, sha256_file


ROWS = (
    ("core", "shoot-plan-core", "backend/internal/shootplanning/execution.go", ("RunModeSession", "FinalizationRevision"), "Bearer API + accepted finalization projection", "public list/detail/run route and scoped repository"),
    ("media", "planning-reference-assets", "backend/internal/planningmedia/inventory.go", ("PlanningMediaManifestV1", "BuildManifest"), "planningmedia maintenance manifest", "exact manifest + mounted volume"),
    ("ingestion", "plan-ingestion-capture", "backend/internal/shootplanning/ingestion/evidence.go", ("Stage1EvidenceGateVersion", "GenerateStage1EvidenceFromStore"), "stage-1 canonical report", "atomic commit and sample decision"),
    ("crm", "shoot-plan-crm-integration", "backend/internal/platform/httpapi/planning_summary.go", ("PlanningSummary", "Link"), "CRM bearer routes", "account-scoped projection"),
    ("share", "plan-share-collaboration", "backend/internal/planshare/ports.go", ("ShareObservationStore", "EnsureFeedback"), "anonymous/Bearer share routes", "proposal/full allowlist"),
    ("reminders", "plan-assignment-reminders", "backend/internal/reminder/freshness.go", ("AssignmentReminderFreshness", "PlanAssignmentReminderView"), "reminder API + maintenance freshness", "photographer-only recipient"),
    ("business", "plan-business-feedback", "backend/internal/shootplanning/business/application.go", ("Draft", "DecisionResult"), "private business draft routes", "facts/draft/CAS apply"),
)


def _revision(repo: Path) -> str:
    value = subprocess.run(["git", "rev-parse", "HEAD"], cwd=repo, text=True, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL, check=False).stdout.strip()
    return value or "working-tree"


def _row(repo: Path, row_id: str, owner: str, relative: str, needles: tuple[str, ...], seam: str, negative: str) -> dict[str, Any]:
    path = repo / relative
    text = path.read_text(encoding="utf-8") if path.is_file() else ""
    present = path.exists() and all(needle in text or needle in str(path) for needle in needles)
    result: dict[str, Any] = {
        "id": row_id,
        "category": "dependency",
        "requirement_ref": f"creative-planning-v1-hardening:{row_id}",
        "owner_feature": owner,
        "public_seam": seam,
        "scenario_ids": ["A1", "A2"],
        "evidence_ids": [f"conformance-{row_id}"],
        "required_viewports": [1600, 1280, 375, "200%"],
        "required_failure_ids": ["F-ownership", "F-negative"],
        "status": "conformant" if present else "blocked_not_implemented",
        "reason": None if present else "required-path-or-symbol-missing",
        "positive_probe": {"status": "passed" if present else "failed", "path": relative, "symbols": list(needles)},
        "negative_probe": {"assertion": negative},
    }
    if path.is_file():
        result["source_sha256"] = sha256_file(path)
    return result


def build_conformance(repo: Path) -> dict[str, Any]:
    openapi = repo / "api/openapi.yaml"
    rows = [_row(repo, *row) for row in ROWS]
    status = "passed" if all(row["status"] == "conformant" for row in rows) else "blocked"
    return {
        "schema": "planning-v1-dependency-conformance-v2",
        "roadmap": "creative-shoot-planning",
        "openapi_sha256": sha256_file(openapi),
        "build_revision": _revision(repo),
        "config_fingerprint": hashlib.sha256(canonical_json({"compose": "planning_media_data", "storage_layout": "planning-media-v1"})).hexdigest(),
        "gate_dispositions": {"stage-1-evidence-go": "pending", "stage-2-evidence-go": "pending"},
        "rows": rows,
        "overall_status": status,
        "implementation_dispatch": "owner-authorized-deferred-evidence" if status == "passed" else "blocked_upstream_scope_defect",
    }


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--repo", type=Path, default=Path.cwd())
    parser.add_argument("--output", type=Path)
    args = parser.parse_args(argv)
    result = build_conformance(args.repo.resolve())
    encoded = canonical_json(result)
    if args.output:
        args.output.write_bytes(encoded)
    else:
        print(encoded.decode(), end="")
    return 0 if result["overall_status"] == "passed" else 9


if __name__ == "__main__":
    raise SystemExit(main())
