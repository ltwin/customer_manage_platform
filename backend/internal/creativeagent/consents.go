package creativeagent

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/creativecontent"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// Consent modes. selected_revisions authorises exactly the revisions listed at
// grant time (an empty set permits no library content); account_library authorises whatever the assistant's own search
// finds inside the personal library, which is the only way a search result can
// ever be shown to a model.
const (
	ScopeSelectedRevisions = "selected_revisions"
	ScopeAccountLibrary    = "account_library"
)

// Data classes describe what kind of bytes may leave, not which resources.
var dataClasses = []string{"text", "image_preview", "media_metadata"}

// maxApprovedRevisions bounds one grant. A photographer approving more than
// this is selecting a library, and should say so with account_library.
const maxApprovedRevisions = 200

type ConsentScope struct {
	Mode        string   `json:"mode"`
	DataClasses []string `json:"data_classes"`
}

type Consent struct {
	ID             string               `json:"id"`
	VendorKey      string               `json:"vendor_key"`
	Purpose        string               `json:"purpose"`
	ConversationID string               `json:"conversation_id,omitempty"`
	Scope          ConsentScope         `json:"scope"`
	PolicyVersion  string               `json:"policy_version"`
	Revision       creativeops.Revision `json:"revision"`
	GrantedAt      time.Time            `json:"granted_at"`
	RevokedAt      *time.Time           `json:"revoked_at,omitempty"`
	// ApprovedRevisions is the evidence summary returned at grant time. It is a
	// count, not the list: the list lives in its own table and is read there.
	ApprovedRevisions int `json:"approved_revisions"`
}

type grantConsent struct {
	ConversationID      string   `json:"conversation_id"`
	VendorKey           string   `json:"vendor_key"`
	Purpose             string   `json:"purpose"`
	Mode                string   `json:"mode"`
	DataClasses         []string `json:"data_classes"`
	SelectedRevisionIDs []string `json:"selected_revision_ids"`
}

// GrantConsent records the photographer's authorisation for content to leave
// the account. No separate AI-analysis permission was introduced, so this
// record is what authorises sending. Each listed revision is still resolved
// through RequireUsable, which enforces more than ownership — see the four
// checks listed in docs/dev/creative-agent.md.
func (s *Service) GrantConsent(ctx context.Context, scope store.AccountScope, command creativeops.Command) (creativeops.Receipt, error) {
	var input grantConsent
	return (creativeops.Executor{}).Run(ctx, scope, creativeops.Operation{
		Key: "agent.grant_egress_consent", Capability: "agent_start",
		Validate: func(raw json.RawMessage) error {
			if err := creativeops.Decode(raw, &input); err != nil {
				return err
			}
			if input.ConversationID == "" || input.Purpose != "creative_assistance" {
				return creativeops.ErrValidation
			}
			if !slices.Contains(s.models.Vendors(), input.VendorKey) {
				return creativeops.ErrValidation
			}
			if len(input.DataClasses) == 0 {
				return creativeops.ErrValidation
			}
			for i, class := range input.DataClasses {
				if !slices.Contains(dataClasses, class) || slices.Contains(input.DataClasses[:i], class) {
					return creativeops.ErrValidation
				}
			}
			switch input.Mode {
			case ScopeSelectedRevisions:
				if len(input.SelectedRevisionIDs) > maxApprovedRevisions {
					return creativeops.ErrValidation
				}
				for i, id := range input.SelectedRevisionIDs {
					if id == "" || slices.Contains(input.SelectedRevisionIDs[:i], id) {
						return creativeops.ErrValidation
					}
				}
			case ScopeAccountLibrary:
				// A library-wide grant names no resource, by definition.
				if len(input.SelectedRevisionIDs) != 0 {
					return creativeops.ErrValidation
				}
			default:
				return creativeops.ErrValidation
			}
			return nil
		},
		Apply: func(ctx context.Context, tx store.TxAccountScope, _ json.RawMessage) (creativeops.Outcome, error) {
			exists, err := tx.Exists(ctx, "creative_agent_conversations", "id=$2", input.ConversationID)
			if err != nil {
				return creativeops.Outcome{}, err
			}
			if !exists {
				return creativeops.Outcome{}, ErrNotFound
			}
			now, err := tx.CreativeNow(ctx)
			if err != nil {
				return creativeops.Outcome{}, err
			}
			id, err := creativeops.NewResourceID("ccec")
			if err != nil {
				return creativeops.Outcome{}, err
			}
			// Resolve every revision before the consent row exists. The global
			// lock order puts content ahead of egress consent, and doing it this
			// way also makes "a refused grant leaves no consent" structural
			// rather than something that depends on the rollback.
			for _, revisionID := range input.SelectedRevisionIDs {
				// The photographer's own display permission. This is not a bare
				// ownership check: besides same-account, ready state and a live
				// retention root, it runs the rights matrix for
				// moodboard_display, requires an unrevoked display grant, and
				// requires every upstream declaration of a derived revision to
				// be granted too.
				if _, err := creativecontent.RequireUsable(ctx, tx, revisionID, "display"); err != nil {
					if errors.Is(err, creativecontent.ErrNotFound) || errors.Is(err, creativecontent.ErrMissingRoot) {
						return creativeops.Outcome{}, ErrNotFound
					}
					return creativeops.Outcome{}, err
				}
			}
			consentScope := ConsentScope{Mode: input.Mode, DataClasses: input.DataClasses}
			encoded, err := json.Marshal(consentScope)
			if err != nil {
				return creativeops.Outcome{}, err
			}
			if err := tx.Insert(ctx, "creative_egress_consents",
				[]string{"id", "vendor_key", "purpose", "conversation_id", "scope", "policy_version", "granted_at", "created_at", "updated_at"},
				id, input.VendorKey, input.Purpose, input.ConversationID, encoded, policyVersion, now, now, now); err != nil {
				return creativeops.Outcome{}, err
			}
			for _, revisionID := range input.SelectedRevisionIDs {
				if err := tx.Insert(ctx, "creative_egress_consent_contents",
					[]string{"consent_id", "content_revision_id", "approved_at"}, id, revisionID, now); err != nil {
					return creativeops.Outcome{}, err
				}
			}
			consent := Consent{ID: id, VendorKey: input.VendorKey, Purpose: input.Purpose,
				ConversationID: input.ConversationID, Scope: consentScope, PolicyVersion: policyVersion,
				Revision: 1, GrantedAt: now, ApprovedRevisions: len(input.SelectedRevisionIDs)}
			response, err := json.Marshal(consent)
			if err != nil {
				return creativeops.Outcome{}, err
			}
			revision := consent.Revision
			return creativeops.Outcome{HTTPStatus: 201, Response: response, ResultKind: "agent_egress_consent", ResultID: &id, ResultRevision: &revision}, nil
		},
	}, command)
}

type revokeConsent struct {
	ConsentID        string               `json:"consent_id"`
	ExpectedRevision creativeops.Revision `json:"expected_revision"`
}

// RevokeConsent stops further sending. It makes no claim about content already
// delivered to the vendor, and the approval record is kept as evidence of what
// was authorised while it was in force.
func (s *Service) RevokeConsent(ctx context.Context, scope store.AccountScope, command creativeops.Command) (creativeops.Receipt, error) {
	var input revokeConsent
	return (creativeops.Executor{}).Run(ctx, scope, creativeops.Operation{
		Key: "agent.revoke_egress_consent", Capability: "agent_start",
		Validate: func(raw json.RawMessage) error {
			if err := creativeops.Decode(raw, &input); err != nil {
				return err
			}
			if input.ConsentID == "" || input.ExpectedRevision < 1 {
				return creativeops.ErrValidation
			}
			return nil
		},
		Apply: func(ctx context.Context, tx store.TxAccountScope, _ json.RawMessage) (creativeops.Outcome, error) {
			consent, err := readConsentForUpdate(ctx, tx, input.ConsentID)
			if err != nil {
				return creativeops.Outcome{}, err
			}
			// Already-withdrawn is reported before the revision check: a caller
			// retrying with the revision they just used deserves "already
			// revoked", not "someone else modified this".
			if consent.RevokedAt != nil {
				return creativeops.Outcome{}, ErrConsentRevoked
			}
			if consent.Revision != input.ExpectedRevision {
				return creativeops.Outcome{}, ErrRevisionConflict
			}
			now, err := tx.CreativeNow(ctx)
			if err != nil {
				return creativeops.Outcome{}, err
			}
			if _, err := tx.Update(ctx, "creative_egress_consents",
				"revoked_at=$3, revision=revision+1, updated_at=$3", "id=$2 AND revision=$4",
				input.ConsentID, now, int64(input.ExpectedRevision)); err != nil {
				return creativeops.Outcome{}, err
			}
			consent.RevokedAt, consent.Revision = &now, consent.Revision+1
			response, err := json.Marshal(consent)
			if err != nil {
				return creativeops.Outcome{}, err
			}
			revision := consent.Revision
			return creativeops.Outcome{HTTPStatus: 200, Response: response, ResultKind: "agent_egress_consent", ResultID: &input.ConsentID, ResultRevision: &revision}, nil
		},
	}, command)
}

func readConsentForUpdate(ctx context.Context, tx store.TxAccountScope, id string) (Consent, error) {
	var c Consent
	var revision int64
	var conversationID *string
	var encodedScope []byte
	err := tx.QueryRowForUpdate(ctx, "creative_egress_consents",
		"id, vendor_key, purpose, conversation_id, scope, policy_version, revision, granted_at, revoked_at", "id=$2", id).
		Scan(&c.ID, &c.VendorKey, &c.Purpose, &conversationID, &encodedScope, &c.PolicyVersion, &revision, &c.GrantedAt, &c.RevokedAt)
	if errors.Is(err, store.ErrNoRows) {
		return Consent{}, ErrNotFound
	}
	if err != nil {
		return Consent{}, err
	}
	if err := json.Unmarshal(encodedScope, &c.Scope); err != nil {
		return Consent{}, err
	}
	c.Revision = creativeops.Revision(revision)
	if conversationID != nil {
		c.ConversationID = *conversationID
	}
	count, err := tx.Count(ctx, "creative_egress_consent_contents", "consent_id=$2", id)
	if err != nil {
		return Consent{}, err
	}
	c.ApprovedRevisions = int(count)
	return c, nil
}
