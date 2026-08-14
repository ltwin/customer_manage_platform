package planshare

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"strings"
	"testing"
)

func TestShareTokenCommitmentRoundTripAndConstantTimeMiss(t *testing.T) {
	selectorRaw := make([]byte, shareSelectorByteLen)
	secret := make([]byte, shareSecretByteLen)
	if _, err := rand.Read(selectorRaw); err != nil {
		t.Fatalf("selector entropy: %v", err)
	}
	if _, err := rand.Read(secret); err != nil {
		t.Fatalf("secret entropy: %v", err)
	}
	selector, err := EncodeShareSelector(selectorRaw)
	if err != nil {
		t.Fatalf("encode selector: %v", err)
	}
	wire, err := ComposeShareTokenWire(selector, secret)
	if err != nil {
		t.Fatalf("compose wire: %v", err)
	}
	if strings.Count(wire, ".") != 2 || !strings.HasPrefix(wire, "sp1.") {
		t.Fatalf("wire format = %q", wire)
	}
	parsed, err := ParseShareToken(wire)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	commitment := CommitShareSecret(parsed.Secret)
	if !CompareShareCommitment(true, parsed.Secret, commitment) {
		t.Fatal("matching commitment must accept")
	}
	if CompareShareCommitment(false, parsed.Secret, commitment) {
		t.Fatal("selector miss must reject after dummy compare")
	}
	wrong := append([]byte(nil), parsed.Secret...)
	wrong[0] ^= 0xff
	if CompareShareCommitment(true, wrong, commitment) {
		t.Fatal("hash mismatch must reject")
	}
	fingerprint := ShareFingerprint(parsed.Selector, commitment)
	if fingerprint == "" || strings.Contains(fingerprint, parsed.Selector) || bytes.Contains([]byte(fingerprint), secret) {
		t.Fatalf("fingerprint must be non-usable digest, got %q", fingerprint)
	}
	if _, err := ParseShareToken("sp1.bad"); err != ErrInvalidShareToken {
		t.Fatalf("invalid token error=%v", err)
	}
}

func TestClaimReceiptCommitmentOnlyHash(t *testing.T) {
	secret := make([]byte, receiptSecretByteLen)
	if _, err := rand.Read(secret); err != nil {
		t.Fatalf("receipt entropy: %v", err)
	}
	wire, err := ComposeClaimReceiptWire(secret)
	if err != nil {
		t.Fatalf("compose receipt: %v", err)
	}
	parsed, err := ParseClaimReceipt(wire)
	if err != nil {
		t.Fatalf("parse receipt: %v", err)
	}
	commitment := CommitClaimReceipt(parsed.Secret)
	if !CompareReceiptCommitment(true, parsed.Secret, commitment) {
		t.Fatal("matching receipt must accept")
	}
	if CompareReceiptCommitment(false, parsed.Secret, commitment) {
		t.Fatal("receipt miss must reject after dummy compare")
	}
}

func TestProposalDTOExcludesFullOnlyFields(t *testing.T) {
	proposal := SharedPlanProposalV1{
		ViewLevel: ViewLevelProposal,
		Title:     "样片",
		PublicScale: SharedPublicScaleV1{
			PlannedShotCount: 3,
		},
		Moodboard:          []SharedMoodboardItemV1{{Ref: "ref", Checksum: "abc"}},
		ProjectionRevision: 1,
	}
	raw, err := json.Marshal(proposal)
	if err != nil {
		t.Fatalf("marshal proposal: %v", err)
	}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(raw, &keys); err != nil {
		t.Fatalf("decode keys: %v", err)
	}
	for _, forbidden := range []string{"shots", "assignment_opportunities", "notes", "price", "customer_id", "order_id"} {
		if _, ok := keys[forbidden]; ok {
			t.Fatalf("proposal DTO must not include %q: %s", forbidden, raw)
		}
	}
	if string(keys["view_level"]) != `"proposal"` {
		t.Fatalf("view_level=%s", keys["view_level"])
	}

	full := SharedPlanFullV1{
		SharedPlanProposalV1: SharedPlanProposalV1{ViewLevel: ViewLevelFull, Title: "成片", ProjectionRevision: 2},
		Shots:                []SharedShotV1{{ID: "shot_1", Position: 1, Title: "主镜头", Revision: 1}},
	}
	fullRaw, err := json.Marshal(full)
	if err != nil {
		t.Fatalf("marshal full: %v", err)
	}
	if !bytes.Contains(fullRaw, []byte(`"shots"`)) || !bytes.Contains(fullRaw, []byte(`"view_level":"full"`)) {
		t.Fatalf("full DTO missing required fields: %s", fullRaw)
	}
	if bytes.Contains(fullRaw, []byte(`"notes"`)) || bytes.Contains(fullRaw, []byte(`"price"`)) {
		t.Fatalf("full DTO leaked private fields: %s", fullRaw)
	}
}
