import type { components } from '../../api/schema'
import { ApiError, request, sharedRequest } from '../../api/transport'

export type SharedPlanProposal = components['schemas']['SharedPlanProposalV1']
export type SharedPlanFull = components['schemas']['SharedPlanFullV1']
export type SharedPlanProjection = SharedPlanProposal | SharedPlanFull
export type ShareManagementProjection = components['schemas']['ShareManagementProjectionV1']
export type ShareViewProjection = components['schemas']['ShareViewProjectionV1']
export type ShareIssueInput = components['schemas']['ShareIssueInputV1']
export type ShareIssueResult = components['schemas']['ShareIssueResultV1']
export type ShareRotateInput = components['schemas']['ShareRotateInputV1']
export type ShareRevokeInput = components['schemas']['ShareRevokeInputV1']
export type ShareRevokeResult = components['schemas']['ShareRevokeResultV1']
export type FeedbackManagementPage = components['schemas']['FeedbackManagementPageV1']
export type FeedbackManagementItem = components['schemas']['FeedbackManagementItemV1']
export type FeedbackDispositionInput = components['schemas']['FeedbackDispositionInputV1']
export type FeedbackDispositionResult = components['schemas']['FeedbackDispositionResultV1']
export type AssignmentManagementPage = components['schemas']['AssignmentManagementPageV1']
export type AssignmentManagementItem = components['schemas']['AssignmentManagementItemV1']
export type AssignmentOfferCreateInput = components['schemas']['AssignmentOfferCreateInputV1']
export type AssignmentOfferCloseInput = components['schemas']['AssignmentOfferCloseInputV1']
export type OfferMutationResult = components['schemas']['OfferMutationResultV1']
export type AssignmentPhotographerRevokeInput = components['schemas']['AssignmentPhotographerRevokeInputV1']
export type AssignmentMutationResult = components['schemas']['AssignmentMutationResultV1']
export type SharedPlanFeedbackCreateInput = components['schemas']['SharedPlanFeedbackCreateInputV1']
export type SharedShotFeedbackCreateInput = components['schemas']['SharedShotFeedbackCreateInputV1']
export type FeedbackCreateResult = components['schemas']['FeedbackCreateResultV1']
export type SharedAssignmentClaimInput = components['schemas']['SharedAssignmentClaimInputV1']
export type AssignmentClaimResult = components['schemas']['AssignmentClaimResultV1']
export type SharedAssignmentSelfRevokeInput = components['schemas']['SharedAssignmentSelfRevokeInputV1']
export type SharedMoodboardItem = components['schemas']['SharedMoodboardItemV1']
export type ExpirySource = components['schemas']['ExpirySourceV1']
export type ExpiryPolicyProjection = components['schemas']['ExpiryPolicyProjectionV1']

export { ApiError }

export function newShareMutationKey(scope: string): string {
  const random = typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function'
    ? crypto.randomUUID()
    : `${Date.now()}-${Math.random().toString(16).slice(2)}`
  return `share-${scope}-${random}`
}

export function getSharedPlan(token: string): Promise<SharedPlanProjection> {
  return sharedRequest<SharedPlanProjection>(`/shared/plans/${encodeURIComponent(token)}`)
}

export function createSharedPlanFeedback(
  token: string,
  body: SharedPlanFeedbackCreateInput,
  idempotencyKey: string,
): Promise<FeedbackCreateResult> {
  return sharedRequest<FeedbackCreateResult>(`/shared/plans/${encodeURIComponent(token)}/feedback`, {
    method: 'POST',
    headers: { 'Idempotency-Key': idempotencyKey },
    body: JSON.stringify(body),
  })
}

export function createSharedShotFeedback(
  token: string,
  shotRef: string,
  body: SharedShotFeedbackCreateInput,
  idempotencyKey: string,
): Promise<FeedbackCreateResult> {
  return sharedRequest<FeedbackCreateResult>(
    `/shared/plans/${encodeURIComponent(token)}/shots/${encodeURIComponent(shotRef)}/feedback`,
    {
      method: 'POST',
      headers: { 'Idempotency-Key': idempotencyKey },
      body: JSON.stringify(body),
    },
  )
}

export function claimSharedAssignment(
  token: string,
  body: SharedAssignmentClaimInput,
  idempotencyKey: string,
): Promise<AssignmentClaimResult> {
  return sharedRequest<AssignmentClaimResult>(`/shared/plans/${encodeURIComponent(token)}/assignments`, {
    method: 'POST',
    headers: { 'Idempotency-Key': idempotencyKey },
    body: JSON.stringify(body),
  })
}

export function selfRevokeSharedAssignment(
  token: string,
  assignmentRef: string,
  body: SharedAssignmentSelfRevokeInput,
  idempotencyKey: string,
): Promise<AssignmentMutationResult> {
  return sharedRequest<AssignmentMutationResult>(
    `/shared/plans/${encodeURIComponent(token)}/assignments/${encodeURIComponent(assignmentRef)}`,
    {
      method: 'DELETE',
      headers: { 'Idempotency-Key': idempotencyKey },
      body: JSON.stringify(body),
    },
  )
}

export function sharedMoodboardContentURL(token: string, ref: string, checksum: string): string {
  return `/api/v1/shared/plans/${encodeURIComponent(token)}/assets/${encodeURIComponent(ref)}/content?v=${encodeURIComponent(checksum)}`
}

export async function fetchSharedMoodboardBlob(token: string, ref: string, checksum: string): Promise<Blob> {
  const response = await fetch(sharedMoodboardContentURL(token, ref, checksum), {
    credentials: 'omit',
    cache: 'no-store',
    referrerPolicy: 'no-referrer',
  })
  if (!response.ok) {
    throw new ApiError(response.status, 'asset_unavailable', '参考图暂不可用')
  }
  return response.blob()
}

export function getShootPlanShares(planID: string, params: {
  offerCursor?: string
  offerLimit?: number
} = {}): Promise<ShareManagementProjection> {
  const search = new URLSearchParams()
  if (params.offerCursor) search.set('offerCursor', params.offerCursor)
  if (params.offerLimit) search.set('offerLimit', String(params.offerLimit))
  const query = search.toString()
  return request<ShareManagementProjection>(`/shoot-plans/${encodeURIComponent(planID)}/shares${query ? `?${query}` : ''}`)
}

export function issueShootPlanShare(
  planID: string,
  body: ShareIssueInput,
  idempotencyKey: string,
): Promise<ShareIssueResult> {
  return request<ShareIssueResult>(`/shoot-plans/${encodeURIComponent(planID)}/shares`, {
    method: 'POST',
    headers: { 'Idempotency-Key': idempotencyKey },
    body: JSON.stringify(body),
  })
}

export function rotateShootPlanShare(
  planID: string,
  shareID: string,
  body: ShareRotateInput,
  idempotencyKey: string,
): Promise<ShareIssueResult> {
  return request<ShareIssueResult>(
    `/shoot-plans/${encodeURIComponent(planID)}/shares/${encodeURIComponent(shareID)}/rotate`,
    {
      method: 'POST',
      headers: { 'Idempotency-Key': idempotencyKey },
      body: JSON.stringify(body),
    },
  )
}

export function revokeShootPlanShare(
  planID: string,
  shareID: string,
  body: ShareRevokeInput,
  idempotencyKey: string,
): Promise<ShareRevokeResult> {
  return request<ShareRevokeResult>(
    `/shoot-plans/${encodeURIComponent(planID)}/shares/${encodeURIComponent(shareID)}`,
    {
      method: 'DELETE',
      headers: { 'Idempotency-Key': idempotencyKey },
      body: JSON.stringify(body),
    },
  )
}

export function getShootPlanFeedback(planID: string, params: {
  cursor?: string
  limit?: number
} = {}): Promise<FeedbackManagementPage> {
  const search = new URLSearchParams()
  if (params.cursor) search.set('cursor', params.cursor)
  if (params.limit) search.set('limit', String(params.limit))
  const query = search.toString()
  return request<FeedbackManagementPage>(`/shoot-plans/${encodeURIComponent(planID)}/feedback${query ? `?${query}` : ''}`)
}

export function setShootPlanFeedbackDisposition(
  planID: string,
  feedbackID: string,
  body: FeedbackDispositionInput,
  idempotencyKey: string,
): Promise<FeedbackDispositionResult> {
  return request<FeedbackDispositionResult>(
    `/shoot-plans/${encodeURIComponent(planID)}/feedback/${encodeURIComponent(feedbackID)}/disposition`,
    {
      method: 'POST',
      headers: { 'Idempotency-Key': idempotencyKey },
      body: JSON.stringify(body),
    },
  )
}

export function getShootPlanAssignments(planID: string, params: {
  cursor?: string
  limit?: number
} = {}): Promise<AssignmentManagementPage> {
  const search = new URLSearchParams()
  if (params.cursor) search.set('cursor', params.cursor)
  if (params.limit) search.set('limit', String(params.limit))
  const query = search.toString()
  return request<AssignmentManagementPage>(`/shoot-plans/${encodeURIComponent(planID)}/assignments${query ? `?${query}` : ''}`)
}

export function revokeShootPlanAssignment(
  planID: string,
  assignmentID: string,
  body: AssignmentPhotographerRevokeInput,
  idempotencyKey: string,
): Promise<AssignmentMutationResult> {
  return request<AssignmentMutationResult>(
    `/shoot-plans/${encodeURIComponent(planID)}/assignments/${encodeURIComponent(assignmentID)}`,
    {
      method: 'DELETE',
      headers: { 'Idempotency-Key': idempotencyKey },
      body: JSON.stringify(body),
    },
  )
}

export function createShootPlanAssignmentOffer(
  planID: string,
  body: AssignmentOfferCreateInput,
  idempotencyKey: string,
): Promise<OfferMutationResult> {
  return request<OfferMutationResult>(`/shoot-plans/${encodeURIComponent(planID)}/assignment-offers`, {
    method: 'POST',
    headers: { 'Idempotency-Key': idempotencyKey },
    body: JSON.stringify(body),
  })
}

export function closeShootPlanAssignmentOffer(
  planID: string,
  offerID: string,
  body: AssignmentOfferCloseInput,
  idempotencyKey: string,
): Promise<OfferMutationResult> {
  return request<OfferMutationResult>(
    `/shoot-plans/${encodeURIComponent(planID)}/assignment-offers/${encodeURIComponent(offerID)}`,
    {
      method: 'DELETE',
      headers: { 'Idempotency-Key': idempotencyKey },
      body: JSON.stringify(body),
    },
  )
}
