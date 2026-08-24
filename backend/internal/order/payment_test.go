package order

import (
	"errors"
	"testing"
	"time"

	"github.com/oapi-codegen/nullable"
)

func pint(value int) *int { return &value }

var (
	paymentShotAt      = time.Date(2026, 8, 1, 2, 0, 0, 0, time.UTC)
	paymentDeliveredAt = time.Date(2026, 8, 10, 2, 0, 0, 0, time.UTC)
)

func mustOutstanding(t *testing.T, order Order) (int, bool) {
	t.Helper()
	if order.OutstandingAmount == nil {
		return 0, false
	}
	return *order.OutstandingAmount, true
}

// DEC-10：建单即视为 amount_paid=0 已录入，未结清且未显式给 outstanding 时推定
// max(price−amount_paid, 0)——新单 price 落地即待收。
func TestPaymentCreateInfersOutstandingFromPrice(t *testing.T) {
	created, err := ApplyCreateInput(CreateInput{CustomerID: "cus", Price: pint(1000)})
	if err != nil {
		t.Fatalf("ApplyCreateInput returned error: %v", err)
	}
	if created.AmountPaid != 0 {
		t.Fatalf("amount_paid = %d, want 0", created.AmountPaid)
	}
	if got, ok := mustOutstanding(t, created); !ok || got != 1000 {
		t.Fatalf("outstanding_amount = %v(%t), want 1000", created.OutstandingAmount, ok)
	}
	if created.PaidAt != nil {
		t.Fatalf("paid_at = %v, want nil（创建不自动写收款时刻）", created.PaidAt)
	}
}

// price 未定价的建单：outstanding 为 NULL，不计入待收合计。
func TestPaymentCreateWithoutPriceLeavesOutstandingNull(t *testing.T) {
	created, err := ApplyCreateInput(CreateInput{CustomerID: "cus"})
	if err != nil {
		t.Fatalf("ApplyCreateInput returned error: %v", err)
	}
	if created.AmountPaid != 0 || created.OutstandingAmount != nil {
		t.Fatalf("amount/outstanding = %d/%v, want 0/nil", created.AmountPaid, created.OutstandingAmount)
	}
}

// 补录建结清单（balance_paid=true）：DEC-10 结清推定 amount=COALESCE(price,0)、outstanding=0；
// paid_at 不自动写——不为历史发明收款时刻。
func TestPaymentCreateSettledBackfillInfersWithoutInventingPaidAt(t *testing.T) {
	created, err := ApplyCreateInput(CreateInput{
		CustomerID:  "cus",
		Price:       pint(2000),
		BalancePaid: pbool(true),
	})
	if err != nil {
		t.Fatalf("ApplyCreateInput returned error: %v", err)
	}
	if created.AmountPaid != 2000 {
		t.Fatalf("amount_paid = %d, want 2000（结清推定）", created.AmountPaid)
	}
	if got, ok := mustOutstanding(t, created); !ok || got != 0 {
		t.Fatalf("outstanding_amount = %v(%t), want 0", created.OutstandingAmount, ok)
	}
	if created.PaidAt != nil {
		t.Fatalf("paid_at = %v, want nil", created.PaidAt)
	}

	// 结清单 price NULL：amount=0、outstanding=0。
	created, err = ApplyCreateInput(CreateInput{CustomerID: "cus", BalancePaid: pbool(true)})
	if err != nil {
		t.Fatalf("ApplyCreateInput settled without price returned error: %v", err)
	}
	if created.AmountPaid != 0 {
		t.Fatalf("amount_paid = %d, want 0", created.AmountPaid)
	}
	if got, ok := mustOutstanding(t, created); !ok || got != 0 {
		t.Fatalf("outstanding_amount = %v(%t), want 0", created.OutstandingAmount, ok)
	}
}

// 建单显式金额优先：部分款 + 未显式给 outstanding → 推定余额；显式 outstanding 原样落库。
func TestPaymentCreateExplicitAmountsWin(t *testing.T) {
	created, err := ApplyCreateInput(CreateInput{CustomerID: "cus", Price: pint(1000), AmountPaid: pint(300)})
	if err != nil {
		t.Fatalf("ApplyCreateInput returned error: %v", err)
	}
	if got, ok := mustOutstanding(t, created); !ok || got != 700 {
		t.Fatalf("outstanding_amount = %v(%t), want 700", created.OutstandingAmount, ok)
	}

	created, err = ApplyCreateInput(CreateInput{CustomerID: "cus", Price: pint(1000), OutstandingAmount: pint(500)})
	if err != nil {
		t.Fatalf("ApplyCreateInput explicit outstanding returned error: %v", err)
	}
	if got, _ := mustOutstanding(t, created); got != 500 {
		t.Fatalf("outstanding_amount = %d, want 500（显式优先）", got)
	}
}

// 建单结清态显式 outstanding≠0 → 400（单向不变量从创建即成立）。
func TestPaymentCreateSettledRejectsNonzeroOutstanding(t *testing.T) {
	_, err := ApplyCreateInput(CreateInput{
		CustomerID:        "cus",
		Price:             pint(1000),
		BalancePaid:       pbool(true),
		OutstandingAmount: pint(1),
	})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("settled create with outstanding=1 error = %v, want validation", err)
	}
}

// 建单负金额 → 400。
func TestPaymentCreateRejectsNegativeAmounts(t *testing.T) {
	if _, err := ApplyCreateInput(CreateInput{CustomerID: "cus", AmountPaid: pint(-1)}); !errors.Is(err, ErrValidation) {
		t.Fatalf("negative amount_paid error = %v, want validation", err)
	}
	if _, err := ApplyCreateInput(CreateInput{CustomerID: "cus", OutstandingAmount: pint(-1)}); !errors.Is(err, ErrValidation) {
		t.Fatalf("negative outstanding_amount error = %v, want validation", err)
	}
}

// v1「标记收讫」不回退：PATCH {balance_paid:true} 推定 amount=price、outstanding=0，
// 且自动落 paid_at=now。
func TestPaymentSettleInfersAmountsAndPaidAt(t *testing.T) {
	now := time.Date(2026, 8, 24, 9, 0, 0, 0, time.UTC)
	input := UpdateInput{BalancePaid: pbool(true)}

	updated, err := ApplyUpdateInput(Order{Status: StatusDelivered, Price: pint(1000), ShotAt: &paymentShotAt, DeliveredAt: &paymentDeliveredAt}, input, now)
	if err != nil {
		t.Fatalf("ApplyUpdateInput settle returned error: %v", err)
	}
	if updated.AmountPaid != 1000 {
		t.Fatalf("amount_paid = %d, want 1000（结清推定）", updated.AmountPaid)
	}
	if got, ok := mustOutstanding(t, updated); !ok || got != 0 {
		t.Fatalf("outstanding_amount = %v(%t), want 0", updated.OutstandingAmount, ok)
	}
	if updated.PaidAt == nil || !updated.PaidAt.Equal(now) {
		t.Fatalf("paid_at = %v, want %v（结清跃迁自动落 now）", updated.PaidAt, now)
	}
}

// 推定不降低既有 amount_paid：已录 1200、price 1000，结清后保持 1200。
func TestPaymentSettleDoesNotLowerExistingAmount(t *testing.T) {
	now := time.Date(2026, 8, 24, 9, 0, 0, 0, time.UTC)
	input := UpdateInput{BalancePaid: pbool(true)}

	updated, err := ApplyUpdateInput(Order{Status: StatusDelivered, Price: pint(1000), AmountPaid: 1200, ShotAt: &paymentShotAt, DeliveredAt: &paymentDeliveredAt}, input, now)
	if err != nil {
		t.Fatalf("ApplyUpdateInput settle returned error: %v", err)
	}
	if updated.AmountPaid != 1200 {
		t.Fatalf("amount_paid = %d, want 1200（推定不降低既有）", updated.AmountPaid)
	}
}

// price NULL 的结清推定：amount 保持既有（COALESCE(price, 既有, 0)），outstanding=0。
func TestPaymentSettleWithoutPriceKeepsExistingAmount(t *testing.T) {
	now := time.Date(2026, 8, 24, 9, 0, 0, 0, time.UTC)
	input := UpdateInput{BalancePaid: pbool(true)}

	updated, err := ApplyUpdateInput(Order{Status: StatusDelivered, AmountPaid: 300, ShotAt: &paymentShotAt, DeliveredAt: &paymentDeliveredAt}, input, now)
	if err != nil {
		t.Fatalf("ApplyUpdateInput settle returned error: %v", err)
	}
	if updated.AmountPaid != 300 {
		t.Fatalf("amount_paid = %d, want 300", updated.AmountPaid)
	}
	if got, ok := mustOutstanding(t, updated); !ok || got != 0 {
		t.Fatalf("outstanding_amount = %v(%t), want 0", updated.OutstandingAmount, ok)
	}
}

// 部分收款联动：录入 amount_paid 后 outstanding=max(price−amount,0)；
// 超收为 0；price NULL 时为 NULL。
func TestPaymentPartialAmountDrivesOutstanding(t *testing.T) {
	input := UpdateInput{AmountPaid: nullable.NewNullableWithValue(300)}
	updated, err := ApplyUpdateInput(Order{Status: StatusShot, Price: pint(1000), ShotAt: &paymentShotAt}, input, time.Time{})
	if err != nil {
		t.Fatalf("ApplyUpdateInput partial returned error: %v", err)
	}
	if got, ok := mustOutstanding(t, updated); !ok || got != 700 {
		t.Fatalf("outstanding_amount = %v(%t), want 700", updated.OutstandingAmount, ok)
	}
	if updated.PaidAt != nil {
		t.Fatalf("paid_at = %v, want nil（金额单独变动不自动写时刻）", updated.PaidAt)
	}

	input = UpdateInput{AmountPaid: nullable.NewNullableWithValue(1500)}
	updated, err = ApplyUpdateInput(Order{Status: StatusShot, Price: pint(1000), ShotAt: &paymentShotAt}, input, time.Time{})
	if err != nil {
		t.Fatalf("ApplyUpdateInput overpaid returned error: %v", err)
	}
	if got, ok := mustOutstanding(t, updated); !ok || got != 0 {
		t.Fatalf("outstanding_amount = %v(%t), want 0（超收归零）", updated.OutstandingAmount, ok)
	}

	input = UpdateInput{AmountPaid: nullable.NewNullableWithValue(300)}
	updated, err = ApplyUpdateInput(Order{Status: StatusShot, ShotAt: &paymentShotAt}, input, time.Time{})
	if err != nil {
		t.Fatalf("ApplyUpdateInput without price returned error: %v", err)
	}
	if updated.OutstandingAmount != nil {
		t.Fatalf("outstanding_amount = %v, want nil（price NULL 不计入待收）", *updated.OutstandingAmount)
	}
}

// 未结清修正：显式 outstanding 优先于推定。
func TestPaymentExplicitOutstandingWinsOverInference(t *testing.T) {
	input := UpdateInput{
		AmountPaid:        nullable.NewNullableWithValue(300),
		OutstandingAmount: nullable.NewNullableWithValue(500),
	}
	updated, err := ApplyUpdateInput(Order{Status: StatusShot, Price: pint(1000), ShotAt: &paymentShotAt}, input, time.Time{})
	if err != nil {
		t.Fatalf("ApplyUpdateInput returned error: %v", err)
	}
	if got, _ := mustOutstanding(t, updated); got != 500 {
		t.Fatalf("outstanding_amount = %d, want 500（显式优先）", got)
	}
}

// 结清态（含同请求置结清）显式 outstanding≠0 → 400。
func TestPaymentSettledRejectsNonzeroOutstanding(t *testing.T) {
	settle := UpdateInput{BalancePaid: pbool(true), OutstandingAmount: nullable.NewNullableWithValue(1)}
	if _, err := ApplyUpdateInput(Order{Status: StatusDelivered, Price: pint(1000), ShotAt: &paymentShotAt, DeliveredAt: &paymentDeliveredAt}, settle, time.Time{}); !errors.Is(err, ErrValidation) {
		t.Fatalf("settle with outstanding=1 error = %v, want validation", err)
	}
	pin := UpdateInput{OutstandingAmount: nullable.NewNullableWithValue(700)}
	if _, err := ApplyUpdateInput(Order{Status: StatusClosed, BalancePaid: true, ShotAt: &paymentShotAt, DeliveredAt: &paymentDeliveredAt}, pin, time.Time{}); !errors.Is(err, ErrValidation) {
		t.Fatalf("closed explicit outstanding error = %v, want validation", err)
	}
}

// 金额字段无 null 语义：显式 null → 400。
func TestPaymentRejectsExplicitNulls(t *testing.T) {
	var amount UpdateInput
	amount.AmountPaid.SetNull()
	if _, err := ApplyUpdateInput(Order{Status: StatusShot, ShotAt: &paymentShotAt}, amount, time.Time{}); !errors.Is(err, ErrValidation) {
		t.Fatalf("amount_paid null error = %v, want validation", err)
	}
	var outstanding UpdateInput
	outstanding.OutstandingAmount.SetNull()
	if _, err := ApplyUpdateInput(Order{Status: StatusShot, ShotAt: &paymentShotAt}, outstanding, time.Time{}); !errors.Is(err, ErrValidation) {
		t.Fatalf("outstanding_amount null error = %v, want validation", err)
	}
	var paidAt UpdateInput
	paidAt.PaidAt.SetNull()
	if _, err := ApplyUpdateInput(Order{Status: StatusShot, ShotAt: &paymentShotAt}, paidAt, time.Time{}); !errors.Is(err, ErrValidation) {
		t.Fatalf("paid_at null error = %v, want validation", err)
	}
}

// 已收讫订单 outstanding 钉死 0：后改 price / 改 amount 均不重算。
func TestPaymentSettledOutstandingPinnedToZero(t *testing.T) {
	var price UpdateInput
	price.Price = nullable.NewNullableWithValue(800)
	updated, err := ApplyUpdateInput(Order{Status: StatusClosed, BalancePaid: true, AmountPaid: 1000, Price: pint(1000), ShotAt: &paymentShotAt, DeliveredAt: &paymentDeliveredAt}, price, time.Time{})
	if err != nil {
		t.Fatalf("ApplyUpdateInput price change returned error: %v", err)
	}
	if got, ok := mustOutstanding(t, updated); !ok || got != 0 {
		t.Fatalf("outstanding_amount = %v(%t), want 0（后改 price 不重算）", updated.OutstandingAmount, ok)
	}

	amount := UpdateInput{AmountPaid: nullable.NewNullableWithValue(400)}
	updated, err = ApplyUpdateInput(Order{Status: StatusClosed, BalancePaid: true, AmountPaid: 1000, ShotAt: &paymentShotAt, DeliveredAt: &paymentDeliveredAt}, amount, time.Time{})
	if err != nil {
		t.Fatalf("ApplyUpdateInput amount correction returned error: %v", err)
	}
	if updated.AmountPaid != 400 {
		t.Fatalf("amount_paid = %d, want 400（终态金额可修正）", updated.AmountPaid)
	}
	if got, ok := mustOutstanding(t, updated); !ok || got != 0 {
		t.Fatalf("outstanding_amount = %v(%t), want 0（钉死）", updated.OutstandingAmount, ok)
	}
}

// 取消结清（非终态 true→false）：金额与 outstanding 保持，outstanding 留 0 是合法态
// （录满全款未点收讫）；再次结清是新的跃迁——paid_at 落新 now，amount 按推定取 max。
func TestPaymentUnsettleKeepsAmountsAndResettleRefreshesPaidAt(t *testing.T) {
	first := time.Date(2026, 8, 24, 9, 0, 0, 0, time.UTC)
	second := time.Date(2026, 8, 26, 15, 0, 0, 0, time.UTC)
	settled := Order{
		Status:            StatusDelivered,
		Price:             pint(1000),
		AmountPaid:        1000,
		PaidAt:            &first,
		BalancePaid:       true,
		ShotAt:            &paymentShotAt,
		DeliveredAt:       &paymentDeliveredAt,
		OutstandingAmount: pint(0),
	}

	unsettle := UpdateInput{BalancePaid: pbool(false)}
	updated, err := ApplyUpdateInput(settled, unsettle, second)
	if err != nil {
		t.Fatalf("ApplyUpdateInput unsettle returned error: %v", err)
	}
	if updated.AmountPaid != 1000 {
		t.Fatalf("amount_paid = %d, want 1000（取消结清不动金额）", updated.AmountPaid)
	}
	if got, ok := mustOutstanding(t, updated); !ok || got != 0 {
		t.Fatalf("outstanding_amount = %v(%t), want 0（留 0 合法）", updated.OutstandingAmount, ok)
	}

	resettle := UpdateInput{BalancePaid: pbool(true)}
	updated, err = ApplyUpdateInput(updated, resettle, second)
	if err != nil {
		t.Fatalf("ApplyUpdateInput resettle returned error: %v", err)
	}
	if updated.PaidAt == nil || !updated.PaidAt.Equal(second) {
		t.Fatalf("paid_at = %v, want %v（新跃迁落新时刻）", updated.PaidAt, second)
	}
	if updated.AmountPaid != 1000 {
		t.Fatalf("amount_paid = %d, want 1000", updated.AmountPaid)
	}
}

// 显式 paid_at 始终优先（补录历史收款时刻、事后修正），结清同请求给值时不落自动 now。
func TestPaymentExplicitPaidAtWins(t *testing.T) {
	now := time.Date(2026, 8, 24, 9, 0, 0, 0, time.UTC)
	historical := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	settle := UpdateInput{BalancePaid: pbool(true)}
	settle.PaidAt = nullable.NewNullableWithValue(historical)

	updated, err := ApplyUpdateInput(Order{Status: StatusDelivered, Price: pint(1000), ShotAt: &paymentShotAt, DeliveredAt: &paymentDeliveredAt}, settle, now)
	if err != nil {
		t.Fatalf("ApplyUpdateInput settle with explicit paid_at returned error: %v", err)
	}
	if updated.PaidAt == nil || !updated.PaidAt.Equal(historical) {
		t.Fatalf("paid_at = %v, want %v（显式优先）", updated.PaidAt, historical)
	}

	correct := UpdateInput{}
	correct.PaidAt = nullable.NewNullableWithValue(historical)
	updated, err = ApplyUpdateInput(Order{Status: StatusShot, ShotAt: &paymentShotAt}, correct, now)
	if err != nil {
		t.Fatalf("ApplyUpdateInput paid_at correction returned error: %v", err)
	}
	if updated.PaidAt == nil || !updated.PaidAt.Equal(historical) {
		t.Fatalf("paid_at = %v, want %v", updated.PaidAt, historical)
	}
}

// 终态订单：收款标记仍不可变，金额可修正（退款/坏账手工通道）；cancelled 未结清可记 outstanding。
func TestPaymentTerminalAmountsPatchable(t *testing.T) {
	mark := UpdateInput{BalancePaid: pbool(true)}
	if _, err := ApplyUpdateInput(Order{Status: StatusCancelled, AmountPaid: 0}, mark, time.Time{}); !errors.Is(err, ErrValidation) {
		t.Fatalf("terminal balance_paid error = %v, want validation", err)
	}

	badDebt := UpdateInput{OutstandingAmount: nullable.NewNullableWithValue(700), Note: strPtrForTest("坏账")}
	updated, err := ApplyUpdateInput(Order{Status: StatusCancelled, Price: pint(700)}, badDebt, time.Time{})
	if err != nil {
		t.Fatalf("cancelled outstanding correction returned error: %v", err)
	}
	if got, _ := mustOutstanding(t, updated); got != 700 {
		t.Fatalf("outstanding_amount = %d, want 700", got)
	}
}

// 未传金额字段：一切保持现状（改备注/推进状态不得触碰支付事实）。
func TestPaymentUntouchedFieldsKeepState(t *testing.T) {
	input := UpdateInput{Note: strPtrForTest("改备注")}
	current := Order{Status: StatusShot, Price: pint(1000), AmountPaid: 300, OutstandingAmount: pint(700), ShotAt: &paymentShotAt}
	updated, err := ApplyUpdateInput(current, input, time.Time{})
	if err != nil {
		t.Fatalf("ApplyUpdateInput note change returned error: %v", err)
	}
	if updated.AmountPaid != 300 {
		t.Fatalf("amount_paid = %d, want 300", updated.AmountPaid)
	}
	if got, _ := mustOutstanding(t, updated); got != 700 {
		t.Fatalf("outstanding_amount = %d, want 700（未触碰不重算）", got)
	}
}

func strPtrForTest(value string) *string { return &value }

func pbool(value bool) *bool { return &value }

// price 单独变更不是推定触发器（DEC-10 字面）：未结清订单 outstanding 保持落库值，
// 允许滞后于 price——消费方按落库值聚合，不承诺 price−amount 实时一致。
func TestPaymentPriceOnlyChangeDoesNotRecomputeOutstanding(t *testing.T) {
	var price UpdateInput
	price.Price = nullable.NewNullableWithValue(800)
	current := Order{
		Status:            StatusShot,
		Price:             pint(1000),
		OutstandingAmount: pint(1000),
		ShotAt:            &paymentShotAt,
	}
	updated, err := ApplyUpdateInput(current, price, time.Time{})
	if err != nil {
		t.Fatalf("ApplyUpdateInput price-only returned error: %v", err)
	}
	if got, _ := mustOutstanding(t, updated); got != 1000 {
		t.Fatalf("outstanding_amount = %d, want 1000（price 单独变更不重算）", got)
	}
}
