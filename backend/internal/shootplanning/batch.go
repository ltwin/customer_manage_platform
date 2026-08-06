package shootplanning

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/samson/customer-manage-platform/backend/internal/platform/idempotency"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

const maxPlanBatchCandidates = 200

type PlanBatchCandidate interface {
	planBatchCandidate()
}

type CreateShotBatchCandidate struct {
	ClientRef            string
	Shot                 ShotWrite
	PositionAfterShotRef *string
}

type CreateReadinessBatchCandidate struct {
	ClientRef string
	Item      ReadinessWrite
}

type LinkReadinessBatchCandidate struct {
	ShotRef      string
	ReadinessRef string
}

func (CreateShotBatchCandidate) planBatchCandidate()      {}
func (CreateReadinessBatchCandidate) planBatchCandidate() {}
func (LinkReadinessBatchCandidate) planBatchCandidate()   {}

type PlanBatchInput struct {
	PlanID               string
	ExpectedPlanRevision int64
	Candidates           []PlanBatchCandidate
}

type PreparedPlanBatch struct {
	input     PlanBatchInput
	canonical []byte
}

type CreatedPlanBatchID struct {
	ClientRef    string `json:"client_ref"`
	ResourceKind string `json:"resource_kind"`
	ServerID     string `json:"server_id"`
}

type PlanBatchLink struct {
	ShotID      string `json:"shot_id"`
	ReadinessID string `json:"readiness_id"`
}

type PlanBatchResult struct {
	PlanID           string               `json:"plan_id"`
	Revision         int64                `json:"revision"`
	Status           PlanStatus           `json:"status"`
	CreatedIDs       []CreatedPlanBatchID `json:"created_ids"`
	CurrentShots     []Shot               `json:"current_shots"`
	CurrentReadiness []ReadinessItem      `json:"current_readiness"`
	CurrentLinks     []PlanBatchLink      `json:"current_links"`
}

func PreparePlanBatch(input PlanBatchInput) (PreparedPlanBatch, error) {
	input.PlanID = strings.TrimSpace(input.PlanID)
	if input.PlanID == "" || input.ExpectedPlanRevision < 1 || len(input.Candidates) < 1 || len(input.Candidates) > maxPlanBatchCandidates {
		return PreparedPlanBatch{}, errors.New("plan batch invalid")
	}
	clientRefs := make(map[string]string)
	canonicalCandidates := make([]map[string]any, 0, len(input.Candidates))
	normalizedCandidates := make([]PlanBatchCandidate, 0, len(input.Candidates))
	for _, candidate := range input.Candidates {
		switch candidate := candidate.(type) {
		case CreateShotBatchCandidate:
			candidate.Shot = cloneShotWrite(candidate.Shot)
			clientRef, err := validateBatchClientRef(candidate.ClientRef, "shot", clientRefs)
			if err != nil {
				return PreparedPlanBatch{}, err
			}
			if candidate.Shot.Title == nil {
				return PreparedPlanBatch{}, errors.New("batch shot title required")
			}
			title := strings.TrimSpace(*candidate.Shot.Title)
			if runeLen(title) < 1 || runeLen(title) > 160 {
				return PreparedPlanBatch{}, errors.New("batch shot title invalid")
			}
			if _, err := normalizeShotWrite(normalizedShotWrite{}, candidate.Shot); err != nil {
				return PreparedPlanBatch{}, err
			}
			candidate.ClientRef = clientRef
			candidate.Shot.Title = &title
			entry := map[string]any{"kind": "create_shot", "client_ref": clientRef, "shot": canonicalShotWrite(candidate.Shot)}
			if candidate.PositionAfterShotRef != nil {
				positionRef := strings.TrimSpace(*candidate.PositionAfterShotRef)
				if positionRef == "" {
					return PreparedPlanBatch{}, errors.New("batch shot position ref invalid")
				}
				candidate.PositionAfterShotRef = &positionRef
				entry["position_after_client_or_shot_ref"] = positionRef
			}
			canonicalCandidates = append(canonicalCandidates, entry)
			normalizedCandidates = append(normalizedCandidates, candidate)
		case CreateReadinessBatchCandidate:
			candidate.Item = cloneReadinessWrite(candidate.Item)
			clientRef, err := validateBatchClientRef(candidate.ClientRef, "readiness", clientRefs)
			if err != nil {
				return PreparedPlanBatch{}, err
			}
			if _, err := normalizeReadinessWrite(candidate.Item, true); err != nil {
				return PreparedPlanBatch{}, err
			}
			canonicalCandidates = append(canonicalCandidates, map[string]any{
				"kind": "create_readiness", "client_ref": clientRef, "item": canonicalReadinessWrite(candidate.Item),
			})
			candidate.ClientRef = clientRef
			normalizedCandidates = append(normalizedCandidates, candidate)
		case LinkReadinessBatchCandidate:
			shotRef := strings.TrimSpace(candidate.ShotRef)
			readinessRef := strings.TrimSpace(candidate.ReadinessRef)
			if shotRef == "" || readinessRef == "" {
				return PreparedPlanBatch{}, errors.New("batch link ref invalid")
			}
			canonicalCandidates = append(canonicalCandidates, map[string]any{
				"kind": "link_readiness", "shot_client_or_id_ref": shotRef, "readiness_client_or_id_ref": readinessRef,
			})
			candidate.ShotRef = shotRef
			candidate.ReadinessRef = readinessRef
			normalizedCandidates = append(normalizedCandidates, candidate)
		default:
			return PreparedPlanBatch{}, errors.New("unknown plan batch candidate")
		}
	}
	canonical, err := json.Marshal(map[string]any{
		"plan_id": input.PlanID, "expected_plan_revision": input.ExpectedPlanRevision, "candidates": canonicalCandidates,
	})
	if err != nil {
		return PreparedPlanBatch{}, err
	}
	input.Candidates = normalizedCandidates
	return PreparedPlanBatch{input: input, canonical: canonical}, nil
}

func cloneShotWrite(input ShotWrite) ShotWrite {
	result := input
	if input.Title != nil {
		title := *input.Title
		result.Title = &title
	}
	return result
}

func cloneReadinessWrite(input ReadinessWrite) ReadinessWrite {
	result := input
	result.Category = cloneStringPointer(input.Category)
	result.Title = cloneStringPointer(input.Title)
	result.Requirement = cloneStringPointer(input.Requirement)
	result.PreflightStatus = cloneStringPointer(input.PreflightStatus)
	result.ResponsibilityHint = cloneStringPointer(input.ResponsibilityHint)
	return result
}

func cloneStringPointer(input *string) *string {
	if input == nil {
		return nil
	}
	value := *input
	return &value
}

func validateBatchClientRef(raw, kind string, seen map[string]string) (string, error) {
	ref := strings.TrimSpace(raw)
	if runeLen(ref) < 1 || runeLen(ref) > 120 {
		return "", errors.New("batch client ref invalid")
	}
	if previous, exists := seen[ref]; exists {
		return "", fmt.Errorf("batch client ref %q reused by %s and %s", ref, previous, kind)
	}
	seen[ref] = kind
	return ref, nil
}

func (a *Application) CommitPlanBatch(
	ctx context.Context,
	scope store.AccountScope,
	key string,
	input PlanBatchInput,
) (PlanBatchResult, error) {
	prepared, err := PreparePlanBatch(input)
	if err != nil {
		return PlanBatchResult{}, err
	}
	response, err := a.idempotency.Execute(ctx, scope, idempotency.Request{
		Operation: idempotency.OperationShootPlanBatch, Key: key,
		ResourceIdentity: idempotency.BatchResource(prepared.input.PlanID), CanonicalBody: prepared.canonical,
	}, func(tx store.TxAccountScope) (idempotency.StoredResponse, error) {
		result, err := a.CommitPreparedPlanBatchInScope(ctx, tx, prepared)
		if err != nil {
			return idempotency.StoredResponse{}, err
		}
		body, err := json.Marshal(result)
		return idempotency.StoredResponse{Status: 200, Body: body}, err
	})
	if err != nil {
		return PlanBatchResult{}, err
	}
	var result PlanBatchResult
	if err := json.Unmarshal(response.Body, &result); err != nil {
		return PlanBatchResult{}, fmt.Errorf("decode plan batch replay: %w", err)
	}
	return result, nil
}

func (a *Application) CommitPreparedPlanBatchInScope(
	ctx context.Context,
	tx store.TxAccountScope,
	prepared PreparedPlanBatch,
) (PlanBatchResult, error) {
	if len(prepared.canonical) == 0 {
		return PlanBatchResult{}, errors.New("prepared plan batch invalid")
	}
	input := prepared.input
	plan, err := a.repo.LockPlan(ctx, tx, input.PlanID)
	if err != nil {
		return PlanBatchResult{}, err
	}
	if plan.Revision != input.ExpectedPlanRevision {
		return PlanBatchResult{}, ErrPlanRevisionConflict
	}
	if plan.Status == PlanStatusCompleted {
		return PlanBatchResult{}, ErrReopenRequired
	}
	if plan.Status == PlanStatusArchived {
		return PlanBatchResult{}, ErrArchivedReadOnly
	}

	resolved := make(map[string]string)
	created := make([]CreatedPlanBatchID, 0)
	shotPositions := make([]struct {
		shotID   string
		afterRef *string
	}, 0)
	for _, candidate := range input.Candidates {
		switch candidate := candidate.(type) {
		case CreateShotBatchCandidate:
			id, err := applyUpsertShot(ctx, tx, input.PlanID, UpsertShotCommand{Shot: candidate.Shot})
			if err != nil {
				return PlanBatchResult{}, err
			}
			resolved[strings.TrimSpace(candidate.ClientRef)] = id
			created = append(created, CreatedPlanBatchID{ClientRef: strings.TrimSpace(candidate.ClientRef), ResourceKind: "shot", ServerID: id})
			shotPositions = append(shotPositions, struct {
				shotID   string
				afterRef *string
			}{shotID: id, afterRef: candidate.PositionAfterShotRef})
		case CreateReadinessBatchCandidate:
			id, err := applyUpsertReadiness(ctx, tx, input.PlanID, UpsertReadinessCommand{Item: candidate.Item})
			if err != nil {
				return PlanBatchResult{}, err
			}
			resolved[strings.TrimSpace(candidate.ClientRef)] = id
			created = append(created, CreatedPlanBatchID{ClientRef: strings.TrimSpace(candidate.ClientRef), ResourceKind: "readiness", ServerID: id})
		}
	}
	if err := applyBatchShotPositions(ctx, tx, input.PlanID, shotPositions, resolved); err != nil {
		return PlanBatchResult{}, err
	}
	for _, candidate := range input.Candidates {
		link, ok := candidate.(LinkReadinessBatchCandidate)
		if !ok {
			continue
		}
		shotID := resolveBatchRef(strings.TrimSpace(link.ShotRef), resolved)
		readinessID := resolveBatchRef(strings.TrimSpace(link.ReadinessRef), resolved)
		if _, err := applyLinkReadiness(ctx, tx, input.PlanID, shotID, readinessID); err != nil {
			return PlanBatchResult{}, err
		}
	}
	status := plan.Status
	if status == PlanStatusReady {
		incomplete, err := requiredReadinessIncomplete(ctx, tx, input.PlanID)
		if err != nil {
			return PlanBatchResult{}, err
		}
		status = StatusAfterStructuralMutation(status, !incomplete)
	}
	updated, err := tx.Update(ctx, "shoot_plans",
		"revision = revision + 1, status = $2, updated_at = clock_timestamp()",
		"id = $3 AND revision = $4", string(status), input.PlanID, input.ExpectedPlanRevision)
	if err != nil {
		return PlanBatchResult{}, err
	}
	if updated != 1 {
		return PlanBatchResult{}, ErrPlanRevisionConflict
	}
	shots, readiness, links, err := loadPlanBatchProjection(ctx, tx, input.PlanID)
	if err != nil {
		return PlanBatchResult{}, err
	}
	return PlanBatchResult{
		PlanID: input.PlanID, Revision: input.ExpectedPlanRevision + 1, Status: status,
		CreatedIDs: created, CurrentShots: shots, CurrentReadiness: readiness, CurrentLinks: links,
	}, nil
}

func applyBatchShotPositions(
	ctx context.Context,
	tx store.TxAccountScope,
	planID string,
	positions []struct {
		shotID   string
		afterRef *string
	},
	resolved map[string]string,
) error {
	ordered, err := currentShotIDs(ctx, tx, planID)
	if err != nil {
		return err
	}
	for _, position := range positions {
		if position.afterRef == nil {
			continue
		}
		afterID := resolveBatchRef(strings.TrimSpace(*position.afterRef), resolved)
		ordered = removeString(ordered, position.shotID)
		index := indexOfString(ordered, afterID)
		if index < 0 {
			return ErrShotNotFound
		}
		ordered = append(ordered, "")
		copy(ordered[index+2:], ordered[index+1:])
		ordered[index+1] = position.shotID
	}
	return applyReorderShots(ctx, tx, planID, ordered)
}

func resolveBatchRef(ref string, resolved map[string]string) string {
	if id, exists := resolved[ref]; exists {
		return id
	}
	return ref
}

func removeString(values []string, target string) []string {
	for index, value := range values {
		if value == target {
			return append(values[:index], values[index+1:]...)
		}
	}
	return values
}

func indexOfString(values []string, target string) int {
	for index, value := range values {
		if value == target {
			return index
		}
	}
	return -1
}

func loadPlanBatchProjection(ctx context.Context, tx store.TxAccountScope, planID string) ([]Shot, []ReadinessItem, []PlanBatchLink, error) {
	shots, err := loadShots(ctx, tx, planID)
	if err != nil {
		return nil, nil, nil, err
	}
	readiness, err := loadReadiness(ctx, tx, planID)
	if err != nil {
		return nil, nil, nil, err
	}
	rows, err := tx.Query(ctx, "shoot_plan_shot_readiness_links", "shot_id, readiness_item_id", "plan_id = $2", planID)
	if err != nil {
		return nil, nil, nil, err
	}
	links := make([]PlanBatchLink, 0)
	for rows.Next() {
		var link PlanBatchLink
		if err := rows.Scan(&link.ShotID, &link.ReadinessID); err != nil {
			rows.Close()
			return nil, nil, nil, err
		}
		links = append(links, link)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, nil, nil, err
	}
	rows.Close()
	sort.Slice(links, func(i, j int) bool {
		if links[i].ShotID == links[j].ShotID {
			return links[i].ReadinessID < links[j].ReadinessID
		}
		return links[i].ShotID < links[j].ShotID
	})
	return shots, readiness, links, nil
}
