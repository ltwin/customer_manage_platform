// 拍摄策划节点原型：画布卡片、类型选择、最大化编辑视图。数据只在内存里，刷新即回到示例。
// 结构对应 V6-079 design：标题 / 基本信息 / 章节（正文 + 参考图组 + 确认）/ 镜头表，正文用 ref 标记绑定引用 ID。
import { openMenu, openPopover, closeFloating, toast, confirmModal, esc } from './ui.mjs'

/* ───────── 图标（lucide 同款线宽） ───────── */
const svg = (b) => `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.65" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">${b}</svg>`
const P = {
  'arrow-left': '<path d="m12 19-7-7 7-7"/><path d="M19 12H5"/>',
  plus: '<path d="M5 12h14"/><path d="M12 5v14"/>',
  x: '<path d="M18 6 6 18"/><path d="m6 6 12 12"/>',
  'arrow-up': '<path d="m5 12 7-7 7 7"/><path d="M12 19V5"/>',
  check: '<path d="M20 6 9 17l-5-5"/>',
  pointer: '<path d="M4.037 4.688a.495.495 0 0 1 .651-.651l16 6.5a.5.5 0 0 1-.063.947l-6.124 1.58a2 2 0 0 0-1.438 1.435l-1.579 6.126a.5.5 0 0 1-.947.063z"/>',
  library: '<path d="m16 6 4 14"/><path d="M12 6v14"/><path d="M8 8v12"/><path d="M4 4v16"/>',
  sparkles: '<path d="M9.937 15.5A2 2 0 0 0 8.5 14.063l-6.135-1.582a.5.5 0 0 1 0-.962L8.5 9.936A2 2 0 0 0 9.937 8.5l1.582-6.135a.5.5 0 0 1 .963 0L14.063 8.5A2 2 0 0 0 15.5 9.937l6.135 1.581a.5.5 0 0 1 0 .964L15.5 14.063a2 2 0 0 0-1.437 1.437l-1.582 6.135a.5.5 0 0 1-.963 0z"/>',
  minimize: '<polyline points="4 14 10 14 10 20"/><polyline points="20 10 14 10 14 4"/><line x1="14" x2="21" y1="10" y2="3"/><line x1="3" x2="10" y1="21" y2="14"/>',
  maximize: '<polyline points="15 3 21 3 21 9"/><polyline points="9 21 3 21 3 15"/><line x1="21" x2="14" y1="3" y2="10"/><line x1="3" x2="10" y1="21" y2="14"/>',
  history: '<path d="M3 12a9 9 0 1 0 9-9 9.75 9.75 0 0 0-6.74 2.74L3 8"/><path d="M3 3v5h5"/><path d="M12 7v5l4 2"/>',
  ellipsis: '<circle cx="12" cy="12" r="1"/><circle cx="19" cy="12" r="1"/><circle cx="5" cy="12" r="1"/>',
  image: '<rect width="18" height="18" x="3" y="3" rx="2" ry="2"/><circle cx="9" cy="9" r="2"/><path d="m21 15-3.086-3.086a2 2 0 0 0-2.828 0L6 21"/>',
  bold: '<path d="M6 12h9a4 4 0 0 1 0 8H7a1 1 0 0 1-1-1V5a1 1 0 0 1 1-1h7a4 4 0 0 1 0 8"/>',
  italic: '<line x1="19" x2="10" y1="4" y2="4"/><line x1="14" x2="5" y1="20" y2="20"/><line x1="15" x2="9" y1="4" y2="20"/>',
  heading: '<path d="M6 12h12"/><path d="M6 20V4"/><path d="M18 20V4"/>',
  list: '<path d="M3 12h.01"/><path d="M3 18h.01"/><path d="M3 6h.01"/><path d="M8 12h13"/><path d="M8 18h13"/><path d="M8 6h13"/>',
  todo: '<rect x="3" y="5" width="6" height="6" rx="1"/><path d="m3 17 2 2 4-4"/><path d="M13 6h8"/><path d="M13 12h8"/><path d="M13 18h8"/>',
  quote: '<path d="M16 3a2 2 0 0 0-2 2v6a2 2 0 0 0 2 2 1 1 0 0 1 1 1v1a2 2 0 0 1-2 2 1 1 0 0 0-1 1v2a1 1 0 0 0 1 1 6 6 0 0 0 6-6V5a2 2 0 0 0-2-2z"/><path d="M5 3a2 2 0 0 0-2 2v6a2 2 0 0 0 2 2 1 1 0 0 1 1 1v1a2 2 0 0 1-2 2 1 1 0 0 0-1 1v2a1 1 0 0 0 1 1 6 6 0 0 0 6-6V5a2 2 0 0 0-2-2z"/>',
  link: '<path d="M10 13a5 5 0 0 0 7.54.54l3-3a5 5 0 0 0-7.07-7.07l-1.72 1.71"/><path d="M14 11a5 5 0 0 0-7.54-.54l-3 3a5 5 0 0 0 7.07 7.07l1.71-1.71"/>',
  at: '<circle cx="12" cy="12" r="4"/><path d="M16 8v5a3 3 0 0 0 6 0v-1a10 10 0 1 0-4 8"/>',
  'chevron-down': '<path d="m6 9 6 6 6-6"/>',
  clapper: '<path d="M20.2 6 3 11l-.9-2.4c-.3-1.1.3-2.2 1.3-2.5l13.5-4c1.1-.3 2.2.3 2.5 1.3Z"/><path d="m6.2 5.3 3.1 3.9"/><path d="m12.4 3.4 3.1 4"/><path d="M3 11h18v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2Z"/>',
  search: '<circle cx="11" cy="11" r="8"/><path d="m21 21-4.3-4.3"/>',
  alert: '<path d="m21.73 18-8-14a2 2 0 0 0-3.48 0l-8 14A2 2 0 0 0 4 21h16a2 2 0 0 0 1.73-3"/><path d="M12 9v4"/><path d="M12 17h.01"/>',
  'eye-off': '<path d="M10.733 5.076a10.744 10.744 0 0 1 11.205 6.575 1 1 0 0 1 0 .696 10.747 10.747 0 0 1-1.444 2.49"/><path d="M14.084 14.158a3 3 0 0 1-4.242-4.242"/><path d="M17.479 17.499a10.75 10.75 0 0 1-15.417-5.151 1 1 0 0 1 0-.696 10.75 10.75 0 0 1 4.446-5.143"/><path d="m2 2 20 20"/>',
  mask: '<path d="M2 12a5 5 0 0 0 5 5 8 8 0 0 1 5 2 8 8 0 0 1 5-2 5 5 0 0 0 5-5V7h-5a8 8 0 0 0-5 2 8 8 0 0 0-5-2H2Z"/><path d="M6 11c1.5 0 3 .5 3 2-2 0-3 0-3-2Z"/><path d="M18 11c-1.5 0-3 .5-3 2 2 0 3 0 3-2Z"/>',
  rings: '<circle cx="9" cy="14" r="5"/><circle cx="15" cy="10" r="5"/>',
  user: '<path d="M19 21v-2a4 4 0 0 0-4-4H9a4 4 0 0 0-4 4v2"/><circle cx="12" cy="7" r="4"/>',
  box: '<path d="M21 8a2 2 0 0 0-1-1.73l-7-4a2 2 0 0 0-2 0l-7 4A2 2 0 0 0 3 8v8a2 2 0 0 0 1 1.73l7 4a2 2 0 0 0 2 0l7-4A2 2 0 0 0 21 16Z"/><path d="m3.3 7 8.7 5 8.7-5"/><path d="M12 22V12"/>',
  file: '<path d="M15 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V7Z"/><path d="M14 2v4a2 2 0 0 0 2 2h4"/>',
  trash: '<path d="M3 6h18"/><path d="M19 6v14c0 1-1 2-2 2H7c-1 0-2-1-2-2V6"/><path d="M8 6V4c0-1 1-2 2-2h4c1 0 2 1 2 2v2"/>',
  grid: '<rect width="7" height="7" x="3" y="3" rx="1"/><rect width="7" height="7" x="14" y="3" rx="1"/><rect width="7" height="7" x="14" y="14" rx="1"/><rect width="7" height="7" x="3" y="14" rx="1"/>',
  rows: '<rect width="18" height="7" x="3" y="3" rx="1"/><rect width="18" height="7" x="3" y="14" rx="1"/>',
  undo: '<path d="M9 14 4 9l5-5"/><path d="M4 9h10.5a5.5 5.5 0 0 1 5.5 5.5 5.5 5.5 0 0 1-5.5 5.5H11"/>',
}
const I = (n) => `<span class="pi">${svg(P[n] ?? '')}</span>`
const hydrate = (root = document) => root.querySelectorAll('[data-pi]').forEach((el) => { el.innerHTML = svg(P[el.dataset.pi] ?? ''); el.removeAttribute('data-pi') })

/* ───────── 示例图片（v4 示例影像，仅作占位） ───────── */
const PH = {
  '01': [850, 1275, '光落在她停留的地方'], '02': [850, 567, '把风景框进画面'], '03': [850, 1275, '一束光'],
  '04': [850, 567, '海岸边的紫色'], '05': [850, 1275, '白裙与拱形'], '06': [850, 1249, '柔软的白'],
  '07': [850, 1133, '花影'], '08': [850, 1275, '城市光影'], '09': [850, 567, '弧线与玫瑰色'],
  '10': [850, 1275, '等一阵海风'], '11': [850, 638, '蓝色'], '12': [850, 1133, '去海边找留白'],
}
const src = (n) => `../v4/assets/photo-${n}.jpg`
const LIBRARY = Object.entries(PH).map(([n, [w, h, t]]) => ({ n, w, h, title: t }))
const ON_CANVAS = ['03', '02', '08', '10', '05']

/* ───────── 策划类型与模板 ───────── */
const TYPES = [
  { key: 'cosplay', name: 'Cosplay 拍摄', icon: 'mask', desc: 'IP 拆解、情绪板、后期合成 SOP、2+4+3 交付阵列', ready: true },
  { key: 'wedding', name: '婚纱外景', icon: 'rings', desc: '路线与时段、服装更换、家人合影清单', ready: false },
  { key: 'portrait', name: '人像写真', icon: 'user', desc: '情绪与造型、场景动线、精修要求', ready: false },
  { key: 'product', name: '商业产品', icon: 'box', desc: '卖点拆解、布光方案、平台尺寸交付', ready: false },
]
const TEMPLATE = {
  key: 'cosplay-sop', version: 1,
  sections: [
    { key: 'ip_breakdown', title: 'IP 拆解', stage: '前期策划', groups: ['角色亮点', '动作参考', '角色元素', '场景元素'], ph: '视觉锚点：角色亮点、动作参考、角色元素\n世界观对齐：场景元素与美术调性' },
    { key: 'creative_direction', title: '创意方向', stage: '前期策划', groups: ['参考'], ph: '候选方向、选定方向，以及和常见作品的差别' },
    { key: 'moodboard', title: '情绪板', stage: '前期策划', groups: ['光影逻辑', '构图与透视', '场景资产与材质'], ph: '色彩脚本：冷暖倾向与情感基调' },
    { key: 'preparation', title: '准备清单', stage: '准备', groups: ['参考'], ph: '妆造、服装、道具、场地、灯具与拍摄素材，逐项勾选\n合成片场地一般为无影棚' },
    { key: 'lighting', title: '光源逻辑', stage: '拍摄', groups: ['光位参考'], ph: '主光锚定、环境光匹配、特殊光效（给后期留接口）' },
    { key: 'body_staging', title: '人体拆分', stage: '拍摄', groups: ['参考'], ph: '肢体遮挡处理，便于后期抠像与深度关系重建' },
    { key: 'post_composite', title: '后期合成', stage: '后期', groups: ['参考'], ph: '资产重构、空间构建、视觉特效与影调——列出本次适用的步骤' },
    { key: 'ai_assist', title: 'AI 辅助', stage: '后期', groups: ['参考'], ph: '场景优化、景深优化、局部重绘' },
  ],
  slots: [
    { key: 'kv', name: '主视觉 KV', desc: '全身大场景，交代世界观', target: 2, framing: '全身' },
    { key: 'half', name: '半身', desc: '动态瞬间 / splash，强调张力', target: 4, framing: '半身' },
    { key: 'closeup', name: '特写', desc: '面部细节，强调角色神态', target: 3, framing: '特写' },
  ],
}
const STAGES = ['前期策划', '准备', '拍摄', '后期', '交付']
// 视觉设定是设定列表形状，排在情绪板之后、准备清单之前
const ORDER = ['ip_breakdown', 'creative_direction', 'moodboard', 'visual_settings', 'preparation', 'lighting', 'body_staging', 'post_composite', 'ai_assist', 'shots']
const numOf = (key) => String(ORDER.indexOf(key) + 1).padStart(2, '0')
const STATE_TEXT = { empty: '空', draft: '草稿', confirmed: '已确认', changed: '确认后有修改', na: '本次不适用' }

/* ───────── 示例策划 ───────── */
let uid = 100
const id = (p) => `${p}_${(uid++).toString(36)}`
const ref = (rid, n, group, caption, source) => ({ id: rid, n, group, caption, source })
const chip = (rid) => `<span class="ref-chip" contenteditable="false" data-ref="${rid}"></span>`

function samplePlan() {
  const sec = (key, extra) => ({ key, rev: 1, confirmedRev: null, na: false, headline: '', body: '', refs: [], ...extra })
  return {
    id: 'doc_furina', type: 'cosplay', revision: 18,
    title: '芙宁娜 · 空剧院的独自谢幕',
    brief: { work: '原神', character: '芙宁娜', variant: '常服', forms: ['合成'], location: '无影棚（剧院场景后期合成）', constraints: '交付 9 张（2+4+3）；coser 自备假发与礼帽；棚拍半天' },
    sections: [
      sec('ip_breakdown', {
        rev: 12, confirmedRev: 12, headline: '水与审判：礼帽、异色瞳、舞台上的双重身份',
        body: `<h3>视觉锚点</h3><ul><li><strong>角色亮点</strong>：高顶礼帽与水滴装饰、异色瞳、手杖，整体是「舞台上的审判者」${chip('r1')}</li><li><strong>动作参考</strong>：谢幕礼、单手持杖指向观众、提裙转身 ${chip('r3')}</li><li><strong>角色元素</strong>：蓝白配色、水纹、天平与审判意象 ${chip('r2')}</li></ul><h3>世界观对齐</h3><p>枫丹：美好年代的唯美古典风叠加蒸汽机械。美术资产以冷蓝、黄铜、大理石为主调，场景优先选剧院、法庭、水道 ${chip('r4')}。</p>`,
        refs: [ref('r1', '05', '角色亮点', '礼帽与蓝白配色', { site: 'ArtStation', author: '示例作者 A' }), ref('r3', '01', '动作参考', '单手指向观众的谢幕姿态', { site: 'Pinterest', author: '示例作者 B' }), ref('r2', '06', '角色元素', '水纹与白色材质'), ref('r4', '02', '场景元素', '古典建筑框景', { site: '资产库' })],
      }),
      sec('creative_direction', {
        rev: 17, confirmedRev: 15, headline: '散场后的空剧院，她仍在完成一场没有观众的演出',
        body: `<h3>选定方向 · 空剧院的独自谢幕</h3><p>夜场散场后的歌剧院，观众席空无一人，只剩舞台顶灯打在她身上。用「审判席」般的孤立感表达角色的双重身份。</p><h3>与常见作品的差别</h3><ul><li>常见拍法集中在白天水边、喷泉与整体蓝色滤镜；本方案改为夜场室内，水只以舞台反光和飞溅出现。</li><li>常见构图让人物占满画面；本方案用大场景留白，让人物显得渺小 ${chip('r9')}。</li></ul><h3>备选（未采用）</h3><ul><li>A · 水下法庭：冲击力强，但合成成本高、肢体遮挡复杂。</li><li>C · 街头日常：差异化足够，但角色识别度弱。</li></ul>`,
      }),
      sec('moodboard', {
        rev: 14, headline: '冷蓝夜场 + 黄铜暖点，情绪从压抑到释放',
        body: '<h3>色彩脚本</h3><p>冷蓝灰占七成，黄铜暖点不超过一成，高光留给礼帽与水花。情绪从压抑（整体暗调）到释放（KV 02 的水花爆发），后半组镜头逐渐提亮。</p>',
        refs: [ref('r5', '03', '光影逻辑', '单一顶光，暗部大面积留黑', { site: 'Pinterest', author: '示例作者 C' }), ref('r6', '08', '光影逻辑', '硬光与清晰投影'), ref('r7', '11', '光影逻辑', '冷蓝环境色'), ref('r8', '09', '构图与透视', '弧线引导视线'), ref('r9', '12', '构图与透视', '大面积留白，人物偏小', { site: 'ArtStation', author: '示例作者 D' }), ref('r10', '04', '场景资产与材质', '天鹅绒与花材'), ref('r11', '07', '场景资产与材质', '花影投射')],
      }),
      sec('preparation', {
        rev: 14, headline: '无影棚半天，水花素材单独拍',
        body: '<h3 class="sub-group">妆造</h3><ul class="task"><li class="done">假发修剪与定型（coser 自备）</li><li>异色美瞳 · 左白右蓝</li><li>水滴贴饰 ×6</li></ul><h3 class="sub-group">服装与道具</h3><ul class="task"><li class="done">礼帽、外套熨烫</li><li>手杖：黄铜色喷涂</li></ul><h3 class="sub-group">场地与灯具</h3><ul class="task"><li class="done"><strong>无影棚</strong>半天（合成片场地用无影棚即可）</li><li>顶光：柔光箱 + 蜂巢</li><li>两盏轮廓灯 + 冷色凝胶</li></ul><h3 class="sub-group">拍摄素材</h3><ul class="task"><li>水花飞溅：棚内单独拍，黑底高速快门</li><li>剧院空镜：素材库或 AI 生成</li></ul>',
      }),
      sec('lighting', {
        rev: 18, headline: '舞台顶灯为主光，冷色轮廓给后期留接口',
        body: `<h3>主光锚定</h3><p>场景光源是舞台顶灯，棚内主光放在头顶偏前 30°，柔光箱加蜂巢收光，保证人物与剧院光位一致 ${chip('r12')}。</p><h3>环境光匹配</h3><p>剧院暗部是冷蓝，用低功率蓝色反光模拟环境色溢出；地面接触处保留真实阴影，方便后期接地。</p><h3>特殊光效</h3><p>两侧冷色轮廓光勾出礼帽与发丝边缘，<strong>为后期水花与舞台烟雾预留接口</strong>。</p>`,
        refs: [ref('r12', '10', '光位参考', '顶光 + 双侧轮廓')],
      }),
      sec('body_staging'),
      sec('post_composite', {
        rev: 9, headline: '抠像保发丝，透视对齐机位，补接触阴影',
        body: '<h3>资产重构</h3><ul><li><strong>抠像</strong>：发丝与礼帽网纱边缘单独处理，保留半透明。</li><li><strong>形体雕刻</strong>：按人体结构修肩颈线，不做单纯推脸。</li><li><strong>高低频磨皮</strong>：保留皮肤纹理。</li></ul><h3>空间构建</h3><ul><li><strong>透视匹配</strong>：剧院素材的地平线与机位高度对齐（相机高约 1.2 m）。</li><li><strong>光影遮蔽</strong>：补脚下接触阴影，解决「漂浮感」。</li></ul><h3>视觉特效与影调</h3><ul><li>水花粒子、舞台烟雾；全图统一冷蓝；轻微颗粒与色散，消除「数码味」。</li></ul>',
      }),
      sec('ai_assist', { rev: 9, body: '<ul><li>剧院空镜不足时，用 AI 生成观众席与穹顶，再与实拍做透视匹配。</li><li>局部重绘：修复穿帮的灯架与地面胶带。</li></ul>' }),
    ],
    shots: {
      rev: 15,
      items: [
        { id: 's1', rev: 15, slot: 'kv', framing: '全身', title: '空剧院谢幕', board: { n: '08', style: '写实' }, refs: ['r5', 'r9'], action: '面向空观众席行谢幕礼，手杖斜指前方', expression: '平静，带一点骄傲', composition: '舞台中轴略俯拍，人物占画面 1/6，观众席大面积留白', lighting: '顶光单点 + 两侧冷色轮廓', notes: '合成：剧院背景 + 地面水面反光' },
        { id: 's2', rev: 15, slot: 'kv', framing: '全身', title: '水花爆发', board: { n: '10', style: '写实' }, refs: ['r7'], action: '转身甩开外套，水花从脚下爆开', expression: '释放，嘴角上扬', composition: '低机位仰拍，水花占画面下三分之一', lighting: '顶光 + 逆光打亮水花', notes: '水花素材棚内单独拍' },
        { id: 's3', rev: 11, slot: 'half', framing: '半身', title: '指向观众', board: { n: '05', style: '草图' }, refs: ['r3'], action: '单手持杖指向镜头', expression: '审判者的冷静', composition: '三分法，手杖形成对角线', lighting: '侧上方主光', notes: '' },
        { id: 's4', rev: 11, slot: 'half', framing: '半身', title: '提裙回眸', board: { n: '01', style: '草图' }, refs: [], action: '提起裙摆回头', expression: '一瞬的脆弱', composition: '人物偏右，左侧留白', lighting: '轮廓光为主，脸部低调', notes: '' },
        { id: 's5', rev: 9, slot: 'half', framing: '半身', title: '手杖落地', board: null, refs: [], action: '手杖杵地，水滴溅起', expression: '', composition: '', lighting: '', notes: '' },
        { id: 's6', rev: 9, slot: 'closeup', framing: '特写', title: '异色瞳', board: { n: '06', style: '灰度' }, refs: ['r2'], action: '侧脸，眼神看向镜头外', expression: '若有所思', composition: '只取眼睛与帽檐', lighting: '眼神光两点', notes: '美瞳需提前适应' },
      ],
    },
    history: [
      { rev: 18, who: 'agent', run: '拍摄策划 · 芙宁娜', at: '10:42', what: ['光源逻辑'] },
      { rev: 17, who: 'me', at: '10:31', what: ['创意方向'] },
      { rev: 16, who: 'me', at: '10:30', what: ['确认 IP 拆解'] },
      { rev: 15, who: 'agent', run: '拍摄策划 · 芙宁娜', at: '10:12', what: ['镜头 · 空剧院谢幕', '镜头 · 水花爆发'] },
      { rev: 14, who: 'agent', run: '拍摄策划 · 芙宁娜', at: '10:05', what: ['情绪板', '准备清单'] },
      { rev: 13, who: 'me', at: '09:58', what: ['基本信息'] },
      { rev: 12, who: 'agent', run: '拍摄策划 · 芙宁娜', at: '09:51', what: ['IP 拆解'] },
      { rev: 1, who: 'me', at: '09:40', what: ['新建策划'] },
    ],
  }
}
// 示例：人设三视图 + 场景主图与派生角度；镜头关联设定，分镜记录生成时的设定版本
function withSettings(p) {
  p.settings = {
    rev: 17,
    items: [
      { id: 'set_char', kind: 'character', name: '芙宁娜 · 常服', rev: 16, confirmedRev: 16, notes: '礼帽高度约为头高 1.2 倍；左白右蓝异色瞳；手杖黄铜色，杖头水滴造型。', basis: ['r1', 'r2'], views: [{ view: '正面', n: '05' }, { view: '侧面', n: '01' }, { view: '背面', n: '06' }] },
      { id: 'set_scene', kind: 'scene', name: '空剧院舞台', rev: 17, confirmedRev: 15, notes: '夜场散场后；舞台顶灯单点，观众席深红天鹅绒座椅隐在暗处；地面黑色亚克力，带舞台反光。#17 把穹顶从玻璃改为彩绘。', basis: ['r4', 'r9'], views: [{ view: '主场景', n: '02' }, { view: '反打 · 观众席', n: '12' }, { view: '细节 · 舞台地面', n: '11' }] },
    ],
  }
  const set = (id, sets, basedOn) => { const sh = p.shots.items.find((x) => x.id === id); sh.sets = sets; if (sh.board && basedOn) sh.board.basedOn = basedOn }
  set('s1', ['set_char', 'set_scene'], { set_char: 16, set_scene: 15 })
  set('s2', ['set_char', 'set_scene'], { set_char: 16, set_scene: 17 })
  set('s3', ['set_char', 'set_scene'], { set_char: 16, set_scene: 17 })
  set('s4', ['set_char'], { set_char: 16 })
  set('s5', ['set_char', 'set_scene'])
  set('s6', [], null)
  // 确认时那一版的内容（真实实现从 confirmed_rev 对应的修订载荷里取），用于「看改动」
  const dir = p.sections.find((s) => s.key === 'creative_direction')
  dir.confirmedSnap = {
    headline: '散场后的空剧院，她独自完成谢幕',
    body: dir.body.replace('只剩舞台顶灯打在她身上', '舞台顶灯打在她身上').replace(/<li>常见构图让人物占满画面.*?<\/li>/, ''),
    refs: [],
  }
  p.sections.find((s) => s.key === 'ip_breakdown').confirmedSnap = snapOf(p, { kind: 'section', key: 'ip_breakdown' })
  const scene = findSet(p, 'set_scene')
  scene.confirmedSnap = { name: scene.name, notes: scene.notes.replace('#17 把穹顶从玻璃改为彩绘。', '穹顶为玻璃。'), views: [{ view: '主场景', n: '09' }, ...scene.views.slice(1)] }
  findSet(p, 'set_char').confirmedSnap = snapOf(p, { kind: 'setting', id: 'set_char' })
  p.runs = []
  return p
}
function emptyPlan() {
  return {
    id: id('doc'), type: 'cosplay', revision: 1, title: '未命名策划',
    brief: { work: '', character: '', variant: '', forms: [], location: '', constraints: '' },
    sections: TEMPLATE.sections.map((s) => ({ key: s.key, rev: 1, confirmedRev: null, na: false, headline: '', body: '', refs: [] })),
    settings: { rev: 1, items: [] },
    shots: { rev: 1, items: [] },
    history: [{ rev: 1, who: 'me', at: now(), what: ['新建策划'] }],
    runs: [],
  }
}

/* ───────── 状态 ───────── */
const plans = new Map()
let nodes = []
let selected = null
let current = null // 当前打开的策划
let offline = false
const saving = new Map() // 单元 → 定时器
let previewRev = null
let side = null // 右栏：null | 'history' | 'agent'，两者互斥
const diffs = new Map() // 单元 key → 打开着的改动对照 { mode: 'confirmed' | 'run', base, ... }
let shotView = 'cards' // 镜头表：逐张卡片 / 紧凑总览
let shotPick = null // 选镜头生成分镜图时的已选集合
let pickStyle = '草图'

function reset() {
  plans.clear()
  const p = withSettings(samplePlan())
  plans.set(p.id, p)
  convs = sampleConvs()
  p.workId = 'conv_plan'
  Object.assign(panel, { open: false, viewId: null, running: false, draft: { text: '', units: [], skill: false } })
  nodes = [
    { id: 'n1', kind: 'image', n: '03', title: '参考 · 舞台顶光', x: -620, y: -250, w: 170, h: 255 },
    { id: 'n2', kind: 'image', n: '02', title: '参考 · 剧院框景', x: -620, y: 60, w: 170, h: 113 },
    { id: 'n3', kind: 'image', n: '08', title: '分镜 · 空剧院谢幕', x: -420, y: -250, w: 170, h: 255 },
    { id: 'n4', kind: 'image', n: '10', title: '分镜 · 水花爆发', x: -420, y: 60, w: 170, h: 255 },
    { id: 'n5', kind: 'plan', doc: p.id, x: -170, y: -250, w: 360, h: 352 },
  ]
  selected = 'n5'
  renderCanvas()
  renderCanvasAgent()
}

const now = () => { const d = new Date(); return `${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}` }
const tmplOf = (key) => TEMPLATE.sections.find((s) => s.key === key)
const allRefs = (p) => [...p.sections.flatMap((s) => groupOrder(s).flatMap((g) => s.refs.filter((r) => r.group === g)))]
const groupOrder = (s) => { const t = tmplOf(s.key).groups; return [...t, ...new Set(s.refs.map((r) => r.group).filter((g) => !t.includes(g)))] }
const refNo = (p, rid) => allRefs(p).findIndex((r) => r.id === rid) + 1
const findRef = (p, rid) => allRefs(p).find((r) => r.id === rid)
const isBlank = (s) => !s.headline.trim() && !s.body.replace(/<[^>]*>/g, '').trim() && !s.refs.length
function stateOf(s) {
  if (s.na) return 'na'
  if (isBlank(s)) return 'empty'
  if (s.confirmedRev != null) return s.confirmedRev === s.rev ? 'confirmed' : 'changed'
  return 'draft'
}
const shotsState = (p) => (p.shots.items.length ? 'draft' : 'empty')
const setState = (x) => (x.confirmedRev == null ? 'draft' : x.confirmedRev === x.rev ? 'confirmed' : 'changed')
function settingsState(p) {
  const xs = p.settings.items.map(setState)
  if (!xs.length) return 'empty'
  if (xs.includes('changed')) return 'changed'
  return xs.every((x) => x === 'confirmed') ? 'confirmed' : 'draft'
}
const findSet = (p, sid) => p.settings.items.find((x) => x.id === sid)
// 分镜记录的设定版本与当前不同（或设定已删除）就算过时
const isStale = (p, sh) => Boolean(sh.board?.basedOn && Object.entries(sh.board.basedOn).some(([k, r]) => findSet(p, k)?.rev !== r))
const staleCount = (p, sid) => p.shots.items.filter((sh) => isStale(p, sh) && (!sid || sh.board.basedOn[sid] != null)).length

/* 完成度：镜头已规划（必填字段齐）、分镜图已齐，与数量分开算 */
const SHOT_REQUIRED = [['action', '动作'], ['composition', '构图'], ['lighting', '光线']]
const missingOf = (sh) => SHOT_REQUIRED.filter(([k]) => !sh[k]?.trim()).map(([, l]) => l)
const TARGET = TEMPLATE.slots.reduce((a, sl) => a + sl.target, 0)
function shotStats(p) {
  const xs = p.shots.items
  return { total: xs.length, planned: xs.filter((s) => !missingOf(s).length).length, boards: xs.filter((s) => s.board && !isStale(p, s)).length, stale: staleCount(p) }
}
const slotPrefix = (k) => (k === 'kv' ? 'KV' : k === 'half' ? 'H' : 'C')
function shotNo(p, s) {
  const list = p.shots.items.filter((x) => x.slot === s.slot)
  return `${slotPrefix(s.slot)}${String(list.indexOf(s) + 1).padStart(2, '0')}`
}

/* 单元快照：章节 / 镜头 / 设定 / 基本信息。真实实现从修订载荷里按单元取，这里在内存里留一份 */
const uk = (u) => (u.kind === 'brief' ? 'brief' : `${u.kind}:${u.key ?? u.id}`)
const ukParse = (k) => { const [kind, v] = k.split(':'); return kind === 'section' ? { kind, key: v } : kind === 'brief' ? { kind } : { kind, id: v } }
function unitGet(p, u) {
  if (u.kind === 'section') return p.sections.find((s) => s.key === u.key)
  if (u.kind === 'shot') return p.shots.items.find((s) => s.id === u.id)
  if (u.kind === 'setting') return findSet(p, u.id)
  if (u.kind === 'brief') return p.brief
  return null
}
function snapOf(p, u) {
  const x = unitGet(p, u)
  if (!x) return null
  if (u.kind === 'section') return structuredClone({ headline: x.headline, body: x.body, refs: x.refs })
  if (u.kind === 'setting') return structuredClone({ name: x.name, notes: x.notes, views: x.views })
  const { conflict, ...rest } = x
  return structuredClone(rest)
}
function applySnap(p, u, snap) {
  if (u.kind === 'shot') {
    const i = p.shots.items.findIndex((s) => s.id === u.id)
    if (!snap) { if (i >= 0) p.shots.items.splice(i, 1); return } // 本轮新增的镜头：恢复即删除
    if (i < 0) p.shots.items.push(structuredClone(snap))
    else Object.assign(p.shots.items[i], structuredClone(snap))
    return
  }
  Object.assign(unitGet(p, u), structuredClone(snap))
}
function unitTitle(p, u) {
  if (u.kind === 'section') return tmplOf(u.key).title
  if (u.kind === 'brief') return '基本信息'
  if (u.kind === 'setting') return `设定 · ${findSet(p, u.id)?.name ?? '已删除'}`
  const sh = p.shots.items.find((x) => x.id === u.id)
  return sh ? `镜头 · ${shotNo(p, sh)} ${sh.title}` : '镜头（已删除）'
}

/* 改动对照：正文按段落比较，相邻的删改对按字符标出差别；字段逐项比较；图片按 ID 比较增删 */
function lcs(a, b, eq = (x, y) => x === y) {
  const n = a.length
  const m = b.length
  const T = Array.from({ length: n + 1 }, () => new Int32Array(m + 1))
  for (let i = n - 1; i >= 0; i--) for (let j = m - 1; j >= 0; j--) T[i][j] = eq(a[i], b[j]) ? T[i + 1][j + 1] + 1 : Math.max(T[i + 1][j], T[i][j + 1])
  const ops = []
  let i = 0
  let j = 0
  while (i < n && j < m) {
    if (eq(a[i], b[j])) { ops.push({ op: '=', a: a[i++], b: b[j++] }) } else if (T[i + 1][j] >= T[i][j + 1]) ops.push({ op: '-', a: a[i++] })
    else ops.push({ op: '+', b: b[j++] })
  }
  while (i < n) ops.push({ op: '-', a: a[i++] })
  while (j < m) ops.push({ op: '+', b: b[j++] })
  return { ops, same: T[0][0] }
}
function inlineDiff(a = '', b = '') {
  if (a === b) return esc(b)
  const A = [...a]
  const B = [...b]
  if (A.length * B.length > 250000) return `<del>${esc(a)}</del><ins>${esc(b)}</ins>`
  const { ops, same } = lcs(A, B)
  if (same < Math.min(A.length, B.length) * 0.35) return `${a ? `<del>${esc(a)}</del>` : ''}${b ? `<ins>${esc(b)}</ins>` : ''}`
  // 合成连续片段，再把夹在改动之间的零碎相同字（≤ 2 字）并进改动，避免读成碎片
  const segs = []
  for (const o of ops) { const t = o.op === '+' ? o.b : o.a; if (segs.at(-1)?.op === o.op) segs.at(-1).t += t; else segs.push({ op: o.op, t }) }
  const clean = []
  segs.forEach((g, k) => {
    if (g.op === '=' && [...g.t].length <= 2 && k > 0 && k < segs.length - 1) clean.push({ op: '-', t: g.t }, { op: '+', t: g.t })
    else clean.push(g)
  })
  let out = ''
  let del = ''
  let add = ''
  const flush = () => { if (del) out += `<del>${esc(del)}</del>`; if (add) out += `<ins>${esc(add)}</ins>`; del = ''; add = '' }
  for (const g of clean) { if (g.op === '=') { flush(); out += esc(g.t) } else if (g.op === '-') del += g.t; else add += g.t }
  flush()
  return out
}
function blocksOf(html) {
  const d = document.createElement('div')
  d.innerHTML = html
  d.querySelectorAll('.ref-chip').forEach((c) => c.remove())
  const out = []
  const walk = (el) => { for (const c of el.children) { if (/^(UL|OL)$/.test(c.tagName)) walk(c); else out.push({ tag: c.tagName.toLowerCase(), text: c.textContent.replace(/\s+/g, ' ').trim() }) } }
  walk(d)
  return out.filter((x) => x.text)
}
function bodyDiff(before, after) {
  const { ops } = lcs(blocksOf(before), blocksOf(after), (x, y) => x.text === y.text)
  const out = []
  let quiet = 0
  const flushQuiet = () => { if (quiet) out.push(`<div class="df-quiet">${quiet} 段未改动</div>`); quiet = 0 }
  for (let k = 0; k < ops.length; k++) {
    const o = ops[k]
    if (o.op === '=') { if (o.a.tag === 'h3') { flushQuiet(); out.push(`<div class="df-b df-h3">${esc(o.a.text)}</div>`) } else quiet++; continue }
    flushQuiet()
    // 一段删除紧跟一段新增：当作同一段的修改，标出字符级差别
    if (o.op === '-' && ops[k + 1]?.op === '+') { const n = ops[k + 1].b; out.push(`<div class="df-b df-${n.tag}">${inlineDiff(o.a.text, n.text)}</div>`); k++; continue }
    out.push(`<div class="df-b df-${(o.a ?? o.b).tag} ${o.op === '-' ? 'is-del' : 'is-add'}">${o.op === '-' ? `<del>${esc(o.a.text)}</del>` : `<ins>${esc(o.b.text)}</ins>`}</div>`)
  }
  flushQuiet()
  return out.join('')
}
const thumbs = (ns, cls) => ns.map((n) => `<img class="${cls}" src="${src(n)}" alt="">`).join('')
function diffBody(p, u, base) {
  const now = snapOf(p, u)
  const rows = []
  const field = (label, a, b) => { if ((a ?? '') !== (b ?? '')) rows.push(`<div class="df-f"><span>${label}</span><div>${inlineDiff(a ?? '', b ?? '')}</div></div>`) }
  if (!base) return '<p class="df-note">这一处是本轮新增的。恢复会删除它。</p>'
  if (!now) return '<p class="df-note">这一处已被删除。</p>'
  if (u.kind === 'section') {
    field('一句话', base.headline, now.headline)
    const body = bodyDiff(base.body, now.body)
    if (blocksOf(base.body).map((x) => x.text).join() !== blocksOf(now.body).map((x) => x.text).join()) rows.push(`<div class="df-body-text">${body}</div>`)
    const ids = (xs) => xs.map((r) => r.id)
    const added = now.refs.filter((r) => !ids(base.refs).includes(r.id))
    const removed = base.refs.filter((r) => !ids(now.refs).includes(r.id))
    if (added.length || removed.length) rows.push(`<div class="df-f"><span>参考图</span><div class="df-imgs">${thumbs(added.map((r) => r.n), 'add')}${thumbs(removed.map((r) => r.n), 'del')}<small>${added.length ? `新增 ${added.length} 张` : ''}${added.length && removed.length ? '，' : ''}${removed.length ? `去掉 ${removed.length} 张` : ''}</small></div></div>`)
  } else if (u.kind === 'setting') {
    field('名称', base.name, now.name)
    field('说明', base.notes, now.notes)
    const a = base.views.map((v) => v.n).join()
    if (a !== now.views.map((v) => v.n).join()) rows.push(`<div class="df-f"><span>设定图</span><div class="df-imgs">${thumbs(base.views.map((v) => v.n), 'del')}<i>→</i>${thumbs(now.views.map((v) => v.n), 'add')}</div></div>`)
  } else if (u.kind === 'shot') {
    field('标题', base.title, now.title)
    field('景别', base.framing, now.framing)
    for (const [k, l] of [['action', '动作'], ['expression', '表情'], ['composition', '构图'], ['lighting', '光线'], ['notes', '备注']]) field(l, base[k], now[k])
    if (base.board?.n !== now.board?.n) rows.push(`<div class="df-f"><span>分镜图</span><div class="df-imgs">${base.board ? thumbs([base.board.n], 'del') : '<small>无</small>'}<i>→</i>${now.board ? thumbs([now.board.n], 'add') : '<small>无</small>'}</div></div>`)
  } else {
    for (const [k, l] of [['work', '作品'], ['character', '角色'], ['variant', '版本'], ['location', '地点'], ['constraints', '约束']]) field(l, base[k], now[k])
    field('拍摄形式', base.forms.join(' / '), now.forms.join(' / '))
  }
  return rows.join('') || '<p class="df-note">内容和对照版本一致。</p>'
}
// 每个单元下方的对照面板：确认后的改动、或助手某一轮的改动
function diffPanelHTML(p, u) {
  const d = diffs.get(uk(u))
  if (!d) return ''
  const canConfirm = u.kind === 'section' || u.kind === 'setting'
  const x = unitGet(p, u)
  const confirmed = canConfirm && x && x.confirmedRev === x.rev
  const title = d.mode === 'confirmed' ? `自确认版本 #${d.from} 以来改了什么` : `助手本轮改了什么 · ${esc(d.run)}`
  return `<div class="df" data-diff="${uk(u)}" role="region" aria-label="${esc(title)}">
    <header>${I('history')}<b>${title}</b><span class="grow"></span><button class="icon-btn" data-df="close" aria-label="收起对照">${I('x')}</button></header>
    <div class="df-main">${diffBody(p, u, d.base)}</div>
    <footer><button class="btn" data-df="history">在历史版本里看</button><span class="grow"></span>
      <button class="btn" data-df="restore">${I('undo')}${d.mode === 'confirmed' ? '恢复到确认时的内容' : '恢复这一处'}</button>
      ${canConfirm ? (confirmed ? `<button class="btn ps-confirm done" data-df="close">${I('check')}已确认</button>` : `<button class="btn btn-primary" data-df="confirm">${x?.confirmedRev != null ? '看过了，重新确认' : '看过了，确认'}</button>`) : '<button class="btn btn-primary" data-df="seen">看过了</button>'}
    </footer></div>`
}

/* ───────── 画布 ───────── */
const world = document.getElementById('world')
function renderCanvas() {
  world.innerHTML = nodes.map((n) => (n.kind === 'plan' ? planNodeHTML(n) : imgNodeHTML(n))).join('')
  hydrate(world)
}
function imgNodeHTML(n) {
  return `<div class="pn pn-img${selected === n.id ? ' sel' : ''}" data-node="${n.id}" style="left:${n.x}px;top:${n.y}px;width:${n.w}px;height:${n.h}px">
    <div class="pn-label">${I('image')}${esc(n.title)}</div><div class="pn-card"><img src="${src(n.n)}" alt="" draggable="false"></div></div>`
}
function planNodeHTML(n) {
  const p = plans.get(n.doc)
  const type = TYPES.find((t) => t.key === p.type)
  const sel = selected === n.id
  const tools = sel ? `<div class="pn-tools"><button class="pn-tool" data-act="open">${I('maximize')}打开策划</button><button class="pn-tool" data-act="rename">重命名</button><button class="pn-tool" data-act="more" aria-label="更多">${I('ellipsis')}</button></div>` : ''
  const label = `<div class="pn-label">${I('clapper')}${esc(p.title)}</div>`
  const blank = p.sections.every(isBlank) && !p.shots.items.length && !p.brief.character
  if (blank) {
    return `<div class="pn pp empty${sel ? ' sel' : ''}" data-node="${n.id}" style="left:${n.x}px;top:${n.y}px;width:${n.w}px;height:${n.h}px">${tools}${label}
      <div class="pn-card"><div class="pp-empty">${I('clapper')}<div>新的${esc(type.name)}策划<br>双击打开填写，或让助手起草</div>
      <div class="pp-empty-acts"><button class="btn" data-act="open">打开</button><button class="btn" data-act="draft">${I('sparkles')}让助手起草</button></div></div>
      <div class="pp-body"><span class="pp-kind">${I(type.icon)}${esc(type.name)}</span><div class="pp-foot"><span class="pp-dots">${dotsHTML(p)}</span><span>0 / 9 已确认</span></div></div></div></div>`
  }
  const cover = coverImgs(p, 3).map((c) => `<img src="${src(c)}" alt="" draggable="false">`).join('')
  const dir = p.sections.find((s) => s.key === 'creative_direction')
  const headline = dir.headline || p.sections.find((s) => s.headline)?.headline || ''
  const confirmed = p.sections.filter((s) => stateOf(s) === 'confirmed').length
  const st = shotStats(p)
  const counts = `<span>已规划 <b>${st.planned}/${TARGET}</b></span><span>分镜图 <b>${st.boards}/${TARGET}</b></span>`
  return `<div class="pn pp${sel ? ' sel' : ''}" data-node="${n.id}" style="left:${n.x}px;top:${n.y}px;width:${n.w}px;height:${n.h}px">${tools}${label}
    <div class="pn-card"><div class="pp-cover">${cover}</div><span class="pp-open">${I('maximize')}双击打开</span>
    <div class="pp-body"><span class="pp-kind">${I(type.icon)}${esc(type.name)}</span>
      <h3 class="pp-title">${esc(p.title)}</h3>
      <p class="pp-sub">${esc([p.brief.work, p.brief.character && `${p.brief.character}${p.brief.variant ? `（${p.brief.variant}）` : ''}`].filter(Boolean).join(' · '))}</p>
      <p class="pp-head">${esc(headline)}</p>
      <div class="pp-foot"><span class="pp-dots">${dotsHTML(p)}</span><span class="pp-shots">${counts}</span></div>
      <div class="pp-foot" style="border:0;padding:0;margin-top:-4px"><span>${confirmed} / ${p.sections.length} 章已确认${staleCount(p) ? ` · ${staleCount(p)} 张分镜待更新` : ''}</span><span>#${p.revision}</span></div>
    </div></div></div>`
}
const dotsHTML = (p) => ORDER.map((k) => { const st = k === 'shots' ? shotsState(p) : k === 'visual_settings' ? settingsState(p) : stateOf(p.sections.find((s) => s.key === k)); const t = k === 'shots' ? '分镜与交付' : k === 'visual_settings' ? '视觉设定' : tmplOf(k).title; return `<span class="st" data-s="${st}" title="${esc(t)} · ${STATE_TEXT[st]}"></span>` }).join('')
function coverImgs(p, k) {
  const out = []
  for (const sl of ['kv', 'half', 'closeup']) for (const s of p.shots.items) if (s.slot === sl && s.board) out.push(s.board.n)
  for (const x of p.settings.items) if (x.kind === 'scene' && x.views[0]) out.push(x.views[0].n)
  for (const r of allRefs(p)) out.push(r.n)
  return [...new Set(out)].slice(0, k)
}

document.getElementById('canvas').addEventListener('pointerdown', (e) => {
  const el = e.target.closest('[data-node]')
  if (e.target.closest('.pn-tools, [data-act]')) return
  const next = el?.dataset.node ?? null
  if (next !== selected) { selected = next; renderCanvas() }
})
document.getElementById('canvas').addEventListener('click', (e) => {
  const act = e.target.closest('[data-act]')?.dataset.act
  const nodeEl = e.target.closest('[data-node]')
  if (!act || !nodeEl) return
  const n = nodes.find((x) => x.id === nodeEl.dataset.node)
  if (act === 'open') openEditor(n)
  if (act === 'draft') { openEditor(n); setTimeout(() => askAgent({ kind: 'doc' }, '按 SOP 起草这份策划，先从角色调研开始'), 300) }
  if (act === 'rename') { const t = prompt('策划名称', plans.get(n.doc).title); if (t?.trim()) { savePlanTitle(plans.get(n.doc), t.trim()); renderCanvas() } }
  if (act === 'more') openMenu([{ label: '打开策划', icon: 'maximize', run: () => openEditor(n) }, { label: '复制为新方案', disabled: true, hint: '后续' }, 'sep', { label: '从画布移除', hint: '策划仍保留', run: () => { nodes = nodes.filter((x) => x !== n); renderCanvas(); toast('已从画布移除，可在「引用已有策划」找回', { label: '撤销', run: () => { nodes.push(n); renderCanvas() } }) } }], { rect: e.target.closest('button').getBoundingClientRect() })
})
document.getElementById('canvas').addEventListener('dblclick', (e) => {
  const el = e.target.closest('[data-node]')
  const n = el && nodes.find((x) => x.id === el.dataset.node)
  if (n?.kind === 'plan') openEditor(n)
})

/* 新建策划：选择类型 */
const railAdd = document.getElementById('railAdd')
railAdd.addEventListener('click', () => {
  const pop = openPopover(`<div class="ptype"><h4>新建拍摄策划 · 选择类型</h4>${TYPES.map((t) => `<button class="ptype-item" data-type="${t.key}" ${t.ready ? '' : 'disabled'}>
    <span class="ptype-ic">${I(t.icon)}</span><span><b>${esc(t.name)}</b><small>${esc(t.desc)}</small></span><span class="ptype-tag">${t.ready ? '' : '即将推出'}</span></button>`).join('')}</div>`, { rect: railAdd.getBoundingClientRect() }, railAdd)
  pop.style.left = `${railAdd.getBoundingClientRect().right + 10}px`
  pop.style.top = `${railAdd.getBoundingClientRect().top}px`
  pop.addEventListener('click', (e) => {
    const t = e.target.closest('[data-type]')
    if (!t || t.disabled) return
    closeFloating()
    const p = emptyPlan()
    plans.set(p.id, p)
    const n = { id: id('n'), kind: 'plan', doc: p.id, x: 260, y: -250, w: 300, h: 280 }
    nodes.push(n)
    selected = n.id
    renderCanvas()
    toast('已新建 Cosplay 拍摄策划', { label: '撤销', run: () => { nodes = nodes.filter((x) => x !== n); renderCanvas() } })
  })
})

/* ───────── 编辑视图 ───────── */
const editor = document.getElementById('editor')
let spy

function openEditor(n) {
  if (current?.id !== n.doc) diffs.clear()
  current = plans.get(n.doc)
  previewRev = null
  shotPick = null
  side = null
  if (!panel.draft.text.trim()) panel.draft.skill = true // 在策划里发消息默认带模板对应的 Skill
  canvasAgent.innerHTML = '' // 面板状态随之进入编辑视图右栏
  const r = document.querySelector(`[data-node="${n.id}"]`)?.getBoundingClientRect()
  if (r) { editor.style.setProperty('--ox', `${r.left + r.width / 2}px`); editor.style.setProperty('--oy', `${r.top + r.height / 2}px`) }
  editor.hidden = false
  editor.classList.remove('closing')
  editor.classList.add('opening')
  setTimeout(() => editor.classList.remove('opening'), 280)
  renderEditor()
  editor.querySelector('.pe-doc').scrollTop = 0
}
function closeEditor() {
  flushAll()
  if (offline && saving.size) toast('有内容尚未保存，已留在本机，下次打开时继续提交')
  shotPick = null
  editor.classList.add('closing')
  setTimeout(() => { editor.hidden = true; editor.classList.remove('closing'); renderCanvas(); renderCanvasAgent() }, 190)
}

function renderEditor({ keepScroll = false } = {}) {
  const p = current
  // 重绘前把还没保存的正文草稿留在模型里，避免整页重绘吞掉输入
  editor.querySelectorAll('.rich').forEach((el) => { if (saving.has(`body:${el.dataset.rich}`)) sec(el.dataset.rich).body = el.innerHTML })
  const doc = editor.querySelector('.pe-doc')
  const top = keepScroll && doc ? doc.scrollTop : 0
  const type = TYPES.find((t) => t.key === p.type)
  editor.innerHTML = `
    <header class="pe-top">
      <button class="icon-btn" data-ed="close" aria-label="还原节点" data-kbd="Esc">${I('minimize')}</button>
      <button class="pe-kind" data-ed="type">${I(type.icon)}${esc(type.name)} · 模板 v${TEMPLATE.version}</button>
      <button class="pe-cur" data-ed="toc" aria-label="章节目录">${I('list')}<span class="t">基本信息</span>${I('chevron-down')}</button>
      <span class="pe-grow"></span>
      <span class="pe-save" id="saveState"><span class="dot"></span><span class="t"></span></span>
      <span class="pe-sep"></span>
      <button class="icon-btn" data-ed="history" aria-label="历史版本" aria-pressed="${side === 'history'}">${I('history')}</button>
      <button class="icon-btn pe-agent-btn" data-ed="agent" aria-label="助手" aria-pressed="${agentInEditor()}" data-state="${panel.running ? 'run' : ''}">${I('sparkles')}</button>
      <button class="icon-btn" data-ed="more" aria-label="更多">${I('ellipsis')}</button>
    </header>
    <div class="pe-main${side === 'history' ? ' with-drawer' : agentInEditor() ? ' with-agent' : ''}">
      <nav class="pe-toc" aria-label="章节">${tocHTML(p)}</nav>
      <div class="pe-doc">${previewRev ? previewBar() : ''}<div class="pe-col">${coverHTML(p)}${briefHTML(p)}${startHTML(p)}${p.sections.map((s) => (s.key === 'preparation' ? settingsHTML(p) : '') + sectionHTML(p, s)).join('')}${shotsHTML(p)}</div></div>
      ${side === 'history' ? drawerHTML(p) : agentInEditor() ? agentHTML('editor') : ''}
    </div>`
  hydrate(editor)
  decorateChips(editor)
  updateSave()
  const d = editor.querySelector('.pe-doc')
  d.scrollTop = top
  if (previewRev) d.classList.add('previewing')
  setupSpy()
  scrollAgentLog()
}

// 目录条目：宽屏画在左栏，窄屏收进顶栏的「目录」菜单
function tocEntries(p) {
  const out = []
  p.sections.forEach((s) => {
    if (s.key === 'preparation') out.push({ key: 'visual_settings', title: '视觉设定', st: settingsState(p), stage: tmplOf(s.key).stage })
    out.push({ key: s.key, title: tmplOf(s.key).title, st: stateOf(s), stage: tmplOf(s.key).stage, na: s.na })
  })
  const ss = shotStats(p)
  out.push({ key: 'shots', title: '分镜与交付', st: shotsState(p), stage: '交付', n: `${ss.planned}/${TARGET}` })
  return out
}
const tocTitle = (key) => (key === 'brief' ? '基本信息' : key === 'visual_settings' ? '视觉设定' : key === 'shots' ? '分镜与交付' : tmplOf(key)?.title ?? '')
function tocHTML(p) {
  let html = ''
  let stage = ''
  for (const e of tocEntries(p)) {
    if (e.stage !== stage && e.key !== 'visual_settings') { stage = e.stage; html += `<div class="pe-toc-stage">${stage}</div>` }
    html += `<a href="#sec-${e.key}" data-toc="${e.key}" class="${e.na ? 'is-na' : ''}" aria-label="${esc(e.title)} · ${STATE_TEXT[e.st]}"><span class="st" data-s="${e.st}"></span><span>${esc(e.title)}</span><span class="n">${e.n ?? numOf(e.key)}</span></a>`
  }
  const confirmed = p.sections.filter((s) => stateOf(s) === 'confirmed').length
  const applicable = p.sections.filter((s) => !s.na).length
  html += `<div class="pe-toc-foot"><span>已确认 ${confirmed} / ${applicable} 章</span><div class="pe-progress"><i style="width:${(confirmed / Math.max(applicable, 1)) * 100}%"></i></div>
    <div class="pe-legend">${['empty', 'draft', 'confirmed', 'changed', 'na'].map((k) => `<span><i class="st" data-s="${k}"></i>${STATE_TEXT[k]}</span>`).join('')}</div></div>`
  return html
}

// 还没有方向、设定和分镜的策划：封面收起，首屏先给基本信息和起草入口
const isFresh = (p) => !p.shots.items.some((s) => s.board) && !p.settings.items.length && !p.sections.find((s) => s.key === 'creative_direction').headline
function coverHTML(p) {
  const type = TYPES.find((t) => t.key === p.type)
  const dir = p.sections.find((s) => s.key === 'creative_direction')
  const boards = p.shots.items.filter((s) => s.board).slice(0, 4)
  const fresh = isFresh(p)
  const strip = boards.length
    ? boards.map((s) => `<figure><img src="${src(s.board.n)}" alt=""><figcaption>${shotNo(p, s)} · ${esc(s.title)}</figcaption></figure>`).join('')
    : '<figure style="display:grid;place-items:center;color:var(--fg-3);font-size:12px;grid-column:span 3">分镜生成后显示在这里</figure>'
  return `<section class="pe-cover${fresh ? ' fresh' : ''}">
    <div><span class="pe-cover-kind">${I(type.icon)}${esc(type.name)}策划</span>
      <h1 contenteditable="plaintext-only" spellcheck="false" data-unit="title">${esc(p.title)}</h1>
      <p class="pe-cover-sub">${esc([p.brief.work, p.brief.character && `${p.brief.character}${p.brief.variant ? `（${p.brief.variant}）` : ''}`, p.brief.forms.join(' / ')].filter(Boolean).join(' · ') || '先填写作品、角色和拍摄限制')}</p>
      ${dir.headline ? `<p class="pe-cover-line"><span>创意方向</span>${esc(dir.headline)}</p>` : ''}
    </div>
    ${fresh ? '' : `<div class="pe-strip">${strip}</div>`}
  </section>`
}
const startHTML = (p) => (isFresh(p) ? `<div class="pe-start">${I('sparkles')}<div><b>填好作品、角色和拍摄限制，就可以开始</b><span>助手会先调研角色，给出 2–3 个创意方向让你选，再往下写。</span></div><button class="btn btn-primary" data-ed="start">根据这些信息起草方向</button></div>` : '')

function briefHTML(p) {
  const b = p.brief
  const f = (k, label, ph) => `<div class="pe-field"><label for="bf-${k}">${label}</label><input id="bf-${k}" data-brief="${k}" value="${esc(b[k])}" placeholder="${ph}"></div>`
  const composite = b.forms.includes('合成')
  return `<div id="sec-brief" data-unit-box="brief"><section class="pe-brief">
    ${f('work', '作品', '如：原神')}${f('character', '角色', '如：芙宁娜')}${f('variant', '版本 / 形态', '如：常服、皮肤名')}
    <div class="pe-field"><label>拍摄形式</label><div class="pe-seg">${['棚拍', '外景', '合成'].map((x) => `<button data-form="${x}" aria-pressed="${b.forms.includes(x)}">${x}</button>`).join('')}</div></div>
    <div class="pe-field"><label for="bf-location">地点</label><input id="bf-location" data-brief="location" value="${esc(b.location)}" placeholder="${composite ? '合成片一般为无影棚' : '棚名或外景地'}">${composite && !b.location ? '<span class="hint">合成片场地一般为无影棚</span>' : ''}</div>
    ${f('constraints', '约束', '交付数量、预算、时间、器材')}
  </section>${diffPanelHTML(p, { kind: 'brief' })}</div>`
}
function rerenderBrief() {
  const el = editor.querySelector('#sec-brief')
  if (!el) return
  el.outerHTML = briefHTML(current)
  hydrate(editor.querySelector('#sec-brief'))
}

function sectionHTML(p, s) {
  const t = tmplOf(s.key)
  const st = stateOf(s)
  const confirmBtn = confirmBtnHTML(s)
  const groups = groupOrder(s)
  const refs = groups.map((g) => {
    const list = s.refs.filter((r) => r.group === g)
    if (!list.length && t.groups.length === 1) return ''
    return `<div class="rg"><div class="rg-head">${esc(g)}<span class="c">${list.length}</span></div><div class="rg-row">${list.map((r) => refHTML(p, r)).join('')}<button class="rf-add" data-add="${s.key}" data-group="${esc(g)}">${I('plus')}添加参考图</button></div></div>`
  }).join('')
  const singleAdd = t.groups.length === 1 && !s.refs.length ? `<div class="rg"><div class="rg-row"><button class="rf-add" data-add="${s.key}" data-group="${esc(t.groups[0])}" style="width:auto;height:36px;padding:0 12px;display:inline-flex;gap:6px">${I('plus')}添加参考图</button></div></div>` : ''
  return `<section class="ps${s.na ? ' is-na' : ''}${s.agentFlag === 'writing' ? ' agent-writing' : ''}" id="sec-${s.key}" data-sec="${s.key}">
    <div class="ps-head"><span class="ps-no">${numOf(s.key)}</span><h2>${esc(t.title)}</h2><span class="ps-state"><span class="st" data-s="${st}"></span>${STATE_TEXT[st]}</span>
      <div class="ps-acts">${s.na ? '' : confirmBtn}<button class="icon-btn" data-sec-act="ask" aria-label="请助手修改这一章" title="请助手修改：可先选中一段文字">${I('sparkles')}</button><button class="icon-btn" data-sec-act="na" aria-label="${s.na ? '恢复适用' : '本次不适用'}" aria-pressed="${s.na}">${I('eye-off')}</button><button class="icon-btn" data-sec-act="more" aria-label="更多">${I('ellipsis')}</button></div></div>
    ${s.na ? `<p class="ps-na-note">本次拍摄不需要这一章。<button class="btn" data-sec-act="na" style="height:26px;margin-left:8px;font-size:12px">恢复</button></p>` : ''}
    <div class="ps-body-wrap">
      <input class="ps-headline" data-headline="${s.key}" value="${esc(s.headline)}" placeholder="一句话概括这一章">
      <div class="ps-flags">${flagsHTML(s)}</div>
      ${diffPanelHTML(p, { kind: 'section', key: s.key })}
      ${s.conflict ? conflictHTML(s) : ''}
      <div class="ps-inner">
        <div class="ps-editor">
          <div class="ps-bar" role="toolbar" aria-label="格式">
            <button data-fmt="bold" aria-label="加粗">${I('bold')}</button><button data-fmt="italic" aria-label="斜体">${I('italic')}</button><button data-fmt="h3" aria-label="小标题">${I('heading')}</button><i></i>
            <button data-fmt="ul" aria-label="列表">${I('list')}</button><button data-fmt="task" aria-label="待办列表">${I('todo')}</button><button data-fmt="quote" aria-label="引用块">${I('quote')}</button><button data-fmt="link" aria-label="链接">${I('link')}</button><i></i>
            <button class="wide" data-fmt="ref">${I('at')}引用参考</button>
          </div>
          <div class="rich" contenteditable="true" spellcheck="false" data-rich="${s.key}" data-ph="${esc(t.ph)}">${s.body}</div>
        </div>
        <div class="ps-refs">${refs}${singleAdd}</div>
      </div>
    </div>
  </section>`
}
function confirmBtnHTML(s) {
  const st = stateOf(s)
  return st === 'confirmed'
    ? `<button class="btn ps-confirm done" data-sec-act="unconfirm">${I('check')}已确认</button>`
    : `<button class="btn btn-primary ps-confirm" data-sec-act="confirm" ${st === 'empty' || st === 'na' ? 'disabled style="opacity:.35"' : ''}>${st === 'changed' ? '重新确认' : '确认本章'}</button>`
}
function flagsHTML(s) {
  const st = stateOf(s)
  const flags = []
  const open = diffs.has(`section:${s.key}`)
  if (st === 'changed' && !open) flags.push(`<div class="ps-flag">${I('alert')}确认后又改过（#${s.confirmedRev} → #${s.rev}），看过后重新确认 · <button data-sec-act="diff">看改动</button></div>`)
  if (s.agentFlag === 'writing') flags.push('<div class="ps-flag run"><span class="dot"></span>助手正在写这一章…</div>')
  if (s.agentFlag === 'done' && !open) flags.push(`<div class="ps-flag">${I('sparkles')}助手刚修改了这一章（#${s.rev}）· <button data-sec-act="run-diff">看改动</button></div>`)
  return flags.join('')
}
function refHTML(p, r) {
  const [w, h] = PH[r.n]
  return `<figure class="rf" data-ref-id="${r.id}" style="width:${Math.round((132 * w) / h)}px"><img src="${src(r.n)}" alt="" draggable="false"><span class="rf-no">${refNo(p, r.id)}</span>
    <figcaption class="rf-cap">${esc(r.caption)}${r.source ? `<small>${esc(r.source.site)}${r.source.author ? ` · ${esc(r.source.author)}` : ''}</small>` : ''}</figcaption>
    <button class="rf-menu" data-ref-menu="${r.id}" aria-label="引用操作">${I('ellipsis')}</button></figure>`
}
// 冲突：并排看两边改了什么；可以整章择一，也可以在下方编辑出合并后的最终内容，参考图逐张保留
function conflictHTML(s) {
  const c = s.conflict
  const cols = `<div class="cols"><div class="col"><b>助手的版本 · #${c.rev}</b>${c.theirs}</div><div class="col"><b>你的修改（未保存）</b>${c.mine}</div></div>`
  if (!c.merging) {
    return `<div class="ps-conflict" role="alert"><header>${I('alert')}这一章刚被助手修改，你的修改还没保存</header>${cols}
      <footer><button class="btn" data-sec-act="take">用助手的版本</button><button class="btn" data-sec-act="keep">保留我的</button><button class="btn btn-primary" data-sec-act="merge">合并后保存</button></footer></div>`
  }
  const union = [...c.mineRefs, ...c.theirsRefs.filter((r) => !c.mineRefs.some((m) => m.id === r.id))]
  const side = (r) => { const a = c.mineRefs.some((m) => m.id === r.id); const b = c.theirsRefs.some((m) => m.id === r.id); return a && b ? '两边都有' : a ? '只在你的版本' : '助手新加' }
  const differ = union.some((r) => side(r) !== '两边都有')
  return `<div class="ps-conflict merging" role="region" aria-label="合并两边的修改"><header>${I('alert')}合并两边的修改：在下方编辑出最终内容</header>${cols}
    <div class="mg"><b>最终内容（可编辑，先填入了你的版本）</b><div class="rich mg-edit" contenteditable="true" spellcheck="false" data-merge="${s.key}">${c.draft ?? c.mineBody}</div>
    ${differ ? `<b>参考图（逐张决定保留）</b><div class="mg-refs">${union.map((r) => `<label class="mg-ref"><input type="checkbox" data-mg-ref="${r.id}" ${c.keepRefs.includes(r.id) ? 'checked' : ''}><img src="${src(r.n)}" alt=""><span>${esc(r.caption)}<small>${side(r)}</small></span></label>`).join('')}</div>` : ''}</div>
    <footer><button class="btn" data-sec-act="merge-cancel">返回</button><button class="btn btn-primary" data-sec-act="merge-save">保存合并结果</button></footer></div>`
}

function settingsHTML(p) {
  const st = settingsState(p)
  const card = (x) => {
    const xs = setState(x)
    const views = x.views.map((v) => `<figure class="sv"><img src="${src(v.n)}" alt=""><figcaption>${esc(v.view)}</figcaption></figure>`).join('')
    const basis = x.basis.filter((rid) => findRef(p, rid)).map(chip).join('')
    const used = p.shots.items.filter((sh) => sh.sets?.includes(x.id)).length
    const stale = staleCount(p, x.id)
    const btn = xs === 'confirmed' ? `<button class="btn ps-confirm done" data-set-act="unconfirm">${I('check')}已确认</button>` : `<button class="btn btn-primary ps-confirm" data-set-act="confirm">${xs === 'changed' ? '重新确认' : '确认设定'}</button>`
    return `<article class="set-card" data-set="${x.id}" data-kind="${x.kind}">
      <header class="set-head"><span class="set-kind">${x.kind === 'character' ? '人设' : '场景'}</span><h3>${esc(x.name)}</h3><span class="ps-state"><span class="st" data-s="${xs}"></span>${STATE_TEXT[xs]}</span>
        <span class="set-used">${used} 个镜头使用</span><div class="ps-acts">${btn}<button class="icon-btn" data-set-act="more" aria-label="设定操作">${I('ellipsis')}</button></div></header>
      ${xs === 'changed' && !diffs.has(`setting:${x.id}`) ? `<div class="ps-flag set-flag">${I('alert')}设定改过（#${x.confirmedRev} → #${x.rev}）${stale ? `，${stale} 张分镜基于旧设定` : ''}，看过后重新确认 · <button data-set-act="diff">看改动</button></div>` : ''}
      ${diffPanelHTML(p, { kind: 'setting', id: x.id })}
      <div class="set-views">${views}<button class="set-add" data-set-act="view">${I('plus')}${x.kind === 'character' ? '加细节图' : '派生角度'}</button></div>
      <p class="set-notes">${esc(x.notes)}</p>
      ${basis ? `<p class="set-basis"><span>生成依据</span>${basis}</p>` : ''}
    </article>`
  }
  const group = (kind, title, desc) => `<div class="sh-group"><div class="sh-group-head"><h3>${title}</h3><span>${desc}</span></div>
    <div class="set-list">${p.settings.items.filter((x) => x.kind === kind).map(card).join('')}<button class="sh-add" data-set-add="${kind}">${I('plus')}添加${kind === 'character' ? '人设' : '场景'}</button></div></div>`
  return `<section class="ps" id="sec-visual_settings" data-sec="visual_settings">
    <div class="ps-head"><span class="ps-no">${numOf('visual_settings')}</span><h2>视觉设定</h2><span class="ps-state"><span class="st" data-s="${st}"></span>${STATE_TEXT[st]}</span>
      <div class="ps-acts"><button class="btn" data-set-add="gen">${I('sparkles')}让助手生成设定</button></div></div>
    <p class="ps-na-note" style="color:var(--fg-2)">分镜以这里的设定为参考生成。先定人设与场景并确认，各镜头的角色和场景才能一致，后期也能照着复刻。</p>
    ${group('character', '人设', '三视图 · 由助手基于搜集或你提供的角色资料生成')}
    ${group('scene', '场景', '主场景图定下空间、陈设与光线，其余角度由它派生')}
  </section>`
}
function rerenderSettings() {
  const el = editor.querySelector('[data-sec="visual_settings"]')
  el.outerHTML = settingsHTML(current)
  hydrate(editor.querySelector('[data-sec="visual_settings"]'))
  decorateChips(editor)
  rerenderShots()
}
// 选分镜图生成范围时的默认勾选：没有图的、基于旧设定的
const needsBoard = (p, s) => !s.board || isStale(p, s)
function shotsHTML(p) {
  const st = shotStats(p)
  const groups = TEMPLATE.slots.map((sl) => {
    const list = p.shots.items.filter((s) => s.slot === sl.key)
    const head = `<div class="sh-group-head"><h3>${sl.name}</h3><span>${sl.desc}</span><span class="cnt${list.length < sl.target ? ' short' : ''}">${list.length} / ${sl.target}</span></div>`
    if (shotView === 'grid') {
      const holes = Array.from({ length: Math.max(0, sl.target - list.length) }, (_, k) => `<div class="sg-tile hole"><span class="sh-no">${slotPrefix(sl.key)}${String(list.length + k + 1).padStart(2, '0')}</span><span>待补</span></div>`).join('')
      return `<div class="sh-group">${head}<div class="sg-row" data-slot="${sl.key}">${list.map((s) => tileHTML(p, s)).join('')}${holes}</div></div>`
    }
    return `<div class="sh-group">${head}<div class="sh-list">${list.map((s) => shotHTML(p, s)).join('')}<button class="sh-add" data-shot-add="${sl.key}">${I('plus')}添加${sl.name}镜头</button></div></div>`
  }).join('')
  const pickBar = shotPick ? `<div class="sh-pickbar" role="region" aria-label="选择要生成分镜图的镜头"><span>已选 <b>${shotPick.size}</b> 格</span><span class="sep"></span><span class="lbl">风格</span><button class="sh-chip" data-pick="style">${esc(pickStyle)}${I('chevron-down')}</button>
      <span class="grow"></span><button class="btn" data-pick="cancel">取消</button><button class="btn btn-primary" data-pick="next" ${shotPick.size ? '' : 'disabled style="opacity:.4"'}>交给助手，下一步看消耗</button></div>` : ''
  return `<section class="ps${shotPick ? ' picking' : ''}" id="sec-shots" data-sec="shots">
    <div class="ps-head"><span class="ps-no">${numOf('shots')}</span><h2>分镜与交付</h2><span class="ps-state"><span class="st" data-s="${shotsState(p)}"></span>${st.total} 个镜头</span>
      <div class="ps-acts"><div class="seg-toggle" role="group" aria-label="镜头视图"><button data-shot-view="cards" aria-pressed="${shotView === 'cards'}" aria-label="逐张卡片">${I('rows')}</button><button data-shot-view="grid" aria-pressed="${shotView === 'grid'}" aria-label="紧凑总览">${I('grid')}</button></div>
        <button class="btn" data-ed="fill">补齐镜头方案</button><button class="btn" data-ed="pick">${I('sparkles')}生成分镜图</button></div></div>
    <div class="sh-stats">${statsHTML(p)}</div>
    <p class="ps-na-note" style="color:var(--fg-2)">商业交付标准 2 + 4 + 3。数量是目标值，可以多也可以少。${shotView === 'grid' ? '总览用来比较整组的动作、构图和情绪，点一格打开细调。' : ''}</p>
    ${groups}${pickBar}</section>`
}
function statsHTML(p) {
  const st = shotStats(p)
  return `<span><b>${st.planned}</b> / ${TARGET} 已规划<small>动作、构图、光线齐全</small></span><span><b>${st.boards}</b> / ${TARGET} 分镜图可用</span>${st.stale ? `<span class="warn">${I('alert')}${st.stale} 张基于旧设定</span>` : ''}`
}
// 紧凑总览里的一格：分镜图、编号、标题、动作；缺字段和旧设定直接标出
function tileHTML(p, s) {
  const miss = missingOf(s)
  const picked = shotPick?.has(s.id)
  return `<button class="sg-tile${picked ? ' picked' : ''}" data-tile="${s.id}" ${shotPick ? `aria-pressed="${picked}"` : ''}>
    ${s.board ? `<img src="${src(s.board.n)}" alt="">` : '<span class="sg-empty">无分镜图</span>'}<span class="sh-no">${shotNo(p, s)}</span>
    ${shotPick ? `<span class="sh-check" aria-hidden="true">${picked ? I('check') : ''}</span>` : ''}
    <span class="sg-cap"><b>${esc(s.title)}</b><span>${esc(s.action || '—')}</span>${miss.length ? `<em>待补 ${miss.join('、')}</em>` : isStale(p, s) ? `<em>${I('alert')}设定已更新</em>` : ''}</span></button>`
}
function shotHTML(p, s) {
  const no = shotNo(p, s)
  const miss = missingOf(s)
  const check = shotPick ? `<button class="sh-check" data-pick-id="${s.id}" aria-pressed="${shotPick.has(s.id)}" aria-label="选择 ${no}">${shotPick.has(s.id) ? I('check') : ''}</button>` : ''
  const board = s.board
    ? `<div class="sh-board"><img src="${src(s.board.n)}" alt=""><span class="sh-no">${no}</span>${check}<div class="tagrow"><span class="sh-tag">${esc(s.board.style)}</span></div>${isStale(p, s) ? `<div class="sh-stale">${I('alert')}设定已更新<button data-regen="${s.id}">重新生成</button></div>` : ''}</div>`
    : `<div class="sh-board empty"><span class="sh-no">${no}</span>${check}${I('image')}<span>还没有分镜图</span><div class="row"><button class="btn" data-board="${s.id}">添加分镜图</button><button class="btn" data-regen="${s.id}">${I('sparkles')}生成</button></div></div>`
  const fld = (k, label) => `<dt>${label}</dt><dd contenteditable="plaintext-only" data-shot="${s.id}" data-field="${k}">${esc(s[k])}</dd>`
  const refs = s.refs.map((rid) => { const r = findRef(p, rid); return r ? `<img src="${src(r.n)}" alt="参考 ${refNo(p, rid)}" title="参考 ${refNo(p, rid)} · ${esc(r.caption)}">` : '' }).join('')
  const sets = s.sets?.length
    ? s.sets.map((sid) => { const x = findSet(p, sid); return x ? `<span class="set-chip"><img src="${src(x.views[0].n)}" alt="">${esc(x.name)}</span>` : '' }).join('')
    : '<span class="set-chip none">未关联设定</span>'
  return `<article class="sh${shotPick?.has(s.id) ? ' picked' : ''}" data-slot="${s.slot}" data-shot-id="${s.id}">${board}
    <div class="sh-main"><div class="sh-top"><input class="sh-title" data-shot="${s.id}" data-field="title" value="${esc(s.title)}">
      ${miss.length ? `<span class="sh-miss">待补 ${miss.join('、')}</span>` : ''}
      <button class="sh-chip" data-shot-chip="framing" data-id="${s.id}">景别 · ${esc(s.framing)}${I('chevron-down')}</button>
      ${s.board ? `<button class="sh-chip" data-shot-chip="style" data-id="${s.id}">${esc(s.board.style)}${I('chevron-down')}</button>` : ''}
      <button class="icon-btn" data-shot-more="${s.id}" aria-label="镜头操作">${I('ellipsis')}</button></div>
      <div class="sh-sets">${sets}<button class="set-chip edit" data-shot-sets="${s.id}" aria-label="选择设定">${I('plus')}</button></div>
      <dl class="sh-fields">${fld('action', '动作')}${fld('expression', '表情')}${fld('composition', '构图')}${fld('lighting', '光线')}${fld('notes', '备注')}</dl>
      <div class="sh-refs">${refs}<button class="add" data-shot-ref="${s.id}" aria-label="添加参考">${I('plus')}</button></div>
    </div>${diffPanelHTML(p, { kind: 'shot', id: s.id })}</article>`
}

function drawerHTML(p) {
  return `<aside class="pe-drawer" aria-label="历史版本"><header>历史版本<button class="icon-btn" data-ed="history" aria-label="关闭">${I('x')}</button></header>
    <div class="hv-list">${p.history.map((h, i) => `<div class="hv${previewRev === h.rev ? ' on' : ''}" data-hv="${h.rev}">
      <span class="hv-av ${h.who === 'agent' ? 'is-agent' : ''}">${h.who === 'agent' ? I('sparkles') : '你'}</span>
      <div><div class="hv-who">${h.who === 'agent' ? '助手' : '你'}<span>#${h.rev} · ${h.at}</span>${i === 0 ? ' <span class="hv-cur">当前</span>' : ''}</div>
      ${h.run ? `<div class="hv-cur">对话：${esc(h.run)}</div>` : ''}
      <div class="hv-what">${h.what.map((w) => `<span>${esc(w)}</span>`).join('')}</div>
      ${i ? '<div class="hv-acts"><button class="btn" data-hv-act="preview">预览</button><button class="btn btn-primary" data-hv-act="restore">恢复到这个版本</button></div>' : ''}</div></div>`).join('')}</div></aside>`
}
const previewBar = () => `<div class="pe-preview-bar">${I('history')}正在预览 #${previewRev} 的版本（只读）<span class="grow"></span><button class="btn" data-hv-act="exit">回到当前</button><button class="btn btn-primary" data-hv-act="restore-cur">恢复到这个版本</button></div>`

/* 正文里的引用 chip：按 ID 渲染当前编号；引用已删除时不顺延 */
function decorateChips(root) {
  root.querySelectorAll('.ref-chip').forEach((c) => {
    const r = findRef(current, c.dataset.ref)
    c.classList.toggle('gone', !r)
    c.innerHTML = r ? `<img src="${src(r.n)}" alt="">参考 ${refNo(current, r.id)}` : '引用已删除'
  })
}

/* ───────── 保存（按冲突单元串行，演示用） ─────────
   本地内容立即写进内存模型（对应 design §7.3 第 7 条的本机留存），保存任务只负责提交；
   断网时关掉再打开，看到的仍是本机内容，顶栏显示「未保存 · 内容在本机」。 */
function markDirty(unit, apply) {
  clearTimeout(saving.get(unit)?.t)
  apply()
  const delay = unit.startsWith('body:') ? 1500 : 300
  saving.set(unit, { state: 'dirty', t: setTimeout(() => commit(unit), delay) })
  updateSave()
}
function commit(unit) {
  const job = saving.get(unit)
  if (!job) return
  job.state = 'saving'
  updateSave()
  job.t = setTimeout(() => {
    if (offline) { job.state = 'retry'; updateSave(); job.t = setTimeout(() => commit(unit), 2500); return }
    current.revision += 1
    current.history.unshift({ rev: current.revision, who: 'me', at: now(), what: [unitLabel(unit)] })
    saving.delete(unit)
    refreshChrome()
  }, 500)
}
function flushAll() { for (const [u, j] of saving) if (j.state === 'dirty') { clearTimeout(j.t); commit(u) } }
function unitLabel(u) {
  const [k, v] = u.split(':')
  if (k === 'title') return '标题'
  if (k === 'brief') return '基本信息'
  if (k === 'shot') return `镜头 · ${current.shots.items.find((s) => s.id === v)?.title ?? ''}`
  return tmplOf(v)?.title ?? v
}
function updateSave() {
  const el = editor.querySelector('#saveState')
  if (!el) return
  const jobs = [...saving.values()]
  const s = jobs.some((j) => j.state === 'retry') ? 'retry' : jobs.length ? 'saving' : 'ok'
  el.dataset.s = s
  el.title = s === 'retry' ? '网络恢复后自动提交；关掉策划或刷新页面，内容也会留在这台设备上' : ''
  el.querySelector('.t').textContent = s === 'retry' ? '未保存 · 内容在本机，重试中' :s === 'saving' ? (jobs.some((j) => j.state === 'saving') ? '保存中…' : '有修改未保存') : `已保存 · #${current.revision}`
}
// 保存后只刷新状态相关的外框，不重绘正在编辑的正文
function refreshChrome() {
  editor.querySelector('.pe-toc').innerHTML = tocHTML(current)
  editor.querySelectorAll('[data-sec]').forEach((el) => {
    const s = current.sections.find((x) => x.key === el.dataset.sec)
    if (!s) return
    const st = stateOf(s)
    el.querySelector('.ps-state').innerHTML = `<span class="st" data-s="${st}"></span>${STATE_TEXT[st]}`
    const btn = el.querySelector('.ps-confirm')
    if (btn) btn.outerHTML = confirmBtnHTML(s)
    const flags = el.querySelector('.ps-flags')
    if (flags) flags.innerHTML = flagsHTML(s)
    hydrate(el.querySelector('.ps-head'))
  })
  const drawer = editor.querySelector('.pe-drawer')
  if (drawer) { drawer.outerHTML = drawerHTML(current); hydrate(editor.querySelector('.pe-drawer')) }
  const stats = editor.querySelector('.sh-stats')
  if (stats) { stats.innerHTML = statsHTML(current); hydrate(stats) }
  setupSpy()
  updateSave()
}
function rerenderSection(key) {
  const el = editor.querySelector(`[data-sec="${key}"]`)
  const i = current.sections.findIndex((s) => s.key === key)
  if (!el || i < 0) return
  el.outerHTML = sectionHTML(current, current.sections[i])
  const fresh = editor.querySelector(`[data-sec="${key}"]`)
  hydrate(fresh)
  decorateChips(editor)
  editor.querySelector('.pe-toc').innerHTML = tocHTML(current)
  setupSpy()
}
function savePlanTitle(p, t) { p.title = t; p.revision += 1; p.history.unshift({ rev: p.revision, who: 'me', at: now(), what: ['标题'] }) }
const sec = (key) => current.sections.find((s) => s.key === key)

/* ───────── 编辑视图事件 ───────── */
editor.addEventListener('click', async (e) => {
  const ed = e.target.closest('[data-ed]')?.dataset.ed
  if (ed === 'close') return closeEditor()
  if (ed === 'history') { side = side === 'history' ? null : 'history'; previewRev = null; renderEditor({ keepScroll: true }); return }
  if (ed === 'agent') return setPanelOpen(!agentInEditor())
  if (ed === 'fill') return askAgent({ kind: 'shots' }, '补齐还缺的镜头方案，只写文字，先不生成分镜图')
  if (ed === 'pick') { shotPick = new Set(current.shots.items.filter((s) => needsBoard(current, s)).map((s) => s.id)); rerenderShots(); return editor.querySelector('#sec-shots').scrollIntoView({ behavior: 'smooth', block: 'start' }) }
  if (ed === 'start') { const b = current.brief; return askAgent({ kind: 'doc' }, `根据基本信息起草：先调研${b.character || '角色'}，再给 2–3 个创意方向`) }
  if (ed === 'toc') return openMenu([{ label: '基本信息', run: () => jumpTo('#sec-brief') }, ...tocEntries(current).map((x) => ({ label: x.title, hint: STATE_TEXT[x.st], checked: x.key === curToc, run: () => jumpTo(`#sec-${x.key}`) }))], { rect: e.target.closest('button').getBoundingClientRect() })
  if (ed === 'type') return openMenu(TYPES.map((t) => ({ label: t.name, disabled: !t.ready, checked: t.key === current.type, hint: t.ready ? '' : '即将推出' })).concat(['sep', { section: '类型决定模板；已有内容的策划不能换类型' }]), { rect: e.target.closest('button').getBoundingClientRect() })
  if (ed === 'more') return openMenu([{ label: '导出 PDF', icon: 'download', disabled: true, hint: '后续' }, { label: '复制为新方案', icon: 'copy', disabled: true, hint: '后续' }, 'sep', { label: '移入回收站', danger: true, run: async () => { if (await confirmModal({ title: '把策划移入回收站？', body: '画布上的入口会显示「策划在回收站」，随时可以恢复。彻底删除要在回收站里单独操作。', ok: '移入回收站' })) { closeEditor(); toast('已移入回收站', { label: '撤销', run: () => {} }) } } }], { rect: e.target.closest('button').getBoundingClientRect(), align: 'end' })

  // 历史
  const hv = e.target.closest('[data-hv]')
  const hvAct = e.target.closest('[data-hv-act]')?.dataset.hvAct
  if (hv && !hvAct) { editor.querySelectorAll('.hv').forEach((x) => x.classList.toggle('on', x === hv)); return }
  if (hvAct === 'preview') { previewRev = Number(hv.dataset.hv); renderEditor({ keepScroll: true }); return }
  if (hvAct === 'exit') { previewRev = null; renderEditor({ keepScroll: true }); return }
  if (hvAct === 'restore' || hvAct === 'restore-cur') {
    const k = hvAct === 'restore' ? Number(hv.dataset.hv) : previewRev
    if (!(await confirmModal({ title: `恢复到 #${k}？`, body: '会生成一个新版本，之后的历史都保留。恢复之前各窗口、助手手里的旧版本号全部失效，需要重读后再改。', ok: '恢复' }))) return
    current.revision += 1
    current.history.unshift({ rev: current.revision, who: 'me', at: now(), what: [`恢复到 #${k}`] })
    previewRev = null
    renderEditor({ keepScroll: true })
    return toast(`已恢复为 #${k} 的内容，新版本 #${current.revision}`)
  }

  // 章节
  const secEl = e.target.closest('[data-sec]')
  const act = e.target.closest('[data-sec-act]')?.dataset.secAct
  if (secEl && act) {
    const s = sec(secEl.dataset.sec)
    if (act === 'confirm') { s.confirmedRev = s.rev; s.confirmedSnap = snapOf(current, { kind: 'section', key: s.key }); s.agentFlag = null; diffs.delete(`section:${s.key}`); current.revision += 1; current.history.unshift({ rev: current.revision, who: 'me', at: now(), what: [`确认 ${tmplOf(s.key).title}`] }); rerenderSection(s.key); refreshChrome(); return toast(`已确认「${tmplOf(s.key).title}」#${s.rev}`) }
    if (act === 'unconfirm') { s.confirmedRev = null; rerenderSection(s.key); return }
    if (act === 'na') { s.na = !s.na; s.rev = current.revision + 1; current.revision += 1; rerenderSection(s.key); refreshChrome(); return }
    if (act === 'history') { side = 'history'; renderEditor({ keepScroll: true }); return }
    if (act === 'diff') return openDiff({ kind: 'section', key: s.key }, { mode: 'confirmed', from: s.confirmedRev, base: s.confirmedSnap })
    if (act === 'run-diff' && s.lastRun) return showRunDiff(current.id, s.lastRun.runId, s.lastRun.i)
    if (act === 'ask') return askAgent({ kind: 'section', key: s.key }, '')
    if (act === 'take') { s.body = s.conflict.theirsBody; s.refs = s.conflict.theirsRefs; s.rev = s.conflict.rev; s.conflict = null; rerenderSection(s.key); return toast('已采用助手的版本') }
    if (act === 'keep') { s.body = s.conflict.mineBody; s.refs = s.conflict.mineRefs; s.conflict = null; current.revision += 1; s.rev = current.revision; current.history.unshift({ rev: current.revision, who: 'me', at: now(), what: [tmplOf(s.key).title] }); rerenderSection(s.key); refreshChrome(); return toast(`已用最新版本号重新提交你的修改 · #${current.revision}`) }
    if (act === 'merge') { s.conflict.merging = true; s.conflict.keepRefs = [...new Set([...s.conflict.mineRefs, ...s.conflict.theirsRefs].map((r) => r.id))]; rerenderSection(s.key); editor.querySelector(`[data-merge="${s.key}"]`)?.focus(); return }
    if (act === 'merge-cancel') { s.conflict.draft = editor.querySelector(`[data-merge="${s.key}"]`)?.innerHTML; s.conflict.merging = false; return rerenderSection(s.key) }
    if (act === 'merge-save') {
      const c = s.conflict
      const all = [...c.mineRefs, ...c.theirsRefs.filter((r) => !c.mineRefs.some((m) => m.id === r.id))]
      s.body = editor.querySelector(`[data-merge="${s.key}"]`).innerHTML
      s.refs = all.filter((r) => c.keepRefs.includes(r.id))
      s.conflict = null
      current.revision += 1
      s.rev = current.revision
      current.history.unshift({ rev: current.revision, who: 'me', at: now(), what: [`${tmplOf(s.key).title}（合并）`] })
      rerenderSection(s.key)
      refreshChrome()
      return toast(`已保存合并结果 · #${current.revision}`)
    }
    if (act === 'more') return openMenu([{ label: '请助手修改这一章', icon: 'sparkles', sub: '也可以先选中一段文字，只改那一段', run: () => askAgent({ kind: 'section', key: s.key }, '') }, { label: s.na ? '恢复适用' : '本次不适用', run: () => { s.na = !s.na; rerenderSection(s.key); refreshChrome() } }, { label: '查看这一章的历史', icon: 'clock', run: () => { side = 'history'; renderEditor({ keepScroll: true }) } }], { rect: e.target.closest('button').getBoundingClientRect(), align: 'end' })
  }

  // 视觉设定
  const setAct = e.target.closest('[data-set-act]')?.dataset.setAct
  if (setAct) {
    const x = findSet(current, e.target.closest('[data-set]').dataset.set)
    if (setAct === 'diff') return openDiff({ kind: 'setting', id: x.id }, { mode: 'confirmed', from: x.confirmedRev, base: x.confirmedSnap })
    if (setAct === 'confirm') { x.confirmedRev = x.rev; x.confirmedSnap = snapOf(current, { kind: 'setting', id: x.id }); diffs.delete(`setting:${x.id}`); current.revision += 1; current.history.unshift({ rev: current.revision, who: 'me', at: now(), what: [`确认设定 ${x.name}`] }); rerenderSettings(); refreshChrome(); return toast(`已确认「${x.name}」#${x.rev}`) }
    if (setAct === 'unconfirm') { x.confirmedRev = null; return rerenderSettings() }
    if (setAct === 'view') return askAgent({ kind: 'setting', id: x.id }, x.kind === 'character' ? '以三视图为参考，补服装和道具的细节图' : '以主场景图为参考，派生一个反打角度')
    if (setAct === 'more') return openMenu([{ label: '让助手换一版', icon: 'sparkles', run: () => askAgent({ kind: 'setting', id: x.id }, '换一版：') }, { label: '改名称与说明', icon: 'pencil', run: () => { const t = prompt('设定名称', x.name); if (t?.trim()) { x.name = t.trim(); rerenderSettings() } } }, 'sep', { label: '删除设定', danger: true, sub: '用到它的分镜会提示「设定已更新」', run: () => { current.settings.items = current.settings.items.filter((y) => y !== x); current.shots.items.forEach((sh) => { sh.sets = sh.sets?.filter((id) => id !== x.id) }); rerenderSettings(); refreshChrome() } }], { rect: e.target.closest('button').getBoundingClientRect(), align: 'end' })
  }
  const setAdd = e.target.closest('[data-set-add]')?.dataset.setAdd
  if (setAdd) return askAgent({ kind: 'visual_settings' }, setAdd === 'gen' ? '按创意方向和角色资料生成人设三视图和主场景图' : setAdd === 'character' ? '新加一个人设：' : '新加一个场景：')
  const regen = e.target.closest('[data-regen]')?.dataset.regen
  if (regen) { const sh = current.shots.items.find((x) => x.id === regen); return askAgent({ kind: 'shot', id: regen }, `以最新的设定重新生成这一格分镜图，风格：${sh.board?.style ?? pickStyle}`) }

  // 改动对照
  const df = e.target.closest('[data-df]')?.dataset.df
  if (df) return diffAction(ukParse(e.target.closest('[data-diff]').dataset.diff), df)

  // 镜头视图与选择生成范围
  const view = e.target.closest('[data-shot-view]')?.dataset.shotView
  if (view) { shotView = view; return rerenderShots() }
  const pk = e.target.closest('[data-pick]')?.dataset.pick
  if (pk === 'cancel') { shotPick = null; return rerenderShots() }
  if (pk === 'style') return openMenu(['草图', '灰度', '写实'].map((o) => ({ label: o, checked: o === pickStyle, run: () => { pickStyle = o; rerenderShots() } })), { rect: e.target.closest('button').getBoundingClientRect() })
  if (pk === 'next' && shotPick.size) {
    const units = current.shots.items.filter((s) => shotPick.has(s.id)).map((s) => ({ kind: 'shot', id: s.id }))
    shotPick = null
    rerenderShots()
    return askAgent(units, `生成这 ${units.length} 格分镜图，风格：${pickStyle}`)
  }
  const pid = e.target.closest('[data-pick-id]')?.dataset.pickId ?? (shotPick && e.target.closest('[data-tile]')?.dataset.tile)
  if (pid) { shotPick.has(pid) ? shotPick.delete(pid) : shotPick.add(pid); return rerenderShots() }
  const tile = e.target.closest('[data-tile]')?.dataset.tile
  if (tile) { shotView = 'cards'; rerenderShots(); return jumpTo(`[data-shot-id="${tile}"]`) }
  const shotSets = e.target.closest('[data-shot-sets]')?.dataset.shotSets
  if (shotSets) {
    const sh = current.shots.items.find((x) => x.id === shotSets)
    return openMenu(current.settings.items.map((x) => ({ label: `${x.kind === 'character' ? '人设' : '场景'} · ${x.name}`, checked: sh.sets?.includes(x.id), run: () => { sh.sets = sh.sets?.includes(x.id) ? sh.sets.filter((y) => y !== x.id) : [...(sh.sets ?? []), x.id]; markDirty(`shot:${sh.id}`, () => {}); rerenderShots() } })), { rect: e.target.closest('button').getBoundingClientRect() })
  }

  // 正文格式
  const fmt = e.target.closest('[data-fmt]')?.dataset.fmt
  if (fmt) {
    const rich = e.target.closest('.ps-editor').querySelector('.rich')
    rich.focus()
    if (fmt === 'bold') document.execCommand('bold')
    if (fmt === 'italic') document.execCommand('italic')
    if (fmt === 'h3') document.execCommand('formatBlock', false, 'h3')
    if (fmt === 'ul') document.execCommand('insertUnorderedList')
    if (fmt === 'quote') document.execCommand('formatBlock', false, 'blockquote')
    if (fmt === 'link') { const u = prompt('链接地址'); if (u) document.execCommand('createLink', false, u) }
    if (fmt === 'task') { document.execCommand('insertUnorderedList'); const ul = getSelection().anchorNode?.parentElement?.closest('ul'); ul?.classList.add('task') }
    if (fmt === 'ref') return openRefPicker(rich, e.target.closest('button'))
    return onRichInput(rich)
  }

  // 待办勾选
  const li = e.target.closest('.rich ul.task li')
  if (li && e.clientX - li.getBoundingClientRect().left < 22) { li.classList.toggle('done'); onRichInput(li.closest('.rich')); return }

  // 拍摄形式
  const form = e.target.closest('[data-form]')?.dataset.form
  if (form) {
    const b = current.brief
    b.forms = b.forms.includes(form) ? b.forms.filter((x) => x !== form) : [...b.forms, form]
    e.target.closest('[data-form]').setAttribute('aria-pressed', b.forms.includes(form))
    if (form === '合成' && b.forms.includes('合成') && !b.location) { b.location = '无影棚'; editor.querySelector('[data-brief="location"]').value = '无影棚'; toast('合成片场地已默认填为无影棚，可修改') }
    return markDirty('brief', () => {})
  }

  // 参考图
  const add = e.target.closest('[data-add]')
  if (add) return openPicker({ kind: 'section', key: add.dataset.add, group: add.dataset.group })
  const board = e.target.closest('[data-board]')?.dataset.board
  if (board) return openPicker({ kind: 'board', shot: board })
  const shotRef = e.target.closest('[data-shot-ref]')?.dataset.shotRef
  if (shotRef) return openPicker({ kind: 'shotref', shot: shotRef })
  const rm = e.target.closest('[data-ref-menu]')
  if (rm) return refMenu(rm.dataset.refMenu, rm)
  const fig = e.target.closest('.rf')
  if (fig) return lightbox(findRef(current, fig.dataset.refId))

  // 镜头
  const chipBtn = e.target.closest('[data-shot-chip]')
  if (chipBtn) {
    const s = current.shots.items.find((x) => x.id === chipBtn.dataset.id)
    const opts = chipBtn.dataset.shotChip === 'framing' ? ['全身', '半身', '特写', '其他'] : ['草图', '灰度', '写实']
    return openMenu(opts.map((o) => ({ label: o, checked: (chipBtn.dataset.shotChip === 'framing' ? s.framing : s.board.style) === o, run: () => { if (chipBtn.dataset.shotChip === 'framing') s.framing = o; else s.board.style = o; markDirty(`shot:${s.id}`, () => {}); rerenderShots() } })), { rect: chipBtn.getBoundingClientRect() })
  }
  const shotMore = e.target.closest('[data-shot-more]')?.dataset.shotMore
  if (shotMore) {
    const s = current.shots.items.find((x) => x.id === shotMore)
    return openMenu([{ label: '请助手修改这个镜头', icon: 'sparkles', run: () => askAgent({ kind: 'shot', id: s.id }, '') }, { label: '重新生成分镜图', icon: 'sparkles', run: () => askAgent({ kind: 'shot', id: s.id }, `重新生成这一格分镜图，风格：${s.board?.style ?? pickStyle}`) }, 'sep', ...TEMPLATE.slots.filter((sl) => sl.key !== s.slot).map((sl) => ({ label: `移到「${sl.name}」`, run: () => { s.slot = sl.key; markDirty('shots', () => {}); rerenderShots() } })), 'sep', { label: '删除镜头', danger: true, run: () => { const idx = current.shots.items.indexOf(s); current.shots.items.splice(idx, 1); markDirty('shots', () => {}); rerenderShots(); toast('已删除镜头', { label: '撤销', run: () => { current.shots.items.splice(idx, 0, s); rerenderShots() } }) } }], { rect: e.target.closest('button').getBoundingClientRect(), align: 'end' })
  }
  const shotAdd = e.target.closest('[data-shot-add]')?.dataset.shotAdd
  if (shotAdd) {
    const sl = TEMPLATE.slots.find((x) => x.key === shotAdd)
    current.shots.items.push({ id: id('s'), rev: current.revision, slot: shotAdd, framing: sl.framing, title: `新镜头`, board: null, refs: [], action: '', expression: '', composition: '', lighting: '', notes: '' })
    markDirty('shots', () => {})
    rerenderShots()
  }
})
function rerenderShots() {
  const el = editor.querySelector('[data-sec="shots"]')
  el.outerHTML = shotsHTML(current)
  hydrate(editor.querySelector('[data-sec="shots"]'))
  editor.querySelector('.pe-toc').innerHTML = tocHTML(current)
  setupSpy()
}

editor.addEventListener('input', (e) => {
  const mg = e.target.closest('[data-mg-ref]')
  if (mg) { const c = sec(mg.closest('[data-sec]').dataset.sec).conflict; c.keepRefs = mg.checked ? [...c.keepRefs, mg.dataset.mgRef] : c.keepRefs.filter((x) => x !== mg.dataset.mgRef); return }
  if (e.target.closest('[data-merge]')) return
  const rich = e.target.closest('.rich')
  if (rich) return onRichInput(rich)
  const t = e.target
  if (t.dataset.unit === 'title') { const v = t.textContent.trim(); return markDirty('title', () => { current.title = v || '未命名策划' }) }
  if (t.dataset.brief) { const k = t.dataset.brief; const v = t.value; return markDirty('brief', () => { current.brief[k] = v }) }
  if (t.dataset.headline) { const s = sec(t.dataset.headline); const v = t.value; return markDirty(`body:${s.key}`, () => { s.headline = v; s.rev = current.revision + 1; s.agentFlag = null }) }
  if (t.dataset.shot) { const s = current.shots.items.find((x) => x.id === t.dataset.shot); const k = t.dataset.field; const v = t.value ?? t.textContent; return markDirty(`shot:${s.id}`, () => { s[k] = v }) }
})
function onRichInput(rich) {
  const s = sec(rich.dataset.rich)
  const html = rich.innerHTML
  markDirty(`body:${s.key}`, () => { s.body = html; s.rev = current.revision + 1; s.agentFlag = null })
}

// 粘贴：能转换的转换并提示，图片改走「添加参考图」，不静默丢内容
editor.addEventListener('paste', (e) => {
  const rich = e.target.closest('.rich[data-rich]')
  if (!rich) return
  const dt = e.clipboardData
  if ([...dt.items].some((i) => i.kind === 'file' && i.type.startsWith('image/'))) { e.preventDefault(); return toast('图片请用「添加参考图」放进参考图组') }
  const html = dt.getData('text/html')
  if (/<table/i.test(html)) {
    e.preventDefault()
    const doc = new DOMParser().parseFromString(html, 'text/html')
    const rows = [...doc.querySelectorAll('tr')].map((tr) => [...tr.cells].map((c) => c.textContent.trim()).filter(Boolean).join(' · ')).filter(Boolean)
    document.execCommand('insertHTML', false, `<ul>${rows.map((r) => `<li>${esc(r)}</li>`).join('')}</ul>`)
    toast('表格已转为列表')
    return onRichInput(rich)
  }
  e.preventDefault()
  document.execCommand('insertText', false, dt.getData('text/plain'))
})

// @ 引用参考
editor.addEventListener('keydown', (e) => {
  const rich = e.target.closest('.rich[data-rich]')
  if (rich && e.key === '@') { e.preventDefault(); openRefPicker(rich, null) }
  if (e.key === 'Escape' && !document.querySelector('.menu,.popover,.pk-back,.lb-back,.modal-back')) closeEditor()
})
let savedRange = null
function openRefPicker(rich, anchor) {
  const sel = getSelection()
  savedRange = sel.rangeCount ? sel.getRangeAt(0).cloneRange() : null
  const refs = allRefs(current)
  let at
  if (anchor) at = { rect: anchor.getBoundingClientRect() }
  else { const r = savedRange?.getBoundingClientRect(); at = { x: r?.left ?? 200, y: (r?.bottom ?? 200) + 6 } }
  const pop = openPopover(`<div class="at-pop" role="listbox">${refs.length ? refs.map((r) => `<button class="at-it" data-pick="${r.id}"><img src="${src(r.n)}" alt=""><span>${esc(r.caption)}</span><small>参考 ${refNo(current, r.id)}</small></button>`).join('') : '<p style="padding:10px;color:var(--fg-3);margin:0">还没有参考图，先在参考图组里添加</p>'}</div>`, at, anchor)
  pop.querySelector('button')?.focus()
  pop.addEventListener('keydown', (e) => {
    const items = [...pop.querySelectorAll('button')]
    const i = items.indexOf(document.activeElement)
    if (e.key === 'ArrowDown') { e.preventDefault(); items[(i + 1) % items.length]?.focus() }
    if (e.key === 'ArrowUp') { e.preventDefault(); items[(i - 1 + items.length) % items.length]?.focus() }
  })
  pop.addEventListener('click', (e) => {
    const b = e.target.closest('[data-pick]')
    if (!b) return
    closeFloating()
    rich.focus()
    if (savedRange) { const s = getSelection(); s.removeAllRanges(); s.addRange(savedRange) }
    document.execCommand('insertHTML', false, `${chip(b.dataset.pick)}&nbsp;`)
    decorateChips(rich)
    onRichInput(rich)
  })
}

// 引用悬停预览
let peek
editor.addEventListener('pointerover', (e) => {
  const c = e.target.closest('.ref-chip:not(.gone)')
  peek?.remove()
  if (!c) return
  const r = findRef(current, c.dataset.ref)
  const rect = c.getBoundingClientRect()
  peek = document.createElement('div')
  peek.className = 'ref-peek'
  peek.innerHTML = `<img src="${src(r.n)}" alt=""><p>参考 ${refNo(current, r.id)} · ${esc(r.caption)}</p>`
  document.body.append(peek)
  peek.style.left = `${Math.min(rect.left, innerWidth - 232)}px`
  peek.style.top = `${rect.bottom + 8 + 260 > innerHeight ? rect.top - peek.offsetHeight - 8 : rect.bottom + 8}px`
})
editor.addEventListener('pointerout', (e) => { if (e.target.closest('.ref-chip')) peek?.remove() })

function refMenu(rid, btn) {
  const owner = current.sections.find((s) => s.refs.some((r) => r.id === rid))
  const r = owner.refs.find((x) => x.id === rid)
  openMenu([
    { label: '请助手按这张图修改', icon: 'sparkles', sub: '带上这张图和所在章节', run: () => askAgent({ kind: 'section', key: owner.key, refId: rid }, '') },
    { label: '改说明', icon: 'pencil', run: () => { const t = prompt('参考说明', r.caption); if (t != null) { r.caption = t; markDirty(`body:${owner.key}`, () => { owner.rev = current.revision + 1 }); rerenderSection(owner.key) } } },
    ...groupOrder(owner).filter((g) => g !== r.group).map((g) => ({ label: `移到「${g}」`, run: () => { r.group = g; markDirty(`body:${owner.key}`, () => { owner.rev = current.revision + 1 }); renderEditor({ keepScroll: true }) } })),
    'sep',
    { label: '删除引用', danger: true, sub: '正文里指向它的标记会显示「引用已删除」', run: () => { const i = owner.refs.indexOf(r); owner.refs.splice(i, 1); markDirty(`body:${owner.key}`, () => { owner.rev = current.revision + 1 }); renderEditor({ keepScroll: true }); toast(`已删除参考，其余编号已更新`, { label: '撤销', run: () => { owner.refs.splice(i, 0, r); renderEditor({ keepScroll: true }) } }) } },
  ], { rect: btn.getBoundingClientRect(), align: 'end' }, btn)
}

function lightbox(r) {
  if (!r) return
  const back = document.createElement('div')
  back.className = 'lb-back'
  back.innerHTML = `<div class="lb"><img src="${src(r.n)}" alt=""><p><b>参考 ${refNo(current, r.id)}</b> · ${esc(r.caption)}${r.source ? ` · 来源 ${esc(r.source.site)}${r.source.author ? ` / ${esc(r.source.author)}` : ''}` : ''}</p></div>`
  back.addEventListener('click', () => back.remove())
  back.tabIndex = -1
  back.addEventListener('keydown', (e) => { if (e.key === 'Escape') { e.stopPropagation(); back.remove() } })
  document.body.append(back)
  back.focus()
}

/* 添加参考图：当前画布 / 资产库 */
function openPicker(target) {
  let tab = 'canvas'
  let q = ''
  const picked = []
  const single = target.kind === 'board'
  let group = target.group
  const s = target.kind === 'section' ? sec(target.key) : null
  const shot = target.kind !== 'section' ? current.shots.items.find((x) => x.id === target.shot) : null
  const back = document.createElement('div')
  back.className = 'pk-back'
  const where = () => (s ? `${tmplOf(s.key).title} › ${group}` : `镜头「${shot.title}」${single ? '的分镜图' : '的参考'}`)
  const items = () => (tab === 'canvas' ? ON_CANVAS.map((n) => LIBRARY.find((x) => x.n === n)) : LIBRARY).filter((x) => !q || x.title.includes(q))
  const draw = () => {
    back.innerHTML = `<div class="pk" role="dialog" aria-modal="true" aria-label="添加参考图">
      <div class="pk-head"><h3>${single ? '添加分镜图' : '添加参考图'}</h3><span class="to">放入 ${esc(where())}</span><span style="flex:1"></span><button class="icon-btn" data-pk="close" aria-label="关闭">${I('x')}</button></div>
      <div class="pk-tabs" role="tablist"><button class="pk-tab" role="tab" data-tab="canvas" aria-selected="${tab === 'canvas'}">当前画布</button><button class="pk-tab" role="tab" data-tab="library" aria-selected="${tab === 'library'}">资产库</button>
        <label class="pk-search">${I('search')}<input placeholder="${tab === 'canvas' ? '搜索画布上的图片' : '搜索资产库'}" value="${esc(q)}"></label></div>
      <div class="pk-grid">${items().map((x) => { const k = picked.indexOf(x.n); return `<button class="pk-it" data-pk-n="${x.n}" aria-pressed="${k >= 0}"><img src="${src(x.n)}" alt="" style="aspect-ratio:${x.w}/${x.h}">${tab === 'library' && ON_CANVAS.includes(x.n) ? '<span class="on-canvas">在画布上</span>' : ''}<span class="nm">${esc(x.title)}</span><span class="ck">${k >= 0 ? (single ? '✓' : k + 1) : ''}</span></button>` }).join('')}</div>
      <div class="pk-foot">${s && tmplOf(s.key).groups.length > 1 ? `<span class="lbl">分组</span><button class="pk-group" data-pk="group">${esc(group)}${I('chevron-down')}</button>` : ''}<span class="pk-note">${tab === 'canvas' ? '取节点当前显示的版本' : '取素材当前版本'} · 从电脑上传请先放进画布或资产库</span><span class="grow"></span>
        <button class="btn" data-pk="close">取消</button><button class="btn btn-primary" data-pk="ok" ${picked.length ? '' : 'disabled style="opacity:.4"'}>${single ? '设为分镜图' : `添加 ${picked.length || ''} 张`}</button></div></div>`
    hydrate(back)
    const input = back.querySelector('.pk-search input')
    input.addEventListener('input', () => { q = input.value; const pos = input.selectionStart; draw(); const i2 = back.querySelector('.pk-search input'); i2.focus(); i2.setSelectionRange(pos, pos) })
  }
  back.addEventListener('click', (e) => {
    if (e.target === back) return back.remove()
    const tb = e.target.closest('[data-tab]')?.dataset.tab
    if (tb) { tab = tb; return draw() }
    const n = e.target.closest('[data-pk-n]')?.dataset.pkN
    if (n) { const i = picked.indexOf(n); if (single) picked.splice(0, picked.length, ...(i >= 0 ? [] : [n])); else if (i >= 0) picked.splice(i, 1); else picked.push(n); return draw() }
    const a = e.target.closest('[data-pk]')?.dataset.pk
    if (a === 'close') return back.remove()
    if (a === 'group') return openMenu(groupOrder(s).map((g) => ({ label: g, checked: g === group, run: () => { group = g; draw() } })), { rect: e.target.closest('button').getBoundingClientRect() })
    if (a === 'ok' && picked.length) {
      back.remove()
      const added = picked.map((n) => ({ id: id('r'), n, group, caption: LIBRARY.find((x) => x.n === n).title, source: tab === 'library' ? { site: '资产库' } : null }))
      if (s) { s.refs.push(...added); markDirty(`body:${s.key}`, () => { s.rev = current.revision + 1 }) }
      else if (single) { shot.board = { n: picked[0], style: '写实' }; markDirty(`shot:${shot.id}`, () => {}) }
      else { const host = sec('moodboard'); host.refs.push(...added.map((r) => ({ ...r, group: '光影逻辑' }))); shot.refs.push(...added.map((r) => r.id)); markDirty(`shot:${shot.id}`, () => {}) }
      renderEditor({ keepScroll: true })
      added.forEach((r) => editor.querySelector(`[data-ref-id="${r.id}"]`)?.classList.add('flash'))
      toast(single ? '已设为分镜图' : `已添加 ${added.length} 张参考`)
    }
  })
  back.tabIndex = -1
  // 重绘会让焦点落回 body，Esc 挂在 document 上
  const onKey = (e) => { if (e.key === 'Escape' && !document.querySelector('.menu')) { e.stopPropagation(); back.remove() } }
  document.addEventListener('keydown', onKey, true)
  new MutationObserver((_, ob) => { if (!back.isConnected) { document.removeEventListener('keydown', onKey, true); ob.disconnect() } }).observe(document.body, { childList: true })
  draw()
  document.body.append(back)
  back.querySelector('.pk-tab[aria-selected="true"]').focus()
}

/* 目录跟随滚动；窄屏时顶栏「目录」按钮显示当前章节名 */
let curToc = 'brief'
function setupSpy() {
  spy?.disconnect()
  const doc = editor.querySelector('.pe-doc')
  if (!doc) return
  spy = new IntersectionObserver((entries) => {
    for (const en of entries) if (en.isIntersecting) {
      const key = en.target.dataset.sec
      curToc = key
      editor.querySelectorAll('[data-toc]').forEach((a) => a.classList.toggle('on', a.dataset.toc === key))
      const t = editor.querySelector('.pe-cur .t')
      if (t) t.textContent = tocTitle(key)
    }
  }, { root: doc, rootMargin: '-10% 0px -80% 0px' })
  editor.querySelectorAll('.ps[data-sec]').forEach((el) => spy.observe(el))
}
function jumpTo(sel) {
  const el = sel && editor.querySelector(sel)
  if (!el) return toast('这部分已不在策划里')
  el.scrollIntoView({ behavior: 'smooth', block: 'start' })
  el.classList.remove('unit-flash')
  void el.offsetWidth
  el.classList.add('unit-flash')
}
function rerenderUnit(u) {
  if (editor.hidden) return
  if (u.kind === 'section') rerenderSection(u.key)
  else if (u.kind === 'setting') rerenderSettings()
  else if (u.kind === 'shot' || u.kind === 'shots') rerenderShots()
  else if (u.kind === 'brief') rerenderBrief()
  refreshChrome()
}

/* 改动对照：「看改动」直接在这一处下方展开，历史列表退到第二层 */
const unitSel = (u) => (u.kind === 'section' ? `#sec-${u.key}` : u.kind === 'setting' ? `[data-set="${u.id}"]` : u.kind === 'shot' ? `[data-shot-id="${u.id}"]` : '#sec-brief')
function openDiff(u, d) {
  diffs.set(uk(u), d)
  if (u.kind === 'shot') shotView = 'cards'
  rerenderUnit(u)
  editor.querySelector(`[data-diff="${uk(u)}"]`)?.scrollIntoView({ behavior: 'smooth', block: 'nearest' })
}
// 某一轮助手写入里的第 i 处：打开策划、定位、展开「本轮之前 → 现在」的对照
function showRunDiff(docId, runId, i) {
  const p = plans.get(docId)
  const run = p?.runs.find((r) => r.id === runId)
  const it = run?.items[i]
  if (!it) return
  // 镜头的对照在卡片底部，定位到卡片本身；其余定位到对照面板
  const open = () => { diffs.set(uk(it.u), { mode: 'run', base: it.before, run: run.conv, runId, i }); if (it.u.kind === 'shot') shotView = 'cards'; rerenderUnit(it.u); jumpTo(it.u.kind !== 'shot' && editor.querySelector(`[data-diff="${uk(it.u)}"]`) ? `[data-diff="${uk(it.u)}"]` : unitSel(it.u)) }
  if (editor.hidden || current?.id !== docId) { openEditor(nodes.find((n) => n.doc === docId)); setTimeout(open, 320) } else open()
  rerenderAgent()
}
function diffAction(u, act) {
  const d = diffs.get(uk(u))
  const p = current
  const x = unitGet(p, u)
  const done = () => { diffs.delete(uk(u)); rerenderUnit(u); rerenderAgent() }
  if (act === 'close') return done()
  if (act === 'history') { side = 'history'; return renderEditor({ keepScroll: true }) }
  if (act === 'seen') { const it = d.runId && p.runs.find((r) => r.id === d.runId)?.items[d.i]; if (it) it.seen = true; return done() }
  if (act === 'confirm') {
    x.confirmedRev = x.rev
    x.confirmedSnap = snapOf(p, u)
    x.agentFlag = null
    p.revision += 1
    p.history.unshift({ rev: p.revision, who: 'me', at: now(), what: [`确认 ${unitTitle(p, u)}`] })
    toast(`已确认「${unitTitle(p, u)}」#${x.rev}`)
    return done()
  }
  if (act === 'restore') {
    const title = unitTitle(p, u)
    // 恢复到确认时的内容：内容与确认那一版相同，确认状态随之保留；恢复本轮：写回本轮之前的内容
    applySnap(p, u, d.base)
    p.revision += 1
    const y = unitGet(p, u)
    if (y && 'rev' in y) { y.rev = p.revision; y.agentFlag = null }
    if (d.mode === 'confirmed' && y) { y.confirmedRev = p.revision }
    if (d.runId) { const it = p.runs.find((r) => r.id === d.runId)?.items[d.i]; if (it) it.reverted = true }
    p.history.unshift({ rev: p.revision, who: 'me', at: now(), what: [d.mode === 'confirmed' ? `${title} 恢复到确认时的内容` : `${title} 撤回助手本轮`] })
    toast(d.mode === 'confirmed' ? `「${title}」已恢复到确认时的内容` : `已撤回助手对「${title}」的这次修改`)
    diffs.delete(uk(u))
    if (u.kind === 'shot') rerenderShots()
    rerenderUnit(u)
    renderCanvas()
    return rerenderAgent()
  }
}
editor.addEventListener('click', (e) => {
  const a = e.target.closest('[data-toc]')
  if (!a) return
  e.preventDefault()
  editor.querySelector(`#sec-${a.dataset.toc}`)?.scrollIntoView({ behavior: 'smooth', block: 'start' })
})

/* ───────── 助手面板：画布与编辑视图共用一份面板状态 ─────────
   当前会话、输入框草稿、运行状态、面板开合只有一份，画布右侧和编辑视图右栏只是两个显示位置。
   会话挂在画布下；文档按「文档 + 画布」记工作会话。章节、设定、镜头上的助手入口只起草消息。 */
const SKILL_NAME = 'Cosplay 拍摄策划'
let convs = [] // 本画布的全部会话，最近使用的在前
const panel = { open: false, viewId: null, running: false, draft: { text: '', units: [], skill: false } }
function sampleConvs() {
  return [
    { id: 'conv_plan', title: '拍摄策划 · 芙宁娜', turns: [
      { role: 'sys', text: '更早的 14 条消息已压缩为摘要，进度以策划文档为准' },
      { role: 'user', text: '芙宁娜，歌剧院主题的合成片，想和常见的审判庭构图拉开差别', skill: true, units: [] },
      { role: 'proc', items: [{ verb: '调研角色', obj: '芙宁娜 官方设定与立绘' }, { verb: '搜索参考', obj: 'ArtStation · 歌剧院 舞台顶光' }, { verb: '写入策划', obj: 'IP 拆解、情绪板' }] },
      { role: 'ai', html: '<p>给了三个方向，你选了 <b>B · 空剧院谢幕</b>：观众席全空，只留一束舞台顶光。</p><p>IP 拆解和情绪板已写进策划，看过后在章节里确认。</p>' },
      { role: 'user', text: '就 B。剧院正门那张参考太常见了，去掉', skill: true, units: [{ kind: 'section', key: 'moodboard', docId: 'doc_furina', label: '情绪板 #14' }] },
      { role: 'proc', items: [{ verb: '读取策划', obj: '情绪板' }, { verb: '写入策划', obj: '创意方向、情绪板' }] },
      { role: 'ai', html: '<p>已去掉正门参考。后面的光源和分镜都按「空剧院、唯一顶光」来；人设三视图和主场景图也生成好了，等你确认。</p>' },
    ] },
    { id: 'conv_ref', title: '整理参考图', turns: [
      { role: 'user', text: '把画布上的参考图按光影、构图分组', skill: false, units: [] },
      { role: 'ai', html: '<p>已分成两组放在画布左侧。</p>' },
    ] },
  ]
}
const typeName = (p) => (p.type === 'cosplay' ? '拍摄策划' : TYPES.find((t) => t.key === p.type).name)
const convOf = (cid) => convs.find((c) => c.id === cid)
const agentInEditor = () => panel.open && side !== 'history'
// 这份策划在本画布的工作会话；没有或已删除时新建一个，以「类型 · 策划标题」命名
function workOf(p) {
  if (!convOf(p.workId)) {
    const c = { id: id('conv'), title: `${typeName(p)} · ${p.title}`, turns: [] }
    convs.unshift(c)
    p.workId = c.id
  }
  return p.workId
}
const sameUnit = (x, y) => x.docId === y.docId && x.kind === y.kind && x.key === y.key && x.id === y.id && x.quote === y.quote && x.refId === y.refId
const clip = (t, n) => (t.length > n ? `${t.slice(0, n)}…` : t)
function unitInfo(u) {
  const p = plans.get(u.docId)
  if (!p) return { label: '策划已删除', icon: 'file', sel: '' }
  if (u.kind === 'doc') return { label: `策划 · ${p.title}`, icon: 'clapper', sel: '.pe-cover' }
  if (u.kind === 'brief') return { label: '基本信息', icon: 'file', sel: '#sec-brief' }
  // 选中文字或某张参考图：标签带上摘录 / 编号，助手只改这一处
  const tail = u.quote ? ` · “${clip(u.quote, 14)}”` : u.refId ? ` · 参考 ${refNo(p, u.refId) || '已删除'}` : ''
  if (u.kind === 'section') return { label: `${tmplOf(u.key).title}${tail || ` #${p.sections.find((x) => x.key === u.key).rev}`}`, icon: u.refId ? 'image' : 'file', sel: `#sec-${u.key}` }
  if (u.kind === 'visual_settings') return { label: '视觉设定', icon: 'user', sel: '#sec-visual_settings' }
  if (u.kind === 'setting') { const x = findSet(p, u.id); return { label: x ? `${x.kind === 'character' ? '人设' : '场景'} · ${x.name} #${x.rev}` : '设定已删除', icon: 'user', sel: `[data-set="${u.id}"]` } }
  if (u.kind === 'shots') return { label: '分镜与交付', icon: 'clapper', sel: '#sec-shots' }
  const sh = p.shots.items.find((x) => x.id === u.id)
  return { label: sh ? `${shotNo(p, sh)} ${sh.title}${tail}` : '镜头已删除', icon: 'clapper', sel: `[data-shot-id="${u.id}"]` }
}
const skillChip = (rm) => `<span class="ctx-chip pa-skill"><span class="ti">${I('sparkles')}</span><span>${SKILL_NAME}</span>${rm ? `<button data-ag="rm-skill" aria-label="不带 Skill">${I('x')}</button>` : ''}</span>`
const unitChip = (u, rm) => { const info = unitInfo(u); return `<span class="ctx-chip"><span class="ti">${I(info.icon)}</span><button class="pa-go" data-go-doc="${u.docId}" data-go-sel='${esc(info.sel)}'>@${esc(u.label ?? info.label)}</button>${rm != null ? `<button data-ag-rm="${rm}" aria-label="移除">${I('x')}</button>` : ''}</span>` }
function turnHTML(t) {
  if (t.role === 'sys') return `<div class="pa-sys">${I('history')}${esc(t.text)}</div>`
  if (t.role === 'user') {
    const ctx = [t.skill ? skillChip(false) : '', ...t.units.map((u) => unitChip(u))].join('')
    return `<div><div class="msg-user">${ctx ? `<div class="u-ctx">${ctx}</div>` : ''}${esc(t.text)}</div></div>`
  }
  if (t.role === 'review') return reviewHTML(t)
  if (t.role === 'gen') return genHTML(t)
  if (t.role === 'proc') return `<div>${t.items.map((it) => `<div class="p-row p-tool${it.run ? ' run' : ''}" tabindex="-1"><span class="p-ico">${it.run ? '<span class="spin"></span>' : I('check')}</span><span class="p-title"><span class="verb${it.run ? ' shimmer' : ''}">${esc(it.verb)}</span><span class="obj">${esc(it.obj)}</span></span></div>`).join('')}</div>`
  return `<div><div class="p-text">${t.html}</div></div>`
}
// 本轮改动清单：每一处可以跳过去看「本轮之前 → 现在」，在那里确认或恢复
function rvState(p, it) {
  if (it.reverted) return ['reverted', '已恢复']
  const x = unitGet(p, it.u)
  if (!x) return ['reverted', '已删除']
  if (it.u.kind === 'section' || it.u.kind === 'setting') return x.confirmedRev != null && x.confirmedRev >= it.rev ? ['ok', '已确认'] : ['todo', '待确认']
  return it.seen ? ['ok', '看过了'] : ['todo', '待看']
}
function reviewHTML(t) {
  const p = plans.get(t.docId)
  const run = p?.runs.find((r) => r.id === t.runId)
  if (!run) return ''
  const states = run.items.map((it) => rvState(p, it))
  const left = states.filter(([k]) => k === 'todo').length
  return `<div><div class="rv"><div class="rv-head"><b>本轮改了 ${run.items.length} 处</b><span>${left ? `还有 ${left} 处没看` : '都看过了'}</span></div>
    ${run.items.map((it, i) => `<div class="rv-row"><span class="rv-st" data-s="${states[i][0]}">${states[i][1]}</span><span class="rv-name">${esc(unitTitle(p, it.u))}</span><button class="btn" data-rv="${t.docId}|${t.runId}|${i}">看改动</button></div>`).join('')}
    <p class="rv-foot">直接写进了策划；每处可以确认，或恢复成本轮之前的样子。</p></div></div>`
}
// 生成前确认：列出目标镜头、参考连线、模型参数和预计消耗，摄影师确认后才生成
const GEN_MODEL = 'gpt-image-2.5-sunburst · 2K · high'
function genHTML(t) {
  const g = t.g
  const p = plans.get(g.docId)
  const shots = g.ids.map((sid) => p.shots.items.find((x) => x.id === sid)).filter(Boolean)
  const rows = shots.map((sh) => {
    const sets = (sh.sets ?? []).map((sid) => findSet(p, sid)).filter(Boolean)
    const warn = sets.filter((x) => setState(x) !== 'confirmed').map((x) => `「${esc(x.name)}」未确认`)
    return `<li><b>${shotNo(p, sh)}</b> ${esc(sh.title)}<span>${sets.length ? sets.map((x) => `${esc(x.name)} #${x.rev}`).join('、') : '<em>未关联设定</em>'}${sh.refs.length ? ` · 参考 ${sh.refs.length} 张` : ''}${warn.length ? ` · <em>${warn.join('、')}</em>` : ''}</span></li>`
  }).join('')
  const unconfirmed = shots.some((sh) => (sh.sets ?? []).some((sid) => setState(findSet(p, sid)) !== 'confirmed'))
  const acts = g.state === 'ask'
    ? `<div class="pa-row"><button class="btn" data-gen="no">先不生成</button><button class="btn btn-primary" data-gen="ok">确认生成 ${shots.length} 格</button></div>`
    : `<div class="gen-done">${g.state === 'ok' ? `${I('check')}已确认，开始生成` : '已取消'}</div>`
  return `<div><div class="p-confirm gen" data-gid="${g.id}"><div class="ask-q">生成前先确认</div>
    <ul class="gen-list">${rows}</ul>
    <div class="gen-meta"><span>风格 <b>${esc(g.style)}</b></span><span>模型 <b>${GEN_MODEL}</b></span><span>预计 <b>${shots.length * 8}</b> 积分</span></div>
    ${unconfirmed && g.state === 'ask' ? '<p class="ask-hint">有设定还没确认，分镜会以当前版本为参考；建议先确认设定。</p>' : ''}${acts}</div></div>`
}
function agentHTML(where) {
  const p = where === 'editor' ? current : null
  // 面板还没有当前会话：在策划里进入它的工作会话，在画布上进入最近的会话；已有当前会话时不切换
  if (!convOf(panel.viewId)) panel.viewId = p ? workOf(p) : convs[0]?.id ?? workOf(plans.get('doc_furina'))
  const c = convOf(panel.viewId)
  const d = panel.draft
  let note = '画布助手 · 打开策划后在右栏继续同一个对话'
  if (p) {
    const work = convOf(p.workId)
    note = c.id === p.workId ? '这份策划在本画布的工作会话 · 和画布上的助手面板是同一个对话'
      : work ? `这份策划之前在「${esc(work.title)}」里讨论 · <button class="pa-link" data-ag="to-work">切过去</button>`
      : '从这里发消息后，它会成为这份策划的工作会话'
  }
  const ctx = [d.skill ? skillChip(true) : '', ...d.units.map((u, i) => unitChip(u, i))].join('')
  return `<aside class="pe-agent${p ? '' : ' pa-float'}" data-where="${where}" aria-label="助手">
    <header class="pa-head"><button class="thread-btn" data-ag="threads">${esc(c.title)}${I('chevron-down')}</button><span class="pe-grow"></span><button class="icon-btn" data-ag="close" aria-label="收起助手">${I('x')}</button></header>
    <div class="pa-note">${note}</div>
    <div class="log pa-log"><div class="turns">${c.turns.map(turnHTML).join('') || `<div class="pa-empty">${I('sparkles')}<b>和助手一起做这份策划</b><span>章节、设定、镜头上的助手按钮会把要讨论的部分带到这里。</span></div>`}</div></div>
    <div class="pa-compose"><div class="agent-box pa-box">
      <div class="ctx-row">${ctx}<button class="ctx-add" data-ag="add-unit"><span class="at">@</span>${p ? '策划内容' : '策划'}</button></div>
      <textarea data-ag-input rows="2" placeholder="${d.units.length ? '说说要怎么改，比如：保留顶光方向，只把三灯压成两灯' : '问助手，或让它继续下一步…'}">${esc(d.text)}</textarea>
      <div class="composer-foot"><span class="pa-hint">${panel.running ? '<span class="spin"></span>助手正在运行' : 'Enter 发送 · Shift + Enter 换行'}</span><span class="grow"></span><button class="send" data-ag="send" aria-label="发送"${panel.running ? ' disabled' : ''}>${I('arrow-up')}</button></div>
    </div></div>
  </aside>`
}
const canvasAgent = document.getElementById('canvasAgent')
const railAgent = document.getElementById('railAgent')
function scrollAgentLog() { document.querySelectorAll('.pa-log').forEach((log) => { log.scrollTop = log.scrollHeight }) }
// 画布上的面板只在编辑视图收起时显示，避免同一份状态出现两个输入框
function renderCanvasAgent() {
  const show = panel.open && editor.hidden
  canvasAgent.innerHTML = show ? agentHTML('canvas') : ''
  railAgent.setAttribute('aria-pressed', String(panel.open))
  railAgent.dataset.state = panel.running ? 'run' : ''
  if (show) { hydrate(canvasAgent); scrollAgentLog() }
}
function rerenderAgent() {
  const focused = document.activeElement?.matches('[data-ag-input]')
  renderCanvasAgent()
  if (!editor.hidden) {
    const btn = editor.querySelector('.pe-agent-btn')
    if (btn) { btn.dataset.state = panel.running ? 'run' : ''; btn.setAttribute('aria-pressed', String(agentInEditor())) }
    const el = editor.querySelector('.pe-agent')
    if (el) { el.outerHTML = agentHTML('editor'); hydrate(editor.querySelector('.pe-agent')) }
    scrollAgentLog()
  }
  if (focused) focusAgent()
}
function focusAgent() {
  const ta = (editor.hidden ? canvasAgent : editor).querySelector('[data-ag-input]')
  if (!ta) return
  ta.focus()
  ta.setSelectionRange(ta.value.length, ta.value.length)
}
function setPanelOpen(open) {
  panel.open = open
  if (open && side === 'history') side = null
  if (editor.hidden) renderCanvasAgent()
  else renderEditor({ keepScroll: true })
  if (open) focusAgent()
}
// 章节 / 设定 / 镜头入口：打开面板，带上这一处（可带选中的文字或某张参考图）的标签；摄影师补一句要求再发送
function askAgent(units, text = '') {
  panel.draft.units = [].concat(units).map((u) => ({ ...u, docId: current.id }))
  panel.draft.skill = true
  if (text) panel.draft.text = text
  if (!agentInEditor()) setPanelOpen(true)
  else { rerenderAgent(); focusAgent() }
}
// 对话里的标签：在画布上点击时先打开策划，再定位
function goUnit(docId, sel) {
  const n = nodes.find((x) => x.doc === docId)
  if (!n) return toast('这份策划不在当前画布上')
  if (editor.hidden || current?.id !== docId) { openEditor(n); setTimeout(() => jumpTo(sel), 320) } else jumpTo(sel)
}
// 原型里的「运行」：只发生在面板的当前会话里。写入按单元记下本轮之前的内容（真实实现由修订的 run_id 与 changed_units 算出），结束时给出改动清单
function logAgent(p, user, procItems) {
  if (!convOf(panel.viewId)) panel.viewId = workOf(p)
  const c = convOf(panel.viewId)
  if (user) c.turns.push({ role: 'user', skill: true, ...user })
  const proc = { role: 'proc', items: procItems.map((it) => ({ ...it, run: true })) }
  c.turns.push(proc)
  panel.running = true
  rerenderAgent()
  let rec = null
  return {
    conv: c,
    step: (it) => { proc.items.forEach((x) => { x.run = false }); proc.items.push({ ...it, run: true }); rerenderAgent() },
    touch: (u, mutate) => {
      if (!rec) { rec = { id: id('run'), conv: c.title, items: [] }; p.runs.push(rec) }
      const before = snapOf(p, u)
      mutate()
      p.revision += 1
      const x = unitGet(p, u)
      if (x && 'rev' in x) x.rev = p.revision
      let i = rec.items.findIndex((it) => uk(it.u) === uk(u))
      if (i >= 0) rec.items[i].rev = p.revision // 同一轮里改了两次：对照仍从本轮之前算
      else { rec.items.push({ u, before, rev: p.revision }); i = rec.items.length - 1 }
      if (u.kind === 'section') { x.agentFlag = 'done'; x.lastRun = { runId: rec.id, i } }
      p.history.unshift({ rev: p.revision, who: 'agent', run: c.title, at: now(), what: [unitTitle(p, u)] })
      p.workId = c.id
      if (current === p) rerenderUnit(u)
    },
    done: (html) => {
      proc.items.forEach((x) => { x.run = false })
      if (html) c.turns.push({ role: 'ai', html })
      if (rec?.items.length) c.turns.push({ role: 'review', docId: p.id, runId: rec.id })
      panel.running = false
      renderCanvas()
      rerenderAgent()
    },
  }
}
const SCENARIO = /两盏灯|两小时|不能洒水/
const SCENARIO_TEXT = '棚拍只剩两小时、只有两盏灯、不能洒水。保留已确认的创意，调整布光和受影响的镜头，并列出新增准备项。'
function sendAgent(where) {
  const text = panel.draft.text.trim()
  if (panel.running || (!text && !panel.draft.units.length)) return
  const units = panel.draft.units.map((u) => ({ ...u, label: unitInfo(u).label }))
  const skill = panel.draft.skill
  // 从策划里发、或消息带了策划标签：这个会话成为那份策划的工作会话
  const docIds = new Set(units.map((u) => u.docId))
  if (where === 'editor') docIds.add(current.id)
  docIds.forEach((d) => { const p = plans.get(d); if (p) p.workId = panel.viewId })
  panel.draft = { text: '', units: [], skill: where === 'editor' }
  const u = units[0]
  const p = plans.get(u?.docId ?? (where === 'editor' ? current.id : '')) ?? plans.get('doc_furina')
  const run = logAgent(p, { text, units, skill }, [{ verb: '读取策划', obj: units.map((x) => x.label).join('、') || (where === 'editor' ? '全文' : '画布') }])
  const shotIds = units.filter((x) => x.kind === 'shot').map((x) => x.id)
  setTimeout(() => {
    if (SCENARIO.test(text)) return twoLights(p, run)
    if (u?.kind === 'shots' && /补齐|镜头方案/.test(text)) return fillShots(p, run)
    if (shotIds.length && /生成/.test(text)) { run.conv.turns.push({ role: 'gen', g: { id: id('gen'), docId: p.id, ids: shotIds, style: text.match(/风格[：:]\s*(\S+)/)?.[1] ?? pickStyle, state: 'ask' } }); return run.done('') }
    if (u?.kind === 'section') return editSection(p, run, u, text)
    if (u?.kind === 'shot') return editShot(p, run, u, text)
    if (u?.kind === 'setting' && findSet(p, u.id)) {
      const x = findSet(p, u.id)
      run.step({ verb: '生成图片', obj: `${x.name} · 参考 ${x.basis.length} 张资料图` })
      return setTimeout(() => { updateSetting(p, x, run); const n = staleCount(p, x.id); run.done(`<p>「${esc(x.name)}」换了一版。${n ? `有 ${n} 张分镜基于旧设定，你确认设定后我再重新生成。` : ''}</p>`) }, 1500)
    }
    if (u?.kind === 'visual_settings') return run.done(`<div class="p-confirm"><div class="ask-q">生成前先确认</div><div class="ask-hint">人设三视图、主场景图各 1 组，依据 IP 拆解里的角色资料；模型 ${GEN_MODEL}；预计消耗 40 积分。</div><div class="pa-row"><button class="btn" data-ag="later">先不生成</button><button class="btn btn-primary" data-ag="later">确认生成</button></div></div>`)
    if (u?.kind === 'doc' || u?.kind === 'shots') {
      const done = p.sections.filter((s) => stateOf(s) === 'confirmed').length
      const pending = p.settings.items.filter((x) => setState(x) !== 'confirmed').map((x) => esc(x.name))
      const st = shotStats(p)
      return run.done(`<p>读了「${esc(p.title)}」：已确认 ${done} / ${p.sections.length} 章${pending.length ? `，设定「${pending.join('」「')}」还没确认` : ''}；镜头已规划 ${st.planned} / ${TARGET}，分镜图可用 ${st.boards} / ${TARGET}。</p><p>建议先确认视觉设定，再补齐镜头方案。要我从哪一步继续？</p>`)
    }
    run.done('<p>（原型）助手先读取画布和相关策划，再按对话继续下一步。</p>')
  }, 700)
}
// 改一章；带了选中文字时只改那一段，带了参考图时按那张图改
function editSection(p, run, u, text) {
  const s = p.sections.find((x) => x.key === u.key)
  const title = tmplOf(u.key).title
  run.step({ verb: '写入策划', obj: u.quote ? `${title} · 选中的一段` : title })
  s.agentFlag = 'writing'
  if (current === p) rerenderSection(u.key)
  setTimeout(() => {
    const ask = esc(text || '按讨论调整')
    // 摘录可能跨了加粗等标记：按段落文字找到它所在的那一段，只改那一段
    const box = document.createElement('div')
    box.innerHTML = s.body
    const block = u.quote && [...box.querySelectorAll('p, li, h3, blockquote')].find((el) => el.textContent.replace(/\s+/g, ' ').includes(u.quote.replace(/…$/, '')))
    const quoted = Boolean(block)
    run.touch({ kind: 'section', key: u.key }, () => {
      if (quoted) { block.append(`（已按「${text || '讨论'}」调整）`); s.body = box.innerHTML }
      else if (u.refId) s.body += `<p>按这张参考 ${chip(u.refId)} 调整：${ask}（原型示意）。</p>`
      else s.body += `<p>${ask} —— 已按这条要求改写（原型示意）。</p>`
    })
    run.done(`<p>${quoted ? '只改了你选中的那一段，其余没动。' : `改好了「${esc(title)}」。`}</p>`)
  }, 1500)
}
function editShot(p, run, u, text) {
  const sh = p.shots.items.find((x) => x.id === u.id)
  if (!sh) return run.done('<p>这个镜头已经不在策划里了。</p>')
  run.step({ verb: '写入策划', obj: unitTitle(p, u) })
  setTimeout(() => {
    const field = u.quote && ['action', 'expression', 'composition', 'lighting', 'notes'].find((k) => sh[k]?.includes(u.quote))
    run.touch({ kind: 'shot', id: u.id }, () => {
      if (field) sh[field] = sh[field].replace(u.quote, `${u.quote}（按「${text || '讨论'}」调整）`)
      else sh.notes = [sh.notes, text || '按讨论调整'].filter(Boolean).join('；')
    })
    run.done(`<p>改好了「${esc(unitTitle(p, u))}」（原型示意）。</p>`)
  }, 900)
}
// 补齐镜头方案：只写文字，不生成分镜图
function fillShots(p, run) {
  run.step({ verb: '写入策划', obj: '分镜与交付 · 补齐镜头方案' })
  const IDEAS = {
    kv: [['谢幕之后', '背对镜头走向幕布', '看不见表情', '远景，人物在画面底部', '顶光渐暗，只剩轮廓']],
    half: [['扬帽转身', '一手扬起礼帽，身体半转', '得意，眼神挑衅', '人物偏左，礼帽占右上', '顶光 + 轮廓光勾帽檐'], ['杖尖点水', '手杖点地，低头看地面反光', '沉静', '俯拍，地面反光占下半', '顶光为主']],
    closeup: [['帽檐水滴', '指尖拨动帽檐的水滴装饰', '专注', '只取帽檐与手指', '侧逆光打亮水滴'], ['手杖与手套', '戴手套的手握住杖头', '—', '杖头居中，背景虚化', '轮廓光勾出黄铜反光']],
  }
  setTimeout(() => {
    const touched = []
    for (const sh of p.shots.items.filter((x) => missingOf(x).length)) {
      run.touch({ kind: 'shot', id: sh.id }, () => { sh.expression ||= '专注，带一点压迫感'; sh.composition ||= '近景平视，手杖竖在画面中线'; sh.lighting ||= '顶光 + 单侧轮廓' })
      touched.push(shotNo(p, sh))
    }
    for (const sl of TEMPLATE.slots) {
      const have = p.shots.items.filter((x) => x.slot === sl.key).length
      for (const [title, action, expression, composition, lighting] of IDEAS[sl.key].slice(0, Math.max(0, sl.target - have))) {
        const nid = id('s')
        run.touch({ kind: 'shot', id: nid }, () => p.shots.items.push({ id: nid, rev: 0, slot: sl.key, framing: sl.framing, title, board: null, refs: [], sets: ['set_char'], action, expression, composition, lighting, notes: '' }))
        touched.push(shotNo(p, p.shots.items.find((x) => x.id === nid)))
      }
    }
    run.done(touched.length ? `<p>补齐了镜头方案：${touched.join('、')}。只写了文字，没有生成分镜图；要生成时点「生成分镜图」选范围。</p>` : '<p>镜头方案已经齐了。</p>')
  }, 900)
}
// 确认后生成：逐格生成并记下基于的设定版本
function runGen(g) {
  g.state = 'ok'
  const p = plans.get(g.docId)
  const run = logAgent(p, null, [{ verb: '连接参考', obj: '人设、场景设定与镜头参考' }])
  const pool = ['03', '08', '10', '05', '01', '06', '12', '02']
  let k = 0
  const next = () => {
    const sh = p.shots.items.find((x) => x.id === g.ids[k])
    if (!sh) return run.done(`<p>生成好了 ${k} 格（${GEN_MODEL}，${esc(g.style)}），每格都记了基于的设定版本。</p>`)
    run.step({ verb: '生成分镜', obj: `${shotNo(p, sh)} ${sh.title}` })
    setTimeout(() => {
      const n = pool.find((x, j) => j >= (k * 3) % pool.length && x !== sh.board?.n) ?? pool[0]
      run.touch({ kind: 'shot', id: sh.id }, () => { sh.board = { n, style: g.style, basedOn: Object.fromEntries((sh.sets ?? []).map((sid) => [sid, findSet(p, sid)?.rev])) } })
      k++
      next()
    }, 800)
  }
  next()
}
// 演示场景：时间、灯和场地的限制变了，助手改受影响的几处，已确认的创意不动
function twoLights(p, run) {
  const S = (key) => p.sections.find((x) => x.key === key)
  const SH = (sid) => p.shots.items.find((x) => x.id === sid)
  const steps = [
    [{ kind: 'brief' }, () => { p.brief.constraints = '交付 9 张（2+4+3）；coser 自备假发与礼帽；棚拍 2 小时；只有 2 盏灯；棚内不能洒水' }],
    [{ kind: 'section', key: 'lighting' }, () => {
      const s = S('lighting')
      s.headline = '两灯方案：顶光主光 + 单侧冷色轮廓'
      s.body = s.body.replace('用低功率蓝色反光模拟环境色溢出', '不再单独布环境灯，用蓝色反光板把顶光回补到暗部，模拟环境色溢出').replace('两侧冷色轮廓光勾出礼帽与发丝边缘', '只留左侧一盏冷色轮廓光勾出礼帽与发丝边缘，右侧轮廓在后期补')
    }],
    [{ kind: 'section', key: 'preparation' }, () => {
      const s = S('preparation')
      s.headline = '无影棚 2 小时、两盏灯，水花用素材合成'
      s.body = s.body.replace('<li>两盏轮廓灯 + 冷色凝胶</li>', '<li>一盏轮廓灯 + 冷色凝胶（加顶光共两盏）</li><li>蓝色反光板 ×1（替代环境灯）</li>').replace('<li>水花飞溅：棚内单独拍，黑底高速快门</li>', '<li>水花：棚内不能洒水，改用素材库的水花素材合成（先挑 3 组）</li>') + '<h3 class="sub-group">时间安排（2 小时）</h3><ul class="task"><li>先 KV、再半身、最后特写；同一光位的镜头连着拍</li><li>换装与补妆预留 20 分钟</li></ul>'
    }],
    [{ kind: 'shot', id: 's1' }, () => { SH('s1').lighting = '顶光单点 + 左侧冷色轮廓（右侧后期补）' }],
    [{ kind: 'shot', id: 's2' }, () => { const sh = SH('s2'); sh.lighting = '顶光 + 左侧轮廓光（水花在后期打亮）'; sh.notes = '棚内不洒水：水花全部用素材库素材后期合成，拍时让 coser 做出踩水、甩外套的动作' }],
  ]
  let k = 0
  const next = () => {
    const step = steps[k++]
    if (!step) {
      return run.done('<p>按「2 小时、2 盏灯、不能洒水」改了 5 处；已确认的 IP 拆解和创意方向没有动。</p><ul><li>布光从三灯压成两灯：顶光主光加左侧冷色轮廓，环境光改用反光板。</li><li>KV02 的水花全部改为后期合成。分镜图不用重画，只是拍法变了。</li><li>准备清单新增：蓝色反光板、水花素材，以及 2 小时的拍摄顺序。</li></ul><p>下面逐处看改动，确认或恢复。</p>')
    }
    const [u, mutate] = step
    run.step({ verb: '写入策划', obj: unitTitle(p, u) })
    if (u.kind === 'section') { S(u.key).agentFlag = 'writing'; if (current === p) rerenderSection(u.key) }
    setTimeout(() => { run.touch(u, mutate); next() }, 600)
  }
  next()
}
document.addEventListener('click', (e) => {
  if (!e.target.closest('.pe-agent')) return
  const go = e.target.closest('[data-go-doc]')
  if (go) return goUnit(go.dataset.goDoc, go.dataset.goSel)
  const rv = e.target.closest('[data-rv]')?.dataset.rv
  if (rv) { const [docId, runId, i] = rv.split('|'); return showRunDiff(docId, runId, Number(i)) }
  const gen = e.target.closest('[data-gen]')?.dataset.gen
  if (gen) {
    const gid = e.target.closest('[data-gid]')?.dataset.gid
    const t = convs.flatMap((c) => c.turns).find((x) => x.role === 'gen' && x.g.id === gid && x.g.state === 'ask')
    if (!t || panel.running) return
    if (gen === 'no') { t.g.state = 'no'; return rerenderAgent() }
    return runGen(t.g)
  }
  const rm = e.target.closest('[data-ag-rm]')?.dataset.agRm
  if (rm != null) { panel.draft.units.splice(Number(rm), 1); rerenderAgent(); return focusAgent() }
  const ag = e.target.closest('[data-ag]')?.dataset.ag
  const where = e.target.closest('.pe-agent').dataset.where
  if (ag === 'send') return sendAgent(where)
  if (ag === 'close') return setPanelOpen(false)
  if (ag === 'rm-skill') { panel.draft.skill = false; rerenderAgent(); return focusAgent() }
  if (ag === 'later') return toast('原型不演示生成')
  if (ag === 'to-work') { panel.viewId = current.workId; return rerenderAgent() }
  if (ag === 'threads') {
    return openMenu([
      { section: '本画布的对话' },
      ...convs.map((c) => ({ label: c.title, checked: c.id === panel.viewId, hint: [...plans.values()].some((p) => p.workId === c.id) ? '策划工作会话' : '', run: () => { panel.viewId = c.id; rerenderAgent() } })),
      'sep',
      { label: '新对话', icon: 'plus', run: () => { const c = { id: id('conv'), title: '新对话', turns: [] }; convs.unshift(c); panel.viewId = c.id; rerenderAgent(); focusAgent() } },
    ], { rect: e.target.closest('button').getBoundingClientRect() })
  }
  if (ag === 'add-unit') {
    const pick = (u) => () => { if (!panel.draft.units.some((x) => sameUnit(x, u))) panel.draft.units.push(u); if (u.kind === 'doc') panel.draft.skill = true; rerenderAgent(); focusAgent() }
    const skillItem = panel.draft.skill ? [] : [{ label: `带上 Skill · ${SKILL_NAME}`, icon: 'sparkles', run: () => { panel.draft.skill = true; rerenderAgent() } }, 'sep']
    if (where === 'canvas') {
      return openMenu([...skillItem, { section: '画布上的策划' }, ...nodes.filter((n) => n.kind === 'plan').map((n) => ({ label: plans.get(n.doc).title, icon: 'clapper', run: pick({ kind: 'doc', docId: n.doc }) }))], { rect: e.target.closest('button').getBoundingClientRect() })
    }
    const p = current
    return openMenu([
      ...skillItem,
      { label: '整份策划', run: pick({ kind: 'doc', docId: p.id }) },
      { section: '章节' },
      ...p.sections.map((x) => ({ label: tmplOf(x.key).title, run: pick({ kind: 'section', key: x.key, docId: p.id }) })),
      { section: '视觉设定' },
      ...p.settings.items.map((x) => ({ label: `${x.kind === 'character' ? '人设' : '场景'} · ${x.name}`, run: pick({ kind: 'setting', id: x.id, docId: p.id }) })),
      { section: '分镜' },
      { label: '分镜与交付（全部镜头）', run: pick({ kind: 'shots', docId: p.id }) },
    ], { rect: e.target.closest('button').getBoundingClientRect() })
  }
})
document.addEventListener('input', (e) => { if (e.target.matches('[data-ag-input]')) panel.draft.text = e.target.value })
document.addEventListener('keydown', (e) => {
  if (!e.target.matches('[data-ag-input]')) return
  if (e.key === 'Escape') { e.stopPropagation(); e.target.blur(); return }
  if (e.key !== 'Enter' || e.shiftKey || e.isComposing) return
  e.preventDefault()
  sendAgent(e.target.closest('.pe-agent').dataset.where)
}, true)
railAgent.addEventListener('click', () => setPanelOpen(!panel.open))

/* ───────── 演示 ───────── */
const demo = document.getElementById('demo')
demo.addEventListener('click', () => {
  openMenu([
    { section: '在打开的策划里演示' },
    { label: '只剩两小时、两盏灯、不能洒水', icon: 'sparkles', sub: '填好一句要求，发送后逐处看改动', run: scenarioDraft },
    { label: '助手改写「光源逻辑」', icon: 'sparkles', run: agentWrite },
    { label: '你和助手同时改「光源逻辑」', icon: 'clock', run: agentConflict },
    { label: '助手换一版人设（看分镜过时提示）', icon: 'sparkles', run: () => { ensureOpen('#sec-visual_settings'); setTimeout(() => { const x = findSet(current, 'set_char'); const unit = { kind: 'setting', id: x.id, docId: current.id }; const run = logAgent(current, { text: '人设的帽子比例不对，换一版', units: [{ ...unit, label: unitInfo(unit).label }] }, [{ verb: '生成图片', obj: `${x.name} · 三视图` }]); setTimeout(() => { updateSetting(current, x, run); run.done(`<p>「${esc(x.name)}」换了一版 #${x.rev}，有 ${staleCount(current, x.id)} 张分镜基于旧设定。</p>`) }, 1200) }, 300) } },
    { label: offline ? '恢复网络' : '模拟断网保存', icon: 'globe', run: () => { offline = !offline; toast(offline ? '已断网：修改留在本机，顶栏显示「未保存 · 内容在本机」' : '网络已恢复，待保存的修改会自动提交') } },
    'sep',
    { label: '重置示例', run: () => { if (!editor.hidden) closeEditor(); offline = false; saving.clear(); reset() } },
  ], { rect: demo.getBoundingClientRect(), align: 'end' }, demo)
})
function updateSetting(p, x, run) {
  run.touch({ kind: 'setting', id: x.id }, () => { x.views = x.views.map((v, i) => ({ ...v, n: x.kind === 'character' ? ['03', '08', '10'][i % 3] : v.n })) })
  if (current === p) toast(`「${x.name}」已换一版，${staleCount(p, x.id)} 张分镜需要更新`)
}
function ensureOpen(sel = '#sec-lighting') {
  if (editor.hidden) openEditor(nodes.find((n) => n.doc === 'doc_furina'))
  setTimeout(() => editor.querySelector(sel)?.scrollIntoView({ behavior: 'smooth', block: 'start' }), 300)
}
// 把场景要求填进输入框（带整份策划的标签），由摄影师按发送
function scenarioDraft() {
  if (editor.hidden) openEditor(nodes.find((n) => n.doc === 'doc_furina'))
  setTimeout(() => {
    panel.draft = { text: SCENARIO_TEXT, units: [{ kind: 'doc', docId: current.id }], skill: true }
    if (!agentInEditor()) setPanelOpen(true)
    else { rerenderAgent(); focusAgent() }
    toast('已填好要求，按 Enter 发送')
  }, 300)
}
function agentWrite() {
  ensureOpen()
  const s = sec('lighting')
  setTimeout(() => {
    const unit = { kind: 'section', key: 'lighting', docId: current.id }
    const run = logAgent(current, { text: '光源这块补一下地面反光的做法', units: [{ ...unit, label: unitInfo(unit).label }] }, [{ verb: '写入策划', obj: '光源逻辑' }])
    s.agentFlag = 'writing'
    rerenderSection('lighting')
    setTimeout(() => {
      run.touch({ kind: 'section', key: 'lighting' }, () => { s.body = s.body.replace('</p><h3>特殊光效</h3>', '</p><p>补一条：地面铺黑色亚克力板，直接拍出舞台地面的反光，减少后期合成的工作量。</p><h3>特殊光效</h3>') })
      run.done(`<p>补好了「光源逻辑」#${s.rev}：地面铺黑色亚克力板，直接拍出舞台地面的反光。</p>`)
    }, 1800)
  }, 500)
}
function agentConflict() {
  ensureOpen()
  const s = sec('lighting')
  setTimeout(() => {
    const theirsBody = s.body.replace('柔光箱加蜂巢收光', '<strong>改用雷达罩</strong>加蜂巢收光，脸部反差更硬')
    s.conflict = {
      rev: current.revision + 1,
      theirsBody,
      mineBody: s.body.replace('偏前 30°', '偏前 45°'),
      theirsRefs: [...s.refs, ref(id('r'), '04', '光位参考', '雷达罩 + 蜂巢的硬光效果')],
      mineRefs: [...s.refs],
      theirs: '主光放在头顶偏前 30°，<ins>改用雷达罩</ins>加蜂巢收光，<ins>脸部反差更硬</ins>；<ins>新加参考图「雷达罩 + 蜂巢」</ins>。',
      mine: '主光放在头顶偏前 <ins>45°</ins>，柔光箱加蜂巢收光，保证人物与剧院光位一致。',
    }
    current.revision += 1
    current.history.unshift({ rev: current.revision, who: 'agent', run: '拍摄策划 · 芙宁娜', at: now(), what: ['光源逻辑'] })
    rerenderSection('lighting')
    refreshChrome()
  }, 500)
}

/* 选中正文或镜头字段里的一段文字：浮出「请助手修改」，带着这段摘录进对话 */
const selAsk = document.createElement('button')
selAsk.className = 'sel-ask'
selAsk.hidden = true
selAsk.innerHTML = `${I('sparkles')}请助手修改这段`
document.body.append(selAsk)
let selCtx = null
function placeSelAsk() {
  const sel = getSelection()
  if (editor.hidden || previewRev || !sel.rangeCount || sel.isCollapsed) { selAsk.hidden = true; return }
  const r = sel.getRangeAt(0)
  const n = r.commonAncestorContainer
  const host = (n.nodeType === 1 ? n : n.parentElement)?.closest('.rich[data-rich], dd[data-shot]')
  const text = sel.toString().replace(/\s+/g, ' ').trim()
  if (!host || !text) { selAsk.hidden = true; return }
  selCtx = host.dataset.rich ? { kind: 'section', key: host.dataset.rich, quote: clip(text, 200) } : { kind: 'shot', id: host.dataset.shot, quote: clip(text, 200) }
  const rect = r.getBoundingClientRect()
  selAsk.hidden = false
  selAsk.style.left = `${Math.max(8, Math.min(rect.left + rect.width / 2 - 64, innerWidth - 150))}px`
  selAsk.style.top = `${rect.top - 40 < 64 ? rect.bottom + 8 : rect.top - 40}px`
}
document.addEventListener('selectionchange', placeSelAsk)
editor.addEventListener('scroll', placeSelAsk, true)
selAsk.addEventListener('pointerdown', (e) => e.preventDefault())
selAsk.addEventListener('click', () => { selAsk.hidden = true; getSelection().removeAllRanges(); askAgent(selCtx, '') })

addEventListener('beforeunload', (e) => { if (saving.size) e.preventDefault() })
hydrate()
reset()
