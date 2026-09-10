package creativelibrary_test

import (
	"fmt"
	"os"
	"sort"
	"testing"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/creativelibrary"
	"github.com/samson/customer-manage-platform/backend/internal/creativelibrary/textindex"
)

func TestLibrarySearchScale(t *testing.T) {
	if os.Getenv("CREATIVE_LIBRARY_BENCHMARK") != "1" {
		t.Skip("set CREATIVE_LIBRARY_BENCHMARK=1 for 10,000-asset sampling")
	}
	db, a, _ := setup(t)
	seed := createAsset(t, a, "基准源", nil)
	_, err := db.Exec(`INSERT INTO creative_assets(id,account_id,kind,title,normalized_title,description,content_id,content_revision_id,is_favorite,deleted_at)
 SELECT 'bench-'||g,a.account_id,a.kind,'摄影参考 '||g,'摄影参考 '||g,'窗边自然光与室内布光',a.content_id,a.content_revision_id,g%3=0,CASE WHEN g%10=0 THEN clock_timestamp() END FROM generate_series(1,10000)g CROSS JOIN creative_assets a WHERE a.account_id='library-a' AND a.id=$1`, seed.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{
		`INSERT INTO creative_asset_groups(id,account_id,parent_id,name,position) SELECT 'g-'||g,'library-a',CASE WHEN g>50 THEN 'g-'||(g-50) END,'分组 '||g,g FROM generate_series(1,100)g`,
		`INSERT INTO creative_tags(id,account_id,name,normalized_name,color) SELECT 't-'||g,'library-a','色调 '||g,'色调 '||g,'#ABCDEF' FROM generate_series(1,200)g`,
		`INSERT INTO creative_asset_group_members(account_id,asset_id,group_id) SELECT 'library-a','bench-'||g,'g-'||(g%100+1) FROM generate_series(1,10000)g`,
		`INSERT INTO creative_asset_tags(account_id,asset_id,tag_id) SELECT 'library-a','bench-'||g,'t-'||(g%200+1) FROM generate_series(1,10000)g`,
		`INSERT INTO creative_asset_tags(account_id,asset_id,tag_id) SELECT 'library-a','bench-'||g,'t-200' FROM generate_series(1,10000)g WHERE g%200<>199`,
	} {
		if _, err = db.Exec(query); err != nil {
			t.Fatal(err)
		}
	}
	_, err = db.Exec(`INSERT INTO creative_asset_search(account_id,asset_id,normalized_text,asset_revision,normalization_version) SELECT account_id,id,normalized_title||E'\n'||description||E'\n窗边 光线 与100%真实参考',revision,$1 FROM creative_assets a WHERE a.account_id='library-a' AND NOT EXISTS(SELECT 1 FROM creative_asset_search s WHERE s.account_id=a.account_id AND s.asset_id=a.id)`, textindex.Version)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`ANALYZE creative_assets;ANALYZE creative_asset_search;ANALYZE creative_asset_group_members;ANALYZE creative_asset_tags`); err != nil {
		t.Fatal(err)
	}
	cold := time.Now()
	if p := search(t, a, creativelibrary.Search{Q: "光"}); p.TotalCount != 9001 {
		t.Fatalf("fixture count %d", p.TotalCount)
	}
	t.Logf("cold first query %s", time.Since(cold))
	samples := []time.Duration{}
	for i := 0; i < 100; i++ {
		q := creativelibrary.Search{Limit: 30}
		switch i % 8 {
		case 0:
			q.Q = "光"
		case 1:
			q.Q = "100%"
			q.View = "favorites"
		case 2:
			q.GroupID = fmt.Sprintf("g-%d", i%40+1)
			q.Descendants = true
		case 3:
			q.TagIDs = []string{"t-3", "t-200"}
			q.TagMode = "all"
		case 4:
			q.TagIDs = []string{"t-3", "t-200"}
			q.TagMode = "any"
		case 5:
			q.Sort = "name"
		case 6:
			q.View = "trash"
			q.Q = "布光"
		case 7:
			q.Sort = "oldest"
			q.Q = "摄影"
		}
		start := time.Now()
		if _, err = creativelibrary.SearchAssets(t.Context(), a, q); err != nil {
			t.Fatal(err)
		}
		samples = append(samples, time.Since(start))
	}
	sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
	t.Logf("100 mixed queries p50=%s p95=%s max=%s; 10,001 assets/100 groups/200 tags", samples[49], samples[94], samples[99])
	if samples[94] > 500*time.Millisecond {
		t.Fatalf("search p95 over budget %s", samples[94])
	}
	rows, err := db.Query(`EXPLAIN (ANALYZE,BUFFERS,FORMAT TEXT) SELECT id FROM creative_assets a WHERE account_id='library-a' AND deleted_at IS NULL AND EXISTS(SELECT 1 FROM creative_asset_search s WHERE s.account_id=a.account_id AND s.asset_id=a.id AND s.normalized_text LIKE '%光%') ORDER BY created_at DESC,id DESC LIMIT 30`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var line string
		if err = rows.Scan(&line); err != nil {
			t.Fatal(err)
		}
		t.Log(line)
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
}
