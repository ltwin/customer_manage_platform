package creativescene

import (
	"encoding/json"
	"fmt"
	"math"
)

// prepareAgentObjects accepts the assistant's shorter input without changing
// the full object shape required by document commands.
func prepareAgentObjects(ops []json.RawMessage, scene Scene) ([]json.RawMessage, []string, error) {
	var added []string
	out, err := rewriteAgentObjects(ops, func(add bool, items []map[string]json.RawMessage) error {
		if add {
			var err error
			added, err = completeAgentAdds(items, scene)
			if err != nil {
				return err
			}
		}
		for i, item := range items {
			path := fmt.Sprintf("updates[%d]", i)
			if add {
				path = fmt.Sprintf("objects[%d]", i)
			}
			if err := prepareAgentTransform(item, path, add); err != nil {
				return err
			}
		}
		return nil
	})
	return out, added, err
}

func rewriteAgentObjects(ops []json.RawMessage, edit func(bool, []map[string]json.RawMessage) error) ([]json.RawMessage, error) {
	out := make([]json.RawMessage, len(ops))
	for i, raw := range ops {
		out[i] = raw
		var op map[string]json.RawMessage
		if json.Unmarshal(raw, &op) != nil {
			continue
		}
		var kind string
		_ = json.Unmarshal(op["op"], &kind)
		key := map[string]string{"add_objects": "objects", "update_objects": "updates"}[kind]
		if key == "" {
			continue
		}
		var items []map[string]json.RawMessage
		if json.Unmarshal(op[key], &items) != nil || len(items) == 0 || len(items) > maxObjects {
			continue // the document command reports the invalid operation
		}
		for j, item := range items {
			if item == nil {
				return nil, invalid("第 %d 个操作的 %s[%d] 须为对象", i+1, key, j)
			}
		}
		if err := edit(kind == "add_objects", items); err != nil {
			return nil, err
		}
		op[key] = mustJSON(items)
		out[i] = mustJSON(op)
	}
	return out, nil
}

func completeAgentAdds(items []map[string]json.RawMessage, scene Scene) ([]string, error) {
	used := make(map[string]bool, len(scene.Objects)+len(items))
	highest := make(map[string]int)
	reserve := func(parent string, order int) {
		if prior, ok := highest[parent]; !ok || order > prior {
			highest[parent] = order
		}
	}
	for _, o := range scene.Objects {
		used[o.ID] = true
		parent := ""
		if o.ParentID != nil {
			parent = *o.ParentID
		}
		reserve(parent, o.Order)
	}
	parents := make([]string, len(items))
	ids := make([]string, len(items))
	for i, item := range items {
		if raw, ok := item["parent_id"]; ok {
			if json.Unmarshal(raw, &parents[i]) != nil {
				return nil, invalid("objects[%d].parent_id 须为场景中的分组 ID 或 null", i)
			}
		} else {
			item["parent_id"] = json.RawMessage("null")
		}
		if raw, ok := item["order"]; ok {
			var order *int
			if json.Unmarshal(raw, &order) != nil || order == nil {
				return nil, invalid("objects[%d].order 须为整数，也可省略后排到同级末尾", i)
			}
			reserve(parents[i], *order)
		}
		if raw, ok := item["id"]; ok {
			if json.Unmarshal(raw, &ids[i]) != nil || !objectIDPattern.MatchString(ids[i]) {
				return nil, invalid("objects[%d].id 不合法：新增时可省略，由服务端分配；显式 ID 须以 sobj_ 开头，后接 21 位字母、数字、_ 或 -", i)
			}
			used[ids[i]] = true
		}
	}

	for i, item := range items {
		if ids[i] == "" {
			ids[i] = newObjectID()
			for used[ids[i]] {
				ids[i] = newObjectID()
			}
			used[ids[i]] = true
			item["id"] = mustJSON(ids[i])
		}
		if _, ok := item["order"]; !ok {
			parent := parents[i]
			order, exists := highest[parent]
			if exists && order == math.MaxInt {
				return nil, invalid("objects[%d].order 无法追加：同级顺序已达整数上限，请显式指定未使用的顺序", i)
			}
			if exists {
				order++
			}
			highest[parent] = order
			item["order"] = mustJSON(order)
		}
		if _, ok := item["visible"]; !ok {
			item["visible"] = json.RawMessage("true")
		}
		if _, ok := item["locked"]; !ok {
			item["locked"] = json.RawMessage("false")
		}
	}
	return ids, nil
}

func prepareAgentTransform(item map[string]json.RawMessage, path string, add bool) error {
	var camera map[string]json.RawMessage
	_ = json.Unmarshal(item["camera"], &camera)
	if _, nested := camera["look_at"]; nested {
		return invalid("把 %s.camera.look_at 移到 %s.look_at，与 camera、transform 同级；目标是 [x,y,z] 世界坐标", path, path)
	}

	fields := make(map[string]json.RawMessage)
	raw, hasTransform := item["transform"]
	if hasTransform && (json.Unmarshal(raw, &fields) != nil || fields == nil) {
		return invalid("%s.transform 须为对象", path)
	}
	_, hasLookAt := item["look_at"]
	yaw, hasYaw := item["yaw_deg"]
	if hasTransform && len(fields) == 0 && !add && !hasLookAt && !hasYaw {
		return invalid("%s.transform 需要 position、rotation 或 scale 中至少一项", path)
	}

	_, hasRotation := fields["rotation"]
	switch {
	case hasLookAt && hasRotation:
		return invalid("%s 不能同时写 look_at 和 transform.rotation；瞄准目标时省略 rotation", path)
	case hasYaw && (hasLookAt || hasRotation):
		return invalid("%s 的 yaw_deg、look_at 和 transform.rotation 只能写一项", path)
	case hasYaw:
		var degrees *float64
		if json.Unmarshal(yaw, &degrees) != nil || degrees == nil || !finite(*degrees) {
			return invalid("%s.yaw_deg 须为有限的局部 Y 轴角度（度）", path)
		}
		half := math.Mod(*degrees, 360) * (math.Pi / 360)
		fields["rotation"] = mustJSON([4]float64{0, math.Sin(half), 0, math.Cos(half)})
		delete(item, "yaw_deg")
	}

	if !add && !hasTransform && !hasYaw {
		return nil
	}
	identity := Transform{Position: []float64{0, 0, 0}, Rotation: []float64{0, 0, 0, 1}, Scale: []float64{1, 1, 1}}
	for _, component := range []struct {
		key    string
		values []float64
	}{{"position", identity.Position}, {"rotation", identity.Rotation}, {"scale", identity.Scale}} {
		key, target := component.key, component.values
		if raw, ok := fields[key]; ok {
			// A pointer per component distinguishes null from a genuine zero.
			var values []*float64
			if json.Unmarshal(raw, &values) != nil || len(values) != len(target) {
				return invalid("%s.transform.%s 须为 %d 个数字", path, key, len(target))
			}
			for i, value := range values {
				if value == nil {
					return invalid("%s.transform.%s[%d] 须为数字", path, key, i)
				}
				target[i] = *value
			}
		} else if add && (!hasLookAt || key == "scale") {
			fields[key] = mustJSON(target)
		}
	}
	for key := range fields {
		if key != "position" && key != "rotation" && key != "scale" {
			return invalid("%s.transform 没有字段 %s，可用字段：position、rotation、scale", path, key)
		}
	}
	if err := validateTransform(identity); err != nil {
		return invalid("%s.transform：%v", path, err)
	}
	if add || hasTransform || hasYaw {
		item["transform"] = mustJSON(fields)
	}
	return nil
}

// After shot expansion, only base-state updates still carry a transform.
// Complete them from the locked scene for the full document command.
func completeAgentTransforms(ops []json.RawMessage, scene Scene) ([]json.RawMessage, error) {
	byID := indexObjects(scene.Objects)
	return rewriteAgentObjects(ops, func(add bool, items []map[string]json.RawMessage) error {
		if add {
			return nil
		}
		for _, item := range items {
			raw, ok := item["transform"]
			if !ok {
				continue
			}
			var id string
			_ = json.Unmarshal(item["object_id"], &id)
			if i, found := byID[id]; found {
				var transform Transform
				if err := json.Unmarshal(raw, &transform); err != nil {
					return err
				}
				prior := scene.Objects[i].Transform
				if transform.Position == nil {
					transform.Position = prior.Position
				}
				if transform.Rotation == nil {
					transform.Rotation = prior.Rotation
				}
				if transform.Scale == nil {
					transform.Scale = prior.Scale
				}
				item["transform"] = mustJSON(transform)
			}
		}
		return nil
	})
}
