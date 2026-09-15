package creativeskill

import (
	"fmt"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
	"github.com/samson/customer-manage-platform/backend/internal/platform/config"
	"github.com/samson/customer-manage-platform/backend/internal/platform/immutablefs"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
	"github.com/samson/customer-manage-platform/backend/internal/platform/versionedfs"
)

// Compose builds the skill service for the configured storage driver. Skill
// resources keep their own local root rather than sharing the media one: media
// owns an expiry sweep over its root, and a frozen skill version is retained
// until something is entitled to decide otherwise.
//
// The platform publisher's scope is built once, here, from the configured
// account. Composition is where a deployment fact belongs; a request can never
// name that account, and no caller is ever handed the scope.
func Compose(cfg config.Config, db *store.Store) (*Service, error) {
	if db == nil {
		return nil, fmt.Errorf("creative skill service requires a database")
	}
	var platform store.AccountScope
	if cfg.CreativePlatformAccountID != "" {
		platform = db.ScopeFor(auth.AccountContext{AccountID: cfg.CreativePlatformAccountID})
	}
	switch cfg.AvatarStorageDriver {
	case config.StorageDriverOSS:
		client, err := immutablefs.NewOSSClient(immutablefs.OSSConfig{
			Region: cfg.OSSRegion, Endpoint: cfg.OSSEndpoint, Bucket: cfg.OSSBucket, UseCName: cfg.OSSUseCName,
		})
		if err != nil {
			return nil, err
		}
		adapter, err := versionedfs.NewOSS(client, cfg.OSSBucket)
		if err != nil {
			return nil, err
		}
		return NewService(adapter, platform)
	case config.StorageDriverLocal:
		// The part signer exists for browser uploads and is unreachable from
		// this domain: ObjectPort has no multipart surface, so nothing here can
		// call AuthorizePart. A zero authorization is the honest answer for a
		// capability this service never hands out.
		adapter, err := versionedfs.NewLocal(cfg.CreativeSkillLocalRoot,
			func(string, string, int, time.Time) versionedfs.PartAuthorization {
				return versionedfs.PartAuthorization{}
			})
		if err != nil {
			return nil, err
		}
		return NewService(adapter, platform)
	default:
		return nil, fmt.Errorf("unsupported creative skill driver %q", cfg.AvatarStorageDriver)
	}
}
