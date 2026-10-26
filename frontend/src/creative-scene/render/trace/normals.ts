// A vertex's normal in the w of its position (S1 design「构建、缓存与提交」,
// D4): octahedral, two 11-bit parts made one integer under 2²², which a
// float holds exactly. Not the bits of a u32: a pattern that reads as NaN may
// be changed on the way to the GPU. About 0.1° off at worst, plenty for
// shading a reflection. NONE marks a geometry without normals, whose hits
// take the triangle's own. Plain code, for the build workers; the shader's
// decoding is hit.ts decodeNormal, checked against decodeNormal here.

/** The steps along each part. */
export const STEPS = 2047
/** One part's range: w is u + v × ROW. */
export const ROW = 2048
/** No normal: the hit's face normal stands in. */
export const NONE = -1

const signNotZero = (v: number) => (v >= 0 ? 1 : -1)

/** The normal (x, y, z), not necessarily of length one, as the integer stored in w. */
export function encodeNormal(x: number, y: number, z: number): number {
  const l1 = Math.abs(x) + Math.abs(y) + Math.abs(z)
  if (!(l1 > 0)) return NONE
  let u = x / l1
  let v = y / l1
  if (z < 0) {
    const fu = (1 - Math.abs(v)) * signNotZero(u)
    v = (1 - Math.abs(u)) * signNotZero(v)
    u = fu
  }
  const qu = Math.round((u * 0.5 + 0.5) * STEPS)
  const qv = Math.round((v * 0.5 + 0.5) * STEPS)
  return qu + qv * ROW
}

/** The unit normal an encoded w stands for (the shader's decodeNormal, on the CPU). */
export function decodeNormal(w: number): [number, number, number] {
  const qv = Math.floor(w / ROW)
  const qu = w - qv * ROW
  let x = (qu / STEPS) * 2 - 1
  let y = (qv / STEPS) * 2 - 1
  const z = 1 - Math.abs(x) - Math.abs(y)
  const t = Math.max(-z, 0)
  x += x >= 0 ? -t : t
  y += y >= 0 ? -t : t
  const l = Math.hypot(x, y, z)
  return [x / l, y / l, z / l]
}

/** A geometry's normals (xyz per vertex) encoded, one number a vertex; NONE for each without. */
export function encodeNormals(normals: ArrayLike<number> | null, vertices: number): Float32Array {
  const out = new Float32Array(vertices).fill(NONE)
  if (!normals) return out
  for (let i = 0; i < vertices; i++) out[i] = encodeNormal(normals[3 * i], normals[3 * i + 1], normals[3 * i + 2])
  return out
}
