package creativelibrary

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/samson/customer-manage-platform/backend/internal/creativepalette"
)

// Appearance fields are content-derived: every consumer uses the same read
// authorization predicate, including both facet queries and the total count.
func appearanceCondition(q Search, args *sqlArgs) string {
	if q.Shape == "" && q.Color == "" {
		return ""
	}
	cond := ` AND ` + readableContent("creative_assets.content_revision_id", args)
	blob := `o.account_id=$1 AND o.content_revision_id=creative_assets.content_revision_id AND o.role='original' AND b.state='ready'`
	if q.Shape != "" {
		dimensions := `b.width>0 AND b.height>0`
		switch q.Shape {
		case "square":
			dimensions += ` AND GREATEST(b.width,b.height)::bigint*10<=LEAST(b.width,b.height)::bigint*11`
		case "landscape":
			dimensions += ` AND b.width::bigint*10>b.height::bigint*11`
		case "portrait":
			dimensions += ` AND b.height::bigint*10>b.width::bigint*11`
		}
		// Video dimensions are encoded dimensions until orientation backfill is available.
		cond += ` AND kind='image' AND EXISTS(SELECT 1 FROM creative_content_objects o JOIN creative_blobs b ON b.account_id=o.account_id AND b.id=o.blob_id WHERE ` + blob + ` AND ` + dimensions + `)`
	}
	// The matching rules live only here. TestPalettePhotoMatchingUsesProductionSQL
	// and TestPaletteSQLThresholdBoundaries exercise the actual page/count/facet SQL.
	if q.Color != "" {
		c, _ := creativepalette.ParseHex(q.Color) // normalized validated the literal.
		match := `(c->>'weight')::float8>=` + args.bind(creativepalette.MinWeight)
		if c.C < creativepalette.NeutralChroma {
			match += ` AND (c->>'c')::float8<` + args.bind(creativepalette.NeutralChroma) + ` AND abs((c->>'l')::float8-` + args.bind(c.L) + `::float8)<=` + args.bind(creativepalette.NeutralLightness)
		} else {
			hue := `abs((c->>'h')::float8-` + args.bind(c.H) + `::float8)`
			match += ` AND (c->>'c')::float8>=` + args.bind(creativepalette.NeutralChroma) + ` AND LEAST(` + hue + `,360-` + hue + `)<=` + args.bind(creativepalette.HueDistance) + ` AND abs((c->>'l')::float8-` + args.bind(c.L) + `::float8)<=` + args.bind(creativepalette.ColorLightness)
		}
		cond += ` AND kind='image' AND EXISTS(SELECT 1 FROM creative_content_objects o JOIN creative_blobs b ON b.account_id=o.account_id AND b.id=o.blob_id JOIN creative_blob_palettes p ON p.account_id=b.account_id AND p.blob_id=b.id CROSS JOIN LATERAL jsonb_array_elements(p.colors) c WHERE ` + blob + ` AND p.algorithm=` + args.bind(creativepalette.Algorithm) + ` AND p.state='ready' AND ` + match + `)`
	}
	return cond
}

type PaletteColor struct {
	Hex    string  `json:"hex"`
	Weight float64 `json:"weight"`
}

// attachPalettes consumes only already-authorized original blobs. A single
// query serves the page; unreadable content never contributes an ID or color.
func attachPalettes(ctx context.Context, tx libraryReader, assets []Asset) error {
	ids := []string{}
	for i := range assets {
		a := &assets[i]
		a.Palette = []PaletteColor{}
		if a.Kind != "image" || a.Content == nil || a.Unavailable {
			continue
		}
		for _, m := range a.Content.Media {
			if m.Role == "original" {
				ids = append(ids, m.BlobID)
			}
		}
	}
	if len(ids) == 0 {
		return nil
	}
	rows, err := tx.Query(ctx, "creative_blob_palettes", "blob_id,colors", "blob_id=ANY($2::text[]) AND algorithm=$3 AND state='ready'", ids, creativepalette.Algorithm)
	if err != nil {
		return err
	}
	defer rows.Close()
	byBlob := map[string][]PaletteColor{}
	for rows.Next() {
		var id string
		var raw []byte
		if err = rows.Scan(&id, &raw); err != nil {
			return err
		}
		var colors []PaletteColor
		if err = json.Unmarshal(raw, &colors); err != nil {
			return err
		}
		byBlob[id] = colors
	}
	if err = rows.Err(); err != nil {
		return err
	}
	for i := range assets {
		a := &assets[i]
		if a.Kind != "image" || a.Content == nil || a.Unavailable {
			continue
		}
		for _, m := range a.Content.Media {
			if m.Role == "original" {
				if colors := byBlob[m.BlobID]; len(colors) > 0 {
					a.Palette = colors
				}
			}
		}
	}
	return nil
}
func normalizedColor(s string) (string, error) {
	if s == "" {
		return "", nil
	}
	_, err := creativepalette.ParseHex(s)
	return strings.ToLower(s), err
}
