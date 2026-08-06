CREATE TABLE shoot_plans (
    id                      TEXT PRIMARY KEY,
    account_id              TEXT NOT NULL REFERENCES accounts (id),
    title                   TEXT NOT NULL CHECK (char_length(btrim(title)) BETWEEN 1 AND 160),
    subject                 TEXT NOT NULL CHECK (char_length(btrim(subject)) BETWEEN 1 AND 240),
    status                  TEXT NOT NULL DEFAULT 'draft'
                            CHECK (status IN ('draft', 'ready', 'in_progress', 'completed', 'archived')),
    creative_brief          JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(creative_brief) = 'object'),
    planned_look_count      INTEGER CHECK (planned_look_count BETWEEN 1 AND 999),
    planned_scene_count     INTEGER CHECK (planned_scene_count BETWEEN 1 AND 999),
    revision                BIGINT NOT NULL DEFAULT 1 CHECK (revision >= 1),
    execution_fact_revision BIGINT NOT NULL DEFAULT 0 CHECK (execution_fact_revision >= 0),
    created_at              TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    completed_at            TIMESTAMPTZ,
    archived_at             TIMESTAMPTZ,
    UNIQUE (account_id, id)
);

CREATE INDEX shoot_plans_account_updated_idx
    ON shoot_plans (account_id, updated_at DESC, id DESC);
CREATE INDEX shoot_plans_account_status_updated_idx
    ON shoot_plans (account_id, status, updated_at DESC, id DESC);

CREATE TABLE shoot_plan_shots (
    id                       TEXT PRIMARY KEY,
    account_id               TEXT NOT NULL,
    plan_id                  TEXT NOT NULL,
    position                 INTEGER NOT NULL CHECK (position >= 1),
    title                    TEXT NOT NULL CHECK (char_length(btrim(title)) BETWEEN 1 AND 160),
    scene                    TEXT CHECK (char_length(scene) <= 1000),
    action                   TEXT CHECK (char_length(action) <= 1000),
    expression               TEXT CHECK (char_length(expression) <= 1000),
    composition              TEXT CHECK (char_length(composition) <= 1000),
    lighting_text            TEXT CHECK (char_length(lighting_text) <= 1000),
    notes                    TEXT CHECK (char_length(notes) <= 2000),
    framing_tag              TEXT CHECK (framing_tag IS NULL OR framing_tag IN
                                ('extreme_closeup','closeup','medium_closeup','medium','full','wide','extreme_wide','other')),
    lighting_direction_tag   TEXT CHECK (lighting_direction_tag IS NULL OR lighting_direction_tag IN
                                ('front','side','back','top','bottom','mixed','natural','other')),
    lighting_quality_tag     TEXT CHECK (lighting_quality_tag IS NULL OR lighting_quality_tag IN
                                ('hard','soft','mixed','natural','other')),
    palette_tag              TEXT CHECK (palette_tag IS NULL OR palette_tag IN
                                ('warm','cool','neutral','monochrome','high_saturation','low_saturation','mixed','other')),
    shot_type_tag            TEXT CHECK (shot_type_tag IS NULL OR shot_type_tag IN
                                ('portrait','action','interaction','environment','detail','silhouette','narrative','other')),
    taxonomy_version         INTEGER NOT NULL DEFAULT 1 CHECK (taxonomy_version = 1),
    revision                 BIGINT NOT NULL DEFAULT 1 CHECK (revision >= 1),
    execution_revision       BIGINT NOT NULL DEFAULT 0 CHECK (execution_revision >= 0),
    next_event_seq           BIGINT NOT NULL DEFAULT 1 CHECK (next_event_seq >= 1),
    current_outcome_event_id TEXT,
    removed_at               TIMESTAMPTZ,
    created_at               TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (account_id, id),
    UNIQUE (account_id, plan_id, id),
    FOREIGN KEY (account_id, plan_id) REFERENCES shoot_plans (account_id, id)
);

CREATE UNIQUE INDEX shoot_plan_shots_current_position_idx
    ON shoot_plan_shots (account_id, plan_id, position) WHERE removed_at IS NULL;

CREATE TABLE shoot_plan_readiness_items (
    id                            TEXT PRIMARY KEY,
    account_id                    TEXT NOT NULL,
    plan_id                       TEXT NOT NULL,
    category                      TEXT NOT NULL CHECK (category IN ('styling','location','prop_equipment','other')),
    title                         TEXT NOT NULL CHECK (char_length(btrim(title)) BETWEEN 1 AND 240),
    requirement                   TEXT NOT NULL CHECK (requirement IN ('required','optional')),
    preflight_status              TEXT NOT NULL DEFAULT 'unchecked' CHECK (preflight_status IN ('unchecked','checked')),
    responsibility_hint           TEXT NOT NULL DEFAULT 'unassigned'
                                  CHECK (responsibility_hint IN ('photographer','customer','unassigned')),
    default_preparation_lead_days INTEGER CHECK (default_preparation_lead_days BETWEEN 0 AND 365),
    revision                      BIGINT NOT NULL DEFAULT 1 CHECK (revision >= 1),
    removed_at                    TIMESTAMPTZ,
    created_at                    TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    updated_at                    TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (account_id, id),
    UNIQUE (account_id, plan_id, id),
    FOREIGN KEY (account_id, plan_id) REFERENCES shoot_plans (account_id, id)
);

CREATE TABLE shoot_plan_shot_readiness_links (
    account_id        TEXT NOT NULL,
    plan_id           TEXT NOT NULL,
    shot_id           TEXT NOT NULL,
    readiness_item_id TEXT NOT NULL,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (account_id, plan_id, shot_id, readiness_item_id),
    FOREIGN KEY (account_id, plan_id, shot_id)
        REFERENCES shoot_plan_shots (account_id, plan_id, id),
    FOREIGN KEY (account_id, plan_id, readiness_item_id)
        REFERENCES shoot_plan_readiness_items (account_id, plan_id, id)
);

CREATE TABLE shoot_plan_execution_windows (
    account_id            TEXT NOT NULL,
    plan_id               TEXT NOT NULL,
    source                TEXT NOT NULL CHECK (source IN ('manual','schedule_slot')),
    source_ref            TEXT,
    starts_at             TIMESTAMPTZ NOT NULL,
    ends_at               TIMESTAMPTZ NOT NULL,
    timezone              TEXT NOT NULL CHECK (char_length(btrim(timezone)) BETWEEN 1 AND 255),
    live_window_starts_at TIMESTAMPTZ NOT NULL,
    live_window_ends_at   TIMESTAMPTZ NOT NULL,
    rule_version          INTEGER NOT NULL DEFAULT 1 CHECK (rule_version = 1),
    revision              BIGINT NOT NULL DEFAULT 1 CHECK (revision >= 1),
    PRIMARY KEY (account_id, plan_id),
    FOREIGN KEY (account_id, plan_id) REFERENCES shoot_plans (account_id, id),
    CHECK (ends_at > starts_at),
    CHECK (live_window_starts_at <= starts_at AND live_window_ends_at >= ends_at),
    CHECK ((source = 'manual' AND source_ref IS NULL) OR
           (source = 'schedule_slot' AND source_ref IS NOT NULL))
);

CREATE TABLE shoot_plan_run_sessions (
    id                         TEXT PRIMARY KEY,
    account_id                 TEXT NOT NULL,
    plan_id                    TEXT NOT NULL,
    execution_window_revision  BIGINT,
    opened_at                  TIMESTAMPTZ NOT NULL,
    last_active_at             TIMESTAMPTZ NOT NULL,
    closed_at                  TIMESTAMPTZ,
    capture_mode               TEXT NOT NULL CHECK (capture_mode IN ('live','backfill','unknown')),
    idempotency_key_fingerprint BYTEA NOT NULL CHECK (octet_length(idempotency_key_fingerprint) = 32),
    UNIQUE (account_id, id),
    UNIQUE (account_id, plan_id, id),
    FOREIGN KEY (account_id, plan_id) REFERENCES shoot_plans (account_id, id),
    CHECK (closed_at IS NULL OR closed_at >= opened_at)
);

CREATE INDEX shoot_plan_run_sessions_open_idx
    ON shoot_plan_run_sessions (account_id, plan_id, opened_at) WHERE closed_at IS NULL;

CREATE TABLE shoot_plan_execution_events (
    id                    TEXT PRIMARY KEY,
    account_id            TEXT NOT NULL,
    plan_id               TEXT NOT NULL,
    shot_id               TEXT NOT NULL,
    session_id            TEXT,
    shot_event_seq        BIGINT NOT NULL CHECK (shot_event_seq >= 1),
    plan_revision         BIGINT NOT NULL CHECK (plan_revision >= 1),
    result                TEXT NOT NULL CHECK (result IN ('captured','skipped','cleared')),
    skip_reason           TEXT CHECK (skip_reason IS NULL OR skip_reason IN
                            ('preparation_missing','time_insufficient','location_unavailable','subject_unavailable',
                             'creative_change','technical_failure','other')),
    notes                 TEXT CHECK (char_length(notes) <= 1000),
    checked_at            TIMESTAMPTZ NOT NULL,
    capture_mode          TEXT NOT NULL CHECK (capture_mode IN ('live','backfill','unknown')),
    supersedes_event_id   TEXT,
    revision              BIGINT NOT NULL DEFAULT 1 CHECK (revision = 1),
    UNIQUE (account_id, id),
    UNIQUE (account_id, plan_id, shot_id, id),
    UNIQUE (account_id, plan_id, shot_id, shot_event_seq),
    FOREIGN KEY (account_id, plan_id, shot_id)
        REFERENCES shoot_plan_shots (account_id, plan_id, id),
    FOREIGN KEY (account_id, plan_id, session_id)
        REFERENCES shoot_plan_run_sessions (account_id, plan_id, id),
    FOREIGN KEY (account_id, plan_id, shot_id, supersedes_event_id)
        REFERENCES shoot_plan_execution_events (account_id, plan_id, shot_id, id),
    CHECK ((result = 'skipped' AND skip_reason IS NOT NULL) OR
           (result <> 'skipped' AND skip_reason IS NULL))
);

ALTER TABLE shoot_plan_shots
    ADD CONSTRAINT shoot_plan_shots_current_outcome_fk
    FOREIGN KEY (account_id, plan_id, id, current_outcome_event_id)
    REFERENCES shoot_plan_execution_events (account_id, plan_id, shot_id, id)
    DEFERRABLE INITIALLY DEFERRED;

CREATE TABLE shoot_plan_execution_event_voids (
    id              TEXT PRIMARY KEY,
    account_id      TEXT NOT NULL,
    plan_id         TEXT NOT NULL,
    shot_id         TEXT NOT NULL,
    target_event_id TEXT NOT NULL,
    shot_event_seq  BIGINT NOT NULL CHECK (shot_event_seq >= 1),
    reason          TEXT NOT NULL CHECK (char_length(btrim(reason)) BETWEEN 1 AND 500),
    voided_by_account_id TEXT NOT NULL,
    voided_at       TIMESTAMPTZ NOT NULL,
    revision        BIGINT NOT NULL DEFAULT 1 CHECK (revision = 1),
    UNIQUE (account_id, id),
    UNIQUE (account_id, plan_id, shot_id, target_event_id),
    UNIQUE (account_id, plan_id, shot_id, shot_event_seq),
    FOREIGN KEY (account_id, plan_id, shot_id, target_event_id)
        REFERENCES shoot_plan_execution_events (account_id, plan_id, shot_id, id),
    FOREIGN KEY (voided_by_account_id) REFERENCES accounts (id),
    CHECK (voided_by_account_id = account_id)
);

CREATE TABLE shoot_plan_finalization_snapshots (
    id                               TEXT PRIMARY KEY,
    account_id                       TEXT NOT NULL,
    plan_id                          TEXT NOT NULL,
    finalization_revision            BIGINT NOT NULL CHECK (finalization_revision >= 1),
    plan_revision                    BIGINT NOT NULL CHECK (plan_revision >= 1),
    current_shot_ids                 JSONB NOT NULL CHECK (jsonb_typeof(current_shot_ids) = 'array'),
    outcome_event_refs               JSONB NOT NULL CHECK (jsonb_typeof(outcome_event_refs) = 'array'),
    preparation_missing_event_ids    JSONB NOT NULL CHECK (jsonb_typeof(preparation_missing_event_ids) = 'array'),
    execution_fact_revision          BIGINT NOT NULL CHECK (execution_fact_revision >= 0),
    finalized_at                     TIMESTAMPTZ NOT NULL,
    UNIQUE (account_id, id),
    UNIQUE (account_id, plan_id, finalization_revision),
    FOREIGN KEY (account_id, plan_id) REFERENCES shoot_plans (account_id, id)
);

CREATE TABLE planning_reminder_account_generations (
    account_id         TEXT PRIMARY KEY REFERENCES accounts (id),
    target_generation  BIGINT NOT NULL DEFAULT 0 CHECK (target_generation >= 0),
    applied_generation BIGINT NOT NULL DEFAULT 0 CHECK (applied_generation >= 0),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    CHECK (applied_generation <= target_generation)
);

CREATE TABLE planning_reminder_generation_work (
    account_id      TEXT NOT NULL REFERENCES accounts (id),
    generation      BIGINT NOT NULL CHECK (generation >= 1),
    plan_id         TEXT NOT NULL,
    mutation_kind   TEXT NOT NULL CHECK (mutation_kind IN
                        ('crm_plan_link_changed','crm_order_lifecycle_changed','crm_schedule_changed',
                         'settings_timezone_changed','plan_archived','assignment_activated',
                         'assignment_revoked','shoot_started')),
    source_event_id TEXT,
    state           TEXT NOT NULL DEFAULT 'pending' CHECK (state IN ('pending','applied','quarantined')),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    applied_at      TIMESTAMPTZ,
    PRIMARY KEY (account_id, generation),
    UNIQUE (account_id, generation, plan_id, source_event_id, mutation_kind),
    FOREIGN KEY (account_id, plan_id) REFERENCES shoot_plans (account_id, id),
    CHECK ((mutation_kind IN ('assignment_activated','assignment_revoked') AND source_event_id IS NOT NULL) OR
           (mutation_kind NOT IN ('assignment_activated','assignment_revoked') AND source_event_id IS NULL)),
    CHECK ((state = 'applied' AND applied_at IS NOT NULL) OR
           (state <> 'applied' AND applied_at IS NULL))
);

CREATE INDEX planning_reminder_generation_work_state_idx
    ON planning_reminder_generation_work (account_id, state, generation);

CREATE TABLE planning_archive_capability_state (
    singleton_key TEXT PRIMARY KEY CHECK (singleton_key = 'planning-archive-v1'),
    capability    TEXT NOT NULL CHECK (capability IN
                    ('core-v1','planning-share-v1','planning-share-reminder-v1')),
    revision      BIGINT NOT NULL CHECK (revision >= 1),
    promoted_at   TIMESTAMPTZ NOT NULL
);

INSERT INTO planning_archive_capability_state
    (singleton_key, capability, revision, promoted_at)
VALUES ('planning-archive-v1', 'core-v1', 1, clock_timestamp());

CREATE VIEW shoot_plan_list_projection AS
SELECT p.account_id,
       p.id,
       p.title,
       p.subject,
       p.status,
       p.planned_look_count,
       p.planned_scene_count,
       p.revision,
       p.execution_fact_revision,
       p.created_at,
       p.updated_at,
       count(s.id)::BIGINT AS planned_shot_count
FROM shoot_plans p
LEFT JOIN shoot_plan_shots s
  ON s.account_id = p.account_id AND s.plan_id = p.id AND s.removed_at IS NULL
GROUP BY p.account_id, p.id;

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
        'shoot-plan.batch-commit.v1'
    ));
