import { useLayoutEffect, useRef, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { AlertCircle } from 'lucide-react'
import { ApiError, verifyEmail } from '../../api/client'
import { consumeActionToken } from '../../auth/actionToken'
import { establishSession } from '../../auth/session'
import AuthPageShell from './AuthPageShell'
import './LoginPage.css'

type VerificationState = 'verifying' | 'failed'

export default function VerifyEmailPage() {
  const [state, setState] = useState<VerificationState>('verifying')
  const [message, setMessage] = useState('正在验证邮箱并建立安全会话…')
  const started = useRef(false)
  const navigate = useNavigate()

  useLayoutEffect(() => {
    if (started.current) return
    started.current = true
    const token = consumeActionToken()
    if (!token) {
      setState('failed')
      setMessage('验证链接无效或缺少必要信息，请重新获取验证邮件。')
      return
    }

    void establishSession(() => verifyEmail(token))
      .then((established) => {
        if (established) navigate('/dashboard', { replace: true })
      })
      .catch((error: unknown) => {
        setState('failed')
        if (error instanceof ApiError && error.code === 'invalid_or_expired_token') {
          setMessage('验证链接无效或已过期，请重新获取验证邮件。')
          return
        }
        setMessage('暂时无法完成验证，请稍后重试。')
      })
  }, [navigate])

  return (
    <AuthPageShell
      eyebrow="邮箱验证"
      title={state === 'verifying' ? '正在完成验证' : '未能完成验证'}
      subtitle="验证凭证只从当前页面片段读取一次，并已从地址栏移除。"
      footer={
        state === 'failed' ? (
          <p className="auth-foot">
            <Link to="/register">返回注册页</Link>　·　<Link to="/login">前往登录</Link>
          </p>
        ) : undefined
      }
    >
      <p
        className={state === 'failed' ? 'auth-alert' : 'auth-status'}
        role={state === 'failed' ? 'alert' : 'status'}
        aria-live="polite"
      >
        {state === 'failed' && <AlertCircle aria-hidden="true" strokeWidth={2.2} />}
        <span>{message}</span>
      </p>
    </AuthPageShell>
  )
}
