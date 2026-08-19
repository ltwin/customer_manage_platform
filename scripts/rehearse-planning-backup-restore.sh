#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
config=
evidence=
while (($#)); do
  case "$1" in
    --config) (($# >= 2)) || exit 2; config="$2"; shift 2 ;;
    --evidence) (($# >= 2)) || exit 2; evidence="$2"; shift 2 ;;
    *) exit 2 ;;
  esac
done
[[ -n "$config" && -n "$evidence" ]] || exit 2
PYTHONPATH="$repo_root/scripts/lib" python3 -m planninghardening.rehearsal \
  --config "$config" --evidence "$evidence"
