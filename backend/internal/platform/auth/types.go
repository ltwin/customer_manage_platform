package auth

import "time"

type AccountStatus string

const (
	AccountPendingVerification AccountStatus = "pending_verification"
	AccountActive              AccountStatus = "active"
	AccountLegacyUnclaimed     AccountStatus = "legacy_unclaimed"
)

type RegistrationAdmissionMode string

const (
	RegistrationPublic                RegistrationAdmissionMode = "public"
	RegistrationBootstrapFirstAccount RegistrationAdmissionMode = "bootstrap_first_account"
)

type ActionPurpose string

const (
	ActionEmailVerification ActionPurpose = "email_verification"
	ActionLegacyClaim       ActionPurpose = "legacy_claim"
)

type Account struct {
	ID        string
	Email     string
	Status    AccountStatus
	CreatedAt time.Time
}

type BootstrapPlan struct {
	Eligible     bool
	RecipientRef string
}

type DeliveryFailureClass string

const (
	DeliveryCancelled              DeliveryFailureClass = "cancelled"
	DeliveryTimeout                DeliveryFailureClass = "timeout"
	DeliveryProviderRejected       DeliveryFailureClass = "provider_rejected"
	DeliveryTemporarilyUnavailable DeliveryFailureClass = "temporarily_unavailable"
	DeliveryMisconfigured          DeliveryFailureClass = "misconfigured"
)

type DeliveryOutcome struct {
	Attempted         bool
	Accepted          bool
	FailureClass      DeliveryFailureClass
	ProviderMessageID string
	AcceptedAt        time.Time
}

type DispatchResult struct {
	Attempted bool
	Delivery  DeliveryOutcome
}

type Session struct {
	AccountID         string
	AccessToken       string
	AccessExpiresAt   time.Time
	RefreshToken      string
	RefreshExpiresAt  time.Time
	RefreshAbsoluteAt time.Time
	RefreshSessionID  string
	RefreshGeneration string
}

type LoginRecord struct {
	Account      Account
	PasswordHash string
}

type RegistrationRecord struct {
	AccountID        string
	IdentityID       string
	NormalizedEmail  string
	PasswordHash     string
	ActionSelector   string
	ActionSecretHash []byte
	ActionExpiresAt  time.Time
	CreatedAt        time.Time
}

type ActionProof struct {
	Selector   string
	SecretHash []byte
	Purpose    ActionPurpose
}

type RefreshSeed struct {
	FamilyID          string
	GenerationID      string
	WireToken         string
	TokenHash         []byte
	IdleExpiresAt     time.Time
	AbsoluteExpiresAt time.Time
	ReplayUntil       time.Time
	CreatedAt         time.Time
}

type RefreshProof struct {
	GenerationID string
	TokenHash    []byte
}

type RefreshRotation struct {
	AccountID         string
	FamilyID          string
	GenerationID      string
	WireToken         string
	IdleExpiresAt     time.Time
	AbsoluteExpiresAt time.Time
}

type LegacyClaimState string

const (
	LegacyClaimReady            LegacyClaimState = "ready"
	LegacyClaimPendingSameEmail LegacyClaimState = "pending_same_email"
	LegacyClaimAlreadyClaimed   LegacyClaimState = "already_claimed"
	LegacyClaimConflict         LegacyClaimState = "conflict"
	LegacyClaimNoTarget         LegacyClaimState = "no_target"
)

type LegacyClaimResult struct {
	DryRun            bool
	State             LegacyClaimState
	AccountIDRedacted string
	EmailRedacted     string
	LegacyCount       int
	PendingClaimCount int
	Delivery          DeliveryOutcome
}

type LegacyAuthState struct {
	LegacyUnclaimedCount int
	PendingClaimCount    int
	ActiveClaimedCount   int
}

// LegacyClaimRecord carries the repository's transaction result back to the
// account-auth service. Raw identifiers never leave the application boundary.
type LegacyClaimRecord struct {
	State             LegacyClaimState
	AccountID         string
	LegacyCount       int
	PendingClaimCount int
}

type LegacyClaimCommand struct {
	NormalizedEmail  string
	IdentityID       string
	ActionSelector   string
	ActionSecretHash []byte
	ActionExpiresAt  time.Time
	CreatedAt        time.Time
}
