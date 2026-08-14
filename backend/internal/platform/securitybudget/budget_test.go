package securitybudget

import (
	"testing"
	"time"
)

func TestValidateClosedRejectsAuthAndRawDimensions(t *testing.T) {
	if err := ValidateClosed(PolicyV1, ActionAnonymousMutationOuter, DimensionIP); err != nil {
		t.Fatalf("valid combo: %v", err)
	}
	if err := ValidateClosed(PolicyV1, Action("login"), DimensionIP); err == nil {
		t.Fatal("auth login action must be rejected")
	}
	if err := ValidateClosed(PolicyV1, ActionAnonymousReadOuter, Dimension("raw-ip")); err == nil {
		t.Fatal("raw-ip dimension must be rejected")
	}
}

func TestValidateCandidatesPreviousDayGraceWindow(t *testing.T) {
	now := time.Date(2026, 8, 15, 0, 5, 0, 0, time.UTC)
	previous := DigestIdentity{Version: DigestVersionV1, HMAC: bytesFill(0x21, 32)}
	candidates := DigestCandidates{
		Current:            DigestIdentity{Version: DigestVersionV1, HMAC: bytesFill(0x22, 32)},
		Previous:           &previous,
		RolloverGraceUntil: now.Add(10 * time.Minute),
	}
	if err := ValidateCandidates(candidates, now); err != nil {
		t.Fatalf("within previous-day grace: %v", err)
	}

	expired := candidates
	expired.RolloverGraceUntil = now.Add(-time.Second)
	if err := ValidateCandidates(expired, now); err != ErrInvalidCandidates {
		t.Fatalf("expired previous-day grace err=%v", err)
	}

	missingGrace := candidates
	missingGrace.RolloverGraceUntil = time.Time{}
	if err := ValidateCandidates(missingGrace, now); err != ErrInvalidCandidates {
		t.Fatalf("zero grace with previous present err=%v", err)
	}
}

func bytesFill(value byte, n int) []byte {
	out := make([]byte, n)
	for i := range out {
		out[i] = value
	}
	return out
}
