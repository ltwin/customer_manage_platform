CREATE TABLE auth_attempt_budgets (
    action       TEXT        NOT NULL,
    dimension    TEXT        NOT NULL,
    digest       TEXT        NOT NULL,
    window_start TIMESTAMPTZ NOT NULL,
    attempts     INTEGER     NOT NULL,
    PRIMARY KEY (action, dimension, digest),
    CHECK (action IN (
        'register',
        'resend_verification',
        'login',
        'forgot_password',
        'verify_token',
        'reset_token',
        'change_password'
    )),
    CHECK (dimension IN ('subject', 'source')),
    CHECK (digest ~ '^v1:[A-Za-z0-9_-]{43}$'),
    CHECK (attempts > 0)
);

CREATE INDEX auth_attempt_budgets_window_idx
    ON auth_attempt_budgets (window_start);

ALTER TABLE auth_action_tokens
    DROP CONSTRAINT auth_action_tokens_purpose_check,
    ADD CONSTRAINT auth_action_tokens_purpose_check
    CHECK (purpose IN ('email_verification', 'legacy_claim', 'password_reset'));
