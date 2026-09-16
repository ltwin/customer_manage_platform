-- A run is the record of what was sent to a vendor, what it produced and what it
-- may still cost. Dropping one loses evidence that exists nowhere else, so a
-- rollback is refused once any run exists; export first.
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM creative_agent_runs) THEN
  RAISE EXCEPTION 'recorded agent runs require export before rollback';
 END IF;
END $$;
DROP TABLE creative_run_events,creative_agent_steps,creative_run_skill_refs,
 creative_run_inputs,creative_agent_slots,creative_agent_runs;
