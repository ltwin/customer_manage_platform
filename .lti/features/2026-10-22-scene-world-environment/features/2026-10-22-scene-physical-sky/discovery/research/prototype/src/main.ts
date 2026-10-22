// @ts-nocheck — spike page: window.__spike drives each case from Playwright.
import { WebGPURenderer } from 'three/webgpu'
import { BOTTOM, MULTI, SKYVIEW, TRANSMITTANCE, clear, viewRadius, type Atmosphere } from './atmo.ts'
import { multiScatteringTable, skyViewTexel, transmittance, transmittanceParams, transmittanceTable, Table } from './atmoCpu.ts'
import { makeLuts, setAtmosphere } from './atmoGpu.ts'
import { sceneCases } from './scene.ts'

const params = new URLSearchParams(location.search)
const forceWebGL = params.get('backend') === 'webgl'
const canvas = document.getElementById('c') as HTMLCanvasElement
const log = (s: string) => (document.getElementById('log')!.textContent += s + '\n')

const renderer = new WebGPURenderer({ canvas, antialias: false, forceWebGL })
await renderer.init()
const backend = renderer.backend.isWebGPUBackend ? 'webgpu' : 'webgl2'
log(`backend ${backend}`)

function halfToFloat(h: number): number {
  const s = (h & 0x8000) >> 15
  const e = (h & 0x7c00) >> 10
  const f = h & 0x03ff
  if (e === 0) return (s ? -1 : 1) * 2 ** -14 * (f / 1024)
  if (e === 0x1f) return f ? NaN : (s ? -1 : 1) * Infinity
  return (s ? -1 : 1) * 2 ** (e - 15) * (1 + f / 1024)
}

export async function readTarget(target): Promise<Float32Array> {
  const { width, height } = target
  const raw = await renderer.readRenderTargetPixelsAsync(target, 0, 0, width, height)
  if (raw instanceof Float32Array) return raw
  if (raw instanceof Uint16Array) return Float32Array.from(raw, halfToFloat)
  throw new Error(`unexpected readback ${raw?.constructor?.name}`)
}

/** Error stats of a GPU table (RGBA rows as read back) against a CPU one, trying both row orders. */
function compare(gpu: Float32Array, cpu: Table, floor: number) {
  const { w, h } = cpu
  const stats = (flip: boolean) => {
    let maxAbs = 0
    let maxRel = 0
    let sumRel = 0
    let n = 0
    let maxRef = 0
    let nonFinite = 0
    for (let y = 0; y < h; y++) {
      for (let x = 0; x < w; x++) {
        const gy = flip ? h - 1 - y : y
        for (let i = 0; i < 3; i++) {
          const g = gpu[(gy * w + x) * 4 + i]
          const c = cpu.data[(y * w + x) * 3 + i]
          if (!Number.isFinite(g)) {
            nonFinite++
            continue
          }
          const abs = Math.abs(g - c)
          maxAbs = Math.max(maxAbs, abs)
          maxRef = Math.max(maxRef, Math.abs(c))
          if (Math.abs(c) > floor) {
            const rel = abs / Math.abs(c)
            maxRel = Math.max(maxRel, rel)
            sumRel += rel
            n++
          }
        }
      }
    }
    return { maxAbs, maxRel, meanRel: sumRel / Math.max(1, n), counted: n, maxRef, nonFinite }
  }
  const a = stats(false)
  const b = stats(true)
  return a.meanRel <= b.meanRel ? { ...a, rows: 'as-is' } : { ...b, rows: 'flipped' }
}

const cpuCache = new Map<string, { tr: Table; ms: Table }>()
function cpuTables(a: Atmosphere) {
  const key = JSON.stringify(a)
  let t = cpuCache.get(key)
  if (!t) {
    const tr = transmittanceTable(a)
    const ms = multiScatteringTable(a, tr)
    t = { tr, ms }
    cpuCache.set(key, t)
  }
  return t
}

async function luts({ half, atmosphere = clear, sunElevDeg = 30 }: { half: boolean; atmosphere?: Atmosphere; sunElevDeg?: number }) {
  const l = makeLuts(half)
  setAtmosphere(l.u, atmosphere)
  const r = viewRadius(atmosphere)
  l.u.viewRadius.value = r
  l.u.sunMu.value = Math.sin((sunElevDeg * Math.PI) / 180)
  l.u.scale.value = 1
  l.renderAtmosphere(renderer)
  l.renderSkyView(renderer)
  const [gtr, gms, gsv] = [await readTarget(l.tr), await readTarget(l.ms), await readTarget(l.sv)]
  const t0 = performance.now()
  const { tr, ms } = cpuTables(atmosphere)
  const sv = Table.make(SKYVIEW.w, SKYVIEW.h, (u, v) => skyViewTexel(atmosphere, tr, ms, r, l.u.sunMu.value, u, v))
  const cpuMs = performance.now() - t0
  l.dispose()
  return {
    backend,
    half,
    sunElevDeg,
    transmittance: compare(gtr, tr, 1e-4),
    multiScattering: compare(gms, ms, 1e-6),
    skyView: compare(gsv, sv, 1e-6),
    cpuMs,
  }
}

/** The transmittance's smallest values: what fp16 keeps of a grazing ray. */
function grazing(atmosphere: Atmosphere = clear) {
  const rows = []
  for (const v of [0, 0.5 / TRANSMITTANCE.h]) {
    for (const u of [1 - 0.5 / TRANSMITTANCE.w, 0.99, 0.95, 0.9]) {
      const { r, mu } = transmittanceParams(u, v)
      rows.push({ u, v, heightKm: r - BOTTOM, mu, t: transmittance(atmosphere, r, mu) })
    }
  }
  return rows
}

async function timing({ half, atmosphere = clear, repeat = 20 }: { half: boolean; atmosphere?: Atmosphere; repeat?: number }) {
  const l = makeLuts(half)
  setAtmosphere(l.u, atmosphere)
  l.u.viewRadius.value = viewRadius(atmosphere)
  const sync = async () => {
    if (backend === 'webgpu') await renderer.backend.device.queue.onSubmittedWorkDone()
    else await readTarget(l.sv)
  }
  // Warm up: compile.
  l.renderAtmosphere(renderer)
  l.renderSkyView(renderer)
  await sync()
  const time = async (f: () => void) => {
    const out: number[] = []
    for (let i = 0; i < repeat; i++) {
      await sync()
      const t0 = performance.now()
      f()
      await sync()
      out.push(performance.now() - t0)
    }
    out.sort((a, b) => a - b)
    return { median: out[out.length >> 1], min: out[0], max: out[out.length - 1] }
  }
  const empty = await time(() => {})
  const atmo = await time(() => l.renderAtmosphere(renderer))
  const sky = await time(() => {
    l.u.sunMu.value = Math.random()
    l.renderSkyView(renderer)
  })
  l.dispose()
  return { backend, half, empty, atmosphere: atmo, skyView: sky }
}

window.__spike = { backend, luts, grazing, timing, ...sceneCases(renderer, backend, readTarget) }
log('ready')
