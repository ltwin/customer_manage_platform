import { createStore } from 'zustand/vanilla'
import type { Scene } from '../api.ts'
import type { Context } from '../input/keymap.ts'
import type { SceneModel } from '../models/cache.ts'
import type { Device, DevicePreference } from '../input/navigation.ts'
import { emptySelection, type Selection } from '../input/selection.ts'
import type { SavedView } from '../viewState.ts'

// Editor UI state, never part of the document: what is selected, which tool
// and panels are showing, open menus and prompts. The selection, tool,
// panels and camera come back on the next visit (viewState.ts); document
// state lives in the session (sync.ts).

export type Tool = 'box' | 'tweak' | 'move' | 'rotate' | 'scale'

/** Composition guides drawn in the camera view's frame, never in an export. */
export type Guide = 'thirds' | 'golden' | 'diagonals' | 'center'
export const guides: readonly Guide[] = ['thirds', 'golden', 'diagonals', 'center']

export type MenuKind = 'add' | 'object' | 'view'

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

/** A revision from the history list, with the models its snapshot names. */
export type Preview = { revision: string; scene: Scene; models: Record<string, SceneModel> }

export type EditorUI = {
  selection: Selection
  tool: Tool
  toolbar: boolean
  sidebar: boolean
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
  guides: Guide[]
  /** A camera waiting for a click on the object to focus on. */
  pickFocus: string | null
  /** The export dialog is open. */
  exporting: boolean
  /** 景深预览: the camera view blurs as the camera's depth of field does, until turned off. */
  dofPreview: boolean
}

export function createEditorStore(device: DevicePreference, saved: SavedView | null = null) {
  return createStore<EditorUI>(() => ({
    selection: saved?.selection ?? emptySelection,
    tool: saved?.tool ?? 'box',
    toolbar: saved?.toolbar ?? true,
    sidebar: saved?.sidebar ?? true,
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
    guides: saved?.guides ?? [],
    pickFocus: null,
    exporting: false,
    dofPreview: false,
  }))
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
