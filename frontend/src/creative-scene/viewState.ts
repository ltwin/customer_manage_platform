import type { Selection } from './input/selection.ts'
import { displayModes, guides as allGuides, type DisplayMode, type EditorUI, type Guide, type SidebarTab, type Tool } from './state/sceneStore.ts'

// What the photographer was looking at when they left a scene: the camera,
// the selection, the tool and the panels. It is kept per account and scene
// in this browser, like the canvas viewport (creative-viewport:*), so coming
// back shows the editor as it was left. The document and its revisions never
// see it: moving the view is not an edit.

type Vec3 = [number, number, number]

/** In view space (Z up), as the editor's camera holds it. */
export type CameraView = { position: Vec3; target: Vec3; ortho: boolean; zoom: number }

export type SavedView = {
  camera: CameraView | null
  selection: Selection
  tool: Tool
  toolbar: boolean
  sidebar: boolean
  /** Views saved before the asset panel lack it: the object list. */
  sidebarTab?: SidebarTab
  history: boolean
  /** The camera 0 goes to; views saved before cameras had it lack it. */
  lastCamera?: string | null
  guides?: Guide[]
  /** The shading; views saved before C5 lack it and the scene's render.mode decides. */
  display?: DisplayMode | null
  wireBack?: DisplayMode
}

export const viewKey = (account: string | null, documentID: string) =>
  `creative-scene-view:${account ?? 'anonymous'}:${documentID}`

const tools: readonly Tool[] = ['box', 'tweak', 'move', 'rotate', 'scale']
const maxSelected = 5000

const vec3 = (v: unknown): v is Vec3 =>
  Array.isArray(v) && v.length === 3 && v.every((n) => typeof n === 'number' && Number.isFinite(n))

function parseCamera(v: unknown): CameraView | null {
  if (!v || typeof v !== 'object') return null
  const c = v as Record<string, unknown>
  if (!vec3(c.position) || !vec3(c.target) || typeof c.ortho !== 'boolean') return null
  if (typeof c.zoom !== 'number' || !Number.isFinite(c.zoom) || c.zoom <= 0) return null
  return { position: c.position, target: c.target, ortho: c.ortho, zoom: c.zoom }
}

/** A stored view, or null when it is missing or not one this editor wrote. */
export function parseView(v: unknown): SavedView | null {
  if (!v || typeof v !== 'object') return null
  const o = v as Record<string, unknown>
  const sel = o.selection as Record<string, unknown> | undefined
  const selected = sel?.selected
  const active = sel?.active
  if (!Array.isArray(selected) || selected.length > maxSelected || !selected.every((id) => typeof id === 'string')) return null
  if (active !== null && (typeof active !== 'string' || !selected.includes(active))) return null
  if (!tools.includes(o.tool as Tool)) return null
  if (typeof o.toolbar !== 'boolean' || typeof o.sidebar !== 'boolean' || typeof o.history !== 'boolean') return null
  return {
    camera: o.camera === null ? null : parseCamera(o.camera),
    selection: { selected: selected as string[], active: active as string | null },
    tool: o.tool as Tool,
    toolbar: o.toolbar,
    sidebar: o.sidebar,
    sidebarTab: o.sidebarTab === 'assets' ? 'assets' : 'outliner',
    history: o.history,
    lastCamera: typeof o.lastCamera === 'string' ? o.lastCamera : null,
    guides: Array.isArray(o.guides) ? allGuides.filter((g) => (o.guides as unknown[]).includes(g)) : [],
    display: displayMode(o.display),
    wireBack: displayMode(o.wireBack) ?? 'solid',
  }
}

const displayMode = (v: unknown): DisplayMode | null => displayModes.find((d) => d.mode === v)?.mode ?? null

export function readView(storage: Storage | undefined, key: string): SavedView | null {
  try {
    const raw = storage?.getItem(key)
    return raw ? parseView(JSON.parse(raw)) : null
  } catch {
    // A view preference is optional; a broken one starts fresh.
    return null
  }
}

/**
 * Collects view changes and writes them a moment later, or at once on
 * flush (the editor closing, the page going away).
 */
export class ViewSaver {
  private view: SavedView
  private timer: ReturnType<typeof setTimeout> | null = null
  private readonly storage: Storage | undefined
  private readonly key: string

  constructor(storage: Storage | undefined, key: string, initial: SavedView) {
    this.storage = storage
    this.key = key
    this.view = initial
  }

  ui(s: Pick<EditorUI, 'selection' | 'tool' | 'toolbar' | 'sidebar' | 'sidebarTab' | 'history' | 'lastCamera' | 'guides' | 'display' | 'wireBack'>) {
    const v = this.view
    if (
      v.selection === s.selection && v.tool === s.tool && v.toolbar === s.toolbar && v.sidebar === s.sidebar && (v.sidebarTab ?? 'outliner') === s.sidebarTab && v.history === s.history &&
      v.lastCamera === s.lastCamera && v.guides === s.guides && (v.display ?? null) === s.display && (v.wireBack ?? 'solid') === s.wireBack
    )
      return
    this.view = { ...v, selection: s.selection, tool: s.tool, toolbar: s.toolbar, sidebar: s.sidebar, sidebarTab: s.sidebarTab, history: s.history, lastCamera: s.lastCamera, guides: s.guides, display: s.display, wireBack: s.wireBack }
    this.later()
  }

  camera(camera: CameraView) {
    this.view = { ...this.view, camera }
    this.later()
  }

  /**
   * At most one write per 300 ms, with whatever is latest by then. The
   * camera reports every frame while a view change eases in, so waiting for
   * a quiet moment would leave a reload during it with the old view.
   */
  private later() {
    if (this.timer !== null) return
    this.timer = setTimeout(() => this.flush(), 300)
  }

  flush() {
    if (this.timer !== null) clearTimeout(this.timer)
    this.timer = null
    try {
      this.storage?.setItem(this.key, JSON.stringify(this.view))
    } catch {
      // Storage full or blocked: the next visit starts from the default view.
    }
  }
}
