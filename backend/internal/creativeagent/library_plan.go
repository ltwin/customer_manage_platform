package creativeagent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/samson/customer-manage-platform/backend/internal/creativecanvas"
	"github.com/samson/customer-manage-platform/backend/internal/creativelibrary"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

const (
	organizeAssetsTool    = "organize_assets"
	manageLibraryTool     = "manage_library"
	trashAssetsTool       = "trash_assets"
	restoreAssetsTool     = "restore_assets"
	readLibraryResultTool = "read_library_result"
)

func libraryWriteTool(name string) bool {
	return slices.Contains([]string{organizeAssetsTool, manageLibraryTool, trashAssetsTool, restoreAssetsTool, saveLibraryTool}, name)
}

type organizeItem struct {
	ID           string               `json:"asset_id"`
	Revision     creativeops.Revision `json:"expected_revision"`
	Title        *string              `json:"title,omitempty"`
	Description  *string              `json:"description,omitempty"`
	AddTags      []string             `json:"add_tag_ids,omitempty"`
	RemoveTags   []string             `json:"remove_tag_ids,omitempty"`
	AddGroups    []string             `json:"add_group_ids,omitempty"`
	RemoveGroups []string             `json:"remove_group_ids,omitempty"`
}
type libraryAction struct {
	Action     string               `json:"action"`
	GroupID    string               `json:"group_id,omitempty"`
	TagID      string               `json:"tag_id,omitempty"`
	CategoryID json.RawMessage      `json:"category_id,omitempty"`
	Revision   creativeops.Revision `json:"expected_revision,omitempty"`
	Name       *string              `json:"name,omitempty"`
	ParentID   json.RawMessage      `json:"parent_id,omitempty"`
	Position   *int                 `json:"position,omitempty"`
}
type libraryPhasePlan struct {
	Name    string          `json:"name"`
	Payload json.RawMessage `json:"payload"`
}
type libraryItemPlan struct {
	ID       string               `json:"id"`
	Action   string               `json:"action,omitempty"`
	Revision creativeops.Revision `json:"revision,omitempty"`
	Status   string               `json:"status,omitempty"`
	Before   map[string]any       `json:"before,omitempty"`
	Changes  []map[string]any     `json:"changes,omitempty"`
	Phases   []libraryPhasePlan   `json:"phases"`
}
type libraryPlan struct {
	Items         []libraryItemPlan `json:"items"`
	RetentionDays *int              `json:"retention_days"`
}

func phase(name string, payload any) (libraryPhasePlan, error) {
	raw, err := json.Marshal(payload)
	return libraryPhasePlan{Name: name, Payload: raw}, err
}
func validLibraryID(id string) bool { return id != "" && len(id) <= 100 }
func validRelationDiff(add, remove []string) bool {
	if len(add) > 50 || len(remove) > 50 {
		return false
	}
	seen := map[string]bool{}
	for _, id := range add {
		if !validLibraryID(id) || seen[id] {
			return false
		}
		seen[id] = true
	}
	for _, id := range remove {
		if !validLibraryID(id) || seen[id] {
			return false
		}
		seen[id] = true
	}
	return true
}
func libraryFailure(err error) string {
	switch {
	case errors.Is(err, creativelibrary.ErrVersionConflict), errors.Is(err, creativecanvas.ErrVersionConflict):
		return "conflict"
	case errors.Is(err, creativelibrary.ErrNotFound), errors.Is(err, creativecanvas.ErrNotFound), errors.Is(err, store.ErrNoRows):
		return "not_found"
	case errors.Is(err, creativelibrary.ErrTrashed):
		return "trashed"
	case errors.Is(err, creativeops.ErrValidation), errors.Is(err, creativelibrary.ErrGroupDepth), errors.Is(err, creativelibrary.ErrGroupNameTaken):
		return "refused"
	}
	var public creativeops.Public
	if errors.As(err, &public) {
		return "refused"
	}
	return ""
}
func (h *runtimeHandler) prepareLibraryPlan(ctx context.Context, name, raw string) (libraryPlan, error) {
	if err := validateLibraryNulls(raw); err != nil {
		return libraryPlan{}, err
	}
	switch name {
	case manageLibraryTool:
		return h.prepareLibraryActions(ctx, raw)
	case saveLibraryTool:
		return h.prepareLibrarySave(ctx, raw)
	}
	var input struct {
		Items []organizeItem `json:"items"`
	}
	if name == organizeAssetsTool {
		if err := strictArguments(raw, &input); err != nil {
			return libraryPlan{}, err
		}
	} else {
		var versions struct {
			Items []creativelibrary.AssetVersion `json:"items"`
		}
		if err := strictArguments(raw, &versions); err != nil {
			return libraryPlan{}, err
		}
		for _, v := range versions.Items {
			input.Items = append(input.Items, organizeItem{ID: v.ID, Revision: v.Revision})
		}
	}
	if len(input.Items) < 1 || len(input.Items) > 50 {
		return libraryPlan{}, refuseTool("每次 1 到 50 个素材")
	}
	seen := map[string]bool{}
	for _, v := range input.Items {
		if !validLibraryID(v.ID) || v.Revision < 1 || seen[v.ID] {
			return libraryPlan{}, errToolArguments
		}
		seen[v.ID] = true
		if name == organizeAssetsTool {
			if v.Title != nil && (strings.TrimSpace(*v.Title) == "" || utf8.RuneCountInString(*v.Title) > 200) || v.Description != nil && utf8.RuneCountInString(*v.Description) > creativelibrary.DescriptionLimit || !validRelationDiff(v.AddTags, v.RemoveTags) || !validRelationDiff(v.AddGroups, v.RemoveGroups) {
				return libraryPlan{}, errToolArguments
			}
			if v.Title == nil && v.Description == nil && v.AddTags == nil && v.RemoveTags == nil && v.AddGroups == nil && v.RemoveGroups == nil {
				return libraryPlan{}, errToolArguments
			}
		}
	}
	plan := libraryPlan{Items: []libraryItemPlan{}}
	err := h.scope.WithReadSnapshot(ctx, func(tx store.ReadTxAccountScope) error {
		if err := tx.RequireCreativeRead(ctx); err != nil {
			return err
		}
		if name == trashAssetsTool {
			err := tx.QueryRow(ctx, "creative_library_settings", "retention_days", "").Scan(&plan.RetentionDays)
			if err != nil && !errors.Is(err, store.ErrNoRows) {
				return err
			}
		}
		for _, v := range input.Items {
			item := libraryItemPlan{ID: v.ID, Phases: []libraryPhasePlan{}}
			var title, description string
			var deletedAt *time.Time
			err := tx.QueryRow(ctx, "creative_assets", "revision,title,description,deleted_at", "id=$2", v.ID).Scan(&item.Revision, &title, &description, &deletedAt)
			if errors.Is(err, store.ErrNoRows) {
				item.Status = "not_found"
			} else if err != nil {
				return err
			} else if item.Revision != v.Revision {
				item.Status = "conflict"
			} else if deletedAt != nil && name != restoreAssetsTool {
				item.Status = "trashed"
			} else if deletedAt == nil && name == restoreAssetsTool {
				item.Status = "not_trashed"
			}
			if item.Status != "" {
				plan.Items = append(plan.Items, item)
				continue
			}
			revision := v.Revision
			if name != organizeAssetsTool {
				p, err := phase(name, creativelibrary.BatchAssetInput{Assets: []creativelibrary.AssetVersion{{ID: v.ID, Revision: v.Revision}}})
				if err != nil {
					return err
				}
				item.Phases = append(item.Phases, p)
			} else {
				metadata := creativelibrary.MetadataInput{ID: v.ID, ExpectedRevision: revision}
				item.Before = map[string]any{"title": title, "description": description}
				if v.Title != nil && *v.Title != title {
					metadata.Title = v.Title
				}
				if v.Description != nil && *v.Description != description {
					metadata.Description = v.Description
				}
				if metadata.Title != nil || metadata.Description != nil {
					p, err := phase("metadata", metadata)
					if err != nil {
						return err
					}
					item.Phases = append(item.Phases, p)
					revision++
				}
				organize := creativelibrary.OrganizeInput{Assets: []creativelibrary.AssetVersion{{ID: v.ID, Revision: revision}}}
				for _, rel := range []struct {
					kind, table, link, column string
					add, remove               []string
					outAdd, outRemove         *[]string
				}{{"tag", "creative_tags", "creative_asset_tags", "tag_id", v.AddTags, v.RemoveTags, &organize.AddTags, &organize.RemoveTags}, {"group", "creative_asset_groups", "creative_asset_group_members", "group_id", v.AddGroups, v.RemoveGroups, &organize.AddGroups, &organize.RemoveGroups}} {
					for _, add := range []bool{true, false} {
						ids := rel.remove
						if add {
							ids = rel.add
						}
						for _, id := range ids {
							var label string
							err := tx.QueryRow(ctx, rel.table, "name", "id=$2", id).Scan(&label)
							if errors.Is(err, store.ErrNoRows) {
								item.Status = "not_found"
								break
							}
							if err != nil {
								return err
							}
							exists, err := tx.Exists(ctx, rel.link, "asset_id=$2 AND "+rel.column+"=$3", v.ID, id)
							if err != nil {
								return err
							}
							if add == exists {
								continue
							}
							operation := "removed"
							if add {
								*rel.outAdd = append(*rel.outAdd, id)
								operation = "added"
							} else {
								*rel.outRemove = append(*rel.outRemove, id)
							}
							item.Changes = append(item.Changes, map[string]any{"kind": rel.kind, "id": id, "name": label, "change": operation})
						}
					}
				}
				if item.Status != "" {
					item.Phases = nil
					item.Changes = nil
				} else if len(item.Changes) > 0 {
					p, err := phase("organize", organize)
					if err != nil {
						return err
					}
					item.Phases = append(item.Phases, p)
				}
			}
			if len(item.Phases) == 0 && item.Status == "" {
				item.Status = "unchanged"
			}
			plan.Items = append(plan.Items, item)
		}
		return nil
	})
	return plan, err
}

func nullableID(raw json.RawMessage) (*string, error) {
	if raw == nil {
		return nil, nil
	}
	var id *string
	if json.Unmarshal(raw, &id) != nil || id != nil && !validLibraryID(*id) {
		return nil, errToolArguments
	}
	return id, nil
}
func (h *runtimeHandler) prepareLibraryActions(ctx context.Context, raw string) (libraryPlan, error) {
	var args struct {
		Actions []libraryAction `json:"actions"`
	}
	if err := strictArguments(raw, &args); err != nil {
		return libraryPlan{}, err
	}
	if len(args.Actions) < 1 || len(args.Actions) > 20 {
		return libraryPlan{}, refuseTool("每次 1 到 20 个结构动作")
	}
	for _, a := range args.Actions {
		if err := validateLibraryAction(a); err != nil {
			return libraryPlan{}, err
		}
	}
	plan := libraryPlan{Items: []libraryItemPlan{}}
	err := h.scope.WithReadSnapshot(ctx, func(tx store.ReadTxAccountScope) error {
		for _, a := range args.Actions {
			item := libraryItemPlan{Action: a.Action, Phases: []libraryPhasePlan{}}
			payload := map[string]any{"hierarchy_revision": "1"}
			if a.Revision > 0 {
				payload["expected_revision"] = a.Revision
			}
			if a.Name != nil {
				payload["name"] = *a.Name
			}
			if a.Position != nil {
				payload["position"] = *a.Position
			}
			parent, _ := nullableID(a.ParentID)
			category, _ := nullableID(a.CategoryID)
			table := ""
			switch {
			case strings.HasSuffix(a.Action, "group"):
				item.ID = a.GroupID
				table = "creative_asset_groups"
				payload["group_id"] = a.GroupID
				payload["parent_id"] = parent
			case strings.HasSuffix(a.Action, "category"):
				if category != nil {
					item.ID = *category
				}
				table = "creative_tag_categories"
				payload["category_id"] = item.ID
			case strings.HasSuffix(a.Action, "tag"):
				item.ID = a.TagID
				table = "creative_tags"
				payload["tag_id"] = a.TagID
				payload["category_id"] = category
				payload["color"] = "#a1a1a7"
			}
			if !strings.HasPrefix(a.Action, "create_") {
				err := tx.QueryRow(ctx, table, "revision", "id=$2", item.ID).Scan(&item.Revision)
				if errors.Is(err, store.ErrNoRows) {
					item.Status = "not_found"
				} else if err != nil {
					return err
				} else if item.Revision != a.Revision {
					item.Status = "conflict"
				}
				if a.Action == "edit_tag" && item.Status == "" {
					var name, color string
					var oldCategory *string
					if err := tx.QueryRow(ctx, table, "name,color,category_id", "id=$2", item.ID).Scan(&name, &color, &oldCategory); err != nil {
						return err
					}
					payload["color"] = color
					if a.Name == nil {
						payload["name"] = name
					}
					if a.CategoryID == nil {
						payload["category_id"] = oldCategory
					}
				}
			}
			if item.Status == "" {
				p, err := phase(a.Action, payload)
				if err != nil {
					return err
				}
				item.Phases = append(item.Phases, p)
			}
			plan.Items = append(plan.Items, item)
		}
		return nil
	})
	return plan, err
}
func validateLibraryAction(a libraryAction) error {
	allowed := map[string][]string{
		"create_group": {"name", "parent_id", "position"}, "rename_group": {"group_id", "expected_revision", "name"}, "move_group": {"group_id", "expected_revision", "parent_id", "position"}, "delete_group": {"group_id", "expected_revision"},
		"create_category": {"name"}, "rename_category": {"category_id", "expected_revision", "name"}, "delete_category": {"category_id", "expected_revision"},
		"create_tag": {"name", "category_id"}, "edit_tag": {"tag_id", "expected_revision", "name", "category_id"}, "delete_tag": {"tag_id", "expected_revision"},
	}
	fields, ok := allowed[a.Action]
	if !ok {
		return errToolArguments
	}
	present := map[string]bool{"name": a.Name != nil, "parent_id": a.ParentID != nil, "category_id": a.CategoryID != nil, "position": a.Position != nil, "group_id": a.GroupID != "", "tag_id": a.TagID != "", "expected_revision": a.Revision != 0}
	for k, v := range present {
		if v && !slices.Contains(fields, k) {
			return errToolArguments
		}
	}
	if a.Name != nil && (strings.TrimSpace(*a.Name) == "" || utf8.RuneCountInString(*a.Name) > 80) || a.Position != nil && *a.Position < 0 {
		return errToolArguments
	}
	if _, err := nullableID(a.ParentID); err != nil {
		return err
	}
	cat, err := nullableID(a.CategoryID)
	if err != nil {
		return err
	}
	if strings.HasPrefix(a.Action, "create_") || strings.HasPrefix(a.Action, "rename_") {
		if a.Name == nil {
			return errToolArguments
		}
	}
	if !strings.HasPrefix(a.Action, "create_") {
		if a.Revision < 1 {
			return errToolArguments
		}
		switch {
		case strings.HasSuffix(a.Action, "group"):
			if !validLibraryID(a.GroupID) {
				return errToolArguments
			}
		case strings.HasSuffix(a.Action, "tag"):
			if !validLibraryID(a.TagID) {
				return errToolArguments
			}
		case strings.HasSuffix(a.Action, "category"):
			if cat == nil {
				return errToolArguments
			}
		}
	}
	if a.Action == "move_group" && a.ParentID == nil || a.Action == "edit_tag" && a.Name == nil && a.CategoryID == nil {
		return errToolArguments
	}
	return nil
}

type saveLibraryArgs struct {
	NodeID      string   `json:"node_id"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	GroupIDs    []string `json:"group_ids"`
	TagIDs      []string `json:"tag_ids"`
	NewTags     []struct {
		Name       string  `json:"name"`
		CategoryID *string `json:"category_id,omitempty"`
	} `json:"new_tags"`
}

func (h *runtimeHandler) prepareLibrarySave(ctx context.Context, raw string) (libraryPlan, error) {
	var a saveLibraryArgs
	if err := strictArguments(raw, &a); err != nil {
		return libraryPlan{}, err
	}
	if !validLibraryID(a.NodeID) || strings.TrimSpace(a.Description) == "" || len(a.NewTags) > 10 {
		return libraryPlan{}, errToolArguments
	}
	target := creativelibrary.ImportTarget{Title: a.Title, Description: a.Description, GroupIDs: a.GroupIDs, TagIDs: a.TagIDs}
	for i, t := range a.NewTags {
		if strings.TrimSpace(t.Name) == "" || utf8.RuneCountInString(t.Name) > 80 {
			return libraryPlan{}, errToolArguments
		}
		target.NewTags = append(target.NewTags, creativelibrary.NewTag{ClientKey: fmt.Sprint(i), Name: t.Name, Color: "#a1a1a7", CategoryID: t.CategoryID})
	}
	if err := creativelibrary.ValidateImportTarget(target); err != nil {
		return libraryPlan{}, err
	}
	var canvasID string
	var revision creativeops.Revision
	var contentID *string
	err := h.scope.WithReadSnapshot(ctx, func(tx store.ReadTxAccountScope) error {
		if err := tx.QueryRow(ctx, "creative_agent_conversations", "canvas_id", "id=$2", h.held.ConversationID).Scan(&canvasID); err != nil {
			return err
		}
		return tx.QueryRow(ctx, "creative_nodes", "data_revision,content_revision_id", "id=$2 AND canvas_id=$3", a.NodeID, canvasID).Scan(&revision, &contentID)
	})
	if err != nil {
		return libraryPlan{}, err
	}
	if contentID == nil {
		return libraryPlan{}, errToolArguments
	}
	p, err := phase(saveLibraryTool, creativecanvas.SaveNodeInput{CanvasID: canvasID, NodeID: a.NodeID, ExpectedDataRevision: revision, Target: target})
	return libraryPlan{Items: []libraryItemPlan{{ID: a.NodeID, Before: map[string]any{"content_revision_id": *contentID}, Phases: []libraryPhasePlan{p}}}}, err
}

// Null is meaningful only for nullable hierarchy selectors, never an alias for
// omitting metadata or a relation list.
func validateLibraryNulls(raw string) error {
	var root map[string]json.RawMessage
	if err := strictArguments(raw, &root); err != nil {
		return err
	}
	for _, field := range []string{"items", "actions"} {
		value, ok := root[field]
		if !ok {
			continue
		}
		var entries []map[string]json.RawMessage
		if json.Unmarshal(value, &entries) != nil {
			return errToolArguments
		}
		for _, entry := range entries {
			for key, v := range entry {
				if string(v) == "null" && key != "parent_id" && key != "category_id" {
					return errToolArguments
				}
			}
		}
	}
	return nil
}
