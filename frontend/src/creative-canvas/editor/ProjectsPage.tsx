import {
  Aperture,
  ArrowDown,
  ArrowLeft,
  ArrowUpRight,
  Archive,
  FolderOpen,
  Library,
  Plus,
} from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import { Link, useNavigate, useSearchParams } from 'react-router-dom'
import * as api from './api.ts'
import { errorMessage } from './queue.ts'
import { useJournal } from './useJournal.ts'
import './workspace.css'
import './projects.css'

export default function ProjectsPage() {
  const account = api.currentAccount()
  return account ? (
    <Projects key={account} account={account} />
  ) : (
    <main>请先登录</main>
  )
}

function Projects({ account }: { account: string }) {
  const { queue, error: storageError } = useJournal(account)
  useEffect(() => {
    const previous = document.title
    document.title = '创意空间 · 我的项目'
    return () => {
      document.title = previous
    }
  }, [])
  const navigate = useNavigate()
  const [params, setParams] = useSearchParams()
  const archived = params.get('view') === 'archived'
  const [page, setPage] = useState<api.ProjectPage>({
    items: [],
    next_cursor: '',
  })
  const [loading, setLoading] = useState(true)
  const [moreLoading, setMoreLoading] = useState(false)
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState('')
  const [localError, setLocalError] = useState('')
  const [tick, setTick] = useState(0)
  const lifetime = useRef(0)
  useEffect(() => {
    lifetime.current += 1
    return () => {
      lifetime.current += 1
    }
  }, [])
  const generation = useRef(0)
  const projectsSection = useRef<HTMLElement>(null)
  const job = queue?.value.job
  const blocked =
    !queue ||
    queue.busy ||
    !!job ||
    !!storageError ||
    !!localError ||
    submitting

  useEffect(() => {
    const controller = new AbortController()
    const current = ++generation.current
    setLoading(true)
    setMoreLoading(false)
    setPage({ items: [], next_cursor: '' })
    void api
      .read<api.ProjectPage>(
        `/projects?archived=${archived}`,
        controller.signal,
      )
      .then((result) => {
        if (!controller.signal.aborted && current === generation.current) {
          setPage(result)
          setError('')
        }
      })
      .catch((e: unknown) => {
        if (!controller.signal.aborted) setError(errorMessage(e))
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false)
      })
    return () => controller.abort()
  }, [archived, tick])

  useEffect(() => {
    const leave = (event: BeforeUnloadEvent) => {
      if (localError || queue?.localPending || queue?.busy) {
        event.preventDefault()
        event.returnValue = ''
      }
    }
    window.addEventListener('beforeunload', leave)
    return () => window.removeEventListener('beforeunload', leave)
  }, [queue, localError])

  function openCreatedProject(result: unknown) {
    if (
      result &&
      typeof result === 'object' &&
      'default_canvas_id' in result &&
      typeof result.default_canvas_id === 'string' &&
      result.default_canvas_id
    ) {
      void navigate(
        `/creative/canvases/${encodeURIComponent(result.default_canvas_id)}`,
      )
    } else {
      setParams({})
      setTick((value) => value + 1)
    }
  }
  async function create() {
    if (!queue || blocked || queue.value.job || queue.busy) return
    const currentLifetime = lifetime.current
    const requestPath = window.location.pathname
    const isCurrentPage = () =>
      currentLifetime === lifetime.current &&
      requestPath === window.location.pathname
    setSubmitting(true)
    try {
      const result = await queue.enqueue(
        '/projects',
        { name: '未命名项目' },
        'project-form',
      )
      if (!isCurrentPage()) return
      if (!queue.value.job) {
        openCreatedProject(result)
      }
    } catch (e) {
      if (isCurrentPage()) setLocalError(errorMessage(e))
    } finally {
      if (isCurrentPage()) setSubmitting(false)
    }
  }
  async function recover() {
    if (!queue || queue.busy) return
    const currentLifetime = lifetime.current
    const requestPath = window.location.pathname
    const isCurrentPage = () =>
      currentLifetime === lifetime.current &&
      requestPath === window.location.pathname
    setSubmitting(true)
    try {
      const creatingProject = queue.value.job?.path === '/projects'
      const result = await queue.flush()
      if (!isCurrentPage()) return
      if (!queue.value.job) {
        if (creatingProject) openCreatedProject(result)
        else setTick((value) => value + 1)
      }
    } catch (e) {
      if (isCurrentPage()) setLocalError(errorMessage(e))
    } finally {
      if (isCurrentPage()) setSubmitting(false)
    }
  }
  async function more() {
    if (loading || moreLoading || !page.next_cursor) return
    const current = generation.current
    setMoreLoading(true)
    try {
      const result = await api.read<api.ProjectPage>(
        `/projects?archived=${archived}&cursor=${encodeURIComponent(page.next_cursor)}`,
      )
      if (current === generation.current)
        setPage((previous) => ({
          ...result,
          items: [
            ...previous.items,
            ...result.items.filter(
              (p) => !previous.items.some((old) => old.id === p.id),
            ),
          ],
        }))
    } catch (e) {
      if (current === generation.current) setError(errorMessage(e))
    } finally {
      if (current === generation.current) setMoreLoading(false)
    }
  }

  return (
    <main className="cc-workspace ch-home">
      <header className="ch-header">
        <Link className="ch-brand" to="/creative">
          <Aperture size={27} strokeWidth={1.3} />
          <span>创意空间</span>
        </Link>
        <nav className="ch-navigation" aria-label="创意空间导航">
          <Link to="/creative" aria-current="page">
            项目
          </Link>
          <Link to="/creative/library">
            <Library size={15} />
            个人资产库
          </Link>
        </nav>
        <Link className="ch-crm" to="/dashboard" aria-label="返回 CRM">
          <ArrowLeft size={15} />
          <span>返回 CRM</span>
        </Link>
      </header>

      <section className="ch-hero" aria-labelledby="creativeHomeTitle">
        <div className="ch-orbit" aria-hidden="true">
          <span />
          <span />
          <span />
        </div>
        <p className="ch-eyebrow">YOUR SPACE TO CREATE</p>
        <h1 id="creativeHomeTitle">
          下一份作品，
          <br />
          <em>从一个想法开始。</em>
        </h1>
        <p className="ch-intro">给灵感一个项目，让文字、参考与创作有处安放。</p>
        <button
          className="ch-create"
          type="button"
          disabled={blocked}
          aria-busy={submitting}
          aria-label="创建项目"
          onClick={() => void create()}
        >
          <span className="ch-create-icon" aria-hidden="true">
            <Plus size={26} strokeWidth={1.3} />
          </span>
          <span className="ch-create-field">
            <strong>新建项目</strong>
            <span>打开空白画布，即刻开始创作</span>
          </span>
          <ArrowUpRight size={22} aria-hidden="true" />
        </button>
        <div className="ch-hero-links">
          <button
            type="button"
            onClick={() =>
              projectsSection.current?.scrollIntoView({ block: 'start' })
            }
          >
            <ArrowDown size={14} />
            继续已有项目
          </button>
          <span />
          <Link to="/creative/library">
            <Library size={14} />
            整理个人资产
          </Link>
        </div>
      </section>

      {(storageError || localError || error) && (
        <section className="ch-notice" role="alert">
          <p>{storageError || localError || error}</p>
          <button
            className="btn"
            onClick={() => {
              setTick((value) => value + 1)
              if (localError && queue)
                void queue
                  .update(queue.value)
                  .then(() => setLocalError(''))
                  .catch((e: unknown) => setLocalError(errorMessage(e)))
            }}
          >
            重试
          </button>
        </section>
      )}
      {job && !queue?.busy && !submitting && (
        <section className="ch-notice" aria-live="polite">
          <p>
            {job.state === 'rejected'
              ? job.message
              : '有一笔保存尚未确认，恢复后可继续创建项目。'}
          </p>
          {job.state === 'rejected' ? (
            <button
              className="btn"
              onClick={() => {
                void queue
                  ?.dismissRejected()
                  .catch((e: unknown) => setLocalError(errorMessage(e)))
              }}
            >
              关闭被拒绝的请求，保留草稿
            </button>
          ) : (
            <button className="btn" onClick={() => void recover()}>
              查询并恢复原保存
            </button>
          )}
        </section>
      )}

      <section
        className="ch-projects"
        ref={projectsSection}
        aria-labelledby="creativeProjectsTitle"
      >
        <div className="ch-section-heading">
          <div>
            <p className="ch-eyebrow">A WORK IN PROGRESS</p>
            <h2 id="creativeProjectsTitle">我的项目</h2>
          </div>
          <button
            className="btn"
            disabled={blocked}
            aria-busy={submitting}
            onClick={() => void create()}
          >
            <Plus size={16} />
            新建项目
          </button>
        </div>
        <div className="ch-list-toolbar">
          <div className="ch-tabs" role="group" aria-label="项目状态">
            <button aria-pressed={!archived} onClick={() => setParams({})}>
              进行中
            </button>
            <button
              aria-pressed={archived}
              onClick={() => setParams({ view: 'archived' })}
            >
              <Archive size={13} />
              已归档
            </button>
          </div>
          <span>按最近更新排列</span>
        </div>
        {loading ? (
          <div className="ch-list-state" role="status">
            正在加载项目…
          </div>
        ) : (
          <>
            {!error && !page.items.length && (
              <div className="ch-list-state">
                <FolderOpen size={28} strokeWidth={1.2} />
                <h3>
                  {archived ? '还没有归档项目' : '这里，留给你的第一份创作'}
                </h3>
                <p>
                  {archived
                    ? '归档的项目会留在这里，随时可以继续。'
                    : '新建一个项目，或先去个人资产库收集灵感。'}
                </p>
              </div>
            )}
            <div className="ch-project-grid">
              {!archived && (
                <button
                  className="ch-new-card"
                  disabled={blocked}
                  aria-busy={submitting}
                  onClick={() => void create()}
                >
                  <span>
                    <Plus size={25} strokeWidth={1.2} />
                  </span>
                  <strong>开启新的创作</strong>
                  <small>文字、链接与创作灵感</small>
                </button>
              )}
              {page.items.map((project, index) => (
                <Link
                  className="ch-project-card"
                  key={project.id}
                  to={`/creative/canvases/${encodeURIComponent(project.default_canvas_id)}`}
                  aria-label={`打开项目：${project.name}`}
                >
                  <div
                    className={`ch-project-cover ch-cover-${index % 4}`}
                    aria-hidden="true"
                  >
                    <div className="ch-cover-grid" />
                    <div className="ch-cover-sheet">
                      <Aperture size={31} strokeWidth={0.8} />
                      <span>{project.name.slice(0, 1)}</span>
                    </div>
                    <span className="ch-cover-caption">CREATIVE CANVAS</span>
                    <span className="ch-open-arrow">
                      <ArrowUpRight size={19} />
                    </span>
                  </div>
                  <div className="ch-project-details">
                    <h3>{project.name}</h3>
                    <p>
                      <span>{project.archived ? '已归档' : '私人项目'}</span>
                      <time dateTime={project.updated_at}>
                        {new Date(project.updated_at).toLocaleDateString(
                          'zh-CN',
                          { month: 'long', day: 'numeric' },
                        )}
                      </time>
                    </p>
                  </div>
                </Link>
              ))}
            </div>
            {page.next_cursor && (
              <div className="ch-more">
                <button
                  className="btn"
                  disabled={moreLoading}
                  onClick={() => void more()}
                >
                  {moreLoading ? '正在加载项目…' : '加载更多项目'}
                </button>
              </div>
            )}
          </>
        )}
      </section>
      <footer className="ch-footer">
        <Aperture size={17} strokeWidth={1.2} />
        <span>留住灵感，慢慢成形。</span>
        <Link to="/creative/library">
          个人资产库 <ArrowUpRight size={12} />
        </Link>
      </footer>
    </main>
  )
}
