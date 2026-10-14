import { useEffect, useLayoutEffect, useRef } from 'react'

// 离顶部不到这么远就开始读更早的对话，滚到顶时多半已经接上了。
const olderMargin = 400

// head 是最早一条消息的标识：它变了说明前面接上了更早的内容。
export function useAgentScroll(conversation: string, head: string) {
  const history = useRef<HTMLDivElement>(null)
  const content = useRef<HTMLDivElement>(null)
  const pinned = useRef(true)
  const programmaticTop = useRef<number | null>(null)
  const held = useRef<{ height: number; top: number } | null>(null)
  // 前面接上内容后，正在看的那段留在原处。浏览器自己做了滚动锚定就不再挪。
  useLayoutEffect(() => {
    const log = history.current
    const hold = held.current
    held.current = null
    if (!log || !hold || pinned.current || log.scrollTop !== hold.top) return
    log.scrollTop = hold.top + log.scrollHeight - hold.height
    programmaticTop.current = log.scrollTop
  }, [head])
  useEffect(() => {
    const log = history.current
    const body = content.current
    if (!log || !body) return
    pinned.current = true
    programmaticTop.current = null
    let frame = 0
    const stick = () => {
      cancelAnimationFrame(frame)
      frame = requestAnimationFrame(() => {
        if (pinned.current) {
          log.scrollTop = log.scrollHeight
          programmaticTop.current = log.scrollTop
        }
      })
    }
    const observer = new ResizeObserver(stick)
    observer.observe(body)
    observer.observe(log)
    stick()
    return () => {
      observer.disconnect()
      cancelAnimationFrame(frame)
    }
  }, [conversation])

  function onScroll() {
    const log = history.current
    if (!log) return
    // Our previous frame's scroll event may arrive after another token grows
    // the content. Don't mistake that event for a manual scroll away.
    if (
      programmaticTop.current !== null &&
      Math.abs(log.scrollTop - programmaticTop.current) < 1
    )
      return
    pinned.current = log.scrollHeight - log.scrollTop - log.clientHeight < 48
  }
  return {
    history,
    content,
    onScroll,
    follow: () => {
      pinned.current = true
    },
    // 在前面接上内容之前调用，记下当前位置。
    hold: () => {
      const log = history.current
      if (log) held.current = { height: log.scrollHeight, top: log.scrollTop }
    },
    // 贴着底部时以内容高度为准：刚打开、还没滚到底的那一刻不算在顶部。
    nearTop: () => {
      const log = history.current
      if (!log) return false
      const top = pinned.current
        ? log.scrollHeight - log.clientHeight
        : log.scrollTop
      return top < olderMargin
    },
  }
}
