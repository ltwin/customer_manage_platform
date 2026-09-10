import type * as api from './api.ts'

export function newerCanvas(old: api.Canvas | null, c: api.Canvas | null) {
  if (!c || old?.id !== c.id) return c
  return BigInt(c.revision) < BigInt(old.revision) ||
    BigInt(c.project_revision) < BigInt(old.project_revision) ||
    JSON.stringify(c) === JSON.stringify(old)
    ? old
    : c
}

export function matchesCanvas(canvas: api.Canvas | null, id: string): boolean {
  return !!canvas && canvas.id === id
}
