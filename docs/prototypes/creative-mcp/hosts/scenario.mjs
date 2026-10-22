// 同一个剧本在四个宿主里走一遍。事件类型：
// - user：创作者说的话；
// - tool：外部助手调用工具，参数可以是函数 (ctx) => args；
// - assistant：外部助手写的话（Markdown）；
// - permission：宿主自带的工具许可；
// - hint：只在原型里出现的操作提示；
// - open-page：创作者打开了创意空间页面。
// only: 'gui' | 'tui' 限定宿主类型；repeat: true 表示 wait_tasks 没完成就再调一次。
import { canvasUrl } from '../shared/data.mjs'

const SCENE_URL = `${canvasUrl('scene-1')}&tasks=1`

export const STEPS = [
  {
    id: 'look',
    label: '看画布',
    look: '外部助手先自检，再找画布、读内容。三个都是数据工具，不挂卡片，连着调用也不会刷屏。终端宿主只显示结果的前几行，所以第一行必须是结论。',
    ours: ['工具显示名：检查连接、列出画布、读取画布', '三段结果文字：结论 → 字段 → 链接 → 下一步', 'instructions：每次会话先调用 get_capabilities'],
    events: [
      { type: 'user', text: '看看我最近的画布。把《九月，向海而行》里镜头 02 改得更有黄昏感，先生成两张看看。' },
      { type: 'tool', name: 'get_capabilities', args: {} },
      { type: 'tool', name: 'list_canvases', args: { limit: 5 } },
      { type: 'tool', name: 'read_canvas', args: { canvas_id: 'cv_9f2a' } },
    ],
  },
  {
    id: 'prompt',
    label: '改提示词',
    look: '第一次调用写入工具，宿主会弹自己的许可提示，提示里显示的是我们的工具显示名。各宿主的提示样子都是近似的，要等活原型核实。',
    ours: ['工具显示名「写提示词」和 annotations（不是只读）', '结果文字里带聚焦到节点的画布链接'],
    events: [
      { type: 'permission', name: 'set_node_prompt', args: { node_id: 'shot-2', prompt: '黄昏逆光，太阳贴近海平面…' } },
      { type: 'tool', name: 'set_node_prompt', args: { node_id: 'shot-2', prompt: '黄昏逆光，太阳贴近海平面…' } },
    ],
  },
  {
    id: 'dryrun',
    label: '预检',
    look: '要不要预检由外部助手决定（D9），这里它选了预检。在图形界面里，确认按钮就在卡片上；在终端里，外部助手只能用一句话来问。',
    ours: ['生成与任务卡的预检状态：4 个数据点、2 个动作', '预检结果文字：明确写出「没有生成，没有扣点」'],
    events: [
      { type: 'tool', name: 'generate_node', args: { node_id: 'shot-2', count: 2, dry_run: true }, ui: 'tasks' },
      {
        type: 'assistant',
        only: 'gui',
        text: '提示词改好了：太阳贴近海平面的逆光，裙摆和发丝带暖金色轮廓光。生成前的预检在上面的卡片里，确认就点「生成」。',
      },
      {
        type: 'assistant',
        only: 'tui',
        text: `提示词改好了：太阳贴近海平面的逆光，裙摆和发丝带暖金色轮廓光（[在画布中查看](${canvasUrl('shot-2')})）。\n\n生成前确认一下花费：gpt-image-2.5 生成 2 张 3:4，参考「海风」「留白」，**预计 40 点**（剩余 1,252 点）。要生成吗？`,
      },
    ],
  },
  {
    id: 'confirm',
    label: '确认生成',
    look: '图形界面宿主里，卡片自己调用 generate_node，并用 ui/update-model-context 告诉外部助手「已提交，别重复提交」（右侧日志可以看到）。这一轮外部助手不会发言，要等创作者下一句话才会读到这条上下文。',
    ours: ['卡片的 tools/call（generate_node 对卡片可见）', '交给模型的上下文文字'],
    events: [
      { type: 'hint', only: 'gui', text: '在上面的预检卡里点「生成 · 40 点」。' },
      { type: 'user', only: 'tui', text: '可以，生成吧' },
      { type: 'permission', only: 'tui', name: 'generate_node', args: { node_id: 'shot-2', count: 2 } },
      { type: 'tool', only: 'tui', name: 'generate_node', args: { node_id: 'shot-2', count: 2 } },
    ],
  },
  {
    id: 'wait',
    label: '等结果',
    look: '图形界面里，卡片每 1.5 秒经宿主调用一次 wait_tasks，进度和结果都在卡片上，可以「放大查看」或「存入资产库」。终端里由外部助手反复调用 wait_tasks，单次最多等 45 秒。',
    ours: ['任务卡：进行中（骨架屏 + 进度）→ 结果', 'wait_tasks 结果文字：没完成时提示「再调一次」，完成时附图片'],
    events: [
      { type: 'hint', only: 'gui', text: '等卡片出结果，可以试试「放大查看」（全屏）和「存入资产库」。' },
      { type: 'tool', only: 'tui', name: 'wait_tasks', args: (ctx) => ({ task_ids: ctx.taskIds }), repeat: true },
      {
        type: 'assistant',
        only: 'tui',
        text: `两张都好了，在镜头 02 的第 4、5 版：[在画布中查看](${canvasUrl('shot-2')})。\n\n第 5 版的轮廓光更明显，更接近你说的黄昏；第 4 版太阳略高，偏下午。还想调哪里，直接告诉我。`,
      },
    ],
  },
  {
    id: 'scene',
    label: '搭场景',
    look: '回看是场景客户端任务，要有打开的页面才会执行（D2）。卡片和文字给的是同一个链接；终端宿主里只能靠外部助手把链接转述给创作者。',
    ours: ['镜头卡的「等待页面」状态和「打开创意空间」按钮', 'render_view 结果文字：说清楚为什么等、打开哪个链接'],
    events: [
      { type: 'user', text: '按镜头 02 搭个 3D 场景，回看一下主机位。' },
      { type: 'tool', name: 'edit_scene', args: { canvas_id: 'cv_9f2a', node_id: 'scene-1', ops: '…' } },
      { type: 'tool', name: 'render_view', args: { node_id: 'scene-1', camera: '主机位' }, ui: 'shots' },
      {
        type: 'assistant',
        only: 'gui',
        text: '场景搭好了：礁石、人物替身，太阳贴着海平面。回看要由打开的创意空间页面来渲染，点卡片里的「打开创意空间」，打开后会自动继续。',
      },
      {
        type: 'assistant',
        only: 'tui',
        text: `场景搭好了：礁石、人物替身，太阳贴着海平面。\n\n回看要在浏览器里渲染，请先打开这个页面，并在完成前保持开着：\n[九月，向海而行 · 场景](${SCENE_URL})\n\n打开后告诉我，我接着等结果。`,
      },
    ],
  },
  {
    id: 'open',
    label: '打开页面',
    look: '打开的页面自动认领排队的任务，顶部横幅显示「正在为 Codex 执行」。卡片轮询到状态变化后，进入渲染中，然后出图。',
    ours: ['网页端：链接落地后的横幅（web/canvas.html?tasks=1）', '任务从「等待页面」到「渲染中」的文字和卡片状态'],
    events: [
      { type: 'hint', only: 'gui', text: '点卡片里的「打开创意空间」：新标签页打开画布页面，卡片随后进入渲染中。' },
      { type: 'user', only: 'tui', text: '开了' },
      { type: 'open-page', only: 'tui' },
      { type: 'tool', only: 'tui', name: 'wait_tasks', args: (ctx) => ({ task_ids: ctx.sceneTaskIds }), repeat: true },
      {
        type: 'assistant',
        only: 'tui',
        text: `回看好了：主机位 35mm，机位高 0.8m，太阳在地平线上 4°，人物基本是剪影，和镜头 02 的黄昏版一致。[在画布中查看](${canvasUrl('scene-1')})`,
      },
    ],
  },
  {
    id: 'export',
    label: '导出分镜',
    look: '导出一次产生多张图，适合用轮播（3–8 项，每项一个动作）。终端里只能是带链接的列表。',
    ours: ['镜头卡的轮播状态', 'export_shots 结果文字：每个镜头一行，带链接'],
    events: [
      { type: 'user', text: '把三个镜头的机位都导出到画布。' },
      { type: 'tool', name: 'export_shots', args: { node_id: 'scene-1', shots: ['镜头 01', '镜头 02', '镜头 03'] }, ui: 'shots' },
      { type: 'assistant', only: 'gui', text: '3 个镜头已经作为图片节点放进画布，可以直接拿来当生成参考。' },
      {
        type: 'assistant',
        only: 'tui',
        text: `3 个镜头已放进画布：\n1. [镜头 01 · 礁石远景](${canvasUrl('shot-1')})\n2. [镜头 02 · 逆光长裙](${canvasUrl('shot-2')})\n3. [镜头 03 · 回眸](${canvasUrl('shot-3')})\n\n可以直接拿来当生成参考。`,
      },
    ],
  },
]
