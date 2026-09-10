import { createPortal } from 'react-dom'
import StudioDialog from './StudioDialog.tsx'
import { useEffect, useRef, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import {
  Folder,
  Search,
  Maximize,
  Minimize,
  ChevronDown,
  Ellipsis,
  Eye,
  Type,
  Link2,
  Heart,
  Inbox,
  Trash2,
  Plus,
  SlidersHorizontal,
  PanelLeftClose,
  X,
  Pencil,
  ArrowRight,
  Tag as TagIcon,
} from 'lucide-react'
import ConfirmDialog from '../../components/ConfirmDialog'
import * as api from './api.ts'
import { errorMessage } from './queue.ts'
import { emptyCatalog, type Catalog, type Editor } from './libraryState.ts'
import LibraryEditor from './LibraryEditor.tsx'
import './library.css'
type Confirmation = {
  title: string
  body: string
  path: string
  payload: unknown
  label: string
}
const initialPage = (): api.AssetPage => ({
  items: [],
  next_cursor: '',
  total_count: 0,
  library_revision: '1',
})
export default function LibraryPanel({
  disabled,
  busy,
  hidden,
  standalone,
  tick,
  onCreate,
  onClose,
  onWidth,
  onAssets,
  onLoading,
  onCatalog,
  onCommand,
  canDrop,
  onDrop,
}: {
  disabled: boolean
  busy: boolean
  hidden: boolean
  standalone: boolean
  tick: number
  onCreate: () => void
  onClose: () => void
  onWidth: (width: number) => void
  onAssets: (page: api.AssetPage) => void
  onLoading: (loading: boolean) => void
  onCatalog: (catalog: Catalog) => void
  onCommand: (path: string, payload: unknown) => Promise<boolean>
  canDrop: boolean
  onDrop: (asset: api.Asset) => void
}) {
  const [params, setParams] = useSearchParams()
  const [catalog, setCatalog] = useState(emptyCatalog)
  const [page, setPage] = useState(initialPage)
  const [loading, setLoading] = useState(true)
  const [catalogLoading, setCatalogLoading] = useState(true)
  const [moreLoading, setMoreLoading] = useState(false)
  const [error, setError] = useState('')
  const [stale, setStale] = useState(false)
  const [retry, setRetry] = useState(0)
  const [selection, setSelection] = useState<string[]>([])
  const [manager, setManager] = useState(false)
  const [width, setWidth] = useState(318)
  const [groupsOpen, setGroupsOpen] = useState(false)
  const [filtersOpen, setFiltersOpen] = useState(false)
  const [preview, setPreview] = useState<api.Asset | null>(null)
  const [previewResult, setPreviewResult] = useState<{
    key: string
    content?: api.Content
    error?: string
  } | null>(null)
  const previewKey = preview
    ? `${preview.id}:${preview.content_revision_id}`
    : ''
  const previewContent =
    previewResult?.key === previewKey ? previewResult.content : undefined
  const previewError =
    previewResult?.key === previewKey ? previewResult.error : undefined
  const resizeStart = useRef<{ x: number; width: number } | null>(null)
  useEffect(() => onWidth(width), [width, onWidth])
  const expanded = standalone || width >= 500
  useEffect(() => {
    setPreviewResult(null)
    if (!preview || preview.unavailable) return
    const controller = new AbortController()
    void api
      .read<api.Content>(
        `/content-revisions/${preview.content_revision_id}`,
        controller.signal,
      )
      .then((value) => {
        if (!controller.signal.aborted)
          setPreviewResult({ key: previewKey, content: value })
      })
      .catch((e: unknown) => {
        if (!controller.signal.aborted)
          setPreviewResult({ key: previewKey, error: errorMessage(e) })
      })
    return () => controller.abort()
  }, [preview, previewKey])
  const [editor, setEditor] = useState<Editor | null>(null)
  const [confirmation, setConfirmation] = useState<Confirmation | null>(null)
  const generation = useRef(0)
  const queryInput = useRef<HTMLInputElement>(null)
  const view = params.get('lview') ?? 'all',
    group = params.get('lgroup') ?? '',
    keyword = params.get('lq') ?? ''
  const [queryDraft, setQueryDraft] = useState(keyword)
  const [composing, setComposing] = useState(false)
  const [includeDescendants, setIncludeDescendants] = useState(
    params.get('ldesc') === 'true',
  )
  useEffect(
    () => setIncludeDescendants(params.get('ldesc') === 'true'),
    [params],
  )
  const tagIDs = (params.get('ltags') ?? '').split(',').filter(Boolean)
  function filter(key: string, value: string) {
    const next = new URLSearchParams(params)
    if (value) next.set(key, value)
    else next.delete(key)
    if (next.toString() !== params.toString()) {
      setLoading(true)
      onLoading(true)
      setParams(next, { replace: true })
    }
  }
  useEffect(() => {
    setQueryDraft(keyword)
  }, [keyword])
  useEffect(() => {
    if (composing || queryDraft === keyword) return
    const timer = setTimeout(() => {
      const next = new URLSearchParams(params)
      if (queryDraft.trim()) next.set('lq', queryDraft.trim())
      else next.delete('lq')
      if (next.toString() !== params.toString()) {
        setLoading(true)
        onLoading(true)
        setParams(next, { replace: true })
      }
    }, 300)
    return () => clearTimeout(timer)
  }, [queryDraft, keyword, composing, params, setParams, onLoading])
  const query = new URLSearchParams({
    view,
    limit: '30',
    sort: params.get('lsort') ?? 'recent',
    tag_mode: params.get('lmode') ?? 'all',
  })
  if (group) {
    query.set('group_id', group)
    query.set(
      'include_descendants',
      params.get('ldesc') === 'true' ? 'true' : 'false',
    )
  }
  if (keyword) query.set('q', keyword)
  if (tagIDs.length) query.set('tag_ids', tagIDs.join(','))
  if (params.get('lkind')) query.set('kind', params.get('lkind')!)
  const queryKey = query.toString()
  useEffect(() => {
    const controller = new AbortController()
    setCatalogLoading(true)
    void Promise.all([
      api.read<api.GroupPage>('/asset-groups', controller.signal),
      api.read<api.TagPage>('/tags', controller.signal),
      api.read<api.CategoryPage>('/tag-categories', controller.signal),
      api.read<api.LibrarySettings>('/library-settings', controller.signal),
    ])
      .then(([groups, tags, categories, settings]) => {
        if (controller.signal.aborted) return
        if (
          [
            groups.hierarchy_revision,
            tags.hierarchy_revision,
            categories.hierarchy_revision,
          ].some((revision) => revision !== settings.hierarchy_revision)
        ) {
          setError('目录已变化，请刷新列表后再整理。')
          return
        }
        const next = {
          groups: groups.items,
          tags: tags.items,
          categories: categories.items,
          settings,
          hierarchy: settings.hierarchy_revision,
        }
        setCatalog(next)
        onCatalog(next)
      })
      .catch((e: unknown) => {
        if (!controller.signal.aborted) setError(errorMessage(e))
      })
      .finally(() => {
        if (!controller.signal.aborted) setCatalogLoading(false)
      })
    return () => controller.abort()
  }, [tick, retry, onCatalog])
  useEffect(() => {
    onLoading(loading || catalogLoading)
  }, [loading, catalogLoading, onLoading])
  useEffect(() => {
    const controller = new AbortController()
    const current = ++generation.current
    setLoading(true)
    setMoreLoading(false)
    setStale(false)
    setSelection([])
    setError('')
    setPage(initialPage())
    onAssets(initialPage())
    void api
      .read<api.AssetPage>(`/assets?${queryKey}`, controller.signal)
      .then((result) => {
        if (current === generation.current && !controller.signal.aborted) {
          setPage(result)
          onAssets(result)
        }
      })
      .catch((e: unknown) => {
        if (!controller.signal.aborted) setError(errorMessage(e))
      })
      .finally(() => {
        if (!controller.signal.aborted) {
          setLoading(false)
        }
      })
    return () => controller.abort()
  }, [queryKey, tick, retry, onAssets, onLoading])
  async function more() {
    if (loading || moreLoading || !page.next_cursor) return
    const current = generation.current
    setMoreLoading(true)
    try {
      const result = await api.read<api.AssetPage>(
        `/assets?${queryKey}&cursor=${encodeURIComponent(page.next_cursor)}`,
      )
      if (current !== generation.current) return
      if (result.library_revision !== page.library_revision) {
        setStale(true)
        return
      }
      const next = {
        ...result,
        items: [
          ...page.items,
          ...result.items.filter(
            (a) => !page.items.some((old) => old.id === a.id),
          ),
        ],
      }
      setPage(next)
      onAssets(next)
    } catch (e) {
      if (current === generation.current) setError(errorMessage(e))
    } finally {
      if (current === generation.current) setMoreLoading(false)
    }
  }
  function edit(kind: Editor['kind'], value: Partial<Editor> = {}) {
    setManager(false)
    setEditor({ kind, hierarchy: catalog.hierarchy, ...value })
  }
  function confirm(
    path: string,
    payload: unknown,
    title: string,
    body: string,
    label: string,
  ) {
    setConfirmation({ path, payload, title, body, label })
  }
  const selected = page.items.filter((a) => selection.includes(a.id))
  function batch(action: 'trash' | 'restore' | 'purge') {
    confirm(
      `/assets/batch-${action}`,
      {
        assets: selected.map((a) => ({
          asset_id: a.id,
          expected_revision: a.revision,
        })),
      },
      action === 'purge'
        ? '彻底删除所选资产？'
        : action === 'trash'
          ? '移入回收站？'
          : '恢复所选资产？',
      action === 'purge'
        ? '个人库中的条目无法恢复。画布中已经引用的内容会保留。'
        : `${selected.length} 项资产；分组与最爱关系会保留。`,
      action === 'purge'
        ? '彻底删除'
        : action === 'trash'
          ? '移入回收站'
          : '恢复资产',
    )
  }
  function tree(
    parent: string | null,
    seen = new Set<string>(),
  ): React.ReactNode {
    return catalog.groups
      .filter((g) => g.parent_id === parent && !seen.has(g.id))
      .map((g) => (
        <div className="cl-group" key={g.id}>
          <div>
            <button
              className={group === g.id ? 'active' : ''}
              onClick={() => {
                const next = new URLSearchParams(params)
                next.set('lgroup', g.id)
                if (view === 'unclassified') next.set('lview', 'all')
                if (next.toString() !== params.toString()) {
                  setLoading(true)
                  onLoading(true)
                  setParams(next, { replace: true })
                }
              }}
            >
              <Folder size={13} />
              {g.name}
            </button>
            <details className="cl-group-menu">
              <summary aria-label={`分组操作：${g.name}`}>
                <Ellipsis size={14} />
              </summary>
              <div className="cl-row-menu cc-glass">
                <button
                  aria-label={`重命名分组：${g.name}`}
                  disabled={disabled}
                  onClick={() =>
                    edit('group', {
                      id: g.id,
                      name: g.name,
                      revision: g.revision,
                    })
                  }
                >
                  <Pencil size={12} />
                </button>
                <button
                  aria-label={`移动分组：${g.name}`}
                  disabled={disabled}
                  onClick={() =>
                    edit('move', {
                      id: g.id,
                      name: g.name,
                      parent: g.parent_id ?? '',
                      position: g.position,
                      revision: g.revision,
                    })
                  }
                >
                  <ArrowRight size={12} />
                </button>
                <button
                  aria-label={`删除分组：${g.name}`}
                  disabled={disabled}
                  onClick={() =>
                    confirm(
                      `/asset-groups/${g.id}/delete`,
                      {
                        expected_revision: g.revision,
                        hierarchy_revision: catalog.hierarchy,
                      },
                      `删除分组「${g.name}」？`,
                      '资产会保留，直接子组将提升到上一级。',
                      '删除分组',
                    )
                  }
                >
                  <X size={12} />
                </button>
              </div>
            </details>
          </div>
          <div className="cl-group-children">
            {tree(g.id, new Set([...seen, g.id]))}
          </div>
        </div>
      ))
  }
  const currentScope = group
    ? (catalog.groups.find((g) => g.id === group)?.name ?? '分组已删除')
    : ({
        all: '全部资产',
        favorites: '我的最爱',
        unclassified: '未归类',
        trash: '回收站',
      }[view] ?? '全部资产')
  function selectView(key: string) {
    const next = new URLSearchParams(params)
    next.set('lview', key)
    next.delete('lgroup')
    if (next.toString() !== params.toString()) {
      setLoading(true)
      onLoading(true)
      setParams(next, { replace: true })
    }
    setGroupsOpen(false)
  }
  function selectAsset(a: api.Asset, multi: boolean) {
    setSelection((old) =>
      multi
        ? old.includes(a.id)
          ? old.filter((id) => id !== a.id)
          : [...old, a.id].slice(0, 100)
        : [a.id],
    )
  }
  const portalRoot = document.querySelector('.cc-workspace') ?? document.body
  return (
    <>
      <aside
        className={`cc-library cl-library ${standalone ? 'cl-standalone' : ''} ${expanded ? 'cl-expanded' : ''} ${groupsOpen ? 'cl-groups-open' : ''}`}
        style={!standalone ? { width } : undefined}
        id="creative-library"
        aria-label="个人资产库"
        hidden={hidden}
      >
        <header className="cc-library-heading">
          <div>
            <h2>资产库</h2>
            <p>整理、检索，随手放入画布。</p>
          </div>
          {!standalone && (
            <>
              <button
                className="cc-icon-button"
                aria-label={expanded ? '收起资产管理' : '展开资产管理'}
                onClick={() => setWidth(expanded ? 318 : 720)}
              >
                {expanded ? <Minimize size={17} /> : <Maximize size={17} />}
              </button>
              <button
                className="cc-icon-button"
                aria-label="收起资产库"
                onClick={onClose}
              >
                <PanelLeftClose size={17} />
              </button>
            </>
          )}
        </header>
        <div className="cl-search-row">
          <div className="cl-search">
            <Search size={15} />
            <input
              className="input"
              ref={queryInput}
              aria-label="搜索个人资产"
              placeholder="搜索名称、描述或标签"
              value={queryDraft}
              onChange={(e) => setQueryDraft(e.target.value)}
              onCompositionStart={() => setComposing(true)}
              onCompositionEnd={() => setComposing(false)}
              onKeyDown={(e) => {
                if (e.key === 'Enter' && !e.nativeEvent.isComposing)
                  filter('lq', queryDraft.trim())
              }}
            />
            {queryDraft && (
              <button
                aria-label="清除资产搜索"
                onClick={() => {
                  setQueryDraft('')
                  filter('lq', '')
                  queryInput.current?.focus()
                }}
              >
                <X size={14} />
              </button>
            )}
          </div>
          <button
            className="cc-icon-button cl-create"
            aria-label="新增资产"
            disabled={disabled}
            onClick={onCreate}
          >
            <Plus size={19} />
          </button>
        </div>
        <div className="cl-browser">
          <nav className="cl-tree" aria-label="资产目录">
            <div className="cl-views" role="group" aria-label="资产视图">
              {[
                { key: 'all', label: '全部', Icon: Inbox },
                { key: 'favorites', label: '最爱', Icon: Heart },
                { key: 'unclassified', label: '未归类', Icon: Folder },
              ].map(({ key, label, Icon }) => (
                <button
                  key={key}
                  aria-pressed={view === key && !group}
                  onClick={() => selectView(key)}
                >
                  <Icon size={14} />
                  <span>{label}</span>
                </button>
              ))}
            </div>
            <div className="cl-group-heading">
              <span>我的分组</span>
              <button
                className="cc-icon-button"
                aria-label="新建分组"
                disabled={disabled}
                onClick={() => edit('group', { parent: group })}
              >
                <Plus size={14} />
              </button>
            </div>
            <button
              className="cl-all-scope"
              onClick={() => filter('lgroup', '')}
            >
              全库范围
            </button>
            {tree(null)}
            <div className="cl-nav-footer">
              <button
                aria-label="管理分组与标签"
                onClick={() => setManager(true)}
              >
                <TagIcon size={14} />
                标签管理
              </button>
              <button
                aria-pressed={view === 'trash'}
                onClick={() => selectView('trash')}
              >
                <Trash2 size={14} />
                回收站
              </button>
            </div>
          </nav>
          <section className="cl-results" aria-label="资产结果">
            <div className="cl-scope-row">
              <button
                aria-label="切换资产范围"
                aria-expanded={groupsOpen || expanded}
                onClick={() => setGroupsOpen(!groupsOpen)}
              >
                <Folder size={14} />
                <span>{currentScope}</span>
                <ChevronDown size={12} />
              </button>
              <button
                className="cc-icon-button"
                aria-label="分组与筛选"
                aria-expanded={filtersOpen}
                onClick={() => setFiltersOpen(!filtersOpen)}
              >
                <SlidersHorizontal size={15} />
              </button>
            </div>
            {filtersOpen && (
              <section className="cl-filters cc-glass" aria-label="资产筛选">
                <header>
                  <strong>
                    筛选资产{' '}
                    <small>
                      {loading ? '检索中' : `${page.total_count} 项匹配`}
                    </small>
                  </strong>
                  <button
                    className="cc-icon-button"
                    aria-label="关闭筛选"
                    onClick={() => setFiltersOpen(false)}
                  >
                    <X size={14} />
                  </button>
                </header>{' '}
                <label>
                  <input
                    type="checkbox"
                    disabled={!group}
                    checked={includeDescendants}
                    onChange={(e) => {
                      setIncludeDescendants(e.target.checked)
                      filter('ldesc', String(e.target.checked))
                    }}
                  />
                  包含子组
                </label>
                <div className="cl-tags">
                  {catalog.tags.map((t) => (
                    <button
                      key={t.id}
                      aria-pressed={tagIDs.includes(t.id)}
                      onClick={() =>
                        filter(
                          'ltags',
                          (tagIDs.includes(t.id)
                            ? tagIDs.filter((x) => x !== t.id)
                            : [...tagIDs, t.id]
                          ).join(','),
                        )
                      }
                    >
                      <i style={{ background: t.color }} />
                      {t.name}
                    </button>
                  ))}
                </div>
                <div className="cl-filter-selects">
                  <label>
                    标签匹配
                    <select
                      className="input"
                      value={params.get('lmode') ?? 'all'}
                      onChange={(e) => filter('lmode', e.target.value)}
                    >
                      <option value="all">同时包含</option>
                      <option value="any">任意包含</option>
                    </select>
                  </label>
                  <label>
                    资产类型
                    <select
                      className="input"
                      value={params.get('lkind') ?? ''}
                      onChange={(e) => filter('lkind', e.target.value)}
                    >
                      <option value="">全部类型</option>
                      <option value="text">文字</option>
                      <option value="link">链接</option>
                    </select>
                  </label>
                </div>
                <button
                  className="btn"
                  onClick={() => {
                    const next = new URLSearchParams(params)
                    ;[
                      'lgroup',
                      'ldesc',
                      'ltags',
                      'lmode',
                      'lkind',
                      'lq',
                    ].forEach((key) => next.delete(key))
                    setQueryDraft('')
                    if (next.toString() !== params.toString()) {
                      setLoading(true)
                      onLoading(true)
                      setParams(next, { replace: true })
                    }
                  }}
                >
                  清除筛选
                </button>
              </section>
            )}
            {(group || tagIDs.length > 0) && (
              <div className="cl-conditions">
                {group && (
                  <button onClick={() => filter('lgroup', '')}>
                    {currentScope}
                    <X size={11} />
                  </button>
                )}
                {tagIDs.map((id) => (
                  <button
                    key={id}
                    onClick={() =>
                      filter('ltags', tagIDs.filter((x) => x !== id).join(','))
                    }
                  >
                    {catalog.tags.find((t) => t.id === id)?.name ??
                      '已删除标签'}
                    <X size={11} />
                  </button>
                ))}
              </div>
            )}
            <div className="cl-list-heading">
              <span>
                {loading ? '正在加载…' : `${page.total_count} 项资产`}
              </span>
              <label>
                <span className="sr-only">资产排序</span>
                <select
                  aria-label="资产排序"
                  className="input"
                  value={params.get('lsort') ?? 'recent'}
                  onChange={(e) => filter('lsort', e.target.value)}
                >
                  <option value="recent">最近收藏</option>
                  <option value="oldest">最早收藏</option>
                  <option value="name">名称</option>
                </select>
              </label>
              {view === 'trash' && (
                <button
                  className="btn"
                  onClick={() =>
                    edit('settings', { revision: catalog.settings?.revision })
                  }
                >
                  保留设置
                </button>
              )}
            </div>
            {selected.length > 0 && (
              <div className="cl-batch">
                <span>已选 {selected.length} 项</span>
                {view !== 'trash' && (
                  <button
                    className="btn"
                    disabled={disabled}
                    onClick={() => edit('organize', { selected })}
                  >
                    批量整理
                  </button>
                )}
                <button
                  className="btn"
                  disabled={disabled}
                  onClick={() => batch(view === 'trash' ? 'restore' : 'trash')}
                >
                  {view === 'trash' ? '恢复' : '移入回收站'}
                </button>
                {view === 'trash' && (
                  <button
                    className="btn"
                    disabled={disabled}
                    onClick={() => batch('purge')}
                  >
                    彻底删除
                  </button>
                )}
                <button className="btn" onClick={() => setSelection([])}>
                  取消选择
                </button>
              </div>
            )}
            {(error || stale) && (
              <div role="alert" className="cl-error">
                <p>{error || '资产库已变化，请刷新当前列表。'}</p>
                <button className="btn" onClick={() => setRetry((v) => v + 1)}>
                  刷新列表
                </button>
              </div>
            )}

            <div className="cl-scroll">
              <div className="cc-assets cl-asset-grid">
                {page.items.map((a) => (
                  <article
                    key={a.id}
                    className={`cc-asset ${selection.includes(a.id) ? 'selected' : ''}`}
                  >
                    <button
                      className="cl-card-content"
                      aria-label={`选择资产：${a.title}`}
                      aria-pressed={selection.includes(a.id)}
                      draggable={
                        canDrop &&
                        !disabled &&
                        !a.unavailable &&
                        view !== 'trash'
                      }
                      onDragStart={(e) =>
                        e.dataTransfer.setData(
                          'application/creative-asset',
                          a.id,
                        )
                      }
                      onClick={(e) =>
                        selectAsset(a, e.metaKey || e.ctrlKey || e.shiftKey)
                      }
                      onDoubleClick={() => setPreview(a)}
                      onKeyDown={(e) => {
                        if (e.key === ' ') {
                          e.preventDefault()
                          setPreview(a)
                        }
                      }}
                    >
                      <div className={`cl-thumbnail cl-thumbnail-${a.kind}`}>
                        {a.kind === 'text' ? (
                          <Type size={19} />
                        ) : (
                          <Link2 size={19} />
                        )}
                        <p>
                          {a.unavailable
                            ? '当前无权展示'
                            : a.content && 'body' in a.content.payload
                              ? a.content.payload.body
                              : a.content && 'url' in a.content.payload
                                ? a.content.payload.url
                                : ''}
                        </p>
                      </div>
                      <h3>{a.title}</h3>
                    </button>
                    <div className="cl-card-quick">
                      <button
                        className="cc-icon-button"
                        aria-label={`预览资产：${a.title}`}
                        disabled={a.unavailable}
                        onClick={() => setPreview(a)}
                      >
                        <Eye size={14} />
                      </button>
                      {canDrop && view !== 'trash' && (
                        <button
                          className="cc-icon-button"
                          aria-label="放入画布"
                          title="放入画布"
                          disabled={disabled || a.unavailable}
                          onClick={() => onDrop(a)}
                        >
                          <Plus size={16} />
                        </button>
                      )}
                    </div>
                    <details className="cl-asset-menu">
                      <summary aria-label={`资产操作：${a.title}`}>
                        <Ellipsis size={16} />
                      </summary>
                      <div className="cl-row-menu cc-glass">
                        {view !== 'trash' ? (
                          <>
                            <button
                              aria-label={`${a.is_favorite ? '取消最爱' : '加入最爱'}：${a.title}`}
                              disabled={disabled}
                              onClick={() =>
                                void onCommand(`/assets/${a.id}/metadata`, {
                                  expected_revision: a.revision,
                                  is_favorite: !a.is_favorite,
                                })
                              }
                            >
                              <Heart size={14} />
                              {a.is_favorite ? '取消最爱' : '加入最爱'}
                            </button>
                            <button
                              disabled={disabled}
                              onClick={() =>
                                edit('metadata', {
                                  id: a.id,
                                  name: a.title,
                                  description: a.description,
                                  revision: a.revision,
                                })
                              }
                            >
                              <Pencil size={14} />
                              编辑信息
                            </button>
                            <button
                              disabled={disabled}
                              onClick={() =>
                                edit('organize', { selected: [a] })
                              }
                            >
                              <Folder size={14} />
                              整理分组与标签
                            </button>
                          </>
                        ) : (
                          <>
                            <button
                              disabled={disabled}
                              onClick={() =>
                                void onCommand(`/assets/${a.id}/restore`, {
                                  expected_revision: a.revision,
                                })
                              }
                            >
                              恢复资产
                            </button>
                            <button
                              disabled={disabled}
                              onClick={() =>
                                confirm(
                                  `/assets/${a.id}/purge`,
                                  { expected_revision: a.revision },
                                  '彻底删除资产？',
                                  '个人库条目无法恢复，画布中已引用的内容会保留。',
                                  '彻底删除',
                                )
                              }
                            >
                              彻底删除
                            </button>
                          </>
                        )}
                      </div>
                    </details>
                  </article>
                ))}
              </div>{' '}
              {!loading && !error && !page.items.length && (
                <div className="cl-empty">
                  <Inbox size={28} />
                  <p>
                    {view === 'trash'
                      ? '回收站是空的'
                      : keyword || group || tagIDs.length
                        ? '没有找到匹配的资产'
                        : '把灵感收进这里'}
                  </p>
                  <small>可以调整筛选，或添加文字与链接。</small>
                </div>
              )}
              {page.next_cursor && !stale && (
                <button
                  className="btn"
                  disabled={moreLoading}
                  onClick={() => void more()}
                >
                  {moreLoading ? '正在加载…' : '加载更多资产'}
                </button>
              )}
            </div>
          </section>
        </div>
        {!standalone && (
          <footer className="cl-library-foot">
            ↗　拖到画布，或点击素材上的 +
          </footer>
        )}
        {!standalone && (
          <div
            className="cl-resize"
            role="separator"
            aria-label="调整资产库宽度"
            aria-orientation="vertical"
            aria-valuemin={280}
            aria-valuemax={880}
            aria-valuenow={width}
            tabIndex={0}
            onKeyDown={(e) => {
              if (e.key === 'ArrowLeft' || e.key === 'ArrowRight') {
                e.preventDefault()
                setWidth((v) =>
                  Math.min(
                    880,
                    Math.max(280, v + (e.key === 'ArrowRight' ? 20 : -20)),
                  ),
                )
              }
            }}
            onPointerDown={(e) => {
              resizeStart.current = { x: e.clientX, width }
              e.currentTarget.setPointerCapture(e.pointerId)
            }}
            onPointerMove={(e) => {
              if (resizeStart.current)
                setWidth(
                  Math.min(
                    880,
                    Math.max(
                      280,
                      resizeStart.current.width +
                        e.clientX -
                        resizeStart.current.x,
                    ),
                  ),
                )
            }}
            onPointerUp={() => {
              resizeStart.current = null
            }}
            onPointerCancel={() => {
              resizeStart.current = null
            }}
          />
        )}
      </aside>
      {createPortal(
        <>
          {manager && (
            <StudioDialog title="标签与分类" onClose={() => setManager(false)}>
              <section className="cl-manager" aria-label="目录管理">
                <div className="cc-actions">
                  <button
                    className="btn"
                    disabled={disabled}
                    onClick={() => edit('group', { parent: group })}
                  >
                    新建分组
                  </button>
                  <button
                    className="btn"
                    disabled={disabled}
                    onClick={() => edit('tag')}
                  >
                    新建标签
                  </button>
                  <button
                    className="btn"
                    disabled={disabled}
                    onClick={() => edit('category')}
                  >
                    新建分类
                  </button>
                </div>

                {catalog.categories.map((c) => (
                  <div className="cl-manage-row" key={c.id}>
                    <span>{c.name}</span>
                    <button
                      aria-label={`编辑分类：${c.name}`}
                      onClick={() =>
                        edit('category', {
                          id: c.id,
                          name: c.name,
                          revision: c.revision,
                        })
                      }
                    >
                      <Pencil size={12} />
                    </button>
                    <button
                      aria-label={`删除分类：${c.name}`}
                      onClick={() =>
                        confirm(
                          `/tag-categories/${c.id}/delete`,
                          {
                            expected_revision: c.revision,
                            hierarchy_revision: catalog.hierarchy,
                          },
                          '删除标签分类？',
                          '标签会保留并归入未分类。',
                          '删除分类',
                        )
                      }
                    >
                      <X size={12} />
                    </button>
                  </div>
                ))}
                {catalog.tags.map((t) => (
                  <div className="cl-manage-row" key={t.id}>
                    <TagIcon size={12} />
                    <span>{t.name}</span>
                    <small>
                      {catalog.categories.find((c) => c.id === t.category_id)
                        ?.name ?? '未分类'}
                    </small>
                    <button
                      aria-label={`编辑标签：${t.name}`}
                      onClick={() =>
                        edit('tag', {
                          id: t.id,
                          name: t.name,
                          color: t.color,
                          parent: t.category_id ?? '',
                          revision: t.revision,
                        })
                      }
                    >
                      <Pencil size={12} />
                    </button>
                    <button
                      aria-label={`删除标签：${t.name}`}
                      onClick={() =>
                        confirm(
                          `/tags/${t.id}/delete`,
                          {
                            expected_revision: t.revision,
                            hierarchy_revision: catalog.hierarchy,
                          },
                          '删除标签？',
                          '将移除所有资产的此标签，资产本身保留。',
                          '删除标签',
                        )
                      }
                    >
                      <X size={12} />
                    </button>
                  </div>
                ))}
              </section>
            </StudioDialog>
          )}
          {editor && (
            <LibraryEditor
              key={`${editor.kind}:${editor.id ?? 'new'}`}
              editor={editor}
              catalog={catalog}
              disabled={disabled}
              onCancel={() => setEditor(null)}
              onSave={onCommand}
              onDone={() => setEditor(null)}
            />
          )}
          {confirmation && (
            <ConfirmDialog
              title={confirmation.title}
              body={confirmation.body}
              confirmLabel={confirmation.label}
              danger={!confirmation.path.endsWith('restore')}
              busy={busy}
              onCancel={() => setConfirmation(null)}
              onConfirm={() => {
                void onCommand(confirmation.path, confirmation.payload).then(
                  (ok) => {
                    if (ok) setConfirmation(null)
                  },
                )
              }}
            />
          )}

          {preview && (
            <StudioDialog
              title={preview.title}
              onClose={() => setPreview(null)}
            >
              <div className="cl-text-preview">
                {preview.unavailable
                  ? '当前无权展示此内容'
                  : previewError ||
                    (previewContent
                      ? 'body' in previewContent.payload
                        ? previewContent.payload.body
                        : previewContent.payload.url
                      : '正在读取内容…')}
              </div>
            </StudioDialog>
          )}
        </>,
        portalRoot,
      )}
    </>
  )
}
