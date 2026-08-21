import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import { isClaimReceiptWireFormat, isWebCryptoAvailable } from '../src/planning/share/crypto.ts'
import {
  fullViewEligibilityNote,
  isStaleRefreshCode,
  shareConflictMessage,
} from '../src/planning/share/conflictMessages.ts'

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

test('customers can self-revoke an active assignment with the one-time receipt', () => {
  const full = source('../src/planning/share/SharedFullSections.tsx')

  assert.equal(isClaimReceiptWireFormat('cr1.ABCdef1234567890-_ghiJKL'), true)
  assert.equal(isClaimReceiptWireFormat('  cr1.ABCdef1234567890-_ghiJKL  '), true)
  assert.equal(isClaimReceiptWireFormat('K7QF-92'), false)
  assert.equal(isClaimReceiptWireFormat('cr1.'), false)
  assert.equal(isClaimReceiptWireFormat('cr1.短'), false)
  assert.match(full, /selfRevokeSharedAssignment\(/)
  assert.match(full, /expected_assignment_revision: assignment\.revision/)
  assert.match(full, /claim_receipt: wire/)
  assert.match(full, /取消认领/)
  assert.match(full, /凭证不匹配/)
  assert.match(full, /联系摄影师帮你取消/)
})

test('submitted feedback stays visible to its author within the same visit', () => {
  const common = source('../src/planning/share/SharedCommon.tsx')
  const full = source('../src/planning/share/SharedFullSections.tsx')

  assert.match(common, /你已提交的意见/)
  assert.match(common, /setSentFeedback/)
  assert.match(full, /你已对这一镜提过意见/)
})

test('claim section explains photographer-side checks, no customer messaging, and receipt use', () => {
  const full = source('../src/planning/share/SharedFullSections.tsx')

  assert.match(full, /不会给你发消息催/)
  assert.match(full, /认领时会生成一个凭证码/)
})

test('share 409s split into stale-refresh, eligibility, and server-domain messages', () => {
  assert.equal(isStaleRefreshCode('plan_revision_conflict'), true)
  assert.equal(isStaleRefreshCode('share_stale'), true)
  assert.equal(isStaleRefreshCode('expiry_quote_stale'), true)
  assert.equal(isStaleRefreshCode('full_view_not_eligible'), false)
  assert.equal(isStaleRefreshCode('share_generation_exists'), false)

  assert.match(
    shareConflictMessage('full_view_not_eligible', '当前 CRM 关联不满足完整分享资格', '签发冲突：页面已刷新，请核对有效期后重试。'),
    /还不能签发完整档[\s\S]*已定档[\s\S]*方案概览[\s\S]*CRM 关联卡/,
  )
  assert.equal(
    shareConflictMessage('share_stale', '分享版本已变化，请刷新后重试', '轮换冲突：页面已刷新，请核对后重试。'),
    '轮换冲突：页面已刷新，请核对后重试。',
  )
  assert.equal(
    shareConflictMessage('share_generation_exists', '该视角已有有效分享链接，请轮换', '签发冲突：页面已刷新，请核对有效期后重试。'),
    '该视角已有有效分享链接，请轮换',
  )
  assert.equal(
    shareConflictMessage('weird_unknown_code', '', '签发冲突：页面已刷新，请核对有效期后重试。'),
    '签发冲突：页面已刷新，请核对有效期后重试。',
  )
})

test('share panel keeps eligibility errors distinct and explains full view eligibility upfront', () => {
  const panel = source('../src/planning/share/ShareCollaborationPanel.tsx')

  assert.match(panel, /shareConflictMessage\(cause\.code, cause\.message, message\)/)
  assert.match(panel, /view\.view_level === 'full' &&[\s\S]*fullViewEligibilityNote/)
  assert.match(fullViewEligibilityNote, /订单已定档/)
  assert.match(fullViewEligibilityNote, /不会自动降级成概览/)
})

test('anonymous moodboard figures render server-provided caption and usage note', () => {
  const shared = source('../src/planning/share/SharedCommon.tsx')
  const schema = source('../src/api/schema.d.ts')
  const css = source('../src/planning/share/share.css')

  assert.match(schema, /SharedMoodboardItemV1: \{[\s\S]*caption\?: string \| null;[\s\S]*usage_note\?: string;/)
  assert.match(shared, /\(item\.caption \|\| item\.usage_note\)/)
  assert.match(shared, /share-mood-caption/)
  assert.match(shared, /share-mood-usage/)
  assert.match(shared, /alt=\{item\.caption \|\| '分享参考图'\}/)
  assert.match(css, /\.share-mood figcaption \{ display: grid/)
})
