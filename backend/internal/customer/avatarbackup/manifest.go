package avatarbackup

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	customerdomain "github.com/samson/customer-manage-platform/backend/internal/customer"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

const Format = "customer-avatar-exact-generation-v1"

type PointerLister interface {
	ListCurrentPointers(context.Context, store.AccountScope, string, int) ([]customerdomain.Customer, error)
}

type ObjectInventory interface {
	Inventory(context.Context) ([]customerdomain.ObjectItem, error)
}

type Manifest struct {
	Format      string           `json:"format"`
	GeneratedAt time.Time        `json:"generated_at"`
	Current     []CurrentObject  `json:"current"`
	Inventory   InventorySummary `json:"inventory"`
}

type CurrentObject struct {
	AccountID      string `json:"account_id"`
	CustomerID     string `json:"customer_id"`
	AvatarVersion  string `json:"avatar_version"`
	AvatarObjectID string `json:"avatar_object_id"`
	Key            string `json:"key"`
	MediaType      string `json:"media_type"`
	Size           int64  `json:"size"`
	ActualSHA256   string `json:"actual_sha256"`
}

type InventoryObject struct {
	Key          string `json:"key"`
	Size         int64  `json:"size"`
	ActualSHA256 string `json:"actual_sha256"`
}

type InventorySummary struct {
	Count        int               `json:"count"`
	ActualSHA256 string            `json:"actual_sha256"`
	Objects      []InventoryObject `json:"objects"`
}

func Generate(
	ctx context.Context,
	accounts []store.ScopedAccount,
	pointers PointerLister,
	objects ObjectInventory,
	now time.Time,
) (Manifest, error) {
	manifest := Manifest{Format: Format, GeneratedAt: now.UTC(), Current: make([]CurrentObject, 0)}
	inventory, err := objects.Inventory(ctx)
	if err != nil {
		return Manifest{}, fmt.Errorf("inventory avatar objects: %w", err)
	}
	physicalByKey := make(map[string]customerdomain.ObjectMeta, len(inventory))
	for _, item := range inventory {
		physicalByKey[item.Key] = item.Meta
	}
	for _, account := range accounts {
		cursor := ""
		for {
			customers, err := pointers.ListCurrentPointers(ctx, account.Scope, cursor, 1000)
			if err != nil {
				return Manifest{}, fmt.Errorf("list current avatar pointers: %w", err)
			}
			for _, customer := range customers {
				entry, err := currentEntry(account.AccountID, customer, physicalByKey)
				if err != nil {
					return Manifest{}, err
				}
				manifest.Current = append(manifest.Current, entry)
			}
			if len(customers) < 1000 {
				break
			}
			cursor = customers[len(customers)-1].ID
		}
	}
	sort.Slice(manifest.Current, func(i, j int) bool {
		if manifest.Current[i].AccountID == manifest.Current[j].AccountID {
			return manifest.Current[i].CustomerID < manifest.Current[j].CustomerID
		}
		return manifest.Current[i].AccountID < manifest.Current[j].AccountID
	})
	manifest.Inventory = summarizeInventory(inventory)
	return manifest, nil
}

func Verify(
	ctx context.Context,
	expected Manifest,
	accounts []store.ScopedAccount,
	pointers PointerLister,
	objects ObjectInventory,
) error {
	if expected.Format != Format {
		return errors.New("unsupported avatar manifest format")
	}
	actual, err := Generate(ctx, accounts, pointers, objects, expected.GeneratedAt)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(expected.Current, actual.Current) {
		return errors.New("current exact-generation manifest mismatch")
	}
	if !reflect.DeepEqual(expected.Inventory, actual.Inventory) {
		return errors.New("physical avatar inventory manifest mismatch")
	}
	return nil
}

func currentEntry(
	accountID string,
	customer customerdomain.Customer,
	physicalByKey map[string]customerdomain.ObjectMeta,
) (CurrentObject, error) {
	if customer.AvatarVersion == nil || customer.AvatarObjectID == nil ||
		customer.AvatarMediaType == nil || customer.AvatarSize == nil {
		return CurrentObject{}, errors.New("incomplete current avatar pointer")
	}
	ref := customerdomain.ObjectRef{AvatarVersion: *customer.AvatarVersion, AvatarObjectID: *customer.AvatarObjectID}
	key, err := customerdomain.AvatarObjectKey(accountID, customer.ID, ref)
	if err != nil {
		return CurrentObject{}, errors.New("invalid current avatar object reference")
	}
	meta, ok := physicalByKey[key]
	if !ok {
		return CurrentObject{}, errors.New("current avatar generation missing from physical inventory")
	}
	if meta.MediaType != *customer.AvatarMediaType || meta.Size != *customer.AvatarSize ||
		meta.Checksum != *customer.AvatarVersion {
		return CurrentObject{}, errors.New("current avatar metadata mismatch")
	}
	return CurrentObject{
		AccountID: accountID, CustomerID: customer.ID,
		AvatarVersion: *customer.AvatarVersion, AvatarObjectID: *customer.AvatarObjectID,
		Key: key, MediaType: meta.MediaType, Size: meta.Size,
		ActualSHA256: strings.TrimPrefix(meta.Checksum, "sha256-"),
	}, nil
}

func summarizeInventory(items []customerdomain.ObjectItem) InventorySummary {
	sort.Slice(items, func(i, j int) bool { return items[i].Key < items[j].Key })
	objects := make([]InventoryObject, 0, len(items))
	digest := sha256.New()
	for _, item := range items {
		checksum := strings.TrimPrefix(item.Meta.Checksum, "sha256-")
		objects = append(objects, InventoryObject{Key: item.Key, Size: item.Meta.Size, ActualSHA256: checksum})
		_, _ = fmt.Fprintf(digest, "%s\x00%d\x00%s\n", item.Key, item.Meta.Size, checksum)
	}
	return InventorySummary{
		Count: len(objects), ActualSHA256: fmt.Sprintf("%x", digest.Sum(nil)), Objects: objects,
	}
}
