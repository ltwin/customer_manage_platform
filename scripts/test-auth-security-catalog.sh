#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
work_dir="$(mktemp -d)"
trap 'rm -rf "$work_dir"' EXIT
chmod 700 "$work_dir"

contains_forbidden_output() {
  local log_file="$1"
  local forbidden
  for forbidden in \
    'postgres://' \
    'DATABASE_URL=' \
    'AUTH_TOKEN_SECRET=' \
    'RESEND_API_KEY=' \
    'Authorization' \
    'Bearer ' \
    'Set-Cookie' \
    'token=' \
    '#token' \
    'provider_body' \
    'response_body'
  do
    if grep -F -q "$forbidden" "$log_file"; then
      return 0
    fi
  done
  if grep -E -q \
    '([[:alnum:]._%+-]+@[[:alnum:].-]+\.[[:alpha:]]{2,}|eyJ[[:alnum:]_-]*\.[[:alnum:]_-]+\.[[:alnum:]_-]+|[0-9a-f]{32}\.[[:alnum:]_-]{40,})' \
    "$log_file"; then
    return 0
  fi
  return 1
}

run_case() {
  local name="$1"
  shift
  local log_file="$work_dir/$name.log"
  if ! "$@" >"$log_file" 2>&1; then
    printf 'auth security catalog failed: %s\n' "$name" >&2
    if contains_forbidden_output "$log_file"; then
      printf 'captured log withheld: forbidden material detected\n' >&2
    else
      sed -n '1,120p' "$log_file" >&2
    fi
    return 1
  fi
  if contains_forbidden_output "$log_file"; then
    printf 'auth security catalog failed: %s-output-redaction\n' "$name" >&2
    return 1
  fi
	printf 'auth-security/%s=passed\n' "$name"
	: >"$work_dir/case-$name.passed"
}

run_backend_go() {
	local expected_count="$1"
	shift
	local pattern="$1"
	shift
	(
		cd "$repo_root/backend"
		local listed
		listed="$(go test -list "$pattern" "$@")"
		local matched_count
		matched_count="$(printf '%s\n' "$listed" | grep -c '^Test' || true)"
		if [[ "$matched_count" -ne "$expected_count" ]]; then
			printf 'auth security catalog expected %s tests but matched %s\n' "$expected_count" "$matched_count" >&2
			return 1
		fi
		go test -p=1 -run "$pattern" -count=1 -parallel=1 "$@"
	)
}

run_frontend() {
  (
    cd "$repo_root/frontend"
    npm run test:auth
    npm run test:api-client
    npm run build
  )
}

run_generate_check() {
  (cd "$repo_root" && make generate-check)
}

run_repository_script() {
	"$repo_root/$1"
}

verify_browser_evidence() {
	local evidence="$repo_root/.codestable/features/2026-07-30-public-auth-hardening/public-auth-hardening-implementation.md"
	[[ -f "$evidence" ]] || return 1
	grep -F -q 'Browser：1440×900验证desktop forgot／reset布局；375×812验证' "$evidence"
}

require_scenario() {
	local scenario="$1"
	shift
	local required_case
	for required_case in "$@"; do
		if [[ ! -f "$work_dir/case-$required_case.passed" ]]; then
			printf 'auth security catalog failed: A%s missing case %s\n' "$scenario" "$required_case" >&2
			return 1
		fi
	done
	printf 'auth-security/A%s=passed\n' "$scenario"
	: >"$work_dir/scenario-A$scenario.passed"
}

run_case password-session run_backend_go 2 \
  '^(TestBeginPasswordResetIsGenericAndResetTokensArePurposeBound|TestPasswordResetAndChangeRevokeAllRefreshFamilies)$' \
  ./internal/platform/store

run_case origin-cookie run_backend_go 1 \
  '^TestPasswordRoutesOriginCookieAndErrorMatrix$' \
  ./internal/platform/httpapi

run_case limiter-proxy run_backend_go 5 \
  '^(TestAttemptLimiterBudgetMatrixAndConcurrentConsume|TestAttemptLimiterPersistsBudgetAcrossInstancesAndHalfOpenWindow|TestResolveClientSourceUsesOnlyTrustedXFFChain|TestRateLimitedResponseAndEventExposeOnlyAllowlistedMetadata|TestAuthCapabilitiesAndRegistrationGate)$' \
  ./internal/platform/store ./internal/platform/httpapi

run_case timing-enumeration run_backend_go 2 \
  '^(TestLoginUsesOneCostEquivalentCompareAndResponseBudget|TestPasswordResetMailBranchesMeetStatisticalResponseBudget)$' \
  ./internal/platform/auth

run_case api-contract run_generate_check
run_case frontend-auth run_frontend
run_case browser-evidence verify_browser_evidence

run_case event-redaction run_backend_go 6 \
  '^(TestEventProtocolCoversSevenNamesWithOnlyAllowlistedFields|TestEventProtocolRejectsUnknownFailureAndRawSource|TestAuthStructuredEventsUseAllowlistAndRedactedReferences|TestResendAcceptsMailWithIdempotencyAndRedactedEvent|TestResendFailureEventUsesFixedAllowlist|TestSinkAcceptsWithoutLoggingRecipientOrActionURL)$' \
  ./internal/platform/authevent ./internal/platform/httpapi ./internal/platform/authmail

run_case monitor run_backend_go 4 \
  '^(TestAuthMonitorThresholdBoundaries|TestAuthMonitorExitMatrixAndMalformedLineIsolation|TestAuthMonitorSupportsFileAndJournaldAdapters|TestAuthMonitorRejectsUnknownEventFieldsAndReadFailuresWithoutEcho)$' \
  ./cmd/accountctl

run_case production-preflight run_repository_script scripts/test-production-preflight.sh
run_case rotation-rollback run_repository_script scripts/test-auth-rotation-rollback.sh

run_case two-account-isolation run_backend_go 1 \
  '^TestAccountAccessHTTPPostgresE2EAndFullIsolation$' \
  ./internal/platform/httpapi

run_case account-scope-consumers run_backend_go 6 \
  '^(TestAvatarMaintenanceA23ThreePageRestartSingleFlightDueLimitAndBackoff|TestScanRunnerOncePerLocalDay|TestBindingServiceRejectsGroupTamperExpiredAndCrossAccountChat|TestChatAccountResolverReturnsZeroOneManyAndDBError|TestDeliverySenderRunnerAccountFailureDoesNotStarveOtherAccounts|TestPostgresSnapshotRepositoryLoadsNarrowDigestReadModel)$' \
  ./internal/customer ./internal/reminder ./internal/reminder/digest

require_scenario 1 password-session timing-enumeration event-redaction
require_scenario 2 password-session
require_scenario 3 password-session origin-cookie
require_scenario 4 password-session origin-cookie frontend-auth event-redaction
require_scenario 5 origin-cookie
require_scenario 6 limiter-proxy
require_scenario 7 limiter-proxy origin-cookie
require_scenario 8 limiter-proxy event-redaction
require_scenario 9 limiter-proxy event-redaction
require_scenario 10 timing-enumeration
require_scenario 11 api-contract frontend-auth browser-evidence
require_scenario 12 password-session frontend-auth origin-cookie
require_scenario 13 event-redaction
require_scenario 14 monitor
require_scenario 15 monitor
require_scenario 16 production-preflight
require_scenario 17 rotation-rollback
require_scenario 18 two-account-isolation account-scope-consumers

printf '%s\n' 'AUTH_SECURITY_CATALOG {"version":1,"status":"passed","synthetic":true,"scenarios":["A1","A2","A3","A4","A5","A6","A7","A8","A9","A10","A11","A12","A13","A14","A15","A16","A17","A18"],"production_effect":false}'
printf '%s\n' 'auth security catalog: passed'
