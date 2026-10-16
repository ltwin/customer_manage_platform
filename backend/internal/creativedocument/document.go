package creativedocument

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/samson/customer-manage-platform/backend/internal/creativecontent"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

var (
	ErrNotFound = errors.New("creative document not found")
	// ErrRevisionNotFound is an existing document without that revision on
	// record, for example one older than the migration that added history.
	ErrRevisionNotFound = errors.New("creative document revision not found")
	ErrTrashed          = errors.New("creative document is in trash")
	ErrNotTrashed       = errors.New("creative document is not in trash")
	ErrSizeLimit        = errors.New("creative document over size limit")
	ErrConflict         = errors.New("creative document changed")
	// ErrTypeContract means a registered type broke the port contract; it is a
	// server bug, never the caller's fault.
	ErrTypeContract = errors.New("creative document type broke its contract")
)

const (
	maxSnapshotBytes = 512 << 10
	maxUnits         = 4096
	maxTitleRunes    = 200
	defaultTitle     = "未命名"
)

var unitName = regexp.MustCompile(`^[A-Za-z0-9_:.\-]{1,120}$`)

// Conflict is one unit whose current state differs from what the caller
// expected; nil means "does not exist". A whole-document restore reports "*".
type Conflict struct {
	Unit        string                `json:"unit"`
	ExpectedRev *creativeops.Revision `json:"expected_rev"`
	CurrentRev  *creativeops.Revision `json:"current_rev"`
}

// ConflictError carries every conflicting unit; errors.Is matches ErrConflict.
type ConflictError struct{ Conflicts []Conflict }

func (e *ConflictError) Error() string {
	return fmt.Sprintf("%v: %d unit(s)", ErrConflict, len(e.Conflicts))
}
func (e *ConflictError) Is(target error) bool { return target == ErrConflict }

// Document is the head row.
type Document struct {
	ID                string
	TypeKey           string
	Title             string
	ContentRevisionID string
	Revision          int64
	UnitRevs          map[string]int64
	TrashedAt         *time.Time
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

const headColumns = "id,type_key,title,content_revision_id,revision,unit_revs,trashed_at,created_at,updated_at"

type rowReader interface {
	QueryRow(ctx context.Context, table, columns, cond string, args ...any) store.Row
}
type rowsReader interface {
	rowReader
	Query(ctx context.Context, table, columns, cond string, args ...any) (store.Rows, error)
}

func scanHead(row store.Row) (Document, error) {
	var d Document
	var units []byte
	if err := row.Scan(&d.ID, &d.TypeKey, &d.Title, &d.ContentRevisionID, &d.Revision, &units, &d.TrashedAt, &d.CreatedAt, &d.UpdatedAt); err != nil {
		if errors.Is(err, store.ErrNoRows) {
			return Document{}, ErrNotFound
		}
		return Document{}, err
	}
	if err := json.Unmarshal(units, &d.UnitRevs); err != nil {
		return Document{}, err
	}
	return d, nil
}

func readHead(ctx context.Context, tx rowReader, id string) (Document, error) {
	return scanHead(tx.QueryRow(ctx, "creative_documents", headColumns, "id=$2", id))
}

func lockHead(ctx context.Context, tx store.TxAccountScope, id string) (Document, error) {
	return scanHead(tx.QueryRowForUpdate(ctx, "creative_documents", headColumns, "id=$2", id))
}

func refsOf(ctx context.Context, tx rowsReader, documentID string, revision int64) ([]string, error) {
	rows, err := tx.Query(ctx, "creative_document_content_refs", "content_revision_id", "document_id=$2 AND revision=$3", documentID, revision)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	sort.Strings(out)
	return out, rows.Err()
}

type revisionRow struct {
	Title             string
	ContentRevisionID string
	UnitRevs          map[string]int64
}

func readRevisionRow(ctx context.Context, tx rowReader, documentID string, revision int64) (revisionRow, error) {
	var r revisionRow
	var units []byte
	err := tx.QueryRow(ctx, "creative_document_revisions", "title,content_revision_id,unit_revs", "document_id=$2 AND revision=$3", documentID, revision).Scan(&r.Title, &r.ContentRevisionID, &units)
	if errors.Is(err, store.ErrNoRows) {
		return r, ErrRevisionNotFound
	}
	if err != nil {
		return r, err
	}
	return r, json.Unmarshal(units, &r.UnitRevs)
}

// loadInTx reads one revision in a write transaction; revision 0 means current.
func loadInTx(ctx context.Context, tx store.TxAccountScope, head Document, revision int64) (Current, error) {
	cur := Current{Document: head, Revision: head.Revision, Title: head.Title, UnitRevs: head.UnitRevs}
	contentID := head.ContentRevisionID
	if revision != 0 && revision != head.Revision {
		row, err := readRevisionRow(ctx, tx, head.ID, revision)
		if err != nil {
			return Current{}, err
		}
		cur.Revision, cur.Title, cur.UnitRevs, contentID = revision, row.Title, row.UnitRevs, row.ContentRevisionID
	}
	content, err := creativecontent.RequireUsable(ctx, tx, contentID, "display")
	if err != nil {
		return Current{}, err
	}
	cur.Content = content
	cur.Refs, err = refsOf(ctx, tx, head.ID, cur.Revision)
	return cur, err
}

// loadInSnapshot is loadInTx for read transactions.
func loadInSnapshot(ctx context.Context, tx store.ReadTxAccountScope, head Document, revision int64) (Current, error) {
	cur := Current{Document: head, Revision: head.Revision, Title: head.Title, UnitRevs: head.UnitRevs}
	contentID := head.ContentRevisionID
	if revision != 0 && revision != head.Revision {
		row, err := readRevisionRow(ctx, tx, head.ID, revision)
		if err != nil {
			return Current{}, err
		}
		cur.Revision, cur.Title, cur.UnitRevs, contentID = revision, row.Title, row.UnitRevs, row.ContentRevisionID
	}
	content, err := creativecontent.ReadInSnapshot(ctx, tx, contentID, "display")
	if err != nil {
		return Current{}, err
	}
	cur.Content = content
	cur.Refs, err = refsOf(ctx, tx, head.ID, cur.Revision)
	return cur, err
}

func normalizeTitle(title string) (string, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return "", creativeops.Explain(creativeops.ErrValidation, "标题不能为空")
	}
	if utf8.RuneCountInString(title) > maxTitleRunes {
		return "", creativeops.Explain(creativeops.ErrValidation, "标题最多 %d 个字", maxTitleRunes)
	}
	return title, nil
}

// CreateInTx creates a document with its first revision inside the caller's
// transaction; add_node uses it so the node and its document commit together.
func CreateInTx(ctx context.Context, tx store.TxAccountScope, t Type, title string, opts json.RawMessage, actor Actor) (Document, error) {
	if !t.Creatable() {
		return Document{}, creativeops.Explain(creativeops.ErrValidation, "不能新建 %q 文档", t.Key())
	}
	title, err := createTitle(title)
	if err != nil {
		return Document{}, err
	}
	snapshot, err := t.Empty(ctx, tx, title, opts)
	if err != nil {
		return Document{}, err
	}
	draft := creativecontent.Draft{Kind: t.ContentKind(), Payload: snapshot.Payload, Rights: creativecontent.RightsDeclarationInput{}.OrDefault()}
	return create(ctx, tx, t, title, draft, snapshot.Refs, actor)
}

// CreateInternalInTx creates an internal.document from a ready text draft,
// keeping the draft's own rights. Only the opt-in integration harness calls
// it; production never creates this type.
func CreateInternalInTx(ctx context.Context, tx store.TxAccountScope, title string, draft creativecontent.Draft, actor Actor) (Document, error) {
	if draft.Kind != "text" || draft.DeclarationID != "" {
		return Document{}, creativeops.Explain(creativeops.ErrValidation, "内部文档只接受新的文本内容")
	}
	title, err := createTitle(title)
	if err != nil {
		return Document{}, err
	}
	return create(ctx, tx, internalType{}, title, draft, nil, actor)
}

func createTitle(title string) (string, error) {
	if strings.TrimSpace(title) == "" {
		title = defaultTitle
	}
	return normalizeTitle(title)
}

func create(ctx context.Context, tx store.TxAccountScope, t Type, title string, draft creativecontent.Draft, refs []string, actor Actor) (Document, error) {
	units, err := t.Units(draft.Payload)
	if err != nil {
		return Document{}, err
	}
	revs := map[string]int64{MetaUnit: 1}
	for _, u := range units {
		if !unitName.MatchString(u) || u == MetaUnit {
			return Document{}, fmt.Errorf("%w: unit %q", ErrTypeContract, u)
		}
		revs[u] = 1
	}
	if len(revs) > maxUnits {
		return Document{}, ErrSizeLimit
	}
	refs = dedupe(refs)
	for _, ref := range refs {
		if _, err := creativecontent.RequireUsable(ctx, tx, ref, "display"); err != nil {
			return Document{}, err
		}
	}
	if err := checkPayloadSize(draft.Payload); err != nil {
		return Document{}, err
	}
	unitJSON, err := json.Marshal(revs)
	if err != nil {
		return Document{}, err
	}
	d := Document{ID: "cwdoc_" + uuid.NewString(), TypeKey: t.Key(), Title: title, Revision: 1, UnitRevs: revs}
	r, err := creativecontent.WriteAndRetain(ctx, tx, draft, "", nil, func(r creativecontent.Revision) error {
		if err := tx.Insert(ctx, "creative_documents", []string{"id", "type_key", "title", "content_revision_id", "revision", "unit_revs"}, d.ID, d.TypeKey, d.Title, r.ID, int64(1), unitJSON); err != nil {
			return err
		}
		return insertRevision(ctx, tx, d.ID, 1, r.ID, title, actor, nil, []string{"*"}, nil, nil, unitJSON)
	})
	if err != nil {
		return Document{}, err
	}
	d.ContentRevisionID = r.ID
	if err := insertRefs(ctx, tx, d.ID, 1, refs, nil); err != nil {
		return Document{}, err
	}
	return readHead(ctx, tx, d.ID)
}

func insertRevision(ctx context.Context, tx store.TxAccountScope, documentID string, revision int64, contentRevisionID, title string, actor Actor, restoredFrom *int64, changed, annotated, removed []string, unitJSON []byte) error {
	return tx.Insert(ctx, "creative_document_revisions",
		[]string{"document_id", "revision", "content_revision_id", "title", "actor_kind", "run_id", "restored_from", "changed_units", "annotated_units", "removed_units", "unit_revs"},
		documentID, revision, contentRevisionID, title, actor.Kind, actor.RunID, restoredFrom, orEmpty(changed), orEmpty(annotated), orEmpty(removed), unitJSON)
}

func insertRefs(ctx context.Context, tx store.TxAccountScope, documentID string, revision int64, refs []string, already map[string]bool) error {
	rows := make([][]any, 0, len(refs))
	for _, ref := range refs {
		if !already[ref] {
			rows = append(rows, []any{documentID, revision, ref})
		}
	}
	if len(rows) == 0 {
		return nil
	}
	return tx.InsertRows(ctx, "creative_document_content_refs", []string{"document_id", "revision", "content_revision_id"}, rows)
}

func checkPayloadSize(p creativecontent.Payload) error {
	raw, err := json.Marshal(p)
	if err != nil {
		return err
	}
	if len(raw) > maxSnapshotBytes {
		return ErrSizeLimit
	}
	return nil
}

func dedupe(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, v := range in {
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	sort.Strings(out)
	return out
}

func rev(n int64) *creativeops.Revision {
	r := creativeops.Revision(n)
	return &r
}

func unitRevsJSON(revs map[string]int64) map[string]string {
	out := make(map[string]string, len(revs))
	for k, v := range revs {
		out[k] = strconv.FormatInt(v, 10)
	}
	return out
}

// view is the generic part of every read response.
type view struct {
	ID                string               `json:"id"`
	TypeKey           string               `json:"type_key"`
	Title             string               `json:"title"`
	Revision          creativeops.Revision `json:"revision"`
	ContentRevisionID string               `json:"content_revision_id"`
	UnitRevs          map[string]string    `json:"unit_revs"`
	TrashedAt         *time.Time           `json:"trashed_at"`
	CreatedAt         time.Time            `json:"created_at"`
	UpdatedAt         time.Time            `json:"updated_at"`
}

// merge puts the type's fields beside the generic ones; a type may not
// shadow a generic field.
func merge(generic any, typed json.RawMessage) (json.RawMessage, error) {
	raw, err := json.Marshal(generic)
	if err != nil {
		return nil, err
	}
	var out map[string]json.RawMessage
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	if len(typed) > 0 {
		var extra map[string]json.RawMessage
		if err := json.Unmarshal(typed, &extra); err != nil {
			return nil, fmt.Errorf("%w: view is not an object", ErrTypeContract)
		}
		for k, v := range extra {
			if _, ok := out[k]; ok {
				return nil, fmt.Errorf("%w: view shadows %q", ErrTypeContract, k)
			}
			out[k] = v
		}
	}
	return json.Marshal(out)
}

// Read returns a document's current state with its type's fields.
func Read(ctx context.Context, scope store.AccountScope, id string) (json.RawMessage, error) {
	var out json.RawMessage
	err := scope.WithReadSnapshot(ctx, func(tx store.ReadTxAccountScope) error {
		if err := tx.RequireCreativeRead(ctx); err != nil {
			return err
		}
		head, err := readHead(ctx, tx, id)
		if err != nil {
			return err
		}
		t, ok := Lookup(head.TypeKey)
		if !ok {
			return ErrNotFound
		}
		cur, err := loadInSnapshot(ctx, tx, head, 0)
		if err != nil {
			return err
		}
		typed, err := t.View(ctx, tx, cur, nil)
		if err != nil {
			return err
		}
		out, err = merge(view{ID: head.ID, TypeKey: head.TypeKey, Title: head.Title, Revision: creativeops.Revision(head.Revision), ContentRevisionID: head.ContentRevisionID,
			UnitRevs: unitRevsJSON(head.UnitRevs), TrashedAt: head.TrashedAt, CreatedAt: head.CreatedAt, UpdatedAt: head.UpdatedAt}, typed)
		return err
	})
	return out, err
}

// RequirePlaceable checks that an existing document can get another node of
// nodeTypeKey: it exists, is not in trash and maps to that node type.
func RequirePlaceable(ctx context.Context, tx store.TxAccountScope, id, nodeTypeKey string) (Document, error) {
	head, err := readHead(ctx, tx, id)
	if err != nil {
		return Document{}, err
	}
	t, ok := Lookup(head.TypeKey)
	if !ok {
		return Document{}, ErrNotFound
	}
	if t.NodeTypeKey() != nodeTypeKey {
		return Document{}, creativeops.Explain(creativeops.ErrValidation, "文档 %s 不能放进 %q 节点", id, nodeTypeKey)
	}
	if head.TrashedAt != nil {
		return Document{}, ErrTrashed
	}
	return head, nil
}

// NodeSummary is what a canvas node shows for the document behind it.
type NodeSummary struct {
	Exists  bool
	Usable  bool
	Trashed bool
	Title   string
	TypeKey string
	Summary json.RawMessage
	// Content is the current snapshot, for types whose card previews it.
	Content creativecontent.Revision
}

// SummaryBatch reads the documents behind canvas nodes in one snapshot.
// Missing, unregistered or unusable documents come back with Usable false.
func SummaryBatch(ctx context.Context, tx store.ReadTxAccountScope, ids []string) (map[string]NodeSummary, error) {
	out := make(map[string]NodeSummary, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := tx.Query(ctx, "creative_documents", headColumns, "id=ANY($2::text[])", ids)
	if err != nil {
		return nil, err
	}
	var heads []Document
	for rows.Next() {
		d, err := scanHead(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		heads = append(heads, d)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	summaries, err := summarize(ctx, tx, heads)
	if err != nil {
		return nil, err
	}
	for _, d := range heads {
		s := NodeSummary{Exists: true, Trashed: d.TrashedAt != nil, Title: d.Title, TypeKey: d.TypeKey}
		h := summaries[d.ID]
		if h.Err != nil && !errors.Is(h.Err, creativecontent.ErrUsageDenied) {
			return nil, h.Err
		}
		s.Usable, s.Summary, s.Content = h.Usable, h.Summary, h.Content
		out[d.ID] = s
	}
	return out, nil
}

// headSummary is one document's type summary. Usable is false when its type
// is not registered or its snapshot cannot be displayed; Err says why for the
// latter.
type headSummary struct {
	Usable  bool
	Summary json.RawMessage
	Content creativecontent.Revision
	Err     error
}

// summarize reads the current snapshots and references of many documents in
// one query each, then asks every type for its summary.
func summarize(ctx context.Context, tx store.ReadTxAccountScope, heads []Document) (map[string]headSummary, error) {
	out := make(map[string]headSummary, len(heads))
	contentIDs := make([]string, 0, len(heads))
	for _, d := range heads {
		contentIDs = append(contentIDs, d.ContentRevisionID)
	}
	contents, err := creativecontent.ReadDisplayBatch(ctx, tx, contentIDs)
	if err != nil {
		return nil, err
	}
	refs, err := currentRefs(ctx, tx, heads)
	if err != nil {
		return nil, err
	}
	for _, d := range heads {
		t, ok := Lookup(d.TypeKey)
		result := contents[d.ContentRevisionID]
		if !ok || result.Err != nil {
			out[d.ID] = headSummary{Err: result.Err}
			continue
		}
		cur := Current{Document: d, Revision: d.Revision, Title: d.Title, UnitRevs: d.UnitRevs, Content: result.Revision, Refs: refs[d.ID]}
		summary, err := t.Summary(ctx, tx, cur)
		if err != nil {
			return nil, err
		}
		out[d.ID] = headSummary{Usable: true, Summary: summary, Content: result.Revision}
	}
	return out, nil
}

// currentRefs reads the references of every head's current revision at once.
func currentRefs(ctx context.Context, tx rowsReader, heads []Document) (map[string][]string, error) {
	out := make(map[string][]string, len(heads))
	if len(heads) == 0 {
		return out, nil
	}
	ids, revisions := make([]string, len(heads)), make([]int64, len(heads))
	for i, d := range heads {
		ids[i], revisions[i] = d.ID, d.Revision
	}
	rows, err := tx.Query(ctx, "creative_document_content_refs", "document_id,content_revision_id",
		"(document_id,revision) IN (SELECT * FROM unnest($2::text[],$3::bigint[]))", ids, revisions)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, ref string
		if err := rows.Scan(&id, &ref); err != nil {
			return nil, err
		}
		out[id] = append(out[id], ref)
	}
	for _, refs := range out {
		sort.Strings(refs)
	}
	return out, rows.Err()
}
