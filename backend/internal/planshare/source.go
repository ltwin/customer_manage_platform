package planshare

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/planningmedia"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
	"github.com/samson/customer-manage-platform/backend/internal/platform/txcap"
	"github.com/samson/customer-manage-platform/backend/internal/shootplanning/crm"
)

// queryProbe counts scoped table touches for proposal boundary tests.
type queryProbe interface {
	RecordTable(table string)
}

type probeContextKey struct{}

// WithQueryProbe attaches a SQL table probe used by share source readers.
func WithQueryProbe(ctx context.Context, probe queryProbe) context.Context {
	return context.WithValue(ctx, probeContextKey{}, probe)
}

func recordProbe(ctx context.Context, table string) {
	if probe, ok := ctx.Value(probeContextKey{}).(queryProbe); ok && probe != nil {
		probe.RecordTable(table)
	}
}

// CountingQueryProbe is a test helper for proposal query-boundary assertions.
type CountingQueryProbe struct {
	counts map[string]int
}

func NewCountingQueryProbe() *CountingQueryProbe {
	return &CountingQueryProbe{counts: map[string]int{}}
}

func (p *CountingQueryProbe) RecordTable(table string) {
	if p.counts == nil {
		p.counts = map[string]int{}
	}
	p.counts[table]++
}

func (p *CountingQueryProbe) Count(table string) int {
	if p == nil {
		return 0
	}
	return p.counts[table]
}

type shareSourceReader struct {
	tx store.TxAccountScope
}

func (r shareSourceReader) ReadProposalSourceInShare(ctx context.Context, planID string) (ProposalSource, error) {
	recordProbe(ctx, "shoot_plans")
	var (
		title       string
		briefJSON   []byte
		look, scene sql.NullInt64
		revision    int64
		archivedAt  sql.NullTime
	)
	err := r.tx.QueryRow(ctx, "shoot_plans",
		"id, title, creative_brief, planned_look_count, planned_scene_count, revision, archived_at",
		"id = $2", planID).Scan(&planID, &title, &briefJSON, &look, &scene, &revision, &archivedAt)
	if errors.Is(err, store.ErrNoRows) {
		return ProposalSource{}, ErrNotFound
	}
	if err != nil {
		return ProposalSource{}, err
	}
	var brief SharedCreativeBriefV1
	if len(briefJSON) > 0 {
		if err := json.Unmarshal(briefJSON, &brief); err != nil {
			return ProposalSource{}, fmt.Errorf("decode creative brief: %w", err)
		}
	}
	if brief.VisualKeywords == nil {
		brief.VisualKeywords = []string{}
	}
	// planned_shot_count via list projection avoids loading Shot rows.
	recordProbe(ctx, "shoot_plan_list_projection")
	var shotCount int64
	err = r.tx.QueryRow(ctx, "shoot_plan_list_projection", "planned_shot_count", "id = $2", planID).
		Scan(&shotCount)
	if errors.Is(err, store.ErrNoRows) {
		shotCount = 0
	} else if err != nil {
		return ProposalSource{}, err
	}
	scale := SharedPublicScaleV1{PlannedShotCount: int(shotCount)}
	if look.Valid {
		value := int(look.Int64)
		scale.PlannedLookCount = &value
	}
	if scene.Valid {
		value := int(scene.Int64)
		scale.PlannedSceneCount = &value
	}
	window, err := r.loadPublicWindow(ctx, planID)
	if err != nil {
		return ProposalSource{}, err
	}
	return ProposalSource{
		PlanID:             planID,
		Title:              title,
		CreativeBrief:      brief,
		PublicWindow:       window,
		PublicScale:        scale,
		ProjectionRevision: revision,
		Archived:           archivedAt.Valid,
	}, nil
}

func (r shareSourceReader) ReadFullSourceInShare(ctx context.Context, planID string) (FullSource, error) {
	proposal, err := r.ReadProposalSourceInShare(ctx, planID)
	if err != nil {
		return FullSource{}, err
	}
	shots, err := r.loadSharedShots(ctx, planID)
	if err != nil {
		return FullSource{}, err
	}
	readiness, err := r.loadReadinessOpportunities(ctx, planID)
	if err != nil {
		return FullSource{}, err
	}
	return FullSource{ProposalSource: proposal, Shots: shots, Readiness: readiness}, nil
}

func (r shareSourceReader) loadPublicWindow(ctx context.Context, planID string) (*SharedPublicWindowV1, error) {
	recordProbe(ctx, "shoot_plan_execution_windows")
	var startsAt, endsAt time.Time
	var timezone string
	err := r.tx.QueryRow(ctx, "shoot_plan_execution_windows",
		"starts_at, ends_at, timezone", "plan_id = $2", planID).
		Scan(&startsAt, &endsAt, &timezone)
	if errors.Is(err, store.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	startsAt = startsAt.UTC()
	endsAt = endsAt.UTC()
	duration := int(endsAt.Sub(startsAt) / time.Minute)
	if duration < 0 {
		duration = 0
	}
	return &SharedPublicWindowV1{
		StartsAt:        startsAt,
		EndsAt:          endsAt,
		Timezone:        timezone,
		DurationMinutes: duration,
	}, nil
}

func (r shareSourceReader) loadSharedShots(ctx context.Context, planID string) ([]SharedShotV1, error) {
	recordProbe(ctx, "shoot_plan_shots")
	rows, err := r.tx.Query(ctx, "shoot_plan_shots",
		"id, position, title, scene, action, expression, composition, lighting_text, framing_tag, lighting_direction_tag, lighting_quality_tag, palette_tag, shot_type_tag, revision",
		"plan_id = $2 AND removed_at IS NULL", planID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]SharedShotV1, 0)
	for rows.Next() {
		var shot SharedShotV1
		if err := rows.Scan(
			&shot.ID, &shot.Position, &shot.Title, &shot.Scene, &shot.Action, &shot.Expression,
			&shot.Composition, &shot.LightingText, &shot.FramingTag, &shot.LightingDirectionTag,
			&shot.LightingQualityTag, &shot.PaletteTag, &shot.ShotTypeTag, &shot.Revision,
		); err != nil {
			return nil, err
		}
		out = append(out, shot)
	}
	return out, rows.Err()
}

func (r shareSourceReader) loadReadinessOpportunities(ctx context.Context, planID string) ([]ReadinessOpportunitySource, error) {
	recordProbe(ctx, "shoot_plan_readiness_items")
	rows, err := r.tx.Query(ctx, "shoot_plan_readiness_items",
		"id, requirement, default_preparation_lead_days, revision",
		"plan_id = $2 AND removed_at IS NULL", planID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]ReadinessOpportunitySource, 0)
	for rows.Next() {
		var item ReadinessOpportunitySource
		var lead sql.NullInt64
		if err := rows.Scan(&item.ReadinessItemID, &item.Content, &lead, &item.TargetRevision); err != nil {
			return nil, err
		}
		if lead.Valid {
			value := int(lead.Int64)
			item.PreparationLeadDaysPreview = &value
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

type shareEligibilityReader struct {
	tx          store.TxAccountScope
	fingerprint string
}

func (r shareEligibilityReader) PreReadShareEligibility(
	ctx context.Context,
	_ txcap.ValidatedShareContext,
) (ShareEligibilityHint, error) {
	gen, err := loadGenerationByFingerprint(ctx, r.tx, r.fingerprint)
	if err != nil {
		return ShareEligibilityHint{}, err
	}
	hint := ShareEligibilityHint{PlanID: gen.PlanID}
	if gen.EligibilityLinkEpochID != nil {
		hint.LinkEpochID = *gen.EligibilityLinkEpochID
	}
	return hint, nil
}

func (r shareEligibilityReader) LockAndRecheckShareEligibilityInShare(
	ctx context.Context,
	hint ShareEligibilityHint,
) (ShareEligibility, error) {
	// Anonymous GET uses non-locking live eligibility (design §2.2).
	return liveShareEligibility(ctx, r.tx, hint)
}

func liveShareEligibility(
	ctx context.Context,
	tx store.TxAccountScope,
	hint ShareEligibilityHint,
) (ShareEligibility, error) {
	now := time.Now().UTC()
	conn, err := crm.LoadConnection(ctx, tx, hint.PlanID, false)
	if err != nil {
		if errors.Is(err, crm.ErrNotFound) {
			return ShareEligibility{Eligible: false, ObservedAt: now}, nil
		}
		return ShareEligibility{}, err
	}
	if conn.State != crm.StateOrderLinked ||
		conn.CustomerID == nil || conn.OrderID == nil || conn.LinkEpochID == nil {
		return ShareEligibility{
			LinkEpochID: hint.LinkEpochID,
			Eligible:    false,
			ObservedAt:  now,
		}, nil
	}
	if hint.LinkEpochID != "" && *conn.LinkEpochID != hint.LinkEpochID {
		return ShareEligibility{
			LinkEpochID: *conn.LinkEpochID,
			Eligible:    false,
			ObservedAt:  now,
		}, nil
	}
	var orderStatus string
	recordProbe(ctx, "orders")
	err = tx.QueryRow(ctx, "orders", "status", "id = $2", *conn.OrderID).Scan(&orderStatus)
	if errors.Is(err, store.ErrNoRows) {
		return ShareEligibility{LinkEpochID: *conn.LinkEpochID, Eligible: false, ObservedAt: now}, nil
	}
	if err != nil {
		return ShareEligibility{}, err
	}
	eligible := false
	for _, status := range fullEligibleOrderStatuses {
		if status == orderStatus {
			eligible = true
			break
		}
	}
	out := ShareEligibility{
		LinkEpochID: *conn.LinkEpochID,
		Eligible:    eligible,
		ObservedAt:  now,
	}
	var slotID string
	err = tx.QueryRow(ctx, "schedule_slots", "id", "type = 'shoot' AND order_id = $2", *conn.OrderID).Scan(&slotID)
	if err == nil {
		out.SlotID = &slotID
	} else if !errors.Is(err, store.ErrNoRows) {
		return ShareEligibility{}, err
	}
	var windowRev int64
	err = tx.QueryRow(ctx, "shoot_plan_execution_windows", "revision", "plan_id = $2", hint.PlanID).Scan(&windowRev)
	if err == nil {
		out.WindowRevision = &windowRev
	} else if !errors.Is(err, store.ErrNoRows) {
		return ShareEligibility{}, err
	}
	return out, nil
}

type shareMediaReader struct {
	tx    store.TxAccountScope
	media *planningmedia.Application
}

func (r shareMediaReader) ListMoodboardBindingsInShare(
	ctx context.Context,
	planID string,
) ([]MoodboardBindingRef, error) {
	recordProbe(ctx, "planning_media_bindings")
	bindingRows, err := r.tx.Query(ctx, "planning_media_bindings",
		"id, asset_id, generation",
		"plan_id = $2 AND holder_kind = 'plan' AND holder_id = $2 AND purpose = 'moodboard_display' AND state = 'active'",
		planID)
	if err != nil {
		return nil, err
	}
	defer bindingRows.Close()
	type bindingRow struct {
		id, assetID string
		generation  int
	}
	bindings := make([]bindingRow, 0)
	for bindingRows.Next() {
		var row bindingRow
		if err := bindingRows.Scan(&row.id, &row.assetID, &row.generation); err != nil {
			return nil, err
		}
		bindings = append(bindings, row)
	}
	if err := bindingRows.Err(); err != nil {
		return nil, err
	}
	out := make([]MoodboardBindingRef, 0, len(bindings))
	for _, binding := range bindings {
		recordProbe(ctx, "planning_media_generations")
		var checksum string
		err := r.tx.QueryRow(ctx, "planning_media_generations",
			"display_checksum", "asset_id = $2 AND generation = $3",
			binding.assetID, binding.generation).Scan(&checksum)
		if errors.Is(err, store.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, err
		}
		recordProbe(ctx, "planning_media_assets")
		var caption string
		if err := r.tx.QueryRow(ctx, "planning_media_assets",
			"display_name", "id = $2", binding.assetID).Scan(&caption); err != nil {
			return nil, err
		}
		recordProbe(ctx, "planning_media_rights_declarations")
		var sourceClass string
		if err := r.tx.QueryRow(ctx, "planning_media_rights_declarations",
			"source_class", "asset_id = $2 AND generation = $3",
			binding.assetID, binding.generation).Scan(&sourceClass); err != nil {
			return nil, err
		}
		out = append(out, MoodboardBindingRef{
			BindingID:       binding.id,
			AssetID:         binding.assetID,
			ExactGeneration: binding.generation,
			DisplayChecksum: checksum,
			Caption:         caption,
			UsageNote:       moodboardUsageNote(sourceClass),
		})
	}
	return out, nil
}
