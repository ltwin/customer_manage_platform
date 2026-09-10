import { useId, useState } from 'react'
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
  needsRights?: boolean
  asset?: boolean
}
export default function ContentForm({
  draft,
  onChange,
  onSave,
  disabled,
  submitDisabled = false,
  label,
  needsRights = true,
  asset = false,
}: Props) {
  const errorID = useId()
  const [source, setSource] = useState('')
  const [error, setError] = useState('')
  const [composing, setComposing] = useState(false)
  function save() {
    setError('')
    if (composing || disabled || submitDisabled) return
    if (asset && !draft.title.trim()) {
      setError('请填写资产名称')
      return
    }
    if (!draft.value.trim()) {
      setError(draft.kind === 'text' ? '请填写正文' : '请填写链接')
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
    if (needsRights && !source) {
      setError('请确认内容来源')
      return
    }
    onSave({
      kind: draft.kind,
      payload: payload(draft.kind, draft.value),
      rights:
        source === 'reference'
          ? { source_class: 'unknown_web', rights_basis: 'citation_or_display' }
          : {
              source_class: 'photographer_owned',
              rights_basis: 'ownership_attested',
            },
    })
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
        {draft.kind === 'text' ? '正文' : '链接地址'}
        <textarea
          className="input resize-none"
          style={{ resize: 'none' }}
          rows={asset ? 4 : 9}
          value={draft.value}
          maxLength={draft.kind === 'text' ? 100000 : 4096}
          disabled={disabled}
          aria-label={draft.kind === 'text' ? '正文' : '链接地址'}
          aria-invalid={!!error}
          aria-describedby={errorID}
          onCompositionStart={() => setComposing(true)}
          onCompositionEnd={() => setComposing(false)}
          onChange={(e) => onChange({ ...draft, value: e.target.value })}
        />
      </label>
      {needsRights && (
        <label>
          内容来源
          <select
            className="input"
            value={source}
            disabled={disabled}
            onChange={(e) => setSource(e.target.value)}
          >
            <option value="">请选择并确认</option>
            <option value="owned">本人创作，确认拥有权利</option>
            <option value="reference">网络引用，仅供展示参考</option>
          </select>
        </label>
      )}
      <p id={errorID} className="cc-help" role={error ? 'alert' : undefined}>
        {error ||
          (draft.kind === 'link'
            ? '只保存链接，不抓取网页内容。'
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
