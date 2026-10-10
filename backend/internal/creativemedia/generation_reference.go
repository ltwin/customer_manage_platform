package creativemedia

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"strings"

	"github.com/google/uuid"

	"github.com/samson/customer-manage-platform/backend/internal/creativecontent"
	"github.com/samson/customer-manage-platform/backend/internal/platform/llmgateway"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// GenerationSource 是画布按发送顺序给出的参考身份；字节与摘要由本包从存储读出。
type GenerationSource struct {
	RevisionID string
	Role       string
}

// GenerationReferencesInTx 把画布参考固定为Gateway参考：摘要、MIME和大小只取自
// 已就绪的original对象（文字取正文），并要求生成用途授权。顺序与重复条目保持原样。
func (s *Service) GenerationReferencesInTx(ctx context.Context, tx store.TxAccountScope, sources []GenerationSource) ([]llmgateway.GenerationReference, error) {
	out := make([]llmgateway.GenerationReference, 0, len(sources))
	for _, src := range sources {
		rev, err := creativecontent.RequireGenerationReferenceRow(ctx, tx, tx.QueryRow, src.RevisionID)
		if err != nil {
			return nil, err
		}
		ref := llmgateway.GenerationReference{Role: src.Role, RevisionID: src.RevisionID, Kind: llmgateway.GenerationKind(rev.Kind)}
		if rev.Kind == "text" {
			if rev.Payload.Body == nil {
				return nil, llmgateway.ErrConflict
			}
			sum := sha256.Sum256([]byte(*rev.Payload.Body))
			ref.Digest, ref.MIME, ref.ByteSize = hex.EncodeToString(sum[:]), "text/plain", int64(len(*rev.Payload.Body))
			out = append(out, ref)
			continue
		}
		for _, m := range rev.Media {
			if m.Role != "original" {
				continue
			}
			var digest string
			if err := tx.QueryRow(ctx, "creative_blobs", "sha256", "id=$2 AND state='ready'", m.BlobID).Scan(&digest); err != nil {
				return nil, err
			}
			ref.Digest, ref.MIME, ref.ByteSize = strings.TrimPrefix(digest, "sha256-"), m.Mime, m.ByteSize
		}
		if ref.Digest == "" {
			return nil, creativecontent.ErrNotFound
		}
		out = append(out, ref)
	}
	return out, nil
}

// GenerationReaderInTx 是事务内的可信事实端口，供准入预检在同一事务里读取，
// 不另开连接。语义与 GenerationReaderFor 相同。
func (s *Service) GenerationReaderInTx(tx store.TxAccountScope) llmgateway.GenerationMediaReader {
	return txGenerationReader{tx: tx}
}

type txGenerationReader struct{ tx store.TxAccountScope }

func (r txGenerationReader) ResolveGenerationMedia(ctx context.Context, ref llmgateway.GenerationReference) (llmgateway.GenerationMediaMetadata, error) {
	// 文字参考只有身份事实；正文已由画布编入提示词。
	if ref.Kind == llmgateway.GenerationText {
		rev, err := creativecontent.RequireGenerationReferenceRow(ctx, r.tx, r.tx.QueryRow, ref.RevisionID)
		if err != nil {
			return llmgateway.GenerationMediaMetadata{}, err
		}
		if rev.Kind != "text" || rev.Payload.Body == nil {
			return llmgateway.GenerationMediaMetadata{}, llmgateway.ErrConflict
		}
		sum := sha256.Sum256([]byte(*rev.Payload.Body))
		if hex.EncodeToString(sum[:]) != ref.Digest || ref.MIME != "text/plain" || int64(len(*rev.Payload.Body)) != ref.ByteSize {
			return llmgateway.GenerationMediaMetadata{}, llmgateway.ErrConflict
		}
		return llmgateway.GenerationMediaMetadata{RevisionID: ref.RevisionID, Digest: ref.Digest, ExtractorVersion: FactsExtractorVersion, Kind: ref.Kind, MIME: ref.MIME, ByteSize: ref.ByteSize}, nil
	}
	if err := validGenerationReference(ref); err != nil {
		return llmgateway.GenerationMediaMetadata{}, err
	}
	rev, err := creativecontent.RequireGenerationReferenceRow(ctx, r.tx, r.tx.QueryRow, ref.RevisionID)
	if err != nil {
		return llmgateway.GenerationMediaMetadata{}, err
	}
	return resolveGenerationMedia(ctx, r.tx, rev, ref)
}

// GenerationReferenceOpenerFor 绑定一个账号，按冻结身份打开参考original对象的字节，
// 供Adapter在Submit时外发。每次打开重查生成用途授权，并登记短期读pin防止对象被回收。
func (s *Service) GenerationReferenceOpenerFor(scope store.AccountScope) llmgateway.GenerationReferenceOpener {
	return &referenceOpener{svc: s, scope: scope}
}

type referenceOpener struct {
	svc   *Service
	scope store.AccountScope
}

func (o *referenceOpener) OpenGenerationReference(ctx context.Context, ref llmgateway.GenerationReference) (io.ReadCloser, error) {
	if err := validGenerationReference(ref); err != nil {
		return nil, err
	}
	var key, version string
	err := o.scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		rev, err := creativecontent.RequireGenerationReferenceRow(ctx, tx, tx.QueryRow, ref.RevisionID)
		if err != nil {
			return err
		}
		meta, err := resolveGenerationMedia(ctx, tx, rev, ref)
		if err != nil {
			return err
		}
		blobID := ""
		for _, m := range rev.Media {
			if m.Role == "original" {
				blobID = m.BlobID
			}
		}
		var objectVersion *string
		if err := tx.QueryRow(ctx, "creative_blobs", "object_key,object_version", "id=$2 AND state='ready'", blobID).Scan(&key, &objectVersion); err != nil {
			return err
		}
		if objectVersion == nil || meta.ByteSize != ref.ByteSize {
			return creativecontent.ErrNotFound
		}
		version = *objectVersion
		now, err := tx.CreativeNow(ctx)
		if err != nil {
			return err
		}
		return tx.Insert(ctx, "creative_blob_read_pins", []string{"id", "blob_id", "owner_kind", "expires_at"}, "ccpn_"+uuid.NewString(), blobID, "processing", now.Add(o.svc.cfg.ReadPinTTL))
	})
	if err != nil {
		return nil, err
	}
	body, stat, err := o.svc.adapter.OpenVersion(ctx, key, version, nil)
	if err != nil {
		return nil, err
	}
	if stat.Size != ref.ByteSize {
		_ = body.Close()
		return nil, creativecontent.ErrNotFound
	}
	return body, nil
}
