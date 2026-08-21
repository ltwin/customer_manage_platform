import { useState } from 'react'
import type { FormEvent } from 'react'
import {
  ApiError,
  claimSharedAssignment,
  createSharedShotFeedback,
  newShareMutationKey,
  selfRevokeSharedAssignment,
  type SharedAssignmentClaimInput,
  type SharedAssignmentSelfRevokeInput,
  type SharedPlanFull,
} from './api'
import { generateClaimReceiptMaterial, isClaimReceiptWireFormat, isWebCryptoAvailable } from './crypto'
import OneTimeSecretDialog from './OneTimeSecretDialog'

export function FullShotsSection({
  token,
  shots,
  onConflict,
}: {
  token: string
  shots: SharedPlanFull['shots']
  onConflict: () => void
}) {
  return (
    <section className="share-sec" id="share-shots">
      <span className="share-eyebrow">逐镜方案</span>
      <h2>每一个镜头怎么拍</h2>
      <p className="share-hint">哪条觉得不合适，直接点这条下面的「这条我有想法」。</p>
      <div className="share-shot-list">
        {shots.map((shot) => (
          <article className="share-shot" key={shot.id} id={`share-shot-${shot.id}`}>
            <p className="share-shot-pos">{String(shot.position).padStart(2, '0')}</p>
            <h3>{shot.title}</h3>
            <dl>
              {shot.scene && <div><dt>场景</dt><dd>{shot.scene}</dd></div>}
              {shot.action && <div><dt>动作要点</dt><dd>{shot.action}</dd></div>}
              {shot.expression && <div><dt>表情</dt><dd>{shot.expression}</dd></div>}
              {shot.composition && <div><dt>构图</dt><dd>{shot.composition}</dd></div>}
              {shot.lighting_text && <div><dt>光线</dt><dd>{shot.lighting_text}</dd></div>}
            </dl>
            <ShotFeedbackForm token={token} shotID={shot.id} shotRevision={shot.revision} onConflict={onConflict} />
          </article>
        ))}
      </div>
    </section>
  )
}

function ShotFeedbackForm({
  token,
  shotID,
  shotRevision,
  onConflict,
}: {
  token: string
  shotID: string
  shotRevision: number
  onConflict: () => void
}) {
  const [open, setOpen] = useState(false)
  const [nickname, setNickname] = useState('')
  const [content, setContent] = useState('')
  const [busy, setBusy] = useState(false)
  const [message, setMessage] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [sent, setSent] = useState<string | null>(null)
  const [key] = useState(() => newShareMutationKey(`shot-feedback-${shotID}`))

  async function submit(event: FormEvent) {
    event.preventDefault()
    const text = content.trim()
    if (text.length < 1 || text.length > 2000) {
      setError('请填写 1 到 2000 字的纯文本意见。')
      return
    }
    setBusy(true)
    setError(null)
    try {
      await createSharedShotFeedback(token, shotID, {
        expected_shot_revision: shotRevision,
        author_display_name: nickname.trim().slice(0, 40) || undefined,
        content: text,
        policy_version: 'v1',
      }, key)
      setContent('')
      setMessage('这条意见已送出。')
      setSent(text)
      setOpen(false)
    } catch (cause) {
      if (cause instanceof ApiError && cause.status === 409) {
        setError('镜头已更新。页面将刷新，你刚写的内容仍保留。')
        onConflict()
      } else {
        setError(cause instanceof Error ? cause.message : '发送失败')
      }
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="share-shot-actions">
      {!open && (
        <button className="btn btn-secondary btn-sm share-touch" type="button" onClick={() => setOpen(true)}>
          这条我有想法
        </button>
      )}
      {message && !sent && <p className="share-status" role="status">{message}</p>}
      {sent && (
        <p className="share-status" role="status">
          你已对这一镜提过意见：「{sent.length > 80 ? `${sent.slice(0, 80)}…` : sent}」
        </p>
      )}
      {open && (
        <form className="share-fb-inline" onSubmit={(event) => { void submit(event) }}>
          <label className="field">
            <span>怎么称呼你（可选）</span>
            <input maxLength={40} value={nickname} disabled={busy} onChange={(event) => setNickname(event.target.value)} />
          </label>
          <label className="field">
            <span>意见正文</span>
            <textarea rows={3} maxLength={2000} value={content} disabled={busy} onChange={(event) => setContent(event.target.value)} required />
          </label>
          <div className="share-form-actions">
            <button className="btn btn-ghost btn-sm share-touch" type="button" onClick={() => setOpen(false)}>取消</button>
            <button className="btn btn-primary btn-sm share-touch" type="submit" disabled={busy}>发送</button>
          </div>
          {error && <p className="share-inline-error" role="alert">{error}</p>}
        </form>
      )}
    </div>
  )
}

export function FullAssignmentsSection({
  token,
  opportunities,
  onReload,
}: {
  token: string
  opportunities: SharedPlanFull['assignment_opportunities']
  onReload: () => void
}) {
  const [receipt, setReceipt] = useState<string | null>(null)
  const cryptoOK = isWebCryptoAvailable()

  return (
    <section className="share-sec" id="share-assignments">
      <span className="share-eyebrow">准备分工</span>
      <h2>我可以认领哪几项</h2>
      <p className="share-hint">认领后摄影师会在拍摄前几天自己核对一遍，不会给你发消息催。认领时会生成一个凭证码，想自己取消认领时需要用到它。</p>
      {!cryptoOK && (
        <p className="share-inline-error" role="alert">当前浏览器不支持安全随机数，认领已禁用。请换用支持 Web Crypto 的浏览器。</p>
      )}
      <div className="share-claim-list">
        {opportunities.map((item) => (
          <div className="share-claim" key={item.offer_id}>
            <div>
              <b>{item.content}</b>
              <p className="share-hint">
                {item.assignment_kind === 'readiness' ? '准备项' : '现场协助'}
                {item.preparation_lead_days_preview != null ? ` · 建议提前 ${item.preparation_lead_days_preview} 天` : ''}
              </p>
              {item.active_assignment && (
                <p className="share-hint">已有认领：{item.active_assignment.claimed_by_display_name}</p>
              )}
            </div>
            {!item.active_assignment && (
              <ClaimForm
                token={token}
                disabled={!cryptoOK}
                offerID={item.offer_id}
                kind={item.assignment_kind}
                readinessItemID={item.readiness_item_id ?? undefined}
                targetRevision={item.target_revision}
                onClaimed={(wire) => {
                  setReceipt(wire)
                  onReload()
                }}
                onConflict={onReload}
              />
            )}
            {item.active_assignment && (
              <SelfRevokeForm
                token={token}
                assignment={item.active_assignment}
                onRevoked={onReload}
                onConflict={onReload}
              />
            )}
          </div>
        ))}
      </div>
      {receipt && (
        <OneTimeSecretDialog
          title="请立刻保存认领凭证"
          warning="关闭后无法再次查看这串凭证。之后若要自行撤销认领，需要当前完整档链接与这串凭证。"
          secretLabel="认领凭证"
          secretValue={receipt}
          onClose={() => setReceipt(null)}
        />
      )}
    </section>
  )
}

function ClaimForm({
  token,
  disabled,
  offerID,
  kind,
  readinessItemID,
  targetRevision,
  onClaimed,
  onConflict,
}: {
  token: string
  disabled: boolean
  offerID: string
  kind: 'readiness' | 'on_site_support'
  readinessItemID?: string
  targetRevision: number
  onClaimed: (receiptWire: string) => void
  onConflict: () => void
}) {
  const [nickname, setNickname] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [key] = useState(() => newShareMutationKey('claim'))

  async function submit(event: FormEvent) {
    event.preventDefault()
    if (disabled) return
    setBusy(true)
    setError(null)
    try {
      const material = await generateClaimReceiptMaterial()
      const body: SharedAssignmentClaimInput = {
        target: kind === 'readiness'
          ? { kind: 'readiness', readiness_item_id: readinessItemID }
          : { kind: 'on_site_support', offer_id: offerID },
        expected_target_revision: targetRevision,
        claim_receipt_commitment: material.commitment,
        policy_version: 'v1',
      }
      const trimmed = nickname.trim()
      if (trimmed) body.claimed_by_display_name = trimmed.slice(0, 40)
      await claimSharedAssignment(token, body, key)
      onClaimed(material.receiptWire)
    } catch (cause) {
      if (cause instanceof ApiError && cause.status === 409) {
        setError('这项已被其他人认领或已更新。页面将刷新，你的昵称输入会保留。')
        onConflict()
      } else {
        setError(cause instanceof Error ? cause.message : '认领失败')
      }
    } finally {
      setBusy(false)
    }
  }

  return (
    <form className="share-claim-form" onSubmit={(event) => { void submit(event) }}>
      <label className="field">
        <span>怎么称呼你（可选）</span>
        <input
          maxLength={40}
          value={nickname}
          disabled={busy || disabled}
          onChange={(event) => setNickname(event.target.value)}
        />
      </label>
      <button className="btn btn-primary share-touch" type="submit" disabled={busy || disabled}>认领</button>
      {error && <p className="share-inline-error" role="alert">{error}</p>}
    </form>
  )
}

function SelfRevokeForm({
  token,
  assignment,
  onRevoked,
  onConflict,
}: {
  token: string
  assignment: NonNullable<SharedPlanFull['assignment_opportunities'][number]['active_assignment']>
  onRevoked: () => void
  onConflict: () => void
}) {
  const [open, setOpen] = useState(false)
  const [receipt, setReceipt] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [done, setDone] = useState(false)
  // 幂等键跟随请求体签名：网络失败重试复用同键；改过凭证后换新键，避免 canonical 漂移撞 409。
  const [attempt, setAttempt] = useState<{ signature: string; key: string } | null>(null)

  async function submit(event: FormEvent) {
    event.preventDefault()
    const wire = receipt.trim()
    if (!isClaimReceiptWireFormat(wire)) {
      setError('请输入认领时生成的完整凭证（cr1. 开头的一串字符）。找不到凭证码的话，联系摄影师帮你取消。')
      return
    }
    const body: SharedAssignmentSelfRevokeInput = {
      expected_assignment_revision: assignment.revision,
      claim_receipt: wire,
      policy_version: 'v1',
    }
    const signature = JSON.stringify({ assignment: assignment.id, body })
    const key = attempt?.signature === signature ? attempt.key : newShareMutationKey('self-revoke')
    setAttempt({ signature, key })
    setBusy(true)
    setError(null)
    try {
      await selfRevokeSharedAssignment(token, assignment.id, body, key)
      setDone(true)
      setOpen(false)
      onRevoked()
    } catch (cause) {
      if (cause instanceof ApiError && cause.status === 404) {
        setError('凭证不匹配，或这项认领已经有变化。可刷新后重试，或联系摄影师帮你取消。')
      } else if (cause instanceof ApiError && cause.status === 409) {
        setError('这项认领已有变化。页面将刷新。')
        onConflict()
      } else {
        setError(cause instanceof Error ? cause.message : '取消认领失败，请重试。')
      }
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="share-claim-revoke">
      {!open && !done && (
        <button className="btn btn-ghost btn-sm share-touch" type="button" onClick={() => setOpen(true)}>取消认领</button>
      )}
      {done && <span className="share-status">已取消认领，这项准备重新变为待认领。</span>}
      {open && (
        <form className="share-claim-form" onSubmit={(event) => { void submit(event) }}>
          <label className="field">
            <span>输入认领时的凭证码</span>
            <input
              value={receipt}
              disabled={busy}
              onChange={(event) => setReceipt(event.target.value)}
              placeholder="cr1.…"
              autoComplete="off"
              spellCheck={false}
            />
          </label>
          <div className="share-form-actions">
            <button className="btn btn-ghost btn-sm share-touch" type="button" onClick={() => setOpen(false)}>收起</button>
            <button className="btn btn-secondary btn-sm share-touch" type="submit" disabled={busy}>确认取消认领</button>
          </div>
          {error && <p className="share-inline-error" role="alert">{error}</p>}
        </form>
      )}
    </div>
  )
}
