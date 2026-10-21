import { useEffect, useRef, useState, type ReactNode } from 'react'
import type { components } from '../../api/schema'
import { ApiError } from '../../api/transport.ts'
import type {
  SaveModelRequest,
  SaveTarget,
} from '../../creative-scene/saveModel.ts'
import * as api from './api.ts'
import StudioDialog from './StudioDialog.tsx'

type Result = components['schemas']['CreativeAssetCreateResult']
type Phase = 'ready' | 'rendering' | 'saving' | 'refreshing' | 'blocked'

/**
 * 场景里的「存入资产库」：存的是打开弹框时摄影师看到的那一版模型和材质。
 * 那一版在此期间被改了（409），等编辑器显示出新版再重新判断，确认后再存。
 * 存入前按冻结的材质渲染卡片缩略图并暂存上传。
 */
export default function SaveModelDialog({
  request,
  groupField,
  groupName,
  onSaved,
  onClose,
}: {
  request: SaveModelRequest
  /** 归入分组的选择框，表单从 FormData 读 group。 */
  groupField: ReactNode
  groupName: (id: string) => string
  onSaved: (notice: string) => void
  onClose: () => void
}) {
  const [target, setTarget] = useState<SaveTarget>(request.target)
  const [title, setTitle] = useState(request.target.name)
  const touched = useRef(false)
  const [phase, setPhase] = useState<Phase>('ready')
  const [message, setMessage] = useState('')
  // 同一份请求重试沿用同一个键；请求内容变了（换了版本、改了标题或分组）就换新键。
  const attempt = useRef<{ body: string; key: string } | null>(null)
  // 同一版的缩略图只渲染上传一次，重试沿用；换了版本或上传过期才重做。
  const thumbnail = useRef<{ target: SaveTarget; id: string } | null>(null)
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
      } catch {
        if (signal.aborted) return
        setPhase('ready')
        setMessage('缩略图生成失败，没有存入，可以重试')
        return
      }
      if (signal.aborted) return
      thumbnail.current = { target, id: thumbnailID }
    }
    const payload: components['schemas']['CreativeAssetFromSceneModelPayload'] = {
      document_id: request.documentID,
      object_id: target.objectID,
      expected_object_rev: target.unitRev,
      thumbnail_upload_id: thumbnailID,
      target: { title: name, group_ids: groupID ? [groupID] : [] },
    }
    const key =
      attempt.current?.body === JSON.stringify(payload)
        ? attempt.current.key
        : crypto.randomUUID()
    attempt.current = { body: JSON.stringify(payload), key }
    setPhase('saving')
    try {
      const result = await api.send<Result>(
        account,
        '/assets/from-scene-model',
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
          ? `这个模型已在资产库中，已归入${where}`
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
      setMessage(saveError(e))
    }
  }

  async function refresh(currentRev: string | null) {
    setPhase('refreshing')
    setMessage('模型刚被修改，正在读取最新版本…')
    const signal = abort.current?.signal
    if (!signal) return
    const next = await request.refresh(currentRev ?? '0', signal)
    if (signal.aborted) return
    if ('error' in next) {
      setPhase('blocked')
      setMessage(`模型已被修改，现在不能存入：${next.error}`)
      return
    }
    setTarget(next)
    if (!touched.current) setTitle(next.name)
    attempt.current = null
    setPhase('ready')
    setMessage('模型已更新为最新版本，确认后再存')
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
        <p className="cc-muted">
          保存模型、参数和材质；位置、旋转和缩放不随资产保存。同一模型配同样的材质已在库中时只追加归类。
        </p>
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
function saveError(e: unknown): string {
  if (!(e instanceof ApiError)) return '存入失败，请检查网络后重试'
  if (e.code === 'creative_model_unavailable') return '这个模型不可用，不能存入'
  if (e.code === 'creative_state_conflict') return '场景在回收站，请先恢复'
  if (e.code === 'creative_upload_unavailable') return '缩略图上传已失效，请重试'
  if (e.status === 404) return '场景或分组已不存在，请关闭后重试'
  return '存入失败，请稍后重试'
}
