// Package creativestaging owns staged uploads: verified media published
// without a target, waiting for an explicit user edit to place it. The media
// worker writes them; the canvas consumes one inside a batch command, so the
// canvas is only ever changed by the user's own ordered edits.
package creativestaging

import (
	"context"
	"errors"
	"path"
	"strings"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// Reason marks a candidate row as a staged upload rather than a binding that
// needs review; staged rows never appear in the review list.
const Reason = "staged"

// ErrUnavailable reports a staged upload that is missing, expired, already
// placed or discarded. The edit that names it never commits.
var ErrUnavailable = errors.New("creative staged upload unavailable")

// Staged is a usable staged upload locked for placement.
type Staged struct {
	ID                string
	UploadID          string
	Kind              string
	ContentRevisionID string
	FileName          string
}

// SceneExportTarget scopes a staged original to the scene that rendered it.
type SceneExportTarget struct {
	CanvasID    string `json:"canvas_id"`
	SceneNodeID string `json:"scene_node_id"`
}

// Lock takes the staged row of an upload for update. Only a pending,
// unexpired staged row without a scene export target qualifies.
func Lock(ctx context.Context, tx store.TxAccountScope, uploadID string) (Staged, error) {
	return lock(ctx, tx, "target_snapshot->'scene_export' IS NULL", uploadID, Reason)
}

// LockSceneExport also accepts unscoped uploads from older scene clients.
func LockSceneExport(ctx context.Context, tx store.TxAccountScope, uploadID string, target SceneExportTarget) (Staged, error) {
	return lock(ctx, tx, "COALESCE(target_snapshot->'scene_export' = jsonb_build_object('canvas_id',$4::text,'scene_node_id',$5::text), true)", uploadID, Reason, target.CanvasID, target.SceneNodeID)
}

func lock(ctx context.Context, tx store.TxAccountScope, targetFilter string, args ...any) (Staged, error) {
	var s Staged
	var revision *string
	err := tx.QueryRowForUpdate(ctx, "creative_upload_candidates", "id,upload_id,declared_kind,content_revision_id,original_name", "upload_id=$2 AND reason=$3 AND state='pending' AND expires_at>clock_timestamp() AND "+targetFilter, args...).Scan(&s.ID, &s.UploadID, &s.Kind, &revision, &s.FileName)
	if errors.Is(err, store.ErrNoRows) || err == nil && revision == nil {
		return Staged{}, ErrUnavailable
	}
	if err != nil {
		return Staged{}, err
	}
	s.ContentRevisionID = *revision
	return s, nil
}

// Place marks a locked staged row as placed on a node by the given change.
// The revision is now rooted by the node and the change history.
func Place(ctx context.Context, tx store.TxAccountScope, s Staged, changeID, nodeID string, nodeRevision int64) error {
	_, err := tx.Update(ctx, "creative_upload_candidates", "state='applied',content_id=NULL,content_revision_id=NULL,adopted_change_id=$2,adopted_target_kind='node',adopted_target_id=$3,adopted_target_revision=$4,revision=revision+1,updated_at=clock_timestamp()", "id=$5", changeID, nodeID, nodeRevision, s.ID)
	return err
}

// Consume marks a locked staged row as used by a document command. The
// staged revision loses its holder (GC reclaims it); the document's new
// revision holds what it took from it.
func Consume(ctx context.Context, tx store.TxAccountScope, s Staged, documentID string, documentRevision int64) error {
	_, err := tx.Update(ctx, "creative_upload_candidates", "state='applied',content_id=NULL,content_revision_id=NULL,adopted_target_kind='document',adopted_target_id=$2,adopted_target_revision=$3,revision=revision+1,updated_at=clock_timestamp()", "id=$4", documentID, documentRevision, s.ID)
	return err
}

// ConsumeForAsset marks a locked staged row as used by a new library asset,
// whose content revision now holds what it took from it.
func ConsumeForAsset(ctx context.Context, tx store.TxAccountScope, s Staged, assetID string) error {
	_, err := tx.Update(ctx, "creative_upload_candidates", "state='applied',content_id=NULL,content_revision_id=NULL,adopted_target_kind='asset',adopted_target_id=$2,adopted_target_revision=1,revision=revision+1,updated_at=clock_timestamp()", "id=$3", assetID, s.ID)
	return err
}

// Title is the default node title for an uploaded file: the name without its
// last extension, at most 200 characters.
func Title(name string) string {
	title := strings.TrimSuffix(name, path.Ext(name))
	if strings.TrimSpace(title) == "" {
		title = name
	}
	if r := []rune(title); len(r) > 200 {
		title = string(r[:200])
	}
	return title
}
