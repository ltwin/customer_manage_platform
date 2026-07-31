import { useState } from 'react'
import type { FormEvent } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { AlertCircle } from 'lucide-react'
import { ApiError, changePassword } from '../../api/client'
import { setAnonymous } from '../../auth/session'
import AuthPageShell from './AuthPageShell'
import './LoginPage.css'

export default function ChangePasswordPage() {
  const [currentPassword, setCurrentPassword] = useState('')
  const [password, setPassword] = useState('')
  const [confirmation, setConfirmation] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [formError, setFormError] = useState<string | null>(null)
  const navigate = useNavigate()

  async function onSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setFormError(null)
    if (currentPassword.length < 12 || password.length < 12 || password.length > 72) {
      setFormError('当前密码与新密码均需满足 12–72 个字符。')
      return
    }
    if (password !== confirmation) {
      setFormError('两次输入的新密码不一致。')
      return
    }
    setSubmitting(true)
    try {
      await changePassword(currentPassword, password)
      setAnonymous()
      navigate('/login', { replace: true, state: { notice: '密码已修改，所有设备需要重新登录。' } })
    } catch (error) {
      if (error instanceof ApiError && error.status === 401) {
        setFormError('当前密码不正确。')
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
      eyebrow="账号安全"
      title="修改登录密码"
      subtitle="修改成功后会撤销所有设备的刷新会话，并返回登录页。"
      footer={<p className="auth-foot"><Link to="/settings">返回设置</Link></p>}
    >
      <form onSubmit={onSubmit} noValidate>
        {formError && (
          <p className="auth-alert" role="alert">
            <AlertCircle aria-hidden="true" strokeWidth={2.2} />
            <span>{formError}</span>
          </p>
        )}
        <div className="field">
          <label htmlFor="current-password">当前密码</label>
          <input id="current-password" className="input" type="password" autoComplete="current-password" autoFocus required value={currentPassword} onChange={(event) => { setCurrentPassword(event.target.value); if (formError) setFormError(null) }} />
        </div>
        <div className="field">
          <label htmlFor="change-password">新密码</label>
          <input id="change-password" className="input" type="password" autoComplete="new-password" minLength={12} maxLength={72} required value={password} onChange={(event) => { setPassword(event.target.value); if (formError) setFormError(null) }} />
        </div>
        <div className="field">
          <label htmlFor="change-confirmation">再次输入新密码</label>
          <input id="change-confirmation" className="input" type="password" autoComplete="new-password" minLength={12} maxLength={72} required value={confirmation} onChange={(event) => { setConfirmation(event.target.value); if (formError) setFormError(null) }} />
        </div>
        <button className="btn btn-primary auth-btn-block" type="submit" disabled={submitting}>
          {submitting && <span className="auth-spinner" aria-hidden="true" />}
          {submitting ? '正在修改…' : '修改密码并退出所有设备'}
        </button>
      </form>
    </AuthPageShell>
  )
}
