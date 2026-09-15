// Package versionedfstest holds the conformance suite every versionedfs
// adapter must pass, plus an in-memory adapter to run it against.
package versionedfstest

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/samson/customer-manage-platform/backend/internal/platform/versionedfs"
)

// Memory is an adapter whose version identity is fixed per write: publishing
// identical bytes to one key twice produces two distinct versions that are
// deleted independently.
//
// It exists because that property has consequences and the local adapter cannot
// express it. Local names a version by its content hash, so two publishes of
// the same bytes converge on one object — which makes any domain branch that
// only runs when two writes of one declared file disagree unreachable, and any
// delete of "my own copy" a delete of the other writer's copy too. A suite that
// only ever ran against local would be measuring one of the two possible
// storage contracts while claiming to measure the port.
type Memory struct {
	driver   string
	mu       sync.Mutex
	counter  int
	objects  map[string]map[string][]byte // key -> version -> bytes
	sessions map[string]map[int][]byte    // key\x00session -> part -> bytes
}

// NewMemory takes the driver name it should report. A caller that records where
// a version lives may constrain that name — the skill schema enumerates the
// deployment's real drivers — and a test standing in for a specific one says so
// at the call site. Reporting "oss" is accurate rather than a convenience:
// this adapter models per-write version identity, which is what OSS has on the
// versioning-enabled bucket the adapter requires (docs/dev/object-storage.md).
func NewMemory(driver string) *Memory {
	return &Memory{
		driver:   driver,
		objects:  map[string]map[string][]byte{},
		sessions: map[string]map[int][]byte{},
	}
}

func (m *Memory) Driver() string { return m.driver }
func (m *Memory) Bucket() string { return "memory" }

// ObjectCount reports how many object versions the store holds, across keys. It
// answers "did anything get left behind" without a caller having to rebuild the
// server-internal keys it is not supposed to know.
func (m *Memory) ObjectCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	total := 0
	for _, versions := range m.objects {
		total += len(versions)
	}
	return total
}

// The key rules mirror the real adapters: a server-produced relative path.
func validKey(key string) error {
	if key == "" || key[0] == '/' || bytes.Contains([]byte(key), []byte("..")) {
		return fmt.Errorf("%w: invalid object key", versionedfs.ErrState)
	}
	return nil
}

func sessionRef(key, session string) string { return key + "\x00" + session }

func (m *Memory) InitMultipart(ctx context.Context, key, _ string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err := validKey(key); err != nil {
		return "", err
	}
	session := uuid.NewString()
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessions[sessionRef(key, session)] = map[int][]byte{}
	return session, nil
}

func (m *Memory) AuthorizePart(ctx context.Context, key, session string, part int, expires time.Time) (versionedfs.PartAuthorization, error) {
	if err := ctx.Err(); err != nil {
		return versionedfs.PartAuthorization{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.sessions[sessionRef(key, session)]; !ok {
		return versionedfs.PartAuthorization{}, versionedfs.ErrState
	}
	// The URL is a placeholder: parts reach this adapter through WritePart, the
	// same seam the local adapter uses, never over the network.
	return versionedfs.PartAuthorization{
		Number: part, Method: "PUT", URL: "memory://" + key, ExpiresAt: expires,
	}, nil
}

// WritePart satisfies versionedfs.LocalPartWriter so the suite can drive the
// staging half of the port without a signed-URL transport.
func (m *Memory) WritePart(ctx context.Context, key, session string, part int, body io.Reader, limit int64) (versionedfs.Part, error) {
	if err := ctx.Err(); err != nil {
		return versionedfs.Part{}, err
	}
	payload, err := io.ReadAll(io.LimitReader(body, limit+1))
	if err != nil {
		return versionedfs.Part{}, err
	}
	if int64(len(payload)) > limit {
		return versionedfs.Part{}, versionedfs.ErrSizeLimit
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	parts, ok := m.sessions[sessionRef(key, session)]
	if !ok {
		return versionedfs.Part{}, versionedfs.ErrState
	}
	parts[part] = payload
	sum := sha256.Sum256(payload)
	return versionedfs.Part{Number: part, ETag: hex.EncodeToString(sum[:]), Size: int64(len(payload))}, nil
}

func (m *Memory) ListParts(ctx context.Context, key, session string) ([]versionedfs.Part, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	parts, ok := m.sessions[sessionRef(key, session)]
	if !ok {
		return nil, versionedfs.ErrState
	}
	numbers := make([]int, 0, len(parts))
	for number := range parts {
		numbers = append(numbers, number)
	}
	sort.Ints(numbers)
	list := make([]versionedfs.Part, 0, len(numbers))
	for _, number := range numbers {
		sum := sha256.Sum256(parts[number])
		list = append(list, versionedfs.Part{
			Number: number, ETag: hex.EncodeToString(sum[:]), Size: int64(len(parts[number])),
		})
	}
	return list, nil
}

func (m *Memory) CompleteMultipart(ctx context.Context, key, session string, parts []versionedfs.Part) (string, error) {
	actual, err := m.ListParts(ctx, key, session)
	if err != nil {
		return "", err
	}
	if len(actual) != len(parts) {
		return "", fmt.Errorf("%w: parts mismatch", versionedfs.ErrState)
	}
	for i := range parts {
		if actual[i].Number != parts[i].Number || actual[i].ETag != parts[i].ETag {
			return "", fmt.Errorf("%w: part %d changed", versionedfs.ErrState, parts[i].Number)
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	staged := m.sessions[sessionRef(key, session)]
	var assembled []byte
	for _, p := range actual {
		assembled = append(assembled, staged[p.Number]...)
	}
	delete(m.sessions, sessionRef(key, session))
	return m.storeLocked(key, assembled), nil
}

func (m *Memory) AbortMultipart(ctx context.Context, key, session string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, sessionRef(key, session))
	return nil
}

// storeLocked mints the new version. The counter is what makes identity
// per-write: nothing about the bytes takes part in the name.
func (m *Memory) storeLocked(key string, payload []byte) string {
	m.counter++
	version := "v" + strconv.Itoa(m.counter)
	if m.objects[key] == nil {
		m.objects[key] = map[string][]byte{}
	}
	m.objects[key][version] = payload
	return version
}

func (m *Memory) PublishVerified(ctx context.Context, key string, body io.Reader, size int64, _ string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err := validKey(key); err != nil {
		return "", err
	}
	payload, err := io.ReadAll(io.LimitReader(body, size+1))
	if err != nil {
		return "", err
	}
	if int64(len(payload)) != size {
		return "", fmt.Errorf("%w: published size mismatch", versionedfs.ErrState)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.storeLocked(key, payload), nil
}

func (m *Memory) StatVersion(ctx context.Context, key, version string) (versionedfs.ObjectStat, error) {
	if err := ctx.Err(); err != nil {
		return versionedfs.ObjectStat{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	payload, ok := m.objects[key][version]
	if !ok {
		return versionedfs.ObjectStat{}, versionedfs.ErrNotFound
	}
	return versionedfs.ObjectStat{Size: int64(len(payload)), Version: version}, nil
}

func (m *Memory) OpenVersion(ctx context.Context, key, version string, rng *versionedfs.ByteRange) (io.ReadCloser, versionedfs.ObjectStat, error) {
	stat, err := m.StatVersion(ctx, key, version)
	if err != nil {
		return nil, versionedfs.ObjectStat{}, err
	}
	m.mu.Lock()
	payload := m.objects[key][version]
	m.mu.Unlock()
	if rng == nil {
		return io.NopCloser(bytes.NewReader(payload)), stat, nil
	}
	if rng.Start < 0 || rng.End < rng.Start || rng.End >= stat.Size {
		return nil, versionedfs.ObjectStat{}, versionedfs.ErrRange
	}
	return io.NopCloser(bytes.NewReader(payload[rng.Start : rng.End+1])), stat, nil
}

func (m *Memory) DeleteExact(ctx context.Context, key, version string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.objects[key], version)
	return nil
}
