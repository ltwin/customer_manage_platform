import { message, useOnline } from './helpers'
import { useEffect, useRef, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { useShell } from '../components/shellContext'
import ConfirmDialog from '../components/ConfirmDialog'
import * as api from './api'
import { AssetImage } from './shared'
import './creative.css'

export default function SpacesPage() {
  const navigate = useNavigate()
  const { notify } = useShell()
  const online = useOnline()
  const [pilot, setPilot] = useState<api.Pilot | null>(null)
  const [spaces, setSpaces] = useState<api.Workspace[]>([])
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const [tick, setTick] = useState(0)
  const [check, setCheck] = useState<api.Preflight | null>(null)
  const [confirmStop, setConfirmStop] = useState(false)
  const createKey = useRef(crypto.randomUUID())
  const inFlight = useRef(false)
  useEffect(() => {
    const controller = new AbortController()
    setError('')
    void api
      .pilotState(controller.signal)
      .then(async (p) => {
        const items =
          p.state === 'legacy_write'
            ? []
            : await api.listSpaces(controller.signal)
        if (!controller.signal.aborted) {
          setPilot(p)
          setSpaces(items)
        }
      })
      .catch((e: unknown) => {
        if (!controller.signal.aborted) setError(message(e))
      })
    return () => controller.abort()
  }, [tick])
  async function act(fn: () => Promise<void>) {
    if (inFlight.current || !online) return
    inFlight.current = true
    setBusy(true)
    setError('')
    try {
      await fn()
    } catch (e) {
      setError(message(e))
    } finally {
      inFlight.current = false
      setBusy(false)
    }
  }
  function create() {
    void act(async () => {
      const space = await api.createSpace(createKey.current)
      createKey.current = crypto.randomUUID()
      navigate(`/creative-workspaces/${space.id}`)
    })
  }
  function enter() {
    void act(async () => {
      const result = await api.preflight()
      setCheck(result)
      if (result.eligible) {
        await api.enroll()
        setTick((t) => t + 1)
        notify('创意空间已开启')
      }
    })
  }
  return (
    <main className="cw-page">
      <header className="cw-header">
        <div>
          <p className="cw-eyebrow">收集 · 整理 · 去拍摄</p>
          <h1>创意空间</h1>
          <p className="cw-muted">先把灵感放进来，慢慢找到想拍的画面。</p>
        </div>
        {pilot?.state === 'pilot_new_write' && (
          <button
            className="btn btn-primary"
            disabled={busy || !online}
            onClick={create}
          >
            ＋ 新建空间
          </button>
        )}
      </header>
      <div className="cw-status" aria-live="polite">
        {!online && <p>当前离线，已加载内容可查看，恢复网络后再编辑。</p>}
        {error && (
          <p role="alert">
            {error}{' '}
            <button className="btn" onClick={() => setTick((t) => t + 1)}>
              重新加载
            </button>
          </p>
        )}
        {!pilot && !error && <p>正在打开创意空间…</p>}
      </div>
      {pilot?.state === 'legacy_write' && (
        <section className="cw-onboarding">
          <h2>给零散灵感一个落点</h2>
          <p>
            把图片、链接或聊天里的几句话放在一起。挑出想拍的画面，拍摄时直接看图。
          </p>
          <p>
            开启后，旧策划记录会保留为只读。先完成进行中的旧策划，再进入创意空间。
          </p>
          <button
            className="btn btn-primary"
            disabled={busy || !online || !pilot.can_enroll}
            onClick={enter}
          >
            {busy ? '正在检查…' : '检查并开启创意空间'}
          </button>
          {!pilot.can_enroll && <p>本账号尚未加入先导试用，旧策划仍可继续使用。</p>}
          {check && !check.eligible && (
            <div className="cw-checklist">
              <h3>开启前，还有这些事需要处理</h3>
              {check.items
                .filter((i) => i.count > 0)
                .map((item) => (
                  <article key={item.key}>
                    <strong>
                      {item.title} · {item.count}
                    </strong>
                    <p>{item.how_to}</p>
                  </article>
                ))}
              <Link to="/shoot-plans">前往旧策划</Link>
            </div>
          )}
        </section>
      )}
      {pilot?.state === 'stopped' && (
        <p className="cw-banner">
          创意空间已停止试用，素材和拍摄记录仍可查看。
        </p>
      )}
      <div className="cw-space-grid">
        {spaces
          .filter((s) => !s.archived)
          .map((space) => (
            <Link
              className={`cw-space ${space.kind === 'inbox' ? 'cw-inbox' : ''}`}
              key={space.id}
              to={`/creative-workspaces/${space.id}`}
            >
              <div className="cw-space-cover">
                <AssetImage
                  workspaceID={space.id}
                  assetID={space.cover_asset_id}
                  checksum={space.cover_checksum}
                  alt={api.displayName(space)}
                />
              </div>
              <div className="cw-space-info">
                <h2>{api.displayName(space)}</h2>
                <p>
                  {space.kind === 'inbox'
                    ? '还没有项目归属的日常灵感'
                    : space.link?.name || '随时开始，不必先起名字'}
                </p>
                <small>
                  {space.card_count} 张素材 · {space.shoot_item_count}{' '}
                  个要拍的画面
                </small>
              </div>
            </Link>
          ))}
      </div>
      {spaces.some((s) => s.archived) && (
        <details className="cw-archive">
          <summary>已归档的空间</summary>
          {spaces
            .filter((s) => s.archived)
            .map((s) => (
              <p key={s.id}>
                <Link to={`/creative-workspaces/${s.id}`}>
                  {api.displayName(s)}
                </Link>
              </p>
            ))}
        </details>
      )}
      <footer className="cw-footer">
        <Link to="/shoot-plans">旧策划记录 →</Link>
        {pilot?.state === 'pilot_new_write' && (
          <button
            className="btn btn-ghost"
            onClick={() => setConfirmStop(true)}
          >
            停止试用
          </button>
        )}
      </footer>
      {confirmStop && (
        <ConfirmDialog
          title="停止试用创意空间？"
          body="现有素材和拍摄记录会保留为只读。停止后不能自行重新开启。"
          confirmLabel="停止试用"
          danger
          busy={busy}
          onCancel={() => setConfirmStop(false)}
          onConfirm={() => {
            void act(async () => {
              await api.stopPilot()
              setConfirmStop(false)
              setTick((t) => t + 1)
            })
          }}
        />
      )}
    </main>
  )
}
