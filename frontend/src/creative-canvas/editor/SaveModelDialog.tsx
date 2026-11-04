import { useEffect, useRef, useState, type ReactNode } from 'react'
import type { components } from '../../api/schema'
import { ApiError } from '../../api/transport.ts'
import { ThumbnailError, type SaveRequest } from '../../creative-scene/saveModel.ts'
import * as api from './api.ts'
import StudioDialog from './StudioDialog.tsx'

type Result = components['schemas']['CreativeAssetCreateResult']
type ModelPayload = components['schemas']['CreativeAssetFromSceneModelPayload']
type MaterialPayload = components['schemas']['CreativeAssetFromSceneMaterialPayload']
/** 两种请求共有的字段。 */
type Common = Pick<ModelPayload & MaterialPayload, 'document_id' | 'object_id' | 'expected_object_rev' | 'thumbnail_upload_id' | 'target'>
type Phase = 'ready' | 'rendering' | 'saving' | 'refreshing' | 'blocked'
type Props = {
  request: SaveRequest
  /** 归入分组的选择框，表单从 FormData 读 group。 */
  groupField: ReactNode
  groupName: (id: string) => string
  onSaved: (notice: string) => void
  onClose: () => void
}

/**
 * 场景里的「存入资产库」：模型连同材质，或者一个槽位的材质（存为材质资产）。
 * 两者的弹框、冲突处理和缩略图做法相同，只是接口和说明不同。
 */
export default function SaveModelDialog({ request, ...rest }: Props) {
  return request.kind === 'material' ? (
    <SaveDialog
      {...rest}
      request={request}
      noun="材质"
      endpoint="/assets/from-scene-material"
      payload={(t, common): MaterialPayload => ({ ...common, slot_id: t.slotID })}
      note="保存这个槽位的材质参数、贴图和映射；用到别的槽位时复制一份，之后互不影响。同样的材质已在库中时只追加归类。"
    />
  ) : (
    <SaveDialog
      {...rest}
      request={request}
      noun="模型"
      endpoint="/assets/from-scene-model"
      payload={(_, common): ModelPayload => common}
      note="保存模型、参数和材质；位置、旋转和缩放不随资产保存。同一模型配同样的材质已在库中时只追加归类。"
    />
  )
}

type Target = { objectID: string; unitRev: string; name: string }

/**
 * 存的是打开弹框时摄影师看到的那一版。那一版在此期间被改了（409），等编辑器
 * 显示出新版再重新判断，确认后再存。存入前按冻结的内容渲染卡片缩略图并暂存上传。
 */
function SaveDialog<T extends Target>({
  request,
  noun,
  endpoint,
  payload: build,
  note,
  groupField,
  groupName,
  onSaved,
  onClose,
}: Omit<Props, 'request'> & {
  request: {
    documentID: string
    target: T
    thumbnail(target: T, signal: AbortSignal): Promise<string>
    refresh(currentRev: string, signal: AbortSignal): Promise<T | { error: string }>
  }
  noun: string
  endpoint: string
  /** 这一种的请求体：共有字段加上它独有的。 */
  payload: (target: T, common: Common) => ModelPayload | MaterialPayload
  note: string
}) {
  const [target, setTarget] = useState<T>(request.target)
  const [title, setTitle] = useState(request.target.name)
  const touched = useRef(false)
  const [phase, setPhase] = useState<Phase>('ready')
  const [message, setMessage] = useState('')
  // 同一份请求重试沿用同一个键；请求内容变了（换了版本、改了标题或分组）就换新键。
  const attempt = useRef<{ body: string; key: string } | null>(null)
  // 同一版的缩略图只渲染上传一次，重试沿用；换了版本或上传过期才重做。
  const thumbnail = useRef<{ target: T; id: string } | null>(null)
  // 在 effect 里成对创建和中止：StrictMode 挂载后会多跑一轮清理。
  const abort = useRef<AbortController | null>(null)
  useEffect(() => {
    const controller = new AbortController()
    abort.current = controller
    return () => controller.abort()
  }, [])

  async function save(groupID: string, redone = false) {
    const name = title.trim()
    if (!name) {
      setMessage('请填写资产名称')
      return
    }
    const account = api.currentAccount()
    const signal = abort.current?.signal
    if (!account || !signal) return
    setMessage('')
    let thumbnailID = thumbnail.current?.target === target ? thumbnail.current.id : ''
    if (!thumbnailID) {
      setPhase('rendering')
      try {
        thumbnailID = await request.thumbnail(target, signal)
      } catch (err) {
        if (signal.aborted) return
        setPhase('ready')
        setMessage(err instanceof ThumbnailError ? `缩略图生成失败：${err.message}，没有存入，可以重试` : '缩略图生成失败，没有存入，可以重试')
        return
      }
      if (signal.aborted) return
      thumbnail.current = { target, id: thumbnailID }
    }
    const payload = build(target, {
      document_id: request.documentID,
      object_id: target.objectID,
      expected_object_rev: target.unitRev,
      thumbnail_upload_id: thumbnailID,
      target: { title: name, group_ids: groupID ? [groupID] : [] },
    })
    const key =
      attempt.current?.body === JSON.stringify(payload)
        ? attempt.current.key
        : crypto.randomUUID()
    attempt.current = { body: JSON.stringify(payload), key }
    setPhase('saving')
    try {
      const result = await api.send<Result>(
        account,
        endpoint,
        JSON.stringify({
          operation_id: key,
          client_created_at: new Date().toISOString(),
          payload,
        }),
        key,
      )
      const where = groupID ? `「${groupName(groupID)}」` : '资产库'
      onSaved(
        result.reused
          ? `这个${noun}已在资产库中，已归入${where}`
          : `已存入${where}`,
      )
      onClose()
    } catch (e) {
      const conflict =
        e instanceof ApiError && e.code === 'creative_document_conflict'
          ? e.details
          : undefined
      if (conflict && 'conflicts' in conflict) {
        await refresh(conflict.conflicts[0]?.current_rev ?? null)
        return
      }
      // 缩略图上传已过期：重新渲染上传，自动重做一次。
      if (e instanceof ApiError && e.code === 'creative_upload_unavailable' && !redone) {
        thumbnail.current = null
        await save(groupID, true)
        return
      }
      setPhase('ready')
      setMessage(saveError(e, noun))
    }
  }

  async function refresh(currentRev: string | null) {
    setPhase('refreshing')
    setMessage(`${noun}刚被修改，正在读取最新版本…`)
    const signal = abort.current?.signal
    if (!signal) return
    const next = await request.refresh(currentRev ?? '0', signal)
    if (signal.aborted) return
    if ('error' in next) {
      setPhase('blocked')
      setMessage(`${noun}已被修改，现在不能存入：${next.error}`)
      return
    }
    setTarget(next)
    if (!touched.current) setTitle(next.name)
    attempt.current = null
    setPhase('ready')
    setMessage(`${noun}已更新为最新版本，确认后再存`)
  }

  return (
    <StudioDialog title="存入个人资产库" onClose={onClose}>
      <form
        className="cc-form"
        noValidate
        onSubmit={(e) => {
          e.preventDefault()
          if (phase !== 'ready') return
          void save(String(new FormData(e.currentTarget).get('group') ?? ''))
        }}
      >
        <p className="cc-muted">{note}</p>
        <label>
          资产名称
          <input
            className="input"
            name="title"
            value={title}
            maxLength={200}
            autoFocus
            onChange={(e) => {
              touched.current = true
              setTitle(e.target.value)
            }}
          />
        </label>
        <label>
          归入分组
          {groupField}
        </label>
        {message && (
          <p className="cc-muted" role="status">
            {message}
          </p>
        )}
        <button className="btn btn-primary" disabled={phase !== 'ready'}>
          {phase === 'rendering'
            ? '正在生成缩略图…'
            : phase === 'saving'
              ? '正在存入…'
              : '存入'}
        </button>
      </form>
    </StudioDialog>
  )
}

// 不暴露错误原文：只说摄影师能做什么。
function saveError(e: unknown, noun: string): string {
  if (!(e instanceof ApiError)) return '存入失败，请检查网络后重试'
  if (e.code === 'creative_model_unavailable') return '这个模型不可用，不能存入'
  if (e.code === 'creative_material_slot_empty') return '这个槽位已没有指定材质，不能存入'
  if (e.code === 'creative_image_unavailable') return `${noun}里有图片已不可用，换图后再存`
  if (e.code === 'creative_state_conflict') return '场景在回收站，请先恢复'
  if (e.code === 'creative_upload_unavailable') return '缩略图上传已失效，请重试'
  if (e.status === 404) return '场景或分组已不存在，请关闭后重试'
  return '存入失败，请稍后重试'
}
