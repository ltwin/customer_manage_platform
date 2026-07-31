package auth

import (
	"bytes"
	"crypto/hkdf"
	"crypto/sha256"
	"encoding/hex"
	"net/netip"
	"testing"
	"time"
)

func TestDeriveKeyMatchesVersionedHKDFProtocol(t *testing.T) {
	t.Parallel()
	const root = "synthetic-root-secret"
	for _, label := range []string{accessSigningLabel, replayAEADLabel} {
		label := label
		t.Run(label, func(t *testing.T) {
			t.Parallel()
			want, err := hkdf.Key(sha256.New, []byte(root), nil, label, sha256.Size)
			if err != nil {
				t.Fatalf("derive reference HKDF: %v", err)
			}
			got := deriveKey(root, label)
			if !bytes.Equal(got, want) {
				t.Fatalf("derived key mismatch: got %s want %s", hex.EncodeToString(got), hex.EncodeToString(want))
			}
			if len(got) != sha256.Size {
				t.Fatalf("derived key length: got %d want %d", len(got), sha256.Size)
			}
		})
	}
	if bytes.Equal(deriveKey(root, accessSigningLabel), deriveKey(root, replayAEADLabel)) {
		t.Fatal("versioned labels must derive distinct keys")
	}
}

func TestLimiterDigesterMatchesVersionedProtocol(t *testing.T) {
	t.Parallel()
	digester := NewLimiterDigester("synthetic-root")

	if got, want := digester.Subject(AuthActionLogin, []byte("owner@example.test")), "v1:A7_G3sRfkbn-OE_woV3IMCDA6GDF8zUndcpF8vcSCSU"; got != want {
		t.Fatalf("subject digest = %q, want %q", got, want)
	}
	if got, want := digester.Source(AuthActionLogin, netip.MustParseAddr("2001:db8::1")), "v1:FaJOOyq0aYb9s5Lux4GHSYPaP_oywEedyOpr0gOfl_8"; got != want {
		t.Fatalf("source digest = %q, want %q", got, want)
	}
	if got := digester.Source(AuthActionLogin, netip.MustParseAddr("::ffff:192.0.2.1")); got != digester.Source(AuthActionLogin, netip.MustParseAddr("192.0.2.1")) {
		t.Fatalf("IPv4-mapped source must use canonical Unmap bytes: %q", got)
	}
	if digester.Subject(AuthActionRegister, []byte("owner@example.test")) == digester.Subject(AuthActionLogin, []byte("owner@example.test")) {
		t.Fatal("different actions must have distinct digest namespaces")
	}
}

func TestRootRotationChangesLimiterNamespace(t *testing.T) {
	t.Parallel()
	subject := []byte("synthetic-subject")
	source := netip.MustParseAddr("192.0.2.10")
	oldNamespace := NewLimiterDigester("synthetic-old-root")
	newNamespace := NewLimiterDigester("synthetic-new-root")

	if oldNamespace.Subject(AuthActionLogin, subject) == newNamespace.Subject(AuthActionLogin, subject) {
		t.Fatal("root rotation must invalidate the old limiter subject namespace")
	}
	if oldNamespace.Source(AuthActionLogin, source) == newNamespace.Source(AuthActionLogin, source) {
		t.Fatal("root rotation must invalidate the old limiter source namespace")
	}
}

func TestReplayCipherEnvelopeAndAADContract(t *testing.T) {
	t.Parallel()
	nonces := bytes.NewReader(append(bytes.Repeat([]byte{0x11}, 12), bytes.Repeat([]byte{0x22}, 12)...))
	cipher, err := NewReplayCipher("synthetic-root-secret", nonces)
	if err != nil {
		t.Fatalf("new replay cipher: %v", err)
	}
	deadline := time.Date(2026, 7, 31, 8, 9, 10, 0, time.FixedZone("offset", 8*60*60))
	first, err := cipher.Seal("parent", "family", deadline, "successor-wire")
	if err != nil {
		t.Fatalf("seal first: %v", err)
	}
	second, err := cipher.Seal("parent", "family", deadline, "successor-wire")
	if err != nil {
		t.Fatalf("seal second: %v", err)
	}
	if first[0] != replayEnvelopeV1 || len(first) < 29 {
		t.Fatalf("invalid envelope header/length: version=%d length=%d", first[0], len(first))
	}
	if bytes.Equal(first[1:13], second[1:13]) {
		t.Fatal("successive envelopes reused the nonce")
	}
	opened, err := cipher.Open("parent", "family", deadline.UTC(), first)
	if err != nil || opened != "successor-wire" {
		t.Fatalf("open valid envelope: value=%q err=%v", opened, err)
	}

	mutations := map[string]func([]byte) []byte{
		"unknown version": func(value []byte) []byte { value[0] = 2; return value },
		"nonce tamper":    func(value []byte) []byte { value[1] ^= 1; return value },
		"cipher tamper":   func(value []byte) []byte { value[len(value)-1] ^= 1; return value },
		"short envelope":  func(value []byte) []byte { return value[:28] },
	}
	for name, mutate := range mutations {
		name, mutate := name, mutate
		t.Run(name, func(t *testing.T) {
			value := mutate(append([]byte(nil), first...))
			if _, err := cipher.Open("parent", "family", deadline, value); err == nil {
				t.Fatal("mutated envelope should be rejected")
			}
		})
	}
	if _, err := cipher.Open("other-parent", "family", deadline, first); err == nil {
		t.Fatal("AAD parent mismatch should be rejected")
	}
	if _, err := cipher.Open("parent", "other-family", deadline, first); err == nil {
		t.Fatal("AAD family mismatch should be rejected")
	}
	if _, err := cipher.Open("parent", "family", deadline.Add(time.Second), first); err == nil {
		t.Fatal("AAD deadline mismatch should be rejected")
	}
	wrongKey, err := NewReplayCipher("rotated-synthetic-root", bytes.NewReader(bytes.Repeat([]byte{0x33}, 12)))
	if err != nil {
		t.Fatalf("new wrong-key cipher: %v", err)
	}
	if _, err := wrongKey.Open("parent", "family", deadline, first); err == nil {
		t.Fatal("root rotation should reject old replay envelope")
	}
}

func TestBearerTokenStoresOnlySecretHash(t *testing.T) {
	t.Parallel()
	random := bytes.NewReader(bytes.Repeat([]byte{0x5a}, 48))
	wire, selector, digest, err := newBearerToken(random)
	if err != nil {
		t.Fatalf("new bearer token: %v", err)
	}
	if len(selector) != 32 || len(digest) != sha256.Size {
		t.Fatalf("unexpected selector/hash length: selector=%d hash=%d", len(selector), len(digest))
	}
	parsedSelector, parsedDigest, err := parseBearerToken(wire)
	if err != nil {
		t.Fatalf("parse bearer token: %v", err)
	}
	if parsedSelector != selector || !bytes.Equal(parsedDigest, digest) {
		t.Fatal("parsed bearer proof differs from generated proof")
	}
	if bytes.Contains(digest, bytes.Repeat([]byte{0x5a}, 8)) {
		t.Fatal("stored digest appears to contain raw bearer secret")
	}
	for _, malformed := range []string{"", selector, selector + ".", ".secret", selector + ".bad.secret"} {
		if _, _, err := parseBearerToken(malformed); err == nil {
			t.Fatalf("malformed bearer token accepted: %q", malformed)
		}
	}
}
