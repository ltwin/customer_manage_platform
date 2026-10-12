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
