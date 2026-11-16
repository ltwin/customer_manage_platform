import test from 'node:test'
import assert from 'node:assert/strict'
import { buildOutline, outlineTypes, type OutlineOptions } from '../src/creative-canvas/editor/outline.ts'
import type { CanvasNode } from '../src/creative-canvas/editor/api.ts'
import { nodeRect, revealViewport, zoomLimits } from '../src/creative-canvas/editor/viewport.ts'

const node = (
  id: string,
  type: string,
  created: string,
  extra: { parent?: string; title?: string } = {},
) =>
  ({
    id,
    parent_id: extra.parent ?? null,
    created_at: created,
    metadata: { type_key: type, type_version: 1, title: extra.title ?? id, intent: '', x: 0, y: 0, width: 100, height: 100, z_order: 0 },
  }) as CanvasNode

const none: OutlineOptions = { query: '', types: new Set(), collapsed: new Set(), pending: new Set() }
const opts = (o: Partial<OutlineOptions>): OutlineOptions => ({ ...none, ...o })
const ids = (o: ReturnType<typeof buildOutline>) => o.rows.map((r) => `${'  '.repeat(r.depth)}${r.id}`)

// 3 张顶层图片 + 1 个分组（含 2 段文本）。
const canvas = [
  node('a', 'core.image', '2026-11-01T10:00:00Z'),
  node('g', 'core.group', '2026-11-01T10:01:00Z'),
  node('t1', 'core.text', '2026-11-01T10:02:00Z', { parent: 'g' }),
  node('b', 'core.image', '2026-11-01T10:03:00Z'),
  node('t2', 'core.text', '2026-11-01T10:04:00.5Z', { parent: 'g' }),
  node('c', 'core.image', '2026-11-01T10:05:00Z'),
]

test('groups nest, newest first at every level, groups count too', () => {
  const o = buildOutline(canvas, none)
  assert.deepEqual(ids(o), ['c', 'b', 'g', '  t2', '  t1', 'a'])
  assert.equal(o.total, 6)
  assert.equal(o.matched, null)
  const g = o.rows.find((r) => r.id === 'g')!
  assert.equal(g.isGroup, true)
  assert.equal(g.expanded, true)
  assert.equal(g.hasChildren, true)
})

test('creation order ignores position and stacking; ties fall back to id', () => {
  const moved = canvas.map((n) =>
    n.id === 'a' ? ({ ...n, metadata: { ...n.metadata, x: -999, y: -999, z_order: 99 } } as CanvasNode) : n,
  )
  assert.deepEqual(ids(buildOutline(moved, none)), ['c', 'b', 'g', '  t2', '  t1', 'a'])
  // 秒内的小数位不等长时按时间比较，不按字符串。
  const fraction = [node('x', 'core.text', '2026-11-01T10:00:00.9Z'), node('y', 'core.text', '2026-11-01T10:00:00.25Z')]
  assert.deepEqual(ids(buildOutline(fraction, none)), ['x', 'y'])
  const tie = [node('m', 'core.text', '2026-11-01T10:00:00Z'), node('k', 'core.text', '2026-11-01T10:00:00Z')]
  assert.deepEqual(ids(buildOutline(tie, none)), ['k', 'm'])
})

test('unconfirmed nodes come first, latest local first, whatever their time says', () => {
  const local = [...canvas, node('p1', 'core.text', '2020-01-01T00:00:00Z'), node('p2', 'core.text', '2020-01-01T00:00:00Z')]
  assert.deepEqual(ids(buildOutline(local, opts({ pending: new Set(['p1', 'p2']) }))).slice(0, 3), ['p2', 'p1', 'c'])
})

test('collapsed groups hide children but still count them', () => {
  const o = buildOutline(canvas, opts({ collapsed: new Set(['g']) }))
  assert.deepEqual(ids(o), ['c', 'b', 'g', 'a'])
  assert.equal(o.rows.find((r) => r.id === 'g')!.expanded, false)
  assert.equal(o.total, 6)
})

test('search matches the shown title only, ignoring case and width', () => {
  const nodes = [
    node('h1', 'core.image', '2026-11-01T10:00:00Z', { title: '晨光人像' }),
    node('h2', 'core.image', '2026-11-01T10:01:00Z', { title: '人像-晨光' }),
    node('body', 'core.text', '2026-11-01T10:02:00Z', { title: '正文' }),
    node('dsc', 'core.image', '2026-11-01T10:03:00Z', { title: 'DSC02879' }),
    ...Array.from({ length: 9 }, (_, i) => node(`n${i}`, 'core.text', `2026-10-01T10:0${i}:00Z`)),
  ]
  const o = buildOutline(nodes, opts({ query: ' 晨光 ' }))
  assert.deepEqual(ids(o), ['h2', 'h1'])
  assert.equal(o.matched, 2)
  assert.equal(o.total, 13)
  assert.deepEqual(ids(buildOutline(nodes, opts({ query: 'ＤＳＣ' }))), ['dsc'])
  assert.deepEqual(ids(buildOutline(nodes, opts({ query: 'dsc' }))), ['dsc'])
  // 空标题按画布的兜底名搜索。
  const blank = [node('u', 'core.image', '2026-11-01T10:00:00Z', { title: '' })]
  assert.deepEqual(ids(buildOutline(blank, opts({ query: '未命名图片' }))), ['u'])
  // 只有空白的查询等于没搜。
  assert.equal(buildOutline(nodes, opts({ query: '   ' })).matched, null)
})

test('type filter keeps hit ancestors as an open path that does not count', () => {
  const nodes = [...canvas, node('i2', 'core.image', '2026-11-01T10:06:00Z', { parent: 'g' })]
  const o = buildOutline(nodes, opts({ types: new Set(['core.image']), collapsed: new Set(['g']) }))
  assert.deepEqual(ids(o), ['c', 'b', 'g', '  i2', 'a'])
  const g = o.rows.find((r) => r.id === 'g')!
  assert.equal(g.match, false)
  assert.equal(g.expanded, true)
  assert.equal(o.rows.find((r) => r.id === 'i2')!.match, true)
  assert.equal(o.matched, 4)
  // 勾上分组时分组自身命中并计数。
  assert.equal(buildOutline(nodes, opts({ types: new Set(['core.group']) })).matched, 1)
})

test('search and type filter combine; unknown types drop out once filtering', () => {
  const nodes = [...canvas, node('odd', 'vendor.thing', '2026-11-01T11:00:00Z')]
  assert.equal(buildOutline(nodes, none).rows[0]!.id, 'odd')
  assert.equal(buildOutline(nodes, opts({ types: new Set(outlineTypes) })).rows.some((r) => r.id === 'odd'), false)
  const both = buildOutline(nodes, opts({ query: 't', types: new Set(['core.text']) }))
  assert.deepEqual(ids(both), ['g', '  t2', '  t1'])
  assert.equal(both.matched, 2)
})

test('no hits and an empty canvas give empty rows without throwing', () => {
  const miss = buildOutline(canvas, opts({ query: '不存在' }))
  assert.deepEqual(miss.rows, [])
  assert.equal(miss.matched, 0)
  assert.equal(miss.total, 6)
  const empty = buildOutline([], opts({ query: 'x', types: new Set(['core.image']) }))
  assert.deepEqual(empty, { rows: [], total: 0, matched: 0 })
  assert.deepEqual(buildOutline([], none), { rows: [], total: 0, matched: null })
})

test('a child whose parent is missing shows at the top level', () => {
  const orphan = [node('lost', 'core.text', '2026-11-01T10:00:00Z', { parent: 'gone' })]
  assert.deepEqual(ids(buildOutline(orphan, none)), ['lost'])
})

const stage = { width: 1600, height: 1000 }
const insets = { left: 400, right: 40, top: 90, bottom: 90 } // 左侧面板开着
const centre = (v: { x: number; y: number; zoom: number }, r: { x: number; y: number; width: number; height: number }) => ({
  x: v.x + (r.x + r.width / 2) * v.zoom,
  y: v.y + (r.y + r.height / 2) * v.zoom,
})
// 可见区中心：(400 + (1600-440)/2, 90 + (1000-180)/2) = (980, 500)

test('reveal keeps the zoom and centres a node that fits', () => {
  const r = { x: 5000, y: -3000, width: 300, height: 328 }
  const v = revealViewport(r, stage, insets, { x: 0, y: 0, zoom: 1.5 })
  assert.equal(v.zoom, 1.5)
  assert.deepEqual(centre(v, r), { x: 980, y: 500 })
})

test('reveal zooms out only as far as needed for a node too big, never in', () => {
  const big = { x: 0, y: 0, width: 2320, height: 400 } // 可见宽 1160 → 0.5
  const v = revealViewport(big, stage, insets, { x: 0, y: 0, zoom: 1 })
  assert.equal(v.zoom, 0.5)
  assert.deepEqual(centre(v, big), { x: 980, y: 500 })
  const huge = { x: 0, y: 0, width: 1e6, height: 1e6 }
  assert.equal(revealViewport(huge, stage, insets, { x: 0, y: 0, zoom: 1 }).zoom, zoomLimits.min)
  // 已经很小的倍率不会被放大。
  assert.equal(revealViewport({ x: 0, y: 0, width: 10, height: 10 }, stage, insets, { x: 0, y: 0, zoom: 0.3 }).zoom, 0.3)
})

test('node rect is in world space and includes the label', () => {
  const nodes = [
    { ...node('g', 'core.group', '2026-11-01T10:00:00Z'), metadata: { ...node('g', 'core.group', '').metadata, x: 1000, y: 500 } },
    { ...node('c', 'core.image', '2026-11-01T10:00:00Z', { parent: 'g' }), metadata: { ...node('c', 'core.image', '').metadata, x: 20, y: 60, width: 300, height: 200 } },
  ] as CanvasNode[]
  assert.deepEqual(nodeRect(nodes, 'c'), { x: 1020, y: 532, width: 300, height: 228 })
  assert.equal(nodeRect(nodes, 'missing'), null)
})
