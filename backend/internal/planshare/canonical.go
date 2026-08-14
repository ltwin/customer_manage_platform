package planshare

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/idempotency"
)

func marshalIssueCanonical(
	expectedPlanRevision int64,
	view ViewLevel,
	commitment ShareSecretCommitment,
	expiresAt time.Time,
	source ExpirySourceV1,
	policyVersion string,
) ([]byte, error) {
	return json.Marshal(issueCanonicalV1{
		ExpectedPlanRevision: expectedPlanRevision,
		ViewLevel:            view,
		SecretCommitment:     encodeCommitment(commitment),
		ExpiresAt:            expiresAt.UTC(),
		ExpirySource:         source,
		PolicyVersion:        policyVersion,
	})
}

func marshalRotateCanonical(
	expectedShareRevision int64,
	commitment ShareSecretCommitment,
	expiresAt time.Time,
	source ExpirySourceV1,
	policyVersion string,
) ([]byte, error) {
	return json.Marshal(rotateCanonicalV1{
		ExpectedShareRevision: expectedShareRevision,
		NewSecretCommitment:   encodeCommitment(commitment),
		ExpiresAt:             expiresAt.UTC(),
		ExpirySource:          source,
		PolicyVersion:         policyVersion,
	})
}

func marshalRevokeCanonical(expectedShareRevision int64, policyVersion string) ([]byte, error) {
	return json.Marshal(revokeCanonicalV1{
		ExpectedShareRevision: expectedShareRevision,
		PolicyVersion:         policyVersion,
	})
}

type issueCanonicalV1 struct {
	ExpectedPlanRevision int64          `json:"expected_plan_revision"`
	ViewLevel            ViewLevel      `json:"view_level"`
	SecretCommitment     string         `json:"secret_commitment"`
	ExpiresAt            time.Time      `json:"expires_at"`
	ExpirySource         ExpirySourceV1 `json:"expiry_source"`
	PolicyVersion        string         `json:"policy_version"`
}

type rotateCanonicalV1 struct {
	ExpectedShareRevision int64          `json:"expected_share_revision"`
	NewSecretCommitment   string         `json:"new_secret_commitment"`
	ExpiresAt             time.Time      `json:"expires_at"`
	ExpirySource          ExpirySourceV1 `json:"expiry_source"`
	PolicyVersion         string         `json:"policy_version"`
}

type revokeCanonicalV1 struct {
	ExpectedShareRevision int64  `json:"expected_share_revision"`
	PolicyVersion         string `json:"policy_version"`
}

func issueResource(planID string, view ViewLevel) idempotency.ResourceIdentity {
	return idempotency.ShareIssueResource(planID, string(view))
}

func generationResource(planID, shareID string) idempotency.ResourceIdentity {
	return idempotency.ShareGenerationResource(planID, shareID)
}

func decodeIssueResult(body []byte) (ShareIssueResultV1, error) {
	var result ShareIssueResultV1
	if err := json.Unmarshal(body, &result); err != nil {
		return ShareIssueResultV1{}, fmt.Errorf("decode share issue replay: %w", err)
	}
	return result, nil
}

func decodeRevokeResult(body []byte) (ShareRevokeResultV1, error) {
	var result ShareRevokeResultV1
	if err := json.Unmarshal(body, &result); err != nil {
		return ShareRevokeResultV1{}, fmt.Errorf("decode share revoke replay: %w", err)
	}
	return result, nil
}
