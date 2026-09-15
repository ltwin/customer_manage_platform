package versionedfs

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Local keeps staging parts and published blobs under one private root. Part
// writes arrive only through the authenticated API (LocalPartWriter); the
// adapter never serves arbitrary filesystem paths.
type Local struct {
	root string
	sign func(key, session string, part int, expires time.Time) PartAuthorization
}

func NewLocal(root string, sign func(key, session string, part int, expires time.Time) PartAuthorization) (*Local, error) {
	if root == "" || sign == nil {
		return nil, errors.New("creative media local root and part signer are required")
	}
	if err := os.MkdirAll(root, 0o750); err != nil {
		return nil, fmt.Errorf("create creative media root: %w", err)
	}
	real, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, fmt.Errorf("resolve creative media root: %w", err)
	}
	return &Local{root: real, sign: sign}, nil
}

func (l *Local) Driver() string { return "local" }
func (l *Local) Bucket() string { return "" }

func validKey(key string) error {
	if key == "" || strings.Contains(key, "\\") || path.IsAbs(key) || path.Clean(key) != key || strings.HasPrefix(key, "../") || key == "." {
		return fmt.Errorf("%w: invalid object key", ErrState)
	}
	return nil
}
func (l *Local) dir(key string) (string, error) {
	if err := validKey(key); err != nil {
		return "", err
	}
	return filepath.Join(l.root, filepath.FromSlash(key)), nil
}
func validSession(session string) error {
	if _, err := uuid.Parse(session); err != nil {
		return fmt.Errorf("%w: invalid multipart session", ErrState)
	}
	return nil
}

func (l *Local) InitMultipart(ctx context.Context, key, _ string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	dir, err := l.dir(key)
	if err != nil {
		return "", err
	}
	session := uuid.NewString()
	if err := os.MkdirAll(filepath.Join(dir, "sessions", session), 0o750); err != nil {
		return "", fmt.Errorf("init local multipart: %w", err)
	}
	return session, nil
}
func (l *Local) AuthorizePart(ctx context.Context, key, session string, part int, expires time.Time) (PartAuthorization, error) {
	if err := ctx.Err(); err != nil {
		return PartAuthorization{}, err
	}
	if err := validKey(key); err != nil {
		return PartAuthorization{}, err
	}
	if err := validSession(session); err != nil {
		return PartAuthorization{}, err
	}
	return l.sign(key, session, part, expires), nil
}
func (l *Local) WritePart(ctx context.Context, key, session string, part int, body io.Reader, limit int64) (Part, error) {
	if err := ctx.Err(); err != nil {
		return Part{}, err
	}
	dir, err := l.dir(key)
	if err != nil {
		return Part{}, err
	}
	if err := validSession(session); err != nil {
		return Part{}, err
	}
	sessionDir := filepath.Join(dir, "sessions", session)
	if _, err := os.Stat(sessionDir); err != nil {
		return Part{}, ErrNotFound
	}
	if part < 1 || part > 10000 {
		return Part{}, fmt.Errorf("%w: part number", ErrState)
	}
	tmp, err := os.CreateTemp(sessionDir, ".part-")
	if err != nil {
		return Part{}, fmt.Errorf("write local part: %w", err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	hasher := sha256.New()
	size, err := io.Copy(io.MultiWriter(tmp, hasher), io.LimitReader(body, limit+1))
	if err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return Part{}, fmt.Errorf("write local part: %w", err)
	}
	if size == 0 || size > limit {
		return Part{}, ErrSizeLimit
	}
	etag := hex.EncodeToString(hasher.Sum(nil))
	// The last complete write of a part wins, exactly like an object store.
	if err := os.Rename(tmp.Name(), filepath.Join(sessionDir, strconv.Itoa(part))); err != nil {
		return Part{}, fmt.Errorf("publish local part: %w", err)
	}
	return Part{Number: part, ETag: etag, Size: size}, nil
}
func (l *Local) ListParts(ctx context.Context, key, session string) ([]Part, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	dir, err := l.dir(key)
	if err != nil {
		return nil, err
	}
	if err := validSession(session); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(filepath.Join(dir, "sessions", session))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	parts := []Part{}
	for _, entry := range entries {
		number, err := strconv.Atoi(entry.Name())
		if err != nil || entry.IsDir() {
			continue
		}
		p, err := l.readPart(filepath.Join(dir, "sessions", session, entry.Name()), number)
		if err != nil {
			return nil, err
		}
		parts = append(parts, p)
	}
	sort.Slice(parts, func(i, j int) bool { return parts[i].Number < parts[j].Number })
	return parts, nil
}
func (l *Local) readPart(name string, number int) (Part, error) {
	f, err := os.Open(name)
	if err != nil {
		return Part{}, err
	}
	defer func() { _ = f.Close() }()
	hasher := sha256.New()
	size, err := io.Copy(hasher, f)
	if err != nil {
		return Part{}, err
	}
	return Part{Number: number, ETag: hex.EncodeToString(hasher.Sum(nil)), Size: size}, nil
}
func (l *Local) CompleteMultipart(ctx context.Context, key, session string, parts []Part) (string, error) {
	dir, err := l.dir(key)
	if err != nil {
		return "", err
	}
	actual, err := l.ListParts(ctx, key, session)
	if err != nil {
		return "", err
	}
	if len(actual) != len(parts) {
		return "", fmt.Errorf("%w: parts mismatch", ErrState)
	}
	for i := range parts {
		if actual[i].Number != parts[i].Number || actual[i].ETag != parts[i].ETag {
			return "", fmt.Errorf("%w: part %d changed", ErrState, parts[i].Number)
		}
	}
	tmp, err := os.CreateTemp(dir, ".object-")
	if err != nil {
		return "", fmt.Errorf("assemble local object: %w", err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	hasher := sha256.New()
	for _, p := range actual {
		if err := ctx.Err(); err != nil {
			_ = tmp.Close()
			return "", err
		}
		f, err := os.Open(filepath.Join(dir, "sessions", session, strconv.Itoa(p.Number)))
		if err != nil {
			_ = tmp.Close()
			return "", err
		}
		_, err = io.Copy(io.MultiWriter(tmp, hasher), f)
		_ = f.Close()
		if err != nil {
			_ = tmp.Close()
			return "", fmt.Errorf("assemble local object: %w", err)
		}
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	version := hex.EncodeToString(hasher.Sum(nil))
	if err := os.MkdirAll(filepath.Join(dir, "versions"), 0o750); err != nil {
		return "", err
	}
	target := filepath.Join(dir, "versions", version)
	if _, err := os.Stat(target); err == nil {
		return version, nil // Same bytes already fixed; completion is idempotent.
	}
	if err := os.Rename(tmp.Name(), target); err != nil {
		return "", fmt.Errorf("fix local object version: %w", err)
	}
	return version, nil
}
func (l *Local) AbortMultipart(ctx context.Context, key, session string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	dir, err := l.dir(key)
	if err != nil {
		return err
	}
	if err := validSession(session); err != nil {
		return err
	}
	return os.RemoveAll(filepath.Join(dir, "sessions", session))
}
func validVersion(version string) error {
	if len(version) != 64 {
		return ErrNotFound
	}
	if _, err := hex.DecodeString(version); err != nil {
		return ErrNotFound
	}
	return nil
}
func (l *Local) StatVersion(ctx context.Context, key, version string) (ObjectStat, error) {
	if err := ctx.Err(); err != nil {
		return ObjectStat{}, err
	}
	dir, err := l.dir(key)
	if err != nil {
		return ObjectStat{}, err
	}
	if err := validVersion(version); err != nil {
		return ObjectStat{}, err
	}
	info, err := os.Stat(filepath.Join(dir, "versions", version))
	if errors.Is(err, fs.ErrNotExist) {
		return ObjectStat{}, ErrNotFound
	}
	if err != nil {
		return ObjectStat{}, err
	}
	return ObjectStat{Size: info.Size(), Version: version}, nil
}
func (l *Local) OpenVersion(ctx context.Context, key, version string, rng *ByteRange) (io.ReadCloser, ObjectStat, error) {
	stat, err := l.StatVersion(ctx, key, version)
	if err != nil {
		return nil, ObjectStat{}, err
	}
	dir, _ := l.dir(key)
	f, err := os.Open(filepath.Join(dir, "versions", version))
	if err != nil {
		return nil, ObjectStat{}, err
	}
	if rng == nil {
		return f, stat, nil
	}
	if rng.Start < 0 || rng.End < rng.Start || rng.End >= stat.Size {
		_ = f.Close()
		return nil, ObjectStat{}, ErrRange
	}
	if _, err := f.Seek(rng.Start, io.SeekStart); err != nil {
		_ = f.Close()
		return nil, ObjectStat{}, err
	}
	return struct {
		io.Reader
		io.Closer
	}{io.LimitReader(f, rng.End-rng.Start+1), f}, stat, nil
}
func (l *Local) PublishVerified(ctx context.Context, key string, body io.Reader, size int64, _ string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	dir, err := l.dir(key)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Join(dir, "versions"), 0o750); err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(dir, ".publish-")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	hasher := sha256.New()
	written, err := io.Copy(io.MultiWriter(tmp, hasher), io.LimitReader(body, size+1))
	if err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return "", err
	}
	if written != size {
		return "", fmt.Errorf("%w: published size mismatch", ErrState)
	}
	version := hex.EncodeToString(hasher.Sum(nil))
	target := filepath.Join(dir, "versions", version)
	if _, err := os.Stat(target); err == nil {
		return version, nil
	}
	if err := os.Rename(tmp.Name(), target); err != nil {
		return "", err
	}
	return version, nil
}
func (l *Local) DeleteExact(ctx context.Context, key, version string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	dir, err := l.dir(key)
	if err != nil {
		return err
	}
	if err := validVersion(version); err != nil {
		return err
	}
	err = os.Remove(filepath.Join(dir, "versions", version))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}
