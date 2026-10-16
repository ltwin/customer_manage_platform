DO $$ BEGIN
 IF EXISTS (SELECT 1 FROM llm_usage_reservations WHERE settlement_state='not_applicable') THEN
  RAISE EXCEPTION 'unbilled generation calls exist; retain compatible worker and schema';
 END IF;
END $$;
ALTER TABLE llm_usage_reservations DROP CONSTRAINT llm_reservation_no_billing_values;
ALTER TABLE llm_usage_reservations DROP CONSTRAINT llm_usage_reservations_settlement_state_check;
ALTER TABLE llm_usage_reservations ADD CONSTRAINT llm_usage_reservations_settlement_state_check
 CHECK(settlement_state IN ('unclaimed','reserved','partially_settled','settled','unknown','released'));
