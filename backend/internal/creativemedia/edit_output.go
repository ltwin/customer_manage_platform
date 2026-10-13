package creativemedia

import (
	"context"
	"io"
	"os"

	"github.com/google/uuid"
	"github.com/samson/customer-manage-platform/backend/internal/creativecontent"
	"github.com/samson/customer-manage-platform/backend/internal/creativepalette"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// EditSource is the exact original object an image edit renders from.
type EditSource struct {
	Mime   string
	Size   int64
	SHA256 string
}

// OpenEditSource opens the original object of an image revision at its
// recorded version. The edit candidate holds the revision for GC, so no read
// pin is taken; the caller closes the body.
func (s *Service) OpenEditSource(ctx context.Context, scope store.AccountScope, revisionID string) (io.ReadCloser, EditSource, error) {
	var src EditSource
	var key, version string
	err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		rev, err := creativecontent.RequireUsable(ctx, tx, revisionID, "display")
		if err != nil {
			return err
		}
		if rev.Kind != "image" {
			return ErrUnsupported
		}
		blob := ""
		for _, m := range rev.Media {
			if m.Role == "original" {
				blob = m.BlobID
			}
		}
		if blob == "" {
			return ErrNotFound
		}
		return tx.QueryRow(ctx, "creative_blobs", "object_key,object_version,mime,byte_size,sha256", "id=$2 AND state='ready' AND storage_driver=$3 AND bucket=$4", blob, s.adapter.Driver(), s.adapter.Bucket()).Scan(&key, &version, &src.Mime, &src.Size, &src.SHA256)
	})
	if err != nil {
		return nil, EditSource{}, err
	}
	body, stat, err := s.adapter.OpenVersion(ctx, key, version, nil)
	if err != nil {
		return nil, EditSource{}, err
	}
	if stat.Version != version || stat.Size != src.Size {
		_ = body.Close()
		return nil, EditSource{}, ErrState
	}
	return body, src, nil
}

// EditOutput is a rendered edit result that passed the upload verifier, with
// blob identities allocated. Its fields are private so callers cannot forge
// the identities or the facts they bind later.
type EditOutput struct {
	account                         string
	original, display               string
	originalVersion, displayVersion string
	verified                        Verified
	staged                          string
}

// EditFacts are the verified properties a candidate reports.
type EditFacts struct {
	Mime   string `json:"mime"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

func (o *EditOutput) OriginalBlob() string { return o.original }
func (o *EditOutput) DisplayBlob() string  { return o.display }
func (o *EditOutput) Facts() EditFacts {
	f := EditFacts{Mime: o.verified.Mime, Size: o.verified.Size, SHA256: o.verified.SHA256}
	if o.verified.Width != nil && o.verified.Height != nil {
		f.Width, f.Height = *o.verified.Width, *o.verified.Height
	}
	return f
}

// Close removes the verifier's staged files.
func (o *EditOutput) Close() {
	_ = os.Remove(o.staged)
	if d := o.verified.Display; d != nil {
		_ = os.Remove(d.Path)
	}
}

// VerifyEditOutput runs a rendered file through the same verifier as an
// upload: sniffed type, full decode, palette and display rendition. The
// renderer's own report is not trusted.
func (s *Service) VerifyEditOutput(ctx context.Context, scope store.AccountScope, path, mime string) (*EditOutput, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	verified, staged, err := s.verifier.Verify(ctx, f, "image", mime, s.cfg.ImageMaxBytes)
	if err != nil {
		return nil, err
	}
	o := &EditOutput{account: scope.AccountID(), original: "ccbl_" + uuid.NewString(), verified: verified, staged: staged}
	if verified.Display != nil {
		o.display = "ccbl_" + uuid.NewString()
	}
	return o, nil
}

// InsertEditBlobsInTx records the output's blobs as verifying before any
// object is written, so an interrupted write can still be found and
// reclaimed. The caller holds the account write barrier.
func (s *Service) InsertEditBlobsInTx(ctx context.Context, tx store.TxAccountScope, o *EditOutput) error {
	if o.account != tx.AccountID() {
		return ErrState
	}
	v := o.verified
	if err := tx.Insert(ctx, "creative_blobs", []string{"id", "storage_driver", "bucket", "object_key", "sha256", "byte_size", "mime", "width", "height", "codec_metadata", "state"}, o.original, s.adapter.Driver(), s.adapter.Bucket(), generatedObjectKey(o.account, o.original, "original"), v.SHA256, v.Size, v.Mime, v.Width, v.Height, v.Codecs, "verifying"); err != nil {
		return err
	}
	if d := v.Display; d != nil {
		return tx.Insert(ctx, "creative_blobs", []string{"id", "storage_driver", "bucket", "object_key", "sha256", "byte_size", "mime", "width", "height", "state"}, o.display, s.adapter.Driver(), s.adapter.Bucket(), generatedObjectKey(o.account, o.display, "display"), d.SHA256, d.Size, d.Mime, d.Width, d.Height, "verifying")
	}
	return nil
}

// PublishEditBlobs writes the objects outside any transaction and records
// each version while the blob is still verifying. A blob abandoned meanwhile
// is no longer verifying, and the write reports ErrState.
func (s *Service) PublishEditBlobs(ctx context.Context, scope store.AccountScope, o *EditOutput) error {
	publish := func(blob, role, file, mime string, size int64) (string, error) {
		f, err := os.Open(file)
		if err != nil {
			return "", err
		}
		defer func() { _ = f.Close() }()
		version, err := s.adapter.PublishVerified(ctx, generatedObjectKey(o.account, blob, role), f, size, mime)
		if err != nil {
			return "", err
		}
		return version, scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
			n, err := tx.Update(ctx, "creative_blobs", "object_version=$3", "id=$2 AND state='verifying'", blob, version)
			if err == nil && n != 1 {
				err = ErrState
			}
			return err
		})
	}
	var err error
	if o.originalVersion, err = publish(o.original, "original", o.staged, o.verified.Mime, o.verified.Size); err != nil {
		return err
	}
	if d := o.verified.Display; d != nil {
		o.displayVersion, err = publish(o.display, "display", d.Path, d.Mime, d.Size)
	}
	return err
}

// ReadyEditBlobsInTx makes the published blobs readable without booking
// quota: an edit result is only charged when applied. Unheld, they fall to
// GC's never-booked grace.
func (s *Service) ReadyEditBlobsInTx(ctx context.Context, tx store.TxAccountScope, o *EditOutput) error {
	for _, b := range []struct{ id, version string }{{o.original, o.originalVersion}, {o.display, o.displayVersion}} {
		if b.id == "" {
			continue
		}
		n, err := tx.Update(ctx, "creative_blobs", "state='ready',verified_at=clock_timestamp()", "id=$2 AND object_version=$3 AND state='verifying'", b.id, b.version)
		if err != nil {
			return err
		}
		if n != 1 {
			return ErrState
		}
	}
	return creativepalette.WriteInTx(ctx, tx, o.original, o.verified.Palette)
}

// AbandonEditBlobsInTx hands verifying blobs that will never become ready
// to GC. The grace outlasts a write still in flight, which then fails its
// version update instead of leaving an object behind a deleted row. Ready
// blobs need nothing: without a holder GC reclaims them anyway.
func AbandonEditBlobsInTx(ctx context.Context, tx store.TxAccountScope, ids ...string) error {
	live := make([]string, 0, len(ids))
	for _, id := range ids {
		if id != "" {
			live = append(live, id)
		}
	}
	if len(live) == 0 {
		return nil
	}
	now, err := tx.CreativeNow(ctx)
	if err != nil {
		return err
	}
	_, err = tx.Update(ctx, "creative_blobs", "state='deleting',delete_after=$3", "id=ANY($2::text[]) AND state='verifying'", live, now.Add(gcUnbookedGrace))
	return err
}
