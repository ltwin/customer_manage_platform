import { Color, DoubleSide, FrontSide, MeshPhysicalMaterial } from 'three'
import type { ParamValue } from '../builtins/catalog.ts'
import { materialEntry } from './catalog.ts'
import { injectPattern, patternsSupported } from './inject.ts'
import { principledProps, seeThrough } from './principled.ts'
import type { SlotMaterial } from './resolve.ts'

// Material instances shared by every mesh that asks for the same thing:
// kind, preset, complete parameters and sidedness. A preset's shader program
// is keyed by its ID, so any number of parameter sets share one program and
// differ only in uniforms. An instance nobody uses is freed a moment later,
// not at once: switching shading back and forth would recompile it.

export const unknownGrey = '#8a8a8a'
const releaseAfterMs = 1000

type Entry = { material: MeshPhysicalMaterial; refs: number; timer: ReturnType<typeof setTimeout> | null }
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

/** Whether meshes drawing m are see-through, for the AO (render/transparent.ts). */
export function isSeeThrough(m: SlotMaterial): boolean {
  return m.kind === 'principled' && seeThrough(m.params)
}

let warned = false

export function makeMaterial(m: SlotMaterial, doubleSided: boolean): MeshPhysicalMaterial {
  const side = doubleSided ? DoubleSide : FrontSide
  if (m.kind === 'unknown') return new MeshPhysicalMaterial({ color: unknownGrey, roughness: 0.9, metalness: 0, side })
  if (m.kind === 'principled') return new MeshPhysicalMaterial({ ...principledProps(m.params), side, vertexColors: !!m.vertexColors })
  const p = m.params
  const entry = materialEntry(m.id)
  const material = new MeshPhysicalMaterial({ color: p.color as string, roughness: p.roughness as number, metalness: p.metalness as number, side })
  if (!entry || !patternsSupported) {
    if (!warned) console.warn('程序化材质无法注入当前 three.js 的着色器，按原理化 BSDF 显示')
    warned = true
    return material
  }
  material.color = new Color('#ffffff')
  const uniforms = Object.fromEntries(
    Object.entries(entry.params).map(([k, spec]) => [`pm_${k}`, { value: spec.type === 'color' ? new Color(p[k] as string) : (p[k] as number) }]),
  )
  material.onBeforeCompile = (shader) => {
    Object.assign(shader.uniforms, uniforms)
    if (!injectPattern(shader, entry)) console.warn(`程序化材质 ${m.id} 注入失败`)
  }
  material.customProgramCacheKey = () => `pattern:${m.id}`
  material.name = m.id
  return material
}

/** The shared instance for key, made from m the first time. */
export function materialFor(key: string, m: SlotMaterial, doubleSided: boolean): MeshPhysicalMaterial {
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
