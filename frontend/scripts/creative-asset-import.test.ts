import { test } from 'node:test'
import assert from 'node:assert/strict'
import {
  contentImport,
  pastedKind,
  remainingTime,
} from '../src/creative-canvas/assets/importContent.ts'
test('paste distinguishes a complete safe URL from prose, markup and unsafe schemes', () => {
  for (const url of ['https://example.com/a?b=1', ' http://example.com '])
    assert.equal(pastedKind(url), 'link')
  for (const text of [
    'hello',
    'javascript:alert(1)',
    'https://example.com hello',
    '<b>hello</b>',
  ])
    assert.equal(pastedKind(text), 'text')
  assert.throws(() => contentImport('link', 'javascript:alert(1)', '', ''))
})
test('creation freezes destination and preserves text without title deduplication', () => {
  const a = contentImport('text', '第一行\n第二行', '同名', 'g-1')
  assert.deepEqual(a.group_ids, ['g-1'])
  assert.deepEqual(a.content.payload, { body: '第一行\n第二行' })
  assert.deepEqual(contentImport('text', '第一行\n第二行', '同名', 'g-1'), a)
  assert.equal(
    contentImport('link', 'https://example.com', '', '').title,
    'example.com',
  )
})
test('trash countdown uses each asset deadline, rounds up and does not invent expired days', () => {
  assert.equal(remainingTime(null, 0), '不会自动清除')
  assert.equal(remainingTime(new Date(86400001).toISOString(), 0), '还剩 2 天')
  assert.equal(remainingTime(new Date(0).toISOString(), 0), '已到期，等待清除')
})
test('import bounds match content validation and reject credentials in URLs', () => {
  assert.throws(
    () => contentImport('text', 'x'.repeat(100001), '', ''),
    /100000/,
  )
  assert.doesNotThrow(() => contentImport('text', '字'.repeat(100000), '', ''))
  assert.throws(
    () =>
      contentImport('link', 'https://example.com/' + 'x'.repeat(4096), '', ''),
    /4096/,
  )
  assert.throws(
    () => contentImport('link', 'https://name:secret@example.com', '', ''),
    /账号/,
  )
})

test('link authority validation preserves @ in query and fragment', () => {
  for (const url of [
    'https://example.com?email=a@b.com',
    'https://example.com#@samson',
    'https://example.com/path/@samson',
  ])
    assert.deepEqual(contentImport('link', url, '', '').content.payload, {
      url,
    })
  assert.throws(
    () => contentImport('link', 'https://@example.com', '', ''),
    /账号/,
  )
})
