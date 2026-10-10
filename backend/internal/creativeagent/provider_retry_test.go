package creativeagent

import (
	"context"
	"testing"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/llmgateway"
)

// useRetryingGateway swaps in a gateway with the production retry chain and a
// wait that records instead of sleeping.
func useRetryingGateway(t *testing.T, f *fixture) *int {
	t.Helper()
	models, err := llmgateway.DefaultCatalog(deepSeekOnly)
	if err != nil {
		t.Fatal(err)
	}
	waits := 0
	gateway, err := llmgateway.New(llmgateway.Config{
		Catalog: models, Providers: map[llmgateway.ProviderKey]llmgateway.Provider{llmgateway.ProviderOpenAICompatible: f.vendor},
		Credential: deepSeekOnly, DefaultBudget: llmgateway.BudgetPolicy{MonthlyLimitMicros: 100_000_000, MonthlyTokenLimit: 100_000_000},
		ChatRetry: true, RetryWait: func(ctx context.Context, delay time.Duration) error {
			if delay <= 0 || delay > 5*time.Second {
				t.Fatalf("delay %v", delay)
			}
			waits++
			return ctx.Err()
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	f.service.gateway = gateway
	return &waits
}

func TestExhaustedProvidersEndRunWithoutReconciliation(t *testing.T) {
	f := setup(t)
	waits := useRetryingGateway(t, f)
	f.queue()
	f.vendor.fail = &llmgateway.ProviderError{Outcome: llmgateway.OutcomeUnknown, Class: "server_error", StatusCode: 503}
	c := f.conversation(f.alice, f.canvasID)
	run, err := f.startRun(f.alice, c.ID, f.consent(f.alice, c.ID).ID, InstructionSegment{Type: "text", Text: "整理参考"})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.work(f.alice, run.ID); err != nil {
		t.Fatal(err)
	}
	after, err := f.service.ReadRun(t.Context(), f.alice, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.State != RunFailed || after.ErrorCode != "creative_model_providers_exhausted" || after.SettlementState != "unknown" {
		t.Fatalf("run must fail while retaining billing evidence: %+v", after)
	}
	if f.vendor.calls() != 6 || *waits != 5 {
		t.Fatalf("calls=%d waits=%d", f.vendor.calls(), *waits)
	}
	if n := f.count("creative_agent_slots", "run_id=$1", run.ID); n != 0 {
		t.Fatal("failed run still holds its slot")
	}
	if n := f.count("llm_usage_reservations", "settlement_state='unknown' AND remaining_hold_micros>0"); n != 6 {
		t.Fatalf("unknown billing evidence lost: %d", n)
	}
	if _, err := f.service.SweepRuns(t.Context(), f.alice, 20); err != nil {
		t.Fatal(err)
	}
	after, err = f.service.ReadRun(t.Context(), f.alice, run.ID)
	if err != nil || after.State != RunFailed {
		t.Fatalf("sweep reopened run: %+v %v", after, err)
	}
	if err := f.work(f.alice, run.ID); err != nil {
		t.Fatal(err)
	}
	if f.vendor.calls() != 6 {
		t.Fatal("redelivery purchased more attempts")
	}
}

// The step links only the request that answered; the attempt it superseded
// still holds unknown money, and the run must say so.
func TestRetriedSuccessKeepsSupersededAttemptUnsettled(t *testing.T) {
	f := setup(t)
	useRetryingGateway(t, f)
	f.queue()
	f.vendor.script = func(_ context.Context, r llmgateway.ProviderRequest, n int) (llmgateway.Result, error) {
		if n == 1 {
			return llmgateway.Result{}, &llmgateway.ProviderError{Outcome: llmgateway.OutcomeUnknown, Class: "server_error", StatusCode: 503}
		}
		input, cached, output := int64(120), int64(0), int64(40)
		return llmgateway.Result{
			Text: "好的，我来整理这些参考。", FinishReason: llmgateway.FinishStop, Model: r.Snapshot.RequestModelID,
			Usage:             llmgateway.UsageEvidence{InputTotalTokens: &input, InputCachedTokens: &cached, OutputTokens: &output},
			ProviderRequestID: "stub-retried",
		}, nil
	}
	c := f.conversation(f.alice, f.canvasID)
	run, err := f.startRun(f.alice, c.ID, f.consent(f.alice, c.ID).ID, InstructionSegment{Type: "text", Text: "整理参考"})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.work(f.alice, run.ID); err != nil {
		t.Fatal(err)
	}
	after, err := f.service.ReadRun(t.Context(), f.alice, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.State != RunSucceeded || after.SettlementState != "unknown" || f.vendor.calls() != 2 {
		t.Fatalf("retried success hid the superseded attempt: %+v calls=%d", after, f.vendor.calls())
	}
}
