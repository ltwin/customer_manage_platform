// Package creativemedia owns upload sessions, verification, publication and
// authorized reads of image/video/audio bytes for the creative namespace.
package creativemedia

import (
	"context"
	"errors"
	"io"
	"time"
)

var (
	ErrNotFound      = errors.New("creative upload not found")
	ErrState         = errors.New("creative upload state does not allow this action")
	ErrExpired       = errors.New("creative upload expired")
	ErrUnsupported   = errors.New("creative media format unsupported")
	ErrSizeLimit     = errors.New("creative media exceeds size limit")
	ErrQuota         = errors.New("creative media quota exceeded")
	ErrTicket        = errors.New("creative media ticket invalid")
	ErrRange         = errors.New("creative media range not satisfiable")
	ErrEpoch         = errors.New("creative upload execution epoch is stale")
	ErrUnknownResult = errors.New("external storage result unknown")
)

// Part is a server-observed uploaded chunk. Client reports are never trusted.
type Part struct {
	Number int
	ETag   string
	Size   int64
}

// PartAuthorization is a short-lived write capability for exactly one part.
type PartAuthorization struct {
	Number    int               `json:"part_number"`
	Method    string            `json:"method"`
	URL       string            `json:"url"`
	Headers   map[string]string `json:"headers"`
	ExpiresAt time.Time         `json:"expires_at"`
}

// ByteRange is an inclusive single range already validated by the caller.
type ByteRange struct {
	Start int64
	End   int64
}

// ObjectStat describes a fixed version without exposing credentials.
type ObjectStat struct {
	Size    int64
	Version string
}

// Adapter is the streaming object port. Keys are produced by the server; the
// adapter never accepts a client-chosen key, and every read is version-fixed.
type Adapter interface {
	Driver() string
	Bucket() string
	// InitMultipart starts a staging upload at key and returns its session id.
	InitMultipart(ctx context.Context, key, mime string) (string, error)
	// AuthorizePart signs one part write for the session; the URL is temporary.
	AuthorizePart(ctx context.Context, key, session string, part int, expires time.Time) (PartAuthorization, error)
	// ListParts reports the parts the storage actually holds for the session.
	ListParts(ctx context.Context, key, session string) ([]Part, error)
	// CompleteMultipart fixes the staged object and returns its version.
	CompleteMultipart(ctx context.Context, key, session string, parts []Part) (string, error)
	AbortMultipart(ctx context.Context, key, session string) error
	StatVersion(ctx context.Context, key, version string) (ObjectStat, error)
	// OpenVersion streams a fixed version, optionally one inclusive byte range.
	OpenVersion(ctx context.Context, key, version string, rng *ByteRange) (io.ReadCloser, ObjectStat, error)
	// PublishVerified writes trusted bytes to a preallocated final key.
	PublishVerified(ctx context.Context, key string, body io.Reader, size int64, mime string) (string, error)
	DeleteExact(ctx context.Context, key, version string) error
}

// LocalPartWriter is satisfied by the local adapter only; the API forwards
// signed part uploads into it.
type LocalPartWriter interface {
	WritePart(ctx context.Context, key, session string, part int, body io.Reader, limit int64) (Part, error)
}
