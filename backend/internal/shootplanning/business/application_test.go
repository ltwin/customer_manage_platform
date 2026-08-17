package business

import (
	"context"
	"reflect"
	"testing"
	"time"
)

func TestBusinessDetailUsesFixedReadBudget(t *testing.T) {
	reader := &detailReaderSpy{}
	app := &Application{detail: reader, now: func() time.Time { return reader.now }}

	detail, err := app.getDetailInScope(context.Background(), nil, "plan-1")
	if err != nil {
		t.Fatal(err)
	}
	if detail.OrderAdjustment != nil || detail.ScheduleDuration != nil {
		t.Fatalf("empty latest-draft projection = %+v", detail)
	}
	want := []string{"plan", "facts", "rules", "targets", "latest_drafts"}
	if !reflect.DeepEqual(reader.calls, want) {
		t.Fatalf("detail read calls = %v, want %v", reader.calls, want)
	}
}

func TestEffectiveDraftStatusPriority(t *testing.T) {
	tests := []struct {
		name       string
		kind       DraftKind
		mode       ScheduleTargetMode
		mutate     func(*DraftRecord, *PlanSource, *FactsView, *EffectiveRules, *CurrentTargets)
		wantStatus DraftStatus
		wantReason *StaleReason
	}{
		{name: "fresh order", kind: DraftOrderAdjustment, wantStatus: StatusFresh},
		{name: "terminal applied wins", kind: DraftOrderAdjustment, mutate: func(r *DraftRecord, _ *PlanSource, _ *FactsView, _ *EffectiveRules, targets *CurrentTargets) {
			r.TerminalStatus = StatusApplied
			r.SupersededByDraftID = stringPtr("newer")
			targets.Order = nil
		}, wantStatus: StatusApplied},
		{name: "terminal dismissed wins", kind: DraftOrderAdjustment, mutate: func(r *DraftRecord, _ *PlanSource, _ *FactsView, _ *EffectiveRules, _ *CurrentTargets) {
			r.TerminalStatus = StatusDismissed
			r.SupersededByDraftID = stringPtr("newer")
		}, wantStatus: StatusDismissed},
		{name: "superseded before expired", kind: DraftOrderAdjustment, mutate: func(r *DraftRecord, _ *PlanSource, _ *FactsView, _ *EffectiveRules, _ *CurrentTargets) {
			r.SupersededByDraftID = stringPtr("newer")
			r.ExpiresAt = r.CreatedAt
		}, wantStatus: StatusStale, wantReason: staleReasonPtr(StaleSuperseded)},
		{name: "expired before plan", kind: DraftOrderAdjustment, mutate: func(r *DraftRecord, plan *PlanSource, _ *FactsView, _ *EffectiveRules, _ *CurrentTargets) {
			r.ExpiresAt = r.CreatedAt
			plan.Revision++
		}, wantStatus: StatusStale, wantReason: staleReasonPtr(StaleExpired)},
		{name: "plan before facts", kind: DraftOrderAdjustment, mutate: func(_ *DraftRecord, plan *PlanSource, facts *FactsView, _ *EffectiveRules, _ *CurrentTargets) {
			plan.Revision++
			facts.Revision++
		}, wantStatus: StatusStale, wantReason: staleReasonPtr(StalePlanRevision)},
		{name: "facts before connection", kind: DraftOrderAdjustment, mutate: func(_ *DraftRecord, _ *PlanSource, facts *FactsView, _ *EffectiveRules, targets *CurrentTargets) {
			facts.Revision++
			targets.CRM.ConnectionRevision++
		}, wantStatus: StatusStale, wantReason: staleReasonPtr(StaleFactsRevision)},
		{name: "connection before projection", kind: DraftOrderAdjustment, mutate: func(_ *DraftRecord, _ *PlanSource, _ *FactsView, _ *EffectiveRules, targets *CurrentTargets) {
			targets.CRM.ConnectionRevision++
			*targets.CRM.ProjectionRevision++
		}, wantStatus: StatusStale, wantReason: staleReasonPtr(StaleCRMConnection)},
		{name: "projection before rule", kind: DraftOrderAdjustment, mutate: func(_ *DraftRecord, _ *PlanSource, _ *FactsView, rules *EffectiveRules, targets *CurrentTargets) {
			*targets.CRM.ProjectionRevision++
			rules.RuleVersion = "rules-v2"
		}, wantStatus: StatusStale, wantReason: staleReasonPtr(StaleCRMProjection)},
		{name: "rule before missing order", kind: DraftOrderAdjustment, mutate: func(_ *DraftRecord, _ *PlanSource, _ *FactsView, rules *EffectiveRules, targets *CurrentTargets) {
			rules.RuleVersion = "rules-v2"
			targets.Order = nil
		}, wantStatus: StatusStale, wantReason: staleReasonPtr(StaleRuleVersion)},
		{name: "order missing", kind: DraftOrderAdjustment, mutate: func(_ *DraftRecord, _ *PlanSource, _ *FactsView, _ *EffectiveRules, targets *CurrentTargets) {
			targets.Order = nil
		}, wantStatus: StatusStale, wantReason: staleReasonPtr(StaleOrderTargetMissing)},
		{name: "order stage before fingerprint", kind: DraftOrderAdjustment, mutate: func(_ *DraftRecord, _ *PlanSource, _ *FactsView, _ *EffectiveRules, targets *CurrentTargets) {
			targets.Order.Status = "cancelled"
			targets.Order.Price = intPtrValue(999)
		}, wantStatus: StatusStale, wantReason: staleReasonPtr(StaleOrderStage)},
		{name: "order fingerprint", kind: DraftOrderAdjustment, mutate: func(_ *DraftRecord, _ *PlanSource, _ *FactsView, _ *EffectiveRules, targets *CurrentTargets) {
			targets.Order.Price = intPtrValue(999)
		}, wantStatus: StatusStale, wantReason: staleReasonPtr(StaleOrderTarget)},
		{name: "missing update target before schedule stage", kind: DraftScheduleDuration, mode: ScheduleUpdateExisting, mutate: func(_ *DraftRecord, _ *PlanSource, _ *FactsView, _ *EffectiveRules, targets *CurrentTargets) {
			targets.Order.Status = "delivered"
			targets.Slot = nil
		}, wantStatus: StatusStale, wantReason: staleReasonPtr(StaleSlotTargetMissing)},
		{name: "schedule stage before changed update target", kind: DraftScheduleDuration, mode: ScheduleUpdateExisting, mutate: func(_ *DraftRecord, _ *PlanSource, _ *FactsView, _ *EffectiveRules, targets *CurrentTargets) {
			targets.Order.Status = "delivered"
			targets.Slot.EndsAt = targets.Slot.EndsAt.Add(time.Minute)
		}, wantStatus: StatusStale, wantReason: staleReasonPtr(StaleScheduleStage)},
		{name: "eligible create new invalidated by new slot", kind: DraftScheduleDuration, mode: ScheduleCreateNew, mutate: func(_ *DraftRecord, _ *PlanSource, _ *FactsView, _ *EffectiveRules, _ *CurrentTargets) {}, wantStatus: StatusStale, wantReason: staleReasonPtr(StaleSlotTarget)},
		{name: "create new stage before new slot", kind: DraftScheduleDuration, mode: ScheduleCreateNew, mutate: func(_ *DraftRecord, _ *PlanSource, _ *FactsView, _ *EffectiveRules, targets *CurrentTargets) {
			targets.Order.Status = "delivered"
		}, wantStatus: StatusStale, wantReason: staleReasonPtr(StaleScheduleStage)},
		{name: "slot missing", kind: DraftScheduleDuration, mode: ScheduleUpdateExisting, mutate: func(_ *DraftRecord, _ *PlanSource, _ *FactsView, _ *EffectiveRules, targets *CurrentTargets) {
			targets.Slot = nil
		}, wantStatus: StatusStale, wantReason: staleReasonPtr(StaleSlotTargetMissing)},
		{name: "slot fingerprint", kind: DraftScheduleDuration, mode: ScheduleUpdateExisting, mutate: func(_ *DraftRecord, _ *PlanSource, _ *FactsView, _ *EffectiveRules, targets *CurrentTargets) {
			targets.Slot.EndsAt = targets.Slot.EndsAt.Add(time.Minute)
		}, wantStatus: StatusStale, wantReason: staleReasonPtr(StaleSlotTarget)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app, record, plan, facts, rules, targets := staleStatusFixture(t, tt.kind)
			if tt.mutate != nil {
				tt.mutate(&record, &plan, &facts, &rules, &targets)
			}
			status, reason, err := app.effectiveDraftStatus(record, plan, facts, rules, targets, tt.mode)
			if err != nil {
				t.Fatal(err)
			}
			if status != tt.wantStatus || !reflect.DeepEqual(reason, tt.wantReason) {
				t.Fatalf("status = %s reason = %v, want %s %v", status, reason, tt.wantStatus, tt.wantReason)
			}
		})
	}
}

type detailReaderSpy struct {
	calls []string
	now   time.Time
}

func (s *detailReaderSpy) LoadPlanSource(context.Context, readScope, string) (PlanSource, error) {
	s.calls = append(s.calls, "plan")
	return PlanSource{ID: "plan-1", Status: "draft", Revision: 1}, nil
}
func (s *detailReaderSpy) LoadFacts(context.Context, readScope, string) (FactsView, error) {
	s.calls = append(s.calls, "facts")
	return FactsView{}, nil
}
func (s *detailReaderSpy) LoadEffectiveRules(context.Context, readScope) (EffectiveRules, error) {
	s.calls = append(s.calls, "rules")
	return EffectiveRules{RuleVersion: "rules-v1"}, nil
}
func (s *detailReaderSpy) LoadCurrentTargets(context.Context, readScope, string) (CurrentTargets, error) {
	s.calls = append(s.calls, "targets")
	return CurrentTargets{}, nil
}
func (s *detailReaderSpy) LoadLatestDrafts(context.Context, readScope, string) (map[DraftKind]DraftRecord, error) {
	s.calls = append(s.calls, "latest_drafts")
	return map[DraftKind]DraftRecord{}, nil
}

func staleStatusFixture(t *testing.T, kind DraftKind) (*Application, DraftRecord, PlanSource, FactsView, EffectiveRules, CurrentTargets) {
	t.Helper()
	now := time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)
	sourceProjectionRevision := int64(4)
	currentProjectionRevision := int64(4)
	order := OrderTarget{ID: "order-1", CustomerID: "customer-1", Status: "consulting", Price: intPtrValue(100)}
	start := now.Add(24 * time.Hour)
	slot := ScheduleTarget{ID: "slot-1", Type: "shoot", OrderID: order.ID, StartsAt: start, EndsAt: start.Add(time.Hour)}
	orderFingerprint, _, err := FingerprintOrderTarget(order)
	if err != nil {
		t.Fatal(err)
	}
	slotFingerprint, _, err := FingerprintScheduleTarget(slot)
	if err != nil {
		t.Fatal(err)
	}
	record := DraftRecord{
		ID: "draft-1", PlanID: "plan-1", Kind: kind, TerminalStatus: StatusFresh,
		CreatedAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour), Revision: 1,
		Source: SourceSnapshot{
			PlanRevision: 3, BusinessFactsRevision: 2, ConnectionRevision: 5,
			ProjectionRevision: &sourceProjectionRevision, RuleVersion: "rules-v1",
			OrderID: order.ID, OrderFingerprint: orderFingerprint,
			SlotID: stringPtr(slot.ID), SlotFingerprint: stringPtr(slotFingerprint),
		},
	}
	plan := PlanSource{ID: record.PlanID, Status: "draft", Revision: record.Source.PlanRevision}
	facts := FactsView{Revision: record.Source.BusinessFactsRevision}
	rules := EffectiveRules{RuleVersion: record.Source.RuleVersion}
	targets := CurrentTargets{
		CRM:   CRMSource{ConnectionRevision: record.Source.ConnectionRevision, ProjectionRevision: &currentProjectionRevision, OrderID: stringPtr(order.ID), SlotID: stringPtr(slot.ID)},
		Order: &order, Slot: &slot,
	}
	return &Application{now: func() time.Time { return now }}, record, plan, facts, rules, targets
}

func staleReasonPtr(reason StaleReason) *StaleReason { return &reason }
