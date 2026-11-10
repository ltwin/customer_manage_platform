// Quaternius Universal Base Characters posed through the spike ① retargeting, drawn for the owner.
import * as THREE from '../retarget-spike/three.ts'
import { GLTFLoader } from '/Users/samson/workspace/my_project/customer_manage_platform/frontend/node_modules/three/examples/jsm/loaders/GLTFLoader.js'
import { apply, mirror, prepare, tableMapping, turn, type Character, type Pose } from '../retarget-spike/retarget.ts'
import { ueTable } from './ue.ts'

async function load(who: string) {
  const gltf = await new GLTFLoader().loadAsync(`/assets/ubc/Superhero_${who}_FullBody.gltf`)
  const json = gltf.parser.json
  const joints = new Set<number>((json.skins ?? []).flatMap((s: { joints: number[] }) => s.joints))
  for (const [o, a] of gltf.parser.associations) {
    const i = (a as { nodes?: number }).nodes
    if (i !== undefined && o instanceof THREE.Object3D) {
      o.userData.gltfName = json.nodes[i].name ?? ''
      o.userData.joint = joints.has(i)
    }
  }
  gltf.scene.traverse((o) => ((o as THREE.Mesh).isMesh && (o.frustumCulled = false)))
  const mapping = Object.fromEntries(Object.entries(tableMapping(gltf.scene, ueTable)).filter(([, o]) => o!.userData.joint))
  return { root: gltf.scene, rig: prepare(gltf.scene, mapping) }
}

const curl = (side: 'left', deg: number, fingers = ['Index', 'Middle', 'Ring', 'Little']): Pose =>
  Object.fromEntries(fingers.flatMap((f) => ['Proximal', 'Intermediate', 'Distal'].map((j) => [`${side}${f}${j}`, turn([0, 0, 1], -deg)])))
const fist: Pose = { ...curl('left', 80), leftThumbProximal: turn([0, 1, 0], 35), leftThumbDistal: turn([0, 0, 1], -50) }
const point: Pose = { ...curl('left', 85, ['Middle', 'Ring', 'Little']), leftThumbProximal: turn([0, 1, 0], 30), leftThumbDistal: turn([0, 0, 1], -40) }
const elbow: Pose = { leftUpperArm: turn([0, 1, 0], -90), leftLowerArm: turn([0, 0, 1], 90) }
const columns: [string, Pose][] = [
  ['零姿态（T 姿势）', {}],
  ['左臂前伸、肘上弯，左手握拳', { ...elbow, ...fist }],
  ['镜像到右臂；头左转 45°', { ...mirror({ ...elbow, ...fist }), head: turn([0, 1, 0], 45) }],
  ['前倾 30°、左腿前抬、左手指向前', { spine: turn([1, 0, 0], 30), leftUpperLeg: turn([1, 0, 0], -90), leftLowerLeg: turn([1, 0, 0], 60), leftUpperArm: turn([0, 1, 0], -80), ...point }],
  ['坐姿：双腿前抬、膝弯 90°', { leftUpperLeg: turn([1, 0, 0], -90), rightUpperLeg: turn([1, 0, 0], -90), leftLowerLeg: turn([1, 0, 0], 90), rightLowerLeg: turn([1, 0, 0], 90), leftUpperArm: turn([0, 0, 1], -70), rightUpperArm: turn([0, 0, 1], 70) }],
]

function setup(width: number, height: number) {
  const canvas = document.createElement('canvas')
  canvas.width = width
  canvas.height = height
  const renderer = new THREE.WebGLRenderer({ canvas, antialias: true, preserveDrawingBuffer: true })
  renderer.setPixelRatio(1)
  renderer.setSize(width, height, false)
  renderer.setScissorTest(true)
  renderer.outputColorSpace = THREE.SRGBColorSpace
  renderer.setClearColor(0x2b2d31, 1)
  renderer.clear()
  return { canvas, renderer }
}

function draw(renderer: THREE.WebGLRenderer, c: { root: THREE.Object3D; rig: Character }, x: number, y: number, w: number, h: number, frame: (rig: Character) => { centre: THREE.Vector3; size: number; dir: THREE.Vector3 }) {
  const scene = new THREE.Scene()
  scene.add(new THREE.HemisphereLight(0xffffff, 0x404040, 2.2))
  const sun = new THREE.DirectionalLight(0xffffff, 1.8)
  sun.position.copy(new THREE.Vector3(1, 2, 3).applyQuaternion(c.rig.facing))
  scene.add(sun)
  scene.add(c.root)
  c.root.updateMatrixWorld(true)
  const { centre, size, dir } = frame(c.rig)
  const camera = new THREE.PerspectiveCamera(30, w / h, size / 100, size * 50)
  camera.position.copy(centre).add(dir.normalize().multiplyScalar(size).applyQuaternion(c.rig.facing))
  camera.up.set(0, 1, 0).applyQuaternion(c.rig.facing)
  camera.lookAt(centre)
  renderer.setViewport(x, y, w, h)
  renderer.setScissor(x, y, w, h)
  renderer.render(scene, camera)
}

function finish(canvas: HTMLCanvasElement, labels: [string, number, number][]) {
  const out = document.createElement('canvas')
  out.width = canvas.width
  out.height = canvas.height
  const ctx = out.getContext('2d')!
  ctx.drawImage(canvas, 0, 0)
  ctx.fillStyle = '#e6e3dc'
  ctx.font = '16px "PingFang SC", sans-serif'
  for (const [t, x, y] of labels) ctx.fillText(t, x, y)
  ;(window as unknown as { shot: string }).shot = out.toDataURL('image/png')
}

const body = (rig: Character) => ({ centre: rig.mapping.hips!.getWorldPosition(new THREE.Vector3()).add(new THREE.Vector3(0, 0.1, 0)), size: 5.2, dir: new THREE.Vector3(0.7, 0.3, 1) })

async function poses() {
  const chars = [await load('Male'), await load('Female')]
  const cw = 300, ch = 420, label = 28
  const width = cw * columns.length, height = (ch + label) * chars.length
  const { canvas, renderer } = setup(width, height)
  const labels: [string, number, number][] = []
  chars.forEach((c, r) => columns.forEach(([name, pose], k) => {
    apply(c.rig, pose)
    draw(renderer, c, k * cw, height - (r + 1) * (ch + label), cw, ch, body)
    labels.push([name, k * cw + 8, r * (ch + label) + 20])
  }))
  finish(canvas, labels)
}

async function hands() {
  const c = await load('Male')
  const cells: [string, Pose][] = [
    ['零姿态（T）', {}],
    ['握拳', fist],
    ['食指指向', point],
    ['手绕前臂转 90°', { leftHand: turn([1, 0, 0], 90) }],
    ['手绕前臂转 150°', { leftHand: turn([1, 0, 0], 150) }],
  ]
  const cw = 320, ch = 320, label = 28
  const { canvas, renderer } = setup(cw * cells.length, ch + label)
  const labels: [string, number, number][] = []
  cells.forEach(([name, pose], k) => {
    apply(c.rig, pose)
    const twist = name.includes('转')
    draw(renderer, c, k * cw, 0, cw, ch, (rig) => {
      const elbow = rig.mapping.leftLowerArm!.getWorldPosition(new THREE.Vector3())
      const hand = rig.mapping.leftHand!.getWorldPosition(new THREE.Vector3())
      const mid = rig.mapping.leftMiddleProximal!.getWorldPosition(new THREE.Vector3())
      return twist ? { centre: elbow.clone().lerp(hand, 0.6), size: 0.9, dir: new THREE.Vector3(0.1, 0.5, 1) } : { centre: hand.clone().lerp(mid, 0.8), size: 0.45, dir: new THREE.Vector3(0.3, 0.6, 1) }
    })
    labels.push([name, k * cw + 8, 20])
  })
  finish(canvas, labels)
}

;(location.search.includes('hands') ? hands() : poses()).catch((e) => ((window as unknown as { failed: string }).failed = String(e?.stack ?? e)))
