package creativemedia

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"io"
	"testing"
)

// A photograph-sized image shrinks to at most 2048 px on its long edge, keeps
// its proportions and fits the byte bound.
func TestAgentPreviewShrinksLargeImages(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 3000, 1500))
	seed := uint32(1)
	for i := range src.Pix {
		seed = seed*1664525 + 1013904223
		src.Pix[i] = uint8(seed >> 24)
		if i%4 == 3 {
			src.Pix[i] = 255
		}
	}
	var raw bytes.Buffer
	if err := png.Encode(&raw, src); err != nil {
		t.Fatal(err)
	}
	open := func(context.Context) (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(raw.Bytes())), nil }
	const limit = 2 << 20
	out, err := agentPreview(t.Context(), open, int64(raw.Len()), limit)
	if err != nil {
		t.Fatal(err)
	}
	preview, format, err := image.Decode(bytes.NewReader(out))
	if err != nil || format != "jpeg" || len(out) > limit {
		t.Fatalf("preview %s %d bytes %v", format, len(out), err)
	}
	if b := preview.Bounds(); max(b.Dx(), b.Dy()) > 2048 || b.Dx() != 2*b.Dy() {
		t.Fatalf("preview bounds %v", b)
	}
	_, err = agentPreview(t.Context(), open, int64(raw.Len()), 100)
	var sizeLimit *ImagePreviewLimitError
	if !errors.As(err, &sizeLimit) || sizeLimit.SizeBytes != int64(raw.Len()) || sizeLimit.LimitBytes != 100 || !errors.Is(err, ErrSizeLimit) {
		t.Fatalf("preview limit lacks original size and ceiling: %v", err)
	}
}
