import { useEffect, useLayoutEffect, useRef, type ReactNode } from 'react'
import { createPortal } from 'react-dom'

export type CanvasMenuItem = {
  label: string
  icon: ReactNode
  action: () => void
  disabled?: boolean
  danger?: boolean
  shortcut?: string
}

export default function CanvasContextMenu({
  x,
  y,
  label,
  items,
  onClose,
}: {
  x: number
  y: number
  label: string
  items: CanvasMenuItem[]
  onClose: () => void
}) {
  const ref = useRef<HTMLDivElement>(null)
  const previous = useRef(document.activeElement)
  const closeRef = useRef(onClose)
  closeRef.current = onClose
  useLayoutEffect(() => {
    const menu = ref.current
    if (!menu) return
    menu.style.left = `${Math.max(8, Math.min(x, window.innerWidth - menu.offsetWidth - 8))}px`
    menu.style.top = `${Math.max(8, Math.min(y, window.innerHeight - menu.offsetHeight - 8))}px`
    const first = menu.querySelector<HTMLButtonElement>('button:not(:disabled)')
    ;(first ?? menu).focus()
  }, [x, y])
  useEffect(() => {
    const outside = (event: PointerEvent) => {
      if (event.target instanceof Node && !ref.current?.contains(event.target))
        closeRef.current()
    }
    const close = () => closeRef.current()
    document.addEventListener('pointerdown', outside, true)
    window.addEventListener('resize', close)
    window.addEventListener('blur', close)
    return () => {
      document.removeEventListener('pointerdown', outside, true)
      window.removeEventListener('resize', close)
      window.removeEventListener('blur', close)
    }
  }, [])
  return createPortal(
    <div
      ref={ref}
      role="menu"
      aria-label={label}
      tabIndex={-1}
      className="cc-context-menu"
      style={{ left: x, top: y }}
      onContextMenu={(event) => {
        event.preventDefault()
        event.stopPropagation()
      }}
      onPointerDown={(event) => event.stopPropagation()}
      onClick={(event) => event.stopPropagation()}
      onKeyDown={(event) => {
        event.stopPropagation()
        if (event.key === 'Escape' || event.key === 'Tab') {
          event.preventDefault()
          onClose()
          if (
            previous.current instanceof HTMLElement &&
            previous.current.isConnected
          )
            previous.current.focus()
          return
        }
        if (!['ArrowDown', 'ArrowUp', 'Home', 'End'].includes(event.key)) return
        event.preventDefault()
        const buttons = Array.from(
          ref.current?.querySelectorAll<HTMLButtonElement>(
            'button:not(:disabled)',
          ) ?? [],
        )
        if (!buttons.length) return
        const current = buttons.findIndex(
          (button) => button === document.activeElement,
        )
        const next =
          event.key === 'Home'
            ? 0
            : event.key === 'End'
              ? buttons.length - 1
              : (current +
                  (event.key === 'ArrowDown' ? 1 : -1) +
                  buttons.length) %
                buttons.length
        buttons[next]?.focus()
      }}
    >
      {items.map((item) => (
        <button
          key={item.label}
          type="button"
          role="menuitem"
          aria-label={item.label}
          disabled={item.disabled}
          className={item.danger ? 'is-danger' : undefined}
          onClick={() => {
            onClose()
            item.action()
          }}
        >
          {item.icon}
          <span>{item.label}</span>
          {item.shortcut && <kbd>{item.shortcut}</kbd>}
        </button>
      ))}
    </div>,
    document.body,
  )
}
