package creativemedia

import (
	"context"
	"io"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/creativecontent"
	"github.com/samson/customer-manage-platform/backend/internal/platform/llmgateway"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// AgentImage binds the display rendition of an immutable revision. The caller
// checks egress consent; this port checks account ownership, display rights and
// bounded stored bytes. The usual media reader pins the exact object version.
func (s *Service) AgentImage(ctx context.Context, scope store.AccountScope, revisionID string, maxBytes int64) (*llmgateway.ImageInput, error) {
	var media creativecontent.MediaObject
	var digest string
	err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		if err := tx.RequireCreativeCapability(ctx, "creative_read"); err != nil {
			return err
		}
		rev, err := creativecontent.RequireUsable(ctx, tx, revisionID, "display")
		if err != nil {
			return err
		}
		if rev.Kind != "image" {
			return llmgateway.ErrCapability
		}
		for _, candidate := range rev.Media {
			if candidate.Role == "display" {
				media = candidate
				break
			}
			if candidate.Role == "original" {
				media = candidate
			}
		}
		if media.BlobID == "" {
			return creativecontent.ErrNotFound
		}
		if media.ByteSize <= 0 || media.ByteSize > maxBytes {
			return llmgateway.ErrValidation
		}
		return tx.QueryRow(ctx, "creative_blobs", "sha256", "id=$2 AND state='ready'", media.BlobID).Scan(&digest)
	})
	if err != nil {
		return nil, err
	}
	return &llmgateway.ImageInput{
		RevisionID: revisionID, Digest: digest, MIME: media.Mime, ByteSize: media.ByteSize,
		Open: func(ctx context.Context) (io.ReadCloser, error) {
			token := s.tickets.sign(ticketClaims{Account: scope.AccountID(), Revision: revisionID, Role: media.Role, Purpose: "display", ExpiresAt: time.Now().Add(s.cfg.TicketTTL).Unix()})
			stream, err := s.Open(ctx, func(string) store.AccountScope { return scope }, token, revisionID, media.Role, nil)
			if err != nil {
				return nil, err
			}
			return stream.Body, nil
		},
	}, nil
}
