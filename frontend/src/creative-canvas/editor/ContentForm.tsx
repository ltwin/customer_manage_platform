import { useId, useState, type ReactNode } from 'react'
import type { Draft } from './journal.ts'
import type { ContentDraft } from './api.ts'

import { payload } from './content.ts'

type Props = {
  draft: Draft
  onChange: (draft: Draft) => void
  onSave: (content: ContentDraft) => void
  disabled: boolean
  submitDisabled?: boolean
  label: string
  asset?: boolean
  extraFields?: ReactNode
}
export default function ContentForm({
  draft,
  onChange,
  onSave,
  disabled,
  submitDisabled = false,
  label,
  asset = false,
  extraFields,
}: Props) {
  const errorID = useId()
  const [error, setError] = useState('')
  const [composing, setComposing] = useState(false)
  function save() {
    setError('')
    if (composing || disabled || submitDisabled) return
    if (asset && !draft.title.trim()) {
      setError('请填写资产名称')
      return
    }
    const media = !['text', 'link'].includes(draft.kind)
    if (!media && !draft.value.trim()) {
      setError(draft.kind === 'text' ? '请填写正文' : '请填写链接')
      return
    }
    if (media && draft.value.length > 2000) {
      setError('说明最多 2000 字')
      return
    }
    if (draft.kind === 'link') {
      try {
        const u = new URL(draft.value)
        if (
          !['http:', 'https:'].includes(u.protocol) ||
          u.username ||
          u.password
        )
          throw new Error()
      } catch {
        setError('请输入不含账号口令的 HTTP 或 HTTPS 链接')
        return
      }
    }
    // Source declaration is reserved for later; the server records the
    // photographer's own work by default.
    onSave({ kind: draft.kind, payload: payload(draft.kind, draft.value) })
  }
  return (
    <form
      className="cc-form"
      noValidate
      onSubmit={(e) => {
        e.preventDefault()
        save()
      }}
    >
      {asset && (
        <>
          <label>
            资产名称
            <input
              className="input"
              value={draft.title}
              maxLength={200}
              disabled={disabled}
              onChange={(e) => onChange({ ...draft, title: e.target.value })}
            />
          </label>
          <label>
            内容类型
            <select
              className="input"
              value={draft.kind}
              disabled={disabled}
              onChange={(e) =>
                onChange({
                  ...draft,
                  kind: e.target.value === 'link' ? 'link' : 'text',
                  value: '',
                })
              }
            >
              <option value="text">文字</option>
              <option value="link">链接</option>
            </select>
          </label>
        </>
      )}
      <label>
        {draft.kind === 'text'
          ? '正文'
          : draft.kind === 'link'
            ? '链接地址'
            : '媒体说明'}
        <textarea
          className="input resize-none"
          style={{ resize: 'none' }}
          rows={asset ? 4 : draft.kind === 'text' ? 9 : 4}
          value={draft.value}
          maxLength={
            draft.kind === 'text' ? 100000 : draft.kind === 'link' ? 4096 : 2000
          }
          disabled={disabled}
          aria-label={
            draft.kind === 'text'
              ? '正文'
              : draft.kind === 'link'
                ? '链接地址'
                : '媒体说明'
          }
          aria-invalid={!!error}
          aria-describedby={errorID}
          onCompositionStart={() => setComposing(true)}
          onCompositionEnd={() => setComposing(false)}
          onChange={(e) => onChange({ ...draft, value: e.target.value })}
        />
      </label>
      {extraFields}
      <p id={errorID} className="cc-help" role={error ? 'alert' : undefined}>
        {error ||
          (draft.kind === 'link'
            ? '只保存链接，不抓取网页内容。'
            : !['text', 'link'].includes(draft.kind)
              ? '说明随媒体保存为新修订；媒体文件本身不变。'
              : '输入保存在本机；点击保存后才写入项目。')}
      </p>
      <button
        className="btn btn-primary"
        disabled={disabled || submitDisabled || composing}
        type="submit"
      >
        {label}
      </button>
    </form>
  )
}
