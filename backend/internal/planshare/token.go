package planshare

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"
)

const (
	shareTokenVersionPrefix = "sp1"
	receiptVersionPrefix    = "cr1"
	shareSelectorByteLen    = 12
	shareSecretByteLen      = 32
	receiptSecretByteLen    = 32
)

var (
	ErrInvalidShareToken = errors.New("invalid share token")
	ErrInvalidReceipt    = errors.New("invalid claim receipt")

	// dummySecretCommitment is compared on selector miss so timing does not
	// advertise existence. It must never equal a caller-generated SHA-256.
	dummySecretCommitment = sha256.Sum256([]byte("planshare-dummy-secret-commitment-v1"))
)

// ShareSecretCommitment is the caller-generated SHA-256(secret) the server stores.
type ShareSecretCommitment [sha256.Size]byte

// ClaimReceiptCommitment is the caller-generated SHA-256(receipt secret).
type ClaimReceiptCommitment [sha256.Size]byte

// ParsedShareToken is the strict wire form sp1.<selector>.<secret>.
type ParsedShareToken struct {
	Selector string
	Secret   []byte
}

// ParsedClaimReceipt is the strict wire form cr1.<secret>.
type ParsedClaimReceipt struct {
	Secret []byte
}

func CommitShareSecret(secret []byte) ShareSecretCommitment {
	return sha256.Sum256(secret)
}

func CommitClaimReceipt(secret []byte) ClaimReceiptCommitment {
	return sha256.Sum256(secret)
}

func ParseShareToken(wire string) (ParsedShareToken, error) {
	prefix, rest, ok := strings.Cut(wire, ".")
	if !ok || prefix != shareTokenVersionPrefix {
		return ParsedShareToken{}, ErrInvalidShareToken
	}
	selector, encodedSecret, ok := strings.Cut(rest, ".")
	if !ok || selector == "" || encodedSecret == "" || strings.Contains(encodedSecret, ".") {
		return ParsedShareToken{}, ErrInvalidShareToken
	}
	selectorRaw, err := base64.RawURLEncoding.DecodeString(selector)
	if err != nil || len(selectorRaw) != shareSelectorByteLen {
		return ParsedShareToken{}, ErrInvalidShareToken
	}
	secret, err := base64.RawURLEncoding.DecodeString(encodedSecret)
	if err != nil || len(secret) != shareSecretByteLen {
		return ParsedShareToken{}, ErrInvalidShareToken
	}
	return ParsedShareToken{Selector: selector, Secret: secret}, nil
}

func ParseClaimReceipt(wire string) (ParsedClaimReceipt, error) {
	prefix, encoded, ok := strings.Cut(wire, ".")
	if !ok || prefix != receiptVersionPrefix || encoded == "" || strings.Contains(encoded, ".") {
		return ParsedClaimReceipt{}, ErrInvalidReceipt
	}
	secret, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil || len(secret) != receiptSecretByteLen {
		return ParsedClaimReceipt{}, ErrInvalidReceipt
	}
	return ParsedClaimReceipt{Secret: secret}, nil
}

// CompareShareCommitment performs a constant-time compare against the stored
// commitment. When found is false, a dummy commitment is used so miss paths
// still execute the same compare work.
func CompareShareCommitment(found bool, secret []byte, stored ShareSecretCommitment) bool {
	presented := CommitShareSecret(secret)
	target := dummySecretCommitment
	if found {
		target = stored
	}
	matched := subtle.ConstantTimeCompare(presented[:], target[:]) == 1
	return found && matched
}

func CompareReceiptCommitment(found bool, secret []byte, stored ClaimReceiptCommitment) bool {
	presented := CommitClaimReceipt(secret)
	var dummy ClaimReceiptCommitment = sha256.Sum256([]byte("planshare-dummy-receipt-commitment-v1"))
	target := dummy
	if found {
		target = stored
	}
	matched := subtle.ConstantTimeCompare(presented[:], target[:]) == 1
	return found && matched
}

// ShareFingerprint is a non-usable diagnostic digest for Bearer lists and logs.
func ShareFingerprint(selector string, commitment ShareSecretCommitment) string {
	h := sha256.New()
	_, _ = io.WriteString(h, "planshare-fingerprint-v1\n")
	_, _ = io.WriteString(h, selector)
	_, _ = h.Write([]byte{'\n'})
	_, _ = h.Write(commitment[:])
	return base64.RawURLEncoding.EncodeToString(h.Sum(nil))
}

// EncodeShareSelector encodes 12 random bytes as base64url raw-unpadded.
func EncodeShareSelector(raw []byte) (string, error) {
	if len(raw) != shareSelectorByteLen {
		return "", fmt.Errorf("share selector must be %d bytes", shareSelectorByteLen)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// ComposeShareTokenWire builds the one-time client-side link material.
func ComposeShareTokenWire(selector string, secret []byte) (string, error) {
	if selector == "" || len(secret) != shareSecretByteLen {
		return "", ErrInvalidShareToken
	}
	if _, err := base64.RawURLEncoding.DecodeString(selector); err != nil {
		return "", ErrInvalidShareToken
	}
	return shareTokenVersionPrefix + "." + selector + "." + base64.RawURLEncoding.EncodeToString(secret), nil
}

// ComposeClaimReceiptWire builds the one-time client-side receipt material.
func ComposeClaimReceiptWire(secret []byte) (string, error) {
	if len(secret) != receiptSecretByteLen {
		return "", ErrInvalidReceipt
	}
	return receiptVersionPrefix + "." + base64.RawURLEncoding.EncodeToString(secret), nil
}

func encodeCommitment(commitment ShareSecretCommitment) string {
	return base64.RawURLEncoding.EncodeToString(commitment[:])
}

// DecodeSecretCommitment decodes a base64url raw-unpadded 32-byte SHA-256 commitment.
func DecodeSecretCommitment(wire string) (ShareSecretCommitment, error) {
	raw, err := base64.RawURLEncoding.DecodeString(wire)
	if err != nil || len(raw) != shareSecretByteLen {
		return ShareSecretCommitment{}, validationError("secret_commitment must be 32-byte sha256")
	}
	var out ShareSecretCommitment
	copy(out[:], raw)
	return out, nil
}
