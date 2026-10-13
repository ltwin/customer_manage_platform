package creativeimageedit

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

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
		MaxEdge:  min(media.MaxImageEdge, maxCropEdge), MaxPixels: media.MaxImagePixels,
	}
	if runtime.GOOS == "linux" {
		cfg.Choom = "choom"
	}
	return cfg
}

// Enabled applies the same switch and GC precondition in API and worker.
func Enabled(value string, gcMode creativemedia.GCMode) (bool, error) {
	on, err := ParseSwitch(strings.TrimSpace(value))
	if err != nil || !on {
		return false, err
	}
	if gcMode != creativemedia.GCDelete {
		return false, fmt.Errorf("CREATIVE_IMAGE_EDIT=on requires CREATIVE_MEDIA_GC_MODE=delete: unapplied edit results are only reclaimed by media GC")
	}
	return true, nil
}

// EnabledFromValues leaves unrelated GC configuration alone when the API has
// image editing disabled. The worker still validates its own GC runner config.
func EnabledFromValues(value, gcValue string) (bool, error) {
	on, err := ParseSwitch(strings.TrimSpace(value))
	if err != nil || !on {
		return false, err
	}
	mode, err := creativemedia.ParseGCMode(strings.TrimSpace(gcValue))
	if err != nil {
		return false, err
	}
	return Enabled(value, mode)
}
