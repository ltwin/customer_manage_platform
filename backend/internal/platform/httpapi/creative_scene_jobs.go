package httpapi

import (
	"time"

	"github.com/gin-gonic/gin"

	"github.com/samson/customer-manage-platform/backend/internal/creativeagent"
	"github.com/samson/customer-manage-platform/backend/internal/creativescene"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
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

// ExportCreativeSceneShots places a photographer's export images beside the
// scene node.
func (h *handlers) ExportCreativeSceneShots(c *gin.Context, id string, _ ExportCreativeSceneShotsParams) {
	h.creativeWrite(c, "document_id", id, creativescene.Export)
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

// GetCreativeSceneRunChanges lists one run's changes to a scene. The run is
// checked first so another account's run, or a missing one, is a 404.
func (h *handlers) GetCreativeSceneRunChanges(c *gin.Context, id string, params GetCreativeSceneRunChangesParams) {
	scope, ok := h.creativeScope(c)
	if !ok {
		return
	}
	creativeRead(c, func() (creativescene.RunChangeSet, error) {
		if params.RunId == "" {
			return creativescene.RunChangeSet{}, creativeops.Explain(creativeops.ErrValidation, "缺少 run_id")
		}
		if err := creativeagent.RequireRun(c.Request.Context(), scope, params.RunId); err != nil {
			return creativescene.RunChangeSet{}, err
		}
		return creativescene.RunChanges(c.Request.Context(), scope, id, params.RunId)
	})
}
