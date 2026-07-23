#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

python3 "$repo_root/scripts/v1_ops_contract.py" \
  --design "$repo_root/.codestable/features/2026-07-22-v1-hardening/v1-hardening-design.md" \
  --catalog "$repo_root/.codestable/features/2026-07-22-v1-hardening/v1-hardening-ops-case-catalog.yaml" \
  --self-test
