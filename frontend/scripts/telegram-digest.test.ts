import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'

const storage = new Map<string, string>()
Object.defineProperty(globalThis, 'localStorage', {
  configurable: true,
  value: {
    getItem: (key: string) => storage.get(key) ?? null,
    setItem: (key: string, value: string) => storage.set(key, value),
    removeItem: (key: string) => storage.delete(key),
    clear: () => storage.clear(),
    key: (index: number) => [...storage.keys()][index] ?? null,
    get length() { return storage.size },
  } satisfies Storage,
})

const { createTelegramBindToken } = await import('../src/api/client.ts')
const { openTelegramDeepLink } = await import('../src/components/telegramBinding.ts')

test('bind-token API wrapper posts to the generated contract and never persists response', async () => {
  const requests: Array<{ url: string; init?: RequestInit }> = []
  globalThis.fetch = async (input, init) => {
    requests.push({ url: String(input), init })
    return Response.json({ token: 'opaque', deep_link: 'https://t.me/studio_digest_bot?start=opaque' })
  }
  const before = new Map(storage)
  const response = await createTelegramBindToken()
  assert.equal(response.token, 'opaque')
  assert.equal(requests[0]?.url, '/api/v1/settings/telegram/bind-token')
  assert.equal(requests[0]?.init?.method, 'POST')
  assert.deepEqual(storage, before)
})

test('deep-link opener uses noopener,noreferrer and reports popup blocking', () => {
  const calls: unknown[][] = []
  const opened = openTelegramDeepLink('https://t.me/example_bot?start=opaque', (...args) => {
    calls.push(args)
    return {} as Window
  })
  assert.equal(opened, true)
  assert.deepEqual(calls[0], ['https://t.me/example_bot?start=opaque', '_blank', 'noopener,noreferrer'])
  assert.equal(openTelegramDeepLink('https://t.me/example_bot?start=opaque', () => null), false)
})

test('Account settings page exposes binding states without rendering secrets or storage/log sinks', async () => {
  // D11：重定向到 AccountSettingsPage（旧 SettingsPage 已删）
  const source = await readFile(new URL('../src/account/settings/AccountSettingsPage.tsx', import.meta.url), 'utf8')
  for (const label of ['绑定 Telegram', '重新绑定', '正在生成绑定链接', '弹窗被浏览器拦截', 'role="alert"']) {
    assert.match(source, new RegExp(label))
  }
  assert.match(source, /className="btn btn-primary"/)
  assert.match(source, /disabled=\{binding\}/)
  assert.match(source, /onClick=\{\(\) => void onBindTelegram\(\)\}/)
  assert.doesNotMatch(source, /localStorage|sessionStorage|console\.(?:log|error)/)
  assert.doesNotMatch(source, /\{\s*(?:bind)?token\s*\}/i)
  assert.doesNotMatch(source, /href=\{[^}]*deepLink/i)
})

test('Telegram binding card keeps mobile wrapping and keyboard focus contracts', async () => {
  const css = await readFile(new URL('../src/index.css', import.meta.url), 'utf8')
  assert.match(css, /\.btn:focus-visible[^{]*\{[^}]*outline:\s*2px solid var\(--accent\)/s)
  assert.match(css, /\.telegram-binding-actions\s*\{[^}]*flex-wrap:\s*wrap/s)
  assert.match(css, /\.telegram-binding-actions \.btn\s*\{[^}]*min-width:\s*0/s)
})
