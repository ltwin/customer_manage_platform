import { Matrix3, Vector3 } from 'three'
import * as tsl from 'three/tsl'
import type { TSLNode } from '../../materials/patterns.ts'

// The area lights' form factors (data: 面光源, Epic design「直接光的入口」).
// three's LTC (Heitz, Dupuy, Hill and Neubelt 2016, nodes/functions/BSDF/
// LTC.js) integrates a polygon; an ellipse is integrated as in Heitz and Hill
// 2017, "Real-Time Line- and Disk-Light Shading with Linearly Transformed
// Cosines" (selfshadow/ltc_code, ltc_disk): under the LTC's matrix an
// ellipse stays an ellipse, whose cone from the shaded point is that of a
// sphere cap with the same form factor and average direction (the vector
// form factor F: |F| the form factor, F / |F| the direction). The horizon is
// clipped as three clips a polygon's (LTC_ClippedSphereFormFactor, copied
// here: three/tsl does not export it).
//
// Each form factor is over π: a Lambertian surface's irradiance from a face
// of luminance L is π · L · F.z. The raster (direct.ts) and the hit points
// (trace/hit.ts) shade with these, I4. A CPU copy of the ellipse's
// (ellipseVectorFormFactorCPU, ltcEllipseCPU) is what the unit tests check
// (scripts/creative-scene-ltc.test.ts); keep the two the same.
//
// Checked against three r186's LTC.js: on an upgrade, read the frame (its T2
// negated) and the clip again (render/lightModel/nodes.ts lists the others).

type Op = (...args: TSLNode[]) => TSLNode
const T = tsl as unknown as Record<string, Op>
const { Fn, abs, acos, clamp, cos, cross, dot, float, inverseSqrt, length, mat3, max, min, normalize, select, sqrt, vec3 } = T

/** three's LTC_ClippedSphereFormFactor: a vector form factor f's horizon-clipped form factor, z the normal. */
export const clippedSphereFormFactor = (f: TSLNode): TSLNode => {
  const l = length(f)
  return max(l.mul(l).add(f.z).div(l.add(1)), 0)
}

/** clippedSphereFormFactor of f in any frame, the normal n: |f| and f · n are all it reads. */
export const clippedFormFactor = (f: TSLNode, n: TSLNode): TSLNode => {
  const l = length(f)
  return max(l.mul(l).add(dot(f, n)).div(l.add(1)), 0)
}

/** three's LTC_EdgeVectorFormFactor: an edge v1 → v2 of unit vectors' share of the vector form factor. */
const edgeVectorFormFactor = (v1: TSLNode, v2: TSLNode): TSLNode => {
  const x = dot(v1, v2)
  const y = abs(x).toVar()
  // three's rational fit to θ / sin θ / 2π.
  const a = y.mul(0.0145206).add(0.4965155).mul(y).add(0.8543985)
  const b = y.add(4.1616724).mul(y).add(3.417594)
  const v = a.div(b).toVar()
  const thetaSinTheta = select(x.greaterThan(0), v, inverseSqrt(max(float(1).sub(x.mul(x)), 1e-7)).mul(0.5).sub(v))
  return cross(v1, v2).mul(thetaSinTheta)
}

/**
 * A rectangle's vector form factor from P (three's corners p0…p3, its winding), in the corners' frame, unclipped; 0
 * from its back. three's winding sums to −F here: its LTC frame (T2 negated) is a mirror, which turns it round.
 */
export const rectVectorFormFactor = (P: TSLNode, p0: TSLNode, p1: TSLNode, p2: TSLNode, p3: TSLNode): TSLNode => {
  const lit = dot(cross(p1.sub(p0), p3.sub(p0)), P.sub(p0)).greaterThanEqual(0)
  const c0 = normalize(p0.sub(P)).toVar()
  const c1 = normalize(p1.sub(P)).toVar()
  const c2 = normalize(p2.sub(P)).toVar()
  const c3 = normalize(p3.sub(P)).toVar()
  const f = edgeVectorFormFactor(c0, c1).add(edgeVectorFormFactor(c1, c2)).add(edgeVectorFormFactor(c2, c3)).add(edgeVectorFormFactor(c3, c0))
  return select(lit, f.negate(), vec3(0))
}

/**
 * The three real roots of x³ + c2 x² + c1 x + c0 = 0, as ltc_disk orders
 * them: [middle, smallest, largest], the smallest being the cone's one
 * negative eigenvalue. By Viète's cosines, not ltc_disk's Blinn: his
 * algorithm D's root is not always the smallest, and a disc seen face on (a
 * double root) takes it to 0 / 0. The acos is clamped, so a double root is
 * two equal ones.
 */
const solveCubic = Fn(([c0, c1, c2]: TSLNode[]) => {
  const shift = c2.div(3).toVar()
  const p = c1.sub(c2.mul(shift)).toVar()
  const q = shift.mul(shift).mul(shift).mul(2).sub(shift.mul(c1)).add(c0)
  const m = sqrt(max(p.div(-3), 0)).mul(2).toVar()
  const phi = acos(clamp(q.mul(3).div(min(p, -1e-20).mul(max(m, 1e-20))), -1, 1)).div(3).toVar()
  const root = (k: number) => m.mul(cos(phi.sub((2 * Math.PI * k) / 3))).sub(shift)
  return vec3(root(1), root(2), root(0))
})

/**
 * An ellipse's vector form factor from the origin: centre C, half axes V1 and
 * V2 (conjugate, not necessarily at right angles), unclipped. The caller
 * makes sure the origin is in front of it.
 */
export const ellipseVectorFormFactor = Fn(([C, V1In, V2In]: TSLNode[]) => {
  // The ellipse's own axes: the eigenvectors of V1, V2's Gram matrix.
  const d11 = dot(V1In, V1In).toVar()
  const d22 = dot(V2In, V2In).toVar()
  const d12 = dot(V1In, V2In).toVar()
  const tr = d11.add(d22)
  const det = sqrt(max(d11.mul(d22).sub(d12.mul(d12)), 0))
  const u = sqrt(max(tr.sub(det.mul(2)), 0)).mul(0.5)
  const v = sqrt(tr.add(det.mul(2))).mul(0.5)
  const eMax = u.add(v).mul(u.add(v)).toVar()
  const eMin = u.sub(v).mul(u.sub(v)).toVar()
  const wide = d11.greaterThan(d22)
  const W1 = select(wide, V1In.mul(d12).add(V2In.mul(eMax.sub(d11))), V2In.mul(d12).add(V1In.mul(eMax.sub(d22))))
  const W2 = select(wide, V1In.mul(d12).add(V2In.mul(eMin.sub(d11))), V2In.mul(d12).add(V1In.mul(eMin.sub(d22))))
  const skew = abs(d12).div(sqrt(d11.mul(d22))).greaterThan(1e-4)
  const a0 = select(skew, float(1).div(eMax), float(1).div(d11)).toVar()
  const b0 = select(skew, float(1).div(eMin), float(1).div(d22)).toVar()
  const V1 = select(skew, normalize(W1), V1In.mul(sqrt(a0))).toVar()
  const V2 = select(skew, normalize(W2), V2In.mul(sqrt(b0))).toVar()
  const V3 = cross(V1, V2).toVar()
  V3.assign(select(dot(C, V3).lessThan(0), V3.negate(), V3))
  // The cone through the ellipse, in the plane z = 1 of (V1, V2, V3).
  const L = max(dot(V3, C), 1e-6).toVar()
  const x0 = dot(V1, C).div(L).toVar()
  const y0 = dot(V2, C).div(L).toVar()
  const a = a0.mul(L).mul(L).toVar()
  const b = b0.mul(L).mul(L).toVar()
  const c0 = a.mul(b)
  const c1 = a.mul(b).mul(x0.mul(x0).add(y0.mul(y0)).add(1)).sub(a).sub(b)
  const c2 = float(1).sub(a.mul(x0.mul(x0).add(1))).sub(b.mul(y0.mul(y0).add(1)))
  const roots = solveCubic(c0, c1, c2).toVar()
  const e1 = roots.x
  const e2 = roots.y
  const e3 = roots.z
  const avgDir = normalize(V1.mul(a.mul(x0).div(a.sub(e2))).add(V2.mul(b.mul(y0).div(b.sub(e2)))).add(V3))
  const L1 = sqrt(e2.negate().div(e3))
  const L2 = sqrt(e2.negate().div(e1))
  const formFactor = L1.mul(L2).mul(inverseSqrt(L1.mul(L1).add(1).mul(L2.mul(L2).add(1))))
  return avgDir.mul(formFactor)
})

/**
 * An ellipse's horizon-clipped LTC integral from P (view space, its normal N,
 * the way to the eye V), the LTC's inverse matrix mInv: centre, halfX and
 * halfY its half axes; 0 behind it, which is the side halfX × halfY points to
 * (it lights along its −Z). In three's LTC_Evaluate's frame, its T2 negated.
 */
export const ltcEllipse = ({ N, V, P, mInv, centre, halfX, halfY }: Record<'N' | 'V' | 'P' | 'mInv' | 'centre' | 'halfX' | 'halfY', TSLNode>): TSLNode => {
  const lit = dot(cross(halfX, halfY), centre.sub(P)).greaterThan(0)
  const T1 = normalize(V.sub(N.mul(dot(V, N))))
  const T2 = cross(N, T1).negate()
  const m = mInv.mul(mat3(T1, T2, N).transpose()).toVar()
  const f = ellipseVectorFormFactor(m.mul(centre.sub(P)), m.mul(halfX), m.mul(halfY))
  return vec3(select(lit, clippedSphereFormFactor(f), float(0)))
}

/** An ellipse's vector form factor from P with three's identity LTC, in view space: |F| over the light, F / |F| its average direction (sheen, the diffuse's Fresnel). */
export const ellipseViewFormFactor = (P: TSLNode, centre: TSLNode, halfX: TSLNode, halfY: TSLNode): TSLNode => {
  const lit = dot(cross(halfX, halfY), centre.sub(P)).greaterThan(0)
  return select(lit, ellipseVectorFormFactor(centre.sub(P), halfX, halfY), vec3(0))
}

// The CPU copy, for the unit tests.

/** solveCubic on the CPU: [middle, smallest, largest]. */
export function solveCubicCPU(c0: number, c1: number, c2: number): [number, number, number] {
  const shift = c2 / 3
  const p = c1 - c2 * shift
  const q = 2 * shift ** 3 - shift * c1 + c0
  const m = 2 * Math.sqrt(Math.max(-p / 3, 0))
  const phi = Math.acos(Math.min(1, Math.max(-1, (3 * q) / (Math.min(p, -1e-20) * Math.max(m, 1e-20))))) / 3
  const root = (k: number) => m * Math.cos(phi - (2 * Math.PI * k) / 3) - shift
  return [root(1), root(2), root(0)]
}

/** ellipseVectorFormFactor on the CPU. */
export function ellipseVectorFormFactorCPU(C: Vector3, V1In: Vector3, V2In: Vector3): Vector3 {
  const d11 = V1In.dot(V1In)
  const d22 = V2In.dot(V2In)
  const d12 = V1In.dot(V2In)
  let a: number
  let b: number
  let V1: Vector3
  let V2: Vector3
  if (Math.abs(d12) / Math.sqrt(d11 * d22) > 1e-4) {
    const tr = d11 + d22
    const det = Math.sqrt(Math.max(d11 * d22 - d12 * d12, 0))
    const u = 0.5 * Math.sqrt(Math.max(tr - 2 * det, 0))
    const v = 0.5 * Math.sqrt(tr + 2 * det)
    const eMax = (u + v) ** 2
    const eMin = (u - v) ** 2
    const [P1, P2, d] = d11 > d22 ? [V1In, V2In, d11] : [V2In, V1In, d22]
    V1 = P1.clone().multiplyScalar(d12).addScaledVector(P2, eMax - d).normalize()
    V2 = P1.clone().multiplyScalar(d12).addScaledVector(P2, eMin - d).normalize()
    a = 1 / eMax
    b = 1 / eMin
  } else {
    a = 1 / d11
    b = 1 / d22
    V1 = V1In.clone().multiplyScalar(Math.sqrt(a))
    V2 = V2In.clone().multiplyScalar(Math.sqrt(b))
  }
  const V3 = new Vector3().crossVectors(V1, V2)
  if (C.dot(V3) < 0) V3.negate()
  const L = Math.max(V3.dot(C), 1e-6)
  const x0 = V1.dot(C) / L
  const y0 = V2.dot(C) / L
  a *= L * L
  b *= L * L
  const [e1, e2, e3] = solveCubicCPU(a * b, a * b * (1 + x0 * x0 + y0 * y0) - a - b, 1 - a * (1 + x0 * x0) - b * (1 + y0 * y0))
  const avgDir = V1.clone().multiplyScalar((a * x0) / (a - e2)).addScaledVector(V2, (b * y0) / (b - e2)).add(V3).normalize()
  const L1 = Math.sqrt(-e2 / e3)
  const L2 = Math.sqrt(-e2 / e1)
  return avgDir.multiplyScalar((L1 * L2) / Math.sqrt((1 + L1 * L1) * (1 + L2 * L2)))
}

/** The LTC frame's matrix on the CPU: mInv × (T1, T2, N)ᵀ, T2 negated as three's. */
export function ltcFrameCPU(N: Vector3, V: Vector3, mInv: Matrix3): Matrix3 {
  const T1 = V.clone().addScaledVector(N, -V.dot(N)).normalize()
  const T2 = new Vector3().crossVectors(N, T1).negate()
  return mInv.clone().multiply(new Matrix3().set(T1.x, T1.y, T1.z, T2.x, T2.y, T2.z, N.x, N.y, N.z))
}

/** ltcEllipse on the CPU. */
export function ltcEllipseCPU(N: Vector3, V: Vector3, P: Vector3, mInv: Matrix3, centre: Vector3, halfX: Vector3, halfY: Vector3): number {
  if (!(new Vector3().crossVectors(halfX, halfY).dot(centre.clone().sub(P)) > 0)) return 0
  const m = ltcFrameCPU(N, V, mInv)
  const f = ellipseVectorFormFactorCPU(centre.clone().sub(P).applyMatrix3(m), halfX.clone().applyMatrix3(m), halfY.clone().applyMatrix3(m))
  const l = f.length()
  return Math.max((l * l + f.z) / (l + 1), 0)
}
