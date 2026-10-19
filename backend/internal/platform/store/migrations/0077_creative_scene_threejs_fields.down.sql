-- The old format has no decay; a light with anything but three.js's 2 cannot
-- go back without changing how it falls off. Refuse and fix forward instead.
DO $$ BEGIN
 IF EXISTS (
  SELECT 1 FROM creative_content_revisions r
  JOIN creative_contents c ON c.account_id = r.account_id AND c.id = r.content_id
  CROSS JOIN LATERAL jsonb_array_elements(CASE WHEN jsonb_typeof(r.payload->'scene'->'objects') = 'array' THEN r.payload->'scene'->'objects' ELSE '[]' END) AS e(o)
  WHERE c.kind = 'scene' AND jsonb_typeof(o->'light') = 'object' AND (o->'light'->>'decay')::float8 <> 2) THEN
  RAISE EXCEPTION 'scene lights with a decay other than 2 exist; fix forward instead of rolling back';
 END IF;
END $$;
WITH scenes AS (
 SELECT r.account_id, r.id, r.payload->'scene' AS scene
 FROM creative_content_revisions r
 JOIN creative_contents c ON c.account_id = r.account_id AND c.id = r.content_id
 WHERE c.kind = 'scene' AND jsonb_typeof(r.payload->'scene'->'objects') = 'array'
), rewritten AS (
 SELECT s.account_id, s.id, jsonb_set(
  CASE WHEN jsonb_typeof(s.scene->'environment'->'fog') = 'object' THEN
   jsonb_set(s.scene, '{environment,fog}', (s.scene->'environment'->'fog') - 'near' - 'far'
    || jsonb_build_object('near_m', s.scene->'environment'->'fog'->'near', 'far_m', s.scene->'environment'->'fog'->'far'))
  ELSE s.scene END,
  '{objects}',
  COALESCE((SELECT jsonb_agg(CASE
    WHEN jsonb_typeof(o->'camera') = 'object' THEN
     jsonb_set(o, '{camera}', (o->'camera') - 'near' - 'far'
      || jsonb_build_object('near_m', o->'camera'->'near', 'far_m', o->'camera'->'far'))
    WHEN jsonb_typeof(o->'light') = 'object' THEN
     jsonb_set(o, '{light}', (o->'light') - 'distance' - 'decay' - 'angle' - 'penumbra'
      || jsonb_build_object(
       'range_m', NULLIF(o->'light'->'distance', '0'),
       'angle_deg', CASE WHEN o->'light' ? 'angle' THEN to_jsonb(round(degrees((o->'light'->>'angle')::float8 * 2)::numeric, 9)) END,
       'penumbra', o->'light'->'penumbra'))
    ELSE o END ORDER BY e.n)
   FROM jsonb_array_elements(s.scene->'objects') WITH ORDINALITY AS e(o, n)), '[]'::jsonb)) AS scene
 FROM scenes s
)
UPDATE creative_content_revisions r SET payload = jsonb_set(r.payload, '{scene}', w.scene)
FROM rewritten w
WHERE r.account_id = w.account_id AND r.id = w.id;
