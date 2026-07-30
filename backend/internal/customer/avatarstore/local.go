package avatarstore

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	customerdomain "github.com/samson/customer-manage-platform/backend/internal/customer"
)

const (
	contentFileName  = "content"
	metadataFileName = "metadata.json"
)

type Local struct{ root string }

func NewLocal(root string) (*Local, error) {
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, fmt.Errorf("resolve avatar local root: %w", err)
	}
	local := &Local{root: realRoot}
	if err := local.cleanupStaleTemps(time.Now().Add(-time.Hour)); err != nil {
		return nil, err
	}
	return local, nil
}

func (s *Local) cleanupStaleTemps(olderThan time.Time) error {
	return filepath.WalkDir(s.root, func(current string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), ".avatar-tmp-") {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.ModTime().Before(olderThan) {
			if err := os.RemoveAll(current); err != nil {
				return err
			}
			return fs.SkipDir
		}
		return nil
	})
}

func (s *Local) PutImmutable(
	ctx context.Context,
	key string,
	body customerdomain.AvatarContent,
	expected customerdomain.ObjectMeta,
) (customerdomain.PutResult, error) {
	if err := ctx.Err(); err != nil {
		return customerdomain.PutResult{}, err
	}
	if expected.MediaType != body.MediaType() || expected.Size != body.Size() || expected.Checksum != body.Checksum() {
		return customerdomain.PutResult{}, customerdomain.ErrAvatarObjectIntegrity
	}
	finalDir, err := s.objectDir(key)
	if err != nil {
		return customerdomain.PutResult{}, err
	}
	if meta, err := s.Stat(ctx, key); err == nil {
		if sameMeta(meta, expected) {
			return customerdomain.PutResult{Meta: meta, Created: false}, nil
		}
		return customerdomain.PutResult{}, customerdomain.ErrAvatarObjectIntegrity
	} else if !errors.Is(err, customerdomain.ErrAvatarObjectNotFound) {
		return customerdomain.PutResult{}, err
	}

	parent := filepath.Dir(finalDir)
	if err := os.MkdirAll(parent, 0o750); err != nil {
		return customerdomain.PutResult{}, temporaryError("create avatar generation parent", err)
	}
	tempDir, err := os.MkdirTemp(parent, ".avatar-tmp-")
	if err != nil {
		return customerdomain.PutResult{}, temporaryError("create avatar temp generation", err)
	}
	defer func() { _ = os.RemoveAll(tempDir) }()
	if err := writeDurableFile(filepath.Join(tempDir, contentFileName), body.Bytes()); err != nil {
		return customerdomain.PutResult{}, err
	}
	metadata, err := json.Marshal(expected)
	if err != nil {
		return customerdomain.PutResult{}, fmt.Errorf("encode avatar metadata: %w", err)
	}
	if err := writeDurableFile(filepath.Join(tempDir, metadataFileName), metadata); err != nil {
		return customerdomain.PutResult{}, err
	}
	if err := syncDir(tempDir); err != nil {
		return customerdomain.PutResult{}, temporaryError("sync avatar temp generation", err)
	}
	if err := os.Rename(tempDir, finalDir); err != nil {
		if meta, statErr := s.Stat(ctx, key); statErr == nil && sameMeta(meta, expected) {
			return customerdomain.PutResult{Meta: meta, Created: false}, nil
		}
		return customerdomain.PutResult{}, temporaryError("publish avatar generation", err)
	}
	if err := syncDir(parent); err != nil {
		return customerdomain.PutResult{}, temporaryError("sync avatar generation parent", err)
	}
	return customerdomain.PutResult{Meta: expected, Created: true}, nil
}

func (s *Local) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	if _, err := s.Stat(ctx, key); err != nil {
		return nil, err
	}
	dir, _ := s.objectDir(key)
	file, err := os.Open(filepath.Join(dir, contentFileName))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, customerdomain.ErrAvatarObjectNotFound
	}
	if err != nil {
		return nil, temporaryError("open avatar content", err)
	}
	return file, nil
}

func (s *Local) Stat(ctx context.Context, key string) (customerdomain.ObjectMeta, error) {
	if err := ctx.Err(); err != nil {
		return customerdomain.ObjectMeta{}, err
	}
	dir, err := s.objectDir(key)
	if err != nil {
		return customerdomain.ObjectMeta{}, err
	}
	metadata, err := os.ReadFile(filepath.Join(dir, metadataFileName))
	if errors.Is(err, fs.ErrNotExist) {
		return customerdomain.ObjectMeta{}, customerdomain.ErrAvatarObjectNotFound
	}
	if err != nil {
		return customerdomain.ObjectMeta{}, temporaryError("read avatar metadata", err)
	}
	var meta customerdomain.ObjectMeta
	if err := json.Unmarshal(metadata, &meta); err != nil {
		return customerdomain.ObjectMeta{}, customerdomain.ErrAvatarObjectIntegrity
	}
	content, err := os.ReadFile(filepath.Join(dir, contentFileName))
	if errors.Is(err, fs.ErrNotExist) {
		return customerdomain.ObjectMeta{}, customerdomain.ErrAvatarObjectIntegrity
	}
	if err != nil {
		return customerdomain.ObjectMeta{}, temporaryError("read avatar content", err)
	}
	if len(content) == 0 || len(content) > customerdomain.MaxAvatarContentBytes || int64(len(content)) != meta.Size {
		return customerdomain.ObjectMeta{}, customerdomain.ErrAvatarObjectIntegrity
	}
	digest := sha256.Sum256(content)
	if fmt.Sprintf("sha256-%x", digest) != meta.Checksum {
		return customerdomain.ObjectMeta{}, customerdomain.ErrAvatarObjectIntegrity
	}
	return meta, nil
}

func (s *Local) List(ctx context.Context, prefix, cursor string, limit int) (customerdomain.ObjectPage, error) {
	if limit < 1 || limit > 1000 {
		return customerdomain.ObjectPage{}, customerdomain.ErrAvatarObjectKey
	}
	prefixDir, err := s.prefixDir(prefix)
	if err != nil {
		return customerdomain.ObjectPage{}, err
	}
	keys := make([]string, 0)
	err = filepath.WalkDir(prefixDir, func(current string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if errors.Is(walkErr, fs.ErrNotExist) {
				return nil
			}
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.Type()&fs.ModeSymlink != 0 {
			return customerdomain.ErrAvatarObjectKey
		}
		if entry.IsDir() || entry.Name() != metadataFileName {
			return nil
		}
		rel, err := filepath.Rel(s.root, filepath.Dir(current))
		if err != nil {
			return err
		}
		keys = append(keys, filepath.ToSlash(rel))
		return nil
	})
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return customerdomain.ObjectPage{}, temporaryOrIntegrityError("list avatar objects", err)
	}
	sort.Strings(keys)
	items := make([]customerdomain.ObjectItem, 0, limit)
	for _, key := range keys {
		if key <= cursor {
			continue
		}
		meta, err := s.Stat(ctx, key)
		if err != nil {
			return customerdomain.ObjectPage{}, err
		}
		items = append(items, customerdomain.ObjectItem{Key: key, Meta: meta})
		if len(items) == limit {
			break
		}
	}
	done := len(items) == 0 || items[len(items)-1].Key == lastKeyAfterCursor(keys, cursor)
	nextCursor := ""
	if len(items) > 0 {
		nextCursor = items[len(items)-1].Key
	}
	return customerdomain.ObjectPage{Items: items, NextCursor: nextCursor, Done: done}, nil
}

// Inventory returns every complete physical generation in stable key order.
// It is intended for a stopped application's exact-generation backup gate.
func (s *Local) Inventory(ctx context.Context) ([]customerdomain.ObjectItem, error) {
	const (
		hasContent  = 1
		hasMetadata = 2
	)
	filesByDir := make(map[string]int)
	err := filepath.WalkDir(s.root, func(current string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.IsDir() {
			if strings.HasPrefix(entry.Name(), ".avatar-tmp-") {
				return customerdomain.ErrAvatarObjectIntegrity
			}
			return nil
		}
		switch entry.Name() {
		case contentFileName:
			filesByDir[filepath.Dir(current)] |= hasContent
		case metadataFileName:
			filesByDir[filepath.Dir(current)] |= hasMetadata
		default:
			return customerdomain.ErrAvatarObjectIntegrity
		}
		return nil
	})
	if err != nil {
		return nil, temporaryOrIntegrityError("inventory avatar objects", err)
	}
	items := make([]customerdomain.ObjectItem, 0, len(filesByDir))
	for dir, flags := range filesByDir {
		if flags != hasContent|hasMetadata {
			return nil, customerdomain.ErrAvatarObjectIntegrity
		}
		rel, err := filepath.Rel(s.root, dir)
		if err != nil {
			return nil, temporaryError("derive avatar inventory key", err)
		}
		key := filepath.ToSlash(rel)
		if _, _, _, err := customerdomain.ParseAvatarObjectKey(key); err != nil {
			return nil, err
		}
		meta, err := s.Stat(ctx, key)
		if err != nil {
			return nil, err
		}
		items = append(items, customerdomain.ObjectItem{Key: key, Meta: meta})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Key < items[j].Key })
	return items, nil
}

func (s *Local) Delete(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	dir, err := s.objectDir(key)
	if err != nil {
		return err
	}
	if err := os.RemoveAll(dir); err != nil {
		return temporaryError("delete avatar generation", err)
	}
	if err := syncDir(filepath.Dir(dir)); err != nil {
		return temporaryError("sync avatar delete parent", err)
	}
	if _, err := os.Stat(dir); !errors.Is(err, fs.ErrNotExist) {
		if err == nil {
			return temporaryError("confirm avatar generation deletion", errors.New("generation remains visible"))
		}
		return temporaryError("confirm avatar generation deletion", err)
	}
	return nil
}

func (s *Local) objectDir(key string) (string, error) {
	parts := strings.Split(key, "/")
	if len(parts) != 6 || parts[0] != "avatars" || parts[2] != "customers" {
		return "", customerdomain.ErrAvatarObjectKey
	}
	canonical, err := customerdomain.AvatarObjectKey(parts[1], parts[3], customerdomain.ObjectRef{
		AvatarVersion: parts[4], AvatarObjectID: parts[5],
	})
	if err != nil || canonical != key {
		return "", customerdomain.ErrAvatarObjectKey
	}
	dir, err := s.safeJoin(key)
	if err != nil {
		return "", err
	}
	if err := validateGenerationLeaves(dir); err != nil {
		return "", err
	}
	return dir, nil
}

func (s *Local) prefixDir(prefix string) (string, error) {
	if prefix == "" || path.IsAbs(prefix) || path.Clean(prefix) != prefix || !strings.HasPrefix(prefix, "avatars/") {
		return "", customerdomain.ErrAvatarObjectKey
	}
	return s.safeJoin(prefix)
}

func (s *Local) safeJoin(key string) (string, error) {
	joined := filepath.Join(s.root, filepath.FromSlash(key))
	rel, err := filepath.Rel(s.root, joined)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", customerdomain.ErrAvatarObjectKey
	}
	current := s.root
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return "", temporaryError("inspect avatar object path", err)
		}
		if info.Mode()&fs.ModeSymlink != 0 {
			return "", customerdomain.ErrAvatarObjectKey
		}
	}
	return joined, nil
}

func validateGenerationLeaves(dir string) error {
	for _, name := range []string{contentFileName, metadataFileName} {
		info, err := os.Lstat(filepath.Join(dir, name))
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return temporaryError("inspect avatar generation file", err)
		}
		if info.Mode()&fs.ModeSymlink != 0 {
			return customerdomain.ErrAvatarObjectKey
		}
		if !info.Mode().IsRegular() {
			return customerdomain.ErrAvatarObjectIntegrity
		}
	}
	return nil
}

func writeDurableFile(path string, content []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return temporaryError("create avatar generation file", err)
	}
	if _, err := file.Write(content); err != nil {
		_ = file.Close()
		return temporaryError("write avatar generation file", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return temporaryError("sync avatar generation file", err)
	}
	if err := file.Close(); err != nil {
		return temporaryError("close avatar generation file", err)
	}
	return nil
}

func syncDir(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = dir.Close() }()
	return dir.Sync()
}

func temporaryError(operation string, _ error) error {
	return fmt.Errorf("%w: %s", customerdomain.ErrAvatarObjectTemporary, operation)
}

func temporaryOrIntegrityError(operation string, err error) error {
	if errors.Is(err, customerdomain.ErrAvatarObjectIntegrity) ||
		errors.Is(err, customerdomain.ErrAvatarObjectKey) || errors.Is(err, context.Canceled) ||
		errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return temporaryError(operation, err)
}

func sameMeta(left, right customerdomain.ObjectMeta) bool {
	return left.MediaType == right.MediaType && left.Size == right.Size && left.Checksum == right.Checksum
}

func lastKeyAfterCursor(keys []string, cursor string) string {
	for i := len(keys) - 1; i >= 0; i-- {
		if keys[i] > cursor {
			return keys[i]
		}
	}
	return ""
}
