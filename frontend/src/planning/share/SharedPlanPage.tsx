import { useEffect, useState } from 'react'
import { useParams } from 'react-router-dom'
import { ApiError, getSharedPlan, type SharedPlanProjection } from './api'
import SharedFullView from './SharedFullView'
import SharedProposalView from './SharedProposalView'
import './share.css'

type LoadState =
  | { kind: 'loading' }
  | { kind: 'unavailable' }
  | { kind: 'retryable'; message: string }
  | { kind: 'ready'; plan: SharedPlanProjection }

export default function SharedPlanPage() {
  const { token = '' } = useParams()
  const [state, setState] = useState<LoadState>({ kind: 'loading' })

  async function load(currentToken: string) {
    setState({ kind: 'loading' })
    try {
      const plan = await getSharedPlan(currentToken)
      setState({ kind: 'ready', plan })
    } catch (error) {
      if (error instanceof ApiError && error.status === 404) {
        setState({ kind: 'unavailable' })
        return
      }
      setState({
        kind: 'retryable',
        message: error instanceof ApiError
          ? (error.status >= 500 ? '服务暂时不可用，请稍后重试。' : error.message)
          : '网络异常，请检查连接后重试。',
      })
    }
  }

  useEffect(() => {
    if (!token) {
      setState({ kind: 'unavailable' })
      return
    }
    void load(token)
  }, [token])

  if (state.kind === 'loading') {
    return (
      <main className="share-page" role="status" aria-live="polite">
        <p className="share-status">正在打开分享页…</p>
      </main>
    )
  }

  if (state.kind === 'unavailable') {
    return (
      <main className="share-page share-expired">
        <p className="share-eyebrow">影约 · 拍摄策划</p>
        <h1>这个链接已经失效了</h1>
        <p>可能是摄影师更新了链接、方案有了新版本，或合作安排发生变化。你之前提交的意见和认领都还在，不会丢。</p>
        <p>需要新链接的话，直接联系你的摄影师。</p>
      </main>
    )
  }

  if (state.kind === 'retryable') {
    return (
      <main className="share-page share-retry" role="alert">
        <h1>暂时打不开这份方案</h1>
        <p>{state.message}</p>
        <button className="btn btn-primary share-touch" type="button" onClick={() => { void load(token) }}>重试</button>
      </main>
    )
  }

  if (state.plan.view_level === 'proposal') {
    return (
      <SharedProposalView
        token={token}
        plan={state.plan}
        onProjectionConflict={() => { void load(token) }}
      />
    )
  }

  return (
    <SharedFullView
      token={token}
      plan={state.plan}
      onReload={() => { void load(token) }}
    />
  )
}
