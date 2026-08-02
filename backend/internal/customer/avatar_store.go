package customer

import (
	"github.com/samson/customer-manage-platform/backend/internal/avatarmedia"
)

const MaxAvatarContentBytes = avatarmedia.MaxContentBytes

var (
	ErrAvatarObjectNotFound  = avatarmedia.ErrObjectNotFound
	ErrAvatarObjectKey       = avatarmedia.ErrObjectKey
	ErrAvatarObjectIntegrity = avatarmedia.ErrObjectIntegrity
	ErrAvatarObjectTemporary = avatarmedia.ErrObjectTemporary
)

// AvatarContent 是 avatarmedia.Content 的客户域别名（字节语义不变）。
type AvatarContent = avatarmedia.Content

func NewAvatarContent(content []byte, mediaType string) (AvatarContent, error) {
	return avatarmedia.NewContent(content, mediaType)
}

type ObjectRef = avatarmedia.ObjectRef
type ObjectMeta = avatarmedia.ObjectMeta
type PutResult = avatarmedia.PutResult
type ObjectItem = avatarmedia.ObjectItem
type ObjectPage = avatarmedia.ObjectPage

// AvatarObjectStore 是客户编排使用的中性 ObjectStore 端口。
type AvatarObjectStore = avatarmedia.ObjectStore
