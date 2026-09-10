-- recent 排序骨架。传入已转义的 q_pattern；其他输入由服务端验证。
-- $1 account; $2 text[] tags; $3 all/any; $4 q_pattern/null;
-- $5 view; $6 group_id/null; $7 descendants; $8 kind/null;
-- $9 cursor_created_at/null; $10 cursor_id/null; $11 limit。
WITH RECURSIVE group_scope(id) AS (
 SELECT id FROM creative_asset_groups WHERE account_id=$1 AND id=$6
 UNION
 SELECT g.id FROM creative_asset_groups g JOIN group_scope s ON g.parent_id=s.id
 WHERE g.account_id=$1 AND $7::boolean
), filtered AS (
 SELECT a.* FROM creative_assets a
 WHERE a.account_id=$1
 AND (($5='trash' AND a.deleted_at IS NOT NULL) OR ($5<>'trash' AND a.deleted_at IS NULL))
 AND ($5<>'favorites' OR a.is_favorite)
 AND ($5<>'unclassified' OR NOT EXISTS(SELECT 1 FROM creative_asset_group_members m WHERE m.account_id=a.account_id AND m.asset_id=a.id))
 AND ($6::text IS NULL OR EXISTS(SELECT 1 FROM creative_asset_group_members m JOIN group_scope g ON g.id=m.group_id WHERE m.account_id=a.account_id AND m.asset_id=a.id))
 AND ($8::text IS NULL OR a.kind=$8)
 AND (cardinality($2::text[])=0 OR
  ($3='any' AND EXISTS(SELECT 1 FROM creative_asset_tags t WHERE t.account_id=a.account_id AND t.asset_id=a.id AND t.tag_id=ANY($2::text[]))) OR
  ($3='all' AND (SELECT count(DISTINCT t.tag_id) FROM creative_asset_tags t WHERE t.account_id=a.account_id AND t.asset_id=a.id AND t.tag_id=ANY($2::text[]))=cardinality($2::text[])))
 AND ($4::text IS NULL OR EXISTS(SELECT 1 FROM creative_asset_search s WHERE s.account_id=a.account_id AND s.asset_id=a.id AND s.normalized_text LIKE $4 ESCAPE '\')
  OR EXISTS(SELECT 1 FROM creative_asset_tags atg JOIN creative_tags t ON t.account_id=atg.account_id AND t.id=atg.tag_id WHERE atg.account_id=a.account_id AND atg.asset_id=a.id AND t.normalized_name LIKE $4 ESCAPE '\'))
)
SELECT id FROM filtered
WHERE $9::timestamptz IS NULL OR (created_at,id)<($9::timestamptz,$10::text)
ORDER BY created_at DESC,id DESC LIMIT $11;
