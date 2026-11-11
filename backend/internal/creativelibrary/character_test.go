package creativelibrary_test

import (
	"errors"
	"sync"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/creativecontent"
	"github.com/samson/customer-manage-platform/backend/internal/creativelibrary"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

func TestCharacterCreateSearchAndSave(t *testing.T) {
	db, a, b := setup(t)
	ctx := t.Context()
	if _, err := db.Exec(`
INSERT INTO creative_blobs(id,account_id,storage_driver,object_key,object_version,sha256,byte_size,mime,state,verified_at)
VALUES('card-test-png','library-a','local','character','v1','sha256-'||repeat('1',64),50,'image/png','ready',clock_timestamp())`); err != nil {
		t.Fatal(err)
	}
	var image creativecontent.Revision
	if err := a.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		if err := tx.RequireCreativeCapability(ctx, "manual_write"); err != nil {
			return err
		}
		var err error
		image, err = creativecontent.WriteMediaAndRetain(ctx, tx, creativecontent.Draft{Kind: "image"},
			[]creativecontent.ObjectBinding{{Role: "original", BlobID: "card-test-png"}}, func(r creativecontent.Revision) error {
				_, err := creativelibrary.CreateFromRevisionInTx(ctx, tx, r, creativelibrary.ImportTarget{Title: "立绘"})
				return err
			})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	card := creativecontent.CharacterPayload{
		Name: "阿尔托莉雅", Variant: "第三灵基", Appearance: "银甲",
		IP: &creativecontent.CharacterIP{Name: "ＦＡＴＥ"}, Work: "FGO",
		Aliases: []string{"Saber"}, Views: []creativecontent.CharacterView{{ContentRevisionID: image.ID, View: "front"}},
		Sources: []creativecontent.CharacterSource{},
	}
	create := cmd(t, creativelibrary.CreateCharacterInput{Character: card})
	receipt, err := creativelibrary.CreateCharacter(ctx, a, create)
	initial := decode[creativelibrary.AssetResult](t, receipt, err)
	if receipt.Outcome.HTTPStatus != 201 || initial.Title != "阿尔托莉雅 · 第三灵基" || initial.Revision != 1 {
		t.Fatalf("initial asset: %+v", initial)
	}
	replay, err := creativelibrary.CreateCharacter(ctx, a, create)
	if err != nil || !sameJSON(t, replay.Outcome.Response, receipt.Outcome.Response) {
		t.Fatalf("idempotent replay: %v", err)
	}
	for _, query := range []creativelibrary.Search{
		{CharacterIP: "命运系列", CharacterWork: "FGO"},
		{CharacterWork: "Fate/Grand Order"},
		{Q: "saber", Kind: "character"},
		// Catalog aliases match at query time, not from the stored text.
		{Q: "FGO", Kind: "character"},
		{Q: "命运系列"},
	} {
		page := search(t, a, query)
		if len(page.Items) != 1 || page.Items[0].ID != initial.ID ||
			page.Items[0].Character == nil || page.Items[0].Character.Work != "Fate/Grand Order" {
			t.Fatalf("query %+v did not find the card: %+v", query, page)
		}
	}
	hints, err := creativelibrary.SuggestCharacterIPs(ctx, a, "fate", 5)
	if err != nil || len(hints.Items) != 1 || hints.Items[0].CharacterCount != 1 {
		t.Fatalf("IP suggestions: %+v, %v", hints, err)
	}
	foreign, err := creativelibrary.SuggestCharacterIPs(ctx, b, "fate", 5)
	if err != nil || len(foreign.Items) != 1 || foreign.Items[0].CharacterCount != 0 {
		t.Fatalf("other account IP suggestions: %+v, %v", foreign, err)
	}

	customCard := card
	customCard.IP = &creativecontent.CharacterIP{Name: "星环档案"}
	customCard.Work = "第一卷"
	customReceipt, err := creativelibrary.CreateCharacter(ctx, a, cmd(t, creativelibrary.CreateCharacterInput{Character: customCard}))
	custom := decode[creativelibrary.AssetResult](t, customReceipt, err)
	customIPs, err := creativelibrary.SuggestCharacterIPs(ctx, a, "星环", 5)
	if err != nil || len(customIPs.Items) != 1 || customIPs.Items[0].Builtin || customIPs.Items[0].CharacterCount != 1 {
		t.Fatalf("custom IP suggestions: %+v, %v", customIPs, err)
	}
	// Simulate a card filed under a custom spelling before the catalog added it.
	if _, err := db.Exec(`UPDATE creative_asset_search SET character_ip_name='genshin' WHERE asset_id=$1`, custom.ID); err != nil {
		t.Fatal(err)
	}
	page := search(t, a, creativelibrary.Search{CharacterIP: "genshin-impact"})
	if len(page.Items) != 1 || page.Items[0].ID != custom.ID {
		t.Fatalf("catalog expansion hid old custom IP: %+v", page)
	}

	input := creativelibrary.SaveCharacterRevisionInput{ID: initial.ID, ExpectedRevision: 1, Character: card}
	receipt, err = creativelibrary.SaveCharacterRevision(ctx, a, cmd(t, input))
	unchanged := decode[creativelibrary.AssetResult](t, receipt, err)
	if !unchanged.Reused || unchanged.ContentRevisionID != initial.ContentRevisionID || unchanged.LibraryRevision != custom.LibraryRevision {
		t.Fatalf("no-op produced a write: %+v", unchanged)
	}
	input.Character.Appearance = "银甲和蓝色斗篷"
	commands := []creativeops.Command{cmd(t, input), cmd(t, input)}
	var wg sync.WaitGroup
	results := make([]error, len(commands))
	for i, c := range commands {
		wg.Go(func() {
			_, results[i] = creativelibrary.SaveCharacterRevision(ctx, a, c)
		})
	}
	wg.Wait()
	if results[0] == nil {
		results[0], results[1] = results[1], results[0]
	}
	if !errors.Is(results[0], creativelibrary.ErrVersionConflict) || results[1] != nil {
		t.Fatalf("concurrent saves: %v", results)
	}
	latest := current(t, a, initial.ID)
	if latest.Revision != 2 || latest.ContentRevisionID == initial.ContentRevisionID ||
		latest.Content == nil || latest.Content.Sequence != 2 {
		t.Fatalf("wrong current version: %+v", latest)
	}
	_, err = creativelibrary.Trash(ctx, a, cmd(t, creativelibrary.AssetVersion{ID: initial.ID, Revision: 2}))
	if err != nil {
		t.Fatal(err)
	}
	input.ExpectedRevision = 3
	if _, err := creativelibrary.SaveCharacterRevision(ctx, a, cmd(t, input)); !errors.Is(err, creativelibrary.ErrTrashed) {
		t.Fatalf("trashed card write: %v", err)
	}
}
