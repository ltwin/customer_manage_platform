package httpapi

import (
	"net/netip"
	"strings"
	"testing"
)

func TestResolveClientSourceUsesOnlyTrustedXFFChain(t *testing.T) {
	trusted := []netip.Prefix{
		netip.MustParsePrefix("192.0.2.0/24"),
		netip.MustParsePrefix("2001:db8:1::/48"),
	}
	tests := map[string]struct {
		direct string
		xff    string
		want   string
	}{
		"untrusted direct ignores xff": {
			direct: "203.0.113.10:4321", xff: "198.51.100.7", want: "203.0.113.10",
		},
		"trusted proxy peels right to left": {
			direct: "192.0.2.10:4321", xff: "198.51.100.7, 192.0.2.9", want: "198.51.100.7",
		},
		"all trusted selects leftmost": {
			direct: "192.0.2.10:4321", xff: "192.0.2.7, 192.0.2.9", want: "192.0.2.7",
		},
		"mapped direct is canonical": {
			direct: "[::ffff:203.0.113.10]:4321", xff: "198.51.100.7", want: "203.0.113.10",
		},
		"port in xff invalidates whole chain": {
			direct: "192.0.2.10:4321", xff: "198.51.100.7:1234", want: "192.0.2.10",
		},
		"quoted xff invalidates whole chain": {
			direct: "192.0.2.10:4321", xff: "\"198.51.100.7\"", want: "192.0.2.10",
		},
		"empty xff item invalidates whole chain": {
			direct: "192.0.2.10:4321", xff: "198.51.100.7, ,192.0.2.9", want: "192.0.2.10",
		},
		"more than sixteen hops invalidates whole chain": {
			direct: "192.0.2.10:4321", xff: strings.Repeat("198.51.100.7,", 16) + "198.51.100.8", want: "192.0.2.10",
		},
		"overlong header invalidates whole chain": {
			direct: "192.0.2.10:4321", xff: strings.Repeat(" ", 1025), want: "192.0.2.10",
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			got := resolveClientSource(test.direct, test.xff, trusted)
			if got.String() != test.want {
				t.Fatalf("source = %q, want %q", got, test.want)
			}
		})
	}
}
