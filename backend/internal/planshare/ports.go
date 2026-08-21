package planshare

import (
	"context"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/planningmedia"
	"github.com/samson/customer-manage-platform/backend/internal/platform/txcap"
	"github.com/samson/customer-manage-platform/backend/internal/shootplanning/planningreminder"
)

// ShareTokenResolver validates a presented token and returns sealed capability
// types only. Implementations must never construct store.AccountScope.
type ShareTokenResolver interface {
	Resolve(
		ctx context.Context,
		presentedToken string,
	) (txcap.ValidatedShareContext, txcap.ShareTransactionCapability, error)
}

// ShareTxScope is the planshare-owned callback view inside one physical share
// transaction. Every method is a typed participant; AccountScope / raw SQL are
// intentionally absent.
type ShareTxScope interface {
	PlanningReminderFence() planningreminder.FenceTxView
	Generations() ShareGenerationStore
	Feedback() ShareFeedbackStore
	Assignments() ShareAssignmentStore
	SharedAssetRefs() SharedAssetRefStore
	Observations() ShareObservationStore
	AssignmentSourceEvents() ShareAssignmentSourceEventStore
	ReplayAdmissions() ShareReplayAdmissionStore
	PlanSources() PlanShareSourceReader
	Eligibility() ShareEligibilityReader
	Media() ShareMediaReader
}

// ShareGenerationStore is the typed generation participant.
type ShareGenerationStore interface {
	LoadByValidatedContext(ctx context.Context) (ShareGeneration, error)
	TouchFirstOpenedAt(ctx context.Context, now time.Time) error
	LatchEligibilityInvalidated(ctx context.Context, now time.Time) error
	shareGenerationStoreSeal()
}

// ShareFeedbackStore is the typed feedback participant.
type ShareFeedbackStore interface {
	Insert(ctx context.Context, row ShareFeedbackInsert) (ShareFeedbackRow, error)
	LockByID(ctx context.Context, planID, feedbackID string) (ShareFeedbackRow, error)
	UpdateDisposition(
		ctx context.Context,
		planID, feedbackID string,
		expectedRevision int64,
		disposition FeedbackDisposition,
		now time.Time,
	) (ShareFeedbackRow, error)
	shareFeedbackStoreSeal()
}

// ShareFeedbackInsert is the create payload inside a share transaction.
type ShareFeedbackInsert struct {
	ID                string
	PlanID            string
	TokenGenerationID string
	TargetKind        FeedbackTargetKind
	TargetID          *string
	TargetRevision    int64
	AuthorDisplayName string
	Content           string
	DeepLinkKind      DeepLinkKind
	DeepLinkShotID    *string
	CreatedAt         time.Time
}

// ShareFeedbackRow is the persisted feedback fact.
type ShareFeedbackRow struct {
	ID                string
	PlanID            string
	TokenGenerationID string
	TargetKind        FeedbackTargetKind
	TargetID          *string
	TargetRevision    int64
	AuthorDisplayName string
	Content           string
	Disposition       FeedbackDisposition
	Revision          int64
	DeepLinkKind      DeepLinkKind
	DeepLinkShotID    *string
	CreatedAt         time.Time
	DispositionAt     *time.Time
}

// FeedbackObservationInput records a de-contented feedback observation.
type FeedbackObservationInput struct {
	PlanID            string
	TokenGenerationID string
	Kind              string
	SourceFactID      string
	PolicyVersion     string
	OccurredAt        time.Time
	SlotID            *string
	WindowRevision    *int64
}

// ShareObservationStore is the typed observation participant.
type ShareObservationStore interface {
	EnsureFullOpen(ctx context.Context, input FullOpenObservationInput) error
	EnsureFeedback(ctx context.Context, input FeedbackObservationInput) error
	EnsureAssignment(ctx context.Context, input AssignmentObservationInput) error
	shareObservationStoreSeal()
}

// AssignmentObservationInput records a de-contented assignment observation.
type AssignmentObservationInput struct {
	PlanID             string
	TokenGenerationID  string
	Kind               string
	SourceFactID       string
	SourceFactRevision *int64
	PolicyVersion      string
	OccurredAt         time.Time
	SlotID             *string
	WindowRevision     *int64
}

// ShareAssignmentSourceEventStore is the typed source-event participant.
type ShareAssignmentSourceEventStore interface {
	Insert(ctx context.Context, row ShareAssignmentSourceEventInsert) error
	shareAssignmentSourceEventStoreSeal()
}

// ShareAssignmentSourceEventInsert is the versioned outbox fact.
type ShareAssignmentSourceEventInsert struct {
	EventID                     string
	PlanID                      string
	AssignmentID                string
	AssignmentRevision          int64
	AccountSourceGeneration     int64
	EventKind                   string
	AssignmentKind              AssignmentKind
	ReadinessItemID             *string
	PreparationLeadDaysSnapshot *int
	LeadRuleVersion             *string
	ContentFingerprint          string
	OccurredAt                  time.Time
}

// ShareReplayAdmissionStore records exact-frame admission only from the current
// ledger claim inside the first callback.
type ShareReplayAdmissionStore interface {
	RecordForCurrentLedgerClaim(ctx context.Context, frame CanonicalAnonymousMutationFrameV1) error
	HasExactUnexpired(
		ctx context.Context,
		tokenGenerationID string,
		operation string,
		keyDigest []byte,
		fingerprint []byte,
		now time.Time,
	) (bool, error)
}

// CanonicalAnonymousMutationFrameV1 is the unique anonymous mutation frame.
type CanonicalAnonymousMutationFrameV1 struct {
	Operation             string
	FrameHash             string
	IdempotencyKey        string
	TokenGenerationID     string
	ExactFrameFingerprint []byte
}

// SharedAssetRefUpsert creates or reuses a generation-bound moodboard ref.
type SharedAssetRefUpsert struct {
	PlanID            string
	TokenGenerationID string
	Binding           MoodboardBindingRef
}

// SharedAssetRefRow is the generation-bound moodboard access ref used by content.
type SharedAssetRefRow struct {
	PlanID            string
	TokenGenerationID string
	BindingID         string
	AssetID           string
	ExactGeneration   int
	DisplayChecksum   string
	Ref               string
}

// SharedAssetRefStore is the typed anonymous media-ref participant.
type SharedAssetRefStore interface {
	UpsertMoodboardRef(ctx context.Context, input SharedAssetRefUpsert) (SharedMoodboardItemV1, error)
	LoadActiveByRef(ctx context.Context, tokenGenerationID, ref string) (SharedAssetRefRow, error)
	sharedAssetRefStoreSeal()
}

// FullOpenObservationInput records the first successful full GET per generation.
type FullOpenObservationInput struct {
	PlanID                  string
	TokenGenerationID       string
	SlotID                  *string
	ExecutionWindowRevision *int64
	PolicyVersion           string
	OccurredAt              time.Time
}

// ShareAssignmentStore is the typed assignment participant.
type ShareAssignmentStore interface {
	Insert(ctx context.Context, row ShareAssignmentInsert) (ShareAssignmentRow, error)
	LockByID(ctx context.Context, planID, assignmentID string) (ShareAssignmentRow, error)
	LockActiveByTarget(
		ctx context.Context,
		planID string,
		kind AssignmentKind,
		readinessItemID, offerID *string,
	) (ShareAssignmentRow, error)
	FindActiveByTarget(
		ctx context.Context,
		planID string,
		kind AssignmentKind,
		readinessItemID, offerID *string,
	) (ShareAssignmentRow, error)
	RevokeCAS(
		ctx context.Context,
		planID, assignmentID string,
		expectedRevision int64,
		revokedBy AssignmentRevokedBy,
		now time.Time,
	) (ShareAssignmentRow, error)
	ExistsActiveReadiness(ctx context.Context, planID, readinessItemID string) (bool, error)
	shareAssignmentStoreSeal()
}

// ShareAssignmentInsert is the claim payload inside a share/bearer transaction.
type ShareAssignmentInsert struct {
	ID                          string
	PlanID                      string
	TokenGenerationID           string
	AssignmentKind              AssignmentKind
	ReadinessItemID             *string
	OfferID                     *string
	ContentSnapshot             string
	ClaimedByDisplayName        string
	PreparationLeadDaysSnapshot *int
	LeadRuleVersion             *string
	ClaimReceiptCommitment      ClaimReceiptCommitment
	ClaimedAt                   time.Time
}

// ShareAssignmentRow is the persisted assignment fact.
type ShareAssignmentRow struct {
	ID                          string
	PlanID                      string
	TokenGenerationID           string
	AssignmentKind              AssignmentKind
	ReadinessItemID             *string
	OfferID                     *string
	ContentSnapshot             string
	ClaimedByDisplayName        string
	PreparationLeadDaysSnapshot *int
	LeadRuleVersion             *string
	Status                      AssignmentStatus
	ClaimReceiptCommitment      ClaimReceiptCommitment
	Revision                    int64
	ClaimedAt                   time.Time
	RevokedAt                   *time.Time
	RevokedBy                   *AssignmentRevokedBy
}

// ProposalSource is the core/CRM safe projection for proposal view.
// It must not contain shots, readiness, or business facts.
type ProposalSource struct {
	PlanID             string
	Title              string
	CreativeBrief      SharedCreativeBriefV1
	PublicWindow       *SharedPublicWindowV1
	PublicScale        SharedPublicScaleV1
	ProjectionRevision int64
	Archived           bool
}

// FullSource extends proposal source with current shot and readiness safety fields.
type FullSource struct {
	ProposalSource
	Shots     []SharedShotV1
	Readiness []ReadinessOpportunitySource
}

// ReadinessOpportunitySource is the readiness-derived offer input.
type ReadinessOpportunitySource struct {
	ReadinessItemID            string
	Content                    string
	TargetRevision             int64
	PreparationLeadDaysPreview *int
}

// ShareEligibilityHint is a non-locking pre-read identity for CRM lock order.
type ShareEligibilityHint struct {
	PlanID      string
	LinkEpochID string
}

// ShareEligibility is the live eligibility projection without price fields.
type ShareEligibility struct {
	LinkEpochID    string
	Eligible       bool
	SlotID         *string
	WindowRevision *int64
	ObservedAt     time.Time
}

// MoodboardBindingRef identifies an active moodboard_display binding.
type MoodboardBindingRef struct {
	BindingID       string
	AssetID         string
	ExactGeneration int
	DisplayChecksum string
	Caption         string
	UsageNote       string
}

// SharedDisplayPermitRequest asks planningmedia for a display ContentPermit.
type SharedDisplayPermitRequest struct {
	PlanID          string
	BindingID       string
	AssetID         string
	ExactGeneration int
	DisplayChecksum string
}

// PlanShareSourceReader is the typed core source port.
type PlanShareSourceReader interface {
	ReadProposalSourceInShare(ctx context.Context, planID string) (ProposalSource, error)
	ReadFullSourceInShare(ctx context.Context, planID string) (FullSource, error)
}

// ShareEligibilityReader is the typed CRM eligibility port.
type ShareEligibilityReader interface {
	PreReadShareEligibility(ctx context.Context, grant txcap.ValidatedShareContext) (ShareEligibilityHint, error)
	LockAndRecheckShareEligibilityInShare(ctx context.Context, hint ShareEligibilityHint) (ShareEligibility, error)
}

// ShareMediaReader is the typed planningmedia port for anonymous moodboard.
type ShareMediaReader interface {
	ListMoodboardBindingsInShare(ctx context.Context, planID string) ([]MoodboardBindingRef, error)
	IssueDisplayPermitInShare(ctx context.Context, req SharedDisplayPermitRequest) (planningmedia.ContentPermit, error)
}
