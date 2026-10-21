// Package creativescene is the scene document type: a 3D arrangement of
// objects edited by hand and by the assistant. It plugs into
// creativedocument, which owns revisions, per-unit concurrency, history and
// trash; this package owns the payload, its units, operations and restore.
package creativescene

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/samson/customer-manage-platform/backend/internal/creativecontent"
	"github.com/samson/customer-manage-platform/backend/internal/creativedocument"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

const (
	TypeKey     = "scene"
	NodeTypeKey = "core.scene"
	contentKind = "scene"
)

type sceneType struct{}

// init registers the type; cmd/server and cmd/creative-worker import the
// package for this side effect.
func init() { creativedocument.Register(sceneType{}) }

func (sceneType) Key() string         { return TypeKey }
func (sceneType) NodeTypeKey() string { return NodeTypeKey }
func (sceneType) ContentKind() string { return contentKind }
func (sceneType) Creatable() bool     { return true }

// newScene is the first snapshot: a procedural sky and one locked ground.
func newScene() Scene {
	elevation, azimuth, turbidity := 35.0, 135.0, 3.0
	ground := builtins["ground@1"]
	return Scene{
		Environment: Environment{
			Sky:     Sky{Kind: "procedural", SunElevationDeg: &elevation, SunAzimuthDeg: &azimuth, Turbidity: &turbidity},
			Ambient: Ambient{Color: "#ffffff", Intensity: 0.4},
		},
		Render: Render{Mode: "clay", Engine: EngineWorkbench, Resolution: defaultResolution},
		Objects: []Object{{
			ID:        newObjectID(),
			Kind:      KindPrimitive,
			Name:      "地面",
			Transform: Transform{Position: []float64{0, 0, 0}, Rotation: []float64{0, 0, 0, 1}, Scale: []float64{1, 1, 1}},
			Visible:   true,
			Locked:    true,
			Primitive: &Primitive{Builtin: ground.Builtin, Params: ground.defaults()},
		}},
	}
}

func (sceneType) Empty(_ context.Context, _ store.TxAccountScope, _ string, opts json.RawMessage) (creativedocument.Snapshot, error) {
	if trimmed := bytes.TrimSpace(opts); len(trimmed) > 0 && !bytes.Equal(trimmed, []byte("null")) && !bytes.Equal(trimmed, []byte("{}")) {
		return creativedocument.Snapshot{}, invalid("新建场景不接受 document_options")
	}
	return encodeScene(newScene())
}

func (sceneType) Units(p creativecontent.Payload) ([]string, error) {
	s, err := decodeScene(p)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, 3+2*len(s.Objects))
	out = append(out, unitEnvironment, unitRender, unitObjects)
	for _, o := range s.Objects {
		out = append(out, placementUnit(o.ID), contentUnit(o.ID))
	}
	return out, nil
}

func (sceneType) Touched(_ context.Context, _ store.TxAccountScope, cur creativedocument.Current, _ creativedocument.Actor, raws []json.RawMessage) ([]string, error) {
	s, err := decodeScene(cur.Content.Payload)
	if err != nil {
		return nil, err
	}
	ops, err := decodeOps(raws)
	if err != nil {
		return nil, err
	}
	byID := indexObjects(s.Objects)
	var out []string
	for _, op := range ops {
		units, err := op.touched(s, byID)
		if err != nil {
			return nil, err
		}
		out = append(out, units...)
	}
	return out, nil
}

func (sceneType) Apply(ctx context.Context, tx store.TxAccountScope, cur creativedocument.Current, _ creativedocument.Actor, retain creativedocument.Retain, raws []json.RawMessage) (creativedocument.Snapshot, creativedocument.Changes, error) {
	s, err := decodeScene(cur.Content.Payload)
	if err != nil {
		return creativedocument.Snapshot{}, creativedocument.Changes{}, err
	}
	ops, err := decodeOps(raws)
	if err != nil {
		return creativedocument.Snapshot{}, creativedocument.Changes{}, err
	}
	env := newOpEnv(ctx, tx, retain, cur)
	if err := env.verifyExisting(ops); err != nil {
		return creativedocument.Snapshot{}, creativedocument.Changes{}, err
	}
	c := newChangeSet()
	for _, op := range ops {
		if err := op.apply(env, &s, c); err != nil {
			return creativedocument.Snapshot{}, creativedocument.Changes{}, err
		}
	}
	response, err := env.finish(s)
	if err != nil {
		return creativedocument.Snapshot{}, creativedocument.Changes{}, err
	}
	if err := checkFocusTargets(&s, c.focus); err != nil {
		return creativedocument.Snapshot{}, creativedocument.Changes{}, err
	}
	if err := Validate(s); err != nil {
		return creativedocument.Snapshot{}, creativedocument.Changes{}, err
	}
	snap, err := encodeScene(s)
	changes := c.result()
	changes.Response = response
	return snap, changes, err
}

func (sceneType) RestoreTouched(_ context.Context, _ store.TxAccountScope, old, cur creativedocument.Current, units []string) ([]string, error) {
	o, err := decodeScene(old.Content.Payload)
	if err != nil {
		return nil, err
	}
	c, err := decodeScene(cur.Content.Payload)
	if err != nil {
		return nil, err
	}
	return restoreTouched(o, c, units)
}

func (sceneType) RestoreUnits(_ context.Context, _ store.TxAccountScope, old, cur creativedocument.Current, _ creativedocument.Retain, units []string) (creativedocument.Snapshot, creativedocument.Changes, error) {
	o, err := decodeScene(old.Content.Payload)
	if err != nil {
		return creativedocument.Snapshot{}, creativedocument.Changes{}, err
	}
	c, err := decodeScene(cur.Content.Payload)
	if err != nil {
		return creativedocument.Snapshot{}, creativedocument.Changes{}, err
	}
	next, changes, err := restoreUnits(o, c, units)
	if err != nil {
		return creativedocument.Snapshot{}, creativedocument.Changes{}, err
	}
	snap, err := encodeScene(next)
	return snap, changes, err
}

func (sceneType) View(ctx context.Context, tx store.ReadTxAccountScope, cur creativedocument.Current, units []string) (json.RawMessage, error) {
	if units == nil {
		return view(ctx, tx, cur)
	}
	s, err := decodeScene(cur.Content.Payload)
	if err != nil {
		return nil, err
	}
	return unitView(ctx, tx, s, units)
}

func (sceneType) Summary(_ context.Context, _ store.ReadTxAccountScope, cur creativedocument.Current) (json.RawMessage, error) {
	return summary(cur)
}
