package avatarmedia

import (
	"bytes"
	"encoding/json"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDualKeyRoundTripAndRejects(t *testing.T) {
	ref := ObjectRef{
		AvatarVersion:  "sha256-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		AvatarObjectID: "11111111111111111111111111111111",
	}
	customer, err := CustomerKey("acc_test", "cus_test", ref)
	if err != nil {
		t.Fatalf("CustomerKey: %v", err)
	}
	wantCustomer := "avatars/acc_test/customers/cus_test/" + ref.AvatarVersion + "/" + ref.AvatarObjectID
	if customer.String() != wantCustomer {
		t.Fatalf("customer key drift: got %s want %s", customer, wantCustomer)
	}
	parsed, err := ParseKey(customer.String())
	if err != nil || parsed.SubjectKind != SubjectCustomer || parsed.SubjectID != "cus_test" {
		t.Fatalf("parse customer: %+v err=%v", parsed, err)
	}

	profile, err := AccountProfileKey("acc_test", ref)
	if err != nil {
		t.Fatalf("AccountProfileKey: %v", err)
	}
	wantProfile := "avatars/acc_test/account-profile/" + ref.AvatarVersion + "/" + ref.AvatarObjectID
	if profile.String() != wantProfile {
		t.Fatalf("profile key drift: got %s want %s", profile, wantProfile)
	}
	parsedProfile, err := ParseKey(profile.String())
	if err != nil || parsedProfile.SubjectKind != SubjectAccountProfile || parsedProfile.SubjectID != "acc_test" {
		t.Fatalf("parse profile: %+v err=%v", parsedProfile, err)
	}

	for _, bad := range []string{
		"../escape",
		"avatars/acc/customers/cus/sha256-aa/not-hex",
		"avatars/acc/unknown/cus/" + ref.AvatarVersion + "/" + ref.AvatarObjectID,
		"avatars/acc/account-profile/" + ref.AvatarVersion + "/short",
	} {
		if _, err := ParseKey(bad); err == nil {
			t.Fatalf("ParseKey(%q) must fail", bad)
		}
	}
}

func TestTypedPrefixesEndAtPathSegmentBoundary(t *testing.T) {
	customer, err := CustomerPrefix("acc", "cust")
	if err != nil || customer.String() != "avatars/acc/customers/cust/" {
		t.Fatalf("CustomerPrefix boundary: %q err=%v", customer.String(), err)
	}
	account, err := AccountPrefix("acc")
	if err != nil || account.String() != "avatars/acc/" {
		t.Fatalf("AccountPrefix boundary: %q err=%v", account.String(), err)
	}
	if _, err := ParsePrefix(customer.String()); err != nil {
		t.Fatalf("typed trailing-slash prefix must parse: %v", err)
	}
}

func TestGoldenKeyAndManifestBytes(t *testing.T) {
	ref := ObjectRef{
		AvatarVersion:  "sha256-0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		AvatarObjectID: "abcdef0123456789abcdef0123456789",
	}
	customer, err := CustomerKey("account-golden", "customer-golden", ref)
	if err != nil {
		t.Fatalf("CustomerKey: %v", err)
	}
	profile, err := AccountProfileKey("account-golden", ref)
	if err != nil {
		t.Fatalf("AccountProfileKey: %v", err)
	}
	keys := map[string]string{
		"customer_key":        customer.String(),
		"account_profile_key": profile.String(),
	}
	assertGoldenJSON(t, "testdata/golden/dual-keys.json", keys)

	generatedAt := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)
	manifest := ManifestV2{
		Format: FormatV2, GeneratedAt: generatedAt,
		Current: []CurrentObjectV2{{
			AccountID: "account-golden", SubjectKind: SubjectCustomer, SubjectID: "customer-golden",
			AvatarVersion: ref.AvatarVersion, AvatarObjectID: ref.AvatarObjectID,
			Key: customer.String(), MediaType: "image/png", Size: 12,
			ActualSHA256: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		}},
		Inventory: InventorySummary{
			Count: 1,
			ActualSHA256: mustInventoryDigest([]InventoryObject{{
				Key: customer.String(), Size: 12,
				ActualSHA256: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
			}}),
			Objects: []InventoryObject{{
				Key: customer.String(), Size: 12,
				ActualSHA256: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
			}},
		},
	}
	var buf bytes.Buffer
	if err := EncodeManifestCanonical(&buf, manifest); err != nil {
		t.Fatalf("encode manifest: %v", err)
	}
	assertGoldenBytes(t, "testdata/golden/manifest-v2-customer-only.json", buf.Bytes())
}

func TestDecodeConfirmKeepsOriginalBytes(t *testing.T) {
	var buf bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode fixture: %v", err)
	}
	raw := buf.Bytes()
	content, err := DecodeConfirm(raw, "image/png")
	if err != nil {
		t.Fatalf("DecodeConfirm: %v", err)
	}
	if content.MediaType() != "image/png" || !bytes.Equal(content.Bytes(), raw) {
		t.Fatalf("shared decode must return original compliant bytes")
	}
}

func assertGoldenJSON(t *testing.T, rel string, value any) {
	t.Helper()
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatalf("marshal golden: %v", err)
	}
	raw = append(raw, '\n')
	assertGoldenBytes(t, rel, raw)
}

func assertGoldenBytes(t *testing.T, rel string, got []byte) {
	t.Helper()
	path := rel
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir golden: %v", err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s (set UPDATE_GOLDEN=1 to create): %v", path, err)
	}
	if !bytes.Equal(want, got) {
		t.Fatalf("golden mismatch %s\nwant:\n%s\ngot:\n%s", path, want, got)
	}
}

func mustInventoryDigest(objects []InventoryObject) string {
	items := make([]ObjectItem, 0, len(objects))
	for _, object := range objects {
		parsed, err := ParseKey(object.Key)
		if err != nil {
			panic(err)
		}
		items = append(items, ObjectItem{
			Key:  parsed.Key,
			Meta: ObjectMeta{Size: object.Size, Checksum: "sha256-" + object.ActualSHA256},
		})
	}
	return SummarizeInventory(items).ActualSHA256
}
