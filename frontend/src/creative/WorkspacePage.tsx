import { message, useOnline } from './helpers'
import { useObservations } from './useObservations'
import { useEffect, useRef, useState } from 'react'
import { Link, useParams, useSearchParams, useNavigate } from 'react-router-dom'
import { useShell } from '../components/shellContext'
import ConfirmDialog from '../components/ConfirmDialog'
import { listCustomers, listOrders } from '../api/client'
import * as api from './api'
import { AssetImage, WorkspaceMediaCache } from './shared'
import './creative.css'

type FileJob = { file: File; asset?: api.Asset; error?: string }

export default function WorkspacePage() {
  const { id = '' } = useParams()
  return (
    <WorkspaceMediaCache key={id}>
      <WorkspaceContents />
    </WorkspaceMediaCache>
  )
}
function WorkspaceContents() {
 const navigate = useNavigate()
 const [leaveTo, setLeaveTo] = useState<string | null>(null)
  const { id = '' } = useParams()
  const [params, setParams] = useSearchParams()
  const { notify } = useShell()
  const online = useOnline()
  const view = params.get('view') ?? 'wall'
  const [data, setData] = useState<api.Detail | null>(null)
  const [pilot, setPilot] = useState<api.Pilot | null>(null)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const [tick, setTick] = useState(0)
  const [raw, setRaw] = useState('')
  const [memo, setMemo] = useState('')
  const [name, setName] = useState('')
  const [selected, setSelected] = useState<string[]>([])
  const [groupName, setGroupName] = useState('')
  const [group, setGroup] = useState('')
  const [jobs, setJobs] = useState<FileJob[]>([])
  const [progress, setProgress] = useState('')
  const [dragged, setDragged] = useState('')
  const [remove, setRemove] = useState<api.Command | null>(null)
  const [current, setCurrent] = useState('')
  const [note, setNote] = useState('')
  const [moveTo, setMoveTo] = useState('')
  const [spaces, setSpaces] = useState<api.Workspace[]>([])
  const keys = useRef(new Map<string, string>())
  const pending = useRef(false)
  const fileInput = useRef<HTMLInputElement>(null)
  const writable =
    pilot?.state === 'pilot_new_write' &&
    !data?.workspace.archived &&
    online &&
    !busy
  const pendingLive = useObservations(
    id,
    view,
    online,
    !!data && pilot?.state === 'pilot_new_write',
  )
  const dirty = !!raw || !!memo || jobs.length > 0
  useEffect(() => {
    const controller = new AbortController()
    setError('')
    void Promise.all([
      api.detail(id, controller.signal),
      api.pilotState(controller.signal),
      api.listSpaces(controller.signal),
    ])
      .then(([d, p, s]) => {
        if (!controller.signal.aborted) {
          setData(d)
          setPilot(p)
          setSpaces(s)
          setName(d.workspace.name)
        }
      })
      .catch((e: unknown) => {
        if (!controller.signal.aborted) setError(message(e))
      })
    return () => controller.abort()
  }, [id, tick])
  useEffect(() => {
    if (!dirty) return
    const warn = (e: BeforeUnloadEvent) => e.preventDefault()
    window.addEventListener('beforeunload', warn)
    return () => window.removeEventListener('beforeunload', warn)
  }, [dirty])
  useEffect(() => {
    if (!dirty) return
    const intercept = (event: MouseEvent) => {
      if (event.defaultPrevented || event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return
      const anchor = event.target instanceof Element ? event.target.closest<HTMLAnchorElement>('a[href]') : null
      if (!anchor || anchor.target === '_blank' || anchor.hasAttribute('download')) return
      const url = new URL(anchor.href)
      if (url.origin !== location.origin || url.pathname === location.pathname) return
      event.preventDefault(); event.stopPropagation(); setLeaveTo(url.pathname + url.search + url.hash)
    }
    document.addEventListener('click', intercept, true)
    return () => document.removeEventListener('click', intercept, true)
  }, [dirty])
  function keyFor(body: unknown) {
    const value = JSON.stringify(body)
    let key = keys.current.get(value)
    if (!key) {
      key = crypto.randomUUID()
      keys.current.set(value, key)
    }
    return { key, clear: () => keys.current.delete(value) }
  }
  async function act(fn: () => Promise<void>) {
    if (pending.current || !online) return
    pending.current = true
    setBusy(true)
    setError('')
    try {
      await fn()
    } catch (e) {
      setError(message(e))
    } finally {
      pending.current = false
      setBusy(false)
      setProgress('')
    }
  }
  async function mutate(body: api.Command, success = '已保存') {
    const d = await api.command(id, body)
    setData(d)
    notify(success)
  }
  async function refresh() {
    setData(await api.detail(id))
  }
  function select(cardID: string) {
    setSelected((ids) =>
      ids.includes(cardID) ? ids.filter((i) => i !== cardID) : [...ids, cardID],
    )
  }
  function addFiles(files: FileList | File[]) {
    if (!writable) return
    const incoming = Array.from(files)
    if (jobs.length + incoming.length > 50) {
      setError('每次最多搬入 50 张图片')
      return
    }
    setJobs((old) => [
      ...old,
      ...incoming.map((file) => ({
        file,
        error: !['image/jpeg', 'image/png', 'image/webp'].includes(file.type)
          ? '暂不支持此格式，请使用 JPG、PNG 或 WebP 图片'
          : undefined,
      })),
    ])
  }
  async function importAll() {
    const next = [...jobs]
    for (let i = 0; i < next.length; i++) {
      const job = next[i]
      if (
        job.asset ||
        !['image/jpeg', 'image/png', 'image/webp'].includes(job.file.type)
      )
        continue
      setProgress(`正在上传第 ${i + 1} / ${next.length} 张图片`)
      try {
        next[i] = { file: job.file, asset: await api.upload(id, job.file) }
      } catch (e) {
        next[i] = { file: job.file, error: message(e) }
      }
      setJobs([...next])
    }
    const assets = next.flatMap((j) => (j.asset ? [j.asset.id] : []))
    if (!raw.trim() && assets.length === 0) return
    const body = { raw_text: raw, asset_ids: assets }
    const attempt = keyFor(['import', body])
    const result = await api.importCards(id, body, attempt.key)
    attempt.clear()
    setRaw('')
    setJobs(next.filter((j) => !j.asset))
    await refresh()
    notify(`已搬入 ${result.cards.length} 张素材`)
  }
  async function shoot(merge: boolean) {
    const attempt = keyFor(['shots', selected, merge])
    await api.createShots(id, selected, merge, attempt.key)
    attempt.clear()
    setSelected([])
    await refresh()
    setParams({ view: 'shots' })
    notify('已加入拍摄清单')
  }
  function setView(next: string) {
    setParams(next === 'wall' ? {} : { view: next })
  }
  if (!data)
    return (
      <main className="cw-page">
        <Link to="/creative-workspaces">← 创意空间</Link>
        <p role={error ? 'alert' : 'status'}>{error || '正在打开空间…'}</p>
        {error && (
          <button className="btn" onClick={() => setTick((t) => t + 1)}>
            重新加载
          </button>
        )}
      </main>
    )
  const { workspace, cards, groups, shoot_items: shots, memos } = data
  const shown = group ? cards.filter((c) => c.group_id === group) : cards
  const item =
    shots.find((s) => s.id === current) ??
    shots.find((s) => !s.result) ??
    shots[0]
  const readOnly = pilot?.state !== 'pilot_new_write' || workspace.archived
  return (
    <main
      className={`cw-page ${view === 'live' ? 'cw-live' : ''}`}
      onDragOver={(e) => {
        if (e.dataTransfer.types.includes('Files')) e.preventDefault()
      }}
      onDrop={(e) => {
        if (e.dataTransfer.files.length) {
          e.preventDefault()
          addFiles(e.dataTransfer.files)
        }
      }}
    >
      <Link className="cw-back" to="/creative-workspaces">
        ← 所有空间
      </Link>
      <header className="cw-header">
        <div>
          <p className="cw-eyebrow">
            {view === 'live'
              ? '在现场'
              : workspace.kind === 'inbox'
                ? '随手收集'
                : '创意空间'}
          </p>
          <h1>{api.displayName(workspace)}</h1>
          {workspace.link && (
            <Link
              className="cw-muted"
              to={
                workspace.link.kind === 'order'
                  ? `/orders?order=${workspace.link.id}`
                  : `/customers/${workspace.link.id}`
              }
            >
              {workspace.link.broken ? '原关联已失效' : workspace.link.name} ↗
            </Link>
          )}
        </div>
        {view === 'live' ? (
          <button className="btn" onClick={() => setView('wall')}>
            返回创作
          </button>
        ) : (
          <button
            className="btn btn-primary"
            disabled={shots.length === 0}
            onClick={() => setView('live')}
          >
            进入现场 →
          </button>
        )}
      </header>
      <div className="cw-status" aria-live="polite">
        {error && (
          <p role="alert">
            {error}{' '}
            <button className="btn" onClick={() => setTick((t) => t + 1)}>
              刷新内容
            </button>
          </p>
        )}
        {!online && <p>当前离线，清单可查看。完成、跳过和备忘尚不能保存。</p>}
        {pendingLive && (
          <p>
            本次离线打开已暂记，保持页面打开，联网后补报；拍摄结果仍需联网后手动记录。
          </p>
        )}
        {readOnly && (
          <p>
            当前空间只读。
            {workspace.archived && pilot?.state === 'pilot_new_write' && (
              <button
                className="btn"
                disabled={!online || busy}
                onClick={() => {
                  void act(() =>
                    mutate(
                      {
                        operation: 'update_workspace',
                        archived: false,
                        expected_revision: workspace.revision,
                      },
                      '空间已恢复',
                    ),
                  )
                }}
              >
                恢复空间
              </button>
            )}
          </p>
        )}
        {progress && <p role="status">{progress}</p>}
      </div>
      {view !== 'live' && (
        <>
          <nav className="cw-tabs" aria-label="空间内容">
            {[
              ['wall', '灵感墙'],
              ['shots', `拍摄清单 · ${shots.length}`],
              ['memos', '拍摄备忘'],
            ].map(([v, label]) => (
              <button
                className={view === v ? 'active' : ''}
                key={v}
                onClick={() => setView(v)}
                aria-current={view === v ? 'page' : undefined}
              >
                {label}
              </button>
            ))}
          </nav>
          <details className="cw-organize">
            <summary>空间名称与关联</summary>
            <form
              noValidate
              onSubmit={(e) => {
                e.preventDefault()
                void act(() =>
                  mutate({
                    operation: 'update_workspace',
                    name,
                    expected_revision: workspace.revision,
                  }),
                )
              }}
            >
              <label>
                空间名称
                <input
                  className="input"
                  value={name}
                  onChange={(e) => setName(e.target.value)}
                  maxLength={80}
                  disabled={!writable || workspace.kind === 'inbox'}
                />
              </label>
              <button
                className="btn"
                disabled={!writable || workspace.kind === 'inbox'}
              >
                保存名称
              </button>
            </form>
            {workspace.kind !== 'inbox' && (
              <LinkEditor
                disabled={!writable}
                linked={!!workspace.link}
                save={(value) => {
                  void act(() =>
                    mutate({
                      operation: 'update_workspace',
                      link: value,
                      expected_revision: workspace.revision,
                    }),
                  )
                }}
              />
            )}
            {workspace.kind !== 'inbox' && !workspace.archived && (
              <button
                className="btn btn-ghost"
                disabled={!writable}
                onClick={() => {
                  void act(() =>
                    mutate(
                      {
                        operation: 'update_workspace',
                        archived: true,
                        expected_revision: workspace.revision,
                      },
                      '已归档，素材仍保留，可随时恢复',
                    ),
                  )
                }}
              >
                归档空间
              </button>
            )}
          </details>
        </>
      )}
      {view === 'wall' && (
        <>
          {!readOnly && (
            <section className="cw-import">
              <form
                noValidate
                onSubmit={(e) => {
                  e.preventDefault()
                  void act(importAll)
                }}
              >
                <label htmlFor="cw-paste">把灵感放进来</label>
                <textarea
                  id="cw-paste"
                  className="input resize-none"
                  rows={3}
                  style={{ resize: 'none' }}
                  placeholder="粘贴一段聊天、几条想法或链接，每一行成为一张卡片。也可以把图片拖到这里。"
                  value={raw}
                  disabled={!writable}
                  onChange={(e) => setRaw(e.target.value)}
                />
                <div className="cw-toolbar">
                  <input
                    ref={fileInput}
                    type="file"
                    hidden
                    accept="image/jpeg,image/png,image/webp"
                    multiple
                    onChange={(e) => {
                      if (e.target.files) addFiles(e.target.files)
                      e.target.value = ''
                    }}
                  />
                  <button
                    type="button"
                    className="btn"
                    disabled={!writable}
                    onClick={() => fileInput.current?.click()}
                  >
                    ＋ 选择图片
                  </button>
                  <small>JPG、PNG、WebP · 每次最多 50 张</small>
                  <button
                    className="btn btn-primary"
                    disabled={!writable || (!raw.trim() && !jobs.length)}
                  >
                    搬入素材
                  </button>
                </div>
                {jobs.length > 0 && (
                  <ul className="cw-upload-list">
                    {jobs.map((job, i) => (
                      <li key={`${job.file.name}-${i}`}>
                        <span>{job.file.name}</span>
                        <small>
                          {job.asset
                            ? '已上传，等待成卡'
                            : (job.error ?? '等待上传')}
                        </small>
                        <button
                          type="button"
                          className="btn btn-ghost"
                          disabled={busy}
                          onClick={() =>
                            setJobs((js) => js.filter((_, at) => at !== i))
                          }
                        >
                          移出队列
                        </button>
                      </li>
                    ))}
                  </ul>
                )}
              </form>
            </section>
          )}
          <div className="cw-toolbar">
            <span className="cw-muted">{cards.length} 张素材</span>
            <label>
              分组{' '}
              <select
                className="input"
                value={group}
                onChange={(e) => setGroup(e.target.value)}
              >
                <option value="">全部素材</option>
                {groups.map((g) => (
                  <option value={g.id} key={g.id}>
                    {g.name || '未命名分组'}
                  </option>
                ))}
              </select>
            </label>
            {group && (
              <button
                className="btn"
                disabled={!writable}
                onClick={() => {
                  void act(async () => {
                    await mutate(
                      { operation: 'delete_group', id: group },
                      '分组已解散，素材保留',
                    )
                    setGroup('')
                  })
                }}
              >
                解散分组
              </button>
            )}
          </div>
          {selected.length > 0 && (
            <section className="cw-selection">
              <strong>已选 {selected.length} 张</strong>
              <button
                className="btn btn-primary"
                disabled={!writable}
                onClick={() => {
                  void act(() => shoot(false))
                }}
              >
                加入拍摄清单
              </button>
              <button
                className="btn"
                disabled={!writable}
                onClick={() => {
                  void act(() => shoot(true))
                }}
              >
                合成一个画面
              </button>
              <button className="btn btn-ghost" onClick={() => setSelected([])}>
                取消选择
              </button>
              <form
                noValidate
                onSubmit={(e) => {
                  e.preventDefault()
                  void act(async () => {
                    await mutate(
                      {
                        operation: 'create_group',
                        ids: selected,
                        name: groupName,
                      },
                      '已建立分组',
                    )
                    setSelected([])
                    setGroupName('')
                  })
                }}
              >
                <label>
                  新分组
                  <input
                    className="input"
                    value={groupName}
                    onChange={(e) => setGroupName(e.target.value)}
                    maxLength={40}
                  />
                </label>
                <button className="btn" disabled={!writable}>
                  成组
                </button>
              </form>
              {groups.length > 0 && (
                <label>
                  放入分组
                  <select
                    className="input"
                    value=""
                    disabled={!writable}
                    onChange={(e) => {
                      const value = e.target.value
                      void act(async () => {
                        await mutate({
                          operation: 'set_group',
                          ids: selected,
                          group_id: value === 'none' ? null : value,
                        })
                        setSelected([])
                      })
                    }}
                  >
                    <option value="" disabled>
                      选择分组
                    </option>
                    <option value="none">移出分组</option>
                    {groups.map((g) => (
                      <option key={g.id} value={g.id}>
                        {g.name || '未命名分组'}
                      </option>
                    ))}
                  </select>
                </label>
              )}
            </section>
          )}
          {shown.length === 0 && (
            <section className="cw-empty">
              <h2>
                {cards.length ? '这个分组还没有素材' : '灵感不必一开始就有条理'}
              </h2>
              <p>先放一张参考图，或粘贴一段聊天。想拍什么，可以慢慢挑。</p>
            </section>
          )}
          <div className="cw-wall">
            {shown.map((card) => (
              <article
                id={`card-${card.id}`}
                className={`cw-card ${selected.includes(card.id) ? 'selected' : ''}`}
                key={card.id}
                draggable={writable}
                onDragStart={(e) => {
                  setDragged(card.id)
                  e.dataTransfer.setData('text/plain', card.id)
                }}
                onDragOver={(e) => {
                  if (dragged) e.preventDefault()
                }}
                onDrop={(e) => {
                  if (!dragged) return
                  e.preventDefault()
                  e.stopPropagation()
                  const ids = cards
                    .filter((c) => c.id !== dragged)
                    .map((c) => c.id)
                  ids.splice(ids.indexOf(card.id), 0, dragged)
                  setDragged('')
                  void act(() => mutate({ operation: 'reorder_cards', ids }))
                }}
                onDragEnd={() => setDragged('')}
              >
                <button
                  className="cw-card-main"
                  aria-label={`选择素材：${card.text || card.caption || card.url || '参考图'}`}
                  aria-pressed={selected.includes(card.id)}
                  disabled={readOnly}
                  onClick={() => select(card.id)}
                >
                  {card.type === 'image' ? (
                    <AssetImage
                      workspaceID={id}
                      assetID={card.asset_id}
                      checksum={card.display_checksum}
                      alt={card.caption || '拍摄参考图'}
                    />
                  ) : (
                    <span className={`cw-card-copy ${card.type}`}>
                      <small>{card.type === 'link' ? '链接' : '灵感'}</small>
                      <span>{card.text || card.url}</span>
                    </span>
                  )}
                  <span className="cw-select-mark" aria-hidden="true">
                    {selected.includes(card.id) ? '✓' : '＋'}
                  </span>
                </button>
                {card.type === 'link' && (
                  <a
                    className="cw-card-link"
                    href={card.url}
                    target="_blank"
                    rel="noreferrer"
                  >
                    打开原链接 ↗
                  </a>
                )}
                {card.caption && <p className="cw-caption">{card.caption}</p>}
                <details className="cw-card-options">
                  <summary>整理素材</summary>
                  <CardEditor
                    card={card}
                    disabled={!writable}
                    save={(body) => {
                      void act(() => mutate(body))
                    }}
                  />
                  <div className="cw-toolbar">
                    <button
                      className="btn"
                      disabled={!writable || cards[0]?.id === card.id}
                      onClick={() => {
                        void act(() =>
                          mutate({
                            operation: 'reorder_cards',
                            ids: [card.id],
                          }),
                        )
                      }}
                    >
                      移到最前
                    </button>
                    <button
                      className="btn btn-danger-ghost"
                      disabled={!writable}
                      onClick={() =>
                        setRemove({ operation: 'remove_card', id: card.id })
                      }
                    >
                      移除素材
                    </button>
                  </div>
                  <label>
                    挪到其他空间
                    <select
                      className="input"
                      disabled={!writable}
                      value={moveTo}
                      onChange={(e) => setMoveTo(e.target.value)}
                    >
                      <option value="">选择空间</option>
                      {spaces
                        .filter((s) => s.id !== id && !s.archived)
                        .map((s) => (
                          <option key={s.id} value={s.id}>
                            {api.displayName(s)}
                          </option>
                        ))}
                    </select>
                  </label>
                  <button
                    className="btn"
                    disabled={!writable || !moveTo}
                    onClick={() => {
                      void act(async () => {
                        await mutate(
                          {
                            operation: 'move_card',
                            id: card.id,
                            to_workspace_id: moveTo,
                          },
                          '素材已挪走',
                        )
                        setMoveTo('')
                        setSelected((ids) => ids.filter((v) => v !== card.id))
                      })
                    }}
                  >
                    挪过去
                  </button>
                </details>
              </article>
            ))}
          </div>
        </>
      )}
      {view === 'shots' && (
        <section className="cw-shots">
          {shots.length === 0 && (
            <div className="cw-empty">
              <h2>挑出想拍的画面</h2>
              <p>回到灵感墙，选择参考素材，再加入拍摄清单。</p>
              <button className="btn" onClick={() => setView('wall')}>
                去灵感墙
              </button>
            </div>
          )}
          {shots.map((shot) => (
            <article className="cw-shot" key={shot.id}>
              <div className="cw-shot-image">
                <AssetImage
                  workspaceID={id}
                  assetID={shot.ref_asset_id}
                  checksum={shot.ref_display_checksum}
                  alt={shot.title}
                />
              </div>
              <div>
                <h2>{shot.title}</h2>
                <p>{shot.description}</p>
                <p className="cw-muted">
                  {shot.result === 'done'
                    ? '已拍'
                    : shot.result === 'skipped'
                      ? '已跳过'
                      : '还想拍'}
                  {shot.result_note && ` · ${shot.result_note}`}
                </p>
                <button
                  className="btn"
                  onClick={() => {
                    setCurrent(shot.id)
                    setView('live')
                  }}
                >
                  现场查看
                </button>
                {shot.source_state === 'available' ? (
                  <button
                    className="btn btn-ghost"
                    onClick={() => {
                      if (shot.source_workspace_id && shot.source_workspace_id !== id) { navigate(`/creative-workspaces/${shot.source_workspace_id}#card-${shot.source_card_id ?? ''}`); return }
                        setSelected(
                        shot.source_card_id ? [shot.source_card_id] : [],
                      )
                      setView('wall')
                    }}
                  >
                    查看原始灵感
                  </button>
                ) : (
                  <span className="cw-muted">原始灵感已不可用，画面保留</span>
                )}
                <details>
                  <summary>编辑拍摄项</summary>
                  <ShotEditor
                    shot={shot}
                    disabled={!writable}
                    save={(body) => {
                      void act(() => mutate(body))
                    }}
                  />
                  <button
                    className="btn"
                    disabled={!writable || shots[0]?.id === shot.id}
                    onClick={() => {
                      void act(() =>
                        mutate({
                          operation: 'reorder_shoot_items',
                          ids: [shot.id],
                        }),
                      )
                    }}
                  >
                    排到最前
                  </button>
                  <button
                    className="btn btn-danger-ghost"
                    disabled={!writable}
                    onClick={() =>
                      setRemove({
                        operation: 'remove_shoot_item',
                        id: shot.id,
                        expected_revision: shot.revision,
                      })
                    }
                  >
                    移出清单
                  </button>
                </details>
              </div>
            </article>
          ))}
        </section>
      )}
      {(view === 'memos' || view === 'live') && (
        <details open={view === 'memos'} className="cw-memos">
          <summary>拍摄备忘 · {memos.length}</summary>
          <p className="cw-muted">给自己记几件事，随时勾掉，不影响拍摄。</p>
          {memos.map((m) => (
            <div className="cw-memo" key={m.id}>
              <label>
                <input
                  type="checkbox"
                  checked={m.checked}
                  disabled={!writable}
                  onChange={(e) => {
                    const checked = e.target.checked
                    void act(() =>
                      mutate({ operation: 'update_memo', id: m.id, checked }),
                    )
                  }}
                />
                <span className={m.checked ? 'checked' : ''}>{m.text}</span>
              </label>
              <button
                className="btn btn-ghost"
                disabled={!writable || memos[0]?.id === m.id}
                onClick={() => {
                  void act(() =>
                    mutate({ operation: 'reorder_memos', ids: [m.id] }),
                  )
                }}
              >
                置顶
              </button>
              <button
                className="btn btn-ghost"
                disabled={!writable}
                onClick={() =>
                  setRemove({ operation: 'delete_memo', id: m.id })
                }
              >
                移除
              </button>
            </div>
          ))}
          {!readOnly && (
            <form
              noValidate
              onSubmit={(e) => {
                e.preventDefault()
                void act(async () => {
                  const attempt = keyFor(['memo', memo])
                  await api.createMemos(id, memo, attempt.key)
                  attempt.clear()
                  setMemo('')
                  await refresh()
                  notify('备忘已记下')
                })
              }}
            >
              <label htmlFor="cw-memo">新增备忘，一行一条</label>
              <textarea
                id="cw-memo"
                className="input resize-none"
                rows={3}
                style={{ resize: 'none' }}
                value={memo}
                disabled={!writable}
                onChange={(e) => setMemo(e.target.value)}
                placeholder="备用电池&#10;租的伞记得还"
              />
              <button className="btn" disabled={!writable || !memo.trim()}>
                记下备忘
              </button>
            </form>
          )}
        </details>
      )}
      {view === 'live' &&
        (item ? (
          <section className="cw-live-stage">
            <div className="cw-live-image">
              <AssetImage
                workspaceID={id}
                assetID={item.ref_asset_id}
                checksum={item.ref_display_checksum}
                alt={item.title}
              />
            </div>
            <div className="cw-live-caption">
              <h2>{item.title}</h2>
              <p>{item.description}</p>
              {item.result_note && <p>{item.result_note}</p>}
            </div>
            <nav className="cw-live-picker" aria-label="选择拍摄画面">
              {shots.map((s, i) => (
                <button
                  className={s.id === item.id ? 'active' : ''}
                  key={s.id}
                  aria-label={`${s.title}，${s.result === 'done' ? '已拍' : s.result === 'skipped' ? '已跳过' : '待拍'}`}
                  aria-pressed={s.id === item.id}
                  onClick={() => {
                    setCurrent(s.id)
                    setNote('')
                  }}
                >
                  {i + 1}
                  {s.result === 'done'
                    ? ' ✓'
                    : s.result === 'skipped'
                      ? ' −'
                      : ''}
                </button>
              ))}
            </nav>
            <div className="cw-live-actions">
              <button
                className="btn btn-primary"
                disabled={!writable}
                onClick={() => {
                  void act(() =>
                    mutate(
                      {
                        operation: 'update_shoot_item',
                        id: item.id,
                        expected_revision: item.revision,
                        ...(item.result
                          ? { clear_result: true }
                          : { result: 'done' }),
                      },
                      item.result ? '已撤销' : '已记为拍完',
                    ),
                  )
                }}
              >
                {item.result ? '撤销记录' : '✓ 拍完了'}
              </button>
              <button
                className="btn"
                disabled={!writable}
                onClick={() => {
                  void act(() =>
                    mutate(
                      {
                        operation: 'update_shoot_item',
                        id: item.id,
                        expected_revision: item.revision,
                        result: 'skipped',
                      },
                      '已跳过',
                    ),
                  )
                }}
              >
                这次跳过
              </button>
              <details>
                <summary>补记</summary>
                <form
                  noValidate
                  onSubmit={(e) => {
                    e.preventDefault()
                    void act(async () => {
                      await mutate(
                        {
                          operation: 'update_shoot_item',
                          id: item.id,
                          expected_revision: item.revision,
                          result_note: note,
                        },
                        '补记已保存',
                      )
                      setNote('')
                    })
                  }}
                >
                  <label>
                    现场笔记
                    <textarea
                      className="input resize-none"
                      rows={2}
                      style={{ resize: 'none' }}
                      maxLength={500}
                      value={note}
                      onChange={(e) => setNote(e.target.value)}
                    />
                  </label>
                  <button className="btn" disabled={!writable}>
                    保存补记
                  </button>
                </form>
              </details>
            </div>
          </section>
        ) : (
          <p>还没有要拍的画面。回到灵感墙，先挑选素材。</p>
        ))}
      {leaveTo && <ConfirmDialog title="还有素材没有保存" body="未搬入的素材和未保存的备忘会丢失。已保存内容会保留。" confirmLabel="离开空间" onCancel={() => setLeaveTo(null)} onConfirm={() => { const path = leaveTo; setLeaveTo(null); navigate(path) }} />}
      {remove && (
        <ConfirmDialog
          title={
            remove.operation === 'delete_memo'
              ? '移除这条备忘？'
              : '从这里移除？'
          }
          body="已形成的拍摄项与执行历史会保留。"
          confirmLabel="移除"
          danger
          busy={busy}
          onCancel={() => setRemove(null)}
          onConfirm={() => {
            void act(async () => {
              await mutate(remove, '已移除')
              setSelected((ids) => ids.filter((v) => v !== remove.id))
              setRemove(null)
            })
          }}
        />
      )}
    </main>
  )
}

function CardEditor({
  card,
  disabled,
  save,
}: {
  card: api.Card
  disabled: boolean
  save: (body: api.Command) => void
}) {
  const [text, setText] = useState(
    card.type === 'text' ? (card.text ?? '') : (card.caption ?? ''),
  )
  if (card.type === 'link') return null
  return (
    <form
      noValidate
      onSubmit={(e) => {
        e.preventDefault()
        save({
          operation: 'update_card',
          id: card.id,
          ...(card.type === 'text' ? { text } : { caption: text }),
        })
      }}
    >
      <label>
        {card.type === 'text' ? '灵感内容' : '图片说明'}
        <textarea
          className="input resize-none"
          rows={2}
          style={{ resize: 'none' }}
          value={text}
          maxLength={card.type === 'text' ? 2000 : 200}
          disabled={disabled}
          onChange={(e) => setText(e.target.value)}
        />
      </label>
      <button className="btn" disabled={disabled}>
        保存
      </button>
    </form>
  )
}
function ShotEditor({
  shot,
  disabled,
  save,
}: {
  shot: api.ShootItem
  disabled: boolean
  save: (body: api.Command) => void
}) {
  const [title, setTitle] = useState(shot.title)
  const [description, setDescription] = useState(shot.description)
  return (
    <form
      noValidate
      onSubmit={(e) => {
        e.preventDefault()
        save({
          operation: 'update_shoot_item',
          id: shot.id,
          expected_revision: shot.revision,
          title,
          description,
        })
      }}
    >
      <label>
        画面名称
        <input
          className="input"
          value={title}
          maxLength={200}
          disabled={disabled}
          onChange={(e) => setTitle(e.target.value)}
        />
      </label>
      <label>
        画面说明
        <textarea
          className="input resize-none"
          rows={2}
          style={{ resize: 'none' }}
          value={description}
          maxLength={2000}
          disabled={disabled}
          onChange={(e) => setDescription(e.target.value)}
        />
      </label>
      <button className="btn" disabled={disabled || !title.trim()}>
        保存拍摄项
      </button>
    </form>
  )
}
function LinkEditor({
  disabled,
  linked,
  save,
}: {
  disabled: boolean
  linked: boolean
  save: (value: NonNullable<api.Command['link']>) => void
}) {
  const [kind, setKind] = useState<'order' | 'customer'>('order')
  const [q, setQ] = useState('')
  const [options, setOptions] = useState<{ id: string; name: string }[]>([])
  const [value, setValue] = useState('')
  const [error, setError] = useState('')
  const [composing, setComposing] = useState(false)
  useEffect(() => {
    let active = true
    if (disabled || composing) return
    const timer = setTimeout(() => {
      const result =
        kind === 'order'
          ? allOrderChoices(q)
          : listCustomers({ q, pageSize: 100 }).then((r) =>
              r.items.map((c) => ({ id: c.id, name: c.display_name })),
            )
      void result
        .then((items) => {
          if (active) {
            setOptions(items)
            setError('')
          }
        })
        .catch((e: unknown) => {
          if (active) setError(message(e))
        })
    }, 300)
    return () => {
      active = false
      clearTimeout(timer)
    }
  }, [kind, q, disabled, composing])
  return (
    <form
      className="cw-link-editor"
      noValidate
      onSubmit={(e) => {
        e.preventDefault()
        if (value) save({ kind, id: value })
      }}
    >
      <p className="cw-muted">可选关联，仅方便双向查看，不改变订单或档期。</p>
      <label>
        关联类型
        <select
          className="input"
          value={kind}
          disabled={disabled}
          onChange={(e) => {
            setKind(e.target.value === 'customer' ? 'customer' : 'order')
            setValue('')
            setOptions([])
          }}
        >
          <option value="order">订单</option>
          <option value="customer">客户</option>
        </select>
      </label>
      <label>
        查找
        <input
          className="input"
          value={q}
          disabled={disabled}
          onCompositionStart={() => setComposing(true)}
          onCompositionEnd={() => setComposing(false)}
          onChange={(e) => {
            setQ(e.target.value)
            setValue('')
          }}
        />
      </label>
      {q && (
        <button className="btn" type="button" onClick={() => setQ('')}>
          清除查找
        </button>
      )}
      <label>
        选择关联
        <select
          className="input"
          value={value}
          disabled={disabled}
          onChange={(e) => setValue(e.target.value)}
        >
          <option value="">
            请选择{options.length >= 100 ? '（可输入名称缩小范围）' : ''}
          </option>
          {options.map((o) => (
            <option value={o.id} key={o.id}>
              {o.name}
            </option>
          ))}
        </select>
      </label>
      {error && <p role="alert">{error}</p>}
      <button className="btn" disabled={disabled || !value}>
        关联
      </button>
      {linked && (
        <button
          type="button"
          className="btn btn-ghost"
          disabled={disabled}
          onClick={() => save({ kind: '', id: '' })}
        >
          解除关联
        </button>
      )}
    </form>
  )
}

async function allOrderChoices(q: string) {
  const items: { id: string; name: string }[] = []
  let page = 1
  while (true) {
    const result = await listOrders({ page, pageSize: 100 })
    items.push(
      ...result.items
        .filter((o) =>
          o.title.toLocaleLowerCase().includes(q.toLocaleLowerCase()),
        )
        .map((o) => ({ id: o.id, name: o.title })),
    )
    if (page * 100 >= result.total || items.length >= 100)
      return items.slice(0, 100)
    page++
  }
}
