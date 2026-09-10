import { useState } from 'react'
import type { Group, Tag, Category } from './api.ts'
import type { Organization } from './libraryState.ts'
export default function OrganizationPicker({
  groups,
  tags,
  categories,
  value,
  onChange,
  disabled,
  allowNew = true,
}: {
  groups: Group[]
  tags: Tag[]
  categories: Category[]
  value: Organization
  onChange: (value: Organization) => void
  disabled: boolean
  allowNew?: boolean
}) {
  const [search, setSearch] = useState('')
  const [color, setColor] = useState('#BDCEC9')
  const [category, setCategory] = useState('')
  const [composing, setComposing] = useState(false)
  const norm = (s: string) => s.normalize('NFKC').trim().toLowerCase()
  const toggle = (key: 'groupIDs' | 'tagIDs', id: string) =>
    onChange({
      ...value,
      [key]: value[key].includes(id)
        ? value[key].filter((x) => x !== id)
        : [...value[key], id],
    })
  return (
    <fieldset className="cl-picker" disabled={disabled}>
      <legend>分组与标签</legend>
      <details>
        <summary>选择分组 · {value.groupIDs.length}</summary>
        <div className="cl-options">
          {groups.map((g) => (
            <label key={g.id}>
              <input
                type="checkbox"
                checked={value.groupIDs.includes(g.id)}
                onChange={() => toggle('groupIDs', g.id)}
              />
              {g.name}
            </label>
          ))}
          {!groups.length && <p>尚未创建分组</p>}
        </div>
      </details>
      <label>
        查找标签
        <div className="cl-search">
          <input
            className="input"
            aria-label="查找标签"
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            onCompositionStart={() => setComposing(true)}
            onCompositionEnd={() => setComposing(false)}
          />
          {search && (
            <button
              type="button"
              aria-label="清除标签查找"
              onClick={() => setSearch('')}
            >
              ×
            </button>
          )}
        </div>
      </label>
      <div className="cl-tags">
        {tags
          .filter(
            (t) =>
              norm(t.name).includes(norm(search)) ||
              value.tagIDs.includes(t.id),
          )
          .map((t) => (
            <button
              type="button"
              key={t.id}
              aria-pressed={value.tagIDs.includes(t.id)}
              onClick={() => toggle('tagIDs', t.id)}
            >
              <i style={{ background: t.color }} />
              {t.name}
            </button>
          ))}
      </div>
      {value.newTags.map((t) => (
        <button
          className="cl-draft-tag"
          type="button"
          key={t.client_tag_key}
          aria-label={`移除待创建标签：${t.name}`}
          onClick={() =>
            onChange({
              ...value,
              newTags: value.newTags.filter(
                (x) => x.client_tag_key !== t.client_tag_key,
              ),
            })
          }
        >
          {t.name} ×
        </button>
      ))}
      {allowNew &&
        search.trim() &&
        !tags.some((t) => norm(t.name) === norm(search)) &&
        !value.newTags.some((t) => norm(t.name) === norm(search)) && (
          <div className="cl-new-tag">
            <label>
              新标签颜色
              <input
                type="color"
                value={color}
                onChange={(e) => setColor(e.target.value)}
              />
            </label>
            <label>
              新标签分类
              <select
                className="input"
                aria-label="新标签分类"
                value={category}
                onChange={(e) => setCategory(e.target.value)}
              >
                <option value="">未分类</option>
                {categories.map((c) => (
                  <option value={c.id} key={c.id}>
                    {c.name}
                  </option>
                ))}
              </select>
            </label>
            <button
              className="btn"
              type="button"
              disabled={composing || search.length > 80}
              onClick={() => {
                onChange({
                  ...value,
                  newTags: [
                    ...value.newTags,
                    {
                      client_tag_key: crypto.randomUUID(),
                      name: search.trim(),
                      color,
                      category_id: category || null,
                    },
                  ],
                })
                setSearch('')
              }}
            >
              新建标签「{search.trim()}」
            </button>
            <small>随资产一起保存，取消时不会创建。</small>
          </div>
        )}
    </fieldset>
  )
}
