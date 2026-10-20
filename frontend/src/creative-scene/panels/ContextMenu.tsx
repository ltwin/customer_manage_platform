import { useEffect, useRef } from 'react'

export type MenuItem =
  | { label: string; shortcut?: string; disabled?: boolean; checked?: boolean; run: () => void; /** Why it is disabled, shown under the label. */ hint?: string }
  | 'separator'
  | { section: string }

/** A menu at a screen point; closes on outside press or after a choice. */
export function ContextMenu({ x, y, items, onClose }: { x: number; y: number; items: MenuItem[]; onClose: () => void }) {
  const ref = useRef<HTMLDivElement>(null)
  useEffect(() => {
    ref.current?.querySelector<HTMLButtonElement>('button:not(:disabled)')?.focus()
    const away = (e: PointerEvent) => {
      if (!ref.current?.contains(e.target as Node)) onClose()
    }
    window.addEventListener('pointerdown', away, true)
    return () => window.removeEventListener('pointerdown', away, true)
  }, [onClose])
  const left = Math.min(x, window.innerWidth - 240)
  const top = Math.min(y, window.innerHeight - 32 * items.length - 16)
  return (
    <div ref={ref} className="cc-scene-menu" role="menu" style={{ left, top: Math.max(8, top) }}>
      {items.map((item, i) =>
        item === 'separator' ? (
          <hr key={i} />
        ) : 'section' in item ? (
          <div key={i} className="cc-scene-menu-section">
            {item.section}
          </div>
        ) : (
          <button
            key={i}
            role="menuitem"
            disabled={item.disabled}
            aria-checked={item.checked}
            onClick={() => {
              onClose()
              item.run()
            }}
          >
            <span>
              {item.checked !== undefined && <span className="cc-scene-check">{item.checked ? '●' : ''}</span>}
              {item.label}
              {item.disabled && item.hint && <small className="cc-scene-menu-hint">{item.hint}</small>}
            </span>
            {item.shortcut && <kbd>{item.shortcut}</kbd>}
          </button>
        ),
      )}
    </div>
  )
}
