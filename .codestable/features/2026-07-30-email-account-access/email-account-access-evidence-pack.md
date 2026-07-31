---
doc_type: feature-evidence-pack
feature: email-account-access
status: generated
---

# email-account-access 证据包

## 1. 范围

- 方案：`.codestable/features/2026-07-30-email-account-access/email-account-access-design.md`
- 执行清单：`.codestable/features/2026-07-30-email-account-access/email-account-access-checklist.yaml`

## 2. DoD 结果

```json
{
  "gate_id": "dod-runner",
  "stage": "implementation.before_review",
  "status": "passed",
  "blocking": [],
  "warnings": [],
  "evidence": [
    {
      "command": "cd backend && go test -p=1 ./internal/platform/auth/... ./internal/platform/store/... ./internal/platform/httpapi/... ./cmd/accountctl/... -count=1 -parallel=1",
      "exit_code": 0,
      "stdout": "ok  \tgithub.com/samson/customer-manage-platform/backend/internal/platform/auth\t0.381s\nok  \tgithub.com/samson/customer-manage-platform/backend/internal/platform/store\t27.993s\nok  \tgithub.com/samson/customer-manage-platform/backend/internal/platform/httpapi\t27.342s\nok  \tgithub.com/samson/customer-manage-platform/backend/cmd/accountctl\t0.267s\n",
      "stderr": "",
      "id": "CMD-001",
      "core": true,
      "failure_handling": "fix-or-block"
    },
    {
      "command": "cd frontend && npm run test:auth && npm run test:api-client && npm run build",
      "exit_code": 0,
      "stdout": "\n> frontend@0.0.0 test:auth\n> node --test --experimental-transform-types scripts/auth.test.ts\n\n(node:16753) ExperimentalWarning: Transform Types is an experimental feature and might change at any time\n(Use `node --trace-warnings ...` to show where the warning was created)\n✔ startup restore moves restoring to authenticated through refresh then /me without storage (16.998083ms)\n✔ failed startup refresh becomes anonymous and keeps access out of storage (0.136959ms)\n✔ JSON, multipart, avatar Blob, and export share one refresh flight and replay once (8.168583ms)\n✔ a late 401 from the old access generation replays without opening a second refresh flight (0.358625ms)\n✔ network, 403, 429, and 5xx failures never start refresh (0.407542ms)\n✔ a replayed 401 does not refresh twice and returns to anonymous (0.226ms)\n✔ verification action token is consumed from fragment and removed before use (0.095333ms)\n✔ auth routes and pages are mounted without the legacy storage token contract (1.388292ms)\nℹ tests 8\nℹ suites 0\nℹ pass 8\nℹ fail 0\nℹ cancelled 0\nℹ skipped 0\nℹ todo 0\nℹ duration_ms 129.412875\n\n> frontend@0.0.0 test:api-client\n> node --test --experimental-transform-types scripts/api-client.test.ts\n\n(node:16775) ExperimentalWarning: Transform Types is an experimental feature and might change at any time\n(Use `node --trace-warnings ...` to show where the warning was created)\n✔ ApiError preserves generated typed details (9.986167ms)\n✔ order client sends schedulable_at and Idempotency-Key (0.449ms)\n✔ schedule client sends range and per-attempt key (0.32625ms)\n✔ a protected 401 refreshes once and becomes anonymous when refresh also fails (0.36575ms)\nℹ tests 4\nℹ suites 0\nℹ pass 4\nℹ fail 0\nℹ cancelled 0\nℹ skipped 0\nℹ todo 0\nℹ duration_ms 100.723\n\n> frontend@0.0.0 build\n> tsc -b && vite build\n\nvite v8.1.3 building client environment for production...\n\u001b[2K\ntransforming...✓ 1853 modules transformed.\nrendering chunks...\ncomputing gzip size...\ndist/index.html                   0.75 kB │ gzip:   0.44 kB\ndist/assets/index-DGH3UqLY.css   65.12 kB │ gzip:  12.44 kB\ndist/assets/index--_VWqY1Y.js   604.69 kB │ gzip: 177.66 kB\n\n✓ built in 206ms\n",
      "stderr": "[plugin builtin:vite-reporter] \n(!) Some chunks are larger than 500 kB after minification. Consider:\n- Using dynamic import() to code-split the application\n- Use build.rolldownOptions.output.codeSplitting to improve chunking: https://rolldown.rs/reference/OutputOptions.codeSplitting\n- Adjust chunk size limit for this warning via build.chunkSizeWarningLimit.\n",
      "id": "CMD-002",
      "core": true,
      "failure_handling": "fix-or-block"
    },
    {
      "command": "make generate-check",
      "exit_code": 0,
      "stdout": "cd backend && go tool oapi-codegen -config oapi-codegen.yaml ../api/openapi.yaml\ncd frontend && npm run generate\n\n> frontend@0.0.0 generate\n> openapi-typescript ../api/openapi.yaml -o src/api/schema.d.ts\n\n✨ openapi-typescript 7.13.0\n🚀 ../api/openapi.yaml → src/api/schema.d.ts [57.5ms]\n",
      "stderr": "",
      "id": "CMD-003",
      "core": true,
      "failure_handling": "fix-or-block"
    },
    {
      "command": "./scripts/test-auth-legacy-cutover.sh",
      "exit_code": 0,
      "stdout": "auth-legacy-check/accountctl=passed\nauth-legacy-check/legacy-access-jwt=passed\nauth-legacy-check/legacy-password-only-shape=passed\nauth-legacy-check/seed-startup-retired=passed\nauth-legacy-check/account-auth-admission=passed\nauth-legacy-check/rollback-harness=passed\nAUTH_LEGACY_HARNESS {\"report\":\"auth_legacy_cutover\",\"dry_run_state\":\"ready\",\"claim_state\":\"ready\",\"account_ref\":\"account:e9a4ed7a9f8f\",\"email_ref\":\"email:353587cfa45e\",\"account_id_preserved\":true,\"business_counts_preserved\":true,\"avatar_checksum_preserved\":true,\"legacy_unclaimed_count\":0,\"pending_claim_count\":0,\"active_claimed_count\":1}\nAUTH_LEGACY_HARNESS {\"report\":\"auth_legacy_rollback\",\"migration_checksum\":\"sha256:e0c4444d00eb085ea76d4e878ece8e0aa7a89521f868a41ed9d53ebe639d590b\",\"legacy_schema_version_before\":12,\"legacy_schema_version_after\":11,\"legacy_down_passed\":true,\"legacy_data_preserved\":true,\"new_style_down_blocked\":true,\"new_style_marker\":\"auth_schema_down_blocked_new_accounts\",\"new_style_account_preserved\":true,\"old_binary_degradation_boundary\":\"new_style_accounts_unavailable_to_old_binary\"}\nAUTH_LEGACY_CUTOVER_CHECK {\"report\":\"auth_legacy_cutover_checks\",\"ready\":true,\"checks\":[\"accountctl_contract\",\"legacy_password_only_shape_rejected\",\"legacy_access_jwt_rejected\",\"seed_startup_path_retired\",\"account_admission_concurrency\",\"repo_pinned_rollback\"]}\nauth legacy cutover fixture tests: passed\n",
      "stderr": "",
      "id": "CMD-004",
      "core": true,
      "failure_handling": "fix-or-block"
    },
    {
      "command": "./scripts/test-production-preflight.sh",
      "exit_code": 0,
      "stdout": "production preflight fixture tests: passed (24 cases)\n",
      "stderr": "",
      "id": "CMD-005",
      "core": true,
      "failure_handling": "fix-or-block"
    },
    {
      "command": "make check",
      "exit_code": 0,
      "stdout": ".156583ms)\n✔ download collaboration triggers once and always releases its object URL (0.32575ms)\n✔ DataExportCard exposes loading, retry, 401, PII, avatar, and persistence-safe contracts (1.27025ms)\n✔ DataExportCard relies on native button keyboard behavior and shared mobile-safe styles (0.522042ms)\nℹ tests 8\nℹ suites 0\nℹ pass 8\nℹ fail 0\nℹ cancelled 0\nℹ skipped 0\nℹ todo 0\nℹ duration_ms 105.051167\ncd frontend && npm run test:v1-hardening\n\n> frontend@0.0.0 test:v1-hardening\n> node --test --experimental-transform-types scripts/v1-hardening.test.ts\n\n(node:22819) ExperimentalWarning: Transform Types is an experimental feature and might change at any time\n(Use `node --trace-warnings ...` to show where the warning was created)\n✔ page read states have one observable presentation in the fixed priority order (0.706125ms)\n✔ page-owned data becomes stale on refresh failure without becoming empty (0.0995ms)\n✔ initial failure and successful empty remain mutually exclusive (0.052292ms)\n✔ customer results keep one page-owned query across the 768/769 responsive boundary (1.124333ms)\n✔ note keyboard actions separate submit, IME composition, multiline, and cancel (0.070541ms)\n✔ note submission gate rejects a repeated submit until the active request settles (0.064375ms)\n✔ empty customer picker keyboard navigation never exposes an invalid active option (0.088542ms)\n✔ mobile calendar keeps write actions hidden while dense and long drawer content stays bounded (2.865042ms)\n✔ mobile shell keeps Customers and Calendar direct with safe-area and touch-size contracts (0.891417ms)\nℹ tests 9\nℹ suites 0\nℹ pass 9\nℹ fail 0\nℹ cancelled 0\nℹ skipped 0\nℹ todo 0\nℹ duration_ms 110.517208\n./scripts/test-auth-legacy-cutover.sh\nauth-legacy-check/accountctl=passed\nauth-legacy-check/legacy-access-jwt=passed\nauth-legacy-check/legacy-password-only-shape=passed\nauth-legacy-check/seed-startup-retired=passed\nauth-legacy-check/account-auth-admission=passed\nauth-legacy-check/rollback-harness=passed\nAUTH_LEGACY_HARNESS {\"report\":\"auth_legacy_cutover\",\"dry_run_state\":\"ready\",\"claim_state\":\"ready\",\"account_ref\":\"account:e9a4ed7a9f8f\",\"email_ref\":\"email:353587cfa45e\",\"account_id_preserved\":true,\"business_counts_preserved\":true,\"avatar_checksum_preserved\":true,\"legacy_unclaimed_count\":0,\"pending_claim_count\":0,\"active_claimed_count\":1}\nAUTH_LEGACY_HARNESS {\"report\":\"auth_legacy_rollback\",\"migration_checksum\":\"sha256:e0c4444d00eb085ea76d4e878ece8e0aa7a89521f868a41ed9d53ebe639d590b\",\"legacy_schema_version_before\":12,\"legacy_schema_version_after\":11,\"legacy_down_passed\":true,\"legacy_data_preserved\":true,\"new_style_down_blocked\":true,\"new_style_marker\":\"auth_schema_down_blocked_new_accounts\",\"new_style_account_preserved\":true,\"old_binary_degradation_boundary\":\"new_style_accounts_unavailable_to_old_binary\"}\nAUTH_LEGACY_CUTOVER_CHECK {\"report\":\"auth_legacy_cutover_checks\",\"ready\":true,\"checks\":[\"accountctl_contract\",\"legacy_password_only_shape_rejected\",\"legacy_access_jwt_rejected\",\"seed_startup_path_retired\",\"account_admission_concurrency\",\"repo_pinned_rollback\"]}\nauth legacy cutover fixture tests: passed\n./scripts/test-production-preflight.sh\nproduction preflight fixture tests: passed (24 cases)\nbash ./scripts/test-v1-ops-common.sh\nv1 ops common fixture tests: passed (15 cases)\npython3 ./scripts/lib/v1-ops-package-selftest.py\nv1 ops package helper self-test: passed\nbash ./scripts/test-v1-ops-backup-restore-safety.sh\nv1 ops package helper self-test: passed\nstage/restore-failure-stop=pass\nstage/restore-failure-stop=pass\nv1 ops backup/restore safety test: passed\n./scripts/test-v1-ops-contract.sh\nv1 ops catalog contract: passed (148 cases; negative corpus rejected)\nv1 ops results contract: passed (148 cases)\ncd backend && go tool oapi-codegen -config oapi-codegen.yaml ../api/openapi.yaml\ncd frontend && npm run generate\n\n> frontend@0.0.0 generate\n> openapi-typescript ../api/openapi.yaml -o src/api/schema.d.ts\n\n✨ openapi-typescript 7.13.0\n🚀 ../api/openapi.yaml → src/api/schema.d.ts [64.8ms]\n",
      "stderr": "[plugin builtin:vite-reporter] \n(!) Some chunks are larger than 500 kB after minification. Consider:\n- Using dynamic import() to code-split the application\n- Use build.rolldownOptions.output.codeSplitting to improve chunking: https://rolldown.rs/reference/OutputOptions.codeSplitting\n- Adjust chunk size limit for this warning via build.chunkSizeWarningLimit.\n",
      "id": "CMD-006",
      "core": true,
      "failure_handling": "fix-or-block"
    }
  ],
  "providers": {},
  "feature": "2026-07-30-email-account-access",
  "inputs": {
    "checklist": ".codestable/features/2026-07-30-email-account-access/email-account-access-checklist.yaml"
  },
  "input_digests": {
    "checklist": "c0b7d24be3aa1d6ea581a07f01c87c542ba2a889df854ab2deb92bf2890113c8"
  }
}
```

## 3. 验证命令

命令取自执行清单 `dod.commands`；执行状态见上方 DoD 结果。

## 4. 范围与清洁度

方案字节数：44874
执行清单字节数：11883

## 5. 残余风险

- scope-gate 在 email-account-access 执行清单的规范文字中命中 `TODO`。
- scope-gate 在 email-account-access 执行清单的规范文字中命中 `FIXME`。
- scope-gate 在 public-auth-hardening 执行清单的规范文字中命中 `TODO`。
- 以上均是“禁止临时 TODO／FIXME”的清洁度规则文本，不是源码施工痕迹；不构成核心缺口。

## 6. Provider 信号

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

## 7. Gate 结果

```json
{
  "gate_id": "scope-gate",
  "stage": "implementation.before_review",
  "status": "passed",
  "blocking": [],
  "warnings": [
    "cleanliness marker TODO in .codestable/features/2026-07-30-email-account-access/email-account-access-checklist.yaml",
    "cleanliness marker FIXME in .codestable/features/2026-07-30-email-account-access/email-account-access-checklist.yaml",
    "cleanliness marker TODO in .codestable/features/2026-07-30-public-auth-hardening/public-auth-hardening-checklist.yaml"
  ],
  "evidence": [
    {
      "changed_files": [
        ".codestable/requirements/CONTEXT.md",
        ".codestable/requirements/VISION.md",
        ".env.example",
        "Makefile",
        "README.md",
        "api/openapi.yaml",
        "backend/cmd/server/main.go",
        "backend/cmd/server/main_test.go",
        "backend/internal/customer/customer_test.go",
        "backend/internal/platform/auth/password.go",
        "backend/internal/platform/auth/service.go",
        "backend/internal/platform/auth/token.go",
        "backend/internal/platform/config/config.go",
        "backend/internal/platform/config/config_test.go",
        "backend/internal/platform/httpapi/api.gen.go",
        "backend/internal/platform/httpapi/auth.go",
        "backend/internal/platform/httpapi/auth_test.go",
        "backend/internal/platform/httpapi/envelope.go",
        "backend/internal/platform/httpapi/router.go",
        "backend/internal/platform/store/account.go",
        "backend/internal/platform/store/export_test.go",
        "backend/internal/platform/store/store.go",
        "backend/internal/platform/store/store_test.go",
        "backend/internal/platform/store/telegram_digest_migration_test.go",
        "backend/internal/reminder/digest/binding_test.go",
        "backend/internal/reminder/digest/repository_test.go",
        "backend/internal/reminder/service_test.go",
        "backend/oapi-codegen.yaml",
        "frontend/package.json",
        "frontend/scripts/api-client.test.ts",
        "frontend/scripts/customer-avatar.test.ts",
        "frontend/scripts/data-export.test.ts",
        "frontend/scripts/prototype-contract.test.mjs",
        "frontend/src/App.tsx",
        "frontend/src/api/client.ts",
        "frontend/src/api/schema.d.ts",
        "frontend/src/auth/token.ts",
        "frontend/src/components/customers/customerAvatarMedia.ts",
        "frontend/src/index.css",
        "frontend/src/pages/LoginPage.css",
        "frontend/src/pages/LoginPage.tsx",
        "frontend/src/pages/WelcomePage.css",
        "frontend/src/pages/WelcomePage.tsx",
        "scripts/production-preflight.sh",
        "scripts/test-production-preflight.sh",
        "scripts/test-v1-ops-common.sh",
        ".codestable/brainstorms/self-service-account-system/approval-report.md",
        ".codestable/brainstorms/self-service-account-system/brainstorm.md",
        ".codestable/features/2026-07-30-email-account-access/email-account-access-checklist.yaml",
        ".codestable/features/2026-07-30-email-account-access/email-account-access-design-review.md",
        ".codestable/features/2026-07-30-email-account-access/email-account-access-design.md",
        ".codestable/features/2026-07-30-email-account-access/email-account-access-evidence-pack.md",
        ".codestable/features/2026-07-30-email-account-access/email-account-access-implementation.md",
        ".codestable/features/2026-07-30-email-account-access/email-account-access-mail-checkpoint.md",
        ".codestable/features/2026-07-30-public-auth-hardening/public-auth-hardening-checklist.yaml",
        ".codestable/features/2026-07-30-public-auth-hardening/public-auth-hardening-design-review.md",
        ".codestable/features/2026-07-30-public-auth-hardening/public-auth-hardening-design.md",
        ".codestable/requirements/adrs/005-separate-account-identity-and-credential.md",
        ".codestable/requirements/adrs/006-short-lived-access-and-rotating-refresh-sessions.md",
        ".codestable/requirements/adrs/007-claim-legacy-seed-account-in-place.md",
        ".codestable/requirements/self-service-account-system.md",
        ".codestable/roadmap/self-service-account-system/approval-report.md",
        ".codestable/roadmap/self-service-account-system/goal-features/email-account-access.md",
        ".codestable/roadmap/self-service-account-system/goal-features/public-auth-hardening.md",
        ".codestable/roadmap/self-service-account-system/goal-plan.md",
        ".codestable/roadmap/self-service-account-system/goal-protocol-audit.md",
        ".codestable/roadmap/self-service-account-system/goal-protocol-feature-loop.md",
        ".codestable/roadmap/self-service-account-system/goal-protocol-gates.md",
        ".codestable/roadmap/self-service-account-system/goal-protocol.md",
        ".codestable/roadmap/self-service-account-system/goal-state.yaml",
        ".codestable/roadmap/self-service-account-system/self-service-account-system-items.yaml",
        ".codestable/roadmap/self-service-account-system/self-service-account-system-roadmap-review.md",
        ".codestable/roadmap/self-service-account-system/self-service-account-system-roadmap.md",
        "backend/cmd/accountctl/main.go",
        "backend/cmd/accountctl/main_test.go",
        "backend/internal/platform/auth/crypto.go",
        "backend/internal/platform/auth/crypto_test.go",
        "backend/internal/platform/auth/errors.go",
        "backend/internal/platform/auth/mail.go",
        "backend/internal/platform/auth/service_test.go",
        "backend/internal/platform/auth/token_test.go",
        "backend/internal/platform/auth/types.go",
        "backend/internal/platform/authmail/resend.go",
        "backend/internal/platform/authmail/resend_live_test.go",
        "backend/internal/platform/authmail/resend_test.go",
        "backend/internal/platform/authmail/sink.go",
        "backend/internal/platform/authmail/sink_test.go",
        "backend/internal/platform/httpapi/auth_e2e_isolation_test.go",
        "backend/internal/platform/httpapi/auth_events.go",
        "backend/internal/platform/store/auth_account.go",
        "backend/internal/platform/store/auth_application_test.go",
        "backend/internal/platform/store/auth_legacy_cutover_harness_test.go",
        "backend/internal/platform/store/auth_migration_test.go",
        "backend/internal/platform/store/auth_session.go",
        "backend/internal/platform/store/migrations/0012_account_auth.down.sql",
        "backend/internal/platform/store/migrations/0012_account_auth.up.sql",
        "frontend/scripts/auth.test.ts",
        "frontend/src/api/transport.ts",
        "frontend/src/auth/actionToken.ts",
        "frontend/src/auth/session.ts",
        "frontend/src/pages/auth/AuthPageShell.tsx",
        "frontend/src/pages/auth/LoginPage.css",
        "frontend/src/pages/auth/LoginPage.tsx",
        "frontend/src/pages/auth/RegisterPage.tsx",
        "frontend/src/pages/auth/VerifyEmailPage.tsx",
        "scripts/test-auth-legacy-cutover.sh"
      ],
      "ignored_machine_artifacts": [
        ".codestable/features/2026-07-30-email-account-access/email-account-access-dod-results.json",
        ".codestable/features/2026-07-30-email-account-access/email-account-access-evidence-pack-results.json",
        ".codestable/features/2026-07-30-email-account-access/email-account-access-gate-results.json"
      ],
      "allowed_prefixes": [
        ".codestable/features/2026-07-30-email-account-access",
        ".codestable/brainstorms/self-service-account-system",
        ".codestable/features/2026-07-30-email-account-access",
        ".codestable/features/2026-07-30-public-auth-hardening",
        ".codestable/requirements",
        ".codestable/roadmap/self-service-account-system",
        ".env.example",
        "Makefile",
        "README.md",
        "api",
        "backend",
        "frontend",
        "scripts"
      ]
    }
  ],
  "providers": {},
  "feature": "2026-07-30-email-account-access",
  "inputs": {
    "feature_dir": ".codestable/features/2026-07-30-email-account-access"
  },
  "input_digests": {}
}
```
