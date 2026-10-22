// W-1 外部助手接入页：选宿主 → 添加（复制发给助手，或按步骤手动加）→ 授权 → 试一句。
// 右栏是已连接列表和断开。菜单名称照各宿主 2026-10 的文档写，上线前按 U1 再核对一遍。
import { mountSide } from '../../creative-workspace/v6/side.mjs'
import { ic, esc } from '../../creative-workspace/v6/ui.mjs'
import { HOSTS, CONNECTIONS, MCP_URL, photo } from '../shared/data.mjs'
import { mountStates, toast, copy } from '../shared/proto.mjs'

const $ = (s) => document.querySelector(s)
const V6 = new URL('../../creative-workspace/v6/', import.meta.url)
const REPO = 'https://github.com/<待定>/creative-plugins.git' // U3：公开插件仓库位置待 owner 定

const ADMIN_TEXT = (where) =>
  `你好，我想在${where}里用创意空间（我的创作工具）。请帮忙在组织设置里添加一个自定义连接器：\n名称：创意空间\n地址：${MCP_URL}\n认证：OAuth（每个成员各自登录授权自己的账号，彼此看不到数据）\n添加后我自己完成授权就行，谢谢！`

// 每个宿主的接入方式。agent：可以整段发给助手让它自己装；steps：只能自己动手；fallback：没有权限时
const GUIDES = {
  codex: {
    title: '让 Codex 自己安装插件',
    agent: `帮我安装创意空间插件：\n1. 执行 codex plugin marketplace add ${REPO}\n2. 执行 codex plugin add creative@creative-plugins\n3. 装好后提醒我新开一个会话，按提示在浏览器里授权创意空间账号。`,
    agentLabel: '复制发给 Codex',
    manual: {
      label: '不用插件，自己配置',
      code: `codex mcp add creative --url ${MCP_URL}\ncodex mcp login creative`,
      note: '插件会把工具超时调到 5 分钟；手动配置用默认的 60 秒，长任务靠 wait_tasks 续等，也能用。',
    },
    caveat: '插件仓库地址待定（U3）',
  },
  claude_code: {
    title: '在 Claude Code 里授权',
    steps: [
      '先在 Claude 里添加「创意空间」连接器（见「Claude」那一项）。同一个账号的 Claude Code 会自动看到它。',
      '在 Claude Code 里输入 <code>/mcp</code>，选「创意空间」，按提示在浏览器里授权。',
    ],
    fallback: {
      title: '没有添加连接器的权限，或者用 API key 登录 Claude Code？',
      text: '改用本机配置，只在这台电脑上生效：',
      agent: `帮我连接创意空间：执行 claude mcp add --transport http --scope user creative ${MCP_URL}，然后提醒我输入 /mcp 完成授权。`,
      agentLabel: '复制发给 Claude Code',
    },
  },
  claude: {
    title: '在 Claude 里添加连接器',
    steps: [
      '打开 Claude 的「设置 → 连接器」，点「添加自定义连接器」。',
      `名称填「创意空间」，地址填 <code>${MCP_URL}</code>。`,
      '点「连接」，在打开的授权页登录并允许。',
      '在对话框的「＋ → 连接器」里打开「创意空间」。',
    ],
    tip: '桌面端、网页和 Claude Code 共用同一个连接器，加一次就行。',
    fallback: {
      title: '看不到「添加自定义连接器」？',
      text: 'Team 和 Enterprise 由组织管理员添加。把下面这段发给管理员；等不及的话，可以先用 Claude Code 的本机配置。',
      agent: ADMIN_TEXT(' Claude '),
      agentLabel: '复制发给管理员',
    },
  },
  chatgpt: {
    title: '在 ChatGPT 里添加应用',
    steps: [
      '打开 ChatGPT 的「设置 → 应用 → 高级设置」，打开「开发者模式」。',
      `点「创建应用」：名称填「创意空间」，MCP 服务器地址填 <code>${MCP_URL}</code>，认证选 OAuth。`,
      '保存后会打开创意空间的授权页，登录并允许。',
      '新对话里点「＋」选择「创意空间」，就可以开始了。',
    ],
    fallback: {
      title: '工作区里看不到「开发者模式」？',
      text: 'Business 和 Enterprise 工作区要由管理员开启开发者模式，或者由管理员添加这个应用。把下面这段发给管理员：',
      agent: ADMIN_TEXT(' ChatGPT 工作区').replace('自定义连接器', '自定义应用（MCP）'),
      agentLabel: '复制发给管理员',
    },
  },
}

const STARTERS = ['看看我最近的画布', '把《九月，向海而行》的镜头 02 改成黄昏，先生成两张看看', '按镜头 02 搭个 3D 场景，回看一下主机位']

let host = 'codex'
let conns = CONNECTIONS.map((c) => ({ ...c }))

// ---- 侧栏：复用 v6 的应用侧栏，加一项「外部助手」 ----
mountSide($('#side'), {
  recent: [
    { name: '九月，向海而行', thumb: photo(10), state: 'run', href: 'canvas.html' },
    { name: '雪山婚纱 · 二稿', thumb: photo(2) },
    { name: '品牌 Lookbook · 春季', thumb: photo(9), state: 'wait' },
  ],
})
$('#side').querySelectorAll('a[href^="./"]').forEach((a) => (a.href = new URL(a.getAttribute('href'), V6).href))
$('#side .side-nav').insertAdjacentHTML('beforeend', `<a href="connect.html" data-view="agents" data-tip="外部助手" aria-current="page">${ic('link')}<span class="side-t">外部助手</span></a>`)

// ---- 宿主选择 ----
function renderHosts() {
  $('#hosts').innerHTML = HOSTS.map(
    (h) => `<button type="button" role="radio" aria-checked="${h.id === host}" data-host="${h.id}">
      <span class="mark">${esc(h.mark)}</span><span class="hn">${esc(h.name)}</span><span class="hs">${esc(h.sub ?? (h.kind === 'tui' ? '终端' : ''))}</span></button>`,
  ).join('')
}

const copyBlock = (text, label) => `<div class="copy-block"><pre>${esc(text)}</pre><button type="button" class="btn btn-primary" data-copy="${esc(text)}">${ic('copy')}${label}</button></div>`

function renderGuide() {
  const g = GUIDES[host]
  $('#add-t').textContent = g.title
  let html = ''
  if (g.agent) html += `<p class="p2">复制这段话，发给 ${host === 'codex' ? 'Codex' : '助手'}，它会自己执行：</p>${copyBlock(g.agent, g.agentLabel)}`
  if (g.caveat) html += `<p class="p3 warn">${esc(g.caveat)}</p>`
  if (g.manual)
    html += `<details class="more"><summary>${g.manual.label}</summary><div class="field code"><pre>${esc(g.manual.code)}</pre><button class="icon-btn" data-copy="${esc(g.manual.code)}" aria-label="复制命令">${ic('copy')}</button></div><p class="p3">${esc(g.manual.note)}</p></details>`
  if (g.steps) html += `<ol class="sub-steps">${g.steps.map((s) => `<li>${s}</li>`).join('')}</ol>`
  if (g.tip) html += `<p class="p3">${esc(g.tip)}</p>`
  if (g.fallback)
    html += `<details class="more fallback"><summary>${esc(g.fallback.title)}</summary><p class="p2">${esc(g.fallback.text)}</p>${copyBlock(g.fallback.agent, g.fallback.agentLabel)}${host === 'claude' ? '<p class="p3"><button type="button" class="link" data-goto="claude_code">改用 Claude Code 本机配置 →</button></p>' : ''}</details>`
  $('#add').innerHTML = html
}

// ---- 已连接 ----
function renderConns() {
  if (!conns.length) {
    $('#conns').innerHTML = `<li class="empty">还没有外部助手连接。按左边的步骤接入后，会出现在这里。</li>`
    return
  }
  $('#conns').innerHTML = conns
    .map(
      (c) => `<li data-id="${c.id}">
        <span class="mark">${esc(HOSTS.find((h) => h.id === c.host)?.mark)}</span>
        <div class="c-main"><b>${esc(c.name)}</b><small>${esc(c.client)} · 授权于 ${esc(c.authorized)}</small>
          <small class="last"><i class="dot ${c.active ? 'on' : ''}"></i>${c.active ? `活跃 · ${esc(c.last)}调用` : `最近调用：${esc(c.last)}`}</small></div>
        <button type="button" class="btn sm" data-disconnect="${c.id}">断开</button>
        <div class="confirm" hidden><p>断开 ${esc(c.name)}？它要重新授权才能再用；排队中的场景任务会取消。</p>
          <div><button type="button" class="btn sm danger" data-yes="${c.id}">断开</button><button type="button" class="btn sm" data-no>取消</button></div></div>
      </li>`,
    )
    .join('')
}

document.addEventListener('click', (e) => {
  const t = e.target
  const h = t.closest('[data-host]')
  if (h) {
    host = h.dataset.host
    renderHosts()
    renderGuide()
  }
  const g = t.closest('[data-goto]')
  if (g) {
    host = g.dataset.goto
    renderHosts()
    renderGuide()
    $('#add .fallback')?.setAttribute('open', '')
  }
  const c = t.closest('[data-copy]')
  if (c) copy(c.dataset.copy)
  const d = t.closest('[data-disconnect]')
  if (d) {
    const li = d.closest('li')
    li.querySelector('.confirm').hidden = false
    d.hidden = true
  }
  if (t.closest('[data-no]')) renderConns()
  const y = t.closest('[data-yes]')
  if (y) {
    const c2 = conns.find((x) => x.id === y.dataset.yes)
    conns = conns.filter((x) => x !== c2)
    renderConns()
    toast(`已断开 ${c2.name}，它的令牌已失效`)
  }
})

$('#starters').innerHTML = STARTERS.map((s) => `<button type="button" class="starter" data-copy="${esc(s)}">${ic('copy')}${esc(s)}</button>`).join('')
$('#mcp-url').textContent = MCP_URL
$('#copy-url').innerHTML = ic('copy')
$('#copy-url').dataset.copy = MCP_URL

renderHosts()
renderGuide()
mountStates({
  options: [
    { id: 'connected', label: '有连接' },
    { id: 'empty', label: '还没有连接' },
  ],
  onChange: (v) => {
    conns = v === 'empty' ? [] : CONNECTIONS.map((c) => ({ ...c }))
    renderConns()
  },
})
