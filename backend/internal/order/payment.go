package order

import (
	"time"
)

// applyPaymentFacts 在订单最终态上收敛支付事实（DEC-10 金额联动推定）：
//   - 显式金额始终优先；
//   - 置 balance_paid=true 且未显式给金额时，推定 amount_paid=max(既有, COALESCE(price,0))
//     （不降低既有）；已收讫订单后续编辑（改价/改备注）不再触碰金额——推定只在
//     真正发生结清跃迁（或建结清单）时抬高；
//   - amountRecorded 表示本请求录入了 amount_paid（建单恒为 true——视为 0 已录入，
//     PATCH 仅显式给值时为 true）：未结清且未显式给 outstanding_amount 时推定
//     max(price−amount_paid, 0)，price 为 NULL 时为 NULL；
//   - 已收讫订单 outstanding 钉死 0：后改 price/amount 不重算；反向不要求——
//     录满全款而未点收讫的订单 outstanding 为 0 但 balance_paid 仍 false；
//   - price 单独变更不是推定触发器（含商务调价直写路径）：未结清订单的 outstanding
//     可能滞后于 price，消费方按落库值聚合。
//
// 单向不变量 balance_paid=true ⇒ outstanding_amount=0 在此为服务层保证，
// 迁移 0033 另以表级 CHECK 兜底。
func applyPaymentFacts(current, next Order, amountRecorded, explicitAmount, explicitOutstanding bool) Order {
	if next.BalancePaid {
		if !explicitAmount && !current.BalancePaid {
			if settled := coalescePrice(next.Price); settled > next.AmountPaid {
				next.AmountPaid = settled
			}
		}
		outstanding := 0
		next.OutstandingAmount = &outstanding
		return next
	}
	if !explicitOutstanding && amountRecorded {
		next.OutstandingAmount = inferredOutstanding(next.Price, next.AmountPaid)
	}
	return next
}

// autoSettlePaidAt 在 balance_paid 由 false 实际跃迁为 true 且未显式提供 paid_at 时
// 落服务端 now（v1「标记收讫」路径的支付时刻补全）。创建不走此路径——
// 补录结清单不发明历史收款时刻；取消结清后的再次结清是新跃迁，落新时刻。
func autoSettlePaidAt(current, next Order, explicitPaidAt bool, now time.Time) Order {
	if !current.BalancePaid && next.BalancePaid && !explicitPaidAt {
		paidAt := now
		next.PaidAt = &paidAt
	}
	return next
}

// validatePaymentFacts 兜底显式支付字段的领域硬约束：金额非负、结清态 outstanding
// 必须为 0（单向不变量）。显式 null 的拒绝在字段应用阶段完成（与 shot_at 同模式）。
func validatePaymentFacts(next Order, explicitOutstanding bool) error {
	if next.AmountPaid < 0 {
		return ValidationError{Message: "amount_paid 不能为负"}
	}
	if next.OutstandingAmount != nil && *next.OutstandingAmount < 0 {
		return ValidationError{Message: "outstanding_amount 不能为负"}
	}
	if next.BalancePaid && explicitOutstanding && next.OutstandingAmount != nil && *next.OutstandingAmount != 0 {
		return ValidationError{Message: "已结清订单 outstanding_amount 必须为 0"}
	}
	return nil
}

func coalescePrice(price *int) int {
	if price == nil {
		return 0
	}
	return *price
}

func inferredOutstanding(price *int, amountPaid int) *int {
	if price == nil {
		return nil
	}
	outstanding := *price - amountPaid
	if outstanding < 0 {
		outstanding = 0
	}
	return &outstanding
}
