package creativeskill

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"path"
	"strings"
	"unicode/utf8"

	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
)

const (
	packageManifestName = "manifest.json"
	packageBodyName     = "SKILL.md"
)

// Package is one importable directory, already read and hashed. It is the
// auditable material an administrator points the import command at; it is not
// an authority about anything after the import, because the frozen version is.
type Package struct {
	Slug         string
	DisplayName  string
	Description  string
	Instructions string
	Manifest     Manifest
	Resources    []ResourceDeclaration
	bodies       map[string][]byte
}

// PublishIntent is everything about an import that the directory cannot decide:
// who is publishing, under which operation, onto which revision of the skill,
// and whether the result becomes the recommended version.
type PublishIntent struct {
	OperationID           string
	Origin                string
	ExpectedSkillRevision creativeops.Revision
	Activate              bool
}

// Request combines the two into one publication intent.
func (p Package) Request(intent PublishIntent) ImportRequest {
	return ImportRequest{
		OperationID: intent.OperationID, Origin: intent.Origin,
		Slug: p.Slug, DisplayName: p.DisplayName, Description: p.Description,
		Instructions: p.Instructions, Manifest: p.Manifest, Resources: p.Resources,
		ExpectedSkillRevision: intent.ExpectedSkillRevision, Activate: intent.Activate,
	}
}

// Body returns the bytes of one declared file as a fresh reader, so staging a
// file twice reads the same content rather than an exhausted stream.
func (p Package) Body(name string) (io.Reader, bool) {
	body, ok := p.bodies[name]
	if !ok {
		return nil, false
	}
	return bytes.NewReader(body), true
}

// packageManifest is the on-disk form of the declaration. It is decoded
// strictly: a misspelled field in a package that grants tool access should stop
// the import, not silently import a narrower skill than the file describes.
type packageManifest struct {
	ManifestSchemaVersion int    `json:"manifest_schema_version"`
	Key                   string `json:"key"`
	// Version records what the embedded package format called this release.
	// Version numbers are now assigned by the server under the skill's row lock,
	// so this is read and deliberately not consumed — dropping the field would
	// fail the strict decode, and honouring it would let a number written in a
	// file contradict the number the frozen version actually received.
	Version                   int      `json:"version"`
	DisplayName               string   `json:"display_name"`
	Description               string   `json:"description"`
	InputKinds                []string `json:"input_kinds"`
	MaxInputs                 int      `json:"max_inputs"`
	ToolAllowlist             []string `json:"tool_allowlist"`
	RequiredModelCapabilities []string `json:"required_model_capabilities"`
	OutputContract            string   `json:"output_contract"`
	CompletionCheckKey        string   `json:"completion_check_key"`
}

// ReadPackage reads one package directory. Every file other than the manifest
// and the body is a declared resource at its relative path; there is no
// convention by which a file in the directory fails to be declared, so what an
// operator can see in the directory is exactly what gets published.
//
// That promise is why a hidden or non-regular entry is an error rather than
// something to skip or to publish. A .DS_Store an operator cannot see would
// otherwise either become a permanent declared resource of a frozen version —
// versions are not deleted in this phase — or, appearing after the first run,
// change the content digest and turn the pinned seed operation id into a
// conflict that can never be replayed. Failing here costs one `rm`.
func ReadPackage(fsys fs.FS) (Package, error) {
	manifestBody, err := readPackageFile(fsys, packageManifestName, maxManifestBytes)
	if err != nil {
		return Package{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(manifestBody))
	decoder.DisallowUnknownFields()
	var declared packageManifest
	if err := decoder.Decode(&declared); err != nil {
		return Package{}, fmt.Errorf("%w: %s: %v", creativeops.ErrValidation, packageManifestName, err)
	}
	instructions, err := readPackageFile(fsys, packageBodyName, maxInstructionBytes)
	if err != nil {
		return Package{}, err
	}
	pkg := Package{
		Slug: declared.Key, DisplayName: declared.DisplayName, Description: declared.Description,
		Instructions: string(instructions),
		Manifest: Manifest{
			ManifestSchemaVersion: declared.ManifestSchemaVersion, InputKinds: declared.InputKinds,
			MaxInputs: declared.MaxInputs, ToolAllowlist: declared.ToolAllowlist,
			RequiredModelCapabilities: declared.RequiredModelCapabilities,
			OutputContract:            declared.OutputContract, CompletionCheckKey: declared.CompletionCheckKey,
		},
		bodies: map[string][]byte{},
	}
	// The package total is checked against the same budget BeginImport enforces,
	// and starts from the two files already read. This is a fail-fast guard, not
	// a second authority: the manifest is re-encoded before the real check, so
	// the byte count there can differ slightly from the file on disk.
	budget := len(instructions) + len(manifestBody)
	if err := fs.WalkDir(fsys, ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if strings.HasPrefix(entry.Name(), ".") && name != "." {
			return fmt.Errorf("%w: %q is hidden; a package publishes only what an operator can see", creativeops.ErrValidation, name)
		}
		if entry.IsDir() {
			return nil
		}
		// A symlink reads as a regular file through fs.ReadFile, so its target's
		// bytes would be published under an innocent relative path.
		if !entry.Type().IsRegular() {
			return fmt.Errorf("%w: %q is not a regular file", creativeops.ErrValidation, name)
		}
		if name == packageManifestName || name == packageBodyName {
			return nil
		}
		if !validResourcePath(name) {
			return fmt.Errorf("%w: resource path %q", creativeops.ErrValidation, name)
		}
		if len(pkg.Resources) == maxResourceCount {
			return fmt.Errorf("%w: more than %d resources, starting at %q", ErrLimit, maxResourceCount, name)
		}
		body, err := readPackageFile(fsys, name, maxResourceBytes)
		if err != nil {
			return err
		}
		// Caught here rather than at BeginImport, which knows the rule but not
		// which file broke it. The same goes for the limits above and below: a
		// mistyped --package-dir is an ordinary operator error, and it should
		// cost one stat call, not a directory read into memory.
		if len(body) == 0 {
			return fmt.Errorf("%w: %q is empty", creativeops.ErrValidation, name)
		}
		// This phase publishes UTF-8 reference text and nothing else. The bytes
		// decide that, never the extension: a declared mime is a claim about a
		// file, and a package that could widen the accepted types by renaming
		// one would make the limit decorative.
		if !utf8.Valid(body) {
			return fmt.Errorf("%w: %q is not UTF-8 text", creativeops.ErrValidation, name)
		}
		if budget += len(body); budget > maxPackageBytes {
			return fmt.Errorf("%w: the package passes %d bytes at %q", ErrLimit, maxPackageBytes, name)
		}
		sum := sha256.Sum256(body)
		pkg.Resources = append(pkg.Resources, ResourceDeclaration{
			Path: name, Mime: resourceMime(name), ByteSize: int64(len(body)),
			SHA256: hex.EncodeToString(sum[:]),
		})
		pkg.bodies[name] = body
		return nil
	}); err != nil {
		return Package{}, err
	}
	// WalkDir visits lexically, so the declarations are already ordered; the
	// digest does not depend on it, but a stable read makes two runs over one
	// directory produce byte-identical requests.
	return pkg, nil
}

// readPackageFile reads one file after checking the size the directory already
// reports. Reading first and checking afterwards would let a mistyped
// --package-dir pull an arbitrarily large file into memory before any limit
// applied, and the operator would pay for it before being told the name of the
// file that was too big.
func readPackageFile(fsys fs.FS, name string, limit int64) ([]byte, error) {
	info, err := fs.Stat(fsys, name)
	if err != nil {
		return nil, fmt.Errorf("%w: read %s: %v", creativeops.ErrValidation, name, err)
	}
	if info.Size() > limit {
		return nil, fmt.Errorf("%w: %q is %d bytes, over the %d limit", ErrLimit, name, info.Size(), limit)
	}
	body, err := fs.ReadFile(fsys, name)
	if err != nil {
		return nil, fmt.Errorf("%w: read %s: %v", creativeops.ErrValidation, name, err)
	}
	// The stat above is advice, not a guarantee: the file may have grown between
	// the two calls. BeginImport re-checks the declaration it is handed, and this
	// keeps the two answers from disagreeing.
	if int64(len(body)) > limit {
		return nil, fmt.Errorf("%w: %q is %d bytes, over the %d limit", ErrLimit, name, len(body), limit)
	}
	return body, nil
}

// resourceMime keeps the package format from carrying a per-file content type
// an operator could get wrong. A skill resource is instructions for a model to
// read, so the set of types worth distinguishing is small and closed.
//
// There is no binary case. Every resource this phase accepts is UTF-8 text, so
// an unrecognised extension is text of an unknown flavour, not an opaque blob;
// declaring it application/octet-stream would describe bytes the importer has
// already refused to accept.
func resourceMime(name string) string {
	switch path.Ext(name) {
	case ".md":
		return "text/markdown; charset=utf-8"
	case ".json":
		return "application/json"
	default:
		return "text/plain; charset=utf-8"
	}
}
