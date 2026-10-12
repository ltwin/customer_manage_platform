import { useState } from 'react'
import { halfRating, stepRating } from '../assets/selection.ts'
import { Star } from 'lucide-react'
import { ratingText } from './libraryState.ts'

/**
 * 只读星级：五颗星，半星用裁切。没有评分时不渲染。
 * 可编辑的版本随评分交互（V6-068）补上。
 */
export function Stars({
  value,
  size = 11,
}: {
  value: number | null | undefined
  size?: number
}) {
  if (!value) return null
  return (
    <span className="cl-stars" role="img" aria-label={ratingText(value)}>
      {[1, 2, 3, 4, 5].map((n) => {
        const fill = Math.max(0, Math.min(1, value - n + 1))
        return (
          <span key={n} className="cl-star">
            <Star size={size} />
            {fill > 0 && (
              <span
                className="cl-star-fill"
                style={{ width: `${fill * 100}%` }}
              >
                <Star size={size} />
              </span>
            )}
          </span>
        )
      })}
    </span>
  )
}

/** 原型同款半星滑杆：指针预览，点击当前分数清除，方向键每次半星。 */
export function StarRating({
  value,
  onChange,
  disabled = false,
  label = '评分',
  size = 18,
}: {
  value: number | null
  onChange: (value: number | null) => void
  disabled?: boolean
  label?: string
  size?: number
}) {
  const [preview, setPreview] = useState<number | null>(null)
  const displayed = preview ?? value ?? 0
  return (
    <span
      className="al-rating"
      role="slider"
      tabIndex={disabled ? -1 : 0}
      aria-label={label}
      aria-valuemin={0}
      aria-valuemax={5}
      aria-valuenow={value ?? 0}
      aria-valuetext={value ? `${value} 星` : '未评分'}
      aria-disabled={disabled}
      onPointerLeave={() => setPreview(null)}
      onKeyDown={(e) => {
        if (disabled || e.nativeEvent.isComposing) return
        if (!e.metaKey && !e.ctrlKey && !e.altKey && /^[0-5]$/.test(e.key)) {
          e.preventDefault()
          e.stopPropagation()
          onChange(Number(e.key) || null)
          return
        }
        if (
          [
            'ArrowRight',
            'ArrowUp',
            'ArrowLeft',
            'ArrowDown',
            'Home',
            'Delete',
            'Backspace',
          ].includes(e.key)
        ) {
          e.preventDefault()
          e.stopPropagation()
          onChange(
            ['Home', 'Delete', 'Backspace'].includes(e.key)
              ? null
              : stepRating(
                  value,
                  ['ArrowRight', 'ArrowUp'].includes(e.key) ? 1 : -1,
                ),
          )
        }
      }}
    >
      {[1, 2, 3, 4, 5].map((n) => {
        const pick = (
          e:
            | React.PointerEvent<HTMLSpanElement>
            | React.MouseEvent<HTMLSpanElement>,
        ) => {
          const r = e.currentTarget.getBoundingClientRect()
          return halfRating(n, (e.clientX - r.left) / r.width)
        }
        return (
          <span
            key={n}
            className="cl-star"
            data-star={n}
            onPointerMove={(e) => {
              if (!disabled) setPreview(pick(e))
            }}
            onClick={(e) => {
              e.stopPropagation()
              if (!disabled) {
                const next = pick(e)
                setPreview(null)
                onChange(next === value ? null : next)
              }
            }}
          >
            <Star size={size} />
            <span
              className="cl-star-fill"
              style={{
                width: `${Math.max(0, Math.min(1, displayed - n + 1)) * 100}%`,
              }}
            >
              <Star size={size} />
            </span>
          </span>
        )
      })}
      <span className="al-rating-value">
        {displayed ? displayed.toFixed(1) : ''}
      </span>
    </span>
  )
}
