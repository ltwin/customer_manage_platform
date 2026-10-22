// W-2 授权确认页、W-3 授权完成页。
// 授权页就是 account 级 consent（D5）：一次允许 = 全部创意能力，所以文字要把「能做什么、不能做什么、花不花钱」说清楚。
// 客户端身份分两种：
// - CIMD：client_id 是一个网址，能显示来源域名；
// - DCR：自行注册，名称无法验证，要加警示。
import { ACCOUNT } from '../shared/data.mjs'
import { mountStates } from '../shared/proto.mjs'

const $ = (s) => document.querySelector(s)
const esc = (s) => String(s ?? '').replace(/[&<>"]/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;' })[c])
const svg = (d, w = 2) => `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="${w}" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">${d}</svg>`
const IC = {
  aperture: svg('<circle cx="12" cy="12" r="10"/><path d="m14.31 8 5.74 9.94M9.69 8h11.48M7.38 12l5.74-9.94M9.69 16 3.95 6.06M14.31 16H2.83m13.79-4-5.74 9.94"/>'),
  check: svg('<path d="M20 6 9 17l-5-5"/>', 2.4),
  x: svg('<path d="M18 6 6 18M6 6l12 12"/>', 2.2),
  alert: svg('<circle cx="12" cy="12" r="10"/><path d="M12 8v4M12 16h.01"/>'),
  shield: svg('<path d="M20 13c0 5-3.5 7.5-7.66 8.95a1 1 0 0 1-.67-.01C7.5 20.5 4 18 4 13V6a1 1 0 0 1 1-1c2 0 4.5-1.2 6.24-2.72a1.17 1.17 0 0 1 1.52 0C14.51 3.81 17 5 19 5a1 1 0 0 1 1 1z"/><path d="m9 12 2 2 4-4"/>'),
}

const CLIENTS = {
  codex: { mark: '›', name: 'Codex', ok: true, from: '客户端信息来自 openai.com', redirect: '127.0.0.1:1455', loopback: true },
  chatgpt: { mark: 'G', name: 'ChatGPT', ok: true, from: '客户端信息来自 chatgpt.com', redirect: 'chatgpt.com', loopback: false },
  unverified: { mark: '?', name: 'My MCP Client', ok: false, from: '这个客户端是自行注册的，名称无法验证', redirect: 'localhost:6274', loopback: true },
}
const CAN = [
  ['读取和修改你的画布、提示词', ''],
  ['生成图片和视频', `按你的规则扣点，本月剩余 ${ACCOUNT.credits.toLocaleString()} 点`],
  ['整理资产库', '包括移入回收站和恢复'],
  ['搭建 3D 场景，回看和导出分镜', '需要你打开创意空间页面'],
  ['读取平台和你自己的 Skill', ''],
]
const CANNOT = ['看到客户、订单、日程等经营数据', '看到你的密码，或修改账号和订阅设置']

let client = CLIENTS.codex

const li = (ic, t, small) => `<li>${ic}<span>${esc(t)}${small ? `<br><small>${esc(small)}</small>` : ''}</span></li>`

function consent() {
  return `
    <div class="client"><span class="client-mark">${esc(client.mark)}</span><span class="client-arrow">→</span><span class="client-mark us">${IC.aperture}</span></div>
    <h2>允许 ${esc(client.name)} 使用你的创意空间？</h2>
    <p class="verify ${client.ok ? 'ok' : ''}">${client.ok ? IC.shield : IC.alert}${esc(client.from)}</p>
    ${client.ok ? '' : `<div class="alert warn">${IC.alert}<span>只有你刚刚自己在助手里发起了连接，才点「允许」。不认识这个名字就拒绝。</span></div>`}
    <div class="account"><span class="av">${esc(ACCOUNT.name[0])}</span><div><b>${esc(ACCOUNT.name)}</b><small>授权给这个账号</small></div><button type="button" class="text-btn" data-act="switch">切换账号</button></div>
    <div class="scope">
      <div class="can"><h3>授权后它可以</h3><ul>${CAN.map(([t, s]) => li(IC.check, t, s)).join('')}</ul></div>
      <div class="cannot"><h3>它不能</h3><ul>${CANNOT.map((t) => li(IC.x, t)).join('')}</ul></div>
    </div>
    <div class="actions"><button type="button" class="btn" data-act="deny">拒绝</button><button type="button" class="btn btn-primary" data-act="allow">允许</button></div>
    <p class="fine">授权后回到 <code>${esc(client.redirect)}</code> · 随时可以在「创意空间 → 外部助手」里断开</p>`
}

function login() {
  return `
    <h2>登录后继续授权 ${esc(client.name)}</h2>
    <p class="verify">${esc(client.name)} 请求连接你的创意空间账号。先登录，下一步再确认授权范围。</p>
    <form id="login" novalidate>
      <div class="field"><label for="acc">手机号或邮箱</label><input id="acc" autocomplete="username" value="138 0000 0000"></div>
      <div class="field"><label for="pwd">密码</label><input id="pwd" type="password" autocomplete="current-password" value="photo2026"></div>
      <div class="actions" style="grid-template-columns:1fr"><button class="btn btn-primary" type="submit">登录并继续</button></div>
    </form>
    <p class="fine">原型里的账号密码只用于演示，不是真实凭证。</p>`
}

const result = (kind, title, text, extra = '') =>
  `<div class="result"><span class="big ${kind}">${kind === 'err' ? IC.alert : kind === 'no' ? IC.x : IC.check}</span><h2>${title}</h2><p>${text}</p>${extra}</div>`

function render(state) {
  const card = $('#card')
  if (state === 'login') card.innerHTML = login()
  else if (state === 'done')
    card.innerHTML = result('', `已连接 ${esc(client.name)}`, `可以关掉这个页面，回到 ${esc(client.name)} 继续。`, `<p class="fine">之后在「创意空间 → 外部助手」里能看到这个连接，也可以断开。</p>`)
  else if (state === 'returning') card.innerHTML = result('', '授权完成', `正在返回 ${esc(client.name)}…`, `<p class="fine">原型说明：网页宿主会直接跳回自己的页面，不停留在这里。</p>`)
  else if (state === 'denied') card.innerHTML = result('no', '已拒绝', `${esc(client.name)} 没有拿到任何权限，可以关掉这个页面。`)
  else if (state === 'error')
    card.innerHTML = result('err', '这个授权链接无效或已过期', '回到你的助手里，重新发起连接。', `<p class="fine">错误代码 <code>invalid_request</code> · 授权码 10 分钟内有效</p>`)
  else card.innerHTML = consent()
}

$('#brand-mark').innerHTML = IC.aperture
$('#card').addEventListener('click', (e) => {
  const act = e.target.closest('[data-act]')?.dataset.act
  if (act === 'switch') render('login')
  if (act === 'deny') render('denied')
  if (act === 'allow') {
    e.target.closest('button').disabled = true
    e.target.closest('button').textContent = '正在授权…'
    setTimeout(() => render(client.loopback ? 'done' : 'returning'), 700)
  }
})
$('#card').addEventListener('submit', (e) => {
  e.preventDefault()
  render('consent')
})

mountStates({
  options: [
    { id: 'codex', label: '本机宿主（Codex）' },
    { id: 'chatgpt', label: '网页宿主（ChatGPT）' },
    { id: 'unverified', label: '未验证的客户端' },
    { id: 'login', label: '未登录' },
    { id: 'done', label: '完成页' },
    { id: 'denied', label: '已拒绝' },
    { id: 'error', label: '链接无效' },
  ],
  onChange: (s) => {
    if (CLIENTS[s]) client = CLIENTS[s]
    render(CLIENTS[s] ? 'consent' : s)
  },
})
