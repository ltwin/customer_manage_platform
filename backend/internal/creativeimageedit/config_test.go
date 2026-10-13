package creativeimageedit

import (
	"errors"
	"math"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/creativemedia"
)

func TestEnabledRequiresDeletingGC(t *testing.T) {
	for _, tc := range []struct {
		value   string
		gc      creativemedia.GCMode
		on, bad bool
	}{
		{"", creativemedia.GCScan, false, false},
		{"off", creativemedia.GCOff, false, false},
		{"on", creativemedia.GCScan, false, true},
		{"on", creativemedia.GCOff, false, true},
		{" on ", creativemedia.GCDelete, true, false},
		{"true", creativemedia.GCDelete, false, true},
	} {
		on, err := Enabled(tc.value, tc.gc)
		if on != tc.on || (err != nil) != tc.bad {
			t.Errorf("%q/%s: %v %v", tc.value, tc.gc, on, err)
		}
	}
}

func TestDisabledAPILeavesGCConfigAlone(t *testing.T) {
	for _, value := range []string{"", "off"} {
		if on, err := EnabledFromValues(value, "typo"); on || err != nil {
			t.Fatalf("disabled API: %v %v", on, err)
		}
	}
	if _, err := EnabledFromValues("on", "typo"); err == nil {
		t.Fatal("enabled accepted invalid GC")
	}
	if on, err := EnabledFromValues("on", "delete"); !on || err != nil {
		t.Fatalf("enabled: %v %v", on, err)
	}
}

func TestCropBoundsDoNotOverflow(t *testing.T) {
	for _, crop := range []Crop{{Left: math.MaxInt, Width: 2, Height: 2}, {Top: math.MaxInt, Width: 2, Height: 2}, {Width: math.MaxInt, Height: 2}, {Width: 2, Height: math.MaxInt}, {Left: 11999, Width: 2, Height: 2}} {
		if cropFits(crop, 12000, 12000) {
			t.Fatalf("accepted %+v", crop)
		}
		req := Request{Mime: "image/png", Action: ActionCrop, Crop: crop}
		if !errors.Is(req.validate(), ErrParameters) {
			t.Fatalf("codec accepted %+v", crop)
		}
	}
	if !cropFits(Crop{Width: 12000, Height: 12000}, 12000, 12000) {
		t.Fatal("edge rejected")
	}
}
