import { useEffect, useRef, useState, type PointerEvent } from 'react'
import { ChevronLeft } from 'lucide-react'
import { readScene } from '../../creative-scene/api.ts'
import { listClick, type Selection } from '../../creative-scene/input/selection.ts'
import type { SceneRefDraft } from './promptReferences.ts'
import {
  dragSelection,
  edgeScroll,
  pickedObjects,
  pickerRows,
  type PickerObject,
} from './scenePickerModel.ts'

const maxObjects = 50
const kindIcon: Record<string, string> = {
  group: '▤',
  light: '✦',
  camera: '◉',
  model_instance: '◆',
  volume: '☁',
}

export type SceneObjects = { title: string; objects: PickerObject[] }
// 同一次打开菜单里展开过的场景不再重读。
export type SceneObjectCache = Map<string, Promise<SceneObjects>>

function loadObjects(cache: SceneObjectCache, documentID: string) {
  let load = cache.get(documentID)
  if (!load) {
    load = readScene(documentID).then((doc) => ({
      title: doc.title,
      objects: doc.scene.objects.map((o) => ({
        id: o.id,
        name: o.name,
        kind: o.kind,
        parent_id: o.parent_id,
        order: o.order,
      })),
    }))
    // 读失败不留在缓存里，点「重试」能重新读。
    load.catch(() => cache.delete(documentID))
    cache.set(documentID, load)
  }
  return load
}

// 场景的物件列表：单击、Cmd/Ctrl 增减、Shift 范围，按住在列表里拖过连续多行。
// 一个都不选（或选「整个场景」）就是引用整个场景。
export function ScenePicker({
  documentID,
  title,
  initial = [],
  cache,
  onBack,
  onCancel,
  onConfirm,
}: {
  documentID: string
  title: string
  initial?: string[]
  cache: SceneObjectCache
  onBack?: () => void
  onCancel: () => void
  onConfirm: (ref: SceneRefDraft) => void
}) {
  const [scene, setScene] = useState<SceneObjects | null>(null)
  const [failed, setFailed] = useState('')
  const [attempt, setAttempt] = useState(0)
  const [query, setQuery] = useState('')
  const [selection, setSelection] = useState<Selection>({
    selected: initial,
    active: initial.at(-1) ?? null,
  })
  const list = useRef<HTMLDivElement>(null)
  const drag = useRef<{
    anchor: string
    base: Selection
    additive: boolean
    moved: boolean
    y: number
    x: number
    frame: number
  } | null>(null)

  useEffect(() => {
    let live = true
    setFailed('')
    loadObjects(cache, documentID).then(
      (value) => {
        if (live) setScene(value)
      },
      (e: unknown) => {
        if (live) setFailed(e instanceof Error ? e.message : '物件列表没有读出来')
      },
    )
    return () => {
      live = false
    }
  }, [cache, documentID, attempt])
  useEffect(() => () => cancelAnimationFrame(drag.current?.frame ?? 0), [])

  const rows = scene ? pickerRows(scene.objects, query) : []
  // 重新编辑时，原来引用的物件可能已经不在场景里：照样列出、保持选中，
  // 摄影师可以取消它；不取消就原样带上，不会悄悄变成整个场景。
  const known = new Set(scene?.objects.map((o) => o.id))
  const missing = scene ? initial.filter((id) => !known.has(id)) : []
  const order = [
    ...missing,
    ...rows.filter((row) => row.matched).map((row) => row.object.id),
  ]
  const count = selection.selected.length

  function confirm() {
    if (!scene || count > maxObjects) return
    const picked = pickedObjects(selection, scene.objects)
    const gone = missing.filter((id) => selection.selected.includes(id))
    onConfirm({
      documentID,
      title: scene.title || title,
      objectIDs: [...picked.map((o) => o.id), ...gone],
      names: [...picked.map((o) => o.name), ...gone.map(() => '')],
    })
  }
  function rowAt(x: number, y: number): string | null {
    const el = document.elementFromPoint(x, y)?.closest<HTMLElement>('[data-object]')
    return el && list.current?.contains(el) && el.dataset.matched === 'true'
      ? (el.dataset.object ?? null)
      : null
  }
  function dragTo(x: number, y: number) {
    const d = drag.current
    if (!d) return
    const id = rowAt(x, y)
    if (!id) return
    if (id !== d.anchor) d.moved = true
    if (d.moved) setSelection(dragSelection(d.base, d.anchor, id, order, d.additive))
  }
  // 拖到上下边缘时自动滚，滚的同时按指针所在的行继续扩选。
  function scrollLoop() {
    const d = drag.current
    const el = list.current
    if (!d || !el) return
    const rect = el.getBoundingClientRect()
    const step = edgeScroll(d.y, rect.top, rect.bottom)
    if (step) {
      el.scrollTop += step
      dragTo(d.x, Math.min(Math.max(d.y, rect.top + 1), rect.bottom - 1))
    }
    d.frame = requestAnimationFrame(scrollLoop)
  }
  function onPointerDown(e: PointerEvent<HTMLDivElement>) {
    if (e.button !== 0 || e.shiftKey) return
    const id = rowAt(e.clientX, e.clientY)
    if (!id) return
    const additive = e.metaKey || e.ctrlKey
    drag.current = {
      anchor: id,
      base: additive ? selection : { selected: [], active: null },
      additive,
      moved: false,
      x: e.clientX,
      y: e.clientY,
      frame: requestAnimationFrame(scrollLoop),
    }
    e.currentTarget.setPointerCapture(e.pointerId)
  }
  function onPointerMove(e: PointerEvent<HTMLDivElement>) {
    const d = drag.current
    if (!d) return
    d.x = e.clientX
    d.y = e.clientY
    dragTo(e.clientX, e.clientY)
  }
  function onPointerUp(e: PointerEvent<HTMLDivElement>) {
    const d = drag.current
    drag.current = null
    if (d) cancelAnimationFrame(d.frame)
    if (d?.moved) return
    const id = rowAt(e.clientX, e.clientY)
    if (!id) return
    setSelection((old) =>
      listClick(old, id, order, { mod: e.metaKey || e.ctrlKey, shift: e.shiftKey }),
    )
  }

  return (
    <div className="cc-scene-picker">
      <header>
        {onBack && (
          <button type="button" aria-label="返回" title="返回" onClick={onBack}>
            <ChevronLeft size={14} />
          </button>
        )}
        <b>{scene?.title || title || '未命名场景'}</b>
      </header>
      <input
        type="search"
        className="cc-prompt-search"
        aria-label="搜索物件"
        placeholder="搜索物件"
        value={query}
        autoFocus
        onChange={(e) => setQuery(e.target.value)}
        onKeyDown={(e) => {
          e.stopPropagation()
          if (e.nativeEvent.isComposing) return
          if (e.key === 'Enter') {
            e.preventDefault()
            confirm()
          } else if (e.key === 'Escape') {
            e.preventDefault()
            onCancel()
          } else if (e.key === 'ArrowLeft' && !query && onBack) {
            e.preventDefault()
            onBack()
          }
        }}
      />
      {failed ? (
        <p role="alert">
          {failed}
          <button type="button" onClick={() => setAttempt((n) => n + 1)}>
            重试
          </button>
        </p>
      ) : !scene ? (
        <p role="status">正在读取物件…</p>
      ) : (
        <div
          ref={list}
          className="cc-scene-picker-list"
          role="listbox"
          aria-label="场景里的物件"
          aria-multiselectable="true"
          onPointerDown={onPointerDown}
          onPointerMove={onPointerMove}
          onPointerUp={onPointerUp}
          onPointerCancel={() => {
            cancelAnimationFrame(drag.current?.frame ?? 0)
            drag.current = null
          }}
        >
          <button
            type="button"
            role="option"
            className="cc-scene-picker-whole"
            aria-selected={count === 0}
            onClick={() => setSelection({ selected: [], active: null })}
          >
            整个场景
          </button>
          {missing.map((id) => (
            <div
              key={id}
              role="option"
              data-object={id}
              data-matched="true"
              data-missing="true"
              aria-selected={selection.selected.includes(id)}
              title={id}
              style={{ paddingLeft: 8 }}
            >
              <span className="cc-mention-icon">⊘</span>
              <span>已不在场景里的物件</span>
            </div>
          ))}
          {rows.map(({ object, depth, matched }) => (
            <div
              key={object.id}
              role="option"
              data-object={object.id}
              data-matched={matched}
              aria-selected={selection.selected.includes(object.id)}
              aria-disabled={!matched || undefined}
              style={{ paddingLeft: 8 + depth * 14 }}
            >
              <span className="cc-mention-icon">{kindIcon[object.kind] ?? '▣'}</span>
              <span>{object.name || '未命名'}</span>
            </div>
          ))}
          {!rows.length && <p>没有匹配的物件</p>}
        </div>
      )}
      <footer>
        <span role="status">
          {count === 0
            ? '整个场景'
            : count > maxObjects
              ? `一次最多引用 ${maxObjects} 个物件`
              : `已选 ${count} 个物件`}
        </span>
        <button
          type="button"
          disabled={!scene || count > maxObjects}
          onClick={confirm}
        >
          插入
        </button>
      </footer>
    </div>
  )
}
