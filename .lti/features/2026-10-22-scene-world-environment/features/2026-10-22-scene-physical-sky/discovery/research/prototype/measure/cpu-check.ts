import { clear, SUN_LUX, viewRadius, type Atmosphere, type Vec3 } from '../src/atmo.ts'
import { luminance, multiScatteringTable, skyIrradiance, sunTransmittance, transmittanceTable } from '../src/atmoCpu.ts'
const t0 = performance.now()
const a: Atmosphere = { ...clear, mieScale: Number(process.env.MIE_SCALE ?? 1) }
const tr = transmittanceTable(a, 128, 32)
const t1 = performance.now()
const ms = multiScatteringTable(a, tr, 16, 16)
const t2 = performance.now()
console.log(`tables: transmittance 128x32 ${(t1 - t0).toFixed(1)} ms, multi 16x16 ${(t2 - t1).toFixed(1)} ms`)
const r = viewRadius(a)
for (const el of [90, 70, 60, 55, 30, 10, 6, 2, 0, -2, -4, -6, -9, -12, -18]) {
  const e = (el * Math.PI) / 180
  const sun: Vec3 = [Math.cos(e), Math.sin(e), 0]
  const s0 = performance.now()
  const sky = skyIrradiance(a, tr, ms, r, sun)
  const s1 = performance.now()
  const ts = sunTransmittance(a, r, sun)
  const direct = luminance(ts) * Math.max(0, Math.sin(e)) * SUN_LUX
  const diffuse = luminance(sky) * SUN_LUX
  const E = direct + diffuse
  const ev = Math.log2((E * 100) / 250)
  console.log(`sun ${String(el).padStart(4)}°: direct ${direct.toFixed(1).padStart(9)} lx  sky ${diffuse.toFixed(2).padStart(9)} lx  E ${E.toFixed(2).padStart(10)} lx  EV100 ${ev.toFixed(2).padStart(6)}  sunT ${ts.map((x) => x.toFixed(3)).join(',')}  (${(s1 - s0).toFixed(1)} ms)`)
}
