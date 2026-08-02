package accountprofile

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/avatarmedia"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// Service 是账号资料唯一 mutation seam。
type Service struct {
	repo    Repository
	objects ObjectStore
	now     func() time.Time
}

func NewService(repo Repository, objects ObjectStore) *Service {
	return &Service{repo: repo, objects: objects, now: time.Now}
}

func (s *Service) WithClock(now func() time.Time) *Service {
	if now != nil {
		s.now = now
	}
	return s
}

func (s *Service) Get(ctx context.Context, scope store.AccountScope) (Profile, error) {
	profile, _, err := s.repo.Snapshot(ctx, scope)
	return profile, err
}

func (s *Service) Patch(
	ctx context.Context,
	scope store.AccountScope,
	expectedProfileRevision int64,
	input PatchInput,
) (Profile, error) {
	if expectedProfileRevision < 0 {
		return Profile{}, ValidationError{Message: "profile_revision 非法"}
	}
	normalized, clear, err := normalizeDisplayName(input.DisplayName)
	if err != nil {
		return Profile{}, err
	}
	var next *string
	if !clear {
		next = normalized
	}
	var result Profile
	err = scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		locked, exists, err := s.repo.Lock(ctx, tx)
		if err != nil {
			return err
		}
		if exists && sameOptionalDisplayName(locked.DisplayName, next) {
			result = locked
			return nil
		}
		if !exists {
			if expectedProfileRevision != 0 {
				return ErrProfileRevisionConflict
			}
			inserted, err := s.repo.InsertDisplayName(ctx, tx, next, s.now().UTC())
			if errors.Is(err, store.ErrNoRows) {
				updated, err := s.repo.UpdateDisplayName(ctx, tx, expectedProfileRevision, next, s.now().UTC())
				if err != nil {
					return err
				}
				result = updated
				return nil
			}
			if err != nil {
				return err
			}
			result = inserted
			return nil
		}
		if locked.ProfileRevision != expectedProfileRevision {
			return ErrProfileRevisionConflict
		}
		updated, err := s.repo.UpdateDisplayName(ctx, tx, expectedProfileRevision, next, s.now().UTC())
		if err != nil {
			return err
		}
		result = updated
		return nil
	})
	return result, err
}

func (s *Service) SetAvatar(
	ctx context.Context,
	scope store.AccountScope,
	expectedAvatarRevision int64,
	content Content,
) (Profile, error) {
	if expectedAvatarRevision < 0 {
		return Profile{}, ValidationError{Message: "avatar_revision 非法"}
	}
	snapshot, _, err := s.repo.Snapshot(ctx, scope)
	if err != nil {
		return Profile{}, err
	}
	if snapshot.AvatarVersion != nil && *snapshot.AvatarVersion == content.Checksum() {
		current, valid, err := s.verifySameContentCurrent(ctx, scope, expectedAvatarRevision, content.Checksum())
		if err != nil {
			return Profile{}, err
		}
		if valid {
			return current, nil
		}
	}
	return s.publishAndSwitch(ctx, scope, expectedAvatarRevision, content)
}

func (s *Service) RemoveAvatar(
	ctx context.Context,
	scope store.AccountScope,
	expectedAvatarRevision int64,
) (Profile, error) {
	if expectedAvatarRevision < 0 {
		return Profile{}, ValidationError{Message: "avatar_revision 非法"}
	}
	var result Profile
	err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		locked, exists, err := s.repo.Lock(ctx, tx)
		if err != nil {
			return err
		}
		if !exists || locked.AvatarVersion == nil {
			result = locked
			return nil
		}
		if locked.AvatarRevision != expectedAvatarRevision {
			return ErrAvatarRevisionConflict
		}
		oldRef := objectRefFromProfile(locked)
		updated, err := s.repo.ClearAvatarPointer(ctx, tx, expectedAvatarRevision, s.now().UTC())
		if err != nil {
			return err
		}
		if err := s.repo.EnqueueGC(ctx, tx, oldRef, s.now().UTC().Add(avatarGCGrace)); err != nil {
			return err
		}
		result = updated
		return nil
	})
	return result, err
}

func (s *Service) ReadAvatar(
	ctx context.Context,
	scope store.AccountScope,
	requestedVersion string,
) (Content, error) {
	current, _, err := s.repo.Snapshot(ctx, scope)
	if err != nil {
		return Content{}, err
	}
	if current.AvatarVersion == nil {
		return Content{}, ErrNotFound
	}
	if requestedVersion != *current.AvatarVersion {
		return Content{}, ErrAvatarVersionStale
	}
	content, valid, err := s.loadCurrentContent(ctx, scope.AccountID(), current)
	if err != nil {
		return Content{}, err
	}
	if !valid {
		return Content{}, ErrAvatarObjectIntegrity
	}
	return content, nil
}

func (s *Service) verifySameContentCurrent(
	ctx context.Context,
	scope store.AccountScope,
	expectedRevision int64,
	desiredChecksum string,
) (Profile, bool, error) {
	var current Profile
	var valid bool
	err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		locked, exists, err := s.repo.Lock(ctx, tx)
		if err != nil {
			return err
		}
		if !exists || locked.AvatarVersion == nil || *locked.AvatarVersion != desiredChecksum {
			return nil
		}
		valid, err = s.verifyCurrent(ctx, scope.AccountID(), locked)
		if err != nil {
			return err
		}
		if !valid && locked.AvatarRevision != expectedRevision {
			return ErrAvatarRevisionConflict
		}
		current = locked
		return nil
	})
	return current, valid, err
}

func (s *Service) publishAndSwitch(
	ctx context.Context,
	scope store.AccountScope,
	expectedRevision int64,
	content Content,
) (Profile, error) {
	for range maxFreshGenerationAttempts {
		objectID, err := newAvatarObjectID()
		if err != nil {
			return Profile{}, err
		}
		ref := ObjectRef{AvatarVersion: content.Checksum(), AvatarObjectID: objectID}
		key, err := avatarmedia.AccountProfileKey(scope.AccountID(), ref)
		if err != nil {
			return Profile{}, err
		}
		meta := ObjectMeta{
			MediaType:  content.MediaType(),
			Size:       content.Size(),
			Checksum:   content.Checksum(),
			ModifiedAt: s.now().UTC(),
		}
		published, err := s.putFreshGeneration(ctx, key, content, meta)
		if errors.Is(err, errRetryFreshGeneration) {
			continue
		}
		if err != nil {
			return Profile{}, err
		}
		meta = published.Meta

		var result Profile
		var orphan bool
		err = scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
			locked, exists, err := s.repo.Lock(ctx, tx)
			if err != nil {
				return err
			}
			burned, err := s.repo.GCExistsForUpdate(ctx, tx, objectID)
			if err != nil {
				return err
			}
			if burned {
				return errRetryFreshGeneration
			}
			stored, err := s.objects.Stat(ctx, key)
			if errors.Is(err, avatarmedia.ErrObjectNotFound) || errors.Is(err, avatarmedia.ErrObjectIntegrity) {
				return errRetryFreshGeneration
			}
			if err != nil {
				return err
			}
			if !avatarmedia.SameObjectMeta(stored, meta) {
				return errRetryFreshGeneration
			}

			if locked.AvatarVersion != nil && *locked.AvatarVersion == content.Checksum() {
				valid, err := s.verifyCurrent(ctx, scope.AccountID(), locked)
				if err != nil {
					return err
				}
				if valid {
					if err := s.repo.EnqueueGC(ctx, tx, ref, s.now().UTC().Add(avatarGCGrace)); err != nil {
						return err
					}
					result = locked
					return nil
				}
			}
			if exists && locked.AvatarRevision != expectedRevision {
				orphan = true
				return ErrAvatarRevisionConflict
			}
			if !exists {
				if expectedRevision != 0 {
					orphan = true
					return ErrAvatarRevisionConflict
				}
				inserted, err := s.repo.InsertAvatarPointer(ctx, tx, ref, meta)
				if errors.Is(err, store.ErrNoRows) {
					updated, err := s.repo.SwitchAvatarPointer(ctx, tx, expectedRevision, ref, meta)
					if err != nil {
						if errors.Is(err, ErrAvatarRevisionConflict) {
							orphan = true
						}
						return err
					}
					result = updated
					return nil
				}
				if err != nil {
					return err
				}
				result = inserted
				return nil
			}
			oldRef, hadOld := optionalObjectRefFromProfile(locked)
			updated, err := s.repo.SwitchAvatarPointer(ctx, tx, expectedRevision, ref, meta)
			if err != nil {
				if errors.Is(err, ErrAvatarRevisionConflict) {
					orphan = true
				}
				return err
			}
			if hadOld {
				if err := s.repo.EnqueueGC(ctx, tx, oldRef, s.now().UTC().Add(avatarGCGrace)); err != nil {
					return err
				}
			}
			result = updated
			return nil
		})
		if errors.Is(err, errRetryFreshGeneration) {
			continue
		}
		if orphan {
			_ = s.objects.Delete(ctx, key)
		}
		return result, err
	}
	return Profile{}, errors.New("avatar fresh generation attempts exhausted")
}

func (s *Service) putFreshGeneration(
	ctx context.Context,
	key avatarmedia.Key,
	content Content,
	meta ObjectMeta,
) (avatarmedia.PutResult, error) {
	result, err := s.objects.PutImmutable(ctx, key, content, meta)
	if errors.Is(err, ErrAvatarObjectTemporary) {
		result, err = s.objects.PutImmutable(ctx, key, content, meta)
		if err == nil && !result.Created && avatarmedia.SameObjectMeta(result.Meta, meta) {
			return result, nil
		}
	}
	if errors.Is(err, ErrAvatarObjectIntegrity) {
		return avatarmedia.PutResult{}, errRetryFreshGeneration
	}
	if err != nil {
		return avatarmedia.PutResult{}, err
	}
	if !result.Created {
		return avatarmedia.PutResult{}, errRetryFreshGeneration
	}
	if !avatarmedia.SameObjectMeta(result.Meta, meta) {
		return avatarmedia.PutResult{}, errRetryFreshGeneration
	}
	return result, nil
}

func (s *Service) verifyCurrent(ctx context.Context, accountID string, profile Profile) (bool, error) {
	_, valid, err := s.loadCurrentContent(ctx, accountID, profile)
	return valid, err
}

func (s *Service) loadCurrentContent(ctx context.Context, accountID string, profile Profile) (Content, bool, error) {
	ref, ok := optionalObjectRefFromProfile(profile)
	if !ok || profile.AvatarMediaType == nil || profile.AvatarSize == nil {
		return Content{}, false, nil
	}
	key, err := avatarmedia.AccountProfileKey(accountID, ref)
	if err != nil {
		return Content{}, false, err
	}
	meta, err := s.objects.Stat(ctx, key)
	if errors.Is(err, avatarmedia.ErrObjectNotFound) || errors.Is(err, avatarmedia.ErrObjectIntegrity) {
		return Content{}, false, nil
	}
	if err != nil {
		return Content{}, false, err
	}
	expected := ObjectMeta{MediaType: *profile.AvatarMediaType, Size: *profile.AvatarSize, Checksum: ref.AvatarVersion}
	if !avatarmedia.SameObjectMeta(meta, expected) {
		return Content{}, false, nil
	}
	stream, err := s.objects.Open(ctx, key)
	if errors.Is(err, avatarmedia.ErrObjectNotFound) || errors.Is(err, avatarmedia.ErrObjectIntegrity) {
		return Content{}, false, nil
	}
	if err != nil {
		return Content{}, false, err
	}
	raw, readErr := io.ReadAll(io.LimitReader(stream, avatarmedia.MaxContentBytes+1))
	closeErr := stream.Close()
	if readErr != nil {
		return Content{}, false, readErr
	}
	if closeErr != nil {
		return Content{}, false, closeErr
	}
	if int64(len(raw)) != expected.Size || len(raw) > avatarmedia.MaxContentBytes {
		return Content{}, false, nil
	}
	digest := sha256.Sum256(raw)
	if fmt.Sprintf("sha256-%x", digest) != expected.Checksum {
		return Content{}, false, nil
	}
	validated, ok := avatarmedia.ConfirmIntegrity(raw, expected.MediaType)
	if !ok {
		return Content{}, false, nil
	}
	return validated, true, nil
}

func newAvatarObjectID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", fmt.Errorf("generate avatar object id: %w", err)
	}
	return fmt.Sprintf("%x", value), nil
}

func optionalObjectRefFromProfile(profile Profile) (ObjectRef, bool) {
	if profile.AvatarVersion == nil || profile.AvatarObjectID == nil {
		return ObjectRef{}, false
	}
	return ObjectRef{AvatarVersion: *profile.AvatarVersion, AvatarObjectID: *profile.AvatarObjectID}, true
}

func objectRefFromProfile(profile Profile) ObjectRef {
	ref, _ := optionalObjectRefFromProfile(profile)
	return ref
}
