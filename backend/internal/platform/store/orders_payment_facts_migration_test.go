package store_test

import (
	"context"
	"database/sql"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// TestOrdersPaymentFactsMigrationBackfillsExistingOrders 锁住 0033 的一次性 backfill 与
// 表级不变量：已结清单推定 amount=COALESCE(price,0)、outstanding=0；未结清单（含
// cancelled，按 epic 字面不排除）amount=0、outstanding=price（price NULL → NULL 不计入
// 待收合计）；paid_at 一律不回填；balance_paid=true ⇒ outstanding=0 由 CHECK 恒成立。
func TestOrdersPaymentFactsMigrationBackfillsExistingOrders(t *testing.T) {
	url := startPostgres(t)
	if err := store.MigrateStepsForTest(url, 32); err != nil {
		t.Fatalf("migrate pre-payment-facts schema: %v", err)
	}
	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatalf("open pre-payment-facts database: %v", err)
	}
	ctx := context.Background()

	const account = "pay-acct"
	if _, err := db.ExecContext(ctx,
		`INSERT INTO accounts (id, password_hash) VALUES ($1, 'hash')`, account); err != nil {
		t.Fatalf("insert account: %v", err)
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO customers (id, account_id, display_name, channel)
		 VALUES ('cus-pay', $1, 'legacy', 'other')`, account); err != nil {
		t.Fatalf("insert customer: %v", err)
	}

	type expectation struct {
		amountPaid      int
		outstanding     sql.NullInt64
		wantOutstanding bool
	}
	orders := []struct {
		id          string
		status      string
		price       any
		balancePaid bool
		expect      expectation
	}{
		// 已结清单：amount=COALESCE(price,0)、outstanding=0。
		{id: "ord-settled", status: "delivered", price: 1000, balancePaid: true,
			expect: expectation{amountPaid: 1000, outstanding: sql.NullInt64{Int64: 0, Valid: true}, wantOutstanding: true}},
		{id: "ord-settled-noprice", status: "delivered", price: nil, balancePaid: true,
			expect: expectation{amountPaid: 0, outstanding: sql.NullInt64{Int64: 0, Valid: true}, wantOutstanding: true}},
		// 未结清单（含 cancelled）：amount=0、outstanding=price；price NULL → NULL。
		{id: "ord-unsettled", status: "shot", price: 800, balancePaid: false,
			expect: expectation{amountPaid: 0, outstanding: sql.NullInt64{Int64: 800, Valid: true}, wantOutstanding: true}},
		{id: "ord-unsettled-noprice", status: "consulting", price: nil, balancePaid: false,
			expect: expectation{amountPaid: 0, wantOutstanding: false}},
		{id: "ord-cancelled-unsettled", status: "cancelled", price: 500, balancePaid: false,
			expect: expectation{amountPaid: 0, outstanding: sql.NullInt64{Int64: 500, Valid: true}, wantOutstanding: true}},
		{id: "ord-cancelled-settled", status: "cancelled", price: 300, balancePaid: true,
			expect: expectation{amountPaid: 300, outstanding: sql.NullInt64{Int64: 0, Valid: true}, wantOutstanding: true}},
	}
	for _, order := range orders {
		if _, err := db.ExecContext(ctx,
			`INSERT INTO orders (id, account_id, customer_id, status, price, balance_paid)
			 VALUES ($1, $2, 'cus-pay', $3, $4, $5)`,
			order.id, account, order.status, order.price, order.balancePaid); err != nil {
			t.Fatalf("insert order %s: %v", order.id, err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close pre-payment-facts database: %v", err)
	}

	if err := store.MigrateUp(url); err != nil {
		t.Fatalf("migrate payment-facts schema: %v", err)
	}
	db, err = sql.Open("pgx", url)
	if err != nil {
		t.Fatalf("reopen migrated database: %v", err)
	}
	for _, order := range orders {
		var amountPaid int
		var outstanding sql.NullInt64
		var paidAt sql.NullTime
		if err := db.QueryRowContext(ctx,
			`SELECT amount_paid, outstanding_amount, paid_at FROM orders WHERE id = $1`, order.id,
		).Scan(&amountPaid, &outstanding, &paidAt); err != nil {
			t.Fatalf("read backfilled order %s: %v", order.id, err)
		}
		if amountPaid != order.expect.amountPaid {
			t.Fatalf("order %s: amount_paid = %d, want %d", order.id, amountPaid, order.expect.amountPaid)
		}
		if outstanding.Valid != order.expect.wantOutstanding || outstanding.Int64 != order.expect.outstanding.Int64 {
			t.Fatalf("order %s: outstanding_amount = %v, want %v", order.id, outstanding, order.expect.outstanding)
		}
		if paidAt.Valid {
			t.Fatalf("order %s: paid_at = %v, want NULL（backfill 不发明收款时刻）", order.id, paidAt.Time)
		}
	}

	// 表级单向不变量：任何直写路径都不能让结清订单带非零 outstanding。
	if _, err := db.ExecContext(ctx,
		`UPDATE orders SET balance_paid = true WHERE id = 'ord-unsettled'`); err == nil {
		t.Fatal("settle unsettled order with outstanding=800 must violate table CHECK")
	}
	// 金额非负同样在列级 CHECK 兜底。
	if _, err := db.ExecContext(ctx,
		`UPDATE orders SET amount_paid = -1 WHERE id = 'ord-unsettled'`); err == nil {
		t.Fatal("negative amount_paid must violate column CHECK")
	}
	if _, err := db.ExecContext(ctx,
		`UPDATE orders SET outstanding_amount = -1 WHERE id = 'ord-unsettled'`); err == nil {
		t.Fatal("negative outstanding_amount must violate column CHECK")
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close before down migration: %v", err)
	}

	// 0033 之上已叠加 0034（归因快照）与 0035（健康度参数）：先退 0035、0034 再退 0033，
	// 本测试锁的是 0033 自身的可逆性。
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatal("generation media facts rollback", err)
	}
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatal("execution continuation rollback", err)
	}
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatal("agent control rollback", err)
	}
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatal("agent checkpoint rollback", err)
	}
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatalf("rollback agent-context migration: %v", err)
	}
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatalf("rollback agent-runs migration: %v", err)
	}
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatalf("rollback skill-foundation migration: %v", err)
	}
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatalf("rollback agent-conversations migration: %v", err)
	}
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatalf("rollback llm-gateway migration: %v", err)
	}
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatalf("rollback creative-canvas-events migration: %v", err)
	}
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatalf("rollback creative-media migration: %v", err)
	}
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatalf("rollback creative-canvas-commands migration: %v", err)
	}
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatalf("rollback creative-library-organization migration: %v", err)
	}
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatalf("rollback creative-text-canvas migration: %v", err)
	}
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatalf("rollback creative-foundation migration: %v", err)
	}
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatalf("rollback creative-workspace migration: %v", err)
	}
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatalf("rollback settings-health-tiers migration: %v", err)
	}
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatalf("rollback attribution-snapshot migration: %v", err)
	}
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatalf("rollback payment-facts migration: %v", err)
	}
	db, err = sql.Open("pgx", url)
	if err != nil {
		t.Fatalf("reopen rolled-back database: %v", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close rolled-back database: %v", err)
		}
	}()
	var columns int
	if err := db.QueryRowContext(ctx,
		`SELECT count(*) FROM information_schema.columns
		 WHERE table_name = 'orders' AND column_name IN ('amount_paid', 'outstanding_amount', 'paid_at')`,
	).Scan(&columns); err != nil {
		t.Fatalf("inspect rolled-back columns: %v", err)
	}
	if columns != 0 {
		t.Fatalf("rolled-back orders still has %d payment-facts columns", columns)
	}
}
