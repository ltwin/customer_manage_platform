---
doc_type: feature-evidence-pack
feature: 2026-08-05-planning-reference-assets
status: generated
---

# 2026-08-05-planning-reference-assets evidence pack

## 1. Scope

- Design: `.codestable/features/2026-08-05-planning-reference-assets/planning-reference-assets-design.md`
- Checklist: `.codestable/features/2026-08-05-planning-reference-assets/planning-reference-assets-checklist.yaml`

## 2. DoD Results

```json
{
  "gate_id": "dod-runner",
  "stage": "acceptance",
  "status": "passed",
  "blocking": [],
  "warnings": [],
  "evidence": [
    {
      "command": "make generate-check",
      "exit_code": 0,
      "stdout": "cd backend && go tool oapi-codegen -config oapi-codegen.yaml ../api/openapi.yaml\ncd backend/internal/shootplanning/httpcontract && go tool oapi-codegen -config oapi-codegen.yaml ../../../../api/openapi.yaml\ncd frontend && npm run generate\n\n> frontend@0.0.0 generate\n> openapi-typescript ../api/openapi.yaml -o src/api/schema.d.ts\n\n✨ openapi-typescript 7.13.0\n🚀 ../api/openapi.yaml → src/api/schema.d.ts [71.5ms]\n",
      "stderr": "",
      "id": "CMD-001",
      "core": true,
      "failure_handling": "fix-or-block"
    },
    {
      "command": "cd backend && go test -p=1 ./internal/planningmedia ./internal/platform/immutablefs ./internal/customer/avatarstore ./internal/customer/avatarimage ./internal/customer ./internal/shootplanning ./internal/platform/idempotency ./internal/platform/httpapi -count=1 -parallel=1",
      "exit_code": 0,
      "stdout": "ok  \tgithub.com/samson/customer-manage-platform/backend/internal/planningmedia\t7.486s\nok  \tgithub.com/samson/customer-manage-platform/backend/internal/platform/immutablefs\t0.270s\nok  \tgithub.com/samson/customer-manage-platform/backend/internal/customer/avatarstore\t1.082s\nok  \tgithub.com/samson/customer-manage-platform/backend/internal/customer/avatarimage\t0.289s\nok  \tgithub.com/samson/customer-manage-platform/backend/internal/customer\t45.098s\nok  \tgithub.com/samson/customer-manage-platform/backend/internal/shootplanning\t16.348s\nok  \tgithub.com/samson/customer-manage-platform/backend/internal/platform/idempotency\t3.947s\nok  \tgithub.com/samson/customer-manage-platform/backend/internal/platform/httpapi\t25.861s\n",
      "stderr": "",
      "id": "CMD-002",
      "core": true,
      "failure_handling": "fix-or-block"
    },
    {
      "command": "cd backend && go test -p=1 ./internal/planningmedia -run 'TestPostgres|TestSharedTx|TestGC|TestRestore|TestUploadFaults' -count=1 -parallel=1",
      "exit_code": 0,
      "stdout": "ok  \tgithub.com/samson/customer-manage-platform/backend/internal/planningmedia\t7.848s\n",
      "stderr": "",
      "id": "CMD-003",
      "core": true,
      "failure_handling": "fix-or-block"
    },
    {
      "command": "cd frontend && npm run test:planning-media",
      "exit_code": 0,
      "stdout": "\n> frontend@0.0.0 test:planning-media\n> node --test --experimental-transform-types scripts/planning-media.test.ts\n\n(node:2434) ExperimentalWarning: Transform Types is an experimental feature and might change at any time\n(Use `node --trace-warnings ...` to show where the warning was created)\n✔ planning media API keeps multipart upload separate from JSON request helper (0.3385ms)\n✔ planning media UI exposes staged, active, corrupt and retryable states (0.065209ms)\n✔ planning media upload accepts only declared supported image types (0.048417ms)\n✔ run mode renders read-only reference sheet without blocking capture (0.092125ms)\nℹ tests 4\nℹ suites 0\nℹ pass 4\nℹ fail 0\nℹ cancelled 0\nℹ skipped 0\nℹ todo 0\nℹ duration_ms 85.531375\n",
      "stderr": "",
      "id": "CMD-004",
      "core": true,
      "failure_handling": "fix-or-block"
    },
    {
      "command": "cd frontend && npm run build && npm run lint",
      "exit_code": 0,
      "stdout": "\n> frontend@0.0.0 build\n> tsc -b && vite build\n\nvite v8.1.3 building client environment for production...\n\u001b[2K\ntransforming...✓ 90 modules transformed.\nrendering chunks...\ncomputing gzip size...\ndist/index.html                   0.75 kB │ gzip:   0.44 kB\ndist/assets/index-Bn-DyN5d.css   49.90 kB │ gzip:  10.13 kB\ndist/assets/index-DyV-TW-N.js   619.26 kB │ gzip: 179.76 kB\n\n✓ built in 161ms\n\n> frontend@0.0.0 lint\n> oxlint\n\n",
      "stderr": "[plugin builtin:vite-reporter] \n(!) Some chunks are larger than 500 kB after minification. Consider:\n- Using dynamic import() to code-split the application\n- Use build.rolldownOptions.output.codeSplitting to improve chunking: https://rolldown.rs/reference/OutputOptions.codeSplitting\n- Adjust chunk size limit for this warning via build.chunkSizeWarningLimit.\n",
      "id": "CMD-005",
      "core": true,
      "failure_handling": "fix-or-block"
    },
    {
      "command": "make check",
      "exit_code": 0,
      "stdout": "test scripts/avatar-layout.test.mjs\n\n✔ customer profile flex growth excludes the avatar (0.57675ms)\nℹ tests 1\nℹ suites 0\nℹ pass 1\nℹ fail 0\nℹ cancelled 0\nℹ skipped 0\nℹ todo 0\nℹ duration_ms 36.011875\ncd frontend && npm run test:telegram-digest\n\n> frontend@0.0.0 test:telegram-digest\n> node --test --experimental-transform-types scripts/telegram-digest.test.ts\n\n(node:5561) ExperimentalWarning: Transform Types is an experimental feature and might change at any time\n(Use `node --trace-warnings ...` to show where the warning was created)\n✔ bind-token API wrapper posts to the generated contract and never persists response (9.443667ms)\n✔ deep-link opener uses noopener,noreferrer and reports popup blocking (0.104625ms)\n✔ Settings page exposes binding states without rendering secrets or storage/log sinks (2.532125ms)\n✔ Telegram binding card keeps mobile wrapping and keyboard focus contracts (0.495542ms)\nℹ tests 4\nℹ suites 0\nℹ pass 4\nℹ fail 0\nℹ cancelled 0\nℹ skipped 0\nℹ todo 0\nℹ duration_ms 93.381708\ncd frontend && npm run test:data-export\n\n> frontend@0.0.0 test:data-export\n> node --test --experimental-transform-types scripts/data-export.test.ts\n\n(node:5579) ExperimentalWarning: Transform Types is an experimental feature and might change at any time\n(Use `node --trace-warnings ...` to show where the warning was created)\n✔ fetchDataExport sends Bearer and returns a complete JSON Blob with the exact server filename (10.967791ms)\n✔ fetchDataExport accepts only application/json base media type with legal parameters (1.757083ms)\n✔ fetchDataExport accepts only the whole fixed filename and otherwise uses a safe UTC fallback (2.448792ms)\n✔ fetchDataExport reuses ApiError for 500 and 401 responses (0.412667ms)\n✔ body/blob rejection returns no result and download collaboration creates no object URL (0.142417ms)\n✔ download collaboration triggers once and always releases its object URL (0.351417ms)\n✔ DataExportCard exposes loading, retry, 401, PII, avatar, and persistence-safe contracts (1.398083ms)\n✔ DataExportCard relies on native button keyboard behavior and shared mobile-safe styles (0.698875ms)\nℹ tests 8\nℹ suites 0\nℹ pass 8\nℹ fail 0\nℹ cancelled 0\nℹ skipped 0\nℹ todo 0\nℹ duration_ms 100.674208\ncd frontend && npm run test:v1-hardening\n\n> frontend@0.0.0 test:v1-hardening\n> node --test --experimental-transform-types scripts/v1-hardening.test.ts\n\n(node:5598) ExperimentalWarning: Transform Types is an experimental feature and might change at any time\n(Use `node --trace-warnings ...` to show where the warning was created)\n✔ page read states have one observable presentation in the fixed priority order (0.591958ms)\n✔ page-owned data becomes stale on refresh failure without becoming empty (0.0905ms)\n✔ initial failure and successful empty remain mutually exclusive (0.05025ms)\n✔ customer results keep one page-owned query across the 768/769 responsive boundary (0.809375ms)\n✔ note keyboard actions separate submit, IME composition, multiline, and cancel (0.090291ms)\n✔ note submission gate rejects a repeated submit until the active request settles (0.103209ms)\n✔ empty customer picker keyboard navigation never exposes an invalid active option (0.09325ms)\n✔ mobile calendar keeps write actions hidden while dense and long drawer content stays bounded (0.580542ms)\n✔ mobile shell keeps Customers and Calendar direct with safe-area and touch-size contracts (0.58025ms)\nℹ tests 9\nℹ suites 0\nℹ pass 9\nℹ fail 0\nℹ cancelled 0\nℹ skipped 0\nℹ todo 0\nℹ duration_ms 86.681209\n./scripts/test-v1-ops-contract.sh\nv1 ops catalog contract: passed (148 cases; negative corpus rejected)\ncd backend && go tool oapi-codegen -config oapi-codegen.yaml ../api/openapi.yaml\ncd backend/internal/shootplanning/httpcontract && go tool oapi-codegen -config oapi-codegen.yaml ../../../../api/openapi.yaml\ncd frontend && npm run generate\n\n> frontend@0.0.0 generate\n> openapi-typescript ../api/openapi.yaml -o src/api/schema.d.ts\n\n✨ openapi-typescript 7.13.0\n🚀 ../api/openapi.yaml → src/api/schema.d.ts [60.8ms]\n",
      "stderr": "[plugin builtin:vite-reporter] \n(!) Some chunks are larger than 500 kB after minification. Consider:\n- Using dynamic import() to code-split the application\n- Use build.rolldownOptions.output.codeSplitting to improve chunking: https://rolldown.rs/reference/OutputOptions.codeSplitting\n- Adjust chunk size limit for this warning via build.chunkSizeWarningLimit.\n",
      "id": "CMD-006",
      "core": true,
      "failure_handling": "fix-or-block"
    }
  ],
  "providers": {},
  "feature": "2026-08-05-planning-reference-assets",
  "inputs": {
    "checklist": ".codestable/features/2026-08-05-planning-reference-assets/planning-reference-assets-checklist.yaml"
  },
  "input_digests": {
    "checklist": "7859f075f52b20abd8e8e4a8af62069c6288fdbd3e737bc20534776618f70179"
  }
}
```

## 3. Validation Commands

Extracted from checklist `dod.commands`; see DoD Results for command status.

## 4. Scope And Cleanliness

Design bytes: 29789
Checklist bytes: 6894

## 5. Residual Risks

- none

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
  "stage": "acceptance",
  "status": "passed",
  "blocking": [],
  "warnings": [],
  "evidence": [
    {
      "changed_files": [
        ".codestable/attention.md",
        ".codestable/features/2026-08-05-planning-reference-assets/planning-reference-assets-checklist.yaml",
        ".codestable/features/2026-08-05-planning-reference-assets/planning-reference-assets-design.md",
        ".codestable/roadmap/creative-shoot-planning/creative-shoot-planning-items.yaml",
        ".codestable/roadmap/creative-shoot-planning/creative-shoot-planning-roadmap.md",
        ".codestable/roadmap/creative-shoot-planning/goal-features/planning-reference-assets.md",
        ".codestable/roadmap/creative-shoot-planning/goal-plan.md",
        ".codestable/roadmap/creative-shoot-planning/goal-state.yaml",
        "api/openapi.yaml",
        "backend/cmd/server/main.go",
        "backend/internal/customer/avatarstore/local.go",
        "backend/internal/customer/avatarstore/local_test.go",
        "backend/internal/platform/httpapi/api.gen.go",
        "backend/internal/platform/httpapi/auth.go",
        "backend/internal/platform/httpapi/router.go",
        "backend/internal/platform/idempotency/idempotency.go",
        "backend/internal/platform/store/planning_migration_test.go",
        "backend/internal/platform/store/scope.go",
        "backend/internal/platform/store/store_test.go",
        "backend/internal/platform/store/telegram_digest_migration_test.go",
        "backend/internal/shootplanning/application.go",
        "backend/internal/shootplanning/execution.go",
        "backend/internal/shootplanning/httpcontract/api.gen.go",
        "backend/internal/shootplanning/model.go",
        "backend/oapi-codegen.yaml",
        "frontend/package.json",
        "frontend/src/api/schema.d.ts",
        "frontend/src/planning/ShootPlanRunPage.tsx",
        "frontend/src/planning/ShootPlanWorkspacePage.tsx",
        "frontend/src/planning/api.ts",
        "frontend/src/planning/planning.css",
        "frontend/src/planning/run.css",
        ".codestable/features/2026-08-05-planning-reference-assets/planning-reference-assets-acceptance.md",
        ".codestable/features/2026-08-05-planning-reference-assets/planning-reference-assets-dod-contract-results.json",
        ".codestable/features/2026-08-05-planning-reference-assets/planning-reference-assets-evidence-pack.md",
        ".codestable/features/2026-08-05-planning-reference-assets/planning-reference-assets-implementation.md",
        ".codestable/features/2026-08-05-planning-reference-assets/planning-reference-assets-qa.md",
        ".codestable/features/2026-08-05-planning-reference-assets/planning-reference-assets-review.md",
        "backend/internal/planningmedia/application.go",
        "backend/internal/planningmedia/domain.go",
        "backend/internal/planningmedia/inventory.go",
        "backend/internal/planningmedia/inventory_test.go",
        "backend/internal/planningmedia/lifecycle_integration_test.go",
        "backend/internal/planningmedia/matrix.go",
        "backend/internal/planningmedia/matrix_test.go",
        "backend/internal/planningmedia/pipeline.go",
        "backend/internal/planningmedia/pipeline_test.go",
        "backend/internal/planningmedia/repository.go",
        "backend/internal/planningmedia/repository_integration_test.go",
        "backend/internal/planningmedia/upload_spool.go",
        "backend/internal/planningmedia/upload_spool_test.go",
        "backend/internal/platform/httpapi/planning_media.go",
        "backend/internal/platform/immutablefs/store.go",
        "backend/internal/platform/immutablefs/store_test.go",
        "backend/internal/platform/store/migrations/0017_planning_media.down.sql",
        "backend/internal/platform/store/migrations/0017_planning_media.up.sql",
        "backend/internal/shootplanning/media_holder.go",
        "frontend/scripts/planning-media.test.ts",
        "frontend/src/planning/panels/PlanningMediaPanel.tsx"
      ],
      "ignored_machine_artifacts": [
        ".codestable/features/2026-08-05-planning-reference-assets/planning-reference-assets-dod-results.json",
        ".codestable/features/2026-08-05-planning-reference-assets/planning-reference-assets-evidence-pack-results.json",
        ".codestable/features/2026-08-05-planning-reference-assets/planning-reference-assets-gate-results.json"
      ],
      "allowed_prefixes": [
        ".codestable/features/2026-08-05-planning-reference-assets",
        ".codestable/features/2026-08-05-planning-reference-assets",
        ".codestable/roadmap/creative-shoot-planning",
        ".codestable/attention.md",
        "api/openapi.yaml",
        "backend/cmd/server/main.go",
        "backend/oapi-codegen.yaml",
        "backend/internal/customer",
        "backend/internal/platform/httpapi",
        "backend/internal/platform/idempotency",
        "backend/internal/platform/immutablefs",
        "backend/internal/platform/store",
        "backend/internal/shootplanning",
        "backend/internal/planningmedia",
        "frontend/package.json",
        "frontend/src/api/schema.d.ts",
        "frontend/src/planning",
        "frontend/scripts/planning-media.test.ts"
      ]
    }
  ],
  "providers": {},
  "feature": "2026-08-05-planning-reference-assets",
  "inputs": {
    "feature_dir": ".codestable/features/2026-08-05-planning-reference-assets"
  },
  "input_digests": {}
}
```
