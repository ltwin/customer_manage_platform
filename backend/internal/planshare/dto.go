package planshare

import "time"

// ViewLevel is immutable for one ShareGeneration.
type ViewLevel string

const (
	ViewLevelProposal ViewLevel = "proposal"
	ViewLevelFull     ViewLevel = "full"
)

// SharedCreativeBriefV1 is the anonymous-safe brief projection allowlist.
type SharedCreativeBriefV1 struct {
	WorkTitle      *string  `json:"work_title,omitempty"`
	CharacterName  *string  `json:"character_name,omitempty"`
	ThemeStatement *string  `json:"theme_statement,omitempty"`
	Mood           *string  `json:"mood,omitempty"`
	VisualKeywords []string `json:"visual_keywords,omitempty"`
}

// SharedPublicWindowV1 is derived from the execution window only.
type SharedPublicWindowV1 struct {
	StartsAt        time.Time `json:"starts_at"`
	EndsAt          time.Time `json:"ends_at"`
	Timezone        string    `json:"timezone"`
	DurationMinutes int       `json:"duration_minutes"`
}

// SharedPublicScaleV1 mirrors core PublicPlanScale with nullable look/scene.
type SharedPublicScaleV1 struct {
	PlannedShotCount  int  `json:"planned_shot_count"`
	PlannedLookCount  *int `json:"planned_look_count"`
	PlannedSceneCount *int `json:"planned_scene_count"`
}

// SharedMoodboardItemV1 exposes only anonymous media refs plus a
// photographer-authored caption and a rights-derived usage note.
type SharedMoodboardItemV1 struct {
	Ref       string `json:"ref"`
	Checksum  string `json:"checksum"`
	Caption   string `json:"caption,omitempty"`
	UsageNote string `json:"usage_note,omitempty"`
}

// moodboardUsageNotes keeps the anonymous-view wording aligned with the
// frontend mediaRights mirror; basis is bijective to source class (matrix.go).
var moodboardUsageNotes = map[string]string{
	"official":           "官方资料 · 引用作风格参考",
	"anime_screenshot":   "动画截图 · 引用作风格参考",
	"setting_book":       "设定集资料 · 引用作风格参考",
	"fan":                "同人作品 · 引用作风格参考",
	"unknown_web":        "网络来源 · 引用作风格参考",
	"photographer_owned": "摄影师创作 · 用作风格沟通",
	"licensed":           "已获许可素材 · 用于风格沟通",
	"customer_supplied":  "客户提供 · 已获展示同意",
}

func moodboardUsageNote(sourceClass string) string {
	if note, ok := moodboardUsageNotes[sourceClass]; ok {
		return note
	}
	return "引用作风格参考"
}

// SharedPlanProposalV1 is the exact proposal anonymous DTO skeleton.
type SharedPlanProposalV1 struct {
	ViewLevel          ViewLevel               `json:"view_level"`
	Title              string                  `json:"title"`
	CreativeBrief      SharedCreativeBriefV1   `json:"creative_brief"`
	PublicWindow       *SharedPublicWindowV1   `json:"public_window,omitempty"`
	PublicScale        SharedPublicScaleV1     `json:"public_scale"`
	Moodboard          []SharedMoodboardItemV1 `json:"moodboard"`
	ProjectionRevision int64                   `json:"projection_revision"`
}

// SharedShotV1 is the full-only shot allowlist (no notes/private fields).
type SharedShotV1 struct {
	ID                   string  `json:"id"`
	Position             int     `json:"position"`
	Title                string  `json:"title"`
	Scene                *string `json:"scene,omitempty"`
	Action               *string `json:"action,omitempty"`
	Expression           *string `json:"expression,omitempty"`
	Composition          *string `json:"composition,omitempty"`
	LightingText         *string `json:"lighting_text,omitempty"`
	FramingTag           *string `json:"framing_tag,omitempty"`
	LightingDirectionTag *string `json:"lighting_direction_tag,omitempty"`
	LightingQualityTag   *string `json:"lighting_quality_tag,omitempty"`
	PaletteTag           *string `json:"palette_tag,omitempty"`
	ShotTypeTag          *string `json:"shot_type_tag,omitempty"`
	Revision             int64   `json:"revision"`
}

// SharedActiveAssignmentV1 is the anonymous active assignment summary.
type SharedActiveAssignmentV1 struct {
	ID                   string `json:"id"`
	ClaimedByDisplayName string `json:"claimed_by_display_name"`
	Revision             int64  `json:"revision"`
}

// SharedAssignmentOpportunityV1 is a full-only claim opportunity.
type SharedAssignmentOpportunityV1 struct {
	OfferID                    string                    `json:"offer_id"`
	AssignmentKind             string                    `json:"assignment_kind"`
	ReadinessItemID            *string                   `json:"readiness_item_id,omitempty"`
	Content                    string                    `json:"content"`
	PreparationLeadDaysPreview *int                      `json:"preparation_lead_days_preview,omitempty"`
	TargetRevision             int64                     `json:"target_revision"`
	ActiveAssignment           *SharedActiveAssignmentV1 `json:"active_assignment,omitempty"`
}

// SharedPlanFullV1 extends proposal with full-only fields.
type SharedPlanFullV1 struct {
	SharedPlanProposalV1
	Shots                   []SharedShotV1                  `json:"shots"`
	AssignmentOpportunities []SharedAssignmentOpportunityV1 `json:"assignment_opportunities"`
}
