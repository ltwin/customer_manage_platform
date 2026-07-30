#!/usr/bin/env python3
"""Validate v1-hardening ops smoke results against the frozen catalog."""

from __future__ import annotations

import argparse
import copy
import hashlib
import json
import sys
from collections import Counter
from pathlib import Path, PurePosixPath
from typing import Any

import yaml


FEATURE = "2026-07-22-v1-hardening"
TOP_FIELDS = {
    "schema_version", "feature", "status", "target", "catalog", "coverage",
    "suite_cleanup", "cases",
}
CASE_FIELDS = {
    "case_id", "scenario_ids", "fixture", "action", "oracle_class", "expected",
    "observed", "result", "stages", "target_before", "target_after",
    "package_oracle", "backup_package", "lock_observations", "sentinel_unchanged",
    "operation_cleanup", "harness_cleanup", "evidence_paths",
}
OBSERVED_FIELDS = {
    "exit_code", "terminal_stage", "engine_calls", "target_mutation_started",
    "lock_roles", "cleanup_objects", "side_effects",
}
ENGINE_FIELDS = {"readonly", "lock_metadata", "target_mutation"}
STAGE_FIELDS = {"name", "operation_status", "oracle_status"}
CLEANUP_FIELDS = {"result", "residual_resources"}
LOCK_FIELDS = {
    "role", "container_id_sha256", "image_declared_volume_count", "state",
    "network_mode", "mount_count", "started", "removed", "removed_by",
    "anonymous_volume_count_after",
}
TARGET_FIELDS = {
    "app_state", "db_counts", "marker_sha256", "manifest_sha256",
}
APP_FIELDS = {"logical", "engine_status", "running", "paused", "restarting", "dead"}
ENVELOPE_FIELDS = {"status", "value", "error_class"}
PACKAGE_FIELDS = {
    "status", "files", "metadata_schema_version", "checksums_verified",
    "manifest_verified", "self_check_passed", "temp_package_residuals",
}
RESULT_VALUES = {"pending", "pass", "fail", "blocked"}
OPERATION_VALUES = {"not-run", "ok", "failed", "blocked"}
POLICY_RANK = {"forbidden": 0, "readonly": 1, "lock-metadata": 2, "target-operation": 3}
ENGINE_STATUS = {"created", "running", "paused", "restarting", "removing", "exited", "dead", "absent", "unknown"}
LOGICAL_STATUS = {"running", "stopped", "rejected", "absent"}
ERROR_CLASSES = {"artifact-missing", "artifact-invalid", "schema-missing", "query-failed", "permission-denied", "unknown"}
FIVE_FILES = {"database.sql", "avatar-volume.tgz", "avatar-manifest.json", "metadata.json", "SHA256SUMS"}
SHA256_CHARS = set("0123456789abcdef")
FENCED_PLANS = {"BKR", "BKE", "BKF", "BKP", "LCK", "LCO", "RSP", "RSR", "RSE", "RSF", "COR"}
BACKUP_ORDER = [
    "backup-stop", "backup-pg-dump", "backup-avatar-tar", "backup-manifest",
    "backup-package-verify", "backup-state-restore", "backup-health", "backup-publish",
]
RESTORE_ORDER = [
    "package-validate", "restore-stop", "restore-db-replace", "restore-avatar-replace",
    "restore-verify", "restore-start", "restore-health",
]
LOCK_PREFIX = ["lock-acquire", "lock-post-inspect", "helper-fence", "target-identity"]
ALLOWED_STAGES = {
    "dependency-check", "path-validate", "complete", "usage", "endpoint-select",
    "env-parse", "config-matrix", "compose-version", "compose-render", "image-safety",
    "lock-acquire", "lock-post-inspect", "helper-fence", "target-identity",
    "package-validate", "backup-stop", "backup-pg-dump", "backup-avatar-tar",
    "backup-manifest", "backup-package-verify", "backup-state-restore", "backup-health",
    "backup-publish", "restore-stop", "restore-db-replace", "restore-avatar-replace",
    "restore-verify", "restore-start", "restore-health", "restore-failure-stop", "cleanup",
}


def sha256(path: Path) -> str:
    return hashlib.sha256(path.read_bytes()).hexdigest()


def error(errors: list[str], case_id: str | None, message: str) -> None:
    errors.append(f"{case_id}: {message}" if case_id else message)


def is_sha256(value: Any) -> bool:
    return isinstance(value, str) and len(value) == 64 and set(value) <= SHA256_CHARS


def mapping_fields(value: Any, fields: set[str], label: str, errors: list[str], case_id: str | None = None) -> bool:
    if not isinstance(value, dict):
        error(errors, case_id, f"{label} must be a mapping")
        return False
    if set(value) != fields:
        error(errors, case_id, f"{label} fields must equal {sorted(fields)}")
        return False
    return True


def validate_cleanup(value: Any, label: str, errors: list[str], case_id: str) -> None:
    if not mapping_fields(value, CLEANUP_FIELDS, label, errors, case_id):
        return
    if value["result"] not in {"pass", "fail"}:
        error(errors, case_id, f"{label}.result must be pass|fail")
    if not isinstance(value["residual_resources"], list) or not all(isinstance(item, str) for item in value["residual_resources"]):
        error(errors, case_id, f"{label}.residual_resources must be string[]")


def validate_envelope(value: Any, label: str, errors: list[str], case_id: str) -> None:
    if not mapping_fields(value, ENVELOPE_FIELDS, label, errors, case_id):
        return
    status = value["status"]
    if status == "observed":
        if value["value"] is None or value["error_class"] is not None:
            error(errors, case_id, f"{label} observed envelope requires value and null error_class")
    elif status in {"absent", "unreadable"}:
        if value["value"] is not None or value["error_class"] not in ERROR_CLASSES:
            error(errors, case_id, f"{label} {status} envelope has invalid value/error_class")
    else:
        error(errors, case_id, f"{label}.status is invalid")


def validate_target(value: Any, label: str, errors: list[str], case_id: str) -> None:
    if value is None:
        return
    if not mapping_fields(value, TARGET_FIELDS, label, errors, case_id):
        return
    app = value["app_state"]
    if mapping_fields(app, APP_FIELDS, f"{label}.app_state", errors, case_id):
        if app["logical"] not in LOGICAL_STATUS or app["engine_status"] not in ENGINE_STATUS:
            error(errors, case_id, f"{label}.app_state status is invalid")
        booleans = [app[key] for key in ("running", "paused", "restarting", "dead")]
        if app["logical"] == "absent":
            if any(item is not None for item in booleans):
                error(errors, case_id, f"{label}.app_state absent requires null booleans")
        elif any(not isinstance(item, bool) for item in booleans):
            error(errors, case_id, f"{label}.app_state requires boolean state fields")
        else:
            expected_predicates = {
                "running": ("running", True, False, False, False),
                "exited": ("stopped", False, False, False, False),
                "created": ("rejected", False, False, False, False),
                "paused": ("rejected", True, True, False, False),
                "restarting": ("rejected", True, False, True, False),
                "removing": ("rejected", False, False, False, False),
                "dead": ("rejected", False, False, False, True),
                "unknown": ("rejected", False, False, False, False),
            }
            predicate = expected_predicates.get(app["engine_status"])
            actual = (app["logical"], app["running"], app["paused"], app["restarting"], app["dead"])
            if predicate is not None and actual != predicate:
                error(errors, case_id, f"{label}.app_state logical/Engine predicate is inconsistent")
    for key in ("db_counts", "marker_sha256", "manifest_sha256"):
        validate_envelope(value[key], f"{label}.{key}", errors, case_id)


def validate_observed(case: dict[str, Any], errors: list[str]) -> None:
    case_id = case["case_id"]
    expected = case["expected"]
    observed = case["observed"]
    if not mapping_fields(observed, OBSERVED_FIELDS, "observed", errors, case_id):
        return
    calls = observed["engine_calls"]
    if not mapping_fields(calls, ENGINE_FIELDS, "observed.engine_calls", errors, case_id):
        return
    if any(not isinstance(value, int) or isinstance(value, bool) or value < 0 for value in calls.values()):
        error(errors, case_id, "engine call counts must be non-negative integers")
    used_rank = 3 if calls["target_mutation"] else 2 if calls["lock_metadata"] else 1 if calls["readonly"] else 0
    if used_rank > POLICY_RANK[expected["engine_call_policy"]]:
        error(errors, case_id, "observed Engine calls exceed expected policy")
    for key in ("exit_code", "terminal_stage"):
        if observed[key] != expected[key]:
            error(errors, case_id, f"observed.{key} differs from expected")
    for key, expected_key in (("lock_roles", "required_lock_roles"), ("cleanup_objects", "cleanup_objects")):
        if sorted(observed[key]) != sorted(expected[expected_key]):
            error(errors, case_id, f"observed.{key} differs from expected")
    if observed["target_mutation_started"] and not expected["target_mutation_allowed"]:
        error(errors, case_id, "target mutation started without authorization")
    if set(observed["side_effects"]) & set(expected["forbidden_side_effects"]):
        error(errors, case_id, "forbidden side effect observed")


def validate_stages(case: dict[str, Any], errors: list[str]) -> None:
    case_id = case["case_id"]
    expected = case["expected"]
    stages = case["stages"]
    if not isinstance(stages, list):
        error(errors, case_id, "stages must be a list")
        return
    if any(not isinstance(item, dict) or set(item) != STAGE_FIELDS for item in stages):
        error(errors, case_id, "each stage must use the canonical fields")
        return
    names = [item["name"] for item in stages]
    if len(names) != len(set(names)):
        error(errors, case_id, "stage names must be unique")
    by_name = {item["name"]: item for item in stages}
    for item in stages:
        if item["name"] not in ALLOWED_STAGES:
            error(errors, case_id, f"unknown stage {item['name']}")
        if item["operation_status"] not in OPERATION_VALUES or item["oracle_status"] not in RESULT_VALUES:
            error(errors, case_id, f"invalid stage status for {item['name']}")
    required = required_stage_outcomes(case)
    required_names = list(required)
    if names != required_names:
        error(errors, case_id, f"stage order/set must equal frozen plan {required_names}")
    for name, status in required.items():
        item = by_name.get(name)
        if item is None or item["operation_status"] != status:
            error(errors, case_id, f"stage plan {expected['stage_plan']} requires {name}={status}")
        elif item["oracle_status"] != "pass":
            error(errors, case_id, f"required stage {name} oracle must pass")


def required_stage_outcomes(case: dict[str, Any]) -> dict[str, str]:
    expected = case["expected"]
    plan = expected["stage_plan"]
    terminal = expected["terminal_stage"]
    if plan == "PFB":
        return {name: "ok" for name in ("dependency-check", "env-parse", "config-matrix", "complete")}
    if plan == "PFC":
        return {name: "ok" for name in ("dependency-check", "endpoint-select", "env-parse", "config-matrix", "compose-version", "compose-render", "complete")}
    if plan in {"PFR", "DEP", "PATH"}:
        return {terminal: "failed"}
    if plan == "COR":
        return {"cleanup": "failed"}
    if plan in {"BKR", "BKE"}:
        names = LOCK_PREFIX + BACKUP_ORDER + ["complete"]
        skipped = {"backup-stop", "backup-state-restore", "backup-health"} if plan == "BKE" else set()
        return {name: "not-run" if name in skipped else "ok" for name in names}
    if plan == "BKF":
        outcomes = {name: "ok" for name in LOCK_PREFIX}
        terminal_index = BACKUP_ORDER.index(terminal)
        for index, name in enumerate(BACKUP_ORDER):
            outcomes[name] = "ok" if index < terminal_index else "failed" if name == terminal else "not-run"
        outcomes["backup-state-restore"] = "ok"
        if terminal != "backup-health":
            outcomes["backup-health"] = "ok"
        return outcomes
    if plan == "BKP":
        if terminal == "image-safety":
            return {"image-safety": "failed"}
        return {
            "lock-acquire": "ok", "lock-post-inspect": "ok", "helper-fence": "ok",
            "target-identity": "failed",
        }
    if plan == "LCK":
        if terminal == "lock-acquire":
            return {"lock-acquire": "failed", "helper-fence": "not-run"}
        if terminal == "lock-post-inspect":
            return {"lock-acquire": "ok", "lock-post-inspect": "failed", "helper-fence": "not-run"}
        return {"lock-acquire": "ok", "lock-post-inspect": "ok", "helper-fence": "failed"}
    if plan == "LCO":
        return {"lock-acquire": "ok", "lock-post-inspect": "ok", "helper-fence": "not-run", "complete": "ok"}
    if plan == "RSP":
        if expected["engine_call_policy"] == "forbidden":
            return {terminal: "failed"}
        if terminal == "target-identity":
            return {
                "lock-acquire": "ok", "lock-post-inspect": "ok", "helper-fence": "ok",
                "target-identity": "failed",
            }
        return {
            "lock-acquire": "ok", "lock-post-inspect": "ok", "helper-fence": "ok",
            "target-identity": "ok", "package-validate": "failed",
        }
    if plan in {"RSR", "RSE"}:
        names = LOCK_PREFIX + RESTORE_ORDER + ["complete"]
        skipped = {"restore-stop", "restore-start", "restore-health"} if plan == "RSE" else set()
        return {name: "not-run" if name in skipped else "ok" for name in names}
    if plan == "RSF":
        outcomes = {name: "ok" for name in LOCK_PREFIX}
        terminal_index = RESTORE_ORDER.index(terminal)
        for index, name in enumerate(RESTORE_ORDER):
            outcomes[name] = "ok" if index < terminal_index else "failed" if name == terminal else "not-run"
        outcomes["restore-failure-stop"] = "ok"
        return outcomes
    return {terminal: "failed"}


def validate_backup_package(case: dict[str, Any], errors: list[str]) -> None:
    case_id = case["case_id"]
    expectation = case["expected"]["backup_package_expectation"]
    package = case["backup_package"]
    if expectation == "none":
        if package is not None:
            error(errors, case_id, "backup_package must be null when expectation is none")
        return
    if not mapping_fields(package, PACKAGE_FIELDS, "backup_package", errors, case_id):
        return
    if package["temp_package_residuals"]:
        error(errors, case_id, "backup package has temporary residuals")
    if expectation == "published-valid":
        if package["status"] != "published" or set(package["files"]) != FIVE_FILES:
            error(errors, case_id, "published backup must contain the exact five files")
        if package["metadata_schema_version"] != 1 or not all(package[key] is True for key in ("checksums_verified", "manifest_verified", "self_check_passed")):
            error(errors, case_id, "published backup verification is incomplete")
    elif expectation == "not-published-clean":
        if package["status"] not in {"not-created", "partial-cleaned"}:
            error(errors, case_id, "failed backup published a final package")


def validate_locks(case: dict[str, Any], errors: list[str]) -> None:
    case_id = case["case_id"]
    observations = case["lock_observations"]
    if not isinstance(observations, list):
        error(errors, case_id, "lock_observations must be a list")
        return
    allowed = case["expected"]["allowed_operation_removers"]
    roles_seen: Counter[str] = Counter()
    for item in observations:
        if not mapping_fields(item, LOCK_FIELDS, "lock_observation", errors, case_id):
            continue
        if item["state"] not in {"created", "absent", None} or item["removed_by"] not in {"owner", "authorized-breaker", "harness-finalizer", "none", None}:
            error(errors, case_id, "lock observation enum is invalid")
        if item["container_id_sha256"] is not None and not is_sha256(item["container_id_sha256"]):
            error(errors, case_id, "lock observation must contain only a SHA-256 container identity")
        if item["state"] == "created" and item["role"] != "invalid-candidate":
            if item["image_declared_volume_count"] != 0 or item["network_mode"] != "none" or item["mount_count"] != 0 or item["started"] is not False:
                error(errors, case_id, "created lock/helper violates volume/network/start invariants")
        if item["role"] == "invalid-candidate" and all((
            item["image_declared_volume_count"] == 0,
            item["network_mode"] == "none",
            item["mount_count"] == 0,
            item["started"] is False,
            item["state"] == "created",
        )):
            error(errors, case_id, "invalid-candidate did not record the injected post-create invariant violation")
        if isinstance(item["role"], str):
            roles_seen[item["role"]] += 1
        if item["removed_by"] == "harness-finalizer" and item["role"] in allowed:
            error(errors, case_id, "operation-owned lock/helper leaked to harness finalizer")
        if item["removed"] is True and item["removed_by"] in {None, "none"}:
            error(errors, case_id, "removed lock/helper must name its remover")
        if item["removed"] is False and item["removed_by"] not in {None, "none"}:
            error(errors, case_id, "unremoved lock/helper cannot name a remover")
        expected_remover = allowed.get(item["role"])
        if expected_remover and item["removed"] is True and item["removed_by"] != expected_remover:
            error(errors, case_id, "lock/helper was removed by an unauthorized generation")
        if item["removed"] is True and item["anonymous_volume_count_after"] != 0:
            error(errors, case_id, "removed lock/helper left anonymous volumes")
    for role in case["expected"]["required_lock_roles"]:
        if roles_seen[role] == 0:
            error(errors, case_id, f"required lock role {role} has no structural observation")
            continue
        remover = allowed.get(role)
        role_items = [item for item in observations if isinstance(item, dict) and item.get("role") == role]
        if remover == "none":
            if not any(item.get("removed") is False and item.get("removed_by") in {None, "none"} for item in role_items):
                error(errors, case_id, f"lock role {role} must remain owned by its observed generation")
        elif remover in {"owner", "authorized-breaker"}:
            if not any(item.get("removed") is True and item.get("removed_by") == remover for item in role_items):
                error(errors, case_id, f"lock role {role} lacks its authorized immutable-ID removal")


def validate_oracle(case: dict[str, Any], errors: list[str]) -> None:
    case_id = case["case_id"]
    oracle = case["oracle_class"]
    before, after = case["target_before"], case["target_after"]
    validate_target(before, "target_before", errors, case_id)
    validate_target(after, "target_after", errors, case_id)
    if oracle in {"pre-mutation-readonly", "pre-mutation-reject"} and before != after:
        error(errors, case_id, "pre-mutation oracle requires before=after")
    elif oracle == "backup-readonly":
        if before is None or after is None:
            error(errors, case_id, "backup oracle requires before and after")
        elif before != after:
            error(errors, case_id, "backup must restore the complete target predicate and data")
    elif oracle == "restore-success":
        package_oracle = case["package_oracle"]
        if after is None or not isinstance(package_oracle, dict) or set(package_oracle) != {"db_counts", "marker_sha256", "manifest_sha256"}:
            error(errors, case_id, "restore success requires target_after and package_oracle")
        else:
            for key in package_oracle:
                envelope = after[key]
                if envelope.get("status") != "observed" or envelope.get("value") != package_oracle[key]:
                    error(errors, case_id, f"restore target_after.{key} differs from package oracle")
            if isinstance(before, dict) and before.get("app_state") != after.get("app_state"):
                error(errors, case_id, "restore success did not restore the original supported app predicate")
    elif oracle == "restore-destructive-failure":
        if after is None:
            error(errors, case_id, "destructive restore failure must record target_after")
        elif not isinstance(after.get("app_state"), dict) or after["app_state"].get("logical") != "stopped" or after["app_state"].get("engine_status") != "exited":
            error(errors, case_id, "destructive restore failure must end stopped/exited")
        if not isinstance(case.get("observed"), dict) or not case["observed"].get("target_mutation_started"):
            error(errors, case_id, "destructive restore failure must record target mutation")


def validate_case(case: Any, catalog_case: dict[str, Any], errors: list[str]) -> None:
    case_id = catalog_case["case_id"]
    if not isinstance(case, dict) or set(case) != CASE_FIELDS:
        error(errors, case_id, f"case fields must equal {sorted(CASE_FIELDS)}")
        return
    for key in ("scenario_ids", "fixture", "action", "oracle_class", "expected"):
        if case[key] != catalog_case[key]:
            error(errors, case_id, f"{key} differs from catalog")
    if case["result"] not in RESULT_VALUES:
        error(errors, case_id, "case result is invalid")
    if not isinstance(case["sentinel_unchanged"], bool) or not case["sentinel_unchanged"]:
        error(errors, case_id, "sentinel changed or was not observed")
    evidence = case["evidence_paths"]
    if not isinstance(evidence, list) or not evidence:
        error(errors, case_id, "evidence_paths must be a non-empty list")
    else:
        for raw in evidence:
            if not isinstance(raw, str) or PurePosixPath(raw).is_absolute() or ".." in PurePosixPath(raw).parts:
                error(errors, case_id, "evidence path must be feature-relative")
    validate_observed(case, errors)
    validate_stages(case, errors)
    validate_backup_package(case, errors)
    validate_locks(case, errors)
    validate_cleanup(case["operation_cleanup"], "operation_cleanup", errors, case_id)
    validate_cleanup(case["harness_cleanup"], "harness_cleanup", errors, case_id)
    expected_cleanup = case["expected"]["operation_cleanup_expected"]
    if isinstance(case["operation_cleanup"], dict) and case["operation_cleanup"].get("result") != expected_cleanup:
        error(errors, case_id, "operation cleanup differs from expected")
    if isinstance(case["harness_cleanup"], dict) and (case["harness_cleanup"].get("result") != "pass" or case["harness_cleanup"].get("residual_resources")):
        error(errors, case_id, "harness cleanup must pass without residuals")
    validate_oracle(case, errors)
    if case["result"] == "pass":
        case_errors_before = [item for item in errors if item.startswith(f"{case_id}:")]
        if case_errors_before:
            error(errors, case_id, "case result=pass is inconsistent with failed structural/oracle checks")


def validate(catalog: Any, results: Any, catalog_path: Path) -> list[str]:
    errors: list[str] = []
    if not isinstance(catalog, dict) or catalog.get("feature") != FEATURE or not isinstance(catalog.get("cases"), list):
        return ["catalog identity or cases are invalid"]
    if not mapping_fields(results, TOP_FIELDS, "results", errors):
        return errors
    if results["schema_version"] != 1 or results["feature"] != FEATURE:
        error(errors, None, "results identity mismatch")
    if results["status"] not in RESULT_VALUES:
        error(errors, None, "results status is invalid")
    target = results["target"]
    if not isinstance(target, dict) or set(target) != {"project_name", "target_hash"} or not isinstance(target.get("project_name"), str) or not target["project_name"] or not is_sha256(target.get("target_hash")):
        error(errors, None, "target must contain non-empty synthetic project_name and target_hash")
    catalog_meta = results["catalog"]
    required = [item["case_id"] for item in catalog["cases"]]
    if not isinstance(catalog_meta, dict) or set(catalog_meta) != {"path", "sha256", "required_case_ids"}:
        error(errors, None, "catalog metadata fields are invalid")
    else:
        if catalog_meta["sha256"] != sha256(catalog_path) or catalog_meta["required_case_ids"] != required:
            error(errors, None, "catalog sha or required IDs mismatch")
    cases = results["cases"] if isinstance(results["cases"], list) else []
    ids = [item.get("case_id") for item in cases if isinstance(item, dict)]
    counts = Counter(ids)
    duplicates = sorted(key for key, count in counts.items() if count > 1)
    missing = sorted(set(required) - set(ids))
    unknown = sorted(set(ids) - set(required))
    coverage = results["coverage"]
    if not isinstance(coverage, dict) or set(coverage) != {"executed_case_ids", "missing_case_ids", "unknown_case_ids", "duplicate_case_ids"}:
        error(errors, None, "coverage fields are invalid")
    else:
        if coverage["executed_case_ids"] != ids or coverage["missing_case_ids"] != missing or coverage["unknown_case_ids"] != unknown or coverage["duplicate_case_ids"] != duplicates:
            error(errors, None, "coverage does not describe actual result cases")
    catalog_by_id = {item["case_id"]: item for item in catalog["cases"]}
    for item in cases:
        if isinstance(item, dict) and item.get("case_id") in catalog_by_id:
            validate_case(item, catalog_by_id[item["case_id"]], errors)
    validate_cleanup(results["suite_cleanup"], "suite_cleanup", errors, "suite")
    if isinstance(results["suite_cleanup"], dict) and (results["suite_cleanup"].get("result") != "pass" or results["suite_cleanup"].get("residual_resources")):
        error(errors, None, "suite cleanup must pass without residuals")
    if results["status"] == "pass":
        if missing or unknown or duplicates or any(item.get("result") != "pass" for item in cases if isinstance(item, dict)):
            error(errors, None, "top-level pass requires complete unique passing cases")
    case_results = [item.get("result") for item in cases if isinstance(item, dict)]
    expected_status = (
        "fail" if "fail" in case_results
        or isinstance(results.get("suite_cleanup"), dict) and results["suite_cleanup"].get("result") == "fail"
        else "blocked" if missing or unknown or duplicates or any(item in {"pending", "blocked"} for item in case_results)
        else "pass"
    )
    if results["status"] != expected_status:
        error(errors, None, f"top-level status must be {expected_status} for actual coverage/results")
    return errors


def negative_corpus(catalog: dict[str, Any], results: dict[str, Any], catalog_path: Path) -> list[str]:
    failures: list[str] = []

    def reject(name: str, mutate: Any) -> None:
        candidate = copy.deepcopy(results)
        mutate(candidate)
        if not validate(catalog, candidate, catalog_path):
            failures.append(f"negative corpus accepted: {name}")

    def case(doc: dict[str, Any], case_id: str) -> dict[str, Any] | None:
        return next((item for item in doc["cases"] if item["case_id"] == case_id), None)

    def reject_case(name: str, case_id: str, mutate: Any) -> None:
        if case(results, case_id) is None:
            return
        reject(name, lambda doc: mutate(case(doc, case_id)))

    reject("missing", lambda doc: doc["cases"].pop())
    reject("unknown", lambda doc: doc["cases"].append({**copy.deepcopy(doc["cases"][0]), "case_id": "OPS-UNKNOWN"}))
    reject("duplicate", lambda doc: doc["cases"].append(copy.deepcopy(doc["cases"][0])))
    reject("catalog sha", lambda doc: doc["catalog"].update(sha256="0" * 64))
    reject("expected", lambda doc: doc["cases"][0]["expected"].update(exit_code=99))
    reject("scenario", lambda doc: doc["cases"][0].update(scenario_ids=["NEGATIVE-SCENARIO"]))
    reject("oracle", lambda doc: doc["cases"][0].update(oracle_class="negative-oracle"))
    reject("exit", lambda doc: doc["cases"][0]["observed"].update(exit_code=99))
    reject("terminal", lambda doc: doc["cases"][0]["observed"].update(terminal_stage="usage"))
    reject("stage plan", lambda doc: doc["cases"][0]["stages"].pop())
    if len(results["cases"][0]["stages"]) > 1:
        reject("stage order", lambda doc: doc["cases"][0]["stages"].reverse())
    reject("unknown stage", lambda doc: doc["cases"][0]["stages"].append({"name": "synthetic-extra", "operation_status": "ok", "oracle_status": "pass"}))
    reject("required stage oracle", lambda doc: doc["cases"][0]["stages"][0].update(oracle_status="fail"))
    forbidden_mutation_case = next((
        item["case_id"] for item in results["cases"]
        if isinstance(item, dict) and not item.get("expected", {}).get("target_mutation_allowed", False)
    ), None)
    if forbidden_mutation_case:
        reject_case("target mutation", forbidden_mutation_case, lambda item: item["observed"].update(target_mutation_started=True))
    reject_case("backup no-op", "OPS-BK-SUCCESS-RUNNING", lambda item: item["backup_package"].update(files=[]))
    reject_case("restore package oracle mismatch", "OPS-RS-SUCCESS-RUNNING", lambda item: item["package_oracle"].update(marker_sha256="0" * 64))
    reject_case("restore app predicate", "OPS-RS-SUCCESS-RUNNING", lambda item: item["target_after"]["app_state"].update(logical="stopped", engine_status="exited", running=False))
    reject_case("wrong immutable remover", "OPS-LOCK-STALE-BREAK-OK", lambda item: item["lock_observations"][0].update(removed_by="owner"))
    reject_case("missing lock role observation", "OPS-BK-SUCCESS-RUNNING", lambda item: item.update(lock_observations=[]))
    reject_case("cleanup masking", "OPS-CLEANUP-RESIDUAL", lambda item: item["operation_cleanup"].update(result="pass", residual_resources=[]))
    reject_case("destructive after null", "OPS-RS-FAIL-DB-REPLACE", lambda item: item.update(target_after=None))
    reject("sentinel", lambda doc: doc["cases"][0].update(sentinel_unchanged=False))
    reject("suite cleanup", lambda doc: doc["suite_cleanup"].update(result="fail", residual_resources=["synthetic-project"]))
    return failures


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--catalog", required=True, type=Path)
    parser.add_argument("--results", required=True, type=Path)
    parser.add_argument("--self-test", action="store_true")
    args = parser.parse_args()
    try:
        catalog = yaml.safe_load(args.catalog.read_text(encoding="utf-8"))
        results = json.loads(args.results.read_text(encoding="utf-8"))
    except (OSError, yaml.YAMLError, json.JSONDecodeError) as exc:
        print(f"v1 ops results input error: {exc}", file=sys.stderr)
        return 1
    errors = validate(catalog, results, args.catalog)
    if args.self_test and not errors:
        errors.extend(negative_corpus(catalog, results, args.catalog))
    if errors:
        for item in errors:
            print(item, file=sys.stderr)
        return 1
    print(f"v1 ops results contract: passed ({len(results['cases'])} cases)")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
