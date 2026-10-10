package creativemedia

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	_ "image/png"
	"io"
	"time"

	"golang.org/x/image/draw"

	"github.com/samson/customer-manage-platform/backend/internal/creativecontent"
	"github.com/samson/customer-manage-platform/backend/internal/platform/llmgateway"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// ImagePreviewLimitError carries the original size and the preview ceiling so
// callers can explain an omitted image without failing the conversation.
type ImagePreviewLimitError struct {
	SizeBytes  int64
	LimitBytes int64
}

func (e *ImagePreviewLimitError) Error() string {
	return fmt.Sprintf("image of %d bytes cannot fit preview limit of %d bytes", e.SizeBytes, e.LimitBytes)
}

func (e *ImagePreviewLimitError) Unwrap() error { return ErrSizeLimit }

// AgentImage binds the display rendition of an immutable revision. The caller
// checks egress consent; this port checks account ownership and display rights.
// Media larger than maxBytes is sent as a downscaled JPEG preview. The usual media reader pins the exact object version.
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
		if media.ByteSize <= 0 {
			return llmgateway.ErrValidation
		}
		return tx.QueryRow(ctx, "creative_blobs", "sha256", "id=$2 AND state='ready'", media.BlobID).Scan(&digest)
	})
	if err != nil {
		return nil, err
	}
	open := func(ctx context.Context) (io.ReadCloser, error) {
		token := s.tickets.sign(ticketClaims{Account: scope.AccountID(), Revision: revisionID, Role: media.Role, Purpose: "display", ExpiresAt: time.Now().Add(s.cfg.TicketTTL).Unix()})
		stream, err := s.Open(ctx, func(string) store.AccountScope { return scope }, token, revisionID, media.Role, nil)
		if err != nil {
			return nil, err
		}
		return stream.Body, nil
	}
	if media.ByteSize <= maxBytes {
		return &llmgateway.ImageInput{RevisionID: revisionID, Digest: digest, MIME: media.Mime, ByteSize: media.ByteSize, Open: open}, nil
	}
	preview, err := agentPreview(ctx, open, media.ByteSize, maxBytes)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(preview)
	return &llmgateway.ImageInput{
		RevisionID: revisionID, Digest: "sha256-" + hex.EncodeToString(sum[:]), MIME: "image/jpeg", ByteSize: int64(len(preview)),
		Open: func(context.Context) (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(preview)), nil },
	}, nil
}

// agentPreviewSides are the longest edges tried for an image too large to send
// as stored. Vision models downscale to about 2048 px themselves, so a smaller
// copy loses nothing the model would have seen.
var agentPreviewSides = []int{2048, 1536, 1024}

// agentPreview re-encodes an oversized image as a JPEG within maxBytes.
// Transparent areas are laid on white, the way the canvas shows them.
func agentPreview(ctx context.Context, open func(context.Context) (io.ReadCloser, error), size, maxBytes int64) ([]byte, error) {
	reader, err := open(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = reader.Close() }()
	src, _, err := image.Decode(io.LimitReader(reader, size))
	if err != nil {
		return nil, ErrUnsupported
	}
	b := src.Bounds()
	for _, side := range agentPreviewSides {
		w, h := b.Dx(), b.Dy()
		if longest := max(w, h); longest > side {
			w, h = max(1, w*side/longest), max(1, h*side/longest)
		}
		dst := image.NewRGBA(image.Rect(0, 0, w, h))
		draw.Draw(dst, dst.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
		draw.CatmullRom.Scale(dst, dst.Bounds(), src, b, draw.Over, nil)
		var out bytes.Buffer
		if err = jpeg.Encode(&out, dst, &jpeg.Options{Quality: 85}); err != nil {
			return nil, err
		}
		if int64(out.Len()) <= maxBytes {
			return out.Bytes(), nil
		}
	}
	return nil, &ImagePreviewLimitError{SizeBytes: size, LimitBytes: maxBytes}
}
