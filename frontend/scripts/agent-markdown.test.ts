import { test } from 'node:test'
import assert from 'node:assert/strict'
import {
  closedCodeFence,
  markdownFeatures,
} from '../src/creative-canvas/editor/markdown.ts'

test('optional renderers follow syntax, including nested fences', () => {
  assert.deepEqual(markdownFeatures('普通文本 $5 和 $10'), {
    code: false,
    math: false,
    hasCodeBlock: false,
  })
  assert.equal(markdownFeatures('行内 $$x^2$$').math, true)
  assert.equal(markdownFeatures('```text\n$$not math$$\n```').math, false)
  assert.equal(
    markdownFeatures('```mermaid\ngraph LR\n A-->B\n```').code,
    false,
  )
  assert.equal(markdownFeatures('> ```python\n> print(1)\n> ```').code, true)
  assert.equal(
    markdownFeatures('- 项目\n\n  ~~~go\n  func main() {}\n  ~~~').code,
    true,
  )
  assert.equal(
    markdownFeatures('````text\n```mermaid\nA-->B\n```\n````').hasCodeBlock,
    true,
  )
})

test('diagram fence closure uses original syntax, never run completion', () => {
  for (const raw of [
    '```mermaid\nA-->B',
    '```mermaid\nA-->B\n~~~',
    '````mermaid\nA-->B\n```',
    '```mermaid\nA-->B\n```suffix',
  ])
    assert.equal(closedCodeFence(raw), false, raw)
  for (const raw of [
    '```mermaid\nA-->B\n```',
    '~~~mermaid\nA-->B\n~~~~',
    '```mermaid\r\nA-->B\r\n```\r\n',
    '> ```mermaid\n> A-->B\n> ```',
    '```mermaid\nA-->B\n  ```',
  ])
    assert.equal(closedCodeFence(raw), true, raw)
})

test('closing markers inside code are not container fences', () => {
  assert.equal(closedCodeFence('```mermaid\nA-->B\n> ```'), false)
  assert.equal(closedCodeFence('```mermaid\nA-->B\n    ```'), false)
  const source = '- ```mermaid\n  A-->B\n  ```\n- ```mermaid\n  A-->B'
  assert.equal(closedCodeFence(source, source.indexOf('```')), true)
  assert.equal(closedCodeFence(source, source.lastIndexOf('```mermaid')), false)
})
