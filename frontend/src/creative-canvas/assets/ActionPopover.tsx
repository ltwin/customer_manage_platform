import { createPortal } from 'react-dom'
import { useEffect, useLayoutEffect, useRef, type ReactNode } from 'react'
import { useFocusTrap } from '../../components/useFocusTrap'
import type { Point } from './popover.ts'
export default function ActionPopover({
  at,
  label,
  children,
  onClose,
}: {
  at: Point
  label: string
  children: ReactNode
  onClose: () => void
}) {
  const ref = useFocusTrap<HTMLDivElement>(true, onClose)
  const close = useRef(onClose)
  close.current = onClose
  useLayoutEffect(() => {
    const el = ref.current
    if (!el) return
    el.style.left = `${Math.max(8, Math.min(at.x, innerWidth - el.offsetWidth - 8))}px`
    el.style.top = `${Math.max(8, Math.min(at.y, innerHeight - el.offsetHeight - 8))}px`
  }, [at, ref])
  useEffect(() => {
    const outside = (e: PointerEvent) => {
      if (e.target instanceof Node && !ref.current?.contains(e.target))
        close.current()
    }
    const resized = () => close.current()
    document.addEventListener('pointerdown', outside, true)
    window.addEventListener('resize', resized)
    return () => {
      document.removeEventListener('pointerdown', outside, true)
      window.removeEventListener('resize', resized)
    }
  }, [ref])
  return createPortal(
    <div
      ref={ref}
      role="dialog"
      aria-label={label}
      tabIndex={-1}
      className="al-pop"
    >
      {children}
    </div>,
    document.querySelector('.cc-workspace') ?? document.body,
  )
}
