import { useEffect, useState } from 'react'
import type { FormEvent } from 'react'
import {
  ApiError,
  createSharedPlanFeedback,
  fetchSharedMoodboardBlob,
  newShareMutationKey,
  type SharedMoodboardItem,
  type SharedPlanProposal,
  type SharedPlanProjection,
} from './api'

export function SharedHero({
  plan,
  viewLabel,
}: {
  plan: Pick<SharedPlanProjection, 'title' | 'public_window' | 'public_scale'>
  viewLabel: string
}) {
  return (
    <header className="share-hero">
      <p className="share-eyebrow">影约 · 拍摄策划</p>
      <h1>{plan.title}</h1>
      <p className="share-lead">这是摄影师为这次拍摄准备的创作方案。看完可以直接在页面里留意见，不用注册也不用下载任何东西。</p>
      <div className="share-tag-row">
        <span className="tag">{viewLabel}</span>
        {plan.public_window && (
          <span className="tag">{formatWindow(plan.public_window.starts_at, plan.public_window.ends_at, plan.public_window.timezone)}</span>
        )}
        <span className="tag">{plan.public_scale.planned_shot_count} 个镜头</span>
      </div>
    </header>
  )
}

export function SharedBriefSection({ brief }: { brief: SharedPlanProposal['creative_brief'] }) {
  const lines = [
    brief.theme_statement,
    brief.mood,
    brief.work_title ? `作品：${brief.work_title}` : null,
    brief.character_name ? `角色：${brief.character_name}` : null,
    brief.visual_keywords?.length ? `关键词：${brief.visual_keywords.join('、')}` : null,
  ].filter(Boolean)
  return (
    <section className="share-sec">
      <span className="share-eyebrow">创作意图</span>
      <h2>我们想拍成什么样</h2>
      <p className="share-brief-body">{lines.length > 0 ? lines.join('\n\n') : '摄影师尚未填写公开创作说明。'}</p>
    </section>
  )
}

export function SharedMoodboardSection({ token, items }: { token: string; items: SharedMoodboardItem[] }) {
  if (items.length === 0) return null
  return (
    <section className="share-sec">
      <span className="share-eyebrow">风格方向</span>
      <h2>参考氛围</h2>
      <div className="share-mood">
        {items.map((item) => (
          <MoodboardFigure key={item.ref} token={token} item={item} />
        ))}
      </div>
      <p className="share-hint">参考图仅用于沟通氛围，不代表最终成片。</p>
    </section>
  )
}

function MoodboardFigure({ token, item }: { token: string; item: SharedMoodboardItem }) {
  const [url, setURL] = useState<string | null>(null)
  useEffect(() => {
    let active = true
    let objectURL: string | null = null
    fetchSharedMoodboardBlob(token, item.ref, item.checksum).then((blob) => {
      if (!active) return
      objectURL = URL.createObjectURL(blob)
      setURL(objectURL)
    }).catch(() => {
      if (active) setURL(null)
    })
    return () => {
      active = false
      if (objectURL) URL.revokeObjectURL(objectURL)
    }
  }, [token, item.ref, item.checksum])

  return (
    <figure>
      {url ? <img src={url} alt={item.caption || '分享参考图'} referrerPolicy="no-referrer" /> : <div className="share-mood-placeholder" aria-hidden="true" />}
      {(item.caption || item.usage_note) && (
        <figcaption>
          {item.caption && <span className="share-mood-caption">{item.caption}</span>}
          {item.usage_note && <span className="share-mood-usage">{item.usage_note}</span>}
        </figcaption>
      )}
    </figure>
  )
}

export function SharedScaleSection({
  scale,
  window,
}: {
  scale: SharedPlanProposal['public_scale']
  window: SharedPlanProposal['public_window']
}) {
  return (
    <section className="share-sec">
      <span className="share-eyebrow">规模概览</span>
      <h2>这次大概会拍多少</h2>
      <div className="share-scale">
        <div><b>{scale.planned_shot_count}</b><span>个计划镜头</span></div>
        {scale.planned_look_count != null && <div><b>{scale.planned_look_count}</b><span>套 look</span></div>}
        {scale.planned_scene_count != null && <div><b>{scale.planned_scene_count}</b><span>个场景</span></div>}
        {window && <div><b>{Math.max(1, Math.round(window.duration_minutes / 60))}</b><span>小时计划拍摄</span></div>}
      </div>
    </section>
  )
}

export function SharedFooter() {
  return (
    <footer className="share-foot">
      <p>链接由摄影师发出。请勿把完整链接转发给无关的人。</p>
      <p>此页面通过专属免登录链接访问；链接可能被摄影师随时更新或关闭。</p>
    </footer>
  )
}

export function PlanFeedbackForm({
  token,
  projectionRevision,
  onConflict,
}: {
  token: string
  projectionRevision: number
  onConflict: () => void
}) {
  const [nickname, setNickname] = useState('')
  const [content, setContent] = useState('')
  const [busy, setBusy] = useState(false)
  const [message, setMessage] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)
  // 匿名端不回读他人反馈；这份列表只回显本次访问内自己提交的意见。
  const [sentFeedback, setSentFeedback] = useState<Array<{ who: string; text: string }>>([])
  const [key] = useState(() => newShareMutationKey('plan-feedback'))

  async function submit(event: FormEvent) {
    event.preventDefault()
    const text = content.trim()
    if (text.length < 1 || text.length > 2000) {
      setError('请填写 1 到 2000 字的纯文本意见。')
      return
    }
    setBusy(true)
    setError(null)
    setMessage(null)
    try {
      await createSharedPlanFeedback(token, {
        expected_projection_revision: projectionRevision,
        author_display_name: nickname.trim().slice(0, 40) || undefined,
        content: text,
        policy_version: 'v1',
      }, key)
      setContent('')
      setMessage('意见已送出，摄影师会在工作台看到。')
      setSentFeedback((current) => [{ who: nickname.trim().slice(0, 40) || '匿名', text }, ...current].slice(0, 20))
    } catch (cause) {
      if (cause instanceof ApiError && cause.status === 409) {
        setError('方案已更新。页面将刷新，你刚写的内容仍保留，请确认后再次发送。')
        onConflict()
      } else {
        setError(cause instanceof Error ? cause.message : '发送失败')
      }
    } finally {
      setBusy(false)
    }
  }

  return (
    <section className="share-sec" id="share-feedback">
      <span className="share-eyebrow">整案反馈</span>
      <h2>对整体方案有什么想法</h2>
      <form className="share-fb-form" onSubmit={(event) => { void submit(event) }}>
        <label className="field">
          <span>怎么称呼你（可选，最多 40 字）</span>
          <input maxLength={40} value={nickname} disabled={busy} onChange={(event) => setNickname(event.target.value)} />
          <small>只用于标注这条意见来自谁，不作为登录身份，也不会用来给你发通知。</small>
        </label>
        <label className="field">
          <span>意见正文（1–2000 字纯文本）</span>
          <textarea
            rows={5}
            maxLength={2000}
            value={content}
            disabled={busy}
            onChange={(event) => setContent(event.target.value)}
            required
          />
        </label>
        <button className="btn btn-primary share-touch" type="submit" disabled={busy}>发送意见</button>
        {message && <p className="share-status" role="status">{message}</p>}
        {error && <p className="share-inline-error" role="alert">{error}</p>}
      </form>
      {sentFeedback.length > 0 && (
        <div className="share-fb-list share-fb-sent-list" aria-label="你已提交的意见">
          <p className="share-hint">你在本次访问中提交过的意见：</p>
          {sentFeedback.map((item, index) => (
            <div className="share-fb-item" key={index}>
              <p className="share-fb-who">{item.who} · 已送出</p>
              <p className="share-fb-quote">{item.text}</p>
            </div>
          ))}
        </div>
      )}
    </section>
  )
}

function formatWindow(startsAt: string, endsAt: string, timezone: string): string {
  try {
    const start = new Date(startsAt)
    const end = new Date(endsAt)
    const fmt = new Intl.DateTimeFormat('zh-CN', {
      timeZone: timezone,
      month: '2-digit',
      day: '2-digit',
      hour: '2-digit',
      minute: '2-digit',
      hour12: false,
    })
    return `${fmt.format(start)}–${fmt.format(end)}`
  } catch {
    return `${startsAt}–${endsAt}`
  }
}
