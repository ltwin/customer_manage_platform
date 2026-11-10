import {
  forwardRef,
  useEffect,
  useId,
  useImperativeHandle,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type KeyboardEvent,
} from 'react'
import { createPortal } from 'react-dom'
import { ChevronRight, Library } from 'lucide-react'
import { MediaFigure } from './MediaView.tsx'
import { contentText } from './content.ts'
import {
  cleanMentions,
  limitPrompt,
  type PromptReference,
  type SceneRefDraft,
} from './promptReferences.ts'
import {
  mentionIcon,
  mentionQuery,
  normalizedCaret,
  pendingChip,
  promptCaret,
  promptChip,
  readPromptDOM,
  renderPromptDOM,
  restorePromptCaret,
} from './promptDOM.ts'
import { ScenePicker, type SceneObjectCache } from './ScenePicker.tsx'

export type PromptEditorHandle = {
  insertReferences: (ids: string[]) => void
  openPicker: () => void
  /** 聚焦输入框，光标放到末尾（新建或双击空媒体后，PRD CV-53/54）。 */
  focus: () => void
  /**
   * 在光标处放一个置灰的待选引用，已有待选的就替换它；null 移除。
   * 摄影师正在输入（输入框有焦点）时不动。返回之后是否有待选引用。
   */
  setPending: (id: string | null) => boolean
  /** 把待选引用转为正式引用，返回转正后的正文；没有待选就返回当前正文。 */
  commitPending: () => string
}
// 第一级菜单里的场景：画布上的场景节点。选中后展开物件列表。
export type SceneMenuItem = {
  documentID: string
  title: string
  disabled?: boolean
}
type MenuItem = {
  key: string
  title: string
  kind: string
  label: string
  ref?: PromptReference
  scene?: SceneMenuItem
  disabled?: boolean
}
// 菜单按类型分组，组内画布上选中的排前面。
const groups: { name: string; kinds: (kind: string) => boolean }[] = [
  { name: '场景', kinds: (kind) => kind === 'scene' },
  { name: '图片', kinds: (kind) => kind === 'image' },
  { name: '文本', kinds: (kind) => kind === 'text' },
  {
    name: '其他',
    kinds: (kind) => !['scene', 'image', 'text'].includes(kind),
  },
]
type Expanded = {
  documentID: string
  title: string
  initial: string[]
  // 从第一级菜单展开的才能返回；点正文里的场景标签打开的没有上一级。
  fromMenu: boolean
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
  /** 外层的浮动菜单（如斜杠菜单）先看按键；返回 true 表示已处理。 */
  onKey?: (event: KeyboardEvent<HTMLDivElement>) => boolean
  /** 第一级菜单里列出的场景；不传就没有场景组。 */
  scenes?: SceneMenuItem[]
  /** 选好场景和物件后由外层登记成场景引用，返回它的本机键。 */
  onSceneRef?: (ref: SceneRefDraft) => string
  /** 画布上当前选中的菜单项（sourceID 或 scene:<documentID>），排前并标注。 */
  highlighted?: ReadonlySet<string>
  /** 待选引用在输入框获得焦点时转正了。 */
  onPendingCommit?: () => void
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
    onKey,
    scenes,
    onSceneRef,
    highlighted,
    onPendingCommit,
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
  const [expanded, setExpanded] = useState<Expanded | null>(null)
  const search = useRef<HTMLInputElement>(null)
  const [focusSearch, setFocusSearch] = useState(false)
  const sceneCache = useRef<SceneObjectCache>(new Map())
  // 场景引用等外层登记好、出现在 references 里之后再插入。
  const awaiting = useRef<string | null>(null)
  // 聚焦时转正的待选引用：点击落下光标后再把光标移回它后面。
  const committedAt = useRef<Range | null>(null)
  const listID = useId()
  const needle = (query ?? '').trim().toLowerCase()
  const menu = [
    ...(scenes ?? []).map((scene): MenuItem => ({
      key: `scene:${scene.documentID}`,
      title: scene.title || '未命名场景',
      kind: 'scene',
      label: '场景',
      scene,
      disabled: scene.disabled,
    })),
    ...references
      .filter((r) => !r.scene && r.sourceID)
      .map((r): MenuItem => ({
        key: r.sourceID!,
        title: r.title,
        kind: r.kind,
        label: r.label,
        ref: r,
      })),
  ].filter(
    (item) =>
      !needle ||
      `${item.title} ${item.label}`.toLowerCase().includes(needle) ||
      (item.kind === 'text' &&
        contentText(item.ref?.content?.payload)
          .slice(0, 200)
          .toLowerCase()
          .includes(needle)),
  )
  const sections = groups
    .map((group) => ({
      name: group.name,
      items: menu
        .filter((item) => group.kinds(item.kind))
        .sort(
          (a, b) =>
            Number(!!highlighted?.has(b.key)) -
            Number(!!highlighted?.has(a.key)),
        ),
    }))
    .filter((section) => section.items.length)
  const items = sections.flatMap((section) => section.items)
  const active = Math.min(index, Math.max(0, items.length - 1))
  const ids = useMemo(
    () => new Set(references.flatMap((r) => (r.sourceID ? [r.sourceID] : []))),
    [references],
  )
  const close = () => {
    setQuery(null)
    setExpanded(null)
    queryRange.current = null
    sceneCache.current = new Map()
  }
  function rememberCaret() {
    const selection = window.getSelection()
    if (selection?.rangeCount && editor.current?.contains(selection.anchorNode))
      caret.current = selection.getRangeAt(0).cloneRange()
  }
  function persist(): string {
    if (!editor.current || composing.current) return readPromptDOMOf()
    const el = editor.current
    const before = readPromptDOM(el)
    const value = limitPrompt(cleanMentions(before, ids))
    if (before !== value) {
      const offset = promptCaret(el)
      renderPromptDOM(el, value, references, thumbnails)
      restorePromptCaret(el, Math.min(value.length, offset ?? value.length))
    }
    if (!value && !el.querySelector('[data-pending]')) el.replaceChildren()
    onChange(value)
    rememberCaret()
    return value
  }
  function readPromptDOMOf(): string {
    return editor.current ? readPromptDOM(editor.current) : ''
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
      y: Math.max(8, Math.min(base.bottom + 6, window.innerHeight - 420)),
    })
  }
  function insertReferences(values: string[]) {
    const el = editor.current
    if (!el) return
    el.focus()
    committedAt.current = null
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
  function choose(item: MenuItem, expand = false) {
    if (item.disabled) return
    if (!item.scene) {
      insertReferences([item.key])
      return
    }
    if (expand)
      setExpanded({
        documentID: item.scene.documentID,
        title: item.scene.title,
        initial: [],
        fromMenu: true,
      })
    else
      insertScene({
        documentID: item.scene.documentID,
        title: item.scene.title,
        objectIDs: [],
        names: [],
      })
  }
  function insertScene(scene: SceneRefDraft) {
    if (!onSceneRef) return
    awaiting.current = onSceneRef(scene)
  }
  useEffect(() => {
    const key = awaiting.current
    if (key && references.some((r) => r.sourceID === key)) {
      awaiting.current = null
      insertReferences([key])
    }
    // insertReferences reads this render's references, which now hold the key.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [references])
  // 点正文里的场景标签：打开它的物件列表，确认后用新引用换掉它（换的是新键，撤销回到旧的）。
  function editScene(chip: HTMLElement, scene: SceneRefDraft) {
    const range = document.createRange()
    range.selectNode(chip)
    const rect = chip.getBoundingClientRect()
    queryRange.current = range
    setQuery('')
    setIndex(0)
    setAnchor({
      x: Math.max(8, Math.min(rect.left, window.innerWidth - 280)),
      y: Math.max(8, Math.min(rect.bottom + 6, window.innerHeight - 420)),
    })
    setExpanded({
      documentID: scene.documentID,
      title: scene.title,
      initial: scene.objectIDs,
      fromMenu: false,
    })
  }
  // 搜索框和正文共用：↑↓ 移动，回车插入，→ 展开场景，Esc 关闭。
  function onPickerKey(e: KeyboardEvent<HTMLElement>): boolean {
    if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
      e.preventDefault()
      if (items.length)
        setIndex(
          (active + (e.key === 'ArrowDown' ? 1 : items.length - 1)) %
            items.length,
        )
      return true
    }
    if ((e.key === 'Enter' || e.key === 'Tab') && !e.shiftKey && items.length) {
      e.preventDefault()
      choose(items[active]!)
      return true
    }
    if (e.key === 'ArrowRight' && items[active]?.scene) {
      e.preventDefault()
      choose(items[active]!, true)
      return true
    }
    if (e.key === 'Escape') {
      e.preventDefault()
      close()
      editor.current?.focus()
      return true
    }
    return false
  }
  useEffect(() => {
    if (!focusSearch || query === null) return
    setFocusSearch(false)
    search.current?.focus()
  }, [focusSearch, query])
  function setPending(id: string | null): boolean {
    const el = editor.current
    if (!el) return false
    const old = el.querySelector<HTMLElement>('[data-pending]')
    if (document.activeElement === el) return !!old
    const item = id ? references.find((r) => r.sourceID === id) : undefined
    if (!item) {
      old?.remove()
      return false
    }
    const chip = pendingChip(item, thumbnails[item.content?.id ?? ''])
    if (old) {
      old.replaceWith(chip)
      return true
    }
    const range = caret.current
    if (
      range &&
      el.contains(range.startContainer) &&
      el.contains(range.endContainer)
    ) {
      const at = range.cloneRange()
      at.collapse(false)
      at.insertNode(chip)
      const after = document.createRange()
      after.setStartAfter(chip)
      caret.current = after
    } else el.append(chip)
    return true
  }
  function commitPending(): string {
    const el = editor.current
    if (!el) return ''
    const old = el.querySelector<HTMLElement>('[data-pending]')
    if (!old) return readPromptDOM(el)
    const item = references.find((r) => r.sourceID === old.dataset.pending)
    if (!item) {
      old.remove()
      return readPromptDOM(el)
    }
    const chip = promptChip(item, thumbnails[item.content?.id ?? ''])
    if (document.activeElement === el) {
      // 原生编辑命令进撤销栈：之后按撤销先撤掉这个引用。
      const range = document.createRange()
      range.selectNode(old)
      const selection = window.getSelection()
      selection?.removeAllRanges()
      selection?.addRange(range)
      const box = document.createElement('span')
      box.append(chip, document.createTextNode(' '))
      document.execCommand('insertHTML', false, box.innerHTML)
      committedAt.current = selection?.rangeCount
        ? selection.getRangeAt(0).cloneRange()
        : null
    } else old.replaceWith(chip, document.createTextNode(' '))
    const value = persist()
    onPendingCommit?.()
    return value
  }
  useImperativeHandle(ref, () => ({
    insertReferences,
    openPicker: () => {
      editor.current?.focus()
      committedAt.current = null
      picker(null, '')
      setFocusSearch(true)
    },
    setPending,
    commitPending,
    focus: () => {
      const el = editor.current
      if (!el) return
      el.focus()
      committedAt.current = null
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
        onFocus={() => {
          if (editor.current?.querySelector('[data-pending]')) commitPending()
        }}
        onMouseUp={() => {
          const at = committedAt.current
          committedAt.current = null
          if (!at || !editor.current?.contains(at.startContainer)) return
          const selection = window.getSelection()
          selection?.removeAllRanges()
          selection?.addRange(at)
          rememberCaret()
        }}
        onClick={(e) => {
          if (disabled || !(e.target instanceof Element)) return
          const chip = e.target.closest<HTMLElement>('[data-ref]')
          const scene = references.find(
            (r) => r.sourceID === chip?.dataset.ref,
          )?.scene
          if (chip && scene && onSceneRef) editScene(chip, scene)
        }}
        onBlur={(e) => {
          rememberCaret()
          setPeek(null)
          // 焦点进了菜单里的搜索框：菜单留着，插入位置也留着。
          if (
            e.relatedTarget instanceof Element &&
            e.relatedTarget.closest('.cc-prompt-picker')
          )
            return
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
          if (query === null && onKey?.(e)) return
          if (query !== null && !expanded) {
            if (onPickerKey(e)) return
            if (e.key === 'Enter' && !e.shiftKey) {
              e.preventDefault()
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
            // 点菜单不抢走正文的焦点；搜索框要能点进去。
            onMouseDown={(e) => {
              if (!(e.target instanceof HTMLInputElement)) e.preventDefault()
            }}
          >
            {expanded ? (
              <ScenePicker
                key={expanded.documentID}
                documentID={expanded.documentID}
                title={expanded.title}
                initial={expanded.initial}
                cache={sceneCache.current}
                onBack={
                  expanded.fromMenu
                    ? () => {
                        setExpanded(null)
                        setFocusSearch(true)
                      }
                    : undefined
                }
                onCancel={() => {
                  close()
                  editor.current?.focus()
                }}
                onConfirm={insertScene}
              />
            ) : (
              <>
                <div className="cc-prompt-picker-title">
                  {referenceTitle ??
                    (references.length ? '已连接的参考' : '还没有连接参考')}
                </div>
                <input
                  ref={search}
                  type="search"
                  className="cc-prompt-search"
                  aria-label="搜索可引用的节点"
                  aria-controls={listID}
                  placeholder="搜索标题、类型或文本内容"
                  value={query ?? ''}
                  onChange={(e) => {
                    setQuery(e.target.value)
                    setIndex(0)
                  }}
                  onKeyDown={(e) => {
                    e.stopPropagation()
                    if (e.nativeEvent.isComposing) return
                    if (!onPickerKey(e) && e.key === 'Enter') e.preventDefault()
                  }}
                />
                <div
                  id={listID}
                  role="listbox"
                  aria-label="引用参考"
                  className="cc-prompt-options"
                >
                  {sections.map((section) => (
                    <div role="group" aria-label={section.name} key={section.name}>
                      <div className="cc-prompt-group" aria-hidden="true">
                        {section.name}
                        <small>{section.items.length}</small>
                      </div>
                      {section.items.map((item) => {
                        const i = items.indexOf(item)
                        const thumb = thumbnails[item.ref?.content?.id ?? '']
                        return (
                          <button
                            type="button"
                            role="option"
                            id={`${listID}-${i}`}
                            aria-selected={i === active}
                            aria-disabled={item.disabled || undefined}
                            key={item.key}
                            className={i === active ? 'is-on' : ''}
                            title={
                              item.disabled
                                ? '一条消息最多引用 5 个场景'
                                : undefined
                            }
                            onMouseMove={() => setIndex(i)}
                            onClick={() => choose(item, true)}
                          >
                            {thumb && item.kind === 'image' ? (
                              <img src={thumb} alt="" />
                            ) : (
                              <span className="cc-mention-icon">
                                {mentionIcon(item.kind)}
                              </span>
                            )}
                            <span>{item.title}</span>
                            {highlighted?.has(item.key) && <em>已选中</em>}
                            <small>{item.label}</small>
                            {item.scene && <ChevronRight size={14} aria-hidden="true" />}
                          </button>
                        )
                      })}
                    </div>
                  ))}
                </div>
                {!items.length && (
                  <p>
                    {needle
                      ? '没有匹配的节点'
                      : references.length || scenes?.length
                        ? '没有可引用的节点'
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
              </>
            )}
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
            {preview.scene ? (
              <p>
                {preview.scene.objectIDs.length
                  ? preview.scene.objectIDs
                      .map((_, i) => preview.scene!.names[i] || '未命名')
                      .join('、')
                  : '整个场景'}
              </p>
            ) : preview.content && preview.kind !== 'text' ? (
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
                    : preview.scene
                      ? '场景引用 · 点击修改物件'
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
