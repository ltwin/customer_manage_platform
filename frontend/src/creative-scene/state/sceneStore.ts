import { createStore } from 'zustand/vanilla'
import type { Scene } from '../api.ts'
import type { Context } from '../input/keymap.ts'
import type { SceneModel } from '../models/cache.ts'
import type { Device, DevicePreference } from '../input/navigation.ts'
import { emptySelection, type Selection } from '../input/selection.ts'
import type { CameraView, SavedView } from '../viewState.ts'
import type { TraceStatus } from '../render/trace/base.ts'

// Editor UI state, never part of the document: what is selected, which tool
// and panels are showing, open menus and prompts. The selection, tool,
// panels and camera come back on the next visit (viewState.ts); document
// state lives in the session (sync.ts).

export type Tool = 'box' | 'tweak' | 'move' | 'rotate' | 'scale'

/** Composition guides drawn in the camera view's frame, never in an export. */
export type Guide = 'thirds' | 'golden' | 'diagonals' | 'center'
export const guides: readonly Guide[] = ['thirds', 'golden', 'diagonals', 'center']

export type MenuKind = 'add' | 'object' | 'view' | 'display'

/**
 * Blender's viewport shading. Solid and rendered are the clay and lit looks
 * the exports use; wireframe and material preview are for editing only.
 */
export type DisplayMode = 'wireframe' | 'solid' | 'preview' | 'rendered'
export type Look = 'clay' | 'lit'
export const displayModes: readonly { mode: DisplayMode; label: string }[] = [
  { mode: 'wireframe', label: '线框' },
  { mode: 'solid', label: '白模' },
  { mode: 'preview', label: '材质预览' },
  { mode: 'rendered', label: '最终渲染' },
]

export type Popup =
  | { kind: 'menu'; menu: MenuKind; x: number; y: number }
  | { kind: 'delete'; x: number; y: number; ids: string[] }
  | { kind: 'rename'; x: number; y: number; id: string }

/**
 * What the code panel edits: a model about to be created at a point on the
 * ground, or a model instance. `opened` changes each time the panel is
 * opened anew; a new model becoming an instance keeps the same panel.
 */
export type CodeTarget = ({ kind: 'new'; position: [number, number, number] } | { kind: 'object'; id: string }) & { opened: number }

export type SidebarTab = 'outliner' | 'assets'

/** The properties area's tabs, in Blender's order (R2 design「属性区选项卡」). */
export type PropertiesTab = 'render' | 'environment' | 'object'
export type SceneTab = Exclude<PropertiesTab, 'object'>

/** The viewport's renderer, for the render tab to show. */
export type Backend = { kind: 'webgpu' | 'webgl2'; compatibility: boolean }

/** A revision from the history list, with the models its snapshot names. */
export type Preview = { revision: string; scene: Scene; models: Record<string, SceneModel> }

export type EditorUI = {
  selection: Selection
  tool: Tool
  toolbar: boolean
  sidebar: boolean
  /** What the sidebar's top half shows: the object list or the asset panel. */
  sidebarTab: SidebarTab
  /** What the properties area shows; not saved with the view. */
  propertiesTab: PropertiesTab
  /** The scene tab last stayed on: clearing the selection goes back to it from the object tab. */
  sceneTab: SceneTab
  /** The viewport's renderer once made. */
  backend: Backend | null
  /**
   * The viewport's world-space layer (render/trace/base.ts): off and why
   * from the start, the rest once the real-time GI's lit look has drawn
   * (the render panel's notes, S1 design「降级与提示」).
   */
  trace: TraceStatus | null
  /** The layer ready, the viewport's probes still warming in the background (S2 design「就绪的那一刻」): the panel says it prepares. */
  probesWarming: boolean
  popup: Popup | null
  sheet: boolean
  history: boolean
  /** The code panel; it and the history panel share one place. */
  code: CodeTarget | null
  preview: Preview | null
  /** A G / R / S transform is running; its text goes to the status bar. */
  modal: string | null
  boxing: boolean
  notice: string
  device: DevicePreference
  guessed: Device | null
  ortho: boolean
  /** Closing while edits wait on a conflict asks first. */
  confirmClose: boolean
  /** The camera the viewport looks through (0), or null for free navigation. */
  cameraView: string | null
  /** The camera 0 goes to when none is selected. */
  lastCamera: string | null
  /** The free view the camera view was entered from: 0 again goes back to it, navigating by hand drops it. */
  viewBack: CameraView | null
  guides: Guide[]
  /** A camera waiting for a click on the object to focus on. */
  pickFocus: string | null
  /** The export dialog is open. */
  exporting: boolean
  /** 景深预览: the camera view blurs as the camera's depth of field does, until turned off. */
  dofPreview: boolean
  /** The shading this browser chose for the scene; null until chosen, then render.mode decides. */
  display: DisplayMode | null
  /** Where Shift+Z goes back to from wireframe. */
  wireBack: DisplayMode
}

export function createEditorStore(device: DevicePreference, saved: SavedView | null = null) {
  return createStore<EditorUI>(() => ({
    selection: saved?.selection ?? emptySelection,
    tool: saved?.tool ?? 'box',
    toolbar: saved?.toolbar ?? true,
    sidebar: saved?.sidebar ?? true,
    sidebarTab: saved?.sidebarTab ?? 'outliner',
    propertiesTab: 'environment',
    sceneTab: 'environment',
    backend: null,
    trace: null,
    probesWarming: false,
    popup: null,
    sheet: false,
    history: saved?.history ?? false,
    code: null,
    preview: null,
    modal: null,
    boxing: false,
    notice: '',
    device,
    guessed: null,
    ortho: saved?.camera?.ortho ?? false,
    confirmClose: false,
    cameraView: null,
    lastCamera: saved?.lastCamera ?? null,
    viewBack: null,
    guides: saved?.guides ?? [],
    pickFocus: null,
    exporting: false,
    dofPreview: false,
    display: saved?.display ?? null,
    wireBack: saved?.wireBack ?? 'solid',
  }))
}

/**
 * The properties tab once the selection changes (R2 design「属性区选项卡」):
 * selecting where nothing was selected shows the object; clearing the
 * selection from the object tab goes back to the scene tab last stayed on.
 * A tab picked by hand stays until the next such change.
 */
export function tabAfterSelection(ui: Pick<EditorUI, 'propertiesTab' | 'sceneTab'>, had: boolean, has: boolean): PropertiesTab {
  if (!had && has) return 'object'
  if (had && !has && ui.propertiesTab === 'object') return ui.sceneTab
  return ui.propertiesTab
}

/** The shading shown: the one chosen here, or the scene's suggested look. */
export function displayOf(ui: Pick<EditorUI, 'display'>, render: { mode: string } | undefined): DisplayMode {
  return ui.display ?? (render?.mode === 'lit' ? 'rendered' : 'solid')
}

/** Shift+Z: wireframe and back to the mode before it. */
export function toggleWireframe(ui: Pick<EditorUI, 'wireBack'>, shown: DisplayMode): Pick<EditorUI, 'display' | 'wireBack'> {
  return shown === 'wireframe' ? { display: ui.wireBack, wireBack: ui.wireBack } : { display: 'wireframe', wireBack: shown }
}

export type EditorStore = ReturnType<typeof createEditorStore>

/** Which keymap context applies now; the innermost layer wins. */
export function contextOf(ui: EditorUI): Context {
  if (ui.modal) return 'modal'
  if (ui.popup || ui.exporting) return 'menu'
  if (ui.boxing) return 'boxselect'
  if (ui.preview) return 'preview'
  return 'idle'
}
