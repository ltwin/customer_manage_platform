import { useEffect, useRef, useState } from 'react'
import type { components } from '../../api/schema'
import { ApiError } from '../../api/transport.ts'
import * as api from '../editor/api.ts'
import Picker from '../editor/Picker.tsx'
import StudioDialog from '../editor/StudioDialog.tsx'
import { groupOptions } from '../editor/libraryState.ts'
import { errorMessage } from '../editor/queue.ts'
import LibraryPicker from './LibraryPicker.tsx'
import { conflict, prepareOperation, rejected } from './operations.ts'
import { CharacterImage, CharacterIPFields } from './CharacterFields.tsx'
import { characterOf, characterViews, type Character } from './character.ts'
import type { LibraryCatalog } from './useLibrary.ts'

type Result = components['schemas']['CreativeAssetCreateResult']
type Draft = Omit<Character, 'aliases' | 'source_urls'> & {
  aliasesText: string
  sourceURLsText: string
}
type Target = components['schemas']['CreativeCharacterTarget']
type ImagePicker = 'views' | 'sources'
const asView = (id: string): Character['views'][number] => ({
  content_revision_id: id,
  view: 'other',
})
function draftOf(card?: Character, views: string[] = []): Draft {
  return {
    name: '',
    appearance: '',
    views: views.slice(0, 4).map(asView),
    sources: [],
    ...card,
    aliasesText: card?.aliases?.join('\n') ?? '',
    sourceURLsText: card?.source_urls?.join('\n') ?? '',
  }
}

export default function CharacterDialog({
  account,
  asset,
  views = [],
  group,
  catalog,
  onSaved,
  onClose,
}: {
  account: string
  asset?: api.Asset
  views?: string[]
  group?: string
  catalog?: Pick<LibraryCatalog, 'groups' | 'tags'> | null
  onSaved: (result: Result) => void
  onClose: () => void
}) {
  // One slot per card, or per set of images for a new card: the draft and an
  // unconfirmed save always belong to the same card.
  const slot = asset ? asset.id : `new:${views.join(',')}`
  const draftKey = `creative:character:${account}:${slot}`
  const [base, setBase] = useState(asset)
  const [initial, setInitial] = useState(() =>
    draftOf(characterOf(asset?.content), views),
  )
  const [draft, setDraft] = useState(
    () => readDraft(draftKey, asset?.content_revision_id) ?? initial,
  )
  useEffect(() => {
    // Only an edit is kept; it is tied to the card content it started from,
    // so a rating or a tag elsewhere never discards it.
    try {
      if (sameDraft(draft, initial)) window.localStorage.removeItem(draftKey)
      else
        window.localStorage.setItem(
          draftKey,
          JSON.stringify({ basis: base?.content_revision_id ?? '', draft }),
        )
    } catch {
      /* keep the draft in memory if storage is unavailable */
    }
  }, [draftKey, draft, initial, base])
  const [target, setTarget] = useState<Target>({
    group_ids: group ? [group] : [],
    tag_ids: [],
  })
  const [pick, setPick] = useState<ImagePicker | null>(null)
  const pendingKey = `creative:character-operation:${account}:${slot}`
  const [stored] = useState(() => readPending(pendingKey, asset?.id))
  const [phase, setPhase] = useState<
    'ready' | 'saving' | 'unknown' | 'conflict' | 'reading'
  >(stored ? 'unknown' : 'ready')
  const [error, setError] = useState(stored ? '有一次保存结果待确认' : '')
  const pending = useRef(stored)
  const running = useRef(false)
  const busy = phase === 'saving' || phase === 'reading'
  const locked = busy || phase === 'unknown'
  const update = <K extends keyof Draft>(key: K, value: Draft[K]) =>
    setDraft((current) => ({ ...current, [key]: value }))
  const close = () => {
    if (!busy && !running.current) onClose()
  }
  useEffect(() => {
    if (!busy) return
    const leave = (e: BeforeUnloadEvent) => e.preventDefault()
    window.addEventListener('beforeunload', leave)
    return () => window.removeEventListener('beforeunload', leave)
  }, [busy])

  function acceptImages(assets: api.Asset[], kind: ImagePicker) {
    const ids = [...new Set(assets.map((item) => item.content_revision_id))]
    setDraft((previous) =>
      kind === 'views'
        ? {
            ...previous,
            views: [...previous.views, ...ids.map(asView)].slice(0, 4),
          }
        : {
            ...previous,
            sources: [
              ...previous.sources,
              ...ids.map((id) => ({ content_revision_id: id })),
            ].slice(0, 12),
          },
    )
    setPick(null)
  }

  function moveView(index: number, delta: number) {
    const next = [...draft.views]
    const to = index + delta
    const view = next[index]
    const neighbor = next[to]
    if (view && neighbor) {
      next[to] = view
      next[index] = neighbor
      update('views', next)
    }
  }

  async function save() {
    if (running.current || phase === 'conflict') return
    const { aliasesText, sourceURLsText, ...fields } = draft
    if (!pending.current) {
      if (
        !fields.name.trim() ||
        !fields.appearance.trim() ||
        !fields.views.length
      ) {
        setError('请填写角色名和外貌描述，至少选一张参考图')
        return
      }
      const lines = (value: string) =>
        value
          .split('\n')
          .map((item) => item.trim())
          .filter(Boolean)
      const aliases = lines(aliasesText)
      const sourceURLs = lines(sourceURLsText)
      if (aliases.length > 10 || sourceURLs.length > 10) {
        setError('别名和来源链接各最多 10 项')
        return
      }
      const character: Character = {
        ...fields,
        ip: fields.ip?.name.trim() ? fields.ip : undefined,
        work: fields.ip?.name.trim() ? fields.work : undefined,
        aliases,
        source_urls: sourceURLs,
      }
      pending.current = base
        ? prepareOperation(
            `/assets/${encodeURIComponent(base.id)}/character-revisions`,
            {
              expected_revision: base.revision,
              character,
            } satisfies components['schemas']['SaveCreativeCharacterRevisionRequest']['payload'],
          )
        : prepareOperation('/assets/characters', {
            character,
            target,
          } satisfies components['schemas']['CreateCreativeCharacterRequest']['payload'])
      storePending(pendingKey, pending.current)
    }
    const request = pending.current
    setError('')
    setPhase('saving')
    running.current = true
    try {
      const result = await api.send<Result>(
        account,
        request.path,
        request.body,
        request.id,
      )
      try {
        window.localStorage.removeItem(draftKey)
      } catch {
        /* optional storage */
      }
      pending.current = null
      storePending(pendingKey, null)
      onSaved(result)
    } catch (e) {
      if (rejected(e)) {
        pending.current = null
        storePending(pendingKey, null)
      }
      if (conflict(e)) {
        setError(
          '这张角色卡已在别处更新。可以保留你的修改覆盖它，或放弃修改载入最新版',
        )
        setPhase('conflict')
      } else if (rejected(e)) {
        const ids =
          e instanceof ApiError &&
          e.details &&
          'content_revision_ids' in e.details
            ? e.details.content_revision_ids
            : []
        const badViews = draft.views.flatMap((view, i) =>
          ids.includes(view.content_revision_id) ? [`参考图 ${i + 1}`] : [],
        )
        const badSources = draft.sources.flatMap((source, i) =>
          ids.includes(source.content_revision_id) ? [`原始素材 ${i + 1}`] : [],
        )
        const unavailable = [...badViews, ...badSources]
        setError(
          unavailable.length
            ? `${unavailable.join('、')}不可用，请更换后保存`
            : errorMessage(e),
        )
        setPhase('ready')
      } else {
        setError('保存结果待确认，重试会继续查询同一次保存')
        setPhase('unknown')
      }
    } finally {
      running.current = false
    }
  }

  // keep rebases the edit onto the latest revision; otherwise it is dropped.
  // A change elsewhere that left the card content alone (a tag, a rating)
  // keeps the edit either way.
  async function loadLatest(keep: boolean) {
    if (!base || running.current) return
    setPhase('reading')
    running.current = true
    try {
      const fresh = await api.read<api.Asset>(
        `/assets/${encodeURIComponent(base.id)}`,
      )
      const card = characterOf(fresh.content)
      if (!card || fresh.unavailable || fresh.deleted_at)
        throw new Error('角色卡当前不可编辑')
      const contentChanged =
        fresh.content_revision_id !== base.content_revision_id
      const latest = draftOf(card)
      setBase(fresh)
      setInitial(latest)
      if (!keep && contentChanged) setDraft(latest)
      setPhase('ready')
      setError(
        contentChanged
          ? keep
            ? '已改为基于最新版本，保存会用你的修改覆盖它'
            : ''
          : '角色卡内容没有变化，可以继续保存',
      )
    } catch (e) {
      setError(errorMessage(e))
      setPhase('conflict')
    } finally {
      running.current = false
    }
  }

  return (
    <>
      <StudioDialog
        title={asset ? '编辑角色卡' : '新建角色卡'}
        onClose={close}
        closeDisabled={busy}
        overlayClassName="al-character-overlay"
      >
        <form
          className="cc-form al-character-form"
          noValidate
          onSubmit={(e) => {
            e.preventDefault()
            void save()
          }}
        >
          <fieldset disabled={locked}>
            <p className="cc-muted">仅限虚构角色。</p>
            <label>
              角色名
              <input
                className="input"
                value={draft.name}
                maxLength={60}
                data-autofocus
                onChange={(e) => update('name', e.target.value)}
              />
            </label>
            <label>
              外貌描述
              <textarea
                className="input"
                value={draft.appearance}
                maxLength={2000}
                onChange={(e) => update('appearance', e.target.value)}
              />
            </label>
            <CharacterIPFields
              ip={draft.ip ?? { name: '' }}
              work={draft.work ?? ''}
              disabled={locked}
              onChange={(ip, work) =>
                setDraft((current) => ({ ...current, ip, work }))
              }
            />
            <label>
              版本说明
              <input
                className="input"
                value={draft.variant ?? ''}
                maxLength={40}
                onChange={(e) => update('variant', e.target.value)}
              />
            </label>
          </fieldset>
          <fieldset disabled={locked}>
            <legend>参考图 · {draft.views.length}/4</legend>
            <p className="cc-muted">
              第一张作为封面，参考图按这里的顺序用于生成。
            </p>
            <ol className="al-character-list">
              {draft.views.map((view, i) => (
                <li key={view.content_revision_id}>
                  <CharacterImage
                    id={view.content_revision_id}
                    label={`参考图 ${i + 1}`}
                  />
                  <div>
                    <Picker
                      label={`参考图 ${i + 1} 的视角`}
                      variant="field"
                      disabled={locked}
                      options={[...characterViews]}
                      value={view.view}
                      onChange={(value) => {
                        const angle = characterViews.find(
                          (entry) => entry.value === value,
                        )
                        if (angle)
                          update(
                            'views',
                            draft.views.map((item, j) =>
                              j === i ? { ...item, view: angle.value } : item,
                            ),
                          )
                      }}
                    />
                    <input
                      className="input"
                      aria-label={`参考图 ${i + 1} 备注`}
                      placeholder="视角备注（选填）"
                      maxLength={200}
                      value={view.note ?? ''}
                      onChange={(e) =>
                        update(
                          'views',
                          draft.views.map((item, j) =>
                            j === i ? { ...item, note: e.target.value } : item,
                          ),
                        )
                      }
                    />
                  </div>
                  <div className="al-character-tools">
                    <button
                      type="button"
                      className="btn"
                      aria-label={`上移参考图 ${i + 1}`}
                      disabled={i === 0}
                      onClick={() => moveView(i, -1)}
                    >
                      ↑
                    </button>
                    <button
                      type="button"
                      className="btn"
                      aria-label={`下移参考图 ${i + 1}`}
                      disabled={i === draft.views.length - 1}
                      onClick={() => moveView(i, 1)}
                    >
                      ↓
                    </button>
                    <button
                      type="button"
                      className="btn"
                      aria-label={`移除参考图 ${i + 1}`}
                      onClick={() =>
                        update(
                          'views',
                          draft.views.filter((_, j) => i !== j),
                        )
                      }
                    >
                      移除
                    </button>
                  </div>
                </li>
              ))}
            </ol>
            <button
              type="button"
              className="btn"
              disabled={draft.views.length >= 4}
              onClick={() => setPick('views')}
            >
              添加参考图
            </button>
          </fieldset>
          <fieldset disabled={locked}>
            <legend>原始素材 · {draft.sources.length}/12</legend>
            <ol className="al-character-list">
              {draft.sources.map((source, i) => (
                <li key={source.content_revision_id}>
                  <CharacterImage
                    id={source.content_revision_id}
                    label={`原始素材 ${i + 1}`}
                  />
                  <span>原始素材 {i + 1}</span>
                  <button
                    type="button"
                    className="btn"
                    aria-label={`移除原始素材 ${i + 1}`}
                    onClick={() =>
                      update(
                        'sources',
                        draft.sources.filter((_, j) => i !== j),
                      )
                    }
                  >
                    移除
                  </button>
                </li>
              ))}
            </ol>
            <button
              type="button"
              className="btn"
              disabled={draft.sources.length >= 12}
              onClick={() => setPick('sources')}
            >
              添加原始素材
            </button>
          </fieldset>
          <fieldset disabled={locked}>
            <legend>补充信息</legend>
            <label>
              别名（每行一个）
              <textarea
                className="input"
                value={draft.aliasesText}
                maxLength={610}
                onChange={(e) => update('aliasesText', e.target.value)}
              />
            </label>
            <Picker
              label="来源类别"
              variant="field"
              disabled={locked}
              value={draft.origin ?? ''}
              options={[
                { value: '', label: '未说明' },
                { value: 'original', label: '原创' },
                { value: 'derivative', label: '作品衍生' },
              ]}
              onChange={(value) =>
                update(
                  'origin',
                  value === 'original' || value === 'derivative'
                    ? value
                    : undefined,
                )
              }
            />
            <label>
              来源备注
              <textarea
                className="input"
                value={draft.attribution ?? ''}
                maxLength={2000}
                onChange={(e) => update('attribution', e.target.value)}
              />
            </label>
            <label>
              来源链接（每行一个）
              <textarea
                className="input"
                value={draft.sourceURLsText}
                maxLength={20490}
                onChange={(e) => update('sourceURLsText', e.target.value)}
              />
            </label>
          </fieldset>
          {!asset && (
            <fieldset disabled={locked}>
              <legend>归类</legend>
              <Picker
                label="归入分组"
                variant="field"
                disabled={locked}
                options={groupOptions(catalog?.groups ?? [])}
                values={target.group_ids ?? []}
                onToggle={(id) =>
                  setTarget((current) => ({
                    ...current,
                    group_ids: toggle(current.group_ids ?? [], id),
                  }))
                }
              />
              <Picker
                label="打标签"
                variant="field"
                disabled={locked}
                options={(catalog?.tags ?? []).map((tag) => ({
                  label: tag.name,
                  value: tag.id,
                }))}
                values={target.tag_ids ?? []}
                onToggle={(id) =>
                  setTarget((current) => ({
                    ...current,
                    tag_ids: toggle(current.tag_ids ?? [], id),
                  }))
                }
              />
            </fieldset>
          )}
          <p className="al-character-error" role="status" aria-live="polite">
            {error}
          </p>
          <footer>
            <button
              type="button"
              className="btn"
              disabled={busy}
              onClick={close}
            >
              关闭
            </button>
            {phase === 'conflict' ? (
              <>
                <button
                  type="button"
                  className="btn"
                  onClick={() => void loadLatest(false)}
                >
                  放弃修改，载入最新版
                </button>
                <button
                  type="button"
                  className="btn btn-primary"
                  onClick={() => void loadLatest(true)}
                >
                  保留我的修改
                </button>
              </>
            ) : (
              <button type="submit" className="btn btn-primary" disabled={busy}>
                {phase === 'saving'
                  ? '正在保存…'
                  : phase === 'unknown'
                    ? '确认保存结果'
                    : asset
                      ? '存为新版本'
                      : '创建角色卡'}
              </button>
            )}
          </footer>
        </form>
      </StudioDialog>
      {pick && (
        <LibraryPicker
          kind="image"
          title={pick === 'views' ? '选择参考图' : '选择原始素材'}
          note="从资产库选择图片"
          confirm="添加"
          max={(pick === 'views' ? 4 : 12) - draft[pick].length}
          accept={(item) =>
            item.kind === 'image' &&
            !draft[pick].some(
              (existing) =>
                existing.content_revision_id === item.content_revision_id,
            )
          }
          onPick={(items) => acceptImages(items, pick)}
          onClose={() => setPick(null)}
        />
      )}
    </>
  )
}

function readDraft(key: string, basis?: string): Draft | null {
  try {
    const raw = window.localStorage.getItem(key)
    if (!raw) return null
    const saved = JSON.parse(raw) as { basis?: unknown; draft?: Partial<Draft> }
    const value = saved?.draft
    if (
      saved?.basis === (basis ?? '') &&
      typeof value?.name === 'string' &&
      typeof value.appearance === 'string' &&
      typeof value.aliasesText === 'string' &&
      typeof value.sourceURLsText === 'string' &&
      Array.isArray(value.views) &&
      Array.isArray(value.sources)
    )
      return { ...draftOf(), ...value } as Draft
  } catch {
    /* private storage is optional */
  }
  return null
}

function sameDraft(a: Draft, b: Draft): boolean {
  return JSON.stringify(a) === JSON.stringify(b)
}

function toggle(ids: string[], id: string): string[] {
  return ids.includes(id) ? ids.filter((value) => value !== id) : [...ids, id]
}

type PendingCharacter = ReturnType<typeof prepareOperation>
function readPending(key: string, assetID?: string): PendingCharacter | null {
  try {
    const raw = window.localStorage.getItem(key)
    if (!raw) return null
    const value = JSON.parse(raw) as Partial<PendingCharacter>
    const path = assetID
      ? `/assets/${encodeURIComponent(assetID)}/character-revisions`
      : '/assets/characters'
    if (
      value?.path === path &&
      typeof value.id === 'string' &&
      typeof value.body === 'string'
    )
      return { path, id: value.id, body: value.body }
  } catch {
    /* private storage is optional */
  }
  return null
}
function storePending(key: string, value: PendingCharacter | null) {
  try {
    if (value) window.localStorage.setItem(key, JSON.stringify(value))
    else window.localStorage.removeItem(key)
  } catch {
    /* the current dialog still retains the request for retry */
  }
}
