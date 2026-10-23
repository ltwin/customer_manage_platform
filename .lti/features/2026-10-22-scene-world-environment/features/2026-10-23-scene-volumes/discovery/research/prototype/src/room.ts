// @ts-nocheck — spike: the test scenes. Z up, as the product's viewport.
import { BoxGeometry, Color, DirectionalLight, HemisphereLight, Mesh, MeshStandardMaterial, PerspectiveCamera, PlaneGeometry, PointLight, Scene, SphereGeometry, SpotLight, CylinderGeometry } from 'three/webgpu'
import { cascadeSunShadow } from '/@fs/Users/samson/workspace/my_project/customer_manage_platform/frontend/src/creative-scene/render/sunShadow.ts'

export type Lights = { sun: DirectionalLight; spot: SpotLight | null; point: PointLight | null }

function box(scene: Scene, size: number[], at: number[], color = 0xb8b4ac) {
  const m = new Mesh(new BoxGeometry(...size), new MeshStandardMaterial({ color, roughness: 0.85 }))
  m.position.set(...at)
  m.castShadow = true
  m.receiveShadow = true
  scene.add(m)
  return m
}

/**
 * A 6 × 6 × 3 m room, the sun low through a window in its east wall (a cross
 * of mullions), a spot from the ceiling, a lamp; a yard outside.
 */
export function room({ sunElev = 22, sunAz = 15, spot = true, point = true, outside = false, view = 'window' } = {}) {
  const scene = new Scene()
  scene.background = new Color(0x8fb4e0)
  const ground = new Mesh(new PlaneGeometry(200, 200), new MeshStandardMaterial({ color: 0x9a9488, roughness: 0.95 }))
  ground.receiveShadow = true
  scene.add(ground)
  const t = 0.2
  // Walls: west, north, south solid; east with a 2 × 1.4 m window, sill at 0.8 m.
  box(scene, [t, 6 + 2 * t, 3], [-3 - t / 2, 0, 1.5])
  box(scene, [6, t, 3], [0, 3 + t / 2, 1.5])
  box(scene, [6, t, 3], [0, -3 - t / 2, 1.5])
  box(scene, [t, 2, 3], [3 + t / 2, -2, 1.5])
  box(scene, [t, 2, 3], [3 + t / 2, 2, 1.5])
  box(scene, [t, 2, 0.8], [3 + t / 2, 0, 0.4])
  box(scene, [t, 2, 0.8], [3 + t / 2, 0, 2.6])
  box(scene, [t, 0.08, 1.4], [3 + t / 2, 0, 1.5], 0x5a5048)
  box(scene, [t, 2, 0.08], [3 + t / 2, 0, 1.5], 0x5a5048)
  box(scene, [6 + 2 * t, 6 + 2 * t, t], [0, 0, 3 + t / 2])
  // Furniture.
  box(scene, [1.6, 0.9, 0.75], [0.6, -0.6, 0.375], 0x8a6a50)
  const ball = new Mesh(new SphereGeometry(0.35, 48, 24), new MeshStandardMaterial({ color: 0xd0c8c0, roughness: 0.4 }))
  ball.position.set(-1.4, -1.5, 0.35)
  ball.castShadow = ball.receiveShadow = true
  scene.add(ball)
  const column = new Mesh(new CylinderGeometry(0.2, 0.2, 2.2, 32), new MeshStandardMaterial({ color: 0xc0b8b0 }))
  column.rotation.x = Math.PI / 2
  column.position.set(-1.2, 1.2, 1.1)
  column.castShadow = column.receiveShadow = true
  scene.add(column)
  scene.add(new HemisphereLight(0x9cb8e0, 0x6a6258, 1.2))

  const sun = new DirectionalLight(0xfff1dc, 6)
  const e = (sunElev * Math.PI) / 180
  const a = (sunAz * Math.PI) / 180
  sun.position.set(Math.cos(e) * Math.cos(a) * 50, Math.cos(e) * Math.sin(a) * 50, Math.sin(e) * 50)
  sun.castShadow = true
  sun.shadow.mapSize.set(1024, 1024)
  scene.add(sun, sun.target)
  cascadeSunShadow(sun)

  const lights: Lights = { sun, spot: null, point: null }
  if (spot) {
    const s = new SpotLight(0xffe0b0, 60, 0, 0.35, 0.35, 2)
    s.position.set(-2.2, -2.2, 2.95)
    s.target.position.set(-1.4, -1.5, 0)
    s.castShadow = true
    s.shadow.mapSize.set(1024, 1024)
    scene.add(s, s.target)
    lights.spot = s
  }
  if (point) {
    const p = new PointLight(0xffc890, 6, 0, 2)
    p.position.set(-2.0, 2.0, 1.7)
    p.castShadow = true
    p.shadow.mapSize.set(512, 512)
    scene.add(p)
    lights.point = p
  }

  const camera = new PerspectiveCamera(60, 16 / 9, 0.05, 5000)
  camera.up.set(0, 0, 1)
  if (outside) {
    camera.position.set(14, -10, 3)
    camera.lookAt(0, 0, 1.2)
  } else if (view === 'spot') {
    camera.position.set(2.2, 1.6, 1.7)
    camera.lookAt(-1.8, -1.8, 1.0)
  } else {
    camera.position.set(-2.7, -1.6, 1.55)
    camera.lookAt(3, 0.5, 1.05)
  }
  camera.updateMatrixWorld()
  return { scene, camera, lights }
}

/** The volumes each case puts in the scene: data: 体积物件's fields. */
export const fills = {
  /** One box over the room's air: the window's shafts, the spot's cone, the lamp's glow. */
  room: [{ shape: 'box', position: [0, 0, 1.5], scale: [6, 6, 3], scatter_color: '#e8e4dc', scatter_density: 0.08, anisotropy: 0.6, absorption_color: '#cccccc', absorption_density: 0, edge_falloff: 0.05 }],
  /** The same and a dense blue-absorbing sphere: coloured transmittance, self-shadowing. */
  two: [
    { shape: 'box', position: [0, 0, 1.5], scale: [6, 6, 3], scatter_color: '#e8e4dc', scatter_density: 0.3, anisotropy: 0.6, absorption_color: '#cccccc', absorption_density: 0, edge_falloff: 0.05 },
    { shape: 'sphere', position: [1.2, 0.6, 1.4], scale: [1.4, 1.4, 1.4], scatter_color: '#ffffff', scatter_density: 4, anisotropy: 0, absorption_color: '#ffd080', absorption_density: 2, edge_falloff: 0.5 },
  ],
}

/** Morning mist over the yard, and the room's air. */
export const yard = [
  { shape: 'box', position: [0, 0, 2], scale: [60, 60, 4], scatter_color: '#ffffff', scatter_density: 0.04, anisotropy: 0.5, absorption_color: '#cccccc', absorption_density: 0, edge_falloff: 0.3 },
]
fills.yard = yard
/** A dense smoke ball in the sun on the yard, a column of smoke in the room. */
fills.smoke = [
  { shape: 'sphere', position: [6, -4, 1.6], scale: [2.4, 2.4, 2.4], scatter_color: '#d8d8d8', scatter_density: 3, anisotropy: 0.2, absorption_color: '#808080', absorption_density: 1, edge_falloff: 0.4 },
  { shape: 'box', position: [-1.4, -1.5, 1.6], scale: [0.8, 0.8, 2.2], scatter_color: '#e0e0e0', scatter_density: 2.5, anisotropy: 0.2, absorption_color: '#808080', absorption_density: 0.5, edge_falloff: 0.5 },
]

/** n volumes side by side over the room: the cost of each one more. */
export function many(n: number) {
  const out = []
  for (let i = 0; i < n; i++) {
    const x = -2.25 + (i % 4) * 1.5
    const y = i < 4 ? -1.5 : 1.5
    out.push({ shape: i % 2 ? 'sphere' : 'box', position: [x, y, 1.5], scale: [1.5, 3, 3], scatter_color: '#e8e4dc', scatter_density: 0.3, anisotropy: 0.6, absorption_color: '#cccccc', absorption_density: 0, edge_falloff: 0.1 })
  }
  return out
}
