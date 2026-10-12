package creativepalette

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	_ "image/jpeg"
	"os"
	"reflect"
	"testing"
	"time"
)

func TestExtract(t *testing.T) {
	for _, tc := range []struct {
		name, hex string
		rgb       color.NRGBA
	}{
		{"red", "#e04030", color.NRGBA{224, 64, 48, 255}},
		{"gray", "#808080", color.NRGBA{128, 128, 128, 255}},
		{"white", "#ffffff", color.NRGBA{255, 255, 255, 255}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			img := image.NewNRGBA(image.Rect(0, 0, 100, 80))
			for y := 0; y < 80; y++ {
				for x := 0; x < 100; x++ {
					img.SetNRGBA(x, y, tc.rgb)
				}
			}
			got := Extract(img)
			if got.State != "ready" || len(got.Colors) != 1 || got.Colors[0].Hex != tc.hex || got.Colors[0].Weight != 1 {
				t.Fatalf("%+v", got)
			}
			if !reflect.DeepEqual(got, Extract(img)) {
				t.Fatal("non deterministic")
			}
		})
	}
}
func TestTransparentAndFiveColors(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 50, 10))
	if got := Extract(img); got.State != "failed" || got.Reason == "" {
		t.Fatalf("%+v", got)
	}
	colors := []color.NRGBA{{255, 0, 0, 255}, {0, 255, 0, 255}, {0, 0, 255, 255}, {255, 255, 255, 255}, {0, 0, 0, 255}}
	for y := 0; y < 10; y++ {
		for x := 0; x < 50; x++ {
			img.SetNRGBA(x, y, colors[x/10])
		}
	}
	got := Extract(img)
	if len(got.Colors) != 5 {
		t.Fatalf("%+v", got)
	}
	for _, c := range got.Colors {
		if c.Weight < .19 || c.Weight > .21 {
			t.Fatalf("%+v", got)
		}
	}
}
func TestParseHex(t *testing.T) {
	for _, hex := range []string{"#e08040", "#888888", "#ffffff"} {
		c, err := ParseHex(hex)
		if err != nil || c.Hex != hex {
			t.Fatalf("%s: %+v %v", hex, c, err)
		}
	}
	for _, s := range []string{"red", "#123", "#12345g", "123456", "#1234567"} {
		if _, err := ParseHex(s); err == nil {
			t.Fatalf("accepted %s", s)
		}
	}
}

func TestPhotoSamples(t *testing.T) {
	for i := 1; i <= 12; i++ {
		path := fmt.Sprintf("../../../docs/prototypes/creative-workspace/v4/assets/photo-%02d.jpg", i)
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		img, _, err := image.Decode(bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		start := time.Now()
		got := Extract(img)
		if got.State != "ready" || len(got.Colors) != 5 || !reflect.DeepEqual(got, Extract(img)) {
			t.Fatalf("%s: %+v", path, got)
		}
		b, _ := json.Marshal(got)
		t.Logf("photo-%02d %s %s", i, time.Since(start), b)
	}
}
