package creativeagent

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strconv"
	"strings"
)

//go:embed skillpkg
var skillFiles embed.FS

const (
	skillManifestName = "manifest.json"
	skillBodyName     = "SKILL.md"
	// A skill is instructions, not a data set. These bounds keep one package
	// from quietly consuming a run's whole input budget.
	maxSkillFileBytes    = 64 << 10
	maxSkillPackageBytes = 256 << 10
)

var ErrSkillUnavailable = errors.New("creative skill unavailable")

// SkillManifest is the platform's own declaration of a package. Eino's skill
// front matter cannot carry version, digest or permissions, so those stay here
// and the framework only ever sees the projection it needs.
type SkillManifest struct {
	ManifestSchemaVersion int      `json:"manifest_schema_version"`
	Key                   string   `json:"key"`
	Version               int      `json:"version"`
	DisplayName           string   `json:"display_name"`
	Description           string   `json:"description"`
	InputKinds            []string `json:"input_kinds"`
	MaxInputs             int      `json:"max_inputs"`
	// ToolAllowlist is an upper bound intersected with the photographer's own
	// action scope at execution time. A skill can never widen it.
	ToolAllowlist             []string `json:"tool_allowlist"`
	RequiredModelCapabilities []string `json:"required_model_capabilities"`
	OutputContract            string   `json:"output_contract"`
	CompletionCheckKey        string   `json:"completion_check_key"`
}

// SkillPackage is one immutable, versioned deployment artifact. Instructions
// and resources are fixed at load; a run holds this snapshot and never resolves
// to whatever the deployment published later.
type SkillPackage struct {
	Manifest     SkillManifest
	Digest       string
	Instructions string
	// Resources are the package's read-only references, keyed by the exact
	// relative path a run may ask for. Lookup by key is what prevents traversal.
	Resources map[string][]byte
}

func (p SkillPackage) ref() string {
	return p.Manifest.Key + "@" + strconv.Itoa(p.Manifest.Version)
}

// clone deep-copies every field a holder could write through. A SkillPackage is
// a value, but its map, byte slices and manifest slices are not: without this a
// caller could edit the registry's own instructions or widen a tool allowlist
// while the digest — the answer to "which text ran" — stayed the same.
func (p SkillPackage) clone() SkillPackage {
	p.Manifest.InputKinds = append([]string(nil), p.Manifest.InputKinds...)
	p.Manifest.ToolAllowlist = append([]string(nil), p.Manifest.ToolAllowlist...)
	p.Manifest.RequiredModelCapabilities = append([]string(nil), p.Manifest.RequiredModelCapabilities...)
	resources := make(map[string][]byte, len(p.Resources))
	for name, body := range p.Resources {
		resources[name] = append([]byte(nil), body...)
	}
	p.Resources = resources
	return p
}

// SkillRegistry is the deployment's fixed catalog. It is built once at startup
// so a package that fails validation stops the process instead of surfacing
// mid-run.
type SkillRegistry struct {
	packages map[string]SkillPackage
	refs     []string
}

func NewSkillRegistry(packages []SkillPackage) (*SkillRegistry, error) {
	registry := &SkillRegistry{packages: make(map[string]SkillPackage, len(packages))}
	for _, p := range packages {
		if err := validateSkill(p); err != nil {
			return nil, err
		}
		ref := p.ref()
		if existing, duplicate := registry.packages[ref]; duplicate {
			// Two artifacts claiming one identity is a release accident. Taking
			// either one would make "which text ran" unanswerable afterwards.
			if existing.Digest != p.Digest {
				return nil, fmt.Errorf("%w: %s published twice with digests %s and %s", ErrSkillUnavailable, ref, existing.Digest, p.Digest)
			}
			return nil, fmt.Errorf("%w: %s declared twice", ErrSkillUnavailable, ref)
		}
		// Cloned on the way in as well: the caller still holds the slice it built,
		// and a package published from it must not change afterwards.
		registry.packages[ref] = p.clone()
		registry.refs = append(registry.refs, ref)
	}
	sort.Strings(registry.refs)
	return registry, nil
}

func validateSkill(p SkillPackage) error {
	m := p.Manifest
	if m.ManifestSchemaVersion != 1 || m.Key == "" || m.Version < 1 || m.DisplayName == "" || m.Description == "" {
		return fmt.Errorf("%w: manifest identity", ErrSkillUnavailable)
	}
	if !skillKeyPattern(m.Key) {
		return fmt.Errorf("%w: skill key %q", ErrSkillUnavailable, m.Key)
	}
	if p.Digest == "" || p.Instructions == "" || m.CompletionCheckKey == "" {
		return fmt.Errorf("%w: %s is incomplete", ErrSkillUnavailable, p.ref())
	}
	if len(m.ToolAllowlist) == 0 || m.MaxInputs < 1 {
		return fmt.Errorf("%w: %s declares no work it can do", ErrSkillUnavailable, p.ref())
	}
	total := len(p.Instructions)
	for name, body := range p.Resources {
		if name != path.Clean(name) || path.IsAbs(name) || strings.HasPrefix(name, "..") {
			return fmt.Errorf("%w: %s resource %q", ErrSkillUnavailable, p.ref(), name)
		}
		if len(body) > maxSkillFileBytes {
			return fmt.Errorf("%w: %s resource %q exceeds the file bound", ErrSkillUnavailable, p.ref(), name)
		}
		total += len(body)
	}
	if total > maxSkillPackageBytes {
		return fmt.Errorf("%w: %s exceeds the package bound", ErrSkillUnavailable, p.ref())
	}
	return nil
}

func skillKeyPattern(key string) bool {
	if len(key) == 0 || len(key) > 64 || key[0] < 'a' || key[0] > 'z' {
		return false
	}
	for _, r := range key {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' {
			return false
		}
	}
	return true
}

// Get resolves a package the run already fixed. Callers pass the version that
// was frozen at run creation, never "latest". The copy handed out is theirs to
// keep; editing it cannot reach the published package.
func (r *SkillRegistry) Get(key string, version int) (SkillPackage, error) {
	p, err := r.lookup(key, version)
	if err != nil {
		return SkillPackage{}, err
	}
	return p.clone(), nil
}

// lookup returns the stored package itself. In-package callers may only read
// through it; everything that leaves the registry goes through a clone.
func (r *SkillRegistry) lookup(key string, version int) (SkillPackage, error) {
	p, ok := r.packages[key+"@"+strconv.Itoa(version)]
	if !ok {
		return SkillPackage{}, fmt.Errorf("%w: %s@%d", ErrSkillUnavailable, key, version)
	}
	return p, nil
}

// Resource reads one declared file of a fixed package. An undeclared path is
// refused by key lookup; there is no filesystem underneath this call.
func (r *SkillRegistry) Resource(key string, version int, name string) ([]byte, error) {
	p, err := r.lookup(key, version)
	if err != nil {
		return nil, err
	}
	body, ok := p.Resources[name]
	if !ok {
		return nil, fmt.Errorf("%w: %s has no resource %q", ErrSkillUnavailable, p.ref(), name)
	}
	return append([]byte(nil), body...), nil
}

// packagesInOrder is the in-package listing seam. It hands back the stored
// values so a catalog request does not copy every resource byte to read a few
// manifest fields; callers project out of them and never write.
func (r *SkillRegistry) packagesInOrder() []SkillPackage {
	list := make([]SkillPackage, 0, len(r.refs))
	for _, ref := range r.refs {
		list = append(list, r.packages[ref])
	}
	return list
}

// LoadEmbeddedSkills reads the packages shipped with this binary. Publishing is
// a deployment act: nothing here reads a path chosen at runtime.
func LoadEmbeddedSkills() ([]SkillPackage, error) {
	dirs, err := fs.ReadDir(skillFiles, "skillpkg")
	if err != nil {
		return nil, err
	}
	var packages []SkillPackage
	for _, dir := range dirs {
		if !dir.IsDir() {
			continue
		}
		p, err := loadSkillDir(path.Join("skillpkg", dir.Name()))
		if err != nil {
			return nil, err
		}
		packages = append(packages, p)
	}
	return packages, nil
}

func loadSkillDir(root string) (SkillPackage, error) {
	p := SkillPackage{Resources: map[string][]byte{}}
	// The digest covers every byte that can reach a model, keyed by path, so a
	// moved or renamed reference changes the identity too.
	hashes := map[string]string{}
	err := fs.WalkDir(skillFiles, root, func(name string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		body, err := fs.ReadFile(skillFiles, name)
		if err != nil {
			return err
		}
		relative, err := relativeSkillPath(root, name)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(body)
		hashes[relative] = hex.EncodeToString(sum[:])
		switch relative {
		case skillManifestName:
			if err := json.Unmarshal(body, &p.Manifest); err != nil {
				return fmt.Errorf("%w: %s manifest: %v", ErrSkillUnavailable, root, err)
			}
		case skillBodyName:
			p.Instructions = string(body)
		default:
			p.Resources[relative] = body
		}
		return nil
	})
	if err != nil {
		return SkillPackage{}, err
	}
	p.Digest = digestOf(hashes)
	return p, nil
}

func relativeSkillPath(root, name string) (string, error) {
	relative := strings.TrimPrefix(name, root+"/")
	if relative == name || relative == "" {
		return "", fmt.Errorf("%w: %s is outside %s", ErrSkillUnavailable, name, root)
	}
	return relative, nil
}

func digestOf(hashes map[string]string) string {
	names := make([]string, 0, len(hashes))
	for name := range hashes {
		names = append(names, name)
	}
	sort.Strings(names)
	sum := sha256.New()
	for _, name := range names {
		// hash.Hash never reports a write error, and the separators keep two
		// different path/hash splits from folding into one digest.
		sum.Write([]byte(name + "\x00" + hashes[name] + "\x00"))
	}
	return hex.EncodeToString(sum.Sum(nil))
}
