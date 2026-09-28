// 提示词编辑器：文字与引用 chip 混排。数据仍是一段字符串，chip 对应 @[节点 id] token。
import { byId, mainSrc, refLabels, MENTION } from './store.mjs'
import { ic, esc } from './ui.mjs'

const TYPE_ICON = { text: 'file-text', image: 'image', video: 'video' }

// chip 显示缩略图 + 节点名；「发给模型时的编号」放在悬停预览里，句子里读起来仍是自然语言
export function chipHTML(u, label) {
  const src = u.type !== 'text' ? mainSrc(u) : null
  const thumb = src ? `<img src="${src}" alt="" draggable="false">` : `<span class="ti">${ic(TYPE_ICON[u.type])}</span>`
  return `<span class="mention" contenteditable="false" data-ref="${u.id}" data-label="${esc(label)}">${thumb}<span class="m-label">${esc(u.title)}</span></span>`
}

// 字符串 → 编辑器 HTML。已经断开的引用不再显示，下次输入时随序列化一起清掉
export function toHTML(prompt, nodeId) {
  const labels = refLabels(nodeId)
  let out = '', last = 0
  for (const m of (prompt ?? '').matchAll(MENTION)) {
    out += esc(prompt.slice(last, m.index))
    const u = labels.has(m[1]) && byId(m[1])
    if (u) out += chipHTML(u, labels.get(u.id))
    last = m.index + m[0].length
  }
  return (out + esc((prompt ?? '').slice(last))).replace(/\n/g, '<br>')
}

// 编辑器 DOM → 字符串
export function fromDOM(el) {
  let s = ''
  const walk = (node) => {
    for (const c of node.childNodes) {
      if (c.nodeType === Node.TEXT_NODE) s += c.nodeValue
      else if (c.dataset?.ref) s += `@[${c.dataset.ref}]`
      else if (c.tagName === 'BR') s += '\n'
      else {
        // Chrome 换行时偶尔会包一层 div
        if (/^(DIV|P)$/.test(c.tagName) && s && !s.endsWith('\n')) s += '\n'
        walk(c)
      }
    }
  }
  walk(el)
  return s.replace(/\u00a0/g, ' ').replace(/\n$/, '')
}

// 光标前正在输入的 @查询：返回 { query, range }，range 覆盖 @ 与已输入的查询词
export function mentionQuery(el) {
  const sel = getSelection()
  if (!sel.rangeCount || !sel.isCollapsed) return null
  const { startContainer: node, startOffset: offset } = sel.getRangeAt(0)
  if (node.nodeType !== Node.TEXT_NODE || !el.contains(node)) return null
  // 中文输入法下 @ 可能是全角 ＠
  const m = node.nodeValue.slice(0, offset).match(/(?:^|[\s ])([@＠])([^\s @＠]{0,12})$/)
  if (!m) return null
  const range = document.createRange()
  range.setStart(node, offset - m[2].length - 1)
  range.setEnd(node, offset)
  return { query: m[2], range }
}

// 用 chip 替换 range（没有 range 时插在光标处），后面补一个空格并把光标放到空格后
export function insertChip(el, u, label, range) {
  el.focus()
  const sel = getSelection()
  if (!range) {
    range = sel.rangeCount && el.contains(sel.getRangeAt(0).startContainer) ? sel.getRangeAt(0) : null
    if (!range) { range = document.createRange(); range.selectNodeContents(el); range.collapse(false) }
  }
  range.deleteContents()
  // 和前后文字之间各留一个空格，已有空白时不重复
  const probe = document.createRange()
  probe.selectNodeContents(el)
  probe.setEnd(range.startContainer, range.startOffset)
  const prev = probe.toString().slice(-1)
  probe.selectNodeContents(el)
  probe.setStart(range.endContainer, range.endOffset)
  const next = probe.toString().slice(0, 1)
  const tpl = document.createElement('template')
  tpl.innerHTML = chipHTML(u, label)
  const chip = tpl.content.firstChild
  const space = document.createTextNode(/\s/.test(next) ? '' : ' ')
  range.insertNode(space)
  range.insertNode(chip)
  if (prev && !/\s/.test(prev)) chip.before(document.createTextNode(' '))
  const after = document.createRange()
  if (space.length) after.setStart(space, 1)
  else after.setStartAfter(space)
  after.collapse(true)
  sel.removeAllRanges()
  sel.addRange(after)
}

export function caretToEnd(el) {
  el.focus()
  const r = document.createRange()
  r.selectNodeContents(el)
  r.collapse(false)
  getSelection().removeAllRanges()
  getSelection().addRange(r)
}
