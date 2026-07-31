import assert from 'node:assert/strict'
import test from 'node:test'

import {
  ApiError,
  createOrder,
  createScheduleSlot,
  listCustomers,
  listOrders,
  listScheduleSlots,
} from '../src/api/client.ts'
import { getAccessToken, setAuthenticated } from '../src/auth/session.ts'

const authAccount = {
  id: 'account-fixture', email: 'fixture@example.invalid',
  created_at: '2026-07-31T00:00:00Z', timezone: 'Asia/Shanghai',
}
const access = (token: string) => ({ access_token: token, token_type: 'Bearer' as const, expires_in: 600 as const })

const storage = new Map<string, string>()
Object.defineProperty(globalThis, 'localStorage', {
  configurable: true,
  value: {
    getItem: (key: string) => storage.get(key) ?? null,
    setItem: (key: string, value: string) => storage.set(key, value),
    removeItem: (key: string) => storage.delete(key),
    clear: () => storage.clear(),
    key: (index: number) => [...storage.keys()][index] ?? null,
    get length() { return storage.size },
  } satisfies Storage,
})

test('ApiError preserves generated typed details', async () => {
  globalThis.fetch = async () => new Response(JSON.stringify({
    error: {
      code: 'order_in_use',
      message: '订单仍被拍摄档期引用',
      details: {
        schedule_slot_id: 'slot-1',
        schedule_start_at: '2026-07-10T08:00:00Z',
      },
    },
  }), { status: 409, headers: { 'Content-Type': 'application/json' } })

  await assert.rejects(
    listOrders(),
    (error: unknown) => error instanceof ApiError &&
      error.code === 'order_in_use' &&
      error.details?.schedule_slot_id === 'slot-1' &&
      error.details.schedule_start_at === '2026-07-10T08:00:00Z',
  )
})

test('order client sends schedulable_at and Idempotency-Key', async () => {
  const requests: Array<{ url: string; init?: RequestInit }> = []
  globalThis.fetch = async (input, init) => {
    requests.push({ url: String(input), init })
    if (String(input).includes('?')) {
      return new Response(JSON.stringify({ items: [], total: 0 }), { status: 200 })
    }
    return new Response(JSON.stringify({
      id: 'ord-1',
      account_id: 'acct-1',
      created_at: '2026-07-10T08:00:00Z',
      customer_id: 'cus-1',
      status: 'consulting',
      deposit_paid: false,
      balance_paid: false,
    }), { status: 201 })
  }

  await listOrders({ schedulableAt: '2026-07-10T10:00:00Z' })
  await createOrder({ customer_id: 'cus-1', creation_mode: 'new' }, 'attempt-key')

  assert.match(requests[0]?.url ?? '', /schedulable_at=2026-07-10T10%3A00%3A00Z/)
  assert.equal(new Headers(requests[1]?.init?.headers).get('Idempotency-Key'), 'attempt-key')
})

test('schedule client sends range and per-attempt key', async () => {
  const requests: Array<{ url: string; init?: RequestInit }> = []
  globalThis.fetch = async (input, init) => {
    requests.push({ url: String(input), init })
    if (init?.method === 'POST') {
      return new Response(JSON.stringify({
        slot: {
          id: 'slot-1', account_id: 'acct-1', created_at: '2026-07-10T08:00:00Z',
          start_at: '2026-07-10T08:00:00Z', end_at: '2026-07-10T09:00:00Z', type: 'hold',
        },
        overlaps: [],
      }), { status: 201 })
    }
    return new Response('[]', { status: 200 })
  }

  await listScheduleSlots('2026-07-01T00:00:00Z', '2026-08-01T00:00:00Z')
  await createScheduleSlot({
    start_at: '2026-07-10T08:00:00Z',
    end_at: '2026-07-10T09:00:00Z',
    type: 'hold',
  }, 'slot-attempt-key')

  assert.match(requests[0]?.url ?? '', /schedule\/slots\?from=/)
  assert.equal(new Headers(requests[1]?.init?.headers).get('Idempotency-Key'), 'slot-attempt-key')
})

test('a protected 401 refreshes once and becomes anonymous when refresh also fails', async () => {
  setAuthenticated(access('synthetic-token'), authAccount)
  let refreshCalls = 0
  globalThis.fetch = async (input) => {
    if (String(input).endsWith('/auth/refresh')) refreshCalls += 1
    return Response.json({
      error: { code: 'unauthorized', message: '未认证' },
    }, { status: 401 })
  }

  await assert.rejects(
    listCustomers({ page: 1, pageSize: 20 }),
    (error: unknown) => error instanceof ApiError && error.status === 401,
  )
  assert.equal(refreshCalls, 1)
  assert.equal(getAccessToken(), null)
})
