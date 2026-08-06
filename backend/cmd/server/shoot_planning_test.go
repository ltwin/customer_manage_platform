package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/platform/idempotency"
	"github.com/samson/customer-manage-platform/backend/internal/platform/planningcapability"
)

type fakeArchiveCapabilityStartupReader struct {
	state planningcapability.ArchiveCapabilityState
	err   error
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

	for _, capability := range []planningcapability.ArchiveCapability{
		planningcapability.ArchiveCapabilityPlanningShare,
		planningcapability.ArchiveCapabilityReminder,
	} {
		t.Run(string(capability), func(t *testing.T) {
			reader := fakeArchiveCapabilityStartupReader{state: planningcapability.ArchiveCapabilityState{
				SingletonKey: planningcapability.SingletonKey,
				Capability:   capability,
				Revision:     2,
			}}
			if _, err := composeShootPlanningApplication(t.Context(), reader, idempotency.NewExecutor()); err == nil ||
				!strings.Contains(err.Error(), "unavailable server wiring") {
				t.Fatalf("higher capability must fail closed, got %v", err)
			}
		})
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
