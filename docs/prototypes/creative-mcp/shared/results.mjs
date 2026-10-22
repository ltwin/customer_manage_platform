// 工具结果：文字宿主唯一能看到的东西，也是图形界面宿主里模型读的那一份（P1 文字为本）。
// 每条文字的格式都一样：
// - 第一行是结论；
// - 然后是关键字段；
// - 链接用 Markdown，指向生产地址；
// - 最后一行「下一步」写给外部助手看。
// structuredContent 只给卡片用，保持精简。签名图片地址这类只给卡片的数据放 _meta。
import { ACCOUNT, CANVAS, OTHER_CANVASES, canvasUrl, photo } from './data.mjs'

const text = (lines) => ({ type: 'text', text: lines.filter((l) => l !== null).join('\n') })
const link = (label, url) => `[${label}](${url})`

// 工具的显示名（tool title）：宿主的工具行和许可提示里会用到。
export const TITLES = {
  get_capabilities: '检查连接',
  list_canvases: '列出画布',
  read_canvas: '读取画布',
  set_node_prompt: '写提示词',
  generate_node: '生成',
  wait_tasks: '等待任务',
  save_to_library: '存入资产库',
  edit_scene: '修改场景',
  render_view: '回看',
  export_shots: '导出分镜',
}
// 读工具标 readOnlyHint，宿主据此少弹确认；生成会扣点，不标只读。
export const READ_ONLY = new Set(['get_capabilities', 'list_canvases', 'read_canvas', 'wait_tasks'])

const PROMPT =
  '黄昏逆光，太阳贴近海平面，长裙被海风向右后方吹起，人物半剪影，发丝和裙摆边缘有暖金色轮廓光；低机位 35mm，地平线在画面下三分之一，海面保留细碎高光，整体偏暖、对比柔和。'
export const GEN = {
  node: { id: 'shot-2', title: '镜头 02 · 逆光长裙' },
  model: 'gpt-image-2.5',
  count: 2,
  aspect: '3:4',
  cost: 40,
  refs: [
    { title: '参考 · 海风', thumb: photo(10) },
    { title: '参考 · 留白', thumb: photo(11) },
  ],
  results: [
    { url: photo(5), w: 850, h: 1275, label: '第 4 版' },
    { url: photo(8), w: 850, h: 1275, label: '第 5 版' },
  ],
}
export const RENDER = { url: photo(4), w: 850, h: 567, label: '主机位 · 35mm', meta: ['机位高 0.8m · 朝向西南', '太阳高度 4°'] }
export const SHOTS = [
  { url: photo(2), w: 850, h: 567, label: '镜头 01 · 礁石远景', meta: ['24mm · 机位高 1.6m', '人物占画面 1/8'], node: 'shot-1' },
  { url: photo(4), w: 850, h: 567, label: '镜头 02 · 逆光长裙', meta: ['35mm · 机位高 0.8m', '太阳贴地平线'], node: 'shot-2' },
  { url: photo(11), w: 850, h: 638, label: '镜头 03 · 回眸', meta: ['85mm · 机位高 1.4m', '背景压缩'], node: 'shot-3' },
]

export function getCapabilities({ pageOpen = false, host = 'Codex' } = {}) {
  return {
    content: [
      text([
        `已连接创意空间账号 ${ACCOUNT.name}，本月剩余 ${ACCOUNT.credits.toLocaleString()} 点。`,
        '- 可用生成模型：gpt-image-2.5（图片，20 点/张）、seedance-2（视频，120 点/条）',
        `- 打开的创意空间页面：${pageOpen ? '1 个（九月，向海而行）' : '没有。建模、回看、导出分镜需要创作者先打开画布页面'}`,
        `- 本连接：${host} · 授权于 10 月 20 日`,
        '下一步：用 list_canvases 找画布；写画布和生成前不必再调本工具。',
      ]),
    ],
  }
}

export function listCanvases() {
  const rows = [
    { ...CANVAS, nodes: 12 },
    ...OTHER_CANVASES.map((c) => ({ ...c, url: `${CANVAS.url.replace(CANVAS.id, c.id)}` })),
  ]
  return {
    content: [
      text([
        `最近编辑的 ${rows.length} 张画布：`,
        ...rows.map((c, i) => `${i + 1}. ${link(c.name, c.url ?? CANVAS.url)} · ${c.nodes} 个节点 · ${c.edited}编辑 · \`${c.id}\``),
        '下一步：用 read_canvas(canvas_id) 读取内容。',
      ]),
    ],
  }
}

export function readCanvas() {
  return {
    content: [
      text([
        `画布「${CANVAS.name}」· 12 个节点 · ${link('打开画布', CANVAS.url)}`,
        '- 分组「参考素材」：参考 · 海风（图片）、参考 · 留白（图片）',
        '- 镜头 01 · 礁石远景（图片，3 版）',
        '- 镜头 02 · 逆光长裙（图片，3 版）`shot-2` · 上游：参考 · 海风、参考 · 留白',
        '  当前提示词：「白天海边，长裙，人物正面…」',
        '- 镜头 03 · 回眸（图片，2 版）',
        '- 场景 · 海边礁石（3D 场景）',
        '- 另有 6 个文字节点（拍摄说明、服装清单…）',
        '下一步：要看某个节点的图片，用 read_asset；改提示词用 set_node_prompt。',
      ]),
    ],
  }
}

export function setNodePrompt() {
  return {
    content: [
      text([
        `已更新「${GEN.node.title}」的提示词（之前的版本可在节点历史里恢复）。`,
        `- 新提示词：「${PROMPT}」`,
        `- ${link('在画布中查看', canvasUrl(GEN.node.id))}`,
        '下一步：generate_node 生成；需要先确认花费时加 dry_run: true。',
      ]),
    ],
  }
}

export function generateDryRun() {
  const r = GEN
  return {
    content: [
      text([
        '预检完成：没有生成，没有扣点。',
        `- 节点：${r.node.title}`,
        `- 模型：${r.model} · ${r.count} 张 · ${r.aspect}`,
        `- 参考：${r.refs.map((x) => x.title).join('、')}`,
        `- 预计扣 ${r.cost} 点，当前剩余 ${ACCOUNT.credits.toLocaleString()} 点`,
        '下一步：创作者同意后，用同样参数调用 generate_node，去掉 dry_run。',
      ]),
    ],
    structuredContent: {
      view: 'dry_run',
      node: r.node,
      model: r.model,
      count: r.count,
      aspect: r.aspect,
      cost: r.cost,
      balance: ACCOUNT.credits,
      refs: r.refs.map((x) => ({ title: x.title })),
    },
    _meta: { thumbs: r.refs.map((x) => x.thumb), canvas_url: canvasUrl(r.node.id) },
  }
}

// task: { id, kind, status, progress?, startedAt? }
export function taskLines(t) {
  const url = canvasUrl(t.kind === 'generate' ? GEN.node.id : 'scene-1')
  if (t.kind === 'generate') {
    if (t.status === 'succeeded')
      return [
        `生成完成：${GEN.node.title} 新增 ${GEN.count} 张（${GEN.results.map((x) => x.label).join('、')}），扣 ${GEN.cost} 点。`,
        `- ${link('在画布中查看', url)}`,
        '- 图片附在下方，可直接判断效果',
      ]
    return [
      `生成进行中：\`${t.id}\` · ${GEN.node.title} · ${GEN.count} 张 · ${Math.round((t.progress ?? 0) * 100)}%`,
      `- ${link('在画布中查看进度', url)}`,
    ]
  }
  if (t.status === 'waiting_for_page')
    return [
      `回看任务在排队：\`${t.id}\` 需要一个打开的创意空间页面来渲染，目前没有。`,
      `- 请创作者打开：${link('九月，向海而行 · 场景', pageLink())}`,
      '- 页面打开后任务会自动继续，完成前要保持页面开着',
    ]
  if (t.status === 'succeeded')
    return [`回看完成：${RENDER.label}（${RENDER.meta.join('，')}）。`, `- ${link('在画布中查看', url)}`]
  return [`回看渲染中：\`${t.id}\` · 由打开的页面执行 · ${Math.round((t.progress ?? 0) * 100)}%`]
}
const pageLink = () => `${CANVAS.url}?focus=scene-1&tasks=1`

function taskImages(t) {
  if (t.status !== 'succeeded') return []
  const list = t.kind === 'generate' ? GEN.results : [RENDER]
  return list.map((x) => ({ type: 'image', url: x.url, w: x.w, h: x.h }))
}

function taskView(t) {
  const base = { id: t.id, kind: t.kind, status: t.status, progress: t.progress ?? 0 }
  if (t.kind === 'generate')
    return { ...base, title: GEN.node.title, count: GEN.count, aspect: GEN.aspect, cost: GEN.cost }
  return { ...base, title: '回看 · 主机位', aspect: '3:2' }
}
function taskMeta(t) {
  const meta = { canvas_url: canvasUrl(t.kind === 'generate' ? GEN.node.id : 'scene-1') }
  if (t.status === 'waiting_for_page') meta.open_url = pageLink()
  if (t.status === 'succeeded') meta.results = t.kind === 'generate' ? GEN.results : [RENDER]
  return meta
}

export function generateSubmit(t) {
  return {
    content: [
      text([
        `已提交生成：\`${t.id}\` · ${GEN.node.title} · ${GEN.count} 张 · 预计 ${GEN.cost} 点，约 20 秒。`,
        `- ${link('在画布中查看进度', canvasUrl(GEN.node.id))}`,
        `下一步：wait_tasks(task_ids: ["${t.id}"]) 等结果；不要重复提交。`,
      ]),
    ],
    structuredContent: { view: 'tasks', tasks: [taskView(t)] },
    _meta: { tasks: { [t.id]: taskMeta(t) } },
  }
}

// 单次最多等 45 秒（Codex 默认 60 秒超时）；到时还没完成就原样返回状态。
export function waitTasks(tasks, { waited = false } = {}) {
  const done = tasks.every((t) => t.status === 'succeeded' || t.status === 'failed')
  const lines = tasks.flatMap(taskLines)
  if (!done) lines.push(waited ? '已等待 45 秒，任务还没完成。下一步：再次调用 wait_tasks 继续等待。' : '下一步：再次调用 wait_tasks 继续等待。')
  return {
    content: [text(lines), ...tasks.flatMap(taskImages)],
    structuredContent: { view: 'tasks', tasks: tasks.map(taskView) },
    _meta: { tasks: Object.fromEntries(tasks.map((t) => [t.id, taskMeta(t)])) },
  }
}

export function editScene({ host = 'Codex' } = {}) {
  return {
    content: [
      text([
        '已修改场景「海边礁石」：新增礁石 3 块、人物替身 1 个，主光改为太阳（高度 4°，色温 3200K），加海面雾 0.15。',
        `- 修订署名为「${host}」，创作者可在场景历史里预览和恢复 · ${link('打开场景', canvasUrl('scene-1'))}`,
        '下一步：render_view 回看效果。',
      ]),
    ],
  }
}

export function renderView(t) {
  return waitTasks([t])
}

export function exportShots() {
  return {
    content: [
      text([
        `已导出 ${SHOTS.length} 个镜头到画布，各自成为一个图片节点：`,
        ...SHOTS.map((s, i) => `${i + 1}. ${link(s.label, canvasUrl(s.node))}（${s.meta.join('，')}）`),
        '下一步：可以用这些截图做参考，继续 generate_node。',
      ]),
      ...SHOTS.map((s) => ({ type: 'image', url: s.url, w: s.w, h: s.h })),
    ],
    structuredContent: { view: 'shots', shots: SHOTS.map((s) => ({ label: s.label, meta: s.meta })) },
    _meta: { shots: SHOTS.map((s) => ({ url: s.url, w: s.w, h: s.h, canvas_url: canvasUrl(s.node) })) },
  }
}

export function saveToLibrary(n) {
  return { content: [text([`已存入资产库「未归类」：${n} 张。`])] }
}
