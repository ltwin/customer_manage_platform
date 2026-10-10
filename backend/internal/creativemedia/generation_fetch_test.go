package creativemedia

import (
	"context"
	"testing"
)

func TestGenerationFetcherOnlyOpensListedHTTPSHosts(t *testing.T) {
	f, err := NewGenerationHTTPFetcher([]string{"assets.example"})
	if err != nil {
		t.Fatal(err)
	}
	for _, location := range []string{"http://assets.example/a", "https://other.example/a", "https://assets.example:8443/a", "https://secret@assets.example/a", "https://assets.example/a#fragment", "https://127.0.0.1/a"} {
		if _, err = f.Open(context.Background(), location); err != ErrGenerationFetch {
			t.Fatal("unsafe URL accepted", err)
		}
	}
}
