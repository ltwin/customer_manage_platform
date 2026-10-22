package creativescene

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"
	"strings"

	"github.com/samson/customer-manage-platform/backend/internal/creativecanvas"
	"github.com/samson/customer-manage-platform/backend/internal/creativecontent"
)

// An export image is stale once the scene changes in a way that would render
// it differently. The render fingerprint hashes what one camera and one pass
// would draw (design「过时判断」): the whole scene, with what that pass cannot
// show cleared. Clearing rather than listing fields means a field added later
// counts by default.

const (
	PassClay = "clay"
	PassLine = "line"
	PassLit  = "lit"

	fingerprintVersion = "v1"
)

// Passes in node column order.
var Passes = []string{PassClay, PassLine, PassLit}

// Fingerprint is the render fingerprint of camera cameraID drawn as pass. ok
// is false when the scene has no such camera. s comes from decodeScene, so
// its cameras are complete.
func Fingerprint(s Scene, cameraID, pass string) (string, bool) {
	p := Scene{Environment: s.Environment}
	if pass == PassLine {
		p.Environment = Environment{}
	}
	found := false
	for _, o := range s.Objects {
		if o.Kind == KindCamera && o.ID != cameraID || o.Kind == KindLight && pass != PassLit {
			continue
		}
		o.Name, o.Locked, o.Order = "", false, 0
		if pass != PassLit {
			o.MaterialSlots = nil
		}
		if o.Camera != nil {
			found = true
			c := *o.Camera
			c.Label = nil
			// Depth of field only blurs clay and lit images.
			if pass == PassLine || c.DepthOfField != nil && !*c.DepthOfField {
				c.FStop, c.Focus, c.FocusObjectID = nil, nil, nil
			}
			if pass == PassLine {
				c.DepthOfField = nil
			}
			o.Camera = &c
		}
		p.Objects = append(p.Objects, o)
	}
	if !found {
		return "", false
	}
	slices.SortFunc(p.Objects, func(a, b Object) int { return strings.Compare(a.ID, b.ID) })
	// The render settings never counted, except the exposure and the
	// renderer of a lit image (clay and line art are the workbench's); at 0
	// and the workbench they are left out, so fingerprints from before they
	// existed still match. The output resolution never counts.
	render := fingerprintRender{}
	if pass == PassLit {
		render.ExposureEV = s.Render.ExposureEV
		if s.Render.Engine != EngineWorkbench {
			render.Engine = s.Render.Engine
		}
	}
	raw, err := json.Marshal(struct {
		V        int              `json:"v"`
		Pass     string           `json:"pass"`
		CameraID string           `json:"camera_id"`
		Scene    fingerprintScene `json:"scene"`
	}{1, pass, cameraID, fingerprintScene{p.Environment, render, p.Objects}})
	if err != nil {
		return "", false
	}
	sum := sha256.Sum256(raw)
	return fingerprintVersion + ":" + hex.EncodeToString(sum[:]), true
}

// fingerprintScene is Scene as hashed: the same JSON for the same fields.
type fingerprintScene struct {
	Environment Environment       `json:"environment"`
	Render      fingerprintRender `json:"render"`
	Objects     []Object          `json:"objects"`
}

type fingerprintRender struct {
	Mode       string  `json:"mode"`
	ExposureEV float64 `json:"exposure_ev,omitempty"`
	Engine     string  `json:"engine,omitempty"`
}

// Freshness is the canvas's ExportFreshness: it compares each export image's
// fingerprint with the current snapshot's.
type Freshness struct{}

var _ creativecanvas.ExportFreshness = Freshness{}

// Check decodes each source scene once and fingerprints each (scene,
// camera, pass) once: every re-export adds nodes that share them. A
// fingerprint of another version can only come from a future projection;
// until one exists it reads as stale.
func (Freshness) Check(sources []creativecanvas.ExportSource, current map[string]creativecontent.Revision) map[string]string {
	type shot struct{ doc, camera, pass string }
	scenes := map[string]*Scene{}
	prints := map[shot]string{}
	out := make(map[string]string, len(sources))
	for _, src := range sources {
		out[src.NodeID] = creativecanvas.BasedOnStale
		if !strings.HasPrefix(src.Fingerprint, fingerprintVersion+":") {
			continue
		}
		key := shot{src.DocumentID, src.CameraObjectID, src.Pass}
		fp, ok := prints[key]
		if !ok {
			s, decoded := scenes[src.DocumentID]
			if !decoded {
				if d, err := decodeScene(current[src.DocumentID].Payload); err == nil {
					s = &d
				}
				scenes[src.DocumentID] = s
			}
			if s != nil {
				fp, _ = Fingerprint(*s, src.CameraObjectID, src.Pass)
			}
			prints[key] = fp
		}
		if fp != "" && fp == src.Fingerprint {
			out[src.NodeID] = creativecanvas.BasedOnCurrent
		}
	}
	return out
}
