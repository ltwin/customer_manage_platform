#!/usr/bin/env python3
"""Fail-closed pre-dispatch gate for creative planning evidence approvals."""

from __future__ import annotations

import argparse
import hashlib
import json
import re
import sys
from pathlib import Path
from typing import Any


ALLOWED_BINDINGS = {
    ("shoot-plan-crm-integration", "stage-1-evidence-go"),
    ("plan-business-feedback", "stage-2-evidence-go"),
}
ALLOWED_DECISION_STATUS = {"pending", "approved", "rejected"}
EXIT_CODES = {"passed": 0, "needs-human": 2, "failed": 3, "blocked": 4}
SHA256_RE = re.compile(r"^[0-9a-f]{64}$")


def emit(status: str, feature: str, decision: str, reason: str, next_action: str, **evidence: Any) -> int:
    payload = {
        "status": status,
        "feature": feature,
        "decision": decision,
        "reason": reason,
        "next": next_action,
        "evidence": evidence,
    }
    print(json.dumps(payload, ensure_ascii=False, sort_keys=True))
    return EXIT_CODES[status]


def scalar(raw: str) -> str:
    value = raw.strip()
    if len(value) >= 2 and value[0] == value[-1] and value[0] in {'"', "'"}:
        return value[1:-1]
    return value


def parse_approval_frontmatter(path: Path) -> dict[str, Any]:
    text = path.read_text(encoding="utf-8")
    lines = text.splitlines()
    if not lines or lines[0].strip() != "---":
        raise ValueError("approval-report missing YAML frontmatter start")
    try:
        end = next(i for i, line in enumerate(lines[1:], 1) if line.strip() == "---")
    except StopIteration as exc:
        raise ValueError("approval-report missing YAML frontmatter end") from exc

    result: dict[str, Any] = {"approvals": {}, "approval_evidence": {}}
    section = ""
    evidence_decision = ""
    for raw in lines[1:end]:
        if not raw.strip() or raw.lstrip().startswith("#"):
            continue
        indent = len(raw) - len(raw.lstrip(" "))
        stripped = raw.strip()
        if indent == 0 and ":" in stripped:
            key, value = stripped.split(":", 1)
            section = key if not value.strip() else ""
            evidence_decision = ""
            if value.strip():
                result[key] = scalar(value)
            continue
        if section == "approvals" and indent == 2 and ":" in stripped:
            key, value = stripped.split(":", 1)
            result["approvals"][key] = scalar(value)
            continue
        if section == "approval_evidence" and indent == 2 and stripped.endswith(":"):
            evidence_decision = stripped[:-1]
            result["approval_evidence"][evidence_decision] = {}
            continue
        if section == "approval_evidence" and evidence_decision and indent == 4 and ":" in stripped:
            key, value = stripped.split(":", 1)
            result["approval_evidence"][evidence_decision][key] = scalar(value)
    return result


def repo_root_for(roadmap: Path) -> Path:
    for candidate in (roadmap, *roadmap.parents):
        if (candidate / ".git").exists():
            return candidate
    raise ValueError("roadmap is not inside a git worktree")


def canonical_evidence_path(roadmap: Path, raw_path: str) -> Path:
    supplied = Path(raw_path)
    if supplied.is_absolute():
        raise ValueError("approval evidence path must be repository-relative")
    repo_root = repo_root_for(roadmap)
    lexical = (repo_root / supplied) if raw_path.startswith(".codestable/") else (roadmap / supplied)
    evidence_root = (roadmap / "evidence").resolve()
    resolved = lexical.resolve(strict=True)
    try:
        resolved.relative_to(evidence_root)
    except ValueError as exc:
        raise ValueError("approval evidence path must stay under roadmap/evidence") from exc
    if not resolved.is_file() or resolved.suffix != ".json":
        raise ValueError("approval evidence must be a regular JSON file")
    return resolved


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--roadmap", required=True)
    parser.add_argument("--feature", required=True)
    parser.add_argument("--decision", required=True)
    parser.add_argument("--json", action="store_true", help="kept for the fixed workflow CLI")
    args = parser.parse_args()

    binding = (args.feature, args.decision)
    if binding not in ALLOWED_BINDINGS:
        return emit(
            "blocked",
            args.feature,
            args.decision,
            "feature/decision mapping is not recognized",
            "use the exact mapping declared in goal-protocol-feature-loop.md",
        )

    try:
        roadmap = Path(args.roadmap).resolve(strict=True)
    except (FileNotFoundError, RuntimeError) as exc:
        return emit("blocked", args.feature, args.decision, str(exc), "provide the canonical roadmap directory")
    if not roadmap.is_dir() or roadmap.name != "creative-shoot-planning":
        return emit(
            "blocked",
            args.feature,
            args.decision,
            "roadmap identity is not creative-shoot-planning",
            "provide .codestable/roadmap/creative-shoot-planning",
        )

    approval_path = roadmap / "approval-report.md"
    try:
        approval = parse_approval_frontmatter(approval_path)
    except (OSError, UnicodeError, ValueError) as exc:
        return emit("blocked", args.feature, args.decision, str(exc), "repair the canonical approval report")
    if approval.get("doc_type") != "approval-report" or approval.get("unit") != "creative-shoot-planning":
        return emit(
            "blocked",
            args.feature,
            args.decision,
            "approval-report identity/schema is not recognized",
            "repair doc_type/unit before retrying",
        )

    decision_status = approval["approvals"].get(args.decision)
    if decision_status not in ALLOWED_DECISION_STATUS:
        return emit(
            "blocked",
            args.feature,
            args.decision,
            "named decision is missing or has an unknown status",
            "repair the canonical named decision",
            approval=str(approval_path),
        )
    if decision_status == "pending":
        return emit(
            "needs-human",
            args.feature,
            args.decision,
            "real-shoot evidence decision is pending",
            "generate the canonical evidence report and obtain owner approval; keep current_feature_index unchanged",
            approval=str(approval_path),
        )
    if decision_status == "rejected":
        return emit(
            "failed",
            args.feature,
            args.decision,
            "owner rejected the evidence go decision",
            "keep the goal in handoff until the product direction is revised",
            approval=str(approval_path),
        )

    binding_data = approval["approval_evidence"].get(args.decision)
    if not isinstance(binding_data, dict):
        return emit(
            "failed",
            args.feature,
            args.decision,
            "approved decision has no approval_evidence binding",
            "atomically bind path, SHA-256 and gate version in approval-report.md",
        )
    raw_path = binding_data.get("path", "")
    expected_sha = binding_data.get("sha256", "")
    expected_gate_version = binding_data.get("gate_version", "")
    if not raw_path or not SHA256_RE.fullmatch(expected_sha) or not expected_gate_version:
        return emit(
            "failed",
            args.feature,
            args.decision,
            "approved decision has an incomplete path/SHA-256/gate-version binding",
            "repair the atomic approval binding and obtain owner confirmation if evidence bytes changed",
        )

    try:
        evidence_path = canonical_evidence_path(roadmap, raw_path)
        evidence_bytes = evidence_path.read_bytes()
    except (OSError, RuntimeError, ValueError) as exc:
        return emit("failed", args.feature, args.decision, str(exc), "restore the bound canonical evidence file")

    actual_sha = hashlib.sha256(evidence_bytes).hexdigest()
    if actual_sha != expected_sha:
        return emit(
            "failed",
            args.feature,
            args.decision,
            "evidence SHA-256 does not match the owner-approved binding",
            "regenerate evidence and obtain a new owner approval",
            evidence_path=str(evidence_path),
            expected_sha256=expected_sha,
            actual_sha256=actual_sha,
        )
    try:
        report = json.loads(evidence_bytes)
    except (UnicodeDecodeError, json.JSONDecodeError) as exc:
        return emit("blocked", args.feature, args.decision, str(exc), "repair the canonical evidence JSON schema")
    if not isinstance(report, dict):
        return emit("blocked", args.feature, args.decision, "evidence JSON root must be an object", "repair the evidence schema")

    report_gate_version = report.get("gate_version")
    report_status = report.get("status")
    report_stale = report.get("stale", False)
    if not isinstance(report_gate_version, str) or not report_gate_version or not isinstance(report_status, str):
        return emit(
            "blocked",
            args.feature,
            args.decision,
            "evidence JSON is missing string gate_version/status",
            "regenerate with the versioned stage evidence schema",
        )
    if report_gate_version != expected_gate_version:
        return emit(
            "failed",
            args.feature,
            args.decision,
            "evidence gate version does not match the owner-approved binding",
            "regenerate evidence and obtain a new owner approval",
            expected_gate_version=expected_gate_version,
            actual_gate_version=report_gate_version,
        )
    if not isinstance(report_stale, bool):
        return emit("blocked", args.feature, args.decision, "evidence stale must be boolean when present", "repair the evidence schema")
    if report_stale or report_status in {"stale", "failed", "blocked"}:
        return emit(
            "failed",
            args.feature,
            args.decision,
            "evidence report is not current and passed",
            "regenerate evidence and obtain a new owner approval",
            evidence_status=report_status,
            evidence_stale=report_stale,
        )
    if report_status != "passed":
        return emit(
            "blocked",
            args.feature,
            args.decision,
            "evidence status is not a recognized terminal pass/fail value",
            "repair the evidence schema",
            evidence_status=report_status,
        )

    return emit(
        "passed",
        args.feature,
        args.decision,
        "owner approval and evidence binding are valid",
        "continue feature implementation dispatch",
        approval=str(approval_path),
        evidence_path=str(evidence_path),
        sha256=actual_sha,
        gate_version=report_gate_version,
    )


if __name__ == "__main__":
    sys.exit(main())
