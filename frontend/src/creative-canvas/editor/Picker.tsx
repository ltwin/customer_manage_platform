import {
  useEffect,
  useId,
  useLayoutEffect,
  useRef,
  useState,
  type KeyboardEvent,
  type ReactNode,
} from 'react'
import { createPortal } from 'react-dom'
import { Check, ChevronDown } from 'lucide-react'
import './picker.css'

export type PickerOption = {
  value: string
  label: string
  /** 高亮时显示在名称下方的一句话。 */
  description?: string
  icon?: ReactNode
  /** 右侧的小标签，如能力或耗时。 */
  meta?: string
  /** 层级缩进，用于子分组。 */
  depth?: number
  disabled?: boolean
}

type Common = {
  label: string
  options: PickerOption[]
  disabled?: boolean
  placeholder?: string
  /** field：表单里的整行输入框样式；缺省是安静的 chip。 */
  variant?: 'chip' | 'field'
  className?: string
  /** 触发按钮的内容；缺省显示所选项的图标和名称。 */
  children?: ReactNode
  /** 列表最小宽度；缺省与触发按钮同宽。 */
  minWidth?: number
  align?: 'start' | 'end'
  empty?: string
}
type Single = Common & {
  value: string
  onChange: (value: string) => void
  /** 给出时带一个同名隐藏字段，让外层表单照常读取 FormData。 */
  name?: string
  values?: never
  onToggle?: never
}
/** 多选：点选只切换该项，列表保持打开。 */
type Multiple = Common & {
  values: string[]
  onToggle: (value: string) => void
  value?: never
  onChange?: never
  name?: never
}

const gap = 6
const margin = 8

/**
 * 画布里的下拉选择：替代原生 select，列表浮在工作区（或所在弹窗）顶层，
 * 不被面板的滚动区裁切，也不继承面板的按钮样式。
 * 焦点留在触发按钮上，用 aria-activedescendant 指向高亮项。
 */
export default function Picker(props: Single | Multiple) {
  const {
    label,
    options,
    disabled,
    placeholder = '请选择',
    variant = 'chip',
    className = '',
    children,
    minWidth = 0,
    align = 'start',
    empty = '暂无可选项',
  } = props
  const multiple = props.values !== undefined
  const isSelected = (option: PickerOption) =>
    props.values !== undefined
      ? props.values.includes(option.value)
      : option.value === props.value
  const id = useId()
  const trigger = useRef<HTMLButtonElement>(null)
  const list = useRef<HTMLDivElement>(null)
  const [open, setOpen] = useState(false)
  const [active, setActive] = useState(-1)
  const [host, setHost] = useState<HTMLElement | null>(null)
  const chosen = options.filter(isSelected)
  const summary = multiple
    ? chosen.map((o) => o.label).join('、')
    : chosen[0]?.label

  function show() {
    if (disabled) return
    const index = options.findIndex((o) => isSelected(o) && !o.disabled)
    setActive(index >= 0 ? index : step(options, -1, 1))
    // 弹窗的遮罩自成层叠上下文，列表要放进去才压得住弹窗；
    // 其余情况浮在工作区里，才拿得到主题变量。
    const t = trigger.current
    setHost(
      t?.closest<HTMLElement>('.overlay') ??
        t?.closest<HTMLElement>('.cc-workspace') ??
        document.body,
    )
    setOpen(true)
  }
  function close(refocus = true) {
    setOpen(false)
    if (refocus) trigger.current?.focus()
  }
  function choose(option: PickerOption | undefined) {
    if (!option || option.disabled) return
    if (props.values !== undefined) {
      props.onToggle(option.value)
      return
    }
    close()
    if (option.value !== props.value) props.onChange(option.value)
  }

  useEffect(() => {
    if (disabled) setOpen(false)
  }, [disabled])
  useEffect(() => {
    if (!open) return
    const inside = (target: EventTarget | null) =>
      target instanceof Node &&
      (!!trigger.current?.contains(target) || !!list.current?.contains(target))
    const dismiss = (event: Event) => {
      if (!inside(event.target)) setOpen(false)
    }
    document.addEventListener('pointerdown', dismiss, true)
    document.addEventListener('wheel', dismiss, true)
    document.addEventListener('scroll', dismiss, true)
    return () => {
      document.removeEventListener('pointerdown', dismiss, true)
      document.removeEventListener('wheel', dismiss, true)
      document.removeEventListener('scroll', dismiss, true)
    }
  }, [open])
  // 先按自然尺寸渲染，再量出列表大小决定朝上还是朝下，并夹在视口内。
  useLayoutEffect(() => {
    if (!open) return
    const place = () => {
      const t = trigger.current?.getBoundingClientRect()
      const l = list.current
      if (!t || !l) return
      l.style.minWidth = `${Math.max(t.width, minWidth)}px`
      l.style.maxHeight = ''
      const height = l.offsetHeight
      const width = l.offsetWidth
      const below = window.innerHeight - t.bottom - gap - margin
      const above = t.top - gap - margin
      const up = height > below && above > below
      l.dataset.side = up ? 'top' : 'bottom'
      l.style.maxHeight = `${Math.max(120, up ? above : below)}px`
      l.style.top = up ? 'auto' : `${t.bottom + gap}px`
      l.style.bottom = up ? `${window.innerHeight - t.top + gap}px` : 'auto'
      const left = align === 'end' ? t.right - width : t.left
      l.style.left = `${Math.max(margin, Math.min(left, window.innerWidth - width - margin))}px`
    }
    place()
    window.addEventListener('resize', place)
    return () => window.removeEventListener('resize', place)
  }, [open, minWidth, align, options.length])
  // 只滚列表自己：scrollIntoView 会连带滚动外层，触发上面的关闭。
  useEffect(() => {
    const l = list.current
    const item =
      open && active >= 0 ? document.getElementById(`${id}-${active}`) : null
    if (!l || !item) return
    if (item.offsetTop < l.scrollTop) l.scrollTop = item.offsetTop - gap
    else if (item.offsetTop + item.offsetHeight > l.scrollTop + l.clientHeight)
      l.scrollTop = item.offsetTop + item.offsetHeight - l.clientHeight + gap
  }, [open, active, id])

  function onKeyDown(event: KeyboardEvent<HTMLButtonElement>) {
    if (!open) {
      if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
        event.preventDefault()
        show()
      }
      return
    }
    switch (event.key) {
      case 'ArrowDown':
      case 'ArrowUp':
        event.preventDefault()
        setActive((i) => step(options, i, event.key === 'ArrowDown' ? 1 : -1))
        break
      case 'Home':
        event.preventDefault()
        setActive(step(options, -1, 1))
        break
      case 'End':
        event.preventDefault()
        setActive(step(options, options.length, -1))
        break
      case 'Enter':
      case ' ':
        event.preventDefault()
        choose(options[active])
        break
      case 'Escape':
        // 只关列表，不让外层面板或弹窗把自己也关掉。
        event.preventDefault()
        event.stopPropagation()
        close()
        break
      case 'Tab':
        close(false)
        break
    }
  }

  return (
    <span className={`cc-picker is-${variant} ${className}`}>
      <button
        ref={trigger}
        type="button"
        className="cc-picker-trigger"
        role="combobox"
        aria-label={label}
        aria-haspopup="listbox"
        aria-expanded={open}
        aria-controls={open ? id : undefined}
        aria-activedescendant={
          open && active >= 0 ? `${id}-${active}` : undefined
        }
        disabled={disabled}
        title={summary || undefined}
        onClick={() => (open ? close() : show())}
        onKeyDown={onKeyDown}
      >
        {children ?? (
          <>
            {!multiple && chosen[0]?.icon}
            <span className={`cc-picker-value${summary ? '' : ' is-empty'}`}>
              {summary || placeholder}
            </span>
            <ChevronDown size={12} className="cc-picker-chev" />
          </>
        )}
      </button>
      {props.name !== undefined && (
        <input type="hidden" name={props.name} value={props.value} />
      )}
      {open &&
        host &&
        createPortal(
          <div
            ref={list}
            id={id}
            className="cc-picker-list"
            role="listbox"
            aria-label={label}
            aria-multiselectable={multiple || undefined}
            // 点选不抢走触发按钮的焦点。
            onPointerDown={(e) => e.preventDefault()}
          >
            {options.length === 0 && (
              <div className="cc-picker-empty">{empty}</div>
            )}
            {options.map((option, index) => {
              const on = isSelected(option)
              return (
                <div
                  key={option.value}
                  id={`${id}-${index}`}
                  role="option"
                  aria-selected={on}
                  aria-disabled={option.disabled || undefined}
                  className={`cc-picker-option${index === active ? ' is-active' : ''}${on ? ' is-selected' : ''}`}
                  style={
                    option.depth
                      ? { paddingLeft: 5 + option.depth * 18 }
                      : undefined
                  }
                  // 用 pointermove 而不是 enter：说明展开会推动下方各项，
                  // 指针不动时不该因此换高亮。
                  onPointerMove={() => {
                    if (!option.disabled && index !== active) setActive(index)
                  }}
                  onClick={() => {
                    setActive(index)
                    choose(option)
                  }}
                >
                  {multiple && (
                    <span className="cc-picker-box" aria-hidden="true">
                      {on && <Check size={12} />}
                    </span>
                  )}
                  {option.icon && (
                    <span className="cc-picker-icon">{option.icon}</span>
                  )}
                  <span className="cc-picker-text">
                    <span className="cc-picker-label">{option.label}</span>
                    {option.description && index === active && (
                      <span className="cc-picker-desc">
                        {option.description}
                      </span>
                    )}
                  </span>
                  {option.meta && (
                    <span className="cc-picker-meta">{option.meta}</span>
                  )}
                  {on && !multiple && !option.meta && (
                    <Check size={14} className="cc-picker-check" />
                  )}
                </div>
              )
            })}
          </div>,
          host,
        )}
    </span>
  )
}

function step(options: PickerOption[], from: number, by: 1 | -1): number {
  for (let i = from + by; i >= 0 && i < options.length; i += by)
    if (!options[i].disabled) return i
  return from >= 0 && from < options.length ? from : -1
}
