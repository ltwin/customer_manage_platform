package reminder_test

import (
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/planshare"
	"github.com/samson/customer-manage-platform/backend/internal/reminder"
)

func TestOwnerPortsDoNotCrossResponsibilities(t *testing.T) {
	t.Parallel()
	// planshare safe reader must not implement epoch repository methods.
	src := planshare.NewAssignmentReminderSourceReader()
	_ = src
	epochs := reminder.NewReminderReconcileEpochRepository()
	_ = epochs
	// Compile-time: AssignmentReminderSourceReader has no CreateEpochInScope.
	type noEpoch interface {
		CreateEpochInScope()
	}
	var asAny any = src
	if _, ok := asAny.(noEpoch); ok {
		t.Fatal("planshare source reader must not expose epoch writes")
	}
}
