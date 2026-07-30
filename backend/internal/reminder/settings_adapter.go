package reminder

import (
	"context"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
	"github.com/samson/customer-manage-platform/backend/internal/settings"
)

// SettingsAdapter 把 settings.Service 适配为 reminder.SettingsLoader。
type SettingsAdapter struct {
	svc *settings.Service
}

func NewSettingsAdapter(svc *settings.Service) SettingsAdapter {
	return SettingsAdapter{svc: svc}
}

func (a SettingsAdapter) Load(ctx context.Context, scope store.AccountScope) (SettingsView, error) {
	s, err := a.svc.Get(ctx, scope)
	if err != nil {
		return SettingsView{}, err
	}
	return SettingsView{
		Timezone:          s.Timezone,
		BirthdayLeadDays:  s.BirthdayLeadDays,
		FollowUpAfterDays: s.FollowUpAfterDays,
		ChurnDaysFor:      s.ChurnDaysFor,
	}, nil
}
