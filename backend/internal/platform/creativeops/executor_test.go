package creativeops_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/google/uuid"

	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/jobs"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store/storetest"
)

// Compile the selected ADK in the real production dependency graph, in addition
// to the isolated checkpoint/skill probes retained with the design.
var _ = adk.NewChatModelAgent

func TestMain(m *testing.M) { storetest.Main(m, store.MigrateUp) }

type fixture struct {
	db    *sql.DB
	store *store.Store
	a, b  store.AccountScope
	url   string
}

func setup(t *testing.T) fixture {
	t.Helper()
	url := storetest.NewURL(t)
	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.Exec(`INSERT INTO accounts(id,password_hash,status) VALUES ('creative-a','test','active'),('creative-b','test','active');
 INSERT INTO creative_account_capabilities(account_id,read_enabled,manual_write_enabled) VALUES ('creative-a',true,true),('creative-b',true,true);
 CREATE TABLE creative_test_effects(account_id TEXT NOT NULL, marker TEXT NOT NULL, PRIMARY KEY(account_id,marker));`)
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)
	return fixture{db: db, store: st, url: url, a: st.ScopeFor(auth.AccountContext{AccountID: "creative-a"}), b: st.ScopeFor(auth.AccountContext{AccountID: "creative-b"})}
}
func operation() creativeops.Operation {
	return creativeops.Operation{Key: "test_add_v1", Capability: "manual_write", Validate: func(p json.RawMessage) error {
		var body struct {
			Marker string `json:"marker"`
		}
		if err := creativeops.Decode(p, &body); err != nil {
			return err
		}
		if body.Marker == "" {
			return creativeops.ErrValidation
		}
		return nil
	}, Apply: func(ctx context.Context, tx store.TxAccountScope, p json.RawMessage) (creativeops.Outcome, error) {
		var body struct {
			Marker string `json:"marker"`
		}
		if err := creativeops.Decode(p, &body); err != nil {
			return creativeops.Outcome{}, err
		}
		err := tx.Insert(ctx, "creative_test_effects", []string{"marker"}, body.Marker)
		return creativeops.Outcome{HTTPStatus: 201, Response: json.RawMessage(`{"saved":true}`), ResultKind: "test_effect"}, err
	}}
}
func command(marker string) creativeops.Command {
	p, _ := json.Marshal(map[string]string{"marker": marker})
	return creativeops.Command{OperationID: uuid.NewString(), CreatedAt: time.Now().UTC(), Payload: p}
}
func count(t *testing.T, f fixture, table string) int {
	t.Helper()
	var n int
	// Test-owned fixed table names only; never production query input.
	if err := f.db.QueryRow("SELECT count(*) FROM " + table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestConcurrentReceiptReplayAndIsolation(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	cmd := command("one")
	op := operation()
	exec := creativeops.Executor{}
	var wg sync.WaitGroup
	for range 12 {
		wg.Go(func() {
			r, err := exec.Run(ctx, f.a, op, cmd)
			if err != nil || r.Outcome.HTTPStatus != 201 {
				t.Errorf("replay: %+v %v", r, err)
			}
		})
	}
	wg.Wait()
	if n := count(t, f, "creative_test_effects"); n != 1 {
		t.Fatalf("effects=%d", n)
	}
	changed := cmd
	changed.Payload = json.RawMessage(`{"marker":"different"}`)
	if _, err := exec.Run(ctx, f.a, op, changed); !errors.Is(err, creativeops.ErrConflict) {
		t.Fatalf("different hash: %v", err)
	}
	if _, err := exec.Lookup(ctx, f.b, cmd.OperationID); !errors.Is(err, creativeops.ErrNotFound) {
		t.Fatalf("cross-account receipt: %v", err)
	}
	if _, err := exec.Run(ctx, f.b, op, cmd); err != nil {
		t.Fatal(err)
	}
	if n := count(t, f, "creative_test_effects"); n != 2 {
		t.Fatalf("account effects=%d", n)
	}
	// Old rollout rows cannot deny an active account; authentication still can.
	if _, err := f.db.Exec("UPDATE creative_account_capabilities SET manual_write_enabled=false WHERE account_id='creative-a'"); err != nil {
		t.Fatal(err)
	}
	if _, err := exec.Run(ctx, f.a, op, command("two")); err != nil {
		t.Fatalf("active account: %v", err)
	}
	if _, err := f.db.Exec("UPDATE accounts SET status='pending_verification' WHERE id='creative-a'"); err != nil {
		t.Fatal(err)
	}
	if _, err := exec.Run(ctx, f.a, op, command("three")); !errors.Is(err, store.ErrCreativeAccessDenied) {
		t.Fatalf("inactive account: %v", err)
	}

}

func TestRollbackValidationAndExpiry(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	exec := creativeops.Executor{}
	op := operation()
	cmd := command("rollback")
	original := op.Apply
	failure := errors.New("after effect before receipt")
	op.Apply = func(ctx context.Context, tx store.TxAccountScope, p json.RawMessage) (creativeops.Outcome, error) {
		if _, err := original(ctx, tx, p); err != nil {
			return creativeops.Outcome{}, err
		}
		return creativeops.Outcome{}, failure
	}
	if _, err := exec.Run(ctx, f.a, op, cmd); !errors.Is(err, failure) {
		t.Fatal(err)
	}
	if count(t, f, "creative_test_effects") != 0 || count(t, f, "creative_operation_receipts") != 0 {
		t.Fatal("rollback left effects")
	}
	op.Apply = original
	if _, err := exec.Run(ctx, f.a, op, cmd); err != nil {
		t.Fatal(err)
	}
	invalid := command("unknown")
	invalid.Payload = json.RawMessage(`{"marker":"unknown","account_id":"creative-b"}`)
	if _, err := exec.Run(ctx, f.a, op, invalid); !errors.Is(err, creativeops.ErrValidation) {
		t.Fatalf("extra field: %v", err)
	}
	old := command("old")
	old.CreatedAt = time.Now().Add(-91 * 24 * time.Hour)
	if _, err := exec.Run(ctx, f.a, op, old); !errors.Is(err, creativeops.ErrExpired) {
		t.Fatal(err)
	}
	if _, err := f.db.Exec("UPDATE creative_operation_receipts SET created_at=now()-interval '91 days',retained_until=now()-interval '1 day'"); err != nil {
		t.Fatal(err)
	}
	if _, err := exec.Lookup(ctx, f.a, cmd.OperationID); !errors.Is(err, creativeops.ErrExpired) {
		t.Fatal(err)
	}
}

func TestEinoAdapterUsesSameApplicationReceipt(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	cmd := command("eino")
	op := operation()
	exec := creativeops.Executor{}
	catalog, err := creativeops.NewCatalog([]creativeops.Definition{{Key: op.Key, Description: "添加测试内容", SchemaVersion: 1, Kind: "command", RequiredCapability: "manual_write", OutputSchema: json.RawMessage(`{"type":"object"}`), InputSchema: json.RawMessage(`{"type":"object","properties":{"marker":{"type":"string"}},"required":["marker"],"additionalProperties":false}`)}})
	if err != nil {
		t.Fatal(err)
	}
	tool, err := catalog.BindEino(op.Key, func(ctx context.Context, p json.RawMessage) (creativeops.Receipt, error) {
		c := cmd
		c.Payload = p
		return exec.Run(ctx, f.a, op, c)
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tool.Info(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := tool.InvokableRun(ctx, string(cmd.Payload)); err != nil {
		t.Fatal(err)
	}
	if _, err := exec.Run(ctx, f.a, op, cmd); err != nil {
		t.Fatal(err)
	}
	if count(t, f, "creative_test_effects") != 1 {
		t.Fatal("adapter bypassed application identity")
	}
	if _, err := catalog.BindEino("not_registered", nil); !errors.Is(err, creativeops.ErrNotFound) {
		t.Fatal(err)
	}
}

func TestAtomicQueueAndWorkerEffectReplay(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	if err := f.store.MigrateCreativeJobs(ctx); err != nil {
		t.Fatal(err)
	}
	var attempts atomic.Int64
	exec := creativeops.Executor{}
	op := operation()
	runtime, err := f.store.NewJobRuntime([]store.JobHandler{{Kind: "test_effect", Work: func(ctx context.Context, scope store.AccountScope, r jobs.Request) error {
		attempts.Add(1)
		_, err := exec.Run(ctx, scope, op, creativeops.Command{OperationID: r.OperationID, CreatedAt: r.CreatedAt, Payload: r.Payload})
		return err
	}}}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	request := jobs.Request{Kind: "test_effect", OperationID: uuid.NewString(), CreatedAt: time.Now(), Payload: json.RawMessage(`{"marker":"worker"}`)}
	rollback := errors.New("rollback queue")
	err = f.a.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		if err := tx.Insert(ctx, "creative_test_effects", []string{"marker"}, "rollback-acceptance"); err != nil {
			return err
		}
		if _, err := jobs.EnqueueInTx(ctx, tx.Jobs(runtime), request); err != nil {
			return err
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatal(err)
	}
	if count(t, f, "creative_jobs.river_job") != 0 || count(t, f, "creative_test_effects") != 0 {
		t.Fatal("job escaped rollback")
	}
	// Two deliveries of the same effect identity must execute one domain effect.
	for i := range 2 {
		if err := f.a.WithTxScope(ctx, func(tx store.TxAccountScope) error {
			if i == 0 {
				if err := tx.Insert(ctx, "creative_test_effects", []string{"marker"}, "committed-acceptance"); err != nil {
					return err
				}
			}
			_, err := jobs.EnqueueInTx(ctx, tx.Jobs(runtime), request)
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	var sameTx bool
	if err := f.db.QueryRow("SELECT e.xmin = j.xmin FROM creative_test_effects e CROSS JOIN creative_jobs.river_job j WHERE e.marker='committed-acceptance' ORDER BY j.id LIMIT 1").Scan(&sameTx); err != nil || !sameTx {
		t.Fatalf("business and queue use different transactions: %v %v", sameTx, err)
	}
	if err := runtime.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		stop, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := runtime.Stop(stop); err != nil {
			t.Error(err)
		}
	})
	deadline := time.Now().Add(15 * time.Second)
	for {
		var complete int
		if err := f.db.QueryRow("SELECT count(*) FROM creative_jobs.river_job WHERE state='completed'").Scan(&complete); err != nil {
			t.Fatal(err)
		}
		if complete == 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("completed %d, attempts %d", complete, attempts.Load())
		}
		time.Sleep(20 * time.Millisecond)
	}
	if count(t, f, "creative_test_effects") != 2 {
		t.Fatal("duplicate worker effect")
	}
	if _, err := exec.Lookup(ctx, f.b, request.OperationID); !errors.Is(err, creativeops.ErrNotFound) {
		t.Fatal(err)
	}
	// A forged runtime/empty transaction cannot smuggle a job into another database.
	if _, err := jobs.EnqueueInTx(ctx, (store.TxAccountScope{}).Jobs(runtime), request); err == nil {
		t.Fatal("zero scope enqueued")
	}
}

func TestRevisionJSON(t *testing.T) {
	for _, s := range []string{`0`, `1`, `"0"`, `"01"`, `"+1"`, `"9223372036854775808"`, `null`, `"1.1"`} {
		var r creativeops.Revision
		if json.Unmarshal([]byte(s), &r) == nil {
			t.Errorf("accepted %s", s)
		}
	}
	var r creativeops.Revision
	if err := json.Unmarshal([]byte(`"9223372036854775807"`), &r); err != nil {
		t.Fatal(err)
	}
	got, err := json.Marshal(r)
	if err != nil || string(got) != `"9223372036854775807"` {
		t.Fatalf("revision %s %v", got, err)
	}
	for _, s := range []string{`{"value":1} {}`, `{"value":1,"extra":2}`, `[]`} {
		var body struct {
			Value int `json:"value"`
		}
		if creativeops.Decode([]byte(s), &body) == nil {
			t.Errorf("accepted %s", s)
		}
	}
	id, err := creativeops.NewResourceID("cwnode")
	if err != nil || !strings.HasPrefix(id, "cwnode_") {
		t.Fatal(fmt.Sprint(id, err))
	}
}

func TestHeaderAndOperationIdentityMustMatch(t *testing.T) {
	id := "8a523818-8704-483a-9fe5-46b36be83ca8"
	for _, header := range []string{"", uuid.NewString(), strings.ToUpper(id)} {
		if err := creativeops.ValidateOperationKey(header, id); !errors.Is(err, creativeops.ErrValidation) {
			t.Fatalf("accepted header %q", header)
		}
	}
	if err := creativeops.ValidateOperationKey(id, id); err != nil {
		t.Fatal(err)
	}
}

func TestRejectsAmbiguousJSONBeforeEffects(t *testing.T) {
	f := setup(t)
	op := operation()
	exec := creativeops.Executor{}
	cmd := command("ambiguous")
	for _, payload := range []string{
		`{"marker":"A","marker":null}`,
		`{"marker":"B","marker":null}`,
		`{"marker":"A","Marker":"B"}`,
		`{"Marker":"B","marker":"A"}`,
	} {
		cmd.Payload = json.RawMessage(payload)
		if _, err := exec.Run(t.Context(), f.a, op, cmd); !errors.Is(err, creativeops.ErrValidation) {
			t.Errorf("accepted ambiguous JSON %s: %v", payload, err)
		}
	}
	for _, payload := range []string{`{"nested":{"x":1,"x":2}}`, `{"items":[{"name":"a","Name":"b"}]}`, `{"key":1,"Key":2}`} {
		var v map[string]any
		if err := creativeops.Decode([]byte(payload), &v); !errors.Is(err, creativeops.ErrValidation) {
			t.Errorf("nested ambiguity accepted: %s", payload)
		}
	}
	if count(t, f, "creative_test_effects") != 0 || count(t, f, "creative_operation_receipts") != 0 {
		t.Fatal("ambiguous payload committed")
	}
}
