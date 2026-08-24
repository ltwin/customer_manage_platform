import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

// 应用外壳布局契约：内容列在宽屏下的水平对齐。
// .content 有 1280px max-width；若无水平 auto 边距，宽横屏（主区 > 1280px）下内容列贴左，
// 右侧留出大片失衡空白（2026-08-24 dashboard v2 桌面横屏验收反馈）。
// 与 avatar-layout.test.mjs 同模式（静态 CSS 断言，不起浏览器）。
const stylesheet = readFileSync(new URL('../src/index.css', import.meta.url), 'utf8')

test('content column centers horizontally instead of hugging the left edge on wide viewports', () => {
  const contentRule = stylesheet.match(/\.content\s*\{[^}]*\}/)?.[0] ?? ''
  assert.ok(contentRule.length > 0, 'missing .content rule')
  assert.match(
    contentRule,
    /max-width:\s*1280px/,
    '.content must keep its readable measure cap',
  )
  assert.match(
    contentRule,
    /margin-inline:\s*auto\s*;/,
    'capped .content must center horizontally so wide landscape screens do not leave a lopsided right blank strip',
  )
})

test('content-wide variant fills the main area with a bounded ceiling', () => {
  const wideRule = stylesheet.match(/\.content\.content-wide\s*\{[^}]*\}/)?.[0] ?? ''
  assert.ok(wideRule.length > 0, 'missing .content.content-wide rule')
  assert.match(
    wideRule,
    /max-width:\s*1760px/,
    'the wide variant must raise the cap so data-dense pages fill landscape screens, yet stay bounded (centered) on ultrawide displays',
  )
})
