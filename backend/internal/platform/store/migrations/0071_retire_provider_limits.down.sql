-- Keep the widened check: rolling back the binary must not invalidate attempts
-- already written without a platform quota. Historical rows remain compatible.
SELECT 1;
