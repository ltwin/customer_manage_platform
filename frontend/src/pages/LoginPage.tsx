import { useState } from 'react'
import type { FormEvent } from 'react'
import { useLocation, useNavigate } from 'react-router-dom'
import { ApiError, login } from '../api/client'
import { setToken } from '../auth/token'

export default function LoginPage() {
  const [password, setPassword] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const navigate = useNavigate()
  const location = useLocation()

  async function onSubmit(e: FormEvent) {
    e.preventDefault()
    if (password === '') {
      setError('请输入密码')
      return
    }
    setSubmitting(true)
    setError(null)
    try {
      const { token } = await login(password)
      setToken(token)
      const from = (location.state as { from?: string } | null)?.from ?? '/'
      navigate(from, { replace: true })
    } catch (err) {
      setError(err instanceof ApiError ? err.message : '网络错误，请重试')
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <main className="page login-page">
      <h1>摄影师 CRM</h1>
      <form onSubmit={onSubmit} className="card">
        <label htmlFor="password">登录密码</label>
        <input
          id="password"
          className="input"
          type="password"
          value={password}
          onChange={(e) => setPassword(e.target.value)}
          autoFocus
        />
        <button className="btn btn-primary" type="submit" disabled={submitting}>
          {submitting ? '登录中…' : '登录'}
        </button>
        {error && <p role="alert" className="error">{error}</p>}
      </form>
    </main>
  )
}
