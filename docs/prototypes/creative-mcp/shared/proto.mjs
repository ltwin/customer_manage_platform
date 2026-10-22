// 原型状态切换：页面右下角的小面板，只在原型里出现。选中的状态写进地址栏 ?state=，刷新后保持。
export function mountStates({ options, onChange, label = '原型状态' }) {
  const q = new URLSearchParams(location.search)
  let value = options.some((o) => o.id === q.get('state')) ? q.get('state') : options[0].id
  const el = document.createElement('div')
  el.className = 'proto-states'
  el.setAttribute('role', 'group')
  el.setAttribute('aria-label', label)
  const draw = () => {
    el.innerHTML = `<span>${label}</span>${options.map((o) => `<button type="button" data-id="${o.id}" aria-pressed="${o.id === value}">${o.label}</button>`).join('')}`
  }
  el.addEventListener('click', (e) => {
    const b = e.target.closest('[data-id]')
    if (!b || b.dataset.id === value) return
    value = b.dataset.id
    const u = new URL(location.href)
    u.searchParams.set('state', value)
    history.replaceState(null, '', u)
    draw()
    onChange(value)
  })
  draw()
  document.body.append(el)
  onChange(value)
  return { get value() { return value } }
}

export function toast(text, action) {
  document.querySelector('.toast')?.remove()
  const t = document.createElement('div')
  t.className = 'toast'
  t.setAttribute('role', 'status')
  t.innerHTML = `<span></span>${action ? `<button class="toast-act" type="button">${action.label}</button>` : ''}`
  t.firstChild.textContent = text
  if (action) t.querySelector('button').addEventListener('click', () => { t.remove(); action.run() })
  document.body.append(t)
  setTimeout(() => t.remove(), 4000)
}

export async function copy(text) {
  try {
    await navigator.clipboard.writeText(text)
    toast('已复制')
  } catch {
    toast('浏览器不允许复制，请手动选中')
  }
}
