export type NoteInputAction = 'submit' | 'cancel' | 'none'

export type NoteSubmitGate = {
  tryStart: () => boolean
  finish: () => void
}

type NoteInputEvent = {
  key: string
  isComposing: boolean
  shiftKey?: boolean
  multiline?: boolean
}

export function noteInputAction({
  key,
  isComposing,
  shiftKey = false,
  multiline = false,
}: NoteInputEvent): NoteInputAction {
  if (key === 'Escape') return 'cancel'
  if (key !== 'Enter' || isComposing) return 'none'
  if (multiline && shiftKey) return 'none'
  return 'submit'
}

export function createNoteSubmitGate(): NoteSubmitGate {
  let inFlight = false

  return {
    tryStart() {
      if (inFlight) return false
      inFlight = true
      return true
    },
    finish() {
      inFlight = false
    },
  }
}
