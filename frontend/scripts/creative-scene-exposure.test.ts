import test from 'node:test'
import assert from 'node:assert/strict'
import { Color, Fog, PointLight, Scene } from 'three'
import { BOTTOM, coefficients, groundRadius, luminance, MIE_ABSORPTION, MIE_ABSORPTION_TINT, PRESETS, presetOf, SUN_LUX, sunTint, viewRadius, type Vec3 } from '../src/creative-scene/world/atmosphere/constants.ts'
import { CpuAtmosphere } from '../src/creative-scene/world/atmosphere/cpu.ts'
import { applyExposure, autoEV100, environmentEV, environmentScale, ev100, exposureScale, isoThirds, nearestStep, newCameraExposure, nightDarkening, preExposure, shutterThirds, viewerEV100 } from '../src/creative-scene/world/exposure.ts'
import { frameExposure } from '../src/creative-scene/render/litLook.ts'
import { NIGHT_TINT, nightLookColor, nightLookWeight } from '../src/creative-scene/world/nightLook.ts'

// W2: the physical sky's light on the CPU and the exposure arithmetic
// (AC-W2-3, AC-W2-4, design「曝光」「太阳的亮度与色温」).

const sunAbove: Vec3 = [SUN_LUX, SUN_LUX, SUN_LUX]
const mu = (elevationDeg: number) => Math.sin((elevationDeg * Math.PI) / 180)

test('a clear sky with the sun at 60° lights level ground with 100–130 klux, most of it the sun', () => {
  const d = new CpuAtmosphere(PRESETS.clear).daylight(mu(60), sunAbove)
  assert.ok(d.horizontal > 100_000 && d.horizontal < 130_000, `${d.horizontal.toFixed(0)} lux`)
  assert.ok(luminance(d.sky) > 8_000 && luminance(d.sky) < 25_000, `sky ${luminance(d.sky).toFixed(0)} lux`)
  assert.ok(d.sky[2] > d.sky[0], 'the sky light is blue')
})

test('haze darkens the day, a plateau brightens it; behind the ground the sun is gone', () => {
  const at40 = (p: (typeof PRESETS)[keyof typeof PRESETS]) => new CpuAtmosphere(p).daylight(mu(40), sunAbove)
  const clear = at40(PRESETS.clear)
  assert.ok(at40(PRESETS.haze).horizontal < clear.horizontal * 0.9, 'haze')
  assert.ok(at40(PRESETS.highland).horizontal > clear.horizontal, 'plateau')
  const dust = at40(PRESETS.dust)
  assert.ok(dust.sun[0] / dust.sun[2] > (clear.sun[0] / clear.sun[2]) * 1.5, 'dust turns the sun yellow')
  const below = new CpuAtmosphere(PRESETS.clear).daylight(mu(-3), sunAbove)
  assert.deepEqual(below.sun, [0, 0, 0])
  assert.ok(below.horizontal > 0 && below.horizontal < 1000, `twilight ${below.horizontal.toFixed(1)} lux`)
})

// Twilight as drawn: with coarser tables the meter stayed 6 stops up with the
// sun 7–11° down, then fell 6 stops within a degree, and the automatic
// exposure went dark at dusk, then jumped bright (owner 2026-10-23).
test('twilight dims smoothly, as the GPU draws it: no cliff down to the sun 12° below', () => {
  const cpu = new CpuAtmosphere(PRESETS.clear)
  const ev = (elevationDeg: number) => environmentEV(cpu.daylight(mu(elevationDeg), sunAbove).horizontal)
  let last = ev(4)
  for (let e = 3.5; e >= -12; e -= 0.5) {
    const now = ev(e)
    assert.ok(now < last && last - now < 2, `${e}°: EV ${now.toFixed(2)} after ${last.toFixed(2)}`)
    last = now
  }
  assert.ok(ev(-9) < -2 && ev(-9) > -5, `the sun 9° down meters ${ev(-9).toFixed(2)}`)
})

test('the ground under the scene is at its altitude, the camera 10 m above it; Mie absorption is not normalised', () => {
  assert.equal(groundRadius(PRESETS.highland), BOTTOM + 4)
  assert.ok(Math.abs(viewRadius(PRESETS.highland) - (BOTTOM + 4.01)) < 1e-9)
  assert.deepEqual(coefficients(PRESETS.clear).mieAbsorption, MIE_ABSORPTION_TINT.map((t) => t * MIE_ABSORPTION))
  assert.equal(presetOf({ altitude_m: 0, air_density: 1, dust_density: 12, ozone_density: 1 }), 'haze')
  assert.equal(presetOf({ altitude_m: 0, air_density: 1, dust_density: 13, ozone_density: 1 }), null)
})

test('EV100 is log₂(N² / t) − log₂(S / 100); sunny 16 is about 15', () => {
  assert.ok(Math.abs(ev100(16, 1 / 125, 100) - 14.97) < 0.01)
  assert.ok(Math.abs(ev100(2.8, 1 / 4000, 100) - 14.94) < 0.01, 'the camera defaults')
  assert.ok(Math.abs(ev100(2.8, 1 / 30, 1600) - 3.88) < 0.01)
  assert.equal(exposureScale(0), 1 / 1.2)
  assert.ok(Math.abs(exposureScale(15) * 2 ** 15 * 1.2 - 1) < 1e-12)
})

test('shutters and ISOs go in thirds of a stop', () => {
  for (const steps of [shutterThirds, isoThirds]) {
    for (let i = 1; i < steps.length; i++) {
      const third = Math.log2(steps[i] / steps[i - 1])
      assert.ok(third > 0.2 && third < 0.48, `${steps[i - 1]} → ${steps[i]}: ${third.toFixed(2)} stops`)
    }
  }
  assert.deepEqual([shutterThirds[0], shutterThirds.at(-1), isoThirds[0], isoThirds.at(-1)], [1 / 8000, 30, 25, 102400])
  assert.equal(nearestStep(shutterThirds, 1 / 522), 1 / 500)
})

test('a camera added under a physical sky is as bright as the viewport, within a third of a stop, as far as f/2.8 reaches', () => {
  const off = (ev: number) => {
    const e = newCameraExposure(ev, 2.8)
    return { ...e, off: ev100(2.8, e.shutter_s, e.iso) - ev }
  }
  assert.deepEqual(newCameraExposure(12, 2.8), { iso: 100, shutter_s: 1 / 500 })
  assert.deepEqual(newCameraExposure(-5, 2.8), { iso: 800, shutter_s: 30 }, 'no shutter is long enough: the ISO makes up the rest')
  // The marked thirds are rounded (0.6 s is 0.63 s): the nearest is up to about a fifth of a stop off.
  for (let ev = -11.9; ev <= 17.9; ev += 0.1) assert.ok(Math.abs(off(ev).off) <= 0.25, `EV ${ev.toFixed(1)}: ${JSON.stringify(off(ev))}`)
  const bright = off(19)
  assert.deepEqual([bright.iso, bright.shutter_s], [25, 1 / 8000])
  assert.ok(bright.off < -1 && bright.off > -1.2, 'past the reach it is darker, the known limit')
})

test('the sun at 6500 K is neutral; warmer and cooler change the colour, not the brightness', () => {
  for (const v of sunTint(6500)) assert.ok(Math.abs(v - 1) < 1e-9)
  for (const k of [2000, 3200, 5000, 8000, 12000]) {
    const c = sunTint(k)
    assert.ok(Math.abs(luminance(c) - 1) < 1e-9, `${k} K luminance ${luminance(c)}`)
    assert.ok(c.every((v) => v >= 0), `${k} K: ${c.join(', ')}`)
  }
  const warm = sunTint(2000)
  assert.ok(warm[0] > warm[1] && warm[1] > warm[2], 'warm: red over blue')
  const cool = sunTint(12000)
  assert.ok(cool[2] > cool[0], 'cool: blue over red')
})

test('s_env keeps noon and night near 1 in half floats; it clamps at EV −12 and 20', () => {
  assert.equal(environmentEV(1e-9), -12)
  assert.equal(environmentEV(1e12), 20)
  const noon = new CpuAtmosphere(PRESETS.clear).daylight(mu(60), sunAbove)
  // The sky's radiance at noon, stored × s_env: about E / π scaled down to O(0.1–1).
  const stored = (luminance(noon.sky) / Math.PI) * environmentScale(noon.horizontal)
  assert.ok(stored > 0.01 && stored < 2, String(stored))
})

test('the CPU atmosphere is quick enough to follow a slider (measured, not asserted: design targets 30 ms and 3 ms on the reference device)', () => {
  const t0 = performance.now()
  const cpu = new CpuAtmosphere(PRESETS.haze)
  const t1 = performance.now()
  for (let i = 0; i < 5; i++) cpu.daylight(mu(10 + i), sunAbove)
  const t2 = performance.now()
  console.log(`CpuAtmosphere: atmosphere change ${(t1 - t0).toFixed(1)} ms, time change ${((t2 - t1) / 5).toFixed(2)} ms`)
})

// design「预曝光的落点」: k goes onto the three objects before every picture.

function lampScene() {
  const scene = new Scene()
  const lamp = new PointLight('#ffffff', 40)
  lamp.userData.baseIntensity = 40
  const studio = new PointLight('#ffffff', 3)
  scene.add(lamp, studio)
  scene.environmentIntensity = 0.7
  scene.fog = new Fog('#808080', 1, 100)
  return { scene, lamp, studio }
}

test('under another sky k = 1: lights back at their data, the sky light and fog untouched', () => {
  const { scene, lamp, studio } = lampScene()
  lamp.intensity = 123
  applyExposure(scene, 1)
  assert.equal(lamp.intensity, 40)
  assert.equal(studio.intensity, 3, 'a light without baseIntensity (the studio light) is not one of the scene’s')
  assert.equal(scene.environmentIntensity, 0.7, 'the sky light is the look’s to set')
  assert.equal(scene.fog!.color.getHexString(), '808080')
  assert.equal(preExposure.value, 1)
})

test('under a physical sky k scales the lights, the sky light by k / s_env and the fog by E / π · k', () => {
  const { scene, lamp, studio } = lampScene()
  const k = exposureScale(15)
  scene.userData.physical = { skyLightScale: 1e-4, fogColor: new Color(0.5, 0.5, 0.5), lux: 100_000 }
  applyExposure(scene, k)
  assert.equal(lamp.intensity, 40 * k)
  assert.equal(studio.intensity, 3)
  assert.ok(Math.abs(scene.environmentIntensity - k / 1e-4) < 1e-12)
  const fog = 0.5 * (100_000 / Math.PI) * k
  assert.ok(Math.abs(scene.fog!.color.r - fog) < 1e-9, `${scene.fog!.color.r} vs ${fog}`)
  assert.equal(preExposure.value, k)
  // Again at another k: from the data, not from what was written.
  applyExposure(scene, 2 * k)
  assert.equal(lamp.intensity, 80 * k)
  assert.ok(Math.abs(scene.fog!.color.r - 2 * fog) < 1e-9)
  applyExposure(scene, 1)
})

// Epic revision v5: the automatic exposure meters the scene's light and
// darkens the night by a few stops only (design「物理单位与曝光」「夜间压暗曲线」).

test('the night darkening is a straight line from a sunny noon to a moonless night, flat beyond them', () => {
  for (const [evm, d] of [[20, 0], [15, 0], [2, 1.75], [-11, 3.5], [-12, 3.5]] as const) {
    assert.ok(Math.abs(nightDarkening(evm) - d) < 1e-12, `EV ${evm}: ${nightDarkening(evm)}, want ${d}`)
  }
  let last = nightDarkening(20)
  for (let evm = 20; evm >= -12; evm -= 0.01) {
    const d = nightDarkening(evm)
    assert.ok(d >= last - 1e-12 && d - last < 0.01, `EV ${evm.toFixed(2)}: ${last} → ${d}`)
    last = d
  }
})

test('the automatic exposure is the incident meter plus the darkening, less the compensation', () => {
  const noon = autoEV100(100_000, 0)
  assert.ok(Math.abs(noon - environmentEV(100_000)) < 1e-12, 'noon is metered as it is')
  assert.ok(Math.abs(autoEV100(100_000, 1) - (noon - 1)) < 1e-12, '+1 is a stop brighter')
  // Mid grey lit like the origin shows D stops under noon's: the full moon of 2026-10-26 in Beijing, the new moon's constant term.
  const under = (lux: number) => autoEV100(lux, 0) - environmentEV(lux)
  assert.ok(under(0.221) >= 2 && under(0.221) <= 3, `full moon ${under(0.221).toFixed(2)} stops`)
  assert.ok(under(1.013e-3) >= 3 && under(1.013e-3) <= 4, `new moon ${under(1.013e-3).toFixed(2)} stops`)
  assert.equal(viewerEV100({ mode: 'manual', ev100: -3 }, 0.221), -3, 'manual keeps its EV100')
  assert.equal(viewerEV100({ mode: 'auto', compensation: -0.5 }, 0.221), autoEV100(0.221, -0.5))
})

test('a frame under a physical sky is exposed by its viewer: a camera, or else the default camera; the night look follows the scene', () => {
  const physical = { environment: { sky: { kind: 'physical', night: { night_look: 0.5 } } }, render: { exposure_ev: 2, exposure_ev100: 12, exposure_mode: 'auto', exposure_compensation: 1 } } as unknown as Parameters<typeof frameExposure>[0]
  const lux = 0.221
  const look = nightLookWeight(lux, 0.5)
  assert.ok(look > 0.49 && look <= 0.5, `a full moon's look at night_look 0.5: ${look}`)
  assert.deepEqual(frameExposure(physical, true, undefined, lux), { exposure: 1, preExposure: exposureScale(autoEV100(lux, 1)), nightLook: look }, 'the default camera, automatic')
  const manual = { ...physical, render: { ...physical.render, exposure_mode: 'manual' } } as typeof physical
  assert.deepEqual(frameExposure(manual, true, undefined, lux), { exposure: 1, preExposure: exposureScale(12), nightLook: look }, 'the default camera, manual')
  assert.deepEqual(frameExposure(manual, true, { mode: 'auto', compensation: 0 }, lux), { exposure: 1, preExposure: exposureScale(autoEV100(lux, 0)), nightLook: look }, 'a scene camera, automatic')
  assert.deepEqual(frameExposure(physical, true, { mode: 'manual', ev100: -3 }, lux), { exposure: 1, preExposure: exposureScale(-3), nightLook: look }, 'a scene camera, manual')
  assert.deepEqual(frameExposure(physical, false, undefined, lux), { exposure: 1, preExposure: 1, nightLook: 0 }, 'clay')
  const procedural = { ...physical, environment: { sky: { kind: 'procedural' } } } as typeof physical
  assert.deepEqual(frameExposure(procedural, true, { mode: 'auto', compensation: 0 }, lux), { exposure: 4, preExposure: 1, nightLook: 0 }, 'another sky: the compensation of old')
})

// Epic revision v5: the night look turns the picture blue and paler as night falls, keeping its brightness.

test('the night look is none until the golden hour, full from a full moon on, times night_look', () => {
  const at = (ev: number) => (250 * 2 ** ev) / 100
  assert.equal(nightLookWeight(at(4), 1), 0)
  assert.equal(nightLookWeight(at(15), 1), 0)
  assert.equal(nightLookWeight(at(-4), 1), 1)
  assert.equal(nightLookWeight(at(-11), 0.5), 0.5)
  assert.equal(nightLookWeight(at(-11), 0), 0)
  const mid = nightLookWeight(at(0), 1)
  assert.ok(Math.abs(mid - 0.5) < 1e-9, `half way at EV 0: ${mid}`)
})

test('the night look keeps the luminance and moves the colour toward blue', () => {
  assert.ok(Math.abs(luminance(NIGHT_TINT) - 1) < 1e-9)
  assert.ok(NIGHT_TINT[2] > NIGHT_TINT[0], 'blue over red')
  const warm: Vec3 = [0.6, 0.5, 0.4]
  const looked = nightLookColor(warm, 1)
  assert.ok(Math.abs(luminance(looked) - luminance(warm)) < 1e-9, 'the same luminance')
  assert.ok(looked[2] / looked[0] > warm[2] / warm[0], 'bluer')
  assert.deepEqual(nightLookColor(warm, 0), warm, 'none at weight 0')
})
