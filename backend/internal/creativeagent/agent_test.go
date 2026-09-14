package creativeagent

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/samson/customer-manage-platform/backend/internal/creativecontent"
	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/llmgateway"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store/storetest"
)

func TestMain(m *testing.M) { storetest.Main(m, store.MigrateUp) }

type fixture struct {
	t        *testing.T
	db       *sql.DB
	service  *Service
	alice    store.AccountScope
	bob      store.AccountScope
	canvasID string
}

func setup(t *testing.T) *fixture {
	t.Helper()
	url := storetest.NewURL(t)
	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`INSERT INTO accounts(id,password_hash,status) VALUES ('agent-a','test','active'),('agent-b','test','active');
		INSERT INTO creative_projects(id,account_id,name) VALUES ('ccpj_a','agent-a','项目'),('ccpj_b','agent-b','项目');
		INSERT INTO creative_canvases(id,account_id,project_id,name) VALUES ('cccv_a','agent-a','ccpj_a','画布'),('cccv_b','agent-b','ccpj_b','画布')`); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(t.Context(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)
	models, err := llmgateway.OpenCatalog("", nil)
	if err != nil {
		t.Fatal(err)
	}
	service, err := Compose(models)
	if err != nil {
		t.Fatal(err)
	}
	return &fixture{t: t, db: db, service: service,
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
		{{Type: "reference"}},                   // a locator with nothing to locate
		{{Type: "notice", Text: "只有文案"}},        // a notice needs a stable code
		{{Type: "invented", Text: "x"}},         // unknown kinds do not render
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

func TestASkillIsUnavailableUntilItsToolsAreRegistered(t *testing.T) {
	f := setup(t)
	view, err := f.service.Catalog(t.Context(), f.alice)
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Skills) == 0 {
		t.Fatal("the deployment ships at least one skill package")
	}
	for _, skill := range view.Skills {
		if skill.Available {
			t.Fatalf("%s@%d claims to be available while no tool is registered", skill.Key, skill.Version)
		}
		if skill.UnavailableWhy == "" || skill.Digest == "" {
			t.Fatalf("%s@%d must stay visible with its reason and digest", skill.Key, skill.Version)
		}
	}
}

func TestTwoPackagesClaimingOneIdentityAreRefused(t *testing.T) {
	packages, err := LoadEmbeddedSkills()
	if err != nil {
		t.Fatal(err)
	}
	if len(packages) == 0 {
		t.Fatal("no embedded skill packages")
	}
	forged := packages[0]
	forged.Instructions += "\n偷偷改过的一句"
	forged.Digest = "0000000000000000000000000000000000000000000000000000000000000000"
	if _, err := NewSkillRegistry([]SkillPackage{packages[0], forged}); !errors.Is(err, ErrSkillUnavailable) {
		t.Fatalf("one key and version must resolve to one text, got %v", err)
	}
}

// TestAPublishedSkillCannotBeEditedBehindItsDigest pins what the digest is for:
// whatever ran, the registry can still produce that exact text. A SkillPackage
// is a value, but its map and slices are not, so neither the caller holding a
// copy nor the slice the registry was built from may be a window into it.
func TestAPublishedSkillCannotBeEditedBehindItsDigest(t *testing.T) {
	packages, err := LoadEmbeddedSkills()
	if err != nil {
		t.Fatal(err)
	}
	if len(packages) == 0 || packages[0].Manifest.Key != "reference-direction" {
		t.Fatalf("this test edits the shipped package; got %+v", packages)
	}
	registry, err := NewSkillRegistry(packages)
	if err != nil {
		t.Fatal(err)
	}
	const resource = "references/comparison-checklist.md"
	original, err := registry.Get("reference-direction", 1)
	if err != nil {
		t.Fatal(err)
	}
	want := append([]byte(nil), original.Resources[resource]...)
	if len(want) == 0 || len(original.Manifest.ToolAllowlist) == 0 {
		t.Fatal("the package must ship a resource and a tool allowlist to edit")
	}
	wantTool := original.Manifest.ToolAllowlist[0]

	// Three reaches through what Get handed back: the map, the bytes behind one
	// entry, and the manifest's own slice.
	original.Resources[resource+".injected"] = []byte("凭空多出来的参考")
	original.Resources[resource][0] = 'X'
	original.Manifest.ToolAllowlist[0] = "delete_everything@1"
	// And the same reaches through the slice NewSkillRegistry was handed.
	packages[0].Resources[resource][0] = 'X'
	packages[0].Manifest.ToolAllowlist[0] = "delete_everything@1"
	packages[0].Resources[resource+".sneaked"] = []byte("构造之后塞进来的")

	again, err := registry.Get("reference-direction", 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{resource + ".injected", resource + ".sneaked"} {
		if _, forged := again.Resources[name]; forged {
			t.Fatalf("a caller added %s to a published package", name)
		}
	}
	if got := again.Resources[resource]; !bytes.Equal(got, want) {
		t.Fatalf("the published resource changed while its digest did not: %q", got)
	}
	if got := again.Manifest.ToolAllowlist[0]; got != wantTool {
		t.Fatalf("a caller widened the published tool allowlist to %q", got)
	}
	body, err := registry.Resource("reference-direction", 1, resource)
	if err != nil || !bytes.Equal(body, want) {
		t.Fatalf("the resource read followed the edit: %q %v", body, err)
	}
}

func TestASkillResourceOutsideThePackageIsRefused(t *testing.T) {
	f := setup(t)
	if _, err := f.service.skills.Resource("reference-direction", 1, "../../../etc/passwd"); !errors.Is(err, ErrSkillUnavailable) {
		t.Fatalf("resource lookup must be by declared key only, got %v", err)
	}
	if _, err := f.service.skills.Resource("reference-direction", 1, "references/comparison-checklist.md"); err != nil {
		t.Fatalf("a declared resource must be readable: %v", err)
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
