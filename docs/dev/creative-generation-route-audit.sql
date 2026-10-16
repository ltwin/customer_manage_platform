-- 只读一致性扫描。返回零行才符合关系契约；异常只隔离核实，不用于自动删除。
SELECT r.account_id, r.root_request_id, 'route_request_or_membership' AS issue
FROM llm_generation_routes r
WHERE NOT EXISTS (SELECT 1 FROM llm_requests q WHERE q.account_id=r.account_id AND q.id=r.root_request_id AND q.request_kind='generation')
   OR NOT EXISTS (SELECT 1 FROM llm_generation_route_calls c WHERE c.account_id=r.account_id AND c.root_request_id=r.root_request_id AND c.ordinal=0 AND c.request_id=r.root_request_id)
   OR NOT EXISTS (SELECT 1 FROM llm_generation_route_calls c WHERE c.account_id=r.account_id AND c.root_request_id=r.root_request_id AND c.ordinal=r.ordinal AND c.request_id=r.current_request_id)
   OR (r.final_request_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM llm_generation_route_calls c WHERE c.account_id=r.account_id AND c.root_request_id=r.root_request_id AND c.request_id=r.final_request_id))
   OR r.ordinal>=r.max_submissions
UNION ALL
SELECT c.account_id, c.root_request_id, 'call_request_or_route'
FROM llm_generation_route_calls c
WHERE NOT EXISTS (SELECT 1 FROM llm_generation_routes r WHERE r.account_id=c.account_id AND r.root_request_id=c.root_request_id)
   OR NOT EXISTS (SELECT 1 FROM llm_requests q WHERE q.account_id=c.account_id AND q.id=c.request_id AND q.request_kind='generation')
   OR NOT EXISTS (SELECT 1 FROM llm_generation_requests g WHERE g.account_id=c.account_id AND g.request_id=c.request_id);
