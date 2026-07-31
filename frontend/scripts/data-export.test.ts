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

const client = await import('../src/api/client.ts')
const session = await import('../src/auth/session.ts')
const download = await import('../src/components/dataExportDownload.ts')

const exactFilename = 'photographer-crm-export-20260721T083015Z.json'
const authAccount = {
  id: 'account-fixture', email: 'fixture@example.invalid',
  created_at: '2026-07-31T00:00:00Z', timezone: 'Asia/Shanghai',
}
const access = (token: string) => ({ access_token: token, token_type: 'Bearer' as const, expires_in: 600 as const })

test('fetchDataExport sends Bearer and returns a complete JSON Blob with the exact server filename', async () => {
  session.setAuthenticated(access('fixture-token'), authAccount)
  let authorization = ''
  globalThis.fetch = async (_input, init) => {
    authorization = new Headers(init?.headers).get('Authorization') ?? ''
    return new Response(new Blob(['{"schema_version":1}'], { type: 'application/json' }), {
      status: 200,
      headers: {
        'Content-Type': 'application/json',
        'Content-Disposition': `attachment; filename="${exactFilename}"`,
      },
    })
  }

  const result = await client.fetchDataExport()

  assert.equal(authorization, 'Bearer fixture-token')
  assert.equal(result.filename, exactFilename)
  assert.equal(await result.blob.text(), '{"schema_version":1}')
})

test('fetchDataExport accepts only application/json base media type with legal parameters', async () => {
  for (const contentType of [
    'application/json',
    'Application/JSON; charset=utf-8',
    'application/json; charset="utf-8"; profile=fixture',
  ]) {
    globalThis.fetch = async () => exportResponse(contentType)
    await assert.doesNotReject(client.fetchDataExport())
  }

  for (const contentType of [
    '',
    'application/json; charset',
    'text/application/json',
    'text/plain; note=application/json',
    'application/problem+json',
    'application/jsonish',
  ]) {
    globalThis.fetch = async () => exportResponse(contentType)
    await assert.rejects(client.fetchDataExport(), client.ApiError)
  }
})

test('fetchDataExport accepts only the whole fixed filename and otherwise uses a safe UTC fallback', async () => {
  globalThis.fetch = async () => exportResponse('application/json', `attachment; filename="${exactFilename}"`)
  assert.equal((await client.fetchDataExport()).filename, exactFilename)

  for (const disposition of [
    null,
    'attachment; filename="../escape.json"',
    'attachment; filename="photographer-crm-export-20260721T083015Z.json.exe"',
    'attachment; filename="unexpected.json"',
    `attachment; filename="${'a'.repeat(300)}"`,
    `attachment; filename=${exactFilename}`,
  ]) {
    globalThis.fetch = async () => exportResponse('application/json', disposition)
    const result = await client.fetchDataExport()
    assert.match(result.filename, /^photographer-crm-export-[0-9]{8}T[0-9]{6}Z\.json$/)
  }
})

test('fetchDataExport reuses ApiError for 500 and 401 responses', async () => {
  session.setAuthenticated(access('fixture-token'), authAccount)
  globalThis.fetch = async () => Response.json({ error: { code: 'internal', message: 'fixture failure' } }, { status: 500 })
  await assert.rejects(client.fetchDataExport(), (error: unknown) =>
    error instanceof client.ApiError && error.status === 500 && error.code === 'internal')

  session.setAuthenticated(access('fixture-token'), authAccount)
  globalThis.fetch = async () => Response.json({ error: { code: 'unauthorized', message: '未认证' } }, { status: 401 })
  await assert.rejects(client.fetchDataExport(), (error: unknown) =>
    error instanceof client.ApiError && error.status === 401)
  assert.equal(session.getAccessToken(), null)
})

test('body/blob rejection returns no result and download collaboration creates no object URL', async () => {
  globalThis.fetch = async () => ({
    ok: true,
    status: 200,
    headers: new Headers({
      'Content-Type': 'application/json',
      'Content-Disposition': `attachment; filename="${exactFilename}"`,
    }),
    blob: async () => { throw new Error('synthetic body interruption') },
  }) as Response

  let created = 0
  let clicked = 0
  Object.defineProperty(URL, 'createObjectURL', { configurable: true, value: () => { created += 1; return 'blob:fixture' } })
  Object.defineProperty(URL, 'revokeObjectURL', { configurable: true, value: () => undefined })
  Object.defineProperty(globalThis, 'document', {
    configurable: true,
    value: { createElement: () => ({ click: () => { clicked += 1 }, href: '', download: '' }) },
  })

  await assert.rejects(download.requestAndDownloadDataExport(), /synthetic body interruption/)
  assert.equal(created, 0)
  assert.equal(clicked, 0)
})

test('download collaboration triggers once and always releases its object URL', async () => {
  let created = 0
  let clicked = 0
  const revoked: string[] = []
  Object.defineProperty(URL, 'createObjectURL', { configurable: true, value: () => `blob:fixture-${++created}` })
  Object.defineProperty(URL, 'revokeObjectURL', { configurable: true, value: (url: string) => revoked.push(url) })
  Object.defineProperty(globalThis, 'document', {
    configurable: true,
    value: { createElement: () => ({ click: () => { clicked += 1 }, href: '', download: '' }) },
  })

  download.saveDataExport({ blob: new Blob(['{}']), filename: exactFilename })

  assert.equal(created, 1)
  assert.equal(clicked, 1)
  assert.deepEqual(revoked, ['blob:fixture-1'])
})

test('DataExportCard exposes loading, retry, 401, PII, avatar, and persistence-safe contracts', async () => {
  const card = await readFile(new URL('../src/components/DataExportCard.tsx', import.meta.url), 'utf8')
  for (const contract of [
    '导出全部 JSON 数据',
    '正在准备导出…',
    '姓名、手机号、社交身份、备注、Telegram chat ID',
    '受控位置',
    '及时删除',
    '头像图片未包含',
    '不可跨部署恢复',
    'role="alert"',
  ]) {
    assert.match(card, new RegExp(contract))
  }
  assert.match(card, /disabled=\{downloading\}/)
  assert.match(card, /if \(downloading\) return/)
  assert.match(card, /error instanceof ApiError && error\.status === 401/)
  assert.doesNotMatch(card, /localStorage|sessionStorage|indexedDB|console\.(?:log|error)/)

  const settingsPage = await readFile(new URL('../src/pages/SettingsPage.tsx', import.meta.url), 'utf8')
  assert.match(settingsPage, /<DataExportCard/)
  assert.match(settingsPage, /const dataExportCard[\s\S]*if \(!presentation\.showReadyData \|\| !settings\)/)
  assert.match(settingsPage, /settings-stack/)
})

test('DataExportCard relies on native button keyboard behavior and shared mobile-safe styles', async () => {
  const card = await readFile(new URL('../src/components/DataExportCard.tsx', import.meta.url), 'utf8')
  const css = await readFile(new URL('../src/index.css', import.meta.url), 'utf8')
  assert.match(card, /<button className="btn btn-primary" type="button"/)
  assert.match(css, /\.btn:focus-visible[^{]*\{[^}]*outline:\s*2px solid var\(--accent\)/s)
  assert.match(css, /\.settings-stack\s*\{[^}]*max-width:\s*40rem/s)
  assert.match(css, /\.form-stack\s*\{[^}]*flex-direction:\s*column/s)
})

function exportResponse(contentType: string, disposition: string | null = `attachment; filename="${exactFilename}"`): Response {
  const headers = new Headers()
  if (contentType) headers.set('Content-Type', contentType)
  if (disposition) headers.set('Content-Disposition', disposition)
  return new Response(new Blob(['{}']), { status: 200, headers })
}
