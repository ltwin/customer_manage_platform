package creativelibrary

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// groupDeletion is what a delete undoes: the group itself, the children it
// promoted (in their old order) and the assets that were filed in it. The ids
// are historical keys; a restore skips the assets purged since.
type groupDeletion struct {
	group    Group
	children []string
	members  []string
}

func snapshotGroup(ctx context.Context, tx store.TxAccountScope, target Group, groups []Group) (groupDeletion, error) {
	d := groupDeletion{group: target, children: []string{}, members: []string{}}
	for _, g := range groups { // readGroups orders by position, id
		if parentKey(g.ParentID) == target.ID {
			d.children = append(d.children, g.ID)
		}
	}
	rows, err := tx.Query(ctx, "creative_asset_group_members", "asset_id", "group_id=$2", target.ID)
	if err != nil {
		return d, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return d, err
		}
		d.members = append(d.members, id)
	}
	return d, rows.Err()
}

func (d groupDeletion) save(ctx context.Context, tx store.TxAccountScope, hierarchyAfter creativeops.Revision) (string, error) {
	snapshot, err := json.Marshal(d.group)
	if err != nil {
		return "", err
	}
	id := "ccgd_" + uuid.NewString()
	return id, tx.Insert(ctx, "creative_asset_group_deletions", []string{"id", "group_snapshot", "promoted_child_ids", "member_asset_ids", "hierarchy_revision_after"}, id, snapshot, d.children, d.members, int64(hierarchyAfter))
}

type RestoreGroupInput struct {
	DeletionID string `json:"deletion_id"`
}

// RestoreGroup undoes one group deletion within 30 days. It only applies while the group
// structure is exactly as the delete left it; filing assets in the meantime
// does not count, and memberships are restored as a union with today's.
func RestoreGroup(ctx context.Context, sc store.AccountScope, c creativeops.Command) (creativeops.Receipt, error) {
	return libraryRun(ctx, sc, c, "group_restore", func(v RestoreGroupInput) error {
		if v.DeletionID == "" {
			return creativeops.ErrValidation
		}
		return nil
	}, func(ctx context.Context, tx store.TxAccountScope, v RestoreGroupInput) (LibraryResult, error) {
		var raw []byte
		var d groupDeletion
		var after int64
		var restored *time.Time
		err := tx.QueryRowForUpdate(ctx, "creative_asset_group_deletions", "group_snapshot,promoted_child_ids,member_asset_ids,hierarchy_revision_after,restored_at", "id=$2 AND created_at>clock_timestamp()-interval '30 days'", v.DeletionID).Scan(&raw, &d.children, &d.members, &after, &restored)
		if errors.Is(err, store.ErrNoRows) {
			return LibraryResult{}, ErrNotFound
		}
		if err != nil {
			return LibraryResult{}, err
		}
		if restored != nil {
			return LibraryResult{}, ErrVersionConflict
		}
		if err := hierarchy(ctx, tx, creativeops.Revision(after)); err != nil {
			return LibraryResult{}, err
		}
		if err := json.Unmarshal(raw, &d.group); err != nil {
			return LibraryResult{}, err
		}
		groups, err := readGroups(ctx, tx)
		if err != nil {
			return LibraryResult{}, err
		}
		if err := d.restoreTree(ctx, tx, groups); err != nil {
			return LibraryResult{}, err
		}
		if err := d.restoreMembers(ctx, tx); err != nil {
			return LibraryResult{}, err
		}
		if _, err := tx.Update(ctx, "creative_asset_group_deletions", "restored_at=clock_timestamp()", "id=$2", v.DeletionID); err != nil {
			return LibraryResult{}, err
		}
		return libraryResult(ctx, tx, d.group.ID, d.group.Revision, true, true)
	})
}

// restoreTree puts the group back at its old place and its promoted children
// back under it. The hierarchy is unchanged since the delete, so this is the
// exact inverse of the delete's renumbering.
func (d *groupDeletion) restoreTree(ctx context.Context, tx store.TxAccountScope, groups []Group) error {
	promoted := map[string]bool{}
	for _, id := range d.children {
		promoted[id] = true
	}
	siblings := []Group{}
	for _, g := range groups {
		if parentKey(g.ParentID) == parentKey(d.group.ParentID) && !promoted[g.ID] {
			siblings = append(siblings, g)
		}
	}
	sort.SliceStable(siblings, func(i, j int) bool { return siblings[i].Position < siblings[j].Position })
	pos := min(d.group.Position, len(siblings))
	d.group.Revision++
	if err := tx.Insert(ctx, "creative_asset_groups", []string{"id", "parent_id", "name", "position", "revision"}, d.group.ID, d.group.ParentID, d.group.Name, pos, int64(d.group.Revision)); err != nil {
		return err
	}
	for i, g := range siblings {
		at := i
		if i >= pos {
			at++
		}
		if g.Position != at {
			if _, err := tx.Update(ctx, "creative_asset_groups", "position=$2,revision=revision+1", "id=$3", at, g.ID); err != nil {
				return err
			}
		}
	}
	for i, id := range d.children {
		if _, err := tx.Update(ctx, "creative_asset_groups", "parent_id=$2,position=$3,revision=revision+1", "id=$4", d.group.ID, i, id); err != nil {
			return err
		}
	}
	return nil
}

// restoreMembers refiles the surviving assets, trashed ones included, since a
// trashed asset keeps its relations. If an asset has meanwhile reached the
// per-asset group limit, the whole undo is refused rather than silently
// dropping that membership; the caller's transaction rolls back and the
// snapshot stays restorable.
func (d *groupDeletion) restoreMembers(ctx context.Context, tx store.TxAccountScope) error {
	if len(d.members) == 0 {
		return nil
	}
	cond := "id=ANY($2::text[]) AND NOT EXISTS(SELECT 1 FROM creative_asset_group_members m WHERE m.account_id=$1 AND m.asset_id=creative_assets.id AND m.group_id=$3)"
	full, err := tx.Exists(ctx, "creative_assets", cond+" AND (SELECT count(*) FROM creative_asset_group_members m WHERE m.account_id=$1 AND m.asset_id=creative_assets.id)>=100", d.members, d.group.ID)
	if err != nil {
		return err
	}
	if full {
		return ErrGroupRestoreLimit
	}
	rows, err := tx.Query(ctx, "creative_assets", "id", cond, d.members, d.group.ID)
	if err != nil {
		return err
	}
	refile := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		refile = append(refile, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	sort.Strings(refile)
	for _, id := range refile {
		if err := tx.Insert(ctx, "creative_asset_group_members", []string{"asset_id", "group_id"}, id, d.group.ID); err != nil {
			return err
		}
	}
	if len(refile) == 0 {
		return nil
	}
	if _, err := tx.Update(ctx, "creative_assets", "revision=revision+1,updated_at=clock_timestamp()", "id=ANY($2::text[])", refile); err != nil {
		return err
	}
	_, err = tx.Update(ctx, "creative_asset_search", "asset_revision=(SELECT a.revision FROM creative_assets a WHERE a.account_id=$1 AND a.id=creative_asset_search.asset_id)", "asset_id=ANY($2::text[])", refile)
	return err
}
