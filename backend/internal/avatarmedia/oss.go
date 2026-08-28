package avatarmedia

import (
	"context"
	"errors"
	"io"

	"github.com/samson/customer-manage-platform/backend/internal/platform/immutablefs"
)

// avatarNamespacePrefix 是头像对象在共享 bucket 中的键前缀；
// OSSStore 的 Inventory 只扫描该命名空间，避免把其他域的对象卷入 fail-closed 校验。
const avatarNamespacePrefix = "avatars/"

// OSSStore 把 immutablefs.ObjectStore（string key）适配为 typed avatarmedia 端口。
// 键校验（ParseKey fail closed）与 zero-prefix 拒绝在本层完成，底座只负责对象语义。
type OSSStore struct {
	base immutablefs.ObjectStore
}

var (
	_ ObjectStore     = (*OSSStore)(nil)
	_ ObjectInventory = (*OSSStore)(nil)
)

// NewOSSStore 创建头像 typed adapter；nil base 会被拒绝，避免运行期 panic。
func NewOSSStore(base immutablefs.ObjectStore) (*OSSStore, error) {
	if base == nil {
		return nil, errors.New("avatarmedia OSS base store is nil")
	}
	return &OSSStore{base: base}, nil
}

// PutImmutable 校验 typed key/content 后写入底层不可变对象存储。
func (s *OSSStore) PutImmutable(ctx context.Context, key Key, body Content, expected ObjectMeta) (PutResult, error) {
	if err := parseObjectKey(key); err != nil {
		return PutResult{}, err
	}
	// base 层只校验字节（size/checksum），MediaType 一致性是 typed 端口契约。
	if expected.MediaType != body.MediaType() || expected.Size != body.Size() || expected.Checksum != body.Checksum() {
		return PutResult{}, ErrObjectIntegrity
	}
	meta, created, err := s.base.PutImmutable(ctx, key.value, body.Bytes(), toBaseMetadata(expected))
	if err != nil {
		return PutResult{}, mapBaseError(err)
	}
	return PutResult{Meta: fromBaseMetadata(meta), Created: created}, nil
}

// Open 打开 typed 头像对象；调用方负责关闭返回流。
func (s *OSSStore) Open(ctx context.Context, key Key) (io.ReadCloser, error) {
	if err := parseObjectKey(key); err != nil {
		return nil, err
	}
	reader, err := s.base.Open(ctx, key.value)
	if err != nil {
		return nil, mapBaseError(err)
	}
	return reader, nil
}

// Stat 读取 typed 头像对象元数据。
func (s *OSSStore) Stat(ctx context.Context, key Key) (ObjectMeta, error) {
	if err := parseObjectKey(key); err != nil {
		return ObjectMeta{}, err
	}
	meta, err := s.base.Stat(ctx, key.value)
	if err != nil {
		return ObjectMeta{}, mapBaseError(err)
	}
	return fromBaseMetadata(meta), nil
}

// List 按 typed prefix/cursor 返回稳定 keyset 页面。
func (s *OSSStore) List(ctx context.Context, prefix Prefix, cursor Cursor, limit int) (ObjectPage, error) {
	if prefix.IsZero() {
		return ObjectPage{}, ErrObjectKey
	}
	if _, err := ParsePrefix(prefix.value); err != nil {
		return ObjectPage{}, ErrObjectKey
	}
	page, err := s.base.List(ctx, prefix.value, cursor.value, limit)
	if err != nil {
		return ObjectPage{}, mapBaseError(err)
	}
	items := make([]ObjectItem, 0, len(page.Items))
	for _, item := range page.Items {
		parsed, err := ParseKey(item.Key)
		if err != nil {
			return ObjectPage{}, err
		}
		items = append(items, ObjectItem{Key: parsed.Key, Meta: fromBaseMetadata(item.Meta)})
	}
	next := Cursor{}
	if page.NextCursor != "" {
		next = CursorFromRaw(page.NextCursor)
	}
	return ObjectPage{Items: items, NextCursor: next, Done: page.Done}, nil
}

// Delete 幂等删除 typed 头像对象。
func (s *OSSStore) Delete(ctx context.Context, key Key) error {
	if err := parseObjectKey(key); err != nil {
		return err
	}
	if err := s.base.Delete(ctx, key.value); err != nil {
		return mapBaseError(err)
	}
	return nil
}

// Inventory 只枚举 avatars/ 命名空间并逐键 ParseKey 归类，语义与
// avatarmedia.Local.Inventory 对齐；共享 bucket 下其他前缀不进入结果。
func (s *OSSStore) Inventory(ctx context.Context) ([]ObjectItem, error) {
	items := make([]ObjectItem, 0)
	cursor := ""
	for {
		page, err := s.base.List(ctx, avatarNamespacePrefix, cursor, 1000)
		if err != nil {
			return nil, mapBaseError(err)
		}
		for _, item := range page.Items {
			parsed, err := ParseKey(item.Key)
			if err != nil {
				return nil, err
			}
			items = append(items, ObjectItem{Key: parsed.Key, Meta: fromBaseMetadata(item.Meta)})
		}
		if page.Done || page.NextCursor == "" {
			return items, nil
		}
		cursor = page.NextCursor
	}
}

func parseObjectKey(key Key) error {
	if _, err := ParseKey(key.value); err != nil {
		return ErrObjectKey
	}
	return nil
}

func mapBaseError(err error) error {
	switch {
	case errors.Is(err, immutablefs.ErrNotFound):
		return ErrObjectNotFound
	case errors.Is(err, immutablefs.ErrIntegrity):
		return ErrObjectIntegrity
	case errors.Is(err, immutablefs.ErrInvalidKey):
		return ErrObjectKey
	case errors.Is(err, immutablefs.ErrTemporary):
		return ErrObjectTemporary
	default:
		return err
	}
}

func toBaseMetadata(meta ObjectMeta) immutablefs.Metadata {
	return immutablefs.Metadata{
		MediaType:  meta.MediaType,
		Size:       meta.Size,
		Checksum:   meta.Checksum,
		ModifiedAt: meta.ModifiedAt,
	}
}

func fromBaseMetadata(meta immutablefs.Metadata) ObjectMeta {
	return ObjectMeta{
		MediaType:  meta.MediaType,
		Size:       meta.Size,
		Checksum:   meta.Checksum,
		ModifiedAt: meta.ModifiedAt,
	}
}
