package creativeimageedit

import (
	"sort"

	"github.com/samson/customer-manage-platform/backend/internal/creativemedia"
)

const (
	qualityMin = 1
	qualityMax = 100
	// The public crop contract has a fixed hard ceiling. A deployment may
	// enforce smaller image limits, which admission and capabilities share.
	maxCropEdge = 12000
)

type Capabilities struct {
	Available       bool     `json:"available"`
	Actions         []string `json:"actions"`
	InputMIMETypes  []string `json:"input_mime_types"`
	QualityMin      int      `json:"quality_min"`
	QualityMax      int      `json:"quality_max"`
	MaxEdge         int      `json:"max_edge"`
	MaxPixels       int64    `json:"max_pixels"`
	MaxOutputBytes  int64    `json:"max_output_bytes"`
	ReadyTTLSeconds int      `json:"ready_ttl_seconds"`
}

// Capabilities describes domain policy and this service's verifier limits.
// A nil receiver describes the disabled feature; it is not a worker heartbeat.
func (s *Service) Capabilities() Capabilities {
	cfg := creativemedia.DefaultConfig()
	edge, pixels, bytes := cfg.MaxImageEdge, cfg.MaxImagePixels, cfg.ImageMaxBytes
	if s != nil {
		edge, pixels, bytes = s.media.ImageEditLimits()
	}
	mimes := make([]string, 0, len(loaders))
	for mime := range loaders {
		mimes = append(mimes, mime)
	}
	sort.Strings(mimes)
	return Capabilities{Available: s != nil, Actions: []string{ActionCompress, ActionCrop}, InputMIMETypes: mimes, QualityMin: qualityMin, QualityMax: qualityMax, MaxEdge: min(edge, maxCropEdge), MaxPixels: pixels, MaxOutputBytes: bytes, ReadyTTLSeconds: int(readyTTL.Seconds())}
}

// cropFits uses subtraction after bounding the origin, so hostile integers
// cannot overflow the right/bottom edge calculation.
func cropFits(c Crop, width, height int) bool {
	return c.Left >= 0 && c.Top >= 0 && c.Width >= 1 && c.Height >= 1 && c.Left < width && c.Top < height && c.Width <= width-c.Left && c.Height <= height-c.Top
}

// BusyError names the existing candidate so callers can resume or discard it.
// An active node execution still uses ErrNodeBusy without a render id.
type BusyError struct{ RenderID string }

func (e BusyError) Error() string { return ErrNodeBusy.Error() }
func (e BusyError) Unwrap() error { return ErrNodeBusy }
