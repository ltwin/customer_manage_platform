-- Empty limit_key identifies attempts created without a platform supplier quota.
-- Keep historical keys and counters for in-flight cleanup.
ALTER TABLE llm_attempts DROP CONSTRAINT llm_attempts_limit_key_check;
ALTER TABLE llm_attempts ADD CONSTRAINT llm_attempts_limit_key_check CHECK(char_length(limit_key) BETWEEN 0 AND 120);
