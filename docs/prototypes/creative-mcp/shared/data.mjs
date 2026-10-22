// 原型共用的示例数据：账号、宿主、连接、画布、图片。全部是假数据，不连后端。
const here = (p) => new URL(p, import.meta.url).href

// 图片沿用 v4 原型的样张；用绝对地址，iframe 里的卡片也能直接加载。
export const photo = (n) => here(`../../creative-workspace/v4/assets/photo-${String(n).padStart(2, '0')}.jpg`)

// 生产域名占位。文字里显示这个地址，点击时打开本地原型页面。
export const ORIGIN = 'https://studio.example.com'
export const MCP_URL = `${ORIGIN}/mcp`
export const page = (path) => here(`../web/${path}`)

export const ACCOUNT = { name: 'Samson', credits: 1252 }

// 首批四个宿主（D7）。kind 决定有没有内嵌卡片。
export const HOSTS = [
  { id: 'chatgpt', name: 'ChatGPT', kind: 'gui', mark: 'G' },
  { id: 'claude', name: 'Claude', sub: '桌面端 / 网页', kind: 'gui', mark: 'C' },
  { id: 'claude_code', name: 'Claude Code', kind: 'tui', mark: '>' },
  { id: 'codex', name: 'Codex CLI', kind: 'tui', mark: '›' },
]

export const CONNECTIONS = [
  { id: 'cxcon_8h21', host: 'codex', name: 'Codex', client: '本机 · 127.0.0.1 回调', authorized: '10 月 20 日', last: '3 分钟前', active: true },
  { id: 'cxcon_3k9p', host: 'claude', name: 'Claude', client: 'claude.ai', authorized: '10 月 18 日', last: '昨天 21:40', active: false },
]

export const CANVAS = {
  id: 'cv_9f2a',
  name: '九月，向海而行',
  edited: '10 分钟前',
  url: `${ORIGIN}/creative/canvases/cv_9f2a`,
}
export const OTHER_CANVASES = [
  { id: 'cv_2b7c', name: '雪山婚纱 · 二稿', nodes: 18, edited: '昨天' },
  { id: 'cv_5e1d', name: '品牌 Lookbook · 春季', nodes: 9, edited: '10 月 16 日' },
]

// 画布上的节点：坐标是画布世界坐标，宽度固定，高度按图片比例。
export const NODES = [
  { id: 'ref-1', title: '参考 · 海风', x: 40, y: 40, w: 180, img: photo(10), ratio: 2 / 3, group: 'refs' },
  { id: 'ref-2', title: '参考 · 留白', x: 40, y: 360, w: 180, img: photo(11), ratio: 4 / 3, group: 'refs' },
  { id: 'shot-1', title: '镜头 01 · 礁石远景', x: 320, y: 40, w: 240, img: photo(2), ratio: 3 / 2 },
  { id: 'shot-2', title: '镜头 02 · 逆光长裙', x: 320, y: 260, w: 240, img: photo(1), ratio: 2 / 3 },
  { id: 'shot-3', title: '镜头 03 · 回眸', x: 640, y: 40, w: 240, img: photo(3), ratio: 2 / 3 },
  { id: 'scene-1', title: '场景 · 海边礁石', x: 640, y: 440, w: 280, img: photo(4), ratio: 3 / 2, scene: true },
]

export const canvasUrl = (focus) => `${CANVAS.url}${focus ? `?focus=${focus}` : ''}`
export const canvasPage = (q = '') => page(`canvas.html${q}`)
