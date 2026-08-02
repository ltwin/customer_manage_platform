package avatarmedia

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	contentFileName  = "content"
	metadataFileName = "metadata.json"
)

// Local 是首版本地卷 ObjectStore + typed Inventory。
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
	key Key,
	body Content,
	expected ObjectMeta,
) (PutResult, error) {
	if err := ctx.Err(); err != nil {
		return PutResult{}, err
	}
	if expected.MediaType != body.MediaType() || expected.Size != body.Size() || expected.Checksum != body.Checksum() {
		return PutResult{}, ErrObjectIntegrity
	}
	finalDir, err := s.objectDir(key)
	if err != nil {
		return PutResult{}, err
	}
	if meta, err := s.Stat(ctx, key); err == nil {
		if SameObjectMeta(meta, expected) {
			return PutResult{Meta: meta, Created: false}, nil
		}
		return PutResult{}, ErrObjectIntegrity
	} else if !errors.Is(err, ErrObjectNotFound) {
		return PutResult{}, err
	}

	parent := filepath.Dir(finalDir)
	if err := os.MkdirAll(parent, 0o750); err != nil {
		return PutResult{}, temporaryError("create avatar generation parent", err)
	}
	tempDir, err := os.MkdirTemp(parent, ".avatar-tmp-")
	if err != nil {
		return PutResult{}, temporaryError("create avatar temp generation", err)
	}
	defer func() { _ = os.RemoveAll(tempDir) }()
	if err := writeDurableFile(filepath.Join(tempDir, contentFileName), body.Bytes()); err != nil {
		return PutResult{}, err
	}
	metadata, err := json.Marshal(expected)
	if err != nil {
		return PutResult{}, fmt.Errorf("encode avatar metadata: %w", err)
	}
	if err := writeDurableFile(filepath.Join(tempDir, metadataFileName), metadata); err != nil {
		return PutResult{}, err
	}
	if err := syncDir(tempDir); err != nil {
		return PutResult{}, temporaryError("sync avatar temp generation", err)
	}
	if err := os.Rename(tempDir, finalDir); err != nil {
		if meta, statErr := s.Stat(ctx, key); statErr == nil && SameObjectMeta(meta, expected) {
			return PutResult{Meta: meta, Created: false}, nil
		}
		return PutResult{}, temporaryError("publish avatar generation", err)
	}
	if err := syncDir(parent); err != nil {
		return PutResult{}, temporaryError("sync avatar generation parent", err)
	}
	return PutResult{Meta: expected, Created: true}, nil
}

func (s *Local) Open(ctx context.Context, key Key) (io.ReadCloser, error) {
	if _, err := s.Stat(ctx, key); err != nil {
		return nil, err
	}
	dir, _ := s.objectDir(key)
	file, err := os.Open(filepath.Join(dir, contentFileName))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, ErrObjectNotFound
	}
	if err != nil {
		return nil, temporaryError("open avatar content", err)
	}
	return file, nil
}

func (s *Local) Stat(ctx context.Context, key Key) (ObjectMeta, error) {
	if err := ctx.Err(); err != nil {
		return ObjectMeta{}, err
	}
	dir, err := s.objectDir(key)
	if err != nil {
		return ObjectMeta{}, err
	}
	metadata, err := os.ReadFile(filepath.Join(dir, metadataFileName))
	if errors.Is(err, fs.ErrNotExist) {
		return ObjectMeta{}, ErrObjectNotFound
	}
	if err != nil {
		return ObjectMeta{}, temporaryError("read avatar metadata", err)
	}
	var meta ObjectMeta
	if err := json.Unmarshal(metadata, &meta); err != nil {
		return ObjectMeta{}, ErrObjectIntegrity
	}
	content, err := os.ReadFile(filepath.Join(dir, contentFileName))
	if errors.Is(err, fs.ErrNotExist) {
		return ObjectMeta{}, ErrObjectIntegrity
	}
	if err != nil {
		return ObjectMeta{}, temporaryError("read avatar content", err)
	}
	if len(content) == 0 || len(content) > MaxContentBytes || int64(len(content)) != meta.Size {
		return ObjectMeta{}, ErrObjectIntegrity
	}
	digest := sha256.Sum256(content)
	if fmt.Sprintf("sha256-%x", digest) != meta.Checksum {
		return ObjectMeta{}, ErrObjectIntegrity
	}
	return meta, nil
}

func (s *Local) List(ctx context.Context, prefix Prefix, cursor Cursor, limit int) (ObjectPage, error) {
	if limit < 1 || limit > 1000 {
		return ObjectPage{}, ErrObjectKey
	}
	if prefix.IsZero() {
		return ObjectPage{}, ErrObjectKey
	}
	prefixDir, err := s.safeJoin(prefix.value)
	if err != nil {
		return ObjectPage{}, err
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
			return ErrObjectKey
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
		return ObjectPage{}, temporaryOrIntegrityError("list avatar objects", err)
	}
	sort.Strings(keys)
	items := make([]ObjectItem, 0, limit)
	cursorValue := cursor.value
	for _, raw := range keys {
		if raw <= cursorValue {
			continue
		}
		parsed, err := ParseKey(raw)
		if err != nil {
			return ObjectPage{}, err
		}
		meta, err := s.Stat(ctx, parsed.Key)
		if err != nil {
			return ObjectPage{}, err
		}
		items = append(items, ObjectItem{Key: parsed.Key, Meta: meta})
		if len(items) == limit {
			break
		}
	}
	done := len(items) == 0 || items[len(items)-1].Key.value == lastKeyAfterCursor(keys, cursorValue)
	next := Cursor{}
	if len(items) > 0 {
		next = CursorFromKey(items[len(items)-1].Key)
	}
	return ObjectPage{Items: items, NextCursor: next, Done: done}, nil
}

// Inventory 返回每个完整物理代次；ParseKey 归类；新鲜 `.avatar-tmp-*` → integrity。
func (s *Local) Inventory(ctx context.Context) ([]ObjectItem, error) {
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
				return ErrObjectIntegrity
			}
			return nil
		}
		switch entry.Name() {
		case contentFileName:
			filesByDir[filepath.Dir(current)] |= hasContent
		case metadataFileName:
			filesByDir[filepath.Dir(current)] |= hasMetadata
		default:
			return ErrObjectIntegrity
		}
		return nil
	})
	if err != nil {
		return nil, temporaryOrIntegrityError("inventory avatar objects", err)
	}
	items := make([]ObjectItem, 0, len(filesByDir))
	for dir, flags := range filesByDir {
		if flags != hasContent|hasMetadata {
			return nil, ErrObjectIntegrity
		}
		rel, err := filepath.Rel(s.root, dir)
		if err != nil {
			return nil, temporaryError("derive avatar inventory key", err)
		}
		parsed, err := ParseKey(filepath.ToSlash(rel))
		if err != nil {
			return nil, err
		}
		meta, err := s.Stat(ctx, parsed.Key)
		if err != nil {
			return nil, err
		}
		items = append(items, ObjectItem{Key: parsed.Key, Meta: meta})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Key.value < items[j].Key.value })
	return items, nil
}

func (s *Local) Delete(ctx context.Context, key Key) error {
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

func (s *Local) objectDir(key Key) (string, error) {
	if _, err := ParseKey(key.value); err != nil {
		return "", ErrObjectKey
	}
	dir, err := s.safeJoin(key.value)
	if err != nil {
		return "", err
	}
	if err := validateGenerationLeaves(dir); err != nil {
		return "", err
	}
	return dir, nil
}

func (s *Local) safeJoin(key string) (string, error) {
	joined := filepath.Join(s.root, filepath.FromSlash(key))
	rel, err := filepath.Rel(s.root, joined)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", ErrObjectKey
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
			return "", ErrObjectKey
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
			return ErrObjectKey
		}
		if !info.Mode().IsRegular() {
			return ErrObjectIntegrity
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
	return fmt.Errorf("%w: %s", ErrObjectTemporary, operation)
}

func temporaryOrIntegrityError(operation string, err error) error {
	if errors.Is(err, ErrObjectIntegrity) ||
		errors.Is(err, ErrObjectKey) || errors.Is(err, context.Canceled) ||
		errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return temporaryError(operation, err)
}

func lastKeyAfterCursor(keys []string, cursor string) string {
	for i := len(keys) - 1; i >= 0; i-- {
		if keys[i] > cursor {
			return keys[i]
		}
	}
	return ""
}
