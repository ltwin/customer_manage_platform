import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import type { FormEvent } from 'react'
import { useNavigate } from 'react-router-dom'
import {
  ApiError,
  closeShootPlanAssignmentOffer,
  createShootPlanAssignmentOffer,
  getShootPlanAssignments,
  getShootPlanFeedback,
  getShootPlanShares,
  issueShootPlanShare,
  newShareMutationKey,
  revokeShootPlanAssignment,
  revokeShootPlanShare,
  rotateShootPlanShare,
  setShootPlanFeedbackDisposition,
  type AssignmentManagementItem,
  type ExpiryPolicyProjection,
  type ExpirySource,
  type FeedbackManagementItem,
  type ShareManagementProjection,
  type ShareViewProjection,
} from './api'
import { composeShareURL, generateShareSecretMaterial, isWebCryptoAvailable } from './crypto'
import OneTimeSecretDialog from './OneTimeSecretDialog'

type FocusTarget =
  | { kind: 'feedback' }
  | { kind: 'shot'; shotID: string }
  | { kind: 'offer'; offerID: string }
  | { kind: 'readiness'; readinessItemID: string }
  | null

export default function ShareCollaborationPanel({
  planID,
  planRevision,
  readOnly,
  focus,
}: {
  planID: string
  planRevision: number
  readOnly: boolean
  focus: FocusTarget
}) {
  const navigate = useNavigate()
  const [management, setManagement] = useState<ShareManagementProjection | null>(null)
  const [feedback, setFeedback] = useState<FeedbackManagementItem[]>([])
  const [feedbackCursor, setFeedbackCursor] = useState<string | null>(null)
  const [assignments, setAssignments] = useState<AssignmentManagementItem[]>([])
  const [assignmentCursor, setAssignmentCursor] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const [oneTimeURL, setOneTimeURL] = useState<string | null>(null)
  const [confirm, setConfirm] = useState<{ title: string; body: string; onConfirm: () => Promise<void> } | null>(null)
  const cryptoOK = isWebCryptoAvailable()
  const feedbackSectionRef = useRef<HTMLElement | null>(null)

  const reload = useCallback(async () => {
    setError(null)
    try {
      const [shareProj, feedbackPage, assignmentPage] = await Promise.all([
        getShootPlanShares(planID),
        getShootPlanFeedback(planID, { limit: 50 }),
        getShootPlanAssignments(planID, { limit: 50 }),
      ])
      setManagement(shareProj)
      setFeedback(feedbackPage.items)
      setFeedbackCursor(feedbackPage.next_cursor ?? null)
      setAssignments(assignmentPage.items)
      setAssignmentCursor(assignmentPage.next_cursor ?? null)
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : '分享协作区加载失败')
    }
  }, [planID])

  useEffect(() => {
    void reload()
  }, [reload, planRevision])

  useEffect(() => {
    if (!focus || !management) return
    if (focus.kind === 'feedback' || focus.kind === 'shot') {
      feedbackSectionRef.current?.scrollIntoView({ behavior: 'smooth', block: 'start' })
      if (focus.kind === 'shot') {
        document.getElementById(`share-mgmt-shot-${focus.shotID}`)?.scrollIntoView({ behavior: 'smooth', block: 'center' })
      }
    }
    if (focus.kind === 'offer') {
      document.getElementById(`share-mgmt-offer-${focus.offerID}`)?.scrollIntoView({ behavior: 'smooth', block: 'center' })
    }
    if (focus.kind === 'readiness') {
      document.getElementById(`share-mgmt-readiness-${focus.readinessItemID}`)?.scrollIntoView({ behavior: 'smooth', block: 'center' })
    }
  }, [focus, management, feedback, assignments])

  const proposalView = useMemo(
    () => management?.share_views.find((view) => view.view_level === 'proposal') ?? null,
    [management],
  )
  const fullView = useMemo(
    () => management?.share_views.find((view) => view.view_level === 'full') ?? null,
    [management],
  )

  async function loadMoreFeedback() {
    if (!feedbackCursor) return
    const page = await getShootPlanFeedback(planID, { cursor: feedbackCursor, limit: 50 })
    setFeedback((current) => [...current, ...page.items])
    setFeedbackCursor(page.next_cursor ?? null)
  }

  async function loadMoreAssignments() {
    if (!assignmentCursor) return
    const page = await getShootPlanAssignments(planID, { cursor: assignmentCursor, limit: 50 })
    setAssignments((current) => [...current, ...page.items])
    setAssignmentCursor(page.next_cursor ?? null)
  }

  return (
    <div className="share-collab">
      <div className="planning-section-head">
        <div>
          <p className="planning-eyebrow">分享协作</p>
          <h2>链接、反馈与认领</h2>
          <p>方案概览与完整档各自独立签发与轮换；刷新后只能看到指纹与状态，完整链接只在签发成功时展示一次。</p>
        </div>
      </div>
      {error && <p className="planning-inline-error" role="alert">{error}</p>}
      {!cryptoOK && (
        <p className="planning-inline-error" role="alert">当前浏览器不支持安全随机数，签发与轮换已禁用。</p>
      )}
      <div className="share-view-grid">
        {proposalView && (
          <ShareViewCard
            title="方案概览档"
            view={proposalView}
            planID={planID}
            planRevision={planRevision}
            readOnly={readOnly || !cryptoOK}
            busy={busy}
            setBusy={setBusy}
            setError={setError}
            setOneTimeURL={setOneTimeURL}
            askConfirm={setConfirm}
            onDone={reload}
          />
        )}
        {fullView && (
          <ShareViewCard
            title="完整档"
            view={fullView}
            planID={planID}
            planRevision={planRevision}
            readOnly={readOnly || !cryptoOK}
            busy={busy}
            setBusy={setBusy}
            setError={setError}
            setOneTimeURL={setOneTimeURL}
            askConfirm={setConfirm}
            onDone={reload}
          />
        )}
      </div>

      <section className="planning-panel share-collab-panel" ref={feedbackSectionRef} id="share-feedback-section">
        <div className="planning-panel-head">
          <div>
            <h2>客户反馈</h2>
            <p>采纳或忽略只记录你的判断，不会自动改创作 brief 或镜头。</p>
          </div>
        </div>
        <div className="share-fb-list">
          {feedback.length === 0 && <p className="share-hint">还没有反馈。</p>}
          {feedback.map((item) => (
            <article
              className="share-fb-item"
              key={item.feedback_id}
              id={item.target.kind === 'shot' ? `share-mgmt-shot-${item.target.shot_id}` : undefined}
            >
              <p className="share-fb-who">
                {item.author_display_name || '匿名'}
                {' · '}
                {item.target.kind === 'shot' ? `第 ${item.target.shot_id} 镜` : '整案反馈'}
              </p>
              <p className="share-fb-quote">{item.content}</p>
              <div className="share-form-actions">
                <span className="tag">{dispositionLabel(item.disposition)}</span>
                {item.disposition === 'pending' && !readOnly && (
                  <>
                    <button
                      className="btn btn-secondary btn-sm share-touch"
                      type="button"
                      disabled={busy}
                      onClick={() => {
                        void (async () => {
                          setBusy(true)
                          setError(null)
                          try {
                            const result = await setShootPlanFeedbackDisposition(
                              planID,
                              item.feedback_id,
                              { expected_feedback_revision: item.revision, disposition: 'adopted' },
                              newShareMutationKey('fb-adopt'),
                            )
                            await reload()
                            if (result.deep_link_target.kind === 'shot') {
                              navigate(`/shoot-plans/${encodeURIComponent(planID)}?tab=shots&focus=shot:${result.deep_link_target.shot_id}`)
                            }
                          } catch (cause) {
                            handleConflict(cause, setError, reload, '反馈版本已变化，页面已刷新，请核对后重试。')
                          } finally {
                            setBusy(false)
                          }
                        })()
                      }}
                    >
                      标记采纳
                    </button>
                    <button
                      className="btn btn-ghost btn-sm share-touch"
                      type="button"
                      disabled={busy}
                      onClick={() => {
                        void (async () => {
                          setBusy(true)
                          setError(null)
                          try {
                            await setShootPlanFeedbackDisposition(
                              planID,
                              item.feedback_id,
                              { expected_feedback_revision: item.revision, disposition: 'ignored' },
                              newShareMutationKey('fb-ignore'),
                            )
                            await reload()
                          } catch (cause) {
                            handleConflict(cause, setError, reload, '反馈版本已变化，页面已刷新，请核对后重试。')
                          } finally {
                            setBusy(false)
                          }
                        })()
                      }}
                    >
                      忽略
                    </button>
                  </>
                )}
                {item.disposition === 'adopted' && item.target.kind === 'shot' && (
                  <button
                    className="btn btn-primary btn-sm share-touch"
                    type="button"
                    onClick={() => navigate(`/shoot-plans/${encodeURIComponent(planID)}?tab=shots&focus=shot:${item.target.kind === 'shot' ? item.target.shot_id : ''}`)}
                  >
                    去修改该镜头
                  </button>
                )}
              </div>
            </article>
          ))}
        </div>
        {feedbackCursor && (
          <button className="btn btn-secondary share-touch" type="button" onClick={() => { void loadMoreFeedback() }}>加载更多反馈</button>
        )}
      </section>

      <section className="planning-panel share-collab-panel">
        <div className="planning-panel-head">
          <div>
            <h2>认领记录</h2>
            <p>链接轮换或撤销不会删除已有认领；摄影师可随时撤销。</p>
          </div>
        </div>
        <div className="share-fb-list">
          {assignments.length === 0 && <p className="share-hint">还没有认领。</p>}
          {assignments.map((item) => (
            <article
              className="share-fb-item"
              key={item.assignment_id}
              id={
                item.deep_link_target.kind === 'offer'
                  ? `share-mgmt-offer-${item.deep_link_target.offer_id}`
                  : item.deep_link_target.kind === 'readiness'
                    ? `share-mgmt-readiness-${item.deep_link_target.readiness_item_id}`
                    : undefined
              }
            >
              <p className="share-fb-who">{item.claimed_by_display_name || '匿名'}</p>
              <p className="share-fb-quote">{item.content_snapshot}</p>
              <div className="share-form-actions">
                <span className="tag">{item.status === 'active' ? '已认领' : '已撤销'}</span>
                {item.status === 'active' && !readOnly && (
                  <button
                    className="btn btn-ghost btn-sm share-touch"
                    type="button"
                    disabled={busy}
                    onClick={() => setConfirm({
                      title: `撤销「${item.content_snapshot}」的认领？`,
                      body: '撤销后该项回到无人认领。不会给对方发送任何通知。',
                      onConfirm: async () => {
                        setBusy(true)
                        setError(null)
                        try {
                          await revokeShootPlanAssignment(
                            planID,
                            item.assignment_id,
                            { expected_assignment_revision: item.revision, policy_version: 'v1' },
                            newShareMutationKey('asg-revoke'),
                          )
                          await reload()
                        } catch (cause) {
                          handleConflict(cause, setError, reload, '认领版本已变化，页面已刷新，请核对后重试。')
                        } finally {
                          setBusy(false)
                        }
                      },
                    })}
                  >
                    撤销认领
                  </button>
                )}
              </div>
            </article>
          ))}
        </div>
        {assignmentCursor && (
          <button className="btn btn-secondary share-touch" type="button" onClick={() => { void loadMoreAssignments() }}>加载更多认领</button>
        )}
      </section>

      <OnSiteOfferPanel
        planID={planID}
        planRevision={planRevision}
        offers={management?.on_site_offers ?? []}
        offersNextCursor={management?.offers_next_cursor ?? null}
        readOnly={readOnly}
        busy={busy}
        setBusy={setBusy}
        setError={setError}
        askConfirm={setConfirm}
        onDone={reload}
      />

      {oneTimeURL && (
        <OneTimeSecretDialog
          title="请立刻保存分享链接"
          warning="关闭后无法再次查看完整链接。工作台刷新后只保留指纹与状态。"
          secretLabel="完整分享链接"
          secretValue={oneTimeURL}
          onClose={() => setOneTimeURL(null)}
        />
      )}
      {confirm && (
        <div className="overlay open" role="presentation">
          <section className="dialog planning-dialog" role="dialog" aria-modal="true" aria-labelledby="shareConfirmTitle">
            <h2 id="shareConfirmTitle">{confirm.title}</h2>
            <p>{confirm.body}</p>
            <div className="dialog-actions">
              <button className="btn btn-ghost" type="button" onClick={() => setConfirm(null)}>取消</button>
              <button
                className="btn btn-danger"
                type="button"
                onClick={() => {
                  const action = confirm.onConfirm
                  setConfirm(null)
                  void action()
                }}
              >
                确认
              </button>
            </div>
          </section>
        </div>
      )}
    </div>
  )
}

function ShareViewCard({
  title,
  view,
  planID,
  planRevision,
  readOnly,
  busy,
  setBusy,
  setError,
  setOneTimeURL,
  askConfirm,
  onDone,
}: {
  title: string
  view: ShareViewProjection
  planID: string
  planRevision: number
  readOnly: boolean
  busy: boolean
  setBusy: (value: boolean) => void
  setError: (value: string | null) => void
  setOneTimeURL: (value: string | null) => void
  askConfirm: (value: { title: string; body: string; onConfirm: () => Promise<void> } | null) => void
  onDone: () => Promise<void>
}) {
  const policy = view.expiry_policy
  const [expiresAt, setExpiresAt] = useState(policy.resolved_default_expires_at)
  const [useDefault, setUseDefault] = useState(true)
  const latest = view.latest_generation

  useEffect(() => {
    setExpiresAt(policy.resolved_default_expires_at)
    setUseDefault(true)
  }, [policy.resolved_default_expires_at, policy.default_quote.evaluated_at])

  function buildExpirySource(): ExpirySource {
    if (useDefault) {
      return { kind: 'quoted_default', default_quote: policy.default_quote }
    }
    return { kind: 'explicit' }
  }

  async function issue() {
    setBusy(true)
    setError(null)
    try {
      const material = await generateShareSecretMaterial()
      const result = await issueShootPlanShare(planID, {
        expected_plan_revision: planRevision,
        view_level: view.view_level,
        secret_commitment: material.commitment,
        expires_at: useDefault ? policy.resolved_default_expires_at : expiresAt,
        expiry_source: buildExpirySource(),
        policy_version: 'v1',
      }, newShareMutationKey(`issue-${view.view_level}`))
      setOneTimeURL(composeShareURL(result.selector, material.secret))
      await onDone()
    } catch (cause) {
      handleConflict(cause, setError, onDone, '签发冲突：页面已刷新，请核对有效期后重试。')
    } finally {
      setBusy(false)
    }
  }

  async function rotate() {
    if (!latest) return
    setBusy(true)
    setError(null)
    try {
      const material = await generateShareSecretMaterial()
      const result = await rotateShootPlanShare(planID, latest.share_id, {
        expected_share_revision: latest.revision,
        new_secret_commitment: material.commitment,
        expires_at: useDefault ? policy.resolved_default_expires_at : expiresAt,
        expiry_source: buildExpirySource(),
        policy_version: 'v1',
      }, newShareMutationKey(`rotate-${view.view_level}`))
      setOneTimeURL(composeShareURL(result.selector, material.secret))
      await onDone()
    } catch (cause) {
      handleConflict(cause, setError, onDone, '轮换冲突：页面已刷新，请核对后重试。')
    } finally {
      setBusy(false)
    }
  }

  return (
    <section className="planning-panel share-collab-panel">
      <div className="planning-panel-head">
        <div>
          <h2>{title}</h2>
          <p>{view.view_level === 'proposal' ? '公开创作摘要与整案反馈。' : '含逐镜与分工认领。'}</p>
        </div>
        {latest ? (
          <span className="tag">{stateLabel(latest.effective_state)}</span>
        ) : (
          <span className="tag">尚未签发</span>
        )}
      </div>
      {latest && (
        <dl className="share-kv">
          <div><dt>指纹</dt><dd>{latest.fingerprint}</dd></div>
          <div><dt>代数</dt><dd>第 {latest.generation} 代</dd></div>
          <div><dt>到期</dt><dd>{formatInstant(latest.expires_at)}</dd></div>
          {latest.ended_reason && <div><dt>结束原因</dt><dd>{latest.ended_reason}</dd></div>}
        </dl>
      )}
      <ExpiryEditor
        policy={policy}
        expiresAt={expiresAt}
        useDefault={useDefault}
        disabled={readOnly || busy}
        onExpiresAt={setExpiresAt}
        onUseDefault={setUseDefault}
      />
      <div className="share-form-actions">
        {(!latest || latest.effective_state !== 'active') && (
          <button className="btn btn-primary share-touch" type="button" disabled={readOnly || busy} onClick={() => { void issue() }}>
            签发链接
          </button>
        )}
        {latest?.effective_state === 'active' && (
          <>
            <button
              className="btn btn-secondary share-touch"
              type="button"
              disabled={readOnly || busy}
              onClick={() => askConfirm({
                title: `轮换${title}链接？`,
                body: '旧链接会立即失效。另一档分享链接不受影响。已有反馈与认领会保留。',
                onConfirm: rotate,
              })}
            >
              轮换链接
            </button>
            <button
              className="btn btn-danger share-touch"
              type="button"
              disabled={readOnly || busy}
              onClick={() => askConfirm({
                title: `撤销${title}分享？`,
                body: '撤销后此档链接立即失效。另一档不受影响。已有反馈与认领会保留。',
                onConfirm: async () => {
                  setBusy(true)
                  setError(null)
                  try {
                    await revokeShootPlanShare(
                      planID,
                      latest.share_id,
                      { expected_share_revision: latest.revision, policy_version: 'v1' },
                      newShareMutationKey(`revoke-${view.view_level}`),
                    )
                    await onDone()
                  } catch (cause) {
                    handleConflict(cause, setError, onDone, '撤销冲突：页面已刷新，请核对后重试。')
                  } finally {
                    setBusy(false)
                  }
                },
              })}
            >
              撤销
            </button>
          </>
        )}
      </div>
    </section>
  )
}

function ExpiryEditor({
  policy,
  expiresAt,
  useDefault,
  disabled,
  onExpiresAt,
  onUseDefault,
}: {
  policy: ExpiryPolicyProjection
  expiresAt: string
  useDefault: boolean
  disabled: boolean
  onExpiresAt: (value: string) => void
  onUseDefault: (value: boolean) => void
}) {
  return (
    <div className="share-expiry">
      <label className="share-secret-ack">
        <input
          type="checkbox"
          checked={useDefault}
          disabled={disabled}
          onChange={(event) => onUseDefault(event.target.checked)}
        />
        <span>使用默认到期（{formatInstant(policy.resolved_default_expires_at)}）</span>
      </label>
      {!useDefault && (
        <label className="field">
          <span>绝对到期时间（UTC）</span>
          <input
            type="datetime-local"
            disabled={disabled}
            value={toDatetimeLocal(expiresAt)}
            min={toDatetimeLocal(policy.min_expires_at)}
            max={toDatetimeLocal(policy.max_expires_at)}
            onChange={(event) => onExpiresAt(fromDatetimeLocal(event.target.value))}
          />
          <small className="planning-field-help">
            允许范围 {formatInstant(policy.min_expires_at)} ~ {formatInstant(policy.max_expires_at)}
          </small>
        </label>
      )}
    </div>
  )
}

function OnSiteOfferPanel({
  planID,
  planRevision,
  offers,
  offersNextCursor,
  readOnly,
  busy,
  setBusy,
  setError,
  askConfirm,
  onDone,
}: {
  planID: string
  planRevision: number
  offers: NonNullable<ShareManagementProjection['on_site_offers']>
  offersNextCursor: string | null
  readOnly: boolean
  busy: boolean
  setBusy: (value: boolean) => void
  setError: (value: string | null) => void
  askConfirm: (value: { title: string; body: string; onConfirm: () => Promise<void> } | null) => void
  onDone: () => Promise<void>
}) {
  const [content, setContent] = useState('')
  const [moreOffers, setMoreOffers] = useState(offers)
  const [cursor, setCursor] = useState(offersNextCursor)

  useEffect(() => {
    setMoreOffers(offers)
    setCursor(offersNextCursor)
  }, [offers, offersNextCursor])

  async function createOffer(event: FormEvent) {
    event.preventDefault()
    const text = content.trim()
    if (!text) return
    setBusy(true)
    setError(null)
    try {
      await createShootPlanAssignmentOffer(planID, {
        expected_plan_revision: planRevision,
        assignment_kind: 'on_site_support',
        content: text,
        policy_version: 'v1',
      }, newShareMutationKey('offer-create'))
      setContent('')
      await onDone()
    } catch (cause) {
      handleConflict(cause, setError, onDone, '创建协助项冲突：页面已刷新，输入已保留。')
    } finally {
      setBusy(false)
    }
  }

  return (
    <section className="planning-panel share-collab-panel">
      <div className="planning-panel-head">
        <div>
          <h2>现场协助邀约</h2>
          <p>创建后会出现在完整档分享页供认领。</p>
        </div>
      </div>
      {!readOnly && (
        <form className="share-offer-form" onSubmit={(event) => { void createOffer(event) }}>
          <label className="field">
            <span>协助内容</span>
            <input value={content} disabled={busy} onChange={(event) => setContent(event.target.value)} required />
          </label>
          <button className="btn btn-primary share-touch" type="submit" disabled={busy}>创建邀约</button>
        </form>
      )}
      <div className="share-fb-list">
        {moreOffers.map((offer) => (
          <article className="share-fb-item" key={offer.offer_id} id={`share-mgmt-offer-${offer.offer_id}`}>
            <p className="share-fb-quote">{offer.content}</p>
            <div className="share-form-actions">
              <span className="tag">{offer.state === 'open' ? '开放中' : '已关闭'}</span>
              {offer.state === 'open' && !readOnly && (
                <button
                  className="btn btn-ghost btn-sm share-touch"
                  type="button"
                  disabled={busy}
                  onClick={() => askConfirm({
                    title: '关闭这项现场协助邀约？',
                    body: '关闭后分享页不再接受新认领。已有认领会保留。',
                    onConfirm: async () => {
                      setBusy(true)
                      setError(null)
                      try {
                        await closeShootPlanAssignmentOffer(
                          planID,
                          offer.offer_id,
                          { expected_offer_revision: offer.revision, policy_version: 'v1' },
                          newShareMutationKey('offer-close'),
                        )
                        await onDone()
                      } catch (cause) {
                        handleConflict(cause, setError, onDone, '关闭冲突：页面已刷新，请核对后重试。')
                      } finally {
                        setBusy(false)
                      }
                    },
                  })}
                >
                  关闭
                </button>
              )}
            </div>
          </article>
        ))}
      </div>
      {cursor && (
        <button
          className="btn btn-secondary share-touch"
          type="button"
          onClick={() => {
            void (async () => {
              const page = await getShootPlanShares(planID, { offerCursor: cursor, offerLimit: 50 })
              setMoreOffers((current) => [...current, ...page.on_site_offers])
              setCursor(page.offers_next_cursor ?? null)
            })()
          }}
        >
          加载更多邀约
        </button>
      )}
    </section>
  )
}

function handleConflict(
  cause: unknown,
  setError: (value: string | null) => void,
  reload: () => Promise<void>,
  message: string,
) {
  if (cause instanceof ApiError && cause.status === 409) {
    setError(message)
    void reload()
    return
  }
  setError(cause instanceof Error ? cause.message : '操作失败')
}

function dispositionLabel(value: FeedbackManagementItem['disposition']): string {
  switch (value) {
    case 'pending':
      return '未处理'
    case 'adopted':
      return '已采纳'
    case 'ignored':
      return '已忽略'
  }
}

function stateLabel(value: NonNullable<ShareViewProjection['latest_generation']>['effective_state']): string {
  switch (value) {
    case 'active':
      return '分享中'
    case 'expired':
      return '已过期'
    case 'rotated':
      return '已轮换'
    case 'revoked':
      return '已撤销'
    case 'eligibility_invalidated':
      return '已失资格'
    case 'archived':
      return '已归档失效'
  }
}

function formatInstant(value: string): string {
  try {
    return new Intl.DateTimeFormat('zh-CN', {
      dateStyle: 'medium',
      timeStyle: 'short',
    }).format(new Date(value))
  } catch {
    return value
  }
}

function toDatetimeLocal(iso: string): string {
  const date = new Date(iso)
  if (Number.isNaN(date.getTime())) return ''
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${date.getUTCFullYear()}-${pad(date.getUTCMonth() + 1)}-${pad(date.getUTCDate())}T${pad(date.getUTCHours())}:${pad(date.getUTCMinutes())}`
}

function fromDatetimeLocal(value: string): string {
  if (!value) return new Date().toISOString()
  return new Date(`${value}:00.000Z`).toISOString()
}
