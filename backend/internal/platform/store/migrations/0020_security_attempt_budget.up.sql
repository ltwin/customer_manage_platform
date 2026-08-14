CREATE TABLE security_attempt_budgets_v1 (
    policy_version TEXT        NOT NULL,
    action         TEXT        NOT NULL,
    dimension      TEXT        NOT NULL,
    digest_version TEXT        NOT NULL,
    hmac_digest    BYTEA       NOT NULL,
    window_start   TIMESTAMPTZ NOT NULL,
    attempts       INTEGER     NOT NULL,
    PRIMARY KEY (policy_version, action, dimension, digest_version, hmac_digest),
    CHECK (policy_version = 'v1'),
    CHECK (action IN (
        'planshare_anonymous_read_v1',
        'planshare_anonymous_mutation_outer_v1',
        'planshare_anonymous_mutation_business_v1'
    )),
    CHECK (dimension IN (
        'ip',
        'token_ip',
        'token_generation',
        'token_generation_ip'
    )),
    CHECK (digest_version <> ''),
    CHECK (octet_length(hmac_digest) = 32),
    CHECK (attempts > 0)
);

CREATE INDEX security_attempt_budgets_v1_window_idx
    ON security_attempt_budgets_v1 (window_start);

CREATE TABLE security_attempt_budget_guards_v1 (
    policy_version TEXT        NOT NULL,
    action         TEXT        NOT NULL,
    dimension      TEXT        NOT NULL,
    digest_version TEXT        NOT NULL,
    hmac_digest    BYTEA       NOT NULL,
    last_used_at   TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (policy_version, action, dimension, digest_version, hmac_digest),
    CHECK (policy_version = 'v1'),
    CHECK (action IN (
        'planshare_anonymous_read_v1',
        'planshare_anonymous_mutation_outer_v1',
        'planshare_anonymous_mutation_business_v1'
    )),
    CHECK (dimension IN (
        'ip',
        'token_ip',
        'token_generation',
        'token_generation_ip'
    )),
    CHECK (digest_version <> ''),
    CHECK (octet_length(hmac_digest) = 32)
);

CREATE INDEX security_attempt_budget_guards_v1_last_used_idx
    ON security_attempt_budget_guards_v1 (last_used_at);
