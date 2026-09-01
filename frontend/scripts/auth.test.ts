import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'

import {
  establishSession,
  getAccessToken,
  getAuthSnapshot,
  logoutSession,
  restoreSession,
  setAnonymous,
  setAuthenticated,
} from '../src/auth/session.ts'
import { consumeActionToken } from '../src/auth/actionToken.ts'
import {
  ApiError,
  fetchAvatarBlob,
  fetchDataExport,
  listCustomers,
  putCustomerAvatar,
} from '../src/api/client.ts'

let webStorageCalls = 0
const forbiddenStorage = {
  getItem() { webStorageCalls += 1; throw new Error('auth must not read Web Storage') },
  setItem() { webStorageCalls += 1; throw new Error('auth must not write Web Storage') },
  removeItem() { webStorageCalls += 1; throw new Error('auth must not mutate Web Storage') },
  clear() { webStorageCalls += 1; throw new Error('auth must not clear Web Storage') },
  key() { return null },
  length: 0,
} satisfies Storage
Object.defineProperty(globalThis, 'localStorage', { configurable: true, value: forbiddenStorage })
Object.defineProperty(globalThis, 'sessionStorage', { configurable: true, value: forbiddenStorage })
Object.defineProperty(globalThis, 'indexedDB', {
  configurable: true,
  value: { open() { webStorageCalls += 1; throw new Error('auth must not use IndexedDB') } },
})

const access = (token: string) => ({ access_token: token, token_type: 'Bearer', expires_in: 600 })
const account = {
  id: 'account-a', email: 'masked@example.invalid', created_at: '2026-07-31T00:00:00Z', timezone: 'Asia/Shanghai',
}

test('startup restore moves restoring to authenticated through refresh then /me without storage', async () => {
  const calls: string[] = []
  globalThis.fetch = async (input, init) => {
    const url = String(input)
    calls.push(url)
    if (url.endsWith('/auth/refresh')) return Response.json(access('restored-access'))
    assert.equal(new Headers(init?.headers).get('Authorization'), 'Bearer restored-access')
    return Response.json(account)
  }

  assert.equal(getAuthSnapshot().status, 'restoring')
  await restoreSession()

  const restored = getAuthSnapshot()
  assert.equal(restored.status, 'authenticated')
  assert.deepEqual(restored.status === 'authenticated' ? restored.account : null, account)
  assert.equal(restored.status === 'authenticated' ? restored.accessToken : null, 'restored-access')
  assert.equal(restored.status === 'authenticated' && restored.expiresAt > Date.now(), true)
  assert.equal(restored.sessionEpoch > 0, true)
  assert.equal(getAccessToken(), 'restored-access')
  assert.deepEqual(calls, ['/api/v1/auth/refresh', '/api/v1/me'])
  assert.equal(webStorageCalls, 0)
})

test('failed startup refresh becomes anonymous and keeps access out of storage', async () => {
  setAnonymous()
  globalThis.fetch = async () => Response.json(
    { error: { code: 'unauthorized', message: '未认证' } },
    { status: 401 },
  )

  await restoreSession()

  assert.equal(getAuthSnapshot().status, 'anonymous')
  assert.equal(getAccessToken(), null)
  assert.equal(webStorageCalls, 0)
})

test('JSON, multipart, avatar Blob, and export share one refresh flight and replay once', async () => {
  setAuthenticated(access('expired-access'), account)
  const routeCalls = new Map<string, number>()
  let refreshCalls = 0
  globalThis.fetch = async (input, init) => {
    const url = String(input)
    if (url.endsWith('/auth/refresh')) {
      refreshCalls += 1
      await new Promise((resolve) => setTimeout(resolve, 5))
      return Response.json(access('fresh-access'))
    }
    const key = `${init?.method ?? 'GET'} ${url}`
    routeCalls.set(key, (routeCalls.get(key) ?? 0) + 1)
    const authorization = new Headers(init?.headers).get('Authorization')
    if (authorization === 'Bearer expired-access') {
      return Response.json({ error: { code: 'unauthorized', message: '未认证' } }, { status: 401 })
    }
    assert.equal(authorization, 'Bearer fresh-access')
    if (url.endsWith('/export')) {
      return new Response(new Blob(['{}'], { type: 'application/json' }), {
        headers: {
          'Content-Type': 'application/json',
          'Content-Disposition': 'attachment; filename="photographer-crm-export-20260731T000000Z.json"',
        },
      })
    }
    if (url.includes('/avatar/content')) {
      return new Response(new Blob(['avatar'], { type: 'image/png' }))
    }
    if (init?.method === 'PUT') return Response.json({ id: 'customer-a' })
    return Response.json({ items: [], total: 0, page: 1, page_size: 20 })
  }

  await Promise.all([
    listCustomers(),
    putCustomerAvatar('customer-a', new File(['avatar'], 'avatar.png'), 'ar-1'),
    fetchAvatarBlob('/api/v1/customers/customer-a/avatar/content?v=sha256-a', new AbortController().signal),
    fetchDataExport(),
  ])

  assert.equal(refreshCalls, 1)
  assert.equal(getAccessToken(), 'fresh-access')
  assert.deepEqual([...routeCalls.values()], [2, 2, 2, 2])
  assert.equal(webStorageCalls, 0)
})

test('a late 401 from the old access generation replays without opening a second refresh flight', async () => {
  setAuthenticated(access('old-generation'), account)
  let refreshCalls = 0
  let protectedCalls = 0
  let releaseLateResponse = () => undefined
  const lateResponse = new Promise<void>((resolve) => {
    releaseLateResponse = resolve
  })
  globalThis.fetch = async (input, init) => {
    if (String(input).endsWith('/auth/refresh')) {
      refreshCalls += 1
      return Response.json(access('new-generation'))
    }
    protectedCalls += 1
    const authorization = new Headers(init?.headers).get('Authorization')
    if (authorization === 'Bearer new-generation') {
      return Response.json({ items: [], total: 0, page: 1, page_size: 20 })
    }
    if (protectedCalls === 2) await lateResponse
    return Response.json({ error: { code: 'unauthorized', message: '未认证' } }, { status: 401 })
  }

  const first = listCustomers()
  const late = listCustomers()
  await first
  releaseLateResponse()
  await late

  assert.equal(refreshCalls, 1)
  assert.equal(getAccessToken(), 'new-generation')
})

test('a late account-A 401 cannot replay its payload with account-B credentials', async () => {
  setAuthenticated(access('account-A-access'), { ...account, id: 'account-a' })
  const protectedAuthorizations: Array<string | null> = []
  let releaseAccountAResponse = () => undefined
  const accountAResponse = new Promise<void>((resolve) => {
    releaseAccountAResponse = resolve
  })
  globalThis.fetch = async (input, init) => {
    const url = String(input)
    const authorization = new Headers(init?.headers).get('Authorization')
    if (url.endsWith('/me')) {
      assert.equal(authorization, 'Bearer account-B-access')
      return Response.json({ ...account, id: 'account-b' })
    }
    protectedAuthorizations.push(authorization)
    await accountAResponse
    return Response.json({ error: { code: 'unauthorized', message: '未认证' } }, { status: 401 })
  }

  const accountARequest = listCustomers()
  assert.equal(await establishSession(async () => access('account-B-access')), true)
  releaseAccountAResponse()
  await assert.rejects(accountARequest, (error: unknown) => error instanceof ApiError && error.status === 401)

  assert.deepEqual(protectedAuthorizations, ['Bearer account-A-access'])
  const current = getAuthSnapshot()
  assert.equal(current.status, 'authenticated')
  assert.equal(current.status === 'authenticated' ? current.account.id : null, 'account-b')
  assert.equal(getAccessToken(), 'account-B-access')
})

test('logout invalidates an in-flight refresh before it can restore authentication', async () => {
  setAuthenticated(access('pre-logout-access'), account)
  const order: string[] = []
  let releaseRefresh = () => undefined
  const refreshResponse = new Promise<void>((resolve) => {
    releaseRefresh = resolve
  })
  globalThis.fetch = async (input) => {
    const url = String(input)
    if (url.endsWith('/auth/refresh')) {
      order.push('refresh-start')
      await refreshResponse
      order.push('refresh-response')
      return Response.json(access('post-logout-access'))
    }
    if (url.endsWith('/auth/logout')) {
      order.push('logout')
      return new Response(null, { status: 204 })
    }
    return Response.json({ error: { code: 'unauthorized', message: '未认证' } }, { status: 401 })
  }

  const protectedRequest = listCustomers()
  await new Promise((resolve) => setTimeout(resolve, 0))
  const logout = logoutSession()
  assert.equal(getAuthSnapshot().status, 'anonymous')
  releaseRefresh()
  await logout
  await assert.rejects(protectedRequest, (error: unknown) => error instanceof ApiError && error.status === 401)

  assert.deepEqual(order, ['refresh-start', 'refresh-response', 'logout'])
  assert.equal(getAuthSnapshot().status, 'anonymous')
  assert.equal(getAccessToken(), null)
})

test('a late auth action cannot overwrite the newer session action', async () => {
  setAnonymous()
  let releaseFirstAction = () => undefined
  const firstActionResponse = new Promise<void>((resolve) => {
    releaseFirstAction = resolve
  })
  globalThis.fetch = async (_input, init) => {
    const authorization = new Headers(init?.headers).get('Authorization')
    if (authorization === 'Bearer account-B-access') {
      return Response.json({ ...account, id: 'account-b' })
    }
    throw new Error(`unexpected current-account authorization: ${authorization}`)
  }

  const first = establishSession(async () => {
    await firstActionResponse
    return access('account-A-access')
  })
  await new Promise((resolve) => setTimeout(resolve, 0))
  const second = establishSession(async () => access('account-B-access'))
  releaseFirstAction()

  assert.equal(await first, false)
  assert.equal(await second, true)
  const current = getAuthSnapshot()
  assert.equal(current.status, 'authenticated')
  assert.equal(current.status === 'authenticated' ? current.account.id : null, 'account-b')
  assert.equal(getAccessToken(), 'account-B-access')
})

test('network, 403, 429, and 5xx failures never start refresh', async () => {
  setAuthenticated(access('current-access'), account)
  let refreshCalls = 0
  for (const status of [403, 429, 500]) {
    globalThis.fetch = async (input) => {
      if (String(input).endsWith('/auth/refresh')) refreshCalls += 1
      return Response.json({ error: { code: 'fixture', message: 'fixture' } }, { status })
    }
    await assert.rejects(listCustomers(), (error: unknown) => error instanceof ApiError && error.status === status)
  }
  globalThis.fetch = async (input) => {
    if (String(input).endsWith('/auth/refresh')) refreshCalls += 1
    throw new TypeError('synthetic network failure')
  }
  await assert.rejects(listCustomers(), /synthetic network failure/)
  assert.equal(refreshCalls, 0)
  assert.equal(getAuthSnapshot().status, 'authenticated')
})

test('a replayed 401 does not refresh twice and returns to anonymous', async () => {
  setAuthenticated(access('expired-again'), account)
  let protectedCalls = 0
  let refreshCalls = 0
  globalThis.fetch = async (input) => {
    if (String(input).endsWith('/auth/refresh')) {
      refreshCalls += 1
      return Response.json(access('still-invalid'))
    }
    protectedCalls += 1
    return Response.json({ error: { code: 'unauthorized', message: '未认证' } }, { status: 401 })
  }

  await assert.rejects(listCustomers(), (error: unknown) => error instanceof ApiError && error.status === 401)

  assert.equal(refreshCalls, 1)
  assert.equal(protectedCalls, 2)
  assert.equal(getAuthSnapshot().status, 'anonymous')
  assert.equal(getAccessToken(), null)
})

test('verification action token is consumed from fragment and removed before use', () => {
  let replaced = ''
  const token = consumeActionToken(
    { hash: '#token=selector.synthetic-secret', pathname: '/verify-email', search: '?source=mail' },
    { state: null, replaceState: (_state, _unused, url) => { replaced = String(url) } },
  )
  assert.equal(token, 'selector.synthetic-secret')
  assert.equal(replaced, '/verify-email?source=mail')
  assert.equal(webStorageCalls, 0)
})

test('auth routes and pages are mounted without the legacy storage token contract', async () => {
  // D11：ChangePasswordPage/SettingsPage → ChangePasswordForm / AccountSecurityPage / AccountMenu
  const [
    app,
    loginPage,
    registerPage,
    verifyPage,
    forgotPage,
    resetPage,
    changeForm,
    accountMenu,
    securityPage,
    appShell,
    welcomePage,
    sessionSource,
    makefile,
  ] = await Promise.all([
    readFile('src/App.tsx', 'utf8'),
    readFile('src/pages/auth/LoginPage.tsx', 'utf8'),
    readFile('src/pages/auth/RegisterPage.tsx', 'utf8'),
    readFile('src/pages/auth/VerifyEmailPage.tsx', 'utf8'),
    readFile('src/pages/auth/ForgotPasswordPage.tsx', 'utf8'),
    readFile('src/pages/auth/ResetPasswordPage.tsx', 'utf8'),
    readFile('src/account/ChangePasswordForm.tsx', 'utf8'),
    readFile('src/account/AccountMenu.tsx', 'utf8'),
    readFile('src/account/AccountSecurityPage.tsx', 'utf8'),
    readFile('src/components/AppShell.tsx', 'utf8'),
    readFile('src/pages/WelcomePage.tsx', 'utf8'),
    readFile('src/auth/session.ts', 'utf8'),
    readFile('../Makefile', 'utf8'),
  ])

  assert.match(app, /path="\/register"/)
  assert.match(app, /path="\/verify-email"/)
  assert.match(app, /path="\/forgot-password"/)
  assert.match(app, /path="\/reset-password"/)
  assert.match(app, /path="\/change-password"/)
  assert.match(app, /Navigate to="\/account\/security\/password" replace/)
  // Characterization (plan-share S7): action-token routes and anonymous share skip session restore.
  // /shared/plans/:token is a public route — no AnonymousEntry, no AppShell.
  assert.match(
    app,
    /isActionTokenRoute = location\.pathname === '\/verify-email' \|\| location\.pathname === '\/reset-password'/,
  )
  assert.match(app, /isPublicShareRoute = location\.pathname\.startsWith\('\/shared\/plans\/'\)/)
  assert.match(app, /skipSessionRestore = isActionTokenRoute \|\| isPublicShareRoute/)
  assert.match(app, /path="\/shared\/plans\/:token"/)
  assert.match(app, /element=\{<SharedPlanPage \/>\}/)
  const shareRoute = app.match(/<Route path="\/shared\/plans\/:token" element=\{<SharedPlanPage \/>\} \/>/)?.[0] ?? ''
  assert.equal(shareRoute, '<Route path="/shared/plans/:token" element={<SharedPlanPage />} />')
  assert.doesNotMatch(shareRoute, /AnonymousEntry|AppShell|RequireAuth/)
  const transport = await readFile('src/api/transport.ts', 'utf8')
  // Characterization (plan-share S1/S3/S7): publicRequest still defaults to same-origin;
  // sharedRequest uses credentials omit for anonymous share API.
  assert.match(transport, /credentials: init\.credentials \?\? 'same-origin'/)
  assert.match(transport, /export async function sharedRequest/)
  assert.match(transport, /credentials: 'omit'/)
  assert.match(transport, /referrerPolicy: 'no-referrer'/)
  assert.match(loginPage, /login\(email, password\)/)
  assert.match(loginPage, /to="\/forgot-password"/)
  assert.doesNotMatch(loginPage, /手机号|短信|验证码/)
  assert.match(registerPage, /fetchAuthCapabilities/)
  assert.match(registerPage, /capability === 'open' && !submitted/)
  assert.match(verifyPage, /consumeActionToken/)
  assert.match(verifyPage, /useLayoutEffect/)
  assert.match(forgotPage, /forgotPassword\(email\)/)
  assert.match(forgotPage, /如果该邮箱可用于密码恢复/)
  assert.match(resetPage, /consumeActionToken/)
  assert.match(resetPage, /useLayoutEffect/)
  assert.match(resetPage, /if \(started\.current\) return/)
  assert.match(resetPage, /resetPassword\(tokenRef\.current, password\)/)
  assert.match(changeForm, /changePassword\(currentPassword, password\)/)
  assert.match(changeForm, /setAnonymous\(\)/)
  assert.match(accountMenu, /logoutSession\(\)/)
  assert.match(securityPage, /to="\/account\/security\/password"/)
  assert.match(securityPage, /logoutSession\(\)/)
  assert.match(welcomePage, /fetchAuthCapabilities/)
  assert.match(welcomePage, /to="\/register"/)
  // 注册守卫：门禁不再逐个登记 runner，test-frontend 用 glob 发现 scripts/*.test.*，
  // 本文件因此自动入闸。改为钉住发现机制——换回手工清单会让这条失败。
  assert.match(makefile, /node --test .*"scripts\/\*\.test\.ts" "scripts\/\*\.test\.mjs"/)
  assert.doesNotMatch(
    [app, loginPage, registerPage, verifyPage, forgotPage, resetPage, changeForm, accountMenu, securityPage, appShell, welcomePage, sessionSource].join('\n'),
    /crm_token/,
  )
  assert.doesNotMatch([forgotPage, resetPage, changeForm].join('\n'), /localStorage|sessionStorage|indexedDB|console\./)
  assert.doesNotMatch(sessionSource, /localStorage|sessionStorage|indexedDB/)
})