import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import {
  generationGrantSelectable,
  mediaSourceOptions,
  purposeAllowed,
  rightsDeclarationFor,
} from '../src/planning/mediaRights.ts'

const api = readFileSync(new URL('../src/planning/api.ts', import.meta.url), 'utf8')
const panel = readFileSync(new URL('../src/planning/panels/PlanningMediaPanel.tsx', import.meta.url), 'utf8')
const runPage = readFileSync(new URL('../src/planning/ShootPlanRunPage.tsx', import.meta.url), 'utf8')
const ingestion = readFileSync(new URL('../src/planning/ShootPlanIngestionPage.tsx', import.meta.url), 'utf8')
const workspace = readFileSync(new URL('../src/planning/ShootPlanWorkspacePage.tsx', import.meta.url), 'utf8')

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

test('media rights matrix mirror matches the server decision table', () => {
  assert.equal(purposeAllowed('photographer_owned', false, 'generation_reference'), true)
  assert.equal(purposeAllowed('licensed', true, 'generation_reference'), true)
  assert.equal(purposeAllowed('licensed', false, 'generation_reference'), false)
  assert.equal(purposeAllowed('anime_screenshot', false, 'generation_reference'), false)
  assert.equal(purposeAllowed('customer_supplied', false, 'shot_reference_display'), true)
  assert.equal(generationGrantSelectable('licensed'), true)
  assert.equal(generationGrantSelectable('photographer_owned'), false)
  assert.deepEqual(rightsDeclarationFor('anime_screenshot', false), {
    source_class: 'anime_screenshot', rights_basis: 'citation_or_display', license_generation_reference_granted: false,
  })
  assert.deepEqual(rightsDeclarationFor('licensed', true), {
    source_class: 'licensed', rights_basis: 'license_recorded', license_generation_reference_granted: true,
  })
  assert.equal(mediaSourceOptions.length, 8)
  assert.equal(new Set(mediaSourceOptions.map((option) => option.basis)).size, 4)
})

test('media panel picks purpose with matrix hints and binds assets to shots', () => {
  assert.match(panel, /mediaPurposeOptions\.map/)
  assert.match(panel, /（本来源禁止）/)
  assert.match(panel, /已获得生成参考授权/)
  assert.match(panel, /rightsDeclarationFor\(source, generationGrant\)/)
  assert.match(panel, /挂到该镜头/)
  assert.match(panel, /'shot_reference_display'/)
  assert.match(panel, /asset\.rights &&/)
  assert.match(panel, /asset\.active_bindings/)
  assert.match(panel, /purposeAllowed\(option\.value, true, 'generation_reference'\)/)
  assert.match(workspace, /shots=\{plan\.shots\.map/)
})

test('ingestion upload declares a selectable source instead of a hardcoded one', () => {
  assert.match(ingestion, /rightsDeclarationFor\(uploadSource, false\)/)
  assert.match(ingestion, /ingestion-upload-source/)
  assert.doesNotMatch(ingestion, /source_class: 'customer_supplied'/)
})
