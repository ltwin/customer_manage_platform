package avatarmedia

import (
	"crypto/sha256"
	"fmt"
)

const MaxContentBytes = 5 * 1024 * 1024

// Content 持有 caller-owned、可重放的精确字节（共享层不强制重编码）。
type Content struct {
	bytes     []byte
	mediaType string
	checksum  string
}

func NewContent(content []byte, mediaType string) (Content, error) {
	if len(content) == 0 || len(content) > MaxContentBytes {
		return Content{}, ValidationError{Message: "头像内容大小非法"}
	}
	switch mediaType {
	case "image/jpeg", "image/png", "image/webp":
	default:
		return Content{}, ValidationError{Message: "头像媒体类型非法"}
	}
	digest := sha256.Sum256(content)
	return Content{
		bytes:     append([]byte(nil), content...),
		mediaType: mediaType,
		checksum:  fmt.Sprintf("sha256-%x", digest),
	}, nil
}

func (c Content) Bytes() []byte     { return append([]byte(nil), c.bytes...) }
func (c Content) MediaType() string { return c.mediaType }
func (c Content) Size() int64       { return int64(len(c.bytes)) }
func (c Content) Checksum() string  { return c.checksum }
