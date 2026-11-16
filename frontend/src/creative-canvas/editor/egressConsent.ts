// 账号外发同意的读写（egress-consent-account-level）。弹窗和「外发说明」面板
// 在 EgressNotice.tsx，这里只管服务端状态、本机缓存和结果未知的写入。
import type { components } from '../../api/schema'
import { ApiError } from '../../api/transport.ts'
import * as api from './api.ts'
import {
  agentStep,
  definitelyRejected,
  egressPolicyVersion,
  stepBody,
  type AgentStep,
} from './agentState.ts'

export type EgressConsentState =
  components['schemas']['CreativeEgressConsentState']
type GrantPayload = components['schemas']['CreativeEgressConsentGrantPayload']
type RevokePayload =
  components['schemas']['CreativeEgressConsentRevokePayload']

// 每个账号一份，组件之间共用：画布助手和首页都不重复读。
const cache = new Map<string, EgressConsentState>()
// 结果未知（断网、超时、5xx）的写入按意图留着原信封：同一个意图再点一次时
// 重放原操作，服务端回放原回执，不会另起一次同意去盖掉别处刚做的撤回。
const unsettled = new Map<string, AgentStep>()

function readConsent(): Promise<EgressConsentState> {
  return api.read<EgressConsentState>('/egress-consent')
}
async function writeConsent(
  account: string,
  path: string,
  payload: GrantPayload | RevokePayload,
): Promise<EgressConsentState> {
  const key = `${account}\n${path}\n${JSON.stringify(payload)}`
  const retried = unsettled.get(key)
  const step = retried ?? agentStep(path, payload)
  unsettled.set(key, step)
  let receipt: EgressConsentState
  try {
    receipt = await api.send<EgressConsentState>(
      account,
      path,
      stepBody(step),
      step.operation,
    )
  } catch (e) {
    if (definitelyRejected(e)) unsettled.delete(key)
    throw e
  }
  unsettled.delete(key)
  // 重放回来的回执是当时的结果，之后状态可能又变了：以现在读到的为准。
  const state = retried ? await readConsent() : receipt
  cache.set(account, state)
  return state
}
// revision 是摄影师看到的那份状态的版本：别处在这之后撤回或改过，服务端拒绝
// （creative_revision_conflict），不会凭一份过时的界面把同意恢复回来。
export function agreeToNotice(
  account: string,
  library: boolean,
  revision: string,
): Promise<EgressConsentState> {
  return writeConsent(account, '/egress-consent', {
    policy_version: egressPolicyVersion,
    library_access: library,
    expected_revision: revision,
  })
}
export function withdrawConsent(account: string): Promise<EgressConsentState> {
  return writeConsent(account, '/egress-consent/revoke', {})
}
export async function accountConsent(
  account: string,
): Promise<EgressConsentState> {
  const cached = cache.get(account)
  if (cached) return cached
  const state = await readConsent()
  cache.set(account, state)
  return state
}
// 发送被拒，说是需要同意或同意已撤回：本机记的状态过时了，下次发送重新问。
export function forgetConsent(account: string) {
  cache.delete(account)
}
export function isConsentRefusal(error: unknown): boolean {
  return (
    error instanceof ApiError &&
    (error.code === 'creative_egress_required' ||
      error.code === 'creative_egress_revoked')
  )
}
// 摄影师看到的同意状态在别处改过了（另一台设备撤回、改了素材库选择）。
export function isStaleConsent(error: unknown): boolean {
  return error instanceof ApiError && error.code === 'creative_revision_conflict'
}
export function isPolicyChanged(error: unknown): boolean {
  return (
    error instanceof ApiError && error.code === 'creative_egress_policy_changed'
  )
}
