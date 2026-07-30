package customer

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"regexp"
	"strings"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
	_ "golang.org/x/image/webp"
)

const (
	avatarGCGrace              = 24 * time.Hour
	maxFreshGenerationAttempts = 4
)

var (
	ErrAvatarRevisionConflict = errors.New("avatar_revision_conflict")
	ErrAvatarVersionStale     = errors.New("avatar_version_stale")
	errRetryFreshGeneration   = errors.New("retry with fresh avatar generation")
	avatarKeySegmentPattern   = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)
)

type AvatarApplication struct {
	repo    AvatarRepository
	objects AvatarObjectStore
	now     func() time.Time
}

func (a *AvatarApplication) ReadContent(
	ctx context.Context,
	scope store.AccountScope,
	customerID, requestedVersion string,
) (AvatarContent, error) {
	current, err := a.repo.Snapshot(ctx, scope, customerID)
	if err != nil {
		return AvatarContent{}, err
	}
	if current.AvatarVersion == nil {
		return AvatarContent{}, ErrNotFound
	}
	if requestedVersion != *current.AvatarVersion {
		return AvatarContent{}, ErrAvatarVersionStale
	}
	content, valid, err := a.loadCurrentContent(ctx, current)
	if err != nil {
		return AvatarContent{}, err
	}
	if !valid {
		return AvatarContent{}, ErrAvatarObjectIntegrity
	}
	return content, nil
}

func NewAvatarApplication(repo AvatarRepository, objects AvatarObjectStore) *AvatarApplication {
	return &AvatarApplication{repo: repo, objects: objects, now: time.Now}
}

// ValidateSetTarget 在昂贵的图片解码前做账号归属与 merged 状态预检；Set 内仍会锁内重验。
func (a *AvatarApplication) ValidateSetTarget(
	ctx context.Context,
	scope store.AccountScope,
	customerID string,
) error {
	current, err := a.repo.Snapshot(ctx, scope, customerID)
	if err != nil {
		return err
	}
	if current.Status == StatusMerged {
		return ErrCustomerMerged
	}
	return nil
}

func (a *AvatarApplication) Set(
	ctx context.Context,
	scope store.AccountScope,
	customerID string,
	expectedRevision int64,
	content AvatarContent,
) (Customer, error) {
	if expectedRevision < 0 {
		return Customer{}, ValidationError{Message: "avatar_revision 非法"}
	}
	snapshot, err := a.repo.Snapshot(ctx, scope, customerID)
	if err != nil {
		return Customer{}, err
	}
	if snapshot.Status == StatusMerged {
		return Customer{}, ErrCustomerMerged
	}
	if snapshot.AvatarVersion != nil && *snapshot.AvatarVersion == content.Checksum() {
		current, valid, err := a.verifySameContentCurrent(ctx, scope, customerID, expectedRevision, content.Checksum())
		if err != nil {
			return Customer{}, err
		}
		if valid {
			return current, nil
		}
	}
	return a.publishAndSwitch(ctx, scope, customerID, expectedRevision, content)
}

func (a *AvatarApplication) Remove(
	ctx context.Context,
	scope store.AccountScope,
	customerID string,
	expectedRevision int64,
) (Customer, error) {
	if expectedRevision < 0 {
		return Customer{}, ValidationError{Message: "avatar_revision 非法"}
	}
	var result Customer
	err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		locked, err := a.repo.Lock(ctx, tx, customerID)
		if err != nil {
			return err
		}
		if locked.AvatarVersion == nil {
			result = locked
			return nil
		}
		if locked.AvatarRevision != expectedRevision {
			return ErrAvatarRevisionConflict
		}
		oldRef := objectRefFromCustomer(locked)
		updated, err := a.repo.ClearPointer(ctx, tx, customerID, expectedRevision)
		if err != nil {
			return err
		}
		if err := a.repo.EnqueueGC(ctx, tx, customerID, oldRef, a.now().UTC().Add(avatarGCGrace)); err != nil {
			return err
		}
		result = updated
		return nil
	})
	return result, err
}

func (a *AvatarApplication) verifySameContentCurrent(
	ctx context.Context,
	scope store.AccountScope,
	customerID string,
	expectedRevision int64,
	desiredChecksum string,
) (Customer, bool, error) {
	var current Customer
	var valid bool
	err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		locked, err := a.repo.Lock(ctx, tx, customerID)
		if err != nil {
			return err
		}
		if locked.Status == StatusMerged {
			return ErrCustomerMerged
		}
		if locked.AvatarVersion == nil || *locked.AvatarVersion != desiredChecksum {
			return nil
		}
		valid, err = a.verifyCurrent(ctx, locked)
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

func (a *AvatarApplication) publishAndSwitch(
	ctx context.Context,
	scope store.AccountScope,
	customerID string,
	expectedRevision int64,
	content AvatarContent,
) (Customer, error) {
	for range maxFreshGenerationAttempts {
		objectID, err := newAvatarObjectID()
		if err != nil {
			return Customer{}, err
		}
		ref := ObjectRef{AvatarVersion: content.Checksum(), AvatarObjectID: objectID}
		snapshot, err := a.repo.Snapshot(ctx, scope, customerID)
		if err != nil {
			return Customer{}, err
		}
		key, err := AvatarObjectKey(snapshot.AccountID, customerID, ref)
		if err != nil {
			return Customer{}, err
		}
		meta := ObjectMeta{
			MediaType:  content.MediaType(),
			Size:       content.Size(),
			Checksum:   content.Checksum(),
			ModifiedAt: a.now().UTC(),
		}
		published, err := a.putFreshGeneration(ctx, key, content, meta)
		if errors.Is(err, errRetryFreshGeneration) {
			continue
		}
		if err != nil {
			return Customer{}, err
		}
		meta = published.Meta

		var result Customer
		err = scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
			locked, err := a.repo.Lock(ctx, tx, customerID)
			if err != nil {
				return err
			}
			if locked.Status == StatusMerged {
				return ErrCustomerMerged
			}
			burned, err := a.repo.GCExistsForUpdate(ctx, tx, customerID, objectID)
			if err != nil {
				return err
			}
			if burned {
				return errRetryFreshGeneration
			}
			stored, err := a.objects.Stat(ctx, key)
			if errors.Is(err, ErrAvatarObjectNotFound) || errors.Is(err, ErrAvatarObjectIntegrity) {
				return errRetryFreshGeneration
			}
			if err != nil {
				return err
			}
			if !sameObjectMeta(stored, meta) {
				return errRetryFreshGeneration
			}

			if locked.AvatarVersion != nil && *locked.AvatarVersion == content.Checksum() {
				valid, err := a.verifyCurrent(ctx, locked)
				if err != nil {
					return err
				}
				if valid {
					if err := a.repo.EnqueueGC(ctx, tx, customerID, ref, a.now().UTC().Add(avatarGCGrace)); err != nil {
						return err
					}
					result = locked
					return nil
				}
			}
			if locked.AvatarRevision != expectedRevision {
				return ErrAvatarRevisionConflict
			}
			oldRef, hadOld := optionalObjectRefFromCustomer(locked)
			updated, err := a.repo.SwitchPointer(ctx, tx, customerID, expectedRevision, ref, meta)
			if err != nil {
				return err
			}
			if hadOld {
				if err := a.repo.EnqueueGC(ctx, tx, customerID, oldRef, a.now().UTC().Add(avatarGCGrace)); err != nil {
					return err
				}
			}
			result = updated
			return nil
		})
		if errors.Is(err, errRetryFreshGeneration) {
			continue
		}
		return result, err
	}
	return Customer{}, errors.New("avatar fresh generation attempts exhausted")
}

func (a *AvatarApplication) putFreshGeneration(
	ctx context.Context,
	key string,
	content AvatarContent,
	meta ObjectMeta,
) (PutResult, error) {
	result, err := a.objects.PutImmutable(ctx, key, content, meta)
	if errors.Is(err, ErrAvatarObjectTemporary) {
		result, err = a.objects.PutImmutable(ctx, key, content, meta)
		if err == nil && !result.Created && sameObjectMeta(result.Meta, meta) {
			return result, nil
		}
	}
	if errors.Is(err, ErrAvatarObjectIntegrity) {
		return PutResult{}, errRetryFreshGeneration
	}
	if err != nil {
		return PutResult{}, err
	}
	if !result.Created {
		return PutResult{}, errRetryFreshGeneration
	}
	if !sameObjectMeta(result.Meta, meta) {
		return PutResult{}, errRetryFreshGeneration
	}
	return result, nil
}

func (a *AvatarApplication) verifyCurrent(ctx context.Context, customer Customer) (bool, error) {
	_, valid, err := a.loadCurrentContent(ctx, customer)
	return valid, err
}

func (a *AvatarApplication) loadCurrentContent(ctx context.Context, customer Customer) (AvatarContent, bool, error) {
	ref, ok := optionalObjectRefFromCustomer(customer)
	if !ok || customer.AvatarMediaType == nil || customer.AvatarSize == nil {
		return AvatarContent{}, false, nil
	}
	key, err := AvatarObjectKey(customer.AccountID, customer.ID, ref)
	if err != nil {
		return AvatarContent{}, false, err
	}
	meta, err := a.objects.Stat(ctx, key)
	if errors.Is(err, ErrAvatarObjectNotFound) || errors.Is(err, ErrAvatarObjectIntegrity) {
		return AvatarContent{}, false, nil
	}
	if err != nil {
		return AvatarContent{}, false, err
	}
	expected := ObjectMeta{MediaType: *customer.AvatarMediaType, Size: *customer.AvatarSize, Checksum: ref.AvatarVersion}
	if !sameObjectMeta(meta, expected) {
		return AvatarContent{}, false, nil
	}
	stream, err := a.objects.Open(ctx, key)
	if errors.Is(err, ErrAvatarObjectNotFound) || errors.Is(err, ErrAvatarObjectIntegrity) {
		return AvatarContent{}, false, nil
	}
	if err != nil {
		return AvatarContent{}, false, err
	}
	content, readErr := io.ReadAll(io.LimitReader(stream, MaxAvatarContentBytes+1))
	closeErr := stream.Close()
	if readErr != nil {
		return AvatarContent{}, false, readErr
	}
	if closeErr != nil {
		return AvatarContent{}, false, closeErr
	}
	if int64(len(content)) != expected.Size || len(content) > MaxAvatarContentBytes {
		return AvatarContent{}, false, nil
	}
	digest := sha256.Sum256(content)
	if fmt.Sprintf("sha256-%x", digest) != expected.Checksum {
		return AvatarContent{}, false, nil
	}
	_, format, err := image.DecodeConfig(bytes.NewReader(content))
	if err != nil || imageMediaTypes[format] != expected.MediaType {
		return AvatarContent{}, false, nil
	}
	validated, err := NewAvatarContent(content, expected.MediaType)
	if err != nil {
		return AvatarContent{}, false, nil
	}
	return validated, true, nil
}

var imageMediaTypes = map[string]string{"jpeg": "image/jpeg", "png": "image/png", "webp": "image/webp"}

func AvatarObjectKey(accountID, customerID string, ref ObjectRef) (string, error) {
	if !avatarKeySegmentPattern.MatchString(accountID) || !avatarKeySegmentPattern.MatchString(customerID) ||
		!avatarVersionPattern.MatchString(ref.AvatarVersion) || !avatarObjectIDPattern.MatchString(ref.AvatarObjectID) {
		return "", ErrAvatarObjectKey
	}
	return fmt.Sprintf("avatars/%s/customers/%s/%s/%s", accountID, customerID, ref.AvatarVersion, ref.AvatarObjectID), nil
}

func ParseAvatarObjectKey(key string) (accountID, customerID string, ref ObjectRef, err error) {
	parts := strings.Split(key, "/")
	if len(parts) != 6 || parts[0] != "avatars" || parts[2] != "customers" {
		return "", "", ObjectRef{}, ErrAvatarObjectKey
	}
	ref = ObjectRef{AvatarVersion: parts[4], AvatarObjectID: parts[5]}
	canonical, buildErr := AvatarObjectKey(parts[1], parts[3], ref)
	if buildErr != nil || canonical != key {
		return "", "", ObjectRef{}, ErrAvatarObjectKey
	}
	return parts[1], parts[3], ref, nil
}

var (
	avatarVersionPattern  = regexp.MustCompile(`^sha256-[0-9a-f]{64}$`)
	avatarObjectIDPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)
)

func newAvatarObjectID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", fmt.Errorf("generate avatar object id: %w", err)
	}
	return fmt.Sprintf("%x", value), nil
}

func optionalObjectRefFromCustomer(customer Customer) (ObjectRef, bool) {
	if customer.AvatarVersion == nil || customer.AvatarObjectID == nil {
		return ObjectRef{}, false
	}
	return ObjectRef{AvatarVersion: *customer.AvatarVersion, AvatarObjectID: *customer.AvatarObjectID}, true
}

func objectRefFromCustomer(customer Customer) ObjectRef {
	ref, _ := optionalObjectRefFromCustomer(customer)
	return ref
}

func sameObjectMeta(left, right ObjectMeta) bool {
	return left.MediaType == right.MediaType && left.Size == right.Size && left.Checksum == right.Checksum
}
