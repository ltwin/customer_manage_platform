import {
  Archive,
  ArchiveRestore,
  Ellipsis,
  ExternalLink,
  PencilLine,
  Plus,
  Search,
} from 'lucide-react'
import { useEffect, useRef, useState, type KeyboardEvent } from 'react'
import { Link, useNavigate, useSearchParams } from 'react-router-dom'
import { ApiError } from '../../api/transport.ts'
import * as api from '../editor/api.ts'
import { errorMessage } from '../editor/queue.ts'
import { useJournal } from '../editor/useJournal.ts'
import '../editor/workspace.css'
import { useSidebarRefresh } from './creativeShell.ts'
import ProjectCover from './ProjectCover.tsx'
import {
  canvasPath,
  editedAgo,
  parseSort,
  projectSorts,
  projectState,
  useProjectList,
} from './projectList.ts'
import { decideProjectName, projectNameMessages } from './projectName.ts'
import './projects.css'

type Toast = { text: string; action?: { label: string; run: () => void } }
type StateResult = { project_id: string; revision: string; archived?: boolean }
type Op = { path: string; body: string; operation: string }

const busyNote = '有任务在运行，结束后可归档'

// 每次点击生成一个操作身份；结果未知时重试复用同一个身份，服务端按它回放。
function newOp(path: string, payload: unknown): Op {
  const operation = crypto.randomUUID()
  return {
    path,
    operation,
    body: JSON.stringify({
      operation_id: operation,
      client_created_at: new Date().toISOString(),
      payload,
    }),
  }
}

// 4xx（408、429 除外）说明服务端明确拒绝了这次操作；其余情况结果未知。
function definitelyRejected(e: unknown): e is ApiError {
  return (
    e instanceof ApiError &&
    e.status >= 400 &&
    e.status < 500 &&
    e.status !== 408 &&
    e.status !== 429
  )
}

// 空值表示去掉这个参数，默认值不写进网址。
function withParams(
  current: URLSearchParams,
  changes: Record<string, string>,
): URLSearchParams {
  const next = new URLSearchParams(current)
  for (const [name, value] of Object.entries(changes)) {
    if (value) next.set(name, value)
    else next.delete(name)
  }
  return next
}

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
    document.title = '项目 · 创意空间'
    return () => {
      document.title = previous
    }
  }, [])
  const navigate = useNavigate()
  const [params, setParams] = useSearchParams()
  const archived = params.get('view') === 'archived'
  const q = (params.get('q') ?? '').trim().slice(0, 100)
  const sort = parseSort(params.get('sort'))
  const list = useProjectList(archived, q, sort)
  const page = list.page
  // 输入框的草稿跟着网址里的搜索词走：网址被别处改了（返回、清除）就显示网址里的。
  const [draft, setDraft] = useState({ q, text: q })
  const text = draft.q === q ? draft.text : q
  const [submitting, setSubmitting] = useState(false)
  const [localError, setLocalError] = useState('')
  const [toast, setToast] = useState<Toast | null>(null)
  const [menu, setMenu] = useState<string | null>(null)
  const [renaming, setRenaming] = useState<string | null>(null)
  const [changes, setChanges] = useState(0)
  useSidebarRefresh(changes)
  const lifetime = useRef(0)
  useEffect(() => {
    lifetime.current += 1
    return () => {
      lifetime.current += 1
    }
  }, [])
  const job = queue?.value.job
  const blocked =
    !queue ||
    queue.busy ||
    !!job ||
    !!storageError ||
    !!localError ||
    submitting

  function update(changes: Record<string, string>) {
    setParams((current) => withParams(current, changes), { replace: true })
  }

  // 搜索词防抖 300ms 再写进网址，网址变化才会请求。
  useEffect(() => {
    const next = text.trim()
    if (next === q) return
    const timer = setTimeout(() => {
      setDraft({ q: next, text })
      setParams((current) => withParams(current, { q: next }), {
        replace: true,
      })
    }, 300)
    return () => clearTimeout(timer)
  }, [text, q, setParams])

  useEffect(() => {
    if (!toast) return
    const timer = setTimeout(() => setToast(null), toast.action ? 6000 : 2400)
    return () => clearTimeout(timer)
  }, [toast])

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
      list.reload()
    }
  }
  // 新建走 journal：结果不确定时按操作身份查回执，不会建出两个项目。
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
      if (!queue.value.job) openCreatedProject(result)
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
        else list.reload()
      }
    } catch (e) {
      if (isCurrentPage()) setLocalError(errorMessage(e))
    } finally {
      if (isCurrentPage()) setSubmitting(false)
    }
  }

  // 改名、归档、恢复直接发请求，不占 journal，不影响新建。
  async function perform(
    op: Op,
    label: string,
    done: (result: StateResult) => void,
  ) {
    try {
      done(await api.send<StateResult>(account, op.path, op.body, op.operation))
      setChanges((value) => value + 1)
    } catch (e) {
      if (definitelyRejected(e)) {
        setToast({
          text:
            e.code === 'creative_revision_conflict'
              ? projectNameMessages.conflict
              : e.code === 'creative_project_busy'
                ? busyNote
                : e.message,
        })
      } else {
        setToast({
          text: `${label}结果未确认`,
          action: { label: '重试', run: () => void perform(op, label, done) },
        })
      }
      list.reload()
    }
  }

  function rename(project: api.Project, input: string) {
    setRenaming(null)
    const decision = decideProjectName(input, project.name)
    if (decision.kind === 'keep') return
    if (decision.kind === 'too-long') {
      setToast({ text: projectNameMessages.tooLong })
      return
    }
    const { name } = decision
    list.patch((items) =>
      items.map((p) => (p.id === project.id ? { ...p, name } : p)),
    )
    void perform(
      newOp(`/projects/${encodeURIComponent(project.id)}/rename`, {
        expected_revision: project.revision,
        name,
      }),
      '改名',
      list.reload,
    )
  }

  // 归档或恢复成功后卡片移出当前标签；撤销就是用新的修订号发相反的操作。
  function setArchived(
    project: api.Project,
    archive: boolean,
    revision: string,
    undoing = false,
  ) {
    const action = archive ? 'archive' : 'restore'
    void perform(
      newOp(`/projects/${encodeURIComponent(project.id)}/${action}`, {
        expected_revision: revision,
      }),
      archive ? '归档' : '恢复',
      (result) => {
        list.patch((items) => items.filter((p) => p.id !== project.id))
        list.reload()
        setToast(
          undoing
            ? { text: `已撤销${archive ? '恢复' : '归档'}「${project.name}」` }
            : {
                text: `已${archive ? '归档' : '恢复'}「${project.name}」`,
                action: {
                  label: '撤销',
                  run: () =>
                    setArchived(project, !archive, result.revision, true),
                },
              },
        )
      },
    )
  }

  const empty = page && !page.items.length
  return (
    <>
      <main className="cs-main">
        <div className="pp-view">
          <header className="pp-head">
            <h1>项目</h1>
            <div className="pp-seg" role="group" aria-label="项目状态">
              <button
                type="button"
                aria-pressed={!archived}
                onClick={() => update({ view: '' })}
              >
                当前
              </button>
              <button
                type="button"
                aria-pressed={archived}
                onClick={() => update({ view: 'archived' })}
              >
                已归档
              </button>
            </div>
            <span className="pp-grow" />
            <label className="pp-search">
              <Search size={14} />
              <input
                type="search"
                aria-label="搜索项目"
                placeholder="搜索项目"
                maxLength={100}
                value={text}
                onChange={(event) =>
                  setDraft({ q, text: event.currentTarget.value })
                }
              />
            </label>
            <label className="pp-sort">
              <select
                aria-label="排序"
                value={sort}
                onChange={(event) =>
                  update({
                    sort:
                      event.currentTarget.value === 'edited_desc'
                        ? ''
                        : event.currentTarget.value,
                  })
                }
              >
                {projectSorts.map((s) => (
                  <option key={s.value} value={s.value}>
                    {s.label}
                  </option>
                ))}
              </select>
            </label>
            <button
              className="btn btn-primary"
              type="button"
              disabled={blocked}
              aria-busy={submitting}
              onClick={() => void create()}
            >
              <Plus size={16} />
              新建项目
            </button>
          </header>

          {(storageError || localError) && (
            <section className="pp-notice" role="alert">
              <p>{storageError || localError}</p>
              {localError && queue && (
                <button
                  className="btn"
                  type="button"
                  onClick={() =>
                    void queue
                      .update(queue.value)
                      .then(() => setLocalError(''))
                      .catch((e: unknown) => setLocalError(errorMessage(e)))
                  }
                >
                  重试
                </button>
              )}
            </section>
          )}
          {job && !queue?.busy && !submitting && (
            <section className="pp-notice" aria-live="polite">
              <p>
                {job.state === 'rejected'
                  ? job.message
                  : '有一笔保存尚未确认，恢复后可继续创建项目。'}
              </p>
              {job.state === 'rejected' ? (
                <button
                  className="btn"
                  type="button"
                  onClick={() => {
                    void queue
                      ?.dismissRejected()
                      .catch((e: unknown) => setLocalError(errorMessage(e)))
                  }}
                >
                  关闭被拒绝的请求，保留草稿
                </button>
              ) : (
                <button
                  className="btn"
                  type="button"
                  onClick={() => void recover()}
                >
                  查询并恢复原保存
                </button>
              )}
            </section>
          )}
          {list.error && (
            <section className="pp-notice" role="alert">
              <p>{list.error}</p>
              <button className="btn" type="button" onClick={list.reload}>
                重试
              </button>
            </section>
          )}

          {!page ? (
            !list.error && (
              <p className="pp-empty" role="status">
                正在加载项目…
              </p>
            )
          ) : empty ? (
            <div className="pp-empty">
              {q ? (
                <>
                  <p>没有名称包含「{q}」的项目</p>
                  <button
                    className="btn"
                    type="button"
                    onClick={() => {
                      setDraft({ q: '', text: '' })
                      update({ q: '' })
                    }}
                  >
                    清除搜索
                  </button>
                </>
              ) : archived ? (
                <p>还没有归档的项目。归档后项目会留在这里，随时可以恢复。</p>
              ) : (
                <>
                  <p>还没有项目</p>
                  <button
                    className="btn btn-primary"
                    type="button"
                    disabled={blocked}
                    onClick={() => void create()}
                  >
                    <Plus size={16} />
                    新建项目
                  </button>
                </>
              )}
            </div>
          ) : (
            <div className="pp-grid">
              {page.items.map((project) => (
                <ProjectCard
                  key={project.id}
                  project={project}
                  menuOpen={menu === project.id}
                  renaming={renaming === project.id}
                  onMenu={(open) => setMenu(open ? project.id : null)}
                  onRename={() => {
                    setMenu(null)
                    setRenaming(project.id)
                  }}
                  onRenamed={(value) => rename(project, value)}
                  onArchive={(archive) => {
                    setMenu(null)
                    setArchived(project, archive, project.revision)
                  }}
                />
              ))}
            </div>
          )}
          {page?.next_cursor && (
            <div className="pp-more">
              <button
                className="btn"
                type="button"
                disabled={list.moreLoading}
                onClick={() => void list.more()}
              >
                {list.moreLoading ? '正在加载项目…' : '加载更多项目'}
              </button>
            </div>
          )}
        </div>
      </main>
      {toast && (
        <div className="pp-toast" role="status">
          <span>{toast.text}</span>
          {toast.action && (
            <button
              type="button"
              onClick={() => {
                const run = toast.action?.run
                setToast(null)
                run?.()
              }}
            >
              {toast.action.label}
            </button>
          )}
        </div>
      )}
    </>
  )
}

function ProjectCard({
  project,
  menuOpen,
  renaming,
  onMenu,
  onRename,
  onRenamed,
  onArchive,
}: {
  project: api.Project
  menuOpen: boolean
  renaming: boolean
  onMenu: (open: boolean) => void
  onRename: () => void
  onRenamed: (value: string) => void
  onArchive: (archive: boolean) => void
}) {
  const trigger = useRef<HTMLButtonElement>(null)
  const menuRef = useRef<HTMLDivElement>(null)
  // Enter 和失焦都会提交，只认第一次。
  const settled = useRef(false)

  const latestOnMenu = useRef(onMenu)
  useEffect(() => {
    latestOnMenu.current = onMenu
  })
  // 打开时把焦点放到第一项；点菜单外关闭。
  useEffect(() => {
    if (!menuOpen) return
    menuRef.current?.querySelector<HTMLElement>('[role="menuitem"]')?.focus()
    const close = (event: PointerEvent) => {
      const target = event.target as Node
      if (
        !menuRef.current?.contains(target) &&
        !trigger.current?.contains(target)
      )
        latestOnMenu.current(false)
    }
    document.addEventListener('pointerdown', close)
    return () => document.removeEventListener('pointerdown', close)
  }, [menuOpen])

  function menuKey(event: KeyboardEvent) {
    const items = Array.from(
      menuRef.current?.querySelectorAll<HTMLElement>(
        '[role="menuitem"]:not(:disabled)',
      ) ?? [],
    )
    const index = items.indexOf(document.activeElement as HTMLElement)
    if (event.key === 'Escape') {
      event.preventDefault()
      onMenu(false)
      trigger.current?.focus()
    } else if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
      event.preventDefault()
      const step = event.key === 'ArrowDown' ? 1 : -1
      items[(index + step + items.length) % items.length]?.focus()
    }
  }

  return (
    <article className="pp-card">
      <Link
        className="pp-open"
        to={canvasPath(project)}
        aria-label={`打开项目：${project.name}`}
      >
        <ProjectCover project={project} state={projectState(project)} />
      </Link>
      <div className="pp-meta">
        <span className="pp-title">
          {renaming ? (
            <input
              className="pp-rename"
              aria-label="项目名称"
              defaultValue={project.name}
              autoFocus
              onFocus={(event) => {
                settled.current = false
                event.currentTarget.select()
              }}
              onKeyDown={(event) => {
                if (event.nativeEvent.isComposing) return
                if (event.key === 'Enter' || event.key === 'Escape') {
                  event.preventDefault()
                  settled.current = true
                  onRenamed(
                    event.key === 'Enter'
                      ? event.currentTarget.value
                      : project.name,
                  )
                }
              }}
              onBlur={(event) => {
                if (!settled.current) onRenamed(event.currentTarget.value)
              }}
            />
          ) : (
            <strong title={project.name}>{project.name}</strong>
          )}
          <small>
            {project.archived ? '已归档 · ' : ''}
            {editedAgo(project.edited_at)}
          </small>
        </span>
        <button
          ref={trigger}
          className="pp-menu-btn"
          type="button"
          aria-label={`项目操作：${project.name}`}
          aria-haspopup="menu"
          aria-expanded={menuOpen}
          onClick={() => onMenu(!menuOpen)}
        >
          <Ellipsis size={16} />
        </button>
        {menuOpen && (
          <div
            ref={menuRef}
            className="cc-menu pp-menu"
            role="menu"
            aria-label={`项目操作：${project.name}`}
            onKeyDown={menuKey}
          >
            <Link role="menuitem" to={canvasPath(project)}>
              <ExternalLink size={16} />
              <span>打开</span>
            </Link>
            {project.archived ? (
              <button
                role="menuitem"
                type="button"
                onClick={() => onArchive(false)}
              >
                <ArchiveRestore size={16} />
                <span>恢复</span>
              </button>
            ) : (
              <>
                <button role="menuitem" type="button" onClick={onRename}>
                  <PencilLine size={16} />
                  <span>重命名</span>
                </button>
                <span className="cc-menu-sep" />
                {/* 归档只看 running：显示「等你确认」但也在跑的项目同样不能归档 */}
                <button
                  role="menuitem"
                  type="button"
                  className="pp-menu-two"
                  disabled={project.running}
                  onClick={() => onArchive(true)}
                >
                  <Archive size={16} />
                  <span>
                    归档
                    {project.running && <small>{busyNote}</small>}
                  </span>
                </button>
              </>
            )}
          </div>
        )}
      </div>
    </article>
  )
}
