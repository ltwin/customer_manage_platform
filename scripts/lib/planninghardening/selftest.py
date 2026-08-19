#!/usr/bin/env python3
"""Conformance, evidence, rehearsal and release-readiness self-test."""

from __future__ import annotations

import copy
import hashlib
import json
import shutil
import subprocess
import tempfile
from datetime import datetime, timedelta, timezone
from pathlib import Path
from typing import Callable

from planningbackup.package import ContractError, load_json, sha256_file, validate_package, write_checksums, write_json
from planningbackup.preflight import _package_sha256
from planningbackup.selftest import _legacy_selftest, _write_v2

from .conformance import build_conformance
from .evidence import (
    INTERVAL_CONFIDENCE,
    INTERVAL_METHOD,
    _wilson_interval,
    build_evidence_index,
    validate_sample_decisions,
    validate_stage1,
    validate_stage2,
)
from .release import (
    ARTIFACTS,
    CHECK_ARTIFACTS,
    REQUIRED_AUTOMATED_CHECKS,
    REQUIRED_CHECK_IDS,
    REQUIRED_CONFORMANCE_IDS,
    REQUIRED_E2E_IDS,
    REQUIRED_FAILURE_IDS,
    REQUIRED_H1_H2_IDS,
    REQUIRED_RESPONSIVE_IDS,
    REQUIRED_VIEWPORTS,
    REHEARSAL_PACKAGE_DIRECTORY,
    PLANNINGCTL_READINESS_ARTIFACT,
    _migration_head,
    _verify_readiness_with_bindings,
    verify_readiness,
)


APP_IMAGE_DIGEST = "sha256:" + "2" * 64
POSTGRES_IMAGE_DIGEST = "sha256:" + "3" * 64
RESTORE_TOOL_DIGEST = "sha256:" + "4" * 64


def _stage1(path: Path, build_revision: str = "build-1") -> None:
    write_json(path, {
        "gate_version": "planning-evidence-stage1-v1",
        "status": "passed",
        "stale": False,
        "window_id": "window-1",
        "build_revision": build_revision,
        "sample_decisions": [{"plan_id": f"plan-{index}", "disposition": "include", "owner_review_ref": "synthetic-owner-review"} for index in range(1, 6)],
        "cohort_complete": True,
        "g1": {"distinct_live_plan_count": 3, "eligible_plan_count": 5, "required_sample_count": 5, "threshold": 3, "live_plan_ids": ["plan-1", "plan-2", "plan-3"], "threshold_met": True},
        "g2": {"successful_plan_count": 4, "required_sample_count": 5, "active_seconds_samples": [{"plan_id": f"plan-{index}", "active_seconds": value} for index, value in ((1, 240), (2, 480), (3, 600), (4, 900))], "median_active_seconds": 540, "median_threshold_seconds": 600, "median_threshold_met": True, "abandoned_plan_count": 1, "terminal_plan_count": 5, "abandon_ratio": 0.2},
        "exclusions": [],
        "rule_versions": {"evidence_projector": "planning-evidence-stage1-v1", "active_time": "plan-build-active-idle-cap-v1", "run_capture_mode": "run-mode-capture-mode-v1", "idle_rules": [1]},
        "projection_sha256": "a" * 64,
    })


def _interval(successes: int, total: int) -> dict[str, object] | None:
    bounds = _wilson_interval(successes, total)
    if bounds is None:
        return None
    return {
        "method": INTERVAL_METHOD,
        "confidence_level": INTERVAL_CONFIDENCE,
        "lower": bounds[0],
        "upper": bounds[1],
    }


def _stage2(path: Path, build_revision: str) -> None:
    write_json(path, {
        "gate_version": "planning-evidence-stage2-v1",
        "status": "passed",
        "stale": False,
        "window_id": "window-2",
        "build_revision": build_revision,
        "sample_decisions": [{"shoot_key": f"shoot-{index}", "disposition": "include", "owner_review_ref": "synthetic-owner-review"} for index in range(1, 6)],
        "cohort_complete": True,
        "g3": {
            "eligible_shared_shoot_count": 5,
            "required_sample_count": 5,
            "interacted_shared_shoot_count": 2,
            "interaction_rate": 0.4,
            "interaction_threshold": 0.4,
            "interaction_threshold_met": True,
            "interaction_rate_interval": _interval(2, 5),
            "baseline_preparation_missing_shot_count": 4,
            "baseline_finalized_shot_count": 10,
            "baseline_preparation_missing_rate": 0.4,
            "baseline_preparation_missing_rate_interval": _interval(4, 10),
            "shared_preparation_missing_shot_count": 2,
            "shared_finalized_shot_count": 10,
            "shared_preparation_missing_rate": 0.2,
            "shared_preparation_missing_rate_interval": _interval(2, 10),
            "preparation_missing_rate_decreased": True,
            "preparation_missing_absolute_rate_decrease": 0.2,
            "comparable_subset": {
                "shoot_keys": ["shoot-1", "shoot-2", "shoot-3"],
                "baseline_preparation_missing_shot_count": 3,
                "baseline_finalized_shot_count": 8,
                "baseline_preparation_missing_rate": 0.375,
                "baseline_preparation_missing_rate_interval": _interval(3, 8),
                "shared_preparation_missing_shot_count": 1,
                "shared_finalized_shot_count": 8,
                "shared_preparation_missing_rate": 0.125,
                "shared_preparation_missing_rate_interval": _interval(1, 8),
                "absolute_rate_decrease": 0.25,
            },
            "limitations": ["five-shoot cohort; interval remains wide"],
        },
        "exclusions": [],
        "rule_versions": {
            "evidence_projector": "planning-evidence-stage2-v1",
            "interaction_deduplication": "shared-shoot-interaction-v1",
            "completion_revision": "shoot-finalization-v1",
            "small_sample_interval": INTERVAL_METHOD,
        },
        "projection_sha256": "b" * 64,
    })


def _sample_decisions(path: Path) -> None:
    write_json(path, {
        "schema": "planning-observation-sample-decision-v1",
        "decision_id": "decision-stage-2-window-2",
        "decision_revision": 1,
        "cohort": "stage_2_g3",
        "decisions": [{
            "candidate_ref": "candidate-shoot-1",
            "canonical_shoot_key": "shoot-1",
            "decision": "eligible",
            "reason": "eligible",
            "source_fact_refs": [{"kind": "plan_finalization", "id": "finalization-1", "revision": 1, "sha256": "c" * 64}],
            "decided_at": "2026-08-18T00:00:00Z",
            "reviewer_ref": "owner-review-1",
        }],
    })


def _revision(repo: Path) -> str:
    return subprocess.run(["git", "rev-parse", "HEAD"], cwd=repo, text=True, stdout=subprocess.PIPE, check=True).stdout.strip()


def _write_rehearsal_package(root: Path) -> str:
    package = root / REHEARSAL_PACKAGE_DIRECTORY
    legacy = root / ".planning-restore-package-v1-fixture"
    for path in (package, legacy):
        if path.exists():
            shutil.rmtree(path)
    _legacy_selftest().write_package(legacy)
    package.mkdir(mode=0o700)
    try:
        _write_v2(package, legacy)
    finally:
        shutil.rmtree(legacy)
    metadata = load_json(package / "metadata.json")
    metadata["images"] = {
        "app": APP_IMAGE_DIGEST,
        "postgres": POSTGRES_IMAGE_DIGEST,
        "restore_tool": RESTORE_TOOL_DIGEST,
    }
    write_json(package / "metadata.json", metadata)
    write_checksums(package)
    validate_package(package)
    return _package_sha256(package)


def _rehearsal(package_sha256: str) -> dict[str, object]:
    times = [f"2026-08-18T00:0{index}:00Z" for index in range(7)]
    stages = []
    for index, stage in enumerate(("preflight", "stop_writes", "replace", "migrate", "validate", "reopen")):
        stages.append({"stage": stage, "status": "passed", "started_at": times[index], "finished_at": times[index + 1]})
    return {
        "schema": "planning-restore-rehearsal-v1",
        "rehearsal_id": "production-shaped-1",
        "environment_class": "production_shaped_non_production",
        "deployment_identity_sha256": "1" * 64,
        "source_image_digests": {"app": APP_IMAGE_DIGEST, "postgres": POSTGRES_IMAGE_DIGEST},
        "restore_tool_digest": RESTORE_TOOL_DIGEST,
        "package_sha256": package_sha256,
        "stage_results": stages,
        "database_oracle": {"status": "passed", "sha256": "6" * 64},
        "avatar_oracle": {"status": "passed", "sha256": "7" * 64},
        "planning_media_oracle": {"status": "passed", "sha256": "8" * 64},
        "failure_cases": [
            {"id": "preflight-reject", "kind": "preflight_rejection", "status": "passed", "terminal_state": "rejected_before_mutation"},
            {"id": "destructive-stop", "kind": "destructive_failure_stop", "status": "passed", "terminal_state": "failed_restore_stopped"},
        ],
        "started_at": times[0],
        "finished_at": times[-1],
        "status": "passed",
    }


def _evidence_index(root: Path) -> dict[str, object]:
    stage_1 = {"json_path": "stage-1.json", "sha256": sha256_file(root / "stage-1.json"), "gate_version": "planning-evidence-stage1-v1", "stale": False, "status": "passed"}
    stage_2 = {"json_path": "stage-2.json", "sha256": sha256_file(root / "stage-2.json"), "gate_version": "planning-evidence-stage2-v1", "stale": False, "status": "passed"}
    return {
        "schema": "planning-v1-evidence-index-v1",
        "window_refs": ["window-1", "window-2"],
        "stage_1": stage_1,
        "stage_2": stage_2,
        "sample_decision_digest": "b" * 64,
        "cohort_comparison": {
            "shared_keys": ["shoot-1", "shoot-2", "shoot-3"],
            "stage_1_only": ["shoot-baseline-only"],
            "stage_2_only": ["shoot-4", "shoot-5"],
            "limitations": ["five-shoot cohorts; Wilson intervals remain wide"],
        },
        "generated_at": "2026-08-18T00:00:00Z",
        "validator_version": "planninghardening-evidence-v1",
        "approval": {
            "stage-1-evidence-go": {"status": "approved", "path": "stage-1.json", "sha256": stage_1["sha256"], "gate_version": stage_1["gate_version"]},
            "stage-2-evidence-go": {"status": "approved", "path": "stage-2.json", "sha256": stage_2["sha256"], "gate_version": stage_2["gate_version"]},
        },
        "overall_status": "passed",
    }


def _fixture_stage_bindings(root: Path) -> dict[str, dict[str, str]]:
    return {
        "stage-1-evidence-go": {
            "evidence_path": str((root / "stage-1.json").resolve(strict=True)),
            "sha256": sha256_file(root / "stage-1.json"),
            "gate_version": "planning-evidence-stage1-v1",
        },
        "stage-2-evidence-go": {
            "evidence_path": str((root / "stage-2.json").resolve(strict=True)),
            "sha256": sha256_file(root / "stage-2.json"),
            "gate_version": "planning-evidence-stage2-v1",
        },
    }


def _planningctl_fixture_digest(document: dict[str, object]) -> str:
    field_order = (
        "frame_version", "environment", "deployment", "schema_hash", "content_hash",
        "generated_at", "expires_at", "current", "current_revision", "target", "live_builds",
        "target_wiring_ready", "control_plane_provenance", "digest",
    )
    unsigned = {field: "" if field == "digest" else document[field] for field in field_order}
    return hashlib.sha256(
        json.dumps(unsigned, ensure_ascii=False, separators=(",", ":")).encode("utf-8")
    ).hexdigest()


def _write_artifacts(root: Path, repo: Path) -> None:
    revision = _revision(repo)
    _stage1(root / "stage-1.json", revision)
    _stage2(root / "stage-2.json", revision)
    package_sha256 = _write_rehearsal_package(root)

    responsive_rows = []
    for scenario in REQUIRED_RESPONSIVE_IDS:
        zoom = scenario.startswith("zoom-200-")
        if zoom:
            width, height = 640, 450
        elif scenario.startswith("desktop-1600-"):
            width, height = 1600, 1000
        elif scenario.startswith("mobile-375-"):
            width, height = 375, 812
        else:
            width, height = 1280, 900
        device_scale_factor = 2 if zoom or scenario.startswith("mobile-375-") else 1
        physical_width = width * device_scale_factor
        responsive_rows.append({
            "scenario": scenario,
            "status": "passed",
            "viewport": {"width": width, "height": height, "scale": 2 if zoom else 1, "coarse": scenario.startswith("mobile-375-"), "device_scale_factor": device_scale_factor},
            "geometry": {"client_width": width, "scroll_width": width},
            "layout": {"inner_width": width, "client_width": width, "visual_viewport_scale": 1, "device_pixel_ratio": device_scale_factor, "physical_width": physical_width, "effective_css_width": width},
            "semantics": {"landmarks": 2, "h1": 1, "status_regions": 1, "unlabeled_controls": 0},
            "undersized_targets": [],
            "screenshot": f"{scenario}.png",
        })
    documents = {
        "planning_v1_dependency_conformance.json": build_conformance(repo),
        "planning_v1_e2e_results.json": {
            "schema": "planning-v1-e2e-results-v1",
            "base_origin": "https://planning.invalid",
            "authenticated": True,
            "plan_ref": "sha256-" + "e" * 64,
            "credential_material_persisted": False,
            "trace_material_persisted": False,
            "scenarios": [{"id": scenario_id, "status": "passed", "assertions": [f"{scenario_id} passed"]} for scenario_id in REQUIRED_E2E_IDS],
            "overall_status": "passed",
            "failure": None,
        },
        "planning_v1_failure_matrix.json": {
            "schema": "planning-v1-failure-matrix-v1",
            "cases": [{"id": case_id, "status": "passed", "oracle": f"{case_id} oracle", "evidence": f"{case_id} evidence"} for case_id in REQUIRED_FAILURE_IDS],
            "executed_case_count": len(REQUIRED_FAILURE_IDS),
            "coverage_complete": True,
            "deferred_cases": [],
            "overall_status": "passed",
        },
        "planning_v1_h1_h2_negative_matrix.json": {
            "schema": "planning-v1-h1-h2-negative-matrix-v1",
            "rows": [{"id": row_id, "status": "passed", "oracle": f"{row_id} oracle"} for row_id in REQUIRED_H1_H2_IDS],
            "forbidden_semantics": ["private business facts"],
            "overall_status": "passed",
        },
        "planning_v1_prototype_responsive_a11y_matrix.json": {
            "schema": "planning-v1-prototype-responsive-a11y-matrix-v1",
            "rows": responsive_rows,
            "required_viewports": list(REQUIRED_VIEWPORTS),
            "automated_checks": list(REQUIRED_AUTOMATED_CHECKS),
            "manual_checks_deferred": [],
            "overall_status": "passed",
        },
        "planning_v1_evidence_index.json": _evidence_index(root),
        "planning_restore_rehearsal.json": _rehearsal(package_sha256),
    }
    for filename, document in documents.items():
        write_json(root / filename, document)


def _write_check_artifacts(root: Path, repo: Path) -> None:
    now = datetime.now(timezone.utc)
    generated = (now - timedelta(minutes=1)).isoformat(timespec="seconds").replace("+00:00", "Z")
    expires = (now + timedelta(hours=1)).isoformat(timespec="seconds").replace("+00:00", "Z")
    retained = (now + timedelta(days=30)).isoformat(timespec="seconds").replace("+00:00", "Z")
    candidate_digest = "sha256:" + "9" * 64
    rollback_digest = "sha256:" + "1" * 64
    tool_digest = RESTORE_TOOL_DIGEST
    documents = {
        "additive_schema_available": {"schema": "planning-v1-additive-schema-readiness-v1", "status": "passed", "migration_head": _migration_head(repo), "schema_sha256": "1" * 64, "down_migration_forbidden": True},
        "candidate_rollback_marker_readers": {"schema": "planning-v1-marker-reader-readiness-v1", "status": "passed", "candidate_image_digest": candidate_digest, "rollback_image_digest": rollback_digest, "understood_markers": ["core-v1", "planning-share-v1", "planning-share-reminder-v1"], "startup_fail_closed": True},
        "old_processes_exited": {"schema": "planning-v1-old-processes-exited-v1", "status": "passed", "observed_at": generated, "processes": [{"process_ref": "old-app-1", "image_digest": rollback_digest, "marker_capability": "core-v1", "status": "exited"}]},
        "trusted_readiness_inventory_fresh": {"schema": "planning-v1-trusted-readiness-inventory-v1", "status": "passed", "environment_id": "production-shaped-test", "deployment_id": "deployment-1", "generated_at": generated, "expires_at": expires, "current_marker": "core-v1", "target_marker": "planning-share-v1", "live_builds": [{"build_ref": "candidate-1", "image_digest": candidate_digest, "marker_capability": "planning-share-v1", "wiring_status": "passed"}], "wiring_evidence_sha256": "2" * 64, "control_plane_provenance": "synthetic-selftest", "permission_assumptions": ["single trusted operator"]},
        "share_reminder_writers_enabled": {"schema": "planning-v1-share-reminder-writers-v1", "status": "passed", "active_marker": "planning-share-v1", "share_writer_enabled": True, "reminder_writer_enabled": True, "archive_participant_enabled": True, "writer_image_digest": candidate_digest},
        "schema_v2_restore_pair_retained": {"schema": "planning-v1-schema-v2-retention-v1", "status": "passed", "writer_image_digest": candidate_digest, "restore_tool_digest": tool_digest, "writer_supports_schema_v2": True, "reader_supports_schema_v1": True, "reader_supports_schema_v2": True, "retained_until": retained},
    }
    for check_id, document in documents.items():
        write_json(root / CHECK_ARTIFACTS[check_id], document)
    inventory_sha256 = sha256_file(root / CHECK_ARTIFACTS["trusted_readiness_inventory_fresh"])
    planningctl_readiness = {
        "frame_version": 1,
        "environment": "production-shaped-test",
        "deployment": "deployment-1",
        "schema_hash": documents["additive_schema_available"]["schema_sha256"],
        "content_hash": inventory_sha256,
        "generated_at": generated,
        "expires_at": expires,
        "current": "core-v1",
        "current_revision": 2,
        "target": "planning-share-v1",
        "live_builds": ["candidate-1"],
        "target_wiring_ready": True,
        "control_plane_provenance": "synthetic-selftest",
        "digest": "",
    }
    planningctl_readiness["digest"] = _planningctl_fixture_digest(planningctl_readiness)
    write_json(root / PLANNINGCTL_READINESS_ARTIFACT, planningctl_readiness)
    write_json(root / CHECK_ARTIFACTS["archive_adjacent_cas_readback"], {
        "schema": "planning-v1-archive-cas-readback-v1",
        "status": "passed",
        "expected_revision": 2,
        "observed_revision": 3,
        "previous_marker": "core-v1",
        "current_marker": "planning-share-v1",
        "inventory_sha256": inventory_sha256,
        "readiness_digest": planningctl_readiness["digest"],
        "readback_at": generated,
    })


def _write_readiness(path: Path, repo: Path, status: str = "passed") -> None:
    root = path.parent
    digests = {key: sha256_file(root / filename) for key, filename in ARTIFACTS.items()}
    check_digests = {check_id: sha256_file(root / filename) for check_id, filename in CHECK_ARTIFACTS.items()}
    write_json(path, {
        "schema": "planning-v1-release-readiness-v1",
        "build_revision": _revision(repo),
        "openapi_sha256": sha256_file(repo / "api/openapi.yaml"),
        "migration_head": _migration_head(repo),
        **digests,
        "archive_readiness": {
            "current_marker": "core-v1",
            "target_marker": "planning-share-v1",
            "expected_revision": 2,
            "trusted_inventory_sha256": check_digests["trusted_readiness_inventory_fresh"],
            "planningctl_readiness_sha256": sha256_file(root / PLANNINGCTL_READINESS_ARTIFACT),
            "planningctl_readback_sha256": check_digests["archive_adjacent_cas_readback"],
        },
        "rollback": {"app_image_digest": "sha256:" + "1" * 64, "understands_active_marker": True, "restore_tool_digest": "sha256:" + "4" * 64, "reads_schema_v2": True, "schema_down_forbidden": True},
        "required_checks": [{"id": check_id, "status": "passed", "sha256": check_digests[check_id]} for check_id in REQUIRED_CHECK_IDS],
        "status": status,
    })


def _expect_contract_error(action: Callable[[], object], message: str) -> None:
    try:
        action()
    except ContractError:
        return
    raise AssertionError(message)


def _expect_mutation_rejected(
    path: Path,
    original: dict[str, object],
    mutate: Callable[[dict[str, object]], None],
    validate: Callable[[Path], object],
    message: str,
) -> None:
    candidate = copy.deepcopy(original)
    mutate(candidate)
    write_json(path, candidate)
    _expect_contract_error(lambda: validate(path), message)


def main() -> int:
    repo = Path(__file__).resolve().parents[3]
    conformance = build_conformance(repo)
    if conformance["overall_status"] != "passed" or conformance["gate_dispositions"]["stage-2-evidence-go"] != "pending":
        raise AssertionError("conformance admission unexpectedly failed or gate was mutated")

    with tempfile.TemporaryDirectory(prefix="planning-hardening-test-") as raw:
        root = Path(raw)
        stage1 = root / "stage1.json"
        _stage1(stage1)
        validate_stage1(stage1)
        index = build_evidence_index(stage1, None, None)
        if index["overall_status"] != "blocked" or index["stage_2"]["status"] != "blocked":
            raise AssertionError("missing stage-2 evidence must remain blocked")
        invalid = root / "invalid.json"
        _stage1(invalid)
        invalid_document = load_json(invalid)
        invalid_document["g1"]["live_plan_ids"].append("plan-1")
        write_json(invalid, invalid_document)
        _expect_contract_error(lambda: validate_stage1(invalid), "duplicate live plan accepted")

        stage1_original = load_json(stage1)
        stage1_mutations = (
            (lambda document: document.__setitem__("status", []), "stage-1 array status escaped contract rejection"),
            (lambda document: document["sample_decisions"][0].__setitem__("disposition", []), "stage-1 array disposition escaped contract rejection"),
            (lambda document: document["g1"].__setitem__("live_plan_ids", [[]]), "stage-1 non-string live ID escaped contract rejection"),
            (lambda document: document["g2"]["active_seconds_samples"][0].__setitem__("plan_id", []), "stage-1 non-string G2 plan ID escaped contract rejection"),
            (lambda document: document.__setitem__("exclusions", [{"reason": [], "count": 1, "plan_ids": ["plan-5"]}]), "stage-1 malformed exclusions escaped contract rejection"),
            (lambda document: document["rule_versions"].__setitem__("idle_rules", [[]]), "stage-1 malformed rule versions escaped contract rejection"),
        )
        for mutate, message in stage1_mutations:
            _expect_mutation_rejected(invalid, stage1_original, mutate, validate_stage1, message)

        stage2 = root / "stage2.json"
        _stage2(stage2, "build-1")
        validate_stage2(stage2)
        stage2_original = load_json(stage2)
        stage2_mutations = (
            (lambda document: document.__setitem__("status", {}), "stage-2 object status escaped contract rejection"),
            (lambda document: document["sample_decisions"][0].__setitem__("disposition", []), "stage-2 array disposition escaped contract rejection"),
            (lambda document: document["g3"]["interaction_rate_interval"].__setitem__("lower", "0"), "stage-2 malformed interval escaped contract rejection"),
            (lambda document: document["g3"]["comparable_subset"].__setitem__("shoot_keys", [[]]), "stage-2 malformed comparable subset escaped contract rejection"),
            (lambda document: document.__setitem__("exclusions", [{"reason": "duplicate", "count": 1, "shoot_keys": [[]]}]), "stage-2 malformed exclusions escaped contract rejection"),
            (lambda document: document["rule_versions"].__setitem__("small_sample_interval", []), "stage-2 malformed interval rule escaped contract rejection"),
        )
        for mutate, message in stage2_mutations:
            _expect_mutation_rejected(invalid, stage2_original, mutate, validate_stage2, message)

        decisions = root / "sample-decisions.json"
        _sample_decisions(decisions)
        validate_sample_decisions(decisions)
        decision_original = load_json(decisions)
        decision_mutations = (
            (lambda document: document.__setitem__("decision_revision", True), "boolean decision revision escaped contract rejection"),
            (lambda document: document.__setitem__("cohort", []), "array cohort escaped contract rejection"),
            (lambda document: document["decisions"][0].__setitem__("candidate_ref", []), "malformed candidate ref escaped contract rejection"),
            (lambda document: document["decisions"][0].__setitem__("decision", []), "array sample decision escaped contract rejection"),
            (lambda document: document["decisions"][0].__setitem__("decided_at", []), "malformed decision timestamp escaped contract rejection"),
            (lambda document: document["decisions"][0].__setitem__("source_fact_refs", [[]]), "malformed source fact row escaped contract rejection"),
            (lambda document: document["decisions"][0]["source_fact_refs"][0].__setitem__("revision", True), "boolean source fact revision escaped contract rejection"),
        )
        for mutate, message in decision_mutations:
            _expect_mutation_rejected(invalid, decision_original, mutate, validate_sample_decisions, message)

        artifacts = root / "artifacts"
        artifacts.mkdir()
        readiness = artifacts / "planning_v1_release_readiness.json"
        _write_artifacts(artifacts, repo)
        _write_check_artifacts(artifacts, repo)
        _write_readiness(readiness, repo)
        _expect_contract_error(
            lambda: verify_readiness(readiness, repo),
            "canonical pending stage approvals allowed a release pass",
        )
        fixture_bindings = _fixture_stage_bindings(artifacts)

        def verify_fixture(candidate: Path) -> dict[str, object]:
            return _verify_readiness_with_bindings(candidate, repo, fixture_bindings)

        if verify_fixture(readiness)["derived_status"] != "passed":
            raise AssertionError("valid structural release fixture rejected")

        evidence_index_path = artifacts / "planning_v1_evidence_index.json"
        original_index = load_json(evidence_index_path)
        absolute_stage = copy.deepcopy(original_index)
        absolute_path = str((artifacts / "stage-1.json").resolve(strict=True))
        absolute_stage["stage_1"]["json_path"] = absolute_path
        absolute_stage["approval"]["stage-1-evidence-go"]["path"] = absolute_path
        write_json(evidence_index_path, absolute_stage)
        _write_readiness(readiness, repo)
        _expect_contract_error(lambda: verify_fixture(readiness), "absolute stage evidence path accepted")

        (artifacts / "nested").mkdir()
        traversal_stage = copy.deepcopy(original_index)
        traversal_path = "nested/../stage-1.json"
        traversal_stage["stage_1"]["json_path"] = traversal_path
        traversal_stage["approval"]["stage-1-evidence-go"]["path"] = traversal_path
        write_json(evidence_index_path, traversal_stage)
        _write_readiness(readiness, repo)
        _expect_contract_error(lambda: verify_fixture(readiness), "stage evidence path traversal accepted")
        write_json(evidence_index_path, original_index)

        package = artifacts / REHEARSAL_PACKAGE_DIRECTORY
        hidden_package = artifacts / ".planning_restore_package.missing"
        package.rename(hidden_package)
        try:
            _write_readiness(readiness, repo)
            _expect_contract_error(lambda: verify_fixture(readiness), "missing rehearsal package accepted")
        finally:
            hidden_package.rename(package)

        database_dump = package / "database.sql"
        original_database_dump = database_dump.read_bytes()
        database_dump.write_bytes(original_database_dump + b"\n-- changed after rehearsal\n")
        _write_readiness(readiness, repo)
        _expect_contract_error(lambda: verify_fixture(readiness), "changed rehearsal package bytes accepted")
        database_dump.write_bytes(original_database_dump)

        rehearsal_path = artifacts / "planning_restore_rehearsal.json"
        original_rehearsal = load_json(rehearsal_path)
        arbitrary_rehearsal = copy.deepcopy(original_rehearsal)
        arbitrary_rehearsal["package_sha256"] = "5" * 64
        write_json(rehearsal_path, arbitrary_rehearsal)
        _write_readiness(readiness, repo)
        _expect_contract_error(lambda: verify_fixture(readiness), "arbitrary rehearsal package digest accepted")
        write_json(rehearsal_path, original_rehearsal)

        planningctl_path = artifacts / PLANNINGCTL_READINESS_ARTIFACT
        original_planningctl = load_json(planningctl_path)
        unbound_planningctl = copy.deepcopy(original_planningctl)
        unbound_planningctl["environment"] = "different-environment"
        unbound_planningctl["digest"] = _planningctl_fixture_digest(unbound_planningctl)
        write_json(planningctl_path, unbound_planningctl)
        _write_readiness(readiness, repo)
        _expect_contract_error(lambda: verify_fixture(readiness), "unbound planningctl readiness frame accepted")
        write_json(planningctl_path, original_planningctl)

        readback_path = artifacts / CHECK_ARTIFACTS["archive_adjacent_cas_readback"]
        original_readback = load_json(readback_path)
        unbound_readback = copy.deepcopy(original_readback)
        unbound_readback["readiness_digest"] = "0" * 64
        write_json(readback_path, unbound_readback)
        _write_readiness(readiness, repo)
        _expect_contract_error(lambda: verify_fixture(readiness), "CAS readback without readiness digest binding accepted")
        write_json(readback_path, original_readback)
        _write_readiness(readiness, repo)

        release_artifact_mutations = (
            ("planning_v1_failure_matrix.json", lambda document: document.__setitem__("overall_status", []), "failure matrix array status escaped contract rejection"),
            ("planning_v1_failure_matrix.json", lambda document: document["cases"][0].__setitem__("status", {}), "failure case object status escaped contract rejection"),
            ("planning_v1_h1_h2_negative_matrix.json", lambda document: document.__setitem__("overall_status", []), "H1/H2 array status escaped contract rejection"),
            ("planning_v1_h1_h2_negative_matrix.json", lambda document: document["rows"][0].__setitem__("status", {}), "H1/H2 row object status escaped contract rejection"),
            ("planning_v1_prototype_responsive_a11y_matrix.json", lambda document: document.__setitem__("overall_status", []), "responsive array status escaped contract rejection"),
            ("planning_v1_prototype_responsive_a11y_matrix.json", lambda document: document["rows"][0].__setitem__("status", {}), "responsive row object status escaped contract rejection"),
            ("planning_v1_evidence_index.json", lambda document: document.__setitem__("overall_status", []), "evidence index array status escaped contract rejection"),
            ("planning_v1_evidence_index.json", lambda document: document["stage_1"].__setitem__("status", []), "evidence stage array status escaped contract rejection"),
            ("planning_v1_evidence_index.json", lambda document: document["approval"]["stage-1-evidence-go"].__setitem__("status", []), "approval array status escaped contract rejection"),
            ("planning_v1_evidence_index.json", lambda document: document["cohort_comparison"].__setitem__("limitations", []), "empty cohort limitations escaped contract rejection"),
            ("planning_v1_evidence_index.json", lambda document: document["cohort_comparison"].__setitem__("shared_keys", [[]]), "malformed cohort key escaped contract rejection"),
        )
        for filename, mutate, message in release_artifact_mutations:
            artifact_path = artifacts / filename
            original = load_json(artifact_path)
            mutated = copy.deepcopy(original)
            mutate(mutated)
            write_json(artifact_path, mutated)
            _write_readiness(readiness, repo)
            _expect_contract_error(lambda: verify_fixture(readiness), message)
            write_json(artifact_path, original)

        _write_readiness(readiness, repo)
        malformed_readiness = load_json(readiness)
        malformed_readiness["status"] = []
        write_json(readiness, malformed_readiness)
        _expect_contract_error(lambda: verify_fixture(readiness), "release root array status escaped contract rejection")

        _write_readiness(readiness, repo)
        malformed_readiness = load_json(readiness)
        malformed_readiness["required_checks"][0]["status"] = []
        write_json(readiness, malformed_readiness)
        _expect_contract_error(lambda: verify_fixture(readiness), "release check array status escaped contract rejection")

        coverage_sets = (
            ("planning_v1_dependency_conformance.json", "rows", "id", REQUIRED_CONFORMANCE_IDS),
            ("planning_v1_e2e_results.json", "scenarios", "id", REQUIRED_E2E_IDS),
            ("planning_v1_failure_matrix.json", "cases", "id", REQUIRED_FAILURE_IDS),
            ("planning_v1_h1_h2_negative_matrix.json", "rows", "id", REQUIRED_H1_H2_IDS),
            ("planning_v1_prototype_responsive_a11y_matrix.json", "rows", "scenario", REQUIRED_RESPONSIVE_IDS),
        )
        for filename, collection, identity_key, required_ids in coverage_sets:
            artifact_path = artifacts / filename
            original = load_json(artifact_path)
            for required_id in required_ids:
                missing = copy.deepcopy(original)
                missing[collection] = [row for row in missing[collection] if row[identity_key] != required_id]
                if filename == "planning_v1_failure_matrix.json":
                    missing["executed_case_count"] = len(missing[collection])
                write_json(artifact_path, missing)
                _write_readiness(readiness, repo)
                _expect_contract_error(lambda: verify_fixture(readiness), f"missing required evidence ID accepted: {required_id}")
            write_json(artifact_path, original)

        _write_readiness(readiness, repo)
        complete_readiness = load_json(readiness)
        for check_id in REQUIRED_CHECK_IDS:
            missing_check = copy.deepcopy(complete_readiness)
            missing_check["required_checks"] = [row for row in missing_check["required_checks"] if row["id"] != check_id]
            write_json(readiness, missing_check)
            _expect_contract_error(lambda: verify_fixture(readiness), f"missing readiness check ID accepted: {check_id}")

        _write_readiness(readiness, repo)
        for filename in (*CHECK_ARTIFACTS.values(), PLANNINGCTL_READINESS_ARTIFACT, "stage-1.json", "stage-2.json"):
            artifact_path = artifacts / filename
            hidden_path = artifacts / f".{filename}.missing"
            artifact_path.rename(hidden_path)
            try:
                _expect_contract_error(lambda: verify_fixture(readiness), f"missing sibling evidence accepted: {filename}")
            finally:
                hidden_path.rename(artifact_path)

        for filename in ("stage-1.json", "stage-2.json"):
            artifact_path = artifacts / filename
            original_bytes = artifact_path.read_bytes()
            artifact_path.write_bytes(original_bytes + b" ")
            _expect_contract_error(lambda: verify_fixture(readiness), f"changed canonical stage bytes accepted: {filename}")
            artifact_path.write_bytes(original_bytes)

        _write_readiness(readiness, repo)
        for check_id, filename in CHECK_ARTIFACTS.items():
            check_path = artifacts / filename
            original_bytes = check_path.read_bytes()
            check_path.write_bytes(original_bytes + b" ")
            _expect_contract_error(lambda: verify_fixture(readiness), f"changed readiness sibling bytes accepted: {check_id}")
            check_path.write_bytes(original_bytes)

        _write_readiness(readiness, repo)
        broken = load_json(readiness)
        broken["rollback"]["reads_schema_v2"] = False
        write_json(readiness, broken)
        _expect_contract_error(lambda: verify_fixture(readiness), "rollback without v2 reader accepted")

        _write_readiness(readiness, repo)
        broken = load_json(readiness)
        broken["required_checks"] = [{"id": "arbitrary", "status": "passed"}]
        write_json(readiness, broken)
        _expect_contract_error(lambda: verify_fixture(readiness), "arbitrary release check IDs accepted")

        _write_readiness(readiness, repo)
        stale = load_json(readiness)
        stale["build_revision"] = "0" * 40
        write_json(readiness, stale)
        _expect_contract_error(lambda: verify_fixture(readiness), "stale build revision accepted")

        _write_readiness(readiness, repo)
        e2e_path = artifacts / "planning_v1_e2e_results.json"
        changed = load_json(e2e_path)
        changed["scenarios"].append({"id": "changed", "status": "passed", "assertions": ["changed bytes"]})
        write_json(e2e_path, changed)
        _expect_contract_error(lambda: verify_fixture(readiness), "sibling artifact hash mismatch accepted")

        _write_artifacts(artifacts, repo)
        fake_rehearsal = load_json(artifacts / "planning_restore_rehearsal.json")
        fake_rehearsal["environment_class"] = "synthetic"
        write_json(artifacts / "planning_restore_rehearsal.json", fake_rehearsal)
        _write_readiness(readiness, repo)
        _expect_contract_error(lambda: verify_fixture(readiness), "synthetic rehearsal accepted")

        _write_artifacts(artifacts, repo)
        pending = load_json(artifacts / "planning_v1_evidence_index.json")
        pending["stage_2"] = {"status": "blocked", "stale": True, "reason": "owner evidence pending"}
        pending["approval"] = {"stage-1-evidence-go": "pending", "stage-2-evidence-go": "pending"}
        pending["overall_status"] = "blocked"
        write_json(artifacts / "planning_v1_evidence_index.json", pending)
        _write_readiness(readiness, repo)
        _expect_contract_error(lambda: verify_fixture(readiness), "pending stage evidence fake pass accepted")

    print("planning hardening validator selftest: passed")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
