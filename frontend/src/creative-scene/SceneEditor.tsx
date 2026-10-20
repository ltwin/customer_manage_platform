import { useCallback, useEffect, useMemo, useRef, useState, useSyncExternalStore, type FocusEvent as ReactFocusEvent, type KeyboardEvent as ReactKeyboardEvent, type PointerEvent as ReactPointerEvent } from 'react'
import { Box, CircleHelp, History, ImageDown, Minimize2 } from 'lucide-react'
import { useStore } from 'zustand'
import ConfirmDialog from '../components/ConfirmDialog'
import { currentAccount } from '../creative-canvas/editor/api.ts'
import { discardCandidate, uploadGenerated, type Upload } from '../creative-canvas/editor/media.ts'
import { activityText, AgentActivity, editedObjects, highlightedObjects, jobPlaceholders } from './agentActivity.ts'
import { exportScene, readScene, type ObjectUpdate, type Scene, type SceneObject } from './api.ts'
import { builtins } from './builtins/catalog.ts'
import { clearSceneContext, setSceneContext } from './context.ts'
import { clearParent, dropRows, groupSelection, parentToActive, ungroupSelection, type DropPlace } from './grouping.ts'
import { clearOp, duplicate, groundBuiltin, localPose, newCamera, newEmpty, newPrimitive, objectsLabel, transformOp, visibilityOp } from './editing.ts'
import { runManualExport, type ExportPlan, type Progress } from './host/exportShots.ts'
import { cameraFraming } from './host/framing.ts'
import { loadModels, openShooter } from './host/render.ts'
import { ExportDialog } from './panels/ExportDialog.tsx'
import { escapeTarget, handleKey, isMac, shortcutFor, typing, type Action } from './input/keymap.ts'
import { readPreference, viewDirections, writePreference, type DevicePreference } from './input/navigation.ts'
import { boxSelect, click, emptySelection, listClick, prune, selectAll, topmost } from './input/selection.ts'
import { ContextMenu, type MenuItem } from './panels/ContextMenu.tsx'
import { ConflictPrompt } from './panels/ConflictPrompt.tsx'
import { FirstRunHint } from './panels/FirstRunHint.tsx'
import { ModelEdits, sharingInstances, withPreviews } from './modelEdits.ts'
import { modelCache, modelSummaries } from './models/cache.ts'
import { CodePanel } from './panels/CodePanel.tsx'
import { HistoryPanel } from './panels/HistoryPanel.tsx'
import { MenuBar } from './panels/MenuBar.tsx'
import { ModelParams } from './panels/ModelParams.tsx'
import { Outliner } from './panels/Outliner.tsx'
import { Properties } from './panels/Properties.tsx'
import { ShortcutSheet } from './panels/ShortcutSheet.tsx'
import { StatusBar } from './panels/StatusBar.tsx'
import { Toolbar } from './panels/Toolbar.tsx'
import type { SceneJobLauncher } from './host/launcher.ts'
import { iframeFactory, SandboxHost } from './sandbox/host.ts'
import { defaultModuleCode } from './sandbox/template.ts'
import { renderThumbnail } from './sandbox/thumbnail.ts'
import { openScene } from './session.ts'
import type { WireOp } from './state/apply.ts'
import { contextOf, createEditorStore, type MenuKind } from './state/sceneStore.ts'
import { UndoStack } from './state/undo.ts'
import type { LocalTransform, Mode } from './transform/modal.ts'
import { readView, viewKey, ViewSaver } from './viewState.ts'
import { Viewport, type ViewportHandle } from './Viewport.tsx'
import './scene.css'

function webgl2Available(): boolean {
  try {
    return !!document.createElement('canvas').getContext('webgl2')
  } catch {
    return false
  }
}

function storage(): Storage | undefined {
  try {
    return window.localStorage
  } catch {
    return undefined
  }
}

const noObjects: ReadonlySet<string> = new Set()
const modifierKeys = new Set(['ShiftLeft', 'ShiftRight', 'ControlLeft', 'ControlRight', 'MetaLeft', 'MetaRight', 'AltLeft', 'AltRight'])

const agentNotice = '助手正在修改此物件，你的修改可能让它重做'
const focusKinds: readonly SceneObject['kind'][] = ['primitive', 'model_instance', 'group']
const savedWaitMs = 15_000

/** Resolves true once nothing waits to be saved, false after the wait. */
function whenSaved(session: ReturnType<typeof openScene>, ms: number): Promise<boolean> {
  if (!session.hasUnsaved()) return Promise.resolve(true)
  return new Promise((resolve) => {
    const timer = window.setTimeout(() => {
      stop()
      resolve(false)
    }, ms)
    const stop = session.store.subscribe(() => {
      if (session.hasUnsaved()) return
      window.clearTimeout(timer)
      stop()
      resolve(true)
    })
  })
}

const discardUpload = (upload: Upload) =>
  upload.publication?.kind === 'candidate' ? discardCandidate(upload.publication.id, upload.publication.revision) : Promise.resolve()

/**
 * `host` is the page's job launcher: while the editor is open it claims this
 * scene's jobs first. `canvasID` and `sceneNodeID` say where exports go; the
 * editor always opens from a canvas node, and without them there is no export.
 */
export default function SceneEditor({ documentID, onClose, host, canvasID, sceneNodeID }: { documentID: string; onClose: () => void; host?: SceneJobLauncher; canvasID?: string; sceneNodeID?: string }) {
  const session = useMemo(() => openScene(documentID), [documentID])
  // Attach and connect in effects, paired with their cleanup: StrictMode
  // runs cleanup and setup once more after mounting.
  useEffect(() => {
    session.attach()
    return () => session.detach()
  }, [session])
  const undo = useMemo(() => new UndoStack(session), [session])
  useEffect(() => undo.connect(), [undo])
  useSyncExternalStore(
    useCallback((cb) => undo.subscribe(cb), [undo]),
    () => `${undo.canUndo}:${undo.canRedo}`,
  )
  // The view as it was left: camera, selection, tool and panels.
  const view = useMemo(() => {
    const key = viewKey(currentAccount(), documentID)
    const saved = readView(storage(), key)
    const initial = saved ?? { camera: null, selection: emptySelection, tool: 'box' as const, toolbar: true, sidebar: true, history: false }
    return { saved, saver: new ViewSaver(storage(), key, initial) }
  }, [documentID])
  const ui = useMemo(() => createEditorStore(readPreference(storage()), view.saved), [view])
  const mac = useMemo(() => isMac(), [])
  const [webgl] = useState(webgl2Available)
  const s = useStore(session.store)
  const u = useStore(ui)
  const handle = useRef<ViewportHandle | null>(null)
  const root = useRef<HTMLElement>(null)
  const [confirmRestore, setConfirmRestore] = useState(false)
  const [exportRun, setExportRun] = useState<{ busy: boolean; progress: Progress | null }>({ busy: false, progress: null })
  // The code sandbox starts with the first bake and goes with the editor.
  const sandbox = useRef<SandboxHost | null>(null)
  const modelEdits = useMemo(
    () =>
      new ModelEdits({
        session,
        cache: modelCache,
        compile: (input) => {
          sandbox.current ??= new SandboxHost(iframeFactory(root.current ?? document.body))
          return sandbox.current.compile(input)
        },
        thumbnail: renderThumbnail,
        upload: (file, declared, signal) => uploadGenerated(file, declared, { kind: 'staged' }, signal),
        discard: (upload) =>
          upload.publication?.kind === 'candidate' ? discardCandidate(upload.publication.id, upload.publication.revision) : Promise.resolve(),
      }),
    [session],
  )
  useEffect(
    () => () => {
      modelEdits.dispose()
      sandbox.current?.dispose()
      sandbox.current = null
    },
    [modelEdits],
  )
  const baking = useStore(modelEdits.store)
  useEffect(() => host?.prefer(documentID), [host, documentID])
  // What the assistant is doing to this scene: outlines, placeholders, status.
  const agentActivity = useMemo(() => new AgentActivity(documentID), [documentID])
  useEffect(() => agentActivity.start(), [agentActivity])
  const agent = useStore(agentActivity.store)
  const highlighted = useMemo(() => highlightedObjects(agent), [agent])
  const placeholders = useMemo(() => jobPlaceholders(agent.jobs), [agent.jobs])

  const objects = useMemo(() => s.view?.scene.objects ?? [], [s.view])
  const shown: Scene | undefined = u.preview?.scene ?? s.view?.scene
  const localModels = useStore(modelCache.local)
  const failedModels = useStore(modelCache.failed)
  const models = useMemo(() => u.preview?.models ?? modelSummaries(s.base?.models, localModels), [u.preview, s.base, localModels])
  const readOnly = s.trashed || s.missing || !!u.preview || !s.base
  const byID = useMemo(() => new Map(objects.map((o) => [o.id, o])), [objects])
  const selected = u.selection.selected
  const active = u.selection.active ? (byID.get(u.selection.active) ?? null) : null
  const shownActive = u.preview ? (u.preview.scene.objects.find((o) => o.id === active?.id) ?? null) : active
  const shownModel = shownActive?.model?.content_revision_id
  // Bakes waiting for their uploads show on their instances, in the viewport only.
  const drawn = useMemo(() => (shown && !u.preview ? withPreviews(shown, baking.previews) : shown), [shown, u.preview, baking.previews])
  const activity = Object.values(baking.status).some((x) => x.state === 'baking')
    ? '正在生成模型…'
    : Object.values(baking.status).some((x) => x.state === 'uploading')
      ? '正在保存模型…'
      : activityText(agent.jobs, byID)

  useEffect(() => {
    const previous = window.document.activeElement
    root.current?.focus()
    return () => {
      if (previous instanceof HTMLElement && previous.isConnected) previous.focus()
    }
  }, [])
  useEffect(() => {
    // Before the first read there are no objects yet, only a remembered selection.
    if (!s.view) return
    const sel = ui.getState().selection
    const next = prune(sel, objects)
    if (next !== sel) ui.setState({ selection: next })
  }, [objects, ui, s.view])
  // Selecting what the assistant works on says so; it does not stop anything.
  const lastSelected = useRef(selected)
  useEffect(() => {
    if (lastSelected.current === selected) return
    lastSelected.current = selected
    if (selected.some((id) => highlighted.has(id))) ui.setState({ notice: agentNotice })
  }, [selected, highlighted, ui])
  // The assistant panel sends this with a new run; a scene that cannot be edited is not offered.
  const editable = !!s.base && !s.trashed && !s.missing
  useEffect(() => {
    if (editable) setSceneContext({ documentID, selected: [...selected] })
    else clearSceneContext(documentID)
  }, [documentID, selected, editable])
  useEffect(() => () => clearSceneContext(documentID), [documentID])
  useEffect(() => {
    const { saver } = view
    const stop = ui.subscribe((state) => saver.ui(state))
    const flush = () => saver.flush()
    window.addEventListener('pagehide', flush)
    return () => {
      stop()
      window.removeEventListener('pagehide', flush)
      saver.flush()
    }
  }, [ui, view])
  useEffect(() => {
    if (!u.notice) return
    const t = window.setTimeout(() => ui.setState({ notice: '' }), 4000)
    return () => window.clearTimeout(t)
  }, [u.notice, ui])
  // Pushed as they happen: copying undo.notice on every render looped, since
  // each copy re-renders the editor.
  useEffect(() => undo.onNotice((notice) => ui.setState({ notice })), [undo, ui])

  const notify = (notice: string) => ui.setState({ notice })
  /** Editing what the assistant works on goes ahead, with the soft notice. */
  const warnAgent = (ids: readonly string[]) => {
    if (ids.some((id) => highlighted.has(id))) notify(agentNotice)
  }
  const edit = (ops: WireOp[], label: string, options?: Parameters<typeof session.edit>[2]) => {
    const out = session.edit(ops, label, options)
    if (out) warnAgent(editedObjects(ops))
    return out
  }
  const select = (ids: string[]) => ui.setState({ selection: { selected: ids, active: ids.at(-1) ?? null } })
  /** The selection a transform moves: topmost, unlocked. */
  const movable = () => topmost(selected, objects).filter((id) => !byID.get(id)?.locked)
  const label = (verb: string, ids: string[]) => objectsLabel(verb, ids.length, byID.get(ids[0])?.name)

  /** Ctrl+G, Ctrl+Alt+G, Ctrl+P and Alt+P: one edit each, keeping world positions. */
  const regroup = (action: 'group' | 'ungroup' | 'parent' | 'clear-parent') => {
    const sel = { selected, active: active?.id ?? null }
    const r =
      action === 'group'
        ? groupSelection(objects, sel)
        : action === 'ungroup'
          ? ungroupSelection(objects, sel)
          : action === 'parent'
            ? parentToActive(objects, sel)
            : clearParent(objects, sel)
    if (!r) return
    if ('error' in r) return notify(r.error)
    if (!edit([r.op], r.label, r.inverse ? { inverse: r.inverse } : undefined)) return
    if (action === 'group' && r.op.op === 'reparent_objects') {
      const g = r.op.create_groups?.[0]
      if (g) select([g.id])
    }
  }

  const drop = (moving: string[], target: string, place: DropPlace) => {
    const r = dropRows(objects, moving, target, place)
    if (!r) return
    if ('error' in r) return notify(r.error)
    edit([r.op], r.label, r.inverse ? { inverse: r.inverse } : undefined)
  }

  const transform = (mode: Mode) => {
    const ids = movable()
    if (!ids.length) return notify(selected.length ? '锁定的物件不能变换，先在属性栏解锁' : '先选中物件')
    handle.current?.startTransform(mode, ids, { label: label(mode === 'grab' ? '移动' : mode === 'rotate' ? '旋转' : '缩放', ids) })
  }
  const remove = (ids: string[]) => {
    if (ids.length) edit([{ op: 'remove_objects', object_ids: ids }], label('删除', ids))
  }
  const duplicateAndMove = () => {
    const ids = topmost(selected, objects)
    if (!ids.length) return notify('先选中物件')
    const copies = duplicate(objects, ids)
    const copied = new Set(copies.map((c) => c.id))
    const roots = copies.filter((c) => !c.parent_id || !copied.has(c.parent_id)).map((c) => c.id)
    const held = edit([{ op: 'add_objects', objects: copies }], label('复制', ids), { held: true })
    if (!held) return
    select(roots)
    // The copies exist in the 3D scene once React has rendered them.
    let tries = 0
    const start = () => {
      if (handle.current?.startTransform('grab', roots, { held: held.id, label: held.label })) return
      if (++tries < 5) requestAnimationFrame(start)
      else session.release(held.id)
    }
    requestAnimationFrame(start)
  }
  const addCamera = () => {
    const pose = handle.current?.viewPose()
    if (!pose) return
    const o = newCamera(pose, objects)
    if (edit([{ op: 'add_objects', objects: [o] }], `添加「${o.name}」`)) {
      select([o.id])
      ui.setState({ lastCamera: o.id })
    }
  }
  const addEmpty = () => {
    const o = newEmpty(handle.current?.centrePoint() ?? [0, 0, 0], objects)
    if (edit([{ op: 'add_objects', objects: [o] }], `添加「${o.name}」`)) select([o.id])
  }
  /** 0 looks through the active camera, else the last one used, else the first. */
  const cameraView = () => {
    const cameras = (shown?.objects ?? []).filter((o) => o.kind === 'camera')
    const target = active?.kind === 'camera' ? active : (cameras.find((c) => c.id === u.lastCamera) ?? cameras[0])
    if (!target) return notify('场景里没有摄像机，先在「添加」菜单里添加摄像机')
    ui.setState({ cameraView: target.id, lastCamera: target.id })
  }
  /** Ctrl+Alt+0 moves the active camera to where the view looks from. */
  const cameraToView = () => {
    if (active?.kind !== 'camera') return notify('先选中一台摄像机')
    if (active.locked) return notify('锁定的物件不能变换，先在属性栏解锁')
    const pose = handle.current?.viewPose()
    if (!pose) return
    const transform = localPose(objects, active.parent_id ?? null, pose, active.transform.scale)
    if (edit([{ op: 'update_objects', updates: [{ object_id: active.id, transform }] }], `「${active.name || '摄像机'}」对齐当前视角`))
      ui.setState({ cameraView: active.id, lastCamera: active.id })
  }
  /** A click while picking sets the camera's focus object instead of selecting. */
  const pickFocus = (id: string | null) => {
    const cameraID = ui.getState().pickFocus
    if (!cameraID) return false
    const target = id ? byID.get(id) : undefined
    if (!target) {
      notify('点选要对焦的物件，Esc 取消')
      return true
    }
    if (!focusKinds.includes(target.kind)) {
      notify('只能对焦到基础体、模型实例或空物体')
      return true
    }
    ui.setState({ pickFocus: null })
    edit([{ op: 'update_objects', updates: [{ object_id: cameraID, camera: { focus_object_id: target.id } }] }], `「${byID.get(cameraID)?.name || '摄像机'}」对焦到「${target.name || '物件'}」`)
    return true
  }
  /** The photographer's export: saved edits first, then the server's snapshot (E1). */
  const startExport = async (plan: ExportPlan) => {
    if (!canvasID || !sceneNodeID) return
    setExportRun({ busy: true, progress: null })
    if (!(await whenSaved(session, savedWaitMs))) {
      setExportRun({ busy: false, progress: null })
      return notify('还有修改没有保存，保存后再导出')
    }
    const account = currentAccount() ?? ''
    const out = await runManualExport(
      {
        read: (id) => readScene(id),
        loadModels: (doc) => loadModels(doc.scene, doc.models),
        open: (doc) => openShooter(doc.scene, doc.models),
        upload: (file, declared, signal) => uploadGenerated(file, declared, { kind: 'staged' }, signal),
        discard: discardUpload,
        send: (operation, createdAt, payload) => exportScene(account, documentID, operation, createdAt, payload),
        wait: (ms) => new Promise((resolve) => window.setTimeout(resolve, ms)),
      },
      { documentID, canvasID, sceneNodeID, plan },
      new AbortController().signal,
      (progress) => setExportRun({ busy: true, progress }),
    )
    setExportRun({ busy: false, progress: null })
    if (out.ok) {
      ui.setState({ exporting: false })
      notify(`已导出 ${out.result.nodes.length} 张图到画布`)
    } else notify(out.message)
  }
  /** Adds where the menu was opened: Shift+A or a right click over the ground. */
  const addPrimitive = (builtin: string, at?: { x: number; y: number }) => {
    const point = handle.current?.groundPoint(at) ?? [0, 0, 0]
    const o = newPrimitive(builtin, point, objects)
    if (edit([{ op: 'add_objects', objects: [o] }], `添加「${o.name}」`)) select([o.id])
  }
  const openMenu = (menu: MenuKind, x?: number, y?: number) => {
    const p = handle.current?.pointer() ?? { x: 200, y: 200 }
    ui.setState({ popup: { kind: 'menu', menu, x: x ?? p.x, y: y ?? p.y } })
  }
  /** Opens the code panel in the history panel's place. */
  const openCode = (target: { kind: 'new'; position: [number, number, number] } | { kind: 'object'; id: string }) =>
    ui.setState({ code: { ...target, opened: Date.now() }, history: false, preview: null })
  const generate = async (code: string) => {
    const target = ui.getState().code
    if (!target) return
    if (target.kind === 'new') {
      const out = await modelEdits.create({ code, position: target.position })
      if (!out.ok) {
        if (!out.superseded && out.stage !== 'compile') notify(out.message)
        return
      }
      const add = out.intent.ops.find((op) => op.op === 'add_objects')
      const id = add?.op === 'add_objects' ? add.objects[0]?.id : undefined
      if (!id) return
      select([id])
      // The same panel goes on editing the model it just made.
      if (ui.getState().code?.opened === target.opened) ui.setState({ code: { kind: 'object', id, opened: target.opened } })
      return
    }
    const o = session.state.view?.scene.objects.find((x) => x.id === target.id)
    warnAgent([target.id])
    const rev = o?.model?.content_revision_id
    const generator = rev ? await modelCache.generator(rev).catch(() => null) : null
    const out = await modelEdits.update({
      objectID: target.id,
      code,
      params: generator?.params ?? {},
      libVersion: generator?.lib_version ?? 1,
      applyToAll: false,
      label: `修改「${o?.name || '代码模型'}」代码`,
    })
    if (!out.ok && !out.superseded && out.stage !== 'compile') notify(out.message)
  }
  /** A parameter change rebakes the model; false when it did not take. */
  const retune = async (object: SceneObject, params: Record<string, unknown>, applyToAll: boolean): Promise<boolean> => {
    const rev = object.model?.content_revision_id
    if (!rev) return false
    warnAgent([object.id])
    let generator
    try {
      generator = await modelCache.generator(rev)
    } catch {
      notify('读不到这个模型的代码，参数没有生效')
      return false
    }
    const out = await modelEdits.update({ objectID: object.id, code: generator.code, params, libVersion: generator.lib_version, applyToAll, label: `调整「${object.name || '代码模型'}」参数` })
    if (out.ok || out.superseded) return true
    notify(out.stage === 'compile' ? `参数没有生效：${out.message}` : out.message)
    return false
  }
  const setDevice = (device: DevicePreference) => {
    writePreference(storage(), device)
    ui.setState({ device })
  }
  const close = () => {
    if (session.blockedCount() > 0) ui.setState({ confirmClose: true })
    else onClose()
  }

  const run = (action: Action) => {
    if (contextOf(ui.getState()) === 'modal' && action !== 'save') {
      handle.current?.modalAction(action as Parameters<ViewportHandle['modalAction']>[0])
      return
    }
    const editing = !readOnly
    switch (action) {
      case 'add-menu':
        return editing && openMenu('add')
      case 'grab':
      case 'rotate':
      case 'scale':
        return editing && transform(action)
      case 'duplicate':
        return editing && duplicateAndMove()
      case 'delete-confirm': {
        const ids = topmost(selected, objects)
        if (!editing || !ids.length) return
        const p = handle.current?.pointer() ?? { x: 200, y: 200 }
        return ui.setState({ popup: { kind: 'delete', x: p.x, y: p.y, ids } })
      }
      case 'delete':
        return editing && remove(topmost(selected, objects))
      case 'hide':
      case 'hide-unselected':
      case 'reveal': {
        const op = editing ? visibilityOp(objects, selected, action) : null
        if (op) edit([op], action === 'hide' ? label('隐藏', selected) : action === 'reveal' ? '全部显示' : '隐藏未选物件')
        return
      }
      case 'clear-location':
      case 'clear-rotation':
      case 'clear-scale': {
        const what = action === 'clear-location' ? 'location' : action === 'clear-rotation' ? 'rotation' : 'scale'
        const op = editing ? clearOp(objects, movable(), what) : null
        if (op?.op === 'update_objects')
          edit([op], label(what === 'location' ? '清除位置' : what === 'rotation' ? '清除旋转' : '清除缩放', op.updates.map((u) => u.object_id)))
        return
      }
      case 'group':
      case 'ungroup':
      case 'parent':
      case 'clear-parent':
        return editing && regroup(action)
      case 'rename': {
        if (!editing || !active) return
        const p = handle.current?.pointer() ?? { x: 200, y: 200 }
        return ui.setState({ popup: { kind: 'rename', x: p.x, y: p.y, id: active.id } })
      }
      case 'select-all':
        return ui.setState({ selection: selectAll(objects, u.selection) })
      case 'deselect-all':
        return ui.setState({ selection: emptySelection })
      case 'toggle-tool':
        return ui.setState({ tool: u.tool === 'box' ? 'tweak' : 'box' })
      case 'toggle-toolbar':
        return ui.setState({ toolbar: !u.toolbar })
      case 'toggle-sidebar':
        return ui.setState({ sidebar: !u.sidebar })
      case 'view-front':
      case 'view-back':
      case 'view-right':
      case 'view-left':
      case 'view-top':
      case 'view-bottom':
        return handle.current?.setView(viewDirections[action])
      case 'view-ortho':
        if (u.cameraView) ui.setState({ cameraView: null })
        return handle.current?.toggleOrtho()
      case 'camera-view':
        return cameraView()
      case 'camera-to-view':
        return editing && cameraToView()
      case 'frame-selected':
        return selected.length && handle.current?.frame(selected)
      case 'frame-all': {
        const visible = (shown?.objects ?? []).filter((o) => o.visible)
        const withoutGround = visible.filter((o) => o.primitive?.builtin !== groundBuiltin)
        return handle.current?.frame((withoutGround.length ? withoutGround : visible).map((o) => o.id))
      }
      case 'view-menu':
        return openMenu('view')
      case 'undo':
        if (editing) undo.undo()
        return
      case 'redo':
        if (editing) undo.redo()
        return
      case 'save':
        return notify('场景会自动保存，不需要手动保存')
      case 'shortcuts':
        return ui.setState({ sheet: !u.sheet })
      case 'escape': {
        if (u.pickFocus) return ui.setState({ pickFocus: null })
        if (u.exporting) return exportRun.busy || ui.setState({ exporting: false })
        const target = escapeTarget({ modal: false, menu: !!u.popup || u.sheet, preview: !!u.preview, selection: selected.length > 0 })
        if (target === 'close-menu') ui.setState({ popup: null, sheet: false })
        else if (target === 'exit-preview') ui.setState({ preview: null })
        else if (target === 'deselect') ui.setState({ selection: emptySelection })
        return
      }
    }
  }

  const onKeyDown = (e: ReactKeyboardEvent) => {
    // Keys never reach the canvas behind the editor, matched or not.
    e.stopPropagation()
    if (typing(e.nativeEvent)) return
    const native = e.nativeEvent
    if (modifierKeys.has(native.code)) handle.current?.modifiers(native)
    const context = contextOf(ui.getState())
    if (context === 'modal' && handle.current?.modalNumeric(native)) {
      e.preventDefault()
      return
    }
    handleKey(native, context, mac, run)
  }
  const onKeyUp = (e: ReactKeyboardEvent) => {
    e.stopPropagation()
    if (modifierKeys.has(e.nativeEvent.code)) handle.current?.modifiers(e.nativeEvent)
  }
  // Enter in a field or a closed popup drops focus on the page body, where
  // shortcuts stop working; hand it back to the editor.
  const onBlur = (e: ReactFocusEvent) => {
    if (e.relatedTarget || e.target === root.current) return
    queueMicrotask(() => {
      if (document.activeElement === document.body) root.current?.focus({ preventScroll: true })
    })
  }
  const onPointerDown = (e: ReactPointerEvent) => {
    if (e.target instanceof HTMLCanvasElement) root.current?.focus({ preventScroll: true })
  }

  const menuItems = (menu: MenuKind, at?: { x: number; y: number }): MenuItem[] => {
    const none = !selected.length
    const k = (a: Action) => shortcutFor(a, mac)
    if (menu === 'add') {
      // The ground comes last: a scene already has one, and a second is rare.
      const item = (b: (typeof builtins)[number]) => ({ label: b.label, run: () => addPrimitive(b.builtin, at) })
      return [
        { section: '基础体' },
        ...builtins.filter((b) => b.builtin !== groundBuiltin).map(item),
        'separator',
        ...builtins.filter((b) => b.builtin === groundBuiltin).map(item),
        'separator',
        { label: '代码模型…', run: () => openCode({ kind: 'new', position: handle.current?.groundPoint(at) ?? [0, 0, 0] }) },
        { section: '其他' },
        { label: '摄像机', run: addCamera },
        { label: '空物体', run: addEmpty },
      ]
    }
    if (menu === 'object')
      return [
        { label: '复制', shortcut: k('duplicate'), disabled: none, run: () => run('duplicate') },
        { label: '删除', shortcut: k('delete-confirm'), disabled: none, run: () => remove(topmost(selected, objects)) },
        'separator',
        { label: '隐藏', shortcut: k('hide'), disabled: none, run: () => run('hide') },
        { label: '隐藏未选', shortcut: k('hide-unselected'), run: () => run('hide-unselected') },
        { label: '全部显示', shortcut: k('reveal'), run: () => run('reveal') },
        'separator',
        { label: '清除位置', shortcut: k('clear-location'), disabled: none, run: () => run('clear-location') },
        { label: '清除旋转', shortcut: k('clear-rotation'), disabled: none, run: () => run('clear-rotation') },
        { label: '清除缩放', shortcut: k('clear-scale'), disabled: none, run: () => run('clear-scale') },
        { label: '重命名', shortcut: k('rename'), disabled: !active, run: () => run('rename') },
        'separator',
        { label: '编组', shortcut: k('group'), disabled: none, run: () => run('group') },
        { label: '取消编组', shortcut: k('ungroup'), disabled: !selected.some((id) => byID.get(id)?.kind === 'group'), run: () => run('ungroup') },
        { label: '设置父级', shortcut: k('parent'), disabled: active?.kind !== 'group' || selected.length < 2, run: () => run('parent') },
        { label: '清除父级', shortcut: k('clear-parent'), disabled: !selected.some((id) => byID.get(id)?.parent_id), run: () => run('clear-parent') },
        'separator',
        { label: '编辑代码', disabled: active?.kind !== 'model_instance', run: () => active && openCode({ kind: 'object', id: active.id }) },
      ]
    const guessed = u.guessed === 'trackpad' ? '触控板' : '鼠标'
    return [
      { label: '前视', shortcut: k('view-front'), run: () => run('view-front') },
      { label: '后视', shortcut: k('view-back'), run: () => run('view-back') },
      { label: '右视', shortcut: k('view-right'), run: () => run('view-right') },
      { label: '左视', shortcut: k('view-left'), run: () => run('view-left') },
      { label: '顶视', shortcut: k('view-top'), run: () => run('view-top') },
      { label: '底视', shortcut: k('view-bottom'), run: () => run('view-bottom') },
      { label: u.ortho ? '切换为透视' : '切换为正交', shortcut: k('view-ortho'), run: () => run('view-ortho') },
      { label: '相机视图', shortcut: k('camera-view'), run: () => run('camera-view') },
      { label: '摄像机对齐当前视角', shortcut: k('camera-to-view'), disabled: readOnly || active?.kind !== 'camera', run: () => run('camera-to-view') },
      'separator',
      { label: '聚焦选中', shortcut: k('frame-selected'), disabled: none, run: () => run('frame-selected') },
      { label: '显示全部', shortcut: k('frame-all'), run: () => run('frame-all') },
      { section: '导航设备' },
      { label: `自动（当前推测：${guessed}）`, checked: u.device === 'auto', run: () => setDevice('auto') },
      { label: '鼠标', checked: u.device === 'mouse', run: () => setDevice('mouse') },
      { label: '触控板', checked: u.device === 'trackpad', run: () => setDevice('trackpad') },
      'separator',
      { label: '显示工具栏', shortcut: k('toggle-toolbar'), checked: u.toolbar, run: () => run('toggle-toolbar') },
      { label: '显示侧栏', shortcut: k('toggle-sidebar'), checked: u.sidebar, run: () => run('toggle-sidebar') },
    ]
  }

  const contextItems = (objectID: string | null, at: { x: number; y: number }): MenuItem[] => {
    if (!objectID) return menuItems('add', at)
    return menuItems('object').filter((i) => i === 'separator' || 'section' in i || !i.disabled)
  }

  const hint = (() => {
    const m = mac ? '⌘' : 'Ctrl'
    if (u.modal) return `${u.modal}  ·  X/Y/Z 锁轴 · ${m} 吸附 · Shift 精细 · 输入数字 · 左键确认 · 右键取消`
    if (u.pickFocus) return '点选要对焦的物件（基础体、模型实例或空物体） · Esc 取消'
    if (u.preview) return `正在预览第 ${u.preview.revision} 版 · 只读`
    if (s.trashed) return '场景在回收站，请先恢复后再编辑'
    if (selected.length) return 'G 移动 · R 旋转 · S 缩放 · Shift+D 复制 · X 删除'
    return '左键选择 · 拖动框选 · 中键环绕（Alt+左键） · Shift+A 添加'
  })()
  const deviceText = `导航：${u.device === 'auto' ? `${u.guessed === 'trackpad' ? '触控板' : '鼠标'}（自动）` : u.device === 'trackpad' ? '触控板' : '鼠标'}`

  const restore = async () => {
    const preview = ui.getState().preview
    setConfirmRestore(false)
    if (!preview) return
    const result = await session.restoreWhole(preview.revision)
    if (result === 'restored') {
      ui.setState({ preview: null, history: false, notice: `已恢复到第 ${preview.revision} 版` })
    } else
      notify(result === 'conflict' ? '场景刚被修改，请重新选择版本' : result === 'busy' ? '还有修改在保存，请稍后再恢复' : '恢复失败，请稍后再试')
  }

  return (
    <section ref={root} tabIndex={-1} className="cc-maximized-document cc-surface cc-scene-editor" aria-label="3D 场景编辑" onKeyDown={onKeyDown} onKeyUp={onKeyUp} onBlur={onBlur} onPointerDownCapture={onPointerDown}>
      <header>
        <Box size={18} />
        <h2>{shown && s.base ? (s.view?.title ?? s.base.title) : '3D 场景'}</h2>
        <MenuBar onOpen={(menu, x, y) => openMenu(menu, x, y)} disabled={(menu) => readOnly && menu !== 'view'} />
        {canvasID && sceneNodeID && (
          <button className="btn" disabled={readOnly} onClick={() => ui.setState({ exporting: true })} title="导出分镜到画布">
            <ImageDown size={16} />
            导出
          </button>
        )}
        <button className="btn" aria-pressed={u.history} onClick={() => ui.setState({ history: !u.history, preview: null, code: null })} title="历史版本">
          <History size={16} />
          历史
        </button>
        <button className="btn" aria-label="快捷键速查" title={`快捷键速查 · ${shortcutFor('shortcuts', mac)}`} onClick={() => ui.setState({ sheet: !u.sheet })}>
          <CircleHelp size={16} />
        </button>
        <button className="btn" onClick={close}>
          <Minimize2 size={16} />
          还原节点
        </button>
      </header>
      <div className="cc-scene-body">
        {!webgl ? (
          <p role="alert" className="cc-scene-message">
            当前浏览器无法显示 3D 场景，请使用最新版 Chrome、Edge 或 Safari
          </p>
        ) : s.missing ? (
          <div role="alert" className="cc-scene-message">
            <p>场景已不存在，未保存的修改已丢失</p>
            <button className="btn" onClick={onClose}>
              回到画布
            </button>
          </div>
        ) : !s.base || !shown ? (
          <div role={s.readError ? 'alert' : 'status'} className="cc-scene-message">
            <p>{s.readError || '正在读取场景…'}</p>
            {s.readError && (
              <button className="btn" onClick={() => void session.start()}>
                重新读取
              </button>
            )}
          </div>
        ) : (
          <>
            {u.toolbar && <Toolbar tool={u.tool} disabled={readOnly} onTool={(tool) => ui.setState({ tool })} />}
            {u.sidebar && (
              <aside className="cc-scene-sidebar" aria-label="侧栏">
                <Outliner
                  objects={shown.objects}
                  selection={u.selection}
                  highlighted={u.preview ? noObjects : highlighted}
                  readOnly={readOnly}
                  onSelect={(id, mods, order) => ui.setState({ selection: listClick(u.selection, id, order, mods) })}
                  onToggle={(id, field, value) => edit([{ op: 'update_objects', updates: [{ object_id: id, [field]: value }] }], `${value ? (field === 'visible' ? '显示' : '锁定') : field === 'visible' ? '隐藏' : '解锁'}「${byID.get(id)?.name || '物件'}」`)}
                  onDrop={(moving, target, place) => drop(moving, target, place)}
                />
                <Properties
                  object={shownActive}
                  count={selected.length}
                  readOnly={readOnly}
                  model={shownModel ? models[shownModel] : undefined}
                  modelFailed={!!shownModel && !!failedModels[shownModel]}
                  onUpdate={(update: ObjectUpdate, text, merge) => edit([{ op: 'update_objects', updates: [update] }], text, merge ? { merge } : undefined)}
                  objects={shown.objects}
                  focusDistance={shownActive?.kind === 'camera' ? focusOf(shown, shownActive.id) : undefined}
                  picking={!!u.pickFocus && u.pickFocus === shownActive?.id}
                  onPickFocus={() => shownActive && ui.setState({ pickFocus: u.pickFocus ? null : shownActive.id })}
                >
                  {shownActive?.kind === 'model_instance' && shownModel && (
                    <ModelParams
                      key={shownActive.id}
                      generator={models[shownModel]?.generator ?? null}
                      sharedWith={u.preview ? 0 : sharingInstances(objects, shownActive.id).length}
                      readOnly={readOnly}
                      onCommit={(params, all) => retune(shownActive, params, all)}
                      onEditCode={() => openCode({ kind: 'object', id: shownActive.id })}
                    />
                  )}
                </Properties>
              </aside>
            )}
            <div className="cc-scene-main">
              {u.preview && (
                <div className="cc-scene-banner" role="status">
                  正在预览第 {u.preview.revision} 版 · 只读
                  <button className="btn" disabled={s.queue.length > 0} onClick={() => setConfirmRestore(true)}>
                    恢复到此版本
                  </button>
                  <button className="btn" onClick={() => ui.setState({ preview: null })}>
                    退出预览
                  </button>
                </div>
              )}
              {s.trashed && (
                <div className="cc-scene-banner" role="status">
                  场景在回收站，请先在画布上恢复后再编辑
                </div>
              )}
              <ConflictPrompt groups={s.conflicts} trashed={s.trashed} onReapply={(id) => session.reapply(id)} onDiscard={(id) => session.discard(id)} />
              <Viewport
                scene={drawn ?? shown}
                models={models}
                ghost={u.preview ? null : baking.ghost}
                highlighted={u.preview ? noObjects : highlighted}
                placeholders={u.preview ? undefined : placeholders}
                readOnly={readOnly}
                ui={ui}
                mac={mac}
                handle={handle}
                camera={view.saved?.camera ?? null}
                onCamera={(camera) => view.saver.camera(camera)}
                onPick={(id, shift) => pickFocus(id) || ui.setState({ selection: click(ui.getState().selection, id, shift) })}
                onBox={(ids, mode) => ui.setState({ selection: boxSelect(ui.getState().selection, ids, mode) })}
                onCommit={(results: LocalTransform[], text, held) => {
                  edit([transformOp(results)], text)
                  if (held) session.release(held)
                }}
                onCancelHeld={(held) => session.release(held)}
                onContextMenu={(x, y, objectID) => {
                  if (objectID && !ui.getState().selection.selected.includes(objectID)) ui.setState({ selection: click(emptySelection, objectID, false) })
                  ui.setState({ popup: { kind: 'menu', menu: objectID && !readOnly ? 'object' : readOnly ? 'view' : 'add', x, y } })
                }}
                onDragging={(dragging) => session.setDragging(dragging)}
              />
              <StatusBar hint={hint} status={s.status} notice={u.notice} device={deviceText} activity={activity} />
              <FirstRunHint onShortcuts={() => ui.setState({ sheet: true })} />
            </div>
            {u.code && !u.history && (
              <CodePanel
                key={u.code.opened}
                title={u.code.kind === 'new' ? '新建代码模型' : `「${byID.get(u.code.id)?.name || '代码模型'}」的代码`}
                isNew={u.code.kind === 'new'}
                loadCode={() => {
                  const target = ui.getState().code
                  if (!target || target.kind === 'new') return Promise.resolve(defaultModuleCode)
                  const rev = byID.get(target.id)?.model?.content_revision_id
                  return rev ? modelCache.generator(rev).then((g) => g.code) : Promise.reject(new Error('这个物件不是代码模型'))
                }}
                status={baking.status[u.code.kind === 'new' ? 'new' : u.code.id]}
                readOnly={readOnly}
                onGenerate={(code) => void generate(code)}
                onClose={() => ui.setState({ code: null })}
              />
            )}
            {u.history && (
              <HistoryPanel
                documentID={documentID}
                revision={s.base.revision}
                previewing={u.preview?.revision ?? null}
                onPreview={(revision, scene, models) => ui.setState({ preview: { revision, scene, models }, selection: emptySelection })}
                onClose={() => ui.setState({ history: false, preview: null })}
              />
            )}
          </>
        )}
      </div>
      {u.popup?.kind === 'menu' && (
        <ContextMenu
          x={u.popup.x}
          y={u.popup.y}
          items={u.popup.menu === 'object' ? contextItems(active?.id ?? null, u.popup) : menuItems(u.popup.menu, u.popup)}
          onClose={() => {
            ui.setState({ popup: null })
            root.current?.focus()
          }}
        />
      )}
      {u.popup?.kind === 'delete' && (
        <ContextMenu
          x={u.popup.x}
          y={u.popup.y}
          items={[{ label: `删除 ${u.popup.ids.length} 个物件`, run: () => u.popup?.kind === 'delete' && remove(u.popup.ids) }]}
          onClose={() => {
            ui.setState({ popup: null })
            root.current?.focus()
          }}
        />
      )}
      {u.popup?.kind === 'rename' && (
        <RenamePopup
          x={u.popup.x}
          y={u.popup.y}
          name={byID.get(u.popup.id)?.name ?? ''}
          onDone={(name) => {
            const p = ui.getState().popup
            ui.setState({ popup: null })
            root.current?.focus()
            if (name !== null && p?.kind === 'rename') edit([{ op: 'update_objects', updates: [{ object_id: p.id, name }] }], '重命名')
          }}
        />
      )}
      {u.sheet && <ShortcutSheet mac={mac} onClose={() => ui.setState({ sheet: false })} />}
      {u.exporting && (
        <ExportDialog
          cameras={objects.filter((o) => o.kind === 'camera')}
          busy={exportRun.busy}
          progress={exportRun.progress}
          onExport={(plan) => void startExport(plan)}
          onClose={() => ui.setState({ exporting: false })}
        />
      )}
      {u.confirmClose && (
        <ConfirmDialog
          title="有修改因冲突没有保存"
          body={`有 ${session.blockedCount()} 处修改因冲突没有保存，关闭后会丢失。`}
          confirmLabel="仍然关闭"
          cancelLabel="返回处理"
          danger
          onConfirm={() => {
            session.discardConflicts()
            ui.setState({ confirmClose: false })
            onClose()
          }}
          onCancel={() => ui.setState({ confirmClose: false })}
        />
      )}
      {confirmRestore && u.preview && (
        <ConfirmDialog
          title={`恢复到第 ${u.preview.revision} 版`}
          body="场景会整体回到这一版，产生一个新版本；之后仍可以在历史里恢复回来。撤销记录会清空。"
          confirmLabel="恢复"
          onConfirm={() => void restore()}
          onCancel={() => setConfirmRestore(false)}
        />
      )}
    </section>
  )
}

function RenamePopup({ x, y, name, onDone }: { x: number; y: number; name: string; onDone: (name: string | null) => void }) {
  const [text, setText] = useState(name)
  return (
    <div className="cc-scene-menu cc-scene-rename" style={{ left: Math.min(x, window.innerWidth - 260), top: y }}>
      <input
        autoFocus
        value={text}
        maxLength={80}
        aria-label="物件名称"
        onChange={(e) => setText(e.target.value)}
        onBlur={() => onDone(null)}
        onKeyDown={(e) => {
          e.stopPropagation()
          if (e.key === 'Enter') onDone(text === name ? null : text)
          if (e.key === 'Escape') onDone(null)
        }}
      />
    </div>
  )
}

/** Where a camera focuses, as the export resolves it. */
function focusOf(scene: Scene, id: string): number | undefined {
  const framing = cameraFraming(scene, id)
  return 'error' in framing ? undefined : framing.optics?.focusM
}
