import type { components } from '../../api/schema'
import { request } from '../../api/transport.ts'
import { currentAccount } from '../../creative-canvas/editor/api.ts'

// The scene job endpoints as one page's renderer uses them. Calls are bound
// to the account that started the page: after a login change they fail
// without reaching the server, so a job claimed for one account is never
// reported under another.

export type SceneJob = components['schemas']['CreativeSceneJob']
export type JobKind = components['schemas']['CreativeSceneJobKind']
export type CompletePayload = components['schemas']['CreativeSceneJobCompletePayload']
export type Completion = components['schemas']['CreativeSceneJobCompletion']
export type JobErrorCode = NonNullable<CompletePayload['error']>['code']

/** The kinds a page runs. */
export const pageJobKinds: JobKind[] = ['bake_model', 'render_view', 'export_shots']
/** Renewal pace; the server's lease is 30 seconds. */
export const leaseEveryMs = 10_000

export type JobApi = {
  /** The oldest job this page can run, of one scene when `documentID` is given; null when none waits. */
  claim(input: { documentID?: string; kinds: JobKind[] }): Promise<SceneJob | null>
  lease(jobID: string, claimToken: string): Promise<void>
  complete(jobID: string, operationID: string, createdAt: string, payload: CompletePayload): Promise<Completion>
}

/** A random ID naming this tab to the server (the API's 16 to 64 URL-safe characters). */
export function newEditorSessionID(): string {
  return crypto.randomUUID().replaceAll('-', '')
}

const jobPath = (id: string, action: string) => `/creative/scene-jobs/${encodeURIComponent(id)}/${action}`

export function jobApi(account: string, editorSessionID: string): JobApi {
  const post = <T>(path: string, body: unknown, headers?: Record<string, string>): Promise<T> =>
    currentAccount() === account
      ? request<T>(path, { method: 'POST', headers, body: JSON.stringify(body) })
      : Promise.reject(new Error('账号已变化'))
  return {
    claim: async ({ documentID, kinds }) => {
      const out = await post<components['schemas']['CreativeSceneJobClaim'] | undefined>('/creative/scene-jobs/claim', {
        editor_session_id: editorSessionID,
        ...(documentID ? { document_id: documentID } : {}),
        kinds,
      })
      return out?.job ?? null
    },
    lease: async (jobID, claimToken) => {
      await post(jobPath(jobID, 'lease'), { claim_token: claimToken })
    },
    complete: (jobID, operationID, createdAt, payload) =>
      post<Completion>(
        jobPath(jobID, 'complete'),
        { operation_id: operationID, client_created_at: createdAt, payload },
        { 'Idempotency-Key': operationID },
      ),
  }
}
