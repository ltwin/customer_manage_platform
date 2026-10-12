import {
  Aperture,
  ArrowLeft,
  Folder,
  Library,
  PanelLeftClose,
  PanelLeftOpen,
  Sparkles,
} from 'lucide-react'
import { useEffect, useState, useSyncExternalStore } from 'react'
import { Link } from 'react-router-dom'
import { useOptionalAccountCenter } from '../../account/AccountCenterContext.tsx'
import { fallbackAvatarLabel } from '../../account/accountAvatarMedia.ts'
import type * as api from '../editor/api.ts'
import {
  canvasPath,
  coverMedia,
  firstImage,
  projectState,
  readRecentProjects,
  recentLimit,
  type ProjectState,
} from './projectList.ts'
import './sidebar.css'

// 创意空间的应用侧栏：首页、项目页、资产库共用。展开显示全称，折叠只剩图标，
// 图标位置不变。结构从上到下：品牌 / 导航 / 最近打开 / 返回 CRM 与账号。

export type SidebarView = 'home' | 'projects' | 'library'

const storageKey = 'creative-side-collapsed'
const navItems = [
  { view: 'home', label: '首页', to: '/creative', icon: Sparkles },
  { view: 'projects', label: '项目', to: '/creative/projects', icon: Folder },
  { view: 'library', label: '资产库', to: '/creative/library', icon: Library },
] as const

function readCollapsed(): boolean {
  try {
    return localStorage.getItem(storageKey) === '1'
  } catch {
    return false
  }
}
function writeCollapsed(value: boolean) {
  try {
    localStorage.setItem(storageKey, value ? '1' : '0')
  } catch {
    // 无痕窗口等情况下存不了，只影响下次打开时的默认状态
  }
}

function useMedia(query: string): boolean {
  return useSyncExternalStore(
    (notify) => {
      const media = matchMedia(query)
      media.addEventListener('change', notify)
      return () => media.removeEventListener('change', notify)
    },
    () => matchMedia(query).matches,
  )
}

// 不在首页时侧栏自己读最近项目；切回标签页、refresh 变化时重读，读失败就保留上一份。
function useOwnRecent(enabled: boolean, refresh: number): api.Project[] {
  const [items, setItems] = useState<api.Project[]>([])
  useEffect(() => {
    if (!enabled) return
    let controller = new AbortController()
    const load = () => {
      controller.abort()
      controller = new AbortController()
      readRecentProjects(controller.signal)
        .then((page) => setItems(page.items))
        .catch(() => {})
    }
    const visible = () => {
      if (document.visibilityState === 'visible') load()
    }
    load()
    document.addEventListener('visibilitychange', visible)
    return () => {
      controller.abort()
      document.removeEventListener('visibilitychange', visible)
    }
  }, [enabled, refresh])
  return items
}

type Tip = { text: string; left: number; top: number }

export default function AppSidebar({
  current,
  recent,
  refresh = 0,
}: {
  current: SidebarView
  // 页面改动了项目（改名、归档、恢复）后加一，侧栏随之重读。
  refresh?: number
  // 首页与「继续创作」共用同一次请求，传进来；不传时侧栏自己读。
  recent?: api.Project[]
}) {
  const narrow = useMedia('(max-width: 1100px) and (min-width: 761px)')
  const phone = useMedia('(max-width: 760px)')
  const [preference, setPreference] = useState(readCollapsed)
  // 中等宽度自动折叠，手机上改成顶栏；这两种情况都不改动记下的偏好。
  const collapsed = !phone && (preference || narrow)
  const forced = narrow || phone
  const [tip, setTip] = useState<Tip | null>(null)
  const own = useOwnRecent(recent === undefined, refresh)
  const projects = (recent ?? own).slice(0, recentLimit)
  const center = useOptionalAccountCenter()
  const profile = center?.profile.kind === 'ready' ? center.profile.data : null
  const email = center?.account.email ?? ''
  const name = profile?.display_name?.trim() || email

  // 折叠时名字藏起来了，悬停或聚焦时显示在图标右侧。提示层是 fixed，不会被侧栏裁掉。
  function showTip(target: EventTarget) {
    if (!collapsed || !(target instanceof Element)) return
    const anchor = target.closest<HTMLElement>('[data-tip]')
    if (!anchor?.dataset.tip) return
    const rect = anchor.getBoundingClientRect()
    const side = anchor.closest('.cs-side')?.getBoundingClientRect()
    setTip({
      text: anchor.dataset.tip,
      left: (side?.right ?? rect.right) + 8,
      top: rect.top + rect.height / 2,
    })
  }
  function hideTip(target: EventTarget, related: EventTarget | null) {
    const anchor =
      target instanceof Element ? target.closest('[data-tip]') : null
    if (anchor && related instanceof Node && anchor.contains(related)) return
    setTip(null)
  }

  return (
    <>
      <aside
        className={`cs-side${collapsed ? ' collapsed' : ''}${forced ? ' forced' : ''}`}
        aria-label="创意空间导航"
        onPointerOver={(event) => showTip(event.target)}
        onPointerOut={(event) => hideTip(event.target, event.relatedTarget)}
        onFocus={(event) => showTip(event.target)}
        onBlur={(event) => hideTip(event.target, event.relatedTarget)}
      >
        <div className="cs-side-top">
          <Link className="cs-side-brand" to="/creative" data-tip="创意空间">
            <Aperture size={20} strokeWidth={1.5} />
            <span className="cs-side-text">创意空间</span>
          </Link>
          <button
            className="cs-side-toggle"
            type="button"
            aria-label={collapsed ? '展开侧栏' : '收起侧栏'}
            aria-expanded={!collapsed}
            data-tip="展开侧栏"
            onClick={() => {
              writeCollapsed(!preference)
              setPreference(!preference)
              setTip(null)
            }}
          >
            {collapsed ? (
              <PanelLeftOpen size={16} />
            ) : (
              <PanelLeftClose size={16} />
            )}
          </button>
        </div>
        <nav className="cs-side-nav">
          {navItems.map(({ view, label, to, icon: Icon }) => (
            <Link
              key={view}
              to={to}
              data-tip={label}
              aria-current={view === current ? 'page' : undefined}
            >
              <Icon size={16} />
              <span className="cs-side-text">{label}</span>
            </Link>
          ))}
        </nav>
        {/* 最近打开排在导航后面：导航加条目时它整体下移，放不下就在自己的区域里滚动 */}
        <section className="cs-side-recent">
          <div className="cs-side-label">
            <span className="cs-side-text">最近打开</span>
          </div>
          <nav aria-label="最近打开的项目" onScroll={() => setTip(null)}>
            {projects.map((project) => (
              <Link
                key={project.id}
                to={canvasPath(project)}
                data-tip={project.name}
              >
                <RecentThumb
                  revision={firstImage(project)}
                  state={projectState(project)}
                />
                <span className="cs-side-text">{project.name}</span>
              </Link>
            ))}
          </nav>
        </section>
        <div className="cs-side-foot">
          <Link className="cs-side-link" to="/dashboard" data-tip="返回 CRM">
            <ArrowLeft size={16} />
            <span className="cs-side-text">返回 CRM</span>
          </Link>
          {center && (
            <div className="cs-side-account" data-tip={name}>
              <span className="cs-side-avatar" aria-hidden="true">
                {fallbackAvatarLabel(profile?.display_name, email)}
              </span>
              <span className="cs-side-text">{name}</span>
            </div>
          )}
        </div>
      </aside>
      {collapsed && tip && (
        <div
          className="cs-side-tip"
          role="tooltip"
          style={{ left: tip.left, top: tip.top }}
        >
          {tip.text}
        </div>
      )}
    </>
  )
}

function RecentThumb({
  revision,
  state,
}: {
  revision: string | undefined
  state: ProjectState
}) {
  const [image, setImage] = useState<{ revision: string; url: string }>()
  useEffect(() => {
    if (!revision) return
    let active = true
    coverMedia(revision)
      .then((ticket) => {
        if (active) setImage({ revision, url: ticket.url })
      })
      .catch(() => {})
    return () => {
      active = false
    }
  }, [revision])
  return (
    <span className="cs-side-thumb">
      {image && image.revision === revision && (
        <img src={image.url} alt="" draggable={false} />
      )}
      {state && (
        <i
          className={`cs-side-dot ${state}`}
          role="img"
          aria-label={state === 'wait' ? '等你确认' : '运行中'}
        />
      )}
    </span>
  )
}
