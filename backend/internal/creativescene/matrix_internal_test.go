package creativescene

import (
	"math"
	"testing"
)

// The golden values come from three.js 0.186 (frontend/node_modules/three):
// Matrix4.compose of the inputs, then determinant, invert and decompose.
var matrixGolden = []struct {
	name     string
	p        vec3
	q        quat
	s        vec3
	det      float64
	inv      mat4
	dq       quat
	ds       vec3
	position vec3
}{
	{
		name: "rotated, non-uniform scale",
		p:    vec3{1, 2, 3}, q: quat{-0.0575399881803354, -0.529169808944497, 0.215672410090385, 0.818629265655496}, s: vec3{2, 0.5, 1.5},
		det: 1.5,
		inv: mat4{0.17346472482745, -0.584429288569544, -0.594138240040957, 0, 0.207004171246499, 1.80069824514405, -0.0893645463629791, 0, 0.420784104175867, -0.644924985248088, 0.288891284082469, 0, -1.84982537984805, -1.08219224597428, -0.0938065194804912, 1},
		dq:  quat{-0.0575399881803354, -0.529169808944497, 0.215672410090385, 0.818629265655496}, ds: vec3{2, 0.5, 1.5},
		position: vec3{1, 2, 3},
	},
	{
		// A mirrored object: three.js puts the negative scale on X.
		name: "mirrored",
		p:    vec3{-4, 0.25, 9}, q: quat{0.346707542766055, 0.924984163573468, -0.0758403902571983, 0.135817569309168}, s: vec3{-1, 1, 3},
		det: -3,
		inv: mat4{0.722694935312147, 0.661998887820528, 0.0662231102650205, 0, -0.620797057979762, 0.74808422998952, -0.0781600904445202, 0, 0.303847072189029, -0.0461243684549998, -0.317201215381724, 0, 0.311355356042266, 2.87609380987973, 3.13924340210672, 1},
		dq:  quat{0.346707542766055, 0.924984163573468, -0.0758403902571983, 0.135817569309168}, ds: vec3{-1, 1, 3},
		position: vec3{-4, 0.25, 9},
	},
}

func near(t *testing.T, what string, got, want []float64) {
	t.Helper()
	for i := range want {
		if math.Abs(got[i]-want[i]) > 1e-9 {
			t.Fatalf("%s: got %v, want %v", what, got, want)
		}
	}
}

func TestMatrixMatchesThreejs(t *testing.T) {
	for _, c := range matrixGolden {
		t.Run(c.name, func(t *testing.T) {
			m := compose(c.p, c.q, c.s)
			near(t, "determinant", []float64{m.determinant()}, []float64{c.det})
			inv := m.invert()
			near(t, "invert", inv[:], c.inv[:])
			p, q, s := m.decompose()
			near(t, "position", p[:], c.position[:])
			near(t, "quaternion", q[:], c.dq[:])
			near(t, "scale", s[:], c.ds[:])
		})
	}
	if inv := (mat4{}).invert(); inv != (mat4{}) {
		t.Fatalf("a singular matrix inverts to zeros in three.js, got %v", inv)
	}
}

// TestKeepWorldLocal is the reparent formula against three.js:
// parent.invert().multiply(world).decompose(), with a rotated, scaled parent.
func TestKeepWorldLocal(t *testing.T) {
	parent := mat4{1.39341341869433, 0, -1.43471218179905, 0, 0, 2, 0, 0, 1.43471218179905, 0, 1.39341341869433, 0, 2, 1, -3, 1}
	world := mat4{0.936293363584199, 0.312991825785468, -0.159345079307978, 0, -0.289629477625515, 0.944702485994894, 0.153791997988964, 0, 0.198669330795061, -0.0978433950072557, 0.975170327201816, 0, 5, 1, 0, 1}
	p, q, s := parent.invert().mul(world).decompose()
	near(t, "position", p[:], []float64{-0.0309740723285361, 0, 2.12109420037003})
	near(t, "quaternion", q[:], []float64{-0.000738459434010749, -0.298391140214303, 0.166277514054719, 0.939847844301968})
	near(t, "scale", s[:], []float64{0.5, 0.5, 0.5})
}
