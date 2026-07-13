package customer

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"time"
)

const MaxAvatarContentBytes = 5 * 1024 * 1024

var (
	ErrAvatarObjectNotFound  = errors.New("avatar object not found")
	ErrAvatarObjectKey       = errors.New("invalid avatar object key")
	ErrAvatarObjectIntegrity = errors.New("avatar object integrity mismatch")
	ErrAvatarObjectTemporary = errors.New("temporary avatar object error")
)

// AvatarContent 持有 caller-owned、可重放的规范化精确字节。
type AvatarContent struct {
	bytes     []byte
	mediaType string
	checksum  string
}

func NewAvatarContent(content []byte, mediaType string) (AvatarContent, error) {
	if len(content) == 0 || len(content) > MaxAvatarContentBytes {
		return AvatarContent{}, ValidationError{Message: "头像内容大小非法"}
	}
	switch mediaType {
	case "image/jpeg", "image/png", "image/webp":
	default:
		return AvatarContent{}, ValidationError{Message: "头像媒体类型非法"}
	}
	digest := sha256.Sum256(content)
	return AvatarContent{
		bytes:     append([]byte(nil), content...),
		mediaType: mediaType,
		checksum:  fmt.Sprintf("sha256-%x", digest),
	}, nil
}

func (c AvatarContent) Bytes() []byte     { return append([]byte(nil), c.bytes...) }
func (c AvatarContent) MediaType() string { return c.mediaType }
func (c AvatarContent) Size() int64       { return int64(len(c.bytes)) }
func (c AvatarContent) Checksum() string  { return c.checksum }

type ObjectRef struct {
	AvatarVersion  string
	AvatarObjectID string
}

type ObjectMeta struct {
	MediaType  string
	Size       int64
	Checksum   string
	ModifiedAt time.Time
}

type PutResult struct {
	Meta    ObjectMeta
	Created bool
}

type ObjectItem struct {
	Key  string
	Meta ObjectMeta
}

type ObjectPage struct {
	Items      []ObjectItem
	NextCursor string
	Done       bool
}

type AvatarObjectStore interface {
	PutImmutable(context.Context, string, AvatarContent, ObjectMeta) (PutResult, error)
	Open(context.Context, string) (io.ReadCloser, error)
	Stat(context.Context, string) (ObjectMeta, error)
	List(context.Context, string, string, int) (ObjectPage, error)
	Delete(context.Context, string) error
}
