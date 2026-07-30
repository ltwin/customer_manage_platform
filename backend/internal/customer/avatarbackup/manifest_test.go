package avatarbackup

import (
	"context"
	"testing"
	"time"

	customerdomain "github.com/samson/customer-manage-platform/backend/internal/customer"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

type fakePointers struct{ customers []customerdomain.Customer }

func (f fakePointers) ListCurrentPointers(
	_ context.Context,
	_ store.AccountScope,
	cursor string,
	_ int,
) ([]customerdomain.Customer, error) {
	if cursor != "" {
		return nil, nil
	}
	return f.customers, nil
}

type fakeInventory struct{ items []customerdomain.ObjectItem }

func (f fakeInventory) Inventory(context.Context) ([]customerdomain.ObjectItem, error) {
	return append([]customerdomain.ObjectItem(nil), f.items...), nil
}

func TestManifestGenerateAndVerifyExactObjectID(t *testing.T) {
	version := "sha256-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	objectID := "11111111111111111111111111111111"
	mediaType := "image/png"
	size := int64(42)
	customer := customerdomain.Customer{
		ID: "cus_test",
		AvatarMetadata: customerdomain.AvatarMetadata{
			AvatarVersion: &version, AvatarObjectID: &objectID,
			AvatarMediaType: &mediaType, AvatarSize: &size,
		},
	}
	key, err := customerdomain.AvatarObjectKey("acc_test", customer.ID, customerdomain.ObjectRef{
		AvatarVersion: version, AvatarObjectID: objectID,
	})
	if err != nil {
		t.Fatalf("AvatarObjectKey: %v", err)
	}
	accounts := []store.ScopedAccount{{AccountID: "acc_test"}}
	physical := fakeInventory{items: []customerdomain.ObjectItem{{
		Key:  key,
		Meta: customerdomain.ObjectMeta{MediaType: mediaType, Size: size, Checksum: version},
	}}}
	generatedAt := time.Date(2026, 7, 13, 0, 0, 0, 0, time.UTC)
	manifest, err := Generate(context.Background(), accounts, fakePointers{[]customerdomain.Customer{customer}}, physical, generatedAt)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(manifest.Current) != 1 || manifest.Current[0].AvatarObjectID != objectID ||
		manifest.Current[0].ActualSHA256 != version[len("sha256-"):] || manifest.Inventory.Count != 1 {
		t.Fatalf("manifest mismatch: %+v", manifest)
	}
	if err := Verify(context.Background(), manifest, accounts, fakePointers{[]customerdomain.Customer{customer}}, physical); err != nil {
		t.Fatalf("Verify: %v", err)
	}

	wrongObjectID := "22222222222222222222222222222222"
	wrongCustomer := customer
	wrongCustomer.AvatarObjectID = &wrongObjectID
	wrongKey, _ := customerdomain.AvatarObjectKey("acc_test", customer.ID, customerdomain.ObjectRef{
		AvatarVersion: version, AvatarObjectID: wrongObjectID,
	})
	wrongPhysical := fakeInventory{items: []customerdomain.ObjectItem{{
		Key:  wrongKey,
		Meta: customerdomain.ObjectMeta{MediaType: mediaType, Size: size, Checksum: version},
	}}}
	err = Verify(context.Background(), manifest, accounts, fakePointers{[]customerdomain.Customer{wrongCustomer}}, wrongPhysical)
	if err == nil || err.Error() != "current exact-generation manifest mismatch" {
		t.Fatalf("wrong object id must fail exact verification, got %v", err)
	}
}
