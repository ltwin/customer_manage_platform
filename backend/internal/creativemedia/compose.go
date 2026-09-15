package creativemedia

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
	"github.com/samson/customer-manage-platform/backend/internal/platform/config"
	"github.com/samson/customer-manage-platform/backend/internal/platform/immutablefs"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
	"github.com/samson/customer-manage-platform/backend/internal/platform/versionedfs"
)

const (
	ticketBasePath = "/api/v1/creative/media"
	partPath       = "/api/v1/creative/media-parts"
)

// Compose builds the media service for the configured storage driver. Both
// the API and the worker call it so tickets, keys and limits agree.
func Compose(cfg config.Config, ticketKey []byte, logger *slog.Logger) (*Service, error) {
	mediaCfg := DefaultConfig()
	mediaCfg.FFProbe = cfg.CreativeFFProbe
	mediaCfg.QuotaLimitBytes = cfg.CreativeMediaQuotaBytes
	verifier := NewVerifier(mediaCfg)
	switch cfg.AvatarStorageDriver {
	case config.StorageDriverOSS:
		client, err := immutablefs.NewOSSClient(immutablefs.OSSConfig{Region: cfg.OSSRegion, Endpoint: cfg.OSSEndpoint, Bucket: cfg.OSSBucket, UseCName: cfg.OSSUseCName})
		if err != nil {
			return nil, err
		}
		adapter, err := versionedfs.NewOSS(client, cfg.OSSBucket)
		if err != nil {
			return nil, err
		}
		svc, err := NewService(mediaCfg, adapter, verifier, ticketKey, ticketBasePath)
		if err != nil {
			return nil, err
		}
		svc.SetLogger(logger)
		return svc, nil
	case config.StorageDriverLocal:
		// The local adapter needs the part signer, which needs the service key:
		// build a signer-only service first, then the real one.
		signerOnly, err := NewService(mediaCfg, signerAdapter{}, verifier, ticketKey, ticketBasePath)
		if err != nil {
			return nil, err
		}
		adapter, err := versionedfs.NewLocal(cfg.CreativeMediaLocalRoot, signerOnly.PartSigner(partPath))
		if err != nil {
			return nil, err
		}
		svc, err := NewService(mediaCfg, adapter, verifier, ticketKey, ticketBasePath)
		if err != nil {
			return nil, err
		}
		svc.SetLogger(logger)
		return svc, nil
	default:
		return nil, fmt.Errorf("unsupported creative media driver %q", cfg.AvatarStorageDriver)
	}
}

type signerAdapter struct{ versionedfs.Adapter }

func (signerAdapter) Driver() string { return "local" }
func (signerAdapter) Bucket() string { return "" }

// SweepAllAccounts runs the expiry sweep for every active account.
func (s *Service) SweepAllAccounts(ctx context.Context, db *store.Store, limit int) error {
	ids, err := db.ActiveAccountIDs(ctx, 10000)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if _, err := s.SweepExpired(ctx, db.ScopeFor(auth.AccountContext{AccountID: id}), limit); err != nil {
			return fmt.Errorf("account %s: %w", id, err)
		}
	}
	return nil
}
