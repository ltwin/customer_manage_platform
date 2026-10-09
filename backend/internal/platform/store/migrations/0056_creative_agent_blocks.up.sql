-- What the photographer sees of a run: an append-only ordered list of blocks.
-- Each change to a block commits together with one creative_run_events row, so
-- a snapshot plus the events after its seq always rebuild the same list.
-- type/state are validated by the Go registry so a new block kind needs no
-- migration; payload_version is per kind, independent of the event envelope.
CREATE TABLE creative_agent_blocks (
 id TEXT PRIMARY KEY CHECK(id ~ '^ccbk_[0-9a-f-]{36}$'), account_id TEXT NOT NULL,
 run_id TEXT NOT NULL,
 ordinal INT NOT NULL CHECK(ordinal>0),
 type TEXT NOT NULL CHECK(type ~ '^[a-z][a-z_]{0,31}$'),
 state TEXT NOT NULL CHECK(state ~ '^[a-z][a-z_]{0,31}$'),
 revision BIGINT NOT NULL DEFAULT 1 CHECK(revision>0),
 payload_version INT NOT NULL DEFAULT 1 CHECK(payload_version>0),
 -- Idempotency within the run: a worker redoing a step upserts the same block.
 source_key TEXT NOT NULL CHECK(char_length(source_key) BETWEEN 1 AND 160),
 hidden BOOLEAN NOT NULL DEFAULT false,
 payload JSONB NOT NULL CHECK(jsonb_typeof(payload)='object' AND octet_length(payload::text)<=262144),
 started_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 finished_at TIMESTAMPTZ,
 created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 UNIQUE(account_id,id),
 UNIQUE(account_id,run_id,ordinal),
 UNIQUE(account_id,run_id,source_key)
);
ALTER TABLE creative_agent_runs
 ADD COLUMN next_block_ordinal INT NOT NULL DEFAULT 1 CHECK(next_block_ordinal>0),
 -- The single point a run waits on; respond must name this block.
 ADD COLUMN waiting_block_id TEXT;

-- One wake-up channel for every realtime stream. The payload names a topic
-- only, never content; readers fetch from the database after waking.
CREATE FUNCTION notify_creative_run_event() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 PERFORM pg_notify('creative_realtime', json_build_object('Kind','agent_run','Account',NEW.account_id,'ID',NEW.run_id)::text);
 RETURN NULL;
END;
$$;
CREATE TRIGGER creative_run_events_notify AFTER INSERT ON creative_run_events
 FOR EACH ROW EXECUTE FUNCTION notify_creative_run_event();

-- Canvas invalidation moves onto the shared channel. A null ID still means
-- every canvas of the account.
CREATE OR REPLACE FUNCTION notify_creative_canvas_changed() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE row_data jsonb; owner_id text; target_id text;
BEGIN
 IF TG_OP = 'DELETE' THEN row_data := to_jsonb(OLD); ELSE row_data := to_jsonb(NEW); END IF;
 IF TG_OP = 'UPDATE' AND OLD IS NOT DISTINCT FROM NEW THEN RETURN NULL; END IF;
 owner_id := row_data->>'account_id';
 IF TG_TABLE_NAME = 'accounts' THEN owner_id := row_data->>'id'; END IF;
 IF TG_ARGV[0] = 'canvas' THEN target_id := row_data->>'canvas_id'; END IF;
 IF TG_TABLE_NAME = 'creative_canvases' THEN target_id := row_data->>'id'; END IF;
 PERFORM pg_notify('creative_realtime', json_build_object('Kind','canvas','Account', owner_id, 'ID', target_id)::text);
 RETURN NULL;
END;
$$;
