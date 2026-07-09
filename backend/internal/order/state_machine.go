package order

import (
	"fmt"
	"time"
)

var allowedTransitions = map[[2]string]struct{}{
	{StatusConsulting, StatusScheduled}: {},
	{StatusScheduled, StatusShot}:       {},
	{StatusShot, StatusSelected}:        {},
	{StatusSelected, StatusRetouching}:  {},
	{StatusRetouching, StatusDelivered}: {},
	{StatusDelivered, StatusClosed}:     {},
	{StatusShot, StatusDelivered}:       {},
	{StatusSelected, StatusDelivered}:   {},
	{StatusConsulting, StatusCancelled}: {},
	{StatusScheduled, StatusCancelled}:  {},
	{StatusShot, StatusCancelled}:       {},
	{StatusSelected, StatusCancelled}:   {},
	{StatusRetouching, StatusCancelled}: {},
	{StatusDelivered, StatusCancelled}:  {},
}

func validStatus(status string) bool {
	switch status {
	case StatusConsulting, StatusScheduled, StatusShot, StatusSelected,
		StatusRetouching, StatusDelivered, StatusClosed, StatusCancelled:
		return true
	default:
		return false
	}
}

func canTransition(from, to string) bool {
	_, ok := allowedTransitions[[2]string{from, to}]
	return ok
}

func ApplyCreateInput(input CreateInput) (Order, error) {
	status := StatusConsulting
	if input.Status != nil {
		status = *input.Status
	}
	if !validStatus(status) {
		return Order{}, ValidationError{Message: "status 非法"}
	}
	order := Order{
		CustomerID:  input.CustomerID,
		PackageID:   input.PackageID,
		Title:       input.Title,
		Status:      status,
		Price:       input.Price,
		ShotAt:      input.ShotAt,
		DeliveredAt: input.DeliveredAt,
		Note:        input.Note,
	}
	if input.DepositPaid != nil {
		order.DepositPaid = *input.DepositPaid
	}
	if input.BalancePaid != nil {
		order.BalancePaid = *input.BalancePaid
	}
	if err := validateFinalState(Order{}, order, UpdateInput{}); err != nil {
		return Order{}, err
	}
	return order, nil
}

func ApplyUpdateInput(current Order, input UpdateInput, now time.Time) (Order, error) {
	if !validStatus(current.Status) {
		return Order{}, ValidationError{Message: "当前 status 非法"}
	}
	next := current
	targetStatus := current.Status
	if input.Status != nil {
		if !validStatus(*input.Status) {
			return Order{}, ValidationError{Message: "status 非法"}
		}
		targetStatus = *input.Status
	}

	if input.DepositPaid != nil {
		next.DepositPaid = *input.DepositPaid
	}
	if input.BalancePaid != nil {
		next.BalancePaid = *input.BalancePaid
	}
	if input.Title != nil {
		next.Title = input.Title
	}
	if input.Price.IsSpecified() {
		if input.Price.IsNull() {
			next.Price = nil
		} else {
			price := input.Price.MustGet()
			next.Price = &price
		}
	}
	if input.Note != nil {
		next.Note = input.Note
	}
	if input.ShotAt.IsSpecified() {
		if input.ShotAt.IsNull() {
			return Order{}, ValidationError{Message: "shot_at 不可置空"}
		} else {
			shotAt := input.ShotAt.MustGet()
			next.ShotAt = &shotAt
		}
	}
	if input.DeliveredAt.IsSpecified() {
		if input.DeliveredAt.IsNull() {
			return Order{}, ValidationError{Message: "delivered_at 不可置空"}
		} else {
			deliveredAt := input.DeliveredAt.MustGet()
			next.DeliveredAt = &deliveredAt
		}
	}

	statusChanged := targetStatus != current.Status
	if statusChanged {
		if !canTransition(current.Status, targetStatus) {
			return Order{}, fmt.Errorf("%w: %s -> %s", ErrInvalidStatusTransition, current.Status, targetStatus)
		}
		next.Status = targetStatus
		if targetStatus == StatusShot && !input.ShotAt.IsSpecified() && next.ShotAt == nil {
			shotAt := now
			next.ShotAt = &shotAt
		}
		if targetStatus == StatusDelivered && !input.DeliveredAt.IsSpecified() && next.DeliveredAt == nil {
			deliveredAt := now
			next.DeliveredAt = &deliveredAt
		}
	}

	if err := validateFinalState(current, next, input); err != nil {
		return Order{}, err
	}
	return next, nil
}

func validateFinalState(current, next Order, input UpdateInput) error {
	if next.Status == StatusClosed && !next.BalancePaid {
		return fmt.Errorf("%w: closed 订单必须已结清", ErrUnpaidBalance)
	}
	if terminalStatus(current.Status) && (input.DepositPaid != nil || input.BalancePaid != nil) {
		return ValidationError{Message: "终态订单不可修改收款标记"}
	}
	if input.ShotAt.IsSpecified() && !shotAtReached(current, next) {
		return ValidationError{Message: "shot_at 仅可用于已到达拍摄的订单"}
	}
	if input.DeliveredAt.IsSpecified() && !deliveredAtReached(current, next) {
		return ValidationError{Message: "delivered_at 仅可用于已到达交付的订单"}
	}
	if next.ShotAt != nil && !statusAllowsShotAt(next.Status) {
		return ValidationError{Message: "shot_at 仅可用于已到达拍摄的订单"}
	}
	if next.DeliveredAt != nil && !statusAllowsDeliveredAt(next.Status) {
		return ValidationError{Message: "delivered_at 仅可用于已到达交付的订单"}
	}
	if statusRequiresShotAt(next.Status) && next.ShotAt == nil {
		return ValidationError{Message: "已到达拍摄的订单必须有 shot_at"}
	}
	if statusRequiresDeliveredAt(next.Status) && next.DeliveredAt == nil {
		return ValidationError{Message: "已到达交付的订单必须有 delivered_at"}
	}
	return nil
}

func terminalStatus(status string) bool {
	return status == StatusClosed || status == StatusCancelled
}

func statusAllowsShotAt(status string) bool {
	switch status {
	case StatusShot, StatusSelected, StatusRetouching, StatusDelivered, StatusClosed, StatusCancelled:
		return true
	default:
		return false
	}
}

func statusAllowsDeliveredAt(status string) bool {
	switch status {
	case StatusDelivered, StatusClosed, StatusCancelled:
		return true
	default:
		return false
	}
}

func statusRequiresShotAt(status string) bool {
	switch status {
	case StatusShot, StatusSelected, StatusRetouching, StatusDelivered, StatusClosed:
		return true
	default:
		return false
	}
}

func statusRequiresDeliveredAt(status string) bool {
	return status == StatusDelivered || status == StatusClosed
}

func shotAtReached(current, next Order) bool {
	return statusReachedShot(current.Status) || statusReachedShot(next.Status) || current.ShotAt != nil
}

func deliveredAtReached(current, next Order) bool {
	return statusReachedDelivered(current.Status) || statusReachedDelivered(next.Status) || current.DeliveredAt != nil
}

func statusReachedShot(status string) bool {
	switch status {
	case StatusShot, StatusSelected, StatusRetouching, StatusDelivered, StatusClosed:
		return true
	default:
		return false
	}
}

func statusReachedDelivered(status string) bool {
	return status == StatusDelivered || status == StatusClosed
}
