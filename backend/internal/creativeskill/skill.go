// Package creativeskill owns skill identity, frozen versions, their declared
// resources and the trusted import that publishes them. A skill's authoritative
// content lives in PostgreSQL and its resource bytes in the platform object
// store, so maintaining one no longer means releasing the binary.
//
// Nothing here is reachable from a photographer's request. Publishing is a
// trusted administrative act: the write ports below are called by the admin
// command line, never by an HTTP handler.
package creativeskill

import (
	"encoding/json"
	"errors"
	"path"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
)

var (
	ErrNotFound = errors.New("creative skill not found")
	// ErrRevisionConflict means another publisher moved the skill first. The
	// import is kept so the caller can decide explicitly, not overwritten.
	ErrRevisionConflict = errors.New("creative skill revision conflict")
	// ErrSkillIntent means the request's create-or-extend intent does not match
	// what exists: a revision named for a skill that is not there, or no
	// revision named for one that is. It is deliberately not a conflict —
	// telling the caller to re-read a revision and retry would be advice that
	// can never work, because nothing about their request is racing anyone.
	ErrSkillIntent = errors.New("creative skill import intent does not match the existing skill")
	// ErrOriginNotPermitted means this account may not publish under the origin
	// it asked for. Which account owns the platform catalog is a deployment
	// fact, so the answer does not depend on which entry point is calling.
	ErrOriginNotPermitted = errors.New("creative skill origin not permitted for this account")
	// ErrImportConflict means one operation id arrived carrying two different
	// requests. Replaying an import is safe; redefining one is not.
	ErrImportConflict = errors.New("creative skill import conflict")
	ErrImportState    = errors.New("creative skill import state does not allow this action")
	// ErrImportExpired is distinct from ErrImportState on purpose: an import
	// that ran out of window needs a new operation id, and the caller cannot
	// work that out from "wrong state".
	ErrImportExpired = errors.New("creative skill import expired")
	// ErrResourceUnavailable means the object store could not serve or accept a
	// version's bytes. The port's own error values stop at this package's
	// boundary: they are shared with creativemedia, whose vocabulary the API
	// edge routes on, so letting one escape would render a missing skill
	// resource as a missing media upload.
	ErrResourceUnavailable = errors.New("creative skill resource unavailable")
	// ErrContentMismatch means staged bytes did not match what the import
	// declared. The declaration is the contract; the bytes are the claim.
	ErrContentMismatch = errors.New("creative skill content does not match its declaration")
	ErrLimit           = errors.New("creative skill limit exceeded")
)

const (
	// versionSchemaVersion is the format of a frozen version document, which is
	// not the same number as the version itself.
	versionSchemaVersion = 1
	// manifestSchemaVersion is the format of the declaration inside a version.
	manifestSchemaVersion = 1

	// A skill is instructions, not a data set. These bounds keep one package
	// from quietly consuming a run's whole input budget. They are part of the
	// versioned limits policy; raising one is a limits change, not a tweak.
	//
	// MaxResourceCount and MaxPackageBytes are exported because a run is judged
	// by the limits it was created under, so they have to reach the assistant's
	// published Limits. They are projected there rather than retyped: two
	// spellings of one bound is how a package that imports cleanly starts
	// failing at execution.
	maxResourceBytes = 64 << 10
	// MaxResourceCount bounds the declared files of one version.
	MaxResourceCount    = 20
	maxInstructionBytes = 64 << 10
	maxManifestBytes    = 64 << 10
	// MaxPackageBytes bounds one package's whole text.
	MaxPackageBytes = 256 << 10

	// importTTL bounds how long an unfinished import keeps its staged objects.
	// Nothing sweeps it automatically yet; the admin command reclaims them.
	importTTL = 24 * time.Hour
)

// Manifest is the platform's own declaration of a version. Identity, name and
// description are columns of the skill and the version rather than manifest
// fields: two places that can disagree about a name is one place too many.
type Manifest struct {
	ManifestSchemaVersion int      `json:"manifest_schema_version"`
	InputKinds            []string `json:"input_kinds"`
	MaxInputs             int      `json:"max_inputs"`
	// ToolAllowlist is an upper bound intersected with the photographer's own
	// action scope at execution time. A skill can never widen it.
	ToolAllowlist             []string `json:"tool_allowlist"`
	RequiredModelCapabilities []string `json:"required_model_capabilities"`
	OutputContract            string   `json:"output_contract"`
	// CompletionCheckKey selects a registered checker. A package cannot ship
	// code, so this can only ever name something the deployment already has.
	CompletionCheckKey string `json:"completion_check_key"`
}

// Resource is one declared file of a version: what a caller may ask for and
// verify. The object address that actually holds the bytes stays inside this
// package — it is a server-internal location, not a reusable storage key.
type Resource struct {
	Path     string `json:"path"`
	Mime     string `json:"mime"`
	ByteSize int64  `json:"byte_size"`
	SHA256   string `json:"sha256"`
}

// Version is the discovery projection of one frozen version.
type Version struct {
	ID             string               `json:"id"`
	SkillID        string               `json:"skill_id"`
	OwnerAccountID string               `json:"-"`
	VersionNumber  int                  `json:"version_number"`
	SchemaVersion  int                  `json:"schema_version"`
	DisplayName    string               `json:"display_name"`
	Description    string               `json:"description"`
	Digest         string               `json:"digest"`
	ExecutionState string               `json:"execution_state"`
	DisabledReason string               `json:"disabled_reason,omitempty"`
	SkillRevision  creativeops.Revision `json:"-"`
	CreatedAt      time.Time            `json:"created_at"`
}

// Snapshot is the whole frozen content a run holds. A run resolves it once and
// keeps it; it never re-reads the skill's current version pointer.
type Snapshot struct {
	Version
	Instructions string     `json:"instructions"`
	Manifest     Manifest   `json:"manifest"`
	Resources    []Resource `json:"resources"`
	// SkillAvailability is the owning skill's switch as read alongside the
	// version, so one lookup answers both halves of "may this start work now".
	SkillAvailability string `json:"skill_availability"`
	// Origin is the owning skill's catalog, read from the row rather than
	// inferred from whether the reader happens to own it.
	Origin string `json:"origin"`
}

// Skill is the mutable identity that owns a line of versions.
type Skill struct {
	ID               string               `json:"id"`
	OwnerAccountID   string               `json:"-"`
	Origin           string               `json:"origin"`
	Slug             string               `json:"slug"`
	DisplayName      string               `json:"display_name"`
	Description      string               `json:"description"`
	CurrentVersionID string               `json:"current_version_id,omitempty"`
	Availability     string               `json:"availability"`
	Revision         creativeops.Revision `json:"-"`
}

// ResourceDeclaration is what an import promises to upload. Staged bytes are
// checked against it, so the declaration — not the upload — fixes the digest.
type ResourceDeclaration struct {
	Path     string `json:"path"`
	Mime     string `json:"mime"`
	ByteSize int64  `json:"byte_size"`
	SHA256   string `json:"sha256"`
}

// ImportRequest is one complete publication intent. Everything that decides
// what gets frozen is in here, which is what makes a retry provably the same
// request rather than a second edit wearing the same operation id.
type ImportRequest struct {
	// OperationID identifies the attempt; the rest identifies the content.
	OperationID string
	// Origin is chosen by the trusted caller. A platform skill exists only
	// because an administrator published it as one.
	Origin       string
	Slug         string
	DisplayName  string
	Description  string
	Instructions string
	Manifest     Manifest
	Resources    []ResourceDeclaration
	// ExpectedSkillRevision is empty when this import creates the skill, and
	// otherwise must match the skill row it is about to add a version to.
	ExpectedSkillRevision creativeops.Revision
	// Activate moves the skill's current version pointer. Existing messages and
	// runs keep the version they already fixed.
	Activate bool
}

// Import is the bounded record of one publication attempt.
type Import struct {
	ID       string               `json:"id"`
	State    string               `json:"state"`
	SkillID  string               `json:"skill_id"`
	Digest   string               `json:"digest"`
	Revision creativeops.Revision `json:"-"`
	// ResultVersionID is set once, when the import finalises.
	ResultVersionID string    `json:"result_version_id,omitempty"`
	ExpiresAt       time.Time `json:"expires_at"`
	// Pending lists the declared paths whose bytes have not been staged yet.
	Pending []string `json:"pending"`
}

func validSlug(slug string) bool {
	if len(slug) == 0 || len(slug) > 64 || slug[0] < 'a' || slug[0] > 'z' {
		return false
	}
	for _, r := range slug {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' {
			return false
		}
	}
	return true
}

// validResourcePath accepts one normalised relative path and nothing else. An
// absolute path, a traversal or a name that cleans to something different is
// refused here rather than being normalised into a different file.
func validResourcePath(name string) bool {
	if name == "" || len(name) > 255 || !utf8.ValidString(name) {
		return false
	}
	if path.IsAbs(name) || path.Clean(name) != name || strings.HasPrefix(name, "../") || name == ".." {
		return false
	}
	return !strings.Contains(name, "\\")
}

func (m Manifest) validate() error {
	if m.ManifestSchemaVersion != manifestSchemaVersion || m.MaxInputs < 1 {
		return creativeops.ErrValidation
	}
	if m.CompletionCheckKey == "" || m.OutputContract == "" {
		return creativeops.ErrValidation
	}
	// A skill that declares no tools and no inputs describes no work; accepting
	// it would put an entry in the catalog that can never reach its own
	// completion standard.
	if len(m.ToolAllowlist) == 0 || len(m.InputKinds) == 0 {
		return creativeops.ErrValidation
	}
	for _, list := range [][]string{m.InputKinds, m.ToolAllowlist, m.RequiredModelCapabilities} {
		for _, item := range list {
			if item == "" || !utf8.ValidString(item) {
				return creativeops.ErrValidation
			}
		}
	}
	return nil
}

// normalized replaces absent lists with empty ones. A manifest that omits
// required_model_capabilities is valid — the field means "needs nothing
// special" — but a nil slice serialises as null, and the API declares an array.
//
// This runs on the way out and never on the way in. What the API shows is a
// presentation rule that may change; what the digest covers is a persisted
// identity that may not. Normalising before the digest would tie the two
// together, so that relaxing an array's rendering silently re-hashes every
// package imported under the old rule — see decodeManifest.
func (m Manifest) normalized() Manifest {
	if m.InputKinds == nil {
		m.InputKinds = []string{}
	}
	if m.ToolAllowlist == nil {
		m.ToolAllowlist = []string{}
	}
	if m.RequiredModelCapabilities == nil {
		m.RequiredModelCapabilities = []string{}
	}
	return m
}

// decodeManifest is the only way a stored manifest re-enters the program, so
// the nil-versus-empty distinction that the digest has to preserve cannot reach
// a caller. Every read path goes through here; none of them normalises itself.
func decodeManifest(raw []byte) (Manifest, error) {
	var m Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return Manifest{}, err
	}
	return m.normalized(), nil
}

// encoded is both what gets stored and what the digest covers, so the stored
// declaration and the identity of the version can never drift apart.
//
// It marshals the manifest exactly as it was declared. These bytes reach
// contentDigest and, through it, the request_hash a retried import is compared
// against — so any change to this encoding retroactively invalidates every
// import already recorded, and a replay of a pinned operation id would report a
// conflict that no retry can clear. TestTheManifestEncodingIsAPersistedContract
// pins it for that reason.
func (m Manifest) encoded() ([]byte, error) {
	body, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	if len(body) > maxManifestBytes {
		return nil, ErrLimit
	}
	return body, nil
}

// validate checks everything about a request that does not need the database.
func (r ImportRequest) validate() error {
	if !creativeops.ValidOperationID(r.OperationID) {
		return creativeops.ErrValidation
	}
	if r.Origin != "platform" && r.Origin != "account" {
		return creativeops.ErrValidation
	}
	if !validSlug(r.Slug) {
		return creativeops.ErrValidation
	}
	if n := utf8.RuneCountInString(r.DisplayName); n < 1 || n > 120 {
		return creativeops.ErrValidation
	}
	if n := utf8.RuneCountInString(r.Description); n < 1 || n > 500 {
		return creativeops.ErrValidation
	}
	if r.Instructions == "" || !utf8.ValidString(r.Instructions) {
		return creativeops.ErrValidation
	}
	if len(r.Instructions) > maxInstructionBytes {
		return ErrLimit
	}
	if err := r.Manifest.validate(); err != nil {
		return err
	}
	manifest, err := r.Manifest.encoded()
	if err != nil {
		return err
	}
	if len(r.Resources) > MaxResourceCount {
		return ErrLimit
	}
	total := len(r.Instructions) + len(manifest)
	seen := make(map[string]bool, len(r.Resources))
	for _, res := range r.Resources {
		if !validResourcePath(res.Path) || seen[res.Path] {
			return creativeops.ErrValidation
		}
		seen[res.Path] = true
		if res.Mime == "" || len(res.Mime) > 127 || !validHex64(res.SHA256) {
			return creativeops.ErrValidation
		}
		if res.ByteSize < 1 {
			return creativeops.ErrValidation
		}
		if res.ByteSize > maxResourceBytes {
			return ErrLimit
		}
		total += int(res.ByteSize)
	}
	if total > MaxPackageBytes {
		return ErrLimit
	}
	return nil
}

func validHex64(s string) bool {
	if len(s) != 64 {
		return false
	}
	for i := range len(s) {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}
