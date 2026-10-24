import { lazy, type ComponentType } from 'react'

// 创意空间各页面按需加载。preload 与 lazy 共用同一次 import：进创意空间后
// 空闲时先把其它页面下载好，第一次点过去就不必再等 chunk。
function lazyPage(load: () => Promise<{ default: ComponentType }>) {
  let pending: Promise<{ default: ComponentType }> | undefined
  const preload = () =>
    (pending ??= load().catch((error: unknown) => {
      // 预取失败（断网等）不能把失败缓存下来，真正打开时再试一次。
      pending = undefined
      throw error
    }))
  return { Page: lazy(preload), preload }
}

const home = lazyPage(() => import('./home/HomePage.tsx'))
const projects = lazyPage(() => import('./home/ProjectsPage.tsx'))
const library = lazyPage(() => import('./assets/AssetsPage.tsx'))
const agents = lazyPage(() => import('./agents/AgentsPage.tsx'))
const canvas = lazyPage(() => import('./editor/WorkspacePage.tsx'))

export const CreativeHomePage = home.Page
export const CreativeProjectsPage = projects.Page
export const CreativeAssetsPage = library.Page
export const CreativeAgentsPage = agents.Page
export const CreativeCanvasPage = canvas.Page

export function preloadCreativePages() {
  for (const page of [home, projects, library, agents, canvas])
    page.preload().catch(() => {})
}

function pageOf(pathname: string) {
  if (pathname === '/creative') return home
  if (pathname === '/creative/projects') return projects
  if (pathname === '/creative/library') return library
  if (pathname === '/creative/agents') return agents
  if (pathname.startsWith('/creative/canvases/')) return canvas
  return undefined
}

// 指针移到（或焦点落到）指向创意空间页面的链接上时预取那一页，其它链接不管。
export function preloadLinkTarget(target: EventTarget) {
  if (!(target instanceof Element)) return
  const link = target.closest('a[href]')
  if (!(link instanceof HTMLAnchorElement) || link.origin !== location.origin)
    return
  pageOf(link.pathname)
    ?.preload()
    .catch(() => {})
}
