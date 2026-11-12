package creativeagent

import "github.com/cloudwego/eino/schema"

// The gateway accepts object properties and enums. Operation-specific
// required fields, nullable values and exclusivity are checked at execution.
func sceneEditOperation() *schema.ParameterInfo {
	str := func() *schema.ParameterInfo { return &schema.ParameterInfo{Type: schema.String} }
	number := func() *schema.ParameterInfo { return &schema.ParameterInfo{Type: schema.Number} }
	boolean := func() *schema.ParameterInfo { return &schema.ParameterInfo{Type: schema.Boolean} }
	object := func(props map[string]*schema.ParameterInfo) *schema.ParameterInfo {
		return &schema.ParameterInfo{Type: schema.Object, SubParams: props}
	}
	array := func(item *schema.ParameterInfo) *schema.ParameterInfo {
		return &schema.ParameterInfo{Type: schema.Array, ElemInfo: item}
	}

	transform := object(map[string]*schema.ParameterInfo{
		"position": array(number()), "rotation": array(number()), "scale": array(number()),
	})
	transform.Desc = "局部 position[3]、rotation[4] 单位四元数、scale[3]。更新仅传要改的分量"
	lookAt := array(number())
	lookAt.Desc = "[x,y,z] 世界坐标，与 camera、transform 同级。look_at、yaw_deg、transform.rotation 只选一项"
	yaw := number()
	yaw.Desc = "水平朝向（度）：绕父级 Y 轴转到这个角度，替换原有旋转（含俯仰），不是增量"
	camera := object(map[string]*schema.ParameterInfo{
		"focal_mm": number(), "sensor_mm": array(number()), "aspect": str(), "near": number(), "far": number(),
		"label": str(), "depth_of_field": boolean(), "f_stop": number(), "focus": number(), "focus_object_id": str(),
		"iso": number(), "shutter_s": number(), "exposure_mode": str(), "exposure_compensation": number(),
	})
	primitive := object(map[string]*schema.ParameterInfo{"builtin": str(), "params": object(nil), "faces": object(nil)})
	model := object(map[string]*schema.ParameterInfo{"content_revision_id": str()})
	placement := func() map[string]*schema.ParameterInfo {
		return map[string]*schema.ParameterInfo{
			"name": str(), "visible": boolean(), "locked": boolean(),
			"transform": transform, "look_at": lookAt, "yaw_deg": yaw,
			"camera": camera, "light": object(nil), "volume": object(nil),
			"material_slots": object(nil), "primitive": primitive,
		}
	}
	add, update := placement(), placement()
	add["id"] = &schema.ParameterInfo{Type: schema.String, Desc: "省略则由服务端分配"}
	add["kind"] = &schema.ParameterInfo{Type: schema.String, Required: true, Enum: []string{"primitive", "light", "camera", "group", "model_instance", "volume"}}
	add["parent_id"] = &schema.ParameterInfo{Type: schema.String, Desc: "分组 ID，根级省略"}
	add["order"] = &schema.ParameterInfo{Type: schema.Integer, Desc: "省略时追加到同级末尾"}
	add["model"] = model
	add["only_in_shot"] = boolean()
	update["object_id"] = &schema.ParameterInfo{Type: schema.String, Required: true, Desc: "read_scene 返回的物件 ID"}
	update["animation"] = object(map[string]*schema.ParameterInfo{"clear": array(str())})

	return object(map[string]*schema.ParameterInfo{
		"op":      {Type: schema.String, Required: true, Enum: []string{"add_objects", "update_objects", "remove_objects", "reparent_objects", "set_environment", "set_render", "set_instance_model", "add_shot", "update_shot", "remove_shot", "revert_run"}},
		"objects": array(object(add)), "updates": array(object(update)), "object_ids": array(str()),
		"moves": array(object(nil)), "create_groups": array(object(nil)), "remove_groups": array(str()),
		"environment": object(nil), "render": object(nil),
		"object_id": str(), "scope": str(), "model": model,
		"id": str(), "name": str(), "frame": {Type: schema.Integer}, "camera_object_id": str(), "copy_from": str(),
		"run_id": str(),
	})
}
