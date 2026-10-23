package creativescene

import (
	"context"
	"encoding/json"
	"math"

	"github.com/samson/customer-manage-platform/backend/internal/creativedocument"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// ShotExposure is how a lit screenshot under a physical sky was exposed
// (Epic revision v5): by the scene camera it was taken from, or by the
// default camera for a viewpoint or a preset. render_view reports it so the
// assistant knows that a viewpoint's picture is only as bright as a camera's
// when both are exposed alike (owner 2026-10-23).
type ShotExposure struct {
	// By is camera or default_camera.
	By   string `json:"by"`
	Mode string `json:"mode"`
	// Compensation (stops) exposes an ambient-metered one, EV100 a manual one.
	Compensation *float64 `json:"compensation,omitempty"`
	EV100        *float64 `json:"ev100,omitempty"`
}

// RenderExposure is a render_view request's exposure in the scene it drew;
// nil for clay, under another sky, or for a camera no longer in the scene.
func RenderExposure(s Scene, req RenderRequest) *ShotExposure {
	if req.Mode != PassLit || s.Environment.Sky.Kind != SkyPhysical {
		return nil
	}
	if req.CameraObjectID == "" {
		r := s.Render
		if r.ExposureMode == ExposureManual {
			return &ShotExposure{By: "default_camera", Mode: ExposureManual, EV100: &r.ExposureEV100}
		}
		return &ShotExposure{By: "default_camera", Mode: ExposureAmbient, Compensation: &r.ExposureCompensation}
	}
	for _, o := range s.Objects {
		if o.ID != req.CameraObjectID || o.Camera == nil {
			continue
		}
		c := o.Camera
		if *c.ExposureMode == ExposureManual {
			// EV100 = log2(N² / t) − log2(S / 100), to a hundredth of a stop.
			ev := math.Round((math.Log2(*c.FStop**c.FStop / *c.ShutterS)-math.Log2(*c.ISO/100))*100) / 100
			return &ShotExposure{By: "camera", Mode: ExposureManual, EV100: &ev}
		}
		return &ShotExposure{By: "camera", Mode: ExposureAmbient, Compensation: c.ExposureCompensation}
	}
	return nil
}

// RenderExposureInTx is a finished render_view job's RenderExposure at the
// scene revision its screenshot shows.
func RenderExposureInTx(ctx context.Context, tx store.TxAccountScope, job Job, revision creativeops.Revision) (*ShotExposure, error) {
	var req RenderRequest
	if err := json.Unmarshal(job.Request, &req); err != nil {
		return nil, err
	}
	if req.Mode != PassLit {
		return nil, nil
	}
	cur, err := creativedocument.ReadAtInTx(ctx, tx, job.DocumentID, TypeKey, int64(revision))
	if err != nil {
		return nil, err
	}
	s, err := decodeScene(cur.Content.Payload)
	if err != nil {
		return nil, err
	}
	return RenderExposure(s, req), nil
}
