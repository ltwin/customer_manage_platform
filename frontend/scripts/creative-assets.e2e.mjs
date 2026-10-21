// Browser regression for the standalone asset library page (V6-067.1 with
// V6-016 / V6-060). The API is an in-memory mock that filters, sorts, pages
// and counts like the server; each check runs in a fresh context.
// Run against the Vite dev server: node scripts/creative-assets.e2e.mjs.
import assert from 'node:assert/strict'
import { createHash } from 'node:crypto'
import { mkdirSync, readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { chromium } from 'playwright'

const base = process.env.CREATIVE_EDITOR_URL ?? 'http://localhost:5173'
const output = process.env.CREATIVE_QA_OUTPUT_DIR ?? '/tmp/creative-assets-qa'
mkdirSync(output, { recursive: true })
const webm = readFileSync(fileURLToPath(new URL('../../backend/internal/creativemedia/testdata/sample.webm', import.meta.url)))
const mp3 = readFileSync(fileURLToPath(new URL('../../backend/internal/creativemedia/testdata/sample.mp3', import.meta.url)))
const glb = readFileSync(fileURLToPath(new URL('../../backend/internal/creativeglb/testdata/exported-template.glb', import.meta.url)))
// 模型预览用 WebGL：和场景编辑器的 e2e 一样走 SwiftShader。
const browser = await chromium.launch({ headless: true, args: ['--use-angle=swiftshader', '--enable-unsafe-swiftshader'] })
const errors = []
const results = {}
let context, page, server

const DAY = 86400000
const now = Date.now()
const GiB = 1024 ** 3
const groups = [
  { id: 'g-wed', parent_id: null, name: '婚礼', position: 0, revision: '1' },
  { id: 'g-out', parent_id: 'g-wed', name: '外景', position: 0, revision: '1' },
  { id: 'g-in', parent_id: 'g-wed', name: '棚拍', position: 1, revision: '1' },
  { id: 'g-brand', parent_id: null, name: '品牌', position: 1, revision: '1' },
]
const categories = [
  { id: 'c-style', name: '风格', position: 0, revision: '1' },
  { id: 'c-light', name: '光线', position: 1, revision: '1' },
]
const tags = [
  { id: 't-warm', name: '暖调', color: '#d9a07a', category_id: 'c-style', revision: '1' },
  { id: 't-film', name: '胶片', color: '#8fa37a', category_id: 'c-style', revision: '1' },
  { id: 't-back', name: '逆光', color: '#d9c38a', category_id: 'c-light', revision: '1' },
  { id: 't-misc', name: '待整理', color: '#a1a1a7', category_id: null, revision: '1' },
]
const mime = { image: 'image/jpeg', video: 'video/webm', audio: 'audio/mpeg' }
function asset(o) {
  const kind = o.kind ?? 'image'
  const original = { role: 'original', blob_id: `b-${o.id}`, mime: mime[kind], byte_size: 2_400_000, width: kind === 'audio' ? null : (o.w ?? 1200), height: kind === 'audio' ? null : (o.h ?? 800), duration_ms: o.duration ?? null }
  const media = mime[kind] ? [original, ...(kind === 'image' ? [{ ...original, role: 'display' }] : [])] : undefined
  const payload = kind === 'text' ? { body: o.body } : kind === 'link' ? { url: o.url } : { caption: '' }
  return {
    id: o.id,
    title: o.title,
    description: o.description ?? '',
    rating: o.rating ?? null,
    palette: kind === 'image' ? (o.palette ?? [{ hex: '#d9a07a', weight: 1 }]) : [],
    kind,
    revision: '1',
    content_id: `c-${o.id}`,
    content_revision_id: `r-${o.id}`,
    content: { id: `r-${o.id}`, content_id: `c-${o.id}`, kind, sequence: '1', payload, truncated: false, ...(media ? { media } : {}) },
    unavailable: false,
    is_favorite: !!o.fav,
    created_at: new Date(now - (o.age ?? 30) * DAY).toISOString(),
    deleted_at: o.trashed ? new Date(now - DAY).toISOString() : null,
    purge_after: o.trashed ? new Date(now + 29 * DAY).toISOString() : null,
    group_ids: o.groups ?? [],
    tag_ids: o.tags ?? [],
  }
}
// 7 份有名字的素材覆盖五种类型，另有 63 张填充图，合计 70 项（超过一页 60 项）。
const fixture = () => [
  asset({ id: 'f-sea', title: '海边逆光', w: 1200, h: 1800, rating: 5, groups: ['g-out'], tags: ['t-back', 't-warm'], age: 1, fav: true, description: '日落前二十分钟' }),
  asset({ id: 'f-veil', title: '白纱与拱门', w: 1600, h: 1067, rating: 4.5, groups: ['g-out', 'g-brand'], tags: ['t-warm', 't-film'], age: 2 }),
  asset({ id: 'f-studio', title: '棚拍布光', w: 1000, h: 1000, rating: 3.5, groups: ['g-in'], tags: ['t-film'], age: 3 }),
  asset({ id: 'f-clip', title: '海风走位', kind: 'video', w: 1920, h: 1080, duration: 12_000, rating: 4, groups: ['g-out'], tags: ['t-back'], age: 4 }),
  asset({ id: 'f-voice', title: '现场环境音', kind: 'audio', duration: 184_000, groups: ['g-in'], tags: ['t-misc'], age: 5 }),
  asset({ id: 'f-note', title: '拍摄脚本 v2', kind: 'text', body: '07:30 集合\n08:10 海边逆光\n09:00 白纱与拱门', rating: 4, tags: ['t-misc'], age: 6 }),
  asset({ id: 'f-ref', title: '布光参考', kind: 'link', url: 'https://www.example.com/lighting/101', groups: ['g-brand'], age: 10 }),
  asset({ id: 'f-old', title: '旧照片', trashed: true, age: 40 }),
  ...Array.from({ length: 63 }, (_, i) =>
    asset({ id: `f-${String(i).padStart(2, '0')}`, title: `素材 ${String(i).padStart(2, '0')}`, w: 1200, h: i % 3 ? 800 : 1500, rating: i % 4 ? null : 3, age: 20 + i }),
  ),
]

const key = (s) => s.normalize('NFKC').toLowerCase().trim()
function mockServer(assets) {
  const s = { settings:{revision:'1',hierarchy_revision:'1',library_revision:'1',retention_days:30}, uploads:new Map(), assets, reads: [], other: [], writes: [], receipts: new Map(), groups: structuredClone(groups), tags: structuredClone(tags), categories: structuredClone(categories), hierarchy: '1', deletions: new Map(), projects: [], canvases: new Map(), digests: new Map(), failNext: null, loseNext: null, revision: '1', hold: null, pendingRelease: 0 }
  const live = () => s.assets.filter((a) => !a.deleted_at)
  const descendants = (id) => [id, ...s.groups.filter((g) => g.parent_id === id).map((g) => g.id)]
  function scope(q, { kind = true, tagFilter = true } = {}) {
    const view = q.get('view') ?? 'all'
    let items = view === 'trash' ? s.assets.filter((a) => a.deleted_at) : live()
    if (view === 'favorites') items = items.filter((a) => a.is_favorite)
    if (view === 'unclassified') items = items.filter((a) => !a.group_ids.length)
    if (view === 'recent') items = items.filter((a) => now - Date.parse(a.created_at) <= 7 * DAY)
    const g = q.get('group_id')
    if (g) {
      const ids = q.get('include_descendants') === 'true' ? descendants(g) : [g]
      items = items.filter((a) => a.group_ids.some((x) => ids.includes(x)))
    }
    if (kind && q.get('kind')) items = items.filter((a) => a.kind === q.get('kind'))
    const text = key(q.get('q') ?? '')
    if (text)
      items = items.filter((a) =>
        key([a.title, a.description, a.content.payload.body ?? '', a.content.payload.url ?? '', ...a.tag_ids.map((t) => s.tags.find((x) => x.id === t).name)].join(' ')).includes(text),
      )
    if (q.get('shape')) items = items.filter(a => {
      const m = a.content?.media?.find(m => m.role === 'original')
      if (a.unavailable || a.kind !== 'image' || !m?.width || !m.height) return false
      const shape = Math.max(m.width,m.height)*10 <= Math.min(m.width,m.height)*11 ? 'square' : m.width > m.height ? 'landscape' : 'portrait'
      return shape === q.get('shape')
    })
    // UI mock tests parameter propagation; matching math has real Go/PG tests.
    if (q.get('color')) items = items.filter(a => !a.unavailable && a.palette.some(c => c.hex === q.get('color')))
    if (q.get('unrated') === 'true') items = items.filter((a) => a.rating === null)
    if (q.get('min_rating')) items = items.filter((a) => a.rating !== null && a.rating >= Number(q.get('min_rating')))
    const selected = (q.get('tag_ids') ?? '').split(',').filter(Boolean)
    if (tagFilter && selected.length)
      items = items.filter((a) => (q.get('tag_mode') === 'any' ? selected.some((t) => a.tag_ids.includes(t)) : selected.every((t) => a.tag_ids.includes(t))))
    return items
  }
  const sorters = {
    recent: (a, b) => b.created_at.localeCompare(a.created_at),
    oldest: (a, b) => a.created_at.localeCompare(b.created_at),
    name: (a, b) => (key(a.title) < key(b.title) ? -1 : key(a.title) > key(b.title) ? 1 : 0),
    rating: (a, b) => (b.rating ?? -1) - (a.rating ?? -1) || b.created_at.localeCompare(a.created_at),
  }
  s.handle = async (route) => {
    const request = route.request()
    const url = new URL(request.url())
    const path = url.pathname
    // 页面已经放弃的请求（条件变了被中止）再回应会报错，忽略即可。
    const json = (value, status = 200) => route.fulfill({ status, json: value }).catch(() => {})
    if (path.endsWith('/auth/refresh')) return json({ access_token: 'qa', token_type: 'Bearer', expires_in: 600 })
    if (path.endsWith('/me')) return json({ id: 'qa-assets', email: 'qa@example.test', created_at: '2026-01-01T00:00:00Z', timezone: 'Asia/Shanghai' })
    if (path.endsWith('/account/profile')) return json({ display_name: 'Samson', profile_revision: '1', avatar_revision: '0' })
    if (path.endsWith('/creative/projects') && request.method() === 'GET') return json({ items: s.projects.filter((p) => !p.archived && p.name.includes(url.searchParams.get('q') ?? '')), next_cursor: '', activity_counts: { running: 0, waiting: 0 } })
    if (path.endsWith('/creative/asset-groups') && request.method() === 'GET') {
      const counts = Object.fromEntries(s.groups.map((g) => [g.id, live().filter((a) => a.group_ids.some((x) => descendants(g.id).includes(x))).length]))
      return json({ items: s.groups, hierarchy_revision: s.hierarchy, asset_counts: counts })
    }
    if (path.endsWith('/creative/tags') && request.method() === 'GET') return json({ items: s.tags.map((t) => ({ ...t, asset_count: s.assets.filter((a) => a.tag_ids.includes(t.id)).length })), hierarchy_revision: '1' })
    if (path.endsWith('/creative/tag-categories') && request.method() === 'GET') return json({ items: s.categories, hierarchy_revision: '1' })
    if(path.endsWith('/creative/media-capabilities')) return json({formats:[{kind:'image',mime:'image/png',extensions:['.png']}],image_max_bytes:1024,av_max_bytes:1024,batch_limit:20,part_size:1024})
    if(path.endsWith('/creative/library-settings') && request.method()==='GET')return json(s.settings)
    if(/\/creative\/uploads\/[^/]+$/.test(path)&&request.method()==='GET')return json(s.uploads.get(path.split('/').at(-1)))
    if(path.endsWith('/part-authorizations'))return json({parts:[{part_number:1,url:base+'/api/v1/__qa_part',method:'PUT',headers:{}}]})
    if(path.endsWith('/__qa_part'))return route.fulfill({status:200,body:''})
    if (path.endsWith('/creative/library-usage')) return json({ asset_count: live().length, stored_bytes: Math.round(3.24 * GiB), limit_bytes: 20 * GiB, pending_release_bytes: s.pendingRelease })
    if (path.endsWith('/creative/media-access-tickets')) {
      const id = request.postDataJSON().content_revision_id.slice(2)
      const a = s.assets.find((x) => x.id === id)
      if (a?.kind === 'model' && request.postDataJSON().role === 'original') return json({ url: `${base}/__qa_media/${id}.glb`, expires_at: new Date(Date.now() + 3600000).toISOString() })
      if (a?.kind === 'model') {
        const thumb = '<svg xmlns="http://www.w3.org/2000/svg" width="256" height="256"><rect x="104" y="20" width="48" height="216" fill="#d9d5cd"/></svg>'
        return json({ url: `data:image/svg+xml,${encodeURIComponent(thumb)}`, expires_at: new Date(Date.now() + 3600000).toISOString() })
      }
      if (a?.kind === 'audio') return json({ url: `${base}/__qa_media/${id}.mp3`, expires_at: new Date(Date.now() + 3600000).toISOString() })
      if (a?.kind === 'video') return json({ url: `${base}/__qa_media/${id}.webm`, expires_at: new Date(Date.now() + 3600000).toISOString() })
      const hue = [...id].reduce((h, c) => h + c.charCodeAt(0), 0) % 360
      const m = a.content.media[0]
      const art = `<svg xmlns="http://www.w3.org/2000/svg" width="${m.width}" height="${m.height}"><rect width="100%" height="100%" fill="hsl(${hue} 22% 46%)"/><circle cx="70%" cy="30%" r="14%" fill="hsl(${(hue + 40) % 360} 45% 66%)"/></svg>`
      return json({ url: `data:image/svg+xml,${encodeURIComponent(art)}`, expires_at: new Date(Date.now() + 3600000).toISOString() })
    }
    if (path.endsWith('/creative/assets') && request.method() === 'GET') {
      const q = url.searchParams
      s.reads.push(q.toString())
      if (s.hold && s.hold.match(q)) {
        if (s.hold.wait) await s.hold.wait(q)
        else await new Promise((r) => setTimeout(r, s.hold.ms))
      }
      const items = scope(q).sort(sorters[q.get('sort') ?? 'recent'])
      const limit = Number(q.get('limit') ?? 30)
      const offset = Number(q.get('cursor') || 0)
      let facets
      if (q.get('include_facets') === 'true') {
        if (q.get('cursor')) return json({ error: { code: 'validation_failed', message: 'include_facets 只能用于首屏' } }, 400)
        const byKind = scope(q, { kind: false })
        const selected = (q.get('tag_ids') ?? '').split(',').filter(Boolean)
        const loose = scope(q, { tagFilter: false })
        facets = {
          kinds: Object.fromEntries(['text', 'link', 'image', 'video', 'audio', 'model'].map((k) => [k, byKind.filter((a) => a.kind === k).length])),
          tags: s.tags.map((t) => ({
            tag_id: t.id,
            count: loose.filter((a) => (q.get('tag_mode') === 'any' ? a.tag_ids.includes(t.id) : [...selected, t.id].every((x) => a.tag_ids.includes(x)))).length,
          })),
        }
      }
      return json({ items: items.slice(offset, offset + limit), next_cursor: offset + limit < items.length ? String(offset + limit) : '', total_count: items.length, library_revision: s.revision, ...(facets ? { facets } : {}) })
    }
    if (path.endsWith('/creative/assets/digest-matches')) {
      const items = (url.searchParams.get('digests') ?? '').split(',').flatMap((digest) => {
        const a = s.assets.find((a) => a.id === s.digests.get(digest))
        return a ? [{ digest, asset: a }] : []
      })
      return json({ items })
    }
    if (/\/creative\/assets\/[^/]+$/.test(path) && request.method() === 'GET') {
      const a = s.assets.find((a) => a.id === path.split('/').at(-1))
      return a ? json(a) : json({ error: { code: 'not_found', message: '素材不存在' } }, 404)
    }
    if (/\/creative\/canvases\/[^/]+$/.test(path) && request.method() === 'GET') return json(s.canvases.get(path.split('/').at(-1)))
    if (request.method() === 'POST' && path.includes('/creative/')) {
      const body = request.postDataJSON(), payload = body.payload, op = body.operation_id
      s.writes.push({ path, body, key: request.headers()['idempotency-key'] })
      if (s.receipts.has(op)) return json(s.receipts.get(op))
      if (s.failNext && path.endsWith(s.failNext.path)) {
        const failure = s.failNext; s.failNext = null
        return json({ error: { code: failure.code ?? 'creative_revision_conflict', message: failure.message ?? '测试请求失败' } }, failure.status ?? 409)
      }
      let result = { id: 'ok', revision: '2', hierarchy_revision: s.hierarchy, library_revision: s.revision }
      const versions = payload.items ?? payload.assets ?? (payload.expected_revision ? [{ asset_id: path.split('/').at(-2), expected_revision: payload.expected_revision }] : [])
      if (path.includes('/assets/') && versions.some((v) => s.assets.find((a) => a.id === v.asset_id)?.revision !== v.expected_revision)) return json({ error: { code: 'creative_revision_conflict', message: '版本冲突' } }, 409)
      if(path.endsWith('/creative/assets')) {
        const a=asset({id:'new-'+op,title:payload.title,kind:payload.content.kind,body:payload.content.payload.body,url:payload.content.payload.url,groups:payload.group_ids,age:0});s.assets.unshift(a);result={asset_id:a.id,revision:a.revision,content_revision_id:a.content_revision_id,library_revision:s.revision,tag_mapping:{}}
      } else if(path.endsWith('/creative/uploads')) {
        const id='upload-'+op;result={id,state:'uploading',revision:'1',part_count:1,part_size:1024,target:payload.target,name:payload.file_name};s.uploads.set(id,result)
      } else if(path.endsWith('/complete')) {
        const u=s.uploads.get(path.split('/').at(-2));const a=asset({id:'file-'+op,title:u.target.asset.title,groups:u.target.asset.group_ids,age:0});s.assets.unshift(a);u.state='ready';u.publication={kind:'asset',id:a.id,revision:'1'};result=u
      } else if(path.endsWith('/creative/library-settings')) {
        s.settings={...s.settings,retention_days:payload.retention_days,revision:String(Number(s.settings.revision)+1)}
      } else if(path.endsWith('/library/empty-trash')) {
        if(payload.expected_library_revision!==s.revision)return json({error:{code:'creative_revision_conflict',message:'版本冲突'}},409)
        const purged_count=s.assets.filter(a=>a.deleted_at).length;s.assets=s.assets.filter(a=>!a.deleted_at);result={purged_count,library_revision:String(Number(s.revision)+1)}
      } else if(path.endsWith('/assets/batch-restore')) {
        for(const v of payload.assets){const a=s.assets.find(a=>a.id===v.asset_id);a.deleted_at=null;a.purge_after=null;a.revision=String(Number(a.revision)+1)}
      } else if(path.endsWith('/assets/batch-purge')) {
        s.assets=s.assets.filter(a=>!payload.assets.some(v=>v.asset_id===a.id))
      } else if (path.endsWith('/assets/batch-rate')) {
        for (const item of payload.items) { const a = s.assets.find((a) => a.id === item.asset_id); a.rating = item.rating; a.revision = String(Number(a.revision) + 1) }
        result = { items: payload.items.map((item) => { const a = s.assets.find((a) => a.id === item.asset_id); return { id: a.id, revision: a.revision } }), library_revision: String(Number(s.revision) + 1) }
      } else if (path.endsWith('/metadata')) {
        const a = s.assets.find((a) => a.id === path.split('/').at(-2)); const { expected_revision: _rev, ...patch } = payload
        Object.assign(a, patch); a.revision = String(Number(a.revision) + 1); result.id = a.id; result.revision = a.revision
      } else if (path.endsWith('/assets/batch-organize')) {
        for (const v of payload.assets) { const a = s.assets.find((a) => a.id === v.asset_id); for (const field of ['group_ids', 'tag_ids']) a[field] = [...new Set([...a[field].filter((id) => !(payload['remove_' + field] ?? []).includes(id)), ...(payload['add_' + field] ?? [])])]; a.revision = String(Number(a.revision) + 1) }
      } else if (path.endsWith('/assets/batch-trash')) {
        for (const v of payload.assets) { const a = s.assets.find((a) => a.id === v.asset_id); a.deleted_at = new Date().toISOString(); a.revision = String(Number(a.revision) + 1) }
      } else if (/\/assets\/[^/]+\/import-replace$/.test(path)) {
        const a = s.assets.find((a) => a.id === path.split('/').at(-2)); a.deleted_at = null; a.purge_after = null; a.title = payload.title
        a.group_ids = [...new Set([...a.group_ids, ...payload.group_ids])]; a.revision = String(Number(a.revision) + 1); result.id = a.id; result.revision = a.revision
      } else if (/\/assets\/[^/]+\/restore$/.test(path)) {
        const a = s.assets.find((a) => a.id === path.split('/').at(-2)); a.deleted_at = null; a.purge_after = null; a.revision = String(Number(a.revision) + 1)
      } else if (path.endsWith('/creative/asset-groups')) {
        const g = { id: 'g-new-' + s.groups.length, parent_id: payload.parent_id ?? null, name: payload.name, position: s.groups.length, revision: '1' }; s.groups.push(g); s.hierarchy = String(Number(s.hierarchy) + 1); result.id = g.id
      } else if (/\/asset-groups\/[^/]+\/rename$/.test(path)) {
        const g = s.groups.find((g) => g.id === path.split('/').at(-2)); g.name = payload.name; g.revision = String(Number(g.revision) + 1); s.hierarchy = String(Number(s.hierarchy) + 1)
      } else if (path.endsWith('/creative/tags')) {
        const t = { id: 't-new-' + s.tags.length, ...payload, revision: '1' }; s.tags.push(t); result.id = t.id
      } else if (/\/tags\/[^/]+\/edit$/.test(path)) {
        const t = s.tags.find((t) => t.id === path.split('/').at(-2)); const { expected_revision: _r, hierarchy_revision: _h, ...patch } = payload
        Object.assign(t, patch); t.revision = String(Number(t.revision) + 1)
      } else if (/\/tags\/[^/]+\/delete$/.test(path)) {
        const id = path.split('/').at(-2); s.tags = s.tags.filter((t) => t.id !== id)
        s.assets.filter((a) => a.tag_ids.includes(id)).forEach((a) => { a.tag_ids = a.tag_ids.filter((v) => v !== id); a.revision = String(Number(a.revision) + 1) })
      } else if (path.endsWith('/creative/tag-categories')) {
        const c = { id: 'c-new-' + s.categories.length, name: payload.name, position: s.categories.length, revision: '1' }; s.categories.push(c); result.id = c.id
      } else if (/\/tag-categories\/[^/]+\/edit$/.test(path)) {
        const c = s.categories.find((c) => c.id === path.split('/').at(-2)); c.name = payload.name; c.revision = String(Number(c.revision) + 1)
      } else if (/\/tag-categories\/[^/]+\/delete$/.test(path)) {
        const id = path.split('/').at(-2); s.categories = s.categories.filter((c) => c.id !== id)
        s.tags.filter((t) => t.category_id === id).forEach((t) => { t.category_id = null; t.revision = String(Number(t.revision) + 1) })
      } else if (/\/asset-groups\/[^/]+\/delete$/.test(path)) {
        const id = path.split('/').at(-2), g = s.groups.find((g) => g.id === id)
        if (payload.hierarchy_revision !== s.hierarchy || !g) return json({ error: { code: 'creative_revision_conflict', message: '分组已经有新的调整，无法撤销' } }, 409)
        const deletion = 'delete-' + id
        s.deletions.set(deletion, { group: structuredClone(g), members: s.assets.filter((a) => a.group_ids.includes(id)).map((a) => a.id), children: s.groups.filter((g) => g.parent_id === id).map((g) => g.id) })
        s.groups = s.groups.filter((g) => g.id !== id); s.groups.filter((g) => g.parent_id === id).forEach((g) => { g.parent_id = null })
        s.assets.forEach((a) => { a.group_ids = a.group_ids.filter((v) => v !== id) }); s.hierarchy = String(Number(s.hierarchy) + 1)
        result = { ...result, deletion_id: deletion, hierarchy_revision: s.hierarchy }
      } else if (/\/asset-groups\/deletions\/[^/]+\/restore$/.test(path)) {
        const snap = s.deletions.get(path.split('/').at(-2)); s.groups.push(snap.group)
        s.groups.filter((g) => snap.children.includes(g.id)).forEach((g) => { g.parent_id = snap.group.id })
        s.assets.filter((a) => snap.members.includes(a.id)).forEach((a) => { a.group_ids = [...new Set([...a.group_ids, snap.group.id])] }); s.hierarchy = String(Number(s.hierarchy) + 1)
      } else if (path.endsWith('/creative/projects')) {
        const id = 'project-' + (s.projects.length + 1), canvas = 'canvas-' + id
        s.projects.push({ id, name: payload.name, default_canvas_id: canvas, revision: '1', archived: false, edited_at: new Date().toISOString(), running: false, waiting: false, cover: { nodes: [], edges: [] } })
        s.canvases.set(canvas, { id: canvas, nodes: [], topology_revision: '1' }); result = { project_id: id, default_canvas_id: canvas, revision: '1' }
      } else if (path.endsWith('/commands')) {
        const c = s.canvases.get(path.split('/').at(-2))
        if (c.topology_revision !== payload.expected_topology_revision) return json({ error: { code: 'creative_revision_conflict', message: '结构版本冲突' } }, 409)
        c.nodes.push(...payload.actions.map((a) => ({ id: a.node_id, parent_id: null, metadata: { x: a.x, y: a.y, width: 300, height: 300 } }))); c.topology_revision = String(Number(c.topology_revision) + 1)
        result = { change_id: 'change-' + op, result_revision: c.topology_revision, result_topology_revision: c.topology_revision, object_results: [], created_ids: payload.actions.map((a) => a.node_id) }
      } else return json({ error: { code: 'unavailable', message: 'not modelled' } }, 503)
      s.revision = String(Number(s.revision) + 1); s.receipts.set(op, structuredClone(result))
      if (s.loseNext && path.endsWith(s.loseNext)) { s.loseNext = null; return route.abort('failed') }
      return json(result)
    }
    s.other.push(`${request.method()} ${path}`)
    return json({ error: { code: 'unavailable', message: 'not modelled' } }, 503)
  }
  return s
}

// 瀑布流按列排布，DOM 顺序不是阅读顺序；要比对结果顺序的检查用方格打开。
async function fresh({ width = 1440, height = 900, assets = fixture(), layout = 'masonry' } = {}) {
  await context?.close()
  context = await browser.newContext({ viewport: { width, height } })
  if (layout === 'grid')
    await context.addInitScript(() => {
      if (!localStorage.getItem('creative-assets:prefs'))
        localStorage.setItem('creative-assets:prefs', JSON.stringify({ layout: 'grid', size: 220, inspector: true }))
    })
  server = mockServer(assets)
  page = await context.newPage()
  page.setDefaultTimeout(6000)
  page.on('pageerror', (error) => errors.push(error.message))
  await page.route('**/api/v1/**', server.handle)
  await page.route('**/__qa_media/**', (route) => {
    const url = route.request().url()
    if (url.endsWith('.glb')) return route.fulfill({ status: 200, contentType: 'model/gltf-binary', body: glb })
    return route.fulfill({ status: 200, contentType: url.endsWith('.mp3') ? 'audio/mpeg' : 'video/webm', body: url.endsWith('.mp3') ? mp3 : webm })
  })
}
const open = async (query = '') => {
  await page.goto(`${base}/creative/library${query}`)
  await page.locator('.al-tree').waitFor({ state: 'attached' })
  await settled()
}
const until = async (fn, label, ms = 5000) => {
  const start = Date.now()
  while (!(await fn())) {
    if (Date.now() - start > ms) throw new Error(`timeout: ${label}`)
    await new Promise((r) => setTimeout(r, 30))
  }
}
// 做一个会换条件的动作：等新的列表请求真正发出，再等结果画完。
async function after(run) {
  const n = server.reads.length
  await run()
  await until(() => server.reads.length > n, 'list request')
  await settled()
}
const tiles = () => page.locator('.al-tile')
const tile = (title) => page.locator('.al-tile', { has: page.getByText(title, { exact: true }) }).first()
const titles = async () => page.locator('.al-tile').evaluateAll((els) => els.map((e) => e.getAttribute('aria-label')))
const params = () => new URL(page.url()).searchParams
const lastRead = () => new URLSearchParams(server.reads.at(-1))
const settled = () => page.waitForFunction(() => !document.querySelector('.al-tiles[aria-busy="true"]'))
const tree = () => page.getByRole('complementary', { name: '资料库' })
// 树里的一行（视图、分组或标签），按名称精确找，不会撞上「子分组：…」按钮。
const row = (name) => tree().locator('.al-row', { has: page.locator('.al-name', { hasText: new RegExp(`^${name}$`) }) })
const n = async (name) => (await row(name).locator('.al-n').innerText()).trim()
const board = () => page.getByRole('region', { name: '资产' })
const noHorizontalScroll = async (label) => {
  const r = await page.evaluate(() => ({ doc: document.documentElement.scrollWidth, win: innerWidth, board: [...document.querySelectorAll('.al-board, .al-scroll')].map((e) => e.scrollWidth - e.clientWidth) }))
  assert(r.doc <= r.win, `${label}: page scrolls sideways ${r.doc} > ${r.win}`)
  assert(r.board.every((d) => d <= 0), `${label}: board scrolls sideways ${r.board}`)
}
async function check(name, run) {
  if (process.env.CREATIVE_QA_CHECK && !name.includes(process.env.CREATIVE_QA_CHECK)) return
  try {
    await run()
    results[name] = 'pass'
  } catch (error) {
    results[name] = `fail: ${error.message}`
    await page?.screenshot({ path: `${output}/${name}-failure.png`, fullPage: true }).catch(() => {})
  }
}

// AC-01：独立页面，不加载画布；三档宽度的布局，都没有横向滚动。
await check('AC-01', async () => {
  await fresh()
  await open()
  assert.equal(await page.locator('.react-flow').count(), 0)
  assert(!server.other.some((r) => r.includes('/canvases')), server.other.join('|'))
  assert.equal(await page.getByRole('complementary', { name: '创意空间导航' }).count(), 1)
  assert.equal(await page.getByRole('link', { name: '资产库' }).getAttribute('aria-current'), 'page')
  for (const name of ['资料库', '检查器']) assert(await page.getByRole('complementary', { name }).isVisible(), name)
  assert(await board().isVisible())
  await noHorizontalScroll('1440')
  // 工具条放得下：标题和排序不被挤得截断，缩放滑杆可用。
  for (const el of [page.locator('.al-crumb h2'), page.getByRole('combobox', { name: '排序' }).locator('.cc-picker-value')])
    assert(await el.evaluate((e) => e.scrollWidth <= e.clientWidth), 'toolbar text is truncated')
  assert(await page.getByRole('slider', { name: '缩略图大小' }).isVisible())
  await page.screenshot({ path: `${output}/ac01-desktop.png` })
  // 检查器可以收起，记在本机，刷新后保持。
  await page.getByRole('button', { name: '检查器' }).click()
  assert.equal(await page.getByRole('complementary', { name: '检查器' }).count(), 0)
  await after(() => page.reload())
  assert.equal(await page.getByRole('complementary', { name: '检查器' }).count(), 0)
  assert.equal(await page.getByRole('button', { name: '检查器' }).getAttribute('aria-pressed'), 'false')
  await page.getByRole('button', { name: '检查器' }).click()

  await page.setViewportSize({ width: 1000, height: 800 })
  // 等侧栏折叠的过渡动画结束再截图。
  await page.waitForTimeout(400)
  assert(await tree().isVisible())
  assert.equal(await page.getByRole('complementary', { name: '检查器' }).isVisible(), false)
  assert.equal(await page.getByRole('button', { name: '检查器' }).isVisible(), false)
  await noHorizontalScroll('1000')
  await page.screenshot({ path: `${output}/ac01-medium.png` })

  await page.setViewportSize({ width: 390, height: 844 })
  await page.waitForTimeout(400)
  assert.equal(await tree().isVisible(), false)
  // 手机上没有缩放滑杆，瀑布流至少两列。
  assert.equal(await page.locator('.al-tiles .cl-masonry-col').count(), 2)
  const side = await page.locator('.cs-side').boundingBox()
  assert(side.height < 80 && side.width >= 380, `sidebar becomes a top bar ${JSON.stringify(side)}`)
  const views = page.getByRole('combobox', { name: '视图' })
  assert(await views.isVisible())
  await noHorizontalScroll('390')
  await page.screenshot({ path: `${output}/ac01-phone.png` })
  await after(() => views.selectOption({ label: '　外景' }))
  await page.getByRole('heading', { name: /婚礼.*外景/ }).waitFor()
  assert.equal(params().get('group'), 'g-out')
  assert.equal(await tiles().count(), 3)
  await noHorizontalScroll('390 group')
})

// AC-02：资料库树：固定视图、两层分组（计数含子组）、按分类的标签、用量。
await check('AC-02', async () => {
  await fresh({ layout: 'grid' })
  await open()
  const views = await tree().getByRole('navigation', { name: '视图' }).getByRole('button').allInnerTexts()
  assert.deepEqual(views.map((v) => v.trim()), ['全部', '未归类', '最爱', '最近添加', '回收站'])
  assert.equal(await n('婚礼'), '5')
  assert.equal(await n('外景'), '3')
  assert.equal(await n('棚拍'), '2')
  assert.equal(await n('品牌'), '2')
  // 收起父组，子组随之隐藏；进入子组时父组自动展开。
  await tree().getByRole('button', { name: '子分组：婚礼' }).click()
  assert.equal(await row('外景').count(), 0)
  await tree().getByRole('button', { name: '子分组：婚礼' }).click()
  assert.equal(await row('外景').count(), 1)
  // 最近添加：7 天内，按添加时间倒序。
  await after(() => row('最近添加').click())
  assert.equal(lastRead().get('view'), 'recent')
  assert.deepEqual(await titles(), ['海边逆光', '白纱与拱门', '棚拍布光', '海风走位', '现场环境音', '拍摄脚本 v2'])
  await after(() => row('全部').click())
  // 标签按分类列出，数量随当前条件；点一下加入筛选。
  const cats = await tree().locator('.al-tag-cat').allInnerTexts()
  assert.deepEqual(cats, ['风格', '光线', '未分类'])
  assert.equal(await n('暖调'), '2')
  await after(() => row('暖调').click())
  assert.equal(params().get('tags'), 't-warm')
  assert.equal(await row('暖调').getAttribute('aria-pressed'), 'true')
  assert.deepEqual(await titles(), ['海边逆光', '白纱与拱门'])
  // all 模式下的数量是「再勾上它还剩几项」。
  assert.equal(await n('胶片'), '1')
  const usage = await tree().locator('.al-usage').innerText()
  assert.match(usage, /70 项 · 已用 3\.2 GB \/ 20 GB/)
  assert.doesNotMatch(usage, /待释放/)
  const meter = await tree().getByRole('meter', { name: '空间用量' }).locator('i').evaluate((e) => e.style.width)
  assert.match(meter, /^16\.\d+%$/)
  await page.screenshot({ path: `${output}/ac02-tree.png` })
})

// AC-03 + V6-016：类型分段带数量、搜索、标签模式、pill、排序、布局偏好、换视图清空、面包屑与子分组。
await check('AC-03', async () => {
  await fresh({ layout: 'grid' })
  await open()
  const kinds = page.getByRole('group', { name: '类型' })
  assert.deepEqual((await kinds.getByRole('button').allInnerTexts()).map((t) => t.replace(/\s+/g, '')), ['全部70', '图片66', '视频1', '音频1', '文字1', '链接1'])
  // 列表、总数和计数来自同一个首屏请求，不另发计数请求。
  const firstReads = server.reads.filter((r) => !r.includes('cursor'))
  assert(firstReads.every((r) => r.includes('include_facets=true')), firstReads.join('\n'))
  // 进入棚拍：没有的类型不显示；面包屑显示父组。
  await after(() => row('棚拍').click())
  await page.getByRole('heading', { name: /婚礼.*棚拍/ }).waitFor()
  assert.deepEqual((await kinds.getByRole('button').allInnerTexts()).map((t) => t.replace(/\s+/g, '')), ['全部2', '图片1', '音频1'])
  await after(() => board().getByRole('heading').getByRole('button', { name: '婚礼' }).click())
  await page.getByRole('heading', { name: '婚礼', exact: true }).waitFor()
  // 进入父组：子分组 chip，包含子组的素材。
  const chips = await page.getByLabel('子分组').getByRole('button').allInnerTexts()
  assert.deepEqual(chips.map((t) => t.replace(/\s+/g, '')), ['外景3', '棚拍2'])
  assert.equal(lastRead().get('include_descendants'), 'true')
  assert.equal(await tiles().count(), 5)
  await after(() => page.getByLabel('子分组').getByRole('button', { name: /外景/ }).click())
  await page.getByRole('heading', { name: /婚礼.*外景/ }).waitFor()
  // 进分组记进历史，可以后退。
  await after(() => page.goBack())
  await page.getByRole('heading', { name: '婚礼', exact: true }).waitFor()
  await after(() => row('全部').click())

  // 关键词覆盖标题、备注、正文、网址和标签。
  const search = page.getByRole('searchbox', { name: '搜索素材' })
  assert.match(await search.getAttribute('placeholder'), /标题.*备注.*正文.*网址.*标签/)
  for (const [q, expected] of [
    ['日落', ['海边逆光']],
    ['08:10', ['拍摄脚本 v2']],
    ['example.com', ['布光参考']],
    ['胶片', ['白纱与拱门', '棚拍布光']],
  ]) {
    await after(() => search.fill(q))
    assert.equal(params().get('q'), q)
    assert.deepEqual(await titles(), expected, q)
  }
  // 按 / 聚焦搜索框。
  await page.locator('.al-tile').first().focus()
  await page.keyboard.press('/')
  assert(await search.evaluate((e) => e === document.activeElement))
  await after(() => board().getByRole('button', { name: '移除条件：“胶片”' }).click())
  assert.equal(await search.inputValue(), '')
  assert.equal(params().has('q'), false)

  // 标签：选一个没有「同时满足 / 满足任一」，选两个后出现，切换后结果变化。
  await board().getByRole('button', { name: '标签', exact: true }).click()
  const pop = page.getByRole('dialog', { name: '标签筛选' })
  await after(() => pop.getByRole('button', { name: /暖调/ }).click())
  assert.equal(await pop.getByRole('button', { name: /同时满足|满足任一/ }).count(), 0)
  await after(() => pop.getByRole('button', { name: /胶片/ }).click())
  assert.deepEqual(await titles(), ['白纱与拱门'])
  await after(() => pop.getByRole('button', { name: '同时满足全部所选' }).click())
  assert.equal(params().get('mode'), 'any')
  assert.deepEqual(await titles(), ['海边逆光', '白纱与拱门', '棚拍布光'])
  // any 模式的数量忽略已选标签。
  assert.equal((await pop.getByRole('button', { name: /逆光/ }).locator('.cl-tag-n').innerText()).trim(), '2')
  await page.keyboard.press('Escape')
  const pills = page.getByLabel('生效的条件')
  assert.deepEqual((await pills.locator('.al-pill').allInnerTexts()).map((t) => t.trim()), ['暖调', '胶片', '满足任一', '清除全部'])
  await after(() => pills.getByRole('button', { name: '移除条件：暖调' }).click())
  assert.equal(params().get('tags'), 't-film')
  assert.equal(await pills.getByRole('button', { name: /满足任一|同时满足/ }).count(), 0)

  // 评分筛选与按评分排序。
  await page.getByRole('combobox', { name: '评分' }).click()
  await after(() => page.getByRole('option', { name: '4 星以上' }).click())
  assert.equal(lastRead().get('min_rating'), '4')
  assert.deepEqual(await titles(), ['白纱与拱门'])
  await after(() => pills.getByRole('button', { name: '清除全部' }).click())
  await page.getByRole('combobox', { name: '评分' }).click()
  await after(() => page.getByRole('option', { name: '未评分' }).click())
  assert.equal(lastRead().get('unrated'), 'true')
  assert.equal(lastRead().has('min_rating'), false)
  await after(() => pills.getByRole('button', { name: '清除全部' }).click())
  await page.getByRole('combobox', { name: '排序' }).click()
  await after(() => page.getByRole('option', { name: '评分' }).click())
  assert.equal(lastRead().get('sort'), 'rating')
  assert.deepEqual((await titles()).slice(0, 4), ['海边逆光', '白纱与拱门', '海风走位', '拍摄脚本 v2'])

  // 换视图时清空筛选，排序保留。
  await after(() => kinds.getByRole('button', { name: /图片/ }).click())
  await after(() => search.fill('素材'))
  await after(() => row('最爱').click())
  assert.deepEqual(Object.fromEntries(params()), { view: 'favorites', sort: 'rating' })
  assert.equal(await search.inputValue(), '')
  assert.deepEqual(await titles(), ['海边逆光'])

  // 瀑布流 / 方格和缩略图大小记在本机。
  await after(() => row('全部').click())
  await board().getByRole('button', { name: '瀑布流' }).click()
  await page.locator('.al-tiles .cl-masonry-col').first().waitFor()
  await page.getByRole('slider', { name: '缩略图大小' }).evaluate((el) => {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value').set.call(el, '300')
    el.dispatchEvent(new Event('input', { bubbles: true }))
  })
  // 浏览区约 820px 宽，300px 的卡片排两列。
  await page.waitForFunction(() => document.querySelectorAll('.al-tiles .cl-masonry-col').length === 2)
  await page.screenshot({ path: `${output}/ac03-masonry.png` })
  await after(() => page.reload())
  assert.equal(await board().getByRole('button', { name: '瀑布流' }).getAttribute('aria-pressed'), 'true')
  assert.equal(await page.getByRole('slider', { name: '缩略图大小' }).inputValue(), '300')
  assert.equal(await page.locator('.al-tiles .cl-masonry-col').count(), 2)
  await board().getByRole('button', { name: '方格' }).click()
  const [w, h] = await tile('海边逆光').locator('.al-thumb').evaluate((e) => [e.clientWidth, e.clientHeight])
  assert(w >= 300 && Math.abs(w - h) <= 1, `grid cells are square ${w}×${h}`)
  await page.screenshot({ path: `${output}/ac03-grid.png` })
})

// V6-016 AC-03：快速切换时旧请求不覆盖新结果；空结果可以清除条件或扩大范围。
await check('V6-016-AC-03', async () => {
  await fresh({ layout: 'grid' })
  await open()
  server.hold = { match: (q) => q.get('kind') === 'video', ms: 1200 }
  const kinds = page.getByRole('group', { name: '类型' })
  await kinds.getByRole('button', { name: /视频/ }).click()
  await after(() => kinds.getByRole('button', { name: /文字/ }).click())
  // 等到被拖住的视频请求也回来了，结果仍是后选的文字。
  await page.waitForTimeout(1500)
  assert.deepEqual(await titles(), ['拍摄脚本 v2'])
  assert.equal(await kinds.getByRole('button', { name: /文字/ }).getAttribute('aria-pressed'), 'true')
  server.hold = null
  await after(() => row('棚拍').click())
  await after(() => page.getByRole('searchbox', { name: '搜索素材' }).fill('海边'))
  await page.getByText('没有符合条件的素材').waitFor()
  await page.screenshot({ path: `${output}/v6016-empty.png` })
  await after(() => page.getByRole('button', { name: '在全部里查找' }).click())
  assert.equal(params().get('q'), '海边')
  assert.equal(params().has('group'), false)
  assert.deepEqual(await titles(), ['海边逆光', '拍摄脚本 v2'])
  await after(() => page.getByRole('searchbox', { name: '搜索素材' }).fill('没有这个词'))
  await page.getByText('没有符合条件的素材').waitFor()
  assert.equal(await page.getByRole('button', { name: '在全部里查找' }).count(), 0)
  await after(() => page.getByRole('button', { name: '清除条件' }).click())
  assert.equal(await tiles().count(), 60)
})

// 翻页：滚到底部附近按游标取下一页，不再带计数；翻页时资料库变化则提示刷新。
await check('paging', async () => {
  await fresh()
  await open()
  assert.equal(await tiles().count(), 60)
  await page.locator('.al-end').scrollIntoViewIfNeeded()
  await page.waitForFunction(() => document.querySelectorAll('.al-tile').length === 70)
  const next = new URLSearchParams(server.reads.find((r) => r.includes('cursor=')))
  assert.equal(next.get('cursor'), '60')
  assert.equal(next.has('include_facets'), false)
  assert.equal(new Set(await titles()).size, 70)
  await after(() => page.reload())
  server.revision = '2'
  await page.locator('.al-end').scrollIntoViewIfNeeded()
  await page.getByText('资产库刚有变化，后面的内容需要重新读取。').waitFor()
  assert.equal(await tiles().count(), 60)
  await after(() => page.getByRole('button', { name: '刷新' }).click())
  await page.getByText('资产库刚有变化').waitFor({ state: 'detached' })
  // 切回标签页时重读。
  await after(() => page.evaluate(() => document.dispatchEvent(new Event('visibilitychange'))))
})

// 切回标签页刷新首屏时，旧列表仍可见，但不能拿旧游标继续翻页，否则会跳过中间一页。
await check('refresh-paging', async () => {
  const assets = Array.from({ length: 180 }, (_, i) => asset({ id: `refresh-${i}`, title: `素材 ${i}`, age: i }))
  await fresh({ assets, layout: 'grid' })
  await open()
  await page.locator('.al-end').scrollIntoViewIfNeeded()
  await page.waitForFunction(() => document.querySelectorAll('.al-tile').length === 120)
  await page.locator('.al-scroll').evaluate((e) => e.scrollTo({ top: 0 }))
  await page.waitForTimeout(100)
  let release
  const held = new Promise((resolve) => { release = resolve })
  server.hold = { match: (q) => !q.has('cursor'), wait: () => held }
  const start = server.reads.length
  await page.evaluate(() => document.dispatchEvent(new Event('visibilitychange')))
  await until(() => server.reads.length > start, 'refresh request')
  try {
    assert.equal(await tiles().count(), 120, 'keep the current page visible during refresh')
    await page.locator('.al-end').scrollIntoViewIfNeeded()
    await page.waitForTimeout(250)
    assert.equal(server.reads.slice(start).some((r) => new URLSearchParams(r).has('cursor')), false, 'must not page with an old cursor while the first page is refreshing')
  } finally {
    // 首屏回来前回到顶部，避免底部观察器立即加载下一页，掩盖首屏断言。
    await page.locator('.al-scroll').evaluate((e) => e.scrollTo({ top: 0 }))
    await page.waitForTimeout(100)
    release()
    server.hold = null
  }
  await page.waitForFunction(() => document.querySelectorAll('.al-tile').length === 60)
  for (const count of [120, 180]) {
    await page.locator('.al-end').scrollIntoViewIfNeeded()
    await page.waitForFunction((n) => document.querySelectorAll('.al-tile').length === n, count)
  }
  assert.deepEqual(await titles(), assets.map((a) => a.title), 'refresh and subsequent pages must not omit or reorder any assets')
})

// AC-01/03 边界：长分组名、子分组和200字搜索词不能把浏览区或移除按钮撑出屏幕。
await check('long-content', async () => {
  const originalNames = groups.map((g) => g.name)
  const longQuery = '测试'.repeat(100)
  groups[0].name = '父组'.repeat(25)
  groups[1].name = '子组'.repeat(25)
  try {
    for (const width of [390, 1000, 1440]) {
      await fresh({ width })
      await open('?group=g-out')
      await noHorizontalScroll(`long breadcrumbs ${width}`)
      await after(() => page.locator('.al-crumb h2 button').click())
      assert.equal(params().get('group'), 'g-wed')
      await noHorizontalScroll(`long subgroup ${width}`)
      await after(() => page.getByRole('button', { name: groups[1].name, exact: false }).filter({ has: page.locator('small') }).click())
      assert.equal(params().get('group'), 'g-out')
      await after(() => page.getByRole('searchbox', { name: '搜索素材' }).fill(longQuery))
      const remove = page.getByRole('button', { name: `移除条件：“${longQuery}”` })
      const r = await remove.boundingBox()
      assert(r && r.x >= 0 && r.x + r.width <= width, `remove button outside viewport at ${width}`)
      await noHorizontalScroll(`long search ${width}`)
      await page.screenshot({ path: `${output}/long-content-${width}.png` })
      await after(() => remove.click())
      assert.equal(params().has('q'), false)
      assert.equal(await page.getByRole('searchbox', { name: '搜索素材' }).inputValue(), '')
    }
  } finally {
    groups.forEach((g, i) => { g.name = originalNames[i] })
  }
})

// AC-09 + V6-060：卡片下方只有标题和只读星级；五种类型各有样式；链接不抓取网页。
await check('AC-09', async () => {
  await fresh()
  const external = []
  page.on('request', (r) => {
    if (r.url().includes('example.com')) external.push(r.url())
  })
  await open('?view=recent')
  const sea = tile('海边逆光')
  assert.equal((await sea.locator('.al-cap > span').first().innerText()).trim(), '海边逆光')
  assert.equal(await sea.getByRole('img', { name: '5 星' }).count(), 1)
  const fill = await sea.locator('.cl-star-fill').first().evaluate((e) => getComputedStyle(e).color)
  assert.equal(fill, 'rgb(255, 255, 255)')
  assert.equal(await tile('白纱与拱门').getByRole('img', { name: '4.5 星' }).count(), 1)
  // 视频：首帧封面 + 时长。
  const clip = tile('海风走位')
  await clip.locator('video[preload="metadata"]').waitFor({ state: 'attached' })
  assert.equal((await clip.locator('.cl-video-badge').innerText()).trim(), '0:12')
  // 音频：装饰条 + 时长，不是通用图标；未评分的没有星级。
  const voice = tile('现场环境音')
  assert.equal(await voice.locator('.cl-wave i').count(), 36)
  assert.equal((await voice.locator('.cl-card-row small').innerText()).trim(), '3:04')
  assert.equal(await voice.locator('.cc-audio, .cc-media-placeholder').count(), 0)
  assert.equal(await voice.locator('.al-cap').count(), 0)
  // 文字：正文前几行；卡片下方不再重复标题，只放星级。
  const note = tile('拍摄脚本 v2')
  assert.match(await note.locator('.cl-card-text p').innerText(), /07:30 集合/)
  assert.equal((await note.locator('.al-cap > span').first().innerText()).trim(), '')
  assert.equal(await note.getByRole('img', { name: '4 星' }).count(), 1)
  await page.screenshot({ path: `${output}/ac09-cards.png` })
  await after(() => row('品牌').click())
  const ref = tile('布光参考')
  assert.equal((await ref.locator('.cl-card-link small').innerText()).trim(), 'example.com')
  assert.equal(external.length, 0, external.join('|'))
  // 单击选中，检查器显示详情；点空白处取消。
  await tile('白纱与拱门').click()
  const inspector = page.getByRole('complementary', { name: '检查器' })
  assert.equal(await inspector.getByRole('textbox', { name: '标题', exact: true }).inputValue(), '白纱与拱门')
  assert.match(await inspector.innerText(), /1600 × 1067/)
  assert.match(await inspector.innerText(), /外景/)
  await page.screenshot({ path: `${output}/ac09-inspector.png` })
  await page.locator('.al-scroll').click({ position: { x: 5, y: 600 } })
  await inspector.getByText('没有选中素材').waitFor()
})


const saved = async () => { await page.waitForFunction(() => document.querySelector('.al-page')?.getAttribute('aria-busy') === 'false') }
const selectedIDs = () => page.locator('.al-tile.is-on').evaluateAll((els) => els.map((e) => e.dataset.asset))
await check('AC-04-selection', async () => {
  await fresh({ layout: 'grid' }); await open('?view=recent')
  await tile('海边逆光').getByRole('button', { name: '选择：海边逆光', exact: true }).focus()
  await page.keyboard.press('Enter'); assert.deepEqual(await selectedIDs(), ['f-sea'])
  await page.keyboard.press('Enter'); assert.deepEqual(await selectedIDs(), [])
  await tile('海边逆光').click()
  await tile('棚拍布光').click({ modifiers: ['Shift'] })
  assert.deepEqual(await selectedIDs(), ['f-sea', 'f-veil', 'f-studio'])
  await tile('白纱与拱门').click({ modifiers: ['Meta'] })
  assert.deepEqual(await selectedIDs(), ['f-sea', 'f-studio'])
  await page.keyboard.press('Escape'); assert.deepEqual(await selectedIDs(), [])
  await page.keyboard.press('Meta+a'); assert.equal((await selectedIDs()).length, 6)
  await page.keyboard.press('Escape')
  await tile('海边逆光').click(); await tile('白纱与拱门').click({ modifiers: ['Meta'] })
  const transfer = await page.evaluateHandle(() => new DataTransfer())
  await tile('海边逆光').dispatchEvent('dragstart', { dataTransfer: transfer })
  await row('棚拍').dispatchEvent('drop', { dataTransfer: transfer }); await saved()
  assert(server.assets.filter((a) => ['f-sea', 'f-veil'].includes(a.id)).every((a) => a.group_ids.includes('g-in')))
  await page.screenshot({ path: `${output}/step3-multi-inspector.png` })
  assert.equal(await page.getByRole('toolbar', { name: '批量操作' }).isVisible(), false)
  await page.getByRole('button', { name: '检查器', exact: true }).click()
  assert(await page.getByRole('toolbar', { name: '批量操作' }).isVisible())
  await page.setViewportSize({ width: 390, height: 844 })
  await noHorizontalScroll('step3 phone batch')
  await page.screenshot({ path: `${output}/step3-phone-batch.png` })
})
await check('AC-05-edit-rate', async () => {
  await fresh(); await open('?view=recent'); await tile('白纱与拱门').click()
  const inspector = page.getByRole('complementary', { name: '检查器' })
  const title = inspector.getByRole('textbox', { name: '标题', exact: true })
  await title.fill('白纱新标题'); await title.press('Tab'); await saved()
  assert.equal(server.assets.find((a) => a.id === 'f-veil').title, '白纱新标题')
  const slider = inspector.getByRole('slider', { name: '评分', exact: true })
  const third = slider.locator('[data-star="3"]')
  await third.click({ position: { x: 3, y: 8 } }); await saved()
  assert.equal(server.assets.find((a) => a.id === 'f-veil').rating, 2.5)
  await third.click({ position: { x: 3, y: 8 } }); await saved()
  assert.equal(server.assets.find((a) => a.id === 'f-veil').rating, null)
  await slider.focus(); await page.keyboard.press('ArrowRight'); await saved()
  assert.equal(server.assets.find((a) => a.id === 'f-veil').rating, .5)
  await tile('海边逆光').click({ modifiers: ['Meta'] }); await page.keyboard.press('4'); await saved()
  assert.equal(server.assets.find((a) => a.id === 'f-sea').rating, 4)
  await page.locator('.al-toast').getByRole('button', { name: '撤销', exact: true }).click(); await saved()
  assert.equal(server.assets.find((a) => a.id === 'f-sea').rating, 5)
  assert.equal(server.assets.find((a) => a.id === 'f-veil').rating, .5)
  await inspector.getByRole('button', { name: '标签', exact: true }).click()
  await page.getByRole('textbox', { name: '搜索或新建标签' }).fill('新风格')
  await page.getByRole('button', { name: '新建标签「新风格」' }).click(); await saved()
  const newTag = server.tags.find((t) => t.name === '新风格')
  assert(newTag && server.assets.filter((a) => ['f-sea', 'f-veil'].includes(a.id)).every((a) => a.tag_ids.includes(newTag.id)))
  await page.getByRole('dialog', { name: '打标签', exact: true }).press('Escape')
  server.failNext = { path: '/assets/batch-rate', status: 409 }
  await page.keyboard.press('2'); await saved()
  await page.locator('.al-toast').getByText('已在别处修改，已刷新').waitFor()
  assert.equal(server.assets.find((a) => a.id === 'f-sea').rating, 5)
  server.loseNext = '/assets/batch-rate'
  await page.keyboard.press('3')
  await page.locator('.al-toast').getByRole('button', { name: '重试' }).waitFor()
  await page.locator('.al-toast').getByRole('button', { name: '重试' }).click(); await saved()
  const rates = server.writes.filter((w) => w.path.endsWith('/assets/batch-rate'))
  assert.equal(rates.at(-1).key, rates.at(-2).key)
  assert.equal(server.assets.find((a) => a.id === 'f-sea').rating, 3)
})
await check('AC-06-place', async () => {
  await fresh(); await open('?view=recent'); await tile('海边逆光').click(); await tile('白纱与拱门').click({ modifiers: ['Meta'] })
  const inspector = page.getByRole('complementary', { name: '检查器' })
  await inspector.getByRole('button', { name: '放入项目…', exact: true }).click()
  await page.getByRole('button', { name: '新建项目并放入', exact: true }).click()
  await page.getByRole('textbox', { name: '新项目名称' }).fill('素材整理测试')
  server.failNext = { path: '/commands', status: 503, code: 'temporary' }
  await page.getByRole('button', { name: '新建并放入', exact: true }).click()
  await page.locator('.al-toast').getByRole('button', { name: '重试' }).waitFor()
  assert.equal(server.projects.length, 1)
  await page.locator('.al-toast').getByRole('button', { name: '重试' }).click(); await saved()
  assert.equal(server.projects.length, 1)
  const writes = server.writes.filter((w) => w.path.endsWith('/commands'))
  assert.equal(writes[0].key, writes[1].key)
  const first = writes[1].body.payload
  assert.equal(first.type, 'batch'); assert.equal(first.actions.length, 2)
  assert.deepEqual(first.actions.map((a) => [a.x, a.y]), [[0, 0], [340, 0]])
  assert(first.change_group_id)
  // 同一项目已有节点：右边缘+40，结构冲突时重读并换新命令再试一次。
  await inspector.getByRole('button', { name: '放入项目…', exact: true }).click()
  server.failNext = { path: '/commands', status: 409, code: 'creative_revision_conflict' }
  await page.getByRole('dialog', { name: '放入项目', exact: true }).getByRole('button', { name: '素材整理测试', exact: true }).click(); await saved()
  const again = server.writes.filter((w) => w.path.endsWith('/commands')).slice(-2)
  assert.notEqual(again[0].key, again[1].key)
  assert.deepEqual(again[1].body.payload.actions.map((a) => [a.x, a.y]), [[680, 0], [1020, 0]])
  assert.equal(server.canvases.values().next().value.nodes.length, 4)
})
await check('marquee-limit', async () => {
  await fresh({ layout: 'grid' }); await open('?view=recent')
  const scroll = await page.locator('.al-scroll').boundingBox(), first = await tiles().first().boundingBox()
  await page.mouse.move(scroll.x + 2, scroll.y + 2); await page.mouse.down()
  await page.mouse.move(first.x + first.width - 4, first.y + first.height - 4, { steps: 6 }); await page.mouse.up()
  assert.deepEqual(await selectedIDs(), ['f-sea'])
  await page.keyboard.press('Escape')
  await fresh({ layout: 'grid', assets: Array.from({ length: 120 }, (_, i) => asset({ id: 'limit-' + i, title: '素材 ' + i, age: i })) }); await open()
  await page.locator('.al-end').scrollIntoViewIfNeeded(); await page.waitForFunction(() => document.querySelectorAll('.al-tile').length === 120)
  await page.keyboard.press('Meta+a'); assert.equal((await selectedIDs()).length, 120)
  await page.getByRole('complementary', { name: '检查器' }).getByText('一次最多处理 100 项').waitFor()
  assert(await page.getByRole('complementary', { name: '检查器' }).getByRole('button', { name: '放入项目…', exact: true }).isDisabled())
  await page.keyboard.press('4'); assert.equal(server.writes.length, 0)
})
await check('group-and-tag-editors', async () => {
  await fresh(); await open('?view=recent')
  await tree().getByRole('button', { name: '新建分组', exact: true }).click()
  await tree().getByRole('textbox', { name: '新分组名称', exact: true }).fill('新建分类')
  await tree().getByRole('textbox', { name: '新分组名称', exact: true }).press('Enter'); await saved()
  await row('新建分类').waitFor()
  await row('新建分类').click({ button: 'right' }); await page.getByRole('menuitem', { name: '重命名', exact: true }).click()
  await tree().getByRole('textbox', { name: '分组名称', exact: true }).fill('改名分类'); await tree().getByRole('textbox', { name: '分组名称', exact: true }).press('Enter'); await saved()
  await row('改名分类').waitFor()
  await tile('海边逆光').click(); await tile('白纱与拱门').click({ modifiers: ['Meta'] })
  const inspector = page.getByRole('complementary', { name: '检查器' })
  assert.equal(await inspector.locator('.al-tagchip.partial').filter({ hasText: '品牌' }).count(), 1)
  await inspector.getByRole('button', { name: '补齐分组：品牌', exact: true }).click(); await saved()
  assert(server.assets.filter((a) => ['f-sea', 'f-veil'].includes(a.id)).every((a) => a.group_ids.includes('g-brand')))
  await inspector.getByRole('button', { name: '标签', exact: true }).click()
  const pop = page.getByRole('dialog', { name: '打标签', exact: true })
  await pop.getByRole('button', { name: '暖调', exact: true }).click(); await saved()
  assert(server.assets.filter((a) => ['f-sea', 'f-veil'].includes(a.id)).every((a) => !a.tag_ids.includes('t-warm')))
  assert(await pop.isVisible())
  await page.screenshot({ path: `${output}/step3-tag-picker.png` })
})
await check('V6-018-undo', async () => {
  await fresh(); await open()
  await row('婚礼').click({ button: 'right' })
  await page.getByRole('menuitem', { name: '删除分组', exact: true }).click()
  await page.getByRole('alertdialog').getByRole('button', { name: '删除分组', exact: true }).click(); await saved()
  assert(!server.groups.some((g) => g.id === 'g-wed'))
  assert.equal(server.groups.find((g) => g.id === 'g-out').parent_id, null)
  assert.equal(server.assets.length, 71)
  await page.locator('.al-toast').getByRole('button', { name: '撤销', exact: true }).click(); await saved()
  assert(server.groups.some((g) => g.id === 'g-wed'))
  assert.equal(server.groups.find((g) => g.id === 'g-out').parent_id, 'g-wed')
  await page.reload(); await settled(); await row('婚礼').waitFor()
})

await check('V6-018-restore-refusal', async () => {
  for (const [code, text] of [
    ['creative_revision_conflict', '分组已经有新的调整，无法撤销；请刷新后查看'],
    ['creative_group_restore_limit', '部分素材已达到分组数量上限，无法恢复'],
  ]) {
    await fresh(); await open()
    await row('婚礼').click({ button: 'right' })
    await page.getByRole('menuitem', { name: '删除分组', exact: true }).click()
    await page.getByRole('alertdialog').getByRole('button', { name: '删除分组', exact: true }).click(); await saved()
    server.failNext = { path: '/restore', status: 409, code, message: text }
    await page.locator('.al-toast').getByRole('button', { name: '撤销', exact: true }).click(); await saved()
    await page.getByText(text, { exact: true }).waitFor()
    assert(!server.groups.some((g) => g.id === 'g-wed'))
    assert(await page.getByRole('button', { name: '关闭提示', exact: true }).isEnabled())
  }
})

await check('failure-recovery-guards', async () => {
  const unavailable = fixture(); unavailable[0].unavailable = true
  await fresh({ assets: unavailable }); await open()
  await tile('海边逆光').click({ button: 'right' })
  assert(await page.getByRole('menuitem', { name: '下载', exact: true }).isDisabled())
  assert(await page.getByRole('menuitem', { name: '放入项目…', exact: true }).isDisabled())
  await page.keyboard.press('Escape')
  await tile('白纱与拱门').click()
  // 列表仍可用，但详细读取发现内容已不可用；不能把只读失败留成 pending。
  server.assets.find((a) => a.id === 'f-veil').unavailable = true
  await page.getByRole('complementary', { name: '检查器' }).getByRole('button', { name: '下载', exact: true }).click()
  await page.getByText('素材当前无法下载', { exact: true }).waitFor()
  assert(await page.getByRole('button', { name: '关闭提示', exact: true }).isEnabled())
  await tile('棚拍布光').click()
  assert(await page.getByRole('textbox', { name: '标题', exact: true }).isEnabled())
  assert.equal(server.writes.length, 0)

  await fresh({ layout: 'grid', assets: Array.from({ length: 120 }, (_, i) => asset({ id: 'limit-' + i, title: '素材 ' + i, age: i })) }); await open()
  await page.locator('.al-end').scrollIntoViewIfNeeded()
  await page.waitForFunction(() => document.querySelectorAll('.al-tile').length === 120)
  await tile('素材 0').click(); server.loseNext = '/assets/batch-rate'; await page.keyboard.press('3')
  await page.locator('.al-toast').getByRole('button', { name: '重试', exact: true }).waitFor()
  await page.keyboard.press('Meta+a')
  const transfer = await page.evaluateHandle(() => new DataTransfer())
  await tile('素材 0').dispatchEvent('dragstart', { dataTransfer: transfer })
  await row('棚拍').dispatchEvent('drop', { dataTransfer: transfer })
  const retry = page.locator('.al-toast').getByRole('button', { name: '重试', exact: true })
  assert.equal(await retry.count(), 1, 'invalid drop must preserve unknown-operation recovery')
  await retry.click(); await saved()
  assert.equal(server.writes.length, 2)
  assert.equal(server.writes[0].body.operation_id, server.writes[1].body.operation_id)
  await tile('素材 1').click()
  assert(await page.getByRole('textbox', { name: '标题', exact: true }).isEnabled())
})

await check('viewer-opening', async () => {
  await fresh(); await open()
  const card = tile('海边逆光')
  await card.click()
  assert.equal(await page.locator('.viewer-back').count(), 0)
  await card.dblclick()
  await page.getByRole('dialog', { name: '查看：海边逆光', exact: true }).waitFor()
  await page.keyboard.press('Escape')
  await page.locator('.viewer-back').waitFor({ state: 'detached' })
  assert(await card.evaluate((el) => el === document.activeElement))
  await card.press('Enter')
  await page.locator('.viewer-back').waitFor()
  await page.keyboard.press('Escape')
  await card.press('Space')
  await page.locator('.viewer-back').waitFor()
  await page.keyboard.press('Escape')
  await card.hover()
  await card.getByRole('button', { name: '查看：海边逆光', exact: true }).click()
  await page.locator('.viewer-back').waitFor()
})

const viewer = () => page.locator('.viewer')
const viewerReady = async () => { await viewer().waitFor(); await until(async () => await viewer().getByRole('textbox', { name: '标题', exact: true }).isEnabled(), 'viewer detail') }
await check('viewer-performance-revisit', async () => {
  await fresh()
  let originalReads = 0
  await page.route('**/creative/media-access-tickets', route => {
    const b = route.request().postDataJSON()
    if (b.role === 'original' && ['r-f-sea', 'r-f-veil'].includes(b.content_revision_id))
      return route.fulfill({ json: { url: `${base}/__qa_perf/${b.content_revision_id}.svg`, expires_at: new Date(Date.now() + 3600000).toISOString(), mime: 'image/svg+xml', byte_size: 200 } })
    return route.fallback()
  })
  await page.route('**/__qa_perf/*', route => {
    originalReads++
    return route.fulfill({ contentType: 'image/svg+xml', headers: { 'Cache-Control': 'private, no-store' }, body: '<svg xmlns="http://www.w3.org/2000/svg" width="1200" height="1800"><rect width="100%" height="100%" fill="blue"/></svg>' })
  })
  await open()
  const reads = []
  page.on('request', r => { if (r.method() === 'GET' && /\/creative\/assets\/f-(sea|veil)$/.test(r.url())) reads.push(r.url()) })
  await tile('海边逆光').dblclick(); await viewerReady()
  await page.waitForFunction(() => document.querySelector('.v-image img')?.dataset.quality === 'original')
  await viewer().getByRole('button', { name: '下一项' }).click(); await viewerReady()
  await page.waitForFunction(() => document.querySelector('.v-image img')?.dataset.quality === 'original')
  const initialReads = reads.length
  assert.equal(originalReads, 2, '两张原图各读取一次，后台解码与显示复用同一份字节')
  for (let i = 0; i < 3; i++) {
    await viewer().getByRole('button', { name: '上一项' }).click(); await viewerReady()
    await viewer().getByRole('button', { name: '下一项' }).click(); await viewerReady()
  }
  assert.equal(reads.length, initialReads, `往返三次应复用详情，额外读取 ${reads.length - initialReads} 次`)
  assert.equal(originalReads, 2, '媒体接口 no-store 时，往返仍不得再次下载原图')
  const immediate = await viewer().evaluate(async el => {
    el.querySelector('.v-nav.prev').click()
    await new Promise(requestAnimationFrame)
    return { image: !!el.querySelector('.v-image img'), loading: el.textContent.includes('正在读取素材…') }
  })
  assert(immediate.image && !immediate.loading, '翻回已看图片应直接显示，不能重新进入整屏加载态')
  // 写入后的 refresh 必须让详情缓存失效。
  await viewerReady(); await viewer().getByRole('button', { name: '关闭查看' }).focus(); await page.keyboard.press('2'); await saved()
  await viewerReady()
  assert.equal(await viewer().getByRole('slider', { name: '评分', exact: true }).getAttribute('aria-valuenow'), '2')
  assert(reads.length > initialReads, '写入后应重新校验详情')
  console.log(JSON.stringify({ performance: { initialReads, revisits: 6, repeatReads: 0, originalReads, repeatOriginalReads: 0 } }))
})
await check('viewer-performance-progressive', async () => {
  await fresh(); await open()
  await tile('海边逆光').locator('img').evaluate(img => img.decode())
  let detailRelease, imageRelease
  const detailWait = new Promise(r => { detailRelease = r })
  const imageWait = new Promise(r => { imageRelease = r })
  await page.route('**/creative/assets/f-sea', async route => { await detailWait; await route.fulfill({ json: server.assets[0] }).catch(() => {}) })
  await page.route('**/creative/media-access-tickets', route => {
    const b = route.request().postDataJSON()
    if (b.content_revision_id === 'r-f-sea' && b.role === 'original')
      return route.fulfill({ json: { url: `${base}/__qa_original.svg`, expires_at: new Date(Date.now() + 3600000).toISOString(), mime: 'image/svg+xml', byte_size: 200 } })
    return route.fallback()
  })
  await page.route('**/__qa_original.svg', async route => {
    await imageWait
    await route.fulfill({ contentType: 'image/svg+xml', headers: { 'Cache-Control': 'private, max-age=3600' }, body: '<svg xmlns="http://www.w3.org/2000/svg" width="1200" height="1800"><rect width="100%" height="100%" fill="blue"/></svg>' }).catch(() => {})
  })
  try {
    await tile('海边逆光').dblclick()
    await page.waitForFunction(() => document.querySelector('.v-image img')?.naturalWidth > 0)
    assert(await viewer().getByRole('textbox', { name: '标题', exact: true }).isDisabled(), '详情尚未返回时禁止编辑')
    assert.match(await viewer().locator('.v-image img').getAttribute('src'), /^data:/, '先显示列表已加载的展示图')
    detailRelease(); await viewerReady()
    const surface = viewer().locator('.v-image'), box = await surface.boundingBox()
    await surface.dispatchEvent('wheel', { deltaY: -100, deltaX: 0, clientX: box.x + box.width / 2, clientY: box.y + box.height / 2 })
    await viewer().getByRole('button', { name: '还原缩放' }).waitFor()
    const zoom = await viewer().getByRole('button', { name: '还原缩放' }).innerText()
    imageRelease()
    await page.waitForFunction(() => document.querySelector('.v-image img')?.dataset.quality === 'original')
    assert.equal(await viewer().getByRole('button', { name: '还原缩放' }).innerText(), zoom, '原图替换展示图不重置缩放')
    await viewer().getByRole('button', { name: '下一项' }).click(); await viewerReady()
    assert.equal(await viewer().getByRole('button', { name: '还原缩放' }).count(), 0, '翻项仍重置缩放')
  } finally { detailRelease(); imageRelease() }
})
await check('viewer-performance-preview-failure', async () => {
  await fresh()
  let release, originalRequests = 0
  const wait = new Promise(r => { release = r })
  await page.route('**/creative/media-access-tickets', route => {
    const b = route.request().postDataJSON()
    if (b.content_revision_id === 'r-f-sea')
      return route.fulfill({ json: { url: `${base}/__qa_${b.role}.svg`, expires_at: new Date(Date.now() + 3600000).toISOString(), mime: 'image/svg+xml', byte_size: 200 } })
    return route.fallback()
  })
  await page.route('**/__qa_display.svg', route => route.fulfill({ status: 404, body: 'missing preview' }))
  await page.route('**/__qa_original.svg', async route => {
    originalRequests++
    await wait
    await route.fulfill({ contentType: 'image/svg+xml', body: '<svg xmlns="http://www.w3.org/2000/svg" width="1200" height="1800"><rect width="100%" height="100%" fill="blue"/></svg>' }).catch(() => {})
  })
  try {
    await open(); await tile('海边逆光').dblclick(); await viewerReady()
    await until(() => originalRequests > 0, 'original started')
    await until(async () => await viewer().getByText('正在加载媒体…', { exact: true }).count() > 0 || await viewer().getByText('媒体加载失败', { exact: true }).count() > 0, 'preview settled')
    assert.equal(await viewer().getByText('媒体加载失败', { exact: true }).count(), 0, '展示图失败不能中断正在读取的原图')
    release()
    await page.waitForFunction(() => {
      const img = document.querySelector('.v-image img')
      return img?.dataset.quality === 'original' && img.naturalWidth > 0
    })
    assert.equal(originalRequests, 1, '无需人工重试即可显示原图')
  } finally { release() }
})
await check('viewer-image-zoom', async () => {
  await fresh(); await open()
  // 卡片取展示图；浮窗必须另取原图，不能沿用卡片那张。
  const id = await tile('海边逆光').getAttribute('data-asset')
  const original = page.waitForRequest((r) => r.url().endsWith('/media-access-tickets') && r.postDataJSON()?.content_revision_id === `r-${id}` && r.postDataJSON()?.role === 'original', { timeout: 5000 })
  await tile('海边逆光').dblclick(); await viewerReady(); await original
  await page.waitForFunction(() => document.querySelector('.v-image img')?.naturalWidth > 0)
  const metrics = await viewer().evaluate((el) => {
    const stage = el.querySelector('.v-stage'), img = el.querySelector('.v-image img'), panel = el.querySelector('.v-panel')
    return { panel: panel.getBoundingClientRect().width, fit: getComputedStyle(img).objectFit, ratio: img.offsetHeight / img.offsetWidth,
      bodyPadding: getComputedStyle(el.querySelector('.v-body')).padding, star: el.querySelector('[data-star]').getBoundingClientRect().width,
      skinBlur: getComputedStyle(el).backdropFilter, backdrop: getComputedStyle(el.querySelector('.v-backdrop')).filter,
      fits: img.offsetWidth <= stage.clientWidth && img.offsetHeight <= stage.clientHeight }
  })
  assert.equal(metrics.panel, 340); assert.equal(metrics.fit, 'contain'); assert(Math.abs(metrics.ratio - 1.5) < 0.01); assert(metrics.fits)
  assert.equal(metrics.bodyPadding, '18px 18px 12px'); assert.equal(metrics.star, 22); assert.equal(metrics.skinBlur, 'none'); assert(metrics.backdrop.includes('blur(48px)'))
  const img = page.locator('.v-image img'), surface = page.locator('.v-image')
  await img.click(); assert.equal(await page.getByRole('button', { name: '还原缩放' }).count(), 0)
  const box = await surface.boundingBox()
  await surface.dispatchEvent('wheel', { deltaY: -400, deltaX: 0, ctrlKey: true, clientX: box.x + box.width * .6, clientY: box.y + box.height * .4 })
  await page.getByRole('button', { name: '还原缩放' }).waitFor()
  assert.match(await page.getByRole('button', { name: '还原缩放' }).innerText(), /800%/)
  await surface.dispatchEvent('wheel', { deltaY: 8.5, deltaX: 40.5 })
  const pan = await img.evaluate((el) => { const m = new DOMMatrix(getComputedStyle(el).transform); return { s: m.a, x: m.e, y: m.f } })
  assert.equal(pan.s, 8)
  await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2); await page.mouse.down(); await page.mouse.move(box.x + box.width - 10, box.y + box.height - 10); await page.mouse.up()
  await page.getByRole('button', { name: '还原缩放' }).click()
  assert.equal(await page.getByRole('button', { name: '还原缩放' }).count(), 0)
  await surface.dispatchEvent('wheel', { deltaY: -100, deltaX: 0, clientX: box.x + box.width / 2, clientY: box.y + box.height / 2 })
  await viewer().getByRole('button', { name: '下一项' }).click(); await viewerReady()
  assert.equal(await page.getByRole('button', { name: '还原缩放' }).count(), 0)
  await page.screenshot({ path: `${output}/step4-viewer-desktop.png` })
})
await check('viewer-edit-and-popovers', async () => {
  await fresh(); await open(); await tile('海边逆光').dblclick(); await viewerReady()
  const title = viewer().getByRole('textbox', { name: '标题', exact: true })
  await title.fill('浮窗改名'); await title.press('Enter'); await saved()
  assert.equal(server.assets[0].title, '浮窗改名')
  const note = viewer().getByRole('textbox', { name: '备注', exact: true })
  await note.fill('浮窗备注'); await note.press('Tab'); await saved(); assert.equal(server.assets[0].description, '浮窗备注')
  const star = viewer().getByRole('slider', { name: '评分', exact: true })
  await star.focus(); await star.press('ArrowLeft'); await saved(); assert.equal(server.assets[0].rating, 4.5)
  await star.press('2'); await saved(); assert.equal(server.assets[0].rating, 2, 'focused rating accepts digit keys')
  await star.press('0'); await saved(); assert.equal(server.assets[0].rating, null, 'focused rating accepts clearing')
  await viewer().getByRole('button', { name: '关闭查看' }).focus(); await page.keyboard.press('2'); await saved(); assert.equal(server.assets[0].rating, 2)
  await viewer().getByRole('button', { name: '标签', exact: true }).click()
  const pop = viewer().getByRole('dialog', { name: '打标签', exact: true })
  await pop.waitFor(); assert(await pop.evaluate((el) => !!el.closest('.viewer')))
  await pop.getByRole('button', { name: '胶片', exact: true }).click(); await saved(); assert(server.assets[0].tag_ids.includes('t-film'))
  await page.keyboard.press('Escape'); await pop.waitFor({ state: 'detached' }); assert(await viewer().isVisible())
  assert.equal(await page.evaluate(() => document.activeElement?.textContent?.trim()), '标签')
  await viewer().getByRole('button', { name: '归入分组', exact: true }).click()
  await viewer().getByRole('dialog', { name: '归入分组', exact: true }).getByRole('button', { name: '品牌', exact: true }).click(); await saved()
  assert(server.assets[0].group_ids.includes('g-brand'))
  await viewer().getByRole('button', { name: '移出分组：品牌', exact: true }).click(); await saved(); assert(!server.assets[0].group_ids.includes('g-brand'))
  server.failNext = { path: '/metadata', status: 409 }
  await title.fill('冲突改名'); await title.press('Enter'); await saved()
  assert.equal(await title.inputValue(), '浮窗改名')
  await viewer().getByText('已在别处修改，已刷新', { exact: true }).waitFor()
  server.loseNext = '/assets/batch-rate'
  await viewer().getByRole('button', { name: '关闭查看' }).focus(); await page.keyboard.press('3')
  await viewer().getByRole('button', { name: '重试', exact: true }).waitFor()
  assert(await viewer().getByRole('button', { name: '关闭查看' }).isDisabled())
  await page.keyboard.press('Escape'); assert(await viewer().isVisible())
  await viewer().getByRole('button', { name: '重试', exact: true }).click(); await saved()
  assert.equal(server.writes.at(-1).key, server.writes.at(-2).key)
  for (let i = 0; i < 30; i++) { await page.keyboard.press('Tab'); assert(await page.evaluate(() => !!document.activeElement?.closest('.viewer'))) }
})
await check('viewer-project-download-trash', async () => {
  await fresh(); await open(); await tile('海边逆光').dblclick(); await viewerReady()
  await viewer().getByRole('button', { name: '放入项目…', exact: true }).click()
  const project = viewer().getByRole('dialog', { name: '放入项目', exact: true })
  await project.getByRole('button', { name: '新建项目并放入', exact: true }).click()
  await project.getByRole('textbox', { name: '新项目名称', exact: true }).fill('浮窗项目')
  await project.getByRole('button', { name: '新建并放入', exact: true }).click(); await saved()
  assert.equal(server.projects.length, 1); assert.equal([...server.canvases.values()][0].nodes.length, 1)
  await viewer().getByRole('button', { name: '打开画布', exact: true }).waitFor()
  const ticket = page.waitForRequest((r) => r.url().endsWith('/media-access-tickets') && r.postDataJSON()?.purpose === 'download')
  const download = page.waitForEvent('download')
  await viewer().getByRole('button', { name: '下载', exact: true }).click(); await download; assert.equal((await ticket).postDataJSON().role, 'original'); await saved()
  await viewer().getByRole('button', { name: '移到回收站', exact: true }).click()
  await viewer().getByRole('alertdialog').getByRole('button', { name: '确认', exact: true }).click(); await saved()
  assert(server.assets[0].deleted_at); await viewer().getByText('已移到回收站', { exact: true }).first().waitFor()
  await viewer().getByRole('button', { name: '恢复', exact: true }).click(); await saved()
  assert.equal(server.assets[0].deleted_at, null); assert.equal(await viewer().getByRole('button', { name: '恢复', exact: true }).count(), 0)
  await page.keyboard.press('Escape'); await tile('海边逆光').waitFor()
  await until(async () => tile('海边逆光').evaluate((el) => document.activeElement === el), 'restored tile regains focus even after DOM replacement')
})
await check('viewer-types-responsive', async () => {
  await fresh(); await open()
  for (const [title, kind] of [['海风走位', 'video'], ['现场环境音', 'audio']]) {
    await tile(title).dblclick(); await viewerReady()
    const media = viewer().locator(`.v-media ${kind}`)
    await media.waitFor(); assert(await media.evaluate((el) => el.controls))
    await viewer().getByRole('button', { name: '关闭查看' }).focus()
    let reached = false
    for (let i = 0; i < 40 && !reached; i++) { await page.keyboard.press('Tab'); reached = await media.evaluate((el) => document.activeElement === el) }
    assert(reached, `${kind} native controls participate in keyboard focus loop`)
    await media.evaluate((el) => new Promise((resolve) => { if (el.readyState >= 1) resolve(); else el.addEventListener('loadedmetadata', resolve, { once: true }) }))
    await viewer().getByRole('button', { name: '关闭查看' }).focus(); await page.keyboard.press('Space')
    await until(async () => await media.evaluate((el) => !el.paused), 'media plays on Space')
    await page.keyboard.press('Space'); assert(await media.evaluate((el) => el.paused))
    await page.keyboard.press('Escape')
  }
  server.assets.find((a) => a.id === 'f-note').content.payload.body = '完整正文\n'.repeat(500)
  await tile('拍摄脚本 v2').dblclick(); await viewerReady(); assert.equal((await viewer().locator('.v-text p').innerText()).trim(), '完整正文\n'.repeat(500).trim())
  await page.keyboard.press('Escape')
  await tile('布光参考').dblclick(); await viewerReady()
  assert.equal(await viewer().getByRole('link', { name: '打开网址' }).getAttribute('href'), 'https://www.example.com/lighting/101')
  await page.keyboard.press('Escape')
  await page.setViewportSize({ width: 390, height: 844 }); await tile('海边逆光').dblclick(); await viewerReady()
  assert.equal(await viewer().evaluate((el) => getComputedStyle(el).gridTemplateColumns), '390px')
  await noHorizontalScroll('phone viewer')
  await page.screenshot({ path: `${output}/step4-viewer-phone.png` })
})

await check('viewer-blur-close-recovery', async () => {
  await fresh(); await open(); await tile('海边逆光').dblclick(); await viewerReady()
  await viewer().getByRole('textbox', { name: '标题', exact: true }).fill('保留待确认改名')
  server.loseNext = '/metadata'
  await page.locator('.viewer-back').click({ position: { x: 4, y: 4 } })
  await viewer().getByRole('button', { name: '重试', exact: true }).waitFor()
  await viewer().getByRole('button', { name: '重试', exact: true }).click(); await saved()
  assert.equal(server.writes[0].key, server.writes[1].key)
  await viewer().getByRole('button', { name: '关闭查看' }).click()
  await page.locator('.viewer-back').waitFor({ state: 'detached' })
})

await check('viewer-detail-recovery', async () => {
  await fresh(); await open()
  let release
  const waiting = new Promise((r) => { release = r })
  await page.route('**/creative/assets/f-sea', async (route) => { await waiting; await route.fulfill({ json: server.assets[0] }).catch(() => {}) })
  await tile('海边逆光').dblclick()
  await viewer().getByText('正在读取素材…', { exact: true }).waitFor()
  await viewer().getByRole('button', { name: '下一项' }).click(); await viewerReady()
  release(); await page.waitForTimeout(80)
  assert.equal(await viewer().getByRole('textbox', { name: '标题', exact: true }).inputValue(), '白纱与拱门')
  await page.keyboard.press('Escape')
  await page.unroute('**/creative/assets/f-sea')
  let failed = true
  await page.route('**/creative/assets/f-ref', async (route) => failed ? route.fulfill({ status: 503, json: { error: { code: 'temporary', message: '详情暂时读不到' } } }) : route.fallback())
  await tile('布光参考').dblclick(); await viewer().getByText('详情暂时读不到', { exact: true }).waitFor()
  assert(await viewer().getByRole('textbox', { name: '标题', exact: true }).isDisabled())
  failed = false; await viewer().getByRole('button', { name: '重试', exact: true }).click(); await viewerReady()
  await page.keyboard.press('Escape')
  const note = server.assets.find((a) => a.id === 'f-note')
  note.content.truncated = true; note.content.payload.body = '摘要'
  await page.route('**/creative/content-revisions/r-f-note', (route) => route.fulfill({ json: { ...note.content, truncated: false, payload: { body: '完整正文来自详情修订' } } }))
  await tile('拍摄脚本 v2').dblclick(); await viewerReady()
  assert.equal(await viewer().locator('.v-text p').innerText(), '完整正文来自详情修订')
})

await check('step5-import-text-link-paste',async()=>{
 await fresh();await open('?group=g-out')
 await tree().getByRole('button',{name:'导入',exact:true}).click();await page.getByRole('menuitem',{name:'新建文字'}).click()
 const dialog=page.getByRole('dialog',{name:'新建文字'});await dialog.getByLabel('标题（可选）').fill('新脚本');await dialog.getByLabel('文字内容').fill('完整的脚本正文');await dialog.getByRole('button',{name:'添加',exact:true}).click()
 await until(()=>server.assets.some(a=>a.title==='新脚本'),'text saved');assert.deepEqual(server.assets.find(a=>a.title==='新脚本').group_ids,['g-out'])
 await until(()=>!page.isClosed(),'live');await settled()
 await page.locator('.al-board').focus()
 await page.evaluate(()=>{const data=new DataTransfer();data.setData('text/plain','https://example.com/reference');document.querySelector('.al-board').dispatchEvent(new ClipboardEvent('paste',{clipboardData:data,bubbles:true,cancelable:true}))})
 await until(()=>server.assets.some(a=>a.title==='example.com'),'link paste saved')
 assert.deepEqual(server.assets.find(a=>a.title==='example.com').group_ids,['g-out'])
 const count=server.writes.length;await page.getByRole('searchbox').focus()
 await page.getByRole('searchbox').evaluate(el=>{const data=new DataTransfer();data.setData('text/plain','not an asset');el.dispatchEvent(new ClipboardEvent('paste',{clipboardData:data,bubbles:true,cancelable:true}))})
 await page.waitForTimeout(60);assert.equal(server.writes.length,count)
})
await check('step5-import-files-same-name-and-skips',async()=>{
 await fresh();await open('?group=g-out')
 await page.getByLabel('上传素材文件').setInputFiles([{name:'same.png',mimeType:'image/png',buffer:Buffer.from('first')},{name:'same.png',mimeType:'image/png',buffer:Buffer.from('second')},{name:'bad.exe',mimeType:'application/octet-stream',buffer:Buffer.from('bad')},{name:'huge.png',mimeType:'image/png',buffer:Buffer.alloc(1025)}])
 await until(()=>server.assets.filter(a=>a.title==='same').length===2,'duplicate filenames accepted')
 assert(server.assets.filter(a=>a.title==='same').every(a=>a.group_ids.includes('g-out')))
 const text=await page.locator('.cc-upload-tray').innerText();assert(text.includes('暂不支持此文件格式'));assert(text.includes('超过'))
 await page.screenshot({path:output+'/step5-import.png'})
})
await check('import-duplicates',async()=>{
 const digest=(text)=>createHash('sha256').update(text).digest('hex')
 const file=(name,text)=>({name,mimeType:'image/png',buffer:Buffer.from(text)})
 const uploaded=()=>server.writes.filter(w=>w.path.endsWith('/creative/uploads')).map(w=>w.body.payload.file_name)
 const replaces=()=>server.writes.filter(w=>w.path.endsWith('/import-replace'))
 await fresh();server.digests.set(digest('sea'),'f-sea');server.digests.set(digest('old'),'f-old');await open('?group=g-in')
 const dialog=page.getByRole('dialog')
 // 不重复的照常上传；重复的逐个问，先问资产库里的，再问回收站里的。
 await page.getByLabel('上传素材文件').setInputFiles([file('sea_final.png','sea'),file('fresh.png','fresh'),file('old.png','old')])
 await dialog.getByRole('heading',{name:'资产库里已有相同内容'}).waitFor()
 assert((await dialog.innerText()).includes('「sea_final.png」与资产库里的「海边逆光」内容完全相同'))
 await until(()=>server.assets.some(a=>a.title==='fresh'),'fresh file imported while asking')
 assert.deepEqual(uploaded(),['fresh.png'])
 await page.screenshot({path:output+'/import-duplicates.png'})
 await dialog.getByRole('button',{name:'替换'}).click()
 await dialog.getByRole('heading',{name:'回收站里有相同内容'}).waitFor()
 const sea=server.assets.find(a=>a.id==='f-sea')
 assert.equal(sea.title,'sea_final');assert.deepEqual(sea.group_ids.sort(),['g-in','g-out']);assert.equal(sea.rating,5)
 assert.deepEqual(replaces()[0].body.payload,{expected_revision:'1',title:'sea_final',group_ids:['g-in']})
 await dialog.getByRole('button',{name:'新增'}).click()
 await dialog.waitFor({state:'detached'})
 await until(()=>server.assets.some(a=>a.title==='old'&&!a.deleted_at),'added copy imported')
 assert(server.assets.find(a=>a.id==='f-old').deleted_at,'adding left the trashed one alone')
 assert.deepEqual(uploaded(),['fresh.png','old.png']);assert.equal(replaces().length,1)

 // 其余也这样处理：一次跳过全部，不上传也不替换。
 await page.getByLabel('上传素材文件').setInputFiles([file('a.png','sea'),file('b.png','old')])
 await dialog.getByText('其余 1 个重复文件也这样处理').click()
 await dialog.getByRole('button',{name:'跳过'}).click()
 await dialog.waitFor({state:'detached'})
 assert.deepEqual(uploaded(),['fresh.png','old.png']);assert.equal(replaces().length,1)

 // 替换失败留在弹窗里，重试沿用原操作身份；Esc 等于跳过。
 server.failNext={path:'/import-replace',status:503,code:'temporary',message:'网络暂时不可用'}
 await page.getByLabel('上传素材文件').setInputFiles([file('c.png','sea')])
 await dialog.getByRole('button',{name:'替换'}).click()
 await dialog.getByRole('alert').filter({hasText:'网络暂时不可用'}).waitFor()
 await dialog.getByRole('button',{name:'重试替换'}).click()
 await dialog.waitFor({state:'detached'})
 const [failed,retried]=replaces().slice(-2)
 assert.equal(failed.key,retried.key);assert.equal(server.assets.find(a=>a.id==='f-sea').title,'c')
 await page.getByLabel('上传素材文件').setInputFiles([file('d.png','old')])
 await dialog.waitFor();await page.keyboard.press('Escape');await dialog.waitFor({state:'detached'})
 assert.deepEqual(uploaded(),['fresh.png','old.png'])

 // 同批两个相同文件逐个处理：替换第一个后，第二个已并进同一份素材，不再询问，也不带过期版本重发。
 const before=replaces().length
 await page.getByLabel('上传素材文件').setInputFiles([file('s1.png','sea'),file('s2.png','sea')])
 await dialog.getByText('其余 1 个重复文件也这样处理').waitFor()
 await dialog.getByRole('button',{name:'替换'}).click()
 await dialog.waitFor({state:'detached'})
 await page.getByText('同批 1 个相同文件一并处理').waitFor()
 assert.equal(replaces().length,before+1);assert.equal(server.assets.find(a=>a.id==='f-sea').title,'s1')

 // 素材在别处改过：冲突后读回最新版本，再替换用新请求而不是重放旧请求。
 await page.getByLabel('上传素材文件').setInputFiles([file('s3.png','sea')])
 await dialog.waitFor()
 const sea2=server.assets.find(a=>a.id==='f-sea');sea2.revision=String(Number(sea2.revision)+1);const edited=sea2.revision
 await dialog.getByRole('button',{name:'替换'}).click()
 await dialog.getByRole('alert').filter({hasText:'刚在别处改过'}).waitFor()
 await dialog.getByRole('button',{name:'重试替换'}).click()
 await dialog.waitFor({state:'detached'})
 const [stale,renewed]=replaces().slice(-2)
 assert.notEqual(stale.key,renewed.key);assert.equal(renewed.body.payload.expected_revision,edited)
 assert.equal(server.assets.find(a=>a.id==='f-sea').title,'s3')

 // 查重请求失败不拦导入。
 await page.route('**/creative/assets/digest-matches*',route=>route.fulfill({status:503,json:{error:{code:'temporary',message:'x'}}}))
 await page.getByLabel('上传素材文件').setInputFiles([file('e.png','sea')])
 await until(()=>uploaded().includes('e.png'),'import continues when the check fails')
})
await check('step5-trash-restore-purge',async()=>{
 await fresh();await open('?view=trash')
 await page.getByText('移入回收站 30 天后自动清除',{exact:true}).waitFor()
 assert((await page.locator('.al-trash-left').innerText()).includes('还剩'))
 await tile('旧照片').click();await page.getByRole('complementary',{name:'检查器'}).getByRole('button',{name:'恢复',exact:true}).click()
 await until(()=>!server.assets.find(a=>a.id==='f-old').deleted_at,'restored');await settled()
 server.assets.find(a=>a.id==='f-old').deleted_at=new Date().toISOString();server.assets.find(a=>a.id==='f-old').revision='3'
 await open('?view=trash');await tile('旧照片').click({button:'right'});await page.getByRole('menuitem',{name:'永久删除'}).click()
 await page.getByRole('alertdialog').getByRole('button',{name:'取消',exact:true}).click();assert(server.assets.some(a=>a.id==='f-old'))
 await tile('旧照片').click({button:'right'});await page.getByRole('menuitem',{name:'永久删除'}).click();await page.getByRole('alertdialog').getByRole('button',{name:'确认',exact:true}).click()
 await until(()=>!server.assets.some(a=>a.id==='f-old'),'purged')
})
// V6-075 AC-07：永久删除说清空间何时释放；已标记回收的字节在用量面板单列。
await check('V6-075-AC-07', async () => {
  const notice = '此操作不可撤销。画布里已经用到的副本会保留，空间在它们也不再使用后释放。'
  await fresh()
  server.pendingRelease = Math.round(1.5 * GiB)
  await open('?view=trash')
  const usage = tree().locator('.al-usage')
  assert.match(await usage.innerText(), /其中 1\.5 GB 待释放/)
  await page.screenshot({ path: `${output}/v6-075-ac07-usage.png` })
  await tile('旧照片').click({ button: 'right' })
  await page.getByRole('menuitem', { name: '永久删除' }).click()
  const purge = page.getByRole('alertdialog')
  await purge.getByText(notice, { exact: true }).waitFor()
  await page.screenshot({ path: `${output}/v6-075-ac07-purge.png` })
  await purge.getByRole('button', { name: '取消', exact: true }).click()
  await page.getByRole('button', { name: '清空回收站', exact: true }).click()
  await page.getByRole('alertdialog').getByText(/将永久删除 1 项。此操作不可撤销。画布里已经用到的副本会保留，空间在它们也不再使用后释放。/).waitFor()
  await page.screenshot({ path: `${output}/v6-075-ac07-empty-trash.png` })
})
await check('step5-empty-trash-conflict-and-unknown',async()=>{
 await fresh();await open('?view=trash');await page.getByRole('button',{name:'清空回收站',exact:true}).click()
 await page.getByText(/将永久删除 1 项/).waitFor();server.assets.push(asset({id:'extra-old',title:'另一个旧图',trashed:true}));server.revision='2'
 await page.getByRole('alertdialog').getByRole('button',{name:'永久删除',exact:true}).click()
 await page.getByText('回收站有变化，请再确认一次',{exact:true}).waitFor();await page.getByText(/将永久删除 2 项/).waitFor()
 assert.equal(server.assets.filter(a=>a.deleted_at).length,2)
 server.loseNext='/library/empty-trash';await page.getByRole('alertdialog').getByRole('button',{name:'永久删除',exact:true}).click()
 await page.getByRole('button',{name:'重试原操作',exact:true}).waitFor();await page.getByRole('button',{name:'重试原操作',exact:true}).click()
 await page.getByRole('alertdialog').waitFor({state:'detached'})
 const writes=server.writes.filter(w=>w.path.endsWith('/empty-trash'));assert.equal(writes.length,3);assert.notEqual(writes[0].key,writes[1].key);assert.equal(writes[1].key,writes[2].key)
 assert.equal(server.assets.filter(a=>a.deleted_at).length,0)
})
await check('step5-trash-settings-mobile',async()=>{
 await fresh({width:390,height:844});server.settings.retention_days=7;await open('?view=trash')
 await page.getByText('移入回收站 7 天后自动清除',{exact:true}).waitFor();await noHorizontalScroll('trash phone')
 await page.getByLabel('回收站保留期限').selectOption('');await page.getByText('不会自动清除',{exact:true}).first().waitFor()
 assert.equal(server.settings.retention_days,null)
 await tile('旧照片').click();assert(await page.getByRole('toolbar',{name:'批量操作'}).getByRole('button',{name:'恢复',exact:true}).isEnabled())
 assert(await page.locator('.al-phone-import').isVisible());await page.screenshot({path:output+'/step5-trash-phone.png'})
})

await check('step5-file-drop-paste-quota',async()=>{
 await fresh();await open('?group=g-brand')
 assert.equal(await page.locator('.al-head .al-phone-import').isVisible(),false)
 const data=await page.evaluateHandle(()=>{const d=new DataTransfer();d.items.add(new File(['pixels'],'drop.png',{type:'image/png'}));return d})
 await board().dispatchEvent('dragover',{dataTransfer:data});await page.getByText('松手导入',{exact:true}).waitFor();await page.screenshot({path:output+'/step5-drop.png'})
 await board().dispatchEvent('drop',{dataTransfer:data})
 await until(()=>server.assets.some(a=>a.title==='drop'),'file drop');assert.deepEqual(server.assets.find(a=>a.title==='drop').group_ids,['g-brand'])
 await settled();await page.waitForFunction(()=>!document.querySelector('.cc-upload-tray .cc-upload-bar'))
 server.failNext={path:'/creative/uploads',status:413,code:'creative_media_quota_exceeded',message:'空间不足'}
 await board().evaluate(el=>{const d=new DataTransfer();d.items.add(new File(['pixels'],'clipboard.png',{type:'image/png'}));el.dispatchEvent(new ClipboardEvent('paste',{clipboardData:d,bubbles:true,cancelable:true}))})
 await page.locator('.cc-upload-tray').getByText('空间不足',{exact:true}).waitFor()
 assert(!(server.assets.some(a=>a.title==='clipboard')));assert((await page.locator('.al-toast').innerText()).includes('clipboard.png'))
})
await check('step5-viewer-permanent-delete',async()=>{
 await fresh();await open('?view=trash');await tile('旧照片').dblclick();await until(async()=>await viewer().getByRole('button',{name:'永久删除',exact:true}).isEnabled(),'trash detail loaded')
 await viewer().getByRole('button',{name:'永久删除',exact:true}).click();await page.getByRole('alertdialog').getByRole('button',{name:'确认',exact:true}).click()
 await page.locator('.viewer-back').waitFor({state:'detached'});await until(()=>!server.assets.some(a=>a.id==='f-old'),'viewer purge')
})

await check('step5-r2-rejected-import-keeps-draft',async()=>{
 await fresh();await open();await tree().getByRole('button',{name:'导入',exact:true}).click();await page.getByRole('menuitem',{name:'新建文字'}).click()
 const dialog=page.getByRole('dialog',{name:'新建文字'});await dialog.getByLabel('文字内容').fill('不可丢失的正文');server.failNext={path:'/creative/assets',status:400,code:'validation_failed',message:'请检查内容'}
 await dialog.getByRole('button',{name:'添加',exact:true}).click();await page.getByText('请检查内容',{exact:true}).first().waitFor()
 assert.equal(await dialog.getByLabel('文字内容').inputValue(),'不可丢失的正文');assert(await dialog.getByRole('button',{name:'添加',exact:true}).isEnabled())
 server.loseNext='/creative/assets';await dialog.getByRole('button',{name:'添加',exact:true}).click();await dialog.getByRole('button',{name:'重试原操作',exact:true}).click();await dialog.waitFor({state:'detached'})
 assert.equal(server.assets.filter(a=>a.content.payload.body==='不可丢失的正文').length,1)
 const w=server.writes.filter(w=>w.path.endsWith('/creative/assets'));assert.equal(w[1].key,w[2].key)
})
await check('step5-r2-upload-keeps-unknown-empty-trash',async()=>{
 await fresh();await open();let release;const wait=new Promise(r=>{release=r})
 await page.route('**/creative/uploads/*/complete',async route=>{await wait;await route.fallback()})
 await page.getByLabel('上传素材文件').setInputFiles({name:'delayed.png',mimeType:'image/png',buffer:Buffer.from('pixels')})
 await until(()=>server.uploads.size===1,'upload started');await after(()=>row('回收站').click())
 await page.getByRole('button',{name:'清空回收站',exact:true}).click();await page.getByText(/将永久删除 1 项/).waitFor();server.loseNext='/library/empty-trash'
 await page.getByRole('alertdialog').getByRole('button',{name:'永久删除',exact:true}).click();await page.getByRole('button',{name:'重试原操作',exact:true}).waitFor()
 release();await until(()=>server.assets.some(a=>a.title==='delayed'),'upload completed');await page.waitForTimeout(200)
 assert.equal(params().get('view'),'trash');await page.getByRole('alertdialog').getByRole('button',{name:'重试原操作',exact:true}).click();await page.getByRole('alertdialog').waitFor({state:'detached'})
 const writes=server.writes.filter(w=>w.path.endsWith('/empty-trash'));assert.equal(writes[0].key,writes[1].key)
})

await check('step5-r2-paste-validation-retains-content',async()=>{
 await fresh();await open();const before=server.writes.length
 await board().evaluate(el=>{const d=new DataTransfer();d.setData('text/plain','字'.repeat(100001));el.dispatchEvent(new ClipboardEvent('paste',{clipboardData:d,bubbles:true,cancelable:true}))})
 let dialog=page.getByRole('dialog',{name:'新建文字'});await dialog.getByText('文字内容最多 100000 字，请缩短后再试',{exact:true}).waitFor();assert.equal((await dialog.getByLabel('文字内容').inputValue()).length,100001);assert.equal(server.writes.length,before)
 await dialog.getByRole('button',{name:'取消',exact:true}).click()
 await board().evaluate(el=>{const d=new DataTransfer();d.setData('text/plain','https://name:secret@example.com');el.dispatchEvent(new ClipboardEvent('paste',{clipboardData:d,bubbles:true,cancelable:true}))})
 dialog=page.getByRole('dialog',{name:'添加链接'});await dialog.getByText('网址不能包含账号或密码',{exact:true}).waitFor();assert.equal(await dialog.getByLabel('网址',{exact:true}).inputValue(),'https://name:secret@example.com');assert.equal(server.writes.length,before)
 await dialog.getByRole('button',{name:'取消',exact:true}).click()
 server.failNext={path:'/creative/assets',status:400,code:'validation_failed',message:'正文校验未通过'}
 await board().evaluate(el=>{const d=new DataTransfer();d.setData('text/plain','服务端拒绝也不能丢失');el.dispatchEvent(new ClipboardEvent('paste',{clipboardData:d,bubbles:true,cancelable:true}))})
 dialog=page.getByRole('dialog',{name:'新建文字'});await dialog.getByText('正文校验未通过',{exact:true}).waitFor();assert.equal(await dialog.getByLabel('文字内容').inputValue(),'服务端拒绝也不能丢失')
})

await check('step5-followup-drop-preserves-empty-trash',async()=>{
 await fresh();await open('?view=trash');await page.getByRole('button',{name:'清空回收站',exact:true}).click();await page.getByText(/将永久删除 1 项/).waitFor()
 server.loseNext='/library/empty-trash';await page.getByRole('alertdialog').getByRole('button',{name:'永久删除',exact:true}).click();await page.getByRole('button',{name:'重试原操作',exact:true}).waitFor()
 const dialog=page.getByRole('alertdialog'),overlay=page.locator('.overlay').filter({has:dialog})
 const data=await page.evaluateHandle(()=>{const d=new DataTransfer();d.items.add(new File(['pixels'],'blocked.png',{type:'image/png'}));return d})
 // 遮罩并非 role=alertdialog 的后代，不能只检查 target.closest(dialog)。
 assert.equal(await overlay.evaluate(el=>!!el.closest('[role="dialog"],[role="alertdialog"]')),false)
 for(const target of [overlay,dialog,board()]) {
  await target.dispatchEvent('dragover',{dataTransfer:data})
  await target.dispatchEvent('drop',{dataTransfer:data});await page.waitForTimeout(50)
  assert.equal(params().get('view'),'trash');assert(await dialog.isVisible());assert.equal(server.uploads.size,0);assert.equal(await page.locator('.al-drop').count(),0)
 }
 await page.getByLabel('上传素材文件').setInputFiles({name:'late-chooser.png',mimeType:'image/png',buffer:Buffer.from('pixels')});await page.waitForTimeout(50);assert.equal(server.uploads.size,0)
 await dialog.getByRole('button',{name:'重试原操作',exact:true}).click();await dialog.waitFor({state:'detached'})
 const writes=server.writes.filter(w=>w.path.endsWith('/empty-trash'));assert.equal(writes.length,2);assert.equal(writes[0].key,writes[1].key)
})
await check('step5-followup-import-error-isolation',async()=>{
 await fresh();await open();let release;const wait=new Promise(r=>{release=r})
 await page.route('**/creative/uploads/*/complete',async route=>{await wait;await route.fulfill({status:413,json:{error:{code:'creative_media_quota_exceeded',message:'空间不足'}}})})
 await page.getByLabel('上传素材文件').setInputFiles({name:'background.png',mimeType:'image/png',buffer:Buffer.from('pixels')});await until(()=>server.uploads.size===1,'upload started')
 await tree().getByRole('button',{name:'导入',exact:true}).click();await page.getByRole('menuitem',{name:'添加链接'}).click()
 const dialog=page.getByRole('dialog',{name:'添加链接'});await dialog.getByLabel('网址',{exact:true}).fill('https://example.com/new')
 release();await page.locator('.al-toast').getByText('background.png：空间不足',{exact:true}).waitFor();assert.equal(await dialog.getByRole('alert').count(),0)
 server.failNext={path:'/creative/assets',status:400,code:'validation_failed',message:'链接创建失败'};await dialog.getByRole('button',{name:'添加',exact:true}).click()
 await dialog.getByRole('alert').getByText('链接创建失败',{exact:true}).waitFor();assert.equal(await dialog.getByLabel('网址',{exact:true}).inputValue(),'https://example.com/new')
})
await check('step5-followup-import-visible-view',async()=>{
 await fresh();await open('?view=favorites')
 await board().evaluate(el=>{const d=new DataTransfer();d.setData('text/plain','最爱视图导入的新文字');el.dispatchEvent(new ClipboardEvent('paste',{clipboardData:d,bubbles:true,cancelable:true}))})
 await until(()=>server.assets.some(a=>a.title==='最爱视图导入的新文字'),'import saved');assert.equal(params().get('view'),null);await tile('最爱视图导入的新文字').waitFor()
 await open('?group=g-out');await page.getByLabel('上传素材文件').setInputFiles({name:'keep-group.png',mimeType:'image/png',buffer:Buffer.from('pixels')})
 await until(()=>server.assets.some(a=>a.title==='keep-group'),'file saved');assert.equal(params().get('group'),'g-out');await tile('keep-group').waitFor()
 await open('?view=recent');await board().evaluate(el=>{const d=new DataTransfer();d.setData('text/plain','最近添加视图导入');el.dispatchEvent(new ClipboardEvent('paste',{clipboardData:d,bubbles:true,cancelable:true}))})
 await until(()=>server.assets.some(a=>a.title==='最近添加视图导入'),'recent saved');assert.equal(params().get('view'),'recent');await tile('最近添加视图导入').waitFor()
})

await check('step6-appearance-filters', async () => {
  await fresh({assets:[asset({id:'warm',title:'暖色竖幅',w:100,h:150,palette:[{hex:'#d9a07a',weight:.7},{hex:'#8a6550',weight:.3}]}),asset({id:'gray',title:'黑白方形',w:110,h:100,palette:[{hex:'#a1a1a7',weight:1}]}),asset({id:'wide',title:'横幅图片',w:1920,h:1080,palette:[{hex:'#ffffff',weight:1}]}),asset({id:'video',title:'横幅视频',kind:'video',w:1920,h:1080}),asset({id:'text',title:'文字',kind:'text',body:'正文'})]})
  await open()
  await page.getByRole('button',{name:'颜色',exact:true}).click()
  const pop = page.getByRole('dialog',{name:'颜色筛选'})
  assert.equal(await pop.locator('.al-pal-grid button').count(),12)
  await page.screenshot({path:`${output}/step6-colors.png`})
  if (process.env.CREATIVE_PROTOTYPE_URL) {
    const reference = await context.newPage()
    await reference.goto(process.env.CREATIVE_PROTOTYPE_URL)
    await reference.locator('#fColor').click()
    const values = (el) => {const s=getComputedStyle(el);return {width:s.width,padding:s.padding,font:s.fontSize,gap:s.gap,radius:s.borderRadius,bg:s.backgroundColor,shadow:s.boxShadow}}
    for (const [actual, expected] of [['.al-pal-pop','.pal-pop'],['.al-pal-grid','.pal-grid'],['.al-pal-grid button','.pal-grid button'],['.al-pal-pop p','.pal-pop p']]) {
      assert.deepEqual(await page.locator(actual).first().evaluate(values),await reference.locator(expected).first().evaluate(values),actual)
    }
    await reference.screenshot({path:`${output}/step6-prototype-colors.png`})
    await reference.close()
  }

  await after(()=>pop.getByRole('button',{name:'橙 #d9a07a',exact:true}).click())
  assert.equal(params().get('color'),'#d9a07a');assert.equal(lastRead().get('color'),'#d9a07a');assert.equal(await tiles().count(),1)
  await tile('暖色竖幅').click()
  const inspector=page.getByRole('complementary',{name:'检查器'})
  assert.equal(await inspector.locator('.al-palette button').count(),2)
  const palStyle=await inspector.locator('.al-palette button').first().evaluate(el=>({height:getComputedStyle(el).height,radius:getComputedStyle(el).borderRadius,gap:getComputedStyle(el.parentElement).gap}))
  assert.deepEqual(palStyle,{height:'26px',radius:'6px',gap:'6px'})
  await page.screenshot({path:`${output}/step6-inspector.png`})
  await after(()=>inspector.getByRole('button',{name:'按 #8a6550 筛选',exact:true}).click())
  assert.equal(lastRead().get('color'),'#8a6550')
  await after(()=>page.getByRole('button',{name:'清除颜色',exact:true}).click())
  await page.getByRole('combobox',{name:'画幅',exact:true}).click()
  await after(()=>page.getByRole('option',{name:'方形',exact:true}).click())
  assert.equal(params().get('shape'),'square');assert.equal(await tiles().count(),1);await tile('黑白方形').waitFor()
  await after(()=>page.getByRole('button',{name:'清除画幅',exact:true}).click())
  await page.getByRole('combobox',{name:'画幅',exact:true}).click()
  await after(()=>page.getByRole('option',{name:'横幅',exact:true}).click())
  await tile('横幅图片').waitFor();assert.equal(await tiles().count(),1);assert.equal(await tile('横幅视频').count(),0)
  await after(()=>page.getByRole('button',{name:'清除全部',exact:true}).click())
  assert.equal(await tiles().count(),5)
})
await check('step6-appearance-mobile-and-url',async()=>{
  await fresh({width:390,height:844})
  await open('?color=%23d9a07a&shape=portrait')
  assert.equal(lastRead().get('shape'),'portrait');assert.equal(lastRead().get('color'),'#d9a07a')
  await noHorizontalScroll('appearance phone')
  await page.getByRole('button',{name:'颜色',exact:true}).click()
  const pop=page.getByRole('dialog',{name:'颜色筛选'})
  const box=await pop.boundingBox();assert(box.x>=0&&box.x+box.width<=390)
  await page.screenshot({path:`${output}/step6-phone.png`})
  await page.keyboard.press('Escape');assert.equal(await pop.count(),0)
  await after(()=>page.getByRole('button',{name:'清除全部',exact:true}).click())
  assert.equal(params().get('color'),null);assert.equal(params().get('shape'),null)
})

await check('step6-audit-inspector-video-and-duplicate-colors',async()=>{
 await fresh({assets:[asset({id:'video-rotated',title:'旋转视频',kind:'video',w:1920,h:1080}),asset({id:'duplicate',title:'同色聚类',palette:[{hex:'#a1a1a7',weight:.6},{hex:'#a1a1a7',weight:.4}]})]})
 const warnings=[]
 page.on('console',msg=>{if(msg.type()==='error'||msg.type()==='warning')warnings.push(msg.text())})
 await open();await tile('同色聚类').click()
 const inspector=page.getByRole('complementary',{name:'检查器'})
 assert.equal(await inspector.locator('.al-palette button').count(),2)
 assert(!warnings.some(w=>w.includes('same key')),warnings.join('\n'))
 await tile('旋转视频').click()
 await inspector.getByText('编码尺寸',{exact:true}).waitFor()
 const props=await inspector.locator('.al-props').innerText()
 assert(!props.includes('横幅')&&!props.includes('竖幅')&&!props.includes('方形'),props)
})
await check('tag-tree-manage', async () => {
  await fresh(); await open()
  const tags = tree().locator('section[aria-labelledby="alTags"]')
  const cat = (name) => tags.locator('.al-tag-cat', { has: page.locator('.al-name', { hasText: new RegExp(`^${name}$`) }) })
  // 新建分类，再在分类下新建标签；重名留在输入框提示。
  await tree().getByRole('button', { name: '新建标签或分类', exact: true }).click()
  await page.getByRole('menuitem', { name: '新建分类', exact: true }).click()
  await tree().getByRole('textbox', { name: '新分类名称', exact: true }).fill('场景')
  await tree().getByRole('textbox', { name: '新分类名称', exact: true }).press('Enter'); await saved()
  await cat('场景').waitFor()
  await cat('场景').click({ button: 'right' }); await page.getByRole('menuitem', { name: '新建标签', exact: true }).click()
  const input = tree().getByRole('textbox', { name: '新标签名称', exact: true })
  await input.fill('暖调'); await input.press('Enter')
  await tree().getByRole('alert').getByText('已有「暖调」，换个名称').waitFor()
  await input.fill('海边'); await input.press('Enter'); await saved()
  const sea = server.tags.find((t) => t.name === '海边')
  assert.equal(sea.category_id, server.categories.find((c) => c.name === '场景').id)
  await row('海边').waitFor()
  // 改名、改颜色、移到分类。
  await row('海边').click({ button: 'right' }); await page.getByRole('menuitem', { name: '重命名', exact: true }).click()
  await tree().getByRole('textbox', { name: '标签名称', exact: true }).fill('海景'); await tree().getByRole('textbox', { name: '标签名称', exact: true }).press('Enter'); await saved()
  assert.equal(server.tags.find((t) => t.id === sea.id).name, '海景')
  await row('海景').click({ button: 'right' }); await page.getByRole('menuitem', { name: '颜色…', exact: true }).click()
  await page.getByRole('dialog', { name: '标签颜色' }).getByRole('button', { name: '蓝', exact: true }).click(); await saved()
  assert.equal(server.tags.find((t) => t.id === sea.id).color, '#5f7ea8')
  await row('海景').click({ button: 'right' }); await page.getByRole('menuitem', { name: '移到分类…', exact: true }).click()
  await page.getByRole('dialog', { name: '移到分类' }).getByRole('button', { name: '光线', exact: true }).click(); await saved()
  assert.equal(server.tags.find((t) => t.id === sea.id).category_id, 'c-light')
  // 拖到分类上也能移动。
  await row('待整理').dragTo(cat('风格')); await saved()
  assert.equal(server.tags.find((t) => t.id === 't-misc').category_id, 'c-style')
  // 删除被引用的标签：确认框说明影响的素材数，取消不删，确认后从素材上移除并退出筛选。
  await row('暖调').click()
  await until(async () => params().get('tags') === 't-warm', 'tag filter')
  await row('暖调').click({ button: 'right' })
  await page.getByRole('menuitem', { name: /删除标签/ }).click()
  const dialog = page.getByRole('alertdialog')
  await dialog.getByText('2 项素材（含回收站）会去掉这个标签，素材本身保留。删除后无法撤销。').waitFor()
  await page.screenshot({ path: `${output}/tag-delete-confirm.png` })
  await dialog.getByRole('button', { name: '取消', exact: true }).click()
  assert(server.tags.some((t) => t.id === 't-warm'))
  await row('暖调').click({ button: 'right' }); await page.getByRole('menuitem', { name: /删除标签/ }).click()
  await dialog.getByRole('button', { name: '删除标签', exact: true }).click(); await saved()
  assert(!server.tags.some((t) => t.id === 't-warm'))
  assert(server.assets.every((a) => !a.tag_ids.includes('t-warm')))
  await until(async () => !params().get('tags'), 'filter cleared')
  // 删除分类：标签归入未分类。
  await cat('光线').click({ button: 'right' }); await page.getByRole('menuitem', { name: '删除分类', exact: true }).click()
  await dialog.getByText('其中 2 个标签会保留，归入「未分类」；素材上的标签不受影响。').waitFor()
  await dialog.getByRole('button', { name: '删除分类', exact: true }).click(); await saved()
  assert.equal(server.tags.find((t) => t.id === 't-back').category_id, null)
  await cat('未分类').waitFor()
  await page.screenshot({ path: `${output}/tag-tree.png` })
})

// C6 AC-6：模型卡片只显示缩略图，资产库页在打开模型前不加载 3D 引擎；查看浮窗按资产的材质显示、转着看、切换白模与材质，信息与清单一致，下载得到 .glb。
const pillarManifest = { bounds: { min: [-0.375, 0, -0.375], max: [0.375, 3, 0.375] }, triangles: 152, slots: [{ id: 'stone', hint: 'stone' }, { id: 'trim', hint: 'stone' }], parts: ['base', 'capital', 'shaft'], anchors: [{ id: 'top', position: [0, 3, 0] }] }
const modelAsset = () => {
  const a = asset({ id: 'm-pillar', title: '哥特束柱', age: 0 })
  a.kind = 'model'
  a.palette = []
  a.content = {
    ...a.content,
    kind: 'model',
    payload: { model: { format: 'glb', name_hint: '哥特束柱', manifest: pillarManifest, source_revision_id: 'ccrv_scene',
      materials: { stone: { preset: 'paint@1', params: { color: '#c03030', roughness: 0.6, metalness: 0 } } },
      generator: { language: 'three-module', contract_version: 1, lib_version: 1, code_sha256: 'sha256-' + 'a'.repeat(64), mount: 'floor',
        params_schema: { height: { type: 'number', default: 3, min: 1, max: 5, unit: '米', label: '高度' } }, params: { height: 3 } } } },
    media: [{ role: 'original', blob_id: 'b-glb', mime: 'model/gltf-binary', byte_size: glb.length, width: null, height: null, duration_ms: null }, { role: 'thumbnail', blob_id: 'b-png', mime: 'image/png', byte_size: 900, width: 256, height: 256, duration_ms: null }],
  }
  return a
}
await check('C6-model-preview', async () => {
  await fresh({ assets: [modelAsset(), ...fixture()] })
  const engine = []
  page.on('request', (r) => {
    if (/\/deps\/(three|@react-three)|creative-scene\/preview\//.test(r.url())) engine.push(r.url())
  })
  await open()
  await after(() => page.getByRole('group', { name: '类型' }).getByRole('button', { name: /^模型/ }).click())
  assert.equal(lastRead().get('kind'), 'model')
  assert.equal(await tiles().count(), 1)
  assert.equal((await tile('哥特束柱').locator('.cl-video-badge').innerText()).trim(), '3D')
  await tile('哥特束柱').locator('.cc-media img').waitFor()
  assert.deepEqual(engine, [], 'browsing model cards loads no 3D engine')

  await tile('哥特束柱').dblclick()
  await viewerReady()
  const stage = viewer().locator('.v-model canvas')
  await stage.waitFor()
  await until(async () => (await viewer().locator('.v-model-still').count()) === 0, 'model loaded', 10000)
  await page.waitForTimeout(300)
  assert(engine.length > 0, 'opening a model loads the 3D engine')
  for (const [label, value] of [['尺寸', '0.75 × 3.00 × 0.75 米'], ['参考点', '底部中心'], ['面数', '152'], ['材质槽', 'stone（stone）：漆面、trim（stone）'], ['高度', '3 米']])
    assert.equal(await viewer().locator('dt', { hasText: new RegExp(`^${label}$`) }).locator('xpath=following-sibling::dd[1]').innerText(), value, label)
  assert.equal(await viewer().getByRole('button', { name: '放入项目…' }).count(), 0, 'a model has no project placement')

  const shot = () => stage.screenshot()
  // The share of pixels in the asset's red paint: the stone slot in 「材质」, none in clay.
  const red = (png) => page.evaluate(async (src) => {
    const i = new Image()
    await new Promise((resolve) => { i.onload = resolve; i.src = src })
    const c = document.createElement('canvas')
    c.width = i.width
    c.height = i.height
    const g = c.getContext('2d')
    g.drawImage(i, 0, 0)
    const p = g.getImageData(0, 0, c.width, c.height).data
    let n = 0
    for (let k = 0; k < p.length; k += 4) if (p[k] > p[k + 1] + 60 && p[k] > p[k + 2] + 60) n++
    return n / (p.length / 4)
  }, `data:image/png;base64,${png.toString('base64')}`)
  assert.equal(await viewer().getByRole('button', { name: '材质', exact: true }).getAttribute('aria-pressed'), 'true', 'it opens in the asset\'s materials, like its card')
  await viewer().getByText('放进场景后可以换材质', { exact: true }).waitFor()
  const front = await shot()
  assert((await red(front)) > 0.01, 'the stone slot shows the paint the asset was saved with')
  const box = await stage.boundingBox()
  await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2)
  await page.mouse.down()
  await page.mouse.move(box.x + box.width / 2 + 160, box.y + box.height / 2, { steps: 8 })
  await page.mouse.up()
  await page.waitForTimeout(300)
  const turned = await shot()
  assert(!front.equals(turned), 'dragging turns the model')
  await viewer().getByRole('button', { name: '白模', exact: true }).click()
  await page.waitForTimeout(500)
  const clay = await shot()
  assert(!turned.equals(clay), 'clay looks different from the materials')
  assert((await red(clay)) < 0.001, 'clay has no paint')
  await page.screenshot({ path: `${output}/model-preview.png` })

  const ticket = page.waitForRequest((r) => r.url().endsWith('/media-access-tickets') && r.postDataJSON()?.purpose === 'download')
  const download = page.waitForEvent('download')
  await viewer().getByRole('button', { name: '下载', exact: true }).click()
  assert.equal((await download).suggestedFilename(), '哥特束柱.glb')
  assert.equal((await ticket).postDataJSON().role, 'original')
})

await context?.close()
await browser.close()
console.log(JSON.stringify(results, null, 1))
if (errors.length) console.error('page errors:', errors)
assert(Object.values(results).every((r) => r === 'pass') && !errors.length)
