import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

const api = readFileSync(new URL('../src/planning/api.ts', import.meta.url), 'utf8')
const panel = readFileSync(new URL('../src/planning/panels/PlanningMediaPanel.tsx', import.meta.url), 'utf8')
const runPage = readFileSync(new URL('../src/planning/ShootPlanRunPage.tsx', import.meta.url), 'utf8')

test('planning media API keeps multipart upload separate from JSON request helper', () => {
  assert.match(api, /FormData/)
  assert.match(api, /planningMediaRequest/)
  assert.doesNotMatch(api, /permitted_uses/)
})

test('planning media UI exposes staged, active, corrupt and retryable states', () => {
  assert.match(panel, /staged/)
  assert.match(panel, /corrupt/)
  assert.match(panel, /挂到整案/)
  assert.match(panel, /role="status"/)
})

test('planning media upload accepts only declared supported image types', () => {
  assert.match(panel, /image\/jpeg,image\/png,image\/webp/)
  assert.match(api, /declared_media_type/)
})

test('run mode renders read-only reference sheet without blocking capture', () => {
  assert.match(runPage, /asset_access_refs/)
  assert.match(runPage, /本镜参考/)
  assert.match(runPage, /不影响现场记录/)
  assert.match(runPage, /fetchPlanAssetDisplay/)
})
