package creativelibrary

import (
	"context"
	"encoding/json"
	"math"
	"sort"

	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

type RateItem struct {
	ID               string               `json:"asset_id"`
	ExpectedRevision creativeops.Revision `json:"expected_revision"`
	Rating           *float64             `json:"rating"`
}
type RateInput struct {
	Items []RateItem `json:"items"`
}
type AssetRevision struct {
	ID       string               `json:"id"`
	Revision creativeops.Revision `json:"revision"`
}
type RateResult struct {
	Items           []AssetRevision      `json:"items"`
	LibraryRevision creativeops.Revision `json:"library_revision"`
}

// ratingHalves converts 0.5–5 in half steps to the stored 1–10; anything else is 0.
func ratingHalves(r float64) int16 {
	h := r * 2
	if h < 1 || h > 10 || h != math.Trunc(h) {
		return 0
	}
	return int16(h)
}

// BatchRate writes each item's own rating, so an undo can put back several
// different ratings in one request. The batch is all or nothing.
func BatchRate(ctx context.Context, sc store.AccountScope, c creativeops.Command) (creativeops.Receipt, error) {
	// rating is required: null clears it, a missing field is a client bug.
	var shape struct {
		Items []map[string]json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal(c.Payload, &shape); err != nil {
		return creativeops.Receipt{}, creativeops.ErrValidation
	}
	for _, item := range shape.Items {
		if _, ok := item["rating"]; !ok {
			return creativeops.Receipt{}, creativeops.ErrValidation
		}
	}
	return libraryCommand(ctx, sc, c, "batch_rate", func(v RateInput) error {
		if len(v.Items) < 1 || len(v.Items) > 100 {
			return creativeops.ErrValidation
		}
		seen := map[string]bool{}
		for _, it := range v.Items {
			if it.ID == "" || it.ExpectedRevision < 1 || seen[it.ID] || it.Rating != nil && ratingHalves(*it.Rating) == 0 {
				return creativeops.ErrValidation
			}
			seen[it.ID] = true
		}
		return nil
	}, func(ctx context.Context, tx store.TxAccountScope, v RateInput) (RateResult, string, creativeops.Revision, error) {
		sort.Slice(v.Items, func(i, j int) bool { return v.Items[i].ID < v.Items[j].ID })
		out := RateResult{Items: make([]AssetRevision, 0, len(v.Items))}
		changed := false
		for _, it := range v.Items {
			a, err := lockAsset(ctx, tx, it.ID, it.ExpectedRevision)
			if err != nil {
				return out, "", 0, err
			}
			if a.DeletedAt != nil {
				return out, "", 0, ErrTrashed
			}
			var halves *int16
			if it.Rating != nil {
				h := ratingHalves(*it.Rating)
				halves = &h
			}
			if (a.Rating == nil) != (halves == nil) || a.Rating != nil && ratingHalves(*a.Rating) != *halves {
				if _, err := tx.Update(ctx, "creative_assets", "rating_halves=$2,revision=revision+1,updated_at=clock_timestamp()", "id=$3", halves, a.ID); err != nil {
					return out, "", 0, err
				}
				a.Revision++
				if _, err := tx.Update(ctx, "creative_asset_search", "asset_revision=$2", "asset_id=$3", a.Revision, a.ID); err != nil {
					return out, "", 0, err
				}
				changed = true
			}
			out.Items = append(out.Items, AssetRevision{ID: a.ID, Revision: a.Revision})
		}
		r, err := libraryResult(ctx, tx, out.Items[0].ID, out.Items[0].Revision, changed, false)
		out.LibraryRevision = r.LibraryRevision
		return out, r.ID, r.Revision, err
	})
}
