import test from 'node:test'
import assert from 'node:assert/strict'
import {
  activeLevels,
  alphaOf,
  cellOf,
  coveredOf,
  emod,
  fadeSpans,
  GRID,
  isDue,
  levelsFor,
  maturityOf,
  offsetsFor,
  originCell,
  periodsFor,
  PICTURE,
  reachOf,
  slotOf,
  spacingOf,
  viewportSettleFrames,
  VIEWPORT,
} from '../src/creative-scene/render/probes/cascade.ts'
import { bordersOf, decodeOct, encodeOct, texelCentre } from '../src/creative-scene/render/probes/octahedral.ts'

// scene-probe-gi (S2): the probe cascade's pure parts (design「级联的放置与轮转」「更新」) and the octahedral maps.

test('levels are 0.25 m apart at the finest and double each level; a box reaches 3/4 of its width ahead', () => {
  assert.deepEqual([0, 1, 2, 3, 4].map(spacingOf), [0.25, 0.5, 1, 2, 4])
  assert.deepEqual([0, 1, 2, 3, 4].map(reachOf), [3, 6, 12, 24, 48], 'the design’s coverage table, ahead')
  assert.deepEqual([2, 3, 4].map(coveredOf), [10.5, 21, 42], 'covered whole: the cell and a half the edge fades over kept')
})

test('a distance takes three levels within 10.5 m, a fourth to 21 m and a fifth beyond (owner 2026-10-26)', () => {
  assert.equal(levelsFor(0), 3)
  assert.equal(levelsFor(8), 3, 'the reference scenes, within 10 m')
  assert.equal(levelsFor(10.5), 3)
  assert.equal(levelsFor(10.51), 4)
  assert.equal(levelsFor(21), 4)
  assert.equal(levelsFor(21.01), 5)
  assert.equal(levelsFor(36), 5, 'the 30 × 20 m hall')
  assert.equal(levelsFor(500), 5, 'never more than five')
  assert.equal(levelsFor(Number.NaN), 3)
})

test('a level once on stays down to 3/4 of where it came on, and never past what is allocated', () => {
  assert.equal(activeLevels(5, 11, 3), 4, 'on past 10.5 m')
  assert.equal(activeLevels(5, 8, 4), 4, 'kept at 8 m: above 7.875 m')
  assert.equal(activeLevels(5, 7.8, 4), 3, 'off below it')
  assert.equal(activeLevels(5, 16, 5), 5, 'the fifth, on past 21 m, kept down to 15.75 m')
  assert.equal(activeLevels(5, 15.7, 5), 4, 'and below that down to the fourth')
  assert.equal(activeLevels(3, 30, 3), 3, 'no more than allocated: a camera pulled back past them makes a volume with more (volume.ts outgrown)')
})

test('the coarsest level fades over the room its box has past the scene, the camera’s surroundings kept', () => {
  // Level 2 (1 m cells), its box [0, 16) on each axis; the camera at 4 cells along x, 8 along y and z.
  const eye = [4, 8, 8] as const
  const scene = { min: [2, 3, 6], max: [10, 13, 9.2] } as const
  const { lo, hi } = fadeSpans(2, [0, 0, 0], scene, eye, false)
  assert.deepEqual(lo, [1.3333333333333333, 2.5, 2.6666666666666665], 'behind the camera a third of its 4 cells; below, the room before the scene less half a cell')
  assert.deepEqual(hi, [4, 2.5, 2.6666666666666665], 'ahead the most; up to the scene 3 cells, less half; or a third of the camera’s 8')
  assert.deepEqual(fadeSpans(2, [0, 0, 0], { min: [-3, 0, 0], max: [17, 16, 16] }, eye, false), { lo: [1, 1, 1], hi: [1, 1, 1] }, 'a scene out of every face: the one cell, coarser levels taking over')
  assert.deepEqual(fadeSpans(4, [0, 0, 0], { min: [-3, 0, 0], max: [17, 16, 16] }, [1, 2, 2].map((c) => c * 16) as unknown as readonly [number, number, number], true).hi, [4, 2.6666666666666665, 2.6666666666666665], 'five levels on, no coarser to come: a third of the camera’s distance (4 m cells)')
  assert.deepEqual(fadeSpans(2, [0, 0, 0], null, eye, false).hi, [4, 2.6666666666666665, 2.6666666666666665], 'no scene: the camera’s distance alone')
})

test('a box sits 3/4 ahead of the camera, 1/4 behind, centred up and down, on its own grid', () => {
  // The view's world is Z up (space.ts): the camera 1.4 m high.
  const eye = [0.1, 2.1, 1.4] as const
  // Looking along −Y (the editor's front): level 0 is 4 m wide, its centre 1 m ahead.
  assert.deepEqual(originCell(0, eye, [0, -1, 0]), [-8, -4, -3])
  // Cells are spacing-sized: the box's lowest corner, in metres, a multiple of 0.25.
  const [x, y, z] = originCell(0, eye, [0, -1, 0]).map((c) => c * spacingOf(0))
  assert.ok(x <= 0.1 - 2 && x > 0.1 - 2 - 0.25 && y <= 2.1 - 1 - 2 && y > 2.1 - 1 - 2 - 0.25 && z <= 1.4 - 2, `${x}, ${y}, ${z}`)
  // Looking straight down, the horizontal part is zero: centred on the camera.
  assert.deepEqual(originCell(2, eye, [0, 0, -1]), [-8, -6, -7])
  // Level 2, 16 m wide, looking along +X tilted down: the horizontal part, not normalised, sets how far ahead; the
  // box stays centred on the camera's height.
  assert.deepEqual(originCell(2, [0, 0, 0], [0.5, 0, -0.866]), [-6, -8, -8])
  assert.deepEqual(originCell(2, [0, 0, 0], [0, 0.5, -0.866]), [-8, -6, -8])
})

test('slots wrap each axis modulo 16, negative cells included (Euclidean, not WGSL’s %)', () => {
  assert.equal(emod(-1, 16), 15)
  assert.equal(emod(-16, 16), 0)
  assert.equal(emod(-17, 16), 15)
  assert.equal(emod(17, 16), 1)
  assert.equal(slotOf([0, 0, 0]), 0)
  assert.equal(slotOf([-1, 0, 0]), 15)
  assert.equal(slotOf([0, -1, 0]), 15 * 16)
  assert.equal(slotOf([0, 0, -1]), 15 * 256)
  assert.equal(slotOf([16, 32, -48]), 0, 'a whole box over is the same slot')
  // Every slot of a box stands for a cell inside it, each once, wherever the box is.
  for (const origin of [[0, 0, 0], [-8, -3, -4], [-23, 5, -17]] as const) {
    const seen = new Set<string>()
    for (let s = 0; s < GRID ** 3; s++) {
      const cell = cellOf(s, origin)
      assert.equal(slotOf(cell), s)
      cell.forEach((c, i) => assert.ok(c >= origin[i] && c < origin[i] + GRID, `slot ${s} in the box at ${origin}: ${cell}`))
      seen.add(cell.join())
    }
    assert.equal(seen.size, GRID ** 3)
  }
})

test('a box moved by one cell keeps the slots of the cells it still covers and resets one face of them', () => {
  const before = [-8, -3, -4] as const
  const after = [-7, -3, -4] as const
  let kept = 0
  for (let s = 0; s < GRID ** 3; s++) if (cellOf(s, before).join() === cellOf(s, after).join()) kept++
  assert.equal(kept, GRID ** 3 - GRID ** 2, 'all but the face it left behind')
})

test('the viewport updates levels at a period set by the top level’s roots, staggered so each frame carries about the same', () => {
  assert.deepEqual(periodsFor(1), [1, 1, 2, 2, 4], 'the reference scenes')
  assert.deepEqual(periodsFor(256), [1, 1, 2, 2, 4])
  assert.deepEqual(periodsFor(257), [2, 2, 4, 4, 8])
  assert.deepEqual(periodsFor(1024), [2, 2, 4, 4, 8])
  assert.deepEqual(periodsFor(2000), [4, 4, 8, 8, 16], 'the hall')
  for (const periods of [periodsFor(1), periodsFor(2000)]) {
    const per = periods.map((p, k) => Array.from({ length: 64 }, (_, f) => f).filter((f) => isDue(k, f, periods)).length)
    assert.deepEqual(per, periods.map((p) => 64 / p), 'each level every P frames')
    const loads = Array.from({ length: 16 }, (_, f) => periods.filter((_, k) => isDue(k, f, periods)).length)
    assert.ok(Math.max(...loads) - Math.min(...loads) <= 1, `levels a frame: ${loads}`)
  }
  assert.deepEqual(offsetsFor(periodsFor(2000)), [0, 1, 2, 3, 6])
  assert.equal(isDue(0, -3, [2, 2, 4, 4, 8]), isDue(0, 1, [2, 2, 4, 4, 8]), 'negative frames wrap too')
})

test('the history weighs nothing for A0 updates after an edit, then rises from a half to alpha max', () => {
  assert.equal(alphaOf(0, VIEWPORT), 0)
  assert.equal(alphaOf(11, VIEWPORT), 0)
  assert.equal(alphaOf(12, VIEWPORT), 0.5)
  assert.equal(alphaOf(13, VIEWPORT), 1 - 1 / 3)
  assert.ok(alphaOf(24, VIEWPORT) < 0.93 && alphaOf(25, VIEWPORT) === 0.93, 'at 0.93 from A0 + 13 on')
  assert.equal(alphaOf(10_000, VIEWPORT), 0.93)
  // A picture: two runs of 32, the second a plain mean of its own.
  assert.equal(alphaOf(31, PICTURE), 0)
  assert.equal(alphaOf(32, PICTURE), 0.5)
  assert.equal(alphaOf(63, PICTURE), 1 - 1 / 33, 'the 32nd of the second run weighs this frame 1/33')
})

test('the viewport settles once the slowest level drawn has caught up and settled; without the layer, as before (design「收敛与重绘」)', () => {
  assert.equal(viewportSettleFrames('realtime_gi', true, 3, periodsFor(40)), 52, 'the reference scenes: three levels, every other frame at the slowest')
  assert.equal(viewportSettleFrames('realtime_gi', true, 4, periodsFor(40)), 52)
  assert.equal(viewportSettleFrames('realtime_gi', true, 5, periodsFor(2000)), 416, 'the hall, recorded only (owner 2026-10-26)')
  assert.equal(viewportSettleFrames('realtime_gi', false, 3, periodsFor(40)), 12, 'the layer not built: convergeFrames')
  assert.equal(viewportSettleFrames('workbench', false, 3, periodsFor(40)), 1)
})

test('a probe’s data fades in over its first A0 updates, whatever its edit age', () => {
  assert.equal(maturityOf(0, VIEWPORT), 0)
  assert.equal(maturityOf(6, VIEWPORT), 0.5)
  assert.equal(maturityOf(12, VIEWPORT), 1)
  assert.equal(maturityOf(400, VIEWPORT), 1)
  assert.equal(maturityOf(16, PICTURE), 0.5)
})

// --- octahedral maps ------------------------------------------------------------

const unit = (v: number[]) => {
  const l = Math.hypot(...v)
  return v.map((x) => x / l) as [number, number, number]
}
const close = (a: readonly number[], b: readonly number[], eps = 1e-12) => a.every((x, i) => Math.abs(x - b[i]) <= eps) || assert.fail(`${a} ≠ ${b}`)

test('a direction encodes to a point in [-1, 1]² and decodes back, the lower hemisphere folded to the corners', () => {
  for (let k = 0; k < 2000; k++) {
    // Fibonacci points on the sphere, and the axes and the folds themselves.
    const y = 1 - (2 * (k + 0.5)) / 2000
    const r = Math.sqrt(1 - y * y)
    const a = k * Math.PI * (3 - Math.sqrt(5))
    const dir = unit([r * Math.cos(a), r * Math.sin(a), y])
    const e = encodeOct(dir)
    assert.ok(Math.abs(e[0]) <= 1 && Math.abs(e[1]) <= 1, `${e}`)
    close(decodeOct(e), dir, 1e-12)
  }
  for (const dir of [[0, 0, 1], [0, 0, -1], [1, 0, 0], [-1, 0, 0], [0, 1, 0], [0, -1, 0], [1, 1, 0], [-1, 0, -1]]) close(decodeOct(encodeOct(unit(dir))), unit(dir))
  close(encodeOct([0, 0, 1]), [0, 0])
  close(encodeOct([0, 0, -1]), [1, 1], 0)
  assert.ok(Math.abs(encodeOct(unit([0.3, -0.2, -0.9]))[0]) + Math.abs(encodeOct(unit([0.3, -0.2, -0.9]))[1]) > 1, 'below the equator: outside the inner diamond')
})

/** A point beyond a tile's edge, folded back in as the octahedral map continues across it. */
function fold([u, v]: [number, number]): [number, number] {
  if (v < -1) [u, v] = [-u, -2 - v]
  if (v > 1) [u, v] = [-u, 2 - v]
  if (u < -1) [u, v] = [-2 - u, -v]
  if (u > 1) [u, v] = [2 - u, -v]
  return [u, v]
}

test('a tile’s border copies the interior texel across the fold, each border texel from one source', () => {
  for (const n of [8, 16]) {
    const sources = new Map<string, string>()
    for (let i = 0; i < n; i++) {
      for (let j = 0; j < n; j++) {
        for (const [bi, bj] of bordersOf(i, j, n)) {
          assert.ok(bi === -1 || bi === n || bj === -1 || bj === n, `${bi}, ${bj} on the border`)
          assert.ok(!sources.has(`${bi},${bj}`), `${bi}, ${bj} written twice`)
          sources.set(`${bi},${bj}`, `${i},${j}`)
          // The border texel's centre, folded back, is the source's: the same direction.
          close(fold(texelCentre(bi, bj, n)), texelCentre(i, j, n), 1e-12)
          close(decodeOct(fold(texelCentre(bi, bj, n))), decodeOct(texelCentre(i, j, n)), 1e-12)
        }
      }
    }
    assert.equal(sources.size, 4 * n + 4, 'every border texel, corners included')
  }
  assert.deepEqual(bordersOf(3, 3, 8), [], 'an inner texel writes no border')
})
