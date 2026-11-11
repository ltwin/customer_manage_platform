package creativelibrary

import (
	"errors"
	"slices"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
)

func TestBuiltinCharacterIPCatalog(t *testing.T) {
	if builtinCharacterIPsErr != nil {
		t.Fatal(builtinCharacterIPsErr)
	}
	if n := len(builtinCharacterIPs.entries); n < 50 || n > 100 {
		t.Fatalf("initial catalog contains %d entries, expected 50–100", n)
	}
}

func TestCharacterWorkAliasesAreScopedToIP(t *testing.T) {
	catalog, err := readCharacterIPCatalog([]byte(`{"ips":[
		{"key":"moon","name":"月","works":[{"name":"月的编年史","aliases":["Chronicle"]}]},
		{"key":"star","name":"星","works":[{"name":"星的编年史","aliases":["Chronicle"]}]}
	]}`))
	if err != nil {
		t.Fatal(err)
	}
	var ambiguous *AmbiguousCharacterWorkError
	if _, err := catalog.filter("", "chronicle"); !errors.As(err, &ambiguous) || len(ambiguous.Candidates) != 2 {
		t.Fatalf("expected two matching works: %v", err)
	}
	got, err := catalog.filter("moon", "chronicle")
	if err != nil || !slices.Equal(got.works, []string{"月的编年史", "chronicle"}) {
		t.Fatalf("work under explicit IP: %+v, %v", got, err)
	}
}

func TestCharacterKeywordMatchesCatalogLabels(t *testing.T) {
	catalog, err := readCharacterIPCatalog([]byte(`{"ips":[
		{"key":"moon","name":"月","aliases":["Luna"],"works":[{"name":"月的编年史","aliases":["MC"]}]}
	]}`))
	if err != nil {
		t.Fatal(err)
	}
	ip := catalog.keyword(" LUNA ")
	if !slices.Equal(ip.ipKeys, []string{"moon"}) || !slices.Equal(ip.ipNames, []string{"月", "luna"}) || ip.works != nil {
		t.Fatalf("IP alias: %+v", ip)
	}
	work := catalog.keyword("mc")
	if work.ipKeys != nil || !slices.Equal(work.works, []string{"月的编年史", "mc"}) {
		t.Fatalf("work alias: %+v", work)
	}
	if none := catalog.keyword("月光"); none.ipKeys != nil || none.works != nil {
		t.Fatalf("a partial word matched the catalog: %+v", none)
	}
}

func TestCharacterFiltersRejectOtherKinds(t *testing.T) {
	for _, q := range []Search{
		{Kind: "image", CharacterIP: "fate", Limit: 20},
		{Kind: "model", CharacterWork: "FGO", Limit: 20},
	} {
		if _, err := q.normalized(); !errors.Is(err, creativeops.ErrValidation) {
			t.Fatalf("%+v: %v", q, err)
		}
	}
	for _, q := range []Search{
		{Kind: "character", CharacterIP: "fate", Limit: 20},
		{CharacterWork: "FGO", Limit: 20},
	} {
		if _, err := q.normalized(); err != nil {
			t.Fatalf("%+v: %v", q, err)
		}
	}
}
