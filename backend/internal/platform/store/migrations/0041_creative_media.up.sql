-- FND-05: image/video/audio content, upload sessions, blobs and read retention.
-- Explicit account keys only; no foreign keys. Cross-table ownership is guarded by services.
ALTER TABLE creative_contents DROP CONSTRAINT creative_contents_kind_check;
ALTER TABLE creative_contents ADD CONSTRAINT creative_contents_kind_check CHECK (kind IN ('text','link','image','video','audio'));
ALTER TABLE creative_assets DROP CONSTRAINT creative_assets_kind_check;
ALTER TABLE creative_assets ADD CONSTRAINT creative_assets_kind_check CHECK (kind IN ('text','link','image','video','audio'));
CREATE TABLE creative_blobs (
 id TEXT PRIMARY KEY, account_id TEXT NOT NULL,
 storage_driver TEXT NOT NULL CHECK(storage_driver IN ('local','oss')),
 bucket TEXT NOT NULL DEFAULT '', object_key TEXT NOT NULL, object_version TEXT,
 sha256 TEXT NOT NULL CHECK(sha256 ~ '^sha256-[a-f0-9]{64}$'),
 byte_size BIGINT NOT NULL CHECK(byte_size>0), mime TEXT NOT NULL CHECK(char_length(mime) BETWEEN 3 AND 120),
 width INTEGER CHECK(width>0), height INTEGER CHECK(height>0), duration_ms BIGINT CHECK(duration_ms>=0),
 codec_metadata JSONB NOT NULL DEFAULT '{}' CHECK(jsonb_typeof(codec_metadata)='object'),
 state TEXT NOT NULL DEFAULT 'verifying' CHECK(state IN ('verifying','ready','deleting','deleted')),
 verified_at TIMESTAMPTZ, delete_after TIMESTAMPTZ,
 quota_booked_at TIMESTAMPTZ, quota_released_at TIMESTAMPTZ,
 created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 UNIQUE(account_id,id), UNIQUE(storage_driver,bucket,object_key),
 CHECK((width IS NULL)=(height IS NULL)),
 CHECK(state<>'ready' OR (verified_at IS NOT NULL AND object_version IS NOT NULL)),
 CHECK(quota_released_at IS NULL OR (quota_booked_at IS NOT NULL AND state='deleted'))
);
CREATE INDEX creative_blob_gc_due ON creative_blobs(delete_after,account_id,id) WHERE state='deleting';
CREATE TABLE creative_content_objects (
 account_id TEXT NOT NULL, content_revision_id TEXT NOT NULL, blob_id TEXT NOT NULL,
 role TEXT NOT NULL CHECK(role IN ('original','display','thumbnail','attachment')),
 position INTEGER NOT NULL CHECK(position>=0),
 PRIMARY KEY(account_id,content_revision_id,role,position)
);
CREATE INDEX creative_content_object_blob ON creative_content_objects(account_id,blob_id);
CREATE TABLE creative_uploads (
 id TEXT PRIMARY KEY, account_id TEXT NOT NULL, operation_id UUID NOT NULL,
 rights_declaration_id TEXT NOT NULL, publish_operation_id UUID NOT NULL,
 state TEXT NOT NULL DEFAULT 'created' CHECK(state IN ('created','uploading','verifying','ready','failed','expired','cancelled')),
 io_phase TEXT NOT NULL DEFAULT 'none' CHECK(io_phase IN ('none','initializing','completing','promoting')),
 execution_epoch BIGINT NOT NULL DEFAULT 0 CHECK(execution_epoch>=0), lease_until TIMESTAMPTZ,
 revision BIGINT NOT NULL DEFAULT 1 CHECK(revision>0),
 declared_kind TEXT NOT NULL CHECK(declared_kind IN ('image','video','audio')),
 declared_mime TEXT NOT NULL CHECK(char_length(declared_mime) BETWEEN 3 AND 120),
 declared_size BIGINT NOT NULL CHECK(declared_size>0),
 reserved_bytes BIGINT NOT NULL CHECK(reserved_bytes>=0), quota_settled_at TIMESTAMPTZ,
 original_name TEXT NOT NULL CHECK(char_length(original_name) BETWEEN 1 AND 255),
 staging_key TEXT NOT NULL, staging_version TEXT, multipart_id TEXT,
 expires_at TIMESTAMPTZ NOT NULL,
 target_kind TEXT NOT NULL CHECK(target_kind IN ('asset','node')),
 target_snapshot JSONB NOT NULL CHECK(jsonb_typeof(target_snapshot)='object'),
 blob_id TEXT, handed_off_at TIMESTAMPTZ, published_blob_id_snapshot TEXT,
 publication_result_kind TEXT CHECK(publication_result_kind IN ('asset','node','candidate')),
 publication_result_id TEXT, publication_result_revision BIGINT CHECK(publication_result_revision>0),
 error_code TEXT NOT NULL DEFAULT '', created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 UNIQUE(account_id,id), UNIQUE(account_id,operation_id), UNIQUE(account_id,publish_operation_id), UNIQUE(staging_key),
 CONSTRAINT creative_upload_ready_handoff CHECK(state<>'ready' OR (blob_id IS NULL AND handed_off_at IS NOT NULL AND published_blob_id_snapshot IS NOT NULL AND publication_result_kind IS NOT NULL AND publication_result_id IS NOT NULL)),
 CHECK((publication_result_kind IS NULL)=(publication_result_id IS NULL)),
 CHECK((io_phase='initializing' AND state='created') OR (io_phase='completing' AND state='uploading') OR (io_phase='promoting' AND state='verifying') OR io_phase='none' OR state IN ('failed','expired','cancelled'))
);
CREATE INDEX creative_upload_rights ON creative_uploads(account_id,rights_declaration_id);
CREATE INDEX creative_upload_due ON creative_uploads(expires_at,account_id,id) WHERE state NOT IN ('ready','failed','expired','cancelled');
CREATE INDEX creative_upload_blob_root ON creative_uploads(account_id,blob_id) WHERE blob_id IS NOT NULL;
CREATE INDEX creative_upload_lease ON creative_uploads(lease_until,account_id,id) WHERE state IN ('created','uploading','verifying');
CREATE TABLE creative_upload_parts (
 account_id TEXT NOT NULL, upload_id TEXT NOT NULL,
 part_number INTEGER NOT NULL CHECK(part_number BETWEEN 1 AND 10000),
 etag TEXT NOT NULL, byte_size BIGINT NOT NULL CHECK(byte_size>0),
 PRIMARY KEY(account_id,upload_id,part_number)
);
CREATE TABLE creative_upload_candidates (
 id TEXT PRIMARY KEY, account_id TEXT NOT NULL, upload_id TEXT NOT NULL,
 content_id TEXT, content_revision_id TEXT,
 state TEXT NOT NULL DEFAULT 'pending' CHECK(state IN ('pending','applied','discarded','expired')),
 revision BIGINT NOT NULL DEFAULT 1 CHECK(revision>0),
 reason TEXT NOT NULL DEFAULT '',
 target_snapshot JSONB NOT NULL CHECK(jsonb_typeof(target_snapshot)='object'),
 original_name TEXT NOT NULL DEFAULT '', declared_kind TEXT NOT NULL DEFAULT 'image' CHECK(declared_kind IN ('image','video','audio')),
 expires_at TIMESTAMPTZ NOT NULL, adopted_change_id TEXT,
 adopted_target_kind TEXT CHECK(adopted_target_kind IN ('asset','node')), adopted_target_id TEXT,
 adopted_target_revision BIGINT CHECK(adopted_target_revision>0),
 created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 UNIQUE(account_id,id), UNIQUE(account_id,upload_id),
 CHECK((content_id IS NULL)=(content_revision_id IS NULL)),
 CHECK((state='pending' AND content_id IS NOT NULL) OR (state<>'pending' AND content_id IS NULL)),
 CHECK(state<>'applied' OR (adopted_target_kind IS NOT NULL AND adopted_target_id IS NOT NULL)),
 CHECK((adopted_target_kind IS NULL)=(adopted_target_id IS NULL))
);
CREATE INDEX creative_candidate_due ON creative_upload_candidates(expires_at,account_id,id) WHERE state='pending';
CREATE INDEX creative_candidate_revision_root ON creative_upload_candidates(account_id,content_revision_id) WHERE state='pending';
CREATE TABLE creative_blob_read_pins (
 id TEXT PRIMARY KEY, account_id TEXT NOT NULL, blob_id TEXT NOT NULL,
 owner_kind TEXT NOT NULL CHECK(owner_kind IN ('download','display','processing')),
 expires_at TIMESTAMPTZ NOT NULL, created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 UNIQUE(account_id,id)
);
CREATE INDEX creative_read_pin_root ON creative_blob_read_pins(account_id,blob_id,expires_at);
CREATE TABLE creative_revision_holds (
 id TEXT PRIMARY KEY, account_id TEXT NOT NULL, content_revision_id TEXT NOT NULL,
 reason TEXT NOT NULL CHECK(reason IN ('migration','backup','recovery')),
 expires_at TIMESTAMPTZ, created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 UNIQUE(account_id,id)
);
CREATE INDEX creative_hold_root ON creative_revision_holds(account_id,content_revision_id);
CREATE TABLE creative_media_quotas (
 account_id TEXT PRIMARY KEY,
 reserved_bytes BIGINT NOT NULL DEFAULT 0 CHECK(reserved_bytes>=0),
 stored_bytes BIGINT NOT NULL DEFAULT 0 CHECK(stored_bytes>=0),
 limit_bytes BIGINT NOT NULL CHECK(limit_bytes>0),
 revision BIGINT NOT NULL DEFAULT 1 CHECK(revision>0),
 CHECK(reserved_bytes+stored_bytes<=limit_bytes)
);
