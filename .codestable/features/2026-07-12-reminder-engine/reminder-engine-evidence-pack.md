---
doc_type: feature-evidence-pack
feature: 2026-07-12-reminder-engine
created: 2026-07-13
---

# reminder-engine implementation evidence pack

## Scope

- Design: approved
- Checklist steps: S1–S8 all `done`
- Branch: `feat/reminder-engine`
- Baseline: `7549150ae4da89929e27f00fdf18587267364019`

## Commands run

| ID | Command | Result |
|---|---|---|
| CMD-001 partial | `go build ./...` + `golangci-lint run ./...` + frontend lint/build + `go test ./... -count=1 -parallel=1` | passed (2026-07-13) |
| CMD-002 | `make generate` twice (idempotent) | passed; `generate-check` vs HEAD fails until commit (expected) |
| CMD-003 | `cd backend && go test ./... -count=1 -parallel=1` | passed |

## Step evidence (summary)

- S1: OpenAPI `reminder-engine` tag + include-tags; bind-token/dashboard 404 tests updated
- S2: migrations 0009/0010; settings + reminder packages
- S3: settings Get/Patch + AccountTimezoneProvider via settings.Service
- S4: `go test ./internal/reminder/...` — idempotent scan, birthday/follow_up/churn, auto-dismiss
- S5: handlers + router registration
- S6: ScanRunner + dual-runner lifecycle in main
- S7: Merge reassigns reminders
- S8: frontend pages + customer tab; tsc/oxlint/vite build green

## Changed files (implementation)

See `git status` / untracked `backend/internal/{reminder,settings}/`, handlers, migrations, frontend pages.

## Notes

- Full `make check` fails `generate-check` only because generated files differ from HEAD (not yet committed).
- Browser screenshots for scenarios 21–23 deferred to QA.
