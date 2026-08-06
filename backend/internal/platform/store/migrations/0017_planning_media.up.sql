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
        'planning-media.lease.release.v1'
    ));

CREATE TABLE planning_media_assets (
    id TEXT PRIMARY KEY,
    account_id TEXT NOT NULL REFERENCES accounts(id),
    upload_context_plan_id TEXT NOT NULL,
    display_name TEXT NOT NULL DEFAULT '' CHECK (char_length(display_name) <= 160),
    state TEXT NOT NULL CHECK (state IN ('staged','active','gc_pending','deleted','corrupt')),
    current_generation BIGINT NOT NULL DEFAULT 1 CHECK (current_generation >= 1),
    revision BIGINT NOT NULL DEFAULT 1 CHECK (revision >= 1),
    gc_eligible_at TIMESTAMPTZ,
    gc_rule_version INTEGER NOT NULL DEFAULT 1 CHECK (gc_rule_version = 1),
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    deleted_at TIMESTAMPTZ,
    UNIQUE(account_id, id),
    FOREIGN KEY(account_id, upload_context_plan_id) REFERENCES shoot_plans(account_id, id)
);
CREATE INDEX planning_media_assets_plan_idx ON planning_media_assets(account_id, upload_context_plan_id, updated_at DESC, id DESC);

CREATE TABLE planning_media_rights_declarations (
    id TEXT PRIMARY KEY,
    account_id TEXT NOT NULL,
    asset_id TEXT NOT NULL,
    generation BIGINT NOT NULL CHECK (generation >= 1),
    source_class TEXT NOT NULL,
    rights_basis TEXT NOT NULL,
    evidence_summary TEXT CHECK (char_length(evidence_summary) <= 500),
    license_generation_reference_granted BOOLEAN NOT NULL DEFAULT FALSE,
    matrix_version INTEGER NOT NULL CHECK (matrix_version = 1),
    declared_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    UNIQUE(account_id, id), UNIQUE(account_id, asset_id, generation),
    UNIQUE(account_id, asset_id, generation, id),
    FOREIGN KEY(account_id, asset_id) REFERENCES planning_media_assets(account_id, id),
    CHECK (
        (source_class IN ('official','anime_screenshot','setting_book','fan','unknown_web') AND rights_basis='citation_or_display') OR
        (source_class='photographer_owned' AND rights_basis='ownership_attested') OR
        (source_class='licensed' AND rights_basis='license_recorded') OR
        (source_class='customer_supplied' AND rights_basis='display_consent')
    ),
    CHECK (source_class='licensed' OR license_generation_reference_granted=FALSE)
);

CREATE TABLE planning_media_generations (
    account_id TEXT NOT NULL,
    asset_id TEXT NOT NULL,
    generation BIGINT NOT NULL CHECK (generation >= 1),
    rights_declaration_id TEXT NOT NULL,
    original_checksum TEXT NOT NULL,
    display_checksum TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY(account_id, asset_id, generation),
    FOREIGN KEY(account_id, asset_id) REFERENCES planning_media_assets(account_id, id),
    FOREIGN KEY(account_id, asset_id, generation, rights_declaration_id)
        REFERENCES planning_media_rights_declarations(account_id, asset_id, generation, id)
);

ALTER TABLE planning_media_assets
    ADD CONSTRAINT planning_media_assets_current_generation_fk
    FOREIGN KEY(account_id, id, current_generation)
    REFERENCES planning_media_generations(account_id, asset_id, generation)
    DEFERRABLE INITIALLY DEFERRED;

CREATE TABLE planning_media_renditions (
    account_id TEXT NOT NULL,
    asset_id TEXT NOT NULL,
    generation BIGINT NOT NULL,
    kind TEXT NOT NULL CHECK (kind IN ('original','display')),
    mime_type TEXT NOT NULL,
    byte_size BIGINT NOT NULL CHECK (byte_size > 0),
    width INTEGER NOT NULL CHECK (width > 0),
    height INTEGER NOT NULL CHECK (height > 0),
    checksum TEXT NOT NULL,
    internal_object_key TEXT NOT NULL,
    PRIMARY KEY(account_id, asset_id, generation, kind),
    UNIQUE(account_id, internal_object_key),
    FOREIGN KEY(account_id, asset_id, generation) REFERENCES planning_media_generations(account_id, asset_id, generation)
);

CREATE TABLE planning_media_bindings (
    id TEXT PRIMARY KEY,
    account_id TEXT NOT NULL,
    asset_id TEXT NOT NULL,
    generation BIGINT NOT NULL,
    holder_kind TEXT NOT NULL CHECK (holder_kind IN ('plan','shot')),
    holder_id TEXT NOT NULL,
    plan_id TEXT NOT NULL,
    purpose TEXT NOT NULL CHECK (purpose IN ('moodboard_display','shot_reference_display','generation_reference')),
    state TEXT NOT NULL CHECK (state IN ('active','released')),
    revision BIGINT NOT NULL DEFAULT 1 CHECK (revision >= 1),
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    released_at TIMESTAMPTZ,
    UNIQUE(account_id, id),
    FOREIGN KEY(account_id, asset_id, generation) REFERENCES planning_media_generations(account_id, asset_id, generation),
    FOREIGN KEY(account_id, plan_id) REFERENCES shoot_plans(account_id, id)
);
CREATE UNIQUE INDEX planning_media_bindings_active_idx ON planning_media_bindings(account_id, asset_id, generation, holder_kind, holder_id, purpose) WHERE state='active';

CREATE VIEW planning_media_asset_gallery AS
SELECT
    a.account_id,
    a.id,
    a.upload_context_plan_id,
    a.display_name,
    a.state,
    a.current_generation,
    a.revision,
    a.gc_eligible_at,
    a.gc_rule_version,
    a.created_at,
    a.updated_at,
    a.deleted_at,
    r.checksum AS display_checksum
FROM planning_media_assets a
JOIN planning_media_renditions r
  ON r.account_id = a.account_id
 AND r.asset_id = a.id
 AND r.generation = a.current_generation
 AND r.kind = 'display';

CREATE VIEW planning_media_shot_access_refs AS
SELECT b.account_id, b.plan_id, b.holder_id AS shot_id, b.created_at, b.id AS binding_id,
       a.id AS asset_id, a.display_name, b.generation, r.checksum AS display_checksum
FROM planning_media_bindings b
JOIN planning_media_assets a
  ON a.account_id = b.account_id AND a.id = b.asset_id
JOIN planning_media_renditions r
  ON r.account_id = b.account_id AND r.asset_id = b.asset_id
 AND r.generation = b.generation AND r.kind = 'display'
WHERE b.state = 'active' AND b.holder_kind = 'shot' AND b.purpose = 'shot_reference_display';

CREATE TABLE planning_media_leases (
    id TEXT PRIMARY KEY,
    account_id TEXT NOT NULL,
    asset_id TEXT NOT NULL,
    generation BIGINT NOT NULL,
    binding_id TEXT NOT NULL,
    owner_kind TEXT NOT NULL,
    owner_id TEXT NOT NULL,
    purpose TEXT NOT NULL,
    state TEXT NOT NULL CHECK (state IN ('active','released','expired')),
    expires_at TIMESTAMPTZ NOT NULL,
    revision BIGINT NOT NULL DEFAULT 1 CHECK (revision >= 1),
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    released_at TIMESTAMPTZ,
    UNIQUE(account_id, id),
    FOREIGN KEY(account_id, binding_id) REFERENCES planning_media_bindings(account_id, id),
    FOREIGN KEY(account_id, asset_id, generation) REFERENCES planning_media_generations(account_id, asset_id, generation)
);

CREATE TABLE planning_media_read_pins (
    id TEXT PRIMARY KEY,
    account_id TEXT NOT NULL,
    asset_id TEXT NOT NULL,
    generation BIGINT NOT NULL,
    authorization_anchor TEXT NOT NULL CHECK (authorization_anchor IN ('binding','upload_context')),
    binding_id TEXT,
    upload_context_plan_id TEXT,
    state TEXT NOT NULL CHECK (state IN ('active','released','expired')),
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    released_at TIMESTAMPTZ,
    UNIQUE(account_id, id),
    FOREIGN KEY(account_id, asset_id, generation) REFERENCES planning_media_generations(account_id, asset_id, generation),
    FOREIGN KEY(account_id, binding_id) REFERENCES planning_media_bindings(account_id, id),
    FOREIGN KEY(account_id, upload_context_plan_id) REFERENCES shoot_plans(account_id, id),
    CHECK ((authorization_anchor='binding' AND binding_id IS NOT NULL AND upload_context_plan_id IS NULL) OR
           (authorization_anchor='upload_context' AND binding_id IS NULL AND upload_context_plan_id IS NOT NULL))
);

CREATE TABLE planning_media_object_inventory (
    internal_object_key TEXT PRIMARY KEY,
    account_id TEXT NOT NULL,
    asset_id TEXT NOT NULL,
    generation BIGINT NOT NULL,
    rendition TEXT NOT NULL CHECK (rendition IN ('original','display')),
    checksum TEXT NOT NULL,
    byte_size BIGINT NOT NULL CHECK (byte_size > 0),
    inventory_version INTEGER NOT NULL DEFAULT 1 CHECK (inventory_version = 1),
    first_seen_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    UNIQUE(account_id, internal_object_key)
);
