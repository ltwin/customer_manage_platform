package llmgateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// Each provider gets its initial call plus five retries. There is deliberately
// no cross-turn circuit state: every new turn starts at the top of its route.
const chatAttemptsPerDeployment = 6
const chatRetryInterval = 5 * time.Second

var ErrProvidersExhausted = errors.New("all model providers failed")

type chatRoutePlan struct {
	Deployments []string
	State       string
	FinalID     string
	FinalKey    string
}

func waitChatRetry(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (s *Service) chatDeployment(model, deployment string) (ModelConfig, error) {
	if deployment == "" {
		return s.catalog.Model(model)
	}
	return s.catalog.Deployment(model, deployment)
}

// The plan is written in the same transaction as the first request. Old unknown
// requests without a plan never acquire a new retry policy retroactively.
func (s *Service) ensureChatRoute(ctx context.Context, tx store.TxAccountScope, view RequestView) error {
	row, err := scanRequest(tx.QueryRowForUpdate(ctx, "llm_requests", requestColumns, "id=$2", view.ID))
	if err != nil || row.attemptsUsed != 0 {
		return err
	}
	exists, err := tx.Exists(ctx, "llm_chat_routes", "root_request_id=$2", view.ID)
	if err != nil || exists {
		return err
	}
	keys := []string{row.modelSnapshot.DeploymentKey}
	model := s.catalog.models[view.ModelKey]
	for _, key := range model.Deployments {
		if key != keys[0] && s.catalog.deployments[key].Enabled {
			keys = append(keys, key)
		}
	}
	raw, err := json.Marshal(keys)
	if err != nil {
		return err
	}
	return tx.Insert(ctx, "llm_chat_routes", []string{"root_request_id", "deployments"}, view.ID, raw)
}

func (s *Service) chatRoute(ctx context.Context, scope store.AccountScope, root string) (chatRoutePlan, bool, error) {
	var plan chatRoutePlan
	var raw []byte
	var finalID, finalKey *string
	err := scope.QueryRow(ctx, "llm_chat_routes", "deployments,state,final_request_id,final_binding_key", "root_request_id=$2", root).
		Scan(&raw, &plan.State, &finalID, &finalKey)
	if errors.Is(err, store.ErrNoRows) {
		return plan, false, nil
	}
	if err != nil {
		return plan, false, err
	}
	if finalID != nil {
		plan.FinalID = *finalID
	}
	if finalKey != nil {
		plan.FinalKey = *finalKey
	}
	err = json.Unmarshal(raw, &plan.Deployments)
	return plan, true, err
}

// routeStep is what one finished attempt licenses the route to do next.
type routeStep int

const (
	routeStop     routeStep = iota // hand the failure to the caller
	routeRetry                     // same deployment again after the interval
	routeFailover                  // next deployment after the interval
	routeDone                      // a complete result exists
)

// chatRouteRun is the in-memory progress of one walk over a durable route.
// Everything durable lives in the route and request rows; this only carries
// what the next attempt and the final error need.
type chatRouteRun struct {
	outcome   CallOutcome
	nextAt    time.Time
	admitted  bool  // this walk admitted some attempt, so it was routable at all
	budgetErr error // a later attempt no longer fit beside the holds earlier ones kept
	final     RequestView
	finalKey  string
}

func retryBindingKey(root string, ordinal int) string {
	return fmt.Sprintf("retry:%s:%d", root, ordinal)
}

func (s *Service) callRouted(ctx context.Context, session CallSession, in CallInput, chat ChatRequest, root RequestView, plan chatRoutePlan) (CallOutcome, error) {
	switch plan.State {
	case "exhausted":
		return CallOutcome{RequestID: root.ID}, ErrProvidersExhausted
	case "succeeded":
		view, err := s.Get(ctx, session.Scope, plan.FinalID)
		if err != nil {
			return CallOutcome{RequestID: plan.FinalID}, err
		}
		return s.deliver(ctx, session, session.bindingOf(plan.FinalKey), view, in.Consume)
	}
	// Use a relative timeout so an injected clock and the durable deadline agree.
	ctx, cancel := context.WithTimeout(withSink(ctx, in.Sink), session.Deadline.Sub(s.now()))
	defer cancel()
	run := chatRouteRun{outcome: CallOutcome{RequestID: root.ID}}
	for deploymentIndex, deployment := range plan.Deployments {
	attempts:
		for attempt := range chatAttemptsPerDeployment {
			ordinal := deploymentIndex*chatAttemptsPerDeployment + attempt
			next, err := s.chatAttempt(ctx, session, in, chat, root.ID, deployment, ordinal, &run)
			switch {
			case err != nil:
				return run.outcome, err
			case next == routeFailover:
				break attempts
			case next == routeDone:
				if err := s.completeChatRoute(ctx, session.Scope, root.ID, run.final.ID, run.finalKey); err != nil {
					return run.outcome, err
				}
				delivered, err := s.deliver(ctx, session, session.bindingOf(run.finalKey), run.final, in.Consume)
				delivered.Dispatched = run.outcome.Dispatched
				return delivered, err
			}
		}
	}
	return run.outcome, s.exhaustChatRoute(ctx, session.Scope, root.ID, run)
}

// chatAttempt runs or replays one ordinal of the route. A replayed ordinal
// re-admits onto its own reservation and request, so it never pays twice.
func (s *Service) chatAttempt(ctx context.Context, session CallSession, in CallInput, chat ChatRequest, root, deployment string, ordinal int, run *chatRouteRun) (routeStep, error) {
	if err := ctx.Err(); err != nil {
		return routeStop, err
	}
	// A disabled or withdrawn deployment is not a reason to send to it. This is
	// checked before admission, which would otherwise refuse a withdrawn one as
	// a conflict on every replay and pin the turn forever.
	if _, err := s.catalog.Deployment(session.ModelKey, deployment); err != nil {
		return routeFailover, nil
	}
	if delay := run.nextAt.Sub(s.now()); delay > 0 {
		if err := s.retryWait(ctx, delay); err != nil {
			return routeStop, err
		}
	}
	key := in.BindingKey
	if ordinal > 0 {
		key = retryBindingKey(root, ordinal)
	}
	selected := session
	selected.deploymentKey = deployment
	binding := session.bindingOf(key)
	reservation, view, err := s.admit(ctx, selected, binding, chat)
	if errors.Is(err, ErrBudget) && ordinal > 0 {
		// Failed attempts keep their holds, so a later one may stop fitting the
		// month. A cheaper deployment still might; otherwise the route ends.
		run.budgetErr = err
		return routeFailover, nil
	}
	if err != nil {
		return routeStop, err
	}
	run.admitted = true
	run.outcome.RequestID = view.ID
	if view.State == StatePrepared && view.AttemptsUsed == 0 {
		if in.Sink != nil && ordinal > 0 {
			in.Sink.Reset()
		}
		var sent bool
		view, sent, err = s.advanceTurn(ctx, selected, binding, view, &reservation.HoldGeneration, chat, in.Stream)
		run.outcome.Dispatched = run.outcome.Dispatched || sent
		// Database/permission failures are not provider availability failures.
		if err != nil && (view.State == "" || view.State == StateDispatching || view.State == StateStreaming) {
			return routeStop, err
		}
	}
	switch {
	case view.State == StateSucceeded:
		run.final, run.finalKey = view, key
		return routeDone, nil
	case view.CancelRequested || view.State == StateCancelled || ctx.Err() != nil:
		return routeStop, ErrCancelled
	case view.State == StateDispatching || view.State == StateStreaming:
		return routeStop, ErrUnknown // another holder owns this one-shot permit
	}
	failure, readErr := s.chatFailure(ctx, session.Scope, view.ID)
	if readErr != nil {
		return routeStop, readErr
	}
	if failure.Finished == nil {
		if err != nil {
			return routeStop, err
		}
		return routeStop, ErrUnknown // crash with no transport evidence
	}
	// Stop the result lifecycle; retain the independently metered hold.
	if err := s.stopChatAttempt(ctx, session.Scope, view.ID); err != nil {
		return routeStop, err
	}
	next := failure.next()
	if next == routeStop {
		if err != nil {
			return routeStop, err
		}
		return routeStop, &ProviderError{Outcome: OutcomeRejected, Class: failure.Class, StatusCode: failure.Status}
	}
	run.nextAt = failure.Finished.Add(chatRetryInterval)
	return next, nil
}

// exhaustChatRoute ends the route and says why no attempt succeeded: plain
// provider failure, a month that could not hold another attempt, or a model
// with no deployment that could be admitted at all.
func (s *Service) exhaustChatRoute(ctx context.Context, scope store.AccountScope, root string, run chatRouteRun) error {
	err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		_, err := tx.Update(ctx, "llm_chat_routes", "state='exhausted'", "root_request_id=$2 AND state='active'", root)
		return err
	})
	switch {
	case err != nil:
		return err
	case run.budgetErr != nil:
		return fmt.Errorf("%w: %w", ErrProvidersExhausted, run.budgetErr)
	case !run.admitted:
		return fmt.Errorf("%w: %w", ErrProvidersExhausted, ErrUnavailable)
	default:
		return ErrProvidersExhausted
	}
}

type chatAttemptFailure struct {
	Class    string
	Status   int
	Finished *time.Time
}

func (s *Service) chatFailure(ctx context.Context, scope store.AccountScope, requestID string) (chatAttemptFailure, error) {
	var failure chatAttemptFailure
	var class *string
	var status *int
	err := scope.QueryRow(ctx, "llm_requests", "failure_class,failure_http_status", "id=$2", requestID).Scan(&class, &status)
	if err != nil {
		return failure, err
	}
	if class != nil {
		failure.Class = *class
	}
	if status != nil {
		failure.Status = *status
	}
	err = scope.QueryRow(ctx, "llm_attempts", "transport_finished_at", "request_id=$2 AND attempt_number=1", requestID).Scan(&failure.Finished)
	if errors.Is(err, store.ErrNoRows) {
		err = nil
	}
	return failure, err
}

// next is the retry policy as a pure function of the stored failure evidence,
// so a replay after restart takes exactly the branch the first run took.
func (f chatAttemptFailure) next() routeStep {
	switch f.Status {
	case 429, 500, 502, 503, 504:
		return routeRetry
	case 401, 402, 403:
		return routeFailover
	}
	switch f.Class {
	// response_status: a stream that ended on a status other than completed,
	// failed or incomplete. Relays answer an upstream drop this way.
	case "timeout", "transport", "server_error", "stream_error", "incomplete_stream", "response_failed", "response_status", "admission_refused":
		return routeRetry
	case "insufficient_balance", "model_routing_changed":
		return routeFailover
	default:
		return routeStop
	}
}

func (s *Service) stopChatAttempt(ctx context.Context, scope store.AccountScope, requestID string) error {
	return scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		_, err := tx.Update(ctx, "llm_requests", "state='failed',retry_eligible=FALSE,revision=revision+1,updated_at=$3",
			"id=$2 AND state IN ('unknown','prepared') AND attempts_used=1 AND EXISTS (SELECT 1 FROM llm_attempts WHERE account_id=$1 AND request_id=$2 AND transport_finished_at IS NOT NULL)", requestID, s.now())
		return err
	})
}

func (s *Service) completeChatRoute(ctx context.Context, scope store.AccountScope, root, final, key string) error {
	return scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		_, err := tx.Update(ctx, "llm_chat_routes", "state='succeeded',final_request_id=$3,final_binding_key=$4", "root_request_id=$2 AND state='active'", root, final, key)
		return err
	})
}
