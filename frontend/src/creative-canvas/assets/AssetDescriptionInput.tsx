import { useId, useState } from 'react'

export const ASSET_DESCRIPTION_LIMIT = 1000
export function assetDescriptionError(value: string): string | null {
  const count = Array.from(value).length
  return count > ASSET_DESCRIPTION_LIMIT
    ? `描述最多 ${ASSET_DESCRIPTION_LIMIT} 字，当前 ${count} 字；请缩短后保存`
    : null
}

/** 保留旧长描述和未通过校验的输入，仅保存明确修改且符合限制的值。 */
export default function AssetDescriptionInput({
  description,
  disabled,
  onSave,
}: {
  description: string
  disabled: boolean
  onSave: (description: string) => void
}) {
  const [value, setValue] = useState(description)
  const hintId = useId()
  const error = value !== description ? assetDescriptionError(value) : null
  return (
    <>
      <textarea
        className="al-ins-note-input"
        aria-label="备注"
        aria-describedby={hintId}
        aria-invalid={!!error}
        value={value}
        placeholder="为什么收藏它、准备用在哪…"
        disabled={disabled}
        onChange={(event) => setValue(event.currentTarget.value)}
        onBlur={() => {
          if (value !== description && !error) onSave(value)
        }}
        onKeyDown={(event) => {
          if (event.key === 'Escape') {
            event.stopPropagation()
            setValue(description)
          }
        }}
      />
      <small id={hintId} role={error ? 'alert' : undefined}>
        {error ?? `${Array.from(value).length} 字 · 修改后最多 1000 字`}
      </small>
    </>
  )
}
