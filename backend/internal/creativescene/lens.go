package creativescene

import (
	"math"

	"github.com/samson/customer-manage-platform/backend/internal/creativecanvas"
)

// lensOf is the camera's lens as an export records it (design「助手用导出图
// 重绘」). It focuses where the renderer does (framing.ts focusDistance,
// cameraOptics.clampFocus): the focus object's origin measured along the
// camera's view (local −Z) while the object exists, focus otherwise, kept
// between near and far and beyond the lens.
func lensOf(scene Scene, cam Object) creativecanvas.SceneExportLens {
	c := cam.Camera
	l := creativecanvas.SceneExportLens{FocalMM: *c.FocalMM, DepthOfField: *c.DepthOfField, FStop: *c.FStop, FocusM: *c.Focus}
	if id := *c.FocusObjectID; id != "" {
		s := &lookAtScene{objects: make(map[string]Object, len(scene.Objects)), world: map[string]mat4{}}
		for _, o := range scene.Objects {
			s.objects[o.ID] = o
		}
		target, found := s.objects[id]
		view, okView := s.worldMatrix(cam.ID)
		at, okAt := s.worldMatrix(id)
		if found && okView && okAt {
			forward := vec3{-view[8], -view[9], -view[10]}.normalize()
			d := vec3{at[12], at[13], at[14]}.sub(vec3{view[12], view[13], view[14]})
			l.FocusM = d[0]*forward[0] + d[1]*forward[1] + d[2]*forward[2]
			l.FocusObjectName = target.Name
		}
	}
	l.FocusM = math.Min(math.Max(l.FocusM, math.Max(*c.Near, *c.FocalMM/1000*1.01)), *c.Far)
	// Millimetres are as close as anyone reads a focus distance.
	l.FocusM = math.Round(l.FocusM*1000) / 1000
	return l
}
