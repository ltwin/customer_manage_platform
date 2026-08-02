// Package avatarimage validates and normalizes customer avatar uploads.
package avatarimage

import (
	"bytes"
	"errors"
	"fmt"
	"image/png"

	"github.com/disintegration/imaging"

	"github.com/samson/customer-manage-platform/backend/internal/avatarmedia"
	customerdomain "github.com/samson/customer-manage-platform/backend/internal/customer"
)

const maxOutputEdge = 512

type Processor struct{}

func NewProcessor() Processor { return Processor{} }

func (Processor) Process(raw []byte, declaredMediaType string) (customerdomain.AvatarContent, error) {
	confirmed, err := avatarmedia.DecodeConfirm(raw, declaredMediaType)
	if err != nil {
		return customerdomain.AvatarContent{}, mapValidation(err)
	}
	return normalizeCustomer(confirmed)
}

// normalizeCustomer 是 customer 专属：≤512 边长重采样 + PNG 重编码。
func normalizeCustomer(confirmed avatarmedia.Content) (customerdomain.AvatarContent, error) {
	decoded, err := imaging.Decode(bytes.NewReader(confirmed.Bytes()), imaging.AutoOrientation(true))
	if err != nil {
		return customerdomain.AvatarContent{}, customerdomain.ValidationError{Message: "头像文件无法完整解码"}
	}
	width, height := decoded.Bounds().Dx(), decoded.Bounds().Dy()
	if width <= 0 || height <= 0 || width > 4096 || height > 4096 {
		return customerdomain.AvatarContent{}, customerdomain.ValidationError{Message: "方向校正后的头像尺寸非法"}
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
		return customerdomain.AvatarContent{}, customerdomain.ValidationError{Message: "规范化头像超过 5 MiB"}
	}
	return customerdomain.NewAvatarContent(normalized.Bytes(), "image/png")
}

func mapValidation(err error) error {
	var ve avatarmedia.ValidationError
	if errors.As(err, &ve) {
		return customerdomain.ValidationError{Message: ve.Message}
	}
	return err
}
