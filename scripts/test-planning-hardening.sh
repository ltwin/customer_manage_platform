#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PYTHONPATH="$repo_root/scripts/lib" python3 -m planninghardening.selftest
PYTHONPATH="$repo_root/scripts/lib" python3 -m planninghardening.rehearsal_selftest
bash -n "$repo_root/scripts/rehearse-planning-backup-restore.sh" "$repo_root/scripts/verify-planning-v1-release-readiness.sh"
grep -Fq 'planninghardening.rehearsal' "$repo_root/scripts/rehearse-planning-backup-restore.sh"
! grep -Eq 'release|deploy|promotion|cutover' "$repo_root/scripts/rehearse-planning-backup-restore.sh"
tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT
set +e
PYTHONPATH="$repo_root/scripts/lib" python3 -m planninghardening.conformance --repo "$repo_root" --output "$tmp_dir/conformance.json"
conformance_code=$?
set -e
[[ "$conformance_code" -eq 0 ]] || exit "$conformance_code"
grep -Fq '"stage-2-evidence-go":"pending"' "$tmp_dir/conformance.json"
printf 'planning hardening validators: passed\n'
