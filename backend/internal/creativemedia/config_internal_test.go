package creativemedia

import "testing"

// The 3D scene exports at up to 8K on the long side: an 8192² image is
// accepted, one pixel row more is not.
func TestDefaultConfigTakesAn8KSquare(t *testing.T) {
	v := NewVerifier(DefaultConfig())
	if !v.validDimensions(8192, 8192) {
		t.Fatal("8192 × 8192 refused")
	}
	if v.validDimensions(8192, 8193) {
		t.Fatal("8192 × 8193 accepted")
	}
}
