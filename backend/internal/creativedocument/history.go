package creativedocument

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

const (
	defaultRevisionLimit = 50
	maxRevisionLimit     = 100
	maxViewUnits         = 200
)

// RevisionQuery filters a document's history.
type RevisionQuery struct {
	RunID         string
	SinceRevision int64 // 0 means from the first revision
	Limit         int
	Cursor        string
}

// RevisionItem is one history entry without its snapshot.
type RevisionItem struct {
	Revision       creativeops.Revision  `json:"revision"`
	ActorKind      string                `json:"actor_kind"`
	RunID          *string               `json:"run_id"`
	RestoredFrom   *creativeops.Revision `json:"restored_from"`
	ChangedUnits   []string              `json:"changed_units"`
	AnnotatedUnits []string              `json:"annotated_units"`
	RemovedUnits   []string              `json:"removed_units"`
	Title          string                `json:"title"`
	CreatedAt      time.Time             `json:"created_at"`
}

// RevisionPage is newest first; NextCursor is nil on the last page.
type RevisionPage struct {
	Items      []RevisionItem `json:"items"`
	NextCursor *string        `json:"next_cursor"`
}

type revisionCursor struct {
	Query string `json:"query"`
	Below int64  `json:"below"`
}

func cursorQuery(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:])
}

// ListRevisions pages a document's history, optionally only one run's writes
// or only what came after a revision.
func ListRevisions(ctx context.Context, scope store.AccountScope, documentID string, q RevisionQuery) (RevisionPage, error) {
	if q.Limit == 0 {
		q.Limit = defaultRevisionLimit
	}
	if q.Limit < 1 || q.Limit > maxRevisionLimit || q.SinceRevision < 0 {
		return RevisionPage{}, creativeops.Explain(creativeops.ErrValidation, "limit 必须在 1 到 %d 之间", maxRevisionLimit)
	}
	bound := cursorQuery(scope.AccountID(), documentID, q.RunID, fmt.Sprint(q.SinceRevision))
	below := int64(0)
	if q.Cursor != "" {
		data, err := base64.RawURLEncoding.DecodeString(q.Cursor)
		var c revisionCursor
		if err != nil || len(q.Cursor) > 512 || creativeops.Decode(data, &c) != nil || c.Query != bound || c.Below < 1 {
			return RevisionPage{}, creativeops.Explain(creativeops.ErrValidation, "游标无效")
		}
		below = c.Below
	}
	page := RevisionPage{Items: []RevisionItem{}}
	err := scope.WithReadSnapshot(ctx, func(tx store.ReadTxAccountScope) error {
		if err := tx.RequireCreativeRead(ctx); err != nil {
			return err
		}
		if _, err := readHead(ctx, tx, documentID); err != nil {
			return err
		}
		cond := "document_id=$2 AND revision>$3 AND ($4::bigint=0 OR revision<$4) AND ($5::text='' OR run_id=$5)"
		rows, err := tx.QueryPage(ctx, "creative_document_revisions", "revision,actor_kind,run_id,restored_from,changed_units,annotated_units,removed_units,title,created_at", cond,
			[]store.OrderBy{{Column: "revision", Desc: true}}, q.Limit+1, 0, documentID, q.SinceRevision, below, q.RunID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var item RevisionItem
			var revision int64
			var restored *int64
			if err := rows.Scan(&revision, &item.ActorKind, &item.RunID, &restored, &item.ChangedUnits, &item.AnnotatedUnits, &item.RemovedUnits, &item.Title, &item.CreatedAt); err != nil {
				return err
			}
			item.Revision = creativeops.Revision(revision)
			if restored != nil {
				item.RestoredFrom = rev(*restored)
			}
			page.Items = append(page.Items, item)
		}
		return rows.Err()
	})
	if err != nil {
		return RevisionPage{}, err
	}
	if len(page.Items) > q.Limit {
		page.Items = page.Items[:q.Limit]
		raw, err := json.Marshal(revisionCursor{Query: bound, Below: int64(page.Items[q.Limit-1].Revision)})
		if err != nil {
			return RevisionPage{}, err
		}
		next := base64.RawURLEncoding.EncodeToString(raw)
		page.NextCursor = &next
	}
	return page, nil
}

type revisionView struct {
	Revision       creativeops.Revision  `json:"revision"`
	Title          string                `json:"title"`
	ActorKind      string                `json:"actor_kind"`
	RunID          *string               `json:"run_id"`
	RestoredFrom   *creativeops.Revision `json:"restored_from"`
	ChangedUnits   []string              `json:"changed_units"`
	AnnotatedUnits []string              `json:"annotated_units"`
	RemovedUnits   []string              `json:"removed_units"`
	UnitRevs       map[string]string     `json:"unit_revs"`
	CreatedAt      time.Time             `json:"created_at"`
	View           json.RawMessage       `json:"view"`
}

// ReadRevision returns one old revision, narrowed to units when given.
func ReadRevision(ctx context.Context, scope store.AccountScope, documentID string, revision int64, units []string) (json.RawMessage, error) {
	if revision < 1 || len(units) > maxViewUnits {
		return nil, creativeops.Explain(creativeops.ErrValidation, "修订号或单元数量不合法")
	}
	for _, u := range units {
		if !unitName.MatchString(u) {
			return nil, creativeops.Explain(creativeops.ErrValidation, "单元名 %q 不合法", u)
		}
	}
	var out json.RawMessage
	err := scope.WithReadSnapshot(ctx, func(tx store.ReadTxAccountScope) error {
		if err := tx.RequireCreativeRead(ctx); err != nil {
			return err
		}
		head, err := readHead(ctx, tx, documentID)
		if err != nil {
			return err
		}
		t, ok := Lookup(head.TypeKey)
		if !ok {
			return ErrNotFound
		}
		var v revisionView
		var restored *int64
		var unitsRaw []byte
		err = tx.QueryRow(ctx, "creative_document_revisions", "title,actor_kind,run_id,restored_from,changed_units,annotated_units,removed_units,unit_revs,created_at", "document_id=$2 AND revision=$3", documentID, revision).
			Scan(&v.Title, &v.ActorKind, &v.RunID, &restored, &v.ChangedUnits, &v.AnnotatedUnits, &v.RemovedUnits, &unitsRaw, &v.CreatedAt)
		if errors.Is(err, store.ErrNoRows) {
			return ErrRevisionNotFound
		}
		if err != nil {
			return err
		}
		cur, err := loadInSnapshot(ctx, tx, head, revision)
		if err != nil {
			return err
		}
		if v.View, err = t.View(ctx, tx, cur, units); err != nil {
			return err
		}
		v.Revision, v.UnitRevs = creativeops.Revision(revision), unitRevsJSON(cur.UnitRevs)
		if restored != nil {
			v.RestoredFrom = rev(*restored)
		}
		out, err = json.Marshal(v)
		return err
	})
	return out, err
}
