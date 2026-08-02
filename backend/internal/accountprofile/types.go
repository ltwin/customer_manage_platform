package accountprofile

import (
	"errors"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/avatarmedia"
)

const (
	MaxDisplayNameGraphemes    = 40
	avatarGCGrace              = 24 * time.Hour
	maxFreshGenerationAttempts = 4
)

var (
	ErrValidationFailed        = errors.New("validation_failed")
	ErrProfileRevisionConflict = errors.New("profile_revision_conflict")
	ErrAvatarRevisionConflict  = errors.New("avatar_revision_conflict")
	ErrAvatarVersionStale      = errors.New("avatar_version_stale")
	ErrNotFound                = errors.New("not_found")
	ErrAvatarObjectIntegrity   = avatarmedia.ErrObjectIntegrity
	ErrAvatarObjectTemporary   = avatarmedia.ErrObjectTemporary
	errRetryFreshGeneration    = errors.New("retry with fresh avatar generation")
)

// Profile 是账号资料读模型（无行时为虚拟默认）。
type Profile struct {
	DisplayName     *string
	ProfileRevision int64
	AvatarRevision  int64
	AvatarVersion   *string
	AvatarObjectID  *string
	AvatarMediaType *string
	AvatarSize      *int64
	AvatarUpdatedAt *time.Time
	UpdatedAt       *time.Time
}

// OptionalString 区分 omitted / null / value。
type OptionalString struct {
	Set   bool
	Value *string
}

// PatchInput 是展示名称 mutation 输入。
type PatchInput struct {
	DisplayName OptionalString
}

// ObjectRef / ObjectMeta / Content 复用共享媒体类型。
type ObjectRef = avatarmedia.ObjectRef
type ObjectMeta = avatarmedia.ObjectMeta
type Content = avatarmedia.Content
type ObjectStore = avatarmedia.ObjectStore

// ValidationError 携带可读校验消息。
type ValidationError struct {
	Message string
}

func (e ValidationError) Error() string { return e.Message }
func (e ValidationError) Unwrap() error { return ErrValidationFailed }

// GCItem 是到期 GC 行投影。
type GCItem struct {
	Ref      ObjectRef
	Attempts int
}

// VirtualDefault 返回无行时的虚拟资料。
func VirtualDefault() Profile {
	return Profile{ProfileRevision: 0, AvatarRevision: 0}
}
