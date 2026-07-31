package store

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
)

type AuthReadinessState struct {
	SchemaVersion      int
	DatabaseReady      bool
	LimiterSchemaReady bool
	LegacyCutoverReady bool
}

func (s *Store) InspectAuthReadiness(ctx context.Context) (AuthReadinessState, error) {
	var (
		version      int
		dirty        bool
		tableReady   bool
		columnsReady bool
		primaryReady bool
		indexReady   bool
	)
	if err := s.pool.QueryRow(ctx, `SELECT version, dirty FROM schema_migrations`).Scan(&version, &dirty); err != nil {
		return AuthReadinessState{}, fmt.Errorf("inspect auth schema version: %w", err)
	}
	if err := s.pool.QueryRow(ctx, `SELECT
		to_regclass('public.auth_attempt_budgets') IS NOT NULL,
		COALESCE((SELECT array_agg(column_name::text ORDER BY ordinal_position) =
			ARRAY['action', 'dimension', 'digest', 'window_start', 'attempts']::text[]
		 FROM information_schema.columns
		 WHERE table_schema = 'public' AND table_name = 'auth_attempt_budgets'), false),
		EXISTS (
			SELECT 1
			FROM pg_constraint constraint_record
			WHERE constraint_record.conrelid = to_regclass('public.auth_attempt_budgets')
			  AND constraint_record.contype = 'p'
			  AND (
				SELECT array_agg(attribute.attname::text ORDER BY key_column.ordinality)
				FROM unnest(constraint_record.conkey) WITH ORDINALITY AS key_column(attnum, ordinality)
				JOIN pg_attribute attribute
				  ON attribute.attrelid = constraint_record.conrelid
				 AND attribute.attnum = key_column.attnum
			  ) = ARRAY['action', 'dimension', 'digest']::text[]
		),
		EXISTS (
			SELECT 1
			FROM pg_index index_record
			JOIN pg_class index_class ON index_class.oid = index_record.indexrelid
			JOIN pg_class table_class ON table_class.oid = index_record.indrelid
			JOIN pg_namespace namespace_record ON namespace_record.oid = table_class.relnamespace
			WHERE namespace_record.nspname = 'public'
			  AND table_class.relname = 'auth_attempt_budgets'
			  AND index_class.relname = 'auth_attempt_budgets_window_idx'
			  AND index_record.indisvalid
			  AND index_record.indisready
			  AND index_record.indnkeyatts = 1
			  AND index_record.indnatts = 1
			  AND index_record.indpred IS NULL
			  AND index_record.indexprs IS NULL
			  AND index_record.indkey::text = (
				SELECT attribute.attnum::text
				FROM pg_attribute attribute
				WHERE attribute.attrelid = table_class.oid
				  AND attribute.attname = 'window_start'
			  )
		)`,
	).Scan(&tableReady, &columnsReady, &primaryReady, &indexReady); err != nil {
		return AuthReadinessState{}, fmt.Errorf("inspect auth limiter schema: %w", err)
	}
	limiterSchemaReady := !dirty && tableReady && columnsReady && primaryReady && indexReady
	if limiterSchemaReady {
		probeReady, probeErr := s.probeAuthLimiterSchema(ctx)
		if probeErr != nil {
			return AuthReadinessState{}, probeErr
		}
		limiterSchemaReady = probeReady
	}
	legacy, err := s.InspectLegacyState(ctx)
	if err != nil {
		return AuthReadinessState{}, err
	}
	return AuthReadinessState{
		SchemaVersion:      version,
		DatabaseReady:      !dirty,
		LimiterSchemaReady: limiterSchemaReady,
		LegacyCutoverReady: legacy.LegacyUnclaimedCount == 0 && legacy.PendingClaimCount == 0,
	}, nil
}

func (s *Store) probeAuthLimiterSchema(ctx context.Context) (bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("begin auth limiter readiness probe: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	digest := "v1:" + strings.Repeat("A", 43)
	if _, err := tx.Exec(ctx, `
		DELETE FROM auth_attempt_budgets
		WHERE action = 'login' AND dimension = 'subject' AND digest = $1`, digest); err != nil {
		return false, classifyLimiterProbeError("reset synthetic row", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO auth_attempt_budgets (action, dimension, digest, window_start, attempts)
		VALUES ('login', 'subject', $1, now(), 1)`, digest); err != nil {
		return false, classifyLimiterProbeError("insert synthetic row", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO auth_attempt_budgets (action, dimension, digest, window_start, attempts)
		VALUES ('login', 'subject', $1, now(), 1)
		ON CONFLICT (action, dimension, digest)
		DO UPDATE SET attempts = auth_attempt_budgets.attempts + 1`, digest); err != nil {
		return false, classifyLimiterProbeError("exercise conflict update", err)
	}

	invalidRows := []struct {
		name  string
		query string
	}{
		{name: "action", query: `INSERT INTO auth_attempt_budgets VALUES ('unsupported', 'subject', $1, now(), 1)`},
		{name: "dimension", query: `INSERT INTO auth_attempt_budgets VALUES ('login', 'unexpected', $1, now(), 1)`},
		{name: "digest", query: `INSERT INTO auth_attempt_budgets VALUES ('login', 'source', 'raw-source' || left($1, 0), now(), 1)`},
		{name: "attempts", query: `INSERT INTO auth_attempt_budgets VALUES ('login', 'source', $1, now(), 0)`},
	}
	for _, probe := range invalidRows {
		if _, err := tx.Exec(ctx, `SAVEPOINT auth_limiter_readiness_check`); err != nil {
			return false, fmt.Errorf("savepoint auth limiter %s probe: %w", probe.name, err)
		}
		_, probeErr := tx.Exec(ctx, probe.query, digest)
		if probeErr == nil {
			return false, nil
		}
		var postgresError *pgconn.PgError
		if !errors.As(probeErr, &postgresError) || postgresError.Code != "23514" {
			return false, classifyLimiterProbeError("reject invalid "+probe.name, probeErr)
		}
		if _, err := tx.Exec(ctx, `ROLLBACK TO SAVEPOINT auth_limiter_readiness_check`); err != nil {
			return false, fmt.Errorf("rollback auth limiter %s probe: %w", probe.name, err)
		}
		if _, err := tx.Exec(ctx, `RELEASE SAVEPOINT auth_limiter_readiness_check`); err != nil {
			return false, fmt.Errorf("release auth limiter %s probe: %w", probe.name, err)
		}
	}
	return true, nil
}

func classifyLimiterProbeError(operation string, err error) error {
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) {
		switch postgresError.Code {
		case "23502", "23505", "23514", "42P01", "42P10", "42703":
			return nil
		}
	}
	return fmt.Errorf("%s: %w", operation, err)
}
