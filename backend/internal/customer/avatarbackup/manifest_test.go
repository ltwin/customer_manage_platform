package avatarbackup

import (
	"context"
	"testing"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/avatarmedia"
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

type fakeProfilePointers struct{ pointers []avatarmedia.CurrentPointer }

func (f fakeProfilePointers) ListCurrent(
	_ context.Context,
	_ string,
	_ any,
	cursor string,
	_ int,
) ([]avatarmedia.CurrentPointer, error) {
	if cursor != "" {
		return nil, nil
	}
	return append([]avatarmedia.CurrentPointer(nil), f.pointers...), nil
}

func TestManifestGenerateV2AndVerifyExactObjectID(t *testing.T) {
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
	manifest, err := Generate(
		context.Background(), accounts, fakePointers{[]customerdomain.Customer{customer}}, nil, physical, generatedAt,
	)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if manifest.Format != FormatV2 {
		t.Fatalf("generate must emit v2, got %s", manifest.Format)
	}
	if len(manifest.Current) != 1 || manifest.Current[0].AvatarObjectID != objectID ||
		manifest.Current[0].SubjectKind != avatarmedia.SubjectCustomer ||
		manifest.Current[0].SubjectID != customer.ID ||
		manifest.Current[0].ActualSHA256 != version[len("sha256-"):] || manifest.Inventory.Count != 1 {
		t.Fatalf("manifest mismatch: %+v", manifest)
	}
	if err := Verify(
		context.Background(), FormatV2, ManifestV1{}, manifest, accounts,
		fakePointers{[]customerdomain.Customer{customer}}, nil, physical,
	); err != nil {
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
	err = Verify(
		context.Background(), FormatV2, ManifestV1{}, manifest, accounts,
		fakePointers{[]customerdomain.Customer{wrongCustomer}}, nil, wrongPhysical,
	)
	if err == nil || err.Error() != "current exact-generation manifest mismatch" {
		t.Fatalf("wrong object id must fail exact verification, got %v", err)
	}
}

func TestProjectV1ExcludesAccountProfileKeys(t *testing.T) {
	version := "sha256-bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	objectID := "33333333333333333333333333333333"
	mediaType := "image/png"
	size := int64(7)
	customer := customerdomain.Customer{
		ID: "cus_test",
		AvatarMetadata: customerdomain.AvatarMetadata{
			AvatarVersion: &version, AvatarObjectID: &objectID,
			AvatarMediaType: &mediaType, AvatarSize: &size,
		},
	}
	customerKey, _ := customerdomain.AvatarObjectKey("acc_test", customer.ID, customerdomain.ObjectRef{
		AvatarVersion: version, AvatarObjectID: objectID,
	})
	profileKey, err := avatarmedia.AccountProfileKey("acc_test", avatarmedia.ObjectRef{
		AvatarVersion: version, AvatarObjectID: "44444444444444444444444444444444",
	})
	if err != nil {
		t.Fatalf("AccountProfileKey: %v", err)
	}
	accounts := []store.ScopedAccount{{AccountID: "acc_test"}}
	physical := fakeInventory{items: []customerdomain.ObjectItem{
		{Key: customerKey, Meta: customerdomain.ObjectMeta{MediaType: mediaType, Size: size, Checksum: version}},
		{Key: profileKey, Meta: customerdomain.ObjectMeta{MediaType: mediaType, Size: size, Checksum: version}},
	}}
	generatedAt := time.Date(2026, 8, 2, 0, 0, 0, 0, time.UTC)
	v1, err := avatarmedia.ProjectV1ForVerify(
		context.Background(),
		[]avatarmedia.AccountRef{{AccountID: "acc_test"}},
		adaptPointers(accounts, fakePointers{[]customerdomain.Customer{customer}}),
		physical,
		generatedAt,
	)
	if err != nil {
		t.Fatalf("ProjectV1ForVerify: %v", err)
	}
	if v1.Inventory.Count != 1 || len(v1.Current) != 1 || v1.Current[0].CustomerID != customer.ID {
		t.Fatalf("v1 projection must be customer-only: %+v", v1)
	}
	if err := Verify(
		context.Background(), Format, v1, Manifest{}, accounts,
		fakePointers{[]customerdomain.Customer{customer}}, nil, physical,
	); err != nil {
		t.Fatalf("v1 verify via projection: %v", err)
	}
}

func TestManifestGenerateV2MixedAccountProfileAndCustomer(t *testing.T) {
	version := "sha256-cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	customerObject := "55555555555555555555555555555555"
	profileObject := "66666666666666666666666666666666"
	mediaType := "image/jpeg"
	size := int64(11)
	customer := customerdomain.Customer{
		ID: "cus_mixed",
		AvatarMetadata: customerdomain.AvatarMetadata{
			AvatarVersion: &version, AvatarObjectID: &customerObject,
			AvatarMediaType: &mediaType, AvatarSize: &size,
		},
	}
	customerKey, _ := customerdomain.AvatarObjectKey("acc_mixed", customer.ID, customerdomain.ObjectRef{
		AvatarVersion: version, AvatarObjectID: customerObject,
	})
	profileKey, err := avatarmedia.AccountProfileKey("acc_mixed", avatarmedia.ObjectRef{
		AvatarVersion: version, AvatarObjectID: profileObject,
	})
	if err != nil {
		t.Fatalf("AccountProfileKey: %v", err)
	}
	accounts := []store.ScopedAccount{{AccountID: "acc_mixed"}}
	physical := fakeInventory{items: []customerdomain.ObjectItem{
		{Key: customerKey, Meta: customerdomain.ObjectMeta{MediaType: mediaType, Size: size, Checksum: version}},
		{Key: profileKey, Meta: customerdomain.ObjectMeta{MediaType: mediaType, Size: size, Checksum: version}},
	}}
	profiles := fakeProfilePointers{pointers: []avatarmedia.CurrentPointer{{
		AccountID: "acc_mixed", SubjectKind: avatarmedia.SubjectAccountProfile, SubjectID: "acc_mixed",
		AvatarVersion: version, AvatarObjectID: profileObject, MediaType: mediaType, Size: size,
	}}}
	generatedAt := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)
	manifest, err := Generate(
		context.Background(), accounts, fakePointers{[]customerdomain.Customer{customer}}, profiles, physical, generatedAt,
	)
	if err != nil {
		t.Fatalf("Generate mixed: %v", err)
	}
	if len(manifest.Current) != 2 || manifest.Inventory.Count != 2 {
		t.Fatalf("mixed manifest size: %+v", manifest)
	}
	kinds := map[string]string{}
	for _, entry := range manifest.Current {
		kinds[entry.SubjectKind] = entry.SubjectID
	}
	if kinds[avatarmedia.SubjectCustomer] != "cus_mixed" || kinds[avatarmedia.SubjectAccountProfile] != "acc_mixed" {
		t.Fatalf("mixed subjects: %+v", kinds)
	}
	if err := Verify(
		context.Background(), FormatV2, ManifestV1{}, manifest, accounts,
		fakePointers{[]customerdomain.Customer{customer}}, profiles, physical,
	); err != nil {
		t.Fatalf("Verify mixed: %v", err)
	}
}
