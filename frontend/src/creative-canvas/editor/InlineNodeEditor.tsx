import { useLayoutEffect, useRef, useState } from 'react'
import type { Draft } from './journal.ts'
import { validateContentValue } from './content.ts'

export function InlineNodeTitle({
  title,
  disabled,
  onSave,
  onClose,
}: {
  title: string
  disabled: boolean
  onSave: (title: string) => void
  onClose: () => void
}) {
  const [value, setValue] = useState(title)
  const titleWidth = Array.from(value).reduce(
    (width, char) => width + (char.charCodeAt(0) > 255 ? 2 : 1),
    2,
  )
  const input = useRef<HTMLInputElement>(null)
  const finished = useRef(false)
  useLayoutEffect(() => {
    input.current?.focus()
    input.current?.select()
  }, [])
  function finish(save: boolean) {
    if (finished.current) return
    finished.current = true
    if (save && !disabled && value.trim() !== title) onSave(value.trim())
    onClose()
  }
  return (
    <input
      ref={input}
      className="cc-inline-title nodrag nopan nowheel"
      aria-label="节点名称"
      value={value}
      maxLength={200}
      disabled={disabled}
      style={{ width: `${Math.max(4, titleWidth)}ch` }}
      onChange={(event) => setValue(event.target.value)}
      onPointerDown={(event) => event.stopPropagation()}
      onClick={(event) => event.stopPropagation()}
      onDoubleClick={(event) => event.stopPropagation()}
      onBlur={() => finish(true)}
      onKeyDown={(event) => {
        event.stopPropagation()
        if (event.nativeEvent.isComposing) return
        if (event.key === 'Enter' || event.key === 'Escape') {
          event.preventDefault()
          finish(event.key === 'Enter')
        }
      }}
    />
  )
}

export type InlineContentEditing = {
  nodeID: string
  draft: Draft | null
  disabled: boolean
  onChange: (draft: Draft) => void
  onSave: (draft: Draft) => void
  onCancel: (original: Draft) => void
}

export function InlineNodeText({
  draft,
  disabled,
  onChange,
  onSave,
  onCancel,
}: Omit<InlineContentEditing, 'nodeID' | 'draft'> & { draft: Draft }) {
  // Freeze the starting revision and value: remote updates must not replace
  // active input or bypass the workspace's conflict check when saving.
  const [original] = useState(draft)
  const [value, setValue] = useState(draft.value)
  const [error, setError] = useState('')
  const input = useRef<HTMLTextAreaElement>(null)
  const finished = useRef(false)
  useLayoutEffect(() => {
    input.current?.focus()
  }, [])
  function save() {
    if (finished.current || disabled) return
    if (value === original.value) {
      finished.current = true
      onCancel(original)
      return
    }
    const problem = validateContentValue(original.kind, value)
    if (problem) {
      setError(problem)
      return
    }
    finished.current = true
    onSave({ ...original, value })
    queueMicrotask(() => {
      finished.current = false
    })
  }
  return (
    <div className="cc-inline-content nodrag nopan nowheel">
      <textarea
        ref={input}
        className="cc-inline-text nodrag nopan nowheel"
        aria-label={original.kind === 'link' ? '链接地址' : '正文'}
        aria-invalid={!!error}
        placeholder="输入文字或粘贴链接"
        value={value}
        maxLength={original.kind === 'link' ? 4096 : 100000}
        disabled={disabled}
        onPointerDown={(event) => event.stopPropagation()}
        onClick={(event) => event.stopPropagation()}
        onDoubleClick={(event) => event.stopPropagation()}
        onChange={(event) => {
          setValue(event.target.value)
          setError('')
          onChange({ ...original, value: event.target.value })
        }}
        onBlur={save}
        onKeyDown={(event) => {
          event.stopPropagation()
          if (event.nativeEvent.isComposing) return
          if (event.key === 'Escape') {
            event.preventDefault()
            finished.current = true
            onCancel(original)
          } else if ((event.ctrlKey || event.metaKey) && event.key === 'Enter') {
            event.preventDefault()
            save()
          }
        }}
      />
      {error && (
        <span className="cc-inline-error" role="alert">{error}</span>
      )}
    </div>
  )
}
