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
        'shoot-plan.crm-link.v1',
        'plan-share.issue.v1',
        'plan-share.rotate.v1',
        'plan-share.revoke.v1',
        'plan-share.feedback-disposition.v1',
        'plan-share.feedback-plan-create.v1',
        'plan-share.feedback-shot-create.v1'
    ));

CREATE TABLE share_feedbacks (
    id                   TEXT PRIMARY KEY,
    account_id           TEXT NOT NULL REFERENCES accounts (id),
    plan_id              TEXT NOT NULL,
    token_generation_id  TEXT NOT NULL,
    target_kind          TEXT NOT NULL CHECK (target_kind IN ('plan', 'shot')),
    target_id            TEXT,
    target_revision      BIGINT NOT NULL CHECK (target_revision >= 1),
    author_display_name  TEXT NOT NULL CHECK (char_length(author_display_name) BETWEEN 1 AND 40),
    content              TEXT NOT NULL CHECK (char_length(content) BETWEEN 1 AND 2000),
    disposition          TEXT NOT NULL CHECK (disposition IN ('pending', 'adopted', 'ignored')),
    revision             BIGINT NOT NULL DEFAULT 1 CHECK (revision >= 1),
    deep_link_kind       TEXT NOT NULL CHECK (deep_link_kind IN ('feedback_section', 'shot')),
    deep_link_shot_id    TEXT,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    disposition_at       TIMESTAMPTZ,
    UNIQUE (account_id, id),
    FOREIGN KEY (account_id, plan_id) REFERENCES shoot_plans (account_id, id),
    FOREIGN KEY (account_id, token_generation_id) REFERENCES share_generations (account_id, id),
    CHECK (
        (target_kind = 'plan'
            AND target_id IS NULL
            AND deep_link_kind = 'feedback_section'
            AND deep_link_shot_id IS NULL)
        OR (target_kind = 'shot'
            AND target_id IS NOT NULL
            AND deep_link_kind = 'shot'
            AND deep_link_shot_id = target_id)
    ),
    CHECK (
        (disposition = 'pending' AND disposition_at IS NULL)
        OR (disposition IN ('adopted', 'ignored') AND disposition_at IS NOT NULL)
    )
);

CREATE INDEX share_feedbacks_plan_created_idx
    ON share_feedbacks (account_id, plan_id, created_at DESC, id DESC);

CREATE TABLE share_replay_admissions (
    account_id               TEXT NOT NULL REFERENCES accounts (id),
    token_generation_id      TEXT NOT NULL,
    operation                TEXT NOT NULL,
    idempotency_key_digest   BYTEA NOT NULL CHECK (octet_length(idempotency_key_digest) = 32),
    exact_frame_fingerprint  BYTEA NOT NULL CHECK (octet_length(exact_frame_fingerprint) = 32),
    admitted_at              TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    expires_at               TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (account_id, token_generation_id, operation, idempotency_key_digest),
    UNIQUE (token_generation_id, operation, idempotency_key_digest),
    FOREIGN KEY (account_id, token_generation_id) REFERENCES share_generations (account_id, id),
    CHECK (expires_at > admitted_at)
);

CREATE INDEX share_replay_admissions_expires_idx
    ON share_replay_admissions (expires_at);
