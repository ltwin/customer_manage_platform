package avatarbackup

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/avatarmedia"
	customerdomain "github.com/samson/customer-manage-platform/backend/internal/customer"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

const (
	Format   = avatarmedia.FormatV1
	FormatV2 = avatarmedia.FormatV2
)

type PointerLister interface {
	ListCurrentPointers(context.Context, store.AccountScope, string, int) ([]customerdomain.Customer, error)
}

// ProfilePointerLister 列举 account_profile current pointer。
type ProfilePointerLister interface {
	ListCurrent(ctx context.Context, accountID string, scope any, cursor string, limit int) ([]avatarmedia.CurrentPointer, error)
}

type ObjectInventory interface {
	Inventory(context.Context) ([]customerdomain.ObjectItem, error)
}

type Manifest = avatarmedia.ManifestV2
type ManifestV1 = avatarmedia.ManifestV1
type CurrentObject = avatarmedia.CurrentObjectV2
type InventoryObject = avatarmedia.InventoryObject
type InventorySummary = avatarmedia.InventorySummary

// Generate 生成 v2 mixed manifest（customer＋account_profile）；profiles 可为 nil。
func Generate(
	ctx context.Context,
	accounts []store.ScopedAccount,
	pointers PointerLister,
	profiles ProfilePointerLister,
	objects ObjectInventory,
	now time.Time,
) (Manifest, error) {
	return avatarmedia.GenerateV2Mixed(ctx, toAccountRefs(accounts), mergePointers(accounts, pointers, profiles), objects, now)
}

// GenerateCustomerOnly 保留 customer-only v2（表征／兼容测试）。
func GenerateCustomerOnly(
	ctx context.Context,
	accounts []store.ScopedAccount,
	pointers PointerLister,
	objects ObjectInventory,
	now time.Time,
) (Manifest, error) {
	return avatarmedia.GenerateV2(ctx, toAccountRefs(accounts), adaptPointers(accounts, pointers), objects, now)
}

// Verify 按 format 分派：v2 用 mixed generate；v1 用内部投影比对。
func Verify(
	ctx context.Context,
	format string,
	v1 ManifestV1,
	v2 Manifest,
	accounts []store.ScopedAccount,
	pointers PointerLister,
	profiles ProfilePointerLister,
	objects ObjectInventory,
) error {
	list := mergePointers(accounts, pointers, profiles)
	refs := toAccountRefs(accounts)
	switch format {
	case FormatV2:
		return avatarmedia.VerifyV2Mixed(ctx, v2, refs, list, objects)
	case Format:
		return avatarmedia.VerifyV1(ctx, v1, refs, adaptPointers(accounts, pointers), objects)
	default:
		return errors.New("unsupported avatar manifest format")
	}
}

func toAccountRefs(accounts []store.ScopedAccount) []avatarmedia.AccountRef {
	refs := make([]avatarmedia.AccountRef, 0, len(accounts))
	for _, account := range accounts {
		refs = append(refs, avatarmedia.AccountRef{AccountID: account.AccountID})
	}
	return refs
}

func adaptPointers(accounts []store.ScopedAccount, pointers PointerLister) avatarmedia.CustomerPointerFunc {
	scopes := make(map[string]store.AccountScope, len(accounts))
	for _, account := range accounts {
		scopes[account.AccountID] = account.Scope
	}
	return func(ctx context.Context, accountID, cursor string, limit int) ([]avatarmedia.CurrentPointer, error) {
		scope, ok := scopes[accountID]
		if !ok {
			return nil, fmt.Errorf("unknown account scope: %s", accountID)
		}
		customers, err := pointers.ListCurrentPointers(ctx, scope, cursor, limit)
		if err != nil {
			return nil, err
		}
		out := make([]avatarmedia.CurrentPointer, 0, len(customers))
		for _, customer := range customers {
			if customer.AvatarVersion == nil || customer.AvatarObjectID == nil ||
				customer.AvatarMediaType == nil || customer.AvatarSize == nil {
				return nil, errors.New("incomplete current avatar pointer")
			}
			out = append(out, avatarmedia.CurrentPointer{
				AccountID: accountID, SubjectKind: avatarmedia.SubjectCustomer, SubjectID: customer.ID,
				AvatarVersion: *customer.AvatarVersion, AvatarObjectID: *customer.AvatarObjectID,
				MediaType: *customer.AvatarMediaType, Size: *customer.AvatarSize,
			})
		}
		return out, nil
	}
}

func mergePointers(
	accounts []store.ScopedAccount,
	customers PointerLister,
	profiles ProfilePointerLister,
) avatarmedia.CustomerPointerFunc {
	customerFn := adaptPointers(accounts, customers)
	scopes := make(map[string]store.AccountScope, len(accounts))
	for _, account := range accounts {
		scopes[account.AccountID] = account.Scope
	}
	return func(ctx context.Context, accountID, cursor string, limit int) ([]avatarmedia.CurrentPointer, error) {
		out, err := customerFn(ctx, accountID, cursor, limit)
		if err != nil {
			return nil, err
		}
		// 客户页未耗尽时先继续分页；耗尽后（含空页）追加 account_profile。
		if len(out) >= limit || profiles == nil {
			return out, nil
		}
		scope, ok := scopes[accountID]
		if !ok {
			return nil, fmt.Errorf("unknown account scope: %s", accountID)
		}
		profilePtrs, err := profiles.ListCurrent(ctx, accountID, scope, "", limit)
		if err != nil {
			return nil, err
		}
		return append(out, profilePtrs...), nil
	}
}
