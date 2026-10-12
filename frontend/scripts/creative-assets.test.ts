import test from 'node:test'
import assert from 'node:assert/strict'
import {
  blank,
  clearFilters,
  enter,
  groupTree,
  listPath,
  narrowed,
  readPrefs,
  readQuery,
  tagSections,
  writePrefs,
  writeQuery,
} from '../src/creative-canvas/assets/query.ts'
import {
  audioBars,
  formatBytes,
  formatDuration,
  linkDomain,
} from '../src/creative-canvas/editor/libraryState.ts'

const store = new Map<string, string>()
let broken = false
Object.defineProperty(globalThis, 'localStorage', {
  configurable: true,
  value: {
    getItem: (k: string) => {
      if (broken) throw new Error('blocked')
      return store.get(k) ?? null
    },
    setItem: (k: string, v: string) => {
      if (broken) throw new Error('blocked')
      store.set(k, v)
    },
  },
})

const params = (s: string) => new URLSearchParams(s)

test('unknown values fall back to defaults and a group forces the all view', () => {
  assert.deepEqual(readQuery(params('')), blank())
  const q = readQuery(
    params(
      'view=trash&group=g1&kind=pdf&rating=2&sort=size&mode=any&tags=a,b,a,',
    ),
  )
  assert.equal(q.view, 'all')
  assert.equal(q.group, 'g1')
  assert.equal(q.kind, '')
  assert.equal(q.rating, '')
  assert.equal(q.sort, 'recent')
  assert.deepEqual(q.tags, ['a', 'b'])
  assert.equal(q.mode, 'any')
  assert.equal(readQuery(params(`q=${'字'.repeat(250)}`)).q.length, 200)
})

test('the address only carries non-default conditions and round-trips', () => {
  assert.equal(writeQuery(blank()).toString(), '')
  const q = {
    ...blank(),
    view: 'favorites' as const,
    tags: ['t1', 't2'],
    mode: 'any' as const,
    kind: 'image' as const,
    q: '窗边',
    rating: '4.5' as const,
    sort: 'rating' as const,
  }
  assert.deepEqual(readQuery(writeQuery(q)), q)
  // 只有一个标签时没有「满足任一」之分，不写 mode。
  assert.equal(
    writeQuery({ ...blank(), tags: ['t1'], mode: 'any' }).has('mode'),
    false,
  )
  // 进了分组就不再写视图。
  assert.equal(writeQuery({ ...q, group: 'g1' }).has('view'), false)
})

test('changing view or group clears filters but keeps the sort', () => {
  const q = {
    ...blank(),
    tags: ['t'],
    kind: 'video' as const,
    q: 'x',
    rating: 'none' as const,
    sort: 'name' as const,
  }
  assert.deepEqual(enter(q, 'trash'), {
    ...blank(),
    view: 'trash',
    sort: 'name',
  })
  assert.deepEqual(enter(q, 'trash', 'g1'), {
    ...blank(),
    group: 'g1',
    sort: 'name',
  })
  assert.equal(narrowed(q), true)
  assert.equal(narrowed(clearFilters(q)), false)
  assert.equal(clearFilters(q).sort, 'name')
})

test('the first page asks for facets, later pages only carry the cursor', () => {
  const q = {
    ...blank(),
    group: 'g1',
    tags: ['a', 'b'],
    mode: 'any' as const,
    kind: 'audio' as const,
    q: '海',
    rating: '4' as const,
  }
  const first = new URL(listPath(q, 60), 'http://x')
  assert.equal(first.pathname, '/assets')
  assert.deepEqual(Object.fromEntries(first.searchParams), {
    view: 'all',
    limit: '60',
    sort: 'recent',
    group_id: 'g1',
    include_descendants: 'true',
    kind: 'audio',
    q: '海',
    tag_ids: 'a,b',
    tag_mode: 'any',
    min_rating: '4',
    include_facets: 'true',
  })
  const next = new URL(listPath(q, 60, 'c2'), 'http://x').searchParams
  assert.equal(next.get('cursor'), 'c2')
  assert.equal(next.has('include_facets'), false)
  const unrated = new URL(
    listPath({ ...blank(), rating: 'none' }, 60),
    'http://x',
  ).searchParams
  assert.equal(unrated.get('unrated'), 'true')
  assert.equal(unrated.has('min_rating'), false)
  assert.equal(unrated.has('tag_mode'), false)
})

test('display preferences survive bad data and blocked storage', () => {
  store.clear()
  assert.deepEqual(readPrefs(), {
    layout: 'masonry',
    size: 220,
    inspector: true,
  })
  writePrefs({ layout: 'grid', size: 300, inspector: false })
  assert.deepEqual(readPrefs(), { layout: 'grid', size: 300, inspector: false })
  store.set(
    'creative-assets:prefs',
    JSON.stringify({ layout: 'x', size: 9999, inspector: 'no' }),
  )
  assert.deepEqual(readPrefs(), {
    layout: 'masonry',
    size: 360,
    inspector: true,
  })
  store.set('creative-assets:prefs', '{not json')
  assert.equal(readPrefs().size, 220)
  broken = true
  assert.doesNotThrow(() =>
    writePrefs({ layout: 'grid', size: 140, inspector: true }),
  )
  assert.equal(readPrefs().layout, 'masonry')
  broken = false
})

test('groups nest two levels and tags list by category with uncategorized last', () => {
  const g = (id: string, parent: string | null) => ({
    id,
    parent_id: parent,
    name: id,
    position: 0,
    revision: '1',
  })
  assert.deepEqual(
    groupTree([g('a', null), g('a1', 'a'), g('b', null), g('a2', 'a')]).map(
      (x) => [x.group.id, x.children.map((c) => c.id)],
    ),
    [
      ['a', ['a1', 'a2']],
      ['b', []],
    ],
  )
  const t = (id: string, category: string | null) => ({
    id,
    name: id,
    color: '#fff',
    category_id: category,
    revision: '1',
  })
  const sections = tagSections(
    [t('warm', 'style'), t('loose', null), t('soft', 'light')],
    [
      { id: 'style', name: '风格', position: 0, revision: '1' },
      { id: 'empty', name: '空分类', position: 1, revision: '1' },
      { id: 'light', name: '光线', position: 2, revision: '1' },
    ] as never,
  )
  assert.deepEqual(
    sections.map((s) => [s.name, s.tags.map((x) => x.id)]),
    [
      ['风格', ['warm']],
      ['光线', ['soft']],
      ['未分类', ['loose']],
    ],
  )
})

test('card helpers format durations, sizes and domains', () => {
  assert.equal(formatDuration(null), '')
  assert.equal(formatDuration(0), '')
  assert.equal(formatDuration(5_400), '0:05')
  assert.equal(formatDuration(184_000), '3:04')
  assert.equal(formatDuration(3_725_000), '1:02:05')
  assert.equal(formatBytes(0), '0 B')
  assert.equal(formatBytes(1536), '1.5 KB')
  assert.equal(formatBytes(20 * 1024 ** 3), '20 GB')
  assert.equal(formatBytes(3.24 * 1024 ** 3), '3.2 GB')
  assert.equal(linkDomain('https://www.example.com/a?b=1'), 'example.com')
  assert.equal(linkDomain('not a url'), 'not a url')
})

test('audio bars are decided by the asset id', () => {
  const a = audioBars('asset-1')
  assert.equal(a.length, 36)
  assert.deepEqual(audioBars('asset-1'), a)
  assert.notDeepEqual(audioBars('asset-2'), a)
  assert(a.every((h) => h >= 0.2 && h <= 1))
})
