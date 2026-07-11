import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

const stylesheet = readFileSync(new URL('../src/index.css', import.meta.url), 'utf8')

test('customer profile flex growth excludes the avatar', () => {
  assert.match(
    stylesheet,
    /\.profile-head\s*>\s*div:not\(\.avatar\)\s*\{[^}]*\bflex:\s*1\s+1\s+0\s*;/,
    'the flexible profile text container must explicitly exclude .avatar',
  )
  assert.doesNotMatch(
    stylesheet,
    /\.profile-head\s*>\s*div\s*\{[^}]*\bflex:/,
    'a broad direct-div flex rule also stretches the avatar',
  )
})
