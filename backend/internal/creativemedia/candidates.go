package creativemedia

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/samson/customer-manage-platform/backend/internal/creativecanvas"
	"github.com/samson/customer-manage-platform/backend/internal/creativecontent"
	"github.com/samson/customer-manage-platform/backend/internal/creativelibrary"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativegraph"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

const candidateColumns = "id,upload_id,state,revision,reason,declared_kind,original_name,content_revision_id,target_snapshot,expires_at,adopted_target_kind,adopted_target_id,adopted_target_revision,created_at"

func scanCandidate(row store.Row) (Candidate, error) {
	var c Candidate
	var revision int64
	var kind, id *string
	var targetRevision *int64
	err := row.Scan(&c.ID, &c.UploadID, &c.State, &revision, &c.Reason, &c.Kind, &c.FileName, &c.ContentRevisionID, &c.Target, &c.ExpiresAt, &kind, &id, &targetRevision, &c.CreatedAt)
	if errors.Is(err, store.ErrNoRows) {
		return c, ErrNotFound
	}
	if err != nil {
		return c, err
	}
	c.Revision = creativeops.Revision(revision)
	if kind != nil && id != nil {
		p := Publication{Kind: *kind, ID: *id, Revision: 1}
		if targetRevision != nil {
			p.Revision = creativeops.Revision(*targetRevision)
		}
		c.Adopted = &p
	}
	return c, nil
}

func (s *Service) ListCandidates(ctx context.Context, scope store.AccountScope) (CandidatePage, error) {
	page := CandidatePage{Items: []Candidate{}}
	err := scope.WithReadSnapshot(ctx, func(tx store.ReadTxAccountScope) error {
		if err := tx.RequireCreativeRead(ctx); err != nil {
			return err
		}
		rows, err := tx.QueryPage(ctx, "creative_upload_candidates", candidateColumns, "state='pending' AND expires_at>clock_timestamp()", []store.OrderBy{{Column: "created_at", Desc: true}, {Column: "id", Desc: true}}, 100, 0)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			c, err := scanCandidate(rows)
			if err != nil {
				return err
			}
			page.Items = append(page.Items, c)
		}
		return rows.Err()
	})
	return page, err
}

func lockCandidate(ctx context.Context, tx store.TxAccountScope, id string) (Candidate, error) {
	return scanCandidate(tx.QueryRowForUpdate(ctx, "creative_upload_candidates", candidateColumns, "id=$2", id))
}

// AdoptCandidate binds a pending candidate's revision to a (possibly new)
// target. Adoption, discard and expiry lock the same row; only one wins.
func (s *Service) AdoptCandidate(ctx context.Context, scope store.AccountScope, c creativeops.Command) (creativeops.Receipt, error) {
	return run(ctx, scope, "media.adopt_candidate", c, func(v CandidateInput) error {
		if v.CandidateID == "" || v.CandidateRevision < 1 {
			return creativeops.ErrValidation
		}
		if v.Target != nil {
			return v.Target.validate()
		}
		return nil
	}, func(ctx context.Context, tx store.TxAccountScope, v CandidateInput) (creativeops.Outcome, error) {
		// Library root first so asset targets never lock canvas before library.
		if _, err := creativelibrary.LockLibraryInTx(ctx, tx); err != nil {
			return creativeops.Outcome{}, err
		}
		candidate, err := lockCandidate(ctx, tx, v.CandidateID)
		if err != nil {
			return creativeops.Outcome{}, err
		}
		if candidate.Revision != v.CandidateRevision {
			return creativeops.Outcome{}, creativecanvas.ErrVersionConflict
		}
		now, err := tx.CreativeNow(ctx)
		if err != nil {
			return creativeops.Outcome{}, err
		}
		if candidate.State != "pending" || !now.Before(candidate.ExpiresAt) || candidate.ContentRevisionID == nil {
			return creativeops.Outcome{}, ErrState
		}
		target := Target{}
		if v.Target != nil {
			target = *v.Target
		} else if err := json.Unmarshal(candidate.Target, &target); err != nil {
			return creativeops.Outcome{}, err
		}
		if err := validateTargetInTx(ctx, tx, target, candidate.Kind); err != nil {
			if errors.Is(err, creativecanvas.ErrTargetChanged) {
				return creativeops.Outcome{}, creativecanvas.ErrVersionConflict
			}
			return creativeops.Outcome{}, err
		}
		r, err := creativecontent.RequireUsable(ctx, tx, *candidate.ContentRevisionID, "display")
		if err != nil {
			return creativeops.Outcome{}, err
		}
		var publication Publication
		changeID := ""
		if target.Kind == "asset" {
			asset, err := creativelibrary.CreateFromRevisionInTx(ctx, tx, r, target.Asset.library())
			if err != nil {
				return creativeops.Outcome{}, err
			}
			publication = Publication{Kind: "asset", ID: asset.ID, Revision: asset.Revision}
		} else {
			change, err := creativecanvas.BindNodeRevisionInTx(ctx, tx, target.Node.canvas(), r, c.OperationID)
			if errors.Is(err, creativecanvas.ErrTargetChanged) {
				return creativeops.Outcome{}, creativecanvas.ErrVersionConflict
			}
			if err != nil {
				return creativeops.Outcome{}, err
			}
			publication = Publication{Kind: "node", ID: target.Node.NodeID, Revision: change.ResultRevision}
			changeID = change.ChangeID
		}
		var adoptedChange *string
		if changeID != "" {
			adoptedChange = &changeID
		}
		if _, err := tx.Update(ctx, "creative_upload_candidates", "state='applied',content_id=NULL,content_revision_id=NULL,adopted_change_id=$2,adopted_target_kind=$3,adopted_target_id=$4,adopted_target_revision=$5,target_snapshot=$6,revision=revision+1,updated_at=clock_timestamp()", "id=$7", adoptedChange, publication.Kind, publication.ID, int64(publication.Revision), creativegraph.Canonical(target), candidate.ID); err != nil {
			return creativeops.Outcome{}, err
		}
		candidate.State, candidate.Revision, candidate.Adopted, candidate.ContentRevisionID = "applied", candidate.Revision+1, &publication, nil
		return outcome(200, "candidate", candidate.ID, candidate.Revision, candidate)
	})
}

// DiscardCandidate releases the candidate root; the revision becomes eligible
// for future cleanup when nothing else references it.
func (s *Service) DiscardCandidate(ctx context.Context, scope store.AccountScope, c creativeops.Command) (creativeops.Receipt, error) {
	return run(ctx, scope, "media.discard_candidate", c, func(v CandidateInput) error {
		if v.CandidateID == "" || v.CandidateRevision < 1 || v.Target != nil {
			return creativeops.ErrValidation
		}
		return nil
	}, func(ctx context.Context, tx store.TxAccountScope, v CandidateInput) (creativeops.Outcome, error) {
		candidate, err := lockCandidate(ctx, tx, v.CandidateID)
		if err != nil {
			return creativeops.Outcome{}, err
		}
		if candidate.Revision != v.CandidateRevision {
			return creativeops.Outcome{}, creativecanvas.ErrVersionConflict
		}
		if candidate.State != "pending" {
			return creativeops.Outcome{}, ErrState
		}
		if _, err := tx.Update(ctx, "creative_upload_candidates", "state='discarded',content_id=NULL,content_revision_id=NULL,revision=revision+1,updated_at=clock_timestamp()", "id=$2", candidate.ID); err != nil {
			return creativeops.Outcome{}, err
		}
		candidate.State, candidate.Revision, candidate.ContentRevisionID = "discarded", candidate.Revision+1, nil
		return outcome(200, "candidate", candidate.ID, candidate.Revision, candidate)
	})
}
