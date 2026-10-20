// Where a model dropped from the asset library lands (design「吸附」): its
// reference point on the first surface along the pointer's ray that suits its
// mount, as in Blender's asset browser. Document space throughout (Y up, the
// model's front facing +Z); the viewport converts its hits before calling.

export type Vec3 = [number, number, number]
export type Quat = [number, number, number, number]
export type Mount = 'floor' | 'wall' | 'ceiling' | 'free'

/** A surface the ray met, nearest first: the point and the face's world normal, facing the ray. */
export type Hit = { point: Vec3; normal: Vec3 }
export type Bounds = { min: readonly number[]; max: readonly number[] }
export type Ray = { origin: Vec3; direction: Vec3 }
export type Placed = { position: Vec3; rotation: Quat }

/** Faces within 45° of up, of down, or of vertical. */
const steep = Math.SQRT1_2
const identity: Quat = [0, 0, 0, 1]

const faceUp = (n: Vec3) => n[1] >= steep
const faceDown = (n: Vec3) => n[1] <= -steep
const faceSide = (n: Vec3) => Math.abs(n[1]) < steep

/** The bounds' bottom centre, in model space. */
function bottomCentre(b: Bounds): Vec3 {
  return [(b.min[0] + b.max[0]) / 2, b.min[1], (b.min[2] + b.max[2]) / 2]
}

const minus = (a: Vec3, b: Vec3): Vec3 => [a[0] - b[0], a[1] - b[1], a[2] - b[2]]

/** A turn about Y that points the model's front (+Z) along the normal's horizontal part. */
function facing(n: Vec3): Quat {
  const yaw = Math.atan2(n[0], n[2])
  return [0, Math.sin(yaw / 2), 0, Math.cos(yaw / 2)]
}

/**
 * The placement for a model of this mount and bounds. Without a suitable
 * surface it is placed like a free model: its bounds' bottom on the first
 * face that looks up, else on the ground where the ray meets it, else at
 * `fallback` on the ground (dragging towards the sky). It is never buried.
 */
export function snap({ hits, ray, mount, bounds, fallback }: { hits: readonly Hit[]; ray: Ray | null; mount: Mount; bounds: Bounds; fallback: Vec3 }): Placed {
  if (mount === 'wall') {
    const hit = hits.find((h) => faceSide(h.normal))
    if (hit) return { position: hit.point, rotation: facing(hit.normal) }
  } else if (mount !== 'free') {
    const hit = hits.find((h) => (mount === 'ceiling' ? faceDown(h.normal) : faceUp(h.normal)))
    if (hit) return { position: hit.point, rotation: identity }
  }
  // A free model's reference point is its centre: its bounds' bottom goes on the surface.
  const up = hits.find((h) => faceUp(h.normal))
  if (up) return { position: minus(up.point, bottomCentre(bounds)), rotation: identity }
  const ground = ray && ray.direction[1] < 0 && ray.origin[1] > 0 ? ray.origin[1] / -ray.direction[1] : null
  const at: Vec3 = ground === null ? [fallback[0], 0, fallback[2]] : [ray!.origin[0] + ray!.direction[0] * ground, 0, ray!.origin[2] + ray!.direction[2] * ground]
  return { position: minus(at, bottomCentre(bounds)), rotation: identity }
}
