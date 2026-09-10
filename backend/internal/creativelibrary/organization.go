package creativelibrary

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

type libraryReader interface {
	Query(context.Context, string, string, string, ...any) (store.Rows, error)
	QueryRow(context.Context, string, string, string, ...any) store.Row
	QueryPage(context.Context, string, string, string, []store.OrderBy, int, int, ...any) (store.Rows, error)
	Count(context.Context, string, string, ...any) (int64, error)
	Exists(context.Context, string, string, ...any) (bool, error)
}
type Settings struct {
	RetentionDays     *int                 `json:"retention_days"`
	Revision          creativeops.Revision `json:"revision"`
	HierarchyRevision creativeops.Revision `json:"hierarchy_revision"`
	LibraryRevision   creativeops.Revision `json:"library_revision"`
}
type LibraryResult struct {
	ID                string               `json:"id"`
	Revision          creativeops.Revision `json:"revision"`
	HierarchyRevision creativeops.Revision `json:"hierarchy_revision"`
	LibraryRevision   creativeops.Revision `json:"library_revision"`
}
type Group struct {
	ID       string               `json:"id"`
	ParentID *string              `json:"parent_id"`
	Name     string               `json:"name"`
	Position int                  `json:"position"`
	Revision creativeops.Revision `json:"revision"`
}
type GroupPage struct {
	Items             []Group              `json:"items"`
	HierarchyRevision creativeops.Revision `json:"hierarchy_revision"`
}
type GroupInput struct {
	ID                string               `json:"group_id"`
	Name              string               `json:"name"`
	ParentID          *string              `json:"parent_id"`
	Position          *int                 `json:"position"`
	ExpectedRevision  creativeops.Revision `json:"expected_revision,omitempty"`
	HierarchyRevision creativeops.Revision `json:"hierarchy_revision"`
}

func validName(s string) bool { return strings.TrimSpace(s) != "" && utf8.RuneCountInString(s) <= 80 }
func libraryRun[T any](ctx context.Context, sc store.AccountScope, c creativeops.Command, key string, validate func(T) error, apply func(context.Context, store.TxAccountScope, T) (LibraryResult, error)) (creativeops.Receipt, error) {
	return (creativeops.Executor{}).Run(ctx, sc, creativeops.Operation{Key: "library." + key, Capability: "manual_write", Validate: func(raw json.RawMessage) error {
		var v T
		if err := creativeops.Decode(raw, &v); err != nil {
			return err
		}
		return validate(v)
	}, Apply: func(ctx context.Context, tx store.TxAccountScope, raw json.RawMessage) (creativeops.Outcome, error) {
		var v T
		if err := creativeops.Decode(raw, &v); err != nil {
			return creativeops.Outcome{}, err
		}
		if _, err := lockLibrary(ctx, tx); err != nil {
			return creativeops.Outcome{}, err
		}
		result, err := apply(ctx, tx, v)
		if err != nil {
			return creativeops.Outcome{}, err
		}
		body, err := json.Marshal(result)
		if err != nil {
			return creativeops.Outcome{}, err
		}
		return creativeops.Outcome{HTTPStatus: 200, Response: body, ResultKind: "library", ResultID: &result.ID, ResultRevision: &result.Revision}, nil
	}}, c)
}
func readSettings(ctx context.Context, tx libraryReader) (Settings, error) {
	var s Settings
	err := tx.QueryRow(ctx, "creative_library_settings", "retention_days,revision,hierarchy_revision,library_revision", "").Scan(&s.RetentionDays, &s.Revision, &s.HierarchyRevision, &s.LibraryRevision)
	if errors.Is(err, store.ErrNoRows) {
		return Settings{Revision: 1, HierarchyRevision: 1, LibraryRevision: 1}, nil
	}
	return s, err
}
func GetSettings(ctx context.Context, sc store.AccountScope) (Settings, error) {
	var s Settings
	err := sc.WithReadSnapshot(ctx, func(tx store.ReadTxAccountScope) error {
		if err := tx.RequireCreativeRead(ctx); err != nil {
			return err
		}
		var err error
		s, err = readSettings(ctx, tx)
		return err
	})
	return s, err
}
func hierarchy(ctx context.Context, tx store.TxAccountScope, expected creativeops.Revision) error {
	s, err := readSettings(ctx, tx)
	if err != nil {
		return err
	}
	if s.HierarchyRevision != expected {
		return ErrVersionConflict
	}
	return nil
}
func libraryResult(ctx context.Context, tx store.TxAccountScope, id string, revision creativeops.Revision, changed, structural bool) (LibraryResult, error) {
	if changed {
		set := "library_revision=library_revision+1"
		if structural {
			set += ",hierarchy_revision=hierarchy_revision+1"
		}
		if _, err := tx.Update(ctx, "creative_library_settings", set, ""); err != nil {
			return LibraryResult{}, err
		}
	}
	s, err := readSettings(ctx, tx)
	return LibraryResult{ID: id, Revision: revision, HierarchyRevision: s.HierarchyRevision, LibraryRevision: s.LibraryRevision}, err
}
func readGroups(ctx context.Context, tx libraryReader) ([]Group, error) {
	rows, err := tx.QueryPage(ctx, "creative_asset_groups", "id,parent_id,name,position,revision", "", []store.OrderBy{{Column: "position"}, {Column: "id"}}, 10001, 0)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	groups := []Group{}
	for rows.Next() {
		var g Group
		if err := rows.Scan(&g.ID, &g.ParentID, &g.Name, &g.Position, &g.Revision); err != nil {
			return nil, err
		}
		groups = append(groups, g)
	}
	return groups, rows.Err()
}
func ListGroups(ctx context.Context, sc store.AccountScope) (GroupPage, error) {
	var p GroupPage
	err := sc.WithReadSnapshot(ctx, func(tx store.ReadTxAccountScope) error {
		if err := tx.RequireCreativeRead(ctx); err != nil {
			return err
		}
		s, err := readSettings(ctx, tx)
		if err != nil {
			return err
		}
		p.HierarchyRevision = s.HierarchyRevision
		p.Items, err = readGroups(ctx, tx)
		return err
	})
	return p, err
}
func parentKey(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
func groupCommand(ctx context.Context, sc store.AccountScope, c creativeops.Command, action string) (creativeops.Receipt, error) {
	return libraryRun(ctx, sc, c, "group_"+action, func(v GroupInput) error {
		if v.HierarchyRevision < 1 || (action != "create" && (v.ID == "" || v.ExpectedRevision < 1)) || ((action == "create" || action == "rename") && !validName(v.Name)) || (v.Position != nil && *v.Position < 0) {
			return creativeops.ErrValidation
		}
		return nil
	}, func(ctx context.Context, tx store.TxAccountScope, v GroupInput) (LibraryResult, error) {
		groups, err := readGroups(ctx, tx)
		if err != nil {
			return LibraryResult{}, err
		}
		byID := map[string]Group{}
		for _, g := range groups {
			byID[g.ID] = g
		}
		target, exists := byID[v.ID]
		if action != "create" {
			if !exists {
				return LibraryResult{}, ErrNotFound
			}
			if target.Revision != v.ExpectedRevision {
				return LibraryResult{}, ErrVersionConflict
			}
		}
		if err := hierarchy(ctx, tx, v.HierarchyRevision); err != nil {
			return LibraryResult{}, err
		}
		original := map[string]Group{}
		for id, g := range byID {
			original[id] = g
		}
		if action == "create" || action == "move" {
			if v.ParentID != nil {
				if _, ok := byID[*v.ParentID]; !ok {
					return LibraryResult{}, ErrNotFound
				}
			}
			seen := map[string]bool{}
			for id := parentKey(v.ParentID); id != ""; id = parentKey(byID[id].ParentID) {
				if id == v.ID || seen[id] {
					return LibraryResult{}, creativeops.ErrValidation
				}
				seen[id] = true
			}
			if action == "create" {
				if len(groups) >= 10000 {
					return LibraryResult{}, creativeops.ErrValidation
				}
				target = Group{ID: "ccag_" + uuid.NewString(), Name: v.Name, Revision: 1}
			}
			oldParent := parentKey(target.ParentID)
			target.ParentID = v.ParentID
			byID[target.ID] = target
			siblings := []Group{}
			for _, g := range groups {
				if g.ID != target.ID && parentKey(g.ParentID) == parentKey(v.ParentID) {
					siblings = append(siblings, g)
				}
			}
			pos := len(siblings)
			if v.Position != nil {
				pos = *v.Position
				if pos > len(siblings) {
					return LibraryResult{}, creativeops.ErrValidation
				}
			}
			siblings = append(siblings, Group{})
			copy(siblings[pos+1:], siblings[pos:])
			siblings[pos] = target
			for i, g := range siblings {
				g.Position = i
				byID[g.ID] = g
			}
			if oldParent != parentKey(v.ParentID) {
				i := 0
				for _, g := range groups {
					if g.ID != target.ID && parentKey(g.ParentID) == oldParent {
						g.Position = i
						i++
						byID[g.ID] = g
					}
				}
			}
		} else if action == "rename" {
			target.Name = v.Name
			byID[target.ID] = target
		} else if action == "delete" {
			if err := removeRelations(ctx, tx, "creative_asset_group_members", "group_id", target.ID); err != nil {
				return LibraryResult{}, err
			}
			delete(byID, target.ID)
			siblings := []Group{}
			for _, g := range groups {
				if g.ID == target.ID {
					continue
				}
				if parentKey(g.ParentID) == target.ID {
					g.ParentID = target.ParentID
					g.Position = target.Position
					byID[g.ID] = g
				}
				if parentKey(g.ParentID) == parentKey(target.ParentID) {
					siblings = append(siblings, g)
				}
			}
			sort.SliceStable(siblings, func(i, j int) bool { return siblings[i].Position < siblings[j].Position })
			for i, g := range siblings {
				g.Position = i
				byID[g.ID] = g
			}
			if _, err := tx.Delete(ctx, "creative_asset_groups", "id=$2", target.ID); err != nil {
				return LibraryResult{}, err
			}
		}
		changed := action == "delete"
		for id, g := range byID {
			old, existed := original[id]
			if !existed {
				err = tx.Insert(ctx, "creative_asset_groups", []string{"id", "parent_id", "name", "position"}, id, g.ParentID, g.Name, g.Position)
				changed = true
			} else if g.Name != old.Name || g.Position != old.Position || parentKey(g.ParentID) != parentKey(old.ParentID) {
				_, err = tx.Update(ctx, "creative_asset_groups", "parent_id=$2,name=$3,position=$4,revision=revision+1", "id=$5", g.ParentID, g.Name, g.Position, id)
				g.Revision++
				byID[id] = g
				changed = true
			}
			if err != nil {
				return LibraryResult{}, err
			}
		}
		rev := target.Revision
		if g, ok := byID[target.ID]; ok {
			rev = g.Revision
		}
		return libraryResult(ctx, tx, target.ID, rev, changed, true)
	})
}
func CreateGroup(ctx context.Context, s store.AccountScope, c creativeops.Command) (creativeops.Receipt, error) {
	return groupCommand(ctx, s, c, "create")
}
func RenameGroup(ctx context.Context, s store.AccountScope, c creativeops.Command) (creativeops.Receipt, error) {
	return groupCommand(ctx, s, c, "rename")
}
func MoveGroup(ctx context.Context, s store.AccountScope, c creativeops.Command) (creativeops.Receipt, error) {
	return groupCommand(ctx, s, c, "move")
}
func DeleteGroup(ctx context.Context, s store.AccountScope, c creativeops.Command) (creativeops.Receipt, error) {
	return groupCommand(ctx, s, c, "delete")
}
func removeRelations(ctx context.Context, tx store.TxAccountScope, table, column, id string) error {
	// table/column are application constants, never request-derived identifiers.
	if _, err := tx.Update(ctx, "creative_assets", "revision=revision+1,updated_at=clock_timestamp()", "id IN (SELECT asset_id FROM "+table+" WHERE account_id=$1 AND "+column+"=$2)", id); err != nil {
		return err
	}
	if _, err := tx.Update(ctx, "creative_asset_search", "asset_revision=(SELECT a.revision FROM creative_assets a WHERE a.account_id=$1 AND a.id=creative_asset_search.asset_id)", "asset_id IN (SELECT asset_id FROM "+table+" WHERE account_id=$1 AND "+column+"=$2)", id); err != nil {
		return err
	}
	_, err := tx.Delete(ctx, table, column+"=$2", id)
	return err
}
