// 第三批：真实 PostgreSQL 上的协议实验，不是生产 Harness/Gateway 的实现测试。
package cmdesignprobe

import (
	"database/sql"
	"fmt"
	"os"
	"sync"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store/storetest"
)

func TestMain(m *testing.M) { storetest.Main(m, func(string) error { return nil }) }
func setup(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("pgx", storetest.NewURL(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	b, err := os.ReadFile(os.Getenv("CREATIVE_GATEWAY_HARNESS_SCHEMA"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(string(b)); err != nil {
		t.Fatal(err)
	}
	return db
}
func exec(t *testing.T, db *sql.DB, q string, args ...any) {
	t.Helper()
	if _, err := db.Exec(q, args...); err != nil {
		t.Fatal(err)
	}
}
func scalar(t *testing.T, db *sql.DB, q string, args ...any) int64 {
	t.Helper()
	var n int64
	if err := db.QueryRow(q, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
func TestReservationClaimAndPartialSettlement(t *testing.T) {
	db := setup(t)
	exec(t, db, `INSERT INTO llm_budgets(account_id,period_start,currency,limit_micros) VALUES('a','2026-09-01','USD',20),('b','2026-09-01','USD',20)`)
	// 同稳定调用方身份的两次初始预留只占一次；条件修改与建立预留同事务。
	for i := 0; i < 2; i++ {
		tx, err := db.Begin()
		if err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec(`SELECT 1 FROM llm_budgets WHERE account_id='a' AND period_start='2026-09-01' AND currency='USD' FOR UPDATE`); err != nil {
			t.Fatal(err)
		}
		var exists bool
		if err = tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM llm_usage_reservations WHERE account_id='a' AND caller_service='creative' AND caller_operation_id='op')`).Scan(&exists); err != nil {
			t.Fatal(err)
		}
		if !exists {
			res, e := tx.Exec(`UPDATE llm_budgets SET reserved_micros=reserved_micros+20 WHERE account_id='a' AND period_start='2026-09-01' AND currency='USD' AND spent_micros+reserved_micros+20<=limit_micros`)
			if e != nil {
				t.Fatal(e)
			}
			n, e := res.RowsAffected()
			if e != nil || n != 1 {
				t.Fatalf("reserve %d %v", n, e)
			}
			if _, err = tx.Exec(`INSERT INTO llm_usage_reservations VALUES('v','a','creative','op',NULL,'2026-09-01','USD',20,20)`); err != nil {
				t.Fatal(err)
			}
		}
		if err = tx.Commit(); err != nil {
			t.Fatal(err)
		}
	}
	exec(t, db, `INSERT INTO llm_requests(id,account_id,caller_service,caller_operation_id,request_hash,state) VALUES('q','a','creative','call','hash','prepared')`)
	exec(t, db, `UPDATE llm_usage_reservations SET request_id='q' WHERE account_id='a' AND id='v' AND (request_id IS NULL OR request_id='q')`)
	if n := scalar(t, db, `SELECT reserved_micros FROM llm_budgets WHERE account_id='a'`); n != 20 {
		t.Fatalf("double reserve: %d", n)
	}
	if n := scalar(t, db, `SELECT reserved_micros FROM llm_budgets WHERE account_id='b'`); n != 0 {
		t.Fatalf("other account: %d", n)
	}
	if _, err := db.Exec(`INSERT INTO llm_usage_reservations VALUES('v2','a','creative','other','q','2026-09-01','USD',20,20)`); err == nil {
		t.Fatal("double claim accepted")
	}
	// 部分报告：6已知+14保留；全量报告9后释放hold，事务失败不能半结算。
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(`UPDATE llm_budgets SET spent_micros=6,reserved_micros=14 WHERE account_id='a'; UPDATE llm_usage_reservations SET remaining_hold_micros=14 WHERE account_id='a' AND id='v'`); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if n := scalar(t, db, `SELECT spent_micros+reserved_micros FROM llm_budgets WHERE account_id='a'`); n != 20 {
		t.Fatalf("lost unknown hold %d", n)
	}
	tx, err = db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(`UPDATE llm_budgets SET spent_micros=9,reserved_micros=0 WHERE account_id='a'; UPDATE llm_usage_reservations SET remaining_hold_micros=0 WHERE account_id='a' AND id='v'`); err != nil {
		t.Fatal(err)
	}
	if err = tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if n := scalar(t, db, `SELECT remaining_hold_micros FROM llm_usage_reservations WHERE id='v' AND account_id='a'`); n != 14 {
		t.Fatalf("partial rollback %d", n)
	}
}

func TestConcurrentCostCorrection(t *testing.T) {
	db := setup(t)
	exec(t, db, `INSERT INTO llm_budgets(account_id,period_start,currency,limit_micros,spent_micros) VALUES('a','2026-09-01','USD',100,10);
 INSERT INTO llm_usage_measurements VALUES('m0','a','attempt','base','total','USD',NULL,10),('m8','a','attempt','eight','total','USD','m0',8),('m7','a','attempt','seven','total','USD','m0',7);
 INSERT INTO llm_cost_positions VALUES('a','attempt','total','USD','m0',10,1)`)
	// 接受当前证据的更正才调账；并发分叉必须有一方被拒绝，不能按同一旧10扣两遍。
	apply := func(id, expected string, cost int64) (bool, error) {
		tx, err := db.Begin()
		if err != nil {
			return false, err
		}
		defer tx.Rollback()
		if _, err = tx.Exec(`SELECT 1 FROM llm_budgets WHERE account_id='a' AND period_start='2026-09-01' AND currency='USD' FOR UPDATE`); err != nil {
			return false, err
		}
		var current string
		var old, rev int64
		if err = tx.QueryRow(`SELECT current_measurement_id,booked_cost_micros,revision FROM llm_cost_positions WHERE account_id='a' AND attempt_id='attempt' AND cost_component='total' AND currency='USD' FOR UPDATE`).Scan(&current, &old, &rev); err != nil {
			return false, err
		}
		if current != expected {
			return false, nil
		}
		if _, err = tx.Exec(`UPDATE llm_cost_positions SET current_measurement_id=$1,booked_cost_micros=$2,revision=revision+1 WHERE account_id='a' AND attempt_id='attempt' AND cost_component='total' AND currency='USD'`, id, cost); err != nil {
			return false, err
		}
		if _, err = tx.Exec(`UPDATE llm_budgets SET spent_micros=spent_micros+$1,revision=revision+1 WHERE account_id='a' AND period_start='2026-09-01' AND currency='USD'`, cost-old); err != nil {
			return false, err
		}
		if _, err = tx.Exec(`INSERT INTO llm_settlement_receipts VALUES('a','attempt','total','USD',$1,$2,$3,$4)`, rev+1, id, old, cost); err != nil {
			return false, err
		}
		return true, tx.Commit()
	}
	start := make(chan struct{})
	type outcome struct {
		ok  bool
		err error
	}
	results := make(chan outcome, 2)
	var wg sync.WaitGroup
	for _, v := range []struct {
		id   string
		cost int64
	}{{"m8", 8}, {"m7", 7}} {
		wg.Add(1)
		go func(id string, cost int64) {
			defer wg.Done()
			<-start
			ok, err := apply(id, "m0", cost)
			results <- outcome{ok, err}
		}(v.id, v.cost)
	}
	close(start)
	wg.Wait()
	close(results)
	wins := 0
	for r := range results {
		if r.err != nil {
			t.Fatal(r.err)
		}
		if r.ok {
			wins++
		}
	}
	if wins != 1 {
		t.Fatalf("expected exactly one winner, got %d", wins)
	}
	var current string
	var booked int64
	if err := db.QueryRow(`SELECT current_measurement_id,booked_cost_micros FROM llm_cost_positions WHERE account_id='a'`).Scan(&current, &booked); err != nil {
		t.Fatal(err)
	}
	if spent := scalar(t, db, `SELECT spent_micros FROM llm_budgets WHERE account_id='a'`); spent != booked || (spent != 7 && spent != 8) {
		t.Fatalf("wrong adjustment spent=%d booked=%d", spent, booked)
	}
	if ok, err := apply(current, "m0", booked); err != nil || ok {
		t.Fatalf("duplicate applied %v %v", ok, err)
	}
	exec(t, db, `INSERT INTO llm_usage_measurements VALUES('verified','a','attempt','verified','total','USD',$1,7)`, current)
	if ok, err := apply("verified", current, 7); err != nil || !ok {
		t.Fatalf("verified correction %v %v", ok, err)
	}
	if spent := scalar(t, db, `SELECT spent_micros FROM llm_budgets WHERE account_id='a'`); spent != 7 {
		t.Fatalf("final %d", spent)
	}
	if n := scalar(t, db, `SELECT count(*) FROM llm_settlement_receipts`); n != 2 {
		t.Fatalf("duplicate receipt %d", n)
	}
}

func seedRun(t *testing.T, db *sql.DB) {
	exec(t, db, `INSERT INTO creative_agent_runs(id,account_id,state,execution_epoch,claim_token,lease_until,deadline_at) VALUES('run','a','running',1,'old',now()+interval '1 hour',now()+interval '2 hours'); INSERT INTO creative_agent_slots VALUES('a','run','old'); INSERT INTO llm_requests(id,account_id,caller_service,caller_operation_id,request_hash,state) VALUES('q','a','creative','call','hash','prepared')`)
}
func TestCancelFenceAndDispatchIntent(t *testing.T) {
	for _, cancelFirst := range []bool{true, false} {
		t.Run(fmt.Sprint(cancelFirst), func(t *testing.T) {
			db := setup(t)
			seedRun(t, db)
			cancel := func() {
				exec(t, db, `BEGIN; SELECT 1 FROM creative_agent_slots WHERE account_id='a' FOR UPDATE; UPDATE creative_agent_runs SET execution_epoch=execution_epoch+1,cancel_requested_at=clock_timestamp(),lease_until=NULL WHERE account_id='a' AND id='run'; UPDATE llm_requests SET cancel_requested_at=clock_timestamp() WHERE account_id='a' AND id='q'; COMMIT`)
			}
			dispatch := func() bool {
				tx, err := db.Begin()
				if err != nil {
					t.Fatal(err)
				}
				defer tx.Rollback()
				if _, err = tx.Exec(`SELECT 1 FROM creative_agent_slots WHERE account_id='a' FOR UPDATE`); err != nil {
					t.Fatal(err)
				}
				var valid bool
				if err = tx.QueryRow(`SELECT execution_epoch=1 AND cancel_requested_at IS NULL AND lease_until>clock_timestamp() AND deadline_at>clock_timestamp() FROM creative_agent_runs WHERE account_id='a' AND id='run' FOR UPDATE`).Scan(&valid); err != nil {
					t.Fatal(err)
				}
				if !valid {
					return false
				}
				res, err := tx.Exec(`UPDATE llm_requests SET state='dispatching' WHERE account_id='a' AND id='q' AND state='prepared' AND cancel_requested_at IS NULL`)
				if err != nil {
					t.Fatal(err)
				}
				n, err := res.RowsAffected()
				if err != nil {
					t.Fatal(err)
				}
				if n != 1 {
					return false
				}
				if _, err = tx.Exec(`INSERT INTO llm_attempts VALUES('a1','a','q',1,'permit1','dispatching')`); err != nil {
					t.Fatal(err)
				}
				if err = tx.Commit(); err != nil {
					t.Fatal(err)
				}
				return true
			}
			if cancelFirst {
				cancel()
				if dispatch() {
					t.Fatal("cancelled run dispatched")
				}
			} else {
				if !dispatch() {
					t.Fatal("valid dispatch rejected")
				}
				cancel()
				exec(t, db, `UPDATE llm_attempts SET dispatch_state='unknown' WHERE id='a1'; UPDATE llm_requests SET state='unknown' WHERE id='q'`)
				if _, err := db.Exec(`INSERT INTO llm_attempts VALUES('a2','a','q',2,'permit2','dispatching')`); err == nil {
					t.Fatal("unknown attempt resent")
				}
			}
			exec(t, db, `UPDATE creative_agent_slots SET run_id='next',claim_token='new' WHERE account_id='a'`)
			res, err := db.Exec(`UPDATE creative_agent_slots SET run_id=NULL,claim_token=NULL WHERE account_id='a' AND run_id='run' AND claim_token='old'`)
			if err != nil {
				t.Fatal(err)
			}
			n, err := res.RowsAffected()
			if err != nil || n != 0 {
				t.Fatalf("old worker released new slot %d %v", n, err)
			}
		})
	}
}
func TestEventRollbackAndPruneCursor(t *testing.T) {
	db := setup(t)
	seedRun(t, db)
	appendEvent := func(commit bool) {
		tx, err := db.Begin()
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		var seq int64
		if err = tx.QueryRow(`UPDATE creative_agent_runs SET last_event_seq=last_event_seq+1 WHERE account_id='a' AND id='run' RETURNING last_event_seq`).Scan(&seq); err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec(`INSERT INTO creative_run_events(account_id,run_id,seq,event_type) VALUES('a','run',$1,'step.state_changed')`, seq); err != nil {
			t.Fatal(err)
		}
		if commit {
			if err = tx.Commit(); err != nil {
				t.Fatal(err)
			}
		}
	}
	appendEvent(true)
	appendEvent(false)
	appendEvent(true)
	if n := scalar(t, db, `SELECT last_event_seq FROM creative_agent_runs WHERE account_id='a' AND id='run'`); n != 2 {
		t.Fatalf("rollback left sequence %d", n)
	}
	if n := scalar(t, db, `SELECT count(*) FROM creative_run_events WHERE account_id='a' AND run_id='run' AND seq>1`); n != 1 {
		t.Fatalf("missed snapshot tail %d", n)
	}
	exec(t, db, `BEGIN; DELETE FROM creative_run_events WHERE account_id='a' AND run_id='run' AND seq<=2; UPDATE creative_agent_runs SET pruned_through_seq=2 WHERE account_id='a' AND id='run'; COMMIT`)
	if n := scalar(t, db, `SELECT count(*) FROM creative_agent_runs WHERE account_id='a' AND id='run' AND 1<pruned_through_seq`); n != 1 {
		t.Fatal("empty event table lost stale cursor evidence")
	}
	if _, err := db.Exec(`UPDATE creative_agent_runs SET pruned_through_seq=3 WHERE id='run'`); err == nil {
		t.Fatal("invalid prune watermark accepted")
	}
}
func TestResultConsumptionAtomicity(t *testing.T) {
	db := setup(t)
	seedRun(t, db)
	exec(t, db, `UPDATE llm_requests SET state='succeeded',result_payload='{"tool_calls":[{"name":"CreateTextNodes"}]}',result_hash='complete' WHERE account_id='a' AND id='q'; INSERT INTO llm_result_consumers VALUES('a','q','creative','model-step','pending')`)
	consume := func(commit bool) {
		tx, err := db.Begin()
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		var state string
		if err = tx.QueryRow(`SELECT state FROM llm_result_consumers WHERE account_id='a' AND request_id='q' AND caller_service='creative' AND consumer_key='model-step' FOR UPDATE`).Scan(&state); err != nil {
			t.Fatal(err)
		}
		if state == "consumed" {
			return
		}
		if _, err = tx.Exec(`INSERT INTO creative_agent_steps VALUES('tool-step','a','run','model-step',0,'hash'); UPDATE llm_result_consumers SET state='consumed' WHERE account_id='a' AND request_id='q' AND caller_service='creative' AND consumer_key='model-step'`); err != nil {
			t.Fatal(err)
		}
		if commit {
			if err = tx.Commit(); err != nil {
				t.Fatal(err)
			}
		}
	}
	consume(false)
	if n := scalar(t, db, `SELECT count(*) FROM creative_agent_steps`); n != 0 {
		t.Fatal("failed consume left tool plan")
	}
	if n := scalar(t, db, `SELECT count(*) FROM llm_result_consumers WHERE state='pending'`); n != 1 {
		t.Fatal("failed consume released hold")
	}
	consume(true)
	consume(true)
	if n := scalar(t, db, `SELECT count(*) FROM creative_agent_steps`); n != 1 {
		t.Fatalf("duplicate tool plans %d", n)
	}
	if n := scalar(t, db, `SELECT count(*) FROM llm_result_consumers WHERE state='pending'`); n != 0 {
		t.Fatal("successful consumption leaked hold")
	}
	if _, err := db.Exec(`INSERT INTO creative_agent_steps VALUES('other','a','run','model-step',0,'hash')`); err == nil {
		t.Fatal("duplicate tool-call identity accepted")
	}
	var fks int
	if err := db.QueryRow(`SELECT count(*) FROM pg_constraint WHERE contype='f' AND connamespace='public'::regnamespace`).Scan(&fks); err != nil || fks != 0 {
		t.Fatalf("foreign keys %d %v", fks, err)
	}
}

func TestVerifiedRejectionRearmsOnlyWithBudget(t *testing.T) {
	db := setup(t)
	seedRun(t, db)
	exec(t, db, `INSERT INTO llm_budgets(account_id,period_start,currency,limit_micros,reserved_micros) VALUES('a','2026-09-01','USD',20,20);
 INSERT INTO llm_usage_reservations VALUES('v','a','creative','call','q','2026-09-01','USD',20,20,1);
 UPDATE llm_requests SET state='unknown' WHERE account_id='a' AND id='q';
 INSERT INTO llm_attempts VALUES('a1','a','q',1,'p1','unknown',1)`)
	// 可靠核实为未受理/无费用：释放旧hold、同request转prepared，绝不终结为无法恢复的failed。
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(`SELECT 1 FROM llm_budgets WHERE account_id='a' AND period_start='2026-09-01' AND currency='USD' FOR UPDATE;
 UPDATE llm_requests SET state='prepared' WHERE account_id='a' AND id='q' AND state='unknown';
 UPDATE llm_usage_reservations SET remaining_hold_micros=0 WHERE account_id='a' AND id='v';
 UPDATE llm_attempts SET dispatch_state='rejected' WHERE account_id='a' AND id='a1';
 INSERT INTO llm_usage_measurements VALUES('zero','a','a1','verified-unaccepted','total','USD',NULL,0);
 UPDATE llm_budgets SET reserved_micros=0 WHERE account_id='a' AND period_start='2026-09-01' AND currency='USD'`); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	// B在间隙用掉全部余额。
	exec(t, db, `BEGIN; UPDATE llm_budgets SET reserved_micros=20 WHERE account_id='a' AND period_start='2026-09-01' AND currency='USD'; INSERT INTO llm_usage_reservations VALUES('vb','a','other','other-call',NULL,'2026-09-01','USD',20,20,1); COMMIT`)
	rearm := func() bool {
		tx, err := db.Begin()
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		if _, err = tx.Exec(`SELECT 1 FROM llm_budgets WHERE account_id='a' AND period_start='2026-09-01' AND currency='USD' FOR UPDATE`); err != nil {
			t.Fatal(err)
		}
		var state string
		if err = tx.QueryRow(`SELECT state FROM llm_requests WHERE account_id='a' AND id='q' AND cancel_requested_at IS NULL FOR UPDATE`).Scan(&state); err != nil {
			t.Fatal(err)
		}
		if state != "prepared" {
			return false
		}
		var hold, generation int64
		if err = tx.QueryRow(`SELECT remaining_hold_micros,hold_generation FROM llm_usage_reservations WHERE account_id='a' AND id='v' FOR UPDATE`).Scan(&hold, &generation); err != nil {
			t.Fatal(err)
		}
		if hold != 0 || generation != 1 {
			t.Fatalf("unexpected retry generation %d hold %d", generation, hold)
		}
		var proof bool
		if err = tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM llm_attempts a JOIN llm_usage_measurements m ON m.account_id=a.account_id AND m.attempt_id=a.id WHERE a.account_id='a' AND a.request_id='q' AND a.attempt_number=1 AND a.dispatch_state='rejected' AND m.measurement_key='verified-unaccepted' AND m.cost_micros=0)`).Scan(&proof); err != nil {
			t.Fatal(err)
		}
		if !proof {
			t.Fatal("missing rejection proof")
		}
		res, err := tx.Exec(`UPDATE llm_budgets SET reserved_micros=reserved_micros+20 WHERE account_id='a' AND period_start='2026-09-01' AND currency='USD' AND spent_micros+reserved_micros+20<=limit_micros`)
		if err != nil {
			t.Fatal(err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			t.Fatal(err)
		}
		if n == 0 {
			return false
		}
		if _, err = tx.Exec(`UPDATE llm_usage_reservations SET remaining_hold_micros=20,hold_generation=2 WHERE account_id='a' AND id='v' AND hold_generation=1;
 INSERT INTO llm_attempts VALUES('a2','a','q',2,'p2','dispatching',2);
 UPDATE llm_requests SET state='dispatching' WHERE account_id='a' AND id='q'`); err != nil {
			t.Fatal(err)
		}
		if err = tx.Commit(); err != nil {
			t.Fatal(err)
		}
		return true
	}
	if rearm() {
		t.Fatal("retry dispatched without budget")
	}
	if n := scalar(t, db, `SELECT count(*) FROM llm_attempts WHERE account_id='a' AND request_id='q'`); n != 1 {
		t.Fatal("budget denial created attempt")
	}
	if n := scalar(t, db, `SELECT remaining_hold_micros FROM llm_usage_reservations WHERE account_id='a' AND id='v'`); n != 0 {
		t.Fatal("budget denial changed hold")
	}
	exec(t, db, `BEGIN; UPDATE llm_budgets SET reserved_micros=reserved_micros-20 WHERE account_id='a' AND period_start='2026-09-01' AND currency='USD'; UPDATE llm_usage_reservations SET remaining_hold_micros=0 WHERE account_id='a' AND id='vb'; COMMIT`)
	if !rearm() {
		t.Fatal("available budget did not rearm")
	}
	if rearm() {
		t.Fatal("repeated dispatch got second permit")
	}
	if n := scalar(t, db, `SELECT count(*) FROM llm_requests WHERE account_id='a' AND id='q'`); n != 1 {
		t.Fatal("retry changed logical request")
	}
	if n := scalar(t, db, `SELECT reserved_micros FROM llm_budgets WHERE account_id='a'`); n != 20 {
		t.Fatalf("retry reservation %d", n)
	}
	if n := scalar(t, db, `SELECT count(*) FROM llm_attempts WHERE account_id='a' AND request_id='q' AND reservation_generation=2 AND dispatch_state='dispatching'`); n != 1 {
		t.Fatal("missing generation-bound attempt")
	}
}
