import { useLayoutEffect, useRef, useState } from 'react'
import type { FormEvent } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { AlertCircle } from 'lucide-react'
import { ApiError, resetPassword } from '../../api/client'
import { consumeActionToken } from '../../auth/actionToken'
import { setAnonymous } from '../../auth/session'
import AuthPageShell from './AuthPageShell'
import './LoginPage.css'

type TokenState = 'reading' | 'ready' | 'missing'

export default function ResetPasswordPage() {
  const tokenRef = useRef<string | null>(null)
  const started = useRef(false)
  const [tokenState, setTokenState] = useState<TokenState>('reading')
  const [password, setPassword] = useState('')
  const [confirmation, setConfirmation] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [formError, setFormError] = useState<string | null>(null)
  const navigate = useNavigate()

  useLayoutEffect(() => {
    if (started.current) return
    started.current = true
    tokenRef.current = consumeActionToken()
    setTokenState(tokenRef.current ? 'ready' : 'missing')
  }, [])

  async function onSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setFormError(null)
    if (!tokenRef.current) {
      setTokenState('missing')
      return
    }
    if (password.length < 12 || password.length > 72) {
      setFormError('新密码长度需为 12–72 个字符。')
      return
    }
    if (password !== confirmation) {
      setFormError('两次输入的新密码不一致。')
      return
    }
    setSubmitting(true)
    try {
      await resetPassword(tokenRef.current, password)
      tokenRef.current = null
      setAnonymous()
      navigate('/login', { replace: true, state: { notice: '密码已重置，请使用新密码登录。' } })
    } catch (error) {
      if (error instanceof ApiError && error.code === 'invalid_or_expired_token') {
        tokenRef.current = null
        setTokenState('missing')
        setFormError('重置链接无效或已过期，请重新获取。')
      } else if (error instanceof ApiError && error.status === 429) {
        setFormError('尝试次数过多，请稍后再试。')
      } else if (error instanceof ApiError) {
        setFormError(error.message)
      } else {
        setFormError('网络错误，请稍后重试。')
      }
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <AuthPageShell
      eyebrow="重置密码"
      title="设置新的登录密码"
      subtitle="重置凭证只从当前页面片段读取一次，并已立即从地址栏移除。"
      footer={<p className="auth-foot"><Link to="/forgot-password">重新获取链接</Link>　·　<Link to="/login">返回登录</Link></p>}
    >
      {tokenState === 'reading' && <p className="auth-status" role="status">正在读取安全凭证…</p>}
      {tokenState === 'missing' && (
        <p className="auth-alert" role="alert">
          <AlertCircle aria-hidden="true" strokeWidth={2.2} />
          <span>{formError ?? '重置链接无效或缺少必要信息，请重新获取。'}</span>
        </p>
      )}
      {tokenState === 'ready' && (
        <form onSubmit={onSubmit} noValidate>
          {formError && (
            <p className="auth-alert" role="alert">
              <AlertCircle aria-hidden="true" strokeWidth={2.2} />
              <span>{formError}</span>
            </p>
          )}
          <div className="field">
            <label htmlFor="reset-password">新密码</label>
            <input
              id="reset-password"
              className="input"
              type="password"
              autoComplete="new-password"
              minLength={12}
              maxLength={72}
              autoFocus
              required
              value={password}
              onChange={(event) => {
                setPassword(event.target.value)
                if (formError) setFormError(null)
              }}
            />
          </div>
          <div className="field">
            <label htmlFor="reset-confirmation">再次输入新密码</label>
            <input
              id="reset-confirmation"
              className="input"
              type="password"
              autoComplete="new-password"
              minLength={12}
              maxLength={72}
              required
              value={confirmation}
              onChange={(event) => {
                setConfirmation(event.target.value)
                if (formError) setFormError(null)
              }}
            />
          </div>
          <button className="btn btn-primary auth-btn-block" type="submit" disabled={submitting}>
            {submitting && <span className="auth-spinner" aria-hidden="true" />}
            {submitting ? '正在重置…' : '重置密码'}
          </button>
        </form>
      )}
    </AuthPageShell>
  )
}
