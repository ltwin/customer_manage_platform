import { Color, Matrix4, Sphere, Vector2, Vector3, Vector4, type AmbientLight, type Camera, type DirectionalLight, type HemisphereLight, type Light, type Object3D, type OrthographicCamera, type PerspectiveCamera, type PointLight, type Scene, type SpotLight, type Texture } from 'three'
import * as tsl from 'three/tsl'
import type { TSLNode } from '../../materials/patterns.ts'
import { chordFactor, coefficients, type Volume } from './medium.ts'

// What every volume pass reads (scene-volumes design「登记与出图」「选灯与编译」):
// the volumes of the scene being drawn and the lights that light them, as
// uniforms shared by the viewport and the offscreen shooter, written just
// before a picture and drawn in the same synchronous call (prepareVolumes,
// as world/atmosphere/aerial.ts prepareAerial). The lights' slots decide the
// march's code: their kinds and whether each casts make its structure
// signature; positions, colours and cones are uniforms.

type Op = (...args: TSLNode[]) => TSLNode
const { uniform, uniformArray } = tsl as unknown as Record<string, Op>
type Uniform<T> = TSLNode & { value: T }
type Swappable = TSLNode & { value: Texture }

export const MAX_VOLUMES = 8
/** Lights that light the volumes, and their shadow maps: 4 a casting directional light (its cascades), 1 a spot or point. With the depth and the environment, 14 of WebGPU's 16 textures a stage. */
export const MAX_LIGHTS = 6
export const MAX_MAPS = 12

/** A volume object as <Volume> registers it: its document-space group, read live, and its fields. */
export type VolumeEntry = { object: Object3D; volume: Volume; rank: number }

/** The scene's volume objects by object ID (objects/Volume.tsx). */
export function volumeObjects(scene: Object3D): Map<string, VolumeEntry> {
  let map = scene.userData.volumeObjects as Map<string, VolumeEntry> | undefined
  if (!map) {
    map = new Map()
    scene.userData.volumeObjects = map
  }
  return map
}

// The volumes: inverse world matrix; σs and g; σt and the edge falloff; shape (0 box, 1 sphere) and the chord factor.
const inv = Array.from({ length: MAX_VOLUMES }, () => new Matrix4())
const sct = Array.from({ length: MAX_VOLUMES }, () => new Vector4())
const ext = Array.from({ length: MAX_VOLUMES }, () => new Vector4())
const shp = Array.from({ length: MAX_VOLUMES }, () => new Vector4())
const casting = Array.from({ length: MAX_VOLUMES }, () => 0)

export type VolumeUniforms = {
  inv: TSLNode
  sct: TSLNode
  ext: TSLNode
  shp: TSLNode
  count: Uniform<number>
  /** 1 while a lit picture with volumes is drawn. */
  on: Uniform<number>
  /** The volumes that cast shadows on surfaces, by index, and how many. */
  castList: TSLNode
  castCount: Uniform<number>
}

export const V: VolumeUniforms = {
  inv: uniformArray(inv, 'mat4'),
  sct: uniformArray(sct, 'vec4'),
  ext: uniformArray(ext, 'vec4'),
  shp: uniformArray(shp, 'vec4'),
  count: uniform(0, 'int') as Uniform<number>,
  on: uniform(0) as Uniform<number>,
  castList: uniformArray(casting, 'int'),
  castCount: uniform(0, 'int') as Uniform<number>,
}

export type SlotKind = 'directional' | 'spot' | 'point'
/** A light that lights the volumes: its kind, whether its shadow map is sampled, and how many maps that takes. */
export type Slot = { kind: SlotKind; cast: boolean; light: DirectionalLight | SpotLight | PointLight }

/** What a slot's uniforms hold; the march reads them (lights.ts). */
export type SlotUniforms = {
  /** color × intensity, pre-exposed. */
  radiance: Uniform<Color>
  /** A spot's or point's world position. */
  position: Uniform<Vector3>
  /** Toward a directional light; along a spot's axis. */
  direction: Uniform<Vector3>
  /** A spot's cos(angle) and cos(angle · (1 − penumbra)). */
  cone: Uniform<Vector2>
  /** distance (0: none) and decay. */
  falloff: Uniform<Vector2>
  /** A point's shadow camera near and far. */
  range: Uniform<Vector2>
  /** The shadow matrices (one, or a cascade's each) and biases. */
  matrices: Uniform<Matrix4>[]
  biases: Uniform<number>[]
  /** A cascade's share of the view's near..far. */
  cascades: Uniform<Vector2>[]
}

const cascadeCount = 4

export const slotUniforms: SlotUniforms[] = Array.from({ length: MAX_LIGHTS }, () => ({
  radiance: uniform(new Color()) as Uniform<Color>,
  position: uniform(new Vector3()) as Uniform<Vector3>,
  direction: uniform(new Vector3()) as Uniform<Vector3>,
  cone: uniform(new Vector2()) as Uniform<Vector2>,
  falloff: uniform(new Vector2()) as Uniform<Vector2>,
  range: uniform(new Vector2()) as Uniform<Vector2>,
  matrices: Array.from({ length: cascadeCount }, () => uniform(new Matrix4()) as Uniform<Matrix4>),
  biases: Array.from({ length: cascadeCount }, () => uniform(0) as Uniform<number>),
  cascades: Array.from({ length: cascadeCount }, () => uniform(new Vector2()) as Uniform<Vector2>),
}))

/** The camera's near and far for picking a cascade (as CSMShadowNode does), the sky light from the ambient and hemisphere lights, the environment's intensity. */
export const viewRange = uniform(new Vector2(0.1, 1000)) as Uniform<Vector2>
export const ambientRadiance = uniform(new Color()) as Uniform<Color>
export const environmentIntensity = uniform(1) as Uniform<number>

type Cascaded = { lights: { shadow?: { map?: { depthTexture?: Texture | null } | null; matrix: Matrix4; bias: number } }[]; _cascades: Vector2[] }
type Lit = DirectionalLight | SpotLight | PointLight

const cascadesOf = (light: DirectionalLight) => (light.shadow as { shadowNode?: Partial<Cascaded> }).shadowNode as Cascaded | undefined
const mapsOf = (kind: SlotKind) => (kind === 'directional' ? cascadeCount : 1)

/** Whether a light's shadow maps exist to be sampled: three makes them when a material first builds the light's shadow. */
function castsNow(light: Lit): boolean {
  if (!light.castShadow) return false
  if ((light as DirectionalLight).isDirectionalLight) {
    const node = cascadesOf(light as DirectionalLight)
    return node?.lights?.length === cascadeCount && node.lights.every((l) => !!l.shadow?.map?.depthTexture)
  }
  return !!light.shadow.map?.depthTexture
}

/** A light as the selection sees it. */
export type Candidate = { kind: SlotKind; cast: boolean; sun: boolean; intensity: number; position: Vector3; decay: number }

/**
 * Which lights light the volumes, at most MAX_LIGHTS and MAX_MAPS shadow
 * maps: a casting sun or moon first, then by the light they bring to the
 * volumes, a directional light its intensity, a spot or point its intensity
 * at the nearest point of the volumes' bounding spheres (with its decay).
 * Ties keep the scene's order. The rest only light surfaces.
 */
export function selectLights(candidates: readonly Candidate[], bounds: readonly Sphere[]): number[] {
  const estimate = (c: Candidate) => {
    if (c.kind === 'directional') return c.intensity
    let nearest = Infinity
    for (const b of bounds) nearest = Math.min(nearest, Math.max(c.position.distanceTo(b.center) - b.radius, 0))
    return c.intensity / Math.max(Math.pow(nearest, c.decay), 0.01)
  }
  const order = candidates.map((c, i) => ({ i, first: c.sun && c.cast ? 1 : 0, e: estimate(c) }))
  order.sort((a, b) => b.first - a.first || b.e - a.e || a.i - b.i)
  const out: number[] = []
  let maps = 0
  for (const { i } of order) {
    if (out.length === MAX_LIGHTS) break
    const c = candidates[i]
    const need = c.cast ? mapsOf(c.kind) : 0
    if (maps + need > MAX_MAPS) continue
    maps += need
    out.push(i)
  }
  return out
}

/** The march's structure: each slot's kind and whether it casts, and whether there is an environment. */
export const signatureOf = (slots: readonly Pick<Slot, 'kind' | 'cast'>[], environment: boolean) => `${slots.map((s) => `${s.kind}${s.cast ? '+' : ''}`).join(',')}|${environment ? 'env' : ''}`

/** What the march is built for, as prepareVolumes left it. */
export const lighting = { slots: [] as Slot[], environment: null as Texture | null, signature: '' }

/** Which volumes cast on surfaces, as indices into the uniforms (in document order). */
export function castList(volumes: readonly Pick<Volume, 'cast_shadow'>[]): number[] {
  const out: number[] = []
  volumes.forEach((v, i) => v.cast_shadow && out.push(i))
  return out
}

function visibleInScene(o: Object3D): boolean {
  for (let p: Object3D | null = o; p; p = p.parent) if (!p.visible) return false
  return true
}

const sphere = new Sphere()
const bounds: Sphere[] = []
const corner = new Vector3()
const targetAt = new Vector3()
const tint = new Color()

/**
 * Before a picture: the scene's visible volumes (the first MAX_VOLUMES in
 * document order) and the lights that light them, or off when the look
 * draws none (only the lit looks mark the scene, objects/Lighting.tsx) or
 * no volume is visible. After applyExposure, whose intensities it reads.
 */
export function prepareVolumes(scene: Scene, camera: Camera, drawn: boolean) {
  const entries = drawn && scene.userData.volumes === true ? [...volumeObjects(scene).values()].filter((e) => visibleInScene(e.object)) : []
  if (entries.length === 0) {
    V.on.value = 0
    V.count.value = 0
    V.castCount.value = 0
    return
  }
  entries.sort((a, b) => a.rank - b.rank)
  const list = entries.slice(0, MAX_VOLUMES)
  bounds.length = 0
  list.forEach(({ object, volume }, i) => {
    object.updateWorldMatrix(true, false)
    inv[i].copy(object.matrixWorld).invert()
    const c = coefficients(volume)
    sct[i].set(c.scatter[0], c.scatter[1], c.scatter[2], volume.anisotropy)
    ext[i].set(c.extinction[0], c.extinction[1], c.extinction[2], volume.edge_falloff)
    shp[i].set(volume.shape === 'sphere' ? 1 : 0, chordFactor(volume), 0, 0)
    // The unit box's corners bound either shape.
    sphere.center.setFromMatrixPosition(object.matrixWorld)
    sphere.radius = corner.set(0.5, 0.5, 0.5).applyMatrix4(object.matrixWorld).distanceTo(sphere.center)
    for (const [x, y, z] of [[-0.5, 0.5, 0.5], [0.5, -0.5, 0.5], [0.5, 0.5, -0.5]] as const) sphere.radius = Math.max(sphere.radius, corner.set(x, y, z).applyMatrix4(object.matrixWorld).distanceTo(sphere.center))
    bounds.push(sphere.clone())
  })
  V.count.value = list.length
  const casts = castList(list.map((e) => e.volume))
  casts.forEach((v, k) => (casting[k] = v))
  V.castCount.value = casts.length
  V.on.value = 1

  const { near, far } = camera as PerspectiveCamera | OrthographicCamera
  viewRange.value.set(near, far)

  // The lights, in the scene's order.
  const lights: Lit[] = []
  ambientRadiance.value.setRGB(0, 0, 0)
  scene.traverseVisible((o) => {
    const l = o as Light
    if (!l.isLight || l.userData.studio === true) return
    // An even radiance from all round, as the materials take them (unoccluded).
    if ((l as AmbientLight).isAmbientLight) ambientRadiance.value.add(tint.copy(l.color).multiplyScalar(l.intensity / Math.PI))
    else if ((l as HemisphereLight).isHemisphereLight) {
      const h = l as HemisphereLight
      ambientRadiance.value.add(tint.copy(h.color).add(h.groundColor).multiplyScalar(h.intensity / (2 * Math.PI)))
    } else if ((l as DirectionalLight).isDirectionalLight || (l as SpotLight).isSpotLight || (l as PointLight).isPointLight) lights.push(l as Lit)
  })
  const candidates = lights.map((light): Candidate => {
    const kind: SlotKind = (light as DirectionalLight).isDirectionalLight ? 'directional' : (light as SpotLight).isSpotLight ? 'spot' : 'point'
    return { kind, cast: castsNow(light), sun: light.userData.sun === true, intensity: light.intensity, position: light.getWorldPosition(new Vector3()), decay: kind === 'directional' ? 0 : (light as PointLight).decay }
  })
  const slots = selectLights(candidates, bounds).map((i): Slot => ({ kind: candidates[i].kind, cast: candidates[i].cast, light: lights[i] }))
  slots.forEach((slot, k) => writeSlot(slotUniforms[k], slot))
  lighting.slots = slots
  lighting.environment = scene.environment
  environmentIntensity.value = scene.environmentIntensity
  lighting.signature = signatureOf(slots, scene.environment !== null)
}

function writeSlot(u: SlotUniforms, { kind, cast, light }: Slot) {
  u.radiance.value.copy(light.color).multiplyScalar(light.intensity)
  light.getWorldPosition(u.position.value)
  if (kind === 'point') {
    const p = light as PointLight
    u.falloff.value.set(p.distance, p.decay)
    u.range.value.set(p.shadow.camera.near, p.shadow.camera.far)
    if (cast) u.biases[0].value = p.shadow.bias
    return
  }
  const aimed = light as DirectionalLight | SpotLight
  aimed.target.getWorldPosition(targetAt)
  if (kind === 'directional') {
    u.direction.value.subVectors(u.position.value, targetAt).normalize()
    return
  }
  const s = light as SpotLight
  u.direction.value.subVectors(targetAt, u.position.value).normalize()
  u.cone.value.set(Math.cos(s.angle), Math.cos(s.angle * (1 - s.penumbra)))
  u.falloff.value.set(s.distance, s.decay)
  if (cast) u.biases[0].value = s.shadow.bias
}

/**
 * After the scene pass of a picture (VolumeNode.updateBefore): the shadow
 * maps three drew this frame and their matrices, and the environment, into
 * the march's texture nodes. three makes a light's maps as its materials
 * build and frees them when it stops casting, so they are read when drawn.
 */
export function bindVolumeTextures(maps: readonly (readonly Swappable[])[], skies: readonly Swappable[]) {
  lighting.slots.forEach(({ kind, cast, light }, k) => {
    if (!cast) return
    const u = slotUniforms[k]
    if (kind === 'directional') {
      const node = cascadesOf(light as DirectionalLight)!
      node.lights.forEach((l, c) => {
        const shadow = l.shadow!
        const map = shadow.map?.depthTexture
        if (map && maps[k][c]) maps[k][c].value = map
        u.matrices[c].value.copy(shadow.matrix)
        u.biases[c].value = shadow.bias
        u.cascades[c].value.copy(node._cascades[c])
      })
      return
    }
    const map = light.shadow.map?.depthTexture
    if (map && maps[k][0]) maps[k][0].value = map
    u.matrices[0].value.copy(light.shadow.matrix)
  })
  if (lighting.environment) for (const s of skies) s.value = lighting.environment
}

/** A slot's shadow textures as they are now: what a march built for it first binds. */
export function slotMaps({ kind, light }: Slot): Texture[] {
  if (kind === 'directional') return cascadesOf(light as DirectionalLight)!.lights.map((l) => l.shadow!.map!.depthTexture!)
  return [light.shadow.map!.depthTexture!]
}

/** After a picture: off, so a renderer without prepareVolumes draws none. */
export function finishVolumes() {
  V.on.value = 0
  V.castCount.value = 0
}
