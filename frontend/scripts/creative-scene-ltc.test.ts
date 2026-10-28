import test from 'node:test'
import assert from 'node:assert/strict'
import { Matrix3, Vector3 } from 'three'
import { ellipseVectorFormFactorCPU, ltcEllipseCPU, solveCubicCPU } from '../src/creative-scene/render/lightModel/ltc.ts'

// The ellipse's form factor (scene-area-light tasks 3.2, render/lightModel/ltc.ts), its CPU copy: against the
// closed form for a disc seen face on, numerical integration for a slanted ellipse off to the side, and three's
// polygon LTC (its frame, T2 negated) on a 256-gon for the same ellipse under an LTC's skewed matrix.

const v = (x: number, y: number, z: number) => new Vector3(x, y, z)

/** The vector form factor of the ellipse C + r (cos t V1 + sin t V2) from the origin, (1/π) ∫ ω cos θl dA / d², by the midpoint rule. */
function integrate(C: Vector3, V1: Vector3, V2: Vector3, rs = 400, ts = 800): Vector3 {
  const n = new Vector3().crossVectors(V1, V2)
  const jacobian = n.length()
  n.normalize()
  const out = new Vector3()
  const q = new Vector3()
  for (let i = 0; i < rs; i++) {
    const r = (i + 0.5) / rs
    for (let j = 0; j < ts; j++) {
      const t = ((j + 0.5) / ts) * 2 * Math.PI
      q.copy(C).addScaledVector(V1, r * Math.cos(t)).addScaledVector(V2, r * Math.sin(t))
      const d2 = q.lengthSq()
      const w = q.clone().normalize()
      const dA = (r * jacobian * (2 * Math.PI)) / (rs * ts)
      out.addScaledVector(w, (Math.abs(w.dot(n)) * dA) / d2)
    }
  }
  return out.divideScalar(Math.PI)
}

/** three r186's LTC_Evaluate (LTC.js) with the exact edge integral, for a polygon in its winding. */
function ltcPolygon(N: Vector3, V: Vector3, P: Vector3, mInv: Matrix3, points: Vector3[]): number {
  const lightNormal = new Vector3().crossVectors(points[1].clone().sub(points[0]), points[points.length - 1].clone().sub(points[0]))
  if (lightNormal.dot(P.clone().sub(points[0])) < 0) return 0
  const T1 = V.clone().addScaledVector(N, -V.dot(N)).normalize()
  const T2 = new Vector3().crossVectors(N, T1).negate()
  const basis = new Matrix3().set(T1.x, T1.y, T1.z, T2.x, T2.y, T2.z, N.x, N.y, N.z)
  const mat = mInv.clone().multiply(basis)
  const coords = points.map((p) => p.clone().sub(P).applyMatrix3(mat).normalize())
  const f = new Vector3()
  coords.forEach((a, k) => {
    const b = coords[(k + 1) % coords.length]
    const theta = Math.acos(Math.min(1, Math.max(-1, a.dot(b))))
    f.addScaledVector(new Vector3().crossVectors(a, b).normalize(), theta / (2 * Math.PI))
  })
  const l = f.length()
  return Math.max((l * l + f.z) / (l + 1), 0)
}

test('the cubic gives its middle root, its smallest, its largest', () => {
  // (x − 1)(x + 2)(x − 3) = x³ − 2x² − 5x + 6
  const [m, s, l] = solveCubicCPU(6, -5, -2)
  assert.ok(Math.abs(m - 1) < 1e-9 && Math.abs(s + 2) < 1e-9 && Math.abs(l - 3) < 1e-9, `${[m, s, l]}`)
})

test('a disc face on: R² / (R² + d²), straight at it', () => {
  for (const [R, d] of [
    [0.5, 1],
    [1, 1],
    [0.1, 3],
    [2, 0.5],
  ]) {
    const f = ellipseVectorFormFactorCPU(v(0, 0, d), v(R, 0, 0), v(0, R, 0))
    const want = (R * R) / (R * R + d * d)
    assert.ok(Math.abs(f.z - want) < 1e-6 * Math.max(1, want) && Math.hypot(f.x, f.y) < 1e-9, `R ${R} d ${d}: ${f.toArray()} vs ${want}`)
  }
})

test('a slanted ellipse off to the side, its half axes not at right angles: as integrated', () => {
  for (const [C, V1, V2] of [
    [v(0.8, -0.3, 1.5), v(0.6, 0.1, 0.2), v(-0.1, 0.25, 0.15)],
    [v(-0.4, 0.9, 0.7), v(0.3, 0, -0.1), v(0.2, 0.5, 0.3)],
    [v(0, 0, 2), v(1, 0, 0), v(0, 0.3, 0)],
  ] as const) {
    const got = ellipseVectorFormFactorCPU(C, V1, V2)
    const want = integrate(C, V1, V2)
    assert.ok(got.distanceTo(want) < 2e-3 * want.length(), `${C.toArray()}: ${got.toArray()} vs ${want.toArray()}`)
  }
})

test('under an LTC’s skewed matrix the ellipse integrates as three’s polygon LTC does a 256-gon of it, and nothing from behind', () => {
  // The LTC tables' shape (LTC.js): columns (a, 0, b), (0, 1, 0), (c, 0, d).
  const mInv = new Matrix3().set(1.2, 0, -0.4, 0, 1, 0, 0.3, 0, 0.9)
  const N = v(0, 0, 1)
  const P = v(0, 0, 0)
  for (const [V, centre, halfX, halfY] of [
    // Lit along its −Z, so halfX × halfY points away from P.
    [v(0.6, 0, 0.8), v(0.5, 0.9, 1.4), v(0.4, 0, 0), v(0, 0.3, 0.1)],
    [v(-0.2, 0.7, 0.68), v(-0.7, -0.6, 1.1), v(0.35, -0.1, 0.15), v(0.2, 0.3, 0)],
  ] as const) {
    assert.ok(new Vector3().crossVectors(halfX, halfY).dot(centre.clone().sub(P)) > 0, 'lit side')
    const n = 256
    // three's corners go round clockwise in the light's XY plane (p0 at +x −y, p1 at −x −y, ...).
    const polygon = Array.from({ length: n }, (_, k) => {
      const t = (-2 * Math.PI * k) / n
      return centre.clone().addScaledVector(halfX, Math.cos(t)).addScaledVector(halfY, Math.sin(t))
    })
    const want = ltcPolygon(N, V.clone().normalize(), P, mInv, polygon)
    const got = ltcEllipseCPU(N, V.clone().normalize(), P, mInv, centre, halfX, halfY)
    assert.ok(want > 0.01 && Math.abs(got - want) < 2e-3 * want, `${centre.toArray()}: ${got} vs ${want}`)
    // From behind: nothing.
    assert.equal(ltcEllipseCPU(N, V.clone().normalize(), P, mInv, centre, halfY, halfX), 0)
  }
})
