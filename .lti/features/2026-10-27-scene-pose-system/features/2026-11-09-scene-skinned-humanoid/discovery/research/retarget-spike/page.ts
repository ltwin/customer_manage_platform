// Spike ①: the same poses on four characters, drawn for the owner to judge.
import * as THREE from './three.ts'
import { GLTFLoader } from '/Users/samson/workspace/my_project/customer_manage_platform/frontend/node_modules/three/examples/jsm/loaders/GLTFLoader.js'
import { apply, constraintsOf, mirror, mixamoMapping, prepare, runConstraints, turn, type Constraint, type Mapping, type Pose } from './retarget.ts'

type Loaded = { name: string; root: THREE.Object3D; mapping: Mapping; constraints: Constraint[] }

const vrm0Thumb: Record<string, string> = { leftThumbProximal: 'leftThumbMetacarpal', leftThumbIntermediate: 'leftThumbProximal', rightThumbProximal: 'rightThumbMetacarpal', rightThumbIntermediate: 'rightThumbProximal' }

async function load(name: string, file: string): Promise<Loaded> {
  const gltf = await new GLTFLoader().loadAsync(`/assets/${file}`)
  const json = gltf.parser.json
  const joints = new Set<number>((json.skins ?? []).flatMap((s: { joints: number[] }) => s.joints))
  const byIndex = new Map<number, THREE.Object3D>()
  for (const [o, a] of gltf.parser.associations) {
    const i = (a as { nodes?: number }).nodes
    if (i !== undefined && o instanceof THREE.Object3D) {
      byIndex.set(i, o)
      o.userData.gltfName = json.nodes[i].name ?? ''
      o.userData.joint = joints.has(i)
    }
  }
  let mapping: Mapping
  if (json.extensions?.VRMC_vrm) mapping = Object.fromEntries(Object.entries(json.extensions.VRMC_vrm.humanoid.humanBones as Record<string, { node: number }>).map(([b, { node }]) => [b, byIndex.get(node)!]))
  else if (json.extensions?.VRM) mapping = Object.fromEntries((json.extensions.VRM.humanoid.humanBones as { bone: string; node: number }[]).map(({ bone, node }) => [vrm0Thumb[bone] ?? bone, byIndex.get(node)!]))
  else mapping = mixamoMapping(gltf.scene)
  gltf.scene.traverse((o) => {
    if ((o as THREE.Mesh).isMesh) {
      o.frustumCulled = false
      const m = (o as THREE.Mesh).material as THREE.MeshStandardMaterial
      if (m && 'side' in m) m.side = THREE.DoubleSide
    }
  })
  return { name, root: gltf.scene, mapping, constraints: constraintsOf(json, (i) => byIndex.get(i)!) }
}

const elbow: Pose = { leftUpperArm: turn([0, 1, 0], -90), leftLowerArm: turn([0, 0, 1], 90) }
const rows: [string, Pose][] = [
  ['T 姿势（零姿态）', {}],
  ['左臂前伸、左肘向上弯 90°', elbow],
  ['镜像：右臂前伸、右肘上弯；头向左转 45°', { ...mirror(elbow), head: turn([0, 1, 0], 45) }],
  ['上身前倾 30°、左大腿前抬 90°、左小臂内旋 90°', { spine: turn([1, 0, 0], 30), leftUpperLeg: turn([1, 0, 0], -90), leftUpperArm: turn([0, 0, 1], -60), leftLowerArm: turn([1, 0, 0], 90) }],
]

async function main() {
  const characters = await Promise.all([
    load('Xbot（Mixamo，T 姿势）', 'Xbot.glb'),
    load('Michelle（Mixamo，Z 轴朝上导出）', 'Michelle.glb'),
    load('VRM 1.0（扭转辅助骨）', 'VRM1_Constraint_Twist_Sample.vrm'),
    load('VRM 0.x（面朝 −Z）', 'three-vrm-girl.vrm'),
  ])
  const cell = 360
  const label = 28
  const width = cell * characters.length
  const height = (cell + label) * rows.length + label
  const canvas = document.createElement('canvas')
  canvas.width = width
  canvas.height = height
  document.body.appendChild(canvas)
  const renderer = new THREE.WebGLRenderer({ canvas, antialias: true, preserveDrawingBuffer: true })
  renderer.setPixelRatio(1)
  renderer.setSize(width, height, false)
  renderer.setScissorTest(true)
  renderer.outputColorSpace = THREE.SRGBColorSpace
  renderer.setClearColor(0x2b2d31, 1)
  renderer.clear()

  const prepared = characters.map((c) => ({ ...c, rig: prepare(c.root, c.mapping) }))
  rows.forEach(([, pose], r) => {
    prepared.forEach((c, k) => {
      apply(c.rig, pose)
      const scene = new THREE.Scene()
      scene.add(new THREE.HemisphereLight(0xffffff, 0x404040, 2.2))
      const sun = new THREE.DirectionalLight(0xffffff, 1.6)
      sun.position.copy(new THREE.Vector3(1, 2, 3).applyQuaternion(c.rig.facing))
      scene.add(sun)
      const holder = new THREE.Group()
      holder.add(c.root)
      scene.add(holder)
      // A floor-to-head frame: the character's T-pose size, seen from its front left.
      const box = new THREE.Box3().setFromObject(c.root, true)
      const centre = box.getCenter(new THREE.Vector3())
      const size = box.getSize(new THREE.Vector3()).length()
      const camera = new THREE.PerspectiveCamera(30, 1, size / 100, size * 10)
      camera.position.copy(centre).add(new THREE.Vector3(0.75, 0.35, 1).normalize().multiplyScalar(size * 1.9).applyQuaternion(c.rig.facing))
      camera.up.set(0, 1, 0).applyQuaternion(c.rig.facing)
      camera.lookAt(centre)
      const x = k * cell
      const y = height - label - r * (cell + label) - cell
      renderer.setViewport(x, y, cell, cell)
      renderer.setScissor(x, y, cell, cell)
      renderer.render(scene, camera)
    })
  })
  const out = document.createElement('canvas')
  out.width = width
  out.height = height
  const ctx = out.getContext('2d')!
  ctx.drawImage(canvas, 0, 0)
  ctx.fillStyle = '#e6e3dc'
  ctx.font = '16px "PingFang SC", sans-serif'
  prepared.forEach((c, k) => ctx.fillText(c.name, k * cell + 10, 20))
  rows.forEach(([name], r) => ctx.fillText(name, 10, label + r * (cell + label) + label - 8 + cell))
  ;(window as unknown as { shot: string }).shot = out.toDataURL('image/png')
}

/** Close-ups of the left forearm as the hand rolls about the arm: twist helpers that only a constraint would move. */
async function twist() {
  const characters = await Promise.all([load('VRM 1.0 未执行约束', 'VRM1_Constraint_Twist_Sample.vrm'), load('VRM 1.0 执行约束', 'VRM1_Constraint_Twist_Sample.vrm'), load('Xbot（无辅助骨）', 'Xbot.glb')])
  const rolls = [0, 90, 150]
  const cell = 380
  const label = 28
  const width = cell * rolls.length
  const height = (cell + label) * characters.length
  const canvas = document.createElement('canvas')
  canvas.width = width
  canvas.height = height
  const renderer = new THREE.WebGLRenderer({ canvas, antialias: true, preserveDrawingBuffer: true })
  renderer.setSize(width, height, false)
  renderer.setScissorTest(true)
  renderer.outputColorSpace = THREE.SRGBColorSpace
  renderer.setClearColor(0x2b2d31, 1)
  renderer.clear()
  characters.forEach((c, r) => {
    const rig = prepare(c.root, c.mapping)
    rolls.forEach((deg, k) => {
      apply(rig, { leftHand: turn([1, 0, 0], deg) })
      if (r === 1) runConstraints(c.constraints)
      const scene = new THREE.Scene()
      scene.add(new THREE.HemisphereLight(0xffffff, 0x404040, 2.2))
      const sun = new THREE.DirectionalLight(0xffffff, 1.6)
      sun.position.copy(new THREE.Vector3(0.3, 2, 3).applyQuaternion(rig.facing))
      scene.add(sun)
      scene.add(c.root)
      const elbow = c.mapping.leftLowerArm!.getWorldPosition(new THREE.Vector3())
      const hand = c.mapping.leftHand!.getWorldPosition(new THREE.Vector3())
      const centre = elbow.clone().lerp(hand, 0.6)
      const span = elbow.distanceTo(hand)
      const camera = new THREE.PerspectiveCamera(30, 1, span / 100, span * 50)
      camera.position.copy(centre).add(new THREE.Vector3(0.1, 0.5, 1).normalize().multiplyScalar(span * 3.2).applyQuaternion(rig.facing))
      camera.up.set(0, 1, 0).applyQuaternion(rig.facing)
      camera.lookAt(centre)
      const y = height - (r + 1) * (cell + label) + label
      renderer.setViewport(k * cell, y - label, cell, cell)
      renderer.setScissor(k * cell, y - label, cell, cell)
      renderer.render(scene, camera)
    })
  })
  const out = document.createElement('canvas')
  out.width = width
  out.height = height
  const ctx = out.getContext('2d')!
  ctx.drawImage(canvas, 0, 0)
  ctx.fillStyle = '#e6e3dc'
  ctx.font = '16px "PingFang SC", sans-serif'
  characters.forEach((c, r) => rolls.forEach((deg, k) => ctx.fillText(`${c.name}：手绕前臂转 ${deg}°`, k * cell + 8, r * (cell + label) + 20)))
  ;(window as unknown as { shot: string }).shot = out.toDataURL('image/png')
}

;(location.search.includes('twist') ? twist() : main()).catch((e) => ((window as unknown as { failed: string }).failed = String(e?.stack ?? e)))
