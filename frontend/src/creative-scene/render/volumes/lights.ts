import type { CubeTexture, Texture } from 'three'
import * as tsl from 'three/tsl'
import { PMREMNode, type NodeBuilder } from 'three/webgpu'
import type { TSLNode } from '../../materials/patterns.ts'
import { ambientRadiance, environmentIntensity, slotMaps, slotUniforms, type Slot, type SlotUniforms } from './state.ts'

// How the lights light a volume (scene-volumes design「步进」光源): each slot of
// state.ts as three lights a surface, its distance falloff
// (getDistanceAttenuation), a spot's cone (as SpotLightNode) and a
// directional light's cascade picked as CSMShadowNode picks it, times its
// shadow map where it casts; and the sky light, an even radiance from all
// round. Built for one structure of slots; the march rebuilds on another.

type Op = (...args: TSLNode[]) => TSLNode
const { abs, cubeTexture, dot, float, getDistanceAttenuation, length, max, mix, select, smoothstep, step, texture, vec2, vec3, vec4, viewZToOrthographicDepth, viewZToPerspectiveDepth } = tsl as unknown as Record<string, Op>
const If = tsl.If as unknown as (cond: TSLNode, then: () => void) => void

/** A light at a point: the radiance arriving (shadowed), the unit direction toward it, how far it is. */
export type LightAt = { radiance: TSLNode; toward: TSLNode; distance: TSLNode }

type Swappable = TSLNode & { value: Texture }

/** A 2D shadow map at world p: 1 lit, 0 shadowed, 1 outside the map (as three's ShadowNode reads it). */
function shadow2D(map: Swappable, u: SlotUniforms, c: number, p: TSLNode): TSLNode {
  const clip = u.matrices[c].mul(vec4(p, 1))
  const s = clip.xyz.div(clip.w)
  const uv = vec2(s.x, s.y.oneMinus())
  const inside = uv.x.greaterThanEqual(0).and(uv.x.lessThanEqual(1)).and(uv.y.greaterThanEqual(0)).and(uv.y.lessThanEqual(1)).and(s.z.lessThanEqual(1))
  return select(inside, map.sample(uv).compare(s.z.add(u.biases[c])), float(1))
}

function directional(slot: Slot, u: SlotUniforms, maps: Swappable[], p: TSLNode, viewZ: TSLNode): LightAt {
  let visible: TSLNode = float(1)
  if (slot.cast) {
    // The slices are shares of near..far of the view depth, as the surfaces pick them.
    const share = viewZToOrthographicDepth(viewZ, u.range.x, u.range.y)
    const v = float(1).toVar()
    maps.forEach((map, c) => {
      If(share.greaterThanEqual(u.cascades[c].x).and(share.lessThanEqual(u.cascades[c].y)), () => {
        v.assign(shadow2D(map, u, c, p))
      })
    })
    visible = v
  }
  return { radiance: u.radiance.mul(visible), toward: u.direction, distance: float(1e4) }
}

function spot(slot: Slot, u: SlotUniforms, maps: Swappable[], p: TSLNode): LightAt {
  const v = p.sub(u.position)
  const d = length(v)
  const toward = v.negate().div(max(d, 1e-4))
  const cone = smoothstep(u.cone.x, u.cone.y, dot(toward.negate(), u.direction))
  const falloff = getDistanceAttenuation({ lightDistance: d, cutoffDistance: u.falloff.x, decayExponent: u.falloff.y })
  const visible = slot.cast ? shadow2D(maps[0], u, 0, p) : float(1)
  return { radiance: u.radiance.mul(cone).mul(falloff).mul(visible), toward, distance: d }
}

function point(slot: Slot, u: SlotUniforms, maps: Swappable[], p: TSLNode): LightAt {
  const v = p.sub(u.position)
  const d = length(v)
  const toward = v.negate().div(max(d, 1e-4))
  const falloff = getDistanceAttenuation({ lightDistance: d, cutoffDistance: u.falloff.x, decayExponent: u.falloff.y })
  let visible: TSLNode = float(1)
  if (slot.cast) {
    // As PointShadowNode: the cube face's depth is the largest axis of the way from the light.
    const q = abs(v)
    const m = max(max(q.x, q.y), q.z)
    const depth = viewZToPerspectiveDepth(max(m, u.range.x).negate(), u.range.x, u.range.y).add(u.biases[0])
    // Sampled at every step and masked by arithmetic, never in a branch: three makes an if of a select and turns
    // off WGSL's derivative uniformity, and a lane that had not taken the branch held a zero direction, so the
    // quad's derivatives of the cube coordinate came out NaN, and so the light (owner 2026-10-26: the volume
    // white across a casting point light's reach).
    const within = step(u.range.x, m).mul(step(m, u.range.y))
    visible = mix(float(1), maps[0].sample(v.div(max(d, 1e-4))).compare(depth), within)
  }
  return { radiance: u.radiance.mul(falloff).mul(visible), toward, distance: d }
}

/** The six axes the sky light is averaged over. */
const axes = [
  [1, 0, 0],
  [-1, 0, 0],
  [0, 1, 0],
  [0, -1, 0],
  [0, 0, 1],
  [0, 0, -1],
] as const

/**
 * The sky light: the environment's PMREM at roughness 1 averaged over the
 * axes, all six from one node's texture. A PMREMNode an axis shared one
 * binding only once the PMREM was made (three shares a texture by its
 * uuid); before that each held a placeholder of its own, six of the stage's
 * 16, over the limit with the depth and 12 shadow maps.
 */
class SkyLightNode extends PMREMNode {
  constructor(environment: Texture) {
    super(environment, null, float(1) as never)
  }

  setup(builder: NodeBuilder) {
    let sum: TSLNode = vec3(0)
    for (const a of axes) {
      this.uvNode = vec3(...a) as never
      sum = sum.add(super.setup(builder) as unknown as TSLNode)
    }
    return sum.div(axes.length) as never
  }
}

/**
 * The lights of slots as one march builds them. Each slot's texture nodes
 * are made here with the maps as they are now and swapped every frame after
 * (state.ts bindVolumeTextures); environment, the scene's environment
 * then, is the sky light's PMREM, likewise swapped.
 */
export function volumeLights(slots: readonly Slot[], environment: Texture | null) {
  const maps = slots.map((slot) => (slot.cast ? slotMaps(slot).map((map) => (slot.kind === 'point' ? cubeTexture(map as CubeTexture) : texture(map)) as Swappable) : []))
  const skies: Swappable[] = environment ? [new SkyLightNode(environment) as unknown as Swappable] : []
  // Per pixel, not per step: the environment's, and the ambient and hemisphere lights.
  const sky = (): TSLNode => (skies.length ? vec3(skies[0]).mul(environmentIntensity).add(ambientRadiance) : ambientRadiance)
  return {
    /** Each slot's shadow texture nodes, and the sky light's: what bindVolumeTextures swaps. */
    maps,
    skies,
    sky,
    /** Every slot's light at world p, whose view depth is viewZ (negative in front). */
    at(p: TSLNode, viewZ: TSLNode): LightAt[] {
      return slots.map((slot, k) => {
        const u = slotUniforms[k]
        if (slot.kind === 'directional') return directional(slot, u, maps[k], p, viewZ)
        if (slot.kind === 'spot') return spot(slot, u, maps[k], p)
        return point(slot, u, maps[k], p)
      })
    },
  }
}
