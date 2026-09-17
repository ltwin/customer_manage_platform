package creativeagent

import (
	"context"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"

	"github.com/samson/customer-manage-platform/backend/internal/platform/llmgateway"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// Each construction injects the current trusted claim. No account, model or
// transaction handle is recovered from Eino's serialized session values.
type runExecution struct {
	runtime    *runtimeHandler
	checkpoint *runCheckpoint
	config     adk.ChatModelAgentConfig
	prompt     []*schema.Message
	outcome    turnOutcome
}

func (s *Service) newRunExecution(ctx context.Context, scope store.AccountScope, held claim) (*runExecution, error) {
	if err := s.stillRunnable(ctx, scope, held); err != nil {
		return nil, err
	}
	prompt, revisions, err := s.assemble(ctx, scope, held)
	if err != nil {
		return nil, err
	}

	execution := &runExecution{prompt: prompt}
	var current currentStep
	runtime := &runtimeHandler{BaseChatModelAgentMiddleware: &adk.BaseChatModelAgentMiddleware{}, service: s, scope: scope, held: held, revisions: revisions, current: &current}
	adapter, err := s.gateway.NewEinoModel(llmgateway.ModelSession{
		CallSession: llmgateway.CallSession{
			Scope:           scope,
			Limits:          func(tx store.TxAccountScope) llmgateway.LimitView { return tx.LLMLimitView() },
			CallerService:   callerService,
			CallerGroupID:   held.RunID,
			GroupTokenLimit: runTokenCeiling(s.limits, held.Model),
			GroupDeadline:   held.Deadline,
			ModelKey:        held.Model.ModelKey,
			OutputLimit:     held.Model.Capability.MaxOutputTokens,
			Deadline:        held.Deadline,
			// The last check, inside the transaction that records the dispatch
			// intent. An authorisation withdrawn in a transaction that commits
			// first stops the bytes; one withdrawn after is honestly too late.
			Admit: func(tx store.TxAccountScope) error {
				return s.admitDispatchInTx(ctx, tx, held, revisions)
			},
		},
		// The step is the durable name of this turn, so a resumed run replays
		// the request it already paid for instead of buying a second one.
		TurnKey: func(ctx context.Context, chat llmgateway.ChatRequest) (string, error) {
			previous, _ := current.get()
			stepID, bindingKey, err := s.prepareModelStep(ctx, scope, held, previous, chat)
			if err != nil {
				return "", err
			}
			current.set(stepID, bindingKey)
			return bindingKey, nil
		},
		Consume: func(tx store.TxAccountScope, result llmgateway.Result) error {
			return execution.consumeInTx(ctx, tx, result)
		},
	})
	if err != nil {
		return nil, translateGateway(err)
	}
	toolsConfig, handlers, err := runtime.configure(ctx)
	if err != nil {
		return nil, err
	}
	execution.config = adk.ChatModelAgentConfig{
		Name:        agentName,
		Description: "创作助手",
		Instruction: instructionOf(held.Skill),
		Model:       adapter,
		ToolsConfig: toolsConfig,
		Handlers:    handlers,
		// The framework's own ceiling is a second line of defence, never the
		// budget: the run's persisted turn count is what actually bounds it.
		MaxIterations: s.limits.ModelTurns,
	}

	execution.runtime = runtime
	execution.checkpoint, err = newRunCheckpoint(runtime)
	if err != nil {
		return nil, err
	}
	return execution, nil
}

// The Gateway calls this inside its consumer transaction. Losing execution
// authority must roll back consumption so the current holder can save the result.
func (e *runExecution) consumeInTx(ctx context.Context, tx store.TxAccountScope, result llmgateway.Result) error {
	h := e.runtime
	stepID, _ := h.current.get()
	if err := h.planInTx(ctx, tx, stepID, result); err != nil {
		return err
	}
	h.mu.Lock()
	rejected := h.resultErr != nil
	h.mu.Unlock()
	if rejected {
		result.Text = ""
	}
	delivered, err := h.service.consumeResultInTx(ctx, tx, h.held, stepID, result)
	if err != nil {
		return err
	}
	e.outcome.Delivered = e.outcome.Delivered || delivered
	return nil
}

func (e *runExecution) run(ctx context.Context) (turnOutcome, error) {
	h := e.runtime
	_, found, err := e.checkpoint.Get(ctx, h.held.RunID)
	if err != nil {
		return e.outcome, err
	}
	if !found {
		count, err := h.scope.Count(ctx, "creative_agent_steps", "run_id=$2", h.held.RunID)
		if err != nil {
			return e.outcome, err
		}
		if count != 0 {
			return e.outcome, errCheckpointMissing
		}
	}
	agent, err := adk.NewChatModelAgent(ctx, &e.config)
	if err != nil {
		return e.outcome, err
	}
	runner := adk.NewRunner(ctx, adk.RunnerConfig{Agent: agent, CheckPointStore: e.checkpoint})
	var iterator *adk.AsyncIterator[*adk.AgentEvent]
	if found {
		iterator, err = runner.Resume(ctx, h.held.RunID)
		if err != nil {
			return e.outcome, err
		}
	} else {
		iterator = runner.Run(ctx, e.prompt, adk.WithCheckPointID(h.held.RunID))
	}
	runErr := drainAgent(iterator)
	// Keep queries after cancellation bounded, and derive delivery from the
	// journal: a replayed Gateway result does not invoke Consume a second time.
	lookupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), finishTimeout)
	defer cancel()
	step, binding := h.current.get()
	e.outcome.StepID = step
	if binding != "" {
		if view, err := h.service.gateway.RequestForBinding(lookupCtx, h.scope, callerService, binding); err == nil {
			e.outcome.RequestID = view.ID
		}
	}
	delivered, err := h.scope.Count(lookupCtx, "creative_agent_messages", "run_id=$2 AND role='assistant' AND status='complete'", h.held.RunID)
	if err != nil && runErr == nil {
		runErr = err
	}
	e.outcome.Delivered = e.outcome.Delivered || delivered > 0
	return e.outcome, translateGateway(runErr)
}
