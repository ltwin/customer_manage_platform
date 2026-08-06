package planningmedia

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"sync/atomic"
)

var (
	ErrImageSizeInvalid   = errors.New("image_size_invalid")
	ErrImageFormatInvalid = errors.New("image_format_invalid")
)

// UploadSpool is a request-private, bounded upload source. The filesystem path
// never leaves this package and is removed by Close after success, replay, or
// failure.
type UploadSpool struct {
	path      string
	size      int64
	checksum  string
	mediaType string
	opens     atomic.Int32
	closeOnce sync.Once
}

func NewUploadSpool(ctx context.Context, source io.Reader, declaredMediaType string) (*UploadSpool, error) {
	if source == nil {
		return nil, ErrImageSizeInvalid
	}
	declaredMediaType = strings.ToLower(strings.TrimSpace(declaredMediaType))
	if _, ok := supportedImageFormats[declaredMediaType]; !ok {
		return nil, fmt.Errorf("image_format_unsupported")
	}
	temporary, err := os.CreateTemp("", "planning-media-upload-*")
	if err != nil {
		return nil, fmt.Errorf("create upload spool: %w", err)
	}
	path := temporary.Name()
	cleanup := func() {
		_ = temporary.Close()
		_ = os.Remove(path)
	}
	hasher := sha256.New()
	buffer := make([]byte, 64*1024)
	prefix := make([]byte, 0, 16)
	var size int64
	for {
		if err := ctx.Err(); err != nil {
			cleanup()
			return nil, err
		}
		read, readErr := source.Read(buffer)
		if read > 0 {
			size += int64(read)
			if size > MaxUploadBytes {
				cleanup()
				return nil, ErrImageSizeInvalid
			}
			if len(prefix) < cap(prefix) {
				remaining := cap(prefix) - len(prefix)
				if remaining > read {
					remaining = read
				}
				prefix = append(prefix, buffer[:remaining]...)
			}
			if _, err := hasher.Write(buffer[:read]); err != nil {
				cleanup()
				return nil, err
			}
			if _, err := temporary.Write(buffer[:read]); err != nil {
				cleanup()
				return nil, fmt.Errorf("write upload spool: %w", err)
			}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			cleanup()
			return nil, fmt.Errorf("read upload: %w", readErr)
		}
	}
	if size == 0 {
		cleanup()
		return nil, ErrImageSizeInvalid
	}
	actualMediaType := sniffImageMediaType(prefix)
	if actualMediaType == "" || actualMediaType != declaredMediaType {
		cleanup()
		return nil, ErrImageFormatInvalid
	}
	if err := temporary.Close(); err != nil {
		_ = os.Remove(path)
		return nil, fmt.Errorf("close upload spool: %w", err)
	}
	return &UploadSpool{
		path: path, size: size,
		checksum: "sha256-" + hex.EncodeToString(hasher.Sum(nil)), mediaType: actualMediaType,
	}, nil
}

func (s *UploadSpool) readAll(ctx context.Context) ([]byte, error) {
	if s == nil || s.path == "" {
		return nil, ErrImageSizeInvalid
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.opens.Add(1)
	file, err := os.Open(s.path)
	if err != nil {
		return nil, fmt.Errorf("open upload spool: %w", err)
	}
	defer func() { _ = file.Close() }()
	body, err := io.ReadAll(io.LimitReader(file, MaxUploadBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read upload spool: %w", err)
	}
	if int64(len(body)) != s.size || len(body) > MaxUploadBytes || digest(body) != s.checksum {
		return nil, ErrImageSizeInvalid
	}
	return body, nil
}

func (s *UploadSpool) Close() error {
	if s == nil {
		return nil
	}
	var err error
	s.closeOnce.Do(func() { err = os.Remove(s.path) })
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func sniffImageMediaType(prefix []byte) string {
	switch {
	case len(prefix) >= 3 && prefix[0] == 0xff && prefix[1] == 0xd8 && prefix[2] == 0xff:
		return "image/jpeg"
	case len(prefix) >= 8 && string(prefix[:8]) == "\x89PNG\r\n\x1a\n":
		return "image/png"
	case len(prefix) >= 12 && string(prefix[:4]) == "RIFF" && string(prefix[8:12]) == "WEBP":
		return "image/webp"
	default:
		return ""
	}
}
