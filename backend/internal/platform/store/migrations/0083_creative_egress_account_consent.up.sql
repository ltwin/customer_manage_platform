-- Account-level egress consent. A consent row whose conversation_id is NULL is
-- the photographer's answer to the pre-send notice for the whole account; the
-- per-conversation rows and their revision lists become read-only history.
-- Explicit account keys only; no foreign keys, as in 0044.

-- A consent row never changes content once written. Turning library access on
-- or off writes a new row and stamps the old one superseded.
ALTER TABLE creative_egress_consents
 ADD COLUMN superseded_at TIMESTAMPTZ;

-- At most one unstamped account-level row per account.
CREATE UNIQUE INDEX creative_egress_account_consent_current
 ON creative_egress_consents(account_id)
 WHERE conversation_id IS NULL AND superseded_at IS NULL AND revoked_at IS NULL;

-- One row per account naming the consent in force. Granting and revoking lock
-- it FOR UPDATE; the gate reads it FOR SHARE, so dispatches across many
-- conversations of one account do not queue behind each other.
CREATE TABLE creative_egress_consent_states (
 account_id TEXT PRIMARY KEY,
 -- NULL means no consent: never granted, or revoked.
 current_consent_id TEXT CHECK(current_consent_id IS NULL OR current_consent_id ~ '^ccec_[0-9a-f-]{36}$'),
 -- Tells "revoked" apart from "never granted" once current_consent_id is NULL.
 last_revoked_at TIMESTAMPTZ,
 revision BIGINT NOT NULL DEFAULT 1 CHECK(revision>0),
 created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp()
);
