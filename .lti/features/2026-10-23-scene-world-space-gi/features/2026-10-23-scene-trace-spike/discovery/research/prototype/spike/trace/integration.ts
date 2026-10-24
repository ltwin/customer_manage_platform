// S0 group 1, integration (scene-trace-spike design「判定标准」组 1): the scene packed by three-mesh-bvh's
// BVHComputeData, with each object's material index in its transform slot; one ray a pixel traced in a TSL
// compute kernel that calls the library's raycastFirstHit (no WGSL of the spike's own); and the hits held
// against the raster's distance, flat normal and object, pixel by pixel.
import { FloatType, Matrix4, Mesh, RenderTarget, Vector3, type Material, type Object3D } from 'three'
import { cameraPosition, cross, dFdx, dFdy, float, Fn, If, instancedArray, instanceIndex, normalize, positionWorld, uint, uniform, vec2, vec4 } from 'three/tsl'
import { MeshBasicNodeMaterial, type WebGPURenderer } from 'three/webgpu'
import { BVHComputeData } from 'three-mesh-bvh/webgpu'
import type { Built } from './scene.ts'
import { traceShim } from './trace.ts'

/** No object: the material index a miss reads. */
const NONE = 0xffffffff

/** The transform slot's first spare word (TransformStruct: matrixWorld 16, inverse 16, visible 1, _alignment0). */
const MATERIAL_WORD = 33

/** BVHComputeData with each object's material index in its transform slot, where upstream leaves room for it. */
export class TracedScene extends BVHComputeData {
  materialOf = new Map<Object3D, number>()

  writeTransformData(info: { object: Object3D }, premultiply: Matrix4, slot: number, target: ArrayBuffer) {
    // @ts-expect-error -- private in the typings, the override upstream documents for this
    super.writeTransformData(info, premultiply, slot, target)
    const words = (this.structs as unknown as { transform: { getLength(): number } }).transform.getLength()
    new Uint32Array(target)[slot * words + MATERIAL_WORD] = this.materialOf.get(info.object) ?? NONE
  }
}

/** The meshes a ray may hit: visible, on layer 0 (the picture's), opaque (the editor's prepass rule). */
export function collect(built: Built): Mesh[] {
  return built.meshes.filter((m) => m.visible && m.layers.isEnabled(0) && !(m.material as Material).transparent)
}

export function pack(built: Built) {
  const meshes = collect(built)
  const started = performance.now()
  const data = new TracedScene(meshes, { attributes: { position: 'vec4f', normal: 'vec4f' } })
  // The material index here is the mesh's own: the picture's raster id below draws the same.
  meshes.forEach((m, i) => data.materialOf.set(m, i))
  data.update()
  return { data, meshes, packMs: performance.now() - started }
}

/** One ray a pixel from the camera: per pixel (distance along the ray or -1, world normal) and the material index. */
export function primaryKernel(data: TracedScene, width: number, height: number) {
  const cameraWorld = uniform(new Matrix4())
  const projectionInverse = uniform(new Matrix4())
  const eye = uniform(new Vector3())
  const hits = instancedArray(width * height, 'vec4')
  const materials = instancedArray(width * height, 'uint')
  const transforms = data.storage as unknown as { transforms: { element(i: unknown): { get(name: string): unknown } } }
  const traceFirst = traceShim(data)
  const kernel = Fn(() => {
    const i = instanceIndex
    const x = i.mod(width)
    const y = i.div(width)
    // Row 0 at the top, as the raster's readback.
    const ndc = vec2(float(x).add(0.5).div(width).mul(2).sub(1), float(1).sub(float(y).add(0.5).div(height).mul(2)))
    const view = projectionInverse.mul(vec4(ndc, 0.5, 1))
    const world = cameraWorld.mul(vec4(view.xyz.div(view.w), 1)).xyz
    const hit = traceFirst(eye, normalize(world.sub(eye))).toVar('primary')
    If(hit.get('didHit'), () => {
      hits.element(i).assign(vec4(hit.get('dist'), hit.get('normal')))
      materials.element(i).assign(transforms.transforms.element(hit.get('objectIndex')).get('_alignment0'))
    }).Else(() => {
      hits.element(i).assign(vec4(-1, 0, 0, 0))
      materials.element(i).assign(uint(NONE))
    })
  })().compute(width * height, [64])
  return {
    kernel,
    hits,
    materials,
    aim(camera: { matrixWorld: Matrix4; projectionMatrixInverse: Matrix4; position: Vector3 }) {
      cameraWorld.value.copy(camera.matrixWorld)
      projectionInverse.value.copy(camera.projectionMatrixInverse)
      eye.value.setFromMatrixPosition(camera.matrixWorld)
    },
  }
}

/** The raster's per pixel (distance from the camera, flat world normal) and mesh index + 1 (0: nothing), drawn with stand-in materials. */
async function rasterReference(renderer: WebGPURenderer, built: Built, meshes: Mesh[], width: number, height: number) {
  const target = new RenderTarget(width, height, { type: FloatType })
  const keep = meshes.map((m) => m.material)
  const background = built.scene.background
  built.scene.background = null
  renderer.setClearColor(0x000000, 0)
  const read = async (material: (m: Mesh, i: number) => Material) => {
    meshes.forEach((m, i) => (m.material = material(m, i)))
    renderer.setRenderTarget(target)
    renderer.clear()
    renderer.render(built.scene, built.camera)
    renderer.setRenderTarget(null)
    return (await renderer.readRenderTargetPixelsAsync(target, 0, 0, width, height)) as Float32Array
  }
  const flat = new MeshBasicNodeMaterial()
  // The normal in rgb, as n / 2 + 1/2 (the scene's materials clamp their output at 0, render/gpu.ts clampMaterialOutput),
  // and the id and distance in rg of a second pass: a material's alpha is its opacity's.
  flat.colorNode = vec4(normalize(cross(dFdx(positionWorld), dFdy(positionWorld))).mul(0.5).add(0.5), 1)
  const normals = await read(() => flat)
  const ids = await read((_, i) => {
    const m = new MeshBasicNodeMaterial()
    m.colorNode = vec4(float(i + 1), positionWorld.sub(cameraPosition).length(), 0, 1)
    return m
  })
  // As the trace's: (distance, normal) per pixel.
  const geometry = new Float32Array(normals.length)
  for (let p = 0; p < width * height; p++) geometry.set([ids[4 * p + 1], 2 * normals[4 * p] - 1, 2 * normals[4 * p + 1] - 1, 2 * normals[4 * p + 2] - 1], 4 * p)
  meshes.forEach((m, i) => (m.material = keep[i]))
  built.scene.background = background
  target.dispose()
  return { geometry, ids }
}

/** Group 1 over a scene: the pack, one traced frame twice, and the hits against the raster. */
export async function runIntegration(renderer: WebGPURenderer, built: Built, width: number, height: number) {
  const { data, meshes, packMs } = pack(built)
  const primary = primaryKernel(data, width, height)
  primary.aim(built.camera)
  const trace = async () => {
    const started = performance.now()
    renderer.compute(primary.kernel)
    const hits = new Float32Array(await renderer.getArrayBufferAsync(primary.hits.value))
    const materials = new Uint32Array(await renderer.getArrayBufferAsync(primary.materials.value))
    return { hits, materials, ms: performance.now() - started }
  }
  const first = await trace()
  const gpuFirstMs = await renderer.resolveTimestampsAsync('compute')
  const second = await trace()
  const gpuSecondMs = await renderer.resolveTimestampsAsync('compute')
  const same = first.hits.every((v, k) => Object.is(v, second.hits[k])) && first.materials.every((v, k) => v === second.materials[k])

  // The kernel's WGSL, for the record (three keeps it on the compute's node builder state).
  const shader = ((renderer as unknown as { _nodes: { getForCompute(n: unknown): { computeShader: string } } })._nodes.getForCompute(primary.kernel)).computeShader
  const center = 4 * (Math.floor(height / 2) * width + Math.floor(width / 2))
  const probe = [...first.hits.slice(center, center + 4)]
  const raster = await rasterReference(renderer, built, meshes, width, height)
  // WebGPU reads a render target back from its top row (R0 environment.md); a flip check guards the assumption.
  const flipped = { geometry: new Float32Array(raster.geometry.length), ids: new Float32Array(raster.ids.length) }
  for (let y = 0; y < height; y++) {
    flipped.geometry.set(raster.geometry.subarray(4 * y * width, 4 * (y + 1) * width), 4 * (height - 1 - y) * width)
    flipped.ids.set(raster.ids.subarray(4 * y * width, 4 * (y + 1) * width), 4 * (height - 1 - y) * width)
  }
  const idAgreement = (r: { ids: Float32Array }) => {
    let same = 0
    for (let p = 0; p < width * height; p++) if (r.ids[4 * p] - 1 === (first.materials[p] === NONE ? -1 : first.materials[p])) same++
    return same / (width * height)
  }
  const rows = { asRead: idAgreement(raster), flipped: idAgreement(flipped) }
  if (rows.flipped > rows.asRead) Object.assign(raster, flipped)
  const at = (x: number, y: number) => y * width + x
  let compared = 0, edges = 0, off = 0, missMismatch = 0, idOff = 0, distOff = 0, normalOff = 0
  const mask = new Uint8ClampedArray(width * height * 4)
  for (let y = 1; y < height - 1; y++) {
    for (let x = 1; x < width - 1; x++) {
      const p = at(x, y)
      const rd = raster.geometry[4 * p]
      const rid = raster.ids[4 * p] - 1
      const td = first.hits[4 * p]
      const tid = first.materials[p]
      // An edge: the raster's object, distance or facet changes beside the pixel (its flat normal comes from screen
      // derivatives, which mix two facets where a 2 × 2 quad straddles them).
      let edge = false
      for (const q of [at(x - 1, y), at(x + 1, y), at(x, y - 1), at(x, y + 1)]) {
        const d = raster.geometry[4 * q]
        const facet = raster.geometry[4 * p + 1] * raster.geometry[4 * q + 1] + raster.geometry[4 * p + 2] * raster.geometry[4 * q + 2] + raster.geometry[4 * p + 3] * raster.geometry[4 * q + 3]
        if (raster.ids[4 * q] - 1 !== rid || Math.abs(d - rd) > 0.02 * Math.max(rd, d, 1e-3) || facet < Math.cos((3 * Math.PI) / 180)) edge = true
      }
      if (edge) {
        edges++
        mask.set([60, 60, 60, 255], 4 * p)
        continue
      }
      compared++
      const rasterHit = rid >= 0
      const traceHit = td >= 0
      let bad = false
      if (rasterHit !== traceHit) {
        missMismatch++
        bad = true
      } else if (rasterHit) {
        if (tid !== rid) {
          idOff++
          bad = true
        }
        if (Math.abs(td - rd) > 0.005 * rd) {
          distOff++
          bad = true
        }
        const dot = Math.abs(raster.geometry[4 * p + 1] * first.hits[4 * p + 1] + raster.geometry[4 * p + 2] * first.hits[4 * p + 2] + raster.geometry[4 * p + 3] * first.hits[4 * p + 3])
        if (dot < Math.cos((3 * Math.PI) / 180)) {
          normalOff++
          bad = true
        }
      }
      if (bad) off++
      mask.set(bad ? [255, 40, 40, 255] : [0, 0, 0, 255], 4 * p)
    }
  }
  data.dispose()
  return {
    meshes: meshes.length,
    triangles: built.triangles,
    packMs: Math.round(packMs),
    traceMs: [Math.round(first.ms), Math.round(second.ms)],
    gpuMs: [gpuFirstMs, gpuSecondMs],
    deterministic: same,
    probe,
    rows,
    shader,
    pixels: { compared, edges, off, offFraction: off / compared, missMismatch, idOff, distOff, normalOff },
    mask: { width, height, rgba: mask },
    normals: first.hits,
  }
}
