import type { ReactNode } from 'react'
import { Link } from 'react-router-dom'
import { Camera, Check, Sparkles } from 'lucide-react'

type AuthPageShellProps = {
  eyebrow: string
  title: string
  subtitle: string
  children: ReactNode
  footer?: ReactNode
}

export default function AuthPageShell({
  eyebrow,
  title,
  subtitle,
  children,
  footer,
}: AuthPageShellProps) {
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
            {eyebrow}
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
                <b>客户画像自动累积</b>　生日、偏好、复购节奏都留在档案里
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
                <b>该跟进的人不会漏</b>　定金、修图、回访到点提醒
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
            <h2>{title}</h2>
            <p>{subtitle}</p>
          </header>
          {children}
          {footer}
          <p className="auth-legal">
            客户资料仅存于你的账号内，不做跨账号共享，可随时整包导出。
          </p>
        </div>
      </main>
    </div>
  )
}
