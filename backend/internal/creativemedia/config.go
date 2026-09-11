package creativemedia

import "time"

// Config fixes the validated media limits. Formats are enabled only when the
// verification tooling for them is actually available at runtime.
type Config struct {
	ImageMaxBytes    int64
	AVMaxBytes       int64
	PartSize         int64
	MaxParts         int
	SessionTTL       time.Duration
	SignatureTTL     time.Duration
	CandidateTTL     time.Duration
	TicketTTL        time.Duration
	ReadPinTTL       time.Duration
	QuotaLimitBytes  int64
	MaxImageEdge     int
	MaxImagePixels   int64
	DisplayMaxEdge   int
	FFProbe          string
	MaxProbeDuration time.Duration
}

func DefaultConfig() Config {
	return Config{
		ImageMaxBytes:    25 << 20,
		AVMaxBytes:       250 << 20,
		PartSize:         8 << 20,
		MaxParts:         100,
		SessionTTL:       24 * time.Hour,
		SignatureTTL:     10 * time.Minute,
		CandidateTTL:     7 * 24 * time.Hour,
		TicketTTL:        10 * time.Minute,
		ReadPinTTL:       120 * time.Second,
		QuotaLimitBytes:  20 << 30,
		MaxImageEdge:     12000,
		MaxImagePixels:   60_000_000,
		DisplayMaxEdge:   1600,
		FFProbe:          "ffprobe",
		MaxProbeDuration: 60 * time.Second,
	}
}
