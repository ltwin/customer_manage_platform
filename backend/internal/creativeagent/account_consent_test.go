package creativeagent

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// agree records the account's agreement to the current notice and returns the
// state the client would read back.
func (f *fixture) agree(scope store.AccountScope, library bool) ConsentState {
	f.t.Helper()
	state, err := f.tryAgree(scope, policyVersion, library)
	if err != nil {
		f.t.Fatalf("agree: %v", err)
	}
	return state
}

// accountConsent agrees to the current notice and returns the consent in
// force, the way every gate will see it.
func (f *fixture) accountConsent(scope store.AccountScope, library bool) Consent {
	f.t.Helper()
	f.agree(scope, library)
	c, err := f.gate(scope, "")
	if err != nil {
		f.t.Fatalf("gate after agreeing: %v", err)
	}
	return c
}

// tryAgree agrees on what the account's state reads now, the way a client
// that has just read it would.
func (f *fixture) tryAgree(scope store.AccountScope, version string, library bool) (ConsentState, error) {
	f.t.Helper()
	seen, err := f.service.ReadAccountConsent(f.t.Context(), scope)
	if err != nil {
		f.t.Fatal(err)
	}
	return f.agreeOn(scope, seen.Revision, version, library)
}

func (f *fixture) agreeOn(scope store.AccountScope, revision, version string, library bool) (ConsentState, error) {
	f.t.Helper()
	receipt, err := f.service.GrantAccountConsent(f.t.Context(), scope,
		command(f.t, map[string]any{"policy_version": version, "library_access": library, "expected_revision": revision}))
	if err != nil {
		return ConsentState{}, err
	}
	var state ConsentState
	if err := json.Unmarshal(receipt.Outcome.Response, &state); err != nil {
		f.t.Fatal(err)
	}
	return state, nil
}

func (f *fixture) withdraw(scope store.AccountScope) ConsentState {
	f.t.Helper()
	receipt, err := f.service.RevokeAccountConsent(f.t.Context(), scope, command(f.t, map[string]any{}))
	if err != nil {
		f.t.Fatalf("withdraw: %v", err)
	}
	var state ConsentState
	if err := json.Unmarshal(receipt.Outcome.Response, &state); err != nil {
		f.t.Fatal(err)
	}
	return state
}

func (f *fixture) gate(scope store.AccountScope, admitted string) (Consent, error) {
	f.t.Helper()
	var c Consent
	err := scope.WithTxScope(f.t.Context(), func(tx store.TxAccountScope) error {
		var err error
		c, err = effectiveConsentInTx(f.t.Context(), tx, admitted)
		return err
	})
	return c, err
}

// ageConsent makes the consent read as agreed under an older notice, which is
// what every stored consent becomes when the server's notice moves on.
func (f *fixture) ageConsent(id string) {
	f.t.Helper()
	if _, err := f.db.Exec(`UPDATE creative_egress_consents SET policy_version='egress-2026-10-9' WHERE id=$1`, id); err != nil {
		f.t.Fatal(err)
	}
}

func TestAccountConsentLifecycle(t *testing.T) {
	f := setup(t)
	ctx := t.Context()

	state, err := f.service.ReadAccountConsent(ctx, f.alice)
	if err != nil {
		t.Fatal(err)
	}
	if state.Effective || state.Consent != nil || state.Revision != "0" || state.PolicyVersion != policyVersion || state.RevokedAt != nil {
		t.Fatalf("an account that never answered: %+v", state)
	}

	first := f.agree(f.alice, true)
	if !first.Effective || first.Revision != "1" || first.Consent == nil || !first.Consent.LibraryAccess ||
		first.Consent.PolicyVersion != policyVersion {
		t.Fatalf("after agreeing: %+v", first)
	}
	var mode, recipient string
	var conversation sql.NullString
	if err := f.db.QueryRow(`SELECT scope->>'mode', vendor_key, conversation_id FROM creative_egress_consents WHERE id=$1`,
		first.Consent.ID).Scan(&mode, &recipient, &conversation); err != nil {
		t.Fatal(err)
	}
	if mode != ScopeAccountLibrary || recipient != "platform" || conversation.Valid {
		t.Fatalf("account-level row: mode %q recipient %q conversation %v", mode, recipient, conversation)
	}

	// Agreeing again to what is in force writes nothing, under a new operation.
	again := f.agree(f.alice, true)
	if again.Consent.ID != first.Consent.ID || again.Revision != "1" {
		t.Fatalf("an identical agreement changed the state: %+v", again)
	}
	if n := f.count("creative_egress_consents", "account_id=$1", "agent-a"); n != 1 {
		t.Fatalf("an identical agreement wrote %d rows", n)
	}

	// Turning library search off writes a new row and stamps the old one.
	off := f.agree(f.alice, false)
	if off.Consent.ID == first.Consent.ID || off.Consent.LibraryAccess || off.Revision != "2" || !off.Effective {
		t.Fatalf("library off: %+v", off)
	}
	if n := f.count("creative_egress_consents", "id=$1 AND superseded_at IS NOT NULL AND revoked_at IS NULL", first.Consent.ID); n != 1 {
		t.Fatal("the previous consent was not stamped superseded")
	}

	revoked := f.withdraw(f.alice)
	if revoked.Effective || revoked.Consent != nil || revoked.RevokedAt == nil || revoked.Revision != "3" {
		t.Fatalf("after withdrawing: %+v", revoked)
	}
	if n := f.count("creative_egress_consents", "id=$1 AND revoked_at IS NOT NULL", off.Consent.ID); n != 1 {
		t.Fatal("the withdrawn consent was not stamped")
	}
	// Withdrawing what is already withdrawn reports the state, not an error.
	twice := f.withdraw(f.alice)
	if twice.Revision != "3" || twice.Consent != nil || twice.RevokedAt == nil {
		t.Fatalf("withdrawing twice: %+v", twice)
	}
	// Both rows stay as evidence.
	if n := f.count("creative_egress_consents", "account_id=$1", "agent-a"); n != 2 {
		t.Fatalf("evidence rows: %d", n)
	}

	back := f.agree(f.alice, true)
	if !back.Effective || back.RevokedAt != nil || back.Revision != "4" {
		t.Fatalf("agreeing after withdrawal: %+v", back)
	}
}

func TestAgreeingToAnOutdatedNoticeIsRefused(t *testing.T) {
	f := setup(t)
	if _, err := f.tryAgree(f.alice, "egress-2026-10-2", true); !errors.Is(err, ErrPolicyChanged) {
		t.Fatalf("an outdated notice must be refused, got %v", err)
	}
	if n := f.count("creative_egress_consent_states", "account_id=$1", "agent-a"); n != 0 {
		t.Fatal("a refused agreement left a state row")
	}
	for _, payload := range []map[string]any{
		{"policy_version": policyVersion, "expected_revision": "0"},
		{"library_access": true, "expected_revision": "0"},
		{"policy_version": policyVersion, "library_access": true},
		{"policy_version": policyVersion, "library_access": true, "expected_revision": "01"},
		{"policy_version": policyVersion, "library_access": true, "expected_revision": 0},
		{"policy_version": policyVersion, "library_access": true, "expected_revision": "0", "conversation_id": "ccco_x"},
	} {
		if _, err := f.service.GrantAccountConsent(t.Context(), f.alice, command(t, payload)); !errors.Is(err, creativeops.ErrValidation) {
			t.Fatalf("payload %v must be refused, got %v", payload, err)
		}
	}
	if _, err := f.service.RevokeAccountConsent(t.Context(), f.alice, command(t, map[string]any{"consent_id": "x"})); !errors.Is(err, creativeops.ErrValidation) {
		t.Fatalf("a revoke payload with fields must be refused, got %v", err)
	}
}

func TestTheSameConsentOperationReplaysItsReceipt(t *testing.T) {
	f := setup(t)
	cmd := command(t, map[string]any{"policy_version": policyVersion, "library_access": true, "expected_revision": "0"})
	first, err := f.service.GrantAccountConsent(t.Context(), f.alice, cmd)
	if err != nil {
		t.Fatal(err)
	}
	f.agree(f.alice, false)
	second, err := f.service.GrantAccountConsent(t.Context(), f.alice, cmd)
	if err != nil {
		t.Fatal(err)
	}
	if !sameJSON(t, first.Outcome.Response, second.Outcome.Response) {
		t.Fatalf("a replayed operation answered differently:\n%s\n%s", first.Outcome.Response, second.Outcome.Response)
	}
	if n := f.count("creative_egress_consents", "account_id=$1", "agent-a"); n != 2 {
		t.Fatalf("a replay wrote a row, %d rows", n)
	}

	revoke := command(t, map[string]any{})
	a, err := f.service.RevokeAccountConsent(t.Context(), f.alice, revoke)
	if err != nil {
		t.Fatal(err)
	}
	b, err := f.service.RevokeAccountConsent(t.Context(), f.alice, revoke)
	if err != nil {
		t.Fatal(err)
	}
	if !sameJSON(t, a.Outcome.Response, b.Outcome.Response) {
		t.Fatal("a replayed withdrawal answered differently")
	}
}

// sameJSON compares two receipts by value; a replay is read back from jsonb.
func sameJSON(t *testing.T, a, b json.RawMessage) bool {
	t.Helper()
	var x, y ConsentState
	if err := json.Unmarshal(a, &x); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &y); err != nil {
		t.Fatal(err)
	}
	return reflect.DeepEqual(x, y)
}

func TestAccountConsentStaysWithinItsAccount(t *testing.T) {
	f := setup(t)
	f.agree(f.alice, true)
	state, err := f.service.ReadAccountConsent(t.Context(), f.bob)
	if err != nil {
		t.Fatal(err)
	}
	if state.Effective || state.Consent != nil || state.Revision != "0" {
		t.Fatalf("another account's consent leaked: %+v", state)
	}
	if _, err := f.gate(f.bob, ""); !errors.Is(err, ErrEgressRequired) {
		t.Fatalf("an account that never agreed passed the gate: %v", err)
	}
	f.withdraw(f.bob)
	if _, err := f.gate(f.alice, ""); err != nil {
		t.Fatalf("another account's withdrawal reached this one: %v", err)
	}
}

func TestWithdrawingRetiresEverySummaryOfTheAccount(t *testing.T) {
	f := setup(t)
	f.agree(f.alice, true)
	one := f.conversation(f.alice, f.canvasID)
	two := f.conversation(f.alice, f.canvasID)
	theirs := f.conversation(f.bob, "cccv_b")
	insert := func(account, id, conversation, run string) {
		if _, err := f.db.Exec(`INSERT INTO creative_agent_compactions
			(account_id,id,conversation_id,run_id,covers_through_ordinal,summary,trigger,model_key,tokens_before,tokens_after,created_at)
			VALUES($1,$2,$3,$4,1,'摘要','auto','m',10,5,now())`, account, id, conversation, run); err != nil {
			t.Fatal(err)
		}
	}
	insert("agent-a", "cccp_00000000-0000-4000-8000-000000000001", one.ID, "ccrn_1")
	insert("agent-a", "cccp_00000000-0000-4000-8000-000000000002", two.ID, "ccrn_2")
	insert("agent-b", "cccp_00000000-0000-4000-8000-000000000003", theirs.ID, "ccrn_3")
	f.withdraw(f.alice)
	if n := f.count("creative_agent_compactions", "account_id='agent-a' AND invalidated_reason='consent_revoked'"); n != 2 {
		t.Fatalf("%d of the account's summaries retired, want 2", n)
	}
	if n := f.count("creative_agent_compactions", "account_id='agent-b' AND invalidated_at IS NULL"); n != 1 {
		t.Fatal("another account's summary was retired")
	}

	// Closing library search retires the summaries written while it was
	// open; opening it, or agreeing again with it closed, retires nothing.
	f.agree(f.alice, true)
	insert("agent-a", "cccp_00000000-0000-4000-8000-000000000004", one.ID, "ccrn_4")
	f.agree(f.alice, false)
	if n := f.count("creative_agent_compactions", "account_id='agent-a' AND invalidated_at IS NULL"); n != 0 {
		t.Fatalf("%d summaries outlived closing the library", n)
	}
	insert("agent-a", "cccp_00000000-0000-4000-8000-000000000005", one.ID, "ccrn_5")
	f.agree(f.alice, true)
	if n := f.count("creative_agent_compactions", "account_id='agent-a' AND invalidated_at IS NULL"); n != 1 {
		t.Fatal("opening the library retired a summary")
	}
}

// A window that read the state before another device withdrew cannot bring
// the consent back: the agreement is conditional on the state it was shown.
func TestAnAgreementOnAStaleStateIsRefused(t *testing.T) {
	f := setup(t)
	shown := f.agree(f.alice, true)
	f.withdraw(f.alice)
	if _, err := f.agreeOn(f.alice, shown.Revision, policyVersion, false); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("an agreement on a stale state: %v", err)
	}
	state, err := f.service.ReadAccountConsent(t.Context(), f.alice)
	if err != nil || state.Effective || state.RevokedAt == nil {
		t.Fatalf("the refused agreement changed the state: %+v %v", state, err)
	}
	if _, err := f.agreeOn(f.bob, "1", policyVersion, true); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("an account that never agreed is at revision 0: %v", err)
	}
	if n := f.count("creative_egress_consent_states", "account_id='agent-b'"); n != 0 {
		t.Fatal("a refused first agreement left a state row")
	}
	again, err := f.agreeOn(f.alice, state.Revision, policyVersion, false)
	if err != nil || !again.Effective || again.Consent.LibraryAccess {
		t.Fatalf("agreeing on the state just read: %+v %v", again, err)
	}
}

// TestTheGateFollowsTheDecisionTable pins every row of the design's table.
func TestTheGateFollowsTheDecisionTable(t *testing.T) {
	f := setup(t)

	if _, err := f.gate(f.alice, ""); !errors.Is(err, ErrEgressRequired) {
		t.Fatalf("no state row: %v", err)
	}

	old := f.agree(f.alice, true)
	f.ageConsent(old.Consent.ID)
	if _, err := f.gate(f.alice, ""); !errors.Is(err, ErrEgressRequired) {
		t.Fatalf("consent to an older notice: %v", err)
	}

	current := f.agree(f.alice, true)
	c, err := f.gate(f.alice, "")
	if err != nil || c.ID != current.Consent.ID || c.Scope.Mode != ScopeAccountLibrary || len(c.Scope.DataClasses) != len(dataClasses) {
		t.Fatalf("consent in force: %+v %v", c, err)
	}
	if _, err := f.gate(f.alice, current.Consent.ID); err != nil {
		t.Fatalf("a run admitted under the consent in force: %v", err)
	}
	if _, err := f.gate(f.alice, old.Consent.ID); !errors.Is(err, ErrEgressRequired) {
		t.Fatalf("a run admitted under an older notice: %v", err)
	}
	// Changing the library choice does not stop a run admitted before it.
	off := f.agree(f.alice, false)
	c, err = f.gate(f.alice, current.Consent.ID)
	if err != nil || c.ID != off.Consent.ID || c.Scope.Mode != ScopeSelectedRevisions {
		t.Fatalf("after turning the library off: %+v %v", c, err)
	}

	f.withdraw(f.alice)
	if _, err := f.gate(f.alice, ""); !errors.Is(err, ErrConsentRevoked) {
		t.Fatalf("withdrawn: %v", err)
	}
	if _, err := f.gate(f.alice, off.Consent.ID); !errors.Is(err, ErrConsentRevoked) {
		t.Fatalf("a run admitted before the withdrawal: %v", err)
	}
	// Agreeing again lets a run admitted under the same notice continue.
	f.agree(f.alice, true)
	if _, err := f.gate(f.alice, off.Consent.ID); err != nil {
		t.Fatalf("after agreeing again: %v", err)
	}
}

// Dispatches across an account's conversations share the state row; granting
// and revoking wait for them. Asserted with lock_timeout, not elapsed time: a
// share lock held by the gate lets another reader through at once and makes a
// writer fail its lock wait.
func TestTheGateSharesTheStateRowAndBlocksWriters(t *testing.T) {
	f := setup(t)
	f.agree(f.alice, true)
	holding, release := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- f.alice.WithTxScope(context.Background(), func(tx store.TxAccountScope) error {
			if _, err := effectiveConsentInTx(context.Background(), tx, ""); err != nil {
				return err
			}
			close(holding)
			<-release
			return nil
		})
	}()
	<-holding
	defer func() {
		close(release)
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}()

	try := func(lock string) error {
		tx, err := f.db.BeginTx(t.Context(), nil)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback() }()
		if _, err := tx.Exec(`SET LOCAL lock_timeout = '300ms'`); err != nil {
			t.Fatal(err)
		}
		_, err = tx.Exec(`SELECT 1 FROM creative_egress_consent_states WHERE account_id='agent-a' FOR ` + lock)
		return err
	}
	if err := try("SHARE"); err != nil {
		t.Fatalf("a second gate waited behind the first: %v", err)
	}
	if err := try("UPDATE"); err == nil || !strings.Contains(err.Error(), "lock timeout") {
		t.Fatalf("a writer went past a gate in flight: %v", err)
	}
}
