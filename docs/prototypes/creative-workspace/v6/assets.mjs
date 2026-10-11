// 资产库独立页原型（Eagle 方向）。数据只在内存里，刷新即回到示例。
import { ic, esc, hydrateIcons, openMenu, openPopover, closeFloating, toast, confirmModal } from './ui.mjs'
import { openViewer, starsHTML, bindStars } from './viewer.mjs'
import { mountSide } from './side.mjs'


const $ = (id) => document.getElementById(id)
const photo = (n) => `../v4/assets/photo-${String(n).padStart(2, '0')}.jpg`
const RATIO = { 1: 1.5, 2: 0.667, 3: 1.5, 4: 0.667, 5: 1.5, 6: 1.47, 7: 1.333, 8: 1.5, 9: 0.667, 10: 1.5, 11: 0.75, 12: 1.333 }

// ——分组、标签、智能分组——
const groups = [
  { id: 'g1', name: '婚纱外景参考', parent: null, open: true },
  { id: 'g11', name: '海边', parent: 'g1' },
  { id: 'g12', name: '拱门与建筑', parent: 'g1' },
  { id: 'g2', name: '布光参考', parent: null },
  { id: 'g3', name: '品牌 Lookbook · 2026 春', parent: null, open: false },
  { id: 'g31', name: '色板', parent: 'g3' },
  { id: 'g4', name: '姿势库', parent: null },
  { id: 'g5', name: '文案与脚本', parent: null },
]
const cats = [
  { id: 'c1', name: '风格' },
  { id: 'c2', name: '场景' },
  { id: 'c3', name: '光线' },
  { id: 'c4', name: '用途' },
]
const tags = [
  { id: 't1', name: '黑白', cat: 'c1', color: '#a1a1a7' },
  { id: 't2', name: '低饱和', cat: 'c1', color: '#b8a99a' },
  { id: 't3', name: '胶片感', cat: 'c1', color: '#c9a27e' },
  { id: 't4', name: '海边', cat: 'c2', color: '#7da4b8' },
  { id: 't5', name: '建筑', cat: 'c2', color: '#9a9a9a' },
  { id: 't6', name: '花卉', cat: 'c2', color: '#c98a96' },
  { id: 't7', name: '街头', cat: 'c2', color: '#8c8c8c' },
  { id: 't8', name: '自然光', cat: 'c3', color: '#d9c38a' },
  { id: 't9', name: '硬光', cat: 'c3', color: '#e0e0e0' },
  { id: 't10', name: '暗调', cat: 'c3', color: '#5d5d63' },
  { id: 't11', name: '客户参考', cat: 'c4', color: '#9cc0a0' },
  { id: 't12', name: '样片', cat: 'c4', color: '#d7a06c' },
]
const smarts = [
  { id: 's1', name: '暖调海边', rules: { color: '#d9a07a', tags: ['t4'] } },
  { id: 's2', name: '竖幅黑白人像', rules: { shape: 'portrait', tags: ['t1'] } },
  { id: 's3', name: '还没打标签', rules: { untagged: true } },
  { id: 's4', name: '精选 · 4.5 星以上', rules: { minRating: 4.5 } },
]

// ——素材——
let seq = 100
const A = (o) => ({ id: `a${seq++}`, note: '', groups: [], tags: [], fav: false, added: 1, ...o })
const assets = [
  A({ kind: 'image', title: '光落在她停留的地方', p: 1, groups: ['g12', 'g2'], tags: ['t1', 't5', 't9'], fav: true, added: 0, size: '2.4 MB', dims: '4000 × 6000', note: '硬光打在墙面形成的切线，可以借给拱门那组。' }),
  A({ kind: 'image', title: '把风景框进画面', p: 2, groups: ['g12'], tags: ['t5', 't8'], added: 0, size: '1.8 MB', dims: '6000 × 4000' }),
  A({ kind: 'image', title: '一束光，就够了', p: 3, groups: ['g2'], tags: ['t10', 't9', 't12'], fav: true, added: 1, size: '2.1 MB', dims: '4000 × 6000', note: '伦勃朗光，主灯 45° 偏高，反光板补暗部。' }),
  A({ kind: 'image', title: '海岸边的紫色诗句', p: 4, groups: ['g11', 'g31'], tags: ['t4', 't6'], added: 1, size: '3.0 MB', dims: '6000 × 4000' }),
  A({ kind: 'image', title: '白裙与拱形的对话', p: 5, groups: ['g12', 'g4'], tags: ['t1', 't5'], added: 2, size: '2.2 MB', dims: '4000 × 6000' }),
  A({ kind: 'image', title: '柔软的白，安静的黑', p: 6, groups: ['g4', 'g3'], tags: ['t1', 't11'], added: 3, size: '1.9 MB', dims: '4080 × 6000' }),
  A({ kind: 'image', title: '把夏天留在花影里', p: 7, groups: ['g11'], tags: ['t4', 't6', 't8'], fav: true, added: 4, size: '2.6 MB', dims: '4500 × 6000' }),
  A({ kind: 'image', title: '城市里的光影练习', p: 8, groups: ['g4'], tags: ['t7', 't9'], added: 5, size: '2.3 MB', dims: '4000 × 6000' }),
  A({ kind: 'image', title: '弧线，和一点玫瑰色', p: 9, groups: ['g31'], tags: ['t5', 't2'], added: 6, size: '2.7 MB', dims: '6000 × 4000' }),
  A({ kind: 'image', title: '等一阵海风', p: 10, groups: ['g11'], tags: ['t4', 't8', 't2'], added: 8, size: '2.0 MB', dims: '4000 × 6000' }),
  A({ kind: 'image', title: '蓝色没有标准答案', p: 11, groups: [], tags: [], added: 9, size: '3.1 MB', dims: '6000 × 4500' }),
  A({ kind: 'image', title: '去海边找一片留白', p: 12, groups: ['g11', 'g1'], tags: ['t4', 't5', 't3'], added: 12, size: '2.5 MB', dims: '4500 × 6000' }),
  A({ kind: 'video', title: '海边走位 · 试拍', p: 4, groups: ['g11', 'g4'], tags: ['t4', 't11'], added: 2, size: '48 MB', dims: '1920 × 1080', duration: '0:18' }),
  A({ kind: 'video', title: '拱门下的转身', p: 9, groups: ['g12'], tags: ['t5'], added: 7, size: '62 MB', dims: '1920 × 1080', duration: '0:24' }),
  A({ kind: 'text', title: '婚纱外景 · 拍摄脚本 v2', groups: ['g5', 'g1'], tags: ['t11'], added: 1, text: '17:20 到场，先拍拱门下的静态；17:50 转到海边，逆光走位三组；18:10 日落前十分钟只拍背影和剪影。备用：起风时改拍花丛前的近景。' }),
  A({ kind: 'text', title: '品牌口径', groups: ['g5', 'g3'], tags: [], added: 4, text: '安静、克制、不摆拍。画面里只留一个视觉重心，色彩不超过三种，人物不看镜头。' }),
  A({ kind: 'text', title: '客户偏好 · 林小姐', groups: ['g5'], tags: ['t11'], added: 10, text: '喜欢低饱和和留白，不要正面大笑；对侧脸比较满意。婚纱是缎面，硬光下容易反光。' }),
  A({ kind: 'link', title: 'Tadao Ando · 光之教堂', url: 'archdaily.com', p: 2, groups: ['g12'], tags: ['t5'], added: 3 }),
  A({ kind: 'link', title: '日落时间与方位 · 青岛', url: 'suncalc.org', groups: ['g1'], tags: [], added: 5 }),
  A({ kind: 'audio', title: '海浪环境声', groups: ['g11'], tags: ['t4'], added: 6, duration: '1:42', size: '3.2 MB' }),
  A({ kind: 'audio', title: '片头配乐 · 钢琴', groups: ['g3'], tags: [], added: 11, duration: '0:58', size: '2.1 MB' }),
  A({ kind: 'image', title: '旧样片 · 逆光过曝', p: 10, trashed: 23, tags: ['t8'], added: 20, size: '2.0 MB', dims: '4000 × 6000' }),
  A({ kind: 'text', title: '废弃的脚本 v1', trashed: 6, added: 30, text: '第一版脚本，时间线太紧，已改为 v2。' }),
]
// 评分（半星为最小单位）与生成来源：生成后存入的素材带着自己的提示词和参考
const RATINGS = { '光落在她停留的地方': 5, '一束光，就够了': 4.5, '白裙与拱形的对话': 4, '把夏天留在花影里': 4.5, '柔软的白，安静的黑': 3.5, '等一阵海风': 3, '去海边找一片留白': 4, '海边走位': 2.5, '婚纱外景 · 拍摄脚本 v2': 4 }
for (const a of assets) a.rating = RATINGS[a.title.split(' · ')[0]] ?? RATINGS[a.title] ?? 0
Object.assign(assets.find((a) => a.title === '把夏天留在花影里'), { gen: { prompt: '海边花丛前的新娘侧影，傍晚逆光，低饱和，前景是粉色夹竹桃，人物只占画面三分之一，留出天空', model: 'GPT Image', ratio: '3:4', res: '2K', refs: [10, 4] } })
Object.assign(assets.find((a) => a.title === '等一阵海风'), { gen: { prompt: '同上，换成竖幅 4:5，人物居中偏下，海面保留细浪', model: 'GPT Image', ratio: '4:5', res: '2K', refs: [7] } })

// 生成记录：生成结果自动出现在这里，存入资产库才算资产（参考 LibTV「生成历史」）
const generations = [
  { id: 'r1', p: 7, day: '今天', prompt: '海边花丛前的新娘侧影，傍晚逆光，低饱和，前景是粉色夹竹桃，人物只占画面三分之一，留出天空', model: 'GPT Image', saved: false, refs: [10, 4] },
  { id: 'r2', p: 10, day: '今天', prompt: '同上，换成竖幅 4:5，人物居中偏下', model: 'GPT Image', saved: true, refs: [] },
  { id: 'r3', p: 4, day: '今天', prompt: '海岸边紫色花卉，前景虚化', model: 'GPT Image', saved: false, refs: [] },
  { id: 'r4', p: 1, day: '10月9日', prompt: '黑白建筑光影，硬光切线落在人物肩部', model: 'GPT Image', saved: false, refs: [] },
  { id: 'r5', p: 5, day: '10月9日', prompt: '白裙人物站在拱门中，留白多', model: 'GPT Image', saved: true, refs: [] },
  { id: 'r6', p: 12, day: '10月8日', prompt: '海边白色建筑，自然光，画面右侧留白', model: 'GPT Image', saved: false, refs: [] },
]
const projects = ['九月，向海而行', '雪山婚纱 · 二稿', '品牌 Lookbook · 春季', '毕业照 · 光影练习']

// ——状态——
const st = {
  view: 'all', group: null, smart: null,
  kind: 'all', color: null, shape: null, tagIds: [], tagMode: 'all', q: '', minRating: null,
  sort: '最近添加', layout: 'masonry', w: 220,
  sel: new Set(), anchor: null,
}

// ——主色提取：缩到 32px，按 4 位量化取前 5 个差异足够大的色——
const palettes = new Map()
function extract(src) {
  if (palettes.has(src)) return Promise.resolve(palettes.get(src))
  return new Promise((resolve) => {
    const img = new Image()
    img.onload = () => {
      const c = document.createElement('canvas')
      c.width = c.height = 32
      const g = c.getContext('2d', { willReadFrequently: true })
      g.drawImage(img, 0, 0, 32, 32)
      const d = g.getImageData(0, 0, 32, 32).data
      const buckets = new Map()
      for (let i = 0; i < d.length; i += 4) {
        const k = ((d[i] >> 4) << 8) | ((d[i + 1] >> 4) << 4) | (d[i + 2] >> 4)
        const b = buckets.get(k) ?? { n: 0, r: 0, g: 0, b: 0 }
        b.n++; b.r += d[i]; b.g += d[i + 1]; b.b += d[i + 2]
        buckets.set(k, b)
      }
      const sorted = [...buckets.values()].sort((a, b) => b.n - a.n).map((b) => [b.r / b.n, b.g / b.n, b.b / b.n])
      const out = []
      for (const c1 of sorted) {
        if (out.every((c2) => dist(c1, c2) > 48)) out.push(c1)
        if (out.length === 5) break
      }
      const hex = out.map(toHex)
      palettes.set(src, hex)
      resolve(hex)
    }
    img.onerror = () => resolve([])
    img.src = src
  })
}
const dist = (a, b) => Math.hypot(a[0] - b[0], a[1] - b[1], a[2] - b[2])
const toHex = (c) => `#${c.map((v) => Math.round(v).toString(16).padStart(2, '0')).join('')}`
const fromHex = (h) => [1, 3, 5].map((i) => parseInt(h.slice(i, i + 2), 16))
const SWATCHES = ['#d9a07a', '#c9655d', '#d9c38a', '#8fa37a', '#6f9fa8', '#5f7ea8', '#9b86b0', '#c98a96', '#f1efe9', '#a1a1a7', '#5d5d63', '#1d1d1f']
const srcOf = (a) => (a.src ?? (a.p ? photo(a.p) : null))
// 有彩色按色相 + 明度比，中性色只和中性色比，免得暖色把黑白照片也找出来
function hsl([r, g, b]) {
  r /= 255; g /= 255; b /= 255
  const max = Math.max(r, g, b), min = Math.min(r, g, b), l = (max + min) / 2, d = max - min
  if (!d) return [0, 0, l]
  const h = max === r ? ((g - b) / d) % 6 : max === g ? (b - r) / d + 2 : (r - g) / d + 4
  return [(h * 60 + 360) % 360, d, l] // 第二项用色度，接近纯白纯黑时不会虚高
}
function near(c, swatch) {
  const [h1, c1, l1] = hsl(fromHex(c)), [h2, c2, l2] = hsl(fromHex(swatch))
  if (c2 < 0.08) return c1 < 0.08 && Math.abs(l1 - l2) < 0.16
  const dh = Math.min(Math.abs(h1 - h2), 360 - Math.abs(h1 - h2))
  return c1 >= 0.1 && dh < 24 && Math.abs(l1 - l2) < 0.3
}
const matchesColor = (a, hex) => (palettes.get(srcOf(a)) ?? []).some((c) => near(c, hex))
const shapeOf = (a) => {
  const r = a.ratio ?? RATIO[a.p]
  if (!r || a.kind === 'text' || a.kind === 'audio') return null
  if (a.kind === 'video' || a.kind === 'link') return 'landscape'
  return r > 1.1 ? 'portrait' : r < 0.9 ? 'landscape' : 'square'
}
const SHAPES = { landscape: '横幅', portrait: '竖幅', square: '方形' }
const KINDS = [['all', '全部'], ['image', '图片'], ['video', '视频'], ['text', '文字'], ['link', '链接'], ['audio', '音频']]
const KIND_ICON = { image: 'image', video: 'video', text: 'file-text', link: 'link', audio: 'audio-lines' }

// ——查询——
const live = () => assets.filter((a) => !a.trashed)
function descendants(gid) {
  const out = [gid]
  for (const g of groups) if (g.parent === gid) out.push(...descendants(g.id))
  return out
}
function scope() {
  if (st.view === 'trash') return assets.filter((a) => a.trashed)
  if (st.group) {
    const ids = descendants(st.group)
    return live().filter((a) => a.groups.some((g) => ids.includes(g)))
  }
  if (st.view === 'unsorted') return live().filter((a) => !a.groups.length)
  if (st.view === 'fav') return live().filter((a) => a.fav)
  if (st.view === 'recent') return live().filter((a) => a.added <= 7)
  return live()
}
function results() {
  const q = st.q.trim().toLowerCase()
  const rules = st.smart ? smarts.find((s) => s.id === st.smart).rules : {}
  const color = st.color ?? rules.color
  const shape = st.shape ?? rules.shape
  const tagIds = [...new Set([...st.tagIds, ...(rules.tags ?? [])])]
  const list = scope().filter((a) => {
    if (st.kind !== 'all' && a.kind !== st.kind) return false
    if (rules.untagged && a.tags.length) return false
    const minRating = st.minRating ?? rules.minRating
    if (minRating === 'none' ? a.rating > 0 : minRating && a.rating < minRating) return false
    if (color && !matchesColor(a, color)) return false
    if (shape && shapeOf(a) !== shape) return false
    if (tagIds.length) {
      const hit = tagIds.filter((t) => a.tags.includes(t)).length
      if (st.tagMode === 'all' ? hit < tagIds.length : hit === 0) return false
    }
    if (q) {
      const hay = [a.title, a.note, a.text ?? '', ...a.tags.map((t) => tagName(t))].join(' ').toLowerCase()
      if (!hay.includes(q)) return false
    }
    return true
  })
  const order = { 最近添加: (x, y) => x.added - y.added, 最早添加: (x, y) => y.added - x.added, 评分: (x, y) => y.rating - x.rating || x.added - y.added, 名称: (x, y) => x.title.localeCompare(y.title, 'zh-CN') }[st.sort]
  return list.sort(order)
}
const tagName = (id) => tags.find((t) => t.id === id)?.name ?? ''
const groupName = (id) => groups.find((g) => g.id === id)?.name ?? ''
const countIn = (gid) => { const ids = descendants(gid); return live().filter((a) => a.groups.some((g) => ids.includes(g))).length }

// ——渲染：树——
function renderTree() {
  const views = [
    ['all', 'images', '全部', live().length],
    ['unsorted', 'box', '未归类', live().filter((a) => !a.groups.length).length],
    ['fav', 'heart', '最爱', live().filter((a) => a.fav).length],
    ['recent', 'clock', '最近添加', live().filter((a) => a.added <= 7).length],
    ['gen', 'sparkles', '生成记录', generations.length],
    ['trash', 'trash-2', '回收站', assets.filter((a) => a.trashed).length],
  ]
  $('views').innerHTML = views
    .map(([id, icon, name, n]) => `<button class="row" data-view="${id}" aria-current="${!st.group && !st.smart && st.view === id}">${ic(icon)}<span class="name">${name}</span><span class="n">${n}</span></button>`)
    .join('')
  $('smart').innerHTML = smarts
    .map((s) => `<button class="row" data-smart="${s.id}" aria-current="${st.smart === s.id}">${ic('bookmark')}<span class="name">${esc(s.name)}</span><span class="n">${countSmart(s)}</span></button>`)
    .join('')
  const branch = (parent, depth) =>
    groups
      .filter((g) => g.parent === parent)
      .map((g) => {
        const kids = groups.some((x) => x.parent === g.id)
        return `<button class="row" role="treeitem" data-group="${g.id}" aria-current="${st.group === g.id}" style="padding-left:${8 + depth * 16}px" ${kids ? `aria-expanded="${!!g.open}"` : ''}>
          <span class="i twist ${kids ? (g.open ? 'open' : '') : 'none'}" data-twist="${g.id}">${ic('chevron-right')}</span>${ic('folder')}<span class="name">${esc(g.name)}</span><span class="n">${countIn(g.id)}</span></button>${kids && g.open ? branch(g.id, depth + 1) : ''}`
      })
      .join('')
  $('groups').innerHTML = branch(null, 0)
  $('tags').innerHTML = cats
    .map((c) => `<div class="tag-cat">${esc(c.name)}</div>${tags
      .filter((t) => t.cat === c.id)
      .map((t) => `<button class="row" data-tag="${t.id}" aria-current="${st.tagIds.includes(t.id)}"><span class="tag-dot" style="background:${t.color}"></span><span class="name">${esc(t.name)}</span><span class="n">${live().filter((a) => a.tags.includes(t.id)).length}</span></button>`)
      .join('')}`)
    .join('')
  const used = 1.84
  $('usage').innerHTML = `${live().length} 项 · 已用 ${used} GB / 20 GB<div class="meter"><i style="width:${(used / 20) * 100}%"></i></div>`
}
function countSmart(s) {
  const saved = { ...st }
  Object.assign(st, { view: 'all', group: null, smart: s.id, kind: 'all', color: null, shape: null, tagIds: [], q: '', minRating: null })
  const n = results().length
  Object.assign(st, saved)
  return n
}

// ——渲染：主区——
function renderHead() {
  let title = ''
  if (st.view === 'gen') title = '生成记录'
  else if (st.smart) title = esc(smarts.find((s) => s.id === st.smart).name)
  else if (st.group) {
    const path = []
    for (let g = groups.find((x) => x.id === st.group); g; g = groups.find((x) => x.id === g.parent)) path.unshift(g)
    title = path.map((g, i) => (i < path.length - 1 ? `<a href="#" data-group="${g.id}">${esc(g.name)}</a><span class="sep">/</span>` : esc(g.name))).join('')
  } else title = { all: '全部', unsorted: '未归类', fav: '最爱', recent: '最近添加', trash: '回收站' }[st.view]
  const n = st.view === 'gen' ? generations.length : results().length
  $('crumb').innerHTML = `<span>${title}</span><small>${n} 项</small>`
  $('trashNote').hidden = st.view !== 'trash' || !n
  const counts = Object.fromEntries(KINDS.map(([k]) => [k, k === 'all' ? scope().length : scope().filter((a) => a.kind === k).length]))
  $('kinds').innerHTML = KINDS.filter(([k]) => k === 'all' || counts[k] || st.kind === k).map(([k, label]) => `<button data-kind="${k}" aria-pressed="${st.kind === k}">${label}<small>${counts[k]}</small></button>`).join('')
  $('filters').hidden = st.view === 'gen'
  $('fColor').classList.toggle('on', !!st.color)
  $('fColorDot').style.background = st.color ?? ''
  $('fShape').classList.toggle('on', !!st.shape)
  $('fShape').querySelector('.v').textContent = st.shape ? SHAPES[st.shape] : '画幅'
  $('fRating').classList.toggle('on', !!st.minRating)
  $('fRating').querySelector('.v').textContent = ratingLabel(st.minRating) ?? '评分'
  $('fTags').classList.toggle('on', st.tagIds.length > 0)
  $('fTags').querySelector('.v').textContent = st.tagIds.length ? `标签 · ${st.tagIds.length}` : '标签'
  $('sort').querySelector('.v').textContent = st.sort
  // 已生效的条件
  const pills = []
  if (st.smart) {
    const r = smarts.find((s) => s.id === st.smart).rules
    pills.push(`<span class="pill">智能分组：${[r.color ? '颜色' : '', r.shape ? SHAPES[r.shape] : '', ...(r.tags ?? []).map(tagName), r.untagged ? '无标签' : '', ratingLabel(r.minRating) ?? ''].filter(Boolean).join(' + ')}</span>`)
  }
  if (st.color) pills.push(`<span class="pill"><b style="background:${st.color}"></b>颜色相近<button data-clear="color" aria-label="清除颜色">${ic('x')}</button></span>`)
  if (st.shape) pills.push(`<span class="pill">${SHAPES[st.shape]}<button data-clear="shape" aria-label="清除画幅">${ic('x')}</button></span>`)
  if (st.minRating) pills.push(`<span class="pill">${ratingLabel(st.minRating)}<button data-clear="minRating" aria-label="清除评分">${ic('x')}</button></span>`)
  for (const t of st.tagIds) pills.push(`<span class="pill">${esc(tagName(t))}<button data-clear-tag="${t}" aria-label="移除标签">${ic('x')}</button></span>`)
  if (st.tagIds.length > 1) pills.push(`<span class="pill">${st.tagMode === 'all' ? '同时满足' : '满足任一'}</span>`)
  if (st.q) pills.push(`<span class="pill">“${esc(st.q)}”<button data-clear="q" aria-label="清除搜索">${ic('x')}</button></span>`)
  if (st.color || st.shape || st.tagIds.length || st.q || st.minRating) pills.push(`<span class="pill clear" data-clear="all">清除全部</span>`)
  pills.push(`<span class="grow"></span>`)
  if ((st.color || st.shape || st.tagIds.length || st.minRating) && !st.smart) pills.push(`<span class="pill clear" id="saveSmart">${ic('bookmark')} 存为智能分组</span>`)
  $('active').innerHTML = pills.length > 1 && st.view !== 'gen' ? pills.join('') : ''
  // 子分组
  const kids = st.group && !st.q ? groups.filter((g) => g.parent === st.group) : []
  $('subgroups').innerHTML = kids
    .map((g) => {
      const ids = descendants(g.id)
      const pics = live().filter((a) => a.p && a.groups.some((x) => ids.includes(x))).slice(0, 3)
      return `<button class="subgroup" data-group="${g.id}"><span class="mini">${pics.map((a) => `<i style="background-image:url(${photo(a.p)})"></i>`).join('')}</span>${esc(g.name)}<small>${countIn(g.id)}</small></button>`
    })
    .join('')
}

function thumb(a) {
  if (a.kind === 'text') return `<div class="thumb card-text"><strong>${esc(a.title)}</strong><p>${esc(a.text)}</p></div>`
  if (a.kind === 'audio') {
    const bars = Array.from({ length: 36 }, (_, i) => 8 + Math.abs(Math.sin(i * 1.7 + a.title.length)) * 28 + ((i * 7) % 5))
    return `<div class="thumb card-audio"><div class="wave">${bars.map((h) => `<i style="height:${h}px"></i>`).join('')}</div><div class="row2"><span class="play">${ic('play')}</span>${esc(a.title)}<span class="grow"></span>${a.duration}</div></div>`
  }
  if (a.kind === 'link')
    return `<div class="thumb card-link">${a.p ? `<div class="og" style="background-image:url(${photo(a.p)})"></div>` : ''}<div class="meta"><b>${esc(a.title)}</b><small><i></i>${esc(a.url)}</small></div></div>`
  return `<div class="thumb"><img src="${srcOf(a)}" alt="" loading="lazy" draggable="false" ${a.ratio || RATIO[a.p] ? `style="aspect-ratio:${1 / (a.ratio ?? RATIO[a.p])}"` : ''}>${a.kind === 'video' ? `<span class="kind-badge">${ic('play')}${a.duration}</span>` : ''}${a.trashed ? `<span class="trash-left">还剩 ${a.trashed} 天</span>` : ''}</div>`
}
// 卡片下方只放标题和评分：尺寸、类型这类信息在检查器里看。文字、链接、音频卡片里已经写着标题，不再重复。
function caption(a) {
  const stars = a.rating ? starsHTML(a.rating, { size: 'sm', readOnly: true }) : ''
  if (a.kind === 'image' || a.kind === 'video') return `<div class="cap"><span>${esc(a.title)}</span>${stars}</div>`
  return stars ? `<div class="cap"><span></span>${stars}</div>` : ''
}
function renderTiles() {
  const box = $('tiles')
  box.className = `tiles${st.layout === 'grid' ? ' grid' : ''}`
  box.style.setProperty('--w', `${st.w}px`)
  if (st.view === 'gen') return renderGenerations()
  const list = results()
  box.innerHTML = list
    .map((a) => `<div class="tile" data-id="${a.id}" aria-selected="${st.sel.has(a.id)}" draggable="${!a.trashed}" tabindex="-1">
      ${thumb(a)}
      <button class="tick" data-tick aria-label="选择">${ic('check')}</button>
      ${a.trashed ? '' : `<button class="fav${a.fav ? ' on' : ''}" data-fav aria-label="${a.fav ? '取消最爱' : '加入最爱'}">${ic('heart')}</button>`}
      <button class="open" data-open aria-label="放大查看">${ic('maximize')}</button>
      ${caption(a)}
    </div>`)
    .join('')
  const empty = $('empty')
  empty.hidden = list.length > 0
  if (!list.length) {
    const filtered = st.color || st.shape || st.tagIds.length || st.q || st.kind !== 'all'
    empty.innerHTML = filtered
      ? `<span class="i" data-i="search"></span><strong>没有符合条件的素材</strong><span>放宽条件，或者到「全部」里找找。</span><div class="row-btns"><button class="btn" data-clear="all">清除条件</button>${st.group || st.view !== 'all' ? '<button class="btn" data-widen>在全部里查找</button>' : ''}</div>`
      : st.view === 'trash'
        ? '<strong>回收站是空的</strong><span>删除的素材会在这里保留 30 天。</span>'
        : `<strong>这里还没有素材</strong><span>把文件拖进来，或者从别的分组拖到左侧的「${esc(st.group ? groupName(st.group) : '分组')}」上。</span><div class="row-btns"><button class="btn btn-primary" data-import>导入</button></div>`
    hydrateIcons(empty)
  }
}
function renderGenerations() {
  const days = [...new Set(generations.map((g) => g.day))]
  $('tiles').innerHTML = days
    .map((d) => `<div class="date-head">${d}</div>${generations
      .filter((g) => g.day === d)
      .map((g) => `<div class="tile" data-gen="${g.id}" aria-selected="${st.sel.has(g.id)}"><div class="thumb"><img src="${photo(g.p)}" alt="" draggable="false" style="aspect-ratio:${1 / RATIO[g.p]}"><span class="kind-badge">${g.saved ? `${ic('check')}已在资产库` : `${ic('sparkles')}${esc(g.model)}`}</span></div>
        <button class="open" data-open aria-label="放大查看">${ic('maximize')}</button><div class="cap"><span>${esc(g.prompt)}</span></div></div>`)
      .join('')}`)
    .join('')
  $('empty').hidden = true
}

// ——检查器——
function renderInspector() {
  const el = $('inspector')
  if (st.view === 'gen') {
    const g = generations.find((x) => st.sel.has(x.id))
    if (!g) {
      el.innerHTML = `<div class="ins-empty"><strong>生成记录</strong><p>每次生成的结果都会自动出现在这里，不占资产库，也不会自己分组。</p><p>觉得有用的，点「存入资产库」再整理。</p></div>`
      return
    }
    el.innerHTML = `<div class="ins-pad">
      <div class="ins-preview"><img src="${photo(g.p)}" alt=""></div>
      <section class="ins-sec"><header>提示词</header><p style="margin:0;font-size:12px;line-height:1.7;color:var(--fg-2)">${esc(g.prompt)}</p></section>
      <dl class="props"><dt>模型</dt><dd>${esc(g.model)}</dd><dt>生成于</dt><dd>${g.day}</dd><dt>来自</dt><dd>九月，向海而行</dd></dl>
      <div class="ins-actions"><button class="btn btn-primary wide" data-save-gen="${g.id}" ${g.saved ? 'disabled' : ''}>${g.saved ? '已存入资产库' : '存入资产库…'}</button><button class="btn" data-dl>${ic('download')}下载</button><button class="btn" data-reuse>${ic('copy')}复制提示词</button></div>
    </div>`
    return
  }
  const sel = assets.filter((a) => st.sel.has(a.id))
  if (!sel.length) {
    const list = results()
    el.innerHTML = `<div class="ins-empty"><strong>${list.length} 项素材</strong><p>点选查看详情，⌘ / Shift 点击或在空白处拖拽框选多个。</p><p><kbd>空格</kbd> 放大查看　<kbd>1–5</kbd> 评分　<kbd>⌘A</kbd> 全选　<kbd>⌫</kbd> 移到回收站</p><p>把素材拖到左侧分组上即可归入，一份素材可以同时在多个分组。</p></div>`
    hydrateIcons(el)
    return
  }
  if (sel.length > 1) {
    const all = (id) => sel.every((a) => a.tags.includes(id))
    const anyTags = tags.filter((t) => sel.some((a) => a.tags.includes(t.id)))
    const anyGroups = groups.filter((g) => sel.some((a) => a.groups.includes(g.id)))
    const pics = sel.filter((a) => srcOf(a)).slice(0, 3)
    el.innerHTML = `<div class="ins-pad">
      <div class="stack">${pics.map((a) => `<i style="background-image:url(${srcOf(a)})"></i>`).join('')}</div>
      <strong style="font-size:15px">已选 ${sel.length} 项</strong>
      <section class="ins-sec"><header>评分<span>${new Set(sel.map((a) => a.rating)).size > 1 ? '各不相同，点选后统一' : ''}</span></header>${starsHTML(new Set(sel.map((a) => a.rating)).size > 1 ? 0 : sel[0].rating, { label: '批量评分' })}</section>
      <section class="ins-sec"><header>分组</header><div class="chips">${anyGroups.map((g) => `<span class="tagchip${sel.every((a) => a.groups.includes(g.id)) ? '' : ' partial'}">${esc(g.name)}<button data-ungroup="${g.id}" aria-label="移出分组">${ic('x')}</button></span>`).join('')}<button class="addchip" data-add-group>${ic('plus')}归入分组</button></div></section>
      <section class="ins-sec"><header>标签<span>半透明表示只有部分素材有</span></header><div class="chips">${anyTags.map((t) => `<span class="tagchip${all(t.id) ? '' : ' partial'}"><span class="tag-dot" style="background:${t.color}"></span>${esc(t.name)}<button data-untag="${t.id}" aria-label="移除标签">${ic('x')}</button></span>`).join('')}<button class="addchip" data-add-tag>${ic('plus')}标签</button></div></section>
      ${insActions(sel)}
    </div>`
    return
  }
  const a = sel[0]
  const pal = palettes.get(srcOf(a)) ?? []
  el.innerHTML = `<div class="ins-pad">
    <div class="ins-preview">${a.kind === 'image' || a.kind === 'video' ? `<img src="${srcOf(a)}" alt="">` : thumb(a)}</div>
    <div style="display:grid;gap:8px">
      <input class="ins-title" id="insTitle" value="${esc(a.title)}" aria-label="标题" ${a.trashed ? 'disabled' : ''}>
      <div class="ins-rating">${starsHTML(a.rating, { label: '评分' })}</div>
      <textarea class="ins-note" id="insNote" placeholder="备注：为什么收藏它、准备用在哪…" ${a.trashed ? 'disabled' : ''}>${esc(a.note)}</textarea>
    </div>
    ${pal.length ? `<section class="ins-sec"><header>主色<span>点一下找相近颜色</span></header><div class="palette">${pal.map((c) => `<button style="background:${c}" data-pal="${c}" aria-label="按 ${c} 筛选" title="${c}"></button>`).join('')}</div></section>` : ''}
    <section class="ins-sec"><header>分组</header><div class="chips">${a.groups.map((g) => `<span class="tagchip">${esc(groupName(g))}<button data-ungroup="${g}" aria-label="移出分组">${ic('x')}</button></span>`).join('')}<button class="addchip" data-add-group>${ic('plus')}归入分组</button></div></section>
    <section class="ins-sec"><header>标签</header><div class="chips">${a.tags.map((id) => { const t = tags.find((x) => x.id === id); return `<span class="tagchip"><span class="tag-dot" style="background:${t.color}"></span>${esc(t.name)}<button data-untag="${id}" aria-label="移除标签">${ic('x')}</button></span>` }).join('')}<button class="addchip" data-add-tag>${ic('plus')}标签</button></div></section>
    <dl class="props">
      <dt>类型</dt><dd>${{ image: '图片', video: `视频 · ${a.duration}`, text: '文字', link: '链接', audio: `音频 · ${a.duration}` }[a.kind]}</dd>
      ${a.dims ? `<dt>尺寸</dt><dd>${a.dims}${shapeOf(a) ? ` · ${SHAPES[shapeOf(a)]}` : ''}</dd>` : ''}
      ${a.size ? `<dt>大小</dt><dd>${a.size}</dd>` : ''}
      ${a.url ? `<dt>网址</dt><dd><a href="#">${esc(a.url)}</a></dd>` : ''}
      <dt>添加于</dt><dd>${a.added === 0 ? '今天' : `${a.added} 天前`}</dd>
    </dl>
    ${insActions([a])}
  </div>`
}
function insActions(sel) {
  if (sel.every((a) => a.trashed))
    return `<div class="ins-actions"><button class="btn btn-primary" data-restore>${ic('undo-2')}恢复</button><button class="btn" data-purge style="color:var(--danger)">${ic('trash-2')}永久删除</button></div>`
  const fav = sel.every((a) => a.fav)
  return `<div class="ins-actions">
    <button class="btn btn-primary wide" data-to-project>${ic('arrow-up-right')}放入项目…</button>
    <button class="btn" data-fav-all>${ic('heart')}${fav ? '取消最爱' : '加入最爱'}</button>
    <button class="btn" data-dl>${ic('download')}下载</button>
    <button class="btn wide" data-trash>${ic('trash-2')}移到回收站</button></div>`
}

function renderBatch() {
  const n = st.sel.size
  const bar = $('batch')
  bar.hidden = n < 2 || st.view === 'gen'
  if (bar.hidden) return
  const trash = st.view === 'trash'
  bar.innerHTML = `<strong>已选 ${n} 项</strong>${trash
    ? `<button class="chip" data-restore>${ic('undo-2')}恢复</button><button class="chip danger" data-purge>${ic('trash-2')}永久删除</button>`
    : `<button class="chip" data-add-group>${ic('folder')}归入分组</button><button class="chip" data-add-tag>${ic('tag')}打标签</button><button class="chip" data-rate-all>${ic('star')}评分</button><button class="chip" data-fav-all>${ic('heart')}最爱</button><button class="chip" data-to-project>${ic('arrow-up-right')}放入项目</button><button class="chip danger" data-trash>${ic('trash-2')}回收站</button>`}<button class="icon-btn x" data-deselect aria-label="取消选择">${ic('x')}</button>`
}

function render() {
  renderTree()
  renderHead()
  renderTiles()
  renderInspector()
  renderBatch()
}

// ——导航——
// 换视图时筛选条件跟着清空；要跨分组找东西用搜索或智能分组
function go(next) {
  Object.assign(st, { view: 'all', group: null, smart: null, kind: 'all', color: null, shape: null, tagIds: [], q: '', minRating: null, ...next })
  $('q').value = st.q
  st.sel.clear()
  $('scroll').scrollTop = 0
  render()
}

// ——素材操作——
const selected = () => assets.filter((a) => st.sel.has(a.id))
function moveToTrash(list) {
  if (!list.length) return
  list.forEach((a) => { a.trashed = 30 })
  st.sel.clear()
  render()
  toast(`已移到回收站 ${list.length} 项`, { label: '撤销', run: () => { list.forEach((a) => { delete a.trashed }); render() } })
}
function restore(list) {
  list.forEach((a) => { delete a.trashed })
  st.sel.clear()
  render()
  toast(`已恢复 ${list.length} 项`)
}
async function purge(list) {
  if (!(await confirmModal({ title: `永久删除 ${list.length} 项？`, body: '删除后无法恢复。已经放进画布的内容不受影响，画布里的节点会保留。', ok: '永久删除' }))) return
  list.forEach((a) => assets.splice(assets.indexOf(a), 1))
  st.sel.clear()
  render()
}
function addToGroup(list, gid) {
  const fresh = list.filter((a) => !a.groups.includes(gid))
  fresh.forEach((a) => a.groups.push(gid))
  render()
  toast(fresh.length ? `已归入「${groupName(gid)}」${fresh.length} 项` : `都已在「${groupName(gid)}」里`)
}
function groupMenu(btn, list) {
  const items = []
  const walk = (parent, depth) => groups.filter((g) => g.parent === parent).forEach((g) => {
    items.push({ label: `${'　'.repeat(depth)}${g.name}`, icon: 'folder', checked: list.every((a) => a.groups.includes(g.id)), run: () => addToGroup(list, g.id) })
    walk(g.id, depth + 1)
  })
  walk(null, 0)
  items.push('sep', { label: '新建分组并归入…', icon: 'plus', run: () => newGroup(null, list) })
  openMenu(items, { rect: btn.getBoundingClientRect(), align: 'end' }, btn)
}
function tagPicker(btn, list, mode = 'assign') {
  const render2 = (filter = '') => {
    const f = filter.trim()
    const body = cats
      .map((c) => {
        const ts = tags.filter((t) => t.cat === c.id && t.name.includes(f))
        if (!ts.length) return ''
        return `<div class="cat">${esc(c.name)}</div><div class="chips">${ts
          .map((t) => {
            const on = mode === 'filter' ? st.tagIds.includes(t.id) : list.every((a) => a.tags.includes(t.id))
            return `<button data-pick="${t.id}" aria-pressed="${on}"><span class="tag-dot" style="background:${t.color}"></span>${esc(t.name)}</button>`
          })
          .join('')}</div>`
      })
      .join('')
    const create = f && !tags.some((t) => t.name === f) && mode === 'assign' ? `<div class="chips" style="margin-top:10px"><button data-create="${esc(f)}">${ic('plus')}新建标签「${esc(f)}」</button></div>` : ''
    return body + create
  }
  const head = mode === 'filter'
    ? `<p>按标签筛选</p><div class="mode"><span>多个标签时</span><span class="seg"><button data-mode="all" aria-pressed="${st.tagMode === 'all'}">同时满足</button><button data-mode="any" aria-pressed="${st.tagMode === 'any'}">满足任一</button></span></div>`
    : `<p>给 ${list.length} 项素材打标签</p>`
  const pop = openPopover(`<div class="tag-pop">${head}<input placeholder="搜索或新建标签" id="tagFind"><div id="tagBody">${render2()}</div></div>`, { rect: btn.getBoundingClientRect(), align: 'end' }, btn)
  const input = pop.querySelector('#tagFind')
  input.focus()
  input.addEventListener('input', () => { pop.querySelector('#tagBody').innerHTML = render2(input.value) })
  pop.addEventListener('click', (e) => {
    const m = e.target.closest('[data-mode]')
    if (m) {
      st.tagMode = m.dataset.mode
      pop.querySelectorAll('[data-mode]').forEach((x) => x.setAttribute('aria-pressed', String(x === m)))
      render()
      return
    }
    const c = e.target.closest('[data-create]')
    if (c) {
      const t = { id: `t${seq++}`, name: c.dataset.create, cat: 'c4', color: '#a1a1a7' }
      tags.push(t)
      list.forEach((a) => a.tags.includes(t.id) || a.tags.push(t.id))
      input.value = ''
      pop.querySelector('#tagBody').innerHTML = render2()
      render()
      return
    }
    const b = e.target.closest('[data-pick]')
    if (!b) return
    const id = b.dataset.pick
    if (mode === 'filter') {
      st.tagIds = st.tagIds.includes(id) ? st.tagIds.filter((x) => x !== id) : [...st.tagIds, id]
      st.sel.clear()
    } else {
      const on = list.every((a) => a.tags.includes(id))
      list.forEach((a) => { a.tags = on ? a.tags.filter((x) => x !== id) : [...new Set([...a.tags, id])] })
    }
    pop.querySelector('#tagBody').innerHTML = render2(input.value)
    render()
  })
}
function newGroup(parent, list = []) {
  const name = parent ? '新建子分组' : '新建分组'
  const g = { id: `g${seq++}`, name, parent, open: true }
  groups.push(g)
  if (parent) groups.find((x) => x.id === parent).open = true
  list.forEach((a) => a.groups.push(g.id))
  renderTree()
  renameGroup(g, true)
}
function renameGroup(g, fresh) {
  const row = $('groups').querySelector(`[data-group="${g.id}"] .name`)
  if (!row) return
  const input = document.createElement('input')
  input.className = 'ins-title'
  input.style.cssText = 'font-size:13px;font-weight:400;margin:0;padding:2px 6px'
  input.value = g.name
  row.replaceWith(input)
  input.focus()
  input.select()
  let cancel = false
  input.addEventListener('click', (e) => e.stopPropagation())
  input.addEventListener('keydown', (e) => {
    e.stopPropagation()
    if (e.isComposing) return
    if (e.key === 'Escape') cancel = true
    if (e.key === 'Enter' || e.key === 'Escape') input.blur()
  })
  input.addEventListener('blur', () => {
    const v = input.value.trim()
    const clash = groups.some((x) => x !== g && x.parent === g.parent && x.name === v)
    if (cancel && fresh) groups.splice(groups.indexOf(g), 1)
    else if (!cancel && v && !clash) g.name = v
    if (clash) toast(`同一层已经有「${v}」`)
    render()
  })
}
function groupContext(g, at) {
  const depth = g.parent ? 1 : 0
  openMenu([
    { label: '重命名', icon: 'pencil', run: () => renameGroup(g) },
    { label: '新建子分组', icon: 'plus', disabled: depth >= 1, sub: depth >= 1 ? '最多两层' : undefined, run: () => newGroup(g.id) },
    'sep',
    { label: '删除分组', icon: 'trash-2', danger: true, sub: '素材仍在库里，子分组升到上一级', run: () => deleteGroup(g) },
  ], at)
}
function deleteGroup(g) {
  const snapshot = { groups: structuredClone(groups), memb: assets.map((a) => [...a.groups]) }
  groups.filter((x) => x.parent === g.id).forEach((x) => { x.parent = g.parent })
  groups.splice(groups.indexOf(g), 1)
  assets.forEach((a) => { a.groups = a.groups.filter((x) => x !== g.id) })
  if (st.group === g.id) st.group = null
  render()
  toast(`已删除分组「${g.name}」，素材都还在`, { label: '撤销', run: () => { groups.splice(0, groups.length, ...snapshot.groups); assets.forEach((a, i) => { a.groups = snapshot.memb[i] ?? a.groups }); render() } })
}
function toProject(btn, n = st.sel.size) {
  openMenu([{ section: '放到哪个项目的画布上' }, ...projects.map((p) => ({ label: p, icon: 'folder', run: () => toast(`已把 ${n} 项放入「${p}」，在画布右侧横排摆开`) })), 'sep', { label: '新建项目并放入', icon: 'plus', run: () => toast(`已新建项目并放入 ${n} 项`) }], { rect: btn.getBoundingClientRect(), align: 'end' }, btn)
}

// ——导入——
function importFiles(files) {
  let added = 0, dup = 0
  for (const f of files) {
    const title = f.name.replace(/\.[^.]+$/, '')
    if (assets.some((a) => !a.trashed && a.title === title)) { dup++; continue }
    const kind = f.type.startsWith('image/') ? 'image' : f.type.startsWith('video/') ? 'video' : f.type.startsWith('audio/') ? 'audio' : 'text'
    const a = A({ kind, title, src: kind === 'image' ? URL.createObjectURL(f) : undefined, groups: st.group ? [st.group] : [], added: 0, size: `${(f.size / 1048576).toFixed(1)} MB`, text: kind === 'text' ? '（从文件导入的文字）' : undefined, duration: kind === 'audio' || kind === 'video' ? '0:00' : undefined })
    if (kind === 'image') {
      const img = new Image()
      img.onload = () => { a.ratio = img.naturalHeight / img.naturalWidth; a.dims = `${img.naturalWidth} × ${img.naturalHeight}`; extract(a.src).then(render) }
      img.src = a.src
    }
    assets.unshift(a)
    added++
  }
  if (st.view === 'trash' || st.view === 'gen') go({ view: 'all' })
  else render()
  toast(`已导入 ${added} 项${st.group ? `到「${groupName(st.group)}」` : ''}${dup ? `，${dup} 项已在库里，跳过` : ''}`)
}
function importMenu(btn) {
  openMenu([
    { label: '上传文件', icon: 'upload', hint: '或直接拖进来', run: () => $('file').click() },
    { label: '添加链接', icon: 'link', hint: '⌘V 粘贴网址', run: () => addLink() },
    { label: '新建文字', icon: 'file-text', run: () => addText() },
  ], { rect: btn.getBoundingClientRect() }, btn)
}
function addLink(url) {
  const u = url ?? 'pinterest.com/pin/1234'
  const a = A({ kind: 'link', title: '新链接 · 正在读取标题…', url: u.replace(/^https?:\/\//, ''), groups: st.group ? [st.group] : [], added: 0 })
  assets.unshift(a)
  st.sel = new Set([a.id])
  render()
  setTimeout(() => { a.title = '参考 · 海边婚纱构图'; a.p = 7; render() }, 900)
}
function addText(text = '') {
  const a = A({ kind: 'text', title: '未命名文字', text: text || '写点什么…', groups: st.group ? [st.group] : [], added: 0 })
  assets.unshift(a)
  st.sel = new Set([a.id])
  render()
  $('insTitle')?.select()
}

// ——评分——
const ratingLabel = (r) => (r === 'none' ? '未评分' : r === 5 ? '5 星' : r ? `${r} 星以上` : null)
function rate(list, v) {
  const old = list.map((a) => a.rating)
  list.forEach((a) => { a.rating = v })
  render()
  viewer?.refresh()
  toast(v ? `${list.length > 1 ? `${list.length} 项` : ''}评为 ${v} 星` : '已清除评分', { label: '撤销', run: () => { list.forEach((a, i) => { a.rating = old[i] }); render(); viewer?.refresh() } })
}

function ratePop(at, list, owner) {
  const pop = openPopover(`<div class="rate-pop"><p>给 ${list.length} 项评分</p>${starsHTML(list.length === 1 ? list[0].rating : 0, { size: 'lg', label: '评分' })}<small>点星星左半边是半颗，数字键 1–5 打整星，0 清除</small></div>`, at, owner)
  bindStars(pop, (_, v) => { closeFloating(); rate(list, v) })
}

// ——查看浮窗——
let viewer = null
const KIND_NAME = { image: '图片', video: '视频', text: '文字', link: '链接', audio: '音频' }
function genSection(g) {
  return `<section class="v-sec"><header>生成信息<button class="icon-btn sm" data-copy aria-label="复制提示词">${ic('copy')}</button></header>
    <p class="v-prompt">${esc(g.prompt)}</p>${g.prompt.length > 60 ? '<button class="v-more" data-more>展开</button>' : ''}
    <div class="v-meta"><span>${ic('sparkles')}${esc(g.model)}</span><span>${g.ratio ?? '3:4'}</span><span>${g.res ?? '2K'}</span></div>
    ${g.refs?.length ? `<header style="margin-top:6px">参考</header><div class="v-refs">${g.refs.map((n) => `<button style="background-image:url(${photo(n)})" aria-label="参考图" data-ref></button>`).join('')}</div>` : ''}</section>`
}
function openAssetViewer(id) {
  const ids = results().map((a) => a.id)
  const at = ids.indexOf(id)
  if (at < 0) return
  const get = (i) => assets.find((a) => a.id === ids[i])
  viewer = openViewer({
    count: ids.length,
    index: at,
    stage: (i) => { const a = get(i); return { kind: a.kind, src: srcOf(a), title: a.title, text: a.text, url: a.url, duration: a.duration } },
    panel: (i) => {
      const a = get(i)
      st.sel = new Set([a.id])
      const chips = (list, name, attr) => list.map((x) => `<span class="tagchip">${name(x)}<button ${attr}="${x}" aria-label="移除">${ic('x')}</button></span>`).join('')
      return `<div class="v-body">
        <span class="v-kind">${KIND_NAME[a.kind]}${a.dims ? ` · ${a.dims}` : ''}${a.duration ? ` · ${a.duration}` : ''}${a.trashed ? ' · 在回收站' : ''}</span>
        <input class="v-title" data-field="title" value="${esc(a.title)}" aria-label="标题" ${a.trashed ? 'disabled' : ''}>
        ${starsHTML(a.rating, { size: 'lg', label: '评分' })}
        <dl class="v-rows"><dt>添加于</dt><dd>${a.added === 0 ? '今天' : `${a.added} 天前`}</dd>${a.size ? `<dt>大小</dt><dd>${a.size}</dd>` : ''}${a.url ? `<dt>网址</dt><dd>${esc(a.url)}</dd>` : ''}</dl>
        ${a.gen ? genSection(a.gen) : ''}
        <section class="v-sec"><header>分组</header><div class="chips">${chips(a.groups, (g) => esc(groupName(g)), 'data-ungroup')}<button class="addchip" data-add-group>${ic('plus')}归入分组</button></div></section>
        <section class="v-sec"><header>标签</header><div class="chips">${chips(a.tags, (t) => `<span class="tag-dot" style="background:${tags.find((x) => x.id === t).color}"></span>${esc(tagName(t))}`, 'data-untag')}<button class="addchip" data-add-tag>${ic('plus')}标签</button></div></section>
        <section class="v-sec"><header>备注</header><textarea class="ins-note" data-field="note" placeholder="为什么收藏它、准备用在哪…">${esc(a.note)}</textarea></section>
      </div>
      <div class="v-foot">${a.trashed
        ? `<button class="btn btn-primary" data-restore>${ic('undo-2')}恢复</button><button class="btn danger" data-purge>${ic('trash-2')}永久删除</button>`
        : `<button class="btn btn-primary wide" data-to-project>${ic('arrow-up-right')}放入项目…</button><button class="btn" data-dl>${ic('download')}下载</button><button class="btn danger" data-trash>${ic('trash-2')}移到回收站</button>`}</div>`
    },
    onPanel: (e, i, v) => {
      const a = get(i)
      if (e.type === 'change') {
        const f = e.target.dataset.field
        if (f === 'title' && e.target.value.trim()) a.title = e.target.value.trim()
        if (f === 'note') a.note = e.target.value
        render()
        return
      }
      const t = e.target.closest('button')
      if (!t) return
      if ('copy' in t.dataset) return toast('已复制提示词')
      if ('more' in t.dataset) { const p = t.previousElementSibling; p.classList.toggle('open'); t.textContent = p.classList.contains('open') ? '收起' : '展开'; return }
      if ('ref' in t.dataset) return toast('参考图在资产库里的位置（原型未展开）')
      if ('trash' in t.dataset) { a.trashed = 30; render(); v.refresh(); return toast('已移到回收站', { label: '撤销', run: () => { delete a.trashed; render(); v.refresh() } }) }
      if ('restore' in t.dataset) { delete a.trashed; render(); return v.refresh() }
      act(e)
      setTimeout(() => v.refresh())
    },
    onKey: (e) => { if (/^[0-5]$/.test(e.key)) rate(selected(), Number(e.key)) },
    onClose: () => { viewer = null; render() },
  })
  bindStars(document.querySelector('.v-panel'), (_, val) => rate(selected(), val))
}
function openGenViewer(id) {
  const at = generations.findIndex((g) => g.id === id)
  viewer = openViewer({
    count: generations.length,
    index: at,
    stage: (i) => ({ kind: 'image', src: photo(generations[i].p), title: generations[i].prompt }),
    panel: (i) => {
      const g = generations[i]
      st.sel = new Set([g.id])
      return `<div class="v-body"><span class="v-kind">生成图片</span>
        <dl class="v-rows"><dt>创建于</dt><dd>${g.day} 10:55</dd><dt>来自</dt><dd>九月，向海而行</dd><dt>状态</dt><dd>${g.saved ? '已存入资产库' : '只在生成记录里'}</dd></dl>
        ${genSection(g)}</div>
        <div class="v-foot"><button class="btn btn-primary" data-save-gen="${g.id}" ${g.saved ? 'disabled' : ''}>${ic('library')}${g.saved ? '已在资产库' : '存入资产库'}</button><button class="btn" data-gen-to-project>${ic('arrow-up-right')}放入项目…</button><button class="btn" data-dl>${ic('download')}下载</button><button class="btn danger" data-drop-gen>${ic('trash-2')}删除记录</button></div>`
    },
    onPanel: (e, i, v) => {
      const t = e.target.closest('button')
      if (!t || e.type !== 'click') return
      if ('copy' in t.dataset) return toast('已复制提示词')
      if ('more' in t.dataset) { const p = t.previousElementSibling; p.classList.toggle('open'); t.textContent = p.classList.contains('open') ? '收起' : '展开'; return }
      if ('genToProject' in t.dataset) return toProject(t, 1)
      if ('dropGen' in t.dataset) return toast('原型里不删除记录')
      act(e)
      setTimeout(() => v.refresh(), 0)
    },
    onClose: () => { viewer = null; render() },
  })
}

// ——事件：树——
document.querySelector('.tree').addEventListener('click', (e) => {
  const twist = e.target.closest('[data-twist]')
  if (twist) { const g = groups.find((x) => x.id === twist.dataset.twist); g.open = !g.open; renderTree(); return }
  const v = e.target.closest('[data-view]')
  if (v) return go({ view: v.dataset.view })
  const s = e.target.closest('[data-smart]')
  if (s) return go({ smart: s.dataset.smart })
  const g = e.target.closest('[data-group]')
  if (g) return go({ group: g.dataset.group })
  const t = e.target.closest('[data-tag]')
  if (t) {
    const id = t.dataset.tag
    st.tagIds = st.tagIds.includes(id) ? st.tagIds.filter((x) => x !== id) : [...st.tagIds, id]
    if (st.view === 'gen' || st.view === 'trash') st.view = 'all'
    st.sel.clear()
    render()
  }
})
$('groups').addEventListener('contextmenu', (e) => {
  const row = e.target.closest('[data-group]')
  if (!row) return
  e.preventDefault()
  groupContext(groups.find((g) => g.id === row.dataset.group), { x: e.clientX, y: e.clientY })
})
$('newGroup').addEventListener('click', () => newGroup(null))
$('newSmart').addEventListener('click', (e) => saveSmart(e.currentTarget))
$('manageTags').addEventListener('click', () => toast('标签管理：改名、换颜色、移动分类、合并（原型未展开）'))
$('import').addEventListener('click', (e) => importMenu(e.currentTarget))
function saveSmart(btn) {
  const rules = { color: st.color ?? undefined, shape: st.shape ?? undefined, tags: st.tagIds.length ? [...st.tagIds] : undefined }
  const parts = [rules.color ? `颜色接近 <b style="display:inline-block;width:10px;height:10px;border-radius:50%;background:${rules.color}"></b>` : '', rules.shape ? `画幅 <b>${SHAPES[rules.shape]}</b>` : '', ...(rules.tags ?? []).map((t) => `标签 <b>${esc(tagName(t))}</b>`)].filter(Boolean)
  const pop = openPopover(`<div class="smart-pop"><h4>存为智能分组</h4><span style="color:var(--fg-3)">符合条件的素材会自动出现在里面，以后新导入的也一样。</span>${parts.length ? parts.map((p) => `<div class="rule">${p}</div>`).join('') : '<div class="rule">先在上方选颜色、画幅或标签</div>'}<input id="smartName" placeholder="名称，例如：暖调海边"><div class="row-btns"><button class="btn" data-cancel>取消</button><button class="btn btn-primary" data-ok ${parts.length ? '' : 'disabled'}>保存</button></div></div>`, { rect: btn.getBoundingClientRect(), align: 'end' }, btn)
  pop.querySelector('#smartName')?.focus()
  pop.addEventListener('click', (e) => {
    if (e.target.closest('[data-cancel]')) closeFloating()
    if (e.target.closest('[data-ok]')) {
      const s = { id: `s${seq++}`, name: pop.querySelector('#smartName').value.trim() || '未命名智能分组', rules }
      smarts.push(s)
      closeFloating()
      go({ smart: s.id })
    }
  })
}

// ——事件：筛选——
$('kinds').addEventListener('click', (e) => { const b = e.target.closest('[data-kind]'); if (b) { st.kind = b.dataset.kind; st.sel.clear(); render() } })
$('fColor').addEventListener('click', (e) => {
  const btn = e.currentTarget
  const pop = openPopover(`<div class="pal-pop"><p>找颜色相近的素材（取自每张图的主色）</p><div class="pal-grid">${SWATCHES.map((c) => `<button data-c="${c}" style="background:${c}" aria-pressed="${st.color === c}" aria-label="${c}"></button>`).join('')}</div></div>`, { rect: btn.getBoundingClientRect() }, btn)
  pop.addEventListener('click', (ev) => { const c = ev.target.closest('[data-c]'); if (!c) return; st.color = st.color === c.dataset.c ? null : c.dataset.c; closeFloating(); st.sel.clear(); render() })
})
$('fShape').addEventListener('click', (e) => openMenu([...Object.entries(SHAPES).map(([k, v]) => ({ label: v, checked: st.shape === k, run: () => { st.shape = st.shape === k ? null : k; render() } }))], { rect: e.currentTarget.getBoundingClientRect() }, e.currentTarget))
$('fTags').addEventListener('click', (e) => tagPicker(e.currentTarget, [], 'filter'))
$('fRating').addEventListener('click', (e) => openMenu([null, 5, 4.5, 4, 3, 'none'].map((r) => ({ label: r === null ? '不限' : ratingLabel(r), checked: st.minRating === r, run: () => { st.minRating = r; st.sel.clear(); render() } })), { rect: e.currentTarget.getBoundingClientRect() }, e.currentTarget))
$('sort').addEventListener('click', (e) => openMenu(['最近添加', '最早添加', '评分', '名称'].map((s) => ({ label: s, checked: st.sort === s, run: () => { st.sort = s; render() } })), { rect: e.currentTarget.getBoundingClientRect(), align: 'end' }, e.currentTarget))
document.querySelectorAll('[data-layout]').forEach((b) => b.addEventListener('click', () => {
  st.layout = b.dataset.layout
  document.querySelectorAll('[data-layout]').forEach((x) => x.setAttribute('aria-pressed', String(x === b)))
  renderTiles()
}))
$('zoom').addEventListener('input', (e) => { st.w = Number(e.target.value); $('tiles').style.setProperty('--w', `${st.w}px`) })
$('emptyTrash').addEventListener('click', () => purge(assets.filter((a) => a.trashed)))
$('toggleInspector').addEventListener('click', (e) => {
  const off = document.body.classList.toggle('no-inspector')
  e.currentTarget.setAttribute('aria-pressed', String(!off))
})
let qTimer
$('q').addEventListener('input', (e) => { clearTimeout(qTimer); qTimer = setTimeout(() => { st.q = e.target.value; st.sel.clear(); if (st.view === 'gen') st.view = 'all'; render() }, 120) })
document.addEventListener('click', (e) => {
  const c = e.target.closest('[data-clear]')
  if (c) {
    const k = c.dataset.clear
    if (k === 'all') Object.assign(st, { color: null, shape: null, tagIds: [], q: '', kind: 'all', minRating: null })
    else st[k] = k === 'q' ? '' : null
    if (k === 'q' || k === 'all') $('q').value = ''
    render()
  }
  const ct = e.target.closest('[data-clear-tag]')
  if (ct) { st.tagIds = st.tagIds.filter((x) => x !== ct.dataset.clearTag); render() }
  if (e.target.closest('[data-widen]')) { st.group = null; st.smart = null; st.view = 'all'; render() }
  if (e.target.closest('#saveSmart')) saveSmart(e.target.closest('#saveSmart'))
  if (e.target.closest('[data-import]')) importMenu(e.target.closest('[data-import]'))
  const sg = e.target.closest('.subgroups [data-group], .crumb [data-group]')
  if (sg) { e.preventDefault(); go({ group: sg.dataset.group }) }
})

// ——事件：素材——
const tilesEl = $('tiles')
tilesEl.addEventListener('click', (e) => {
  const tile = e.target.closest('.tile')
  if (!tile) return
  const id = tile.dataset.id ?? tile.dataset.gen
  if (e.target.closest('[data-open]')) return st.view === 'gen' ? openGenViewer(id) : openAssetViewer(id)
  if (e.target.closest('[data-fav]')) {
    const a = assets.find((x) => x.id === id)
    a.fav = !a.fav
    render()
    return
  }
  const order = st.view === 'gen' ? generations.map((g) => g.id) : results().map((a) => a.id)
  if (e.shiftKey && st.anchor) {
    const [i, j] = [order.indexOf(st.anchor), order.indexOf(id)].sort((x, y) => x - y)
    st.sel = new Set(order.slice(i, j + 1))
  } else if (e.metaKey || e.ctrlKey || e.target.closest('[data-tick]')) {
    st.sel.has(id) ? st.sel.delete(id) : st.sel.add(id)
    st.anchor = id
  } else {
    st.sel = new Set([id])
    st.anchor = id
  }
  paintSelection()
})
// 只换选中态，不重绘网格：重绘会换掉节点，双击就接不上了
function paintSelection() {
  tilesEl.querySelectorAll('.tile').forEach((t) => t.setAttribute('aria-selected', String(st.sel.has(t.dataset.id ?? t.dataset.gen))))
  renderInspector()
  renderBatch()
}
tilesEl.addEventListener('dblclick', (e) => {
  const t = e.target.closest('.tile')
  if (t?.dataset.id) openAssetViewer(t.dataset.id)
  else if (t?.dataset.gen) openGenViewer(t.dataset.gen)
})
tilesEl.addEventListener('contextmenu', (e) => {
  const t = e.target.closest('.tile[data-id]')
  if (!t) return
  e.preventDefault()
  if (!st.sel.has(t.dataset.id)) { st.sel = new Set([t.dataset.id]); render() }
  const list = selected()
  const at = { x: e.clientX, y: e.clientY }
  const btn = { getBoundingClientRect: () => new DOMRect(e.clientX, e.clientY, 0, 0) }
  if (list.every((a) => a.trashed)) return openMenu([{ label: '恢复', icon: 'undo-2', run: () => restore(list) }, { label: '永久删除', icon: 'trash-2', danger: true, run: () => purge(list) }], at)
  openMenu([
    { label: '放大查看', icon: 'maximize', hint: '空格', run: () => openAssetViewer(list[0].id) },
    { label: '评分…', icon: 'star', hint: '1–5', run: () => ratePop(at, list) },
    { label: '放入项目…', icon: 'arrow-up-right', run: () => toProject(btn) },
    'sep',
    { label: '归入分组…', icon: 'folder', run: () => groupMenu(btn, list) },
    { label: '打标签…', icon: 'tag', run: () => tagPicker(btn, list) },
    { label: list.every((a) => a.fav) ? '取消最爱' : '加入最爱', icon: 'heart', run: () => { const on = !list.every((a) => a.fav); list.forEach((a) => { a.fav = on }); render() } },
    ...(st.group ? [{ label: `移出「${groupName(st.group)}」`, icon: 'unlink', run: () => { list.forEach((a) => { a.groups = a.groups.filter((g) => g !== st.group) }); st.sel.clear(); render() } }] : []),
    'sep',
    { label: '下载', icon: 'download', run: () => toast('开始下载') },
    { label: '移到回收站', icon: 'trash-2', danger: true, hint: '⌫', run: () => moveToTrash(list) },
  ], at)
})
// 检查器与批量条共用动作
function act(e) {
  const t = e.target.closest('button')
  if (!t) return
  const list = selected()
  const d = t.dataset
  if ('addGroup' in d) groupMenu(t, list)
  else if ('addTag' in d) tagPicker(t, list)
  else if ('favAll' in d) { const on = !list.every((a) => a.fav); list.forEach((a) => { a.fav = on }); render() }
  else if ('toProject' in d) toProject(t)
  else if ('trash' in d) moveToTrash(list)
  else if ('restore' in d) restore(list)
  else if ('purge' in d) purge(list)
  else if ('deselect' in d) { st.sel.clear(); render() }
  else if ('rateAll' in d) ratePop({ rect: t.getBoundingClientRect() }, list, t)
  else if ('dl' in d) toast('开始下载')
  else if ('reuse' in d) toast('已复制提示词')
  else if (d.ungroup) { list.forEach((a) => { a.groups = a.groups.filter((g) => g !== d.ungroup) }); render() }
  else if (d.untag) { list.forEach((a) => { a.tags = a.tags.filter((x) => x !== d.untag) }); render() }
  else if (d.pal) { st.color = d.pal; st.view = 'all'; st.group = null; st.smart = null; st.sel.clear(); render(); toast('按这个颜色找相近的素材') }
  else if (d.saveGen) {
    const g = generations.find((x) => x.id === d.saveGen)
    openMenu([{ section: '存入资产库' }, { label: '不放进分组', icon: 'box', run: () => saveGen(g, null) }, ...groups.filter((x) => !x.parent).map((x) => ({ label: x.name, icon: 'folder', run: () => saveGen(g, x.id) }))], { rect: t.getBoundingClientRect() }, t)
  }
}
function saveGen(g, gid) {
  g.saved = true
  assets.unshift(A({ kind: 'image', title: g.prompt.slice(0, 16), p: g.p, groups: gid ? [gid] : [], added: 0, note: g.prompt, dims: '2048 × 3072', size: '1.6 MB' }))
  render()
  toast(gid ? `已存入「${groupName(gid)}」` : '已存入资产库')
}
$('inspector').addEventListener('click', act)
bindStars($('inspector'), (_, v) => rate(selected(), v))
$('batch').addEventListener('click', act)
$('inspector').addEventListener('change', (e) => {
  const a = selected()[0]
  if (!a) return
  if (e.target.id === 'insTitle' && e.target.value.trim()) { a.title = e.target.value.trim(); renderTiles() }
  if (e.target.id === 'insNote') a.note = e.target.value
})

// 框选：在空白处按下拖动
const scrollEl = $('scroll')
scrollEl.addEventListener('pointerdown', (e) => {
  if (e.button !== 0 || e.target.closest('.tile, button, .empty')) return
  const box = scrollEl.getBoundingClientRect()
  const start = { x: e.clientX, y: e.clientY }
  const base = e.metaKey || e.shiftKey ? new Set(st.sel) : new Set()
  const m = document.createElement('div')
  m.className = 'marquee'
  $('board').append(m)
  let moved = false
  const move = (ev) => {
    const x = Math.min(start.x, ev.clientX), y = Math.min(start.y, ev.clientY)
    const w = Math.abs(ev.clientX - start.x), h = Math.abs(ev.clientY - start.y)
    if (w + h < 6 && !moved) return
    moved = true
    const board = $('board').getBoundingClientRect()
    Object.assign(m.style, { left: `${x - board.left}px`, top: `${Math.max(y, box.top) - board.top}px`, width: `${w}px`, height: `${h}px` })
    const next = new Set(base)
    tilesEl.querySelectorAll('.tile').forEach((t) => {
      const r = t.getBoundingClientRect()
      if (r.left < x + w && r.right > x && r.top < y + h && r.bottom > y) next.add(t.dataset.id ?? t.dataset.gen)
    })
    st.sel = next
    tilesEl.querySelectorAll('.tile').forEach((t) => t.setAttribute('aria-selected', String(next.has(t.dataset.id ?? t.dataset.gen))))
  }
  const up = () => {
    m.remove()
    removeEventListener('pointermove', move)
    removeEventListener('pointerup', up)
    if (!moved) st.sel.clear()
    render()
  }
  addEventListener('pointermove', move)
  addEventListener('pointerup', up)
})

// 拖素材到左侧分组
let dragging = null
tilesEl.addEventListener('dragstart', (e) => {
  const t = e.target.closest('.tile[data-id]')
  if (!t) return
  if (!st.sel.has(t.dataset.id)) { st.sel = new Set([t.dataset.id]) }
  dragging = selected()
  e.dataTransfer.setData('text/plain', dragging.map((a) => a.title).join('\n'))
  e.dataTransfer.effectAllowed = 'copy'
})
tilesEl.addEventListener('dragend', () => { dragging = null; document.querySelectorAll('.drop-on').forEach((x) => x.classList.remove('drop-on')) })
$('groups').addEventListener('dragover', (e) => {
  const row = e.target.closest('[data-group]')
  if (!row || !dragging) return
  e.preventDefault()
  document.querySelectorAll('.drop-on').forEach((x) => x !== row && x.classList.remove('drop-on'))
  row.classList.add('drop-on')
})
$('groups').addEventListener('dragleave', (e) => e.target.closest('[data-group]')?.classList.remove('drop-on'))
$('groups').addEventListener('drop', (e) => {
  const row = e.target.closest('[data-group]')
  if (!row || !dragging) return
  e.preventDefault()
  addToGroup(dragging, row.dataset.group)
  dragging = null
})

// 从系统拖入文件
const board = $('board')
board.addEventListener('dragover', (e) => {
  if (!e.dataTransfer.types.includes('Files')) return
  e.preventDefault()
  $('drop').hidden = false
  $('dropTarget').textContent = st.group ? `放进「${groupName(st.group)}」` : '放进资产库，之后再整理'
})
board.addEventListener('dragleave', (e) => { if (!board.contains(e.relatedTarget)) $('drop').hidden = true })
board.addEventListener('drop', (e) => {
  if (!e.dataTransfer.files.length) return
  e.preventDefault()
  $('drop').hidden = true
  importFiles(e.dataTransfer.files)
})
$('file').addEventListener('change', (e) => { importFiles(e.target.files); e.target.value = '' })
// 粘贴网址或文字
document.addEventListener('paste', (e) => {
  if (e.target.closest('input, textarea')) return
  const files = [...(e.clipboardData?.files ?? [])]
  if (files.length) return importFiles(files)
  const text = e.clipboardData?.getData('text')?.trim()
  if (!text) return
  if (/^https?:\/\//.test(text)) addLink(text)
  else addText(text)
})

document.addEventListener('keydown', (e) => {
  if (e.target.closest('input, textarea')) return
  if (e.key === '/') { e.preventDefault(); $('q').focus() }
  if ((e.key === ' ' || e.key === 'Enter') && st.sel.size === 1) { e.preventDefault(); const id = [...st.sel][0]; st.view === 'gen' ? openGenViewer(id) : openAssetViewer(id) }
  if (/^[0-5]$/.test(e.key) && st.sel.size && st.view !== 'gen' && !e.metaKey) rate(selected(), Number(e.key))
  if ((e.metaKey || e.ctrlKey) && e.key === 'a') { e.preventDefault(); st.sel = new Set(st.view === 'gen' ? generations.map((g) => g.id) : results().map((a) => a.id)); render() }
  if ((e.key === 'Backspace' || e.key === 'Delete') && st.sel.size && st.view !== 'trash' && st.view !== 'gen') moveToTrash(selected())
  if (e.key === 'Escape' && st.sel.size) { st.sel.clear(); render() }
})

// ——启动——
mountSide($('side'), { current: 'assets' })
hydrateIcons()
render()
Promise.all(assets.map(srcOf).concat(generations.map((g) => photo(g.p))).filter(Boolean).map(extract)).then(render)
