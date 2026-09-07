package creativeworkspace

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// PostgresRepository 是本域唯一接触 store.AccountScope 的层。
// 所有查询由 scope 强制 account_id 过滤（ADR-001）；cond / columns 全部是编译期常量。
type PostgresRepository struct{}

func NewPostgresRepository() PostgresRepository { return PostgresRepository{} }

type scope interface {
	AccountID() string
	Query(context.Context, string, string, string, ...any) (store.Rows, error)
	QueryRow(context.Context, string, string, string, ...any) store.Row
	QueryRowForUpdate(context.Context, string, string, string, ...any) store.Row
	QueryPage(context.Context, string, string, string, []store.OrderBy, int, int, ...any) (store.Rows, error)
	Count(context.Context, string, string, ...any) (int64, error)
	Exists(context.Context, string, string, ...any) (bool, error)
	Insert(context.Context, string, []string, ...any) error
	Update(context.Context, string, string, string, ...any) (int64, error)
	Delete(context.Context, string, string, ...any) (int64, error)
	Upsert(context.Context, string, []string, []string, []string, ...any) error
	ScalarAggregate(context.Context, string, string, string, string, ...any) store.Row
}

const (
	workspaceColumns = "id, kind, name, link_order_id, link_customer_id, archived_at, revision, created_at, updated_at, last_opened_at"
	cardColumns      = "id, card_type, text_body, link_url, asset_id, caption, source_class, batch_id, batch_seq, revision, created_at, archived_at"
	groupColumns     = "id, workspace_id, name, position, created_at"
	shootColumns     = "id, workspace_id, title, description, ref_asset_id, source_card_id, source_card_rev, source_card_ids, position, status, result, result_note, result_at, revision, created_at, updated_at"
	memoColumns      = "id, workspace_id, text_body, checked, position, created_at, updated_at"
	assetColumns     = "id, workspace_id, source_class, rights_basis, generation_reference_granted, original_media_type, original_size, original_width, original_height, original_checksum, display_media_type, display_size, display_width, display_height, display_checksum, created_at"
)

func newID(prefix string) string { return prefix + "_" + uuid.NewString() }

// ---- workspaces ----

type workspaceRow struct {
	Workspace
	linkOrderID    *string
	linkCustomerID *string
	archivedAt     *time.Time
}

func scanWorkspace(row interface{ Scan(...any) error }) (workspaceRow, error) {
	var r workspaceRow
	err := row.Scan(&r.ID, &r.Kind, &r.Name, &r.linkOrderID, &r.linkCustomerID, &r.archivedAt, &r.Revision, &r.CreatedAt, &r.UpdatedAt, &r.LastOpenedAt)
	if err != nil {
		return r, err
	}
	r.Archived = r.archivedAt != nil
	switch {
	case r.linkOrderID != nil:
		r.Link = &WorkspaceLink{Kind: LinkOrder, ID: *r.linkOrderID}
	case r.linkCustomerID != nil:
		r.Link = &WorkspaceLink{Kind: LinkCustomer, ID: *r.linkCustomerID}
	}
	return r, nil
}

func (PostgresRepository) InsertWorkspace(ctx context.Context, sc scope, kind WorkspaceKind, name string, now time.Time) (Workspace, error) {
	id := newID("cws")
	if err := sc.Insert(ctx, "creative_workspaces", []string{"id", "kind", "name", "revision", "created_at", "updated_at", "last_opened_at"},
		id, string(kind), name, 1, now, now, now); err != nil {
		return Workspace{}, fmt.Errorf("insert workspace: %w", err)
	}
	return Workspace{ID: id, Kind: kind, Name: name, Revision: 1, CreatedAt: now, UpdatedAt: now, LastOpenedAt: now}, nil
}

func (PostgresRepository) FindInbox(ctx context.Context, sc scope) (Workspace, error) {
	r, err := scanWorkspace(sc.QueryRow(ctx, "creative_workspaces", workspaceColumns, "kind = 'inbox'"))
	if errors.Is(err, store.ErrNoRows) {
		return Workspace{}, ErrNotFound
	}
	if err != nil {
		return Workspace{}, fmt.Errorf("find inbox: %w", err)
	}
	return r.Workspace, nil
}

func (PostgresRepository) GetWorkspace(ctx context.Context, sc scope, id string) (Workspace, error) {
	r, err := scanWorkspace(sc.QueryRow(ctx, "creative_workspaces", workspaceColumns, "id = $2", id))
	if errors.Is(err, store.ErrNoRows) {
		return Workspace{}, ErrNotFound
	}
	if err != nil {
		return Workspace{}, fmt.Errorf("get workspace: %w", err)
	}
	return r.Workspace, nil
}

func (PostgresRepository) LockWorkspace(ctx context.Context, sc scope, id string) (Workspace, error) {
	r, err := scanWorkspace(sc.QueryRowForUpdate(ctx, "creative_workspaces", workspaceColumns, "id = $2", id))
	if errors.Is(err, store.ErrNoRows) {
		return Workspace{}, ErrNotFound
	}
	if err != nil {
		return Workspace{}, fmt.Errorf("lock workspace: %w", err)
	}
	return r.Workspace, nil
}

func (PostgresRepository) ListWorkspaces(ctx context.Context, sc scope, includeArchived bool) ([]Workspace, error) {
	cond := ""
	if !includeArchived {
		cond = "archived_at IS NULL"
	}
	rows, err := sc.QueryPage(ctx, "creative_workspaces", workspaceColumns, cond,
		[]store.OrderBy{{Column: "last_opened_at", Desc: true}, {Column: "id", Desc: true}}, 500, 0)
	if err != nil {
		return nil, fmt.Errorf("list workspaces: %w", err)
	}
	defer rows.Close()
	out := make([]Workspace, 0, 16)
	for rows.Next() {
		r, err := scanWorkspace(rows)
		if err != nil {
			return nil, fmt.Errorf("scan workspace: %w", err)
		}
		out = append(out, r.Workspace)
	}
	return out, rows.Err()
}

// UpdateWorkspace 写入变化字段并递增 revision；expectedRevision>0 时 CAS。
func (PostgresRepository) UpdateWorkspace(ctx context.Context, sc scope, id string, expectedRevision int64, set map[string]any, now time.Time) error {
	// set 的键必须来自下方白名单（编译期常量），避免任何请求派生 SQL。
	clauses := make([]string, 0, len(set)+2)
	args := make([]any, 0, len(set)+4)
	next := 2
	for _, col := range []string{"name", "link_order_id", "link_customer_id", "archived_at", "last_opened_at"} {
		v, ok := set[col]
		if !ok {
			continue
		}
		clauses = append(clauses, fmt.Sprintf("%s = $%d", col, next))
		args = append(args, v)
		next++
	}
	clauses = append(clauses, fmt.Sprintf("updated_at = $%d", next))
	args = append(args, now)
	next++
	clauses = append(clauses, "revision = revision + 1")
	cond := fmt.Sprintf("id = $%d", next)
	args = append(args, id)
	next++
	if expectedRevision > 0 {
		cond += fmt.Sprintf(" AND revision = $%d", next)
		args = append(args, expectedRevision)
	}
	n, err := sc.Update(ctx, "creative_workspaces", strings.Join(clauses, ", "), cond, args...)
	if err != nil {
		return fmt.Errorf("update workspace: %w", err)
	}
	if n != 1 {
		if expectedRevision > 0 {
			return ErrRevisionConflict
		}
		return ErrNotFound
	}
	return nil
}

// WorkspaceSummaries 为列表补卡片数 / 封面 / 拍摄项数（不 JOIN：scope 只接受单表）。
func (PostgresRepository) WorkspaceSummaries(ctx context.Context, sc scope, ws []Workspace) error {
	if len(ws) == 0 {
		return nil
	}
	idx := make(map[string]int, len(ws))
	for i := range ws {
		idx[ws[i].ID] = i
	}
	// 卡片数与封面：遍历成员关系（按 position），首个图片卡作封面。
	rows, err := sc.QueryPage(ctx, "creative_card_memberships", "workspace_id, card_id", "",
		[]store.OrderBy{{Column: "workspace_id"}, {Column: "position"}}, 100000, 0)
	if err != nil {
		return fmt.Errorf("summaries memberships: %w", err)
	}
	cardWS := make(map[string]string, 256)
	orderedCards := make([]string, 0, 256)
	for rows.Next() {
		var wsID, cardID string
		if err := rows.Scan(&wsID, &cardID); err != nil {
			rows.Close()
			return err
		}
		if i, ok := idx[wsID]; ok {
			ws[i].CardCount++
			cardWS[cardID] = wsID
			orderedCards = append(orderedCards, cardID)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	if len(orderedCards) > 0 {
		crow, err := sc.Query(ctx, "creative_cards", "id, asset_id", "card_type = 'image' AND archived_at IS NULL AND asset_id IS NOT NULL")
		if err != nil {
			return fmt.Errorf("summaries cards: %w", err)
		}
		imageAsset := make(map[string]string, 128)
		for crow.Next() {
			var id, asset string
			if err := crow.Scan(&id, &asset); err != nil {
				crow.Close()
				return err
			}
			imageAsset[id] = asset
		}
		crow.Close()
		covered := make(map[string]bool, len(ws))
		coverAssets := make([]string, 0, len(ws))
		for _, cardID := range orderedCards {
			wsID := cardWS[cardID]
			if covered[wsID] {
				continue
			}
			if asset, ok := imageAsset[cardID]; ok {
				a := asset
				ws[idx[wsID]].CoverAssetID = &a
				covered[wsID] = true
				coverAssets = append(coverAssets, asset)
			}
		}
		if len(coverAssets) > 0 {
			arow, err := sc.Query(ctx, "creative_workspace_assets", "id, display_checksum", "")
			if err != nil {
				return fmt.Errorf("summaries assets: %w", err)
			}
			sums := make(map[string]string, len(coverAssets))
			for arow.Next() {
				var id, sum string
				if err := arow.Scan(&id, &sum); err != nil {
					arow.Close()
					return err
				}
				sums[id] = sum
			}
			arow.Close()
			for i := range ws {
				if ws[i].CoverAssetID != nil {
					if s, ok := sums[*ws[i].CoverAssetID]; ok {
						v := s
						ws[i].CoverChecksum = &v
					}
				}
			}
		}
	}
	srow, err := sc.Query(ctx, "creative_shoot_items", "workspace_id", "status = 'active'")
	if err != nil {
		return fmt.Errorf("summaries shoot items: %w", err)
	}
	defer srow.Close()
	for srow.Next() {
		var wsID string
		if err := srow.Scan(&wsID); err != nil {
			return err
		}
		if i, ok := idx[wsID]; ok {
			ws[i].ShootItemCount++
		}
	}
	return srow.Err()
}

// ResolveLinks 为关联补名称；目标不存在 / 已归档 / 已取消时标 Broken 但保留 ID。
func (PostgresRepository) ResolveLinks(ctx context.Context, sc scope, ws []Workspace) error {
	orderIDs := make([]string, 0, 8)
	customerIDs := make([]string, 0, 8)
	for _, w := range ws {
		if w.Link == nil {
			continue
		}
		if w.Link.Kind == LinkOrder {
			orderIDs = append(orderIDs, w.Link.ID)
		} else {
			customerIDs = append(customerIDs, w.Link.ID)
		}
	}
	type target struct {
		name, sub string
		broken    bool
	}
	orders := make(map[string]target, len(orderIDs))
	orderCustomer := make(map[string]string, len(orderIDs))
	if len(orderIDs) > 0 {
		rows, err := sc.Query(ctx, "orders", "id, title, status, customer_id", "id = ANY($2)", orderIDs)
		if err != nil {
			return fmt.Errorf("resolve order links: %w", err)
		}
		for rows.Next() {
			var id, status, custID string
			var title *string
			if err := rows.Scan(&id, &title, &status, &custID); err != nil {
				rows.Close()
				return err
			}
			t := target{sub: status, broken: status == "cancelled"}
			if title != nil {
				t.name = *title
			}
			orders[id] = t
			orderCustomer[id] = custID
			if t.name == "" {
				customerIDs = append(customerIDs, custID)
			}
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
	}
	customers := make(map[string]target, len(customerIDs))
	if len(customerIDs) > 0 {
		rows, err := sc.Query(ctx, "customers", "id, display_name, status", "id = ANY($2)", customerIDs)
		if err != nil {
			return fmt.Errorf("resolve customer links: %w", err)
		}
		for rows.Next() {
			var id, name, status string
			if err := rows.Scan(&id, &name, &status); err != nil {
				rows.Close()
				return err
			}
			customers[id] = target{name: name, broken: status != "active"}
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
	}
	for id, t := range orders {
		if t.name == "" {
			if c, ok := customers[orderCustomer[id]]; ok {
				t.name = c.name
				orders[id] = t
			}
		}
	}
	for i := range ws {
		l := ws[i].Link
		if l == nil {
			continue
		}
		var t target
		var ok bool
		if l.Kind == LinkOrder {
			t, ok = orders[l.ID]
		} else {
			t, ok = customers[l.ID]
		}
		if !ok {
			l.Broken = true
			continue
		}
		l.Name, l.Sub, l.Broken = t.name, t.sub, t.broken
	}
	return nil
}

func (PostgresRepository) LinkTargetExists(ctx context.Context, sc scope, link LinkInput) (bool, error) {
	switch link.Kind {
	case LinkOrder:
		return sc.Exists(ctx, "orders", "id = $2", link.ID)
	case LinkCustomer:
		return sc.Exists(ctx, "customers", "id = $2 AND status <> 'merged'", link.ID)
	}
	return false, nil
}

// WorkspacesLinkedTo 供订单 / 客户详情反向显示关联空间入口。
func (PostgresRepository) WorkspacesLinkedTo(ctx context.Context, sc scope, link LinkInput) ([]Workspace, error) {
	cond := "link_order_id = $2 AND archived_at IS NULL"
	if link.Kind == LinkCustomer {
		cond = "link_customer_id = $2 AND archived_at IS NULL"
	}
	rows, err := sc.QueryPage(ctx, "creative_workspaces", workspaceColumns, cond,
		[]store.OrderBy{{Column: "last_opened_at", Desc: true}}, 50, 0, link.ID)
	if err != nil {
		return nil, fmt.Errorf("workspaces linked to: %w", err)
	}
	defer rows.Close()
	out := make([]Workspace, 0, 4)
	for rows.Next() {
		r, err := scanWorkspace(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r.Workspace)
	}
	return out, rows.Err()
}

// ---- cards / memberships / groups ----

func scanCard(row interface{ Scan(...any) error }) (Card, error) {
	var c Card
	var text, url, asset, batchID *string
	var sourceClass *string
	var batchSeq *int
	var archivedAt *time.Time
	if err := row.Scan(&c.ID, &c.Type, &text, &url, &asset, &c.Caption, &sourceClass, &batchID, &batchSeq, &c.Revision, &c.CreatedAt, &archivedAt); err != nil {
		return c, err
	}
	if text != nil {
		c.Text = *text
	}
	if url != nil {
		c.URL = *url
	}
	if asset != nil {
		c.AssetID = *asset
	}
	if sourceClass != nil {
		s := SourceClass(*sourceClass)
		c.SourceClass = &s
	}
	if batchID != nil {
		c.BatchID = *batchID
	}
	if batchSeq != nil {
		c.BatchSeq = *batchSeq
	}
	c.Archived = archivedAt != nil
	return c, nil
}

// ListWorkspaceCards 返回空间内有效卡片（按 position），并填充图片卡的 checksum / 尺寸。
func (r PostgresRepository) ListWorkspaceCards(ctx context.Context, sc scope, workspaceID string) ([]Card, error) {
	mrows, err := sc.QueryPage(ctx, "creative_card_memberships", "card_id, group_id, position", "workspace_id = $2",
		[]store.OrderBy{{Column: "position"}, {Column: "card_id"}}, 5000, 0, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("list memberships: %w", err)
	}
	type mem struct {
		group *string
		pos   int
	}
	order := make([]string, 0, 64)
	mems := make(map[string]mem, 64)
	for mrows.Next() {
		var cardID string
		var group *string
		var pos int
		if err := mrows.Scan(&cardID, &group, &pos); err != nil {
			mrows.Close()
			return nil, err
		}
		order = append(order, cardID)
		mems[cardID] = mem{group: group, pos: pos}
	}
	mrows.Close()
	if err := mrows.Err(); err != nil {
		return nil, err
	}
	if len(order) == 0 {
		return []Card{}, nil
	}
	crows, err := sc.Query(ctx, "creative_cards", cardColumns, "id = ANY($2) AND archived_at IS NULL", order)
	if err != nil {
		return nil, fmt.Errorf("list cards: %w", err)
	}
	byID := make(map[string]Card, len(order))
	for crows.Next() {
		c, err := scanCard(crows)
		if err != nil {
			crows.Close()
			return nil, err
		}
		byID[c.ID] = c
	}
	crows.Close()
	out := make([]Card, 0, len(order))
	assetIDs := make([]string, 0, 16)
	for _, id := range order {
		c, ok := byID[id]
		if !ok {
			continue
		}
		c.WorkspaceID = workspaceID
		c.GroupID = mems[id].group
		c.Position = mems[id].pos
		if c.AssetID != "" {
			assetIDs = append(assetIDs, c.AssetID)
		}
		out = append(out, c)
	}
	if len(assetIDs) > 0 {
		if err := r.fillCardAssets(ctx, sc, out, assetIDs); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (PostgresRepository) fillCardAssets(ctx context.Context, sc scope, cards []Card, assetIDs []string) error {
	rows, err := sc.Query(ctx, "creative_workspace_assets", "id, display_checksum, display_width, display_height", "id = ANY($2)", assetIDs)
	if err != nil {
		return fmt.Errorf("card assets: %w", err)
	}
	defer rows.Close()
	type dim struct {
		sum  string
		w, h int
	}
	dims := make(map[string]dim, len(assetIDs))
	for rows.Next() {
		var id, sum string
		var w, h int
		if err := rows.Scan(&id, &sum, &w, &h); err != nil {
			return err
		}
		dims[id] = dim{sum, w, h}
	}
	for i := range cards {
		if d, ok := dims[cards[i].AssetID]; ok {
			cards[i].Checksum, cards[i].Width, cards[i].Height = d.sum, d.w, d.h
		}
	}
	return rows.Err()
}

func (PostgresRepository) GetCard(ctx context.Context, sc scope, id string) (Card, error) {
	c, err := scanCard(sc.QueryRow(ctx, "creative_cards", cardColumns, "id = $2", id))
	if errors.Is(err, store.ErrNoRows) {
		return Card{}, ErrNotFound
	}
	if err != nil {
		return Card{}, fmt.Errorf("get card: %w", err)
	}
	var group *string
	var pos int
	if err := sc.QueryRow(ctx, "creative_card_memberships", "workspace_id, group_id, position", "card_id = $2", id).Scan(&c.WorkspaceID, &group, &pos); err != nil && !errors.Is(err, store.ErrNoRows) {
		return Card{}, fmt.Errorf("get membership: %w", err)
	}
	c.GroupID, c.Position = group, pos
	return c, nil
}

func (PostgresRepository) MaxCardPosition(ctx context.Context, sc scope, workspaceID string) (int, error) {
	var max *int
	if err := sc.ScalarAggregate(ctx, "creative_card_memberships", store.AggregateMax, "position", "workspace_id = $2", workspaceID).Scan(&max); err != nil {
		return 0, fmt.Errorf("max position: %w", err)
	}
	if max == nil {
		return -1, nil
	}
	return *max, nil
}

func (PostgresRepository) InsertBatch(ctx context.Context, sc scope, workspaceID, sourceKind string, rawText *string, count int, now time.Time) (ImportBatch, error) {
	id := newID("cbt")
	if err := sc.Insert(ctx, "creative_import_batches", []string{"id", "workspace_id", "source_kind", "raw_text", "card_count", "created_at"},
		id, workspaceID, sourceKind, rawText, count, now); err != nil {
		return ImportBatch{}, fmt.Errorf("insert batch: %w", err)
	}
	return ImportBatch{ID: id, WorkspaceID: workspaceID, SourceKind: sourceKind, CardCount: count, CreatedAt: now}, nil
}

// InsertCard 写卡片行 + 成员关系行。
func (PostgresRepository) InsertCard(ctx context.Context, sc scope, c Card, now time.Time) (Card, error) {
	c.ID = newID("ccd")
	c.Revision = 1
	c.CreatedAt = now
	var text, url, asset, sourceClass, batchID any
	var batchSeq any
	if c.Type == CardText {
		text = c.Text
	}
	if c.Type == CardLink {
		url = c.URL
	}
	if c.Type == CardImage {
		asset = c.AssetID
		if c.SourceClass != nil {
			sourceClass = string(*c.SourceClass)
		}
	}
	if c.BatchID != "" {
		batchID, batchSeq = c.BatchID, c.BatchSeq
	}
	if err := sc.Insert(ctx, "creative_cards", []string{"id", "card_type", "text_body", "link_url", "asset_id", "caption", "source_class", "batch_id", "batch_seq", "revision", "created_at", "updated_at"},
		c.ID, string(c.Type), text, url, asset, c.Caption, sourceClass, batchID, batchSeq, 1, now, now); err != nil {
		return Card{}, fmt.Errorf("insert card: %w", err)
	}
	if err := sc.Insert(ctx, "creative_card_memberships", []string{"card_id", "workspace_id", "group_id", "position", "updated_at"},
		c.ID, c.WorkspaceID, nullable(c.GroupID), c.Position, now); err != nil {
		return Card{}, fmt.Errorf("insert membership: %w", err)
	}
	return c, nil
}

func (PostgresRepository) UpdateCard(ctx context.Context, sc scope, id string, caption, text *string, now time.Time) error {
	clauses := []string{"updated_at = $2", "revision = revision + 1"}
	args := []any{now}
	n := 3
	if caption != nil {
		clauses = append(clauses, fmt.Sprintf("caption = $%d", n))
		args = append(args, *caption)
		n++
	}
	if text != nil {
		clauses = append(clauses, fmt.Sprintf("text_body = $%d", n))
		args = append(args, *text)
		n++
	}
	args = append(args, id)
	updated, err := sc.Update(ctx, "creative_cards", strings.Join(clauses, ", "), fmt.Sprintf("id = $%d AND archived_at IS NULL", n), args...)
	if err != nil {
		return fmt.Errorf("update card: %w", err)
	}
	if updated != 1 {
		return ErrNotFound
	}
	return nil
}

// ArchiveCard 归档卡片并移除成员关系：卡片行保留（账号持有），拍摄项来源引用随之变 unavailable。
func (PostgresRepository) ArchiveCard(ctx context.Context, sc scope, id string, now time.Time) error {
	updated, err := sc.Update(ctx, "creative_cards", "archived_at = $2, updated_at = $2, revision = revision + 1", "id = $3 AND archived_at IS NULL", now, id)
	if err != nil {
		return fmt.Errorf("archive card: %w", err)
	}
	if updated != 1 {
		return ErrNotFound
	}
	if _, err := sc.Delete(ctx, "creative_card_memberships", "card_id = $2", id); err != nil {
		return fmt.Errorf("delete membership: %w", err)
	}
	return nil
}

func (PostgresRepository) SetMembership(ctx context.Context, sc scope, cardID, workspaceID string, groupID *string, position int, now time.Time) error {
	n, err := sc.Update(ctx, "creative_card_memberships", "workspace_id = $2, group_id = $3, position = $4, updated_at = $5", "card_id = $6", workspaceID, nullable(groupID), position, now, cardID)
	if err != nil {
		return fmt.Errorf("set membership: %w", err)
	}
	if n != 1 {
		return ErrCardNotInWorkspace
	}
	return nil
}

func (PostgresRepository) SetPosition(ctx context.Context, sc scope, table, idCol, workspaceID, id string, position int, now time.Time) error {
	switch table {
	case "creative_card_memberships", "creative_shoot_items", "creative_shoot_memos":
	default:
		return fmt.Errorf("set position: bad table %q", table)
	}
	switch idCol {
	case "card_id", "id":
	default:
		return fmt.Errorf("set position: bad col %q", idCol)
	}
	_, err := sc.Update(ctx, table, "position = $2, updated_at = $3", idCol+" = $4 AND workspace_id = $5", position, now, id, workspaceID)
	if err != nil {
		return fmt.Errorf("set position %s: %w", table, err)
	}
	return nil
}

func (PostgresRepository) SetCardsGroup(ctx context.Context, sc scope, workspaceID string, cardIDs []string, groupID *string, now time.Time) (int64, error) {
	n, err := sc.Update(ctx, "creative_card_memberships", "group_id = $2, updated_at = $3", "workspace_id = $4 AND card_id = ANY($5)", nullable(groupID), now, workspaceID, cardIDs)
	if err != nil {
		return 0, fmt.Errorf("set cards group: %w", err)
	}
	return n, nil
}

func (PostgresRepository) ListGroups(ctx context.Context, sc scope, workspaceID string) ([]Group, error) {
	rows, err := sc.QueryPage(ctx, "creative_card_groups", groupColumns, "workspace_id = $2", []store.OrderBy{{Column: "position"}, {Column: "id"}}, 500, 0, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("list groups: %w", err)
	}
	defer rows.Close()
	out := make([]Group, 0, 8)
	for rows.Next() {
		var g Group
		if err := rows.Scan(&g.ID, &g.WorkspaceID, &g.Name, &g.Position, &g.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

func (PostgresRepository) InsertGroup(ctx context.Context, sc scope, workspaceID, name string, position int, now time.Time) (Group, error) {
	id := newID("cgp")
	if err := sc.Insert(ctx, "creative_card_groups", []string{"id", "workspace_id", "name", "position", "created_at"}, id, workspaceID, name, position, now); err != nil {
		return Group{}, fmt.Errorf("insert group: %w", err)
	}
	return Group{ID: id, WorkspaceID: workspaceID, Name: name, Position: position, CreatedAt: now}, nil
}

func (PostgresRepository) RenameGroup(ctx context.Context, sc scope, workspaceID, id, name string) error {
	n, err := sc.Update(ctx, "creative_card_groups", "name = $2", "id = $3 AND workspace_id = $4", name, id, workspaceID)
	if err != nil {
		return fmt.Errorf("rename group: %w", err)
	}
	if n != 1 {
		return ErrNotFound
	}
	return nil
}

func (PostgresRepository) DeleteGroup(ctx context.Context, sc scope, workspaceID, id string) error {
	n, err := sc.Delete(ctx, "creative_card_groups", "id = $2 AND workspace_id = $3", id, workspaceID)
	if err != nil {
		return fmt.Errorf("delete group: %w", err)
	}
	if n != 1 {
		return ErrNotFound
	}
	return nil
}

func (PostgresRepository) GroupExists(ctx context.Context, sc scope, workspaceID, id string) (bool, error) {
	return sc.Exists(ctx, "creative_card_groups", "id = $2 AND workspace_id = $3", id, workspaceID)
}

// ---- shoot items ----

func scanShoot(row interface{ Scan(...any) error }) (ShootItem, error) {
	var s ShootItem
	var ref, srcID *string
	var srcRev *int64
	var ids []byte
	var result *string
	if err := row.Scan(&s.ID, &s.WorkspaceID, &s.Title, &s.Description, &ref, &srcID, &srcRev, &ids, &s.Position, &s.Status, &result, &s.ResultNote, &s.ResultAt, &s.Revision, &s.CreatedAt, &s.UpdatedAt); err != nil {
		return s, err
	}
	s.RefAssetID, s.SourceCardID, s.SourceCardRev = ref, srcID, srcRev
	s.SourceCardIDs = []string{}
	if len(ids) > 0 {
		if err := json.Unmarshal(ids, &s.SourceCardIDs); err != nil {
			return s, fmt.Errorf("decode source_card_ids: %w", err)
		}
	}
	if result != nil {
		r := ShootResult(*result)
		s.Result = &r
	}
	return s, nil
}

func (r PostgresRepository) ListShootItems(ctx context.Context, sc scope, workspaceID string, includeTombstone bool) ([]ShootItem, error) {
	cond := "workspace_id = $2 AND status = 'active'"
	if includeTombstone {
		cond = "workspace_id = $2"
	}
	rows, err := sc.QueryPage(ctx, "creative_shoot_items", shootColumns, cond, []store.OrderBy{{Column: "position"}, {Column: "id"}}, 1000, 0, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("list shoot items: %w", err)
	}
	out := make([]ShootItem, 0, 16)
	for rows.Next() {
		s, err := scanShoot(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, s)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, r.fillShootRefs(ctx, sc, out)
}

// fillShootRefs 计算来源两态与参考图 checksum。
func (PostgresRepository) fillShootRefs(ctx context.Context, sc scope, items []ShootItem) error {
	srcIDs := make([]string, 0, len(items))
	assetIDs := make([]string, 0, len(items))
	for i := range items {
		items[i].SourceState = SourceUnavailable
		if items[i].SourceCardID != nil {
			srcIDs = append(srcIDs, *items[i].SourceCardID)
		}
		if items[i].RefAssetID != nil {
			assetIDs = append(assetIDs, *items[i].RefAssetID)
		}
	}
	if len(srcIDs) > 0 {
		rows, err := sc.Query(ctx, "creative_cards", "id", "id = ANY($2) AND archived_at IS NULL", srcIDs)
		if err != nil {
			return fmt.Errorf("shoot refs cards: %w", err)
		}
		alive := make(map[string]bool, len(srcIDs))
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			alive[id] = true
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		memberships, err := sc.Query(ctx, "creative_card_memberships", "card_id, workspace_id", "card_id = ANY($2)", srcIDs)
		if err != nil {
			return err
		}
		workspaces := make(map[string]string, len(srcIDs))
		for memberships.Next() {
			var cardID, workspaceID string
			if err := memberships.Scan(&cardID, &workspaceID); err != nil {
				memberships.Close()
				return err
			}
			workspaces[cardID] = workspaceID
		}
		memberships.Close()
		if err := memberships.Err(); err != nil {
			return err
		}
		// 来源仍须在某个空间内才算 available（移出空间 = 归档卡片，已由 alive 覆盖）
		for i := range items {
			if items[i].SourceCardID != nil && alive[*items[i].SourceCardID] {
				items[i].SourceState = SourceAvailable
				if workspaceID, ok := workspaces[*items[i].SourceCardID]; ok {
					items[i].SourceWorkspaceID = &workspaceID
				} else {
					items[i].SourceState = SourceUnavailable
				}
			}
		}
	}
	if len(assetIDs) > 0 {
		rows, err := sc.Query(ctx, "creative_workspace_assets", "id, display_checksum", "id = ANY($2)", assetIDs)
		if err != nil {
			return fmt.Errorf("shoot refs assets: %w", err)
		}
		sums := make(map[string]string, len(assetIDs))
		for rows.Next() {
			var id, sum string
			if err := rows.Scan(&id, &sum); err != nil {
				rows.Close()
				return err
			}
			sums[id] = sum
		}
		rows.Close()
		for i := range items {
			if items[i].RefAssetID != nil {
				if s, ok := sums[*items[i].RefAssetID]; ok {
					v := s
					items[i].RefChecksum = &v
				}
			}
		}
	}
	return nil
}

func (PostgresRepository) InsertShootItem(ctx context.Context, sc scope, s ShootItem, now time.Time) (ShootItem, error) {
	s.ID = newID("csi")
	s.Revision = 1
	s.Status = ShootActive
	s.CreatedAt, s.UpdatedAt = now, now
	if s.SourceCardIDs == nil {
		s.SourceCardIDs = []string{}
	}
	idsJSON, err := json.Marshal(s.SourceCardIDs)
	if err != nil {
		return ShootItem{}, err
	}
	if err := sc.Insert(ctx, "creative_shoot_items", []string{"id", "workspace_id", "title", "description", "ref_asset_id", "source_card_id", "source_card_rev", "source_card_ids", "position", "status", "revision", "created_at", "updated_at"},
		s.ID, s.WorkspaceID, s.Title, s.Description, nullable(s.RefAssetID), nullable(s.SourceCardID), s.SourceCardRev, idsJSON, s.Position, string(ShootActive), 1, now, now); err != nil {
		return ShootItem{}, fmt.Errorf("insert shoot item: %w", err)
	}
	return s, nil
}

func (PostgresRepository) LockShootItem(ctx context.Context, sc scope, id string) (ShootItem, error) {
	s, err := scanShoot(sc.QueryRowForUpdate(ctx, "creative_shoot_items", shootColumns, "id = $2", id))
	if errors.Is(err, store.ErrNoRows) {
		return ShootItem{}, ErrNotFound
	}
	if err != nil {
		return ShootItem{}, fmt.Errorf("lock shoot item: %w", err)
	}
	return s, nil
}

func (PostgresRepository) UpdateShootItem(ctx context.Context, sc scope, id string, expectedRevision int64, set map[string]any, now time.Time) error {
	clauses := make([]string, 0, 8)
	args := make([]any, 0, 10)
	n := 2
	for _, col := range []string{"title", "description", "status", "tombstoned_at", "result", "result_note", "result_at"} {
		v, ok := set[col]
		if !ok {
			continue
		}
		clauses = append(clauses, fmt.Sprintf("%s = $%d", col, n))
		args = append(args, v)
		n++
	}
	clauses = append(clauses, fmt.Sprintf("updated_at = $%d", n), "revision = revision + 1")
	args = append(args, now)
	n++
	cond := fmt.Sprintf("id = $%d", n)
	args = append(args, id)
	n++
	if expectedRevision > 0 {
		cond += fmt.Sprintf(" AND revision = $%d", n)
		args = append(args, expectedRevision)
	}
	updated, err := sc.Update(ctx, "creative_shoot_items", strings.Join(clauses, ", "), cond, args...)
	if err != nil {
		return fmt.Errorf("update shoot item: %w", err)
	}
	if updated != 1 {
		if expectedRevision > 0 {
			return ErrRevisionConflict
		}
		return ErrNotFound
	}
	return nil
}

func (PostgresRepository) MaxShootPosition(ctx context.Context, sc scope, workspaceID string) (int, error) {
	var max *int
	if err := sc.ScalarAggregate(ctx, "creative_shoot_items", store.AggregateMax, "position", "workspace_id = $2", workspaceID).Scan(&max); err != nil {
		return 0, fmt.Errorf("max shoot position: %w", err)
	}
	if max == nil {
		return -1, nil
	}
	return *max, nil
}

// ---- memos ----

func (PostgresRepository) ListMemos(ctx context.Context, sc scope, workspaceID string) ([]Memo, error) {
	rows, err := sc.QueryPage(ctx, "creative_shoot_memos", memoColumns, "workspace_id = $2", []store.OrderBy{{Column: "position"}, {Column: "id"}}, 500, 0, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("list memos: %w", err)
	}
	defer rows.Close()
	out := make([]Memo, 0, 8)
	for rows.Next() {
		var m Memo
		if err := rows.Scan(&m.ID, &m.WorkspaceID, &m.Text, &m.Checked, &m.Position, &m.CreatedAt, &m.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (PostgresRepository) InsertMemo(ctx context.Context, sc scope, workspaceID, text string, position int, now time.Time) (Memo, error) {
	id := newID("cmm")
	if err := sc.Insert(ctx, "creative_shoot_memos", []string{"id", "workspace_id", "text_body", "checked", "position", "created_at", "updated_at"}, id, workspaceID, text, false, position, now, now); err != nil {
		return Memo{}, fmt.Errorf("insert memo: %w", err)
	}
	return Memo{ID: id, WorkspaceID: workspaceID, Text: text, Position: position, CreatedAt: now, UpdatedAt: now}, nil
}

func (PostgresRepository) UpdateMemo(ctx context.Context, sc scope, workspaceID, id string, text *string, checked *bool, now time.Time) error {
	clauses := []string{"updated_at = $2"}
	args := []any{now}
	n := 3
	if text != nil {
		clauses = append(clauses, fmt.Sprintf("text_body = $%d", n))
		args = append(args, *text)
		n++
	}
	if checked != nil {
		clauses = append(clauses, fmt.Sprintf("checked = $%d", n))
		args = append(args, *checked)
		n++
	}
	args = append(args, id, workspaceID)
	updated, err := sc.Update(ctx, "creative_shoot_memos", strings.Join(clauses, ", "), fmt.Sprintf("id = $%d AND workspace_id = $%d", n, n+1), args...)
	if err != nil {
		return fmt.Errorf("update memo: %w", err)
	}
	if updated != 1 {
		return ErrNotFound
	}
	return nil
}

func (PostgresRepository) DeleteMemo(ctx context.Context, sc scope, workspaceID, id string) error {
	n, err := sc.Delete(ctx, "creative_shoot_memos", "id = $2 AND workspace_id = $3", id, workspaceID)
	if err != nil {
		return fmt.Errorf("delete memo: %w", err)
	}
	if n != 1 {
		return ErrNotFound
	}
	return nil
}

func (PostgresRepository) MaxMemoPosition(ctx context.Context, sc scope, workspaceID string) (int, error) {
	var max *int
	if err := sc.ScalarAggregate(ctx, "creative_shoot_memos", store.AggregateMax, "position", "workspace_id = $2", workspaceID).Scan(&max); err != nil {
		return 0, fmt.Errorf("max memo position: %w", err)
	}
	if max == nil {
		return -1, nil
	}
	return *max, nil
}

// ---- assets ----

func (PostgresRepository) InsertAsset(ctx context.Context, sc scope, a Asset) error {
	if err := sc.Insert(ctx, "creative_workspace_assets", []string{"id", "workspace_id", "source_class", "rights_basis", "generation_reference_granted",
		"original_media_type", "original_size", "original_width", "original_height", "original_checksum",
		"display_media_type", "display_size", "display_width", "display_height", "display_checksum", "created_at"},
		a.ID, a.WorkspaceID, string(a.SourceClass), a.RightsBasis, a.GenerationReferenceGranted,
		a.OriginalMediaType, a.OriginalSize, a.OriginalWidth, a.OriginalHeight, a.OriginalChecksum,
		a.DisplayMediaType, a.DisplaySize, a.DisplayWidth, a.DisplayHeight, a.DisplayChecksum, a.CreatedAt); err != nil {
		return fmt.Errorf("insert asset: %w", err)
	}
	return nil
}

func (PostgresRepository) GetAsset(ctx context.Context, sc scope, id string) (Asset, error) {
	var a Asset
	err := sc.QueryRow(ctx, "creative_workspace_assets", assetColumns, "id = $2", id).Scan(
		&a.ID, &a.WorkspaceID, &a.SourceClass, &a.RightsBasis, &a.GenerationReferenceGranted,
		&a.OriginalMediaType, &a.OriginalSize, &a.OriginalWidth, &a.OriginalHeight, &a.OriginalChecksum,
		&a.DisplayMediaType, &a.DisplaySize, &a.DisplayWidth, &a.DisplayHeight, &a.DisplayChecksum, &a.CreatedAt)
	if errors.Is(err, store.ErrNoRows) {
		return Asset{}, ErrNotFound
	}
	if err != nil {
		return Asset{}, fmt.Errorf("get asset: %w", err)
	}
	return a, nil
}

func (PostgresRepository) AssetsInWorkspace(ctx context.Context, sc scope, workspaceID string, ids []string) (map[string]Asset, error) {
	rows, err := sc.Query(ctx, "creative_workspace_assets", assetColumns, "workspace_id = $2 AND id = ANY($3)", workspaceID, ids)
	if err != nil {
		return nil, fmt.Errorf("assets in workspace: %w", err)
	}
	defer rows.Close()
	out := make(map[string]Asset, len(ids))
	for rows.Next() {
		var a Asset
		if err := rows.Scan(&a.ID, &a.WorkspaceID, &a.SourceClass, &a.RightsBasis, &a.GenerationReferenceGranted,
			&a.OriginalMediaType, &a.OriginalSize, &a.OriginalWidth, &a.OriginalHeight, &a.OriginalChecksum,
			&a.DisplayMediaType, &a.DisplaySize, &a.DisplayWidth, &a.DisplayHeight, &a.DisplayChecksum, &a.CreatedAt); err != nil {
			return nil, err
		}
		out[a.ID] = a
	}
	return out, rows.Err()
}

// ---- pilot ----

func (PostgresRepository) GetPilot(ctx context.Context, sc scope) (PilotAccount, error) {
	var p PilotAccount
	err := sc.QueryRow(ctx, "creative_pilot_accounts", "state, enrolled_at, stopped_at, window_id, revision", "").Scan(&p.State, &p.EnrolledAt, &p.StoppedAt, &p.WindowID, &p.Revision)
	if errors.Is(err, store.ErrNoRows) {
		return PilotAccount{State: PilotLegacyWrite, Revision: 0}, nil
	}
	if err != nil {
		return PilotAccount{}, fmt.Errorf("get pilot: %w", err)
	}
	return p, nil
}

func (PostgresRepository) UpsertPilot(ctx context.Context, sc scope, p PilotAccount, now time.Time) error {
	// creative_pilot_accounts 以 account_id 为主键，由 scope 注入。
	if err := sc.Upsert(ctx, "creative_pilot_accounts",
		[]string{"state", "enrolled_at", "stopped_at", "window_id", "revision", "updated_at"},
		[]string{"account_id"},
		[]string{"state", "enrolled_at", "stopped_at", "window_id", "revision", "updated_at"},
		string(p.State), p.EnrolledAt, p.StoppedAt, p.WindowID, p.Revision, now); err != nil {
		return fmt.Errorf("upsert pilot: %w", err)
	}
	return nil
}

// ---- helpers ----

func nullable(v *string) any {
	if v == nil || *v == "" {
		return nil
	}
	return *v
}
