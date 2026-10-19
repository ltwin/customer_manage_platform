package creativescene

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"
	"strings"

	"github.com/samson/customer-manage-platform/backend/internal/creativecanvas"
	"github.com/samson/customer-manage-platform/backend/internal/creativecontent"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
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
	raw, err := json.Marshal(struct {
		V        int    `json:"v"`
		Pass     string `json:"pass"`
		CameraID string `json:"camera_id"`
		Scene    Scene  `json:"scene"`
	}{1, pass, cameraID, p})
	if err != nil {
		return "", false
	}
	sum := sha256.Sum256(raw)
	return fingerprintVersion + ":" + hex.EncodeToString(sum[:]), true
}

// Freshness is the canvas's ExportFreshness: it compares each export image's
// fingerprint with the current snapshot's.
type Freshness struct{}

var _ creativecanvas.ExportFreshness = Freshness{}

// Check decodes each source scene once. A fingerprint of another version can
// only come from a future projection; until one exists it reads as stale.
func (Freshness) Check(_ context.Context, _ store.ReadTxAccountScope, sources []creativecanvas.ExportSource, current map[string]creativecontent.Revision) (map[string]string, error) {
	scenes := map[string]*Scene{}
	out := make(map[string]string, len(sources))
	for _, src := range sources {
		s, ok := scenes[src.DocumentID]
		if !ok {
			if decoded, err := decodeScene(current[src.DocumentID].Payload); err == nil {
				s = &decoded
			}
			scenes[src.DocumentID] = s
		}
		out[src.NodeID] = creativecanvas.BasedOnStale
		if s == nil || !strings.HasPrefix(src.Fingerprint, fingerprintVersion+":") {
			continue
		}
		if fp, ok := Fingerprint(*s, src.CameraObjectID, src.Pass); ok && fp == src.Fingerprint {
			out[src.NodeID] = creativecanvas.BasedOnCurrent
		}
	}
	return out, nil
}
