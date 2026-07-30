---
doc_type: feature-evidence-pack
feature: 2026-07-21-data-export
status: generated
---

# 2026-07-21-data-export evidence pack

## 1. Scope

- Design: `.codestable/features/2026-07-21-data-export/data-export-design.md`
- Checklist: `.codestable/features/2026-07-21-data-export/data-export-checklist.yaml`

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
      "command": "make check",
      "exit_code": 0,
      "stdout": "est result knowledge follows the persisted phase facts (0.052084ms)\n✔ create failure classification never treats an idempotency binding conflict as a new attempt (0.039167ms)\n✔ deterministic failure rotates only the changed current step attempt (0.180792ms)\n✔ backfill completion persists pending id, then draft id, then clears pending (0.126209ms)\n✔ backfill local persistence retries do not repeat a known order request (0.21775ms)\n✔ schedule backfill timestamps are pruned by the final order status (0.056083ms)\n✔ completed handoff restores the original local range, note, customer, and order (0.154083ms)\n✔ schedule dialog stays closed during backfill handoff and reopens on return (0.033708ms)\n✔ calendar drawer cannot cover an open schedule dialog (0.023208ms)\n✔ recovery actions distinguish unknown, customer change, and status sync (0.070417ms)\n✔ only path A can compensate its created order and expired recovery only abandons (0.044542ms)\n✔ editing keeps the current scheduled order visible when it is excluded from candidates (0.035833ms)\n✔ status sync searches refreshed customer pages before stable unfiltered fallback (0.134708ms)\n✔ scheduled and later non-cancelled states satisfy status sync (0.03425ms)\nℹ tests 32\nℹ suites 0\nℹ pass 32\nℹ fail 0\nℹ cancelled 0\nℹ skipped 0\nℹ todo 0\nℹ duration_ms 142.516667\ncd frontend && npm run test:avatar-layout\n\n> frontend@0.0.0 test:avatar-layout\n> node --test scripts/avatar-layout.test.mjs\n\n✔ customer profile flex growth excludes the avatar (0.445167ms)\nℹ tests 1\nℹ suites 0\nℹ pass 1\nℹ fail 0\nℹ cancelled 0\nℹ skipped 0\nℹ todo 0\nℹ duration_ms 35.420916\ncd frontend && npm run test:telegram-digest\n\n> frontend@0.0.0 test:telegram-digest\n> node --test --experimental-transform-types scripts/telegram-digest.test.ts\n\n(node:34226) ExperimentalWarning: Transform Types is an experimental feature and might change at any time\n(Use `node --trace-warnings ...` to show where the warning was created)\n✔ bind-token API wrapper posts to the generated contract and never persists response (9.761ms)\n✔ deep-link opener uses noopener,noreferrer and reports popup blocking (0.112875ms)\n✔ Settings page exposes binding states without rendering secrets or storage/log sinks (2.386459ms)\n✔ Telegram binding card keeps mobile wrapping and keyboard focus contracts (0.523667ms)\nℹ tests 4\nℹ suites 0\nℹ pass 4\nℹ fail 0\nℹ cancelled 0\nℹ skipped 0\nℹ todo 0\nℹ duration_ms 100.818792\ncd frontend && npm run test:data-export\n\n> frontend@0.0.0 test:data-export\n> node --test --experimental-transform-types scripts/data-export.test.ts\n\n(node:34278) ExperimentalWarning: Transform Types is an experimental feature and might change at any time\n(Use `node --trace-warnings ...` to show where the warning was created)\n✔ fetchDataExport sends Bearer and returns a complete JSON Blob with the exact server filename (11.802958ms)\n✔ fetchDataExport accepts only application/json base media type with legal parameters (1.823792ms)\n✔ fetchDataExport accepts only the whole fixed filename and otherwise uses a safe UTC fallback (2.572167ms)\n✔ fetchDataExport reuses ApiError for 500 and 401 responses (0.409042ms)\n✔ body/blob rejection returns no result and download collaboration creates no object URL (0.157041ms)\n✔ download collaboration triggers once and always releases its object URL (0.37ms)\n✔ DataExportCard exposes loading, retry, 401, PII, avatar, and persistence-safe contracts (1.324708ms)\n✔ DataExportCard relies on native button keyboard behavior and shared mobile-safe styles (0.495625ms)\nℹ tests 8\nℹ suites 0\nℹ pass 8\nℹ fail 0\nℹ cancelled 0\nℹ skipped 0\nℹ todo 0\nℹ duration_ms 105.164834\ncd backend && go tool oapi-codegen -config oapi-codegen.yaml ../api/openapi.yaml\ncd frontend && npm run generate\n\n> frontend@0.0.0 generate\n> openapi-typescript ../api/openapi.yaml -o src/api/schema.d.ts\n\n✨ openapi-typescript 7.13.0\n🚀 ../api/openapi.yaml → src/api/schema.d.ts [67.6ms]\ngit diff --exit-code -- backend/internal/platform/httpapi/api.gen.go frontend/src/api/schema.d.ts\n",
      "stderr": "[plugin builtin:vite-reporter] \n(!) Some chunks are larger than 500 kB after minification. Consider:\n- Using dynamic import() to code-split the application\n- Use build.rolldownOptions.output.codeSplitting to improve chunking: https://rolldown.rs/reference/OutputOptions.codeSplitting\n- Adjust chunk size limit for this warning via build.chunkSizeWarningLimit.\n",
      "id": "CMD-001",
      "core": true,
      "failure_handling": "fix-or-block"
    },
    {
      "command": "make generate && git diff --exit-code -- backend/internal/platform/httpapi/api.gen.go frontend/src/api/schema.d.ts",
      "exit_code": 0,
      "stdout": "cd backend && go tool oapi-codegen -config oapi-codegen.yaml ../api/openapi.yaml\ncd frontend && npm run generate\n\n> frontend@0.0.0 generate\n> openapi-typescript ../api/openapi.yaml -o src/api/schema.d.ts\n\n✨ openapi-typescript 7.13.0\n🚀 ../api/openapi.yaml → src/api/schema.d.ts [115.8ms]\n",
      "stderr": "",
      "id": "CMD-002",
      "core": true,
      "failure_handling": "fix-or-block"
    },
    {
      "command": "cd backend && go test ./internal/platform/store/... ./internal/dataexport/... ./internal/platform/httpapi/... ./cmd/server/... -count=1 -parallel=1",
      "exit_code": 0,
      "stdout": "ok  \tgithub.com/samson/customer-manage-platform/backend/internal/platform/store\t59.191s\nok  \tgithub.com/samson/customer-manage-platform/backend/internal/dataexport\t36.969s\nok  \tgithub.com/samson/customer-manage-platform/backend/internal/platform/httpapi\t73.700s\nok  \tgithub.com/samson/customer-manage-platform/backend/cmd/server\t1.018s\n",
      "stderr": "",
      "id": "CMD-003",
      "core": true,
      "failure_handling": "fix-or-block"
    },
    {
      "command": "cd frontend && npm run test:data-export && npm run build",
      "exit_code": 0,
      "stdout": "\n> frontend@0.0.0 test:data-export\n> node --test --experimental-transform-types scripts/data-export.test.ts\n\n(node:36070) ExperimentalWarning: Transform Types is an experimental feature and might change at any time\n(Use `node --trace-warnings ...` to show where the warning was created)\n✔ fetchDataExport sends Bearer and returns a complete JSON Blob with the exact server filename (34.492ms)\n✔ fetchDataExport accepts only application/json base media type with legal parameters (3.746958ms)\n✔ fetchDataExport accepts only the whole fixed filename and otherwise uses a safe UTC fallback (50.202709ms)\n✔ fetchDataExport reuses ApiError for 500 and 401 responses (2.813166ms)\n✔ body/blob rejection returns no result and download collaboration creates no object URL (1.220667ms)\n✔ download collaboration triggers once and always releases its object URL (1.144666ms)\n✔ DataExportCard exposes loading, retry, 401, PII, avatar, and persistence-safe contracts (5.131208ms)\n✔ DataExportCard relies on native button keyboard behavior and shared mobile-safe styles (6.354042ms)\nℹ tests 8\nℹ suites 0\nℹ pass 8\nℹ fail 0\nℹ cancelled 0\nℹ skipped 0\nℹ todo 0\nℹ duration_ms 327.555209\n\n> frontend@0.0.0 build\n> tsc -b && vite build\n\nvite v8.1.3 building client environment for production...\n\u001b[2K\ntransforming...✓ 69 modules transformed.\nrendering chunks...\ncomputing gzip size...\ndist/index.html                   0.75 kB │ gzip:   0.44 kB\ndist/assets/index-xL3zlJ_T.css   31.42 kB │ gzip:   6.89 kB\ndist/assets/index-Dup2l4HW.js   555.35 kB │ gzip: 163.95 kB\n\n✓ built in 278ms\n",
      "stderr": "[plugin builtin:vite-reporter] \n(!) Some chunks are larger than 500 kB after minification. Consider:\n- Using dynamic import() to code-split the application\n- Use build.rolldownOptions.output.codeSplitting to improve chunking: https://rolldown.rs/reference/OutputOptions.codeSplitting\n- Adjust chunk size limit for this warning via build.chunkSizeWarningLimit.\n",
      "id": "CMD-004",
      "core": true,
      "failure_handling": "fix-or-block"
    }
  ],
  "providers": {}
}
```

## 3. Validation Commands

Extracted from checklist `dod.commands`; see DoD Results for command status.

## 4. Scope And Cleanliness

Design bytes: 30207
Checklist bytes: 7789

## 5. Residual Risks

- Machine-runner residual warnings: none；当前 canonical DoD 的 CMD-001～CMD-004 均 exit 0。
- Feature residual risks: A13 的 React 401/500/Blob rejection/快速重复触发/Settings failure 仍需 QA 真实浏览器执行；A14 的精确 viewport、DPR、键盘与 PNG 元数据仍需 QA 重拍/复核。
- 已批准边界：同步全内存物化没有文件大小、耗时或并发硬上限；headers 写出后的 transport 中断不可逆；头像 URL 是当前部署的 reference-only 引用，不承诺跨部署恢复。
- 非阻塞建议：typed-nil Clock/Repository 与 partial writer 额外覆盖尚未处理，保留给 acceptance residual risk；真正取消请求的通用 request status 可能仍显示默认 200，应结合专用 cancellation event 判断。

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
  "warnings": [],
  "evidence": [
    {
      "changed_files": [
        ".codestable/roadmap/photographer-private-crm/photographer-private-crm-items.yaml",
        ".codestable/roadmap/photographer-private-crm/photographer-private-crm-roadmap.md",
        "Makefile",
        "api/openapi.yaml",
        "backend/cmd/server/main.go",
        "backend/internal/platform/httpapi/api.gen.go",
        "backend/internal/platform/httpapi/auth.go",
        "backend/internal/platform/httpapi/customers_test.go",
        "backend/internal/platform/httpapi/router.go",
        "backend/internal/platform/httpapi/router_test.go",
        "backend/internal/platform/store/scope.go",
        "backend/internal/settings/service.go",
        "backend/oapi-codegen.yaml",
        "frontend/package.json",
        "frontend/src/api/client.ts",
        "frontend/src/api/schema.d.ts",
        "frontend/src/pages/SettingsPage.tsx",
        ".codestable/features/2026-07-21-data-export/data-export-checklist.yaml",
        ".codestable/features/2026-07-21-data-export/data-export-design-review.md",
        ".codestable/features/2026-07-21-data-export/data-export-design.md",
        ".codestable/features/2026-07-21-data-export/data-export-dod-contract-results.json",
        ".codestable/features/2026-07-21-data-export/data-export-dod-results-cmd001-retry.json",
        ".codestable/features/2026-07-21-data-export/data-export-evidence-pack.md",
        ".codestable/features/2026-07-21-data-export/data-export-implementation.md",
        ".codestable/features/2026-07-21-data-export/data-export-review.md",
        ".codestable/features/2026-07-21-data-export/evidence/data-export-api-sample.md",
        ".codestable/features/2026-07-21-data-export/evidence/data-export-browser-capture-metadata.md",
        ".codestable/features/2026-07-21-data-export/evidence/data-export-settings-375.png",
        ".codestable/features/2026-07-21-data-export/evidence/data-export-settings-desktop.png",
        ".codestable/features/2026-07-21-data-export/goal-plan.md",
        ".codestable/features/2026-07-21-data-export/goal-protocol.md",
        ".codestable/features/2026-07-21-data-export/goal-state.yaml",
        "backend/internal/dataexport/model.go",
        "backend/internal/dataexport/repository.go",
        "backend/internal/dataexport/repository_test.go",
        "backend/internal/dataexport/service.go",
        "backend/internal/dataexport/service_test.go",
        "backend/internal/platform/httpapi/data_export.go",
        "backend/internal/platform/httpapi/data_export_route_test.go",
        "backend/internal/platform/httpapi/data_export_test.go",
        "backend/internal/platform/store/scope_tx.go",
        "backend/internal/platform/store/scope_tx_integration_test.go",
        "frontend/scripts/data-export.test.ts",
        "frontend/src/components/DataExportCard.tsx",
        "frontend/src/components/dataExportDownload.ts"
      ],
      "ignored_machine_artifacts": [
        ".codestable/features/2026-07-21-data-export/data-export-dod-results.json",
        ".codestable/features/2026-07-21-data-export/data-export-evidence-pack-results.json",
        ".codestable/features/2026-07-21-data-export/data-export-gate-results.json"
      ],
      "allowed_prefixes": [
        ".codestable/features/2026-07-21-data-export",
        ".codestable/features/2026-07-21-data-export",
        ".codestable/roadmap/photographer-private-crm",
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
