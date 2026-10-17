import { useCallback, useEffect, useMemo, useRef, useState, useSyncExternalStore, type FocusEvent as ReactFocusEvent, type KeyboardEvent as ReactKeyboardEvent, type PointerEvent as ReactPointerEvent } from 'react'
import { Box, CircleHelp, History, Minimize2 } from 'lucide-react'
import { useStore } from 'zustand'
import ConfirmDialog from '../components/ConfirmDialog'
import { currentAccount } from '../creative-canvas/editor/api.ts'
import type { ObjectUpdate, Scene } from './api.ts'
import { builtins } from './builtins/catalog.ts'
import { clearOp, duplicate, groundBuiltin, newPrimitive, objectsLabel, transformOp, visibilityOp } from './editing.ts'
import { escapeTarget, handleKey, isMac, shortcutFor, typing, type Action } from './input/keymap.ts'
import { readPreference, viewDirections, writePreference, type DevicePreference } from './input/navigation.ts'
import { boxSelect, click, emptySelection, listClick, prune, selectAll, topmost } from './input/selection.ts'
import { ContextMenu, type MenuItem } from './panels/ContextMenu.tsx'
import { ConflictPrompt } from './panels/ConflictPrompt.tsx'
import { FirstRunHint } from './panels/FirstRunHint.tsx'
import { modelCache, modelSummaries } from './models/cache.ts'
import { HistoryPanel } from './panels/HistoryPanel.tsx'
import { MenuBar } from './panels/MenuBar.tsx'
import { Outliner } from './panels/Outliner.tsx'
import { Properties } from './panels/Properties.tsx'
import { ShortcutSheet } from './panels/ShortcutSheet.tsx'
import { StatusBar } from './panels/StatusBar.tsx'
import { Toolbar } from './panels/Toolbar.tsx'
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

const modifierKeys = new Set(['ShiftLeft', 'ShiftRight', 'ControlLeft', 'ControlRight', 'MetaLeft', 'MetaRight', 'AltLeft', 'AltRight'])

export default function SceneEditor({ documentID, onClose }: { documentID: string; onClose: () => void }) {
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
  const edit = (ops: WireOp[], label: string, options?: Parameters<typeof session.edit>[2]) => session.edit(ops, label, options)
  const select = (ids: string[]) => ui.setState({ selection: { selected: ids, active: ids.at(-1) ?? null } })
  /** The selection a transform moves: topmost, unlocked. */
  const movable = () => topmost(selected, objects).filter((id) => !byID.get(id)?.locked)
  const label = (verb: string, ids: string[]) => objectsLabel(verb, ids.length, byID.get(ids[0])?.name)

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
        return handle.current?.toggleOrtho()
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
        <button className="btn" aria-pressed={u.history} onClick={() => ui.setState({ history: !u.history, preview: null })} title="历史版本">
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
                  readOnly={readOnly}
                  onSelect={(id, mods, order) => ui.setState({ selection: listClick(u.selection, id, order, mods) })}
                  onToggle={(id, field, value) => edit([{ op: 'update_objects', updates: [{ object_id: id, [field]: value }] }], `${value ? (field === 'visible' ? '显示' : '锁定') : field === 'visible' ? '隐藏' : '解锁'}「${byID.get(id)?.name || '物件'}」`)}
                />
                <Properties
                  object={shownActive}
                  count={selected.length}
                  readOnly={readOnly}
                  model={shownModel ? models[shownModel] : undefined}
                  modelFailed={!!shownModel && !!failedModels[shownModel]}
                  onUpdate={(update: ObjectUpdate, text, merge) => edit([{ op: 'update_objects', updates: [update] }], text, merge ? { merge } : undefined)}
                />
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
                scene={shown}
                models={models}
                readOnly={readOnly}
                ui={ui}
                mac={mac}
                handle={handle}
                camera={view.saved?.camera ?? null}
                onCamera={(camera) => view.saver.camera(camera)}
                onPick={(id, shift) => ui.setState({ selection: click(ui.getState().selection, id, shift) })}
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
              <StatusBar hint={hint} status={s.status} notice={u.notice} device={deviceText} />
              <FirstRunHint onShortcuts={() => ui.setState({ sheet: true })} />
            </div>
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
