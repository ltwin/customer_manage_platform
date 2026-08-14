package main

import (
	"context"
	"errors"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/planshare"
	"github.com/samson/customer-manage-platform/backend/internal/platform/idempotency"
	"github.com/samson/customer-manage-platform/backend/internal/platform/planningcapability"
	"github.com/samson/customer-manage-platform/backend/internal/reminder"
	"github.com/samson/customer-manage-platform/backend/internal/shootplanning"
	"github.com/samson/customer-manage-platform/backend/internal/shootplanning/ingestion"
)

type fakeArchiveCapabilityStartupReader struct {
	state planningcapability.ArchiveCapabilityState
	err   error
}

func TestComposeShootPlanningApplicationWithIngestionRejectsNoopSink(t *testing.T) {
	core := fakeArchiveCapabilityStartupReader{state: planningcapability.ArchiveCapabilityState{
		SingletonKey: planningcapability.SingletonKey,
		Capability:   planningcapability.ArchiveCapabilityCore,
		Revision:     1,
	}}
	_, err := composeShootPlanningApplicationWithIngestion(
		t.Context(), core, idempotency.NewExecutor(), nil, shootplanning.NoopPlanReadyObservationSink{},
	)
	if !errors.Is(err, shootplanning.ErrPlanReadyObservationWiringMismatch) {
		t.Fatalf("route-enabled noop sink must fail closed, got %v", err)
	}
	if _, err := composeShootPlanningApplicationWithIngestion(
		t.Context(), core, idempotency.NewExecutor(), nil,
		ingestion.NewPlanReadyObservationAdapter(ingestion.NewRepository()),
	); err != nil {
		t.Fatalf("real ingestion adapter should compose: %v", err)
	}
}

func (reader fakeArchiveCapabilityStartupReader) Current(context.Context) (planningcapability.ArchiveCapabilityState, error) {
	return reader.state, reader.err
}

func TestComposeShootPlanningApplicationFailsClosed(t *testing.T) {
	core := fakeArchiveCapabilityStartupReader{state: planningcapability.ArchiveCapabilityState{
		SingletonKey: planningcapability.SingletonKey,
		Capability:   planningcapability.ArchiveCapabilityCore,
		Revision:     1,
	}}
	application, err := composeShootPlanningApplication(t.Context(), core, idempotency.NewExecutor())
	if err != nil || application == nil {
		t.Fatalf("core composition should succeed: app=%v err=%v", application, err)
	}

	share := fakeArchiveCapabilityStartupReader{state: planningcapability.ArchiveCapabilityState{
		SingletonKey: planningcapability.SingletonKey,
		Capability:   planningcapability.ArchiveCapabilityPlanningShare,
		Revision:     2,
	}}
	shareApp, err := composeShootPlanningApplication(t.Context(), share, idempotency.NewExecutor())
	if err != nil || shareApp == nil {
		t.Fatalf("planning-share-v1 composition should succeed with real guard: app=%v err=%v", shareApp, err)
	}

	reminderCap := fakeArchiveCapabilityStartupReader{state: planningcapability.ArchiveCapabilityState{
		SingletonKey: planningcapability.SingletonKey,
		Capability:   planningcapability.ArchiveCapabilityReminder,
		Revision:     3,
	}}
	reminderApp, err := composeShootPlanningApplication(t.Context(), reminderCap, idempotency.NewExecutor())
	if err != nil || reminderApp == nil {
		t.Fatalf("planning-share-reminder-v1 composition must succeed with real archive/CRM wiring: app=%v err=%v", reminderApp, err)
	}

	readFailure := errors.New("marker unavailable")
	if _, err := composeShootPlanningApplication(t.Context(), fakeArchiveCapabilityStartupReader{err: readFailure}, idempotency.NewExecutor()); err == nil || !errors.Is(err, readFailure) {
		t.Fatalf("startup marker read failure must fail closed, got %v", err)
	}
	if _, err := composeShootPlanningApplication(t.Context(), nil, idempotency.NewExecutor()); err == nil {
		t.Fatal("missing startup reader must fail closed")
	}
	if _, err := composeShootPlanningApplication(t.Context(), core, nil); err == nil {
		t.Fatal("missing shared idempotency executor must fail closed")
	}
}

func TestShootPlanningOptionsReminderWiringMatrix(t *testing.T) {
	t.Parallel()

	opts, err := shootPlanningOptionsForCapability(planningcapability.ArchiveCapabilityReminder)
	if err != nil {
		t.Fatalf("reminder options: %v", err)
	}
	app, err := shootplanning.NewApplication(shootplanning.NewPostgresRepository(), idempotency.NewExecutor(), opts...)
	if err != nil || app == nil {
		t.Fatalf("reminder wiring ready composition failed: %v", err)
	}

	// Non-reminder capabilities must keep archive participant Disabled.
	for _, capability := range []planningcapability.ArchiveCapability{
		planningcapability.ArchiveCapabilityCore,
		planningcapability.ArchiveCapabilityPlanningShare,
	} {
		capability := capability
		opts, err := shootPlanningOptionsForCapability(capability)
		if err != nil {
			t.Fatalf("%s options: %v", capability, err)
		}
		if _, err := shootplanning.NewApplication(shootplanning.NewPostgresRepository(), idempotency.NewExecutor(), opts...); err != nil {
			t.Fatalf("%s composition with production options: %v", capability, err)
		}
		// Injecting a real archive participant on non-reminder capability must fail.
		bad := append([]shootplanning.ApplicationOption{}, opts...)
		bad = append(bad, shootplanning.WithArchiveReminderParticipant(reminder.NewPlanArchiveReminderAdapter()))
		if _, err := shootplanning.NewApplication(shootplanning.NewPostgresRepository(), idempotency.NewExecutor(), bad...); !errors.Is(err, shootplanning.ErrArchiveWiringMismatch) {
			t.Fatalf("%s + real archive participant want ErrArchiveWiringMismatch, got %v", capability, err)
		}
	}

	// Reminder capability rejects Disabled archive participant / wrong policy / missing guard.
	if _, err := shootplanning.NewApplication(
		shootplanning.NewPostgresRepository(),
		idempotency.NewExecutor(),
		shootplanning.WithArchiveImpactPolicy(shootplanning.PlanningShareReminderArchiveImpactPolicyV1{}),
		shootplanning.WithReadinessRemovalGuard(planshare.ReadinessRemovalGuard{}),
		shootplanning.WithArchiveReminderParticipant(shootplanning.DisabledPlanArchiveReminderParticipant{}),
		shootplanning.WithCRMReminder(reminder.NewCRMReminderLifecycleAdapter(nil), true),
	); !errors.Is(err, shootplanning.ErrArchiveWiringMismatch) {
		t.Fatalf("disabled archive participant: %v", err)
	}
	if _, err := shootplanning.NewApplication(
		shootplanning.NewPostgresRepository(),
		idempotency.NewExecutor(),
		shootplanning.WithArchiveImpactPolicy(shootplanning.PlanningShareArchiveImpactPolicyV1{}),
		shootplanning.WithReadinessRemovalGuard(planshare.ReadinessRemovalGuard{}),
		shootplanning.WithArchiveReminderParticipant(reminder.NewPlanArchiveReminderAdapter()),
		shootplanning.WithCRMReminder(reminder.NewCRMReminderLifecycleAdapter(nil), true),
	); !errors.Is(err, shootplanning.ErrArchiveWiringMismatch) {
		t.Fatalf("wrong archive policy on reminder capability: %v", err)
	}
	if _, err := shootplanning.NewApplication(
		shootplanning.NewPostgresRepository(),
		idempotency.NewExecutor(),
		shootplanning.WithArchiveImpactPolicy(shootplanning.PlanningShareReminderArchiveImpactPolicyV1{}),
		shootplanning.WithArchiveReminderParticipant(reminder.NewPlanArchiveReminderAdapter()),
		shootplanning.WithCRMReminder(reminder.NewCRMReminderLifecycleAdapter(nil), true),
	); !errors.Is(err, shootplanning.ErrReadinessGuardWiringMismatch) {
		t.Fatalf("missing readiness guard: %v", err)
	}
}
