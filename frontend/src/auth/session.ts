import type { components } from '../api/schema.ts'

export type AuthStatus = 'anonymous' | 'restoring' | 'authenticated'
export type Me = components['schemas']['Account']
export type AccessTokenResponse = components['schemas']['AccessTokenResponse']

type AuthBase = Readonly<{
  generation: number
  sessionEpoch: number
}>

export type AuthSnapshot =
  | (AuthBase & Readonly<{ status: 'anonymous' | 'restoring' }>)
  | (AuthBase & Readonly<{
      status: 'authenticated'
      account: Me
      accessToken: string
      expiresAt: number
    }>)

type AuthSnapshotUpdate =
  | Readonly<{ status: 'anonymous' | 'restoring'; sessionEpoch: number }>
  | Readonly<{
      status: 'authenticated'
      account: Me
      accessToken: string
      expiresAt: number
      sessionEpoch: number
    }>

type RefreshFlight = Readonly<{
  epoch: number
  promise: Promise<AccessTokenResponse | null>
}>

type RestoreFlight = Readonly<{
  epoch: number
  promise: Promise<void>
}>

let accessToken: string | null = null
let sessionEpoch = 0
let snapshot: AuthSnapshot = Object.freeze({
  status: 'restoring',
  generation: 0,
  sessionEpoch,
})
let refreshFlight: RefreshFlight | null = null
let restoreFlight: RestoreFlight | null = null
const cookieMutationFlights = new Set<Promise<unknown>>()
const listeners = new Set<() => void>()

export function getAuthSnapshot(): AuthSnapshot {
  return snapshot
}

export function subscribeAuth(listener: () => void): () => void {
  listeners.add(listener)
  return () => listeners.delete(listener)
}

export function getAccessToken(): string | null {
  return accessToken
}

export function getAuthGeneration(): number {
  return snapshot.generation
}

export function setAuthenticated(grant: AccessTokenResponse, account: Me): void {
  const epoch = advanceSessionEpoch()
  commitAuthenticated(epoch, grant, account)
}

export function setAnonymous(): void {
  const epoch = advanceSessionEpoch()
  accessToken = null
  publish({ status: 'anonymous', sessionEpoch: epoch })
}

export async function establishSession(
  action: () => Promise<AccessTokenResponse>,
): Promise<boolean> {
  const priorMutations = [...cookieMutationFlights]
  const epoch = advanceSessionEpoch()
  accessToken = null
  publish({ status: 'anonymous', sessionEpoch: epoch })

  const flight = (async () => {
    await Promise.allSettled(priorMutations)
    if (!isCurrentEpoch(epoch)) return false

    const rawGrant = await action()
    const grant = parseAccessGrant(rawGrant)
    if (!grant) throw new Error('invalid access token response')
    if (!isCurrentEpoch(epoch)) return false

    const response = await fetchWithAccess('/api/v1/me', {}, grant.access_token)
    if (!response.ok) throw new Error('current account request failed')
    const account = parseAccount(await response.json())
    if (!account) throw new Error('invalid current account response')
    if (!isCurrentEpoch(epoch)) return false

    commitAuthenticated(epoch, grant, account)
    return true
  })()
  cookieMutationFlights.add(flight)
  try {
    return await flight
  } catch (error) {
    setAnonymousIfCurrent(epoch)
    throw error
  } finally {
    cookieMutationFlights.delete(flight)
  }
}

export function restoreSession(): Promise<void> {
  if (restoreFlight?.epoch === sessionEpoch) return restoreFlight.promise

  const epoch = advanceSessionEpoch()
  accessToken = null
  publish({ status: 'restoring', sessionEpoch: epoch })
  const promise = (async () => {
    const grant = await refreshAccessToken(epoch)
    if (!grant || !isCurrentEpoch(epoch)) return
    try {
      const response = await fetchWithAccess('/api/v1/me', {}, grant.access_token)
      if (!response.ok) {
        setAnonymousIfCurrent(epoch)
        return
      }
      const account = parseAccount(await response.json())
      if (!account) {
        setAnonymousIfCurrent(epoch)
        return
      }
      if (isCurrentEpoch(epoch)) commitAuthenticated(epoch, grant, account)
    } catch {
      setAnonymousIfCurrent(epoch)
    }
  })()
  const flight = { epoch, promise }
  restoreFlight = flight
  void promise.finally(() => {
    if (restoreFlight === flight) restoreFlight = null
  })
  return promise
}

export async function authorizedFetch(
  input: RequestInfo | URL,
  init: RequestInit = {},
): Promise<Response> {
  const requestToken = accessToken
  const requestEpoch = sessionEpoch
  const first = await fetchWithAccess(input, init, requestToken)
  if (first.status !== 401 || !isCurrentEpoch(requestEpoch)) return first

  if (accessToken === requestToken) {
    if (!(await refreshAccessToken(requestEpoch))) return first
  } else if (!accessToken) {
    return first
  }
  if (!isCurrentEpoch(requestEpoch) || !accessToken) return first

  const replay = await fetchWithAccess(input, init, accessToken)
  if (replay.status === 401) setAnonymousIfCurrent(requestEpoch)
  return replay
}

export function authorizedFetchOnce(
	input: RequestInfo | URL,
	init: RequestInit = {},
): Promise<Response> {
	return fetchWithAccess(input, init)
}

export async function logoutSession(): Promise<void> {
  const priorMutations = [...cookieMutationFlights]
  const epoch = advanceSessionEpoch()
  accessToken = null
  publish({ status: 'anonymous', sessionEpoch: epoch })

  const flight = (async () => {
    await Promise.allSettled(priorMutations)
    if (!isCurrentEpoch(epoch)) return
    await fetch('/api/v1/auth/logout', {
      method: 'POST',
      credentials: 'same-origin',
    })
  })()
  cookieMutationFlights.add(flight)
  try {
    await flight
  } finally {
    cookieMutationFlights.delete(flight)
  }
}

async function refreshAccessToken(epoch: number): Promise<AccessTokenResponse | null> {
  if (!isCurrentEpoch(epoch)) return null
  if (refreshFlight?.epoch === epoch) return refreshFlight.promise

  const promise = (async () => {
    try {
      const response = await fetch('/api/v1/auth/refresh', {
        method: 'POST',
        credentials: 'same-origin',
      })
      if (!response.ok) {
        setAnonymousIfCurrent(epoch)
        return null
      }
      const grant = parseAccessGrant(await response.json())
      if (!grant) {
        setAnonymousIfCurrent(epoch)
        return null
      }
      if (!isCurrentEpoch(epoch)) return null

      accessToken = grant.access_token
      if (snapshot.status === 'authenticated') {
        commitAuthenticated(epoch, grant, snapshot.account)
      }
      return grant
    } catch {
      setAnonymousIfCurrent(epoch)
      return null
    }
  })()
  const flight = { epoch, promise }
  refreshFlight = flight
  cookieMutationFlights.add(promise)
  void promise.finally(() => {
    cookieMutationFlights.delete(promise)
    if (refreshFlight === flight) refreshFlight = null
  })
  return promise
}

function fetchWithAccess(
  input: RequestInfo | URL,
  init: RequestInit = {},
  token: string | null = accessToken,
): Promise<Response> {
  const headers = new Headers(init.headers)
  if (token) headers.set('Authorization', `Bearer ${token}`)
  return fetch(input, {
    ...init,
    credentials: init.credentials ?? 'same-origin',
    headers,
  })
}

function commitAuthenticated(epoch: number, grant: AccessTokenResponse, account: Me): void {
  if (!isCurrentEpoch(epoch)) return
  accessToken = grant.access_token
  publish({
    status: 'authenticated',
    account,
    accessToken: grant.access_token,
    expiresAt: Date.now() + grant.expires_in * 1000,
    sessionEpoch: epoch,
  })
}

function setAnonymousIfCurrent(epoch: number): void {
  if (!isCurrentEpoch(epoch)) return
  setAnonymous()
}

function advanceSessionEpoch(): number {
  sessionEpoch += 1
  return sessionEpoch
}

function isCurrentEpoch(epoch: number): boolean {
  return sessionEpoch === epoch
}

function publish(next: AuthSnapshotUpdate): void {
  snapshot = Object.freeze({ ...next, generation: snapshot.generation + 1 }) as AuthSnapshot
  listeners.forEach((listener) => listener())
}

function parseAccessGrant(value: unknown): AccessTokenResponse | null {
  if (!value || typeof value !== 'object') return null
  const body = value as Partial<AccessTokenResponse>
  if (
    typeof body.access_token !== 'string' ||
    body.access_token === '' ||
    body.token_type !== 'Bearer' ||
    body.expires_in !== 600
  ) return null
  return body as AccessTokenResponse
}

function parseAccount(value: unknown): Me | null {
  if (!value || typeof value !== 'object') return null
  const account = value as Partial<Me>
  if (
    typeof account.id !== 'string' || account.id === '' ||
    typeof account.email !== 'string' || account.email === '' ||
    typeof account.created_at !== 'string' || account.created_at === '' ||
    typeof account.timezone !== 'string' || account.timezone === ''
  ) return null
  return account as Me
}
