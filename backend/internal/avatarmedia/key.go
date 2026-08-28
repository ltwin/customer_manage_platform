package avatarmedia

import (
	"fmt"
	"path"
	"regexp"
	"strings"
)

const (
	SubjectCustomer       = "customer"
	SubjectAccountProfile = "account_profile"
)

var (
	segmentPattern  = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)
	versionPattern  = regexp.MustCompile(`^sha256-[0-9a-f]{64}$`)
	objectIDPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)
)

// Key 是经 builder／ParseKey 产生的 canonical 对象键；adapter 不接受未解析任意相对路径。
type Key struct{ value string }

func (k Key) String() string { return k.value }
func (k Key) IsZero() bool   { return k.value == "" }

// Prefix 是 List 的受限前缀。
type Prefix struct{ value string }

func (p Prefix) String() string { return p.value }
func (p Prefix) IsZero() bool   { return p.value == "" }

// Cursor 是 List 分页游标（空表示从头）。
type Cursor struct{ value string }

func (c Cursor) String() string { return c.value }
func (c Cursor) IsZero() bool   { return c.value == "" }

func CursorFromKey(key Key) Cursor { return Cursor(key) }

// CursorFromRaw 从 checkpoint／分页字符串构造游标；空串表示从头。
func CursorFromRaw(raw string) Cursor { return Cursor{value: raw} }

// ObjectRef 标识不可复用的物理代次。
type ObjectRef struct {
	AvatarVersion  string
	AvatarObjectID string
}

// ParsedKey 是 ParseKey 的归类结果。
type ParsedKey struct {
	Key         Key
	AccountID   string
	SubjectKind string
	SubjectID   string
	Ref         ObjectRef
}

// CustomerKey 构建既有客户头像 canonical key（字节原样保持）。
func CustomerKey(accountID, customerID string, ref ObjectRef) (Key, error) {
	if !segmentPattern.MatchString(accountID) || !segmentPattern.MatchString(customerID) ||
		!versionPattern.MatchString(ref.AvatarVersion) || !objectIDPattern.MatchString(ref.AvatarObjectID) {
		return Key{}, ErrObjectKey
	}
	return Key{value: fmt.Sprintf(
		"avatars/%s/customers/%s/%s/%s",
		accountID, customerID, ref.AvatarVersion, ref.AvatarObjectID,
	)}, nil
}

// AccountProfileKey 构建账号资料头像 canonical key。
func AccountProfileKey(accountID string, ref ObjectRef) (Key, error) {
	if !segmentPattern.MatchString(accountID) ||
		!versionPattern.MatchString(ref.AvatarVersion) || !objectIDPattern.MatchString(ref.AvatarObjectID) {
		return Key{}, ErrObjectKey
	}
	return Key{value: fmt.Sprintf(
		"avatars/%s/account-profile/%s/%s",
		accountID, ref.AvatarVersion, ref.AvatarObjectID,
	)}, nil
}

// ParseKey 将相对路径严格归类为 customer 或 account_profile；未知／非法 fail closed。
func ParseKey(raw string) (ParsedKey, error) {
	if err := validateRelativePath(raw); err != nil {
		return ParsedKey{}, ErrObjectKey
	}
	parts := strings.Split(raw, "/")
	switch {
	case len(parts) == 6 && parts[0] == "avatars" && parts[2] == "customers":
		ref := ObjectRef{AvatarVersion: parts[4], AvatarObjectID: parts[5]}
		canonical, err := CustomerKey(parts[1], parts[3], ref)
		if err != nil || canonical.value != raw {
			return ParsedKey{}, ErrObjectKey
		}
		return ParsedKey{
			Key: canonical, AccountID: parts[1], SubjectKind: SubjectCustomer,
			SubjectID: parts[3], Ref: ref,
		}, nil
	case len(parts) == 5 && parts[0] == "avatars" && parts[2] == "account-profile":
		ref := ObjectRef{AvatarVersion: parts[3], AvatarObjectID: parts[4]}
		canonical, err := AccountProfileKey(parts[1], ref)
		if err != nil || canonical.value != raw {
			return ParsedKey{}, ErrObjectKey
		}
		return ParsedKey{
			Key: canonical, AccountID: parts[1], SubjectKind: SubjectAccountProfile,
			SubjectID: parts[1], Ref: ref,
		}, nil
	default:
		return ParsedKey{}, ErrObjectKey
	}
}

// CustomerPrefix 构建客户主体下列举前缀。
func CustomerPrefix(accountID, customerID string) (Prefix, error) {
	if !segmentPattern.MatchString(accountID) || !segmentPattern.MatchString(customerID) {
		return Prefix{}, ErrObjectKey
	}
	return Prefix{value: fmt.Sprintf("avatars/%s/customers/%s/", accountID, customerID)}, nil
}

// AccountPrefix 构建账号头像根下列举前缀。
func AccountPrefix(accountID string) (Prefix, error) {
	if !segmentPattern.MatchString(accountID) {
		return Prefix{}, ErrObjectKey
	}
	return Prefix{value: fmt.Sprintf("avatars/%s/", accountID)}, nil
}

// ParsePrefix 校验 List 前缀：相对、clean、avatars/ 下；允许单个末尾 /
// 表示完整路径段边界，避免 OSS 的字符串前缀跨账号/客户匹配。
func ParsePrefix(raw string) (Prefix, error) {
	trimmed := strings.TrimSuffix(raw, "/")
	if raw == "" || path.IsAbs(raw) || path.Clean(trimmed) != trimmed || !strings.HasPrefix(raw, "avatars/") {
		return Prefix{}, ErrObjectKey
	}
	if strings.Contains(raw, "\\") || strings.Contains(raw, "//") {
		return Prefix{}, ErrObjectKey
	}
	return Prefix{value: raw}, nil
}

func validateRelativePath(value string) error {
	if value == "" || strings.Contains(value, "\\") || path.IsAbs(value) ||
		strings.HasPrefix(value, "./") || path.Clean(value) != value {
		return ErrObjectKey
	}
	for _, part := range strings.Split(value, "/") {
		if part == "" || part == "." || part == ".." {
			return ErrObjectKey
		}
	}
	return nil
}
