package creativeagent

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/samson/customer-manage-platform/backend/internal/creativecontent"
	"github.com/samson/customer-manage-platform/backend/internal/creativeskill"
	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/llmgateway"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store/storetest"
	"github.com/samson/customer-manage-platform/backend/internal/platform/versionedfs"
)

func TestMain(m *testing.M) { storetest.Main(m, store.MigrateUp) }

type fixture struct {
	t  *testing.T
	db *sql.DB
	st *store.Store
	// skills is the real skill store behind the assistant, published into by
	// the real import protocol. A stub would exercise the projection but not
	// the seam between the two packages, which is the part that is new.
	skills *creativeskill.Service
	// vendor stands in for the company that receives the bytes. Everything
	// between this fixture and it — admission, the hold, the dispatch intent,
	// the persisted complete result, the accounting — is the real gateway.
	vendor   *stubVendor
	service  *Service
	alice    store.AccountScope
	bob      store.AccountScope
	platform store.AccountScope
	canvasID string
}

// platformPublisher is a third account, so that neither test account is the one
// publishing: both read the platform catalog as ordinary accounts do.
const platformPublisher = "agent-platform"

func setup(t *testing.T) *fixture {
	t.Helper()
	url := storetest.NewURL(t)
	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`INSERT INTO accounts(id,password_hash,status) VALUES ('agent-a','test','active'),('agent-b','test','active'),('agent-platform','test','active');
		INSERT INTO creative_projects(id,account_id,name) VALUES ('ccpj_a','agent-a','项目'),('ccpj_b','agent-b','项目');
		INSERT INTO creative_canvases(id,account_id,project_id,name) VALUES ('cccv_a','agent-a','ccpj_a','画布'),('cccv_b','agent-b','ccpj_b','画布')`); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(t.Context(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)
	// A credential is supplied so the deployment's model is actually enabled.
	// Without one the catalog lists it as unavailable, and every run test would
	// pass for the wrong reason — refused before anything was exercised.
	models, err := llmgateway.DefaultCatalog(func(string) (string, bool) { return "test-credential", true })
	if err != nil {
		t.Fatal(err)
	}
	vendor := &stubVendor{}
	gateway, err := llmgateway.New(llmgateway.Config{
		Catalog:       models,
		Providers:     map[llmgateway.ProviderKey]llmgateway.Provider{llmgateway.ProviderOpenAICompatible: vendor},
		Credential:    func(string) (string, bool) { return "test-credential", true },
		DefaultBudget: llmgateway.BudgetPolicy{MonthlyLimitMicros: 20_000_000, MonthlyTokenLimit: 5_000_000},
	})
	if err != nil {
		t.Fatal(err)
	}
	// Skill resources never arrive as browser uploads, so the local adapter's
	// part signer is unreachable through the port this domain holds.
	objects, err := versionedfs.NewLocal(t.TempDir(),
		func(string, string, int, time.Time) versionedfs.PartAuthorization {
			return versionedfs.PartAuthorization{}
		})
	if err != nil {
		t.Fatal(err)
	}
	platform := st.ScopeFor(auth.AccountContext{AccountID: platformPublisher})
	skills, err := creativeskill.NewService(objects, platform)
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewService(gateway, models, skills)
	if err != nil {
		t.Fatal(err)
	}
	return &fixture{t: t, db: db, st: st, skills: skills, vendor: vendor, service: service, platform: platform,
		alice:    st.ScopeFor(auth.AccountContext{AccountID: "agent-a"}),
		bob:      st.ScopeFor(auth.AccountContext{AccountID: "agent-b"}),
		canvasID: "cccv_a"}
}

func command(t *testing.T, payload any) creativeops.Command {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	return creativeops.Command{OperationID: uuid.NewString(), CreatedAt: time.Now().UTC(), Payload: raw}
}

func (f *fixture) conversation(scope store.AccountScope, canvasID string) Conversation {
	f.t.Helper()
	receipt, err := f.service.CreateConversation(f.t.Context(), scope,
		command(f.t, map[string]string{"canvas_id": canvasID, "title": "参考整理"}))
	if err != nil {
		f.t.Fatalf("create conversation: %v", err)
	}
	var c Conversation
	if err := json.Unmarshal(receipt.Outcome.Response, &c); err != nil {
		f.t.Fatal(err)
	}
	return c
}

// revision stores one text revision owned by scope and keeps it rooted in an
// asset, which is what makes it legitimately readable by its own account.
func (f *fixture) revision(scope store.AccountScope, body string) string {
	f.t.Helper()
	var id string
	err := scope.WithTxScope(f.t.Context(), func(tx store.TxAccountScope) error {
		if err := tx.RequireCreativeCapability(f.t.Context(), "manual_write"); err != nil {
			return err
		}
		draft := creativecontent.Draft{Kind: "text", Payload: creativecontent.Payload{Body: &body}}
		r, err := creativecontent.WriteAndRetain(f.t.Context(), tx, draft, "", nil, func(r creativecontent.Revision) error {
			return tx.Insert(f.t.Context(), "creative_assets",
				[]string{"id", "kind", "title", "normalized_title", "content_id", "content_revision_id"},
				"ccas_"+uuid.NewString(), r.Kind, "参考", "参考", r.ContentID, r.ID)
		})
		id = r.ID
		return err
	})
	if err != nil {
		f.t.Fatalf("store revision: %v", err)
	}
	return id
}

func (f *fixture) count(table, cond string, args ...any) int {
	f.t.Helper()
	var n int
	if err := f.db.QueryRow(`SELECT count(*) FROM `+table+` WHERE `+cond, args...).Scan(&n); err != nil {
		f.t.Fatal(err)
	}
	return n
}

func userText(text string) newMessage {
	return newMessage{Role: "user", Status: "complete", Body: Body{Blocks: []Block{{Type: "text", Text: text}}}}
}

func TestAConversationTakesItsProjectFromTheCanvasNotTheRequest(t *testing.T) {
	f := setup(t)
	c := f.conversation(f.alice, f.canvasID)
	if c.ProjectID != "ccpj_a" {
		t.Fatalf("conversation should follow its canvas to project ccpj_a, got %q", c.ProjectID)
	}
	// Another account's canvas is not addressable, even though the id is valid.
	_, err := f.service.CreateConversation(t.Context(), f.alice,
		command(t, map[string]string{"canvas_id": "cccv_b", "title": "越界"}))
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("a canvas of another account must not be found, got %v", err)
	}
}

func TestConcurrentAppendsNeverShareAnOrdinal(t *testing.T) {
	f := setup(t)
	c := f.conversation(f.alice, f.canvasID)
	ctx := t.Context()

	holding := make(chan struct{})
	release := make(chan struct{})
	firstDone := make(chan Message, 1)
	go func() {
		var m Message
		err := f.alice.WithTxScope(ctx, func(tx store.TxAccountScope) error {
			var err error
			m, err = f.service.appendMessageInTx(ctx, tx, c.ID, userText("先说的"))
			if err != nil {
				return err
			}
			close(holding)
			<-release // Keep the conversation row locked while the rival tries.
			return nil
		})
		if err != nil {
			t.Error(err)
		}
		firstDone <- m
	}()
	<-holding

	secondDone := make(chan Message, 1)
	go func() {
		var m Message
		err := f.alice.WithTxScope(ctx, func(tx store.TxAccountScope) error {
			var err error
			m, err = f.service.appendMessageInTx(ctx, tx, c.ID, userText("后说的"))
			return err
		})
		if err != nil {
			t.Error(err)
		}
		secondDone <- m
	}()

	select {
	case m := <-secondDone:
		t.Fatalf("the second append took ordinal %d without waiting for the row lock", m.Ordinal)
	case <-time.After(300 * time.Millisecond):
	}
	close(release)
	first, second := <-firstDone, <-secondDone
	if first.Ordinal != 1 || second.Ordinal != 2 {
		t.Fatalf("ordinals must be consecutive, got %d and %d", first.Ordinal, second.Ordinal)
	}
}

func TestAStepThatAlreadyProducedItsMessageReplaysIt(t *testing.T) {
	f := setup(t)
	c := f.conversation(f.alice, f.canvasID)
	ctx := t.Context()
	answer := newMessage{Role: "assistant", Status: "complete", SourceStepID: "ccst_" + uuid.NewString(),
		Body: Body{Blocks: []Block{{Type: "text", Text: "这是回答"}}}}

	var first, second Message
	for _, target := range []*Message{&first, &second} {
		if err := f.alice.WithTxScope(ctx, func(tx store.TxAccountScope) error {
			m, err := f.service.appendMessageInTx(ctx, tx, c.ID, answer)
			*target = m
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	if first.ID != second.ID {
		t.Fatalf("a replayed step consumption appended a second message %s next to %s", second.ID, first.ID)
	}
	if n := f.count("creative_agent_messages", "conversation_id=$1", c.ID); n != 1 {
		t.Fatalf("one step result must project into one message, found %d", n)
	}
}

func TestPagingBackwardsReturnsEachPageInReadingOrder(t *testing.T) {
	f := setup(t)
	c := f.conversation(f.alice, f.canvasID)
	ctx := t.Context()
	for i := range 5 {
		if err := f.alice.WithTxScope(ctx, func(tx store.TxAccountScope) error {
			_, err := f.service.appendMessageInTx(ctx, tx, c.ID, userText(string(rune('a'+i))))
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	page, err := f.service.ListMessages(ctx, f.alice, c.ID, "", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 2 || page.Items[0].Ordinal != 4 || page.Items[1].Ordinal != 5 {
		t.Fatalf("newest page must read forwards, got %+v", page.Items)
	}
	if page.PrevCursor != "4" {
		t.Fatalf("prev cursor should point at the oldest item shown, got %q", page.PrevCursor)
	}
	older, err := f.service.ListMessages(ctx, f.alice, c.ID, page.PrevCursor, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(older.Items) != 2 || older.Items[0].Ordinal != 2 || older.Items[1].Ordinal != 3 {
		t.Fatalf("the cursor is exclusive, got %+v", older.Items)
	}
}

func grant(t *testing.T, f *fixture, conversationID string, revisions []string) (Consent, error) {
	t.Helper()
	payload := map[string]any{"conversation_id": conversationID, "vendor_key": "deepseek",
		"purpose": "creative_assistance", "mode": ScopeSelectedRevisions,
		"data_classes": []string{"text"}, "selected_revision_ids": revisions}
	receipt, err := f.service.GrantConsent(t.Context(), f.alice, command(t, payload))
	if err != nil {
		return Consent{}, err
	}
	var c Consent
	if err := json.Unmarshal(receipt.Outcome.Response, &c); err != nil {
		t.Fatal(err)
	}
	return c, nil
}

func TestAnotherAccountsRevisionCannotBeAuthorisedForEgress(t *testing.T) {
	f := setup(t)
	c := f.conversation(f.alice, f.canvasID)
	theirs := f.revision(f.bob, "别人的素材")
	if _, err := grant(t, f, c.ID, []string{theirs}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a revision of another account must not be authorisable, got %v", err)
	}
	if n := f.count("creative_egress_consents", "account_id=$1", "agent-a"); n != 0 {
		t.Fatalf("a refused grant left %d consents behind", n)
	}
}

func TestAnUnknownVendorCannotBeAuthorised(t *testing.T) {
	f := setup(t)
	c := f.conversation(f.alice, f.canvasID)
	mine := f.revision(f.alice, "我的素材")
	payload := map[string]any{"conversation_id": c.ID, "vendor_key": "some-other-company",
		"purpose": "creative_assistance", "mode": ScopeSelectedRevisions,
		"data_classes": []string{"text"}, "selected_revision_ids": []string{mine}}
	_, err := f.service.GrantConsent(t.Context(), f.alice, command(t, payload))
	if !errors.Is(err, creativeops.ErrValidation) {
		t.Fatalf("a destination this deployment cannot reach must be refused, got %v", err)
	}
}

func TestTheSameGrantOperationReplaysOneConsent(t *testing.T) {
	f := setup(t)
	c := f.conversation(f.alice, f.canvasID)
	mine := f.revision(f.alice, "我的素材")
	payload := map[string]any{"conversation_id": c.ID, "vendor_key": "deepseek",
		"purpose": "creative_assistance", "mode": ScopeSelectedRevisions,
		"data_classes": []string{"text"}, "selected_revision_ids": []string{mine}}
	cmd := command(t, payload)
	first, err := f.service.GrantConsent(t.Context(), f.alice, cmd)
	if err != nil {
		t.Fatal(err)
	}
	second, err := f.service.GrantConsent(t.Context(), f.alice, cmd)
	if err != nil {
		t.Fatal(err)
	}
	if *first.Outcome.ResultID != *second.Outcome.ResultID {
		t.Fatalf("a retried grant minted a second consent %s", *second.Outcome.ResultID)
	}
	if n := f.count("creative_egress_consents", "account_id=$1", "agent-a"); n != 1 {
		t.Fatalf("expected exactly one consent, found %d", n)
	}
}

func TestRevokingTwiceIsRefusedAndTheApprovalEvidenceStays(t *testing.T) {
	f := setup(t)
	c := f.conversation(f.alice, f.canvasID)
	mine := f.revision(f.alice, "我的素材")
	consent, err := grant(t, f, c.ID, []string{mine})
	if err != nil {
		t.Fatal(err)
	}
	revoke := func(expected creativeops.Revision) error {
		_, err := f.service.RevokeConsent(t.Context(), f.alice,
			command(t, map[string]any{"consent_id": consent.ID, "expected_revision": expected}))
		return err
	}
	if err := revoke(consent.Revision); err != nil {
		t.Fatal(err)
	}
	if err := revoke(consent.Revision + 1); !errors.Is(err, ErrConsentRevoked) {
		t.Fatalf("a withdrawn consent stays withdrawn whatever revision is offered, got %v", err)
	}
	// The record of what was authorised survives the withdrawal; it is evidence,
	// not a permission, and content already sent cannot be recalled.
	if n := f.count("creative_egress_consent_contents", "consent_id=$1", consent.ID); n != 1 {
		t.Fatalf("revocation erased the approval evidence, %d rows left", n)
	}
}

func TestARevokeWithAStaleRevisionLeavesTheConsentInForce(t *testing.T) {
	f := setup(t)
	c := f.conversation(f.alice, f.canvasID)
	mine := f.revision(f.alice, "我的素材")
	consent, err := grant(t, f, c.ID, []string{mine})
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.service.RevokeConsent(t.Context(), f.alice,
		command(t, map[string]any{"consent_id": consent.ID, "expected_revision": consent.Revision + 5}))
	if !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("a stale expected revision must conflict, got %v", err)
	}
	if n := f.count("creative_egress_consents", "id=$1 AND revoked_at IS NULL", consent.ID); n != 1 {
		t.Fatal("a refused revoke must not withdraw the consent")
	}
}

// TestNoReadOrWriteCrossesAnAccountBoundary pins the guards milestone A claims
// on the paths where the identifier arrives in the URL: the service must resolve
// it inside the caller's own scope, never trust that the caller owns it.
func TestNoReadOrWriteCrossesAnAccountBoundary(t *testing.T) {
	f := setup(t)
	ctx := t.Context()
	hers := f.conversation(f.alice, f.canvasID)
	mine := f.revision(f.alice, "我的素材")
	consent, err := grant(t, f, hers.ID, []string{mine})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.alice.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		_, err := f.service.appendMessageInTx(ctx, tx, hers.ID, userText("只有我看得见"))
		return err
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := f.service.ListMessages(ctx, f.bob, hers.ID, "", 10); !errors.Is(err, ErrNotFound) {
		t.Fatalf("another account read the conversation, got %v", err)
	}
	if page, err := f.service.ListConversations(ctx, f.bob, f.canvasID, 10, ""); err != nil || len(page.Items) != 0 {
		t.Fatalf("another account listed the conversation: %+v %v", page.Items, err)
	}
	_, err = f.service.RevokeConsent(ctx, f.bob,
		command(t, map[string]any{"consent_id": consent.ID, "expected_revision": consent.Revision}))
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("another account revoked the consent, got %v", err)
	}
	if n := f.count("creative_egress_consents", "id=$1 AND revoked_at IS NULL", consent.ID); n != 1 {
		t.Fatal("the consent must still be in force")
	}
	// Granting on someone else's conversation is refused even though the
	// conversation id is real: the id arrives from the URL, not from ownership.
	_, err = f.service.GrantConsent(ctx, f.bob, command(t, map[string]any{
		"conversation_id": hers.ID, "vendor_key": "deepseek", "purpose": "creative_assistance",
		"mode": ScopeAccountLibrary, "data_classes": []string{"text"}}))
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("another account granted egress on the conversation, got %v", err)
	}
}

func TestAMessageKeepsTheContentItShowsReadable(t *testing.T) {
	f := setup(t)
	ctx := t.Context()
	c := f.conversation(f.alice, f.canvasID)
	shown := f.revision(f.alice, "被引用的正文")
	message := userText("看这个")
	message.ContentRefs = []ContentRef{{RevisionID: shown, Role: "input"}}
	if err := f.alice.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		_, err := f.service.appendMessageInTx(ctx, tx, c.ID, message)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if n := f.count("creative_message_content_refs", "content_revision_id=$1 AND role='input'", shown); n != 1 {
		t.Fatalf("the message did not become a retention root, %d refs", n)
	}
	// The asset moves on, the conversation does not: dropping every other root
	// is what the photographer's next save does to the revision a message shows.
	if _, err := f.db.Exec(`DELETE FROM creative_assets WHERE content_revision_id=$1`, shown); err != nil {
		t.Fatal(err)
	}
	kept, err := creativecontent.Read(ctx, f.alice, shown)
	if err != nil {
		t.Fatalf("a message must keep what it shows readable: %v", err)
	}
	if kept.Payload.Body == nil || *kept.Payload.Body != "被引用的正文" {
		t.Fatalf("reading history returned the wrong body: %+v", kept.Payload)
	}
	// And the reference is what does it: an unreferenced revision that loses its
	// asset is gone, so this is a root, not a blanket pass.
	orphan := f.revision(f.alice, "没人引用的正文")
	if _, err := f.db.Exec(`DELETE FROM creative_assets WHERE content_revision_id=$1`, orphan); err != nil {
		t.Fatal(err)
	}
	if _, err := creativecontent.Read(ctx, f.alice, orphan); !errors.Is(err, creativecontent.ErrNotFound) {
		t.Fatalf("a revision no root holds must be unreadable, got %v", err)
	}

	// A malformed or repeated reference is refused before the message row
	// exists, so a half-written message never reaches the reading history.
	for _, refs := range [][]ContentRef{
		{{RevisionID: shown, Role: "sideways"}},
		{{RevisionID: "", Role: "input"}},
		{{RevisionID: shown, Role: "input"}, {RevisionID: shown, Role: "input"}},
	} {
		bad := userText("坏引用")
		bad.ContentRefs = refs
		err := f.alice.WithTxScope(ctx, func(tx store.TxAccountScope) error {
			_, err := f.service.appendMessageInTx(ctx, tx, c.ID, bad)
			return err
		})
		if !errors.Is(err, creativeops.ErrValidation) {
			t.Fatalf("refs %+v must be refused, got %v", refs, err)
		}
	}
	if n := f.count("creative_agent_messages", "conversation_id=$1", c.ID); n != 1 {
		t.Fatalf("a refused reference left %d messages behind", n)
	}
}

func TestAnOversizedOrMalformedBodyIsRefused(t *testing.T) {
	f := setup(t)
	ctx := t.Context()
	c := f.conversation(f.alice, f.canvasID)
	write := func(m newMessage) error {
		return f.alice.WithTxScope(ctx, func(tx store.TxAccountScope) error {
			_, err := f.service.appendMessageInTx(ctx, tx, c.ID, m)
			return err
		})
	}
	oversized := userText(strings.Repeat("字", f.service.limits.MessageBodyBytes))
	if err := write(oversized); !errors.Is(err, ErrLimit) {
		t.Fatalf("a body past the frozen limit must be refused as a limit, got %v", err)
	}
	for _, blocks := range [][]Block{
		{},
		{{Type: "text"}},                        // text with no words
		{{Type: "text", Text: "x", RefID: "r"}}, // a text block is not a locator
		{{Type: "content_ref"}},                 // a locator with nothing to locate
		{{Type: "reference", RefID: "ccrv_x"}},  // v1's untyped locator is not a v2 kind
		{{Type: "notice", Text: "只有文案"}},        // a notice needs a stable code
		{{Type: "invented", Text: "x"}},         // unknown kinds do not render
		// A skill block is the server's own reading, so a partial one is not a
		// smaller truth — it names a version nobody can resolve again.
		{{Type: "skill_ref"}},
		{{Type: "skill_ref", Skill: &BlockSkill{SkillID: "ccsk_x", VersionID: "ccsv_x"}}},
		{{Type: "skill_ref", Text: "旁白", Skill: &BlockSkill{
			SkillID: "ccsk_x", VersionID: "ccsv_x", VersionNumber: 1, Digest: "d", DisplayName: "n"}}},
	} {
		if err := write(newMessage{Role: "user", Status: "complete", Body: Body{Blocks: blocks}}); !errors.Is(err, creativeops.ErrValidation) {
			t.Fatalf("blocks %+v must be refused, got %v", blocks, err)
		}
	}
	// The other side of the block-count bound: one message is a message, not a
	// transcript someone pasted in whole.
	crowded := make([]Block, 201)
	for i := range crowded {
		crowded[i] = Block{Type: "text", Text: "x"}
	}
	if err := write(newMessage{Role: "user", Status: "complete", Body: Body{Blocks: crowded}}); !errors.Is(err, creativeops.ErrValidation) {
		t.Fatalf("a body past the block count must be refused, got %v", err)
	}
	if n := f.count("creative_agent_messages", "conversation_id=$1", c.ID); n != 0 {
		t.Fatalf("a refused body left %d messages behind", n)
	}
}

// TestRetryingTheSameRevokeSaysItIsAlreadyWithdrawn covers what a client really
// does: it retries with the revision it just used. That must not be reported as
// "someone else modified this". Note this is a fresh operation each time, so it
// exercises the domain path rather than the operation receipt's own replay.
func TestRetryingTheSameRevokeSaysItIsAlreadyWithdrawn(t *testing.T) {
	f := setup(t)
	c := f.conversation(f.alice, f.canvasID)
	mine := f.revision(f.alice, "我的素材")
	consent, err := grant(t, f, c.ID, []string{mine})
	if err != nil {
		t.Fatal(err)
	}
	revoke := func() error {
		_, err := f.service.RevokeConsent(t.Context(), f.alice,
			command(t, map[string]any{"consent_id": consent.ID, "expected_revision": consent.Revision}))
		return err
	}
	if err := revoke(); err != nil {
		t.Fatal(err)
	}
	if err := revoke(); !errors.Is(err, ErrConsentRevoked) {
		t.Fatalf("a retried revoke must say it is already withdrawn, got %v", err)
	}
}

// publishSkill puts one skill into the platform catalog through the real
// import protocol, which is the only way a version comes into existence.
func (f *fixture) publishSkill(slug, displayName string) creativeskill.Version {
	f.t.Helper()
	return f.importSkill(slug, displayName, 0, true)
}

// mediaRevision stores one ready image revision the way the media worker does.
// A test needs a real one because "may this account display it" and "can this
// deployment send it to a model" are different questions, and only a genuine
// media kind tells them apart.
func (f *fixture) mediaRevision(scope store.AccountScope, kind, mime string) string {
	f.t.Helper()
	blobID := "ccbl_" + uuid.NewString()
	if _, err := f.db.Exec(`INSERT INTO creative_blobs(id,account_id,storage_driver,object_key,object_version,
		sha256,byte_size,mime,width,height,state,verified_at)
		VALUES ($1,$2,'local',$3,'v1',$4,1024,$5,800,600,'ready',now())`,
		blobID, scope.AccountID(), "blobs/"+blobID, "sha256-"+strings.Repeat("a", 64), mime); err != nil {
		f.t.Fatal(err)
	}
	var id string
	err := scope.WithTxScope(f.t.Context(), func(tx store.TxAccountScope) error {
		if err := tx.RequireCreativeCapability(f.t.Context(), "manual_write"); err != nil {
			return err
		}
		draft := creativecontent.Draft{Kind: kind}
		r, err := creativecontent.WriteMediaAndRetain(f.t.Context(), tx, draft,
			[]creativecontent.ObjectBinding{{Role: "original", BlobID: blobID}},
			func(r creativecontent.Revision) error {
				return tx.Insert(f.t.Context(), "creative_assets",
					[]string{"id", "kind", "title", "normalized_title", "content_id", "content_revision_id"},
					"ccas_"+uuid.NewString(), r.Kind, "参考图", "参考图", r.ContentID, r.ID)
			})
		id = r.ID
		return err
	})
	if err != nil {
		f.t.Fatalf("store media revision: %v", err)
	}
	return id
}

// registerSkillTools gives this deployment the tool the fixture's skills
// declare. Without it every skill is correctly unavailable — FND-08 registers
// the real ones — and a test about anything else would pass for that reason
// instead of its own.
func (f *fixture) registerSkillTools() {
	f.t.Helper()
	f.service.tools = []ToolEntry{{
		Key: "search_assets", Version: 1, DisplayName: "检索素材",
		EffectClass: "read_only", MaxImpact: 0,
	}}
}

// publishOwnSkill publishes under an ordinary account rather than the platform
// publisher, which is what a test needs to ask "can someone else reach this".
func (f *fixture) publishOwnSkill(slug, displayName string) creativeskill.Version {
	f.t.Helper()
	req := skillRequest("account", slug, displayName)
	req.Activate = true
	return f.runImport(f.bob, req)
}

// importSkill is one run of the import protocol. Separating it from
// publishSkill is what lets a test append a version to an existing skill
// without moving its recommended pointer, which is the one case where the
// catalog's text changes while its version ids do not.
func (f *fixture) importSkill(slug, displayName string, expected creativeops.Revision, activate bool) creativeskill.Version {
	f.t.Helper()
	req := skillRequest("platform", slug, displayName)
	req.ExpectedSkillRevision = expected
	req.Activate = activate
	return f.runImport(f.platform, req)
}

var skillBody = []byte("# 比较清单\n\n1. 光线\n2. 色调\n")

func skillRequest(origin, slug, displayName string) creativeskill.ImportRequest {
	sum := sha256.Sum256(skillBody)
	return creativeskill.ImportRequest{
		OperationID: uuid.NewString(), Origin: origin, Slug: slug,
		DisplayName: displayName, Description: "在个人库中检索参考、比较候选。",
		Instructions: "先检索，再比较，最后写方向。",
		Manifest: creativeskill.Manifest{
			ManifestSchemaVersion: 1, InputKinds: []string{"text"}, MaxInputs: 20,
			ToolAllowlist: []string{"search_assets@1"}, RequiredModelCapabilities: []string{"tool_calling"},
			OutputContract: "一组参考节点加一段创作方向文字。", CompletionCheckKey: "reference_direction_v1",
		},
		Resources: []creativeskill.ResourceDeclaration{{
			Path: "references/comparison-checklist.md", Mime: "text/markdown; charset=utf-8",
			ByteSize: int64(len(skillBody)), SHA256: hex.EncodeToString(sum[:]),
		}},
	}
}

func (f *fixture) runImport(scope store.AccountScope, req creativeskill.ImportRequest) creativeskill.Version {
	f.t.Helper()
	record, err := f.skills.BeginImport(f.t.Context(), scope, req)
	if err != nil {
		f.t.Fatal(err)
	}
	for _, pending := range record.Pending {
		if _, err := f.skills.StageResource(f.t.Context(), scope, record.ID, pending, bytes.NewReader(skillBody)); err != nil {
			f.t.Fatal(err)
		}
	}
	version, err := f.skills.FinalizeImport(f.t.Context(), scope, record.ID)
	if err != nil {
		f.t.Fatal(err)
	}
	return version
}

// skill_catalog_revision answers "has what I cached changed". A client that
// caches on it and sees the same value will not refetch, so anything the
// catalog renders has to be inside it.
//
// The case that breaks a hand-picked field list: importing a version without
// activating it renames the skill — the import always writes the catalog text —
// while the recommended pointer, and therefore every version id and digest in
// the answer, stays where it was.
func TestRenamingASkillWithoutActivatingMovesTheCatalogRevision(t *testing.T) {
	f := setup(t)
	first := f.publishSkill("reference-direction", "参考整理与创作方向")
	before, err := f.service.Catalog(t.Context(), f.alice)
	if err != nil {
		t.Fatal(err)
	}

	f.importSkill("reference-direction", "参考整理（改名后）", first.SkillRevision, false)
	after, err := f.service.Catalog(t.Context(), f.alice)
	if err != nil {
		t.Fatal(err)
	}

	// The premise: the rename reached the catalog and the recommended version
	// did not move. Without both, this test would pass for the wrong reason.
	if len(after.Skills) != 1 || after.Skills[0].DisplayName != "参考整理（改名后）" {
		t.Fatalf("the rename did not reach the catalog: %+v", after.Skills)
	}
	if after.Skills[0].VersionID != first.ID || after.Skills[0].Digest != first.Digest {
		t.Fatalf("the recommended version moved, so this is not the case under test: %+v", after.Skills[0])
	}
	if before.SkillCatalogRevision == after.SkillCatalogRevision {
		t.Fatalf("the catalog text changed but the revision did not (%s); a client caching on it keeps the old name",
			after.SkillCatalogRevision)
	}
}

// Nothing ships inside the binary any more. A deployment that has not run the
// import yet says so plainly, rather than serving a copy compiled in months ago.
func TestTheCatalogIsEmptyUntilSomethingIsImported(t *testing.T) {
	f := setup(t)
	view, err := f.service.Catalog(t.Context(), f.alice)
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Skills) != 0 {
		t.Fatalf("a deployment with no import reported %+v", view.Skills)
	}
	// The rest of the catalog is unaffected: ordinary conversation does not
	// depend on a skill being published.
	if len(view.Models) == 0 || view.LimitsVersion != limitsVersion {
		t.Fatalf("the catalog lost its other halves: %+v", view)
	}
	before := view.SkillCatalogRevision
	f.publishSkill("reference-direction", "参考整理与创作方向")
	after, err := f.service.Catalog(t.Context(), f.alice)
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Skills) != 1 {
		t.Fatalf("the imported skill did not reach the catalog: %+v", after.Skills)
	}
	// The revision exists so a client can tell "same answer" from "look again".
	if after.SkillCatalogRevision == before || after.SkillCatalogRevision == "" {
		t.Fatalf("the skill catalog revision did not move: %q", after.SkillCatalogRevision)
	}
	if after.SkillCatalogRevision == after.CatalogVersion {
		t.Fatal("the skill catalog revision must not be the model catalog's version")
	}
}

func TestASkillIsUnavailableUntilItsToolsAreRegistered(t *testing.T) {
	f := setup(t)
	published := f.publishSkill("reference-direction", "参考整理与创作方向")
	view, err := f.service.Catalog(t.Context(), f.alice)
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Skills) != 1 {
		t.Fatalf("the published skill is missing from the catalog: %+v", view.Skills)
	}
	skill := view.Skills[0]
	if skill.Available {
		t.Fatalf("%s claims to be available while no tool is registered", skill.Slug)
	}
	if skill.UnavailableWhy == "" || skill.Digest != published.Digest {
		t.Fatalf("a skill must stay visible with its reason and digest: %+v", skill)
	}
	// The same judgement applies to one version asked about directly.
	version, err := f.service.SkillVersion(t.Context(), f.alice, published.SkillID, published.ID)
	if err != nil {
		t.Fatal(err)
	}
	if version.Available || version.UnavailableWhy == "" {
		t.Fatalf("version %+v", version)
	}
}

// The version endpoint answers with the declaration, and with nothing a caller
// could use to reach the bytes another way.
func TestAVersionAnswerCarriesItsDeclarationAndNoStorageAddress(t *testing.T) {
	f := setup(t)
	published := f.publishSkill("reference-direction", "参考整理与创作方向")

	view, err := f.service.SkillVersion(t.Context(), f.alice, published.SkillID, published.ID)
	if err != nil {
		t.Fatal(err)
	}
	if view.Origin != "platform" || view.VersionNumber != 1 || view.Digest != published.Digest {
		t.Fatalf("version %+v", view)
	}
	if len(view.Resources) != 1 || view.Resources[0].Path != "references/comparison-checklist.md" {
		t.Fatalf("the declaration is missing: %+v", view.Resources)
	}
	// Serialising the answer is the real check: a storage key added to the
	// domain type later would show up here rather than in production.
	encoded, err := json.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"object_key", "object_version", "bucket", "storage_driver", "instructions"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("the version answer carries %q: %s", forbidden, encoded)
		}
	}
}

// The picker endpoint searches, and it answers nothing at all to an account
// that may not use the creative space.
func TestTheSkillsEndpointSearchesAndChecksCapabilityFirst(t *testing.T) {
	f := setup(t)
	direction := f.publishSkill("reference-direction", "参考整理与创作方向")
	f.publishSkill("retouch-notes", "修图要点")

	page, err := f.service.ListSkills(t.Context(), f.alice, "reference", "", 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].SkillID != direction.SkillID {
		t.Fatalf("search returned %+v", page.Items)
	}
	// Capability first, as everywhere else in this service: an account that is
	// not active learns nothing about this deployment, not even an empty list
	// built from real rows.
	if _, err := f.db.Exec(`UPDATE accounts SET status='pending_verification' WHERE id='agent-b'`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.ListSkills(t.Context(), f.bob, "", "", 20); !errors.Is(err, store.ErrCreativeAccessDenied) {
		t.Fatalf("a suspended account read the directory: %v", err)
	}
	if _, err := f.service.SkillVersion(t.Context(), f.bob, direction.SkillID, direction.ID); !errors.Is(err, store.ErrCreativeAccessDenied) {
		t.Fatalf("a suspended account read a version: %v", err)
	}
}

func TestACatalogReportsTheFrozenLimitsAndReachableVendors(t *testing.T) {
	f := setup(t)
	view, err := f.service.Catalog(t.Context(), f.alice)
	if err != nil {
		t.Fatal(err)
	}
	if view.LimitsVersion != limitsVersion || view.Limits.Version != limitsVersion {
		t.Fatalf("limits version must be frozen, got %q", view.LimitsVersion)
	}
	if len(view.Vendors) == 0 || view.Vendors[0] != "deepseek" {
		t.Fatalf("the catalog must name the companies it can reach, got %v", view.Vendors)
	}
	for _, model := range view.Models {
		if model.VendorKey == "" {
			t.Fatalf("model %s has no vendor to authorise", model.ModelKey)
		}
	}
}

// Every array the catalog declares is required and non-nullable, and a Go nil
// slice marshals as null. A client looping over one would fault — so the
// deployment with nothing registered, which is every deployment today, is
// exactly the case that has to hold.
//
// This asserts the wire bytes rather than the Go values: a nil slice and an
// empty one are indistinguishable at a length check, and the difference only
// appears after encoding.
func TestEveryCatalogArrayIsAnArrayOnTheWire(t *testing.T) {
	f := setup(t)
	f.service.tools = nil // Explicitly exercise a deployment without tools.
	view, err := f.service.Catalog(t.Context(), f.alice)
	if err != nil {
		t.Fatal(err)
	}
	// The premise: nothing is registered and nothing is imported, so every list
	// below is empty. A populated catalog would pass for the wrong reason.
	if len(view.Tools) != 0 || len(view.Skills) != 0 {
		t.Fatalf("this deployment has something registered, so the empty case is untested: %+v", view)
	}
	encoded, err := json.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &fields); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"vendors", "models", "skills", "tools"} {
		raw, present := fields[name]
		if !present {
			t.Fatalf("%q is required by the contract but absent from %s", name, encoded)
		}
		if string(raw) == "null" {
			t.Fatalf("%q serialised as null; the contract declares a non-nullable array", name)
		}
	}
}
