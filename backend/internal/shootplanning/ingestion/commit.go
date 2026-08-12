package ingestion

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
	"github.com/samson/customer-manage-platform/backend/internal/shootplanning"
)

var (
	ErrCommitInvalid    = errors.New("ingestion_commit_invalid")
	ErrNoKeptCandidates = errors.New("ingestion_commit_no_kept_candidates")
	ErrReferenceTarget  = errors.New("reference_link_target_invalid")
	ErrReferenceURL     = errors.New("reference_link_url_invalid")
)

type DecisionAction string

const (
	DecisionKeep    DecisionAction = "keep"
	DecisionDiscard DecisionAction = "discard"
)

type ShotDecision struct {
	CandidateID          string                  `json:"candidate_id"`
	Action               DecisionAction          `json:"action"`
	ClientRef            string                  `json:"client_ref,omitempty"`
	Shot                 shootplanning.ShotWrite `json:"shot,omitempty"`
	PositionAfterShotRef *string                 `json:"position_after_client_or_shot_ref,omitempty"`
	Reason               string                  `json:"reason,omitempty"`
}
type ReadinessDecision struct {
	CandidateID string                       `json:"candidate_id"`
	Action      DecisionAction               `json:"action"`
	ClientRef   string                       `json:"client_ref,omitempty"`
	Item        shootplanning.ReadinessWrite `json:"item,omitempty"`
	Reason      string                       `json:"reason,omitempty"`
}
type LinkDecision struct {
	CandidateID            string         `json:"candidate_id"`
	Action                 DecisionAction `json:"action"`
	ShotClientOrIDRef      string         `json:"shot_client_or_id_ref"`
	ReadinessClientOrIDRef string         `json:"readiness_client_or_id_ref"`
	Reason                 string         `json:"reason,omitempty"`
}
type ReferenceLinkDecision struct {
	CandidateID         string         `json:"candidate_id"`
	Action              DecisionAction `json:"action"`
	RawURL              string         `json:"raw_url"`
	Label               *string        `json:"label,omitempty"`
	TargetKind          string         `json:"target_kind"`
	TargetClientOrIDRef *string        `json:"target_client_or_id_ref,omitempty"`
	Reason              string         `json:"reason,omitempty"`
}
type AssetBindingDecision struct {
	CandidateID         string `json:"candidate_id"`
	AssetID             string `json:"asset_id"`
	Generation          int    `json:"generation"`
	TargetKind          string `json:"target_kind"`
	TargetClientOrIDRef string `json:"target_client_or_id_ref"`
	Purpose             string `json:"purpose"`
}

type IngestionCommitCanonicalV1 struct {
	SessionID               string                  `json:"session_id"`
	ExpectedSessionRevision int64                   `json:"expected_session_revision"`
	PlanID                  string                  `json:"plan_id"`
	ExpectedPlanRevision    int64                   `json:"expected_plan_revision"`
	ShotDecisions           []ShotDecision          `json:"shot_decisions"`
	ReadinessDecisions      []ReadinessDecision     `json:"readiness_decisions"`
	LinkDecisions           []LinkDecision          `json:"link_decisions"`
	ReferenceLinkDecisions  []ReferenceLinkDecision `json:"reference_link_decisions"`
	AssetBindings           []AssetBindingDecision  `json:"asset_bindings"`
}
type PreparedCommit struct {
	Input      IngestionCommitCanonicalV1
	Core       *shootplanning.PreparedPlanBatch
	References []PreparedReferenceLink
	Canonical  []byte
}
type PreparedReferenceLink struct {
	CandidateID         string
	RawURL              string
	URLDigest           string
	Label               *string
	SourceHint          *string
	TargetKind          string
	TargetClientOrIDRef string
}
type PlanReferenceLink struct {
	ID                string     `json:"id"`
	PlanID            string     `json:"plan_id"`
	TargetKind        string     `json:"target_kind"`
	TargetID          string     `json:"target_id"`
	URL               string     `json:"url"`
	URLDigest         string     `json:"url_digest"`
	Label             *string    `json:"label,omitempty"`
	SourceHint        *string    `json:"source_hint,omitempty"`
	SourceSessionID   string     `json:"source_session_id"`
	SourceCandidateID string     `json:"source_candidate_id"`
	Revision          int64      `json:"revision"`
	RemovedAt         *time.Time `json:"removed_at,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
}

func PrepareIngestionCommit(input IngestionCommitCanonicalV1) (PreparedCommit, error) {
	input.SessionID, input.PlanID = strings.TrimSpace(input.SessionID), strings.TrimSpace(input.PlanID)
	if input.SessionID == "" || input.PlanID == "" || input.ExpectedSessionRevision < 1 || input.ExpectedPlanRevision < 1 {
		return PreparedCommit{}, ErrCommitInvalid
	}
	seen := map[string]string{}
	coreCandidates := []shootplanning.PlanBatchCandidate{}
	refs := []PreparedReferenceLink{}
	for _, decision := range input.ShotDecisions {
		if err := validateDecisionID(seen, decision.CandidateID, "shot"); err != nil {
			return PreparedCommit{}, err
		}
		if decision.Action == DecisionKeep {
			ref := decision.ClientRef
			if ref == "" {
				ref = "candidate_" + decision.CandidateID
			}
			coreCandidates = append(coreCandidates, shootplanning.CreateShotBatchCandidate{ClientRef: ref, Shot: decision.Shot, PositionAfterShotRef: decision.PositionAfterShotRef})
		} else if decision.Action != DecisionDiscard {
			return PreparedCommit{}, ErrCommitInvalid
		}
	}
	for _, decision := range input.ReadinessDecisions {
		if err := validateDecisionID(seen, decision.CandidateID, "readiness"); err != nil {
			return PreparedCommit{}, err
		}
		if decision.Action == DecisionKeep {
			ref := decision.ClientRef
			if ref == "" {
				ref = "candidate_" + decision.CandidateID
			}
			coreCandidates = append(coreCandidates, shootplanning.CreateReadinessBatchCandidate{ClientRef: ref, Item: decision.Item})
		} else if decision.Action != DecisionDiscard {
			return PreparedCommit{}, ErrCommitInvalid
		}
	}
	for _, decision := range input.LinkDecisions {
		if err := validateDecisionID(seen, decision.CandidateID, "readiness_link"); err != nil {
			return PreparedCommit{}, err
		}
		if decision.Action == DecisionKeep {
			coreCandidates = append(coreCandidates, shootplanning.LinkReadinessBatchCandidate{ShotRef: decision.ShotClientOrIDRef, ReadinessRef: decision.ReadinessClientOrIDRef})
		} else if decision.Action != DecisionDiscard {
			return PreparedCommit{}, ErrCommitInvalid
		}
	}
	for _, decision := range input.ReferenceLinkDecisions {
		if err := validateDecisionID(seen, decision.CandidateID, "reference_link"); err != nil {
			return PreparedCommit{}, err
		}
		if decision.Action == DecisionKeep {
			if err := ValidateReferenceURL(decision.RawURL); err != nil {
				return PreparedCommit{}, err
			}
			if decision.TargetKind != "plan" && decision.TargetKind != "shot" {
				return PreparedCommit{}, ErrReferenceTarget
			}
			target := ""
			if decision.TargetClientOrIDRef != nil {
				target = strings.TrimSpace(*decision.TargetClientOrIDRef)
			}
			if decision.TargetKind == "shot" && target == "" {
				return PreparedCommit{}, ErrReferenceTarget
			}
			refs = append(refs, PreparedReferenceLink{CandidateID: decision.CandidateID, RawURL: decision.RawURL, URLDigest: digestURL(decision.RawURL), Label: decision.Label, TargetKind: decision.TargetKind, TargetClientOrIDRef: target})
		} else if decision.Action != DecisionDiscard {
			return PreparedCommit{}, ErrCommitInvalid
		}
	}
	if len(coreCandidates) == 0 && len(refs) == 0 && len(input.AssetBindings) == 0 {
		return PreparedCommit{}, ErrNoKeptCandidates
	}
	var core *shootplanning.PreparedPlanBatch
	var err error
	if len(coreCandidates) > 0 {
		prepared := shootplanning.PlanBatchInput{PlanID: input.PlanID, ExpectedPlanRevision: input.ExpectedPlanRevision, Candidates: coreCandidates}
		value, prepareErr := shootplanning.PreparePlanBatch(prepared)
		if prepareErr != nil {
			return PreparedCommit{}, prepareErr
		}
		core = &value
	}
	canonical, err := canonicalCommit(input)
	if err != nil {
		return PreparedCommit{}, err
	}
	return PreparedCommit{Input: input, Core: core, References: refs, Canonical: canonical}, nil
}

type ReferenceTarget struct {
	CandidateID string
	TargetKind  string
	TargetID    string
}
type PlanTargetProof struct {
	planID       string
	targetKind   string
	targetID     string
	planRevision int64
	operation    string
}

func (p PlanTargetProof) PlanID() string     { return p.planID }
func (p PlanTargetProof) TargetKind() string { return p.targetKind }
func (p PlanTargetProof) TargetID() string   { return p.targetID }
func (p PlanTargetProof) Operation() string  { return p.operation }

func AuthorizeReferenceLinkTargetsInScope(ctx context.Context, tx store.TxAccountScope, planID string, expectedPlanRevision int64, targets []ReferenceTarget, operation string) ([]PlanTargetProof, error) {
	if operation != "reference_link_create" {
		return nil, ErrReferenceTarget
	}
	var revision int64
	var status string
	if err := tx.QueryRowForUpdate(ctx, "shoot_plans", "revision,status", "id = $2", planID).Scan(&revision, &status); errors.Is(err, store.ErrNoRows) {
		return nil, ErrPlanNotFound
	} else if err != nil {
		return nil, err
	}
	if revision != expectedPlanRevision {
		return nil, ErrPlanRevision
	}
	if status == "archived" {
		return nil, ErrSessionTerminal
	}
	result := make([]PlanTargetProof, 0, len(targets))
	for _, target := range targets {
		if target.TargetKind != "plan" && target.TargetKind != "shot" {
			return nil, ErrReferenceTarget
		}
		if target.TargetKind == "plan" && target.TargetID != planID {
			return nil, ErrReferenceTarget
		}
		if target.TargetKind == "shot" {
			var id string
			err := tx.QueryRow(ctx, "shoot_plan_shots", "id", "id = $2 AND plan_id = $3 AND removed_at IS NULL", target.TargetID, planID).Scan(&id)
			if errors.Is(err, store.ErrNoRows) {
				return nil, ErrReferenceTarget
			}
			if err != nil {
				return nil, err
			}
		}
		result = append(result, PlanTargetProof{planID: planID, targetKind: target.TargetKind, targetID: target.TargetID, planRevision: revision, operation: operation})
	}
	return result, nil
}

func CommitReferenceLinksInScope(ctx context.Context, tx store.TxAccountScope, planID, sessionID string, links []PreparedReferenceLink, proofs []PlanTargetProof) ([]PlanReferenceLink, error) {
	if len(links) != len(proofs) {
		return nil, ErrReferenceTarget
	}
	byCandidate := map[string]PlanTargetProof{}
	for i, proof := range proofs {
		byCandidate[links[i].CandidateID] = proof
	}
	result := make([]PlanReferenceLink, 0, len(links))
	now := time.Now().UTC()
	for _, link := range links {
		proof, ok := byCandidate[link.CandidateID]
		if !ok || proof.planID != planID || proof.operation != "reference_link_create" {
			return nil, ErrReferenceTarget
		}
		id := "prl_" + uuid.NewString()
		if _, err := tx.InsertReturningID(ctx, "shoot_plan_reference_links", []string{"id", "plan_id", "target_kind", "target_id", "url", "url_digest", "label", "source_hint", "source_session_id", "source_candidate_id", "revision", "created_at"}, id, planID, proof.targetKind, proof.targetID, link.RawURL, link.URLDigest, link.Label, link.SourceHint, sessionID, link.CandidateID, int64(1), now); err != nil {
			return nil, err
		}
		result = append(result, PlanReferenceLink{ID: id, PlanID: planID, TargetKind: proof.targetKind, TargetID: proof.targetID, URL: link.RawURL, URLDigest: link.URLDigest, Label: link.Label, SourceHint: link.SourceHint, SourceSessionID: sessionID, SourceCandidateID: link.CandidateID, Revision: 1, CreatedAt: now})
	}
	return result, nil
}

func ValidateReferenceURL(raw string) error {
	if len([]byte(raw)) < 1 || len([]byte(raw)) > 2048 {
		return ErrReferenceURL
	}
	parsed, err := url.ParseRequestURI(raw)
	if err != nil || parsed.Host == "" || (!strings.EqualFold(parsed.Scheme, "http") && !strings.EqualFold(parsed.Scheme, "https")) {
		return ErrReferenceURL
	}
	return nil
}

func validateCommitAgainstSession(input IngestionCommitCanonicalV1, session Session) error {
	content := make(map[string]ContentCandidate, len(session.CandidateSnapshot.ContentCandidates))
	for _, candidate := range session.CandidateSnapshot.ContentCandidates {
		content[candidate.CandidateID] = candidate
	}
	references := make(map[string]ReferenceLinkCandidate, len(session.CandidateSnapshot.ReferenceLinkCandidates))
	for _, candidate := range session.CandidateSnapshot.ReferenceLinkCandidates {
		references[candidate.CandidateID] = candidate
	}
	assets := make(map[string]AssetBindingDecision, len(session.CandidateSnapshot.AssetBindingCandidates))
	for _, candidate := range session.CandidateSnapshot.AssetBindingCandidates {
		assets[candidate.CandidateID] = candidate
	}
	keptShots := make(map[string]struct{})
	keptReadiness := make(map[string]struct{})
	seenContent := make(map[string]string, len(content))
	for _, decision := range input.ShotDecisions {
		candidate, ok := content[decision.CandidateID]
		if !ok {
			return fmt.Errorf("%w: shot candidate %s is not current", ErrCommitInvalid, decision.CandidateID)
		}
		if candidate.Kind != "shot" {
			return fmt.Errorf("%w: candidate %s is not a shot", ErrCommitInvalid, decision.CandidateID)
		}
		if err := validateDecisionID(seenContent, decision.CandidateID, "shot"); err != nil {
			return err
		}
		if candidate.Action == "needs_confirmation" || candidate.SourceStatus == "source_changed" || candidate.SourceStatus == "source_missing" {
			return fmt.Errorf("%w: shot candidate %s needs source confirmation", ErrCommitInvalid, decision.CandidateID)
		}
		if !decisionMatchesCandidate(decision.Action, candidate.Action) {
			return fmt.Errorf("%w: shot candidate %s action does not match preview", ErrCommitInvalid, decision.CandidateID)
		}
		if decision.Action == DecisionKeep {
			ref := decision.ClientRef
			if ref == "" {
				ref = "candidate_" + decision.CandidateID
			}
			keptShots[ref] = struct{}{}
		}
	}
	for _, decision := range input.ReadinessDecisions {
		candidate, ok := content[decision.CandidateID]
		if !ok {
			return fmt.Errorf("%w: readiness candidate %s is not current", ErrCommitInvalid, decision.CandidateID)
		}
		if candidate.Kind != "readiness" {
			return fmt.Errorf("%w: candidate %s is not readiness", ErrCommitInvalid, decision.CandidateID)
		}
		if err := validateDecisionID(seenContent, decision.CandidateID, "readiness"); err != nil {
			return err
		}
		if candidate.Action == "needs_confirmation" || candidate.SourceStatus == "source_changed" || candidate.SourceStatus == "source_missing" {
			return fmt.Errorf("%w: readiness candidate %s needs source confirmation", ErrCommitInvalid, decision.CandidateID)
		}
		if !decisionMatchesCandidate(decision.Action, candidate.Action) {
			return fmt.Errorf("%w: readiness candidate %s action does not match preview", ErrCommitInvalid, decision.CandidateID)
		}
		if decision.Action == DecisionKeep {
			ref := decision.ClientRef
			if ref == "" {
				ref = "candidate_" + decision.CandidateID
			}
			keptReadiness[ref] = struct{}{}
		}
	}
	for _, decision := range input.ReferenceLinkDecisions {
		candidate, ok := references[decision.CandidateID]
		if !ok || candidate.RawURL != decision.RawURL || candidate.TargetKind != decision.TargetKind || !stringPtrEqual(candidate.Label, decision.Label) || !stringPtrEqual(candidate.TargetClientOrIDRef, decision.TargetClientOrIDRef) {
			return fmt.Errorf("%w: reference candidate %s is not current", ErrCommitInvalid, decision.CandidateID)
		}
		if err := validateDecisionID(seenContent, decision.CandidateID, "reference_link"); err != nil {
			return err
		}
		if candidate.Action == "needs_confirmation" || candidate.SourceStatus == "source_changed" || candidate.SourceStatus == "source_missing" {
			return fmt.Errorf("%w: reference candidate %s needs source confirmation", ErrCommitInvalid, decision.CandidateID)
		}
		if !decisionMatchesCandidate(decision.Action, candidate.Action) {
			return fmt.Errorf("%w: reference candidate %s action does not match preview", ErrCommitInvalid, decision.CandidateID)
		}
	}
	for _, decision := range input.LinkDecisions {
		var candidate *ReadinessLinkCandidate
		for i := range session.CandidateSnapshot.ReadinessLinkCandidates {
			if session.CandidateSnapshot.ReadinessLinkCandidates[i].CandidateID == decision.CandidateID {
				candidate = &session.CandidateSnapshot.ReadinessLinkCandidates[i]
				break
			}
		}
		if candidate == nil || candidate.ReadinessClientOrIDRef != decision.ReadinessClientOrIDRef || candidate.ShotClientOrIDRef != decision.ShotClientOrIDRef {
			return fmt.Errorf("%w: readiness link candidate %s is not current", ErrCommitInvalid, decision.CandidateID)
		}
		if err := validateDecisionID(seenContent, decision.CandidateID, "readiness_link"); err != nil {
			return err
		}
		if candidate.Action == "needs_confirmation" || candidate.SourceStatus == "source_changed" || candidate.SourceStatus == "source_missing" {
			return fmt.Errorf("%w: readiness link candidate %s needs source confirmation", ErrCommitInvalid, decision.CandidateID)
		}
		if !decisionMatchesCandidate(decision.Action, candidate.Action) {
			return fmt.Errorf("%w: readiness link candidate %s action does not match preview", ErrCommitInvalid, decision.CandidateID)
		}
		shotRef := strings.TrimSpace(decision.ShotClientOrIDRef)
		readinessRef := strings.TrimSpace(decision.ReadinessClientOrIDRef)
		// A link may point at a candidate created by this batch (client ref) or
		// at an existing same-plan resource (server id).  The core batch
		// participant performs the scoped existence/plan check for server ids.
		_, shotOK := keptShots[shotRef]
		_, readinessOK := keptReadiness[readinessRef]
		shotOK = shotOK || strings.HasPrefix(shotRef, "shot_")
		readinessOK = readinessOK || strings.HasPrefix(readinessRef, "ready_")
		if !shotOK || !readinessOK {
			return fmt.Errorf("%w: readiness link target is not a current session candidate", ErrCommitInvalid)
		}
	}
	for _, decision := range input.AssetBindings {
		candidate, ok := assets[decision.CandidateID]
		if !ok || !sameAssetBindingDecision(candidate, decision) {
			return fmt.Errorf("%w: asset binding candidate %s is not current", ErrCommitInvalid, decision.CandidateID)
		}
		if err := validateDecisionID(seenContent, decision.CandidateID, "asset_binding"); err != nil {
			return err
		}
	}
	for id := range content {
		if _, ok := seenContent[id]; !ok {
			return fmt.Errorf("%w: missing content decision %s", ErrCommitInvalid, id)
		}
	}
	for id := range references {
		if _, ok := seenContent[id]; !ok {
			return fmt.Errorf("%w: missing reference decision %s", ErrCommitInvalid, id)
		}
	}
	for _, candidate := range session.CandidateSnapshot.ReadinessLinkCandidates {
		if _, ok := seenContent[candidate.CandidateID]; !ok {
			return fmt.Errorf("%w: missing readiness link decision %s", ErrCommitInvalid, candidate.CandidateID)
		}
	}
	for id := range assets {
		if _, ok := seenContent[id]; !ok {
			return fmt.Errorf("%w: missing asset binding decision %s", ErrCommitInvalid, id)
		}
	}
	return nil
}

func sameAssetBindingDecision(a, b AssetBindingDecision) bool {
	return a.AssetID == b.AssetID && a.Generation == b.Generation && a.TargetKind == b.TargetKind &&
		a.TargetClientOrIDRef == b.TargetClientOrIDRef && a.Purpose == b.Purpose
}

func decisionMatchesCandidate(decision DecisionAction, candidate string) bool {
	if candidate == "discard" {
		return decision == DecisionDiscard
	}
	return decision == DecisionKeep
}

func stringPtrEqual(a, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func validateDecisionID(seen map[string]string, id, kind string) error {
	if strings.TrimSpace(id) == "" {
		return ErrCommitInvalid
	}
	if prior, ok := seen[id]; ok {
		return fmt.Errorf("%w: candidate %s reused by %s and %s", ErrCommitInvalid, id, prior, kind)
	}
	seen[id] = kind
	return nil
}
func canonicalCommit(input IngestionCommitCanonicalV1) ([]byte, error) {
	sort.SliceStable(input.ShotDecisions, func(i, j int) bool { return input.ShotDecisions[i].CandidateID < input.ShotDecisions[j].CandidateID })
	sort.SliceStable(input.ReadinessDecisions, func(i, j int) bool {
		return input.ReadinessDecisions[i].CandidateID < input.ReadinessDecisions[j].CandidateID
	})
	sort.SliceStable(input.LinkDecisions, func(i, j int) bool { return input.LinkDecisions[i].CandidateID < input.LinkDecisions[j].CandidateID })
	sort.SliceStable(input.ReferenceLinkDecisions, func(i, j int) bool {
		return input.ReferenceLinkDecisions[i].CandidateID < input.ReferenceLinkDecisions[j].CandidateID
	})
	return jsonMarshal(input)
}
func jsonMarshal(value any) ([]byte, error) { return json.Marshal(value) }
