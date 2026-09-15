package versionedfs_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/versionedfs"
	"github.com/samson/customer-manage-platform/backend/internal/platform/versionedfs/versionedfstest"
)

// The port has two consumers with different needs — media blobs staged by a
// browser, skill resources published by an administrator — so the contract is
// asserted once, here, rather than inside whichever domain happened to exercise
// it. The subjects are the two adapters registered below; the OSS adapter needs
// credentials this suite does not have, so the properties most likely to
// diverge there (version identity, the declared-size check) are still unproven
// against it. See the note on VersionIdentity for what OSS is expected to be.
func TestLocalAdapterConformsToThePort(t *testing.T) {
	versionedfstest.RunAdapterSuite(t, versionedfstest.ContentAddressed, func(t *testing.T) versionedfs.Adapter {
		adapter, err := versionedfs.NewLocal(filepath.Join(t.TempDir(), "objects"),
			func(string, string, int, time.Time) versionedfs.PartAuthorization {
				return versionedfs.PartAuthorization{}
			})
		if err != nil {
			t.Fatal(err)
		}
		return adapter
	})
}

func TestMemoryAdapterConformsToThePort(t *testing.T) {
	versionedfstest.RunAdapterSuite(t, versionedfstest.PerWrite, func(*testing.T) versionedfs.Adapter {
		return versionedfstest.NewMemory("memory")
	})
}
