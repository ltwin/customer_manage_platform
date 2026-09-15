package creativeskill

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"

	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
	"github.com/samson/customer-manage-platform/backend/internal/platform/versionedfs"
)

// ObjectPort is the slice of the platform object store a skill needs: trusted
// writes of already-verified bytes and reads fixed to one exact version. The
// media package's multipart staging surface is deliberately absent, because a
// skill resource is never uploaded by a browser. Driver and Bucket are here so
// a version can record which object it reads, not to widen the port.
type ObjectPort interface {
	Driver() string
	Bucket() string
	PublishVerified(ctx context.Context, key string, body io.Reader, size int64, mime string) (string, error)
	StatVersion(ctx context.Context, key, version string) (versionedfs.ObjectStat, error)
	OpenVersion(ctx context.Context, key, version string, rng *versionedfs.ByteRange) (io.ReadCloser, versionedfs.ObjectStat, error)
	DeleteExact(ctx context.Context, key, version string) error
}

// Service is the skill application. Its write ports are trusted administrative
// entry points; no HTTP handler may reach them.
type Service struct {
	objects ObjectPort
	// platformAccountID is the one account allowed to publish platform skills.
	// It lives here rather than in the command that happens to call today,
	// because the rule is about which account owns the platform catalog, not
	// about which binary is running. Empty means this deployment has none.
	platformAccountID string
}

func NewService(objects ObjectPort, platformAccountID string) (*Service, error) {
	if objects == nil {
		return nil, errors.New("creative skill service requires an object port")
	}
	return &Service{objects: objects, platformAccountID: platformAccountID}, nil
}

// mayPublishAs is the §5 barrier. An ordinary account writing origin=platform
// would otherwise put itself in every account's catalog, and the import path is
// where that has to be refused — not in whichever caller remembered to check.
func (s *Service) mayPublishAs(origin, accountID string) error {
	if origin != "platform" {
		return nil
	}
	if s.platformAccountID == "" || s.platformAccountID != accountID {
		return ErrOriginNotPermitted
	}
	return nil
}

// stagedObject is one uploaded resource recorded on the import. It carries the
// exact object identity so finalising needs no second upload and reclaiming a
// failed import needs no guesswork about which bytes it owns.
type stagedObject struct {
	Path     string `json:"path"`
	Driver   string `json:"storage_driver"`
	Bucket   string `json:"bucket"`
	Key      string `json:"object_key"`
	Version  string `json:"object_version"`
	Mime     string `json:"mime"`
	ByteSize int64  `json:"byte_size"`
	SHA256   string `json:"sha256"`
}

// importDocument is the verified intent an import froze at BeginImport. Object
// upload and the database cannot share a transaction, so the intent has to
// survive between the two; this copy expires with the import and is never a
// second authority for a published version.
type importDocument struct {
	Origin       string                `json:"origin"`
	Slug         string                `json:"slug"`
	DisplayName  string                `json:"display_name"`
	Description  string                `json:"description"`
	Instructions string                `json:"instructions"`
	Manifest     json.RawMessage       `json:"manifest"`
	Resources    []ResourceDeclaration `json:"resources"`
	Activate     bool                  `json:"activate"`
	Digest       string                `json:"digest"`
}

func (d importDocument) declaration(name string) (ResourceDeclaration, bool) {
	for _, r := range d.Resources {
		if r.Path == name {
			return r, true
		}
	}
	return ResourceDeclaration{}, false
}

// skillObjectPrefix is the namespace this package owns. Media, avatars and
// planning objects live under their own prefixes and are never reachable here.
const skillObjectPrefix = "creative-skills/"

// objectKey locates one staged resource. An import owns its own prefix, so a
// retried upload rewrites its own place and two imports can never collide.
//
// The check is that the prefix survived, not that the result is clean: Join
// cleans for us, and cleaning is exactly how a segment containing ".." would
// escape into another namespace while still looking well-formed.
func objectKey(accountID, importID, name string) (string, error) {
	key := path.Join(skillObjectPrefix, accountID, "imports", importID, name)
	if !strings.HasPrefix(key, skillObjectPrefix+accountID+"/imports/"+importID+"/") {
		return "", creativeops.ErrValidation
	}
	return key, nil
}

// Usable reports whether this version may start new work. Both switches matter:
// a skill can be withdrawn as a whole, and one bad version can be withdrawn on
// its own without touching the rest of its line.
func (s Snapshot) Usable() bool {
	return s.ExecutionState == "active" && s.SkillAvailability == "active"
}

const versionColumns = "id,skill_id,version_number,schema_version,display_name_snapshot," +
	"description_snapshot,instructions,manifest,digest,execution_status,disabled_reason,created_at"

// ResolveVersion reads one fixed version and its declared resources. Callers
// pass the version a message or a run already froze, never a current pointer.
// Resource bytes are not read here; a catalog request must not touch storage.
func (s *Service) ResolveVersion(ctx context.Context, scope store.AccountScope, skillID, versionID string) (Snapshot, error) {
	if skillID == "" || versionID == "" {
		return Snapshot{}, creativeops.ErrValidation
	}
	var snapshot Snapshot
	err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		var manifest []byte
		var reason *string
		err := tx.QueryRow(ctx, "creative_skill_versions", versionColumns, "id=$2 AND skill_id=$3", versionID, skillID).
			Scan(&snapshot.ID, &snapshot.SkillID, &snapshot.VersionNumber, &snapshot.SchemaVersion,
				&snapshot.DisplayName, &snapshot.Description, &snapshot.Instructions, &manifest,
				&snapshot.Digest, &snapshot.ExecutionState, &reason, &snapshot.CreatedAt)
		if errors.Is(err, store.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if reason != nil {
			snapshot.DisabledReason = *reason
		}
		if err := json.Unmarshal(manifest, &snapshot.Manifest); err != nil {
			return err
		}
		snapshot.OwnerAccountID = tx.AccountID()
		var revision int64
		if err := tx.QueryRow(ctx, "creative_skills", "availability,revision", "id=$2", skillID).
			Scan(&snapshot.SkillAvailability, &revision); err != nil {
			if errors.Is(err, store.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		snapshot.SkillRevision = creativeops.Revision(revision)
		resources, err := readResourceRows(ctx, tx, versionID)
		if err != nil {
			return err
		}
		snapshot.Resources = make([]Resource, 0, len(resources))
		for _, r := range resources {
			snapshot.Resources = append(snapshot.Resources, Resource{Path: r.Path, Mime: r.Mime, ByteSize: r.ByteSize, SHA256: r.SHA256})
		}
		return nil
	})
	if err != nil {
		return Snapshot{}, err
	}
	return snapshot, nil
}

func readResourceRows(ctx context.Context, tx store.TxAccountScope, versionID string) ([]stagedObject, error) {
	rows, err := tx.QueryPage(ctx, "creative_skill_version_resources",
		"path,mime,byte_size,sha256,storage_driver,bucket,object_key,object_version",
		"skill_version_id=$2", []store.OrderBy{{Column: "path"}}, maxResourceCount, 0, versionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []stagedObject
	for rows.Next() {
		var r stagedObject
		if err := rows.Scan(&r.Path, &r.Mime, &r.ByteSize, &r.SHA256, &r.Driver, &r.Bucket, &r.Key, &r.Version); err != nil {
			return nil, err
		}
		list = append(list, r)
	}
	return list, rows.Err()
}

// ReadResource returns the bytes of one declared file. An undeclared path is
// refused by the lookup: there is no directory underneath this call, and the
// caller never names a storage location. The content is re-verified against the
// hash the version froze, so a replaced or corrupted object fails loudly rather
// than reaching a model.
func (s *Service) ReadResource(ctx context.Context, scope store.AccountScope, versionID, name string) ([]byte, error) {
	if versionID == "" || !validResourcePath(name) {
		return nil, creativeops.ErrValidation
	}
	var located stagedObject
	err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		err := tx.QueryRow(ctx, "creative_skill_version_resources",
			"path,mime,byte_size,sha256,storage_driver,bucket,object_key,object_version",
			"skill_version_id=$2 AND path=$3", versionID, name).
			Scan(&located.Path, &located.Mime, &located.ByteSize, &located.SHA256,
				&located.Driver, &located.Bucket, &located.Key, &located.Version)
		if errors.Is(err, store.ErrNoRows) {
			return ErrNotFound
		}
		return err
	})
	if err != nil {
		return nil, err
	}
	// Storage I/O stays outside the transaction: a slow object read must never
	// hold a database row lock.
	body, _, err := s.objects.OpenVersion(ctx, located.Key, located.Version, nil)
	if err != nil {
		// The port's errors stop here. A frozen version whose object has gone
		// missing is a server-side integrity fault, and versionedfs.ErrNotFound
		// is the very value creativemedia adopted — letting it out would have
		// the API edge answer "upload session not found" about a skill file.
		return nil, fmt.Errorf("%w: %s: %v", ErrResourceUnavailable, name, err)
	}
	defer func() { _ = body.Close() }()
	bytes, sum, err := readBounded(body, located.ByteSize)
	if err != nil {
		return nil, err
	}
	if sum != located.SHA256 {
		return nil, ErrContentMismatch
	}
	return bytes, nil
}

// readBounded reads exactly size bytes and hashes them. It reads one byte past
// the bound on purpose: a longer object is a mismatch, not a truncation to
// accept quietly.
func readBounded(r io.Reader, size int64) ([]byte, string, error) {
	if size < 1 || size > maxResourceBytes {
		return nil, "", ErrLimit
	}
	buf := make([]byte, size+1)
	read, err := io.ReadFull(r, buf)
	switch {
	case errors.Is(err, io.ErrUnexpectedEOF), errors.Is(err, io.EOF):
	case err != nil:
		return nil, "", err
	}
	if int64(read) != size {
		return nil, "", ErrContentMismatch
	}
	body := buf[:read]
	sum := sha256.Sum256(body)
	return body, hex.EncodeToString(sum[:]), nil
}

// ActivateVersion moves the skill's recommended pointer. Messages and runs that
// already fixed a version keep it; this only changes what a new selection gets.
func (s *Service) ActivateVersion(ctx context.Context, scope store.AccountScope, skillID, versionID string, expected creativeops.Revision) error {
	if skillID == "" || versionID == "" || expected < 1 {
		return creativeops.ErrValidation
	}
	return scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		var revision int64
		err := tx.QueryRowForUpdate(ctx, "creative_skills", "revision", "id=$2", skillID).Scan(&revision)
		if errors.Is(err, store.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if creativeops.Revision(revision) != expected {
			return ErrRevisionConflict
		}
		var status string
		err = tx.QueryRow(ctx, "creative_skill_versions", "execution_status", "id=$2 AND skill_id=$3", versionID, skillID).Scan(&status)
		if errors.Is(err, store.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if status != "active" {
			return ErrImportState
		}
		now, err := tx.CreativeNow(ctx)
		if err != nil {
			return err
		}
		_, err = tx.Update(ctx, "creative_skills",
			"current_version_id=$3, revision=revision+1, updated_at=$4", "id=$2", skillID, versionID, now)
		return err
	})
}

// DisableVersion withdraws one version from new work without deleting it or
// changing its digest. History keeps rendering what it always did.
func (s *Service) DisableVersion(ctx context.Context, scope store.AccountScope, versionID, reason string) error {
	if versionID == "" {
		return creativeops.ErrValidation
	}
	if n := len([]rune(reason)); n < 1 || n > 500 {
		return creativeops.ErrValidation
	}
	return scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		now, err := tx.CreativeNow(ctx)
		if err != nil {
			return err
		}
		changed, err := tx.Update(ctx, "creative_skill_versions",
			"execution_status='disabled', disabled_at=$3, disabled_reason=$4",
			"id=$2 AND execution_status='active'", versionID, now, reason)
		if err != nil {
			return err
		}
		if changed == 0 {
			// Either it does not exist in this account or it is already off.
			// Both mean the caller has nothing left to do here.
			exists, err := tx.Exists(ctx, "creative_skill_versions", "id=$2", versionID)
			if err != nil {
				return err
			}
			if !exists {
				return ErrNotFound
			}
		}
		return nil
	})
}
