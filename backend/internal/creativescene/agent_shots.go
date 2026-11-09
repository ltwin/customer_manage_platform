package creativescene

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"
)

// Shots for the assistant (Epic design「助手写入规则」). edit_scene names a
// shot and the tool layer writes every key S1–S6 need in the same batch, so
// the scene command only checks them (P8), as it does for the editor. The
// assistant says shot, as the photographer does; commands and data say
// marker. Three operations exist only here and expand into set_timeline and
// update_objects: add_shot (S2a, or S2b with copy_from), update_shot (S3)
// and remove_shot (S4); an add_objects item may be only_in_shot (S6).

// shotFrameStep is how far after the last shot a new one goes by default,
// leaving frames between for animation later.
const shotFrameStep = 10

type addShotOp struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Frame          *int   `json:"frame"`
	CameraObjectID string `json:"camera_object_id"`
	CopyFrom       string `json:"copy_from"`
}

type updateShotOp struct {
	ID             string  `json:"id"`
	Name           *string `json:"name"`
	Frame          *int    `json:"frame"`
	CameraObjectID *string `json:"camera_object_id"`
}

type removeShotOp struct {
	ID string `json:"id"`
}

var shotOps = []string{"add_shot", "update_shot", "remove_shot"}

// expandShots turns the assistant's shot operations and its writes at shot
// into scene commands, against s before the batch. It returns the ids of
// the shots added.
func expandShots(ops []json.RawMessage, s Scene, shot string) ([]json.RawMessage, []string, error) {
	structural, keyed, cleared := false, shot != "", false
	byID := indexObjects(s.Objects)
	for i, raw := range ops {
		var head struct {
			Op      string            `json:"op"`
			Objects []json.RawMessage `json:"objects"`
			Updates []json.RawMessage `json:"updates"`
			Moves   []struct {
				ObjectID string `json:"object_id"`
			} `json:"moves"`
		}
		if json.Unmarshal(raw, &head) != nil {
			continue // the scene command reports the shape
		}
		if slices.Contains(shotOps, head.Op) {
			structural = true
		}
		// Keys are in the parent's space: under another parent every shot's
		// position would jump.
		for _, m := range head.Moves {
			if j, ok := byID[m.ObjectID]; ok && slices.ContainsFunc(slices.Collect(maps.Keys(s.Objects[j].Animation)), func(p string) bool { return strings.HasPrefix(p, "transform.") }) {
				return nil, nil, invalid("物件 %s 的位置、旋转或缩放按分镜记了关键帧，改挂会让各分镜里的位置跳变：先清除它的变换动画再改挂", m.ObjectID)
			}
		}
		for _, item := range slices.Concat(head.Objects, head.Updates) {
			var fields map[string]json.RawMessage
			if json.Unmarshal(item, &fields) != nil {
				continue
			}
			// Clearing (S5) is the one animation write of the assistant's own.
			if raw, ok := fields["animation"]; ok {
				if head.Op != "update_objects" || !onlyClear(raw) {
					return nil, nil, invalid("第 %d 个操作：animation 只能写 clear 清除动画；按分镜改属性用 shot，改分镜用 add_shot、update_shot、remove_shot", i+1)
				}
				cleared = true
			}
			if _, ok := fields["only_in_shot"]; ok {
				keyed = true
			}
		}
	}
	switch {
	case cleared && (structural || shot != ""):
		return nil, nil, invalid("清除动画改的是所有分镜共用的值，要单独提交：不带 shot，也不和 add_shot、update_shot、remove_shot 写在同一次修改里")
	case structural && keyed:
		return nil, nil, invalid("add_shot、update_shot、remove_shot 改分镜列表，要单独提交，不能和 shot 或 only_in_shot 写在同一次修改里")
	case structural:
		return expandShotList(ops, s)
	case shot != "":
		out, err := expandAtShot(ops, s, shot)
		return out, nil, err
	}
	return ops, nil, refuseKeyedBaseWrites(ops, s)
}

// onlyClear reports whether an animation write only clears channels.
func onlyClear(raw json.RawMessage) bool {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return false
	}
	_, ok := fields["clear"]
	return ok && len(fields) == 1
}

// shotWork is the shot list and the channels as the shot operations of one
// edit leave them, before they are written as commands.
type shotWork struct {
	scene    Scene
	markers  []Marker
	channels map[string]map[string][]Key // object id → path → keys
}

func newShotWork(s Scene) *shotWork {
	w := &shotWork{scene: s, markers: slices.Clone(s.Timeline.Markers), channels: map[string]map[string][]Key{}}
	for _, o := range s.Objects {
		if len(o.Animation) == 0 {
			continue
		}
		w.channels[o.ID] = map[string][]Key{}
		for path, ch := range o.Animation {
			w.channels[o.ID][path] = slices.Clone(ch.Keys)
		}
	}
	return w
}

func (w *shotWork) marker(id string) (int, error) {
	if id == "" {
		return 0, invalid("缺少分镜 id")
	}
	i := slices.IndexFunc(w.markers, func(m Marker) bool { return m.ID == id })
	if i < 0 {
		return 0, invalid("分镜 %s 不存在", id)
	}
	return i, nil
}

func (w *shotWork) frameTaken(frame int, except string) error {
	if i := slices.IndexFunc(w.markers, func(m Marker) bool { return m.Frame == frame && m.ID != except }); i >= 0 {
		return invalid("第 %d 帧已经是分镜 %s，一个分镜占一帧", frame, w.markers[i].ID)
	}
	return nil
}

// eachChannel visits the channels in object and path order, so the keys
// come out the same every time.
func (w *shotWork) eachChannel(visit func(o Object, spec *channelSpec, keys *[]Key) error) error {
	for _, o := range w.scene.Objects {
		paths := w.channels[o.ID]
		for _, path := range slices.Sorted(maps.Keys(paths)) {
			keys := paths[path]
			spec := channelOf(path, o.Kind)
			if spec == nil {
				continue
			}
			if err := visit(o, spec, &keys); err != nil {
				return err
			}
			paths[path] = keys
		}
	}
	return nil
}

func setKey(keys []Key, k Key) []Key {
	i, found := slices.BinarySearchFunc(keys, k.Frame, func(x Key, f int) int { return x.Frame - f })
	if found {
		keys[i] = k
		return keys
	}
	return slices.Insert(keys, i, k)
}

func deleteKey(keys []Key, frame int) ([]Key, Key, bool) {
	i, found := slices.BinarySearchFunc(keys, frame, func(x Key, f int) int { return x.Frame - f })
	if !found {
		return keys, Key{}, false
	}
	k := keys[i]
	return slices.Delete(keys, i, i+1), k, true
}

func (w *shotWork) addShot(op addShotOp) (string, error) {
	if op.ID == "" {
		op.ID = newMarkerID()
	}
	if slices.ContainsFunc(w.markers, func(m Marker) bool { return m.ID == op.ID }) {
		return "", invalid("分镜 %s 已经存在", op.ID)
	}
	frame := 0
	if len(w.markers) > 0 {
		frame = slices.MaxFunc(w.markers, func(a, b Marker) int { return a.Frame - b.Frame }).Frame + shotFrameStep
	}
	if op.Frame != nil {
		frame = *op.Frame
	}
	if err := w.frameTaken(frame, ""); err != nil {
		return "", err
	}
	// S2b takes the copied shot as it stands, S2a the default state.
	from := -1
	if op.CopyFrom != "" {
		i, err := w.marker(op.CopyFrom)
		if err != nil {
			return "", err
		}
		from = w.markers[i].Frame
	}
	err := w.eachChannel(func(o Object, spec *channelSpec, keys *[]Key) error {
		value, err := baseValue(o, spec)
		if from >= 0 {
			value, err = sampledValue(spec, *keys, from)
		}
		if err != nil {
			return err
		}
		*keys = setKey(*keys, Key{Frame: frame, Value: value})
		return nil
	})
	w.markers = append(w.markers, Marker{ID: op.ID, Name: op.Name, Frame: frame, CameraObjectID: op.CameraObjectID})
	return op.ID, err
}

func (w *shotWork) updateShot(op updateShotOp) error {
	i, err := w.marker(op.ID)
	if err != nil {
		return err
	}
	m := &w.markers[i]
	if op.Name != nil {
		m.Name = *op.Name
	}
	if op.CameraObjectID != nil {
		m.CameraObjectID = *op.CameraObjectID
	}
	if op.Frame == nil || *op.Frame == m.Frame {
		return nil
	}
	if err := w.frameTaken(*op.Frame, m.ID); err != nil {
		return err
	}
	// S3: each channel's key at the shot moves with it.
	old, frame := m.Frame, *op.Frame
	m.Frame = frame
	return w.eachChannel(func(_ Object, _ *channelSpec, keys *[]Key) error {
		var k Key
		var found bool
		if *keys, k, found = deleteKey(*keys, old); found {
			k.Frame = frame
			*keys = setKey(*keys, k)
		}
		return nil
	})
}

func (w *shotWork) removeShot(op removeShotOp) error {
	i, err := w.marker(op.ID)
	if err != nil {
		return err
	}
	frame := w.markers[i].Frame
	w.markers = slices.Delete(w.markers, i, i+1)
	// S4: the keys at the shot go with it.
	return w.eachChannel(func(_ Object, _ *channelSpec, keys *[]Key) error {
		*keys, _, _ = deleteKey(*keys, frame)
		return nil
	})
}

// patches are the animation writes that take each object's channels from
// the scene's to the work's: keys gone are deleted, keys new or changed set.
func (w *shotWork) patches() map[string]animationPatch {
	out := map[string]animationPatch{}
	for _, o := range w.scene.Objects {
		var p animationPatch
		for _, path := range slices.Sorted(maps.Keys(w.channels[o.ID])) {
			was, now := o.Animation[path].Keys, w.channels[o.ID][path]
			for _, k := range was {
				if !slices.ContainsFunc(now, func(n Key) bool { return n.Frame == k.Frame }) {
					p.Delete = append(p.Delete, keyFrame{Path: path, Frame: k.Frame})
				}
			}
			for _, k := range now {
				if !slices.ContainsFunc(was, func(x Key) bool { return x.Frame == k.Frame && bytes.Equal(x.Value, k.Value) && x.Interp == k.Interp }) {
					p.Set = append(p.Set, keyWrite{Path: path, Frame: k.Frame, Value: k.Value, Interp: k.Interp})
				}
			}
		}
		if len(p.Delete) > 0 || len(p.Set) > 0 {
			out[o.ID] = p
		}
	}
	return out
}

// expandShotList runs the shot operations in order and writes the result as
// one set_timeline, where the first of them was, and the keys they moved,
// merged into the batch's update_objects so each object unit is written once.
func expandShotList(ops []json.RawMessage, s Scene) ([]json.RawMessage, []string, error) {
	w := newShotWork(s)
	var added []string
	out := make([]json.RawMessage, 0, len(ops)+1)
	timelineAt := -1
	for i, raw := range ops {
		var head struct {
			Op string `json:"op"`
		}
		_ = json.Unmarshal(raw, &head)
		var err error
		switch head.Op {
		case "add_shot":
			var op addShotOp
			if err = decodeShotOp(raw, &op, i); err == nil {
				var id string
				id, err = w.addShot(op)
				added = append(added, id)
			}
		case "update_shot":
			var op updateShotOp
			if err = decodeShotOp(raw, &op, i); err == nil {
				err = w.updateShot(op)
			}
		case "remove_shot":
			var op removeShotOp
			if err = decodeShotOp(raw, &op, i); err == nil {
				err = w.removeShot(op)
			}
		default:
			out = append(out, raw)
			continue
		}
		if err != nil {
			return nil, nil, err
		}
		if timelineAt < 0 {
			timelineAt = len(out)
			out = append(out, nil)
		}
	}
	slices.SortStableFunc(w.markers, func(a, b Marker) int { return a.Frame - b.Frame })
	timeline, err := json.Marshal(map[string]any{"op": "set_timeline", "timeline": map[string]any{"markers": w.markers}})
	if err != nil {
		return nil, nil, err
	}
	out[timelineAt] = timeline
	out, err = mergeAnimation(out, w.patches())
	return out, added, err
}

func decodeShotOp(raw json.RawMessage, op any, i int) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return invalid("第 %d 个操作格式不合法", i+1)
	}
	delete(fields, "op")
	rest, _ := json.Marshal(fields)
	dec := json.NewDecoder(bytes.NewReader(rest))
	dec.DisallowUnknownFields()
	if err := dec.Decode(op); err != nil {
		return invalid("第 %d 个操作：%v", i+1, err)
	}
	return nil
}

// mergeAnimation adds patches to the batch's first update_objects, to the
// object's own item when it has one, or as a new update_objects at the end.
func mergeAnimation(ops []json.RawMessage, patches map[string]animationPatch) ([]json.RawMessage, error) {
	if len(patches) == 0 {
		return ops, nil
	}
	at := slices.IndexFunc(ops, func(raw json.RawMessage) bool {
		var head struct {
			Op string `json:"op"`
		}
		return json.Unmarshal(raw, &head) == nil && head.Op == "update_objects"
	})
	op := map[string]any{"op": "update_objects", "updates": []any{}}
	var items []map[string]any
	if at >= 0 {
		var decoded struct {
			Updates []map[string]any `json:"updates"`
		}
		if err := json.Unmarshal(ops[at], &decoded); err != nil {
			return nil, err
		}
		items = decoded.Updates
	}
	for _, id := range slices.Sorted(maps.Keys(patches)) {
		i := slices.IndexFunc(items, func(it map[string]any) bool { return it["object_id"] == id })
		if i < 0 {
			items = append(items, map[string]any{"object_id": id})
			i = len(items) - 1
		}
		items[i]["animation"] = patches[id]
	}
	op["updates"] = items
	raw, err := json.Marshal(op)
	if err != nil {
		return nil, err
	}
	if at < 0 {
		return append(ops, raw), nil
	}
	out := slices.Clone(ops)
	out[at] = raw
	return out, nil
}

// expandAtShot writes the batch's whitelisted fields into shot: a property
// with a channel gets a key at the shot's frame, one without gets its first
// channel (S1: the other shots keep the value from before, as does the
// default state); a value the shot already shows is left alone. An
// only_in_shot object is hidden everywhere but at the shot (S6).
func expandAtShot(ops []json.RawMessage, s Scene, shot string) ([]json.RawMessage, error) {
	mi := slices.IndexFunc(s.Timeline.Markers, func(m Marker) bool { return m.ID == shot })
	if mi < 0 {
		return nil, invalid("分镜 %s 不存在：read_scene 的 timeline 里列着现有的分镜", shot)
	}
	frame := s.Timeline.Markers[mi].Frame
	shown := EvaluateAt(s, frame).Objects
	byID := indexObjects(s.Objects)
	out := make([]json.RawMessage, 0, len(ops))
	for _, raw := range ops {
		var op map[string]json.RawMessage
		var head struct {
			Op string `json:"op"`
		}
		if json.Unmarshal(raw, &op) != nil || json.Unmarshal(raw, &head) != nil {
			out = append(out, raw)
			continue
		}
		key := map[string]string{"add_objects": "objects", "update_objects": "updates"}[head.Op]
		var items []map[string]json.RawMessage
		if key == "" || json.Unmarshal(op[key], &items) != nil {
			out = append(out, raw)
			continue
		}
		kept := items[:0]
		for _, item := range items {
			var err error
			if head.Op == "add_objects" {
				err = onlyInShot(item, s.Timeline.Markers, shot)
			} else {
				var u objectUpdate
				_ = json.Unmarshal(mustJSON(item), &u)
				if i, ok := byID[u.ObjectID]; ok {
					err = keyAtShot(item, u, s.Objects[i], shown[i], s.Timeline.Markers, frame)
				}
			}
			if err != nil {
				return nil, err
			}
			if len(item) > 1 || head.Op == "add_objects" {
				kept = append(kept, item)
			}
		}
		if len(kept) == 0 {
			continue
		}
		op[key] = mustJSON(kept)
		out = append(out, mustJSON(op))
	}
	return out, nil
}

// onlyInShot is S6 for one add_objects item.
func onlyInShot(item map[string]json.RawMessage, markers []Marker, shot string) error {
	raw, ok := item["only_in_shot"]
	if !ok {
		return nil
	}
	delete(item, "only_in_shot")
	var only bool
	if json.Unmarshal(raw, &only) != nil {
		return invalid("only_in_shot 须为 true 或 false")
	}
	if !only {
		return nil
	}
	keys := make([]Key, 0, len(markers))
	for _, m := range markers {
		keys = append(keys, Key{Frame: m.Frame, Value: mustJSON(m.ID == shot), Interp: interpConstant})
	}
	item["visible"] = mustJSON(false)
	item["animation"] = mustJSON(map[string]Channel{"visible": {Keys: keys}})
	return nil
}

// keyAtShot rewrites one update_objects item written at the shot at frame.
// o is the object as stored, shown as the shot shows it.
func keyAtShot(item map[string]json.RawMessage, u objectUpdate, o, shown Object, markers []Marker, frame int) error {
	written := writtenChannels(u)
	if len(written) == 0 {
		return nil
	}
	var p animationPatch
	for _, path := range slices.Sorted(maps.Keys(written)) {
		spec := channelOf(path, o.Kind)
		if spec == nil {
			continue // the scene command refuses what the object has not
		}
		value := written[path]
		_, keyed := o.Animation[path]
		now, err := baseValue(shown, spec)
		if err != nil {
			return err
		}
		if !keyed && jsonEqual(now, value) {
			continue
		}
		if !keyed {
			before, err := baseValue(o, spec)
			if err != nil {
				return err
			}
			for _, m := range markers {
				if m.Frame != frame {
					p.Set = append(p.Set, keyWrite{Path: path, Frame: m.Frame, Value: before})
				}
			}
		}
		p.Set = append(p.Set, keyWrite{Path: path, Frame: frame, Value: value})
	}
	// The written fields go into keys, never into the default state.
	delete(item, "transform")
	delete(item, "visible")
	if u.Camera != nil {
		var cam map[string]json.RawMessage
		_ = json.Unmarshal(item["camera"], &cam)
		for path := range written {
			if field, ok := strings.CutPrefix(path, "camera."); ok {
				delete(cam, field)
			}
		}
		if len(cam) == 0 {
			delete(item, "camera")
		} else {
			item["camera"] = mustJSON(cam)
		}
	}
	if len(p.Set) > 0 {
		item["animation"] = mustJSON(p)
	}
	return nil
}

// refuseKeyedBaseWrites is the rule for writes without a shot: changing the
// base value of a property with a channel would be overridden at every key,
// so the tool refuses it and says what to do. A channel cleared in the same
// item goes, and its base value is what every shot shows.
func refuseKeyedBaseWrites(ops []json.RawMessage, s Scene) error {
	byID := indexObjects(s.Objects)
	var keyed []string
	for _, raw := range ops {
		var op struct {
			Op      string         `json:"op"`
			Updates []objectUpdate `json:"updates"`
			Objects []struct {
				OnlyInShot *bool `json:"only_in_shot"`
			} `json:"objects"`
		}
		if json.Unmarshal(raw, &op) != nil {
			continue
		}
		for _, o := range op.Objects {
			if o.OnlyInShot != nil {
				return invalid("only_in_shot 要和 shot 一起用：shot 指明物件只出现在哪个分镜")
			}
		}
		for _, u := range op.Updates {
			i, ok := byID[u.ObjectID]
			if !ok {
				continue
			}
			o, written := s.Objects[i], writtenChannels(u)
			for _, path := range slices.Sorted(maps.Keys(written)) {
				spec := channelOf(path, o.Kind)
				if _, has := o.Animation[path]; !has || spec == nil || u.Animation != nil && slices.Contains(u.Animation.Clear, path) {
					continue
				}
				if base, err := baseValue(o, spec); err == nil && !jsonEqual(base, written[path]) {
					keyed = append(keyed, fmt.Sprintf("%s 的 %s", o.ID, path))
				}
			}
		}
	}
	if len(keyed) > 0 {
		return invalid("这些属性按分镜记了关键帧，不指明 shot 改的是默认状态，各分镜仍按关键帧显示：%s。指定 shot 写进那个分镜；要所有分镜一致，先用 update_objects 的 animation:{clear:[路径]} 清除动画（基础值同时写成要的值）", strings.Join(keyed, "、"))
	}
	return nil
}

// writtenChannels are the whitelisted properties u writes, as values.
func writtenChannels(u objectUpdate) map[string]json.RawMessage {
	o := Object{Visible: u.Visible != nil && *u.Visible, Camera: u.Camera}
	if u.Transform != nil {
		o.Transform = *u.Transform
	}
	out := map[string]json.RawMessage{}
	for path, spec := range channelByPath {
		switch {
		case spec.camera && u.Camera == nil, path == "visible" && u.Visible == nil, strings.HasPrefix(path, "transform.") && u.Transform == nil:
			continue
		}
		field := reflect.ValueOf(spec.field(&o)).Elem()
		if (field.Kind() == reflect.Pointer || field.Kind() == reflect.Slice) && field.IsNil() {
			continue
		}
		out[path] = mustJSON(field.Interface())
	}
	return out
}

// baseValue is o's value of the property spec keys, as JSON.
func baseValue(o Object, spec *channelSpec) (json.RawMessage, error) {
	if spec.camera && o.Camera == nil {
		return nil, invalid("物件 %s 不是摄像机", o.ID)
	}
	return json.Marshal(reflect.ValueOf(spec.field(&o)).Elem().Interface())
}

// sampledValue is a channel's value at frame, as JSON.
func sampledValue(spec *channelSpec, keys []Key, frame int) (json.RawMessage, error) {
	raw, nums, err := sampleKeys(spec.sample, keys, frame)
	switch {
	case err != nil || nums == nil:
		return raw, err
	case spec.sample == sampleNumber:
		return json.Marshal(nums[0])
	}
	return json.Marshal(nums)
}

func jsonEqual(a, b json.RawMessage) bool {
	var x, y any
	return json.Unmarshal(a, &x) == nil && json.Unmarshal(b, &y) == nil && reflect.DeepEqual(x, y)
}

func mustJSON(v any) json.RawMessage {
	raw, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return raw
}

func newMarkerID() string { return "smk_" + newObjectID()[len("sobj_"):] }
