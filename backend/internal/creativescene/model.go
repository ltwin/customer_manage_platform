package creativescene

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/samson/customer-manage-platform/backend/internal/creativecontent"
	"github.com/samson/customer-manage-platform/backend/internal/creativedocument"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
)

const (
	KindPrimitive = "primitive"
	KindLight     = "light"
	KindCamera    = "camera"
	KindGroup     = "group"

	maxObjects       = 2000
	maxCameras       = 32
	maxNameRunes     = 80
	maxLabelRunes    = 40
	maxPositionM     = 100000
	minScale         = 0.001
	maxScale         = 1000
	quaternionSlack  = 1e-3
	maxSlots         = 32
	maxSlotParams    = 32
	maxSlotTextRunes = 80
)

var (
	objectIDPattern = regexp.MustCompile(`^sobj_[A-Za-z0-9_-]{21}$`)
	slotKeyPattern  = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
	aspectPattern   = regexp.MustCompile(`^([1-9]|[12][0-9]|3[0-2]):([1-9]|[12][0-9]|3[0-2])$`)
)

// Scene is the payload of one scene revision, stored under Payload.Scene.
type Scene struct {
	Environment Environment `json:"environment"`
	Render      Render      `json:"render"`
	Objects     []Object    `json:"objects"`
}

type Environment struct {
	Sky     Sky     `json:"sky"`
	Ambient Ambient `json:"ambient"`
	Fog     *Fog    `json:"fog"`
}

// Sky carries only the fields of its kind.
type Sky struct {
	Kind            string   `json:"kind"`
	SunElevationDeg *float64 `json:"sun_elevation_deg,omitempty"`
	SunAzimuthDeg   *float64 `json:"sun_azimuth_deg,omitempty"`
	Turbidity       *float64 `json:"turbidity,omitempty"`
	HDRI            *string  `json:"hdri,omitempty"`
	Color           *string  `json:"color,omitempty"`
}

type Ambient struct {
	Color     string  `json:"color"`
	Intensity float64 `json:"intensity"`
}

type Fog struct {
	Color string  `json:"color"`
	NearM float64 `json:"near_m"`
	FarM  float64 `json:"far_m"`
}

type Render struct {
	Mode string `json:"mode"`
}

// Object is one node of the scene tree. Placement (name, transform, visible,
// locked) and content (primitive, material_slots, light, camera) are separate
// units; parent_id and order belong to the objects unit.
type Object struct {
	ID            string                  `json:"id"`
	Kind          string                  `json:"kind"`
	Name          string                  `json:"name"`
	ParentID      *string                 `json:"parent_id"`
	Order         int                     `json:"order"`
	Transform     Transform               `json:"transform"`
	Visible       bool                    `json:"visible"`
	Locked        bool                    `json:"locked"`
	Primitive     *Primitive              `json:"primitive,omitempty"`
	MaterialSlots map[string]MaterialSlot `json:"material_slots,omitempty"`
	Light         *Light                  `json:"light,omitempty"`
	Camera        *Camera                 `json:"camera,omitempty"`
}

type Transform struct {
	Position []float64 `json:"position"`
	Rotation []float64 `json:"rotation"`
	Scale    []float64 `json:"scale"`
}

// Primitive params may be partial on input; the stored snapshot always holds
// every catalog key.
type Primitive struct {
	Builtin string         `json:"builtin"`
	Params  map[string]any `json:"params,omitempty"`
}

type MaterialSlot struct {
	Preset string         `json:"preset"`
	Params map[string]any `json:"params"`
}

type Light struct {
	Type       string   `json:"type"`
	Color      string   `json:"color"`
	Intensity  float64  `json:"intensity"`
	RangeM     *float64 `json:"range_m"`
	AngleDeg   *float64 `json:"angle_deg"`
	Penumbra   *float64 `json:"penumbra"`
	CastShadow bool     `json:"cast_shadow"`
}

type Camera struct {
	FocalMM  float64   `json:"focal_mm"`
	SensorMM []float64 `json:"sensor_mm"`
	Aspect   string    `json:"aspect"`
	NearM    float64   `json:"near_m"`
	FarM     float64   `json:"far_m"`
	Label    string    `json:"label"`
}

func placementUnit(id string) string { return "object:" + id }
func contentUnit(id string) string   { return "object:" + id + ":content" }

// objectOfUnit splits an object unit name; ok is false for any other unit.
func objectOfUnit(unit string) (id string, content bool, ok bool) {
	rest, found := strings.CutPrefix(unit, "object:")
	if !found {
		return "", false, false
	}
	if id, content = strings.CutSuffix(rest, ":content"); content {
		return id, true, objectIDPattern.MatchString(id)
	}
	return rest, false, objectIDPattern.MatchString(rest)
}

func invalid(format string, args ...any) error {
	return creativeops.Explain(creativeops.ErrValidation, format, args...)
}

// decodeScene reads a stored snapshot. Numbers inside free-form params come
// back as json.Number and are made float64 so comparisons and re-encoding
// behave like freshly applied values.
func decodeScene(p creativecontent.Payload) (Scene, error) {
	var s Scene
	if err := creativeops.Decode(p.Scene, &s); err != nil {
		return Scene{}, err
	}
	for i := range s.Objects {
		canonicalNumbers(&s.Objects[i])
	}
	return s, nil
}

func canonicalNumbers(o *Object) {
	if o.Primitive != nil {
		for k, v := range o.Primitive.Params {
			if f, ok := number(v); ok {
				o.Primitive.Params[k] = f
			}
		}
	}
	for _, slot := range o.MaterialSlots {
		for k, v := range slot.Params {
			if f, ok := number(v); ok {
				slot.Params[k] = f
			}
		}
	}
}

func encodeScene(s Scene) (creativedocument.Snapshot, error) {
	raw, err := json.Marshal(s)
	if err != nil {
		return creativedocument.Snapshot{}, err
	}
	return creativedocument.Snapshot{Payload: creativecontent.Payload{Scene: raw}}, nil
}

// decodeInput decodes client input strictly and then insists on every field
// whose json tag lacks omitempty, so a missing visible never reads as false.
func decodeInput(raw json.RawMessage, target any) error {
	if err := creativeops.Decode(raw, target); err != nil {
		return err
	}
	return requireFields(raw, reflect.TypeOf(target).Elem(), "")
}

func requireFields(raw json.RawMessage, t reflect.Type, path string) error {
	if string(raw) == "null" {
		return nil
	}
	switch t.Kind() {
	case reflect.Pointer:
		return requireFields(raw, t.Elem(), path)
	case reflect.Slice:
		if t.Elem().Kind() != reflect.Struct {
			return nil
		}
		var items []json.RawMessage
		if err := json.Unmarshal(raw, &items); err != nil {
			return err
		}
		for i, item := range items {
			if err := requireFields(item, t.Elem(), fmt.Sprintf("%s[%d]", path, i)); err != nil {
				return err
			}
		}
	case reflect.Map:
		if t.Elem().Kind() != reflect.Struct {
			return nil
		}
		var items map[string]json.RawMessage
		if err := json.Unmarshal(raw, &items); err != nil {
			return err
		}
		for k, item := range items {
			if err := requireFields(item, t.Elem(), path+"."+k); err != nil {
				return err
			}
		}
	case reflect.Struct:
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(raw, &fields); err != nil {
			return err
		}
		for i := range t.NumField() {
			f := t.Field(i)
			name, opts, _ := strings.Cut(f.Tag.Get("json"), ",")
			if !f.IsExported() || name == "-" || name == "" {
				continue
			}
			v, ok := fields[name]
			if !ok {
				if opts == "omitempty" {
					continue
				}
				return invalid("缺少字段 %s", strings.TrimPrefix(path+"."+name, "."))
			}
			if err := requireFields(v, f.Type, path+"."+name); err != nil {
				return err
			}
		}
	}
	return nil
}

// newObjectID is a 21-character nanoid; clients make their own the same way.
func newObjectID() string {
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789_-"
	b := make([]byte, 21)
	_, _ = rand.Read(b)
	for i := range b {
		b[i] = alphabet[b[i]&63]
	}
	return "sobj_" + string(b)
}

// Validate checks the invariants S1–S8 on a whole snapshot. The 512 KiB
// payload limit is enforced by the document layer.
func Validate(s Scene) error {
	if len(s.Objects) > maxObjects {
		return creativeops.Explain(creativedocument.ErrSizeLimit, "物件不能超过 %d 个", maxObjects)
	}
	if err := validateEnvironment(s.Environment); err != nil {
		return err
	}
	if s.Render.Mode != "clay" && s.Render.Mode != "lit" {
		return invalid("渲染模式须为 clay 或 lit")
	}
	byID := make(map[string]*Object, len(s.Objects))
	cameras := 0
	for i := range s.Objects {
		o := &s.Objects[i]
		if !objectIDPattern.MatchString(o.ID) {
			return invalid("物件 ID %q 不合法", o.ID)
		}
		if _, dup := byID[o.ID]; dup {
			return invalid("物件 ID %s 重复", o.ID)
		}
		byID[o.ID] = o
		if err := validateObject(o); err != nil {
			return invalid("物件 %s：%v", o.ID, err)
		}
		if o.Kind == KindCamera {
			cameras++
		}
	}
	if cameras > maxCameras {
		return creativeops.Explain(creativedocument.ErrSizeLimit, "摄像机不能超过 %d 台", maxCameras)
	}
	return validateTree(s.Objects, byID)
}

func validateEnvironment(e Environment) error {
	sky := e.Sky
	switch sky.Kind {
	case "procedural":
		if sky.HDRI != nil || sky.Color != nil || !between(sky.SunElevationDeg, -10, 90) || !between(sky.SunAzimuthDeg, 0, 360) || !between(sky.Turbidity, 1, 20) {
			return invalid("程序化天空须有太阳高度（-10…90）、方位（0…360）和浑浊度（1…20），且不带其他字段")
		}
	case "hdri":
		if sky.HDRI == nil || !versionedID.MatchString(*sky.HDRI) || sky.Color != nil || sky.SunElevationDeg != nil || sky.SunAzimuthDeg != nil || sky.Turbidity != nil {
			return invalid("HDRI 天空须有合法的 hdri，且不带其他字段")
		}
	case "color":
		if sky.Color == nil || !colorPattern.MatchString(*sky.Color) || sky.HDRI != nil || sky.SunElevationDeg != nil || sky.SunAzimuthDeg != nil || sky.Turbidity != nil {
			return invalid("纯色天空须有 #rrggbb 颜色，且不带其他字段")
		}
	default:
		return invalid("未知天空类型 %q", sky.Kind)
	}
	if !colorPattern.MatchString(e.Ambient.Color) || !in(e.Ambient.Intensity, 0, 10) {
		return invalid("环境光须有 #rrggbb 颜色和 0…10 的强度")
	}
	if f := e.Fog; f != nil && (!colorPattern.MatchString(f.Color) || !finite(f.NearM) || !finite(f.FarM) || f.NearM <= 0 || f.FarM <= f.NearM) {
		return invalid("雾须有 #rrggbb 颜色，且 0 < near_m < far_m")
	}
	return nil
}

func validateObject(o *Object) error {
	if utf8.RuneCountInString(o.Name) > maxNameRunes {
		return fmt.Errorf("名称不能超过 %d 个字", maxNameRunes)
	}
	if o.Order < 0 {
		return errors.New("order 不能为负")
	}
	if err := validateTransform(o.Transform); err != nil {
		return err
	}
	content := map[string]bool{"primitive": o.Primitive != nil, "light": o.Light != nil, "camera": o.Camera != nil, "material_slots": len(o.MaterialSlots) > 0}
	var allowed []string
	switch o.Kind {
	case KindPrimitive:
		allowed = []string{"primitive", "material_slots"}
	case KindLight:
		allowed = []string{"light"}
	case KindCamera:
		allowed = []string{"camera"}
	case KindGroup:
	default:
		return fmt.Errorf("不支持的类型 %q", o.Kind)
	}
	for field, present := range content {
		if present && !slices.Contains(allowed, field) {
			return fmt.Errorf("%s 物件不能有 %s", o.Kind, field)
		}
	}
	switch o.Kind {
	case KindPrimitive:
		if o.Primitive == nil {
			return errors.New("缺少 primitive")
		}
		b, err := lookupBuiltin(o.Primitive.Builtin)
		if err != nil {
			return err
		}
		if err := b.checkParams(o.Primitive.Params); err != nil {
			return err
		}
		return validateSlots(o.MaterialSlots)
	case KindLight:
		if o.Light == nil {
			return errors.New("缺少 light")
		}
		return validateLight(*o.Light)
	case KindCamera:
		if o.Camera == nil {
			return errors.New("缺少 camera")
		}
		return validateCamera(*o.Camera)
	}
	return nil
}

func validateTransform(t Transform) error {
	if len(t.Position) != 3 || len(t.Rotation) != 4 || len(t.Scale) != 3 {
		return errors.New("transform 须为 position[3]、rotation[4]、scale[3]")
	}
	for _, v := range t.Position {
		if !in(v, -maxPositionM, maxPositionM) {
			return fmt.Errorf("位置每轴须在 ±%d m 以内", maxPositionM)
		}
	}
	norm := 0.0
	for _, v := range t.Rotation {
		if !finite(v) {
			return errors.New("旋转须为有限数")
		}
		norm += v * v
	}
	if math.Abs(math.Sqrt(norm)-1) > quaternionSlack {
		return errors.New("旋转须为单位四元数")
	}
	for _, v := range t.Scale {
		if !in(v, minScale, maxScale) {
			return fmt.Errorf("缩放每项须在 %g…%g 之间", minScale, float64(maxScale))
		}
	}
	return nil
}

func validateSlots(slots map[string]MaterialSlot) error {
	if len(slots) > maxSlots {
		return fmt.Errorf("材质槽不能超过 %d 个", maxSlots)
	}
	for id, slot := range slots {
		if !slotKeyPattern.MatchString(id) || !versionedID.MatchString(slot.Preset) {
			return fmt.Errorf("材质槽 %q 的名称或预设不合法", id)
		}
		if len(slot.Params) > maxSlotParams {
			return fmt.Errorf("材质槽 %s 的参数不能超过 %d 个", id, maxSlotParams)
		}
		for k, v := range slot.Params {
			if !slotKeyPattern.MatchString(k) {
				return fmt.Errorf("材质槽 %s 的参数名 %q 不合法", id, k)
			}
			switch x := v.(type) {
			case bool:
			case string:
				if utf8.RuneCountInString(x) > maxSlotTextRunes {
					return fmt.Errorf("材质槽 %s 的参数 %s 过长", id, k)
				}
			default:
				if _, ok := number(v); !ok {
					return fmt.Errorf("材质槽 %s 的参数 %s 须为数值、文字或布尔值", id, k)
				}
			}
		}
	}
	return nil
}

func validateLight(l Light) error {
	switch l.Type {
	case "directional", "point", "spot":
	default:
		return fmt.Errorf("未知灯光类型 %q", l.Type)
	}
	if !colorPattern.MatchString(l.Color) || !in(l.Intensity, 0, 1000) {
		return errors.New("灯光须有 #rrggbb 颜色和 0…1000 的强度")
	}
	if l.RangeM != nil && (!finite(*l.RangeM) || *l.RangeM <= 0) {
		return errors.New("range_m 须大于 0")
	}
	if l.AngleDeg != nil && !in(*l.AngleDeg, 1, 179) || l.Type == "spot" && l.AngleDeg == nil {
		return errors.New("聚光灯须有 1…179 的 angle_deg")
	}
	if l.Penumbra != nil && !in(*l.Penumbra, 0, 1) {
		return errors.New("penumbra 须在 0…1 之间")
	}
	return nil
}

func validateCamera(c Camera) error {
	if !in(c.FocalMM, 8, 800) {
		return errors.New("焦距须在 8…800 mm 之间")
	}
	if len(c.SensorMM) != 2 || !in(c.SensorMM[0], 1, 100) || !in(c.SensorMM[1], 1, 100) {
		return errors.New("传感器尺寸须为 [宽, 高]，各 1…100 mm")
	}
	if !aspectPattern.MatchString(c.Aspect) {
		return errors.New("画幅比须为「宽:高」，各 1…32 的整数")
	}
	if !finite(c.NearM) || !finite(c.FarM) || c.NearM <= 0 || c.FarM <= c.NearM {
		return errors.New("须满足 0 < near_m < far_m")
	}
	if utf8.RuneCountInString(c.Label) > maxLabelRunes {
		return fmt.Errorf("标签不能超过 %d 个字", maxLabelRunes)
	}
	return nil
}

// validateTree is S2 and S3: parents are groups, no cycles, and siblings
// never share an order.
func validateTree(objects []Object, byID map[string]*Object) error {
	type sibling struct {
		parent string
		order  int
	}
	taken := make(map[sibling]string, len(objects))
	for i := range objects {
		o := &objects[i]
		parent := ""
		if o.ParentID != nil {
			p, ok := byID[*o.ParentID]
			if !ok {
				return invalid("物件 %s 的父物件 %s 不存在", o.ID, *o.ParentID)
			}
			if p.Kind != KindGroup {
				return invalid("物件 %s 的父物件 %s 不是分组", o.ID, p.ID)
			}
			parent = p.ID
		}
		key := sibling{parent, o.Order}
		if other, dup := taken[key]; dup {
			return invalid("物件 %s 与 %s 的顺序重复", other, o.ID)
		}
		taken[key] = o.ID
	}
	const (
		visiting = 1
		done     = 2
	)
	state := make(map[string]int, len(objects))
	for i := range objects {
		var path []string
		for id := objects[i].ID; ; {
			if state[id] == done {
				break
			}
			if state[id] == visiting {
				return invalid("物件 %s 的层级成环", id)
			}
			state[id] = visiting
			path = append(path, id)
			parent := byID[id].ParentID
			if parent == nil {
				break
			}
			id = *parent
		}
		for _, id := range path {
			state[id] = done
		}
	}
	return nil
}

func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }

func in(v, lo, hi float64) bool { return finite(v) && v >= lo && v <= hi }

func between(v *float64, lo, hi float64) bool { return v != nil && in(*v, lo, hi) }
