import { useState } from 'react'
import type { FormEvent } from 'react'
import { Link, useLocation, useNavigate } from 'react-router-dom'
import { AlertCircle, Camera, Check, Sparkles } from 'lucide-react'
import { ApiError, login } from '../api/client'
import { setToken } from '../auth/token'
import './LoginPage.css'

type Channel = 'password' | 'code'

export default function LoginPage() {
  const [channel, setChannel] = useState<Channel>('password')
  const [password, setPassword] = useState('')
  const [fieldError, setFieldError] = useState<string | null>(null)
  const [formError, setFormError] = useState<string | null>(null)
  const [submitting, setSubmitting] = useState(false)
  const navigate = useNavigate()
  const location = useLocation()

  async function onSubmit(e: FormEvent) {
    e.preventDefault()
    setFormError(null)

    // 字段级校验：提交前拦下明显不合法的输入，不打网络
    if (password === '') {
      setFieldError('请输入登录密码')
      return
    }
    setFieldError(null)

    setSubmitting(true)
    try {
      const { token } = await login(password)
      setToken(token)
      // 未登录时 / 是欢迎页，登录后默认落到经营台
      const from = (location.state as { from?: string } | null)?.from ?? '/dashboard'
      navigate(from, { replace: true })
    } catch (err) {
      // 401 归因到密码字段；其余作为整体错误提示
      if (err instanceof ApiError && err.status === 401) {
        setFieldError('密码不正确')
        setFormError('密码不正确，请重新输入。')
      } else if (err instanceof ApiError) {
        setFormError(err.message)
      } else {
        setFormError('网络错误，请稍后重试。')
      }
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <div className="auth-shell">
      <aside className="auth-aside" data-od-id="auth-aside">
        <img className="auth-aside-photo" src="/marketing/hero-studio.jpg" alt="柔光影棚一角" />
        <div className="auth-aside-veil" />

        <Link className="auth-brand" to="/" data-od-id="brand-home">
          <span className="brand-mark" aria-hidden="true">
            <Camera strokeWidth={2} />
          </span>
          <span>
            影约 CRM<small>摄影师私域客户经营</small>
          </span>
        </Link>

        <div className="auth-pitch" data-od-id="aside-pitch">
          <span className="auth-eyebrow">
            <Sparkles aria-hidden="true" />
            欢迎回来
          </span>
          <h1>
            把客户、档期、套系
            <br />
            都收进<em>一个工作台</em>
          </h1>
          <p>散落在微信、QQ、Telegram 的聊天记录，不该是你唯一的客户档案。</p>

          <ul className="auth-proof">
            <li>
              <span className="auth-proof-dot" aria-hidden="true">
                <Check strokeWidth={3} />
              </span>
              <span>
                <b>客户画像自动累积</b>　每单拍完，生日、偏好、复购节奏都留在档案里
              </span>
            </li>
            <li>
              <span className="auth-proof-dot" aria-hidden="true">
                <Check strokeWidth={3} />
              </span>
              <span>
                <b>档期冲突当场拦下</b>　同一时段重复约拍，保存前就会被拦住
              </span>
            </li>
            <li>
              <span className="auth-proof-dot" aria-hidden="true">
                <Check strokeWidth={3} />
              </span>
              <span>
                <b>该跟进的人不会漏</b>　定金、修图、回访到点提醒推到 Telegram
              </span>
            </li>
          </ul>
        </div>

        <div className="auth-aside-foot" data-od-id="aside-foot">
          <span className="auth-avatars" aria-hidden="true">
            <img src="/marketing/av-1.jpg" alt="" />
            <img src="/marketing/av-2.jpg" alt="" />
            <img src="/marketing/av-3.jpg" alt="" />
            <img src="/marketing/av-4.jpg" alt="" />
            <span>+12</span>
          </span>
          <span>写真 · Cosplay · 情绪片摄影师正在用它管客户</span>
        </div>
      </aside>

      <main className="auth-main" data-od-id="auth-main">
        <div className="auth-card">
          <header>
            <h2>登录你的经营台</h2>
            <p>工作室单账号登录，暂不支持自助注册。</p>
          </header>

          <div className="auth-tabs" role="tablist" aria-label="登录方式">
            <button
              type="button"
              role="tab"
              id="auth-tab-password"
              className="auth-tab"
              aria-selected={channel === 'password'}
              aria-controls="auth-panel-password"
              onClick={() => setChannel('password')}
              data-od-id="tab-password"
            >
              账号密码
            </button>
            <button
              type="button"
              role="tab"
              id="auth-tab-code"
              className="auth-tab"
              aria-selected={channel === 'code'}
              aria-controls="auth-panel-code"
              onClick={() => setChannel('code')}
              data-od-id="tab-phone-code"
            >
              手机号验证码
              <span className="auth-tab-soon">即将开放</span>
            </button>
          </div>

          {channel === 'password' ? (
            <form
              id="auth-panel-password"
              role="tabpanel"
              aria-labelledby="auth-tab-password"
              onSubmit={onSubmit}
              noValidate
              data-od-id="form-password"
            >
              {formError && (
                <p className="auth-alert" role="alert">
                  <AlertCircle aria-hidden="true" strokeWidth={2.2} />
                  <span>{formError}</span>
                </p>
              )}

              <div className={fieldError ? 'field show-err' : 'field'}>
                <label htmlFor="password">登录密码</label>
                <input
                  id="password"
                  className={fieldError ? 'input invalid' : 'input'}
                  type="password"
                  value={password}
                  autoComplete="current-password"
                  aria-invalid={fieldError ? true : undefined}
                  aria-describedby={fieldError ? 'password-err' : undefined}
                  autoFocus
                  onChange={(e) => {
                    setPassword(e.target.value)
                    // 输入即消解错误，不让红框黏着用户
                    if (fieldError) setFieldError(null)
                    if (formError) setFormError(null)
                  }}
                />
                <span className="err" id="password-err">
                  {fieldError}
                </span>
              </div>

              <div className="auth-row-between">
                <span className="auth-checkbox">登录状态保留在本机浏览器</span>
              </div>

              <button
                className="btn btn-primary auth-btn-block"
                type="submit"
                disabled={submitting}
                data-od-id="submit-password"
              >
                {submitting && <span className="auth-spinner" aria-hidden="true" />}
                {submitting ? '登录中…' : '登录'}
              </button>
            </form>
          ) : (
            <div
              id="auth-panel-code"
              role="tabpanel"
              aria-labelledby="auth-tab-code"
              className="auth-soon-note"
              data-od-id="panel-phone-code"
            >
              <b>手机号验证码登录即将开放。</b>
              <br />
              该通道需要后端提供发码与校验接口，当前版本尚未开通。请先使用账号密码登录。
            </div>
          )}

          <p className="auth-foot">
            还没有账号？
            <Link to="/" data-od-id="link-apply">
              了解影约 CRM
            </Link>
          </p>

          <p className="auth-legal">
            登录即代表同意服务协议与隐私政策。客户资料仅存于你的账号内，不做跨账号共享。
          </p>
        </div>
      </main>
    </div>
  )
}
