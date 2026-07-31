import { useState } from 'react'
import type { FormEvent } from 'react'
import { Link, useLocation, useNavigate } from 'react-router-dom'
import { AlertCircle } from 'lucide-react'
import { ApiError, login } from '../../api/client'
import { establishSession } from '../../auth/session'
import AuthPageShell from './AuthPageShell'
import './LoginPage.css'

export default function LoginPage() {
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [fieldError, setFieldError] = useState<string | null>(null)
  const [formError, setFormError] = useState<string | null>(null)
  const [submitting, setSubmitting] = useState(false)
  const navigate = useNavigate()
  const location = useLocation()

  async function onSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setFieldError(null)
    setFormError(null)
    if (email.trim() === '' || password === '') {
      setFieldError('请输入邮箱和登录密码')
      return
    }

    setSubmitting(true)
    try {
      if (await establishSession(() => login(email, password))) {
        const from = (location.state as { from?: string } | null)?.from ?? '/dashboard'
        navigate(from, { replace: true })
      }
    } catch (error) {
      if (error instanceof ApiError && error.status === 401) {
        setFieldError('邮箱或密码不正确')
        setFormError('邮箱或密码不正确，请重新输入。')
      } else if (
        error instanceof ApiError &&
        error.status === 403 &&
        error.code === 'email_verification_required'
      ) {
        setFormError('邮箱尚未验证，请先完成邮件中的验证步骤。')
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
      eyebrow="欢迎回来"
      title="登录你的经营台"
      subtitle="使用已验证的邮箱和密码登录。"
      footer={
        <p className="auth-foot">
          还没有账号？<Link to="/register">查看是否开放注册</Link>
        </p>
      }
    >
      <form onSubmit={onSubmit} noValidate data-od-id="form-email-password">
        {formError && (
          <p className="auth-alert" role="alert">
            <AlertCircle aria-hidden="true" strokeWidth={2.2} />
            <span>{formError}</span>
          </p>
        )}

        <div className={fieldError ? 'field show-err' : 'field'}>
          <label htmlFor="email">邮箱</label>
          <input
            id="email"
            className={fieldError ? 'input invalid' : 'input'}
            type="email"
            value={email}
            autoComplete="username"
            autoFocus
            aria-invalid={fieldError ? true : undefined}
            aria-describedby={fieldError ? 'login-err' : undefined}
            onChange={(event) => {
              setEmail(event.target.value)
              if (fieldError) setFieldError(null)
              if (formError) setFormError(null)
            }}
          />
        </div>

        <div className={fieldError ? 'field show-err' : 'field'}>
          <label htmlFor="password">登录密码</label>
          <input
            id="password"
            className={fieldError ? 'input invalid' : 'input'}
            type="password"
            value={password}
            autoComplete="current-password"
            aria-invalid={fieldError ? true : undefined}
            aria-describedby={fieldError ? 'login-err' : undefined}
            onChange={(event) => {
              setPassword(event.target.value)
              if (fieldError) setFieldError(null)
              if (formError) setFormError(null)
            }}
          />
          <span className="err" id="login-err">
            {fieldError}
          </span>
        </div>

        <button
          className="btn btn-primary auth-btn-block"
          type="submit"
          disabled={submitting}
          data-od-id="submit-email-password"
        >
          {submitting && <span className="auth-spinner" aria-hidden="true" />}
          {submitting ? '登录中…' : '登录'}
        </button>
      </form>
    </AuthPageShell>
  )
}
