package creativescene

// Keyframe channels (data: 场景时间线). An object's animation maps a channel
// path to keyframes of one property; channelSpecs is the whitelist of paths,
// which unit each lives in, which objects may have it and how it is sampled
// (patterns: 关键帧求值). Bone channels join the list in child 4.

// sampleKind is how a channel's value goes between two keys.
type sampleKind int

const (
	sampleNumber sampleKind = iota
	sampleVector
	sampleQuaternion
	// sampleDiscrete values only step: booleans, enums, strings and arrays
	// read as a whole such as sensor_mm.
	sampleDiscrete
)

type channelSpec struct {
	path string
	// content puts the channel in object:<id>:content, else object:<id>,
	// next to the base field it keys.
	content bool
	// camera limits the channel to camera objects.
	camera bool
	sample sampleKind
	// field points at the base field the channel's value lands in. For a
	// camera channel o.Camera is set (the camera flag is checked first).
	field func(o *Object) any
}

var channelSpecs = []channelSpec{
	{path: "transform.position", sample: sampleVector, field: func(o *Object) any { return &o.Transform.Position }},
	{path: "transform.rotation", sample: sampleQuaternion, field: func(o *Object) any { return &o.Transform.Rotation }},
	{path: "transform.scale", sample: sampleVector, field: func(o *Object) any { return &o.Transform.Scale }},
	{path: "visible", sample: sampleDiscrete, field: func(o *Object) any { return &o.Visible }},
	{path: "camera.focal_mm", content: true, camera: true, sample: sampleNumber, field: func(o *Object) any { return &o.Camera.FocalMM }},
	{path: "camera.f_stop", content: true, camera: true, sample: sampleNumber, field: func(o *Object) any { return &o.Camera.FStop }},
	{path: "camera.focus", content: true, camera: true, sample: sampleNumber, field: func(o *Object) any { return &o.Camera.Focus }},
	{path: "camera.exposure_compensation", content: true, camera: true, sample: sampleNumber, field: func(o *Object) any { return &o.Camera.ExposureCompensation }},
	{path: "camera.iso", content: true, camera: true, sample: sampleNumber, field: func(o *Object) any { return &o.Camera.ISO }},
	{path: "camera.shutter_s", content: true, camera: true, sample: sampleNumber, field: func(o *Object) any { return &o.Camera.ShutterS }},
	{path: "camera.sensor_mm", content: true, camera: true, sample: sampleDiscrete, field: func(o *Object) any { return &o.Camera.SensorMM }},
	{path: "camera.aspect", content: true, camera: true, sample: sampleDiscrete, field: func(o *Object) any { return &o.Camera.Aspect }},
	{path: "camera.depth_of_field", content: true, camera: true, sample: sampleDiscrete, field: func(o *Object) any { return &o.Camera.DepthOfField }},
	{path: "camera.focus_object_id", content: true, camera: true, sample: sampleDiscrete, field: func(o *Object) any { return &o.Camera.FocusObjectID }},
	{path: "camera.exposure_mode", content: true, camera: true, sample: sampleDiscrete, field: func(o *Object) any { return &o.Camera.ExposureMode }},
}

var channelByPath = func() map[string]*channelSpec {
	m := make(map[string]*channelSpec, len(channelSpecs))
	for i := range channelSpecs {
		m[channelSpecs[i].path] = &channelSpecs[i]
	}
	return m
}()

// channelOf is the spec of path on an object of kind, or nil when the path
// is not animatable there.
func channelOf(path, kind string) *channelSpec {
	c := channelByPath[path]
	if c == nil || (c.camera && kind != KindCamera) {
		return nil
	}
	return c
}
