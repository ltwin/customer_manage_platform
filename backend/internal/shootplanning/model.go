// Package shootplanning owns the independent creative shoot plan aggregate.
package shootplanning

import (
	"bytes"
	"encoding/json"
	"errors"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/planningmedia"
)

type PlanStatus string

const (
	PlanStatusDraft      PlanStatus = "draft"
	PlanStatusReady      PlanStatus = "ready"
	PlanStatusInProgress PlanStatus = "in_progress"
	PlanStatusCompleted  PlanStatus = "completed"
	PlanStatusArchived   PlanStatus = "archived"
)

type ShotResult string

const (
	ShotResultCaptured ShotResult = "captured"
	ShotResultSkipped  ShotResult = "skipped"
	ShotResultCleared  ShotResult = "cleared"
)

type CaptureMode string

const (
	CaptureModeLive     CaptureMode = "live"
	CaptureModeBackfill CaptureMode = "backfill"
	CaptureModeUnknown  CaptureMode = "unknown"
)

type CreativeBrief struct {
	WorkTitle      *string  `json:"work_title,omitempty"`
	CharacterName  *string  `json:"character_name,omitempty"`
	ThemeStatement *string  `json:"theme_statement,omitempty"`
	Mood           *string  `json:"mood,omitempty"`
	VisualKeywords []string `json:"visual_keywords,omitempty"`
}

type PublicPlanScale struct {
	PlannedLookCount  *int `json:"planned_look_count"`
	PlannedSceneCount *int `json:"planned_scene_count"`
	PlannedShotCount  int  `json:"planned_shot_count"`
}

type PlanExecutionWindow struct {
	Source             string    `json:"source"`
	SourceRef          *string   `json:"source_ref"`
	StartsAt           time.Time `json:"starts_at"`
	EndsAt             time.Time `json:"ends_at"`
	Timezone           string    `json:"timezone"`
	LiveWindowStartsAt time.Time `json:"live_window_starts_at"`
	LiveWindowEndsAt   time.Time `json:"live_window_ends_at"`
	RuleVersion        int       `json:"rule_version"`
	Revision           int64     `json:"revision"`
}

type ShootPlan struct {
	ID                    string               `json:"id"`
	Title                 string               `json:"title"`
	Subject               string               `json:"subject"`
	Status                PlanStatus           `json:"status"`
	CreativeBrief         CreativeBrief        `json:"creative_brief"`
	PublicScale           PublicPlanScale      `json:"public_scale"`
	ExecutionWindow       *PlanExecutionWindow `json:"execution_window"`
	Revision              int64                `json:"revision"`
	ExecutionFactRevision int64                `json:"execution_fact_revision"`
	CreatedAt             time.Time            `json:"created_at"`
	UpdatedAt             time.Time            `json:"updated_at"`
	CompletedAt           *time.Time           `json:"completed_at"`
	ArchivedAt            *time.Time           `json:"archived_at"`
}

type Shot struct {
	ID                   string                         `json:"id"`
	PlanID               string                         `json:"plan_id"`
	Position             int                            `json:"position"`
	Title                string                         `json:"title"`
	Scene                *string                        `json:"scene"`
	Action               *string                        `json:"action"`
	Expression           *string                        `json:"expression"`
	Composition          *string                        `json:"composition"`
	Lighting             *string                        `json:"lighting_text"`
	Notes                *string                        `json:"notes"`
	FramingTag           *string                        `json:"framing_tag"`
	LightingDirectionTag *string                        `json:"lighting_direction_tag"`
	LightingQualityTag   *string                        `json:"lighting_quality_tag"`
	PaletteTag           *string                        `json:"palette_tag"`
	ShotTypeTag          *string                        `json:"shot_type_tag"`
	TaxonomyVersion      int                            `json:"taxonomy_version"`
	Revision             int64                          `json:"revision"`
	ExecutionRevision    int64                          `json:"execution_revision"`
	NextEventSequence    int64                          `json:"-"`
	ReadinessItemIDs     []string                       `json:"readiness_item_ids"`
	CurrentOutcome       *CurrentOutcome                `json:"current_outcome"`
	AssetAccessRefs      []planningmedia.AssetAccessRef `json:"asset_access_refs"`
	RemovedAt            *time.Time                     `json:"-"`
}

type ReadinessItem struct {
	ID                         string     `json:"id"`
	PlanID                     string     `json:"plan_id"`
	Category                   string     `json:"category"`
	Title                      string     `json:"title"`
	Requirement                string     `json:"requirement"`
	PreflightStatus            string     `json:"preflight_status"`
	ResponsibilityHint         string     `json:"responsibility_hint"`
	DefaultPreparationLeadDays *int       `json:"default_preparation_lead_days"`
	Revision                   int64      `json:"revision"`
	RemovedAt                  *time.Time `json:"-"`
}

type ExecutionFactKind string

const (
	ExecutionFactResult ExecutionFactKind = "result"
	ExecutionFactVoid   ExecutionFactKind = "void"
)

// ExecutionFact is the replay input shared by result and void events. Sequence is
// allocated by the server per shot and is the sole ordering authority.
type ExecutionFact struct {
	Kind              ExecutionFactKind `json:"kind"`
	ID                string            `json:"id"`
	PlanID            string            `json:"plan_id"`
	ShotID            string            `json:"shot_id"`
	SessionID         *string           `json:"session_id,omitempty"`
	Sequence          int64             `json:"shot_event_seq"`
	PlanRevision      int64             `json:"plan_revision,omitempty"`
	Result            ShotResult        `json:"result,omitempty"`
	SkipReason        *string           `json:"skip_reason,omitempty"`
	Notes             *string           `json:"notes,omitempty"`
	CheckedAt         time.Time         `json:"checked_at,omitempty"`
	CaptureMode       CaptureMode       `json:"capture_mode,omitempty"`
	SupersedesEventID *string           `json:"supersedes_event_id,omitempty"`
	TargetEventID     string            `json:"target_event_id,omitempty"`
	Reason            string            `json:"reason,omitempty"`
	VoidedAt          time.Time         `json:"voided_at,omitempty"`
	Revision          int64             `json:"revision"`
}

func (fact ExecutionFact) MarshalJSON() ([]byte, error) {
	switch fact.Kind {
	case ExecutionFactResult:
		body := map[string]any{
			"kind": fact.Kind, "id": fact.ID, "plan_id": fact.PlanID, "shot_id": fact.ShotID,
			"shot_event_seq": fact.Sequence, "plan_revision": fact.PlanRevision, "result": fact.Result,
			"checked_at": fact.CheckedAt, "capture_mode": fact.CaptureMode, "revision": fact.Revision,
		}
		if fact.SessionID != nil {
			body["session_id"] = *fact.SessionID
		}
		if fact.SkipReason != nil {
			body["skip_reason"] = *fact.SkipReason
		}
		if fact.Notes != nil {
			body["notes"] = *fact.Notes
		}
		if fact.SupersedesEventID != nil {
			body["supersedes_event_id"] = *fact.SupersedesEventID
		}
		return json.Marshal(body)
	case ExecutionFactVoid:
		return json.Marshal(map[string]any{
			"kind": fact.Kind, "id": fact.ID, "plan_id": fact.PlanID, "shot_id": fact.ShotID,
			"target_event_id": fact.TargetEventID, "shot_event_seq": fact.Sequence, "reason": fact.Reason,
			"voided_at": fact.VoidedAt, "revision": fact.Revision,
		})
	default:
		return nil, errors.New("cannot marshal unknown execution fact kind")
	}
}

type CurrentOutcome struct {
	Set         bool        `json:"-"`
	EventID     string      `json:"event_id"`
	Result      ShotResult  `json:"result"`
	SkipReason  *string     `json:"skip_reason,omitempty"`
	CheckedAt   time.Time   `json:"checked_at"`
	CaptureMode CaptureMode `json:"capture_mode"`
}

// Optional distinguishes an omitted patch field from explicit null and value.
type Optional[T any] struct {
	Specified bool
	Null      bool
	Value     T
}

func (optional *Optional[T]) UnmarshalJSON(data []byte) error {
	optional.Specified = true
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		optional.Null = true
		var zero T
		optional.Value = zero
		return nil
	}
	optional.Null = false
	return json.Unmarshal(data, &optional.Value)
}

// PlanCommand is a closed application union. Each concrete type carries one
// discriminator and therefore cannot represent a multi-operation partial write.
type PlanCommand interface {
	planCommand()
	operation() string
}

type CreativeBriefPatch struct {
	WorkTitle      Optional[string]
	CharacterName  Optional[string]
	ThemeStatement Optional[string]
	Mood           Optional[string]
	VisualKeywords Optional[[]string]
}

type UpdateBriefCommand struct {
	Title         *string
	Subject       *string
	CreativeBrief *CreativeBriefPatch
}

type ShotWrite struct {
	Title                *string
	Scene                Optional[string]
	Action               Optional[string]
	Expression           Optional[string]
	Composition          Optional[string]
	Lighting             Optional[string]
	Notes                Optional[string]
	FramingTag           Optional[string]
	LightingDirectionTag Optional[string]
	LightingQualityTag   Optional[string]
	PaletteTag           Optional[string]
	ShotTypeTag          Optional[string]
}

type UpsertShotCommand struct {
	ShotID            *string
	Shot              ShotWrite
	InsertAfterShotID *string
}
type ReorderShotsCommand struct{ OrderedShotIDs []string }
type RemoveShotCommand struct {
	ShotID                      string
	AcknowledgeExecutionHistory bool
}

type ReadinessWrite struct {
	Category                   *string
	Title                      *string
	Requirement                *string
	PreflightStatus            *string
	ResponsibilityHint         *string
	DefaultPreparationLeadDays Optional[int]
}
type UpsertReadinessCommand struct {
	ReadinessID *string
	Item        ReadinessWrite
}
type RemoveReadinessCommand struct{ ReadinessID string }
type SetPreflightCommand struct {
	ReadinessID     string
	PreflightStatus string
}
type LinkReadinessCommand struct {
	ShotID      string
	ReadinessID string
}
type UnlinkReadinessCommand struct {
	ShotID      string
	ReadinessID string
}
type SetPublicScaleCommand struct {
	PlannedLookCount  Optional[int]
	PlannedSceneCount Optional[int]
}
type SetExecutionWindowCommand struct {
	StartsAt           time.Time
	EndsAt             time.Time
	Timezone           string
	LiveWindowStartsAt time.Time
	LiveWindowEndsAt   time.Time
}
type ClearExecutionWindowCommand struct{}

func (UpdateBriefCommand) planCommand()          {}
func (UpsertShotCommand) planCommand()           {}
func (ReorderShotsCommand) planCommand()         {}
func (RemoveShotCommand) planCommand()           {}
func (UpsertReadinessCommand) planCommand()      {}
func (RemoveReadinessCommand) planCommand()      {}
func (SetPreflightCommand) planCommand()         {}
func (LinkReadinessCommand) planCommand()        {}
func (UnlinkReadinessCommand) planCommand()      {}
func (SetPublicScaleCommand) planCommand()       {}
func (SetExecutionWindowCommand) planCommand()   {}
func (ClearExecutionWindowCommand) planCommand() {}

func (UpdateBriefCommand) operation() string          { return "update_brief" }
func (UpsertShotCommand) operation() string           { return "upsert_shot" }
func (ReorderShotsCommand) operation() string         { return "reorder_shots" }
func (RemoveShotCommand) operation() string           { return "remove_shot" }
func (UpsertReadinessCommand) operation() string      { return "upsert_readiness" }
func (RemoveReadinessCommand) operation() string      { return "remove_readiness" }
func (SetPreflightCommand) operation() string         { return "set_preflight" }
func (LinkReadinessCommand) operation() string        { return "link_readiness" }
func (UnlinkReadinessCommand) operation() string      { return "unlink_readiness" }
func (SetPublicScaleCommand) operation() string       { return "set_public_scale" }
func (SetExecutionWindowCommand) operation() string   { return "set_execution_window" }
func (ClearExecutionWindowCommand) operation() string { return "clear_execution_window" }
