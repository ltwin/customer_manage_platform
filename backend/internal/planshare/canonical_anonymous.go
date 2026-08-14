package planshare

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"unicode/utf8"

	"github.com/samson/customer-manage-platform/backend/internal/platform/idempotency"
)

const anonymousMutationFrameLabel = "planshare-anonymous-mutation-frame-v1"

// TupleV1 encodes multi-segment secondary identity as base64url(raw JSON string array).
func TupleV1(parts ...string) string {
	raw, err := marshalCanonicalJSON(parts)
	if err != nil {
		panic(fmt.Sprintf("tuple v1 encode: %v", err))
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}

// FeedbackResource is the Bearer disposition resource constructor.
func FeedbackResource(planID, feedbackID string) idempotency.ResourceIdentity {
	return idempotency.FeedbackResource(planID, feedbackID)
}

// AnonymousPlanFeedbackResource binds plan feedback to a token generation.
func AnonymousPlanFeedbackResource(planID, generationID string) idempotency.ResourceIdentity {
	return idempotency.AnonymousPlanFeedbackResource(planID, generationID)
}

// AnonymousShotFeedbackResource binds shot feedback to generation+shot via TupleV1.
func AnonymousShotFeedbackResource(planID, generationID, shotID string) idempotency.ResourceIdentity {
	return idempotency.AnonymousShotFeedbackResource(planID, TupleV1(generationID, shotID))
}

// BuildCanonicalAnonymousMutationFrameV1Bytes freezes the unique anonymous mutation frame.
func BuildCanonicalAnonymousMutationFrameV1Bytes(
	operation string,
	resource idempotency.ResourceIdentity,
	typedBody []byte,
) ([]byte, CanonicalAnonymousMutationFrameV1, error) {
	if operation == "" || resource.Kind() == "" || resource.PrimaryID() == "" {
		return nil, CanonicalAnonymousMutationFrameV1{}, validationError("anonymous mutation frame incomplete")
	}
	if !json.Valid(typedBody) || !utf8.Valid(typedBody) {
		return nil, CanonicalAnonymousMutationFrameV1{}, validationError("anonymous mutation body invalid")
	}
	resourceArr, err := marshalCanonicalJSON([]string{
		resource.Kind(),
		resource.PrimaryID(),
		resource.SecondaryID(),
	})
	if err != nil {
		return nil, CanonicalAnonymousMutationFrameV1{}, err
	}
	opJSON, err := marshalCanonicalJSON(operation)
	if err != nil {
		return nil, CanonicalAnonymousMutationFrameV1{}, err
	}
	var b bytes.Buffer
	b.WriteString(`[`)
	label, err := marshalCanonicalJSON(anonymousMutationFrameLabel)
	if err != nil {
		return nil, CanonicalAnonymousMutationFrameV1{}, err
	}
	b.Write(label)
	b.WriteByte(',')
	b.Write(opJSON)
	b.WriteByte(',')
	b.Write(resourceArr)
	b.WriteByte(',')
	b.Write(typedBody)
	b.WriteByte(']')
	frame := b.Bytes()
	sum := sha256.Sum256(frame)
	return frame, CanonicalAnonymousMutationFrameV1{
		Operation: operation,
		FrameHash: hex.EncodeToString(sum[:]),
	}, nil
}

func ExactFrameFingerprint(frameBytes []byte) []byte {
	sum := sha256.Sum256(frameBytes)
	out := make([]byte, len(sum))
	copy(out, sum[:])
	return out
}

func IdempotencyKeyDigest(key string) []byte {
	sum := sha256.Sum256([]byte(key))
	out := make([]byte, len(sum))
	copy(out, sum[:])
	return out
}

func marshalCanonicalJSON(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	out := buf.Bytes()
	if len(out) > 0 && out[len(out)-1] == '\n' {
		out = out[:len(out)-1]
	}
	if !utf8.Valid(out) {
		return nil, validationError("canonical json invalid utf-8")
	}
	return out, nil
}
