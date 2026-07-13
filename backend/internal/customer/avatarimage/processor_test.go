package avatarimage

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"testing"

	customerdomain "github.com/samson/customer-manage-platform/backend/internal/customer"
)

func TestProcessorAcceptsJPEGPNGAndWebPDeterministically(t *testing.T) {
	processor := NewProcessor()
	fixtures := []struct {
		name      string
		mediaType string
		content   []byte
	}{
		{name: "jpeg", mediaType: "image/jpeg", content: encodeJPEG(t, 1024, 256)},
		{name: "png", mediaType: "image/png", content: encodePNG(t, 256, 1024)},
		{name: "webp", mediaType: "image/webp", content: readFixture(t, "testdata/tux.lossless.webp")},
	}

	for _, fixture := range fixtures {
		t.Run(fixture.name, func(t *testing.T) {
			first, err := processor.Process(fixture.content, fixture.mediaType)
			if err != nil {
				t.Fatalf("Process: %v", err)
			}
			second, err := processor.Process(fixture.content, fixture.mediaType)
			if err != nil {
				t.Fatalf("Process second: %v", err)
			}
			if first.MediaType() != "image/png" || first.Size() > customerdomain.MaxAvatarContentBytes {
				t.Fatalf("normalized metadata mismatch: media=%s size=%d", first.MediaType(), first.Size())
			}
			if !bytes.Equal(first.Bytes(), second.Bytes()) || first.Checksum() != second.Checksum() {
				t.Fatal("same input in one build must produce identical bytes and checksum")
			}
			decoded, err := png.Decode(bytes.NewReader(first.Bytes()))
			if err != nil {
				t.Fatalf("decode normalized PNG: %v", err)
			}
			bounds := decoded.Bounds()
			if bounds.Dx() > 512 || bounds.Dy() > 512 {
				t.Fatalf("longest edge exceeds 512: %v", bounds)
			}
		})
	}
}

func TestProcessorAppliesEXIFOrientationAndRemovesMetadata(t *testing.T) {
	raw := readFixture(t, "testdata/orientation_6.jpg")
	config, err := jpeg.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("decode fixture config: %v", err)
	}
	content, err := NewProcessor().Process(raw, "image/jpeg")
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	decoded, err := png.Decode(bytes.NewReader(content.Bytes()))
	if err != nil {
		t.Fatalf("decode normalized PNG: %v", err)
	}
	if decoded.Bounds().Dx() != config.Height || decoded.Bounds().Dy() != config.Width {
		t.Fatalf("orientation 6 should swap dimensions: input=%dx%d output=%v", config.Width, config.Height, decoded.Bounds())
	}
	if bytes.Contains(content.Bytes(), []byte("Exif")) {
		t.Fatal("normalized PNG must not retain EXIF metadata")
	}
}

func TestProcessorRejectsInvalidInputs(t *testing.T) {
	processor := NewProcessor()
	tests := []struct {
		name      string
		mediaType string
		content   []byte
	}{
		{name: "empty", mediaType: "image/png"},
		{name: "fake mime", mediaType: "image/jpeg", content: encodePNG(t, 2, 2)},
		{name: "corrupt", mediaType: "image/png", content: []byte("not-an-image")},
		{name: "gif disabled", mediaType: "image/gif", content: []byte("GIF89a")},
		{name: "svg disabled", mediaType: "image/svg+xml", content: []byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`)},
		{name: "heic disabled", mediaType: "image/heic", content: []byte("ftypheic")},
		{name: "over five MiB", mediaType: "image/png", content: make([]byte, customerdomain.MaxAvatarContentBytes+1)},
		{name: "width 4097", mediaType: "image/png", content: encodePNG(t, 4097, 1)},
		{name: "height 4097", mediaType: "image/png", content: encodePNG(t, 1, 4097)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := processor.Process(tt.content, tt.mediaType); err == nil {
				t.Fatal("invalid input should fail")
			}
		})
	}
}

func encodeJPEG(t *testing.T, width, height int) []byte {
	t.Helper()
	var output bytes.Buffer
	if err := jpeg.Encode(&output, fixtureImage(width, height), &jpeg.Options{Quality: 90}); err != nil {
		t.Fatalf("encode JPEG: %v", err)
	}
	return output.Bytes()
}

func encodePNG(t *testing.T, width, height int) []byte {
	t.Helper()
	var output bytes.Buffer
	if err := png.Encode(&output, fixtureImage(width, height)); err != nil {
		t.Fatalf("encode PNG: %v", err)
	}
	return output.Bytes()
}

func fixtureImage(width, height int) image.Image {
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := range height {
		for x := range width {
			img.SetNRGBA(x, y, color.NRGBA{R: uint8(x % 251), G: uint8(y % 241), B: 127, A: 255})
		}
	}
	return img
}

func readFixture(t *testing.T, path string) []byte {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return content
}
