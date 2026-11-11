import { useState } from 'react'
import type { components } from '../../api/schema'
import { CharacterImage } from '../assets/CharacterFields.tsx'
import {
  characterOf,
  characterViewLabel,
  type Character,
} from '../assets/character.ts'
import * as api from './api.ts'

export default function CharacterProvenance({
  card,
}: {
  card: components['schemas']['CreativeGenerationCharacter']
}) {
  const [source, setSource] = useState<Character>()
  const [loading, setLoading] = useState(false)
  const [failed, setFailed] = useState(false)
  const [from, to] = card.images
  async function load() {
    if (source || loading) return
    setFailed(false)
    setLoading(true)
    try {
      const revision = await api.read<api.Content>(
        `/content-revisions/${encodeURIComponent(card.content_revision_id)}`,
      )
      const payload = characterOf(revision)
      if (!payload) throw new Error('角色卡版本不可用')
      setSource(payload)
    } catch {
      setFailed(true)
    } finally {
      setLoading(false)
    }
  }

  return (
    <details
      className="cc-character-provenance"
      onToggle={(event) => {
        if (event.currentTarget.open && !failed) void load()
      }}
    >
      <summary>
        {card.name} · {from === to ? `图${from}` : `图${from}–图${to}`}
      </summary>
      {loading && <p role="status">正在读取角色卡版本…</p>}
      {failed && (
        <button type="button" onClick={() => void load()}>
          角色卡版本读取失败，重试
        </button>
      )}
      {source && (
        <>
          {/* Identity, not the internal revision ID, tells same-name cards apart. */}
          {(source.ip || source.work || source.variant) && (
            <p>
              {[source.ip?.name, source.work, source.variant]
                .filter(Boolean)
                .join(' · ')}
            </p>
          )}
          <p>{source.appearance}</p>
          <div className="cc-character-provenance-images">
            {source.views.map((view) => (
              <CharacterImage
                key={view.content_revision_id}
                id={view.content_revision_id}
                label={characterViewLabel(view.view)}
              />
            ))}
          </div>
        </>
      )}
    </details>
  )
}
