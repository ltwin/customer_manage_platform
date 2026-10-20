// The editor's keymap: Blender 2.80+ defaults (left-click select). This table
// is the only source for the dispatcher, the status bar, menu shortcut labels
// and the shortcut sheet, so what is shown always matches what happens.

export type Context = 'idle' | 'modal' | 'boxselect' | 'menu' | 'preview'

export type Action =
  | 'add-menu'
  | 'grab'
  | 'rotate'
  | 'scale'
  | 'duplicate'
  | 'delete-confirm'
  | 'delete'
  | 'hide'
  | 'hide-unselected'
  | 'reveal'
  | 'clear-location'
  | 'clear-rotation'
  | 'clear-scale'
  | 'rename'
  | 'group'
  | 'ungroup'
  | 'parent'
  | 'clear-parent'
  | 'select-all'
  | 'deselect-all'
  | 'toggle-tool'
  | 'toggle-toolbar'
  | 'toggle-sidebar'
  | 'view-front'
  | 'view-back'
  | 'view-right'
  | 'view-left'
  | 'view-top'
  | 'view-bottom'
  | 'view-ortho'
  | 'camera-view'
  | 'camera-to-view'
  | 'frame-selected'
  | 'frame-all'
  | 'view-menu'
  | 'undo'
  | 'redo'
  | 'save'
  | 'shortcuts'
  | 'escape'
  | 'axis-x'
  | 'axis-y'
  | 'axis-z'
  | 'plane-x'
  | 'plane-y'
  | 'plane-z'
  | 'confirm'
  | 'cancel'

export type Group = '导航' | '选择' | '变换' | '编辑' | '视图' | '通用'

/** Modifiers must match exactly; an omitted one must not be held. */
export type Mods = { mod?: boolean; shift?: boolean; alt?: boolean }

export type Binding = {
  codes: string[]
  mods?: Mods
  contexts: Context[]
  action: Action
  label: string
  group: Group
}

/** Pointer gestures shown on the sheet and status bar; not dispatched here. */
export type Gesture = { keys: string; label: string; group: Group }

const idle: Context[] = ['idle']
const viewing: Context[] = ['idle', 'preview']
const modal: Context[] = ['modal']

export const bindings: readonly Binding[] = [
  { codes: ['KeyA'], mods: { shift: true }, contexts: idle, action: 'add-menu', label: '添加', group: '编辑' },
  { codes: ['KeyG'], contexts: idle, action: 'grab', label: '移动', group: '变换' },
  { codes: ['KeyR'], contexts: idle, action: 'rotate', label: '旋转', group: '变换' },
  { codes: ['KeyS'], contexts: idle, action: 'scale', label: '缩放', group: '变换' },
  { codes: ['KeyD'], mods: { shift: true }, contexts: idle, action: 'duplicate', label: '复制', group: '编辑' },
  { codes: ['KeyX'], contexts: idle, action: 'delete-confirm', label: '删除', group: '编辑' },
  { codes: ['Delete'], contexts: idle, action: 'delete', label: '直接删除', group: '编辑' },
  { codes: ['KeyH'], contexts: idle, action: 'hide', label: '隐藏', group: '编辑' },
  { codes: ['KeyH'], mods: { shift: true }, contexts: idle, action: 'hide-unselected', label: '隐藏未选', group: '编辑' },
  { codes: ['KeyH'], mods: { alt: true }, contexts: idle, action: 'reveal', label: '全部显示', group: '编辑' },
  { codes: ['KeyG'], mods: { alt: true }, contexts: idle, action: 'clear-location', label: '清除位置', group: '变换' },
  { codes: ['KeyR'], mods: { alt: true }, contexts: idle, action: 'clear-rotation', label: '清除旋转', group: '变换' },
  { codes: ['KeyS'], mods: { alt: true }, contexts: idle, action: 'clear-scale', label: '清除缩放', group: '变换' },
  { codes: ['F2'], contexts: idle, action: 'rename', label: '重命名', group: '编辑' },
  { codes: ['KeyG'], mods: { mod: true }, contexts: idle, action: 'group', label: '编组', group: '编辑' },
  { codes: ['KeyG'], mods: { mod: true, alt: true }, contexts: idle, action: 'ungroup', label: '取消编组', group: '编辑' },
  { codes: ['KeyP'], mods: { mod: true }, contexts: idle, action: 'parent', label: '设置父级（挂到活动物件下）', group: '编辑' },
  { codes: ['KeyP'], mods: { alt: true }, contexts: idle, action: 'clear-parent', label: '清除父级', group: '编辑' },
  { codes: ['KeyA'], contexts: idle, action: 'select-all', label: '全选', group: '选择' },
  { codes: ['KeyA'], mods: { alt: true }, contexts: idle, action: 'deselect-all', label: '取消全选', group: '选择' },
  { codes: ['KeyW'], contexts: idle, action: 'toggle-tool', label: '切换框选 / 调整', group: '选择' },
  { codes: ['KeyT'], contexts: viewing, action: 'toggle-toolbar', label: '工具栏', group: '视图' },
  { codes: ['KeyN'], contexts: viewing, action: 'toggle-sidebar', label: '侧栏', group: '视图' },
  { codes: ['Digit1', 'Numpad1'], contexts: viewing, action: 'view-front', label: '前视', group: '视图' },
  { codes: ['Digit1', 'Numpad1'], mods: { mod: true }, contexts: viewing, action: 'view-back', label: '后视', group: '视图' },
  { codes: ['Digit3', 'Numpad3'], contexts: viewing, action: 'view-right', label: '右视', group: '视图' },
  { codes: ['Digit3', 'Numpad3'], mods: { mod: true }, contexts: viewing, action: 'view-left', label: '左视', group: '视图' },
  { codes: ['Digit7', 'Numpad7'], contexts: viewing, action: 'view-top', label: '顶视', group: '视图' },
  { codes: ['Digit7', 'Numpad7'], mods: { mod: true }, contexts: viewing, action: 'view-bottom', label: '底视', group: '视图' },
  { codes: ['Digit5', 'Numpad5'], contexts: viewing, action: 'view-ortho', label: '正交 / 透视', group: '视图' },
  { codes: ['Digit0', 'Numpad0'], contexts: viewing, action: 'camera-view', label: '相机视图', group: '视图' },
  { codes: ['Digit0', 'Numpad0'], mods: { mod: true, alt: true }, contexts: idle, action: 'camera-to-view', label: '摄像机对齐当前视角', group: '视图' },
  { codes: ['Period', 'NumpadDecimal'], contexts: viewing, action: 'frame-selected', label: '聚焦选中', group: '视图' },
  { codes: ['Home'], contexts: viewing, action: 'frame-all', label: '显示全部', group: '视图' },
  { codes: ['Backquote'], contexts: viewing, action: 'view-menu', label: '视图菜单', group: '视图' },
  { codes: ['KeyZ'], mods: { mod: true }, contexts: idle, action: 'undo', label: '撤销', group: '通用' },
  { codes: ['KeyZ'], mods: { mod: true, shift: true }, contexts: idle, action: 'redo', label: '重做', group: '通用' },
  { codes: ['KeyS'], mods: { mod: true }, contexts: ['idle', 'modal', 'boxselect', 'menu', 'preview'], action: 'save', label: '保存（自动）', group: '通用' },
  { codes: ['Slash'], mods: { shift: true }, contexts: viewing, action: 'shortcuts', label: '快捷键速查', group: '通用' },
  { codes: ['Escape'], contexts: ['idle', 'boxselect', 'menu', 'preview'], action: 'escape', label: '取消 / 退出', group: '通用' },
  { codes: ['KeyX'], contexts: modal, action: 'axis-x', label: '锁定 X 轴', group: '变换' },
  { codes: ['KeyY'], contexts: modal, action: 'axis-y', label: '锁定 Y 轴', group: '变换' },
  { codes: ['KeyZ'], contexts: modal, action: 'axis-z', label: '锁定 Z 轴', group: '变换' },
  { codes: ['KeyX'], mods: { shift: true }, contexts: modal, action: 'plane-x', label: '排除 X 轴', group: '变换' },
  { codes: ['KeyY'], mods: { shift: true }, contexts: modal, action: 'plane-y', label: '排除 Y 轴', group: '变换' },
  { codes: ['KeyZ'], mods: { shift: true }, contexts: modal, action: 'plane-z', label: '排除 Z 轴', group: '变换' },
  { codes: ['KeyG'], contexts: modal, action: 'grab', label: '改为移动', group: '变换' },
  { codes: ['KeyR'], contexts: modal, action: 'rotate', label: '改为旋转', group: '变换' },
  { codes: ['KeyS'], contexts: modal, action: 'scale', label: '改为缩放', group: '变换' },
  { codes: ['Enter', 'NumpadEnter', 'Space'], contexts: modal, action: 'confirm', label: '确认', group: '变换' },
  { codes: ['Escape'], contexts: modal, action: 'cancel', label: '取消', group: '变换' },
]

export const gestures: readonly Gesture[] = [
  { keys: '中键拖动', label: '环绕视角', group: '导航' },
  { keys: 'Shift + 中键拖动', label: '平移视角', group: '导航' },
  { keys: 'Ctrl + 中键拖动 / 滚轮', label: '推拉视角', group: '导航' },
  { keys: 'Alt + 左键拖动', label: '环绕（没有中键时）', group: '导航' },
  { keys: '触控板双指滑动', label: '环绕（触控板模式）', group: '导航' },
  { keys: '触控板捏合', label: '推拉', group: '导航' },
  { keys: '左键单击', label: '选中', group: '选择' },
  { keys: 'Shift + 单击', label: '加选 / 设为活动 / 取消', group: '选择' },
  { keys: '左键拖动', label: '框选（Shift 加选，Ctrl 减选）', group: '选择' },
  { keys: '变换中按 Ctrl', label: '吸附（1 m / 5° / 0.1）', group: '变换' },
  { keys: '变换中按 Shift', label: '精细调整', group: '变换' },
  { keys: '变换中输入数字', label: '精确数值，Tab 换轴', group: '变换' },
  { keys: '右键', label: '菜单；变换中为取消', group: '编辑' },
]

export type KeyLike = {
  code: string
  key?: string
  metaKey: boolean
  ctrlKey: boolean
  shiftKey: boolean
  altKey: boolean
  isComposing?: boolean
  /** The key is held down and the system is repeating it. */
  repeat?: boolean
  target?: unknown
}

export function isMac(platform: string = typeof navigator === 'undefined' ? '' : navigator.platform): boolean {
  return /Mac|iPhone|iPad/.test(platform)
}

/** Keys typed into a field or during IME composition are never shortcuts. */
export function typing(e: KeyLike): boolean {
  if (e.isComposing) return true
  const t = e.target as { tagName?: string; isContentEditable?: boolean } | null | undefined
  if (!t) return false
  if (t.isContentEditable) return true
  return ['INPUT', 'TEXTAREA', 'SELECT'].includes((t.tagName ?? '').toUpperCase())
}

function codeOf(e: KeyLike, mac: boolean): string {
  // Mac laptops have no separate Delete key.
  return mac && e.code === 'Backspace' ? 'Delete' : e.code
}

export function matches(e: KeyLike, b: Binding, mac: boolean): boolean {
  const mod = mac ? e.metaKey : e.ctrlKey
  const other = mac ? e.ctrlKey : e.metaKey
  const m = b.mods ?? {}
  return (
    b.codes.includes(codeOf(e, mac)) &&
    mod === !!m.mod &&
    e.shiftKey === !!m.shift &&
    e.altKey === !!m.alt &&
    !other
  )
}

export function lookup(e: KeyLike, context: Context, mac: boolean): Binding | null {
  if (typing(e)) return null
  return bindings.find((b) => b.contexts.includes(context) && matches(e, b, mac)) ?? null
}

export type KeyEventLike = KeyLike & { preventDefault(): void; stopPropagation(): void }

/** Holding Cmd+Z steps back one edit after another, as in Blender. */
const repeating: ReadonlySet<Action> = new Set(['undo', 'redo'])

/**
 * Handles one keydown at the editor root. A matched key is claimed so the
 * browser and the canvas behind the editor do nothing with it. A held key
 * acts once: repeats would flip a panel or an axis back and forth, and each
 * repeated edit would be a revision of its own.
 */
export function handleKey(
  e: KeyEventLike,
  context: Context,
  mac: boolean,
  run: (action: Action) => void,
): boolean {
  const b = lookup(e, context, mac)
  if (!b) return false
  e.preventDefault()
  e.stopPropagation()
  if (!e.repeat || repeating.has(b.action)) run(b.action)
  return true
}

/**
 * Esc closes the innermost thing: a modal transform, then a menu, then a
 * history preview, then the selection. It never closes the editor.
 */
export function escapeTarget(s: { modal: boolean; menu: boolean; preview: boolean; selection: boolean }):
  | 'cancel-modal'
  | 'close-menu'
  | 'exit-preview'
  | 'deselect'
  | 'none' {
  if (s.modal) return 'cancel-modal'
  if (s.menu) return 'close-menu'
  if (s.preview) return 'exit-preview'
  if (s.selection) return 'deselect'
  return 'none'
}

const keyNames: Record<string, string> = {
  Period: '.',
  NumpadDecimal: '.',
  Backquote: '`',
  Slash: '/',
  Escape: 'Esc',
  Space: '空格',
  NumpadEnter: 'Enter',
}

function keyName(code: string, mac: boolean): string {
  if (code === 'Delete') return mac ? '⌫' : 'Delete'
  if (keyNames[code]) return keyNames[code]
  const m = /^(?:Key|Digit|Numpad)(.+)$/.exec(code)
  return m ? m[1] : code
}

/** How a binding reads on screen, for example 「Shift A」 or 「⌘Z」. */
export function keysLabel(b: Binding, mac: boolean): string {
  if (b.action === 'shortcuts') return '?'
  const m = b.mods ?? {}
  const parts = [
    m.mod ? (mac ? '⌘' : 'Ctrl') : '',
    m.alt ? (mac ? '⌥' : 'Alt') : '',
    m.shift ? (mac ? '⇧' : 'Shift') : '',
  ].filter(Boolean)
  const key = keyName(b.codes[0], mac)
  return mac ? parts.join('') + key : [...parts, key].join(' + ')
}

/** The label of an action's shortcut in a context, for menus and hints. */
export function shortcutFor(action: Action, mac: boolean, context: Context = 'idle'): string {
  const b = bindings.find((x) => x.action === action && x.contexts.includes(context))
  return b ? keysLabel(b, mac) : ''
}
