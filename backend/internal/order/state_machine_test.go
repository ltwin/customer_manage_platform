package order

import (
	"errors"
	"testing"
	"time"

	"github.com/oapi-codegen/nullable"
)

func TestCanTransitionMatrix(t *testing.T) {
	statuses := []string{
		StatusConsulting,
		StatusScheduled,
		StatusShot,
		StatusSelected,
		StatusRetouching,
		StatusDelivered,
		StatusClosed,
		StatusCancelled,
	}
	allowed := map[[2]string]bool{
		{StatusConsulting, StatusScheduled}: true,
		{StatusScheduled, StatusShot}:       true,
		{StatusShot, StatusSelected}:        true,
		{StatusSelected, StatusRetouching}:  true,
		{StatusRetouching, StatusDelivered}: true,
		{StatusDelivered, StatusClosed}:     true,
		{StatusShot, StatusDelivered}:       true,
		{StatusSelected, StatusDelivered}:   true,
	}
	for _, from := range statuses[:6] {
		allowed[[2]string{from, StatusCancelled}] = true
	}

	for _, from := range statuses {
		for _, to := range statuses {
			got := canTransition(from, to)
			want := allowed[[2]string{from, to}]
			if got != want {
				t.Fatalf("canTransition(%q, %q) = %v, want %v", from, to, got, want)
			}
		}
	}
}

func TestApplyUpdateInputWritesTransitionTimestamps(t *testing.T) {
	now := time.Date(2026, 7, 9, 10, 0, 0, 0, time.UTC)
	status := StatusShot
	updated, err := ApplyUpdateInput(Order{Status: StatusScheduled}, UpdateInput{Status: &status}, now)
	if err != nil {
		t.Fatalf("ApplyUpdateInput scheduled->shot returned error: %v", err)
	}
	if updated.Status != StatusShot || updated.ShotAt == nil || !updated.ShotAt.Equal(now) {
		t.Fatalf("scheduled->shot = status %q shot_at %v, want shot_at now", updated.Status, updated.ShotAt)
	}

	explicit := time.Date(2026, 6, 1, 8, 30, 0, 0, time.UTC)
	status = StatusShot
	updated, err = ApplyUpdateInput(Order{Status: StatusScheduled}, UpdateInput{
		Status: &status,
		ShotAt: nullable.NewNullableWithValue(explicit),
	}, now)
	if err != nil {
		t.Fatalf("ApplyUpdateInput explicit shot_at returned error: %v", err)
	}
	if updated.ShotAt == nil || !updated.ShotAt.Equal(explicit) {
		t.Fatalf("shot_at = %v, want explicit %v", updated.ShotAt, explicit)
	}

	status = StatusDelivered
	updated, err = ApplyUpdateInput(Order{Status: StatusShot, ShotAt: &explicit}, UpdateInput{Status: &status}, now)
	if err != nil {
		t.Fatalf("ApplyUpdateInput shot->delivered returned error: %v", err)
	}
	if updated.DeliveredAt == nil || !updated.DeliveredAt.Equal(now) {
		t.Fatalf("delivered_at = %v, want now", updated.DeliveredAt)
	}
}

func TestApplyUpdateInputRejectsExplicitNullTransitionTimestamps(t *testing.T) {
	now := time.Date(2026, 7, 9, 10, 0, 0, 0, time.UTC)
	status := StatusShot
	_, err := ApplyUpdateInput(Order{Status: StatusScheduled}, UpdateInput{
		Status: &status,
		ShotAt: nullable.NewNullNullable[time.Time](),
	}, now)
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("scheduled->shot with shot_at null error = %v, want ErrValidation", err)
	}

	shotAt := time.Date(2026, 6, 1, 8, 0, 0, 0, time.UTC)
	status = StatusDelivered
	_, err = ApplyUpdateInput(Order{Status: StatusShot, ShotAt: &shotAt}, UpdateInput{
		Status:      &status,
		DeliveredAt: nullable.NewNullNullable[time.Time](),
	}, now)
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("shot->delivered with delivered_at null error = %v, want ErrValidation", err)
	}
}

func TestApplyUpdateInputPaymentGateAndAtomicResult(t *testing.T) {
	shotAt := time.Date(2026, 6, 1, 8, 0, 0, 0, time.UTC)
	deliveredAt := time.Date(2026, 6, 9, 8, 0, 0, 0, time.UTC)
	status := StatusClosed
	current := Order{
		Status:      StatusDelivered,
		ShotAt:      &shotAt,
		DeliveredAt: &deliveredAt,
		BalancePaid: false,
	}

	_, err := ApplyUpdateInput(current, UpdateInput{Status: &status}, deliveredAt)
	if !errors.Is(err, ErrUnpaidBalance) {
		t.Fatalf("closing unpaid order error = %v, want ErrUnpaidBalance", err)
	}

	note := "should not matter when caller discards failed result"
	_, err = ApplyUpdateInput(current, UpdateInput{Status: &status, Note: &note}, deliveredAt)
	if !errors.Is(err, ErrUnpaidBalance) {
		t.Fatalf("mixed failed close error = %v, want ErrUnpaidBalance", err)
	}

	paid := true
	updated, err := ApplyUpdateInput(current, UpdateInput{Status: &status, BalancePaid: &paid}, deliveredAt)
	if err != nil {
		t.Fatalf("one-step paid close returned error: %v", err)
	}
	if updated.Status != StatusClosed || !updated.BalancePaid {
		t.Fatalf("updated = status %q balance %v, want closed paid", updated.Status, updated.BalancePaid)
	}
}

func TestApplyUpdateInputFieldCorrectionInvariants(t *testing.T) {
	now := time.Date(2026, 7, 9, 10, 0, 0, 0, time.UTC)
	_, err := ApplyUpdateInput(Order{Status: StatusConsulting}, UpdateInput{
		ShotAt: nullable.NewNullableWithValue(now),
	}, now)
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("prewriting shot_at error = %v, want ErrValidation", err)
	}

	_, err = ApplyUpdateInput(Order{Status: StatusShot, ShotAt: &now}, UpdateInput{
		ShotAt: nullable.NewNullNullable[time.Time](),
	}, now)
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("clearing reached shot_at error = %v, want ErrValidation", err)
	}

	falseValue := false
	_, err = ApplyUpdateInput(Order{
		Status:      StatusClosed,
		ShotAt:      &now,
		DeliveredAt: &now,
		BalancePaid: true,
	}, UpdateInput{BalancePaid: &falseValue}, now)
	if !errors.Is(err, ErrUnpaidBalance) {
		t.Fatalf("closed balance=false error = %v, want ErrUnpaidBalance", err)
	}

	trueValue := true
	_, err = ApplyUpdateInput(Order{Status: StatusCancelled}, UpdateInput{DepositPaid: &trueValue}, now)
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("terminal payment correction error = %v, want ErrValidation", err)
	}

	note := "取消原因"
	updated, err := ApplyUpdateInput(Order{Status: StatusCancelled}, UpdateInput{Note: &note}, now)
	if err != nil {
		t.Fatalf("terminal note correction returned error: %v", err)
	}
	if updated.Note == nil || *updated.Note != note {
		t.Fatalf("note = %v, want %q", updated.Note, note)
	}
}

func TestApplyUpdateInputRejectsTimestampPrewriteOnCancel(t *testing.T) {
	now := time.Date(2026, 7, 9, 10, 0, 0, 0, time.UTC)
	shotAt := time.Date(2026, 6, 1, 8, 0, 0, 0, time.UTC)
	status := StatusCancelled
	_, err := ApplyUpdateInput(Order{Status: StatusConsulting}, UpdateInput{
		Status: &status,
		ShotAt: nullable.NewNullableWithValue(shotAt),
	}, now)
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("consulting->cancelled with shot_at error = %v, want ErrValidation", err)
	}

	current := Order{Status: StatusShot, ShotAt: &shotAt}
	updated, err := ApplyUpdateInput(current, UpdateInput{Status: &status}, now)
	if err != nil {
		t.Fatalf("shot->cancelled with existing shot_at returned error: %v", err)
	}
	if updated.Status != StatusCancelled || updated.ShotAt == nil || !updated.ShotAt.Equal(shotAt) {
		t.Fatalf("shot->cancelled = status %q shot_at %v, want cancelled preserving %v", updated.Status, updated.ShotAt, shotAt)
	}
}

func TestApplyCreateInputBackfillInvariants(t *testing.T) {
	status := StatusDelivered
	_, err := ApplyCreateInput(CreateInput{CreationMode: CreationModeBackfill, Status: &status})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("backfill delivered without timestamps error = %v, want ErrValidation", err)
	}

	status = StatusClosed
	shotAt := time.Date(2026, 6, 1, 8, 0, 0, 0, time.UTC)
	deliveredAt := time.Date(2026, 6, 9, 8, 0, 0, 0, time.UTC)
	_, err = ApplyCreateInput(CreateInput{CreationMode: CreationModeBackfill, Status: &status, ShotAt: &shotAt, DeliveredAt: &deliveredAt})
	if !errors.Is(err, ErrUnpaidBalance) {
		t.Fatalf("backfill closed unpaid error = %v, want ErrUnpaidBalance", err)
	}

	status = StatusConsulting
	_, err = ApplyCreateInput(CreateInput{CreationMode: CreationModeBackfill, Status: &status, ShotAt: &shotAt})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("consulting with shot_at error = %v, want ErrValidation", err)
	}

	status = StatusCancelled
	created, err := ApplyCreateInput(CreateInput{CreationMode: CreationModeBackfill, Status: &status, Note: testStringPtr("取消")})
	if err != nil {
		t.Fatalf("cancelled backfill returned error: %v", err)
	}
	if created.Status != StatusCancelled {
		t.Fatalf("status = %q, want cancelled", created.Status)
	}
}

func testStringPtr(value string) *string {
	return &value
}
