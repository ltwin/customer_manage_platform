package creativeimageedit

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/samson/customer-manage-platform/backend/internal/creativemedia"
)

// ParseSwitch reads CREATIVE_IMAGE_EDIT. Unset means off: offering editing is
// an explicit decision, and it requires GC to be deleting.
func ParseSwitch(v string) (bool, error) {
	switch v {
	case "", "off":
		return false, nil
	case "on":
		return true, nil
	}
	return false, fmt.Errorf("creative image edit %q: want on or off", v)
}

// DefaultCodecConfig uses the binaries on PATH, the upload pixel limits and a
// render root of its own so SweepTemp only ever touches render directories.
// choom exists only on Linux, where the worker image provides it.
func DefaultCodecConfig() CodecConfig {
	media := creativemedia.DefaultConfig()
	cfg := CodecConfig{
		Vips: "vips", VipsHeader: "vipsheader",
		TempRoot: filepath.Join(os.TempDir(), "creative-image-edit"),
		MaxEdge:  media.MaxImageEdge, MaxPixels: media.MaxImagePixels,
	}
	if runtime.GOOS == "linux" {
		cfg.Choom = "choom"
	}
	return cfg
}
