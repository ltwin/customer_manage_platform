package order

import (
	"testing"
	"time"

	"github.com/oapi-codegen/nullable"

	"github.com/samson/customer-manage-platform/backend/internal/platform/clock"
)

func testDeliveryPolicy(t *testing.T, timezone string, slaDays int) DeliveryPolicy {
	t.Helper()
	accountClock, err := clock.NewAccountClock(timezone)
	if err != nil {
		t.Fatalf("NewAccountClock(%q) returned error: %v", timezone, err)
	}
	return NewDeliveryPolicy(accountClock, slaDays)
}

func mustDate(t *testing.T, value *time.Time) string {
	t.Helper()
	if value == nil {
		t.Fatalf("expected delivery_due_at, got nil")
	}
	return clock.FormatDate(*value)
}

// S2：跃迁到 shot 时按账号时区 + SLA 自动派生，且标记为非覆盖。
func TestDeliveryDueDerivedOnShotTransition(t *testing.T) {
	now := time.Date(2026, 7, 9, 10, 0, 0, 0, time.UTC)
	status := StatusShot
	input := UpdateInput{Status: &status}
	input.deliveryPolicy = testDeliveryPolicy(t, "Asia/Shanghai", 14)

	updated, err := ApplyUpdateInput(Order{Status: StatusScheduled}, input, now)
	if err != nil {
		t.Fatalf("ApplyUpdateInput scheduled->shot returned error: %v", err)
	}
	if got, want := mustDate(t, updated.DeliveryDueAt), "2026-07-23"; got != want {
		t.Fatalf("delivery_due_at = %q, want %q", got, want)
	}
	if updated.DeliveryDueIsOverride {
		t.Fatalf("delivery_due_is_override = true, want false for derived value")
	}
}

// S2 边界：账号时区决定 shot_at 落在哪个自然日，派生结果随之移动。
func TestDeliveryDueUsesAccountTimezoneDayBoundary(t *testing.T) {
	// UTC 2026-07-09T17:30Z 在上海已是 07-10，在洛杉矶仍是 07-09。
	shotAt := time.Date(2026, 7, 9, 17, 30, 0, 0, time.UTC)
	status := StatusShot

	shanghai := UpdateInput{Status: &status}
	shanghai.ShotAt = nullable.NewNullableWithValue(shotAt)
	shanghai.deliveryPolicy = testDeliveryPolicy(t, "Asia/Shanghai", 14)
	updated, err := ApplyUpdateInput(Order{Status: StatusScheduled}, shanghai, shotAt)
	if err != nil {
		t.Fatalf("shanghai ApplyUpdateInput returned error: %v", err)
	}
	if got, want := mustDate(t, updated.DeliveryDueAt), "2026-07-24"; got != want {
		t.Fatalf("shanghai delivery_due_at = %q, want %q", got, want)
	}

	losAngeles := UpdateInput{Status: &status}
	losAngeles.ShotAt = nullable.NewNullableWithValue(shotAt)
	losAngeles.deliveryPolicy = testDeliveryPolicy(t, "America/Los_Angeles", 14)
	updated, err = ApplyUpdateInput(Order{Status: StatusScheduled}, losAngeles, shotAt)
	if err != nil {
		t.Fatalf("los angeles ApplyUpdateInput returned error: %v", err)
	}
	if got, want := mustDate(t, updated.DeliveryDueAt), "2026-07-23"; got != want {
		t.Fatalf("los angeles delivery_due_at = %q, want %q", got, want)
	}
}

// S3：显式传入即订单级覆盖并标记 is_override。
func TestDeliveryDueExplicitOverrideMarksFlag(t *testing.T) {
	now := time.Date(2026, 7, 9, 10, 0, 0, 0, time.UTC)
	status := StatusShot
	override := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	input := UpdateInput{Status: &status}
	input.DeliveryDueAt.Set(override)
	input.deliveryPolicy = testDeliveryPolicy(t, "Asia/Shanghai", 14)

	updated, err := ApplyUpdateInput(Order{Status: StatusScheduled}, input, now)
	if err != nil {
		t.Fatalf("ApplyUpdateInput returned error: %v", err)
	}
	if got, want := mustDate(t, updated.DeliveryDueAt), "2026-08-01"; got != want {
		t.Fatalf("delivery_due_at = %q, want %q", got, want)
	}
	if !updated.DeliveryDueIsOverride {
		t.Fatalf("delivery_due_is_override = false, want true for explicit value")
	}
}

// S4：派生值随 shot_at 修正重算。
func TestDeliveryDueDerivedRecalculatesOnShotAtChange(t *testing.T) {
	original := time.Date(2026, 7, 9, 2, 0, 0, 0, time.UTC)
	originalDue := time.Date(2026, 7, 23, 0, 0, 0, 0, time.UTC)
	current := Order{
		Status:        StatusShot,
		ShotAt:        &original,
		DeliveryDueAt: &originalDue,
	}
	corrected := time.Date(2026, 7, 12, 2, 0, 0, 0, time.UTC)
	input := UpdateInput{ShotAt: nullable.NewNullableWithValue(corrected)}
	input.deliveryPolicy = testDeliveryPolicy(t, "Asia/Shanghai", 14)

	updated, err := ApplyUpdateInput(current, input, corrected)
	if err != nil {
		t.Fatalf("ApplyUpdateInput returned error: %v", err)
	}
	if got, want := mustDate(t, updated.DeliveryDueAt), "2026-07-26"; got != want {
		t.Fatalf("delivery_due_at = %q, want %q（派生值须随 shot_at 重算）", got, want)
	}
}

// S5：覆盖值不随 shot_at 修正被冲掉。
func TestDeliveryDueOverrideSurvivesShotAtChange(t *testing.T) {
	original := time.Date(2026, 7, 9, 2, 0, 0, 0, time.UTC)
	override := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	current := Order{
		Status:                StatusShot,
		ShotAt:                &original,
		DeliveryDueAt:         &override,
		DeliveryDueIsOverride: true,
	}
	corrected := time.Date(2026, 7, 12, 2, 0, 0, 0, time.UTC)
	input := UpdateInput{ShotAt: nullable.NewNullableWithValue(corrected)}
	input.deliveryPolicy = testDeliveryPolicy(t, "Asia/Shanghai", 14)

	updated, err := ApplyUpdateInput(current, input, corrected)
	if err != nil {
		t.Fatalf("ApplyUpdateInput returned error: %v", err)
	}
	if got, want := mustDate(t, updated.DeliveryDueAt), "2026-08-01"; got != want {
		t.Fatalf("delivery_due_at = %q, want %q（覆盖值不得被派生冲掉）", got, want)
	}
	if !updated.DeliveryDueIsOverride {
		t.Fatalf("delivery_due_is_override 被重置，want 保持 true")
	}
}

// S6：改 SLA 后不触碰 shot_at 的写路径（改备注等）不得重写已落库的应交付日。
func TestDeliveryDueSLAChangeDoesNotRewriteUntouchedOrder(t *testing.T) {
	shotAt := time.Date(2026, 7, 9, 2, 0, 0, 0, time.UTC)
	stored := time.Date(2026, 7, 23, 0, 0, 0, 0, time.UTC)
	current := Order{Status: StatusShot, ShotAt: &shotAt, DeliveryDueAt: &stored}

	// 账号 SLA 已从 14 改成 30，但本次 PATCH 只改备注、不触碰 shot_at。
	note := "补充说明"
	input := UpdateInput{Note: &note}
	input.deliveryPolicy = testDeliveryPolicy(t, "Asia/Shanghai", 30)

	updated, err := ApplyUpdateInput(current, input, shotAt)
	if err != nil {
		t.Fatalf("ApplyUpdateInput returned error: %v", err)
	}
	if got, want := mustDate(t, updated.DeliveryDueAt), "2026-07-23"; got != want {
		t.Fatalf("delivery_due_at = %q, want %q（改 SLA 不得重写历史落库值）", got, want)
	}

	// 只有 shot_at 真正变更时才按新 SLA 重算。
	corrected := time.Date(2026, 7, 10, 2, 0, 0, 0, time.UTC)
	recalc := UpdateInput{ShotAt: nullable.NewNullableWithValue(corrected)}
	recalc.deliveryPolicy = testDeliveryPolicy(t, "Asia/Shanghai", 30)
	updated, err = ApplyUpdateInput(current, recalc, corrected)
	if err != nil {
		t.Fatalf("ApplyUpdateInput recalc returned error: %v", err)
	}
	if got, want := mustDate(t, updated.DeliveryDueAt), "2026-08-09"; got != want {
		t.Fatalf("delivery_due_at = %q, want %q（shot_at 变更时按当前 SLA 重算）", got, want)
	}
}

// S7：补录建单直接带 shot_at 时建单即落应交付日。
func TestDeliveryDueDerivedOnBackfillCreate(t *testing.T) {
	shotAt := time.Date(2026, 7, 9, 2, 0, 0, 0, time.UTC)
	status := StatusShot
	input := CreateInput{
		CreationMode: CreationModeBackfill,
		CustomerID:   "cus_1",
		Status:       &status,
		ShotAt:       &shotAt,
	}
	input.deliveryPolicy = testDeliveryPolicy(t, "Asia/Shanghai", 14)

	created, err := ApplyCreateInput(input)
	if err != nil {
		t.Fatalf("ApplyCreateInput returned error: %v", err)
	}
	if got, want := mustDate(t, created.DeliveryDueAt), "2026-07-23"; got != want {
		t.Fatalf("delivery_due_at = %q, want %q", got, want)
	}
	if created.DeliveryDueIsOverride {
		t.Fatalf("delivery_due_is_override = true, want false for derived value")
	}
}

// 无 shot_at 的订单不产生应交付日；未注入账号上下文时不猜测派生。
func TestDeliveryDueAbsentWithoutShotAtOrPolicy(t *testing.T) {
	now := time.Date(2026, 7, 9, 10, 0, 0, 0, time.UTC)
	status := StatusScheduled
	withPolicy := UpdateInput{Status: &status}
	withPolicy.deliveryPolicy = testDeliveryPolicy(t, "Asia/Shanghai", 14)

	updated, err := ApplyUpdateInput(Order{Status: StatusConsulting}, withPolicy, now)
	if err != nil {
		t.Fatalf("ApplyUpdateInput returned error: %v", err)
	}
	if updated.DeliveryDueAt != nil {
		t.Fatalf("delivery_due_at = %v, want nil（未拍摄不产生应交付日）", updated.DeliveryDueAt)
	}

	shotStatus := StatusShot
	withoutPolicy := UpdateInput{Status: &shotStatus}
	updated, err = ApplyUpdateInput(Order{Status: StatusScheduled}, withoutPolicy, now)
	if err != nil {
		t.Fatalf("ApplyUpdateInput without policy returned error: %v", err)
	}
	if updated.DeliveryDueAt != nil {
		t.Fatalf("delivery_due_at = %v, want nil（无账号上下文时不猜测派生）", updated.DeliveryDueAt)
	}
}

// 显式覆盖只对已拍摄且未取消的订单成立：否则 consulting/cancelled 订单也能被打上
// 应交付日并进入交付队列索引，使 delivery_due_at IS NOT NULL 与队列集合进一步脱节。
func TestDeliveryDueOverrideRejectedOnUnshotOrCancelledOrder(t *testing.T) {
	due := time.Date(2026, 7, 23, 0, 0, 0, 0, time.UTC)
	now := time.Date(2026, 7, 9, 10, 0, 0, 0, time.UTC)

	consulting := CreateInput{CustomerID: "cus_1", DeliveryDueAt: &due}
	if _, err := ApplyCreateInput(consulting); err == nil {
		t.Fatalf("未拍摄订单接受了 delivery_due_at，应拒绝")
	}

	shotAt := time.Date(2026, 7, 9, 2, 0, 0, 0, time.UTC)
	cancelled := Order{Status: StatusShot, ShotAt: &shotAt}
	target := StatusCancelled
	input := UpdateInput{Status: &target}
	input.DeliveryDueAt.Set(due)
	if _, err := ApplyUpdateInput(cancelled, input, now); err == nil {
		t.Fatalf("取消订单接受了 delivery_due_at，应拒绝")
	}
}

// 取消已拍摄订单时保留既有应交付日（落库事实不因取消而抹除），
// 该值不得被当作「在交付队列中」的判定依据——队列须联合 status 过滤。
func TestDeliveryDueRetainedOnCancelledOrder(t *testing.T) {
	now := time.Date(2026, 7, 9, 10, 0, 0, 0, time.UTC)
	shotAt := time.Date(2026, 7, 9, 2, 0, 0, 0, time.UTC)
	due := time.Date(2026, 7, 23, 0, 0, 0, 0, time.UTC)
	current := Order{Status: StatusShot, ShotAt: &shotAt, DeliveryDueAt: &due}

	target := StatusCancelled
	updated, err := ApplyUpdateInput(current, UpdateInput{Status: &target}, now)
	if err != nil {
		t.Fatalf("cancel shot order returned error: %v", err)
	}
	if updated.DeliveryDueAt == nil || !updated.DeliveryDueAt.Equal(due) {
		t.Fatalf("delivery_due_at = %v, want 保留 %v", updated.DeliveryDueAt, due)
	}
}

// 显式传 null 撤销订单级覆盖：清除覆盖标记并回到自动派生。
// 与「未传字段」区分——未传保持现状，传 null 才撤销（三态语义）。
func TestDeliveryDueExplicitNullClearsOverrideAndRederives(t *testing.T) {
	now := time.Date(2026, 7, 9, 10, 0, 0, 0, time.UTC)
	shotAt := time.Date(2026, 7, 9, 2, 0, 0, 0, time.UTC)
	manual := time.Date(2026, 7, 25, 0, 0, 0, 0, time.UTC)
	current := Order{
		Status:                StatusShot,
		ShotAt:                &shotAt,
		DeliveryDueAt:         &manual,
		DeliveryDueIsOverride: true,
	}

	var clear UpdateInput
	clear.DeliveryDueAt.SetNull()
	clear.deliveryPolicy = testDeliveryPolicy(t, "Asia/Shanghai", 14)

	updated, err := ApplyUpdateInput(current, clear, now)
	if err != nil {
		t.Fatalf("clear override returned error: %v", err)
	}
	if updated.DeliveryDueIsOverride {
		t.Fatalf("delivery_due_is_override = true, want false（显式 null 应撤销覆盖）")
	}
	// 撤销后立即按当前 shot_at + SLA 重新派生：2026-07-09 + 14 = 2026-07-23。
	if got, want := mustDate(t, updated.DeliveryDueAt), "2026-07-23"; got != want {
		t.Fatalf("delivery_due_at = %q, want %q（撤销后应回到自动派生）", got, want)
	}
}

// 未传 delivery_due_at 时保持覆盖态不变，证明三态中「未传」与「null」语义不同。
func TestDeliveryDueOmittedKeepsOverride(t *testing.T) {
	now := time.Date(2026, 7, 9, 10, 0, 0, 0, time.UTC)
	shotAt := time.Date(2026, 7, 9, 2, 0, 0, 0, time.UTC)
	manual := time.Date(2026, 7, 25, 0, 0, 0, 0, time.UTC)
	current := Order{
		Status:                StatusShot,
		ShotAt:                &shotAt,
		DeliveryDueAt:         &manual,
		DeliveryDueIsOverride: true,
	}

	note := "只改备注"
	untouched := UpdateInput{Note: &note}
	untouched.deliveryPolicy = testDeliveryPolicy(t, "Asia/Shanghai", 14)

	updated, err := ApplyUpdateInput(current, untouched, now)
	if err != nil {
		t.Fatalf("update note returned error: %v", err)
	}
	if !updated.DeliveryDueIsOverride {
		t.Fatalf("delivery_due_is_override = false, want true（未传字段不应撤销覆盖）")
	}
	if got, want := mustDate(t, updated.DeliveryDueAt), "2026-07-25"; got != want {
		t.Fatalf("delivery_due_at = %q, want %q（未传字段不应改动既有值）", got, want)
	}
}

// 撤销覆盖但订单没有 shot_at 时，清空为无应交付日而不是留下旧值。
func TestDeliveryDueExplicitNullWithoutShotAtClearsValue(t *testing.T) {
	now := time.Date(2026, 7, 9, 10, 0, 0, 0, time.UTC)
	manual := time.Date(2026, 7, 25, 0, 0, 0, 0, time.UTC)
	current := Order{Status: StatusScheduled, DeliveryDueAt: &manual, DeliveryDueIsOverride: true}

	var clear UpdateInput
	clear.DeliveryDueAt.SetNull()
	clear.deliveryPolicy = testDeliveryPolicy(t, "Asia/Shanghai", 14)

	updated, err := ApplyUpdateInput(current, clear, now)
	if err != nil {
		t.Fatalf("clear override returned error: %v", err)
	}
	if updated.DeliveryDueIsOverride {
		t.Fatalf("delivery_due_is_override = true, want false")
	}
	if updated.DeliveryDueAt != nil {
		t.Fatalf("delivery_due_at = %v, want nil（无 shot_at 时撤销即清空）", updated.DeliveryDueAt)
	}
}

// 已取消订单传显式 null：允许清除，但不得凭空派生新值——否则「撤销覆盖」会成为
// cancelled 守卫（不接受显式 delivery_due_at）的绕过口。
func TestDeliveryDueExplicitNullOnCancelledDoesNotRederive(t *testing.T) {
	now := time.Date(2026, 7, 9, 10, 0, 0, 0, time.UTC)
	shotAt := time.Date(2026, 7, 9, 2, 0, 0, 0, time.UTC)
	manual := time.Date(2026, 7, 25, 0, 0, 0, 0, time.UTC)
	cancelled := Order{
		Status:                StatusCancelled,
		ShotAt:                &shotAt,
		DeliveryDueAt:         &manual,
		DeliveryDueIsOverride: true,
	}

	var clear UpdateInput
	clear.DeliveryDueAt.SetNull()
	clear.deliveryPolicy = testDeliveryPolicy(t, "Asia/Shanghai", 14)

	updated, err := ApplyUpdateInput(cancelled, clear, now)
	if err != nil {
		t.Fatalf("clear override on cancelled returned error: %v", err)
	}
	if updated.DeliveryDueIsOverride {
		t.Fatalf("delivery_due_is_override = true, want false（清除应生效）")
	}
	if updated.DeliveryDueAt != nil {
		t.Fatalf("delivery_due_at = %v, want nil（已取消订单不得凭空派生新应交付日）", updated.DeliveryDueAt)
	}
}

// policy 未就绪时拒绝撤销，而不是静默把覆盖值清成 nil（净数据丢失）。
func TestDeliveryDueExplicitNullRequiresReadyPolicy(t *testing.T) {
	now := time.Date(2026, 7, 9, 10, 0, 0, 0, time.UTC)
	shotAt := time.Date(2026, 7, 9, 2, 0, 0, 0, time.UTC)
	manual := time.Date(2026, 7, 25, 0, 0, 0, 0, time.UTC)
	current := Order{
		Status:                StatusShot,
		ShotAt:                &shotAt,
		DeliveryDueAt:         &manual,
		DeliveryDueIsOverride: true,
	}

	var clear UpdateInput
	clear.DeliveryDueAt.SetNull()
	// 不注入 deliveryPolicy：模拟组合根漏接线。

	if _, err := ApplyUpdateInput(current, clear, now); err == nil {
		t.Fatalf("policy 未就绪时撤销应被拒绝，而不是静默清除覆盖值")
	}
}
