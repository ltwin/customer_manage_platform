package schedule

import (
	"context"
	"strings"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

type Repository interface {
	Create(context.Context, store.AccountScope, PreparedCreate, time.Time) (CreateResult, error)
	CreatePreparedInScope(context.Context, store.TxAccountScope, PreparedCreate, string, time.Time) (CreateResult, error)
	LookupOrderCustomerID(context.Context, store.AccountScope, PreparedCreate) (string, error)
	LookupOrderCustomerIDInScope(context.Context, store.TxAccountScope, PreparedCreate) (string, error)
	List(context.Context, store.AccountScope, ListFilter) ([]ListItem, error)
	Update(context.Context, store.AccountScope, string, UpdateInput, time.Time) (Slot, error)
	Delete(context.Context, store.AccountScope, string) error
}

type Service struct {
	repo  Repository
	clock Clock
}

func NewService(repo Repository, clock Clock) *Service {
	return &Service{repo: repo, clock: clock}
}

func (s *Service) PrepareCreate(input CreateInput) (PreparedCreate, error) {
	input.StartAt = input.StartAt.UTC()
	input.EndAt = input.EndAt.UTC()
	input.Type = strings.TrimSpace(input.Type)
	input.OrderID = trimmedPtr(input.OrderID)
	input.Note = trimmedPtr(input.Note)
	slot := Slot{
		StartAt: input.StartAt,
		EndAt:   input.EndAt,
		Type:    input.Type,
		OrderID: input.OrderID,
		Note:    input.Note,
	}
	if err := ValidateSlot(slot); err != nil {
		return PreparedCreate{}, err
	}
	return PreparedCreate{Input: input, slot: slot}, nil
}

func (s *Service) Create(ctx context.Context, scope store.AccountScope, input CreateInput) (CreateResult, error) {
	prepared, err := s.PrepareCreate(input)
	if err != nil {
		return CreateResult{}, err
	}
	return s.repo.Create(ctx, scope, prepared, s.clock.Now().UTC())
}

func (s *Service) LookupOrderCustomerID(
	ctx context.Context,
	scope store.AccountScope,
	prepared PreparedCreate,
) (string, error) {
	return s.repo.LookupOrderCustomerID(ctx, scope, prepared)
}

func (s *Service) LookupOrderCustomerIDInScope(
	ctx context.Context,
	scope store.TxAccountScope,
	prepared PreparedCreate,
) (string, error) {
	return s.repo.LookupOrderCustomerIDInScope(ctx, scope, prepared)
}

func (s *Service) CreatePreparedInScope(
	ctx context.Context,
	scope store.TxAccountScope,
	prepared PreparedCreate,
	expectedCustomerID string,
) (CreateResult, error) {
	if err := ValidateSlot(prepared.slot); err != nil {
		return CreateResult{}, err
	}
	return s.repo.CreatePreparedInScope(ctx, scope, prepared, expectedCustomerID, s.clock.Now().UTC())
}

func (s *Service) List(ctx context.Context, scope store.AccountScope, filter ListFilter) ([]ListItem, error) {
	filter.From = filter.From.UTC()
	filter.To = filter.To.UTC()
	if filter.From.IsZero() || filter.To.IsZero() || !filter.To.After(filter.From) {
		return nil, ValidationError{Message: "to 必须晚于 from"}
	}
	return s.repo.List(ctx, scope, filter)
}

func (s *Service) Update(
	ctx context.Context,
	scope store.AccountScope,
	id string,
	input UpdateInput,
) (Slot, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return Slot{}, ValidationError{Message: "档期 id 必填"}
	}
	input = normalizeUpdateInput(input)
	return s.repo.Update(ctx, scope, id, input, s.clock.Now().UTC())
}

func (s *Service) Delete(ctx context.Context, scope store.AccountScope, id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return ValidationError{Message: "档期 id 必填"}
	}
	return s.repo.Delete(ctx, scope, id)
}

func normalizeUpdateInput(input UpdateInput) UpdateInput {
	if input.StartAt != nil {
		value := input.StartAt.UTC()
		input.StartAt = &value
	}
	if input.EndAt != nil {
		value := input.EndAt.UTC()
		input.EndAt = &value
	}
	if input.Type != nil {
		value := strings.TrimSpace(*input.Type)
		input.Type = &value
	}
	if input.OrderID.IsSpecified() && !input.OrderID.IsNull() {
		value := strings.TrimSpace(input.OrderID.MustGet())
		input.OrderID.Set(value)
	}
	if input.Note.IsSpecified() && !input.Note.IsNull() {
		value := strings.TrimSpace(input.Note.MustGet())
		if value == "" {
			input.Note.SetNull()
		} else {
			input.Note.Set(value)
		}
	}
	return input
}

func trimmedPtr(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}
