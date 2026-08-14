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
        'plan-ingestion.commit.v1',
        'shoot-plan.crm-link.v1'
    ));

CREATE TABLE plan_crm_connections (
    account_id             TEXT NOT NULL,
    plan_id                TEXT NOT NULL,
    customer_id            TEXT,
    order_id               TEXT,
    link_epoch_id          TEXT,
    linked_order_snapshot  JSONB,
    state                  TEXT NOT NULL DEFAULT 'independent'
                           CHECK (state IN ('independent', 'customer_linked', 'order_linked', 'order_cancelled', 'order_deleted')),
    connection_revision    BIGINT NOT NULL DEFAULT 1 CHECK (connection_revision >= 1),
    next_event_seq         BIGINT NOT NULL DEFAULT 1 CHECK (next_event_seq >= 1),
    updated_at             TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (account_id, plan_id),
    UNIQUE (account_id, plan_id, connection_revision),
    FOREIGN KEY (account_id, plan_id) REFERENCES shoot_plans (account_id, id),
    FOREIGN KEY (account_id, customer_id) REFERENCES customers (account_id, id),
    FOREIGN KEY (account_id, order_id) REFERENCES orders (account_id, id) ON DELETE SET NULL,
    CHECK (
        (state = 'independent' AND customer_id IS NULL AND order_id IS NULL
            AND link_epoch_id IS NULL AND linked_order_snapshot IS NULL)
        OR (state = 'customer_linked' AND customer_id IS NOT NULL AND order_id IS NULL
            AND link_epoch_id IS NULL AND linked_order_snapshot IS NULL)
        OR (state = 'order_linked' AND customer_id IS NOT NULL AND order_id IS NOT NULL
            AND link_epoch_id IS NOT NULL AND linked_order_snapshot IS NOT NULL)
        OR (state = 'order_cancelled' AND customer_id IS NOT NULL AND order_id IS NOT NULL
            AND link_epoch_id IS NOT NULL AND linked_order_snapshot IS NOT NULL)
        OR (state = 'order_deleted' AND customer_id IS NOT NULL AND order_id IS NULL
            AND link_epoch_id IS NOT NULL AND linked_order_snapshot IS NOT NULL)
    ),
    CHECK (linked_order_snapshot IS NULL OR jsonb_typeof(linked_order_snapshot) = 'object')
);

CREATE INDEX plan_crm_connections_customer_idx
    ON plan_crm_connections (account_id, customer_id, plan_id)
    WHERE customer_id IS NOT NULL;
CREATE INDEX plan_crm_connections_order_idx
    ON plan_crm_connections (account_id, order_id, plan_id)
    WHERE order_id IS NOT NULL;

CREATE TABLE plan_schedule_projections (
    account_id               TEXT NOT NULL,
    plan_id                  TEXT NOT NULL,
    order_id                 TEXT,
    slot_id                  TEXT,
    slot_source_fingerprint  TEXT,
    starts_at                TIMESTAMPTZ,
    ends_at                  TIMESTAMPTZ,
    timezone                 TEXT,
    status                   TEXT NOT NULL
                             CHECK (status IN (
                                 'missing_slot',
                                 'active_applied',
                                 'active_manual_override',
                                 'active_unapplied',
                                 'inactive_past',
                                 'inactive_order_cancelled',
                                 'inactive_order_deleted',
                                 'inactive_unlinked'
                             )),
    apply_suppressed         BOOLEAN NOT NULL DEFAULT FALSE,
    rule_version             INTEGER NOT NULL DEFAULT 1 CHECK (rule_version = 1),
    projection_revision      BIGINT NOT NULL DEFAULT 1 CHECK (projection_revision >= 1),
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (account_id, plan_id),
    FOREIGN KEY (account_id, plan_id) REFERENCES shoot_plans (account_id, id),
    FOREIGN KEY (account_id, plan_id) REFERENCES plan_crm_connections (account_id, plan_id),
    FOREIGN KEY (account_id, order_id) REFERENCES orders (account_id, id) ON DELETE SET NULL,
    FOREIGN KEY (account_id, slot_id) REFERENCES schedule_slots (account_id, id) ON DELETE SET NULL,
    CHECK ((starts_at IS NULL) = (ends_at IS NULL)),
    CHECK (ends_at IS NULL OR ends_at > starts_at)
);

CREATE INDEX plan_schedule_projections_slot_idx
    ON plan_schedule_projections (account_id, slot_id, plan_id)
    WHERE slot_id IS NOT NULL;
CREATE INDEX plan_schedule_projections_order_idx
    ON plan_schedule_projections (account_id, order_id, plan_id)
    WHERE order_id IS NOT NULL;

CREATE TABLE plan_crm_connection_events (
    id                         TEXT PRIMARY KEY,
    account_id                 TEXT NOT NULL,
    plan_id                    TEXT NOT NULL,
    crm_event_seq              BIGINT NOT NULL CHECK (crm_event_seq >= 1),
    kind                       TEXT NOT NULL CHECK (kind IN (
                                   'customer_linked',
                                   'order_linked',
                                   'order_unlinked',
                                   'customer_unlinked',
                                   'customer_merged',
                                   'order_cancelled',
                                   'order_deleted',
                                   'schedule_projected',
                                   'schedule_cleared',
                                   'schedule_adopted',
                                   'manual_overrode',
                                   'window_suppressed'
                               )),
    from_refs                  JSONB NOT NULL CHECK (jsonb_typeof(from_refs) = 'object'),
    to_refs                    JSONB NOT NULL CHECK (jsonb_typeof(to_refs) = 'object'),
    link_epoch_snapshot        JSONB,
    source_fingerprint         TEXT,
    connection_revision_after  BIGINT CHECK (connection_revision_after IS NULL OR connection_revision_after >= 1),
    projection_revision_after  BIGINT CHECK (projection_revision_after IS NULL OR projection_revision_after >= 1),
    plan_revision_after        BIGINT CHECK (plan_revision_after IS NULL OR plan_revision_after >= 1),
    window_revision_after      BIGINT CHECK (window_revision_after IS NULL OR window_revision_after >= 1),
    occurred_at                TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (account_id, id),
    UNIQUE (account_id, plan_id, crm_event_seq),
    FOREIGN KEY (account_id, plan_id) REFERENCES shoot_plans (account_id, id),
    CHECK (connection_revision_after IS NOT NULL OR projection_revision_after IS NOT NULL),
    CHECK (link_epoch_snapshot IS NULL OR jsonb_typeof(link_epoch_snapshot) = 'object')
);

CREATE INDEX plan_crm_connection_events_plan_idx
    ON plan_crm_connection_events (account_id, plan_id, crm_event_seq);

CREATE TABLE planning_reminder_generation_resolutions (
    account_id  TEXT NOT NULL,
    generation  BIGINT NOT NULL CHECK (generation >= 1),
    plan_id     TEXT NOT NULL,
    resolution  TEXT NOT NULL CHECK (resolution = 'lifecycle_applied'),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (account_id, generation),
    FOREIGN KEY (account_id, generation)
        REFERENCES planning_reminder_generation_work (account_id, generation),
    FOREIGN KEY (account_id, plan_id) REFERENCES shoot_plans (account_id, id)
);

INSERT INTO plan_crm_connections (account_id, plan_id, state, connection_revision, next_event_seq, updated_at)
SELECT account_id, id, 'independent', 1, 1, clock_timestamp()
FROM shoot_plans
ON CONFLICT (account_id, plan_id) DO NOTHING;

CREATE OR REPLACE VIEW shoot_plan_list_projection AS
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
       count(s.id)::BIGINT AS planned_shot_count,
       c.customer_id AS crm_customer_id,
       c.order_id AS crm_order_id
FROM shoot_plans p
LEFT JOIN shoot_plan_shots s
  ON s.account_id = p.account_id AND s.plan_id = p.id AND s.removed_at IS NULL
LEFT JOIN plan_crm_connections c
  ON c.account_id = p.account_id AND c.plan_id = p.id
GROUP BY p.account_id, p.id, c.customer_id, c.order_id;

CREATE VIEW planning_summary_by_customer AS
WITH membership AS (
    SELECT c.account_id,
           c.customer_id AS scope_id,
           p.id AS plan_id,
           p.title,
           p.status,
           p.updated_at,
           (SELECT count(*)::BIGINT
              FROM shoot_plan_shots s
             WHERE s.account_id = p.account_id AND s.plan_id = p.id AND s.removed_at IS NULL) AS shot_count,
           (SELECT count(*)::BIGINT
              FROM shoot_plan_readiness_items r
             WHERE r.account_id = p.account_id AND r.plan_id = p.id AND r.removed_at IS NULL
               AND r.requirement = 'required' AND r.preflight_status = 'unchecked') AS readiness_unchecked_count,
           w.starts_at,
           w.ends_at,
           w.timezone,
           w.source,
           CASE
               WHEN c.state = 'order_cancelled' THEN 'order_cancelled'
               WHEN c.state = 'order_deleted' THEN 'order_deleted'
               ELSE NULL
           END AS link_warning
    FROM plan_crm_connections c
    JOIN shoot_plans p ON p.account_id = c.account_id AND p.id = c.plan_id
    LEFT JOIN shoot_plan_execution_windows w
      ON w.account_id = p.account_id AND w.plan_id = p.id
    WHERE c.customer_id IS NOT NULL
      AND p.status <> 'archived'
),
agg AS (
    SELECT account_id, scope_id,
           count(*)::BIGINT AS plan_count,
           count(*) FILTER (WHERE status IN ('draft', 'ready', 'in_progress'))::BIGINT AS active_plan_count
    FROM membership
    GROUP BY account_id, scope_id
),
primary_plan AS (
    SELECT DISTINCT ON (account_id, scope_id)
           account_id, scope_id, plan_id, title, status, shot_count, readiness_unchecked_count,
           starts_at, ends_at, timezone, source, link_warning
    FROM membership
    ORDER BY account_id, scope_id, updated_at DESC, plan_id DESC
)
SELECT a.account_id,
       a.scope_id AS customer_id,
       a.plan_count,
       a.active_plan_count,
       p.plan_id AS primary_plan_id,
       p.title AS primary_title,
       p.status AS primary_status,
       p.shot_count AS primary_shot_count,
       p.readiness_unchecked_count AS primary_readiness_unchecked_count,
       p.starts_at AS window_starts_at,
       p.ends_at AS window_ends_at,
       p.timezone AS window_timezone,
       p.source AS window_source,
       p.link_warning
FROM agg a
JOIN primary_plan p ON p.account_id = a.account_id AND p.scope_id = a.scope_id;

CREATE VIEW planning_summary_by_order AS
WITH membership AS (
    SELECT c.account_id,
           c.order_id AS scope_id,
           p.id AS plan_id,
           p.title,
           p.status,
           p.updated_at,
           (SELECT count(*)::BIGINT
              FROM shoot_plan_shots s
             WHERE s.account_id = p.account_id AND s.plan_id = p.id AND s.removed_at IS NULL) AS shot_count,
           (SELECT count(*)::BIGINT
              FROM shoot_plan_readiness_items r
             WHERE r.account_id = p.account_id AND r.plan_id = p.id AND r.removed_at IS NULL
               AND r.requirement = 'required' AND r.preflight_status = 'unchecked') AS readiness_unchecked_count,
           w.starts_at,
           w.ends_at,
           w.timezone,
           w.source,
           CASE
               WHEN c.state = 'order_cancelled' THEN 'order_cancelled'
               WHEN c.state = 'order_deleted' THEN 'order_deleted'
               ELSE NULL
           END AS link_warning
    FROM plan_crm_connections c
    JOIN shoot_plans p ON p.account_id = c.account_id AND p.id = c.plan_id
    LEFT JOIN shoot_plan_execution_windows w
      ON w.account_id = p.account_id AND w.plan_id = p.id
    WHERE c.order_id IS NOT NULL
      AND p.status <> 'archived'
),
agg AS (
    SELECT account_id, scope_id,
           count(*)::BIGINT AS plan_count,
           count(*) FILTER (WHERE status IN ('draft', 'ready', 'in_progress'))::BIGINT AS active_plan_count
    FROM membership
    GROUP BY account_id, scope_id
),
primary_plan AS (
    SELECT DISTINCT ON (account_id, scope_id)
           account_id, scope_id, plan_id, title, status, shot_count, readiness_unchecked_count,
           starts_at, ends_at, timezone, source, link_warning
    FROM membership
    ORDER BY account_id, scope_id, updated_at DESC, plan_id DESC
)
SELECT a.account_id,
       a.scope_id AS order_id,
       a.plan_count,
       a.active_plan_count,
       p.plan_id AS primary_plan_id,
       p.title AS primary_title,
       p.status AS primary_status,
       p.shot_count AS primary_shot_count,
       p.readiness_unchecked_count AS primary_readiness_unchecked_count,
       p.starts_at AS window_starts_at,
       p.ends_at AS window_ends_at,
       p.timezone AS window_timezone,
       p.source AS window_source,
       p.link_warning
FROM agg a
JOIN primary_plan p ON p.account_id = a.account_id AND p.scope_id = a.scope_id;

CREATE VIEW planning_summary_by_slot AS
WITH membership AS (
    SELECT pr.account_id,
           pr.slot_id AS scope_id,
           p.id AS plan_id,
           p.title,
           p.status,
           p.updated_at,
           (SELECT count(*)::BIGINT
              FROM shoot_plan_shots s
             WHERE s.account_id = p.account_id AND s.plan_id = p.id AND s.removed_at IS NULL) AS shot_count,
           (SELECT count(*)::BIGINT
              FROM shoot_plan_readiness_items r
             WHERE r.account_id = p.account_id AND r.plan_id = p.id AND r.removed_at IS NULL
               AND r.requirement = 'required' AND r.preflight_status = 'unchecked') AS readiness_unchecked_count,
           w.starts_at,
           w.ends_at,
           w.timezone,
           w.source,
           CASE
               WHEN c.state = 'order_cancelled' THEN 'order_cancelled'
               WHEN c.state = 'order_deleted' THEN 'order_deleted'
               ELSE NULL
           END AS link_warning
    FROM plan_schedule_projections pr
    JOIN plan_crm_connections c
      ON c.account_id = pr.account_id AND c.plan_id = pr.plan_id
    JOIN shoot_plans p ON p.account_id = pr.account_id AND p.id = pr.plan_id
    JOIN schedule_slots sl
      ON sl.account_id = pr.account_id AND sl.id = pr.slot_id
    LEFT JOIN shoot_plan_execution_windows w
      ON w.account_id = p.account_id AND w.plan_id = p.id
    WHERE pr.slot_id IS NOT NULL
      AND c.order_id IS NOT NULL
      AND sl.order_id = c.order_id
      AND p.status <> 'archived'
),
agg AS (
    SELECT account_id, scope_id,
           count(*)::BIGINT AS plan_count,
           count(*) FILTER (WHERE status IN ('draft', 'ready', 'in_progress'))::BIGINT AS active_plan_count
    FROM membership
    GROUP BY account_id, scope_id
),
primary_plan AS (
    SELECT DISTINCT ON (account_id, scope_id)
           account_id, scope_id, plan_id, title, status, shot_count, readiness_unchecked_count,
           starts_at, ends_at, timezone, source, link_warning
    FROM membership
    ORDER BY account_id, scope_id, updated_at DESC, plan_id DESC
)
SELECT a.account_id,
       a.scope_id AS slot_id,
       a.plan_count,
       a.active_plan_count,
       p.plan_id AS primary_plan_id,
       p.title AS primary_title,
       p.status AS primary_status,
       p.shot_count AS primary_shot_count,
       p.readiness_unchecked_count AS primary_readiness_unchecked_count,
       p.starts_at AS window_starts_at,
       p.ends_at AS window_ends_at,
       p.timezone AS window_timezone,
       p.source AS window_source,
       p.link_warning
FROM agg a
JOIN primary_plan p ON p.account_id = a.account_id AND p.scope_id = a.scope_id;
