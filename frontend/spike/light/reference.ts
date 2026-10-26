// S0 group 1: the reference. Direct light from lights with sizes, sampled on their surfaces and traced to the
// surface through the world-space layer (GI-S1's base), averaged over many frames; and the raster's direct light
// under the candidate shadows, drawn with the same camera, to hold against it (design「参考解」「测量与判定」).
//
// The surfaces come from the raster (a G-buffer of world position, normal, albedo and roughness/metalness), so the
// two pictures agree pixel for pixel on what is seen; only the light reaching each surface differs.
//
//   sphere (point, spot)   a light of radius r: radiance I / (π r²), area-sampled; r = 0 is the point light
//   disc (directional)     angular diameter θ: radiance E / (π sin²(θ/2)), sampled in its cone; θ = 0 is the sun
//   rectangle, ellipse     an area light of luminance L facing its −Z, area-sampled
//
// Each light's contribution is f(ω) · L · cos θs · (cos θl / d² · area, or the cone's solid angle), the visibility
// one shadow ray. The shadowed and unshadowed sums go to their own targets; their ratio is the shadow the raster's is
// held against.
import { Color, DoubleSide, FloatType, NearestFilter, Vector3, AdditiveBlending, type AmbientLight, type DirectionalLight, type Light, type Material, type Object3D, type PerspectiveCamera, type PointLight, type RectAreaLight, type Scene, type SpotLight } from 'three'
import * as tsl from 'three/tsl'
import { MeshBasicNodeMaterial, QuadMesh, RenderTarget, RendererUtils, type WebGPURenderer } from 'three/webgpu'
import { materialEntry } from '../../src/creative-scene/materials/catalog.ts'
import { slotOf, unknownGrey } from '../../src/creative-scene/materials/instances.ts'
import { patternFns, type TSLNode } from '../../src/creative-scene/materials/patterns.ts'
import type { SlotMaterial } from '../../src/creative-scene/materials/resolve.ts'
import { gpuOf } from '../../src/creative-scene/render/gpu.ts'
import type { TraceLayer } from '../../src/creative-scene/render/trace/layer.ts'
import { collectMeshes, type Traced } from '../../src/creative-scene/render/trace/collect.ts'
import { traceWithin } from '../../src/creative-scene/render/trace/wgsl.ts'

type Op = (...args: TSLNode[]) => TSLNode
const T = tsl as unknown as Record<string, Op>
const { BRDF_GGX, BRDF_Lambert, F_Schlick, Fn, If, abs, clamp, cos, cross, dot, float, fwidth, hash, length, max, mix, normalize, select, sin, smoothstep, sqrt, texture, uniform, varying, vec2, vec3, vec4 } = T
const { faceDirection, modelScale, normalLocal, normalWorld, positionLocal, positionWorld, screenCoordinate, screenUV } = tsl as unknown as Record<string, TSLNode>

const CHECK_LAYER = 28

export type Size = { width: number; height: number }

/** A light as the reference takes it, in world space; size is r (m), θ (rad) or the area's width and height. */
export type RefLight =
  | { kind: 'sphere'; position: Vector3; radiance: Color; intensity: Color; radius: number; spot: { axis: Vector3; cosOuter: number; cosInner: number } | null; cutoff: number }
  | { kind: 'disc'; toward: Vector3; irradiance: Color; angle: number }
  | { kind: 'rectangle' | 'ellipse'; position: Vector3; x: Vector3; y: Vector3; emits: Vector3; width: number; height: number; luminance: Color }

/** The lights the raster draws (their intensities as this frame has them, the exposure on them), with sizes: r metres, the sun sunSize degrees across. */
export function refLights(scene: Scene, size: number, shape: 'rectangle' | 'ellipse' = 'rectangle', sunSize = size): RefLight[] {
  const out: RefLight[] = []
  scene.updateMatrixWorld()
  scene.traverseVisible((o) => {
    const l = o as Light
    if (!l.isLight || l.intensity <= 0) return
    const color = l.color.clone().multiplyScalar(l.intensity)
    const at = l.getWorldPosition(new Vector3())
    if ((l as PointLight).isPointLight || (l as SpotLight).isSpotLight) {
      const s = l as SpotLight
      const spot = s.isSpotLight
        ? { axis: s.target.getWorldPosition(new Vector3()).sub(at).normalize(), cosOuter: Math.cos(s.angle), cosInner: Math.cos(s.angle * (1 - s.penumbra)) }
        : null
      const radius = size
      out.push({ kind: 'sphere', position: at, intensity: color, radiance: radius > 0 ? color.clone().multiplyScalar(1 / (Math.PI * radius * radius)) : color, radius, spot, cutoff: (l as PointLight).distance })
    } else if ((l as DirectionalLight).isDirectionalLight) {
      const d = l as DirectionalLight
      const toward = at.clone().sub(d.target.getWorldPosition(new Vector3())).normalize()
      out.push({ kind: 'disc', toward, irradiance: color, angle: (sunSize * Math.PI) / 180 })
    } else if ((l as RectAreaLight).isRectAreaLight) {
      const r = l as RectAreaLight
      const e = r.matrixWorld.elements
      const x = new Vector3(e[0], e[1], e[2]).normalize()
      const y = new Vector3(e[4], e[5], e[6]).normalize()
      const emits = new Vector3(e[8], e[9], e[10]).normalize().negate()
      out.push({ kind: shape, position: at, x, y, emits, width: r.width, height: r.height, luminance: color })
    }
  })
  return out
}

const floatTarget = (s: Size) => {
  const rt = new RenderTarget(s.width, s.height, { type: FloatType, depthBuffer: true })
  rt.texture.minFilter = rt.texture.magFilter = NearestFilter
  return rt
}

/** draw with the camera seeing the traced meshes and the lights alone, on no background and without fog. */
function isolated(renderer: WebGPURenderer, scene: Scene, camera: PerspectiveCamera, traced: readonly Traced[], draw: () => void) {
  const lights: Object3D[] = []
  scene.traverse((o) => (o as Light).isLight && lights.push(o))
  const shown = [...traced.map((t) => t.mesh), ...lights]
  const state = RendererUtils.resetRendererState(renderer, undefined as never)
  const mask = camera.layers.mask
  const { background, fog } = scene
  for (const o of shown) o.layers.enable(CHECK_LAYER)
  try {
    camera.layers.set(CHECK_LAYER)
    scene.background = null
    scene.fog = null
    renderer.setClearColor(0x000000, 0)
    draw()
  } finally {
    for (const o of shown) o.layers.disable(CHECK_LAYER)
    camera.layers.mask = mask
    scene.background = background
    scene.fog = fog
    RendererUtils.restoreRendererState(renderer, state)
  }
}

function swapMaterials(traced: readonly Traced[], make: (m: Material) => Material): () => void {
  const made = new Map<Material, Material>()
  const of = (m: Material) => {
    let x = made.get(m)
    if (!x) made.set(m, (x = make(m)))
    return x
  }
  const before = traced.map((t) => t.mesh.material)
  for (const t of traced) t.mesh.material = Array.isArray(t.mesh.material) ? t.mesh.material.map(of) : of(t.mesh.material)
  return () => {
    traced.forEach((t, i) => (t.mesh.material = before[i]))
    for (const m of made.values()) m.dispose()
  }
}

// The raster's material outputs, unlit (render/trace/checks.ts outputsMaterial).
const patPos: TSLNode = varying(positionLocal.mul(modelScale), 'vRefPatPos')
const patNormal: TSLNode = varying(normalLocal.div(modelScale), 'vRefPatNormal')
const footprint: TSLNode = max(length(fwidth(patPos)), float(1e-6))

function outputsMaterial(m: SlotMaterial, what: 'albedo' | 'outputs'): MeshBasicNodeMaterial {
  const material = new MeshBasicNodeMaterial({ side: DoubleSide, fog: false })
  material.toneMapped = false
  let o: { albedo: TSLNode; rough: TSLNode; metal: TSLNode }
  if (m.kind === 'pattern' && patternFns[m.id]) {
    const pm = Object.fromEntries(Object.entries(materialEntry(m.id)?.params ?? {}).map(([k, spec]) => [k, spec.type === 'color' ? uniform(new Color(m.params[k] as string)) : uniform(m.params[k] as number)]))
    o = patternFns[m.id]({ pos: patPos, width: fwidth, normal: patNormal, footprint, pm })
  } else {
    const p: Record<string, unknown> = m.kind === 'principled' ? m.params : { color: unknownGrey, roughness: 0.9, metalness: 0 }
    const c = new Color(p.color as string)
    o = { albedo: vec3(c.r, c.g, c.b), rough: float(p.roughness as number), metal: float(p.metalness as number) }
    material.vertexColors = what === 'albedo' && m.kind === 'principled' && !!m.vertexColors
  }
  material.colorNode = what === 'albedo' ? vec4(o.albedo, 1) : vec4(o.rough, o.metal, 0, 1)
  return material
}

/** Material outputs read back with negatives cut to 0 (lesson tsl-gpu-readback-measure): positions offset, normals packed. */
const POSITION_OFFSET = 1024

function geometryMaterial(what: 'position' | 'normal'): MeshBasicNodeMaterial {
  const m = new MeshBasicNodeMaterial({ side: DoubleSide, fog: false })
  m.toneMapped = false
  m.colorNode = what === 'position' ? vec4(positionWorld.add(POSITION_OFFSET), 1) : vec4(normalize(normalWorld).mul(faceDirection).mul(0.5).add(0.5), 1)
  return m
}

/** R2's low-discrepancy pair for frame n, shifted per pixel and per light (Cranley–Patterson). */
function randomPair(frame: TSLNode, salt: number): TSLNode {
  const seed = screenCoordinate.x.floor().add(screenCoordinate.y.floor().mul(4096)).add(float(salt * 16777216 + 1))
  const shift = vec2(hash(seed), hash(seed.add(7919)))
  return vec2(frame.mul(0.7548776662466927), frame.mul(0.5698402909980532)).add(shift).fract()
}

/** An orthonormal pair across a unit axis. */
function across(a: TSLNode): [TSLNode, TSLNode] {
  const up = select(abs(a.z).lessThan(0.9), vec3(0, 0, 1), vec3(1, 0, 0))
  const t = normalize(cross(up, a)).toVar()
  return [t, cross(a, t).toVar()]
}

const v3 = (v: Vector3) => vec3(v.x, v.y, v.z)
const c3 = (c: Color) => vec3(c.r, c.g, c.b)

/** The reference pass's material: one sample a light at each pixel, shadowed or not. */
function referenceMaterial(layer: TraceLayer, lights: readonly RefLight[], g: Record<'position' | 'normal' | 'albedo' | 'outputs', RenderTarget>, eye: Vector3, frame: TSLNode, shadowed: boolean, debug: string | null = null): MeshBasicNodeMaterial {
  const m = new MeshBasicNodeMaterial({ fog: false, transparent: true, blending: AdditiveBlending, depthTest: false, depthWrite: false })
  m.toneMapped = false
  const eyeU = uniform(eye.clone())
  m.outputNode = Fn(() => {
    const out = vec3(0).toVar()
    // A render target's rows run the other way from the quad's screenUV.
    const uv = vec2(screenUV.x, float(1).sub(screenUV.y)).toVar()
    const P4 = texture(g.position.texture, uv).toVar()
    If(P4.w.greaterThan(0.5), () => {
      const P = P4.xyz.sub(POSITION_OFFSET).toVar()
      const N = normalize(texture(g.normal.texture, uv).xyz.mul(2).sub(1)).toVar()
      const albedo = texture(g.albedo.texture, uv).xyz.toVar()
      const ro = texture(g.outputs.texture, uv).toVar()
      const roughness = clamp(ro.x, 0.0525, 1).toVar()
      const metal = ro.y.toVar()
      const V = normalize(eyeU.sub(P)).toVar()
      const diffuseColor = albedo.mul(float(1).sub(metal)).toVar()
      const f0 = mix(vec3(0.04), albedo, metal).toVar()
      const brdf = (L: TSLNode) => {
        const h = normalize(L.add(V))
        const F = F_Schlick({ f0: vec3(0.04), f90: float(1), dotVH: clamp(dot(V, h), 0, 1) } as never)
        return BRDF_Lambert({ diffuseColor } as never)
          .mul(float(1).sub(F))
          .add(BRDF_GGX({ lightDirection: L, f0, f90: 1, roughness, normalView: N, viewDirection: V } as never))
      }
      const lifted = P.add(N.mul(2e-3)).toVar()
      const visible = (dir: TSLNode, reach: TSLNode) => {
        if (!shadowed) return float(1)
        const hit = traceWithin(layer, lifted, dir, reach).toVar() as TSLNode & { get(k: string): TSLNode }
        return select(hit.get('didHit'), float(0), float(1))
      }
      lights.forEach((light, i) => {
        const u = randomPair(frame, i + 1).toVar()
        if (light.kind === 'sphere') {
          const c = v3(light.position)
          const r = light.radius
          // A point on the sphere, uniformly by area (r = 0: the centre).
          const z = float(1).sub(u.x.mul(2))
          const ring = sqrt(max(float(0), float(1).sub(z.mul(z))))
          const phi = u.y.mul(Math.PI * 2)
          const nl = vec3(ring.mul(cos(phi)), ring.mul(sin(phi)), z).toVar()
          const q = r > 0 ? c.add(nl.mul(r)) : c
          const toward = q.sub(P).toVar()
          const d = length(toward).toVar()
          const L = toward.div(d).toVar()
          const cosS = max(dot(N, L), 0)
          const geometry = r > 0 ? max(dot(nl, L.negate()), 0).div(d.mul(d)).mul(4 * Math.PI * r * r) : float(1).div(max(d.mul(d), 0.01))
          let shine: TSLNode = r > 0 ? c3(light.radiance) : c3(light.intensity)
          if (light.spot) {
            const along = dot(normalize(P.sub(c)), v3(light.spot.axis))
            const cone = light.spot.cosInner > light.spot.cosOuter + 1e-6 ? smoothstep(float(light.spot.cosOuter), float(light.spot.cosInner), along) : select(along.greaterThan(light.spot.cosOuter), float(1), float(0))
            shine = shine.mul(cone)
          }
          if (light.cutoff > 0) {
            const dc = length(c.sub(P))
            shine = shine.mul(clamp(float(1).sub(dc.div(light.cutoff).pow(4)), 0, 1).pow(2))
          }
          if (debug === 'normal') out.assign(N)
          else if (debug === 'position') out.assign(P)
          else if (debug === 'cos') out.assign(vec3(cosS, geometry, d))
          else if (debug === 'shine') out.assign(shine)
          else out.addAssign(brdf(L).mul(shine).mul(cosS).mul(geometry).mul(visible(L, d.sub(4e-3))))
        } else if (light.kind === 'disc') {
          const a = v3(light.toward)
          const half = light.angle / 2
          let L: TSLNode = a
          let shine: TSLNode = c3(light.irradiance)
          if (half > 0) {
            // A direction in the cone, uniformly by solid angle; radiance × solid angle = E · Ω / (π sin²(θ/2)).
            const [t, b] = across(a)
            const cosMax = Math.cos(half)
            const ct = float(1).sub(u.x.mul(1 - cosMax))
            const st = sqrt(max(float(0), float(1).sub(ct.mul(ct))))
            const phi = u.y.mul(Math.PI * 2)
            L = normalize(a.mul(ct).add(t.mul(st.mul(cos(phi)))).add(b.mul(st.mul(sin(phi))))).toVar()
            const omega = 2 * Math.PI * (1 - cosMax)
            shine = shine.mul(omega / (Math.PI * Math.sin(half) ** 2))
          }
          out.addAssign(brdf(L).mul(shine).mul(max(dot(N, L), 0)).mul(visible(L, float(0))))
        } else {
          // A point on the area light, uniformly by area.
          let local: TSLNode
          let area: number
          if (light.kind === 'rectangle') {
            local = u.sub(0.5).mul(vec2(light.width, light.height))
            area = light.width * light.height
          } else {
            const rr = sqrt(u.x)
            const phi = u.y.mul(Math.PI * 2)
            local = vec2(rr.mul(cos(phi)), rr.mul(sin(phi))).mul(vec2(light.width / 2, light.height / 2))
            area = (Math.PI * light.width * light.height) / 4
          }
          const q = v3(light.position).add(v3(light.x).mul(local.x)).add(v3(light.y).mul(local.y))
          const toward = q.sub(P).toVar()
          const d = length(toward).toVar()
          const L = toward.div(d).toVar()
          const cosL = max(dot(v3(light.emits), L.negate()), 0)
          const geometry = cosL.div(d.mul(d)).mul(area)
          out.addAssign(brdf(L).mul(c3(light.luminance)).mul(max(dot(N, L), 0)).mul(geometry).mul(visible(L, d.sub(4e-3))))
        }
      })
    })
    return vec4(out, 1)
  })()
  return m
}

export type Comparison = {
  size: Size
  frames: number
  /** World position xyz of each pixel's surface (w 1, 0 where none), rows as below. */
  position: Float32Array
  /** Linear RGBA, rows bottom up as three reads them: the reference's sums ÷ frames, and the raster's mean. */
  reference: { shadowed: Float32Array; unshadowed: Float32Array }
  raster: { shadowed: Float32Array; unshadowed: Float32Array; first: Float32Array }
  referenceMs: number
  rasterMs: number
  /** With converge: the reference's shadowed sum at refFrames and at twice as many, each ÷ its frames. */
  converge?: { at: Float32Array; twice: Float32Array }
}

async function readBack(renderer: WebGPURenderer, rt: RenderTarget, s: Size, scale = 1): Promise<Float32Array> {
  const a = (await renderer.readRenderTargetPixelsAsync(rt, 0, 0, s.width, s.height)) as Float32Array
  if (scale !== 1) for (let i = 0; i < a.length; i++) a[i] *= scale
  return a
}

/**
 * The reference (refFrames samples a light a pixel) and the raster's direct light (rasterFrames frames averaged,
 * and its first alone), both with and without shadows, at size, through camera.
 */
export async function compare(renderer: WebGPURenderer, scene: Scene, camera: PerspectiveCamera, layer: TraceLayer, lights: readonly RefLight[], size: Size, refFrames: number, rasterFrames: number, converge = false): Promise<Comparison> {
  const g = gpuOf(renderer)
  const traced = collectMeshes(scene, slotOf)
  const targets = {
    position: floatTarget(size),
    normal: floatTarget(size),
    albedo: floatTarget(size),
    outputs: floatTarget(size),
  }
  const sums = { shadowed: floatTarget(size), unshadowed: floatTarget(size) }
  const raster = { shadowed: floatTarget(size), unshadowed: floatTarget(size), first: floatTarget(size), frame: floatTarget(size) }
  const frameU = uniform(0)
  const eye = new Vector3().setFromMatrixPosition(camera.matrixWorld)
  const passes = {
    shadowed: new QuadMesh(referenceMaterial(layer, lights, targets, eye, frameU, true)),
    unshadowed: new QuadMesh(referenceMaterial(layer, lights, targets, eye, frameU, false, new URLSearchParams(location.search).get('refDebug'))),
  }
  const meanMaterial = new MeshBasicNodeMaterial({ fog: false, transparent: true, blending: AdditiveBlending, depthTest: false, depthWrite: false })
  meanMaterial.toneMapped = false
  meanMaterial.outputNode = texture(raster.frame.texture, vec2(screenUV.x, float(1).sub(screenUV.y)))
  const add = new QuadMesh(meanMaterial)
  const atN = floatTarget(size)
  const copyMaterial = new MeshBasicNodeMaterial({ fog: false, depthTest: false, depthWrite: false })
  copyMaterial.toneMapped = false
  // The sums are drawn by quads, the same way up as the copy's screenUV.
  copyMaterial.outputNode = texture(sums.shadowed.texture, screenUV)
  const copy = new QuadMesh(copyMaterial)

  let referenceMs = 0
  let rasterMs = 0
  isolated(renderer, scene, camera, traced, () => {
    // Sums add up frame after frame: nothing clears a target but the clear() calls below.
    renderer.autoClear = false
    for (const what of ['position', 'normal'] as const) {
      const back = swapMaterials(traced, () => geometryMaterial(what))
      try {
        renderer.setRenderTarget(targets[what])
        renderer.clear()
        renderer.render(scene, camera)
      } finally {
        back()
      }
    }
    for (const what of ['albedo', 'outputs'] as const) {
      const back = swapMaterials(traced, (m) => outputsMaterial(slotOf(m)!, what))
      try {
        renderer.setRenderTarget(targets[what])
        renderer.clear()
        renderer.render(scene, camera)
      } finally {
        back()
      }
    }
    // The reference: frames of one sample a light a pixel, summed.
    const started = performance.now()
    for (const k of ['shadowed', 'unshadowed'] as const) {
      renderer.setRenderTarget(sums[k])
      renderer.clear()
    }
    for (let f = 0; f < refFrames; f++) {
      frameU.value = f
      for (const k of ['shadowed', 'unshadowed'] as const) {
        renderer.setRenderTarget(sums[k])
        passes[k].render(renderer)
      }
    }
    referenceMs = performance.now() - started
    if (converge) {
      // The sum at refFrames copied aside, then as many frames more on top of it.
      renderer.setRenderTarget(atN)
      renderer.clear()
      copy.render(renderer)
      for (let f = refFrames; f < refFrames * 2; f++) {
        frameU.value = f
        renderer.setRenderTarget(sums.shadowed)
        passes.shadowed.render(renderer)
      }
    }

    // The raster's direct light: no ambient light, no environment; shadows as the lights cast them, or none.
    const ambient: Light[] = []
    scene.traverse((o) => (o as AmbientLight).isAmbientLight && ambient.push(o as Light))
    const intensities = ambient.map((l) => l.intensity)
    const { environment } = scene
    for (const l of ambient) l.intensity = 0
    scene.environment = null
    const rasterStarted = performance.now()
    try {
      for (const k of ['shadowed', 'unshadowed'] as const) {
        renderer.setRenderTarget(raster[k])
        renderer.clear()
        const casting = k === 'shadowed' ? [] : traced.filter((t) => t.mesh.castShadow)
        for (const t of casting) t.mesh.castShadow = false
        try {
          for (let f = 0; f < (k === 'shadowed' ? rasterFrames : 1); f++) {
            g.advanceFrame()
            renderer.setRenderTarget(raster.frame)
            renderer.clear()
            renderer.render(scene, camera)
            if (f === 0 && k === 'shadowed') {
              renderer.setRenderTarget(raster.first)
              renderer.clear()
              add.render(renderer)
            }
            renderer.setRenderTarget(raster[k])
            add.render(renderer)
          }
        } finally {
          for (const t of casting) t.mesh.castShadow = true
        }
      }
    } finally {
      ambient.forEach((l, i) => (l.intensity = intensities[i]))
      scene.environment = environment
    }
    rasterMs = performance.now() - rasterStarted
  })
  try {
    const converged = converge ? { at: await readBack(renderer, atN, size, 1 / refFrames), twice: await readBack(renderer, sums.shadowed, size, 1 / (2 * refFrames)) } : undefined
    // The G-buffer is drawn by renderer.render, its rows the other way from the quads' sums: turned to match them.
    const drawn = await readBack(renderer, targets.position, size)
    const row = size.width * 4
    const position = new Float32Array(drawn.length)
    for (let y = 0; y < size.height; y++) position.set(drawn.subarray((size.height - 1 - y) * row, (size.height - y) * row), y * row)
    for (let i = 0; i < position.length; i += 4) for (let k = 0; k < 3; k++) position[i + k] -= POSITION_OFFSET
    return {
      size,
      frames: refFrames,
      position,
      converge: converged,
      reference: { shadowed: converged ? converged.twice : await readBack(renderer, sums.shadowed, size, 1 / refFrames), unshadowed: await readBack(renderer, sums.unshadowed, size, 1 / refFrames) },
      raster: { shadowed: await readBack(renderer, raster.shadowed, size, 1 / rasterFrames), unshadowed: await readBack(renderer, raster.unshadowed, size), first: await readBack(renderer, raster.first, size) },
      referenceMs,
      rasterMs,
    }
  } finally {
    for (const rt of [...Object.values(targets), ...Object.values(sums), ...Object.values(raster), atN]) rt.dispose()
    copyMaterial.dispose()
    for (const p of Object.values(passes)) p.material.dispose()
    meanMaterial.dispose()
  }
}

/** Luminance of pixel i of a linear RGBA array. */
export const lum = (a: Float32Array, i: number) => 0.2126 * a[i * 4] + 0.7152 * a[i * 4 + 1] + 0.0722 * a[i * 4 + 2]

