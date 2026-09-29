// 资产库抽屉：画布旁「找到并放入」。分组是层级、可多重归类；标签是横向筛选；
// 最近使用 / 本画布已用 / 最爱 / 未归类是固定视图。完整整理（分组树拖拽、检查器、回收站）留给整理模式。
import { photos, state, byId, connect, checkpoint } from './store.mjs'
import { on, emit, commit, select, addAsset, toWorld, pickFiles, fillWith, fillable } from './canvas.mjs'
import { ic, esc, openMenu, openPopover, closeFloating, toast } from './ui.mjs'

const KEY = 'creative-canvas-prototype-v6.2:library'

// ---------- 数据 ----------
const TAGS = [
  ['t-back', '逆光', '#e2a453', '光线'], ['t-soft', '柔光', '#d9c7a1', '光线'],
  ['t-warm', '暖调', '#e58a6b', '色调'], ['t-cool', '冷调', '#7fa7d9', '色调'],
  ['t-pastel', '低饱和', '#b9a8d9', '色调'],
  ['t-sea', '海边', '#6fc2c0', '场景'], ['t-city', '城市', '#9aa0a6', '场景'],
  ['t-white', '白裙', '#ededee', '服装'], ['t-flower', '花艺', '#e39bb5', '服装'],
]
function seed() {
  const groups = [
    ['g-client', '客户样片', null], ['g-client-sea', '海边婚纱', 'g-client'], ['g-client-city', '城市人像', 'g-client'],
    ['g-style', '风格参考', null], ['g-light', '光线与时刻', null], ['g-wardrobe', '服装道具', null],
  ].map(([id, name, parent]) => ({ id, name, parent }))
  // 照片编号 → [分组, 标签, 最爱]
  const plan = {
    '01': [['g-client-sea', 'g-light'], ['t-back', 't-warm', 't-sea'], true],
    '02': [['g-style'], ['t-sea', 't-pastel']],
    '03': [['g-light'], ['t-back', 't-soft']],
    '04': [['g-client-sea', 'g-style'], ['t-sea', 't-pastel'], true],
    '05': [['g-client-city', 'g-wardrobe'], ['t-white', 't-city']],
    '06': [['g-wardrobe'], ['t-white', 't-soft']],
    '07': [['g-wardrobe', 'g-style'], ['t-flower', 't-soft']],
    '08': [['g-client-city', 'g-light'], ['t-city', 't-cool']],
    '09': [[], ['t-pastel']],
    '10': [['g-client-sea'], ['t-sea', 't-white', 't-back'], true],
    '11': [['g-style'], ['t-cool']],
    '12': [[], ['t-sea', 't-pastel']],
  }
  const images = photos.map((p) => {
    const no = p.src.match(/photo-(\d+)/)[1]
    const [g, t, fav] = plan[no]
    return { id: `a-${no}`, kind: 'image', title: p.title, src: p.src, w: p.w, h: p.h, groups: g, tags: t, fav: Boolean(fav) }
  })
  const texts = [
    ['a-t1', '胶片暖调', '<p>低饱和暖调，高光微微溢出，暗部偏青，保留细腻颗粒。</p>', ['g-style'], ['t-warm', 't-pastel']],
    ['a-t2', '海边逆光', '<p>太阳在画面外左上方，发丝与裙摆形成轮廓光，脸部补一点反光板。</p>', ['g-light'], ['t-back', 't-sea']],
    ['a-t3', '客户偏好 · 林小姐', '<p>不喜欢正脸大特写，偏爱背影与侧脸；不要过度磨皮。</p>', [], []],
  ].map(([id, title, text, g, t]) => ({ id, kind: 'text', title, text, groups: g, tags: t, fav: false }))
  // 收藏时间：越靠前越新
  const assets = [...images, ...texts].map((a, i) => ({ ...a, added: 1000 - i }))
  return { groups, assets, recent: ['a-10', 'a-05', 'a-t2', 'a-04'] }
}
function load() {
  try { const raw = localStorage.getItem(KEY); if (raw) return JSON.parse(raw) } catch { /* 损坏时回到示例 */ }
  return seed()
}
const lib = load()
const persist = () => localStorage.setItem(KEY, JSON.stringify(lib))
const tagOf = new Map(TAGS.map(([id, name, color, cat]) => [id, { id, name, color, cat }]))
const groupOf = (id) => lib.groups.find((g) => g.id === id)
const childrenOf = (id) => lib.groups.filter((g) => g.parent === id)
// 一个组的内容包含子组内容（Eagle「在父文件夹显示子文件夹内容」）
function subtree(id) {
  const out = new Set([id])
  for (const g of lib.groups) if (g.parent && out.has(g.parent)) out.add(g.id)
  return out
}
const inGroup = (a, id) => { const s = subtree(id); return a.groups.some((g) => s.has(g)) }
const plain = (html) => new DOMParser().parseFromString(html, 'text/html').body.textContent
// 画布上已经用过这份资产：节点记着来源，示例节点按图片地址对上
const onCanvas = (a) => state.nodes.filter((n) => n.assetId === a.id || (a.src && n.versions?.includes(a.src)))

// ---------- 视图状态 ----------
const VIEWS = [['all', '全部'], ['recent', '最近使用'], ['canvas', '本画布'], ['fav', '最爱'], ['unfiled', '未归类']]
const TYPES = [['all', '全部'], ['image', '图片'], ['text', '文字']]
// pickFor：为节点选参考（可多选）；fillFor：给空节点选一张图填进去；editing：分组正在新建或改名
const v = { scope: 'all', query: '', type: 'all', tags: [], mode: 'and', sort: 'added', sel: new Set(), pickFor: null, fillFor: null, editing: null }
const collapsed = new Set()

function matches(a, scope = v.scope, useQuery = true) {
  if (v.fillFor && a.kind !== 'image') return false
  if (v.type !== 'all' && a.kind !== v.type) return false
  if (v.tags.length && !(v.mode === 'and' ? v.tags.every((t) => a.tags.includes(t)) : v.tags.some((t) => a.tags.includes(t)))) return false
  if (useQuery && v.query) {
    const q = v.query.toLowerCase()
    const hay = [a.title, a.text ? plain(a.text) : '', ...a.tags.map((t) => tagOf.get(t)?.name ?? '')].join(' ').toLowerCase()
    if (!hay.includes(q)) return false
  }
  if (scope === 'all') return true
  if (scope === 'recent') return lib.recent.includes(a.id)
  if (scope === 'canvas') return onCanvas(a).length > 0
  if (scope === 'fav') return a.fav
  if (scope === 'unfiled') return !a.groups.length
  return inGroup(a, scope)
}
function sorted(list) {
  if (v.scope === 'recent' && v.sort === 'added') return [...list].sort((a, b) => lib.recent.indexOf(a.id) - lib.recent.indexOf(b.id))
  return [...list].sort(v.sort === 'name' ? (a, b) => a.title.localeCompare(b.title, 'zh-CN') : (a, b) => b.added - a.added)
}
const results = (scope) => sorted(lib.assets.filter((a) => matches(a, scope)))
// 「全部」且没有关键词时按分组平铺浏览；有关键词或筛选即变成一张结果网格
const overview = () => v.scope === 'all' && !v.query && !v.tags.length
const scopeName = (id) => VIEWS.find((x) => x[0] === id)?.[1] ?? groupOf(id)?.name ?? '全部'
const trail = (id) => { const out = []; for (let g = groupOf(id); g; g = groupOf(g.parent)) out.unshift(g); return out }

// ---------- 渲染 ----------
const drawer = document.getElementById('assets')
const PREVIEW = 6
function tile(a) {
  const used = onCanvas(a).length
  const on = v.sel.has(a.id) ? ' on' : ''
  const marks = `${a.fav ? `<span class="t-fav" aria-label="最爱">${ic('heart')}</span>` : ''}${used
    ? `<button class="t-used" data-jump="${a.id}" title="已在画布上，点击定位">${ic('check')}${used > 1 ? used : ''}</button>` : ''}`
  const check = `<button class="t-check" data-check="${a.id}" aria-label="选择" aria-pressed="${Boolean(on)}">${ic('check')}</button>`
  const add = `<button class="t-add" data-add="${a.id}" aria-label="放入画布">${ic('plus')}</button>`
  if (a.kind === 'text')
    return `<div class="tile tile-text${on}" draggable="true" data-asset="${a.id}" tabindex="0" aria-label="${esc(a.title)}">
      <b>${esc(a.title)}</b><span class="t-body">${esc(plain(a.text))}</span>${marks}${check}${add}</div>`
  return `<div class="tile${on}" draggable="true" data-asset="${a.id}" tabindex="0" aria-label="${esc(a.title)}" style="aspect-ratio:${a.w}/${a.h}">
    <img src="${a.src}" alt="" loading="lazy" draggable="false"><span class="tile-name">${esc(a.title)}</span>${marks}${check}${add}</div>`
}
// 瀑布流两列：按累计高度分配，保持从左到右、从上到下的阅读顺序
function masonry(list) {
  const cols = [[], []], hgt = [0, 0]
  for (const a of list) {
    const i = hgt[0] <= hgt[1] ? 0 : 1
    cols[i].push(a)
    hgt[i] += a.kind === 'text' ? 0.62 : a.h / a.w
  }
  return `<div class="masonry">${cols.map((c) => `<div class="col">${c.map(tile).join('')}</div>`).join('')}</div>`
}
// 平铺浏览用等大方格：一屏能扫过几个组；超出的最后一格给「+N」进组
function preview(id, items) {
  const more = items.length > PREVIEW
  const shown = items.slice(0, more ? PREVIEW - 1 : PREVIEW)
  const rest = items[PREVIEW - 1]
  const cell = more ? `<button class="more-cell" data-scope="${id}" aria-label="查看全部 ${items.length} 项">${rest.kind === 'image' ? `<img src="${rest.src}" alt="" loading="lazy">` : ''}<span>+${items.length - shown.length}</span></button>` : ''
  return `<div class="grid">${shown.map(tile).join('')}${cell}</div>`
}
// 分组名称就地编辑：新建和改名共用一个输入框，Enter 确认、Esc 或失焦取消
const nameInput = (value, label) => `<input class="name-input" data-name-input value="${esc(value)}" maxlength="24" placeholder="分组名称" aria-label="${label}">`
const editingNew = (parent) => v.editing?.kind === 'new' && v.editing.parent === parent
function subChips(id) {
  const subs = childrenOf(id)
  const adding = editingNew(id)
  if (!subs.length && !adding) return ''
  return `<div class="sub-chips">${subs.map((s) => v.editing?.kind === 'rename' && v.editing.id === s.id
    ? `<span class="chip-edit">${ic('folder')}${nameInput(s.name, '重命名分组')}</span>`
    : `<button data-scope="${s.id}" data-group-menu-at="${s.id}">${ic('folder')}${esc(s.name)}<em>${results(s.id).length}</em></button>`).join('')}${adding ? `<span class="chip-edit">${ic('folder')}${nameInput('', '新建子分组')}</span>` : ''}</div>`
}
function sectionHTML(g) {
  const items = results(g.id)
  const open = !collapsed.has(g.id)
  const real = Boolean(groupOf(g.id))
  if (g.id === 'unfiled' && !items.length) return ''
  const renaming = v.editing?.kind === 'rename' && v.editing.id === g.id
  return `<section class="lib-sec" data-sec="${g.id}">
    <header class="sec-head">
      ${renaming ? `<span class="sec-edit">${ic('chevron-down')}${nameInput(g.name, '重命名分组')}</span>` : `<button class="sec-toggle" data-toggle="${g.id}" aria-expanded="${open}">${ic('chevron-down')}<span>${esc(g.name)}</span><em>${items.length}</em></button>`}
      <span class="sec-actions">${real ? `<button class="icon-btn sm" data-group-menu="${g.id}" aria-label="分组操作">${ic('ellipsis')}</button>` : ''}
      <button class="sec-open" data-scope="${g.id}">查看全部${ic('chevron-right')}</button></span>
    </header>
    ${open ? `${subChips(g.id)}
      ${items.length ? preview(g.id, items) : '<p class="sec-empty">这个分组还没有素材，把画布上的节点拖到组名上即可收进来</p>'}` : ''}
  </section>`
}
function bodyHTML() {
  if (overview()) {
    // 未归类放在最后，和普通分组一样能直接拖用，提醒顺手整理
    // 新建入口放在最上面，新组也插到最前，输入的位置就是它出现的位置
    const roots = lib.groups.filter((g) => !g.parent)
    const bar = editingNew(null)
      ? `<div class="groups-bar editing">${ic('folder')}${nameInput('', '新建分组')}</div>`
      : `<div class="groups-bar"><span>${roots.length} 个分组</span><button data-a="new-group">${ic('plus')}新建分组</button></div>`
    return bar + [...roots, { id: 'unfiled', name: '未归类' }].map(sectionHTML).join('')
  }
  const list = results(v.scope)
  const chips = groupOf(v.scope) && !v.query ? subChips(v.scope) : ''
  if (list.length) return chips + masonry(list)
  const narrowed = v.query || v.tags.length || v.type !== 'all'
  return `${chips}<div class="lib-empty">${ic('search')}<p>${narrowed ? '没有符合条件的素材' : v.scope === 'canvas' ? '这张画布还没有用到资产库里的素材' : '这里还是空的'}</p>
    ${narrowed ? `<button class="btn" data-clear>清除条件</button>${v.scope !== 'all' ? '<button class="btn" data-scope="all" data-keep>在全部资产中查找</button>' : ''}` : ''}</div>`
}

function crumbHTML() {
  if (overview()) return ''
  const path = groupOf(v.scope) ? trail(v.scope) : []
  const renaming = path.length && v.editing?.kind === 'rename' && v.editing.id === v.scope
  const parts = path.length
    ? path.map((g, i) => i === path.length - 1 ? (renaming ? nameInput(g.name, '重命名分组') : `<b>${esc(g.name)}</b>`) : `<button data-scope="${g.id}">${esc(g.name)}</button>`).join('<i>/</i>')
    : `<b>${esc(v.query && v.scope === 'all' ? '搜索结果' : scopeName(v.scope))}</b>`
  const back = path.length > 1 ? path.at(-2).id : 'all'
  return `<div class="crumb"><button class="icon-btn sm" data-scope="${back}" aria-label="返回">${ic('chevron-left')}</button>
    <span class="path">${parts}</span><em>${results(v.scope).length} 项</em>${path.length ? `<button class="icon-btn sm" data-group-menu="${v.scope}" aria-label="分组操作">${ic('ellipsis')}</button>` : ''}</div>`
}
function toolsHTML() {
  const tagBtn = `<button class="tool${v.tags.length ? ' on' : ''}" data-a="tags" aria-haspopup="true">${ic('tag')}标签${v.tags.length ? `<em>${v.tags.length}</em>` : ''}</button>`
  const sortBtn = `<button class="tool" data-a="sort" aria-haspopup="true">${v.sort === 'name' ? '名称' : v.scope === 'recent' ? '最近' : '收藏时间'}${ic('chevron-down')}</button>`
  const seg = `<div class="seg" role="group" aria-label="类型">${TYPES.map(([k, l]) => `<button data-type="${k}" aria-pressed="${v.type === k}">${l}</button>`).join('')}</div>`
  const active = v.tags.length ? `<div class="active-tags">${v.tags.map((t) => { const g = tagOf.get(t); return `<button data-untag="${t}" style="--c:${g.color}"><i></i>${esc(g.name)}${ic('x')}</button>` }).join('')}
    ${v.tags.length > 1 ? `<button class="mode" data-a="mode">${v.mode === 'and' ? '同时满足' : '满足任一'}</button>` : ''}</div>` : ''
  return `<div class="lib-tools">${v.fillFor ? '<span class="tools-note">只显示图片</span>' : seg}<span class="grow"></span>${tagBtn}${sortBtn}</div>${active}`
}
function footHTML() {
  const n = v.sel.size
  if (v.fillFor) return `<div class="lib-foot pick"><span>点一张图片填进「${esc(byId(v.fillFor)?.title ?? '节点')}」</span><span class="grow"></span><button class="btn" data-a="cancel-pick">取消</button></div>`
  if (v.pickFor) {
    const target = byId(v.pickFor)
    return `<div class="lib-foot pick"><span>为「${esc(target?.title ?? '节点')}」选参考${n ? ` · 已选 ${n}` : ''}</span><span class="grow"></span>
      <button class="btn" data-a="cancel-pick">取消</button><button class="btn btn-primary" data-a="confirm-pick" ${n ? '' : 'disabled'}>添加${n ? ` ${n} 项` : ''}</button></div>`
  }
  if (n) return `<div class="lib-foot pick"><span>已选 ${n} 项</span><span class="grow"></span>
    <button class="btn" data-a="clear-sel">取消</button><button class="btn btn-primary" data-a="place-sel">放入画布</button></div>`
  return `<div class="lib-foot">拖入画布 · 空格预览 · ⌘ 点击多选</div>`
}
function paint() {
  if (drawer.hidden) return
  const active = document.activeElement
  const typing = active?.matches?.('.lib-search input') ? [active.selectionStart, active.selectionEnd] : null
  const scroll = drawer.querySelector('.shelf')?.scrollTop ?? 0
  const sameScope = drawer.dataset.view === v.scope
  drawer.dataset.view = v.scope
  usedSig = sig()
  drawer.classList.toggle('picking', Boolean(v.pickFor || v.fillFor))
  drawer.classList.toggle('filling', Boolean(v.fillFor))
  drawer.innerHTML = `<div class="panel-head"><h2>${v.fillFor ? '选择图片' : v.pickFor ? '选择参考素材' : '资产库'}</h2><span class="grow"></span>
      <button class="icon-btn" data-a="upload" aria-label="上传到资产库" title="上传">${ic('upload')}</button>
      <button class="icon-btn" data-a="expand" aria-label="整理模式" title="整理模式">${ic('maximize')}</button>
      <button class="icon-btn" data-a="close" aria-label="收起资产库" title="收起">${ic('panel-left-close')}</button></div>
    <label class="search lib-search">${ic('search')}<input placeholder="${v.scope === 'all' ? '搜索全部资产' : `在「${esc(scopeName(v.scope))}」中搜索`}" value="${esc(v.query)}" aria-label="搜索资产">${v.query ? `<button data-a="clear-q" aria-label="清除">${ic('x')}</button>` : ''}</label>
    <div class="views" role="tablist">${VIEWS.map(([k, l]) => `<button role="tab" data-scope="${k}" aria-selected="${v.scope === k || (k === 'all' && Boolean(groupOf(v.scope)))}">${l}</button>`).join('')}</div>
    ${crumbHTML()}${toolsHTML()}
    <div class="shelf" tabindex="-1">${bodyHTML()}</div>${footHTML()}`
  if (sameScope) drawer.querySelector('.shelf').scrollTop = scroll
  if (typing) { const inp = drawer.querySelector('.lib-search input'); inp.focus(); inp.setSelectionRange(...typing) }
}

// ---------- 动作 ----------
const byAsset = (id) => lib.assets.find((a) => a.id === id)
function used(ids) {
  lib.recent = [...ids, ...lib.recent.filter((x) => !ids.includes(x))].slice(0, 12)
  persist()
}
// 一批素材横排放在 at 附近，整批一次撤销
export function placeAssets(ids, at) {
  const list = ids.map(byAsset).filter(Boolean)
  if (!list.length) return []
  checkpoint()
  const gap = 40, w = 300, x0 = at.x - ((list.length - 1) * (w + gap)) / 2
  const nodes = list.map((a, i) => addAsset(a, { x: x0 + i * (w + gap), y: at.y }, { batch: true }))
  select(nodes.map((n) => n.id))
  commit()
  used(list.map((a) => a.id))
  return nodes
}
const viewCenter = () => toWorld(innerWidth / 2 + 180, innerHeight / 2)
// 从 @ 选择器进入：选中的素材竖排放在目标左侧，连线并按顺序引用
function confirmPick() {
  const target = byId(v.pickFor)
  const list = [...v.sel].map(byAsset).filter(Boolean)
  if (!target || !list.length) return close()
  checkpoint()
  const hs = list.map((a) => (a.kind === 'text' ? 220 : Math.round((300 * a.h) / a.w)))
  const colH = hs.reduce((s, h) => s + h, 0) + 32 * (hs.length - 1)
  // 参考列与目标竖直居中；左边已有节点就继续往左让，不叠在别的节点上
  const top = target.y + target.h / 2 - colH / 2
  const others = state.nodes.filter((n) => n.type !== 'group' && n.id !== target.id)
  const hit = (x) => others.some((n) => n.x < x + 300 + 24 && n.x + n.w + 24 > x && n.y < top + colH + 24 && n.y + n.h + 24 > top)
  let x = target.x - 380
  for (let i = 0; i < 8 && hit(x); i++) x -= 340
  let y = top
  const nodes = list.map((a, i) => {
    const n = addAsset(a, { x: x + 150, y: y + hs[i] / 2 }, { batch: true })
    y += hs[i] + 32
    connect(n.id, target.id)
    return n
  })
  const tokens = nodes.map((n) => `@[${n.id}]`).join(' ')
  target.prompt = `${(target.prompt ?? '').replace(/\s*$/, target.prompt ? ' ' : '')}${tokens} `
  used(list.map((a) => a.id))
  close()
  select([target.id])
  commit()
  emit('focus', target.id)
  toast(`已连接 ${nodes.length} 个参考`)
}
// 空节点直接用这张图，不另建节点
function fillFrom(assetId, nodeId = v.fillFor) {
  const a = byAsset(assetId), n = byId(nodeId)
  if (!a || a.kind !== 'image' || !fillable(n)) return close()
  close()
  fillWith(n, { src: a.src, title: a.title, assetId: a.id })
  used([a.id])
}
function jump(assetId) {
  const nodes = onCanvas(byAsset(assetId))
  if (!nodes.length) return
  select(nodes.map((n) => n.id))
  emit('fit', new Set(nodes.map((n) => n.id)))
}
function toggleSel(id, range) {
  if (range && lastPicked) {
    const ids = [...drawer.querySelectorAll('[data-asset]')].map((el) => el.dataset.asset)
    const [i, j] = [ids.indexOf(lastPicked), ids.indexOf(id)].sort((a, b) => a - b)
    if (i >= 0) { ids.slice(i, j + 1).forEach((x) => v.sel.add(x)); return paint() }
  }
  v.sel.has(id) ? v.sel.delete(id) : v.sel.add(id)
  lastPicked = id
  paint()
}
let lastPicked = null

// ---------- 悬停放大 / 空格快速查看 ----------
let peek = null, peekTimer = 0
function showPeek(el) {
  clearTimeout(peekTimer)
  peekTimer = setTimeout(() => {
    const a = byAsset(el.dataset.asset)
    if (!a || !el.isConnected || document.querySelector('.lib-look')) return
    hidePeek()
    peek = document.createElement('div')
    peek.className = 'ref-peek lib-peek'
    peek.innerHTML = `${a.kind === 'image' ? `<img src="${a.src}" alt="">` : `<div class="peek-text">${esc(plain(a.text))}</div>`}
      <div class="peek-cap"><b>${esc(a.title)}</b><span>${metaLine(a)}</span></div>`
    document.body.append(peek)
    const r = el.getBoundingClientRect(), w = peek.offsetWidth, h = peek.offsetHeight
    peek.style.left = `${Math.min(drawer.getBoundingClientRect().right + 12, innerWidth - w - 8)}px`
    peek.style.top = `${Math.max(8, Math.min(r.top + r.height / 2 - h / 2, innerHeight - h - 8))}px`
  }, 360)
}
function hidePeek() { clearTimeout(peekTimer); peek?.remove(); peek = null }
function metaLine(a) {
  const groups = a.groups.map((g) => groupOf(g)?.name).filter(Boolean)
  const tags = a.tags.map((t) => tagOf.get(t)?.name).filter(Boolean)
  return esc([a.kind === 'image' ? `${a.w}×${a.h}` : '文字', groups.join('、') || '未归类', tags.join(' · ')].filter(Boolean).join('　'))
}
function quickLook(id) {
  const a = byAsset(id)
  if (!a) return
  hidePeek()
  document.querySelector('.lib-look')?.remove()
  const el = document.createElement('div')
  el.className = 'lib-look'
  el.tabIndex = -1
  el.innerHTML = `<figure>${a.kind === 'image' ? `<img src="${a.src}" alt="${esc(a.title)}">` : `<div class="look-text">${a.text}</div>`}
    <figcaption><b>${esc(a.title)}</b><span>${metaLine(a)}</span><span class="grow"></span>
    <button class="btn" data-look-fav>${ic('heart')}${a.fav ? '取消最爱' : '加入最爱'}</button>
    <button class="btn btn-primary" data-look-add>${ic('plus')}放入画布</button></figcaption></figure>`
  document.body.append(el)
  el.focus()
  const done = () => { el.remove(); drawer.querySelector(`[data-asset="${id}"]`)?.focus() }
  el.addEventListener('click', (e) => {
    if (e.target.closest('[data-look-add]')) { done(); return placeAssets([id], viewCenter()) }
    if (e.target.closest('[data-look-fav]')) { a.fav = !a.fav; persist(); done(); return paint() }
    if (!e.target.closest('figure')) done()
  })
  el.addEventListener('keydown', (e) => {
    e.stopPropagation()
    if (e.key === ' ' || e.key === 'Escape') { e.preventDefault(); done() }
    if (e.key === 'ArrowRight' || e.key === 'ArrowLeft') {
      const ids = [...drawer.querySelectorAll('[data-asset]')].map((x) => x.dataset.asset)
      const next = ids[(ids.indexOf(id) + (e.key === 'ArrowRight' ? 1 : -1) + ids.length) % ids.length]
      el.remove(); quickLook(next)
    }
  })
}

// ---------- 标签与排序弹层 ----------
function tagPopover(btn) {
  const cats = [...new Set(TAGS.map((t) => t[3]))]
  const count = (t) => lib.assets.filter((a) => a.tags.includes(t) && matches({ ...a, tags: [...a.tags] }, v.scope, true)).length
  const html = () => `<label class="search tp-search">${ic('search')}<input placeholder="查找标签" aria-label="查找标签"></label>
    <div class="tp-list">${cats.map((c) => `<div class="tp-cat">${c}</div>${TAGS.filter((t) => t[3] === c).map(([id, name, color]) =>
      `<button class="tp-item" data-tag="${id}" data-name="${name}" aria-pressed="${v.tags.includes(id)}" style="--c:${color}"><i></i><span>${name}</span><em>${count(id)}</em>${v.tags.includes(id) ? ic('check') : ''}</button>`).join('')}`).join('')}</div>
    <div class="tp-foot"><button data-tp-mode>${v.mode === 'and' ? '同时满足全部所选' : '满足任一所选'}</button><span class="grow"></span><button data-tp-clear ${v.tags.length ? '' : 'disabled'}>清除</button></div>`
  const pop = openPopover(html(), { rect: btn.getBoundingClientRect(), align: 'end' }, btn, 'tag-pop')
  const filter = () => { const q = pop.querySelector('input').value.trim(); pop.querySelectorAll('.tp-item').forEach((b) => (b.hidden = q && !b.dataset.name.includes(q))) }
  pop.addEventListener('click', (e) => {
    const t = e.target.closest('[data-tag]')?.dataset.tag
    if (t) v.tags = v.tags.includes(t) ? v.tags.filter((x) => x !== t) : [...v.tags, t]
    else if (e.target.closest('[data-tp-mode]')) v.mode = v.mode === 'and' ? 'or' : 'and'
    else if (e.target.closest('[data-tp-clear]')) v.tags = []
    else return
    const q = pop.querySelector('input').value
    pop.innerHTML = html()
    pop.querySelector('input').value = q
    filter()
    paint()
  })
  pop.addEventListener('input', filter)
  pop.addEventListener('keydown', (e) => { if (e.key !== 'Escape') e.stopPropagation() })
  pop.querySelector('input').focus()
}
function sortMenu(btn) {
  const opts = [['added', v.scope === 'recent' ? '最近使用' : '收藏时间'], ['name', '名称']]
  const pop = openPopover(`<div class="menu-flat">${opts.map(([k, l]) => `<button class="menu-item" data-sort="${k}"><span>${l}</span>${v.sort === k ? `<span class="check">${ic('check')}</span>` : ''}</button>`).join('')}</div>`,
    { rect: btn.getBoundingClientRect(), align: 'end' }, btn, 'menu')
  pop.addEventListener('click', (e) => { const k = e.target.closest('[data-sort]')?.dataset.sort; if (k) { v.sort = k; closeFloating(); paint() } })
}

// ---------- 分组管理 ----------
// 删除分组只去掉归类关系：资产保留，子组升到上一级
function startNew(parent = null) {
  if (parent) collapsed.delete(parent)
  v.editing = { kind: 'new', parent }
  paint()
  focusName()
}
function startRename(id) {
  v.editing = { kind: 'rename', id }
  paint()
  focusName(true)
}
function focusName(select) {
  const inp = drawer.querySelector('[data-name-input]')
  if (!inp) return
  inp.focus()
  if (select) inp.select()
  inp.scrollIntoView({ block: 'nearest' })
}
function commitName(value) {
  const e = v.editing
  v.editing = null
  const name = value.trim()
  if (!e || !name) return paint()
  const siblings = lib.groups.filter((g) => g.parent === (e.kind === 'new' ? e.parent : groupOf(e.id)?.parent) && g.id !== e.id)
  if (siblings.some((g) => g.name === name)) { paint(); return toast(`同一层已有「${name}」`) }
  if (e.kind === 'new') {
    const g = { id: `g-${(seqNo++).toString(36)}`, name, parent: e.parent }
    if (e.parent) lib.groups.push(g)
    else lib.groups.unshift(g)
    toast(`已新建「${name}」`)
  } else {
    const g = groupOf(e.id)
    if (g) g.name = name
  }
  persist()
  paint()
}
function deleteGroup(id) {
  const g = groupOf(id)
  if (!g) return
  const before = JSON.stringify(lib)
  const members = lib.assets.filter((a) => a.groups.includes(id)).length
  for (const c of childrenOf(id)) c.parent = g.parent
  for (const a of lib.assets) a.groups = a.groups.filter((x) => x !== id)
  lib.groups = lib.groups.filter((x) => x.id !== id)
  persist()
  if (v.scope === id) v.scope = g.parent ?? 'all'
  paint()
  toast(`已删除「${g.name}」${members ? `，${members} 项素材仍在资产库` : ''}`, {
    label: '撤销',
    run: () => { Object.assign(lib, JSON.parse(before)); persist(); paint() },
  })
}
function groupMenu(id, rect) {
  const g = groupOf(id)
  if (!g) return
  const depth = trail(id).length
  openMenu([
    { label: '重命名', icon: 'pencil', run: () => startRename(id) },
    // 最多两层，面包屑和子组 chip 都不需要再折叠
    ...(depth < 2 ? [{ label: '新建子分组', icon: 'folder', run: () => startNew(id) }] : []),
    'sep',
    { label: '删除分组', icon: 'trash-2', danger: true, sub: childrenOf(id).length ? '素材保留，子分组移到上一级' : '素材保留在资产库', run: () => deleteGroup(id) },
  ], { rect, align: 'end' })
}

// ---------- 画布内容存回资产库 ----------
let seqNo = Date.now() % 100000
function toAsset(n, groupId) {
  const src = n.versions?.[n.main ?? n.versions.length - 1]
  const known = lib.assets.find((a) => (n.assetId && a.id === n.assetId) || (src && a.src === src))
  if (known) {
    // 已在库里：只追加归类，不重复建资产
    if (groupId && !known.groups.includes(groupId)) known.groups.push(groupId)
    return { a: known, fresh: false }
  }
  const base = { id: `a-n${(seqNo++).toString(36)}`, title: n.title, groups: groupId ? [groupId] : [], tags: [], fav: false, added: Math.max(0, ...lib.assets.map((x) => x.added)) + 1 }
  const a = n.type === 'text'
    ? (n.text ? { ...base, kind: 'text', text: n.text } : null)
    : (src ? { ...base, kind: 'image', src, w: n.w, h: n.h } : null)
  if (a) lib.assets.push(a)
  return { a, fresh: Boolean(a) }
}
function saveNodes(ids, groupId) {
  const nodes = ids.map(byId).filter((n) => n && n.type !== 'group')
  const out = []
  for (const n of nodes) {
    const r = toAsset(n, groupId)
    // 节点记住来源，「本画布」视图和已用角标靠它对上
    if (r.a) { n.assetId = r.a.id; out.push(r) }
  }
  if (!out.length) return toast('这些节点还没有内容，无法存入资产库')
  persist()
  const where = groupId ? `「${groupOf(groupId).name}」` : '资产库'
  const fresh = out.filter((r) => r.fresh).length
  toast(fresh ? `已存入${where} ${fresh} 项` : `已归入${where}`)
  commit()
  paint()
}
function saveMenu({ ids, at }) {
  const items = [{ id: '', name: '不归类，只存入' }, ...lib.groups.map((g) => ({ id: g.id, name: `${g.parent ? '　' : ''}${g.name}` }))]
  const pop = openPopover(`<div class="menu-label">存入资产库</div>${items.map((g) => `<button class="menu-item" data-save="${g.id}">${ic(g.id ? 'folder' : 'bookmark')}<span>${esc(g.name)}</span></button>`).join('')}`, at, null, 'menu')
  pop.addEventListener('click', (e) => {
    const b = e.target.closest('[data-save]')
    if (!b) return
    closeFloating()
    saveNodes(ids, b.dataset.save || null)
  })
}

// ---------- 交互 ----------
function setScope(id, keep = false) {
  v.scope = id
  if (!keep) v.query = ''
  if (id === 'recent') v.sort = 'added'
  hidePeek()
  paint()
  drawer.querySelector('.shelf').scrollTop = 0
}
drawer.addEventListener('click', (e) => {
  const t = e.target
  const act = t.closest('[data-a]')?.dataset.a
  if (act === 'close') return close()
  if (act === 'upload') return pickFiles()
  if (act === 'expand') return toast('整理模式在下一步做：分组树、批量归类与检查器')
  if (act === 'clear-q') { v.query = ''; paint(); return drawer.querySelector('.lib-search input').focus() }
  if (act === 'tags') return tagPopover(t.closest('[data-a]'))
  if (act === 'sort') return sortMenu(t.closest('[data-a]'))
  if (act === 'mode') { v.mode = v.mode === 'and' ? 'or' : 'and'; return paint() }
  if (act === 'clear-sel') { v.sel.clear(); return paint() }
  if (act === 'place-sel') { const ids = [...v.sel]; v.sel.clear(); placeAssets(ids, viewCenter()); return paint() }
  if (act === 'cancel-pick') return close()
  if (act === 'new-group') return startNew(groupOf(v.scope) ? v.scope : null)
  if (act === 'confirm-pick') return confirmPick()
  const gm = t.closest('[data-group-menu]')
  if (gm) return groupMenu(gm.dataset.groupMenu, gm.getBoundingClientRect())
  if (t.closest('[data-name-input], .chip-edit, .sec-edit')) return
  if (t.closest('[data-clear]')) { v.query = ''; v.tags = []; v.type = 'all'; return paint() }
  const scope = t.closest('[data-scope]')
  if (scope) return setScope(scope.dataset.scope, 'keep' in scope.dataset)
  const toggle = t.closest('[data-toggle]')?.dataset.toggle
  if (toggle) { collapsed.has(toggle) ? collapsed.delete(toggle) : collapsed.add(toggle); return paint() }
  const type = t.closest('[data-type]')?.dataset.type
  if (type) { v.type = type; return paint() }
  const untag = t.closest('[data-untag]')?.dataset.untag
  if (untag) { v.tags = v.tags.filter((x) => x !== untag); return paint() }
  const jumpTo = t.closest('[data-jump]')?.dataset.jump
  if (jumpTo) return jump(jumpTo)
  const check = t.closest('[data-check]')?.dataset.check
  if (check) return toggleSel(check, e.shiftKey)
  const add = t.closest('[data-add]')?.dataset.add
  if (add) return placeAssets([add], viewCenter())
  const tileEl = t.closest('[data-asset]')
  if (!tileEl) return
  const id = tileEl.dataset.asset
  if (v.fillFor) return fillFrom(id)
  // 选参考模式、已有多选或按住 ⌘ / Shift 时点击就是选择；否则打开快速查看
  if (v.pickFor || v.sel.size || e.metaKey || e.ctrlKey || e.shiftKey) return toggleSel(id, e.shiftKey)
  quickLook(id)
})
// 失焦等于确认，和画布上重命名节点的习惯一致；空名称视为取消
drawer.addEventListener('focusout', (e) => {
  if (e.target.matches?.('[data-name-input]') && v.editing) commitName(e.target.value)
})
// 子组 chip 右键打开分组菜单
drawer.addEventListener('contextmenu', (e) => {
  const el = e.target.closest('[data-group-menu-at], [data-sec] .sec-head')
  const id = el?.dataset.groupMenuAt ?? el?.closest('[data-sec]')?.dataset.sec
  if (!groupOf(id)) return
  e.preventDefault()
  groupMenu(id, { left: e.clientX, right: e.clientX, top: e.clientY, bottom: e.clientY })
})
drawer.addEventListener('input', (e) => {
  if (!e.target.matches('.lib-search input')) return
  v.query = e.target.value.trim()
  paint()
})
drawer.addEventListener('keydown', (e) => {
  e.stopPropagation()
  if (e.target.matches?.('[data-name-input]')) {
    if (e.key === 'Enter') { e.preventDefault(); commitName(e.target.value) }
    if (e.key === 'Escape') { e.preventDefault(); v.editing = null; paint() }
    return
  }
  const tileEl = e.target.closest?.('[data-asset]')
  if (e.key === 'Escape') {
    if (e.target.matches('.lib-search input') && v.query) { v.query = ''; return paint() }
    if (v.sel.size && !v.pickFor) { v.sel.clear(); return paint() }
    return close()
  }
  if (!tileEl) return
  if (e.key === ' ') { e.preventDefault(); quickLook(tileEl.dataset.asset) }
  if (e.key === 'Enter') { e.preventDefault(); const id = tileEl.dataset.asset; v.fillFor ? fillFrom(id) : v.pickFor ? toggleSel(id) : placeAssets([id], viewCenter()) }
})
drawer.addEventListener('pointerover', (e) => {
  const el = e.target.closest?.('.tile[data-asset]')
  if (!el) return
  // 移到卡片上的按钮时让位给按钮提示
  if (e.target.closest('button')) return hidePeek()
  if (!el.contains(e.relatedTarget) || !peek) showPeek(el)
})
// 在同一张卡片里移动（图片 → 标题）不算离开
drawer.addEventListener('pointerout', (e) => {
  const el = e.target.closest?.('.tile[data-asset]')
  if (el && !el.contains(e.relatedTarget)) hidePeek()
})
drawer.addEventListener('scroll', hidePeek, true)
// 拖出一张时，如果它在多选里，就带上整批
drawer.addEventListener('dragstart', (e) => {
  const id = e.target.closest?.('[data-asset]')?.dataset.asset
  if (!id) return
  hidePeek()
  const ids = v.sel.has(id) ? [...v.sel] : [id]
  e.dataTransfer.setData('application/x-assets', JSON.stringify(ids))
  e.dataTransfer.effectAllowed = 'copy'
  if (ids.length > 1) {
    const ghost = document.createElement('div')
    ghost.className = 'drag-ghost'
    ghost.textContent = `${ids.length} 项`
    document.body.append(ghost)
    e.dataTransfer.setDragImage(ghost, 20, 14)
    setTimeout(() => ghost.remove())
  }
})

// ---------- 开合与外部钩子 ----------
const toggleBtn = document.getElementById('assets-toggle')
export function open({ pickFor = null, fillFor = null } = {}) {
  const wasHidden = drawer.hidden
  drawer.hidden = false
  toggleBtn.setAttribute('aria-pressed', 'true')
  v.pickFor = pickFor
  v.fillFor = fillFor
  v.sel.clear()
  // 带着任务进来时从全部分组开始找，不停在上次浏览的视图和条件里
  if (pickFor || fillFor) Object.assign(v, { scope: 'all', query: '', type: 'all', tags: [] })
  paint()
  if (pickFor) toast('选择一个或多个素材作为参考')
  if (wasHidden) emit('panels')
}
export function close() {
  if (drawer.hidden) return
  drawer.hidden = true
  toggleBtn.setAttribute('aria-pressed', 'false')
  v.pickFor = v.fillFor = v.editing = null
  v.sel.clear()
  hidePeek()
  emit('panels')
}
export const toggle = () => (drawer.hidden ? open() : close())

// 画布把拖入的资产 id 交给这里放置
on('asset-drop', ({ ids, at, into }) => {
  if (into && ids.length === 1 && byAsset(ids[0])?.kind === 'image') return fillFrom(ids[0], into)
  placeAssets(ids, at)
})
on('assets-fill', (id) => open({ fillFor: id }))

// 节点拖到分组标题（或整个抽屉）上：高亮落点，松手存入
function dropZone(x, y) {
  if (drawer.hidden) return null
  // 被拖的节点盖在抽屉上面，elementFromPoint 只会拿到节点本身，所以按矩形判定
  const inside = (r) => x >= r.left && x <= r.right && y >= r.top && y <= r.bottom
  if (!inside(drawer.getBoundingClientRect())) return null
  const sec = [...drawer.querySelectorAll('[data-sec]')].find((el) => inside(el.getBoundingClientRect()))
  const id = sec?.dataset.sec ?? v.scope
  return { el: sec?.querySelector('.sec-head') ?? drawer, group: groupOf(id) ? id : null }
}
let hot = null
function setHot(z) {
  const el = z?.el ?? null
  if (hot === el) return
  hot?.classList.remove('drop-hot')
  hot = el
  hot?.classList.add('drop-hot')
}
on('node-drag', ({ x, y }) => setHot(dropZone(x, y)))
on('node-drop', (out) => {
  const z = dropZone(out.x, out.y)
  setHot(null)
  if (!z) return
  out.taken = true
  saveNodes(out.ids, z.group)
})
on('save-asset', saveMenu)
on('asset-used', (id) => { if (id) used([id]); paint() })
// 画布变化只影响「已用」角标和「本画布」视图：签名不变就不重绘，拖动节点时抽屉不闪
let usedSig = ''
const sig = () => lib.assets.map((a) => onCanvas(a).length).join(',')
on('change', () => {
  if (drawer.hidden) return
  const s = sig()
  if (s === usedSig) return
  usedSig = s
  requestAnimationFrame(paint)
})
