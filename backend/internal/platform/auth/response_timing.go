package auth

import (
	"context"
	cryptorand "crypto/rand"
	"fmt"
	"io"
	"math/big"
	"time"
)

type responseBudget struct {
	floor     time.Duration
	maxJitter time.Duration
}

var (
	mailResponseBudget = responseBudget{floor: time.Second, maxJitter: 50 * time.Millisecond}
	authResponseBudget = responseBudget{floor: 300 * time.Millisecond, maxJitter: 25 * time.Millisecond}
)

func withAuthResponseBudget[T any](
	ctx context.Context,
	service *Service,
	budget responseBudget,
	action func() (T, error),
) (T, error) {
	var zero T
	startedAt := service.now()
	jitter, err := authResponseJitter(service.timingRandom, budget.maxJitter)
	if err != nil {
		if delayErr := service.finishAuthResponse(ctx, startedAt, budget.floor); delayErr != nil {
			return zero, classifyAuthFailure("wait for auth response budget", delayErr)
		}
		return zero, classifyAuthFailure("generate auth response jitter", err)
	}
	result, actionErr := action()
	if err := service.finishAuthResponse(ctx, startedAt, budget.floor+jitter); err != nil {
		return zero, classifyAuthFailure("wait for auth response budget", err)
	}
	return result, actionErr
}

func (s *Service) finishAuthResponse(ctx context.Context, startedAt time.Time, target time.Duration) error {
	elapsed := s.now().Sub(startedAt)
	if elapsed < 0 {
		elapsed = 0
	}
	remaining := target - elapsed
	if remaining <= 0 {
		return nil
	}
	if s.responseDelay == nil {
		return fmt.Errorf("response delay unavailable")
	}
	return s.responseDelay(ctx, remaining)
}

func authResponseJitter(random io.Reader, maximum time.Duration) (time.Duration, error) {
	if random == nil || maximum < 0 {
		return 0, fmt.Errorf("invalid timing random configuration")
	}
	if maximum == 0 {
		return 0, nil
	}
	value, err := cryptorand.Int(random, big.NewInt(int64(maximum)+1))
	if err != nil {
		return 0, fmt.Errorf("read timing randomness: %w", err)
	}
	return time.Duration(value.Int64()), nil
}

func waitForAuthResponse(ctx context.Context, duration time.Duration) error {
	if duration <= 0 {
		return nil
	}
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
