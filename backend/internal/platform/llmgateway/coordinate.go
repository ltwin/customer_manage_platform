package llmgateway

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// callNamespace is fixed forever. A turn's operation identity is derived from
// it, so changing it would detach every stored binding from its step and turn
// the next resume into a second paid call.
var callNamespace = uuid.MustParse("7353fc6d-2a8d-4ab2-b734-edb48d3406f2")

// maxBindingKeyLen bounds the caller's own step name.
const maxBindingKeyLen = 128

// CallSession binds one caller to an account, a caller group and the ceilings
// that apply to every turn it makes. One session serves exactly one model, so a
// skill can never quietly become a second, unmetered model exit.
type CallSession struct {
	Scope  store.AccountScope
	Limits func(store.TxAccountScope) LimitView

	CallerService      string
	CallerGroupID      string
	GroupLimitMicros   int64
	GroupTokenLimit    int64
	GroupDeadline      time.Time
	AccountLimitMicros int64
	AccountTokenLimit  int64

	ModelKey    string
	OutputLimit int
	Deadline    time.Time

	// EstimateInputTokens bounds the input before a request exists. It may only
	// raise the built-in bound: a hold smaller than what Prepare checks against
	// would admit a call the budget does not actually cover.
	EstimateInputTokens func(ChatRequest) int64

	// Admit is the caller's own last check, run inside the transaction that
	// records the dispatch intent. It exists because some refusals are only
	// sound when they commit together with that intent: an authorisation
	// withdrawn in a transaction that commits first must stop the bytes, and a
	// check in an earlier transaction of its own leaves a window where the
	// withdrawal commits after the check passed and before the intent does.
	//
	// It returns the caller's own error unchanged. Nil means no extra check.
	Admit func(store.TxAccountScope) error
}

func (s CallSession) validate() error {
	if s.Scope.AccountID() == "" {
		return fmt.Errorf("%w: call session needs an account scope", ErrValidation)
	}
	if s.Limits == nil {
		return fmt.Errorf("%w: call session needs platform admission", ErrValidation)
	}
	if s.CallerService == "" || s.CallerGroupID == "" || s.ModelKey == "" {
		return fmt.Errorf("%w: call session identity", ErrValidation)
	}
	if s.OutputLimit <= 0 || s.Deadline.IsZero() || s.GroupDeadline.IsZero() {
		return fmt.Errorf("%w: call session bounds", ErrValidation)
	}
	return nil
}

// CallInput is one complete turn: which step is running, what it sends, and how
// the caller's own persistence joins the result.
type CallInput struct {
	// BindingKey is the caller's own durable name for this step. Resuming the
	// same step must pass the same key — that is the whole mechanism by which a
	// restart replays an existing request instead of paying for a second call.
	// It must therefore come from something the caller persisted (a run and step
	// number, a command id), never from a value generated per process.
	BindingKey string
	Chat       ChatRequest
	Stream     bool
	// Consume runs inside the transaction that marks the result consumed, so the
	// caller's own rows and that mark commit together or not at all.
	Consume func(store.TxAccountScope, Result) error
}

// CallOutcome reports what the turn settled on.
type CallOutcome struct {
	RequestID string
	Result    Result
	// Dispatched is false when the turn answered from a result an earlier run
	// already paid for.
	Dispatched bool
	// AlreadyConsumed is true when this caller's persistence had joined the same
	// result in an earlier committed transaction, so Consume did not run again.
	AlreadyConsumed bool
}

// callBinding is the durable identity derived from one step name. The unique
// keys on the reservation and request tables are what actually store the
// binding; deriving the ids from the step name keeps that single source of
// truth instead of adding a second table that claims to own it.
type callBinding struct {
	reserveOp   string
	requestOp   string
	consumerKey string
}

func (s CallSession) bindingOf(key string) callBinding {
	return callBinding{
		reserveOp:   derivedOperationID(s.CallerService, key, "reserve"),
		requestOp:   derivedOperationID(s.CallerService, key, "request"),
		consumerKey: "bind:" + key,
	}
}

func derivedOperationID(callerService, bindingKey, purpose string) string {
	return uuid.NewSHA1(callNamespace, []byte(callerService+"\x00"+bindingKey+"\x00"+purpose)).String()
}

// ReserveOperationID is the reservation identity a turn with this binding key
// will claim. A caller that has to take a hold *before* that turn exists — a run
// whose budget must be admitted when it is created, not when a worker picks it
// up — creates the reservation under this id, and the turn then replays onto it
// instead of taking a second hold beside the first.
//
// It is exported rather than left to callers to reproduce: the derivation is the
// binding, and two spellings of it would silently double-book an account.
func ReserveOperationID(callerService, bindingKey string) string {
	return derivedOperationID(callerService, bindingKey, "reserve")
}

// Call is the one coordinated path from admission to consumption. Every
// business entry goes through it rather than repeating the reserve → prepare →
// dispatch → compensate sequence, because each step of that sequence has a
// compensation and a lock order that only hold when they run together.
//
// It is safe to call again with the same binding key after any failure or
// restart: a turn that already produced a result replays it, and a turn whose
// transport outcome was never settled refuses to run rather than paying twice.
func (s *Service) Call(ctx context.Context, session CallSession, in CallInput) (CallOutcome, error) {
	chat, err := session.chatOf(in.Chat)
	if err != nil {
		return CallOutcome{}, err
	}
	if in.BindingKey == "" || len(in.BindingKey) > maxBindingKeyLen {
		return CallOutcome{}, fmt.Errorf("%w: caller binding key", ErrValidation)
	}
	binding := session.bindingOf(in.BindingKey)

	reservation, view, err := s.admit(ctx, session, binding, chat)
	if err != nil {
		return CallOutcome{}, err
	}
	// From here on the identity exists, so every return carries it: a caller that
	// has to ask for verification needs to name the request it is asking about.
	outcome := CallOutcome{RequestID: view.ID}
	view, outcome.Dispatched, err = s.runTurn(ctx, session, binding, view, reservation.HoldGeneration, chat, in.Stream)
	if err != nil {
		return outcome, err
	}
	delivered, err := s.deliver(ctx, session, binding, view, in.Consume)
	if err != nil {
		return outcome, err
	}
	outcome.Result, outcome.AlreadyConsumed = delivered.Result, delivered.AlreadyConsumed
	return outcome, nil
}

// chatOf fills the session's fixed fields and refuses a request aimed at
// another model than the one this session is metered against.
func (s CallSession) chatOf(chat ChatRequest) (ChatRequest, error) {
	if err := s.validate(); err != nil {
		return ChatRequest{}, err
	}
	if chat.ContractVersion == "" {
		chat.ContractVersion = ContractVersion
	}
	if chat.ModelKey == "" {
		chat.ModelKey = s.ModelKey
	}
	if chat.OutputLimit == 0 {
		chat.OutputLimit = s.OutputLimit
	}
	if chat.ModelKey != s.ModelKey {
		return ChatRequest{}, fmt.Errorf("%w: a session serves exactly one model", ErrValidation)
	}
	if err := chat.Validate(); err != nil {
		return ChatRequest{}, err
	}
	return chat, nil
}

// admit takes the hold and fixes the request identity in one transaction.
//
// They may not be two commits. A reservation is claimable exactly once, and only
// while it is still unclaimed, so a Prepare that fails after its reservation
// committed leaves a hold no request will ever claim — and compensating by
// releasing that hold is worse, because `released` is not `unclaimed` either:
// the next Call with the same binding key replays straight onto it and fails
// with "reservation already claimed" forever, with no code path back. Committing
// both together means a refused preparation simply never happened, and the step
// is free to run again once the caller fixes what was refused.
//
// This matters because Prepare enforces things Reserve does not: model
// capability, the request deadline, and that the hold still covers the request.
func (s *Service) admit(
	ctx context.Context,
	session CallSession,
	binding callBinding,
	chat ChatRequest,
) (Reservation, RequestView, error) {
	// Prepare checks the request against the built-in bound, so a session
	// estimator may only raise it. Reserving less than Prepare demands would
	// admit a turn and then refuse it with a budget error it cannot act on.
	bound := EstimateInputTokens(chat)
	if session.EstimateInputTokens != nil {
		if custom := session.EstimateInputTokens(chat); custom > bound {
			bound = custom
		}
	}
	var (
		reservation Reservation
		view        RequestView
	)
	err := session.Scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		var err error
		reservation, err = s.ReserveInTx(ctx, tx, ReserveInput{
			CallerService:        session.CallerService,
			CallerOperationID:    binding.reserveOp,
			CallerGroupID:        session.CallerGroupID,
			GroupLimitMicros:     session.GroupLimitMicros,
			GroupTokenLimit:      session.GroupTokenLimit,
			GroupDeadline:        session.GroupDeadline,
			ModelKey:             session.ModelKey,
			InputTokenUpperBound: bound,
			OutputLimit:          chat.OutputLimit,
			AccountLimitMicros:   session.AccountLimitMicros,
			AccountTokenLimit:    session.AccountTokenLimit,
			ExpiresAt:            session.Deadline,
		})
		if err != nil {
			return err
		}
		view, err = s.PrepareInTx(ctx, tx, PrepareInput{
			CallerService:     session.CallerService,
			CallerOperationID: binding.requestOp,
			CallerGroupID:     session.CallerGroupID,
			ReservationID:     reservation.ID,
			ConsumerKey:       binding.consumerKey,
			Chat:              chat,
			Deadline:          session.Deadline,
		})
		return err
	})
	if err != nil {
		return Reservation{}, RequestView{}, err
	}
	return reservation, view, nil
}

// runTurn drives the request to a complete result. It dispatches only from
// prepared, and only re-dispatches after the gateway itself proved the previous
// attempt was never accepted and never billed.
func (s *Service) runTurn(
	ctx context.Context,
	session CallSession,
	binding callBinding,
	view RequestView,
	generation int64,
	chat ChatRequest,
	stream bool,
) (RequestView, bool, error) {
	dispatched := false
	for {
		switch view.State {
		case StateSucceeded:
			return view, dispatched, nil
		case StateFailed:
			return RequestView{}, dispatched, fmt.Errorf("%w: request failed as %q", ErrState, view.FailureClass)
		case StateCancelled:
			return RequestView{}, dispatched, fmt.Errorf("%w: request was cancelled", ErrCancelled)
		case StateDispatching, StateStreaming, StateUnknown:
			// The provider may already have answered and billed this identity.
			// Re-sending it to find out is exactly the double charge the one-shot
			// permit exists to prevent, so recovery goes through verification.
			return RequestView{}, dispatched, fmt.Errorf(
				"%w: request %s is unsettled and needs verification before it runs again", ErrUnknown, view.ID)
		case StatePrepared:
		default:
			return RequestView{}, dispatched, fmt.Errorf("%w: request state %q", ErrState, view.State)
		}

		var (
			permit Permit
			err    error
		)
		if view.RetryEligible {
			// The hold was given back with the rejection, so it has to be rebuilt
			// at today's ceilings — in the same transaction as the dispatch intent
			// it is rebuilt for. See RedispatchInTx for what splitting them costs.
			permit, generation, err = s.redispatch(ctx, session, binding, view.ID, generation)
		} else {
			permit, err = s.beginDispatch(ctx, session, binding, view.ID)
		}
		if err != nil {
			return RequestView{}, dispatched, err
		}
		dispatched = true
		_, execErr := s.Execute(ctx, session.Scope, session.Limits, permit, chat, stream)

		next, err := s.Get(ctx, session.Scope, view.ID)
		if err != nil {
			if execErr != nil {
				return RequestView{}, dispatched, errors.Join(execErr, err)
			}
			return RequestView{}, dispatched, err
		}
		if execErr != nil && (next.State != StatePrepared || !next.RetryEligible) {
			return RequestView{}, dispatched, execErr
		}
		view = next
	}
}

// redispatch re-admits and takes the permit in one transaction, so a refusal
// rolls the re-admission back with it and leaves the step exactly as resumable
// as it was.
func (s *Service) redispatch(
	ctx context.Context,
	session CallSession,
	binding callBinding,
	requestID string,
	generation int64,
) (Permit, int64, error) {
	var (
		permit      Permit
		reservation Reservation
	)
	err := session.Scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		if err := session.admit(tx); err != nil {
			return err
		}
		var err error
		permit, reservation, err = s.RedispatchInTx(ctx, tx, session.Limits, requestID, generation,
			session.AccountLimitMicros, session.AccountTokenLimit)
		return err
	})
	if err != nil {
		return Permit{}, generation, s.compensate(ctx, session, binding, requestID, err)
	}
	return permit, reservation.HoldGeneration, nil
}

func (s *Service) beginDispatch(ctx context.Context, session CallSession, binding callBinding, requestID string) (Permit, error) {
	var permit Permit
	err := session.Scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		if err := session.admit(tx); err != nil {
			return err
		}
		var err error
		permit, err = s.BeginDispatchInTx(ctx, tx, session.Limits(tx), requestID)
		return err
	})
	if err != nil {
		return Permit{}, s.compensate(ctx, session, binding, requestID, err)
	}
	return permit, nil
}

func (s CallSession) admit(tx store.TxAccountScope) error {
	if s.Admit == nil {
		return nil
	}
	return s.Admit(tx)
}

// compensate decides what a refused dispatch leaves behind. It returns the
// caller's error unchanged; only the side effect differs.
//
// Ending a request is only right when this call owns the turn *and* the identity
// can provably never dispatch again, so the terminal conditions are listed
// explicitly rather than assumed by default. Everything else stays exactly as it
// is, because cancelling would destroy a step that was still recoverable inside
// its deadline:
//
//   - ErrRateLimited — shared provider admission, clears on its own.
//   - ErrBudget — a ceiling another caller is occupying right now, or a month
//     bucket that a settlement below its hold will free again. The Harness
//     surfaces this as something the account can act on, so the gateway must not
//     unilaterally declare the step dead.
//   - ErrState — another worker owns the turn, or it is already terminal.
//   - anything unrecognised, including a failure to even read the request.
func (s *Service) compensate(ctx context.Context, session CallSession, binding callBinding, requestID string, err error) error {
	switch {
	case errors.Is(err, ErrDeadline), errors.Is(err, ErrAttemptsUsed), errors.Is(err, ErrCancelled):
		// This identity is finished. The request owns the reservation, so it ends
		// as a unit rather than having the hold released behind its back.
		s.endUndispatched(ctx, session, binding, requestID)
	}
	return err
}

// endUndispatched closes a request that can never dispatch. Failing to do so
// only parks budget and a retention claim, so it is logged rather than returned:
// the caller's own error is the one worth reporting.
func (s *Service) endUndispatched(ctx context.Context, session CallSession, binding callBinding, requestID string) {
	if err := session.Scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		view, err := s.CancelInTx(ctx, tx, requestID)
		if err != nil && !errors.Is(err, ErrState) {
			return err
		}
		// Only a request this transaction actually cancelled is known to be
		// incapable of ever producing a result. A turn another worker finished, or
		// one still in flight, keeps its retention claim: that claim is the only
		// thing that lets an already paid result still be consumed, and releasing
		// it here would throw the result away for good.
		if view.State != StateCancelled {
			return nil
		}
		if err := s.AbandonConsumerInTx(ctx, tx, requestID, session.CallerService, binding.consumerKey); err != nil &&
			!errors.Is(err, ErrState) {
			return err
		}
		return nil
	}); err != nil {
		s.logger.ErrorContext(ctx, "llm gateway could not end an undispatched request",
			"request_id", requestID, "error", err)
	}
}

// deliver hands the complete result to the caller's own persistence inside the
// transaction that marks it consumed. A step and its result therefore commit
// together: the caller can never hold a result the gateway still offers to
// another consumer, nor mark one consumed whose rows rolled back.
func (s *Service) deliver(
	ctx context.Context,
	session CallSession,
	binding callBinding,
	view RequestView,
	consume func(store.TxAccountScope, Result) error,
) (CallOutcome, error) {
	outcome := CallOutcome{RequestID: view.ID}
	err := session.Scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		result, already, err := s.ConsumeInTx(ctx, tx, view.ID, session.CallerService, binding.consumerKey,
			func(r Result) error {
				if consume == nil {
					return nil
				}
				return consume(tx, r)
			})
		if err != nil {
			return err
		}
		outcome.Result, outcome.AlreadyConsumed = result, already
		return nil
	})
	if err != nil {
		return CallOutcome{}, err
	}
	return outcome, nil
}
