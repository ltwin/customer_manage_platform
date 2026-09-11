import { useState } from 'react'
import StudioDialog from './StudioDialog.tsx'
import type { ContentRights } from './media.ts'

// One rights confirmation per batch; the choice mirrors ContentForm exactly.
export default function UploadDialog({
  files,
  destination,
  onConfirm,
  onCancel,
}: {
  files: File[]
  destination: string
  onConfirm: (rights: ContentRights) => void
  onCancel: () => void
}) {
  const [source, setSource] = useState('')
  const [error, setError] = useState('')
  return (
    <StudioDialog title="导入媒体" onClose={onCancel}>
      <form
        className="cc-form"
        noValidate
        onSubmit={(e) => {
          e.preventDefault()
          if (!source) {
            setError('请确认内容来源')
            return
          }
          onConfirm(
            source === 'reference'
              ? { source_class: 'unknown_web', rights_basis: 'citation_or_display' }
              : { source_class: 'photographer_owned', rights_basis: 'ownership_attested' },
          )
        }}
      >
        <p className="cc-muted">
          {files.length} 个文件将{destination}。文件在服务器完成校验后才可用。
        </p>
        <ul className="cc-upload-files">
          {files.slice(0, 8).map((f, i) => (
            <li key={`${f.name}:${i}`}>
              <span>{f.name}</span>
              <small>{Math.max(1, Math.round(f.size / 1024))} KB</small>
            </li>
          ))}
          {files.length > 8 && <li>…还有 {files.length - 8} 个</li>}
        </ul>
        <label>
          内容来源
          <select
            className="input"
            aria-label="内容来源"
            value={source}
            onChange={(e) => setSource(e.target.value)}
          >
            <option value="">请选择并确认</option>
            <option value="owned">本人创作，确认拥有权利</option>
            <option value="reference">网络引用，仅供展示参考</option>
          </select>
        </label>
        <p className="cc-help" role={error ? 'alert' : undefined}>
          {error || '同一批文件共用一次来源确认。'}
        </p>
        <button className="btn btn-primary" type="submit">
          开始上传
        </button>
      </form>
    </StudioDialog>
  )
}
