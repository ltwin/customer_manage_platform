package order

import (
	"time"

	"github.com/oapi-codegen/nullable"

	"github.com/samson/customer-manage-platform/backend/internal/platform/clock"
)

// DeliveryPolicy 是派生应交付日所需的账号级上下文（时区 + 默认 SLA 天数）。
// 由 service 从 settings 取值后填入输入结构，领域层不反向依赖 settings 域。
type DeliveryPolicy struct {
	Clock   clock.AccountClock
	SLADays int
	ready   bool
}

// NewDeliveryPolicy 构造可用的派生策略；SLA 越界时回落账号默认 14 天由调用方保证。
func NewDeliveryPolicy(accountClock clock.AccountClock, slaDays int) DeliveryPolicy {
	return DeliveryPolicy{Clock: accountClock, SLADays: slaDays, ready: true}
}

// DeriveDeliveryDue 按 shot_at 与账号 SLA 算出 date-only 应交付日。
func (p DeliveryPolicy) DeriveDeliveryDue(shotAt time.Time) time.Time {
	return clock.AddDays(p.Clock.LocalDate(shotAt), p.SLADays)
}

// nullableFromPointer 把建单侧的二态指针提升为三态：nil 表示未传，而非显式 null。
// 建单没有「撤销覆盖」语义，因此永不产出 explicit null——这一点由类型保证，
// 不依赖调用方自律。
func nullableFromPointer(value *time.Time) nullable.Nullable[time.Time] {
	var result nullable.Nullable[time.Time]
	if value != nil {
		result.Set(*value)
	}
	return result
}

// applyDeliveryDue 在订单最终态上收敛应交付日：
//   - 显式传入日期即订单级覆盖，标记 is_override 且不再被自动派生改写；
//   - 显式传入 null 撤销覆盖，清除标记后按当前 shot_at 重新派生（无 shot_at 则清空）；
//   - 未传该字段时覆盖值保持不变；
//   - 派生值只在 shot_at 本次真正落值或变更时重算，其余写路径（改备注、改价、推进状态）
//     不得触碰既有落库值——否则改了账号 SLA 之后随便编辑一个字段就会静默重写历史应交付日。
//
// policy 未就绪（调用方未提供账号上下文）时只处理覆盖、不做派生，避免用错误时区
// 落库既成事实。
//
// 调用点固定在 validateFinalState 之后，因此进入本函数的 next 已满足
// 「有 shot_at ⇒ 已到达拍摄」不变量；显式覆盖的合法性由 validateDeliveryDue 单独把关。
func applyDeliveryDue(
	next Order,
	explicit nullable.Nullable[time.Time],
	policy DeliveryPolicy,
	shotAtChanged bool,
) Order {
	clearedOverride := false
	if explicit.IsSpecified() {
		if !explicit.IsNull() {
			due := clock.DateOnly(explicit.MustGet())
			next.DeliveryDueAt = &due
			next.DeliveryDueIsOverride = true
			return next
		}
		// 显式 null：撤销订单级覆盖，交还给自动派生。
		next.DeliveryDueAt = nil
		next.DeliveryDueIsOverride = false
		clearedOverride = true
	}
	if next.DeliveryDueIsOverride || !policy.ready || next.ShotAt == nil {
		return next
	}
	// 已取消订单不在交付链条内：既不接受显式覆盖（validateDeliveryDue），也不凭空
	// 产生新的派生值——否则「撤销覆盖」会成为 cancelled 守卫的绕过口。清除仍允许。
	if next.Status == StatusCancelled {
		return next
	}
	// 撤销覆盖后立即重新派生；其余情况只在 shot_at 落值或变更时重算。
	if !shotAtChanged && !clearedOverride {
		return next
	}
	due := policy.DeriveDeliveryDue(*next.ShotAt)
	next.DeliveryDueAt = &due
	return next
}

// validateDeliveryDue 约束显式覆盖：应交付日只对已经拍摄的订单有意义。
// 没有这一条时，consulting 甚至 cancelled 订单都能被打上 delivery_due_at 并进入
// 交付队列索引，使 delivery_due_at IS NOT NULL 与「在交付队列中」进一步脱节。
// 显式 null（撤销覆盖）不受状态约束——任何状态都允许清除；但撤销依赖账号上下文
// 重新派生，policy 未就绪时拒绝而不是静默清成 nil（净数据丢失）。
func validateDeliveryDue(next Order, explicit nullable.Nullable[time.Time], policy DeliveryPolicy) error {
	if !explicit.IsSpecified() {
		return nil
	}
	if explicit.IsNull() {
		if !policy.ready {
			return ValidationError{Message: "账号交付配置不可用，暂不能撤销应交付日覆盖"}
		}
		return nil
	}
	if next.ShotAt == nil {
		return ValidationError{Message: "delivery_due_at 仅可用于已到达拍摄的订单"}
	}
	if next.Status == StatusCancelled {
		return ValidationError{Message: "已取消订单不接受 delivery_due_at"}
	}
	return nil
}

// shotAtChanged 判断本次写入是否让 shot_at 首次落值或发生变更。
func shotAtChanged(current, next Order) bool {
	switch {
	case current.ShotAt == nil && next.ShotAt == nil:
		return false
	case current.ShotAt == nil || next.ShotAt == nil:
		return true
	default:
		return !current.ShotAt.Equal(*next.ShotAt)
	}
}
