package creativeagent

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"strings"

	"github.com/samson/customer-manage-platform/backend/internal/creativecontent"
	"github.com/samson/customer-manage-platform/backend/internal/creativescene"
	"github.com/samson/customer-manage-platform/backend/internal/platform/llmgateway"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// Built-in textured materials (C5c2 data: 内置贴图库「助手」) are the
// platform's catalog, not account content: read_asset gives one by
// builtin_id without the library grant, and its thumbnail rides the same
// image locator as an account image, prefixed so it never reaches the media
// store or the consent's content list.
const builtinImagePrefix = "builtin:"

func builtinImage(id string) bool { return strings.HasPrefix(id, builtinImagePrefix) }

// agentImage is an image the model is sent: a built-in thumbnail from the
// binary, anything else from the media store.
func (s *Service) agentImage(ctx context.Context, scope store.AccountScope, id string, maxBytes int64) (*llmgateway.ImageInput, error) {
	if material, ok := strings.CutPrefix(id, builtinImagePrefix); ok {
		thumb, ok := creativescene.BuiltinThumbnail(material)
		if !ok {
			return nil, creativecontent.ErrNotFound
		}
		sum := sha256.Sum256(thumb)
		return &llmgateway.ImageInput{RevisionID: id, Digest: "sha256-" + hex.EncodeToString(sum[:]), MIME: "image/webp", ByteSize: int64(len(thumb)),
			Open: func(context.Context) (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(thumb)), nil }}, nil
	}
	if s.images == nil {
		return nil, ErrModelCapability
	}
	return s.images.AgentImage(ctx, scope, id, maxBytes)
}

// readBuiltinAsset is read_asset of a built-in material: its facts and slot,
// its parameters' defaults, and its thumbnail on the terms of a material
// asset's.
func (s *Service) readBuiltinAsset(ctx context.Context, scope store.AccountScope, access readAccess, id string) (map[string]any, error) {
	facts, ok := creativescene.BuiltinMaterialByID(id)
	if !ok {
		return nil, refuseTool("没有内置材质 %q；可用的有：%s", id, strings.Join(creativescene.BuiltinMaterialIDs(), "、"))
	}
	var consent Consent
	if err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		var err error
		consent, err = access.consent(ctx, tx)
		return err
	}); err != nil {
		return nil, err
	}
	result := map[string]any{"builtin_id": id, "material": facts}
	switch {
	case !access.imageCapable():
		result["thumbnail_omitted"] = "model_cannot_see_images"
	case !consentHas(consent, "image_preview"):
		result["thumbnail_omitted"] = "image_preview_not_allowed"
	default:
		result["image_revision_id"] = builtinImagePrefix + id
	}
	return result, nil
}
