package reminder_test

import (
	"bytes"
	"encoding/binary"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/samson/customer-manage-platform/backend/internal/reminder"
)

func TestCanonicalFingerprintSegmentBoundariesAndControls(t *testing.T) {
	baseMembers := []reminder.CanonicalGroupMemberV1{{
		AssignmentID: "asgn_a", AssignmentRevision: 1, ContentFingerprint: "fp1",
	}}
	base := reminder.CanonicalPlanAssignmentReminderGroupV1Input{
		AccountID: "acct", PlanID: "plan", OrderID: "ord", SlotID: "slot",
		DueDate: "2026-08-20", LeadRuleVersion: "platform-default-v1",
		Members: baseMembers,
	}
	left := reminder.CanonicalPlanAssignmentReminderGroupV1(base)

	// 分段边界：account/plan 字段拼接歧义必须被 length prefix 打破。
	ambigA := base
	ambigA.AccountID = "ac"
	ambigA.PlanID = "ctplan"
	ambigB := base
	ambigB.AccountID = "acct"
	ambigB.PlanID = "plan"
	if bytes.Equal(
		reminder.CanonicalPlanAssignmentReminderGroupV1(ambigA),
		reminder.CanonicalPlanAssignmentReminderGroupV1(ambigB),
	) {
		t.Fatal("ambiguous concatenation must not collide under length-prefix")
	}

	controls := []string{
		"a/b",
		"a<>&b",
		"line\u2028break",
		"para\u2029break",
		"中文昵称不是收件人",
		"with\x00null",
		"with\nnewline",
	}
	seen := map[string]struct{}{}
	for _, c := range controls {
		in := base
		in.Members = []reminder.CanonicalGroupMemberV1{{
			AssignmentID: c, AssignmentRevision: 2, ContentFingerprint: "fp/" + c,
		}}
		canon := reminder.CanonicalPlanAssignmentReminderGroupV1(in)
		fp := reminder.GroupFingerprintSHA256(canon)
		if len(fp) != 64 {
			t.Fatalf("fingerprint len=%d want 64", len(fp))
		}
		if _, ok := seen[fp]; ok {
			t.Fatalf("duplicate fingerprint for control %q", c)
		}
		seen[fp] = struct{}{}
		if !bytes.Contains(canon, []byte(c)) && utf8.ValidString(c) {
			// control may include null; still require length frame presence
			t.Fatalf("canonical bytes missing field payload for %q", c)
		}
	}

	// member 排序稳定：逆序输入相同 fingerprint。
	unsorted := base
	unsorted.Members = []reminder.CanonicalGroupMemberV1{
		{AssignmentID: "asgn_b", AssignmentRevision: 1, ContentFingerprint: "fpb"},
		{AssignmentID: "asgn_a", AssignmentRevision: 1, ContentFingerprint: "fpa"},
	}
	sorted := base
	sorted.Members = []reminder.CanonicalGroupMemberV1{
		{AssignmentID: "asgn_a", AssignmentRevision: 1, ContentFingerprint: "fpa"},
		{AssignmentID: "asgn_b", AssignmentRevision: 1, ContentFingerprint: "fpb"},
	}
	if reminder.GroupFingerprintSHA256(reminder.CanonicalPlanAssignmentReminderGroupV1(unsorted)) !=
		reminder.GroupFingerprintSHA256(reminder.CanonicalPlanAssignmentReminderGroupV1(sorted)) {
		t.Fatal("member sort must be stable for fingerprint")
	}

	// length prefix is big-endian uint32
	var length uint32
	_ = binary.Read(bytes.NewReader(left[:4]), binary.BigEndian, &length)
	if int(length) != len("plan-assignment-reminder-group.v1") {
		t.Fatalf("version length prefix = %d", length)
	}
	_ = left
}

func TestComputeAssignmentDueDateCalendarDaysNot24h(t *testing.T) {
	// America/New_York spring-forward: 2026-03-08 02:00 does not exist; use slot after.
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	slot := time.Date(2026, 3, 10, 10, 0, 0, 0, loc) // local shoot date 2026-03-10
	due, validUntil, err := reminder.ComputeAssignmentDueDate(slot, "America/New_York", 3, now)
	if err != nil {
		t.Fatal(err)
	}
	if got := reminder.FormatDate(due); got != "2026-03-07" {
		t.Fatalf("due=%s want 2026-03-07 (calendar days)", got)
	}
	if !validUntil.Equal(slot.UTC()) {
		t.Fatalf("valid_until=%s want %s", validUntil, slot.UTC())
	}

	// StartAt <= now → not schedulable
	if _, _, err := reminder.ComputeAssignmentDueDate(now, "Asia/Shanghai", 1, now); err == nil {
		t.Fatal("expected error when StartAt <= now")
	}
}

func TestReduceDesiredReminderGroupsEligibilityAndGrouping(t *testing.T) {
	now := time.Date(2026, 8, 14, 8, 0, 0, 0, time.UTC)
	slotStart := time.Date(2026, 8, 20, 2, 0, 0, 0, time.UTC) // Asia/Shanghai local 2026-08-20 10:00
	orderID := "ord_1"
	slotID := "slot_1"
	lead3 := 3
	rule := "platform-default-v1"
	readyID := "ready_1"
	readyID2 := "ready_2"

	baseReady := reminder.SafeAssignmentInput{
		AssignmentID: "asgn_1", AssignmentRevision: 1, AssignmentKind: reminder.AssignmentKindReadiness,
		SourceState: reminder.SourceStateActive, ReadinessItemID: &readyID,
		ContentSnapshot: "妆造准备", ContentFingerprint: "fp_ready_1",
		PreparationLeadDaysSnapshot: &lead3, LeadRuleVersion: &rule,
		SourceOccurredAt: now,
	}
	baseReady2 := baseReady
	baseReady2.AssignmentID = "asgn_2"
	baseReady2.ReadinessItemID = &readyID2
	baseReady2.ContentSnapshot = "服装准备"
	baseReady2.ContentFingerprint = "fp_ready_2"

	sidecar := reminder.PlanOrderSlotSidecar{
		OrderID: &orderID, OrderActive: true, SlotID: &slotID,
		SlotType: "shoot", SlotStartAt: &slotStart,
	}

	out, err := reminder.ReduceDesiredReminderGroups(reminder.ReduceDesiredReminderGroupsInput{
		AccountID: "acct", PlanID: "plan", Assignments: []reminder.SafeAssignmentInput{baseReady2, baseReady},
		Sidecar: sidecar, Timezone: "Asia/Shanghai", Now: now, ActivationGeneration: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Groups) != 1 || len(out.Groups[0].Members) != 2 {
		t.Fatalf("want 1 group with 2 members, got %+v", out.Groups)
	}
	if reminder.FormatDate(out.Groups[0].DueDate) != "2026-08-17" {
		t.Fatalf("due=%s want 2026-08-17", reminder.FormatDate(out.Groups[0].DueDate))
	}
	if out.Groups[0].Members[0].AssignmentID != "asgn_1" || out.Groups[0].Members[0].Position != 0 {
		t.Fatalf("members must sort by assignment_id: %+v", out.Groups[0].Members)
	}
	for _, s := range out.Sources {
		if s.ProjectionState != reminder.ProjectionStateGrouped {
			t.Fatalf("source %s state=%s", s.AssignmentID, s.ProjectionState)
		}
	}

	// on-site ineligible; no formal readiness → no group
	onSite := reminder.SafeAssignmentInput{
		AssignmentID: "asgn_os", AssignmentRevision: 1,
		AssignmentKind: reminder.AssignmentKindOnSiteSupport, SourceState: reminder.SourceStateActive,
		ContentSnapshot: "现场协助", ContentFingerprint: "fp_os", SourceOccurredAt: now,
	}
	out2, err := reminder.ReduceDesiredReminderGroups(reminder.ReduceDesiredReminderGroupsInput{
		AccountID: "acct", PlanID: "plan", Assignments: []reminder.SafeAssignmentInput{onSite},
		Sidecar: sidecar, Timezone: "Asia/Shanghai", Now: now, ActivationGeneration: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out2.Groups) != 0 || out2.Sources[0].ProjectionState != reminder.ProjectionStateIneligibleOnSite {
		t.Fatalf("on-site must be ineligible without groups: %+v", out2)
	}

	// no future slot → unscheduled
	past := now.Add(-time.Hour)
	sidecarPast := sidecar
	sidecarPast.SlotStartAt = &past
	out3, err := reminder.ReduceDesiredReminderGroups(reminder.ReduceDesiredReminderGroupsInput{
		AccountID: "acct", PlanID: "plan", Assignments: []reminder.SafeAssignmentInput{baseReady},
		Sidecar: sidecarPast, Timezone: "Asia/Shanghai", Now: now, ActivationGeneration: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out3.Groups) != 0 || out3.Sources[0].ProjectionState != reminder.ProjectionStateUnscheduled {
		t.Fatalf("past slot must unscheduled: %+v", out3)
	}

	// different lead rule → two groups
	lead7 := 7
	rule7 := "readiness-item-v1"
	other := baseReady2
	other.PreparationLeadDaysSnapshot = &lead7
	other.LeadRuleVersion = &rule7
	out4, err := reminder.ReduceDesiredReminderGroups(reminder.ReduceDesiredReminderGroupsInput{
		AccountID: "acct", PlanID: "plan", Assignments: []reminder.SafeAssignmentInput{baseReady, other},
		Sidecar: sidecar, Timezone: "Asia/Shanghai", Now: now, ActivationGeneration: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out4.Groups) != 2 {
		t.Fatalf("different lead rules must split groups: %d", len(out4.Groups))
	}

	// late claim: due in the past is still created (overdue pending)
	nearSlot := now.Add(2 * time.Hour)
	sidecarNear := sidecar
	sidecarNear.SlotStartAt = &nearSlot
	out5, err := reminder.ReduceDesiredReminderGroups(reminder.ReduceDesiredReminderGroupsInput{
		AccountID: "acct", PlanID: "plan", Assignments: []reminder.SafeAssignmentInput{baseReady},
		Sidecar: sidecarNear, Timezone: "Asia/Shanghai", Now: now, ActivationGeneration: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out5.Groups) != 1 {
		t.Fatalf("late claim must still create overdue group, got %d", len(out5.Groups))
	}
}

func TestDiffDesiredAgainstCurrentTruthTable(t *testing.T) {
	due := time.Date(2026, 8, 17, 0, 0, 0, 0, time.UTC)
	valid := time.Date(2026, 8, 20, 2, 0, 0, 0, time.UTC)
	desired := reminder.DesiredReminderGroup{
		OrderID: "ord", SlotID: "slot", DueDate: due, TimezoneSnapshot: "Asia/Shanghai",
		ValidUntil: valid, LeadRuleVersion: "platform-default-v1",
		GroupFingerprint:     "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		ActivationGeneration: 2,
	}

	curPending := reminder.CurrentReminderGroup{
		GroupID: "g1", PlanID: "plan", OrderID: "ord", SlotID: "slot", DueDate: due,
		TimezoneSnapshot: "Asia/Shanghai", ValidUntil: valid, LeadRuleVersion: "platform-default-v1",
		GroupFingerprint: desired.GroupFingerprint, ValidityRevision: 1, State: reminder.GroupStateCurrent,
		ReminderID: "r1", ReminderStatus: reminder.StatusPending, ActivationGeneration: 1,
	}

	// exact same → no-op
	plan := reminder.DiffDesiredAgainstCurrent([]reminder.CurrentReminderGroup{curPending}, []reminder.DesiredReminderGroup{desired}, nil)
	if len(plan.NoOpGroupIDs) != 1 || len(plan.Creates)+len(plan.Withdrawals)+len(plan.TemporalUpdates) != 0 {
		t.Fatalf("exact same must no-op: %+v", plan)
	}

	// fingerprint same, valid_until changed → temporal update
	desired2 := desired
	desired2.ValidUntil = valid.Add(30 * time.Minute)
	plan = reminder.DiffDesiredAgainstCurrent([]reminder.CurrentReminderGroup{curPending}, []reminder.DesiredReminderGroup{desired2}, nil)
	if len(plan.TemporalUpdates) != 1 || plan.TemporalUpdates[0].NextValidityRev != 2 {
		t.Fatalf("temporal metadata change: %+v", plan)
	}

	// fingerprint change pending → withdraw+dismiss + create
	desired3 := desired
	desired3.GroupFingerprint = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	plan = reminder.DiffDesiredAgainstCurrent([]reminder.CurrentReminderGroup{curPending}, []reminder.DesiredReminderGroup{desired3}, nil)
	if len(plan.Withdrawals) != 1 || !plan.Withdrawals[0].Dismiss || len(plan.Creates) != 1 {
		t.Fatalf("pending fingerprint change: %+v", plan)
	}

	// done → fingerprint change: withdraw without dismiss + create
	curDone := curPending
	curDone.ReminderStatus = reminder.StatusDone
	plan = reminder.DiffDesiredAgainstCurrent([]reminder.CurrentReminderGroup{curDone}, []reminder.DesiredReminderGroup{desired3}, nil)
	if len(plan.Withdrawals) != 1 || plan.Withdrawals[0].Dismiss || len(plan.Creates) != 1 {
		t.Fatalf("done fingerprint change must not dismiss: %+v", plan)
	}

	// withdrawn history only → create new (no current)
	plan = reminder.DiffDesiredAgainstCurrent(nil, []reminder.DesiredReminderGroup{desired}, nil)
	if len(plan.Creates) != 1 {
		t.Fatalf("recurrence after withdraw must create: %+v", plan)
	}
}
