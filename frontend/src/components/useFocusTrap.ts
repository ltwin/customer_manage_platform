import { useEffect, useRef } from 'react'

// Only the topmost mounted dialog owns keyboard navigation and initial focus.
const focusTrapStack: HTMLElement[] = []

const focusableSelector = [
  'a[href]',
  'button:not([disabled])',
  'input:not([disabled])',
  'select:not([disabled])',
  'textarea:not([disabled])',
  '[tabindex]:not([tabindex="-1"])',
].join(',')

export function useFocusTrap<T extends HTMLElement>(
  open: boolean,
  onClose: () => void,
  closeEnabled = true,
) {
  const containerRef = useRef<T>(null)
  const returnFocusRef = useRef<HTMLElement | null>(null)
  const onCloseRef = useRef(onClose)
  const closeEnabledRef = useRef(closeEnabled)
  onCloseRef.current = onClose
  closeEnabledRef.current = closeEnabled

  useEffect(() => {
    if (!open) return
    returnFocusRef.current =
      document.activeElement instanceof HTMLElement
        ? document.activeElement
        : null
    const container = containerRef.current
    if (!container) return
    focusTrapStack.push(container)
    const focusInitial = () => {
      if (focusTrapStack.at(-1) !== container) return
      const first = focusableElements(container)[0]
      ;(first ?? container).focus()
    }
    let secondFrame = 0
    const focusFrame = window.requestAnimationFrame(() => {
      focusInitial()
      secondFrame = window.requestAnimationFrame(focusInitial)
    })

    function onKeyDown(event: KeyboardEvent) {
      const container = containerRef.current
      if (!container || focusTrapStack.at(-1) !== container) return
      if (event.key === 'Escape' && closeEnabledRef.current) {
        event.preventDefault()
        event.stopPropagation()
        onCloseRef.current()
        return
      }
      if (event.key !== 'Tab') return
      const elements = focusableElements(container)
      if (elements.length === 0) {
        event.preventDefault()
        container.focus()
        return
      }
      const current = elements.indexOf(document.activeElement as HTMLElement)
      const next = event.shiftKey
        ? current <= 0
          ? elements.length - 1
          : current - 1
        : current < 0 || current === elements.length - 1
          ? 0
          : current + 1
      if (
        (event.shiftKey && current <= 0) ||
        (!event.shiftKey && (current < 0 || current === elements.length - 1))
      ) {
        event.preventDefault()
        elements[next]?.focus()
      }
    }

    document.addEventListener('keydown', onKeyDown)
    return () => {
      window.cancelAnimationFrame(focusFrame)
      window.cancelAnimationFrame(secondFrame)
      document.removeEventListener('keydown', onKeyDown)
      const wasTop = focusTrapStack.at(-1) === container
      const index = focusTrapStack.lastIndexOf(container)
      if (index !== -1) focusTrapStack.splice(index, 1)
      const returnTarget = returnFocusRef.current
      if (
        wasTop &&
        returnTarget?.isConnected &&
        !returnTarget.closest('[aria-hidden="true"]')
      ) {
        returnTarget.focus({ preventScroll: true })
      }
    }
  }, [open])

  return containerRef
}

function focusableElements(container: HTMLElement): HTMLElement[] {
  return Array.from(
    container.querySelectorAll<HTMLElement>(focusableSelector),
  ).filter(
    (element) =>
      element.getAttribute('aria-hidden') !== 'true' &&
      element.offsetParent !== null,
  )
}
