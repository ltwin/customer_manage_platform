package ingestion

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/samson/customer-manage-platform/backend/internal/planningmedia"
	"github.com/samson/customer-manage-platform/backend/internal/platform/idempotency"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
	"github.com/samson/customer-manage-platform/backend/internal/shootplanning"
)

type Application struct {
	repo        Repository
	idempotency *idempotency.Executor
	now         func() time.Time
	core        *shootplanning.Application
	media       *planningmedia.Application
}

func NewApplication(repo Repository, executor *idempotency.Executor, options ...func(*Application)) *Application {
	app := &Application{repo: repo, idempotency: executor, now: time.Now}
	for _, option := range options {
		if option != nil {
			option(app)
		}
	}
	return app
}
func WithApplicationClock(now func() time.Time) func(*Application) {
	return func(app *Application) {
		if now != nil {
			app.now = now
		}
	}
}
func WithCoreApplication(core *shootplanning.Application) func(*Application) {
	return func(app *Application) { app.core = core }
}
func WithPlanningMediaApplication(media *planningmedia.Application) func(*Application) {
	return func(app *Application) { app.media = media }
}

type CreateInput struct {
	PlanID                 string                 `json:"plan_id"`
	ExpectedPlanRevision   int64                  `json:"expected_plan_revision"`
	SourceText             string                 `json:"source_text,omitempty"`
	StagedAssetIntentCount int                    `json:"staged_asset_intent_count,omitempty"`
	StagedAssetIntents     []AssetBindingDecision `json:"staged_asset_intents,omitempty"`
}
type PreviewInput struct {
	PlanID                  string                           `json:"plan_id"`
	SessionID               string                           `json:"session_id"`
	ExpectedSessionRevision int64                            `json:"expected_session_revision"`
	SourceText              *string                          `json:"source_text,omitempty"`
	StagedAssetIntentCount  int                              `json:"staged_asset_intent_count,omitempty"`
	StagedAssetIntents      []AssetBindingDecision           `json:"staged_asset_intents,omitempty"`
	ContentOverrides        []ContentCandidateOverride       `json:"content_overrides,omitempty"`
	ReferenceLinkOverrides  []ReferenceLinkCandidateOverride `json:"reference_link_overrides,omitempty"`
	ReadinessLinkOverrides  []ReadinessLinkCandidateOverride `json:"readiness_link_overrides,omitempty"`
	ReadinessLinkSelections []ReadinessLinkSelection         `json:"readiness_link_selections,omitempty"`
}
type TransitionInput struct {
	PlanID                  string       `json:"plan_id"`
	SessionID               string       `json:"session_id"`
	ExpectedSessionRevision int64        `json:"expected_session_revision"`
	State                   SessionState `json:"state"`
}

func (a *Application) CreateSession(ctx context.Context, scope store.AccountScope, key string, input CreateInput) (Session, error) {
	if strings.TrimSpace(input.SourceText) == "" && len(input.StagedAssetIntents) == 0 {
		return Session{}, errors.New("source_text_required")
	}
	sessionID := "ing_" + uuid.NewString()
	parsed, err := Parse(ParseInput{SessionID: sessionID, ParserVersion: ParserVersionV1, FirstSeenSessionRevision: 1, SourceText: input.SourceText, StagedAssetIntentCount: input.StagedAssetIntentCount, StagedAssetIntents: input.StagedAssetIntents})
	if err != nil {
		return Session{}, err
	}
	canonical, err := json.Marshal(struct {
		PlanID                 string                 `json:"plan_id"`
		ExpectedPlanRevision   int64                  `json:"expected_plan_revision"`
		SourceChecksum         string                 `json:"source_checksum"`
		SourceText             string                 `json:"source_text"`
		StagedAssetIntentCount int                    `json:"staged_asset_intent_count"`
		StagedAssetIntents     []AssetBindingDecision `json:"staged_asset_intents,omitempty"`
	}{input.PlanID, input.ExpectedPlanRevision, parsed.SourceChecksum, input.SourceText, input.StagedAssetIntentCount, input.StagedAssetIntents})
	if err != nil {
		return Session{}, err
	}
	response, err := a.idempotency.Execute(ctx, scope, idempotency.Request{Operation: idempotency.OperationPlanIngestionCreate, Key: key, ResourceIdentity: idempotency.PlanIngestionCreateResource(input.PlanID), CanonicalBody: canonical}, func(tx store.TxAccountScope) (idempotency.StoredResponse, error) {
		session, err := a.repo.CreateSessionInScope(ctx, tx, CreateSessionInput{SessionID: sessionID, PlanID: input.PlanID, ExpectedPlanRevision: input.ExpectedPlanRevision, SourceText: input.SourceText, Parsed: parsed})
		if err != nil {
			return idempotency.StoredResponse{}, err
		}
		if _, _, err := a.repo.RecordBuildActivityInScope(ctx, tx, ActivityFact{
			PlanID: input.PlanID, SessionID: session.ID, TickID: "create:" + key, Kind: "create",
		}); err != nil {
			return idempotency.StoredResponse{}, err
		}
		body, err := json.Marshal(session)
		return idempotency.StoredResponse{Status: 201, Body: body}, err
	})
	if err != nil {
		return Session{}, err
	}
	var session Session
	if err := json.Unmarshal(response.Body, &session); err != nil {
		return Session{}, fmt.Errorf("decode ingestion create replay: %w", err)
	}
	return session, nil
}

func (a *Application) GetSession(ctx context.Context, scope store.AccountScope, planID, sessionID string) (Session, error) {
	session, err := a.repo.GetSession(ctx, scope, planID, sessionID)
	if err != nil {
		return Session{}, err
	}
	if session.RedactedAt != nil {
		session.SourceText = nil
		session.CandidateSnapshot = CandidateSnapshot{ContentCandidates: session.CandidateSnapshot.ContentCandidates, ReadinessLinkCandidates: session.CandidateSnapshot.ReadinessLinkCandidates, ReferenceLinkCandidates: session.CandidateSnapshot.ReferenceLinkCandidates, DroppedCandidates: session.CandidateSnapshot.DroppedCandidates, AssetBindingCandidates: session.CandidateSnapshot.AssetBindingCandidates}
	}
	return session, nil
}

func (a *Application) Preview(ctx context.Context, scope store.AccountScope, key string, input PreviewInput) (Session, error) {
	if input.ExpectedSessionRevision < 1 || strings.TrimSpace(input.SessionID) == "" || strings.TrimSpace(input.PlanID) == "" {
		return Session{}, ErrReparseInput
	}
	canonical, err := json.Marshal(input)
	if err != nil {
		return Session{}, err
	}
	response, err := a.idempotency.Execute(ctx, scope, idempotency.Request{Operation: idempotency.OperationPlanIngestionPreview, Key: key, ResourceIdentity: idempotency.PlanIngestionSessionResource(input.PlanID, input.SessionID), CanonicalBody: canonical}, func(tx store.TxAccountScope) (idempotency.StoredResponse, error) {
		current, err := a.repo.LockSession(ctx, tx, input.PlanID, input.SessionID)
		if err != nil {
			return idempotency.StoredResponse{}, err
		}
		if current.State != SessionEditing {
			return idempotency.StoredResponse{}, ErrSessionTerminal
		}
		if current.Revision != input.ExpectedSessionRevision {
			return idempotency.StoredResponse{}, ErrSessionRevision
		}
		source := current.SourceText
		if input.SourceText != nil {
			source = input.SourceText
		}
		if source == nil || (strings.TrimSpace(*source) == "" && len(input.StagedAssetIntents) == 0 && len(current.CandidateSnapshot.AssetBindingCandidates) == 0) {
			return idempotency.StoredResponse{}, errors.New("source_text_required")
		}
		assetIntents := input.StagedAssetIntents
		if assetIntents == nil {
			assetIntents = append([]AssetBindingDecision(nil), current.CandidateSnapshot.AssetBindingCandidates...)
		}
		parsed, err := Parse(ParseInput{SessionID: current.ID, ParserVersion: current.ParserVersion, FirstSeenSessionRevision: current.Revision + 1, SourceText: *source, StagedAssetIntentCount: input.StagedAssetIntentCount, StagedAssetIntents: assetIntents})
		if err != nil {
			return idempotency.StoredResponse{}, err
		}
		next, err := Reconcile(current.CandidateSnapshot, parsed, current.Revision+1)
		if err != nil {
			return idempotency.StoredResponse{}, err
		}
		parsed.ContentCandidates = next.ContentCandidates
		parsed.ReadinessLinkCandidates = next.ReadinessLinkCandidates
		parsed.ReferenceLinkCandidates = next.ReferenceLinkCandidates
		parsed.DroppedCandidates = next.DroppedCandidates
		parsed.Segments = next.Segments
		if err := applyPreviewOverrides(&parsed, current, input); err != nil {
			return idempotency.StoredResponse{}, err
		}
		material, err := previewMateriallyChanged(current, *source, parsed)
		if err != nil {
			return idempotency.StoredResponse{}, err
		}
		if !material {
			body, err := json.Marshal(current)
			return idempotency.StoredResponse{Status: 200, Body: body}, err
		}
		updated, err := a.repo.ReplacePreviewInScope(ctx, tx, current, *source, parsed)
		if err != nil {
			return idempotency.StoredResponse{}, err
		}
		if _, _, err := a.repo.RecordBuildActivityInScope(ctx, tx, ActivityFact{
			PlanID: input.PlanID, SessionID: input.SessionID, TickID: "preview:" + key, Kind: "preview",
		}); err != nil {
			return idempotency.StoredResponse{}, err
		}
		body, err := json.Marshal(updated)
		return idempotency.StoredResponse{Status: 200, Body: body}, err
	})
	if err != nil {
		return Session{}, err
	}
	var session Session
	if err := json.Unmarshal(response.Body, &session); err != nil {
		return Session{}, fmt.Errorf("decode ingestion preview replay: %w", err)
	}
	return session, nil
}

func (a *Application) Transition(ctx context.Context, scope store.AccountScope, key string, input TransitionInput) (Session, error) {
	// Committing is deliberately only available through Commit, which writes
	// the core batch, reference links, media bindings, activity tick and the
	// terminal session state in one transaction.  The public transition seam
	// is therefore abandon-only.
	if input.State != SessionAbandoned {
		return Session{}, ErrReparseInput
	}
	canonical, err := json.Marshal(input)
	if err != nil {
		return Session{}, err
	}
	response, err := a.idempotency.Execute(ctx, scope, idempotency.Request{Operation: idempotency.OperationPlanIngestionTransition, Key: key, ResourceIdentity: idempotency.PlanIngestionSessionResource(input.PlanID, input.SessionID), CanonicalBody: canonical}, func(tx store.TxAccountScope) (idempotency.StoredResponse, error) {
		current, err := a.repo.LockSession(ctx, tx, input.PlanID, input.SessionID)
		if err != nil {
			return idempotency.StoredResponse{}, err
		}
		if current.Revision != input.ExpectedSessionRevision {
			return idempotency.StoredResponse{}, ErrSessionRevision
		}
		updated, err := a.repo.TransitionInScope(ctx, tx, current, input.State)
		if err != nil {
			return idempotency.StoredResponse{}, err
		}
		if input.State == SessionAbandoned {
			if _, _, err := a.repo.AccumulateAndAbandonInScope(ctx, tx, ActivityFact{
				PlanID: input.PlanID, SessionID: input.SessionID, TickID: "abandon:" + key,
			}); err != nil {
				return idempotency.StoredResponse{}, err
			}
		}
		body, err := json.Marshal(updated)
		return idempotency.StoredResponse{Status: 200, Body: body}, err
	})
	if err != nil {
		return Session{}, err
	}
	var session Session
	if err := json.Unmarshal(response.Body, &session); err != nil {
		return Session{}, fmt.Errorf("decode ingestion transition replay: %w", err)
	}
	return session, nil
}

func previewMateriallyChanged(current Session, sourceText string, parsed ParseOutput) (bool, error) {
	if current.SourceText == nil || *current.SourceText != sourceText || current.SourceChecksum != parsed.SourceChecksum {
		return true, nil
	}
	currentJSON, err := json.Marshal(current.CandidateSnapshot)
	if err != nil {
		return false, err
	}
	nextJSON, err := json.Marshal(snapshotFromParse(parsed))
	if err != nil {
		return false, err
	}
	return !bytes.Equal(currentJSON, nextJSON), nil
}

type CommitResult struct {
	Session        Session                        `json:"session"`
	PlanBatch      *shootplanning.PlanBatchResult `json:"plan_batch,omitempty"`
	ReferenceLinks []PlanReferenceLink            `json:"reference_links"`
	MediaBindings  []planningmedia.BindingResult  `json:"media_bindings"`
}

func (a *Application) Commit(ctx context.Context, scope store.AccountScope, key string, input IngestionCommitCanonicalV1) (CommitResult, error) {
	prepared, err := PrepareIngestionCommit(input)
	if err != nil {
		return CommitResult{}, err
	}
	response, err := a.idempotency.Execute(ctx, scope, idempotency.Request{Operation: idempotency.OperationPlanIngestionCommit, Key: key, ResourceIdentity: idempotency.PlanIngestionSessionResource(input.PlanID, input.SessionID), CanonicalBody: prepared.Canonical}, func(tx store.TxAccountScope) (idempotency.StoredResponse, error) {
		current, err := a.repo.LockSession(ctx, tx, input.PlanID, input.SessionID)
		if err != nil {
			return idempotency.StoredResponse{}, err
		}
		if current.State != SessionEditing || current.Revision != input.ExpectedSessionRevision {
			return idempotency.StoredResponse{}, ErrSessionRevision
		}
		if err := validateCommitAgainstSession(input, current); err != nil {
			return idempotency.StoredResponse{}, err
		}
		if prepared.Core != nil && a.core == nil {
			return idempotency.StoredResponse{}, errors.New("core_application_missing")
		}
		var batch *shootplanning.PlanBatchResult
		planRevision := input.ExpectedPlanRevision
		if prepared.Core != nil {
			value, err := a.core.CommitPreparedPlanBatchInScope(ctx, tx, *prepared.Core)
			if err != nil {
				return idempotency.StoredResponse{}, err
			}
			batch, planRevision = &value, value.Revision
		}
		resolved := map[string]string{}
		if batch != nil {
			for _, created := range batch.CreatedIDs {
				resolved[created.ClientRef] = created.ServerID
			}
		}
		targets := make([]ReferenceTarget, 0, len(prepared.References))
		for _, link := range prepared.References {
			targetID := link.TargetClientOrIDRef
			if link.TargetKind == "plan" {
				targetID = input.PlanID
			} else if value, ok := resolved[targetID]; ok {
				targetID = value
			}
			targets = append(targets, ReferenceTarget{CandidateID: link.CandidateID, TargetKind: link.TargetKind, TargetID: targetID})
		}
		proofs, err := AuthorizeReferenceLinkTargetsInScope(ctx, tx, input.PlanID, planRevision, targets, "reference_link_create")
		if err != nil {
			return idempotency.StoredResponse{}, err
		}
		referenceLinks, err := CommitReferenceLinksInScope(ctx, tx, input.PlanID, input.SessionID, prepared.References, proofs)
		if err != nil {
			return idempotency.StoredResponse{}, err
		}
		mediaResults := make([]planningmedia.BindingResult, 0, len(input.AssetBindings))
		if len(input.AssetBindings) > 0 && a.media == nil {
			return idempotency.StoredResponse{}, errors.New("planning_media_application_missing")
		}
		for _, binding := range input.AssetBindings {
			targetID := binding.TargetClientOrIDRef
			if binding.TargetKind == "plan" {
				targetID = input.PlanID
			} else if value, ok := resolved[targetID]; ok {
				targetID = value
			}
			holderKind := planningmedia.HolderShot
			if binding.TargetKind == "plan" {
				holderKind = planningmedia.HolderPlan
			}
			result, err := a.media.BindPreparedAssetsInScope(ctx, tx, planningmedia.PreparedBindingInput{PlanID: input.PlanID, AssetID: binding.AssetID, Generation: binding.Generation, HolderKind: holderKind, HolderID: targetID, Purpose: planningmedia.Purpose(binding.Purpose), ExpectedPlanRevision: planRevision})
			if err != nil {
				return idempotency.StoredResponse{}, err
			}
			mediaResults = append(mediaResults, result)
		}
		if _, _, err := a.repo.RecordBuildActivityInScope(ctx, tx, ActivityFact{PlanID: input.PlanID, SessionID: input.SessionID, TickID: "commit:" + key, Kind: "commit"}); err != nil {
			return idempotency.StoredResponse{}, err
		}
		finished, err := a.repo.TransitionInScope(ctx, tx, current, SessionCommitted)
		if err != nil {
			return idempotency.StoredResponse{}, err
		}
		result := CommitResult{Session: finished, PlanBatch: batch, ReferenceLinks: referenceLinks, MediaBindings: mediaResults}
		body, err := json.Marshal(result)
		return idempotency.StoredResponse{Status: 200, Body: body}, err
	})
	if err != nil {
		return CommitResult{}, err
	}
	var result CommitResult
	if err := json.Unmarshal(response.Body, &result); err != nil {
		return CommitResult{}, fmt.Errorf("decode ingestion commit replay: %w", err)
	}
	return result, nil
}
