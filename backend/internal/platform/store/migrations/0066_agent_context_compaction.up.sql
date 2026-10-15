-- Context compaction: a conversation's earlier turns become a handoff summary
-- the next runs carry instead of the turns themselves. Messages are never
-- rewritten; a checkpoint only changes what later runs freeze as history.
CREATE TABLE creative_agent_compactions (
 account_id text NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
 id text NOT NULL CHECK (id ~ '^cccp_[0-9a-f-]{36}$'),
 conversation_id text NOT NULL,
 -- The run whose compaction step produced this checkpoint; one at most.
 run_id text NOT NULL,
 -- The last message the summary covers. Always the end of a turn.
 covers_through_ordinal bigint NOT NULL CHECK (covers_through_ordinal > 0),
 -- The summary this one rolled forward from, if any.
 previous_compaction_id text,
 summary text NOT NULL CHECK (octet_length(summary) BETWEEN 1 AND 65536),
 trigger text NOT NULL CHECK (trigger IN ('auto','manual')),
 focus text CHECK (focus IS NULL OR char_length(focus) BETWEEN 1 AND 500),
 model_key text NOT NULL,
 tokens_before bigint NOT NULL CHECK (tokens_before >= 0),
 tokens_after bigint NOT NULL CHECK (tokens_after >= 0),
 -- The estimated parts of what the next run will carry, shown until a request
 -- has been measured again.
 composition jsonb CHECK (composition IS NULL OR
  (jsonb_typeof(composition) = 'object' AND octet_length(composition::text) <= 4096)),
 created_at timestamptz NOT NULL,
 -- A withdrawn authorisation retires every checkpoint of its conversation.
 invalidated_at timestamptz,
 invalidated_reason text CHECK (invalidated_reason IS NULL OR invalidated_reason IN ('consent_revoked')),
 PRIMARY KEY (account_id, id),
 UNIQUE (account_id, run_id),
 FOREIGN KEY (account_id, conversation_id) REFERENCES creative_agent_conversations(account_id, id) ON DELETE CASCADE,
 CHECK ((invalidated_at IS NULL) = (invalidated_reason IS NULL))
);
CREATE INDEX creative_agent_compactions_current
 ON creative_agent_compactions(account_id, conversation_id, covers_through_ordinal DESC)
 WHERE invalidated_at IS NULL;

-- A compact run only compacts. A chat run may compact first: its history is
-- then frozen by the worker after the summary exists, from the plan fixed here.
ALTER TABLE creative_agent_runs
 ADD COLUMN kind text NOT NULL DEFAULT 'chat' CHECK (kind IN ('chat','compact')),
 ADD COLUMN compaction_state text CHECK (compaction_state IN ('pending','completed','skipped','fallback')),
 ADD COLUMN compaction_plan jsonb CHECK (compaction_plan IS NULL OR jsonb_typeof(compaction_plan) = 'object'),
 ADD CONSTRAINT creative_agent_runs_compaction_pair_check CHECK ((compaction_state IS NULL) = (compaction_plan IS NULL));

ALTER TABLE creative_run_inputs DROP CONSTRAINT creative_run_inputs_input_role_check;
ALTER TABLE creative_run_inputs ADD CONSTRAINT creative_run_inputs_input_role_check
 CHECK (input_role IN ('history','instruction','selection','upstream','attachment','tool_result','compaction','skill_index'));

-- A compaction step is the one model call that is not part of the
-- conversation. The other columns record what a model request carried, so the
-- next estimate can be calibrated against what the provider actually counted.
ALTER TABLE creative_agent_steps DROP CONSTRAINT creative_agent_steps_kind_check;
ALTER TABLE creative_agent_steps ADD CONSTRAINT creative_agent_steps_kind_check
 CHECK (kind IN ('model','tool','check','compaction'));
ALTER TABLE creative_agent_steps
 ADD COLUMN input_text_bytes bigint CHECK (input_text_bytes IS NULL OR input_text_bytes >= 0),
 ADD COLUMN input_image_count integer CHECK (input_image_count IS NULL OR input_image_count >= 0),
 ADD COLUMN input_tokens bigint CHECK (input_tokens IS NULL OR input_tokens >= 0),
 ADD COLUMN context_composition jsonb CHECK (context_composition IS NULL OR
  (jsonb_typeof(context_composition) = 'object' AND octet_length(context_composition::text) <= 4096));
