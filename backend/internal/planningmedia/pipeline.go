package planningmedia

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"image"
	"image/png"

	"github.com/disintegration/imaging"
	_ "golang.org/x/image/webp"
)

const (
	PipelineVersion       = 1
	MaxUploadBytes        = 20 * 1024 * 1024
	MaxInputEdge          = 12000
	MaxPixels       int64 = 60_000_000
	MaxDisplayEdge        = 2560
)

var supportedImageFormats = map[string]string{"image/jpeg": "jpeg", "image/png": "png", "image/webp": "webp"}

type Rendition struct {
	Bytes     []byte
	MediaType string
	Size      int64
	Width     int
	Height    int
	Checksum  string
}

type ProcessedImage struct {
	Original Rendition
	Display  Rendition
}

func ProcessImage(raw []byte, declaredMediaType string) (ProcessedImage, error) {
	if len(raw) == 0 || len(raw) > MaxUploadBytes {
		return ProcessedImage{}, fmt.Errorf("image_size_invalid")
	}
	want, ok := supportedImageFormats[declaredMediaType]
	if !ok {
		return ProcessedImage{}, fmt.Errorf("image_format_unsupported")
	}
	if declaredMediaType == "image/webp" && isAnimatedWebP(raw) {
		return ProcessedImage{}, fmt.Errorf("image_animation_unsupported")
	}
	config, actual, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil || actual != want {
		return ProcessedImage{}, fmt.Errorf("image_format_invalid")
	}
	if !validDimensions(config.Width, config.Height) {
		return ProcessedImage{}, fmt.Errorf("image_dimensions_invalid")
	}
	decoded, err := imaging.Decode(bytes.NewReader(raw), imaging.AutoOrientation(true))
	if err != nil {
		return ProcessedImage{}, fmt.Errorf("image_decode_invalid")
	}
	w, h := decoded.Bounds().Dx(), decoded.Bounds().Dy()
	if !validDimensions(w, h) {
		return ProcessedImage{}, fmt.Errorf("image_dimensions_invalid")
	}
	original := Rendition{Bytes: append([]byte(nil), raw...), MediaType: declaredMediaType, Size: int64(len(raw)), Width: w, Height: h, Checksum: digest(raw)}
	if w > MaxDisplayEdge || h > MaxDisplayEdge {
		if w >= h {
			decoded = imaging.Resize(decoded, MaxDisplayEdge, 0, imaging.Lanczos)
		} else {
			decoded = imaging.Resize(decoded, 0, MaxDisplayEdge, imaging.Lanczos)
		}
	}
	var out bytes.Buffer
	if err := imaging.Encode(&out, decoded, imaging.PNG, imaging.PNGCompressionLevel(png.BestSpeed)); err != nil {
		return ProcessedImage{}, fmt.Errorf("display_encode_failed")
	}
	displayBytes := out.Bytes()
	display := Rendition{Bytes: append([]byte(nil), displayBytes...), MediaType: "image/png", Size: int64(len(displayBytes)), Width: decoded.Bounds().Dx(), Height: decoded.Bounds().Dy(), Checksum: digest(displayBytes)}
	return ProcessedImage{Original: original, Display: display}, nil
}

func isAnimatedWebP(raw []byte) bool {
	if len(raw) < 12 || string(raw[:4]) != "RIFF" || string(raw[8:12]) != "WEBP" {
		return false
	}
	for offset := 12; offset+8 <= len(raw); {
		chunk := string(raw[offset : offset+4])
		size := int(raw[offset+4]) | int(raw[offset+5])<<8 | int(raw[offset+6])<<16 | int(raw[offset+7])<<24
		if size < 0 || offset+8+size > len(raw) {
			return false
		}
		if chunk == "ANIM" || chunk == "ANMF" || (chunk == "VP8X" && size >= 1 && raw[offset+8]&0x02 != 0) {
			return true
		}
		offset += 8 + size + size%2
	}
	return false
}

func validDimensions(w, h int) bool {
	return w > 0 && h > 0 && w <= MaxInputEdge && h <= MaxInputEdge && int64(w)*int64(h) <= MaxPixels
}
func digest(raw []byte) string {
	sum := sha256.Sum256(raw)
	return "sha256-" + hex.EncodeToString(sum[:])
}
