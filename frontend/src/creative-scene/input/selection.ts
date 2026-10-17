import type { SceneObject } from '../api.ts'

// Selection is UI state: never in the document, kept in this browser for the
// next visit (viewState.ts). Blender semantics: Shift+click
// adds, makes active, or removes the active object; box select replaces,
// adds or subtracts.

export type Selection = { selected: string[]; active: string | null }

export const emptySelection: Selection = { selected: [], active: null }

/** Only visible, unlocked objects can be picked in the viewport. */
export function pickable(o: Pick<SceneObject, 'visible' | 'locked'>): boolean {
  return o.visible && !o.locked
}

export function click(sel: Selection, id: string | null, shift: boolean): Selection {
  if (!id) return shift ? sel : emptySelection
  if (!shift) return { selected: [id], active: id }
  if (!sel.selected.includes(id)) return { selected: [...sel.selected, id], active: id }
  if (sel.active !== id) return { ...sel, active: id }
  const selected = sel.selected.filter((x) => x !== id)
  return { selected, active: selected.at(-1) ?? null }
}

export function boxSelect(sel: Selection, ids: readonly string[], mode: 'set' | 'add' | 'subtract'): Selection {
  if (mode === 'set') return { selected: [...ids], active: ids.includes(sel.active ?? '') ? sel.active : (ids[0] ?? null) }
  if (mode === 'add') {
    const selected = [...sel.selected, ...ids.filter((id) => !sel.selected.includes(id))]
    return { selected, active: sel.active ?? selected[0] ?? null }
  }
  const selected = sel.selected.filter((id) => !ids.includes(id))
  return { selected, active: sel.active && selected.includes(sel.active) ? sel.active : (selected.at(-1) ?? null) }
}

export function selectAll(objects: readonly SceneObject[], sel: Selection): Selection {
  const selected = objects.filter(pickable).map((o) => o.id)
  return { selected, active: sel.active && selected.includes(sel.active) ? sel.active : (selected[0] ?? null) }
}

/**
 * The object list: click selects, mod+click toggles, Shift+click selects the
 * range from the active object in list order.
 */
export function listClick(
  sel: Selection,
  id: string,
  order: readonly string[],
  mods: { mod: boolean; shift: boolean },
): Selection {
  if (mods.shift && sel.active) {
    const a = order.indexOf(sel.active)
    const b = order.indexOf(id)
    if (a >= 0 && b >= 0) {
      const range = order.slice(Math.min(a, b), Math.max(a, b) + 1)
      return { selected: [...sel.selected, ...range.filter((x) => !sel.selected.includes(x))], active: id }
    }
  }
  if (mods.mod) {
    if (!sel.selected.includes(id)) return { selected: [...sel.selected, id], active: id }
    const selected = sel.selected.filter((x) => x !== id)
    return { selected, active: sel.active === id ? (selected.at(-1) ?? null) : sel.active }
  }
  return { selected: [id], active: id }
}

/** Drops objects that no longer exist after a snapshot refresh. */
export function prune(sel: Selection, objects: readonly SceneObject[]): Selection {
  const present = new Set(objects.map((o) => o.id))
  const selected = sel.selected.filter((id) => present.has(id))
  if (selected.length === sel.selected.length) return sel
  return { selected, active: sel.active && present.has(sel.active) ? sel.active : (selected.at(-1) ?? null) }
}

/** The selected objects with no selected ancestor: what a transform moves. */
export function topmost(selected: readonly string[], objects: readonly SceneObject[]): string[] {
  const parent = new Map(objects.map((o) => [o.id, o.parent_id ?? null]))
  const chosen = new Set(selected)
  return selected.filter((id) => {
    for (let p = parent.get(id) ?? null, guard = 0; p && guard < objects.length; p = parent.get(p) ?? null, guard++)
      if (chosen.has(p)) return false
    return parent.has(id)
  })
}
