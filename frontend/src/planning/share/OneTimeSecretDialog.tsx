import { useState } from 'react'

type OneTimeSecretDialogProps = {
  title: string
  warning: string
  secretLabel: string
  secretValue: string
  onClose: () => void
}

export default function OneTimeSecretDialog({
  title,
  warning,
  secretLabel,
  secretValue,
  onClose,
}: OneTimeSecretDialogProps) {
  const [copied, setCopied] = useState(false)
  const [ack, setAck] = useState(false)

  async function copy() {
    try {
      await navigator.clipboard.writeText(secretValue)
      setCopied(true)
    } catch {
      setCopied(false)
    }
  }

  return (
    <div className="overlay open" role="presentation">
      <section className="dialog planning-dialog share-secret-dialog" role="dialog" aria-modal="true" aria-labelledby="shareSecretTitle">
        <h2 id="shareSecretTitle">{title}</h2>
        <p className="share-secret-warning" role="alert">{warning}</p>
        <label className="field">
          <span>{secretLabel}</span>
          <textarea className="share-secret-value" readOnly rows={3} value={secretValue} />
        </label>
        <div className="dialog-actions">
          <button className="btn btn-secondary" type="button" onClick={() => { void copy() }}>
            {copied ? '已复制' : '复制'}
          </button>
        </div>
        <label className="share-secret-ack">
          <input type="checkbox" checked={ack} onChange={(event) => setAck(event.target.checked)} />
          <span>我已保存，并理解关闭后无法再次查看</span>
        </label>
        <div className="dialog-actions">
          <button className="btn btn-primary" type="button" disabled={!ack} onClick={onClose}>关闭</button>
        </div>
      </section>
    </div>
  )
}
