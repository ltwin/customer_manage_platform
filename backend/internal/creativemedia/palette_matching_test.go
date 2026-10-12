package creativemedia_test

import (
	"encoding/json"
	"fmt"
	"image/color"
	"math"
	"os"
	"slices"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/creativelibrary"
	"github.com/samson/customer-manage-platform/backend/internal/creativemedia"
	"github.com/samson/customer-manage-platform/backend/internal/creativepalette"
)

// These assertions traverse Verify -> persisted palettes -> SearchAssets SQL,
// including total_count/facets. There is no second Go implementation of matching.
func TestPalettePhotoMatchingUsesProductionSQL(t *testing.T) {
	f := setup(t)
	f.cfg.QuotaLimitBytes = 32 << 20
	f.svc = paletteService(t, f, f.local)
	ids := make([]string, 12)
	dominant := make([]string, 12)
	for i := range 12 {
		filename := fmt.Sprintf("photo-%02d.jpg", i+1)
		body, err := os.ReadFile("../../../docs/prototypes/creative-workspace/v4/assets/" + filename)
		if err != nil {
			t.Fatal(err)
		}
		u := f.uploadFile(t, f.a, filename, "image/jpeg", "image", body, creativemedia.Target{Kind: "asset", Asset: &creativemedia.AssetTarget{Title: filename}})
		a, err := creativelibrary.GetAsset(t.Context(), f.a, u.Publication.ID)
		if err != nil || len(a.Palette) != 5 {
			t.Fatalf("%s: %+v %v", filename, a.Palette, err)
		}
		ids[i], dominant[i] = a.ID, a.Palette[0].Hex
	}
	query := func(hex string) []string {
		t.Helper()
		p, err := creativelibrary.SearchAssets(t.Context(), f.a, creativelibrary.Search{Limit: 100, Color: hex, IncludeFacets: true})
		if err != nil {
			t.Fatal(err)
		}
		got := []string{}
		for _, a := range p.Items {
			got = append(got, a.ID)
		}
		if p.TotalCount != int64(len(got)) || p.Facets.Kinds["image"] != p.TotalCount {
			t.Fatalf("count mismatch %+v", p)
		}
		return got
	}
	for i, id := range ids {
		if !slices.Contains(query(dominant[i]), id) {
			t.Fatalf("photo %02d not found by its dominant color %s", i+1, dominant[i])
		}
	}
	for _, hex := range []string{"#d9a07a", "#c9655d", "#d9c38a"} {
		got := query(hex)
		for _, i := range []int{1, 5, 6, 8} {
			if slices.Contains(got, ids[i-1]) {
				t.Fatalf("warm %s matched grayscale photo %02d", hex, i)
			}
		}
	}
	if !slices.Contains(query("#c98a96"), ids[6]) {
		t.Fatal("pink flowers missing")
	}
	if !slices.Contains(query("#d9c38a"), ids[11]) {
		t.Fatal("yellow hillside missing")
	}
}

func TestPaletteSQLThresholdBoundaries(t *testing.T) {
	f := setup(t)
	u := f.uploadFile(t, f.a, "test.png", "image/png", "image", solidPNG(t, color.NRGBA{R: 224, G: 128, B: 64, A: 255}), creativemedia.Target{Kind: "asset", Asset: &creativemedia.AssetTarget{Title: "匹配边界"}})
	a, err := creativelibrary.GetAsset(t.Context(), f.a, u.Publication.ID)
	if err != nil {
		t.Fatal(err)
	}
	gray, _ := creativepalette.ParseHex("#808080")
	hue, _ := creativepalette.ParseHex("#ff0060") // Close to 0°, exercises circular distance.
	for _, tc := range []struct {
		name  string
		query creativepalette.Color
		color creativepalette.Color
		want  bool
	}{
		{"neutral same", gray, gray, true},
		{"neutral lightness inside", gray, creativepalette.Color{L: gray.L + .119999, C: .039999, Weight: 1}, true},
		{"neutral lightness outside", gray, creativepalette.Color{L: gray.L + .120001, C: .039999, Weight: 1}, false},
		{"neutral cannot match chromatic boundary", gray, creativepalette.Color{L: gray.L, C: .04, Weight: 1}, false},
		{"colored hue wrap inside", hue, creativepalette.Color{L: hue.L, C: .04, H: math.Mod(hue.H-24.999+360, 360), Weight: .1}, true},
		{"colored hue wrap outside", hue, creativepalette.Color{L: hue.L, C: .04, H: math.Mod(hue.H-25.001+360, 360), Weight: 1}, false},
		{"colored lightness inside", hue, creativepalette.Color{L: hue.L + .199999, C: .04, H: hue.H, Weight: 1}, true},
		{"colored lightness outside", hue, creativepalette.Color{L: hue.L + .200001, C: .04, H: hue.H, Weight: 1}, false},
		{"colored excludes neutral", hue, creativepalette.Color{L: hue.L, C: .039999, H: hue.H, Weight: 1}, false},
		{"less than ten percent", hue, creativepalette.Color{L: hue.L, C: .04, H: hue.H, Weight: .099999}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := json.Marshal([]creativepalette.Color{tc.color})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = f.db.Exec(`UPDATE creative_blob_palettes SET colors=$1 WHERE account_id='media-a' AND blob_id=$2`, raw, a.Content.Media[0].BlobID); err != nil {
				t.Fatal(err)
			}
			p, err := creativelibrary.SearchAssets(t.Context(), f.a, creativelibrary.Search{Limit: 10, Color: tc.query.Hex, IncludeFacets: true})
			if err != nil {
				t.Fatal(err)
			}
			want := int64(0)
			if tc.want {
				want = 1
			}
			if p.TotalCount != want || int64(len(p.Items)) != want || p.Facets.Kinds["image"] != want {
				t.Fatalf("got=%+v want=%d", p, want)
			}
		})
	}
}
