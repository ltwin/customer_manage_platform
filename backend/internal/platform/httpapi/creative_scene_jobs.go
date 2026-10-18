package httpapi

import (
	"time"

	"github.com/gin-gonic/gin"
	"github.com/samson/customer-manage-platform/backend/internal/creativescene"
)

// Scene client jobs: a render host (an open creative workspace tab) claims,
// renews and completes them. Claim and lease need no idempotency key: a
// repeated claim takes the next job, a repeated lease extends the same one.

func (h *handlers) ClaimCreativeSceneJob(c *gin.Context) {
	scope, ok := h.creativeScope(c)
	if !ok {
		return
	}
	var input creativescene.ClaimInput
	if !readSmallJSON(c, &input) {
		return
	}
	job, err := creativescene.Claim(c.Request.Context(), scope, input)
	if err != nil {
		creativeError(c, err)
		return
	}
	setNoStore(c)
	if job == nil {
		c.Status(204)
		return
	}
	c.JSON(200, gin.H{"job": job})
}

func (h *handlers) LeaseCreativeSceneJob(c *gin.Context, jobID string) {
	scope, ok := h.creativeScope(c)
	if !ok {
		return
	}
	var input struct {
		ClaimToken string `json:"claim_token"`
	}
	if !readSmallJSON(c, &input) {
		return
	}
	until, err := creativescene.Lease(c.Request.Context(), scope, jobID, input.ClaimToken)
	if err != nil {
		creativeError(c, err)
		return
	}
	setNoStore(c)
	c.JSON(200, gin.H{"lease_until": until.UTC().Format(time.RFC3339Nano)})
}

func (h *handlers) CompleteCreativeSceneJob(c *gin.Context, jobID string, _ CompleteCreativeSceneJobParams) {
	h.creativeWrite(c, "job_id", jobID, creativescene.Complete)
}

func (h *handlers) ListCreativeSceneJobs(c *gin.Context, id string) {
	scope, ok := h.creativeScope(c)
	if !ok {
		return
	}
	creativeRead(c, func() (gin.H, error) {
		items, err := creativescene.ListActive(c.Request.Context(), scope, id)
		return gin.H{"items": items}, err
	})
}
