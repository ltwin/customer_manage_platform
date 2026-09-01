package accountprofile_test

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/accountprofile"
	"github.com/samson/customer-manage-platform/backend/internal/avatarmedia"
	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store/storetest"
)

func TestMain(m *testing.M) { storetest.Main(m, store.MigrateUp) }

func TestGetVirtualDefaultDoesNotWrite(t *testing.T) {
	ctx := context.Background()
	s := openStore(t)
	scope := createAccount(t, s, "acct-profile-virtual")
	svc := accountprofile.NewService(accountprofile.NewPostgresRepository(), newMemoryStore())
	profile, err := svc.Get(ctx, scope)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if profile.DisplayName != nil || profile.ProfileRevision != 0 || profile.AvatarRevision != 0 {
		t.Fatalf("virtual default mismatch: %+v", profile)
	}
	exists, err := scope.Exists(ctx, "account_profiles", "")
	if err != nil || exists {
		t.Fatalf("Get must not write row: exists=%v err=%v", exists, err)
	}
}

func TestColumnLocalConcurrentFirstWrite(t *testing.T) {
	ctx := context.Background()
	s := openStore(t)
	scope := createAccount(t, s, "acct-profile-cas")
	objects := newMemoryStore()
	svc := accountprofile.NewService(accountprofile.NewPostgresRepository(), objects)
	content := mustPNG(t)

	var wg sync.WaitGroup
	errs := make(chan error, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, err := svc.Patch(ctx, scope, 0, accountprofile.PatchInput{
			DisplayName: accountprofile.OptionalString{Set: true, Value: strPtr("摄影师甲")},
		})
		errs <- err
	}()
	go func() {
		defer wg.Done()
		_, err := svc.SetAvatar(ctx, scope, 0, content)
		errs <- err
	}()
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent first write: %v", err)
		}
	}
	profile, err := svc.Get(ctx, scope)
	if err != nil {
		t.Fatalf("Get after concurrent: %v", err)
	}
	if profile.DisplayName == nil || *profile.DisplayName != "摄影师甲" {
		t.Fatalf("display_name lost: %+v", profile)
	}
	if profile.AvatarVersion == nil || *profile.AvatarVersion != content.Checksum() {
		t.Fatalf("avatar lost: %+v", profile)
	}
	if profile.ProfileRevision < 1 || profile.AvatarRevision < 1 {
		t.Fatalf("revisions not advanced: %+v", profile)
	}
}

func TestDisplayNameGraphemeAndClear(t *testing.T) {
	ctx := context.Background()
	s := openStore(t)
	scope := createAccount(t, s, "acct-profile-name")
	svc := accountprofile.NewService(accountprofile.NewPostgresRepository(), newMemoryStore())
	_, err := svc.Patch(ctx, scope, 0, accountprofile.PatchInput{
		DisplayName: accountprofile.OptionalString{Set: true, Value: strPtr("")},
	})
	if err == nil {
		t.Fatal("empty string must fail validation")
	}
	long := ""
	for range 41 {
		long += "影"
	}
	_, err = svc.Patch(ctx, scope, 0, accountprofile.PatchInput{
		DisplayName: accountprofile.OptionalString{Set: true, Value: &long},
	})
	if err == nil {
		t.Fatal("41 graphemes must fail")
	}
	name := "AbC"
	first, err := svc.Patch(ctx, scope, 0, accountprofile.PatchInput{
		DisplayName: accountprofile.OptionalString{Set: true, Value: &name},
	})
	if err != nil || first.DisplayName == nil || *first.DisplayName != "AbC" {
		t.Fatalf("case-preserving set: %+v err=%v", first, err)
	}
	cleared, err := svc.Patch(ctx, scope, first.ProfileRevision, accountprofile.PatchInput{
		DisplayName: accountprofile.OptionalString{Set: true, Value: nil},
	})
	if err != nil || cleared.DisplayName != nil {
		t.Fatalf("clear: %+v err=%v", cleared, err)
	}
}

func TestAvatarNoOpAndRemove(t *testing.T) {
	ctx := context.Background()
	s := openStore(t)
	scope := createAccount(t, s, "acct-profile-avatar")
	objects := newMemoryStore()
	svc := accountprofile.NewService(accountprofile.NewPostgresRepository(), objects)
	content := mustPNG(t)
	first, err := svc.SetAvatar(ctx, scope, 0, content)
	if err != nil {
		t.Fatalf("set: %v", err)
	}
	noOp, err := svc.SetAvatar(ctx, scope, 0, content)
	if err != nil || noOp.AvatarRevision != first.AvatarRevision || objects.puts != 1 {
		t.Fatalf("same-content no-op: %+v puts=%d err=%v", noOp, objects.puts, err)
	}
	removed, err := svc.RemoveAvatar(ctx, scope, first.AvatarRevision)
	if err != nil || removed.AvatarVersion != nil {
		t.Fatalf("remove: %+v err=%v", removed, err)
	}
	again, err := svc.RemoveAvatar(ctx, scope, 0)
	if err != nil || again.AvatarRevision != removed.AvatarRevision {
		t.Fatalf("absent no-op: %+v err=%v", again, err)
	}
}

func TestGuardedDownSQLPresent(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	down := filepath.Join(filepath.Dir(file), "..", "platform", "store", "migrations", "0015_account_profiles.down.sql")
	raw, err := os.ReadFile(down)
	if err != nil {
		t.Fatalf("read down: %v", err)
	}
	if !bytes.Contains(raw, []byte("refuse destructive down")) {
		t.Fatal("guarded down must refuse non-empty tables")
	}
}

func openStore(t *testing.T) *store.Store {
	t.Helper()
	url := startPostgres(t)
	if err := store.MigrateUp(url); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	s, err := store.Open(context.Background(), url)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(s.Close)
	return s
}

func startPostgres(t *testing.T) string {
	t.Helper()
	return storetest.NewURL(t)
}

func createAccount(t *testing.T, s *store.Store, id string) store.AccountScope {
	t.Helper()
	if err := s.CreateAccount(context.Background(), id, "test-hash"); err != nil {
		t.Fatalf("create account %s: %v", id, err)
	}
	activateLegacyTestAccount(t, s, id)
	return s.ScopeFor(auth.AccountContext{AccountID: id})
}

func activateLegacyTestAccount(t *testing.T, s *store.Store, id string) {
	t.Helper()
	mail := &activationMailSender{}
	service := auth.NewService(
		s,
		auth.NewTokenIssuer("account-profile-test-activation-root"),
		auth.WithAuthMailSender(mail),
		auth.WithAttemptLimiter(s),
		auth.WithPublicBaseURL("https://account-profile.test"),
	)
	result, err := service.BeginLegacyClaim(context.Background(), id+"@account-profile.test", false)
	if err != nil || result.State != auth.LegacyClaimReady || mail.wire == "" {
		t.Fatalf("begin legacy test-account activation: state=%s err=%v", result.State, err)
	}
	if _, err := service.VerifyEmail(context.Background(), mail.wire, auth.ClientMeta{
		SourceIP: netip.MustParseAddr("127.0.0.1"),
	}); err != nil {
		t.Fatalf("verify legacy test-account: %v", err)
	}
}

type activationMailSender struct{ wire string }

func (m *activationMailSender) Send(_ context.Context, mail auth.AuthMail) (auth.MailReceipt, error) {
	actionURL, err := url.Parse(mail.ActionURL)
	if err != nil {
		return auth.MailReceipt{}, fmt.Errorf("parse activation action URL: %w", err)
	}
	fragment, err := url.ParseQuery(actionURL.Fragment)
	if err != nil {
		return auth.MailReceipt{}, fmt.Errorf("parse activation action fragment: %w", err)
	}
	m.wire = fragment.Get("token")
	return auth.MailReceipt{ProviderMessageID: "account-profile-test-activation", AcceptedAt: time.Now().UTC()}, nil
}

func strPtr(v string) *string { return &v }

func mustPNG(t *testing.T) avatarmedia.Content {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x * 20), G: uint8(y * 20), B: 90, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode: %v", err)
	}
	content, err := avatarmedia.DecodeConfirm(buf.Bytes(), "image/png")
	if err != nil {
		t.Fatalf("DecodeConfirm: %v", err)
	}
	return content
}

type memoryStore struct {
	mu      sync.Mutex
	objects map[string]struct {
		body []byte
		meta avatarmedia.ObjectMeta
	}
	puts int
}

func newMemoryStore() *memoryStore {
	return &memoryStore{objects: map[string]struct {
		body []byte
		meta avatarmedia.ObjectMeta
	}{}}
}

func (s *memoryStore) PutImmutable(_ context.Context, key avatarmedia.Key, content avatarmedia.Content, meta avatarmedia.ObjectMeta) (avatarmedia.PutResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.objects[key.String()]; ok {
		return avatarmedia.PutResult{Created: false, Meta: existing.meta}, nil
	}
	s.puts++
	s.objects[key.String()] = struct {
		body []byte
		meta avatarmedia.ObjectMeta
	}{body: content.Bytes(), meta: meta}
	return avatarmedia.PutResult{Created: true, Meta: meta}, nil
}

func (s *memoryStore) Open(_ context.Context, key avatarmedia.Key) (io.ReadCloser, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	object, ok := s.objects[key.String()]
	if !ok {
		return nil, avatarmedia.ErrObjectNotFound
	}
	return io.NopCloser(bytes.NewReader(append([]byte(nil), object.body...))), nil
}

func (s *memoryStore) Stat(_ context.Context, key avatarmedia.Key) (avatarmedia.ObjectMeta, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	object, ok := s.objects[key.String()]
	if !ok {
		return avatarmedia.ObjectMeta{}, avatarmedia.ErrObjectNotFound
	}
	return object.meta, nil
}

func (*memoryStore) List(context.Context, avatarmedia.Prefix, avatarmedia.Cursor, int) (avatarmedia.ObjectPage, error) {
	return avatarmedia.ObjectPage{Done: true}, nil
}

func (s *memoryStore) Delete(_ context.Context, key avatarmedia.Key) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.objects, key.String())
	return nil
}
