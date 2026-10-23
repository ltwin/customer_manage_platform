// @ts-nocheck — spike: volume lighting, two ways, one light evaluation.
//
//  A (march): per pixel, a ray from the camera to the opaque surface, stepped
//    only where it crosses a volume; each step lit by every light with its
//    shadow map. At a scale of the screen, into two targets (in-scattered
//    light, transmittance), then upsampled by depth into the composite.
//  B (froxels): the view cut into FX × FY × FZ cells, exponential in depth
//    over the volumes' depth range; a pass lights each cell (same code as A),
//    a pass integrates each column front to back, the composite looks the
//    pixel's depth up. Cells live in a 2D atlas: fragment passes only, so
//    the same TSL runs under WebGL2 (which cannot write storage textures).
//
// Both write colour · T + L before tone mapping (Epic design「合成顺序」第 3 步).
import { Color, DepthTexture, PointShadowNode, ShadowNode, HalfFloatType, Matrix4, NodeMaterial, QuadMesh, Quaternion, RenderTarget, Vector2, Vector3, Vector4, NearestFilter } from 'three/webgpu'
import {
  Fn, If, Loop, abs, clamp, cubeTexture, dot, exp, float, floor, getViewPosition, int, interleavedGradientNoise, ivec2, length, log, max, min, mix, normalize,
  outputStruct, positionWorld, pow, screenCoordinate, struct, screenUV, select, smoothstep, sqrt, texture, textureLoad, uniform, uniformArray, vec2, vec3, vec4, viewZToPerspectiveDepth,
} from 'three/tsl'

export const MAX = 8

/** A pass's two outputs: one function call, two targets. */
const Pair = struct({ a: 'vec4', b: 'vec4' })
const both = (f) => {
  const r = f()
  return outputStruct(r.get('a'), r.get('b'))
}

/** data: 体积物件's fields as uniforms, at most MAX. */
export class Volumes {
  inv = Array.from({ length: MAX }, () => new Matrix4())
  sct = Array.from({ length: MAX }, () => new Vector4()) // σs rgb, g
  ext = Array.from({ length: MAX }, () => new Vector4()) // σt rgb, edge falloff
  shp = Array.from({ length: MAX }, () => new Vector4()) // 0 box, 1 sphere
  world: { m: Matrix4; shape: string }[] = []
  uInv = uniformArray(this.inv, 'mat4')
  uSct = uniformArray(this.sct, 'vec4')
  uExt = uniformArray(this.ext, 'vec4')
  uShp = uniformArray(this.shp, 'vec4')
  count = uniform(0, 'int')
  set(list) {
    this.world = []
    list.forEach((v, i) => {
      const m = new Matrix4().compose(new Vector3(...v.position), new Quaternion(), new Vector3(...v.scale))
      this.world.push({ m, shape: v.shape })
      this.inv[i].copy(m).invert()
      const sc = new Color(v.scatter_color)
      const ac = new Color(v.absorption_color)
      const s = [sc.r, sc.g, sc.b].map((c) => c * v.scatter_density)
      const a = [ac.r, ac.g, ac.b].map((c) => (1 - c) * v.absorption_density)
      this.sct[i].set(s[0], s[1], s[2], v.anisotropy)
      this.ext[i].set(s[0] + a[0], s[1] + a[1], s[2] + a[2], v.edge_falloff)
      this.shp[i].set(v.shape === 'sphere' ? 1 : 0, 0, 0, 0)
    })
    this.count.value = list.length
  }
  /** The volumes' view-depth range for camera: [near, far], or null when none is in front. */
  depthRange(camera) {
    let lo = Infinity
    let hi = -Infinity
    const p = new Vector3()
    for (const { m } of this.world) {
      for (let c = 0; c < 8; c++) {
        p.set(c & 1 ? 0.5 : -0.5, c & 2 ? 0.5 : -0.5, c & 4 ? 0.5 : -0.5).applyMatrix4(m).applyMatrix4(camera.matrixWorldInverse)
        lo = Math.min(lo, -p.z)
        hi = Math.max(hi, -p.z)
      }
    }
    if (hi <= camera.near) return null
    return [Math.max(lo, camera.near), Math.min(hi, camera.far)]
  }
}

const safe = (x) => select(abs(x).lessThan(1e-7), float(1e-7), x)

/** Where the ray o + t·d (world) crosses volume i: vec2(t in, t out), in > out for a miss. */
function hit(V, i, o, d) {
  const inv = V.uInv.element(i)
  const ol = inv.mul(vec4(o, 1)).xyz.toVar()
  const dl = inv.mul(vec4(d, 0)).xyz.toVar()
  const r = vec2(1e9, -1e9).toVar()
  If(V.uShp.element(i).x.lessThan(0.5), () => {
    const id = vec3(1).div(vec3(safe(dl.x), safe(dl.y), safe(dl.z)))
    const a = vec3(-0.5).sub(ol).mul(id)
    const b = vec3(0.5).sub(ol).mul(id)
    const lo = min(a, b)
    const hi = max(a, b)
    r.assign(vec2(max(max(lo.x, lo.y), lo.z), min(min(hi.x, hi.y), hi.z)))
  }).Else(() => {
    const A = dot(dl, dl)
    const B = dot(ol, dl)
    const disc = B.mul(B).sub(A.mul(dot(ol, ol).sub(0.25)))
    If(disc.greaterThan(0), () => {
      const s = sqrt(disc)
      r.assign(vec2(B.negate().sub(s), B.negate().add(s)).div(A))
    })
  })
  return r
}

/** Volume i's density share at world p: 1 inside, falling to 0 over edge_falloff at the boundary. */
function density(V, i, p) {
  const l = V.uInv.element(i).mul(vec4(p, 1)).xyz
  const q = abs(l)
  const e = select(V.uShp.element(i).x.lessThan(0.5), float(0.5).sub(max(max(q.x, q.y), q.z)), float(0.5).sub(length(l))).mul(2)
  const edge = V.uExt.element(i).w
  return select(e.lessThanEqual(0), float(0), select(edge.lessThan(1e-4), float(1), smoothstep(0, edge, e)))
}

const hg = (g, c) => {
  const g2 = g.mul(g)
  return g2.oneMinus().div(pow(g2.add(1).sub(g.mul(c).mul(2)), 1.5).mul(4 * Math.PI))
}

function shadow2D(map, matrix, p) {
  const c = matrix.mul(vec4(p, 1))
  const s = c.xyz.div(c.w)
  const uv = vec2(s.x, s.y.oneMinus())
  const inside = uv.x.greaterThanEqual(0).and(uv.x.lessThanEqual(1)).and(uv.y.greaterThanEqual(0)).and(uv.y.lessThanEqual(1)).and(s.z.lessThanEqual(1))
  return select(inside, texture(map, uv).compare(s.z), float(1))
}

/**
 * The lights as the volumes see them: per frame, from the scene's lights
 * (whose intensity is already pre-exposed in the product). The shadow maps
 * must exist: build after a first render.
 */
export function lightRig(lights, camera) {
  const rig = []
  const refresh = []
  const { sun, spot, point } = lights
  if (sun) {
    const toL = uniform(new Vector3())
    const E = uniform(new Color())
    const node = sun.userData.cascades ?? sun.shadow.shadowNode
    const cascades = node.lights.map((l, i) => ({ map: l.shadow.map.depthTexture, matrix: uniform(l.shadow.matrix), range: uniform(node._cascades[i]) }))
    refresh.push(() => {
      const a = new Vector3()
      const b = new Vector3()
      sun.getWorldPosition(a)
      sun.target.getWorldPosition(b)
      toL.value.subVectors(a, b).normalize()
      E.value.copy(sun.color).multiplyScalar(sun.intensity)
    })
    const near = uniform(camera.near)
    const far = uniform(camera.far)
    rig.push({
      kind: 'sun',
      at: (p, vz) => {
        const share = vz.sub(near).div(far.sub(near))
        const vis = float(1).toVar()
        for (const c of cascades) If(share.greaterThanEqual(c.range.x).and(share.lessThan(c.range.y)), () => {
          vis.assign(shadow2D(c.map, c.matrix, p))
        })
        return { E: E.mul(vis), toL, maxT: float(1e4) }
      },
    })
  }
  if (spot) {
    const pos = uniform(new Vector3())
    const axis = uniform(new Vector3())
    const E = uniform(new Color())
    const cone = uniform(new Vector2())
    const map = spot.shadow.map.depthTexture
    const matrix = uniform(spot.shadow.matrix)
    refresh.push(() => {
      spot.getWorldPosition(pos.value)
      spot.target.getWorldPosition(axis.value)
      axis.value.sub(pos.value).normalize()
      E.value.copy(spot.color).multiplyScalar(spot.intensity)
      cone.value.set(Math.cos(spot.angle), Math.cos(spot.angle * (1 - spot.penumbra)))
    })
    rig.push({
      kind: 'spot',
      at: (p) => {
        const v = p.sub(pos)
        const d2 = dot(v, v)
        const d = sqrt(d2)
        const toL = v.negate().div(d)
        const att = smoothstep(cone.x, cone.y, dot(toL.negate(), axis)).div(max(d2, 0.01))
        return { E: E.mul(att).mul(shadow2D(map, matrix, p)), toL, maxT: d }
      },
    })
  }
  if (point) {
    const pos = uniform(new Vector3())
    const E = uniform(new Color())
    const map = point.shadow.map.depthTexture
    const near = uniform(0)
    const far = uniform(0)
    refresh.push(() => {
      point.getWorldPosition(pos.value)
      E.value.copy(point.color).multiplyScalar(point.intensity)
      near.value = point.shadow.camera.near
      far.value = point.shadow.camera.far
    })
    rig.push({
      kind: 'point',
      at: (p) => {
        const v = p.sub(pos)
        const q = abs(v)
        const m = max(max(q.x, q.y), q.z)
        const dp = viewZToPerspectiveDepth(m.negate(), near, far)
        const vis = select(m.lessThan(far), cubeTexture(map, v).compare(dp), float(1))
        const d2 = dot(v, v)
        const d = sqrt(d2)
        return { E: E.mul(vis).div(max(d2, 0.01)), toL: v.negate().div(d), maxT: d }
      },
    })
  }
  // Ambient (optional): the scene's ambient and hemisphere lights as an even
  // radiance from all round, as the materials take them (unoccluded): with an
  // isotropic share the in-scattered light is σs · L. Kept on the rig, not a light.
  const ambient = uniform(new Color())
  refresh.push(() => {
    ambient.value.setRGB(0, 0, 0)
    lights.scene?.traverse((o) => {
      if (o.isAmbientLight) ambient.value.add(o.color.clone().multiplyScalar(o.intensity / Math.PI))
      if (o.isHemisphereLight) ambient.value.add(o.color.clone().add(o.groundColor).multiplyScalar(o.intensity / (2 * Math.PI)))
    })
    if (!lights.ambient) ambient.value.setRGB(0, 0, 0)
  })
  return { rig, ambient, refresh: () => refresh.forEach((f) => f()) }
}

/**
 * The light scattered towards the camera per metre at world p (S) and the
 * extinction there (σt), for view direction vd and view depth vz.
 * selfShadow: each light dimmed by the volumes between p and it, taken as
 * uniform (the edge falloff ignored): a chord per volume, no march.
 */
function inscatter(V, rigAll, p, vd, vz, selfShadow, mask = null) {
  const rig = rigAll.rig
  const S = vec3(0).toVar()
  const st = vec3(0).toVar()
  const lit = rig.map((l) => {
    const r = l.at(p, vz)
    const E = vec3(r.E).toVar()
    const toL = vec3(r.toL).toVar()
    if (selfShadow === true || selfShadow === 'all') {
      const tau = vec3(0).toVar()
      Loop(V.count, ({ i }) => {
        const h = hit(V, i, p, toL)
        tau.addAssign(V.uExt.element(i).xyz.mul(max(min(h.y, r.maxT).sub(max(h.x, 0)), 0)))
      })
      E.mulAssign(exp(tau.negate()))
    }
    return { E, toL, maxT: r.maxT }
  })
  Loop(V.count, ({ i }) => {
    const rho = (mask ? select(mask.bitAnd(int(1).shiftLeft(i)).notEqual(0), density(V, i, p), float(0)) : density(V, i, p)).toVar()
    If(rho.greaterThan(0), () => {
      const sct = V.uSct.element(i)
      st.addAssign(V.uExt.element(i).xyz.mul(rho))
      const sum = vec3(0).toVar()
      for (const l of lit) {
        let E = l.E
        // 'own': dimmed by this volume only, along its chord towards the light (other volumes' shadows not counted).
        if (selfShadow === 'own') {
          const h = hit(V, i, p, l.toL)
          E = E.mul(exp(V.uExt.element(i).xyz.mul(max(min(h.y, l.maxT).sub(max(h.x, 0)), 0)).negate()))
        }
        sum.addAssign(E.mul(hg(sct.w, dot(l.toL, vd))))
      }
      S.addAssign(sct.xyz.mul(rho).mul(sum.add(rigAll.ambient)))
    })
  })
  return { S, st }
}

/** One step of the integration (Hillaire 2015, energy-conserving), in place. */
function step(L, T, S, st, dt) {
  const Tr = exp(st.mul(dt).negate()).toVar()
  L.addAssign(T.mul(S.sub(S.mul(Tr)).div(max(st, vec3(1e-6)))))
  T.mulAssign(Tr)
}

/** What both need of the camera, per frame. */
export function cameraRig(camera) {
  const projInv = uniform(camera.projectionMatrixInverse)
  const world = uniform(camera.matrixWorld)
  const pos = uniform(new Vector3())
  return { projInv, world, pos, refresh: () => camera.getWorldPosition(pos.value) }
}

function pass(width, height, count) {
  const rt = new RenderTarget(width, height, { type: HalfFloatType, count, depthBuffer: false })
  for (const t of rt.textures) {
    t.minFilter = t.magFilter = NearestFilter
    t.generateMipmaps = false
  }
  return rt
}

/** A: the march, at scale of the screen. */
export function marchA(V, rig, cam, depth, { width, height, scale, steps, selfShadow, blur, useMask = true }) {
  const w = Math.round(width * scale)
  const h = Math.round(height * scale)
  const rt = pass(w, h, 2)
  const m = new NodeMaterial()
  m.fragmentNode = both(Fn(() => {
    const d = texture(depth, screenUV).r
    const vp = getViewPosition(screenUV, d, cam.projInv).toVar()
    const surface = select(d.greaterThanEqual(1), float(1e4), length(vp))
    const dirV = normalize(vp).toVar()
    const dir = cam.world.mul(vec4(dirV, 0)).xyz.toVar()
    const t0 = float(1e9).toVar()
    const t1 = float(0).toVar()
    // The volumes this ray crosses in front of the surface: the steps look at these only.
    const crossed = int(0).toVar()
    Loop(V.count, ({ i }) => {
      const r = hit(V, i, cam.pos, dir)
      If(r.y.greaterThan(max(r.x, 0)).and(r.x.lessThan(surface)), () => {
        t0.assign(min(t0, max(r.x, 0)))
        t1.assign(max(t1, r.y))
        crossed.assign(crossed.bitOr(int(1).shiftLeft(i)))
      })
    })
    t1.assign(min(t1, surface))
    const L = vec3(0).toVar()
    const T = vec3(1).toVar()
    If(t1.greaterThan(t0), () => {
      const dt = t1.sub(t0).div(steps)
      const jitter = interleavedGradientNoise(screenCoordinate.xy)
      Loop(steps, ({ i }) => {
        const t = t0.add(float(i).add(jitter).mul(dt))
        const p = cam.pos.add(dir.mul(t)).toVar()
        const { S, st } = inscatter(V, rig, p, dir, t.mul(dirV.z.negate()), selfShadow, useMask ? crossed : null)
        step(L, T, S, st, dt)
      })
    })
    return Pair(vec4(L, surface.mul(dirV.z.negate())), vec4(T, 1))
  }))
  const quad = new QuadMesh(m)
  // Optional: a separable blur over the march's noise, weighted by depth likeness (7 taps a way).
  const blurred = [pass(w, h, 2), pass(w, h, 2)]
  const blurPass = (src, dx, dy) => {
    const b = new NodeMaterial()
    b.fragmentNode = both(Fn(() => {
      const c = ivec2(floor(screenUV.mul(vec2(w, h))))
      const centre = textureLoad(src.textures[0], c)
      const L = vec3(0).toVar()
      const T = vec3(0).toVar()
      const sum = float(0).toVar()
      for (let k = -3; k <= 3; k++) {
        const at = ivec2(clamp(vec2(c).add(vec2(dx * k, dy * k)), vec2(0), vec2(w - 1, h - 1)))
        const l = textureLoad(src.textures[0], at)
        const wgt = float(Math.exp(-(k * k) / 4.5)).div(abs(l.a.sub(centre.a)).div(centre.a.mul(0.02)).add(1))
        L.addAssign(l.rgb.mul(wgt))
        T.addAssign(textureLoad(src.textures[1], at).rgb.mul(wgt))
        sum.addAssign(wgt)
      }
      return Pair(vec4(L.div(sum), centre.a), vec4(T.div(sum), 1))
    }))
    return { q: new QuadMesh(b), b }
  }
  const h1 = blurPass(rt, 1, 0)
  const v1 = blurPass(blurred[0], 0, 1)
  const out = blur ? blurred[1] : rt
  return {
    rt: out,
    render(renderer) {
      renderer.setRenderTarget(rt)
      quad.render(renderer)
      if (blur) {
        renderer.setRenderTarget(blurred[0])
        h1.q.render(renderer)
        renderer.setRenderTarget(blurred[1])
        v1.q.render(renderer)
      }
    },
    /** colour · T + L, upsampled by depth (four texels weighted by bilinear share and depth likeness). */
    apply(color, depth) {
      if (scale === 1) return color.rgb.mul(texture(out.textures[1], screenUV).rgb).add(texture(out.textures[0], screenUV).rgb)
      const d = texture(depth, screenUV).r
      const vz = select(d.greaterThanEqual(1), float(1e4), getViewPosition(screenUV, d, cam.projInv).z.negate())
      const at = screenUV.mul(vec2(w, h)).sub(0.5)
      const base = floor(at)
      const f = at.sub(base)
      const L = vec3(0).toVar()
      const T = vec3(0).toVar()
      const sum = float(0).toVar()
      for (const [ox, oy] of [[0, 0], [1, 0], [0, 1], [1, 1]]) {
        const c = ivec2(clamp(base.add(vec2(ox, oy)), vec2(0), vec2(w - 1, h - 1)))
        const l = textureLoad(out.textures[0], c)
        const bw = (ox ? f.x : f.x.oneMinus()).mul(oy ? f.y : f.y.oneMinus())
        const wgt = bw.add(1e-3).div(abs(l.a.sub(vz)).div(vz.mul(0.02)).add(1)).toVar()
        L.addAssign(l.rgb.mul(wgt))
        T.addAssign(textureLoad(out.textures[1], c).rgb.mul(wgt))
        sum.addAssign(wgt)
      }
      return color.rgb.mul(T.div(sum)).add(L.div(sum))
    },
    dispose() {
      rt.dispose()
      blurred.forEach((r) => r.dispose())
      m.dispose()
    },
  }
}

/** B: froxels. */
export function froxelsB(V, rig, cam, { fx, fy, fz, selfShadow, jitter = 0.5 }) {
  const cols = 8
  const rows = Math.ceil(fz / cols)
  const W = fx * cols
  const H = fy * rows
  const injected = pass(W, H, 2)
  const integrated = pass(W, H, 2)
  for (const t of integrated.textures) t.minFilter = t.magFilter = 1006 // LinearFilter
  const near = uniform(1)
  const ratio = uniform(2) // far / near
  const depthOf = (k) => near.mul(pow(ratio, k.div(fz)))
  const cell = () => {
    const px = floor(screenUV.mul(vec2(W, H)))
    const tile = floor(px.div(vec2(fx, fy)))
    const local = px.sub(tile.mul(vec2(fx, fy)))
    return { local, k: tile.y.mul(cols).add(tile.x) }
  }
  const rayOf = (uv) => {
    const vpN = getViewPosition(uv, float(0.5), cam.projInv).toVar()
    return { vpN, perDepth: length(vpN).div(vpN.z.negate()) }
  }

  const inject = new NodeMaterial()
  inject.fragmentNode = both(Fn(() => {
    const { local, k } = cell()
    const uv = local.add(0.5).div(vec2(fx, fy))
    const { vpN } = rayOf(uv)
    const vz = depthOf(k.add(jitter))
    const p = cam.world.mul(vec4(vpN.mul(vz.div(vpN.z.negate())), 1)).xyz.toVar()
    const vd = normalize(p.sub(cam.pos))
    const { S, st } = inscatter(V, rig, p, vd, vz, selfShadow)
    return Pair(vec4(S, 1), vec4(st, 1))
  }))

  const integrate = new NodeMaterial()
  integrate.fragmentNode = both(Fn(() => {
    const { local, k } = cell()
    const { perDepth } = rayOf(local.add(0.5).div(vec2(fx, fy)))
    const L = vec3(0).toVar()
    const T = vec3(1).toVar()
    Loop(fz, ({ i }) => {
      If(float(i).lessThanEqual(k), () => {
        const fi = float(i)
        const ty = floor(fi.div(cols))
        const c = ivec2(vec2(fi.sub(ty.mul(cols)), ty).mul(vec2(fx, fy)).add(local))
        const dt = depthOf(fi.add(1)).sub(depthOf(fi)).mul(perDepth)
        step(L, T, textureLoad(injected.textures[0], c).rgb, textureLoad(injected.textures[1], c).rgb, dt)
      })
    })
    return Pair(vec4(L, 1), vec4(T, 1))
  }))

  const q1 = new QuadMesh(inject)
  const q2 = new QuadMesh(integrate)
  const on = uniform(0)
  return {
    rt: integrated,
    render(renderer, camera) {
      const r = V.depthRange(camera)
      on.value = r ? 1 : 0
      if (!r) return
      near.value = r[0]
      ratio.value = Math.max(r[1] / r[0], 1.0001)
      renderer.setRenderTarget(injected)
      q1.render(renderer)
      renderer.setRenderTarget(integrated)
      q2.render(renderer)
    },
    apply(color, depth) {
      const d = texture(depth, screenUV).r
      const vz = select(d.greaterThanEqual(1), float(1e4), getViewPosition(screenUV, d, cam.projInv).z.negate())
      const u = log(vz.div(near)).div(log(ratio)).mul(fz).sub(1)
      const i0 = floor(u)
      const w = u.sub(i0)
      const slice = (k) => {
        const kk = clamp(k, 0, fz - 1)
        const ty = floor(kk.div(cols))
        const at = vec2(kk.sub(ty.mul(cols)), ty).mul(vec2(fx, fy)).add(clamp(screenUV.mul(vec2(fx, fy)), vec2(0.5), vec2(fx - 0.5, fy - 0.5))).div(vec2(W, H))
        const none = k.lessThan(0)
        return { L: select(none, vec3(0), texture(integrated.textures[0], at).rgb), T: select(none, vec3(1), texture(integrated.textures[1], at).rgb) }
      }
      const a = slice(i0)
      const b = slice(i0.add(1))
      return mix(color.rgb, color.rgb.mul(mix(a.T, b.T, w)).add(mix(a.L, b.L, w)), on)
    },
    dispose() {
      injected.dispose()
      integrated.dispose()
      inject.dispose()
      integrate.dispose()
    },
  }
}

/**
 * cast_shadow: each light's shadow on the surfaces times the light let
 * through by the volumes between the surface and the light (the chords, as
 * for self-shadowing, every volume). Before the first render: three keeps
 * the first shadow node a light builds.
 */
export function volumeShadows(V, lights) {
  const through = (toL, maxT) =>
    Fn(() => {
      const p = positionWorld.toVar()
      const tau = vec3(0).toVar()
      Loop(V.count, ({ i }) => {
        const h = hit(V, i, p, toL(p))
        tau.addAssign(V.uExt.element(i).xyz.mul(max(min(h.y, maxT(p)).sub(max(h.x, 0)), 0)))
      })
      return exp(tau.negate())
    })()
  const { sun, spot, point } = lights
  if (sun) {
    const node = sun.shadow.shadowNode
    sun.userData.cascades = node
    const toL = uniform(new Vector3())
    sun.userData.refreshToL = () => {
      const a = new Vector3()
      const b = new Vector3()
      sun.getWorldPosition(a)
      sun.target.getWorldPosition(b)
      toL.value.subVectors(a, b).normalize()
    }
    sun.shadow.shadowNode = node.mul(through(() => toL, () => float(1e4)))
  }
  for (const [l, Base] of [[spot, ShadowNode], [point, PointShadowNode]]) {
    if (!l) continue
    const pos = uniform(new Vector3())
    l.userData.refreshToL = () => l.getWorldPosition(pos.value)
    const base = new Base(l)
    l.shadow.shadowNode = base.mul(through((p) => normalize(pos.sub(p)), (p) => length(pos.sub(p))))
  }
  return () => [sun, spot, point].forEach((l) => l?.userData.refreshToL?.())
}
