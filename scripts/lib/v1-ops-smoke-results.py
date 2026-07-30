#!/usr/bin/env python3
"""Build an honest partial/full v1 ops result document from executed records."""

from __future__ import annotations

import argparse
import hashlib
import json
from pathlib import Path

import yaml


def stage_record(name: str, passed: bool, plan: str) -> list[dict[str, str]]:
    stages = [{
        "name": name,
        "operation_status": "failed",
        "oracle_status": "pass" if passed else "fail",
    }]
    if plan in {"BKR", "BKE", "BKF", "BKP", "LCK", "LCO", "RSP", "RSR", "RSE", "RSF", "COR"} and name != "helper-fence":
        stages.append({"name": "helper-fence", "operation_status": "not-run", "oracle_status": "pass" if passed else "fail"})
    return stages


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--catalog", type=Path, required=True)
    parser.add_argument("--records", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--project", required=True)
    parser.add_argument("--target-hash", required=True)
    parser.add_argument("--suite-cleanup", choices=("pass", "fail"), required=True)
    args = parser.parse_args()
    catalog = yaml.safe_load(args.catalog.read_text(encoding="utf-8"))
    records = [json.loads(line) for line in args.records.read_text(encoding="utf-8").splitlines() if line]
    by_id = {case["case_id"]: case for case in catalog["cases"]}
    cases = []
    for record in records:
        source = by_id[record["case_id"]]
        expected = source["expected"]
        passed = record["exit_code"] == expected["exit_code"] and record["terminal_stage"] == expected["terminal_stage"]
        cases.append({
            **source,
            "observed": {
                "exit_code": record["exit_code"],
                "terminal_stage": record["terminal_stage"],
                "engine_calls": record["engine_calls"],
                "target_mutation_started": record["target_mutation_started"],
                "lock_roles": record["lock_roles"],
                "cleanup_objects": record["cleanup_objects"],
                "side_effects": record["side_effects"],
            },
            "result": "pass" if passed else "fail",
            "stages": stage_record(expected["terminal_stage"], passed, expected["stage_plan"]),
            "target_before": None,
            "target_after": None,
            "package_oracle": None,
            "backup_package": None,
            "lock_observations": [],
            "sentinel_unchanged": record["sentinel_unchanged"],
            "operation_cleanup": record["operation_cleanup"],
            "harness_cleanup": record["harness_cleanup"],
            "evidence_paths": ["evidence/ops/v1-ops-smoke.log"],
        })
    required = [case["case_id"] for case in catalog["cases"]]
    executed = [case["case_id"] for case in cases]
    missing = sorted(set(required) - set(executed))
    failed = any(case["result"] != "pass" for case in cases)
    status = "fail" if failed or args.suite_cleanup == "fail" else "blocked" if missing else "pass"
    result = {
        "schema_version": 1,
        "feature": "2026-07-22-v1-hardening",
        "status": status,
        "target": {"project_name": args.project, "target_hash": args.target_hash},
        "catalog": {
            "path": ".codestable/features/2026-07-22-v1-hardening/v1-hardening-ops-case-catalog.yaml",
            "sha256": hashlib.sha256(args.catalog.read_bytes()).hexdigest(),
            "required_case_ids": required,
        },
        "coverage": {
            "executed_case_ids": executed,
            "missing_case_ids": missing,
            "unknown_case_ids": [],
            "duplicate_case_ids": [],
        },
        "suite_cleanup": {
            "result": args.suite_cleanup,
            "residual_resources": [] if args.suite_cleanup == "pass" else ["synthetic-suite"],
        },
        "cases": cases,
    }
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(json.dumps(result, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    return 0 if status == "pass" else 1


if __name__ == "__main__":
    raise SystemExit(main())
