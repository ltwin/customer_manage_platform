// Typed values during a modal transform, Blender's simple mode: digits, a
// decimal point, a minus that flips the sign, Backspace, and Tab to move to
// the next axis. A typed value overrides the pointer on its axis.

export type Numeric = {
  /** One buffer per axis the transform can take values on (1 to 3). */
  buffers: string[]
  index: number
}

export function startNumeric(axes: number): Numeric {
  return { buffers: Array.from({ length: Math.max(1, Math.min(3, axes)) }, () => ''), index: 0 }
}

export const numericActive = (n: Numeric) => n.buffers.some((b) => b !== '')

const digit = (code: string): string | null => {
  const m = /^(?:Digit|Numpad)([0-9])$/.exec(code)
  return m ? m[1] : null
}

/**
 * Applies one key. Returns the new state, or null when the key is not
 * numeric input and should go to the keymap instead.
 */
export function numericKey(n: Numeric, e: { code: string; shiftKey: boolean }): Numeric | null {
  const buffers = [...n.buffers]
  const cur = buffers[n.index]
  const d = digit(e.code)
  if (d !== null) {
    buffers[n.index] = cur === '-0' ? `-${d}` : cur === '0' ? d : cur + d
    return { ...n, buffers }
  }
  switch (e.code) {
    case 'Period':
    case 'NumpadDecimal':
      if (cur.includes('.')) return n
      buffers[n.index] = cur === '' || cur === '-' ? `${cur}0.` : `${cur}.`
      return { ...n, buffers }
    case 'Minus':
    case 'NumpadSubtract':
      buffers[n.index] = cur.startsWith('-') ? cur.slice(1) : `-${cur}`
      return { ...n, buffers }
    case 'Backspace':
      if (cur === '' && n.index > 0) return { ...n, index: n.index - 1 }
      buffers[n.index] = cur.slice(0, -1)
      return { ...n, buffers }
    case 'Tab': {
      const step = e.shiftKey ? -1 : 1
      return { ...n, index: (n.index + step + buffers.length) % buffers.length }
    }
  }
  return null
}

/** The typed value per axis; null where nothing usable was typed. */
export function numericValues(n: Numeric): (number | null)[] {
  return n.buffers.map((b) => {
    if (b === '' || b === '-') return null
    const v = Number(b)
    return Number.isFinite(v) ? v : null
  })
}

/** How the status bar shows the input, the current axis bracketed. */
export function numericText(n: Numeric, names: readonly string[]): string {
  return n.buffers
    .map((b, i) => {
      const text = `${names[i] ?? ''} ${b === '' ? '0' : b}`.trim()
      return i === n.index ? `[${text}]` : text
    })
    .join('  ')
}
