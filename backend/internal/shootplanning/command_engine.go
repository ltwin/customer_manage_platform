package shootplanning

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/samson/customer-manage-platform/backend/internal/platform/planningcapability"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
	"github.com/samson/customer-manage-platform/backend/internal/shootplanning/crm"
	"github.com/samson/customer-manage-platform/backend/internal/shootplanning/planningreminder"
)

func (a *Application) applyPlanCommandInScope(
	ctx context.Context,
	tx store.TxAccountScope,
	planID string,
	expectedRevision int64,
	command PlanCommand,
) (PlanMutationResult, error) {
	switch command.(type) {
	case SetExecutionWindowCommand, ClearExecutionWindowCommand:
		return a.applyManualWindowWithCRM(ctx, tx, planID, expectedRevision, command)
	}
	plan, err := a.repo.LockPlan(ctx, tx, planID)
	if err != nil {
		return PlanMutationResult{}, err
	}
	if plan.Revision != expectedRevision {
		return PlanMutationResult{}, ErrPlanRevisionConflict
	}
	switch plan.Status {
	case PlanStatusCompleted:
		return PlanMutationResult{}, ErrReopenRequired
	case PlanStatusArchived:
		return PlanMutationResult{}, ErrArchivedReadOnly
	}

	changed := true
	projection := make(map[string]any)
	switch command := command.(type) {
	case UpdateBriefCommand:
		err = applyBriefCommand(ctx, tx, plan, command)
	case UpsertShotCommand:
		var id string
		id, err = applyUpsertShot(ctx, tx, planID, command)
		projection["shot_id"] = id
	case ReorderShotsCommand:
		err = applyReorderShots(ctx, tx, planID, command.OrderedShotIDs)
	case RemoveShotCommand:
		err = applyRemoveShot(ctx, tx, planID, command)
	case UpsertReadinessCommand:
		var id string
		id, err = applyUpsertReadiness(ctx, tx, planID, command)
		projection["readiness_id"] = id
	case RemoveReadinessCommand:
		err = a.applyRemoveReadiness(ctx, tx, planID, command.ReadinessID)
	case SetPreflightCommand:
		err = applySetPreflight(ctx, tx, planID, command)
	case LinkReadinessCommand:
		changed, err = applyLinkReadiness(ctx, tx, planID, command.ShotID, command.ReadinessID)
	case UnlinkReadinessCommand:
		changed, err = applyUnlinkReadiness(ctx, tx, planID, command.ShotID, command.ReadinessID)
	case SetPublicScaleCommand:
		err = applyPublicScale(ctx, tx, planID, command)
	default:
		err = errors.New("unknown plan command")
	}
	if err != nil {
		return PlanMutationResult{}, err
	}
	status := plan.Status
	if status == PlanStatusReady {
		incomplete, err := requiredReadinessIncomplete(ctx, tx, planID)
		if err != nil {
			return PlanMutationResult{}, err
		}
		status = StatusAfterStructuralMutation(status, !incomplete)
	}
	revision := plan.Revision
	if changed {
		setClause := "revision = revision + 1, updated_at = clock_timestamp(), status = $2"
		updated, err := tx.Update(ctx, "shoot_plans", setClause, "id = $3 AND revision = $4", string(status), planID, expectedRevision)
		if err != nil {
			return PlanMutationResult{}, err
		}
		if updated != 1 {
			return PlanMutationResult{}, ErrPlanRevisionConflict
		}
		revision++
	}
	return PlanMutationResult{PlanID: planID, Revision: revision, Status: status, ChangedProjection: projection}, nil
}

func applyBriefCommand(ctx context.Context, tx store.TxAccountScope, plan ShootPlan, command UpdateBriefCommand) error {
	title, subject, brief := plan.Title, plan.Subject, plan.CreativeBrief
	if command.Title != nil {
		title = strings.TrimSpace(*command.Title)
		if runeLen(title) < 1 || runeLen(title) > 160 {
			return validationError("title invalid")
		}
	}
	if command.Subject != nil {
		subject = strings.TrimSpace(*command.Subject)
		if runeLen(subject) < 1 || runeLen(subject) > 240 {
			return validationError("subject invalid")
		}
	}
	if command.CreativeBrief != nil {
		if err := applyBriefPatch(&brief, *command.CreativeBrief); err != nil {
			return err
		}
	}
	briefJSON, err := json.Marshal(brief)
	if err != nil {
		return err
	}
	_, err = tx.Update(ctx, "shoot_plans", "title = $2, subject = $3, creative_brief = $4", "id = $5", title, subject, briefJSON, plan.ID)
	return err
}

func applyBriefPatch(brief *CreativeBrief, patch CreativeBriefPatch) error {
	apply := func(target **string, value Optional[string], max int) error {
		if !value.Specified {
			return nil
		}
		if value.Null {
			*target = nil
			return nil
		}
		normalized := strings.TrimSpace(value.Value)
		if normalized == "" {
			*target = nil
			return nil
		}
		if runeLen(normalized) > max {
			return validationError("creative brief field too long")
		}
		*target = &normalized
		return nil
	}
	if err := apply(&brief.WorkTitle, patch.WorkTitle, 120); err != nil {
		return err
	}
	if err := apply(&brief.CharacterName, patch.CharacterName, 120); err != nil {
		return err
	}
	if err := apply(&brief.ThemeStatement, patch.ThemeStatement, 2000); err != nil {
		return err
	}
	if err := apply(&brief.Mood, patch.Mood, 500); err != nil {
		return err
	}
	if patch.VisualKeywords.Specified {
		if patch.VisualKeywords.Null {
			brief.VisualKeywords = nil
		} else {
			if len(patch.VisualKeywords.Value) > 20 {
				return validationError("too many visual keywords")
			}
			seen := make(map[string]struct{}, len(patch.VisualKeywords.Value))
			keywords := make([]string, 0, len(patch.VisualKeywords.Value))
			for _, keyword := range patch.VisualKeywords.Value {
				keyword = strings.TrimSpace(keyword)
				if runeLen(keyword) < 1 || runeLen(keyword) > 40 {
					return validationError("visual keyword invalid")
				}
				if _, exists := seen[keyword]; exists {
					return validationError("visual keywords must be unique")
				}
				seen[keyword] = struct{}{}
				keywords = append(keywords, keyword)
			}
			brief.VisualKeywords = keywords
		}
	}
	return nil
}

func applyUpsertShot(ctx context.Context, tx store.TxAccountScope, planID string, command UpsertShotCommand) (string, error) {
	if command.ShotID == nil {
		if command.Shot.Title == nil {
			return "", validationError("shot title required")
		}
		title := strings.TrimSpace(*command.Shot.Title)
		if runeLen(title) < 1 || runeLen(title) > 160 {
			return "", validationError("shot title invalid")
		}
		position, err := insertionPosition(ctx, tx, planID, command.InsertAfterShotID)
		if err != nil {
			return "", err
		}
		if err := shiftShotPositions(ctx, tx, planID, position); err != nil {
			return "", err
		}
		id := "shot_" + uuid.NewString()
		values, err := normalizeShotWrite(normalizedShotWrite{}, command.Shot)
		if err != nil {
			return "", err
		}
		if err := tx.Insert(ctx, "shoot_plan_shots",
			[]string{"id", "plan_id", "position", "title", "scene", "action", "expression", "composition", "lighting_text", "notes", "framing_tag", "lighting_direction_tag", "lighting_quality_tag", "palette_tag", "shot_type_tag"},
			id, planID, position, title, values.scene, values.action, values.expression, values.composition, values.lighting, values.notes,
			values.framingTag, values.lightingDirectionTag, values.lightingQualityTag, values.paletteTag, values.shotTypeTag); err != nil {
			return "", err
		}
		return id, nil
	}
	id := strings.TrimSpace(*command.ShotID)
	var current normalizedShotWrite
	if err := tx.QueryRowForUpdate(ctx, "shoot_plan_shots",
		"title, scene, action, expression, composition, lighting_text, notes, framing_tag, lighting_direction_tag, lighting_quality_tag, palette_tag, shot_type_tag",
		"plan_id = $2 AND id = $3 AND removed_at IS NULL", planID, id).Scan(
		&current.title, &current.scene, &current.action, &current.expression, &current.composition, &current.lighting,
		&current.notes, &current.framingTag, &current.lightingDirectionTag, &current.lightingQualityTag, &current.paletteTag, &current.shotTypeTag,
	); errors.Is(err, store.ErrNoRows) {
		return "", ErrShotNotFound
	} else if err != nil {
		return "", err
	}
	values, err := normalizeShotWrite(current, command.Shot)
	if err != nil {
		return "", err
	}
	if command.Shot.Title != nil {
		values.title = strings.TrimSpace(*command.Shot.Title)
		if runeLen(values.title) < 1 || runeLen(values.title) > 160 {
			return "", validationError("shot title invalid")
		}
	}
	_, err = tx.Update(ctx, "shoot_plan_shots",
		"title = $2, scene = $3, action = $4, expression = $5, composition = $6, lighting_text = $7, notes = $8, framing_tag = $9, lighting_direction_tag = $10, lighting_quality_tag = $11, palette_tag = $12, shot_type_tag = $13, revision = revision + 1, updated_at = clock_timestamp()",
		"plan_id = $14 AND id = $15 AND removed_at IS NULL", values.title, values.scene, values.action, values.expression,
		values.composition, values.lighting, values.notes, values.framingTag, values.lightingDirectionTag,
		values.lightingQualityTag, values.paletteTag, values.shotTypeTag, planID, id)
	return id, err
}

type normalizedShotWrite struct {
	title                                                            string
	scene, action, expression, composition, lighting, notes          *string
	framingTag, lightingDirectionTag, lightingQualityTag, paletteTag *string
	shotTypeTag                                                      *string
}

func normalizeShotWrite(current normalizedShotWrite, input ShotWrite) (normalizedShotWrite, error) {
	textFields := []struct {
		target **string
		value  Optional[string]
		max    int
	}{
		{&current.scene, input.Scene, 1000}, {&current.action, input.Action, 1000},
		{&current.expression, input.Expression, 1000}, {&current.composition, input.Composition, 1000},
		{&current.lighting, input.Lighting, 1000}, {&current.notes, input.Notes, 2000},
	}
	for _, field := range textFields {
		if err := applyOptionalShotText(field.target, field.value, field.max, nil); err != nil {
			return normalizedShotWrite{}, err
		}
	}
	tagFields := []struct {
		target  **string
		value   Optional[string]
		allowed map[string]struct{}
	}{
		{&current.framingTag, input.FramingTag, stringSet("extreme_closeup", "closeup", "medium_closeup", "medium", "full", "wide", "extreme_wide", "other")},
		{&current.lightingDirectionTag, input.LightingDirectionTag, stringSet("front", "side", "back", "top", "bottom", "mixed", "natural", "other")},
		{&current.lightingQualityTag, input.LightingQualityTag, stringSet("hard", "soft", "mixed", "natural", "other")},
		{&current.paletteTag, input.PaletteTag, stringSet("warm", "cool", "neutral", "monochrome", "high_saturation", "low_saturation", "mixed", "other")},
		{&current.shotTypeTag, input.ShotTypeTag, stringSet("portrait", "action", "interaction", "environment", "detail", "silhouette", "narrative", "other")},
	}
	for _, field := range tagFields {
		if err := applyOptionalShotText(field.target, field.value, 0, field.allowed); err != nil {
			return normalizedShotWrite{}, err
		}
	}
	return current, nil
}

func applyOptionalShotText(target **string, value Optional[string], max int, allowed map[string]struct{}) error {
	if !value.Specified {
		return nil
	}
	if value.Null {
		*target = nil
		return nil
	}
	normalized := strings.TrimSpace(value.Value)
	if normalized == "" {
		*target = nil
		return nil
	}
	if max > 0 && runeLen(normalized) > max {
		return validationError("shot field too long")
	}
	if allowed != nil {
		if _, exists := allowed[normalized]; !exists {
			return validationError("shot taxonomy invalid")
		}
	}
	*target = &normalized
	return nil
}

func stringSet(values ...string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		result[value] = struct{}{}
	}
	return result
}

func insertionPosition(ctx context.Context, tx store.TxAccountScope, planID string, after *string) (int, error) {
	if after == nil {
		count, err := tx.Count(ctx, "shoot_plan_shots", "plan_id = $2 AND removed_at IS NULL", planID)
		if err != nil || count == 0 {
			return 1, err
		}
		var maxPosition int
		if err := tx.ScalarAggregate(ctx, "shoot_plan_shots", store.AggregateMax, "position", "plan_id = $2 AND removed_at IS NULL", planID).Scan(&maxPosition); err != nil {
			return 0, err
		}
		return maxPosition + 1, nil
	}
	var position int
	if err := tx.QueryRow(ctx, "shoot_plan_shots", "position", "plan_id = $2 AND id = $3 AND removed_at IS NULL", planID, *after).Scan(&position); errors.Is(err, store.ErrNoRows) {
		return 0, ErrShotNotFound
	} else if err != nil {
		return 0, err
	}
	return position + 1, nil
}

func shiftShotPositions(ctx context.Context, tx store.TxAccountScope, planID string, from int) error {
	rows, err := tx.Query(ctx, "shoot_plan_shots", "id, position", "plan_id = $2 AND removed_at IS NULL", planID)
	if err != nil {
		return err
	}
	type positioned struct {
		id       string
		position int
	}
	var items []positioned
	for rows.Next() {
		var item positioned
		if err := rows.Scan(&item.id, &item.position); err != nil {
			rows.Close()
			return err
		}
		if item.position >= from {
			items = append(items, item)
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	sort.Slice(items, func(i, j int) bool { return items[i].position > items[j].position })
	for _, item := range items {
		if _, err := tx.Update(ctx, "shoot_plan_shots", "position = $2", "plan_id = $3 AND id = $4", item.position+1, planID, item.id); err != nil {
			return err
		}
	}
	return nil
}

func applyReorderShots(ctx context.Context, tx store.TxAccountScope, planID string, ordered []string) error {
	current, err := currentShotIDs(ctx, tx, planID)
	if err != nil {
		return err
	}
	if !sameStringSet(current, ordered) {
		return validationError("shot_order_mismatch")
	}
	if len(ordered) == 0 {
		return nil
	}
	var maxPosition int
	if err := tx.ScalarAggregate(ctx, "shoot_plan_shots", store.AggregateMax, "position", "plan_id = $2 AND removed_at IS NULL", planID).Scan(&maxPosition); err != nil {
		return err
	}
	if maxPosition > 2147483647-len(ordered)-1 {
		return errors.New("shot position overflow")
	}
	offset := maxPosition + len(ordered) + 1
	if _, err := tx.Update(ctx, "shoot_plan_shots", "position = position + $2", "plan_id = $3 AND removed_at IS NULL", offset, planID); err != nil {
		return err
	}
	for index, id := range ordered {
		updated, err := tx.Update(ctx, "shoot_plan_shots", "position = $2", "plan_id = $3 AND id = $4 AND removed_at IS NULL", index+1, planID, id)
		if err != nil {
			return err
		}
		if updated != 1 {
			return validationError("shot_order_mismatch")
		}
	}
	return nil
}

func currentShotIDs(ctx context.Context, tx store.TxAccountScope, planID string) ([]string, error) {
	rows, err := tx.Query(ctx, "shoot_plan_shots", "id, position", "plan_id = $2 AND removed_at IS NULL", planID)
	if err != nil {
		return nil, err
	}
	type item struct {
		id       string
		position int
	}
	var items []item
	for rows.Next() {
		var value item
		if err := rows.Scan(&value.id, &value.position); err != nil {
			rows.Close()
			return nil, err
		}
		items = append(items, value)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	sort.Slice(items, func(i, j int) bool { return items[i].position < items[j].position })
	ids := make([]string, len(items))
	for index := range items {
		ids[index] = items[index].id
	}
	return ids, nil
}

func applyRemoveShot(ctx context.Context, tx store.TxAccountScope, planID string, command RemoveShotCommand) error {
	var id string
	if err := tx.QueryRowForUpdate(ctx, "shoot_plan_shots", "id", "plan_id = $2 AND id = $3 AND removed_at IS NULL", planID, command.ShotID).Scan(&id); errors.Is(err, store.ErrNoRows) {
		return ErrShotNotFound
	} else if err != nil {
		return err
	}
	history, err := tx.Count(ctx, "shoot_plan_execution_events", "plan_id = $2 AND shot_id = $3", planID, id)
	if err != nil {
		return err
	}
	if history > 0 && !command.AcknowledgeExecutionHistory {
		return ErrExecutionHistoryAckRequired
	}
	if _, err := tx.Delete(ctx, "shoot_plan_shot_readiness_links", "plan_id = $2 AND shot_id = $3", planID, id); err != nil {
		return err
	}
	_, err = tx.Update(ctx, "shoot_plan_shots", "removed_at = clock_timestamp(), updated_at = clock_timestamp()", "plan_id = $2 AND id = $3", planID, id)
	return err
}

func applyUpsertReadiness(ctx context.Context, tx store.TxAccountScope, planID string, command UpsertReadinessCommand) (string, error) {
	if command.ReadinessID == nil {
		values, err := normalizeReadinessWrite(command.Item, true)
		if err != nil {
			return "", err
		}
		id := "ready_" + uuid.NewString()
		if err := tx.Insert(ctx, "shoot_plan_readiness_items",
			[]string{"id", "plan_id", "category", "title", "requirement", "preflight_status", "responsibility_hint", "default_preparation_lead_days"},
			id, planID, values.category, values.title, values.requirement, values.preflight, values.responsibility, values.leadDays); err != nil {
			return "", err
		}
		return id, nil
	}
	id := *command.ReadinessID
	var current normalizedReadiness
	if err := tx.QueryRowForUpdate(ctx, "shoot_plan_readiness_items",
		"category, title, requirement, preflight_status, responsibility_hint, default_preparation_lead_days",
		"plan_id = $2 AND id = $3 AND removed_at IS NULL", planID, id).Scan(&current.category, &current.title, &current.requirement, &current.preflight, &current.responsibility, &current.leadDays); errors.Is(err, store.ErrNoRows) {
		return "", ErrReadinessNotFound
	} else if err != nil {
		return "", err
	}
	values, err := mergeReadinessWrite(current, command.Item)
	if err != nil {
		return "", err
	}
	_, err = tx.Update(ctx, "shoot_plan_readiness_items",
		"category = $2, title = $3, requirement = $4, preflight_status = $5, responsibility_hint = $6, default_preparation_lead_days = $7, revision = revision + 1, updated_at = clock_timestamp()",
		"plan_id = $8 AND id = $9 AND removed_at IS NULL", values.category, values.title, values.requirement, values.preflight, values.responsibility, values.leadDays, planID, id)
	return id, err
}

type normalizedReadiness struct {
	category, title, requirement, preflight, responsibility string
	leadDays                                                *int
}

func normalizeReadinessWrite(input ReadinessWrite, create bool) (normalizedReadiness, error) {
	result := normalizedReadiness{category: "other", requirement: "optional", preflight: "unchecked", responsibility: "unassigned"}
	if input.Category != nil {
		result.category = *input.Category
	}
	if input.Title != nil {
		result.title = strings.TrimSpace(*input.Title)
	}
	if input.Requirement != nil {
		result.requirement = *input.Requirement
	}
	if input.PreflightStatus != nil {
		result.preflight = *input.PreflightStatus
	}
	if input.ResponsibilityHint != nil {
		result.responsibility = *input.ResponsibilityHint
	}
	if input.DefaultPreparationLeadDays.Specified && !input.DefaultPreparationLeadDays.Null {
		leadDays := input.DefaultPreparationLeadDays.Value
		result.leadDays = &leadDays
	}
	if create && result.title == "" {
		return normalizedReadiness{}, validationError("readiness title required")
	}
	if !validReadiness(result) {
		return normalizedReadiness{}, validationError("readiness invalid")
	}
	return result, nil
}

func mergeReadinessWrite(current normalizedReadiness, input ReadinessWrite) (normalizedReadiness, error) {
	if input.Category != nil {
		current.category = *input.Category
	}
	if input.Title != nil {
		current.title = strings.TrimSpace(*input.Title)
	}
	if input.Requirement != nil {
		current.requirement = *input.Requirement
	}
	if input.PreflightStatus != nil {
		current.preflight = *input.PreflightStatus
	}
	if input.ResponsibilityHint != nil {
		current.responsibility = *input.ResponsibilityHint
	}
	if input.DefaultPreparationLeadDays.Specified {
		if input.DefaultPreparationLeadDays.Null {
			current.leadDays = nil
		} else {
			leadDays := input.DefaultPreparationLeadDays.Value
			current.leadDays = &leadDays
		}
	}
	if !validReadiness(current) {
		return normalizedReadiness{}, validationError("readiness invalid")
	}
	return current, nil
}

func validReadiness(value normalizedReadiness) bool {
	category := value.category == "styling" || value.category == "location" || value.category == "prop_equipment" || value.category == "other"
	requirement := value.requirement == "required" || value.requirement == "optional"
	preflight := value.preflight == "unchecked" || value.preflight == "checked"
	responsibility := value.responsibility == "photographer" || value.responsibility == "customer" || value.responsibility == "unassigned"
	validLeadDays := value.leadDays == nil || (*value.leadDays >= 0 && *value.leadDays <= 365)
	return category && requirement && preflight && responsibility && validLeadDays && runeLen(value.title) >= 1 && runeLen(value.title) <= 240
}

func (a *Application) applyRemoveReadiness(ctx context.Context, tx store.TxAccountScope, planID, readinessID string) error {
	var id string
	if err := tx.QueryRowForUpdate(ctx, "shoot_plan_readiness_items", "id", "plan_id = $2 AND id = $3 AND removed_at IS NULL", planID, readinessID).Scan(&id); errors.Is(err, store.ErrNoRows) {
		return ErrReadinessNotFound
	} else if err != nil {
		return err
	}
	if err := a.removalGuard.AssertRemovableInScope(ctx, tx, planID, readinessID); err != nil {
		return err
	}
	if _, err := tx.Delete(ctx, "shoot_plan_shot_readiness_links", "plan_id = $2 AND readiness_item_id = $3", planID, readinessID); err != nil {
		return err
	}
	_, err := tx.Update(ctx, "shoot_plan_readiness_items", "removed_at = clock_timestamp(), updated_at = clock_timestamp()", "plan_id = $2 AND id = $3", planID, readinessID)
	return err
}

func applySetPreflight(ctx context.Context, tx store.TxAccountScope, planID string, command SetPreflightCommand) error {
	if command.PreflightStatus != "checked" && command.PreflightStatus != "unchecked" {
		return validationError("preflight invalid")
	}
	updated, err := tx.Update(ctx, "shoot_plan_readiness_items", "preflight_status = $2, revision = revision + 1, updated_at = clock_timestamp()", "plan_id = $3 AND id = $4 AND removed_at IS NULL", command.PreflightStatus, planID, command.ReadinessID)
	if err != nil {
		return err
	}
	if updated != 1 {
		return ErrReadinessNotFound
	}
	return nil
}

func applyLinkReadiness(ctx context.Context, tx store.TxAccountScope, planID, shotID, readinessID string) (bool, error) {
	if err := requireCurrentShotAndReadiness(ctx, tx, planID, shotID, readinessID); err != nil {
		return false, err
	}
	row := tx.InsertOnConflictDoNothingReturning(ctx, "shoot_plan_shot_readiness_links",
		[]string{"plan_id", "shot_id", "readiness_item_id"}, []string{"account_id", "plan_id", "shot_id", "readiness_item_id"}, []string{"shot_id"}, planID, shotID, readinessID)
	var returned string
	if err := row.Scan(&returned); errors.Is(err, store.ErrNoRows) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	return true, nil
}

func applyUnlinkReadiness(ctx context.Context, tx store.TxAccountScope, planID, shotID, readinessID string) (bool, error) {
	if err := requireCurrentShotAndReadiness(ctx, tx, planID, shotID, readinessID); err != nil {
		return false, err
	}
	deleted, err := tx.Delete(ctx, "shoot_plan_shot_readiness_links", "plan_id = $2 AND shot_id = $3 AND readiness_item_id = $4", planID, shotID, readinessID)
	return deleted > 0, err
}

func requireCurrentShotAndReadiness(ctx context.Context, tx store.TxAccountScope, planID, shotID, readinessID string) error {
	shot, err := tx.Exists(ctx, "shoot_plan_shots", "plan_id = $2 AND id = $3 AND removed_at IS NULL", planID, shotID)
	if err != nil {
		return err
	}
	if !shot {
		return ErrShotNotFound
	}
	readiness, err := tx.Exists(ctx, "shoot_plan_readiness_items", "plan_id = $2 AND id = $3 AND removed_at IS NULL", planID, readinessID)
	if err != nil {
		return err
	}
	if !readiness {
		return ErrReadinessNotFound
	}
	return nil
}

func applyPublicScale(ctx context.Context, tx store.TxAccountScope, planID string, command SetPublicScaleCommand) error {
	var look, scene any
	if command.PlannedLookCount.Specified && !command.PlannedLookCount.Null {
		if command.PlannedLookCount.Value < 1 || command.PlannedLookCount.Value > 999 {
			return validationError("look count invalid")
		}
		look = command.PlannedLookCount.Value
	}
	if command.PlannedSceneCount.Specified && !command.PlannedSceneCount.Null {
		if command.PlannedSceneCount.Value < 1 || command.PlannedSceneCount.Value > 999 {
			return validationError("scene count invalid")
		}
		scene = command.PlannedSceneCount.Value
	}
	_, err := tx.Update(ctx, "shoot_plans",
		"planned_look_count = CASE WHEN $2 THEN $3::integer ELSE planned_look_count END, planned_scene_count = CASE WHEN $4 THEN $5::integer ELSE planned_scene_count END",
		"id = $6", command.PlannedLookCount.Specified, look, command.PlannedSceneCount.Specified, scene, planID)
	return err
}

func (a *Application) applyManualWindowWithCRM(
	ctx context.Context,
	tx store.TxAccountScope,
	planID string,
	expectedRevision int64,
	command PlanCommand,
) (PlanMutationResult, error) {
	crmCommand := crm.Command{Kind: crm.KindClearWindow}
	if set, ok := command.(SetExecutionWindowCommand); ok {
		if !set.EndsAt.After(set.StartsAt) || set.LiveWindowStartsAt.After(set.StartsAt) || set.LiveWindowEndsAt.Before(set.EndsAt) {
			return PlanMutationResult{}, validationError("invalid_execution_window")
		}
		if _, err := time.LoadLocation(set.Timezone); err != nil {
			return PlanMutationResult{}, validationError("invalid_execution_window")
		}
		crmCommand = crm.Command{Kind: crm.KindSetManual, Manual: &crm.Window{
			Source: "manual", StartsAt: set.StartsAt, EndsAt: set.EndsAt, Timezone: set.Timezone,
			LiveWindowStartsAt: set.LiveWindowStartsAt, LiveWindowEndsAt: set.LiveWindowEndsAt,
			RuleVersion: crm.RuleVersion,
		}}
	}
	outcome, err := a.crm.ApplyManualInScope(ctx, tx, planID, expectedRevision, crmCommand)
	if err != nil {
		return PlanMutationResult{}, mapCRMError(err)
	}
	return PlanMutationResult{
		PlanID: outcome.PlanID, Revision: outcome.Revision, Status: PlanStatus(outcome.Status),
		ChangedProjection: crmChangedProjection(outcome),
	}, nil
}

func requiredReadinessIncomplete(ctx context.Context, tx store.TxAccountScope, planID string) (bool, error) {
	count, err := tx.Count(ctx, "shoot_plan_readiness_items", "plan_id = $2 AND removed_at IS NULL AND requirement = $3 AND preflight_status = $4", planID, "required", "unchecked")
	return count > 0, err
}

func (a *Application) transitionPlanInScope(ctx context.Context, tx store.TxAccountScope, planID string, transition PlanTransition) (PlanTransitionResult, error) {
	var lockedFence planningreminder.LockedFenceTx
	var capability planningcapability.ArchiveCapabilityState
	var err error
	if transition.Kind == TransitionArchive {
		if a.archivePolicy.Capability() == planningcapability.ArchiveCapabilityReminder {
			lockedFence, err = tx.PlanningReminderFence().LockCurrentAccount(ctx)
			if err != nil {
				return PlanTransitionResult{}, err
			}
		}
		capability, err = tx.ArchiveCapability().Current(ctx)
		if err != nil {
			return PlanTransitionResult{}, err
		}
		if capability.Capability != a.archivePolicy.Capability() {
			return PlanTransitionResult{}, ErrArchiveWiringMismatch
		}
	}
	plan, err := a.repo.LockPlan(ctx, tx, planID)
	if err != nil {
		return PlanTransitionResult{}, err
	}
	if plan.Revision != transition.ExpectedRevision {
		return PlanTransitionResult{}, ErrPlanRevisionConflict
	}
	if plan.Status == PlanStatusArchived {
		return PlanTransitionResult{}, ErrArchivedReadOnly
	}

	var next PlanStatus
	switch transition.Kind {
	case TransitionMarkReady:
		incomplete, err := requiredReadinessIncomplete(ctx, tx, planID)
		if err != nil {
			return PlanTransitionResult{}, err
		}
		next, err = TransitionPlanState(plan.Status, transition.Kind, TransitionFacts{RequiredReadinessComplete: !incomplete})
		if err != nil {
			return PlanTransitionResult{}, err
		}
	case TransitionStart:
		next, err = TransitionPlanState(plan.Status, transition.Kind, TransitionFacts{})
		if err != nil {
			return PlanTransitionResult{}, err
		}
	case TransitionComplete:
		return a.completePlanInScope(ctx, tx, plan, *transition.ExpectedExecutionFactRevision)
	case TransitionReopen:
		next, err = TransitionPlanState(plan.Status, transition.Kind, TransitionFacts{})
		if err != nil {
			return PlanTransitionResult{}, err
		}
	case TransitionArchive:
		required := a.archivePolicy.RequiredAcknowledgement()
		if !exactAcknowledgement(transition.ArchiveAcknowledgement, required) {
			return PlanTransitionResult{}, &ArchiveAcknowledgementRequiredError{Required: required}
		}
		next = PlanStatusArchived
	default:
		return PlanTransitionResult{}, ErrInvalidPlanTransition
	}

	now := a.now().UTC()
	var generation int64
	if lockedFence != nil {
		generation, err = lockedFence.ReserveGeneration(ctx, planningreminder.MutationFact{PlanID: planID, MutationKind: planningreminder.MutationPlanArchived})
		if err != nil {
			return PlanTransitionResult{}, err
		}
	}
	set := "status = $2, revision = revision + 1, updated_at = $3"
	if next == PlanStatusArchived {
		set += ", archived_at = $3"
	}
	if transition.Kind == TransitionReopen {
		set += ", completed_at = NULL"
	}
	updated, err := tx.Update(ctx, "shoot_plans", set, "id = $4 AND revision = $5", string(next), now, planID, plan.Revision)
	if err != nil {
		return PlanTransitionResult{}, err
	}
	if updated != 1 {
		return PlanTransitionResult{}, ErrPlanRevisionConflict
	}
	if next == PlanStatusArchived {
		if _, err := tx.Update(ctx, "shoot_plan_run_sessions", "closed_at = $2", "plan_id = $3 AND closed_at IS NULL", now, planID); err != nil {
			return PlanTransitionResult{}, err
		}
		if lockedFence != nil {
			if err := a.archiveParticipant.OnPlanArchivedInScope(ctx, tx, planID, plan.Revision+1, generation, now); err != nil {
				return PlanTransitionResult{}, err
			}
			if err := lockedFence.MarkApplied(ctx, generation); err != nil {
				return PlanTransitionResult{}, err
			}
		}
	}
	return PlanTransitionResult{PlanID: planID, Revision: plan.Revision + 1, Status: next}, nil
}

func sameStringSet(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	leftSet := make(map[string]struct{}, len(left))
	for _, value := range left {
		leftSet[value] = struct{}{}
	}
	if len(leftSet) != len(left) {
		return false
	}
	rightSet := make(map[string]struct{}, len(right))
	for _, value := range right {
		if _, ok := leftSet[value]; !ok {
			return false
		}
		rightSet[value] = struct{}{}
	}
	return len(rightSet) == len(right)
}
