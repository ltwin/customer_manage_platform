import { useState } from 'react'
import type { FormEvent } from 'react'
import { Link } from 'react-router-dom'
import { AlertCircle, CheckCircle2 } from 'lucide-react'
import { ApiError, forgotPassword } from '../../api/client'
import AuthPageShell from './AuthPageShell'
import './LoginPage.css'

export default function ForgotPasswordPage() {
  const [email, setEmail] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [submitted, setSubmitted] = useState(false)
  const [formError, setFormError] = useState<string | null>(null)

  async function onSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setFormError(null)
    if (email.trim() === '') {
      setFormError('请输入用于登录的邮箱。')
      return
    }
    setSubmitting(true)
    try {
      await forgotPassword(email)
      setSubmitted(true)
    } catch (error) {
      if (error instanceof ApiError && error.status === 429) {
        setFormError('请求过于频繁，请稍后再试。')
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
      eyebrow="找回密码"
      title="获取密码重置链接"
      subtitle="无论邮箱是否存在，公开响应都保持一致。"
      footer={<p className="auth-foot"><Link to="/login">返回登录</Link></p>}
    >
      {submitted ? (
        <p className="auth-alert auth-alert-ok" role="status" aria-live="polite">
          <CheckCircle2 aria-hidden="true" strokeWidth={2.2} />
          <span>如果该邮箱可用于密码恢复，重置邮件会很快送达。请从邮件链接继续。</span>
        </p>
      ) : (
        <form onSubmit={onSubmit} noValidate>
          {formError && (
            <p className="auth-alert" role="alert">
              <AlertCircle aria-hidden="true" strokeWidth={2.2} />
              <span>{formError}</span>
            </p>
          )}
          <div className="field">
            <label htmlFor="forgot-email">登录邮箱</label>
            <input
              id="forgot-email"
              className="input"
              type="email"
              autoComplete="username"
              autoFocus
              required
              value={email}
              onChange={(event) => {
                setEmail(event.target.value)
                if (formError) setFormError(null)
              }}
            />
          </div>
          <button className="btn btn-primary auth-btn-block" type="submit" disabled={submitting}>
            {submitting && <span className="auth-spinner" aria-hidden="true" />}
            {submitting ? '正在提交…' : '发送重置邮件'}
          </button>
        </form>
      )}
    </AuthPageShell>
  )
}
