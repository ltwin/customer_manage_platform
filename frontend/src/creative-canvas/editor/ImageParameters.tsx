import {
  imageParameterKeys,
  optionLabel,
  parameterValue,
  selectedOption,
  type GenerationParameter,
} from './generation.ts'

export function RatioGlyph({
  ratio,
  size = 16,
}: {
  ratio: string
  size?: number
}) {
  const [w = 1, h = 1] = ratio.split(':').map(Number)
  const r = w > 0 && h > 0 ? w / h : 1
  const width = ratio === 'auto' ? 10 : r >= 1 ? 14 : Math.max(5, 14 * r)
  const height = ratio === 'auto' ? 10 : r >= 1 ? Math.max(5, 14 / r) : 14
  return (
    <svg width={size} height={size} viewBox="0 0 16 16" aria-hidden="true">
      <rect
        x={(16 - width) / 2 + 0.5}
        y={(16 - height) / 2 + 0.5}
        width={width - 1}
        height={height - 1}
        rx="1.5"
        fill="none"
        stroke="currentColor"
        strokeDasharray={ratio === 'auto' ? '2 1.5' : undefined}
      />
    </svg>
  )
}

type Props = {
  fields: GenerationParameter[]
  values: Record<string, unknown>
  disabled: boolean
  onChange: (key: string, value: string | number | boolean) => void
  onError: (message: string) => void
}

/** GPT Image 一类模型的分节参数面板：画质、清晰度、背景、比例、数量。 */
export default function ImageParameters({
  fields,
  values,
  disabled,
  onChange,
  onError,
}: Props) {
  const ratio = fields.find((f) => f.key === 'aspect_ratio')
  const autoRatio = !!ratio && selectedOption(ratio, values) === 'auto'
  return (
    <div className="cc-image-params">
      {imageParameterKeys.map((key) => {
        const field = fields.find((f) => f.key === key)
        if (!field) return null
        const current = selectedOption(field, values)
        const muted = key === 'resolution' && autoRatio
        return (
          <fieldset key={key} disabled={disabled || muted}>
            <legend>
              {field.label}
              {muted && <small>自动比例由模型决定尺寸</small>}
            </legend>
            <div
              className={key === 'aspect_ratio' ? 'cc-ratio-grid' : 'cc-segment'}
              role="radiogroup"
              aria-label={field.label}
            >
              {field.options.map((option) => (
                <button
                  key={option}
                  type="button"
                  role="radio"
                  aria-checked={current === option}
                  className={current === option ? 'is-active' : ''}
                  onClick={() => {
                    try {
                      const v = parameterValue(field, option)
                      if (v !== undefined) onChange(key, v)
                    } catch (e) {
                      onError(e instanceof Error ? e.message : String(e))
                    }
                  }}
                >
                  {key === 'aspect_ratio' && (
                    <RatioGlyph ratio={option} size={20} />
                  )}
                  <span>{optionLabel(key, option)}</span>
                </button>
              ))}
            </div>
          </fieldset>
        )
      })}
    </div>
  )
}
