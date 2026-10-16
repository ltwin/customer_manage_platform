-- 未计费生成只保留许可身份，金额未知且不占预算；旧行完全不改写。
ALTER TABLE llm_usage_reservations DROP CONSTRAINT llm_usage_reservations_settlement_state_check;
ALTER TABLE llm_usage_reservations ADD CONSTRAINT llm_usage_reservations_settlement_state_check
 CHECK(settlement_state IN ('unclaimed','reserved','partially_settled','settled','unknown','released','not_applicable'));
ALTER TABLE llm_usage_reservations ADD CONSTRAINT llm_reservation_no_billing_values
 CHECK(settlement_state <> 'not_applicable' OR
 (initial_reserved_micros=0 AND initial_reserved_tokens=0 AND remaining_hold_micros=0
 AND remaining_hold_tokens=0 AND actual_micros IS NULL AND actual_tokens IS NULL));
