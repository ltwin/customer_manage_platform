-- FND-07 milestone B: the run's own facts. 0044 owns what a photographer reads
-- afterwards; this owns what actually executed — one write slot per account, the
-- frozen inputs, the ordered steps and the events a panel follows.
-- Explicit account keys only; no foreign keys, exactly as 0044 and 0045.
CREATE TABLE creative_agent_runs (
 id TEXT PRIMARY KEY CHECK(id ~ '^ccrn_[0-9a-f-]{36}$'), account_id TEXT NOT NULL,
 conversation_id TEXT NOT NULL,
 -- Read from the conversation, never from the request: the canvas a run may
 -- touch is decided by where the conversation lives.
 canvas_id TEXT NOT NULL,
 -- One run per trigger message. A retried creation replays the original
 -- receipt instead of starting a second run against the same words.
 trigger_message_id TEXT NOT NULL,
 egress_consent_id TEXT NOT NULL,
 model_key TEXT NOT NULL,
 -- What the catalog said at creation. A run is judged by the deployment it was
 -- created under, so this is a snapshot and never re-read.
 model_snapshot JSONB NOT NULL,
 -- The one skill this run fixed, if any. All three columns move together: a
 -- version id without its digest is a reference nobody can verify again.
 skill_id TEXT, skill_version_id TEXT, skill_snapshot JSONB,
 state TEXT NOT NULL CHECK(state IN
  ('queued','running','waiting_input','waiting_apply','reconciling',
   'succeeded','partial','failed','cancelled')),
 -- Bumped on every takeover. An effect carrying an old epoch is refused, which
 -- is what stops a worker that lost its lease from writing after the fact.
 execution_epoch BIGINT NOT NULL DEFAULT 1 CHECK(execution_epoch>0),
 -- Occupancy identity, mirrored on the slot row. A release must match both, so
 -- an old run can never free a slot somebody else has since taken.
 claim_token TEXT NOT NULL,
 -- Only a run with a live worker holds a lease. Queued, waiting, reconciling
 -- and terminal runs have none — a slot may be held with nobody working.
 lease_until TIMESTAMPTZ,
 cancel_requested_at TIMESTAMPTZ,
 next_step_ordinal BIGINT NOT NULL DEFAULT 1 CHECK(next_step_ordinal>0),
 last_event_seq BIGINT NOT NULL DEFAULT 0 CHECK(last_event_seq>=0),
 -- The continuous prefix events have been cleaned up through. A cursor below
 -- it cannot be caught up by reading, only by taking a fresh snapshot.
 pruned_through_seq BIGINT NOT NULL DEFAULT 0 CHECK(pruned_through_seq>=0),
 limits_version TEXT NOT NULL CHECK(char_length(limits_version) BETWEEN 1 AND 64),
 limits_snapshot JSONB NOT NULL,
 initial_llm_reservation_id TEXT,
 -- Cost is its own question. An unknown bill never keeps a delivered run from
 -- reaching a terminal state, and a terminal state never closes accounting.
 settlement_state TEXT NOT NULL DEFAULT 'not_started'
  CHECK(settlement_state IN ('not_started','pending','settled','unknown')),
 error_code TEXT,
 revision BIGINT NOT NULL DEFAULT 1 CHECK(revision>0),
 -- The bounded execution window. Waiting and recovery never extend it.
 deadline_at TIMESTAMPTZ NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 started_at TIMESTAMPTZ,
 finished_at TIMESTAMPTZ,
 UNIQUE(account_id,id),
 UNIQUE(account_id,trigger_message_id),
 CHECK(deadline_at>created_at),
 CHECK((skill_id IS NULL)=(skill_version_id IS NULL)
   AND (skill_id IS NULL)=(skill_snapshot IS NULL)),
 -- Terminal and finished are the same fact stated twice; letting them disagree
 -- would leave a run that is over according to one column and live in another.
 CHECK((state IN ('succeeded','partial','failed','cancelled'))=(finished_at IS NOT NULL)),
 CHECK(lease_until IS NULL OR state='running')
);
CREATE INDEX creative_agent_run_conversation
 ON creative_agent_runs(account_id,conversation_id,created_at DESC,id DESC);

-- One write slot per account: the whole point is that there is no second row to
-- take. Occupied means all three columns; free means none, because a half-filled
-- slot is one nobody can release.
CREATE TABLE creative_agent_slots (
 account_id TEXT PRIMARY KEY,
 run_id TEXT, claim_token TEXT, acquired_at TIMESTAMPTZ,
 updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 CHECK((run_id IS NULL)=(claim_token IS NULL) AND (run_id IS NULL)=(acquired_at IS NULL))
);

-- Frozen inputs. These are historical facts and are never updated to whatever
-- the source says later; they are also retention roots, which is why a located
-- revision is a column and not something buried in a JSON payload.
CREATE TABLE creative_run_inputs (
 account_id TEXT NOT NULL, run_id TEXT NOT NULL,
 -- A batch is one submission. Supplements append a batch rather than editing
 -- the first one.
 batch_ordinal INT NOT NULL CHECK(batch_ordinal>=0),
 ordinal INT NOT NULL CHECK(ordinal>=0),
 -- history is what the conversation already said when this run was created; it
 -- is frozen here rather than re-read at dispatch, so a turn appended by
 -- another window belongs to the next run and not to this one.
 input_role TEXT NOT NULL CHECK(input_role IN ('history','instruction','selection','upstream','attachment','tool_result')),
 -- What the model is actually allowed to perceive of this item.
 read_level TEXT NOT NULL CHECK(read_level IN ('text','image_preview','metadata_only')),
 -- Either the photographer's own words or a located revision. A truncated
 -- summary alone is not an input: the run has to be able to send what it froze.
 body TEXT,
 content_revision_id TEXT,
 source_revision_snapshot JSONB,
 created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(account_id,run_id,batch_ordinal,ordinal),
 CHECK(body IS NOT NULL OR content_revision_id IS NOT NULL)
);
CREATE INDEX creative_run_input_revision
 ON creative_run_inputs(account_id,content_revision_id) WHERE content_revision_id IS NOT NULL;

-- Which frozen skill version a run was told to follow. FND-10 reads this table
-- and 0045's message table as the two roots that keep a version alive.
CREATE TABLE creative_run_skill_refs (
 account_id TEXT NOT NULL, run_id TEXT NOT NULL,
 segment_ordinal INT NOT NULL CHECK(segment_ordinal>=0),
 skill_id TEXT NOT NULL, skill_version_id TEXT NOT NULL,
 -- The publishing account, stored as a value: resolving this again must never
 -- need another account's scope.
 skill_owner_account_id TEXT NOT NULL,
 digest TEXT NOT NULL CHECK(digest ~ '^[0-9a-f]{64}$'),
 created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(account_id,run_id,segment_ordinal)
);
CREATE INDEX creative_run_skill_version
 ON creative_run_skill_refs(account_id,skill_version_id);

CREATE TABLE creative_agent_steps (
 id TEXT PRIMARY KEY CHECK(id ~ '^ccst_[0-9a-f-]{36}$'), account_id TEXT NOT NULL,
 run_id TEXT NOT NULL,
 ordinal BIGINT NOT NULL CHECK(ordinal>0),
 attempt INT NOT NULL DEFAULT 1 CHECK(attempt>0),
 retry_of_step_id TEXT,
 kind TEXT NOT NULL CHECK(kind IN ('model','tool','check')),
 -- Step state is independent of run state; the two are never filled from each
 -- other. 'unknown' is a real outcome and must not be written as a failure.
 state TEXT NOT NULL CHECK(state IN ('prepared','dispatched','succeeded','failed','unknown')),
 execution_epoch BIGINT NOT NULL CHECK(execution_epoch>0),
 tool_key TEXT, tool_version INT,
 llm_request_id TEXT,
 operation_id TEXT,
 -- A tool plan's identity inside one model answer. Unique below, so the same
 -- answer can never be expanded into a second set of tool calls.
 parent_model_step_id TEXT, tool_call_index INT,
 input_hash TEXT NOT NULL CHECK(char_length(input_hash) BETWEEN 1 AND 128),
 input JSONB NOT NULL,
 output JSONB,
 error_code TEXT,
 started_at TIMESTAMPTZ, finished_at TIMESTAMPTZ,
 created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 UNIQUE(account_id,id),
 UNIQUE(account_id,run_id,ordinal,attempt),
 CHECK((parent_model_step_id IS NULL)=(tool_call_index IS NULL)),
 CHECK(kind<>'model' OR parent_model_step_id IS NULL)
);
CREATE UNIQUE INDEX creative_step_tool_call
 ON creative_agent_steps(account_id,parent_model_step_id,tool_call_index)
 WHERE parent_model_step_id IS NOT NULL;
CREATE INDEX creative_step_run ON creative_agent_steps(account_id,run_id,ordinal,attempt);
-- A model step names at most one gateway request. Two steps claiming one
-- request would mean two business facts billed against a single call.
CREATE UNIQUE INDEX creative_step_llm_request
 ON creative_agent_steps(account_id,llm_request_id) WHERE llm_request_id IS NOT NULL;

-- Events are a short-lived projection for a reader that is following along.
-- They are never a receipt: the step and the message are.
CREATE TABLE creative_run_events (
 account_id TEXT NOT NULL, run_id TEXT NOT NULL,
 -- Handed out under the run's row lock, never MAX(seq)+1.
 seq BIGINT NOT NULL CHECK(seq>0),
 schema_version INT NOT NULL CHECK(schema_version>0),
 event_type TEXT NOT NULL CHECK(char_length(event_type) BETWEEN 1 AND 64),
 payload JSONB NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(account_id,run_id,seq)
);
CREATE INDEX creative_run_event_age ON creative_run_events(account_id,created_at);
