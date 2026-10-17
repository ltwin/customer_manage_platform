import { createStore } from 'zustand/vanilla'
import type { Scene } from '../api.ts'
import type { Context } from '../input/keymap.ts'
import type { Device, DevicePreference } from '../input/navigation.ts'
import { emptySelection, type Selection } from '../input/selection.ts'

// Editor UI state that is never saved: what is selected, which tool and
// panels are showing, open menus and prompts. Document state lives in the
// session (sync.ts).

export type Tool = 'box' | 'tweak' | 'move' | 'rotate' | 'scale'

export type MenuKind = 'add' | 'object' | 'view'

export type Popup =
  | { kind: 'menu'; menu: MenuKind; x: number; y: number }
  | { kind: 'delete'; x: number; y: number; ids: string[] }
  | { kind: 'rename'; x: number; y: number; id: string }

export type Preview = { revision: string; scene: Scene }

export type EditorUI = {
  selection: Selection
  tool: Tool
  toolbar: boolean
  sidebar: boolean
  popup: Popup | null
  sheet: boolean
  history: boolean
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
}

export function createEditorStore(device: DevicePreference) {
  return createStore<EditorUI>(() => ({
    selection: emptySelection,
    tool: 'box',
    toolbar: true,
    sidebar: true,
    popup: null,
    sheet: false,
    history: false,
    preview: null,
    modal: null,
    boxing: false,
    notice: '',
    device,
    guessed: null,
    ortho: false,
    confirmClose: false,
  }))
}

export type EditorStore = ReturnType<typeof createEditorStore>

/** Which keymap context applies now; the innermost layer wins. */
export function contextOf(ui: EditorUI): Context {
  if (ui.modal) return 'modal'
  if (ui.popup) return 'menu'
  if (ui.boxing) return 'boxselect'
  if (ui.preview) return 'preview'
  return 'idle'
}
