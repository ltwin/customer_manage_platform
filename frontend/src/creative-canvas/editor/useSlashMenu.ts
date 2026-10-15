import { useState, type KeyboardEvent } from 'react'
import type { SlashItem } from './agentContext.ts'

/**
 * 斜杠菜单的高亮与按键，画布助手和首页输入框共用：上下换项，Enter / Tab 选中，
 * Esc 收起；草稿一变就回到第一项，收起的菜单也随之重新出现。
 */
export function useSlashMenu(
  draft: string,
  suggestions: SlashItem[],
  choose: (item: SlashItem) => void,
) {
  const [pick, setPick] = useState({ draft: '', index: 0 })
  const [dismissed, setDismissed] = useState<string | null>(null)
  const items = dismissed === draft ? [] : suggestions
  const active = !items.length
    ? -1
    : pick.draft === draft
      ? Math.min(pick.index, items.length - 1)
      : 0
  // 返回 true 表示按键已被菜单用掉，输入框不再处理。
  function onKey(event: KeyboardEvent): boolean {
    if (!items.length) return false
    const item = items[active]
    switch (event.key) {
      case 'ArrowDown':
      case 'ArrowUp': {
        event.preventDefault()
        const by = event.key === 'ArrowDown' ? 1 : items.length - 1
        setPick({ draft, index: (active + by) % items.length })
        return true
      }
      case 'Enter':
      case 'Tab':
        if (event.shiftKey || !item) return false
        // 已经写全的命令，回车照常发送。
        if (
          event.key === 'Enter' &&
          item.kind === 'command' &&
          draft === `/${item.name}`
        )
          return false
        event.preventDefault()
        choose(item)
        return true
      case 'Escape':
        event.preventDefault()
        setDismissed(draft)
        return true
    }
    return false
  }
  return {
    items,
    active,
    hover: (index: number) => setPick({ draft, index }),
    onKey,
  }
}
