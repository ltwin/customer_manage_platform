import { useEffect, useId, useRef, useState, type KeyboardEvent } from 'react'
import { Check, Search, Wand2, X } from 'lucide-react'
import { skillMatches, type Skill, type SlashItem } from './agentContext.ts'
import './skills.css'

/**
 * 输入框工具栏里的 Skill 入口：点开是浮在输入框上方的面板，搜索并挑一个内置 Skill；
 * 再点已选的那个就是不用。选中的 Skill 以标签显示在输入框里，随这条消息发出。
 */
export function SkillPanel({
  skills,
  loading,
  value,
  disabled,
  onChange,
}: {
  skills: Skill[]
  loading: boolean
  value: string
  disabled?: boolean
  onChange: (skillVersionID: string) => void
}) {
  const id = useId()
  const trigger = useRef<HTMLButtonElement>(null)
  const panel = useRef<HTMLDivElement>(null)
  const [open, setOpen] = useState(false)
  const [query, setQuery] = useState('')
  const [index, setIndex] = useState(0)
  const list = skills.filter((skill) => skillMatches(skill, query))
  const active = Math.min(index, list.length - 1)

  useEffect(() => {
    if (disabled) setOpen(false)
  }, [disabled])
  useEffect(() => {
    if (!open) return
    const dismiss = (event: PointerEvent) => {
      const target = event.target
      if (
        target instanceof Node &&
        !panel.current?.contains(target) &&
        !trigger.current?.contains(target)
      )
        setOpen(false)
    }
    document.addEventListener('pointerdown', dismiss, true)
    return () => document.removeEventListener('pointerdown', dismiss, true)
  }, [open])

  function show() {
    setQuery('')
    const selected = skills.findIndex((s) => s.skill_version_id === value)
    setIndex(Math.max(0, selected))
    setOpen(true)
  }
  function close() {
    setOpen(false)
    trigger.current?.focus()
  }
  function choose(skill: Skill | undefined) {
    if (!skill) return
    setOpen(false)
    onChange(skill.skill_version_id === value ? '' : skill.skill_version_id)
  }
  function onKeyDown(event: KeyboardEvent<HTMLInputElement>) {
    if (event.nativeEvent.isComposing) return
    if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
      event.preventDefault()
      if (list.length)
        setIndex(
          (active + (event.key === 'ArrowDown' ? 1 : list.length - 1)) %
            list.length,
        )
    } else if (event.key === 'Enter') {
      event.preventDefault()
      choose(list[active])
    } else if (event.key === 'Escape') {
      // 只收起面板，不让外层面板或弹窗跟着关。
      event.preventDefault()
      event.stopPropagation()
      close()
    }
  }

  return (
    <>
      <button
        ref={trigger}
        type="button"
        className={`cc-skill-trigger${value ? ' is-on' : ''}`}
        aria-label="Skill"
        aria-haspopup="dialog"
        aria-expanded={open}
        title="Skill · 输入 / 也能调出"
        disabled={disabled}
        onClick={() => (open ? close() : show())}
      >
        <Wand2 size={15} />
      </button>
      {open && (
        <div ref={panel} className="cc-skill-panel" role="dialog" aria-label="Skill">
          <div className="cc-skill-head">
            <h3>Skill</h3>
            <label className="cc-skill-search">
              <Search size={14} aria-hidden="true" />
              <input
                autoFocus
                aria-label="搜索 Skill"
                placeholder="搜索 Skill"
                role="combobox"
                aria-expanded="true"
                aria-controls={id}
                aria-activedescendant={
                  active >= 0 ? `${id}-${active}` : undefined
                }
                value={query}
                onChange={(event) => {
                  setQuery(event.target.value)
                  setIndex(0)
                }}
                onKeyDown={onKeyDown}
              />
            </label>
          </div>
          <div className="cc-skill-group">内置</div>
          <ul id={id} role="listbox" aria-label="内置 Skill">
            {list.map((skill, i) => {
              const on = skill.skill_version_id === value
              return (
                <li
                  key={skill.skill_id}
                  id={`${id}-${i}`}
                  role="option"
                  aria-selected={on}
                  className={i === active ? 'is-active' : undefined}
                  onPointerMove={() => i !== active && setIndex(i)}
                  onPointerDown={(event) => event.preventDefault()}
                  onClick={() => choose(skill)}
                >
                  <span className="cc-skill-icon">
                    <Wand2 size={15} />
                  </span>
                  <span className="cc-skill-text">
                    <span className="cc-skill-name">
                      {skill.display_name}
                      <code>/{skill.slug}</code>
                    </span>
                    {skill.description && (
                      <span className="cc-skill-desc">{skill.description}</span>
                    )}
                  </span>
                  {on && <Check size={14} className="cc-skill-check" />}
                </li>
              )
            })}
          </ul>
          {!list.length && (
            <p className="cc-skill-empty">
              {loading
                ? '加载中…'
                : skills.length
                  ? '没有匹配的 Skill'
                  : '还没有可用的 Skill'}
            </p>
          )}
          <p className="cc-skill-foot">在输入框里输入 / 也能调用</p>
        </div>
      )}
    </>
  )
}

// 输入框里这条消息要用的 Skill；点叉不用。
export function SkillTag({
  skill,
  disabled,
  onRemove,
}: {
  skill: Skill
  disabled?: boolean
  onRemove: () => void
}) {
  return (
    <span className="cc-skill-tag" title={skill.description || undefined}>
      <Wand2 size={13} aria-hidden="true" />
      <span>{skill.display_name}</span>
      <button
        type="button"
        aria-label={`不用 Skill「${skill.display_name}」`}
        disabled={disabled}
        onClick={onRemove}
      >
        <X size={12} />
      </button>
    </span>
  )
}

// 输入 / 时浮在输入框上方的菜单：命令在前，内置 Skill 在后。
export function SlashMenu({
  items,
  active,
  onHover,
  onChoose,
}: {
  items: SlashItem[]
  active: number
  onHover: (index: number) => void
  onChoose: (item: SlashItem) => void
}) {
  const id = useId()
  if (!items.length) return null
  return (
    <ul className="cc-slash" role="listbox" aria-label="命令与 Skill">
      {items.map((item, index) => {
        const heading =
          index === 0 || items[index - 1].kind !== item.kind
            ? item.kind === 'command'
              ? '命令'
              : 'Skill'
            : null
        const key = item.kind === 'command' ? item.name : item.skill.skill_id
        return [
          heading && (
            <li key={`${key}-group`} role="presentation" className="cc-slash-group">
              {heading}
            </li>
          ),
          <li
            key={key}
            id={`${id}-${index}`}
            role="option"
            aria-selected={index === active}
            className={index === active ? 'is-active' : undefined}
            onPointerDown={(event) => event.preventDefault()}
            onPointerMove={() => index !== active && onHover(index)}
            onClick={() => onChoose(item)}
          >
            {item.kind === 'command' ? (
              <>
                <code>/{item.name}</code>
                <span className="cc-slash-desc">{item.description}</span>
              </>
            ) : (
              <>
                <Wand2 size={14} aria-hidden="true" />
                <span className="cc-slash-name">{item.skill.display_name}</span>
                <code>/{item.skill.slug}</code>
                <span className="cc-slash-desc">{item.skill.description}</span>
              </>
            )}
          </li>,
        ]
      })}
    </ul>
  )
}
