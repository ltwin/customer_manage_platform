import test from 'node:test'
import assert from 'node:assert/strict'
import { luminance, type Vec3 } from '../src/creative-scene/world/atmosphere/constants.ts'
import { CpuAtmosphere } from '../src/creative-scene/world/atmosphere/cpu.ts'
import { completeSky, type PhysicalSky } from '../src/creative-scene/world/physicalSky.ts'

// W4 (scene-aerial-perspective design「froxel 的生成」): the aerial perspective as a froxel integrates it, on the CPU.

const atmosphere = (patch: Partial<PhysicalSky> = {}) => new CpuAtmosphere(completeSky({ kind: 'physical', date: '2026-06-21', ...patch }) as PhysicalSky)
const toward = (elevationDeg: number, azimuthDeg = 0): Vec3 => {
  const e = (elevationDeg * Math.PI) / 180
  const a = (azimuthDeg * Math.PI) / 180
  return [Math.cos(e) * Math.sin(a), Math.sin(e), Math.cos(e) * Math.cos(a)]
}

test('the air between blues what is far at noon, lets less of it through the farther it is, and nothing at the camera', () => {
  const cpu = atmosphere()
  const sun = toward(60, 90)
  const level = toward(0)
  let last = 1
  for (const km of [0.5, 2, 8, 32]) {
    const { scattered, transmittance } = cpu.aerial(level, sun, km)
    assert.ok(luminance(transmittance) < last, `${km} km: less let through`)
    last = luminance(transmittance)
    assert.ok(scattered[2] > scattered[0], `${km} km: bluer than red`)
  }
  const near = cpu.aerial(level, sun, 0.001)
  const far = cpu.aerial(level, sun, 32)
  assert.ok(luminance(near.transmittance) > 0.9999 && luminance(near.scattered) < luminance(far.scattered) * 1e-3, 'a metre away, next to nothing')
})

test('a view down is integrated level, as far over the plain', () => {
  const cpu = atmosphere()
  const sun = toward(40, 120)
  const down = cpu.aerial(toward(-20, 30), sun, 10)
  const level = cpu.aerial(toward(0, 30), sun, 10)
  assert.deepEqual(down, level)
})

test('haze lets less through; with the sun down and no moon the air scatters next to nothing', () => {
  const clear = atmosphere().aerial(toward(0), toward(30, 90), 10)
  const hazy = atmosphere({ dust_density: 12 }).aerial(toward(0), toward(30, 90), 10)
  assert.ok(luminance(hazy.transmittance) < luminance(clear.transmittance) * 0.5)
  const night = atmosphere().aerial(toward(0), toward(-18, 90), 10)
  assert.ok(luminance(night.scattered) < luminance(clear.scattered) * 0.01)
})
