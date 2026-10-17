import { useEffect, useMemo, useRef, useState, type MutableRefObject } from 'react'
import { Canvas, useThree } from '@react-three/fiber'
import {
  CameraControls,
  CameraControlsImpl,
  GizmoHelper,
  GizmoViewport,
  Grid,
  Sky,
  TransformControls,
} from '@react-three/drei'
import { useStore } from 'zustand'
import {
  Box3,
  Matrix4,
  Object3D,
  OrthographicCamera,
  PerspectiveCamera,
  Plane,
  Raycaster,
  Vector2,
  Vector3,
  type Camera,
} from 'three'
import { SelectionBox } from 'three/addons/interactive/SelectionBox.js'
import type { Scene, SceneEnvironment, SceneObject } from './api.ts'
import { buttonActions, classifyWheel, resolveDevice, wheelAction, type WheelSample } from './input/navigation.ts'
import { pickable, topmost } from './input/selection.ts'
import type { EditorStore, Tool } from './state/sceneStore.ts'
import { numericKey, numericText } from './transform/numeric.ts'
import {
  constraintAxes,
  cycleConstraint,
  evaluate,
  localFromWorld,
  setPrecision,
  startModal,
  switchMode,
  unchanged,
  type Axis,
  type LocalTransform,
  type ModalState,
  type Mode,
  type Modifiers,
  type Target,
  type View,
} from './transform/modal.ts'
import { SceneObjects } from './objects/SceneObjects.tsx'

export type ModalAction =
  | 'axis-x' | 'axis-y' | 'axis-z'
  | 'plane-x' | 'plane-y' | 'plane-z'
  | 'grab' | 'rotate' | 'scale'
  | 'confirm' | 'cancel'

export type ViewportHandle = {
  /** Starts G / R / S on these objects; false when there is nothing to move. */
  startTransform(mode: Mode, ids: string[], options?: { held?: string; label?: string }): boolean
  modalAction(action: ModalAction): void
  /** Typed digits during a transform; false when the key is not numeric input. */
  modalNumeric(e: { code: string; shiftKey: boolean }): boolean
  modifiers(e: { metaKey: boolean; ctrlKey: boolean; shiftKey: boolean; altKey: boolean }): void
  /**
   * Where a screen point meets the ground, for placing new objects; a point
   * outside the viewport places at the view's centre.
   */
  groundPoint(at?: { x: number; y: number }): [number, number, number]
  pointer(): { x: number; y: number }
  setView(direction: readonly number[]): void
  toggleOrtho(): boolean
  frame(ids: readonly string[]): void
}

export type ViewportProps = {
  scene: Scene
  readOnly: boolean
  ui: EditorStore
  mac: boolean
  handle: MutableRefObject<ViewportHandle | null>
  onPick(id: string | null, shift: boolean): void
  onBox(ids: string[], mode: 'set' | 'add' | 'subtract'): void
  onCommit(results: LocalTransform[], label: string, held: string | null): void
  onCancelHeld(held: string): void
  onContextMenu(x: number, y: number, objectID: string | null): void
  onDragging(dragging: boolean): void
}

const { ACTION } = CameraControlsImpl
const actionCode = { rotate: ACTION.ROTATE, truck: ACTION.TRUCK, dolly: ACTION.DOLLY, none: ACTION.NONE } as const
const modeName: Record<Mode, string> = { grab: '移动', rotate: '旋转', scale: '缩放' }
const axisName = ['X', 'Y', 'Z']
const dragThreshold = 4

type Rect = { x0: number; y0: number; x1: number; y1: number }

export function Viewport(props: ViewportProps) {
  const [box, setBox] = useState<Rect | null>(null)
  const selection = useStore(props.ui, (s) => s.selection)
  const tool = useStore(props.ui, (s) => s.tool)
  const ortho = useStore(props.ui, (s) => s.ortho)
  const modal = useStore(props.ui, (s) => s.modal)
  const selected = useMemo(() => new Set(selection.selected), [selection.selected])
  const wrap = useRef<HTMLDivElement>(null)
  const r = box && wrap.current ? wrap.current.getBoundingClientRect() : null
  return (
    <div ref={wrap} className="cc-scene-viewport">
      <Canvas frameloop="demand" shadows dpr={[1, 2]} camera={{ position: [10, 8, 12], fov: 45, near: 0.05, far: 5000 }}>
        <Environment environment={props.scene.environment} />
        <Grid position={[0, 0.002, 0]} infiniteGrid cellSize={1} sectionSize={5} cellColor="#9a968e" sectionColor="#77736c" fadeDistance={80} />
        <SceneObjects scene={props.scene} selected={selected} active={selection.active} />
        <OrthoSwitch ortho={ortho} />
        <CameraControls makeDefault />
        {!props.readOnly && !modal && ['move', 'rotate', 'scale'].includes(tool) && (
          <Gizmo tool={tool} scene={props.scene} ids={topmost(selection.selected, props.scene.objects)} mac={props.mac} onCommit={props.onCommit} onDragging={props.onDragging} />
        )}
        <GizmoHelper alignment="top-right" margin={[64, 64]}>
          <GizmoViewport axisColors={['#e5484d', '#46a758', '#3e63dd']} labelColor="#ffffff" />
        </GizmoHelper>
        <Controller {...props} setBox={setBox} tool={tool} />
      </Canvas>
      {box && r && (
        <div
          className="cc-scene-box"
          style={{
            left: Math.min(box.x0, box.x1) - r.left,
            top: Math.min(box.y0, box.y1) - r.top,
            width: Math.abs(box.x1 - box.x0),
            height: Math.abs(box.y1 - box.y0),
          }}
        />
      )}
    </div>
  )
}

function sunDirection(env: SceneEnvironment): Vector3 {
  const sky = env.sky
  const el = ((sky.sun_elevation_deg ?? 35) * Math.PI) / 180
  const az = ((sky.sun_azimuth_deg ?? 135) * Math.PI) / 180
  return new Vector3(Math.sin(az) * Math.cos(el), Math.sin(el), Math.cos(az) * Math.cos(el))
}

/** Clay lighting: the scene's sky and ambient, one sun with shadows. */
function Environment({ environment }: { environment: SceneEnvironment }) {
  const sun = sunDirection(environment).multiplyScalar(60)
  const sky = environment.sky
  return (
    <>
      {sky.kind === 'procedural' ? (
        <Sky sunPosition={[sun.x, Math.max(sun.y, 0.5), sun.z]} turbidity={sky.turbidity ?? 3} />
      ) : (
        <color attach="background" args={[sky.kind === 'color' ? (sky.color ?? '#8a8f98') : '#8a8f98']} />
      )}
      {environment.fog && <fog attach="fog" args={[environment.fog.color, environment.fog.near_m, environment.fog.far_m]} />}
      <ambientLight color={environment.ambient.color} intensity={environment.ambient.intensity + 0.3} />
      <hemisphereLight args={['#ffffff', '#6b6760', 0.6]} />
      <directionalLight
        position={[sun.x, Math.max(sun.y, 2), sun.z]}
        intensity={2.2}
        castShadow
        shadow-mapSize={[2048, 2048]}
        shadow-camera-left={-30}
        shadow-camera-right={30}
        shadow-camera-top={30}
        shadow-camera-bottom={-30}
      />
    </>
  )
}

/** Swaps between perspective and orthographic, keeping where the view looks. */
function OrthoSwitch({ ortho }: { ortho: boolean }) {
  const { set, camera, size } = useThree()
  const controls = useThree((s) => s.controls) as CameraControlsImpl | null
  const pending = useRef<{ position: Vector3; target: Vector3 } | null>(null)
  useEffect(() => {
    const isOrtho = (camera as OrthographicCamera).isOrthographicCamera === true
    if (isOrtho === ortho) return
    const target = controls ? controls.getTarget(new Vector3()) : new Vector3()
    const position = camera.position.clone()
    const distance = position.distanceTo(target)
    let next: Camera
    if (ortho) {
      const persp = camera as PerspectiveCamera
      const height = 2 * distance * Math.tan(((persp.fov ?? 45) * Math.PI) / 360)
      const o = new OrthographicCamera(-size.width / 2, size.width / 2, size.height / 2, -size.height / 2, -5000, 5000)
      o.zoom = size.height / height
      next = o
    } else {
      next = new PerspectiveCamera(45, size.width / size.height, 0.05, 5000)
    }
    next.position.copy(position)
    pending.current = { position, target }
    set({ camera: next as PerspectiveCamera })
  }, [ortho, camera, controls, set, size])
  useEffect(() => {
    const p = pending.current
    if (!p || !controls) return
    pending.current = null
    void controls.setLookAt(p.position.x, p.position.y, p.position.z, p.target.x, p.target.y, p.target.z, false)
  }, [controls])
  return null
}

type ModalRun = {
  state: ModalState
  originals: Map<string, { position: Vector3; quaternion: Object3D['quaternion']; scale: Vector3 }>
  /** Tweak-tool drags confirm on release. */
  dragConfirm: boolean
  held: string | null
  label: string
  mods: Modifiers
  last: { results: LocalTransform[]; values: number[] } | null
}

function Controller(props: ViewportProps & { setBox(b: Rect | null): void; tool: Tool }) {
  const { camera, scene, gl, invalidate } = useThree()
  const controls = useThree((s) => s.controls) as CameraControlsImpl | null
  const latest = useRef(props)
  latest.current = props
  const pointer = useRef(new Vector2(0, 0))
  const modal = useRef<ModalRun | null>(null)
  const samples = useRef<WheelSample[]>([])

  useEffect(() => {
    if (!controls) return
    controls.mouseButtons.right = ACTION.NONE
    controls.mouseButtons.wheel = ACTION.NONE
    controls.mouseButtons.left = ACTION.NONE
    controls.mouseButtons.middle = ACTION.ROTATE
    controls.dollyToCursor = true
  }, [controls])

  useEffect(() => {
    const el = gl.domElement
    const raycaster = new Raycaster()
    const rect = () => el.getBoundingClientRect()
    const ndc = (x: number, y: number) => {
      const r = rect()
      return new Vector2(((x - r.left) / r.width) * 2 - 1, -((y - r.top) / r.height) * 2 + 1)
    }
    const view: View = {
      ray: (p) => {
        raycaster.setFromCamera(ndc(p.x, p.y), camera)
        return raycaster.ray.clone()
      },
      toScreen: (w) => {
        const v = w.clone().project(camera)
        const r = rect()
        return new Vector2(r.left + ((v.x + 1) / 2) * r.width, r.top + ((1 - v.y) / 2) * r.height)
      },
      toViewer: () => camera.getWorldDirection(new Vector3()).negate(),
    }
    const objectsByID = () => new Map(latest.current.scene.objects.map((o) => [o.id, o]))
    // Hiding a group does not hide its children, so only the object itself counts.
    const canPick = (o: SceneObject | undefined) => !!o && pickable(o)
    const objectOf = (hit: Object3D): string | null => {
      for (let n: Object3D | null = hit; n; n = n.parent) {
        const id = n.userData?.objectID as string | undefined
        if (id) return id
      }
      return null
    }
    const objectAt = (x: number, y: number): string | null => {
      raycaster.setFromCamera(ndc(x, y), camera)
      const byID = objectsByID()
      for (const hit of raycaster.intersectObjects(scene.children, true)) {
        const id = objectOf(hit.object)
        if (id && canPick(byID.get(id))) return id
      }
      return null
    }
    const node = (id: string) => scene.getObjectByName(id) ?? null
    const targetsOf = (ids: readonly string[]): Target[] => {
      scene.updateMatrixWorld(true)
      return ids
        .map((id) => node(id))
        .filter((n): n is Object3D => !!n)
        .map((n) => ({ id: n.name, world: n.matrixWorld.clone(), parentWorld: n.parent?.matrixWorld.clone() ?? new Matrix4() }))
    }

    const status = (m: ModalRun) => {
      const s = m.state
      const c = s.constraint
      const where = c.kind === 'axis' ? `${axisName[c.axis]} 轴${c.space === 'local' ? '（自身）' : ''}` : c.kind === 'plane' ? `排除 ${axisName[c.axis]} 轴${c.space === 'local' ? '（自身）' : ''}` : ''
      const values = m.last?.values ?? []
      const unit = s.mode === 'grab' ? ' m' : s.mode === 'rotate' ? '°' : ''
      const digits = s.mode === 'rotate' ? 1 : 3
      const typed = s.numeric.buffers.some((b) => b !== '')
      const shown = typed
        ? numericText(s.numeric, constraintAxes(c).map((a) => (s.mode === 'grab' ? axisName[a] : '')))
        : values.map((v) => `${v.toFixed(digits)}${unit}`).join('  ')
      return [modeName[s.mode], where, shown].filter(Boolean).join(' · ')
    }
    const run = () => {
      const m = modal.current
      if (!m) return
      const e = evaluate(m.state, pointer.current, m.mods)
      m.state = e.state
      m.last = { results: e.results, values: e.values }
      for (const r of e.results) {
        const n = node(r.id)
        if (!n) continue
        n.position.set(...r.position)
        n.quaternion.set(...r.rotation)
        n.scale.set(...r.scale)
      }
      invalidate()
      latest.current.ui.setState({ modal: status(m) })
    }
    const finish = (commit: boolean) => {
      const m = modal.current
      if (!m) return
      modal.current = null
      latest.current.ui.setState({ modal: null })
      latest.current.onDragging(false)
      const changed = m.last && !unchanged(m.state.targets, m.last.results)
      if (commit && m.last && (changed || m.held)) latest.current.onCommit(m.last.results, m.label, m.held)
      else {
        for (const [id, o] of m.originals) {
          const n = node(id)
          if (!n) continue
          n.position.copy(o.position)
          n.quaternion.copy(o.quaternion)
          n.scale.copy(o.scale)
        }
        if (m.held) latest.current.onCancelHeld(m.held)
      }
      invalidate()
    }
    const begin = (mode: Mode, ids: string[], options: { held?: string; label?: string; dragConfirm?: boolean } = {}) => {
      if (modal.current || !ids.length) return false
      const targets = targetsOf(ids)
      if (!targets.length) return false
      const originals = new Map(
        targets.map((t) => {
          const n = node(t.id)!
          return [t.id, { position: n.position.clone(), quaternion: n.quaternion.clone(), scale: n.scale.clone() }]
        }),
      )
      modal.current = {
        state: startModal(mode, targets, pointer.current.clone(), view),
        originals,
        dragConfirm: !!options.dragConfirm,
        held: options.held ?? null,
        label: options.label ?? `${modeName[mode]} ${targets.length} 个物件`,
        mods: { snap: false, shift: false },
        last: null,
      }
      latest.current.onDragging(true)
      run()
      return true
    }
    const applyButtons = (e: { altKey: boolean; shiftKey: boolean; ctrlKey: boolean }) => {
      if (!controls) return
      const a = buttonActions({ alt: e.altKey, shift: e.shiftKey, ctrl: e.ctrlKey })
      controls.mouseButtons.left = actionCode[a.left]
      controls.mouseButtons.middle = actionCode[a.middle]
    }

    latest.current.handle.current = {
      startTransform: (mode, ids, options) => begin(mode, ids, options),
      modalAction: (action) => {
        const m = modal.current
        if (!m) return
        if (action === 'confirm') return finish(true)
        if (action === 'cancel') return finish(false)
        if (action === 'grab' || action === 'rotate' || action === 'scale') {
          if (m.state.mode !== action) m.state = switchMode(m.state, action, pointer.current)
        } else {
          const axis = (['x', 'y', 'z'].indexOf(action.slice(-1)) as Axis)
          m.state = cycleConstraint(m.state, axis, action.startsWith('plane'))
        }
        run()
      },
      modalNumeric: (e) => {
        const m = modal.current
        if (!m) return false
        const next = numericKey(m.state.numeric, e)
        if (!next) return false
        m.state = { ...m.state, numeric: next }
        run()
        return true
      },
      modifiers: (e) => {
        applyButtons(e)
        const m = modal.current
        if (!m) return
        const snap = latest.current.mac ? e.metaKey : e.ctrlKey
        m.mods = { snap, shift: e.shiftKey }
        m.state = setPrecision(m.state, pointer.current, e.shiftKey && !snap)
        run()
      },
      groundPoint: (at) => {
        const r = rect()
        if (at && at.x >= r.left && at.x <= r.right && at.y >= r.top && at.y <= r.bottom) {
          raycaster.setFromCamera(ndc(at.x, at.y), camera)
          const hit = raycaster.ray.intersectPlane(new Plane(new Vector3(0, 1, 0), 0), new Vector3())
          if (hit && hit.length() < 5000) return [hit.x, 0, hit.z]
        }
        const t = controls ? controls.getTarget(new Vector3()) : new Vector3()
        return [t.x, 0, t.z]
      },
      pointer: () => ({ x: pointer.current.x, y: pointer.current.y }),
      setView: (dir) => {
        if (!controls) return
        const target = controls.getTarget(new Vector3())
        const distance = Math.max(1, camera.position.distanceTo(target))
        // Looking straight down needs a tiny offset or the up vector flips.
        const d = new Vector3(dir[0], dir[1], dir[2] + (Math.abs(dir[1]) === 1 ? 1e-4 : 0)).normalize()
        void controls.setLookAt(target.x + d.x * distance, target.y + d.y * distance, target.z + d.z * distance, target.x, target.y, target.z, true)
      },
      toggleOrtho: () => {
        const next = !latest.current.ui.getState().ortho
        latest.current.ui.setState({ ortho: next })
        return next
      },
      frame: (ids) => {
        if (!controls) return
        const bounds = new Box3()
        for (const id of ids) {
          const n = node(id)
          if (n) bounds.expandByObject(n)
        }
        if (!bounds.isEmpty()) void controls.fitToBox(bounds, true, { paddingTop: 0.5, paddingBottom: 0.5, paddingLeft: 0.5, paddingRight: 0.5 })
      },
    }

    let down: { x: number; y: number; button: number; shift: boolean; mod: boolean; objectID: string | null; dragging: boolean } | null = null

    const onPointerDown = (e: PointerEvent) => {
      pointer.current.set(e.clientX, e.clientY)
      applyButtons(e)
      if (modal.current) {
        e.preventDefault()
        e.stopImmediatePropagation()
        if (e.button === 0) finish(true)
        if (e.button === 2) finish(false)
        return
      }
      const p = latest.current
      if (e.button === 0 && !e.altKey) {
        if (gizmo.current?.axis) return
        if (p.readOnly) return
        down = { x: e.clientX, y: e.clientY, button: 0, shift: e.shiftKey, mod: p.mac ? e.metaKey : e.ctrlKey, objectID: objectAt(e.clientX, e.clientY), dragging: false }
        el.setPointerCapture(e.pointerId)
      } else if (e.button === 2) {
        down = { x: e.clientX, y: e.clientY, button: 2, shift: e.shiftKey, mod: false, objectID: null, dragging: false }
      }
    }
    const onPointerMove = (e: PointerEvent) => {
      pointer.current.set(e.clientX, e.clientY)
      const m = modal.current
      if (m) {
        const snap = latest.current.mac ? e.metaKey : e.ctrlKey
        if (snap !== m.mods.snap || e.shiftKey !== m.mods.shift) {
          m.mods = { snap, shift: e.shiftKey }
          m.state = setPrecision(m.state, pointer.current, e.shiftKey && !snap)
        }
        run()
        return
      }
      if (!down) return
      const moved = Math.hypot(e.clientX - down.x, e.clientY - down.y) > dragThreshold
      if (down.button !== 0 || (!down.dragging && !moved)) {
        if (moved) down.dragging = true
        return
      }
      const p = latest.current
      if (!down.dragging) {
        down.dragging = true
        if (p.tool === 'tweak' && down.objectID) {
          const sel = p.ui.getState().selection
          const ids = sel.selected.includes(down.objectID) ? topmost(sel.selected, p.scene.objects) : [down.objectID]
          if (!sel.selected.includes(down.objectID)) p.onPick(down.objectID, false)
          pointer.current.set(down.x, down.y)
          begin('grab', ids, { dragConfirm: true })
          pointer.current.set(e.clientX, e.clientY)
          run()
          down = null
          return
        }
        p.ui.setState({ boxing: true })
      }
      p.setBox({ x0: down.x, y0: down.y, x1: e.clientX, y1: e.clientY })
    }
    const finishBox = (e: PointerEvent, d: NonNullable<typeof down>) => {
      const p = latest.current
      const a = ndc(d.x, d.y)
      const b = ndc(e.clientX, e.clientY)
      const selector = new SelectionBox(camera, scene)
      const byID = objectsByID()
      const ids = new Set<string>()
      for (const hit of selector.select(new Vector3(a.x, a.y, 0.5), new Vector3(b.x, b.y, 0.5))) {
        const id = objectOf(hit)
        if (id && canPick(byID.get(id))) ids.add(id)
      }
      p.onBox([...ids], d.shift ? 'add' : d.mod ? 'subtract' : 'set')
      p.ui.setState({ boxing: false })
      p.setBox(null)
    }
    const onPointerUp = (e: PointerEvent) => {
      const m = modal.current
      if (m?.dragConfirm) {
        finish(true)
        return
      }
      const d = down
      down = null
      if (!d) return
      const p = latest.current
      if (d.button === 0) {
        if (p.ui.getState().boxing) finishBox(e, d)
        else if (!d.dragging) p.onPick(d.objectID, d.shift)
      } else if (d.button === 2 && !d.dragging) {
        p.onContextMenu(e.clientX, e.clientY, objectAt(e.clientX, e.clientY))
      }
    }
    const onContextMenu = (e: Event) => e.preventDefault()
    const onWheel = (e: WheelEvent) => {
      e.preventDefault()
      if (!controls || modal.current) return
      const sample: WheelSample = { deltaX: e.deltaX, deltaY: e.deltaY, deltaMode: e.deltaMode, ctrlKey: e.ctrlKey, shiftKey: e.shiftKey, altKey: e.altKey, metaKey: e.metaKey, t: e.timeStamp }
      samples.current = [...samples.current.filter((s) => sample.t - s.t < 400), sample].slice(-12)
      const ui = latest.current.ui
      const guess = classifyWheel(samples.current)
      if (guess && guess !== ui.getState().guessed) ui.setState({ guessed: guess })
      const device = resolveDevice(ui.getState().device, ui.getState().guessed)
      const a = wheelAction(sample, device)
      const isOrtho = (camera as OrthographicCamera).isOrthographicCamera === true
      if (a.kind === 'dolly') {
        const k = e.ctrlKey ? 0.01 : 0.0015
        if (isOrtho) void controls.zoom(-a.amount * k * (camera as OrthographicCamera).zoom, false)
        else void controls.dolly(-a.amount * k * Math.max(1, controls.distance), false)
      } else if (a.kind === 'orbit') {
        void controls.rotate(-a.dx * 0.004, -a.dy * 0.004, false)
      } else {
        const k = (isOrtho ? 1 / (camera as OrthographicCamera).zoom : controls.distance * 0.0015)
        void controls.truck(a.dx * k, a.dy * k, false)
      }
    }
    // Safari reports trackpad pinches as gesture events.
    let gestureScale = 1
    const onGestureStart = (e: Event) => {
      e.preventDefault()
      gestureScale = 1
    }
    const onGestureChange = (e: Event) => {
      e.preventDefault()
      const scale = (e as Event & { scale: number }).scale
      if (!controls) return
      const step = scale / gestureScale
      gestureScale = scale
      const isOrtho = (camera as OrthographicCamera).isOrthographicCamera === true
      if (isOrtho) void controls.zoom((step - 1) * (camera as OrthographicCamera).zoom, false)
      else void controls.dolly((step - 1) * Math.max(1, controls.distance), false)
    }
    el.addEventListener('pointerdown', onPointerDown, true)
    window.addEventListener('pointermove', onPointerMove)
    el.addEventListener('pointerup', onPointerUp)
    el.addEventListener('contextmenu', onContextMenu)
    el.addEventListener('wheel', onWheel, { passive: false })
    el.addEventListener('gesturestart', onGestureStart)
    el.addEventListener('gesturechange', onGestureChange)
    return () => {
      el.removeEventListener('pointerdown', onPointerDown, true)
      window.removeEventListener('pointermove', onPointerMove)
      el.removeEventListener('pointerup', onPointerUp)
      el.removeEventListener('contextmenu', onContextMenu)
      el.removeEventListener('wheel', onWheel)
      el.removeEventListener('gesturestart', onGestureStart)
      el.removeEventListener('gesturechange', onGestureChange)
      if (modal.current) finish(false)
    }
  }, [camera, scene, gl, controls, invalidate])
  return null
}

/** The gizmo, read on pointer down: a press on its axes is not a selection click. */
const gizmo: { current: { axis: string | null } | null } = { current: null }

/** Move / rotate / scale gizmo on a proxy at the selection's midpoint. */
function Gizmo({
  tool,
  scene,
  ids,
  mac,
  onCommit,
  onDragging,
}: {
  tool: Tool
  scene: Scene
  ids: string[]
  mac: boolean
  onCommit: ViewportProps['onCommit']
  onDragging: ViewportProps['onDragging']
}) {
  const { scene: three, invalidate } = useThree()
  const proxy = useMemo(() => new Object3D(), [])
  const [snap, setSnap] = useState(false)
  const drag = useRef<{ targets: Target[]; start: Matrix4 } | null>(null)
  const byID = useMemo(() => new Map(scene.objects.map((o) => [o.id, o])), [scene.objects])
  const movable = ids.filter((id) => byID.get(id) && !byID.get(id)!.locked)
  useEffect(() => {
    const key = (e: KeyboardEvent) => setSnap(mac ? e.metaKey : e.ctrlKey)
    window.addEventListener('keydown', key)
    window.addEventListener('keyup', key)
    return () => {
      window.removeEventListener('keydown', key)
      window.removeEventListener('keyup', key)
    }
  }, [mac])
  useEffect(() => {
    if (drag.current) return
    three.updateMatrixWorld(true)
    const center = new Vector3()
    let n = 0
    for (const id of movable) {
      const o = three.getObjectByName(id)
      if (o) {
        center.add(new Vector3().setFromMatrixPosition(o.matrixWorld))
        n++
      }
    }
    if (n) proxy.position.copy(center.divideScalar(n))
    proxy.quaternion.identity()
    proxy.scale.set(1, 1, 1)
    proxy.updateMatrixWorld(true)
    invalidate()
  })
  useEffect(() => () => {
    gizmo.current = null
  }, [])
  if (!movable.length) return null
  const mode = tool === 'move' ? 'translate' : tool === 'rotate' ? 'rotate' : 'scale'
  return (
    <>
      <primitive object={proxy} />
      <TransformControls
        ref={(c) => {
          gizmo.current = c as unknown as { axis: string | null } | null
        }}
        object={proxy}
        mode={mode}
        space="world"
        translationSnap={snap ? 1 : null}
        rotationSnap={snap ? (5 * Math.PI) / 180 : null}
        scaleSnap={snap ? 0.1 : null}
        onMouseDown={() => {
          three.updateMatrixWorld(true)
          drag.current = {
            start: proxy.matrixWorld.clone(),
            targets: movable
              .map((id) => three.getObjectByName(id))
              .filter((o): o is Object3D => !!o)
              .map((o) => ({ id: o.name, world: o.matrixWorld.clone(), parentWorld: o.parent?.matrixWorld.clone() ?? new Matrix4() })),
          }
          onDragging(true)
        }}
        onObjectChange={() => {
          const d = drag.current
          if (!d) return
          proxy.updateMatrixWorld(true)
          const delta = proxy.matrixWorld.clone().multiply(d.start.clone().invert())
          for (const t of d.targets) {
            const r = localFromWorld(t, delta.clone().multiply(t.world))
            const o = three.getObjectByName(t.id)
            if (!o) continue
            o.position.set(...r.position)
            o.quaternion.set(...r.rotation)
            o.scale.set(...r.scale)
          }
          invalidate()
        }}
        onMouseUp={() => {
          const d = drag.current
          drag.current = null
          onDragging(false)
          if (!d) return
          proxy.updateMatrixWorld(true)
          const delta = proxy.matrixWorld.clone().multiply(d.start.clone().invert())
          const results = d.targets.map((t) => localFromWorld(t, delta.clone().multiply(t.world)))
          if (!unchanged(d.targets, results)) onCommit(results, `${tool === 'move' ? '移动' : tool === 'rotate' ? '旋转' : '缩放'} ${results.length} 个物件`, null)
        }}
      />
    </>
  )
}
