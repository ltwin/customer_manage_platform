package dashboard

import (
	"context"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/clock"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// TimezoneProvider 是经营台取账号有效时区的最小依赖（method 见 design D9）。
// composition root 注入 settings.Service（其已实现 TimezoneForAccount），
// dashboard 包因此不 import settings/httpapi，也不注入他域业务服务。
type TimezoneProvider interface {
	TimezoneForAccount(ctx context.Context, accountID string) (string, error)
}

// Repository 是经营台聚合读面：按窗口一次载入五块（经 AccountScope 直查，ADR-001 / D1）。
type Repository interface {
	LoadDashboard(ctx context.Context, scope store.AccountScope, window Window) (Dashboard, error)
}

// Window 是 Service 依账号时区算好、交给 Repository 的日界与窗口。
// date-only 字段用于 due_date 字符串比较；瞬时字段用于 timestamp 相交与落窗。
type Window struct {
	Today       time.Time // date-only：账号本地今日
	DueBefore   time.Time // date-only：今日+2，due_date ≤ 此值落近 3 天窗
	DayStart    time.Time // 瞬时：本地今日 00:00（含），今日档期相交下界
	DayEnd      time.Time // 瞬时：本地次日 00:00（不含），今日档期相交上界
	RecentStart time.Time // 瞬时：本地今日-29 的 00:00（含），近 30 天窗下界
	RecentEnd   time.Time // 瞬时：本地次日 00:00（不含），近 30 天窗上界
}

// Service 编排「取时区 → 算窗口 → 调仓库」；无领域写、无路由依赖（ADR-003）。
type Service struct {
	repo     Repository
	timezone TimezoneProvider
	now      func() time.Time
}

func NewService(repo Repository, timezone TimezoneProvider) *Service {
	return &Service{repo: repo, timezone: timezone, now: time.Now}
}

// WithClock 注入 now（测试用，确定性窗口）。
func (s *Service) WithClock(now func() time.Time) *Service {
	if now != nil {
		s.now = now
	}
	return s
}

// Get 按账号时区聚合经营台五块。时区读取或解析失败即返回 error（handler 落 500），
// 绝不用浏览器或默认时区继续算（design D3 / S12）。
func (s *Service) Get(ctx context.Context, scope store.AccountScope, accountID string) (Dashboard, error) {
	timezone, err := s.timezone.TimezoneForAccount(ctx, accountID)
	if err != nil {
		return Dashboard{}, err
	}
	accountClock, err := clock.NewAccountClock(timezone)
	if err != nil {
		return Dashboard{}, err
	}
	today := accountClock.LocalDate(s.now())
	dayStart, dayEnd := accountClock.DayBounds(today)
	recentStart, _ := accountClock.DayBounds(clock.AddDays(today, -29))
	window := Window{
		Today:       today,
		DueBefore:   clock.AddDays(today, 2),
		DayStart:    dayStart,
		DayEnd:      dayEnd,
		RecentStart: recentStart,
		RecentEnd:   dayEnd,
	}
	return s.repo.LoadDashboard(ctx, scope, window)
}
