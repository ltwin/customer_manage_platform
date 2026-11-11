package creativecontent_test

import (
	"errors"
	"slices"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/creativecontent"
	"github.com/samson/customer-manage-platform/backend/internal/planningmedia"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

func TestCharacterImagesAreScopedAndHeld(t *testing.T) {
	db, a, b := setup(t)
	ctx := t.Context()
	if _, err := db.Exec(`
INSERT INTO creative_blobs(id,account_id,storage_driver,object_key,object_version,sha256,byte_size,mime,state,verified_at) VALUES
 ('character-png-a','content-a','local','character-a','v1','sha256-'||repeat('1',64),50,'image/png','ready',clock_timestamp()),
 ('character-png-b','content-b','local','character-b','v1','sha256-'||repeat('2',64),50,'image/png','ready',clock_timestamp())`); err != nil {
		t.Fatal(err)
	}
	image := func(scope store.AccountScope, blob string, rights creativecontent.RightsDeclarationInput) creativecontent.Revision {
		t.Helper()
		var revision creativecontent.Revision
		if err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
			var err error
			revision, err = creativecontent.WriteMediaAndRetain(ctx, tx, creativecontent.Draft{Kind: "image", Rights: rights},
				[]creativecontent.ObjectBinding{{Role: "original", BlobID: blob}}, func(r creativecontent.Revision) error { return asset(ctx, tx, r) })
			return err
		}); err != nil {
			t.Fatal(err)
		}
		return revision
	}

	view := image(a, "character-png-a", creativecontent.RightsDeclarationInput{})
	foreign := image(b, "character-png-b", creativecontent.RightsDeclarationInput{})
	licensed := image(a, "character-png-a", creativecontent.RightsDeclarationInput{
		SourceClass: planningmedia.SourceLicensed, RightsBasis: planningmedia.RightsLicenseRecorded,
	})
	deleted := image(a, "character-png-a", creativecontent.RightsDeclarationInput{})
	if _, err := db.Exec("UPDATE creative_content_revisions SET state='deleted' WHERE id=$1", deleted.ID); err != nil {
		t.Fatal(err)
	}
	text, err := write(t, a, textDraft("不是图片"), "", nil, func(tx store.TxAccountScope, r creativecontent.Revision) error { return asset(ctx, tx, r) })
	if err != nil {
		t.Fatal(err)
	}

	card := creativecontent.CharacterPayload{
		Name: " 云 ", Appearance: "青绿色短发",
		Views:   []creativecontent.CharacterView{{ContentRevisionID: view.ID, View: "front"}},
		Sources: []creativecontent.CharacterSource{{ContentRevisionID: view.ID}, {ContentRevisionID: licensed.ID}},
	}
	save := func(card creativecontent.CharacterPayload) (creativecontent.Revision, error) {
		var revision creativecontent.Revision
		err := a.WithTxScope(ctx, func(tx store.TxAccountScope) error {
			if err := tx.RequireCreativeCapability(ctx, "manual_write"); err != nil {
				return err
			}
			var err error
			revision, err = creativecontent.WriteCharacterAndRetain(ctx, tx, card, "", func(r creativecontent.Revision) error {
				return asset(ctx, tx, r)
			})
			return err
		})
		return revision, err
	}

	for _, tc := range []struct{ name, id string }{
		{"other account", foreign.ID}, {"not an image", text.ID},
		{"not ready", deleted.ID}, {"display only", licensed.ID},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bad := card
			bad.Views = append(slices.Clone(card.Views), creativecontent.CharacterView{ContentRevisionID: tc.id, View: "other"})
			var unavailable *creativecontent.CharacterImageError
			if _, err := save(bad); !errors.As(err, &unavailable) || !slices.Equal(unavailable.RevisionIDs, []string{tc.id}) {
				t.Fatalf("expected rejected reference %q, got %v", tc.id, err)
			}
		})
	}
	var count int
	if err := db.QueryRow("SELECT count(*) FROM creative_contents WHERE kind='character'").Scan(&count); err != nil || count != 0 {
		t.Fatalf("rejected writes left %d character contents: %v", count, err)
	}
	created, err := save(card)
	if err != nil {
		t.Fatal(err)
	}
	got, err := creativecontent.Read(ctx, a, created.ID)
	if err != nil || got.Payload.Character == nil || got.Payload.Character.Name != "云" || len(got.Media) != 0 {
		t.Fatalf("read character: %+v, %v", got, err)
	}
	if err := db.QueryRow("SELECT count(*) FROM creative_content_revision_refs WHERE account_id='content-a' AND holder_revision_id=$1 AND role='character'", got.ID).Scan(&count); err != nil || count != 2 {
		t.Fatalf("wanted two distinct held revisions, got %d: %v", count, err)
	}
	draft := creativecontent.Draft{Kind: creativecontent.CharacterKind, Payload: got.Payload}
	if !errors.Is(creativecontent.Validate(draft), creativeops.ErrValidation) {
		t.Fatal("generic content validation accepted a character")
	}
	if err := a.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		_, err := creativecontent.WriteDocumentAndRetain(ctx, tx, draft, func(r creativecontent.Revision) error { return asset(ctx, tx, r) })
		return err
	}); !errors.Is(err, creativeops.ErrValidation) {
		t.Fatalf("document writer accepted a character: %v", err)
	}
}
