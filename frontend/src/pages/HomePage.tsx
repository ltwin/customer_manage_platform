import { useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { ApiError, fetchMe } from '../api/client'
import type { Me } from '../api/client'

export default function HomePage() {
  const [me, setMe] = useState<Me | null>(null)
  const [error, setError] = useState<string | null>(null)
  const navigate = useNavigate()

  useEffect(() => {
    let cancelled = false
    fetchMe()
      .then((data) => {
        if (!cancelled) setMe(data)
      })
      .catch((err: unknown) => {
        if (cancelled) return
        if (err instanceof ApiError && err.status === 401) {
          navigate('/login', { replace: true })
          return
        }
        setError(err instanceof Error ? err.message : '加载失败')
      })
    return () => {
      cancelled = true
    }
  }, [navigate])

  if (error) {
    return (
      <main className="page">
        <p role="alert" className="error">{error}</p>
      </main>
    )
  }
  if (!me) {
    return (
      <main className="page">
        <p>加载中…</p>
      </main>
    )
  }
  return (
    <main className="page">
      <h1>今日经营台</h1>
      <section className="card">
        <h2>当前账号</h2>
        <dl>
          <dt>账号 ID</dt>
          <dd>{me.id}</dd>
          <dt>创建时间</dt>
          <dd>{me.created_at}</dd>
        </dl>
      </section>
    </main>
  )
}
