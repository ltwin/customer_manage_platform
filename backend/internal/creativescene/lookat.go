package creativescene

import (
	"encoding/json"
	"math"
)

// look_at is an edit_scene convenience: an add_objects or update_objects item
// may name a world point instead of a rotation. resolveLookAt turns it into a
// whole transform the way three.js Object3D.lookAt would, before the batch
// reaches the scene command, so the command's strict decoding never sees it.
// Cameras and lights point their local −Z at the target, other objects +Z.

type vec3 [3]float64

// mat4 is column-major like three.js Matrix4.elements.
type mat4 [16]float64

type quat [4]float64 // x, y, z, w

func (a vec3) sub(b vec3) vec3 { return vec3{a[0] - b[0], a[1] - b[1], a[2] - b[2]} }

func (a vec3) cross(b vec3) vec3 {
	return vec3{a[1]*b[2] - a[2]*b[1], a[2]*b[0] - a[0]*b[2], a[0]*b[1] - a[1]*b[0]}
}

func (a vec3) lengthSq() float64 { return a[0]*a[0] + a[1]*a[1] + a[2]*a[2] }

func (a vec3) normalize() vec3 {
	l := math.Sqrt(a.lengthSq())
	if l == 0 {
		return a
	}
	return vec3{a[0] / l, a[1] / l, a[2] / l}
}

func identity4() mat4 { return mat4{0: 1, 5: 1, 10: 1, 15: 1} }

// compose is three.js Matrix4.compose.
func compose(p vec3, q quat, s vec3) mat4 {
	x, y, z, w := q[0], q[1], q[2], q[3]
	x2, y2, z2 := x+x, y+y, z+z
	xx, xy, xz := x*x2, x*y2, x*z2
	yy, yz, zz := y*y2, y*z2, z*z2
	wx, wy, wz := w*x2, w*y2, w*z2
	return mat4{
		(1 - (yy + zz)) * s[0], (xy + wz) * s[0], (xz - wy) * s[0], 0,
		(xy - wz) * s[1], (1 - (xx + zz)) * s[1], (yz + wx) * s[1], 0,
		(xz + wy) * s[2], (yz - wx) * s[2], (1 - (xx + yy)) * s[2], 0,
		p[0], p[1], p[2], 1,
	}
}

// mul is a × b.
func (a mat4) mul(b mat4) mat4 {
	var out mat4
	for col := range 4 {
		for row := range 4 {
			var v float64
			for k := range 4 {
				v += a[k*4+row] * b[col*4+k]
			}
			out[col*4+row] = v
		}
	}
	return out
}

func (a mat4) apply(p vec3) vec3 {
	w := 1 / (a[3]*p[0] + a[7]*p[1] + a[11]*p[2] + a[15])
	return vec3{
		(a[0]*p[0] + a[4]*p[1] + a[8]*p[2] + a[12]) * w,
		(a[1]*p[0] + a[5]*p[1] + a[9]*p[2] + a[13]) * w,
		(a[2]*p[0] + a[6]*p[1] + a[10]*p[2] + a[14]) * w,
	}
}

// determinant is three.js Matrix4.determinant.
func (a mat4) determinant() float64 {
	n11, n12, n13, n14 := a[0], a[4], a[8], a[12]
	n21, n22, n23, n24 := a[1], a[5], a[9], a[13]
	n31, n32, n33, n34 := a[2], a[6], a[10], a[14]
	n41, n42, n43, n44 := a[3], a[7], a[11], a[15]
	return n41*(n14*n23*n32-n13*n24*n32-n14*n22*n33+n12*n24*n33+n13*n22*n34-n12*n23*n34) +
		n42*(n11*n23*n34-n11*n24*n33+n14*n21*n33-n13*n21*n34+n13*n24*n31-n14*n23*n31) +
		n43*(n11*n24*n32-n11*n22*n34-n14*n21*n32+n12*n21*n34+n14*n22*n31-n12*n24*n31) +
		n44*(-n13*n22*n31-n11*n23*n32+n11*n22*n33+n13*n21*n32-n12*n21*n33+n12*n23*n31)
}

// invert is three.js Matrix4.invert; a singular matrix gives zeros.
func (a mat4) invert() mat4 {
	n11, n21, n31, n41 := a[0], a[1], a[2], a[3]
	n12, n22, n32, n42 := a[4], a[5], a[6], a[7]
	n13, n23, n33, n43 := a[8], a[9], a[10], a[11]
	n14, n24, n34, n44 := a[12], a[13], a[14], a[15]
	t11 := n23*n34*n42 - n24*n33*n42 + n24*n32*n43 - n22*n34*n43 - n23*n32*n44 + n22*n33*n44
	t12 := n14*n33*n42 - n13*n34*n42 - n14*n32*n43 + n12*n34*n43 + n13*n32*n44 - n12*n33*n44
	t13 := n13*n24*n42 - n14*n23*n42 + n14*n22*n43 - n12*n24*n43 - n13*n22*n44 + n12*n23*n44
	t14 := n14*n23*n32 - n13*n24*n32 - n14*n22*n33 + n12*n24*n33 + n13*n22*n34 - n12*n23*n34
	det := n11*t11 + n21*t12 + n31*t13 + n41*t14
	if det == 0 {
		return mat4{}
	}
	d := 1 / det
	return mat4{
		t11 * d,
		(n24*n33*n41 - n23*n34*n41 - n24*n31*n43 + n21*n34*n43 + n23*n31*n44 - n21*n33*n44) * d,
		(n22*n34*n41 - n24*n32*n41 + n24*n31*n42 - n21*n34*n42 - n22*n31*n44 + n21*n32*n44) * d,
		(n23*n32*n41 - n22*n33*n41 - n23*n31*n42 + n21*n33*n42 + n22*n31*n43 - n21*n32*n43) * d,
		t12 * d,
		(n13*n34*n41 - n14*n33*n41 + n14*n31*n43 - n11*n34*n43 - n13*n31*n44 + n11*n33*n44) * d,
		(n14*n32*n41 - n12*n34*n41 - n14*n31*n42 + n11*n34*n42 + n12*n31*n44 - n11*n32*n44) * d,
		(n12*n33*n41 - n13*n32*n41 + n13*n31*n42 - n11*n33*n42 - n12*n31*n43 + n11*n32*n43) * d,
		t13 * d,
		(n14*n23*n41 - n13*n24*n41 - n14*n21*n43 + n11*n24*n43 + n13*n21*n44 - n11*n23*n44) * d,
		(n12*n24*n41 - n14*n22*n41 + n14*n21*n42 - n11*n24*n42 - n12*n21*n44 + n11*n22*n44) * d,
		(n13*n22*n41 - n12*n23*n41 - n13*n21*n42 + n11*n23*n42 + n12*n21*n43 - n11*n22*n43) * d,
		t14 * d,
		(n13*n24*n31 - n14*n23*n31 + n14*n21*n33 - n11*n24*n33 - n13*n21*n34 + n11*n23*n34) * d,
		(n14*n22*n31 - n12*n24*n31 - n14*n21*n32 + n11*n24*n32 + n12*n21*n34 - n11*n22*n34) * d,
		(n12*n23*n31 - n13*n22*n31 + n13*n21*n32 - n11*n23*n32 - n12*n21*n33 + n11*n22*n33) * d,
	}
}

// decompose is three.js Matrix4.decompose: a negative determinant flips the
// X scale. With shear (a non-uniformly scaled parent of a rotated child) the
// rotation part is not orthogonal and the result only approximates it, as in
// three.js; the quaternion is normalized so the scene accepts it.
func (a mat4) decompose() (vec3, quat, vec3) {
	sx := math.Sqrt(vec3{a[0], a[1], a[2]}.lengthSq())
	sy := math.Sqrt(vec3{a[4], a[5], a[6]}.lengthSq())
	sz := math.Sqrt(vec3{a[8], a[9], a[10]}.lengthSq())
	if a.determinant() < 0 {
		sx = -sx
	}
	r := a
	for i, s := range []float64{sx, sy, sz} {
		if s == 0 {
			continue
		}
		r[i*4], r[i*4+1], r[i*4+2] = r[i*4]/s, r[i*4+1]/s, r[i*4+2]/s
	}
	return vec3{a[12], a[13], a[14]}, quatFromMatrix(r).normalize(), vec3{sx, sy, sz}
}

// extractRotation is three.js Matrix4.extractRotation: the columns divided by
// their lengths.
func (a mat4) extractRotation() mat4 {
	out := identity4()
	for col := range 3 {
		c := vec3{a[col*4], a[col*4+1], a[col*4+2]}
		l := math.Sqrt(c.lengthSq())
		if l == 0 {
			l = 1
		}
		out[col*4], out[col*4+1], out[col*4+2] = c[0]/l, c[1]/l, c[2]/l
	}
	return out
}

// lookAtMatrix is three.js Matrix4.lookAt, degenerate cases included.
func lookAtMatrix(eye, target, up vec3) mat4 {
	z := eye.sub(target)
	if z.lengthSq() == 0 {
		z[2] = 1
	}
	z = z.normalize()
	x := up.cross(z)
	if x.lengthSq() == 0 {
		if math.Abs(up[2]) == 1 {
			z[0] += 0.0001
		} else {
			z[2] += 0.0001
		}
		z = z.normalize()
		x = up.cross(z)
	}
	x = x.normalize()
	y := z.cross(x)
	m := identity4()
	m[0], m[4], m[8] = x[0], y[0], z[0]
	m[1], m[5], m[9] = x[1], y[1], z[1]
	m[2], m[6], m[10] = x[2], y[2], z[2]
	return m
}

// quatFromMatrix is three.js Quaternion.setFromRotationMatrix.
func quatFromMatrix(te mat4) quat {
	m11, m12, m13 := te[0], te[4], te[8]
	m21, m22, m23 := te[1], te[5], te[9]
	m31, m32, m33 := te[2], te[6], te[10]
	trace := m11 + m22 + m33
	switch {
	case trace > 0:
		s := 0.5 / math.Sqrt(trace+1)
		return quat{(m32 - m23) * s, (m13 - m31) * s, (m21 - m12) * s, 0.25 / s}
	case m11 > m22 && m11 > m33:
		s := 2 * math.Sqrt(1+m11-m22-m33)
		return quat{0.25 * s, (m12 + m21) / s, (m13 + m31) / s, (m32 - m23) / s}
	case m22 > m33:
		s := 2 * math.Sqrt(1+m22-m11-m33)
		return quat{(m12 + m21) / s, 0.25 * s, (m23 + m32) / s, (m13 - m31) / s}
	default:
		s := 2 * math.Sqrt(1+m33-m11-m22)
		return quat{(m13 + m31) / s, (m23 + m32) / s, 0.25 * s, (m21 - m12) / s}
	}
}

// mulQuat is a × b (three.js Quaternion.multiplyQuaternions).
func mulQuat(a, b quat) quat {
	ax, ay, az, aw := a[0], a[1], a[2], a[3]
	bx, by, bz, bw := b[0], b[1], b[2], b[3]
	return quat{
		ax*bw + aw*bx + ay*bz - az*by,
		ay*bw + aw*by + az*bx - ax*bz,
		az*bw + aw*bz + ax*by - ay*bx,
		aw*bw - ax*bx - ay*by - az*bz,
	}
}

func (q quat) normalize() quat {
	l := math.Sqrt(q[0]*q[0] + q[1]*q[1] + q[2]*q[2] + q[3]*q[3])
	if l == 0 {
		return quat{0, 0, 0, 1}
	}
	return quat{q[0] / l, q[1] / l, q[2] / l, q[3] / l}
}

// lookAtRotation is the local rotation three.js Object3D.lookAt gives an
// object whose parent has world matrix parent (nil at the root) and whose
// world position is eye. The result is normalized: a parent with non-uniform
// scale makes three.js return a quaternion slightly off unit length, which the
// scene would refuse.
func lookAtRotation(kind string, eye, target vec3, parent *mat4) quat {
	up := vec3{0, 1, 0}
	var m mat4
	if kind == KindCamera || kind == KindLight {
		m = lookAtMatrix(eye, target, up)
	} else {
		m = lookAtMatrix(target, eye, up)
	}
	q := quatFromMatrix(m)
	if parent != nil {
		p := quatFromMatrix(parent.extractRotation())
		q = mulQuat(quat{-p[0], -p[1], -p[2], p[3]}, q)
	}
	return q.normalize()
}

// lookAtItem is what resolveLookAt needs from one add or update item; the
// item itself stays raw so everything else reaches strict decoding as sent.
type lookAtItem struct {
	ID        string    `json:"id"`
	ObjectID  string    `json:"object_id"`
	Kind      string    `json:"kind"`
	ParentID  *string   `json:"parent_id"`
	LookAt    []float64 `json:"look_at"`
	Transform *struct {
		Position []float64 `json:"position"`
		Rotation []float64 `json:"rotation"`
		Scale    []float64 `json:"scale"`
	} `json:"transform"`
}

// lookAtScene answers where things are for resolveLookAt: the current
// snapshot plus the objects the batch adds.
type lookAtScene struct {
	objects map[string]Object
	added   map[string]lookAtItem
	world   map[string]mat4
}

func (s *lookAtScene) parentOf(id string) *string {
	if a, ok := s.added[id]; ok {
		return a.ParentID
	}
	if o, ok := s.objects[id]; ok {
		return o.ParentID
	}
	return nil
}

// ancestors lists the parent, grandparent and so on of object id whose
// parent is parent, as the batch would leave them; false when the chain comes
// back to an object already on it. The arguments are the model's, read before
// the scene's own checks refuse a loop, so the walk has to stop on its own.
func (s *lookAtScene) ancestors(id string, parent *string) ([]string, bool) {
	seen := map[string]bool{id: true}
	var out []string
	for p := parent; p != nil; p = s.parentOf(*p) {
		if seen[*p] {
			return nil, false
		}
		seen[*p] = true
		out = append(out, *p)
	}
	return out, true
}

// worldMatrix is the world matrix of an existing or added object; added
// objects with look_at count with the rotation they were sent with, which is
// why moving a parent and aiming its child in one batch is refused. Callers
// check the chain with ancestors first; a loop is still not followed.
func (s *lookAtScene) worldMatrix(id string) (mat4, bool) {
	return s.worldMatrixOnce(id, map[string]bool{})
}

func (s *lookAtScene) worldMatrixOnce(id string, visiting map[string]bool) (mat4, bool) {
	if visiting[id] {
		return mat4{}, false
	}
	visiting[id] = true
	if m, ok := s.world[id]; ok {
		return m, true
	}
	var local mat4
	if a, ok := s.added[id]; ok {
		if a.Transform == nil || len(a.Transform.Position) != 3 || len(a.Transform.Rotation) != 4 || len(a.Transform.Scale) != 3 {
			return mat4{}, false
		}
		t := a.Transform
		local = compose(vec3(t.Position), quat(t.Rotation), vec3(t.Scale))
	} else if o, ok := s.objects[id]; ok {
		t := o.Transform
		if len(t.Position) != 3 || len(t.Rotation) != 4 || len(t.Scale) != 3 {
			return mat4{}, false
		}
		local = compose(vec3(t.Position), quat(t.Rotation), vec3(t.Scale))
	} else {
		return mat4{}, false
	}
	m := local
	if p := s.parentOf(id); p != nil {
		pm, ok := s.worldMatrixOnce(*p, visiting)
		if !ok {
			return mat4{}, false
		}
		m = pm.mul(local)
	}
	s.world[id] = m
	return m, true
}

// resolveLookAt replaces look_at with transform in the add_objects and
// update_objects items of ops; operations without look_at pass through
// byte for byte.
func resolveLookAt(ops []json.RawMessage, cur Scene) ([]json.RawMessage, error) {
	type decoded struct {
		Op      string            `json:"op"`
		Objects []json.RawMessage `json:"objects"`
		Updates []json.RawMessage `json:"updates"`
		Moves   []struct {
			ObjectID string `json:"object_id"`
		} `json:"moves"`
	}
	heads := make([]decoded, len(ops))
	found := false
	for i, raw := range ops {
		if json.Unmarshal(raw, &heads[i]) != nil {
			return ops, nil // the scene command reports the shape
		}
		for _, items := range [][]json.RawMessage{heads[i].Objects, heads[i].Updates} {
			for _, item := range items {
				var probe map[string]json.RawMessage
				if json.Unmarshal(item, &probe) == nil {
					if _, ok := probe["look_at"]; ok {
						found = true
					}
				}
			}
		}
	}
	if !found {
		return ops, nil
	}

	s := &lookAtScene{objects: make(map[string]Object, len(cur.Objects)), added: map[string]lookAtItem{}, world: map[string]mat4{}}
	for _, o := range cur.Objects {
		s.objects[o.ID] = o
	}
	// moved lists what the batch moves besides look_at items themselves:
	// reparented objects and updated transforms.
	reparented, moved := map[string]bool{}, map[string]bool{}
	for _, h := range heads {
		switch h.Op {
		case "add_objects":
			for _, raw := range h.Objects {
				var it lookAtItem
				if json.Unmarshal(raw, &it) == nil && it.ID != "" {
					s.added[it.ID] = it
				}
			}
		case "update_objects":
			for _, raw := range h.Updates {
				var it lookAtItem
				if json.Unmarshal(raw, &it) == nil && (it.Transform != nil || it.LookAt != nil) {
					moved[it.ObjectID] = true
				}
			}
		case "reparent_objects":
			for _, m := range h.Moves {
				reparented[m.ObjectID] = true
			}
		}
	}

	out := make([]json.RawMessage, len(ops))
	for i, h := range heads {
		var items []json.RawMessage
		key := ""
		switch h.Op {
		case "add_objects":
			items, key = h.Objects, "objects"
		case "update_objects":
			items, key = h.Updates, "updates"
		default:
			out[i] = ops[i]
			continue
		}
		changed := false
		next := make([]json.RawMessage, len(items))
		for j, raw := range items {
			resolved, ok, err := s.resolveItem(raw, h.Op == "add_objects", reparented, moved)
			if err != nil {
				return nil, err
			}
			next[j] = raw
			if ok {
				next[j], changed = resolved, true
			}
		}
		if !changed {
			out[i] = ops[i]
			continue
		}
		var whole map[string]json.RawMessage
		if err := json.Unmarshal(ops[i], &whole); err != nil {
			return nil, err
		}
		list, err := json.Marshal(next)
		if err != nil {
			return nil, err
		}
		whole[key] = list
		if out[i], err = json.Marshal(whole); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// resolveItem returns the item with look_at turned into transform; ok is
// false when the item has no look_at.
func (s *lookAtScene) resolveItem(raw json.RawMessage, add bool, reparented, moved map[string]bool) (json.RawMessage, bool, error) {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return nil, false, nil
	}
	if _, ok := fields["look_at"]; !ok {
		return nil, false, nil
	}
	var it lookAtItem
	if err := json.Unmarshal(raw, &it); err != nil {
		return nil, false, invalid("look_at 须为 [x, y, z]")
	}
	id := it.ObjectID
	if add {
		id = it.ID
	}
	if len(it.LookAt) != 3 || !finite(it.LookAt[0]) || !finite(it.LookAt[1]) || !finite(it.LookAt[2]) {
		return nil, false, invalid("物件 %s 的 look_at 须为 [x, y, z] 世界坐标", id)
	}
	if it.Transform != nil && it.Transform.Rotation != nil {
		return nil, false, invalid("物件 %s 不能同时写 look_at 和 transform.rotation", id)
	}

	kind, parent := it.Kind, it.ParentID
	var position, scale []float64
	if it.Transform != nil {
		position, scale = it.Transform.Position, it.Transform.Scale
	}
	if !add {
		o, ok := s.objects[id]
		if !ok {
			return nil, false, invalid("物件 %s 不存在", id)
		}
		kind, parent = o.Kind, o.ParentID
		if position == nil {
			position = o.Transform.Position
		}
		if scale == nil {
			scale = o.Transform.Scale
		}
	}
	if position == nil {
		return nil, false, invalid("新增物件 %s 用 look_at 时须写 transform.position", id)
	}
	if scale == nil {
		scale = []float64{1, 1, 1}
	}
	if len(position) != 3 || len(scale) != 3 {
		return nil, false, invalid("物件 %s 的 transform 须为 position[3]、scale[3]", id)
	}

	if reparented[id] {
		return nil, false, invalid("物件 %s 在同一次修改里也被改了父级，请分两次提交", id)
	}
	ancestors, ok := s.ancestors(id, parent)
	if !ok {
		return nil, false, invalid("物件 %s 的父级链成环", id)
	}
	for _, p := range ancestors {
		if reparented[p] || moved[p] {
			return nil, false, invalid("物件 %s 的父级在同一次修改里也被移动，请分两次提交", id)
		}
	}
	var parentWorld *mat4
	if parent != nil {
		m, ok := s.worldMatrix(*parent)
		if !ok {
			return nil, false, invalid("物件 %s 的父级 %s 不存在", id, *parent)
		}
		parentWorld = &m
	}

	eye := vec3(position)
	if parentWorld != nil {
		eye = parentWorld.apply(eye)
	}
	target := vec3(it.LookAt)
	if eye.sub(target).lengthSq() < 1e-12 {
		return nil, false, invalid("物件 %s 的 look_at 目标点与物件位置重合", id)
	}
	q := lookAtRotation(kind, eye, target, parentWorld)
	transform, err := json.Marshal(Transform{Position: position, Rotation: q[:], Scale: scale})
	if err != nil {
		return nil, false, err
	}
	delete(fields, "look_at")
	fields["transform"] = transform
	out, err := json.Marshal(fields)
	return out, true, err
}
