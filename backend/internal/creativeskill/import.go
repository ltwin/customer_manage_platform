package creativeskill

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

const importColumns = "id,operation_id,request_hash,target_skill_id,expected_skill_revision," +
	"state,verified_manifest,staged_objects,result_version_id,expires_at,revision"

// importRow is the persisted attempt. It is deliberately not exported: an
// import is a protocol between the admin command and this package, not a
// resource anyone else observes.
type importRow struct {
	ID              string
	OperationID     string
	RequestHash     string
	SkillID         string
	Expected        *int64
	State           string
	Doc             importDocument
	Staged          []stagedObject
	ResultVersionID *string
	ExpiresAt       time.Time
	Revision        int64
}

func (r importRow) staticView() Import {
	view := Import{
		ID: r.ID, State: r.State, SkillID: r.SkillID, Digest: r.Doc.Digest,
		Revision: creativeops.Revision(r.Revision), ExpiresAt: r.ExpiresAt,
		Pending: []string{},
	}
	if r.ResultVersionID != nil {
		view.ResultVersionID = *r.ResultVersionID
	}
	staged := make(map[string]bool, len(r.Staged))
	for _, object := range r.Staged {
		staged[object.Path] = true
	}
	for _, declared := range r.Doc.Resources {
		if !staged[declared.Path] {
			view.Pending = append(view.Pending, declared.Path)
		}
	}
	return view
}

// outOfWindow answers for an unfinished attempt only. A finalized import is
// allowed to be replayed long after its window closed: it has a version, and
// the window only ever governed the staged objects it no longer needs.
func (r importRow) outOfWindow(now time.Time) bool {
	switch r.State {
	case "expired":
		return true
	case "preparing", "ready":
		return now.After(r.ExpiresAt)
	}
	return false
}

func (r importRow) stagedFor(name string) (stagedObject, bool) {
	for _, object := range r.Staged {
		if object.Path == name {
			return object, true
		}
	}
	return stagedObject{}, false
}

type importScanner interface{ Scan(dest ...any) error }

func scanImport(row importScanner) (importRow, error) {
	var r importRow
	var doc, staged []byte
	if err := row.Scan(&r.ID, &r.OperationID, &r.RequestHash, &r.SkillID, &r.Expected,
		&r.State, &doc, &staged, &r.ResultVersionID, &r.ExpiresAt, &r.Revision); err != nil {
		return importRow{}, err
	}
	if len(doc) > 0 {
		if err := json.Unmarshal(doc, &r.Doc); err != nil {
			return importRow{}, err
		}
	}
	if len(staged) > 0 {
		if err := json.Unmarshal(staged, &r.Staged); err != nil {
			return importRow{}, err
		}
	}
	return r, nil
}

func loadImport(ctx context.Context, tx store.TxAccountScope, forUpdate bool, cond string, args ...any) (importRow, error) {
	query := tx.QueryRow
	if forUpdate {
		query = tx.QueryRowForUpdate
	}
	r, err := scanImport(query(ctx, "creative_skill_imports", importColumns, cond, args...))
	if errors.Is(err, store.ErrNoRows) {
		return importRow{}, ErrNotFound
	}
	return r, err
}

// BeginImport records one publication intent and fixes what it will freeze.
// Everything that decides the outcome is hashed here, so a retry of the same
// operation is provably the same request and a different one is refused rather
// than quietly becoming a second edit under the first attempt's name.
func (s *Service) BeginImport(ctx context.Context, scope store.AccountScope, req ImportRequest) (Import, error) {
	if err := req.validate(); err != nil {
		return Import{}, err
	}
	manifest, err := req.Manifest.encoded()
	if err != nil {
		return Import{}, err
	}
	doc := importDocument{
		Origin: req.Origin, Slug: req.Slug, DisplayName: req.DisplayName,
		Description: req.Description, Instructions: req.Instructions,
		Manifest: manifest, Resources: req.Resources, Activate: req.Activate,
		Digest: contentDigest(req.DisplayName, req.Description, req.Instructions, manifest, req.Resources),
	}
	hash := requestHash(req, manifest)
	var view Import
	var expired bool
	err = scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		// Two callers retrying one operation must not both miss the lookup and
		// both insert: the loser would hit UNIQUE(account_id,operation_id) and
		// surface an untyped database error instead of replaying. This is the
		// same advisory lock creativeops.Executor takes for the same reason.
		if err := tx.LockCreativeOperation(ctx, req.OperationID); err != nil {
			return err
		}
		existing, err := loadImport(ctx, tx, true, "operation_id=$2", req.OperationID)
		if err == nil {
			if existing.RequestHash != hash {
				return ErrImportConflict
			}
			var replayErr error
			expired, replayErr = replayImport(ctx, tx, existing, &view)
			return replayErr
		}
		if !errors.Is(err, ErrNotFound) {
			return err
		}
		skillID, err := targetSkill(ctx, tx, req)
		if err != nil {
			return err
		}
		payload, err := json.Marshal(doc)
		if err != nil {
			return err
		}
		id, err := creativeops.NewResourceID("ccsi")
		if err != nil {
			return err
		}
		now, err := tx.CreativeNow(ctx)
		if err != nil {
			return err
		}
		// An import with no resources has nothing left to upload, so it is ready
		// the moment it is recorded.
		state := "preparing"
		if len(req.Resources) == 0 {
			state = "ready"
		}
		var expected *int64
		if req.ExpectedSkillRevision > 0 {
			value := int64(req.ExpectedSkillRevision)
			expected = &value
		}
		row := importRow{
			ID: id, OperationID: req.OperationID, RequestHash: hash, SkillID: skillID,
			Expected: expected, State: state, Doc: doc, ExpiresAt: now.Add(importTTL), Revision: 1,
		}
		if err := tx.Insert(ctx, "creative_skill_imports",
			[]string{"id", "operation_id", "request_hash", "target_skill_id", "expected_skill_revision",
				"state", "verified_manifest", "expires_at", "created_at", "updated_at"},
			id, req.OperationID, hash, skillID, expected, state, payload, row.ExpiresAt, now, now); err != nil {
			return err
		}
		view = row.staticView()
		return nil
	})
	if err != nil {
		return Import{}, err
	}
	if expired {
		return Import{}, ErrImportExpired
	}
	return view, nil
}

// replayImport answers a retry of an operation that already exists. An
// unfinished import that has outlived its window is recorded as expired rather
// than handed back as if it were still live: its staged objects may already
// have been reclaimed, so continuing it would stage bytes onto a record that
// can never finalise.
//
// It reports expiry instead of returning an error, because returning one here
// would roll the transaction back and take the transition with it. The caller
// raises ErrImportExpired after the commit.
func replayImport(ctx context.Context, tx store.TxAccountScope, existing importRow, view *Import) (bool, error) {
	if existing.State == "expired" {
		return true, nil
	}
	if existing.State == "preparing" || existing.State == "ready" {
		now, err := tx.CreativeNow(ctx)
		if err != nil {
			return false, err
		}
		if now.After(existing.ExpiresAt) {
			if _, err := tx.Update(ctx, "creative_skill_imports",
				"state='expired', revision=revision+1, updated_at=$3", "id=$2", existing.ID, now); err != nil {
				return false, err
			}
			return true, nil
		}
	}
	*view = existing.staticView()
	return false, nil
}

// targetSkill names the skill this import will extend. A new skill's identity
// is minted here but its row is only written when a version actually freezes,
// so a failed import leaves no empty entry occupying a slug.
func targetSkill(ctx context.Context, tx store.TxAccountScope, req ImportRequest) (string, error) {
	var id, origin string
	var revision int64
	err := tx.QueryRow(ctx, "creative_skills", "id,origin,revision", "slug=$2", req.Slug).Scan(&id, &origin, &revision)
	switch {
	case errors.Is(err, store.ErrNoRows):
		if req.ExpectedSkillRevision != 0 {
			return "", ErrRevisionConflict
		}
		return creativeops.NewResourceID("ccsk")
	case err != nil:
		return "", err
	}
	// Naming an existing skill without saying which revision is being extended
	// is the "create" intent aimed at something that already exists.
	if req.ExpectedSkillRevision == 0 || creativeops.Revision(revision) != req.ExpectedSkillRevision {
		return "", ErrRevisionConflict
	}
	if origin != req.Origin {
		return "", ErrImportConflict
	}
	return id, nil
}

// StageResource uploads the bytes of one declared file. The declaration made at
// BeginImport is the contract: bytes that do not match its size and hash are
// refused, so what finalises is always what was hashed into the digest.
func (s *Service) StageResource(ctx context.Context, scope store.AccountScope, importID, name string, body io.Reader) (Import, error) {
	if importID == "" || !validResourcePath(name) || body == nil {
		return Import{}, creativeops.ErrValidation
	}
	var row importRow
	var outOfWindow bool
	if err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		loaded, err := loadImport(ctx, tx, false, "id=$2", importID)
		if err != nil {
			return err
		}
		now, err := tx.CreativeNow(ctx)
		if err != nil {
			return err
		}
		row, outOfWindow = loaded, loaded.outOfWindow(now)
		return nil
	}); err != nil {
		return Import{}, err
	}
	declared, declaredOK := row.Doc.declaration(name)
	if !declaredOK {
		return Import{}, creativeops.ErrValidation
	}
	// Checked before the upload, not only in the recording transaction: bytes
	// published onto a record that can never finalise are orphans nobody is
	// tracking.
	if outOfWindow {
		return Import{}, ErrImportExpired
	}
	if row.State != "preparing" {
		return Import{}, ErrImportState
	}
	// A retry reads the result the first attempt already fixed rather than
	// publishing a second object under the same key.
	if _, staged := row.stagedFor(name); staged {
		return row.staticView(), nil
	}
	payload, sum, err := readBounded(body, declared.ByteSize)
	if err != nil {
		return Import{}, err
	}
	if sum != declared.SHA256 {
		return Import{}, ErrContentMismatch
	}
	key, err := objectKey(scope.AccountID(), importID, name)
	if err != nil {
		return Import{}, err
	}
	// Storage I/O is outside every transaction: the database must not hold a
	// row lock across a network write.
	objectVersion, err := s.objects.PublishVerified(ctx, key, bytes.NewReader(payload), declared.ByteSize, declared.Mime)
	if err != nil {
		return Import{}, fmt.Errorf("%w: publish %s: %v", ErrResourceUnavailable, name, err)
	}
	mine := stagedObject{
		Path: name, Driver: s.objects.Driver(), Bucket: s.objects.Bucket(), Key: key,
		Version: objectVersion, Mime: declared.Mime, ByteSize: declared.ByteSize, SHA256: declared.SHA256,
	}
	var view Import
	var superseded *stagedObject
	err = scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		current, err := loadImport(ctx, tx, true, "id=$2", importID)
		if err != nil {
			return err
		}
		// Reconciling our object against the record comes before every state
		// gate, and the order is load-bearing. A concurrent upload of the same
		// file may have won the lock and moved the import on; whether the
		// import already holds these bytes does not depend on the state it has
		// since reached. Gating first would send this call down the failure
		// path below, which deletes the object it just published — and on the
		// local driver that object is content-addressed, so the winner's copy
		// and ours are the same file. The import, and soon a frozen version,
		// would reference bytes that no longer exist.
		if winner, staged := current.stagedFor(name); staged {
			if winner.Key != mine.Key || winner.Version != mine.Version {
				superseded = &mine
			}
			view = current.staticView()
			return nil
		}
		now, err := tx.CreativeNow(ctx)
		if err != nil {
			return err
		}
		if current.outOfWindow(now) {
			return ErrImportExpired
		}
		if current.State != "preparing" {
			return ErrImportState
		}
		current.Staged = append(current.Staged, mine)
		state := current.State
		if len(current.Staged) == len(current.Doc.Resources) {
			state = "ready"
		}
		objects, err := json.Marshal(current.Staged)
		if err != nil {
			return err
		}
		if _, err := tx.Update(ctx, "creative_skill_imports",
			"staged_objects=$3, state=$4, revision=revision+1, updated_at=$5", "id=$2",
			importID, objects, state, now); err != nil {
			return err
		}
		current.State, current.Revision = state, current.Revision+1
		view = current.staticView()
		return nil
	})
	if err != nil {
		// The bytes are published but nothing recorded them, so drop them here
		// rather than leaving work for reclamation — except when the commit
		// outcome is unknown, where the row may in fact reference them.
		// A crash before this line still orphans the object, which is why
		// reclamation sweeps the import's key prefix and not staged_objects.
		if !errors.Is(err, store.ErrCommitOutcomeUnknown) {
			_ = s.objects.DeleteExact(ctx, mine.Key, mine.Version)
		}
		return Import{}, err
	}
	if superseded != nil {
		// Best effort: the import does not reference these bytes, and a failure
		// here only leaves them for the reclaim command.
		_ = s.objects.DeleteExact(ctx, superseded.Key, superseded.Version)
	}
	return view, nil
}

// FinalizeImport freezes the version in one short transaction. Every byte it
// needs was verified and uploaded beforehand, so no object I/O happens here and
// the same import always produces the same single version.
func (s *Service) FinalizeImport(ctx context.Context, scope store.AccountScope, importID string) (Version, error) {
	if importID == "" {
		return Version{}, creativeops.ErrValidation
	}
	var frozen Version
	err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		row, err := loadImport(ctx, tx, true, "id=$2", importID)
		if err != nil {
			return err
		}
		if row.State == "finalized" && row.ResultVersionID != nil {
			frozen, err = readVersion(ctx, tx, *row.ResultVersionID)
			return err
		}
		now, err := tx.CreativeNow(ctx)
		if err != nil {
			return err
		}
		if row.outOfWindow(now) {
			return ErrImportExpired
		}
		if row.State != "ready" {
			return ErrImportState
		}
		resources, err := matchStaged(row)
		if err != nil {
			return err
		}
		number, err := reserveVersionNumber(ctx, tx, row, now)
		if err != nil {
			return err
		}
		versionID, err := creativeops.NewResourceID("ccsv")
		if err != nil {
			return err
		}
		if err := tx.Insert(ctx, "creative_skill_versions",
			[]string{"id", "skill_id", "version_number", "schema_version", "display_name_snapshot",
				"description_snapshot", "instructions", "manifest", "digest", "created_at"},
			versionID, row.SkillID, number, versionSchemaVersion, row.Doc.DisplayName,
			row.Doc.Description, row.Doc.Instructions, []byte(row.Doc.Manifest), row.Doc.Digest, now); err != nil {
			return err
		}
		for _, object := range resources {
			if err := tx.Insert(ctx, "creative_skill_version_resources",
				[]string{"skill_version_id", "path", "mime", "byte_size", "sha256",
					"storage_driver", "bucket", "object_key", "object_version", "created_at"},
				versionID, object.Path, object.Mime, object.ByteSize, object.SHA256,
				object.Driver, object.Bucket, object.Key, object.Version, now); err != nil {
				return err
			}
		}
		if err := publishVersion(ctx, tx, row, versionID, now); err != nil {
			return err
		}
		if _, err := tx.Update(ctx, "creative_skill_imports",
			"state='finalized', result_version_id=$3, revision=revision+1, updated_at=$4", "id=$2",
			importID, versionID, now); err != nil {
			return err
		}
		frozen, err = readVersion(ctx, tx, versionID)
		return err
	})
	if err != nil {
		return Version{}, err
	}
	return frozen, nil
}

// matchStaged proves every declared file has an uploaded object. The real
// proof that the bytes match happened at StageResource, which hashed them
// before publishing; the size and hash compared here were copied from the
// declaration, so within one run they are equal by construction. What this
// catches is a staged_objects list that was edited or truncated behind the
// service's back — enough to keep a hand-patched row from freezing a version
// whose digest describes files it does not have.
func matchStaged(row importRow) ([]stagedObject, error) {
	matched := make([]stagedObject, 0, len(row.Doc.Resources))
	for _, declared := range row.Doc.Resources {
		object, staged := row.stagedFor(declared.Path)
		if !staged || object.SHA256 != declared.SHA256 || object.ByteSize != declared.ByteSize {
			return nil, ErrContentMismatch
		}
		matched = append(matched, object)
	}
	return matched, nil
}

// reserveVersionNumber takes the next number under the skill's row lock and
// creates the skill itself if this import is the one that introduces it.
func reserveVersionNumber(ctx context.Context, tx store.TxAccountScope, row importRow, now time.Time) (int, error) {
	var number int
	var revision int64
	err := tx.QueryRowForUpdate(ctx, "creative_skills", "next_version_number,revision", "id=$2", row.SkillID).
		Scan(&number, &revision)
	switch {
	case errors.Is(err, store.ErrNoRows):
		if row.Expected != nil {
			return 0, ErrRevisionConflict
		}
		// UNIQUE(account_id,slug) decides a race between two imports that both
		// set out to create this slug; the loser reports a conflict rather than
		// silently joining someone else's skill line.
		if err := tx.Insert(ctx, "creative_skills",
			[]string{"id", "origin", "slug", "display_name", "description", "created_at", "updated_at"},
			row.SkillID, row.Doc.Origin, row.Doc.Slug, row.Doc.DisplayName, row.Doc.Description, now, now); err != nil {
			return 0, err
		}
		return 1, nil
	case err != nil:
		return 0, err
	}
	if row.Expected == nil || *row.Expected != revision {
		return 0, ErrRevisionConflict
	}
	return number, nil
}

// publishVersion moves the skill forward: the next number is consumed, the
// catalog text follows the newest version, and the recommended pointer moves
// only when the import asked for it.
func publishVersion(ctx context.Context, tx store.TxAccountScope, row importRow, versionID string, now time.Time) error {
	set := "next_version_number=next_version_number+1, revision=revision+1," +
		" display_name=$3, description=$4, updated_at=$5"
	args := []any{row.SkillID, row.Doc.DisplayName, row.Doc.Description, now}
	if row.Doc.Activate {
		set += ", current_version_id=$6"
		args = append(args, versionID)
	}
	changed, err := tx.Update(ctx, "creative_skills", set, "id=$2", args...)
	if err != nil {
		return err
	}
	if changed != 1 {
		return ErrNotFound
	}
	return nil
}

func readVersion(ctx context.Context, tx store.TxAccountScope, versionID string) (Version, error) {
	var v Version
	var manifest []byte
	var reason *string
	err := tx.QueryRow(ctx, "creative_skill_versions", versionColumns, "id=$2", versionID).
		Scan(&v.ID, &v.SkillID, &v.VersionNumber, &v.SchemaVersion, &v.DisplayName, &v.Description,
			new(string), &manifest, &v.Digest, &v.ExecutionState, &reason, &v.CreatedAt)
	if errors.Is(err, store.ErrNoRows) {
		return Version{}, ErrNotFound
	}
	if err != nil {
		return Version{}, err
	}
	if reason != nil {
		v.DisabledReason = *reason
	}
	v.OwnerAccountID = tx.AccountID()
	// The owning skill's revision travels with the version so a publisher can
	// chain the next import without guessing what it just moved to.
	var revision int64
	if err := tx.QueryRow(ctx, "creative_skills", "revision", "id=$2", v.SkillID).Scan(&revision); err != nil {
		if errors.Is(err, store.ErrNoRows) {
			return Version{}, ErrNotFound
		}
		return Version{}, err
	}
	v.SkillRevision = creativeops.Revision(revision)
	return v, nil
}
