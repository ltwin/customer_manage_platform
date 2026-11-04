import { Box2, Box3, LinearFilter, LinearMipmapLinearFilter, NoColorSpace, RepeatWrapping, RGBAFormat, RGFormat, SRGBColorSpace, UnsignedByteType, Vector2, Vector3, type ColorSpace, type Texture } from 'three'
import { texture as textureNode, uv, vec4 } from 'three/tsl'
import { MeshBasicNodeMaterial, QuadMesh, RenderTarget, type WebGPURenderer } from 'three/webgpu'
import { acquireTexture, holdImages, releaseTexture, textureKeys, type ImageHold, type TextureKey } from '../../materials/images.ts'
import { mappingOf, type MapKind } from '../../materials/maps.ts'
import type { TSLNode } from '../../materials/patterns.ts'
import { renderPolicy } from '../../materials/principled.ts'
import type { SlotMaterial } from '../../materials/resolve.ts'

// The textures a traced hit samples (C5c1 design「世界空间层」): WebGPU has
// no bindless, so each device keeps three texture arrays, base colour
// (sRGB), normal (RG, z rebuilt at the hit) and ORM, every image a layer of
// one of them by its TextureKey (the raster's image cache's key: a colour
// key in the colours, and so on), counted, a layer given back used again.
// An image is resampled onto a render target of the layer's size with its
// mipmaps, and each level copied into its layer: the CPU keeps no layer's
// pixels, and one layer coming does not make the whole array's mipmaps
// again. An array grows by 1.25 into a new one, the layers copied over, its
// node taking the new texture: a shader built with it is not built again.
//
// A layer is −1 in its material rows (tables.ts) till it is written: the
// hit uses the slot's constant parameters meanwhile, and an image past the
// device's layers keeps them for good (said in the render tab).

/** A layer's side: the viewport's, and an export's (design「数据」). */
export const LAYER_SIZE = { viewport: 512, export: 1024 } as const

/** How each array stores its texels: what the raster samples, the normal's z left out. */
const formats: Record<MapKind, { format: typeof RGBAFormat | typeof RGFormat; colorSpace: ColorSpace }> = {
  color: { format: RGBAFormat, colorSpace: SRGBColorSpace },
  normal: { format: RGFormat, colorSpace: NoColorSpace },
  orm: { format: RGBAFormat, colorSpace: NoColorSpace },
}

/** An array texture has two layers at least: three takes one for a plain 2D texture, another binding. */
const MIN_LAYERS = 2

/**
 * The layers of one array, by TextureKey: a key asked for again shares its
 * layer, one given back by every asker goes back to be used again, lowest
 * first. room is how many the array has: grown by 1.25 when a layer past it
 * is taken, never past max. take() is null when max layers are in use.
 */
export class LayerSlots {
  private readonly held = new Map<TextureKey, { layer: number; refs: number }>()
  private free: number[] = []
  private next = 0
  room: number
  readonly max: number

  constructor(max: number, room = MIN_LAYERS) {
    this.max = Math.max(max, MIN_LAYERS)
    this.room = Math.min(Math.max(room, MIN_LAYERS), this.max)
  }

  take(key: TextureKey): number | null {
    const h = this.held.get(key)
    if (h) {
      h.refs++
      return h.layer
    }
    const layer = this.free.length > 0 ? this.free.shift()! : this.next < this.max ? this.next++ : null
    if (layer === null) return null
    if (layer >= this.room) this.room = Math.min(this.max, Math.max(layer + 1, Math.ceil(this.room * 1.25)))
    this.held.set(key, { layer, refs: 1 })
    return layer
  }

  give(key: TextureKey) {
    const h = this.held.get(key)
    if (!h || --h.refs > 0) return
    this.held.delete(key)
    this.free.push(h.layer)
    this.free.sort((a, b) => a - b)
  }

  layerOf(key: TextureKey): number | undefined {
    return this.held.get(key)?.layer
  }
}

/** What a material row holds of a slot's textures (tables.ts): 8 floats, two vec4s, and the clip value of its first. */
export type RowMaps = {
  /** The base colour's, normal's and ORM's layers (−1: none, or not written), then the mapping: 0 box projection, 1 UV. */
  layers: [number, number, number, number]
  /** Texture units per plane unit along u and w (box projection: 1/size and aspect/size; UV: the repeat), the rotation in radians, the normal's strength. */
  transform: [number, number, number, number]
  /** clip's cutoff over the opacity, which a hit holds the base colour's alpha to (above 1: cut everywhere); 0 when nothing is cut. */
  clip: number
}

/** A row's textures where it has none: all −1, nothing cut. */
export const NO_MAPS: RowMaps = { layers: [-1, -1, -1, 0], transform: [1, 1, 0, 1], clip: 0 }

/**
 * A slot's textures as its row holds them: each key's layer once written
 * (layerOf, −1 till then), its mapping, and clip's cutoff over its opacity
 * (raster: opacity × alpha ≥ cutoff, instances.ts applyTextures). aspectOf
 * gives a texture's width over height, the box projection's tile height.
 */
export function rowMaps(m: SlotMaterial, layerOf: (key: TextureKey) => number, aspectOf: (key: TextureKey) => number): RowMaps {
  if (m.kind !== 'principled' || !m.texturing) return NO_MAPS
  const t = m.texturing
  const keys = textureKeys(t)
  const policy = renderPolicy(m)
  const opacity = Number(m.params.opacity ?? 1)
  const clip = policy.alphaCutoff === null ? 0 : opacity > 0 ? Math.max(policy.alphaCutoff / opacity, 1e-6) : 2
  const at = (key: TextureKey | undefined) => (key === undefined ? -1 : layerOf(key))
  const mapping = mappingOf(t)
  const first = keys.color ?? keys.normal ?? keys.orm
  const scale = mapping.kind === 'triplanar' ? [1 / mapping.size, (first ? aspectOf(first) : 1) / mapping.size] : mapping.repeat
  return {
    layers: [at(keys.color), at(keys.normal), at(keys.orm), mapping.kind === 'uv' ? 1 : 0],
    transform: [scale[0], scale[1], mapping.rotation, mapping.strength],
    clip,
  }
}

/** A key the arrays hold or wait for. */
type Want = {
  kind: MapKind
  layer: number | null
  texture: Texture
  /** The image held till it is written: its pixels are on the texture. */
  hold: ImageHold | null
  written: boolean
  /** The image failed, or did not fit the image budget: nothing to wait for. */
  failed: boolean
}

const kindOf = (key: TextureKey) => key.slice(0, key.indexOf(':')) as MapKind

export class TraceMaps {
  /** The arrays as the hit samples them (hit.ts): a node's value is swapped for a larger array. */
  readonly nodes: Record<MapKind, TSLNode>
  readonly size: number
  private readonly arrays: Record<MapKind, RenderTarget>
  private readonly slots: Record<MapKind, LayerSlots>
  private readonly wants = new Map<TextureKey, Want>()
  /** Images in, to be written at the next update. */
  private arrived: TextureKey[] = []
  private readonly scratch: Partial<Record<MapKind, RenderTarget>> = {}
  private quad: QuadMesh | null = null
  private readonly resamplers: Partial<Record<MapKind, { material: MeshBasicNodeMaterial; source: TSLNode }>> = {}
  private readonly renderer: () => WebGPURenderer
  private disposed = false
  /** Asks for a sync when an image comes in (TraceBase's redraw). */
  onArrival: (() => void) | null = null

  /**
   * @param renderer the device's, asked for only when a layer is written
   * @param size LAYER_SIZE's
   * @param maxLayers the device's maxTextureArrayLayers
   */
  constructor(renderer: () => WebGPURenderer, size: number, maxLayers: number) {
    this.renderer = renderer
    this.size = size
    const kinds: MapKind[] = ['color', 'normal', 'orm']
    this.slots = Object.fromEntries(kinds.map((k) => [k, new LayerSlots(maxLayers)])) as Record<MapKind, LayerSlots>
    this.arrays = Object.fromEntries(kinds.map((k) => [k, this.array(k, MIN_LAYERS)])) as Record<MapKind, RenderTarget>
    this.nodes = Object.fromEntries(kinds.map((k) => [k, textureNode(this.arrays[k].texture)])) as Record<MapKind, TSLNode>
  }

  private array(kind: MapKind, layers: number): RenderTarget {
    const { format, colorSpace } = formats[kind]
    // Mipmaps allocated at the first upload (initTexture), then generated never: each layer's are copied in.
    const rt = new RenderTarget(this.size, this.size, { depth: layers, format, type: UnsignedByteType, colorSpace, generateMipmaps: true, minFilter: LinearMipmapLinearFilter, magFilter: LinearFilter, depthBuffer: false })
    rt.texture.wrapS = rt.texture.wrapT = RepeatWrapping
    rt.texture.anisotropy = 8
    rt.texture.name = `trace_maps_${kind}`
    return rt
  }

  /** The mip levels of a layer. */
  private get levels(): number {
    return Math.floor(Math.log2(this.size)) + 1
  }

  /** A key's layer once written, else −1. */
  layerOf(key: TextureKey): number {
    const w = this.wants.get(key)
    return w?.written && w.layer !== null ? w.layer : -1
  }

  /** A texture's width over height, as its image showed it (images.ts). */
  aspectOf(key: TextureKey): number {
    return (this.wants.get(key)?.texture.userData.aspect as number | undefined) ?? 1
  }

  /** Keys still waiting for their image to be written. */
  get pending(): number {
    let n = 0
    for (const w of this.wants.values()) if (!w.written && !w.failed && w.layer !== null) n++
    return n
  }

  /** Keys past the device's layers: their slots keep the constant parameters. */
  get over(): TextureKey[] {
    return [...this.wants].filter(([, w]) => w.layer === null).map(([k]) => k)
  }

  /**
   * The keys the traced materials sample, sorted (an export's layers then
   * follow the keys' order alone, design「就绪与确定性」): new ones are
   * asked for, the gone given back; the images come since written. Whether
   * any key's layer changed, its rows to be written again.
   */
  update(keys: readonly TextureKey[]): boolean {
    let changed = false
    const wanted = new Set(keys)
    for (const [key, w] of this.wants) {
      if (wanted.has(key)) continue
      this.wants.delete(key)
      this.slots[w.kind].give(key)
      w.hold?.release()
      releaseTexture(key)
      changed ||= w.written
    }
    for (const key of keys) if (!this.wants.has(key)) this.ask(key)
    const arrived = this.arrived.splice(0)
    for (const key of arrived) {
      const w = this.wants.get(key)
      if (!w || w.written || w.layer === null || !w.hold) continue
      this.write(w)
      w.written = true
      w.hold.release()
      w.hold = null
      changed = true
    }
    return changed
  }

  private ask(key: TextureKey) {
    const kind = kindOf(key)
    const layer = this.slots[kind].take(key)
    const w: Want = { kind, layer, texture: acquireTexture(key), hold: null, written: false, failed: false }
    this.wants.set(key, w)
    if (layer === null) return
    holdImages([key]).then(
      (hold) => {
        if (this.disposed || this.wants.get(key) !== w) return hold.release()
        w.hold = hold
        this.arrived.push(key)
        this.onArrival?.()
      },
      // A failed image, or one past the image budget: the slot keeps its constants, as the raster keeps the placeholder.
      () => {
        w.failed = true
      },
    )
  }

  /**
   * The resample of one kind's image onto its scratch target: its texels as
   * the array stores them (a colour decoded from sRGB as it is sampled and
   * encoded again by the sRGB target). One material a kind, its source node
   * pointed at each image in turn: built once.
   */
  private resampler(kind: MapKind, image: Texture): MeshBasicNodeMaterial {
    let r = this.resamplers[kind]
    if (!r) {
      const source = textureNode(image, uv())
      const material = new MeshBasicNodeMaterial()
      material.fragmentNode = kind === 'normal' ? vec4(source.xy, 0, 1) : source
      material.toneMapped = false
      r = { material, source }
      this.resamplers[kind] = r
    }
    r.source.value = image
    return r.material
  }

  private write(w: Want) {
    const renderer = this.renderer()
    const kind = w.kind
    this.fit(kind, renderer)
    const { format, colorSpace } = formats[kind]
    const scratch = (this.scratch[kind] ??= new RenderTarget(this.size, this.size, { format, type: UnsignedByteType, colorSpace, generateMipmaps: true, minFilter: LinearMipmapLinearFilter, depthBuffer: false }))
    const material = this.resampler(kind, w.texture)
    this.quad ??= new QuadMesh(material)
    this.quad.material = material
    const before = renderer.getRenderTarget()
    renderer.setRenderTarget(scratch)
    this.quad.render(renderer)
    renderer.setRenderTarget(before)
    const array = this.arrays[kind].texture
    for (let level = 0; level < this.levels; level++) {
      const s = Math.max(1, this.size >> level)
      renderer.copyTextureToTexture(scratch.texture, array, new Box2(new Vector2(0, 0), new Vector2(s, s)), new Vector3(0, 0, w.layer!), level, level)
    }
  }

  /** The array of kind with room for its layers: a larger one made and the layers copied over. */
  private fit(kind: MapKind, renderer: WebGPURenderer) {
    const room = this.slots[kind].room
    const old = this.arrays[kind]
    if (!(old.texture.userData.allocated as boolean | undefined)) this.allocate(old, renderer)
    if (old.depth >= room) return
    const next = this.array(kind, room)
    this.allocate(next, renderer)
    for (let level = 0; level < this.levels; level++) {
      const s = Math.max(1, this.size >> level)
      renderer.copyTextureToTexture(old.texture, next.texture, new Box3(new Vector3(0, 0, 0), new Vector3(s, s, old.depth)), new Vector3(0, 0, 0), level, level)
    }
    this.arrays[kind] = next
    this.nodes[kind].value = next.texture
    old.dispose()
  }

  /** The GPU texture made with its mip levels, which the array's copies fill: three would otherwise generate them over every layer at each copy. */
  private allocate(rt: RenderTarget, renderer: WebGPURenderer) {
    renderer.initTexture(rt.texture)
    rt.texture.generateMipmaps = false
    rt.texture.userData.allocated = true
  }

  dispose() {
    this.disposed = true
    for (const [key, w] of this.wants) {
      w.hold?.release()
      releaseTexture(key)
    }
    this.wants.clear()
    for (const rt of Object.values(this.arrays)) rt.dispose()
    for (const rt of Object.values(this.scratch)) rt?.dispose()
    for (const r of Object.values(this.resamplers)) r?.material.dispose()
  }
}
