import { createContext, useContext, useEffect } from 'react'
import type * as api from '../editor/api.ts'

// 创意空间外壳（侧栏 + 深色布局）由 CreativeLayout 常驻渲染，页面只渲染内容区。
// 页面原先直接传给侧栏的东西改走这里。
export type CreativeShell = {
  // 首页与「继续创作」共用同一次读取，交给侧栏的「最近打开」。
  setHomeRecent: (items: api.Project[]) => void
  // 页面改动了项目（改名、归档、恢复）后调用，侧栏随之重读。
  refreshSidebar: () => void
  // 资产库导入或批量操作进行中时拦住所有链接，包括侧栏。
  setNavigationBlocked: (blocked: boolean) => void
}

export const CreativeShellContext = createContext<CreativeShell | null>(null)

export function useCreativeShell(): CreativeShell {
  const shell = useContext(CreativeShellContext)
  if (!shell) throw new Error('useCreativeShell 必须在 CreativeLayout 内使用')
  return shell
}

// 页面自己的改动计数变化时（跳过初始的 0）让侧栏重读。
export function useSidebarRefresh(changes: number) {
  const { refreshSidebar } = useCreativeShell()
  useEffect(() => {
    if (changes) refreshSidebar()
  }, [changes, refreshSidebar])
}
