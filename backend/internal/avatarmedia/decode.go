package avatarmedia

import (
	"bytes"
	"image"
	_ "image/jpeg"
	_ "image/png"

	_ "golang.org/x/image/webp"
)

const maxInputDimension = 4096

var allowedFormats = map[string]string{
	"image/jpeg": "jpeg",
	"image/png":  "png",
	"image/webp": "webp",
}

var imageMediaTypes = map[string]string{
	"jpeg": "image/jpeg",
	"png":  "image/png",
	"webp": "image/webp",
}

// DecodeConfirm 解码确认 JPEG／PNG／WebP、大小／尺寸边界、声明与实际格式一致，
// 返回原始合规字节与真实 media type；不强制重编码。
func DecodeConfirm(raw []byte, declaredMediaType string) (Content, error) {
	if len(raw) == 0 || len(raw) > MaxContentBytes {
		return Content{}, ValidationError{Message: "头像文件大小必须在 1 byte 到 5 MiB 之间"}
	}
	wantFormat, ok := allowedFormats[declaredMediaType]
	if !ok {
		return Content{}, ValidationError{Message: "头像仅支持 JPEG、PNG、WebP"}
	}
	config, actualFormat, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		return Content{}, ValidationError{Message: "头像文件无法解码"}
	}
	if actualFormat != wantFormat {
		return Content{}, ValidationError{Message: "头像声明媒体类型与实际格式不符"}
	}
	if !validDimensions(config.Width, config.Height) {
		return Content{}, ValidationError{Message: "头像宽高必须在 1 到 4096 像素之间"}
	}
	return NewContent(raw, declaredMediaType)
}

// ConfirmIntegrity 供读路径使用：校验字节可按期望 media type 解码且尺寸合法。
// 成功时返回基于原始字节的 Content；失败返回 false（不返回 ValidationError，对齐既有读路径语义）。
func ConfirmIntegrity(raw []byte, expectedMediaType string) (Content, bool) {
	if len(raw) == 0 || len(raw) > MaxContentBytes {
		return Content{}, false
	}
	wantFormat, ok := allowedFormats[expectedMediaType]
	if !ok {
		return Content{}, false
	}
	_, actualFormat, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil || actualFormat != wantFormat {
		return Content{}, false
	}
	if imageMediaTypes[actualFormat] != expectedMediaType {
		return Content{}, false
	}
	content, err := NewContent(raw, expectedMediaType)
	if err != nil {
		return Content{}, false
	}
	return content, true
}

func validDimensions(width, height int) bool {
	return width > 0 && height > 0 && width <= maxInputDimension && height <= maxInputDimension
}
