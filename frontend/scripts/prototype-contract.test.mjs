import { readFile } from 'node:fs/promises'
import { test } from 'node:test'
import assert from 'node:assert/strict'

const root = new URL('../', import.meta.url)

async function read(relativePath) {
  return readFile(new URL(relativePath, root), 'utf8')
}

test('prototype routes are mounted in the React app', async () => {
  const app = await read('src/App.tsx')
  for (const route of ['/dashboard', '/customers', '/customers/new', '/customers/:id', '/calendar', '/packages']) {
    assert.match(app, new RegExp(`path="${route.replace('/', '\\/')}"`))
  }
})

test('prototype data keeps the roadmap entities available for UI foundations', async () => {
  const data = await read('src/crm/prototypeData.ts')
  for (const name of ['customers', 'packages', 'orders', 'slots', 'reminders']) {
    assert.match(data, new RegExp(`export const ${name}\\b`))
  }
  for (const enumValue of ['xiaohongshu', 'douyin', 'weibo', 'referral', 'wechat', 'telegram']) {
    assert.match(data, new RegExp(enumValue))
  }
})

test('migrated design system exposes shell, dashboard, list, detail, calendar, and package styles', async () => {
  const css = await read('src/index.css')
  for (const token of ['--bg', '--surface', '--fg', '--muted', '--border', '--accent']) {
    assert.match(css, new RegExp(token))
  }
  for (const selector of ['.app-shell', '.sidebar', '.bottom-nav', '.stat-grid', '.table-wrap', '.detail-grid', '.cal-grid', '.pkg-grid']) {
    assert.match(css, new RegExp(selector.replace('.', '\\.')))
  }
  assert.match(css, /\[data-theme="dark"\]/)
})

test('new customer referral picks from active customers instead of manual id input', async () => {
  const page = await read('src/pages/CustomerNewPage.tsx')

  assert.match(page, /import CustomerPicker from ['"]\.\.\/components\/customers\/CustomerPicker['"]/)
  assert.match(page, /<CustomerPicker[\s\S]*candidateStatuses=\{\['active'\]\}/)
  assert.match(page, /onChange=\{\(choice\) => setReferrerCustomerID\(choice\?\.id \?\? ''\)\}/)
  assert.match(page, /请选择介绍人/)
  assert.doesNotMatch(page, /介绍人客户 ID/)
  assert.doesNotMatch(page, /<select\s+id="referrerCustomerID"/)
})
