// Package creativecontent owns immutable content and current usage guards.
package creativecontent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/samson/customer-manage-platform/backend/internal/planningmedia"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

var (
	ErrNotFound    = errors.New("creative content not found")
	ErrUsageDenied = errors.New("creative content usage denied")
	ErrMissingRoot = errors.New("creative content has no owning reference")
)

type Payload struct {
	Body        *string `json:"body,omitempty"`
	URL         *string `json:"url,omitempty"`
	Title       *string `json:"title,omitempty"`
	Description *string `json:"description,omitempty"`
	Caption     *string `json:"caption,omitempty"`
}

// MediaObject is an authorized description of one stored rendition. It never
// carries storage keys or versions; bytes are read through access tickets.
type MediaObject struct {
	Role       string `json:"role"`
	BlobID     string `json:"blob_id"`
	Mime       string `json:"mime"`
	ByteSize   int64  `json:"byte_size"`
	Width      *int   `json:"width"`
	Height     *int   `json:"height"`
	DurationMs *int64 `json:"duration_ms"`
}

// ObjectBinding is the trusted server-side input that attaches verified blobs
// to a new media revision. Clients never supply it.
type ObjectBinding struct {
	Role     string
	BlobID   string
	Position int
}

// IsMediaKind reports whether a content kind stores its payload as blobs.
func IsMediaKind(kind string) bool { return kind == "image" || kind == "video" || kind == "audio" }

// RightsDeclarationInput exposes only the current display declaration fields.
// Generation consent is intentionally absent from this namespace.
type RightsDeclarationInput struct {
	SourceClass     planningmedia.SourceClass `json:"source_class"`
	RightsBasis     planningmedia.RightsBasis `json:"rights_basis"`
	EvidenceSummary string                    `json:"evidence_summary,omitempty"`
}

// OrDefault fills an omitted declaration with the photographer's own work.
// Clients no longer ask for a source; the field stays reserved for licensed
// or cited material.
func (r RightsDeclarationInput) OrDefault() RightsDeclarationInput {
	if r.SourceClass == "" && r.RightsBasis == "" {
		r.SourceClass = planningmedia.SourcePhotographerOwned
		r.RightsBasis = planningmedia.RightsOwnershipAttested
	}
	return r
}

type Draft struct {
	Rights  RightsDeclarationInput `json:"rights"`
	Kind    string                 `json:"kind"`
	Payload Payload                `json:"payload"`
	// DeclarationID reuses a declaration persisted earlier by a trusted flow
	// (media upload sessions). It is never accepted from clients.
	DeclarationID string `json:"-"`
}
type Revision struct {
	Truncated     bool                 `json:"truncated"`
	ID            string               `json:"id"`
	ContentID     string               `json:"content_id"`
	Kind          string               `json:"kind"`
	Sequence      creativeops.Revision `json:"sequence"`
	Payload       Payload              `json:"payload"`
	Media         []MediaObject        `json:"media,omitempty"`
	DeclarationID string               `json:"-"`
	OriginNodeID  *string              `json:"-"`
}

func Validate(d Draft) error {
	switch d.Kind {
	case "text":
		if d.Payload.Body == nil || strings.TrimSpace(*d.Payload.Body) == "" || utf8.RuneCountInString(*d.Payload.Body) > 100000 || d.Payload.URL != nil || d.Payload.Title != nil || d.Payload.Description != nil || d.Payload.Caption != nil {
			return creativeops.ErrValidation
		}
	case "image", "video", "audio":
		if d.Payload.Body != nil || d.Payload.URL != nil || d.Payload.Title != nil || d.Payload.Description != nil || d.Payload.Caption != nil && utf8.RuneCountInString(*d.Payload.Caption) > 2000 {
			return creativeops.ErrValidation
		}
	case "link":
		if d.Payload.URL == nil || utf8.RuneCountInString(*d.Payload.URL) > 4096 || d.Payload.Body != nil || d.Payload.Caption != nil {
			return creativeops.ErrValidation
		}
		u, err := url.Parse(*d.Payload.URL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil {
			return creativeops.ErrValidation
		}
		if d.Payload.Title != nil && utf8.RuneCountInString(*d.Payload.Title) > 200 || d.Payload.Description != nil && utf8.RuneCountInString(*d.Payload.Description) > 2000 {
			return creativeops.ErrValidation
		}
	default:
		return creativeops.ErrValidation
	}
	return nil
}

func newID(prefix string) string { return prefix + "_" + uuid.NewString() }

// WriteAndRetain creates/forks/appends and requires a real owning root before
// returning. Its caller owns the surrounding creativeops transaction. Media
// kinds must derive from a source revision so the verified objects carry over;
// first media revisions come from WriteMediaAndRetain.
func WriteAndRetain(ctx context.Context, tx store.TxAccountScope, draft Draft, sourceID string, originNodeID *string, retain func(Revision) error) (Revision, error) {
	if IsMediaKind(draft.Kind) && sourceID == "" {
		return Revision{}, creativeops.ErrValidation
	}
	return write(ctx, tx, draft, nil, sourceID, originNodeID, retain)
}

// WriteMediaAndRetain creates the first revision of verified media. Objects are
// trusted bindings established by the media publisher inside the same transaction.
func WriteMediaAndRetain(ctx context.Context, tx store.TxAccountScope, draft Draft, objects []ObjectBinding, retain func(Revision) error) (Revision, error) {
	if !IsMediaKind(draft.Kind) || len(objects) == 0 {
		return Revision{}, creativeops.ErrValidation
	}
	return write(ctx, tx, draft, objects, "", nil, retain)
}

func write(ctx context.Context, tx store.TxAccountScope, draft Draft, objects []ObjectBinding, sourceID string, originNodeID *string, retain func(Revision) error) (Revision, error) {
	if err := Validate(draft); err != nil {
		return Revision{}, err
	}
	if retain == nil {
		return Revision{}, ErrMissingRoot
	}
	result := Revision{ID: newID("ccrv"), ContentID: newID("ccnt"), Kind: draft.Kind, Sequence: 1, Payload: draft.Payload, OriginNodeID: originNodeID}
	var source Revision
	if sourceID != "" {
		var err error
		source, err = RequireUsable(ctx, tx, sourceID, "display")
		if err != nil {
			return Revision{}, err
		}
		if source.Kind != draft.Kind {
			return Revision{}, creativeops.ErrValidation
		}
		result.DeclarationID = source.DeclarationID
		if originNodeID != nil && source.OriginNodeID != nil && *originNodeID == *source.OriginNodeID {
			result.ContentID = source.ContentID
			var sequence int64
			if err := tx.QueryRowForUpdate(ctx, "creative_contents", "last_sequence", "id=$2", source.ContentID).Scan(&sequence); err != nil {
				return Revision{}, err
			}
			if sequence == int64(^uint64(0)>>1) {
				return Revision{}, creativeops.ErrValidation
			}
			result.Sequence = creativeops.Revision(sequence + 1)
			if _, err := tx.Update(ctx, "creative_contents", "last_sequence=$2", "id=$3", int64(result.Sequence), result.ContentID); err != nil {
				return Revision{}, err
			}
		}
	}
	if result.Sequence == 1 {
		if err := tx.Insert(ctx, "creative_contents", []string{"id", "kind", "created_by_kind", "origin_node_id_snapshot", "last_sequence"}, result.ContentID, result.Kind, "photographer", originNodeID, int64(result.Sequence)); err != nil {
			return Revision{}, err
		}
	}
	if result.DeclarationID == "" && draft.DeclarationID != "" {
		var source planningmedia.SourceClass
		var basis planningmedia.RightsBasis
		if err := tx.QueryRowForUpdate(ctx, "creative_rights_declarations", "source_class, rights_basis", "id=$2", draft.DeclarationID).Scan(&source, &basis); err != nil {
			if errors.Is(err, store.ErrNoRows) {
				return Revision{}, ErrUsageDenied
			}
			return Revision{}, err
		}
		granted, err := tx.Exists(ctx, "creative_usage_grants", "declaration_id=$2 AND purpose='display' AND revoked_at IS NULL", draft.DeclarationID)
		if err != nil {
			return Revision{}, err
		}
		if !granted || planningmedia.ValidatePurpose(planningmedia.RightsDeclarationInput{SourceClass: source, RightsBasis: basis}, planningmedia.PurposeMoodboardDisplay) != nil {
			return Revision{}, ErrUsageDenied
		}
		result.DeclarationID = draft.DeclarationID
	}
	if result.DeclarationID == "" {
		draft.Rights = draft.Rights.OrDefault()
		if utf8.RuneCountInString(draft.Rights.EvidenceSummary) > 500 {
			return Revision{}, creativeops.ErrValidation
		}
		declaration := planningmedia.RightsDeclarationInput{SourceClass: draft.Rights.SourceClass, RightsBasis: draft.Rights.RightsBasis, EvidenceSummary: draft.Rights.EvidenceSummary}
		if err := planningmedia.ValidatePurpose(declaration, planningmedia.PurposeMoodboardDisplay); err != nil {
			return Revision{}, ErrUsageDenied
		}
		result.DeclarationID = newID("ccrd")
		if err := tx.Insert(ctx, "creative_rights_declarations", []string{"id", "source_class", "rights_basis", "source_url", "evidence_summary"}, result.DeclarationID, draft.Rights.SourceClass, draft.Rights.RightsBasis, draft.Payload.URL, draft.Rights.EvidenceSummary); err != nil {
			return Revision{}, err
		}
		if err := tx.Insert(ctx, "creative_usage_grants", []string{"id", "declaration_id", "purpose", "evidence"}, newID("ccug"), result.DeclarationID, "display", json.RawMessage(`{"source":"manual_input"}`)); err != nil {
			return Revision{}, err
		}
	}
	payload, err := json.Marshal(draft.Payload)
	if err != nil {
		return Revision{}, err
	}
	provenance := json.RawMessage(`{}`)
	if sourceID != "" {
		var err error
		provenance, err = json.Marshal(map[string]string{"source_revision_id_snapshot": source.ID, "source_content_id_snapshot": source.ContentID})
		if err != nil {
			return Revision{}, err
		}
	}
	if err := tx.Insert(ctx, "creative_content_revisions", []string{"id", "content_id", "sequence", "schema_version", "payload", "rights_declaration_id", "provenance_snapshot"}, result.ID, result.ContentID, int64(result.Sequence), 1, payload, result.DeclarationID, provenance); err != nil {
		return Revision{}, err
	}
	if IsMediaKind(result.Kind) {
		if sourceID != "" {
			// Derived media keeps the exact verified objects of its source.
			objects = nil
			for _, m := range source.Media {
				objects = append(objects, ObjectBinding{Role: m.Role, BlobID: m.BlobID, Position: 0})
			}
		}
		for _, o := range objects {
			if err := requireReadyBlob(ctx, tx, o.BlobID); err != nil {
				return Revision{}, err
			}
			if err := tx.Insert(ctx, "creative_content_objects", []string{"content_revision_id", "blob_id", "role", "position"}, result.ID, o.BlobID, o.Role, o.Position); err != nil {
				return Revision{}, err
			}
		}
		media, err := loadMedia(ctx, tx, result.ID)
		if err != nil {
			return Revision{}, err
		}
		result.Media = media
	}
	if sourceID != "" {
		if err := RetainDerivativeRights(ctx, tx, result.ID, []Revision{source}); err != nil {
			return Revision{}, err
		}
	}
	if err := retain(result); err != nil {
		return Revision{}, err
	}
	if err := requireRoot(ctx, tx, result); err != nil {
		return Revision{}, err
	}
	return result, nil
}

// RequireUsable protects the selected revision. Future GC must take the same
// revision lock and explicitly check every registered root, never a source ID.
type contentReader interface {
	QueryRow(context.Context, string, string, string, ...any) store.Row
	QueryPage(context.Context, string, string, string, []store.OrderBy, int, int, ...any) (store.Rows, error)
	Exists(context.Context, string, string, ...any) (bool, error)
}

// requireReadyBlob locks a blob after the revision, matching the shared order.
func requireReadyBlob(ctx context.Context, tx store.TxAccountScope, blobID string) error {
	var state string
	err := tx.QueryRowForUpdate(ctx, "creative_blobs", "state", "id=$2", blobID).Scan(&state)
	if errors.Is(err, store.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if state != "ready" {
		return ErrNotFound
	}
	return nil
}

// loadMedia projects the verified objects of one revision without storage keys.
func loadMedia(ctx context.Context, tx contentReader, revisionID string) ([]MediaObject, error) {
	rows, err := tx.QueryPage(ctx, "creative_content_objects", "role,blob_id,position", "content_revision_id=$2", []store.OrderBy{{Column: "role"}, {Column: "position"}}, 20, 0, revisionID)
	if err != nil {
		return nil, err
	}
	var bindings []ObjectBinding
	for rows.Next() {
		var b ObjectBinding
		if err = rows.Scan(&b.Role, &b.BlobID, &b.Position); err != nil {
			rows.Close()
			return nil, err
		}
		bindings = append(bindings, b)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return nil, err
	}
	media := []MediaObject{}
	for _, b := range bindings {
		m := MediaObject{Role: b.Role, BlobID: b.BlobID}
		var state string
		err := tx.QueryRow(ctx, "creative_blobs", "state,mime,byte_size,width,height,duration_ms", "id=$2", b.BlobID).Scan(&state, &m.Mime, &m.ByteSize, &m.Width, &m.Height, &m.DurationMs)
		if errors.Is(err, store.ErrNoRows) || err == nil && state != "ready" {
			return nil, ErrNotFound
		}
		if err != nil {
			return nil, err
		}
		media = append(media, m)
	}
	if len(media) == 0 {
		return nil, ErrNotFound
	}
	return media, nil
}

func ReadInSnapshot(ctx context.Context, tx store.ReadTxAccountScope, id, purpose string) (Revision, error) {
	return requireUsable(ctx, tx, tx.QueryRow, id, purpose)
}
func RequireUsable(ctx context.Context, tx store.TxAccountScope, id, purpose string) (Revision, error) {
	return requireUsable(ctx, tx, tx.QueryRowForUpdate, id, purpose)
}
func requireUsable(ctx context.Context, tx contentReader, protectedRow func(context.Context, string, string, string, ...any) store.Row, id, purpose string) (Revision, error) {
	if purpose != "display" && purpose != "ai_analysis" && purpose != "generation_reference" {
		return Revision{}, creativeops.ErrValidation
	}
	// Lock content/declaration before revision, matching the shared lock order.
	var contentID, declarationID string
	err := tx.QueryRow(ctx, "creative_content_revisions", "content_id, rights_declaration_id", "id=$2", id).Scan(&contentID, &declarationID)
	if errors.Is(err, store.ErrNoRows) {
		return Revision{}, ErrNotFound
	}
	if err != nil {
		return Revision{}, err
	}
	var r Revision
	if err := protectedRow(ctx, "creative_contents", "kind, origin_node_id_snapshot", "id=$2", contentID).Scan(&r.Kind, &r.OriginNodeID); err != nil {
		return Revision{}, fmt.Errorf("content identity: %w", err)
	}
	var source planningmedia.SourceClass
	var basis planningmedia.RightsBasis
	if err := protectedRow(ctx, "creative_rights_declarations", "source_class, rights_basis", "id=$2", declarationID).Scan(&source, &basis); err != nil {
		if errors.Is(err, store.ErrNoRows) {
			return Revision{}, ErrUsageDenied
		}
		return Revision{}, fmt.Errorf("read content declaration: %w", err)
	}
	if purpose != "display" {
		return Revision{}, ErrUsageDenied
	} // FND-02 does not grant AI use.
	if err := planningmedia.ValidatePurpose(planningmedia.RightsDeclarationInput{SourceClass: source, RightsBasis: basis}, planningmedia.PurposeMoodboardDisplay); err != nil {
		return Revision{}, ErrUsageDenied
	}
	var payload json.RawMessage
	var state string
	var sequence int64
	err = protectedRow(ctx, "creative_content_revisions", "id, content_id, sequence, payload, rights_declaration_id, state", "id=$2", id).Scan(&r.ID, &r.ContentID, &sequence, &payload, &r.DeclarationID, &state)
	if err != nil {
		return Revision{}, err
	}
	if state != "ready" || r.ContentID != contentID || r.DeclarationID != declarationID {
		return Revision{}, ErrNotFound
	}
	r.Sequence = creativeops.Revision(sequence)
	granted, err := tx.Exists(ctx, "creative_usage_grants", "declaration_id=$2 AND purpose=$3 AND revoked_at IS NULL", r.DeclarationID, purpose)
	if err != nil {
		return Revision{}, err
	}
	denied, err := tx.Exists(ctx, "creative_content_required_grants", "content_revision_id=$2 AND declaration_id NOT IN (SELECT declaration_id FROM creative_usage_grants WHERE account_id=$1 AND purpose=$3 AND revoked_at IS NULL)", id, purpose)
	if err != nil {
		return Revision{}, err
	}
	if !granted || denied {
		return Revision{}, ErrUsageDenied
	}
	if err := requireRoot(ctx, tx, r); err != nil {
		return Revision{}, err
	}
	if err := creativeops.Decode(payload, &r.Payload); err != nil {
		return Revision{}, err
	}
	if IsMediaKind(r.Kind) {
		media, err := loadMedia(ctx, tx, id)
		if err != nil {
			return Revision{}, err
		}
		r.Media = media
	}
	return r, nil
}

func requireRoot(ctx context.Context, tx contentReader, revision Revision) error {
	// Actual owner paths, including matching content identity/type; no arbitrary
	// relationship insertion and no authorization from historical source IDs.
	asset, err := tx.Exists(ctx, "creative_assets", "content_revision_id=$2 AND content_id=$3 AND kind=$4", revision.ID, revision.ContentID, revision.Kind)
	if err != nil {
		return err
	}
	if asset {
		return nil
	}
	node, err := tx.Exists(ctx, "creative_nodes", "content_revision_id=$2 AND content_id=$3 AND type_key=$4", revision.ID, revision.ContentID, "core."+revision.Kind)
	if err != nil {
		return err
	}
	if node {
		return nil
	}
	for _, root := range []struct{ table, condition string }{
		// A conversation is read long after the canvas moved on. What a message
		// showed has to stay readable once the asset points at a newer revision,
		// so the reference registered with the message is a root of its own.
		{"creative_message_content_refs", "content_revision_id=$2"},
		{"creative_change_content_refs", "content_revision_id=$2 AND expires_at>clock_timestamp()"},
		{"creative_node_inputs", "content_revision_id=$2"},
		{"creative_node_prompt_refs", "content_revision_id=$2"},
		{"creative_node_versions", "content_revision_id=$2 AND deleted_at IS NULL AND node_id IN (SELECT id FROM creative_nodes WHERE account_id=$1)"},
		{"creative_node_version_input_refs", "content_revision_id=$2 AND version_id IN (SELECT id FROM creative_node_versions WHERE account_id=$1 AND deleted_at IS NULL AND node_id IN (SELECT id FROM creative_nodes WHERE account_id=$1))"},
		{"creative_execution_refs", "content_revision_id=$2 AND execution_id IN (SELECT id FROM creative_node_executions WHERE account_id=$1 AND (retained_until>clock_timestamp() OR state IN ('queued','running','reconciling')))"},
		{"creative_documents", "content_revision_id=$2"},
		{"creative_upload_candidates", "content_revision_id=$2 AND state='pending' AND expires_at>clock_timestamp()"},
		{"creative_revision_holds", "content_revision_id=$2 AND (expires_at IS NULL OR expires_at>clock_timestamp())"},
	} {
		exists, err := tx.Exists(ctx, root.table, root.condition, revision.ID)
		if err != nil {
			return err
		}
		if exists {
			return nil
		}
	}
	return ErrMissingRoot
}

func Read(ctx context.Context, scope store.AccountScope, id string) (Revision, error) {
	var result Revision
	err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		if err := tx.RequireCreativeCapability(ctx, "creative_read"); err != nil {
			return err
		}
		var err error
		result, err = RequireUsable(ctx, tx, id, "display")
		// A snapshot can outlive its final reference after another editor saves.
		// This public read is unavailable; creation-time missing roots remain errors.
		if errors.Is(err, ErrMissingRoot) {
			return ErrNotFound
		}
		return err
	})
	if err != nil {
		return Revision{}, err
	}
	return result, nil
}

// Preview limits inline text while retaining the exact revision identity.
// Editors must read that revision before replacing a truncated body.
func Preview(r Revision) Revision {
	if r.Payload.Body != nil {
		runes := []rune(*r.Payload.Body)
		if len(runes) > 2000 {
			body := string(runes[:2000])
			r.Payload.Body = &body
			r.Truncated = true
		}
	}
	return r
}
