// Viewport navigation, Blender style. The middle button and Alt + left drag
// both orbit (Shift pans, Ctrl dollies). The wheel depends on the device:
// a mouse wheel dollies, two fingers on a trackpad orbit. Browsers do not say
// which device sent a wheel event, so the editor guesses and the photographer
// can choose in the View menu.

export type Device = 'mouse' | 'trackpad'
export type DevicePreference = 'auto' | Device

export type WheelSample = {
  deltaX: number
  deltaY: number
  deltaMode: number
  ctrlKey: boolean
  shiftKey: boolean
  altKey: boolean
  metaKey: boolean
  /** Event time in milliseconds. */
  t: number
}

export type WheelAction =
  | { kind: 'dolly'; amount: number }
  | { kind: 'orbit'; dx: number; dy: number }
  | { kind: 'pan'; dx: number; dy: number }

const lineHeight = 16
const pageHeight = 800

/** Wheel deltas in pixels, whatever unit the browser reported. */
export function pixels(e: Pick<WheelSample, 'deltaX' | 'deltaY' | 'deltaMode'>): { dx: number; dy: number } {
  const k = e.deltaMode === 1 ? lineHeight : e.deltaMode === 2 ? pageHeight : 1
  return { dx: e.deltaX * k, dy: e.deltaY * k }
}

/**
 * What one wheel event does. A pinch arrives as a wheel with ctrlKey set, so
 * ctrlKey (and Cmd) always dollies, in either mode.
 */
export function wheelAction(e: WheelSample, device: Device): WheelAction {
  const { dx, dy } = pixels(e)
  if (e.ctrlKey || e.metaKey) return { kind: 'dolly', amount: dy }
  if (device === 'mouse') return e.shiftKey ? { kind: 'pan', dx: 0, dy } : { kind: 'dolly', amount: dy }
  return e.shiftKey ? { kind: 'pan', dx, dy } : { kind: 'orbit', dx, dy }
}

/**
 * Guesses the device from recent wheel events, or null when unsure.
 *
 * Working hypothesis until the recorded device samples are in: a trackpad
 * reports pixel deltas with sideways movement, fractions or runs of small
 * steps; a mouse wheel reports lines, or the same large step every notch.
 * Pinches (ctrlKey) say nothing about the device.
 */
export function classifyWheel(samples: readonly WheelSample[]): Device | null {
  const recent = samples.filter((s) => !s.ctrlKey).slice(-8)
  if (!recent.length) return null
  if (recent.some((s) => s.deltaMode !== 0)) return 'mouse'
  if (recent.some((s) => Math.abs(s.deltaX) > 0.5)) return 'trackpad'
  if (recent.some((s) => !Number.isInteger(s.deltaY))) return 'trackpad'
  const steps = recent.map((s) => Math.abs(s.deltaY)).filter((v) => v > 0)
  if (steps.length >= 3 && steps.every((v) => v < 10)) return 'trackpad'
  if (steps.length >= 2 && steps.every((v) => v >= 50 && v === steps[0])) return 'mouse'
  return null
}

/** The device in use: the photographer's choice wins over the guess. */
export function resolveDevice(preference: DevicePreference, guessed: Device | null): Device {
  if (preference !== 'auto') return preference
  return guessed ?? 'mouse'
}

export type ButtonAction = 'rotate' | 'truck' | 'dolly' | 'none'

/**
 * Which camera action each button drives, given the held modifiers. The
 * right button never drives the camera: it opens menus and cancels.
 */
export function buttonActions(mods: { alt: boolean; shift: boolean; ctrl: boolean }): {
  left: ButtonAction
  middle: ButtonAction
} {
  const drag: ButtonAction = mods.shift ? 'truck' : mods.ctrl ? 'dolly' : 'rotate'
  return { left: mods.alt ? drag : 'none', middle: drag }
}

const preferenceKey = 'creative-scene:navigation-device'

export function readPreference(storage: Pick<Storage, 'getItem'> | undefined): DevicePreference {
  try {
    const v = storage?.getItem(preferenceKey)
    return v === 'mouse' || v === 'trackpad' ? v : 'auto'
  } catch {
    return 'auto'
  }
}

export function writePreference(storage: Pick<Storage, 'setItem' | 'removeItem'> | undefined, p: DevicePreference) {
  try {
    if (p === 'auto') storage?.removeItem(preferenceKey)
    else storage?.setItem(preferenceKey, p)
  } catch {
    // Without storage the choice lasts until the page closes.
  }
}

/** Camera directions for the number-row views; Y is up. */
export const viewDirections = {
  'view-front': [0, 0, 1],
  'view-back': [0, 0, -1],
  'view-right': [1, 0, 0],
  'view-left': [-1, 0, 0],
  'view-top': [0, 1, 0],
  'view-bottom': [0, -1, 0],
} as const
