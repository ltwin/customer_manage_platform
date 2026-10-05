import { createPortal } from 'react-dom'
import StudioDialog from './StudioDialog.tsx'
import {
  useCallback,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
} from 'react'
import { useSearchParams } from 'react-router-dom'
import {
  Folder,
  Search,
  Maximize,
  Minimize,
  ChevronDown,
  ChevronLeft,
  ChevronRight,
  Ellipsis,
  Heart,
  Inbox,
  Trash2,
  Plus,
  PanelLeftClose,
  X,
  Pencil,
  ArrowRight,
  Tag as TagIcon,
  Upload,
  FolderPlus,
  RotateCcw,
  Check,
} from 'lucide-react'
import ConfirmDialog from '../../components/ConfirmDialog'
import * as api from './api.ts'
import { errorMessage } from './queue.ts'
import { PREVIEW, emptyCatalog, kindLabel, metaLine, type Catalog, type Editor } from './libraryState.ts'
import LibraryEditor from './LibraryEditor.tsx'
import CanvasContextMenu, { type CanvasMenuItem } from './CanvasContextMenu.tsx'
import { Masonry, Peek, PreviewGrid, QuickLook, Tile, type TileMode } from './LibraryTiles.tsx'
import './library.css'

type Confirmation = {
  title: string
  body: string
  path: string
  payload: unknown
  label: string
}
/** 带着任务打开资产库：给空节点选一份素材填进去，或为节点选参考。 */
export type LibraryTask =
  | { kind: 'fill'; nodeID: string; mediaKind: 'image' | 'video' | 'audio'; title: string }
  | { kind: 'pick'; nodeID: string; title: string }
type NameEdit = { kind: 'new'; parent: string | null } | { kind: 'rename'; id: string }
type Menu = { x: number; y: number; label: string; items: CanvasMenuItem[] }

const initialPage = (): api.AssetPage => ({
  items: [],
  next_cursor: '',
  total_count: 0,
  library_revision: '1',
})
const VIEWS = [
  { key: 'all', label: '全部' },
  { key: 'favorites', label: '最爱' },
  { key: 'unclassified', label: '未归类' },
  { key: 'trash', label: '回收站' },
] as const
const KINDS = [
  ['', '全部'],
  ['image', '图片'],
  ['video', '视频'],
  ['audio', '音频'],
  ['text', '文字'],
  ['link', '链接'],
] as const
const SORTS = [
  ['recent', '最近收藏'],
  ['oldest', '最早收藏'],
  ['name', '名称'],
] as const
const normalizeName = (s: string) => s.normalize('NFKC').trim().toLowerCase()
const FILTER_KEYS = ['lgroup', 'ltags', 'lmode', 'lkind', 'lq']

export default function LibraryPanel({
  disabled,
  busy,
  hidden,
  standalone,
  tick,
  task,
  usage,
  onCreate,
  onClose,
  onWidth,
  onAssets,
  onLoading,
  onCatalog,
  onCommand,
  canPlace,
  onPlace,
  onFill,
  onPick,
  onLocate,
  onImport,
  importDisabled,
}: {
  disabled: boolean
  busy: boolean
  hidden: boolean
  standalone: boolean
  tick: number
  task: LibraryTask | null
  /** 画布上每个内容修订对应的节点，用于「已在画布上」角标与定位。 */
  usage: Map<string, string[]>
  onCreate: () => void
  onClose: () => void
  onWidth: (width: number) => void
  onAssets: (assets: api.Asset[]) => void
  onLoading: (loading: boolean) => void
  onCatalog: (catalog: Catalog) => void
  onCommand: (path: string, payload: unknown) => Promise<boolean>
  canPlace: boolean
  onPlace: (assets: api.Asset[]) => void
  onFill: (asset: api.Asset) => void
  onPick: (assets: api.Asset[]) => void
  onLocate: (nodeIDs: string[]) => void
  onImport: (files: File[], groupID: string) => void
  importDisabled: boolean
}) {
  const fileInput = useRef<HTMLInputElement>(null)
  const shelf = useRef<HTMLDivElement>(null)
  const [dragOver, setDragOver] = useState(false)
  const [params, setParams] = useSearchParams()
  const [catalog, setCatalog] = useState(emptyCatalog)
  const [page, setPage] = useState(initialPage)
  const [previews, setPreviews] = useState<Record<string, api.AssetPage>>({})
  const [loading, setLoading] = useState(true)
  const [previewLoading, setPreviewLoading] = useState(false)
  const [catalogLoading, setCatalogLoading] = useState(true)
  const [moreLoading, setMoreLoading] = useState(false)
  const [error, setError] = useState('')
  const [stale, setStale] = useState(false)
  const [retry, setRetry] = useState(0)
  const [selection, setSelection] = useState<string[]>([])
  const lastPicked = useRef<string | null>(null)
  const [manager, setManager] = useState(false)
  const [width, setWidth] = useState(344)
  const [columns, setColumns] = useState(2)
  const [collapsed, setCollapsed] = useState<Set<string>>(() => new Set())
  const [nameEdit, setNameEdit] = useState<NameEdit | null>(null)
  // 分组名称提交中的锁；进入新的编辑或结束编辑时释放。
  const naming = useRef(false)
  useEffect(() => {
    naming.current = false
  }, [nameEdit])
  const [notice, setNotice] = useState('')
  const [menu, setMenu] = useState<Menu | null>(null)
  const [tagsOpen, setTagsOpen] = useState<DOMRect | null>(null)
  const [look, setLook] = useState<string | null>(null)
  const [peek, setPeek] = useState<{ asset: api.Asset; el: HTMLElement } | null>(null)
  const peekTimer = useRef(0)
  const [editor, setEditor] = useState<Editor | null>(null)
  const [confirmation, setConfirmation] = useState<Confirmation | null>(null)
  const generation = useRef(0)
  const queryInput = useRef<HTMLInputElement>(null)
  useEffect(() => onWidth(width), [width, onWidth])
  const expanded = standalone || width >= 500
  useEffect(() => {
    if (!notice) return
    const timer = setTimeout(() => setNotice(''), 4000)
    return () => clearTimeout(timer)
  }, [notice])

  const view = params.get('lview') ?? 'all',
    group = params.get('lgroup') ?? '',
    keyword = params.get('lq') ?? '',
    sort = params.get('lsort') ?? 'recent',
    tagMode = params.get('lmode') ?? 'all',
    kindParam = params.get('lkind') ?? ''
  const tagIDs = (params.get('ltags') ?? '').split(',').filter(Boolean)
  // 填充任务只接受节点同类的素材。
  const kind = task?.kind === 'fill' ? task.mediaKind : kindParam
  const mode: TileMode = task?.kind ?? 'browse'
  // 「全部」且没有关键词、分组或筛选时按分组平铺浏览；否则是一张结果网格。
  const overview = view === 'all' && !group && !keyword && !tagIDs.length && !kindParam
  const [queryDraft, setQueryDraft] = useState(keyword)
  const [composing, setComposing] = useState(false)
  const apply = useCallback(
    (next: URLSearchParams) => {
      if (next.toString() === params.toString()) return
      setLoading(true)
      onLoading(true)
      setParams(next, { replace: true })
    },
    [params, setParams, onLoading],
  )
  function filter(key: string, value: string) {
    const next = new URLSearchParams(params)
    if (value) next.set(key, value)
    else next.delete(key)
    apply(next)
  }
  function scope(nextView: string, nextGroup = '', keep = false) {
    const next = new URLSearchParams(params)
    next.set('lview', nextView)
    if (nextGroup) next.set('lgroup', nextGroup)
    else next.delete('lgroup')
    if (!keep) {
      next.delete('lq')
      setQueryDraft('')
    }
    apply(next)
    setSelection([])
    shelf.current?.scrollTo({ top: 0 })
  }
  function clearFilters() {
    const next = new URLSearchParams(params)
    FILTER_KEYS.forEach((key) => next.delete(key))
    setQueryDraft('')
    apply(next)
  }
  useEffect(() => {
    setQueryDraft(keyword)
  }, [keyword])
  useEffect(() => {
    if (composing || queryDraft.trim() === keyword) return
    const timer = setTimeout(() => filter('lq', queryDraft.trim()), 300)
    return () => clearTimeout(timer)
  })
  // 带着任务进来时从全部分组开始找，不停在上次浏览的视图和条件里。
  const taskKey = task ? `${task.kind}:${task.nodeID}` : ''
  useEffect(() => {
    if (!taskKey) return
    setSelection([])
    const next = new URLSearchParams(params)
    ;[...FILTER_KEYS, 'lview'].forEach((key) => next.delete(key))
    setQueryDraft('')
    apply(next)
    // 只在任务切换时重置。
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [taskKey])

  const query = new URLSearchParams({ view, limit: '30', sort, tag_mode: tagMode })
  if (group) {
    query.set('group_id', group)
    // 进组即看到它和子组的全部素材（Eagle「在父文件夹显示子文件夹内容」）。
    query.set('include_descendants', 'true')
  }
  if (keyword) query.set('q', keyword)
  if (tagIDs.length) query.set('tag_ids', tagIDs.join(','))
  if (kind) query.set('kind', kind)
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
          [groups.hierarchy_revision, tags.hierarchy_revision, categories.hierarchy_revision].some(
            (revision) => revision !== settings.hierarchy_revision,
          )
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
    onLoading(loading || catalogLoading || previewLoading)
  }, [loading, catalogLoading, previewLoading, onLoading])
  // 结果网格：平铺浏览时不需要，改由各分组预览请求。
  useEffect(() => {
    const controller = new AbortController()
    const current = ++generation.current
    setMoreLoading(false)
    setStale(false)
    setSelection([])
    setError('')
    setPage(initialPage())
    if (overview) {
      setLoading(false)
      return () => controller.abort()
    }
    setLoading(true)
    void api
      .read<api.AssetPage>(`/assets?${queryKey}`, controller.signal)
      .then((result) => {
        if (current === generation.current && !controller.signal.aborted) setPage(result)
      })
      .catch((e: unknown) => {
        if (!controller.signal.aborted) setError(errorMessage(e))
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false)
      })
    return () => controller.abort()
  }, [queryKey, overview, tick, retry])
  const roots = useMemo(() => catalog.groups.filter((g) => !g.parent_id), [catalog.groups])
  const rootsKey = roots.map((g) => g.id).join(',')
  useEffect(() => {
    if (!overview) {
      setPreviews({})
      return
    }
    const controller = new AbortController()
    const scopes = [...rootsKey.split(',').filter(Boolean), '']
    const url = (id: string) => {
      const q = new URLSearchParams({ limit: String(PREVIEW), sort })
      if (id) {
        q.set('view', 'all')
        q.set('group_id', id)
        q.set('include_descendants', 'true')
      } else q.set('view', 'unclassified')
      if (kind) q.set('kind', kind)
      return `/assets?${q.toString()}`
    }
    setPreviewLoading(true)
    void Promise.all(scopes.map((id) => api.read<api.AssetPage>(url(id), controller.signal)))
      .then((pages) => {
        if (!controller.signal.aborted)
          setPreviews(Object.fromEntries(scopes.map((id, i) => [id, pages[i]!])))
      })
      .catch((e: unknown) => {
        if (!controller.signal.aborted) setError(errorMessage(e))
      })
      .finally(() => {
        if (!controller.signal.aborted) setPreviewLoading(false)
      })
    return () => controller.abort()
  }, [overview, rootsKey, sort, kind, tick, retry])
  const known = useMemo(() => {
    const byID = new Map<string, api.Asset>()
    for (const a of page.items) byID.set(a.id, a)
    for (const p of Object.values(previews)) for (const a of p.items) byID.set(a.id, a)
    return byID
  }, [page, previews])
  useEffect(() => onAssets([...known.values()]), [known, onAssets])
  useLayoutEffect(() => {
    const el = shelf.current
    if (!el || typeof ResizeObserver === 'undefined') return
    const observer = new ResizeObserver(([entry]) => {
      const w = entry?.contentRect.width ?? 0
      setColumns(Math.max(2, Math.floor((w + 6) / 170)))
    })
    observer.observe(el)
    return () => observer.disconnect()
  }, [])

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
      setPage({
        ...result,
        items: [...page.items, ...result.items.filter((a) => !page.items.some((old) => old.id === a.id))],
      })
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
  function confirm(path: string, payload: unknown, title: string, body: string, label: string) {
    setConfirmation({ path, payload, title, body, label })
  }
  const groupOf = (id: string | null | undefined) => catalog.groups.find((g) => g.id === id)
  const childrenOf = (id: string) => catalog.groups.filter((g) => g.parent_id === id)
  const trail = (id: string) => {
    const out: api.Group[] = []
    const seen = new Set<string>()
    for (let g = groupOf(id); g && !seen.has(g.id); g = groupOf(g.parent_id)) {
      seen.add(g.id)
      out.unshift(g)
    }
    return out
  }
  const selected = selection.map((id) => known.get(id)).filter((a): a is api.Asset => !!a)
  function batch(action: 'trash' | 'restore' | 'purge', list = selected) {
    confirm(
      `/assets/batch-${action}`,
      { assets: list.map((a) => ({ asset_id: a.id, expected_revision: a.revision })) },
      action === 'purge' ? '彻底删除所选资产？' : action === 'trash' ? '移入回收站？' : '恢复所选资产？',
      action === 'purge'
        ? '个人库中的条目无法恢复。画布中已经引用的内容会保留。'
        : `${list.length} 项资产；分组与最爱关系会保留。`,
      action === 'purge' ? '彻底删除' : action === 'trash' ? '移入回收站' : '恢复资产',
    )
  }

  // ---------- 选择、查看与键盘 ----------
  const tileIDs = () =>
    Array.from(shelf.current?.querySelectorAll<HTMLElement>('[data-asset]') ?? []).map(
      (el) => el.dataset.asset!,
    )
  function toggle(a: api.Asset, range = false) {
    if (range && lastPicked.current) {
      const ids = tileIDs()
      const [i, j] = [ids.indexOf(lastPicked.current), ids.indexOf(a.id)].sort((x, y) => x - y)
      if (i! >= 0 && j! >= 0) {
        setSelection((old) => [...new Set([...old, ...ids.slice(i, j! + 1)])].slice(0, 100))
        return
      }
    }
    lastPicked.current = a.id
    setSelection((old) =>
      old.includes(a.id) ? old.filter((id) => id !== a.id) : [...old, a.id].slice(0, 100),
    )
  }
  function activate(a: api.Asset) {
    if (a.unavailable) return
    if (task?.kind === 'fill') onFill(a)
    else if (task?.kind === 'pick') toggle(a)
    else if (canPlace && view !== 'trash') onPlace([a])
  }
  function clickTile(a: api.Asset, e: React.MouseEvent) {
    if (task?.kind === 'fill') return activate(a)
    // 选参考、已有多选或按住 ⌘ / Shift 时点击就是选择；否则快速查看。
    if (task?.kind === 'pick' || selection.length || e.metaKey || e.ctrlKey || e.shiftKey)
      return toggle(a, e.shiftKey)
    setLook(a.id)
  }
  // 方向键在卡片间移动：取目标方向上中心最近的一张，瀑布流与方格通用。
  function moveFocus(from: HTMLElement, key: string) {
    const r = from.getBoundingClientRect()
    const cx = r.left + r.width / 2,
      cy = r.top + r.height / 2
    let best: HTMLElement | null = null,
      score = Infinity
    for (const el of Array.from(shelf.current?.querySelectorAll<HTMLElement>('[data-asset]') ?? [])) {
      if (el === from) continue
      const b = el.getBoundingClientRect()
      const dx = b.left + b.width / 2 - cx,
        dy = b.top + b.height / 2 - cy
      const ahead =
        key === 'ArrowRight' ? dx > 4 : key === 'ArrowLeft' ? dx < -4 : key === 'ArrowDown' ? dy > 4 : dy < -4
      if (!ahead) continue
      const along = key === 'ArrowLeft' || key === 'ArrowRight' ? Math.abs(dx) : Math.abs(dy)
      const across = key === 'ArrowLeft' || key === 'ArrowRight' ? Math.abs(dy) : Math.abs(dx)
      const s = along + across * 3
      if (s < score) {
        score = s
        best = el
      }
    }
    best?.focus()
    best?.scrollIntoView({ block: 'nearest' })
  }
  function tileKey(a: api.Asset, e: React.KeyboardEvent<HTMLElement>) {
    if (e.target !== e.currentTarget) return
    if (e.key === ' ') {
      e.preventDefault()
      setPeek(null)
      setLook(a.id)
    } else if (e.key === 'Enter') {
      e.preventDefault()
      activate(a)
    } else if (e.key.startsWith('Arrow')) {
      e.preventDefault()
      moveFocus(e.currentTarget, e.key)
    }
  }
  function showPeek(a: api.Asset | null, el?: HTMLElement) {
    window.clearTimeout(peekTimer.current)
    if (!a || !el) return setPeek(null)
    peekTimer.current = window.setTimeout(() => {
      if (el.isConnected) setPeek({ asset: a, el })
    }, 360)
  }
  useEffect(() => () => window.clearTimeout(peekTimer.current), [])
  function dragStart(a: api.Asset, e: React.DragEvent) {
    const list = selection.includes(a.id) && selection.length > 1 ? selected.filter((x) => !x.unavailable) : [a]
    e.dataTransfer.setData('application/creative-asset', list.map((x) => x.id).join(','))
    e.dataTransfer.setData('application/creative-asset+json', JSON.stringify(list))
    e.dataTransfer.effectAllowed = 'copy'
    if (list.length > 1) {
      const ghost = document.createElement('div')
      ghost.className = 'cl-drag-ghost'
      ghost.textContent = `${list.length} 项`
      document.body.append(ghost)
      e.dataTransfer.setDragImage(ghost, 20, 14)
      setTimeout(() => ghost.remove())
    }
  }
  function assetMenu(a: api.Asset, at: { x: number; y: number }) {
    setPeek(null)
    const items: CanvasMenuItem[] =
      view === 'trash'
        ? [
            {
              label: '恢复资产',
              icon: <RotateCcw size={15} />,
              disabled,
              action: () => void onCommand(`/assets/${a.id}/restore`, { expected_revision: a.revision }),
            },
            {
              label: '彻底删除',
              icon: <Trash2 size={15} />,
              danger: true,
              disabled,
              action: () =>
                confirm(
                  `/assets/${a.id}/purge`,
                  { expected_revision: a.revision },
                  '彻底删除资产？',
                  '个人库条目无法恢复，画布中已引用的内容会保留。',
                  '彻底删除',
                ),
            },
          ]
        : [
            {
              label: a.is_favorite ? '取消最爱' : '加入最爱',
              icon: <Heart size={15} />,
              disabled,
              action: () =>
                void onCommand(`/assets/${a.id}/metadata`, {
                  expected_revision: a.revision,
                  is_favorite: !a.is_favorite,
                }),
            },
            {
              label: '编辑信息',
              icon: <Pencil size={15} />,
              disabled,
              action: () =>
                edit('metadata', { id: a.id, name: a.title, description: a.description, revision: a.revision }),
            },
            {
              label: '整理分组与标签',
              icon: <Folder size={15} />,
              disabled,
              action: () => edit('organize', { selected: [a] }),
            },
            'separator',
            {
              label: '移入回收站',
              icon: <Trash2 size={15} />,
              danger: true,
              disabled,
              action: () => batch('trash', [a]),
            },
          ]
    setMenu({ ...at, label: `资产操作：${a.title}`, items })
  }

  // ---------- 分组：就地新建与改名 ----------
  function startNew(parent: string | null) {
    if (parent)
      setCollapsed((old) => {
        const next = new Set(old)
        next.delete(parent)
        return next
      })
    setNameEdit({ kind: 'new', parent })
  }
  // 校验或提交失败时保留输入框和已输入的名称，让摄影师就地修改；只有成功、
  // 空名称或名称未变才关闭。提交中再按 Enter 或失焦不会重复发请求。
  function commitName(input: HTMLInputElement, refocus: boolean) {
    const e = nameEdit
    if (!e || naming.current) return
    const keep = (message: string) => {
      setNotice(message)
      if (refocus) input.focus()
    }
    const name = input.value.trim()
    if (!name) return setNameEdit(null)
    if ([...name].length > 80) return keep('分组名称最多 80 个字')
    const parent = e.kind === 'new' ? e.parent : (groupOf(e.id)?.parent_id ?? null)
    const current = e.kind === 'rename' ? groupOf(e.id) : undefined
    if (current && current.name === name) return setNameEdit(null)
    if (
      catalog.groups.some(
        (g) =>
          g.id !== current?.id &&
          (g.parent_id ?? null) === parent &&
          normalizeName(g.name) === normalizeName(name),
      )
    )
      return keep(`同一层已有「${name}」，换个名称再确认`)
    naming.current = true
    void (
      e.kind === 'new'
        ? onCommand('/asset-groups', { name, parent_id: parent, hierarchy_revision: catalog.hierarchy })
        : onCommand(`/asset-groups/${e.id}/rename`, {
            name,
            expected_revision: current?.revision,
            hierarchy_revision: catalog.hierarchy,
          })
    ).then((ok) => {
      if (!ok) {
        naming.current = false
        if (refocus && input.isConnected) input.focus()
        return
      }
      // 成功后锁保持到编辑状态切换（见上方 effect），避免输入框卸载时补发的失焦再提交一次。
      setNameEdit(null)
      if (e.kind === 'new') setNotice(`已新建「${name}」`)
    })
  }
  function nameInput(value: string, label: string) {
    return (
      <input
        className="cl-name-input"
        aria-label={label}
        defaultValue={value}
        maxLength={80}
        placeholder="分组名称"
        autoFocus
        onFocus={(e) => e.currentTarget.select()}
        onClick={(e) => e.stopPropagation()}
        onKeyDown={(e) => {
          e.stopPropagation()
          if (e.nativeEvent.isComposing) return
          if (e.key === 'Enter') {
            e.preventDefault()
            commitName(e.currentTarget, true)
          } else if (e.key === 'Escape') {
            e.preventDefault()
            setNameEdit(null)
          }
        }}
        // 失焦等于确认，和画布上重命名节点的习惯一致；空名称视为取消。
        // 失焦时校验失败不抢回焦点，输入框留在原位等摄影师回来修改。
        onBlur={(e) => commitName(e.currentTarget, false)}
      />
    )
  }
  const renaming = (id: string) => nameEdit?.kind === 'rename' && nameEdit.id === id
  const addingUnder = (parent: string | null) => nameEdit?.kind === 'new' && nameEdit.parent === parent
  function groupMenu(g: api.Group, at: { x: number; y: number }) {
    const root = !g.parent_id
    const children = childrenOf(g.id).length
    setMenu({
      ...at,
      label: `分组操作：${g.name}`,
      items: [
        {
          label: '重命名分组',
          icon: <Pencil size={15} />,
          disabled,
          action: () => setNameEdit({ kind: 'rename', id: g.id }),
        },
        // 最多两层：面包屑和子组标签都不需要再折叠。
        ...(root
          ? [
              {
                label: '新建子分组',
                icon: <FolderPlus size={15} />,
                disabled,
                action: () => startNew(g.id),
              },
            ]
          : []),
        {
          label: '移动分组',
          icon: <ArrowRight size={15} />,
          disabled,
          action: () =>
            edit('move', {
              id: g.id,
              name: g.name,
              parent: g.parent_id ?? '',
              position: g.position,
              revision: g.revision,
            }),
        },
        'separator',
        {
          label: '删除分组',
          icon: <Trash2 size={15} />,
          danger: true,
          sub: children ? '素材保留，子分组移到上一级' : '素材保留在资产库',
          disabled,
          action: () =>
            confirm(
              `/asset-groups/${g.id}/delete`,
              { expected_revision: g.revision, hierarchy_revision: catalog.hierarchy },
              `删除分组「${g.name}」？`,
              children ? '素材保留在资产库，子分组移到上一级。' : '素材保留在资产库。',
              '删除分组',
            ),
        },
      ],
    })
  }
  const openGroupMenu = (g: api.Group) => (e: React.MouseEvent<HTMLElement>) => {
    e.stopPropagation()
    const r = e.currentTarget.getBoundingClientRect()
    groupMenu(g, { x: r.right - 200, y: r.bottom + 4 })
  }
  const contextGroupMenu = (g: api.Group) => (e: React.MouseEvent) => {
    e.preventDefault()
    groupMenu(g, { x: e.clientX, y: e.clientY })
  }

  // ---------- 渲染 ----------
  const tile = (a: api.Asset, square = false) => (
    <Tile
      key={a.id}
      asset={a}
      square={square}
      selected={selection.includes(a.id)}
      mode={mode}
      used={usage.get(a.content_revision_id)?.length ?? 0}
      canPlace={canPlace && view !== 'trash'}
      draggable={canPlace && !disabled && !a.unavailable && view !== 'trash' && mode === 'browse'}
      onClick={clickTile}
      onToggle={toggle}
      onPlace={(x) => onPlace([x])}
      onMenu={assetMenu}
      onLocate={(x) => onLocate(usage.get(x.content_revision_id) ?? [])}
      onDragStart={dragStart}
      onKey={tileKey}
      onPeek={showPeek}
    />
  )
  function subChips(parent: string) {
    const subs = childrenOf(parent)
    if (!subs.length && !addingUnder(parent)) return null
    return (
      <div className="cl-sub-chips">
        {subs.map((s) =>
          renaming(s.id) ? (
            <span key={s.id} className="cl-chip-edit">
              <Folder size={13} />
              {nameInput(s.name, '重命名分组')}
            </span>
          ) : (
            <button
              key={s.id}
              type="button"
              data-drop-group={s.id}
              onClick={() => scope('all', s.id)}
              onContextMenu={contextGroupMenu(s)}
            >
              <Folder size={13} />
              {s.name}
            </button>
          ),
        )}
        {addingUnder(parent) && (
          <span className="cl-chip-edit">
            <Folder size={13} />
            {nameInput('', '新建子分组')}
          </span>
        )}
      </div>
    )
  }
  function section(g: api.Group | null) {
    const id = g?.id ?? ''
    const p = previews[id]
    const total = Number(p?.total_count ?? 0)
    if (!g && !total) return null
    const name = g?.name ?? '未归类'
    const open = !collapsed.has(id || 'unclassified')
    return (
      <section key={id || 'unclassified'} className="cl-sec" aria-label={name}>
        <header className="cl-sec-head" data-drop-group={id} onContextMenu={g ? contextGroupMenu(g) : undefined}>
          {g && renaming(g.id) ? (
            <span className="cl-sec-edit">
              <ChevronDown size={14} />
              {nameInput(g.name, '重命名分组')}
            </span>
          ) : (
            <button
              type="button"
              className="cl-sec-toggle"
              aria-expanded={open}
              onClick={() =>
                setCollapsed((old) => {
                  const next = new Set(old)
                  const key = id || 'unclassified'
                  if (next.has(key)) next.delete(key)
                  else next.add(key)
                  return next
                })
              }
            >
              <ChevronDown size={14} />
              <span>{name}</span>
              <em>{p ? total : ''}</em>
            </button>
          )}
          <span className="cl-sec-actions">
            {g && (
              <button type="button" className="cl-icon" aria-label={`分组操作：${g.name}`} onClick={openGroupMenu(g)}>
                <Ellipsis size={14} />
              </button>
            )}
            <button
              type="button"
              className="cl-sec-open"
              onClick={() => scope(g ? 'all' : 'unclassified', id)}
            >
              查看全部
              <ChevronRight size={13} />
            </button>
          </span>
        </header>
        {open && (
          <>
            {g && subChips(g.id)}
            {!p ? (
              <p className="cl-sec-empty">正在加载…</p>
            ) : p.items.length ? (
              <PreviewGrid
                page={p}
                label={name}
                tile={(a) => tile(a, true)}
                onMore={() => scope(g ? 'all' : 'unclassified', id)}
              />
            ) : (
              <p className="cl-sec-empty">
                这个分组还没有素材，把画布上的节点拖到组名上即可收进来
              </p>
            )}
          </>
        )}
      </section>
    )
  }
  const path = group ? trail(group) : []
  const scopeName = group
    ? (groupOf(group)?.name ?? '分组已删除')
    : keyword && view === 'all'
      ? '搜索结果'
      : view === 'all'
        ? '筛选结果'
        : (VIEWS.find((v) => v.key === view)?.label ?? '全部')
  const narrowed = !!(keyword || tagIDs.length || kindParam)
  const title =
    task?.kind === 'fill'
      ? `选择${kindLabel[task.mediaKind]}`
      : task?.kind === 'pick'
        ? '选择参考素材'
        : '资产库'
  const lookAsset = look ? known.get(look) : undefined
  const portalRoot = document.querySelector('.cc-workspace') ?? document.body
  const emptyText =
    view === 'trash'
      ? '回收站是空的'
      : narrowed
        ? '没有找到匹配的资产'
        : group
          ? '这个分组还没有素材，把画布上的节点拖到这里即可收进来'
          : view === 'favorites'
            ? '还没有加入最爱的素材'
            : view === 'unclassified'
              ? '所有素材都已归类'
              : '把灵感收进这里'
  return (
    <>
      <aside
        className={`cc-library cl-library ${standalone ? 'cl-standalone' : ''} ${expanded ? 'cl-expanded' : ''} ${dragOver ? 'is-drop-hot' : ''} ${task ? `is-${task.kind}` : ''}`}
        style={!standalone ? { width } : undefined}
        id="creative-library"
        aria-label="个人资产库"
        data-scope-group={group}
        hidden={hidden}
        onKeyDown={(e) => {
          if (e.key !== 'Escape' || e.defaultPrevented) return
          if (selection.length && task?.kind !== 'pick') {
            e.preventDefault()
            setSelection([])
          } else if (!standalone) {
            e.preventDefault()
            onClose()
          }
        }}
        onScroll={() => setPeek(null)}
        onDragOver={(e) => {
          if (!e.dataTransfer.types.includes('Files') || importDisabled) return
          e.preventDefault()
          e.dataTransfer.dropEffect = 'copy'
          setDragOver(true)
        }}
        onDragLeave={(e) => {
          if (!e.currentTarget.contains(e.relatedTarget as Node | null)) setDragOver(false)
        }}
        onDrop={(e) => {
          setDragOver(false)
          const files = Array.from(e.dataTransfer.files ?? [])
          if (!files.length || importDisabled) return
          e.preventDefault()
          onImport(files, group)
        }}
      >
        <header className="cl-head">
          <h2>{title}</h2>
          <span className="cl-grow" />
          <button
            type="button"
            className="cl-icon"
            aria-label="导入文件"
            title="导入图片、视频或音频"
            disabled={importDisabled}
            onClick={() => fileInput.current?.click()}
          >
            <Upload size={16} />
          </button>
          {!task && (
            <button
              type="button"
              className="cl-icon"
              aria-label="新增资产"
              title="新增文字或链接"
              disabled={disabled}
              onClick={onCreate}
            >
              <Plus size={17} />
            </button>
          )}
          {!task && (
            <button
              type="button"
              className="cl-icon"
              aria-label="管理分组与标签"
              title="标签与分类"
              onClick={() => setManager(true)}
            >
              <TagIcon size={15} />
            </button>
          )}
          {!standalone && (
            <>
              <button
                type="button"
                className="cl-icon"
                aria-label={expanded ? '收起资产管理' : '展开资产管理'}
                title={expanded ? '收窄' : '加宽'}
                onClick={() => setWidth(expanded ? 344 : 720)}
              >
                {expanded ? <Minimize size={15} /> : <Maximize size={15} />}
              </button>
              <button type="button" className="cl-icon" aria-label="收起资产库" title="收起" onClick={onClose}>
                <PanelLeftClose size={16} />
              </button>
            </>
          )}
          <input
            ref={fileInput}
            type="file"
            hidden
            multiple
            accept="image/jpeg,image/png,image/webp,video/mp4,video/webm,audio/mpeg,audio/wav"
            onChange={(e) => {
              const files = Array.from(e.target.files ?? [])
              e.target.value = ''
              if (files.length) onImport(files, group)
            }}
          />
        </header>
        <label className="cl-find">
          <Search size={14} />
          <input
            ref={queryInput}
            aria-label="搜索个人资产"
            placeholder={group ? `在「${scopeName}」中搜索` : '搜索名称、描述或标签'}
            value={queryDraft}
            onChange={(e) => setQueryDraft(e.target.value)}
            onCompositionStart={() => setComposing(true)}
            onCompositionEnd={() => setComposing(false)}
            onKeyDown={(e) => {
              if (e.key === 'Enter' && !e.nativeEvent.isComposing) filter('lq', queryDraft.trim())
              if (e.key === 'Escape' && queryDraft) {
                e.preventDefault()
                setQueryDraft('')
                filter('lq', '')
              }
            }}
          />
          {queryDraft && (
            <button
              type="button"
              aria-label="清除资产搜索"
              onClick={() => {
                setQueryDraft('')
                filter('lq', '')
                queryInput.current?.focus()
              }}
            >
              <X size={12} />
            </button>
          )}
        </label>
        <div className="cl-views" role="tablist" aria-label="资产视图">
          {VIEWS.filter((v) => !task || v.key !== 'trash').map((v) => (
            <button
              key={v.key}
              type="button"
              role="tab"
              aria-selected={view === v.key}
              onClick={() => scope(v.key)}
            >
              {v.label}
            </button>
          ))}
        </div>
        {!overview && (
          <div className="cl-crumb" data-drop-group={group || undefined}>
            <button
              type="button"
              className="cl-icon"
              aria-label="返回"
              onClick={() =>
                path.length > 1 ? scope('all', path.at(-2)!.id) : group ? scope('all') : view === 'all' ? clearFilters() : scope('all')
              }
            >
              <ChevronLeft size={14} />
            </button>
            <span className="cl-path">
              {path.length ? (
                path.map((g, i) => (
                  <span key={g.id}>
                    {i > 0 && <i>/</i>}
                    {i < path.length - 1 ? (
                      <button type="button" onClick={() => scope('all', g.id)}>
                        {g.name}
                      </button>
                    ) : renaming(g.id) ? (
                      nameInput(g.name, '重命名分组')
                    ) : (
                      <b>{g.name}</b>
                    )}
                  </span>
                ))
              ) : (
                <b>{scopeName}</b>
              )}
            </span>
            <em>{loading ? '检索中' : `${page.total_count} 项`}</em>
            {groupOf(group) && (
              <button
                type="button"
                className="cl-icon"
                aria-label={`分组操作：${groupOf(group)!.name}`}
                onClick={openGroupMenu(groupOf(group)!)}
              >
                <Ellipsis size={14} />
              </button>
            )}
          </div>
        )}
        <div className="cl-tools">
          {task?.kind === 'fill' ? (
            <span className="cl-tools-note">只显示{kindLabel[task.mediaKind]}</span>
          ) : (
            <button
              type="button"
              className={`cl-tool ${kindParam ? 'is-on' : ''}`}
              aria-label="资产类型"
              aria-haspopup="menu"
              onClick={(e) => {
                const r = e.currentTarget.getBoundingClientRect()
                setMenu({
                  x: r.left,
                  y: r.bottom + 4,
                  label: '资产类型',
                  items: KINDS.map(([k, label]) => ({
                    label,
                    icon: kindParam === k ? <Check size={14} /> : <span className="cl-menu-blank" />,
                    action: () => filter('lkind', k),
                  })),
                })
              }}
            >
              {kindParam ? kindLabel[kindParam] : '全部类型'}
              <ChevronDown size={12} />
            </button>
          )}
          <span className="cl-grow" />
          <button
            type="button"
            className={`cl-tool ${tagIDs.length ? 'is-on' : ''}`}
            aria-haspopup="dialog"
            aria-expanded={!!tagsOpen}
            onClick={(e) => setTagsOpen(tagsOpen ? null : e.currentTarget.getBoundingClientRect())}
          >
            <TagIcon size={13} />
            标签
            {tagIDs.length > 0 && <em>{tagIDs.length}</em>}
          </button>
          <button
            type="button"
            className="cl-tool"
            aria-label="资产排序"
            aria-haspopup="menu"
            onClick={(e) => {
              const r = e.currentTarget.getBoundingClientRect()
              setMenu({
                x: r.right - 160,
                y: r.bottom + 4,
                label: '资产排序',
                items: SORTS.map(([k, label]) => ({
                  label,
                  icon: sort === k ? <Check size={14} /> : <span className="cl-menu-blank" />,
                  action: () => filter('lsort', k === 'recent' ? '' : k),
                })),
              })
            }}
          >
            {SORTS.find(([k]) => k === sort)?.[1]}
            <ChevronDown size={12} />
          </button>
        </div>
        {tagIDs.length > 0 && (
          <div className="cl-active-tags">
            {tagIDs.map((id) => {
              const t = catalog.tags.find((x) => x.id === id)
              return (
                <button
                  key={id}
                  type="button"
                  aria-label={`移除标签筛选：${t?.name ?? '已删除标签'}`}
                  onClick={() => filter('ltags', tagIDs.filter((x) => x !== id).join(','))}
                >
                  <i style={{ background: t?.color }} />
                  {t?.name ?? '已删除标签'}
                  <X size={11} />
                </button>
              )
            })}
            {tagIDs.length > 1 && (
              <button type="button" className="cl-mode" onClick={() => filter('lmode', tagMode === 'all' ? 'any' : '')}>
                {tagMode === 'all' ? '同时满足' : '满足任一'}
              </button>
            )}
          </div>
        )}
        {(error || stale) && (
          <div role="alert" className="cl-error">
            <p>{error || '资产库已变化，请刷新当前列表。'}</p>
            <button type="button" className="btn" onClick={() => setRetry((v) => v + 1)}>
              刷新列表
            </button>
          </div>
        )}
        {notice && (
          <p className="cl-notice" role="status">
            {notice}
          </p>
        )}
        <div className="cl-shelf" ref={shelf} onScroll={() => setPeek(null)}>
          {overview ? (
            <>
              {addingUnder(null) ? (
                <div className="cl-groups-bar is-editing">
                  <Folder size={13} />
                  {nameInput('', '新建分组')}
                </div>
              ) : (
                <div className="cl-groups-bar">
                  <span>{roots.length} 个分组</span>
                  <button type="button" aria-label="新建分组" disabled={disabled} onClick={() => startNew(null)}>
                    <Plus size={13} />
                    新建分组
                  </button>
                </div>
              )}
              {roots.map((g) => section(g))}
              {section(null)}
              {!previewLoading && !roots.length && !Number(previews['']?.total_count ?? 0) && (
                <div className="cl-empty">
                  <Inbox size={22} />
                  <p>把灵感收进这里</p>
                  <small>导入图片、视频、音频，添加文字与链接，或把画布节点拖进来。</small>
                </div>
              )}
            </>
          ) : (
            <>
              {group && !keyword && subChips(group)}
              {page.items.length > 0 && <Masonry items={page.items} columns={columns} tile={(a) => tile(a)} />}
              {!loading && !error && !page.items.length && (
                <div className="cl-empty">
                  <Search size={22} />
                  <p>{emptyText}</p>
                  {narrowed && (
                    <button type="button" className="btn" onClick={clearFilters}>
                      清除条件
                    </button>
                  )}
                  {narrowed && (group || view !== 'all') && (
                    <button type="button" className="btn" onClick={() => scope('all', '', true)}>
                      在全部资产中查找
                    </button>
                  )}
                </div>
              )}
              {page.next_cursor && !stale && (
                <button type="button" className="btn cl-more" disabled={moreLoading} onClick={() => void more()}>
                  {moreLoading ? '正在加载…' : '加载更多资产'}
                </button>
              )}
            </>
          )}
        </div>
        {task?.kind === 'fill' ? (
          <footer className="cl-foot is-task">
            <span>点一份{kindLabel[task.mediaKind]}填进「{task.title}」</span>
            <span className="cl-grow" />
            <button type="button" className="btn" onClick={onClose}>
              取消
            </button>
          </footer>
        ) : task?.kind === 'pick' ? (
          <footer className="cl-foot is-task">
            <span>
              为「{task.title}」选参考{selection.length ? ` · 已选 ${selection.length}` : ''}
            </span>
            <span className="cl-grow" />
            <button type="button" className="btn" onClick={onClose}>
              取消
            </button>
            <button
              type="button"
              className="btn btn-primary"
              disabled={!selected.length || disabled}
              onClick={() => onPick(selected.filter((a) => !a.unavailable))}
            >
              添加{selected.length ? ` ${selected.length} 项` : ''}
            </button>
          </footer>
        ) : selected.length ? (
          <footer className="cl-foot is-task">
            <span>已选 {selected.length} 项</span>
            <span className="cl-grow" />
            {view !== 'trash' && (
              <button
                type="button"
                className="btn"
                aria-label="批量整理"
                disabled={disabled}
                onClick={() => edit('organize', { selected })}
              >
                整理
              </button>
            )}
            <button
              type="button"
              className="btn"
              disabled={disabled}
              onClick={() => batch(view === 'trash' ? 'restore' : 'trash')}
            >
              {view === 'trash' ? '恢复' : '移入回收站'}
            </button>
            {view === 'trash' && (
              <button type="button" className="btn" disabled={disabled} onClick={() => batch('purge')}>
                彻底删除
              </button>
            )}
            <button type="button" className="cl-icon" aria-label="取消选择" title="取消选择" onClick={() => setSelection([])}>
              <X size={15} />
            </button>
            {canPlace && view !== 'trash' && (
              <button
                type="button"
                className="btn btn-primary"
                disabled={disabled}
                onClick={() => {
                  onPlace(selected.filter((a) => !a.unavailable))
                  setSelection([])
                }}
              >
                放入画布
              </button>
            )}
          </footer>
        ) : (
          <footer className="cl-foot">
            {canPlace ? '拖入画布 · 空格预览 · ⌘ 点击多选' : '空格预览 · ⌘ 点击多选 · 拖入文件即导入'}
          </footer>
        )}
      </aside>
      {createPortal(
        <>
          {tagsOpen && (
            <TagPopover
              anchor={tagsOpen}
              catalog={catalog}
              selected={tagIDs}
              mode={tagMode}
              onToggle={(id) =>
                filter('ltags', (tagIDs.includes(id) ? tagIDs.filter((x) => x !== id) : [...tagIDs, id]).join(','))
              }
              onMode={() => filter('lmode', tagMode === 'all' ? 'any' : '')}
              onClear={() => filter('ltags', '')}
              onClose={() => setTagsOpen(null)}
            />
          )}
          {menu && (
            <CanvasContextMenu x={menu.x} y={menu.y} label={menu.label} items={menu.items} onClose={() => setMenu(null)} />
          )}
          {peek && !look && (
            <Peek asset={peek.asset} anchor={peek.el} meta={metaLine(peek.asset, catalog.groups, catalog.tags)} />
          )}
          {lookAsset && (
            <QuickLook
              asset={lookAsset}
              meta={metaLine(lookAsset, catalog.groups, catalog.tags)}
              canPlace={canPlace && view !== 'trash'}
              disabled={disabled}
              onClose={() => {
                const id = lookAsset.id
                setLook(null)
                shelf.current?.querySelector<HTMLElement>(`[data-asset="${id}"]`)?.focus()
              }}
              onStep={(delta) => {
                const ids = tileIDs()
                const next = ids[(ids.indexOf(lookAsset.id) + delta + ids.length) % ids.length]
                if (next) setLook(next)
              }}
              onPlace={() => {
                setLook(null)
                onPlace([lookAsset])
              }}
              onFavorite={() =>
                void onCommand(`/assets/${lookAsset.id}/metadata`, {
                  expected_revision: lookAsset.revision,
                  is_favorite: !lookAsset.is_favorite,
                })
              }
            />
          )}
          {manager && (
            <StudioDialog title="标签与分类" onClose={() => setManager(false)}>
              <section className="cl-manager" aria-label="目录管理">
                <div className="cc-actions">
                  <button className="btn" disabled={disabled} onClick={() => edit('group', { parent: group })}>
                    新建分组
                  </button>
                  <button className="btn" disabled={disabled} onClick={() => edit('tag')}>
                    新建标签
                  </button>
                  <button className="btn" disabled={disabled} onClick={() => edit('category')}>
                    新建分类
                  </button>
                  <button
                    className="btn"
                    disabled={disabled}
                    onClick={() => edit('settings', { revision: catalog.settings?.revision })}
                  >
                    回收站保留设置
                  </button>
                </div>
                {catalog.categories.map((c) => (
                  <div className="cl-manage-row" key={c.id}>
                    <span>{c.name}</span>
                    <button
                      aria-label={`编辑分类：${c.name}`}
                      onClick={() => edit('category', { id: c.id, name: c.name, revision: c.revision })}
                    >
                      <Pencil size={12} />
                    </button>
                    <button
                      aria-label={`删除分类：${c.name}`}
                      onClick={() =>
                        confirm(
                          `/tag-categories/${c.id}/delete`,
                          { expected_revision: c.revision, hierarchy_revision: catalog.hierarchy },
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
                    <small>{catalog.categories.find((c) => c.id === t.category_id)?.name ?? '未分类'}</small>
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
                          { expected_revision: t.revision, hierarchy_revision: catalog.hierarchy },
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
                void onCommand(confirmation.path, confirmation.payload).then((ok) => {
                  if (!ok) return
                  setConfirmation(null)
                  if (confirmation.path.startsWith('/assets/batch-')) setSelection([])
                  if (confirmation.path === `/asset-groups/${group}/delete`)
                    scope('all', groupOf(group)?.parent_id ?? '')
                })
              }}
            />
          )}
        </>,
        portalRoot,
      )}
    </>
  )
}

// 标签筛选弹层：按分类列出，色点 + 选中勾；继续输入即过滤。
function TagPopover({
  anchor,
  catalog,
  selected,
  mode,
  onToggle,
  onMode,
  onClear,
  onClose,
}: {
  anchor: DOMRect
  catalog: Catalog
  selected: string[]
  mode: string
  onToggle: (id: string) => void
  onMode: () => void
  onClear: () => void
  onClose: () => void
}) {
  const ref = useRef<HTMLDivElement>(null)
  const [q, setQ] = useState('')
  const close = useRef(onClose)
  close.current = onClose
  useLayoutEffect(() => {
    const el = ref.current
    if (!el) return
    el.style.left = `${Math.max(8, Math.min(anchor.right - el.offsetWidth, window.innerWidth - el.offsetWidth - 8))}px`
    el.style.top = `${Math.min(anchor.bottom + 4, window.innerHeight - el.offsetHeight - 8)}px`
  }, [anchor])
  useEffect(() => {
    const outside = (e: PointerEvent) => {
      if (e.target instanceof Node && !ref.current?.contains(e.target)) close.current()
    }
    document.addEventListener('pointerdown', outside, true)
    return () => document.removeEventListener('pointerdown', outside, true)
  }, [])
  const match = (t: api.Tag) => !q.trim() || t.name.toLowerCase().includes(q.trim().toLowerCase())
  const cats: { id: string | null; name: string }[] = [
    ...catalog.categories.map((c) => ({ id: c.id as string | null, name: c.name })),
    { id: null, name: '未分类' },
  ]
  return (
    <div
      ref={ref}
      className="cl-tag-pop"
      role="dialog"
      aria-label="标签筛选"
      onKeyDown={(e) => {
        e.stopPropagation()
        if (e.key === 'Escape') {
          e.preventDefault()
          onClose()
        }
      }}
    >
      <label className="cl-find">
        <Search size={13} />
        <input aria-label="筛选标签" placeholder="查找标签" value={q} autoFocus onChange={(e) => setQ(e.target.value)} />
      </label>
      <div className="cl-tag-list">
        {cats.map((c) => {
          const tags = catalog.tags.filter((t) => (t.category_id ?? null) === c.id && match(t))
          if (!tags.length) return null
          return (
            <div key={c.id ?? 'none'}>
              <div className="cl-tag-cat">{c.name}</div>
              {tags.map((t) => (
                <button
                  key={t.id}
                  type="button"
                  className="cl-tag-item"
                  aria-pressed={selected.includes(t.id)}
                  onClick={() => onToggle(t.id)}
                >
                  <i style={{ background: t.color }} />
                  <span>{t.name}</span>
                  {selected.includes(t.id) && <Check size={13} />}
                </button>
              ))}
            </div>
          )
        })}
        {!catalog.tags.some(match) && <p className="cl-sec-empty">{catalog.tags.length ? '没有匹配的标签' : '还没有标签'}</p>}
      </div>
      <div className="cl-tag-foot">
        <button type="button" onClick={onMode}>
          {mode === 'all' ? '同时满足全部所选' : '满足任一所选'}
        </button>
        <span className="cl-grow" />
        <button type="button" disabled={!selected.length} onClick={onClear}>
          清除
        </button>
      </div>
    </div>
  )
}
