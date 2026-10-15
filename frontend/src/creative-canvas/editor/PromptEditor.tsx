import {
  forwardRef,
  useEffect,
  useId,
  useImperativeHandle,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
} from 'react'
import { createPortal } from 'react-dom'
import { Library } from 'lucide-react'
import { MediaFigure } from './MediaView.tsx'
import { contentText } from './content.ts'
import {
  cleanMentions,
  limitPrompt,
  type PromptReference,
} from './promptReferences.ts'
import {
  mentionQuery,
  normalizedCaret,
  promptCaret,
  promptChip,
  readPromptDOM,
  renderPromptDOM,
  restorePromptCaret,
} from './promptDOM.ts'

export type PromptEditorHandle = {
  insertReferences: (ids: string[]) => void
  openPicker: () => void
  /** 聚焦输入框，光标放到末尾（新建或双击空媒体后，PRD CV-53/54）。 */
  focus: () => void
}
type Props = {
  text: string
  label?: string
  placeholder?: string
  referenceTitle?: string
  references: PromptReference[]
  thumbnails: Record<string, string>
  disabled: boolean
  onChange: (text: string) => void
  onGenerate: () => void
  onFiles?: (files: File[]) => void
  onLibrary: () => void
}

export default forwardRef<PromptEditorHandle, Props>(function PromptEditor(
  {
    text,
    references,
    thumbnails,
    disabled,
    onChange,
    onGenerate,
    onLibrary,
    onFiles,
    referenceTitle,
    label = '提示词',
    placeholder = '描述你想创作的内容，@ 引用已连接的参考…',
  },
  ref,
) {
  const editor = useRef<HTMLDivElement>(null)
  const received = useRef<string | null>(null)
  const caret = useRef<Range | null>(null)
  const queryRange = useRef<Range | null>(null)
  const composing = useRef(false)
  const [query, setQuery] = useState<string | null>(null)
  const [index, setIndex] = useState(0)
  const [anchor, setAnchor] = useState<{ x: number; y: number } | null>(null)
  const [peek, setPeek] = useState<{ id: string; x: number; y: number } | null>(
    null,
  )
  const listID = useId()
  const items = references.filter(
    (r) =>
      !query ||
      `${r.title} ${r.label}`.toLowerCase().includes(query.toLowerCase()),
  )
  const active = Math.min(index, Math.max(0, items.length - 1))
  const ids = useMemo(
    () => new Set(references.flatMap((r) => (r.sourceID ? [r.sourceID] : []))),
    [references],
  )
  const close = () => {
    setQuery(null)
    queryRange.current = null
  }
  function rememberCaret() {
    const selection = window.getSelection()
    if (selection?.rangeCount && editor.current?.contains(selection.anchorNode))
      caret.current = selection.getRangeAt(0).cloneRange()
  }
  function persist() {
    if (!editor.current || composing.current) return
    const el = editor.current
    const before = readPromptDOM(el)
    const value = limitPrompt(cleanMentions(before, ids))
    if (before !== value) {
      const offset = promptCaret(el)
      renderPromptDOM(el, value, references, thumbnails)
      restorePromptCaret(el, Math.min(value.length, offset ?? value.length))
    }
    if (!value) el.replaceChildren()
    onChange(value)
    rememberCaret()
  }
  function picker(range: Range | null, value: string) {
    if (!editor.current) return
    const rect = range?.getBoundingClientRect()
    const base = rect?.height ? rect : editor.current.getBoundingClientRect()
    queryRange.current = range
    setQuery(value)
    setIndex(0)
    setAnchor({
      x: Math.max(8, Math.min(base.left, window.innerWidth - 280)),
      y: Math.max(8, Math.min(base.bottom + 6, window.innerHeight - 340)),
    })
  }
  function insertReferences(values: string[]) {
    const el = editor.current
    if (!el) return
    el.focus()
    const range = queryRange.current ?? caret.current
    if (
      range &&
      el.contains(range.startContainer) &&
      el.contains(range.endContainer)
    ) {
      const selection = window.getSelection()
      selection?.removeAllRanges()
      selection?.addRange(range)
    } else restorePromptCaret(el, readPromptDOM(el).length)
    const fragment = document.createElement('span')
    for (const id of values) {
      const item = references.find((r) => r.sourceID === id)
      if (!item) continue
      fragment.append(
        promptChip(item, thumbnails[item.content?.id ?? '']),
        document.createTextNode(' '),
      )
    }
    if (fragment.hasChildNodes()) {
      // 原生编辑命令保留浏览器的文字撤销栈；片段只由受信 DOM 构造器产生。
      document.execCommand('insertHTML', false, fragment.innerHTML)
      persist()
    }
    close()
    setPeek(null)
  }
  useImperativeHandle(ref, () => ({
    insertReferences,
    openPicker: () => {
      editor.current?.focus()
      picker(null, '')
    },
    focus: () => {
      const el = editor.current
      if (!el) return
      el.focus()
      const range = document.createRange()
      range.selectNodeContents(el)
      range.collapse(false)
      const selection = window.getSelection()
      selection?.removeAllRanges()
      selection?.addRange(range)
    },
  }))
  useLayoutEffect(() => {
    const el = editor.current
    if (!el || composing.current) return
    const before = readPromptDOM(el)
    if (received.current !== text && before !== text) {
      const offset = document.activeElement === el ? promptCaret(el) : null
      renderPromptDOM(el, text, references, thumbnails)
      if (offset !== null)
        restorePromptCaret(el, normalizedCaret(before, offset, ids))
    } else {
      for (const chip of Array.from(
        el.querySelectorAll<HTMLElement>('[data-ref]'),
      )) {
        const item = references.find((r) => r.sourceID === chip.dataset.ref)
        if (!item) continue
        const next = promptChip(item, thumbnails[item.content?.id ?? ''])
        if (chip.innerHTML !== next.innerHTML)
          chip.replaceChildren(...Array.from(next.childNodes))
        chip.className = next.className
        chip.title = next.title
        chip.dataset.label = item.label
      }
    }
    received.current = text
  }, [text, references, thumbnails, ids])
  useEffect(() => {
    if (query === null) return
    const dismiss = (e: PointerEvent) => {
      if (
        e.target instanceof Element &&
        !e.target.closest('.cc-prompt-picker') &&
        !editor.current?.contains(e.target)
      )
        close()
    }
    window.addEventListener('pointerdown', dismiss)
    return () => window.removeEventListener('pointerdown', dismiss)
  }, [query])
  const preview = references.find((r) => r.sourceID === peek?.id)
  return (
    <>
      <div
        ref={editor}
        className="cc-generation-prompt cc-prompt-editor"
        role="textbox"
        aria-label={label}
        aria-multiline="true"
        aria-disabled={disabled}
        aria-expanded={query !== null}
        aria-controls={query !== null ? listID : undefined}
        aria-activedescendant={
          query !== null && items.length ? `${listID}-${active}` : undefined
        }
        contentEditable={!disabled}
        suppressContentEditableWarning
        data-placeholder={placeholder}
        tabIndex={0}
        onInput={() => {
          persist()
          if (!composing.current && editor.current) {
            const q = mentionQuery(editor.current)
            if (q) picker(q.range, q.query)
            else close()
          }
        }}
        onSelect={rememberCaret}
        onCompositionStart={() => {
          composing.current = true
          close()
        }}
        onCompositionEnd={() => {
          composing.current = false
          persist()
          if (editor.current) {
            const q = mentionQuery(editor.current)
            if (q) picker(q.range, q.query)
          }
        }}
        onCopy={(e) => {
          const selection = window.getSelection()
          if (!selection?.rangeCount || selection.isCollapsed) return
          e.preventDefault()
          e.clipboardData.setData(
            'text/plain',
            readPromptDOM(selection.getRangeAt(0).cloneContents()),
          )
        }}
        onCut={(e) => {
          const selection = window.getSelection()
          if (!selection?.rangeCount || selection.isCollapsed) return
          e.preventDefault()
          e.clipboardData.setData(
            'text/plain',
            readPromptDOM(selection.getRangeAt(0).cloneContents()),
          )
          document.execCommand('delete')
          persist()
        }}
        onPaste={(e) => {
          e.preventDefault()
          if (disabled) return
          const files = Array.from(e.clipboardData.files)
          if (files.length && onFiles) { onFiles(files); return }
          const fragment = document.createElement('div')
          renderPromptDOM(
            fragment,
            cleanMentions(e.clipboardData.getData('text/plain'), ids),
            references,
            thumbnails,
          )
          document.execCommand('insertHTML', false, fragment.innerHTML)
          persist()
        }}
        onBlur={() => {
          rememberCaret()
          setPeek(null)
          close()
        }}
        onPointerOver={(e) => {
          if (!(e.target instanceof Element)) return
          const chip = e.target.closest<HTMLElement>('[data-ref]')
          if (chip?.dataset.ref) {
            const r = chip.getBoundingClientRect()
            setPeek({
              id: chip.dataset.ref,
              x: Math.max(8, Math.min(r.left, window.innerWidth - 216)),
              y: r.top,
            })
          }
        }}
        onPointerOut={(e) => {
          if (e.target instanceof Element && e.target.closest('[data-ref]'))
            setPeek(null)
        }}
        onKeyDown={(e) => {
          e.stopPropagation()
          if (e.nativeEvent.isComposing || composing.current) return
          if (query !== null) {
            if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
              e.preventDefault()
              if (items.length)
                setIndex(
                  (active + (e.key === 'ArrowDown' ? 1 : items.length - 1)) %
                    items.length,
                )
              return
            }
            if (
              (e.key === 'Enter' || e.key === 'Tab') &&
              !e.shiftKey &&
              items.length
            ) {
              e.preventDefault()
              insertReferences([items[active]!.sourceID!])
              return
            }
            if (e.key === 'Enter' && !e.shiftKey) {
              e.preventDefault()
              return
            }
            if (e.key === 'Escape') {
              e.preventDefault()
              close()
              return
            }
          }
          if (e.key === 'Escape') {
            setPeek(null)
            e.currentTarget.blur()
            return
          }
          if (e.key === 'Enter') {
            e.preventDefault()
            if (e.shiftKey) {
              document.execCommand('insertLineBreak')
              persist()
            } else if (!disabled) onGenerate()
          }
        }}
      />
      {query !== null &&
        anchor &&
        createPortal(
          <div
            className="cc-prompt-picker"
            style={{ left: anchor.x, top: anchor.y }}
            onMouseDown={(e) => e.preventDefault()}
          >
            <div className="cc-prompt-picker-title">
              {referenceTitle ??
                (references.length ? '已连接的参考' : '还没有连接参考')}
            </div>
            <div id={listID} role="listbox" aria-label="引用参考">
              {items.map((item, i) => (
                <button
                  type="button"
                  role="option"
                  id={`${listID}-${i}`}
                  aria-selected={i === active}
                  key={item.key}
                  className={i === active ? 'is-on' : ''}
                  onMouseMove={() => setIndex(i)}
                  onClick={() => insertReferences([item.sourceID!])}
                >
                  {thumbnails[item.content?.id ?? ''] &&
                  item.kind === 'image' ? (
                    <img src={thumbnails[item.content!.id]} alt="" />
                  ) : (
                    <span className="cc-mention-icon">
                      {item.kind === 'text' ? 'T' : '▧'}
                    </span>
                  )}
                  <span>{item.title}</span>
                  <small>{item.label}</small>
                </button>
              ))}
            </div>
            {!items.length && (
              <p>
                {references.length
                  ? '没有匹配的参考'
                  : '先连接画布上的节点，或从资产库添加参考'}
              </p>
            )}
            <button
              type="button"
              className="cc-prompt-library"
              onClick={() => {
                if (queryRange.current) {
                  editor.current?.focus()
                  const s = window.getSelection()
                  s?.removeAllRanges()
                  s?.addRange(queryRange.current)
                  document.execCommand('insertText', false, '')
                  persist()
                }
                close()
                onLibrary()
              }}
            >
              <Library size={14} />
              从资产库添加参考
            </button>
          </div>,
          document.querySelector('.cc-workspace') ?? document.body,
        )}
      {peek &&
        preview &&
        createPortal(
          <div
            className="cc-generation-ref-peek"
            role="tooltip"
            style={{
              left: peek.x,
              ...(peek.y > 310
                ? { bottom: window.innerHeight - peek.y + 8 }
                : { top: peek.y + 36 }),
            }}
          >
            {preview.content && preview.kind !== 'text' ? (
              <MediaFigure
                content={preview.content}
                kind={preview.kind}
                title={preview.title}
              />
            ) : preview.thumbnail ? (
              <img src={preview.thumbnail} alt={preview.title} />
            ) : (
              <p>
                {preview.content
                  ? contentText(preview.content.payload)
                  : '节点尚无内容'}
              </p>
            )}
            <div>
              <b>{preview.title}</b>
              <span>
                {preview.state === 'uploading'
                  ? '上传中，完成后才能发送'
                  : preview.state === 'failed'
                    ? '上传失败'
                    : preview.kind === 'text'
                      ? '文本参考 · 引用处展开为原文'
                      : `发送时记为 [${preview.label}]`}
              </span>
            </div>
          </div>,
          document.querySelector('.cc-workspace') ?? document.body,
        )}
    </>
  )
})
