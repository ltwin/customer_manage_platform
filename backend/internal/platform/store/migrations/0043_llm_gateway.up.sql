-- FND-06: unified LLM gateway admission, dispatch identity and cost ledger.
-- Explicit account keys only; no foreign keys. Cross-table ownership is guarded by services.
-- Money is integer micros of one configured currency; unknown is NULL, never 0.
CREATE TABLE llm_call_groups (
 account_id TEXT NOT NULL,
 caller_service TEXT NOT NULL CHECK(caller_service ~ '^[a-z][a-z0-9_]{0,39}$'),
 caller_group_id TEXT NOT NULL CHECK(char_length(caller_group_id) BETWEEN 1 AND 64),
 limit_micros BIGINT NOT NULL CHECK(limit_micros>=0),
 token_limit BIGINT NOT NULL CHECK(token_limit>=0),
 currency CHAR(3) NOT NULL CHECK(currency ~ '^[A-Z]{3}$'),
 deadline_at TIMESTAMPTZ NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(account_id,caller_service,caller_group_id)
);

CREATE TABLE llm_budgets (
 account_id TEXT NOT NULL,
 period_start DATE NOT NULL CHECK(date_trunc('month',period_start::timestamp)::date=period_start),
 currency CHAR(3) NOT NULL CHECK(currency ~ '^[A-Z]{3}$'),
 limit_micros BIGINT NOT NULL CHECK(limit_micros>=0),
 reserved_micros BIGINT NOT NULL DEFAULT 0 CHECK(reserved_micros>=0),
 spent_micros BIGINT NOT NULL DEFAULT 0 CHECK(spent_micros>=0),
 token_limit BIGINT NOT NULL CHECK(token_limit>=0),
 reserved_tokens BIGINT NOT NULL DEFAULT 0 CHECK(reserved_tokens>=0),
 used_tokens BIGINT NOT NULL DEFAULT 0 CHECK(used_tokens>=0),
 revision BIGINT NOT NULL DEFAULT 1 CHECK(revision>0),
 created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(account_id,period_start,currency)
);

CREATE TABLE llm_usage_reservations (
 id TEXT PRIMARY KEY CHECK(id ~ '^llmv_[0-9a-f-]{36}$'), account_id TEXT NOT NULL,
 caller_service TEXT NOT NULL, caller_operation_id UUID NOT NULL, caller_group_id TEXT NOT NULL,
 group_limit_micros BIGINT NOT NULL CHECK(group_limit_micros>=0),
 group_token_limit BIGINT NOT NULL CHECK(group_token_limit>=0),
 request_id TEXT,
 budget_period DATE NOT NULL, currency CHAR(3) NOT NULL CHECK(currency ~ '^[A-Z]{3}$'),
 initial_reserved_micros BIGINT NOT NULL CHECK(initial_reserved_micros>=0),
 initial_reserved_tokens BIGINT NOT NULL CHECK(initial_reserved_tokens>=0),
 remaining_hold_micros BIGINT NOT NULL CHECK(remaining_hold_micros>=0),
 remaining_hold_tokens BIGINT NOT NULL CHECK(remaining_hold_tokens>=0),
 hold_generation BIGINT NOT NULL DEFAULT 1 CHECK(hold_generation>0),
 retry_eligible BOOLEAN NOT NULL DEFAULT FALSE,
 actual_micros BIGINT CHECK(actual_micros>=0), actual_tokens BIGINT CHECK(actual_tokens>=0),
 settlement_state TEXT NOT NULL DEFAULT 'unclaimed'
  CHECK(settlement_state IN ('unclaimed','reserved','partially_settled','settled','unknown','released')),
 price_version TEXT NOT NULL CHECK(char_length(price_version) BETWEEN 1 AND 64),
 expires_at TIMESTAMPTZ NOT NULL,
 revision BIGINT NOT NULL DEFAULT 1 CHECK(revision>0),
 created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 UNIQUE(account_id,id),
 UNIQUE(account_id,caller_service,caller_operation_id),
 CHECK(settlement_state<>'released' OR (remaining_hold_micros=0 AND remaining_hold_tokens=0))
);
-- One prepared request may claim at most one reservation.
CREATE UNIQUE INDEX llm_reservation_request ON llm_usage_reservations(account_id,request_id) WHERE request_id IS NOT NULL;
CREATE INDEX llm_reservation_group ON llm_usage_reservations(account_id,caller_service,caller_group_id);
CREATE INDEX llm_reservation_expiry ON llm_usage_reservations(settlement_state,expires_at);

CREATE TABLE llm_requests (
 id TEXT PRIMARY KEY CHECK(id ~ '^llmr_[0-9a-f-]{36}$'), account_id TEXT NOT NULL,
 caller_service TEXT NOT NULL, caller_operation_id UUID NOT NULL, caller_group_id TEXT NOT NULL,
 request_hash TEXT NOT NULL CHECK(request_hash ~ '^sha256-[a-f0-9]{64}$'),
 model_key TEXT NOT NULL CHECK(model_key ~ '^[a-z][a-z0-9_.-]{0,63}$'),
 catalog_version TEXT NOT NULL, price_version TEXT NOT NULL,
 model_snapshot JSONB NOT NULL CHECK(jsonb_typeof(model_snapshot)='object'),
 price_snapshot JSONB NOT NULL CHECK(jsonb_typeof(price_snapshot)='object'),
 request_payload JSONB CHECK(request_payload IS NULL OR jsonb_typeof(request_payload)='object'),
 payload_state TEXT NOT NULL DEFAULT 'retained' CHECK(payload_state IN ('retained','purged')),
 output_limit INTEGER NOT NULL CHECK(output_limit>0),
 deadline_at TIMESTAMPTZ NOT NULL,
 state TEXT NOT NULL DEFAULT 'prepared'
  CHECK(state IN ('prepared','dispatching','streaming','succeeded','failed','cancelled','unknown')),
 failure_class TEXT CHECK(failure_class IS NULL OR char_length(failure_class) BETWEEN 1 AND 64),
 attempts_used INTEGER NOT NULL DEFAULT 0 CHECK(attempts_used BETWEEN 0 AND 2),
 retry_eligible BOOLEAN NOT NULL DEFAULT FALSE,
 cancel_requested_at TIMESTAMPTZ, closed_at TIMESTAMPTZ,
 result_payload JSONB CHECK(result_payload IS NULL OR jsonb_typeof(result_payload)='object'),
 result_hash TEXT CHECK(result_hash IS NULL OR result_hash ~ '^sha256-[a-f0-9]{64}$'),
 result_revision BIGINT NOT NULL DEFAULT 0 CHECK(result_revision>=0),
 revision BIGINT NOT NULL DEFAULT 1 CHECK(revision>0),
 created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 retained_until TIMESTAMPTZ NOT NULL,
 UNIQUE(account_id,id),
 UNIQUE(account_id,caller_service,caller_operation_id),
 -- A succeeded request always keeps a complete, hashed result identity.
 CHECK(state<>'succeeded' OR (result_hash IS NOT NULL AND result_revision>0)),
 -- Purged payloads become tombstones that can never be replayed as a first call.
 CHECK(payload_state<>'purged' OR request_payload IS NULL),
 CHECK(state<>'prepared' OR result_hash IS NULL)
);
CREATE INDEX llm_request_state ON llm_requests(account_id,state,deadline_at);
CREATE INDEX llm_request_retention ON llm_requests(retained_until) WHERE payload_state='retained';

CREATE TABLE llm_attempts (
 id TEXT PRIMARY KEY CHECK(id ~ '^llma_[0-9a-f-]{36}$'), account_id TEXT NOT NULL, request_id TEXT NOT NULL,
 attempt_number INTEGER NOT NULL CHECK(attempt_number BETWEEN 1 AND 2),
 reservation_id TEXT NOT NULL, reservation_generation BIGINT NOT NULL CHECK(reservation_generation>0),
 dispatch_state TEXT NOT NULL DEFAULT 'dispatching'
  CHECK(dispatch_state IN ('dispatching','streaming','succeeded','rejected','unknown')),
 dispatch_epoch BIGINT NOT NULL CHECK(dispatch_epoch>=0),
 permit_digest TEXT NOT NULL CHECK(permit_digest ~ '^sha256-[a-f0-9]{64}$'),
 permit_claimed_at TIMESTAMPTZ, permit_released_at TIMESTAMPTZ,
 limit_key TEXT NOT NULL CHECK(char_length(limit_key) BETWEEN 1 AND 120),
 provider_request_id TEXT, error_class TEXT,
 dispatched_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 transport_finished_at TIMESTAMPTZ, finished_at TIMESTAMPTZ,
 created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 UNIQUE(account_id,id),
 UNIQUE(account_id,request_id,attempt_number),
 -- A one-shot dispatch permit can never be claimed twice.
 UNIQUE(account_id,permit_digest),
 CHECK(dispatch_state NOT IN ('succeeded','rejected') OR finished_at IS NOT NULL)
);
-- At most one live attempt per request; unknown keeps holding the slot.
CREATE UNIQUE INDEX llm_attempt_active ON llm_attempts(account_id,request_id)
 WHERE dispatch_state IN ('dispatching','streaming','unknown');
CREATE INDEX llm_attempt_permit ON llm_attempts(limit_key) WHERE permit_released_at IS NULL;

CREATE TABLE llm_usage_measurements (
 id TEXT PRIMARY KEY CHECK(id ~ '^llmu_[0-9a-f-]{36}$'), account_id TEXT NOT NULL, attempt_id TEXT NOT NULL,
 measurement_key TEXT NOT NULL CHECK(char_length(measurement_key) BETWEEN 1 AND 120),
 cost_component TEXT NOT NULL
  CHECK(cost_component IN ('input_cached','input_uncached','output','budget_total_tokens')),
 currency CHAR(3) NOT NULL CHECK(currency ~ '^[A-Z]{3}$'),
 usage_dimensions JSONB NOT NULL CHECK(jsonb_typeof(usage_dimensions)='object'),
 price_snapshot JSONB NOT NULL CHECK(jsonb_typeof(price_snapshot)='object'),
 cost_micros BIGINT CHECK(cost_micros>=0), token_count BIGINT CHECK(token_count>=0),
 certainty TEXT NOT NULL CHECK(certainty IN ('known','unknown')),
 evidence_kind TEXT NOT NULL
  CHECK(evidence_kind IN ('provider_reported','derived_from_usage','verified_rejection','operator_attested')),
 evidence_rank INTEGER NOT NULL CHECK(evidence_rank BETWEEN 1 AND 100),
 provider_revision TEXT, supersedes_id TEXT,
 evidence_digest TEXT NOT NULL CHECK(evidence_digest ~ '^sha256-[a-f0-9]{64}$'),
 created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 UNIQUE(account_id,id),
 UNIQUE(account_id,attempt_id,measurement_key),
 -- known evidence always carries a value; unknown never fabricates a zero.
 CHECK(certainty<>'known' OR cost_micros IS NOT NULL OR token_count IS NOT NULL),
 CHECK(certainty<>'unknown' OR (cost_micros IS NULL AND token_count IS NULL))
);
CREATE INDEX llm_measurement_attempt ON llm_usage_measurements(account_id,attempt_id);

CREATE TABLE llm_measurement_dispositions (
 account_id TEXT NOT NULL, measurement_id TEXT NOT NULL,
 state TEXT NOT NULL CHECK(state IN ('pending','applied','obsolete','disputed')),
 reason TEXT NOT NULL DEFAULT '' CHECK(char_length(reason)<=240),
 resolved_by_measurement_id TEXT,
 created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(account_id,measurement_id)
);
CREATE INDEX llm_disposition_open ON llm_measurement_dispositions(account_id,updated_at) WHERE state IN ('pending','disputed');

CREATE TABLE llm_cost_positions (
 account_id TEXT NOT NULL, attempt_id TEXT NOT NULL,
 cost_component TEXT NOT NULL CHECK(cost_component IN ('input_cached','input_uncached','output')),
 currency CHAR(3) NOT NULL CHECK(currency ~ '^[A-Z]{3}$'),
 current_measurement_id TEXT NOT NULL,
 booked_cost_micros BIGINT NOT NULL CHECK(booked_cost_micros>=0),
 certainty TEXT NOT NULL CHECK(certainty IN ('known','unknown')),
 revision BIGINT NOT NULL DEFAULT 1 CHECK(revision>0),
 created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(account_id,attempt_id,cost_component,currency)
);

CREATE TABLE llm_token_positions (
 account_id TEXT NOT NULL, attempt_id TEXT NOT NULL,
 dimension TEXT NOT NULL CHECK(dimension='budget_total_tokens'),
 current_measurement_id TEXT NOT NULL,
 booked_tokens BIGINT NOT NULL CHECK(booked_tokens>=0),
 certainty TEXT NOT NULL CHECK(certainty IN ('known','unknown')),
 revision BIGINT NOT NULL DEFAULT 1 CHECK(revision>0),
 created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(account_id,attempt_id,dimension)
);

CREATE TABLE llm_settlement_receipts (
 id TEXT PRIMARY KEY CHECK(id ~ '^llmc_[0-9a-f-]{36}$'), account_id TEXT NOT NULL, attempt_id TEXT NOT NULL,
 position_kind TEXT NOT NULL CHECK(position_kind IN ('cost','token')),
 position_key TEXT NOT NULL CHECK(char_length(position_key) BETWEEN 1 AND 64),
 position_revision BIGINT NOT NULL CHECK(position_revision>0),
 measurement_id TEXT NOT NULL,
 old_booked_value BIGINT NOT NULL CHECK(old_booked_value>=0),
 new_booked_value BIGINT NOT NULL CHECK(new_booked_value>=0),
 old_remaining_hold BIGINT NOT NULL CHECK(old_remaining_hold>=0),
 new_remaining_hold BIGINT NOT NULL CHECK(new_remaining_hold>=0),
 budget_period DATE NOT NULL, currency CHAR(3) NOT NULL CHECK(currency ~ '^[A-Z]{3}$'),
 created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 UNIQUE(account_id,id),
 UNIQUE(account_id,attempt_id,position_kind,position_key,position_revision)
);

CREATE TABLE llm_result_consumers (
 account_id TEXT NOT NULL, request_id TEXT NOT NULL,
 caller_service TEXT NOT NULL, consumer_key TEXT NOT NULL CHECK(char_length(consumer_key) BETWEEN 1 AND 120),
 state TEXT NOT NULL DEFAULT 'pending' CHECK(state IN ('pending','consumed','abandoned')),
 registered_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 released_at TIMESTAMPTZ,
 PRIMARY KEY(account_id,request_id,caller_service,consumer_key),
 CHECK((state='pending')=(released_at IS NULL))
);
CREATE INDEX llm_result_consumer_pending ON llm_result_consumers(account_id,request_id) WHERE state='pending';

-- Infrastructure admission shared by one provider credential/deployment. It is
-- deliberately account-free and is never joined into account-scoped queries.
CREATE TABLE platform_llm_limits (
 limit_key TEXT PRIMARY KEY CHECK(char_length(limit_key) BETWEEN 1 AND 120),
 capacity INTEGER NOT NULL CHECK(capacity>0),
 active_count INTEGER NOT NULL DEFAULT 0 CHECK(active_count>=0),
 rate_limit INTEGER NOT NULL CHECK(rate_limit>0),
 window_seconds INTEGER NOT NULL DEFAULT 60 CHECK(window_seconds>0),
 window_start TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 window_count INTEGER NOT NULL DEFAULT 0 CHECK(window_count>=0),
 revision BIGINT NOT NULL DEFAULT 1 CHECK(revision>0),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 CHECK(active_count<=capacity)
);
