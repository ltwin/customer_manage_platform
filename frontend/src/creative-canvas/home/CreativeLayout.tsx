import { Suspense, useEffect, useMemo, useState } from 'react'
import { Outlet, useLocation } from 'react-router-dom'
import type * as api from '../editor/api.ts'
import '../editor/workspace.css'
import { preloadCreativePages, preloadLinkTarget } from '../pages.ts'
import AppSidebar, { type SidebarView } from './AppSidebar.tsx'
import { CreativeShellContext, type CreativeShell } from './creativeShell.ts'

// 创意空间首页、项目、资产库、外部助手共用的外壳：随主包加载，切换页面时侧栏
// 不卸载；只有内容区按需加载，加载中显示骨架。页面返回内容区（main 与浮层），
// DOM 结构与原先各页面自带外壳时相同。

const loadingLabels: Record<SidebarView, string> = {
  home: '正在打开创意空间…',
  projects: '正在打开项目…',
  library: '正在打开资产库…',
  agents: '正在打开外部助手…',
}

function viewOf(pathname: string): SidebarView {
  if (pathname.startsWith('/creative/projects')) return 'projects'
  if (pathname.startsWith('/creative/library')) return 'library'
  if (pathname.startsWith('/creative/agents')) return 'agents'
  return 'home'
}

let preloaded = false

// 进创意空间后空闲时预取其它页面与创作台；省流量模式下不预取。只在生产构建里做：
// 开发模式按模块逐个请求，一次预取几百个请求，会把页面自己的接口挤在后面。
function usePreloadPages() {
  useEffect(() => {
    const connection = (
      navigator as Navigator & { connection?: { saveData?: boolean } }
    ).connection
    if (!import.meta.env.PROD || preloaded || connection?.saveData) return
    const run = () => {
      preloaded = true
      preloadCreativePages()
    }
    if (typeof requestIdleCallback === 'function') {
      const id = requestIdleCallback(run, { timeout: 3000 })
      return () => cancelIdleCallback(id)
    }
    const id = window.setTimeout(run, 1500)
    return () => window.clearTimeout(id)
  }, [])
}

export default function CreativeLayout() {
  const view = viewOf(useLocation().pathname)
  const [homeRecent, setHomeRecent] = useState<api.Project[]>([])
  const [refresh, setRefresh] = useState(0)
  const [blocked, setNavigationBlocked] = useState(false)
  const shell = useMemo<CreativeShell>(
    () => ({
      setHomeRecent,
      refreshSidebar: () => setRefresh((value) => value + 1),
      setNavigationBlocked,
    }),
    [],
  )
  usePreloadPages()

  return (
    <CreativeShellContext.Provider value={shell}>
      <div
        className="cc-workspace cs-app"
        onPointerOver={(e) => preloadLinkTarget(e.target)}
        onFocus={(e) => preloadLinkTarget(e.target)}
        onClickCapture={(e) => {
          if (
            blocked &&
            e.target instanceof Element &&
            e.target.closest('a[href]')
          )
            e.preventDefault()
        }}
      >
        <AppSidebar
          current={view}
          recent={view === 'home' ? homeRecent : undefined}
          refresh={refresh}
        />
        {/* key 按页面区分：每个页面是新的边界，导航时立刻换成骨架，不留旧页面 */}
        <Suspense key={view} fallback={<PageLoading label={loadingLabels[view]} />}>
          <Outlet />
        </Suspense>
      </div>
    </CreativeShellContext.Provider>
  )
}

function PageLoading({ label }: { label: string }) {
  return (
    <main className="cs-main cs-loading" role="status">
      <span className="sr-only">{label}</span>
      <div className="cs-loading-head" />
      <div className="cs-loading-grid">
        {Array.from({ length: 8 }, (_, index) => (
          <div key={index} />
        ))}
      </div>
    </main>
  )
}

// 创作台全屏、没有侧栏；加载中与它自己读画布时的空态一致，chunk 到了无缝接上。
export function CanvasLoading() {
  return (
    <div className="cc-workspace">
      <div className="cc-canvas-empty" role="status">
        <h2>正在打开画布…</h2>
      </div>
    </div>
  )
}
