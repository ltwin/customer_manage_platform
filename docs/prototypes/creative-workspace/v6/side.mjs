// 创意空间的应用侧栏：首页、项目页、资产库共用。展开显示全称，折叠只剩图标，折叠与否记在本机。
// 结构从上到下：品牌 / 导航 / 最近打开 / 返回 CRM 与账号。以后导航加条目，最近打开整体往下挤，放不下就在自己的区域里滚动。
import { ic, esc } from './ui.mjs'

const KEY = 'cw.side.collapsed'
const NAV = [
  { id: 'home', label: '首页', icon: 'sparkles', href: './home.html#home' },
  { id: 'projects', label: '项目', icon: 'folder', href: './home.html#projects' },
  { id: 'assets', label: '资产库', icon: 'library', href: './assets.html' },
]
const thumb = (n) => (n ? `../v4/assets/photo-${String(n).padStart(2, '0')}.jpg` : '')
// 没有项目数据的页面（资产库）用这份示例，和首页的最近项目一致。
const SAMPLE = [
  { name: '九月，向海而行', thumb: thumb(10), state: 'run' },
  { name: '雪山婚纱 · 二稿', thumb: thumb(2), state: 'run' },
  { name: '品牌 Lookbook · 春季', thumb: thumb(9), state: 'wait' },
  { name: '毕业照 · 光影练习', thumb: thumb(8) },
  { name: '未命名项目', thumb: '' },
]

let el = null
let tip = null
let pref = false
// 中等宽度自动折叠，手机上改成顶栏；这两种情况下不显示折叠按钮，也不改动记下的偏好。
const narrow = matchMedia('(max-width: 1100px) and (min-width: 761px)')
const phone = matchMedia('(max-width: 760px)')

function read() {
  try {
    return localStorage.getItem(KEY) === '1'
  } catch {
    return false
  }
}
function write(v) {
  try {
    localStorage.setItem(KEY, v ? '1' : '0')
  } catch {
    // 无痕窗口等情况下存不了，只影响下次打开时的默认状态
  }
}

export function mountSide(root, { current, recent = SAMPLE } = {}) {
  el = root
  el.classList.add('side')
  el.setAttribute('aria-label', '创意空间导航')
  el.innerHTML = `
    <div class="side-top">
      <a class="side-brand" href="./home.html#home" data-tip="创意空间">${ic('aperture')}<span class="side-t">创意空间</span></a>
      <button class="icon-btn side-toggle" type="button"></button>
    </div>
    <nav class="side-nav">${NAV.map((n) => `<a href="${n.href}" data-view="${n.id}" data-tip="${n.label}">${ic(n.icon)}<span class="side-t">${n.label}</span></a>`).join('')}</nav>
    <section class="side-recent-sec">
      <div class="side-label"><span class="side-t">最近打开</span></div>
      <nav class="side-recent" aria-label="最近打开的项目"></nav>
    </section>
    <div class="side-foot">
      <a class="side-link" href="#" data-tip="返回 CRM">${ic('arrow-left')}<span class="side-t">返回 CRM</span></a>
      <div class="side-account" data-tip="Samson · 本月剩余 1,252 点"><span class="avatar">S</span><span class="who side-t">Samson<small>本月剩余 1,252 点</small></span></div>
    </div>`
  el.querySelector('.side-toggle').addEventListener('click', () => {
    pref = !pref
    write(pref)
    apply()
  })
  el.addEventListener('pointerover', showTip)
  el.addEventListener('pointerout', hideTip)
  el.addEventListener('focusin', showTip)
  el.addEventListener('focusout', hideTip)
  pref = read()
  apply()
  narrow.addEventListener('change', apply)
  phone.addEventListener('change', apply)
  setCurrent(current)
  setRecent(recent)
}

export function setCurrent(id) {
  el.querySelectorAll('.side-nav a').forEach((a) => {
    if (a.dataset.view === id) a.setAttribute('aria-current', 'page')
    else a.removeAttribute('aria-current')
  })
}

// list: [{name, thumb, state?: 'run' | 'wait', href?}]
export function setRecent(list) {
  el.querySelector('.side-recent').innerHTML = list
    .slice(0, 5)
    .map((r) => `<a href="${r.href ?? './index.html'}" data-tip="${esc(r.name)}"><span class="thumb" style="${r.thumb ? `background-image:url(${r.thumb})` : ''}">${r.state ? `<i class="dot ${r.state}"></i>` : ''}</span><span class="side-t">${esc(r.name)}</span></a>`)
    .join('')
}

function apply() {
  const on = !phone.matches && (pref || narrow.matches)
  el.classList.toggle('collapsed', on)
  el.classList.toggle('forced', narrow.matches || phone.matches)
  const btn = el.querySelector('.side-toggle')
  btn.innerHTML = ic(on ? 'panel-left-open' : 'panel-left-close')
  btn.setAttribute('aria-label', on ? '展开侧栏' : '收起侧栏')
  btn.setAttribute('aria-expanded', String(!on))
  btn.dataset.tip = '展开侧栏'
  hideTip()
}

// 折叠时名字藏起来了，悬停或聚焦时显示在图标右侧。提示挂在 body 上，不会被侧栏裁掉。
function showTip(e) {
  const t = e.target.closest('[data-tip]')
  if (!t || !el.classList.contains('collapsed')) return
  tip ??= Object.assign(document.createElement('div'), { className: 'side-tip', role: 'tooltip' })
  tip.textContent = t.dataset.tip
  document.body.append(tip)
  const r = t.getBoundingClientRect()
  tip.style.left = `${r.right + 8}px`
  tip.style.top = `${r.top + r.height / 2}px`
}
function hideTip(e) {
  if (e?.relatedTarget && e.target.closest('[data-tip]')?.contains(e.relatedTarget)) return
  tip?.remove()
}
