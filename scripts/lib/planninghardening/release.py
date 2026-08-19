#!/usr/bin/env python3
"""Fail-closed, read-only release and rollback readiness verifier."""

from __future__ import annotations

import argparse
import hashlib
import json
import re
import stat
import subprocess
import sys
from datetime import datetime, timezone
from pathlib import Path
from typing import Any
from urllib.parse import urlsplit

from planningbackup.package import ContractError, canonical_json, load_json, sha256_file, validate_package
from planningbackup.preflight import _package_sha256

from .evidence import validate_cohort_comparison, validate_stage1, validate_stage2
from .rehearsal import validate_rehearsal


HEX40 = re.compile(r"^[0-9a-f]{40}$")
HEX64 = re.compile(r"^[0-9a-f]{64}$")
IMAGE_DIGEST = re.compile(r"^sha256:[0-9a-f]{64}$")
PLAN_REF = re.compile(r"^sha256-[0-9a-f]{64}$")
MIGRATION = re.compile(r"^([0-9]{4})_[A-Za-z0-9_]+\.up\.sql$")
MARKERS = ("core-v1", "planning-share-v1", "planning-share-reminder-v1")
REQUIRED_CHECK_IDS = (
    "additive_schema_available",
    "candidate_rollback_marker_readers",
    "old_processes_exited",
    "trusted_readiness_inventory_fresh",
    "archive_adjacent_cas_readback",
    "share_reminder_writers_enabled",
    "schema_v2_restore_pair_retained",
)
REQUIRED_CONFORMANCE_IDS = ("core", "media", "ingestion", "crm", "share", "reminders", "business", "goal-control")
REQUIRED_E2E_IDS = (
    "authenticated-login", "plan-create", "brief-save", "shot-readiness-create-link",
    "ingestion-atomic-commit", "crm-business-draft", "proposal-issue-read-revoke", "run-mode-execution",
)
REQUIRED_FAILURE_IDS = (
    "draft-run-rejected", "business-without-order", "proposal-revoked-uniform-404", "run-offline-retry",
    "execution-history-supersession", "ingestion-owner-fault-injection",
    "shared-asset-object-restart-late-delete", "reminder-generation-fence-resolution",
    "production-shaped-destructive-restore",
)
REQUIRED_H1_H2_IDS = (
    "proposal-anonymous-business-redaction", "proposal-revoked-uniform-unavailable",
    "authenticated-private-business-only", "h1-dedicated-no-plan-account", "full-share-feedback-assignment",
)
RESPONSIVE_PAGE_IDS = ("index", "workspace", "run", "expired-share")
REQUIRED_RESPONSIVE_IDS = (
    "authenticated-create-1280",
    *(f"{viewport}-{page}" for viewport in ("desktop-1600", "desktop-1280", "mobile-375", "zoom-200") for page in RESPONSIVE_PAGE_IDS),
)
REQUIRED_VIEWPORTS = ("1600x1000", "1280x900", "375x812-coarse", "640x450-css-dpr2-200-percent")
REQUIRED_AUTOMATED_CHECKS = (
    "horizontal overflow", "main/h1 landmarks", "form labels", "keyboard entry",
    "engineering text exclusion", "coarse target sizing", "200 percent CSS reflow",
)
CHECK_ARTIFACTS = {
    "additive_schema_available": "planning_v1_additive_schema_readiness.json",
    "candidate_rollback_marker_readers": "planning_v1_marker_reader_readiness.json",
    "old_processes_exited": "planning_v1_old_processes_exited.json",
    "trusted_readiness_inventory_fresh": "planning_v1_trusted_readiness_inventory.json",
    "archive_adjacent_cas_readback": "planning_v1_archive_cas_readback.json",
    "share_reminder_writers_enabled": "planning_v1_share_reminder_writers.json",
    "schema_v2_restore_pair_retained": "planning_v1_schema_v2_retention.json",
}
ARTIFACTS = {
    "dependency_conformance_sha256": "planning_v1_dependency_conformance.json",
    "e2e_result_sha256": "planning_v1_e2e_results.json",
    "failure_matrix_sha256": "planning_v1_failure_matrix.json",
    "h1_h2_result_sha256": "planning_v1_h1_h2_negative_matrix.json",
    "prototype_a11y_result_sha256": "planning_v1_prototype_responsive_a11y_matrix.json",
    "planning_evidence_index_sha256": "planning_v1_evidence_index.json",
    "backup_rehearsal_sha256": "planning_restore_rehearsal.json",
}
CANONICAL_ROADMAP = Path(".codestable/roadmap/creative-shoot-planning")
CANONICAL_STAGE_GATES = (
    ("stage-1-evidence-go", "shoot-plan-crm-integration"),
    ("stage-2-evidence-go", "plan-business-feedback"),
)
REHEARSAL_PACKAGE_DIRECTORY = "planning_restore_package"
PLANNINGCTL_READINESS_ARTIFACT = "planning_v1_planningctl_readiness.json"


def _expect_keys(value: Any, fields: set[str], key: str) -> dict[str, Any]:
    if not isinstance(value, dict) or set(value) != fields:
        raise ContractError(key)
    return value


def _repo_revision(repo: Path) -> str:
    completed = subprocess.run(
        ["git", "rev-parse", "HEAD"],
        cwd=repo,
        text=True,
        stdout=subprocess.PIPE,
        stderr=subprocess.DEVNULL,
        check=False,
    )
    revision = completed.stdout.strip()
    if completed.returncode != 0 or not HEX40.fullmatch(revision):
        raise ContractError("release-repository-revision")
    return revision


def _migration_head(repo: Path) -> str:
    directory = repo / "backend/internal/platform/store/migrations"
    versions = []
    for path in directory.glob("*.up.sql"):
        match = MIGRATION.fullmatch(path.name)
        if match is None:
            raise ContractError("release-migration-filename")
        versions.append(match.group(1))
    if not versions:
        raise ContractError("release-migration-head")
    return max(versions, key=int)


def _canonical_stage_approvals(repo: Path) -> dict[str, dict[str, str]]:
    try:
        roadmap = (repo / CANONICAL_ROADMAP).resolve(strict=True)
        gate = roadmap / "goal-tools/planning-evidence-dispatch-gate.py"
        gate_info = gate.lstat()
    except (OSError, RuntimeError) as error:
        raise ContractError("release-canonical-stage-gate") from error
    if gate.is_symlink() or not stat.S_ISREG(gate_info.st_mode):
        raise ContractError("release-canonical-stage-gate")
    bindings: dict[str, dict[str, str]] = {}
    for decision, feature in CANONICAL_STAGE_GATES:
        try:
            completed = subprocess.run(
                [
                    sys.executable,
                    str(gate),
                    "--roadmap", str(roadmap),
                    "--feature", feature,
                    "--decision", decision,
                    "--json",
                ],
                cwd=repo,
                text=True,
                stdout=subprocess.PIPE,
                stderr=subprocess.DEVNULL,
                check=False,
            )
        except OSError as error:
            raise ContractError("release-canonical-stage-gate") from error
        try:
            payload = json.loads(completed.stdout)
        except json.JSONDecodeError as error:
            raise ContractError("release-canonical-stage-gate-output") from error
        if completed.returncode != 0 or not isinstance(payload, dict) or payload.get("status") != "passed":
            raise ContractError(f"release-canonical-stage-approval-{decision}")
        if (
            set(payload) != {"status", "feature", "decision", "reason", "next", "evidence"}
            or payload["feature"] != feature
            or payload["decision"] != decision
        ):
            raise ContractError("release-canonical-stage-gate-output")
        evidence = payload.get("evidence")
        if not isinstance(evidence, dict) or set(evidence) != {"approval", "evidence_path", "sha256", "gate_version"}:
            raise ContractError("release-canonical-stage-gate-output")
        evidence_path = evidence["evidence_path"]
        if not isinstance(evidence_path, str) or not Path(evidence_path).is_absolute():
            raise ContractError("release-canonical-stage-gate-output")
        try:
            canonical_path = Path(evidence_path).resolve(strict=True)
            evidence_root = (roadmap / "evidence").resolve(strict=True)
            canonical_path.relative_to(evidence_root)
        except (OSError, RuntimeError, ValueError) as error:
            raise ContractError("release-canonical-stage-path") from error
        if canonical_path.is_symlink() or not stat.S_ISREG(canonical_path.lstat().st_mode):
            raise ContractError("release-canonical-stage-path")
        if evidence["approval"] != str((roadmap / "approval-report.md").resolve(strict=True)):
            raise ContractError("release-canonical-stage-gate-output")
        if not isinstance(evidence["sha256"], str) or not HEX64.fullmatch(evidence["sha256"]):
            raise ContractError("release-canonical-stage-gate-output")
        if not isinstance(evidence["gate_version"], str) or not evidence["gate_version"]:
            raise ContractError("release-canonical-stage-gate-output")
        bindings[decision] = {
            "evidence_path": str(canonical_path),
            "sha256": evidence["sha256"],
            "gate_version": evidence["gate_version"],
        }
    return bindings


def _resolve_bound_stage_path(raw_path: Any, *, repo: Path, index_dir: Path, canonical_path: str) -> Path:
    if not isinstance(raw_path, str) or not raw_path:
        raise ContractError("release-evidence-stage-path")
    supplied = Path(raw_path)
    if supplied.is_absolute() or ".." in supplied.parts:
        raise ContractError("release-evidence-stage-path")
    lexical = repo / supplied if raw_path.startswith(".codestable/") else index_dir / supplied
    try:
        resolved = lexical.resolve(strict=True)
    except (OSError, RuntimeError) as error:
        raise ContractError("release-evidence-stage-path") from error
    if resolved != Path(canonical_path).resolve(strict=True):
        raise ContractError("release-evidence-stage-canonical-binding")
    if resolved.is_symlink() or not stat.S_ISREG(resolved.lstat().st_mode):
        raise ContractError("release-evidence-stage-path")
    return resolved


def _validate_rehearsal_package(bundle_dir: Path, rehearsal: dict[str, Any]) -> None:
    package = bundle_dir / REHEARSAL_PACKAGE_DIRECTORY
    try:
        info = package.lstat()
    except OSError as error:
        raise ContractError("release-rehearsal-package-missing") from error
    if package.is_symlink() or not stat.S_ISDIR(info.st_mode):
        raise ContractError("release-rehearsal-package-directory")
    metadata = validate_package(package)
    if metadata.get("schema_version") != 2:
        raise ContractError("release-rehearsal-package-schema")
    if _package_sha256(package) != rehearsal["package_sha256"]:
        raise ContractError("release-rehearsal-package-digest")
    images = metadata.get("images")
    if not isinstance(images, dict) or images.get("app") != rehearsal["source_image_digests"]["app"] or images.get("postgres") != rehearsal["source_image_digests"]["postgres"] or images.get("restore_tool") != rehearsal["restore_tool_digest"]:
        raise ContractError("release-rehearsal-package-images")


def _validate_conformance(value: Any, revision: str, openapi_sha256: str, roadmap_sha256: str) -> bool:
    fields = {
        "schema", "roadmap", "roadmap_sha256", "openapi_sha256", "build_revision",
        "config_fingerprint", "gate_dispositions", "rows", "overall_status", "implementation_dispatch",
    }
    report = _expect_keys(value, fields, "release-conformance-schema")
    if report["schema"] != "planning-v1-dependency-conformance-v1" or report["roadmap"] != "creative-shoot-planning":
        raise ContractError("release-conformance-version")
    if report["build_revision"] != revision or report["openapi_sha256"] != openapi_sha256:
        raise ContractError("release-conformance-stale")
    expected_fingerprint = hashlib.sha256(canonical_json({"compose": "planning_media_data", "storage_layout": "planning-media-v1"})).hexdigest()
    if report["roadmap_sha256"] != roadmap_sha256 or report["config_fingerprint"] != expected_fingerprint:
        raise ContractError("release-conformance-digest")
    if report["gate_dispositions"] != {"stage-1-evidence-go": "pending", "stage-2-evidence-go": "pending"} or report["implementation_dispatch"] != "owner-authorized-deferred-evidence":
        raise ContractError("release-conformance-gates")
    rows = report["rows"]
    if not isinstance(rows, list):
        raise ContractError("release-conformance-rows")
    identifiers: list[str] = []
    common_fields = {
        "id", "category", "requirement_ref", "owner_feature", "public_seam", "scenario_ids",
        "evidence_ids", "required_viewports", "required_failure_ids", "status", "reason",
        "accepted_artifact_refs", "positive_probe", "negative_probe", "source_sha256",
    }
    for row in rows:
        if not isinstance(row, dict) or set(row) not in (common_fields, common_fields | {"gate_disposition"}):
            raise ContractError("release-conformance-row")
        if not isinstance(row["id"], str) or row["id"] in identifiers:
            raise ContractError("release-conformance-row")
        identifiers.append(row["id"])
        if row.get("status") != "conformant":
            raise ContractError("release-conformance-not-conformant")
        if not HEX64.fullmatch(str(row["source_sha256"])) or not row["accepted_artifact_refs"]:
            raise ContractError("release-conformance-row-evidence")
        if row["id"] == "goal-control" and row.get("gate_disposition") != "pending_owner_evidence":
            raise ContractError("release-conformance-gate-disposition")
    if tuple(identifiers) != REQUIRED_CONFORMANCE_IDS:
        raise ContractError("release-conformance-required-rows")
    if report["overall_status"] != "passed":
        raise ContractError("release-conformance-not-passed")
    return True


def _validate_e2e(value: Any) -> bool:
    report = _expect_keys(value, {
        "schema", "base_origin", "authenticated", "plan_ref", "credential_material_persisted",
        "trace_material_persisted", "scenarios", "overall_status", "failure",
    }, "release-e2e-schema")
    if report["schema"] != "planning-v1-e2e-results-v1":
        raise ContractError("release-e2e-version")
    origin = urlsplit(report["base_origin"]) if isinstance(report["base_origin"], str) else None
    if origin is None or origin.scheme not in ("http", "https") or not origin.netloc or origin.username or origin.password or origin.path not in ("", "/") or origin.query or origin.fragment:
        raise ContractError("release-e2e-origin")
    if report["authenticated"] is not True or report["credential_material_persisted"] is not False or report["trace_material_persisted"] is not False:
        raise ContractError("release-e2e-sanitization")
    if not isinstance(report["plan_ref"], str) or not PLAN_REF.fullmatch(report["plan_ref"]):
        raise ContractError("release-e2e-plan-ref")
    scenarios = report["scenarios"]
    if not isinstance(scenarios, list):
        raise ContractError("release-e2e-scenarios")
    seen: set[str] = set()
    for scenario in scenarios:
        if not isinstance(scenario, dict) or set(scenario) != {"id", "status", "assertions"}:
            raise ContractError("release-e2e-scenario")
        if not isinstance(scenario["id"], str) or not scenario["id"] or scenario["id"] in seen or scenario["status"] != "passed":
            raise ContractError("release-e2e-scenario-status")
        if not isinstance(scenario["assertions"], list) or not scenario["assertions"] or not all(isinstance(item, str) and item for item in scenario["assertions"]):
            raise ContractError("release-e2e-assertions")
        seen.add(scenario["id"])
    if tuple(scenario["id"] for scenario in scenarios) != REQUIRED_E2E_IDS:
        raise ContractError("release-e2e-required-scenarios")
    if report["overall_status"] != "passed" or report["failure"] is not None:
        raise ContractError("release-e2e-not-passed")
    return True


def _validate_failure_matrix(value: Any) -> bool:
    report = _expect_keys(value, {"schema", "cases", "executed_case_count", "coverage_complete", "deferred_cases", "overall_status"}, "release-failure-schema")
    if report["schema"] != "planning-v1-failure-matrix-v1" or not isinstance(report["cases"], list):
        raise ContractError("release-failure-version")
    if isinstance(report["executed_case_count"], bool) or report["executed_case_count"] != len(report["cases"]):
        raise ContractError("release-failure-count")
    if not isinstance(report["coverage_complete"], bool) or not isinstance(report["deferred_cases"], list):
        raise ContractError("release-failure-coverage")
    if not isinstance(report["overall_status"], str) or report["overall_status"] not in ("passed", "partial", "failed", "blocked") or not all(isinstance(item, str) and item for item in report["deferred_cases"]):
        raise ContractError("release-failure-status")
    identifiers: list[str] = []
    for case in report["cases"]:
        if not isinstance(case, dict) or set(case) != {"id", "status", "oracle", "evidence"}:
            raise ContractError("release-failure-case")
        if not isinstance(case["id"], str) or case["id"] in identifiers or not isinstance(case["status"], str) or case["status"] not in ("passed", "failed", "blocked"):
            raise ContractError("release-failure-case")
        if not isinstance(case["oracle"], str) or not case["oracle"] or not isinstance(case["evidence"], str) or not case["evidence"]:
            raise ContractError("release-failure-case-evidence")
        identifiers.append(case["id"])
    if set(identifiers) != set(REQUIRED_FAILURE_IDS) or len(identifiers) != len(REQUIRED_FAILURE_IDS):
        raise ContractError("release-failure-required-cases")
    return (
        bool(report["cases"])
        and all(case["status"] == "passed" for case in report["cases"])
        and report["coverage_complete"]
        and report["deferred_cases"] == []
        and report["overall_status"] == "passed"
    )


def _validate_h1_h2(value: Any) -> bool:
    report = _expect_keys(value, {"schema", "rows", "forbidden_semantics", "overall_status"}, "release-h1-h2-schema")
    if report["schema"] != "planning-v1-h1-h2-negative-matrix-v1" or not isinstance(report["rows"], list) or not report["rows"]:
        raise ContractError("release-h1-h2-version")
    statuses = []
    identifiers: list[str] = []
    for row in report["rows"]:
        if not isinstance(row, dict) or set(row) != {"id", "status", "oracle"} or not isinstance(row["status"], str) or row["status"] not in ("passed", "failed", "not_executed"):
            raise ContractError("release-h1-h2-row")
        if not isinstance(row["id"], str) or row["id"] in identifiers or not isinstance(row["oracle"], str) or not row["oracle"]:
            raise ContractError("release-h1-h2-row")
        identifiers.append(row["id"])
        statuses.append(row["status"])
    if set(identifiers) != set(REQUIRED_H1_H2_IDS) or len(identifiers) != len(REQUIRED_H1_H2_IDS):
        raise ContractError("release-h1-h2-required-rows")
    if not isinstance(report["forbidden_semantics"], list) or not report["forbidden_semantics"]:
        raise ContractError("release-h1-h2-forbidden")
    if not isinstance(report["overall_status"], str) or report["overall_status"] not in ("passed", "partial", "failed", "blocked") or not all(isinstance(item, str) and item for item in report["forbidden_semantics"]):
        raise ContractError("release-h1-h2-status")
    return all(status == "passed" for status in statuses) and report["overall_status"] == "passed"


def _validate_responsive(value: Any) -> bool:
    report = _expect_keys(value, {"schema", "rows", "required_viewports", "automated_checks", "manual_checks_deferred", "overall_status"}, "release-responsive-schema")
    if report["schema"] != "planning-v1-prototype-responsive-a11y-matrix-v1" or not isinstance(report["rows"], list) or not report["rows"]:
        raise ContractError("release-responsive-version")
    identifiers: list[str] = []
    for row in report["rows"]:
        if not isinstance(row, dict) or set(row) != {
            "scenario", "status", "viewport", "geometry", "layout", "semantics",
            "undersized_targets", "screenshot",
        }:
            raise ContractError("release-responsive-row")
        if not isinstance(row["scenario"], str) or row["scenario"] in identifiers or not isinstance(row["status"], str) or row["status"] not in ("passed", "failed", "not_executed"):
            raise ContractError("release-responsive-row")
        viewport = _expect_keys(row["viewport"], {"width", "height", "scale", "coarse", "device_scale_factor"}, "release-responsive-viewport")
        geometry = _expect_keys(row["geometry"], {"client_width", "scroll_width"}, "release-responsive-geometry")
        layout = _expect_keys(row["layout"], {"inner_width", "client_width", "visual_viewport_scale", "device_pixel_ratio", "physical_width", "effective_css_width"}, "release-responsive-layout")
        semantics = _expect_keys(row["semantics"], {"landmarks", "h1", "status_regions", "unlabeled_controls"}, "release-responsive-semantics")
        numeric = (viewport["width"], viewport["height"], viewport["scale"], viewport["device_scale_factor"], geometry["client_width"], geometry["scroll_width"], *layout.values(), *semantics.values())
        if any(isinstance(item, bool) or not isinstance(item, (int, float)) or item < 0 for item in numeric) or not isinstance(viewport["coarse"], bool):
            raise ContractError("release-responsive-number")
        if not isinstance(row["undersized_targets"], list) or not isinstance(row["screenshot"], str) or not row["screenshot"]:
            raise ContractError("release-responsive-evidence")
        if row["scenario"].startswith("desktop-1600-"):
            expected_viewport = (1600, 1000, 1, False, 1)
        elif row["scenario"].startswith("desktop-1280-") or row["scenario"] == "authenticated-create-1280":
            expected_viewport = (1280, 900, 1, False, 1)
        elif row["scenario"].startswith("mobile-375-"):
            expected_viewport = (375, 812, 1, True, 2)
        else:
            expected_viewport = (640, 450, 2, False, 2)
        observed_viewport = (viewport["width"], viewport["height"], viewport["scale"], viewport["coarse"], viewport["device_scale_factor"])
        if observed_viewport != expected_viewport:
            raise ContractError("release-responsive-viewport-contract")
        identifiers.append(row["scenario"])
    if tuple(identifiers) != REQUIRED_RESPONSIVE_IDS:
        raise ContractError("release-responsive-required-rows")
    for key in ("required_viewports", "automated_checks", "manual_checks_deferred"):
        if not isinstance(report[key], list) or not all(isinstance(item, str) and item for item in report[key]):
            raise ContractError("release-responsive-list")
    if not isinstance(report["overall_status"], str) or report["overall_status"] not in ("passed", "partial", "failed", "blocked"):
        raise ContractError("release-responsive-status")
    if tuple(report["required_viewports"]) != REQUIRED_VIEWPORTS or tuple(report["automated_checks"]) != REQUIRED_AUTOMATED_CHECKS:
        raise ContractError("release-responsive-contract")
    zoom_rows = [row for row in report["rows"] if row["scenario"].startswith("zoom-200-")]
    if any(
        row["layout"]["inner_width"] > 640
        or row["layout"]["client_width"] > 640
        or row["layout"]["device_pixel_ratio"] != 2
        or row["layout"]["physical_width"] != 1280
        or row["layout"]["effective_css_width"] > 640
        for row in zoom_rows
    ):
        raise ContractError("release-responsive-zoom-reflow")
    return (
        all(row["status"] == "passed" for row in report["rows"])
        and report["manual_checks_deferred"] == []
        and report["overall_status"] == "passed"
    )


def _stage_ready(
    stage: Any,
    index_dir: Path,
    stage_number: int,
    revision: str,
    repo: Path,
    canonical_binding: dict[str, str],
) -> bool:
    if not isinstance(stage, dict):
        raise ContractError("release-evidence-stage")
    status = stage.get("status")
    stale = stage.get("stale")
    if not isinstance(status, str) or status not in ("passed", "failed", "insufficient", "blocked") or not isinstance(stale, bool):
        raise ContractError("release-evidence-stage-status")
    if status != "passed":
        return False
    if set(stage) != {"json_path", "sha256", "gate_version", "stale", "status"}:
        raise ContractError("release-evidence-passed-stage-schema")
    ready = (
        stale is False
        and isinstance(stage.get("json_path"), str)
        and bool(stage["json_path"])
        and isinstance(stage.get("sha256"), str)
        and bool(HEX64.fullmatch(stage["sha256"]))
        and isinstance(stage.get("gate_version"), str)
        and bool(stage["gate_version"])
    )
    if not ready:
        raise ContractError("release-evidence-passed-stage-incomplete")
    stage_path = _resolve_bound_stage_path(
        stage["json_path"],
        repo=repo,
        index_dir=index_dir,
        canonical_path=canonical_binding["evidence_path"],
    )
    if stage["sha256"] != canonical_binding["sha256"] or stage["gate_version"] != canonical_binding["gate_version"]:
        raise ContractError("release-evidence-stage-canonical-binding")
    if sha256_file(stage_path) != stage["sha256"]:
        raise ContractError("release-evidence-stage-digest")
    canonical = validate_stage1(stage_path) if stage_number == 1 else validate_stage2(stage_path)
    if canonical["status"] != stage["status"] or canonical["stale"] != stage["stale"] or canonical["gate_version"] != stage["gate_version"]:
        raise ContractError("release-evidence-stage-binding")
    if canonical["build_revision"] != revision:
        raise ContractError("release-evidence-stage-stale-build")
    return True


def _approval_ready(approval: Any, stage: dict[str, Any]) -> bool:
    if isinstance(approval, str):
        if approval not in ("pending", "rejected"):
            raise ContractError("release-evidence-approval-status")
        return False
    if not isinstance(approval, dict):
        raise ContractError("release-evidence-approval")
    required = {"status", "path", "sha256", "gate_version"}
    if set(approval) != required or not isinstance(approval["status"], str) or approval["status"] not in ("approved", "pending", "rejected"):
        raise ContractError("release-evidence-approval-schema")
    if approval["status"] != "approved":
        return False
    ready = (
        isinstance(approval["path"], str)
        and bool(approval["path"])
        and approval["path"] == stage.get("json_path")
        and approval["sha256"] == stage.get("sha256")
        and approval["gate_version"] == stage.get("gate_version")
    )
    if not ready:
        raise ContractError("release-evidence-approval-binding")
    return True


def _validate_evidence_index(
    value: Any,
    index_path: Path,
    revision: str,
    repo: Path,
    canonical_bindings: dict[str, dict[str, str]],
) -> bool:
    report = _expect_keys(value, {
        "schema", "window_refs", "stage_1", "stage_2", "sample_decision_digest", "cohort_comparison",
        "generated_at", "validator_version", "approval", "overall_status",
    }, "release-evidence-schema")
    if report["schema"] != "planning-v1-evidence-index-v1" or not isinstance(report["approval"], dict):
        raise ContractError("release-evidence-version")
    if not isinstance(report["overall_status"], str) or report["overall_status"] not in ("passed", "failed", "stale", "blocked"):
        raise ContractError("release-evidence-status")
    if not isinstance(report["window_refs"], list) or len(report["window_refs"]) != 2 or not all(isinstance(item, str) and item for item in report["window_refs"]):
        raise ContractError("release-evidence-window-refs")
    if not isinstance(report["sample_decision_digest"], str) or not HEX64.fullmatch(report["sample_decision_digest"]):
        raise ContractError("release-evidence-sample-decision-digest")
    if not isinstance(report["generated_at"], str) or not report["generated_at"] or report["validator_version"] != "planninghardening-evidence-v1":
        raise ContractError("release-evidence-generator")
    stage_1_ready = _stage_ready(
        report["stage_1"], index_path.parent, 1, revision, repo,
        canonical_bindings["stage-1-evidence-go"],
    )
    stage_2_ready = _stage_ready(
        report["stage_2"], index_path.parent, 2, revision, repo,
        canonical_bindings["stage-2-evidence-go"],
    )
    if stage_2_ready:
        stage2_path = _resolve_bound_stage_path(
            report["stage_2"]["json_path"],
            repo=repo,
            index_dir=index_path.parent,
            canonical_path=canonical_bindings["stage-2-evidence-go"]["evidence_path"],
        )
        stage2_report = validate_stage2(stage2_path)
        stage2_keys = {
            decision["shoot_key"]
            for decision in stage2_report["sample_decisions"]
            if decision["disposition"] == "include"
        }
        comparable_keys = set(stage2_report["g3"]["comparable_subset"]["shoot_keys"])
        validate_cohort_comparison(
            report["cohort_comparison"],
            comparable_keys=comparable_keys,
            stage2_keys=stage2_keys,
            require_shared=True,
        )
    approval = report["approval"]
    if set(approval) != {"stage-1-evidence-go", "stage-2-evidence-go"}:
        raise ContractError("release-evidence-approval-keys")
    approval_1_ready = _approval_ready(approval["stage-1-evidence-go"], report["stage_1"])
    approval_2_ready = _approval_ready(approval["stage-2-evidence-go"], report["stage_2"])
    return stage_1_ready and stage_2_ready and approval_1_ready and approval_2_ready and report["overall_status"] == "passed"


def _utc_timestamp(value: Any, key: str) -> datetime:
    if not isinstance(value, str) or not value.endswith("Z"):
        raise ContractError(key)
    try:
        parsed = datetime.fromisoformat(value[:-1] + "+00:00")
    except ValueError as error:
        raise ContractError(key) from error
    if parsed.tzinfo != timezone.utc:
        raise ContractError(key)
    return parsed


def _planningctl_readiness_digest(report: dict[str, Any]) -> str:
    unsigned = {
        "frame_version": report["frame_version"],
        "environment": report["environment"],
        "deployment": report["deployment"],
        "schema_hash": report["schema_hash"],
        "content_hash": report["content_hash"],
        "generated_at": report["generated_at"],
        "expires_at": report["expires_at"],
        "current": report["current"],
        "current_revision": report["current_revision"],
        "target": report["target"],
        "live_builds": report["live_builds"],
        "target_wiring_ready": report["target_wiring_ready"],
        "control_plane_provenance": report["control_plane_provenance"],
        "digest": "",
    }
    encoded = json.dumps(unsigned, ensure_ascii=False, separators=(",", ":")).encode("utf-8")
    # encoding/json escapes these code points even when the rest of UTF-8 is emitted directly.
    for source, replacement in (
        (b"<", b"\\u003c"),
        (b">", b"\\u003e"),
        (b"&", b"\\u0026"),
        ("\u2028".encode(), b"\\u2028"),
        ("\u2029".encode(), b"\\u2029"),
    ):
        encoded = encoded.replace(source, replacement)
    return hashlib.sha256(encoded).hexdigest()


def _validate_planningctl_readiness(
    value: Any,
    *,
    archive: dict[str, Any],
    inventory: dict[str, Any],
    inventory_sha256: str,
    additive_schema: dict[str, Any],
) -> dict[str, Any]:
    fields = {
        "frame_version", "environment", "deployment", "schema_hash", "content_hash",
        "generated_at", "expires_at", "current", "current_revision", "target",
        "live_builds", "target_wiring_ready", "control_plane_provenance", "digest",
    }
    report = _expect_keys(value, fields, "release-planningctl-readiness")
    _utc_timestamp(report["generated_at"], "release-planningctl-readiness-time")
    _utc_timestamp(report["expires_at"], "release-planningctl-readiness-time")
    expected_builds = sorted(build["build_ref"] for build in inventory["live_builds"])
    if (
        isinstance(report["frame_version"], bool)
        or not isinstance(report["frame_version"], int)
        or report["frame_version"] != 1
        or report["environment"] != inventory["environment_id"]
        or report["deployment"] != inventory["deployment_id"]
        or report["schema_hash"] != additive_schema["schema_sha256"]
        or report["content_hash"] != inventory_sha256
        or report["generated_at"] != inventory["generated_at"]
        or report["expires_at"] != inventory["expires_at"]
        or report["current"] != archive["current_marker"]
        or isinstance(report["current_revision"], bool)
        or not isinstance(report["current_revision"], int)
        or report["current_revision"] != archive["expected_revision"]
        or report["target"] != archive["target_marker"]
        or report["live_builds"] != expected_builds
        or report["target_wiring_ready"] is not True
        or report["control_plane_provenance"] != inventory["control_plane_provenance"]
        or not HEX64.fullmatch(str(report["digest"]))
        or report["digest"] != _planningctl_readiness_digest(report)
    ):
        raise ContractError("release-planningctl-readiness-binding")
    return report


def _validate_check_artifact(
    check_id: str,
    value: Any,
    *,
    migration_head: str,
    archive: dict[str, Any],
    rollback: dict[str, Any],
    rehearsal: dict[str, Any],
    inventory_sha256: str,
    now: datetime,
) -> dict[str, Any]:
    if check_id == "additive_schema_available":
        report = _expect_keys(value, {"schema", "status", "migration_head", "schema_sha256", "down_migration_forbidden"}, "release-additive-schema")
        if report != {
            "schema": "planning-v1-additive-schema-readiness-v1",
            "status": "passed",
            "migration_head": migration_head,
            "schema_sha256": report["schema_sha256"],
            "down_migration_forbidden": True,
        } or not HEX64.fullmatch(str(report["schema_sha256"])):
            raise ContractError("release-additive-schema-status")
        return report

    if check_id == "candidate_rollback_marker_readers":
        report = _expect_keys(value, {"schema", "status", "candidate_image_digest", "rollback_image_digest", "understood_markers", "startup_fail_closed"}, "release-marker-readers")
        if (
            report["schema"] != "planning-v1-marker-reader-readiness-v1"
            or report["status"] != "passed"
            or not IMAGE_DIGEST.fullmatch(str(report["candidate_image_digest"]))
            or not IMAGE_DIGEST.fullmatch(str(report["rollback_image_digest"]))
            or not isinstance(report["understood_markers"], list)
            or tuple(report["understood_markers"]) != MARKERS
            or report["startup_fail_closed"] is not True
            or report["rollback_image_digest"] != rollback["app_image_digest"]
        ):
            raise ContractError("release-marker-readers-status")
        return report

    if check_id == "old_processes_exited":
        report = _expect_keys(value, {"schema", "status", "observed_at", "processes"}, "release-old-processes")
        if report["schema"] != "planning-v1-old-processes-exited-v1" or report["status"] != "passed" or not isinstance(report["processes"], list) or not report["processes"]:
            raise ContractError("release-old-processes-status")
        _utc_timestamp(report["observed_at"], "release-old-processes-time")
        for process in report["processes"]:
            row = _expect_keys(process, {"process_ref", "image_digest", "marker_capability", "status"}, "release-old-process-row")
            if not isinstance(row["process_ref"], str) or not row["process_ref"] or not IMAGE_DIGEST.fullmatch(str(row["image_digest"])) or row["marker_capability"] not in MARKERS or row["status"] != "exited":
                raise ContractError("release-old-process-row-status")
        return report

    if check_id == "trusted_readiness_inventory_fresh":
        fields = {"schema", "status", "environment_id", "deployment_id", "generated_at", "expires_at", "current_marker", "target_marker", "live_builds", "wiring_evidence_sha256", "control_plane_provenance", "permission_assumptions"}
        report = _expect_keys(value, fields, "release-trusted-inventory")
        generated = _utc_timestamp(report["generated_at"], "release-trusted-inventory-time")
        expires = _utc_timestamp(report["expires_at"], "release-trusted-inventory-time")
        if (
            report["schema"] != "planning-v1-trusted-readiness-inventory-v1"
            or report["status"] != "passed"
            or generated > now
            or expires <= now
            or report["current_marker"] != archive["current_marker"]
            or report["target_marker"] != archive["target_marker"]
            or not HEX64.fullmatch(str(report["wiring_evidence_sha256"]))
            or not isinstance(report["environment_id"], str)
            or not report["environment_id"]
            or not isinstance(report["deployment_id"], str)
            or not report["deployment_id"]
            or not isinstance(report["control_plane_provenance"], str)
            or not report["control_plane_provenance"]
            or not isinstance(report["permission_assumptions"], list)
            or not report["permission_assumptions"]
            or not all(isinstance(item, str) and item for item in report["permission_assumptions"])
        ):
            raise ContractError("release-trusted-inventory-status")
        if not isinstance(report["live_builds"], list) or not report["live_builds"]:
            raise ContractError("release-trusted-inventory-builds")
        build_refs: set[str] = set()
        for build in report["live_builds"]:
            row = _expect_keys(build, {"build_ref", "image_digest", "marker_capability", "wiring_status"}, "release-trusted-inventory-build")
            if (
                not isinstance(row["build_ref"], str)
                or not row["build_ref"]
                or row["build_ref"] in build_refs
                or not IMAGE_DIGEST.fullmatch(str(row["image_digest"]))
                or row["marker_capability"] != archive["target_marker"]
                or row["wiring_status"] != "passed"
            ):
                raise ContractError("release-trusted-inventory-build-status")
            build_refs.add(row["build_ref"])
        return report

    if check_id == "archive_adjacent_cas_readback":
        fields = {
            "schema", "status", "expected_revision", "observed_revision", "previous_marker",
            "current_marker", "inventory_sha256", "readiness_digest", "readback_at",
        }
        report = _expect_keys(value, fields, "release-archive-readback")
        _utc_timestamp(report["readback_at"], "release-archive-readback-time")
        if (
            report["schema"] != "planning-v1-archive-cas-readback-v1"
            or report["status"] != "passed"
            or report["expected_revision"] != archive["expected_revision"]
            or report["observed_revision"] != archive["expected_revision"] + 1
            or report["previous_marker"] != archive["current_marker"]
            or report["current_marker"] != archive["target_marker"]
            or report["inventory_sha256"] != inventory_sha256
            or not HEX64.fullmatch(str(report["readiness_digest"]))
        ):
            raise ContractError("release-archive-readback-status")
        return report

    if check_id == "share_reminder_writers_enabled":
        fields = {"schema", "status", "active_marker", "share_writer_enabled", "reminder_writer_enabled", "archive_participant_enabled", "writer_image_digest"}
        report = _expect_keys(value, fields, "release-writers")
        if (
            report["schema"] != "planning-v1-share-reminder-writers-v1"
            or report["status"] != "passed"
            or report["active_marker"] != archive["target_marker"]
            or report["share_writer_enabled"] is not True
            or report["reminder_writer_enabled"] is not True
            or report["archive_participant_enabled"] is not True
            or not IMAGE_DIGEST.fullmatch(str(report["writer_image_digest"]))
        ):
            raise ContractError("release-writers-status")
        return report

    if check_id == "schema_v2_restore_pair_retained":
        fields = {"schema", "status", "writer_image_digest", "restore_tool_digest", "writer_supports_schema_v2", "reader_supports_schema_v1", "reader_supports_schema_v2", "retained_until"}
        report = _expect_keys(value, fields, "release-schema-v2-retention")
        retained_until = _utc_timestamp(report["retained_until"], "release-schema-v2-retention-time")
        if (
            report["schema"] != "planning-v1-schema-v2-retention-v1"
            or report["status"] != "passed"
            or not IMAGE_DIGEST.fullmatch(str(report["writer_image_digest"]))
            or report["restore_tool_digest"] != rollback["restore_tool_digest"]
            or report["restore_tool_digest"] != rehearsal["restore_tool_digest"]
            or report["writer_supports_schema_v2"] is not True
            or report["reader_supports_schema_v1"] is not True
            or report["reader_supports_schema_v2"] is not True
            or retained_until <= now
        ):
            raise ContractError("release-schema-v2-retention-status")
        return report

    raise ContractError("release-readiness-unknown-check")


def _verify_readiness_with_bindings(
    path: Path,
    repo_root: Path,
    canonical_bindings: dict[str, dict[str, str]],
) -> dict[str, Any]:
    document = load_json(path)
    required = {
        "schema", "build_revision", "openapi_sha256", "migration_head", "dependency_conformance_sha256",
        "e2e_result_sha256", "failure_matrix_sha256", "h1_h2_result_sha256", "prototype_a11y_result_sha256",
        "planning_evidence_index_sha256", "backup_rehearsal_sha256", "archive_readiness", "rollback", "required_checks", "status",
    }
    if not isinstance(document, dict) or set(document) != required or document["schema"] != "planning-v1-release-readiness-v1":
        raise ContractError("release-readiness-schema")
    if not isinstance(document["status"], str) or document["status"] not in ("passed", "failed", "stale", "blocked"):
        raise ContractError("release-readiness-status")

    revision = _repo_revision(repo_root)
    openapi_sha256 = sha256_file(repo_root / "api/openapi.yaml")
    migration_head = _migration_head(repo_root)
    if document["build_revision"] != revision or document["openapi_sha256"] != openapi_sha256 or document["migration_head"] != migration_head:
        raise ContractError("release-readiness-stale-build")

    artifacts: dict[str, Any] = {}
    for digest_key, filename in ARTIFACTS.items():
        expected = document[digest_key]
        sibling = path.parent / filename
        if not isinstance(expected, str) or not HEX64.fullmatch(expected) or sha256_file(sibling) != expected:
            raise ContractError("release-readiness-artifact-digest")
        artifacts[digest_key] = load_json(sibling)

    artifact_ready = all((
        _validate_conformance(
            artifacts["dependency_conformance_sha256"], revision, openapi_sha256,
            sha256_file(repo_root / ".codestable/roadmap/creative-shoot-planning/creative-shoot-planning-roadmap.md"),
        ),
        _validate_e2e(artifacts["e2e_result_sha256"]),
        _validate_failure_matrix(artifacts["failure_matrix_sha256"]),
        _validate_h1_h2(artifacts["h1_h2_result_sha256"]),
        _validate_responsive(artifacts["prototype_a11y_result_sha256"]),
        _validate_evidence_index(
            artifacts["planning_evidence_index_sha256"],
            path.parent / ARTIFACTS["planning_evidence_index_sha256"],
            revision,
            repo_root,
            canonical_bindings,
        ),
    ))
    rehearsal = validate_rehearsal(artifacts["backup_rehearsal_sha256"])
    _validate_rehearsal_package(path.parent, rehearsal)

    archive = _expect_keys(document["archive_readiness"], {
        "current_marker", "target_marker", "expected_revision", "trusted_inventory_sha256",
        "planningctl_readiness_sha256", "planningctl_readback_sha256",
    }, "release-archive-readiness")
    if archive["current_marker"] not in MARKERS or archive["target_marker"] not in MARKERS or MARKERS.index(archive["target_marker"]) != MARKERS.index(archive["current_marker"]) + 1:
        raise ContractError("release-archive-marker")
    if isinstance(archive["expected_revision"], bool) or not isinstance(archive["expected_revision"], int) or archive["expected_revision"] < 1:
        raise ContractError("release-archive-revision")
    if (
        not HEX64.fullmatch(str(archive["trusted_inventory_sha256"]))
        or not HEX64.fullmatch(str(archive["planningctl_readiness_sha256"]))
        or not HEX64.fullmatch(str(archive["planningctl_readback_sha256"]))
    ):
        raise ContractError("release-archive-digest")

    rollback = _expect_keys(document["rollback"], {"app_image_digest", "understands_active_marker", "restore_tool_digest", "reads_schema_v2", "schema_down_forbidden"}, "release-rollback-schema")
    for key in ("understands_active_marker", "reads_schema_v2", "schema_down_forbidden"):
        if not isinstance(rollback[key], bool):
            raise ContractError("release-rollback-boolean")
    if not IMAGE_DIGEST.fullmatch(str(rollback["app_image_digest"])) or not IMAGE_DIGEST.fullmatch(str(rollback["restore_tool_digest"])):
        raise ContractError("release-rollback-digest")
    if rollback["restore_tool_digest"] != rehearsal["restore_tool_digest"]:
        raise ContractError("release-rollback-tool-not-rehearsed")

    checks = document["required_checks"]
    if not isinstance(checks, list):
        raise ContractError("release-readiness-checks")
    statuses = []
    check_ids = []
    check_documents: dict[str, dict[str, Any]] = {}
    inventory_path = path.parent / CHECK_ARTIFACTS["trusted_readiness_inventory_fresh"]
    inventory_sha256 = sha256_file(inventory_path)
    now = datetime.now(timezone.utc)
    for check in checks:
        if not isinstance(check, dict) or set(check) != {"id", "status", "sha256"} or not isinstance(check["id"], str) or not isinstance(check["status"], str) or check["status"] not in ("passed", "failed", "stale", "blocked"):
            raise ContractError("release-readiness-check-row")
        if check["id"] not in CHECK_ARTIFACTS or not isinstance(check["sha256"], str) or not HEX64.fullmatch(check["sha256"]):
            raise ContractError("release-readiness-check-evidence")
        sibling = path.parent / CHECK_ARTIFACTS[check["id"]]
        if sha256_file(sibling) != check["sha256"]:
            raise ContractError("release-readiness-check-digest")
        check_document = _validate_check_artifact(
            check["id"], load_json(sibling), migration_head=migration_head, archive=archive,
            rollback=rollback, rehearsal=rehearsal, inventory_sha256=inventory_sha256, now=now,
        )
        if check_document["status"] != check["status"]:
            raise ContractError("release-readiness-check-status-binding")
        check_documents[check["id"]] = check_document
        statuses.append(check["status"])
        check_ids.append(check["id"])
    if tuple(check_ids) != REQUIRED_CHECK_IDS:
        raise ContractError("release-readiness-check-order")
    if archive["trusted_inventory_sha256"] != inventory_sha256:
        raise ContractError("release-archive-inventory-binding")
    readback_path = path.parent / CHECK_ARTIFACTS["archive_adjacent_cas_readback"]
    if archive["planningctl_readback_sha256"] != sha256_file(readback_path):
        raise ContractError("release-archive-readback-binding")
    planningctl_readiness_path = path.parent / PLANNINGCTL_READINESS_ARTIFACT
    if archive["planningctl_readiness_sha256"] != sha256_file(planningctl_readiness_path):
        raise ContractError("release-planningctl-readiness-digest")
    planningctl_readiness = _validate_planningctl_readiness(
        load_json(planningctl_readiness_path),
        archive=archive,
        inventory=check_documents["trusted_readiness_inventory_fresh"],
        inventory_sha256=inventory_sha256,
        additive_schema=check_documents["additive_schema_available"],
    )
    if check_documents["archive_adjacent_cas_readback"]["readiness_digest"] != planningctl_readiness["digest"]:
        raise ContractError("release-archive-readiness-readback-binding")
    marker_readers = check_documents["candidate_rollback_marker_readers"]
    writers = check_documents["share_reminder_writers_enabled"]
    retention = check_documents["schema_v2_restore_pair_retained"]
    if writers["writer_image_digest"] != marker_readers["candidate_image_digest"] or retention["writer_image_digest"] != marker_readers["candidate_image_digest"]:
        raise ContractError("release-candidate-image-binding")

    prerequisites_pass = (
        artifact_ready
        and all(status == "passed" for status in statuses)
        and rollback["understands_active_marker"]
        and rollback["reads_schema_v2"]
        and rollback["schema_down_forbidden"]
    )
    if document["status"] == "passed" and not prerequisites_pass:
        raise ContractError("release-fake-pass")
    return {**document, "derived_status": "passed" if document["status"] == "passed" else document["status"]}


def verify_readiness(path: Path, repo: Path | None = None) -> dict[str, Any]:
    repo_root = (repo or Path(__file__).resolve().parents[3]).resolve()
    canonical_bindings = _canonical_stage_approvals(repo_root)
    return _verify_readiness_with_bindings(path, repo_root, canonical_bindings)


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--evidence", type=Path, required=True)
    parser.add_argument("--output", type=Path)
    args = parser.parse_args(argv)
    try:
        result = verify_readiness(args.evidence)
    except (ContractError, OSError, ValueError, TypeError, KeyError):
        return 9
    encoded = canonical_json(result)
    if args.output:
        args.output.write_bytes(encoded)
    else:
        print(encoded.decode(), end="")
    return 0 if result["derived_status"] == "passed" else 9


if __name__ == "__main__":
    raise SystemExit(main())
