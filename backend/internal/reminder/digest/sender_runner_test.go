package digest

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

type runnerSenderProbe struct{ accounts []string }

func (s *runnerSenderProbe) SendNext(_ context.Context, account store.ScopedAccount) (bool, error) {
	s.accounts = append(s.accounts, account.AccountID)
	if account.AccountID == "broken" {
		return false, errors.New("recipient DB failure")
	}
	return false, nil
}

func TestDeliverySenderRunnerAccountFailureDoesNotStarveOtherAccounts(t *testing.T) {
	accounts := fixedAccounts{accounts: []store.ScopedAccount{{AccountID: "broken"}, {AccountID: "healthy"}}}
	probe := &runnerSenderProbe{}
	NewDeliverySenderRunner(accounts, probe).RunOnce(context.Background())
	if !reflect.DeepEqual(probe.accounts, []string{"broken", "healthy"}) {
		t.Fatalf("account failure starved suffix: %v", probe.accounts)
	}
}

func TestDeliverySenderRunnerPausesDuringIntegrationSuspension(t *testing.T) {
	now := time.Date(2026, 7, 15, 8, 0, 0, 0, time.UTC)
	clock := &fakeClock{now: now}
	state := NewIntegrationState(nil).WithClock(clock.Now)
	state.Suspend(TelegramErrorInvalidAuth, 5*time.Minute)
	accounts := fixedAccounts{accounts: []store.ScopedAccount{{AccountID: "healthy"}}}
	probe := &runnerSenderProbe{}
	runner := NewDeliverySenderRunner(accounts, probe).WithIntegrationState(state)

	runner.RunOnce(context.Background())
	if len(probe.accounts) != 0 {
		t.Fatalf("suspended runner attempted delivery: %v", probe.accounts)
	}
	clock.Set(now.Add(5 * time.Minute))
	runner.RunOnce(context.Background())
	if !reflect.DeepEqual(probe.accounts, []string{"healthy"}) {
		t.Fatalf("runner did not resume at probe window: %v", probe.accounts)
	}
}
