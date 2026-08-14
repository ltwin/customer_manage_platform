-- Plan assignment reminder projection (ITEM-6 S1): source / group / member.
-- Fence / inbox / quarantine / epoch / digest intent remain for later steps.

ALTER TABLE reminders
    DROP CONSTRAINT reminders_type_check;
ALTER TABLE reminders
    ADD CONSTRAINT reminders_type_check CHECK (type IN (
        'birthday',
        'follow_up',
        'churn',
        'custom',
        'plan_assignment_checklist'
    ));

ALTER TABLE reminders
    ADD COLUMN plan_id TEXT;

ALTER TABLE reminders
    ADD CONSTRAINT reminders_plan_fk
    FOREIGN KEY (account_id, plan_id) REFERENCES shoot_plans (account_id, id);

ALTER TABLE reminders
    ADD CONSTRAINT reminders_plan_assignment_checklist_refs_check CHECK (
        (type = 'plan_assignment_checklist'
            AND plan_id IS NOT NULL
            AND order_id IS NOT NULL)
        OR (type <> 'plan_assignment_checklist'
            AND plan_id IS NULL)
    );

CREATE TABLE plan_assignment_reminder_sources (
    account_id                      TEXT NOT NULL REFERENCES accounts (id),
    plan_id                         TEXT NOT NULL,
    assignment_id                   TEXT NOT NULL,
    assignment_revision             BIGINT NOT NULL CHECK (assignment_revision >= 1),
    source_event_id                 TEXT,
    assignment_kind                 TEXT NOT NULL CHECK (assignment_kind IN ('readiness', 'on_site_support')),
    readiness_item_id               TEXT,
    content_snapshot                TEXT NOT NULL CHECK (char_length(content_snapshot) BETWEEN 1 AND 2000),
    claimed_by_display_name_snapshot TEXT CHECK (
                                        claimed_by_display_name_snapshot IS NULL
                                        OR char_length(claimed_by_display_name_snapshot) BETWEEN 1 AND 40
                                    ),
    content_fingerprint             TEXT NOT NULL CHECK (char_length(content_fingerprint) BETWEEN 1 AND 128),
    preparation_lead_days_snapshot  INTEGER CHECK (
                                        preparation_lead_days_snapshot IS NULL
                                        OR (preparation_lead_days_snapshot >= 0 AND preparation_lead_days_snapshot <= 365)
                                    ),
    lead_rule_version               TEXT CHECK (
                                        lead_rule_version IS NULL
                                        OR char_length(lead_rule_version) BETWEEN 1 AND 64
                                    ),
    source_state                    TEXT NOT NULL CHECK (source_state IN ('active', 'revoked')),
    projection_state                TEXT NOT NULL CHECK (projection_state IN (
                                        'grouped',
                                        'unscheduled',
                                        'ineligible_on_site',
                                        'revoked'
                                    )),
    source_occurred_at              TIMESTAMPTZ NOT NULL,
    updated_at                      TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (account_id, plan_id, assignment_id),
    UNIQUE (account_id, plan_id, assignment_id),
    FOREIGN KEY (account_id, plan_id) REFERENCES shoot_plans (account_id, id),
    FOREIGN KEY (account_id, assignment_id) REFERENCES share_assignments (account_id, id),
    CHECK (
        (assignment_kind = 'readiness'
            AND readiness_item_id IS NOT NULL
            AND preparation_lead_days_snapshot IS NOT NULL
            AND lead_rule_version IS NOT NULL)
        OR (assignment_kind = 'on_site_support'
            AND readiness_item_id IS NULL
            AND preparation_lead_days_snapshot IS NULL
            AND lead_rule_version IS NULL)
    ),
    CHECK (
        (source_state = 'revoked' AND projection_state = 'revoked')
        OR (source_state = 'active'
            AND assignment_kind = 'on_site_support'
            AND projection_state = 'ineligible_on_site')
        OR (source_state = 'active'
            AND assignment_kind = 'readiness'
            AND projection_state IN ('grouped', 'unscheduled'))
    )
);

CREATE INDEX plan_assignment_reminder_sources_plan_state_idx
    ON plan_assignment_reminder_sources (account_id, plan_id, projection_state, assignment_id);

CREATE TABLE plan_assignment_reminder_groups (
    group_id                TEXT PRIMARY KEY,
    account_id              TEXT NOT NULL REFERENCES accounts (id),
    plan_id                 TEXT NOT NULL,
    order_id                TEXT NOT NULL,
    slot_id                 TEXT NOT NULL,
    due_date                DATE NOT NULL,
    timezone_snapshot       TEXT NOT NULL CHECK (char_length(timezone_snapshot) BETWEEN 1 AND 64),
    valid_until             TIMESTAMPTZ,
    lead_rule_version       TEXT NOT NULL CHECK (char_length(lead_rule_version) BETWEEN 1 AND 64),
    activation_generation   BIGINT NOT NULL CHECK (activation_generation >= 1),
    validity_revision       BIGINT NOT NULL DEFAULT 1 CHECK (validity_revision >= 1),
    group_fingerprint       TEXT NOT NULL CHECK (char_length(group_fingerprint) = 64),
    state                   TEXT NOT NULL CHECK (state IN ('current', 'withdrawn')),
    withdrawn_reason        TEXT CHECK (
                                withdrawn_reason IS NULL
                                OR withdrawn_reason IN (
                                    'source_changed',
                                    'slot_unavailable',
                                    'order_inactive',
                                    'plan_archived',
                                    'assignment_revoked',
                                    'shoot_started'
                                )
                            ),
    reminder_id             TEXT NOT NULL,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    withdrawn_at            TIMESTAMPTZ,
    UNIQUE (account_id, group_id),
    UNIQUE (account_id, reminder_id),
    FOREIGN KEY (account_id, plan_id) REFERENCES shoot_plans (account_id, id),
    FOREIGN KEY (account_id, order_id) REFERENCES orders (account_id, id),
    FOREIGN KEY (account_id, slot_id) REFERENCES schedule_slots (account_id, id),
    FOREIGN KEY (account_id, reminder_id) REFERENCES reminders (account_id, id),
    CHECK (
        (state = 'current'
            AND valid_until IS NOT NULL
            AND withdrawn_at IS NULL
            AND withdrawn_reason IS NULL)
        OR (state = 'withdrawn'
            AND withdrawn_at IS NOT NULL
            AND withdrawn_reason IS NOT NULL)
    )
);

CREATE UNIQUE INDEX plan_assignment_reminder_groups_current_bucket_uniq
    ON plan_assignment_reminder_groups (account_id, plan_id, slot_id, due_date, lead_rule_version)
    WHERE state = 'current';

CREATE INDEX plan_assignment_reminder_groups_plan_state_idx
    ON plan_assignment_reminder_groups (account_id, plan_id, state, due_date, group_id);

CREATE TABLE plan_assignment_reminder_members (
    account_id              TEXT NOT NULL,
    group_id                TEXT NOT NULL,
    plan_id                 TEXT NOT NULL,
    assignment_id           TEXT NOT NULL,
    assignment_revision     BIGINT NOT NULL CHECK (assignment_revision >= 1),
    content_fingerprint     TEXT NOT NULL CHECK (char_length(content_fingerprint) BETWEEN 1 AND 128),
    position                INTEGER NOT NULL CHECK (position >= 0),
    activation_generation   BIGINT NOT NULL CHECK (activation_generation >= 1),
    PRIMARY KEY (account_id, group_id, assignment_id),
    UNIQUE (account_id, group_id, position),
    FOREIGN KEY (account_id, group_id)
        REFERENCES plan_assignment_reminder_groups (account_id, group_id),
    FOREIGN KEY (account_id, plan_id, assignment_id)
        REFERENCES plan_assignment_reminder_sources (account_id, plan_id, assignment_id)
);

CREATE INDEX plan_assignment_reminder_members_plan_assignment_idx
    ON plan_assignment_reminder_members (account_id, plan_id, assignment_id);
