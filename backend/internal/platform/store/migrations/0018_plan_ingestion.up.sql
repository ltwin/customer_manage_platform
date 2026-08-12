ALTER TABLE idempotency_records
    DROP CONSTRAINT idempotency_records_operation_check;
ALTER TABLE idempotency_records
    ADD CONSTRAINT idempotency_records_operation_check CHECK (operation IN (
        'order.create.v1',
        'schedule-slot.create.v1',
        'shoot-plan.create.v1',
        'shoot-plan.command.v1',
        'shoot-plan.transition.v1',
        'shoot-plan.run-session.open.v1',
        'shoot-plan.shot.capture.v1',
        'shoot-plan.execution-event.void.v1',
        'shoot-plan.batch-commit.v1',
        'planning-media.upload.v1',
        'planning-media.binding.create.v1',
        'planning-media.binding.release.v1',
        'planning-media.lease.reserve.v1',
        'planning-media.lease.release.v1',
        'plan-ingestion.create.v1',
        'plan-ingestion.preview.v1',
        'plan-ingestion.transition.v1',
        'plan-ingestion.commit.v1'
    ));

CREATE TABLE shoot_plan_ingestion_sessions (
    id                       TEXT PRIMARY KEY,
    account_id               TEXT NOT NULL,
    plan_id                  TEXT NOT NULL,
    state                    TEXT NOT NULL CHECK (state IN ('editing','committed','abandoned')),
    revision                 BIGINT NOT NULL DEFAULT 1 CHECK (revision >= 1),
    parser_version           INTEGER NOT NULL CHECK (parser_version = 1),
    source_text              TEXT,
    source_checksum          TEXT NOT NULL,
    source_line_count        INTEGER NOT NULL CHECK (source_line_count BETWEEN 0 AND 1000),
    candidate_snapshot_json  JSONB NOT NULL CHECK (jsonb_typeof(candidate_snapshot_json) = 'object'),
    candidate_schema_version INTEGER NOT NULL DEFAULT 1 CHECK (candidate_schema_version = 1),
    created_at               TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    committed_at             TIMESTAMPTZ,
    abandoned_at             TIMESTAMPTZ,
    redacted_at              TIMESTAMPTZ,
    UNIQUE (account_id, id),
    UNIQUE (account_id, plan_id, id),
    FOREIGN KEY (account_id, plan_id) REFERENCES shoot_plans (account_id, id),
    CHECK ((state = 'editing' AND committed_at IS NULL AND abandoned_at IS NULL) OR
           (state = 'committed' AND committed_at IS NOT NULL AND abandoned_at IS NULL) OR
           (state = 'abandoned' AND committed_at IS NULL AND abandoned_at IS NOT NULL)),
    CHECK ((redacted_at IS NULL) OR (state <> 'editing' AND source_text IS NULL))
);

CREATE UNIQUE INDEX shoot_plan_ingestion_one_editing_idx
    ON shoot_plan_ingestion_sessions (account_id, plan_id)
    WHERE state = 'editing';
CREATE INDEX shoot_plan_ingestion_retention_idx
    ON shoot_plan_ingestion_sessions (state, updated_at, id);

CREATE TABLE shoot_plan_reference_links (
    id                  TEXT PRIMARY KEY,
    account_id          TEXT NOT NULL,
    plan_id             TEXT NOT NULL,
    target_kind         TEXT NOT NULL CHECK (target_kind IN ('plan','shot')),
    target_id           TEXT NOT NULL,
    url                 TEXT NOT NULL CHECK (octet_length(url) BETWEEN 1 AND 2048),
    url_digest          TEXT NOT NULL,
    label               TEXT CHECK (char_length(label) <= 160),
    source_hint         TEXT CHECK (char_length(source_hint) <= 80),
    source_session_id   TEXT NOT NULL,
    source_candidate_id TEXT NOT NULL,
    revision            BIGINT NOT NULL DEFAULT 1 CHECK (revision >= 1),
    removed_at          TIMESTAMPTZ,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (account_id, id),
    UNIQUE (account_id, plan_id, id),
    UNIQUE (account_id, source_session_id, source_candidate_id),
    FOREIGN KEY (account_id, plan_id) REFERENCES shoot_plans (account_id, id),
    FOREIGN KEY (account_id, plan_id, source_session_id)
        REFERENCES shoot_plan_ingestion_sessions (account_id, plan_id, id),
    CHECK ((target_kind = 'plan' AND target_id = plan_id) OR target_kind = 'shot')
);
CREATE INDEX shoot_plan_reference_links_target_idx
    ON shoot_plan_reference_links (account_id, plan_id, target_kind, target_id, created_at, id)
    WHERE removed_at IS NULL;

CREATE TABLE shoot_plan_build_observations (
    account_id                    TEXT NOT NULL,
    plan_id                       TEXT NOT NULL,
    first_ingestion_at            TIMESTAMPTZ NOT NULL,
    first_ready_at                TIMESTAMPTZ,
    last_activity_at              TIMESTAMPTZ NOT NULL,
    active_seconds                BIGINT NOT NULL DEFAULT 0 CHECK (active_seconds >= 0),
    idle_rule_version             INTEGER NOT NULL DEFAULT 1 CHECK (idle_rule_version = 1),
    outcome                       TEXT NOT NULL DEFAULT 'in_progress'
                                  CHECK (outcome IN ('in_progress','first_ready','abandoned')),
    terminal_at                   TIMESTAMPTZ,
    post_terminal_ingestion_count BIGINT NOT NULL DEFAULT 0 CHECK (post_terminal_ingestion_count >= 0),
    revision                      BIGINT NOT NULL DEFAULT 1 CHECK (revision >= 1),
    PRIMARY KEY (account_id, plan_id),
    FOREIGN KEY (account_id, plan_id) REFERENCES shoot_plans (account_id, id),
    CHECK ((outcome = 'in_progress' AND terminal_at IS NULL AND first_ready_at IS NULL) OR
           (outcome = 'first_ready' AND terminal_at IS NOT NULL AND first_ready_at IS NOT NULL) OR
           (outcome = 'abandoned' AND terminal_at IS NOT NULL AND first_ready_at IS NULL))
);

CREATE TABLE shoot_plan_build_activity_ticks (
    account_id             TEXT NOT NULL,
    plan_id                TEXT NOT NULL,
    session_id             TEXT NOT NULL,
    tick_id                TEXT NOT NULL,
    kind                   TEXT NOT NULL CHECK (kind IN ('create','preview','commit','abandon','ready')),
    server_received_at     TIMESTAMPTZ NOT NULL,
    accepted_delta_seconds INTEGER NOT NULL DEFAULT 0 CHECK (accepted_delta_seconds BETWEEN 0 AND 300),
    PRIMARY KEY (account_id, plan_id, tick_id),
    FOREIGN KEY (account_id, plan_id) REFERENCES shoot_plans (account_id, id),
    FOREIGN KEY (account_id, plan_id, session_id)
        REFERENCES shoot_plan_ingestion_sessions (account_id, plan_id, id)
);
CREATE INDEX shoot_plan_build_activity_ticks_session_idx
    ON shoot_plan_build_activity_ticks (account_id, plan_id, session_id, server_received_at, tick_id);
