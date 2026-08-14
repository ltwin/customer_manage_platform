-- ITEM-6 S4: Settings binding revision, DigestIntent revisions, calling permits.
-- Delivery remains the sole send-status authority; Intent rows are immutable.

ALTER TABLE settings
    ADD COLUMN telegram_binding_revision BIGINT NOT NULL DEFAULT 1
        CHECK (telegram_binding_revision >= 1);

ALTER TABLE telegram_deliveries
    ADD COLUMN active_intent_revision BIGINT
        CHECK (active_intent_revision IS NULL OR active_intent_revision >= 1);

-- Account-scoped composite identity required by DigestIntent FK.
ALTER TABLE telegram_deliveries
    ADD CONSTRAINT telegram_deliveries_account_id_id_unique UNIQUE (account_id, id);

CREATE TABLE plan_assignment_reminder_digest_intents (
    account_id                      TEXT NOT NULL,
    delivery_id                     TEXT NOT NULL,
    intent_revision                 BIGINT NOT NULL CHECK (intent_revision >= 1),
    projection_generation           BIGINT NOT NULL CHECK (projection_generation >= 0),
    payload_fingerprint             TEXT NOT NULL,
    recipient_chat_id_snapshot      TEXT NOT NULL,
    recipient_binding_revision      BIGINT NOT NULL CHECK (recipient_binding_revision >= 1),
    recipient_fingerprint           TEXT NOT NULL,
    planning_membership_fingerprint TEXT NOT NULL,
    earliest_valid_until            TIMESTAMPTZ,
    payload_text                    TEXT,
    created_at                      TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    payload_redacted_at             TIMESTAMPTZ,
    PRIMARY KEY (account_id, delivery_id, intent_revision),
    FOREIGN KEY (account_id, delivery_id)
        REFERENCES telegram_deliveries (account_id, id) ON DELETE CASCADE,
    CHECK (
        (payload_text IS NULL AND payload_redacted_at IS NOT NULL)
        OR (payload_text IS NOT NULL AND payload_redacted_at IS NULL)
    ),
    CHECK (
        payload_text IS NULL
        OR (
            char_length(payload_text) <= 4096
            AND octet_length(payload_text) <= 16384
        )
    )
);

CREATE INDEX plan_assignment_reminder_digest_intents_delivery_idx
    ON plan_assignment_reminder_digest_intents (account_id, delivery_id, intent_revision DESC);

CREATE TABLE delivery_send_attempt_permits (
    account_id                 TEXT NOT NULL,
    delivery_id                TEXT NOT NULL,
    attempt_id                 TEXT NOT NULL,
    intent_revision            BIGINT NOT NULL CHECK (intent_revision >= 1),
    recipient_binding_revision BIGINT NOT NULL CHECK (recipient_binding_revision >= 1),
    db_authorized_at           TIMESTAMPTZ NOT NULL,
    start_deadline             TIMESTAMPTZ NOT NULL,
    outcome                    TEXT NOT NULL CHECK (
        outcome IN ('calling', 'definite_failure', 'response_unknown', 'finalized', 'superseded')
    ),
    created_at                 TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    finalized_at               TIMESTAMPTZ,
    PRIMARY KEY (account_id, delivery_id, attempt_id),
    FOREIGN KEY (account_id, delivery_id, intent_revision)
        REFERENCES plan_assignment_reminder_digest_intents (account_id, delivery_id, intent_revision)
        ON DELETE CASCADE,
    CHECK (
        (outcome = 'calling' AND finalized_at IS NULL)
        OR (outcome <> 'calling' AND finalized_at IS NOT NULL)
    ),
    CHECK (start_deadline > db_authorized_at)
);

-- At most one calling permit per delivery; expired calling rows are superseded
-- before a new insert in the same authorizing transaction.
CREATE UNIQUE INDEX delivery_send_attempt_permits_one_calling_idx
    ON delivery_send_attempt_permits (account_id, delivery_id)
    WHERE outcome = 'calling';

CREATE INDEX delivery_send_attempt_permits_deadline_idx
    ON delivery_send_attempt_permits (account_id, delivery_id, start_deadline)
    WHERE outcome = 'calling';
