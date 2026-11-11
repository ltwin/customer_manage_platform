package textindex

import (
	"strings"
	"testing"
)

// A rebuild reads only the payload, so a card's own words must come from it.
func TestDocumentIndexesCharacterCard(t *testing.T) {
	payload := `{"character":{"name":"芙宁娜","variant":"常服","appearance":"白发异色瞳","work":"原神主线","aliases":["Furina"],"ip":{"name":"原神","key":"genshin-impact"},"views":[]}}`
	text, err := Document("芙宁娜 · 常服", "", "", []byte(payload))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"芙宁娜", "常服", "白发异色瞳", "原神主线", "furina", "原神"} {
		if !strings.Contains(text, want) {
			t.Fatalf("%q missing from %q", want, text)
		}
	}
	if strings.Contains(text, "genshin-impact") {
		t.Fatalf("catalog key indexed as text: %q", text)
	}
}
