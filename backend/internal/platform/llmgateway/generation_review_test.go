package llmgateway_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	gw "github.com/samson/customer-manage-platform/backend/internal/platform/llmgateway"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

func generationInvoice(attemptID, key string, amount int64) gw.Measurement {
	return gw.Measurement{
		AttemptID: attemptID, Key: key, Component: gw.ComponentGenerationTotal, Currency: "USD",
		CostMicros: &amount, Certainty: "known", Kind: gw.EvidenceProviderReported,
		EvidenceDigest: sha256Digest(key),
	}
}

func (f *fixture) recordGenerationInvoice(t *testing.T, requestID string, m gw.Measurement) (gw.SettlementReceipt, error) {
	t.Helper()
	var receipt gw.SettlementReceipt
	err := f.a.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
		var err error
		receipt, err = f.gateway.RecordMeasurementInTx(t.Context(), tx, requestID, m)
		return err
	})
	return receipt, err
}

// 最终账单无须与本地推导值共享修订号；迟到用量也不能覆盖已经核实的金额。
func TestGenerationInvoiceOutranksDerivedUsage(t *testing.T) {
	for _, invoiceFirst := range []bool{false, true} {
		name := "usage first"
		if invoiceFirst {
			name = "invoice first"
		}
		t.Run(name, func(t *testing.T) {
			f := setupGateway(t, defaultBudget())
			p, _, _ := genPolicyFixture(t, nil)
			view, err := f.admitGeneration(t, f.a, genPrepared(t, p, newInt64(1000)), genAdmitOptions{})
			if err != nil {
				t.Fatal(err)
			}
			attempt := f.seedGenerationAttempt(t, "gw-a", view.ID)
			invoice := generationInvoice(attempt, "invoice", 900000)
			invoiceStep := func() {
				receipt, err := f.recordGenerationInvoice(t, view.ID, invoice)
				if err != nil || !receipt.Applied || receipt.NewBooked != 900000 {
					t.Fatalf("final invoice must apply: %+v, %v", receipt, err)
				}
			}
			usageStep := func() {
				receipt, err := f.recordGenerationUsage(t, f.a, view.ID, attempt, "usage", map[string]int64{"output_ms": 1000}, "99")
				if err != nil || receipt.Applied == invoiceFirst {
					t.Fatalf("usage priority: %+v, %v", receipt, err)
				}
			}
			if invoiceFirst {
				invoice.ProviderRev = "1"
				invoiceStep()
				usageStep()
			} else {
				usageStep()
				invoiceStep()
			}
			if reserved, spent := f.reservedMicros(t); reserved != 0 || spent != 900000 {
				t.Fatalf("final ledger: reserved=%d spent=%d", reserved, spent)
			}
			var kind string
			var rank int
			if err := f.db.QueryRow(`SELECT evidence_kind,evidence_rank FROM llm_usage_measurements
WHERE account_id='gw-a' AND attempt_id=$1 AND usage_dimensions->>'event_key'='usage'`, attempt).Scan(&kind, &rank); err != nil {
				t.Fatal(err)
			}
			if kind != string(gw.EvidenceDerivedFromUsage) || rank != 5 {
				t.Fatalf("derived evidence: kind=%s rank=%d", kind, rank)
			}
		})
	}
}

// 账单直接到达也须检查冻结上界，真实费用照实入账，阻断来源指向账单证据。
func TestGenerationInvoiceEnforcesFrozenUpperBound(t *testing.T) {
	for _, tc := range []struct {
		name    string
		amount  int64
		blocked bool
	}{{"at bound", 1250000, false}, {"over bound", 1375000, true}} {
		t.Run(tc.name, func(t *testing.T) {
			f := setupGateway(t, defaultBudget())
			p, _, _ := genPolicyFixture(t, nil)
			prepared := genPrepared(t, p, newInt64(1000))
			opts := genAdmitOptions{operationID: uuid.NewString()}
			view, err := f.admitGeneration(t, f.a, prepared, opts)
			if err != nil {
				t.Fatal(err)
			}
			attempt := f.seedGenerationAttempt(t, "gw-a", view.ID)
			invoice := generationInvoice(attempt, "invoice-bound", tc.amount)
			receipt, err := f.recordGenerationInvoice(t, view.ID, invoice)
			if err != nil || !receipt.Applied || receipt.NewBooked != tc.amount || receipt.NewHold != 0 {
				t.Fatalf("invoice receipt: %+v, %v", receipt, err)
			}
			if reserved, spent := f.reservedMicros(t); reserved != 0 || spent != tc.amount {
				t.Fatalf("invoice ledger: reserved=%d spent=%d", reserved, spent)
			}
			if tc.blocked {
				var source string
				if err := f.db.QueryRow(`SELECT source_measurement_id FROM llm_generation_deployment_blocks WHERE account_id='gw-a'`).Scan(&source); err != nil {
					t.Fatal(err)
				}
				if source != receipt.MeasurementID {
					t.Fatalf("block source=%s invoice=%s", source, receipt.MeasurementID)
				}
			}
			if _, err := f.admitGeneration(t, f.a, prepared, genAdmitOptions{}); tc.blocked && !errors.Is(err, gw.ErrUnavailable) || !tc.blocked && err != nil {
				t.Fatalf("admission after invoice: %v", err)
			}
			if replay, err := f.admitGeneration(t, f.a, prepared, opts); err != nil || replay.ID != view.ID {
				t.Fatalf("admission replay: %+v, %v", replay, err)
			}
			if replay, err := f.recordGenerationInvoice(t, view.ID, invoice); err != nil || replay.Applied {
				t.Fatalf("invoice replay: %+v, %v", replay, err)
			}
		})
	}
}

// 超界账单的阻断和账本同事务回滚；未落账的异币种、未知或低等级证据不得阻断。
func TestGenerationInvoiceBlockIsAtomicAndRequiresAppliedCost(t *testing.T) {
	f := setupGateway(t, defaultBudget())
	p, _, _ := genPolicyFixture(t, nil)
	prepared := genPrepared(t, p, newInt64(1000))
	view, err := f.admitGeneration(t, f.a, prepared, genAdmitOptions{})
	if err != nil {
		t.Fatal(err)
	}
	attempt := f.seedGenerationAttempt(t, "gw-a", view.ID)
	rollback := errors.New("rollback invoice")
	err = f.a.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
		if _, err := f.gateway.RecordMeasurementInTx(t.Context(), tx, view.ID, generationInvoice(attempt, "rollback", 1375000)); err != nil {
			return err
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatal(err)
	}
	if reserved, spent := f.reservedMicros(t); reserved != 1250000 || spent != 0 {
		t.Fatalf("rollback ledger: reserved=%d spent=%d", reserved, spent)
	}
	for _, table := range []string{"llm_usage_measurements", "llm_cost_positions", "llm_settlement_receipts", "llm_generation_deployment_blocks"} {
		if n := queryCount(t, f.db, `SELECT count(*) FROM `+table+` WHERE account_id='gw-a'`); n != 0 {
			t.Fatalf("%s survived rollback: %d", table, n)
		}
	}
	if _, err := f.recordGenerationInvoice(t, view.ID, generationInvoice(attempt, "original", 900000)); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		mutate func(*gw.Measurement)
	}{
		{"foreign currency", func(m *gw.Measurement) { m.Currency = "EUR" }},
		{"lower rank", func(m *gw.Measurement) { m.Kind = gw.EvidenceOperatorAttested }},
		{"unknown", func(m *gw.Measurement) { m.Certainty, m.CostMicros = "unknown", nil }},
		{"unordered invoice", func(_ *gw.Measurement) {}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := generationInvoice(attempt, tc.name, 1375000)
			tc.mutate(&m)
			if receipt, err := f.recordGenerationInvoice(t, view.ID, m); err != nil || receipt.Applied {
				t.Fatalf("unapplied evidence: %+v, %v", receipt, err)
			}
			if n := queryCount(t, f.db, `SELECT count(*) FROM llm_generation_deployment_blocks WHERE account_id='gw-a'`); n != 0 {
				t.Fatalf("unapplied evidence blocked admission: %d", n)
			}
		})
	}
	if reserved, spent := f.reservedMicros(t); reserved != 0 || spent != 900000 {
		t.Fatalf("unapplied evidence changed ledger: reserved=%d spent=%d", reserved, spent)
	}
}

// 控制信封按首次受理输入冻结；同一时刻的不同时区表示仍是相同输入。
func TestGenerationReplayValidatesFrozenControls(t *testing.T) {
	f := setupGateway(t, defaultBudget())
	p, _, _ := genPolicyFixture(t, nil)
	in := gw.GenerationAdmission{
		CallerService: "creative_agent", CallerOperationID: uuid.NewString(), CallerGroupID: "run-A",
		GroupLimitMicros: 5000000, GroupTokenLimit: 1000, GroupDeadline: f.now.Add(time.Hour),
		AccountLimitMicros: 10000000, AccountTokenLimit: 2000,
		ConsumerKey: "canvas:original", Deadline: f.now.Add(time.Hour + 123*time.Nanosecond), Prepared: genPrepared(t, p, newInt64(1000)),
	}
	admit := func(input gw.GenerationAdmission) (gw.RequestView, error) {
		var view gw.RequestView
		err := f.a.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
			var err error
			view, err = f.gateway.PrepareGenerationInTx(t.Context(), tx, input)
			return err
		})
		return view, err
	}
	view, err := admit(in)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		mutate func(*gw.GenerationAdmission)
	}{
		{"group", func(in *gw.GenerationAdmission) { in.CallerGroupID = "run-B" }},
		{"deadline", func(in *gw.GenerationAdmission) { in.Deadline = in.Deadline.Add(time.Second) }},
		{"group deadline", func(in *gw.GenerationAdmission) { in.GroupDeadline = in.GroupDeadline.Add(time.Second) }},
		{"consumer", func(in *gw.GenerationAdmission) { in.ConsumerKey = "canvas:other" }},
		{"group amount limit", func(in *gw.GenerationAdmission) { in.GroupLimitMicros++ }},
		{"group token limit", func(in *gw.GenerationAdmission) { in.GroupTokenLimit++ }},
		{"account amount limit", func(in *gw.GenerationAdmission) { in.AccountLimitMicros++ }},
		{"account token limit", func(in *gw.GenerationAdmission) { in.AccountTokenLimit++ }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			changed := in
			tc.mutate(&changed)
			if replay, err := admit(changed); !errors.Is(err, gw.ErrConflict) {
				t.Fatalf("changed controls must conflict: %+v, %v", replay, err)
			}
		})
	}
	// 当前consumer状态及组配置变化不改写首次输入，不影响原operation的回读。
	if _, err := f.db.Exec(`UPDATE llm_result_consumers SET state='consumed',released_at=$2 WHERE account_id='gw-a' AND request_id=$1`, view.ID, f.now); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(`UPDATE llm_call_groups SET limit_micros=6000000,deadline_at=$1 WHERE account_id='gw-a'`, f.now.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	same := in
	same.Deadline = same.Deadline.In(time.FixedZone("offset", 8*3600))
	same.GroupDeadline = same.GroupDeadline.In(time.FixedZone("offset", 8*3600))
	f.now = f.now.Add(3 * time.Hour)
	if replay, err := admit(same); err != nil || replay.ID != view.ID {
		t.Fatalf("identical controls must replay after deadline: %+v, %v", replay, err)
	}
	if n := queryCount(t, f.db, `SELECT count(*) FROM llm_usage_reservations WHERE account_id='gw-a'`); n != 1 {
		t.Fatalf("control conflicts changed reservations: %d", n)
	}
	if reserved, spent := f.reservedMicros(t); reserved != 1250000 || spent != 0 {
		t.Fatalf("control conflicts changed budget: reserved=%d spent=%d", reserved, spent)
	}
}

// 首次受理尚未提交时，并发重放在组锁后也必须重新核对consumer绑定。
func TestGenerationReplayChecksControlsAfterGroupWait(t *testing.T) {
	f := setupGateway(t, defaultBudget())
	p, _, _ := genPolicyFixture(t, nil)
	in := gw.GenerationAdmission{
		CallerService: "creative_agent", CallerOperationID: uuid.NewString(), CallerGroupID: "concurrent-controls",
		GroupLimitMicros: 5000000, GroupTokenLimit: 1000, GroupDeadline: f.now.Add(time.Hour),
		ConsumerKey: "canvas:first", Deadline: f.now.Add(time.Hour), Prepared: genPrepared(t, p, newInt64(1000)),
	}
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	holding, proceed := make(chan struct{}), make(chan struct{})
	firstDone := make(chan error, 1)
	go func() {
		firstDone <- f.a.WithTxScope(ctx, func(tx store.TxAccountScope) error {
			if _, err := f.gateway.PrepareGenerationInTx(ctx, tx, in); err != nil {
				return err
			}
			close(holding)
			select {
			case <-proceed:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	}()
	select {
	case <-holding:
	case err := <-firstDone:
		t.Fatalf("first admission: %v", err)
	}
	secondDone := make(chan error, 1)
	go func() {
		changed := in
		changed.ConsumerKey = "canvas:second"
		secondDone <- f.a.WithTxScope(ctx, func(tx store.TxAccountScope) error {
			_, err := f.gateway.PrepareGenerationInTx(ctx, tx, changed)
			return err
		})
	}()
	deadline := time.Now().Add(5 * time.Second)
	waited := false
	for time.Now().Before(deadline) {
		var n int
		if err := f.db.QueryRowContext(ctx, `SELECT count(*) FROM pg_stat_activity
WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE '%llm_call_groups%'`).Scan(&n); err != nil {
			break
		} else if n > 0 {
			waited = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	close(proceed)
	firstErr, secondErr := <-firstDone, <-secondDone
	if !waited || firstErr != nil || !errors.Is(secondErr, gw.ErrConflict) {
		t.Fatalf("concurrent replay: waited=%v first=%v second=%v", waited, firstErr, secondErr)
	}
	if n := queryCount(t, f.db, `SELECT count(*) FROM llm_usage_reservations WHERE account_id='gw-a'`); n != 1 {
		t.Fatalf("concurrent conflict changed reservations: %d", n)
	}
}
