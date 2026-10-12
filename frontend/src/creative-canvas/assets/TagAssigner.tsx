import { useState } from 'react'
import { Plus } from 'lucide-react'
import type { Asset } from '../editor/api.ts'
import type { LibraryCatalog } from './useLibrary.ts'
import { tagSections } from './query.ts'
/** 与原型一致：按分类的标签 chip、搜索或就地新建，点选后保留弹层。 */
export default function TagAssigner({
  assets,
  catalog,
  blocked,
  onToggle,
  onCreate,
}: {
  assets: Asset[]
  catalog: LibraryCatalog | null
  blocked: boolean
  onToggle: (id: string, add: boolean) => void
  onCreate: (name: string, done: () => void) => void
}) {
  const [q, setQ] = useState('')
  const name = q.trim()
  const tags = catalog?.tags ?? []
  const exists = tags.some(
    (t) =>
      t.name.normalize('NFKC').toLowerCase() ===
      name.normalize('NFKC').toLowerCase(),
  )
  return (
    <div className="al-tag-assign">
      <p>给 {assets.length} 项素材打标签</p>
      <input
        aria-label="搜索或新建标签"
        placeholder="搜索或新建标签"
        value={q}
        maxLength={80}
        onChange={(e) => setQ(e.currentTarget.value)}
      />
      {tagSections(
        tags.filter((t) => t.name.toLowerCase().includes(name.toLowerCase())),
        catalog?.categories ?? [],
      ).map((section) => (
        <section key={section.id ?? 'none'}>
          <div className="al-tag-cat">{section.name}</div>
          <div className="al-chips">
            {section.tags.map((t) => {
              const all = assets.every((a) => a.tag_ids.includes(t.id))
              return (
                <button
                  type="button"
                  className="al-tag-choice"
                  key={t.id}
                  disabled={blocked}
                  aria-pressed={all}
                  onClick={() => onToggle(t.id, !all)}
                >
                  <i className="al-tag-dot" style={{ background: t.color }} />
                  {t.name}
                </button>
              )
            })}
          </div>
        </section>
      ))}
      {name && !exists && (
        <button
          type="button"
          className="al-tag-choice"
          disabled={blocked}
          onClick={() => onCreate(name, () => setQ(''))}
        >
          <Plus size={12} />
          新建标签「{name}」
        </button>
      )}
    </div>
  )
}
