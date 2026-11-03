package creativescene

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/samson/customer-manage-platform/backend/internal/creativecontent"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

const (
	imageKind = "image"
	// maxSceneImages counts distinct images across every slot of a scene
	// (C5c1 task 1.1 keeps 64 within the per-device texture budget).
	maxSceneImages = 64
)

// ErrImageUnavailable is a referenced image revision that does not exist, is
// not an image, or cannot be displayed (400 creative_image_unavailable). It
// is never a document-level 404: the editor reads that as the scene gone.
var ErrImageUnavailable = errors.New("creative scene: image unavailable")

// imageUse is one texture pointing at an image, with the field it sits in.
type imageUse struct {
	op    int // the operation of the batch, or -1 for a path in the scene
	path  string
	image string
}

// slotImages lists the images of slots in slot then kind order; path is
// where the slots sit.
func slotImages(slots map[string]MaterialSlot, op int, path string) []imageUse {
	var out []imageUse
	for _, name := range slices.Sorted(maps.Keys(slots)) {
		for _, e := range slots[name].Maps.entries() {
			out = append(out, imageUse{op: op, path: fmt.Sprintf("%s.%s.maps.%s.content_revision_id", path, name, e.kind), image: e.tex.ContentRevisionID})
		}
	}
	return out
}

// imageRefs are the distinct images a scene's slots reference, sorted. With
// the models they are the snapshot's references.
func imageRefs(objects []Object) []string {
	seen := map[string]bool{}
	for _, o := range objects {
		for _, u := range slotImages(o.MaterialSlots, -1, "") {
			seen[u.image] = true
		}
	}
	return slices.Sorted(maps.Keys(seen))
}

// opImageUses are the textures the operations of a batch write, in request
// order. Only add_objects and update_objects write slots.
func opImageUses(ops []sceneOp) []imageUse {
	var out []imageUse
	for i, op := range ops {
		switch o := op.(type) {
		case *addObjectsOp:
			for j, obj := range o.Objects {
				out = append(out, slotImages(obj.MaterialSlots, i, fmt.Sprintf("objects[%d].material_slots", j))...)
			}
		case *updateObjectsOp:
			for j, u := range o.Updates {
				if u.MaterialSlots != nil {
					out = append(out, slotImages(*u.MaterialSlots, i, fmt.Sprintf("updates[%d].material_slots", j))...)
				}
			}
		}
	}
	return out
}

// sceneImageUses are the textures of the objects whose content a restore
// brought back, as paths in the scene.
func sceneImageUses(s Scene, changed map[string]bool) []imageUse {
	var out []imageUse
	for i, o := range s.Objects {
		if changed == nil || changed[contentUnit(o.ID)] {
			out = append(out, slotImages(o.MaterialSlots, -1, fmt.Sprintf("objects[%d].material_slots", i))...)
		}
	}
	return out
}

// verifyImages checks every image the uses point at, as verifyExisting does
// models: one held by a reference row (held) is read with the others in one
// batch, a new one is locked one by one in sorted order. The first use, in
// request order, of an image that fails is the field reported.
func verifyImages(ctx context.Context, tx store.TxAccountScope, uses []imageUse, held func(string) bool) error {
	seen := map[string]bool{}
	var kept, fresh []string
	for _, u := range uses {
		if seen[u.image] {
			continue
		}
		seen[u.image] = true
		if held(u.image) {
			kept = append(kept, u.image)
		} else {
			fresh = append(fresh, u.image)
		}
	}
	bad := map[string]bool{}
	if len(kept) > 0 {
		batch, err := creativecontent.ReadDisplayBatchInTx(ctx, tx, kept)
		if err != nil {
			return err
		}
		for _, id := range kept {
			if res := batch[id]; res.Err != nil || res.Revision.Kind != imageKind {
				bad[id] = true
			}
		}
	}
	slices.Sort(fresh)
	for _, id := range fresh {
		ok, err := usableImage(ctx, tx, id)
		if err != nil {
			return err
		}
		if !ok {
			bad[id] = true
		}
	}
	for _, u := range uses {
		if bad[u.image] {
			return &creativeops.FieldError{Err: ErrImageUnavailable, Op: u.op, Path: u.path, ContentRevisionID: u.image, Message: fmt.Sprintf("引用的图片 %s 不可用", u.image)}
		}
	}
	return nil
}

// usableImage reports whether a revision is an image this account can
// display; anything else unusable is not the caller's to know apart.
func usableImage(ctx context.Context, tx store.TxAccountScope, id string) (bool, error) {
	r, err := creativecontent.RequireUsable(ctx, tx, id, "display")
	if errors.Is(err, creativecontent.ErrNotFound) || errors.Is(err, creativecontent.ErrUsageDenied) || errors.Is(err, creativecontent.ErrMissingRoot) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return r.Kind == imageKind, nil
}

// imageView is one entry of a read's images: what the editor needs to size
// and place a texture, never a storage address.
type imageView struct {
	Usable bool    `json:"usable"`
	Width  *int    `json:"width"`
	Height *int    `json:"height"`
	MIME   *string `json:"mime"`
}

// readImages reads the listed revisions in the caller's snapshot with a fixed
// number of queries, however many images there are.
func readImages(ctx context.Context, tx store.ReadTxAccountScope, ids []string) (map[string]imageView, error) {
	out := make(map[string]imageView, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	batch, err := creativecontent.ReadDisplayBatch(ctx, tx, ids)
	if err != nil {
		return nil, err
	}
	for _, id := range ids {
		var v imageView
		if res := batch[id]; res.Err == nil && res.Revision.Kind == imageKind {
			for _, m := range res.Revision.Media {
				if m.Role == "original" && m.Width != nil && m.Height != nil {
					mime := m.Mime
					v = imageView{Usable: true, Width: m.Width, Height: m.Height, MIME: &mime}
					break
				}
			}
		}
		out[id] = v
	}
	return out, nil
}
