package settings

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store/storetest"
	"github.com/samson/customer-manage-platform/backend/internal/shootplanning/business"
)

func TestMain(m *testing.M) { storetest.Main(m, store.MigrateUp) }

func TestPlanningBusinessRulesFirstWriteCASAndReplacementSemantics(t *testing.T) {
	ctx := context.Background()
	db := openSettingsStore(t)
	if err := db.CreateAccount(ctx, "settings-business-account", "test-hash"); err != nil {
		t.Fatal(err)
	}
	scope := db.ScopeFor(auth.AccountContext{AccountID: "settings-business-account"})
	service := NewService(NewPostgresRepository())

	start := make(chan struct{})
	results := make(chan error, 2)
	var waitGroup sync.WaitGroup
	for _, amount := range []int{100, 200} {
		amount := amount
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			<-start
			_, err := service.Patch(ctx, scope, PatchInput{PlanningBusinessRules: &PlanningBusinessRulesPatch{
				ExpectedRevision: 0,
				Overrides:        business.RuleOverrides{"assistant_unit_amount": integerPointer(amount)},
			}})
			results <- err
		}()
	}
	close(start)
	waitGroup.Wait()
	close(results)

	var succeeded, conflicted int
	for err := range results {
		switch {
		case err == nil:
			succeeded++
		case errors.Is(err, ErrPlanningBusinessRuleRevision):
			conflicted++
		default:
			t.Fatalf("unexpected concurrent patch error: %v", err)
		}
	}
	if succeeded != 1 || conflicted != 1 {
		t.Fatalf("first write CAS results: succeeded=%d conflicted=%d", succeeded, conflicted)
	}

	current, err := service.Get(ctx, scope)
	if err != nil {
		t.Fatal(err)
	}
	if current.PlanningBusinessRuleRevision != 1 || len(current.PlanningBusinessRuleOverrides) != 1 {
		t.Fatalf("first material rule snapshot = %+v", current)
	}
	var fenceCreatedAt time.Time
	if err := scope.QueryRow(ctx, "account_settings_mutation_fences", "created_at", "TRUE").Scan(&fenceCreatedAt); err != nil {
		t.Fatal(err)
	}

	same, err := service.Patch(ctx, scope, PatchInput{PlanningBusinessRules: &PlanningBusinessRulesPatch{
		ExpectedRevision: 1,
		Overrides:        cloneBusinessRuleOverrides(current.PlanningBusinessRuleOverrides),
	}})
	if err != nil || same.PlanningBusinessRuleRevision != 1 {
		t.Fatalf("same map should be a rule no-op: settings=%+v err=%v", same, err)
	}

	inherited, err := service.Patch(ctx, scope, PatchInput{PlanningBusinessRules: &PlanningBusinessRulesPatch{
		ExpectedRevision: 1,
		Overrides:        business.RuleOverrides{},
	}})
	if err != nil || inherited.PlanningBusinessRuleRevision != 2 || len(inherited.PlanningBusinessRuleOverrides) != 0 {
		t.Fatalf("value to inherit replacement failed: settings=%+v err=%v", inherited, err)
	}

	explicit, err := service.Patch(ctx, scope, PatchInput{PlanningBusinessRules: &PlanningBusinessRulesPatch{
		ExpectedRevision: 2,
		Overrides: business.RuleOverrides{
			"assistant_unit_amount":     nil,
			"extra_retouch_unit_amount": integerPointer(0),
		},
	}})
	if err != nil || explicit.PlanningBusinessRuleRevision != 3 {
		t.Fatalf("explicit null and zero patch failed: settings=%+v err=%v", explicit, err)
	}
	if value, ok := explicit.PlanningBusinessRuleOverrides["assistant_unit_amount"]; !ok || value != nil {
		t.Fatalf("explicit null was not preserved: %+v", explicit.PlanningBusinessRuleOverrides)
	}
	if value := explicit.PlanningBusinessRuleOverrides["extra_retouch_unit_amount"]; value == nil || *value != 0 {
		t.Fatalf("explicit zero was not preserved: %+v", explicit.PlanningBusinessRuleOverrides)
	}

	newTimezone := "America/New_York"
	_, err = service.Patch(ctx, scope, PatchInput{
		Timezone: &newTimezone,
		PlanningBusinessRules: &PlanningBusinessRulesPatch{
			ExpectedRevision: 0,
			Overrides:        business.RuleOverrides{},
		},
	})
	if !errors.Is(err, ErrPlanningBusinessRuleRevision) {
		t.Fatalf("combined timezone patch should fail on stale business CAS: %v", err)
	}
	afterConflict, err := service.Get(ctx, scope)
	if err != nil {
		t.Fatal(err)
	}
	if afterConflict.Timezone != DefaultTimezone || afterConflict.PlanningBusinessRuleRevision != 3 {
		t.Fatalf("combined CAS failure was not atomic: %+v", afterConflict)
	}
	var finalFenceCreatedAt time.Time
	if err := scope.QueryRow(ctx, "account_settings_mutation_fences", "created_at", "TRUE").Scan(&finalFenceCreatedAt); err != nil {
		t.Fatal(err)
	}
	if !finalFenceCreatedAt.Equal(fenceCreatedAt) {
		t.Fatalf("mutation fence creation time changed: before=%v after=%v", fenceCreatedAt, finalFenceCreatedAt)
	}
}

func openSettingsStore(t *testing.T) *store.Store {
	t.Helper()
	ctx := context.Background()
	databaseURL := storetest.NewURL(t)
	db, err := store.Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	return db
}

func integerPointer(value int) *int { return &value }
