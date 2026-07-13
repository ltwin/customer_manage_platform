package customer_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	customerdomain "github.com/samson/customer-manage-platform/backend/internal/customer"
)

func TestAvatarApplicationSetNoOpReplaceAndABA(t *testing.T) {
	ctx := context.Background()
	s := openStore(t)
	scope := createAccount(t, s, "acct-avatar")
	seedCustomer(t, scope, seedCustomerInput{
		ID: "cus_avatar_flow", DisplayName: "头像流程", Channel: customerdomain.ChannelOther,
		Status: customerdomain.StatusActive, CreatedAt: time.Now(), Handle: "avatar-flow",
	})
	objects := newMemoryAvatarStore()
	app := customerdomain.NewAvatarApplication(customerdomain.NewPostgresAvatarRepository(), objects)
	contentA := mustAvatarContent(t, "image/png", "normalized-a")
	contentB := mustAvatarContent(t, "image/webp", "normalized-b")

	first, err := app.Set(ctx, scope, "cus_avatar_flow", 0, contentA)
	if err != nil {
		t.Fatalf("set A: %v", err)
	}
	if first.AvatarRevision != 1 || first.AvatarVersion == nil || *first.AvatarVersion != contentA.Checksum() || first.AvatarObjectID == nil {
		t.Fatalf("first pointer mismatch: %+v", first.AvatarMetadata)
	}
	firstObjectID := *first.AvatarObjectID
	if objects.puts != 1 {
		t.Fatalf("first set puts: want 1, got %d", objects.puts)
	}

	noOp, err := app.Set(ctx, scope, "cus_avatar_flow", 0, contentA)
	if err != nil {
		t.Fatalf("same-content result-unknown replay should converge: %v", err)
	}
	if noOp.AvatarRevision != 1 || *noOp.AvatarObjectID != firstObjectID || objects.puts != 1 {
		t.Fatalf("same-content no-op changed generation: customer=%+v puts=%d", noOp.AvatarMetadata, objects.puts)
	}

	second, err := app.Set(ctx, scope, "cus_avatar_flow", 1, contentB)
	if err != nil {
		t.Fatalf("replace with B: %v", err)
	}
	if second.AvatarRevision != 2 || second.AvatarObjectID == nil || *second.AvatarObjectID == firstObjectID {
		t.Fatalf("replace pointer mismatch: %+v", second.AvatarMetadata)
	}

	third, err := app.Set(ctx, scope, "cus_avatar_flow", 2, contentA)
	if err != nil {
		t.Fatalf("replace B with historical A bytes: %v", err)
	}
	if third.AvatarRevision != 3 || *third.AvatarVersion != contentA.Checksum() || *third.AvatarObjectID == firstObjectID {
		t.Fatalf("ABA must reuse public version but not object generation: %+v", third.AvatarMetadata)
	}
	oldKey, err := customerdomain.AvatarObjectKey(first.AccountID, first.ID, customerdomain.ObjectRef{
		AvatarVersion: *first.AvatarVersion, AvatarObjectID: firstObjectID,
	})
	if err != nil {
		t.Fatalf("old object key: %v", err)
	}
	newKey, err := customerdomain.AvatarObjectKey(third.AccountID, third.ID, customerdomain.ObjectRef{
		AvatarVersion: *third.AvatarVersion, AvatarObjectID: *third.AvatarObjectID,
	})
	if err != nil {
		t.Fatalf("new object key: %v", err)
	}
	if err := objects.Delete(ctx, oldKey); err != nil || !objects.hasExactKey(newKey) {
		t.Fatalf("late delete of historical A must not delete new A generation: err=%v", err)
	}
	if _, err := app.Remove(ctx, scope, "cus_avatar_flow", 1); !errors.Is(err, customerdomain.ErrAvatarRevisionConflict) {
		t.Fatalf("stale ABA delete: want revision conflict, got %v", err)
	}
}

func TestAvatarApplicationRepairsCorruptSameContentWithFreshGeneration(t *testing.T) {
	ctx := context.Background()
	s := openStore(t)
	scope := createAccount(t, s, "acct-avatar-repair")
	seedCustomer(t, scope, seedCustomerInput{
		ID: "cus_avatar_repair", DisplayName: "修复头像", Channel: customerdomain.ChannelOther,
		Status: customerdomain.StatusActive, CreatedAt: time.Now(), Handle: "avatar-repair",
	})
	objects := newMemoryAvatarStore()
	app := customerdomain.NewAvatarApplication(customerdomain.NewPostgresAvatarRepository(), objects)
	content := mustAvatarContent(t, "image/png", "repair-content")
	first, err := app.Set(ctx, scope, "cus_avatar_repair", 0, content)
	if err != nil {
		t.Fatalf("first set: %v", err)
	}
	firstKey := objects.onlyKey(t)
	objects.corruptBody(firstKey, []byte("corrupt"))

	repaired, err := app.Set(ctx, scope, "cus_avatar_repair", 1, content)
	if err != nil {
		t.Fatalf("same-content repair: %v", err)
	}
	if repaired.AvatarRevision != 2 || *repaired.AvatarVersion != *first.AvatarVersion || *repaired.AvatarObjectID == *first.AvatarObjectID {
		t.Fatalf("repair must increment revision and use fresh generation: first=%+v repaired=%+v", first.AvatarMetadata, repaired.AvatarMetadata)
	}
}

func TestAvatarApplicationBurnsPreCurrentGenerationAlreadyInGC(t *testing.T) {
	ctx := context.Background()
	s := openStore(t)
	scope := createAccount(t, s, "acct-avatar-burn")
	seedCustomer(t, scope, seedCustomerInput{
		ID: "cus_avatar_burn", DisplayName: "烧毁代次", Channel: customerdomain.ChannelOther,
		Status: customerdomain.StatusActive, CreatedAt: time.Now(), Handle: "avatar-burn",
	})
	objects := newMemoryAvatarStore()
	var burnedObjectID string
	objects.afterFirstCreate = func(key string, meta customerdomain.ObjectMeta) {
		parts := strings.Split(key, "/")
		burnedObjectID = parts[len(parts)-1]
		if err := scope.Insert(ctx, "avatar_object_gc",
			[]string{"customer_id", "avatar_version", "avatar_object_id", "not_before", "next_attempt_at"},
			"cus_avatar_burn", meta.Checksum, burnedObjectID, time.Now(), time.Now(),
		); err != nil {
			t.Fatalf("pre-current enqueue: %v", err)
		}
	}
	app := customerdomain.NewAvatarApplication(customerdomain.NewPostgresAvatarRepository(), objects)
	result, err := app.Set(ctx, scope, "cus_avatar_burn", 0, mustAvatarContent(t, "image/jpeg", "burn-content"))
	if err != nil {
		t.Fatalf("set after pre-current burn: %v", err)
	}
	if result.AvatarObjectID == nil || *result.AvatarObjectID == burnedObjectID || objects.puts != 2 {
		t.Fatalf("burned generation must not become current: burned=%s result=%+v puts=%d", burnedObjectID, result.AvatarMetadata, objects.puts)
	}
}

func TestAvatarApplicationConcurrentDistinctPUTsUseRevisionCAS(t *testing.T) {
	ctx := context.Background()
	s := openStore(t)
	scope := createAccount(t, s, "acct-avatar-concurrent")
	seedCustomer(t, scope, seedCustomerInput{
		ID: "cus_avatar_concurrent", DisplayName: "并发头像", Channel: customerdomain.ChannelOther,
		Status: customerdomain.StatusActive, CreatedAt: time.Now(), Handle: "avatar-concurrent",
	})
	app := customerdomain.NewAvatarApplication(customerdomain.NewPostgresAvatarRepository(), newMemoryAvatarStore())
	contents := []customerdomain.AvatarContent{
		mustAvatarContent(t, "image/png", "concurrent-a"),
		mustAvatarContent(t, "image/webp", "concurrent-b"),
	}
	errs := make(chan error, len(contents))
	for _, content := range contents {
		content := content
		go func() {
			_, err := app.Set(ctx, scope, "cus_avatar_concurrent", 0, content)
			errs <- err
		}()
	}
	var success, conflict int
	for range contents {
		err := <-errs
		switch {
		case err == nil:
			success++
		case errors.Is(err, customerdomain.ErrAvatarRevisionConflict):
			conflict++
		default:
			t.Fatalf("unexpected concurrent PUT result: %v", err)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatalf("concurrent distinct PUTs: success=%d conflict=%d", success, conflict)
	}
}

func TestAvatarApplicationStorageResultUnknownRetriesSameGeneration(t *testing.T) {
	ctx := context.Background()
	s := openStore(t)
	scope := createAccount(t, s, "acct-avatar-result-unknown")
	seedCustomer(t, scope, seedCustomerInput{
		ID: "cus_avatar_result_unknown", DisplayName: "结果未知", Channel: customerdomain.ChannelOther,
		Status: customerdomain.StatusActive, CreatedAt: time.Now(), Handle: "avatar-result-unknown",
	})
	objects := newMemoryAvatarStore()
	objects.temporaryAfterCreateOnce = true
	app := customerdomain.NewAvatarApplication(customerdomain.NewPostgresAvatarRepository(), objects)
	result, err := app.Set(ctx, scope, "cus_avatar_result_unknown", 0, mustAvatarContent(t, "image/png", "unknown-result"))
	if err != nil {
		t.Fatalf("result-unknown same-generation retry: %v", err)
	}
	if result.AvatarRevision != 1 || objects.puts != 1 {
		t.Fatalf("result-unknown retry must converge on one physical generation: result=%+v puts=%d", result.AvatarMetadata, objects.puts)
	}
}

func TestAvatarApplicationPUTDeleteAndMergeShareCustomerLock(t *testing.T) {
	ctx := context.Background()
	s := openStore(t)
	scope := createAccount(t, s, "acct-avatar-lock")
	for _, customer := range []struct{ id, handle string }{{"cus_avatar_lock_target", "target"}, {"cus_avatar_lock_source", "source"}} {
		seedCustomer(t, scope, seedCustomerInput{
			ID: customer.id, DisplayName: customer.id, Channel: customerdomain.ChannelOther,
			Status: customerdomain.StatusActive, CreatedAt: time.Now(), Handle: customer.handle,
		})
	}
	objects := newMemoryAvatarStore()
	app := customerdomain.NewAvatarApplication(customerdomain.NewPostgresAvatarRepository(), objects)
	initial, err := app.Set(ctx, scope, "cus_avatar_lock_source", 0, mustAvatarContent(t, "image/jpeg", "lock-initial"))
	if err != nil {
		t.Fatalf("set initial avatar: %v", err)
	}

	started := make(chan struct{})
	release := make(chan struct{})
	objects.afterFirstCreate = func(string, customerdomain.ObjectMeta) {
		close(started)
		<-release
	}
	putErr := make(chan error, 1)
	go func() {
		_, err := app.Set(ctx, scope, "cus_avatar_lock_source", initial.AvatarRevision, mustAvatarContent(t, "image/png", "lock-put"))
		putErr <- err
	}()
	<-started
	if _, err := customerService().Merge(ctx, scope, "cus_avatar_lock_target", "cus_avatar_lock_source"); err != nil {
		t.Fatalf("merge while PUT is before customer lock: %v", err)
	}
	close(release)
	if err := <-putErr; !errors.Is(err, customerdomain.ErrCustomerMerged) {
		t.Fatalf("merge-first PUT: want customer_merged, got %v", err)
	}

	removed, err := app.Remove(ctx, scope, "cus_avatar_lock_source", initial.AvatarRevision)
	if err != nil {
		t.Fatalf("merged cleanup after merge-first PUT: %v", err)
	}
	if removed.Status != customerdomain.StatusMerged || removed.AvatarVersion != nil {
		t.Fatalf("cleanup result mismatch: %+v", removed)
	}
}

func TestAvatarApplicationMergedCleanupOnly(t *testing.T) {
	ctx := context.Background()
	s := openStore(t)
	scope := createAccount(t, s, "acct-avatar-merged")
	for _, customer := range []struct{ id, handle string }{{"cus_avatar_target", "target"}, {"cus_avatar_source", "source"}} {
		seedCustomer(t, scope, seedCustomerInput{
			ID: customer.id, DisplayName: customer.id, Channel: customerdomain.ChannelOther,
			Status: customerdomain.StatusActive, CreatedAt: time.Now(), Handle: customer.handle,
		})
	}
	objects := newMemoryAvatarStore()
	app := customerdomain.NewAvatarApplication(customerdomain.NewPostgresAvatarRepository(), objects)
	content := mustAvatarContent(t, "image/jpeg", "merged-avatar")
	withAvatar, err := app.Set(ctx, scope, "cus_avatar_source", 0, content)
	if err != nil {
		t.Fatalf("set source avatar: %v", err)
	}
	if _, err := customerService().Merge(ctx, scope, "cus_avatar_target", "cus_avatar_source"); err != nil {
		t.Fatalf("merge: %v", err)
	}
	if _, err := app.Set(ctx, scope, "cus_avatar_source", withAvatar.AvatarRevision, content); !errors.Is(err, customerdomain.ErrCustomerMerged) {
		t.Fatalf("merged same-content PUT: want customer_merged, got %v", err)
	}
	removed, err := app.Remove(ctx, scope, "cus_avatar_source", withAvatar.AvatarRevision)
	if err != nil {
		t.Fatalf("merged cleanup-only remove: %v", err)
	}
	if removed.Status != customerdomain.StatusMerged || removed.AvatarRevision != withAvatar.AvatarRevision+1 || removed.AvatarVersion != nil {
		t.Fatalf("merged cleanup result mismatch: %+v", removed)
	}
	replayed, err := app.Remove(ctx, scope, "cus_avatar_source", withAvatar.AvatarRevision)
	if err != nil || replayed.AvatarRevision != removed.AvatarRevision {
		t.Fatalf("already-none remove must be no-op before revision comparison: customer=%+v err=%v", replayed, err)
	}
}

func mustAvatarContent(t *testing.T, mediaType, body string) customerdomain.AvatarContent {
	t.Helper()
	_ = mediaType
	digest := sha256.Sum256([]byte(body))
	img := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	fill := color.NRGBA{R: digest[0], G: digest[1], B: digest[2], A: 255}
	for y := range 2 {
		for x := range 2 {
			img.SetNRGBA(x, y, fill)
		}
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, img); err != nil {
		t.Fatalf("encode avatar content: %v", err)
	}
	content, err := customerdomain.NewAvatarContent(encoded.Bytes(), "image/png")
	if err != nil {
		t.Fatalf("new avatar content: %v", err)
	}
	return content
}

type memoryAvatarStore struct {
	mu                       sync.Mutex
	objects                  map[string]memoryAvatarObject
	puts                     int
	afterFirstCreate         func(string, customerdomain.ObjectMeta)
	temporaryAfterCreateOnce bool
}

type memoryAvatarObject struct {
	body []byte
	meta customerdomain.ObjectMeta
}

func newMemoryAvatarStore() *memoryAvatarStore {
	return &memoryAvatarStore{objects: make(map[string]memoryAvatarObject)}
}

func (s *memoryAvatarStore) PutImmutable(_ context.Context, key string, body customerdomain.AvatarContent, expected customerdomain.ObjectMeta) (customerdomain.PutResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if current, ok := s.objects[key]; ok {
		if current.meta != expected || !bytes.Equal(current.body, body.Bytes()) {
			return customerdomain.PutResult{}, customerdomain.ErrAvatarObjectIntegrity
		}
		return customerdomain.PutResult{Meta: current.meta, Created: false}, nil
	}
	s.puts++
	s.objects[key] = memoryAvatarObject{body: body.Bytes(), meta: expected}
	hook := s.afterFirstCreate
	if hook != nil {
		s.afterFirstCreate = nil
		s.mu.Unlock()
		hook(key, expected)
		s.mu.Lock()
	}
	if s.temporaryAfterCreateOnce {
		s.temporaryAfterCreateOnce = false
		return customerdomain.PutResult{}, customerdomain.ErrAvatarObjectTemporary
	}
	return customerdomain.PutResult{Meta: expected, Created: true}, nil
}

func (s *memoryAvatarStore) Open(_ context.Context, key string) (io.ReadCloser, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	object, ok := s.objects[key]
	if !ok {
		return nil, customerdomain.ErrAvatarObjectNotFound
	}
	return io.NopCloser(bytes.NewReader(append([]byte(nil), object.body...))), nil
}

func (s *memoryAvatarStore) Stat(_ context.Context, key string) (customerdomain.ObjectMeta, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	object, ok := s.objects[key]
	if !ok {
		return customerdomain.ObjectMeta{}, customerdomain.ErrAvatarObjectNotFound
	}
	return object.meta, nil
}

func (*memoryAvatarStore) List(context.Context, string, string, int) (customerdomain.ObjectPage, error) {
	return customerdomain.ObjectPage{Done: true}, nil
}

func (s *memoryAvatarStore) Delete(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.objects, key)
	return nil
}

func (s *memoryAvatarStore) hasExactKey(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.objects[key]
	return ok
}

func (s *memoryAvatarStore) onlyKey(t *testing.T) string {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.objects) != 1 {
		t.Fatalf("want exactly one object, got %d", len(s.objects))
	}
	for key := range s.objects {
		return key
	}
	return ""
}

func (s *memoryAvatarStore) corruptBody(key string, body []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	object := s.objects[key]
	object.body = append([]byte(nil), body...)
	s.objects[key] = object
}
