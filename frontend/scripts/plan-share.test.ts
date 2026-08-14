import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import { isWebCryptoAvailable } from '../src/planning/share/crypto.ts'

function source(path: string): string {
  return readFileSync(new URL(path, import.meta.url), 'utf8')
}

const shareSources = [
  '../src/planning/share/api.ts',
  '../src/planning/share/crypto.ts',
  '../src/planning/share/SharedPlanPage.tsx',
  '../src/planning/share/SharedProposalView.tsx',
  '../src/planning/share/SharedFullView.tsx',
  '../src/planning/share/SharedFullSections.tsx',
  '../src/planning/share/SharedCommon.tsx',
  '../src/planning/share/ShareCollaborationPanel.tsx',
  '../src/planning/share/OneTimeSecretDialog.tsx',
].map(source).join('\n')

test('anonymous share route skips session restore and never mounts AnonymousEntry or AppShell', () => {
  const app = source('../src/App.tsx')

  assert.match(app, /isPublicShareRoute = location\.pathname\.startsWith\('\/shared\/plans\/'\)/)
  assert.match(app, /skipSessionRestore = isActionTokenRoute \|\| isPublicShareRoute/)
  const shareRoute = app.match(/<Route path="\/shared\/plans\/:token" element=\{<SharedPlanPage \/>\} \/>/)?.[0] ?? ''
  assert.equal(shareRoute, '<Route path="/shared/plans/:token" element={<SharedPlanPage />} />')
  assert.doesNotMatch(shareRoute, /AnonymousEntry|AppShell|RequireAuth/)
})

test('proposal view source never renders shots or assignment structures', () => {
  const proposal = source('../src/planning/share/SharedProposalView.tsx')

  assert.doesNotMatch(proposal, /\bshots\b|\bassignment\b|createSharedShotFeedback|claimSharedAssignment|FullShotsSection|FullAssignmentsSection/)
  assert.match(proposal, /锁定说明|整案反馈|方案概览/)
  assert.match(source('../src/planning/share/SharedPlanPage.tsx'), /view_level === 'proposal'[\s\S]*SharedProposalView/)
  assert.match(source('../src/planning/share/SharedPlanPage.tsx'), /SharedFullView/)
})

test('token stays in route component memory and never touches Web Storage or console', () => {
  assert.doesNotMatch(shareSources, /localStorage|sessionStorage|indexedDB/)
  assert.doesNotMatch(shareSources, /console\.(?:log|info|debug|warn|error)/)
  assert.match(source('../src/planning/share/SharedPlanPage.tsx'), /useParams\(\)/)
})

test('sharedRequest and moodboard content fetch omit credentials', () => {
  const transport = source('../src/api/transport.ts')
  const api = source('../src/planning/share/api.ts')

  assert.match(transport, /export async function sharedRequest/)
  assert.match(transport, /credentials: 'omit'/)
  assert.match(transport, /cache: 'no-store'/)
  assert.match(transport, /referrerPolicy: 'no-referrer'/)
  assert.match(api, /sharedRequest</)
  assert.match(api, /credentials: 'omit'/)
  assert.match(api, /referrerPolicy: 'no-referrer'/)
  assert.doesNotMatch(source('../src/api/client.ts'), /shared\/plans|ShareIssue|getShootPlanShares/)
})

test('WebCrypto unavailable disables claim and share secret actions without weak fallback', () => {
  assert.equal(isWebCryptoAvailable(), typeof globalThis.crypto?.subtle?.digest === 'function')
  const full = source('../src/planning/share/SharedFullSections.tsx')
  const collab = source('../src/planning/share/ShareCollaborationPanel.tsx')
  const crypto = source('../src/planning/share/crypto.ts')

  assert.match(full, /isWebCryptoAvailable\(\)/)
  assert.match(full, /认领已禁用/)
  assert.match(full, /disabled=\{!cryptoOK\}|disabled=\{busy \|\| disabled\}/)
  assert.match(collab, /签发与轮换已禁用/)
  assert.match(collab, /readOnly=\{readOnly \|\| !cryptoOK\}/)
  assert.doesNotMatch(crypto, /Math\.random\(\)|weak|fallback/)
  assert.match(crypto, /cr1\.|sp1\./)
})

test('formal UI does not copy prototype “full replaces proposal / revoke invalidates all” copy', () => {
  const collab = source('../src/planning/share/ShareCollaborationPanel.tsx')

  assert.doesNotMatch(collab, /已被完整档取代|撤销full会让任何分享页失效|短码/)
  assert.match(collab, /另一档分享链接不受影响|另一档不受影响/)
  assert.match(collab, /方案概览档/)
  assert.match(collab, /完整档/)
})

test('share adapters only consume generated schema types and never accept account_id', () => {
  const api = source('../src/planning/share/api.ts')

  assert.match(api, /import type \{ components \} from '\.\.\/\.\.\/api\/schema'/)
  assert.doesNotMatch(api, /interface\s+(?:Share|Shared|Feedback|Assignment)[A-Za-z]*Input/)
  assert.doesNotMatch(api, /account_id/)
  assert.doesNotMatch(shareSources, /用户/)
})

test('workspace mounts an independent share collaboration tab', () => {
  const workspace = source('../src/planning/ShootPlanWorkspacePage.tsx')

  assert.match(workspace, /分享协作/)
  assert.match(workspace, /ShareCollaborationPanel/)
  assert.match(workspace, /tab === 'share'/)
})

test('Makefile and package.json expose mandatory plan-share runners', () => {
  const pkg = source('../package.json')
  const makefile = source('../../Makefile')

  assert.match(pkg, /"test:plan-share":/)
  assert.match(makefile, /npm run test:plan-share/)
  assert.match(makefile, /planning-prototype-v2\.test\.mjs/)
})
