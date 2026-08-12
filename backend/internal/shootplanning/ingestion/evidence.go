package ingestion

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
	"github.com/samson/customer-manage-platform/backend/internal/shootplanning"
)

const (
	Stage1EvidenceGateVersion = "planning-evidence-stage1-v1"
	stage1RequiredSampleCount = 5
	stage1LivePlanThreshold   = 3
	stage1MedianThreshold     = 600
)

type Stage1SampleDisposition string

const (
	Stage1SampleInclude Stage1SampleDisposition = "include"
	Stage1SampleExclude Stage1SampleDisposition = "exclude"
)

// Stage1SampleDecision is an owner-reviewed cohort decision.  The projector
// verifies and hashes these facts, but it never creates or changes the
// stage-1-evidence-go approval decision.
type Stage1SampleDecision struct {
	PlanID          string                  `json:"plan_id"`
	Disposition     Stage1SampleDisposition `json:"disposition"`
	ExclusionReason string                  `json:"exclusion_reason,omitempty"`
	OwnerReviewRef  string                  `json:"owner_review_ref"`
}

type Stage1EvidenceInput struct {
	WindowID        string
	BuildRevision   string
	SampleDecisions []Stage1SampleDecision
	RunSessions     []shootplanning.RunModeSession
	Observations    []PlanBuildObservation
}

type Stage1EvidenceRuleVersions struct {
	EvidenceProjector string `json:"evidence_projector"`
	ActiveTime        string `json:"active_time"`
	RunCaptureMode    string `json:"run_capture_mode"`
	IdleRules         []int  `json:"idle_rules"`
}

type Stage1G1Evidence struct {
	DistinctLivePlanCount int      `json:"distinct_live_plan_count"`
	EligiblePlanCount     int      `json:"eligible_plan_count"`
	RequiredSampleCount   int      `json:"required_sample_count"`
	Threshold             int      `json:"threshold"`
	LivePlanIDs           []string `json:"live_plan_ids"`
	ThresholdMet          bool     `json:"threshold_met"`
}

type Stage1ActiveSample struct {
	PlanID        string `json:"plan_id"`
	ActiveSeconds int64  `json:"active_seconds"`
}

type Stage1G2Evidence struct {
	SuccessfulPlanCount    int                  `json:"successful_plan_count"`
	RequiredSampleCount    int                  `json:"required_sample_count"`
	ActiveSecondsSamples   []Stage1ActiveSample `json:"active_seconds_samples"`
	MedianActiveSeconds    *float64             `json:"median_active_seconds"`
	MedianThresholdSeconds int                  `json:"median_threshold_seconds"`
	MedianThresholdMet     bool                 `json:"median_threshold_met"`
	AbandonedPlanCount     int                  `json:"abandoned_plan_count"`
	TerminalPlanCount      int                  `json:"terminal_plan_count"`
	AbandonRatio           *float64             `json:"abandon_ratio"`
}

type Stage1Exclusion struct {
	Reason  string   `json:"reason"`
	Count   int      `json:"count"`
	PlanIDs []string `json:"plan_ids"`
}

type Stage1EvidenceReport struct {
	GateVersion      string                     `json:"gate_version"`
	Status           string                     `json:"status"`
	Stale            bool                       `json:"stale"`
	WindowID         string                     `json:"window_id"`
	BuildRevision    string                     `json:"build_revision"`
	SampleDecisions  []Stage1SampleDecision     `json:"sample_decisions"`
	CohortComplete   bool                       `json:"cohort_complete"`
	G1               Stage1G1Evidence           `json:"g1"`
	G2               Stage1G2Evidence           `json:"g2"`
	Exclusions       []Stage1Exclusion          `json:"exclusions"`
	RuleVersions     Stage1EvidenceRuleVersions `json:"rule_versions"`
	ProjectionSHA256 string                     `json:"projection_sha256"`
}

type Stage1EvidenceArtifact struct {
	Report        Stage1EvidenceReport
	CanonicalJSON []byte
	SHA256        string
}

type stage1ProjectionPayload struct {
	GateVersion     string                     `json:"gate_version"`
	Status          string                     `json:"status"`
	Stale           bool                       `json:"stale"`
	WindowID        string                     `json:"window_id"`
	BuildRevision   string                     `json:"build_revision"`
	SampleDecisions []Stage1SampleDecision     `json:"sample_decisions"`
	CohortComplete  bool                       `json:"cohort_complete"`
	G1              Stage1G1Evidence           `json:"g1"`
	G2              Stage1G2Evidence           `json:"g2"`
	Exclusions      []Stage1Exclusion          `json:"exclusions"`
	RuleVersions    Stage1EvidenceRuleVersions `json:"rule_versions"`
}

// GenerateStage1EvidenceFromStore reads only the scoped, already persisted
// facts needed by the projector.  Owner sample decisions remain an explicit
// input so this function cannot silently invent a cohort or approve a gate.
func GenerateStage1EvidenceFromStore(
	ctx context.Context,
	scope store.AccountScope,
	windowID, buildRevision string,
	decisions []Stage1SampleDecision,
) (Stage1EvidenceArtifact, error) {
	planIDs := make([]string, 0, len(decisions))
	seen := make(map[string]struct{}, len(decisions))
	for _, decision := range decisions {
		planID := strings.TrimSpace(decision.PlanID)
		if planID == "" {
			return Stage1EvidenceArtifact{}, fmt.Errorf("stage1 sample plan_id is required")
		}
		if _, ok := seen[planID]; !ok {
			seen[planID] = struct{}{}
			planIDs = append(planIDs, planID)
		}
	}
	if len(planIDs) == 0 {
		return ProjectStage1Evidence(Stage1EvidenceInput{WindowID: windowID, BuildRevision: buildRevision, SampleDecisions: decisions})
	}

	runSessions := make([]shootplanning.RunModeSession, 0)
	rows, err := scope.Query(ctx, "shoot_plan_run_sessions", "id,plan_id,execution_window_revision,opened_at,last_active_at,closed_at,capture_mode", "plan_id = ANY($2)", planIDs)
	if err != nil {
		return Stage1EvidenceArtifact{}, fmt.Errorf("load stage1 run sessions: %w", err)
	}
	for rows.Next() {
		var session shootplanning.RunModeSession
		if err := rows.Scan(&session.ID, &session.PlanID, &session.ExecutionWindowRevision, &session.OpenedAt, &session.LastActiveAt, &session.ClosedAt, &session.CaptureMode); err != nil {
			rows.Close()
			return Stage1EvidenceArtifact{}, fmt.Errorf("scan stage1 run session: %w", err)
		}
		runSessions = append(runSessions, session)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return Stage1EvidenceArtifact{}, fmt.Errorf("iterate stage1 run sessions: %w", err)
	}
	rows.Close()

	observations := make([]PlanBuildObservation, 0, len(planIDs))
	rows, err = scope.Query(ctx, "shoot_plan_build_observations", observationColumns, "plan_id = ANY($2)", planIDs)
	if err != nil {
		return Stage1EvidenceArtifact{}, fmt.Errorf("load stage1 observations: %w", err)
	}
	for rows.Next() {
		var observation PlanBuildObservation
		if err := rows.Scan(&observation.PlanID, &observation.FirstIngestionAt, &observation.FirstReadyAt, &observation.LastActivityAt, &observation.ActiveSeconds, &observation.IdleRuleVersion, &observation.Outcome, &observation.TerminalAt, &observation.PostTerminalIngestionCount, &observation.Revision); err != nil {
			rows.Close()
			return Stage1EvidenceArtifact{}, fmt.Errorf("scan stage1 observation: %w", err)
		}
		observations = append(observations, observation)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return Stage1EvidenceArtifact{}, fmt.Errorf("iterate stage1 observations: %w", err)
	}
	rows.Close()

	return ProjectStage1Evidence(Stage1EvidenceInput{
		WindowID: windowID, BuildRevision: buildRevision, SampleDecisions: decisions,
		RunSessions: runSessions, Observations: observations,
	})
}

func ProjectStage1Evidence(input Stage1EvidenceInput) (Stage1EvidenceArtifact, error) {
	windowID := strings.TrimSpace(input.WindowID)
	buildRevision := strings.TrimSpace(input.BuildRevision)
	if windowID == "" || buildRevision == "" {
		return Stage1EvidenceArtifact{}, fmt.Errorf("stage1 evidence window/build revision is required")
	}

	decisions, included, exclusions, err := normalizeStage1Decisions(input.SampleDecisions)
	if err != nil {
		return Stage1EvidenceArtifact{}, err
	}
	observations, idleRules, err := indexStage1Observations(input.Observations)
	if err != nil {
		return Stage1EvidenceArtifact{}, err
	}

	livePlanSet := make(map[string]struct{})
	for _, session := range input.RunSessions {
		if _, eligible := included[session.PlanID]; eligible && session.CaptureMode == shootplanning.CaptureModeLive {
			livePlanSet[session.PlanID] = struct{}{}
		}
	}
	livePlanIDs := sortedSet(livePlanSet)

	activeSamples := make([]Stage1ActiveSample, 0, len(included))
	abandonedCount := 0
	terminalCount := 0
	missingTerminalFact := false
	for planID := range included {
		observation, ok := observations[planID]
		if !ok {
			addStage1Exclusion(exclusions, "missing_observation", planID)
			missingTerminalFact = true
			continue
		}
		switch observation.Outcome {
		case ObservationFirstReady:
			terminalCount++
			activeSamples = append(activeSamples, Stage1ActiveSample{PlanID: planID, ActiveSeconds: observation.ActiveSeconds})
		case ObservationAbandoned:
			terminalCount++
			abandonedCount++
			addStage1Exclusion(exclusions, "abandoned_before_ready", planID)
		case ObservationInProgress:
			addStage1Exclusion(exclusions, "observation_in_progress", planID)
			missingTerminalFact = true
		default:
			return Stage1EvidenceArtifact{}, fmt.Errorf("unknown observation outcome for plan %s: %s", planID, observation.Outcome)
		}
	}
	sort.Slice(activeSamples, func(i, j int) bool { return activeSamples[i].PlanID < activeSamples[j].PlanID })
	median := medianActiveSeconds(activeSamples)
	var abandonRatio *float64
	if terminalCount > 0 {
		value := float64(abandonedCount) / float64(terminalCount)
		abandonRatio = &value
	}

	cohortComplete := len(decisions) == stage1RequiredSampleCount && len(included) == stage1RequiredSampleCount && !missingTerminalFact
	g1Met := cohortComplete && len(livePlanIDs) >= stage1LivePlanThreshold
	g2Met := cohortComplete && median != nil && *median <= stage1MedianThreshold
	status := "insufficient"
	if cohortComplete {
		status = "failed"
		if g1Met && g2Met {
			status = "passed"
		}
	}

	payload := stage1ProjectionPayload{
		GateVersion:     Stage1EvidenceGateVersion,
		Status:          status,
		Stale:           false,
		WindowID:        windowID,
		BuildRevision:   buildRevision,
		SampleDecisions: decisions,
		CohortComplete:  cohortComplete,
		G1: Stage1G1Evidence{
			DistinctLivePlanCount: len(livePlanIDs),
			EligiblePlanCount:     len(included),
			RequiredSampleCount:   stage1RequiredSampleCount,
			Threshold:             stage1LivePlanThreshold,
			LivePlanIDs:           livePlanIDs,
			ThresholdMet:          g1Met,
		},
		G2: Stage1G2Evidence{
			SuccessfulPlanCount:    len(activeSamples),
			RequiredSampleCount:    stage1RequiredSampleCount,
			ActiveSecondsSamples:   activeSamples,
			MedianActiveSeconds:    median,
			MedianThresholdSeconds: stage1MedianThreshold,
			MedianThresholdMet:     g2Met,
			AbandonedPlanCount:     abandonedCount,
			TerminalPlanCount:      terminalCount,
			AbandonRatio:           abandonRatio,
		},
		Exclusions:   flattenStage1Exclusions(exclusions),
		RuleVersions: Stage1EvidenceRuleVersions{EvidenceProjector: Stage1EvidenceGateVersion, ActiveTime: "plan-build-active-idle-cap-v1", RunCaptureMode: "run-mode-capture-mode-v1", IdleRules: idleRules},
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return Stage1EvidenceArtifact{}, err
	}
	projectionDigest := sha256.Sum256(payloadJSON)
	report := Stage1EvidenceReport{
		GateVersion: payload.GateVersion, Status: payload.Status, Stale: payload.Stale,
		WindowID: payload.WindowID, BuildRevision: payload.BuildRevision,
		SampleDecisions: payload.SampleDecisions, CohortComplete: payload.CohortComplete,
		G1: payload.G1, G2: payload.G2, Exclusions: payload.Exclusions, RuleVersions: payload.RuleVersions,
		ProjectionSHA256: hex.EncodeToString(projectionDigest[:]),
	}
	canonicalJSON, err := json.Marshal(report)
	if err != nil {
		return Stage1EvidenceArtifact{}, err
	}
	artifactDigest := sha256.Sum256(canonicalJSON)
	return Stage1EvidenceArtifact{Report: report, CanonicalJSON: canonicalJSON, SHA256: hex.EncodeToString(artifactDigest[:])}, nil
}

func normalizeStage1Decisions(input []Stage1SampleDecision) ([]Stage1SampleDecision, map[string]struct{}, map[string]map[string]struct{}, error) {
	decisions := append([]Stage1SampleDecision(nil), input...)
	sort.Slice(decisions, func(i, j int) bool { return decisions[i].PlanID < decisions[j].PlanID })
	included := make(map[string]struct{}, len(decisions))
	exclusions := make(map[string]map[string]struct{})
	seen := make(map[string]struct{}, len(decisions))
	for index := range decisions {
		decision := &decisions[index]
		decision.PlanID = strings.TrimSpace(decision.PlanID)
		decision.ExclusionReason = strings.TrimSpace(decision.ExclusionReason)
		decision.OwnerReviewRef = strings.TrimSpace(decision.OwnerReviewRef)
		if decision.PlanID == "" || decision.OwnerReviewRef == "" {
			return nil, nil, nil, fmt.Errorf("stage1 sample plan_id and owner_review_ref are required")
		}
		if _, duplicate := seen[decision.PlanID]; duplicate {
			return nil, nil, nil, fmt.Errorf("duplicate stage1 sample decision for plan %s", decision.PlanID)
		}
		seen[decision.PlanID] = struct{}{}
		switch decision.Disposition {
		case Stage1SampleInclude:
			if decision.ExclusionReason != "" {
				return nil, nil, nil, fmt.Errorf("included stage1 sample %s has exclusion reason", decision.PlanID)
			}
			included[decision.PlanID] = struct{}{}
		case Stage1SampleExclude:
			if decision.ExclusionReason == "" {
				return nil, nil, nil, fmt.Errorf("excluded stage1 sample %s requires a reason", decision.PlanID)
			}
			addStage1Exclusion(exclusions, decision.ExclusionReason, decision.PlanID)
		default:
			return nil, nil, nil, fmt.Errorf("unknown stage1 sample disposition for plan %s", decision.PlanID)
		}
	}
	return decisions, included, exclusions, nil
}

func indexStage1Observations(input []PlanBuildObservation) (map[string]PlanBuildObservation, []int, error) {
	result := make(map[string]PlanBuildObservation, len(input))
	idleSet := make(map[int]struct{})
	for _, observation := range input {
		planID := strings.TrimSpace(observation.PlanID)
		if planID == "" || observation.ActiveSeconds < 0 || observation.IdleRuleVersion < 1 {
			return nil, nil, fmt.Errorf("invalid stage1 observation")
		}
		if _, duplicate := result[planID]; duplicate {
			return nil, nil, fmt.Errorf("duplicate stage1 observation for plan %s", planID)
		}
		observation.PlanID = planID
		result[planID] = observation
		idleSet[observation.IdleRuleVersion] = struct{}{}
	}
	idleRules := make([]int, 0, len(idleSet))
	for version := range idleSet {
		idleRules = append(idleRules, version)
	}
	sort.Ints(idleRules)
	return result, idleRules, nil
}

func medianActiveSeconds(samples []Stage1ActiveSample) *float64 {
	if len(samples) == 0 {
		return nil
	}
	values := make([]int64, 0, len(samples))
	for _, sample := range samples {
		values = append(values, sample.ActiveSeconds)
	}
	sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
	middle := len(values) / 2
	value := float64(values[middle])
	if len(values)%2 == 0 {
		value = float64(values[middle-1]+values[middle]) / 2
	}
	return &value
}

func addStage1Exclusion(exclusions map[string]map[string]struct{}, reason, planID string) {
	if exclusions[reason] == nil {
		exclusions[reason] = make(map[string]struct{})
	}
	exclusions[reason][planID] = struct{}{}
}

func flattenStage1Exclusions(input map[string]map[string]struct{}) []Stage1Exclusion {
	reasons := make([]string, 0, len(input))
	for reason := range input {
		reasons = append(reasons, reason)
	}
	sort.Strings(reasons)
	result := make([]Stage1Exclusion, 0, len(reasons))
	for _, reason := range reasons {
		ids := sortedSet(input[reason])
		result = append(result, Stage1Exclusion{Reason: reason, Count: len(ids), PlanIDs: ids})
	}
	return result
}

func sortedSet(input map[string]struct{}) []string {
	result := make([]string, 0, len(input))
	for value := range input {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}
