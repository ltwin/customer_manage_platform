#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
evidence=
json_output=0
while (($#)); do
  case "$1" in
    --evidence) (($# >= 2)) || exit 2; evidence="$2"; shift 2 ;;
    --json) json_output=1; shift ;;
    *) exit 2 ;;
  esac
done
[[ -n "$evidence" ]] || exit 2
if [[ "$json_output" -eq 1 ]]; then
  PYTHONPATH="$repo_root/scripts/lib" python3 -m planninghardening.release --evidence "$evidence"
else
  PYTHONPATH="$repo_root/scripts/lib" python3 -m planninghardening.release --evidence "$evidence" >/dev/null
  printf 'planning v1 release readiness: passed\n'
fi
