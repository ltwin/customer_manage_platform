package planshare

import (
	"errors"
	"fmt"
	"time"
)

var (
	ErrValidation               = errors.New("validation_failed")
	ErrNotFound                 = errors.New("not_found")
	ErrShareGenerationExists    = errors.New("share_generation_exists")
	ErrFullViewNotEligible      = errors.New("full_view_not_eligible")
	ErrExpiryOutOfRange         = errors.New("expiry_out_of_range")
	ErrExpiryQuoteExpired       = errors.New("expiry_quote_expired")
	ErrExpiryQuoteStale         = errors.New("expiry_quote_stale")
	ErrShareStale               = errors.New("share_stale")
	ErrShareInvalidState        = errors.New("share_invalid_state")
	ErrPlanRevisionConflict     = errors.New("plan_revision_conflict")
	ErrSourceChangedRetry       = errors.New("share_source_changed_retry")
	ErrFeedbackStale            = errors.New("feedback_stale")
	ErrFeedbackNotFound         = errors.New("feedback_not_found")
	ErrShotRevisionConflict     = errors.New("shot_revision_conflict")
	ErrAssignmentAlreadyClaimed = errors.New("assignment_already_claimed")
	ErrAssignmentActive         = errors.New("assignment_active")
	ErrAssignmentStale          = errors.New("assignment_stale")
	ErrAssignmentNotFound       = errors.New("assignment_not_found")
	ErrOfferNotFound            = errors.New("offer_not_found")
	ErrOfferStale               = errors.New("offer_stale")
)

type ValidationError struct {
	Message string
}

func (e ValidationError) Error() string { return e.Message }
func (e ValidationError) Unwrap() error { return ErrValidation }

func validationError(message string) error {
	return ValidationError{Message: message}
}

// ExpiryQuoteExpiredError carries a refreshed per-view policy for UI reconfirm.
type ExpiryQuoteExpiredError struct {
	Refreshed ExpiryPolicyProjectionV1
}

func (e ExpiryQuoteExpiredError) Error() string {
	return ErrExpiryQuoteExpired.Error()
}
func (e ExpiryQuoteExpiredError) Unwrap() error { return ErrExpiryQuoteExpired }

type GenerationState string

const (
	GenerationStateActive                 GenerationState = "active"
	GenerationStateRotated                GenerationState = "rotated"
	GenerationStateRevoked                GenerationState = "revoked"
	GenerationStateEligibilityInvalidated GenerationState = "eligibility_invalidated"
)

type EffectiveState string

const (
	EffectiveActive                 EffectiveState = "active"
	EffectiveExpired                EffectiveState = "expired"
	EffectiveRotated                EffectiveState = "rotated"
	EffectiveRevoked                EffectiveState = "revoked"
	EffectiveEligibilityInvalidated EffectiveState = "eligibility_invalidated"
	EffectiveArchived               EffectiveState = "archived"
)

type ExpirySourceKind string

const (
	ExpirySourceExplicit      ExpirySourceKind = "explicit"
	ExpirySourceQuotedDefault ExpirySourceKind = "quoted_default"
)

type ExpirySourceV1 struct {
	Kind         ExpirySourceKind `json:"kind"`
	DefaultQuote *DefaultQuoteV1  `json:"default_quote,omitempty"`
}

type ShareGeneration struct {
	ID                     string
	PlanID                 string
	ViewLevel              ViewLevel
	Generation             int64
	Selector               string
	SecretCommitment       ShareSecretCommitment
	Fingerprint            string
	EligibilityLinkEpochID *string
	State                  GenerationState
	ExpiresAt              time.Time
	IssuedAt               time.Time
	RotatedAt              *time.Time
	RevokedAt              *time.Time
	InvalidatedAt          *time.Time
	FirstOpenedAt          *time.Time
	Revision               int64
}

type LatestGenerationProjectionV1 struct {
	ShareID        string         `json:"share_id"`
	Generation     int64          `json:"generation"`
	Fingerprint    string         `json:"fingerprint"`
	EffectiveState EffectiveState `json:"effective_state"`
	EndedReason    *string        `json:"ended_reason,omitempty"`
	IssuedAt       time.Time      `json:"issued_at"`
	ExpiresAt      time.Time      `json:"expires_at"`
	EndedAt        *time.Time     `json:"ended_at,omitempty"`
	FirstOpenedAt  *time.Time     `json:"first_opened_at,omitempty"`
	Revision       int64          `json:"revision"`
}

type ShareViewProjectionV1 struct {
	ViewLevel        ViewLevel                     `json:"view_level"`
	ExpiryPolicy     ExpiryPolicyProjectionV1      `json:"expiry_policy"`
	LatestGeneration *LatestGenerationProjectionV1 `json:"latest_generation,omitempty"`
}

type OnSiteOfferProjectionV1 struct {
	OfferID            string     `json:"offer_id"`
	AssignmentKind     string     `json:"assignment_kind"`
	Content            string     `json:"content"`
	State              string     `json:"state"`
	Revision           int64      `json:"revision"`
	CreatedAt          time.Time  `json:"created_at"`
	ClosedAt           *time.Time `json:"closed_at,omitempty"`
	ActiveAssignmentID *string    `json:"active_assignment_id,omitempty"`
}

type ShareManagementProjectionV1 struct {
	ShareViews       []ShareViewProjectionV1   `json:"share_views"`
	OnSiteOffers     []OnSiteOfferProjectionV1 `json:"on_site_offers"`
	OffersNextCursor *string                   `json:"offers_next_cursor,omitempty"`
}

type ShareIssueResultV1 struct {
	ShareID    string          `json:"share_id"`
	Selector   string          `json:"selector"`
	Generation int64           `json:"generation"`
	ViewLevel  ViewLevel       `json:"view_level"`
	State      GenerationState `json:"state"`
	ExpiresAt  time.Time       `json:"expires_at"`
	Revision   int64           `json:"revision"`
}

type ShareRevokeResultV1 struct {
	ShareID   string          `json:"share_id"`
	State     GenerationState `json:"state"`
	Revision  int64           `json:"revision"`
	RevokedAt time.Time       `json:"revoked_at"`
}

type FeedbackTargetKind string

const (
	FeedbackTargetPlan FeedbackTargetKind = "plan"
	FeedbackTargetShot FeedbackTargetKind = "shot"
)

type FeedbackDisposition string

const (
	FeedbackDispositionPending FeedbackDisposition = "pending"
	FeedbackDispositionAdopted FeedbackDisposition = "adopted"
	FeedbackDispositionIgnored FeedbackDisposition = "ignored"
)

type DeepLinkKind string

const (
	DeepLinkFeedbackSection DeepLinkKind = "feedback_section"
	DeepLinkShot            DeepLinkKind = "shot"
)

type FeedbackDeepLinkTargetV1 struct {
	Kind   DeepLinkKind `json:"kind"`
	ShotID *string      `json:"shot_id,omitempty"`
}

type FeedbackTargetV1 struct {
	Kind   FeedbackTargetKind `json:"kind"`
	ShotID *string            `json:"shot_id,omitempty"`
}

type FeedbackManagementItemV1 struct {
	FeedbackID        string                   `json:"feedback_id"`
	Target            FeedbackTargetV1         `json:"target"`
	AuthorDisplayName string                   `json:"author_display_name"`
	Content           string                   `json:"content"`
	Disposition       FeedbackDisposition      `json:"disposition"`
	Revision          int64                    `json:"revision"`
	CreatedAt         time.Time                `json:"created_at"`
	DispositionAt     *time.Time               `json:"disposition_at,omitempty"`
	DeepLinkTarget    FeedbackDeepLinkTargetV1 `json:"deep_link_target"`
}

type FeedbackManagementPageV1 struct {
	Items      []FeedbackManagementItemV1 `json:"items"`
	NextCursor *string                    `json:"next_cursor,omitempty"`
}

type FeedbackCreateResultV1 struct {
	FeedbackID string             `json:"feedback_id"`
	TargetKind FeedbackTargetKind `json:"target_kind"`
	TargetRef  *string            `json:"target_ref,omitempty"`
	Revision   int64              `json:"revision"`
	CreatedAt  time.Time          `json:"created_at"`
}

type FeedbackDispositionResultV1 struct {
	FeedbackID     string                   `json:"feedback_id"`
	Disposition    FeedbackDisposition      `json:"disposition"`
	Revision       int64                    `json:"revision"`
	DeepLinkTarget FeedbackDeepLinkTargetV1 `json:"deep_link_target"`
}

type AssignmentKind string

const (
	AssignmentKindReadiness     AssignmentKind = "readiness"
	AssignmentKindOnSiteSupport AssignmentKind = "on_site_support"
)

type AssignmentStatus string

const (
	AssignmentStatusActive  AssignmentStatus = "active"
	AssignmentStatusRevoked AssignmentStatus = "revoked"
)

type AssignmentRevokedBy string

const (
	AssignmentRevokedByAnonymous    AssignmentRevokedBy = "anonymous"
	AssignmentRevokedByPhotographer AssignmentRevokedBy = "photographer"
)

type AssignmentTargetKind string

const (
	AssignmentTargetReadiness     AssignmentTargetKind = "readiness"
	AssignmentTargetOnSiteSupport AssignmentTargetKind = "on_site_support"
)

type AssignmentDeepLinkKind string

const (
	AssignmentDeepLinkReadiness AssignmentDeepLinkKind = "readiness"
	AssignmentDeepLinkOffer     AssignmentDeepLinkKind = "offer"
)

type AssignmentTargetV1 struct {
	Kind            AssignmentTargetKind `json:"kind"`
	ReadinessItemID *string              `json:"readiness_item_id,omitempty"`
	OfferID         *string              `json:"offer_id,omitempty"`
}

type AssignmentDeepLinkTargetV1 struct {
	Kind            AssignmentDeepLinkKind `json:"kind"`
	ReadinessItemID *string                `json:"readiness_item_id,omitempty"`
	OfferID         *string                `json:"offer_id,omitempty"`
}

type AssignmentManagementItemV1 struct {
	AssignmentID                string                     `json:"assignment_id"`
	AssignmentKind              AssignmentKind             `json:"assignment_kind"`
	Target                      AssignmentTargetV1         `json:"target"`
	ContentSnapshot             string                     `json:"content_snapshot"`
	ClaimedByDisplayName        string                     `json:"claimed_by_display_name"`
	PreparationLeadDaysSnapshot *int                       `json:"preparation_lead_days_snapshot,omitempty"`
	LeadRuleVersion             *string                    `json:"lead_rule_version,omitempty"`
	Status                      AssignmentStatus           `json:"status"`
	Revision                    int64                      `json:"revision"`
	ClaimedAt                   time.Time                  `json:"claimed_at"`
	RevokedAt                   *time.Time                 `json:"revoked_at,omitempty"`
	RevokedBy                   *AssignmentRevokedBy       `json:"revoked_by,omitempty"`
	DeepLinkTarget              AssignmentDeepLinkTargetV1 `json:"deep_link_target"`
}

type AssignmentManagementPageV1 struct {
	Items      []AssignmentManagementItemV1 `json:"items"`
	NextCursor *string                      `json:"next_cursor,omitempty"`
}

type OfferMutationResultV1 struct {
	OfferID  string `json:"offer_id"`
	State    string `json:"state"`
	Revision int64  `json:"revision"`
}

type AssignmentClaimResultV1 struct {
	AssignmentID   string           `json:"assignment_id"`
	AssignmentKind AssignmentKind   `json:"assignment_kind"`
	TargetRef      string           `json:"target_ref"`
	Status         AssignmentStatus `json:"status"`
	Revision       int64            `json:"revision"`
	ClaimedAt      time.Time        `json:"claimed_at"`
}

type AssignmentMutationResultV1 struct {
	AssignmentID string           `json:"assignment_id"`
	Status       AssignmentStatus `json:"status"`
	Revision     int64            `json:"revision"`
	RevokedAt    time.Time        `json:"revoked_at"`
}

func DeriveEffectiveState(gen ShareGeneration, archived bool, now time.Time) (EffectiveState, *string, *time.Time) {
	if archived {
		reason := "plan_archived"
		endedAt := gen.IssuedAt
		if gen.RevokedAt != nil {
			endedAt = *gen.RevokedAt
		} else if gen.RotatedAt != nil {
			endedAt = *gen.RotatedAt
		} else if gen.InvalidatedAt != nil {
			endedAt = *gen.InvalidatedAt
		}
		return EffectiveArchived, &reason, &endedAt
	}
	switch gen.State {
	case GenerationStateRotated:
		reason := "rotated"
		return EffectiveRotated, &reason, gen.RotatedAt
	case GenerationStateRevoked:
		reason := "revoked"
		return EffectiveRevoked, &reason, gen.RevokedAt
	case GenerationStateEligibilityInvalidated:
		reason := "eligibility_invalidated"
		return EffectiveEligibilityInvalidated, &reason, gen.InvalidatedAt
	case GenerationStateActive:
		if !gen.ExpiresAt.After(now.UTC()) {
			reason := "expired"
			ended := gen.ExpiresAt
			return EffectiveExpired, &reason, &ended
		}
		return EffectiveActive, nil, nil
	default:
		reason := fmt.Sprintf("unknown_state_%s", gen.State)
		return EffectiveRevoked, &reason, nil
	}
}
