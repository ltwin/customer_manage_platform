package crm

import "errors"

var (
	ErrValidation                 = errors.New("validation_failed")
	ErrNotFound                   = errors.New("not_found")
	ErrPlanRevisionConflict       = errors.New("plan_revision_conflict")
	ErrCustomerLinkConflict       = errors.New("customer_link_conflict")
	ErrOrderLinkConflict          = errors.New("order_link_conflict")
	ErrProjectionRevisionConflict = errors.New("projection_revision_conflict")
	ErrProjectionMissing          = errors.New("projection_missing")
	ErrProjectionNotActive        = errors.New("projection_not_active")
	ErrProjectionNotFuture        = errors.New("projection_not_future")
	ErrSourceChangedRetry         = errors.New("crm_source_changed_retry")
	ErrSourceChanged              = errors.New("source_changed")
	ErrCustomerMerged             = errors.New("customer_merged")
	ErrOrderStillLinked           = errors.New("order_still_linked")
	ErrArchivedReadOnly           = errors.New("archived_read_only")
	ErrReopenRequired             = errors.New("reopen_required")
	ErrReminderWiringMismatch     = errors.New("crm reminder capability wiring mismatch")
)

type ValidationError struct {
	Message string
}

func (e ValidationError) Error() string { return e.Message }
func (e ValidationError) Unwrap() error { return ErrValidation }
