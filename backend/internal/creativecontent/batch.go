package creativecontent

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/samson/customer-manage-platform/backend/internal/creativereference"
	"github.com/samson/customer-manage-platform/backend/internal/planningmedia"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// DisplayResult preserves per-revision denial semantics in an aggregate read.
// A denied revision never exposes payload or media to its caller.
type DisplayResult struct {
	Revision Revision
	Err      error
}

// ReadDisplayBatch reads a bounded set in the caller's consistent snapshot.
// SQL round trips depend on relation types, not the number of revisions.
func ReadDisplayBatch(ctx context.Context, tx store.ReadTxAccountScope, ids []string) (map[string]DisplayResult, error) {
	return readDisplayBatch(ctx, tx, ids)
}

type batchReader interface {
	contentReader
	Query(context.Context, string, string, string, ...any) (store.Rows, error)
}

func readDisplayBatch(ctx context.Context, tx batchReader, ids []string) (map[string]DisplayResult, error) {
	out := make(map[string]DisplayResult, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	for _, id := range ids {
		out[id] = DisplayResult{Err: ErrNotFound}
	}
	type storedRevision struct {
		revision Revision
		payload  json.RawMessage
		state    string
	}
	revisions := map[string]storedRevision{}
	contentIDs, declarationIDs := []string{}, []string{}
	err := batchRows(ctx, tx, "creative_content_revisions", "id,content_id,sequence,payload,rights_declaration_id,state", "id=ANY($2::text[])", func(row store.Row) error {
		var r storedRevision
		var sequence int64
		if err := row.Scan(&r.revision.ID, &r.revision.ContentID, &sequence, &r.payload, &r.revision.DeclarationID, &r.state); err != nil {
			return err
		}
		r.revision.Sequence = creativeops.Revision(sequence)
		revisions[r.revision.ID] = r
		contentIDs = append(contentIDs, r.revision.ContentID)
		declarationIDs = append(declarationIDs, r.revision.DeclarationID)
		return nil
	}, ids)
	if err != nil {
		return nil, err
	}
	type identity struct {
		kind   string
		origin *string
	}
	contents := map[string]identity{}
	err = batchRows(ctx, tx, "creative_contents", "id,kind,origin_node_id_snapshot", "id=ANY($2::text[])", func(row store.Row) error {
		var id string
		var c identity
		if err := row.Scan(&id, &c.kind, &c.origin); err != nil {
			return err
		}
		contents[id] = c
		return nil
	}, contentIDs)
	if err != nil {
		return nil, err
	}
	declarations := map[string]bool{}
	err = batchRows(ctx, tx, "creative_rights_declarations", "id,source_class,rights_basis", "id=ANY($2::text[])", func(row store.Row) error {
		var id string
		var d planningmedia.RightsDeclarationInput
		if err := row.Scan(&id, &d.SourceClass, &d.RightsBasis); err != nil {
			return err
		}
		declarations[id] = planningmedia.ValidatePurpose(d, planningmedia.PurposeMoodboardDisplay) == nil
		return nil
	}, declarationIDs)
	if err != nil {
		return nil, err
	}
	granted := map[string]bool{}
	err = batchRows(ctx, tx, "creative_content_revisions", "id", `id=ANY($2::text[])
 AND EXISTS (SELECT 1 FROM creative_usage_grants g WHERE g.account_id=$1 AND g.declaration_id=creative_content_revisions.rights_declaration_id AND g.purpose='display' AND g.revoked_at IS NULL)
 AND NOT EXISTS (SELECT 1 FROM creative_content_required_grants r WHERE r.account_id=$1 AND r.content_revision_id=creative_content_revisions.id AND r.declaration_id NOT IN (SELECT declaration_id FROM creative_usage_grants WHERE account_id=$1 AND purpose='display' AND revoked_at IS NULL))`, func(row store.Row) error {
		var id string
		if err := row.Scan(&id); err != nil {
			return err
		}
		granted[id] = true
		return nil
	}, ids)
	if err != nil {
		return nil, err
	}
	live, err := creativereference.Live(ctx, tx, ids)
	if err != nil {
		return nil, err
	}
	mediaIDs := []string{}
	for id, stored := range revisions {
		r := stored.revision
		c, ok := contents[r.ContentID]
		if !ok {
			return nil, fmt.Errorf("content identity: %w", store.ErrNoRows)
		}
		r.Kind, r.OriginNodeID = c.kind, c.origin
		// Keep the single-read ordering: declaration policy, state, grants, owners.
		switch {
		case !declarations[r.DeclarationID]:
			out[id] = DisplayResult{Err: ErrUsageDenied}
			continue
		case stored.state != "ready":
			continue
		case !granted[id]:
			out[id] = DisplayResult{Err: ErrUsageDenied}
			continue
		case !live[id]:
			out[id] = DisplayResult{Err: ErrMissingRoot}
			continue
		}
		if err := creativeops.Decode(stored.payload, &r.Payload); err != nil {
			return nil, err
		}
		out[id] = DisplayResult{Revision: r}
		if IsMediaKind(r.Kind) {
			mediaIDs = append(mediaIDs, id)
		}
	}
	if len(mediaIDs) == 0 {
		return out, nil
	}
	return attachBatchMedia(ctx, tx, out, mediaIDs)
}

func batchRows(ctx context.Context, tx batchReader, table, columns, condition string, scan func(store.Row) error, args ...any) error {
	rows, err := tx.Query(ctx, table, columns, condition, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		if err := scan(rows); err != nil {
			return err
		}
	}
	return rows.Err()
}

func attachBatchMedia(ctx context.Context, tx batchReader, out map[string]DisplayResult, ids []string) (map[string]DisplayResult, error) {
	type binding struct {
		revision, role, blob string
		position             int
	}
	bindings := []binding{}
	blobIDs := []string{}
	// Order each revision's renditions as in loadMedia. Each revision exposes at
	// most twenty objects, preserving the existing single-read contract.
	rows, err := tx.Query(ctx, "creative_content_objects", "content_revision_id,role,blob_id,position", "content_revision_id=ANY($2::text[])", ids)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var b binding
		if err := rows.Scan(&b.revision, &b.role, &b.blob, &b.position); err != nil {
			rows.Close()
			return nil, err
		}
		bindings = append(bindings, b)
		blobIDs = append(blobIDs, b.blob)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	blobs := map[string]MediaObject{}
	err = batchRows(ctx, tx, "creative_blobs", "id,mime,byte_size,width,height,duration_ms", "id=ANY($2::text[]) AND state='ready'", func(row store.Row) error {
		var m MediaObject
		if err := row.Scan(&m.BlobID, &m.Mime, &m.ByteSize, &m.Width, &m.Height, &m.DurationMs); err != nil {
			return err
		}
		blobs[m.BlobID] = m
		return nil
	}, blobIDs)
	if err != nil {
		return nil, err
	}
	sort.Slice(bindings, func(i, j int) bool {
		if bindings[i].role != bindings[j].role {
			return bindings[i].role < bindings[j].role
		}
		return bindings[i].position < bindings[j].position
	})
	counts := map[string]int{}
	for _, b := range bindings {
		if counts[b.revision] >= 20 {
			continue
		}
		counts[b.revision]++
		result := out[b.revision]
		if result.Err != nil {
			continue
		}
		m, ok := blobs[b.blob]
		if !ok {
			out[b.revision] = DisplayResult{Err: ErrNotFound}
			continue
		}
		m.Role = b.role
		result.Revision.Media = append(result.Revision.Media, m)
		out[b.revision] = result
	}
	for _, id := range ids {
		if len(out[id].Revision.Media) == 0 {
			out[id] = DisplayResult{Err: ErrNotFound}
		}
	}
	return out, nil
}
