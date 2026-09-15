package creativeskill

import (
	"fmt"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/config"
	"github.com/samson/customer-manage-platform/backend/internal/platform/immutablefs"
	"github.com/samson/customer-manage-platform/backend/internal/platform/versionedfs"
)

// Compose builds the skill service for the configured storage driver. Skill
// resources keep their own local root rather than sharing the media one: media
// owns an expiry sweep over its root, and a frozen skill version is retained
// until something is entitled to decide otherwise.
func Compose(cfg config.Config) (*Service, error) {
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
		return NewService(adapter, cfg.CreativePlatformAccountID)
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
		return NewService(adapter, cfg.CreativePlatformAccountID)
	default:
		return nil, fmt.Errorf("unsupported creative skill driver %q", cfg.AvatarStorageDriver)
	}
}
