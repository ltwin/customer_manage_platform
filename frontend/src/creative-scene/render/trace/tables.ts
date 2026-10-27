import { Color, CubeUVReflectionMapping, Matrix4, Vector3, type DirectionalLight, type Light, type PointLight, type Scene, type SpotLight } from 'three'
import { storage } from 'three/tsl'
import { StorageBufferAttribute } from 'three/webgpu'
import { materialEntry } from '../../materials/catalog.ts'
import { unknownGrey } from '../../materials/instances.ts'
import { patternFns, type TSLNode } from '../../materials/patterns.ts'
import type { SlotMaterial } from '../../materials/resolve.ts'
import { preExposure } from '../../world/exposure.ts'
import type { Lighting } from './collect.ts'

// What a hit point is shaded with, in one storage buffer of vec4s (S1
// design「内部类型」, D6: one buffer, so the fragment binds six): a header,
// then a material row per traced instance, then a light row per light.
// Values as the raster's uniforms have them: colours linear, lights'
// intensities as applyExposure left them.
//
//   header   8 vec4: (lights, first light, materials, 0), (ambient rgb, 0),
//            (environment intensity, 0, 0, 0), then three columns: the
//            matrix a hit turns a world direction by before it samples the
//            environment (environmentLookup)
//   material 8 vec4: (kind, double-sided, vertex colours, 0), then a vec4 a
//            parameter (a colour's rgb, a number's x), in rowParams' order
//   light    4 vec4: (type, casts shadows, distance, decay),
//            (position, or the direction towards it; spot: cos of its cone),
//            (colour × intensity; spot: cos where its penumbra starts),
//            (spot: the direction it shines, 0)

export const HEADER = 8
export const MATERIAL_ROW = 8
export const LIGHT_ROW = 4

/** A row's first vec4: kind 0 the unknown material's grey, 1 principled, 2 + i the preset presets[i]. */
export const UNKNOWN = 0
export const PRINCIPLED = 1
/** The presets, in the order the hit's Switch takes them (hit.ts). */
export const presets: readonly string[] = Object.keys(patternFns)

export const LIGHT = { directional: 0, point: 1, spot: 2 } as const

/** The principled parameters a hit reads (Epic「命中点着色」: no clearcoat, sheen, … at a hit point); the unknown grey uses them too. */
const principledParams = ['color', 'roughness', 'metalness', 'emissive', 'emissive_intensity']

/** Which of a material's parameters its row holds, in order, and whether each is a colour. */
export function rowParams(m: SlotMaterial): { key: string; color: boolean }[] {
  if (m.kind !== 'pattern') return principledParams.map((key) => ({ key, color: key === 'color' || key === 'emissive' }))
  return Object.entries(materialEntry(m.id)?.params ?? {}).map(([key, spec]) => ({ key, color: spec.type === 'color' }))
}

const unknownParams: Record<string, string | number> = { color: unknownGrey, roughness: 0.9, metalness: 0, emissive: '#000000', emissive_intensity: 0 }

/**
 * m as its row holds it: a preset the hit has no function for (presets) is
 * drawn as the raster draws it (materials/instances.ts build), principled
 * with its colour, roughness and metalness, three's defaults where it has none.
 */
function asRow(m: SlotMaterial): SlotMaterial {
  if (m.kind !== 'pattern' || presets.includes(m.id)) return m
  const p = m.params
  return { kind: 'principled', params: { color: p.color ?? '#ffffff', roughness: p.roughness ?? 1, metalness: p.metalness ?? 0, emissive: '#000000', emissive_intensity: 0 } }
}

/** three's storage(), its typings not following a vec4 array. */
const store = storage as unknown as (value: StorageBufferAttribute, type: string) => TSLNode

const color = new Color()
const a = new Vector3()
const b = new Vector3()
const rotation = new Matrix4()

/**
 * What a direction is turned by before three's PMREM node samples the
 * environment along it, for the sample to be the one the scene's materials
 * take (S1-U1). Their node turns it by the material's rotation, which for
 * scene.environment is the transpose of the scene's environmentRotation,
 * after flipping y where the PMREM is one three rendered (PMREMNode.setup,
 * three r186); in the reflections' pass the rotation is the identity (a
 * quad's material has none). So a hit hands it F · Rᵀ · F · d, F the flip.
 * Column-major 3 × 3.
 */
export function environmentLookup(scene: Scene): number[] {
  const m = rotation.makeRotationFromEuler(scene.environmentRotation).transpose().elements
  const env = scene.environment
  const flipped = !env || env.mapping !== CubeUVReflectionMapping || env.isRenderTargetTexture === true
  const f = flipped ? [1, -1, 1] : [1, 1, 1]
  const out: number[] = []
  for (let c = 0; c < 3; c++) for (let r = 0; r < 3; r++) out.push(m[c * 4 + r] * f[r] * f[c])
  return out
}

export class TraceTables {
  /** The vec4 array the hit reads (hit.ts). */
  readonly node: TSLNode
  private attribute: StorageBufferAttribute
  /** Rows there is room for: the lights start after the materials' room. */
  private room = { materials: 0, lights: 0 }
  private readonly release: (attribute: object) => void
  /** The scene's ambient lights added up, exposed, as setLights last wrote them: L_amb, which the world-space composite takes out of the materials' indirect light (S2 design「合成」). */
  readonly ambient = new Color()
  /** setLights' header and light rows unexposed, and the environment's texture: what relit compares with. */
  private lit: number[] = []
  private environment: string | null = null

  /** @param release lets a buffer's GPU copy go (SceneGPU.release) */
  constructor(release: (attribute: object) => void = () => {}) {
    this.release = release
    this.attribute = this.allocate(16, 8)
    this.node = store(this.attribute, 'vec4').toReadOnly().setName('trace_table')
  }

  private get f32(): Float32Array {
    return this.attribute.array as Float32Array
  }

  /** The table as the CPU holds it (the e2e's digests). */
  get data(): Float32Array {
    return this.f32
  }

  private allocate(materials: number, lights: number): StorageBufferAttribute {
    this.room = { materials, lights }
    const a = new StorageBufferAttribute(new Float32Array((HEADER + materials * MATERIAL_ROW + lights * LIGHT_ROW) * 4), 4)
    a.name = 'trace_tables'
    return a
  }

  /** Room for this many material rows and lights; the node keeps its identity and takes the larger buffer (layer.ts). */
  private reserve(materials: number, lights: number) {
    if (materials <= this.room.materials && lights <= this.room.lights) return
    const kept = (HEADER + this.room.materials * MATERIAL_ROW) * 4
    const old = this.f32
    const next = this.allocate(Math.max(this.room.materials, Math.ceil(materials * 1.25)), Math.max(this.room.lights, Math.ceil(lights * 1.25)))
    ;(next.array as Float32Array).set(old.subarray(0, kept))
    // Between frames: the node takes the new buffer before one is drawn.
    this.release(this.attribute)
    this.attribute = next
    this.node.value = next
  }

  private dirty(firstVec4: number, vec4s: number) {
    const a = this.attribute as StorageBufferAttribute & { addUpdateRange(start: number, count: number): void }
    a.addUpdateRange(firstVec4 * 4, vec4s * 4)
    a.needsUpdate = true
  }

  /** Material row r: m as a hit shades it. */
  setMaterial(r: number, slot: SlotMaterial, doubleSided: boolean) {
    const m = asRow(slot)
    this.reserve(r + 1, this.room.lights)
    const f = this.f32
    const at = (HEADER + r * MATERIAL_ROW) * 4
    f.fill(0, at, at + MATERIAL_ROW * 4)
    const kind = m.kind === 'unknown' ? UNKNOWN : m.kind === 'principled' ? PRINCIPLED : 2 + presets.indexOf(m.id)
    f.set([kind, doubleSided ? 1 : 0, m.kind === 'principled' && m.vertexColors ? 1 : 0, 0], at)
    const params = m.kind === 'unknown' ? unknownParams : m.params
    rowParams(m).forEach(({ key, color: isColor }, i) => {
      const o = at + (1 + i) * 4
      if (isColor) f.set(color.set(params[key] as string).toArray(), o)
      else f[o] = Number(params[key])
    })
    this.dirty(HEADER + r * MATERIAL_ROW, MATERIAL_ROW)
  }

  /**
   * The header and the light rows, every frame after applyExposure (S1
   * design「命中点着色」). Whether the light a hit is lit by changed since
   * the last call, unexposed (S2 design「编辑的分类」): the exposure alone
   * moving does not make the probes gather again.
   */
  setLights(lighting: Lighting, scene: Scene): boolean {
    const { lights, ambient } = lighting
    this.reserve(this.room.materials, lights.length)
    const f = this.f32
    const first = HEADER + this.room.materials * MATERIAL_ROW
    const m = environmentLookup(scene)
    f.set([lights.length, first, this.room.materials, 0, ambient.r, ambient.g, ambient.b, 0, scene.environment ? scene.environmentIntensity : 0, 0, 0, 0, m[0], m[1], m[2], 0, m[3], m[4], m[5], 0, m[6], m[7], m[8], 0], 0)
    lights.forEach((l, i) => this.writeLight(first + i * LIGHT_ROW, l))
    this.dirty(0, HEADER)
    if (lights.length > 0) this.dirty(first, lights.length * LIGHT_ROW)
    this.ambient.copy(ambient)
    return this.relit(scene, first, lights.length)
  }

  /**
   * The header and the light rows as last written, the exposure k taken out
   * of what it scales (ambient, environment intensity, the lights' colour ×
   * intensity), against the last call's: a relative 1e-5 apart or more is a
   * change (they come through float32 and a division by k).
   */
  private relit(scene: Scene, first: number, lights: number): boolean {
    const f = this.f32
    const k = preExposure.value
    // The header and the light rows; the rows between are the materials', which restyle counts.
    const now = [...f.subarray(0, HEADER * 4), ...f.subarray(first * 4, (first + lights * LIGHT_ROW) * 4)]
    for (const at of [4, 5, 6, 8]) now[at] /= k
    for (let i = 0; i < lights; i++) for (const c of [8, 9, 10]) now[(HEADER + i * LIGHT_ROW) * 4 + c] /= k
    const environment = scene.environment?.uuid ?? null
    const was = this.lit
    this.lit = now
    const changed = environment !== this.environment || was.length !== now.length || now.some((x, i) => Math.abs(x - was[i]) > 1e-5 * Math.max(Math.abs(x), Math.abs(was[i]), 1e-30))
    this.environment = environment
    return changed
  }

  private writeLight(vec4: number, l: Light) {
    const f = this.f32
    const at = vec4 * 4
    f.fill(0, at, at + LIGHT_ROW * 4)
    color.copy(l.color).multiplyScalar(l.intensity)
    l.getWorldPosition(a)
    if ((l as DirectionalLight).isDirectionalLight) {
      const d = l as DirectionalLight
      d.target.getWorldPosition(b)
      a.sub(b).normalize()
      f.set([LIGHT.directional, l.castShadow ? 1 : 0, 0, 0, a.x, a.y, a.z, 0, color.r, color.g, color.b, 0], at)
    } else if ((l as SpotLight).isSpotLight) {
      const s = l as SpotLight
      s.target.getWorldPosition(b)
      b.sub(a).normalize()
      f.set([LIGHT.spot, l.castShadow ? 1 : 0, s.distance, s.decay, a.x, a.y, a.z, Math.cos(s.angle), color.r, color.g, color.b, Math.cos(s.angle * (1 - s.penumbra)), b.x, b.y, b.z, 0], at)
    } else {
      const p = l as PointLight
      f.set([LIGHT.point, l.castShadow ? 1 : 0, p.distance, p.decay, a.x, a.y, a.z, 0, color.r, color.g, color.b, 0], at)
    }
  }

  dispose() {
    this.release(this.attribute)
  }
}
