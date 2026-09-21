package llmgateway

import (
	"context"
	"fmt"
	"slices"
)

// GenerationMediaReader 的实现负责账号/资源授权、内容摘要核验和受控元数据探测。
// 此端口仅供服务端依赖注入；客户端元数据不得实现或替代它。
type GenerationMediaReader interface {
	ResolveGenerationMedia(context.Context, GenerationReference) (GenerationMediaMetadata, error)
}

// GenerationMediaMetadata 中nil表示尚未获得事实，0是已知值，不能互相代替。
// 图像时长、音频宽高等不适用字段由模态判定，不得伪造数值。
type GenerationMediaMetadata struct {
	RevisionID       string           `json:"revision_id"`
	Digest           string           `json:"digest"`
	ExtractorVersion string           `json:"extractor_version"`
	Kind             GenerationKind   `json:"kind"`
	MIME             string           `json:"mime"`
	ByteSize         int64            `json:"byte_size"`
	DurationMS       *int64           `json:"duration_ms,omitempty"`
	Width            *int64           `json:"width,omitempty"`
	Height           *int64           `json:"height,omitempty"`
	FrameRate        *GenerationRatio `json:"frame_rate,omitempty"`
}

// GenerationFacts 不可从外部JSON构造；元数据与角色/顺序一起绑定输入参考。
type GenerationFacts struct {
	references []GenerationReference
	media      []GenerationMediaMetadata
}

// ReadGenerationFacts 有界串行读取，单次按内容身份复用探测；不共享跨账号授权结果。
func ReadGenerationFacts(ctx context.Context, request GenerationRequest, reader GenerationMediaReader) (GenerationFacts, error) {
	r, err := normalizeGenerationEnvelope(request)
	if err != nil {
		return GenerationFacts{}, err
	}
	result := GenerationFacts{references: slices.Clone(r.Input.References)}
	cache := make(map[string]GenerationMediaMetadata, len(result.references))
	for _, ref := range result.references {
		if err := ctx.Err(); err != nil {
			return GenerationFacts{}, err
		}
		if reader == nil {
			return GenerationFacts{}, ErrGenerationFactsUnavailable
		}
		key := ref.RevisionID + "/" + ref.Digest
		meta, ok := cache[key]
		if !ok {
			meta, err = reader.ResolveGenerationMedia(ctx, ref)
			if err != nil {
				return GenerationFacts{}, err
			}
			meta = cloneGenerationMedia(meta)
			cache[key] = meta
		}
		if err := validateGenerationMedia(ref, meta); err != nil {
			return GenerationFacts{}, err
		}
		result.media = append(result.media, meta)
	}
	return result, nil
}

func validateGenerationMedia(ref GenerationReference, m GenerationMediaMetadata) error {
	if m.RevisionID != ref.RevisionID || m.Digest != ref.Digest || m.Kind != ref.Kind || m.MIME != ref.MIME || m.ByteSize != ref.ByteSize || !generationToken(m.ExtractorVersion) {
		return fmt.Errorf("%w: generation media identity or metadata", ErrConflict)
	}
	for _, v := range []*int64{m.DurationMS, m.Width, m.Height} {
		if v != nil && *v < 0 {
			return fmt.Errorf("%w: negative media fact", ErrValidation)
		}
	}
	if m.Width != nil && *m.Width == 0 || m.Height != nil && *m.Height == 0 {
		return fmt.Errorf("%w: zero media dimensions", ErrValidation)
	}
	if m.Kind != GenerationVideo && m.Kind != GenerationAudio && m.DurationMS != nil || m.Kind != GenerationVideo && m.Kind != GenerationImage && (m.Width != nil || m.Height != nil) || m.Kind != GenerationVideo && m.FrameRate != nil {
		return fmt.Errorf("%w: inapplicable media fact", ErrValidation)
	}
	if m.FrameRate != nil && (m.FrameRate.Numerator <= 0 || m.FrameRate.Denominator <= 0) {
		return fmt.Errorf("%w: media frame rate", ErrValidation)
	}
	return nil
}

func cloneGenerationMedia(m GenerationMediaMetadata) GenerationMediaMetadata {
	if m.DurationMS != nil {
		v := *m.DurationMS
		m.DurationMS = &v
	}
	if m.Width != nil {
		v := *m.Width
		m.Width = &v
	}
	if m.Height != nil {
		v := *m.Height
		m.Height = &v
	}
	if m.FrameRate != nil {
		v := *m.FrameRate
		m.FrameRate = &v
	}
	return m
}

func (f GenerationFacts) validate(refs []GenerationReference) error {
	if !slices.Equal(refs, f.references) || len(refs) != len(f.media) {
		return fmt.Errorf("%w: generation facts binding", ErrConflict)
	}
	for i, ref := range refs {
		if err := validateGenerationMedia(ref, f.media[i]); err != nil {
			return err
		}
	}
	return nil
}
