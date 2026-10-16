import { RotateCcw } from 'lucide-react'
import { generationFailureText } from './labelMeta.ts'

// 最近一次生成失败：和生成中同一层毛玻璃压住旧内容，不做动画；
// 只有状态点用红色，重试走生成面板的同一条准入路径。
export function GenerationFailure({
  code,
  disabled,
  onRetry,
}: {
  code: string
  disabled: boolean
  onRetry: () => void
}) {
  const text = generationFailureText(code)
  return (
    <div className="cc-generation-failed" role="status" aria-label={text}>
      <p>
        <i aria-hidden="true" />
        {text}
      </p>
      <button
        type="button"
        className="nodrag"
        disabled={disabled}
        onPointerDown={(e) => e.stopPropagation()}
        onClick={(e) => {
          e.stopPropagation()
          onRetry()
        }}
      >
        <RotateCcw size={12} strokeWidth={2.2} />
        重试
      </button>
    </div>
  )
}
