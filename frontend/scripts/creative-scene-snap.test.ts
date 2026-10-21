import test from 'node:test'
import assert from 'node:assert/strict'
import { snap, type Hit, type Vec3 } from '../src/creative-scene/placement/snap.ts'

// C6 design「吸附」: each mount on suitable and unsuitable faces, wall facing, and the fallbacks.

const up: Vec3 = [0, 1, 0]
const down: Vec3 = [0, -1, 0]
const box = { min: [-0.5, 0, -0.5], max: [0.5, 2, 0.5] }
const centred = { min: [-0.5, -1, -0.5], max: [0.5, 1, 0.5] }
// Looking down at 45° from (0, 4, 4) towards the origin.
const ray = { origin: [0, 4, 4] as Vec3, direction: [0, -Math.SQRT1_2, -Math.SQRT1_2] as Vec3 }
const fallback: Vec3 = [3, 0, -2]
const near = (a: readonly number[], b: readonly number[]) => a.every((v, i) => Math.abs(v - b[i]!) < 1e-9)
const assertPlaced = (got: { position: number[]; rotation: number[] }, position: number[], rotation = [0, 0, 0, 1]) => {
  assert.ok(near(got.position, position), `position ${got.position} ≠ ${position}`)
  assert.ok(near(got.rotation, rotation), `rotation ${got.rotation} ≠ ${rotation}`)
}

const top: Hit = { point: [0.2, 1, 0.1], normal: up }
const wall: Hit = { point: [0, 1.5, -1], normal: [0, 0, 1] }
const ceiling: Hit = { point: [0, 3, 0], normal: down }

test('a floor model stands upright on the first face that looks up', () => {
  assertPlaced(snap({ hits: [wall, top], ray, mount: 'floor', bounds: box, fallback }), [0.2, 1, 0.1])
  // A slope of 30° still counts as up; a 60° one does not.
  const slope: Hit = { point: [1, 0.5, 0], normal: [Math.sin(Math.PI / 6), Math.cos(Math.PI / 6), 0] }
  assertPlaced(snap({ hits: [slope], ray, mount: 'floor', bounds: box, fallback }), [1, 0.5, 0])
  const steep: Hit = { point: [1, 0.5, 0], normal: [Math.sin(Math.PI / 3), Math.cos(Math.PI / 3), 0] }
  assertPlaced(snap({ hits: [steep], ray, mount: 'floor', bounds: box, fallback }), [0, 0, 0], undefined)
})

test('a wall model backs onto the first vertical face and faces out', () => {
  assertPlaced(snap({ hits: [top, wall], ray, mount: 'wall', bounds: box, fallback }), [0, 1.5, -1])
  // A wall facing +X turns the front (+Z) a quarter turn to +X.
  const east = snap({ hits: [{ point: [-2, 1, 0], normal: [1, 0, 0] }], ray, mount: 'wall', bounds: box, fallback })
  assertPlaced(east, [-2, 1, 0], [0, Math.sin(Math.PI / 4), 0, Math.cos(Math.PI / 4)])
  // Only the horizontal part of a leaning wall counts.
  const leaning = snap({ hits: [{ point: [0, 1, 2], normal: [0, 0.5, -Math.sqrt(0.75)] }], ray, mount: 'wall', bounds: box, fallback })
  assertPlaced(leaning, [0, 1, 2], [0, 1, 0, 0])
})

test('a ceiling model hangs from the first face that looks down', () => {
  assertPlaced(snap({ hits: [top, ceiling], ray, mount: 'ceiling', bounds: box, fallback }), [0, 3, 0])
})

test('a free model rests its bounds\' bottom on a face that looks up', () => {
  assertPlaced(snap({ hits: [top], ray, mount: 'free', bounds: centred, fallback }), [0.2, 2, 0.1])
})

test('without a suitable face it is placed like a free model: on a face that looks up, else on the ground', () => {
  // No ceiling in the scene: a ceiling model stands on the table instead of being buried in it.
  assertPlaced(snap({ hits: [top], ray, mount: 'ceiling', bounds: box, fallback }), [0.2, 1, 0.1])
  assertPlaced(snap({ hits: [ceiling], ray, mount: 'floor', bounds: box, fallback }), [0, 0, 0], undefined)
  assertPlaced(snap({ hits: [], ray, mount: 'free', bounds: centred, fallback }), [0, 1, 0])
  assertPlaced(snap({ hits: [ceiling], ray, mount: 'wall', bounds: box, fallback }), [0, 0, 0])
  // Off-centre bounds: their bottom centre, not the model's origin, goes on the point.
  assertPlaced(snap({ hits: [], ray, mount: 'floor', bounds: { min: [0, 0.2, 0], max: [1, 1, 2] }, fallback }), [-0.5, -0.2, -1])
})

test('dragging towards the sky lands at the view\'s ground point', () => {
  const sky = { origin: [0, 2, 5] as Vec3, direction: [0, 0.6, -0.8] as Vec3 }
  assertPlaced(snap({ hits: [], ray: sky, mount: 'floor', bounds: box, fallback }), [3, 0, -2])
  assertPlaced(snap({ hits: [], ray: null, mount: 'free', bounds: centred, fallback }), [3, 1, -2])
})
