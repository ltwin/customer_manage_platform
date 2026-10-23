import assert from 'node:assert/strict'
import test from 'node:test'
import { hostGuides } from '../src/creative-canvas/agents/hostGuides.ts'
import { callbackHost, decodeRequest } from '../src/pages/auth/consentRequest.ts'

const mcpUrl = 'https://crm.example.com/mcp'

test('every host guide uses the server-given MCP address and the global config commands', () => {
  const guides = hostGuides(mcpUrl)
  assert.deepEqual(guides.map((g) => g.id), ['claude_code', 'codex', 'claude', 'chatgpt'])
  const byID = Object.fromEntries(guides.map((g) => [g.id, g]))
  assert.match(byID.claude_code.agent?.text ?? '', /claude mcp add --transport http --scope user creative https:\/\/crm\.example\.com\/mcp/)
  assert.match(byID.codex.agent?.text ?? '', /codex mcp add creative --url https:\/\/crm\.example\.com\/mcp/)
  assert.match(byID.codex.agent?.text ?? '', /codex mcp login creative/)
  for (const id of ['claude', 'chatgpt'] as const) {
    assert.ok(byID[id].steps?.some((s) => s.includes(mcpUrl)), `${id} steps name the address`)
    assert.ok(byID[id].fallback?.copy.text.includes(mcpUrl), `${id} admin text names the address`)
  }
  assert.equal(byID.claude.fallback?.switchTo, 'claude_code')
})

function pack(payload: object): string {
  const json = new TextEncoder().encode(JSON.stringify(payload))
  const b64 = btoa(String.fromCharCode(...json)).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '')
  return `v1.${b64}.signature`
}

test('the consent page reads who is asking from the signed request', () => {
  const request = decodeRequest(pack({ client_id: 'oclient_1', client_name: '创意 Claude Code', registration: 'dcr', redirect_uri: 'http://127.0.0.1:51234/callback', display_name: 'Claude Code', exp: 1_900_000_000 }))
  assert.equal(request?.client_name, '创意 Claude Code')
  assert.equal(request?.display_name, 'Claude Code')
  assert.equal(request?.registration, 'dcr')
  assert.deepEqual(callbackHost(request?.redirect_uri ?? ''), { host: '127.0.0.1:51234', loopback: true })
  assert.deepEqual(callbackHost('https://claude.ai/api/mcp/auth_callback'), { host: 'claude.ai', loopback: false })
  assert.equal(decodeRequest(null), null)
  assert.equal(decodeRequest('v1.notbase64!.x'), null)
  assert.equal(decodeRequest(pack({ client_name: 'x' })), null)
})
