package creativescene

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
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
	maxExposureEV    = 10
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

// Fog is three.js Fog: linear from near to far, in metres.
type Fog struct {
	Color string  `json:"color"`
	Near  float64 `json:"near"`
	Far   float64 `json:"far"`
}

// Render is how the scene suggests it be shown and the lit look's exposure
// compensation in stops (toneMappingExposure = 2^exposure_ev); scenes stored
// before exposure existed read 0.
type Render struct {
	Mode       string  `json:"mode"`
	ExposureEV float64 `json:"exposure_ev"`
}

// renderPatch is set_render: only what it carries changes.
type renderPatch struct {
	Mode       *string  `json:"mode,omitempty"`
	ExposureEV *float64 `json:"exposure_ev,omitempty"`
}

// Object is one node of the scene tree. Placement (name, transform, visible,
// locked) and content (primitive, model, material_slots, light, camera) are
// separate units; parent_id and order belong to the objects unit.
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
	Model         *ModelRef               `json:"model,omitempty"`
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

// Light names and means its fields as the three.js light class of its type
// does. Input may leave out all but type; the stored snapshot holds exactly
// the fields of the type (completeLight). A nil field is one not written, so
// distance 0 still encodes.
type Light struct {
	Type       string   `json:"type"`
	Color      *string  `json:"color,omitempty"`
	Intensity  *float64 `json:"intensity,omitempty"`
	CastShadow *bool    `json:"cast_shadow,omitempty"`
	Distance   *float64 `json:"distance,omitempty"`
	Decay      *float64 `json:"decay,omitempty"`
	Angle      *float64 `json:"angle,omitempty"`
	Penumbra   *float64 `json:"penumbra,omitempty"`
}

// lightPatch is a light in update_objects, where type may be left out too.
type lightPatch struct {
	Type       *string  `json:"type,omitempty"`
	Color      *string  `json:"color,omitempty"`
	Intensity  *float64 `json:"intensity,omitempty"`
	CastShadow *bool    `json:"cast_shadow,omitempty"`
	Distance   *float64 `json:"distance,omitempty"`
	Decay      *float64 `json:"decay,omitempty"`
	Angle      *float64 `json:"angle,omitempty"`
	Penumbra   *float64 `json:"penumbra,omitempty"`
}

// Camera fields may all be left out on input; the stored snapshot holds every
// one (completeCamera). near, far and focus are those of three.js
// PerspectiveCamera. FocusObjectID is a soft reference: checked when written
// (checkFocusTargets), left dangling when its target is later removed, and
// then the renderer focuses at focus instead (E7).
type Camera struct {
	FocalMM       *float64  `json:"focal_mm,omitempty"`
	SensorMM      []float64 `json:"sensor_mm,omitempty"`
	Aspect        *string   `json:"aspect,omitempty"`
	Near          *float64  `json:"near,omitempty"`
	Far           *float64  `json:"far,omitempty"`
	Label         *string   `json:"label,omitempty"`
	DepthOfField  *bool     `json:"depth_of_field,omitempty"`
	FStop         *float64  `json:"f_stop,omitempty"`
	Focus         *float64  `json:"focus,omitempty"`
	FocusObjectID *string   `json:"focus_object_id,omitempty"`
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
// behave like freshly applied values. Cameras stored before the depth of
// field fields existed are completed here instead of by a migration.
func decodeScene(p creativecontent.Payload) (Scene, error) {
	var s Scene
	if err := creativeops.Decode(p.Scene, &s); err != nil {
		return Scene{}, err
	}
	for i := range s.Objects {
		canonicalNumbers(&s.Objects[i])
		if c := s.Objects[i].Camera; c != nil {
			completeCamera(c)
		}
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
	// objects is always an array in the contract; a nil slice would store null.
	if s.Objects == nil {
		s.Objects = []Object{}
	}
	raw, err := json.Marshal(s)
	if err != nil {
		return creativedocument.Snapshot{}, err
	}
	return creativedocument.Snapshot{Payload: creativecontent.Payload{Scene: raw}, Refs: modelRefs(s.Objects)}, nil
}

// decodeInput decodes client input strictly and then insists on every field
// whose json tag lacks omitempty, so a missing visible never reads as false.
// null passes only where the contract makes a field nullable, a pointer
// without omitempty (parent_id, fog): anywhere else it
// decodes as the zero value, and position [null, 0, 0] saved as the origin.
func decodeInput(raw json.RawMessage, target any) error {
	if err := creativeops.Decode(raw, target); err != nil {
		var public creativeops.Public
		if errors.As(err, &public) {
			return err
		}
		if err := shapeError(raw, reflect.TypeOf(target).Elem(), ""); err != nil {
			return err
		}
		return invalid("操作格式不合法")
	}
	return requireFields(raw, reflect.TypeOf(target).Elem(), "", false)
}

var jsonUnmarshaler = reflect.TypeFor[json.Unmarshaler]()

// shapeError says where input that failed to decode stops matching t: a
// field t does not have, with the ones it does, or a value of the wrong
// type. The decoder names an unknown field without its path, and the
// assistant writes these by hand.
func shapeError(raw json.RawMessage, t reflect.Type, path string) error {
	if string(raw) == "null" {
		return nil
	}
	if t.Kind() == reflect.Pointer {
		return shapeError(raw, t.Elem(), path)
	}
	wrong := func() error {
		if path == "" {
			return invalid("操作须是 JSON 对象")
		}
		return invalid("字段 %s 的类型不对", strings.TrimPrefix(path, "."))
	}
	switch {
	case t.Kind() == reflect.Interface, t.Kind() == reflect.Slice && t.Elem().Kind() == reflect.Uint8:
		return nil
	case reflect.PointerTo(t).Implements(jsonUnmarshaler):
	case t.Kind() == reflect.Slice:
		var items []json.RawMessage
		if json.Unmarshal(raw, &items) != nil {
			return wrong()
		}
		for i, item := range items {
			if err := shapeError(item, t.Elem(), fmt.Sprintf("%s[%d]", path, i)); err != nil {
				return err
			}
		}
		return nil
	case t.Kind() == reflect.Map:
		var items map[string]json.RawMessage
		if json.Unmarshal(raw, &items) != nil {
			return wrong()
		}
		for _, k := range slices.Sorted(maps.Keys(items)) {
			if err := shapeError(items[k], t.Elem(), path+"."+k); err != nil {
				return err
			}
		}
		return nil
	case t.Kind() == reflect.Struct:
		var fields map[string]json.RawMessage
		if json.Unmarshal(raw, &fields) != nil {
			return wrong()
		}
		var names []string
		types := map[string]reflect.Type{}
		for i := range t.NumField() {
			f := t.Field(i)
			name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
			if f.IsExported() && name != "-" && name != "" {
				names = append(names, name)
				types[name] = f.Type
			}
		}
		for _, k := range slices.Sorted(maps.Keys(fields)) {
			ft, ok := types[k]
			if !ok {
				where := "操作"
				if path != "" {
					where = strings.TrimPrefix(path, ".") + " "
				}
				return invalid("%s没有字段 %s，可用字段：%s", where, k, strings.Join(names, "、"))
			}
			if err := shapeError(fields[k], ft, path+"."+k); err != nil {
				return err
			}
		}
		return nil
	}
	if json.Unmarshal(raw, reflect.New(t).Interface()) != nil {
		return wrong()
	}
	return nil
}

func requireFields(raw json.RawMessage, t reflect.Type, path string, nullable bool) error {
	if string(raw) == "null" {
		if nullable {
			return nil
		}
		return invalid("字段 %s 不能为 null", strings.TrimPrefix(path, "."))
	}
	switch t.Kind() {
	case reflect.Pointer:
		return requireFields(raw, t.Elem(), path, false)
	case reflect.Slice:
		if t.Elem().Kind() == reflect.Uint8 {
			return nil
		}
		var items []json.RawMessage
		if err := json.Unmarshal(raw, &items); err != nil {
			return err
		}
		for i, item := range items {
			if err := requireFields(item, t.Elem(), fmt.Sprintf("%s[%d]", path, i), freeForm(t.Elem())); err != nil {
				return err
			}
		}
	case reflect.Map:
		var items map[string]json.RawMessage
		if err := json.Unmarshal(raw, &items); err != nil {
			return err
		}
		for k, item := range items {
			if err := requireFields(item, t.Elem(), path+"."+k, freeForm(t.Elem())); err != nil {
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
			nullable := f.Type.Kind() == reflect.Pointer && opts != "omitempty"
			if err := requireFields(v, f.Type, path+"."+name, nullable); err != nil {
				return err
			}
		}
	}
	return nil
}

// freeForm values, such as primitive params, may be anything JSON allows;
// the builtin catalog checks them.
func freeForm(t reflect.Type) bool { return t.Kind() == reflect.Interface }

// objectIDProblem says what is wrong with an id: hand-written ones are
// usually a character off.
func objectIDProblem(id string) error {
	rest, ok := strings.CutPrefix(id, "sobj_")
	if !ok {
		return invalid("物件 ID %q 不合法：须以 sobj_ 开头，后接 21 位字母、数字、_ 或 -", id)
	}
	return invalid("物件 ID %q 不合法：sobj_ 后须是 21 位字母、数字、_ 或 -，现在是 %d 位", id, utf8.RuneCountInString(rest))
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
	if !in(s.Render.ExposureEV, -maxExposureEV, maxExposureEV) {
		return invalid("曝光补偿须在 −%d…%d 档之间", maxExposureEV, maxExposureEV)
	}
	byID := make(map[string]*Object, len(s.Objects))
	cameras := 0
	for i := range s.Objects {
		o := &s.Objects[i]
		if !objectIDPattern.MatchString(o.ID) {
			return objectIDProblem(o.ID)
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
	if n := len(modelRefs(s.Objects)); n > maxSceneModels {
		return creativeops.Explain(creativedocument.ErrSizeLimit, "一个场景引用的模型不能超过 %d 个，现有 %d 个", maxSceneModels, n)
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
	if f := e.Fog; f != nil && (!colorPattern.MatchString(f.Color) || !finite(f.Near) || !finite(f.Far) || f.Near <= 0 || f.Far <= f.Near) {
		return invalid("雾须有 #rrggbb 颜色，且 0 < near < far")
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
	content := map[string]bool{"primitive": o.Primitive != nil, "model": o.Model != nil, "light": o.Light != nil, "camera": o.Camera != nil, "material_slots": len(o.MaterialSlots) > 0}
	var allowed []string
	switch o.Kind {
	case KindPrimitive:
		allowed = []string{"primitive", "material_slots"}
	case KindModelInstance:
		allowed = []string{"model", "material_slots"}
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
	case KindModelInstance:
		if err := validateModelRef(o.Model); err != nil {
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

// lightFields lists, in contract order, the fields each light type has
// besides type (L1, L2).
var lightFields = map[string][]string{
	"directional": {"color", "intensity", "cast_shadow"},
	"point":       {"color", "intensity", "cast_shadow", "distance", "decay"},
	"spot":        {"color", "intensity", "cast_shadow", "distance", "decay", "angle", "penumbra"},
}

var lightNames = map[string]string{"directional": "平行光", "point": "点光", "spot": "聚光"}

type lightField struct {
	name  string
	set   bool
	clear func()
}

// fields is every optional field of l in contract order.
func (l *Light) fields() []lightField {
	return []lightField{
		{"color", l.Color != nil, func() { l.Color = nil }},
		{"intensity", l.Intensity != nil, func() { l.Intensity = nil }},
		{"cast_shadow", l.CastShadow != nil, func() { l.CastShadow = nil }},
		{"distance", l.Distance != nil, func() { l.Distance = nil }},
		{"decay", l.Decay != nil, func() { l.Decay = nil }},
		{"angle", l.Angle != nil, func() { l.Angle = nil }},
		{"penumbra", l.Penumbra != nil, func() { l.Penumbra = nil }},
	}
}

// checkLightFields is L2: a light carries only fields its type has. The
// message names the first one that does not belong and the ones that do.
func checkLightFields(l *Light) error {
	has, ok := lightFields[l.Type]
	if !ok {
		return fmt.Errorf("未知灯光类型 %q，可用：directional、point、spot", l.Type)
	}
	for _, f := range l.fields() {
		if f.set && !slices.Contains(has, f.name) {
			return fmt.Errorf("%s没有字段 %s，可用字段：type、%s", lightNames[l.Type], f.name, strings.Join(has, "、"))
		}
	}
	return nil
}

// completeLight checks L2 and fills the three.js default of every field the
// type has and l leaves out (L1).
func completeLight(l *Light) error {
	if err := checkLightFields(l); err != nil {
		return err
	}
	has := lightFields[l.Type]
	fill(&l.Color, "#ffffff")
	fill(&l.Intensity, 1.0)
	fill(&l.CastShadow, false)
	if slices.Contains(has, "distance") {
		fill(&l.Distance, 0.0)
		fill(&l.Decay, 2.0)
	}
	if slices.Contains(has, "angle") {
		fill(&l.Angle, math.Pi/3)
		fill(&l.Penumbra, 0.0)
	}
	return nil
}

// mergeLight is L4: p changes only what it carries. The update itself must
// fit the resulting type; the current fields the type lacks are dropped and
// the ones it gains take their defaults.
func mergeLight(cur Light, p lightPatch) (Light, error) {
	next := Light{Type: cur.Type, Color: p.Color, Intensity: p.Intensity, CastShadow: p.CastShadow, Distance: p.Distance, Decay: p.Decay, Angle: p.Angle, Penumbra: p.Penumbra}
	if p.Type != nil {
		next.Type = *p.Type
	}
	if err := checkLightFields(&next); err != nil {
		return Light{}, err
	}
	keep(&next.Color, cur.Color)
	keep(&next.Intensity, cur.Intensity)
	keep(&next.CastShadow, cur.CastShadow)
	keep(&next.Distance, cur.Distance)
	keep(&next.Decay, cur.Decay)
	keep(&next.Angle, cur.Angle)
	keep(&next.Penumbra, cur.Penumbra)
	has := lightFields[next.Type]
	for _, f := range next.fields() {
		if !slices.Contains(has, f.name) {
			f.clear()
		}
	}
	return next, completeLight(&next)
}

// completeCamera fills every field c leaves out (L3).
func completeCamera(c *Camera) {
	fill(&c.FocalMM, 35.0)
	if c.SensorMM == nil {
		c.SensorMM = []float64{36, 24}
	}
	fill(&c.Aspect, "3:2")
	fill(&c.Near, 0.1)
	fill(&c.Far, 1000.0)
	fill(&c.Label, "")
	fill(&c.DepthOfField, false)
	fill(&c.FStop, 2.8)
	fill(&c.Focus, 10.0)
	fill(&c.FocusObjectID, "")
}

// cameraComplete reports whether c already holds every field, so a stored
// snapshot needs no completion before it is shown.
func cameraComplete(c Camera) bool {
	return c.FocalMM != nil && c.SensorMM != nil && c.Aspect != nil && c.Near != nil && c.Far != nil && c.Label != nil &&
		c.DepthOfField != nil && c.FStop != nil && c.Focus != nil && c.FocusObjectID != nil
}

// mergeCamera is L4 for cameras: p changes only what it carries. Merging
// cannot tell null from absent, so focus_object_id is cleared with "".
func mergeCamera(cur, p Camera) Camera {
	keep(&p.FocalMM, cur.FocalMM)
	if p.SensorMM == nil {
		p.SensorMM = cur.SensorMM
	}
	keep(&p.Aspect, cur.Aspect)
	keep(&p.Near, cur.Near)
	keep(&p.Far, cur.Far)
	keep(&p.Label, cur.Label)
	keep(&p.DepthOfField, cur.DepthOfField)
	keep(&p.FStop, cur.FStop)
	keep(&p.Focus, cur.Focus)
	keep(&p.FocusObjectID, cur.FocusObjectID)
	completeCamera(&p)
	return p
}

// fill sets *p to v when it was not written.
func fill[T any](p **T, v T) {
	if *p == nil {
		*p = &v
	}
}

// keep sets *p to cur when it was not written.
func keep[T any](p **T, cur *T) {
	if *p == nil {
		*p = cur
	}
}

// validateLight checks a completed light: L1 and the ranges of the contract.
func validateLight(l Light) error {
	if err := checkLightFields(&l); err != nil {
		return err
	}
	has := lightFields[l.Type]
	for _, f := range l.fields() {
		if !f.set && slices.Contains(has, f.name) {
			return fmt.Errorf("%s缺少字段 %s", lightNames[l.Type], f.name)
		}
	}
	if !colorPattern.MatchString(*l.Color) || !in(*l.Intensity, 0, 1000) {
		return errors.New("灯光须有 #rrggbb 颜色和 0…1000 的强度")
	}
	if l.Distance != nil && (!finite(*l.Distance) || *l.Distance < 0) {
		return errors.New("distance 须 ≥ 0 米（0 表示不截断）")
	}
	if l.Decay != nil && !in(*l.Decay, 0, 10) {
		return errors.New("decay 须在 0…10 之间")
	}
	if l.Angle != nil && (!finite(*l.Angle) || *l.Angle <= 0 || *l.Angle > math.Pi/2) {
		return errors.New("angle 是光锥半角，须为大于 0、不超过 π/2 的弧度")
	}
	if l.Penumbra != nil && !in(*l.Penumbra, 0, 1) {
		return errors.New("penumbra 须在 0…1 之间")
	}
	return nil
}

// validateCamera checks a completed camera (L3).
func validateCamera(c Camera) error {
	if !cameraComplete(c) {
		return errors.New("摄像机缺少字段")
	}
	if !in(*c.FocalMM, 8, 800) {
		return errors.New("焦距须在 8…800 mm 之间")
	}
	if len(c.SensorMM) != 2 || !in(c.SensorMM[0], 1, 100) || !in(c.SensorMM[1], 1, 100) {
		return errors.New("传感器尺寸须为 [宽, 高]，各 1…100 mm")
	}
	if !aspectPattern.MatchString(*c.Aspect) {
		return errors.New("画幅比须为「宽:高」，各 1…32 的整数")
	}
	if !finite(*c.Near) || !finite(*c.Far) || *c.Near <= 0 || *c.Far <= *c.Near {
		return errors.New("须满足 0 < near < far")
	}
	if utf8.RuneCountInString(*c.Label) > maxLabelRunes {
		return fmt.Errorf("标签不能超过 %d 个字", maxLabelRunes)
	}
	if !in(*c.FStop, 0.5, 64) {
		return errors.New("光圈 f_stop 须在 0.5…64 之间")
	}
	if !finite(*c.Focus) || *c.Focus <= 0 {
		return errors.New("对焦距离 focus 须大于 0 米")
	}
	if id := *c.FocusObjectID; id != "" && !objectIDPattern.MatchString(id) {
		return objectIDProblem(id)
	}
	return nil
}

// focusTargetKinds are the objects a camera may focus on.
var focusTargetKinds = []string{KindPrimitive, KindModelInstance, KindGroup}

// checkFocusTargets validates the focus_object_id that an operation wrote,
// against the scene after the operation. Only written references are
// checked: one left dangling by a later removal stays valid (E7).
func checkFocusTargets(s *Scene, written map[string]bool) error {
	if len(written) == 0 {
		return nil
	}
	byID := make(map[string]*Object, len(s.Objects))
	for i := range s.Objects {
		byID[s.Objects[i].ID] = &s.Objects[i]
	}
	for _, id := range slices.Sorted(maps.Keys(written)) {
		o, ok := byID[id]
		if !ok || o.Camera == nil || o.Camera.FocusObjectID == nil || *o.Camera.FocusObjectID == "" {
			continue
		}
		target := *o.Camera.FocusObjectID
		if !objectIDPattern.MatchString(target) {
			return objectIDProblem(target)
		}
		if target == id {
			return invalid("摄像机 %s 不能对焦到自己", id)
		}
		t, ok := byID[target]
		if !ok {
			return invalid("摄像机 %s 的对焦物件 %s 不存在", id, target)
		}
		if !slices.Contains(focusTargetKinds, t.Kind) {
			return invalid("摄像机 %s 的对焦物件 %s 是 %s，只能对焦到基础体、模型实例或分组（空物体）", id, target, t.Kind)
		}
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
