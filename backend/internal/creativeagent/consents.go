package creativeagent

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strconv"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// Consent modes. On an account-level consent, account_library lets the
// assistant search the personal library and selected_revisions does not.
// Per-conversation rows written before account-level consent used
// selected_revisions with a list of revisions; those rows are history now and
// take no part in any decision.
const (
	ScopeSelectedRevisions = "selected_revisions"
	ScopeAccountLibrary    = "account_library"
)

// Data classes describe what kind of bytes may leave, not which resources.
var dataClasses = []string{"text", "image_preview", "media_metadata", "canvas_metadata"}

type ConsentScope struct {
	Mode        string   `json:"mode"`
	DataClasses []string `json:"data_classes"`
}

type Consent struct {
	ID             string               `json:"id"`
	Purpose        string               `json:"purpose"`
	ConversationID string               `json:"conversation_id,omitempty"`
	Scope          ConsentScope         `json:"scope"`
	PolicyVersion  string               `json:"policy_version"`
	Revision       creativeops.Revision `json:"revision"`
	GrantedAt      time.Time            `json:"granted_at"`
	RevokedAt      *time.Time           `json:"revoked_at,omitempty"`
}

// ErrPolicyChanged refuses a consent to a notice that is no longer the current
// one: the photographer agreed to text the server no longer shows.
var ErrPolicyChanged = errors.New("creative egress notice changed")

// AccountConsent is the account's answer to the pre-send notice.
type AccountConsent struct {
	ID            string    `json:"id"`
	PolicyVersion string    `json:"policy_version"`
	LibraryAccess bool      `json:"library_access"`
	GrantedAt     time.Time `json:"granted_at"`
}

// ConsentState is what the client reads to decide whether to show the notice.
// Revision is a string because an account that never answered has none ("0"),
// which creativeops.Revision cannot encode.
type ConsentState struct {
	PolicyVersion string          `json:"policy_version"`
	Effective     bool            `json:"effective"`
	Revision      string          `json:"revision"`
	Consent       *AccountConsent `json:"consent"`
	RevokedAt     *time.Time      `json:"revoked_at,omitempty"`
}

// rowScanner is the one method the state reader needs from a row.
type rowScanner interface {
	Scan(dest ...any) error
}

// accountScope is the fixed scope of every account-level consent row.
func accountScope(libraryAccess bool) ConsentScope {
	mode := ScopeSelectedRevisions
	if libraryAccess {
		mode = ScopeAccountLibrary
	}
	return ConsentScope{Mode: mode, DataClasses: slices.Clone(dataClasses)}
}

// readConsentRow reads one consent row unlocked. Its content never changes
// once written, so the lock that matters is the one on the state row.
func readConsentRow(ctx context.Context, q store.TxAccountScope, id string) (Consent, error) {
	var c Consent
	var revision int64
	var conversationID *string
	var encodedScope []byte
	err := q.QueryRow(ctx, "creative_egress_consents",
		"id, purpose, conversation_id, scope, policy_version, revision, granted_at, revoked_at", "id=$2", id).
		Scan(&c.ID, &c.Purpose, &conversationID, &encodedScope, &c.PolicyVersion, &revision, &c.GrantedAt, &c.RevokedAt)
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
	return c, nil
}

type consentStateRow struct {
	current     *string
	lastRevoked *time.Time
	revision    int64
}

// consentState builds the client view from a state row (nil: never answered).
func consentState(ctx context.Context, q store.TxAccountScope, row *consentStateRow) (ConsentState, error) {
	state := ConsentState{PolicyVersion: policyVersion, Revision: "0"}
	if row == nil {
		return state, nil
	}
	state.Revision = strconv.FormatInt(row.revision, 10)
	if row.current == nil {
		state.RevokedAt = row.lastRevoked
		return state, nil
	}
	c, err := readConsentRow(ctx, q, *row.current)
	if err != nil {
		return ConsentState{}, err
	}
	state.Consent = &AccountConsent{ID: c.ID, PolicyVersion: c.PolicyVersion,
		LibraryAccess: c.Scope.Mode == ScopeAccountLibrary, GrantedAt: c.GrantedAt}
	state.Effective = c.PolicyVersion == policyVersion
	return state, nil
}

func scanConsentState(row rowScanner) (*consentStateRow, error) {
	var st consentStateRow
	err := row.Scan(&st.current, &st.lastRevoked, &st.revision)
	if errors.Is(err, store.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &st, nil
}

const consentStateColumns = "current_consent_id, last_revoked_at, revision"

// ReadAccountConsent returns the account's consent as the client sees it.
func (s *Service) ReadAccountConsent(ctx context.Context, scope store.AccountScope) (ConsentState, error) {
	var state ConsentState
	err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		if err := tx.RequireCreativeCapability(ctx, "creative_read"); err != nil {
			return err
		}
		row, err := scanConsentState(tx.QueryRow(ctx, "creative_egress_consent_states", consentStateColumns, ""))
		if err != nil {
			return err
		}
		state, err = consentState(ctx, tx, row)
		return err
	})
	return state, err
}

// effectiveConsentInTx is the egress gate. It returns the account's consent in
// force, or the reason there is none. admitted is the consent a run was
// admitted under ("" for a run not created yet); the run continues only while
// that admission was under the current notice too, so a run started under an
// older notice stops at its next dispatch whatever the account has done since.
//
// The state row is read FOR SHARE: dispatches across all of one account's
// conversations hold it together, while granting and revoking take it FOR
// UPDATE and so wait for every dispatch in flight — whichever commits first
// wins. It sits where the per-conversation consent lock used to: after the
// conversation and run, before content.
func effectiveConsentInTx(ctx context.Context, tx store.TxAccountScope, admitted string) (Consent, error) {
	row, err := scanConsentState(tx.QueryRowForShare(ctx, "creative_egress_consent_states", consentStateColumns, ""))
	if err != nil {
		return Consent{}, err
	}
	switch {
	case row == nil:
		return Consent{}, ErrEgressRequired
	case row.current == nil && row.lastRevoked != nil:
		return Consent{}, ErrConsentRevoked
	case row.current == nil:
		return Consent{}, ErrEgressRequired
	}
	c, err := readConsentRow(ctx, tx, *row.current)
	if err != nil {
		return Consent{}, err
	}
	if c.PolicyVersion != policyVersion {
		return Consent{}, ErrEgressRequired
	}
	if admitted != "" && admitted != c.ID {
		was, err := readConsentRow(ctx, tx, admitted)
		if errors.Is(err, ErrNotFound) {
			return Consent{}, ErrEgressRequired
		}
		if err != nil {
			return Consent{}, err
		}
		if was.PolicyVersion != policyVersion {
			return Consent{}, ErrEgressRequired
		}
	}
	return c, nil
}

// lockConsentStateInTx takes the account's state row FOR UPDATE, creating it
// first when create is set. A racing creator's row is simply the one locked.
func lockConsentStateInTx(ctx context.Context, tx store.TxAccountScope, create bool) (row *consentStateRow, created bool, err error) {
	if create {
		var account string
		err := tx.InsertOnConflictDoNothingReturning(ctx, "creative_egress_consent_states",
			[]string{"revision"}, []string{"account_id"}, []string{"account_id"}, int64(1)).Scan(&account)
		switch {
		case err == nil:
			created = true
		case !errors.Is(err, store.ErrNoRows):
			return nil, false, err
		}
	}
	row, err = scanConsentState(tx.QueryRowForUpdate(ctx, "creative_egress_consent_states", consentStateColumns, ""))
	return row, created, err
}

func consentStateOutcome(state ConsentState, revision int64) (creativeops.Outcome, error) {
	response, err := json.Marshal(state)
	if err != nil {
		return creativeops.Outcome{}, err
	}
	var id *string
	if state.Consent != nil {
		id = &state.Consent.ID
	}
	var resultRevision *creativeops.Revision
	if revision > 0 {
		r := creativeops.Revision(revision)
		resultRevision = &r
	}
	return creativeops.Outcome{HTTPStatus: 200, Response: response, ResultKind: "agent_egress_consent",
		ResultID: id, ResultRevision: resultRevision}, nil
}

type grantAccountConsent struct {
	PolicyVersion    string  `json:"policy_version"`
	LibraryAccess    *bool   `json:"library_access"`
	ExpectedRevision *string `json:"expected_revision"`
}

// GrantAccountConsent records the photographer's agreement to the current
// pre-send notice, or a change of the library choice. Agreeing again to what
// is already in force writes nothing.
func (s *Service) GrantAccountConsent(ctx context.Context, scope store.AccountScope, command creativeops.Command) (creativeops.Receipt, error) {
	var input grantAccountConsent
	return (creativeops.Executor{}).Run(ctx, scope, creativeops.Operation{
		Key: "agent.grant_account_egress_consent", Capability: "agent_start",
		Validate: func(raw json.RawMessage) error {
			if err := creativeops.Decode(raw, &input); err != nil {
				return err
			}
			if input.PolicyVersion == "" || len(input.PolicyVersion) > 64 || input.LibraryAccess == nil || input.ExpectedRevision == nil {
				return creativeops.ErrValidation
			}
			if n, err := strconv.ParseInt(*input.ExpectedRevision, 10, 64); err != nil || n < 0 || strconv.FormatInt(n, 10) != *input.ExpectedRevision {
				return creativeops.ErrValidation
			}
			return nil
		},
		Apply: func(ctx context.Context, tx store.TxAccountScope, _ json.RawMessage) (creativeops.Outcome, error) {
			if input.PolicyVersion != policyVersion {
				return creativeops.Outcome{}, ErrPolicyChanged
			}
			row, created, err := lockConsentStateInTx(ctx, tx, true)
			if err != nil {
				return creativeops.Outcome{}, err
			}
			if row == nil {
				return creativeops.Outcome{}, store.ErrNoRows
			}
			// Conditional on the state the photographer was shown: a window
			// opened before another device withdrew must not bring the
			// consent back by flipping the library switch.
			seen := row.revision
			if created {
				seen = 0
			}
			if strconv.FormatInt(seen, 10) != *input.ExpectedRevision {
				return creativeops.Outcome{}, ErrRevisionConflict
			}
			state, err := consentState(ctx, tx, row)
			if err != nil {
				return creativeops.Outcome{}, err
			}
			if state.Effective && state.Consent.LibraryAccess == *input.LibraryAccess {
				return consentStateOutcome(state, row.revision)
			}
			now, err := tx.CreativeNow(ctx)
			if err != nil {
				return creativeops.Outcome{}, err
			}
			// Summaries written while library search was open may quote what
			// it found; closing it retires them, as a withdrawal does.
			if state.Consent != nil && state.Consent.LibraryAccess && !*input.LibraryAccess {
				if err := retireCompactionsInTx(ctx, tx, now); err != nil {
					return creativeops.Outcome{}, err
				}
			}
			// The old row is stamped before the new one exists: the partial
			// unique index allows one unstamped account-level row at a time.
			if row.current != nil {
				if _, err := tx.Update(ctx, "creative_egress_consents",
					"superseded_at=$3, updated_at=$3", "id=$2", *row.current, now); err != nil {
					return creativeops.Outcome{}, err
				}
			}
			id, err := creativeops.NewResourceID("ccec")
			if err != nil {
				return creativeops.Outcome{}, err
			}
			encoded, err := json.Marshal(accountScope(*input.LibraryAccess))
			if err != nil {
				return creativeops.Outcome{}, err
			}
			if err := tx.Insert(ctx, "creative_egress_consents",
				[]string{"id", "vendor_key", "purpose", "scope", "policy_version", "granted_at", "created_at", "updated_at"},
				id, consentRecipient, "creative_assistance", encoded, policyVersion, now, now, now); err != nil {
				return creativeops.Outcome{}, err
			}
			// A row created just now already counts as revision 1.
			bump := "revision+1"
			if created {
				bump = "revision"
			}
			if _, err := tx.Update(ctx, "creative_egress_consent_states",
				"current_consent_id=$2, revision="+bump+", updated_at=$3", "", id, now); err != nil {
				return creativeops.Outcome{}, err
			}
			row.current = &id
			if !created {
				row.revision++
			}
			state, err = consentState(ctx, tx, row)
			if err != nil {
				return creativeops.Outcome{}, err
			}
			return consentStateOutcome(state, row.revision)
		},
	}, command)
}

// RevokeAccountConsent withdraws the account's consent. Its effect is exactly
// that of the notice changing: no new run starts, a run in progress stops at
// its next dispatch, and the next send asks again. Content already delivered
// cannot be recalled, and the consent row stays as evidence.
func (s *Service) RevokeAccountConsent(ctx context.Context, scope store.AccountScope, command creativeops.Command) (creativeops.Receipt, error) {
	return (creativeops.Executor{}).Run(ctx, scope, creativeops.Operation{
		Key: "agent.revoke_account_egress_consent", Capability: "agent_start",
		Validate: func(raw json.RawMessage) error {
			var empty struct{}
			return creativeops.Decode(raw, &empty)
		},
		Apply: func(ctx context.Context, tx store.TxAccountScope, _ json.RawMessage) (creativeops.Outcome, error) {
			row, _, err := lockConsentStateInTx(ctx, tx, false)
			if err != nil {
				return creativeops.Outcome{}, err
			}
			if row == nil || row.current == nil {
				state, err := consentState(ctx, tx, row)
				if err != nil {
					return creativeops.Outcome{}, err
				}
				var revision int64
				if row != nil {
					revision = row.revision
				}
				return consentStateOutcome(state, revision)
			}
			now, err := tx.CreativeNow(ctx)
			if err != nil {
				return creativeops.Outcome{}, err
			}
			if _, err := tx.Update(ctx, "creative_egress_consents",
				"revoked_at=$3, updated_at=$3", "id=$2", *row.current, now); err != nil {
				return creativeops.Outcome{}, err
			}
			if _, err := tx.Update(ctx, "creative_egress_consent_states",
				"current_consent_id=NULL, last_revoked_at=$2, revision=revision+1, updated_at=$2", "", now); err != nil {
				return creativeops.Outcome{}, err
			}
			// A summary was written from what the withdrawn consent let leave;
			// none is carried into any later run of this account.
			if err := retireCompactionsInTx(ctx, tx, now); err != nil {
				return creativeops.Outcome{}, err
			}
			row.current, row.lastRevoked, row.revision = nil, &now, row.revision+1
			state, err := consentState(ctx, tx, row)
			if err != nil {
				return creativeops.Outcome{}, err
			}
			return consentStateOutcome(state, row.revision)
		},
	}, command)
}

// retireCompactionsInTx invalidates every live summary of the account. The
// reason stays consent_revoked for closing the library too: what is withdrawn
// is the part of the consent the summary was written under.
func retireCompactionsInTx(ctx context.Context, tx store.TxAccountScope, now time.Time) error {
	_, err := tx.Update(ctx, "creative_agent_compactions",
		"invalidated_at=$2, invalidated_reason='consent_revoked'", "invalidated_at IS NULL", now)
	return err
}

// readScopeInTx is the consent that opens or closes the library branch of a
// read made outside the gate: the account's current consent, or none. It does
// not judge whether the run may go on — the gate before the next dispatch
// does — so a read in between never fails on consent alone.
func readScopeInTx(ctx context.Context, tx store.TxAccountScope) (Consent, error) {
	row, err := scanConsentState(tx.QueryRowForShare(ctx, "creative_egress_consent_states", consentStateColumns, ""))
	if err != nil || row == nil || row.current == nil {
		return Consent{}, err
	}
	return readConsentRow(ctx, tx, *row.current)
}
