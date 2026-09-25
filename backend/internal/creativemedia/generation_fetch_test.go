package creativemedia

import (
	"context"
	"net/netip"
	"testing"
)

func TestGenerationFetcherRejectsNonPublicAddresses(t *testing.T) {
	for _, ip := range []string{"127.0.0.1", "10.0.0.1", "169.254.169.254", "100.100.100.200", "::1", "::ffff:127.0.0.1", "fc00::1", "64:ff9b::a00:1", "192.0.2.1", "2002:7f00:1::"} {
		t.Run(ip, func(t *testing.T) {
			if publicGenerationAddress(netip.MustParseAddr(ip)) {
				t.Fatal("private/reserved address accepted")
			}
		})
	}
	for _, ip := range []string{"8.8.8.8", "2606:4700::1111"} {
		if !publicGenerationAddress(netip.MustParseAddr(ip)) {
			t.Fatal("public address refused")
		}
	}
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
