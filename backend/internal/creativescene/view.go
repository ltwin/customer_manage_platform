package creativescene

import (
	"context"
	"encoding/json"

	"github.com/samson/customer-manage-platform/backend/internal/creativecontent"
	"github.com/samson/customer-manage-platform/backend/internal/creativedocument"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// view is the scene branch of a read: the stored snapshot, plus a summary of
// every model revision it references (the revision's own reference rows),
// read in the same snapshot with a fixed number of queries.
func view(ctx context.Context, tx store.ReadTxAccountScope, cur creativedocument.Current) (json.RawMessage, error) {
	scene, err := completedScene(cur.Content.Payload)
	if err != nil {
		return nil, err
	}
	models, err := readModels(ctx, tx, cur.Refs)
	if err != nil {
		return nil, err
	}
	return json.Marshal(map[string]any{"scene": scene, "models": models})
}

// completedScene is the stored snapshot as is, unless a camera, the render
// or a physical sky predates one of its fields: then the snapshot is
// decoded, which completes it, so the response always carries every field.
// A physical sky's night (W3) is checked as a whole: completeSky fills it in
// one go.
func completedScene(p creativecontent.Payload) (json.RawMessage, error) {
	var stored struct {
		Render      storedRender `json:"render"`
		Environment struct {
			Sky struct {
				Kind  string           `json:"kind"`
				Night *json.RawMessage `json:"night"`
			} `json:"sky"`
		} `json:"environment"`
		Objects []struct {
			Camera *Camera `json:"camera"`
		} `json:"objects"`
	}
	if err := json.Unmarshal(p.Scene, &stored); err != nil {
		return nil, err
	}
	sky := stored.Environment.Sky
	complete := stored.Render.complete() && (sky.Kind != SkyPhysical || sky.Night != nil && nightComplete(*sky.Night))
	for _, o := range stored.Objects {
		complete = complete && (o.Camera == nil || cameraComplete(*o.Camera))
	}
	if complete {
		return p.Scene, nil
	}
	s, err := decodeScene(p)
	if err != nil {
		return nil, err
	}
	snap, err := encodeScene(s)
	return snap.Payload.Scene, err
}

// storedRender tells which render fields a stored snapshot holds.
type storedRender struct {
	ExposureEV    *float64 `json:"exposure_ev"`
	Engine        *string  `json:"engine"`
	Resolution    *string  `json:"resolution"`
	ExposureEV100 *float64 `json:"exposure_ev100"`
	ExposureMode  *string  `json:"exposure_mode"`
}

// complete reports whether the render needs no completion before it is shown.
func (r storedRender) complete() bool {
	return r.ExposureEV != nil && r.Engine != nil && r.Resolution != nil && r.ExposureEV100 != nil && r.ExposureMode != nil && *r.ExposureMode != exposureAuto
}

// unitView narrows a read to the listed units. Each object appears once with
// its id and kind plus the fields of every listed unit it has; units the
// revision does not hold are skipped. models covers the model instances whose
// content units are listed.
func unitView(ctx context.Context, tx store.ReadTxAccountScope, s Scene, units []string) (json.RawMessage, error) {
	var listed []Object
	out := map[string]any{}
	hierarchy := false
	fields := map[string]map[string]any{}
	add := func(id string, kv map[string]any) {
		if fields[id] == nil {
			fields[id] = map[string]any{}
		}
		for k, v := range kv {
			fields[id][k] = v
		}
	}
	byID := indexObjects(s.Objects)
	for _, u := range units {
		switch u {
		case unitEnvironment:
			out[unitEnvironment] = s.Environment
			continue
		case unitRender:
			out[unitRender] = s.Render
			continue
		case unitObjects:
			hierarchy = true
			continue
		}
		id, content, ok := objectOfUnit(u)
		i, exists := byID[id]
		if !ok || !exists {
			continue
		}
		o := s.Objects[i]
		if content {
			add(id, map[string]any{"primitive": o.Primitive, "model": o.Model, "material_slots": o.MaterialSlots, "light": o.Light, "camera": o.Camera})
			listed = append(listed, o)
		} else {
			add(id, map[string]any{"name": o.Name, "transform": o.Transform, "visible": o.Visible, "locked": o.Locked})
		}
	}
	if hierarchy {
		for _, o := range s.Objects {
			add(o.ID, map[string]any{"parent_id": o.ParentID, "order": o.Order})
		}
	}
	if len(fields) > 0 {
		objects := make([]map[string]any, 0, len(fields))
		for _, o := range s.Objects {
			if f := fields[o.ID]; f != nil {
				f["id"], f["kind"] = o.ID, o.Kind
				objects = append(objects, f)
			}
		}
		out[unitObjects] = objects
	}
	models, err := readModels(ctx, tx, modelRefs(listed))
	if err != nil {
		return nil, err
	}
	return json.Marshal(map[string]any{"scene": out, "models": models})
}

// summary is what a canvas card shows: object counts, the ground included.
func summary(cur creativedocument.Current) (json.RawMessage, error) {
	var s struct {
		Objects []struct {
			Kind string `json:"kind"`
		} `json:"objects"`
	}
	if err := json.Unmarshal(cur.Content.Payload.Scene, &s); err != nil {
		return nil, err
	}
	counts := map[string]int{"objects": len(s.Objects), "primitives": 0, "lights": 0, "cameras": 0}
	for _, o := range s.Objects {
		switch o.Kind {
		case KindPrimitive:
			counts["primitives"]++
		case KindLight:
			counts["lights"]++
		case KindCamera:
			counts["cameras"]++
		}
	}
	return json.Marshal(counts)
}

// nightComplete reports whether a stored night sky holds every field.
func nightComplete(raw json.RawMessage) bool {
	var n Night
	if json.Unmarshal(raw, &n) != nil {
		return false
	}
	return n.Moon != nil && n.Stars != nil && n.MilkyWay != nil && n.MoonStrength != nil && n.MoonTemperatureK != nil &&
		n.StarsStrength != nil && n.StarsTemperatureK != nil && n.NightLook != nil
}
