package avatarimage

import (
	"bytes"
	"image"
	"image/png"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/avatarmedia"
)

// A18：共享 DecodeConfirm 保留原始字节；customer normalize 仍产出 PNG≤512。
func TestSharedDecodeConfirmDoesNotForcePNGWhileCustomerNormalizeDoes(t *testing.T) {
	var raw bytes.Buffer
	if err := png.Encode(&raw, image.NewRGBA(image.Rect(0, 0, 64, 64))); err != nil {
		t.Fatalf("encode: %v", err)
	}
	confirmed, err := avatarmedia.DecodeConfirm(raw.Bytes(), "image/png")
	if err != nil {
		t.Fatalf("DecodeConfirm: %v", err)
	}
	if !bytes.Equal(confirmed.Bytes(), raw.Bytes()) || confirmed.MediaType() != "image/png" {
		t.Fatal("shared decode must keep original compliant bytes")
	}

	normalized, err := NewProcessor().Process(raw.Bytes(), "image/png")
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if normalized.MediaType() != "image/png" {
		t.Fatalf("customer normalize must remain PNG, got %s", normalized.MediaType())
	}
	decoded, err := png.Decode(bytes.NewReader(normalized.Bytes()))
	if err != nil {
		t.Fatalf("decode normalized: %v", err)
	}
	if decoded.Bounds().Dx() > 512 || decoded.Bounds().Dy() > 512 {
		t.Fatalf("customer normalize must keep edge ≤512: %v", decoded.Bounds())
	}
}
