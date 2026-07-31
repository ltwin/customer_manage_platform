---
doc_type: feature-evidence-pack
feature: 2026-07-30-public-auth-hardening
status: generated
---

# 2026-07-30-public-auth-hardening evidence pack

## 1. Scope

- Design: `.codestable/features/2026-07-30-public-auth-hardening/public-auth-hardening-design.md`
- Checklist: `.codestable/features/2026-07-30-public-auth-hardening/public-auth-hardening-checklist.yaml`

## 2. DoD Results

```json
{
  "gate_id": "dod-runner",
  "stage": "implementation.before_review",
  "status": "passed",
  "blocking": [],
  "warnings": [],
  "evidence": [
    {
      "command": "cd backend && go test -p=1 ./internal/platform/auth/... ./internal/platform/store/... ./internal/platform/httpapi/... -count=1 -parallel=1",
      "exit_code": 0,
      "stdout": "ok  \tgithub.com/samson/customer-manage-platform/backend/internal/platform/auth\t1.136s\nok  \tgithub.com/samson/customer-manage-platform/backend/internal/platform/store\t34.785s\nok  \tgithub.com/samson/customer-manage-platform/backend/internal/platform/httpapi\t28.257s\n",
      "stderr": "",
      "id": "CMD-001",
      "core": true,
      "failure_handling": "fix-or-block"
    },
    {
      "command": "cd backend && go test -p=1 ./cmd/accountctl/... -count=1 -parallel=1",
      "exit_code": 0,
      "stdout": "ok  \tgithub.com/samson/customer-manage-platform/backend/cmd/accountctl\t0.618s\n",
      "stderr": "",
      "id": "CMD-002",
      "core": true,
      "failure_handling": "fix-or-block"
    },
    {
      "command": "cd frontend && npm run test:auth && npm run test:api-client && npm run build",
      "exit_code": 0,
      "stdout": "\n> frontend@0.0.0 test:auth\n> node --test --experimental-transform-types scripts/auth.test.ts\n\n(node:60518) ExperimentalWarning: Transform Types is an experimental feature and might change at any time\n(Use `node --trace-warnings ...` to show where the warning was created)\n✔ startup restore moves restoring to authenticated through refresh then /me without storage (9.574541ms)\n✔ failed startup refresh becomes anonymous and keeps access out of storage (0.143333ms)\n✔ JSON, multipart, avatar Blob, and export share one refresh flight and replay once (8.0105ms)\n✔ a late 401 from the old access generation replays without opening a second refresh flight (0.39325ms)\n✔ a late account-A 401 cannot replay its payload with account-B credentials (0.395458ms)\n✔ logout invalidates an in-flight refresh before it can restore authentication (1.879834ms)\n✔ a late auth action cannot overwrite the newer session action (0.410333ms)\n✔ network, 403, 429, and 5xx failures never start refresh (0.319ms)\n✔ a replayed 401 does not refresh twice and returns to anonymous (0.185375ms)\n✔ verification action token is consumed from fragment and removed before use (0.1455ms)\n✔ auth routes and pages are mounted without the legacy storage token contract (2.260916ms)\nℹ tests 11\nℹ suites 0\nℹ pass 11\nℹ fail 0\nℹ cancelled 0\nℹ skipped 0\nℹ todo 0\nℹ duration_ms 110.073792\n\n> frontend@0.0.0 test:api-client\n> node --test --experimental-transform-types scripts/api-client.test.ts\n\n(node:60540) ExperimentalWarning: Transform Types is an experimental feature and might change at any time\n(Use `node --trace-warnings ...` to show where the warning was created)\n✔ ApiError preserves generated typed details (10.114666ms)\n✔ password recovery clients follow public/protected transport boundaries (0.882209ms)\n✔ wrong current password never refreshes and replays the credential mutation (0.253167ms)\n✔ order client sends schedulable_at and Idempotency-Key (0.339667ms)\n✔ schedule client sends range and per-attempt key (0.253458ms)\n✔ a protected 401 refreshes once and becomes anonymous when refresh also fails (0.294083ms)\nℹ tests 6\nℹ suites 0\nℹ pass 6\nℹ fail 0\nℹ cancelled 0\nℹ skipped 0\nℹ todo 0\nℹ duration_ms 94.830167\n\n> frontend@0.0.0 build\n> tsc -b && vite build\n\nvite v8.1.3 building client environment for production...\n\u001b[2K\ntransforming...✓ 1856 modules transformed.\nrendering chunks...\ncomputing gzip size...\ndist/index.html                   0.75 kB │ gzip:   0.44 kB\ndist/assets/index-Cb4g1cod.css   65.29 kB │ gzip:  12.47 kB\ndist/assets/index-Bc4nMxWQ.js   616.00 kB │ gzip: 179.74 kB\n\n✓ built in 208ms\n",
      "stderr": "[plugin builtin:vite-reporter] \n(!) Some chunks are larger than 500 kB after minification. Consider:\n- Using dynamic import() to code-split the application\n- Use build.rolldownOptions.output.codeSplitting to improve chunking: https://rolldown.rs/reference/OutputOptions.codeSplitting\n- Adjust chunk size limit for this warning via build.chunkSizeWarningLimit.\n",
      "id": "CMD-003",
      "core": true,
      "failure_handling": "fix-or-block"
    },
    {
      "command": "make generate-check",
      "exit_code": 0,
      "stdout": "cd backend && go tool oapi-codegen -config oapi-codegen.yaml ../api/openapi.yaml\ncd frontend && npm run generate\n\n> frontend@0.0.0 generate\n> openapi-typescript ../api/openapi.yaml -o src/api/schema.d.ts\n\n✨ openapi-typescript 7.13.0\n🚀 ../api/openapi.yaml → src/api/schema.d.ts [62.2ms]\n",
      "stderr": "",
      "id": "CMD-004",
      "core": true,
      "failure_handling": "fix-or-block"
    },
    {
      "command": "./scripts/test-production-preflight.sh",
      "exit_code": 0,
      "stdout": "production preflight fixture tests: passed (48 cases)\n",
      "stderr": "",
      "id": "CMD-005",
      "core": true,
      "failure_handling": "fix-or-block"
    },
    {
      "command": "./scripts/test-auth-security-catalog.sh",
      "exit_code": 0,
      "stdout": "auth-security/password-session=passed\nauth-security/origin-cookie=passed\nauth-security/limiter-proxy=passed\nauth-security/timing-enumeration=passed\nauth-security/api-contract=passed\nauth-security/frontend-auth=passed\nauth-security/event-redaction=passed\nauth-security/monitor=passed\nauth-security/production-preflight=passed\nauth-security/rotation-rollback=passed\nauth-security/two-account-isolation=passed\nauth-security/account-scope-consumers=passed\nauth-security/A1=passed\nauth-security/A2=passed\nauth-security/A3=passed\nauth-security/A4=passed\nauth-security/A5=passed\nauth-security/A6=passed\nauth-security/A7=passed\nauth-security/A8=passed\nauth-security/A9=passed\nauth-security/A10=passed\nauth-security/A11=passed\nauth-security/A12=passed\nauth-security/A13=passed\nauth-security/A14=passed\nauth-security/A15=passed\nauth-security/A16=passed\nauth-security/A17=passed\nauth-security/A18=passed\nAUTH_SECURITY_CATALOG {\"version\":1,\"status\":\"passed\",\"synthetic\":true,\"scenarios\":[\"A1\",\"A2\",\"A3\",\"A4\",\"A5\",\"A6\",\"A7\",\"A8\",\"A9\",\"A10\",\"A11\",\"A12\",\"A13\",\"A14\",\"A15\",\"A16\",\"A17\",\"A18\"],\"production_effect\":false}\nauth security catalog: passed\n",
      "stderr": "",
      "id": "CMD-006",
      "core": true,
      "failure_handling": "fix-or-block"
    },
    {
      "command": "make check",
      "exit_code": 0,
      "stdout": "separate submit, IME composition, multiline, and cancel (0.085041ms)\n✔ note submission gate rejects a repeated submit until the active request settles (0.07175ms)\n✔ empty customer picker keyboard navigation never exposes an invalid active option (0.0915ms)\n✔ mobile calendar keeps write actions hidden while dense and long drawer content stays bounded (0.550042ms)\n✔ mobile shell keeps Customers and Calendar direct with safe-area and touch-size contracts (0.408959ms)\nℹ tests 9\nℹ suites 0\nℹ pass 9\nℹ fail 0\nℹ cancelled 0\nℹ skipped 0\nℹ todo 0\nℹ duration_ms 88.748375\n./scripts/test-auth-legacy-cutover.sh\nauth-legacy-check/accountctl=passed\nauth-legacy-check/legacy-access-jwt=passed\nauth-legacy-check/legacy-password-only-shape=passed\nauth-legacy-check/seed-startup-retired=passed\nauth-legacy-check/account-auth-admission=passed\nauth-legacy-check/rollback-harness=passed\nAUTH_LEGACY_HARNESS {\"report\":\"auth_legacy_cutover\",\"dry_run_state\":\"ready\",\"claim_state\":\"ready\",\"account_ref\":\"account:e9a4ed7a9f8f\",\"email_ref\":\"email:353587cfa45e\",\"account_id_preserved\":true,\"business_counts_preserved\":true,\"avatar_checksum_preserved\":true,\"legacy_unclaimed_count\":0,\"pending_claim_count\":0,\"active_claimed_count\":1}\nAUTH_LEGACY_HARNESS {\"report\":\"auth_legacy_rollback\",\"migration_checksum\":\"sha256:e0c4444d00eb085ea76d4e878ece8e0aa7a89521f868a41ed9d53ebe639d590b\",\"legacy_schema_version_before\":12,\"legacy_schema_version_after\":11,\"limiter_schema_rollback\":true,\"legacy_down_passed\":true,\"legacy_data_preserved\":true,\"new_style_down_blocked\":true,\"new_style_marker\":\"auth_schema_down_blocked_new_accounts\",\"new_style_account_preserved\":true,\"old_binary_degradation_boundary\":\"new_style_accounts_unavailable_to_old_binary\"}\nAUTH_LEGACY_CUTOVER_CHECK {\"report\":\"auth_legacy_cutover_checks\",\"ready\":true,\"checks\":[\"accountctl_contract\",\"legacy_password_only_shape_rejected\",\"legacy_access_jwt_rejected\",\"seed_startup_path_retired\",\"account_admission_concurrency\",\"repo_pinned_rollback\"]}\nauth legacy cutover fixture tests: passed\n./scripts/test-auth-security-catalog.sh\nauth-security/password-session=passed\nauth-security/origin-cookie=passed\nauth-security/limiter-proxy=passed\nauth-security/timing-enumeration=passed\nauth-security/api-contract=passed\nauth-security/frontend-auth=passed\nauth-security/event-redaction=passed\nauth-security/monitor=passed\nauth-security/production-preflight=passed\nauth-security/rotation-rollback=passed\nauth-security/two-account-isolation=passed\nauth-security/account-scope-consumers=passed\nauth-security/A1=passed\nauth-security/A2=passed\nauth-security/A3=passed\nauth-security/A4=passed\nauth-security/A5=passed\nauth-security/A6=passed\nauth-security/A7=passed\nauth-security/A8=passed\nauth-security/A9=passed\nauth-security/A10=passed\nauth-security/A11=passed\nauth-security/A12=passed\nauth-security/A13=passed\nauth-security/A14=passed\nauth-security/A15=passed\nauth-security/A16=passed\nauth-security/A17=passed\nauth-security/A18=passed\nAUTH_SECURITY_CATALOG {\"version\":1,\"status\":\"passed\",\"synthetic\":true,\"scenarios\":[\"A1\",\"A2\",\"A3\",\"A4\",\"A5\",\"A6\",\"A7\",\"A8\",\"A9\",\"A10\",\"A11\",\"A12\",\"A13\",\"A14\",\"A15\",\"A16\",\"A17\",\"A18\"],\"production_effect\":false}\nauth security catalog: passed\nbash ./scripts/test-v1-ops-common.sh\nv1 ops common fixture tests: passed (15 cases)\npython3 ./scripts/lib/v1-ops-package-selftest.py\nv1 ops package helper self-test: passed\nbash ./scripts/test-v1-ops-backup-restore-safety.sh\nv1 ops package helper self-test: passed\nstage/restore-failure-stop=pass\nstage/restore-failure-stop=pass\nv1 ops backup/restore safety test: passed\n./scripts/test-v1-ops-contract.sh\nv1 ops catalog contract: passed (148 cases; negative corpus rejected)\nv1 ops results contract: passed (148 cases)\ncd backend && go tool oapi-codegen -config oapi-codegen.yaml ../api/openapi.yaml\ncd frontend && npm run generate\n\n> frontend@0.0.0 generate\n> openapi-typescript ../api/openapi.yaml -o src/api/schema.d.ts\n\n✨ openapi-typescript 7.13.0\n🚀 ../api/openapi.yaml → src/api/schema.d.ts [58.3ms]\n",
      "stderr": "[plugin builtin:vite-reporter] \n(!) Some chunks are larger than 500 kB after minification. Consider:\n- Using dynamic import() to code-split the application\n- Use build.rolldownOptions.output.codeSplitting to improve chunking: https://rolldown.rs/reference/OutputOptions.codeSplitting\n- Adjust chunk size limit for this warning via build.chunkSizeWarningLimit.\n",
      "id": "CMD-007",
      "core": true,
      "failure_handling": "fix-or-block"
    }
  ],
  "providers": {},
  "feature": "2026-07-30-public-auth-hardening",
  "inputs": {
    "checklist": ".codestable/features/2026-07-30-public-auth-hardening/public-auth-hardening-checklist.yaml"
  },
  "input_digests": {
    "checklist": "b63fde7ea2e5c927d74d513ebc447818c27f36188b4d802c524f8031f828253b"
  }
}
```

## 3. Validation Commands

Extracted from checklist `dod.commands`; see DoD Results for command status.

## 4. Scope And Cleanliness

Design bytes: 23476
Checklist bytes: 10515

## 5. Residual Risks

- cleanliness marker TODO in .codestable/features/2026-07-30-public-auth-hardening/public-auth-hardening-checklist.yaml

## 6. Provider Signals

```json
{
  "archguard": {
    "status": "skipped",
    "reason": "archguard collection disabled",
    "warnings": []
  },
  "meta_cc": {
    "status": "skipped",
    "reason": "meta-cc collection disabled",
    "warnings": []
  }
}
```

## 7. Gate Results

```json
{
  "gate_id": "scope-gate",
  "stage": "implementation.before_review",
  "status": "passed",
  "blocking": [],
  "warnings": [
    "cleanliness marker TODO in .codestable/features/2026-07-30-public-auth-hardening/public-auth-hardening-checklist.yaml"
  ],
  "evidence": [
    {
      "changed_files": [
        ".codestable/features/2026-07-30-public-auth-hardening/public-auth-hardening-checklist.yaml",
        ".codestable/roadmap/self-service-account-system/goal-state.yaml",
        ".env.example",
        "Dockerfile",
        "Makefile",
        "README.md",
        "api/openapi.yaml",
        "backend/cmd/accountctl/main.go",
        "backend/cmd/accountctl/main_test.go",
        "backend/cmd/server/main.go",
        "backend/internal/customer/customer_test.go",
        "backend/internal/platform/auth/crypto.go",
        "backend/internal/platform/auth/crypto_test.go",
        "backend/internal/platform/auth/errors.go",
        "backend/internal/platform/auth/password.go",
        "backend/internal/platform/auth/service.go",
        "backend/internal/platform/auth/service_test.go",
        "backend/internal/platform/auth/token.go",
        "backend/internal/platform/auth/types.go",
        "backend/internal/platform/authmail/resend.go",
        "backend/internal/platform/authmail/resend_live_test.go",
        "backend/internal/platform/authmail/resend_test.go",
        "backend/internal/platform/config/config.go",
        "backend/internal/platform/config/config_test.go",
        "backend/internal/platform/httpapi/api.gen.go",
        "backend/internal/platform/httpapi/auth.go",
        "backend/internal/platform/httpapi/auth_e2e_isolation_test.go",
        "backend/internal/platform/httpapi/auth_events.go",
        "backend/internal/platform/httpapi/auth_test.go",
        "backend/internal/platform/httpapi/envelope.go",
        "backend/internal/platform/httpapi/router.go",
        "backend/internal/platform/store/auth_account.go",
        "backend/internal/platform/store/auth_application_test.go",
        "backend/internal/platform/store/auth_legacy_cutover_harness_test.go",
        "backend/internal/platform/store/auth_migration_test.go",
        "backend/internal/platform/store/store_test.go",
        "backend/internal/platform/store/telegram_digest_migration_test.go",
        "backend/internal/reminder/digest/repository_test.go",
        "backend/internal/reminder/service_test.go",
        "backend/oapi-codegen.yaml",
        "docker-compose.yml",
        "frontend/scripts/api-client.test.ts",
        "frontend/scripts/auth.test.ts",
        "frontend/src/App.tsx",
        "frontend/src/api/client.ts",
        "frontend/src/api/schema.d.ts",
        "frontend/src/api/transport.ts",
        "frontend/src/auth/session.ts",
        "frontend/src/components/AppShell.tsx",
        "frontend/src/index.css",
        "frontend/src/pages/SettingsPage.tsx",
        "frontend/src/pages/auth/LoginPage.tsx",
        "scripts/production-preflight.sh",
        "scripts/test-auth-legacy-cutover.sh",
        "scripts/test-production-preflight.sh",
        "scripts/test-v1-ops-common.sh",
        ".codestable/features/2026-07-30-public-auth-hardening/public-auth-hardening-implementation.md",
        ".codestable/features/2026-07-30-public-auth-hardening/public-auth-hardening-isolation-report.md",
        ".codestable/features/2026-07-30-public-auth-hardening/public-auth-hardening-monitor-report.md",
        ".codestable/features/2026-07-30-public-auth-hardening/public-auth-hardening-preflight-report.md",
        ".codestable/features/2026-07-30-public-auth-hardening/public-auth-hardening-rollback-catalog.md",
        ".codestable/features/2026-07-30-public-auth-hardening/public-auth-hardening-rotation-report.md",
        ".codestable/features/2026-07-30-public-auth-hardening/public-auth-hardening-security-catalog.md",
        "backend/cmd/accountctl/auth_event.go",
        "backend/cmd/accountctl/auth_monitor.go",
        "backend/cmd/accountctl/auth_monitor_source.go",
        "backend/cmd/accountctl/auth_monitor_test.go",
        "backend/cmd/accountctl/auth_readiness.go",
        "backend/internal/platform/auth/response_timing.go",
        "backend/internal/platform/auth/response_timing_test.go",
        "backend/internal/platform/authevent/event.go",
        "backend/internal/platform/authevent/event_test.go",
        "backend/internal/platform/httpapi/client_meta.go",
        "backend/internal/platform/httpapi/client_meta_test.go",
        "backend/internal/platform/store/auth_limiter.go",
        "backend/internal/platform/store/auth_limiter_internal_test.go",
        "backend/internal/platform/store/auth_limiter_test.go",
        "backend/internal/platform/store/auth_readiness.go",
        "backend/internal/platform/store/migrations/0013_auth_attempt_limiter.down.sql",
        "backend/internal/platform/store/migrations/0013_auth_attempt_limiter.up.sql",
        "docs/ops/auth-root-rotation.md",
        "frontend/src/pages/auth/ChangePasswordPage.tsx",
        "frontend/src/pages/auth/ForgotPasswordPage.tsx",
        "frontend/src/pages/auth/ResetPasswordPage.tsx",
        "scripts/lib/auth-preflight-evidence.py",
        "scripts/test-auth-rotation-rollback.sh",
        "scripts/test-auth-security-catalog.sh"
      ],
      "ignored_machine_artifacts": [],
      "allowed_prefixes": [
        ".codestable/features/2026-07-30-public-auth-hardening",
        ".codestable/features/2026-07-30-public-auth-hardening",
        ".codestable/roadmap/self-service-account-system/goal-state.yaml",
        ".env.example",
        "Dockerfile",
        "Makefile",
        "README.md",
        "api",
        "backend",
        "docker-compose.yml",
        "docs/ops",
        "frontend/scripts",
        "frontend/src",
        "scripts"
      ]
    }
  ],
  "providers": {},
  "feature": "2026-07-30-public-auth-hardening",
  "inputs": {
    "feature_dir": ".codestable/features/2026-07-30-public-auth-hardening"
  },
  "input_digests": {}
}
```
