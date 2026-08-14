package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/platform/idempotency"
	"github.com/samson/customer-manage-platform/backend/internal/platform/planningcapability"
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

	reminder := fakeArchiveCapabilityStartupReader{state: planningcapability.ArchiveCapabilityState{
		SingletonKey: planningcapability.SingletonKey,
		Capability:   planningcapability.ArchiveCapabilityReminder,
		Revision:     3,
	}}
	if _, err := composeShootPlanningApplication(t.Context(), reminder, idempotency.NewExecutor()); err == nil ||
		!strings.Contains(err.Error(), "unavailable server wiring") {
		t.Fatalf("reminder capability must fail closed until ITEM-6, got %v", err)
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
