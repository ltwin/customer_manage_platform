package shootplanning

import (
	"context"
	"errors"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// PlanReadyObservationFact is the narrow fact that the core state machine
// emits when a plan becomes ready.  The ingestion domain owns how this fact
// is accumulated; the core package deliberately does not import that child
// package.
type PlanReadyObservationFact struct {
	PlanID string
	TickID string
}

// PlanReadyObservationSink is a transaction-local observation seam.  A sink
// must be idempotent for TickID and must treat a missing observation as a
// normal no-op; it must never change the core response or plan metrics.
type PlanReadyObservationSink interface {
	AccumulateAndRecordFirstReadyInScope(context.Context, store.TxAccountScope, PlanReadyObservationFact) error
}

// NoopPlanReadyObservationSink is used by builds which do not expose the
// ingestion route.  It is explicit so production composition can fail closed
// when ingestion is enabled without the real sink.
type NoopPlanReadyObservationSink struct{}

func (NoopPlanReadyObservationSink) AccumulateAndRecordFirstReadyInScope(context.Context, store.TxAccountScope, PlanReadyObservationFact) error {
	return nil
}

func isNoopPlanReadyObservationSink(sink PlanReadyObservationSink) bool {
	switch sink.(type) {
	case NoopPlanReadyObservationSink, *NoopPlanReadyObservationSink:
		return true
	default:
		return false
	}
}

var ErrPlanReadyObservationWiringMismatch = errors.New("plan_ready_observation_sink_wiring_mismatch")
