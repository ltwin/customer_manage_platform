-- 只允许在没有新路由数据时回退，避免丢失在途身份和费用关联。
DO $$ BEGIN
 IF EXISTS (SELECT 1 FROM llm_generation_routes) THEN
  RAISE EXCEPTION 'generation routes exist; retain compatible workers and schema';
 END IF;
END $$;
DROP TABLE llm_generation_route_calls;
DROP TABLE llm_generation_routes;
