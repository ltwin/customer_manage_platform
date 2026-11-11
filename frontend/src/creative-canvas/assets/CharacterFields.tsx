import { useEffect, useState, type KeyboardEvent } from 'react'
import type { components } from '../../api/schema'
import * as api from '../editor/api.ts'
import { MediaFigure, useNearViewport } from '../editor/MediaView.tsx'
import Picker from '../editor/Picker.tsx'
import type { Character } from './character.ts'
import './character.css'

type IPSuggestion =
  components['schemas']['CreativeCharacterIPSuggestions']['items'][number]

export function CharacterImage({
  id,
  label,
  className = '',
}: {
  id: string
  label: string
  className?: string
}) {
  const { ref, near } = useNearViewport<HTMLDivElement>()
  const [loaded, setLoaded] = useState<{
    id: string
    content?: api.Content
    failed?: boolean
  }>()
  const [attempt, setAttempt] = useState(0)
  useEffect(() => {
    if (!near) return
    const abort = new AbortController()
    void api
      .read<api.Content>(
        `/content-revisions/${encodeURIComponent(id)}`,
        abort.signal,
      )
      .then((content) => {
        if (!abort.signal.aborted) setLoaded({ id, content })
      })
      .catch(() => {
        if (!abort.signal.aborted) setLoaded({ id, failed: true })
      })
    return () => abort.abort()
  }, [id, near, attempt])
  const current = loaded?.id === id ? loaded : undefined
  return (
    <div className={`al-character-image ${className}`} ref={ref}>
      {current?.failed ? (
        <button
          className="btn"
          type="button"
          onClick={(e) => {
            e.stopPropagation()
            setLoaded(undefined)
            setAttempt((n) => n + 1)
          }}
        >
          图片不可用，重试
        </button>
      ) : (
        <MediaFigure kind="image" title={label} content={current?.content} />
      )}
    </div>
  )
}

// Enter in a suggestion field confirms the typing, not the whole form.
function keepOpen(e: KeyboardEvent<HTMLInputElement>) {
  if (e.key === 'Enter' && !e.nativeEvent.isComposing) e.preventDefault()
}

export function CharacterIPFields({
  ip,
  work,
  onChange,
  disabled = false,
  filter = false,
}: {
  ip: NonNullable<Character['ip']>
  work: string
  onChange: (ip: NonNullable<Character['ip']>, work: string) => void
  disabled?: boolean
  filter?: boolean
}) {
  const [hints, setHints] = useState<{
    query: string
    items: IPSuggestion[]
    error?: boolean
  }>()
  const [composing, setComposing] = useState(false)
  const query = ip.name.trim()
  useEffect(() => {
    if (composing) return
    const abort = new AbortController()
    const timer = window.setTimeout(() => {
      const params = new URLSearchParams({ query, limit: '20' })
      void api
        .read<components['schemas']['CreativeCharacterIPSuggestions']>(
          `/character-ips?${params}`,
          abort.signal,
        )
        .then(({ items }) => {
          if (!abort.signal.aborted) setHints({ query, items })
        })
        .catch(() => {
          if (!abort.signal.aborted) setHints({ query, items: [], error: true })
        })
    }, 300)
    return () => {
      window.clearTimeout(timer)
      abort.abort()
    }
  }, [query, composing])
  const items = hints?.query === query ? hints.items : []
  const selected = items.find((item) =>
    ip.key ? item.key === ip.key : item.name === ip.name,
  )
  return (
    <div className="al-character-ip">
      <label>
        所属 IP
        <input
          className="input"
          value={ip.name}
          maxLength={60}
          disabled={disabled}
          onCompositionStart={() => setComposing(true)}
          onCompositionEnd={() => setComposing(false)}
          onKeyDown={keepOpen}
          // A card's work belongs to its IP: retyping the IP clears it, as
          // picking a candidate does. A filter keeps a work typed on its own.
          onChange={(e) => onChange({ name: e.target.value }, filter ? work : '')}
        />
      </label>
      <Picker
        label="选择所属 IP"
        placeholder="IP 候选"
        disabled={disabled}
        value={selected?.key ?? selected?.name ?? ''}
        empty={
          hints?.error
            ? '暂时读不到候选，可直接输入名称'
            : '暂无匹配，可直接输入名称'
        }
        options={items.map((item) => ({
          value: item.key ?? item.name,
          label: item.name,
          meta: item.character_count ? `${item.character_count} 张` : undefined,
        }))}
        onChange={(value) => {
          const match = items.find((item) => (item.key ?? item.name) === value)
          if (match) onChange({ name: match.name, key: match.key }, '')
        }}
      />
      <label>
        作品
        <input
          className="input"
          value={work}
          maxLength={100}
          disabled={disabled || (!filter && !query)}
          onKeyDown={keepOpen}
          onChange={(e) => onChange(ip, e.target.value)}
        />
      </label>
      {!!selected?.works.length && (
        <Picker
          label="选择作品"
          placeholder="作品候选"
          value={work}
          disabled={disabled}
          options={selected.works.map((name) => ({ label: name, value: name }))}
          onChange={(name) => onChange(ip, name)}
        />
      )}
    </div>
  )
}
