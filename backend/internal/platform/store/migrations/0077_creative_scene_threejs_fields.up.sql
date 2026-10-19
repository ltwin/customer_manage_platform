-- scene-schema-threejs (scene-editor-3d revision v7): fog, camera and light
-- fields take their three.js names and meanings. Before launch, so existing
-- scene snapshots are rewritten in place once and schema_version stays 1.
-- Values keep their meaning: fog and camera near_m/far_m become near/far; a
-- light's range_m (null meaning no cutoff) becomes distance (0 meaning no
-- cutoff), decay takes three.js's 2, and a spot's angle_deg, never defined
-- and never rendered, is read as the full cone and becomes the half-angle
-- angle in radians. Fields a light type does not have are dropped.
WITH scenes AS (
 SELECT r.account_id, r.id, r.payload->'scene' AS scene
 FROM creative_content_revisions r
 JOIN creative_contents c ON c.account_id = r.account_id AND c.id = r.content_id
 WHERE c.kind = 'scene' AND jsonb_typeof(r.payload->'scene'->'objects') = 'array'
), rewritten AS (
 SELECT s.account_id, s.id, jsonb_set(
  CASE WHEN jsonb_typeof(s.scene->'environment'->'fog') = 'object' THEN
   jsonb_set(s.scene, '{environment,fog}', (s.scene->'environment'->'fog') - 'near_m' - 'far_m'
    || jsonb_build_object('near', s.scene->'environment'->'fog'->'near_m', 'far', s.scene->'environment'->'fog'->'far_m'))
  ELSE s.scene END,
  '{objects}',
  COALESCE((SELECT jsonb_agg(CASE
    WHEN jsonb_typeof(o->'camera') = 'object' THEN
     jsonb_set(o, '{camera}', (o->'camera') - 'near_m' - 'far_m'
      || jsonb_build_object('near', o->'camera'->'near_m', 'far', o->'camera'->'far_m'))
    WHEN jsonb_typeof(o->'light') = 'object' THEN
     jsonb_set(o, '{light}', (o->'light') - 'range_m' - 'angle_deg' - 'penumbra'
      || CASE o->'light'->>'type'
       WHEN 'point' THEN jsonb_build_object('distance', COALESCE(NULLIF(o->'light'->'range_m', 'null'), '0'), 'decay', 2)
       WHEN 'spot' THEN jsonb_build_object('distance', COALESCE(NULLIF(o->'light'->'range_m', 'null'), '0'), 'decay', 2,
        'angle', radians((o->'light'->>'angle_deg')::float8) / 2, 'penumbra', COALESCE(NULLIF(o->'light'->'penumbra', 'null'), '0'))
       ELSE '{}'::jsonb END)
    ELSE o END ORDER BY e.n)
   FROM jsonb_array_elements(s.scene->'objects') WITH ORDINALITY AS e(o, n)), '[]'::jsonb)) AS scene
 FROM scenes s
)
UPDATE creative_content_revisions r SET payload = jsonb_set(r.payload, '{scene}', w.scene)
FROM rewritten w
WHERE r.account_id = w.account_id AND r.id = w.id;
