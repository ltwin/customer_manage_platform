import test from 'node:test'
import assert from 'node:assert/strict'
import { setAuthenticated, setAnonymous } from '../src/auth/session.ts'
import { send, read } from '../src/creative-canvas/editor/api.ts'
import type { Canvas } from '../src/creative-canvas/editor/api.ts'
import {
  matchesCanvas,
  newerCanvas,
} from '../src/creative-canvas/editor/snapshot.ts'
const account = {
  id: 'a',
  email: 'a@example.invalid',
  created_at: '2026-01-01T00:00:00Z',
  timezone: 'Asia/Shanghai',
}
const grant = (token: string) => ({
  access_token: token,
  token_type: 'Bearer' as const,
  expires_in: 600 as const,
})
const denied = () =>
  new Response(
    JSON.stringify({ error: { code: 'unauthorized', message: 'expired' } }),
    { status: 401 },
  )

test('expired access refreshes once and resends the original body and operation', async () => {
  const original = globalThis.fetch
  setAuthenticated(grant('old'), account)
  const sent: { body: unknown; key: string | null; token: string | null }[] = []
  let refreshes = 0
  globalThis.fetch = async (input, init) => {
    if (String(input).endsWith('/auth/refresh')) {
      refreshes++
      return Response.json(grant('fresh'))
    }
    const headers = new Headers(init?.headers)
    sent.push({
      body: init?.body,
      key: headers.get('Idempotency-Key'),
      token: headers.get('Authorization'),
    })
    return sent.length === 1 ? denied() : Response.json({ saved: true })
  }
  try {
    await send('a', '/assets', '{"fixed":"body"}', 'fixed-operation')
    assert.equal(refreshes, 1)
    assert.equal(sent.length, 2)
    assert.equal(sent[0].body, sent[1].body)
    assert.equal(sent[0].key, sent[1].key)
    assert.equal(sent[1].token, 'Bearer fresh')
  } finally {
    globalThis.fetch = original
    setAnonymous()
  }
})
test('account switch during refresh never resends an old account operation', async () => {
  const original = globalThis.fetch
  setAuthenticated(grant('old'), account)
  let writes = 0
  globalThis.fetch = async (input) => {
    if (String(input).endsWith('/auth/refresh')) {
      setAuthenticated(grant('other'), { ...account, id: 'b' })
      return Response.json(grant('late-old'))
    }
    writes++
    return denied()
  }
  try {
    await assert.rejects(send('a', '/assets', '{}', 'op'))
    assert.equal(writes, 1)
  } finally {
    globalThis.fetch = original
    setAnonymous()
  }
})
test('receipt reads also recover expired credentials', async () => {
  const original = globalThis.fetch
  setAuthenticated(grant('old'), account)
  let requests = 0
  globalThis.fetch = async (input) =>
    String(input).endsWith('/auth/refresh')
      ? Response.json(grant('fresh'))
      : ++requests === 1
        ? denied()
        : Response.json({ operation_id: 'op' })
  try {
    assert.deepEqual(await read('/operations/op'), { operation_id: 'op' })
    assert.equal(requests, 2)
  } finally {
    globalThis.fetch = original
    setAnonymous()
  }
})
test('same revision authorization projection is accepted; wrong canvas is never writable', () => {
  const initial: Canvas = {
    id: 'a',
    project_id: 'p',
    project_name: 'project',
    project_revision: '1',
    revision: '1',
    topology_revision: '1',
    archived: false,
    edges: [],
    node_inputs: [],
    changes: [],
    object_states: [],
    nodes: [
      {
        id: 'n',
        parent_id: null,
        metadata: {
          type_key: 'core.text',
          type_version: 1,
          title: '',
          intent: '',
          x: 0,
          y: 0,
          width: 280,
          height: 180,
          z_order: 0,
        },
        prompt: null,
        capabilities: {
          actions: [],
          prompt_mode: 'draft',
          disabled_reason: null,
        },
        status: {
          content_state: 'ready',
          generation_state: 'idle',
          active_execution_id: null,
          latest_execution_id: null,
          apply_state: null,
          error: null,
          status_revision: '1',
        },
        placement_revision: '1',
        data_revision: '1',
        data: {
          schema_version: 1,
          config: {},
          content_id: 'c',
          content_revision_id: 'r',
          selected_version_id: null,
          document_id: null,
        },
        content: {
          id: 'r',
          content_id: 'c',
          kind: 'text',
          sequence: '1',
          truncated: false,
          payload: { body: 'secret' },
        },
      },
    ],
  }
  const revoked = structuredClone(initial)
  revoked.nodes[0].status.content_state = 'unavailable'
  delete revoked.nodes[0].content
  assert.equal(newerCanvas(initial, revoked), revoked)
  assert.equal(matchesCanvas(initial, 'b'), false)
  assert.equal(matchesCanvas(null, 'b'), false)
})
