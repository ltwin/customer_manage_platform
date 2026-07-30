// Package avatarimage validates and normalizes customer avatar uploads.
package avatarimage

import (
	"bytes"
	"fmt"
	"image"
	"image/png"

	"github.com/disintegration/imaging"
	_ "golang.org/x/image/webp" // 向 image.Decode 注册 WebP 解码器。

	customerdomain "github.com/samson/customer-manage-platform/backend/internal/customer"
)

const (
	maxInputDimension = 4096
	maxOutputEdge     = 512
)

type Processor struct{}

func NewProcessor() Processor { return Processor{} }

func (Processor) Process(raw []byte, declaredMediaType string) (customerdomain.AvatarContent, error) {
	if len(raw) == 0 || len(raw) > customerdomain.MaxAvatarContentBytes {
		return customerdomain.AvatarContent{}, validationError("头像文件大小必须在 1 byte 到 5 MiB 之间")
	}
	wantFormat, ok := allowedFormats[declaredMediaType]
	if !ok {
		return customerdomain.AvatarContent{}, validationError("头像仅支持 JPEG、PNG、WebP")
	}
	config, actualFormat, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		return customerdomain.AvatarContent{}, validationError("头像文件无法解码")
	}
	if actualFormat != wantFormat {
		return customerdomain.AvatarContent{}, validationError("头像声明媒体类型与实际格式不符")
	}
	if !validDimensions(config.Width, config.Height) {
		return customerdomain.AvatarContent{}, validationError("头像宽高必须在 1 到 4096 像素之间")
	}

	decoded, err := imaging.Decode(bytes.NewReader(raw), imaging.AutoOrientation(true))
	if err != nil {
		return customerdomain.AvatarContent{}, validationError("头像文件无法完整解码")
	}
	width, height := decoded.Bounds().Dx(), decoded.Bounds().Dy()
	if !validDimensions(width, height) {
		return customerdomain.AvatarContent{}, validationError("方向校正后的头像尺寸非法")
	}
	if width > maxOutputEdge || height > maxOutputEdge {
		if width >= height {
			decoded = imaging.Resize(decoded, maxOutputEdge, 0, imaging.Lanczos)
		} else {
			decoded = imaging.Resize(decoded, 0, maxOutputEdge, imaging.Lanczos)
		}
	}

	var normalized bytes.Buffer
	if err := imaging.Encode(
		&normalized,
		decoded,
		imaging.PNG,
		imaging.PNGCompressionLevel(png.BestSpeed),
	); err != nil {
		return customerdomain.AvatarContent{}, fmt.Errorf("encode normalized avatar: %w", err)
	}
	if normalized.Len() > customerdomain.MaxAvatarContentBytes {
		return customerdomain.AvatarContent{}, validationError("规范化头像超过 5 MiB")
	}
	return customerdomain.NewAvatarContent(normalized.Bytes(), "image/png")
}

var allowedFormats = map[string]string{
	"image/jpeg": "jpeg",
	"image/png":  "png",
	"image/webp": "webp",
}

func validDimensions(width, height int) bool {
	return width > 0 && height > 0 && width <= maxInputDimension && height <= maxInputDimension
}

func validationError(message string) error {
	return customerdomain.ValidationError{Message: message}
}
