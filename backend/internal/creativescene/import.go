package creativescene

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/url"
	"slices"
	"unicode/utf8"

	"github.com/samson/customer-manage-platform/backend/internal/creativecontent"
	"github.com/samson/customer-manage-platform/backend/internal/creativedocument"
	"github.com/samson/customer-manage-platform/backend/internal/creativeglb"
	"github.com/samson/customer-manage-platform/backend/internal/creativelibrary"
	"github.com/samson/customer-manage-platform/backend/internal/creativestaging"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// importModelOperationKey imports an external GLB into the library.
const importModelOperationKey = "library.create_asset_from_glb"

const (
	maxImportFileNameRunes    = 255
	maxImportAttributionRunes = 2000
	maxImportSourceURLBytes   = 2048
)

// ImportModelInput is the payload of POST /creative/assets/from-glb.
type ImportModelInput struct {
	GLBUploadID       string                       `json:"glb_upload_id"`
	ThumbnailUploadID string                       `json:"thumbnail_upload_id"`
	TextureUploadIDs  []string                     `json:"texture_upload_ids"`
	Mount             string                       `json:"mount,omitempty"`
	Import            ModelImport                  `json:"import"`
	Materials         map[string]MaterialSlot      `json:"materials"`
	Target            creativelibrary.ImportTarget `json:"target"`
}

// ImportModelToLibrary writes an external GLB the browser converted (api:
// 导入模型到资产库) as an imported model owned by the library, with the
// materials the conversion made, and the asset that holds it. The GLB, the
// thumbnail and every extracted image are staged uploads, all consumed here
// or none. Imports are never deduplicated.
func ImportModelToLibrary(ctx context.Context, scope store.AccountScope, c creativeops.Command) (creativeops.Receipt, error) {
	var in ImportModelInput
	var textures []imageUse
	return (creativeops.Executor{}).Run(ctx, scope, creativeops.Operation{Key: importModelOperationKey, Capability: "manual_write",
		Validate: func(raw json.RawMessage) error {
			if err := creativeops.Decode(raw, &in); err != nil {
				return err
			}
			if in.GLBUploadID == "" || in.ThumbnailUploadID == "" || in.TextureUploadIDs == nil || in.Materials == nil {
				return invalid("缺少 GLB、缩略图、texture_upload_ids 或 materials")
			}
			if in.Mount == "" {
				in.Mount = "floor"
			}
			if !slices.Contains(mounts, in.Mount) {
				return importInvalid("mount", "mount 须为 floor、wall、ceiling 或 free")
			}
			if err := checkImport(in.Import); err != nil {
				return err
			}
			if len(in.TextureUploadIDs) > maxSceneImages {
				return &creativedocument.LimitError{Limit: maxSceneImages, Message: fmt.Sprintf("一次最多导入 %d 张图片", maxSceneImages)}
			}
			for i, id := range in.TextureUploadIDs {
				if id == "" || slices.Index(in.TextureUploadIDs, id) < i {
					return importInvalid(fmt.Sprintf("texture_upload_ids[%d]", i), "texture_upload_ids 不能有空值或重复")
				}
			}
			// The texture rules first, as the scene command reports them: at
			// the field. The rest of a slot's shape is named by the slot.
			if err := writeSlots(in.Materials, "materials"); err != nil {
				var field *creativeops.FieldError
				if errors.As(err, &field) {
					field.Op = -1
				}
				return err
			}
			if err := validateSlots(in.Materials); err != nil {
				return importInvalid("materials", "materials：%v", err)
			}
			textures = slotTextures(in.Materials, -1, "materials")
			if n := len(usedImages(textures)); n > maxSceneImages {
				return &creativedocument.LimitError{Limit: maxSceneImages, Message: fmt.Sprintf("materials 引用的不同图片不能超过 %d 张，现有 %d 张", maxSceneImages, n)}
			}
			return creativelibrary.ValidateImportTarget(in.Target)
		},
		Apply: func(ctx context.Context, tx store.TxAccountScope, _ json.RawMessage) (creativeops.Outcome, error) {
			// Lock order: library root, the staged GLB, thumbnail and images,
			// then the model and its asset.
			if err := creativelibrary.ValidateImportTargetInTx(ctx, tx, in.Target); err != nil {
				return creativeops.Outcome{}, err
			}
			glb, glbStaged, err := lockUpload(ctx, tx, in.GLBUploadID, modelKind)
			if err != nil {
				return creativeops.Outcome{}, err
			}
			staged, ok := decodeModelPayload(glbStaged.Payload.Model)
			if !ok || staged.Generator != nil {
				return creativeops.Outcome{}, &UploadUnavailableError{UploadID: in.GLBUploadID}
			}
			slots := slotIDs(staged.Manifest)
			for _, name := range slices.Sorted(maps.Keys(in.Materials)) {
				if !slices.Contains(slots, name) {
					return creativeops.Outcome{}, importInvalid("materials."+name, "GLB 没有槽位 %s", name)
				}
			}
			thumb, thumbStaged, err := lockUpload(ctx, tx, in.ThumbnailUploadID, imageKind)
			if err != nil {
				return creativeops.Outcome{}, err
			}
			glbBlob, glbOK := original(glbStaged)
			thumbBlob, thumbOK := original(thumbStaged)
			if !glbOK {
				return creativeops.Outcome{}, &UploadUnavailableError{UploadID: in.GLBUploadID}
			}
			if !thumbOK || thumbBlob.Mime != "image/png" {
				return creativeops.Outcome{}, importInvalid("thumbnail_upload_id", "缩略图须为 PNG 图片")
			}
			used := usedImages(textures)
			uploads := make([]creativestaging.Staged, len(in.TextureUploadIDs))
			for i, id := range in.TextureUploadIDs {
				if uploads[i], _, err = lockUpload(ctx, tx, id, imageKind); err != nil {
					return creativeops.Outcome{}, err
				}
				if _, found := slices.BinarySearch(used, uploads[i].ContentRevisionID); !found {
					return creativeops.Outcome{}, importInvalid(fmt.Sprintf("texture_upload_ids[%d]", i), "上传 %s 的图片没有被 materials 用到", id)
				}
			}
			if err := verifyImages(ctx, tx, textures, func(string) bool { return true }); err != nil {
				return creativeops.Outcome{}, err
			}
			result, err := writeImport(ctx, tx, in, staged.Manifest, glbStaged.DeclarationID, used, []creativecontent.ObjectBinding{{Role: "original", BlobID: glbBlob.BlobID}, {Role: "thumbnail", BlobID: thumbBlob.BlobID}})
			if err != nil {
				return creativeops.Outcome{}, err
			}
			for _, s := range append([]creativestaging.Staged{glb, thumb}, uploads...) {
				if err := creativestaging.ConsumeForAsset(ctx, tx, s, result.ID); err != nil {
					return creativeops.Outcome{}, err
				}
			}
			return assetOutcome(result)
		}}, c)
}

// checkImport checks model.import as data: 模型内容「导入的模型」 has it.
func checkImport(m ModelImport) error {
	if n := utf8.RuneCountInString(m.FileName); n < 1 || n > maxImportFileNameRunes {
		return importInvalid("import.file_name", "原文件名须为 1–%d 个字", maxImportFileNameRunes)
	}
	if utf8.RuneCountInString(m.Attribution) > maxImportAttributionRunes {
		return importInvalid("import.attribution", "授权说明不能超过 %d 个字", maxImportAttributionRunes)
	}
	if m.SourceURL == "" {
		return nil
	}
	u, err := url.Parse(m.SourceURL)
	if len(m.SourceURL) > maxImportSourceURLBytes || err != nil || u.Scheme != "http" && u.Scheme != "https" || u.Host == "" {
		return importInvalid("import.source_url", "来源页面须为 http 或 https 地址，不超过 %d 字节", maxImportSourceURLBytes)
	}
	return nil
}

// importInvalid is a validation error at a field of the payload.
func importInvalid(path, format string, args ...any) error {
	return &creativeops.FieldError{Err: creativeops.ErrValidation, Op: -1, Path: path, Message: fmt.Sprintf(format, args...)}
}

// lockUpload is lockStaged for the import, where an upload of the wrong
// declared kind is as unavailable as an expired one: the browser uploaded
// something else under that ID, and only a new upload helps.
func lockUpload(ctx context.Context, tx store.TxAccountScope, uploadID, kind string) (creativestaging.Staged, creativecontent.Revision, error) {
	s, r, err := lockStaged(ctx, tx, uploadID, kind)
	if errors.Is(err, creativeops.ErrValidation) {
		return s, r, &UploadUnavailableError{UploadID: uploadID}
	}
	return s, r, err
}

// writeImport writes the imported model over the staged GLB and thumbnail
// blobs, holding the images its materials use, and creates its asset. The
// title's first 80 characters are the name hint.
func writeImport(ctx context.Context, tx store.TxAccountScope, in ImportModelInput, manifest creativeglb.Manifest, declaration string, textures []string, objects []creativecontent.ObjectBinding) (creativelibrary.AssetResult, error) {
	hint := []rune(in.Target.Title)
	hint = hint[:min(len(hint), maxNameHintRunes)]
	imp := in.Import
	payload, err := json.Marshal(ModelPayload{Format: "glb", Generator: &Generator{Language: languageImported, Mount: in.Mount}, Manifest: manifest, NameHint: string(hint), Materials: in.Materials, Import: &imp})
	if err != nil {
		return creativelibrary.AssetResult{}, err
	}
	draft := creativecontent.Draft{Kind: modelKind, DeclarationID: declaration, Payload: creativecontent.Payload{Model: payload}}
	var result creativelibrary.AssetResult
	if _, err := creativecontent.WriteModelAndRetain(ctx, tx, draft, "", objects, func(r creativecontent.Revision) error {
		if err := creativecontent.HoldTextures(ctx, tx, r.ID, textures); err != nil {
			return err
		}
		var err error
		result, err = creativelibrary.CreateFromRevisionInTx(ctx, tx, r, in.Target)
		return err
	}); err != nil {
		return creativelibrary.AssetResult{}, err
	}
	return result, nil
}
