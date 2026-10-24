// The spike page's scenes (scene-trace-spike design「穿刺页面」): a reference scene drawn from its JSON with the
// editor's own geometry and materials, or the stress scene from its generator. Lit simply (a sun and a sky
// fill): the spike's raster is the reference its rays are checked against, under the same lights, not the
// product's look.
import { BufferAttribute, BufferGeometry, Color, DirectionalLight, HemisphereLight, Mesh, PerspectiveCamera, PointLight, Quaternion, Scene, SpotLight, Vector3 } from 'three'
import { MeshStandardNodeMaterial } from 'three/webgpu'
import { primitiveGeometry } from '../../src/creative-scene/builtins/geometry.ts'
import { verticalFov } from '../../src/creative-scene/cameraOptics.ts'
import { makeMaterial } from '../../src/creative-scene/materials/instances.ts'
import { catalogMaterial, primitiveDefault } from '../../src/creative-scene/materials/resolve.ts'
import { sunVector } from '../../src/creative-scene/world/astro.ts'
import { counts, generate } from '../../scripts/fixtures/scene-stress/generate.mjs'

export type Built = { scene: Scene; camera: PerspectiveCamera; meshes: Mesh[]; triangles: number; buildMs: number }

/** The canvas's shape: 3:2, as the exports. */
export const ASPECT = 1.5

type Doc = {
  environment: { sky: { kind: string; sun_elevation_deg?: number; sun_azimuth_deg?: number } }
  objects: {
    kind: string
    transform: { position: number[]; rotation: number[]; scale: number[] }
    primitive?: { builtin: string; params: Record<string, unknown> }
    material_slots?: Record<string, { preset: string; params: Record<string, unknown> }>
    light?: { type: string; color: string; intensity: number; angle?: number; penumbra?: number; decay?: number; distance?: number }
    camera?: { focal_mm: number }
  }[]
}

function lights(scene: Scene, sun: Vector3) {
  scene.add(new HemisphereLight(new Color('#c6d4e8'), new Color('#7a7468'), 0.6))
  const key = new DirectionalLight(new Color('#fff6ea'), 3)
  key.position.copy(sun).multiplyScalar(50)
  scene.add(key, key.target)
}

function placed<T extends { position: Vector3; quaternion: Quaternion; scale: Vector3 }>(o: T, t: Doc['objects'][number]['transform']): T {
  o.position.fromArray(t.position)
  o.quaternion.fromArray(t.rotation)
  o.scale.fromArray(t.scale)
  return o
}

/** A reference scene (scripts/fixtures/scene-reference) as the editor's geometry and materials draw it. */
export async function buildReference(name: string): Promise<Built> {
  const started = performance.now()
  const doc = (await (await fetch(`/scripts/fixtures/scene-reference/${name}.json`)).json()) as Doc
  const scene = new Scene()
  const sky = doc.environment.sky
  const sunny = sky.kind === 'procedural' || sky.kind === 'physical'
  lights(scene, sunny ? sunVector(sky.sun_elevation_deg ?? 35, sky.sun_azimuth_deg ?? 135) : new Vector3(0.3, 1, 0.2).normalize())
  scene.background = new Color(sunny ? '#9fb4cc' : '#0b0c0e')
  const meshes: Mesh[] = []
  let camera: PerspectiveCamera | null = null
  let triangles = 0
  for (const o of doc.objects) {
    if (o.kind === 'primitive' && o.primitive) {
      const geometry = primitiveGeometry(o.primitive.builtin, o.primitive.params)
      if (!geometry) continue
      const slot = o.material_slots?.main
      const slotMaterial = (slot && catalogMaterial(slot.preset, slot.params)) || primitiveDefault
      const material = makeMaterial(slotMaterial, false)
      const mesh = placed(new Mesh(geometry, material), o.transform)
      // The slot's material, for the hit points' material table (group 3).
      mesh.userData.slot = slotMaterial
      triangles += (geometry.index ? geometry.index.count : geometry.attributes.position.count) / 3
      meshes.push(mesh)
      scene.add(mesh)
    } else if (o.kind === 'light' && o.light) {
      const l = o.light
      const light = l.type === 'spot' ? new SpotLight(l.color, l.intensity, l.distance ?? 0, l.angle, l.penumbra, l.decay) : new PointLight(l.color, l.intensity, l.distance ?? 0, l.decay)
      placed(light, o.transform)
      if (light instanceof SpotLight) {
        // A spot looks down its -Z, as the editor's (objects/lights.ts).
        light.target.position.copy(light.position).add(new Vector3(0, 0, -1).applyQuaternion(light.quaternion))
        scene.add(light.target)
      }
      scene.add(light)
    } else if (o.kind === 'camera' && o.camera && !camera) {
      camera = placed(new PerspectiveCamera(verticalFov(o.camera.focal_mm, [36, 24], ASPECT), ASPECT, 0.05, 500), o.transform)
    }
  }
  if (!camera) throw new Error(`${name}: no camera`)
  scene.updateMatrixWorld(true)
  return { scene, camera, meshes, triangles, buildMs: performance.now() - started }
}

type StressSpec = Parameters<typeof generate>[0] & { camera: { position: number[]; target: number[]; focal_mm: number } }

/** The stress scene (scripts/fixtures/scene-stress): every geometry once, drawn by every instance of it. */
export async function buildStress(): Promise<Built & { geometries: number; instances: number; generateMs: number }> {
  const started = performance.now()
  const spec = (await (await fetch('/scripts/fixtures/scene-stress/spec.json')).json()) as StressSpec
  const generated = generate(spec)
  const generateMs = performance.now() - started
  const tint: Record<string, string> = { box: '#cfcac0', blob: '#b8a48a', knot: '#8aa0b8', column: '#d8d2c4', terrain: '#8f9a78' }
  const materials = new Map<string, MeshStandardNodeMaterial>()
  const geometries = new Map<string, BufferGeometry>()
  for (const g of generated.geometries) {
    const geometry = new BufferGeometry()
    geometry.setAttribute('position', new BufferAttribute(g.positions, 3))
    geometry.setAttribute('normal', new BufferAttribute(g.normals, 3))
    geometry.setIndex(new BufferAttribute(g.indices, 1))
    geometry.computeBoundingSphere()
    geometries.set(g.id, geometry)
    if (!materials.has(g.type)) materials.set(g.type, new MeshStandardNodeMaterial({ color: tint[g.type], roughness: 0.7 }))
  }
  const scene = new Scene()
  scene.background = new Color('#9fb4cc')
  lights(scene, sunVector(40, 150))
  const meshes: Mesh[] = []
  for (const inst of generated.instances) {
    const g = generated.geometries.find((x) => x.id === inst.geometry)!
    const mesh = new Mesh(geometries.get(inst.geometry), materials.get(g.type))
    mesh.matrixAutoUpdate = false
    mesh.matrix.fromArray(inst.matrix)
    meshes.push(mesh)
    scene.add(mesh)
  }
  const camera = new PerspectiveCamera(verticalFov(spec.camera.focal_mm, [36, 24], ASPECT), ASPECT, 0.05, 500)
  camera.position.fromArray(spec.camera.position)
  camera.lookAt(new Vector3().fromArray(spec.camera.target))
  scene.updateMatrixWorld(true)
  const c = counts(spec)
  return { scene, camera, meshes, triangles: c.triangles, geometries: c.geometries, instances: c.instances, buildMs: performance.now() - started, generateMs }
}
