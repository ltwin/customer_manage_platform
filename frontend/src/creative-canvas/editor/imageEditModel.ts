import type { components } from '../../api/schema'

// 图片编辑一期（V6-076）：裁切几何、格式说明与「创建候选 → 等待 → 同步采用」流程。
// 这里不碰网络和 DOM，界面与测试共用；请求由调用方注入。

export type ImageEditCapabilities =
  components['schemas']['CreativeImageEditCapabilities']
export type ImageEditRender = components['schemas']['CreativeImageEditRender']
export type ImageEditApplyResult =
  components['schemas']['CreativeImageEditApplyResult']
type CreateBody = components['schemas']['CreateCreativeImageEditRenderRequest']
type ContentRevision = components['schemas']['CreativeContentRevision']
type ApplyBody = components['schemas']['ApplyCreativeImageEditRequest']

export type Size = { width: number; height: number }
/** 方向校正后的源图像素坐标；拖动中允许小数，应用前取整。 */
export type Box = { left: number; top: number; width: number; height: number }

export const qualityPresets = [
  { label: '高质量', value: 90 },
  { label: '均衡', value: 80 },
  { label: '小文件', value: 60 },
] as const
export const qualityDefault = 80

export const cropRatios = [
  { key: 'free', label: '自由' },
  { key: 'original', label: '原图比例' },
  { key: '1:1', label: '1:1' },
  { key: '4:3', label: '4:3' },
  { key: '3:4', label: '3:4' },
  { key: '16:9', label: '16:9' },
  { key: '9:16', label: '9:16' },
] as const
export type CropRatio = (typeof cropRatios)[number]['key']

/** 宽 / 高；自由比例为 null。 */
export function ratioValue(ratio: CropRatio, source: Size): number | null {
  if (ratio === 'free') return null
  if (ratio === 'original') return source.width / source.height
  const [w, h] = ratio.split(':').map(Number)
  return w / h
}

export const wholeImage = (source: Size): Box => ({
  left: 0,
  top: 0,
  width: source.width,
  height: source.height,
})

const clamp = (v: number, lo: number, hi: number) =>
  Math.min(Math.max(v, lo), hi)

/** 切换比例：保持选区中心，取源图内能放下的最大选区。 */
export function fitRatio(box: Box, ratio: number, source: Size): Box {
  let width = source.width
  let height = width / ratio
  if (height > source.height) {
    height = source.height
    width = height * ratio
  }
  const cx = box.left + box.width / 2
  const cy = box.top + box.height / 2
  return {
    left: clamp(cx - width / 2, 0, source.width - width),
    top: clamp(cy - height / 2, 0, source.height - height),
    width,
    height,
  }
}

export function moveBox(start: Box, dx: number, dy: number, source: Size): Box {
  return {
    ...start,
    left: clamp(start.left + dx, 0, source.width - start.width),
    top: clamp(start.top + dy, 0, source.height - start.height),
  }
}

/** 拖动的是哪条边或哪个角：n/s/e/w 及其组合。 */
export type Handle = 'n' | 's' | 'e' | 'w' | 'ne' | 'nw' | 'se' | 'sw'
export const handles: readonly Handle[] = ['nw', 'n', 'ne', 'e', 'se', 's', 'sw', 'w']

/** 选区不小于 1 个源像素（产品稿 §5.1）。 */
const minEdge = 1

/**
 * 拖动边或角。对边/对角固定不动；固定比例时以拖动方向为主轴推出另一边，
 * 并整体限制在图片内，不会因越界而变形。
 */
export function resizeBox(
  start: Box,
  handle: Handle,
  dx: number,
  dy: number,
  ratio: number | null,
  source: Size,
): Box {
  const right = start.left + start.width
  const bottom = start.top + start.height
  const west = handle.includes('w')
  const east = handle.includes('e')
  const north = handle.includes('n')
  const south = handle.includes('s')
  // 可伸展的最大宽高：从固定的那一侧量到图片边缘。
  const maxW = west ? right : east ? source.width - start.left : source.width
  const maxH = north ? bottom : south ? source.height - start.top : source.height
  let width = west ? start.width - dx : east ? start.width + dx : start.width
  let height = north ? start.height - dy : south ? start.height + dy : start.height
  if (ratio === null) {
    width = clamp(width, minEdge, maxW)
    height = clamp(height, minEdge, maxH)
  } else {
    const horizontal = west || east
    const vertical = north || south
    // 角：取变化更大的一边为准；边：拖哪边就以哪边为准。
    const byWidth =
      horizontal &&
      (!vertical || Math.abs(width - start.width) / start.width >= Math.abs(height - start.height) / start.height)
    // 只拖上下边时，宽度以中线为轴向两侧伸展；只拖左右边时同理。
    const limitW = vertical && !horizontal ? 2 * Math.min(start.left + start.width / 2, source.width - start.left - start.width / 2) : maxW
    const limitH = horizontal && !vertical ? 2 * Math.min(start.top + start.height / 2, source.height - start.top - start.height / 2) : maxH
    const most = Math.min(limitW, limitH * ratio)
    const least = Math.max(minEdge, minEdge * ratio)
    width = clamp(byWidth ? width : height * ratio, least, most)
    height = width / ratio
  }
  const cx = start.left + start.width / 2
  const cy = start.top + start.height / 2
  const left = west ? right - width : east ? start.left : cx - width / 2
  const top = north ? bottom - height : south ? start.top : cy - height / 2
  return { left, top, width, height }
}

/** 应用时的整数像素框：四边各自取整，结果仍在图片内且至少 1 像素。 */
export function roundBox(box: Box, source: Size): Box {
  const left = clamp(Math.round(box.left), 0, source.width - 1)
  const top = clamp(Math.round(box.top), 0, source.height - 1)
  const right = clamp(Math.round(box.left + box.width), left + 1, source.width)
  const bottom = clamp(Math.round(box.top + box.height), top + 1, source.height)
  return { left, top, width: right - left, height: bottom - top }
}

export function isWholeImage(box: Box, source: Size): boolean {
  const r = roundBox(box, source)
  return r.left === 0 && r.top === 0 && r.width === source.width && r.height === source.height
}

export function formatLabel(mime: string): string {
  return (
    { 'image/jpeg': 'JPEG', 'image/png': 'PNG', 'image/webp': 'WebP' }[mime] ??
    (mime.split('/')[1]?.toUpperCase() || '未知格式')
  )
}

/** 压缩前说明会输出什么格式（产品稿 §4.3）；PNG 要等编码时才知道有没有透明像素。 */
export function compressOutputNote(mime: string): string {
  switch (mime) {
    case 'image/jpeg':
      return '输出 JPEG'
    case 'image/png':
      return 'PNG 不含透明像素时转 JPEG，含透明像素时转 WebP（保留透明）'
    case 'image/webp':
      return '输出 WebP，保留透明'
  }
  return '该格式不支持压缩'
}

export function cropOutputNote(mime: string): string {
  switch (mime) {
    case 'image/jpeg':
      return '输出高质量 JPEG'
    case 'image/png':
      return '输出 PNG，保留透明'
    case 'image/webp':
      return '输出无损 WebP，保留透明'
  }
  return '该格式不支持裁切'
}

export function formatBytes(n: number): string {
  if (n < 1024) return `${n} B`
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(n < 10 * 1024 ? 1 : 0)} KB`
  return `${(n / 1024 / 1024).toFixed(n < 10 * 1024 * 1024 ? 2 : 1)} MB`
}

/** (原始 − 输出) / 原始，一位小数；依据实际字节数。 */
export function reductionPercent(source: number, output: number): number {
  if (source <= 0) return 0
  return Math.round(((source - output) / source) * 1000) / 10
}

/** 编辑入口的状态（产品稿 §3.1）。hidden 时不展示入口。 */
export type EditGate =
  | { hidden: true }
  | { hidden: false; disabled: false }
  | { hidden: false; disabled: true; reason: string }

export function imageEditGate(input: {
  capabilities: ImageEditCapabilities | null
  typeKey: string
  hasContent: boolean
  single: boolean
  /** 本机文件还在上传或服务器图片还没加载出来。 */
  preparing: boolean
  readOnly: boolean
  generating: boolean
  sourceMime: string | null
  unavailable: boolean
}): EditGate {
  if (!input.capabilities?.available || input.typeKey !== 'core.image' || !input.single || !input.hasContent)
    return { hidden: true }
  const off = (reason: string): EditGate => ({ hidden: false, disabled: true, reason })
  if (input.readOnly) return off('画布只读，不能编辑图片')
  if (input.preparing) return off('正在准备图片')
  if (input.unavailable || !input.sourceMime) return off('原文件无法读取')
  if (!input.capabilities.input_mime_types.includes(input.sourceMime))
    return off(`${formatLabel(input.sourceMime)} 格式暂不支持编辑`)
  if (input.generating) return off('节点正在生成，完成后再编辑')
  return { hidden: false, disabled: false }
}

// ---- 创建候选 → 等待 → 同步采用 ----

export type EditTransport = {
  create(body: CreateBody): Promise<ImageEditRender>
  get(id: string): Promise<ImageEditRender>
  discard(id: string): Promise<ImageEditRender>
  apply(canvasID: string, nodeID: string, body: ApplyBody): Promise<ImageEditApplyResult>
  sleep(ms: number, signal: AbortSignal): Promise<void>
  uuid(): string
}

/** 请求失败的形状：有 status 表示服务端明确拒绝，没有表示结果未知（断网、超时）。 */
export type EditFailure = { status?: number; code?: string; message: string; renderID?: string }

export type EditPhase = 'encoding' | 'applying' | 'confirming'

export type EditOutcome =
  | { kind: 'applied'; render: ImageEditRender; result?: ImageEditApplyResult }
  | { kind: 'not_reduced'; sourceSize?: number }
  | { kind: 'failed'; message: string; busyRenderID?: string }
  | { kind: 'cancelled' }
  /** 采用请求已发出但结果未知：不能说成功，也不能说没应用。 */
  | { kind: 'unconfirmed'; renderID: string }

export type EditRequest = {
  canvasID: string
  nodeID: string
  sourceRevisionID: string
  action: 'compress' | 'crop'
  parameters: { quality: number } | Box
  /** 采用时读取节点当前的数据版本；标题等元数据的改动不应让结果作废。 */
  dataRevision: () => components['schemas']['CreativeRevision']
}

const pollInterval = 1000
// 渲染最多 2 分钟（后端 renderDeadline），多等一点让过期由服务端判定。
const pollLimit = 150
const confirmLimit = 5

export function failureMessage(f: EditFailure): string {
  switch (f.code) {
    case 'creative_image_edit_source_changed':
      return '图片已变化，结果尚未应用。请以当前内容重新编辑'
    case 'creative_image_edit_node_busy':
      return f.renderID ? '这张图片还有一项未完成的编辑' : '节点正在生成，完成后再编辑'
    case 'creative_image_edit_busy':
      return '图片编辑任务较多，请稍后重试'
    case 'creative_image_edit_expired':
      return '编辑结果已过期，请重新应用'
    case 'creative_quota_exceeded':
      return '存储空间不足，结果没有应用。释放空间后再试'
    case 'creative_image_edit_unavailable':
      return '图片编辑暂不可用'
    case 'creative_image_edit_colour_unknown':
      return '图片的色彩信息无法可靠识别，没有处理'
  }
  return f.message || '处理失败，请重试'
}

export function renderFailure(code: string | undefined): string {
  switch (code) {
    case 'unsupported':
      return '图片格式不支持编辑'
    case 'colour_unknown':
      return '图片的色彩信息无法可靠识别，没有处理'
    case 'too_large':
      return '图片超出可处理的尺寸或大小上限'
    case 'invalid_parameters':
      return '裁切范围超出图片，请重新选择'
  }
  return '处理失败，原图没有改动，可以重试'
}

const asFailure = (e: unknown): EditFailure => {
  const x = e as { status?: unknown; code?: unknown; message?: unknown; details?: { render_id?: unknown } }
  return {
    status: typeof x?.status === 'number' ? x.status : undefined,
    code: typeof x?.code === 'string' ? x.code : undefined,
    message: typeof x?.message === 'string' ? x.message : '',
    renderID: typeof x?.details?.render_id === 'string' ? x.details.render_id : undefined,
  }
}
// 4xx 是服务端明确的拒绝；5xx 和网络错误不知道是否生效。
const known = (f: EditFailure) => f.status !== undefined && f.status < 500

/**
 * 点击「应用」后的完整流程。等待期间取消（关闭面板、取消裁切、离开页面）
 * 只丢弃候选，不会在后台采用；采用一旦发出，只查原操作，不再重新编码。
 */
export async function runImageEdit(
  t: EditTransport,
  req: EditRequest,
  signal: AbortSignal,
  onPhase: (phase: EditPhase) => void = () => {},
): Promise<EditOutcome> {
  const createOp = t.uuid()
  const applyOp = t.uuid()
  const drop = (id: string) => t.discard(id).catch(() => undefined)
  onPhase('encoding')
  let render: ImageEditRender
  for (let attempt = 0; ; attempt++) {
    try {
      render = await t.create({
        operation_id: createOp,
        canvas_id: req.canvasID,
        node_id: req.nodeID,
        source_revision_id: req.sourceRevisionID,
        action: req.action,
        parameters: req.parameters,
      })
      break
    } catch (e) {
      const f = asFailure(e)
      // 同一操作标识重发是幂等的：响应丢了就再问一次。
      if (!known(f) && attempt < 2 && !signal.aborted) continue
      return { kind: 'failed', message: failureMessage(f), busyRenderID: f.renderID }
    }
  }
  for (let polls = 0; render.state === 'queued' || render.state === 'running'; polls++) {
    if (signal.aborted || polls >= pollLimit) {
      await drop(render.id)
      return signal.aborted ? { kind: 'cancelled' } : { kind: 'failed', message: '处理超时，原图没有改动，可以重试' }
    }
    try {
      await t.sleep(pollInterval, signal)
      render = await t.get(render.id)
    } catch {
      // 轮询失败不改变候选；下一轮再看，取消由 signal 决定。
    }
  }
  switch (render.state) {
    case 'applied':
      return { kind: 'applied', render }
    case 'not_reduced':
      return { kind: 'not_reduced', sourceSize: render.source_size }
    case 'failed':
      return { kind: 'failed', message: renderFailure(render.error_code) }
    case 'discarded':
    case 'expired':
      return { kind: 'failed', message: '编辑结果已过期，请重新应用' }
  }
  if (signal.aborted) {
    await drop(render.id)
    return { kind: 'cancelled' }
  }
  onPhase('applying')
  // 每次真正发出的采用请求体；重发必须原样，服务端才会把它当成同一操作回放。
  let sent: ApplyBody | undefined
  for (let attempt = 0; ; attempt++) {
    sent = {
      operation_id: applyOp,
      client_created_at: new Date().toISOString(),
      payload: {
        render_id: render.id,
        expected_source_revision_id: req.sourceRevisionID,
        expected_data_revision: req.dataRevision(),
      },
    }
    try {
      const result = await t.apply(req.canvasID, req.nodeID, sent)
      return { kind: 'applied', render: await t.get(render.id).catch(() => render), result }
    } catch (e) {
      const f = asFailure(e)
      if (f.code === 'creative_revision_conflict' && attempt < 3) {
        // 只是标题等元数据变了：等快照刷新后带上新的数据版本，采用同一结果。
        await t.sleep(500, new AbortController().signal)
        continue
      }
      if (known(f)) {
        // 明确被拒的结果不会再用：释放它，节点不再被占着。
        await drop(render.id)
        return { kind: 'failed', message: failureMessage(f) }
      }
      break
    }
  }
  // 采用已发出、结果未知：读候选自己的状态，不重新编码。
  onPhase('confirming')
  for (let i = 0; i < confirmLimit; i++) {
    try {
      const got = await t.get(render.id)
      if (got.state === 'applied') return { kind: 'applied', render: got }
      if (got.state !== 'ready') return { kind: 'failed', message: '结果没有应用，原图没有改动' }
      // 仍是 ready：原样重发。已提交就回放回执，没提交就现在提交。
      const result = await t.apply(req.canvasID, req.nodeID, sent)
      return { kind: 'applied', render: await t.get(render.id).catch(() => got), result }
    } catch (e) {
      const f = asFailure(e)
      if (known(f)) return { kind: 'failed', message: failureMessage(f) }
    }
    await t.sleep(pollInterval * 2, new AbortController().signal)
  }
  return { kind: 'unconfirmed', renderID: render.id }
}

// ---- 界面共用的展示文字 ----

export function originalOf(content: ContentRevision | undefined) {
  return content?.media?.find((m) => m.role === 'original')
}

export function sourceSize(content: ContentRevision | undefined): Size | null {
  const o = originalOf(content)
  return o?.width && o.height ? { width: o.width, height: o.height } : null
}

export const phaseLabel: Record<EditPhase, string> = {
  encoding: '正在处理',
  applying: '正在应用',
  confirming: '正在确认结果',
}

export type Outcome = Exclude<EditOutcome, { kind: 'cancelled' }>

export function outcomeSummary(o: Outcome, action: 'compress' | 'crop'): string {
  switch (o.kind) {
    case 'applied': {
      const out = o.render.output
      if (!out) return action === 'crop' ? '已裁切' : '已压缩'
      if (action === 'crop') return `已裁切为 ${out.width} × ${out.height}`
      const src = o.render.source_size ?? 0
      return `已压缩：${formatBytes(src)} → ${formatBytes(out.size)}，减少 ${reductionPercent(src, out.size)}%`
    }
    case 'not_reduced':
      return '当前设置未减小文件，请尝试降低质量'
    case 'unconfirmed':
      return '结果尚未确认，请稍后查看画布，不要重复应用'
    case 'failed':
      return o.message
  }
}
