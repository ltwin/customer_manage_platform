package order

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
	"github.com/samson/customer-manage-platform/backend/internal/shootplanning/business"
)

var ErrBusinessAdjustmentScope = errors.New("order_business_adjustment_scope_invariant")

type BusinessAdjustmentParticipant struct{}

func NewBusinessAdjustmentParticipant() BusinessAdjustmentParticipant {
	return BusinessAdjustmentParticipant{}
}

type lockedBusinessAdjustmentScope struct {
	tx     store.TxAccountScope
	target business.OrderTarget
	active bool
	used   bool
	result business.AppliedOrder
}

func (BusinessAdjustmentParticipant) WithLockedTargetInScope(
	ctx context.Context,
	tx store.TxAccountScope,
	orderID string,
	callback func(business.LockedOrderScope) error,
) (business.AppliedOrder, error) {
	if callback == nil || orderID == "" {
		return business.AppliedOrder{}, ErrBusinessAdjustmentScope
	}
	var target business.OrderTarget
	err := tx.QueryRowForUpdate(ctx, "orders",
		"id, customer_id, package_id, status, price",
		"id = $2", orderID,
	).Scan(&target.ID, &target.CustomerID, &target.PackageID, &target.Status, &target.Price)
	if errors.Is(err, store.ErrNoRows) {
		return business.AppliedOrder{}, business.ErrNotFound
	}
	if err != nil {
		return business.AppliedOrder{}, fmt.Errorf("lock order business target: %w", err)
	}
	locked := &lockedBusinessAdjustmentScope{tx: tx, target: target, active: true}
	callbackErr := callback(locked)
	locked.active = false
	if callbackErr != nil {
		return business.AppliedOrder{}, callbackErr
	}
	if !locked.used {
		return business.AppliedOrder{}, ErrBusinessAdjustmentScope
	}
	return locked.result, nil
}

func (s *lockedBusinessAdjustmentScope) Target() business.OrderTarget {
	return s.target
}

func (s *lockedBusinessAdjustmentScope) Apply(
	ctx context.Context,
	command business.ApplyOrderCommand,
) (business.AppliedOrder, error) {
	if !s.active || s.used || command.AdjustmentID == "" || command.PlanID == "" ||
		command.DraftID == "" || command.AfterPrice < 0 {
		return business.AppliedOrder{}, ErrBusinessAdjustmentScope
	}
	s.used = true
	beforeFingerprint, _, err := business.FingerprintOrderTarget(s.target)
	if err != nil {
		return business.AppliedOrder{}, err
	}
	if beforeFingerprint != command.BeforeTargetFingerprint {
		return business.AppliedOrder{}, business.ErrDraftStale
	}
	updated, err := s.tx.Update(ctx, "orders", "price = $2", "id = $3", command.AfterPrice, s.target.ID)
	if err != nil {
		return business.AppliedOrder{}, fmt.Errorf("apply order business price: %w", err)
	}
	if updated != 1 {
		return business.AppliedOrder{}, business.ErrNotFound
	}
	afterTarget := s.target
	afterTarget.Price = intPointer(command.AfterPrice)
	afterFingerprint, _, err := business.FingerprintOrderTarget(afterTarget)
	if err != nil {
		return business.AppliedOrder{}, err
	}
	lines, err := json.Marshal(command.Lines)
	if err != nil {
		return business.AppliedOrder{}, err
	}
	warnings, err := json.Marshal(command.Warnings)
	if err != nil {
		return business.AppliedOrder{}, err
	}
	if err := s.tx.Insert(ctx, "order_price_adjustments", []string{
		"id", "order_id", "plan_id", "draft_id", "before_price", "after_price",
		"calculation_mode", "base_price", "lines", "warnings", "rule_version",
		"before_target_fingerprint", "after_target_fingerprint", "applied_by_account_id",
	}, command.AdjustmentID, s.target.ID, command.PlanID, command.DraftID, s.target.Price,
		command.AfterPrice, string(command.CalculationMode), command.BasePrice, lines, warnings,
		command.RuleVersion, beforeFingerprint, afterFingerprint, s.tx.AccountID()); err != nil {
		return business.AppliedOrder{}, fmt.Errorf("append order price adjustment: %w", err)
	}
	s.result = business.AppliedOrder{
		Target: afterTarget, AdjustmentID: command.AdjustmentID,
		BeforePrice: s.target.Price, AfterPrice: command.AfterPrice,
		BeforeFingerprint: beforeFingerprint, AfterFingerprint: afterFingerprint,
	}
	return s.result, nil
}

func intPointer(value int) *int { return &value }

var _ business.OrderAdjustmentParticipant = BusinessAdjustmentParticipant{}
