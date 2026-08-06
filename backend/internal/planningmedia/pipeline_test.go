package planningmedia

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/png"
	"testing"
)

func TestProcessImageDeterministicDisplayAndBounds(t *testing.T) {
	var b bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 64, 32))
	for y := 0; y < 32; y++ {
		for x := 0; x < 64; x++ {
			img.Set(x, y, color.RGBA{uint8(x), uint8(y), 20, 255})
		}
	}
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	a, err := ProcessImage(b.Bytes(), "image/png")
	if err != nil {
		t.Fatal(err)
	}
	c, err := ProcessImage(b.Bytes(), "image/png")
	if err != nil {
		t.Fatal(err)
	}
	if a.Original.Checksum != c.Original.Checksum || a.Display.Checksum != c.Display.Checksum || !bytes.Equal(a.Display.Bytes, c.Display.Bytes) {
		t.Fatal("pipeline must be deterministic")
	}
	if a.Display.MediaType != "image/png" || a.Display.Width > MaxDisplayEdge || a.Display.Height > MaxDisplayEdge {
		t.Fatalf("display=%+v", a.Display)
	}
}

func TestProcessImageRejectsMimeAndPixelBomb(t *testing.T) {
	if _, err := ProcessImage([]byte("not image"), "image/png"); err == nil {
		t.Fatal("corrupt image accepted")
	}
	if _, err := ProcessImage([]byte("GIF89a"), "image/gif"); err == nil {
		t.Fatal("gif accepted")
	}
	base := encodePNG(t, image.NewRGBA(image.Rect(0, 0, 1, 1)))
	if _, err := ProcessImage(pngWithDimensions(t, base, 10_000, 7_000), "image/png"); err == nil || err.Error() != "image_dimensions_invalid" {
		t.Fatalf("pixel bomb accepted or misclassified: %v", err)
	}
	if _, err := ProcessImage(make([]byte, MaxUploadBytes+1), "image/png"); err == nil || err.Error() != "image_size_invalid" {
		t.Fatalf("oversize accepted or misclassified: %v", err)
	}
	animated := append([]byte("RIFF\x0e\x00\x00\x00WEBPVP8X\x01\x00\x00\x00\x02\x00"), 0)
	if _, err := ProcessImage(animated, "image/webp"); err == nil || err.Error() != "image_animation_unsupported" {
		t.Fatalf("animated webp accepted or misclassified: %v", err)
	}
}

func TestProcessImageStripsPNGMetadata(t *testing.T) {
	base := encodePNG(t, image.NewRGBA(image.Rect(0, 0, 2, 2)))
	withText := insertPNGText(t, base, "gps-comment", "secret-location")
	processed, err := ProcessImage(withText, "image/png")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(processed.Original.Bytes, []byte("secret-location")) {
		t.Fatal("fixture does not contain metadata")
	}
	if bytes.Contains(processed.Display.Bytes, []byte("gps-comment")) || bytes.Contains(processed.Display.Bytes, []byte("secret-location")) {
		t.Fatal("display retained PNG metadata")
	}
}

func encodePNG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func pngWithDimensions(t *testing.T, raw []byte, width, height uint32) []byte {
	t.Helper()
	if len(raw) < 33 || string(raw[12:16]) != "IHDR" {
		t.Fatal("invalid PNG fixture")
	}
	result := append([]byte(nil), raw...)
	binary.BigEndian.PutUint32(result[16:20], width)
	binary.BigEndian.PutUint32(result[20:24], height)
	binary.BigEndian.PutUint32(result[29:33], crc32.ChecksumIEEE(result[12:29]))
	return result
}

func insertPNGText(t *testing.T, raw []byte, key, value string) []byte {
	t.Helper()
	if len(raw) < 12 || string(raw[len(raw)-8:len(raw)-4]) != "IEND" {
		t.Fatal("invalid PNG fixture")
	}
	payload := append(append([]byte(key), 0), []byte(value)...)
	chunk := make([]byte, 12+len(payload))
	binary.BigEndian.PutUint32(chunk[:4], uint32(len(payload)))
	copy(chunk[4:8], "tEXt")
	copy(chunk[8:], payload)
	binary.BigEndian.PutUint32(chunk[len(chunk)-4:], crc32.ChecksumIEEE(chunk[4:len(chunk)-4]))
	return append(append(append([]byte(nil), raw[:len(raw)-12]...), chunk...), raw[len(raw)-12:]...)
}
