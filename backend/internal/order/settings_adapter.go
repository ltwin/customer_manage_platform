package order

import (
	"context"

	"github.com/samson/customer-manage-platform/backend/internal/platform/clock"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
	"github.com/samson/customer-manage-platform/backend/internal/settings"
)

// SettingsDeliveryPolicyAdapter 从 settings 域取账号时区与默认交付 SLA，
// 组装成领域侧的 DeliveryPolicy（与 reminder.SettingsAdapter 同惯例：
// 适配器归消费域，settings 不反向依赖业务域）。
type SettingsDeliveryPolicyAdapter struct {
	svc *settings.Service
}

func NewSettingsDeliveryPolicyAdapter(svc *settings.Service) SettingsDeliveryPolicyAdapter {
	return SettingsDeliveryPolicyAdapter{svc: svc}
}

// DeliveryPolicyForAccount 实现 DeliveryPolicyProvider；时区非法时 fail loud，
// 不静默回退默认时区（与 dashboard D3 同口径）。
func (a SettingsDeliveryPolicyAdapter) DeliveryPolicyForAccount(
	ctx context.Context,
	scope store.AccountScope,
) (DeliveryPolicy, error) {
	effective, err := a.svc.Get(ctx, scope)
	if err != nil {
		return DeliveryPolicy{}, err
	}
	accountClock, err := clock.NewAccountClock(effective.Timezone)
	if err != nil {
		return DeliveryPolicy{}, err
	}
	return NewDeliveryPolicy(accountClock, effective.DeliverySLADays), nil
}
