package creativelibrary

import (
	"context"
	"errors"
	"regexp"

	"github.com/google/uuid"
	"github.com/samson/customer-manage-platform/backend/internal/creativelibrary/textindex"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

var colorPattern = regexp.MustCompile(`^#[a-fA-F0-9]{6}$`)

type Tag struct {
	ID         string               `json:"id"`
	Name       string               `json:"name"`
	Color      string               `json:"color"`
	CategoryID *string              `json:"category_id"`
	Revision   creativeops.Revision `json:"revision"`
}
type TagCategory struct {
	ID       string               `json:"id"`
	Name     string               `json:"name"`
	Position int                  `json:"position"`
	Revision creativeops.Revision `json:"revision"`
}
type TagPage struct {
	Items             []Tag                `json:"items"`
	HierarchyRevision creativeops.Revision `json:"hierarchy_revision"`
}
type CategoryPage struct {
	Items             []TagCategory        `json:"items"`
	HierarchyRevision creativeops.Revision `json:"hierarchy_revision"`
}
type TagInput struct {
	ID                string               `json:"tag_id"`
	Name              string               `json:"name"`
	Color             string               `json:"color"`
	CategoryID        *string              `json:"category_id"`
	ExpectedRevision  creativeops.Revision `json:"expected_revision,omitempty"`
	HierarchyRevision creativeops.Revision `json:"hierarchy_revision"`
}
type CategoryInput struct {
	ID                string               `json:"category_id"`
	Name              string               `json:"name"`
	ExpectedRevision  creativeops.Revision `json:"expected_revision,omitempty"`
	HierarchyRevision creativeops.Revision `json:"hierarchy_revision"`
}
type NewTag struct {
	ClientKey  string  `json:"client_tag_key"`
	Name       string  `json:"name"`
	Color      string  `json:"color"`
	CategoryID *string `json:"category_id,omitempty"`
}

func scanTag(row store.Row) (Tag, error) {
	var t Tag
	err := row.Scan(&t.ID, &t.Name, &t.Color, &t.CategoryID, &t.Revision)
	if errors.Is(err, store.ErrNoRows) {
		err = ErrNotFound
	}
	return t, err
}
func requireEntity(ctx context.Context, tx libraryReader, table, id string) error {
	found, err := tx.Exists(ctx, table, "id=$2", id)
	if err != nil {
		return err
	}
	if !found {
		return ErrNotFound
	}
	return nil
}
func ListTags(ctx context.Context, sc store.AccountScope) (TagPage, error) {
	p := TagPage{Items: []Tag{}}
	err := sc.WithReadSnapshot(ctx, func(tx store.ReadTxAccountScope) error {
		if err := tx.RequireCreativeRead(ctx); err != nil {
			return err
		}
		s, err := readSettings(ctx, tx)
		if err != nil {
			return err
		}
		p.HierarchyRevision = s.HierarchyRevision
		rows, err := tx.QueryPage(ctx, "creative_tags", "id,name,color,category_id,revision", "", []store.OrderBy{{Column: "normalized_name"}, {Column: "id"}}, 10001, 0)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			t, err := scanTag(rows)
			if err != nil {
				return err
			}
			p.Items = append(p.Items, t)
		}
		return rows.Err()
	})
	return p, err
}
func ListCategories(ctx context.Context, sc store.AccountScope) (CategoryPage, error) {
	p := CategoryPage{Items: []TagCategory{}}
	err := sc.WithReadSnapshot(ctx, func(tx store.ReadTxAccountScope) error {
		if err := tx.RequireCreativeRead(ctx); err != nil {
			return err
		}
		s, err := readSettings(ctx, tx)
		if err != nil {
			return err
		}
		p.HierarchyRevision = s.HierarchyRevision
		rows, err := tx.QueryPage(ctx, "creative_tag_categories", "id,name,position,revision", "", []store.OrderBy{{Column: "position"}, {Column: "id"}}, 10001, 0)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var c TagCategory
			if err = rows.Scan(&c.ID, &c.Name, &c.Position, &c.Revision); err != nil {
				return err
			}
			p.Items = append(p.Items, c)
		}
		return rows.Err()
	})
	return p, err
}
func createTag(ctx context.Context, tx store.TxAccountScope, v NewTag) (Tag, bool, error) {
	if !validName(v.Name) || !validName(textindex.Normalize(v.Name)) || !colorPattern.MatchString(v.Color) {
		return Tag{}, false, creativeops.ErrValidation
	}
	if v.CategoryID != nil {
		if err := requireEntity(ctx, tx, "creative_tag_categories", *v.CategoryID); err != nil {
			return Tag{}, false, err
		}
	}
	t, err := scanTag(tx.QueryRow(ctx, "creative_tags", "id,name,color,category_id,revision", "normalized_name=$2", textindex.Normalize(v.Name)))
	if err == nil {
		return t, false, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return Tag{}, false, err
	}
	count, err := tx.Count(ctx, "creative_tags", "")
	if err != nil {
		return Tag{}, false, err
	}
	if count >= 10000 {
		return Tag{}, false, creativeops.ErrValidation
	}
	t = Tag{ID: "cctg_" + uuid.NewString(), Name: v.Name, Color: v.Color, CategoryID: v.CategoryID, Revision: 1}
	err = tx.Insert(ctx, "creative_tags", []string{"id", "name", "normalized_name", "color", "category_id"}, t.ID, t.Name, textindex.Normalize(t.Name), t.Color, t.CategoryID)
	return t, true, err
}
func tagCommand(ctx context.Context, s store.AccountScope, c creativeops.Command, action string) (creativeops.Receipt, error) {
	return libraryRun(ctx, s, c, "tag_"+action, func(v TagInput) error {
		if v.HierarchyRevision < 1 || (action != "create" && (v.ID == "" || v.ExpectedRevision < 1)) || (action != "delete" && (!validName(v.Name) || !colorPattern.MatchString(v.Color))) {
			return creativeops.ErrValidation
		}
		return nil
	}, func(ctx context.Context, tx store.TxAccountScope, v TagInput) (LibraryResult, error) {
		var t Tag
		var err error
		if action != "create" {
			t, err = scanTag(tx.QueryRow(ctx, "creative_tags", "id,name,color,category_id,revision", "id=$2", v.ID))
			if err != nil {
				return LibraryResult{}, err
			}
			if t.Revision != v.ExpectedRevision {
				return LibraryResult{}, ErrVersionConflict
			}
		}
		if err = hierarchy(ctx, tx, v.HierarchyRevision); err != nil {
			return LibraryResult{}, err
		}
		changed := true
		switch action {
		case "create":
			t, changed, err = createTag(ctx, tx, NewTag{Name: v.Name, Color: v.Color, CategoryID: v.CategoryID})
		case "edit":
			if v.CategoryID != nil {
				if err = requireEntity(ctx, tx, "creative_tag_categories", *v.CategoryID); err != nil {
					return LibraryResult{}, err
				}
			}
			if !validName(textindex.Normalize(v.Name)) {
				return LibraryResult{}, creativeops.ErrValidation
			}
			duplicate, e := tx.Exists(ctx, "creative_tags", "normalized_name=$2 AND id<>$3", textindex.Normalize(v.Name), v.ID)
			if e != nil {
				return LibraryResult{}, e
			}
			if duplicate {
				return LibraryResult{}, ErrVersionConflict
			}
			changed = t.Name != v.Name || t.Color != v.Color || parentKey(t.CategoryID) != parentKey(v.CategoryID)
			if changed {
				_, err = tx.Update(ctx, "creative_tags", "name=$2,normalized_name=$3,color=$4,category_id=$5,revision=revision+1", "id=$6", v.Name, textindex.Normalize(v.Name), v.Color, v.CategoryID, v.ID)
				t.Revision++
			}
		case "delete":
			if err = removeRelations(ctx, tx, "creative_asset_tags", "tag_id", v.ID); err == nil {
				_, err = tx.Delete(ctx, "creative_tags", "id=$2", v.ID)
			}
		}
		if err != nil {
			return LibraryResult{}, err
		}
		return libraryResult(ctx, tx, t.ID, t.Revision, changed, true)
	})
}
func CreateTag(ctx context.Context, s store.AccountScope, c creativeops.Command) (creativeops.Receipt, error) {
	return tagCommand(ctx, s, c, "create")
}
func EditTag(ctx context.Context, s store.AccountScope, c creativeops.Command) (creativeops.Receipt, error) {
	return tagCommand(ctx, s, c, "edit")
}
func DeleteTag(ctx context.Context, s store.AccountScope, c creativeops.Command) (creativeops.Receipt, error) {
	return tagCommand(ctx, s, c, "delete")
}
func categoryCommand(ctx context.Context, s store.AccountScope, c creativeops.Command, action string) (creativeops.Receipt, error) {
	return libraryRun(ctx, s, c, "category_"+action, func(v CategoryInput) error {
		if v.HierarchyRevision < 1 || (action != "create" && (v.ID == "" || v.ExpectedRevision < 1)) || (action != "delete" && !validName(v.Name)) {
			return creativeops.ErrValidation
		}
		return nil
	}, func(ctx context.Context, tx store.TxAccountScope, v CategoryInput) (LibraryResult, error) {
		var old TagCategory
		var err error
		if action != "create" {
			err = tx.QueryRow(ctx, "creative_tag_categories", "id,name,position,revision", "id=$2", v.ID).Scan(&old.ID, &old.Name, &old.Position, &old.Revision)
			if errors.Is(err, store.ErrNoRows) {
				return LibraryResult{}, ErrNotFound
			}
			if err != nil {
				return LibraryResult{}, err
			}
			if old.Revision != v.ExpectedRevision {
				return LibraryResult{}, ErrVersionConflict
			}
		}
		if err = hierarchy(ctx, tx, v.HierarchyRevision); err != nil {
			return LibraryResult{}, err
		}
		changed := true
		switch action {
		case "create":
			count, e := tx.Count(ctx, "creative_tag_categories", "")
			if e != nil {
				return LibraryResult{}, e
			}
			if count >= 10000 {
				return LibraryResult{}, creativeops.ErrValidation
			}
			old = TagCategory{ID: "cctc_" + uuid.NewString(), Name: v.Name, Position: int(count), Revision: 1}
			err = tx.Insert(ctx, "creative_tag_categories", []string{"id", "name", "position"}, old.ID, old.Name, old.Position)
		case "edit":
			changed = old.Name != v.Name
			if changed {
				_, err = tx.Update(ctx, "creative_tag_categories", "name=$2,revision=revision+1", "id=$3", v.Name, v.ID)
				old.Revision++
			}
		case "delete":
			if _, err = tx.Update(ctx, "creative_tags", "category_id=NULL,revision=revision+1", "category_id=$2", v.ID); err == nil {
				_, err = tx.Delete(ctx, "creative_tag_categories", "id=$2", v.ID)
			}
		}
		if err != nil {
			return LibraryResult{}, err
		}
		return libraryResult(ctx, tx, old.ID, old.Revision, changed, true)
	})
}
func CreateCategory(ctx context.Context, s store.AccountScope, c creativeops.Command) (creativeops.Receipt, error) {
	return categoryCommand(ctx, s, c, "create")
}
func EditCategory(ctx context.Context, s store.AccountScope, c creativeops.Command) (creativeops.Receipt, error) {
	return categoryCommand(ctx, s, c, "edit")
}
func DeleteCategory(ctx context.Context, s store.AccountScope, c creativeops.Command) (creativeops.Receipt, error) {
	return categoryCommand(ctx, s, c, "delete")
}
