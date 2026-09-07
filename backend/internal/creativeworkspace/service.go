package creativeworkspace

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/samson/customer-manage-platform/backend/internal/planningmedia"
	"github.com/samson/customer-manage-platform/backend/internal/platform/immutablefs"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

const (
	maxNameLen        = 80
	maxTextLen        = 2000
	maxURLLen         = 2048
	maxCaptionLen     = 200
	maxGroupNameLen   = 40
	maxMemoLen        = 200
	maxTitleLen       = 200
	maxImportLines    = 200
	maxImportAssets   = 50
	maxShootItemsOnce = 50
)

// Service 是创作空间用例层；所有写在 scope.WithTxScope 内完成。
type Service struct {
	repo    PostgresRepository
	objects immutablefs.ObjectStore
	now     func() time.Time
	pilot   PilotGate
}

// PilotGate 判断账号是否处于 pilot 写态；由 pilot.go 的 Preflight 实现，也可在测试中替换。
type PilotGate interface {
	CanWriteNew(ctx context.Context, sc store.AccountScope) error
}

func NewService(repo PostgresRepository, objects immutablefs.ObjectStore, gate PilotGate) *Service {
	return &Service{repo: repo, objects: objects, now: time.Now, pilot: gate}
}

func (s *Service) WithClock(now func() time.Time) *Service { s.now = now; return s }

func (s *Service) requirePilot(ctx context.Context, sc store.AccountScope) error {
	if s.pilot == nil {
		return ErrPilotRequired
	}
	return s.pilot.CanWriteNew(ctx, sc)
}

// ---- workspaces ----

// EnsureInbox 每账号唯一的「未归类」空间，首次访问时创建。
func (s *Service) EnsureInbox(ctx context.Context, sc store.AccountScope) (Workspace, error) {
	ws, err := s.repo.FindInbox(ctx, sc)
	if err == nil {
		return ws, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return Workspace{}, err
	}
	err = sc.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		if err := s.requirePilotInScope(ctx, tx); err != nil {
			return err
		}
		existing, ferr := s.repo.FindInbox(ctx, tx)
		if ferr == nil {
			ws = existing
			return nil
		}
		if !errors.Is(ferr, ErrNotFound) {
			return ferr
		}
		created, cerr := s.repo.InsertWorkspace(ctx, tx, KindInbox, "", s.now().UTC())
		if cerr != nil {
			return cerr
		}
		ws = created
		return nil
	})
	return ws, err
}

func (s *Service) ListWorkspaces(ctx context.Context, sc store.AccountScope, includeArchived bool) ([]Workspace, error) {
	st, err := s.repo.GetPilot(ctx, sc)
	if err != nil {
		return nil, err
	}
	if st.State == PilotLegacyWrite {
		return nil, ErrPilotRequired
	}
	if st.State == PilotNewWrite {
		if _, err := s.EnsureInbox(ctx, sc); err != nil {
			return nil, err
		}
	}
	ws, err := s.repo.ListWorkspaces(ctx, sc, includeArchived)
	if err != nil {
		return nil, err
	}
	if err := s.repo.WorkspaceSummaries(ctx, sc, ws); err != nil {
		return nil, err
	}
	if err := s.repo.ResolveLinks(ctx, sc, ws); err != nil {
		return nil, err
	}
	// 未归类置顶
	for i := range ws {
		if ws[i].Kind == KindInbox && i != 0 {
			inbox := ws[i]
			copy(ws[1:i+1], ws[0:i])
			ws[0] = inbox
			break
		}
	}
	return ws, nil
}

func (s *Service) CreateWorkspaceInScope(ctx context.Context, tx store.TxAccountScope, input CreateWorkspaceInput) (Workspace, error) {
	if err := s.requirePilotInScope(ctx, tx); err != nil {
		return Workspace{}, err
	}
	name := strings.TrimSpace(input.Name)
	if len([]rune(name)) > maxNameLen {
		return Workspace{}, ValidationError{Message: "空间名称不能超过 80 字"}
	}
	return s.repo.InsertWorkspace(ctx, tx, KindProject, name, s.now().UTC())
}

func (s *Service) CreateWorkspace(ctx context.Context, sc store.AccountScope, input CreateWorkspaceInput) (Workspace, error) {
	if err := s.requirePilot(ctx, sc); err != nil {
		return Workspace{}, err
	}
	var ws Workspace
	err := sc.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		if err := s.requirePilotInScope(ctx, tx); err != nil {
			return err
		}
		created, err := s.CreateWorkspaceInScope(ctx, tx, input)
		ws = created
		return err
	})
	return ws, err
}

func (s *Service) GetWorkspaceDetail(ctx context.Context, sc store.AccountScope, id string, touch bool) (WorkspaceDetail, error) {
	var d WorkspaceDetail
	ws, err := s.repo.GetWorkspace(ctx, sc, id)
	if err != nil {
		return d, err
	}
	if touch && !ws.Archived {
		state, stateErr := s.repo.GetPilot(ctx, sc)
		if stateErr != nil {
			return d, stateErr
		}
		if state.State == PilotNewWrite {
			err := s.withMutableWorkspace(ctx, sc, id, func(tx store.TxAccountScope) error {
				return s.repo.UpdateWorkspace(ctx, tx, id, 0, map[string]any{"last_opened_at": s.now().UTC()}, s.now().UTC())
			})
			if err != nil {
				return d, err
			}
			ws.LastOpenedAt = s.now().UTC()
		}
	}

	list := []Workspace{ws}
	if err := s.repo.ResolveLinks(ctx, sc, list); err != nil {
		return d, err
	}
	d.Workspace = list[0]
	if d.Cards, err = s.repo.ListWorkspaceCards(ctx, sc, id); err != nil {
		return d, err
	}
	if d.Groups, err = s.repo.ListGroups(ctx, sc, id); err != nil {
		return d, err
	}
	if d.ShootItems, err = s.repo.ListShootItems(ctx, sc, id, false); err != nil {
		return d, err
	}
	if d.Memos, err = s.repo.ListMemos(ctx, sc, id); err != nil {
		return d, err
	}
	d.Workspace.CardCount = len(d.Cards)
	d.Workspace.ShootItemCount = len(d.ShootItems)
	return d, nil
}

func (s *Service) UpdateWorkspace(ctx context.Context, sc store.AccountScope, id string, input UpdateWorkspaceInput) (Workspace, error) {
	if err := s.requirePilot(ctx, sc); err != nil {
		return Workspace{}, err
	}
	var out Workspace
	err := sc.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		if err := s.requirePilotInScope(ctx, tx); err != nil {
			return err
		}
		ws, err := s.repo.LockWorkspace(ctx, tx, id)
		if err != nil {
			return err
		}
		if ws.Archived && (input.Archived == nil || *input.Archived || input.Name != nil || input.Link != nil || input.Touch) {
			return ErrWorkspaceArchived
		}

		set := map[string]any{}
		now := s.now().UTC()
		if ws.Kind == KindInbox && (input.Name != nil || input.Link != nil || input.Archived != nil) {
			return ErrInboxImmutable
		}
		if input.Name != nil {
			name := strings.TrimSpace(*input.Name)
			if len([]rune(name)) > maxNameLen {
				return ValidationError{Message: "空间名称不能超过 80 字"}
			}
			set["name"] = name
		}
		if input.Link != nil {
			if input.Link.Kind == "" {
				set["link_order_id"], set["link_customer_id"] = nil, nil
			} else {
				if input.Link.Kind != LinkOrder && input.Link.Kind != LinkCustomer {
					return ValidationError{Message: "关联类型只能是订单或客户"}
				}
				ok, err := s.repo.LinkTargetExists(ctx, tx, *input.Link)
				if err != nil {
					return err
				}
				if !ok {
					return ErrLinkTargetNotFound
				}
				if input.Link.Kind == LinkOrder {
					set["link_order_id"], set["link_customer_id"] = input.Link.ID, nil
				} else {
					set["link_order_id"], set["link_customer_id"] = nil, input.Link.ID
				}
			}
		}
		if input.Archived != nil {
			if *input.Archived {
				set["archived_at"] = now
			} else {
				set["archived_at"] = nil
			}
		}
		if input.Touch {
			set["last_opened_at"] = now
		}
		if len(set) == 0 {
			out = ws
			return nil
		}
		if err := s.repo.UpdateWorkspace(ctx, tx, id, input.ExpectedRevision, set, now); err != nil {
			return err
		}
		out, err = s.repo.GetWorkspace(ctx, tx, id)
		if err != nil {
			return err
		}
		list := []Workspace{out}
		if err := s.repo.ResolveLinks(ctx, tx, list); err != nil {
			return err
		}
		out = list[0]
		return nil
	})
	return out, err
}

// WorkspacesLinkedTo 供订单 / 客户详情反向显示。
func (s *Service) WorkspacesLinkedTo(ctx context.Context, sc store.AccountScope, link LinkInput) ([]Workspace, error) {
	return s.repo.WorkspacesLinkedTo(ctx, sc, link)
}

// ---- cards ----

var urlLine = regexp.MustCompile(`^(https?://|www\.)\S+$`)

func looksLikeURL(line string) bool { return urlLine.MatchString(line) }

func normalizeURL(raw string) (string, error) {
	u := raw
	if strings.HasPrefix(u, "www.") {
		u = "https://" + u
	}
	parsed, err := url.Parse(u)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", ValidationError{Message: "链接格式不正确"}
	}
	if len(u) > maxURLLen {
		return "", ValidationError{Message: "链接过长"}
	}
	return u, nil
}

// SplitImportLines 是批量粘贴的拆行规则：按行、去首尾空白、丢空行；上限 200 行。
func SplitImportLines(raw string) []string {
	raw = strings.ReplaceAll(raw, "\r", "")
	lines := make([]string, 0, 16)
	for _, l := range strings.Split(raw, "\n") {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		if len([]rune(l)) > maxTextLen {
			l = string([]rune(l)[:maxTextLen])
		}
		lines = append(lines, l)
		if len(lines) >= maxImportLines {
			break
		}
	}
	return lines
}

// ImportCardsInScope 一次批量搬入：文本按行成卡（链接行成链接卡），已上传的图片资产按张成卡；共用一个批次。
func (s *Service) ImportCardsInScope(ctx context.Context, tx store.TxAccountScope, input ImportCardsInput) (ImportCardsResult, error) {
	if err := s.requirePilotInScope(ctx, tx); err != nil {
		return ImportCardsResult{}, err
	}
	ws, err := s.lockMutableWorkspace(ctx, tx, input.WorkspaceID)
	if err != nil {
		return ImportCardsResult{}, err
	}
	if ws.Archived {
		return ImportCardsResult{}, ErrWorkspaceArchived
	}
	lines := SplitImportLines(input.RawText)
	if len(lines) == 0 && len(input.AssetIDs) == 0 {
		return ImportCardsResult{}, ValidationError{Message: "没有可搬入的内容"}
	}
	if len(input.AssetIDs) > maxImportAssets {
		return ImportCardsResult{}, ValidationError{Message: "一次最多搬入 50 张图片"}
	}
	if input.GroupID != nil {
		ok, err := s.repo.GroupExists(ctx, tx, ws.ID, *input.GroupID)
		if err != nil {
			return ImportCardsResult{}, err
		}
		if !ok {
			return ImportCardsResult{}, ValidationError{Message: "分组不存在"}
		}
	}
	var assets map[string]Asset
	if len(input.AssetIDs) > 0 {
		assets, err = s.repo.AssetsInWorkspace(ctx, tx, ws.ID, input.AssetIDs)
		if err != nil {
			return ImportCardsResult{}, err
		}
		for _, id := range input.AssetIDs {
			if _, ok := assets[id]; !ok {
				return ImportCardsResult{}, ValidationError{Message: "图片不属于本空间"}
			}
		}
	}
	now := s.now().UTC()
	sourceKind := "single"
	var raw *string
	switch {
	case len(lines) > 0:
		sourceKind = "paste_text"
		r := input.RawText
		raw = &r
	case len(input.AssetIDs) > 0:
		sourceKind = "drop_images"
	}

	total := len(lines) + len(input.AssetIDs)
	batch, err := s.repo.InsertBatch(ctx, tx, ws.ID, sourceKind, raw, total, now)
	if err != nil {
		return ImportCardsResult{}, err
	}
	pos, err := s.repo.MaxCardPosition(ctx, tx, ws.ID)
	if err != nil {
		return ImportCardsResult{}, err
	}
	cards := make([]Card, 0, total)
	seq := 0
	for _, id := range input.AssetIDs {
		seq++
		pos++
		a := assets[id]
		sc := a.SourceClass
		c := Card{Type: CardImage, AssetID: id, SourceClass: &sc, BatchID: batch.ID, BatchSeq: seq, WorkspaceID: ws.ID, GroupID: input.GroupID, Position: pos}
		created, err := s.repo.InsertCard(ctx, tx, c, now)
		if err != nil {
			return ImportCardsResult{}, err
		}
		created.Checksum, created.Width, created.Height = a.DisplayChecksum, a.DisplayWidth, a.DisplayHeight
		cards = append(cards, created)
	}
	for _, line := range lines {
		seq++
		pos++
		c := Card{BatchID: batch.ID, BatchSeq: seq, WorkspaceID: ws.ID, GroupID: input.GroupID, Position: pos}
		if looksLikeURL(line) {
			u, err := normalizeURL(line)
			if err != nil {
				c.Type, c.Text = CardText, line
			} else {
				c.Type, c.URL = CardLink, u
			}
		} else {
			c.Type, c.Text = CardText, line
		}
		created, err := s.repo.InsertCard(ctx, tx, c, now)
		if err != nil {
			return ImportCardsResult{}, err
		}
		cards = append(cards, created)
	}
	if err := s.repo.UpdateWorkspace(ctx, tx, ws.ID, 0, map[string]any{"last_opened_at": now}, now); err != nil {
		return ImportCardsResult{}, err
	}
	return ImportCardsResult{Batch: batch, Cards: cards}, nil
}

func (s *Service) UpdateCard(ctx context.Context, sc store.AccountScope, workspaceID, cardID string, input UpdateCardInput) (Card, error) {
	if err := s.requirePilot(ctx, sc); err != nil {
		return Card{}, err
	}
	var out Card
	err := sc.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		if err := s.requirePilotInScope(ctx, tx); err != nil {
			return err
		}
		if _, err := s.lockMutableWorkspace(ctx, tx, workspaceID); err != nil {
			return err
		}
		c, err := s.repo.GetCard(ctx, tx, cardID)
		if err != nil {
			return err
		}
		if c.WorkspaceID != workspaceID {
			return ErrCardNotInWorkspace
		}
		if input.Caption != nil {
			if c.Type != CardImage {
				return ValidationError{Message: "只有图片可以写说明"}
			}
			if len([]rune(*input.Caption)) > maxCaptionLen {
				return ValidationError{Message: "说明不能超过 200 字"}
			}
		}
		if input.Text != nil {
			if c.Type != CardText {
				return ValidationError{Message: "只有文字卡可以改正文"}
			}
			t := strings.TrimSpace(*input.Text)
			if t == "" || len([]rune(t)) > maxTextLen {
				return ValidationError{Message: "正文需在 1–2000 字之间"}
			}
			input.Text = &t
		}
		if err := s.repo.UpdateCard(ctx, tx, cardID, input.Caption, input.Text, s.now().UTC()); err != nil {
			return err
		}
		out, err = s.repo.GetCard(ctx, tx, cardID)
		return err
	})
	return out, err
}

// RemoveCard 把卡片移出空间：归档卡片行（账号仍持有），来源引用随之 unavailable。
func (s *Service) RemoveCard(ctx context.Context, sc store.AccountScope, workspaceID, cardID string) error {
	if err := s.requirePilot(ctx, sc); err != nil {
		return err
	}
	return sc.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		if err := s.requirePilotInScope(ctx, tx); err != nil {
			return err
		}
		if _, err := s.lockMutableWorkspace(ctx, tx, workspaceID); err != nil {
			return err
		}
		c, err := s.repo.GetCard(ctx, tx, cardID)
		if err != nil {
			return err
		}
		if c.WorkspaceID != workspaceID {
			return ErrCardNotInWorkspace
		}
		return s.repo.ArchiveCard(ctx, tx, cardID, s.now().UTC())
	})
}

// MoveCard 把卡片挪到另一个空间（例如未归类 → 项目空间）；只改一条成员记录（DEC-3）。
func (s *Service) MoveCard(ctx context.Context, sc store.AccountScope, input MoveCardInput) (Card, error) {
	if err := s.requirePilot(ctx, sc); err != nil {
		return Card{}, err
	}
	var out Card
	err := sc.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		if err := s.requirePilotInScope(ctx, tx); err != nil {
			return err
		}
		card, err := s.repo.GetCard(ctx, tx, input.CardID)
		if err != nil {
			return err
		}
		if card.WorkspaceID != input.FromWorkspaceID {
			return ErrCardNotInWorkspace
		}

		if _, err := s.lockMutableWorkspace(ctx, tx, card.WorkspaceID); err != nil {
			return err
		}
		target, err := s.lockMutableWorkspace(ctx, tx, input.ToWorkspaceID)
		if err != nil {
			return err
		}
		if target.Archived {
			return ErrWorkspaceArchived
		}
		if input.ToGroupID != nil {
			ok, err := s.repo.GroupExists(ctx, tx, target.ID, *input.ToGroupID)
			if err != nil {
				return err
			}
			if !ok {
				return ValidationError{Message: "分组不存在"}
			}
		}
		pos, err := s.repo.MaxCardPosition(ctx, tx, target.ID)
		if err != nil {
			return err
		}
		if err := s.repo.SetMembership(ctx, tx, input.CardID, target.ID, input.ToGroupID, pos+1, s.now().UTC()); err != nil {
			return err
		}
		out, err = s.repo.GetCard(ctx, tx, input.CardID)
		return err
	})
	return out, err
}

func (s *Service) ReorderCards(ctx context.Context, sc store.AccountScope, input ReorderInput) error {
	if err := s.requirePilot(ctx, sc); err != nil {
		return err
	}
	return sc.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		if err := s.requirePilotInScope(ctx, tx); err != nil {
			return err
		}
		if _, err := s.lockMutableWorkspace(ctx, tx, input.WorkspaceID); err != nil {
			return err
		}
		current, err := s.repo.ListWorkspaceCards(ctx, tx, input.WorkspaceID)
		if err != nil {
			return err
		}
		order := stableOrder(current, input.CardIDs, func(c Card) string { return c.ID })
		now := s.now().UTC()
		for i, id := range order {
			if err := s.repo.SetPosition(ctx, tx, "creative_card_memberships", "card_id", input.WorkspaceID, id, i, now); err != nil {
				return err
			}
		}
		return nil
	})
}

// stableOrder 以请求顺序为主、缺失项按原相对顺序追加；忽略不属于本空间的 ID。
func stableOrder[T any](current []T, requested []string, key func(T) string) []string {
	present := make(map[string]bool, len(current))
	for _, c := range current {
		present[key(c)] = true
	}
	out := make([]string, 0, len(current))
	seen := make(map[string]bool, len(current))
	for _, id := range requested {
		if present[id] && !seen[id] {
			out = append(out, id)
			seen[id] = true
		}
	}
	for _, c := range current {
		if !seen[key(c)] {
			out = append(out, key(c))
		}
	}
	return out
}

// ---- groups ----

func (s *Service) CreateGroup(ctx context.Context, sc store.AccountScope, input CreateGroupInput) (Group, error) {
	if err := s.requirePilot(ctx, sc); err != nil {
		return Group{}, err
	}
	name := strings.TrimSpace(input.Name)
	if len([]rune(name)) > maxGroupNameLen {
		return Group{}, ValidationError{Message: "分组名不能超过 40 字"}
	}
	var out Group
	err := sc.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		if err := s.requirePilotInScope(ctx, tx); err != nil {
			return err
		}
		ws, err := s.lockMutableWorkspace(ctx, tx, input.WorkspaceID)
		if err != nil {
			return err
		}
		if ws.Archived {
			return ErrWorkspaceArchived
		}
		groups, err := s.repo.ListGroups(ctx, tx, ws.ID)
		if err != nil {
			return err
		}
		now := s.now().UTC()
		out, err = s.repo.InsertGroup(ctx, tx, ws.ID, name, len(groups), now)
		if err != nil {
			return err
		}
		if len(input.CardIDs) > 0 {
			gid := out.ID
			if _, err := s.repo.SetCardsGroup(ctx, tx, ws.ID, input.CardIDs, &gid, now); err != nil {
				return err
			}
		}
		return nil
	})
	return out, err
}

func (s *Service) RenameGroup(ctx context.Context, sc store.AccountScope, workspaceID, groupID, name string) error {
	if err := s.requirePilot(ctx, sc); err != nil {
		return err
	}
	name = strings.TrimSpace(name)
	if len([]rune(name)) > maxGroupNameLen {
		return ValidationError{Message: "分组名不能超过 40 字"}
	}
	return s.withMutableWorkspace(ctx, sc, workspaceID, func(tx store.TxAccountScope) error { return s.repo.RenameGroup(ctx, tx, workspaceID, groupID, name) })
}

// DeleteGroup 解散分组：成员的 group_id 由外键 ON DELETE SET NULL 置空，卡片不受影响。
func (s *Service) DeleteGroup(ctx context.Context, sc store.AccountScope, workspaceID, groupID string) error {
	if err := s.requirePilot(ctx, sc); err != nil {
		return err
	}
	return s.withMutableWorkspace(ctx, sc, workspaceID, func(tx store.TxAccountScope) error { return s.repo.DeleteGroup(ctx, tx, workspaceID, groupID) })
}

func (s *Service) SetCardGroup(ctx context.Context, sc store.AccountScope, input SetCardGroupInput) error {
	if err := s.requirePilot(ctx, sc); err != nil {
		return err
	}
	if len(input.CardIDs) == 0 {
		return ValidationError{Message: "没有选择卡片"}
	}
	return sc.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		if err := s.requirePilotInScope(ctx, tx); err != nil {
			return err
		}
		if _, err := s.lockMutableWorkspace(ctx, tx, input.WorkspaceID); err != nil {
			return err
		}
		if input.GroupID != nil {
			ok, err := s.repo.GroupExists(ctx, tx, input.WorkspaceID, *input.GroupID)
			if err != nil {
				return err
			}
			if !ok {
				return ValidationError{Message: "分组不存在"}
			}
		}
		_, err := s.repo.SetCardsGroup(ctx, tx, input.WorkspaceID, input.CardIDs, input.GroupID, s.now().UTC())
		return err
	})
}

// ---- shoot items ----

// CreateShootItemsInScope 从卡片显式建立拍摄项：值拷贝可见字段，记录来源 ID + revision（DEC-5）。
func (s *Service) CreateShootItemsInScope(ctx context.Context, tx store.TxAccountScope, input CreateShootItemsInput) ([]ShootItem, error) {
	if err := s.requirePilotInScope(ctx, tx); err != nil {
		return nil, err
	}
	if len(input.CardIDs) == 0 {
		return nil, ValidationError{Message: "没有选择卡片"}
	}
	if len(input.CardIDs) > maxShootItemsOnce {
		return nil, ValidationError{Message: "一次最多选择 50 张卡片"}
	}
	ws, err := s.lockMutableWorkspace(ctx, tx, input.WorkspaceID)
	if err != nil {
		return nil, err
	}
	if ws.Archived {
		return nil, ErrWorkspaceArchived
	}
	all, err := s.repo.ListWorkspaceCards(ctx, tx, ws.ID)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]Card, len(all))
	for _, c := range all {
		byID[c.ID] = c
	}
	picked := make([]Card, 0, len(input.CardIDs))
	for _, id := range input.CardIDs {
		c, ok := byID[id]
		if !ok {
			return nil, ErrCardNotInWorkspace
		}
		picked = append(picked, c)
	}
	pos, err := s.repo.MaxShootPosition(ctx, tx, ws.ID)
	if err != nil {
		return nil, err
	}
	now := s.now().UTC()
	made := make([]ShootItem, 0, len(picked))
	if input.Merge && len(picked) > 1 {
		pos++
		item := mergeCards(ws.ID, picked, pos)
		created, err := s.repo.InsertShootItem(ctx, tx, item, now)
		if err != nil {
			return nil, err
		}
		made = append(made, created)
	} else {
		for _, c := range picked {
			pos++
			item := mergeCards(ws.ID, []Card{c}, pos)
			created, err := s.repo.InsertShootItem(ctx, tx, item, now)
			if err != nil {
				return nil, err
			}
			made = append(made, created)
		}
	}
	if err := s.repo.fillShootRefs(ctx, tx, made); err != nil {
		return nil, err
	}
	return made, nil
}

// mergeCards：首张图片作参考图；首条文字作标题、其余文字拼描述；无文字时链接或「要拍的画面 N」作标题。
func mergeCards(workspaceID string, cards []Card, position int) ShootItem {
	item := ShootItem{WorkspaceID: workspaceID, Position: position, SourceCardIDs: make([]string, 0, len(cards))}
	var img *Card
	texts := make([]string, 0, len(cards))
	links := make([]string, 0, 2)
	for i := range cards {
		c := cards[i]
		item.SourceCardIDs = append(item.SourceCardIDs, c.ID)
		switch c.Type {
		case CardImage:
			if img == nil {
				img = &cards[i]
			}
		case CardText:
			texts = append(texts, c.Text)
		case CardLink:
			links = append(links, c.URL)
		}
	}
	primary := cards[0]
	if img != nil {
		primary = *img
		a := img.AssetID
		item.RefAssetID = &a
	}
	id, rev := primary.ID, primary.Revision
	item.SourceCardID, item.SourceCardRev = &id, &rev
	switch {
	case len(texts) > 0:
		item.Title = truncateRunes(texts[0], maxTitleLen)
		item.Description = strings.Join(texts[1:], "\n")
	case len(links) > 0:
		item.Title = truncateRunes(links[0], maxTitleLen)
		item.Description = strings.Join(links[1:], "\n")
	default:
		item.Title = fmt.Sprintf("要拍的画面 %d", position+1)
	}
	if img != nil && img.Caption != "" && item.Description == "" && len(texts) <= 1 {
		item.Description = img.Caption
	}
	if len([]rune(item.Description)) > maxTextLen {
		item.Description = truncateRunes(item.Description, maxTextLen)
	}
	return item
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

func (s *Service) UpdateShootItem(ctx context.Context, sc store.AccountScope, workspaceID, itemID string, input UpdateShootItemInput) (ShootItem, error) {
	if err := s.requirePilot(ctx, sc); err != nil {
		return ShootItem{}, err
	}
	var out ShootItem
	err := sc.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		if err := s.requirePilotInScope(ctx, tx); err != nil {
			return err
		}
		if _, err := s.lockMutableWorkspace(ctx, tx, workspaceID); err != nil {
			return err
		}
		item, err := s.repo.LockShootItem(ctx, tx, itemID)
		if err != nil {
			return err
		}
		if item.WorkspaceID != workspaceID {
			return ErrNotFound
		}
		set := map[string]any{}
		now := s.now().UTC()
		if input.Title != nil {
			t := strings.TrimSpace(*input.Title)
			if t == "" || len([]rune(t)) > maxTitleLen {
				return ValidationError{Message: "标题需在 1–200 字之间"}
			}
			set["title"] = t
		}
		if input.Description != nil {
			if len([]rune(*input.Description)) > maxTextLen {
				return ValidationError{Message: "描述不能超过 2000 字"}
			}
			set["description"] = *input.Description
		}
		if input.Tombstone {
			if item.Status == ShootTombstone {
				out = item
				return nil
			}
			set["status"], set["tombstoned_at"] = string(ShootTombstone), now
		}
		if input.ClearResult {
			set["result"], set["result_at"], set["result_note"] = nil, nil, ""
		} else if input.Result != nil {
			if *input.Result != ResultDone && *input.Result != ResultSkipped {
				return ValidationError{Message: "结果只能是拍到了或跳过"}
			}
			set["result"], set["result_at"] = string(*input.Result), now
		}
		if input.ResultNote != nil {
			if len([]rune(*input.ResultNote)) > 500 {
				return ValidationError{Message: "补记不能超过 500 字"}
			}
			set["result_note"] = *input.ResultNote
		}
		if len(set) == 0 {
			out = item
			return nil
		}
		if err := s.repo.UpdateShootItem(ctx, tx, itemID, input.ExpectedRevision, set, now); err != nil {
			return err
		}
		if input.Result != nil || input.ClearResult || input.ResultNote != nil || input.Tombstone {
			action := "note"
			if input.Result != nil {
				action = "result"
			}
			if input.ClearResult {
				action = "undo"
			}
			if input.Tombstone {
				action = "tombstone"
			}
			result := item.Result
			if input.Result != nil {
				result = input.Result
			}
			if input.ClearResult {
				result = nil
			}
			note := item.ResultNote
			if input.ResultNote != nil {
				note = *input.ResultNote
			}
			if input.ClearResult {
				note = ""
			}
			if err := tx.Insert(ctx, "creative_execution_events", []string{"id", "workspace_id", "shoot_item_id", "action", "previous_result", "result", "result_note", "created_at"}, newID("cev"), workspaceID, itemID, action, item.Result, result, note, now); err != nil {
				return err
			}
		}
		out, err = s.repo.LockShootItem(ctx, tx, itemID)
		if err != nil {
			return err
		}
		list := []ShootItem{out}
		if err := s.repo.fillShootRefs(ctx, tx, list); err != nil {
			return err
		}
		out = list[0]
		return nil
	})
	return out, err
}

func (s *Service) ReorderShootItems(ctx context.Context, sc store.AccountScope, input ReorderShootItemsInput) error {
	if err := s.requirePilot(ctx, sc); err != nil {
		return err
	}
	return sc.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		if err := s.requirePilotInScope(ctx, tx); err != nil {
			return err
		}
		if _, err := s.lockMutableWorkspace(ctx, tx, input.WorkspaceID); err != nil {
			return err
		}
		current, err := s.repo.ListShootItems(ctx, tx, input.WorkspaceID, false)
		if err != nil {
			return err
		}
		order := stableOrder(current, input.ItemIDs, func(i ShootItem) string { return i.ID })
		now := s.now().UTC()
		for i, id := range order {
			if err := s.repo.SetPosition(ctx, tx, "creative_shoot_items", "id", input.WorkspaceID, id, i, now); err != nil {
				return err
			}
		}
		return nil
	})
}

// ---- memos ----

func (s *Service) CreateMemosInScope(ctx context.Context, tx store.TxAccountScope, input CreateMemosInput) ([]Memo, error) {
	if err := s.requirePilotInScope(ctx, tx); err != nil {
		return nil, err
	}
	lines := SplitImportLines(input.Text)
	if len(lines) == 0 {
		return nil, ValidationError{Message: "备忘不能为空"}
	}
	ws, err := s.lockMutableWorkspace(ctx, tx, input.WorkspaceID)
	if err != nil {
		return nil, err
	}
	if ws.Archived {
		return nil, ErrWorkspaceArchived
	}
	pos, err := s.repo.MaxMemoPosition(ctx, tx, ws.ID)
	if err != nil {
		return nil, err
	}
	now := s.now().UTC()
	out := make([]Memo, 0, len(lines))
	for _, l := range lines {
		pos++
		m, err := s.repo.InsertMemo(ctx, tx, ws.ID, truncateRunes(l, maxMemoLen), pos, now)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, nil
}

func (s *Service) UpdateMemo(ctx context.Context, sc store.AccountScope, workspaceID, memoID string, input UpdateMemoInput) error {
	if err := s.requirePilot(ctx, sc); err != nil {
		return err
	}
	if input.Text != nil {
		t := strings.TrimSpace(*input.Text)
		if t == "" || len([]rune(t)) > maxMemoLen {
			return ValidationError{Message: "备忘需在 1–200 字之间"}
		}
		input.Text = &t
	}
	return s.withMutableWorkspace(ctx, sc, workspaceID, func(tx store.TxAccountScope) error {
		return s.repo.UpdateMemo(ctx, tx, workspaceID, memoID, input.Text, input.Checked, s.now().UTC())
	})
}

func (s *Service) DeleteMemo(ctx context.Context, sc store.AccountScope, workspaceID, memoID string) error {
	if err := s.requirePilot(ctx, sc); err != nil {
		return err
	}
	return s.withMutableWorkspace(ctx, sc, workspaceID, func(tx store.TxAccountScope) error { return s.repo.DeleteMemo(ctx, tx, workspaceID, memoID) })
}

func (s *Service) ReorderMemos(ctx context.Context, sc store.AccountScope, input ReorderMemosInput) error {
	if err := s.requirePilot(ctx, sc); err != nil {
		return err
	}
	return sc.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		if err := s.requirePilotInScope(ctx, tx); err != nil {
			return err
		}
		if _, err := s.lockMutableWorkspace(ctx, tx, input.WorkspaceID); err != nil {
			return err
		}
		current, err := s.repo.ListMemos(ctx, tx, input.WorkspaceID)
		if err != nil {
			return err
		}
		order := stableOrder(current, input.MemoIDs, func(m Memo) string { return m.ID })
		now := s.now().UTC()
		for i, id := range order {
			if err := s.repo.SetPosition(ctx, tx, "creative_shoot_memos", "id", input.WorkspaceID, id, i, now); err != nil {
				return err
			}
		}
		return nil
	})
}

// ---- assets ----

// rightsBasisFor 由来源类推导矩阵唯一合法的权利依据（与 planningmedia.ValidateRights 一致）。
func rightsBasisFor(sc SourceClass) (planningmedia.RightsBasis, bool) {
	switch planningmedia.SourceClass(sc) {
	case planningmedia.SourceOfficial, planningmedia.SourceAnimeScreenshot, planningmedia.SourceSettingBook, planningmedia.SourceFan, planningmedia.SourceUnknownWeb:
		return planningmedia.RightsCitationOrDisplay, true
	case planningmedia.SourcePhotographerOwned:
		return planningmedia.RightsOwnershipAttested, true
	case planningmedia.SourceLicensed:
		return planningmedia.RightsLicenseRecorded, true
	case planningmedia.SourceCustomerSupplied:
		return planningmedia.RightsDisplayConsent, true
	}
	return "", false
}

var objectSeg = regexp.MustCompile(`^[A-Za-z0-9._-]{1,128}$`)

func creativeObjectKey(accountID, assetID, rendition, checksum string) (string, error) {
	if !objectSeg.MatchString(accountID) || !objectSeg.MatchString(assetID) || len(checksum) != 71 || !strings.HasPrefix(checksum, "sha256-") {
		return "", fmt.Errorf("creative_object_key_invalid")
	}
	return path.Join("creative", accountID, "assets", assetID, checksum, rendition), nil
}

// UploadAsset 处理一张图片：管线出 original + display 两份渲染，写不可变对象，落资产行。
// 视频等非图片类型在 HTTP 层已拒绝；此处再按管线支持格式兜底。
func (s *Service) UploadAsset(ctx context.Context, sc store.AccountScope, input UploadAssetInput) (Asset, error) {
	if err := s.requirePilot(ctx, sc); err != nil {
		return Asset{}, err
	}
	if s.objects == nil {
		return Asset{}, fmt.Errorf("creative asset object store not configured")
	}
	if !strings.HasPrefix(input.DeclaredMediaType, "image/") {
		return Asset{}, ErrMediaUnsupported
	}
	srcClass := input.SourceClass
	if srcClass == "" {
		srcClass = SourceUnknownWeb
	}
	basis, ok := rightsBasisFor(srcClass)
	if !ok {
		return Asset{}, ValidationError{Message: "素材来源分类无效"}
	}
	if err := planningmedia.ValidateRights(planningmedia.RightsDeclarationInput{SourceClass: planningmedia.SourceClass(srcClass), RightsBasis: basis}); err != nil {
		return Asset{}, ValidationError{Message: "素材权利声明无效"}
	}
	processed, err := planningmedia.ProcessImage(input.Bytes, input.DeclaredMediaType)
	if err != nil {
		return Asset{}, ErrMediaUnsupported
	}
	var out Asset
	published := make([]string, 0, 2)
	err = sc.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		if err := s.requirePilotInScope(ctx, tx); err != nil {
			return err
		}
		ws, err := s.lockMutableWorkspace(ctx, tx, input.WorkspaceID)
		if err != nil {
			return err
		}
		if ws.Archived {
			return ErrWorkspaceArchived
		}
		now := s.now().UTC()
		assetID := "cas_" + uuid.NewString()
		origKey, err := creativeObjectKey(tx.AccountID(), assetID, "original", processed.Original.Checksum)
		if err != nil {
			return err
		}
		dispKey, err := creativeObjectKey(tx.AccountID(), assetID, "display", processed.Display.Checksum)
		if err != nil {
			return err
		}
		for _, r := range []struct {
			key string
			ren planningmedia.Rendition
		}{{origKey, processed.Original}, {dispKey, processed.Display}} {
			_, created, err := s.objects.PutImmutable(ctx, r.key, r.ren.Bytes, immutablefs.Metadata{MediaType: r.ren.MediaType, Size: r.ren.Size, Checksum: r.ren.Checksum, Width: r.ren.Width, Height: r.ren.Height, ModifiedAt: now})
			if err != nil {
				return err
			}
			if created {
				published = append(published, r.key)
			}
		}
		out = Asset{
			ID: assetID, WorkspaceID: ws.ID, SourceClass: srcClass, RightsBasis: string(basis), GenerationReferenceGranted: false,
			OriginalMediaType: processed.Original.MediaType, OriginalSize: processed.Original.Size, OriginalWidth: processed.Original.Width, OriginalHeight: processed.Original.Height, OriginalChecksum: processed.Original.Checksum,
			DisplayMediaType: processed.Display.MediaType, DisplaySize: processed.Display.Size, DisplayWidth: processed.Display.Width, DisplayHeight: processed.Display.Height, DisplayChecksum: processed.Display.Checksum,
			CreatedAt: now,
		}
		return s.repo.InsertAsset(ctx, tx, out)
	})
	if err != nil {
		return Asset{}, s.cleanupFailedUpload(ctx, published, err)
	}
	return out, nil
}

// OpenDisplay 返回展示渲染的只读流；checksum 必须与资产一致（不可变对象，可长缓存）。
func (s *Service) OpenDisplay(ctx context.Context, sc store.AccountScope, workspaceID, assetID, checksum string) (*AssetStream, error) {
	a, err := s.repo.GetAsset(ctx, sc, assetID)
	if err != nil {
		return nil, err
	}
	if a.WorkspaceID != workspaceID && workspaceID != "" {
		// 卡片挪到别的空间后资产仍归原上传空间；允许通过任意本账号空间读取，但校验 checksum。
		_ = a
	}
	if a.DisplayChecksum != checksum {
		return nil, ErrNotFound
	}
	key, err := creativeObjectKey(sc.AccountID(), a.ID, "display", a.DisplayChecksum)
	if err != nil {
		return nil, err
	}
	rc, err := s.objects.Open(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("open display: %w", err)
	}
	return &AssetStream{Body: rc, MediaType: a.DisplayMediaType, Size: a.DisplaySize, ETag: `"` + a.DisplayChecksum + `"`}, nil
}

// AssetStream 是可流式返回的展示图。
type AssetStream struct {
	Body interface {
		Read([]byte) (int, error)
		Close() error
	}
	MediaType string
	Size      int64
	ETag      string
}

// CanonicalHash 用于幂等 canonical body。
func CanonicalHash(parts ...string) string {
	h := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(h[:])
}

// requirePilotInScope checks state after taking the account barrier, including
// callbacks used by the idempotency executor.
func (s *Service) requirePilotInScope(ctx context.Context, tx store.TxAccountScope) error {
	if err := tx.LockCreativeWrite(ctx); err != nil {
		return err
	}
	p, err := s.repo.GetPilot(ctx, tx)
	if err != nil {
		return err
	}
	if p.State != PilotNewWrite {
		return ErrPilotRequired
	}
	return nil
}

func (s *Service) lockMutableWorkspace(ctx context.Context, tx store.TxAccountScope, id string) (Workspace, error) {
	w, err := s.repo.LockWorkspace(ctx, tx, id)
	if err != nil {
		return w, err
	}
	if w.Archived {
		return w, ErrWorkspaceArchived
	}
	return w, nil
}
func (s *Service) withMutableWorkspace(ctx context.Context, sc store.AccountScope, id string, fn func(store.TxAccountScope) error) error {
	return sc.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		if err := s.requirePilotInScope(ctx, tx); err != nil {
			return err
		}
		if _, err := s.lockMutableWorkspace(ctx, tx, id); err != nil {
			return err
		}
		return fn(tx)
	})
}

// Keep objects on uncertain COMMIT: the asset row may already be durable. Such
// objects are reconciled against a database snapshot, never deleted speculatively.
func (s *Service) cleanupFailedUpload(ctx context.Context, keys []string, cause error) error {
	if errors.Is(cause, store.ErrCommitOutcomeUnknown) {
		return cause
	}
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	for i := len(keys) - 1; i >= 0; i-- {
		if err := s.objects.Delete(cleanup, keys[i]); err != nil {
			cause = errors.Join(cause, fmt.Errorf("cleanup creative object %s: %w", keys[i], err))
		}
	}
	return cause
}
