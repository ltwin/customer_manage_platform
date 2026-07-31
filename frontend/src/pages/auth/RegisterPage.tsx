import { useEffect, useState } from 'react'
import type { FormEvent } from 'react'
import { Link } from 'react-router-dom'
import { AlertCircle, CheckCircle2 } from 'lucide-react'
import { ApiError, fetchAuthCapabilities, registerAccount } from '../../api/client'
import AuthPageShell from './AuthPageShell'
import './LoginPage.css'

type CapabilityState = 'loading' | 'open' | 'closed' | 'unavailable'

export default function RegisterPage() {
  const [capability, setCapability] = useState<CapabilityState>('loading')
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [confirmation, setConfirmation] = useState('')
  const [formError, setFormError] = useState<string | null>(null)
  const [submitted, setSubmitted] = useState(false)
  const [submitting, setSubmitting] = useState(false)

  useEffect(() => {
    let active = true
    void fetchAuthCapabilities()
      .then((response) => {
        if (active) setCapability(response.public_registration_enabled ? 'open' : 'closed')
      })
      .catch(() => {
        if (active) setCapability('unavailable')
      })
    return () => {
      active = false
    }
  }, [])

  async function onSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setFormError(null)
    if (capability !== 'open') return
    if (email.trim() === '' || password === '') {
      setFormError('请输入邮箱和密码。')
      return
    }
    if (password !== confirmation) {
      setFormError('两次输入的密码不一致。')
      return
    }

    setSubmitting(true)
    try {
      await registerAccount(email, password)
      setSubmitted(true)
    } catch (error) {
      if (error instanceof ApiError && error.code === 'registration_disabled') {
        setCapability('closed')
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
      eyebrow="创建账号"
      title="注册你的经营台"
      subtitle="开放状态由服务端实时决定；未开放或读取失败时不会显示注册表单。"
      footer={
        <p className="auth-foot">
          已有账号？<Link to="/login">返回登录</Link>
        </p>
      }
    >
      {capability === 'loading' && (
        <p className="auth-status" role="status" aria-live="polite">
          正在确认是否开放注册…
        </p>
      )}

      {capability === 'closed' && (
        <p className="auth-soon-note" role="status">
          <b>自助注册暂未开放。</b>
          <br />
          已有账号仍可正常登录。
        </p>
      )}

      {capability === 'unavailable' && (
        <p className="auth-alert" role="alert">
          <AlertCircle aria-hidden="true" strokeWidth={2.2} />
          <span>暂时无法确认注册状态。为保护账号边界，当前不会开放注册表单。</span>
        </p>
      )}

      {capability === 'open' && submitted && (
        <p className="auth-alert auth-alert-ok" role="status">
          <CheckCircle2 aria-hidden="true" strokeWidth={2.2} />
          <span>如果该邮箱可以注册，验证邮件会很快送达。请从邮件中的链接继续。</span>
        </p>
      )}

      {capability === 'open' && !submitted && (
        <form onSubmit={onSubmit} noValidate data-od-id="form-register">
          {formError && (
            <p className="auth-alert" role="alert">
              <AlertCircle aria-hidden="true" strokeWidth={2.2} />
              <span>{formError}</span>
            </p>
          )}

          <div className="field">
            <label htmlFor="register-email">邮箱</label>
            <input
              id="register-email"
              className="input"
              type="email"
              value={email}
              autoComplete="username"
              autoFocus
              required
              onChange={(event) => {
                setEmail(event.target.value)
                if (formError) setFormError(null)
              }}
            />
          </div>

          <div className="field">
            <label htmlFor="register-password">密码</label>
            <input
              id="register-password"
              className="input"
              type="password"
              value={password}
              autoComplete="new-password"
              required
              aria-describedby="register-password-hint"
              onChange={(event) => {
                setPassword(event.target.value)
                if (formError) setFormError(null)
              }}
            />
            <span className="auth-field-hint" id="register-password-hint">
              密码需满足服务端安全规则，且不会写入浏览器存储。
            </span>
          </div>

          <div className="field">
            <label htmlFor="register-confirmation">再次输入密码</label>
            <input
              id="register-confirmation"
              className="input"
              type="password"
              value={confirmation}
              autoComplete="new-password"
              required
              onChange={(event) => {
                setConfirmation(event.target.value)
                if (formError) setFormError(null)
              }}
            />
          </div>

          <button
            className="btn btn-primary auth-btn-block"
            type="submit"
            disabled={submitting}
            data-od-id="submit-register"
          >
            {submitting && <span className="auth-spinner" aria-hidden="true" />}
            {submitting ? '提交中…' : '创建账号并发送验证邮件'}
          </button>
        </form>
      )}
    </AuthPageShell>
  )
}
