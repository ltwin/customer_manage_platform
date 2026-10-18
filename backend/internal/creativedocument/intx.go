package creativedocument

import (
	"context"
	"encoding/json"

	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// CommandInTx applies a batch inside a transaction the caller already holds,
// for a write that must commit together with other effects (a scene client
// job's result and the run it resumes). The caller's operation owns the
// receipt, the capability check and any run guard; this is commandInTx with
// the same payload and actor rules as CommandAs.
func CommandInTx(ctx context.Context, tx store.TxAccountScope, actor Actor, payload json.RawMessage) (CommandResult, error) {
	if actor.Kind == "agent" && actor.RunID == nil || actor.Kind != "agent" && actor.Kind != "photographer" {
		return CommandResult{}, creativeops.ErrValidation
	}
	in, err := decodeCommand(payload)
	if err != nil {
		return CommandResult{}, err
	}
	return commandInTx(ctx, tx, actor, in)
}

// ReadHeadInTx reads a document row in the caller's transaction without locking
// it. A document of another type is reported as not found.
func ReadHeadInTx(ctx context.Context, tx store.TxAccountScope, id, typeKey string) (Document, error) {
	head, err := readHead(ctx, tx, id)
	if err != nil {
		return Document{}, err
	}
	if head.TypeKey != typeKey {
		return Document{}, ErrNotFound
	}
	return head, nil
}

// ReadCurrentInTx reads a document's current head and snapshot in the caller's
// transaction without locking the head, for checks that a later command in the
// same transaction will repeat under the lock.
func ReadCurrentInTx(ctx context.Context, tx store.TxAccountScope, id, typeKey string) (Current, error) {
	head, err := ReadHeadInTx(ctx, tx, id, typeKey)
	if err != nil {
		return Current{}, err
	}
	return loadInTx(ctx, tx, head, 0)
}

// ReadAtInTx reads a document at one revision (0 is the current one) in the
// caller's write transaction. A document of another type is not found.
func ReadAtInTx(ctx context.Context, tx store.TxAccountScope, id, typeKey string, revision int64) (Current, error) {
	head, err := ReadHeadInTx(ctx, tx, id, typeKey)
	if err != nil {
		return Current{}, err
	}
	return loadInTx(ctx, tx, head, revision)
}

// ReadAtInSnapshot is ReadAtInTx in a read snapshot.
func ReadAtInSnapshot(ctx context.Context, tx store.ReadTxAccountScope, id, typeKey string, revision int64) (Current, error) {
	if err := tx.RequireCreativeRead(ctx); err != nil {
		return Current{}, err
	}
	head, err := readHead(ctx, tx, id)
	if err != nil {
		return Current{}, err
	}
	if head.TypeKey != typeKey {
		return Current{}, ErrNotFound
	}
	return loadInSnapshot(ctx, tx, head, revision)
}

// PageReader is the part of a transaction the history readers use; both a
// write transaction and a read snapshot have it.
type PageReader interface {
	QueryPage(ctx context.Context, table, columns, cond string, order []store.OrderBy, limit, offset int, args ...any) (store.Rows, error)
}

// HistoryRow is who changed which units in one revision.
type HistoryRow struct {
	Revision     int64
	ActorKind    string
	RunID        string
	RestoredFrom int64 // 0 when the revision is not a restore
	Changed      []string
	Removed      []string
}

// WholeRestore reports a revision that put the whole document back.
func (r HistoryRow) WholeRestore() bool { return len(r.Changed) == 1 && r.Changed[0] == "*" }

// History lists a document's revisions after one, oldest first, at most limit.
func History(ctx context.Context, tx PageReader, documentID string, after int64, limit int) ([]HistoryRow, error) {
	rows, err := tx.QueryPage(ctx, "creative_document_revisions", "revision,actor_kind,run_id,restored_from,changed_units,removed_units",
		"document_id=$2 AND revision>$3", []store.OrderBy{{Column: "revision"}}, limit, 0, documentID, after)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []HistoryRow
	for rows.Next() {
		var r HistoryRow
		var run *string
		var restored *int64
		if err := rows.Scan(&r.Revision, &r.ActorKind, &run, &restored, &r.Changed, &r.Removed); err != nil {
			return nil, err
		}
		if run != nil {
			r.RunID = *run
		}
		if restored != nil {
			r.RestoredFrom = *restored
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// RunRevisions lists the revisions one run wrote to a document, oldest first,
// at most limit.
func RunRevisions(ctx context.Context, tx PageReader, documentID, runID string, limit int) ([]int64, error) {
	rows, err := tx.QueryPage(ctx, "creative_document_revisions", "revision", "document_id=$2 AND run_id=$3",
		[]store.OrderBy{{Column: "revision"}}, limit, 0, documentID, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var r int64
		if err := rows.Scan(&r); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// LastRunRevision is the newest revision any of the runs wrote to a document,
// 0 when none did.
func LastRunRevision(ctx context.Context, tx PageReader, documentID string, runIDs []string) (int64, error) {
	if len(runIDs) == 0 {
		return 0, nil
	}
	rows, err := tx.QueryPage(ctx, "creative_document_revisions", "revision", "document_id=$2 AND run_id = ANY($3)",
		[]store.OrderBy{{Column: "revision", Desc: true}}, 1, 0, documentID, runIDs)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	var last int64
	if rows.Next() {
		if err := rows.Scan(&last); err != nil {
			return 0, err
		}
	}
	return last, rows.Err()
}

// RunDocuments lists the documents of one type the runs wrote, the most
// recently written first, at most limit. Documents in the trash are left out.
func RunDocuments(ctx context.Context, tx PageReader, typeKey string, runIDs []string, limit int) ([]string, error) {
	out := []string{}
	for len(out) < limit && len(runIDs) > 0 {
		rows, err := tx.QueryPage(ctx, "creative_document_revisions", "document_id",
			"run_id = ANY($2) AND NOT (document_id = ANY($3)) AND document_id IN (SELECT id FROM creative_documents WHERE account_id=$1 AND type_key=$4 AND trashed_at IS NULL)",
			[]store.OrderBy{{Column: "created_at", Desc: true}, {Column: "revision", Desc: true}}, 1, 0, runIDs, out, typeKey)
		if err != nil {
			return nil, err
		}
		var id string
		found := rows.Next()
		if found {
			err = rows.Scan(&id)
		}
		if err == nil {
			err = rows.Err()
		}
		rows.Close()
		if err != nil {
			return nil, err
		}
		if !found {
			break
		}
		out = append(out, id)
	}
	return out, nil
}
