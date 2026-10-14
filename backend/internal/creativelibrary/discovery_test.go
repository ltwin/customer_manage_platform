package creativelibrary_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/creativelibrary"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
)

func TestDiscoveryPagesKeepFullDescriptionsAndActualTail(t *testing.T) {
	_, a, b := setup(t)
	desc := strings.Repeat("红", 1000)
	for i := 0; i < 23; i++ {
		_, err := creativelibrary.CreateAsset(t.Context(), a, cmd(t, creativelibrary.CreateAssetInput{Title: fmt.Sprintf("参考%d", i), Description: desc, Content: textDraft("画面")}))
		if err != nil {
			t.Fatal(err)
		}
	}
	q := creativelibrary.Search{Limit: 20, Untagged: true}
	seen := map[string]bool{}
	firstCursor := ""
	for {
		raw, err := creativelibrary.DiscoverAssets(t.Context(), a, q, 16000)
		if err != nil {
			t.Fatal(err)
		}
		if len(raw) > 16000 {
			t.Fatal("budget")
		}
		var page struct {
			Items []struct {
				ID          string               `json:"asset_id"`
				Description string               `json:"description"`
				Revision    creativeops.Revision `json:"revision"`
			}
			Next  string `json:"next_cursor"`
			Total int    `json:"total_count"`
		}
		if err = json.Unmarshal(raw, &page); err != nil {
			t.Fatal(err)
		}
		if page.Total != 23 {
			t.Fatal(page.Total)
		}
		for _, v := range page.Items {
			if seen[v.ID] || v.Description != desc || v.Revision != 1 {
				t.Fatalf("lost/duplicate metadata %+v", v)
			}
			seen[v.ID] = true
		}
		if page.Next == "" {
			break
		}
		if firstCursor == "" {
			firstCursor = page.Next
		}
		q.Cursor = page.Next
	}
	if len(seen) != 23 {
		t.Fatal(len(seen))
	}
	q.Cursor = firstCursor
	if _, err := creativelibrary.DiscoverAssets(t.Context(), b, q, 16000); err == nil {
		t.Fatal("cross-account cursor")
	}
	createAsset(t, a, "新增", nil)
	if _, err := creativelibrary.DiscoverAssets(t.Context(), a, q, 16000); !errors.Is(err, creativelibrary.ErrCursorStale) {
		t.Fatal("stale", err)
	}
}

func TestBrowseBeyond500AndDeleteImpactIncludesTrash(t *testing.T) {
	db, a, _ := setup(t)
	asset := createAsset(t, a, "回收站", []creativelibrary.NewTag{{ClientKey: "tag", Name: "真实标签", Color: "#a1a1a7"}})
	_, err := creativelibrary.Trash(t.Context(), a, cmd(t, creativelibrary.AssetVersion{ID: asset.ID, Revision: 1}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO creative_tags(account_id,id,name,normalized_name,color) SELECT 'library-a','cctg_extra_'||lpad(n::text,4,'0'),'标签'||n,'标签'||n,'#a1a1a7' FROM generate_series(1,550) n`); err != nil {
		t.Fatal(err)
	}
	q := creativelibrary.LibraryQuery{Kind: "tag", Limit: 27}
	seen := map[string]bool{}
	for {
		raw, err := creativelibrary.BrowseLibrary(t.Context(), a, q, 5000)
		if err != nil {
			t.Fatal(err)
		}
		var page struct {
			Items []creativelibrary.LibraryItem
			Next  string `json:"next_cursor"`
			Total int    `json:"total_count"`
		}
		if err = json.Unmarshal(raw, &page); err != nil {
			t.Fatal(err)
		}
		if page.Total != 551 {
			t.Fatal(page.Total)
		}
		for _, v := range page.Items {
			if seen[v.ID] {
				t.Fatal("duplicate")
			}
			seen[v.ID] = true
			if v.ID == asset.TagMapping["tag"] && (v.DeleteImpact == nil || v.DeleteImpact.AssetCount == nil || *v.DeleteImpact.AssetCount != 1) {
				t.Fatalf("trash impact %+v", v)
			}
		}
		if page.Next == "" {
			break
		}
		q.Cursor = page.Next
	}
	if len(seen) != 551 {
		t.Fatal(len(seen))
	}
}

func TestBrowseFiltersNormalizeNamesAndCountDirectDeletionImpact(t *testing.T) {
	_, a, _ := setup(t)
	r, err := creativelibrary.CreateGroup(t.Context(), a, cmd(t, creativelibrary.GroupInput{Name: "ＡＢＣ", HierarchyRevision: 1}))
	root := decode[creativelibrary.LibraryResult](t, r, err)
	r, err = creativelibrary.CreateGroup(t.Context(), a, cmd(t, creativelibrary.GroupInput{Name: "子组", ParentID: &root.ID, HierarchyRevision: root.HierarchyRevision}))
	child := decode[creativelibrary.LibraryResult](t, r, err)
	r, err = creativelibrary.CreateAsset(t.Context(), a, cmd(t, creativelibrary.CreateAssetInput{Title: "子组素材", Content: textDraft("参考"), GroupIDs: []string{child.ID}}))
	asset := decode[creativelibrary.AssetResult](t, r, err)
	if _, err := creativelibrary.Trash(t.Context(), a, cmd(t, creativelibrary.AssetVersion{ID: asset.ID, Revision: 1})); err != nil {
		t.Fatal(err)
	}
	raw, err := creativelibrary.BrowseLibrary(t.Context(), a, creativelibrary.LibraryQuery{Kind: "group", Query: "abc", ParentID: json.RawMessage(`null`), Limit: 1}, 64<<10)
	if err != nil {
		t.Fatal(err)
	}
	var page struct{ Items []creativelibrary.LibraryItem }
	if err := json.Unmarshal(raw, &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].ID != root.ID || page.Items[0].DeleteImpact.AssetCount == nil || *page.Items[0].DeleteImpact.AssetCount != 0 || *page.Items[0].DeleteImpact.PromotedGroupCount != 1 {
		t.Fatalf("wrong direct impact %s", raw)
	}
}
