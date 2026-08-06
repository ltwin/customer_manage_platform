// Package immutablefs provides a small, domain-neutral durable adjunct for
// immutable objects.  Callers own key namespaces and metadata semantics.
package immutablefs

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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
)

var (
	ErrNotFound   = errors.New("immutable object not found")
	ErrInvalidKey = errors.New("invalid immutable object key")
	ErrIntegrity  = errors.New("immutable object integrity mismatch")
	ErrTemporary  = errors.New("temporary immutable object error")
)

type Metadata struct {
	MediaType  string    `json:"MediaType"`
	Size       int64     `json:"Size"`
	Checksum   string    `json:"Checksum"`
	ModifiedAt time.Time `json:"ModifiedAt"`
	Width      int       `json:"Width,omitempty"`
	Height     int       `json:"Height,omitempty"`
}

type Item struct {
	Key  string
	Meta Metadata
}

type Page struct {
	Items      []Item
	NextCursor string
	Done       bool
}

type ObjectStore interface {
	PutImmutable(context.Context, string, []byte, Metadata) (Metadata, bool, error)
	Open(context.Context, string) (io.ReadCloser, error)
	Stat(context.Context, string) (Metadata, error)
	List(context.Context, string, string, int) (Page, error)
	Delete(context.Context, string) error
}

type Local struct {
	root       string
	tempPrefix string
}

type LocalOption func(*Local) error

// WithTempPrefix preserves a domain adapter's existing private temporary
// directory convention without giving immutablefs any knowledge of that
// domain's keys or lifecycle.
func WithTempPrefix(prefix string) LocalOption {
	return func(local *Local) error {
		if prefix == "" || prefix == "." || prefix == ".." || strings.ContainsAny(prefix, `/\\`) {
			return ErrInvalidKey
		}
		local.tempPrefix = prefix
		return nil
	}
}

func NewLocal(root string, options ...LocalOption) (*Local, error) {
	if root == "" {
		return nil, ErrInvalidKey
	}
	if err := os.MkdirAll(root, 0o750); err != nil {
		return nil, fmt.Errorf("create immutable root: %w", err)
	}
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, fmt.Errorf("resolve immutable root: %w", err)
	}
	local := &Local{root: realRoot, tempPrefix: ".immutablefs-tmp-"}
	for _, option := range options {
		if option != nil {
			if err := option(local); err != nil {
				return nil, err
			}
		}
	}
	return local, nil
}

func (s *Local) PutImmutable(ctx context.Context, key string, body []byte, expected Metadata) (Metadata, bool, error) {
	if err := ctx.Err(); err != nil {
		return Metadata{}, false, err
	}
	if expected.Size != int64(len(body)) || expected.Size <= 0 || expected.Checksum != checksum(body) {
		return Metadata{}, false, ErrIntegrity
	}
	dir, err := s.objectDir(key)
	if err != nil {
		return Metadata{}, false, err
	}
	if meta, err := s.Stat(ctx, key); err == nil {
		if sameMeta(meta, expected) {
			return meta, false, nil
		}
		return Metadata{}, false, ErrIntegrity
	} else if !errors.Is(err, ErrNotFound) {
		return Metadata{}, false, err
	}
	if err := os.MkdirAll(filepath.Dir(dir), 0o750); err != nil {
		return Metadata{}, false, fmt.Errorf("%w: create parent: %v", ErrTemporary, err)
	}
	tmp, err := os.MkdirTemp(filepath.Dir(dir), s.tempPrefix)
	if err != nil {
		return Metadata{}, false, fmt.Errorf("%w: create temp: %v", ErrTemporary, err)
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	if err := writeFile(filepath.Join(tmp, "content"), body); err != nil {
		return Metadata{}, false, err
	}
	encoded, err := json.Marshal(expected)
	if err != nil {
		return Metadata{}, false, err
	}
	if err := writeFile(filepath.Join(tmp, "metadata.json"), encoded); err != nil {
		return Metadata{}, false, err
	}
	if err := syncDir(tmp); err != nil {
		return Metadata{}, false, err
	}
	if err := os.Rename(tmp, dir); err != nil {
		if meta, statErr := s.Stat(ctx, key); statErr == nil && sameMeta(meta, expected) {
			return meta, false, nil
		}
		return Metadata{}, false, fmt.Errorf("%w: publish: %v", ErrTemporary, err)
	}
	if err := syncDir(filepath.Dir(dir)); err != nil {
		return Metadata{}, false, err
	}
	return expected, true, nil
}

func (s *Local) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	if _, err := s.Stat(ctx, key); err != nil {
		return nil, err
	}
	dir, err := s.objectDir(key)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(filepath.Join(dir, "content"))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("%w: open: %v", ErrTemporary, err)
	}
	return f, nil
}

func (s *Local) Stat(ctx context.Context, key string) (Metadata, error) {
	if err := ctx.Err(); err != nil {
		return Metadata{}, err
	}
	dir, err := s.objectDir(key)
	if err != nil {
		return Metadata{}, err
	}
	metaBytes, err := os.ReadFile(filepath.Join(dir, "metadata.json"))
	if errors.Is(err, fs.ErrNotExist) {
		return Metadata{}, ErrNotFound
	}
	if err != nil {
		return Metadata{}, fmt.Errorf("%w: read metadata: %v", ErrTemporary, err)
	}
	var meta Metadata
	if err := json.Unmarshal(metaBytes, &meta); err != nil {
		return Metadata{}, ErrIntegrity
	}
	body, err := os.ReadFile(filepath.Join(dir, "content"))
	if errors.Is(err, fs.ErrNotExist) {
		return Metadata{}, ErrIntegrity
	}
	if err != nil {
		return Metadata{}, fmt.Errorf("%w: read content: %v", ErrTemporary, err)
	}
	if meta.Size != int64(len(body)) || meta.Size <= 0 || meta.Checksum != checksum(body) {
		return Metadata{}, ErrIntegrity
	}
	return meta, nil
}

func (s *Local) List(ctx context.Context, prefix, cursor string, limit int) (Page, error) {
	if limit < 1 || limit > 1000 {
		return Page{}, ErrInvalidKey
	}
	prefixDir, err := s.safePath(prefix)
	if err != nil {
		return Page{}, err
	}
	keys := make([]string, 0)
	err = filepath.WalkDir(prefixDir, func(current string, entry fs.DirEntry, walkErr error) error {
		if errors.Is(walkErr, fs.ErrNotExist) {
			return nil
		}
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return ErrIntegrity
		}
		if entry.IsDir() || entry.Name() != "metadata.json" {
			return nil
		}
		rel, err := filepath.Rel(s.root, filepath.Dir(current))
		if err != nil {
			return err
		}
		keys = append(keys, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return Page{}, err
	}
	sort.Strings(keys)
	page := Page{Items: make([]Item, 0, limit)}
	for _, key := range keys {
		if key <= cursor {
			continue
		}
		meta, err := s.Stat(ctx, key)
		if err != nil {
			return Page{}, err
		}
		page.Items = append(page.Items, Item{Key: key, Meta: meta})
		if len(page.Items) == limit {
			break
		}
	}
	if len(page.Items) == 0 {
		page.Done = true
	} else {
		page.NextCursor = page.Items[len(page.Items)-1].Key
		page.Done = page.NextCursor == lastAfter(keys, cursor)
	}
	return page, nil
}

func (s *Local) Delete(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	dir, err := s.objectDir(key)
	if err != nil {
		return err
	}
	err = os.RemoveAll(dir)
	if err != nil {
		return fmt.Errorf("%w: remove: %v", ErrTemporary, err)
	}
	if err := syncDir(filepath.Dir(dir)); err != nil {
		return err
	}
	if _, err := os.Stat(dir); !errors.Is(err, fs.ErrNotExist) {
		if err == nil {
			return fmt.Errorf("%w: removed object remains visible", ErrTemporary)
		}
		return fmt.Errorf("%w: confirm remove: %v", ErrTemporary, err)
	}
	return nil
}

func (s *Local) Inventory(ctx context.Context) ([]Item, error) {
	page, err := s.List(ctx, "", "", 1000)
	if err != nil {
		return nil, err
	}
	items := append([]Item(nil), page.Items...)
	for !page.Done {
		page, err = s.List(ctx, "", page.NextCursor, 1000)
		if err != nil {
			return nil, err
		}
		items = append(items, page.Items...)
	}
	return items, nil
}

func (s *Local) OpenVerified(ctx context.Context, key string, expected Metadata) (io.ReadCloser, error) {
	meta, err := s.Stat(ctx, key)
	if err != nil {
		return nil, err
	}
	if !sameMeta(meta, expected) {
		return nil, ErrIntegrity
	}
	return s.Open(ctx, key)
}

func (s *Local) RemoveExact(ctx context.Context, key string, expected Metadata) error {
	meta, err := s.Stat(ctx, key)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if !sameMeta(meta, expected) {
		return ErrIntegrity
	}
	return s.Delete(ctx, key)
}

func (s *Local) objectDir(key string) (string, error) {
	p, err := s.safePath(key)
	if err != nil {
		return "", err
	}
	return p, nil
}
func (s *Local) safePath(key string) (string, error) {
	if key == "" {
		return s.root, nil
	}
	if strings.Contains(key, "\\") || filepath.IsAbs(key) {
		return "", ErrInvalidKey
	}
	clean := path.Clean(key)
	if clean != key {
		return "", ErrInvalidKey
	}
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", ErrInvalidKey
	}
	full := filepath.Join(s.root, filepath.FromSlash(clean))
	rel, err := filepath.Rel(s.root, full)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", ErrInvalidKey
	}
	for current := s.root; current != full; {
		next := filepath.Join(current, strings.SplitN(strings.TrimPrefix(full, current+string(filepath.Separator)), string(filepath.Separator), 2)[0])
		info, err := os.Lstat(next)
		if err == nil && info.Mode()&os.ModeSymlink != 0 {
			return "", ErrInvalidKey
		}
		current = next
	}
	return full, nil
}
func sameMeta(a, b Metadata) bool {
	return a.MediaType == b.MediaType && a.Size == b.Size && a.Checksum == b.Checksum && a.Width == b.Width && a.Height == b.Height
}
func checksum(body []byte) string {
	sum := sha256.Sum256(body)
	return "sha256-" + hex.EncodeToString(sum[:])
}
func writeFile(name string, body []byte) error {
	f, err := os.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("%w: create file: %v", ErrTemporary, err)
	}
	if _, err = f.Write(body); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return fmt.Errorf("%w: write file: %v", ErrTemporary, err)
	}
	return nil
}
func syncDir(name string) error {
	f, err := os.Open(name)
	if err != nil {
		return fmt.Errorf("%w: open dir: %v", ErrTemporary, err)
	}
	err = f.Sync()
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return fmt.Errorf("%w: sync dir: %v", ErrTemporary, err)
	}
	return nil
}
func lastAfter(keys []string, cursor string) string {
	last := ""
	for _, key := range keys {
		if key > cursor {
			last = key
		}
	}
	return last
}
