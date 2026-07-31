package auth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

const (
	accessSigningLabel = "crm-auth/v1/access-signing"
	replayAEADLabel    = "crm-auth/v1/refresh-replay-aead"
	replayEnvelopeV1   = byte(1)
	authSecretBytes    = 32
)

func deriveKey(rootSecret, label string) []byte {
	// RFC 5869：salt=nil 等价于 HashLen 个 0；本协议只取 32 bytes，
	// 因此 Expand 只有 T(1)=HMAC(PRK, info || 0x01)。
	extract := hmac.New(sha256.New, make([]byte, sha256.Size))
	if _, err := extract.Write([]byte(rootSecret)); err != nil {
		panic("auth: HMAC extract write failed")
	}
	prk := extract.Sum(nil)
	expand := hmac.New(sha256.New, prk)
	if _, err := expand.Write([]byte(label)); err != nil {
		panic("auth: HMAC expand info write failed")
	}
	if _, err := expand.Write([]byte{1}); err != nil {
		panic("auth: HMAC expand counter write failed")
	}
	return expand.Sum(nil)
}

func randomID(reader io.Reader) (string, error) {
	raw := make([]byte, 16)
	if _, err := io.ReadFull(reader, raw); err != nil {
		return "", fmt.Errorf("generate id: %w", err)
	}
	return hex.EncodeToString(raw), nil
}

func newBearerToken(reader io.Reader) (wire, selector string, hash []byte, err error) {
	selector, err = randomID(reader)
	if err != nil {
		return "", "", nil, err
	}
	secret := make([]byte, authSecretBytes)
	if _, err := io.ReadFull(reader, secret); err != nil {
		return "", "", nil, fmt.Errorf("generate bearer secret: %w", err)
	}
	wire = selector + "." + base64.RawURLEncoding.EncodeToString(secret)
	digest := sha256.Sum256(secret)
	return wire, selector, digest[:], nil
}

func parseBearerToken(wire string) (string, []byte, error) {
	selector, encoded, found := strings.Cut(wire, ".")
	if !found || selector == "" || encoded == "" || strings.Contains(encoded, ".") {
		return "", nil, ErrInvalidOrExpiredActionToken
	}
	secret, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil || len(secret) != authSecretBytes {
		return "", nil, ErrInvalidOrExpiredActionToken
	}
	digest := sha256.Sum256(secret)
	return selector, digest[:], nil
}

type ReplayCipher struct {
	aead   cipher.AEAD
	random io.Reader
}

func NewReplayCipher(rootSecret string, random io.Reader) (*ReplayCipher, error) {
	return newReplayCipher(deriveKey(rootSecret, replayAEADLabel), random)
}

func newReplayCipher(key []byte, random io.Reader) (*ReplayCipher, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("create replay cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create replay AEAD: %w", err)
	}
	return &ReplayCipher{aead: aead, random: random}, nil
}

func (c *ReplayCipher) Seal(parentGenerationID, familyID string, replayUntil time.Time, successor string) ([]byte, error) {
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := io.ReadFull(c.random, nonce); err != nil {
		return nil, fmt.Errorf("generate replay nonce: %w", err)
	}
	envelope := make([]byte, 1+len(nonce))
	envelope[0] = replayEnvelopeV1
	copy(envelope[1:], nonce)
	return c.aead.Seal(envelope, nonce, []byte(successor), replayAAD(parentGenerationID, familyID, replayUntil)), nil
}

func (c *ReplayCipher) Open(parentGenerationID, familyID string, replayUntil time.Time, envelope []byte) (string, error) {
	minimum := 1 + c.aead.NonceSize() + c.aead.Overhead()
	if len(envelope) < minimum || envelope[0] != replayEnvelopeV1 {
		return "", errors.New("invalid replay envelope")
	}
	nonceEnd := 1 + c.aead.NonceSize()
	plain, err := c.aead.Open(nil, envelope[1:nonceEnd], envelope[nonceEnd:], replayAAD(parentGenerationID, familyID, replayUntil))
	if err != nil {
		return "", errors.New("invalid replay envelope")
	}
	return string(plain), nil
}

func replayAAD(parentGenerationID, familyID string, replayUntil time.Time) []byte {
	parent := []byte(parentGenerationID)
	family := []byte(familyID)
	aad := make([]byte, 1+4+len(parent)+4+len(family)+8)
	aad[0] = replayEnvelopeV1
	offset := 1
	binary.BigEndian.PutUint32(aad[offset:offset+4], uint32(len(parent)))
	offset += 4
	copy(aad[offset:], parent)
	offset += len(parent)
	binary.BigEndian.PutUint32(aad[offset:offset+4], uint32(len(family)))
	offset += 4
	copy(aad[offset:], family)
	offset += len(family)
	binary.BigEndian.PutUint64(aad[offset:], uint64(replayUntil.UTC().Unix()))
	return aad
}
