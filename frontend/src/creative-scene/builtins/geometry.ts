import {
  BoxGeometry,
  BufferGeometry,
  CylinderGeometry,
  ExtrudeGeometry,
  PlaneGeometry,
  Shape,
  SphereGeometry,
} from 'three'
import { mergeGeometries } from 'three/addons/utils/BufferGeometryUtils.js'
import { builtinSpec, paramNumber, type BuiltinSpec } from './catalog.ts'

// Trusted geometry for the built-in primitives. Units are metres, Y is up and
// the front faces +Z. A floor mount has its origin at the bottom centre, a
// wall mount at the bottom centre of its back face.
type Build = (p: (key: string) => number) => BufferGeometry

const build: Record<string, Build> = {
  'ground@1': (p) => {
    const g = new PlaneGeometry(p('size'), p('size'))
    g.rotateX(-Math.PI / 2)
    return g
  },
  'box@1': (p) => {
    const g = new BoxGeometry(p('width'), p('height'), p('depth'))
    g.translate(0, p('height') / 2, 0)
    return g
  },
  'cylinder@1': (p) => {
    const g = new CylinderGeometry(
      p('radius_top'),
      p('radius_bottom'),
      p('height'),
      Math.round(p('radial_segments')),
    )
    g.translate(0, p('height') / 2, 0)
    return g
  },
  'sphere@1': (p) => {
    const segments = Math.round(p('segments'))
    const g = new SphereGeometry(
      p('radius'),
      segments,
      Math.max(4, Math.round(segments / 2)),
    )
    g.translate(0, p('radius'), 0)
    return g
  },
  'plane@1': (p) => {
    const g = new PlaneGeometry(p('width'), p('height'))
    g.translate(0, p('height') / 2, 0)
    return g
  },
  // Steps rise away from the viewer: the lowest step is at the front (+Z).
  'stairs@1': (p) => {
    const count = Math.round(p('step_count'))
    const depth = count * p('step_depth')
    const steps: BufferGeometry[] = []
    for (let i = 0; i < count; i++) {
      const h = (i + 1) * p('step_height')
      const step = new BoxGeometry(p('width'), h, p('step_depth'))
      step.translate(0, h / 2, depth / 2 - (i + 0.5) * p('step_depth'))
      steps.push(step)
    }
    const merged = mergeGeometries(steps) ?? new BufferGeometry()
    for (const s of steps) s.dispose()
    return merged
  },
  // A wall with a round-headed opening. An opening larger than the wall
  // shrinks to fit; the server only checks each parameter's own range.
  'arch@1': (p) => {
    const width = p('width')
    const height = p('height')
    const openingWidth = Math.min(p('opening_width'), width * 0.9)
    const radius = Math.min(openingWidth / 2, height * 0.95)
    const openingHeight = Math.min(
      Math.max(p('opening_height'), radius),
      height * 0.95,
    )
    // The opening reaches the floor, so it is part of the outline rather than
    // a hole that would touch the edge.
    const spring = Math.max(0, openingHeight - radius)
    const outline = new Shape()
    outline.moveTo(-width / 2, 0)
    outline.lineTo(-radius, 0)
    outline.lineTo(-radius, spring)
    outline.absarc(0, spring, radius, Math.PI, 0, true)
    outline.lineTo(radius, 0)
    outline.lineTo(width / 2, 0)
    outline.lineTo(width / 2, height)
    outline.lineTo(-width / 2, height)
    outline.closePath()
    const g = new ExtrudeGeometry(outline, {
      depth: p('depth'),
      bevelEnabled: false,
      curveSegments: 24,
    })
    g.translate(0, 0, -p('depth') / 2)
    return g
  },
}

/** Every catalog entry must have a builder; a test keeps the two in step. */
export const geometryBuilders: Readonly<Record<string, Build>> = build

export function primitiveGeometry(
  builtin: string,
  params: Record<string, unknown> | undefined,
): BufferGeometry | null {
  const spec: BuiltinSpec | undefined = builtinSpec(builtin)
  const make = build[builtin]
  if (!spec || !make) return null
  return make((key) => paramNumber(params, spec, key))
}
