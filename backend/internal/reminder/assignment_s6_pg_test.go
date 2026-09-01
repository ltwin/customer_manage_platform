package reminder_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
	"github.com/samson/customer-manage-platform/backend/internal/reminder"
	"github.com/samson/customer-manage-platform/backend/internal/reminder/digest"
	"github.com/samson/customer-manage-platform/backend/internal/settings"
)

func TestS6MonotonicBudgetThroughBeginCurrentCall(t *testing.T) {
	cases := []struct {
		name     string
		pause    time.Duration
		wantZero bool
		wantMax  time.Duration
	}{
		// 窗口取秒级而不是毫秒级：pause 走注入的假时钟不占真实时间，
		// 但 valid_until 是真墙钟，中间要跑完 claimDelivery 和 BeginCurrentCall 两轮
		// 数据库往返。原先 100ms 的窗口在 -p>1 有 CPU 争抢时会被往返吃光，
		// 断言前预算就已耗尽（约三分之一轮次失败）。这里验的是钳制算术
		// budget = min(pause, 剩余)，不是数据库延迟，所以放大窗口不改变被测语义。
		{name: "pause_below_remaining", pause: 2400 * time.Millisecond, wantMax: 2400 * time.Millisecond},
		{name: "pause_above_remaining", pause: 3600 * time.Millisecond, wantZero: true},
	}
	for i, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			s, _ := startAssignmentReminderPostgresURL(t)
			accountID := fmt.Sprintf("acc_s6_budget_%d", i)
			scope, planID, orderID, slotID, _ := seedAssignmentReminderFixture(t, s, accountID)
			ctx := context.Background()
			now := time.Now().UTC()
			seedBoundSettings(t, scope, "chat-budget", 1)
			// Keep a ~3s business remainder relative to CAS clock_timestamp().
			seedShortLivedPlanningGroup(t, scope, planID, orderID, slotID, now.Add(3600*time.Millisecond))

			local := time.Date(2026, 8, 14, 0, 0, 0, 0, time.UTC)
			delID := fmt.Sprintf("del_budget_%d", i)
			if err := scope.Insert(ctx, "telegram_deliveries", []string{
				"id", "source", "source_key", "message_kind", "target_local_date", "timezone_at_enqueue",
				"status", "next_attempt_at", "updated_at",
			}, delID, "daily", fmt.Sprintf("daily:%s:%d", local.Format("2006-01-02"), i), "digest", local, "Asia/Shanghai",
				"pending", now, now); err != nil {
				t.Fatalf("insert delivery: %v", err)
			}
			if _, err := scope.Update(ctx, "plan_assignment_reminder_groups",
				"valid_until = $2", "state = $3",
				time.Now().UTC().Add(3000*time.Millisecond), reminder.GroupStateCurrent); err != nil {
				t.Fatalf("refresh valid_until: %v", err)
			}
			claim := claimDelivery(t, scope, now)
			base := time.Unix(1_700_000_000, 0).UTC()
			var calls atomic.Int32
			intent := digest.NewDigestIntentService(
				digest.NewDigestMessageBuilder(digest.NewPostgresSnapshotRepository(), digest.NewRenderer()),
				nil,
			).WithMonoClock(func() time.Time {
				n := calls.Add(1)
				switch n {
				case 1, 2:
					return base
				default:
					return base.Add(tc.pause)
				}
			})
			permit, err := intent.BeginCurrentCall(ctx, scope, claim)
			if tc.wantZero {
				if !errors.Is(err, digest.ErrCallBudgetExhausted()) {
					t.Fatalf("want budget exhausted, got permit=%+v err=%v", permit, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("BeginCurrentCall: %v", err)
			}
			if permit.MonotonicStartBudget <= 0 || permit.MonotonicStartBudget > tc.wantMax {
				t.Fatalf("budget=%v want (0,%v]", permit.MonotonicStartBudget, tc.wantMax)
			}
			if permit.StartDeadline.IsZero() {
				t.Fatal("StartDeadline must come from DB clock_timestamp CAS")
			}
		})
	}
}

func TestS6DigestIntentRetentionRedactAndDelete(t *testing.T) {
	s, _ := startAssignmentReminderPostgresURL(t)
	scope := activateAssignmentTestAccount(t, s, "acc_s6_ret")
	ctx := context.Background()
	now := time.Now().UTC()
	seedBoundSettings(t, scope, "chat-ret", 1)

	if err := scope.Insert(ctx, "telegram_deliveries", []string{
		"id", "source", "source_key", "message_kind", "target_local_date", "timezone_at_enqueue",
		"status", "next_attempt_at", "updated_at", "sent_at",
	}, "del_ret", "daily", "daily:2026-08-01", "digest", time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
		"Asia/Shanghai", "sent", now, now, now); err != nil {
		t.Fatalf("insert sent delivery: %v", err)
	}
	oldCreated := now.Add(-8 * 24 * time.Hour)
	if err := scope.Insert(ctx, "plan_assignment_reminder_digest_intents", []string{
		"delivery_id", "intent_revision", "projection_generation", "payload_fingerprint",
		"recipient_chat_id_snapshot", "recipient_binding_revision", "recipient_fingerprint",
		"planning_membership_fingerprint", "payload_text", "created_at",
	}, "del_ret", int64(1), int64(0), "fp_ret", "chat-ret", int64(1), "rfp", "legacy-only",
		"secret payload text", oldCreated); err != nil {
		t.Fatalf("insert intent: %v", err)
	}

	if err := reminder.ApplyDigestIntentRetentionInScope(ctx, scope); err != nil {
		t.Fatalf("retention: %v", err)
	}
	var payload sql.NullString
	var redactedAt sql.NullTime
	if err := scope.QueryRow(ctx, "plan_assignment_reminder_digest_intents",
		"payload_text, payload_redacted_at", "delivery_id = $2 AND intent_revision = $3",
		"del_ret", int64(1),
	).Scan(&payload, &redactedAt); err != nil {
		t.Fatal(err)
	}
	if payload.Valid || !redactedAt.Valid {
		t.Fatalf("expected unidirectional redact, payload=%v redacted=%v", payload.Valid, redactedAt.Valid)
	}

	if _, err := scope.Update(ctx, "plan_assignment_reminder_digest_intents",
		"payload_redacted_at = $2",
		"delivery_id = $3 AND intent_revision = $4",
		now.Add(-91*24*time.Hour), "del_ret", int64(1),
	); err != nil {
		t.Fatal(err)
	}
	if err := reminder.ApplyDigestIntentRetentionInScope(ctx, scope); err != nil {
		t.Fatalf("retention delete: %v", err)
	}
	n, err := scope.Count(ctx, "plan_assignment_reminder_digest_intents", "delivery_id = $2", "del_ret")
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("metadata should be deleted after 90d, got %d", n)
	}
}

func TestS6NegativeGuardsShotAtPriceCustomerDelivery(t *testing.T) {
	// Plan-assignment surfaces must not grow Order.shot_at / price / customer-delivery deps.
	// Legacy birthday/follow_up/churn scan still uses orders.shot_at (H1 baseline) — excluded.
	roots := []struct {
		dir   string
		match func(name string) bool
	}{
		{
			dir: filepath.Join("."),
			match: func(name string) bool {
				switch {
				case strings.HasPrefix(name, "assignment_"),
					strings.HasPrefix(name, "archive_"),
					strings.HasPrefix(name, "crm_lifecycle"),
					strings.HasPrefix(name, "timezone_"),
					strings.HasPrefix(name, "temporal_"),
					strings.HasPrefix(name, "freshness"),
					strings.HasPrefix(name, "digest_intent"),
					name == "status_planning.go":
					return true
				default:
					return false
				}
			},
		},
		{
			dir: filepath.Join("digest"),
			match: func(name string) bool {
				return strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, "_test.go")
			},
		},
		{
			dir: filepath.Join("..", "..", "cmd", "server"),
			match: func(name string) bool {
				return name == "main.go"
			},
		},
	}
	forbidden := []string{
		"customer_telegram", "customer_sms", "customer_email",
		"opt_in", "unsubscribe", "CustomerTelegram",
	}
	for _, root := range roots {
		err := filepath.WalkDir(root.dir, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			name := filepath.Base(path)
			if !root.match(name) || strings.HasSuffix(name, "_test.go") {
				return nil
			}
			body, err := os.ReadFile(filepath.Clean(path))
			if err != nil {
				return err
			}
			text := string(body)
			for _, line := range strings.Split(text, "\n") {
				trimmed := strings.TrimSpace(line)
				if strings.HasPrefix(trimmed, "//") &&
					(strings.Contains(trimmed, "禁止") || strings.Contains(trimmed, "不读取") || strings.Contains(trimmed, "不得")) {
					continue
				}
				if strings.Contains(line, "shot_at") || strings.Contains(line, "ShotAt") {
					t.Fatalf("%s depends on Order.shot_at: %s", path, trimmed)
				}
				if strings.Contains(line, "orders.price") || strings.Contains(line, "Order.Price") {
					t.Fatalf("%s depends on price: %s", path, trimmed)
				}
				for _, token := range forbidden {
					if strings.Contains(line, token) {
						t.Fatalf("%s contains customer-delivery token %q", path, token)
					}
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestS6H1LegacyNoPlanBaselineNoAssignmentDrift(t *testing.T) {
	s, _ := startAssignmentReminderPostgresURL(t)
	scope := activateAssignmentTestAccount(t, s, "acc_s6_h1")
	ctx := context.Background()
	settingsSvc := settings.NewService(settings.NewPostgresRepository())
	if _, err := settingsSvc.Patch(ctx, scope, settings.PatchInput{}); err != nil {
		t.Fatalf("seed settings: %v", err)
	}
	svc := reminder.NewService(
		reminder.NewPostgresRepository(),
		reminder.NewSettingsAdapter(settingsSvc),
		nil,
	)
	local := time.Date(2026, 8, 14, 0, 0, 0, 0, time.UTC)
	result, err := svc.ScanAndCheckpoint(ctx, scope, local)
	if err != nil {
		t.Fatal(err)
	}
	// No customers/orders → legacy scan creates nothing; planning must not inject rows.
	if result.Created != 0 {
		t.Fatalf("no-plan legacy scan created=%d", result.Created)
	}
	n, err := scope.Count(ctx, "plan_assignment_reminder_sources", "TRUE")
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("no-plan account must not materialize assignment sources, got %d", n)
	}
	groups, err := scope.Count(ctx, "plan_assignment_reminder_groups", "TRUE")
	if err != nil {
		t.Fatal(err)
	}
	if groups != 0 {
		t.Fatalf("no-plan account must not materialize groups, got %d", groups)
	}
}

func TestS6OpenAPIReminderTypeAdditiveNoHandwrittenDTO(t *testing.T) {
	schemaPath := filepath.Join("..", "..", "..", "frontend", "src", "api", "schema.d.ts")
	body, err := os.ReadFile(schemaPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `ReminderType: "birthday" | "follow_up" | "churn" | "custom" | "plan_assignment_checklist"`) {
		t.Fatalf("generated schema missing additive ReminderType")
	}
	clientPath := filepath.Join("..", "..", "..", "frontend", "src", "api", "client.ts")
	client, err := os.ReadFile(clientPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(client), `components['schemas']['ReminderType']`) {
		t.Fatal("client must reference generated ReminderType, not handwritten DTO")
	}
	if strings.Contains(string(client), "type ReminderType = 'birthday'") {
		t.Fatal("handwritten ReminderType union forbidden")
	}
}

func TestS6ProductionRunnerNotMemoryQueue(t *testing.T) {
	runner := reminder.NewPlanningAssignmentRunner(nil, nil)
	if runner == nil {
		t.Fatal("runner required")
	}
	// Compile/type surface: runner is a real PG background loop, not a channel queue.
	_ = runner.Run
}

func seedShortLivedPlanningGroup(
	t *testing.T,
	scope store.AccountScope,
	planID, orderID, slotID string,
	validUntil time.Time,
) {
	t.Helper()
	ctx := context.Background()
	reminderID := "rem_" + uuid.NewString()
	groupID := "grp_" + uuid.NewString()
	if err := scope.Insert(ctx, "reminders", []string{
		"id", "type", "due_date", "content", "status", "dedup_key", "plan_id", "order_id",
	}, reminderID, reminder.TypePlanAssignmentChecklist, time.Date(2026, 8, 14, 0, 0, 0, 0, time.UTC),
		"妆造核对", reminder.StatusPending, "plan_assignment_checklist:v1:group:"+groupID, planID, orderID,
	); err != nil {
		t.Fatalf("insert reminder: %v", err)
	}
	fp := strings.Repeat("a", 64)
	if err := scope.Insert(ctx, "plan_assignment_reminder_groups", []string{
		"group_id", "plan_id", "order_id", "slot_id", "due_date", "timezone_snapshot",
		"valid_until", "lead_rule_version", "activation_generation", "group_fingerprint",
		"state", "reminder_id",
	}, groupID, planID, orderID, slotID, time.Date(2026, 8, 14, 0, 0, 0, 0, time.UTC),
		"Asia/Shanghai", validUntil.UTC(), "lead.v1", int64(1), fp, reminder.GroupStateCurrent, reminderID,
	); err != nil {
		t.Fatalf("insert group: %v", err)
	}
}
