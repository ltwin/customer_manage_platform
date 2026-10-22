package creativemedia

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"
)

func pngOf(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for x := range w {
		for y := range h {
			img.Set(x, y, color.NRGBA{R: uint8(x), G: uint8(y), B: 128, A: 200})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestFitPreviewBoundsTheLongestEdge(t *testing.T) {
	small := pngOf(t, 300, 200)
	out, mime, err := FitPreview(small, "image/png", 1024, 1<<20)
	if err != nil || mime != "image/png" || !bytes.Equal(out, small) {
		t.Fatalf("an image within bounds was re-encoded: %s %v", mime, err)
	}
	big := pngOf(t, 2400, 1200)
	out, mime, err = FitPreview(big, "image/png", 1024, 1<<20)
	if err != nil || mime != "image/jpeg" {
		t.Fatalf("large image: %s %v", mime, err)
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(out))
	if err != nil || cfg.Width != 1024 || cfg.Height != 512 {
		t.Fatalf("fitted size %dx%d %v", cfg.Width, cfg.Height, err)
	}
	if _, _, err := FitPreview([]byte("not an image"), "image/png", 1024, 1<<20); err == nil {
		t.Fatal("garbage accepted")
	}
	if _, _, err := FitPreview(big, "image/png", 1024, 10); err == nil {
		t.Fatal("byte ceiling ignored")
	}
}
