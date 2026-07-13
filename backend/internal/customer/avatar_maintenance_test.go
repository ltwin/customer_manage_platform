package customer_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	customerdomain "github.com/samson/customer-manage-platform/backend/internal/customer"
	"github.com/samson/customer-manage-platform/backend/internal/customer/avatarstore"
	platformstore "github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

func TestAvatarMaintenanceReconcilesAndDeletesOnlyExactOldGeneration(t *testing.T) {
	ctx := context.Background()
	s := openStore(t)
	scope := createAccount(t, s, "acct-avatar-maintenance")
	seedCustomer(t, scope, seedCustomerInput{
		ID: "cus_avatar_maintenance", DisplayName: "维护头像", Channel: customerdomain.ChannelOther,
		Status: customerdomain.StatusActive, CreatedAt: time.Now(), Handle: "avatar-maintenance",
	})
	objects, err := avatarstore.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocal: %v", err)
	}
	repo := customerdomain.NewPostgresAvatarRepository()
	app := customerdomain.NewAvatarApplication(repo, objects)
	first, err := app.Set(ctx, scope, "cus_avatar_maintenance", 0, mustAvatarContent(t, "image/png", "maintenance-a"))
	if err != nil {
		t.Fatalf("set A: %v", err)
	}
	second, err := app.Set(ctx, scope, "cus_avatar_maintenance", 1, mustAvatarContent(t, "image/png", "maintenance-b"))
	if err != nil {
		t.Fatalf("set B: %v", err)
	}
	oldRef := customerdomain.ObjectRef{AvatarVersion: *first.AvatarVersion, AvatarObjectID: *first.AvatarObjectID}
	oldKey, _ := customerdomain.AvatarObjectKey(first.AccountID, first.ID, oldRef)
	currentRef := customerdomain.ObjectRef{AvatarVersion: *second.AvatarVersion, AvatarObjectID: *second.AvatarObjectID}
	currentKey, _ := customerdomain.AvatarObjectKey(second.AccountID, second.ID, currentRef)
	past := time.Now().Add(-time.Hour)
	if _, err := scope.Update(ctx, "avatar_object_gc", "not_before = $2, next_attempt_at = $2", "avatar_object_id = $3", past, oldRef.AvatarObjectID); err != nil {
		t.Fatalf("make old GC due: %v", err)
	}

	runner := customerdomain.NewAvatarMaintenanceRunner(s, repo, objects, slog.Default())
	runner.RunOnce(ctx)
	if _, err := objects.Stat(ctx, oldKey); !errors.Is(err, customerdomain.ErrAvatarObjectNotFound) {
		t.Fatalf("old exact generation should be deleted, got %v", err)
	}
	if _, err := objects.Stat(ctx, currentKey); err != nil {
		t.Fatalf("current generation must survive old delete: %v", err)
	}

	if err := scope.Insert(ctx, "avatar_object_gc",
		[]string{"customer_id", "avatar_version", "avatar_object_id", "not_before", "next_attempt_at"},
		second.ID, currentRef.AvatarVersion, currentRef.AvatarObjectID, past, past,
	); err != nil {
		t.Fatalf("insert stale current GC row: %v", err)
	}
	runner.RunOnce(ctx)
	if _, err := objects.Stat(ctx, currentKey); err != nil {
		t.Fatalf("stale current GC row must not call Delete: %v", err)
	}
	count, err := scope.Count(ctx, "avatar_object_gc", "customer_id = $2", second.ID)
	if err != nil || count != 0 {
		t.Fatalf("stale current GC row should be removed: count=%d err=%v", count, err)
	}
}

func TestAvatarMaintenanceDBSessionLossCannotDeleteRepublishedChecksum(t *testing.T) {
	ctx := context.Background()
	url, postgres := startPostgresContainer(t)
	if err := platformstore.MigrateUp(url); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	database, err := platformstore.Open(ctx, url)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(database.Close)
	scope := createAccount(t, database, "acct-avatar-session-loss")
	seedCustomer(t, scope, seedCustomerInput{
		ID: "cus_avatar_session_loss", DisplayName: "会话丢失", Channel: customerdomain.ChannelOther,
		Status: customerdomain.StatusActive, CreatedAt: time.Now(), Handle: "avatar-session-loss",
	})
	local, err := avatarstore.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocal: %v", err)
	}
	blocking := &blockingDeleteStore{AvatarObjectStore: local, started: make(chan struct{}), release: make(chan struct{})}
	repo := customerdomain.NewPostgresAvatarRepository()
	app := customerdomain.NewAvatarApplication(repo, blocking)
	contentA := mustAvatarContent(t, "image/png", "session-loss-a")
	first, err := app.Set(ctx, scope, "cus_avatar_session_loss", 0, contentA)
	if err != nil {
		t.Fatalf("set A: %v", err)
	}
	second, err := app.Set(ctx, scope, "cus_avatar_session_loss", 1, mustAvatarContent(t, "image/png", "session-loss-b"))
	if err != nil {
		t.Fatalf("set B: %v", err)
	}
	oldRef := customerdomain.ObjectRef{AvatarVersion: *first.AvatarVersion, AvatarObjectID: *first.AvatarObjectID}
	blocking.blockKey, _ = customerdomain.AvatarObjectKey(first.AccountID, first.ID, oldRef)
	past := time.Now().Add(-time.Hour)
	if _, err := scope.Update(ctx, "avatar_object_gc", "not_before = $2, next_attempt_at = $2", "avatar_object_id = $3", past, oldRef.AvatarObjectID); err != nil {
		t.Fatalf("make old GC due: %v", err)
	}

	runner := customerdomain.NewAvatarMaintenanceRunner(database, repo, blocking, nil)
	done := make(chan struct{})
	go func() {
		runner.RunOnce(ctx)
		close(done)
	}()
	select {
	case <-blocking.started:
	case <-time.After(10 * time.Second):
		t.Fatal("GC did not reach blocked storage Delete")
	}
	command := `SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE pid <> pg_backend_pid() AND state = 'idle in transaction' AND query LIKE '%avatar_object_gc%'`
	if _, _, err := postgres.Exec(ctx, []string{"psql", "-U", "crm_test", "-d", "crm_test", "-c", command}); err != nil {
		t.Fatalf("terminate GC database session: %v", err)
	}

	republished, err := app.Set(ctx, scope, "cus_avatar_session_loss", second.AvatarRevision, contentA)
	if err != nil {
		t.Fatalf("republish A after DB session loss: %v", err)
	}
	if *republished.AvatarObjectID == *first.AvatarObjectID {
		t.Fatal("republished checksum must use a fresh object_id")
	}
	close(blocking.release)
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("GC did not finish after releasing delayed Delete")
	}
	newRef := customerdomain.ObjectRef{AvatarVersion: *republished.AvatarVersion, AvatarObjectID: *republished.AvatarObjectID}
	newKey, _ := customerdomain.AvatarObjectKey(republished.AccountID, republished.ID, newRef)
	if _, err := local.Stat(ctx, newKey); err != nil {
		t.Fatalf("late old-generation Delete damaged new current: %v", err)
	}
}

func TestAvatarMaintenancePreCurrentGenerationBurnSurvivesDBSessionLoss(t *testing.T) {
	ctx := context.Background()
	url, postgres := startPostgresContainer(t)
	if err := platformstore.MigrateUp(url); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	database, err := platformstore.Open(ctx, url)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(database.Close)
	scope := createAccount(t, database, "acct-avatar-precurrent")
	seedCustomer(t, scope, seedCustomerInput{
		ID: "cus_avatar_precurrent", DisplayName: "发布前代次", Channel: customerdomain.ChannelOther,
		Status: customerdomain.StatusActive, CreatedAt: time.Now(), Handle: "avatar-precurrent",
	})
	local, err := avatarstore.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocal: %v", err)
	}
	raceStore := newPreCurrentRaceStore(local)
	repo := customerdomain.NewPostgresAvatarRepository()
	app := customerdomain.NewAvatarApplication(repo, raceStore)
	content := mustAvatarContent(t, "image/png", "precurrent-content")
	setResult := make(chan struct {
		customer customerdomain.Customer
		err      error
	}, 1)
	go func() {
		customer, err := app.Set(ctx, scope, "cus_avatar_precurrent", 0, content)
		setResult <- struct {
			customer customerdomain.Customer
			err      error
		}{customer: customer, err: err}
	}()
	select {
	case <-raceStore.putStarted:
	case <-time.After(10 * time.Second):
		t.Fatal("PUT did not pause after publishing pre-current generation")
	}

	runner := customerdomain.NewAvatarMaintenanceRunner(database, repo, raceStore, nil)
	runner.RunOnce(ctx)
	past := time.Now().Add(-time.Hour)
	if _, err := scope.Update(ctx, "avatar_object_gc", "not_before = $2, next_attempt_at = $2", "", past); err != nil {
		t.Fatalf("make pre-current GC due: %v", err)
	}
	gcDone := make(chan struct{})
	go func() {
		runner.RunOnce(ctx)
		close(gcDone)
	}()
	select {
	case <-raceStore.deleteStarted:
	case <-time.After(10 * time.Second):
		t.Fatal("GC did not pause deleting pre-current generation")
	}
	command := `SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE pid <> pg_backend_pid() AND state = 'idle in transaction' AND query LIKE '%avatar_object_gc%'`
	if _, _, err := postgres.Exec(ctx, []string{"psql", "-U", "crm_test", "-d", "crm_test", "-c", command}); err != nil {
		t.Fatalf("terminate pre-current GC session: %v", err)
	}
	close(raceStore.putRelease)
	result := <-setResult
	if result.err != nil {
		t.Fatalf("original PUT should burn X and publish fresh Y: %v", result.err)
	}
	if result.customer.AvatarObjectID == nil || *result.customer.AvatarObjectID == raceStore.firstObjectID {
		t.Fatalf("burned X became current: X=%s result=%+v", raceStore.firstObjectID, result.customer.AvatarMetadata)
	}
	close(raceStore.deleteRelease)
	select {
	case <-gcDone:
	case <-time.After(10 * time.Second):
		t.Fatal("pre-current GC did not finish")
	}
	currentRef := customerdomain.ObjectRef{AvatarVersion: *result.customer.AvatarVersion, AvatarObjectID: *result.customer.AvatarObjectID}
	currentKey, _ := customerdomain.AvatarObjectKey(result.customer.AccountID, result.customer.ID, currentRef)
	if _, err := local.Stat(ctx, currentKey); err != nil {
		t.Fatalf("late Delete(X) damaged current Y: %v", err)
	}
}

func TestAvatarMaintenanceRunStopsOnContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	runner := customerdomain.NewAvatarMaintenanceRunner(nil, customerdomain.NewPostgresAvatarRepository(), newMemoryAvatarStore(), nil)
	done := make(chan struct{})
	go func() {
		runner.Run(ctx)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("maintenance runner did not stop after cancellation")
	}
}

func TestAvatarMaintenancePointerAuditRetriesTemporaryAndRevisitsNextCycle(t *testing.T) {
	ctx := context.Background()
	database := openStore(t)
	scope := createAccount(t, database, "acct-avatar-audit-retry")
	seedCustomer(t, scope, seedCustomerInput{
		ID: "cus_avatar_audit_retry", DisplayName: "审计重试", Channel: customerdomain.ChannelOther,
		Status: customerdomain.StatusActive, CreatedAt: time.Now(), Handle: "avatar-audit-retry",
	})
	delegate := newMemoryAvatarStore()
	repo := customerdomain.NewPostgresAvatarRepository()
	app := customerdomain.NewAvatarApplication(repo, delegate)
	current, err := app.Set(ctx, scope, "cus_avatar_audit_retry", 0, mustAvatarContent(t, "image/png", "audit-retry"))
	if err != nil {
		t.Fatalf("set current avatar: %v", err)
	}
	ref := customerdomain.ObjectRef{AvatarVersion: *current.AvatarVersion, AvatarObjectID: *current.AvatarObjectID}
	key, _ := customerdomain.AvatarObjectKey(current.AccountID, current.ID, ref)
	seedCustomer(t, scope, seedCustomerInput{
		ID: "cus_avatar_audit_retry_later", DisplayName: "后页审计", Channel: customerdomain.ChannelOther,
		Status: customerdomain.StatusActive, CreatedAt: time.Now(), Handle: "avatar-audit-retry-later",
	})
	if _, err := app.Set(ctx, scope, "cus_avatar_audit_retry_later", 0, mustAvatarContent(t, "image/png", "audit-later")); err != nil {
		t.Fatalf("set later current avatar: %v", err)
	}
	temporary := &temporaryStatStore{AvatarObjectStore: delegate, key: key}
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	runner := customerdomain.NewAvatarMaintenanceRunner(database, repo, temporary, logger)

	runner.RunOnce(ctx)
	if got := temporary.calls.Load(); got != 3 {
		t.Fatalf("first tick must use three bounded attempts, got %d", got)
	}
	if got := temporary.otherCalls.Load(); got == 0 {
		t.Fatal("permanent temporary first pointer must not starve later pointers")
	}
	if !bytes.Contains(logs.Bytes(), []byte("avatar current pointer audit temporary")) ||
		!bytes.Contains(logs.Bytes(), []byte("attempts=3")) {
		t.Fatalf("temporary audit log missing classification/attempts: %s", logs.String())
	}
	checkpoint, err := repo.LoadCheckpoint(ctx, scope)
	if err != nil {
		t.Fatalf("load checkpoint: %v", err)
	}
	if checkpoint.PointerCursor != "" || checkpoint.PointerCycle != 1 {
		t.Fatalf("exhausted temporary must advance and finish cycle: %+v", checkpoint)
	}

	runner.RunOnce(ctx)
	if got := temporary.calls.Load(); got != 6 {
		t.Fatalf("next complete cycle must revisit temporary pointer, got %d calls", got)
	}
}

func TestAvatarMaintenanceA23ThreePageRestartSingleFlightDueLimitAndBackoff(t *testing.T) {
	ctx := context.Background()
	database := openStore(t)
	scope := createAccount(t, database, "acct-avatar-a23-matrix")
	repo := customerdomain.NewPostgresAvatarRepository()
	objects, err := avatarstore.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocal: %v", err)
	}
	content := mustAvatarContent(t, "image/png", "a23-pages")
	meta := customerdomain.ObjectMeta{
		MediaType: content.MediaType(), Size: content.Size(), Checksum: content.Checksum(), ModifiedAt: time.Now().UTC(),
	}
	for index := 0; index < 201; index++ {
		customerID := fmt.Sprintf("cus_page_%03d", index)
		objectID := fmt.Sprintf("%032x", index+1)
		key, err := customerdomain.AvatarObjectKey("acct-avatar-a23-matrix", customerID, customerdomain.ObjectRef{
			AvatarVersion: content.Checksum(), AvatarObjectID: objectID,
		})
		if err != nil {
			t.Fatalf("page key %d: %v", index, err)
		}
		if _, err := objects.PutImmutable(ctx, key, content, meta); err != nil {
			t.Fatalf("put page object %d: %v", index, err)
		}
		if err := scope.Insert(ctx, "customers", []string{
			"id", "display_name", "channel", "status", "created_at", "avatar_revision",
			"avatar_version", "avatar_object_id", "avatar_media_type", "avatar_size", "avatar_updated_at",
		}, customerID, customerID, customerdomain.ChannelOther, customerdomain.StatusActive, time.Now().UTC(), int64(1),
			content.Checksum(), objectID, content.MediaType(), content.Size(), time.Now().UTC()); err != nil {
			t.Fatalf("insert page customer %d: %v", index, err)
		}
	}

	runner := customerdomain.NewAvatarMaintenanceRunner(database, repo, objects, nil)
	runner.RunOnce(ctx)
	checkpoint, err := repo.LoadCheckpoint(ctx, scope)
	if err != nil {
		t.Fatalf("checkpoint page 1: %v", err)
	}
	if checkpoint.ObjectCursor == "" || checkpoint.PointerCursor == "" || checkpoint.ObjectCycle != 0 || checkpoint.PointerCycle != 0 {
		t.Fatalf("first tick must stop at first raw page: %+v", checkpoint)
	}
	runner.RunOnce(ctx)
	checkpoint, err = repo.LoadCheckpoint(ctx, scope)
	if err != nil {
		t.Fatalf("checkpoint page 2: %v", err)
	}
	if checkpoint.ObjectCursor == "" || checkpoint.PointerCursor == "" || checkpoint.ObjectCycle != 0 || checkpoint.PointerCycle != 0 {
		t.Fatalf("second tick must stop at second raw page: %+v", checkpoint)
	}
	restarted := customerdomain.NewAvatarMaintenanceRunner(database, repo, objects, nil)
	restarted.RunOnce(ctx)
	checkpoint, err = repo.LoadCheckpoint(ctx, scope)
	if err != nil {
		t.Fatalf("checkpoint page 3 after restart: %v", err)
	}
	if checkpoint.ObjectCursor != "" || checkpoint.PointerCursor != "" || checkpoint.ObjectCycle != 1 || checkpoint.PointerCycle != 1 {
		t.Fatalf("third tick after restart must complete both cycles: %+v", checkpoint)
	}

	blocking := &blockingListStore{AvatarObjectStore: objects, started: make(chan struct{}), release: make(chan struct{})}
	accounts := &countingAccounts{AccountScopeEnumerator: database}
	singleFlight := customerdomain.NewAvatarMaintenanceRunner(accounts, repo, blocking, nil)
	firstDone := make(chan struct{})
	go func() {
		singleFlight.RunOnce(ctx)
		close(firstDone)
	}()
	select {
	case <-blocking.started:
	case <-time.After(5 * time.Second):
		t.Fatal("first single-flight tick did not reach object List")
	}
	secondDone := make(chan struct{})
	go func() {
		singleFlight.RunOnce(ctx)
		close(secondDone)
	}()
	select {
	case <-secondDone:
	case <-time.After(time.Second):
		t.Fatal("overlapping tick did not skip immediately")
	}
	if got := accounts.calls.Load(); got != 1 {
		t.Fatalf("overlapping tick enumerated accounts: %d", got)
	}
	close(blocking.release)
	select {
	case <-firstDone:
	case <-time.After(5 * time.Second):
		t.Fatal("first single-flight tick did not finish")
	}

	dueContent := mustAvatarContent(t, "image/png", "a23-due")
	dueMeta := customerdomain.ObjectMeta{
		MediaType: dueContent.MediaType(), Size: dueContent.Size(), Checksum: dueContent.Checksum(), ModifiedAt: time.Now().UTC(),
	}
	past := time.Now().UTC().Add(-time.Hour)
	var failedItem customerdomain.AvatarGCItem
	var failedKey string
	for index := 0; index < 101; index++ {
		customerID := fmt.Sprintf("cus_gc_%03d", index)
		objectID := fmt.Sprintf("f%031x", index+1)
		ref := customerdomain.ObjectRef{AvatarVersion: dueContent.Checksum(), AvatarObjectID: objectID}
		key, _ := customerdomain.AvatarObjectKey("acct-avatar-a23-matrix", customerID, ref)
		if _, err := objects.PutImmutable(ctx, key, dueContent, dueMeta); err != nil {
			t.Fatalf("put due object %d: %v", index, err)
		}
		if err := scope.Insert(ctx, "customers", []string{"id", "display_name", "channel", "status", "created_at"},
			customerID, customerID, customerdomain.ChannelOther, customerdomain.StatusActive, time.Now().UTC()); err != nil {
			t.Fatalf("insert due customer %d: %v", index, err)
		}
		if err := scope.Insert(ctx, "avatar_object_gc",
			[]string{"customer_id", "avatar_version", "avatar_object_id", "not_before", "next_attempt_at"},
			customerID, ref.AvatarVersion, ref.AvatarObjectID, past, past); err != nil {
			t.Fatalf("insert due gc %d: %v", index, err)
		}
		if index == 0 {
			failedItem = customerdomain.AvatarGCItem{CustomerID: customerID, Ref: ref}
			failedKey = key
		}
	}
	failing := &failingDeleteStore{AvatarObjectStore: objects, key: failedKey}
	limited := customerdomain.NewAvatarMaintenanceRunner(database, repo, failing, nil)
	limited.RunOnce(ctx)
	remaining, err := scope.Count(ctx, "avatar_object_gc", "customer_id LIKE $2", "cus_gc_%")
	if err != nil {
		t.Fatalf("count remaining due rows: %v", err)
	}
	if remaining != 2 {
		t.Fatalf("100 due limit with one failure must leave failed+101st rows, got %d", remaining)
	}
	var attempts int
	var nextAttempt time.Time
	if err := scope.QueryRow(ctx, "avatar_object_gc", "attempts, next_attempt_at",
		"customer_id = $2 AND avatar_object_id = $3", failedItem.CustomerID, failedItem.Ref.AvatarObjectID).Scan(&attempts, &nextAttempt); err != nil {
		t.Fatalf("load failed gc backoff: %v", err)
	}
	if attempts != 1 || !nextAttempt.After(past) {
		t.Fatalf("failed delete backoff mismatch: attempts=%d next=%s", attempts, nextAttempt)
	}
}

type temporaryStatStore struct {
	customerdomain.AvatarObjectStore
	key        string
	calls      atomic.Int32
	otherCalls atomic.Int32
}

func (s *temporaryStatStore) Stat(ctx context.Context, key string) (customerdomain.ObjectMeta, error) {
	if key == s.key {
		s.calls.Add(1)
		return customerdomain.ObjectMeta{}, customerdomain.ErrAvatarObjectTemporary
	}
	s.otherCalls.Add(1)
	return s.AvatarObjectStore.Stat(ctx, key)
}

type countingAccounts struct {
	customerdomain.AccountScopeEnumerator
	calls atomic.Int32
}

func (a *countingAccounts) AccountScopes(ctx context.Context) ([]platformstore.ScopedAccount, error) {
	a.calls.Add(1)
	return a.AccountScopeEnumerator.AccountScopes(ctx)
}

type blockingListStore struct {
	customerdomain.AvatarObjectStore
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (s *blockingListStore) List(ctx context.Context, prefix, cursor string, limit int) (customerdomain.ObjectPage, error) {
	s.once.Do(func() {
		close(s.started)
		select {
		case <-ctx.Done():
		case <-s.release:
		}
	})
	return s.AvatarObjectStore.List(ctx, prefix, cursor, limit)
}

type failingDeleteStore struct {
	customerdomain.AvatarObjectStore
	key string
}

func (s *failingDeleteStore) Delete(ctx context.Context, key string) error {
	if key == s.key {
		return customerdomain.ErrAvatarObjectTemporary
	}
	return s.AvatarObjectStore.Delete(ctx, key)
}

type blockingDeleteStore struct {
	customerdomain.AvatarObjectStore
	blockKey string
	started  chan struct{}
	release  chan struct{}
	once     sync.Once
}

func (s *blockingDeleteStore) Delete(ctx context.Context, key string) error {
	if key == s.blockKey {
		s.once.Do(func() { close(s.started) })
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-s.release:
		}
	}
	return s.AvatarObjectStore.Delete(ctx, key)
}

type preCurrentRaceStore struct {
	customerdomain.AvatarObjectStore
	putStarted    chan struct{}
	putRelease    chan struct{}
	deleteStarted chan struct{}
	deleteRelease chan struct{}
	putOnce       sync.Once
	deleteOnce    sync.Once
	firstKey      string
	firstObjectID string
}

func newPreCurrentRaceStore(delegate customerdomain.AvatarObjectStore) *preCurrentRaceStore {
	return &preCurrentRaceStore{
		AvatarObjectStore: delegate,
		putStarted:        make(chan struct{}), putRelease: make(chan struct{}),
		deleteStarted: make(chan struct{}), deleteRelease: make(chan struct{}),
	}
}

func (s *preCurrentRaceStore) PutImmutable(
	ctx context.Context,
	key string,
	body customerdomain.AvatarContent,
	meta customerdomain.ObjectMeta,
) (customerdomain.PutResult, error) {
	result, err := s.AvatarObjectStore.PutImmutable(ctx, key, body, meta)
	if err == nil && result.Created {
		s.putOnce.Do(func() {
			s.firstKey = key
			_, _, ref, _ := customerdomain.ParseAvatarObjectKey(key)
			s.firstObjectID = ref.AvatarObjectID
			close(s.putStarted)
			<-s.putRelease
		})
	}
	return result, err
}

func (s *preCurrentRaceStore) Delete(ctx context.Context, key string) error {
	if key == s.firstKey {
		s.deleteOnce.Do(func() {
			close(s.deleteStarted)
			<-s.deleteRelease
		})
	}
	return s.AvatarObjectStore.Delete(ctx, key)
}
