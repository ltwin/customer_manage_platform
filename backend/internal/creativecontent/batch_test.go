package creativecontent

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store/storetest"
)

type countedReader struct {
	store.ReadTxAccountScope
	queries int
}

func (r *countedReader) Query(ctx context.Context, table, columns, condition string, args ...any) (store.Rows, error) {
	r.queries++
	return r.ReadTxAccountScope.Query(ctx, table, columns, condition, args...)
}
func (r *countedReader) QueryPage(ctx context.Context, table, columns, condition string, order []store.OrderBy, limit, offset int, args ...any) (store.Rows, error) {
	r.queries++
	return r.ReadTxAccountScope.QueryPage(ctx, table, columns, condition, order, limit, offset, args...)
}
func (r *countedReader) QueryRow(ctx context.Context, table, columns, condition string, args ...any) store.Row {
	r.queries++
	return r.ReadTxAccountScope.QueryRow(ctx, table, columns, condition, args...)
}
func (r *countedReader) Exists(ctx context.Context, table, condition string, args ...any) (bool, error) {
	r.queries++
	return r.ReadTxAccountScope.Exists(ctx, table, condition, args...)
}

func TestDisplayBatchMatchesSingleReadsWithConstantQueries(t *testing.T) {
	url := storetest.NewURL(t)
	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if _, err := db.Exec(`INSERT INTO accounts(id,password_hash,status) VALUES ('batch','test','active'),('other','test','active')`); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(t.Context(), url)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	scope := st.ScopeFor(auth.AccountContext{AccountID: "batch"})
	ids := []string{}
	err = scope.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
		for i := 0; i < 100; i++ {
			body := fmt.Sprintf("content %d", i)
			r, err := WriteAndRetain(t.Context(), tx, Draft{Kind: "text", Payload: Payload{Body: &body}}, "", nil, func(r Revision) error {
				return tx.Insert(t.Context(), "creative_assets", []string{"id", "kind", "title", "normalized_title", "content_id", "content_revision_id"}, fmt.Sprintf("asset-%d", i), r.Kind, body, body, r.ContentID, r.ID)
			})
			if err != nil {
				return err
			}
			ids = append(ids, r.ID)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, size := range []int{1, 100} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			err := scope.WithReadSnapshot(t.Context(), func(tx store.ReadTxAccountScope) error {
				reader := &countedReader{ReadTxAccountScope: tx}
				results, err := readDisplayBatch(t.Context(), reader, ids[:size])
				if err != nil {
					return err
				}
				if reader.queries != 5 {
					t.Fatalf("%d revisions took %d SQL calls; want 5", size, reader.queries)
				}
				for _, id := range ids[:size] {
					single, err := ReadInSnapshot(t.Context(), tx, id, "display")
					if err != nil {
						return err
					}
					if results[id].Err != nil || !reflect.DeepEqual(results[id].Revision, single) {
						t.Fatalf("batch differs for %s: %+v / %+v", id, results[id], single)
					}
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
		})
	}
	cases := []struct {
		name, query string
		want        error
	}{
		{"revoked", `UPDATE creative_usage_grants SET revoked_at=clock_timestamp() WHERE declaration_id=(SELECT rights_declaration_id FROM creative_content_revisions WHERE id=$1)`, ErrUsageDenied},
		{"missing owner", `DELETE FROM creative_assets WHERE content_revision_id=$1`, ErrMissingRoot},
		{"wrong identity", `UPDATE creative_assets SET kind='link' WHERE content_revision_id=$1`, ErrMissingRoot},
		{"expired hold", `DELETE FROM creative_assets WHERE content_revision_id=$1`, ErrMissingRoot},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			id := ids[i]
			if tc.name == "expired hold" {
				if _, err := db.Exec(`INSERT INTO creative_revision_holds(account_id,id,content_revision_id,reason,expires_at) VALUES ('batch','expired',$1,'backup',clock_timestamp()-interval '1 hour')`, id); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := db.Exec(tc.query, id); err != nil {
				t.Fatal(err)
			}
			err := scope.WithReadSnapshot(t.Context(), func(tx store.ReadTxAccountScope) error {
				results, err := ReadDisplayBatch(t.Context(), tx, []string{id, ids[99], "missing"})
				if err != nil {
					return err
				}
				if !errors.Is(results[id].Err, tc.want) || results[id].Revision.ID != "" {
					t.Fatalf("denied data leaked: %+v", results[id])
				}
				if results[ids[99]].Err != nil || !errors.Is(results["missing"].Err, ErrNotFound) {
					t.Fatal(results)
				}
				_, err = ReadInSnapshot(t.Context(), tx, id, "display")
				if !errors.Is(err, tc.want) {
					t.Fatalf("single: %v", err)
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
		})
	}
	other := st.ScopeFor(auth.AccountContext{AccountID: "other"})
	if err := other.WithReadSnapshot(t.Context(), func(tx store.ReadTxAccountScope) error {
		results, err := ReadDisplayBatch(t.Context(), tx, ids)
		if err != nil {
			return err
		}
		for _, r := range results {
			if !errors.Is(r.Err, ErrNotFound) {
				t.Fatalf("cross-account content: %+v", r)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
