package creativeskill

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// syncNamespace scopes the operation ids SyncPlatform derives. Changing it
// re-keys every future sync; versions already frozen are unaffected.
var syncNamespace = uuid.MustParse("b6f1d0a4-3c2e-4f8a-9d57-2e8c41a07f13")

// Synced reports one platform sync. Counts are never omitted, for the same
// reason as Reclaimed: "nothing changed" and "nothing could be read" must not
// look alike in a startup log.
type Synced struct {
	Published []string `json:"published"`
	Unchanged int      `json:"unchanged"`
	Failed    int      `json:"failed"`
}

// maxSyncAttempts bounds how many expired attempts one package may step past
// in a single sync. Each one is a whole import window that ran out, so reaching
// it means the store has been failing for weeks, not that the loop should go on.
const maxSyncAttempts = 32

// SyncPlatform makes the platform catalog follow the package directories under
// root: a package whose content differs from its skill's current version is
// published as a new version and activated; one that matches is left alone.
//
// The operation id is derived from the slug, the content digest, the revision
// being extended and an attempt number, so a sync that dies halfway replays on
// the next start instead of freezing a second version, and reverting a package
// to older content after the skill moved on still publishes. An attempt that
// outlived its import window is terminal; the next attempt number takes over.
// A package that is removed from the directory is not withdrawn here:
// withdrawing a platform skill is a decision about work already pinned to it,
// not a side effect of a deploy.
//
// Two directories naming one slug are both refused: published in turn they
// would take turns being current and append a version on every start.
//
// One package that cannot be read or published does not stop the others; the
// first failure is returned with the counts.
func (s *Service) SyncPlatform(ctx context.Context, root fs.FS) (Synced, error) {
	result := Synced{Published: []string{}}
	if s.platform.AccountID() == "" {
		return result, ErrOriginNotPermitted
	}
	entries, err := fs.ReadDir(root, ".")
	if err != nil {
		return result, err
	}
	var firstFailure error
	failed := func(err error) {
		result.Failed++
		if firstFailure == nil {
			firstFailure = err
		}
	}
	// Read everything first: a slug conflict is only visible across packages.
	var packages []syncPackage
	dirs := map[string][]string{}
	for _, entry := range entries {
		// Only directories are packages. A stray file next to them is not
		// something an operator meant to publish.
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		pkg, err := readSyncPackage(root, entry.Name())
		if err != nil {
			failed(fmt.Errorf("package %s: %w", entry.Name(), err))
			continue
		}
		packages = append(packages, pkg)
		dirs[pkg.Slug] = append(dirs[pkg.Slug], pkg.dir)
	}
	sort.Slice(packages, func(i, j int) bool { return packages[i].dir < packages[j].dir })
	for _, pkg := range packages {
		if same := dirs[pkg.Slug]; len(same) > 1 {
			failed(fmt.Errorf("%w: packages %s all declare slug %q",
				creativeops.ErrValidation, strings.Join(same, ", "), pkg.Slug))
			continue
		}
		published, err := s.syncPackage(ctx, pkg)
		switch {
		case err != nil:
			failed(fmt.Errorf("package %s: %w", pkg.dir, err))
		case published:
			result.Published = append(result.Published, pkg.Slug)
		default:
			result.Unchanged++
		}
	}
	return result, firstFailure
}

// syncPackage is one package directory, read and digested.
type syncPackage struct {
	Package
	dir    string
	digest string
}

func readSyncPackage(root fs.FS, dir string) (syncPackage, error) {
	sub, err := fs.Sub(root, dir)
	if err != nil {
		return syncPackage{}, err
	}
	pkg, err := ReadPackage(sub)
	if err != nil {
		return syncPackage{}, err
	}
	manifest, err := pkg.Manifest.encoded()
	if err != nil {
		return syncPackage{}, err
	}
	return syncPackage{
		Package: pkg, dir: dir,
		digest: contentDigest(pkg.DisplayName, pkg.Description, pkg.Instructions, manifest, pkg.Resources),
	}, nil
}

// syncPackage reports whether it published, as opposed to finding the skill
// already carrying this content.
func (s *Service) syncPackage(ctx context.Context, pkg syncPackage) (bool, error) {
	revision, current, err := s.platformSkillState(ctx, pkg.Slug)
	if err != nil {
		return false, err
	}
	if current == pkg.digest {
		return false, nil
	}
	// An expired attempt can never finish, so the next number takes over. A
	// live or finalized one replays, which is what keeps a retried start from
	// freezing the same content twice.
	for attempt := range maxSyncAttempts {
		operation := uuid.NewSHA1(syncNamespace, []byte(pkg.Slug+"\x00"+pkg.digest+"\x00"+
			strconv.FormatInt(int64(revision), 10)+"\x00"+strconv.Itoa(attempt)))
		_, err := s.Publish(ctx, s.platform, pkg.Package, PublishIntent{
			OperationID: operation.String(), Origin: "platform",
			ExpectedSkillRevision: revision, Activate: true,
		})
		if !errors.Is(err, ErrImportExpired) {
			return err == nil, err
		}
	}
	return false, fmt.Errorf("%w: %d attempts in a row ran out of their window", ErrImportExpired, maxSyncAttempts)
}

// platformSkillState reads the revision a new version would extend and the
// digest of the current version. A skill that does not exist yet reports zero
// and "", which is the "create" intent; one with no current version reports
// "" so that the package's content becomes current.
func (s *Service) platformSkillState(ctx context.Context, slug string) (creativeops.Revision, string, error) {
	var revision int64
	var digest string
	err := s.platform.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		var current *string
		err := tx.QueryRow(ctx, "creative_skills", "revision,current_version_id", "slug=$2", slug).
			Scan(&revision, &current)
		if errors.Is(err, store.ErrNoRows) {
			return nil
		}
		if err != nil || current == nil {
			return err
		}
		return tx.QueryRow(ctx, "creative_skill_versions", "digest", "id=$2", *current).Scan(&digest)
	})
	return creativeops.Revision(revision), digest, err
}
