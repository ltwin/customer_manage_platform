package main

import (
	"context"
	"log/slog"
	"time"
)

const (
	authReplaySweepBatch    = 100
	authReplaySweepInterval = time.Minute
)

type replayCiphertextSweeper interface {
	SweepExpiredReplayCiphertexts(context.Context, int) (int64, error)
}

type authReplaySweepRunner struct {
	sweeper replayCiphertextSweeper
	logger  *slog.Logger
}

func newAuthReplaySweepRunner(sweeper replayCiphertextSweeper, logger *slog.Logger) *authReplaySweepRunner {
	if logger == nil {
		logger = slog.Default()
	}
	return &authReplaySweepRunner{sweeper: sweeper, logger: logger}
}

func (r *authReplaySweepRunner) Run(ctx context.Context) {
	ticker := time.NewTicker(authReplaySweepInterval)
	defer ticker.Stop()
	r.run(ctx, ticker.C)
}

func (r *authReplaySweepRunner) run(ctx context.Context, ticks <-chan time.Time) {
	if ctx.Err() != nil {
		return
	}
	r.sweep(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticks:
			r.sweep(ctx)
		}
	}
}

func (r *authReplaySweepRunner) sweep(ctx context.Context) {
	if ctx.Err() != nil {
		return
	}
	if _, err := r.sweeper.SweepExpiredReplayCiphertexts(ctx, authReplaySweepBatch); err != nil {
		if ctx.Err() != nil {
			return
		}
		r.logger.Error(
			"auth replay ciphertext sweep failed",
			slog.String("event", "auth.replay_ciphertext_sweep"),
			slog.String("result", "failed"),
			slog.String("failure_class", "internal"),
		)
	}
}
