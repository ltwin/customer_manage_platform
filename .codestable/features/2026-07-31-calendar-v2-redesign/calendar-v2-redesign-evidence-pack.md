---
doc_type: feature-evidence-pack
feature: 2026-07-31-calendar-v2-redesign
status: generated
---

# Calendar v2 redesign 证据包

## 1. 范围

- 方案：`.codestable/features/2026-07-31-calendar-v2-redesign/calendar-v2-redesign-design.md`
- 执行清单：`.codestable/features/2026-07-31-calendar-v2-redesign/calendar-v2-redesign-checklist.yaml`

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
      "command": "make generate-check",
      "exit_code": 0,
      "stdout": "cd backend && go tool oapi-codegen -config oapi-codegen.yaml ../api/openapi.yaml\ncd frontend && npm run generate\n\n> frontend@0.0.0 generate\n> openapi-typescript ../api/openapi.yaml -o src/api/schema.d.ts\n\n✨ openapi-typescript 7.13.0\n🚀 ../api/openapi.yaml → src/api/schema.d.ts [53.7ms]\n",
      "stderr": "",
      "id": "CMD-001",
      "core": true,
      "failure_handling": "fix-or-block"
    },
    {
      "command": "cd backend && go test -p=1 ./internal/settings/... ./internal/schedule/... ./internal/dataexport/... ./internal/dashboard/... ./internal/platform/store/... ./internal/platform/httpapi/... -count=1 -parallel=1",
      "exit_code": 0,
      "stdout": "ok  \tgithub.com/samson/customer-manage-platform/backend/internal/settings\t0.351s\nok  \tgithub.com/samson/customer-manage-platform/backend/internal/schedule\t6.646s\nok  \tgithub.com/samson/customer-manage-platform/backend/internal/dataexport\t6.218s\nok  \tgithub.com/samson/customer-manage-platform/backend/internal/dashboard\t2.732s\nok  \tgithub.com/samson/customer-manage-platform/backend/internal/platform/store\t33.556s\nok  \tgithub.com/samson/customer-manage-platform/backend/internal/platform/httpapi\t28.114s\n",
      "stderr": "",
      "id": "CMD-002",
      "core": true,
      "failure_handling": "fix-or-block"
    },
    {
      "command": "cd frontend && npm run test:schedule && npm run test:settings && npm run build && npm run lint",
      "exit_code": 0,
      "stdout": "ctions deterministic (1.168542ms)\n✔ month overview follows shoot, hold, conflict, opening, and utilization rules (2.891542ms)\n✔ recurring availability uses Temporal compatible DST mapping and fails closed on reversal (0.46425ms)\n✔ calendar date arrow navigation moves focus without selecting a date (0.090334ms)\n✔ calendar workspace layout mode has one content-width breakpoint (0.038875ms)\n✔ calendar still projects slots when availability settings are unavailable (0.191333ms)\n✔ calendar and conflict preview responses only apply to their latest range generation (0.051083ms)\n✔ pending journal and handoff draft use separate expiry boundaries (0.146083ms)\n✔ a sent backfill request preserves its draft between the 2 and 24 hour boundaries (0.072708ms)\n✔ an unrelated pending flow does not extend an expired backfill draft (0.040625ms)\n✔ order_in_use details produce an account-timezone calendar deep link (0.079792ms)\n✔ known slot recovery links include the account-local date for cross-month navigation (0.063833ms)\n✔ status sync recovery derives the known slot date from its source draft (0.048375ms)\n✔ malformed order_in_use details fall back to the calendar without guessing a date (0.023417ms)\n✔ pending storage failure prevents the create request (0.735875ms)\n✔ an existing tab flow blocks a second flow without overwriting it (0.056042ms)\n✔ automatic replay keeps the original normalized body and attempt key (0.035917ms)\n✔ expired pending flow cannot replay automatically (0.060167ms)\n✔ request result knowledge follows the persisted phase facts (0.052958ms)\n✔ create failure classification never treats an idempotency binding conflict as a new attempt (0.032125ms)\n✔ deterministic failure rotates only the changed current step attempt (0.210417ms)\n✔ backfill completion persists pending id, then draft id, then clears pending (0.132125ms)\n✔ backfill local persistence retries do not repeat a known order request (0.201375ms)\n✔ schedule backfill timestamps are pruned by the final order status (0.064333ms)\n✔ completed handoff restores the original local range, note, customer, and order (0.20325ms)\n✔ schedule dialog stays closed during backfill handoff and reopens on return (0.062333ms)\n✔ calendar detail panel cannot cover an open schedule dialog (0.036292ms)\n✔ recovery actions distinguish unknown, customer change, and status sync (0.082625ms)\n✔ only path A can compensate its created order and expired recovery only abandons (0.0355ms)\n✔ editing keeps the current scheduled order visible when it is excluded from candidates (0.034916ms)\n✔ status sync searches refreshed customer pages before stable unfiltered fallback (0.186208ms)\n✔ scheduled and later non-cancelled states satisfy status sync (0.046958ms)\nℹ tests 41\nℹ suites 0\nℹ pass 41\nℹ fail 0\nℹ cancelled 0\nℹ skipped 0\nℹ todo 0\nℹ duration_ms 148.959833\n\n> frontend@0.0.0 test:settings\n> node --test --experimental-transform-types scripts/settings.test.ts\n\n(node:71052) ExperimentalWarning: Transform Types is an experimental feature and might change at any time\n(Use `node --trace-warnings ...` to show where the warning was created)\n✔ availability draft hydrates all seven days and does not mutate server settings (0.5955ms)\n✔ availability draft validates HH:MM windows and requires end after start (0.172666ms)\n✔ availability draft enforces opening and turnaround minute bounds (0.072792ms)\n✔ availability serializer replaces only availability in the current writable settings snapshot (0.098292ms)\nℹ tests 4\nℹ suites 0\nℹ pass 4\nℹ fail 0\nℹ cancelled 0\nℹ skipped 0\nℹ todo 0\nℹ duration_ms 71.328042\n\n> frontend@0.0.0 build\n> tsc -b && vite build\n\nvite v8.1.3 building client environment for production...\n\u001b[2K\ntransforming...✓ 1873 modules transformed.\nrendering chunks...\ncomputing gzip size...\ndist/index.html                   0.75 kB │ gzip:   0.44 kB\ndist/assets/index-D-0yAwTz.css   80.91 kB │ gzip:  14.95 kB\ndist/assets/index-D0LRnrkG.js   643.47 kB │ gzip: 188.45 kB\n\n✓ built in 190ms\n\n> frontend@0.0.0 lint\n> oxlint\n\n",
      "stderr": "[plugin builtin:vite-reporter] \n(!) Some chunks are larger than 500 kB after minification. Consider:\n- Using dynamic import() to code-split the application\n- Use build.rolldownOptions.output.codeSplitting to improve chunking: https://rolldown.rs/reference/OutputOptions.codeSplitting\n- Adjust chunk size limit for this warning via build.chunkSizeWarningLimit.\n",
      "id": "CMD-003",
      "core": true,
      "failure_handling": "fix-or-block"
    },
    {
      "command": "make check",
      "exit_code": 0,
      "stdout": "multiline, and cancel (0.085875ms)\n✔ note submission gate rejects a repeated submit until the active request settles (0.068125ms)\n✔ empty customer picker keyboard navigation never exposes an invalid active option (0.0865ms)\n✔ mobile calendar keeps v2 write actions reachable while dense detail content stays bounded (1.09325ms)\n✔ mobile shell keeps Customers and Calendar direct with safe-area and touch-size contracts (0.40125ms)\nℹ tests 9\nℹ suites 0\nℹ pass 9\nℹ fail 0\nℹ cancelled 0\nℹ skipped 0\nℹ todo 0\nℹ duration_ms 89.295083\n./scripts/test-auth-legacy-cutover.sh\nauth-legacy-check/accountctl=passed\nauth-legacy-check/legacy-access-jwt=passed\nauth-legacy-check/legacy-password-only-shape=passed\nauth-legacy-check/seed-startup-retired=passed\nauth-legacy-check/account-auth-admission=passed\nauth-legacy-check/rollback-harness=passed\nAUTH_LEGACY_HARNESS {\"report\":\"auth_legacy_cutover\",\"dry_run_state\":\"ready\",\"claim_state\":\"ready\",\"account_ref\":\"account:e9a4ed7a9f8f\",\"email_ref\":\"email:353587cfa45e\",\"account_id_preserved\":true,\"business_counts_preserved\":true,\"avatar_checksum_preserved\":true,\"legacy_unclaimed_count\":0,\"pending_claim_count\":0,\"active_claimed_count\":1}\nAUTH_LEGACY_HARNESS {\"report\":\"auth_legacy_rollback\",\"migration_checksum\":\"sha256:e0c4444d00eb085ea76d4e878ece8e0aa7a89521f868a41ed9d53ebe639d590b\",\"legacy_schema_version_before\":12,\"legacy_schema_version_after\":11,\"limiter_schema_rollback\":true,\"legacy_down_passed\":true,\"legacy_data_preserved\":true,\"new_style_down_blocked\":true,\"new_style_marker\":\"auth_schema_down_blocked_new_accounts\",\"new_style_account_preserved\":true,\"old_binary_degradation_boundary\":\"new_style_accounts_unavailable_to_old_binary\"}\nAUTH_LEGACY_CUTOVER_CHECK {\"report\":\"auth_legacy_cutover_checks\",\"ready\":true,\"checks\":[\"accountctl_contract\",\"legacy_password_only_shape_rejected\",\"legacy_access_jwt_rejected\",\"seed_startup_path_retired\",\"account_admission_concurrency\",\"repo_pinned_rollback\"]}\nauth legacy cutover fixture tests: passed\n./scripts/test-auth-security-catalog.sh\nauth-security/password-session=passed\nauth-security/origin-cookie=passed\nauth-security/limiter-proxy=passed\nauth-security/timing-enumeration=passed\nauth-security/api-contract=passed\nauth-security/frontend-auth=passed\nauth-security/browser-evidence=passed\nauth-security/event-redaction=passed\nauth-security/monitor=passed\nauth-security/production-preflight=passed\nauth-security/rotation-rollback=passed\nauth-security/two-account-isolation=passed\nauth-security/account-scope-consumers=passed\nauth-security/A1=passed\nauth-security/A2=passed\nauth-security/A3=passed\nauth-security/A4=passed\nauth-security/A5=passed\nauth-security/A6=passed\nauth-security/A7=passed\nauth-security/A8=passed\nauth-security/A9=passed\nauth-security/A10=passed\nauth-security/A11=passed\nauth-security/A12=passed\nauth-security/A13=passed\nauth-security/A14=passed\nauth-security/A15=passed\nauth-security/A16=passed\nauth-security/A17=passed\nauth-security/A18=passed\nAUTH_SECURITY_CATALOG {\"version\":1,\"status\":\"passed\",\"synthetic\":true,\"scenarios\":[\"A1\",\"A2\",\"A3\",\"A4\",\"A5\",\"A6\",\"A7\",\"A8\",\"A9\",\"A10\",\"A11\",\"A12\",\"A13\",\"A14\",\"A15\",\"A16\",\"A17\",\"A18\"],\"production_effect\":false}\nauth security catalog: passed\nbash ./scripts/test-v1-ops-common.sh\nv1 ops common fixture tests: passed (15 cases)\npython3 ./scripts/lib/v1-ops-package-selftest.py\nv1 ops package helper self-test: passed\nbash ./scripts/test-v1-ops-backup-restore-safety.sh\nv1 ops package helper self-test: passed\nstage/restore-failure-stop=pass\nstage/restore-failure-stop=pass\nv1 ops backup/restore safety test: passed\n./scripts/test-v1-ops-contract.sh\nv1 ops catalog contract: passed (148 cases; negative corpus rejected)\nv1 ops results contract: passed (148 cases)\ncd backend && go tool oapi-codegen -config oapi-codegen.yaml ../api/openapi.yaml\ncd frontend && npm run generate\n\n> frontend@0.0.0 generate\n> openapi-typescript ../api/openapi.yaml -o src/api/schema.d.ts\n\n✨ openapi-typescript 7.13.0\n🚀 ../api/openapi.yaml → src/api/schema.d.ts [63.1ms]\n",
      "stderr": "[plugin builtin:vite-reporter] \n(!) Some chunks are larger than 500 kB after minification. Consider:\n- Using dynamic import() to code-split the application\n- Use build.rolldownOptions.output.codeSplitting to improve chunking: https://rolldown.rs/reference/OutputOptions.codeSplitting\n- Adjust chunk size limit for this warning via build.chunkSizeWarningLimit.\n",
      "id": "CMD-004",
      "core": true,
      "failure_handling": "fix-or-block"
    }
  ],
  "providers": {}
}
```

## 3. 验证命令

命令取自执行清单 `dod.commands`；实际命令、退出码和输出见上方 DoD 结果。

## 4. 范围与清洁度

方案字节数：23609
执行清单字节数：4359

## 5. 残余风险

- 本机 Docker Desktop 的 Testcontainers mapped port 存在项目已记录的偶发抖动：第一次 DoD 执行的 CMD-002 仅 `platform/store` 报 `port "5432/tcp" not found`；同轮覆盖全部 Go 包的 CMD-004 通过，随后 CMD-002 定向重试与四条 DoD 完整重跑均通过。首轮失败详情保留在本节与 implementation 证据中，`calendar-v2-redesign-dod-results-cmd002-retry.json` 证明定向重试结果。

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
  "warnings": [],
  "evidence": [
    {
      "changed_files": [
        ".codestable/requirements/VISION.md",
        ".codestable/requirements/schedule-calendar.md",
        ".codestable/roadmap/photographer-private-crm/approval-report.md",
        ".codestable/roadmap/photographer-private-crm/goal-state.yaml",
        ".codestable/roadmap/photographer-private-crm/photographer-private-crm-items.yaml",
        ".codestable/roadmap/photographer-private-crm/photographer-private-crm-roadmap.md",
        "CLAUDE.md",
        "Makefile",
        "api/openapi.yaml",
        "backend/internal/dataexport/repository.go",
        "backend/internal/dataexport/repository_test.go",
        "backend/internal/dataexport/service.go",
        "backend/internal/dataexport/service_test.go",
        "backend/internal/platform/httpapi/api.gen.go",
        "backend/internal/platform/httpapi/data_export_route_test.go",
        "backend/internal/platform/httpapi/data_export_test.go",
        "backend/internal/platform/httpapi/schedule.go",
        "backend/internal/platform/httpapi/schedule_test.go",
        "backend/internal/platform/httpapi/settings.go",
        "backend/internal/platform/store/auth_legacy_cutover_harness_test.go",
        "backend/internal/platform/store/auth_limiter_test.go",
        "backend/internal/platform/store/auth_migration_test.go",
        "backend/internal/platform/store/store_test.go",
        "backend/internal/platform/store/telegram_digest_migration_test.go",
        "backend/internal/schedule/model.go",
        "backend/internal/schedule/repository.go",
        "backend/internal/schedule/schedule_test.go",
        "backend/internal/settings/model.go",
        "backend/internal/settings/repository.go",
        "backend/internal/settings/service.go",
        "frontend/package.json",
        "frontend/scripts/api-client.test.ts",
        "frontend/scripts/schedule.test.ts",
        "frontend/scripts/v1-hardening.test.ts",
        "frontend/src/api/client.ts",
        "frontend/src/api/schema.d.ts",
        "frontend/src/components/schedule/ScheduleSlotDialog.tsx",
        "frontend/src/components/schedule/flow.ts",
        "frontend/src/pages/CalendarPage.tsx",
        "frontend/src/pages/SettingsPage.tsx",
        ".codestable/features/2026-07-31-calendar-v2-redesign/calendar-v2-redesign-checklist.yaml",
        ".codestable/features/2026-07-31-calendar-v2-redesign/calendar-v2-redesign-design-review.md",
        ".codestable/features/2026-07-31-calendar-v2-redesign/calendar-v2-redesign-design.md",
        ".codestable/features/2026-07-31-calendar-v2-redesign/calendar-v2-redesign-implementation.md",
        "backend/internal/platform/httpapi/settings_availability_test.go",
        "backend/internal/platform/store/migrations/0014_settings_availability.down.sql",
        "backend/internal/platform/store/migrations/0014_settings_availability.up.sql",
        "backend/internal/platform/store/settings_availability_migration_test.go",
        "backend/internal/settings/availability.go",
        "backend/internal/settings/service_test.go",
        "frontend/scripts/settings.test.ts",
        "frontend/src/components/schedule/conflictPreview.ts",
        "frontend/src/pages/calendar/CalendarToolbar.tsx",
        "frontend/src/pages/calendar/CalendarWorkspace.tsx",
        "frontend/src/pages/calendar/DayDetailPanel.tsx",
        "frontend/src/pages/calendar/MonthCalendar.tsx",
        "frontend/src/pages/calendar/OpeningsDialog.tsx",
        "frontend/src/pages/calendar/WeekCalendar.tsx",
        "frontend/src/pages/calendar/calendar.css",
        "frontend/src/pages/calendar/format.ts",
        "frontend/src/pages/calendar/keyboard.ts",
        "frontend/src/pages/calendar/layoutMode.ts",
        "frontend/src/pages/calendar/model.ts",
        "frontend/src/pages/calendar/requestState.ts",
        "frontend/src/pages/calendar/types.ts",
        "frontend/src/pages/settings/AvailabilityEditor.tsx",
        "frontend/src/pages/settings/availabilityDraft.ts",
        "frontend/src/pages/settings/settings.css"
      ],
      "ignored_machine_artifacts": [],
      "allowed_prefixes": [
        ".codestable/features/2026-07-31-calendar-v2-redesign",
        ".codestable/features/2026-07-31-calendar-v2-redesign",
        ".codestable/requirements",
        ".codestable/roadmap/photographer-private-crm",
        "CLAUDE.md",
        "Makefile",
        "api",
        "backend",
        "frontend"
      ]
    }
  ],
  "providers": {}
}
```

## 8. 2026-08-01 最新验证增补

本增补优先于上方历史归档计数；旧的 schedule 41/41、settings 4/4 是 implementation 初始 evidence pack 的历史快照，不代表当前 gate。

- 当前 canonical：schedule 55/55、settings 6/6、api-client 7/7、v1-hardening 9/9。
- fresh `make generate-check`：exit 0。
- fresh `make check`：一次完整 exit 0，包含 frontend build/lint、Go build、golangci-lint 0 issues、全部 Go 包串行测试、全部前端专项、auth legacy/security、v1 ops 与生成物检查；只有既有 Vite 大 chunk warning。
- `git diff --check`、`git diff --cached --check`：exit 0；staged 为空。
- finite Settings/DELETE canonical race：`qa-evidence/rev-022-delete-canonical-browser-matrix.json`，overall verdict `pass`。
- Settings non-settling / late TZ2 / navigation-unmount / 401 refresh stall：`qa-evidence/rev-023-delete-settings-liveness-browser-matrix.json`，3/3 `pass`。
- 响应式与 Settings UAT：1600/1280/375 DOM+PNG、mobile delete API/DOM、Settings failed/pending/success API/DOM 均保留在 `qa-evidence/`。
- Round 11 reviewer 根据 owner 明确指令被中止；`approval-report.md#code-review-local-only` 记录停止追加 review 的范围。该记录不是 Goal acceptance authorization。
