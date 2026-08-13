---
doc_type: feature-evidence-pack
feature: 2026-08-05-shoot-plan-core
status: generated
---

# 2026-08-05-shoot-plan-core evidence pack

## 1. Scope

- Design: `.codestable/features/2026-08-05-shoot-plan-core/shoot-plan-core-design.md`
- Checklist: `.codestable/features/2026-08-05-shoot-plan-core/shoot-plan-core-checklist.yaml`

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
      "command": "make generate-check",
      "exit_code": 0,
      "stdout": "cd backend && go tool oapi-codegen -config oapi-codegen.yaml ../api/openapi.yaml\ncd backend/internal/shootplanning/httpcontract && go tool oapi-codegen -config oapi-codegen.yaml ../../../../api/openapi.yaml\ncd frontend && npm run generate\n\n> frontend@0.0.0 generate\n> openapi-typescript ../api/openapi.yaml -o src/api/schema.d.ts\n\n✨ openapi-typescript 7.13.0\n🚀 ../api/openapi.yaml → src/api/schema.d.ts [59.8ms]\n",
      "stderr": "",
      "id": "CMD-001",
      "core": true,
      "failure_handling": "fix-or-block"
    },
    {
      "command": "cd backend && go test -p=1 ./internal/shootplanning/... ./internal/platform/idempotency ./internal/platform/planningcapability ./internal/platform/store/... ./internal/order ./internal/schedule ./internal/platform/httpapi ./cmd/server ./cmd/planningctl -count=1 -parallel=1",
      "exit_code": 0,
      "stdout": "ok  \tgithub.com/samson/customer-manage-platform/backend/internal/shootplanning\t15.198s\n?   \tgithub.com/samson/customer-manage-platform/backend/internal/shootplanning/httpcontract\t[no test files]\nok  \tgithub.com/samson/customer-manage-platform/backend/internal/shootplanning/planningreminder\t0.462s\nok  \tgithub.com/samson/customer-manage-platform/backend/internal/platform/idempotency\t4.389s\n?   \tgithub.com/samson/customer-manage-platform/backend/internal/platform/planningcapability\t[no test files]\nok  \tgithub.com/samson/customer-manage-platform/backend/internal/platform/store\t23.241s\nok  \tgithub.com/samson/customer-manage-platform/backend/internal/order\t14.644s\nok  \tgithub.com/samson/customer-manage-platform/backend/internal/schedule\t6.714s\nok  \tgithub.com/samson/customer-manage-platform/backend/internal/platform/httpapi\t25.487s\nok  \tgithub.com/samson/customer-manage-platform/backend/cmd/server\t0.539s\nok  \tgithub.com/samson/customer-manage-platform/backend/cmd/planningctl\t0.422s\n",
      "stderr": "",
      "id": "CMD-002",
      "core": true,
      "failure_handling": "fix-or-block"
    },
    {
      "command": "cd frontend && npm run test:shoot-planning",
      "exit_code": 0,
      "stdout": "\n> frontend@0.0.0 test:shoot-planning\n> node --test --experimental-transform-types scripts/shoot-planning.test.ts\n\n(node:46536) ExperimentalWarning: Transform Types is an experimental feature and might change at any time\n(Use `node --trace-warnings ...` to show where the warning was created)\n✔ shot history acknowledgement is based on retained execution facts, not the current outcome projection (0.327334ms)\n✔ planning API reuses generated OpenAPI types and never accepts account scope or capture mode from callers (0.578166ms)\n✔ planning workspace stays in AppShell while Run Mode is independently authenticated outside it (0.736958ms)\n✔ workspace exposes only the four approved core sections and no later-feature entry points (0.44425ms)\n✔ archive confirmation submits the exact server projection instead of reconstructing effects (0.115334ms)\n✔ revision conflicts refresh server state while dirty brief, scale, and window drafts remain local (0.509167ms)\n✔ planning mutations always carry an idempotency key and retain scoped retry keys (0.295458ms)\n✔ Run Mode updates saved state only after a successful response and keeps failed actions retryable (0.457292ms)\n✔ Run Mode DOM is execution-only and exposes captured, skipped, cleared, and navigation controls (0.14525ms)\n✔ Run Mode has mobile, coarse-pointer, focus, and high-contrast light contracts (0.343542ms)\n✔ planning responsive contract avoids dense tables and preserves coarse-pointer targets (1.011542ms)\nℹ tests 11\nℹ suites 0\nℹ pass 11\nℹ fail 0\nℹ cancelled 0\nℹ skipped 0\nℹ todo 0\nℹ duration_ms 79.787167\n",
      "stderr": "",
      "id": "CMD-003",
      "core": true,
      "failure_handling": "fix-or-block"
    },
    {
      "command": "cd frontend && npm run build && npm run lint",
      "exit_code": 0,
      "stdout": "\n> frontend@0.0.0 build\n> tsc -b && vite build\n\nvite v8.1.3 building client environment for production...\n\u001b[2K\ntransforming...✓ 89 modules transformed.\nrendering chunks...\ncomputing gzip size...\ndist/index.html                   0.75 kB │ gzip:   0.44 kB\ndist/assets/index-D8MMcYaP.css   47.51 kB │ gzip:   9.73 kB\ndist/assets/index-CuPCY80L.js   611.98 kB │ gzip: 177.73 kB\n\n✓ built in 114ms\n\n> frontend@0.0.0 lint\n> oxlint\n\n",
      "stderr": "[plugin builtin:vite-reporter] \n(!) Some chunks are larger than 500 kB after minification. Consider:\n- Using dynamic import() to code-split the application\n- Use build.rolldownOptions.output.codeSplitting to improve chunking: https://rolldown.rs/reference/OutputOptions.codeSplitting\n- Adjust chunk size limit for this warning via build.chunkSizeWarningLimit.\n",
      "id": "CMD-004",
      "core": true,
      "failure_handling": "fix-or-block"
    },
    {
      "command": "make check",
      "exit_code": 0,
      "stdout": "--test scripts/avatar-layout.test.mjs\n\n✔ customer profile flex growth excludes the avatar (0.430875ms)\nℹ tests 1\nℹ suites 0\nℹ pass 1\nℹ fail 0\nℹ cancelled 0\nℹ skipped 0\nℹ todo 0\nℹ duration_ms 34.57475\ncd frontend && npm run test:telegram-digest\n\n> frontend@0.0.0 test:telegram-digest\n> node --test --experimental-transform-types scripts/telegram-digest.test.ts\n\n(node:49688) ExperimentalWarning: Transform Types is an experimental feature and might change at any time\n(Use `node --trace-warnings ...` to show where the warning was created)\n✔ bind-token API wrapper posts to the generated contract and never persists response (8.595708ms)\n✔ deep-link opener uses noopener,noreferrer and reports popup blocking (0.452375ms)\n✔ Settings page exposes binding states without rendering secrets or storage/log sinks (2.017667ms)\n✔ Telegram binding card keeps mobile wrapping and keyboard focus contracts (0.459084ms)\nℹ tests 4\nℹ suites 0\nℹ pass 4\nℹ fail 0\nℹ cancelled 0\nℹ skipped 0\nℹ todo 0\nℹ duration_ms 88.087458\ncd frontend && npm run test:data-export\n\n> frontend@0.0.0 test:data-export\n> node --test --experimental-transform-types scripts/data-export.test.ts\n\n(node:49706) ExperimentalWarning: Transform Types is an experimental feature and might change at any time\n(Use `node --trace-warnings ...` to show where the warning was created)\n✔ fetchDataExport sends Bearer and returns a complete JSON Blob with the exact server filename (10.619167ms)\n✔ fetchDataExport accepts only application/json base media type with legal parameters (1.846708ms)\n✔ fetchDataExport accepts only the whole fixed filename and otherwise uses a safe UTC fallback (2.644667ms)\n✔ fetchDataExport reuses ApiError for 500 and 401 responses (0.403208ms)\n✔ body/blob rejection returns no result and download collaboration creates no object URL (0.142833ms)\n✔ download collaboration triggers once and always releases its object URL (0.347333ms)\n✔ DataExportCard exposes loading, retry, 401, PII, avatar, and persistence-safe contracts (1.191791ms)\n✔ DataExportCard relies on native button keyboard behavior and shared mobile-safe styles (0.43825ms)\nℹ tests 8\nℹ suites 0\nℹ pass 8\nℹ fail 0\nℹ cancelled 0\nℹ skipped 0\nℹ todo 0\nℹ duration_ms 94.865\ncd frontend && npm run test:v1-hardening\n\n> frontend@0.0.0 test:v1-hardening\n> node --test --experimental-transform-types scripts/v1-hardening.test.ts\n\n(node:49734) ExperimentalWarning: Transform Types is an experimental feature and might change at any time\n(Use `node --trace-warnings ...` to show where the warning was created)\n✔ page read states have one observable presentation in the fixed priority order (0.591ms)\n✔ page-owned data becomes stale on refresh failure without becoming empty (0.087875ms)\n✔ initial failure and successful empty remain mutually exclusive (0.050959ms)\n✔ customer results keep one page-owned query across the 768/769 responsive boundary (0.775792ms)\n✔ note keyboard actions separate submit, IME composition, multiline, and cancel (0.090042ms)\n✔ note submission gate rejects a repeated submit until the active request settles (0.069167ms)\n✔ empty customer picker keyboard navigation never exposes an invalid active option (0.079125ms)\n✔ mobile calendar keeps write actions hidden while dense and long drawer content stays bounded (0.4975ms)\n✔ mobile shell keeps Customers and Calendar direct with safe-area and touch-size contracts (0.405209ms)\nℹ tests 9\nℹ suites 0\nℹ pass 9\nℹ fail 0\nℹ cancelled 0\nℹ skipped 0\nℹ todo 0\nℹ duration_ms 86.388042\n./scripts/test-v1-ops-contract.sh\nv1 ops catalog contract: passed (148 cases; negative corpus rejected)\ncd backend && go tool oapi-codegen -config oapi-codegen.yaml ../api/openapi.yaml\ncd backend/internal/shootplanning/httpcontract && go tool oapi-codegen -config oapi-codegen.yaml ../../../../api/openapi.yaml\ncd frontend && npm run generate\n\n> frontend@0.0.0 generate\n> openapi-typescript ../api/openapi.yaml -o src/api/schema.d.ts\n\n✨ openapi-typescript 7.13.0\n🚀 ../api/openapi.yaml → src/api/schema.d.ts [64.8ms]\n",
      "stderr": "[plugin builtin:vite-reporter] \n(!) Some chunks are larger than 500 kB after minification. Consider:\n- Using dynamic import() to code-split the application\n- Use build.rolldownOptions.output.codeSplitting to improve chunking: https://rolldown.rs/reference/OutputOptions.codeSplitting\n- Adjust chunk size limit for this warning via build.chunkSizeWarningLimit.\n",
      "id": "CMD-005",
      "core": true,
      "failure_handling": "fix-or-block"
    }
  ],
  "providers": {},
  "feature": "2026-08-05-shoot-plan-core",
  "inputs": {
    "checklist": ".codestable/features/2026-08-05-shoot-plan-core/shoot-plan-core-checklist.yaml"
  },
  "input_digests": {
    "checklist": "d7a02e7a6298cb15ae30bec2152dadb595dcdedc6385b2b3c70d8578cd001a66"
  }
}
```

## 3. Validation Commands

Extracted from checklist `dod.commands`; see DoD Results for command status.

## 4. Scope And Cleanliness

Design bytes: 71797
Checklist bytes: 11219

## 5. Residual Risks

- cleanliness marker TODO in .codestable/features/2026-08-05-creative-planning-v1-hardening/creative-planning-v1-hardening-checklist.yaml
- cleanliness marker FIXME in .codestable/features/2026-08-05-creative-planning-v1-hardening/creative-planning-v1-hardening-checklist.yaml
- cleanliness marker TODO in .codestable/features/2026-08-05-plan-business-feedback/plan-business-feedback-checklist.yaml
- cleanliness marker FIXME in .codestable/features/2026-08-05-plan-business-feedback/plan-business-feedback-checklist.yaml

## 6. Provider Signals

```json
{
  "archguard": {
    "status": "unavailable",
    "reason": "archguard binary not found on PATH",
    "warnings": []
  },
  "meta_cc": {
    "status": "unavailable",
    "reason": "meta-cc summary not found; realtime session collection is out of scope",
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
    "cleanliness marker TODO in .codestable/features/2026-08-05-creative-planning-v1-hardening/creative-planning-v1-hardening-checklist.yaml",
    "cleanliness marker FIXME in .codestable/features/2026-08-05-creative-planning-v1-hardening/creative-planning-v1-hardening-checklist.yaml",
    "cleanliness marker TODO in .codestable/features/2026-08-05-plan-business-feedback/plan-business-feedback-checklist.yaml",
    "cleanliness marker FIXME in .codestable/features/2026-08-05-plan-business-feedback/plan-business-feedback-checklist.yaml"
  ],
  "evidence": [
    {
      "changed_files": [
        ".codestable/attention.md",
        ".codestable/compound/2026-07-06-accountscope-fail-loud.md",
        ".codestable/requirements/VISION.md",
        "Makefile",
        "api/openapi.yaml",
        "backend/cmd/server/main.go",
        "backend/internal/platform/httpapi/api.gen.go",
        "backend/internal/platform/httpapi/customers_test.go",
        "backend/internal/platform/httpapi/envelope.go",
        "backend/internal/platform/httpapi/orders_test.go",
        "backend/internal/platform/httpapi/router.go",
        "backend/internal/platform/httpapi/schedule_test.go",
        "backend/internal/platform/idempotency/idempotency.go",
        "backend/internal/platform/idempotency/idempotency_test.go",
        "backend/internal/platform/store/scope_tx.go",
        "backend/internal/platform/store/store_test.go",
        "backend/internal/platform/store/telegram_digest_migration_test.go",
        "frontend/package.json",
        "frontend/src/App.tsx",
        "frontend/src/api/schema.d.ts",
        "frontend/src/components/AppShell.tsx",
        ".codestable/brainstorms/creative-shoot-planning/brainstorm.md",
        ".codestable/features/2026-08-05-creative-planning-v1-hardening/creative-planning-v1-hardening-checklist.yaml",
        ".codestable/features/2026-08-05-creative-planning-v1-hardening/creative-planning-v1-hardening-design-review.md",
        ".codestable/features/2026-08-05-creative-planning-v1-hardening/creative-planning-v1-hardening-design.md",
        ".codestable/features/2026-08-05-plan-assignment-reminders/plan-assignment-reminders-checklist.yaml",
        ".codestable/features/2026-08-05-plan-assignment-reminders/plan-assignment-reminders-design-review.md",
        ".codestable/features/2026-08-05-plan-assignment-reminders/plan-assignment-reminders-design.md",
        ".codestable/features/2026-08-05-plan-business-feedback/plan-business-feedback-checklist.yaml",
        ".codestable/features/2026-08-05-plan-business-feedback/plan-business-feedback-design-review.md",
        ".codestable/features/2026-08-05-plan-business-feedback/plan-business-feedback-design.md",
        ".codestable/features/2026-08-05-plan-ingestion-capture/plan-ingestion-capture-checklist.yaml",
        ".codestable/features/2026-08-05-plan-ingestion-capture/plan-ingestion-capture-design-review.md",
        ".codestable/features/2026-08-05-plan-ingestion-capture/plan-ingestion-capture-design.md",
        ".codestable/features/2026-08-05-plan-share-collaboration/plan-share-collaboration-checklist.yaml",
        ".codestable/features/2026-08-05-plan-share-collaboration/plan-share-collaboration-design-review.md",
        ".codestable/features/2026-08-05-plan-share-collaboration/plan-share-collaboration-design.md",
        ".codestable/features/2026-08-05-planning-reference-assets/planning-reference-assets-checklist.yaml",
        ".codestable/features/2026-08-05-planning-reference-assets/planning-reference-assets-design-review.md",
        ".codestable/features/2026-08-05-planning-reference-assets/planning-reference-assets-design.md",
        ".codestable/features/2026-08-05-shoot-plan-core/evidence/s6-ledger-empty-1600.png",
        ".codestable/features/2026-08-05-shoot-plan-core/evidence/s6-workspace-readiness-1280.png",
        ".codestable/features/2026-08-05-shoot-plan-core/evidence/s6-workspace-readiness-375.png",
        ".codestable/features/2026-08-05-shoot-plan-core/evidence/s7-run-mode-375.png",
        ".codestable/features/2026-08-05-shoot-plan-core/shoot-plan-core-checklist.yaml",
        ".codestable/features/2026-08-05-shoot-plan-core/shoot-plan-core-design-review.md",
        ".codestable/features/2026-08-05-shoot-plan-core/shoot-plan-core-design.md",
        ".codestable/features/2026-08-05-shoot-plan-core/shoot-plan-core-dod-contract-results.json",
        ".codestable/features/2026-08-05-shoot-plan-core/shoot-plan-core-evidence-pack.md",
        ".codestable/features/2026-08-05-shoot-plan-core/shoot-plan-core-implementation.md",
        ".codestable/features/2026-08-05-shoot-plan-core/shoot-plan-core-review.md",
        ".codestable/features/2026-08-05-shoot-plan-crm-integration/shoot-plan-crm-integration-checklist.yaml",
        ".codestable/features/2026-08-05-shoot-plan-crm-integration/shoot-plan-crm-integration-design-review.md",
        ".codestable/features/2026-08-05-shoot-plan-crm-integration/shoot-plan-crm-integration-design.md",
        ".codestable/requirements/creative-shoot-planning.md",
        ".codestable/roadmap/creative-shoot-planning/approval-report.md",
        ".codestable/roadmap/creative-shoot-planning/creative-shoot-planning-items.yaml",
        ".codestable/roadmap/creative-shoot-planning/creative-shoot-planning-roadmap-review.md",
        ".codestable/roadmap/creative-shoot-planning/creative-shoot-planning-roadmap.md",
        ".codestable/roadmap/creative-shoot-planning/goal-audit.md",
        ".codestable/roadmap/creative-shoot-planning/goal-features/creative-planning-v1-hardening.md",
        ".codestable/roadmap/creative-shoot-planning/goal-features/plan-assignment-reminders.md",
        ".codestable/roadmap/creative-shoot-planning/goal-features/plan-business-feedback.md",
        ".codestable/roadmap/creative-shoot-planning/goal-features/plan-ingestion-capture.md",
        ".codestable/roadmap/creative-shoot-planning/goal-features/plan-share-collaboration.md",
        ".codestable/roadmap/creative-shoot-planning/goal-features/planning-reference-assets.md",
        ".codestable/roadmap/creative-shoot-planning/goal-features/shoot-plan-core.md",
        ".codestable/roadmap/creative-shoot-planning/goal-features/shoot-plan-crm-integration.md",
        ".codestable/roadmap/creative-shoot-planning/goal-plan.md",
        ".codestable/roadmap/creative-shoot-planning/goal-protocol-audit.md",
        ".codestable/roadmap/creative-shoot-planning/goal-protocol-feature-loop.md",
        ".codestable/roadmap/creative-shoot-planning/goal-protocol-gates.md",
        ".codestable/roadmap/creative-shoot-planning/goal-protocol.md",
        ".codestable/roadmap/creative-shoot-planning/goal-state.yaml",
        ".codestable/roadmap/creative-shoot-planning/goal-tools/planning-evidence-dispatch-gate.py",
        "backend/cmd/planningctl/main.go",
        "backend/cmd/planningctl/main_test.go",
        "backend/cmd/server/shoot_planning_test.go",
        "backend/internal/platform/httpapi/shoot_planning.go",
        "backend/internal/platform/httpapi/shoot_planning_test.go",
        "backend/internal/platform/planningcapability/capability.go",
        "backend/internal/platform/store/idempotency_ledger_view.go",
        "backend/internal/platform/store/migrations/0016_shoot_planning_core.down.sql",
        "backend/internal/platform/store/migrations/0016_shoot_planning_core.up.sql",
        "backend/internal/platform/store/planning_capability.go",
        "backend/internal/platform/store/planning_migration_test.go",
        "backend/internal/platform/store/planning_reminder_fence.go",
        "backend/internal/platform/store/planning_share_migration_contract_test.go",
        "backend/internal/platform/txcap/txcap.go",
        "backend/internal/shootplanning/application.go",
        "backend/internal/shootplanning/batch.go",
        "backend/internal/shootplanning/command_engine.go",
        "backend/internal/shootplanning/command_engine_test.go",
        "backend/internal/shootplanning/execution.go",
        "backend/internal/shootplanning/httpcontract/api.gen.go",
        "backend/internal/shootplanning/httpcontract/oapi-codegen.yaml",
        "backend/internal/shootplanning/model.go",
        "backend/internal/shootplanning/planningreminder/fence.go",
        "backend/internal/shootplanning/planningreminder/fence_test.go",
        "backend/internal/shootplanning/replay.go",
        "backend/internal/shootplanning/replay_test.go",
        "backend/internal/shootplanning/repository.go",
        "backend/internal/shootplanning/repository_integration_test.go",
        "backend/internal/shootplanning/state_machine.go",
        "backend/internal/shootplanning/state_machine_test.go",
        "docs/prototypes/creative-shoot-planning/v2/README.md",
        "docs/prototypes/creative-shoot-planning/v2/index.html",
        "docs/prototypes/creative-shoot-planning/v2/index.html.artifact.json",
        "docs/prototypes/creative-shoot-planning/v2/plan-ingestion.html",
        "docs/prototypes/creative-shoot-planning/v2/plan-ingestion.html.artifact.json",
        "docs/prototypes/creative-shoot-planning/v2/planning-workspace.html",
        "docs/prototypes/creative-shoot-planning/v2/planning-workspace.html.artifact.json",
        "docs/prototypes/creative-shoot-planning/v2/planning.css",
        "docs/prototypes/creative-shoot-planning/v2/run-mode.html",
        "docs/prototypes/creative-shoot-planning/v2/run-mode.html.artifact.json",
        "docs/prototypes/creative-shoot-planning/v2/shared-plan.html",
        "docs/prototypes/creative-shoot-planning/v2/shared-plan.html.artifact.json",
        "frontend/scripts/shoot-planning.test.ts",
        "frontend/src/planning/ShootPlanRunPage.tsx",
        "frontend/src/planning/ShootPlanWorkspacePage.tsx",
        "frontend/src/planning/ShootPlansPage.tsx",
        "frontend/src/planning/StatusBadge.tsx",
        "frontend/src/planning/api.ts",
        "frontend/src/planning/history.ts",
        "frontend/src/planning/panels/BriefPanel.tsx",
        "frontend/src/planning/panels/ExecutionHistoryPanel.tsx",
        "frontend/src/planning/panels/ReadinessPanel.tsx",
        "frontend/src/planning/panels/ShotsPanel.tsx",
        "frontend/src/planning/planning.css",
        "frontend/src/planning/presentation.ts",
        "frontend/src/planning/run.css",
        "frontend/src/planning/runState.ts"
      ],
      "ignored_machine_artifacts": [
        ".codestable/features/2026-08-05-shoot-plan-core/shoot-plan-core-dod-results.json",
        ".codestable/features/2026-08-05-shoot-plan-core/shoot-plan-core-evidence-pack-results.json",
        ".codestable/features/2026-08-05-shoot-plan-core/shoot-plan-core-gate-results.json"
      ],
      "allowed_prefixes": [
        ".codestable/features/2026-08-05-shoot-plan-core",
        ".codestable/attention.md",
        ".codestable/brainstorms/creative-shoot-planning",
        ".codestable/compound/2026-07-06-accountscope-fail-loud.md",
        ".codestable/requirements/VISION.md",
        ".codestable/requirements/creative-shoot-planning.md",
        ".codestable/roadmap/creative-shoot-planning",
        ".codestable/features/2026-08-05-shoot-plan-core",
        ".codestable/features/2026-08-05-planning-reference-assets",
        ".codestable/features/2026-08-05-plan-ingestion-capture",
        ".codestable/features/2026-08-05-shoot-plan-crm-integration",
        ".codestable/features/2026-08-05-plan-share-collaboration",
        ".codestable/features/2026-08-05-plan-assignment-reminders",
        ".codestable/features/2026-08-05-plan-business-feedback",
        ".codestable/features/2026-08-05-creative-planning-v1-hardening",
        "docs/prototypes/creative-shoot-planning",
        "Makefile",
        "api/openapi.yaml",
        "backend/cmd/planningctl",
        "backend/cmd/server/main.go",
        "backend/cmd/server/shoot_planning_test.go",
        "backend/internal/platform/httpapi",
        "backend/internal/platform/idempotency",
        "backend/internal/platform/planningcapability",
        "backend/internal/platform/store",
        "backend/internal/platform/txcap",
        "backend/internal/shootplanning",
        "frontend/package.json",
        "frontend/scripts/shoot-planning.test.ts",
        "frontend/src/App.tsx",
        "frontend/src/api/schema.d.ts",
        "frontend/src/components/AppShell.tsx",
        "frontend/src/planning"
      ]
    }
  ],
  "providers": {},
  "feature": "2026-08-05-shoot-plan-core",
  "inputs": {
    "feature_dir": ".codestable/features/2026-08-05-shoot-plan-core"
  },
  "input_digests": {}
}
```
