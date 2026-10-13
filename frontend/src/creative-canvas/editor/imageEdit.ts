import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { read, send, type CanvasNode, type Content } from './api.ts'
import {
  fitRatio,
  isWholeImage,
  outcomeSummary,
  ratioValue,
  roundBox,
  runImageEdit,
  sourceSize,
  wholeImage,
  type Box,
  type CropRatio,
  type EditPhase,
  type EditTransport,
  type ImageEditApplyResult,
  type ImageEditCapabilities,
  type ImageEditRender,
  type Outcome,
  type Size,
} from './imageEditModel.ts'

// 图片编辑的网络适配：把 imageEditModel 的流程接到真实接口上。

let capabilities: Promise<ImageEditCapabilities | null> | null = null

/** 部署的开关与静态策略；读不到时当作未开启，入口不展示。 */
export function useImageEditCapabilities(): ImageEditCapabilities | null {
  const [value, setValue] = useState<ImageEditCapabilities | null>(null)
  useEffect(() => {
    let live = true
    capabilities ??= read<ImageEditCapabilities>('/image-edit-capabilities').catch(() => {
      // 下次挂载再试，不把一次网络失败记成永久关闭。
      capabilities = null
      return null
    })
    void capabilities.then((c) => {
      if (live) setValue(c)
    })
    return () => {
      live = false
    }
  }, [])
  return value
}

export function imageEditTransport(account: string): EditTransport {
  return {
    uuid: () => crypto.randomUUID(),
    sleep: (ms, signal) =>
      new Promise((resolve) => {
        const timer = setTimeout(resolve, ms)
        signal.addEventListener(
          'abort',
          () => {
            clearTimeout(timer)
            resolve()
          },
          { once: true },
        )
      }),
    create: (body) =>
      send<ImageEditRender>(account, '/image-edit-renders', JSON.stringify(body), body.operation_id),
    get: (id) => read<ImageEditRender>(`/image-edit-renders/${encodeURIComponent(id)}`),
    discard: (id) =>
      send<ImageEditRender>(account, `/image-edit-renders/${encodeURIComponent(id)}/discard`, '{}', crypto.randomUUID()),
    // ApiError 带状态码和错误码；断网等错误没有状态码，流程当作结果未知。
    apply: (canvasID, nodeID, body) =>
      send<ImageEditApplyResult>(
        account,
        `/canvases/${encodeURIComponent(canvasID)}/nodes/${encodeURIComponent(nodeID)}/image-edits/apply`,
        JSON.stringify(body),
        body.operation_id,
      ),
  }
}

/** 一次「应用」的执行者：可取消，关闭界面后仍会把已发出的采用确认完。 */
export function useEditRun(account: string, onSettled: (o: Outcome) => void) {
  const [phase, setPhase] = useState<EditPhase | null>(null)
  const abort = useRef<AbortController | null>(null)
  const settled = useRef(onSettled)
  settled.current = onSettled
  const committing = useRef(false)
  const live = useRef(true)
  useEffect(() => {
    live.current = true
    return () => {
      live.current = false
      // 离开时还在编码就放弃；采用已发出的让它确认完。
      if (!committing.current) abort.current?.abort()
    }
  }, [])
  const run = useCallback(
    (req: Parameters<typeof runImageEdit>[1]) => {
      const controller = new AbortController()
      abort.current = controller
      committing.current = false
      void runImageEdit(imageEditTransport(account), req, controller.signal, (p) => {
        committing.current = p !== 'encoding'
        if (live.current) setPhase(p)
      }).then((o) => {
        if (abort.current === controller) abort.current = null
        committing.current = false
        if (live.current) setPhase(null)
        if (o.kind !== 'cancelled') settled.current(o)
      })
    },
    [account],
  )
  /** 还在编码时取消并丢弃；返回 false 表示采用已发出，取消不了。 */
  const cancel = useCallback(() => {
    if (committing.current) return false
    abort.current?.abort()
    return true
  }, [])
  return { phase, run, cancel }
}

export type CropSession = {
  nodeID: string
  /** 进入裁切时固定的源图修订。 */
  source: Content
  size: Size
  box: Box
  ratio: CropRatio
  phase: EditPhase | null
  error: { message: string; busyRenderID?: string } | null
}

/** 画布内裁切的会话：进入、调整、取消、应用。同一时间只裁一张图。 */
export function useImageCrop({
  account,
  canvasID,
  dataRevision,
  onNotice,
}: {
  account: string
  canvasID: string
  dataRevision: (nodeID: string) => string
  onNotice: (message: string) => void
}) {
  const [session, setSession] = useState<CropSession | null>(null)
  const current = useRef(session)
  current.current = session
  const { phase, run, cancel } = useEditRun(account, (o) => {
    const s = current.current
    if (o.kind === 'applied') {
      setSession(null)
      onNotice(outcomeSummary(o, 'crop'))
      return
    }
    if (!s) {
      // 裁切界面已经退出：结果只能用提示告诉摄影师。
      onNotice(outcomeSummary(o, 'crop'))
      return
    }
    setSession({ ...s, error: { message: outcomeSummary(o, 'crop'), busyRenderID: o.kind === 'failed' ? o.busyRenderID : undefined } })
  })
  const start = useCallback((node: CanvasNode) => {
    const size = sourceSize(node.content)
    if (!node.content || !size) return
    setSession({ nodeID: node.id, source: node.content, size, box: wholeImage(size), ratio: 'free', phase: null, error: null })
  }, [])
  const exit = useCallback(() => {
    if (phase && !cancel()) {
      onNotice('裁切结果正在确认，完成后会提示')
    }
    setSession(null)
  }, [phase, cancel, onNotice])
  const setBox = useCallback((box: Box) => setSession((s) => (s ? { ...s, box, error: null } : s)), [])
  const setRatio = useCallback(
    (ratio: CropRatio) =>
      setSession((s) => {
        if (!s) return s
        const r = ratioValue(ratio, s.size)
        return { ...s, ratio, box: r === null ? s.box : fitRatio(s.box, r, s.size), error: null }
      }),
    [],
  )
  const reset = useCallback(() => setSession((s) => (s ? { ...s, ratio: 'free', box: wholeImage(s.size), error: null } : s)), [])
  const apply = useCallback(() => {
    const s = current.current
    if (!s || phase || isWholeImage(s.box, s.size)) return
    setSession({ ...s, error: null })
    run({
      canvasID,
      nodeID: s.nodeID,
      sourceRevisionID: s.source.id,
      action: 'crop',
      parameters: roundBox(s.box, s.size),
      dataRevision: () => dataRevision(s.nodeID),
    })
  }, [phase, run, canvasID, dataRevision])
  const discardBusy = useCallback(
    (id: string) => {
      void imageEditTransport(account)
        .discard(id)
        .catch(() => undefined)
        .then(apply)
    },
    [account, apply],
  )
  // 同一份会话对象一直传到节点数据里：只在内容变化时换新，避免画布节点反复重算。
  const view = useMemo(() => session && { ...session, phase }, [session, phase])
  return { session: view, start, exit, setBox, setRatio, reset, apply, discardBusy }
}
