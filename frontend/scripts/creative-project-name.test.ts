import test from 'node:test'
import assert from 'node:assert/strict'
import {
  decideProjectName,
  projectNameLimit,
} from '../src/creative-canvas/home/projectName.ts'

test('blank or unchanged names keep the original', () => {
  assert.deepEqual(decideProjectName('', '九月'), { kind: 'keep' })
  assert.deepEqual(decideProjectName(' \n\t ', '九月'), { kind: 'keep' })
  assert.deepEqual(decideProjectName('  九月  ', '九月'), { kind: 'keep' })
})

test('a new name is submitted trimmed', () => {
  assert.deepEqual(decideProjectName('  雪山婚纱 · 二稿 ', '九月'), {
    kind: 'rename',
    name: '雪山婚纱 · 二稿',
  })
})

test('the limit counts code points like the server', () => {
  const emoji = '📷'.repeat(projectNameLimit)
  assert.equal(emoji.length, projectNameLimit * 2)
  assert.deepEqual(decideProjectName(emoji, '九月'), {
    kind: 'rename',
    name: emoji,
  })
  assert.deepEqual(decideProjectName(`${emoji}a`, '九月'), {
    kind: 'too-long',
  })
})
