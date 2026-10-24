// S0 group 3, hit shading (scene-trace-spike design「判定标准」组 3), over the materials scene (every preset):
//   raster   the presets' width-function copies (patterns.ts) with fwidth passed: the fragment WGSL and the pixels
//            against today's materials (I7: the raster draws what it draws today)
//   hits     a compute kernel that traces one ray a pixel and calls the same preset at the hit, from a material table
//            (14 presets inlined behind one switch, and plain principled values), with the width taken two ways:
//              zero  0, leaving the anti-aliasing to frames accumulated with jitter (16 here)
//              diff  the pixel's ray differentials (its neighbours' rays met on the hit's tangent plane): each quantity
//                    the preset passes to width evaluated again one pixel over in x and in y, |dx| + |dy|, as fwidth
//            and the pixel's scale (footprint) from the same differentials; then lit at the hit as three's physical
//            model lights it (direct sun with its shadow map, the hemisphere light, the environment's PMREM)
//   compare  per mesh, against the raster on the same pixels: the material's outputs, and the lit colour without
//            bump (the lighting) and with it (what leaving bump out at hits costs)
//   compile  the kernel's WGSL generation and pipeline, cold (?cold=1) and warm
import { BasicShadowMap, Box3, Color, DirectionalLight, FloatType, HemisphereLight, Matrix4, Mesh, RenderTarget, Vector2, Vector3, type Texture } from 'three'
import { RoomEnvironment } from 'three/addons/environments/RoomEnvironment.js'
import * as tsl from 'three/tsl'
import { MeshBasicNodeMaterial, MeshPhysicalNodeMaterial, PMREMGenerator, StorageBufferAttribute, type WebGPURenderer } from 'three/webgpu'
import { materialEntry } from '../../src/creative-scene/materials/catalog.ts'
import type { SlotMaterial } from '../../src/creative-scene/materials/resolve.ts'
import { preExposure } from '../../src/creative-scene/world/exposure.ts'
import './computePatches.ts'
import { TracedScene } from './integration.ts'
import { bumpNormal, patternFns, type PatternInput, type PatternOut, type TSLNode } from './patterns.ts'
import type { Built } from './scene.ts'
import { traceShim } from './trace.ts'

type Op = (...args: TSLNode[]) => TSLNode
const T = tsl as unknown as Record<string, Op> & { Fn: typeof tsl.Fn; If: typeof tsl.If; Switch: (v: TSLNode) => TSLNode }
const { abs, clamp, dot, float, fwidth, length, max, mix, normalize, pow, reflect, select, uint, uniform, vec2, vec3, vec4, varying, texture } = T

/** The presets in the table's order; one past the last is a plain principled material. */
export const PRESETS = Object.keys(patternFns)
const PLAIN = PRESETS.length
/** vec4s a table row: a header, then a preset's parameters one each (five at most), or a principled's values. */
export const ROW = 8

const round = (v: number, d = 2) => Math.round(v * 10 ** d) / 10 ** d

// ---------------------------------------------------------------------------------------------------------------
// The raster's material, rebuilt on the copies: src/creative-scene/materials/instances.ts build(), line for line.

const patPos: TSLNode = varying(tsl.positionLocal.mul(tsl.modelScale), 'vPatPos')
const patNormal: TSLNode = varying(tsl.normalLocal.div(tsl.modelScale), 'vPatNormal')
const footprint: TSLNode = max(length(fwidth(patPos)), float(1e-6))

function uniforms(id: string, p: Record<string, unknown>) {
  const entry = materialEntry(id)!
  return Object.fromEntries(Object.entries(entry.params).map(([k, spec]) => [k, spec.type === 'color' ? uniform(new Color(p[k] as string)) : uniform(p[k] as number)]))
}

function copyMaterial(m: SlotMaterial & { kind: 'pattern' }, original: MeshPhysicalNodeMaterial) {
  const p = m.params
  const material = new MeshPhysicalNodeMaterial({ color: p.color as string, roughness: p.roughness as number, metalness: p.metalness as number, side: original.side })
  const o = patternFns[m.id]({ width: fwidth, pos: patPos, normal: patNormal, footprint, pm: uniforms(m.id, p) })
  material.colorNode = o.albedo
  material.roughnessNode = o.rough
  material.metalnessNode = o.metal
  material.normalNode = bumpNormal(tsl.positionView, tsl.normalView, o.height)
  material.emissiveNode = o.emission.mul(preExposure)
  material.name = m.id
  material.shadowSide = original.shadowSide
  return material
}

/** The material's outputs as the raster gives them, unlit: (albedo) and (roughness, metalness, emission's luminance). */
function outputsMaterial(m: SlotMaterial, pass: 0 | 1) {
  const material = new MeshBasicNodeMaterial()
  let o: { albedo: TSLNode; rough: TSLNode; metal: TSLNode; emission: TSLNode }
  if (m.kind === 'pattern') o = patternFns[m.id]({ width: fwidth, pos: patPos, normal: patNormal, footprint, pm: uniforms(m.id, m.params) })
  else {
    const p = m.kind === 'principled' ? m.params : { color: '#8a8a8a', roughness: 0.9, metalness: 0 }
    const c = new Color(p.color as string)
    const e = new Color((p.emissive as string) ?? '#000000').multiplyScalar((p.emissive_intensity as number) ?? 0)
    o = { albedo: vec3(c.r, c.g, c.b), rough: float(p.roughness as number), metal: float(p.metalness as number), emission: vec3(e.r, e.g, e.b) }
  }
  material.colorNode = pass === 0 ? vec4(o.albedo, 1) : vec4(o.rough, o.metal, tsl.luminance(o.emission), 1)
  return material
}

// ---------------------------------------------------------------------------------------------------------------
// The material table and the hit kernel.

export function tableRow(m: SlotMaterial): number[] {
  const row = new Array(ROW * 4).fill(0)
  const put = (k: number, v: number[]) => row.splice(4 * k, v.length, ...v)
  if (m.kind === 'pattern' && PRESETS.includes(m.id)) {
    put(0, [PRESETS.indexOf(m.id)])
    Object.entries(materialEntry(m.id)!.params).forEach(([k, spec], j) => {
      if (spec.type === 'color') {
        const c = new Color(m.params[k] as string)
        put(1 + j, [c.r, c.g, c.b])
      } else put(1 + j, [m.params[k] as number])
    })
  } else {
    const p = m.kind === 'principled' ? m.params : { color: '#8a8a8a', roughness: 0.9, metalness: 0 }
    const c = new Color(p.color as string)
    const e = new Color((p.emissive as string) ?? '#000000').multiplyScalar((p.emissive_intensity as number) ?? 0)
    put(0, [PLAIN])
    put(1, [c.r, c.g, c.b])
    put(2, [p.roughness as number, p.metalness as number])
    put(3, [e.r, e.g, e.b])
  }
  return row
}

export type Width = 'zero' | 'diff'
export type Lights = { sun: DirectionalLight; hemi: HemisphereLight; env: Texture; depth: Texture | null }

/** A preset at a hit, with the width taken one way or the other (see the file's head). */
export function atHit(fn: (i: PatternInput) => PatternOut, base: Omit<PatternInput, 'width'>, dpx: TSLNode, dpy: TSLNode, width: Width): PatternOut {
  if (width === 'zero') return fn({ ...base, width: () => float(0) })
  // The same preset one pixel over: the quantities it passes to width, in the order it passes them.
  const record = (list: TSLNode[]) => (x: TSLNode) => (list.push(x), float(0))
  const X: TSLNode[] = [], Y: TSLNode[] = []
  fn({ ...base, pos: base.pos.add(dpx), width: record(X) })
  fn({ ...base, pos: base.pos.add(dpy), width: record(Y) })
  let k = 0
  return fn({
    ...base,
    width: (x) => {
      const i = k++
      return abs(X[i].sub(x)).add(abs(Y[i].sub(x)))
    },
  })
}

/** three's physical model (PhysicalLightingModel, MeshPhysicalNodeMaterial at its defaults: ior 1.5, specular 1) at a hit, in world space. */
export function lit(P: TSLNode, n: TSLNode, v: TSLNode, m: { albedo: TSLNode; rough: TSLNode; metal: TSLNode; emission: TSLNode }, L: ReturnType<typeof lightUniforms>, sunShadow?: () => TSLNode) {
  const specularColor = vec3(0.04)
  const specularF90 = float(1)
  const diffuseContribution = m.albedo.mul(float(1).sub(m.metal))
  const specularColorBlended = mix(specularColor, m.albedo, m.metal)
  // getRoughness without its geometry term (screen derivatives of the normal: none at a hit).
  const roughness = max(m.rough, 0.0525).toVar()
  const dotNV = clamp(dot(n, v), 0, 1)
  const dfg = tsl.DFGLUT({ roughness, dotNV } as never) as TSLNode
  const Ess = dfg.x.add(dfg.y)
  const msComp = specularColorBlended.mul(Ess.reciprocal().sub(1)).add(1)
  const multiscatter = (f0: TSLNode) => {
    const FssEss = f0.mul(dfg.x).add(specularF90.mul(dfg.y))
    const Ems = float(1).sub(Ess)
    const Favg = f0.add(float(1).sub(f0).mul(0.047619))
    const Fms = FssEss.mul(Favg).div(float(1).sub(Ems.mul(Favg)))
    return [FssEss, Fms.mul(Ems)]
  }
  // The sun, through its shadow map as three's BasicShadowFilter reads it (outside the map: lit), or as the caller
  // finds it (a shadow ray: group 4).
  const l = L.sunDir
  const mapShadow = () => {
    const coord = L.shadowMatrix.mul(vec4(P.add(n.mul(L.normalBias)), 1)).toVar()
    const sc = coord.xyz.div(coord.w).toVar()
    const uv = vec2(sc.x, float(1).sub(sc.y))
    const inside = uv.x.greaterThanEqual(0).and(uv.x.lessThanEqual(1)).and(uv.y.greaterThanEqual(0)).and(uv.y.lessThanEqual(1)).and(sc.z.lessThanEqual(1))
    return select(inside, texture(L.depth, uv).compare(sc.z.add(L.bias)), float(1))
  }
  const shadow = sunShadow ? sunShadow() : mapShadow()
  const irradiance = clamp(dot(n, l), 0, 1).mul(L.sunColor).mul(shadow)
  const h = normalize(l.add(v))
  const F = tsl.F_Schlick({ f0: specularColor, f90: specularF90, dotVH: clamp(dot(v, h), 0, 1) } as never) as TSLNode
  const lambert = (c: TSLNode) => tsl.BRDF_Lambert({ diffuseColor: c } as never) as TSLNode
  const directDiffuse = irradiance.mul(lambert(diffuseContribution)).mul(float(1).sub(F))
  const directSpecular = irradiance.mul(tsl.BRDF_GGX({ lightDirection: l, f0: specularColorBlended, f90: 1, roughness, normalView: n, viewDirection: v } as never) as TSLNode).mul(msComp)
  // The hemisphere light: indirect irradiance, less what the specular lobe keeps.
  const [ssD, msD] = multiscatter(specularColor)
  const [ssM, msM] = multiscatter(m.albedo)
  const hemi = mix(L.ground, L.sky, dot(n, L.hemiDir).mul(0.5).add(0.5))
  const hemiDiffuse = hemi.mul(lambert(diffuseContribution)).mul(float(1).sub(ssD.add(msD)))
  // The environment (EnvironmentNode): radiance along the reflection bent toward the normal as roughness grows, and
  // irradiance at its roughest level.
  const r = normalize(mix(reflect(v.negate(), n), n, pow(roughness, 4)))
  const radiance = tsl.pmremTexture(L.env as never, r as never, roughness as never) as TSLNode
  const irradianceEnv = tsl.pmremTexture(L.env as never, n as never, float(1) as never) as TSLNode
  const single = mix(ssD, ssM, m.metal)
  const multi = mix(msD, msM, m.metal)
  const envSpecular = radiance.mul(single).add(multi.mul(irradianceEnv))
  const envDiffuse = diffuseContribution.mul(float(1).sub(ssD.add(msD))).mul(irradianceEnv)
  return directDiffuse.add(directSpecular).add(hemiDiffuse).add(envSpecular).add(envDiffuse).add(m.emission)
}

export function lightUniforms(L: Lights) {
  const sunColor = L.sun.color.clone().multiplyScalar(L.sun.intensity)
  return {
    sunDir: uniform(L.sun.position.clone().sub(L.sun.target.position).normalize()),
    sunColor: uniform(new Vector3(sunColor.r, sunColor.g, sunColor.b)),
    shadowMatrix: uniform(L.sun.shadow.matrix),
    bias: uniform(L.sun.shadow.bias),
    normalBias: uniform(L.sun.shadow.normalBias),
    depth: L.depth,
    sky: uniform(new Vector3(L.hemi.color.r, L.hemi.color.g, L.hemi.color.b).multiplyScalar(L.hemi.intensity)),
    ground: uniform(new Vector3(L.hemi.groundColor.r, L.hemi.groundColor.g, L.hemi.groundColor.b).multiplyScalar(L.hemi.intensity)),
    hemiDir: uniform(L.hemi.position.clone().normalize()),
    env: L.env,
  }
}

/** The material at a hit, from the table: the 14 presets behind one Switch, a plain principled material otherwise. */
export function materialAt(table: TSLNode, mi: TSLNode, posIn: TSLNode, patNIn: TSLNode, fpIn: TSLNode, dpxIn: TSLNode, dpyIn: TSLNode, width: Width) {
  // Every input a variable assigned here, before the Switch: an expression the cases share is otherwise made a
  // variable where TSL first builds it, inside the first case's branch, and the other cases read it unassigned.
  const pos = posIn.toVar()
  const patN = patNIn.toVar()
  const fp = fpIn.toVar()
  const dpx = dpxIn.toVar()
  const dpy = dpyIn.toVar()
  const row = (k: number) => table.element(mi.mul(ROW).add(k))
  const kind = row(0).x.toUint()
  const albedo = vec3(0).toVar()
  const rough = float(0).toVar()
  const metal = float(0).toVar()
  const emission = vec3(0).toVar()
  const sw = T.Switch(kind)
  PRESETS.forEach((id, k) => {
    sw.Case(uint(k), () => {
      const pm = Object.fromEntries(Object.entries(materialEntry(id)!.params).map(([name, spec], j) => [name, spec.type === 'color' ? row(1 + j).xyz : row(1 + j).x]))
      const o = atHit(patternFns[id], { pos, normal: patN, footprint: fp, pm }, dpx, dpy, width)
      albedo.assign(o.albedo)
      rough.assign(o.rough)
      metal.assign(o.metal)
      emission.assign(o.emission)
    })
  })
  sw.Default(() => {
    albedo.assign(row(1).xyz)
    rough.assign(row(2).x)
    metal.assign(row(2).y)
    emission.assign(row(3).xyz)
  })
  return { albedo, rough, metal, emission }
}

function hitKernel(data: TracedScene, table: TSLNode, width: Width, w: number, h: number, L: Lights, salt: number) {
  const traceFirst = traceShim(data)
  const cameraWorld = uniform(new Matrix4())
  const projectionInverse = uniform(new Matrix4())
  const eye = uniform(new Vector3())
  const jitter = uniform(new Vector2(0.5, 0.5))
  const lights = lightUniforms(L)
  const pixels = w * h
  const albedoOut = tsl.instancedArray(pixels, 'vec4')
  const propsOut = tsl.instancedArray(pixels, 'vec4')
  const litOut = tsl.instancedArray(pixels, 'vec4')
  const store = data.storage as unknown as { transforms: TSLNode; attributes: TSLNode }
  const kernel = T.Fn(() => {
    const i = tsl.instanceIndex
    const x = float(i.mod(w))
    const y = float(i.div(w))
    const ray = (px: TSLNode, py: TSLNode) => {
      const ndc = vec2(px.div(w).mul(2).sub(1), float(1).sub(py.div(h).mul(2)))
      const view = projectionInverse.mul(vec4(ndc, 0.5, 1))
      return normalize(cameraWorld.mul(vec4(view.xyz.div(view.w), 1)).xyz.sub(eye))
    }
    const px = x.add(jitter.x)
    const py = y.add(jitter.y)
    const d = ray(px, py).toVar()
    // The pixel's ray differentials: its neighbours' rays, one pixel over in x and in y.
    const dx = ray(px.add(1), py).toVar()
    const dy = ray(px, py.add(1)).toVar()
    const hit = traceFirst(eye, d).toVar('hit') as unknown as { get(k: string): TSLNode }
    albedoOut.element(i).assign(vec4(0))
    propsOut.element(i).assign(vec4(0))
    litOut.element(i).assign(vec4(0))
    T.If(hit.get('didHit'), () => {
      const slot = store.transforms.element(hit.get('objectIndex'))
      // The library's struct types its matrices 'mat4x4f', a name TSL does not know: as mat4 they convert as matrices.
      const inv = T.mat4(slot.get('inverseMatrixWorld')).toVar()
      const M = T.mat4(slot.get('matrixWorld')).toVar()
      const mi = slot.get('_alignment0').toVar()
      const P = eye.add(d.mul(hit.get('dist'))).toVar()
      const nG = vec3(hit.get('normal')).toVar()
      // Where a neighbour's ray meets the hit's tangent plane, less the hit: the surface a pixel spans.
      const onPlane = (r: TSLNode) => eye.add(r.mul(dot(P.sub(eye), nG).div(dot(r, nG)))).sub(P)
      // The columns' lengths (a column read as M[i] loses its swizzle in r186's WGSL: transform the axes instead).
      const scale = vec3(length(M.mul(vec4(1, 0, 0, 0)).xyz), length(M.mul(vec4(0, 1, 0, 0)).xyz), length(M.mul(vec4(0, 0, 1, 0)).xyz)).toVar()
      // The object-space position in true metres (instances.ts patPos) and the differentials in the same space.
      const toPattern = (v: TSLNode, wc: number) => inv.mul(vec4(v, wc)).xyz.mul(scale)
      const pos = toPattern(P, 1).toVar()
      const dpx = toPattern(onPlane(dx), 0).toVar()
      const dpy = toPattern(onPlane(dy), 0).toVar()
      // As fwidth(patPos): |dx| + |dy| per component, its length.
      const fp = max(length(abs(dpx).add(abs(dpy))), 1e-6).toVar()
      const idx = hit.get('indices')
      const b = hit.get('barycoord')
      const vn = (k: TSLNode) => vec3(store.attributes.element(k).get('normal').xyz)
      const nObj = normalize(vn(idx.x).mul(b.x).add(vn(idx.y).mul(b.y)).add(vn(idx.z).mul(b.z))).toVar()
      // The inverse transpose of the world matrix: v * M is transpose(M) * v.
      const nRaw = normalize(vec4(nObj, 0).mul(inv).xyz).toVar()
      // A select, not an If: in r186 a Switch right after an If in the same block chains its first Case onto that If's
      // else (StackNode.Switch keeps the If's pending condition), and the materials would run only for back faces.
      const n = select(dot(nRaw, d).greaterThan(0), nRaw.negate(), nRaw).toVar()
      const { albedo, rough, metal, emission } = materialAt(table, mi, pos, nObj.div(scale), fp, dpx, dpy, width)
      albedoOut.element(i).assign(vec4(albedo, float(mi).add(1)))
      propsOut.element(i).assign(vec4(rough, metal, tsl.luminance(emission), 1))
      // salt: a constant that differs every load when ?cold=1, so no cached pipeline matches (webgpu-perf-warm-shader-cache).
      litOut.element(i).assign(vec4(lit(P, n, d.negate(), { albedo, rough, metal, emission }, lights).add(salt * 1e-12), 1))
    })
  })().compute(pixels, [64])
  return {
    kernel,
    albedoOut,
    propsOut,
    litOut,
    jitter,
    aim(camera: Built['camera']) {
      cameraWorld.value.copy(camera.matrixWorld)
      projectionInverse.value.copy(camera.projectionMatrixInverse)
      eye.value.setFromMatrixPosition(camera.matrixWorld)
    },
  }
}

// ---------------------------------------------------------------------------------------------------------------
// The case.

type Device = { queue: { onSubmittedWorkDone(): Promise<void> } }

const srgb = (v: number) => 255 * Math.min(1, Math.max(0, v <= 0.0031308 ? 12.92 * v : 1.055 * Math.pow(v, 1 / 2.4) - 0.055))

/** Per mesh, over pixels inside it (its four neighbours the same mesh in the raster, and the trace on it too): the mean of |a - b| in 8-bit levels, and the share over 2 levels. */
function compare(ids: Float32Array, traced: Float32Array, a: Float32Array, b: Float32Array, w: number, h: number, encode: (v: number) => number, channels: number[]) {
  const per = new Map<number, { n: number; sum: number; over: number }>()
  for (let y = 1; y < h - 1; y++) {
    for (let x = 1; x < w - 1; x++) {
      const p = y * w + x
      const id = ids[4 * p]
      if (id <= 0) continue
      if ([p - 1, p + 1, p - w, p + w].some((q) => ids[4 * q] !== id) || traced[4 * p + 3] !== id) continue
      let worst = 0, sum = 0
      for (const c of channels) {
        const dv = Math.abs(encode(a[4 * p + c]) - encode(b[4 * p + c]))
        worst = Math.max(worst, dv)
        sum += dv
      }
      const e = per.get(id) ?? { n: 0, sum: 0, over: 0 }
      e.n++
      e.sum += sum / channels.length
      if (worst > 2) e.over++
      per.set(id, e)
    }
  }
  return per
}

function png(w: number, h: number, rgba: Uint8ClampedArray) {
  const c = document.createElement('canvas')
  c.width = w
  c.height = h
  c.getContext('2d')!.putImageData(new ImageData(rgba, w, h), 0, 0)
  return c.toDataURL('image/png')
}

export async function runShading(renderer: WebGPURenderer, built: Built, salt: number) {
  const device = (renderer.backend as unknown as { device: Device }).device
  const w = 1536, h = 1024
  const result: Record<string, unknown> = { salt }
  const { scene, camera } = built
  // See-through meshes are not traced (Epic: they let rays through); hidden here, from the raster too.
  const seeThrough = (m: Mesh) => {
    const s = m.userData.slot as SlotMaterial | undefined
    return s?.kind === 'principled' && (((s.params.transmission as number) ?? 0) > 0 || ((s.params.opacity as number) ?? 1) < 1)
  }
  built.meshes.filter(seeThrough).forEach((m) => (m.visible = false))
  const meshes = built.meshes.filter((m) => m.visible)

  // The lights: the sun casting a shadow map (Basic, unfiltered, as the hits read it), the environment a PMREM.
  const sun = scene.children.find((o): o is DirectionalLight => o instanceof DirectionalLight)!
  const hemi = scene.children.find((o): o is HemisphereLight => o instanceof HemisphereLight)!
  renderer.shadowMap.enabled = true
  renderer.shadowMap.type = BasicShadowMap
  const box = new Box3()
  meshes.forEach((m) => {
    m.castShadow = true
    m.receiveShadow = true
    box.expandByObject(m)
  })
  sun.castShadow = true
  Object.assign(sun.shadow.camera, { left: -6, right: 6, top: 6, bottom: -6, near: 1, far: 150 })
  sun.shadow.camera.updateProjectionMatrix()
  sun.shadow.mapSize.set(2048, 2048)
  sun.shadow.bias = -0.0005
  sun.shadow.normalBias = 0.02
  const pmrem = new PMREMGenerator(renderer)
  const envTarget = pmrem.fromScene(new RoomEnvironment(), 0.04)
  scene.environment = envTarget.texture
  renderer.render(scene, camera)

  const target = new RenderTarget(w, h, { type: FloatType })
  const background = scene.background
  const rasterRead = async () => {
    scene.background = null
    renderer.setClearColor(0x000000, 0)
    renderer.setRenderTarget(target)
    renderer.clear()
    renderer.render(scene, camera)
    renderer.setRenderTarget(null)
    scene.background = background
    return new Float32Array((await renderer.readRenderTargetPixelsAsync(target, 0, 0, w, h)) as Float32Array)
  }
  const products = meshes.map((m) => m.material as MeshPhysicalNodeMaterial)
  const restore = () => meshes.forEach((m, i) => (m.material = products[i]))

  // 1. The raster on the copies, fwidth passed: the same WGSL, the same pixels.
  const litBump = await rasterRead()
  const sameWGSL: Record<string, boolean> = {}
  const copies = meshes.map((m, i) => {
    const s = m.userData.slot as SlotMaterial
    return s.kind === 'pattern' ? copyMaterial(s as SlotMaterial & { kind: 'pattern' }, products[i]) : products[i]
  })
  const debug = (renderer as unknown as { debug: { getShaderAsync(s: unknown, c: unknown, o: unknown): Promise<{ fragmentShader: string; vertexShader: string }> } }).debug
  for (let i = 0; i < meshes.length; i++) {
    const s = meshes[i].userData.slot as SlotMaterial
    if (s.kind !== 'pattern') continue
    const before = await debug.getShaderAsync(scene, camera, meshes[i])
    meshes[i].material = copies[i]
    const after = await debug.getShaderAsync(scene, camera, meshes[i])
    meshes[i].material = products[i]
    sameWGSL[s.id] = before.fragmentShader === after.fragmentShader && before.vertexShader === after.vertexShader
    if (!sameWGSL[s.id]) {
      // The product's own material asked again: whether three gives the same text twice for it.
      const again = await debug.getShaderAsync(scene, camera, meshes[i])
      ;(result.productTwice ??= {} as Record<string, boolean>)[s.id] = again.fragmentShader === before.fragmentShader
      ;(result.copyVsProductAgain ??= {} as Record<string, boolean>)[s.id] = again.fragmentShader === after.fragmentShader
    }
    if (!sameWGSL[s.id] && new URLSearchParams(location.search).get('dump') === '1') (result.wgslDiff ??= {} as Record<string, unknown>)[s.id] = { before: before.fragmentShader, after: after.fragmentShader }
  }
  meshes.forEach((m, i) => (m.material = copies[i]))
  const litCopy = await rasterRead()
  restore()
  let differing = 0, maxDiff = 0
  for (let k = 0; k < litBump.length; k++) {
    const dv = Math.abs(litBump[k] - litCopy[k])
    if (dv > 0) differing++
    maxDiff = Math.max(maxDiff, dv)
  }
  result.raster = { sameWGSL, pixels: { values: litBump.length, differing, maxDiff } }

  // 2. The raster's references: lit without bump, the material's outputs, and which mesh is where.
  const bumps = products.map((p) => p.normalNode)
  products.forEach((p) => {
    p.normalNode = null
    p.needsUpdate = true
  })
  const litFlat = await rasterRead()
  products.forEach((p, i) => {
    p.normalNode = bumps[i]
    p.needsUpdate = true
  })
  meshes.forEach((m) => (m.material = outputsMaterial(m.userData.slot as SlotMaterial, 0)))
  const outAlbedo = await rasterRead()
  meshes.forEach((m) => (m.material = outputsMaterial(m.userData.slot as SlotMaterial, 1)))
  const outProps = await rasterRead()
  meshes.forEach((m, i) => {
    const material = new MeshBasicNodeMaterial()
    material.colorNode = vec4(float(i + 1), 0, 0, 1)
    m.material = material
  })
  const ids = await rasterRead()
  restore()

  // 3. The hits.
  const data = new TracedScene(meshes, { attributes: { position: 'vec4f', normal: 'vec4f' } })
  meshes.forEach((m, i) => data.materialOf.set(m, i))
  data.update()
  const rows = meshes.flatMap((m) => tableRow(m.userData.slot as SlotMaterial))
  const table = tsl.storage(new StorageBufferAttribute(new Float32Array(rows), 4), 'vec4', meshes.length * ROW).toReadOnly()
  const L: Lights = { sun, hemi, env: envTarget.texture, depth: (sun.shadow.map as unknown as { depthTexture: Texture }).depthTexture }
  const nodes = (renderer as unknown as { _nodes: { getForCompute(k: unknown): { computeShader: string } } })._nodes
  const compile: Record<string, unknown> = {}
  const hits: Record<string, { albedo: Float32Array; props: Float32Array; lit: Float32Array }> = {}
  const kernels: Record<string, ReturnType<typeof hitKernel>> = {}
  for (const width of ['diff', 'zero'] as Width[]) {
    const k = hitKernel(data, table, width, w, h, L, salt)
    k.aim(camera)
    let t = performance.now()
    const wgsl = nodes.getForCompute(k.kernel).computeShader
    const wgslMs = performance.now() - t
    t = performance.now()
    renderer.compute(k.kernel)
    await device.queue.onSubmittedWorkDone()
    const pipelineMs = performance.now() - t
    t = performance.now()
    renderer.compute(k.kernel)
    await device.queue.onSubmittedWorkDone()
    if (new URLSearchParams(location.search).get('dump') === '1') (result.wgsl ??= {} as Record<string, string>)[width] = wgsl
    compile[width] = { wgslMs: Math.round(wgslMs), pipelineAndFirstRunMs: Math.round(pipelineMs), secondRunMs: round(performance.now() - t), wgslChars: wgsl.length, wgslKiB: round(wgsl.length / 1024, 1) }
    const read = async (a: TSLNode) => new Float32Array(await renderer.getArrayBufferAsync(a.value))
    hits[width] = { albedo: await read(k.albedoOut), props: await read(k.propsOut), lit: await read(k.litOut) }
    kernels[width] = k
  }
  result.compile = compile
  // Width 0 with the anti-aliasing left to accumulation: 16 frames, jittered over the pixel (R2 sequence).
  {
    const k = kernels.zero
    const sum = { albedo: new Float32Array(w * h * 4), lit: new Float32Array(w * h * 4) }
    const frames = 16
    for (let f = 0; f < frames; f++) {
      k.jitter.value.set((f + 0.5) / frames, (((f * 0.7548776662) % 1) + 1) % 1)
      renderer.compute(k.kernel)
      const a = new Float32Array(await renderer.getArrayBufferAsync(k.albedoOut.value))
      const l = new Float32Array(await renderer.getArrayBufferAsync(k.litOut.value))
      for (let j = 0; j < a.length; j++) {
        sum.albedo[j] += a[j] / frames
        sum.lit[j] += l[j] / frames
      }
    }
    // The mesh index of the centre ray, for the comparison's mask.
    for (let p = 0; p < w * h; p++) sum.albedo[4 * p + 3] = hits.zero.albedo[4 * p + 3]
    hits.zeroAccumulated = { albedo: sum.albedo, props: hits.zero.props, lit: sum.lit }
  }

  // 4. Per mesh, against the raster.
  const names = meshes.map((m) => {
    const s = m.userData.slot as SlotMaterial
    return s.kind === 'pattern' ? s.id : s.kind === 'principled' ? 'principled@1' : 'unknown'
  })
  // One exposure for both lit pictures, so 8-bit levels are not clipped: the raster's 99th percentile luminance at 0.8.
  const lums: number[] = []
  for (let p = 0; p < w * h; p += 7) if (ids[4 * p] > 0) lums.push(0.2126 * litFlat[4 * p] + 0.7152 * litFlat[4 * p + 1] + 0.0722 * litFlat[4 * p + 2])
  lums.sort((a, b) => a - b)
  const exposure = 0.8 / lums[Math.floor(lums.length * 0.99)]
  result.exposure = round(exposure, 4)
  const litEncode = (v: number) => srgb(v * exposure)
  const table2: Record<string, Record<string, unknown>> = {}
  const add = (label: string, per: Map<number, { n: number; sum: number; over: number }>) => {
    for (const [id, e] of per) {
      const name = `${names[id - 1]}#${id - 1}`
      table2[name] = { ...(table2[name] ?? {}), [label]: { pixels: e.n, mean: round(e.sum / e.n), over2: round(e.over / e.n, 4) } }
    }
  }
  for (const [label, hv] of Object.entries(hits)) {
    add(`albedo/${label}`, compare(ids, hv.albedo, outAlbedo, hv.albedo, w, h, srgb, [0, 1, 2]))
    add(`roughMetal/${label}`, compare(ids, hv.albedo, outProps, hv.props, w, h, (v) => 255 * v, [0, 1]))
    add(`litFlat/${label}`, compare(ids, hv.albedo, litFlat, hv.lit, w, h, litEncode, [0, 1, 2]))
    add(`litBump/${label}`, compare(ids, hv.albedo, litBump, hv.lit, w, h, litEncode, [0, 1, 2]))
  }
  result.compare = table2

  // Pictures: the raster without bump, the hits (diff), and |difference| × 8.
  const picture = (f: Float32Array) => {
    const out = new Uint8ClampedArray(w * h * 4)
    for (let p = 0; p < w * h; p++) out.set([litEncode(f[4 * p]), litEncode(f[4 * p + 1]), litEncode(f[4 * p + 2]), 255], 4 * p)
    return out
  }
  const rasterPicture = picture(litFlat)
  const hitPicture = picture(hits.diff.lit)
  const diffPicture = new Uint8ClampedArray(w * h * 4)
  for (let p = 0; p < w * h; p++) for (let c = 0; c < 3; c++) diffPicture[4 * p + c] = 8 * Math.abs(rasterPicture[4 * p + c] - hitPicture[4 * p + c])
  for (let p = 0; p < w * h; p++) diffPicture[4 * p + 3] = 255
  result.pictures = { rasterFlat: png(w, h, rasterPicture), hitsDiff: png(w, h, hitPicture), difference8x: png(w, h, diffPicture), hitsZero: png(w, h, picture(hits.zero.lit)), hitsZeroAccumulated: png(w, h, picture(hits.zeroAccumulated.lit)), rasterBump: png(w, h, picture(litBump)) }
  result.meshes = names
  target.dispose()
  data.dispose()
  return result
}
