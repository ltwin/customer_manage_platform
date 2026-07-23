#!/usr/bin/env python3
"""Validate the frozen v1-hardening ops catalog without executing operations."""

from __future__ import annotations

import argparse
import copy
import re
import sys
from collections import Counter
from pathlib import Path
from typing import Any

import yaml


ORACLE = {
    "PO": "pre-mutation-readonly",
    "PM": "pre-mutation-reject",
    "BR": "backup-readonly",
    "RS": "restore-success",
    "RF": "restore-destructive-failure",
    "CO": "cleanup-only",
}
ENGINE = {
    "F": "forbidden",
    "R": "readonly",
    "L": "lock-metadata",
    "T": "target-operation",
}
CLEANUP = {
    "N": [],
    "L": ["lock-containers", "ops-helpers"],
    "P": ["lock-containers", "ops-helpers", "private-staging"],
    "S": ["lock-containers", "ops-helpers", "private-staging", "temp-package"],
}
EXPECTED_FIELDS = {
    "exit_code",
    "terminal_stage",
    "stage_plan",
    "engine_call_policy",
    "target_mutation_allowed",
    "required_lock_roles",
    "cleanup_objects",
    "forbidden_side_effects",
    "backup_package_expectation",
    "operation_cleanup_expected",
    "allowed_operation_removers",
}
CASE_FIELDS = {"case_id", "scenario_ids", "fixture", "action", "oracle_class", "expected"}


def remover_for(role: str) -> str:
    if role == "stale-candidate":
        return "authorized-breaker"
    if role in {"owner", "new-owner", "winner", "old-owner", "invalid-candidate"}:
        return "owner"
    return "none"


def parse_inventory(design_text: str) -> dict[str, dict[str, Any]]:
    inventory: dict[str, dict[str, Any]] = {}
    for line in design_text.splitlines():
        if not line.startswith("| `OPS-"):
            continue
        cells = [cell.strip() for cell in line.strip().strip("|").split("|")]
        case_ids = re.findall(r"`(OPS-[^`]+)`", cells[0])
        scenarios = [item.strip() for item in cells[1].split(",")]
        oracle, exit_code, terminal, plan, engine, mutation = cells[2:8]
        roles_text, cleanup_code, forbidden_code = cells[8:11]
        roles = (
            []
            if roles_text == "`[]`"
            else [item.strip() for item in roles_text.strip("`[]").split(",")]
        )
        package = (
            "published-valid"
            if plan in {"BKR", "BKE"}
            else "not-published-clean"
            if plan == "BKF"
            else "none"
        )
        expected = {
            "exit_code": int(exit_code),
            "terminal_stage": terminal,
            "stage_plan": plan,
            "engine_call_policy": ENGINE[engine],
            "target_mutation_allowed": mutation == "1",
            "required_lock_roles": roles,
            "cleanup_objects": CLEANUP[cleanup_code],
            "forbidden_side_effects": ["command-marker"] if forbidden_code == "X" else [],
            "backup_package_expectation": package,
            "operation_cleanup_expected": "fail" if plan == "COR" else "pass",
            "allowed_operation_removers": {role: remover_for(role) for role in roles},
        }
        for case_id in case_ids:
            inventory[case_id] = {
                "scenario_ids": scenarios,
                "oracle_class": ORACLE[oracle],
                "expected": expected,
            }
    return inventory


def validate_replay_text(case_id: str, field: str, value: Any) -> list[str]:
    errors: list[str] = []
    if not isinstance(value, str) or not value.strip():
        return [f"{case_id}: {field} must be a non-empty string"]
    lowered = value.casefold()
    if any(marker in lowered for marker in ("tbd", "同上", "see other case", "参见其他 case")):
        errors.append(f"{case_id}: {field} contains a placeholder or cross-case reference")
    if case_id not in value:
        errors.append(f"{case_id}: {field} must name its own case id")
    if field == "fixture" and not all(
        token in value
        for token in ("deployment_mode=", "selector=", "app_engine_state=", "target_or_package=", "fault_or_race_setup=")
    ):
        errors.append(f"{case_id}: fixture does not materialize all replay dimensions")
    if field == "action" and "./scripts/" not in value:
        errors.append(f"{case_id}: action must name the invoked script entry")
    return errors


def validate_catalog_data(catalog: Any, inventory: dict[str, dict[str, Any]]) -> list[str]:
    errors: list[str] = []
    if not isinstance(catalog, dict):
        return ["catalog must be a mapping"]
    if catalog.get("schema_version") != 1:
        errors.append("catalog schema_version must be 1")
    if catalog.get("feature") != "2026-07-22-v1-hardening":
        errors.append("catalog feature identity mismatch")
    cases = catalog.get("cases")
    if not isinstance(cases, list):
        return errors + ["catalog cases must be a list"]

    ids = [case.get("case_id") for case in cases if isinstance(case, dict)]
    counts = Counter(ids)
    duplicates = sorted(case_id for case_id, count in counts.items() if count > 1)
    if duplicates:
        errors.append(f"duplicate case ids: {duplicates}")
    required = set(inventory)
    observed = {case_id for case_id in ids if isinstance(case_id, str)}
    if missing := sorted(required - observed):
        errors.append(f"missing case ids: {missing}")
    if unknown := sorted(observed - required):
        errors.append(f"unknown case ids: {unknown}")

    memberships: Counter[str] = Counter()
    for case in cases:
        if not isinstance(case, dict):
            errors.append("each case must be a mapping")
            continue
        case_id = case.get("case_id")
        if not isinstance(case_id, str) or case_id not in inventory:
            continue
        if set(case) != CASE_FIELDS:
            errors.append(f"{case_id}: case fields must equal {sorted(CASE_FIELDS)}")
        truth = inventory[case_id]
        for field in ("scenario_ids", "oracle_class", "expected"):
            if case.get(field) != truth[field]:
                errors.append(f"{case_id}: {field} differs from frozen inventory")
        expected = case.get("expected")
        if isinstance(expected, dict) and set(expected) != EXPECTED_FIELDS:
            errors.append(f"{case_id}: expected fields must equal {sorted(EXPECTED_FIELDS)}")
        errors.extend(validate_replay_text(case_id, "fixture", case.get("fixture")))
        errors.extend(validate_replay_text(case_id, "action", case.get("action")))
        scenarios = case.get("scenario_ids")
        if isinstance(scenarios, list):
            memberships.update(item for item in scenarios if isinstance(item, str))

    required_memberships = Counter({"A18": 66, "A19": 2, "A20": 41, "A21": 2, "A22": 39})
    if memberships != required_memberships:
        errors.append(
            f"scenario membership mismatch: observed={dict(memberships)} expected={dict(required_memberships)}"
        )
    if len(cases) != 148:
        errors.append(f"catalog must contain 148 cases, observed {len(cases)}")
    return errors


def assert_negative_corpus(catalog: dict[str, Any], inventory: dict[str, dict[str, Any]]) -> list[str]:
    failures: list[str] = []

    def must_reject(name: str, mutate: Any) -> None:
        candidate = copy.deepcopy(catalog)
        mutate(candidate)
        if not validate_catalog_data(candidate, inventory):
            failures.append(f"negative corpus was accepted: {name}")

    must_reject("missing", lambda doc: doc["cases"].pop())
    must_reject("unknown", lambda doc: doc["cases"].append({**doc["cases"][0], "case_id": "OPS-UNKNOWN"}))
    must_reject("duplicate", lambda doc: doc["cases"].append(copy.deepcopy(doc["cases"][0])))
    must_reject("scenario mismatch", lambda doc: doc["cases"][0].update(scenario_ids=["A22"]))
    must_reject("oracle mismatch", lambda doc: doc["cases"][0].update(oracle_class="cleanup-only"))
    must_reject("expected mismatch", lambda doc: doc["cases"][0]["expected"].update(exit_code=99))
    must_reject("fixture placeholder", lambda doc: doc["cases"][0].update(fixture="TBD"))
    must_reject("action cross reference", lambda doc: doc["cases"][0].update(action="see other case"))
    return failures


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--catalog", required=True, type=Path)
    parser.add_argument("--design", required=True, type=Path)
    parser.add_argument("--self-test", action="store_true")
    args = parser.parse_args()

    try:
        inventory = parse_inventory(args.design.read_text())
        catalog = yaml.safe_load(args.catalog.read_text())
    except (OSError, yaml.YAMLError) as error:
        print(f"v1 ops contract input error: {error}", file=sys.stderr)
        return 1

    if len(inventory) != 148:
        print(f"frozen design inventory must contain 148 unique cases, observed {len(inventory)}", file=sys.stderr)
        return 1
    errors = validate_catalog_data(catalog, inventory)
    if args.self_test and not errors:
        errors.extend(assert_negative_corpus(catalog, inventory))
    if errors:
        for error in errors:
            print(error, file=sys.stderr)
        return 1
    print("v1 ops catalog contract: passed (148 cases; negative corpus rejected)")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
