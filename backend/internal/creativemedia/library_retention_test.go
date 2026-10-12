package creativemedia_test

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/samson/customer-manage-platform/backend/internal/creativelibrary"
	"github.com/samson/customer-manage-platform/backend/internal/creativemedia"
	"github.com/samson/customer-manage-platform/backend/internal/creativereference"
	"github.com/samson/customer-manage-platform/backend/internal/platform/jobs"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

func TestLibraryRemovalKeepsLocalMediaUntilLastReferenceReleased(t *testing.T) {
	for _, mode := range []string{"single", "batch", "empty", "expiry"} {
		t.Run(mode, func(t *testing.T) {
			f := setup(t)
			body := sample(t, "sample.png")
			upload := f.uploadFile(t, f.a, "keep.png", "image/png", "image", body, creativemedia.Target{Kind: "asset", Asset: &creativemedia.AssetTarget{Title: "keep"}})
			asset, err := creativelibrary.GetAsset(t.Context(), f.a, upload.Publication.ID)
			if err != nil {
				t.Fatal(err)
			}
			p := project(t, f.a)
			// A real node root, independent of the library asset. A maintenance removal
			// must not release this reference or touch the immutable bytes.
			_, err = f.db.Exec(`INSERT INTO creative_nodes(id,account_id,canvas_id,type_key,x,y,content_id,content_revision_id) VALUES('keep-node','media-a',$1,'core.image',0,0,$2,$3)`, p.CanvasID, asset.ContentID, asset.ContentRevisionID)
			if err != nil {
				t.Fatal(err)
			}
			_ = must[creativelibrary.LibraryResult](t)(creativelibrary.Trash(t.Context(), f.a, command(t, creativelibrary.AssetVersion{ID: asset.ID, Revision: asset.Revision})))
			_ = must[creativelibrary.LibraryResult](t)(creativelibrary.Restore(t.Context(), f.a, command(t, creativelibrary.AssetVersion{ID: asset.ID, Revision: asset.Revision + 1})))
			_ = must[creativelibrary.LibraryResult](t)(creativelibrary.Trash(t.Context(), f.a, command(t, creativelibrary.AssetVersion{ID: asset.ID, Revision: asset.Revision + 2})))
			version := creativelibrary.AssetVersion{ID: asset.ID, Revision: asset.Revision + 3}
			switch mode {
			case "single":
				_, err = creativelibrary.Purge(t.Context(), f.a, command(t, version))
			case "batch":
				_, err = creativelibrary.BatchPurge(t.Context(), f.a, command(t, creativelibrary.BatchAssetInput{Assets: []creativelibrary.AssetVersion{version}}))
			case "empty":
				var page creativelibrary.AssetPage
				page, err = creativelibrary.SearchAssets(t.Context(), f.a, creativelibrary.Search{View: "trash", Limit: 1})
				if err == nil {
					_, err = creativelibrary.EmptyTrash(t.Context(), f.a, command(t, creativelibrary.EmptyTrashInput{ExpectedLibraryRevision: page.LibraryRevision}))
				}
			case "expiry":
				_, err = f.db.Exec(`UPDATE creative_assets SET deleted_at=clock_timestamp()-interval '8 days',purge_after=clock_timestamp()-interval '1 hour' WHERE id=$1`, asset.ID)
				if err == nil {
					err = creativelibrary.NewTrashExpiry(nil).Handler().Work(t.Context(), f.a, jobs.Request{Kind: "library.trash_expiry", OperationID: uuid.NewString(), CreatedAt: time.Now(), Payload: json.RawMessage(`{}`)})
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			ticket, err := f.svc.IssueTicket(t.Context(), f.a, creativemedia.TicketInput{ContentRevisionID: asset.ContentRevisionID, Role: "original", Purpose: "display"})
			if err != nil {
				t.Fatal(err)
			}
			token := ticket.URL[strings.Index(ticket.URL, "ticket=")+7:]
			stream, err := f.svc.Open(t.Context(), func(string) store.AccountScope { return f.a }, token, asset.ContentRevisionID, "original", nil)
			if err != nil {
				t.Fatal(err)
			}
			actual, err := io.ReadAll(stream.Body)
			_ = stream.Body.Close()
			if err != nil || !bytes.Equal(actual, body) {
				t.Fatal("local object changed or lost", err)
			}
			_, err = f.db.Exec(`DELETE FROM creative_nodes WHERE account_id='media-a' AND id='keep-node'`)
			if err != nil {
				t.Fatal(err)
			}
			err = f.a.WithReadSnapshot(t.Context(), func(tx store.ReadTxAccountScope) error {
				live, err := creativereference.Live(t.Context(), tx, []string{asset.ContentRevisionID})
				if live[asset.ContentRevisionID] {
					t.Fatal("last root still retained")
				}
				return err
			})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := f.svc.IssueTicket(t.Context(), f.a, creativemedia.TicketInput{ContentRevisionID: asset.ContentRevisionID, Role: "original", Purpose: "display"}); err == nil {
				t.Fatal("unowned content authorized")
			}
		})
	}
}
