#!/usr/bin/env bash
# CMD-005 / A16：范围守护——禁止产品 profile 泄漏进实现 diff（含未跟踪文件）。
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

base="$(git merge-base HEAD origin/develop 2>/dev/null || true)"
if [[ -z "$base" ]]; then
  base="$(git merge-base HEAD develop 2>/dev/null || true)"
fi
if [[ -z "$base" ]]; then
  base="$(git rev-parse HEAD)"
fi

tmp="$(mktemp)"
trap 'rm -f "$tmp"' EXIT
{
  git diff --name-only --diff-filter=d "${base}...HEAD" || true
  git ls-files --others --exclude-standard || true
} | sed '/^$/d' | sort -u >"$tmp"

patterns='account_profiles|account_profile_avatar_gc|/api/v1/account/profile|AccountCenterLayout|backend/internal/accountprofile|frontend/src/account'
scanned=0
hits=0

while IFS= read -r path; do
  case "$path" in
    .codestable/features/2026-08-02-avatar-media-safety-net/*|\
    .codestable/features/2026-08-02-account-profile-center/*|\
    .codestable/features/2026-08-02-account-privacy-security/*|\
    .codestable/features/2026-08-02-account-system-settings/*|\
    .codestable/features/2026-08-02-account-center-hardening/*|\
    .codestable/roadmap/account-center/*|\
    .codestable/requirements/account-center.md|\
    .codestable/requirements/VISION.md|\
    .codestable/requirements/adrs/004-avatar-object-store-as-binary-adjunct.md|\
    .codestable/goals/*|\
    .reasonix/*|\
    scripts/test-avatar-media-v1-restore-wipe.sh|\
    scripts/check-avatar-media-safety-net-scope.sh|\
    scripts/lib/testdata/avatar-media-golden/*|\
    backend/internal/avatarmedia/testdata/golden/*|\
    backend/internal/avatarmedia/key_test.go|\
    backend/internal/avatarmedia/local_test.go|\
    backend/internal/customer/avatarbackup/manifest_test.go|\
    .codestable/features/2026-08-02-avatar-media-safety-net/impl-notes.md)
      printf 'scope-allow path=%s\n' "$path"
      continue
      ;;
  esac
  [[ -f "$path" ]] || continue
  scanned=$((scanned + 1))
  if command -v rg >/dev/null 2>&1; then
    if rg -n -e "$patterns" -- "$path"; then
      hits=$((hits + 1))
    fi
  else
    if grep -nE "$patterns" -- "$path"; then
      hits=$((hits + 1))
    fi
  fi
done <"$tmp"

if [[ "$hits" -ne 0 ]]; then
  printf 'avatar-media-safety-net scope check: FAIL product profile leakage\n' >&2
  exit 1
fi
printf 'avatar-media-safety-net scope check: pass (%d files scanned)\n' "$scanned"
exit 0
