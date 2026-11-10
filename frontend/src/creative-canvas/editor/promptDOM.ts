import {
  cleanMentions,
  mentionToken,
  type PromptReference,
} from './promptReferences.ts'

// 这棵子树由浏览器编辑器管理，React 不渲染它的子节点。
// 待选引用（data-pending）不是正文：不读、不存、不发送。
export function readPromptDOM(root: Node): string {
  let text = ''
  for (const child of Array.from(root.childNodes)) {
    if (child.nodeType === Node.TEXT_NODE) text += child.textContent ?? ''
    else if (child instanceof HTMLElement) {
      if (child.dataset.pending !== undefined) continue
      if (child.dataset.ref) text += mentionToken(child.dataset.ref)
      else if (child.tagName === 'BR') text += '\n'
      else {
        if (/^(DIV|P)$/.test(child.tagName) && text && !text.endsWith('\n'))
          text += '\n'
        text += readPromptDOM(child)
      }
    }
  }
  return text.replace(/\u00a0/g, ' ')
}

export function promptChip(
  ref: PromptReference,
  url?: string,
): HTMLSpanElement {
  const chip = document.createElement('span')
  chip.className = 'cc-prompt-mention'
  chip.contentEditable = 'false'
  chip.dataset.ref = ref.sourceID ?? ''
  chip.dataset.label = ref.label
  chip.title =
    ref.state === 'uploading'
      ? '上传中'
      : ref.state === 'failed'
        ? '上传失败'
        : ref.scene
          ? sceneChipTitle(ref)
          : ref.kind === 'text'
            ? '文本参考 · 引用处展开为原文'
            : `发送时记为 [${ref.label}]`
  if (ref.state) chip.classList.add(`is-${ref.state}`)
  const src = url || ref.thumbnail
  if (ref.state === 'uploading') {
    const spin = document.createElement('span')
    spin.className = 'cc-mention-spin'
    chip.append(spin)
  } else if (src && ref.kind === 'image') {
    const img = document.createElement('img')
    img.src = src
    img.alt = ''
    img.draggable = false
    chip.append(img)
  } else {
    const icon = document.createElement('span')
    icon.className = 'cc-mention-icon'
    icon.textContent = mentionIcon(ref.kind)
    chip.append(icon)
  }
  const label = document.createElement('span')
  label.className = 'cc-mention-title'
  label.textContent = ref.title
  chip.append(label)
  return chip
}

export function mentionIcon(kind: string): string {
  return kind === 'text'
    ? 'T'
    : kind === 'video'
      ? '▷'
      : kind === 'scene'
        ? '◫'
        : '▧'
}

function sceneChipTitle(ref: PromptReference): string {
  const scene = ref.scene!
  const names = scene.objectIDs.map((_, i) => scene.names[i] || '未命名')
  return names.length
    ? `${scene.title || '未命名场景'}：${names.join('、')} · 点击修改物件`
    : `整个场景：${scene.title || '未命名场景'} · 点击选择物件`
}

// 置灰的待选引用：点进输入框后才转为正式引用。没有 data-ref，所以不进正文。
export function pendingChip(ref: PromptReference, url?: string): HTMLSpanElement {
  const chip = promptChip(ref, url)
  delete chip.dataset.ref
  chip.dataset.pending = ref.sourceID ?? ''
  chip.classList.add('is-pending')
  chip.title = '待选引用 · 点进输入框后加入'
  return chip
}

export function renderPromptDOM(
  root: HTMLElement,
  text: string,
  refs: PromptReference[],
  urls: Record<string, string>,
) {
  const fragment = document.createDocumentFragment()
  const byID = new Map(refs.map((r) => [r.sourceID, r]))
  let last = 0
  for (const match of text.matchAll(/@\[([\w-]+)\]/g)) {
    fragment.append(document.createTextNode(text.slice(last, match.index)))
    const ref = byID.get(match[1]!)
    if (ref) fragment.append(promptChip(ref, urls[ref.content?.id ?? '']))
    last = match.index + match[0].length
  }
  fragment.append(document.createTextNode(text.slice(last)))
  root.replaceChildren(fragment)
}

export function mentionQuery(
  root: HTMLElement,
): { query: string; range: Range } | null {
  const selection = window.getSelection()
  if (!selection?.rangeCount || !selection.isCollapsed) return null
  const range = selection.getRangeAt(0)
  if (
    !root.contains(range.startContainer) ||
    range.startContainer.nodeType !== Node.TEXT_NODE
  )
    return null
  const before = (range.startContainer.textContent ?? '').slice(
    0,
    range.startOffset,
  )
  const match = before.match(/(?:^|\s)[@＠]([^\s@＠]{0,64})$/)
  if (!match) return null
  const queryRange = range.cloneRange()
  queryRange.setStart(
    range.startContainer,
    range.startOffset - match[1]!.length - 1,
  )
  return { query: match[1]!, range: queryRange }
}

export function promptCaret(root: HTMLElement): number | null {
  const selection = window.getSelection()
  if (!selection?.rangeCount || !root.contains(selection.anchorNode))
    return null
  const before = selection.getRangeAt(0).cloneRange()
  before.selectNodeContents(root)
  before.setEnd(selection.anchorNode!, selection.anchorOffset)
  return readPromptDOM(before.cloneContents()).length
}

export function restorePromptCaret(root: HTMLElement, offset: number) {
  const range = document.createRange()
  let found = false
  const walk = (parent: Node) => {
    for (const child of Array.from(parent.childNodes)) {
      if (found) return
      if (child instanceof HTMLElement && child.dataset.pending !== undefined)
        continue
      const atomic = child instanceof HTMLElement && !!child.dataset.ref
      const length = atomic
        ? mentionToken(child.dataset.ref!).length
        : child.nodeType === Node.TEXT_NODE
          ? (child.textContent ?? '').length
          : child instanceof HTMLElement && child.tagName === 'BR'
            ? 1
            : 0
      if (atomic || child.nodeType === Node.TEXT_NODE || length) {
        if (offset <= length) {
          if (atomic || child.nodeType !== Node.TEXT_NODE) {
            if (offset) range.setStartAfter(child)
            else range.setStartBefore(child)
          } else range.setStart(child, offset)
          found = true
          return
        }
        offset -= length
      } else walk(child)
    }
  }
  walk(root)
  if (!found) {
    range.selectNodeContents(root)
    range.collapse(false)
  }
  range.collapse(true)
  const selection = window.getSelection()
  selection?.removeAllRanges()
  selection?.addRange(range)
}

export function normalizedCaret(
  text: string,
  offset: number,
  ids: ReadonlySet<string>,
) {
  return cleanMentions(text.slice(0, offset), ids).length
}
