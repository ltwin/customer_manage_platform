import { useMemo, useState, useSyncExternalStore } from 'react'
import { Navigate, useNavigate, useSearchParams } from 'react-router-dom'
import { AlertCircle, Check, ShieldCheck, X } from 'lucide-react'
import { ApiError } from '../../api/transport.ts'
import { getAuthSnapshot, logoutSession, subscribeAuth } from '../../auth/session'
import { decideAuthorization } from '../../creative-canvas/agents/api.ts'
import AuthPageShell from './AuthPageShell'
import { callbackHost, decodeRequest } from './consentRequest.ts'
import './LoginPage.css'
import './AgentConsentPage.css'

// 外部助手的授权确认页（epic creative-mcp-server W-2/W-3）。这一页就是账号级 consent：
// 一次允许 = 全部创意能力，所以写清楚它能做什么、不能做什么、内容会去哪里。
// 视觉依据 docs/prototypes/creative-mcp/web/authorize.html。

const localErrors: Record<string, string> = {
  invalid_client: '发起连接的客户端无法识别',
  invalid_redirect_uri: '这个客户端的回调地址不在它登记的范围内',
  server_error: '授权服务暂时出错',
}

const can: [string, string][] = [
  ['读取和修改你的画布、提示词', ''],
  ['生成图片和视频', '按你的额度扣费，和你自己生成一样'],
  ['整理资产库', '包括移入回收站和恢复'],
  ['搭建 3D 场景，回看和导出分镜', '需要你打开创意空间页面'],
  ['读取平台和你自己的 Skill', ''],
]
const cannot = ['看到客户、订单、日程等经营数据', '看到你的密码，或修改账号设置']

type Phase = 'consent' | 'sending' | 'returning' | 'denied' | 'error'

export default function AgentConsentPage() {
  const [params] = useSearchParams()
  const navigate = useNavigate()
  const auth = useSyncExternalStore(subscribeAuth, getAuthSnapshot)
  const raw = params.get('request')
  const request = useMemo(() => decodeRequest(raw), [raw])
  const expired = request !== null && request.exp * 1000 < Date.now()
  const local = params.get('error')
  const [phase, setPhase] = useState<Phase>(request && !expired ? 'consent' : 'error')
  const [message, setMessage] = useState<string | null>(null)
  const email = auth.status === 'authenticated' ? auth.account.email : ''
  const name = request?.display_name ?? '外部助手'
  const target = request ? callbackHost(request.redirect_uri) : { host: '', loopback: false }

  // 通用的登录回跳只保留路径（hardening A1/A2）；授权请求包在查询串里，这一页自己带着它去登录。
  if (auth.status !== 'authenticated') {
    return <Navigate to="/login" replace state={{ from: `/oauth/consent?${params.toString()}` }} />
  }

  async function decide(decision: 'allow' | 'deny') {
    if (!raw) return
    setPhase('sending')
    setMessage(null)
    try {
      const { redirect_to } = await decideAuthorization({ request: raw, decision })
      setPhase(decision === 'allow' ? 'returning' : 'denied')
      // 命令行宿主在本机起了一个回调地址，网页宿主在自己的服务端；两种都要整页跳过去，宿主才拿得到结果。
      window.location.assign(redirect_to)
    } catch (error) {
      setPhase(error instanceof ApiError && error.status === 400 ? 'error' : 'consent')
      setMessage(error instanceof ApiError ? error.message : '网络错误，请稍后重试。')
    }
  }

  async function switchAccount() {
    await logoutSession()
    navigate('/login', { replace: true, state: { from: `/oauth/consent?${params.toString()}` } })
  }

  const pitch = (
    <>
      <h1>
        让你常用的助手，
        <br />
        直接用你的<em>创意空间</em>
      </h1>
      <p>授权后，它能读写你的画布、整理资产库，改动实时出现在浏览器里，署名为它的名字。不需要把密码交给任何助手：授权只在这个页面完成。</p>
    </>
  )

  if (phase === 'error') {
    const reason = local ? (localErrors[local] ?? '授权请求不合法') : expired ? '这个授权链接已过期' : message ?? '这个授权链接无效'
    return (
      <AuthPageShell eyebrow="外部助手授权" title="无法继续授权" subtitle={reason} pitch={pitch} legal="回到你的助手里，重新发起连接。授权链接 10 分钟内有效。">
        <div className="consent-result">
          <span className="consent-big err" aria-hidden="true">
            <AlertCircle />
          </span>
          {local && (
            <p className="consent-fine">
              错误代码 <code>{local}</code>
            </p>
          )}
        </div>
      </AuthPageShell>
    )
  }

  if (phase === 'returning' || phase === 'denied') {
    const allowed = phase === 'returning'
    return (
      <AuthPageShell
        eyebrow="外部助手授权"
        title={allowed ? `已连接 ${name}` : '已拒绝'}
        subtitle={
          allowed
            ? target.loopback
              ? `正在交回 ${name}。浏览器显示完成后，可以关掉这个页面，回到 ${name} 继续。`
              : `正在返回 ${name}…`
            : `${name} 没有拿到任何权限，可以关掉这个页面。`
        }
        pitch={pitch}
        legal="之后在「创意空间 → 外部助手」里能看到这个连接，也可以断开。"
      >
        <div className="consent-result">
          <span className={allowed ? 'consent-big' : 'consent-big no'} aria-hidden="true">
            {allowed ? <Check /> : <X />}
          </span>
        </div>
      </AuthPageShell>
    )
  }

  const verified = request?.registration === 'cimd'
  return (
    <AuthPageShell
      eyebrow="外部助手授权"
      title={`允许 ${name} 使用你的创意空间？`}
      subtitle={verified ? `客户端信息来自 ${callbackHost(request.client_id).host}` : '这个客户端是自行注册的，名称无法验证'}
      pitch={pitch}
      legal={
        <>
          授权后回到 <code>{target.host}</code> · 随时可以在「创意空间 → 外部助手」里断开
        </>
      }
    >
      <div className="consent">
        {!verified && (
          <p className="auth-alert consent-warn" role="note">
            <AlertCircle aria-hidden="true" strokeWidth={2.2} />
            <span>只有你刚刚自己在 {request?.client_name} 里发起了连接，才点「允许」。不认识这个名字就拒绝。</span>
          </p>
        )}
        {message && (
          <p className="auth-alert" role="alert">
            <AlertCircle aria-hidden="true" strokeWidth={2.2} />
            <span>{message}</span>
          </p>
        )}
        <div className="consent-account">
          <span className="consent-av" aria-hidden="true">
            {email.slice(0, 1).toUpperCase()}
          </span>
          <div>
            <b>{email}</b>
            <small>授权给这个账号</small>
          </div>
          <button type="button" className="consent-text-btn" onClick={() => void switchAccount()}>
            切换账号
          </button>
        </div>
        <div className="consent-scope">
          <div className="consent-can">
            <h3>授权后它可以</h3>
            <ul>
              {can.map(([text, note]) => (
                <li key={text}>
                  <Check aria-hidden="true" />
                  <span>
                    {text}
                    {note && (
                      <>
                        <br />
                        <small>{note}</small>
                      </>
                    )}
                  </span>
                </li>
              ))}
            </ul>
          </div>
          <div className="consent-cannot">
            <h3>它不能</h3>
            <ul>
              {cannot.map((text) => (
                <li key={text}>
                  <X aria-hidden="true" />
                  <span>{text}</span>
                </li>
              ))}
            </ul>
          </div>
          <p className="consent-egress">
            <ShieldCheck aria-hidden="true" />
            <span>它读到的画布、素材和文字会发送给 {name} 使用的 AI 模型服务。</span>
          </p>
        </div>
        <div className="consent-actions">
          <button type="button" className="btn" disabled={phase === 'sending'} onClick={() => void decide('deny')}>
            拒绝
          </button>
          <button type="button" className="btn btn-primary" disabled={phase === 'sending'} onClick={() => void decide('allow')}>
            {phase === 'sending' ? '正在授权…' : '允许'}
          </button>
        </div>
      </div>
    </AuthPageShell>
  )
}
