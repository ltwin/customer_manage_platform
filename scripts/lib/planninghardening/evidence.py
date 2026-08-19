#!/usr/bin/env python3
"""Strict stage report, sample decision and evidence-index validation."""

from __future__ import annotations

import argparse
import hashlib
import math
import statistics
from datetime import datetime, timezone
from pathlib import Path
from typing import Any

from planningbackup.package import ContractError, canonical_json, load_json, sha256_file


SAMPLE_REASONS = {
    "eligible", "non_production", "not_creative", "not_completed", "unknown_execution_window",
    "duplicate_shoot", "not_shared_eligible", "outside_window", "superseded_finalization", "invalid_source_fact",
}
HEX_DIGITS = frozenset("0123456789abcdef")
INTERVAL_METHOD = "wilson-score-95-v1"
INTERVAL_CONFIDENCE = 0.95
INTERVAL_Z = 1.959963984540054


def _is_non_empty_string(value: Any) -> bool:
    return isinstance(value, str) and bool(value)


def _is_non_negative_int(value: Any) -> bool:
    return isinstance(value, int) and not isinstance(value, bool) and value >= 0


def _is_sha256(value: Any) -> bool:
    return isinstance(value, str) and len(value) == 64 and all(character in HEX_DIGITS for character in value)


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


def _unique_strings(value: Any, key: str, *, non_empty: bool = False) -> list[str]:
    if not isinstance(value, list) or (non_empty and not value):
        raise ContractError(key)
    if not all(_is_non_empty_string(item) for item in value):
        raise ContractError(key)
    if len(set(value)) != len(value):
        raise ContractError(key)
    return value


def _validate_exclusions(value: Any, identity_key: str, key: str) -> None:
    if not isinstance(value, list):
        raise ContractError(key)
    seen_reasons: set[str] = set()
    for exclusion in value:
        row = _expect_keys(exclusion, {"reason", "count", identity_key}, key)
        reason = row["reason"]
        identities = _unique_strings(row[identity_key], key, non_empty=True)
        if not _is_non_empty_string(reason) or reason in seen_reasons:
            raise ContractError(key)
        if not _is_non_negative_int(row["count"]) or row["count"] != len(identities):
            raise ContractError(key)
        seen_reasons.add(reason)


def _wilson_interval(successes: int, total: int) -> tuple[float, float] | None:
    if total == 0:
        return None
    proportion = successes / total
    denominator = 1 + (INTERVAL_Z * INTERVAL_Z / total)
    center = (proportion + INTERVAL_Z * INTERVAL_Z / (2 * total)) / denominator
    margin = (
        INTERVAL_Z
        * math.sqrt((proportion * (1 - proportion) / total) + (INTERVAL_Z * INTERVAL_Z / (4 * total * total)))
        / denominator
    )
    return max(0.0, center - margin), min(1.0, center + margin)


def _validate_interval(value: Any, successes: int, total: int, key: str) -> None:
    expected = _wilson_interval(successes, total)
    if expected is None:
        if value is not None:
            raise ContractError(key)
        return
    interval = _expect_keys(value, {"method", "confidence_level", "lower", "upper"}, key)
    if interval["method"] != INTERVAL_METHOD or interval["confidence_level"] != INTERVAL_CONFIDENCE:
        raise ContractError(key)
    lower = interval["lower"]
    upper = interval["upper"]
    if any(isinstance(item, bool) or not isinstance(item, (int, float)) for item in (lower, upper)):
        raise ContractError(key)
    if not (0 <= lower <= upper <= 1):
        raise ContractError(key)
    if not math.isclose(lower, expected[0], rel_tol=0, abs_tol=1e-12) or not math.isclose(upper, expected[1], rel_tol=0, abs_tol=1e-12):
        raise ContractError(key)


def validate_cohort_comparison(
    value: Any,
    *,
    comparable_keys: set[str] | None = None,
    stage2_keys: set[str] | None = None,
    require_shared: bool = False,
) -> dict[str, Any]:
    comparison = _expect_keys(value, {"shared_keys", "stage_1_only", "stage_2_only", "limitations"}, "evidence-cohort-comparison")
    shared = set(_unique_strings(comparison["shared_keys"], "evidence-cohort-shared", non_empty=require_shared))
    stage1_only = set(_unique_strings(comparison["stage_1_only"], "evidence-cohort-stage1-only"))
    stage2_only = set(_unique_strings(comparison["stage_2_only"], "evidence-cohort-stage2-only"))
    _unique_strings(comparison["limitations"], "evidence-cohort-limitations", non_empty=True)
    if shared & stage1_only or shared & stage2_only or stage1_only & stage2_only:
        raise ContractError("evidence-cohort-overlap")
    if comparable_keys is not None and shared != comparable_keys:
        raise ContractError("evidence-cohort-comparable-binding")
    if stage2_keys is not None and shared | stage2_only != stage2_keys:
        raise ContractError("evidence-cohort-stage2-binding")
    return comparison


def validate_sample_decisions(path: Path) -> dict[str, Any]:
    document = load_json(path)
    if not isinstance(document, dict) or set(document) != {"schema", "decision_id", "decision_revision", "cohort", "decisions"}:
        raise ContractError("sample-decision-schema")
    if document["schema"] != "planning-observation-sample-decision-v1" or not isinstance(document["cohort"], str) or document["cohort"] not in ("stage_1_g1_g2", "stage_2_g3"):
        raise ContractError("sample-decision-version")
    if not _is_non_empty_string(document["decision_id"]):
        raise ContractError("sample-decision-id")
    if isinstance(document["decision_revision"], bool) or not isinstance(document["decision_revision"], int) or document["decision_revision"] < 1 or not isinstance(document["decisions"], list):
        raise ContractError("sample-decision-revision")
    seen: set[str] = set()
    for decision in document["decisions"]:
        fields = {"candidate_ref", "canonical_shoot_key", "decision", "reason", "source_fact_refs", "decided_at", "reviewer_ref"}
        if not isinstance(decision, dict) or set(decision) != fields:
            raise ContractError("sample-decision-row")
        key = decision["canonical_shoot_key"]
        if not isinstance(key, str) or not key or key in seen:
            raise ContractError("sample-decision-duplicate")
        seen.add(key)
        if not _is_non_empty_string(decision["candidate_ref"]) or not _is_non_empty_string(decision["decided_at"]):
            raise ContractError("sample-decision-identity")
        _utc_timestamp(decision["decided_at"], "sample-decision-time")
        if not isinstance(decision["decision"], str) or decision["decision"] not in ("eligible", "excluded"):
            raise ContractError("sample-decision-reason")
        if not isinstance(decision["reason"], str) or decision["reason"] not in SAMPLE_REASONS:
            raise ContractError("sample-decision-reason")
        if (decision["decision"] == "eligible") != (decision["reason"] == "eligible"):
            raise ContractError("sample-decision-disposition-reason")
        if not isinstance(decision["source_fact_refs"], list) or not decision["source_fact_refs"]:
            raise ContractError("sample-decision-facts")
        seen_facts: set[tuple[str, str, int]] = set()
        for fact in decision["source_fact_refs"]:
            base_fields = {"kind", "id", "revision"}
            if not isinstance(fact, dict) or set(fact) not in (base_fields, base_fields | {"sha256"}):
                raise ContractError("sample-decision-fact-row")
            if not _is_non_empty_string(fact["kind"]) or not _is_non_empty_string(fact["id"]):
                raise ContractError("sample-decision-fact-value")
            if isinstance(fact["revision"], bool) or not isinstance(fact["revision"], int) or fact["revision"] < 1:
                raise ContractError("sample-decision-fact-value")
            identity = (fact["kind"], fact["id"], fact["revision"])
            if identity in seen_facts or ("sha256" in fact and not _is_sha256(fact["sha256"])):
                raise ContractError("sample-decision-fact-value")
            seen_facts.add(identity)
        if not isinstance(decision["reviewer_ref"], str) or not decision["reviewer_ref"]:
            raise ContractError("sample-decision-reviewer")
    return document


def _expect_keys(value: Any, fields: set[str], key: str) -> dict[str, Any]:
    if not isinstance(value, dict) or set(value) != fields:
        raise ContractError(key)
    return value


def validate_stage1(path: Path) -> dict[str, Any]:
    document = load_json(path)
    fields = {"gate_version", "status", "stale", "window_id", "build_revision", "sample_decisions", "cohort_complete", "g1", "g2", "exclusions", "rule_versions", "projection_sha256"}
    report = _expect_keys(document, fields, "stage1-schema")
    if report["gate_version"] != "planning-evidence-stage1-v1" or not isinstance(report["status"], str) or report["status"] not in ("insufficient", "failed", "passed") or not isinstance(report["stale"], bool):
        raise ContractError("stage1-header")
    if not isinstance(report["window_id"], str) or not report["window_id"] or not isinstance(report["build_revision"], str) or not report["build_revision"] or not isinstance(report["cohort_complete"], bool):
        raise ContractError("stage1-identity")
    decisions = report["sample_decisions"]
    if not isinstance(decisions, list):
        raise ContractError("stage1-decisions")
    includes: set[str] = set()
    seen_decisions: set[str] = set()
    for row in decisions:
        if not isinstance(row, dict) or not {"plan_id", "disposition", "owner_review_ref"}.issubset(row) or set(row) - {"plan_id", "disposition", "owner_review_ref", "exclusion_reason"}:
            raise ContractError("stage1-decision-row")
        plan_id = row["plan_id"]
        if not isinstance(plan_id, str) or not plan_id or plan_id in seen_decisions or not isinstance(row["disposition"], str) or row["disposition"] not in ("include", "exclude") or not isinstance(row["owner_review_ref"], str) or not row["owner_review_ref"]:
            raise ContractError("stage1-decision-value")
        seen_decisions.add(plan_id)
        if row["disposition"] == "include":
            if row.get("exclusion_reason") not in (None, ""):
                raise ContractError("stage1-include-reason")
            includes.add(plan_id)
        elif not isinstance(row.get("exclusion_reason"), str) or not row.get("exclusion_reason"):
            raise ContractError("stage1-exclusion-reason")
    g1 = _expect_keys(report["g1"], {"distinct_live_plan_count", "eligible_plan_count", "required_sample_count", "threshold", "live_plan_ids", "threshold_met"}, "stage1-g1-schema")
    live_ids = _unique_strings(g1["live_plan_ids"], "stage1-g1-live-ids")
    if not set(live_ids).issubset(includes):
        raise ContractError("stage1-g1-live-ids")
    for key in ("distinct_live_plan_count", "eligible_plan_count", "required_sample_count", "threshold"):
        if isinstance(g1[key], bool) or not isinstance(g1[key], int) or g1[key] < 0:
            raise ContractError("stage1-g1-number")
    if not isinstance(g1["threshold_met"], bool) or g1["distinct_live_plan_count"] != len(live_ids) or g1["eligible_plan_count"] != len(includes) or g1["required_sample_count"] != 5 or g1["threshold"] != 3:
        raise ContractError("stage1-g1-counts")
    if g1["threshold_met"] != (bool(report["cohort_complete"]) and len(live_ids) >= g1["threshold"]):
        raise ContractError("stage1-g1-threshold")
    g2 = _expect_keys(report["g2"], {"successful_plan_count", "required_sample_count", "active_seconds_samples", "median_active_seconds", "median_threshold_seconds", "median_threshold_met", "abandoned_plan_count", "terminal_plan_count", "abandon_ratio"}, "stage1-g2-schema")
    samples = g2["active_seconds_samples"]
    if not isinstance(samples, list):
        raise ContractError("stage1-g2-samples")
    values = []
    seen_samples: set[str] = set()
    for sample in samples:
        row = _expect_keys(sample, {"plan_id", "active_seconds"}, "stage1-g2-sample")
        if not _is_non_empty_string(row["plan_id"]) or row["plan_id"] in seen_samples or row["plan_id"] not in includes or not _is_non_negative_int(row["active_seconds"]):
            raise ContractError("stage1-g2-sample-value")
        seen_samples.add(row["plan_id"])
        values.append(row["active_seconds"])
    median = statistics.median(values) if values else None
    for key in ("successful_plan_count", "required_sample_count", "median_threshold_seconds", "abandoned_plan_count", "terminal_plan_count"):
        if isinstance(g2[key], bool) or not isinstance(g2[key], int) or g2[key] < 0:
            raise ContractError("stage1-g2-number")
    reported_median = g2["median_active_seconds"]
    if reported_median is not None and (isinstance(reported_median, bool) or not isinstance(reported_median, (int, float))):
        raise ContractError("stage1-g2-median")
    if not isinstance(g2["median_threshold_met"], bool) or g2["successful_plan_count"] != len(values) or g2["required_sample_count"] != 5 or reported_median != median:
        raise ContractError("stage1-g2-counts")
    terminal = g2["terminal_plan_count"]
    abandoned = g2["abandoned_plan_count"]
    if not isinstance(terminal, int) or not isinstance(abandoned, int) or terminal < 0 or abandoned < 0 or abandoned > terminal or terminal != len(values) + abandoned:
        raise ContractError("stage1-g2-terminal")
    expected_ratio = (abandoned / terminal) if terminal else None
    reported_ratio = g2["abandon_ratio"]
    if reported_ratio is not None and (isinstance(reported_ratio, bool) or not isinstance(reported_ratio, (int, float))):
        raise ContractError("stage1-g2-ratio")
    if reported_ratio != expected_ratio or g2["median_threshold_seconds"] != 600:
        raise ContractError("stage1-g2-ratio")
    if g2["median_threshold_met"] != (bool(report["cohort_complete"]) and median is not None and median <= 600):
        raise ContractError("stage1-g2-threshold")
    _validate_exclusions(report["exclusions"], "plan_ids", "stage1-exclusions")
    rules = _expect_keys(report["rule_versions"], {"evidence_projector", "active_time", "run_capture_mode", "idle_rules"}, "stage1-rule-versions")
    if rules["evidence_projector"] != "planning-evidence-stage1-v1" or not _is_non_empty_string(rules["active_time"]) or not _is_non_empty_string(rules["run_capture_mode"]):
        raise ContractError("stage1-rule-version-value")
    idle_rules = rules["idle_rules"]
    if not isinstance(idle_rules, list) or any(isinstance(item, bool) or not isinstance(item, int) or item < 1 for item in idle_rules) or len(set(idle_rules)) != len(idle_rules):
        raise ContractError("stage1-rule-version-value")
    if not _is_sha256(report["projection_sha256"]):
        raise ContractError("stage1-projection-hash")
    cohort_complete = len(decisions) == 5 and len(includes) == 5 and terminal == 5
    if report["cohort_complete"] is not cohort_complete:
        raise ContractError("stage1-cohort-complete")
    expected_status = "insufficient"
    if cohort_complete:
        expected_status = "passed" if g1["threshold_met"] and g2["median_threshold_met"] and not report["stale"] else "failed"
    if report["status"] != expected_status:
        raise ContractError("stage1-status-derived")
    return report


def validate_stage2(path: Path) -> dict[str, Any]:
    document = load_json(path)
    fields = {
        "gate_version", "status", "stale", "window_id", "build_revision", "sample_decisions",
        "cohort_complete", "g3", "exclusions", "rule_versions", "projection_sha256",
    }
    report = _expect_keys(document, fields, "stage2-schema")
    if report["gate_version"] != "planning-evidence-stage2-v1" or not isinstance(report["status"], str) or report["status"] not in ("insufficient", "failed", "passed") or not isinstance(report["stale"], bool):
        raise ContractError("stage2-header")
    if not isinstance(report["window_id"], str) or not report["window_id"] or not isinstance(report["build_revision"], str) or not report["build_revision"] or not isinstance(report["cohort_complete"], bool):
        raise ContractError("stage2-identity")
    if not isinstance(report["sample_decisions"], list):
        raise ContractError("stage2-decisions")
    included: set[str] = set()
    seen: set[str] = set()
    for decision in report["sample_decisions"]:
        required = {"shoot_key", "disposition", "owner_review_ref"}
        if not isinstance(decision, dict) or not required.issubset(decision) or set(decision) - required - {"exclusion_reason"}:
            raise ContractError("stage2-decision-row")
        shoot_key = decision["shoot_key"]
        if not isinstance(shoot_key, str) or not shoot_key or shoot_key in seen or not isinstance(decision["disposition"], str) or decision["disposition"] not in ("include", "exclude") or not isinstance(decision["owner_review_ref"], str) or not decision["owner_review_ref"]:
            raise ContractError("stage2-decision-value")
        seen.add(shoot_key)
        if decision["disposition"] == "include":
            if decision.get("exclusion_reason") not in (None, ""):
                raise ContractError("stage2-include-reason")
            included.add(shoot_key)
        elif not isinstance(decision.get("exclusion_reason"), str) or not decision["exclusion_reason"]:
            raise ContractError("stage2-exclusion-reason")

    g3_fields = {
        "eligible_shared_shoot_count", "required_sample_count", "interacted_shared_shoot_count",
        "interaction_rate", "interaction_threshold", "interaction_threshold_met",
        "baseline_preparation_missing_shot_count", "baseline_finalized_shot_count", "baseline_preparation_missing_rate",
        "shared_preparation_missing_shot_count", "shared_finalized_shot_count", "shared_preparation_missing_rate",
        "interaction_rate_interval", "baseline_preparation_missing_rate_interval", "shared_preparation_missing_rate_interval",
        "preparation_missing_rate_decreased", "preparation_missing_absolute_rate_decrease", "comparable_subset", "limitations",
    }
    g3 = _expect_keys(report["g3"], g3_fields, "stage2-g3-schema")
    integer_fields = (
        "eligible_shared_shoot_count", "required_sample_count", "interacted_shared_shoot_count",
        "baseline_preparation_missing_shot_count", "baseline_finalized_shot_count",
        "shared_preparation_missing_shot_count", "shared_finalized_shot_count",
    )
    if any(isinstance(g3[key], bool) or not isinstance(g3[key], int) or g3[key] < 0 for key in integer_fields):
        raise ContractError("stage2-g3-number")
    if (
        g3["eligible_shared_shoot_count"] != len(included)
        or g3["required_sample_count"] != 5
        or g3["interacted_shared_shoot_count"] > len(included)
        or g3["baseline_preparation_missing_shot_count"] > g3["baseline_finalized_shot_count"]
        or g3["shared_preparation_missing_shot_count"] > g3["shared_finalized_shot_count"]
    ):
        raise ContractError("stage2-g3-counts")
    expected_interaction_rate = g3["interacted_shared_shoot_count"] / len(included) if included else None
    expected_baseline_rate = g3["baseline_preparation_missing_shot_count"] / g3["baseline_finalized_shot_count"] if g3["baseline_finalized_shot_count"] else None
    expected_shared_rate = g3["shared_preparation_missing_shot_count"] / g3["shared_finalized_shot_count"] if g3["shared_finalized_shot_count"] else None
    reported_rates = (
        g3["interaction_rate"], g3["interaction_threshold"],
        g3["baseline_preparation_missing_rate"], g3["shared_preparation_missing_rate"],
    )
    if any(item is not None and (isinstance(item, bool) or not isinstance(item, (int, float))) for item in reported_rates):
        raise ContractError("stage2-g3-rates")
    if g3["interaction_rate"] != expected_interaction_rate or g3["interaction_threshold"] != 0.4 or g3["baseline_preparation_missing_rate"] != expected_baseline_rate or g3["shared_preparation_missing_rate"] != expected_shared_rate:
        raise ContractError("stage2-g3-rates")
    _validate_interval(g3["interaction_rate_interval"], g3["interacted_shared_shoot_count"], len(included), "stage2-g3-interaction-interval")
    _validate_interval(g3["baseline_preparation_missing_rate_interval"], g3["baseline_preparation_missing_shot_count"], g3["baseline_finalized_shot_count"], "stage2-g3-baseline-interval")
    _validate_interval(g3["shared_preparation_missing_rate_interval"], g3["shared_preparation_missing_shot_count"], g3["shared_finalized_shot_count"], "stage2-g3-shared-interval")
    interaction_met = bool(report["cohort_complete"] and expected_interaction_rate is not None and expected_interaction_rate >= 0.4)
    missing_decreased = bool(report["cohort_complete"] and expected_baseline_rate is not None and expected_shared_rate is not None and expected_shared_rate < expected_baseline_rate)
    if g3["interaction_threshold_met"] is not interaction_met or g3["preparation_missing_rate_decreased"] is not missing_decreased:
        raise ContractError("stage2-g3-thresholds")
    expected_decrease = expected_baseline_rate - expected_shared_rate if expected_baseline_rate is not None and expected_shared_rate is not None else None
    reported_decrease = g3["preparation_missing_absolute_rate_decrease"]
    if reported_decrease is not None and (isinstance(reported_decrease, bool) or not isinstance(reported_decrease, (int, float))):
        raise ContractError("stage2-g3-absolute-decrease")
    if reported_decrease != expected_decrease:
        raise ContractError("stage2-g3-absolute-decrease")

    comparable = _expect_keys(g3["comparable_subset"], {
        "shoot_keys", "baseline_preparation_missing_shot_count", "baseline_finalized_shot_count",
        "baseline_preparation_missing_rate", "baseline_preparation_missing_rate_interval",
        "shared_preparation_missing_shot_count", "shared_finalized_shot_count",
        "shared_preparation_missing_rate", "shared_preparation_missing_rate_interval", "absolute_rate_decrease",
    }, "stage2-g3-comparable-schema")
    comparable_keys = set(_unique_strings(comparable["shoot_keys"], "stage2-g3-comparable-keys", non_empty=bool(report["cohort_complete"])))
    if not comparable_keys.issubset(included):
        raise ContractError("stage2-g3-comparable-keys")
    comparable_integer_fields = (
        "baseline_preparation_missing_shot_count", "baseline_finalized_shot_count",
        "shared_preparation_missing_shot_count", "shared_finalized_shot_count",
    )
    if any(not _is_non_negative_int(comparable[key]) for key in comparable_integer_fields):
        raise ContractError("stage2-g3-comparable-counts")
    if comparable["baseline_preparation_missing_shot_count"] > comparable["baseline_finalized_shot_count"] or comparable["shared_preparation_missing_shot_count"] > comparable["shared_finalized_shot_count"]:
        raise ContractError("stage2-g3-comparable-counts")
    comparable_baseline_rate = comparable["baseline_preparation_missing_shot_count"] / comparable["baseline_finalized_shot_count"] if comparable["baseline_finalized_shot_count"] else None
    comparable_shared_rate = comparable["shared_preparation_missing_shot_count"] / comparable["shared_finalized_shot_count"] if comparable["shared_finalized_shot_count"] else None
    comparable_decrease = comparable_baseline_rate - comparable_shared_rate if comparable_baseline_rate is not None and comparable_shared_rate is not None else None
    _validate_interval(comparable["baseline_preparation_missing_rate_interval"], comparable["baseline_preparation_missing_shot_count"], comparable["baseline_finalized_shot_count"], "stage2-g3-comparable-baseline-interval")
    _validate_interval(comparable["shared_preparation_missing_rate_interval"], comparable["shared_preparation_missing_shot_count"], comparable["shared_finalized_shot_count"], "stage2-g3-comparable-shared-interval")
    for key, expected in (
        ("baseline_preparation_missing_rate", comparable_baseline_rate),
        ("shared_preparation_missing_rate", comparable_shared_rate),
        ("absolute_rate_decrease", comparable_decrease),
    ):
        actual = comparable[key]
        if actual is not None and (isinstance(actual, bool) or not isinstance(actual, (int, float))):
            raise ContractError("stage2-g3-comparable-rates")
        if actual != expected:
            raise ContractError("stage2-g3-comparable-rates")

    _unique_strings(g3["limitations"], "stage2-g3-limitations", non_empty=True)
    _validate_exclusions(report["exclusions"], "shoot_keys", "stage2-exclusions")
    rules = _expect_keys(report["rule_versions"], {"evidence_projector", "interaction_deduplication", "completion_revision", "small_sample_interval"}, "stage2-rule-versions")
    if rules["evidence_projector"] != "planning-evidence-stage2-v1" or rules["small_sample_interval"] != INTERVAL_METHOD or not _is_non_empty_string(rules["interaction_deduplication"]) or not _is_non_empty_string(rules["completion_revision"]):
        raise ContractError("stage2-rule-version-value")
    if not _is_sha256(report["projection_sha256"]):
        raise ContractError("stage2-projection-hash")
    cohort_complete = len(included) == 5
    if report["cohort_complete"] is not cohort_complete:
        raise ContractError("stage2-cohort-complete")
    comparable_decreased = comparable_decrease is not None and comparable_decrease > 0
    expected_status = "insufficient"
    if cohort_complete:
        expected_status = "passed" if interaction_met and missing_decreased and comparable_decreased and not report["stale"] else "failed"
    if report["status"] != expected_status:
        raise ContractError("stage2-status-derived")
    return report


def build_evidence_index(
    stage1: Path | None,
    stage2: Path | None,
    decisions: Path | None,
    approval: dict[str, Any] | None = None,
    cohort_comparison: dict[str, Any] | None = None,
) -> dict[str, Any]:
    stage1_result: dict[str, Any]
    stage2_result: dict[str, Any]
    window_refs: list[str] = []
    if stage1 is None:
        stage1_result = {"status": "blocked", "stale": True}
    else:
        report = validate_stage1(stage1)
        stage1_result = {"json_path": str(stage1), "sha256": sha256_file(stage1), "gate_version": report["gate_version"], "stale": report["stale"], "status": report["status"]}
        window_refs.append(report["window_id"])
    stage2_report: dict[str, Any] | None = None
    if stage2 is None:
        stage2_result = {"status": "blocked", "stale": True, "reason": "stage-2-producer-pending-owner-evidence"}
    else:
        stage2_report = validate_stage2(stage2)
        stage2_result = {"json_path": str(stage2), "sha256": sha256_file(stage2), "gate_version": stage2_report["gate_version"], "stale": stage2_report["stale"], "status": stage2_report["status"]}
        window_refs.append(stage2_report["window_id"])
    decision_digest = None
    if decisions is not None:
        document = validate_sample_decisions(decisions)
        decision_digest = hashlib.sha256(canonical_json(document)).hexdigest()
    overall = "passed"
    if stage1_result["status"] in ("blocked", "insufficient", "failed") or stage2_result["status"] != "passed":
        overall = "blocked" if stage2_result["status"] == "blocked" else "failed"
    approval_result = approval if approval is not None else {"stage-1-evidence-go": "pending", "stage-2-evidence-go": "pending"}
    approval_ready = True
    for name, stage in (("stage-1-evidence-go", stage1_result), ("stage-2-evidence-go", stage2_result)):
        binding = approval_result.get(name) if isinstance(approval_result, dict) else None
        if not isinstance(binding, dict) or set(binding) != {"status", "path", "sha256", "gate_version"}:
            approval_ready = False
            continue
        if binding["status"] != "approved" or binding["path"] != stage.get("json_path") or binding["sha256"] != stage.get("sha256") or binding["gate_version"] != stage.get("gate_version"):
            approval_ready = False
    if overall == "passed" and (not approval_ready or decision_digest is None):
        overall = "blocked"
    if stage2_report is None:
        comparison_result = {
            "shared_keys": [],
            "stage_1_only": [],
            "stage_2_only": [],
            "limitations": ["stage-2 owner evidence pending"],
        }
    else:
        included_keys = {
            decision["shoot_key"]
            for decision in stage2_report["sample_decisions"]
            if decision["disposition"] == "include"
        }
        comparable_keys = set(stage2_report["g3"]["comparable_subset"]["shoot_keys"])
        comparison_result = cohort_comparison if cohort_comparison is not None else {
            "shared_keys": sorted(comparable_keys),
            "stage_1_only": [],
            "stage_2_only": sorted(included_keys - comparable_keys),
            "limitations": ["reviewed cross-cohort comparison was not supplied"],
        }
        validate_cohort_comparison(
            comparison_result,
            comparable_keys=comparable_keys,
            stage2_keys=included_keys,
            require_shared=stage2_report["status"] == "passed",
        )
        if cohort_comparison is None and overall == "passed":
            overall = "blocked"
    return {
        "schema": "planning-v1-evidence-index-v1",
        "window_refs": window_refs,
        "stage_1": stage1_result,
        "stage_2": stage2_result,
        "sample_decision_digest": decision_digest,
        "cohort_comparison": comparison_result,
        "generated_at": "1970-01-01T00:00:00Z",
        "validator_version": "planninghardening-evidence-v1",
        "approval": approval_result,
        "overall_status": overall,
    }


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--stage1", type=Path)
    parser.add_argument("--stage2", type=Path)
    parser.add_argument("--decisions", type=Path)
    parser.add_argument("--cohort-comparison", type=Path)
    parser.add_argument("--output", type=Path)
    args = parser.parse_args(argv)
    comparison = load_json(args.cohort_comparison) if args.cohort_comparison else None
    result = build_evidence_index(args.stage1, args.stage2, args.decisions, cohort_comparison=comparison)
    encoded = canonical_json(result)
    if args.output:
        args.output.write_bytes(encoded)
    else:
        print(encoded.decode(), end="")
    return 0 if result["overall_status"] == "passed" else 9


if __name__ == "__main__":
    raise SystemExit(main())
