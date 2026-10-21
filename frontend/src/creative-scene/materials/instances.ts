import { Color, DoubleSide, FrontSide } from 'three'
import { float, fwidth, length, max, modelScale, normalLocal, normalView, positionLocal, positionView, uniform, varying } from 'three/tsl'
import { MeshPhysicalNodeMaterial } from 'three/webgpu'
import type { ParamValue } from '../builtins/catalog.ts'
import { materialEntry } from './catalog.ts'
import { bumpNormal, patternFns, type TSLNode } from './patterns.ts'
import { principledProps, seeThrough } from './principled.ts'
import type { SlotMaterial } from './resolve.ts'

// Material instances shared by every mesh that asks for the same thing:
// kind, preset, complete parameters and sidedness. A preset's node graph is
// the same whatever its parameters, which are uniforms, so any number of
// parameter sets share one program. An instance nobody uses is freed a
// moment later, not at once: switching shading back and forth would
// recompile it.

export const unknownGrey = '#8a8a8a'
const releaseAfterMs = 1000

type Entry = { material: MeshPhysicalNodeMaterial; refs: number; timer: ReturnType<typeof setTimeout> | null }
const instances = new Map<string, Entry>()

function sorted(params: Record<string, ParamValue>): Record<string, ParamValue> {
  return Object.fromEntries(Object.keys(params).sort().map((k) => [k, params[k]]))
}

export function keyOf(m: SlotMaterial, doubleSided: boolean): string {
  if (m.kind === 'unknown') return JSON.stringify({ kind: 'unknown', doubleSided })
  const id = m.kind === 'pattern' ? m.id : 'principled'
  const vertexColors = m.kind === 'principled' && !!m.vertexColors
  return JSON.stringify({ kind: m.kind, id, params: sorted(m.params), doubleSided, vertexColors })
}

/**
 * Whether meshes drawing m are see-through. three's render lists put them
 * with the transparent ones (transmission too), which the AO's prepass
 * leaves out.
 */
export function isSeeThrough(m: SlotMaterial): boolean {
  return m.kind === 'principled' && seeThrough(m.params)
}

// The object-space position in true metres, so a scaled object does not
// stretch its pattern, and the normal for the projection weights.
const patPos: TSLNode = varying(positionLocal.mul(modelScale), 'vPatPos')
const patNormal: TSLNode = varying(normalLocal.div(modelScale), 'vPatNormal')
const footprint: TSLNode = max(length(fwidth(patPos)), float(1e-6))

export function makeMaterial(m: SlotMaterial, doubleSided: boolean): MeshPhysicalNodeMaterial {
  const side = doubleSided ? DoubleSide : FrontSide
  if (m.kind === 'unknown') return new MeshPhysicalNodeMaterial({ color: unknownGrey, roughness: 0.9, metalness: 0, side })
  if (m.kind === 'principled') return new MeshPhysicalNodeMaterial({ ...principledProps(m.params), side, vertexColors: !!m.vertexColors })
  const p = m.params
  const entry = materialEntry(m.id)
  const fn = patternFns[m.id]
  const material = new MeshPhysicalNodeMaterial({ color: p.color as string, roughness: p.roughness as number, metalness: p.metalness as number, side })
  if (!entry || !fn) return material
  const pm = Object.fromEntries(Object.entries(entry.params).map(([k, spec]) => [k, spec.type === 'color' ? uniform(new Color(p[k] as string)) : uniform(p[k] as number)]))
  const o = fn({ pos: patPos, normal: patNormal, footprint, pm })
  material.colorNode = o.albedo
  material.roughnessNode = o.rough
  material.metalnessNode = o.metal
  material.normalNode = bumpNormal(positionView, normalView, o.height)
  material.emissiveNode = o.emission
  material.name = m.id
  return material
}

/** The shared instance for key, made from m the first time. */
export function materialFor(key: string, m: SlotMaterial, doubleSided: boolean): MeshPhysicalNodeMaterial {
  let e = instances.get(key)
  if (!e) {
    e = { material: makeMaterial(m, doubleSided), refs: 0, timer: null }
    instances.set(key, e)
    // One made for a render that never mounted is freed like an unused one.
    schedule(key, e)
  }
  return e.material
}

export function retain(key: string) {
  const e = instances.get(key)
  if (!e) return
  e.refs++
  if (e.timer !== null) clearTimeout(e.timer)
  e.timer = null
}

export function release(key: string) {
  const e = instances.get(key)
  if (!e) return
  e.refs = Math.max(0, e.refs - 1)
  if (e.refs === 0) schedule(key, e)
}

function schedule(key: string, e: Entry) {
  if (e.timer !== null) clearTimeout(e.timer)
  e.timer = setTimeout(() => {
    e.timer = null
    if (e.refs > 0 || instances.get(key) !== e) return
    instances.delete(key)
    e.material.dispose()
  }, releaseAfterMs)
}

/** How many instances are alive, for tests. */
export const instanceCount = () => instances.size
