---
doc_type: feature-evidence-pack
feature: telegram-digest
status: generated
---

# telegram-digest evidence pack

## 1. Scope

- Design: `.codestable/features/2026-07-15-telegram-digest/telegram-digest-design.md`
- Checklist: `.codestable/features/2026-07-15-telegram-digest/telegram-digest-checklist.yaml`

## 2. DoD Results

```json
{
  "gate_id": "dod-runner",
  "stage": "implementation.before_review",
  "status": "passed",
  "blocking": [],
  "warnings": [
    "Vite reports the existing/general minified chunk >500kB warning; build exits 0."
  ],
  "evidence": [
    {
      "id": "CMD-001",
      "command": "make check",
      "exit_code": 0,
      "core": true,
      "note": "Second post-hardening run passed with serialized Testcontainers."
    },
    {
      "id": "CMD-002",
      "command": "make generate && git diff --exit-code -- backend/internal/platform/httpapi/api.gen.go frontend/src/api/schema.d.ts",
      "exit_code": 0,
      "core": true
    },
    {
      "id": "CMD-003",
      "command": "cd backend && go test ./internal/reminder/... ./internal/settings/... ./internal/platform/config/... ./internal/platform/httpapi/... ./cmd/server/... -count=1 -parallel=1",
      "exit_code": 0,
      "core": true
    },
    {
      "id": "CMD-004",
      "command": "cd frontend && npm run test:telegram-digest",
      "exit_code": 0,
      "core": true,
      "note": "4/4 tests passed."
    },
    {
      "id": "CMD-005",
      "command": "git diff --check",
      "exit_code": 0,
      "core": true
    }
  ],
  "providers": {}
}
```

## 3. Validation Commands

Extracted from checklist `dod.commands`; see DoD Results for command status.

## 4. Scope And Cleanliness

Design bytes: 43420
Checklist bytes: 12759

## 5. Residual Risks

- Vite reports the existing/general minified chunk >500kB warning; build exits 0.

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
        "backend/cmd/server/main.go",
        "backend/cmd/server/main_test.go",
        ".codestable/features/2026-07-15-telegram-digest/evidence/settings-telegram-375-error.jpg",
        ".codestable/features/2026-07-15-telegram-digest/goal-plan.md",
        ".codestable/features/2026-07-15-telegram-digest/goal-protocol.md",
        ".codestable/features/2026-07-15-telegram-digest/goal-state.yaml",
        ".codestable/features/2026-07-15-telegram-digest/telegram-digest-checklist.yaml",
        ".codestable/features/2026-07-15-telegram-digest/telegram-digest-design-review.md",
        ".codestable/features/2026-07-15-telegram-digest/telegram-digest-design.md",
        ".codestable/features/2026-07-15-telegram-digest/telegram-digest-implementation.md",
        "backend/internal/reminder/digest/adapters.go",
        "backend/internal/reminder/digest/binding.go",
        "backend/internal/reminder/digest/binding_repository.go",
        "backend/internal/reminder/digest/binding_test.go",
        "backend/internal/reminder/digest/cleanup_test.go",
        "backend/internal/reminder/digest/daily.go",
        "backend/internal/reminder/digest/daily_test.go",
        "backend/internal/reminder/digest/fake.go",
        "backend/internal/reminder/digest/gate.go",
        "backend/internal/reminder/digest/gate_test.go",
        "backend/internal/reminder/digest/integration_state.go",
        "backend/internal/reminder/digest/integration_state_test.go",
        "backend/internal/reminder/digest/message_builder.go",
        "backend/internal/reminder/digest/message_builder_test.go",
        "backend/internal/reminder/digest/model.go",
        "backend/internal/reminder/digest/policy.go",
        "backend/internal/reminder/digest/policy_test.go",
        "backend/internal/reminder/digest/poller.go",
        "backend/internal/reminder/digest/poller_test.go",
        "backend/internal/reminder/digest/renderer.go",
        "backend/internal/reminder/digest/repository.go",
        "backend/internal/reminder/digest/repository_test.go",
        "backend/internal/reminder/digest/resolver_test.go",
        "backend/internal/reminder/digest/retry_test.go",
        "backend/internal/reminder/digest/runner.go",
        "backend/internal/reminder/digest/runner_test.go",
        "backend/internal/reminder/digest/sender.go",
        "backend/internal/reminder/digest/sender_runner.go",
        "backend/internal/reminder/digest/sender_runner_test.go",
        "backend/internal/reminder/digest/sender_test.go",
        "backend/internal/reminder/digest/snapshot.go",
        "backend/internal/reminder/digest/snapshot_test.go",
        "backend/internal/reminder/digest/telegram.go",
        "backend/internal/reminder/digest/telegram/client.go",
        "backend/internal/reminder/digest/telegram/client_test.go",
        "backend/internal/reminder/digest/telegram_test.go",
        "backend/internal/reminder/digest/update_handler.go",
        "backend/internal/reminder/digest/update_handler_test.go"
      ],
      "ignored_machine_artifacts": [
        ".codestable/features/2026-07-15-telegram-digest/telegram-digest-dod-results.json",
        ".codestable/features/2026-07-15-telegram-digest/telegram-digest-gate-results.json"
      ],
      "allowed_prefixes": [
        ".codestable/features/2026-07-15-telegram-digest",
        "backend/internal/reminder/digest",
        "backend/cmd/server"
      ]
    }
  ],
  "providers": {}
}
```
