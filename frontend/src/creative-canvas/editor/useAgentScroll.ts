import { useEffect, useRef } from 'react'

export function useAgentScroll(conversation: string) {
  const history = useRef<HTMLDivElement>(null)
  const content = useRef<HTMLDivElement>(null)
  const pinned = useRef(true)
  const programmaticTop = useRef<number | null>(null)
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
  }
}
